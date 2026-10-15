//go:build integration

package store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/notify"

	"github.com/jackc/pgx/v5/pgconn"
)

// Authoring gate: this owns the actual019->020 upgrade, opt-in scope boundary,
// cleanup index and exact preservation of old rows/receipts. Other migration
// owners cannot detect020 widening or rewriting their already-applied schemas.
// Reuse the isolated schema and complete snapshots; no production seam is needed.
func TestCompatibleIssueDeletionMigration020PreservesRowsScopesAndReopen(t *testing.T) {
	f := resolutionStore(t)
	config := alertConfig("Before deletion scope")
	config.Quantity = 1
	if _, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, config); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	ev := groupEvent(t, 1, "migration020-root", "Historical bytes", stamp)
	parts := CaptureItems{
		Attachments:   []CaptureAttachment{{Filename: "history.bin", ContentType: "application/octet-stream", Data: []byte{0, 255, 10, 128}}},
		EnvelopeItems: []CaptureEnvelopeItem{{Type: "future_history", Header: json.RawMessage(`{"type":"future_history","revision":19}`), Data: []byte{254, 0, 13}}},
	}
	root, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts)
	if err != nil || !fresh || root == "" {
		t.Fatalf("historical parent/bytes: root=%s fresh=%v err=%v", root, fresh, err)
	}
	alias := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "migration020-alias", "Historical alias", stamp.Add(time.Second)))
	if _, err = f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, alias}); err != nil {
		t.Fatal(err)
	}
	f.resolve(t, root, "release", "app@1")
	if _, err = f.db.UpdateIssue(f.ctx, f.owner.ID, root, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: f.owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.AddComment(f.ctx, f.owner.ID, root, "Recorded before destructive scope existed"); err != nil {
		t.Fatal(err)
	}
	muted := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 3, "migration020-muted", "Historical mute", stamp))
	if _, err = f.db.UpdateIssue(f.ctx, f.owner.ID, muted, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 5)}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.SaveEnvelopeItem(f.ctx, f.project.ID, "", "independent_history", json.RawMessage(`{"type":"independent_history"}`), []byte{0, 253}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.CreateSession(f.ctx, f.owner.ID, "migration020-session", "migration020-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	lease, err := f.db.ClaimAlertDelivery(f.ctx, 3, time.Second)
	if err != nil {
		t.Fatalf("historical delivery was not actually queued: %v", err)
	}
	if err = f.db.CompleteAlertDelivery(f.ctx, lease, notify.Outcome{Disposition: notify.Retry, Reason: "connection_error", ElapsedMS: 7}, 3); err != nil {
		t.Fatal(err)
	}
	legacyScopes := []string{"org:read", "project:read", "project:releases", "event:read", "event:write"}
	_, hash, prefix := apiTokenCredentials("migration020-restricted")
	restricted, err := f.db.CreateAPIToken(f.ctx, f.owner.ID, f.org.Slug, APITokenInput{Name: "Established restricted credential", Scopes: legacyScopes, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}, ExpiresInDays: 30}, hash, prefix)
	if err != nil {
		t.Fatal(err)
	}
	oldGrant, err := f.db.AuthenticateAPIToken(f.ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	_, defaultHash, _ := apiTokenFixture(t, f.ctx, f.db, f.owner.ID, f.org.Slug, "migration020-default", nil)
	defaultGrant, err := f.db.AuthenticateAPIToken(f.ctx, defaultHash)
	if err != nil {
		t.Fatal(err)
	}
	// Retire only020's metadata and receipt inside this test's owned schema.
	// All application rows and every001..019 receipt stay in the snapshot.
	if _, err = f.db.pool.Exec(f.ctx, `DROP INDEX envelope_items_project_event;
ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK (
 cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases','event:read','event:write']::text[]);
DELETE FROM schema_migrations WHERE version=20`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 19)
	checkRejectedScope := func(db *Store, tokenID, scope string) {
		t.Helper()
		tx, err := db.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, rejected := tx.Exec(f.ctx, "UPDATE api_tokens SET scopes=ARRAY[$2]::text[] WHERE id=$1::bigint", tokenID, scope)
		if err := tx.Rollback(f.ctx); err != nil {
			t.Fatal(err)
		}
		var constraint *pgconn.PgError
		if !errors.As(rejected, &constraint) || constraint.Code != "23514" || constraint.ConstraintName != "api_tokens_scopes_check" {
			t.Fatalf("scope %q was not rejected by the real scope constraint: %v", scope, rejected)
		}
	}
	checkRejectedScope(f.db, restricted.ID, "event:admin")
	groupRequireUnchanged(t, f.ctx, f.db, before)
	url := f.db.pool.Config().ConnString()
	f.db.Close()
	upgraded, err := Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	if after := groupDatabaseSnapshot(t, f.ctx, upgraded, 19); !reflect.DeepEqual(before, after) {
		t.Fatal("020 rewrote an old row, token/grant/default, bytes, policy, outbox or001..019 receipt/checksum")
	}
	for _, old := range []struct {
		hash  string
		grant APITokenGrant
	}{{hash, oldGrant}, {defaultHash, defaultGrant}} {
		if got, err := upgraded.AuthenticateAPIToken(f.ctx, old.hash); err != nil || !reflect.DeepEqual(got, old.grant) {
			t.Fatalf("020 changed an established grant: got=%+v want=%+v err=%v", got, old.grant, err)
		}
	}
	if user, csrf, err := upgraded.Session(f.ctx, "migration020-session"); err != nil || user.ID != f.owner.ID || user.Email != f.owner.Email || user.Name != f.owner.Name || csrf != "migration020-csrf" {
		t.Fatalf("020 changed the established session: user=%+v csrf=%q err=%v", user, csrf, err)
	}
	var usable bool
	var columns []string
	err = upgraded.pool.QueryRow(f.ctx, `SELECT i.indisvalid AND i.indisready AND NOT i.indisunique AND i.indpred IS NULL,
 ARRAY(SELECT a.attname::text FROM unnest(i.indkey) WITH ORDINALITY AS key(attnum,position)
 JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=key.attnum ORDER BY key.position)
 FROM pg_index i JOIN pg_class idx ON idx.oid=i.indexrelid
 WHERE i.indrelid='envelope_items'::regclass AND idx.relname='envelope_items_project_event'`).Scan(&usable, &columns)
	if err != nil || !usable || !reflect.DeepEqual(columns, []string{"project_id", "event_id"}) {
		t.Fatalf("020 cleanup index is not usable over project/event identity: usable=%v columns=%v err=%v", usable, columns, err)
	}
	body, err := migrations.ReadFile("migrations/020_event_admin_scope.sql")
	if err != nil {
		t.Fatal(err)
	}
	var checksum string
	if err = upgraded.pool.QueryRow(f.ctx, "SELECT checksum FROM schema_migrations WHERE version=20").Scan(&checksum); err != nil || checksum != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("020 receipt does not match the embedded migration: checksum=%s err=%v", checksum, err)
	}
	_, adminHash, adminPrefix := apiTokenCredentials("migration020-admin")
	admin, err := upgraded.CreateAPIToken(f.ctx, f.owner.ID, f.org.Slug, APITokenInput{Name: "Opt-in deletion", Scopes: []string{"event:admin"}, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}, ExpiresInDays: 1}, adminHash, adminPrefix)
	if err != nil || !reflect.DeepEqual(admin.Scopes, []string{"event:admin"}) {
		t.Fatalf("020 did not admit exactly the requested opt-in scope: token=%+v err=%v", admin, err)
	}
	adminGrant, err := upgraded.AuthenticateAPIToken(f.ctx, adminHash)
	if err != nil || !reflect.DeepEqual(adminGrant.Scopes, []string{"event:admin"}) || adminGrant.AllProjects || !reflect.DeepEqual(adminGrant.ProjectIDs, oldGrant.ProjectIDs) {
		t.Fatalf("new deletion grant widened its scope or project coverage: %+v err=%v", adminGrant, err)
	}
	checkRejectedScope(upgraded, admin.ID, "event:delete")
	checkpoint := groupDatabaseSnapshot(t, f.ctx, upgraded, 0)
	upgraded.Close()
	again, err := Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	groupRequireUnchanged(t, f.ctx, again, checkpoint)
	if got, err := again.AuthenticateAPIToken(f.ctx, adminHash); err != nil || !reflect.DeepEqual(got, adminGrant) {
		t.Fatalf("reopen lost or widened the explicit deletion grant: %+v err=%v", got, err)
	}
}
