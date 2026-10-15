//go:build integration

package store

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"goodsentry/internal/event"
)

func TestIssueDetailSnapshotDuringRealCaptures(t *testing.T) {
	ctx, db, user, _, project := telemetryStore(t)
	const captureCount = 40 // Below the detail's 50-event page limit.
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	events := make([]event.Event, captureCount)
	for index := range events {
		ordinal := index + 1
		stamp := base.Add(time.Duration(ordinal) * time.Second)
		raw, err := json.Marshal(map[string]any{
			"event_id": fmt.Sprintf("%032x", ordinal), "timestamp": stamp.Format(time.RFC3339Nano),
			"message": "Issue detail snapshot", "fingerprint": []string{"issue-detail-snapshot"},
			"extra": map[string]int{"ordinal": ordinal},
		})
		if err != nil {
			t.Fatal(err)
		}
		events[index], err = event.Normalize(raw, "", stamp)
		if err != nil {
			t.Fatal(err)
		}
	}
	issueID, fresh, err := db.Ingest(ctx, project.ID, events[0])
	if err != nil || !fresh || issueID == "" {
		t.Fatalf("initial capture issue=%q fresh=%v err=%v", issueID, fresh, err)
	}
	writer, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)

	checkSnapshot := func() (int64, error) {
		detail, err := db.IssueDetail(ctx, user.ID, issueID)
		if err != nil {
			return 0, err
		}
		count := detail.Issue.Count
		if detail.Issue.ID != issueID || count < 1 || count > captureCount || int64(len(detail.Events)) != count {
			return count, fmt.Errorf("incoherent issue/event snapshot: issue=%s count=%d events=%d", detail.Issue.ID, count, len(detail.Events))
		}
		for index, summary := range detail.Events {
			expected := events[int(count)-1-index]
			if summary.ID != expected.ID || !summary.DateCreated.Equal(expected.Timestamp) {
				return count, fmt.Errorf("event order from another capture: count=%d index=%d id=%s timestamp=%s", count, index, summary.ID, summary.DateCreated)
			}
		}
		var latest struct {
			EventID string                `json:"event_id"`
			Extra   struct{ Ordinal int } `json:"extra"`
		}
		if err := json.Unmarshal(detail.Event, &latest); err != nil {
			return count, fmt.Errorf("latest raw event: %w", err)
		}
		expected := events[int(count)-1]
		if latest.EventID != expected.ID || int64(latest.Extra.Ordinal) != count || !detail.Issue.LastSeen.Equal(expected.Timestamp) {
			return count, fmt.Errorf("latest raw/issue from another capture: count=%d rawID=%s rawOrdinal=%d lastSeen=%s", count, latest.EventID, latest.Extra.Ordinal, detail.Issue.LastSeen)
		}
		return count, nil
	}
	if count, err := checkSnapshot(); err != nil || count != 1 {
		t.Fatalf("initial snapshot count=%d err=%v", count, err)
	}

	type capture struct {
		event event.Event
		start <-chan struct{}
	}
	commands := make(chan capture)
	written := make(chan error, 1)
	go func() {
		for next := range commands {
			<-next.start
			id, fresh, err := writer.Ingest(ctx, project.ID, next.event)
			if err == nil && (!fresh || id != issueID) {
				err = fmt.Errorf("fresh capture issue=%s fresh=%v", id, fresh)
			}
			written <- err
		}
	}()
	defer close(commands)

	type observation struct {
		count int64
		err   error
	}
	const readerCount, snapshotsPerReader = 3, 4
	beforeCommit, afterCommit := 0, 0
	for index := 1; index < captureCount; index++ {
		// These channels coordinate public calls only. No query inside either
		// operation is paused, intercepted or supplied an expected snapshot.
		start := make(chan struct{})
		commands <- capture{event: events[index], start: start}
		observations := make(chan observation, readerCount*snapshotsPerReader)
		for range readerCount {
			go func() {
				<-start
				for range snapshotsPerReader {
					count, err := checkSnapshot()
					observations <- observation{count: count, err: err}
				}
			}()
		}
		close(start)
		writeErr := <-written
		var readErr error
		for range readerCount * snapshotsPerReader {
			got := <-observations
			if got.err != nil {
				readErr = got.err
			} else if got.count == int64(index) {
				beforeCommit++
			} else if got.count == int64(index+1) {
				afterCommit++
			} else {
				readErr = fmt.Errorf("unexpected round snapshot: capture=%d count=%d", index+1, got.count)
			}
		}
		if writeErr != nil || readErr != nil {
			t.Fatalf("concurrent capture%d write=%v read=%v", index+1, writeErr, readErr)
		}
	}
	if count, err := checkSnapshot(); err != nil || count != captureCount {
		t.Fatalf("final snapshot count=%d err=%v", count, err)
	}
	t.Logf("coherent concurrent snapshots before commit=%d after commit=%d", beforeCommit, afterCommit)
}
