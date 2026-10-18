//go:build integration

package store

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Reads own metadata/bytes, live scope and selected-parent expiry semantics.
// Write-only CaptureItems tests cannot detect a missing reader, cross-event
// lookup, stale membership, payload-in-metadata or unbounded list. Real Store
// fixtures and current PG rows need no production test seam or mock.
func TestIssueAttachmentMetadataBytesAndCurrentScope(t *testing.T) {
	f := resolutionStore(t)
	payload := []byte{0, 255, 1, 254, 2, 0, 128, 42}
	longName, longType := strings.Repeat("界", 3000), strings.Repeat("x", 300)
	ev := groupEvent(t, 1, "attachment-read", "Stored attachment parent", time.Now())
	issue, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "download.bin", ContentType: "application/octet-stream", Data: payload}, {Filename: "empty.txt", ContentType: "text/plain", Data: []byte{}}, {Filename: longName, ContentType: longType, Data: []byte{1}}}})
	if err != nil {
		t.Fatal(err)
	}
	otherEvent := groupEvent(t, 2, "other-attachment", "Other attachment parent", time.Now())
	otherIssue, _, err := f.db.Ingest(f.ctx, f.project.ID, otherEvent, CaptureItems{Attachments: []CaptureAttachment{{Filename: "download.bin", Data: payload}}})
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other attachment project", "go")
	if err != nil {
		t.Fatal(err)
	}
	foreignIssue, _, err := f.db.Ingest(f.ctx, otherProject.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "download.bin", Data: payload}}})
	if err != nil {
		t.Fatal(err)
	}
	var member, outsider string
	for _, actor := range []struct {
		email, name string
		id          *string
	}{{"member@attachments.test", "Member", &member}, {"outsider@attachments.test", "Outsider", &outsider}} {
		if err := f.db.pool.QueryRow(f.ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text", actor.email, []byte("already-hashed"), actor.name).Scan(actor.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.pool.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", f.org.ID, member); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 27)
	files, more, err := f.db.IssueEventAttachments(f.ctx, member, issue, strings.ToUpper(ev.ID), 1)
	if err != nil || more || len(files) != 3 {
		t.Fatal("member cannot read exact event attachment metadata", files, more, err)
	}
	var selected Attachment
	for _, file := range files {
		if len(file.Data) != 0 || file.DateCreated.IsZero() {
			t.Fatal("metadata omitted receipt or included bytes", file)
		}
		switch file.Filename {
		case "download.bin":
			selected = file
			if file.Size != len(payload) || file.MetadataTruncated {
				t.Fatal(file)
			}
		case "empty.txt":
			if file.Size != 0 || file.MetadataTruncated {
				t.Fatal(file)
			}
		default:
			if !file.MetadataTruncated || utf8.RuneCountInString(file.Filename) != 2048 || len(file.ContentType) != 256 || file.Size != 1 {
				t.Fatal("metadata projection is not bounded/disclosed", file)
			}
		}
		data, err := f.db.IssueEventAttachment(f.ctx, member, issue, ev.ID, file.ID, true)
		if err != nil || len(data.Data) != file.Size {
			t.Fatal("selected bytes missing", data, err)
		}
		if file.Filename == "download.bin" && !bytes.Equal(data.Data, payload) {
			t.Fatal("binary changed", data.Data)
		}
	}
	if selected.ID == "" {
		t.Fatal("binary metadata missing")
	}
	uuid := ev.ID[:8] + "-" + ev.ID[8:12] + "-" + ev.ID[12:16] + "-" + ev.ID[16:20] + "-" + ev.ID[20:]
	meta, err := f.db.IssueEventAttachment(f.ctx, f.owner.ID, issue, uuid, strings.ToUpper(selected.ID), false)
	if err != nil || len(meta.Data) != 0 || meta.ID != selected.ID || meta.Size != len(payload) {
		t.Fatal("metadata-only selected read or reference normalization failed", meta, err)
	}
	otherFiles, _, err := f.db.IssueEventAttachments(f.ctx, f.owner.ID, otherIssue, otherEvent.ID, 1)
	if err != nil || len(otherFiles) != 1 {
		t.Fatal(otherFiles, err)
	}
	foreignFiles, _, err := f.db.IssueEventAttachments(f.ctx, f.owner.ID, foreignIssue, ev.ID, 1)
	if err != nil || len(foreignFiles) != 1 {
		t.Fatal(foreignFiles, err)
	}
	for _, bad := range []struct{ actor, target, event, file string }{{outsider, issue, ev.ID, selected.ID}, {f.owner.ID, otherIssue, ev.ID, selected.ID}, {f.owner.ID, issue, otherEvent.ID, otherFiles[0].ID}, {f.owner.ID, issue, ev.ID, otherFiles[0].ID}, {f.owner.ID, issue, ev.ID, foreignFiles[0].ID}, {f.owner.ID, issue, strings.Repeat("f", 32), selected.ID}} {
		if _, err := f.db.IssueEventAttachment(f.ctx, bad.actor, bad.target, bad.event, bad.file, true); !errors.Is(err, ErrNotFound) {
			t.Fatal("attachment escaped selected membership/group/event", bad, err)
		}
	}
	for _, ref := range []string{"latest", "invalid"} {
		if _, _, err := f.db.IssueEventAttachments(f.ctx, f.owner.ID, issue, ref, 1); !errors.Is(err, ErrInvalidEventFilter) {
			t.Fatal("implicit/invalid event selected", ref, err)
		}
	}
	if _, err := f.db.IssueEventAttachment(f.ctx, f.owner.ID, issue, ev.ID, "invalid", true); !errors.Is(err, ErrInvalidEventFilter) {
		t.Fatal("invalid attachment reference accepted", err)
	}
	for _, page := range []int{0, 1000001} {
		if _, _, err := f.db.IssueEventAttachments(f.ctx, f.owner.ID, issue, ev.ID, page); !errors.Is(err, ErrInvalidEventFilter) {
			t.Fatal("invalid metadata page accepted", page, err)
		}
	}
	if after := groupDatabaseSnapshot(t, f.ctx, f.db, 27); !reflect.DeepEqual(after, before) {
		t.Fatal("attachment reads changed stored data")
	}
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{issue, otherIssue}); err != nil {
		t.Fatal(err)
	}
	if data, err := f.db.IssueEventAttachment(f.ctx, member, otherIssue, ev.ID, selected.ID, true); err != nil || !bytes.Equal(data.Data, payload) {
		t.Fatal("canonical merged alias cannot read its selected event", data, err)
	}
	if _, err := f.db.UnmergeIssue(f.ctx, f.owner.ID, issue, []string{otherIssue}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.IssueEventAttachment(f.ctx, member, otherIssue, ev.ID, selected.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("former merged alias leaked the now-detached event", err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, member); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.IssueEventAttachments(f.ctx, member, issue, ev.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked membership listed attachment metadata", err)
	}
	if _, err := f.db.IssueEventAttachment(f.ctx, member, issue, ev.ID, selected.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked membership read file bytes", err)
	}
}

func TestIssueAttachmentPagesExpiryAndFreshReceipt(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 1, "attachment-pages", "Keep parent across file expiry", time.Now())
	parts := CaptureItems{}
	for i := range 28 {
		parts.Attachments = append(parts.Attachments, CaptureAttachment{Filename: fmt.Sprintf("file-%02d.bin", i), Data: []byte{0, byte(i), 255}})
	}
	issue, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts)
	if err != nil {
		t.Fatal(err)
	}
	var all []Attachment
	for page, count := range []int{25, 3, 0} {
		files, more, err := f.db.IssueEventAttachments(f.ctx, f.owner.ID, issue, ev.ID, page+1)
		if err != nil || len(files) != count || more != (page == 0) {
			t.Fatal("attachment page/hasMore wrong", page+1, len(files), more, err)
		}
		all = append(all, files...)
	}
	seen := map[string]bool{}
	for i, file := range all {
		if seen[file.ID] || len(file.Data) != 0 || i > 0 && (file.DateCreated.Before(all[i-1].DateCreated) || file.DateCreated.Equal(all[i-1].DateCreated) && file.ID < all[i-1].ID) {
			t.Fatal("metadata pages overlap, read blobs or reorder receipts", file)
		}
		seen[file.ID] = true
	}
	if _, err := f.db.pool.Exec(f.ctx, "UPDATE attachments SET created_at=now()-interval '2 days'"); err != nil {
		t.Fatal(err)
	}
	if maps, attachments, items, err := f.db.CleanupFiles(f.ctx, 1); err != nil || maps != 0 || attachments != 28 || items != 0 {
		t.Fatal(maps, attachments, items, err)
	}
	if files, more, err := f.db.IssueEventAttachments(f.ctx, f.owner.ID, issue, ev.ID, 1); err != nil || more || files == nil || len(files) != 0 {
		t.Fatal("retained event with expired children not readable as empty", files, more, err)
	}
	if _, err := f.db.IssueEventAttachment(f.ctx, f.owner.ID, issue, ev.ID, all[0].ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired file still readable", err)
	}
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts); err != nil || fresh {
		t.Fatal("retained receipt retry failed", fresh, err)
	}
	if file, err := f.db.IssueEventAttachment(f.ctx, f.owner.ID, issue, ev.ID, all[0].ID, true); err != nil || file.ID != all[0].ID || !file.DateCreated.After(all[0].DateCreated) || len(file.Data) != 3 {
		t.Fatal("same hash did not resolve the fresh child receipt", file, err)
	}
}

func TestAttachmentReadMigration027PreservesAndReopens(t *testing.T) {
	f := resolutionStore(t)
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, groupEvent(t, 1, "before027", "Keep original file", time.Now()), CaptureItems{Attachments: []CaptureAttachment{{Filename: "before027.bin", Data: []byte{0, 255}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "DROP INDEX attachments_event_history; DELETE FROM schema_migrations WHERE version=27"); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
	for range 2 {
		reopened, err := Open(f.ctx, f.db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		if after := groupDatabaseSnapshot(t, f.ctx, reopened, 26); !reflect.DeepEqual(after, before) {
			t.Fatal("027 changed old rows or migration receipts")
		}
		var receipt, index string
		if err := reopened.pool.QueryRow(f.ctx, "SELECT checksum,to_regclass('attachments_event_history')::text FROM schema_migrations WHERE version=27").Scan(&receipt, &index); err != nil {
			t.Fatal(err)
		}
		body, err := migrations.ReadFile("migrations/027_attachment_reads.sql")
		if err != nil || index != "attachments_event_history" || receipt != fmt.Sprintf("%x", sha256.Sum256(body)) {
			t.Fatal("027 receipt/index absent or changed", index, err)
		}
		reopened.Close()
	}
}
