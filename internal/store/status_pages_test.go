//go:build integration

package store

import (
	"errors"
	"testing"
	"time"

	"goodsentry/internal/uptime"
)

func TestStatusPagesAssociateMonitorsAndPersist(t *testing.T) {
	ctx, db, owner, org, _ := telemetryStore(t)
	up, err := db.CreateMonitor(ctx, owner.ID, org.Slug, uptime.Config{
		Name: "Public API", Type: uptime.HEAD, URL: "https://api.example.test", IntervalSeconds: 60,
		TimeoutSeconds: 10, ExpectedStatus: 200, ConfirmationThreshold: 1, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	down, err := db.CreateMonitor(ctx, owner.ID, org.Slug, uptime.Config{
		Name: "Worker", Type: uptime.TCP, URL: "worker.example.test:443", IntervalSeconds: 60,
		TimeoutSeconds: 10, ExpectedStatus: 200, ConfirmationThreshold: 1, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE monitors SET status='down',last_check=$2 WHERE id::text=$1`, down.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	page, err := db.CreateStatusPage(ctx, owner.ID, org.Slug, "  Public status  ", true, []string{up.ID, down.ID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Name != "Public status" || page.Slug == "" || !page.IsPublic || len(page.Monitors) != 2 {
		t.Fatalf("created status page: %+v", page)
	}
	pages, err := db.StatusPages(ctx, owner.ID, org.Slug)
	if err != nil || len(pages) != 1 || pages[0].ID != page.ID || len(pages[0].Monitors) != 2 {
		t.Fatalf("status pages=%+v err=%v", pages, err)
	}
	public, err := db.PublicStatusPage(ctx, page.Slug)
	if err != nil || public.ID != page.ID || len(public.Monitors) != 2 {
		t.Fatalf("public page=%+v err=%v", public, err)
	}
	updated, err := db.UpdateStatusPage(ctx, owner.ID, org.Slug, page.Slug, "Service health", false, []string{up.ID})
	if err != nil || updated.IsPublic || len(updated.Monitors) != 1 || updated.Monitors[0].ID != up.ID {
		t.Fatalf("updated status page=%+v err=%v", updated, err)
	}
	if _, err = db.PublicStatusPage(ctx, page.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private status page public read=%v", err)
	}
	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.StatusPages(ctx, owner.ID, org.Slug)
	if err != nil || len(persisted) != 1 || persisted[0].Name != "Service health" || persisted[0].IsPublic || len(persisted[0].Monitors) != 1 {
		t.Fatalf("reopened status pages=%+v err=%v", persisted, err)
	}
	if err = reopened.DeleteStatusPage(ctx, owner.ID, org.Slug, page.Slug); err != nil {
		t.Fatal(err)
	}
	if pages, err = reopened.StatusPages(ctx, owner.ID, org.Slug); err != nil || len(pages) != 0 {
		t.Fatalf("deleted status pages=%+v err=%v", pages, err)
	}
}
