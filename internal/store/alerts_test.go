//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/event"
	"goodsentry/internal/notify"
	"goodsentry/internal/uptime"
)

func alertConfig(name string) notify.Config {
	c := notify.Defaults()
	c.Name = name
	c.Recipients = []notify.Recipient{{Type: "email", Destination: "owner@example.test"}}
	return c
}

func ingestAlertEvent(ctx context.Context, db *Store, projectID string, id int, environment, kind string) error {
	ev := event.Event{ID: fmt.Sprintf("%032x", id), Type: kind, Title: "Checkout failed", Level: "error", Platform: "go",
		Environment: environment, Timestamp: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Fingerprint: fmt.Sprintf("alert-%d", id), Data: json.RawMessage(`{"message":"Checkout failed"}`)}
	_, _, err := db.Ingest(ctx, projectID, ev)
	return err
}

func TestErrorAlertsUseReceiptCrossingCooldownAndEnvironment(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	c := alertConfig("Production failures")
	c.Quantity, c.CooldownSeconds, c.Environments = 2, 60, []string{"production"}
	c.Recipients = []notify.Recipient{{Type: "team_email"}, {Type: "email", Destination: owner.Email}}
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, c)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Go(func() { errs <- ingestAlertEvent(ctx, db, project.ID, i+1, "production", "error") })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, more, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(items) != 1 || more || items[0].Recipient.Type != "email" || items[0].Recipient.Destination != owner.Email || items[0].Payload.Count != 2 || items[0].Payload.WindowMinutes != 5 || items[0].Payload.Issue == nil || items[0].Payload.EventID == "" || items[0].Payload.OccurredAt.Year() == 2000 {
		t.Fatalf("concurrent crossing/team snapshots: %+v more=%v, %v", items, more, err)
	}
	for range 3 {
		if err := ingestAlertEvent(ctx, db, project.ID, 1, "production", "error"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, "UPDATE events SET created_at=clock_timestamp()-interval '10 minutes' WHERE project_id::text=$1", project.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{20, 21} {
		if err := ingestAlertEvent(ctx, db, project.ID, id, "production", "message"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, "UPDATE alert_rules SET last_triggered=clock_timestamp()-interval '61 seconds' WHERE id::text=$1", rule.ID); err != nil {
		t.Fatal(err)
	}
	if err := ingestAlertEvent(ctx, db, project.ID, 22, "production", "error"); err != nil {
		t.Fatal(err)
	}
	items, _, err = db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("cooldown crossing became a delayed alert or retry duplicated: %+v, %v", items, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE events SET created_at=clock_timestamp()-interval '10 minutes' WHERE project_id::text=$1", project.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{30, 31} {
		if err := ingestAlertEvent(ctx, db, project.ID, id, "production", "error"); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err = db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(items) != 2 {
		t.Fatalf("new post-window crossing missing: %+v, %v", items, err)
	}
	staging := alertConfig("Staging messages")
	staging.Environments = []string{"staging"}
	stage, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, staging)
	if err != nil {
		t.Fatal(err)
	}
	if err := ingestAlertEvent(ctx, db, project.ID, 40, "staging", "transaction"); err != nil {
		t.Fatal(err)
	}
	if deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, stage.ID, 1); err != nil || len(deliveries) != 0 {
		t.Fatalf("transaction created frequency alert: %+v, %v", deliveries, err)
	}
	if err := ingestAlertEvent(ctx, db, project.ID, 41, "staging", "message"); err != nil {
		t.Fatal(err)
	}
	if deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, stage.ID, 1); err != nil || len(deliveries) != 1 {
		t.Fatalf("message ignored or transaction contributed to count: %+v, %v", deliveries, err)
	}
}

func TestUptimeAlertsOnlyQueueConfirmedProjectTransitions(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	c := alertConfig("Uptime transitions")
	c.Kind, c.Environments, c.CooldownSeconds = notify.Uptime, []string{"production"}, 604800
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, c)
	if err != nil {
		t.Fatal(err)
	}
	mc := uptime.Defaults()
	mc.Name, mc.Type, mc.Project, mc.Environment = "Importer", uptime.Heartbeat, project.Slug, "production"
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, mc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Heartbeat(ctx, m.HeartbeatKey); err != nil {
		t.Fatal(err)
	}
	if rows, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1); err != nil || len(rows) != 0 {
		t.Fatalf("initial up queued notification: %+v, %v", rows, err)
	}
	lease := dueMonitor(t, ctx, db, m.ID)
	down, err := db.CompleteMonitor(ctx, lease, uptime.Result{Reason: "heartbeat_missed"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := queueMonitorAlerts(ctx, tx, m.ID, down); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Heartbeat(ctx, m.HeartbeatKey); err != nil {
		t.Fatal(err)
	}
	rows, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(rows) != 2 || rows[0].Payload.Kind != "uptime_recovered" || rows[1].Payload.Kind != "uptime_down" || rows[0].Payload.Monitor == nil || rows[0].Payload.Monitor.ID != m.ID {
		t.Fatalf("down/recovery or trigger dedupe: %+v, %v", rows, err)
	}
}

func TestAlertPermissionsAssignedTeamAndDestinationSnapshots(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	var member, outsider, otherOrg, team string
	for _, identity := range []struct {
		email string
		id    *string
	}{{"member@alert.test", &member}, {"outsider@alert.test", &outsider}} {
		if err := db.pool.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,'Alert user') RETURNING id::text", identity.email, []byte("already-hashed")).Scan(identity.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, member); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('other-alerts','Other alerts') RETURNING id::text").Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", otherOrg, outsider); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, "INSERT INTO teams(organization_id,slug,name) VALUES($1,'custom','Custom') RETURNING id::text", org.ID).Scan(&team); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{member, outsider} {
		if _, err := db.pool.Exec(ctx, "INSERT INTO team_memberships(team_id,user_id) VALUES($1,$2)", team, id); err != nil {
			t.Fatal(err)
		}
	}
	assignProjectTeam(t, ctx, db, project.ID, team)
	c := alertConfig("Assigned team")
	c.Recipients = []notify.Recipient{{Type: "team_email"}}
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, c)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil || n != 1 {
		t.Fatalf("assigned-team/current-members recipients: %d, %v", n, err)
	}
	deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(deliveries) != 1 || deliveries[0].Recipient.Destination != "member@alert.test" {
		t.Fatalf("default/foreign member leaked into assigned team: %+v, %v", deliveries, err)
	}
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"list", func() error { _, err := db.Alerts(ctx, member, org.Slug, project.ID); return err }},
		{"read", func() error { _, err := db.Alert(ctx, member, org.Slug, project.ID, rule.ID); return err }},
		{"deliveries", func() error { _, _, err := db.AlertDeliveries(ctx, member, org.Slug, project.ID, "", 1); return err }},
		{"delivery", func() error {
			_, _, err := db.AlertDelivery(ctx, member, org.Slug, project.ID, deliveries[0].ID)
			return err
		}},
		{"test", func() error { _, err := db.TestAlert(ctx, member, org.Slug, project.ID, rule.ID); return err }},
		{"create", func() error { _, err := db.CreateAlert(ctx, member, org.Slug, project.ID, c); return err }},
		{"replace", func() error { _, err := db.UpdateAlert(ctx, member, org.Slug, project.ID, rule.ID, c); return err }},
		{"delete", func() error { return db.DeleteAlert(ctx, member, org.Slug, project.ID, rule.ID) }},
	} {
		if err := tc.run(); !errors.Is(err, ErrForbidden) {
			t.Fatalf("member %s exposed alert data/action: %v", tc.name, err)
		}
	}
	otherProject, err := db.CreateProject(ctx, outsider, "other-alerts", "Other project", "go")
	if err != nil {
		t.Fatal(err)
	}
	otherRule, err := db.CreateAlert(ctx, outsider, "other-alerts", otherProject.ID, alertConfig("Other rule"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Alerts(ctx, owner.ID, org.Slug, otherProject.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign project listed: %v", err)
	}
	if _, err := db.Alert(ctx, owner.ID, org.Slug, project.ID, otherRule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign rule read: %v", err)
	}
	if _, err := db.UpdateAlert(ctx, owner.ID, org.Slug, project.ID, otherRule.ID, c); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign rule replaced: %v", err)
	}
	if err := db.DeleteAlert(ctx, owner.ID, org.Slug, project.ID, otherRule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign rule deleted: %v", err)
	}
	changed := c
	changed.Kind = notify.Uptime
	if _, err := db.UpdateAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("kind changed: %v", err)
	}
	if _, err := db.pool.Exec(ctx, "DELETE FROM memberships WHERE organization_id::text=$1 AND user_id::text=$2", org.ID, member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("removed membership still notified through team: %v", err)
	}
	snapshot, _, err := db.AlertDelivery(ctx, owner.ID, org.Slug, project.ID, deliveries[0].ID)
	if err != nil || snapshot.Recipient.Destination != "member@alert.test" {
		t.Fatalf("destination snapshot changed with memberships: %+v, %v", snapshot, err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, member); err != nil {
		t.Fatal(err)
	}
	secondTeam, err := db.CreateTeam(ctx, owner.ID, org.Slug, "Second alert team")
	if err != nil {
		t.Fatal(err)
	}
	var secondMember string
	if err := db.pool.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES('second@alert.test',$1,'Second alert user') RETURNING id::text", []byte("already-hashed")).Scan(&secondMember); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, secondMember); err != nil {
		t.Fatal(err)
	}
	if err := db.AddTeamMember(ctx, owner.ID, org.Slug, secondTeam.ID, secondMember); err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	if _, err := db.CompatibleSetProjectTeam(ctx, access, project.ID, secondTeam.Slug, true); err != nil {
		t.Fatal(err)
	}
	multi := alertConfig("Multi-team recipients")
	multi.Recipients = []notify.Recipient{{Type: "team_email"}}
	multiRule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, multi)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, multiRule.ID); err != nil || n != 2 {
		t.Fatalf("multi-team recipients=%d err=%v", n, err)
	}
	multiDeliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, multiRule.ID, 1)
	if err != nil || len(multiDeliveries) != 2 {
		t.Fatalf("multi-team deliveries=%+v err=%v", multiDeliveries, err)
	}
	destinations := map[string]bool{}
	for _, delivery := range multiDeliveries {
		destinations[delivery.Recipient.Destination] = true
	}
	if !destinations["member@alert.test"] || !destinations["second@alert.test"] {
		t.Fatalf("multi-team destinations=%v", destinations)
	}
}

func TestAlertDeliveryHistoryPagesAreComplete(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("History"))
	if err != nil {
		t.Fatal(err)
	}
	for range 26 {
		if _, err := db.TestAlert(ctx, owner.ID, org.Slug, project.ID, rule.ID); err != nil {
			t.Fatal(err)
		}
	}
	first, more, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(first) != 25 || !more {
		t.Fatalf("first history page count=%d more=%v, %v", len(first), more, err)
	}
	second, more, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 2)
	if err != nil || len(second) != 1 || more {
		t.Fatalf("second history page count=%d more=%v, %v", len(second), more, err)
	}
	for _, d := range first {
		if d.ID == second[0].ID {
			t.Fatal("history pages overlap")
		}
	}
}

func TestErrorAlertsBoundPayloadEnvironmentWithoutChangingStoredEvent(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	rule, err := db.CreateAlert(ctx, owner.ID, org.Slug, project.ID, alertConfig("All environments"))
	if err != nil {
		t.Fatal(err)
	}
	environment := strings.Repeat("界", 10000)
	raw, err := json.Marshal(map[string]any{"event_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "message": "failed", "environment": environment})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Ingest(ctx, project.ID, ev); err != nil {
		t.Fatal(err)
	}
	deliveries, _, err := db.AlertDeliveries(ctx, owner.ID, org.Slug, project.ID, rule.ID, 1)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("unfiltered huge environment delivery: count=%d err=%v", len(deliveries), err)
	}
	if deliveries[0].Payload.Environment != strings.Repeat("界", 128) || deliveries[0].Payload.EventID != ev.ID {
		t.Fatalf("payload environment is not bounded or event identity changed: environment rune count=%d event=%q", len([]rune(deliveries[0].Payload.Environment)), deliveries[0].Payload.EventID)
	}
	var storedEnvironment, rawEnvironment string
	if err := db.pool.QueryRow(ctx, "SELECT environment,data->>'environment' FROM events WHERE project_id=$1::bigint AND event_id=$2", project.ID, ev.ID).Scan(&storedEnvironment, &rawEnvironment); err != nil {
		t.Fatal(err)
	}
	if storedEnvironment != environment || rawEnvironment != environment {
		t.Fatal("bounded alert snapshot changed the raw SDK event or matching environment")
	}
}
