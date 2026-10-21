//go:build integration

package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCompatibleEventLookupSnapshotDuringNativeMergeAndSplit(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour)
	_, root := compatibleCapture(t, f.ctx, f.db, f.project, 1, stamp, map[string]any{"message": "Root lookup"})
	sourceEvent, source := compatibleCapture(t, f.ctx, f.db, f.project, 2, stamp.Add(time.Second), map[string]any{"message": "Source lookup", "fingerprint": []string{"source-lookup"}})
	writer, err := Open(f.ctx, f.db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	check := func() (string, error) {
		result, err := f.db.CompatibleEventLookup(f.ctx, f.access, sourceEvent.ID, CompatibleEventLookupFilter{})
		if err != nil {
			return "", err
		}
		if result.Issue == nil || result.Event.ID != sourceEvent.ID || result.Event.GroupID != result.Issue.Issue.ID || result.Event.SiblingsKnown {
			return "", fmt.Errorf("incoherent event/parent association: event=%+v issue=%+v", result.Event, result.Issue)
		}
		issue := result.Issue.Issue
		if issue.ID == root && (issue.Count != 2) || issue.ID == source && (issue.Count != 1) || issue.ID != root && issue.ID != source {
			return "", fmt.Errorf("parent mixed another topology's state: %+v", issue)
		}
		return issue.ID, nil
	}
	if id, err := check(); err != nil || id != source {
		t.Fatalf("unmerged parent=%s err=%v", id, err)
	}
	if _, err := writer.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, []string{root, source}); err != nil {
		t.Fatal(err)
	}
	if id, err := check(); err != nil || id != root {
		t.Fatalf("merged parent=%s err=%v", id, err)
	}
	if _, err := writer.UnmergeIssue(f.ctx, f.owner.ID, root, []string{source}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	failures := make(chan error, 3)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for range 30 {
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
	for range 2 {
		workers.Go(func() {
			<-start
			for range 80 {
				if _, err := check(); err != nil {
					failures <- err
					return
				}
			}
		})
	}
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func TestCompatibleEventLookupUnassociatedAuthorityPrecedesAmbiguity(t *testing.T) {
	f := resolutionStore(t)
	stamp := time.Now().Add(-time.Hour)
	occurrence, issue := compatibleCapture(t, f.ctx, f.db, f.project, 1, stamp, nil)
	other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Unassociated lookup", "go")
	if err != nil {
		t.Fatal(err)
	}
	compatibleCapture(t, f.ctx, f.db, other, 1, stamp, map[string]any{"type": "transaction", "transaction": "Unassociated lookup", "start_timestamp": float64(stamp.Add(-time.Second).Unix())})
	result, err := f.db.CompatibleEventLookup(f.ctx, f.access, occurrence.ID, CompatibleEventLookupFilter{})
	if err != nil || result.Issue == nil || result.Issue.Issue.ID != issue {
		t.Fatalf("unassociated receipt affected issue-only lookup: %+v %v", result, err)
	}
	if _, err := f.db.CompatibleEventLookup(f.ctx, f.access, occurrence.ID, CompatibleEventLookupFilter{IncludeUnassociated: true}); !errors.Is(err, ErrAmbiguousEvent) {
		t.Fatalf("admitted duplicate did not require a project: %v", err)
	}
	if _, err := f.db.CompatibleEventLookup(f.ctx, f.access, occurrence.ID, CompatibleEventLookupFilter{Project: other.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("project selector rescued denied unassociated receipt: %v", err)
	}
	result, err = f.db.CompatibleEventLookup(f.ctx, f.access, occurrence.ID, CompatibleEventLookupFilter{Project: other.ID, IncludeUnassociated: true})
	if err != nil || result.Issue != nil || result.Event.Kind != "transaction" {
		t.Fatalf("explicit project-event authority failed: %+v %v", result, err)
	}
}
