//go:build integration

package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"goodsentry/internal/uptime"
)

func TestCompatibleHeartbeatAndEndpointMigration(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	cfg := uptime.Defaults()
	cfg.Type, cfg.Name, cfg.IntervalSeconds = uptime.Heartbeat, "Endpoint heartbeat", 60
	monitor, err := db.CreateMonitor(ctx, owner.ID, org.Slug, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if monitor.EndpointID == "" || monitor.HeartbeatKey == "" {
		t.Fatalf("monitor endpoint=%q key=%q", monitor.EndpointID, monitor.HeartbeatKey)
	}
	check, err := db.CompatibleHeartbeat(ctx, org.Slug, monitor.EndpointID)
	if err != nil || !check.Up || check.Reason != "heartbeat_received" || check.StartedAt.IsZero() {
		t.Fatalf("heartbeat=%+v err=%v", check, err)
	}
	if _, err = db.CompatibleHeartbeat(ctx, "foreign-org", monitor.EndpointID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign org error=%v", err)
	}
	if _, err = db.CompatibleHeartbeat(ctx, org.Slug, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid endpoint error=%v", err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE monitors SET enabled=false WHERE id::text=$1`, monitor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompatibleHeartbeat(ctx, org.Slug, monitor.EndpointID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled monitor error=%v", err)
	}
	_, _ = db.pool.Exec(ctx, `UPDATE monitors SET enabled=true WHERE id::text=$1`, monitor.ID)

	monitorsBefore := func() string {
		t.Helper()
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m)-'endpoint_id' ORDER BY id),'[]')::text FROM monitors m`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	checksBefore := func() string {
		t.Helper()
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY id),'[]')::text FROM monitor_checks c`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	beforeMonitors, beforeChecks := monitorsBefore(), checksBefore()
	if _, err = db.pool.Exec(ctx, `DROP INDEX monitors_endpoint_id; ALTER TABLE monitors DROP COLUMN endpoint_id; DELETE FROM schema_migrations WHERE version=57`); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if monitorsBefore() != beforeMonitors || checksBefore() != beforeChecks {
			reopened.Close()
			t.Fatal("057 changed prior monitors or checks")
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=57").Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/057_monitor_endpoint_ids.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatalf("057 receipt=%q err=%v", receipt, err)
		}
		var total, distinct int
		if err := reopened.pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT endpoint_id) FROM monitors`).Scan(&total, &distinct); err != nil || total != distinct || total == 0 {
			reopened.Close()
			t.Fatalf("057 endpoints total=%d distinct=%d err=%v", total, distinct, err)
		}
		reopened.Close()
	}
}
