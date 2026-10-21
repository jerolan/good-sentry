//go:build integration

package store

import (
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
		result, err := f.db.CompatibleEventLookup(f.ctx, f.access, "", sourceEvent.ID)
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
