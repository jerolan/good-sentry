//go:build integration

package jobs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"goodsentry/internal/event"
	"goodsentry/internal/jobs"
	"goodsentry/internal/store"

	"github.com/jackc/pgx/v5"
)

// Store tests own release selection. These cases run the actual maintenance
// goroutine against persisted policies, including its idle, stop and restart.
func TestIssueMaintenanceReleaseBinding(t *testing.T) {
	f := newResolutionWorkerFixture(t)
	t.Run("metadata creation after durable deferral and reopen", func(t *testing.T) {
		c := f.pending(t, "creation", false)
		before, beforeHistory := f.snapshot(t, f.db, c.issue.ID)
		stop := jobs.StartIssueMaintenance(f.ctx, f.db, time.Second)
		t.Cleanup(stop)
		var deferred time.Time
		f.wait(t, "pending policy was not durably deferred", func() bool {
			var moved bool
			if err := f.admin.QueryRow(f.ctx, `SELECT next_check_at,next_check_at>=set_at+interval '1 second' FROM issue_resolutions WHERE issue_id=$1::bigint`, c.issue.ID).Scan(&deferred, &moved); err != nil {
				t.Fatal(err)
			}
			return moved
		})
		stop()
		after, history := f.snapshot(t, f.db, c.issue.ID)
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeHistory, history) {
			t.Fatal("polling without a candidate changed public policy or history")
		}
		f.create(t, c)
		db := f.reopen(t)
		var persisted time.Time
		if err := f.admin.QueryRow(f.ctx, `SELECT next_check_at FROM issue_resolutions WHERE issue_id=$1::bigint`, c.issue.ID).Scan(&persisted); err != nil || !persisted.Equal(deferred) {
			t.Fatalf("deferred receipt did not survive reopen: %v %v", persisted, err)
		}
		after, history = f.snapshot(t, db, c.issue.ID)
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeHistory, history) {
			t.Fatal("metadata creation or reopen changed a pending policy without the worker")
		}
		stop2 := jobs.StartIssueMaintenance(f.ctx, db, time.Second)
		t.Cleanup(stop2)
		f.waitBound(t, db, c)
		stop2()
		f.assertOneCapture(t, c)
	})
	t.Run("finalization of existing metadata without capture", func(t *testing.T) {
		c := f.pending(t, "finalization", true)
		stop := jobs.StartIssueMaintenance(f.ctx, f.db, time.Second)
		t.Cleanup(stop)
		f.wait(t, "precreated unfinalized release did not leave a pending policy", func() bool {
			var deferred bool
			if err := f.admin.QueryRow(f.ctx, `SELECT release_id IS NULL AND next_check_at>set_at FROM issue_resolutions WHERE issue_id=$1::bigint`, c.issue.ID).Scan(&deferred); err != nil {
				t.Fatal(err)
			}
			return deferred
		})
		var receipt time.Time
		if err := f.admin.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.UpdateRelease(f.ctx, f.access(), c.version, "", store.ReleaseUpdate{DateReleased: store.ReleaseTime{Set: true, Value: &receipt}}); err != nil {
			t.Fatal(err)
		}
		f.waitBound(t, f.db, c)
		stop()
		f.assertOneCapture(t, c)
	})
	t.Run("blocked binding cancellation then restart drains mute first", func(t *testing.T) {
		c := f.pending(t, "cancellation", false)
		f.create(t, c)
		before, beforeHistory := f.snapshot(t, f.db, c.issue.ID)
		var beforePolicy []byte
		if err := f.admin.QueryRow(f.ctx, `SELECT row_to_json(r)::text FROM issue_resolutions r WHERE issue_id=$1::bigint`, c.issue.ID).Scan(&beforePolicy); err != nil {
			t.Fatal(err)
		}
		blocker, err := f.admin.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback(f.ctx)
		var pid int
		if err = blocker.QueryRow(f.ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			t.Fatal(err)
		}
		if _, err = blocker.Exec(f.ctx, "LOCK TABLE issue_activity IN ACCESS EXCLUSIVE MODE"); err != nil {
			t.Fatal(err)
		}
		stop := jobs.StartIssueMaintenance(f.ctx, f.db, time.Hour)
		t.Cleanup(stop)
		f.wait(t, "worker never reached the blocked binding activity", func() bool {
			var waiting bool
			if _, err := blocker.Exec(f.ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
				t.Fatal(err)
			}
			if err := blocker.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)))`, pid).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			return waiting
		})
		stopped := make(chan struct{})
		go func() { stop(); stop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			t.Fatal("stop did not cancel the blocked binding transaction")
		}
		if err = blocker.Rollback(f.ctx); err != nil {
			t.Fatal(err)
		}
		after, history := f.snapshot(t, f.db, c.issue.ID)
		var afterPolicy []byte
		if err = f.admin.QueryRow(f.ctx, `SELECT row_to_json(r)::text FROM issue_resolutions r WHERE issue_id=$1::bigint`, c.issue.ID).Scan(&afterPolicy); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeHistory, history) || string(beforePolicy) != string(afterPolicy) {
			t.Fatal("canceled binding partially persisted policy or activity")
		}
		mute := f.pending(t, "priority", false)
		minutes := int32(1)
		if _, err = f.db.UpdateIssue(f.ctx, f.owner.ID, mute.issue.ID, store.IssueUpdate{Status: "ignored", Ignore: &store.IssueIgnoreChange{Condition: &store.IssueIgnoreCondition{Minutes: &minutes}}}); err != nil {
			t.Fatal(err)
		}
		// Only the fixture deadline is elapsed; production owns both claims,
		// transaction commits and the observable order of their activity IDs.
		if _, err = f.admin.Exec(f.ctx, `UPDATE issue_ignores SET until=clock_timestamp()-interval '1 second' WHERE issue_id=$1::bigint`, mute.issue.ID); err != nil {
			t.Fatal(err)
		}
		db := f.reopen(t)
		stop2 := jobs.StartIssueMaintenance(f.ctx, db, time.Hour)
		t.Cleanup(stop2)
		boundActivity := f.waitBound(t, db, c)
		stop2()
		i, activities := f.snapshot(t, db, mute.issue.ID)
		if i.Status != "unresolved" || i.Ignore != nil || len(activities) != 4 || activities[0].Type != "unignored" || activities[0].User != nil {
			t.Fatalf("due mute was not drained before binding: %+v %+v", i, activities)
		}
		muteID, err := strconv.ParseInt(activities[0].ID, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		boundID, err := strconv.ParseInt(boundActivity.ID, 10, 64)
		if err != nil || muteID >= boundID {
			t.Fatalf("activity commit order mute=%d binding=%d err=%v", muteID, boundID, err)
		}
		f.assertOneCapture(t, c)
		f.assertOneCapture(t, mute)
	})
}

type resolutionWorkerFixture struct {
	ctx   context.Context
	db    *store.Store
	admin *pgx.Conn
	url   string
	owner store.User
	org   store.Organization
	next  int
}

type resolutionWorkerCase struct {
	project store.Project
	issue   store.Issue
	version store.ReleaseVersion
}

func newResolutionWorkerFixture(t *testing.T) *resolutionWorkerFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	schema := fmt.Sprintf("goodsentry_resolution_worker_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	if _, err = admin.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	owner, org, err := db.Bootstrap(ctx, "release-worker@example.test", []byte("fixture-hash"), "Owner", "Release workers")
	if err != nil {
		t.Fatal(err)
	}
	return &resolutionWorkerFixture{ctx: ctx, db: db, admin: admin, url: u.String(), owner: owner, org: org}
}

func (f *resolutionWorkerFixture) access() store.ManagementAccess {
	return store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
}

func (f *resolutionWorkerFixture) pending(t *testing.T, name string, precreate bool) resolutionWorkerCase {
	t.Helper()
	p, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, name, "go")
	if err != nil {
		t.Fatal(err)
	}
	f.next++
	raw := json.RawMessage(fmt.Sprintf(`{"event_id":"%032x","message":"%s worker","release":"%s@1.0.0"}`, f.next, name, name))
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id, fresh, err := f.db.Ingest(f.ctx, p.ID, ev)
	if err != nil || !fresh {
		t.Fatalf("capture fresh=%v err=%v", fresh, err)
	}
	c := resolutionWorkerCase{project: p, version: store.ReleaseVersion(name + "@2.0.0")}
	if precreate {
		f.create(t, c)
	}
	c.issue, err = f.db.UpdateIssue(f.ctx, f.owner.ID, id, store.IssueUpdate{Status: "resolved", Resolution: &store.IssueResolutionChange{Policy: &store.IssueResolutionInput{Mode: "next_release"}}})
	if err != nil || c.issue.Resolution == nil || c.issue.Resolution.Release != nil || c.issue.Resolution.Baseline == nil {
		t.Fatalf("pending next-release policy: %+v err=%v", c.issue, err)
	}
	return c
}

func (f *resolutionWorkerFixture) create(t *testing.T, c resolutionWorkerCase) {
	t.Helper()
	if _, fresh, err := f.db.CreateRelease(f.ctx, f.access(), store.ReleaseCreate{Version: c.version, Projects: []string{c.project.ID}}); err != nil || !fresh {
		t.Fatalf("metadata release fresh=%v err=%v", fresh, err)
	}
}

func (f *resolutionWorkerFixture) reopen(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(f.ctx, f.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func (f *resolutionWorkerFixture) snapshot(t *testing.T, db *store.Store, id string) (store.Issue, []store.IssueActivity) {
	t.Helper()
	i, err := db.Issue(f.ctx, f.owner.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := db.IssueActivities(f.ctx, f.owner.ID, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	return i, a
}

func (f *resolutionWorkerFixture) wait(t *testing.T, message string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ready() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *resolutionWorkerFixture) waitBound(t *testing.T, db *store.Store, c resolutionWorkerCase) store.IssueActivity {
	t.Helper()
	var bound store.IssueActivity
	f.wait(t, "worker did not bind metadata without a capture", func() bool {
		i, a := f.snapshot(t, db, c.issue.ID)
		if i.Resolution == nil || i.Resolution.Release == nil {
			return false
		}
		if i.Status != "resolved" || i.Count != 1 || i.ResolvedAt == nil || !i.ResolvedAt.Equal(*c.issue.ResolvedAt) || i.RegressedAt != nil || i.Resolution.Release.Version != c.version || !reflect.DeepEqual(i.Resolution.Baseline, c.issue.Resolution.Baseline) {
			t.Fatalf("binding changed issue lifetime or selected wrong metadata: %+v", i)
		}
		if len(a) != 3 || a[0].Type != "resolution_bound" || a[0].User != nil {
			t.Fatalf("binding must record exactly one system activity: %+v", a)
		}
		var data struct {
			Resolution *store.IssueResolution `json:"resolution"`
		}
		if err := json.Unmarshal(a[0].Data, &data); err != nil || !reflect.DeepEqual(data.Resolution, i.Resolution) {
			t.Fatalf("binding activity/reference mismatch: %s err=%v", a[0].Data, err)
		}
		bound = a[0]
		return true
	})
	return bound
}

func (f *resolutionWorkerFixture) assertOneCapture(t *testing.T, c resolutionWorkerCase) {
	t.Helper()
	var count int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM events WHERE project_id=$1::bigint", c.project.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("metadata maintenance changed captures: count=%d err=%v", count, err)
	}
}
