//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func compatibleIssueIDs(records []CompatibleIssueRecord) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.Issue.ID)
	}
	return ids
}

func compatibleIssueIDSet(t *testing.T, records []CompatibleIssueRecord, want ...string) {
	t.Helper()
	got := compatibleIssueIDs(records)
	sort.Strings(got)
	expected := append([]string{}, want...)
	sort.Strings(expected)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("issue IDs=%v want=%v", got, expected)
	}
}

func compatibleEmptyIssue(t *testing.T, ctx context.Context, db *Store, project Project, fingerprint string, count int64) string {
	t.Helper()
	var id string
	if err := db.pool.QueryRow(ctx, `INSERT INTO issues(project_id,fingerprint,title,level,platform,count,first_seen,last_seen)
 VALUES($1,$2,'Retained legacy summary','info','go',$3,'2000-01-01Z','2001-01-01Z') RETURNING id::text`, project.ID, fingerprint, count).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCompatibleIssuesGrantsSelectorsAliasesAndCurrentMembership(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	other, err := db.CreateProject(ctx, owner.ID, org.Slug, project.ID, "javascript")
	if err != nil {
		t.Fatal(err)
	}
	third, err := db.CreateProject(ctx, owner.ID, org.Slug, "Third issue project", "python")
	if err != nil {
		t.Fatal(err)
	}
	var foreignOrg string
	if err := db.pool.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('issue-read-foreign','Foreign') RETURNING id::text").Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg, owner.ID); err != nil {
		t.Fatal(err)
	}
	foreign, err := db.CreateProject(ctx, owner.ID, "issue-read-foreign", "Foreign issue project", "go")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	_, root := compatibleCapture(t, ctx, db, project, 1, base, map[string]any{"user": map[string]string{"id": "root-user"}})
	_, alias := compatibleCapture(t, ctx, db, project, 2, base.Add(time.Second), map[string]any{"fingerprint": []string{"issue-source"}, "user": map[string]string{"id": "source-user"}})
	_, hidden := compatibleCapture(t, ctx, db, other, 1, base, nil)
	_, visible := compatibleCapture(t, ctx, db, third, 1, base, nil)
	_, foreignID := compatibleCapture(t, ctx, db, foreign, 1, base, nil)
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{root, alias}); err != nil {
		t.Fatal(err)
	}
	empty := compatibleEmptyIssue(t, ctx, db, project, "empty-read-contract", 19)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, ProjectIDs: []int64{apiTokenProjectID(t, project.ID), apiTokenProjectID(t, third.ID)}}
	all := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	list := func(access ManagementAccess, projectRef string, filter CompatibleIssueFilter, want ...string) []CompatibleIssueRecord {
		t.Helper()
		rows, err := db.CompatibleIssues(ctx, access, projectRef, filter)
		if err != nil {
			t.Fatal(err)
		}
		compatibleIssueIDSet(t, rows, want...)
		return rows
	}
	list(access, "", CompatibleIssueFilter{Limit: 100}, root, visible, empty)
	// Numeric slug precedence is resolved among currently authorized projects.
	list(access, project.ID, CompatibleIssueFilter{Limit: 100}, root, empty)
	list(all, project.ID, CompatibleIssueFilter{Limit: 100}, hidden)
	list(access, project.Slug, CompatibleIssueFilter{Limit: 100}, root, empty)
	list(access, "", CompatibleIssueFilter{Limit: 100, ProjectRefs: []string{project.Slug, third.ID}}, root, visible, empty)
	list(access, "", CompatibleIssueFilter{Limit: 100, ProjectRefs: []string{"-1"}}, root, visible, empty)
	list(access, project.Slug, CompatibleIssueFilter{Limit: 100, ProjectRefs: []string{third.ID}})
	list(access, "", CompatibleIssueFilter{Limit: 100, IDs: []string{alias, root, hidden, visible, foreignID, "9223372036854775807"}}, root, visible)
	list(access, "", CompatibleIssueFilter{Limit: 100, IDs: []string{hidden, foreignID, "9223372036854775807"}})
	list(access, "", CompatibleIssueFilter{Limit: 100, Query: strings.ToLower(project.Slug + "-" + alias), ShortIDLookup: true}, root)
	for _, selector := range []string{other.ID, foreign.ID, "missing-project"} {
		if _, err := db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, ProjectRefs: []string{selector}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("inaccessible explicit project %s error=%v", selector, err)
		}
	}
	for _, id := range []string{hidden, foreignID, "9223372036854775807"} {
		if _, err := db.CompatibleIssue(ctx, access, id, CompatibleIssueDetailFilter{}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden/absent detail %s error=%v", id, err)
		}
	}
	for _, detailAccess := range []ManagementAccess{access, {UserID: owner.ID, AllProjects: true}} {
		record, err := db.CompatibleIssue(ctx, detailAccess, alias, CompatibleIssueDetailFilter{})
		if err != nil || record.Issue.ID != root || record.Issue.Count != 2 || record.UserCount != 2 || record.OrganizationSlug != org.Slug || record.ProjectPlatform != project.Platform {
			t.Fatalf("canonical granted detail=%+v err=%v", record, err)
		}
	}
	emptyDetail, err := db.CompatibleIssue(ctx, access, empty, CompatibleIssueDetailFilter{Environments: []string{"absent"}})
	if err != nil || emptyDetail.Issue.Count != 19 || emptyDetail.UserCount != 0 || emptyDetail.Latest != nil || emptyDetail.Filtered == nil || emptyDetail.Filtered.Count != 0 || emptyDetail.Filtered.FirstSeen != nil || emptyDetail.Filtered.LastSeen != nil || emptyDetail.Filtered.MatchingEventID != nil || emptyDetail.Filtered.MatchingEnvironment != nil {
		t.Fatalf("empty legacy detail=%+v err=%v", emptyDetail, err)
	}
	stats, err := db.CompatibleIssueStats(ctx, access, []string{visible, alias, root, hidden, foreignID, "9223372036854775807"}, "24h")
	if err != nil || len(stats) != 2 || stats[0].ID != visible || stats[1].ID != root || stats[1].Count != 2 || stats[1].UserCount != 2 {
		t.Fatalf("stats canonical grant/order=%+v err=%v", stats, err)
	}
	groupRequireUnchanged(t, ctx, db, before)
	if _, err := db.pool.Exec(ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", org.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed membership list error=%v", err)
	}
	if _, err := db.CompatibleIssue(ctx, access, alias, CompatibleIssueDetailFilter{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed membership detail error=%v", err)
	}
	if _, err := db.CompatibleIssueStats(ctx, access, []string{root}, "24h"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed membership stats error=%v", err)
	}
}

func TestCompatibleIssuesKnownIdentitiesLifetimeAndFilteredMetrics(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	base := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	users := []any{
		map[string]any{"id": "user@example.com", "email": "backup@example.com"},
		map[string]any{"id": "user@example.com", "email": "different@example.com"},
		map[string]any{"id": json.RawMessage("1.00")}, map[string]any{"id": "1"},
		map[string]any{"id": json.RawMessage("1e2")}, map[string]any{"id": "100"},
		map[string]any{"id": json.RawMessage("1.50")}, map[string]any{"id": "1.5"}, map[string]any{"id": "1.50"},
		map[string]any{"id": "   ", "email": "user@example.com"},
		map[string]any{"id": map[string]int{"bad": 1}, "email": "User@example.com"},
		map[string]any{"email": 42, "username": "user@example.com"},
		map[string]any{"username": false, "ip_address": "127.0.0.1"},
		map[string]any{"ip_address": "{{auto}}", "custom": "unknown"},
		map[string]any{"data": map[string]string{"custom": "unknown"}}, map[string]any{"id": "\u00a0\u2003"}, "scalar",
		map[string]any{"id": true, "email": []any{}, "username": "\t", "ip_address": ""},
		map[string]any{"id": "  exact  "}, map[string]any{"id": "exact"},
		map[string]any{"id": json.RawMessage("-0.0")}, map[string]any{"id": "0"},
		map[string]any{"id": json.RawMessage("123456789012345678901234567890")}, map[string]any{"id": "123456789012345678901234567890"},
		map[string]any{"email": "backup@example.com"}, map[string]any{"username": "backup@example.com"},
		map[string]any{"id": "127.0.0.1", "ip_address": "127.0.0.1"}, map[string]any{"ip_address": "127.0.0.1"},
	}
	var root string
	for n, user := range users {
		attrs := map[string]any{"user": user}
		if n == 0 {
			attrs["exception"] = map[string]any{"values": []any{map[string]any{"type": "UnknownHandledShape", "mechanism": map[string]any{"handled": "false"}}}}
			attrs["transaction"] = "POST /checkout"
			attrs["tags"] = map[string]string{"transaction": "shadow"}
		}
		if n >= 13 {
			attrs["environment"] = "staging"
		}
		if n == len(users)-1 {
			attrs["exception"] = map[string]any{"values": []any{map[string]any{"type": "LastError", "value": "Newest global representative", "mechanism": map[string]any{"handled": false}}}}
		}
		_, root = compatibleCapture(t, ctx, db, project, n+1, base.Add(time.Duration(n+1)*time.Minute), attrs)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	detail, err := db.CompatibleIssue(ctx, access, root, CompatibleIssueDetailFilter{Environments: []string{"production"}})
	if err != nil || detail.Issue.Count != 28 || detail.UserCount != 16 || !detail.IsUnhandled || detail.Latest == nil || detail.Latest.ID != fmt.Sprintf("%032x", 28) || detail.Issue.Environment != "staging" || !detail.Issue.FirstSeen.Equal(base.Add(time.Minute)) || !detail.Issue.LastSeen.Equal(base.Add(28*time.Minute)) {
		t.Fatalf("lifetime/global representative=%+v err=%v", detail, err)
	}
	filtered := detail.Filtered
	if filtered == nil || filtered.Count != 13 || filtered.UserCount != 9 || filtered.IsUnhandled || filtered.FirstSeen == nil || !filtered.FirstSeen.Equal(base.Add(time.Minute)) || filtered.LastSeen == nil || !filtered.LastSeen.Equal(base.Add(13*time.Minute)) || filtered.MatchingEventID == nil || *filtered.MatchingEventID != fmt.Sprintf("%032x", 13) || filtered.MatchingEnvironment == nil || *filtered.MatchingEnvironment != "production" {
		t.Fatalf("filtered metrics=%+v", filtered)
	}
	rows, err := db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, Environments: []string{"production"}})
	if err != nil || len(rows) != 1 || rows[0].UserCount != 16 || rows[0].Filtered == nil || rows[0].Filtered.UserCount != 9 || rows[0].Latest == nil || rows[0].Latest.ID != detail.Latest.ID {
		t.Fatalf("list lifetime/matching metrics=%+v err=%v", rows, err)
	}
	rows, err = db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, Query: `transaction:"POST /checkout"`})
	if err != nil || len(rows) != 1 || rows[0].Issue.Count != 28 || rows[0].Filtered == nil || rows[0].Filtered.Count != 1 || rows[0].Latest == nil || rows[0].Latest.ID != detail.Latest.ID {
		t.Fatalf("issue event-mode query did not match the retained transaction name: %+v err=%v", rows, err)
	}
	if rows, err := db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, Query: "transaction:shadow"}); err != nil || len(rows) != 0 {
		t.Fatalf("compatible issue bare transaction used the shadow tag: %+v err=%v", rows, err)
	}
	noMatch, err := db.CompatibleIssue(ctx, access, root, CompatibleIssueDetailFilter{Environments: []string{"absent"}})
	if err != nil || noMatch.Filtered == nil || noMatch.Filtered.Count != 0 || noMatch.Filtered.UserCount != 0 || noMatch.Filtered.IsUnhandled || noMatch.Filtered.FirstSeen != nil || noMatch.Filtered.LastSeen != nil || noMatch.Filtered.MatchingEventID != nil || noMatch.Filtered.MatchingEnvironment != nil || noMatch.Issue.Count != 28 || noMatch.UserCount != 16 {
		t.Fatalf("nonmatching detail=%+v err=%v", noMatch, err)
	}
	var earliestClock, latestClock time.Time
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&earliestClock); err != nil {
		t.Fatal(err)
	}
	native, err := db.IssueDetail(ctx, owner.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&latestClock); err != nil {
		t.Fatal(err)
	}
	if native.Analytics.UserCount != 16 || native.Issue.Count != 28 || len(native.Events) != 28 {
		t.Fatalf("native analytics differ from its own detail: %+v", native)
	}
	series := native.Analytics.Stats["24h"]
	if len(series) != 24 {
		t.Fatalf("native rolling chart buckets=%d", len(series))
	}
	for n, pair := range series {
		if pair[1] != 0 || n > 0 && pair[0] != series[n-1][0]+3600 {
			t.Fatalf("historical events invented chart activity: %v", series)
		}
	}
	end := time.Unix(series[23][0]+3600, 0)
	if end.Before(earliestClock.Truncate(time.Second)) || end.After(latestClock.Truncate(time.Second)) {
		t.Fatalf("chart was not anchored to this DB transaction: end=%s before=%s after=%s", end, earliestClock, latestClock)
	}
	groupRequireUnchanged(t, ctx, db, before)
}

func TestCompatibleIssuesObservedReleasesGroupedCommentsAndCurrentPolicies(t *testing.T) {
	f := resolutionStore(t)
	base := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	metadataOnly := f.release(t, "metadata-only")
	if _, err := f.db.pool.Exec(f.ctx, "UPDATE releases SET created_at='1999-01-01Z',date_released='1999-02-01Z' WHERE id=$1", metadataOnly.ID); err != nil {
		t.Fatal(err)
	}
	_, root := compatibleCapture(t, f.ctx, f.db, f.project, 1, base, map[string]any{"release": "app@first"})
	_, source := compatibleCapture(t, f.ctx, f.db, f.project, 2, base.Add(time.Minute), map[string]any{"fingerprint": []string{"comment-source"}, "release": "app@last", "environment": "staging"})
	compatibleCapture(t, f.ctx, f.db, f.project, 3, base.Add(-time.Minute), map[string]any{"release": "app@deleted"})
	compatibleCapture(t, f.ctx, f.db, f.project, 4, base.Add(2*time.Minute), map[string]any{"release": "app@unassociated"})
	compatibleCapture(t, f.ctx, f.db, f.project, 5, base.Add(time.Minute), map[string]any{"release": "app@last-tie"})
	firstMetadata, lastMetadata := f.release(t, "app@first"), f.release(t, "app@last-tie")
	firstCommits := []ReleaseCommitInput{
		{ID: "first-commit", Message: "First in release order", Data: json.RawMessage(`{"id":"first-commit"}`), PatchSet: []CommitPatch{{Path: "first.js", Type: "M"}}},
		{ID: "second-commit", Message: "Second in release order", Data: json.RawMessage(`{"id":"second-commit"}`)},
	}
	if _, err := f.db.ReplaceReleaseCommits(f.ctx, f.access, "app@first", firstCommits); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ReplaceReleaseCommits(f.ctx, f.access, "app@last-tie", []ReleaseCommitInput{{ID: "later-release-commit", Data: json.RawMessage(`{"id":"later-release-commit"}`)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `UPDATE releases SET
 created_at=CASE version WHEN 'app@first' THEN '2010-01-01Z'::timestamptz ELSE '2005-01-01Z'::timestamptz END,
 date_released=CASE version WHEN 'app@first' THEN '2010-01-02Z'::timestamptz ELSE NULL END
 WHERE organization_id=$1 AND version IN ('app@first','app@last-tie')`, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM releases WHERE version='app@deleted' AND organization_id=$1", f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM release_projects WHERE project_id=$1 AND release_id=(SELECT id FROM releases WHERE organization_id=$2 AND version='app@unassociated')", f.project.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AddComment(f.ctx, f.owner.ID, root, "Root history"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AddComment(f.ctx, f.owner.ID, source, "Source history retained"); err != nil {
		t.Fatal(err)
	}
	deleted, err := f.db.AddComment(f.ctx, f.owner.ID, source, "Source history removed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, source}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.DeleteComment(f.ctx, f.owner.ID, root, deleted.ID); err != nil {
		t.Fatal(err)
	}
	minutes, count := int32(60), int32(10)
	muted, err := f.db.UpdateIssue(f.ctx, f.owner.ID, root, IssueUpdate{Status: "ignored", Ignore: &IssueIgnoreChange{Condition: &IssueIgnoreCondition{Minutes: &minutes, Count: &count}}})
	if err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	detail, err := f.db.CompatibleIssue(f.ctx, f.access, source, CompatibleIssueDetailFilter{Environments: []string{"production"}})
	if err != nil || detail.Issue.ID != root || detail.NumComments != 2 || !reflect.DeepEqual(detail.Issue.Ignore, muted.Ignore) || detail.Issue.Status != "ignored" || detail.FirstRelease == nil || detail.FirstRelease.Version != "app@first" || detail.LastRelease == nil || detail.LastRelease.Version != "app@last-tie" {
		t.Fatalf("observed releases/comments/policy=%+v err=%v", detail, err)
	}
	issueCommits, err := f.db.CompatibleIssueCommits(f.ctx, f.access, source)
	if err != nil || len(issueCommits) != 2 || issueCommits[0].ID != "first-commit" || issueCommits[1].ID != "second-commit" || len(issueCommits[0].PatchSet) != 1 {
		t.Fatalf("first-release commits did not preserve alias root/order: %+v err=%v", issueCommits, err)
	}
	if detail.FirstRelease.ID != firstMetadata.ID || !detail.FirstRelease.DateCreated.Equal(time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)) || detail.FirstRelease.DateReleased == nil || !detail.FirstRelease.DateReleased.Equal(time.Date(2010, 1, 2, 0, 0, 0, 0, time.UTC)) || detail.LastRelease.ID != lastMetadata.ID || !detail.LastRelease.DateCreated.Equal(time.Date(2005, 1, 1, 0, 0, 0, 0, time.UTC)) || detail.LastRelease.DateReleased != nil {
		t.Fatalf("observed release references used metadata ordering or invented dates: first=%+v last=%+v", detail.FirstRelease, detail.LastRelease)
	}
	if native, err := f.db.IssueDetail(f.ctx, f.owner.ID, source); err != nil || native.CommentTotal != 2 || !reflect.DeepEqual(native.Issue.Ignore, muted.Ignore) {
		t.Fatalf("native grouped history/policy=%+v err=%v", native, err)
	}
	if rows, err := f.db.CompatibleIssues(f.ctx, f.access, "", CompatibleIssueFilter{Limit: 100, Query: "is:ignored"}); err != nil || len(rows) != 1 || rows[0].Issue.ID != root || rows[0].Filtered != nil {
		t.Fatalf("current status query invented an occurrence filter: %+v err=%v", rows, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	resolved := f.resolve(t, root, "release", "app@first")
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM releases WHERE version='app@first' AND organization_id=$1", f.org.ID); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	detail, err = f.db.CompatibleIssue(f.ctx, f.access, root, CompatibleIssueDetailFilter{Environments: []string{"absent"}})
	if err != nil || !reflect.DeepEqual(detail.Issue.Resolution, resolved.Resolution) || detail.Issue.Status != "resolved" || detail.FirstRelease == nil || detail.FirstRelease.Version != "app@last" || detail.LastRelease == nil || detail.LastRelease.Version != "app@last-tie" || detail.NumComments != 2 || detail.Filtered == nil || detail.Filtered.Count != 0 {
		t.Fatalf("read changed policy or included deleted/unassociated release=%+v err=%v", detail, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssuesLifetimeOrdersLookaheadAndRealCharts(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	base := time.Date(2024, 4, 5, 0, 0, 0, 0, time.UTC)
	aFirst, a := compatibleCapture(t, ctx, db, project, 1, base.Add(time.Hour), map[string]any{"fingerprint": []string{"order-a"}, "user": map[string]string{"id": "a1"}})
	aLast, _ := compatibleCapture(t, ctx, db, project, 2, base.Add(4*time.Hour), map[string]any{"fingerprint": []string{"order-a"}, "user": map[string]string{"id": "a2"}, "environment": "staging", "message": "Newest A globally"})
	_, b := compatibleCapture(t, ctx, db, project, 3, base, map[string]any{"fingerprint": []string{"order-b"}, "level": "fatal", "user": map[string]string{"id": "b"}})
	compatibleCapture(t, ctx, db, project, 4, base.Add(2*time.Hour), map[string]any{"fingerprint": []string{"order-b"}, "level": "fatal", "user": map[string]string{"id": "b"}})
	compatibleCapture(t, ctx, db, project, 5, base.Add(3*time.Hour), map[string]any{"fingerprint": []string{"order-b"}, "level": "fatal", "user": map[string]string{"id": "b"}})
	_, c := compatibleCapture(t, ctx, db, project, 6, base.Add(2*time.Hour), map[string]any{"fingerprint": []string{"order-c"}, "level": "warning", "environment": "qa"})
	compatibleCapture(t, ctx, db, project, 7, base.Add(4*time.Hour), map[string]any{"fingerprint": []string{"order-c"}, "level": "warning", "environment": "qa"})
	d := compatibleEmptyIssue(t, ctx, db, project, "order-empty", 9)
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	for name, tc := range map[string]struct {
		filter CompatibleIssueFilter
		want   []string
	}{
		"default lifetime last seen":    {CompatibleIssueFilter{Limit: 100}, []string{c, a, b, d}},
		"ascending count ID ties":       {CompatibleIssueFilter{Limit: 100, Sort: "count"}, []string{a, c, b, d}},
		"descending count ID ties":      {CompatibleIssueFilter{Limit: 100, Sort: "-count"}, []string{d, b, c, a}},
		"ascending first seen":          {CompatibleIssueFilter{Limit: 100, Sort: "first_seen"}, []string{d, b, a, c}},
		"ascending priority":            {CompatibleIssueFilter{Limit: 100, Sort: "priority"}, []string{d, c, a, b}},
		"descending priority":           {CompatibleIssueFilter{Limit: 100, Sort: "-priority"}, []string{b, a, c, d}},
		"known users descending":        {CompatibleIssueFilter{Limit: 100, Sort: "user"}, []string{a, b, d, c}},
		"Sentry date alias":             {CompatibleIssueFilter{Limit: 100, Sort: "date"}, []string{c, a, b, d}},
		"Sentry new alias":              {CompatibleIssueFilter{Limit: 100, Sort: "new"}, []string{c, a, b, d}},
		"Sentry frequency alias":        {CompatibleIssueFilter{Limit: 100, Sort: "freq"}, []string{d, b, c, a}},
		"one provider lookahead":        {CompatibleIssueFilter{Limit: 2}, []string{c, a, b}},
		"offset is returned page start": {CompatibleIssueFilter{Limit: 1, Offset: 1}, []string{a, b}},
		"past result":                   {CompatibleIssueFilter{Limit: 1, Offset: 100}, []string{}},
	} {
		rows, err := db.CompatibleIssues(ctx, access, "", tc.filter)
		if err != nil || !reflect.DeepEqual(compatibleIssueIDs(rows), tc.want) {
			t.Fatalf("%s: IDs=%v err=%v want=%v", name, compatibleIssueIDs(rows), err, tc.want)
		}
	}
	filtered, err := db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, Environments: []string{"production"}})
	if err != nil || !reflect.DeepEqual(compatibleIssueIDs(filtered), []string{a, b}) || filtered[0].Issue.Count != 2 || filtered[0].UserCount != 2 || filtered[0].Latest == nil || filtered[0].Latest.ID != aLast.ID || filtered[0].Issue.Title != aLast.Title || filtered[0].Filtered == nil || filtered[0].Filtered.Count != 1 || filtered[0].Filtered.UserCount != 1 || filtered[0].Filtered.MatchingEventID == nil || *filtered[0].Filtered.MatchingEventID != aFirst.ID {
		t.Fatalf("filtered match replaced lifetime ordering/representative: %+v err=%v", filtered, err)
	}
	start, end := base.Add(2*time.Hour), base.Add(4*time.Hour)
	filtered, err = db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, Start: &start, End: &end})
	if err != nil || !reflect.DeepEqual(compatibleIssueIDs(filtered), []string{c, b}) || filtered[0].Filtered == nil || filtered[0].Filtered.Count != 1 || filtered[1].Filtered == nil || filtered[1].Filtered.Count != 2 || filtered[0].Issue.Count != 2 || filtered[1].Issue.Count != 3 {
		t.Fatalf("inclusive/exclusive occurrence bounds/lifetime metrics=%+v err=%v", filtered, err)
	}
	chartEnd := base.Add(24 * time.Hour)
	positions := map[string][]int{a: {1, 4}, b: {0, 2, 3}, c: {2, 4}}
	for _, period := range []string{"24h", "14d"} {
		rows, err := db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, End: &chartEnd, StatsPeriod: period})
		if err != nil {
			t.Fatal(err)
		}
		compatibleIssueIDSet(t, rows, a, b, c)
		for _, row := range rows {
			count, step := 24, int64(3600)
			if period == "14d" {
				count, step = 14, 86400
			}
			want := make([][2]int64, count)
			for n := range want {
				want[n][0] = chartEnd.Unix() - int64(count-n)*step
			}
			if period == "24h" {
				for _, index := range positions[row.Issue.ID] {
					want[index][1]++
				}
			} else {
				want[13][1] = int64(len(positions[row.Issue.ID]))
			}
			if !reflect.DeepEqual(row.Stats[period], want) || row.Filtered == nil || !reflect.DeepEqual(row.Filtered.Stats[period], want) {
				t.Fatalf("actual zero-filled %s chart for %s: stats=%v matching=%+v want=%v", period, row.Issue.ID, row.Stats, row.Filtered, want)
			}
		}
	}
	for _, tc := range []struct {
		start time.Time
		want  string
	}{{base, "24h"}, {base.Add(-48 * time.Hour), "14d"}} {
		rows, err := db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, Start: &tc.start, End: &chartEnd, StatsPeriod: "auto"})
		if err != nil || len(rows) != 3 || len(rows[0].Stats[tc.want]) == 0 || len(rows[0].Stats) != 1 {
			t.Fatalf("auto range %s chart=%+v err=%v", tc.start, rows, err)
		}
	}
	rows, err := db.CompatibleIssues(ctx, access, project.Slug, CompatibleIssueFilter{Limit: 100, StatsPeriod: "14d"})
	if err != nil {
		t.Fatal(err)
	}
	compatibleIssueIDSet(t, rows, a, b, c, d)
	for _, row := range rows {
		if row.Filtered != nil {
			t.Fatal("chart-only project selection invented an occurrence cutoff")
		}
	}
	groupRequireUnchanged(t, ctx, db, before)
	var now time.Time
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	_, recent := compatibleCapture(t, ctx, db, project, 8, now.Add(-10*time.Minute), map[string]any{"fingerprint": []string{"relative-period"}})
	compatibleCapture(t, ctx, db, project, 9, now.Add(-2*time.Hour), map[string]any{"fingerprint": []string{"relative-period"}})
	before = groupDatabaseSnapshot(t, ctx, db, 0)
	rows, err = db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{Limit: 100, Start: &start, End: &end, Period: time.Hour, StatsPeriod: "24h"})
	if err != nil || len(rows) != 1 || rows[0].Issue.ID != recent || rows[0].Issue.Count != 2 || rows[0].Filtered == nil || rows[0].Filtered.Count != 1 {
		t.Fatalf("relative period did not override valid absolute bounds: %+v err=%v", rows, err)
	}
	var chartCount int64
	for _, pair := range rows[0].Stats["24h"] {
		chartCount += pair[1]
	}
	if chartCount != 1 {
		t.Fatalf("chart ignored active relative occurrence predicate: %v", rows[0].Stats)
	}
	groupRequireUnchanged(t, ctx, db, before)
}

func TestCompatibleIssuesRejectInvalidProviderBoundaries(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	_, root := compatibleCapture(t, ctx, db, project, 1, base, nil)
	earlier := base.Add(-time.Second)
	for name, filter := range map[string]CompatibleIssueFilter{
		"zero limit": {}, "large limit": {Limit: 101}, "negative offset": {Limit: 1, Offset: -1},
		"large offset": {Limit: 1, Offset: 100000001}, "negative period": {Limit: 1, Period: -time.Second},
		"reversed despite relative override": {Limit: 1, Start: &base, End: &earlier, Period: time.Hour},
		"equal bounds":                       {Limit: 1, Start: &base, End: &base}, "noncanonical ID": {Limit: 1, IDs: []string{"01"}},
		"too many IDs": {Limit: 1, IDs: make([]string, 101)}, "mixed all-project selector": {Limit: 1, ProjectRefs: []string{"-1", project.ID}},
		"unsupported sort": {Limit: 1, Sort: "trends"}, "unsupported chart": {Limit: 1, StatsPeriod: "1h"},
		"NUL environment": {Limit: 1, Environments: []string{"bad\x00env"}}, "invalid grammar": {Limit: 1, Query: "release:["},
	} {
		if _, err := db.CompatibleIssues(ctx, access, "", filter); !errors.Is(err, ErrInvalidCompatibleIssueFilter) {
			t.Fatalf("%s error=%v", name, err)
		}
	}
	if _, err := db.CompatibleIssue(ctx, access, root, CompatibleIssueDetailFilter{StatsPeriod: "auto"}); !errors.Is(err, ErrInvalidCompatibleIssueFilter) {
		t.Fatalf("detail accepted list-only auto chart: %v", err)
	}
	for _, ids := range [][]string{nil, {"01"}, make([]string, 101)} {
		if _, err := db.CompatibleIssueStats(ctx, access, ids, "24h"); !errors.Is(err, ErrInvalidCompatibleIssueFilter) {
			t.Fatalf("invalid stats IDs error=%v", err)
		}
	}
	if _, err := db.CompatibleIssueStats(ctx, access, []string{root}, ""); !errors.Is(err, ErrInvalidCompatibleIssueFilter) {
		t.Fatalf("stats accepted disabled/absent-resolved chart: %v", err)
	}
}
