//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/notify"
)

func TestMattermostMigration058PreservesHistoryAndAdmitsRecipient(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	legacy, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("Before058"))
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
 ALTER TABLE alert_deliveries ADD CONSTRAINT alert_deliveries_recipient_type_check CHECK(recipient_type IN ('email','webhook','slack','discord','teams','googlechat','feishu','ntfy','zulip'));
 DELETE FROM schema_migrations WHERE version=58`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 57)
	databaseURL := db.pool.Config().ConnString()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if after := groupDatabaseSnapshot(t, ctx, reopened, 57); !reflect.DeepEqual(before, after) {
		t.Fatal("058 changed preexisting alert data, attempts or migration receipts")
	}
	var receipt string
	if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=58`).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.ReadFile("migrations/058_mattermost_alerts.sql")
	if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatal("058 checksum receipt absent or changed", err)
	}

	configuration := alertConfig("Mattermost")
	configuration.Recipients = []notify.Recipient{{Type: "mattermost", Destination: "https://hooks.example.test/mattermost/fixture"}}
	rule, err := reopened.CreateAlert(ctx, owner.ID, org.Slug, project.ID, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := reopened.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil || n != 1 {
		t.Fatalf("Mattermost test intent admission: %d, %v", n, err)
	}
	claimed, err := reopened.ClaimAlertDelivery(ctx, 3, time.Second)
	if err != nil || claimed.RuleID != rule.ID || claimed.Recipient.Type != "mattermost" || claimed.Payload.URL == "" {
		t.Fatalf("Mattermost durable intent: %+v, %v", claimed, err)
	}
}
