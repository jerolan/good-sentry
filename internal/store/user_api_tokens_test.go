//go:build integration

package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func createUserToken(t *testing.T, db *Store, userID, label string, mask int64, seed int) (UserAPIToken, string) {
	t.Helper()
	hash := fmt.Sprintf("%064x", seed)
	user, err := db.UserByID(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := db.CreateUserAPIToken(t.Context(), userID, user.AuthGeneration, label, mask, hash, []byte(fmt.Sprintf("sealed-%d", seed)))
	if err != nil {
		t.Fatal(err)
	}
	return token, hash
}

func userTokenLabels(tokens []UserAPIToken) []string {
	labels := make([]string, len(tokens))
	for i, token := range tokens {
		labels[i] = token.Label
	}
	return labels
}

func TestUserAPITokenRoundTripAndGrant(t *testing.T) {
	ctx, db, user, _, _ := telemetryStore(t)
	mask, ok := UserTokenMaskChecked([]string{"member:read", "project:read"})
	if !ok || mask != 1|(1<<13) {
		t.Fatalf("scope mask=%d ok=%t", mask, ok)
	}
	token, hash := createUserToken(t, db, user.ID, "Probe", mask, 1001)
	if token.ID <= 0 || token.Label != "Probe" || token.ScopesMask != mask || token.CreatedAt.IsZero() || !reflect.DeepEqual(token.Sealed, []byte("sealed-1001")) {
		t.Fatalf("created token=%+v", token)
	}
	grant, err := db.AuthenticateUserAPIToken(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if grant.User.ID != user.ID || grant.User.Email != user.Email || grant.OrganizationID != "" || !grant.AllProjects || grant.ScopesMask != mask ||
		!reflect.DeepEqual(grant.Scopes, []string{"project:read", "member:read"}) {
		t.Fatalf("grant=%+v", grant)
	}
	page, more, err := db.UserAPITokens(ctx, user.ID, 10, nil)
	if err != nil || more || len(page) != 1 || page[0].ID != token.ID || page[0].Label != "Probe" {
		t.Fatalf("page=%+v more=%t err=%v", page, more, err)
	}
	if err := db.DeleteUserAPIToken(ctx, user.ID, strconv.FormatInt(token.ID, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AuthenticateUserAPIToken(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted credential error=%v", err)
	}
	if err := db.DeleteUserAPIToken(ctx, user.ID, strconv.FormatInt(token.ID, 10)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete error=%v", err)
	}
	if _, ok := UserTokenMaskChecked([]string{"project:read", "unknown:read"}); ok {
		t.Fatal("unknown scope was accepted")
	}
}

func TestUserAPITokenCursorPages(t *testing.T) {
	ctx, db, user, _, _ := telemetryStore(t)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		token, _ := createUserToken(t, db, user.ID, fmt.Sprintf("token-%d", i), int64(i), 2000+i)
		if _, err := db.pool.Exec(ctx, "UPDATE user_api_tokens SET created_at=$2 WHERE id=$1", token.ID, base.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	page := func(cursor *UserAPITokenCursor, limit int) ([]UserAPIToken, bool) {
		t.Helper()
		tokens, more, err := db.UserAPITokens(ctx, user.ID, limit, cursor)
		if err != nil {
			t.Fatal(err)
		}
		return tokens, more
	}
	first, more := page(nil, 2)
	if !more || !reflect.DeepEqual(userTokenLabels(first), []string{"token-0", "token-1"}) {
		t.Fatalf("first page=%+v more=%t", userTokenLabels(first), more)
	}
	second, more := page(&UserAPITokenCursor{Boundary: first[1].CreatedAt}, 2)
	if !more || !reflect.DeepEqual(userTokenLabels(second), []string{"token-2", "token-3"}) {
		t.Fatalf("second page=%+v more=%t", userTokenLabels(second), more)
	}
	third, more := page(&UserAPITokenCursor{Boundary: second[1].CreatedAt}, 2)
	if more || !reflect.DeepEqual(userTokenLabels(third), []string{"token-4"}) {
		t.Fatalf("third page=%+v more=%t", userTokenLabels(third), more)
	}
	previous, more := page(&UserAPITokenCursor{Boundary: second[0].CreatedAt, Reverse: true}, 2)
	if more || !reflect.DeepEqual(userTokenLabels(previous), []string{"token-0", "token-1"}) {
		t.Fatalf("previous page=%+v more=%t", userTokenLabels(previous), more)
	}
	nearest, more := page(&UserAPITokenCursor{Boundary: third[0].CreatedAt, Reverse: true}, 2)
	if !more || !reflect.DeepEqual(userTokenLabels(nearest), []string{"token-2", "token-3"}) {
		t.Fatalf("nearest previous page=%+v more=%t", userTokenLabels(nearest), more)
	}
}

func TestUserAPITokenOwnerIsolationAndCascade(t *testing.T) {
	ctx, db, user, _, _ := telemetryStore(t)
	var secondID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES('second-token@example.test','Second token owner',$1) RETURNING id::text`, []byte("hash")).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	first, _ := createUserToken(t, db, user.ID, "Owner token", 1, 3001)
	second, secondHash := createUserToken(t, db, secondID, "Foreign token", 1, 3002)
	if page, _, err := db.UserAPITokens(ctx, user.ID, 10, nil); err != nil || len(page) != 1 || page[0].ID != first.ID {
		t.Fatalf("owner page=%+v err=%v", page, err)
	}
	if err := db.DeleteUserAPIToken(ctx, user.ID, strconv.FormatInt(second.ID, 10)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete error=%v", err)
	}
	if grant, err := db.AuthenticateUserAPIToken(ctx, secondHash); err != nil || grant.User.ID != secondID {
		t.Fatalf("foreign grant=%+v err=%v", grant, err)
	}
	if err := db.StageCompatRecoveryCodes(ctx, secondID, []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "DELETE FROM users WHERE id=$1::bigint", secondID); err != nil {
		t.Fatal(err)
	}
	var tokens, codes int
	if err := db.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM user_api_tokens WHERE user_id=$1::bigint),(SELECT count(*) FROM compat_recovery_codes WHERE user_id=$1::bigint)", secondID).Scan(&tokens, &codes); err != nil || tokens != 0 || codes != 0 {
		t.Fatalf("cascade tokens=%d codes=%d err=%v", tokens, codes, err)
	}
}

func TestCompatRecoveryCodeLifecycle(t *testing.T) {
	ctx, db, user, _, _ := telemetryStore(t)
	if err := db.StageCompatRecoveryCodes(ctx, user.ID, []string{"first-a", "first-b"}); err != nil {
		t.Fatal(err)
	}
	if err := db.ConsumeCompatRecoveryCode(ctx, user.ID, "wrong"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("wrong code error=%v", err)
	}
	if err := db.ConsumeCompatRecoveryCode(ctx, user.ID, "first-b"); err != nil {
		t.Fatal(err)
	}
	if err := db.ConsumeCompatRecoveryCode(ctx, user.ID, "first-a"); !errors.Is(err, ErrNoRecoveryCodes) {
		t.Fatalf("consumed set error=%v", err)
	}
	if err := db.StageCompatRecoveryCodes(ctx, user.ID, []string{"second-a", "second-b"}); err != nil {
		t.Fatal(err)
	}
	if err := db.ConsumeCompatRecoveryCode(ctx, user.ID, "first-a"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("replaced set retained old code: %v", err)
	}
	if err := db.ConsumeCompatRecoveryCode(ctx, user.ID, "second-b"); err != nil {
		t.Fatal(err)
	}
}

func TestAccountTokensMigration056PreservesPriorRows(t *testing.T) {
	ctx, db, _, _, _ := telemetryStore(t)
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('users',(SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY id),'[]') FROM users u),
 'organizations',(SELECT COALESCE(jsonb_agg(to_jsonb(o) ORDER BY id),'[]') FROM organizations o),
 'projects',(SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY id),'[]') FROM projects p))),'[]')::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	var receiptsBefore string
	if err := db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=55`).Scan(&receiptsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DROP TABLE IF EXISTS compat_recovery_codes; DROP TABLE IF EXISTS user_api_tokens; DELETE FROM schema_migrations WHERE version=56`); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	for range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=56").Scan(&receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/056_account_tokens.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			reopened.Close()
			t.Fatalf("056 receipt=%q err=%v", receipt, err)
		}
		if snapshot() != before {
			reopened.Close()
			t.Fatal("056 changed existing users, organizations or projects")
		}
		var receiptsAfter string
		if err := reopened.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM schema_migrations m WHERE version<=55`).Scan(&receiptsAfter); err != nil || receiptsAfter != receiptsBefore {
			reopened.Close()
			t.Fatalf("056 changed prior receipts: %v", err)
		}
		var tables, columns, index int
		if err := reopened.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('user_api_tokens','compat_recovery_codes')),
 (SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='user_api_tokens' AND column_name IN ('id','user_id','label','scopes_mask','secret_hash','secret','created_at')),
 (SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='user_api_tokens_owner')`).Scan(&tables, &columns, &index); err != nil || tables != 2 || columns != 7 || index != 1 {
			reopened.Close()
			t.Fatalf("056 tables=%d columns=%d index=%d err=%v", tables, columns, index, err)
		}
		reopened.Close()
	}
	if snapshot() != before {
		t.Fatal("final reopen changed existing rows")
	}
}
