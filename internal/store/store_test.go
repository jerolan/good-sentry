//go:build integration

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/event"
)

// This real-database boundary owns durable bootstrap, grouping and SDK retry
// accounting. Concurrent duplicate capture is the regression ordinary parser
// tests cannot expose. The test uses the production database URL interface.
func TestDurableCaptureAndTenantScope(t *testing.T) {
	ctx := context.Background()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("goodsentry_test_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	var user User
	var org Organization
	type setupResult struct {
		user User
		org  Organization
		err  error
	}
	setups := make(chan setupResult, 8)
	var setupGroup sync.WaitGroup
	for range 8 {
		setupGroup.Go(func() {
			u, o, e := db.Bootstrap(ctx, "owner@example.test", []byte("already-hashed"), "Owner", "Example")
			setups <- setupResult{u, o, e}
		})
	}
	setupGroup.Wait()
	close(setups)
	winners := 0
	for result := range setups {
		if result.err == nil {
			winners++
			user = result.user
			org = result.org
		} else if result.err != ErrConflict {
			t.Fatal(result.err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent first-owner winners=%d", winners)
	}
	_, _, err = db.Bootstrap(ctx, "other@example.test", []byte("hash"), "Other", "Other")
	if err != ErrConflict {
		t.Fatalf("bootstrap reopened: %v", err)
	}
	project, err := db.CreateProject(ctx, user.ID, org.Slug, "Backend", "go")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ValidKey(ctx, project.ID, project.PublicKey); err != nil || !ok {
		t.Fatalf("DSN key not usable: %v", err)
	}
	raw := json.RawMessage(`{"event_id":"0123456789abcdef0123456789abcdef","message":"database unavailable","level":"error","tags":{"region":"test"}}`)
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _, _, err := db.Ingest(ctx, project.ID, ev); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	issues, total, err := db.Issues(ctx, user.ID, org.Slug, IssueFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(issues) != 1 || issues[0].Count != 1 {
		t.Fatalf("retry accounting: total=%d issues=%+v", total, issues)
	}
	id := issues[0].ID
	db.Close()
	db, err = Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	detail, err := db.IssueDetail(ctx, user.ID, id)
	if err != nil || detail.Issue.Count != 1 {
		t.Fatalf("restart lost issue: %+v %v", detail.Issue, err)
	}
	if _, err = db.IssueDetail(ctx, "99999999", id); err != ErrNotFound {
		t.Fatalf("tenant boundary failed: %v", err)
	}
	if _, err = db.UpdateIssue(ctx, user.ID, id, IssueUpdate{Status: "resolved"}); err != nil {
		t.Fatal(err)
	}
	ev.ID = "1123456789abcdef0123456789abcdef"
	if _, _, err = db.Ingest(ctx, project.ID, ev); err != nil {
		t.Fatal(err)
	}
	detail, err = db.IssueDetail(ctx, user.ID, id)
	if err != nil || detail.Issue.Status != "unresolved" || detail.Issue.Count != 2 {
		t.Fatalf("regression missing: %+v %v", detail.Issue, err)
	}
}

func TestEnsureDefaultOwnerIsIdempotent(t *testing.T) {
	ctx := context.Background()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("goodsentry_default_owner_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(base)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, org, err := db.EnsureDefaultOwner(ctx, "owner@localhost", []byte("fixture-hash"), "GoodSentry Owner", "Default Organization")
	if err != nil {
		t.Fatal(err)
	}
	second, sameOrg, err := db.EnsureDefaultOwner(ctx, "owner@localhost", []byte("different-hash"), "Ignored Name", "Default Organization")
	if err != nil || second.ID != first.ID || second.Name != first.Name || !bytes.Equal(second.PasswordHash, first.PasswordHash) || sameOrg.ID != org.ID || sameOrg.Slug != "default-organization" {
		t.Fatalf("second owner=%+v org=%+v err=%v", second, sameOrg, err)
	}
	var users, orgs, teams, owners int
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM organizations),(SELECT count(*) FROM teams),(SELECT count(*) FROM memberships WHERE role='owner')`).Scan(&users, &orgs, &teams, &owners); err != nil {
		t.Fatal(err)
	}
	if users != 1 || orgs != 1 || teams != 1 || owners != 1 {
		t.Fatalf("implicit rows users=%d orgs=%d teams=%d owners=%d", users, orgs, teams, owners)
	}
	if _, err = db.Organization(ctx, first.ID, org.Slug); err != nil {
		t.Fatal(err)
	}
}
