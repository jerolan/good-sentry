//go:build integration

package store

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// The real expiry owner must recount stored partitions without resetting fresh
// capture progress, routes or triage history. Existing grouping tests never
// physically expire receipts and cannot catch a cleanup that deletes an empty
// group or advances a mute. PostgreSQL and public store reads need no test seam.
func TestEventRetentionRecountsPartitionsAndPreservesEmptyIssueHistory(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	expired := groupEvent(t, 1, "retention-mixed", "Expired future event", at.Add(365*24*time.Hour))
	retained := groupEvent(t, 2, "retention-mixed", "Retained old event", at.Add(-365*24*time.Hour))
	empty := groupEvent(t, 3, "retention-empty", "Empty issue history", at)
	var roots []string
	for i, label := range []string{"expired", "retained", "empty"} {
		incoming := expired
		if i == 1 {
			incoming = retained
		} else if i == 2 {
			incoming = empty
		}
		id, fresh, err := db.Ingest(ctx, project.ID, incoming, deleteOrderParts(label))
		if err != nil || !fresh {
			t.Fatal(fresh, err)
		}
		roots = append(roots, id)
	}
	if roots[0] != roots[1] || roots[0] == roots[2] {
		t.Fatal("invalid mixed/empty fixture", roots)
	}
	resolved, err := db.UpdateIssue(ctx, owner.ID, roots[0], IssueUpdate{Status: "resolved"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddComment(ctx, owner.ID, roots[2], "Keep this investigation after event expiry"); err != nil {
		t.Fatal(err)
	}
	muted, err := db.UpdateIssue(ctx, owner.ID, roots[2], IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 3), Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: owner.ID}}})
	if err != nil || muted.Ignore == nil || muted.Ignore.Remaining == nil || *muted.Ignore.Remaining != 3 {
		t.Fatal("invalid mute fixture", muted, err)
	}
	if _, err := db.IngestLogs(ctx, project.ID, []byte(fmt.Sprintf(`{"items":[{"timestamp":1,"trace_id":%q,"level":"info","body":"independent log","attributes":{}}]}`, expired.TraceID))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestOTLP(ctx, project.ID, storeOTLPRecords(t, "independent canonical bytes")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Ingest(ctx, project.ID, operationEvent(10, time.Now(), `[{"op":"db"}]`), deleteOrderParts("transaction")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestSpans(ctx, project.ID, spanBatch(t, spanFixture("1111111111111111", true, 1, 2)), 1); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveEnvelopeItem(ctx, project.ID, "independent-orphan", "future", []byte(`{"type":"future"}`), []byte{0, 255}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE event_id=ANY($1::text[])`, []string{expired.ID, empty.ID}); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 24)
	if n, err := db.CleanupEvents(ctx, 1); err != nil || n != 2 {
		t.Fatalf("expired error receipts=%d want2 err=%v", n, err)
	}
	after := groupDatabaseSnapshot(t, ctx, db, 24)
	for table, checkpoint := range before {
		switch table {
		case "issues", "events", "attachments", "envelope_items", "transaction_span_operations":
			continue
		}
		if actual := after[table]; actual != checkpoint {
			t.Fatalf("independent %s changed", table)
		}
	}
	mixed, err := db.IssueDetail(ctx, owner.ID, roots[0])
	if err != nil || mixed.Issue.Count != 1 || mixed.Issue.Title != retained.Title || len(mixed.Events) != 1 || mixed.Events[0].ID != retained.ID || !mixed.Issue.FirstSeen.Equal(retained.Timestamp) || !mixed.Issue.LastSeen.Equal(retained.Timestamp) {
		t.Fatalf("retained partition summary/detail=%+v err=%v", mixed, err)
	}
	if mixed.Issue.Status != resolved.Status || !reflect.DeepEqual(mixed.Issue.Resolution, resolved.Resolution) || !reflect.DeepEqual(mixed.Issue.ResolvedAt, resolved.ResolvedAt) || captureProgressCounter(t, ctx, db, roots[0]) != 2 {
		t.Fatal("recount rewrote resolution/progress")
	}
	detail, err := db.IssueDetail(ctx, owner.ID, roots[2])
	if err != nil || detail.Issue.Count != 0 || string(detail.Event) != "{}" || len(detail.Events) != 0 || len(detail.Comments) != 1 || detail.Comments[0].Text != "Keep this investigation after event expiry" || detail.Analytics.UserCount != 0 {
		t.Fatalf("empty issue/history not readable=%+v err=%v", detail, err)
	}
	if detail.Issue.Title != muted.Title || !detail.Issue.FirstSeen.Equal(muted.FirstSeen) || !detail.Issue.LastSeen.Equal(muted.LastSeen) || detail.Issue.Status != "ignored" || !reflect.DeepEqual(detail.Issue.Ignore, muted.Ignore) || !reflect.DeepEqual(detail.Issue.AssignedTo, muted.AssignedTo) || captureProgressCounter(t, ctx, db, roots[2]) != 1 {
		t.Fatal("empty partition lost historical summary/mute/assignment/progress")
	}
	var events, attachments, opaque int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM events),(SELECT count(*) FROM attachments),(SELECT count(*) FROM envelope_items)`).Scan(&events, &attachments, &opaque); err != nil || !reflect.DeepEqual([]int{events, attachments, opaque}, []int{2, 2, 3}) {
		t.Fatal("dependent or independent bytes changed incorrectly", events, attachments, opaque, err)
	}
	if items, _, err := db.Issues(ctx, owner.ID, org.Slug, IssueFilter{Project: project.ID, Status: "all", Release: expired.Release}); err != nil || len(items) != 0 {
		t.Fatal("expired occurrence still matches native filter", len(items), err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, retained); err != nil || fresh {
		t.Fatal("retained duplicate changed identity", fresh, err)
	}
	if id, fresh, err := db.Ingest(ctx, project.ID, empty, deleteOrderParts("fresh")); err != nil || !fresh || id != roots[2] {
		t.Fatal("post-expiry receipt lost its active group", id, fresh, err)
	}
	if _, fresh, err := db.Ingest(ctx, project.ID, empty); err != nil || fresh {
		t.Fatal("fresh receipt retry advanced progress", fresh, err)
	}
	afterCapture := triageIssue(t, ctx, db, owner.ID, roots[2])
	if afterCapture.Count != 1 || afterCapture.Status != "ignored" || afterCapture.Ignore == nil || *afterCapture.Ignore.Remaining != 2 || captureProgressCounter(t, ctx, db, roots[2]) != 2 {
		t.Fatal("fresh capture did not advance the old mute once", afterCapture)
	}
	firstSeen := 0
	for _, activity := range triageActivities(t, ctx, db, owner.ID, roots[2]) {
		if activity.Type == "first_seen" {
			firstSeen++
		}
	}
	if firstSeen != 1 {
		t.Fatal("expiry repeated lifetime first-capture activity", firstSeen)
	}
}

func TestEventRetentionBoundsAndRecountsEachOwnerOnce(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := groupCapture(t, ctx, db, project, groupEvent(t, 1, "retention-bound", "Bounded owner", time.Now()))
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,data)
 SELECT $1,lpad(to_hex(n),32,'0'),$2,$2,'error','Bounded owner','error',now()+interval '1 year','{}' FROM generate_series(2,601) n`, project.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days'; UPDATE issues SET count=601,receipt_count=601`); err != nil {
		t.Fatal(err)
	}
	for _, pass := range []struct{ removed, remaining int64 }{{250, 351}, {250, 101}, {101, 0}, {0, 0}} {
		if n, err := db.CleanupEvents(ctx, 1); err != nil || n != pass.removed {
			t.Fatalf("bounded error cleanup=%d want=%d err=%v", n, pass.removed, err)
		}
		actual := triageIssue(t, ctx, db, owner.ID, id)
		if actual.Count != pass.remaining || captureProgressCounter(t, ctx, db, id) != 601 {
			t.Fatalf("shared owner recounted more than once: count=%d want=%d receipts=%d", actual.Count, pass.remaining, captureProgressCounter(t, ctx, db, id))
		}
	}
}

func TestEventRetentionPreservesMergedOriginsAndEmptyOriginReuse(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	a := groupEvent(t, 1, "retention-origin-a", "Origin A", at)
	b := groupEvent(t, 2, "retention-origin-b", "Origin B", at)
	kept := groupEvent(t, 3, "retention-origin-a", "Kept origin A", at.Add(time.Second))
	aID := groupCapture(t, ctx, db, project, a)
	bID := groupCapture(t, ctx, db, project, b)
	groupCapture(t, ctx, db, project, kept)
	merged, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{aID, bID})
	if err != nil || !merged.Changed || merged.Issue.ID != aID {
		t.Fatal("invalid merged fixture", merged, err)
	}
	aReceipts, bReceipts := captureProgressCounter(t, ctx, db, aID), captureProgressCounter(t, ctx, db, bID)
	if _, err := db.pool.Exec(ctx, `UPDATE events SET created_at=now()-interval '2 days' WHERE event_id=ANY($1::text[])`, []string{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 24)
	if n, err := db.CleanupEvents(ctx, 1); err != nil || n != 2 {
		t.Fatal("merged receipt cleanup failed", n, err)
	}
	root := triageIssue(t, ctx, db, owner.ID, aID)
	if root.Count != 1 || root.GroupSize != 2 || root.Title != kept.Title || captureProgressCounter(t, ctx, db, aID) != aReceipts || captureProgressCounter(t, ctx, db, bID) != bReceipts {
		t.Fatal("merged topology/progress changed", root)
	}
	origins, err := db.IssueOrigins(ctx, owner.ID, aID, "", 1)
	if err != nil || origins.Total != 2 || len(origins.Data) != 2 || origins.Data[0].Count != 1 || origins.Data[1].Count != 0 || origins.Data[1].FirstSeen != nil || origins.Data[1].LastSeen != nil || !origins.Data[1].ReceivesNewEvents {
		t.Fatal("empty origin or active route lost", origins, err)
	}
	after := groupDatabaseSnapshot(t, ctx, db, 24)
	for _, table := range []string{"issue_hash_routes", "issue_activity", "issue_ignores", "issue_resolutions", "comments", "issue_assignments"} {
		if after[table] != before[table] {
			t.Fatal("retention changed independent group history", table)
		}
	}
	if result, err := db.UnmergeIssue(ctx, owner.ID, aID, []string{bID}); err != nil || !result.Changed {
		t.Fatal("empty origin cannot detach", result, err)
	}
	if detail, err := db.IssueDetail(ctx, owner.ID, bID); err != nil || detail.Issue.Count != 0 || len(detail.Events) != 0 {
		t.Fatal("detached empty origin is not readable", detail, err)
	}
	if id, fresh, err := db.Ingest(ctx, project.ID, b); err != nil || !fresh || id != bID {
		t.Fatal("old identity lost its detached active origin", id, fresh, err)
	}
	if triageIssue(t, ctx, db, owner.ID, bID).Count != 1 || captureProgressCounter(t, ctx, db, bID) != bReceipts+1 || captureProgressCounter(t, ctx, db, aID) != aReceipts {
		t.Fatal("fresh capture advanced the wrong origin")
	}
}

func TestEventRetentionRemovesUnownedNontransactionReceipt(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data,created_at)
 VALUES($1,'unowned-message','message','Historical unowned message','info',now(),'{}',now()-interval '2 days')`, project.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := db.CleanupEvents(ctx, 1); err != nil || n != 1 {
		t.Fatal("unowned nontransaction receipt was stranded", n, err)
	}
	var issues, events int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM issues),(SELECT count(*) FROM events)`).Scan(&issues, &events); err != nil || issues != 0 || events != 0 {
		t.Fatal("expiry invented an owner or kept the receipt", issues, events, err)
	}
}
