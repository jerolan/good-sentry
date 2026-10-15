//go:build integration

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func TestCompatibleIssueUpdateWaitCancellationIsAtomicAndFresh(t *testing.T) {
	f := resolutionStore(t)
	id := triageCapture(t, f.ctx, f.db, f.project)
	writer, err := Open(f.ctx, f.db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	blocker, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(f.ctx)
	if _, err = blocker.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1 FOR UPDATE", id); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() {
		_, err := writer.UpdateCompatibleIssue(ctx, f.access, id, CompatibleIssueChange{Status: "resolved", Assignment: &CompatibleIssueAssignment{Reference: f.owner.ID}})
		done <- err
	}()
	groupWaitBlocked(t, f.ctx, blocker, int(blocker.Conn().PgConn().PID()), 1)
	cancel()
	groupAwaitCancellation(t, done, "compatible issue writer")
	if err = blocker.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	// A real lock barrier also waits for PostgreSQL's asynchronous cancellation
	// cleanup before comparing the transaction's complete persistent footprint.
	barrier, err := f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = barrier.Exec(f.ctx, "SELECT id FROM issues WHERE id=$1 FOR UPDATE", id); err != nil {
		t.Fatal(err)
	}
	if err = barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)

	// Queue a policy-only compatible request behind a preceding state commit.
	// Its effective status must come from the fresh post-wait issue.
	blocker, err = f.db.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(f.ctx)
	if _, err = blocker.Exec(f.ctx, "UPDATE issues SET status='ignored' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	count := int32(7)
	result := make(chan struct {
		record CompatibleIssueRecord
		err    error
	}, 1)
	go func() {
		record, err := writer.UpdateCompatibleIssue(f.ctx, f.access, id, CompatibleIssueChange{Policy: &CompatibleIssuePolicy{Kind: CompatibleIssueIgnorePolicy, Count: &count}})
		result <- struct {
			record CompatibleIssueRecord
			err    error
		}{record, err}
	}()
	groupWaitBlocked(t, f.ctx, blocker, int(blocker.Conn().PgConn().PID()), 1)
	if err = blocker.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil || got.record.Issue.Status != "ignored" || got.record.Issue.Ignore == nil || *got.record.Issue.Ignore.Remaining != 7 {
		t.Fatalf("policy translation used pre-wait state: %+v error=%v", got.record.Issue, got.err)
	}
}

func TestCompatibleIssueUpdateResponseDuringNativeCaptureAndGrouping(t *testing.T) {
	f := resolutionStore(t)
	var clock time.Time
	if err := f.db.pool.QueryRow(f.ctx, "SELECT clock_timestamp()").Scan(&clock); err != nil {
		t.Fatal(err)
	}
	base := clock.Add(-time.Hour)
	_, root := compatibleCapture(t, f.ctx, f.db, f.project, 1, base, map[string]any{"user": map[string]string{"id": "1"}})
	_, source := compatibleCapture(t, f.ctx, f.db, f.project, 2, base.Add(time.Second), map[string]any{"fingerprint": []string{"write-group-source"}, "user": map[string]string{"id": "2"}})
	member := triageMember(t, f.ctx, f.db, f.org, "native-concurrent", "member")
	writer, err := Open(f.ctx, f.db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	const freshCount = 24
	events := make([]event.Event, freshCount)
	for n := range events {
		raw, err := json.Marshal(map[string]any{
			"event_id": fmt.Sprintf("%032x", n+3), "message": "Concurrent compatible issue writes", "fingerprint": []string{"compatible-events"},
			"timestamp": base.Add(time.Duration(n+3) * time.Second).Format(time.RFC3339Nano), "user": map[string]string{"id": fmt.Sprint(n + 3)},
		})
		if err != nil {
			t.Fatal(err)
		}
		events[n], err = event.Normalize(raw, "", clock)
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	failures := make(chan error, 4)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for n := range 48 {
			status := "resolved"
			if n%2 != 0 {
				status = "unresolved"
			}
			record, err := f.db.UpdateCompatibleIssue(f.ctx, f.access, root, CompatibleIssueChange{Status: status, Assignment: &CompatibleIssueAssignment{Reference: f.owner.ID}})
			if err != nil {
				failures <- err
				return
			}
			if record.Issue.Status != status || record.Issue.AssignedTo == nil || record.Issue.AssignedTo.ID != f.owner.ID || record.Issue.ID != root || record.Issue.Count != record.UserCount || compatibleChartTotal(record.Stats) != record.Issue.Count || record.Latest == nil || record.Latest.GroupID != root || record.Issue.GroupSize < 1 || record.Issue.GroupSize > 2 {
				failures <- fmt.Errorf("compatible response escaped its writer transaction: %+v", record)
				return
			}
		}
	})
	workers.Go(func() {
		<-start
		for n := range 40 {
			status := "ignored"
			if n%2 != 0 {
				status = "unresolved"
			}
			issue, err := writer.UpdateIssue(f.ctx, member.ID, root, IssueUpdate{Status: status, Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: member.ID}}})
			if err != nil {
				failures <- err
				return
			}
			if issue.Status != status || issue.AssignedTo == nil || issue.AssignedTo.ID != member.ID {
				failures <- fmt.Errorf("native mutation lost shared lifecycle behavior: %+v", issue)
				return
			}
		}
	})
	workers.Go(func() {
		<-start
		for _, ev := range events {
			id, fresh, err := writer.Ingest(f.ctx, f.project.ID, ev)
			if err != nil || !fresh || id != root {
				failures <- fmt.Errorf("capture id=%s fresh=%v error=%v", id, fresh, err)
				return
			}
		}
	})
	workers.Go(func() {
		<-start
		for range 20 {
			if _, err := writer.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, source}); err != nil {
				failures <- err
				return
			}
			if _, err := writer.UnmergeIssue(f.ctx, f.owner.ID, root, []string{source}); err != nil {
				failures <- err
				return
			}
		}
	})
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	final := compatibleApply(t, f, root, CompatibleIssueChange{Status: "unresolved"})
	if final.Issue.Count != freshCount+1 || final.UserCount != freshCount+1 || compatibleChartTotal(final.Stats) != freshCount+1 || final.Issue.GroupSize != 1 {
		t.Fatalf("overlap lost acknowledged occurrences: %+v", final)
	}
	if native, err := f.db.IssueDetail(f.ctx, f.owner.ID, root); err != nil || !reflect.DeepEqual(native.Issue, final.Issue) || native.Analytics.UserCount != final.UserCount || !reflect.DeepEqual(native.Analytics.Stats, final.Stats) {
		t.Fatalf("native and compatible callers diverged after overlap: %+v error=%v", native, err)
	}
}

func TestCompatibleIssueUpdateMigration018PreservesRowsReceiptsAndReopen(t *testing.T) {
	f := resolutionStore(t)
	config := alertConfig("Before018")
	rule, err := f.db.CreateAlert(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, config)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour)
	_, root := compatibleCapture(t, f.ctx, f.db, f.project, 1, stamp, nil)
	_, source := compatibleCapture(t, f.ctx, f.db, f.project, 2, stamp.Add(time.Second), map[string]any{"fingerprint": []string{"migration018-source"}})
	if _, err = f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, source}); err != nil {
		t.Fatal(err)
	}
	f.resolve(t, root, "release", "app@1")
	if _, err = f.db.AddComment(f.ctx, f.owner.ID, root, "Established before writer scope"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.UpdateIssue(f.ctx, f.owner.ID, root, IssueUpdate{Assignment: &IssueAssignmentChange{Target: &IssueActorRef{Type: "user", ID: f.owner.ID}}}); err != nil {
		t.Fatal(err)
	}
	_, muted := compatibleCapture(t, f.ctx, f.db, f.project, 3, stamp, map[string]any{"fingerprint": []string{"migration018-muted"}})
	if _, err = f.db.UpdateIssue(f.ctx, f.owner.ID, muted, IssueUpdate{Status: "ignored", Ignore: issueIgnoreChange(60, 5)}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.CreateSession(f.ctx, f.owner.ID, "migration018-session", "migration018-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	restricted, _, _ := apiTokenFixture(t, f.ctx, f.db, f.owner.ID, f.org.Slug, "migration018-expired", []int64{apiTokenProjectID(t, f.project.ID)})
	revoked, _, _ := apiTokenFixture(t, f.ctx, f.db, f.owner.ID, f.org.Slug, "migration018-revoked", nil)
	if err = f.db.RevokeAPIToken(f.ctx, f.owner.ID, f.org.Slug, revoked.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.pool.Exec(f.ctx, "UPDATE api_tokens SET expires_at=created_at-interval '1 day' WHERE id=$1", restricted.ID); err != nil {
		t.Fatal(err)
	}
	if deliveries, _, err := f.db.AlertDeliveries(f.ctx, f.owner.ID, f.org.Slug, f.project.ID, rule.ID, 1); err != nil || len(deliveries) != 1 {
		t.Fatalf("migration outbox fixture=%+v error=%v", deliveries, err)
	}
	// Reconstruct only018's predecessor constraint and remove its own receipt.
	// Every other row and migration001..017 remains part of the comparison.
	if _, err = f.db.pool.Exec(f.ctx, `ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_scopes_check;
ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_scopes_check CHECK (
 cardinality(scopes)>0 AND scopes <@ ARRAY['org:read','project:read','project:releases','event:read']::text[]);
DELETE FROM schema_migrations WHERE version=18`); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 17)
	unchangedCatalog := func(db *Store) string {
		t.Helper()
		var catalog map[string]any
		if err := json.Unmarshal([]byte(groupCatalogSnapshot(t, f.ctx, db)), &catalog); err != nil {
			t.Fatal(err)
		}
		constraints := []any{}
		for _, value := range catalog["constraints"].([]any) {
			constraint := value.(map[string]any)
			if constraint["table"] != "api_tokens" || constraint["name"] != "api_tokens_scopes_check" {
				constraints = append(constraints, constraint)
			}
		}
		catalog["constraints"] = constraints
		raw, err := json.Marshal(catalog)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	catalog := unchangedCatalog(f.db)
	url := f.db.pool.Config().ConnString()
	f.db.Close()
	upgraded, err := Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	if after := groupDatabaseSnapshot(t, f.ctx, upgraded, 17); !reflect.DeepEqual(before, after) {
		t.Fatal("018 rewrote old rows or receipts001..017")
	}
	if after := unchangedCatalog(upgraded); after != catalog {
		t.Fatal("018 changed catalog outside token scope allowlist")
	}
	_, hash, prefix := apiTokenCredentials("migration018-writer")
	token, err := upgraded.CreateAPIToken(f.ctx, f.owner.ID, f.org.Slug, APITokenInput{Name: "Issue writer", Scopes: []string{"event:write"}, ProjectIDs: []int64{apiTokenProjectID(t, f.project.ID)}, ExpiresInDays: 1}, hash, prefix)
	if err != nil || !reflect.DeepEqual(token.Scopes, []string{"event:write"}) {
		t.Fatalf("018 writer scope=%+v error=%v", token, err)
	}
	tx, err := upgraded.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, rejected := tx.Exec(f.ctx, "UPDATE api_tokens SET scopes=ARRAY['event:admin']::text[] WHERE id=$1", token.ID)
	_ = tx.Rollback(f.ctx)
	if rejected == nil {
		t.Fatal("018 admitted unimplemented event:admin")
	}
	grant, err := upgraded.AuthenticateAPIToken(f.ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	access := ManagementAccess{UserID: grant.User.ID, OrganizationID: grant.OrganizationID, AllProjects: grant.AllProjects, ProjectIDs: grant.ProjectIDs}
	if got, err := upgraded.UpdateCompatibleIssue(f.ctx, access, root, CompatibleIssueChange{Status: "unresolved"}); err != nil || got.Issue.Status != "unresolved" || got.NumComments != 1 || got.Issue.Count != 2 {
		t.Fatalf("persisted writer grant failed actual mutation: %+v error=%v", got, err)
	}
	beforeRestart := groupDatabaseSnapshot(t, f.ctx, upgraded, 0)
	upgraded.Close()
	again, err := Open(f.ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	if after := groupDatabaseSnapshot(t, f.ctx, again, 0); !reflect.DeepEqual(beforeRestart, after) {
		t.Fatal("reopen reapplied018 or changed persisted writer/data/receipts")
	}
	if after, err := again.AuthenticateAPIToken(f.ctx, hash); err != nil || !reflect.DeepEqual(after, grant) {
		t.Fatalf("reopen lost writer grant: %+v error=%v", after, err)
	}
}
