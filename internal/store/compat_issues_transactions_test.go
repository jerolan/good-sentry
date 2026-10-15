//go:build integration

package store

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func compatibleChartTotal(stats map[string][][2]int64) int64 {
	var total int64
	for _, pair := range stats["24h"] {
		total += pair[1]
	}
	return total
}

func TestCompatibleIssuesAndNativeAnalyticsSnapshotDuringFreshCaptures(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	var clock time.Time
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&clock); err != nil {
		t.Fatal(err)
	}
	base := clock.Add(-time.Hour)
	const captureCount = 54 // Analytics must include history beyond the native raw page of50.
	events := make([]event.Event, captureCount)
	for n := range events {
		raw, err := json.Marshal(map[string]any{
			"event_id": fmt.Sprintf("%032x", n+1), "timestamp": base.Add(time.Duration(n+1) * time.Second).Format(time.RFC3339Nano),
			"message": "Analytics snapshot", "fingerprint": []string{"analytics-snapshot"},
			"user": map[string]string{"id": fmt.Sprint(n + 1)}, "extra": map[string]int{"ordinal": n + 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		events[n], err = event.Normalize(raw, "", base)
		if err != nil {
			t.Fatal(err)
		}
	}
	root, fresh, err := db.Ingest(ctx, project.ID, events[0])
	if err != nil || !fresh {
		t.Fatalf("initial capture=%s fresh=%v err=%v", root, fresh, err)
	}
	writer, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	checkNative := func() (int64, error) {
		detail, err := db.IssueDetail(ctx, owner.ID, root)
		if err != nil {
			return 0, err
		}
		count := detail.Issue.Count
		if count < 1 || count > captureCount || detail.Analytics.UserCount != count || compatibleChartTotal(detail.Analytics.Stats) != count || len(detail.Analytics.Stats["24h"]) != 24 || int64(len(detail.Events)) != min(count, 50) {
			return count, fmt.Errorf("native summary/page/users/chart disagree: count=%d users=%d events=%d stats=%v", count, detail.Analytics.UserCount, len(detail.Events), detail.Analytics.Stats)
		}
		var raw struct {
			EventID string                `json:"event_id"`
			Extra   struct{ Ordinal int } `json:"extra"`
		}
		if err := json.Unmarshal(detail.Event, &raw); err != nil {
			return count, err
		}
		if raw.EventID != events[count-1].ID || int64(raw.Extra.Ordinal) != count || !detail.Issue.LastSeen.Equal(events[count-1].Timestamp) || detail.Events[0].ID != raw.EventID || detail.Events[len(detail.Events)-1].ID != events[count-int64(len(detail.Events))].ID {
			return count, fmt.Errorf("native raw/issue snapshot differs from analytics: count=%d raw=%+v issue=%+v", count, raw, detail.Issue)
		}
		return count, nil
	}
	checkCompatible := func() (int64, error) {
		detail, err := db.CompatibleIssue(ctx, access, root, CompatibleIssueDetailFilter{StatsPeriod: "24h"})
		if err != nil {
			return 0, err
		}
		count := detail.Issue.Count
		if count < 1 || count > captureCount || detail.UserCount != count || compatibleChartTotal(detail.Stats) != count || len(detail.Stats["24h"]) != 24 || detail.Latest == nil || detail.Latest.ID != events[count-1].ID || !detail.Issue.LastSeen.Equal(events[count-1].Timestamp) {
			return count, fmt.Errorf("compatible summary/representative/users/chart disagree: %+v", detail)
		}
		return count, nil
	}
	if count, err := checkNative(); err != nil || count != 1 {
		t.Fatalf("initial native count=%d err=%v", count, err)
	}
	type capture struct {
		value event.Event
		start <-chan struct{}
	}
	commands := make(chan capture)
	written := make(chan error, 1)
	go func() {
		for next := range commands {
			<-next.start
			id, fresh, err := writer.Ingest(ctx, project.ID, next.value)
			if err == nil && (!fresh || id != root) {
				err = fmt.Errorf("fresh analytics capture issue=%s fresh=%v", id, fresh)
			}
			written <- err
		}
	}()
	defer close(commands)
	type observation struct {
		count int64
		err   error
	}
	beforeCommit, afterCommit := 0, 0
	for n := 1; n < captureCount; n++ {
		start := make(chan struct{})
		commands <- capture{events[n], start}
		observations := make(chan observation, 8)
		for range 2 {
			go func() {
				<-start
				for range 2 {
					count, err := checkNative()
					observations <- observation{count, err}
					count, err = checkCompatible()
					observations <- observation{count, err}
				}
			}()
		}
		close(start)
		if err := <-written; err != nil {
			t.Fatal(err)
		}
		for range 8 {
			observation := <-observations
			if observation.err != nil || observation.count != int64(n) && observation.count != int64(n+1) {
				t.Fatalf("capture %d snapshot count=%d err=%v", n+1, observation.count, observation.err)
			}
			if observation.count == int64(n) {
				beforeCommit++
			} else {
				afterCommit++
			}
		}
	}
	if count, err := checkNative(); err != nil || count != captureCount {
		t.Fatalf("final native count=%d err=%v", count, err)
	}
	if count, err := checkCompatible(); err != nil || count != captureCount {
		t.Fatalf("final compatible count=%d err=%v", count, err)
	}
	t.Logf("coherent native/compatible analytics snapshots: before=%d after=%d", beforeCommit, afterCommit)
}

func TestCompatibleIssuesAnalyticsSnapshotDuringNativeMergeAndSplit(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	access := ManagementAccess{UserID: owner.ID, OrganizationID: org.ID, AllProjects: true}
	var clock time.Time
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&clock); err != nil {
		t.Fatal(err)
	}
	base := clock.Add(-time.Hour)
	a, root := compatibleCapture(t, ctx, db, project, 1, base, map[string]any{"user": map[string]string{"id": "a"}, "exception": map[string]any{"values": []any{map[string]any{"type": "Unhandled", "mechanism": map[string]bool{"handled": false}}}}})
	b, source := compatibleCapture(t, ctx, db, project, 2, base.Add(time.Second), map[string]any{"user": map[string]string{"id": "b"}, "fingerprint": []string{"analytics-source"}})
	for _, id := range []string{root, source} {
		if _, err := db.AddComment(ctx, owner.ID, id, "Original partition history"); err != nil {
			t.Fatal(err)
		}
	}
	beforeRaw := groupRawSnapshot(t, ctx, db)
	writer, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	checkRecord := func(record CompatibleIssueRecord) error {
		count := int64(1)
		first, unhandled := b.Timestamp, false
		if record.Issue.ID == root {
			count, first, unhandled = 2, a.Timestamp, true
		} else if record.Issue.ID != source {
			return fmt.Errorf("unknown current root %s", record.Issue.ID)
		}
		if record.Issue.Count != count || record.UserCount != count || record.NumComments != count || record.IsUnhandled != unhandled || compatibleChartTotal(record.Stats) != count || record.Latest == nil || record.Latest.ID != b.ID || !record.Issue.FirstSeen.Equal(first) || !record.Issue.LastSeen.Equal(b.Timestamp) {
			return fmt.Errorf("issue topology and analytics differ: %+v", record)
		}
		return nil
	}
	check := func() error {
		record, err := db.CompatibleIssue(ctx, access, source, CompatibleIssueDetailFilter{StatsPeriod: "24h"})
		if err != nil {
			return err
		}
		if err := checkRecord(record); err != nil {
			return err
		}
		list, err := db.CompatibleIssues(ctx, access, "", CompatibleIssueFilter{IDs: []string{source}, Limit: 100, StatsPeriod: "24h"})
		if err != nil {
			return err
		}
		if len(list) != 1 {
			return fmt.Errorf("canonical ID list lost/duplicated issue: %+v", list)
		}
		if err := checkRecord(list[0]); err != nil {
			return err
		}
		stats, err := db.CompatibleIssueStats(ctx, access, []string{source}, "24h")
		if err != nil {
			return err
		}
		if len(stats) != 1 {
			return fmt.Errorf("canonical stats lost/duplicated issue: %+v", stats)
		}
		count, first, unhandled := int64(1), b.Timestamp, false
		if stats[0].ID == root {
			count, first, unhandled = 2, a.Timestamp, true
		} else if stats[0].ID != source {
			return fmt.Errorf("unknown stats root %s", stats[0].ID)
		}
		if stats[0].Count != count || stats[0].UserCount != count || stats[0].IsUnhandled != unhandled || compatibleChartTotal(stats[0].Stats) != count || !stats[0].FirstSeen.Equal(first) || !stats[0].LastSeen.Equal(b.Timestamp) {
			return fmt.Errorf("stats topology differs from analytics: %+v", stats)
		}
		native, err := db.IssueDetail(ctx, owner.ID, source)
		if err != nil {
			return err
		}
		count = 1
		if native.Issue.ID == root {
			count = 2
		} else if native.Issue.ID != source {
			return fmt.Errorf("unknown native root %s", native.Issue.ID)
		}
		if native.Issue.Count != count || native.Analytics.UserCount != count || native.CommentTotal != int(count) || int64(len(native.Events)) != count || native.Events[0].ID != b.ID || compatibleChartTotal(native.Analytics.Stats) != count {
			return fmt.Errorf("native topology differs from analytics: %+v", native)
		}
		return nil
	}
	if err := check(); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	failures := make(chan error, 3)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for range 40 {
			if _, err := writer.MergeIssues(ctx, owner.ID, org.Slug, []string{root, source}); err != nil {
				failures <- err
				return
			}
			if _, err := writer.UnmergeIssue(ctx, owner.ID, root, []string{source}); err != nil {
				failures <- err
				return
			}
		}
	})
	for range 2 {
		workers.Go(func() {
			<-start
			for range 50 {
				if err := check(); err != nil {
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
	if err := check(); err != nil {
		t.Fatal(err)
	}
	if after := groupRawSnapshot(t, ctx, db); after != beforeRaw {
		t.Fatal("read/group overlap changed raw payloads or durable receipt metadata")
	}
}
