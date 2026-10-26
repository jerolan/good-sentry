//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthGenerationMigration035PreservesCredentialsAndReopens(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	if owner.AuthGeneration != 1 {
		t.Fatalf("bootstrap generation=%d want=1", owner.AuthGeneration)
	}
	secret, hash, prefix := apiTokenCredentials("generation-migration-token")
	token, err := db.CreateAPIToken(ctx, owner.ID, owner.AuthGeneration, org.Slug, APITokenInput{
		Name: "Before035", Scopes: []string{"project:read"}, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}, ExpiresInDays: 30,
	}, hash, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "migration035-session", "migration035-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Reconstruct only the pre035 users shape in this disposable schema. Sessions,
	// tokens, grants and prior receipts deliberately remain in place.
	if _, err := db.pool.Exec(ctx, `ALTER TABLE users DROP COLUMN auth_generation;
 DELETE FROM schema_migrations WHERE version=35`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 34)
	databaseURL := db.pool.Config().ConnString()
	for i := range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(reopened.Close)
		after := groupDatabaseSnapshot(t, ctx, reopened, 34)
		var oldUsers string
		if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(payload ORDER BY payload::text),'[]'::jsonb)::text
 FROM (SELECT to_jsonb(u)-'auth_generation' AS payload FROM users u) history`).Scan(&oldUsers); err != nil {
			t.Fatal(err)
		}
		after["users"] = oldUsers
		if !reflect.DeepEqual(before, after) {
			t.Fatal("035 rewrote an account, credential, grant or earlier migration receipt")
		}
		var generations, users int
		if err := reopened.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE auth_generation<>1),count(*) FROM users`).Scan(&generations, &users); err != nil || generations != 0 || users != 1 {
			t.Fatalf("035 generation defaults wrong=%d users=%d err=%v", generations, users, err)
		}
		admitted, csrf, err := reopened.Session(ctx, "migration035-session")
		if err != nil || admitted.ID != owner.ID || admitted.AuthGeneration != 1 || csrf != "migration035-csrf" {
			t.Fatalf("existing session changed through035: user=%+v csrf=%q err=%v", admitted, csrf, err)
		}
		grant, err := reopened.AuthenticateAPIToken(ctx, hash)
		if err != nil || grant.User.ID != owner.ID || grant.User.AuthGeneration != 1 || len(grant.ProjectIDs) != 1 {
			t.Fatalf("existing token/grant changed through035: %+v err=%v", grant, err)
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=35`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/035_auth_generation.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatalf("035 checksum receipt differs: %q err=%v", receipt, err)
		}
		if i == 0 {
			reopened.Close()
		}
	}
	if token.ID == "" || token.Prefix != secret[:11] {
		t.Fatalf("pre035 token metadata changed: %+v", token)
	}
}

func TestCredentialIssuanceRejectsStaleGenerationAtomically(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	stale := owner.AuthGeneration
	_, err := db.pool.Exec(ctx, `UPDATE users SET auth_generation=auth_generation+1 WHERE id=$1::bigint`, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeSessions, beforeTokens := credentialCounts(t, ctx, db, owner.ID)
	if err := db.CreateSession(ctx, owner.ID, stale, "stale-session", "stale-csrf", time.Now().Add(time.Hour)); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("stale session issuance error=%v", err)
	}
	_, hash, prefix := apiTokenCredentials("stale-generation-token")
	if _, err := db.CreateAPIToken(ctx, owner.ID, stale, org.Slug, APITokenInput{Name: "Stale", Scopes: []string{"project:read"}, ExpiresInDays: 1}, hash, prefix); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("stale token issuance error=%v", err)
	}
	if _, err := db.CreateAPIToken(ctx, owner.ID, 0, org.Slug, APITokenInput{Name: "Missing", Scopes: []string{"project:read"}, ExpiresInDays: 1}, hash, prefix); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("missing-generation token issuance error=%v", err)
	}
	afterSessions, afterTokens := credentialCounts(t, ctx, db, owner.ID)
	if beforeSessions != afterSessions || beforeTokens != afterTokens {
		t.Fatalf("stale issuance wrote credentials: sessions %d->%d tokens %d->%d", beforeSessions, afterSessions, beforeTokens, afterTokens)
	}
	current, err := db.UserByEmail(ctx, owner.Email)
	if err != nil || current.AuthGeneration != stale+1 {
		t.Fatalf("reauthentication generation=%+v err=%v", current, err)
	}
	if err := db.CreateSession(ctx, current.ID, current.AuthGeneration, "fresh-session", "fresh-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	admitted, csrf, err := db.Session(ctx, "fresh-session")
	if err != nil || admitted.AuthGeneration != current.AuthGeneration || csrf != "fresh-csrf" {
		t.Fatalf("fresh session admission=%+v csrf=%q err=%v", admitted, csrf, err)
	}
	if _, err := db.CreateAPIToken(ctx, current.ID, current.AuthGeneration, org.Slug, APITokenInput{Name: "Fresh", Scopes: []string{"project:read"}, ExpiresInDays: 1}, hash, prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AuthenticateAPIToken(ctx, hash); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
}

func credentialCounts(t *testing.T, ctx context.Context, db *Store, userID string) (int, int) {
	t.Helper()
	var sessions, tokens int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE user_id=$1::bigint),
 (SELECT count(*) FROM api_tokens WHERE user_id=$1::bigint)`, userID).Scan(&sessions, &tokens); err != nil {
		t.Fatal(err)
	}
	return sessions, tokens
}

func TestPasswordChangeRevokesEveryOwnedCredentialAtomically(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	var secondOrg Organization
	var member User
	var secondProject Project
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('second-account','Second account') RETURNING id::text`).Scan(&secondOrg.ID); err != nil {
		t.Fatal(err)
	}
	secondOrg.Slug = "second-account"
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, secondOrg.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateProject(ctx, owner.ID, secondOrg.Slug, "Second project", "go")
	if err != nil {
		t.Fatal(err)
	}
	secondProject.ID = created.ID
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('member-account@example.test',$1,'Member') RETURNING id::text,auth_generation`, []byte("member-hash")).Scan(&member.ID, &member.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'admin')`, org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "owner-session-1", "owner-csrf-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "owner-session-2", "owner-csrf-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, member.ID, member.AuthGeneration, "member-session", "member-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ownedToken := func(seed, orgSlug, userID string, generation int64) (APIToken, string) {
		t.Helper()
		_, hash, prefix := apiTokenCredentials(seed)
		token, err := db.CreateAPIToken(ctx, userID, generation, orgSlug, APITokenInput{Name: seed, Scopes: []string{"project:read"}, ExpiresInDays: 30}, hash, prefix)
		if err != nil {
			t.Fatal(err)
		}
		return token, hash
	}
	_, firstHash := ownedToken("owner-first", org.Slug, owner.ID, owner.AuthGeneration)
	_, secondHash := ownedToken("owner-second", secondOrg.Slug, owner.ID, owner.AuthGeneration)
	memberToken, memberHash := ownedToken("member-owned", org.Slug, member.ID, member.AuthGeneration)
	priorRevoked, priorHash := ownedToken("owner-prior-revoked", org.Slug, owner.ID, owner.AuthGeneration)
	personalTokenHash := fmt.Sprintf("%064x", 91001)
	if _, err := db.CreateUserAPIToken(ctx, owner.ID, owner.AuthGeneration, "Owner personal token", UserTokenMask([]string{"org:read"}), personalTokenHash, []byte("sealed-owner-personal-token")); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeAPIToken(ctx, owner.ID, org.Slug, priorRevoked.ID); err != nil {
		t.Fatal(err)
	}
	var priorRevokedAt time.Time
	if err := db.pool.QueryRow(ctx, `SELECT revoked_at FROM api_tokens WHERE id=$1::bigint`, priorRevoked.ID).Scan(&priorRevokedAt); err != nil {
		t.Fatal(err)
	}
	var keysBefore string
	if err := db.pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(public_key,project_id) ORDER BY public_key)::text FROM project_keys`).Scan(&keysBefore); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("replacement-password-hash")
	if err := db.ChangePassword(ctx, owner.ID, owner.AuthGeneration, replacement); err != nil {
		t.Fatal(err)
	}
	current, err := db.UserByID(ctx, owner.ID)
	if err != nil || current.AuthGeneration != owner.AuthGeneration+1 || !reflect.DeepEqual(current.PasswordHash, replacement) {
		t.Fatalf("password generation/hash not replaced: %+v err=%v", current, err)
	}
	var ownerSessions, memberSessions, ownedActive, ownedTotal, memberActive int
	if err := db.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM sessions WHERE user_id=$1::bigint),
 (SELECT count(*) FROM sessions WHERE user_id=$2::bigint),
 (SELECT count(*) FROM api_tokens WHERE user_id=$1::bigint AND revoked_at IS NULL),
 (SELECT count(*) FROM api_tokens WHERE user_id=$1::bigint),
 (SELECT count(*) FROM api_tokens WHERE user_id=$2::bigint AND revoked_at IS NULL)`, owner.ID, member.ID).
		Scan(&ownerSessions, &memberSessions, &ownedActive, &ownedTotal, &memberActive); err != nil {
		t.Fatal(err)
	}
	if ownerSessions != 0 || memberSessions != 1 || ownedActive != 0 || ownedTotal != 3 || memberActive != 1 {
		t.Fatalf("credential revocation scope: owner sessions=%d member sessions=%d owner active=%d total=%d member active=%d", ownerSessions, memberSessions, ownedActive, ownedTotal, memberActive)
	}
	var unchangedRevokedAt time.Time
	if err := db.pool.QueryRow(ctx, `SELECT revoked_at FROM api_tokens WHERE id=$1::bigint`, priorRevoked.ID).Scan(&unchangedRevokedAt); err != nil || !unchangedRevokedAt.Equal(priorRevokedAt) {
		t.Fatalf("prior revocation timestamp changed: before=%v after=%v err=%v", priorRevokedAt, unchangedRevokedAt, err)
	}
	for _, hash := range []string{firstHash, secondHash, priorHash} {
		if _, err := db.AuthenticateAPIToken(ctx, hash); !errors.Is(err, ErrNotFound) {
			t.Fatalf("owned token survived password change: %v", err)
		}
	}
	if _, err := db.AuthenticateUserAPIToken(ctx, personalTokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("personal token survived password change: %v", err)
	}
	if grant, err := db.AuthenticateAPIToken(ctx, memberHash); err != nil || grant.User.ID != member.ID {
		t.Fatalf("unrelated member credential changed: %+v err=%v", grant, err)
	}
	if _, csrf, err := db.Session(ctx, "member-session"); err != nil || csrf != "member-csrf" {
		t.Fatalf("unrelated member session changed: csrf=%q err=%v", csrf, err)
	}
	var keysAfter string
	if err := db.pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(public_key,project_id) ORDER BY public_key)::text FROM project_keys`).Scan(&keysAfter); err != nil || keysBefore != keysAfter {
		t.Fatalf("project SDK keys changed: before=%s after=%s err=%v", keysBefore, keysAfter, err)
	}
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "stale-after-change", "stale-csrf", time.Now().Add(time.Hour)); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("pre-change generation created a session: %v", err)
	}
	if err := db.CreateSession(ctx, owner.ID, current.AuthGeneration, "fresh-after-change", "fresh-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Session(ctx, "fresh-after-change"); err != nil {
		t.Fatalf("new-generation session rejected: %v", err)
	}
	if memberToken.ID == "" || secondProject.ID == "" || project.ID == "" {
		t.Fatal("fixture identity unexpectedly empty")
	}
}

func TestConcurrentPasswordChangesHaveOneWinner(t *testing.T) {
	ctx, db, owner, _, _ := telemetryStore(t)
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "concurrent-session", "concurrent-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	hashes := [][]byte{[]byte("winner-one"), []byte("winner-two")}
	start := make(chan struct{})
	results := make(chan error, len(hashes))
	var group sync.WaitGroup
	for _, hash := range hashes {
		group.Go(func() {
			<-start
			results <- db.ChangePassword(ctx, owner.ID, owner.AuthGeneration, hash)
		})
	}
	close(start)
	group.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrCredentialsChanged):
			stale++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("concurrent changes success=%d stale=%d", success, stale)
	}
	current, err := db.UserByID(ctx, owner.ID)
	if err != nil || current.AuthGeneration != owner.AuthGeneration+1 || (!reflect.DeepEqual(current.PasswordHash, hashes[0]) && !reflect.DeepEqual(current.PasswordHash, hashes[1])) {
		t.Fatalf("concurrent winner state: %+v err=%v", current, err)
	}
	if n, _ := credentialCounts(t, ctx, db, owner.ID); n != 0 {
		t.Fatalf("password change retained %d sessions", n)
	}
}

func TestPasswordChangeGenerationOverflowRollsBackCredentials(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	const maximum = int64(9223372036854775807)
	if _, err := db.pool.Exec(ctx, `UPDATE users SET auth_generation=$2 WHERE id=$1::bigint`, owner.ID, maximum); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, owner.ID, maximum, "overflow-session", "overflow-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, hash, prefix := apiTokenCredentials("overflow-token")
	if _, err := db.CreateAPIToken(ctx, owner.ID, maximum, org.Slug, APITokenInput{Name: "Overflow", Scopes: []string{"project:read"}, ExpiresInDays: 1}, hash, prefix); err != nil {
		t.Fatal(err)
	}
	before, err := db.UserByID(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ChangePassword(ctx, owner.ID, maximum, []byte("overflow-replacement")); err == nil {
		t.Fatal("exhausted generation unexpectedly changed the password")
	}
	after, err := db.UserByID(ctx, owner.ID)
	if err != nil || after.AuthGeneration != maximum || !reflect.DeepEqual(after.PasswordHash, before.PasswordHash) {
		t.Fatalf("overflow changed account state: before=%+v after=%+v err=%v", before, after, err)
	}
	if _, _, err := db.Session(ctx, "overflow-session"); err != nil {
		t.Fatalf("overflow deleted the existing session: %v", err)
	}
	if _, err := db.AuthenticateAPIToken(ctx, hash); err != nil {
		t.Fatalf("overflow revoked the existing token: %v", err)
	}
}

func TestCredentialIssuanceWaitsAndRechecksUnderRepeatableReadDefault(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	config, err := pgxpool.ParseConfig(db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	issuer := &Store{pool: pool}

	for _, tc := range []struct {
		name  string
		issue func(context.Context, int64) error
		count func(context.Context) int
	}{
		{name: "session",
			issue: func(ctx context.Context, generation int64) error {
				return issuer.CreateSession(ctx, owner.ID, generation, "generation-session", "generation-csrf", time.Now().Add(time.Hour))
			},
			count: func(ctx context.Context) int {
				var n int
				if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_hash='generation-session'`).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}},
		{name: "api token",
			issue: func(ctx context.Context, generation int64) error {
				_, hash, prefix := apiTokenCredentials("generation-api-token")
				_, err := issuer.CreateAPIToken(ctx, owner.ID, generation, org.Slug, APITokenInput{Name: "Generation", Scopes: []string{"project:read"}, ExpiresInDays: 1}, hash, prefix)
				return err
			},
			count: func(ctx context.Context) int {
				_, hash, _ := apiTokenCredentials("generation-api-token")
				var n int
				if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM api_tokens WHERE secret_hash=$1`, hash).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var current int64
			if err := db.pool.QueryRow(ctx, `SELECT auth_generation FROM users WHERE id=$1::bigint`, owner.ID).Scan(&current); err != nil {
				t.Fatal(err)
			}
			held, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer held.Rollback(ctx)
			var newGeneration int64
			var holder int
			if err := held.QueryRow(ctx, `UPDATE users SET auth_generation=auth_generation+1 WHERE id=$1::bigint
 RETURNING auth_generation,pg_backend_pid()`, owner.ID).Scan(&newGeneration, &holder); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { result <- tc.issue(ctx, current) }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				var blocked bool
				if err := db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM users%FOR SHARE%')`, holder).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("credential issuer never waited for the user lock")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := held.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, ErrCredentialsChanged) {
				t.Fatalf("issuer under strict default isolation got %v, want ErrCredentialsChanged", err)
			}
			if n := tc.count(ctx); n != 0 {
				t.Fatalf("stale issuer wrote %d credentials", n)
			}
			if err := tc.issue(ctx, newGeneration); err != nil {
				t.Fatalf("fresh generation issuance failed: %v", err)
			}
			if n := tc.count(ctx); n != 1 {
				t.Fatalf("fresh generation wrote %d credentials", n)
			}
		})
	}
}

func TestPasswordChangeRollbackAfterRevocationAndDeferredCommitFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger string
	}{
		{
			name: "after revocation statement",
			trigger: `CREATE TRIGGER password_change_failure AFTER UPDATE ON api_tokens
 FOR EACH ROW EXECUTE FUNCTION password_change_failure()`,
		},
		{
			name: "deferred commit",
			trigger: `CREATE CONSTRAINT TRIGGER password_change_failure AFTER UPDATE ON users
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION password_change_failure()`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, owner, org, _ := telemetryStore(t)
			if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "rollback-session", "rollback-csrf", time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			_, hash, prefix := apiTokenCredentials("rollback-token")
			if _, err := db.CreateAPIToken(ctx, owner.ID, owner.AuthGeneration, org.Slug, APITokenInput{Name: "Rollback token", Scopes: []string{"project:read"}, ExpiresInDays: 1}, hash, prefix); err != nil {
				t.Fatal(err)
			}
			before, err := db.UserByID(ctx, owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.pool.Exec(ctx, `CREATE FUNCTION password_change_failure() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'fixture password change failure'; END $$;`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.pool.Exec(ctx, tc.trigger); err != nil {
				t.Fatal(err)
			}
			if err := db.ChangePassword(ctx, owner.ID, owner.AuthGeneration, []byte("replacement-password-hash")); err == nil {
				t.Fatal("password change committed despite the injected failure")
			}
			after, err := db.UserByID(ctx, owner.ID)
			if err != nil || after.AuthGeneration != before.AuthGeneration || !reflect.DeepEqual(after.PasswordHash, before.PasswordHash) {
				t.Fatalf("user changed after rollback: before=%+v after=%+v err=%v", before, after, err)
			}
			var sessions, activeTokens int
			if err := db.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM sessions WHERE user_id=$1::bigint),
 (SELECT count(*) FROM api_tokens WHERE user_id=$1::bigint AND revoked_at IS NULL)`, owner.ID).Scan(&sessions, &activeTokens); err != nil {
				t.Fatal(err)
			}
			if sessions != 1 || activeTokens != 1 {
				t.Fatalf("partial revocation survived rollback: sessions=%d activeTokens=%d", sessions, activeTokens)
			}
			if _, csrf, err := db.Session(ctx, "rollback-session"); err != nil || csrf != "rollback-csrf" {
				t.Fatalf("rolled-back session=%q err=%v", csrf, err)
			}
			if grant, err := db.AuthenticateAPIToken(ctx, hash); err != nil || grant.User.ID != owner.ID {
				t.Fatalf("rolled-back token=%+v err=%v", grant, err)
			}
		})
	}
}
