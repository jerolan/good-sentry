//go:build integration

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
)

func TestIssueGroupsExclusiveWaitCancellationRollsBack(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	a := groupCapture(t, ctx, db, project, groupEvent(t, 1, "wait-a", "Wait A", base))
	b := groupCapture(t, ctx, db, project, groupEvent(t, 2, "wait-b", "Wait B", base))
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	before := groupDatabaseSnapshot(t, ctx, db, 0)
	blocker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	var blockerPID int
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, "SELECT id FROM issues WHERE id=$1::bigint FOR UPDATE", b); err != nil {
		t.Fatal(err)
	}
	mergeCtx, cancelMerge := context.WithCancel(ctx)
	defer cancelMerge()
	mergeDone := make(chan error, 1)
	go func() { _, err := other.MergeIssues(mergeCtx, owner.ID, org.Slug, []string{a, b}); mergeDone <- err }()
	mergePID := groupWaitBlocked(t, ctx, blocker, blockerPID, 1)[0]
	// The merger has reached an issue row only after obtaining the exclusive
	// project gate. Both public mutation paths must wait behind it now.
	newEvent := groupEvent(t, 3, "wait-b", "Wait B fresh", base.Add(time.Second))
	captureCtx, cancelCapture := context.WithCancel(ctx)
	defer cancelCapture()
	captureDone := make(chan error, 1)
	go func() { _, _, err := db.Ingest(captureCtx, project.ID, newEvent); captureDone <- err }()
	triageCtx, cancelTriage := context.WithCancel(ctx)
	defer cancelTriage()
	triageDone := make(chan error, 1)
	go func() {
		_, err := other.UpdateIssue(triageCtx, owner.ID, a, IssueUpdate{Status: "resolved"})
		triageDone <- err
	}()
	groupWaitBlocked(t, ctx, blocker, mergePID, 2)
	cancelCapture()
	cancelTriage()
	groupAwaitCancellation(t, captureDone, "capture")
	groupAwaitCancellation(t, triageDone, "triage")
	cancelMerge()
	groupAwaitCancellation(t, mergeDone, "merge")
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	groupRequireUnchanged(t, ctx, db, before)
	merged, err := other.MergeIssues(ctx, owner.ID, org.Slug, []string{b, a})
	if err != nil || !merged.Changed || merged.Issue.ID != a {
		t.Fatalf("merge retry=%+v err=%v", merged, err)
	}
	if id, fresh, err := db.Ingest(ctx, project.ID, newEvent); err != nil || !fresh || id != a {
		t.Fatalf("capture retry id=%s fresh=%v err=%v", id, fresh, err)
	}
	if i, err := other.UpdateIssue(ctx, owner.ID, a, IssueUpdate{Status: "resolved"}); err != nil || i.Status != "resolved" || i.Count != 3 {
		t.Fatalf("triage retry=%+v err=%v", i, err)
	}
	var origin string
	if err := db.pool.QueryRow(ctx, "SELECT original_issue_id::text FROM events WHERE event_id=$1", newEvent.ID).Scan(&origin); err != nil || origin != b {
		t.Fatalf("retry original=%s want=%s err=%v", origin, b, err)
	}
}

func TestIssueGroupsConcurrentCapturesTriageAndSplit(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	ids := make([]string, 3)
	allEvents := make([]event.Event, 3)
	for n := range ids {
		allEvents[n] = groupEvent(t, n+1, fmt.Sprintf("concurrent-%d", n), fmt.Sprintf("Origin%d", n), base.Add(time.Duration(n)*time.Second))
		ids[n] = groupCapture(t, ctx, db, project, allEvents[n])
	}
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, ids); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	future := make([]event.Event, 24)
	for n := range future {
		future[n] = groupEvent(t, 100+n, fmt.Sprintf("concurrent-%d", n%3), fmt.Sprintf("Fresh%d", n), base.Add(time.Duration(n+10)*time.Second))
	}
	allEvents = append(allEvents, future...)
	start := make(chan struct{})
	results := make(chan error, 3)
	run := func(work func() error) { go func() { <-start; results <- work() }() }
	run(func() error {
		for _, ev := range future {
			id, fresh, err := other.Ingest(ctx, project.ID, ev)
			if err != nil {
				return err
			}
			if !fresh || (id != ids[0] && id != ids[2]) {
				return fmt.Errorf("concurrent capture id=%s fresh=%v", id, fresh)
			}
		}
		return nil
	})
	run(func() error {
		for n := range 24 {
			status := "resolved"
			if n%2 != 0 {
				status = "unresolved"
			}
			if _, err := other.UpdateIssue(ctx, owner.ID, ids[0], IssueUpdate{Status: status}); err != nil {
				return err
			}
		}
		return nil
	})
	run(func() error {
		for range 8 {
			if _, err := db.UnmergeIssue(ctx, owner.ID, ids[0], []string{ids[2]}); err != nil {
				return err
			}
			if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{ids[0], ids[2]}); err != nil {
				return err
			}
		}
		return nil
	})
	close(start)
	var failure error
	for range 3 {
		if err := <-results; err != nil {
			failure = err
		}
	}
	if failure != nil {
		t.Fatal(failure)
	}
	if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{ids[0], ids[2]}); err != nil {
		t.Fatal(err)
	}
	detail, err := db.IssueDetail(ctx, owner.ID, ids[0])
	if err != nil || detail.Issue.Count != 27 || detail.Issue.GroupSize != 3 || len(detail.Events) != 27 {
		t.Fatalf("concurrent final snapshot=%+v events=%d err=%v", detail.Issue, len(detail.Events), err)
	}
	origins, err := db.IssueOrigins(ctx, owner.ID, ids[0], "", 1)
	if err != nil || origins.Total != 3 || len(origins.Data) != 3 {
		t.Fatalf("concurrent origins=%+v err=%v", origins, err)
	}
	for n, row := range origins.Data {
		if row.ID != ids[n] || row.Count != 9 {
			t.Fatalf("lost original partition n=%d row=%+v", n, row)
		}
	}
	actual := map[string]json.RawMessage{}
	for _, row := range detail.Events {
		actual[row.ID] = row.Data
	}
	for _, ev := range allEvents {
		var want, got any
		if err := json.Unmarshal(ev.Data, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(actual[ev.ID], &got); err != nil || !reflect.DeepEqual(want, got) {
			t.Fatalf("concurrent event raw changed %s err=%v", ev.ID, err)
		}
		if id, fresh, err := other.Ingest(ctx, project.ID, ev); err != nil || fresh || id != ids[0] {
			t.Fatalf("post-race retry id=%s fresh=%v err=%v", id, fresh, err)
		}
	}
}

func TestIssueGroupsWorkersSkipFaultInjectedAliases(t *testing.T) {
	for _, mode := range []string{"ignore", "next_release"} {
		t.Run(mode, func(t *testing.T) {
			f := resolutionStore(t)
			ctx, db, owner, org, project := f.ctx, f.db, f.owner, f.org, f.project
			base := time.Now().UTC().Truncate(time.Second)
			a := groupCapture(t, ctx, db, project, groupEvent(t, 1, "worker-a", "Worker root", base))
			b := groupCapture(t, ctx, db, project, groupEvent(t, 2, "worker-b", "Worker alias", base))
			policyTable := "issue_ignores"
			if mode == "ignore" {
				for _, id := range []string{a, b} {
					if _, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(1, 0)}); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				policyTable = "issue_resolutions"
				f.resolve(t, a, "next_release", "")
				f.resolve(t, b, "next_release", "")
			}
			previous := triageIssue(t, ctx, db, owner.ID, b)
			table := pgx.Identifier{policyTable}.Sanitize()
			var savedPolicy string
			if err := db.pool.QueryRow(ctx, "SELECT to_jsonb(p)::text FROM "+table+" p WHERE issue_id=$1::bigint", b).Scan(&savedPolicy); err != nil {
				t.Fatal(err)
			}
			if _, err := db.MergeIssues(ctx, owner.ID, org.Slug, []string{a, b}); err != nil {
				t.Fatal(err)
			}
			// Fault fixture only, AFTER Open016: the productive grouping writer
			// cannot create an active policy on an alias. Restore its old row and
			// status solely to exercise each public worker's observable defense.
			if _, err := db.pool.Exec(ctx, "INSERT INTO "+table+" SELECT * FROM jsonb_populate_record(NULL::"+table+",$1::jsonb)", savedPolicy); err != nil {
				t.Fatal(err)
			}
			if _, err := db.pool.Exec(ctx, "UPDATE issues SET status=$2,resolved_at=$3,regressed_at=$4 WHERE id=$1::bigint", b, previous.Status, previous.ResolvedAt, previous.RegressedAt); err != nil {
				t.Fatal(err)
			}
			if mode == "ignore" {
				if _, err := db.pool.Exec(ctx, "UPDATE issue_ignores SET until=clock_timestamp()-interval '1 second' WHERE issue_id IN($1::bigint,$2::bigint)", a, b); err != nil {
					t.Fatal(err)
				}
			} else {
				f.release(t, "app@1000")
				if _, err := db.pool.Exec(ctx, "UPDATE issue_resolutions SET next_check_at=clock_timestamp()-interval '1 second' WHERE issue_id IN($1::bigint,$2::bigint)", a, b); err != nil {
					t.Fatal(err)
				}
			}
			before := groupAliasFaultSnapshot(t, ctx, db, b)
			process := func() (bool, error) {
				if mode == "ignore" {
					return db.ExpireIssueIgnore(ctx)
				}
				return db.BindNextIssueRelease(ctx, time.Second)
			}
			if progress, err := process(); err != nil || !progress {
				t.Fatalf("eligible root positive control progress=%v err=%v", progress, err)
			}
			root := triageIssue(t, ctx, db, owner.ID, a)
			if mode == "ignore" {
				if root.Status != "unresolved" || root.Ignore != nil || len(ignoreExpiries(t, ctx, db, owner.ID, a)) != 1 {
					t.Fatalf("root expiry=%+v", root)
				}
			} else if root.Status != "resolved" || root.Resolution == nil || root.Resolution.Release == nil || root.Resolution.Release.Version != "app@1000" {
				t.Fatalf("root binding=%+v", root)
			}
			if progress, err := process(); err != nil || progress {
				t.Fatalf("alias must not be a worker candidate progress=%v err=%v", progress, err)
			}
			if after := groupAliasFaultSnapshot(t, ctx, db, b); after != before {
				t.Fatal("worker changed alias policy/state/receipt/activity")
			}
			// This proves public worker skipping with positive controls. It does
			// not simulate or claim a natural topology change after a row wait.
		})
	}
}

func TestIssueGroupsMigration016PreservesLegacyAndReopens(t *testing.T) {
	f := resolutionStore(t)
	ctx, db, owner, project := f.ctx, f.db, f.owner, f.project
	base := time.Now().UTC().Truncate(time.Second)
	a := groupCapture(t, ctx, db, project, groupEvent(t, 1, "migration-a", "Legacy root", base))
	c := groupCapture(t, ctx, db, project, groupEvent(t, 2, "migration-c", "Legacy original NULL", base))
	legacyAlias := groupCapture(t, ctx, db, project, groupEvent(t, 4, "migration-alias", "Legacy alias", base))
	if _, err := db.UpdateIssue(ctx, owner.ID, a, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 5), Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	f.release(t, "app@1000")
	f.resolve(t, c, "release", "app@1000")
	if _, err := db.AddComment(ctx, owner.ID, c, "Historical resolution comment"); err != nil {
		t.Fatal(err)
	}
	groupLegacy015(t, ctx, db)
	// These are valid pre-feature states, not inferred grouping repairs: a
	// summary/progress divergence, a root without retained raw, a NULL origin
	// on an owned legacy error, and a genuinely unowned historical message.
	if _, err := db.pool.Exec(ctx, "UPDATE issues SET count=10 WHERE id=$1::bigint;", a); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE events SET original_issue_id=NULL WHERE issue_id=$1::bigint", c); err != nil {
		t.Fatal(err)
	}
	// A valid015 flat group already has an older same-project root, an
	// unresolved alias and preserved original ownership on its existing raw.
	if _, err := db.pool.Exec(ctx, "UPDATE issues SET merged_into=$2::bigint WHERE id=$1::bigint", legacyAlias, a); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE events SET issue_id=$2::bigint WHERE original_issue_id=$1::bigint", legacyAlias, a); err != nil {
		t.Fatal(err)
	}
	var empty string
	if err := db.pool.QueryRow(ctx, `INSERT INTO issues(project_id,fingerprint,title,level,platform,count,receipt_count,first_seen,last_seen) VALUES($1::bigint,'legacy-no-raw','Legacy no raw','error','javascript',7,7,$2,$2) RETURNING id::text`, project.ID, base).Scan(&empty); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data) VALUES($1::bigint,$2,'message','Unowned legacy message','error',$3,$4)`, project.ID, fmt.Sprintf("%032x", 30), base, json.RawMessage(`{"message":"Legacy root","fingerprint":["migration-a"]}`)); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, ctx, db, 15)
	url := db.pool.Config().ConnString()
	db.Close()
	upgraded, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	if after := groupDatabaseSnapshot(t, ctx, upgraded, 15); !reflect.DeepEqual(before, after) {
		t.Fatal("016 changed historical columns/raw/policies/receipt001..015")
	}
	root := triageIssue(t, ctx, upgraded, owner.ID, a)
	if root.Count != 10 || root.GroupSize != 2 || root.Ignore == nil || root.Ignore.Remaining == nil || *root.Ignore.Remaining != 5 || root.AssignedTo == nil || root.AssignedTo.ID != owner.ID {
		t.Fatalf("016 rejected/rewrote deliberate count divergence: %+v", root)
	}
	if legacy := triageIssue(t, ctx, upgraded, owner.ID, c); legacy.Resolution == nil {
		t.Fatal("016 removed legacy release policy")
	}
	aliasDetail, err := upgraded.IssueDetail(ctx, owner.ID, legacyAlias)
	if err != nil || aliasDetail.Issue.ID != a || aliasDetail.Issue.GroupSize != 2 || len(aliasDetail.Events) != 2 {
		t.Fatalf("016 did not preserve/canonicalize valid legacy group: %+v err=%v", aliasDetail.Issue, err)
	}
	legacyOrigins, err := upgraded.IssueOrigins(ctx, owner.ID, legacyAlias, "", 1)
	if err != nil || legacyOrigins.IssueID != a || legacyOrigins.Total != 2 || len(legacyOrigins.Data) != 2 || legacyOrigins.Data[0].ID != a || legacyOrigins.Data[1].ID != legacyAlias || legacyOrigins.Data[0].Count != 1 || legacyOrigins.Data[1].Count != 1 {
		t.Fatalf("016 changed valid legacy event partitions: %+v err=%v", legacyOrigins, err)
	}
	origins, err := upgraded.IssueOrigins(ctx, owner.ID, empty, "", 1)
	if err != nil || origins.Total != 1 || len(origins.Data) != 1 || origins.Data[0].Count != 0 || origins.Data[0].FirstSeen != nil || origins.Data[0].LastSeen != nil || origins.Data[0].Title != "Legacy no raw" {
		t.Fatalf("valid empty legacy root=%+v err=%v", origins, err)
	}
	probe, err := upgraded.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, constraintErr := probe.Exec(ctx, "UPDATE issues SET merged_into=id WHERE id=$1::bigint", empty)
	if constraintErr == nil {
		constraintErr = probe.Commit(ctx)
	} else {
		_ = probe.Rollback(ctx)
	}
	if constraintErr == nil {
		t.Fatal("016 accepted a self parent")
	}
	beforeRestart := groupDatabaseSnapshot(t, ctx, upgraded, 0)
	upgraded.Close()
	again, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	if after := groupDatabaseSnapshot(t, ctx, again, 0); !reflect.DeepEqual(beforeRestart, after) {
		t.Fatal("reopen reapplied016 or rewrote legacy rows/receipt")
	}
	aliasDetail, err = again.IssueDetail(ctx, owner.ID, legacyAlias)
	if err != nil || aliasDetail.Issue.ID != a || aliasDetail.Issue.GroupSize != 2 || len(aliasDetail.Events) != 2 {
		t.Fatalf("reopen lost valid legacy group: %+v err=%v", aliasDetail.Issue, err)
	}
}

func TestIssueGroupsMigration016RejectsInvalidPreflightAtomically(t *testing.T) {
	for _, name := range []string{"self", "upward", "cross project", "chain", "active alias", "ownership", "owned transaction"} {
		t.Run(name, func(t *testing.T) {
			ctx, db, owner, org, project := telemetryStore(t)
			base := time.Now().UTC().Truncate(time.Second)
			ids := make([]string, 3)
			for n := range ids {
				ids[n] = groupCapture(t, ctx, db, project, groupEvent(t, n+1, fmt.Sprintf("preflight-%d", n), name, base))
			}
			otherProject, err := db.CreateProject(ctx, owner.ID, org.Slug, "Preflight other project", "go")
			if err != nil {
				t.Fatal(err)
			}
			other := groupCapture(t, ctx, db, otherProject, groupEvent(t, 10, "other-preflight", "Other project", base))
			if name == "active alias" {
				if _, err := db.UpdateIssue(ctx, owner.ID, ids[1], IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(0, 5)}); err != nil {
					t.Fatal(err)
				}
			}
			groupLegacy015(t, ctx, db)
			switch name {
			case "self":
				_, err = db.pool.Exec(ctx, "UPDATE issues SET merged_into=id WHERE id=$1::bigint", ids[0])
			case "upward":
				_, err = db.pool.Exec(ctx, "UPDATE issues SET merged_into=$2::bigint WHERE id=$1::bigint", ids[0], ids[1])
			case "cross project":
				_, err = db.pool.Exec(ctx, "UPDATE issues SET merged_into=$2::bigint WHERE id=$1::bigint", other, ids[0])
			case "chain":
				_, err = db.pool.Exec(ctx, "UPDATE issues SET merged_into=CASE WHEN id=$1::bigint THEN $2::bigint ELSE $3::bigint END WHERE id IN($1::bigint,$2::bigint)", ids[2], ids[1], ids[0])
			case "active alias":
				_, err = db.pool.Exec(ctx, "UPDATE issues SET merged_into=$2::bigint WHERE id=$1::bigint", ids[1], ids[0])
				if err == nil {
					_, err = db.pool.Exec(ctx, "UPDATE events SET issue_id=$2::bigint WHERE issue_id=$1::bigint", ids[1], ids[0])
				}
			case "ownership":
				_, err = db.pool.Exec(ctx, "UPDATE events SET original_issue_id=$2::bigint WHERE issue_id=$1::bigint", ids[0], ids[1])
			case "owned transaction":
				_, err = db.pool.Exec(ctx, "UPDATE events SET kind='transaction' WHERE issue_id=$1::bigint", ids[0])
			}
			if err != nil {
				t.Fatal(err)
			}
			before := groupDatabaseSnapshot(t, ctx, db, 0)
			catalog := groupCatalogSnapshot(t, ctx, db)
			opened, err := Open(ctx, db.pool.Config().ConnString())
			if err == nil {
				opened.Close()
				t.Fatal("016 accepted invalid pre-feature state")
			}
			t.Logf("016 rejected %s: %v", name, err)
			groupRequireUnchanged(t, ctx, db, before)
			if after := groupCatalogSnapshot(t, ctx, db); after != catalog {
				t.Fatal("failed016 committed a partial constraint/index change")
			}
		})
	}
}
