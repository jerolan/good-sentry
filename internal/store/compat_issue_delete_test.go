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

	"goodsentry/internal/notify"
)

func deleteStoreRows(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func deleteStoreRequireIssues(t *testing.T, f resolutionFixture, present bool, ids ...string) {
	t.Helper()
	for _, id := range ids {
		var exists bool
		if err := f.db.pool.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM issues WHERE id=$1::bigint)", id).Scan(&exists); err != nil || exists != present {
			t.Fatalf("issue %s present=%v want=%v err=%v", id, exists, present, err)
		}
	}
}

func deleteStoreApply(t *testing.T, f resolutionFixture, project string, filter CompatibleIssueBulkFilter) {
	t.Helper()
	if err := f.db.DeleteCompatibleIssues(f.ctx, f.access, project, filter); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteCompatibleIssueWholeGroupRetainsIndependentHistoryAndNewGeneration(t *testing.T) {
	f := resolutionStore(t)
	config := alertConfig("Deletion retains notifications")
	config.Quantity = 1
	rule, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Add(-time.Minute)
	aEvent := groupEvent(t, 1, "delete-old-a", "Historical A", stamp)
	bEvent := groupEvent(t, 2, "delete-b", "Historical B", stamp.Add(time.Second))
	cEvent := groupEvent(t, 3, "delete-empty-c", "Retained summary C", stamp.Add(2*time.Second))
	items := CaptureItems{Attachments: []CaptureAttachment{{Filename: "captured.bin", ContentType: "application/octet-stream", Data: []byte{0, 255}}}, EnvelopeItems: []CaptureEnvelopeItem{{Type: "parented", Header: json.RawMessage(`{"type":"parented"}`), Data: []byte{255, 0}}}}
	a, fresh, err := f.db.Ingest(f.ctx, f.project.ID, aEvent, items)
	if err != nil || !fresh {
		t.Fatalf("parent A fresh=%v err=%v", fresh, err)
	}
	b, fresh, err := f.db.Ingest(f.ctx, f.project.ID, bEvent, items)
	if err != nil || !fresh {
		t.Fatalf("parent B fresh=%v err=%v", fresh, err)
	}
	c := groupCapture(t, f.ctx, f.db, f.project, cEvent)
	for _, id := range []string{a, b, c} {
		if _, err := f.db.AddComment(f.ctx, f.owner.ID, id, "Independent historical comment "+id); err != nil {
			t.Fatal(err)
		}
	}
	// Legitimate retained-summary fixture: raw C has expired before the group
	// deletion. Its now-unmatched opaque item must remain independent.
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM events WHERE project_id=$1::bigint AND event_id=$2", f.project.ID, cEvent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{c, b, a}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, a, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 30), Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: f.owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	hashes := hashSamples(t, f, a, CompatibleIssueHashFilter{Limit: 100})
	aHash, bHash := hashForSample(t, hashes, aEvent.ID), hashForSample(t, hashes, bEvent.ID)
	var cHash string
	for _, hash := range hashes {
		if hash.Latest == nil {
			cHash = hash.ID
		}
	}
	if len(hashes) != 3 || cHash == "" {
		t.Fatalf("fixture omitted the active zero-event origin: %+v", hashes)
	}
	if err := f.db.ResetCompatibleIssueHashes(f.ctx, f.access, a, []string{aHash}); err != nil {
		t.Fatal(err)
	}
	newEvent := groupEvent(t, 4, "delete-old-a", "New generation A", stamp.Add(3*time.Second))
	newA := groupCapture(t, f.ctx, f.db, f.project, newEvent)
	control := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 5, "delete-retained-control", "Retained control", stamp))
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other retained project", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := f.db.Ingest(f.ctx, other.ID, aEvent, items); err != nil || !fresh {
		t.Fatalf("same external ID in another project fresh=%v err=%v", fresh, err)
	}
	transaction := operationEvent(6, stamp, `[{"op":"db","description":"SELECT retained","start_timestamp":0,"timestamp":0.5}]`)
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, transaction, items); err != nil || !fresh {
		t.Fatalf("retained transaction fresh=%v err=%v", fresh, err)
	}
	if _, err := f.db.IngestSpans(f.ctx, f.project.ID, spanBatch(t, spanFixture("0000000000000001", true, 1, 2)), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.IngestLogs(f.ctx, f.project.ID, json.RawMessage(`{"items":[{"timestamp":1700000000,"level":"info","body":"Retained log"}]}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{aEvent.ID, cEvent.ID, "", "ffffffffffffffffffffffffffffffff"} {
		if err := f.db.SaveEnvelopeItem(f.ctx, f.project.ID, id, "standalone", json.RawMessage(`{"type":"standalone"}`), []byte{128, 0}); err != nil {
			t.Fatal(err)
		}
	}
	commits := []ReleaseCommitInput{{ID: "retained-commit", Repository: "retained-repo", Message: "Retained commit", Data: json.RawMessage(`{"id":"retained-commit"}`)}}
	if _, _, err := f.db.CreateRelease(f.ctx, f.access, ReleaseCreate{Version: "delete-retained-release", Projects: []string{f.project.ID}, ReleaseUpdate: ReleaseUpdate{Commits: &commits}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateReleaseDeploy(f.ctx, f.access, "delete-retained-release", ReleaseDeployInput{Environment: "production", Projects: []string{f.project.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.SaveSourceMap(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, "retained.js.map", "delete-retained-release", "", "", []byte(`{"version":3,"sources":["retained.ts"],"names":[],"mappings":"AAAA"}`)); err != nil {
		t.Fatal(err)
	}
	lease, err := f.db.ClaimAlertDelivery(f.ctx, 3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.CompleteAlertDelivery(f.ctx, lease, notify.Outcome{Disposition: notify.Delivered, Reason: "local-test-completion"}, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.TestAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, table := range []string{"comments", "issue_activity", "issue_assignments", "issue_ignores", "attachments", "envelope_items", "transaction_span_operations", "spans", "logs", "source_maps", "release_commits", "release_deploys", "alert_deliveries", "alert_attempts"} {
		if before[table] == "" || before[table] == "[]" {
			t.Fatalf("retention fixture must actually populate %s", table)
		}
	}
	if err := f.db.DeleteCompatibleIssue(f.ctx, f.access, a); err != nil {
		t.Fatal(err)
	}
	deleteStoreRequireIssues(t, f, false, a, b, c)
	deleteStoreRequireIssues(t, f, true, newA, control)
	after := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	members := map[string]bool{a: true, b: true, c: true}
	removedEvents := map[string]bool{aEvent.ID: true, bEvent.ID: true}
	for table, old := range before {
		switch table {
		case "issues", "events", "comments", "issue_activity", "issue_assignments", "issue_ignores", "issue_resolutions", "attachments", "envelope_items":
			want := []map[string]any{}
			for _, row := range deleteStoreRows(t, old) {
				drop := members[fmt.Sprint(row["issue_id"])]
				if table == "issues" {
					drop = members[fmt.Sprint(row["id"])]
				} else if table == "attachments" || table == "envelope_items" {
					drop = fmt.Sprint(row["project_id"]) == f.project.ID && removedEvents[fmt.Sprint(row["event_id"])]
				}
				if !drop {
					want = append(want, row)
				}
			}
			if !reflect.DeepEqual(deleteStoreRows(t, after[table]), want) {
				t.Fatalf("deletion lost independent rows or retained selected dependent rows in %s", table)
			}
		case "issue_hash_routes":
			want := deleteStoreRows(t, old)
			for _, row := range want {
				id := strings.ReplaceAll(fmt.Sprint(row["id"]), "-", "")
				if id == bHash || id == cHash {
					row["origin_issue_id"] = nil
				}
			}
			if !reflect.DeepEqual(deleteStoreRows(t, after[table]), want) {
				t.Fatal("deletion replaced route UUIDs, removed a newer generation or retained an owned binding")
			}
		default:
			if after[table] != old {
				t.Fatalf("deletion changed independent %s rows", table)
			}
		}
	}
	if id := hashForSample(t, hashSamples(t, f, newA, CompatibleIssueHashFilter{Limit: 100}), newEvent.ID); id != aHash {
		t.Fatal("same-fingerprint newer generation lost its stable active route")
	}
	stable := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	retry := newEvent
	retry.Release = "retained-retry-must-not-observe"
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, retry); err != nil || fresh || id != newA {
		t.Fatalf("retained receipt retry id=%s fresh=%v err=%v", id, fresh, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, stable)
	future := groupEvent(t, 7, "delete-b", "Fresh B after deletion", stamp.Add(time.Minute))
	newB := groupCapture(t, f.ctx, f.db, f.project, future)
	if members[newB] || newB == newA || hashForSample(t, hashSamples(t, f, newB, CompatibleIssueHashFilter{Limit: 100}), future.ID) != bHash {
		t.Fatal("fresh capture did not reuse the inactive stable route with a new group")
	}
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, bEvent, items); err != nil || !fresh || id != newB {
		t.Fatalf("deleted external ID incorrectly behaved as a tombstone: id=%s fresh=%v err=%v", id, fresh, err)
	}
	triageRequireError(t, f.db.DeleteCompatibleIssue(f.ctx, f.access, a), ErrNotFound)
}

func TestDeleteCompatibleIssueAdmissionAliasAndCurrentMembershipAreAtomic(t *testing.T) {
	f := resolutionStore(t)
	a := f.capture(t, 1, "app@1", time.Now())
	b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "delete-alias", "Alias", time.Now()))
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	member := triageMember(t, f.ctx, f.db, f.org, "delete-member", "member")
	access := ManagementAccess{UserID: member.ID, OrganizationID: f.org.ID, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Individually hidden deletion", "go")
	if err != nil {
		t.Fatal(err)
	}
	hidden := groupCapture(t, f.ctx, f.db, other, groupEvent(t, 1, "individual-hidden", "Hidden", time.Now()))
	foreign := triageForeignOrganization(t, f.ctx, f.db, "individual-delete-foreign")
	if _, err := f.db.pool.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'member')", foreign.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	var merged *IssueMergedError
	if err := f.db.DeleteCompatibleIssue(f.ctx, access, b); !errors.As(err, &merged) || merged.MergedInto != a {
		t.Fatalf("individual alias was silently canonicalized: %v", err)
	}
	for _, id := range []string{"", "0", "01", "-1", "9223372036854775808"} {
		triageRequireError(t, f.db.DeleteCompatibleIssue(f.ctx, access, id), ErrInvalidCompatibleIssueChange)
	}
	readOnly := access
	readOnly.ReadOnly = true
	triageRequireError(t, f.db.DeleteCompatibleIssue(f.ctx, readOnly, "bad"), ErrForbidden)
	for _, denied := range []ManagementAccess{
		{UserID: member.ID, OrganizationID: f.org.ID},
		{UserID: member.ID, OrganizationID: "9223372036854775807", AllProjects: true},
		{UserID: member.ID, OrganizationID: foreign.ID, AllProjects: true},
	} {
		triageRequireError(t, f.db.DeleteCompatibleIssue(f.ctx, denied, a), ErrNotFound)
	}
	triageRequireError(t, f.db.DeleteCompatibleIssue(f.ctx, access, hidden), ErrNotFound)
	triageRequireError(t, f.db.DeleteCompatibleIssue(f.ctx, access, "9223372036854775807"), ErrNotFound)
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM memberships WHERE user_id=$1 AND organization_id=$2", member.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	triageRequireError(t, f.db.DeleteCompatibleIssue(f.ctx, access, a), ErrNotFound)
	triageRequireError(t, f.db.DeleteCompatibleIssues(f.ctx, access, "", CompatibleIssueBulkFilter{IDs: []string{a}, Limit: 1}), ErrNotFound)
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, a, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: "app@1"}}}); err != nil {
		t.Fatal(err)
	}
	var policies int
	if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM issue_resolutions").Scan(&policies); err != nil || policies != 1 {
		t.Fatalf("resolved-policy cascade fixture missing: count=%d err=%v", policies, err)
	}
	if err := f.db.DeleteCompatibleIssue(f.ctx, ManagementAccess{UserID: f.owner.ID, AllProjects: true}, a); err != nil {
		t.Fatalf("authorized session could not infer the issue organization: %v", err)
	}
	if err := f.db.pool.QueryRow(f.ctx, "SELECT count(*) FROM issue_resolutions").Scan(&policies); err != nil || policies != 0 {
		t.Fatalf("deletion retained issue resolution rows: count=%d err=%v", policies, err)
	}
	deleteStoreRequireIssues(t, f, false, a, b)
	deleteStoreRequireIssues(t, f, true, hidden)
}

func TestDeleteCompatibleIssuesWholeIDContainmentIgnoredFiltersAndDedup(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now()
	a := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 1, "delete-explicit-a", "A", stamp))
	alias := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 2, "delete-explicit-alias", "Alias", stamp.Add(time.Second)))
	b := groupCapture(t, f.ctx, f.db, f.project, groupEvent(t, 3, "delete-explicit-b", "B", stamp.Add(2*time.Second)))
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{a, alias}); err != nil {
		t.Fatal(err)
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Hidden deletion", "go")
	if err != nil {
		t.Fatal(err)
	}
	hidden := groupCapture(t, f.ctx, f.db, other, groupEvent(t, 1, "hidden-delete", "Hidden", stamp))
	foreign := triageForeignOrganization(t, f.ctx, f.db, "delete-foreign")
	if _, err := f.db.pool.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreign.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := f.db.CreateProject(f.ctx, f.owner.ID, foreign.Slug, "Foreign deletion", "go")
	if err != nil {
		t.Fatal(err)
	}
	foreignIssue := groupCapture(t, f.ctx, f.db, foreignProject, groupEvent(t, 1, "foreign-delete", "Foreign", stamp))
	f.access.AllProjects = false
	f.access.ProjectIDs = []int64{apiTokenProjectID(t, f.project.ID)}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, id := range []string{hidden, foreignIssue, "9223372036854775807"} {
		deleteStoreApply(t, f, "", CompatibleIssueBulkFilter{IDs: []string{b, id}, Limit: 1})
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	for _, filter := range []CompatibleIssueBulkFilter{
		{IDs: []string{a}, Limit: 0}, {IDs: []string{a}, Limit: 1001}, {IDs: []string{"01"}, Limit: 1},
		{IDs: slices.Repeat([]string{a}, 1001), Limit: 1}, {IDs: []string{a}, ProjectRefs: slices.Repeat([]string{"ignored"}, 101), Limit: 1},
		{IDs: []string{a}, Environments: slices.Repeat([]string{"ignored"}, 101), Limit: 1}, {IDs: []string{a}, Query: "bad\x00", Limit: 1},
	} {
		triageRequireError(t, f.db.DeleteCompatibleIssues(f.ctx, f.access, "", filter), ErrInvalidCompatibleIssueFilter)
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	readOnly := f.access
	readOnly.ReadOnly = true
	triageRequireError(t, f.db.DeleteCompatibleIssues(f.ctx, readOnly, "", CompatibleIssueBulkFilter{}), ErrForbidden)
	for _, filter := range []CompatibleIssueBulkFilter{{Query: `message:"unterminated`, Limit: 1}, {Sort: "unsupported", Limit: 1}} {
		triageRequireError(t, f.db.DeleteCompatibleIssues(f.ctx, f.access, "", filter), ErrInvalidCompatibleIssueFilter)
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	grantBoth := f
	grantBoth.access.ProjectIDs = append(slices.Clone(f.access.ProjectIDs), apiTokenProjectID(t, other.ID))
	deleteStoreApply(t, grantBoth, f.project.Slug, CompatibleIssueBulkFilter{IDs: []string{b, hidden}, Limit: 1})
	groupRequireUnchanged(t, f.ctx, f.db, before)
	emptyGrant := f
	emptyGrant.access.ProjectIDs = nil
	deleteStoreApply(t, emptyGrant, "", CompatibleIssueBulkFilter{IDs: []string{a}, Limit: 1})
	groupRequireUnchanged(t, f.ctx, f.db, before)
	start, end := stamp.Add(time.Hour), stamp.Add(-time.Hour)
	filter := CompatibleIssueBulkFilter{IDs: []string{a, alias, b}, Limit: 1, ProjectRefs: []string{"does-not-exist"}, Environments: []string{"not-an-occurrence"}, Query: "unknown:operator OR", Status: "unsupported", Sort: "unsupported", Start: &start, End: &end, Period: -time.Hour}
	deleteStoreApply(t, f, "", filter)
	deleteStoreRequireIssues(t, f, false, b)
	deleteStoreRequireIssues(t, f, true, a, alias, hidden, foreignIssue)
	deleteStoreApply(t, f, "", CompatibleIssueBulkFilter{IDs: []string{alias, a, alias}, Limit: 1})
	deleteStoreRequireIssues(t, f, false, a, alias)
	deleteStoreRequireIssues(t, f, true, hidden, foreignIssue)
}

func TestDeleteCompatibleIssuesSameOccurrenceFiltersAndTypedStateSelection(t *testing.T) {
	f := resolutionStore(t)
	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	_, split := compatibleCapture(t, f.ctx, f.db, f.project, 1, base, map[string]any{"tags": map[string]string{"customer": "wrong"}})
	compatibleCapture(t, f.ctx, f.db, f.project, 2, base.Add(time.Second), map[string]any{"environment": "staging", "tags": map[string]string{"customer": "right"}})
	_, selected := compatibleCapture(t, f.ctx, f.db, f.project, 3, base.Add(2*time.Second), map[string]any{"fingerprint": []string{"actual-occurrence"}, "tags": map[string]string{"customer": "right"}})
	_, resolved := compatibleCapture(t, f.ctx, f.db, f.project, 4, base.Add(2*time.Second), map[string]any{"fingerprint": []string{"resolved-occurrence"}, "tags": map[string]string{"customer": "right"}})
	compatibleApply(t, f, resolved, CompatibleIssueChange{Status: "resolved"})
	_, outside := compatibleCapture(t, f.ctx, f.db, f.project, 5, base.Add(time.Hour), map[string]any{"fingerprint": []string{"outside-time"}, "tags": map[string]string{"customer": "right"}})
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other filtered deletion", "go")
	if err != nil {
		t.Fatal(err)
	}
	_, elsewhere := compatibleCapture(t, f.ctx, f.db, other, 1, base.Add(2*time.Second), map[string]any{"tags": map[string]string{"customer": "right"}})
	start, end := base, base.Add(time.Minute)
	deleteStoreApply(t, f, "", CompatibleIssueBulkFilter{ProjectRefs: []string{f.project.Slug}, Environments: []string{"production"}, Query: "customer:right", Status: "unresolved", Start: &start, End: &end, Limit: 1})
	deleteStoreRequireIssues(t, f, false, selected)
	deleteStoreRequireIssues(t, f, true, split, resolved, outside, elsewhere)
	// A period is the actual time selector when both relative and explicit
	// bounds are supplied. The older explicit interval must not mask it.
	_, recent := compatibleCapture(t, f.ctx, f.db, f.project, 6, base.Add(time.Minute), map[string]any{"fingerprint": []string{"period-only"}, "tags": map[string]string{"customer": "period"}})
	_, ignored := compatibleCapture(t, f.ctx, f.db, f.project, 7, base, map[string]any{"fingerprint": []string{"ignored-state"}})
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, ignored, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 30)}); err != nil {
		t.Fatal(err)
	}
	oldStart, oldEnd := base.Add(-100*time.Hour), base.Add(-99*time.Hour)
	deleteStoreApply(t, f, "", CompatibleIssueBulkFilter{ProjectRefs: []string{f.project.ID}, Query: "customer:period", Period: time.Hour, Start: &oldStart, End: &oldEnd, Limit: 1000})
	deleteStoreRequireIssues(t, f, false, recent)
	deleteStoreApply(t, f, "", CompatibleIssueBulkFilter{ProjectRefs: []string{f.project.ID}, Status: "unresolved", Limit: 1000})
	deleteStoreRequireIssues(t, f, false, split, outside)
	deleteStoreRequireIssues(t, f, true, resolved, ignored, elsewhere)
	// HTTP owns omitted query versus query=; at Store this is the explicit
	// all-status selector used by an unfiltered project deletion.
	deleteStoreApply(t, f, f.project.Slug, CompatibleIssueBulkFilter{Limit: 1000})
	deleteStoreRequireIssues(t, f, false, resolved, ignored)
	deleteStoreRequireIssues(t, f, true, elsewhere)
}
