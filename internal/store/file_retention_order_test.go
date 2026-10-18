//go:build integration

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Observe actual lock queues and durable generations. Sequential expiry cannot
// detect a debug-only retry acknowledging a deleted map, child capture crossing
// cleanup, or a cutoff that moves while waiting. No test-only production seam.
func TestFileRetentionOrdersDebugOnlyUpload(t *testing.T) {
	for _, first := range []string{"cleanup", "upload"} {
		t.Run(first, func(t *testing.T) {
			f := resolutionStore(t)
			old, fresh, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/debug.map", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", emptySourceMap)
			if err != nil || !fresh {
				t.Fatal(old, fresh, err)
			}
			if _, err := f.db.pool.Exec(f.ctx, `UPDATE source_maps SET created_at=now()-interval '2 days'`); err != nil {
				t.Fatal(err)
			}
			barrier, pid := captureItemsBarrier(t, f)
			if first == "cleanup" {
				if err := lockReleaseArtifacts(f.ctx, barrier, f.project.ID, ""); err != nil {
					t.Fatal(err)
				}
			} else if _, err := barrier.Exec(f.ctx, "LOCK TABLE source_maps IN SHARE MODE"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
			defer cancel()
			cleaned := make(chan error, 1)
			type uploadResult struct {
				file    SourceMapFile
				created bool
				err     error
			}
			uploaded := make(chan uploadResult, 1)
			var early *uploadResult
			clean := func() {
				maps, attachments, items, err := f.db.CleanupFiles(ctx, 1)
				if err == nil && (maps != 1 || attachments != 0 || items != 0) {
					err = fmt.Errorf("cleanup counts %d/%d/%d want1/0/0", maps, attachments, items)
				}
				cleaned <- err
			}
			upload := func() {
				m, created, err := f.db.SaveSourceMap(ctx, f.owner.ID, f.org.Slug, f.project.ID, old.Name, "", "", old.DebugID, emptySourceMap)
				uploaded <- uploadResult{m, created, err}
			}
			if first == "cleanup" {
				go clean()
				groupWaitBlocked(t, ctx, barrier, pid, 1)
				go upload()
				// An ungated writer can finish early. Continue through deletion
				// and inspect its actual acknowledgement/bytes, so the regression
				// fails on the lost generation rather than a private lock shape.
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
			waiting:
				for {
					select {
					case result := <-uploaded:
						early = &result
						break waiting
					case <-ticker.C:
						var blocked int
						if _, err := barrier.Exec(ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
							t.Fatal(err)
						}
						if err := barrier.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid))`, pid).Scan(&blocked); err != nil {
							t.Fatal(err)
						}
						if blocked >= 2 {
							break waiting
						}
					case <-ctx.Done():
						t.Fatal("upload neither completed nor reached the observed blocker", ctx.Err())
					}
				}
			} else {
				go upload()
				uploadPID := groupWaitBlocked(t, ctx, barrier, pid, 1)[0]
				go clean()
				groupWaitBlocked(t, ctx, barrier, uploadPID, 1)
			}
			if err := barrier.Rollback(f.ctx); err != nil {
				t.Fatal(err)
			}
			deleteOrderAwait(t, ctx, cleaned)
			var result uploadResult
			if early != nil {
				result = *early
			} else {
				select {
				case result = <-uploaded:
				case <-ctx.Done():
					t.Fatal("upload did not finish", ctx.Err())
				}
			}
			if result.err != nil || result.created != (first == "cleanup") {
				t.Fatal("upload acknowledged the wrong file generation", result)
			}
			maps, err := f.db.SourceMaps(ctx, f.project.ID, "", "", []string{old.DebugID}, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if first == "cleanup" {
				if len(maps) != 1 || result.file.ID == old.ID || !result.file.DateCreated.After(old.DateCreated) {
					t.Fatal("post-expiry upload lost its fresh acknowledged bytes", maps, result)
				}
			} else if len(maps) != 0 || result.file.ID != old.ID {
				t.Fatal("retained retry extended the original receipt lifetime", maps, result)
			}
		})
	}
}

func TestFileRetentionOrdersCaptureAndStandaloneItems(t *testing.T) {
	for _, writer := range []string{"capture", "orphan"} {
		for _, first := range []string{"cleanup", "writer"} {
			t.Run(writer+"/"+first, func(t *testing.T) {
				f := resolutionStore(t)
				ev := groupEvent(t, 1, "file-order", "Retained event", time.Now())
				parts := CaptureItems{Attachments: []CaptureAttachment{{Filename: "receipt.bin", ContentType: "application/octet-stream", Data: []byte{0, 255, 1}}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "profile", Header: json.RawMessage(`{"type":"profile"}`), Data: []byte("opaque")}}}
				write := func(ctx context.Context) error {
					if writer == "orphan" {
						return f.db.SaveEnvelopeItem(ctx, f.project.ID, "", "session", json.RawMessage(`{"type":"session"}`), []byte("orphan"))
					}
					_, _, err := f.db.Ingest(ctx, f.project.ID, ev, parts)
					return err
				}
				if err := write(f.ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.pool.Exec(f.ctx, `UPDATE attachments SET created_at=now()-interval '2 days'; UPDATE envelope_items SET created_at=now()-interval '2 days'`); err != nil {
					t.Fatal(err)
				}
				before := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
				barrier, pid := captureItemsBarrier(t, f)
				if err := lockProjectIssues(f.ctx, barrier, f.project.ID, true); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
				defer cancel()
				cleaned, written := make(chan error, 1), make(chan error, 1)
				clean := func() {
					maps, attachments, items, err := f.db.CleanupFiles(ctx, 1)
					wantAttachments := int64(0)
					if writer == "capture" {
						wantAttachments = 1
					}
					if err == nil && (maps != 0 || attachments != wantAttachments || items != 1) {
						err = fmt.Errorf("cleanup counts %d/%d/%d", maps, attachments, items)
					}
					cleaned <- err
				}
				if first == "cleanup" {
					go clean()
					groupWaitBlocked(t, ctx, barrier, pid, 1)
					go func() { written <- write(ctx) }()
				} else {
					go func() { written <- write(ctx) }()
					groupWaitBlocked(t, ctx, barrier, pid, 1)
					go clean()
				}
				groupWaitBlocked(t, ctx, barrier, pid, 2)
				if err := barrier.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
				deleteOrderAwait(t, ctx, cleaned)
				deleteOrderAwait(t, ctx, written)
				var attachments, items int
				if err := f.db.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM attachments),(SELECT count(*) FROM envelope_items)").Scan(&attachments, &items); err != nil {
					t.Fatal(err)
				}
				wantItems, wantAttachments := 0, 0
				if first == "cleanup" {
					wantItems = 1
					if writer == "capture" {
						wantAttachments = 1
					}
				}
				if attachments != wantAttachments || items != wantItems {
					t.Fatal("child retry did not respect expiry order", attachments, items)
				}
				after := groupDatabaseSnapshot(t, ctx, f.db, 26)
				for table, checkpoint := range before {
					if table != "attachments" && table != "envelope_items" && after[table] != checkpoint {
						t.Fatal("file expiry/retry changed independent state", table)
					}
				}
			})
		}
	}
}
