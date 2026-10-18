//go:build integration

package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/notify"
	"goodsentry/internal/uptime"
)

// Real child expiry must preserve parent state and alert snapshots, and fresh
// checks must continue transitions from that state. Existing monitor tests never
// remove history and cannot detect rebuilding state or reusing old trigger IDs.
// Use actual Store completion/heartbeat paths without production test hooks.
func TestUptimeRetentionUsesCompletionAndPreservesStateAndAlerts(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	alert := alertConfig("Retained transition snapshots")
	alert.Kind, alert.Environments, alert.CooldownSeconds = notify.Uptime, []string{"production"}, 604800
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alert)
	if err != nil {
		t.Fatal(err)
	}
	c := uptime.Defaults()
	c.Name, c.Type, c.Project, c.Environment = "Retained heartbeat", uptime.Heartbeat, project.Slug, "production"
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.Heartbeat(ctx, m.HeartbeatKey)
	if err != nil {
		t.Fatal(err)
	}
	down, err := db.CompleteMonitor(ctx, dueMonitor(t, ctx, db, m.ID), uptime.Result{Reason: "heartbeat_missed"})
	if err != nil || down.Status != uptime.Down {
		t.Fatal(down, err)
	}
	kept, err := db.Heartbeat(ctx, m.HeartbeatKey)
	if err != nil || kept.PreviousStatus != uptime.Down || kept.Status != uptime.Up {
		t.Fatal(kept, err)
	}
	deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(deliveries) != 2 {
		t.Fatal("missing real transition snapshots", len(deliveries), err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE monitor_checks SET started_at=started_at-interval '2 days',completed_at=completed_at-interval '2 days' WHERE id::text=ANY($1::text[])`, []string{first.ID, down.ID}); err != nil {
		t.Fatal(err)
	}
	// An old probe start must not shorten a current completed result's lifetime.
	if _, err := db.pool.Exec(ctx, `UPDATE monitor_checks SET started_at=completed_at-interval '3 days' WHERE id::text=$1`, kept.ID); err != nil {
		t.Fatal(err)
	}
	history, _, err := db.MonitorChecks(ctx, owner.ID, org.Slug, m.ID, false, 1)
	if err != nil || len(history) != 3 {
		t.Fatal(history, err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 25)
	if n, err := db.CleanupMonitorChecks(ctx, 1); err != nil || n != 2 {
		t.Fatalf("expired completed checks=%d want2 err=%v", n, err)
	}
	after := groupDatabaseSnapshot(t, ctx, db, 25)
	for table, checkpoint := range before {
		if table != "monitor_checks" && after[table] != checkpoint {
			t.Fatal("history expiry changed independent state", table)
		}
	}
	remaining, more, err := db.MonitorChecks(ctx, owner.ID, org.Slug, m.ID, false, 1)
	if err != nil || more || len(remaining) != 1 || !reflect.DeepEqual(remaining[0], history[0]) {
		t.Fatal("retained completed check changed", remaining, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE monitor_checks SET started_at=started_at-interval '2 days',completed_at=completed_at-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, ctx, db, 25)
	if n, err := db.CleanupMonitorChecks(ctx, 1); err != nil || n != 1 {
		t.Fatal("last completed check not removed", n, err)
	}
	after = groupDatabaseSnapshot(t, ctx, db, 25)
	for table, checkpoint := range before {
		if table != "monitor_checks" && after[table] != checkpoint {
			t.Fatal("empty history reset parent or alert state", table)
		}
	}
	if empty, more, err := db.MonitorChecks(ctx, owner.ID, org.Slug, m.ID, false, 1); err != nil || more || len(empty) != 0 {
		t.Fatal("empty history unreadable", empty, more, err)
	}
	fresh, err := db.Heartbeat(ctx, m.HeartbeatKey)
	if err != nil || fresh.IsChange || fresh.PreviousStatus != uptime.Up || fresh.Status != uptime.Up || fresh.ID == first.ID || fresh.ID == down.ID || fresh.ID == kept.ID {
		t.Fatal("fresh heartbeat reset retained state or reused an old trigger identity", fresh, err)
	}
	next, err := db.CompleteMonitor(ctx, dueMonitor(t, ctx, db, m.ID), uptime.Result{Reason: "heartbeat_missed"})
	if err != nil || !next.IsChange || next.PreviousStatus != uptime.Up || next.Status != uptime.Down {
		t.Fatal("next real transition lost prior cached state", next, err)
	}
	all, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(all) != 3 {
		t.Fatal("expiry cancelled/requeued prior transition snapshots", len(all), err)
	}
}

func TestUptimeRetentionPreservesActiveLeaseAndFailureThreshold(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	c := monitorConfig("Leased threshold")
	c.ConfirmationThreshold = 3
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []uptime.Result{{Up: true, Reason: "ok"}, {Reason: "connection_error"}, {Reason: "connection_error"}} {
		if _, err := db.CompleteMonitor(ctx, dueMonitor(t, ctx, db, m.ID), result); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, `UPDATE monitor_checks SET started_at=started_at-interval '2 days',completed_at=completed_at-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	lease := dueMonitor(t, ctx, db, m.ID)
	before := groupDatabaseSnapshot(t, ctx, db, 25)
	parent, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Rollback(ctx)
	if _, err := parent.Exec(ctx, "SELECT id FROM monitors FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if n, err := db.CleanupMonitorChecks(bounded, 1); err != nil || n != 3 {
		t.Fatal("child cleanup waited for or changed its locked parent", n, err)
	}
	after := groupDatabaseSnapshot(t, ctx, db, 25)
	for table, checkpoint := range before {
		if table != "monitor_checks" && after[table] != checkpoint {
			t.Fatal("active lease/state changed during child cleanup", table)
		}
	}
	if err := parent.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimMonitor(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatal("cleanup released the existing lease", err)
	}
	check, err := db.CompleteMonitor(ctx, lease, uptime.Result{Reason: "connection_error"})
	if err != nil || !check.IsChange || check.PreviousStatus != uptime.Up || check.Status != uptime.Down {
		t.Fatal("winning completion lost its fence or threshold progress", check, err)
	}
	if _, err := db.CompleteMonitor(ctx, lease, uptime.Result{Up: true, Reason: "ok"}); !errors.Is(err, ErrConflict) {
		t.Fatal("old completion was accepted twice after history expiry", err)
	}
	current, err := db.Monitor(ctx, owner.ID, org.Slug, m.ID)
	if err != nil || current.ConsecutiveFailures != 3 || current.Status != uptime.Down {
		t.Fatal("expiry reset failure progress", current, err)
	}
}

func TestUptimeRetentionBoundedProgress(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, monitorConfig("Bounded history"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO monitor_checks(monitor_id,started_at,completed_at,up,status,previous_status,is_change,reason)
 SELECT $1,now()-interval '2 days',now()-interval '2 days',false,'unknown','unknown',false,'historical_fixture' FROM generate_series(1,601)`, m.ID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{250, 250, 101, 0} {
		if n, err := db.CleanupMonitorChecks(ctx, 1); err != nil || n != want {
			t.Fatalf("bounded check cleanup=%d want=%d err=%v", n, want, err)
		}
	}
}
