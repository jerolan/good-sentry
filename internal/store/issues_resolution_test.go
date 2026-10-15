//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func TestIssueResolutionSelectedPolicyPersists(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	release, _, err := db.CreateRelease(ctx, access, ReleaseCreate{Version: "app@1.2.3", Projects: []string{project.ID}})
	if err != nil {
		t.Fatal(err)
	}
	archived := "archived"
	finished := time.Date(2026, 2, 20, 4, 5, 6, 123456000, time.FixedZone("fixture", -6*3600))
	if _, err := db.UpdateRelease(ctx, access, release.Version, "", ReleaseUpdate{Status: &archived, DateReleased: ReleaseTime{Set: true, Value: &finished}}); err != nil {
		t.Fatal(err)
	}
	id := triageCapture(t, ctx, db, project)
	got, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: "app@1.2.3"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "resolved" || got.Resolution == nil {
		t.Fatalf("selected release resolution was not persisted: status=%s policy=%+v", got.Status, got.Resolution)
	}
	ref := got.Resolution.Release
	if got.Resolution.Mode != "release" || got.Resolution.Baseline != nil || ref == nil || ref.Version != release.Version || !ref.DateCreated.Equal(release.DateCreated) || ref.DateCreated.Location() != time.UTC || ref.DateReleased == nil || !ref.DateReleased.Equal(finished) || ref.DateReleased.Location() != time.UTC {
		t.Fatalf("explicit archived release reference=%+v", ref)
	}
	activities := triageActivities(t, ctx, db, owner.ID, id)
	if len(activities) != 2 || activities[0].Type != "resolved" || activities[0].User == nil || activities[0].User.ID != owner.ID {
		t.Fatalf("selected resolution activity=%+v", activities)
	}
	var data struct{ Resolution *IssueResolution }
	if err := json.Unmarshal(activities[0].Data, &data); err != nil || !reflect.DeepEqual(data.Resolution, got.Resolution) {
		t.Fatalf("selected activity snapshot=%s err=%v", activities[0].Data, err)
	}
}

type resolutionFixture struct {
	ctx     context.Context
	db      *Store
	owner   User
	org     Organization
	project Project
	access  ManagementAccess
}

func resolutionStore(t *testing.T) resolutionFixture {
	t.Helper()
	ctx, db, owner, org, project := telemetryStore(t)
	return resolutionFixture{ctx, db, owner, org, project, ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}}
}

func (f resolutionFixture) release(t *testing.T, version string) Release {
	t.Helper()
	v, err := ParseReleaseVersion(version)
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := f.db.CreateRelease(f.ctx, f.access, ReleaseCreate{Version: v, Projects: []string{f.project.ID}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f resolutionFixture) capture(t *testing.T, n int, version string, stamp time.Time) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"event_id": fmt.Sprintf("%032x", n), "message": "Release resolution proof", "fingerprint": []string{"release-resolution"}, "release": version, "timestamp": stamp.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev)
	if err != nil || !fresh || id == "" {
		t.Fatalf("capture %d issue=%s fresh=%v err=%v", n, id, fresh, err)
	}
	return id
}

func (f resolutionFixture) resolve(t *testing.T, id, mode string, version ReleaseVersion) Issue {
	t.Helper()
	i, err := f.db.UpdateIssue(f.ctx, f.owner.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: mode, Version: version}}})
	if err != nil || i.Resolution == nil || i.ResolvedAt == nil {
		t.Fatalf("resolve mode=%s version=%q: issue=%+v err=%v", mode, version, i, err)
	}
	return i
}

func (f resolutionFixture) actions(t *testing.T, id, action string) []IssueActivity {
	t.Helper()
	result := []IssueActivity{}
	for _, item := range triageActivities(t, f.ctx, f.db, f.owner.ID, id) {
		if item.Type == action {
			result = append(result, item)
		}
	}
	return result
}

func assertResolutionRegression(t *testing.T, f resolutionFixture, id, reason string, previous *IssueResolution) {
	t.Helper()
	i := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
	actions := f.actions(t, id, "regressed")
	if i.Status != "unresolved" || i.Resolution != nil || i.ResolvedAt != nil || i.RegressedAt == nil || len(actions) != 1 || actions[0].User != nil || !actions[0].DateCreated.Equal(*i.RegressedAt) {
		t.Fatalf("regression issue=%+v actions=%+v", i, actions)
	}
	var data struct {
		EventID, Reason string
		ResolvedAt      time.Time
		Resolution      *IssueResolution
	}
	if err := json.Unmarshal(actions[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.EventID == "" || data.Reason != reason || data.ResolvedAt.IsZero() || !reflect.DeepEqual(data.Resolution, previous) {
		t.Fatalf("regression data=%s expected reason=%s previous=%+v", actions[0].Data, reason, previous)
	}
}

func TestIssueResolutionNoOpSurvivesDeletedMetadata(t *testing.T) {
	f := resolutionStore(t)
	anchor := f.release(t, "app@2")
	id := f.capture(t, 1, "app@1", time.Now())
	original := f.resolve(t, id, "release", anchor.Version)
	if original.Resolution.Comparison != "semantic" || original.Resolution.Mode != "release" || original.Resolution.Baseline != nil || original.Resolution.Release == nil || original.Resolution.Release.Version != anchor.Version || !original.Resolution.Release.DateCreated.Equal(anchor.DateCreated) {
		t.Fatalf("chosen policy=%+v", original.Resolution)
	}
	if err := f.db.DeleteRelease(f.ctx, f.access, anchor.Version, ""); err != nil {
		t.Fatal(err)
	}
	history := triageActivities(t, f.ctx, f.db, f.owner.ID, id)
	for _, update := range []IssueUpdate{{Status: "resolved"}, {Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: anchor.Version}}}} {
		got, err := f.db.UpdateIssue(f.ctx, f.owner.ID, id, update)
		if err != nil || !reflect.DeepEqual(got, original) || !reflect.DeepEqual(history, triageActivities(t, f.ctx, f.db, f.owner.ID, id)) {
			t.Fatalf("no-op reloaded missing metadata: %+v %v", got, err)
		}
	}
	member := triageMember(t, f.ctx, f.db, f.org, "resolution-member", "member")
	assigned, err := f.db.UpdateIssue(f.ctx, member.ID, id, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: member.ID}}})
	if err != nil || assigned.AssignedTo == nil || assigned.AssignedTo.ID != member.ID || !reflect.DeepEqual(assigned.Resolution, original.Resolution) || !assigned.ResolvedAt.Equal(*original.ResolvedAt) {
		t.Fatalf("assignment changed resolution: %+v %v", assigned, err)
	}
	simple, err := f.db.UpdateIssue(f.ctx, member.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{}})
	if err != nil || simple.Status != "resolved" || simple.Resolution != nil {
		t.Fatalf("clear to simple resolution: %+v %v", simple, err)
	}
	f.capture(t, 2, "app@1", time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC))
	if got := triageIssue(t, f.ctx, f.db, f.owner.ID, id); got.Status != "unresolved" || got.AssignedTo == nil || got.AssignedTo.ID != member.ID {
		t.Fatalf("simple resolution or assignment regressed: %+v", got)
	}
}

func TestIssueResolutionSemanticFreshReleaseBoundaries(t *testing.T) {
	for _, tc := range []struct {
		version string
		regress bool
		reason  string
	}{
		{"app@1.9", false, ""},
		{"app@2+one", false, ""},
		{"app@2.0.0+two", false, ""},
		{"app@2.0.1", true, "newer_release"},
		{"", true, "missing_release"},
		{".", true, "missing_release"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			f := resolutionStore(t)
			anchor := f.release(t, "app@2+one")
			id := f.capture(t, 1, "app@1", time.Now())
			original := f.resolve(t, id, "release", anchor.Version)
			if original.Resolution.Comparison != "semantic" {
				t.Fatalf("expected semantic classification: %+v", original.Resolution)
			}
			f.capture(t, 2, tc.version, time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC))
			got := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
			if got.Count != 2 {
				t.Fatalf("late distinct capture count=%d", got.Count)
			}
			if tc.regress {
				assertResolutionRegression(t, f, id, tc.reason, original.Resolution)
			} else if got.Status != "resolved" || !reflect.DeepEqual(got.Resolution, original.Resolution) || !got.ResolvedAt.Equal(*original.ResolvedAt) || len(f.actions(t, id, "regressed")) != 0 {
				t.Fatalf("older/equal release regressed: %+v", got)
			}
		})
	}
}

func TestIssueResolutionCreatedOrderAndImmutableReferences(t *testing.T) {
	for _, tc := range []struct {
		name, candidate string
		semantic        bool
		recreate        bool
		regress         bool
	}{
		{"older ID at same time", "z-older", false, false, false},
		{"newer ID at same time", "a-newer", false, false, true},
		{"recreated exact identity", "opaque-anchor", false, true, false},
		{"incomparable semantic fallback", "other@9", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := resolutionStore(t)
			old := f.release(t, "z-older")
			anchorVersion := "opaque-anchor"
			if tc.semantic {
				anchorVersion = "app@2"
			}
			anchor := f.release(t, anchorVersion)
			newer := f.release(t, tc.candidate)
			stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			if _, err := f.db.pool.Exec(f.ctx, "UPDATE releases SET created_at=$1 WHERE id::text=ANY($2::text[])", stamp, []string{old.ID, anchor.ID, newer.ID}); err != nil {
				t.Fatal(err)
			}
			id := f.capture(t, 1, anchorVersion, time.Now())
			original := f.resolve(t, id, "release", anchor.Version)
			wantComparison := "created"
			if tc.semantic {
				wantComparison = "semantic"
			}
			if original.Resolution.Comparison != wantComparison {
				t.Fatalf("comparison=%s want=%s", original.Resolution.Comparison, wantComparison)
			}
			// Changing live metadata cannot move the immutable selected boundary.
			if _, err := f.db.pool.Exec(f.ctx, "UPDATE releases SET created_at=$2 WHERE id=$1::bigint", anchor.ID, stamp.Add(24*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if tc.recreate {
				if err := f.db.DeleteRelease(f.ctx, f.access, anchor.Version, ""); err != nil {
					t.Fatal(err)
				}
				f.release(t, anchorVersion)
			}
			f.capture(t, 2, tc.candidate, time.Now().Add(-time.Hour))
			if tc.regress {
				assertResolutionRegression(t, f, id, "newer_release", original.Resolution)
			} else if got := triageIssue(t, f.ctx, f.db, f.owner.ID, id); got.Status != "resolved" || !reflect.DeepEqual(got.Resolution, original.Resolution) {
				t.Fatalf("saved boundary changed: %+v", got)
			}
		})
	}
}

func TestIssueResolutionNextMetadataSelectionAndFrozenBoundary(t *testing.T) {
	f := resolutionStore(t)
	id := f.capture(t, 1, "app@1", time.Now())
	pending := f.resolve(t, id, "next_release", "")
	if pending.Resolution.Release != nil || pending.Resolution.Baseline == nil || pending.Resolution.Baseline.Version != "app@1" || pending.Resolution.Comparison != "semantic" {
		t.Fatalf("pending policy=%+v", pending.Resolution)
	}
	archived := f.release(t, "app@1.5")
	status := "archived"
	if _, err := f.db.UpdateRelease(f.ctx, f.access, archived.Version, "", ReleaseUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	f.release(t, "app@3") // Earlier creation must not beat the smallest greater version.
	selected := f.release(t, "app@2+one")
	if progress, err := f.db.BindNextIssueRelease(f.ctx, time.Second); err != nil || !progress {
		t.Fatalf("metadata-only bind progress=%v err=%v", progress, err)
	}
	bound := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
	bindings := f.actions(t, id, "resolution_bound")
	if bound.Status != "resolved" || bound.Resolution == nil || bound.Resolution.Release == nil || bound.Resolution.Release.Version != selected.Version || !reflect.DeepEqual(bound.Resolution.Baseline, pending.Resolution.Baseline) || len(bindings) != 1 || bindings[0].User != nil || len(f.actions(t, id, "regressed")) != 0 {
		t.Fatalf("metadata selection=%+v bindings=%+v", bound, bindings)
	}
	var activity struct{ Resolution *IssueResolution }
	if err := json.Unmarshal(bindings[0].Data, &activity); err != nil || !reflect.DeepEqual(activity.Resolution, bound.Resolution) {
		t.Fatalf("binding activity=%s err=%v", bindings[0].Data, err)
	}
	f.capture(t, 2, "app@2+two", time.Now().Add(-time.Hour))
	if got := triageIssue(t, f.ctx, f.db, f.owner.ID, id); got.Status != "resolved" || !reflect.DeepEqual(got.Resolution, bound.Resolution) {
		t.Fatalf("semantic equality used temporal fallback: %+v", got)
	}
	if _, err := f.db.UpdateRelease(f.ctx, f.access, selected.Version, "", ReleaseUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.DeleteRelease(f.ctx, f.access, selected.Version, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.db.DeleteRelease(f.ctx, f.access, "app@1", ""); err != nil {
		t.Fatal(err)
	}
	history := triageActivities(t, f.ctx, f.db, f.owner.ID, id)
	got := f.resolve(t, id, "next_release", "")
	if !reflect.DeepEqual(got.Resolution, bound.Resolution) || !got.ResolvedAt.Equal(*bound.ResolvedAt) || !reflect.DeepEqual(history, triageActivities(t, f.ctx, f.db, f.owner.ID, id)) {
		t.Fatalf("next no-op replaced a deleted/bound snapshot: %+v", got)
	}
	f.capture(t, 3, string(selected.Version), time.Now().Add(-2*time.Hour))
	assertResolutionRegression(t, f, id, "next_release", bound.Resolution)
	if len(f.actions(t, id, "resolution_bound")) != 1 {
		t.Fatal("recreated bound release generated another binding")
	}
}

func TestIssueResolutionNextCaptureBindsAndRegressesOnce(t *testing.T) {
	f := resolutionStore(t)
	id := f.capture(t, 1, "app@1", time.Now())
	pending := f.resolve(t, id, "next_release", "")
	f.capture(t, 2, "app@0.5", time.Now().Add(-24*time.Hour))
	if got := triageIssue(t, f.ctx, f.db, f.owner.ID, id); got.Status != "resolved" || !reflect.DeepEqual(got.Resolution, pending.Resolution) {
		t.Fatalf("ineligible older capture bound/regressed pending policy: %+v", got)
	}
	f.capture(t, 3, "app@2", time.Now().Add(-48*time.Hour))
	bindings := f.actions(t, id, "resolution_bound")
	if len(bindings) != 1 {
		t.Fatalf("capture binding activities=%+v", bindings)
	}
	var bound struct{ Resolution *IssueResolution }
	if err := json.Unmarshal(bindings[0].Data, &bound); err != nil || bound.Resolution == nil || bound.Resolution.Release == nil || bound.Resolution.Release.Version != "app@2" {
		t.Fatalf("bound activity=%s err=%v", bindings[0].Data, err)
	}
	assertResolutionRegression(t, f, id, "next_release", bound.Resolution)
	before := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
	history := triageActivities(t, f.ctx, f.db, f.owner.ID, id)
	raw := json.RawMessage(`{"event_id":"00000000000000000000000000000003","message":"retry with changed metadata","release":"retry-must-not-observe","fingerprint":["release-resolution"]}`)
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := f.db.Ingest(f.ctx, f.project.ID, ev); err != nil || fresh || !reflect.DeepEqual(before, triageIssue(t, f.ctx, f.db, f.owner.ID, id)) || !reflect.DeepEqual(history, triageActivities(t, f.ctx, f.db, f.owner.ID, id)) {
		t.Fatalf("retry mutated resolution/count/history: fresh=%v err=%v", fresh, err)
	}
	if _, err := f.db.Release(f.ctx, f.access, "retry-must-not-observe", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retry observed new release metadata: %v", err)
	}
}

func TestIssueResolutionNextFinalizationAndCreatedFallback(t *testing.T) {
	f := resolutionStore(t)
	id := f.capture(t, 1, "baseline-build", time.Now())
	planned := f.release(t, "planned-build")
	backdated := f.release(t, "backdated-build")
	pending := f.resolve(t, id, "next_release", "")
	if pending.Resolution.Comparison != "created" {
		t.Fatalf("opaque baseline classification=%+v", pending.Resolution)
	}
	oldFinish := pending.ResolvedAt.Add(-time.Second)
	if _, err := f.db.UpdateRelease(f.ctx, f.access, backdated.Version, "", ReleaseUpdate{DateReleased: ReleaseTime{Set: true, Value: &oldFinish}}); err != nil {
		t.Fatal(err)
	}
	var finish time.Time
	if err := f.db.pool.QueryRow(f.ctx, "SELECT clock_timestamp()").Scan(&finish); err != nil {
		t.Fatal(err)
	}
	finalized, err := f.db.UpdateRelease(f.ctx, f.access, planned.Version, "", ReleaseUpdate{DateReleased: ReleaseTime{Set: true, Value: &finish}})
	if err != nil {
		t.Fatal(err)
	}
	f.release(t, "later-created-build")
	if progress, err := f.db.BindNextIssueRelease(f.ctx, time.Second); err != nil || !progress {
		t.Fatalf("finalization bind=%v err=%v", progress, err)
	}
	bound := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
	if bound.Resolution == nil || bound.Resolution.Release == nil || bound.Resolution.Release.Version != planned.Version || bound.Resolution.Release.DateReleased == nil || !bound.Resolution.Release.DateReleased.Equal(*finalized.DateReleased) || !bound.Resolution.Release.DateCreated.Equal(planned.DateCreated) {
		t.Fatalf("effective timestamp selection=%+v", bound.Resolution)
	}
	f.capture(t, 2, "backdated-build", time.Now().Add(-time.Hour))
	if got := triageIssue(t, f.ctx, f.db, f.owner.ID, id); got.Status != "resolved" {
		t.Fatalf("backdated finalization regressed: %+v", got)
	}
	f.capture(t, 3, "later-created-build", time.Now().Add(-time.Hour))
	assertResolutionRegression(t, f, id, "next_release", bound.Resolution)
}

func TestIssueResolutionPendingNextNoOpAndMissingBaselines(t *testing.T) {
	for _, version := range []string{"", ".", "app@1"} {
		t.Run(version, func(t *testing.T) {
			f := resolutionStore(t)
			id := f.capture(t, 1, version, time.Now())
			if version == "app@1" {
				if err := f.db.DeleteRelease(f.ctx, f.access, "app@1", ""); err != nil {
					t.Fatal(err)
				}
			}
			before := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
			if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "next_release"}}}); err == nil || !reflect.DeepEqual(before, triageIssue(t, f.ctx, f.db, f.owner.ID, id)) {
				t.Fatalf("missing/invalid baseline changed issue: %v", err)
			}
		})
	}
	f := resolutionStore(t)
	id := f.capture(t, 1, "app@1", time.Now())
	pending := f.resolve(t, id, "next_release", "")
	if err := f.db.DeleteRelease(f.ctx, f.access, "app@1", ""); err != nil {
		t.Fatal(err)
	}
	history := triageActivities(t, f.ctx, f.db, f.owner.ID, id)
	if got := f.resolve(t, id, "next_release", ""); !reflect.DeepEqual(got, pending) || !reflect.DeepEqual(history, triageActivities(t, f.ctx, f.db, f.owner.ID, id)) {
		t.Fatalf("pending no-op reloaded deleted baseline: %+v", got)
	}
}

func TestIssueResolutionInvalidTargetsAreAtomicAndMembershipCurrent(t *testing.T) {
	f := resolutionStore(t)
	anchor := f.release(t, "app@2")
	id := f.capture(t, 1, "app@1", time.Now())
	minutes := int32(10)
	if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, id, IssueUpdate{Status: "ignored", Ignore: &IssueIgnoreChange{Condition: &IssueIgnoreCondition{Minutes: &minutes}}}); err != nil {
		t.Fatal(err)
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Foreign project metadata", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.CreateRelease(f.ctx, f.access, ReleaseCreate{Version: "other-only", Projects: []string{other.ID}}); err != nil {
		t.Fatal(err)
	}
	before := triageIssue(t, f.ctx, f.db, f.owner.ID, id)
	history := triageActivities(t, f.ctx, f.db, f.owner.ID, id)
	for _, change := range []IssueUpdate{
		{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: "other-only"}}},
		{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: "absent"}}},
		{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: anchor.Version}}, Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: "999999999"}}},
	} {
		if _, err := f.db.UpdateIssue(f.ctx, f.owner.ID, id, change); !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, triageIssue(t, f.ctx, f.db, f.owner.ID, id)) || !reflect.DeepEqual(history, triageActivities(t, f.ctx, f.db, f.owner.ID, id)) {
			t.Fatalf("invalid resolution/assignment partially changed muted issue: %v", err)
		}
	}
	member := triageMember(t, f.ctx, f.db, f.org, "current-resolution-member", "member")
	if _, err := f.db.UpdateIssue(f.ctx, member.ID, id, IssueUpdate{Status: "resolved", Resolution: &IssueResolutionChange{Policy: &IssueResolutionInput{Mode: "release", Version: anchor.Version}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.pool.Exec(f.ctx, "DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint", f.org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	before = triageIssue(t, f.ctx, f.db, f.owner.ID, id)
	if _, err := f.db.UpdateIssue(f.ctx, member.ID, id, IssueUpdate{Status: "unresolved"}); !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, triageIssue(t, f.ctx, f.db, f.owner.ID, id)) {
		t.Fatalf("removed member changed policy: %v", err)
	}
}

func TestIssueResolutionReleaseCandidatesScopePagesAndLiteralQuery(t *testing.T) {
	f := resolutionStore(t)
	id := f.capture(t, 1, "", time.Now())
	versions := make([]ReleaseVersion, 26)
	for index := range versions {
		versions[index] = f.release(t, fmt.Sprintf("release-%02d", index)).Version
	}
	status := "archived"
	if _, err := f.db.UpdateRelease(f.ctx, f.access, versions[7], "", ReleaseUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Other candidates", "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.CreateRelease(f.ctx, f.access, ReleaseCreate{Version: "release-99", Projects: []string{other.ID}}); err != nil {
		t.Fatal(err)
	}
	member := triageMember(t, f.ctx, f.db, f.org, "candidate-reader", "member")
	for page, wantSize := range map[int]int{1: 25, 2: 1, 3: 0} {
		rows, total, err := f.db.IssueReleases(f.ctx, member.ID, id, "ReLeAsE", page)
		if err != nil || total != 26 || len(rows) != wantSize {
			t.Fatalf("candidate page%d rows=%d total=%d err=%v", page, len(rows), total, err)
		}
		for index, row := range rows {
			want := versions[25-(page-1)*25-index]
			if row.Version != want || row.DateCreated.Location() != time.UTC || row.DateReleased != nil || row.Version == versions[7] && row.Status != "archived" {
				t.Fatalf("candidate page%d index%d=%+v want=%s", page, index, row, want)
			}
		}
	}
	literal := f.release(t, "literal%_\\ ' OR 1=1 🐛")
	f.release(t, "literalXAY unrelated")
	for _, query := range []string{"%", "_", "\\", "' OR 1=1", "🐛"} {
		rows, total, err := f.db.IssueReleases(f.ctx, member.ID, id, query, 1)
		if err != nil || total != 1 || len(rows) != 1 || rows[0].Version != literal.Version {
			t.Fatalf("literal query %q rows=%+v total=%d err=%v", query, rows, total, err)
		}
	}
	foreignOrg := triageForeignOrganization(t, f.ctx, f.db, "foreign-candidate-reader")
	outsider := triageMember(t, f.ctx, f.db, foreignOrg, "foreign-candidate-reader", "owner")
	if _, _, err := f.db.IssueReleases(f.ctx, outsider.ID, id, "", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign member read candidates: %v", err)
	}
}
