//go:build integration

package store

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"goodsentry/internal/event"
	"goodsentry/internal/eventview"
)

func compatibleCapture(t *testing.T, ctx context.Context, db *Store, project Project, n int, stamp time.Time, fields map[string]any) (event.Event, string) {
	t.Helper()
	payload := map[string]any{
		"event_id": fmt.Sprintf("%032x", n), "timestamp": stamp.UTC().Format(time.RFC3339Nano),
		"message": "Compatible event", "fingerprint": []string{"compatible-events"},
		"environment": "production", "release": "app@1", "level": "error",
	}
	for name, value := range fields {
		payload[name] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", stamp)
	if err != nil {
		t.Fatal(err)
	}
	id, fresh, err := db.Ingest(ctx, project.ID, ev)
	if err != nil || !fresh {
		t.Fatalf("capture %s: issue=%s fresh=%v err=%v", ev.ID, id, fresh, err)
	}
	return ev, id
}

func compatibleRecordIDs(records []eventview.Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

func TestCompatibleEventsScopedOwnershipAndCurrentMembership(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	// A visible numeric slug wins; a hidden numeric slug cannot shadow the
	// authorized project's ID. This exercises the provider's scoped resolution.
	other, err := db.CreateProject(ctx, owner.ID, org.Slug, project.ID, "go")
	if err != nil {
		t.Fatal(err)
	}
	var foreignOrg string
	if err := db.pool.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES('compatible-foreign','Foreign') RETURNING id::text").Scan(&foreignOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", foreignOrg, owner.ID); err != nil {
		t.Fatal(err)
	}
	foreign, err := db.CreateProject(ctx, owner.ID, "compatible-foreign", "Foreign project", "go")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	a, root := compatibleCapture(t, ctx, db, project, 1, base, nil)
	b, alias := compatibleCapture(t, ctx, db, project, 2, base.Add(time.Second), map[string]any{"fingerprint": []string{"other-origin"}})
	_, otherIssue := compatibleCapture(t, ctx, db, other, 1, base, nil)
	_, foreignIssue := compatibleCapture(t, ctx, db, foreign, 1, base, nil)
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{root, alias}); err != nil {
		t.Fatal(err)
	}
	const unowned = "ffffffffffffffffffffffffffffffff"
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data)
	 VALUES($1,$2,'message','Legacy unowned','info',$3,'{"message":"Legacy unowned"}')`, project.ID, unowned, base.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	var emptyIssue string
	if err := db.pool.QueryRow(ctx, `INSERT INTO issues(project_id,fingerprint,title,level,platform,first_seen,last_seen)
	 VALUES($1,'empty-compatible','Empty legacy','error','go',$2,$2) RETURNING id::text`, project.ID, base).Scan(&emptyIssue); err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, ProjectIDs: []int64{apiTokenProjectID(t, project.ID)}}
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	for _, ref := range []string{project.ID, project.Slug} {
		rows, err := db.CompatibleEvents(ctx, access, EventProjectTarget(ref), CompatibleEventFilter{Limit: 100})
		if err != nil || !reflect.DeepEqual(compatibleRecordIDs(rows), []string{unowned, b.ID, a.ID}) {
			t.Fatalf("project %s IDs=%v err=%v", ref, compatibleRecordIDs(rows), err)
		}
		if rows[0].GroupID != "" || rows[1].GroupID != root || rows[2].GroupID != root {
			t.Fatalf("project ownership inferred or stale: %+v", rows)
		}
	}
	for _, ref := range []string{root, alias} {
		rows, err := db.CompatibleEvents(ctx, access, EventIssueTarget(ref), CompatibleEventFilter{Limit: 100})
		if err != nil || !reflect.DeepEqual(compatibleRecordIDs(rows), []string{b.ID, a.ID}) || rows[0].GroupID != root || rows[1].GroupID != root {
			t.Fatalf("issue %s IDs=%v err=%v", ref, compatibleRecordIDs(rows), err)
		}
	}
	globalSession := ManagementAccess{UserID: owner.ID, AllProjects: true}
	if detail, err := db.CompatibleEvent(ctx, globalSession, EventIssueTarget(alias), b.ID, nil); err != nil || detail.GroupID != root {
		t.Fatalf("global session issue=%+v err=%v", detail, err)
	}
	if rows, err := db.CompatibleEvents(ctx, globalSession, EventIssueTarget(alias), CompatibleEventFilter{Limit: 100}); err != nil || !reflect.DeepEqual(compatibleRecordIDs(rows), []string{b.ID, a.ID}) {
		t.Fatalf("global session list=%+v err=%v", rows, err)
	}
	all := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	if rows, err := db.CompatibleEvents(ctx, all, EventProjectTarget(project.ID), CompatibleEventFilter{Limit: 100}); err != nil || len(rows) != 1 || rows[0].GroupID != otherIssue {
		t.Fatalf("visible numeric slug did not take precedence: %+v err=%v", rows, err)
	}
	for _, target := range []CompatibleEventTarget{EventProjectTarget(other.ID), EventProjectTarget(foreign.ID), EventIssueTarget(otherIssue), EventIssueTarget(foreignIssue)} {
		if _, err := db.CompatibleEvents(ctx, access, target, CompatibleEventFilter{Limit: 100}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden target list error=%v", err)
		}
		if _, err := db.CompatibleEvent(ctx, access, target, a.ID, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden target detail error=%v", err)
		}
	}
	if rows, err := db.CompatibleEvents(ctx, access, EventIssueTarget(emptyIssue), CompatibleEventFilter{Limit: 100}); err != nil || len(rows) != 0 {
		t.Fatalf("empty issue rows=%v err=%v", rows, err)
	}
	for _, ref := range []string{a.ID, "latest"} {
		if _, err := db.CompatibleEvent(ctx, access, EventIssueTarget(emptyIssue), ref, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("empty issue detail %s error=%v", ref, err)
		}
	}
	if _, err := db.CompatibleEvent(ctx, access, EventIssueTarget(root), unowned, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unowned event appeared in issue: %v", err)
	}
	groupRequireUnchanged(t, ctx, db, before)
	if _, err := db.pool.Exec(ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", org.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	for _, target := range []CompatibleEventTarget{EventProjectTarget(project.ID), EventIssueTarget(root)} {
		if _, err := db.CompatibleEvents(ctx, access, target, CompatibleEventFilter{Limit: 100}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("removed current membership still listed events: %v", err)
		}
		if _, err := db.CompatibleEvent(ctx, access, target, a.ID, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("removed current membership still read event: %v", err)
		}
	}
}

func TestCompatibleEventsOrderingFiltersWindowsAndSample(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	target := EventProjectTarget(project.Slug)
	base := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	stamps := []time.Time{base, base.Add(time.Minute), base.Add(time.Minute), base.Add(2 * time.Minute), base.Add(3 * time.Minute), time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
	attrs := []map[string]any{
		{"message": `payment literal%_\raw`, "tags": map[string]string{"region": "eu", "transaction": "shadow"}, "transaction": "POST /checkout"},
		{"environment": "staging", "release": "app@2", "level": "warning", "tags": [][]string{{"region", "us"}}, "transaction": "POST /checkout"},
		{"release": "app@2", "tags": map[string]string{"region": "eu"}, "transaction": "GET /basket"},
		{"tags": map[string]string{"region": "us"}, "transaction": "POST /checkout"},
		{"environment": "qa", "release": "app@3", "level": "info", "transaction": "POST /checkout"},
		nil,
	}
	events := make([]event.Event, len(stamps))
	var root string
	for n, stamp := range stamps {
		events[n], root = compatibleCapture(t, ctx, db, project, n+1, stamp, attrs[n])
	}
	transaction, _ := compatibleCapture(t, ctx, db, project, 7, base.Add(4*time.Minute), map[string]any{"type": "transaction", "transaction": "transaction-only", "start_timestamp": base.Add(4*time.Minute - time.Second).Format(time.RFC3339Nano)})
	if _, err := db.UpdateIssue(ctx, owner.ID, root, IssueUpdate{Status: "resolved"}); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	check := func(name string, filter CompatibleEventFilter, want []string) []eventview.Record {
		t.Helper()
		rows, err := db.CompatibleEvents(ctx, access, target, filter)
		if err != nil || !reflect.DeepEqual(compatibleRecordIDs(rows), want) {
			t.Fatalf("%s: IDs=%v err=%v want=%v", name, compatibleRecordIDs(rows), err, want)
		}
		for _, row := range rows {
			if row.SiblingsKnown {
				t.Fatalf("list claimed computed siblings: %+v", row)
			}
		}
		return rows
	}
	all := []string{events[4].ID, events[3].ID, events[2].ID, events[1].ID, events[0].ID, events[5].ID}
	check("no implicit cutoff and no transactions", CompatibleEventFilter{Limit: 100}, all)
	check("lookahead is one extra row", CompatibleEventFilter{Limit: 2}, all[:3])
	check("offset denotes page start", CompatibleEventFilter{Limit: 2, Offset: 2}, all[2:5])
	check("past result", CompatibleEventFilter{Limit: 2, Offset: 100}, []string{})
	start, end := base.Add(time.Minute), base.Add(2*time.Minute)
	check("inclusive start exclusive end including equal-time row order", CompatibleEventFilter{Limit: 100, Start: &start, End: &end}, []string{events[2].ID, events[1].ID})
	check("environment OR", CompatibleEventFilter{Limit: 100, Environments: []string{"production", "staging"}}, []string{events[3].ID, events[2].ID, events[1].ID, events[0].ID, events[5].ID})
	check("one occurrence satisfies all selectors", CompatibleEventFilter{Limit: 100, Query: `payment region:eu environment:production release:app@1 level:error is:resolved transaction:"POST /checkout"`}, []string{events[0].ID})
	check("literal percent underscore and slash", CompatibleEventFilter{Limit: 100, Query: `literal%_\raw`}, []string{events[0].ID})
	check("transaction selector uses actual transaction name", CompatibleEventFilter{Limit: 100, Query: `transaction:"POST /checkout"`}, []string{events[4].ID, events[3].ID, events[1].ID, events[0].ID})
	check("explicit transaction tag stays a tag", CompatibleEventFilter{Limit: 100, Query: `tags[transaction]:shadow`}, []string{events[0].ID})
	check("transaction selector does not use shadow tag", CompatibleEventFilter{Limit: 100, Query: `transaction:shadow`}, []string{})
	native, total, err := db.Issues(ctx, owner.ID, org.Slug, IssueFilter{Query: "transaction:shadow", Status: "all"})
	if err != nil || total != 1 || len(native) != 1 || native[0].ID != root {
		t.Fatalf("native bare transaction stopped selecting its tag: issues=%+v total=%d err=%v", native, total, err)
	}
	if row, err := db.CompatibleEvent(ctx, access, target, transaction.ID, nil); err != nil || row.Kind != "transaction" || row.GroupID != "" || !row.SiblingsKnown || row.PreviousID == nil || *row.PreviousID != events[4].ID || row.NextID != nil {
		t.Fatalf("project transaction detail=%+v err=%v", row, err)
	}
	sampled := append([]event.Event{}, events...)
	sort.Slice(sampled, func(i, j int) bool {
		a := fmt.Sprintf("%x", md5.Sum([]byte(project.ID+":"+sampled[i].ID)))
		b := fmt.Sprintf("%x", md5.Sum([]byte(project.ID+":"+sampled[j].ID)))
		return a < b
	})
	wantSample := make([]string, len(sampled))
	for n, ev := range sampled {
		wantSample[n] = ev.ID
	}
	check("stable sample permutation", CompatibleEventFilter{Limit: 100, Sample: true}, wantSample)
	check("sample offset and lookahead", CompatibleEventFilter{Limit: 2, Offset: 1, Sample: true}, wantSample[1:4])
	groupRequireUnchanged(t, ctx, db, before)
	var databaseNow time.Time
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&databaseNow); err != nil {
		t.Fatal(err)
	}
	recent, _ := compatibleCapture(t, ctx, db, project, 8, databaseNow.Add(-10*time.Minute), nil)
	compatibleCapture(t, ctx, db, project, 9, databaseNow.Add(-2*time.Hour), nil)
	check("relative period overrides valid absolute bounds", CompatibleEventFilter{Limit: 100, Start: &start, End: &end, Period: time.Hour}, []string{recent.ID})
}

func TestCompatibleEventDetailSiblingsReleaseAndNoSideEffects(t *testing.T) {
	f := resolutionStore(t)
	release := f.release(t, "app@1")
	base := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	a, root := compatibleCapture(t, f.ctx, f.db, f.project, 1, base, nil)
	b, _ := compatibleCapture(t, f.ctx, f.db, f.project, 2, base.Add(time.Minute), map[string]any{"environment": "staging"})
	c, _ := compatibleCapture(t, f.ctx, f.db, f.project, 3, base.Add(time.Minute), nil)
	d, alias := compatibleCapture(t, f.ctx, f.db, f.project, 4, base.Add(2*time.Minute), map[string]any{"fingerprint": []string{"detail-source"}})
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, alias}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AddComment(f.ctx, f.owner.ID, root, "Read acceptance history"); err != nil {
		t.Fatal(err)
	}
	f.resolve(t, root, "release", "app@1")
	expected := map[string]event.Event{a.ID: a, b.ID: b, c.ID: c, d.ID: d}
	receipts := make(map[string]time.Time, len(expected))
	for id := range expected {
		var receipt time.Time
		if err := f.db.pool.QueryRow(f.ctx, "SELECT created_at FROM events WHERE project_id=$1 AND event_id=$2", f.project.ID, id).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		receipts[id] = receipt
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	check := func(target CompatibleEventTarget, ref string, envs []string, want string, previous, next *string) {
		t.Helper()
		record, err := f.db.CompatibleEvent(f.ctx, f.access, target, ref, envs)
		if err != nil || record.ID != want || record.GroupID != root || !record.SiblingsKnown || !reflect.DeepEqual(record.PreviousID, previous) || !reflect.DeepEqual(record.NextID, next) {
			t.Fatalf("detail %s: record=%+v err=%v want=%s prev=%v next=%v", ref, record, err, want, previous, next)
		}
		if record.ReleaseInfo == nil || record.ReleaseInfo.ID != release.ID || record.ReleaseInfo.Version != "app@1" || !record.ReleaseInfo.DateCreated.Equal(release.DateCreated) || !reflect.DeepEqual(record.ReleaseInfo.DateReleased, release.DateReleased) {
			t.Fatalf("detail lost associated release: %+v", record.ReleaseInfo)
		}
		stored := expected[want]
		var raw, wantRaw any
		if err := json.Unmarshal(record.Data, &raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(stored.Data, &wantRaw); err != nil {
			t.Fatal(err)
		}
		if !record.Received.Equal(receipts[want]) || record.ProjectID != apiTokenProjectID(t, f.project.ID) || !record.Timestamp.Equal(stored.Timestamp) || record.Title != stored.Title || record.Level != stored.Level || record.Environment != stored.Environment || record.Release != stored.Release || !reflect.DeepEqual(raw, wantRaw) {
			t.Fatalf("detail lost durable metadata/raw identity: %+v", record)
		}
	}
	check(EventIssueTarget(alias), c.ID, []string{"production"}, c.ID, &a.ID, &d.ID)
	check(EventProjectTarget(f.project.ID), c.ID, nil, c.ID, &b.ID, &d.ID)
	check(EventIssueTarget(root), "latest", nil, d.ID, &c.ID, nil)
	check(EventIssueTarget(alias), "recommended", nil, d.ID, &c.ID, nil)
	check(EventIssueTarget(root), "oldest", nil, a.ID, nil, &b.ID)
	for _, ref := range []string{a.ID, "latest", "oldest", "recommended"} {
		if _, err := f.db.CompatibleEvent(f.ctx, f.access, EventIssueTarget(alias), ref, []string{"absent"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("environment-hidden detail %s returned %v", ref, err)
		}
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, err := f.db.UnmergeIssue(f.ctx, f.owner.ID, root, []string{alias}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CompatibleEvent(f.ctx, f.access, EventIssueTarget(root), d.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("separated event remained in original group: %v", err)
	}
	if record, err := f.db.CompatibleEvent(f.ctx, f.access, EventIssueTarget(alias), "latest", nil); err != nil || record.ID != d.ID || record.GroupID != alias || record.PreviousID != nil || record.NextID != nil {
		t.Fatalf("separated detail=%+v err=%v", record, err)
	}
}

func TestCompatibleEventsRejectInvalidProviderBoundaries(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ev, root := compatibleCapture(t, ctx, db, project, 1, base, nil)
	earlier := base.Add(-time.Second)
	for name, filter := range map[string]CompatibleEventFilter{
		"zero limit": {}, "large limit": {Limit: 101}, "negative offset": {Limit: 1, Offset: -1},
		"large offset": {Limit: 1, Offset: 100000001}, "negative period": {Limit: 1, Period: -time.Second},
		"reversed instants": {Limit: 1, Start: &base, End: &earlier}, "equal instants": {Limit: 1, Start: &base, End: &base},
		"NUL environment":       {Limit: 1, Environments: []string{"bad\x00env"}},
		"too many environments": {Limit: 1, Environments: make([]string, 101)},
		"long environment":      {Limit: 1, Environments: []string{strings.Repeat("界", 201)}},
		"invalid search":        {Limit: 1, Query: "release:["},
	} {
		if _, err := db.CompatibleEvents(ctx, access, EventProjectTarget(project.ID), filter); !errors.Is(err, ErrInvalidEventFilter) {
			t.Fatalf("%s error=%v", name, err)
		}
	}
	for _, target := range []CompatibleEventTarget{EventProjectTarget(""), EventIssueTarget("01")} {
		if _, err := db.CompatibleEvents(ctx, access, target, CompatibleEventFilter{Limit: 1}); !errors.Is(err, ErrInvalidEventFilter) {
			t.Fatalf("invalid target list error=%v", err)
		}
	}
	if _, err := db.CompatibleEvent(ctx, access, EventProjectTarget(project.ID), "latest", nil); !errors.Is(err, ErrInvalidEventFilter) {
		t.Fatalf("project accepted issue-only selector: %v", err)
	}
	if _, err := db.CompatibleEvent(ctx, access, EventIssueTarget(root), ev.ID, []string{"bad\x00env"}); !errors.Is(err, ErrInvalidEventFilter) {
		t.Fatalf("detail accepted invalid environments: %v", err)
	}
}
