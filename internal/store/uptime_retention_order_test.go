//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"goodsentry/internal/uptime"
)

// Real blocking transactions protect the two ordering contracts that sequential
// cleanup cannot reach: an already waiting heartbeat and a frozen exact cutoff.
// A parent lock regression or a moving/inclusive cutoff fails without test hooks.
func TestUptimeRetentionOrdersWaitingHeartbeatAndLease(t *testing.T) {
	f := resolutionStore(t)
	c := uptime.Defaults()
	c.Name, c.Type = "Waiting heartbeat", uptime.Heartbeat
	m, err := f.db.CreateMonitor(f.ctx, f.owner.ID, f.org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Heartbeat(f.ctx, m.HeartbeatKey); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CompleteMonitor(f.ctx, dueMonitor(t, f.ctx, f.db, m.ID), uptime.Result{Reason: "heartbeat_missed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE monitor_checks SET started_at=started_at-interval '2 days',completed_at=completed_at-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	lease := dueMonitor(t, f.ctx, f.db, m.ID)
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 25)
	barrier, pid := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, "SELECT id FROM monitors FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		fresh, err := f.db.Heartbeat(ctx, m.HeartbeatKey)
		if err == nil && (!fresh.IsChange || fresh.PreviousStatus != uptime.Down || fresh.Status != uptime.Up) {
			err = fmt.Errorf("waiting heartbeat lost prior state: %+v", fresh)
		}
		done <- err
	}()
	groupWaitBlocked(t, ctx, barrier, pid, 1)
	bounded, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	if n, err := f.db.CleanupMonitorChecks(bounded, 1); err != nil || n != 2 {
		t.Fatal("cleanup blocked behind the waiting parent writer", n, err)
	}
	after := groupDatabaseSnapshot(t, ctx, f.db, 25)
	for table, checkpoint := range before {
		if table != "monitor_checks" && after[table] != checkpoint {
			t.Fatal("cleanup changed parent or independent state", table)
		}
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, ctx, done)
	if _, err := f.db.CompleteMonitor(ctx, lease, uptime.Result{Reason: "heartbeat_missed"}); !errors.Is(err, ErrConflict) {
		t.Fatal("old lease overwrote the fresh heartbeat", err)
	}
	history, more, err := f.db.MonitorChecks(ctx, f.owner.ID, f.org.Slug, m.ID, false, 1)
	if err != nil || more || len(history) != 1 || !history[0].IsChange || history[0].PreviousStatus != uptime.Down || history[0].Status != uptime.Up {
		t.Fatal("fresh history lost its retained transition", history, more, err)
	}
}

func TestUptimeRetentionKeepsExactFrozenCompletionCutoff(t *testing.T) {
	f := resolutionStore(t)
	c := uptime.Defaults()
	c.Name, c.Type = "Exact completion boundary", uptime.Heartbeat
	m, err := f.db.CreateMonitor(f.ctx, f.owner.ID, f.org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Heartbeat(f.ctx, m.HeartbeatKey); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 25)
	barrier, barrierPID := captureItemsBarrier(t, f)
	if _, err := barrier.Exec(f.ctx, "LOCK TABLE monitor_checks IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		n, err := f.db.CleanupMonitorChecks(ctx, 1)
		if err == nil && n != 0 {
			err = fmt.Errorf("exact frozen completion boundary deleted: %d", n)
		}
		done <- err
	}()
	pid := groupWaitBlocked(t, ctx, barrier, barrierPID, 1)[0]
	if tag, err := barrier.Exec(ctx, `UPDATE monitor_checks SET
 started_at=(SELECT xact_start-interval '24 hours 1 second' FROM pg_stat_activity WHERE pid=$1),
 completed_at=(SELECT xact_start-interval '24 hours' FROM pg_stat_activity WHERE pid=$1)`, pid); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("could not set the owned exact completion boundary", tag, err)
	}
	var boundary time.Time
	if err := barrier.QueryRow(ctx, "SELECT completed_at FROM monitor_checks").Scan(&boundary); err != nil {
		t.Fatal(err)
	}
	if err := barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	deleteOrderAwait(t, ctx, done)
	history, more, err := f.db.MonitorChecks(ctx, f.owner.ID, f.org.Slug, m.ID, false, 1)
	if err != nil || more || len(history) != 1 || !history[0].CompletedAt.Equal(boundary) {
		t.Fatal("exact completion boundary changed after waiting", history, more, err)
	}
	after := groupDatabaseSnapshot(t, ctx, f.db, 25)
	for table, checkpoint := range before {
		if table != "monitor_checks" && after[table] != checkpoint {
			t.Fatal("waiting at the cutoff changed independent state", table)
		}
	}
}
