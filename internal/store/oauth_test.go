//go:build integration

package store

import (
	"strings"
	"testing"
	"time"
)

func TestCleanupOAuthSecretsPreservesActiveRows(t *testing.T) {
	f := resolutionStore(t)
	client := OAuthClient{ID: "gso_" + strings.Repeat("a", 32), Name: "Retention client", RedirectURIs: []string{"https://client.example.test/callback"}}
	if err := f.db.CreateOAuthClient(f.ctx, client); err != nil {
		t.Fatal(err)
	}
	makeCode := func(hash string, expires time.Time) {
		t.Helper()
		if err := f.db.CreateOAuthCode(f.ctx, OAuthCodeInput{CodeHash: hash, ClientID: client.ID, UserID: f.owner.ID, AuthGeneration: f.owner.AuthGeneration,
			OrganizationID: f.org.ID, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}, Scopes: []string{"org:read"}, RedirectURI: client.RedirectURIs[0],
			Challenge: strings.Repeat("b", 43), ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}
	makeCode(strings.Repeat("1", 64), time.Now().Add(-31*24*time.Hour))
	makeCode(strings.Repeat("2", 64), time.Now().Add(time.Hour))
	var accessID string
	if err := f.db.pool.QueryRow(f.ctx, `INSERT INTO api_tokens(organization_id,user_id,name,secret_hash,prefix,scopes,all_projects,expires_at)
 VALUES($1::bigint,$2::bigint,'OAuth retention',repeat('3',64),'gs_33333333',ARRAY['org:read'],true,now()+interval '1 day') RETURNING id::text`, f.org.ID, f.owner.ID).Scan(&accessID); err != nil {
		t.Fatal(err)
	}
	insertRefresh := func(hash string, expires time.Time, revoked bool) {
		t.Helper()
		var revokedAt any
		if revoked {
			revokedAt = time.Now().Add(-31 * 24 * time.Hour)
		}
		if _, err := f.db.pool.Exec(f.ctx, `INSERT INTO oauth_refresh_tokens(secret_hash,client_id,access_token_id,user_id,organization_id,project_ids,all_projects,scopes,auth_generation,expires_at,revoked_at)
 VALUES($1,$2,$3::bigint,$4::bigint,$5::bigint,ARRAY[]::bigint[],true,ARRAY['org:read'],$6,now()+interval '1 day',$7)`, hash, client.ID, accessID, f.owner.ID, f.org.ID, f.owner.AuthGeneration, revokedAt); err != nil {
			t.Fatal(err)
		}
	}
	insertRefresh(strings.Repeat("4", 64), time.Now().Add(time.Hour), true)
	insertRefresh(strings.Repeat("5", 64), time.Now().Add(time.Hour), false)
	removed, err := f.db.CleanupOAuthSecrets(f.ctx)
	if err != nil || removed != 2 {
		t.Fatalf("cleanup removed=%d err=%v", removed, err)
	}
	var codes, refresh int
	if err := f.db.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM oauth_authorization_codes),(SELECT count(*) FROM oauth_refresh_tokens)`).Scan(&codes, &refresh); err != nil {
		t.Fatal(err)
	}
	if codes != 1 || refresh != 1 {
		t.Fatalf("active secrets removed: codes=%d refresh=%d", codes, refresh)
	}
}
