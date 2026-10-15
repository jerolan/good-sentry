//go:build integration

package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCompatibleIssueBulkGateCancellationPreservesNativeActors(t *testing.T) {
	f := resolutionStore(t)
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	a := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "bulk-wait-a", "Wait A", base))
	b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "bulk-wait-b", "Wait B", base))
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
	// Hold the production project gate, rather than add a synchronization seam
	// to the provider. Both pools use the same schema and advisory namespace.
	if _, err := blocker.Exec(f.ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_schema()||':issue-group:'||$1::text,0))", f.project.ID); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	bulkCtx, cancelBulk := context.WithCancel(f.ctx)
	defer cancelBulk()
	bulkDone := make(chan error, 1)
	go func() {
		_, err := writer.UpdateCompatibleIssues(bulkCtx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a, b}}, CompatibleIssueBulkChange{Merge: true, Change: CompatibleIssueChange{Status: "resolved"}})
		bulkDone <- err
	}()
	captureCtx, cancelCapture := context.WithCancel(f.ctx)
	defer cancelCapture()
	future := groupEvent(t, 3, "bulk-wait-b", "Wait B fresh", base.Add(time.Second))
	captureDone := make(chan error, 1)
	go func() {
		_, _, err := f.db.Ingest(captureCtx, f.project.ID, future)
		captureDone <- err
	}()
	triageCtx, cancelTriage := context.WithCancel(f.ctx)
	defer cancelTriage()
	triageDone := make(chan error, 1)
	go func() {
		_, err := writer.UpdateIssue(triageCtx, f.owner.ID, a, IssueUpdate{Status: "ignored"})
		triageDone <- err
	}()
	groupWaitBlocked(t, f.ctx, blocker, int(blocker.Conn().PgConn().PID()), 3)
	cancelBulk()
	cancelCapture()
	cancelTriage()
	groupAwaitCancellation(t, bulkDone, "bulk writer at project gate")
	groupAwaitCancellation(t, captureDone, "native capture at project gate")
	groupAwaitCancellation(t, triageDone, "native triage at project gate")
	if err := blocker.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	// A real exclusive barrier waits for canceled transactions to relinquish
	// their locks before observing the complete persisted footprint.
	barrier, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.Exec(f.ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_schema()||':issue-group:'||$1::text,0))", f.project.ID); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	result, err := writer.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{b, a}}, CompatibleIssueBulkChange{Merge: true, Change: CompatibleIssueChange{Status: "resolved"}})
	if err != nil || result.First == nil || result.First.ID != a || result.First.Count != 2 || result.First.Status != "resolved" {
		t.Fatalf("bulk retry result=%+v error=%v", result, err)
	}
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, future); err != nil || !fresh || id != a {
		t.Fatalf("native capture retry id=%s fresh=%v error=%v", id, fresh, err)
	}
	if result, err := writer.UpdateIssue(f.ctx, f.owner.ID, a, IssueUpdate{Status: "ignored"}); err != nil || result.Status != "ignored" || result.Count != 3 {
		t.Fatalf("native triage retry issue=%+v error=%v", result, err)
	}
}

func TestCompatibleIssueBulkCrossProjectGateOrderAndConcurrentWriters(t *testing.T) {
	f := resolutionStore(t)
	base := time.Now().Add(-time.Hour)
	gateKey := func(project string) int64 {
		t.Helper()
		var key int64
		if err := f.db.pool.QueryRow(f.ctx, "SELECT hashtextextended(current_schema()||':issue-group:'||$1::text,0)", project).Scan(&key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	firstProject := f.project
	firstKey := gateKey(firstProject.ID)
	// Choose a real pair whose signed gate order reverses project-ID order;
	// the fixture records keys from PostgreSQL, without guessing their hashes.
	var other Project
	var secondKey int64
	for n := range 32 {
		var err error
		other, err = f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, fmt.Sprintf("Gate order %d", n), "go")
		if err != nil {
			t.Fatal(err)
		}
		secondKey = gateKey(other.ID)
		if secondKey < firstKey {
			break
		}
		firstProject, firstKey = other, secondKey
	}
	if secondKey >= firstKey {
		t.Fatal("fixture could not obtain two projects with reversed signed advisory-key order")
	}
	_, a := compatibleCapture(t, f.ctx, f.db, firstProject, 1, base, nil)
	_, b := compatibleCapture(t, f.ctx, f.db, other, 1, base.Add(time.Second), nil)
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
	if _, err := blocker.Exec(f.ctx, "SELECT pg_advisory_xact_lock($1::bigint)", secondKey); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	results := make(chan error, 2)
	go func() {
		_, err := writer.UpdateCompatibleIssues(ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a, b}, Sort: "last_seen"}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "ignored"}})
		results <- err
	}()
	go func() {
		_, err := f.db.UpdateCompatibleIssues(ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{b, a}, Sort: "-last_seen"}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
		results <- err
	}()
	pids := groupWaitBlocked(t, f.ctx, blocker, int(blocker.Conn().PgConn().PID()), 2)
	var laterGateClaims int
	if err := blocker.QueryRow(f.ctx, `SELECT count(*) FROM pg_locks WHERE pid=ANY($1::int[]) AND locktype='advisory' AND granted AND objsubid=1
 AND classid=(($2::bigint>>32)&4294967295)::oid AND objid=($2::bigint&4294967295)::oid`, pids, firstKey).Scan(&laterGateClaims); err != nil {
		t.Fatal(err)
	}
	if laterGateClaims != 0 {
		t.Fatal("bulk acquired a later signed gate before waiting for the first gate")
	}
	if err := blocker.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("oppositely ordered cross-project bulk requests failed: %v", err)
		}
	}
	one := triageIssue(t, f.ctx, f.db, f.owner.ID, a)
	two := triageIssue(t, f.ctx, f.db, f.owner.ID, b)
	if one.Status != two.Status || (one.Status != "resolved" && one.Status != "ignored") || len(f.actions(t, a, "resolved")) != 1 || len(f.actions(t, b, "resolved")) != 1 || len(f.actions(t, a, "ignored")) != 1 || len(f.actions(t, b, "ignored")) != 1 {
		t.Fatalf("cross-project requests did not serialize as complete operations: first=%s second=%s", one.Status, two.Status)
	}
}

func TestCompatibleIssueBulkRechecksStatusAfterExternalRowWriterWithoutBackfill(t *testing.T) {
	f := resolutionStore(t)
	base := time.Now().Add(-time.Hour)
	_, a := compatibleCapture(t, f.ctx, f.db, f.project, 1, base, nil)
	_, b := compatibleCapture(t, f.ctx, f.db, f.project, 2, base.Add(time.Second), map[string]any{"fingerprint": []string{"row-wait-b"}})
	_, laterMatch := compatibleCapture(t, f.ctx, f.db, f.project, 3, base.Add(2*time.Second), map[string]any{"fingerprint": []string{"row-wait-later"}})
	compatibleApply(t, f, a, CompatibleIssueChange{Status: "ignored"})
	compatibleApply(t, f, b, CompatibleIssueChange{Status: "ignored"})
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
	// This is an external row-writer fixture with the maintenance lock shape,
	// not a claim that the real expiry worker ran. It deliberately takes no gate.
	if _, err := blocker.Exec(f.ctx, "UPDATE issues SET status='unresolved' WHERE id=$1", a); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result CompatibleIssueBulkResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := writer.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 2, Status: "ignored", Sort: "last_seen"}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
		done <- outcome{result, err}
	}()
	groupWaitBlocked(t, f.ctx, blocker, int(blocker.Conn().PgConn().PID()), 1)
	if _, err := blocker.Exec(f.ctx, "UPDATE issues SET status='ignored' WHERE id=$1", laterMatch); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.result.First == nil || got.result.First.ID != b {
		t.Fatalf("post-row-wait filter result=%+v error=%v", got.result, got.err)
	}
	bulkRequireStatus(t, f, "unresolved", a)
	bulkRequireStatus(t, f, "resolved", b)
	bulkRequireStatus(t, f, "ignored", laterMatch)
	if len(f.actions(t, a, "resolved")) != 0 || len(f.actions(t, laterMatch, "resolved")) != 0 || len(f.actions(t, b, "resolved")) != 1 {
		t.Fatal("row-wait recheck mutated an excluded root or backfilled the bounded cohort")
	}
}

func TestCompatibleIssueBulkRechecksRootsAndFiltersAfterNativeMergeWait(t *testing.T) {
	for _, query := range []string{"", "is:unresolved"} {
		t.Run("query="+query, func(t *testing.T) {
			f := resolutionStore(t)
			base := time.Now().Add(-time.Hour)
			a := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "bulk-fresh-a", "Resolved winner", base))
			b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "bulk-fresh-b", "Unresolved source", base.Add(time.Second)))
			compatibleApply(t, f, a, CompatibleIssueChange{Status: "resolved"})
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
			if _, err := blocker.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", b); err != nil {
				t.Fatal(err)
			}
			mergeDone := make(chan error, 1)
			go func() {
				_, err := writer.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b})
				mergeDone <- err
			}()
			mergePID := groupWaitBlocked(t, f.ctx, blocker, int(blocker.Conn().PgConn().PID()), 1)[0]
			// Native merge now holds the exclusive gate and waits on B. Bulk
			// sees B as a root while selecting its cohort, then waits on merge.
			type outcome struct {
				result CompatibleIssueBulkResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{b}, Query: query}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "ignored"}})
				done <- outcome{result, err}
			}()
			groupWaitBlocked(t, f.ctx, blocker, mergePID, 1)
			if err := blocker.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-mergeDone; err != nil {
				t.Fatal(err)
			}
			got := <-done
			if got.err != nil {
				t.Fatal(got.err)
			}
			want := "ignored"
			if query != "" {
				want = "resolved"
				if got.result.First != nil {
					t.Fatalf("stale pre-wait filter still selected a resolved root: %+v", got.result)
				}
			} else if got.result.First == nil || got.result.First.ID != a || got.result.First.Count != 2 {
				t.Fatalf("post-wait alias was not re-resolved: %+v", got.result)
			}
			bulkRequireStatus(t, f, want, a, b)
			var mergedInto string
			if err := f.db.pool.QueryRow(f.ctx, "SELECT merged_into::text FROM issues WHERE id=$1", b).Scan(&mergedInto); err != nil || mergedInto != a {
				t.Fatalf("native topology lost: merged_into=%s error=%v", mergedInto, err)
			}
		})
	}
}
