//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This real-database boundary owns enrollment durability, the single-use step
// ledger, recovery-code consumption and challenge lifecycle. Concurrent replay
// and credential-generation changes are the regressions a pure test cannot
// expose.
func twoFactorFixture(t *testing.T) (context.Context, *Store, User, *pgx.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	schema := fmt.Sprintf("goodsentry_two_factor_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	sql, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sql.Close(context.Background()) })
	user, _, err := db.Bootstrap(ctx, "owner@two-factor.test", []byte("hashed"), "Two-factor owner", "Two-factor")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, db, user, sql
}

func twoFactorHashes(seed string) []string {
	hashes := make([]string, 0, 10)
	for index := range 10 {
		hashes = append(hashes, fmt.Sprintf("%s-%02d", seed, index))
	}
	return hashes
}

func TestTwoFactorEnrollmentLifecycle(t *testing.T) {
	ctx, db, user, _ := twoFactorFixture(t)
	sealed := []byte("sealed-secret-value")
	if err := db.StartTwoFactor(ctx, user.ID, user.AuthGeneration, sealed); err != nil {
		t.Fatal(err)
	}
	state, err := db.TwoFactorState(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Confirmed || string(state.Secret) != string(sealed) || state.RecoveryCodesRemaining != 0 || state.LastStep != nil {
		t.Fatalf("pending state=%+v", state)
	}
	if err = db.StartTwoFactor(ctx, user.ID, user.AuthGeneration+1, sealed); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("stale generation start=%v", err)
	}
	if err = db.ConfirmTwoFactor(ctx, user.ID, user.AuthGeneration, 0, nil); err == nil {
		t.Fatal("confirm accepted an empty recovery set")
	}
	if err = db.ConfirmTwoFactor(ctx, user.ID, user.AuthGeneration, 41, twoFactorHashes("code")); err != nil {
		t.Fatal(err)
	}
	state, err = db.TwoFactorState(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Confirmed || state.RecoveryCodesRemaining != 10 || state.LastStep == nil || *state.LastStep != 41 {
		t.Fatalf("confirmed state=%+v", state)
	}
	if err = db.StartTwoFactor(ctx, user.ID, user.AuthGeneration, sealed); !errors.Is(err, ErrConflict) {
		t.Fatalf("re-enrollment while enabled=%v", err)
	}
	if err = db.ReplaceRecoveryCodes(ctx, user.ID, user.AuthGeneration, []string{"replacement"}); err != nil {
		t.Fatal(err)
	}
	if state, err = db.TwoFactorState(ctx, user.ID); err != nil || state.RecoveryCodesRemaining != 1 {
		t.Fatalf("replaced codes state=%+v err=%v", state, err)
	}
	if err = db.DisableTwoFactor(ctx, user.ID, user.AuthGeneration); err != nil {
		t.Fatal(err)
	}
	state, err = db.TwoFactorState(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Confirmed || len(state.Secret) != 0 || state.RecoveryCodesRemaining != 0 || state.LastStep != nil {
		t.Fatalf("disabled state=%+v", state)
	}
	if err = db.ReplaceRecoveryCodes(ctx, user.ID, user.AuthGeneration, []string{"late"}); !errors.Is(err, ErrTwoFactorUnavailable) {
		t.Fatalf("replace while disabled=%v", err)
	}
}

func TestTwoFactorChallengeIsSingleUse(t *testing.T) {
	ctx, db, user, _ := twoFactorFixture(t)
	if err := db.StartTwoFactor(ctx, user.ID, user.AuthGeneration, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmTwoFactor(ctx, user.ID, user.AuthGeneration, 100, twoFactorHashes("code")); err != nil {
		t.Fatal(err)
	}
	expires, err := db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration, "first-challenge", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(expires) < 4*time.Minute {
		t.Fatalf("challenge expiry=%s", expires)
	}
	challenge, err := db.LoginChallenge(ctx, "first-challenge")
	if err != nil {
		t.Fatal(err)
	}
	if challenge.UserID != user.ID || challenge.AuthGeneration != user.AuthGeneration || challenge.Attempts != 0 || challenge.Consumed {
		t.Fatalf("challenge=%+v", challenge)
	}
	if _, err = db.CompleteTOTPChallenge(ctx, "first-challenge", user.ID, user.AuthGeneration, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompleteTOTPChallenge(ctx, "first-challenge", user.ID, user.AuthGeneration, 102); !errors.Is(err, ErrNotFound) {
		t.Fatalf("consumed challenge reuse=%v", err)
	}
	if _, err = db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration, "replay-challenge", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompleteTOTPChallenge(ctx, "replay-challenge", user.ID, user.AuthGeneration, 101); !errors.Is(err, ErrTwoFactorReplay) {
		t.Fatalf("replayed step=%v", err)
	}
	if _, err = db.CompleteTOTPChallenge(ctx, "replay-challenge", user.ID, user.AuthGeneration, 102); err != nil {
		t.Fatal(err)
	}
	state, err := db.TwoFactorState(ctx, user.ID)
	if err != nil || state.LastStep == nil || *state.LastStep != 102 {
		t.Fatalf("last step=%+v err=%v", state.LastStep, err)
	}
	if _, err = db.CompleteTOTPChallenge(ctx, "replay-challenge", user.ID, user.AuthGeneration, 103); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second use of a challenge=%v", err)
	}
}

func TestTwoFactorChallengeAttemptsAndExpiry(t *testing.T) {
	ctx, db, user, sql := twoFactorFixture(t)
	if err := db.StartTwoFactor(ctx, user.ID, user.AuthGeneration, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmTwoFactor(ctx, user.ID, user.AuthGeneration, 7, twoFactorHashes("code")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration, "attempts", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 4; attempt++ {
		if err := db.FailLoginChallenge(ctx, "attempts", 5); err != nil {
			t.Fatalf("failure %d: %v", attempt, err)
		}
		if challenge, err := db.LoginChallenge(ctx, "attempts"); err != nil || challenge.Attempts != attempt {
			t.Fatalf("attempt %d state=%+v err=%v", attempt, challenge, err)
		}
	}
	if err := db.FailLoginChallenge(ctx, "attempts", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginChallenge(ctx, "attempts"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("exhausted challenge=%v", err)
	}
	if err := db.FailLoginChallenge(ctx, "attempts", 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failing an absent challenge=%v", err)
	}
	expired, err := db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration, "expired", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sql.Exec(ctx, "UPDATE login_challenges SET expires_at=$2 WHERE token_hash=$1", "expired", expired.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompleteTOTPChallenge(ctx, "expired", user.ID, user.AuthGeneration, 8); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired challenge=%v", err)
	}
	if _, err = db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration, "expired", 5*time.Minute); err != nil {
		t.Fatalf("expired challenge was not reclaimed: %v", err)
	}
}

func TestTwoFactorRecoveryCodeIsSingleUse(t *testing.T) {
	ctx, db, user, _ := twoFactorFixture(t)
	if err := db.StartTwoFactor(ctx, user.ID, user.AuthGeneration, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmTwoFactor(ctx, user.ID, user.AuthGeneration, 3, []string{"alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration, "recovery-one", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteRecoveryChallenge(ctx, "recovery-one", user.ID, user.AuthGeneration, "alpha"); err != nil {
		t.Fatal(err)
	}
	state, err := db.TwoFactorState(ctx, user.ID)
	if err != nil || state.RecoveryCodesRemaining != 1 {
		t.Fatalf("remaining=%d err=%v", state.RecoveryCodesRemaining, err)
	}
	if _, err := db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration, "recovery-two", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteRecoveryChallenge(ctx, "recovery-two", user.ID, user.AuthGeneration, "alpha"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reused recovery code=%v", err)
	}
	if _, err := db.CompleteRecoveryChallenge(ctx, "recovery-two", user.ID, user.AuthGeneration, "beta"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteRecoveryChallenge(ctx, "recovery-two", user.ID, user.AuthGeneration, "beta"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second use of the same challenge=%v", err)
	}
}

func TestTwoFactorChallengeRequiresMatchingGeneration(t *testing.T) {
	ctx, db, user, _ := twoFactorFixture(t)
	if err := db.StartTwoFactor(ctx, user.ID, user.AuthGeneration, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmTwoFactor(ctx, user.ID, user.AuthGeneration, 11, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration, "generation", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteTOTPChallenge(ctx, "generation", user.ID, user.AuthGeneration+1, 12); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mismatched generation=%v", err)
	}
	if _, err := db.CompleteTOTPChallenge(ctx, "generation", user.ID, user.AuthGeneration, 12); err != nil {
		t.Fatalf("matching generation=%v", err)
	}
	if _, err := db.CreateLoginChallenge(ctx, user.ID, user.AuthGeneration+1, "stale", 5*time.Minute); !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("challenge for a stale generation=%v", err)
	}
}
