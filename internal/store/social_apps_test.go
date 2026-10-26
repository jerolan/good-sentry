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

// This real-database boundary owns organization application durability,
// provider-identity uniqueness, containment and cascade cleanup.
func socialAppFixture(t *testing.T) (context.Context, *Store, *pgx.Conn, User, Organization, Organization, string) {
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
	schema := fmt.Sprintf("goodsentry_social_apps_%d", time.Now().UnixNano())
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
	owner, org, err := db.Bootstrap(ctx, "owner@social-apps.test", []byte("hashed"), "Social owner", "Social apps")
	if err != nil {
		t.Fatal(err)
	}
	var secondID string
	if err = sql.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('second-social','Second social') RETURNING id::text").Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	return ctx, db, sql, owner, org, Organization{ID: secondID, Slug: "second-social", Name: "Second social"}, u.String()
}

func TestSocialAppsPersistAndStayContained(t *testing.T) {
	ctx, db, sql, _, org, second, _ := socialAppFixture(t)
	app, err := db.CreateSocialApp(ctx, org.ID, SocialAppInput{
		ProviderID: "social-apps-abc123", Provider: "openid_connect", Brand: "openid_connect",
		Name: "Corp SSO", ClientID: "client-1", ClientSecret: []byte("sealed-secret"), ServerURL: "https://issuer.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.ID == "" || app.OrganizationID != org.ID || app.Brand != "openid_connect" || app.Name != "Corp SSO" || string(app.ClientSecret) != "sealed-secret" {
		t.Fatalf("app=%+v", app)
	}
	if _, err = db.CreateSocialApp(ctx, org.ID, SocialAppInput{ProviderID: "social-apps-abc123", Provider: "openid_connect", Brand: "openid_connect", Name: "Duplicate", ClientID: "c", ClientSecret: []byte("s"), ServerURL: "https://issuer.example.test"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate provider identity=%v", err)
	}
	if _, err = db.CreateSocialApp(ctx, org.ID, SocialAppInput{ProviderID: "missing", Provider: "openid_connect", Brand: "openid_connect", ServerURL: "https://x.test"}); err == nil {
		t.Fatal("an incomplete application was stored")
	}
	apps, err := db.SocialApps(ctx, org.ID)
	if err != nil || len(apps) != 1 || apps[0].ProviderID != app.ProviderID {
		t.Fatalf("apps=%+v err=%v", apps, err)
	}
	if foreign, err := db.SocialApps(ctx, second.ID); err != nil || len(foreign) != 0 {
		t.Fatalf("second organization apps=%+v err=%v", foreign, err)
	}
	if _, err = db.SocialApp(ctx, second.ID, app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-organization read=%v", err)
	}
	if err = db.DeleteSocialApp(ctx, second.ID, app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-organization delete=%v", err)
	}
	byProvider, err := db.SocialAppByProviderID(ctx, app.ProviderID)
	if err != nil || byProvider.ID != app.ID {
		t.Fatalf("provider lookup=%+v err=%v", byProvider, err)
	}
	publicOrg, publicApps, err := db.PublicOrganizationApps(ctx, org.Slug)
	if err != nil || publicOrg.ID != org.ID || len(publicApps) != 1 || publicApps[0].ProviderID != app.ProviderID {
		t.Fatalf("public apps=%+v org=%+v err=%v", publicApps, publicOrg, err)
	}
	if _, _, err = db.PublicOrganizationApps(ctx, "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent public organization=%v", err)
	}

	updated, err := db.UpdateSocialApp(ctx, org.ID, app.ID, "Corp SSO 2", "client-2", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Corp SSO 2" || updated.ClientID != "client-2" || string(updated.ClientSecret) != "sealed-secret" || updated.ServerURL != "https://issuer.example.test" {
		t.Fatalf("updated=%+v", updated)
	}
	rotated, err := db.UpdateSocialApp(ctx, org.ID, app.ID, "Corp SSO 2", "client-2", []byte("rotated"), "https://login.example.test")
	if err != nil || string(rotated.ClientSecret) != "rotated" || rotated.ServerURL != "https://login.example.test" {
		t.Fatalf("rotated=%+v err=%v", rotated, err)
	}
	if _, err = db.UpdateSocialApp(ctx, org.ID, "999999", "Missing", "client", nil, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update=%v", err)
	}

	// Deleting the organization removes its applications.
	if _, err = sql.Exec(ctx, "DELETE FROM organizations WHERE id=$1::bigint", org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SocialAppByProviderID(ctx, app.ProviderID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cascade cleanup=%v", err)
	}
}

func TestSocialOAuth2ProviderMigrationPreservesExistingApps(t *testing.T) {
	ctx, db, sql, _, org, _, dsn := socialAppFixture(t)
	existing, err := db.CreateSocialApp(ctx, org.ID, SocialAppInput{
		ProviderID: "migration-existing", Provider: "openid_connect", Brand: "openid_connect", Name: "Existing SSO",
		ClientID: "existing-client", ClientSecret: []byte("existing-secret"), ServerURL: "https://issuer.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sql.Exec(ctx, `ALTER TABLE social_apps DROP CONSTRAINT social_apps_provider_check;
		ALTER TABLE social_apps ADD CONSTRAINT social_apps_provider_check CHECK (provider IN ('openid_connect','google'));
		DELETE FROM schema_migrations WHERE version IN (70,71)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	preserved, err := reopened.SocialAppByProviderID(ctx, existing.ProviderID)
	if err != nil || preserved.Name != existing.Name || string(preserved.ClientSecret) != string(existing.ClientSecret) {
		t.Fatalf("preserved app=%+v err=%v", preserved, err)
	}
	if _, err = reopened.CreateSocialApp(ctx, org.ID, SocialAppInput{
		ProviderID: "migration-github", Provider: "github", Brand: "github", Name: "GitHub",
		ClientID: "github-client", ClientSecret: []byte("github-secret"), ServerURL: "https://github.com",
	}); err != nil {
		t.Fatalf("create GitHub application after migration: %v", err)
	}
	if _, err = reopened.CreateSocialApp(ctx, org.ID, SocialAppInput{
		ProviderID: "migration-gitea", Provider: "gitea", Brand: "gitea", Name: "Gitea",
		ClientID: "gitea-client", ClientSecret: []byte("gitea-secret"), ServerURL: "https://gitea.example.test",
	}); err != nil {
		t.Fatalf("create Gitea application after migration: %v", err)
	}
}
