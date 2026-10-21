//go:build integration

package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
	"testing"
)

func TestReleaseSharedIdentityAndCoverage(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Other release project", "go")
	if err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	version, err := ParseReleaseVersion(" App/Version 1 ")
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: version, Projects: []string{project.ID}})
	if err != nil || !created || first.Version != version || first.ShortVersion != version || first.Status != "open" || first.DateReleased != nil || !first.CanWrite || len(first.Projects) != 1 {
		t.Fatalf("create = %+v, created=%v error=%v", first, created, err)
	}
	merged, created, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: version, Projects: []string{other.Slug}})
	if err != nil || created || merged.ID != first.ID || len(merged.Projects) != 2 {
		t.Fatalf("merge = %+v, created=%v error=%v", merged, created, err)
	}
	restricted := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}}
	visible, err := db.Release(ctx, restricted, version, "")
	if err != nil || len(visible.Projects) != 1 || visible.Projects[0].ID != project.ID || visible.CanWrite {
		t.Fatalf("restricted detail = %+v, error=%v", visible, err)
	}
	status := "archived"
	unassociated, err := db.CreateProject(ctx, user.ID, org.Slug, "Unassociated release project", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateRelease(ctx, access, version, "", ReleaseUpdate{Status: &status, ProjectAssertions: []string{unassociated.Slug}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nonassociated project assertion = %v", err)
	}
	unchanged, err := db.Release(ctx, access, version, "")
	if err != nil || !reflect.DeepEqual(unchanged, merged) {
		t.Fatalf("failed assertion changed release = %+v error=%v", unchanged, err)
	}
	asserted, err := db.UpdateRelease(ctx, access, version, "", ReleaseUpdate{ProjectAssertions: []string{project.ID, other.Slug}})
	if err != nil || !reflect.DeepEqual(asserted, merged) {
		t.Fatalf("valid project assertions changed associations = %+v error=%v", asserted, err)
	}
	if _, err := db.UpdateRelease(ctx, restricted, version, "", ReleaseUpdate{Status: &status, ProjectAssertions: []string{project.ID}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("partial coverage update = %v", err)
	}
	if _, _, err := db.CreateRelease(ctx, restricted, ReleaseCreate{Version: version, Projects: []string{project.ID}, ReleaseUpdate: ReleaseUpdate{Status: &status}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("partial coverage merge = %v", err)
	}
	if err := db.DeleteRelease(ctx, restricted, version, project.Slug); err != nil {
		t.Fatal(err)
	}
	remaining, err := db.Release(ctx, access, version, "")
	if err != nil || remaining.ID != first.ID || len(remaining.Projects) != 1 || remaining.Projects[0].ID != other.ID || remaining.Status != "open" {
		t.Fatalf("remaining association = %+v, error=%v", remaining, err)
	}
	if _, err := db.Release(ctx, restricted, version, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed association still visible: %v", err)
	}
	if err := db.DeleteRelease(ctx, access, version, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Release(ctx, access, version, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted release detail = %v", err)
	}
}

func TestReleaseCommitRawOrderRepositoriesAndPreviousHead(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	version := ReleaseVersion("commit-v1")
	raw := json.RawMessage(`{ "id":"first", "repository":"manual/repo", "extra":9223372036854775807, "nul":"\u0000", "huge":1e1000000 }`)
	commits := []ReleaseCommitInput{
		{ID: "first", Repository: "manual/repo", Message: "Initial", Data: raw},
		{ID: "second", Repository: "other/repo", Data: json.RawMessage(`{"id":"second"}`)},
	}
	release, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: version, Projects: []string{project.ID}, ReleaseUpdate: ReleaseUpdate{Commits: &commits}})
	if err != nil || release.CommitCount != 2 || release.LastCommit == nil || release.LastCommit.ID != "first" || string(release.LastCommit.Data) != string(raw) {
		t.Fatalf("created commits = %+v, error=%v", release, err)
	}
	firstDate := release.LastCommit.DateCreated
	commits[0].Message = "Changed metadata, same commit identity"
	if _, err := db.ReplaceReleaseCommits(ctx, access, version, commits); err != nil {
		t.Fatal(err)
	}
	page, err := db.ReleaseCommits(ctx, access, version, project.Slug, 0, 1)
	if err != nil || len(page.Data) != 1 || !page.HasMore || page.HasPrevious || page.Data[0].ID != "first" || !page.Data[0].DateCreated.Equal(firstDate) || string(page.Data[0].Data) != string(raw) {
		t.Fatalf("first commit page = %+v, error=%v", page, err)
	}
	page, err = db.ReleaseCommits(ctx, access, version, "", 1, 1)
	if err != nil || len(page.Data) != 1 || page.HasMore || !page.HasPrevious || page.Data[0].ID != "second" {
		t.Fatalf("second commit page = %+v, error=%v", page, err)
	}
	stamp := time.Date(2020, 1, 2, 3, 4, 5, 123456000, time.UTC)
	commits[0].Timestamp = &stamp
	changed, err := db.ReplaceReleaseCommits(ctx, access, version, commits)
	if err != nil || !changed.LastCommit.DateCreated.Equal(stamp) {
		t.Fatalf("explicit commit timestamp = %+v, error=%v", changed, err)
	}
	commits[0].Timestamp = nil
	changed, err = db.ReplaceReleaseCommits(ctx, access, version, commits)
	if err != nil || !changed.LastCommit.DateCreated.Equal(firstDate) {
		t.Fatalf("first receipt after omitted timestamp = %+v, error=%v", changed, err)
	}
	repos, err := db.ReleaseRepositories(ctx, access, "MANUAL/", 0, 1)
	if err != nil || len(repos.Data) != 1 || repos.Data[0].Name != "manual/repo" || repos.Data[0].Provider.ID != "manual" || repos.Data[0].URL != nil {
		t.Fatalf("actual manual repository = %+v, error=%v", repos, err)
	}
	newer, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: "commit-v2", Projects: []string{project.ID}})
	if err != nil {
		t.Fatal(err)
	}
	previous, err := db.PreviousReleaseWithCommits(ctx, access, newer.Version)
	if err != nil || previous.ID != release.ID || previous.LastCommit == nil || previous.LastCommit.ID != "first" {
		t.Fatalf("previous actual head = %+v, error=%v", previous, err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	persisted, err := reopened.Release(ctx, access, version, "")
	if err != nil || persisted.CommitCount != 2 || !reflect.DeepEqual(persisted.LastCommit, changed.LastCommit) {
		t.Fatalf("reopened release = %+v, error=%v", persisted, err)
	}
	empty := []ReleaseCommitInput{}
	cleared, err := reopened.ReplaceReleaseCommits(ctx, access, version, empty)
	if err != nil || cleared.CommitCount != 0 || cleared.LastCommit != nil {
		t.Fatalf("cleared commits = %+v, error=%v", cleared, err)
	}
	repos, err = reopened.ReleaseRepositories(ctx, access, "", 0, 100)
	if err != nil || len(repos.Data) != 2 || repos.Data[0].Name != "manual/repo" || repos.Data[1].Name != "other/repo" {
		t.Fatalf("unreferenced repositories listed = %+v, error=%v", repos, err)
	}
	if _, err := reopened.PreviousReleaseWithCommits(ctx, access, newer.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty previous commits = %v", err)
	}
}

func TestReleaseDeployScopeObservationAndArtifactDeletion(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Deploy second", "go")
	if err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	version := ReleaseVersion(" exact/deploy version ")
	if _, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: version, Projects: []string{project.ID, other.ID}}); err != nil {
		t.Fatal(err)
	}
	shared, err := db.CreateReleaseDeploy(ctx, access, version, ReleaseDeployInput{Environment: "staging"})
	if err != nil || shared.ID <= 0 || !shared.DateFinished.Equal(shared.DateCreated) || len(shared.Projects) != 2 {
		t.Fatalf("DB receipt finish = %+v, error=%v", shared, err)
	}
	private, err := db.CreateReleaseDeploy(ctx, access, version, ReleaseDeployInput{Environment: "production", Projects: []string{other.ID}})
	if err != nil {
		t.Fatal(err)
	}
	restricted := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}}
	visible, err := db.Release(ctx, restricted, version, "")
	if err != nil || visible.DeployCount != 1 || visible.LastDeploy == nil || visible.LastDeploy.ID != shared.ID || len(visible.LastDeploy.Projects) != 1 {
		t.Fatalf("scoped deploy summary = %+v, error=%v", visible, err)
	}
	if _, err := db.CreateReleaseDeploy(ctx, restricted, version, ReleaseDeployInput{Environment: "default-needs-all"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("partial implicit deploy = %v", err)
	}
	readOnly := access
	readOnly.ReadOnly = true
	read, err := db.Release(ctx, readOnly, version, "")
	if err != nil || read.CanWrite {
		t.Fatalf("read-only credential canWrite = %+v, error=%v", read, err)
	}
	if _, err := db.UpdateRelease(ctx, readOnly, version, "", ReleaseUpdate{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only credential mutation = %v", err)
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, err := db.CreateReleaseDeploy(ctx, access, version, ReleaseDeployInput{Environment: "future", DateStarted: &future}); !errors.Is(err, ErrInvalidReleaseInput) {
		t.Fatalf("effective finish before future start = %v", err)
	}
	list, err := db.ReleaseDeploys(ctx, access, version, 0, 100)
	if err != nil || len(list.Data) != 2 || list.Data[0].ID != private.ID {
		t.Fatalf("durable deploys = %+v, error=%v", list, err)
	}
	for _, tc := range []struct {
		access ManagementAccess
		env    string
		want   int
	}{{restricted, "production", 0}, {restricted, "staging", 1}, {access, "production", 1}} {
		page, err := db.Releases(ctx, tc.access, ReleaseFilter{Environments: []string{tc.env}, Limit: 25})
		if err != nil || len(page.Data) != tc.want {
			t.Fatalf("environment %s scoped release list = %+v, error=%v", tc.env, page, err)
		}
	}
	raw := json.RawMessage(`{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","release":" exact/deploy version ","message":"keep captured data"}`)
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := db.Ingest(ctx, other.ID, ev); err != nil || !fresh {
		t.Fatalf("SDK observation fresh=%v error=%v", fresh, err)
	}
	if _, _, err := db.SaveSourceMap(ctx, user.ID, org.Slug, other.ID, "~/release.map", string(version), "", "", emptySourceMap); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.SaveSourceMap(ctx, user.ID, org.Slug, other.ID, "~/debug.map", "", "", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", emptySourceMap); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteRelease(ctx, access, version, other.ID); err != nil {
		t.Fatal(err)
	}
	maps, err := db.ListSourceMaps(ctx, user.ID, org.Slug, other.ID)
	if err != nil || len(maps) != 1 || maps[0].Release != "" {
		t.Fatalf("release-specific map deletion = %+v, error=%v", maps, err)
	}
	if _, fresh, err := db.Ingest(ctx, other.ID, ev); err != nil || fresh {
		t.Fatalf("duplicate SDK retry fresh=%v error=%v", fresh, err)
	}
	remaining, err := db.Release(ctx, access, version, "")
	if err != nil || len(remaining.Projects) != 1 {
		t.Fatalf("duplicate resurrected project = %+v, error=%v", remaining, err)
	}
	originalData := append(json.RawMessage(nil), ev.Data...)
	ev, err = event.Normalize(json.RawMessage(strings.Replace(string(raw), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 1)), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := db.Ingest(ctx, other.ID, ev); err != nil || !fresh {
		t.Fatalf("new SDK capture fresh=%v error=%v", fresh, err)
	}
	observed, err := db.Release(ctx, access, version, "")
	if err != nil || len(observed.Projects) != 2 || observed.DeployCount != 1 {
		t.Fatalf("fresh observation revived old private deploy = %+v, error=%v", observed, err)
	}
	var stored json.RawMessage
	if err := db.pool.QueryRow(ctx, `SELECT data FROM events WHERE project_id=$1 AND event_id=$2`, other.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var original, persisted any
	if json.Unmarshal(originalData, &original) != nil || json.Unmarshal(stored, &persisted) != nil || !reflect.DeepEqual(original, persisted) {
		t.Fatal("release deletion changed captured raw event")
	}
	if err := db.DeleteRelease(ctx, access, version, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Ingest(ctx, other.ID, ev); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Release(ctx, access, version, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate retry revived deleted entity = %v", err)
	}
}

func TestReleaseConcurrentIdentityAndCommitReplacement(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Concurrent other", "go")
	if err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	start := make(chan struct{})
	type creation struct {
		Release Release
		Created bool
		Err     error
	}
	results := make(chan creation, 8)
	for i := range 8 {
		go func(i int) {
			<-start
			ids := []string{project.ID, other.ID}
			if i%2 == 1 {
				ids[0], ids[1] = ids[1], ids[0]
			}
			r, created, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: "concurrent", Projects: ids})
			results <- creation{r, created, err}
		}(i)
	}
	close(start)
	created := 0
	identity := ""
	for range 8 {
		r := <-results
		if r.Err != nil || len(r.Release.Projects) != 2 {
			t.Fatalf("concurrent create = %+v", r)
		}
		if identity != "" && identity != r.Release.ID {
			t.Fatal("concurrent creation duplicated release identity")
		}
		identity = r.Release.ID
		if r.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("creation acknowledgements = %d, want 1", created)
	}
	versions := []ReleaseVersion{"concurrent", "concurrent-second"}
	if _, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: versions[1], Projects: []string{project.ID}}); err != nil {
		t.Fatal(err)
	}
	sets := [][]ReleaseCommitInput{
		{{ID: "a1", Repository: "repo/a", Data: json.RawMessage(`{"id":"a1"}`)}, {ID: "a2", Repository: "repo/b", Data: json.RawMessage(`{"id":"a2"}`)}},
		{{ID: "b1", Repository: "repo/b", Data: json.RawMessage(`{"id":"b1"}`)}, {ID: "b2", Repository: "repo/a", Data: json.RawMessage(`{"id":"b2"}`)}},
	}
	for _, version := range versions {
		if _, err := db.ReplaceReleaseCommits(ctx, access, version, sets[0]); err != nil {
			t.Fatal(err)
		}
	}
	start = make(chan struct{})
	errorsCh := make(chan error, 8)
	var writers sync.WaitGroup
	for i := range 8 {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			<-start
			_, err := db.ReplaceReleaseCommits(ctx, access, versions[i%2], sets[(i/2)%2])
			errorsCh <- err
		}(i)
	}
	close(start)
	for range 10 {
		page, err := db.ReleaseCommits(ctx, access, versions[0], "", 0, 100)
		if err != nil || len(page.Data) != 2 || page.Data[0].ID[:1] != page.Data[1].ID[:1] {
			t.Fatalf("partial concurrent commit set = %+v, error=%v", page, err)
		}
	}
	writers.Wait()
	for range 8 {
		if err := <-errorsCh; err != nil {
			t.Fatalf("concurrent repository/replacement lock failure: %v", err)
		}
	}

	// Release metadata is assembled by multiple SQL statements. Its count and
	// first commit must come from one snapshot while whole sets are replaced.
	longer := append(append([]ReleaseCommitInput(nil), sets[1]...), ReleaseCommitInput{ID: "b3", Repository: "repo/a", Data: json.RawMessage(`{"id":"b3"}`)})
	summarySets := [][]ReleaseCommitInput{sets[0], longer}
	firstWrite := make(chan struct{})
	writerResult := make(chan error, 1)
	go func() {
		for i := range 100 {
			_, err := db.ReplaceReleaseCommits(ctx, access, versions[0], summarySets[i%2])
			if i == 0 {
				close(firstWrite)
			}
			if err != nil {
				writerResult <- err
				return
			}
		}
		writerResult <- nil
	}()
	<-firstWrite
	var incoherent *Release
	for range 100 {
		r, err := db.Release(ctx, access, versions[0], "")
		if err != nil {
			t.Fatal(err)
		}
		if r.LastCommit == nil || (r.LastCommit.ID == "a1" && r.CommitCount != 2) || (r.LastCommit.ID == "b1" && r.CommitCount != 3) {
			copy := r
			incoherent = &copy
			break
		}
	}
	if err := <-writerResult; err != nil {
		t.Fatal(err)
	}
	if incoherent != nil {
		t.Fatalf("release summary mixed statement snapshots: count=%d last=%+v", incoherent.CommitCount, incoherent.LastCommit)
	}
}

func TestReleaseStandaloneObservationFreshOnly(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	version := ReleaseVersion("sdk/standalone")
	batch := func(spanID, release string) json.RawMessage {
		raw, err := json.Marshal(map[string]any{"items": []any{map[string]any{
			"trace_id": "cccccccccccccccccccccccccccccccc", "span_id": spanID,
			"name": "SDK segment", "status": "ok", "is_segment": true, "start_timestamp": 1, "end_timestamp": 2,
			"attributes": map[string]any{"sentry.release": map[string]any{"type": "string", "value": release}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	first := batch("0000000000000001", string(version))
	if count, err := db.IngestSpans(ctx, project.ID, first, 1); err != nil || count != 1 {
		t.Fatalf("fresh span count=%d error=%v", count, err)
	}
	if err := db.DeleteRelease(ctx, access, version, ""); err != nil {
		t.Fatal(err)
	}
	if count, err := db.IngestSpans(ctx, project.ID, first, 1); err != nil || count != 0 {
		t.Fatalf("duplicate span count=%d error=%v", count, err)
	}
	if _, err := db.Release(ctx, access, version, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate standalone span recreated release: %v", err)
	}
	if count, err := db.IngestSpans(ctx, project.ID, batch("0000000000000002", string(version)), 1); err != nil || count != 1 {
		t.Fatalf("new span count=%d error=%v", count, err)
	}
	archived := "archived"
	updated, err := db.UpdateRelease(ctx, access, version, "", ReleaseUpdate{Status: &archived})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := db.IngestSpans(ctx, project.ID, batch("0000000000000003", string(version)), 1); err != nil || count != 1 {
		t.Fatalf("later span count=%d error=%v", count, err)
	}
	observed, err := db.Release(ctx, access, version, "")
	if err != nil || observed.Status != "archived" || !observed.DateCreated.Equal(updated.DateCreated) {
		t.Fatalf("observation changed metadata = %+v, error=%v", observed, err)
	}
	invalid := strings.Repeat("界", 256)
	if count, err := db.IngestSpans(ctx, project.ID, batch("0000000000000004", invalid), 1); err != nil || count != 1 {
		t.Fatalf("invalid metadata prevented SDK durability count=%d error=%v", count, err)
	}
	page, err := db.Releases(ctx, access, ReleaseFilter{Limit: 25})
	if err != nil || len(page.Data) != 1 || page.Data[0].Version != version {
		t.Fatalf("invalid SDK version created metadata = %+v, error=%v", page, err)
	}
	spans, err := db.Spans(ctx, user.ID, org.Slug, project.ID, "")
	if err != nil || len(spans) != 4 || spans[3].Release != invalid {
		t.Fatalf("invalid SDK version was not preserved: spans=%+v error=%v", spans, err)
	}
}

func TestReleaseMigrationBackfillsPriorHistoryAndReopens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	schema := fmt.Sprintf("goodsentry_release_history_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	prior, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prior.Close(ctx) })
	// Build actual published migration history through009 before opening the
	// current Store. The forward migration must preserve these checksums and data.
	tx, err := prior.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations(version integer PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		prefix, _, _ := strings.Cut(file.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatal(err)
		}
		if version >= 10 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)`, version, fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var userID, orgID, firstProject, secondProject string
	if err := prior.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES('history@example.test','History',$1) RETURNING id::text`, []byte("already-hashed")).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := prior.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('history','History') RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := prior.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	for i, destination := range []*string{&firstProject, &secondProject} {
		if err := prior.QueryRow(ctx, `INSERT INTO projects(organization_id,slug,name,platform) VALUES($1,$2,$2,'go') RETURNING id::text`, orgID, fmt.Sprintf("project-%d", i)).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	version := " historical/版本 "
	long := strings.Repeat("界", 255)
	versions := []string{version, long, "", ".", "..", "bad\u0085version", strings.Repeat("界", 256)}
	for i, value := range versions {
		if _, err := prior.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,release,data,created_at)
			VALUES($1,$2,'message','Retained','info','2000-01-01Z',$3,$4,'2023-01-01Z')`, firstProject, fmt.Sprintf("%032x", i+1), value, json.RawMessage(`{"extra":9223372036854775807,"message":"historical"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := prior.Exec(ctx, `INSERT INTO spans(project_id,trace_id,span_id,name,status,is_segment,start_timestamp,end_timestamp,duration,release,data,created_at)
		VALUES($1,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','aaaaaaaaaaaaaaaa','Old','ok',true,'2000-01-01Z','2000-01-01Z',0,$2,'{}','2022-01-01Z')`, firstProject, version); err != nil {
		t.Fatal(err)
	}
	if _, err := prior.Exec(ctx, `INSERT INTO source_maps(project_id,name,release,checksum,size,data,created_at)
		VALUES($1,'~/old.map',$2,'fixture',$3,$4,'2021-01-01Z')`, secondProject, version, len(emptySourceMap), emptySourceMap); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := prior.QueryRow(ctx, `SELECT json_agg(json_build_object('id',id,'release',release,'data',data) ORDER BY id)::text FROM events`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_ = prior.Close(ctx)
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	access := ManagementAccess{UserID: userID, OrganizationID: orgID, AllProjects: true}
	page, err := db.Releases(ctx, access, ReleaseFilter{Limit: 25})
	if err != nil || len(page.Data) != 2 {
		t.Fatalf("backfilled identities = %+v, error=%v", page, err)
	}
	observed, err := db.Release(ctx, access, ReleaseVersion(version), "")
	if err != nil || len(observed.Projects) != 2 || !observed.DateCreated.Equal(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("earliest receipt and associations = %+v, error=%v", observed, err)
	}
	var after string
	if err := db.pool.QueryRow(ctx, `SELECT json_agg(json_build_object('id',id,'release',release,'data',data) ORDER BY id)::text FROM events`).Scan(&after); err != nil || before != after {
		t.Fatalf("migration rewrote historical events: error=%v", err)
	}
	var applied time.Time
	var migrationCount int
	if err := db.pool.QueryRow(ctx, `SELECT applied_at,(SELECT count(*) FROM schema_migrations WHERE version<=10) FROM schema_migrations WHERE version=10`).Scan(&applied, &migrationCount); err != nil || migrationCount != 10 {
		t.Fatalf("forward migration history count=%d error=%v", migrationCount, err)
	}
	db.Close()
	reopened, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	var reopenedApplied time.Time
	if err := reopened.pool.QueryRow(ctx, `SELECT applied_at FROM schema_migrations WHERE version=10`).Scan(&reopenedApplied); err != nil || !reopenedApplied.Equal(applied) {
		t.Fatalf("reopen reapplied migration: error=%v", err)
	}
	persisted, err := reopened.Release(ctx, access, ReleaseVersion(version), "")
	if err != nil || !reflect.DeepEqual(persisted, observed) {
		t.Fatalf("reopened backfill = %+v, error=%v", persisted, err)
	}
}

func TestReleaseMetadataTimesAreUTC(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	at := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.FixedZone("client", 2*60*60))
	commits := []ReleaseCommitInput{{ID: "utc", Repository: "utc/repo", Timestamp: &at, Data: json.RawMessage(`{"id":"utc"}`)}}
	r, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: "utc", Projects: []string{project.ID}, ReleaseUpdate: ReleaseUpdate{
		DateStarted: ReleaseTime{Set: true, Value: &at}, DateReleased: ReleaseTime{Set: true, Value: &at}, Commits: &commits,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.DateStarted == nil || r.DateReleased == nil || r.LastCommit == nil || len(r.Projects) != 1 {
		t.Fatalf("missing temporal metadata: %+v", r)
	}
	values := []time.Time{r.DateCreated, *r.DateStarted, *r.DateReleased, r.LastCommit.DateCreated, r.Projects[0].DateCreated}
	d, err := db.CreateReleaseDeploy(ctx, access, r.Version, ReleaseDeployInput{Environment: "utc", DateStarted: &at})
	if err != nil || d.DateStarted == nil {
		t.Fatalf("deploy time error=%v", err)
	}
	values = append(values, d.DateCreated, *d.DateStarted, d.DateFinished, d.Projects[0].DateCreated)
	listed, err := db.ReleaseDeploys(ctx, access, r.Version, 0, 25)
	if err != nil || len(listed.Data) != 1 {
		t.Fatalf("deploy list error=%v", err)
	}
	values = append(values, listed.Data[0].DateCreated, *listed.Data[0].DateStarted, listed.Data[0].DateFinished)
	repos, err := db.ReleaseRepositories(ctx, access, "", 0, 25)
	if err != nil || len(repos.Data) != 1 {
		t.Fatalf("repository list error=%v", err)
	}
	values = append(values, repos.Data[0].DateCreated)
	for i, value := range values {
		if value.Location() != time.UTC {
			t.Fatalf("metadata timestamp[%d] uses %s, want UTC: %v", i, value.Location(), value)
		}
	}
	if !r.DateStarted.Equal(at) || !r.LastCommit.DateCreated.Equal(at) {
		t.Fatal("UTC normalization changed the supplied instant")
	}
}

func TestReleaseProjectReferencesResolveWithinAuthorizedScope(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	if _, err := db.CreateProject(ctx, user.ID, org.Slug, project.ID, "go"); err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}}
	r, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: "numeric-project-reference", Projects: []string{project.ID}})
	if err != nil || len(r.Projects) != 1 || r.Projects[0].ID != project.ID {
		t.Fatalf("hidden numeric slug shadowed authorized ID: release=%+v error=%v", r, err)
	}
	if _, err := db.Release(ctx, access, r.Version, project.ID); err != nil {
		t.Fatalf("authorized numeric project detail: %v", err)
	}
}

func TestReleaseSourceMapAcknowledgementAndDeleteOrdering(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	version := " " + strings.Repeat("界", 253) + " "
	upload := func() (bool, error) {
		mapFile, created, err := db.SaveSourceMap(ctx, user.ID, org.Slug, project.ID, "~/ordered.map", version, "", "", emptySourceMap)
		if err == nil && mapFile.Release != version {
			return created, fmt.Errorf("source-map release identity changed")
		}
		return created, err
	}
	if created, err := upload(); err != nil || !created {
		t.Fatalf("255-character exact identity upload created=%v error=%v", created, err)
	}
	if err := db.DeleteRelease(ctx, access, ReleaseVersion(version), ""); err != nil {
		t.Fatal(err)
	}
	maps, err := db.ListSourceMaps(ctx, user.ID, org.Slug, project.ID)
	if err != nil || len(maps) != 0 {
		t.Fatalf("acknowledged upload survived later delete: maps=%+v error=%v", maps, err)
	}
	if created, err := upload(); err != nil || !created {
		t.Fatalf("later upload did not recreate release created=%v error=%v", created, err)
	}
	for range 8 {
		start := make(chan struct{})
		type receipt struct {
			created bool
			err     error
		}
		uploadDone := make(chan receipt, 1)
		deleteDone := make(chan error, 1)
		go func() { <-start; created, err := upload(); uploadDone <- receipt{created, err} }()
		go func() { <-start; deleteDone <- db.DeleteRelease(ctx, access, ReleaseVersion(version), "") }()
		close(start)
		result := <-uploadDone
		if err := <-deleteDone; err != nil || result.err != nil {
			t.Fatalf("concurrent upload/delete error=%v upload=%v", err, result.err)
		}
		maps, err := db.ListSourceMaps(ctx, user.ID, org.Slug, project.ID)
		if err != nil {
			t.Fatal(err)
		}
		r, releaseErr := db.Release(ctx, access, ReleaseVersion(version), "")
		if result.created {
			if len(maps) != 1 || releaseErr != nil || len(r.Projects) != 1 {
				t.Fatalf("post-delete acknowledged upload lost association: maps=%+v release=%+v error=%v", maps, r, releaseErr)
			}
		} else if len(maps) != 0 || !errors.Is(releaseErr, ErrNotFound) {
			t.Fatalf("pre-delete retry left orphan data: maps=%+v release=%+v error=%v", maps, r, releaseErr)
		}
		if len(maps) == 0 {
			if _, err := upload(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestReleaseProjectResolutionIsAtomicAndMembershipIsCurrent(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	var foreignOrg string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('foreign-release','Foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, foreignOrg, user.ID); err != nil {
		t.Fatal(err)
	}
	foreign, err := db.CreateProject(ctx, user.ID, "foreign-release", "Foreign release project", "go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{foreign.ID, "9223372036854775807"} {
		if _, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: "atomic", Projects: []string{project.ID, bad}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid project create = %v", err)
		}
		var count int
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM releases`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("invalid creation partially persisted count=%d error=%v", count, err)
		}
	}
	own, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: "same-version", Projects: []string{project.ID}})
	if err != nil {
		t.Fatal(err)
	}
	foreignAccess := ManagementAccess{UserID: user.ID, OrganizationID: foreignOrg, AllProjects: true}
	other, _, err := db.CreateRelease(ctx, foreignAccess, ReleaseCreate{Version: "same-version", Projects: []string{foreign.ID}})
	if err != nil || own.ID == other.ID || len(other.Projects) != 1 || other.Projects[0].ID != foreign.ID {
		t.Fatalf("organization version identity = %+v error=%v", other, err)
	}
	var member string
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES('release-member@example.test','Member',$1) RETURNING id::text`, []byte("already-hashed")).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	memberAccess := ManagementAccess{UserID: member, OrganizationID: org.ID, AllProjects: true}
	visible, err := db.Release(ctx, memberAccess, own.Version, "")
	if err != nil || visible.CanWrite {
		t.Fatalf("actual member release read = %+v error=%v", visible, err)
	}
	if err := db.DeleteRelease(ctx, memberAccess, own.Version, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member deletion = %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE memberships SET role='admin' WHERE organization_id=$1 AND user_id=$2`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	archived := "archived"
	if _, err := db.UpdateRelease(ctx, memberAccess, own.Version, "", ReleaseUpdate{Status: &archived}); err != nil {
		t.Fatalf("current admin update = %v", err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2`, org.ID, member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Release(ctx, memberAccess, own.Version, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed membership still reads release = %v", err)
	}
}
