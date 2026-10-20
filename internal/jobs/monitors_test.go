//go:build integration

package jobs_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/jobs"
	"goodsentry/internal/store"
	"goodsentry/internal/uptime"
)

// The running worker boundary owns actual probe-to-durable-history wiring,
// cross-pool coordination, restart and cancellation. Pure probe and SQL tests
// cannot detect a scheduler that never runs or records shutdown as an outage.
// Use real PostgreSQL, HTTP and production start/stop, without clock injection.
func TestMonitorWorkersProbeRecoverAndStop(t *testing.T) {
	ctx := context.Background()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL required")
	}
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	schema := fmt.Sprintf("goodsentry_workers_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db2, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	owner, org, err := db.Bootstrap(ctx, "worker@example.test", []byte("unused-fixture-hash"), "Owner", "Workers")
	if err != nil {
		t.Fatal(err)
	}
	var status, requests atomic.Int32
	status.Store(503)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	defer target.Close()
	c := uptime.Defaults()
	c.Name, c.Type, c.URL, c.IntervalSeconds = "HTTP recovery", uptime.GET, target.URL, 86400
	m, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	stop1 := jobs.StartMonitors(ctx, db, 1, true)
	defer stop1()
	stop2 := jobs.StartMonitors(ctx, db2, 1, true)
	defer stop2()
	waitStatus := func(id string, want uptime.Status) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			got, err := db.Monitor(ctx, owner.ID, org.Slug, id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("worker did not record monitor %s as %s", id, want)
	}
	waitStatus(m.ID, uptime.Down)
	status.Store(200)
	if _, err := db.UpdateMonitor(ctx, owner.ID, org.Slug, m.ID, c); err != nil {
		t.Fatal(err)
	}
	waitStatus(m.ID, uptime.Up)
	sslTarget := httptest.NewTLSServer(http.NotFoundHandler())
	defer sslTarget.Close()
	sslConfig := uptime.Defaults()
	sslConfig.Name, sslConfig.Type, sslConfig.URL, sslConfig.IntervalSeconds = "Untrusted SSL certificate", uptime.SSL, sslTarget.URL, 86400
	sslMonitor, err := db.CreateMonitor(ctx, owner.ID, org.Slug, sslConfig)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(sslMonitor.ID, uptime.Down)
	stop1()
	stop2()
	history, _, err := db.MonitorChecks(ctx, owner.ID, org.Slug, m.ID, true, 1)
	if err != nil || len(history) != 2 || history[0].PreviousStatus != uptime.Down || history[0].Status != uptime.Up || history[0].StatusCode == nil || *history[0].StatusCode != 200 || history[1].Reason != "unexpected_status" || requests.Load() != 2 {
		t.Fatalf("two pools produced incorrect checks: requests=%d history=%+v err=%v", requests.Load(), history, err)
	}
	// A new store and worker must keep existing history and process new work.
	db3, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db3.Close()
	history, _, err = db3.MonitorChecks(ctx, owner.ID, org.Slug, m.ID, false, 1)
	if err != nil || len(history) != 2 {
		t.Fatalf("restart lost checks: %+v %v", history, err)
	}
	history, _, err = db3.MonitorChecks(ctx, owner.ID, org.Slug, sslMonitor.ID, false, 1)
	if err != nil || len(history) != 1 {
		t.Fatalf("restart lost SSL worker check: %+v %v", history, err)
	}
	sslCheck := history[0]
	if sslCheck.Up || sslCheck.Status != uptime.Down || sslCheck.Reason != "tls_error" || sslCheck.StatusCode != nil || sslCheck.ResponseTimeMS == nil || *sslCheck.ResponseTimeMS < 0 {
		t.Fatalf("SSL worker recorded incorrect check: %+v", sslCheck)
	}
	started := make(chan struct{}, 1)
	cancelTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer cancelTarget.Close()
	c.Name, c.URL, c.TimeoutSeconds = "Shutdown is not an outage", cancelTarget.URL, 60
	canceled, err := db.CreateMonitor(ctx, owner.ID, org.Slug, c)
	if err != nil {
		t.Fatal(err)
	}
	stop3 := jobs.StartMonitors(ctx, db3, 1, true)
	defer stop3()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("restarted worker did not probe")
	}
	stopped := make(chan struct{})
	go func() { stop3(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel active probe")
	}
	history, _, err = db.MonitorChecks(ctx, owner.ID, org.Slug, canceled.ID, false, 1)
	if err != nil || len(history) != 0 {
		t.Fatalf("shutdown recorded an outage: %+v %v", history, err)
	}
}
