//go:build integration

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func hashSamples(t *testing.T, f resolutionFixture, issue string, filter CompatibleIssueHashFilter) []CompatibleIssueHash {
	t.Helper()
	items, err := f.db.CompatibleIssueHashes(f.ctx, f.access, issue, filter)
	if err != nil {
		t.Fatalf("hash samples for %s: %v", issue, err)
	}
	return items
}

func hashForSample(t *testing.T, items []CompatibleIssueHash, eventID string) string {
	t.Helper()
	for _, item := range items {
		if item.Latest != nil && item.Latest.ID == eventID {
			return item.ID
		}
	}
	t.Fatalf("no hash has actual latest event %s", eventID)
	return ""
}

func hashOnlyChanges(t *testing.T, f resolutionFixture, before map[string]string, allowed ...string) {
	t.Helper()
	after := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, table := range allowed {
		delete(before, table)
		delete(after, table)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("grouping reset changed history, policies, summaries, receipts or queued deliveries")
	}
}

func TestCompatibleIssueHashesActiveOriginsLatestAndLookahead(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	ids := make([]string, 26)
	expectedEvents := make(map[string]bool, len(ids))
	for n := range ids {
		ev := groupEvent(t, n+1, fmt.Sprintf("hash-page-%d", n), fmt.Sprintf("Origin %d", n), stamp)
		ids[n] = groupCapture(t, f.ctx, f.db, f.project, ev)
		expectedEvents[ev.ID] = true
	}
	// Later receipt with an older occurrence must not replace the latest sample;
	// equal timestamps instead use the actual numeric event-row receipt order.
	tied := groupEvent(t, 101, "hash-page-0", "Same time, later row", stamp)
	delete(expectedEvents, fmt.Sprintf("%032x", 1))
	expectedEvents[tied.ID] = true
	groupCapture(t, f.ctx, f.db, f.project, tied)
	groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 102, "hash-page-0", "Late old occurrence", stamp.Add(-time.Minute)))
	firstHash := hashForSample(t, hashSamples(t, f, ids[0], CompatibleIssueHashFilter{Limit: 100}), tied.ID)
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, ids); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	page := hashSamples(t, f, ids[0], CompatibleIssueHashFilter{Limit: 25})
	if len(page) != 26 {
		t.Fatalf("25-row page did not supply exactly one real lookahead: %d", len(page))
	}
	publicIDs := make([]string, len(page))
	for n, item := range page {
		publicIDs[n] = item.ID
		if len(item.ID) != 32 || strings.Trim(item.ID, "0123456789abcdef") != "" || item.Latest == nil || item.Latest.GroupID != ids[0] || item.Latest.ProjectID != apiTokenProjectID(t, f.project.ID) || item.Latest.SiblingsKnown || item.Latest.PreviousID != nil || item.Latest.NextID != nil {
			t.Fatalf("hash identity/sample is not a real origin in the current root: %+v", item)
		}
		if !expectedEvents[item.Latest.ID] {
			t.Fatalf("hash samples duplicate a sibling latest or substitute an older event: %s", item.Latest.ID)
		}
		delete(expectedEvents, item.Latest.ID)
	}
	if !slices.IsSorted(publicIDs) || len(slices.Compact(slices.Clone(publicIDs))) != 26 || hashForSample(t, page, tied.ID) != firstHash {
		t.Fatal("merge changed route identity or UUID page order is unstable")
	}
	tail := hashSamples(t, f, ids[25], CompatibleIssueHashFilter{Offset: 25, Limit: 25})
	if len(tail) != 1 || tail[0].ID != page[25].ID {
		t.Fatalf("alias snapshot did not preserve the second page: %+v", tail)
	}
	if empty := hashSamples(t, f, ids[0], CompatibleIssueHashFilter{Offset: 26, Limit: 25}); len(empty) != 0 {
		t.Fatalf("past-end page invented samples: %+v", empty)
	}
	for _, item := range page {
		if item.Latest.ID == tied.ID {
			var want, got any
			if json.Unmarshal(tied.Data, &want) != nil || json.Unmarshal(item.Latest.Data, &got) != nil || item.Latest.Title != tied.Title || !item.Latest.Timestamp.Equal(stamp) || !reflect.DeepEqual(got, want) {
				t.Fatalf("latest occurrence metadata/raw changed: %+v", item.Latest)
			}
		}
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssueHashResetPreservesHistoryAndNewGenerationRouting(t *testing.T) {
	f := resolutionStore(t)
	rule, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, alertConfig("Reset retains existing outbox"))
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	aEvent := groupEvent(t, 1, "hash-generation-a", "Old A", stamp)
	bEvent := groupEvent(t, 2, "hash-generation-b", "Control B", stamp.Add(time.Second))
	cEvent := groupEvent(t, 3, "hash-generation-c", "Other root C", stamp)
	a := groupCapture(t, f.ctx, f.db, f.project, aEvent)
	b := groupCapture(t, f.ctx, f.db, f.project, bEvent)
	c := groupCapture(t, f.ctx, f.db, f.project, cEvent)
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, a, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 30), Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: f.owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AddComment(f.ctx, f.owner.ID, a, "History stays on the old issue"); err != nil {
		t.Fatal(err)
	}
	if deliveries, _, err := f.db.AlertDeliveries(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID, 1); err != nil || len(deliveries) == 0 {
		t.Fatalf("reset outbox preservation fixture has no actual queued delivery: %+v error=%v", deliveries, err)
	}
	initial := hashSamples(t, f, a, CompatibleIssueHashFilter{Limit: 100})
	aHash := hashForSample(t, initial, aEvent.ID)
	bHash := hashForSample(t, initial, bEvent.ID)
	cHash := hashForSample(t, hashSamples(t, f, c, CompatibleIssueHashFilter{Limit: 100}), cEvent.ID)
	hyphenated := strings.ToUpper(aHash[:8] + "-" + aHash[8:12] + "-" + aHash[12:16] + "-" + aHash[16:20] + "-" + aHash[20:])
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	if err := f.db.ResetCompatibleIssueHashes(f.ctx, f.access, a, []string{aHash, hyphenated, cHash, "ffffffffffffffffffffffffffffffff"}); err != nil {
		t.Fatal(err)
	}
	hashOnlyChanges(t, f, before, "issue_hash_routes", "issue_activity")
	remaining := hashSamples(t, f, a, CompatibleIssueHashFilter{Limit: 100})
	if len(remaining) != 1 || remaining[0].ID != bHash || hashForSample(t, hashSamples(t, f, c, CompatibleIssueHashFilter{Limit: 100}), cEvent.ID) != cHash {
		t.Fatal("mixed reset changed another root or retained the selected active signature")
	}
	actions := f.actions(t, a, "grouping_reset")
	var data struct {
		HashIDs []string `json:"hashIds"`
	}
	if len(actions) != 1 || actions[0].User == nil || actions[0].User.ID != f.owner.ID || json.Unmarshal(actions[0].Data, &data) != nil || !reflect.DeepEqual(data.HashIDs, []string{aHash}) {
		t.Fatalf("reset audit does not describe the actually reset signature: %+v", actions)
	}
	origins, err := f.db.IssueOrigins(f.ctx, f.owner.ID, a, "", 1)
	if err != nil || len(origins.Data) != 2 || origins.Data[0].ID != a || origins.Data[0].ReceivesNewEvents || origins.Data[1].ID != b || !origins.Data[1].ReceivesNewEvents || origins.Data[0].Count != 1 || origins.Data[1].Count != 1 {
		t.Fatalf("reset rewrote historical origins or advertised retired routing: %+v error=%v", origins, err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	if err := f.db.ResetCompatibleIssueHashes(f.ctx, f.access, a, []string{aHash, cHash}); err != nil {
		t.Fatal(err)
	}
	retry := aEvent
	retry.Release = "retry-only-must-not-be-observed@999"
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, retry); err != nil || fresh || id != a {
		t.Fatalf("old event retry reallocated its retired route: id=%s fresh=%v error=%v", id, fresh, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	newEvent := groupEvent(t, 4, "hash-generation-a", "New A generation", stamp.Add(2*time.Second))
	newA := groupCapture(t, f.ctx, f.db, f.project, newEvent)
	if newA == a || newA == b || newA == c {
		t.Fatal("fresh event reused a retired historical origin")
	}
	newIssue := triageIssue(t, f.ctx, f.db, f.owner.ID, newA)
	oldIssue := triageIssue(t, f.ctx, f.db, f.owner.ID, a)
	if newIssue.Status != "unresolved" || newIssue.AssignedTo != nil || newIssue.Ignore != nil || newIssue.Resolution != nil || newIssue.Count != 1 || oldIssue.Count != 2 || oldIssue.Status != "ignored" || oldIssue.Ignore == nil || oldIssue.Ignore.Remaining == nil || *oldIssue.Ignore.Remaining != 30 {
		t.Fatalf("generation creation inherited/changed old triage: new=%+v old=%+v", newIssue, oldIssue)
	}
	if got := hashForSample(t, hashSamples(t, f, newA, CompatibleIssueHashFilter{Limit: 100}), newEvent.ID); got != aHash {
		t.Fatalf("reset replaced the persisted route identity: %s want %s", got, aHash)
	}
	if id := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 5, "hash-generation-b", "Control B stays grouped", stamp.Add(3*time.Second))); id != a {
		t.Fatalf("control signature changed route: %s", id)
	}
	// Historical separation of the retired root is deliberately different from
	// reset. Its old raw moves as before, but the newer A binding stays on newA.
	if result, err := f.db.UnmergeIssue(f.ctx, f.owner.ID, a, []string{a}); err != nil || !result.Changed {
		t.Fatalf("historical separation failed: %+v error=%v", result, err)
	}
	if id := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 6, "hash-generation-a", "A after historical separation", stamp.Add(4*time.Second))); id != newA {
		t.Fatalf("historical unmerge stole the newer route: %s want %s", id, newA)
	}
	if items := hashSamples(t, f, a, CompatibleIssueHashFilter{Limit: 100}); len(items) != 0 {
		t.Fatal("retired standalone historical origin advertised an active route")
	}
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{newA, a}); err != nil {
		t.Fatal(err)
	}
	last := groupEvent(t, 7, "hash-generation-a", "A merged old and new", stamp.Add(5*time.Second))
	if id := groupCapture(t, f.ctx, f.db, f.project, last); id != a {
		t.Fatalf("active newer origin did not follow native merge: %s", id)
	}
	mergedSamples := hashSamples(t, f, a, CompatibleIssueHashFilter{Limit: 100})
	if len(mergedSamples) != 1 || mergedSamples[0].ID != aHash || mergedSamples[0].Latest == nil || mergedSamples[0].Latest.ID != last.ID || mergedSamples[0].Latest.GroupID != a {
		t.Fatalf("merged generations invented or duplicated signatures: %+v", mergedSamples)
	}
	var historicalOwner, historicalOrigin, latestOwner, latestOrigin string
	if err := f.db.pool.QueryRow(f.ctx, `SELECT old.issue_id::text,COALESCE(old.original_issue_id,old.issue_id)::text,new.issue_id::text,new.original_issue_id::text FROM events old CROSS JOIN events new WHERE old.project_id=$1::bigint AND old.event_id=$2 AND new.project_id=$1::bigint AND new.event_id=$3`, f.project.ID, aEvent.ID, last.ID).Scan(&historicalOwner, &historicalOrigin, &latestOwner, &latestOrigin); err != nil {
		t.Fatal(err)
	}
	if historicalOwner != a || historicalOrigin != a || latestOwner != a || latestOrigin != newA {
		t.Fatalf("historical generations lost original ownership: old=%s/%s new=%s/%s", historicalOwner, historicalOrigin, latestOwner, latestOrigin)
	}
}

func TestCompatibleIssueHashesProviderAuthorizationAndAtomicAdmission(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour)
	aEvent := groupEvent(t, 1, "hash-access-a", "Authorized", stamp)
	a := groupCapture(t, f.ctx, f.db, f.project, aEvent)
	b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "hash-access-b", "Alias", stamp))
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Hidden hash project", "go")
	if err != nil {
		t.Fatal(err)
	}
	hidden := groupCapture(t, f.ctx, f.db, other, groupEvent(t, 3, "hidden", "Hidden", stamp))
	if _, err = f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	member := triageMember(t, f.ctx, f.db, f.org, "hash-member", "member")
	access := ManagementAccess{UserID: member.ID, OrganizationID: f.org.ID, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}}
	items, err := f.db.CompatibleIssueHashes(f.ctx, access, b, CompatibleIssueHashFilter{Limit: 100})
	if err != nil || len(items) != 2 {
		t.Fatalf("member-scoped alias read: %+v error=%v", items, err)
	}
	aHash := hashForSample(t, items, aEvent.ID)
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	readOnly := access
	readOnly.ReadOnly = true
	if _, err = f.db.CompatibleIssueHashes(f.ctx, readOnly, a, CompatibleIssueHashFilter{Limit: 100}); err != nil {
		t.Fatalf("read-only management access could not read hashes: %v", err)
	}
	triageRequireError(t, f.db.ResetCompatibleIssueHashes(f.ctx, readOnly, a, []string{aHash}), ErrForbidden)
	var alias *IssueMergedError
	if err = f.db.ResetCompatibleIssueHashes(f.ctx, access, b, []string{aHash}); !errors.As(err, &alias) || alias.MergedInto != a {
		t.Fatalf("alias reset silently retargeted: %v", err)
	}
	for _, denied := range []ManagementAccess{
		{UserID: member.ID, OrganizationID: f.org.ID},
		{UserID: member.ID, OrganizationID: "9223372036854775807", AllProjects: true},
	} {
		_, err = f.db.CompatibleIssueHashes(f.ctx, denied, a, CompatibleIssueHashFilter{Limit: 100})
		triageRequireError(t, err, ErrNotFound)
		triageRequireError(t, f.db.ResetCompatibleIssueHashes(f.ctx, denied, a, []string{aHash}), ErrNotFound)
	}
	_, err = f.db.CompatibleIssueHashes(f.ctx, access, hidden, CompatibleIssueHashFilter{Limit: 100})
	triageRequireError(t, err, ErrNotFound)
	triageRequireError(t, f.db.ResetCompatibleIssueHashes(f.ctx, access, hidden, []string{aHash}), ErrNotFound)
	for _, filter := range []CompatibleIssueHashFilter{{Limit: 0}, {Limit: 101}, {Offset: -1, Limit: 1}, {Offset: 100000001, Limit: 1}} {
		_, err = f.db.CompatibleIssueHashes(f.ctx, access, a, filter)
		triageRequireError(t, err, ErrInvalidCompatibleIssueFilter)
	}
	for _, ids := range [][]string{nil, {aHash, "bad-uuid"}, slices.Repeat([]string{aHash}, 1001)} {
		triageRequireError(t, f.db.ResetCompatibleIssueHashes(f.ctx, access, a, ids), ErrInvalidCompatibleIssueChange)
	}
	for _, invalid := range []string{"01", "0", "-1", "9223372036854775808"} {
		_, err = f.db.CompatibleIssueHashes(f.ctx, access, invalid, CompatibleIssueHashFilter{Limit: 1})
		triageRequireError(t, err, ErrInvalidCompatibleIssueFilter)
		triageRequireError(t, f.db.ResetCompatibleIssueHashes(f.ctx, access, invalid, []string{aHash}), ErrInvalidCompatibleIssueChange)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, err = f.db.pool.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	_, err = f.db.CompatibleIssueHashes(f.ctx, access, a, CompatibleIssueHashFilter{Limit: 100})
	triageRequireError(t, err, ErrNotFound)
	triageRequireError(t, f.db.ResetCompatibleIssueHashes(f.ctx, access, a, []string{aHash}), ErrNotFound)
	groupRequireUnchanged(t, f.ctx, f.db, before)
}
