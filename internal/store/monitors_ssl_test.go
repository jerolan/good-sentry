//go:build integration

package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"goodsentry/internal/uptime"

	"github.com/jackc/pgx/v5/pgconn"
)

// The migration owner verifies existing monitor/check rows survive the widened
// kind constraint. Shared monitor tests own thresholds, fencing and retention.
func TestMonitorSSLMigration033PreservesAndReopens(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	c := monitorConfig("Before033 HTTP")
	c.Project, c.IntervalSeconds = project.Slug, 86400
	legacy, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	code, latency := 200, int64(9)
	if _, err := db.CompleteMonitor(ctx, dueMonitor(t, ctx, db, legacy.ID), uptime.Result{Up: true, StatusCode: &code, ResponseTimeMS: &latency, Reason: "ok"}); err != nil {
		t.Fatal(err)
	}
	heartbeat := uptime.Defaults()
	heartbeat.Name, heartbeat.Type, heartbeat.IntervalSeconds = "Before033 heartbeat", uptime.Heartbeat, 86400
	hb, err := db.CreateMonitor(ctx, owner.ID, org.Slug, heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Heartbeat(ctx, hb.HeartbeatKey); err != nil {
		t.Fatal(err)
	}
	// Restore only the pre033 kind constraint and receipt in this fixture's
	// isolated schema. Keep all earlier rows and migration checksums intact.
	if _, err := db.pool.Exec(ctx, `ALTER TABLE monitors DROP CONSTRAINT monitors_kind_check;
 ALTER TABLE monitors ADD CONSTRAINT monitors_kind_check CHECK(kind IN ('HEAD','GET','POST','TCP','heartbeat'));
 DELETE FROM schema_migrations WHERE version=33`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 32)
	databaseURL := db.pool.Config().ConnString()
	for i := range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(reopened.Close)
		if after := groupDatabaseSnapshot(t, ctx, reopened, 32); !reflect.DeepEqual(before, after) {
			t.Fatal("033 rewrote existing rows or earlier migration receipts")
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=33`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/033_monitors_ssl.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatal("033 checksum receipt absent or changed", err)
		}
		if i == 0 {
			reopened.Close()
		} else {
			db = reopened
		}
	}

	c.Name, c.Type, c.URL, c.Environment = "Certificate after033", uptime.SSL, "https://Certs.Example.test:8443/", "production"
	ssl, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil || !reflect.DeepEqual(ssl.Config, c) || ssl.Status != uptime.Unknown || ssl.LastCheck != nil || ssl.HeartbeatKey != "" {
		t.Fatalf("SSL monitor not admitted after033: %+v, %v", ssl, err)
	}
	lease := dueMonitor(t, ctx, db, ssl.ID)
	if lease.Type != uptime.SSL || lease.URL != c.URL || lease.Environment != c.Environment || lease.Token == "" {
		t.Fatalf("SSL configuration lost while claiming: %+v", lease)
	}
	latency = 17
	check, err := db.CompleteMonitor(ctx, lease, uptime.Result{Up: true, ResponseTimeMS: &latency, Reason: "ok"})
	if err != nil || check.Status != uptime.Up || !check.Up || check.Reason != "ok" || check.StatusCode != nil || check.ResponseTimeMS == nil || *check.ResponseTimeMS != latency {
		t.Fatalf("SSL result lost nullable HTTP status or latency: %+v, %v", check, err)
	}
	stored, err := db.Monitor(ctx, owner.ID, org.Slug, ssl.ID)
	if err != nil || !reflect.DeepEqual(stored.Config, c) || stored.Status != uptime.Up || stored.LastCheck == nil || !stored.LastCheck.Equal(check.CompletedAt) {
		t.Fatalf("SSL completion not persisted: %+v, %v", stored, err)
	}
	history, more, err := db.MonitorChecks(ctx, owner.ID, org.Slug, ssl.ID, false, 1)
	if err != nil || more || len(history) != 1 || !reflect.DeepEqual(history[0], check) {
		t.Fatalf("SSL history result changed: %+v, more=%v, %v", history, more, err)
	}
	beforeInvalid := groupDatabaseSnapshot(t, ctx, db, 0)
	c.Name, c.Type = "Unknown kind after033", uptime.Kind("UNKNOWN")
	_, err = db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	var constraint *pgconn.PgError
	if !errors.As(err, &constraint) || constraint.Code != "23514" || constraint.ConstraintName != "monitors_kind_check" {
		t.Fatalf("unknown kind did not reach monitors_kind_check: %v", err)
	}
	groupRequireUnchanged(t, ctx, db, beforeInvalid)
}
