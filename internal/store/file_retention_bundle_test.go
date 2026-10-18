//go:build integration

package store

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/artifact"
)

// A multi-project bundle must preserve one atomic generation/alias set on both
// sides of cleanup. Native single-map tests cannot detect a partial project
// retry or a reversed multi-gate order. Exercise the actual compatible writer.
func TestFileRetentionOrdersMultiProjectBundle(t *testing.T) {
	for _, first := range []string{"cleanup", "bundle"} {
		t.Run(first, func(t *testing.T) {
			f := resolutionStore(t)
			other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other file bundle", "javascript")
			if err != nil {
				t.Fatal(err)
			}
			access := ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			refs := []string{other.ID, f.project.Slug, f.project.ID}
			maps := []artifact.SourceMap{
				{Name: "~/a.map", DebugID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Data: emptySourceMap, CodeFiles: []string{"~/a.js"}},
				{Name: "~/b.map", DebugID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Data: emptySourceMap, CodeFiles: []string{"~/b.js"}},
			}
			if err := f.db.SaveBundleSourceMaps(f.ctx, access, refs, "", "web", maps, 4); err != nil {
				t.Fatal(err)
			}
			oldIDs := map[string]bool{}
			for _, project := range []string{f.project.ID, other.ID} {
				files, err := f.db.ListSourceMaps(f.ctx, f.owner.ID, f.org.Slug, project)
				if err != nil || len(files) != 2 {
					t.Fatal(files, err)
				}
				for _, file := range files {
					oldIDs[file.ID] = true
				}
			}
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE source_maps SET created_at=now()-interval '2 days'`); err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
			for i := range maps {
				maps[i].CodeFiles = append(maps[i].CodeFiles, "~/new.js")
			}
			barrier, pid := captureItemsBarrier(t, f)
			if err := lockReleaseArtifacts(f.ctx, barrier, f.project.ID, ""); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			cleaned, written := make(chan error, 1), make(chan error, 1)
			clean := func() {
				n, attachments, items, err := f.db.CleanupFiles(ctx, 1)
				if err == nil && (n != 4 || attachments != 0 || items != 0) {
					err = fmt.Errorf("bundle cleanup counts %d/%d/%d", n, attachments, items)
				}
				cleaned <- err
			}
			write := func() { written <- f.db.SaveBundleSourceMaps(ctx, access, refs, "", "web", maps, 4) }
			if first == "cleanup" {
				go clean()
				groupWaitBlocked(t, ctx, barrier, pid, 1)
				go write()
			} else {
				go write()
				groupWaitBlocked(t, ctx, barrier, pid, 1)
				go clean()
			}
			groupWaitBlocked(t, ctx, barrier, pid, 2)
			var released time.Time
			if err := barrier.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&released); err != nil {
				t.Fatal(err)
			}
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			deleteOrderAwait(t, ctx, cleaned)
			deleteOrderAwait(t, ctx, written)
			want := 0
			if first == "cleanup" {
				want = 2
			}
			for _, project := range []string{f.project.ID, other.ID} {
				files, err := f.db.ListSourceMaps(ctx, f.owner.ID, f.org.Slug, project)
				if err != nil || len(files) != want {
					t.Fatal("bundle generation was partial", project, files, err)
				}
				for _, file := range files {
					if oldIDs[file.ID] || file.DateCreated.Before(released) {
						t.Fatal("bundle acknowledged an expired generation", file)
					}
				}
				selected, err := f.db.SourceMaps(ctx, project, "", "web", []string{maps[0].DebugID, maps[1].DebugID}, 1<<20)
				if err != nil || len(selected) != want {
					t.Fatal("bundle capture view was partial", selected, err)
				}
				aliases := map[string][]string{"~/a.map": {"~/a.js", "~/new.js"}, "~/b.map": {"~/b.js", "~/new.js"}}
				for _, file := range selected {
					if !bytes.Equal(file.Data, emptySourceMap) || !reflect.DeepEqual(file.CodeFiles, aliases[file.Name]) {
						t.Fatal("bundle retry lost bytes or aliases", file)
					}
				}
			}
			after := groupDatabaseSnapshot(t, ctx, f.db, 26)
			for table, checkpoint := range before {
				if table != "source_maps" && after[table] != checkpoint {
					t.Fatal("bundle expiry/retry changed independent state", table)
				}
			}
		})
	}
}
