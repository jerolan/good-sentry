//go:build integration

package store

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/uptime"
)

func monitorConfig(name string) uptime.Config {
	c := uptime.Defaults()
	c.Name, c.Type, c.URL = name, uptime.GET, "https://example.test/health"
	return c
}

func dueMonitor(t *testing.T, ctx context.Context, db *Store, id string) uptime.Lease {
	t.Helper()
	if _, err := db.pool.Exec(ctx, "UPDATE monitors SET next_check=clock_timestamp()-interval '1 second' WHERE id::text=$1", id); err != nil {
		t.Fatal(err)
	}
	lease, err := db.ClaimMonitor(ctx)
	if err != nil || lease.ID != id {
		t.Fatalf("claim monitor %s: %+v, %v", id, lease, err)
	}
	return lease
}

func TestMonitorThresholdResetRecoveryAndHistory(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	c := monitorConfig("Checkout")
	c.Project, c.ConfirmationThreshold = project.Slug, 2
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil || m.Status != uptime.Unknown || m.ConsecutiveFailures != 0 || m.LastCheck != nil || m.Project != project.Slug {
		t.Fatalf("new monitor state: %+v, %v", m, err)
	}
	for i, want := range []struct {
		up               bool
		status, previous uptime.Status
		change           bool
		failures         int
	}{
		{false, uptime.Unknown, uptime.Unknown, false, 1},
		{false, uptime.Down, uptime.Unknown, true, 2},
		{true, uptime.Up, uptime.Down, true, 0},
		{false, uptime.Up, uptime.Up, false, 1},
	} {
		lease := dueMonitor(t, ctx, db, m.ID)
		latency := int64(12)
		code, reason := 200, "ok"
		if !want.up {
			code, reason = 503, "unexpected_status"
		}
		check, err := db.CompleteMonitor(ctx, lease, uptime.Result{Up: want.up, ResponseTimeMS: &latency, StatusCode: &code, Reason: reason})
		if err != nil || check.Up != want.up || check.Status != want.status || check.PreviousStatus != want.previous || check.IsChange != want.change || check.ResponseTimeMS == nil || *check.ResponseTimeMS != 12 || check.StatusCode == nil || *check.StatusCode != code || check.Reason != reason || check.StartedAt.After(check.CompletedAt) {
			t.Fatalf("check %d: %+v, %v", i, check, err)
		}
		m, err = db.Monitor(ctx, owner.ID, org.Slug, m.ID)
		if err != nil || m.ConsecutiveFailures != want.failures || m.Status != want.status || m.LastCheck == nil || !m.LastCheck.Equal(check.CompletedAt) || m.NextCheck.Sub(check.CompletedAt) != 60*time.Second {
			t.Fatalf("monitor after check %d: %+v, %v", i, m, err)
		}
	}
	changes, more, err := db.MonitorChecks(ctx, owner.ID, org.Slug, m.ID, true, 1)
	if err != nil || len(changes) != 2 || more || changes[0].Status != uptime.Up || changes[1].Status != uptime.Down {
		t.Fatalf("transitions: %+v more=%v, %v", changes, more, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE monitors SET consecutive_failures=99 WHERE id::text=$1", m.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		lease := dueMonitor(t, ctx, db, m.ID)
		if _, err := db.CompleteMonitor(ctx, lease, uptime.Result{Reason: "connection_error"}); err != nil {
			t.Fatal(err)
		}
	}
	m, err = db.Monitor(ctx, owner.ID, org.Slug, m.ID)
	if err != nil || m.ConsecutiveFailures != 100 {
		t.Fatalf("failure counter did not saturate: %+v, %v", m, err)
	}
	c.Name = "Edited after failures"
	updated, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, c)
	if err != nil || updated.Status != uptime.Down || updated.ConsecutiveFailures != 0 || updated.LastCheck == nil || !updated.LastCheck.Equal(*m.LastCheck) || updated.LastChange == nil || !updated.LastChange.Equal(*m.LastChange) {
		t.Fatalf("configuration replacement lost state/history or kept failures: %+v, %v", updated, err)
	}
}

func TestMonitorClaimsFenceUpdatesPauseAndExpiredLeases(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	c := monitorConfig("Claim race")
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	leases, errs := make(chan uptime.Lease, 8), make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			lease, err := db.ClaimMonitor(ctx)
			leases <- lease
			errs <- err
		})
	}
	wg.Wait()
	close(leases)
	close(errs)
	winners, missing := 0, 0
	var first uptime.Lease
	for lease := range leases {
		if lease.Token != "" {
			first = lease
			winners++
		}
	}
	for err := range errs {
		if errors.Is(err, ErrNotFound) {
			missing++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if winners != 1 || missing != 7 || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(first.Token) || first.ExpiresAt.Sub(first.StartedAt) != 50*time.Second {
		t.Fatalf("claim race winners=%d missing=%d lease=%+v", winners, missing, first)
	}
	c.Enabled = false
	if _, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteMonitor(ctx, first, uptime.Result{Up: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("paused lease completed: %v", err)
	}
	if _, err := db.ClaimMonitor(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("paused monitor claimed: %v", err)
	}
	c.Enabled = true
	if _, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, c); err != nil {
		t.Fatal(err)
	}
	second := dueMonitor(t, ctx, db, m.ID)
	// The old lease has not timed out, but its token was superseded. Testing
	// while a new lease is active distinguishes fencing from an empty-token guard.
	if _, err := db.CompleteMonitor(ctx, first, uptime.Result{Up: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("old token completed under a newer active lease: %v", err)
	}
	c.Name = "Edited"
	if _, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteMonitor(ctx, second, uptime.Result{Up: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("replaced configuration lease completed: %v", err)
	}
	expired := dueMonitor(t, ctx, db, m.ID)
	if _, err := db.pool.Exec(ctx, "UPDATE monitors SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id::text=$1", m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteMonitor(ctx, expired, uptime.Result{Up: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired lease completed: %v", err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	recovered, err := reopened.ClaimMonitor(ctx)
	if err != nil || recovered.ID != m.ID || recovered.Token == expired.Token || recovered.Token == first.Token {
		t.Fatalf("restart did not recover expired lease: %+v, %v", recovered, err)
	}
	if _, err := reopened.CompleteMonitor(ctx, recovered, uptime.Result{Up: true, Reason: "ok"}); err != nil {
		t.Fatal(err)
	}
	c.Type, c.URL = uptime.Heartbeat, ""
	if _, err := reopened.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, c); !errors.Is(err, ErrConflict) {
		t.Fatalf("monitor changed kind: %v", err)
	}
}

func TestHeartbeatReceiptFencesMissedCheckAndRearms(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	c := uptime.Defaults()
	c.Name, c.Type, c.IntervalSeconds, c.ConfirmationThreshold = "Daily import", uptime.Heartbeat, 5, 2
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(m.HeartbeatKey) || m.NextCheck.Sub(m.CreatedAt) != 5*time.Second {
		t.Fatalf("heartbeat creation/deadline: %+v, %v", m, err)
	}
	if _, err := db.ClaimMonitor(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new heartbeat claimed before deadline: %v", err)
	}
	lease := dueMonitor(t, ctx, db, m.ID)
	check, err := db.Heartbeat(ctx, m.HeartbeatKey)
	if err != nil || !check.Up || check.Status != uptime.Up || !check.IsChange || check.Reason != "heartbeat_received" || check.ResponseTimeMS != nil || check.StatusCode != nil {
		t.Fatalf("heartbeat receipt: %+v, %v", check, err)
	}
	if _, err := db.CompleteMonitor(ctx, lease, uptime.Result{Reason: "heartbeat_missed"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale expiry overwrote fresh heartbeat: %v", err)
	}
	c.Enabled = false
	if _, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Heartbeat(ctx, m.HeartbeatKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("paused heartbeat wrote history: %v", err)
	}
	c.Enabled = true
	resumed, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, c)
	if err != nil || resumed.Status != uptime.Up || resumed.ConsecutiveFailures != 0 {
		t.Fatalf("resume lost state: %+v, %v", resumed, err)
	}
	if _, err := db.ClaimMonitor(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resumed heartbeat claimed before new interval: %v", err)
	}
	for i, status := range []uptime.Status{uptime.Up, uptime.Down} {
		lease := dueMonitor(t, ctx, db, m.ID)
		check, err := db.CompleteMonitor(ctx, lease, uptime.Result{Reason: "heartbeat_missed"})
		if err != nil || check.Up || check.Status != status {
			t.Fatalf("missed heartbeat %d: %+v, %v", i, check, err)
		}
	}
	check, err = db.Heartbeat(ctx, m.HeartbeatKey)
	if err != nil || check.PreviousStatus != uptime.Down || check.Status != uptime.Up || !check.IsChange {
		t.Fatalf("heartbeat recovery: %+v, %v", check, err)
	}
	current, err := db.Monitor(ctx, owner.ID, org.Slug, m.ID)
	if err != nil || current.HeartbeatKey != m.HeartbeatKey || current.NextCheck.Sub(check.CompletedAt) != 5*time.Second || current.ConsecutiveFailures != 0 {
		t.Fatalf("heartbeat deadline/reset: %+v, %v", current, err)
	}
	history, more, err := db.MonitorChecks(ctx, owner.ID, org.Slug, m.ID, false, 1)
	if err != nil || len(history) != 4 || more {
		t.Fatalf("pause or stale expiry added history: count=%d more=%v, %v", len(history), more, err)
	}
}

func TestMonitorMembershipProjectScopeAndHistoryPagination(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	var member, outsider, otherOrg string
	for _, identity := range []struct {
		email       string
		destination *string
	}{{"member@monitor.test", &member}, {"outsider@monitor.test", &outsider}} {
		if err := db.pool.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,'Monitor user') RETURNING id::text", identity.email, []byte("already-hashed")).Scan(identity.destination); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, member); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('other-monitors','Other monitors') RETURNING id::text").Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", otherOrg, outsider); err != nil {
		t.Fatal(err)
	}
	otherProject, err := db.CreateProject(ctx, outsider, "other-monitors", "Private service", "go")
	if err != nil {
		t.Fatal(err)
	}
	c := monitorConfig("Private")
	c.Type, c.URL = uptime.SSL, "https://Certificates.Example.test:8443/"
	c.Project, c.Enabled = otherProject.Slug, false
	other, err := db.CreateMonitor(ctx, outsider, "other-monitors", c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign project attached to monitor: %v", err)
	}
	c.Project, c.Name, c.Enabled = project.Slug, "Public metadata", true
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMonitor(ctx, member, org.Slug, c); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member created monitor: %v", err)
	}
	if _, err := db.UpdateMonitor(ctx, member, org.Slug, m.ID, c); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member updated monitor: %v", err)
	}
	if err := db.DeleteMonitor(ctx, member, org.Slug, m.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member deleted monitor: %v", err)
	}
	if items, err := db.Monitors(ctx, member, org.Slug); err != nil || len(items) != 1 || items[0].ID != m.ID {
		t.Fatalf("member read/org list scope: %+v, %v", items, err)
	}
	if _, err := db.Monitor(ctx, owner.ID, org.Slug, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign monitor read: %v", err)
	}
	if _, err := db.Monitor(ctx, owner.ID, org.Slug, "not-a-number"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("opaque monitor ID became a database error: %v", err)
	}
	if _, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, other.ID, c); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign monitor update: %v", err)
	}
	if err := db.DeleteMonitor(ctx, owner.ID, org.Slug, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign monitor delete: %v", err)
	}
	foreignProject := c
	foreignProject.Project = otherProject.Slug
	if _, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, foreignProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign project attached by update: %v", err)
	}
	if _, err := db.Monitors(ctx, outsider, org.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign identity listed monitors: %v", err)
	}
	for i := range 29 {
		lease := dueMonitor(t, ctx, db, m.ID)
		up := i%2 == 0 && i != 28
		if _, err := db.CompleteMonitor(ctx, lease, uptime.Result{Up: up, Reason: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	first, more, err := db.MonitorChecks(ctx, member, org.Slug, m.ID, false, 1)
	if err != nil || len(first) != 25 || !more {
		t.Fatalf("first history page count=%d more=%v, %v", len(first), more, err)
	}
	second, more, err := db.MonitorChecks(ctx, member, org.Slug, m.ID, false, 2)
	if err != nil || len(second) != 4 || more || first[len(first)-1].ID == second[0].ID {
		t.Fatalf("second history page count=%d more=%v, %v", len(second), more, err)
	}
	changes, more, err := db.MonitorChecks(ctx, member, org.Slug, m.ID, true, 2)
	if err != nil || len(changes) != 3 || more {
		t.Fatalf("transition page count=%d more=%v, %v", len(changes), more, err)
	}
	if _, _, err := db.MonitorChecks(ctx, outsider, org.Slug, m.ID, false, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign history read: %v", err)
	}
	stale := dueMonitor(t, ctx, db, m.ID)
	if err := db.DeleteMonitor(ctx, owner.ID, org.Slug, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteMonitor(ctx, stale, uptime.Result{Up: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("deleted monitor lease completed: %v", err)
	}
	var remaining int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM monitor_checks WHERE monitor_id::text=$1", m.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleted monitor history remained=%d, %v", remaining, err)
	}
	if _, err := db.Heartbeat(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("network monitor accepted heartbeat key: %v", err)
	}
}
