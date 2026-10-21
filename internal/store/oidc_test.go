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
}
