//go:build integration

package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Automatic expiry owns a different boundary from manual deletion: release
// metadata cascades, while file bytes and historical policy snapshots survive.
// Real Store writers/readers make a wrong owner or receipt clock observable.
// Owned SQL ages receipts only; no production hook or fake store is needed.
func TestReleaseRetentionPreservesFilesTelemetryAndPolicySnapshots(t *testing.T) {
	f := resolutionStore(t)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Shared release", "go")
	if err != nil {
		t.Fatal(err)
	}
	commits := []ReleaseCommitInput{{ID: "receipt-proof", Repository: "retained/catalog", Data: json.RawMessage(`{"id":"receipt-proof","repository":"retained/catalog"}`)}}
	old, _, err := f.db.CreateRelease(f.ctx, f.access, ReleaseCreate{Version: "receipt@1.0.0", Projects: []string{f.project.ID, other.ID}, ReleaseUpdate: ReleaseUpdate{Commits: &commits}})
	if err != nil {
		t.Fatal(err)
	}
	open := f.release(t, "open@1.0.0")
	kept := f.release(t, "kept@1.0.0")
	future := time.Now().Add(400 * 24 * time.Hour)
	ancient := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	archived := "archived"
	if _, err := f.db.UpdateRelease(f.ctx, f.access, old.Version, "", ReleaseUpdate{Status: &archived, DateReleased: ReleaseTime{Set: true, Value: &future}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.UpdateRelease(f.ctx, f.access, kept.Version, "", ReleaseUpdate{DateReleased: ReleaseTime{Set: true, Value: &ancient}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateReleaseDeploy(f.ctx, f.access, old.Version, ReleaseDeployInput{Environment: "production", Projects: []string{f.project.ID, other.ID}, DateFinished: &future}); err != nil {
		t.Fatal(err)
	}
	mapMeta, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/retained.map", string(old.Version), "web", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", emptySourceMap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, alertConfig("Release history")); err != nil {
		t.Fatal(err)
	}
	ev := groupEvent(t, 1, "selected-release", "Selected snapshot", time.Now().Add(-500*24*time.Hour))
	ev.Release = string(old.Version)
	id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "still-here.bin", Data: []byte{0, 255, 1}}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "profile", Header: json.RawMessage(`{"type":"profile"}`), Data: []byte{255, 0}}}})
	if err != nil || !fresh {
		t.Fatal(id, fresh, err)
	}
	selected := f.resolve(t, id, "release", old.Version)
	if _, err := f.db.AddComment(f.ctx, f.owner.ID, id, "Keep release history"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, id, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: f.owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	pendingEvent := groupEvent(t, 2, "pending-release", "Pending snapshot", time.Now())
	pendingEvent.Release = string(open.Version)
	pendingID := groupCapture(t, f.ctx, f.db, f.project, pendingEvent)
	pending := f.resolve(t, pendingID, "next_release", "")
	span := spanFixture("1234567890abcdef", true, 1, 2)
	span["attributes"].(map[string]any)["sentry.release"] = map[string]any{"type": "string", "value": string(old.Version)}
	if n, err := f.db.IngestSpans(f.ctx, f.project.ID, spanBatch(t, span), 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := f.db.IngestLogs(f.ctx, f.project.ID, json.RawMessage(`{"items":[{"timestamp":1,"level":"info","body":"Keep independent log"}]}`)); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE releases SET created_at=clock_timestamp()-interval '2 days' WHERE id::text=ANY($1::text[])`, []string{old.ID, open.ID}); err != nil {
		t.Fatal(err)
	}
	active := groupEvent(t, 4, "active-old-release", "Fresh capture on an old identity", time.Now())
	active.Release = string(old.Version)
	groupCapture(t, f.ctx, f.db, f.project, active)
	third, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Later association", "go")
	if err != nil {
		t.Fatal(err)
	}
	if associated, created, err := f.db.CreateRelease(f.ctx, f.access, ReleaseCreate{Version: old.Version, Projects: []string{third.ID}}); err != nil || created || associated.ID != old.ID {
		t.Fatal("later association changed shared identity", associated, created, err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 28)
	if n, err := f.db.CleanupReleases(f.ctx, 1); err != nil || n != 2 {
		t.Fatalf("expired release identities=%d want2 err=%v", n, err)
	}
	after := groupDatabaseSnapshot(t, f.ctx, f.db, 28)
	for table, rows := range before {
		switch table {
		case "releases", "release_projects", "release_commits", "release_deploys", "release_deploy_projects":
		default:
			if after[table] != rows {
				t.Fatal("release expiry changed independently owned state", table)
			}
		}
	}
	for _, table := range []string{"release_commits", "release_deploys", "release_deploy_projects"} {
		if after[table] != "[]" {
			t.Fatal("release-owned metadata survived", table, after[table])
		}
	}
	for _, version := range []ReleaseVersion{old.Version, open.Version} {
		if _, err := f.db.Release(f.ctx, f.access, version, ""); !errors.Is(err, ErrNotFound) {
			t.Fatal("expired metadata still readable", version, err)
		}
	}
	if _, err := f.db.Release(f.ctx, f.access, kept.Version, ""); err != nil {
		t.Fatal("user-supplied old release time expired a fresh receipt", err)
	}
	maps, err := f.db.SourceMaps(f.ctx, f.project.ID, string(old.Version), "web", nil, 1<<20)
	if err != nil || len(maps) != 1 || !bytes.Equal(maps[0].Data, emptySourceMap) {
		t.Fatal("independent file selector lost its bytes", err)
	}
	if got := triageIssue(t, f.ctx, f.db, f.owner.ID, id); got.Resolution.Release == nil || !got.Resolution.Release.DateCreated.Equal(selected.Resolution.Release.DateCreated) {
		t.Fatal("selected historical snapshot changed", got)
	}
	checkpoint := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev); err != nil || fresh {
		t.Fatal("retained event retry", fresh, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, checkpoint)
	// Isolate recreation of the baseline from a genuinely different new identity.
	pendingEvent.ID = "00000000000000000000000000000003"
	pendingEvent.Timestamp = time.Now()
	groupCapture(t, f.ctx, f.db, f.project, pendingEvent)
	current := triageIssue(t, f.ctx, f.db, f.owner.ID, pendingID)
	if current.Status != "resolved" || current.Resolution == nil || current.Resolution.Release != nil || !current.Resolution.Baseline.DateCreated.Equal(pending.Resolution.Baseline.DateCreated) {
		t.Fatal("recreated baseline incorrectly bound pending next release", current)
	}
	recreated, created, err := f.db.CreateRelease(f.ctx, f.access, ReleaseCreate{Version: old.Version, Projects: []string{f.project.ID}})
	if err != nil || !created || recreated.ID == old.ID || !recreated.DateCreated.After(old.DateCreated) {
		t.Fatal("fresh metadata generation", recreated, created, err)
	}
	mapAgain, created, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, mapMeta.Name, mapMeta.Release, mapMeta.Dist, mapMeta.DebugID, emptySourceMap)
	if err != nil || created || mapAgain.ID != mapMeta.ID || !mapAgain.DateCreated.Equal(mapMeta.DateCreated) {
		t.Fatal("metadata recreation reset independent file age", mapAgain, created, err)
	}
	if n, err := f.db.CleanupReleases(f.ctx, 1); err != nil || n != 0 {
		t.Fatal("fresh identities expired", n, err)
	}
}

func TestReleaseRetentionBoundedParentProgress(t *testing.T) {
	f := resolutionStore(t)
	kept := f.release(t, "unexpired@1")
	if _, err := f.db.pool.Exec(f.ctx, `WITH inserted AS (
 INSERT INTO releases(organization_id,version,created_at,status)
 SELECT $1::bigint,'release-batch-'||n,clock_timestamp()-interval '2 days',CASE WHEN n%2=0 THEN 'archived' ELSE 'open' END
 FROM generate_series(1,601) n RETURNING id
) INSERT INTO release_projects(release_id,project_id) SELECT id,$2::bigint FROM inserted`, f.org.ID, f.project.ID); err != nil {
		t.Fatal(err)
	}
	remaining := 601
	for _, want := range []int64{250, 250, 101, 0} {
		if n, err := f.db.CleanupReleases(f.ctx, 1); err != nil || n != want {
			t.Fatalf("bounded release progress=%d want%d err=%v", n, want, err)
		}
		remaining -= int(want)
		var parents, associations int
		if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM releases").Scan(&parents); err != nil || parents != remaining+1 {
			t.Fatal("wrong parent budget", parents, err)
		}
		if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM release_projects").Scan(&associations); err != nil || associations != remaining+1 {
			t.Fatal("incomplete owned cascade", associations, err)
		}
	}
	if current, err := f.db.Release(f.ctx, f.access, kept.Version, ""); err != nil || current.ID != kept.ID || !current.DateCreated.Equal(kept.DateCreated) {
		t.Fatal("batch sweep changed retained sibling", current, err)
	}
}
