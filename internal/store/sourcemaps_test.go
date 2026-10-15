//go:build integration

package store

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
)

var emptySourceMap = []byte(`{"version":3,"sources":[],"names":[],"mappings":""}`)

func TestSourceMapsMatchBuildAndProjectAfterRestart(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	for _, f := range []struct{ name, release, dist, debugID string }{
		{"~/app.js.map", "v1", "web", ""},
		{"~/app.js.map", "v1", "mobile", ""},
		{"~/app.js.map", "v2", "web", ""},
		{"~/debug.js.map", "v3", "mobile", "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA"},
	} {
		meta, created, err := db.SaveSourceMap(ctx, user.ID, org.Slug, project.ID, f.name, f.release, f.dist, f.debugID, emptySourceMap)
		if err != nil || !created || meta.Size != int64(len(emptySourceMap)) || meta.Checksum != fmt.Sprintf("%x", sha256.Sum256(emptySourceMap)) {
			t.Fatalf("save map metadata: %+v created=%v err=%v", meta, created, err)
		}
		if f.debugID != "" && meta.DebugID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Fatalf("debug ID not normalized: %q", meta.DebugID)
		}
	}
	other, err := db.CreateProject(ctx, user.ID, org.Slug, "Other maps", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.SaveSourceMap(ctx, user.ID, org.Slug, other.ID, "~/wrong-project.js.map", "v1", "web", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", emptySourceMap); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	for _, tc := range []struct {
		release, dist string
		ids           []string
		want          []string
	}{
		{"v1", "web", nil, []string{"~/app.js.map"}},
		{"v1", "absent", nil, nil},
		{"", "", nil, nil},
		{"", "", []string{"AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA"}, []string{"~/debug.js.map"}},
		{"v1", "web", []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, []string{"~/app.js.map", "~/debug.js.map"}},
	} {
		maps, err := reopened.SourceMaps(ctx, project.ID, tc.release, tc.dist, tc.ids, 1<<20)
		if err != nil || len(maps) != len(tc.want) {
			t.Fatalf("build match release=%q dist=%q ids=%v: %+v, %v", tc.release, tc.dist, tc.ids, maps, err)
		}
		for i, m := range maps {
			if m.Name != tc.want[i] || !bytes.Equal(m.Data, emptySourceMap) {
				t.Fatalf("durable map bytes/project selection wrong: %+v", maps)
			}
		}
	}
	list, err := reopened.ListSourceMaps(ctx, user.ID, org.Slug, project.ID)
	if err != nil || len(list) != 4 {
		t.Fatalf("metadata list: %+v, %v", list, err)
	}
}

func TestSourceMapRetriesConflictAndLoadBudget(t *testing.T) {
	ctx, db, user, org, project := telemetryStore(t)
	var wg sync.WaitGroup
	created, errs := make(chan bool, 8), make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, first, err := db.SaveSourceMap(ctx, user.ID, org.Slug, project.ID, "~/app.js.map", "v1", "web", "", emptySourceMap)
			created <- first
			errs <- err
		})
	}
	wg.Wait()
	close(created)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	firstUploads := 0
	for first := range created {
		if first {
			firstUploads++
		}
	}
	if firstUploads != 1 {
		t.Fatalf("concurrent retry created %d rows, want 1", firstUploads)
	}
	if _, _, err := db.SaveSourceMap(ctx, user.ID, org.Slug, project.ID, "~/app.js.map", "v1", "web", "", []byte(`{"version":3,"sources":[],"names":[],"mappings":";"}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting upload overwrote original: %v", err)
	}
	if maps, err := db.SourceMaps(ctx, project.ID, "v1", "web", nil, int64(len(emptySourceMap)-1)); !errors.Is(err, ErrArtifactLimit) || len(maps) != 0 {
		t.Fatalf("byte limit silently truncated/loaded maps: %+v, %v", maps, err)
	}
	maps, err := db.SourceMaps(ctx, project.ID, "v1", "web", nil, int64(len(emptySourceMap)))
	if err != nil || len(maps) != 1 || !bytes.Equal(maps[0].Data, emptySourceMap) {
		t.Fatalf("retry content changed or exact budget rejected: %+v, %v", maps, err)
	}
	for i := range 100 {
		if _, _, err := db.SaveSourceMap(ctx, user.ID, org.Slug, project.ID, fmt.Sprintf("~/chunk-%03d.js.map", i), "many", "", "", emptySourceMap); err != nil {
			t.Fatal(err)
		}
	}
	if maps, err := db.SourceMaps(ctx, project.ID, "many", "", nil, 1<<20); err != nil || len(maps) != 100 {
		t.Fatalf("100 map boundary rejected: count=%d, %v", len(maps), err)
	}
	if _, _, err := db.SaveSourceMap(ctx, user.ID, org.Slug, project.ID, "~/chunk-100.js.map", "many", "", "", emptySourceMap); err != nil {
		t.Fatal(err)
	}
	if maps, err := db.SourceMaps(ctx, project.ID, "many", "", nil, 1<<20); !errors.Is(err, ErrArtifactLimit) || len(maps) != 0 {
		t.Fatalf("map count limit silently truncated: count=%d, %v", len(maps), err)
	}
}

func TestSourceMapPermissionsUseActualMembershipAndProjectScope(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	if _, _, err := db.SaveSourceMap(ctx, owner.ID, org.Slug, project.ID, "~/app.js.map", "v1", "", "", emptySourceMap); err != nil {
		t.Fatal(err)
	}
	var member, outsider, secondOrg string
	if err := db.pool.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES('member@maps.test',$1,'Member') RETURNING id::text", []byte("already-hashed")).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", org.ID, member); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.SaveSourceMap(ctx, member, org.Slug, project.ID, "~/member.js.map", "v1", "", "", emptySourceMap); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member uploaded source map: %v", err)
	}
	if maps, err := db.ListSourceMaps(ctx, member, org.Slug, project.ID); err != nil || len(maps) != 1 {
		t.Fatalf("member metadata read denied: %+v, %v", maps, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE memberships SET role='admin' WHERE organization_id=$1 AND user_id=$2", org.ID, member); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.SaveSourceMap(ctx, member, org.Slug, project.ID, "~/admin.js.map", "v1", "", "", emptySourceMap); err != nil {
		t.Fatalf("admin upload denied: %v", err)
	}
	if err := db.pool.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES('outsider@maps.test',$1,'Outsider') RETURNING id::text", []byte("already-hashed")).Scan(&outsider); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('other-maps','Other maps') RETURNING id::text").Scan(&secondOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", secondOrg, outsider); err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateProject(ctx, outsider, "other-maps", "Private maps", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.SaveSourceMap(ctx, outsider, org.Slug, project.ID, "~/attack.js.map", "v1", "", "", emptySourceMap); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other organization identity wrote map: %v", err)
	}
	if _, err := db.ListSourceMaps(ctx, outsider, org.Slug, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other organization identity listed map: %v", err)
	}
	if _, _, err := db.SaveSourceMap(ctx, owner.ID, org.Slug, other.ID, "~/attack.js.map", "v1", "", "", emptySourceMap); !errors.Is(err, ErrNotFound) {
		t.Fatalf("project outside selected organization accepted: %v", err)
	}
	if _, err := db.ListSourceMaps(ctx, owner.ID, org.Slug, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("project outside selected organization listed: %v", err)
	}
}
