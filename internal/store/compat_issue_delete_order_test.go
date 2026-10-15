//go:build integration

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Authoring gate: real DELETE must serialize with parent/byte captures on both
// sides of its cut, and its bulk caller must acquire actual signed gate keys.
// CaptureItems gate-only and bulk PUT tests cannot catch a DELETE caller that
// bypasses those contracts. Use isolated schemas and observed PG blockers only.
func deleteOrderParts(label string) CaptureItems {
	return CaptureItems{
		Attachments:   []CaptureAttachment{{Filename: label + ".bin", ContentType: "application/octet-stream", Data: []byte(label + "\x00\xff")}},
		EnvelopeItems: []CaptureEnvelopeItem{{Type: label, Header: json.RawMessage(fmt.Sprintf(`{"type":%q}`, label)), Data: []byte(label + "\x00\xfe")}},
	}
}

func deleteOrderRows(t *testing.T, f resolutionFixture, want ...int) {
	t.Helper()
	var issues, events, attachments, opaque int
	err := f.db.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM issues),(SELECT count(*) FROM events),
 (SELECT count(*) FROM attachments),(SELECT count(*) FROM envelope_items)`).Scan(&issues, &events, &attachments, &opaque)
	if got := []int{issues, events, attachments, opaque}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("actual issue/event/attachment/opaque rows=%v want=%v err=%v", got, want, err)
	}
}

func deleteOrderRoute(t *testing.T, f resolutionFixture, fingerprint string) (string, *string) {
	t.Helper()
	var id string
	var origin *string
	if err := f.db.pool.QueryRow(f.ctx, "SELECT id::text,origin_issue_id::text FROM issue_hash_routes WHERE project_id=$1::bigint AND fingerprint=$2", f.project.ID, fingerprint).Scan(&id, &origin); err != nil {
		t.Fatal(err)
	}
	return id, origin
}

func deleteOrderAwait(t *testing.T, ctx context.Context, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("real DELETE did not finish after its observed blocker was released")
	}
}

func deleteOrderGateWait(t *testing.T, f resolutionFixture, observer pgx.Tx, blockerPID int, project, mode string) {
	t.Helper()
	pid := groupWaitBlocked(t, f.ctx, observer, blockerPID, 1)[0]
	var waiting bool
	err := observer.QueryRow(f.ctx, `WITH wanted AS (
 SELECT hashtextextended(current_schema()||':issue-group:'||$2::text,0) AS key)
 SELECT EXISTS(SELECT 1 FROM pg_locks l,wanted WHERE l.pid=$1 AND l.locktype='advisory'
 AND NOT l.granted AND l.mode=$3 AND l.objsubid=1
 AND l.classid=((wanted.key>>32)&4294967295)::oid AND l.objid=(wanted.key&4294967295)::oid)`, pid, project, mode).Scan(&waiting)
	if err != nil || !waiting {
		t.Fatalf("PID%d was blocked for a reason other than the project's exact %s gate: project=%s waiting=%v err=%v", pid, mode, project, waiting, err)
	}
}

func TestDeleteCompatibleIssueWaitsForAtomicCaptureItems(t *testing.T) {
	f := resolutionStore(t)
	old := groupEvent(t, 1, "delete-items-before", "Old receipt", time.Now().Add(-time.Hour))
	root, _, err := f.db.Ingest(f.ctx, f.project.ID, old, deleteOrderParts("old"))
	if err != nil || root == "" {
		t.Fatalf("initial capture root=%s err=%v", root, err)
	}
	route, _ := deleteOrderRoute(t, f, old.Fingerprint)
	barrier, barrierPID := captureItemsBarrier(t, f)
	if _, err = barrier.Exec(f.ctx, "LOCK TABLE attachments IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	capturer, deleter := captureItemsOther(t, f), captureItemsOther(t, f)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	next := groupEvent(t, 2, "delete-items-before", "In-flight receipt", old.Timestamp.Add(time.Second))
	captureDone := make(chan captureItemsResult, 1)
	go func() {
		id, fresh, err := capturer.Ingest(ctx, f.project.ID, next, deleteOrderParts("inflight"))
		captureDone <- captureItemsResult{id, fresh, err}
	}()
	capturePID := groupWaitBlocked(t, f.ctx, barrier, barrierPID, 1)[0]
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- deleter.DeleteCompatibleIssue(ctx, f.access, root) }()
	deleteOrderGateWait(t, f, barrier, capturePID, f.project.ID, "ExclusiveLock")
	if err = barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if result := captureItemsAwait(t, captureDone); result.err != nil || !result.fresh || result.issue != root {
		t.Fatalf("capture preceding DELETE did not commit to its existing group: %+v", result)
	}
	deleteOrderAwait(t, ctx, deleteDone)
	deleteOrderRows(t, f, 0, 0, 0, 0)
	if id, origin := deleteOrderRoute(t, f, old.Fingerprint); id != route || origin != nil {
		t.Fatalf("DELETE did not retire the captured route without changing its identity: id=%s origin=%v", id, origin)
	}
}

func TestDeleteCompatibleIssueOrdersFreshAndDuplicateItemsAfterItsCut(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate=%v", duplicate), func(t *testing.T) {
			f := resolutionStore(t)
			old := groupEvent(t, 1, "delete-items-after", "Old receipt", time.Now().Add(-time.Hour))
			root, _, err := f.db.Ingest(f.ctx, f.project.ID, old, deleteOrderParts("old"))
			if err != nil || root == "" {
				t.Fatalf("initial capture root=%s err=%v", root, err)
			}
			route, _ := deleteOrderRoute(t, f, old.Fingerprint)
			barrier, barrierPID := captureItemsBarrier(t, f)
			if _, err = barrier.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", root); err != nil {
				t.Fatal(err)
			}
			capturer, deleter := captureItemsOther(t, f), captureItemsOther(t, f)
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			deleteDone := make(chan error, 1)
			go func() { deleteDone <- deleter.DeleteCompatibleIssue(ctx, f.access, root) }()
			deletePID := groupWaitBlocked(t, f.ctx, barrier, barrierPID, 1)[0]
			next := groupEvent(t, 2, "delete-items-after", "New generation receipt", old.Timestamp.Add(time.Second))
			incoming, parts := next, deleteOrderParts("new")
			if duplicate {
				incoming, parts = old, deleteOrderParts("stale")
			}
			captureDone := make(chan captureItemsResult, 1)
			go func() {
				id, fresh, err := capturer.Ingest(ctx, f.project.ID, incoming, parts)
				captureDone <- captureItemsResult{id, fresh, err}
			}()
			deleteOrderGateWait(t, f, barrier, deletePID, f.project.ID, "ShareLock")
			if err = barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			deleteOrderAwait(t, ctx, deleteDone)
			result := captureItemsAwait(t, captureDone)
			if result.err != nil {
				t.Fatal(result.err)
			}
			if duplicate {
				if result.fresh || result.issue != root {
					t.Fatalf("deleted duplicate receipt did not retain its acknowledgment: %+v", result)
				}
				deleteOrderRows(t, f, 0, 0, 0, 0)
				if id, origin := deleteOrderRoute(t, f, old.Fingerprint); id != route || origin != nil {
					t.Fatal("duplicate children reactivated a deleted route")
				}
				id, fresh, err := capturer.Ingest(f.ctx, f.project.ID, next, deleteOrderParts("new"))
				result = captureItemsResult{id, fresh, err}
			}
			if result.err != nil || !result.fresh || result.issue == "" || result.issue == root {
				t.Fatalf("later capture did not create its own generation: %+v", result)
			}
			deleteOrderRows(t, f, 1, 1, 1, 1)
			if id, origin := deleteOrderRoute(t, f, old.Fingerprint); id != route || origin == nil || *origin != result.issue {
				t.Fatalf("new generation lost the stable route identity: id=%s origin=%v result=%+v", id, origin, result)
			}
			var eventID, owner, original, filename, itemType string
			var attachment, opaque []byte
			err = f.db.pool.QueryRow(f.ctx, `SELECT e.event_id,e.issue_id::text,e.original_issue_id::text,a.filename,a.data,i.item_type,i.data
 FROM events e JOIN attachments a ON a.project_id=e.project_id AND a.event_id=e.event_id
 JOIN envelope_items i ON i.project_id=e.project_id AND i.event_id=e.event_id`).Scan(&eventID, &owner, &original, &filename, &attachment, &itemType, &opaque)
			want := deleteOrderParts("new")
			if err != nil || eventID != next.ID || owner != result.issue || original != result.issue || filename != "new.bin" || itemType != "new" || !bytes.Equal(attachment, want.Attachments[0].Data) || !bytes.Equal(opaque, want.EnvelopeItems[0].Data) {
				t.Fatalf("old raw/bytes were resurrected instead of the later capture: event=%s owner=%s original=%s file=%s type=%s err=%v", eventID, owner, original, filename, itemType, err)
			}
		})
	}
}

func TestDeleteCompatibleIssuesOrdersActualSignedProjectGates(t *testing.T) {
	f := resolutionStore(t)
	gateKey := func(project string) int64 {
		t.Helper()
		var key int64
		if err := f.db.pool.QueryRow(f.ctx, "SELECT hashtextextended(current_schema()||':issue-group:'||$1::text,0)", project).Scan(&key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	first, higherKey := f.project, gateKey(f.project.ID)
	var second Project
	var lowerKey int64
	for n := range 32 {
		var err error
		second, err = f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, fmt.Sprintf("Deletion signed gate %d", n), "go")
		if err != nil {
			t.Fatal(err)
		}
		lowerKey = gateKey(second.ID)
		if lowerKey < higherKey {
			break
		}
		first, higherKey = second, lowerKey
	}
	if lowerKey >= higherKey || apiTokenProjectID(t, first.ID) >= apiTokenProjectID(t, second.ID) {
		t.Fatal("fixture did not obtain reversed numeric-project/signed-key order")
	}
	stamp := time.Now().Add(-time.Hour)
	a := groupCapture(t, f.ctx, f.db, first, groupEvent(t, 1, "delete-gate-a", "A", stamp.Add(5*time.Second)))
	aChild := groupCapture(t, f.ctx, f.db, first, groupEvent(t, 2, "delete-gate-a-child", "A child", stamp.Add(6*time.Second)))
	b := groupCapture(t, f.ctx, f.db, second, groupEvent(t, 1, "delete-gate-b", "B", stamp))
	bChild := groupCapture(t, f.ctx, f.db, second, groupEvent(t, 2, "delete-gate-b-child", "B child", stamp.Add(time.Second)))
	for _, group := range [][]string{{a, aChild}, {b, bChild}} {
		if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, group); err != nil {
			t.Fatal(err)
		}
	}
	barrier, barrierPID := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, "SELECT pg_advisory_xact_lock($1::bigint)", lowerKey); err != nil {
		t.Fatal(err)
	}
	writer := captureItemsOther(t, f)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- writer.DeleteCompatibleIssues(ctx, f.access, "", CompatibleIssueBulkFilter{IDs: []string{a, b}, Limit: 1000})
	}()
	deleteOrderGateWait(t, f, barrier, barrierPID, second.ID, "ExclusiveLock")
	probe, _ := captureItemsBarrier(t, f)
	var available bool
	if err := probe.QueryRow(f.ctx, "SELECT pg_try_advisory_xact_lock($1::bigint)", higherKey).Scan(&available); err != nil || !available {
		t.Fatalf("DELETE acquired the later signed gate while waiting for the earlier key: available=%v err=%v", available, err)
	}
	if err := probe.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, ctx, done)
	deleteOrderRows(t, f, 0, 0, 0, 0)
	var routes, active int
	if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*),count(origin_issue_id) FROM issue_hash_routes").Scan(&routes, &active); err != nil || routes != 4 || active != 0 {
		t.Fatalf("bulk DELETE did not remove both complete groups while retaining inactive routes: routes=%d active=%d err=%v", routes, active, err)
	}
}
