//go:build integration

package store

import (
	"errors"
	"sync"
	"testing"
)

func TestCompatibleUserDeletionRetainsOrganizationOwnerAndCascadesAccount(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	foreign := triageMember(t, ctx, db, org, "user-delete-member", "member")

	if _, err := db.pool.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES('user-delete-session',$1::bigint,'csrf',now()+interval '1 hour')`, foreign.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO user_emails(user_id,email,is_verified) VALUES($1::bigint,'verified@user-delete.test',true)`, foreign.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO user_identities(user_id,provider,subject,email) VALUES($1::bigint,'https://idp.test','subject','verified@user-delete.test')`, foreign.ID); err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteCompatibleUser(ctx, owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("delete sole owner error=%v", err)
	}
	if _, err := db.UserByID(ctx, owner.ID); err != nil {
		t.Fatalf("failed deletion removed owner: %v", err)
	}

	second := triageMember(t, ctx, db, org, "user-delete-second-owner", "owner")
	if err := db.DeleteCompatibleUser(ctx, owner.ID); !errors.Is(err, ErrLastInstanceAdmin) {
		t.Fatalf("delete last instance administrator error=%v", err)
	}
	if _, err := db.UserByID(ctx, owner.ID); err != nil {
		t.Fatalf("last administrator deletion removed the account: %v", err)
	}
	if err := db.DeleteCompatibleUser(ctx, foreign.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserByID(ctx, foreign.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user remains: %v", err)
	}
	for _, table := range []string{"sessions", "user_emails", "user_identities", "memberships", "team_memberships"} {
		var rows int
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE user_id=$1::bigint`, foreign.ID).Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("%s rows=%d err=%v", table, rows, err)
		}
	}
	if _, err := db.Organization(ctx, second.ID, org.Slug); err != nil {
		t.Fatalf("account deletion damaged organization: %v", err)
	}
}

func TestConcurrentCompatibleUserDeletionRetainsOneOrganizationOwner(t *testing.T) {
	ctx, db, first, org, _ := telemetryStore(t)
	second := triageMember(t, ctx, db, org, "user-delete-concurrent-owner", "owner")
	type result struct {
		user string
		err  error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var group sync.WaitGroup
	for _, user := range []User{first, second} {
		group.Go(func() {
			<-start
			results <- result{user: user.ID, err: db.DeleteCompatibleUser(ctx, user.ID)}
		})
	}
	close(start)
	group.Wait()
	close(results)

	success, retained := 0, 0
	for result := range results {
		switch {
		case result.err == nil:
			success++
		case errors.Is(result.err, ErrLastOwner), errors.Is(result.err, ErrLastInstanceAdmin):
			retained++
		default:
			t.Fatalf("delete user %s: %v", result.user, result.err)
		}
	}
	if success != 1 || retained != 1 {
		t.Fatalf("concurrent user deletion success=%d retained=%d", success, retained)
	}
	var owners int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE organization_id=$1::bigint AND role='owner'`, org.ID).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("organization owners=%d err=%v", owners, err)
	}
}
