//go:build integration

package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// File receipt lifetime is independent of event time and parent history. A wrong
// owner, selector sweep, retry refresh or unbounded pass fails these actual Store
// consumers. Other retention owners cannot remove these standalone file cohorts.
// Only isolated receipt fixtures use SQL; no production seam or mock is needed.
func TestFileRetentionReceiptsPreserveIndependentState(t *testing.T) {
	f := resolutionStore(t)
	if _, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, alertConfig("Independent file alert")); err != nil {
		t.Fatal(err)
	}
	oldMap, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/app.map", "file-life", "web", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", emptySourceMap)
	if err != nil {
		t.Fatal(err)
	}
	keptMap, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/app.map", "file-life", "mobile", oldMap.DebugID, emptySourceMap)
	if err != nil {
		t.Fatal(err)
	}
	debugMap, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "~/debug.map", "", "", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", emptySourceMap)
	if err != nil {
		t.Fatal(err)
	}
	ev := groupEvent(t, 1, "file-receipt", "Keep stored event", time.Now().Add(-400*24*time.Hour))
	ev.Release = "file-life"
	parts := CaptureItems{
		Attachments:   []CaptureAttachment{{Filename: "expired.bin", ContentType: "application/octet-stream", Data: []byte{0, 255, 1}}, {Filename: "retained.bin", ContentType: "application/octet-stream", Data: []byte{1, 0, 254}}},
		EnvelopeItems: []CaptureEnvelopeItem{{Type: "profile", Header: json.RawMessage(`{"type":"profile","phase":"old"}`), Data: []byte("old opaque")}, {Type: "profile", Header: json.RawMessage(`{"type":"profile","phase":"kept"}`), Data: []byte("retained opaque")}},
	}
	issue, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts)
	if err != nil || !fresh {
		t.Fatal(issue, fresh, err)
	}
	if err := f.db.SaveEnvelopeItem(f.ctx, f.project.ID, "", "session", json.RawMessage(`{"type":"session"}`), []byte("standalone opaque")); err != nil {
		t.Fatal(err)
	}
	if n, err := f.db.IngestOTLP(f.ctx, f.project.ID, storeOTLPRecords(t, "files-independent")); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE source_maps SET created_at=now()-interval '2 days' WHERE id::text=ANY($1::text[])`, []string{oldMap.ID, debugMap.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE attachments SET created_at=now()-interval '2 days' WHERE filename='expired.bin';
 UPDATE envelope_items SET created_at=now()-interval '2 days' WHERE item_type='session' OR header->>'phase'='old'`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
	if maps, attachments, items, err := f.db.CleanupFiles(f.ctx, 1); err != nil || maps != 2 || attachments != 1 || items != 2 {
		t.Fatalf("expired files=%d/%d/%d want2/1/2 err=%v", maps, attachments, items, err)
	}
	checkIndependent := func() {
		t.Helper()
		after := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
		for table, checkpoint := range before {
			if table != "source_maps" && table != "attachments" && table != "envelope_items" && after[table] != checkpoint {
				t.Fatal("file expiry/retry changed independent state", table)
			}
		}
	}
	checkIndependent()
	selected, err := f.db.SourceMaps(f.ctx, f.project.ID, "file-life", "web", nil, 1<<20)
	if err != nil || len(selected) != 0 {
		t.Fatal("expired selector still selected", selected, err)
	}
	selected, err = f.db.SourceMaps(f.ctx, f.project.ID, "file-life", "web", []string{oldMap.DebugID}, 1<<20)
	if err != nil || len(selected) != 1 || !bytes.Equal(selected[0].Data, emptySourceMap) {
		t.Fatal("equivalent retained selector bytes lost", selected, err)
	}
	meta, created, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, keptMap.Name, keptMap.Release, keptMap.Dist, keptMap.DebugID, emptySourceMap)
	if err != nil || created || meta.ID != keptMap.ID || !meta.DateCreated.Equal(keptMap.DateCreated) {
		t.Fatal("retained retry changed first receipt", meta, created, err)
	}
	different := append(append([]byte(nil), emptySourceMap...), '\n')
	if _, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, keptMap.Name, keptMap.Release, keptMap.Dist, keptMap.DebugID, different); !errors.Is(err, ErrConflict) {
		t.Fatal("retained conflict lost", err)
	}
	meta, created, err = f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, debugMap.Name, "", "", debugMap.DebugID, emptySourceMap)
	if err != nil || !created || meta.ID == debugMap.ID || !meta.DateCreated.After(debugMap.DateCreated) {
		t.Fatal("expired selector did not create a fresh generation", meta, created, err)
	}
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev, parts); err != nil || fresh || id != issue {
		t.Fatal("child-only retry advanced event/group identity", id, fresh, err)
	}
	if err := f.db.SaveEnvelopeItem(f.ctx, f.project.ID, "", "session", json.RawMessage(`{"type":"session"}`), []byte("standalone opaque")); err != nil {
		t.Fatal(err)
	}
	checkIndependent()
	var maps, attachments, items int
	if err := f.db.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM source_maps),(SELECT count(*) FROM attachments),(SELECT count(*) FROM envelope_items)").Scan(&maps, &attachments, &items); err != nil || maps != 2 || attachments != 2 || items != 3 {
		t.Fatal("fresh file generations missing or duplicated", maps, attachments, items, err)
	}
	if maps, attachments, items, err := f.db.CleanupFiles(f.ctx, 1); err != nil || maps != 0 || attachments != 0 || items != 0 {
		t.Fatal("fresh generation expired by its former receipt", maps, attachments, items, err)
	}
}

func TestFileRetentionBoundedProgress(t *testing.T) {
	f := resolutionStore(t)
	ev := groupEvent(t, 1, "bulk-files", "Keep parent", time.Now())
	groupCapture(t, f.ctx, f.db, f.project, ev)
	if _, err := f.db.pool.Exec(f.ctx, `INSERT INTO source_maps(project_id,name,debug_id,checksum,size,data,created_at)
 SELECT $1,'bulk-'||n,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','bulk',octet_length($2::bytea),$2,now()-interval '2 days' FROM generate_series(1,601) n`, f.project.ID, emptySourceMap); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `INSERT INTO attachments(id,project_id,event_id,filename,content_type,data,created_at)
 SELECT 'attachment-'||n,$1,$2,'bulk-'||n,'application/octet-stream','bytes',now()-interval '2 days' FROM generate_series(1,601) n`, f.project.ID, ev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `INSERT INTO envelope_items(id,project_id,event_id,item_type,header,data,created_at)
 SELECT 'item-'||n,$1,'','session','{}','bytes',now()-interval '2 days' FROM generate_series(1,601) n`, f.project.ID); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
	for _, want := range []int64{250, 250, 101, 0} {
		if maps, attachments, items, err := f.db.CleanupFiles(f.ctx, 1); err != nil || maps != want || attachments != want || items != want {
			t.Fatal("bounded progress", maps, attachments, items, "want", want, err)
		}
	}
	after := groupDatabaseSnapshot(t, f.ctx, f.db, 26)
	for table, checkpoint := range before {
		if table != "source_maps" && table != "attachments" && table != "envelope_items" && after[table] != checkpoint {
			t.Fatal("bulk file expiry changed independent state", table)
		}
	}
}
