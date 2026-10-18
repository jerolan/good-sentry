//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Real SQL owns current authority and numeric-slug/key containment. Existing
// report persistence tests have no caller grants, and HTTP cannot establish
// row-lock ordering. No mock authority, clock or production test hook is used.
func TestUserReportPathWritersRequireCurrentAuthority(t *testing.T) {
	f := resolutionStore(t)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "go")
	if err != nil {
		t.Fatal(err)
	}
	member := triageMember(t, f.ctx, f.db, f.org, "report-member", "member")
	access := ManagementAccess{UserID: member.ID, OrganizationID: f.org.ID, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}}
	input := UserReportInput{EventID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Comments: "Member feedback"}
	first, err := f.db.SubmitManagedUserReport(f.ctx, access, f.project.Slug, input)
	if err != nil || first.ID == "" {
		t.Fatal("ordinary current member cannot submit", first, err)
	}
	input.Comments = "Same current report through numeric scoped reference"
	again, err := f.db.SubmitManagedUserReport(f.ctx, access, f.project.ID, input)
	if err != nil || again.ID != first.ID || !again.DateCreated.Equal(first.DateCreated) {
		t.Fatal("hidden numeric slug shadowed permitted ID", again, err)
	}
	input.Comments = "DSN path update"
	for _, ref := range []struct{ org, project string }{{f.org.Slug, f.project.Slug}, {f.org.ID, f.project.ID}} {
		report, err := f.db.SubmitDSNUserReport(f.ctx, ref.org, ref.project, f.project.PublicKey, input)
		if err != nil || report.ID != first.ID || !report.DateCreated.Equal(first.DateCreated) {
			t.Fatal("DSN writer selected a different receipt", report, err)
		}
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	readOnly := access
	readOnly.ReadOnly = true
	if _, err := f.db.SubmitManagedUserReport(f.ctx, readOnly, f.project.ID, input); !errors.Is(err, ErrForbidden) {
		t.Fatal("read grant submitted feedback", err)
	}
	if _, err := f.db.SubmitManagedUserReport(f.ctx, access, other.ID, input); !errors.Is(err, ErrNotFound) {
		t.Fatal("restricted grant selected hidden project", err)
	}
	empty := access
	empty.ProjectIDs = nil
	if _, err := f.db.SubmitManagedUserReport(f.ctx, empty, f.project.Slug, input); !errors.Is(err, ErrNotFound) {
		t.Fatal("empty restricted grant became unrestricted", err)
	}
	for _, ref := range []struct{ org, project, key string }{{f.org.Slug, other.ID, f.project.PublicKey}, {"foreign-org", f.project.ID, f.project.PublicKey}, {f.org.Slug, f.project.Slug, other.PublicKey}, {f.org.Slug, f.project.ID, "wrong"}} {
		if _, err := f.db.SubmitDSNUserReport(f.ctx, ref.org, ref.project, ref.key, input); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign SDK authority selected a project", ref.org, ref.project, err)
		}
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, err := f.db.pool.Exec(f.ctx, `DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`, f.org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE project_keys SET enabled=false WHERE public_key=$1`, f.project.PublicKey); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	if _, err := f.db.SubmitManagedUserReport(f.ctx, access, f.project.Slug, input); !errors.Is(err, ErrNotFound) {
		t.Fatal("removed member submitted feedback", err)
	}
	if _, err := f.db.SubmitDSNUserReport(f.ctx, f.org.Slug, f.project.Slug, f.project.PublicKey, input); !errors.Is(err, ErrNotFound) {
		t.Fatal("disabled key submitted feedback", err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestUserReportPathAuthorityLocksAndCancellation(t *testing.T) {
	for _, kind := range []string{"member", "key"} {
		for _, cancelWriter := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%v", kind, cancelWriter), func(t *testing.T) {
				f := resolutionStore(t)
				member := triageMember(t, f.ctx, f.db, f.org, "locked-report-member", "member")
				access := ManagementAccess{UserID: member.ID, OrganizationID: f.org.ID, AllProjects: true}
				input := UserReportInput{EventID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Comments: "After authority and receipt gates"}
				barrier, pid := captureItemsBarrier(t, f)
				if _, err := barrier.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, f.project.ID+":"+input.EventID); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				type result struct {
					report UserReport
					err    error
				}
				done := make(chan result, 1)
				go func() {
					var report UserReport
					var err error
					if kind == "member" {
						report, err = f.db.SubmitManagedUserReport(ctx, access, f.project.Slug, input)
					} else {
						report, err = f.db.SubmitDSNUserReport(ctx, f.org.Slug, f.project.Slug, f.project.PublicKey, input)
					}
					done <- result{report, err}
				}()
				writerPID := groupWaitBlocked(t, f.ctx, barrier, pid, 1)[0]
				revoked := make(chan error, 1)
				go func() {
					var err error
					if kind == "member" {
						_, err = f.db.pool.Exec(f.ctx, `DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`, f.org.ID, member.ID)
					} else {
						_, err = f.db.pool.Exec(f.ctx, `UPDATE project_keys SET enabled=false WHERE public_key=$1`, f.project.PublicKey)
					}
					revoked <- err
				}()
				groupWaitBlocked(t, f.ctx, barrier, writerPID, 1)
				var released time.Time
				if err := barrier.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&released); err != nil {
					t.Fatal(err)
				}
				if cancelWriter {
					cancel()
				} else if err := barrier.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
				got := <-done
				if cancelWriter {
					if got.err == nil || got.report.ID != "" {
						t.Fatal("canceled path writer acknowledged a report", got)
					}
				} else if got.err != nil || got.report.DateCreated.Before(released) {
					t.Fatal("path writer stamped before its receipt gate", got, released)
				}
				deleteOrderAwait(t, f.ctx, revoked)
				if cancelWriter {
					if err := barrier.Rollback(f.ctx); err != nil {
						t.Fatal(err)
					}
				}
				var count int
				want := 1
				if cancelWriter {
					want = 0
				}
				if err := f.db.pool.QueryRow(f.ctx, `SELECT count(*) FROM user_reports`).Scan(&count); err != nil || count != want {
					t.Fatal("authority cancellation left durable feedback", count, want, err)
				}
			})
		}
	}
}

func TestUserReportScopeMigration030PreservesOldGrants(t *testing.T) {
	f := resolutionStore(t)
	apiTokenFixture(t, f.ctx, f.db, f.owner.ID, f.org.Slug, "before030", nil)
	if _, err := f.db.SubmitUserReport(f.ctx, f.project.ID, UserReportInput{EventID: "cccccccccccccccccccccccccccccccc", Comments: "Before scope migration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
 ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK(cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases','event:read','event:write','event:admin']::text[]);
 DELETE FROM schema_migrations WHERE version=30`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 29)
	for range 2 {
		reopened, err := Open(f.ctx, f.db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		if after := groupDatabaseSnapshot(t, f.ctx, reopened, 29); !reflect.DeepEqual(before, after) {
			t.Fatal("030 widened old grants or rewrote prior receipts/rows")
		}
		var receipt string
		if err := reopened.pool.QueryRow(f.ctx, `SELECT checksum FROM schema_migrations WHERE version=30`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/030_user_report_scope.sql")
		if err != nil || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatal("030 receipt mismatch", err)
		}
		reopened.Close()
	}
	_, hash, prefix := apiTokenCredentials("feedback-only030")
	if _, err := f.db.CreateAPIToken(f.ctx, f.owner.ID, f.org.Slug, APITokenInput{Name: "Feedback only", Scopes: []string{"project:write"}, ExpiresInDays: 90}, hash, prefix); err != nil {
		t.Fatal("new opt-in scope unavailable", err)
	}
	grant, err := f.db.AuthenticateAPIToken(f.ctx, hash)
	if err != nil || !reflect.DeepEqual(grant.Scopes, []string{"project:write"}) {
		t.Fatal("feedback scope gained unrelated reads/writes", grant.Scopes, err)
	}
}
