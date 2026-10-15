//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These tests own transaction/outbox/upgrade contracts. Selection and version
// matrices live in issues_resolution_test; transport and lifecycle have separate
// owners. Real PostgreSQL lock dependencies avoid production synchronization seams.
func resolutionStageEvent(t *testing.T, n int, version string) event.Event {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"event_id": fmt.Sprintf("%032x", n), "message": "Resolution transaction", "fingerprint": []string{"resolution-transaction"}, "release": version})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func resolutionStageRelease(t *testing.T, ctx context.Context, db *Store, owner User, org Organization, p Project, version string) {
	t.Helper()
	_, _, err := db.CreateRelease(ctx, ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}, ReleaseCreate{Version: ReleaseVersion(version), Projects: []string{p.Slug}})
	if err != nil {
		t.Fatal(err)
	}
}

func resolutionStageSnapshot(t *testing.T, ctx context.Context, db *Store) string {
	t.Helper()
	var result string
	err := db.pool.QueryRow(ctx, `SELECT jsonb_build_object(
 'issues',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM issues x),
 'events',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM events x),
 'activity',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM issue_activity x),
 'policies',(SELECT jsonb_agg(to_jsonb(x) ORDER BY issue_id) FROM issue_resolutions x),
 'rules',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM alert_rules x),
 'deliveries',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM alert_deliveries x),
 'releases',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM releases x),
 'associations',(SELECT jsonb_agg(to_jsonb(x) ORDER BY release_id,project_id) FROM release_projects x))::text`).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func waitResolutionBlock(t *testing.T, ctx context.Context, tx pgx.Tx, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		// PostgreSQL caches activity observations inside a transaction.
		if _, err := tx.Exec(ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
			t.Fatal(err)
		}
		var blocked bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)))`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("capture did not reach the held release identity")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestIssueResolutionCaptureReleaseWaitCancellationIsAtomic(t *testing.T) {
	ctx, db, owner, org, p := telemetryStore(t)
	other, err := db.CreateProject(ctx, owner.ID, org.Slug, "Other release association", "go")
	if err != nil {
		t.Fatal(err)
	}
	resolutionStageRelease(t, ctx, db, owner, org, other, "app@2")
	id, _, err := db.Ingest(ctx, p.ID, resolutionStageEvent(t, 1, "app@1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: "app@1"}}}); err != nil {
		t.Fatal(err)
	}
	cfg := alertConfig("Atomic release regression")
	cfg.Quantity = 2
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, p.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := resolutionStageSnapshot(t, ctx, db)
	blocker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	var pid int
	if err = blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.Exec(ctx, "SELECT id FROM releases WHERE version='app@2' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	captureCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, _, err := db.Ingest(captureCtx, p.ID, resolutionStageEvent(t, 2, "app@2")); done <- err }()
	waitResolutionBlock(t, ctx, blocker, pid)
	// At the release wait the rule must already be locked. This is observable
	// lock ordering, not an assertion about private function call order.
	probe, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = probe.Exec(ctx, "SELECT id FROM alert_rules WHERE id=$1::bigint FOR UPDATE NOWAIT", rule.ID)
	var pgerr *pgconn.PgError
	locked := errors.As(err, &pgerr) && pgerr.Code == "55P03"
	_ = probe.Rollback(ctx)
	if !locked {
		t.Fatalf("release stage did not retain the prior rule lock: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled capture acknowledged success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("capture cancellation did not release the transaction")
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if after := resolutionStageSnapshot(t, ctx, db); after != before {
		t.Fatal("release wait cancellation partially changed issue/event/policy/activity/rule/outbox/metadata")
	}
	// The same identity is retryable and associates metadata only after success.
	if _, fresh, err := db.Ingest(ctx, p.ID, resolutionStageEvent(t, 2, "app@2")); err != nil || !fresh {
		t.Fatalf("retry fresh=%v err=%v", fresh, err)
	}
	got := triageIssue(t, ctx, db, owner.ID, id)
	if got.Status != "unresolved" || got.Resolution != nil || got.Count != 2 || got.RegressedAt == nil {
		t.Fatalf("successful retry=%+v", got)
	}
	deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, p.ID, rule.ID, 1)
	if err != nil || len(deliveries) != 1 || deliveries[0].Payload.EventID != fmt.Sprintf("%032x", 2) {
		t.Fatalf("retry deliveries=%+v err=%v", deliveries, err)
	}
}

func TestIssueResolutionSuppressesNewOutboxButPreservesHistoryAndRawWindows(t *testing.T) {
	for _, next := range []bool{false, true} {
		t.Run(fmt.Sprintf("next=%v", next), func(t *testing.T) {
			ctx, db, owner, org, p := telemetryStore(t)
			cfg := alertConfig("Release suppression")
			cfg.Quantity = 2
			rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, p.ID, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.TestAlert(ctx, owner.ID, org.Slug, p.ID, rule.ID); err != nil {
				t.Fatal(err)
			}
			history, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, p.ID, rule.ID, 1)
			if err != nil || len(history) != 1 {
				t.Fatal("test alert history missing", err)
			}
			id, _, err := db.Ingest(ctx, p.ID, resolutionStageEvent(t, 1, "app@1"))
			if err != nil {
				t.Fatal(err)
			}
			policy := &IssueResolutionInput{Mode: "release", Version: "app@1"}
			if next {
				policy = &IssueResolutionInput{Mode: "next_release"}
			}
			if _, err = db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: policy}}); err != nil {
				t.Fatal(err)
			}
			if _, fresh, err := db.Ingest(ctx, p.ID, resolutionStageEvent(t, 2, "app@1")); err != nil || !fresh {
				t.Fatalf("resolved capture: %v %v", fresh, err)
			}
			after, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, p.ID, rule.ID, 1)
			if err != nil || !reflect.DeepEqual(history, after) {
				t.Fatalf("resolved capture changed old delivery history: %v", err)
			}
			r, err := db.Alert(ctx, owner.ID, org.Slug, p.ID, rule.ID)
			if err != nil || r.LastTriggered != nil {
				t.Fatalf("suppressed capture consumed cooldown: %+v %v", r, err)
			}
			if _, _, err = db.Ingest(ctx, p.ID, resolutionStageEvent(t, 3, "app@2")); err != nil {
				t.Fatal(err)
			}
			got := triageIssue(t, ctx, db, owner.ID, id)
			if got.Status != "unresolved" || got.Count != 3 {
				t.Fatalf("regression=%+v", got)
			}
			after, _, err = db.AlertDeliveries(ctx, owner.ID, org.Slug, p.ID, rule.ID, 1)
			if err != nil || !reflect.DeepEqual(history, after) {
				t.Fatalf("regression replayed the already crossed raw threshold: %v", err)
			}
			// A new rule whose threshold is the next raw receipt must enqueue the
			// ordinary regression. Muted/resolved occurrences remain in that window.
			cfg.Name, cfg.Quantity = "Ordinary release regression", 4
			eligible, err := db.CreateAlert(ctx, owner.ID, org.Slug, p.ID, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: "app@2"}}}); err != nil {
				t.Fatal(err)
			}
			if _, _, err = db.Ingest(ctx, p.ID, resolutionStageEvent(t, 4, "app@3")); err != nil {
				t.Fatal(err)
			}
			queued, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, p.ID, eligible.ID, 1)
			if err != nil || len(queued) != 1 || queued[0].Payload.Count != 4 {
				t.Fatalf("ordinary regression did not queue: %+v %v", queued, err)
			}
		})
	}
}

func TestIssueResolutionCaptureRuleMutationAndReleaseMetadataOverlap(t *testing.T) {
	ctx, db, owner, org, p := telemetryStore(t)
	id, _, err := db.Ingest(ctx, p.ID, resolutionStageEvent(t, 1, "app@1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "next_release"}}}); err != nil {
		t.Fatal(err)
	}
	resolutionStageRelease(t, ctx, db, owner, org, p, "app@2")
	cfg := alertConfig("Concurrent release stages")
	cfg.Quantity = 2
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, p.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	replica, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replica.Close)
	start := make(chan struct{})
	errs := make(chan error, 5)
	var wg sync.WaitGroup
	for _, work := range []func() error{
		func() error { _, _, e := db.Ingest(ctx, p.ID, resolutionStageEvent(t, 2, "app@2")); return e },
		func() error { _, e := db.UpdateAlert(ctx, owner.ID, org.Slug, p.ID, rule.ID, cfg); return e },
		func() error {
			stamp := time.Now().UTC()
			_, e := db.UpdateRelease(ctx, ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}, "app@2", "", ReleaseUpdate{DateReleased: ReleaseTime{Set: true, Value: &stamp}})
			return e
		},
		func() error { _, e := db.BindNextIssueRelease(ctx, time.Second); return e },
		func() error { _, e := replica.BindNextIssueRelease(ctx, time.Second); return e },
	} {
		wg.Go(func() { <-start; errs <- work() })
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := triageIssue(t, ctx, db, owner.ID, id)
	if got.Status != "unresolved" || got.Count != 2 || got.Resolution != nil {
		t.Fatalf("overlapped capture=%+v", got)
	}
	var bound, regressed int
	for _, a := range triageActivities(t, ctx, db, owner.ID, id) {
		if a.Type == "resolution_bound" {
			bound++
		}
		if a.Type == "regressed" {
			regressed++
		}
	}
	if bound != 1 || regressed != 1 {
		t.Fatalf("overlap duplicated/lost binding or regression: %d %d", bound, regressed)
	}
}

func TestIssueResolutionMigrationPreserves001Through013AndReopen(t *testing.T) {
	ctx, db, owner, org, p := telemetryStore(t)
	id := triageCapture(t, ctx, db, p)
	if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "resolved", Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	muted, _, err := db.Ingest(ctx, p.ID, event.Event{ID: fmt.Sprintf("%032x", 2), Type: "error", Title: "Existing conditional mute", Level: "error", Timestamp: time.Now(), Fingerprint: "other", Data: json.RawMessage(`{"message":"prior mute"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpdateIssue(ctx, owner.ID, muted, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(120, 9)}); err != nil {
		t.Fatal(err)
	}
	resolutionStageRelease(t, ctx, db, owner, org, p, "prior-release")
	if _, err = db.pool.Exec(ctx, "DROP TABLE issue_resolutions; DELETE FROM schema_migrations WHERE version=14"); err != nil {
		t.Fatal(err)
	}
	const prior = `SELECT jsonb_build_object(
 'issues',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM issues x),
 'events',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM events x),
 'activity',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM issue_activity x),
 'assignments',(SELECT jsonb_agg(to_jsonb(x) ORDER BY issue_id) FROM issue_assignments x),
 'ignores',(SELECT jsonb_agg(to_jsonb(x) ORDER BY issue_id) FROM issue_ignores x),
 'releases',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM releases x),
 'receipts',(SELECT jsonb_agg(to_jsonb(x) ORDER BY version) FROM schema_migrations x WHERE version<=13))::text`
	var before, after string
	if err = db.pool.QueryRow(ctx, prior).Scan(&before); err != nil {
		t.Fatal(err)
	}
	url := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if err = reopened.pool.QueryRow(ctx, prior).Scan(&after); err != nil || before != after {
		t.Fatalf("014 changed established data or receipts: equal=%v err=%v", before == after, err)
	}
	legacy := triageIssue(t, ctx, reopened, owner.ID, id)
	if legacy.Status != "resolved" || legacy.Resolution != nil || legacy.AssignedTo == nil {
		t.Fatalf("legacy resolution changed: %+v", legacy)
	}
	if got := triageIssue(t, ctx, reopened, owner.ID, muted); got.Ignore == nil || *got.Ignore.Remaining != 9 {
		t.Fatalf("013 mute changed: %+v", got)
	}
	var receipt time.Time
	if err = reopened.pool.QueryRow(ctx, "SELECT applied_at FROM schema_migrations WHERE version=14").Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	again, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	var repeated time.Time
	if err = again.pool.QueryRow(ctx, "SELECT applied_at FROM schema_migrations WHERE version=14").Scan(&repeated); err != nil || !receipt.Equal(repeated) {
		t.Fatalf("014 reapplied: %v", err)
	}
	if err = again.pool.QueryRow(ctx, prior).Scan(&after); err != nil || before != after {
		t.Fatalf("restart changed established data: %v", err)
	}
	if _, _, err = again.Ingest(ctx, p.ID, triageEvent(t, fmt.Sprintf("%032x", 3), time.Now())); err != nil {
		t.Fatal(err)
	}
	if got := triageIssue(t, ctx, again, owner.ID, id); got.Status != "unresolved" || got.Resolution != nil {
		t.Fatalf("legacy simple resolution stopped regressing: %+v", got)
	}
}
