//go:build integration

package store

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func bulkApply(t *testing.T, f resolutionFixture, project string, filter CompatibleIssueBulkFilter, change CompatibleIssueBulkChange) CompatibleIssueBulkResult {
	t.Helper()
	result, err := f.db.UpdateCompatibleIssues(f.ctx, f.access, project, filter, change)
	if err != nil {
		t.Fatalf("bulk issue update: %v", err)
	}
	return result
}

func bulkRequireStatus(t *testing.T, f resolutionFixture, status string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if got := triageIssue(t, f.ctx, f.db, f.owner.ID, id); got.Status != status {
			t.Fatalf("issue %s status=%s want=%s", id, got.Status, status)
		}
	}
}

func TestCompatibleIssueBulkWholeIDContainmentPrecedesLimits(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour)
	_, first := compatibleCapture(t, f.ctx, f.db, f.project, 1, stamp, nil)
	_, second := compatibleCapture(t, f.ctx, f.db, f.project, 2, stamp.Add(time.Second), map[string]any{"fingerprint": []string{"second"}})
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other bulk", "go")
	if err != nil {
		t.Fatal(err)
	}
	_, hidden := compatibleCapture(t, f.ctx, f.db, other, 1, stamp, nil)
	foreignOrg := triageForeignOrganization(t, f.ctx, f.db, "bulk-foreign")
	if _, err := f.db.pool.Exec(f.ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	foreignProject, err := f.db.CreateProject(f.ctx, f.owner.ID, foreignOrg.Slug, "Foreign bulk", "go")
	if err != nil {
		t.Fatal(err)
	}
	_, foreign := compatibleCapture(t, f.ctx, f.db, foreignProject, 1, stamp, nil)
	f.access.AllProjects = false
	f.access.ProjectIDs = []int64{apiTokenProjectID(t, f.project.ID)}
	command := CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, id := range []string{hidden, foreign, "9223372036854775807"} {
		got := bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1, IDs: []string{first, id}, Sort: "date"}, command)
		if got.First != nil || got.Merge != nil {
			t.Fatalf("out-of-scope ID %s returned a changed issue: %+v", id, got)
		}
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	// A visible ID in another authorized project is still outside this path.
	f.access.ProjectIDs = append(f.access.ProjectIDs, apiTokenProjectID(t, other.ID))
	got := bulkApply(t, f, f.project.Slug, CompatibleIssueBulkFilter{Limit: 1, IDs: []string{first, hidden}}, command)
	if got.First != nil {
		t.Fatalf("path containment was checked after truncation: %+v", got)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	f.access.ProjectIDs = f.access.ProjectIDs[:1]
	for _, selector := range []string{other.ID, foreignProject.ID, "missing"} {
		_, err := f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{first}, ProjectRefs: []string{selector}}, command)
		triageRequireError(t, err, ErrNotFound)
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	for _, tc := range []struct {
		access  ManagementAccess
		project string
		want    error
	}{
		{ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, ProjectIDs: f.access.ProjectIDs, ReadOnly: true}, "", ErrForbidden},
		{ManagementAccess{UserID: f.owner.ID, OrganizationID: foreignOrg.ID, AllProjects: true}, f.project.Slug, ErrNotFound},
		{f.access, other.Slug, ErrNotFound},
	} {
		_, err := f.db.UpdateCompatibleIssues(f.ctx, tc.access, tc.project, CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{first}}, command)
		triageRequireError(t, err, tc.want)
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	// An empty grant is empty, not an implicit all-project token.
	empty := f.access
	empty.ProjectIDs = nil
	got, err = f.db.UpdateCompatibleIssues(f.ctx, empty, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{first}}, command)
	if err != nil || got.First != nil {
		t.Fatalf("empty grant result=%+v error=%v", got, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, ProjectRefs: []string{"-1"}}, command)
	if got.First == nil || got.First.ID != first {
		t.Fatalf("authorized project intersection=%+v", got)
	}
	bulkRequireStatus(t, f, "resolved", first, second)
	bulkRequireStatus(t, f, "unresolved", hidden, foreign)
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	_, err = f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{first}}, command)
	triageRequireError(t, err, ErrNotFound)
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssueBulkSameOccurrenceTimeStatusAndProjectIntersection(t *testing.T) {
	f := resolutionStore(t)
	base := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	_, split := compatibleCapture(t, f.ctx, f.db, f.project, 1, base, map[string]any{"environment": "production", "tags": map[string]string{"customer": "wrong"}})
	compatibleCapture(t, f.ctx, f.db, f.project, 2, base.Add(time.Minute), map[string]any{"environment": "staging", "tags": map[string]string{"customer": "right"}})
	_, selected := compatibleCapture(t, f.ctx, f.db, f.project, 3, base.Add(2*time.Minute), map[string]any{"fingerprint": []string{"same-occurrence"}, "tags": map[string]string{"customer": "right"}})
	_, resolved := compatibleCapture(t, f.ctx, f.db, f.project, 4, base.Add(2*time.Minute), map[string]any{"fingerprint": []string{"resolved-match"}, "tags": map[string]string{"customer": "right"}})
	compatibleApply(t, f, resolved, CompatibleIssueChange{Status: "resolved"})
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other filter", "go")
	if err != nil {
		t.Fatal(err)
	}
	_, elsewhere := compatibleCapture(t, f.ctx, f.db, other, 1, base.Add(2*time.Minute), map[string]any{"tags": map[string]string{"customer": "right"}})
	start, end := base, base.Add(3*time.Minute)
	filter := CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{split, selected, resolved}, Environments: []string{"production"}, Query: "customer:right", Status: "unresolved", Start: &start, End: &end}
	got := bulkApply(t, f, f.project.Slug, filter, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "ignored"}})
	if got.First == nil || got.First.ID != selected || got.First.Status != "ignored" {
		t.Fatalf("same-occurrence selection=%+v", got)
	}
	bulkRequireStatus(t, f, "unresolved", split, elsewhere)
	bulkRequireStatus(t, f, "resolved", resolved)
	filter.Status = "muted"
	got = bulkApply(t, f, f.project.Slug, filter, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
	if got.First == nil || got.First.ID != selected || got.First.Status != "resolved" {
		t.Fatalf("muted selector did not restrict the actual ignored root: %+v", got)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	// The two explicit project selectors are intersected with the path.
	got = bulkApply(t, f, f.project.Slug, CompatibleIssueBulkFilter{Limit: 1000, ProjectRefs: []string{other.ID}}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
	if got.First != nil {
		t.Fatalf("path plus disjoint selectors matched=%+v", got)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	// Relative time replaces valid explicit bounds and uses the database clock;
	// all retained 2024 events fall outside this recent interval.
	filter.Period = time.Hour
	filter.Status = ""
	got = bulkApply(t, f, "", filter, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
	if got.First != nil {
		t.Fatalf("relative interval ignored: %+v", got)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssueBulkOrderLimitHundredOneRootsAndAliasDeduplication(t *testing.T) {
	f := resolutionStore(t)
	base := time.Now().Add(-time.Hour)
	ids := make([]string, 101)
	for n := range ids {
		offset := n
		if n == 1 {
			offset = 0
		}
		_, ids[n] = compatibleCapture(t, f.ctx, f.db, f.project, n+1, base.Add(time.Duration(offset)*time.Second), map[string]any{"fingerprint": []string{fmt.Sprintf("bulk-limit-%d", n)}})
	}
	got := bulkApply(t, f, f.project.Slug, CompatibleIssueBulkFilter{Limit: 1}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
	if got.First == nil || got.First.ID != ids[100] {
		t.Fatalf("default newest order before limit=%+v", got)
	}
	bulkRequireStatus(t, f, "unresolved", ids[:100]...)
	bulkRequireStatus(t, f, "resolved", ids[100])
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: ids}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "ignored"}})
	if got.First == nil || got.First.ID != ids[0] {
		t.Fatalf("101-root operation or numeric response order=%+v", got)
	}
	bulkRequireStatus(t, f, "ignored", ids...)
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Sort: "last_seen", Limit: 1}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
	if got.First == nil || got.First.ID != ids[0] {
		t.Fatalf("ascending oldest ordering=%+v", got)
	}
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	before := len(f.actions(t, ids[0], "unresolved"))
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{ids[1], ids[0], ids[1]}}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "unresolved"}})
	if got.First == nil || got.First.ID != ids[0] || got.First.Count != 2 || len(f.actions(t, ids[0], "unresolved")) != before+1 {
		t.Fatalf("duplicate IDs/current-root aliases repeated mutation=%+v", got)
	}
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 2, IDs: []string{ids[0], ids[1], ids[2]}, Sort: "last_seen"}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved"}})
	if got.First == nil || got.First.ID != ids[0] {
		t.Fatalf("limit after current-root canonicalization=%+v", got)
	}
	bulkRequireStatus(t, f, "resolved", ids[0], ids[2])
	bulkRequireStatus(t, f, "ignored", ids[3])
}

func TestCompatibleIssueBulkPolicyRollbackAndNoOpFootprint(t *testing.T) {
	f := resolutionStore(t)
	base := time.Now().Add(-time.Hour)
	_, a := compatibleCapture(t, f.ctx, f.db, f.project, 1, base, nil)
	_, b := compatibleCapture(t, f.ctx, f.db, f.project, 2, base, map[string]any{"fingerprint": []string{"later-invalid"}})
	compatibleApply(t, f, a, CompatibleIssueChange{Status: "ignored"})
	count := int32(5)
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	// A is valid; B is unresolved and rejects this policy-only command. Any
	// preceding change and activity for A must roll back with the later error.
	_, err := f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a, b}}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Count: &count}}})
	triageRequireError(t, err, ErrInvalidCompatibleIssueChange)
	groupRequireUnchanged(t, f.ctx, f.db, before)
	_, err = f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a, b}}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved", Assignment: &CompatibleIssueAssignment{Reference: "user:9223372036854775807"}}})
	triageRequireError(t, err, ErrNotFound)
	groupRequireUnchanged(t, f.ctx, f.db, before)
	command := CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "ignored", Assignment: &CompatibleIssueAssignment{Reference: strings.ToUpper(f.owner.Email)}, Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Count: &count}}}
	got := bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a, b}}, command)
	if got.First == nil || got.First.AssignedTo == nil || got.First.AssignedTo.ID != f.owner.ID || got.First.Ignore == nil || *got.First.Ignore.Remaining != int64(count) {
		t.Fatalf("bulk assignment/native policy=%+v", got)
	}
	for _, id := range []string{a, b} {
		i := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
		if i.Status != "ignored" || i.AssignedTo == nil || i.AssignedTo.ID != f.owner.ID || i.Ignore == nil || *i.Ignore.Remaining != int64(count) {
			t.Fatalf("bulk did not apply assignment/policy to issue %s: %+v", id, i)
		}
	}
	compatibleCapture(t, f.ctx, f.db, f.project, 3, base.Add(-time.Hour), nil)
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{b, a}}, command)
	if got.First == nil || got.First.Ignore == nil || *got.First.Ignore.Remaining != 4 {
		t.Fatalf("equal bulk reset capture progress=%+v", got)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	minutes := int32(5)
	compatibleApply(t, f, a, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Minutes: &minutes}})
	if _, err := f.db.pool.Exec(f.ctx, "UPDATE issue_ignores SET until=clock_timestamp()-interval '1 second' WHERE issue_id=$1", a); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a, b}}, CompatibleIssueBulkChange{})
	if got.First != nil || got.Merge != nil {
		t.Fatalf("no-command result=%+v", got)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssueBulkCombinedMergeRetainsNativeHistoryAndFutureRouting(t *testing.T) {
	f := resolutionStore(t)
	config := alertConfig("Bulk merge notification control")
	config.Quantity = 1
	rule, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	aEvent := groupEvent(t, 1, "bulk-a", "Winner", base)
	bEvent := groupEvent(t, 2, "bulk-b", "Absorbed", base.Add(time.Second))
	cEvent := groupEvent(t, 3, "bulk-c", "Nested", base.Add(2*time.Second))
	a := groupCapture(t, f.ctx, f.db, f.project, aEvent)
	b := groupCapture(t, f.ctx, f.db, f.project, bEvent)
	c := groupCapture(t, f.ctx, f.db, f.project, cEvent)
	muted, err := f.db.UpdateIssue(f.ctx, f.owner.ID, a, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 6)})
	if err != nil {
		t.Fatal(err)
	}
	compatibleApply(t, f, b, CompatibleIssueChange{Status: "resolved"})
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{c, b}); err != nil {
		t.Fatal(err)
	}
	raw := groupRawSnapshot(t, f.ctx, f.db)
	deliveries, _, err := f.db.AlertDeliveries(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID, 1)
	if err != nil || len(deliveries) == 0 {
		t.Fatalf("notification control not exercised: deliveries=%d error=%v", len(deliveries), err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	_, err = f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{c, a}}, CompatibleIssueBulkChange{Merge: true, Change: CompatibleIssueChange{Assignment: &CompatibleIssueAssignment{Reference: "user:9223372036854775807"}}})
	triageRequireError(t, err, ErrNotFound)
	groupRequireUnchanged(t, f.ctx, f.db, before)
	got := bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{c, a}}, CompatibleIssueBulkChange{Merge: true, Change: CompatibleIssueChange{Assignment: &CompatibleIssueAssignment{Reference: f.owner.ID}}})
	if got.Merge == nil || got.Merge.Parent != a || !reflect.DeepEqual(got.Merge.Children, []string{b}) || got.First == nil || got.First.ID != a || got.First.Count != 3 || got.First.GroupSize != 3 || got.First.AssignedTo == nil || got.First.AssignedTo.ID != f.owner.ID || !reflect.DeepEqual(got.First.Ignore, muted.Ignore) || got.First.Status != "ignored" {
		t.Fatalf("combined current-root merge=%+v", got)
	}
	if groupRawSnapshot(t, f.ctx, f.db) != raw || captureProgressCounter(t, f.ctx, f.db, a) != 1 || captureProgressCounter(t, f.ctx, f.db, b) != 1 || captureProgressCounter(t, f.ctx, f.db, c) != 1 {
		t.Fatal("merge rewrote raw data or replayed receipt progress")
	}
	for _, pair := range []struct{ eventID, origin string }{{aEvent.ID, a}, {bEvent.ID, b}, {cEvent.ID, c}} {
		var current, origin string
		if err := f.db.pool.QueryRow(f.ctx, "SELECT issue_id::text,COALESCE(original_issue_id,issue_id)::text FROM events WHERE event_id=$1", pair.eventID).Scan(&current, &origin); err != nil || current != a || origin != pair.origin {
			t.Fatalf("event %s current=%s origin=%s error=%v", pair.eventID, current, origin, err)
		}
	}
	if after, _, err := f.db.AlertDeliveries(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID, 1); err != nil || !reflect.DeepEqual(after, deliveries) {
		t.Fatalf("historical merge replayed outbox: error=%v", err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{b, c}}, CompatibleIssueBulkChange{Merge: true})
	if got.First == nil || got.First.ID != a || got.Merge == nil || got.Merge.Parent != a || len(got.Merge.Children) != 0 {
		t.Fatalf("already-merged acknowledgement=%+v", got)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, bEvent); err != nil || fresh || id != a {
		t.Fatalf("historical retry id=%s fresh=%v error=%v", id, fresh, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	future := groupEvent(t, 4, "bulk-c", "Fresh absorbed origin", base.Add(3*time.Second))
	if id := groupCapture(t, f.ctx, f.db, f.project, future); id != a {
		t.Fatalf("future fingerprint routed to %s want=%s", id, a)
	}
	i := triageIssue(t, f.ctx, f.db, f.owner.ID, a)
	if i.Count != 4 || i.Ignore == nil || *i.Ignore.Remaining != 5 || captureProgressCounter(t, f.ctx, f.db, a) != 2 || captureProgressCounter(t, f.ctx, f.db, c) != 1 {
		t.Fatalf("fresh merged capture lost native mute/receipt progress=%+v", i)
	}
}

func TestCompatibleIssueBulkProviderBoundariesAndCrossProjectRestrictions(t *testing.T) {
	f := resolutionStore(t)
	_, a := compatibleCapture(t, f.ctx, f.db, f.project, 1, time.Now(), nil)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Cross project bulk", "go")
	if err != nil {
		t.Fatal(err)
	}
	_, b := compatibleCapture(t, f.ctx, f.db, other, 1, time.Now(), nil)
	f.release(t, "bulk-release")
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, change := range []CompatibleIssueBulkChange{
		{Merge: true},
		{Change: CompatibleIssueChange{Status: "resolved", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueReleasePolicy, Version: "bulk-release"}}},
		{Change: CompatibleIssueChange{Status: "resolved", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueLatestReleasePolicy}}},
		{Change: CompatibleIssueChange{Status: "resolvedInNextRelease"}},
	} {
		_, err := f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a, b}}, change)
		triageRequireError(t, err, ErrInvalidCompatibleIssueChange)
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	count := int32(3)
	bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{b, a}}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "muted", Assignment: &CompatibleIssueAssignment{Reference: f.owner.ID}, Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Count: &count}}})
	for _, id := range []string{a, b} {
		i := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
		if i.Status != "ignored" || i.AssignedTo == nil || i.AssignedTo.ID != f.owner.ID || i.Ignore == nil || i.Ignore.Count == nil || *i.Ignore.Count != count {
			t.Fatalf("supported cross-project assignment/mute rejected or lost: %+v", i)
		}
	}
	// A single-project selected release is a supported bulk operation.
	got := bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a}}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueReleasePolicy, Version: "bulk-release"}}})
	if got.First == nil || got.First.Resolution == nil || got.First.Resolution.Release == nil || got.First.Resolution.Release.Version != "bulk-release" {
		t.Fatalf("bulk selected release=%+v", got)
	}
	_, c := compatibleCapture(t, f.ctx, f.db, f.project, 2, time.Now(), map[string]any{"fingerprint": []string{"bulk-release-peer"}})
	latest := f.release(t, "bulk-latest")
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{c, a}}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolved", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueLatestReleasePolicy}}})
	if got.First == nil || got.First.Resolution == nil || got.First.Resolution.Release == nil || got.First.Resolution.Release.Version != latest.Version {
		t.Fatalf("bulk latest acknowledgement=%+v", got)
	}
	for _, id := range []string{a, c} {
		i := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
		if i.Resolution == nil || i.Resolution.Release == nil || i.Resolution.Release.Version != latest.Version {
			t.Fatalf("bulk latest did not share selected version: %+v", i)
		}
	}
	got = bulkApply(t, f, "", CompatibleIssueBulkFilter{Limit: 1000, IDs: []string{a, c}}, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "resolvedInNextRelease"}})
	for _, id := range []string{a, c} {
		i := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
		if i.Status != "resolved" || i.Resolution == nil || i.Resolution.Mode != "next_release" || i.Resolution.Baseline == nil || i.Resolution.Baseline.Version != "app@1" {
			t.Fatalf("bulk next did not retain each real observed baseline: %+v", i)
		}
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, change := range []CompatibleIssueBulkChange{
		{Change: CompatibleIssueChange{Assignment: &CompatibleIssueAssignment{Reference: f.owner.ID}}},
		{Change: CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueClearPolicy}}},
		{Merge: true},
		{Change: CompatibleIssueChange{Status: "reprocessing"}},
	} {
		_, err := f.db.UpdateCompatibleIssues(f.ctx, f.access, "", CompatibleIssueBulkFilter{Limit: 1000}, change)
		triageRequireError(t, err, ErrInvalidCompatibleIssueChange)
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	badTime := time.Now()
	tooManyIDs := make([]string, 1001)
	for n := range tooManyIDs {
		tooManyIDs[n] = a
	}
	for _, filter := range []CompatibleIssueBulkFilter{
		{Limit: 1000, IDs: []string{"01"}}, {Limit: 1000, IDs: []string{"0"}}, {Limit: 1000, IDs: []string{"9223372036854775808"}},
		{Limit: 1000, IDs: tooManyIDs}, {Limit: -1}, {Limit: 1001}, {Limit: 1000, Status: "reprocessing"},
		{Limit: 1000, Sort: "invented"}, {Limit: 1000, Query: "is:invented"}, {Limit: 1000, Start: &badTime, End: &badTime, Period: time.Hour},
	} {
		_, err := f.db.UpdateCompatibleIssues(f.ctx, f.access, "", filter, CompatibleIssueBulkChange{Change: CompatibleIssueChange{Status: "ignored"}})
		triageRequireError(t, err, ErrInvalidCompatibleIssueFilter)
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
}
