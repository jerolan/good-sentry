//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Migration owns the durable category addition and generation FKs. Earlier
// migration tests cannot detect rewriting old issue fields or receipts in031.
// Real old rows and repeated Open calls need no application seam.
func TestFeedbackMigration031PreservesOldFieldsAndReopens(t *testing.T) {
	f := resolutionStore(t)
	a := f.capture(t, 1, "before031@1", time.Now().Add(-time.Hour))
	b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "before031-other", "Other retained error", time.Now()))
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, UserReportInput{EventID: fmt.Sprintf("%032x", 1), Comments: "Legacy report survives031"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.db.pool.Exec(f.ctx, `DROP TABLE feedback_events;
 ALTER TABLE issues DROP CONSTRAINT issues_group_root_fkey;
 ALTER TABLE issues DROP CONSTRAINT issues_project_category_id_key;
 ALTER TABLE issues DROP COLUMN category;
 ALTER TABLE issues ADD CONSTRAINT issues_group_root_fkey FOREIGN KEY(project_id,merged_into)
 REFERENCES issues(project_id,id) ON DELETE SET NULL (merged_into);
 DELETE FROM schema_migrations WHERE version=31`)
	if err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 30)
	for range 2 {
		reopened, err := Open(f.ctx, f.db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		after := groupDatabaseSnapshot(t, f.ctx, reopened, 30)
		var oldIssues string
		if err := reopened.pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(payload ORDER BY payload::text),'[]'::jsonb)::text
 FROM (SELECT to_jsonb(i)-'category' AS payload FROM issues i) preserved`).Scan(&oldIssues); err != nil {
			t.Fatal(err)
		}
		after["issues"] = oldIssues
		delete(after, "feedback_events")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("031 rewrote a prior field, row or migration receipt")
		}
		var wrong, rows int
		if err := reopened.pool.QueryRow(f.ctx, `SELECT count(*) FILTER(WHERE category<>'error'),count(*) FROM issues`).Scan(&wrong, &rows); err != nil || wrong != 0 || rows != 2 {
			t.Fatalf("historical categories wrong=%d rows=%d err=%v", wrong, rows, err)
		}
		var receipt string
		if err := reopened.pool.QueryRow(f.ctx, `SELECT checksum FROM schema_migrations WHERE version=31`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/031_modern_feedback.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatal("031 checksum differs from its saved source", err)
		}
		reopened.Close()
	}
}
