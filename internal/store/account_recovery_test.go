//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/accountmail"
	"goodsentry/internal/notify"
)

func TestPasswordResetMigration036PreservesPriorRowsAndReopens(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "before036-session", "before036-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, hash, prefix := apiTokenCredentials("before036-token")
	if _, err := db.CreateAPIToken(ctx, owner.ID, owner.AuthGeneration, org.Slug, APITokenInput{Name: "Before036", Scopes: []string{"project:read"}, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}, ExpiresInDays: 1}, hash, prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DROP TABLE account_password_resets; DELETE FROM schema_migrations WHERE version=36`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 35)
	databaseURL := db.pool.Config().ConnString()
	for i := range 2 {
		reopened, err := Open(ctx, databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(reopened.Close)
		after := groupDatabaseSnapshot(t, ctx, reopened, 35)
		delete(after, "account_password_resets")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("036 rewrote prior users, credentials or migration receipts")
		}
		if user, csrf, err := reopened.Session(ctx, "before036-session"); err != nil || user.ID != owner.ID || csrf != "before036-csrf" {
			t.Fatalf("prior session changed: %+v csrf=%q err=%v", user, csrf, err)
		}
		if grant, err := reopened.AuthenticateAPIToken(ctx, hash); err != nil || grant.User.ID != owner.ID {
			t.Fatalf("prior token changed: %+v err=%v", grant, err)
		}
		var receipt string
		if err := reopened.pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=36`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/036_account_password_resets.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatalf("036 checksum receipt differs: %q err=%v", receipt, err)
		}
		if i == 0 {
			reopened.Close()
		}
	}
}

func TestPasswordResetEnqueueDeliveryConsumptionAndRevocation(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	box, err := accountmail.NewSecretBox(strings.Repeat("s", 64))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	hash := strings.Repeat("b", 64)
	ciphertext, err := box.Seal(hash, token)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := db.EnqueuePasswordReset(ctx, owner.ID, owner.AuthGeneration, owner.Email, hash, ciphertext, 30*time.Minute)
	if err != nil || !enqueued {
		t.Fatalf("enqueue=%v err=%v", enqueued, err)
	}
	enqueued, err = db.EnqueuePasswordReset(ctx, owner.ID, owner.AuthGeneration, owner.Email, strings.Repeat("c", 64), mustResetCiphertext(t, box, strings.Repeat("c", 64), strings.Repeat("c", 64)), 30*time.Minute)
	if err != nil || enqueued {
		t.Fatalf("request floor enqueue=%v err=%v", enqueued, err)
	}
	var rows int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM account_password_resets WHERE user_id=$1::bigint`, owner.ID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("request floor rows=%d err=%v", rows, err)
	}
	lease, err := db.ClaimPasswordReset(ctx, 3, time.Second)
	if err != nil || lease.ID == "" || lease.UserName != owner.Name || lease.Recipient != owner.Email || lease.Attempts != 1 || !reflect.DeepEqual(lease.TokenCiphertext, ciphertext) || lease.PasswordReset.ExpiresAt.Sub(lease.CreatedAt) != 30*time.Minute {
		t.Fatalf("claim=%+v err=%v", lease, err)
	}
	if err := db.CompletePasswordReset(ctx, lease, notify.Outcome{Disposition: notify.Delivered, Reason: "accepted", ElapsedMS: 3}, 3); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.pool.QueryRow(ctx, `SELECT status FROM account_password_resets WHERE token_hash=$1`, hash).Scan(&status); err != nil || status != "sent" {
		t.Fatalf("delivery status=%q err=%v", status, err)
	}
	if err := db.CreateSession(ctx, owner.ID, owner.AuthGeneration, "reset-old-session", "reset-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, tokenHash, prefix := apiTokenCredentials("reset-old-token")
	if _, err := db.CreateAPIToken(ctx, owner.ID, owner.AuthGeneration, org.Slug, APITokenInput{Name: "Reset old", Scopes: []string{"project:read"}, ExpiresInDays: 1}, tokenHash, prefix); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("reset-replacement-hash")
	if err := db.ResetPassword(ctx, hash, replacement); err != nil {
		t.Fatal(err)
	}
	current, err := db.UserByID(ctx, owner.ID)
	if err != nil || current.AuthGeneration != owner.AuthGeneration+1 || !reflect.DeepEqual(current.PasswordHash, replacement) {
		t.Fatalf("reset account state=%+v err=%v", current, err)
	}
	if n, _ := credentialCounts(t, ctx, db, owner.ID); n != 0 {
		t.Fatalf("reset retained %d sessions", n)
	}
	if _, err := db.AuthenticateAPIToken(ctx, tokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reset token survived: %v", err)
	}
	if err := db.ResetPassword(ctx, hash, []byte("replay")); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("reset replay error=%v", err)
	}
	if status, consumed := resetState(t, ctx, db, hash); status != "consumed" || consumed == nil {
		t.Fatalf("consumed reset status=%q consumed=%v", status, consumed)
	}
}

func TestPasswordResetRetryExpiryAndStaleLease(t *testing.T) {
	ctx, db, owner, _, _ := telemetryStore(t)
	box, err := accountmail.NewSecretBox(strings.Repeat("s", 64))
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("d", 64)
	ciphertext := mustResetCiphertext(t, box, hash, strings.Repeat("d", 64))
	if enqueued, err := db.EnqueuePasswordReset(ctx, owner.ID, owner.AuthGeneration, owner.Email, hash, ciphertext, 30*time.Minute); err != nil || !enqueued {
		t.Fatalf("enqueue=%v err=%v", enqueued, err)
	}
	lease, err := db.ClaimPasswordReset(ctx, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompletePasswordReset(ctx, lease, notify.Outcome{Disposition: notify.Retry, Reason: "connection_error", ElapsedMS: 5}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE account_password_resets SET next_attempt=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	second, err := db.ClaimPasswordReset(ctx, 2, time.Second)
	if err != nil || second.Attempts != 2 {
		t.Fatalf("retry claim=%+v err=%v", second, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE account_password_resets SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimPasswordReset(ctx, 2, time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attempt-exhausted reset reclaimed: %v", err)
	}
	if status, reason := resetFailure(t, ctx, db, hash); status != "failed" || reason != "attempts_exhausted" {
		t.Fatalf("exhausted status=%q reason=%q", status, reason)
	}

	hash = strings.Repeat("e", 64)
	if _, err := db.pool.Exec(ctx, `UPDATE account_password_resets SET created_at=clock_timestamp()-interval '61 seconds', updated_at=clock_timestamp()-interval '61 seconds' WHERE user_id=$1::bigint`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE users SET auth_generation=auth_generation+1 WHERE id=$1::bigint`, owner.ID); err != nil {
		t.Fatal(err)
	}
	current, err := db.UserByID(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if enqueued, err := db.EnqueuePasswordReset(ctx, owner.ID, current.AuthGeneration, owner.Email, hash, mustResetCiphertext(t, box, hash, strings.Repeat("e", 64)), 30*time.Minute); err != nil || !enqueued {
		t.Fatalf("second enqueue=%v err=%v", enqueued, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE account_password_resets SET expires_at=clock_timestamp()-interval '1 second' WHERE token_hash=$1`, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimPasswordReset(ctx, 2, time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired reset was claimable: %v", err)
	}
	if status, reason := resetFailure(t, ctx, db, hash); status != "failed" || reason != "expired" {
		t.Fatalf("expired status=%q reason=%q", status, reason)
	}
}

func mustResetCiphertext(t *testing.T, box *accountmail.SecretBox, hash, token string) []byte {
	t.Helper()
	ciphertext, err := box.Seal(hash, token)
	if err != nil {
		t.Fatal(err)
	}
	return ciphertext
}

func resetState(t *testing.T, ctx context.Context, db *Store, hash string) (string, *time.Time) {
	t.Helper()
	var status string
	var consumed *time.Time
	if err := db.pool.QueryRow(ctx, `SELECT status,consumed_at FROM account_password_resets WHERE token_hash=$1`, hash).Scan(&status, &consumed); err != nil {
		t.Fatal(err)
	}
	return status, consumed
}

func resetFailure(t *testing.T, ctx context.Context, db *Store, hash string) (string, string) {
	t.Helper()
	var status, reason string
	if err := db.pool.QueryRow(ctx, `SELECT status,last_reason FROM account_password_resets WHERE token_hash=$1`, hash).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	return status, reason
}
