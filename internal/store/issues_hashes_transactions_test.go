//go:build integration

package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
)

type hashCaptureResult struct {
	id    string
	fresh bool
	err   error
}

func hashWaitBlockedByEither(t *testing.T, f resolutionFixture, observer pgx.Tx, first, second int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := observer.Exec(f.ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
			t.Fatal(err)
		}
		var waiting int
		if err := observer.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity a WHERE pg_blocking_pids(a.pid) && $1::int[]`, []int{first, second}).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("two independent allocations did not wait on the held route: %d", waiting)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCompatibleIssueHashesCaptureCommitBeforeResetRemainsHistorical(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	first := groupEvent(t, 1, "hash-order", "First", stamp)
	root := groupCapture(t, f.ctx, f.db, f.project, first)
	hash := hashForSample(t, hashSamples(t, f, root, CompatibleIssueHashFilter{Limit: 100}), first.ID)
	writer, err := Open(f.ctx, f.db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	blocker, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(f.ctx)
	if _, err = blocker.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", root); err != nil {
		t.Fatal(err)
	}
	committing := groupEvent(t, 2, "hash-order", "Admitted before reset", stamp.Add(time.Second))
	captureDone := make(chan hashCaptureResult, 1)
	go func() {
		id, fresh, err := writer.Ingest(f.ctx, f.project.ID, committing)
		captureDone <- hashCaptureResult{id, fresh, err}
	}()
	capturePIDs := groupWaitBlocked(t, f.ctx, blocker, int(blocker.Conn().PgConn().PID()), 1)
	resetDone := make(chan error, 1)
	go func() { resetDone <- f.db.ResetCompatibleIssueHashes(f.ctx, f.access, root, []string{hash}) }()
	// Capture already holds the shared project gate while awaiting the issue
	// row. Reset must wait on that actual capture, not overtake its receipt.
	groupWaitBlocked(t, f.ctx, blocker, capturePIDs[0], 1)
	if err = blocker.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-captureDone:
		if got.err != nil || !got.fresh || got.id != root {
			t.Fatalf("capture admitted before reset changed generation: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("capture did not complete after releasing its issue row")
	}
	select {
	case err := <-resetDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reset deadlocked behind committed capture")
	}
	if old := triageIssue(t, f.ctx, f.db, f.owner.ID, root); old.Count != 2 {
		t.Fatalf("reset moved or dropped the preceding capture: %+v", old)
	}
	if items := hashSamples(t, f, root, CompatibleIssueHashFilter{Limit: 100}); len(items) != 0 {
		t.Fatal("committed reset left the signature active")
	}
	newRoot := groupCapture(t, f.ctx, writer, f.project, groupEvent(t, 3, "hash-order", "After reset", stamp.Add(2*time.Second)))
	if newRoot == root || triageIssue(t, f.ctx, f.db, f.owner.ID, newRoot).Count != 1 {
		t.Fatal("capture admitted after reset did not create one new generation")
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	if id, fresh, err := writer.Ingest(f.ctx, f.project.ID, committing); err != nil || fresh || id != root {
		t.Fatalf("late retry lost historical current owner: id=%s fresh=%v error=%v", id, fresh, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssueHashesCanceledResetAndConcurrentAllocationAcrossPools(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	aEvent := groupEvent(t, 1, "hash-concurrent-a", "A", stamp)
	a := groupCapture(t, f.ctx, f.db, f.project, aEvent)
	b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "hash-concurrent-b", "B", stamp))
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	hash := hashForSample(t, hashSamples(t, f, a, CompatibleIssueHashFilter{Limit: 100}), aEvent.ID)
	writer, err := Open(f.ctx, f.db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	blocker, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(f.ctx)
	if _, err = blocker.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", a); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	resetCtx, cancelReset := context.WithCancel(f.ctx)
	defer cancelReset()
	resetDone := make(chan error, 1)
	go func() { resetDone <- writer.ResetCompatibleIssueHashes(resetCtx, f.access, a, []string{hash}) }()
	resetPIDs := groupWaitBlocked(t, f.ctx, blocker, int(blocker.Conn().PgConn().PID()), 1)
	captureCtx, cancelCapture := context.WithCancel(f.ctx)
	defer cancelCapture()
	blockedEvent := groupEvent(t, 3, "hash-concurrent-b", "Canceled behind reset", stamp.Add(time.Second))
	captureDone := make(chan error, 1)
	go func() {
		_, _, err := f.db.Ingest(captureCtx, f.project.ID, blockedEvent)
		captureDone <- err
	}()
	groupWaitBlocked(t, f.ctx, blocker, resetPIDs[0], 1)
	cancelCapture()
	groupAwaitCancellation(t, captureDone, "capture behind exclusive reset")
	cancelReset()
	groupAwaitCancellation(t, resetDone, "reset awaiting actual issue row")
	if err = blocker.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	barrier, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = barrier.Exec(f.ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_schema()||':issue-group:'||$1::text,0))", f.project.ID); err != nil {
		t.Fatal(err)
	}
	if err = barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if err = writer.ResetCompatibleIssueHashes(f.ctx, f.access, a, []string{hash}); err != nil {
		t.Fatal(err)
	}
	// Release observation occurs after actual origin/event allocation. Cancel
	// there to prove rollback of writes already made, not only canceled admission.
	f.release(t, "app@4")
	releaseBlocker, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseBlocker.Rollback(f.ctx)
	if _, err = releaseBlocker.Exec(f.ctx, "SELECT id FROM releases WHERE version='app@4' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	canceledEvent := groupEvent(t, 4, "hash-concurrent-a", "Canceled allocation", stamp.Add(2*time.Second))
	allocationCtx, cancelAllocation := context.WithCancel(f.ctx)
	defer cancelAllocation()
	canceledDone := make(chan error, 1)
	go func() {
		_, _, err := writer.Ingest(allocationCtx, f.project.ID, canceledEvent)
		canceledDone <- err
	}()
	groupWaitBlocked(t, f.ctx, releaseBlocker, int(releaseBlocker.Conn().PgConn().PID()), 1)
	cancelAllocation()
	groupAwaitCancellation(t, canceledDone, "allocated generation awaiting release metadata")
	if err = releaseBlocker.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	// Both replicas now contend on the same inactive durable route, before
	// either first capture is allowed to allocate its next origin.
	routeBlocker, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer routeBlocker.Rollback(f.ctx)
	if _, err = routeBlocker.Exec(f.ctx, "SELECT id FROM issue_hash_routes WHERE replace(id::text,'-','')=$1 FOR UPDATE", hash); err != nil {
		t.Fatal(err)
	}
	survivingEvent := groupEvent(t, 5, "hash-concurrent-a", "Surviving allocation", stamp.Add(3*time.Second))
	allocated := make(chan hashCaptureResult, 2)
	go func() {
		id, fresh, err := writer.Ingest(f.ctx, f.project.ID, survivingEvent)
		allocated <- hashCaptureResult{id, fresh, err}
	}()
	firstAllocator := groupWaitBlocked(t, f.ctx, routeBlocker, int(routeBlocker.Conn().PgConn().PID()), 1)
	secondEvent := groupEvent(t, 7, "hash-concurrent-a", "Concurrent first generation", stamp.Add(4*time.Second))
	go func() {
		id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, secondEvent)
		allocated <- hashCaptureResult{id, fresh, err}
	}()
	hashWaitBlockedByEither(t, f, routeBlocker, int(routeBlocker.Conn().PgConn().PID()), firstAllocator[0])
	if err = routeBlocker.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	newA := ""
	for range 2 {
		select {
		case got := <-allocated:
			if got.err != nil || !got.fresh || got.id == a || (newA != "" && got.id != newA) {
				t.Fatalf("concurrent allocation created conflicting generations: %+v previous=%s", got, newA)
			}
			newA = got.id
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent first captures did not release their route locks")
		}
	}
	if triageIssue(t, f.ctx, f.db, f.owner.ID, newA).Count != 2 || hashForSample(t, hashSamples(t, f, newA, CompatibleIssueHashFilter{Limit: 100}), secondEvent.ID) != hash {
		t.Fatal("surviving allocation did not reuse the inactive route identity")
	}
	c := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 6, "hash-concurrent-c", "Concurrent merge control", stamp))
	if _, err = f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, newA}); err != nil {
		t.Fatal(err)
	}
	// A and B now have different route rows and original generations but share
	// the same effective issue. Native merge/triage must retain a compatible
	// lock order while independent replicas ingest both signatures.
	results := make(chan error, 18)
	for n := range 16 {
		signature := "hash-concurrent-a"
		if n%2 != 0 {
			signature = "hash-concurrent-b"
		}
		ev := groupEvent(t, n+20, signature, fmt.Sprintf("Concurrent receipt %d", n), stamp.Add(time.Duration(n+10)*time.Second))
		db := f.db
		if n%3 == 0 {
			db = writer
		}
		go func(db *Store, ev event.Event) {
			id, fresh, err := db.Ingest(f.ctx, f.project.ID, ev)
			if err == nil && (!fresh || id != a) {
				err = fmt.Errorf("concurrent fresh capture used root %s fresh=%v", id, fresh)
			}
			results <- err
		}(db, ev)
	}
	go func() {
		_, err := writer.UpdateIssue(f.ctx, f.owner.ID, a, IssueUpdate{Status: "ignored"})
		results <- err
	}()
	go func() {
		_, err := writer.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, c})
		results <- err
	}()
	for range 18 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("capture/reset route lock order deadlocked with native actors")
		}
	}
	i := triageIssue(t, f.ctx, f.db, f.owner.ID, a)
	if i.Count != 21 || i.GroupSize != 4 || i.Status != "ignored" || len(hashSamples(t, f, a, CompatibleIssueHashFilter{Limit: 100})) != 3 {
		t.Fatalf("concurrent receipts or native topology lost data: %+v", i)
	}
	var issues, events, canceledRows int
	if err = f.db.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM issues WHERE project_id=$1::bigint),(SELECT count(*) FROM events WHERE project_id=$1::bigint),(SELECT count(*) FROM events WHERE project_id=$1::bigint AND event_id=ANY($2::text[]))`, f.project.ID, []string{blockedEvent.ID, canceledEvent.ID}).Scan(&issues, &events, &canceledRows); err != nil {
		t.Fatal(err)
	}
	if issues != 4 || events != 21 || canceledRows != 0 {
		t.Fatalf("canceled allocation leaked origin/receipt: issues=%d events=%d canceled=%d", issues, events, canceledRows)
	}
}
