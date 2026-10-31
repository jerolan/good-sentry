//go:build integration

package store

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The provider owns retained-parent and live project membership boundaries,
// content identities, legacy checksums and first-receipt metadata. These tests
// use actual capture transactions and independent hashes; HTTP tests own the
// public query grammar, credential scopes and binary response headers.
func TestCompatibleAttachmentsIdentityMetadataFiltersAndRetries(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 501, "compatible-attachments", "Attachment metadata", time.Now())
	parts := CaptureItems{Attachments: []CaptureAttachment{
		{Filename: "proof.BIN", ContentType: "application/octet-stream", Type: "event.custom", Data: []byte{0, 255, 10, 13, 128}},
		{Filename: "screenshot.png", ContentType: "image/png", Data: []byte{137, 80, 78, 71, 0, 255}},
		{Filename: "SCREENSHOT.PNG", ContentType: "application/json", Type: "event.view_hierarchy", Data: []byte(`{"tree":true}`)},
		{Filename: "empty.bin", ContentType: "application/octet-stream", Data: []byte{}},
	}}
	issue, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts)
	if err != nil || !fresh {
		t.Fatal("capture fixture", issue, fresh, err)
	}
	native, more, err := f.db.IssueEventAttachments(f.ctx, f.owner.ID, issue, ev.ID, 1)
	if err != nil || more || len(native) != len(parts.Attachments) {
		t.Fatal("native identity fixture", native, more, err)
	}
	nativeIDs := map[string]string{}
	for _, file := range native {
		nativeIDs[file.Filename] = file.ID
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	files, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.Slug, strings.ToUpper(ev.ID), AttachmentFilter{Limit: 100})
	if err != nil || len(files) != len(parts.Attachments) {
		t.Fatal("compatible list", files, err)
	}
	for index, want := range parts.Attachments {
		got := files[index]
		id, err := strconv.ParseInt(got.ID, 10, 64)
		if err != nil || id < 1 || strconv.FormatInt(id, 10) != got.ID || index > 0 && id <= referenceID(files[index-1].ID) {
			t.Fatal("public IDs must be stable decimal identities in page order", files, err)
		}
		nativeID := fmt.Sprintf("%x", sha256.Sum256(append([]byte(f.project.ID+":"+ev.ID+":"+want.Filename+":"), want.Data...)))
		kind := want.Type
		if kind == "" {
			kind = "event.attachment"
		}
		if nativeIDs[want.Filename] != nativeID || got.ID == nativeID || got.EventID != ev.ID || got.Name != want.Filename || got.Type != kind || got.Mimetype != want.ContentType || got.Size != len(want.Data) || got.SHA1 != fmt.Sprintf("%x", sha1.Sum(want.Data)) || !reflect.DeepEqual(got.Headers, map[string]string{"Content-Type": want.ContentType}) || got.DateCreated.IsZero() || got.DateCreated.Location() != time.UTC || got.Data != nil {
			t.Fatalf("native identity/public metadata mismatch: native=%s want=%+v got=%+v", nativeIDs[want.Filename], want, got)
		}
		for _, withData := range []bool{false, true} {
			selected, err := f.db.CompatibleAttachment(f.ctx, f.access, f.project.ID, ev.ID, got.ID, withData)
			if err != nil || selected.ID != got.ID || selected.SHA1 != got.SHA1 || !selected.DateCreated.Equal(got.DateCreated) || withData && !bytes.Equal(selected.Data, want.Data) || !withData && selected.Data != nil {
				t.Fatalf("selected metadata/bytes for %s withData=%v: %+v %v", got.ID, withData, selected, err)
			}
		}
	}
	for _, filter := range []struct {
		query string
		names []string
	}{{"pRoOf", []string{"proof.BIN"}}, {"is:screenshot", []string{"screenshot.png"}}, {"does-not-exist", []string{}}, {".BIN", []string{"proof.BIN", "empty.bin"}}} {
		found, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.Slug, ev.ID, AttachmentFilter{Query: filter.query, Limit: 100})
		if err != nil || found == nil || len(found) != len(filter.names) {
			t.Fatal("name/screenshot filter", filter.query, found, err)
		}
		for index, name := range filter.names {
			if found[index].Name != name {
				t.Fatal("filter returned wrong attachment kind/name", filter.query, found)
			}
		}
	}
	first, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.Slug, ev.ID, AttachmentFilter{Limit: 2})
	if err != nil || len(first) != 3 || first[0].ID != files[0].ID || first[1].ID != files[1].ID {
		t.Fatal("first page omitted lookahead or order", first, err)
	}
	second, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.Slug, ev.ID, AttachmentFilter{Offset: 2, Limit: 2})
	if err != nil || len(second) != 2 || second[0].ID != files[2].ID || second[1].ID != files[3].ID {
		t.Fatal("second page overlaps or skips identities", second, err)
	}
	if after := groupDatabaseSnapshot(t, f.ctx, f.db, 0); !reflect.DeepEqual(after, before) {
		t.Fatal("metadata and binary reads changed stored receipts")
	}
	duplicate := parts.Attachments[0]
	duplicate.Type, duplicate.ContentType = "event.view_hierarchy", "text/plain"
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{duplicate}}); err != nil || fresh {
		t.Fatal("duplicate parent/file retry", fresh, err)
	}
	retried, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.Slug, ev.ID, AttachmentFilter{Limit: 100})
	if err != nil || !reflect.DeepEqual(retried, files) {
		t.Fatal("retry replaced original decimal ID, type, checksum, metadata or receipt", retried, err)
	}
}

func TestCompatibleAttachmentsSelectedParentAndCurrentScope(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 601, "compatible-file-scope", "Attachment scope", time.Now())
	part := CaptureItems{Attachments: []CaptureAttachment{{Filename: "private.bin", ContentType: "application/octet-stream", Data: []byte{0, 255, 128}}}}
	issue, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, part)
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other attachment API", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.Ingest(f.ctx, other.ID, ev, part); err != nil {
		t.Fatal(err)
	}
	otherEvent := groupEvent(t, 602, "compatible-file-other-event", "Other event", time.Now())
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, otherEvent, part); err != nil {
		t.Fatal(err)
	}
	var foreignOrg string
	if err := f.db.pool.QueryRow(f.ctx, "INSERT INTO organizations(slug,name) VALUES('attachment-api-foreign','Foreign attachments') RETURNING id::text").Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	foreign, err := f.db.CreateProject(f.ctx, f.owner.ID, "attachment-api-foreign", "Foreign files", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.Ingest(f.ctx, foreign.ID, ev, part); err != nil {
		t.Fatal(err)
	}
	list := func(access ManagementAccess, project, event string) []CompatibleAttachment {
		t.Helper()
		files, err := f.db.CompatibleAttachments(f.ctx, access, project, event, AttachmentFilter{Limit: 100})
		if err != nil || len(files) != 1 {
			t.Fatal("scope fixture", project, event, files, err)
		}
		return files
	}
	file := list(f.access, f.project.ID, ev.ID)[0]
	otherFile := list(f.access, other.ID, ev.ID)[0]
	otherEventFile := list(f.access, f.project.ID, otherEvent.ID)[0]
	foreignAccess := f.access
	foreignAccess.OrganizationID = foreignOrg
	foreignFile := list(foreignAccess, foreign.ID, ev.ID)[0]
	if file.ID == otherFile.ID || file.ID == otherEventFile.ID || file.ID == foreignFile.ID {
		t.Fatal("public attachment identities collided across retained parents")
	}
	restricted := ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, ProjectIDs: []int64{referenceID(f.project.ID)}}
	list(restricted, f.project.Slug, ev.ID)
	for _, target := range []struct {
		access  ManagementAccess
		project string
		event   string
		file    string
	}{{restricted, other.ID, ev.ID, otherFile.ID}, {f.access, foreign.ID, ev.ID, foreignFile.ID}, {f.access, f.project.ID, otherEvent.ID, file.ID}, {f.access, f.project.ID, ev.ID, otherEventFile.ID}, {f.access, f.project.ID, ev.ID, foreignFile.ID}, {f.access, f.project.ID, strings.Repeat("f", 32), file.ID}} {
		if _, err := f.db.CompatibleAttachment(f.ctx, target.access, target.project, target.event, target.file, true); !errors.Is(err, ErrNotFound) {
			t.Fatal("file escaped selected project/organization/event/grant", target, err)
		}
	}
	for _, target := range []struct {
		access  ManagementAccess
		project string
	}{{restricted, other.ID}, {f.access, foreign.ID}} {
		if _, err := f.db.CompatibleAttachments(f.ctx, target.access, target.project, ev.ID, AttachmentFilter{Limit: 100}); !errors.Is(err, ErrNotFound) {
			t.Fatal("list escaped current project grants", target, err)
		}
	}
	member := triageMember(t, f.ctx, f.db, f.org, "attachment-api-member", "member")
	memberAccess := ManagementAccess{UserID: member.ID, OrganizationID: f.org.ID, AllProjects: true}
	list(memberAccess, f.project.Slug, ev.ID)
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CompatibleAttachments(f.ctx, memberAccess, f.project.ID, ev.ID, AttachmentFilter{Limit: 100}); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale membership listed files", err)
	}
	if _, err := f.db.CompatibleAttachment(f.ctx, memberAccess, f.project.ID, ev.ID, file.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale membership downloaded bytes", err)
	}
	if err := f.db.DeleteCompatibleIssue(f.ctx, f.access, issue); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.ID, ev.ID, AttachmentFilter{Limit: 100}); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted retained parent listed files", err)
	}
}

func TestCompatibleAttachmentsBoundPublicMetadataAndPreserveOriginals(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 650, "compatible-file-metadata-bounds", "Oversized historical metadata", time.Now())
	part := CaptureAttachment{Filename: strings.Repeat("é", 2100), ContentType: "application/" + strings.Repeat("x", 300), Type: strings.Repeat("界", 150), Data: []byte{0, 255, 128, 42}}
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{part}}); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	files, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.ID, ev.ID, AttachmentFilter{Limit: 100})
	if err != nil || len(files) != 1 {
		t.Fatal("oversized metadata fixture", files, err)
	}
	check := func(file CompatibleAttachment) {
		t.Helper()
		var disclosure struct{ MetadataTruncated bool }
		raw, err := json.Marshal(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &disclosure); err != nil {
			t.Fatal(err)
		}
		if utf8.RuneCountInString(file.Name) != 2048 || file.Name != strings.Repeat("é", 2048) || len(file.Mimetype) != 256 || file.Mimetype != part.ContentType[:256] || utf8.RuneCountInString(file.Type) != 128 || file.Type != strings.Repeat("界", 128) || !disclosure.MetadataTruncated || file.Headers["Content-Type"] != file.Mimetype || file.Size != len(part.Data) || file.SHA1 != fmt.Sprintf("%x", sha1.Sum(part.Data)) {
			t.Fatalf("public metadata must be bounded and disclose truncation: name=%d MIME=%d type=%d disclosure=%v", utf8.RuneCountInString(file.Name), len(file.Mimetype), utf8.RuneCountInString(file.Type), disclosure.MetadataTruncated)
		}
	}
	check(files[0])
	for _, withData := range []bool{false, true} {
		file, err := f.db.CompatibleAttachment(f.ctx, f.access, f.project.ID, ev.ID, files[0].ID, withData)
		if err != nil || withData && !bytes.Equal(file.Data, part.Data) || !withData && file.Data != nil {
			t.Fatal("bounded metadata changed selected bytes", withData, err)
		}
		check(file)
	}
	var nativeID, filename, contentType, kind string
	var data []byte
	if err := f.db.pool.QueryRow(f.ctx, "SELECT id,filename,content_type,attachment_type,data FROM attachments WHERE api_id=$1", files[0].ID).Scan(&nativeID, &filename, &contentType, &kind, &data); err != nil {
		t.Fatal(err)
	}
	wantID := fmt.Sprintf("%x", sha256.Sum256(append([]byte(f.project.ID+":"+ev.ID+":"+part.Filename+":"), part.Data...)))
	if nativeID != wantID || filename != part.Filename || contentType != part.ContentType || kind != part.Type || !bytes.Equal(data, part.Data) || !reflect.DeepEqual(groupDatabaseSnapshot(t, f.ctx, f.db, 0), before) {
		t.Fatal("bounded projection changed original metadata, bytes or content identity")
	}
}

func TestCompatibleAttachmentsLegacyChecksumsAndInvalidBoundaries(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 701, "compatible-file-legacy", "Legacy checksum", time.Now())
	payload := []byte{0, 128, 255, 13, 10}
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "legacy.bin", ContentType: "application/octet-stream", Data: payload}}}); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := f.db.pool.QueryRow(f.ctx, "UPDATE attachments SET checksum_sha1='' WHERE project_id=$1 AND event_id=$2 RETURNING api_id::text", f.project.ID, ev.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	files, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.Slug, ev.ID, AttachmentFilter{Limit: 100})
	if err != nil || len(files) != 1 || files[0].SHA1 != fmt.Sprintf("%x", sha1.Sum(payload)) || files[0].Data != nil {
		t.Fatal("legacy list checksum fallback", files, err)
	}
	for _, withData := range []bool{false, true} {
		file, err := f.db.CompatibleAttachment(f.ctx, f.access, f.project.Slug, ev.ID, id, withData)
		if err != nil || file.SHA1 != files[0].SHA1 || withData && !bytes.Equal(file.Data, payload) || !withData && file.Data != nil {
			t.Fatal("legacy selected checksum/bytes", withData, file, err)
		}
	}
	for _, filter := range []AttachmentFilter{{}, {Limit: 101}, {Limit: 1, Offset: -1}, {Limit: 1, Offset: 100000001}, {Limit: 1, Query: strings.Repeat("é", 201)}, {Limit: 1, Query: "a\x00b"}, {Limit: 1, Query: string([]byte{255})}} {
		if _, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.ID, ev.ID, filter); !errors.Is(err, ErrInvalidEventFilter) {
			t.Fatal("invalid provider filter admitted", filter, err)
		}
	}
	for _, eventID := range []string{"latest", "bad-event", strings.Repeat("a", 31)} {
		if _, err := f.db.CompatibleAttachments(f.ctx, f.access, f.project.ID, eventID, AttachmentFilter{Limit: 1}); !errors.Is(err, ErrInvalidEventFilter) {
			t.Fatal("invalid/implicit parent admitted", eventID, err)
		}
	}
	for _, fileID := range []string{"0", "01", "-1", "+1", "9223372036854775808", strings.Repeat("a", 64)} {
		if _, err := f.db.CompatibleAttachment(f.ctx, f.access, f.project.ID, ev.ID, fileID, false); !errors.Is(err, ErrInvalidEventFilter) {
			t.Fatal("invalid native/noncanonical public ID admitted", fileID, err)
		}
	}
	if after := groupDatabaseSnapshot(t, f.ctx, f.db, 0); !reflect.DeepEqual(after, before) {
		t.Fatal("fallback reads or rejected requests changed original rows/checksum")
	}
}

func TestCompatibleAttachmentMigration032PreservesNativeReceiptsAndReopens(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 801, "compatible-file-upgrade", "Original attachment receipt", time.Now())
	payload := []byte{0, 255, 17, 128}
	issue, _, err := f.db.Ingest(f.ctx, f.project.ID, ev, CaptureItems{Attachments: []CaptureAttachment{{Filename: "before032.bin", ContentType: "application/octet-stream", Data: payload}}})
	if err != nil {
		t.Fatal(err)
	}
	native, _, err := f.db.IssueEventAttachments(f.ctx, f.owner.ID, issue, ev.ID, 1)
	if err != nil || len(native) != 1 {
		t.Fatal(native, err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "DROP INDEX attachments_event_api; ALTER TABLE attachments DROP COLUMN api_id, DROP COLUMN attachment_type, DROP COLUMN checksum_sha1; DELETE FROM schema_migrations WHERE version=32"); err != nil {
		t.Fatal(err)
	}
	oldAttachments := func(db *Store) string {
		t.Helper()
		var rows string
		if err := db.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(a)-'api_id'-'attachment_type'-'checksum_sha1' ORDER BY id)::text FROM attachments a`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 31)
	before["attachments"] = oldAttachments(f.db)
	var stableID string
	for range 2 {
		reopened, err := Open(f.ctx, f.db.pool.Config().ConnString())
		if err != nil {
			t.Fatal(err)
		}
		after := groupDatabaseSnapshot(t, f.ctx, reopened, 31)
		after["attachments"] = oldAttachments(reopened)
		if !reflect.DeepEqual(after, before) {
			reopened.Close()
			t.Fatal("032 changed preexisting rows, native hashes, bytes or receipt clocks")
		}
		files, err := reopened.CompatibleAttachments(f.ctx, f.access, f.project.Slug, ev.ID, AttachmentFilter{Limit: 100})
		if err != nil || len(files) != 1 || files[0].Type != "event.attachment" || files[0].SHA1 != fmt.Sprintf("%x", sha1.Sum(payload)) || stableID != "" && files[0].ID != stableID || files[0].DateCreated != native[0].DateCreated {
			reopened.Close()
			t.Fatal("historical projection/checksum/default kind or stable reopened ID", files, err)
		}
		stableID = files[0].ID
		var persistedSHA1, nativeID, receipt string
		if err := reopened.pool.QueryRow(f.ctx, "SELECT checksum_sha1,id,(SELECT checksum FROM schema_migrations WHERE version=32) FROM attachments WHERE api_id=$1", stableID).Scan(&persistedSHA1, &nativeID, &receipt); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		migration, err := migrations.ReadFile("migrations/032_attachment_api.sql")
		if err != nil || persistedSHA1 != "" || nativeID != native[0].ID || receipt != fmt.Sprintf("%x", sha256.Sum256(migration)) {
			reopened.Close()
			t.Fatal("migration/checksum fallback rewrote existing identity or lacks receipt", persistedSHA1, nativeID, receipt, err)
		}
		reopened.Close()
	}
}
