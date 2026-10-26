//go:build integration

package store

import (
	"errors"
	"testing"
)

func TestOIDCIdentityLinksAndCreatesAccounts(t *testing.T) {
	ctx, db, owner, _, _ := telemetryStore(t)
	linked, err := db.LoginOIDC(ctx, OIDCIdentity{
		Provider: "https://id.example.test", Subject: "subject-owner",
		Email: owner.Email, Name: "Changed provider name",
		Password: []byte("oidc-only-sentinel"),
	})
	if err != nil || linked.ID != owner.ID || linked.Email != owner.Email {
		t.Fatalf("link existing: %+v %v", linked, err)
	}
	again, err := db.LoginOIDC(ctx, OIDCIdentity{
		Provider: "https://id.example.test", Subject: "subject-owner",
		Email: "ignored@example.test", Name: "Ignored",
	})
	if err != nil || again.ID != owner.ID || again.Name != owner.Name {
		t.Fatalf("repeat identity: %+v %v", again, err)
	}
	if _, err = db.LoginOIDC(ctx, OIDCIdentity{
		Provider: "https://id.example.test", Subject: "other-subject",
		Email: owner.Email, Name: "Other",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second provider identity for one user=%v", err)
	}
	if _, err = db.LoginOIDC(ctx, OIDCIdentity{
		Provider: "https://id.example.test", Subject: "missing",
		Email: "missing@example.test", Name: "Missing", AutoCreate: false,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled auto-create=%v", err)
	}
	created, err := db.LoginOIDC(ctx, OIDCIdentity{
		Provider: "https://id.example.test", Subject: "subject-new",
		Email: "new@example.test", Name: "New Person", Password: []byte("oidc-only-sentinel"), AutoCreate: true,
	})
	if err != nil || created.ID == "" || created.Email != "new@example.test" || created.Name != "New Person" || created.AuthGeneration != 1 {
		t.Fatalf("auto-create: %+v %v", created, err)
	}
	var inactiveID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('inactive-primary@example.test',$1,'Inactive') RETURNING id::text`, []byte("inactive-hash")).Scan(&inactiveID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO user_emails(user_id,email,is_verified) VALUES($1::bigint,'inactive-secondary@example.test',true)`, inactiveID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1::bigint`, inactiveID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginOIDC(ctx, OIDCIdentity{
		Provider: "https://id.example.test", Subject: "inactive-secondary", Email: "inactive-secondary@example.test",
		Name: "Duplicate account", Password: []byte("oidc-only-sentinel"), AutoCreate: true,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("auto-create reused a suspended verified secondary email: %v", err)
	}
	var duplicatePrimary int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE lower(email)=lower('inactive-secondary@example.test')`).Scan(&duplicatePrimary); err != nil || duplicatePrimary != 0 {
		t.Fatalf("OIDC created a duplicate of a suspended secondary account: count=%d err=%v", duplicatePrimary, err)
	}
	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for _, input := range []OIDCIdentity{
		{Provider: "https://id.example.test", Subject: "subject-owner", Email: owner.Email},
		{Provider: "https://id.example.test", Subject: "subject-new", Email: "ignored@example.test"},
	} {
		user, err := reopened.LoginOIDC(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if input.Subject == "subject-owner" && user.ID != owner.ID || input.Subject == "subject-new" && user.ID != created.ID {
			t.Fatalf("identity changed after reopen: %+v", user)
		}
	}
	if _, err := reopened.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1::bigint`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.LoginOIDC(ctx, OIDCIdentity{
		Provider: "https://id.example.test", Subject: "subject-new",
		Email: "new@example.test", Name: "New Person", AutoCreate: true,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended identity signed in or was recreated: %v", err)
	}
	var suspendedRows int
	if err := reopened.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE lower(email)='new@example.test'`).Scan(&suspendedRows); err != nil || suspendedRows != 1 {
		t.Fatalf("suspended sign-in created an account: rows=%d err=%v", suspendedRows, err)
	}
}
