//go:build integration

package store

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodsentry/internal/artifact"

	"github.com/jackc/pgx/v5"
)

func artifactChunkFixture(data string) ArtifactChunk {
	return ArtifactChunk{Checksum: fmt.Sprintf("%x", sha1.Sum([]byte(data))), Data: []byte(data)}
}

func TestArtifactChunkStagingQuotaAndExpiry(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	policy := ArtifactStagingPolicy{MaxBytes: 6, TTL: time.Hour}
	first, second, third := artifactChunkFixture("abc"), artifactChunkFixture("def"), artifactChunkFixture("ghij")
	if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{first, first}, policy); err != nil {
		t.Fatal(err)
	}
	var receipt, expiry time.Time
	if err := db.pool.QueryRow(ctx, `SELECT created_at,expires_at FROM artifact_chunks WHERE organization_id=$1 AND checksum=$2`, org.ID, first.Checksum).Scan(&receipt, &expiry); err != nil {
		t.Fatal(err)
	}
	policy.TTL = 2 * time.Hour
	if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{first, second}, policy); err != nil {
		t.Fatal(err)
	}
	var afterReceipt, afterExpiry time.Time
	if err := db.pool.QueryRow(ctx, `SELECT created_at,expires_at FROM artifact_chunks WHERE organization_id=$1 AND checksum=$2`, org.ID, first.Checksum).Scan(&afterReceipt, &afterExpiry); err != nil {
		t.Fatal(err)
	}
	if !afterReceipt.Equal(receipt) || !afterExpiry.After(expiry.Add(59*time.Minute)) {
		t.Fatalf("retry altered receipt or failed TTL refresh: %v/%v -> %v/%v", receipt, expiry, afterReceipt, afterExpiry)
	}
	data, missing, err := db.ArtifactChunkData(ctx, access, []string{project.Slug}, []string{second.Checksum, first.Checksum, second.Checksum}, 9)
	if err != nil || len(missing) != 0 || string(data) != "defabcdef" {
		t.Fatalf("ordered repeats: %q missing=%v err=%v", data, missing, err)
	}
	if _, _, err := db.ArtifactChunkData(ctx, access, []string{project.ID}, []string{second.Checksum, first.Checksum, second.Checksum}, 8); !errors.Is(err, ErrArtifactLimit) {
		t.Fatalf("repeated bytes exceeded read budget: %v", err)
	}
	if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{artifactChunkFixture("x"), third}, policy); !errors.Is(err, ErrArtifactLimit) {
		t.Fatalf("quota error=%v", err)
	}
	conflict := ArtifactChunk{Checksum: first.Checksum, Data: []byte("BAD")}
	if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{artifactChunkFixture("y"), conflict}, ArtifactStagingPolicy{MaxBytes: 100, TTL: time.Hour}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting batch=%v", err)
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM artifact_chunks WHERE organization_id=$1`, org.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("partial quota/conflict batch persisted: count=%d err=%v", count, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE artifact_chunks SET expires_at=clock_timestamp()-interval '1 second' WHERE organization_id=$1 AND checksum=$2`, org.ID, first.Checksum); err != nil {
		t.Fatal(err)
	}
	unknown := artifactChunkFixture("missing")
	data, missing, err = db.ArtifactChunkData(ctx, access, []string{project.ID}, []string{unknown.Checksum, first.Checksum, unknown.Checksum, second.Checksum, first.Checksum}, 100)
	if err != nil || data != nil || !reflect.DeepEqual(missing, []string{unknown.Checksum, first.Checksum}) {
		t.Fatalf("expired/missing order: %q missing=%v err=%v", data, missing, err)
	}
	// Expired bytes are reclaimed by the next write before its live-byte quota.
	if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{artifactChunkFixture("xyz")}, policy); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	data, missing, err = reopened.ArtifactChunkData(ctx, access, []string{project.ID}, []string{second.Checksum}, 3)
	if err != nil || len(missing) != 0 || string(data) != "def" {
		t.Fatalf("reopen data=%q missing=%v error=%v", data, missing, err)
	}
}

func TestArtifactChunkConcurrentQuotaAndCleanup(t *testing.T) {
	ctx, db, user, org, _ := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	start := make(chan struct{})
	results := make(chan error, 8)
	var group sync.WaitGroup
	for i := range 8 {
		group.Go(func() {
			<-start
			results <- db.SaveArtifactChunks(ctx, access, []ArtifactChunk{artifactChunkFixture(fmt.Sprintf("%04d", i))}, ArtifactStagingPolicy{MaxBytes: 4, TTL: time.Hour})
		})
	}
	close(start)
	group.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrArtifactLimit) {
			t.Fatal(err)
		}
	}
	var size int64
	if err := db.pool.QueryRow(ctx, `SELECT COALESCE(sum(size),0)::bigint FROM artifact_chunks WHERE organization_id=$1`, org.ID).Scan(&size); err != nil || success != 1 || size != 4 {
		t.Fatalf("quota race success=%d bytes=%d error=%v", success, size, err)
	}
	// Real rows prove bounded replica cleanup and SKIP LOCKED behavior.
	if _, err := db.pool.Exec(ctx, `INSERT INTO artifact_chunks(organization_id,checksum,data,size,expires_at)
        SELECT $1::bigint,lpad(to_hex(n),40,'0'),'x'::bytea,1,clock_timestamp()-interval '1 hour' FROM generate_series(1,1002) n`, org.ID); err != nil {
		t.Fatal(err)
	}
	held, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	var locked string
	if err := held.QueryRow(ctx, `SELECT checksum FROM artifact_chunks WHERE organization_id=$1 AND expires_at<clock_timestamp() ORDER BY checksum LIMIT 1 FOR UPDATE`, org.ID).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	removed, err := db.CleanupArtifactChunks(ctx)
	if err != nil || removed != 1000 {
		t.Fatalf("bounded cleanup rows=%d err=%v", removed, err)
	}
	var retained bool
	if err := db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artifact_chunks WHERE organization_id=$1 AND checksum=$2)`, org.ID, locked).Scan(&retained); err != nil || !retained {
		t.Fatalf("locked expiry was not skipped: %v", err)
	}
	if err := held.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	removed, err = db.CleanupArtifactChunks(ctx)
	if err != nil || removed != 2 {
		t.Fatalf("remaining cleanup rows=%d err=%v", removed, err)
	}
	var live int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM artifact_chunks WHERE organization_id=$1`, org.ID).Scan(&live); err != nil || live != 1 {
		t.Fatalf("cleanup removed live chunk: %d %v", live, err)
	}
	if err := db.ArtifactUploadAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactUploadCurrentRoleOrganizationAndProjectScope(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	id, err := strconv.ParseInt(project.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	restricted := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, ProjectIDs: []int64{id}}
	hidden, err := db.CreateProject(ctx, user.ID, org.Slug, project.ID, "javascript")
	if err != nil {
		t.Fatal(err)
	}
	chunk := artifactChunkFixture("scope")
	policy := ArtifactStagingPolicy{MaxBytes: 100, TTL: time.Hour}
	if err := db.SaveArtifactChunks(ctx, restricted, []ArtifactChunk{chunk}, policy); err != nil {
		t.Fatal(err)
	}
	if data, _, err := db.ArtifactChunkData(ctx, restricted, []string{project.ID}, []string{chunk.Checksum}, 100); err != nil || string(data) != "scope" {
		t.Fatalf("hidden numeric slug shadowed allowed project: %q %v", data, err)
	}
	if _, _, err := db.ArtifactChunkData(ctx, restricted, []string{project.ID, hidden.ID}, []string{chunk.Checksum}, 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden selected project error=%v", err)
	}
	if err := db.SaveBundleSourceMaps(ctx, restricted, []string{project.ID, hidden.ID}, "scope-build", "", []artifact.SourceMap{{Name: "~/scope.map", Data: emptySourceMap}}, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden project materialization error=%v", err)
	}
	var materialized int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM source_maps`).Scan(&materialized); err != nil || materialized != 0 {
		t.Fatalf("unauthorized batch wrote maps: %d %v", materialized, err)
	}

	var foreignOrg string
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('artifact-foreign','Foreign') RETURNING id::text`).Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, foreignOrg, user.ID); err != nil {
		t.Fatal(err)
	}
	foreign, err := db.CreateProject(ctx, user.ID, "artifact-foreign", "Foreign", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	foreignAccess := ManagementAccess{UserID: user.ID, OrganizationID: foreignOrg, AllProjects: true}
	if data, missing, err := db.ArtifactChunkData(ctx, foreignAccess, []string{foreign.ID}, []string{chunk.Checksum}, 100); err != nil || data != nil || !reflect.DeepEqual(missing, []string{chunk.Checksum}) {
		t.Fatalf("org isolation: %q %v %v", data, missing, err)
	}
	readOnly := restricted
	readOnly.ReadOnly = true
	checks := []struct {
		name   string
		access ManagementAccess
		want   error
	}{
		{"read-only", readOnly, ErrForbidden},
		{"empty project selection", ManagementAccess{UserID: user.ID, OrganizationID: org.ID}, ErrNotFound},
	}
	run := func(access ManagementAccess, want error) {
		t.Helper()
		if err := db.ArtifactUploadAccess(ctx, access); !errors.Is(err, want) {
			t.Fatalf("negotiation error=%v want=%v", err, want)
		}
		if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{chunk}, policy); !errors.Is(err, want) {
			t.Fatalf("staging error=%v want=%v", err, want)
		}
		if _, _, err := db.ArtifactChunkData(ctx, access, []string{project.ID}, []string{chunk.Checksum}, 100); !errors.Is(err, want) {
			t.Fatalf("assembly read error=%v want=%v", err, want)
		}
		if err := db.SaveBundleSourceMaps(ctx, access, []string{project.ID}, "", "", []artifact.SourceMap{{Name: "~/app.js.map", DebugID: "11111111111111111111111111111111", Data: emptySourceMap}}, 100); !errors.Is(err, want) {
			t.Fatalf("materialization error=%v want=%v", err, want)
		}
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) { run(check.access, check.want) })
	}
	if _, err := db.pool.Exec(ctx, `UPDATE memberships SET role='member' WHERE organization_id=$1 AND user_id=$2`, org.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	run(restricted, ErrForbidden)
	if _, err := db.pool.Exec(ctx, `UPDATE memberships SET role='admin' WHERE organization_id=$1 AND user_id=$2`, org.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.ArtifactUploadAccess(ctx, restricted); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2`, org.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	run(restricted, ErrNotFound)
}

func TestArtifactBundleAtomicMapsAliasesAndReleaseIndependence(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Other bundle project", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	maps := []artifact.SourceMap{{Name: "~/app.js.map", DebugID: "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA", Data: emptySourceMap, CodeFiles: []string{"~/z.js", "~/app.js", "~/app.js"}}}
	refs := []string{other.ID, project.Slug, project.ID}
	if err := db.SaveBundleSourceMaps(ctx, access, refs, "bundle-v1", "web", maps, 1); !errors.Is(err, ErrArtifactLimit) {
		t.Fatalf("record product budget=%v", err)
	}
	if err := db.SaveBundleSourceMaps(ctx, access, refs, "bundle-v1", "web", maps, 2); err != nil {
		t.Fatal(err)
	}
	var releaseCount int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM releases WHERE organization_id=$1`, org.ID).Scan(&releaseCount); err != nil || releaseCount != 0 {
		t.Fatalf("compatible upload observed a release: %d %v", releaseCount, err)
	}
	before, err := db.ListSourceMaps(ctx, user.ID, org.Slug, project.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("initial maps=%+v err=%v", before, err)
	}
	maps[0].CodeFiles = []string{"~/new.js", "~/app.js"}
	if err := db.SaveBundleSourceMaps(ctx, access, refs, "bundle-v1", "web", maps, 2); err != nil {
		t.Fatal(err)
	}
	after, err := db.ListSourceMaps(ctx, user.ID, org.Slug, project.ID)
	if err != nil || len(after) != 1 || after[0].ID != before[0].ID || after[0].Checksum != before[0].Checksum || !after[0].DateCreated.Equal(before[0].DateCreated) || !reflect.DeepEqual(after[0].CodeFiles, []string{"~/app.js", "~/new.js", "~/z.js"}) {
		t.Fatalf("alias union/idempotency before=%+v after=%+v err=%v", before, after, err)
	}
	native, created, err := db.SaveSourceMap(ctx, user.ID, org.Slug, project.ID, maps[0].Name, "bundle-v1", "web", maps[0].DebugID, emptySourceMap)
	if err != nil || created || !reflect.DeepEqual(native.CodeFiles, after[0].CodeFiles) {
		t.Fatalf("native retry lost aliases: %+v %v %v", native, created, err)
	}
	// A late conflict on the second project must roll back new maps and aliases
	// already visited on the first project in this same transaction.
	conflictData := []byte(`{"version":3,"sources":[],"names":[],"mappings":";"}`)
	if _, _, err := db.SaveSourceMap(ctx, user.ID, org.Slug, other.ID, "~/z-conflict.js.map", "bundle-v1", "web", "", conflictData); err != nil {
		t.Fatal(err)
	}
	bad := []artifact.SourceMap{
		{Name: "~/new.js.map", Data: emptySourceMap},
		{Name: maps[0].Name, DebugID: maps[0].DebugID, Data: emptySourceMap, CodeFiles: []string{"~/rolled-back.js"}},
		{Name: "~/z-conflict.js.map", Data: emptySourceMap},
	}
	if err := db.SaveBundleSourceMaps(ctx, access, refs, "bundle-v1", "web", bad, 6); !errors.Is(err, ErrConflict) {
		t.Fatalf("late map conflict=%v", err)
	}
	unchanged, err := db.ListSourceMaps(ctx, user.ID, org.Slug, project.ID)
	if err != nil || !reflect.DeepEqual(unchanged, after) {
		t.Fatalf("partial multi-project materialization: %+v want=%+v err=%v", unchanged, after, err)
	}
	loaded, err := db.SourceMaps(ctx, project.ID, "bundle-v1", "web", nil, 1<<20)
	if err != nil || len(loaded) != 1 || !bytes.Equal(loaded[0].Data, emptySourceMap) || !reflect.DeepEqual(loaded[0].CodeFiles, after[0].CodeFiles) || loaded[0].Name != "~/app.js.map" {
		t.Fatalf("consumer map lost original bytes/base/aliases: %+v %v", loaded, err)
	}
	// Uploading to another project of an existing release does not associate it.
	version, _ := ParseReleaseVersion("unassociated-v2")
	if _, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: version, Projects: []string{project.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveBundleSourceMaps(ctx, access, []string{other.ID}, version, "", maps, 1); err != nil {
		t.Fatal(err)
	}
	current, err := db.Release(ctx, access, version, "")
	if err != nil || len(current.Projects) != 1 || current.Projects[0].ID != project.ID {
		t.Fatalf("upload changed release associations: %+v %v", current, err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	durable, err := reopened.ListSourceMaps(ctx, user.ID, org.Slug, project.ID)
	if err != nil || !reflect.DeepEqual(durable, after) {
		t.Fatalf("reopened aliases/metadata changed: %+v %v", durable, err)
	}
}

func TestArtifactChunkReadSnapshotDuringConcurrentReplacement(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	first, second := artifactChunkFixture("one"), artifactChunkFixture("TWO")
	if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{first, second}, ArtifactStagingPolicy{MaxBytes: 6, TTL: 25 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	// Expiry and the actual cleanup provider remove valid immutable chunks
	// while readers preflight metadata and then fetch bytes. Reuploads always
	// use identical bytes and their real content digests; no clock seam is used.
	start := make(chan struct{})
	results := make(chan error, 5)
	var group sync.WaitGroup
	var completeReads, missingReads atomic.Int64
	group.Go(func() {
		<-start
		for range 60 {
			time.Sleep(30 * time.Millisecond)
			if _, err := db.CleanupArtifactChunks(ctx); err != nil {
				results <- err
				return
			}
			time.Sleep(5 * time.Millisecond)
			if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{first, second}, ArtifactStagingPolicy{MaxBytes: 6, TTL: 25 * time.Millisecond}); err != nil {
				results <- err
				return
			}
		}
		results <- nil
	})
	for range 4 {
		group.Go(func() {
			<-start
			for range 250 {
				data, missing, err := db.ArtifactChunkData(ctx, access, []string{project.ID}, []string{first.Checksum, second.Checksum}, 6)
				if err != nil {
					results <- err
					return
				}
				if len(missing) == 0 {
					completeReads.Add(1)
				} else {
					missingReads.Add(1)
				}
				if len(missing) == 0 && string(data) != "oneTWO" {
					results <- fmt.Errorf("snapshot changed after preflight: data=%q missing=%v", data, missing)
					return
				}
				if len(missing) != 0 && (data != nil || !reflect.DeepEqual(missing, []string{first.Checksum, second.Checksum})) {
					results <- fmt.Errorf("partially visible atomic batch: data=%q missing=%v", data, missing)
					return
				}
			}
			results <- nil
		})
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if completeReads.Load() == 0 || missingReads.Load() == 0 {
		t.Fatalf("race did not observe both states: complete=%d missing=%d", completeReads.Load(), missingReads.Load())
	}
	t.Logf("immutable-content race observed complete=%d missing=%d", completeReads.Load(), missingReads.Load())
}

func TestArtifactBundleConcurrentAliasesAndReleaseDeletion(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Concurrent bundle", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	refs := []string{other.ID, project.ID}
	version := ReleaseVersion("concurrent-bundle")
	mapFor := func(alias string) []artifact.SourceMap {
		return []artifact.SourceMap{{Name: "~/app.map", Data: emptySourceMap, CodeFiles: []string{alias}}}
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var group sync.WaitGroup
	for i := range 8 {
		group.Go(func() {
			<-start
			results <- db.SaveBundleSourceMaps(ctx, access, refs, version, "", mapFor(fmt.Sprintf("~/script-%d.js", i)), 2)
		})
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{project.ID, other.ID} {
		maps, err := db.ListSourceMaps(ctx, user.ID, org.Slug, id)
		if err != nil || len(maps) != 1 || len(maps[0].CodeFiles) != 8 {
			t.Fatalf("concurrent alias union project=%s maps=%+v err=%v", id, maps, err)
		}
		for i, alias := range maps[0].CodeFiles {
			if alias != fmt.Sprintf("~/script-%d.js", i) {
				t.Fatalf("nondeterministic aliases: %v", maps[0].CodeFiles)
			}
		}
	}
	for range 8 {
		if _, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: version, Projects: refs}); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		upload := make(chan error, 1)
		deleted := make(chan error, 1)
		go func() {
			<-start
			upload <- db.SaveBundleSourceMaps(ctx, access, refs, version, "", mapFor("~/retry.js"), 2)
		}()
		go func() { <-start; deleted <- db.DeleteRelease(ctx, access, version, "") }()
		close(start)
		if err := <-upload; err != nil {
			t.Fatal(err)
		}
		if err := <-deleted; err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM source_maps WHERE release=$1`, string(version)).Scan(&count); err != nil || count != 0 && count != 2 {
			t.Fatalf("partial upload/delete count=%d error=%v", count, err)
		}
		if _, err := db.Release(ctx, access, version, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("compatible concurrent upload resurrected release: %v", err)
		}
	}
	// Ordered acknowledgments prove both allowed endpoints of the race.
	if _, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: version, Projects: refs}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveBundleSourceMaps(ctx, access, refs, version, "", mapFor("~/ordered.js"), 2); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteRelease(ctx, access, version, ""); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM source_maps WHERE release=$1`, string(version)).Scan(&count); err != nil || count != 0 {
		t.Fatalf("acknowledged upload survived later delete: %d %v", count, err)
	}
	if err := db.SaveBundleSourceMaps(ctx, access, refs, version, "", mapFor("~/restored.js"), 2); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM source_maps WHERE release=$1`, string(version)).Scan(&count); err != nil || count != 2 {
		t.Fatalf("authorized later upload failed restoration: %d %v", count, err)
	}
	if _, err := db.Release(ctx, access, version, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("later upload created metadata: %v", err)
	}
}

func TestArtifactMigrationPreservesExistingMaps(t *testing.T) {
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
	schema := fmt.Sprintf("goodsentry_artifact_history_%d", time.Now().UnixNano())
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
		if version >= 11 {
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
	var userID, orgID, projectID string
	if err := prior.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES('artifact-history@example.test','History',$1) RETURNING id::text`, []byte("already-hashed")).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := prior.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES('artifact-history','History') RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := prior.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	if err := prior.QueryRow(ctx, `INSERT INTO projects(organization_id,slug,name,platform) VALUES($1,'web','Web','javascript') RETURNING id::text`, orgID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(emptySourceMap))
	if _, err := prior.Exec(ctx, `INSERT INTO source_maps(project_id,name,release,dist,checksum,size,data,created_at)
        VALUES($1,'~/historical.map','old','web',$2,$3,$4,'2020-01-02T03:04:05.123456Z')`, projectID, checksum, len(emptySourceMap), emptySourceMap); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := prior.QueryRow(ctx, `SELECT json_agg(to_jsonb(m) ORDER BY id)::text FROM source_maps m`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	var after string
	if err := db.pool.QueryRow(ctx, `SELECT json_agg(to_jsonb(m)-'code_files' ORDER BY id)::text FROM source_maps m`).Scan(&after); err != nil || before != after {
		t.Fatalf("migration rewrote original metadata/data: before=%s after=%s err=%v", before, after, err)
	}
	maps, err := db.SourceMaps(ctx, projectID, "old", "web", nil, 1<<20)
	if err != nil || len(maps) != 1 || len(maps[0].CodeFiles) != 0 || !bytes.Equal(maps[0].Data, emptySourceMap) {
		t.Fatalf("historical consumer changed: %+v %v", maps, err)
	}
	access := ManagementAccess{UserID: userID, OrganizationID: orgID, AllProjects: true}
	if err := db.SaveArtifactChunks(ctx, access, []ArtifactChunk{artifactChunkFixture("after migration")}, ArtifactStagingPolicy{MaxBytes: 100, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	data, missing, err := reopened.ArtifactChunkData(ctx, access, []string{projectID}, []string{artifactChunkFixture("after migration").Checksum}, 100)
	if err != nil || len(missing) != 0 || string(data) != "after migration" {
		t.Fatalf("reopen staging after migration: %q %v %v", data, missing, err)
	}
}

func TestArtifactSourceMapsCoalesceEquivalentSelectors(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: user.ID, OrganizationID: org.ID, AllProjects: true}
	const debugID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sourceMap := artifact.SourceMap{Name: "~/app.js.map", DebugID: debugID, Data: emptySourceMap, CodeFiles: []string{"~/debug.js"}}
	if err := db.SaveBundleSourceMaps(ctx, access, []string{project.ID}, "", "", []artifact.SourceMap{sourceMap}, 1); err != nil {
		t.Fatal(err)
	}
	sourceMap.CodeFiles = []string{"~/release.js", "~/debug.js"}
	if err := db.SaveBundleSourceMaps(ctx, access, []string{project.ID}, "v1", "web", []artifact.SourceMap{sourceMap}, 1); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.SourceMaps(ctx, project.ID, "v1", "web", []string{debugID}, int64(len(emptySourceMap)))
	if err != nil || len(loaded) != 1 || loaded[0].DebugIDOnly || !bytes.Equal(loaded[0].Data, emptySourceMap) || !reflect.DeepEqual(loaded[0].CodeFiles, []string{"~/debug.js", "~/release.js"}) {
		t.Fatalf("equivalent selectors did not form one logical map: %+v error=%v", loaded, err)
	}
	loaded, err = db.SourceMaps(ctx, project.ID, "", "", []string{debugID}, int64(len(emptySourceMap)))
	if err != nil || len(loaded) != 1 || !loaded[0].DebugIDOnly {
		t.Fatalf("debug-only logical selection: %+v error=%v", loaded, err)
	}
	// Published selectors remain separate immutable rows; their equivalent map
	// payload counts once even after the stored row count exceeds100.
	if _, err := db.pool.Exec(ctx, `INSERT INTO source_maps(project_id,name,release,dist,debug_id,checksum,size,data,code_files)
        SELECT project_id,name,'variant-'||n,'web',debug_id,checksum,size,data,ARRAY['~/variant.js']
        FROM source_maps CROSS JOIN generate_series(1,101) n WHERE project_id=$1 AND release=''`, project.ID); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.pool.QueryRow(ctx, `SELECT json_agg(to_jsonb(m) ORDER BY id)::text FROM source_maps m`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	loaded, err = db.SourceMaps(ctx, project.ID, "v1", "web", []string{debugID}, int64(len(emptySourceMap)))
	if err != nil || len(loaded) != 1 || loaded[0].DebugIDOnly || !reflect.DeepEqual(loaded[0].CodeFiles, []string{"~/debug.js", "~/release.js", "~/variant.js"}) {
		t.Fatalf("logical count/byte limit counted selector rows: %+v error=%v", loaded, err)
	}
	var after string
	if err := db.pool.QueryRow(ctx, `SELECT json_agg(to_jsonb(m) ORDER BY id)::text FROM source_maps m`).Scan(&after); err != nil || before != after {
		t.Fatalf("coalescing rewrote durable rows: %v", err)
	}
	// Source URL, file identity, and content are each significant. The reader
	// must retain these alternatives so consumer ambiguity rules still apply.
	different := []artifact.SourceMap{
		{Name: "~/other-base/app.js.map", DebugID: debugID, Data: emptySourceMap},
		{Name: "~/app.js.map", DebugID: debugID, Data: []byte(`{"version":3,"sources":[],"names":[],"mappings":";"}`)},
		{Name: "~/app.js.map", DebugID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Data: emptySourceMap},
	}
	for i, m := range different {
		if err := db.SaveBundleSourceMaps(ctx, access, []string{project.ID}, ReleaseVersion(fmt.Sprintf("different-%d", i)), "", []artifact.SourceMap{m}, 1); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err = db.SourceMaps(ctx, project.ID, "v1", "web", []string{debugID, different[2].DebugID}, 1<<20)
	if err != nil || len(loaded) != 4 {
		t.Fatalf("distinct identities/content/source bases were collapsed: %+v error=%v", loaded, err)
	}
}
