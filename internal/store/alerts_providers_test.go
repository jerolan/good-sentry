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

// PostgreSQL owns038 preservation and durable admission of every explicit
// incoming-webhook recipient. Provider serialization and acknowledgment remain
// pure/transport owners in internal/notify.
func TestProviderMigration038PreservesHistoryAndRecipientTypes(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	legacy, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("Before038"))
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
 ALTER TABLE alert_deliveries ADD CONSTRAINT alert_deliveries_recipient_type_check CHECK(recipient_type IN ('email','webhook','slack'));
 DELETE FROM schema_migrations WHERE version=38`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 37)
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if after := groupDatabaseSnapshot(t, ctx, reopened, 37); !reflect.DeepEqual(before, after) {
			reopened.Close()
			t.Fatal("038 changed existing rules, deliveries, attempts or earlier receipts")
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=38`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/038_provider_alerts.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("038 checksum receipt absent or changed", err)
		}
		reopened.Close()
	}

	providers := []string{"webhook", "slack", "discord", "teams", "googlechat", "feishu", "ntfy", "zulip"}
	configuration := alertConfig("Provider family")
	configuration.Recipients = nil
	for _, provider := range providers {
		configuration.Recipients = append(configuration.Recipients, notify.Recipient{Type: provider, Destination: "https://hooks.example.invalid/" + provider})
	}
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil || n != len(providers) {
		t.Fatalf("provider intents not admitted after038: %d, %v", n, err)
	}
	seen := make(map[string]bool)
	for range providers {
		claimed, err := db.ClaimAlertDelivery(ctx, 3, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		seen[claimed.Recipient.Type] = true
		if claimed.RuleID != rule.ID || claimed.Payload.Kind != "test" || claimed.Payload.URL == "" {
			t.Fatalf("provider delivery lost compact context: %+v", claimed)
		}
		if err := db.CompleteAlertDelivery(ctx, claimed, notify.Outcome{Disposition: notify.Delivered, Reason: "ok", ElapsedMS: 1}, 3); err != nil {
			t.Fatal(err)
		}
	}
	for _, provider := range providers {
		if !seen[provider] {
			t.Fatalf("provider %s was not durably admitted", provider)
		}
	}
}
