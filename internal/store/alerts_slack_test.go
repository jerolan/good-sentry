//go:build integration

package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/notify"
)

// PostgreSQL owns migration preservation and the provider's durable retry floor;
// alert_queue_test owns the shared backoff sequence and lease fencing.
func TestSlackMigration034PreservesHistoryAndDurableRetryHint(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	legacy, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("Before034"))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, legacy.ID); err != nil || n != 1 {
		t.Fatal("legacy intent", n, err)
	}
	lease, err := db.ClaimAlertDelivery(ctx, 3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteAlertDelivery(ctx, lease, notify.Outcome{Disposition: notify.Delivered, Reason: "ok", ElapsedMS: 5}, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `ALTER TABLE alert_deliveries DROP CONSTRAINT alert_deliveries_recipient_type_check;
 ALTER TABLE alert_deliveries ADD CONSTRAINT alert_deliveries_recipient_type_check CHECK(recipient_type IN ('email','webhook'));
 DELETE FROM schema_migrations WHERE version=34`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 33)
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(reopened.Close)
		if after := groupDatabaseSnapshot(t, ctx, reopened, 33); !reflect.DeepEqual(before, after) {
			t.Fatal("034 changed existing rules, deliveries, attempts or earlier receipts")
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=34`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/034_slack_alerts.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatal("034 checksum receipt absent or changed", err)
		}
		reopened.Close()
	}
	c := alertConfig("Slack retry fixture")
	c.Recipients = []notify.Recipient{{Type: "slack", Destination: "https://example.invalid/services/fake?channel=fixture"}}
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, c)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil || n != 1 {
		t.Fatal("Slack intent not admitted after034", n, err)
	}
	lease, err = db.ClaimAlertDelivery(ctx, 3, time.Second)
	p := lease.Payload
	if err != nil || lease.Recipient != c.Recipients[0] || lease.RuleID != rule.ID || lease.RuleName != c.Name || p.Version != 1 || p.ID != lease.ID || p.Kind != "test" || p.Rule.ID != rule.ID || p.Rule.Name != c.Name || p.Project.ID != project.ID || p.Project.Slug != project.Slug || p.Organization != org.Slug || p.OccurredAt.IsZero() || p.URL != "/?view=issues&org="+org.Slug {
		t.Fatalf("Slack claim lost destination/context snapshot: %+v, %v", lease, err)
	}
	for _, invalid := range []notify.Outcome{
		{Disposition: notify.Retry, Reason: "http_status", RetryAfter: -time.Second},
		{Disposition: notify.Delivered, Reason: "ok", RetryAfter: time.Second},
	} {
		checkpoint := groupDatabaseSnapshot(t, ctx, db, 0)
		if err := db.CompleteAlertDelivery(ctx, lease, invalid, 3); err == nil || errors.Is(err, ErrConflict) {
			t.Fatalf("invalid hint accepted or rejected by lease guard: %+v, %v", invalid, err)
		}
		groupRequireUnchanged(t, ctx, db, checkpoint)
	}
	code := 429
	if err := db.CompleteAlertDelivery(ctx, lease, notify.Outcome{Disposition: notify.Retry, Reason: "http_status", StatusCode: &code, ElapsedMS: 12, RetryAfter: 2 * time.Hour}, 3); err != nil {
		t.Fatal(err)
	}
	delivery, history, err := db.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, lease.ID)
	if err != nil || delivery.Status != notify.Pending || delivery.NextAttempt.Sub(delivery.UpdatedAt) != 2*time.Hour || delivery.LastStatusCode == nil || *delivery.LastStatusCode != 429 || len(history) != 1 || history[0].Status != "retry" || history[0].CompletedAt == nil {
		t.Fatalf("429 provider minimum not durably scheduled: %+v, history=%+v, %v", delivery, history, err)
	}
	checkpoint := groupDatabaseSnapshot(t, ctx, db, 0)
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	again, attempts, err := reopened.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, lease.ID)
	if err != nil || !reflect.DeepEqual(delivery, again) || !reflect.DeepEqual(history, attempts) {
		t.Fatalf("restart changed hinted delivery/attempt history: %+v, %+v, %v", again, attempts, err)
	}
	if _, err := reopened.ClaimAlertDelivery(ctx, 3, time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restart claimed Slack before provider deadline: %v", err)
	}
	groupRequireUnchanged(t, ctx, reopened, checkpoint)
	// Advance only this fixture's due clock to exercise a subsequent ordinary
	// retry; the complete exponential sequence remains owned by alert_queue_test.
	if _, err := reopened.pool.Exec(ctx, "UPDATE alert_deliveries SET next_attempt=clock_timestamp()-interval '1 second' WHERE id=$1", lease.ID); err != nil {
		t.Fatal(err)
	}
	next, err := reopened.ClaimAlertDelivery(ctx, 3, time.Second)
	if err != nil || next.ID != lease.ID || next.Attempts != 2 {
		t.Fatalf("due Slack retry not claimable: %+v, %v", next, err)
	}
	if err := reopened.CompleteAlertDelivery(ctx, next, notify.Outcome{Disposition: notify.Retry, Reason: "http_status", StatusCode: &code}, 3); err != nil {
		t.Fatal(err)
	}
	ordinary, _, err := reopened.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, lease.ID)
	if err != nil || ordinary.NextAttempt.Sub(ordinary.UpdatedAt) != 30*time.Second {
		t.Fatalf("hint leaked into a later ordinary retry: %+v, %v", ordinary, err)
	}
}
