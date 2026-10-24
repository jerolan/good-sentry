//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func adminBool(value bool) *bool { return &value }

func insertAdminTestUser(t *testing.T, db *Store, email, name string) User {
	t.Helper()
	var user User
	err := db.pool.QueryRow(t.Context(), `INSERT INTO users(email,password_hash,name)
		VALUES($1,$2,$3) RETURNING id::text,email,name,auth_generation`, email, []byte("admin-test-hash"), name).
		Scan(&user.ID, &user.Email, &user.Name, &user.AuthGeneration)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func waitForAdminLockWait(t *testing.T, db *Store, ctx context.Context, holderPID int, fragment string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err := db.pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE $2
		)`, holderPID, "%"+fragment+"%").Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			var activity string
			_ = db.pool.QueryRow(ctx, `SELECT COALESCE(string_agg(format('pid=%s wait=%s/%s blockers=%s query=%s',pid,wait_event_type,wait_event,pg_blocking_pids(pid),query), E'\n'),'')
				FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state='active'`).Scan(&activity)
			t.Fatalf("operation never waited on %q; active sessions:\n%s", fragment, activity)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForAdminLockWaiters(t *testing.T, db *Store, ctx context.Context, fragment string, minimum int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE $1`, "%"+fragment+"%").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= minimum {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d sessions waited on %q, want at least %d", waiting, fragment, minimum)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestInstanceUserAdminMigration064PreservesExistingAccountsAndCredentials(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	_, tokenHash, prefix := apiTokenCredentials("before064")
	if _, err := db.CreateAPIToken(ctx, owner.ID, owner.AuthGeneration, org.Slug, APITokenInput{
		Name: "Before064", Scopes: []string{"project:read"}, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}, ExpiresInDays: 7,
	}, tokenHash, prefix); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "before064-session", "before064-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var usersBefore string
	if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(id,email,name,password_hash,auth_generation) ORDER BY id),'[]'::jsonb)::text FROM users`).Scan(&usersBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `ALTER TABLE users DROP COLUMN is_instance_admin, DROP COLUMN is_active;
		DELETE FROM schema_migrations WHERE version=64`); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	var usersAfter string
	if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(id,email,name,password_hash,auth_generation) ORDER BY id),'[]'::jsonb)::text FROM users`).Scan(&usersAfter); err != nil || usersAfter != usersBefore {
		t.Fatalf("064 rewrote existing account data: before=%s after=%s err=%v", usersBefore, usersAfter, err)
	}
	var count int
	if err := reopened.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_active AND NOT is_instance_admin`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("064 did not preserve one active non-admin owner: count=%d err=%v", count, err)
	}
	if session, csrf, err := reopened.Session(ctx, "before064-session"); err != nil || session.ID != owner.ID || csrf != "before064-csrf" {
		t.Fatalf("064 changed existing session: user=%+v csrf=%q err=%v", session, csrf, err)
	}
	if grant, err := reopened.AuthenticateAPIToken(ctx, tokenHash); err != nil || grant.User.ID != owner.ID {
		t.Fatalf("064 changed existing API token: grant=%+v err=%v", grant, err)
	}
	var checksum string
	if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=64`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.ReadFile("migrations/064_instance_user_admin.sql")
	if err != nil || checksum != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("064 checksum mismatch: got=%q err=%v", checksum, err)
	}
}

func TestInstanceUserAdminBootstrapAndGrant(t *testing.T) {
	ctx, db, owner, _, _ := telemetryStore(t)
	if !owner.IsInstanceAdmin {
		t.Fatal("first setup owner was not an instance administrator")
	}
	if granted, err := db.GrantInstanceAdmin(ctx, owner.Email); err != nil || granted {
		t.Fatalf("existing administrator grant was not idempotent: granted=%t err=%v", granted, err)
	}
	second := insertAdminTestUser(t, db, "existing-admin@example.test", "Existing account")
	if granted, err := db.GrantInstanceAdmin(ctx, second.Email); err != nil || !granted {
		t.Fatalf("first legacy grant failed: granted=%t err=%v", granted, err)
	}
	if granted, err := db.GrantInstanceAdmin(ctx, second.Email); err != nil || granted {
		t.Fatalf("repeat grant was not a no-op: granted=%t err=%v", granted, err)
	}
	if _, err := db.GrantInstanceAdmin(ctx, "missing-admin@example.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant created a missing account: %v", err)
	}
	third := insertAdminTestUser(t, db, "inactive-admin@example.test", "Inactive account")
	if _, err := db.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1::bigint`, third.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GrantInstanceAdmin(ctx, third.Email); !errors.Is(err, ErrForbidden) {
		t.Fatalf("grant reactivated or promoted an inactive account: %v", err)
	}
	implicit, _, err := db.EnsureDefaultOwner(ctx, "new-implicit@example.test", []byte("implicit-hash"), "Implicit", "New implicit organization")
	if err != nil || implicit.IsInstanceAdmin {
		t.Fatalf("a later implicit owner was promoted: user=%+v err=%v", implicit, err)
	}
	if _, _, err := db.EnsureDefaultOwner(ctx, third.Email, []byte("replacement-hash"), "Inactive", "Inactive organization"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("open mode accepted an inactive configured owner: %v", err)
	}
}

func TestInstanceUserAdminSearchSuspensionAndLastAdmin(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	member := insertAdminTestUser(t, db, "member-account-admin@example.test", "Member account")
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'admin')`, org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdminUsers(ctx, member.ID, "", 1, 25); !errors.Is(err, ErrForbidden) {
		t.Fatalf("organization admin received global access: %v", err)
	}
	page, err := db.AdminUsers(ctx, owner.ID, "MEMBER-ACCOUNT", 1, 1)
	if err != nil || page.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != member.ID || page.Data[0].IsInstanceAdmin || !page.Data[0].IsActive || page.Data[0].OrganizationCount != 1 {
		t.Fatalf("filtered users page=%+v err=%v", page, err)
	}
	if next, err := db.AdminUsers(ctx, owner.ID, "", 2, 1); err != nil || len(next.Data) != 1 || next.Total != 2 {
		t.Fatalf("bounded second page=%+v err=%v", next, err)
	}

	if err := db.CreateSession(ctx, member.ID, member.AuthGeneration, "admin-disabled-session", "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	accountSecret := strings.Repeat("a", 64)
	accountHash := fmt.Sprintf("%x", sha256.Sum256([]byte(accountSecret)))
	if _, err := db.CreateUserAPIToken(ctx, member.ID, member.AuthGeneration, "account", UserTokenMask([]string{"org:read"}), accountHash, []byte("sealed-account-token")); err != nil {
		t.Fatal(err)
	}
	var apiTokenID string
	orgSecret := strings.Repeat("b", 64)
	orgHash := fmt.Sprintf("%x", sha256.Sum256([]byte(orgSecret)))
	if err := db.pool.QueryRow(ctx, `INSERT INTO api_tokens(organization_id,user_id,name,secret_hash,prefix,scopes,all_projects,expires_at)
		VALUES($1::bigint,$2::bigint,'Member token',$3,'gs_bbbbbbbb',ARRAY['org:read'],true,now()+interval '1 day') RETURNING id::text`, org.ID, member.ID, orgHash).Scan(&apiTokenID); err != nil {
		t.Fatal(err)
	}
	client := OAuthClient{ID: "gso_" + strings.Repeat("c", 32), Name: "Suspension test", RedirectURIs: []string{"https://client.example.test/callback"}}
	if err := db.CreateOAuthClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO oauth_refresh_tokens(secret_hash,client_id,access_token_id,user_id,organization_id,project_ids,all_projects,scopes,auth_generation,expires_at)
		VALUES($1,$2,$3::bigint,$4::bigint,$5::bigint,ARRAY[]::bigint[],true,ARRAY['org:read'],$6,now()+interval '1 day')`, strings.Repeat("d", 64), client.ID, apiTokenID, member.ID, org.ID, member.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO login_challenges(token_hash,user_id,auth_generation,expires_at) VALUES($1,$2::bigint,$3,now()+interval '5 minutes')`, strings.Repeat("e", 64), member.ID, member.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateAdminUser(ctx, owner.ID, member.ID, "", AdminUserChange{IsActive: adminBool(false)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Session(ctx, "admin-disabled-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended session remained usable: %v", err)
	}
	if _, err := db.UserByEmail(ctx, member.Email); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended password identity remained usable: %v", err)
	}
	if _, err := db.AuthenticateAPIToken(ctx, orgHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended organization token remained usable: %v", err)
	}
	if _, err := db.AuthenticateUserAPIToken(ctx, accountHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended account token remained usable: %v", err)
	}
	var challenges, accountTokens, revokedRefresh int
	var generation int64
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM login_challenges WHERE user_id=$1::bigint),
		(SELECT count(*) FROM user_api_tokens WHERE user_id=$1::bigint),
		(SELECT count(*) FROM oauth_refresh_tokens WHERE user_id=$1::bigint AND revoked_at IS NOT NULL),
		(SELECT auth_generation FROM users WHERE id=$1::bigint)`, member.ID).Scan(&challenges, &accountTokens, &revokedRefresh, &generation); err != nil {
		t.Fatal(err)
	}
	if challenges != 0 || accountTokens != 0 || revokedRefresh != 1 || generation != member.AuthGeneration+1 {
		t.Fatalf("suspension incomplete: challenges=%d accountTokens=%d revokedRefresh=%d generation=%d", challenges, accountTokens, revokedRefresh, generation)
	}
	if _, err := db.UpdateAdminUser(ctx, owner.ID, member.ID, "", AdminUserChange{IsActive: adminBool(true)}); err != nil {
		t.Fatal(err)
	}
	page, err = db.AdminUsers(ctx, owner.ID, member.Email, 1, 25)
	if err != nil || len(page.Data) != 1 || !page.Data[0].IsActive || page.Data[0].IsInstanceAdmin {
		t.Fatalf("reactivation restored admin state: page=%+v err=%v", page, err)
	}
	if _, _, err := db.Session(ctx, "admin-disabled-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reactivation revived a session: %v", err)
	}
	if _, err := db.AuthenticateAPIToken(ctx, orgHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reactivation revived an organization token: %v", err)
	}
	if _, err := db.AuthenticateUserAPIToken(ctx, accountHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reactivation revived an account token: %v", err)
	}
	if _, err := db.UpdateAdminUser(ctx, owner.ID, owner.ID, "", AdminUserChange{IsInstanceAdmin: adminBool(false)}); !errors.Is(err, ErrLastInstanceAdmin) {
		t.Fatalf("last active administrator was demoted: %v", err)
	}
	var ownerAdmin bool
	if err := db.pool.QueryRow(ctx, `SELECT is_instance_admin FROM users WHERE id=$1::bigint`, owner.ID).Scan(&ownerAdmin); err != nil || !ownerAdmin {
		t.Fatalf("rejected last-admin mutation changed owner state=%t err=%v", ownerAdmin, err)
	}
}

func TestInstanceAdminRechecksActorAfterWaitingForSerializationLock(t *testing.T) {
	ctx, db, owner, _, _ := telemetryStore(t)
	target := insertAdminTestUser(t, db, "pending-admin-change@example.test", "Pending target")
	held, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if _, err := held.Exec(ctx, `SELECT pg_advisory_xact_lock(73042191)`); err != nil {
		t.Fatal(err)
	}
	var holder int
	if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if _, err := held.Exec(ctx, `UPDATE users SET is_instance_admin=false WHERE id=$1::bigint`, owner.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := db.UpdateAdminUser(ctx, owner.ID, target.ID, "", AdminUserChange{IsActive: adminBool(false)})
		result <- err
	}()
	waitForAdminLockWait(t, db, ctx, holder, "pg_advisory_xact_lock(73042191)")
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrForbidden) {
		t.Fatalf("stale administrator acted after waiting: %v", err)
	}
	var targetActive bool
	if err := db.pool.QueryRow(ctx, `SELECT is_active FROM users WHERE id=$1::bigint`, target.ID).Scan(&targetActive); err != nil || !targetActive {
		t.Fatalf("stale administrator changed target active=%t err=%v", targetActive, err)
	}
}

func TestSuspensionWaitsForCredentialIssuerAndRevokesItsCommit(t *testing.T) {
	ctx, db, owner, _, _ := telemetryStore(t)
	member := insertAdminTestUser(t, db, "credential-race@example.test", "Credential race")
	held, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if err = pinUserGeneration(ctx, held, member.ID, member.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	var holder int
	if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	update := make(chan error, 1)
	go func() {
		_, err := db.UpdateAdminUser(ctx, owner.ID, member.ID, "", AdminUserChange{IsActive: adminBool(false)})
		update <- err
	}()
	waitForAdminLockWait(t, db, ctx, holder, "FOR UPDATE OF u")
	secret := strings.Repeat("f", 64)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(secret)))
	if _, err = db.CreateUserAPIToken(ctx, member.ID, member.AuthGeneration, "Racing issuer", UserTokenMask([]string{"org:read"}), hash, []byte("sealed-racing-token")); err != nil {
		t.Fatalf("issuer holding a compatible SHARE lock failed: %v", err)
	}
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-update; err != nil {
		t.Fatal(err)
	}
	if _, err := db.AuthenticateUserAPIToken(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("credential committed before suspension survived revocation: %v", err)
	}
	if err := db.CreateSession(ctx, member.ID, member.AuthGeneration, "late-racing-session", "csrf", time.Now().Add(time.Hour)); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("post-suspension credential issuance error=%v", err)
	}
}

func TestOAuthRefreshRotationLocksUserBeforeRefreshToken(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	member := insertAdminTestUser(t, db, "oauth-rotation-admin-race@example.test", "OAuth rotation race")
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'member')`, org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	client := OAuthClient{ID: "gso_" + strings.Repeat("c", 32), Name: "Rotation order", RedirectURIs: []string{"https://client.example.test/callback"}}
	if err := db.CreateOAuthClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	oldAccessHash := strings.Repeat("a", 64)
	oldRefreshHash := strings.Repeat("b", 64)
	var accessID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO api_tokens(organization_id,user_id,name,secret_hash,prefix,scopes,all_projects,expires_at)
		VALUES($1::bigint,$2::bigint,'OAuth rotation',$3,'gs_aaaaaaaa',ARRAY['org:read'],true,now()+interval '1 hour') RETURNING id::text`, org.ID, member.ID, oldAccessHash).Scan(&accessID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO oauth_refresh_tokens(secret_hash,client_id,access_token_id,user_id,organization_id,project_ids,all_projects,scopes,auth_generation,expires_at)
		VALUES($1,$2,$3::bigint,$4::bigint,$5::bigint,ARRAY[]::bigint[],true,ARRAY['org:read'],$6,now()+interval '1 hour')`, oldRefreshHash, client.ID, accessID, member.ID, org.ID, member.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	held, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if err = held.QueryRow(ctx, `SELECT id FROM users WHERE id=$1::bigint FOR UPDATE`, member.ID).Scan(new(string)); err != nil {
		t.Fatal(err)
	}
	var holder int
	if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	type rotationResult struct {
		issued OAuthIssued
		err    error
	}
	rotated := make(chan rotationResult, 1)
	newAccessHash := strings.Repeat("d", 64)
	newRefreshHash := strings.Repeat("e", 64)
	go func() {
		issued, err := db.RotateOAuthRefresh(ctx, OAuthRotationInput{
			RefreshSecretHash: oldRefreshHash, ClientID: client.ID, AccessSecretHash: newAccessHash,
			NewRefreshSecretHash: newRefreshHash, Prefix: "gs_dddddddd",
			AccessExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour),
		})
		rotated <- rotationResult{issued: issued, err: err}
	}()
	waitForAdminLockWait(t, db, ctx, holder, "FOR SHARE")
	probe, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var refreshID string
	if err := probe.QueryRow(ctx, `SELECT secret_hash FROM oauth_refresh_tokens WHERE secret_hash=$1 AND client_id=$2 FOR UPDATE NOWAIT`, oldRefreshHash, client.ID).Scan(&refreshID); err != nil {
		probe.Rollback(ctx)
		t.Fatalf("refresh rotation locked the credential before waiting for the account: %v", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	suspension := make(chan error, 1)
	go func() {
		_, err := db.UpdateAdminUser(ctx, owner.ID, member.ID, "", AdminUserChange{IsActive: adminBool(false)})
		suspension <- err
	}()
	waitForAdminLockWaiters(t, db, ctx, "FOR UPDATE OF u", 1)
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	rotation := <-rotated
	if rotation.err != nil && !errors.Is(rotation.err, ErrOAuthGrant) {
		t.Fatalf("refresh rotation failed with an unexpected error: %v", rotation.err)
	}
	if err := <-suspension; err != nil {
		t.Fatalf("account suspension failed during refresh rotation: %v", err)
	}
	var usableRefreshes, usableAccessTokens int
	if err := db.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM oauth_refresh_tokens WHERE user_id=$1::bigint AND revoked_at IS NULL),
		(SELECT count(*) FROM api_tokens WHERE user_id=$1::bigint AND revoked_at IS NULL)`, member.ID).Scan(&usableRefreshes, &usableAccessTokens); err != nil {
		t.Fatal(err)
	}
	if usableRefreshes != 0 || usableAccessTokens != 0 {
		t.Fatalf("suspension left OAuth credentials usable: refresh=%d access=%d rotationErr=%v", usableRefreshes, usableAccessTokens, rotation.err)
	}
	for _, hash := range []string{oldAccessHash, newAccessHash} {
		if _, err := db.AuthenticateAPIToken(ctx, hash); !errors.Is(err, ErrNotFound) {
			t.Fatalf("suspended OAuth access token %q remained usable: %v", hash[:8], err)
		}
	}
}

func TestOIDCAutoCreateSerializesWithFirstOwnerBootstrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("goodsentry_admin_bootstrap_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	databaseURL, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := databaseURL.Query()
	query.Set("search_path", schema)
	databaseURL.RawQuery = query.Encode()
	db, err := Open(ctx, databaseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(context.Background())
	})
	held, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if _, err := held.Exec(ctx, `SELECT pg_advisory_xact_lock(73042190)`); err != nil {
		t.Fatal(err)
	}
	var holder int
	if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	type bootstrapResult struct {
		user User
		err  error
	}
	bootstrapped := make(chan bootstrapResult, 1)
	go func() {
		user, _, err := db.Bootstrap(ctx, "first-owner@example.test", []byte("hash"), "First owner", "Bootstrap org")
		bootstrapped <- bootstrapResult{user: user, err: err}
	}()
	waitForAdminLockWaiters(t, db, ctx, "pg_advisory_xact_lock(73042190)", 1)
	type oidcResult struct {
		user User
		err  error
	}
	created := make(chan oidcResult, 1)
	go func() {
		user, err := db.LoginOIDC(ctx, OIDCIdentity{Provider: "https://id.example.test", Subject: "bootstrap-race", Email: "oidc-account@example.test", Name: "OIDC account", Password: []byte("oidc-only"), AutoCreate: true})
		created <- oidcResult{user: user, err: err}
	}()
	waitForAdminLockWaiters(t, db, ctx, "pg_advisory_xact_lock(73042190)", 2)
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	firstOwner := <-bootstrapped
	if firstOwner.err != nil || !firstOwner.user.IsInstanceAdmin {
		t.Fatalf("empty database bootstrap lost first-owner status: user=%+v err=%v", firstOwner.user, firstOwner.err)
	}
	oidcUser := <-created
	if oidcUser.err != nil || oidcUser.user.IsInstanceAdmin {
		t.Fatalf("OIDC account created after bootstrap gained first-owner status: user=%+v err=%v", oidcUser.user, oidcUser.err)
	}
}

func TestInvitationSignupWaitsForCreationLockBeforeOrganizationLock(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	_, tokenHash := sentInvitation(t, ctx, db, owner.ID, org.Slug, "signup-lock@example.test", "member", 73042190)
	held, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if _, err := held.Exec(ctx, `SELECT pg_advisory_xact_lock(73042190)`); err != nil {
		t.Fatal(err)
	}
	var holder int
	if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	type acceptResult struct {
		user User
		err  error
	}
	accepted := make(chan acceptResult, 1)
	go func() {
		user, _, err := db.AcceptInvitation(ctx, tokenHash, "", 0, "New invitee", []byte("password-hash"))
		accepted <- acceptResult{user: user, err: err}
	}()
	waitForAdminLockWait(t, db, ctx, holder, "pg_advisory_xact_lock(73042190)")
	probe, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var lockedOrganization string
	if err := probe.QueryRow(ctx, `SELECT id::text FROM organizations WHERE id=$1::bigint FOR UPDATE NOWAIT`, org.ID).Scan(&lockedOrganization); err != nil {
		probe.Rollback(ctx)
		t.Fatalf("invitation signup locked the organization before waiting for the creation lock: %v", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-accepted
	if result.err != nil || result.user.Email != "signup-lock@example.test" {
		t.Fatalf("serialized invitation signup=%+v err=%v", result.user, result.err)
	}
}

func TestExistingUserInvitationLocksAccountBeforeOrganization(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	member := insertAdminTestUser(t, db, "existing-invitation-lock@example.test", "Existing invitee")
	_, tokenHash := sentInvitation(t, ctx, db, owner.ID, org.Slug, member.Email, "member", 73042191)
	held, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if err = held.QueryRow(ctx, `SELECT id FROM users WHERE id=$1::bigint FOR UPDATE`, member.ID).Scan(new(string)); err != nil {
		t.Fatal(err)
	}
	var holder int
	if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	type acceptResult struct {
		user User
		err  error
	}
	accepted := make(chan acceptResult, 1)
	go func() {
		user, _, err := db.AcceptInvitation(ctx, tokenHash, member.ID, member.AuthGeneration, "", nil)
		accepted <- acceptResult{user: user, err: err}
	}()
	waitForAdminLockWait(t, db, ctx, holder, "FOR SHARE")
	probe, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var lockedOrganization string
	if err := probe.QueryRow(ctx, `SELECT id::text FROM organizations WHERE id=$1::bigint FOR UPDATE NOWAIT`, org.ID).Scan(&lockedOrganization); err != nil {
		probe.Rollback(ctx)
		t.Fatalf("existing-user invitation locked the organization before the account: %v", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-accepted
	if result.err != nil || result.user.ID != member.ID {
		t.Fatalf("existing-user invitation=%+v err=%v", result.user, result.err)
	}
}

func TestNewSignupInvitationDuplicateCheckDoesNotWaitOnLockedAccount(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	member := insertAdminTestUser(t, db, "duplicate-invitation-lock@example.test", "Existing account")
	_, tokenHash := sentInvitation(t, ctx, db, owner.ID, org.Slug, member.Email, "member", 73042190)
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'member')`, org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	held, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if err = held.QueryRow(ctx, `SELECT id FROM users WHERE id=$1::bigint FOR UPDATE`, member.ID).Scan(new(string)); err != nil {
		t.Fatal(err)
	}
	type acceptResult struct {
		user User
		err  error
	}
	accepted := make(chan acceptResult, 1)
	go func() {
		user, _, err := db.AcceptInvitation(ctx, tokenHash, "", 0, "Duplicate signup", []byte("password-hash"))
		accepted <- acceptResult{user: user, err: err}
	}()
	select {
	case result := <-accepted:
		if !errors.Is(result.err, ErrInvalidInvitation) {
			t.Fatalf("duplicate-account signup=%+v err=%v, want invalid invitation", result.user, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("duplicate-account check waited on the user lock held before organization deletion")
	}
}
