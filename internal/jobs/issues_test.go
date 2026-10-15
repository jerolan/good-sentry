//go:build integration

package jobs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/event"
	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
)

// The running worker owns startup catch-up and cancellation of its idle timer.
// Provider tests cannot catch a worker that never runs, waits before startup or
// blocks shutdown for its configured hour. This uses real persisted deadlines
// and production start/stop, without a clock or synchronization test seam.
func TestIssueMaintenanceCatchUpAndStop(t *testing.T) {
	ctx := context.Background()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("goodsentry_issue_workers_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	if _, err = admin.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner, org, err := db.Bootstrap(ctx, "mute-worker@example.test", []byte("fixture-hash"), "Owner", "Mute workers")
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.CreateProject(ctx, owner.ID, org.Slug, "Worker errors", "go")
	if err != nil {
		t.Fatal(err)
	}
	due := func(n int) string {
		t.Helper()
		raw := json.RawMessage(fmt.Sprintf(`{"event_id":"%032x","message":"mute worker %d","fingerprint":["worker-%d"]}`, n, n, n))
		ev, err := event.Normalize(raw, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := db.Ingest(ctx, p.ID, ev)
		if err != nil {
			t.Fatal(err)
		}
		minutes := int32(1)
		if _, err = db.UpdateIssue(ctx, owner.ID, id, store.IssueUpdate{Status: "ignored", Ignore: &store.IssueIgnoreChange{Condition: &store.IssueIgnoreCondition{Minutes: &minutes}}}); err != nil {
			t.Fatal(err)
		}
		// A persisted elapsed deadline models a stopped application catching up.
		// Receipt-to-deadline construction is owned by provider tests.
		if _, err = admin.Exec(ctx, `UPDATE issue_ignores SET until=clock_timestamp()-interval '1 second' WHERE issue_id=$1::bigint`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	waitExpired := func(db *store.Store, id string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			detail, err := db.IssueDetail(ctx, owner.ID, id)
			if err != nil {
				t.Fatal(err)
			}
			if detail.Issue.Status == "unresolved" && detail.Issue.Ignore == nil {
				a, _, err := db.IssueActivities(ctx, owner.ID, id, 1)
				if err != nil || len(a) != 3 || a[0].Type != "unignored" || a[0].User != nil {
					t.Fatalf("worker history=%+v err=%v", a, err)
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("worker did not expire persisted mute on startup")
	}
	first := due(1)
	stop := jobs.StartIssueMaintenance(ctx, db, time.Hour)
	defer stop()
	waitExpired(db, first)
	stopped := make(chan struct{})
	go func() { stop(); stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not cancel the configured idle wait")
	}
	second := due(2)
	beforeCanceledDetail, err := db.IssueDetail(ctx, owner.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	beforeCanceled := beforeCanceledDetail.Issue
	beforeHistory, _, err := db.IssueActivities(ctx, owner.ID, second, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Block the real activity insert after the worker's status/condition writes.
	// Observe its database dependency, then cancel through production stop. The
	// whole transaction must roll back instead of leaving a partially ended mute.
	blocker, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	var blockerPID int
	if err = blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.Exec(ctx, "LOCK TABLE issue_activity IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	stopCanceled := jobs.StartIssueMaintenance(ctx, db, time.Hour)
	defer stopCanceled()
	waitDeadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err = blocker.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)))`, blockerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(waitDeadline) {
			t.Fatal("worker never reached its real activity write")
		}
		time.Sleep(5 * time.Millisecond)
	}
	canceled := make(chan struct{})
	go func() { stopCanceled(); close(canceled) }()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not cancel a blocked expiry transaction")
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// pgx can finish cancellation in Go before its asynchronous connection
	// cleanup releases PostgreSQL locks. Wait for that actual rollback before
	// starting a new SKIP LOCKED worker with an hour-long idle interval.
	releaseCtx, releaseCancel := context.WithTimeout(ctx, 5*time.Second)
	defer releaseCancel()
	var released string
	if err = admin.QueryRow(releaseCtx, "SELECT id::text FROM issues WHERE id=$1::bigint FOR UPDATE", second).Scan(&released); err != nil {
		t.Fatalf("canceled worker did not release its issue row: %v", err)
	}
	afterCanceledDetail, err := db.IssueDetail(ctx, owner.ID, second)
	afterCanceled := afterCanceledDetail.Issue
	if err != nil || !reflect.DeepEqual(afterCanceled, beforeCanceled) {
		t.Fatalf("canceled worker partially changed mute: %+v %v", afterCanceled, err)
	}
	afterHistory, _, err := db.IssueActivities(ctx, owner.ID, second, 1)
	if err != nil || !reflect.DeepEqual(afterHistory, beforeHistory) {
		t.Fatalf("canceled worker partially recorded activity: %+v %v", afterHistory, err)
	}
	db2, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	detail, err := db2.IssueDetail(ctx, owner.ID, second)
	if err != nil || detail.Issue.Status != "ignored" || detail.Issue.Ignore == nil {
		t.Fatalf("stopped worker or reopen changed pending mute: %+v %v", detail.Issue, err)
	}
	stop2 := jobs.StartIssueMaintenance(ctx, db2, time.Hour)
	defer stop2()
	waitExpired(db2, second)
	waitExpired(db2, first)
}
