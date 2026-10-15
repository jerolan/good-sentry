//go:build integration

package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func apiTokenCredentials(seed string) (secret, hash, prefix string) {
	value := sha256.Sum256([]byte(seed))
	secret = "gs_" + hex.EncodeToString(value[:])
	mac := hmac.New(sha256.New, []byte("isolated-api-token-test-key"))
	_, _ = mac.Write([]byte(secret))
	return secret, hex.EncodeToString(mac.Sum(nil)), secret[:11]
}

func apiTokenFixture(t *testing.T, ctx context.Context, db *Store, userID, orgSlug, seed string, projects []int64) (APIToken, string, string) {
	t.Helper()
	secret, hash, prefix := apiTokenCredentials(seed)
	token, err := db.CreateAPIToken(ctx, userID, orgSlug, APITokenInput{
		Name: "Automation", Scopes: []string{"org:read", "project:read"},
		ProjectIDs: projects, ExpiresInDays: 90,
	}, hash, prefix)
	if err != nil {
		t.Fatal(err)
	}
	return token, hash, secret
}

func apiTokenProjectID(t *testing.T, id string) int64 {
	t.Helper()
	value, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAPITokenPersistsDigestAndGrantAcrossReopen(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	projectID := apiTokenProjectID(t, project.ID)
	token, hash, secret := apiTokenFixture(t, ctx, db, user.ID, org.Slug, "persisted", []int64{projectID})
	if token.ID == "" || token.Name != "Automation" || token.Prefix != secret[:11] || token.CreatedBy != user.ID || token.AllProjects ||
		!reflect.DeepEqual(token.Scopes, []string{"org:read", "project:read"}) ||
		!reflect.DeepEqual(token.Projects, []APITokenProject{{ID: project.ID, Slug: project.Slug, Name: project.Name}}) ||
		token.DateCreated.IsZero() || token.ExpiresAt.Sub(token.DateCreated) != 90*24*time.Hour || token.RevokedAt != nil {
		t.Fatalf("created metadata = %+v", token)
	}
	var storedHash, storedRow string
	if err := db.pool.QueryRow(ctx, `SELECT secret_hash,to_jsonb(t)::text FROM api_tokens t WHERE id=$1`, token.ID).Scan(&storedHash, &storedRow); err != nil {
		t.Fatal(err)
	}
	if storedHash != hash || strings.Contains(storedRow, secret) {
		t.Fatal("database did not preserve only the supplied digest and public prefix")
	}
	metadata, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), hash) || strings.Contains(string(metadata), secret) {
		t.Fatal("credential material leaked into metadata")
	}
	grant, err := db.AuthenticateAPIToken(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if grant.User.ID != user.ID || grant.User.Email != user.Email || grant.User.Name != user.Name || grant.OrganizationID != org.ID || grant.AllProjects ||
		!reflect.DeepEqual(grant.ProjectIDs, []int64{projectID}) || !reflect.DeepEqual(grant.Scopes, token.Scopes) {
		t.Fatalf("grant = %+v", grant)
	}
	if _, err := db.AuthenticateAPIToken(ctx, secret); !errors.Is(err, ErrNotFound) {
		t.Fatalf("plaintext lookup error = %v", err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	after, err := reopened.AuthenticateAPIToken(ctx, hash)
	if err != nil || !reflect.DeepEqual(after, grant) {
		t.Fatalf("reopened grant = %+v, error = %v; want %+v", after, err, grant)
	}
	page, err := reopened.APITokens(ctx, user.ID, org.Slug, 1)
	if err != nil || len(page.Data) != 1 || !reflect.DeepEqual(page.Data[0], token) {
		t.Fatalf("reopened metadata = %+v, error = %v", page, err)
	}
}

func TestAPITokenInvalidationUsesCurrentDatabaseState(t *testing.T) {
	for _, cause := range []string{"expired", "revoked", "creator removed"} {
		t.Run(cause, func(t *testing.T) {
			ctx, db, user, org, project := telemetryStore(t)
			token, hash, _ := apiTokenFixture(t, ctx, db, user.ID, org.Slug, cause, nil)
			grant, err := db.AuthenticateAPIToken(ctx, hash)
			if err != nil {
				t.Fatal(err)
			}
			switch cause {
			case "expired":
				if _, err := db.pool.Exec(ctx, `UPDATE api_tokens SET expires_at=now()-interval '1 second' WHERE id=$1`, token.ID); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := db.RevokeAPIToken(ctx, user.ID, org.Slug, token.ID); err != nil {
					t.Fatal(err)
				}
				page, err := db.APITokens(ctx, user.ID, org.Slug, 1)
				if err != nil || len(page.Data) != 1 || page.Data[0].RevokedAt == nil {
					t.Fatalf("revocation metadata = %+v, error = %v", page, err)
				}
				stamp := *page.Data[0].RevokedAt
				if err := db.RevokeAPIToken(ctx, user.ID, org.Slug, token.ID); err != nil {
					t.Fatal(err)
				}
				page, err = db.APITokens(ctx, user.ID, org.Slug, 1)
				if err != nil || len(page.Data) != 1 || page.Data[0].RevokedAt == nil || !page.Data[0].RevokedAt.Equal(stamp) {
					t.Fatalf("repeated revoke changed history = %+v, error = %v", page, err)
				}
			case "creator removed":
				if _, err := db.pool.Exec(ctx, `UPDATE memberships SET role='member' WHERE organization_id=$1 AND user_id=$2`, org.ID, user.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := db.AuthenticateAPIToken(ctx, hash); err != nil {
					t.Fatalf("ordinary member lost read grant: %v", err)
				}
				if _, err := db.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2`, org.ID, user.ID); err != nil {
					t.Fatal(err)
				}
				access := ManagementAccess{UserID: grant.User.ID, OrganizationID: grant.OrganizationID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs}
				if _, err := db.ManagementProjects(ctx, access, "", 0, 100); !errors.Is(err, ErrNotFound) {
					t.Fatalf("stale list grant error = %v", err)
				}
				if _, err := db.ManagementProject(ctx, access, project.ID); !errors.Is(err, ErrNotFound) {
					t.Fatalf("stale detail grant error = %v", err)
				}
			}
			if _, err := db.AuthenticateAPIToken(ctx, hash); !errors.Is(err, ErrNotFound) {
				t.Fatalf("%s token lookup error = %v", cause, err)
			}
			if cause == "creator removed" {
				if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, org.ID, user.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := db.AuthenticateAPIToken(ctx, hash); !errors.Is(err, ErrNotFound) {
					t.Fatalf("reenrollment revived removed credential: %v", err)
				}
			}
		})
	}
}

func TestAPITokenAdministrationAndAtomicProjectBoundary(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	token, _, _ := apiTokenFixture(t, ctx, db, user.ID, org.Slug, "owner", nil)
	var member string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,name) VALUES('token-member@example.test',$1,'Member') RETURNING id::text`, []byte("already-hashed")).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	_, hash, prefix := apiTokenCredentials("member")
	input := APITokenInput{Name: "Member automation", Scopes: []string{"project:read"}, ExpiresInDays: 1}
	if _, err := db.CreateAPIToken(ctx, member, org.Slug, input, hash, prefix); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member create error = %v", err)
	}
	if _, err := db.APITokens(ctx, member, org.Slug, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member history error = %v", err)
	}
	if err := db.RevokeAPIToken(ctx, member, org.Slug, token.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member revoke error = %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE memberships SET role='admin' WHERE organization_id=$1 AND user_id=$2`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	adminToken, err := db.CreateAPIToken(ctx, member, org.Slug, input, hash, prefix)
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.APITokens(ctx, member, org.Slug, 1)
	if err != nil || len(page.Data) != 2 {
		t.Fatalf("admin history = %+v, error = %v", page, err)
	}
	if err := db.RevokeAPIToken(ctx, member, org.Slug, token.ID); err != nil {
		t.Fatalf("admin could not revoke organization credential: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE memberships SET role='member' WHERE organization_id=$1 AND user_id=$2`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	if grant, err := db.AuthenticateAPIToken(ctx, hash); err != nil || !reflect.DeepEqual(grant.Scopes, []string{"project:read"}) {
		t.Fatalf("downgraded creator read grant = %+v, error = %v", grant, err)
	}
	if err := db.RevokeAPIToken(ctx, member, org.Slug, adminToken.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("downgraded administrator revoke error = %v", err)
	}
	var foreignID string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('foreign-token-org','Foreign') RETURNING id::text`).Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, foreignID, user.ID); err != nil {
		t.Fatal(err)
	}
	foreign, err := db.CreateProject(ctx, user.ID, "foreign-token-org", "Foreign", "go")
	if err != nil {
		t.Fatal(err)
	}
	foreignToken, _, _ := apiTokenFixture(t, ctx, db, user.ID, "foreign-token-org", "foreign", nil)
	ownAccess := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	if _, err := db.ManagementProject(ctx, ownAccess, foreign.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign project detail error = %v", err)
	}
	ownProjects, err := db.ManagementProjects(ctx, ownAccess, "", 0, 100)
	if err != nil || len(ownProjects) != 1 || ownProjects[0].ID != project.ID {
		t.Fatalf("organization project isolation = %+v, error = %v", ownProjects, err)
	}
	if err := db.RevokeAPIToken(ctx, user.ID, org.Slug, foreignToken.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign revoke error = %v", err)
	}
	if _, err := db.APITokens(ctx, member, "foreign-token-org", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nonmember history error = %v", err)
	}
	var beforeTokens, beforeProjects int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM api_tokens),(SELECT count(*) FROM api_token_projects)`).Scan(&beforeTokens, &beforeProjects); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []int64{apiTokenProjectID(t, foreign.ID), 9223372036854775807} {
		_, hash, prefix := apiTokenCredentials(fmt.Sprintf("invalid-project-%d", invalid))
		input.ProjectIDs = []int64{apiTokenProjectID(t, project.ID), invalid}
		if _, err := db.CreateAPIToken(ctx, user.ID, org.Slug, input, hash, prefix); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid project %d error = %v", invalid, err)
		}
		var tokens, projects int
		if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM api_tokens),(SELECT count(*) FROM api_token_projects)`).Scan(&tokens, &projects); err != nil {
			t.Fatal(err)
		}
		if tokens != beforeTokens || projects != beforeProjects {
			t.Fatalf("failed creation partially persisted: tokens=%d projects=%d; before=%d,%d", tokens, projects, beforeTokens, beforeProjects)
		}
	}
}

func TestAPITokenAllAndRestrictedProjectGrants(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	_, allHash, _ := apiTokenFixture(t, ctx, db, user.ID, org.Slug, "all-projects", nil)
	restricted, restrictedHash, _ := apiTokenFixture(t, ctx, db, user.ID, org.Slug, "restricted-projects", []int64{apiTokenProjectID(t, project.ID)})
	allGrant, err := db.AuthenticateAPIToken(ctx, allHash)
	if err != nil || !allGrant.AllProjects {
		t.Fatalf("all-project grant = %+v, error = %v", allGrant, err)
	}
	allAccess := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: allGrant.AllProjects, ProjectIDs: allGrant.ProjectIDs}
	future, err := db.CreateProject(ctx, user.ID, org.Slug, "Gateway%_", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateProject(ctx, user.ID, org.Slug, "GatewayXY", "go"); err != nil {
		t.Fatal(err)
	}
	projects, err := db.ManagementProjects(ctx, allAccess, "", 0, 1)
	if err != nil || len(projects) != 2 || projects[0].ID != project.ID || projects[1].ID != future.ID {
		t.Fatalf("future project list and lookahead = %+v, error = %v", projects, err)
	}
	literal, err := db.ManagementProjects(ctx, allAccess, "%_", 0, 100)
	if err != nil || len(literal) != 1 || literal[0].ID != future.ID {
		t.Fatalf("literal substring filter = %+v, error = %v", literal, err)
	}
	for _, reference := range []string{future.ID, future.Slug} {
		p, err := db.ManagementProject(ctx, allAccess, reference)
		if err != nil || p.ID != future.ID {
			t.Fatalf("future project %q = %+v, error = %v", reference, p, err)
		}
	}
	grant, err := db.AuthenticateAPIToken(ctx, restrictedHash)
	if err != nil || grant.AllProjects {
		t.Fatalf("restricted grant = %+v, error = %v", grant, err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs}
	projects, err = db.ManagementProjects(ctx, access, "", 0, 100)
	if err != nil || len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("restricted project list = %+v, error = %v", projects, err)
	}
	if _, err := db.ManagementProject(ctx, access, future.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restricted future project error = %v", err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, project.ID); err != nil {
		t.Fatal(err)
	}
	grant, err = db.AuthenticateAPIToken(ctx, restrictedHash)
	if err != nil || grant.AllProjects || len(grant.ProjectIDs) != 0 {
		t.Fatalf("deleted last project widened grant = %+v, error = %v", grant, err)
	}
	access.ProjectIDs = grant.ProjectIDs
	projects, err = db.ManagementProjects(ctx, access, "", 0, 100)
	if err != nil || len(projects) != 0 {
		t.Fatalf("empty restricted project list = %+v, error = %v", projects, err)
	}
	if _, err := db.ManagementProject(ctx, access, future.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted last project exposed future project: %v", err)
	}
	page, err := db.APITokens(ctx, user.ID, org.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, token := range page.Data {
		if token.ID == restricted.ID {
			found = true
			if token.AllProjects || len(token.Projects) != 0 {
				t.Fatalf("restricted metadata widened after deletion = %+v", token)
			}
		}
	}
	if !found {
		t.Fatal("restricted credential disappeared after project deletion")
	}
}

func TestAPITokenMetadataHistoryPaging(t *testing.T) {
	ctx, db, user, org, _ := telemetryStore(t)
	page, err := db.APITokens(ctx, user.ID, org.Slug, 1)
	if err != nil || len(page.Data) != 0 || page.Page != 1 || page.PageSize != 25 || page.HasMore {
		t.Fatalf("empty token history = %+v, error = %v", page, err)
	}
	created := make([]APIToken, 26)
	for i := range created {
		created[i], _, _ = apiTokenFixture(t, ctx, db, user.ID, org.Slug, fmt.Sprintf("history-%d", i), nil)
	}
	// Equal creation timestamps make descending ID the observable tie-breaker.
	if _, err := db.pool.Exec(ctx, `UPDATE api_tokens SET created_at='2025-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE api_tokens SET expires_at=now()-interval '1 second' WHERE id=$1`, created[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeAPIToken(ctx, user.ID, org.Slug, created[1].ID); err != nil {
		t.Fatal(err)
	}
	first, err := db.APITokens(ctx, user.ID, org.Slug, 1)
	if err != nil || first.Page != 1 || first.PageSize != 25 || len(first.Data) != 25 || !first.HasMore {
		t.Fatalf("first history page = %+v, error = %v", first, err)
	}
	for i, token := range first.Data {
		if token.ID != created[25-i].ID || token.Name != "Automation" {
			t.Fatalf("history[%d] = %+v; want ID %s", i, token, created[25-i].ID)
		}
	}
	if first.Data[24].RevokedAt == nil {
		t.Fatal("revoked credential disappeared from history")
	}
	second, err := db.APITokens(ctx, user.ID, org.Slug, 2)
	if err != nil || second.Page != 2 || second.PageSize != 25 || len(second.Data) != 1 || second.HasMore || second.Data[0].ID != created[0].ID || !second.Data[0].ExpiresAt.Before(time.Now()) {
		t.Fatalf("expired credential history page = %+v, error = %v", second, err)
	}
	third, err := db.APITokens(ctx, user.ID, org.Slug, 3)
	if err != nil || third.Page != 3 || third.PageSize != 25 || len(third.Data) != 0 || third.HasMore {
		t.Fatalf("past-end history page = %+v, error = %v", third, err)
	}
}
