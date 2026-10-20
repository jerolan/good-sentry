//go:build integration

package store

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func hashLegacy018(t *testing.T, f resolutionFixture) {
	t.Helper()
	// This schema is owned by this test. Reconstruct only019's predecessor;
	// accepted rows, fingerprint text and receipts001..018 remain untouched.
	if _, err := f.db.pool.Exec(f.ctx, `DROP TABLE issue_hash_routes;
DROP INDEX issues_project_fingerprint;
ALTER TABLE issues ADD CONSTRAINT issues_project_id_fingerprint_key UNIQUE(project_id,fingerprint);
DELETE FROM schema_migrations WHERE version=19`); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibleIssueHashesMigration019PreservesLegacyBackfillAndReopen(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	aEvent := groupEvent(t, 1, "hash-migration-root", "Legacy root", stamp)
	a := groupCapture(t, f.ctx, f.db, f.project, aEvent)
	bEvent := groupEvent(t, 2, "hash-migration-alias", "Legacy alias", stamp.Add(time.Second))
	b := groupCapture(t, f.ctx, f.db, f.project, bEvent)
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, a, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 5), Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: f.owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AddComment(f.ctx, f.owner.ID, a, "Before route identities existed"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CreateSession(f.ctx, f.owner.ID, f.owner.AuthGeneration, "migration019-session", "migration019-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	hashLegacy018(t, f)
	// Legacy text fingerprints and original_issue_id=NULL are valid storage
	// states. Backfill must not narrow them to normalized SHA256 values.
	var empty string
	if err := f.db.pool.QueryRow(f.ctx, `INSERT INTO issues(project_id,fingerprint,title,level,platform,count,receipt_count,first_seen,last_seen) VALUES($1::bigint,'legacy route / not hexadecimal','No retained raw','error','go',7,11,$2,$2) RETURNING id::text`, f.project.ID, stamp).Scan(&empty); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "UPDATE events SET original_issue_id=NULL WHERE project_id=$1::bigint AND event_id=$2", f.project.ID, aEvent.ID); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 18)
	url := f.db.pool.Config().ConnString()
	f.db.Close()
	upgraded, err := Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	after := groupDatabaseSnapshot(t, f.ctx, upgraded, 18)
	delete(after, "issue_hash_routes")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("019 rewrote an old column, history row, raw payload, policy or001..018 receipt/checksum")
	}
	f.db = upgraded
	merged := hashSamples(t, f, a, CompatibleIssueHashFilter{Limit: 100})
	if len(merged) != 2 || hashForSample(t, merged, aEvent.ID) == hashForSample(t, merged, bEvent.ID) {
		t.Fatal("019 did not backfill separate active routes for a root and its alias")
	}
	emptyHashes := hashSamples(t, f, empty, CompatibleIssueHashFilter{Limit: 100})
	if len(emptyHashes) != 1 || emptyHashes[0].Latest != nil || len(emptyHashes[0].ID) != 32 || strings.Trim(emptyHashes[0].ID, "0123456789abcdef") != "" {
		t.Fatalf("legacy route without retained events did not return a real UUID/null sample: %+v", emptyHashes)
	}
	origins, err := upgraded.IssueOrigins(f.ctx, f.owner.ID, a, "", 1)
	if err != nil || len(origins.Data) != 2 || !origins.Data[0].ReceivesNewEvents || !origins.Data[1].ReceivesNewEvents {
		t.Fatalf("backfilled legacy origins are not active: %+v error=%v", origins, err)
	}
	var routes, badBindings int
	if err := upgraded.pool.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER(WHERE r.origin_issue_id IS NULL OR i.project_id<>r.project_id OR i.fingerprint<>r.fingerprint) FROM issue_hash_routes r LEFT JOIN issues i ON i.id=r.origin_issue_id`).Scan(&routes, &badBindings); err != nil {
		t.Fatal(err)
	}
	if routes != 3 || badBindings != 0 {
		t.Fatalf("backfill omitted/guessed an origin: routes=%d bad=%d", routes, badBindings)
	}
	// An origin cannot disappear while a live routing binding references it.
	// Reset clears the binding first; project deletion can still cascade later.
	tx, err := upgraded.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, rejected := tx.Exec(f.ctx, "DELETE FROM issues WHERE id=$1::bigint", empty)
	_ = tx.Rollback(f.ctx)
	if rejected == nil {
		t.Fatal("active origin deletion silently removed its route")
	}
	checkpoint := groupDatabaseSnapshot(t, f.ctx, upgraded, 0)
	upgraded.Close()
	again, err := Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	if after := groupDatabaseSnapshot(t, f.ctx, again, 0); !reflect.DeepEqual(checkpoint, after) {
		t.Fatal("reopen reapplied019 or changed a stable UUID/backfill/history receipt")
	}
	f.db = again
	if got := hashSamples(t, f, empty, CompatibleIssueHashFilter{Limit: 100}); len(got) != 1 || got[0].ID != emptyHashes[0].ID || got[0].Latest != nil {
		t.Fatal("legacy route identity/null sample did not survive reopen")
	}
	cascade, err := again.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cascade.Rollback(f.ctx)
	if _, err = cascade.Exec(f.ctx, "DELETE FROM projects WHERE id=$1::bigint", f.project.ID); err != nil {
		t.Fatalf("origin reference broke the existing project cascade: %v", err)
	}
	var remaining int
	if err = cascade.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM issues WHERE project_id=$1::bigint)+(SELECT count(*) FROM issue_hash_routes WHERE project_id=$1::bigint)+(SELECT count(*) FROM events WHERE project_id=$1::bigint)`, f.project.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("project cascade left route/origin/event rows: %d error=%v", remaining, err)
	}
	if err = cascade.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	groupRequireUnchanged(t, f.ctx, again, checkpoint)
}

func TestCompatibleIssueHashesMigration019RejectsInconsistentOwnershipAtomically(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour)
	a := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "hash-invalid-a", "A", stamp))
	b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "hash-invalid-b", "B", stamp))
	hashLegacy018(t, f)
	if _, err := f.db.pool.Exec(f.ctx, "UPDATE events SET original_issue_id=$2::bigint WHERE issue_id=$1::bigint", a, b); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	catalog := groupCatalogSnapshot(t, f.ctx, f.db)
	failed, err := Open(f.ctx, f.db.pool.Config().ConnString())
	if err == nil {
		failed.Close()
		t.Fatal("019 guessed a route for inconsistent historical ownership")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ownership") {
		t.Fatalf("019 did not identify the failed ownership invariant: %v", err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if after := groupCatalogSnapshot(t, f.ctx, f.db); after != catalog {
		t.Fatal("failed019 left a route table/index/constraint or removed predecessor uniqueness")
	}
}
