//go:build integration

package store

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
)

func TestOrgWriteScopeMigration040PreservesTokensAndReopens(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	beforeToken, _, _ := apiTokenFixture(t, ctx, db, user, org.Slug, "before040", []int64{apiTokenProjectID(t, project.ID)})
	if _, err := db.pool.Exec(ctx, `ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
 ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK (
  cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases','event:read','event:write','event:admin','project:write']::text[]);
 DELETE FROM schema_migrations WHERE version=40`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]')::text FROM api_tokens t`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			reopened.Close()
			t.Fatal("040 changed existing API tokens")
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=40`).Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/040_org_write_scope.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatal("040 checksum receipt absent or changed", err)
		}
		reopened.Close()
	}
	secret, hash, prefix := apiTokenCredentials("org-write-after-040")
	token, err := db.CreateAPIToken(ctx, user.ID, user.AuthGeneration, org.Slug, APITokenInput{
		Name: "Organization writer", Scopes: []string{"org:read", "org:write"}, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}, ExpiresInDays: 1,
	}, hash, prefix)
	if err != nil || token.ID == "" || token.ID == beforeToken.ID {
		t.Fatalf("org:write issuance: %+v %v", token, err)
	}
	grant, err := db.AuthenticateAPIToken(ctx, hash)
	if err != nil || !reflect.DeepEqual(grant.Scopes, []string{"org:read", "org:write"}) {
		t.Fatalf("org:write grant: %+v %v secret=%t", grant, err, secret == "")
	}
}
