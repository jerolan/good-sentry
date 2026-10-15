//go:build integration

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
)

func triageEvent(t *testing.T, id string, stamp time.Time) event.Event {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"event_id": id, "timestamp": stamp.UTC().Format(time.RFC3339Nano), "message": "Triage regression fixture", "fingerprint": []string{"triage-workflow"}, "level": "error"})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.Normalize(raw, "", stamp)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func triageCapture(t *testing.T, ctx context.Context, db *Store, project Project) string {
	t.Helper()
	id, created, err := db.Ingest(ctx, project.ID, triageEvent(t, "00000000000000000000000000000001", time.Now()))
	if err != nil || !created || id == "" {
		t.Fatalf("capture issue=%q created=%v err=%v", id, created, err)
	}
	return id
}

func triageMember(t *testing.T, ctx context.Context, db *Store, org Organization, label, role string) User {
	t.Helper()
	var u User
	u.Name = label
	u.Email = label + "@example.test"
	if err := db.pool.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES($1,$2,'fixture') RETURNING id::text`, u.Email, u.Name).Scan(&u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,$3)`, org.ID, u.ID, role); err != nil {
		t.Fatal(err)
	}
	return u
}

func triageForeignOrganization(t *testing.T, ctx context.Context, db *Store, label string) Organization {
	t.Helper()
	var org Organization
	org.Slug = label
	org.Name = label
	if err := db.pool.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES($1,$1) RETURNING id::text`, label).Scan(&org.ID); err != nil {
		t.Fatal(err)
	}
	return org
}

func triageIssue(t *testing.T, ctx context.Context, db *Store, userID, id string) Issue {
	t.Helper()
	got, err := db.IssueDetail(ctx, userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Issue
}

func triageActivities(t *testing.T, ctx context.Context, db *Store, userID, id string) []IssueActivity {
	t.Helper()
	var result []IssueActivity
	for page := 1; ; page++ {
		rows, total, err := db.IssueActivities(ctx, userID, id, page)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, rows...)
		if len(result) >= total {
			return result
		}
	}
}

func triageRequireError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error=%v want=%v", got, want)
	}
}

func TestIssueTriageStatusNoOpPreservesResolutionAndHistory(t *testing.T) {
	ctx, db, user, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	before, err := db.UpdateIssue(ctx, user.ID, id, IssueUpdate{Status: "resolved"})
	if err != nil {
		t.Fatal(err)
	}
	activity := triageActivities(t, ctx, db, user.ID, id)
	after, err := db.UpdateIssue(ctx, user.ID, id, IssueUpdate{Status: "resolved"})
	if err != nil {
		t.Fatal(err)
	}
	if before.ResolvedAt == nil || before.ResolvedAt.Location() != time.UTC || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(activity, triageActivities(t, ctx, db, user.ID, id)) {
		t.Fatalf("same status changed resolution/history: before=%+v after=%+v", before, after)
	}
}

func TestIssueTriageAtomicAssignmentCurrentTargetsAndTenantScope(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	member := triageMember(t, ctx, db, org, "actor", "member")
	target := triageMember(t, ctx, db, org, "target", "member")
	foreignOrg := triageForeignOrganization(t, ctx, db, "foreign")
	var err error
	foreign := triageMember(t, ctx, db, foreignOrg, "foreign-target", "member")
	var team, foreignTeam string
	if err = db.pool.QueryRow(ctx, `INSERT INTO teams(organization_id,slug,name) VALUES($1::bigint,'triage','Original team') RETURNING id::text`, org.ID).Scan(&team); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx, `INSERT INTO teams(organization_id,slug,name) VALUES($1::bigint,'foreign','Foreign team') RETURNING id::text`, foreignOrg.ID).Scan(&foreignTeam); err != nil {
		t.Fatal(err)
	}
	id := triageCapture(t, ctx, db, project)
	combined := IssueUpdate{Status: "resolved", Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: target.ID}}}
	got, err := db.UpdateIssue(ctx, member.ID, id, combined)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "resolved" || got.ResolvedAt == nil || got.AssignedTo == nil || got.AssignedTo.ID != target.ID || got.AssignedTo.Name != target.Name {
		t.Fatalf("ordinary member triage=%+v", got)
	}
	history := triageActivities(t, ctx, db, owner.ID, id)
	if len(history) != 3 {
		t.Fatalf("combined update history=%+v", history)
	}
	kinds := map[string]int{}
	for _, a := range history {
		kinds[a.Type]++
		if a.Type != "first_seen" && (a.User == nil || a.User.ID != member.ID) {
			t.Fatalf("actor=%+v", a)
		}
	}
	if kinds["assigned"] != 1 || kinds["resolved"] != 1 || kinds["first_seen"] != 1 {
		t.Fatalf("combined activity=%v", kinds)
	}
	same, err := db.UpdateIssue(ctx, member.ID, id, combined)
	if err != nil || !reflect.DeepEqual(got, same) || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatalf("combined no-op=%+v err=%v", same, err)
	}
	for _, ref := range []IssueActorRef{{Type: "user", ID: foreign.ID}, {Type: "team", ID: foreignTeam}, {Type: "user", ID: "999999"}} {
		_, err = db.UpdateIssue(ctx, member.ID, id, IssueUpdate{Status: "ignored", Assignment: &IssueAssignmentChange{Target: &ref}})
		triageRequireError(t, err, ErrNotFound)
		if !reflect.DeepEqual(got, triageIssue(t, ctx, db, owner.ID, id)) || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
			t.Fatal("invalid target partially changed status, assignment or history")
		}
	}
	for _, change := range []IssueUpdate{{}, {Status: "future"}, {Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: "01"}}}} {
		_, err = db.UpdateIssue(ctx, member.ID, id, change)
		triageRequireError(t, err, ErrInvalidIssueChange)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE users SET name='Renamed target',email='renamed@example.test' WHERE id=$1::bigint`, target.ID); err != nil {
		t.Fatal(err)
	}
	current := triageIssue(t, ctx, db, owner.ID, id)
	if current.AssignedTo.Name != "Renamed target" || current.AssignedTo.Email != "renamed@example.test" {
		t.Fatalf("assignment not current=%+v", current.AssignedTo)
	}
	for _, a := range triageActivities(t, ctx, db, owner.ID, id) {
		if a.Type == "assigned" {
			var change struct {
				To IssueActor `json:"to"`
			}
			if err = json.Unmarshal(a.Data, &change); err != nil {
				t.Fatal(err)
			}
			if change.To.Name != target.Name || change.To.Email != target.Email {
				t.Fatalf("historical target changed=%s", a.Data)
			}
		}
	}
	if _, err = db.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`, org.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if triageIssue(t, ctx, db, owner.ID, id).AssignedTo != nil {
		t.Fatal("member removal retained assignment")
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO memberships VALUES($1::bigint,$2::bigint,'member')`, org.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if triageIssue(t, ctx, db, owner.ID, id).AssignedTo != nil {
		t.Fatal("reenrollment resurrected assignment")
	}
	assigned, err := db.UpdateIssue(ctx, member.ID, id, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "team", ID: team}}})
	if err != nil {
		t.Fatal(err)
	}
	if assigned.AssignedTo == nil || assigned.AssignedTo.Type != "team" || assigned.AssignedTo.Slug != "triage" {
		t.Fatalf("team=%+v", assigned.AssignedTo)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE teams SET name='Renamed team',slug='renamed-team' WHERE id=$1::bigint`, team); err != nil {
		t.Fatal(err)
	}
	if triageIssue(t, ctx, db, owner.ID, id).AssignedTo.Name != "Renamed team" {
		t.Fatal("team metadata stale")
	}
	cleared, err := db.UpdateIssue(ctx, member.ID, id, IssueUpdate{Assignment: &IssueAssignmentChange{}})
	if err != nil || cleared.AssignedTo != nil || cleared.Status != "resolved" || !cleared.ResolvedAt.Equal(*got.ResolvedAt) {
		t.Fatalf("clear=%+v err=%v", cleared, err)
	}
	activities := triageActivities(t, ctx, db, owner.ID, id)
	if activities[0].Type != "unassigned" || !bytes.Contains(activities[0].Data, []byte(`"to": null`)) && !bytes.Contains(activities[0].Data, []byte(`"to":null`)) {
		t.Fatalf("clear activity=%+v", activities[0])
	}
	if _, err = db.UpdateIssue(ctx, member.ID, id, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "team", ID: team}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `DELETE FROM teams WHERE id=$1::bigint`, team); err != nil {
		t.Fatal(err)
	}
	if triageIssue(t, ctx, db, owner.ID, id).AssignedTo != nil {
		t.Fatal("team removal retained assignment")
	}
	_, err = db.UpdateIssue(ctx, foreign.ID, id, IssueUpdate{Status: "ignored"})
	triageRequireError(t, err, ErrNotFound)
	if _, err = db.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`, org.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	_, err = db.UpdateIssue(ctx, member.ID, id, IssueUpdate{Status: "ignored"})
	triageRequireError(t, err, ErrNotFound)
	_, _, err = db.IssueAssignees(ctx, member.ID, id, "", 1)
	triageRequireError(t, err, ErrNotFound)
	_, _, err = db.IssueComments(ctx, foreign.ID, id, 1)
	triageRequireError(t, err, ErrNotFound)
	_, _, err = db.IssueActivities(ctx, member.ID, id, 1)
	triageRequireError(t, err, ErrNotFound)
	if triageIssue(t, ctx, db, owner.ID, id).Status != "resolved" {
		t.Fatal("unauthorized write changed issue")
	}
}

func TestIssueTriageCandidatesAndHistoryPages(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	// Bootstrap contributes one team and owner; 24 more teams give 25+1.
	for i := range 24 {
		if _, err := db.pool.Exec(ctx, `INSERT INTO teams(organization_id,slug,name) VALUES($1::bigint,$2,$3)`, org.ID, fmt.Sprintf("team-%d", i), fmt.Sprintf("Team %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	first, total, err := db.IssueAssignees(ctx, owner.ID, id, "", 1)
	if err != nil || total != 26 || len(first) != 25 {
		t.Fatalf("candidate first=%d total=%d err=%v", len(first), total, err)
	}
	last, total, err := db.IssueAssignees(ctx, owner.ID, id, "", 2)
	if err != nil || total != 26 || len(last) != 1 || last[0].Type != "user" || last[0].ID != owner.ID {
		t.Fatalf("candidate tail=%+v total=%d err=%v", last, total, err)
	}
	prev := int64(0)
	for _, actor := range first {
		n, e := strconv.ParseInt(actor.ID, 10, 64)
		if e != nil || actor.Type != "team" || n <= prev {
			t.Fatalf("numeric candidate ordering=%+v", first)
		}
		prev = n
	}
	empty, total, err := db.IssueAssignees(ctx, owner.ID, id, "", 3)
	if err != nil || total != 26 || len(empty) != 0 {
		t.Fatalf("candidate past end=%+v/%d err=%v", empty, total, err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE teams SET name=$2,slug=$3 WHERE id=$1::bigint`, first[0].ID, `Literal_%\ Name`, `literal-slug`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`_%\`, "LITERAL-SLUG", "TELEMETRY@EXAMPLE.TEST"} {
		matches, n, e := db.IssueAssignees(ctx, owner.ID, id, query, 1)
		if e != nil || n != 1 || len(matches) != 1 {
			t.Fatalf("literal candidate query=%q rows=%+v total=%d err=%v", query, matches, n, e)
		}
	}
	// A team-membership alone never grants organization membership.
	foreignOrg := triageForeignOrganization(t, ctx, db, "elsewhere")
	outsider := triageMember(t, ctx, db, foreignOrg, "outsider", "member")
	if _, err = db.pool.Exec(ctx, `INSERT INTO team_memberships VALUES($1::bigint,$2::bigint)`, first[0].ID, outsider.ID); err != nil {
		t.Fatal(err)
	}
	matches, n, err := db.IssueAssignees(ctx, owner.ID, id, "outsider", 1)
	if err != nil || n != 0 || len(matches) != 0 {
		t.Fatalf("nonmember candidate=%+v/%d err=%v", matches, n, err)
	}
	var comments []Comment
	for i := range 26 {
		c, e := db.AddComment(ctx, owner.ID, id, fmt.Sprintf("Comment %d", i))
		if e != nil {
			t.Fatal(e)
		}
		comments = append(comments, c)
	}
	for page, want := range map[int]int{1: 25, 2: 1, 3: 0} {
		rows, n, e := db.IssueComments(ctx, owner.ID, id, page)
		if e != nil || n != 26 || len(rows) != want {
			t.Fatalf("comment page%d rows=%d total=%d err=%v", page, len(rows), n, e)
		}
		for i, c := range rows {
			if c.ID != comments[25-(page-1)*25-i].ID || !c.CanEdit {
				t.Fatalf("comment order/rights page%d=%+v", page, rows)
			}
		}
	}
	// Receipt dates may tie or move backwards; IDs still define history order.
	if _, err = db.pool.Exec(ctx, `UPDATE comments SET created_at='2030-01-01'::timestamptz WHERE id=$1::bigint`, comments[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE issue_activity SET created_at=CASE WHEN action='first_seen' THEN '2030-01-01'::timestamptz ELSE '2000-01-01'::timestamptz END WHERE issue_id=$1::bigint`, id); err != nil {
		t.Fatal(err)
	}
	var activities []IssueActivity
	for page, want := range map[int]int{1: 25, 2: 2, 3: 0} {
		rows, n, e := db.IssueActivities(ctx, owner.ID, id, page)
		if e != nil || n != 27 || len(rows) != want {
			t.Fatalf("activity page%d rows=%d total=%d err=%v", page, len(rows), n, e)
		}
	}
	activities = triageActivities(t, ctx, db, owner.ID, id)
	for i, a := range activities {
		if a.DateCreated.Location() != time.UTC {
			t.Fatalf("activity zone=%v", a.DateCreated)
		}
		if i > 0 {
			newer, _ := strconv.ParseInt(activities[i-1].ID, 10, 64)
			older, _ := strconv.ParseInt(a.ID, 10, 64)
			if newer <= older {
				t.Fatal("activity not numeric ID descending")
			}
		}
	}
	if activities[len(activities)-1].Type != "first_seen" || activities[len(activities)-1].User != nil {
		t.Fatalf("first seen=%+v", activities[len(activities)-1])
	}
	rows, _, err := db.IssueComments(ctx, owner.ID, id, 1)
	if err != nil || rows[0].ID != comments[25].ID {
		t.Fatalf("comment clock reordered history=%+v err=%v", rows, err)
	}
}

func TestIssueTriageCommentModerationNoOpAndContentDeletion(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	author := triageMember(t, ctx, db, org, "author", "member")
	peer := triageMember(t, ctx, db, org, "peer", "member")
	admin := triageMember(t, ctx, db, org, "admin", "admin")
	id := triageCapture(t, ctx, db, project)
	original, err := db.AddComment(ctx, author.ID, id, "  Private original body  \n")
	if err != nil {
		t.Fatal(err)
	}
	if original.Text != "Private original body" || !original.CanEdit || original.UpdatedAt != nil {
		t.Fatalf("added=%+v", original)
	}
	for _, actor := range []struct {
		user    User
		allowed bool
	}{{author, true}, {peer, false}, {admin, true}, {owner, true}} {
		rows, n, e := db.IssueComments(ctx, actor.user.ID, id, 1)
		if e != nil || n != 1 || len(rows) != 1 || rows[0].CanEdit != actor.allowed {
			t.Fatalf("rights %s=%+v total=%d err=%v", actor.user.Name, rows, n, e)
		}
	}
	history := triageActivities(t, ctx, db, owner.ID, id)
	same, err := db.EditComment(ctx, author.ID, id, original.ID, "\tPrivate original body \n")
	if err != nil || !reflect.DeepEqual(same, original) || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatalf("normalized no-op=%+v err=%v", same, err)
	}
	_, err = db.EditComment(ctx, peer.ID, id, original.ID, "Peer overwrite")
	triageRequireError(t, err, ErrForbidden)
	triageRequireError(t, db.DeleteComment(ctx, peer.ID, id, original.ID), ErrForbidden)
	if !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatal("denied moderation recorded activity")
	}
	edited, err := db.EditComment(ctx, admin.ID, id, original.ID, "Private changed body")
	if err != nil {
		t.Fatal(err)
	}
	if edited.Text != "Private changed body" || edited.UpdatedAt == nil || edited.UpdatedAt.Location() != time.UTC || !edited.DateCreated.Equal(original.DateCreated) {
		t.Fatalf("edited=%+v", edited)
	}
	history = triageActivities(t, ctx, db, owner.ID, id)
	if history[0].Type != "comment_edited" || history[0].User == nil || history[0].User.ID != admin.ID || !history[0].DateCreated.Equal(*edited.UpdatedAt) {
		t.Fatalf("edit activity=%+v", history[0])
	}
	if _, err = db.pool.Exec(ctx, `UPDATE memberships SET role='member' WHERE organization_id=$1::bigint AND user_id=$2::bigint`, org.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	triageRequireError(t, db.DeleteComment(ctx, admin.ID, id, original.ID), ErrForbidden)
	secondProject, err := db.CreateProject(ctx, owner.ID, org.Slug, "Other issue", "go")
	if err != nil {
		t.Fatal(err)
	}
	other := triageCapture(t, ctx, db, secondProject)
	_, err = db.EditComment(ctx, owner.ID, other, original.ID, "Wrong issue")
	triageRequireError(t, err, ErrNotFound)
	triageRequireError(t, db.DeleteComment(ctx, owner.ID, id, "999999"), ErrNotFound)
	for _, text := range []string{"  ", "nul\x00body", string([]byte{0xff}), strings.Repeat("é", 5001)} {
		_, err = db.EditComment(ctx, author.ID, id, original.ID, text)
		triageRequireError(t, err, ErrInvalidIssueChange)
	}
	triageRequireError(t, db.DeleteComment(ctx, owner.ID, id, original.ID), nil)
	rows, n, err := db.IssueComments(ctx, author.ID, id, 1)
	if err != nil || len(rows) != 0 || n != 0 {
		t.Fatalf("deleted content=%+v total=%d err=%v", rows, n, err)
	}
	var stored int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM comments WHERE id=$1::bigint`, original.ID).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("comment body retained rows=%d err=%v", stored, err)
	}
	history = triageActivities(t, ctx, db, owner.ID, id)
	if len(history) != 4 || history[0].Type != "comment_deleted" {
		t.Fatalf("comment lifecycle=%+v", history)
	}
	for _, a := range history {
		if strings.HasPrefix(a.Type, "comment_") {
			var data map[string]string
			if err = json.Unmarshal(a.Data, &data); err != nil || len(data) != 1 || data["commentID"] != original.ID {
				t.Fatalf("content leaked into activity=%s err=%v", a.Data, err)
			}
		}
	}
	// A deleted member's historical authored activities remain, with current
	// organization membership required for any further operation.
	if _, err = db.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint`, org.ID, author.ID); err != nil {
		t.Fatal(err)
	}
	_, err = db.AddComment(ctx, author.ID, id, "After removal")
	triageRequireError(t, err, ErrNotFound)
	_, err = db.EditComment(ctx, author.ID, id, original.ID, "After removal")
	triageRequireError(t, err, ErrNotFound)
}

func TestIssueTriageReceiptLateRegressionDuplicateAndReopen(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	held, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	var holder int
	if err = held.QueryRow(ctx, `SELECT pg_backend_pid() FROM issues WHERE id=$1::bigint FOR UPDATE`, id).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	type updateResult struct {
		i   Issue
		err error
	}
	result := make(chan updateResult, 1)
	go func() {
		i, e := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "resolved"})
		result <- updateResult{i, e}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err = db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, holder).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resolve never waited for the real issue lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var beforeUnlock time.Time
	if err = held.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&beforeUnlock); err != nil {
		t.Fatal(err)
	}
	if err = held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	resolved := <-result
	if resolved.err != nil || resolved.i.ResolvedAt == nil || resolved.i.ResolvedAt.Before(beforeUnlock) {
		t.Fatalf("resolution receipt precedes acquired lock: issue=%+v beforeUnlock=%v err=%v", resolved.i, beforeUnlock, resolved.err)
	}
	priorResolved := *resolved.i.ResolvedAt
	late := triageEvent(t, "00000000000000000000000000000002", time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC))
	concurrentCapture := func(ev event.Event) int {
		t.Helper()
		var group sync.WaitGroup
		results := make(chan struct {
			created bool
			err     error
		}, 8)
		for range 8 {
			group.Go(func() {
				got, created, e := db.Ingest(ctx, project.ID, ev)
				if e == nil && got != id {
					e = fmt.Errorf("grouped into %q want %q", got, id)
				}
				results <- struct {
					created bool
					err     error
				}{created, e}
			})
		}
		group.Wait()
		close(results)
		fresh := 0
		for r := range results {
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.created {
				fresh++
			}
		}
		return fresh
	}
	if fresh := concurrentCapture(late); fresh != 1 {
		t.Fatalf("late unique accepted=%d", fresh)
	}
	regressed := triageIssue(t, ctx, db, owner.ID, id)
	if regressed.Status != "unresolved" || regressed.Count != 2 || regressed.ResolvedAt != nil || regressed.RegressedAt == nil || !regressed.FirstSeen.Equal(late.Timestamp) {
		t.Fatalf("late regression=%+v", regressed)
	}
	history := triageActivities(t, ctx, db, owner.ID, id)
	if len(history) != 3 || history[0].Type != "regressed" || history[0].User != nil || !history[0].DateCreated.Equal(*regressed.RegressedAt) {
		t.Fatalf("regression activity=%+v", history)
	}
	var data struct {
		EventID    string    `json:"eventID"`
		ResolvedAt time.Time `json:"resolvedAt"`
	}
	if err = json.Unmarshal(history[0].Data, &data); err != nil || data.EventID != late.ID || !data.ResolvedAt.Equal(priorResolved) {
		t.Fatalf("regression data=%s err=%v", history[0].Data, err)
	}
	if _, offset := data.ResolvedAt.Zone(); offset != 0 {
		t.Fatalf("regression resolvedAt must be UTC: %s", history[0].Data)
	}
	noOp, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "unresolved"})
	if err != nil || !reflect.DeepEqual(noOp, regressed) {
		t.Fatalf("unresolved no-op clears regression=%+v err=%v", noOp, err)
	}
	again, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "resolved"})
	if err != nil || again.RegressedAt != nil {
		t.Fatalf("manual transition failed to clear regression=%+v err=%v", again, err)
	}
	history = triageActivities(t, ctx, db, owner.ID, id)
	if fresh := concurrentCapture(late); fresh != 0 {
		t.Fatalf("retry accepted=%d", fresh)
	}
	if !reflect.DeepEqual(again, triageIssue(t, ctx, db, owner.ID, id)) || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatal("duplicate retry reopened or changed history/count")
	}
	ignored, err := db.UpdateIssue(ctx, owner.ID, id, IssueUpdate{Status: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	history = triageActivities(t, ctx, db, owner.ID, id)
	future := triageEvent(t, "00000000000000000000000000000003", time.Now().Add(24*time.Hour))
	if _, fresh, e := db.Ingest(ctx, project.ID, future); e != nil || !fresh {
		t.Fatalf("ignored capture fresh=%v err=%v", fresh, e)
	}
	current := triageIssue(t, ctx, db, owner.ID, id)
	if ignored.Status != "ignored" || current.Status != "ignored" || current.Count != 3 || current.RegressedAt != nil || !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatalf("ignored state=%+v", current)
	}
	transaction, err := event.Normalize(json.RawMessage(`{"type":"transaction","event_id":"00000000000000000000000000000004","transaction":"triage transaction","start_timestamp":1700000000,"timestamp":1700000001,"contexts":{"trace":{"trace_id":"11111111111111111111111111111111","span_id":"2222222222222222","op":"test"}}}`), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	transactionID, fresh, err := db.Ingest(ctx, project.ID, transaction)
	if err != nil || !fresh || transactionID != "" {
		t.Fatalf("transaction=%q fresh=%v err=%v", transactionID, fresh, err)
	}
	if !reflect.DeepEqual(history, triageActivities(t, ctx, db, owner.ID, id)) {
		t.Fatal("transaction added issue activity")
	}
	var rawBefore, rawAfter []byte
	if err = db.pool.QueryRow(ctx, `SELECT data::text FROM events WHERE project_id=$1::bigint AND event_id=$2`, project.ID, late.ID).Scan(&rawBefore); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if !reflect.DeepEqual(current, triageIssue(t, ctx, reopened, owner.ID, id)) || !reflect.DeepEqual(history, triageActivities(t, ctx, reopened, owner.ID, id)) {
		t.Fatal("reopen changed durable triage state/history")
	}
	if err = reopened.pool.QueryRow(ctx, `SELECT data::text FROM events WHERE project_id=$1::bigint AND event_id=$2`, project.ID, late.ID).Scan(&rawAfter); err != nil || !bytes.Equal(rawBefore, rawAfter) {
		t.Fatalf("reopen rewrote original event err=%v", err)
	}
}

func TestIssueTriageCommentPagesDuringRealModeration(t *testing.T) {
	ctx, db, owner, _, project := telemetryStore(t)
	id := triageCapture(t, ctx, db, project)
	for i := range 25 {
		if _, err := db.AddComment(ctx, owner.ID, id, fmt.Sprintf("Permanent %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	start := make(chan struct{})
	go func() {
		<-start
		for range 100 {
			c, err := db.AddComment(ctx, owner.ID, id, "Temporary moderation fixture")
			if err != nil {
				done <- err
				return
			}
			if err = db.DeleteComment(ctx, owner.ID, id, c.ID); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	close(start)
	for range 160 {
		rows, total, err := db.IssueComments(ctx, owner.ID, id, 2)
		if err != nil || total < 25 || total > 26 || len(rows) != total-25 {
			t.Fatalf("torn moderation page: len=%d total=%d err=%v", len(rows), total, err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rows, total, err := db.IssueComments(ctx, owner.ID, id, 2)
	if err != nil || total != 25 || len(rows) != 0 {
		t.Fatalf("final moderation page len=%d total=%d err=%v", len(rows), total, err)
	}
}

func TestIssueTriageMigrationRetainsLegacyAssignmentsAndHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	schema := fmt.Sprintf("goodsentry_triage_history_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	prior, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer prior.Close(ctx)
	tx, err := prior.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `CREATE TABLE schema_migrations(version integer PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		prefix, _, _ := strings.Cut(file.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatal(err)
		}
		if version >= 12 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)`, version, fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = prior.Exec(ctx, `INSERT INTO users(id,email,name,password_hash) VALUES(1,'owner@example.test','Owner','fixture'),(2,'member@example.test','Member','fixture'),(3,'foreign@example.test','Foreign','fixture');
 INSERT INTO organizations(id,slug,name) VALUES(1,'history','History'),(2,'foreign','Foreign');
 INSERT INTO memberships VALUES(1,1,'owner'),(1,2,'member'),(2,3,'owner');
 INSERT INTO projects(id,organization_id,slug,name,platform) VALUES(1,1,'history','History','go');
 INSERT INTO issues(id,project_id,fingerprint,title,level,platform,status,count,first_seen,last_seen,resolved_at,assignee_id) VALUES
 (1,1,'valid','Valid','error','go','resolved',1,'2019-01-01Z','2019-01-01Z','2020-01-01Z',2),
 (2,1,'foreign','Foreign target','error','go','unresolved',1,'2019-01-01Z','2019-01-01Z',NULL,3),
 (3,1,'empty','No target','error','go','ignored',1,'2019-01-01Z','2019-01-01Z',NULL,NULL);
 INSERT INTO comments(id,issue_id,user_id,text,created_at) VALUES(1,1,1,'Retained historic content','2020-01-01Z');
 INSERT INTO issue_activity(id,issue_id,user_id,action,data,created_at) VALUES(1,1,NULL,'resolved','{"from":"unresolved","to":"resolved"}','2020-01-01Z')`); err != nil {
		t.Fatal(err)
	}
	const snapshot = `SELECT json_build_object(
 'issues',(SELECT json_agg(json_build_array(id,assignee_id,status,resolved_at,first_seen,last_seen) ORDER BY id) FROM issues),
 'comments',(SELECT json_agg(json_build_array(id,text,created_at) ORDER BY id) FROM comments),
 'activity',(SELECT json_agg(row_to_json(a) ORDER BY id) FROM issue_activity a),
 'migrations',(SELECT json_agg(row_to_json(m) ORDER BY version) FROM schema_migrations m WHERE version<12))::text`
	var before, after string
	if err = prior.QueryRow(ctx, snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = prior.Close(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.pool.QueryRow(ctx, snapshot).Scan(&after); err != nil || before != after {
		t.Fatalf("migration changed legacy values/history: before=%s after=%s err=%v", before, after, err)
	}
	valid := triageIssue(t, ctx, db, "1", "1")
	if valid.AssignedTo == nil || valid.AssignedTo.ID != "2" || valid.AssignedTo.Name != "Member" || valid.RegressedAt != nil || valid.ResolvedAt == nil || valid.ResolvedAt.Location() != time.UTC {
		t.Fatalf("valid migration=%+v", valid)
	}
	for _, id := range []string{"2", "3"} {
		if triageIssue(t, ctx, db, "1", id).AssignedTo != nil {
			t.Fatalf("invalid/absent legacy target became active: %s", id)
		}
	}
	comments, total, err := db.IssueComments(ctx, "1", "1", 1)
	if err != nil || total != 1 || len(comments) != 1 || comments[0].UpdatedAt != nil || !comments[0].DateCreated.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("legacy comments=%+v total=%d err=%v", comments, total, err)
	}
	history := triageActivities(t, ctx, db, "1", "1")
	if len(history) != 1 || history[0].Type != "resolved" || history[0].User != nil {
		t.Fatalf("migration fabricated historical actor/activity=%+v", history)
	}
	if _, err = db.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=1 AND user_id=2;INSERT INTO memberships VALUES(1,2,'member')`); err != nil {
		t.Fatal(err)
	}
	if triageIssue(t, ctx, db, "1", "1").AssignedTo != nil {
		t.Fatal("legacy column resurrected migrated assignment after reenrollment")
	}
	var legacy string
	if err = db.pool.QueryRow(ctx, `SELECT assignee_id::text FROM issues WHERE id=1`).Scan(&legacy); err != nil || legacy != "2" {
		t.Fatalf("legacy value lost=%s err=%v", legacy, err)
	}
	var applied time.Time
	if err = db.pool.QueryRow(ctx, `SELECT applied_at FROM schema_migrations WHERE version=12`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	var reopenedApplied time.Time
	if err = reopened.pool.QueryRow(ctx, `SELECT applied_at FROM schema_migrations WHERE version=12`).Scan(&reopenedApplied); err != nil || !applied.Equal(reopenedApplied) {
		t.Fatalf("migration reapplied err=%v", err)
	}
	if triageIssue(t, ctx, reopened, "1", "1").AssignedTo != nil || !reflect.DeepEqual(history, triageActivities(t, ctx, reopened, "1", "1")) {
		t.Fatal("reopen changed legacy assignment/history")
	}
}
