//go:build integration

package store

import (
	"testing"
	"time"
)

func TestProjectKeyRateAdmissionAndWindowReset(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, Role: "owner", AllProjects: true}
	keys, err := db.CompatibleProjectKeys(ctx, access, project.ID, nil, 0, 100)
	if err != nil || len(keys) != 1 {
		t.Fatalf("initial key: %+v %v", keys, err)
	}
	limited, err := db.UpdateCompatibleProjectKey(ctx, access, project.ID, keys[0].ID, "limited", &CompatibleKeyRateLimit{Window: 60, Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		charge int
		want   bool
	}{{2, true}, {1, true}, {1, false}, {3, false}} {
		allowed, retry, err := db.AdmitProjectKey(ctx, project.ID, limited.Public, step.charge)
		if err != nil || allowed != step.want || !step.want && retry < 1 {
			t.Fatalf("rate charge %d: allowed=%v retry=%d err=%v", step.charge, allowed, retry, err)
		}
	}
	var used int
	if err := db.pool.QueryRow(ctx, `SELECT count FROM project_key_rate_usage WHERE api_id=$1::uuid`, limited.ID).Scan(&used); err != nil || used != 3 {
		t.Fatalf("persisted usage=%d err=%v", used, err)
	}
	unlimited, err := db.CreateCompatibleProjectKey(ctx, access, project.ID, "unlimited", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if allowed, _, err := db.AdmitProjectKey(ctx, project.ID, unlimited.Public, 1000); err != nil || !allowed {
			t.Fatalf("unlimited key denied: %v %v", allowed, err)
		}
	}
	short, err := db.CreateCompatibleProjectKey(ctx, access, project.ID, "short window", &CompatibleKeyRateLimit{Window: 1, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if allowed, _, err := db.AdmitProjectKey(ctx, project.ID, short.Public, 1); err != nil || !allowed {
		t.Fatalf("short first: %v %v", allowed, err)
	}
	if allowed, retry, err := db.AdmitProjectKey(ctx, project.ID, short.Public, 1); err != nil || allowed || retry < 1 {
		t.Fatalf("short denied: %v %d %v", allowed, retry, err)
	}
	time.Sleep(1100 * time.Millisecond)
	if allowed, _, err := db.AdmitProjectKey(ctx, project.ID, short.Public, 1); err != nil || !allowed {
		t.Fatalf("short window did not reset: %v %v", allowed, err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO project_key_rate_usage(api_id,window_start,count)
  VALUES($1::uuid,clock_timestamp()-interval '15 days',9)`, limited.ID); err != nil {
		t.Fatal(err)
	}
	removed, err := db.CleanupProjectKeyRateUsage(ctx, 14*24*time.Hour, 250)
	if err != nil || removed != 1 {
		t.Fatalf("rate retention removed=%d err=%v", removed, err)
	}
	var currentRows int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM project_key_rate_usage WHERE api_id=$1::uuid`, limited.ID).Scan(&currentRows); err != nil || currentRows != 1 {
		t.Fatalf("rate retention removed current window: rows=%d err=%v", currentRows, err)
	}
	if _, err := db.CleanupProjectKeyRateUsage(ctx, 0, 1); err == nil {
		t.Fatal("invalid zero retention accepted")
	}
}
