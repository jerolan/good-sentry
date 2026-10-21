//go:build integration

package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func compatibleApply(t *testing.T, f resolutionFixture, id string, change CompatibleIssueChange) CompatibleIssueRecord {
	t.Helper()
	record, err := f.db.UpdateCompatibleIssue(f.ctx, f.access, id, change)
	if err != nil {
		t.Fatalf("compatible issue change %+v: %v", change, err)
	}
	return record
}

func TestCompatibleIssueUpdateGrantsCurrentMembershipAndAliases(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour)
	_, root := compatibleCapture(t, f.ctx, f.db, f.project, 1, stamp, map[string]any{"user": map[string]string{"id": "owner"}})
	_, source := compatibleCapture(t, f.ctx, f.db, f.project, 2, stamp.Add(time.Second), map[string]any{"fingerprint": []string{"write-source"}})
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other", "go")
	if err != nil {
		t.Fatal(err)
	}
	_, hidden := compatibleCapture(t, f.ctx, f.db, other, 3, stamp, nil)
	foreign := triageForeignOrganization(t, f.ctx, f.db, "foreign-write")
	member := triageMember(t, f.ctx, f.db, f.org, "writer-member", "member")
	access := ManagementAccess{UserID: member.ID, OrganizationID: f.org.ID, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}}
	if _, err = f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, source}); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, change := range []CompatibleIssueChange{{}, {Status: "resolved"}} {
		readOnly := access
		readOnly.ReadOnly = true
		_, err = f.db.UpdateCompatibleIssue(f.ctx, readOnly, root, change)
		triageRequireError(t, err, ErrForbidden)
		_, err = f.db.UpdateCompatibleIssue(f.ctx, access, hidden, change)
		triageRequireError(t, err, ErrNotFound)
		wrongOrg := access
		wrongOrg.OrganizationID = foreign.ID
		_, err = f.db.UpdateCompatibleIssue(f.ctx, wrongOrg, root, change)
		triageRequireError(t, err, ErrNotFound)
		emptyGrant := access
		emptyGrant.ProjectIDs = nil
		_, err = f.db.UpdateCompatibleIssue(f.ctx, emptyGrant, root, change)
		triageRequireError(t, err, ErrNotFound)
		_, err = f.db.UpdateCompatibleIssue(f.ctx, access, source, change)
		var alias *IssueMergedError
		if !errors.As(err, &alias) || alias.MergedInto != root {
			t.Fatalf("stale alias silently retargeted: error=%v", err)
		}
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	got, err := f.db.UpdateCompatibleIssue(f.ctx, access, root, CompatibleIssueChange{Status: "resolved"})
	if err != nil || got.Issue.ID != root || got.Issue.Status != "resolved" || got.Issue.Count != 2 || got.UserCount != 1 || got.Latest == nil || got.Latest.GroupID != root || got.OrganizationSlug != f.org.Slug {
		t.Fatalf("member writer response: %+v error=%v", got, err)
	}
	// Native session access resolves its organization from the issue under lock.
	global := ManagementAccess{UserID: member.ID, AllProjects: true}
	if got, err = f.db.UpdateCompatibleIssue(f.ctx, global, root, CompatibleIssueChange{}); err != nil || got.Issue.Status != "resolved" {
		t.Fatalf("global session read: %+v error=%v", got, err)
	}
	if _, err = f.db.pool.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2", f.org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, change := range []CompatibleIssueChange{{}, {Status: "unresolved"}} {
		_, err = f.db.UpdateCompatibleIssue(f.ctx, access, root, change)
		triageRequireError(t, err, ErrNotFound)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssueUpdateAssignmentReferencesAndAtomicFailures(t *testing.T) {
	f := resolutionStore(t)
	id := triageCapture(t, f.ctx, f.db, f.project)
	target := triageMember(t, f.ctx, f.db, f.org, "assign-target", "member")
	var team string
	if err := f.db.pool.QueryRow(f.ctx, "INSERT INTO teams(organization_id,slug,name) VALUES($1,'on-call','On call') RETURNING id::text", f.org.ID).Scan(&team); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{target.ID, "user:" + target.ID, strings.ToUpper(target.Email)} {
		got := compatibleApply(t, f, id, CompatibleIssueChange{Assignment: &CompatibleIssueAssignment{Reference: reference}})
		if got.Issue.AssignedTo == nil || got.Issue.AssignedTo.ID != target.ID || got.Issue.AssignedTo.Type != "user" || got.Issue.AssignedTo.Email != target.Email {
			t.Fatalf("assignment %q: %+v", reference, got.Issue)
		}
	}
	activities := f.actions(t, id, "assigned")
	if len(activities) != 1 || activities[0].Type != "assigned" || activities[0].User == nil || activities[0].User.ID != f.owner.ID {
		t.Fatalf("equivalent references repeated history or lost actor: %+v", activities)
	}
	got := compatibleApply(t, f, id, CompatibleIssueChange{Assignment: &CompatibleIssueAssignment{Reference: "team:" + team}})
	if got.Issue.AssignedTo == nil || got.Issue.AssignedTo.Type != "team" || got.Issue.AssignedTo.ID != team || got.Issue.AssignedTo.Slug != "on-call" {
		t.Fatalf("team assignment: %+v", got.Issue)
	}
	got = compatibleApply(t, f, id, CompatibleIssueChange{Assignment: &CompatibleIssueAssignment{}})
	if got.Issue.AssignedTo != nil {
		t.Fatalf("explicit unassign: %+v", got.Issue)
	}
	foreign := triageForeignOrganization(t, f.ctx, f.db, "assignment-foreign")
	outsider := triageMember(t, f.ctx, f.db, foreign, "outsider", "member")
	var foreignTeam string
	if err := f.db.pool.QueryRow(f.ctx, "INSERT INTO teams(organization_id,slug,name) VALUES($1,'elsewhere','Elsewhere') RETURNING id::text", foreign.ID).Scan(&foreignTeam); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `INSERT INTO users(email,name,password_hash) VALUES(upper($1),'Fold duplicate','fixture')`, target.Email); err == nil {
		t.Fatal("case-fold duplicate user email was accepted")
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	for _, tc := range []struct {
		reference string
		want      error
	}{
		{outsider.ID, ErrNotFound}, {outsider.Email, ErrNotFound}, {"team:" + foreignTeam, ErrNotFound}, {target.Name, ErrNotFound},
		{"user:9223372036854775807", ErrNotFound}, {"team:9223372036854775807", ErrNotFound},
		{"01", ErrInvalidCompatibleIssueChange}, {"user:01", ErrInvalidCompatibleIssueChange}, {"team:-1", ErrInvalidCompatibleIssueChange},
	} {
		_, err := f.db.UpdateCompatibleIssue(f.ctx, f.access, id, CompatibleIssueChange{Status: "resolved", Assignment: &CompatibleIssueAssignment{Reference: tc.reference}})
		if !errors.Is(err, tc.want) {
			t.Fatalf("mixed invalid target %q: %v want %v", tc.reference, err, tc.want)
		}
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
}

func TestCompatibleIssueUpdatePolicyIntentAndEmptyRead(t *testing.T) {
	f := resolutionStore(t)
	id := triageCapture(t, f.ctx, f.db, f.project)
	minutes, zero := int32(5), int32(0)
	got := compatibleApply(t, f, id, CompatibleIssueChange{Status: "muted", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Minutes: &minutes}})
	if got.Issue.Status != "ignored" || got.Issue.Ignore == nil || *got.Issue.Ignore.Minutes != minutes {
		t.Fatalf("muted alias: %+v", got.Issue)
	}
	if _, err := f.db.pool.Exec(f.ctx, "UPDATE issue_ignores SET until=clock_timestamp()-interval '1 second' WHERE issue_id=$1", id); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	got = compatibleApply(t, f, id, CompatibleIssueChange{})
	if got.Issue.Status != "ignored" || got.Issue.Ignore == nil || got.Issue.Ignore.Until == nil || !got.Issue.Ignore.Until.Before(time.Now()) {
		t.Fatalf("empty change expired the due policy: %+v", got.Issue)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	resolutionClear := CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueClearResolutionPolicy}}
	_, err := f.db.UpdateCompatibleIssue(f.ctx, f.access, id, resolutionClear)
	triageRequireError(t, err, ErrInvalidCompatibleIssueChange)
	groupRequireUnchanged(t, f.ctx, f.db, before)
	// A policy-only clear uses locked ignored status and chooses indefinite mute.
	got = compatibleApply(t, f, id, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueClearPolicy}})
	if got.Issue.Status != "ignored" || got.Issue.Ignore != nil {
		t.Fatalf("clear ignored condition: %+v", got.Issue)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	compatibleApply(t, f, id, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy}})
	groupRequireUnchanged(t, f.ctx, f.db, before)
	for _, bad := range []CompatibleIssueChange{
		{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Count: &zero}},
		{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Minutes: &zero}},
		{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueLatestReleasePolicy}},
		{Status: "resolved", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Minutes: &minutes}},
		{Status: "resolvedInNextRelease", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueClearPolicy}},
	} {
		_, err := f.db.UpdateCompatibleIssue(f.ctx, f.access, id, bad)
		triageRequireError(t, err, ErrInvalidCompatibleIssueChange)
		groupRequireUnchanged(t, f.ctx, f.db, before)
	}
	compatibleApply(t, f, id, CompatibleIssueChange{Status: "resolved"})
	f.release(t, "policy-boundary")
	got = compatibleApply(t, f, id, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueReleasePolicy, Version: "policy-boundary"}})
	if got.Issue.Status != "resolved" || got.Issue.Resolution == nil || got.Issue.Resolution.Release.Version != "policy-boundary" {
		t.Fatalf("policy-only resolution: %+v", got.Issue)
	}
	got = compatibleApply(t, f, id, resolutionClear)
	if got.Issue.Status != "resolved" || got.Issue.Resolution != nil || got.Issue.ResolvedAt == nil {
		t.Fatalf("resolution-only clear: %+v", got.Issue)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	compatibleApply(t, f, id, resolutionClear)
	groupRequireUnchanged(t, f.ctx, f.db, before)
	compatibleApply(t, f, id, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueReleasePolicy, Version: "policy-boundary"}})
	got = compatibleApply(t, f, id, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueClearPolicy}})
	if got.Issue.Status != "resolved" || got.Issue.Resolution != nil || got.Issue.ResolvedAt == nil {
		t.Fatalf("clear resolved condition: %+v", got.Issue)
	}
	compatibleApply(t, f, id, CompatibleIssueChange{Status: "unresolved"})
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	compatibleApply(t, f, id, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueClearPolicy}})
	groupRequireUnchanged(t, f.ctx, f.db, before)
	_, err = f.db.UpdateCompatibleIssue(f.ctx, f.access, id, resolutionClear)
	triageRequireError(t, err, ErrInvalidCompatibleIssueChange)
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssueUpdateLatestAssociationAndImmutablePolicy(t *testing.T) {
	f := resolutionStore(t)
	_, id := compatibleCapture(t, f.ctx, f.db, f.project, 1, time.Now(), nil)
	f.release(t, "metadata-first")
	last := f.release(t, "metadata-last")
	old, tie := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := f.db.pool.Exec(f.ctx, "UPDATE releases SET created_at=CASE WHEN version IN ('metadata-first','metadata-last') THEN $2::timestamptz ELSE $1::timestamptz END", old, tie); err != nil {
		t.Fatal(err)
	}
	archived := "archived"
	if _, err := f.db.UpdateRelease(f.ctx, f.access, last.Version, "", ReleaseUpdate{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Release elsewhere", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.db.CreateRelease(f.ctx, f.access, ReleaseCreate{Version: "other-project-newer", Projects: []string{other.ID}}); err != nil {
		t.Fatal(err)
	}
	command := CompatibleIssueChange{Status: "resolved", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueLatestReleasePolicy}}
	got := compatibleApply(t, f, id, command)
	if got.Issue.Resolution == nil || got.Issue.Resolution.Release == nil || got.Issue.Resolution.Release.Version != last.Version || !got.Issue.Resolution.Release.DateCreated.Equal(tie) {
		t.Fatalf("latest ignored project association/tie/archived metadata: %+v", got.Issue)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	compatibleApply(t, f, id, command)
	groupRequireUnchanged(t, f.ctx, f.db, before)
	newest := f.release(t, "new-associated")
	got = compatibleApply(t, f, id, command)
	if got.Issue.Resolution.Release.Version != newest.Version {
		t.Fatalf("dynamic latest did not advance: %+v", got.Issue)
	}
	frozen := got.Issue
	if _, err = f.db.pool.Exec(f.ctx, "DELETE FROM releases WHERE organization_id=$1 AND version=$2", f.org.ID, newest.Version); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	got = compatibleApply(t, f, id, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueReleasePolicy, Version: newest.Version}})
	if !reflect.DeepEqual(got.Issue, frozen) {
		t.Fatalf("equal policy lost immutable snapshot: got=%+v frozen=%+v", got.Issue, frozen)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	got = compatibleApply(t, f, id, CompatibleIssueChange{Status: "resolvedInNextRelease"})
	if got.Issue.Status != "resolved" || got.Issue.Resolution == nil || got.Issue.Resolution.Mode != "next_release" || got.Issue.Resolution.Baseline == nil || got.Issue.Resolution.Baseline.Version != "app@1" {
		t.Fatalf("next-release alias: %+v", got.Issue)
	}
	_, empty := compatibleCapture(t, f.ctx, f.db, other, 2, time.Now(), map[string]any{"release": "", "fingerprint": []string{"no-associated-release"}})
	if _, err = f.db.pool.Exec(f.ctx, "DELETE FROM release_projects WHERE project_id=$1", other.ID); err != nil {
		t.Fatal(err)
	}
	before = groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	_, err = f.db.UpdateCompatibleIssue(f.ctx, f.access, empty, command)
	triageRequireError(t, err, ErrNotFound)
	groupRequireUnchanged(t, f.ctx, f.db, before)
}

func TestCompatibleIssueUpdateCaptureLifecyclePreservesProgressAndQueue(t *testing.T) {
	f := resolutionStore(t)
	config := alertConfig("Compatible mute lifecycle")
	config.Quantity = 3
	rule, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour)
	_, id := compatibleCapture(t, f.ctx, f.db, f.project, 1, stamp, nil)
	count := int32(2)
	command := CompatibleIssueChange{Status: "muted", Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Count: &count}}
	compatibleApply(t, f, id, command)
	late, _ := compatibleCapture(t, f.ctx, f.db, f.project, 2, stamp.Add(-24*time.Hour), nil)
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	got := compatibleApply(t, f, id, command)
	if got.Issue.Ignore == nil || *got.Issue.Ignore.Remaining != 1 || got.Issue.Count != 2 {
		t.Fatalf("equal compatible policy reset additional-count progress: %+v", got.Issue)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, late); err != nil || fresh {
		t.Fatalf("retry became a fresh receipt: fresh=%v error=%v", fresh, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	latest, _ := compatibleCapture(t, f.ctx, f.db, f.project, 3, stamp.Add(time.Second), nil)
	got = compatibleApply(t, f, id, CompatibleIssueChange{})
	if got.Issue.Status != "unresolved" || got.Issue.Ignore != nil || got.Issue.Count != 3 || len(ignoreExpiries(t, f.ctx, f.db, f.owner.ID, id)) != 1 {
		t.Fatalf("compatible policy did not use native capture expiry: %+v", got.Issue)
	}
	deliveries, _, err := f.db.AlertDeliveries(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID, 1)
	if err != nil || len(deliveries) != 1 || deliveries[0].Payload.EventID != latest.ID || deliveries[0].Payload.Count != 3 {
		t.Fatalf("expiry capture ordinary notification: %+v error=%v", deliveries, err)
	}
	raw := groupRawSnapshot(t, f.ctx, f.db)
	compatibleApply(t, f, id, CompatibleIssueChange{Status: "resolved", Assignment: &CompatibleIssueAssignment{Reference: f.owner.Email}})
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, latest); err != nil || fresh {
		t.Fatalf("resolved retry changed admission: fresh=%v error=%v", fresh, err)
	}
	if after, _, err := f.db.AlertDeliveries(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID, 1); err != nil || !reflect.DeepEqual(after, deliveries) || groupRawSnapshot(t, f.ctx, f.db) != raw {
		t.Fatalf("triage/retry replayed history or rewrote raw events: %+v error=%v", after, err)
	}
	compatibleCapture(t, f.ctx, f.db, f.project, 4, stamp.Add(2*time.Second), nil)
	got = compatibleApply(t, f, id, CompatibleIssueChange{})
	if got.Issue.Status != "unresolved" || got.Issue.RegressedAt == nil || got.Issue.AssignedTo == nil || got.Issue.AssignedTo.ID != f.owner.ID || len(f.actions(t, id, "regressed")) != 1 {
		t.Fatalf("API-created resolution did not regress through native capture: %+v", got.Issue)
	}
}
