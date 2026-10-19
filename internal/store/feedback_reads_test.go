//go:build integration

package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"goodsentry/internal/event"
)

// The real read boundary owns current membership/group scope, receipt paging
// and exact numeric-associated payloads. Capture tests cannot detect an ID-only
// lookup exposing a recreated error or a read performing association writes.
// Existing report readers have neither modern fields nor generation joins.
func TestFeedbackReadersScopePagingAndAssociatedGeneration(t *testing.T) {
	f := resolutionStore(t)
	const targetID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	targetIssue, _, err := f.db.Ingest(f.ctx, f.project.ID, modernErrorWithID(t, targetID))
	if err != nil {
		t.Fatal(err)
	}
	var roots []string
	for n := 1; n <= 27; n++ {
		associated := ""
		if n == 1 {
			associated = targetID
		} else if n == 2 {
			associated = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}
		ev := modernFeedbackEvent(t, fmt.Sprintf("%032x", n), "  Message\n<script>exact</script> ", associated)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(ev.Data, &fields); err != nil {
			t.Fatal(err)
		}
		// Deliberately reverse payload time. Paging is by server receipt.
		fields["timestamp"], _ = json.Marshal(time.Now().Add(-time.Duration(n) * time.Hour).UTC().Format(time.RFC3339Nano))
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		ev, err = event.Normalize(raw, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := f.db.Ingest(f.ctx, f.project.ID, ev)
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, id)
	}
	if _, err := f.db.MergeIssues(f.ctx, f.owner.ID, f.org.Slug, roots); err != nil {
		t.Fatal(err)
	}
	before := groupDatabaseSnapshot(t, f.ctx, f.db, 0)
	first, total, more, err := f.db.IssueFeedback(f.ctx, f.owner.ID, roots[26], 1)
	if err != nil || total != 27 || len(first) != 25 || !more || first[0].EventID != fmt.Sprintf("%032x", 27) || first[0].AssociationState != "absent" || first[0].AssociatedEventID != nil || first[0].AssociatedIssueID != nil {
		t.Fatal("first page/root alias", first, total, more, err)
	}
	last, total, more, err := f.db.IssueFeedback(f.ctx, f.owner.ID, roots[0], 2)
	if err != nil || total != 27 || len(last) != 2 || more || last[0].AssociationState != "pending" || last[1].AssociationState != "linked" || last[1].AssociatedIssueID == nil || *last[1].AssociatedIssueID != targetIssue {
		t.Fatal("last page/association states", last, total, more, err)
	}
	for _, item := range last {
		if item.Message != "  Message\n<script>exact</script> " || item.Name != "SDK reporter" || item.Email != "sdk@example.test" || item.URL != "https://example.test/source" || item.DateCreated.IsZero() || item.DateCreated.Location().String() != "UTC" {
			t.Fatal("lost feedback text/server receipt", item)
		}
	}
	detail, err := f.db.IssueDetail(f.ctx, f.owner.ID, roots[0])
	if err != nil || detail.FeedbackCount != 27 || len(detail.FeedbackItems) != 25 || detail.UserReportCount != 0 || len(detail.UserReports) != 0 || !reflect.DeepEqual(detail.FeedbackItems, first) {
		t.Fatal("native detail omitted real feedback page", detail, err)
	}
	linked, err := f.db.FeedbackAssociatedEvent(f.ctx, f.owner.ID, roots[26], fmt.Sprintf("%032x", 1))
	var stored json.RawMessage
	if err := f.db.pool.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, targetID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err != nil || linked.EventID != targetID || linked.IssueID != targetIssue || !bytes.Equal(linked.Event, stored) {
		t.Fatal("associated reader lost exact target", linked, err)
	}
	for _, id := range []string{fmt.Sprintf("%032x", 2), fmt.Sprintf("%032x", 3), "cccccccccccccccccccccccccccccccc"} {
		if _, err := f.db.FeedbackAssociatedEvent(f.ctx, f.owner.ID, roots[0], id); !errors.Is(err, ErrNotFound) {
			t.Fatal("absent/pending reference was readable", id, err)
		}
	}
	if _, _, _, err := f.db.IssueFeedback(f.ctx, f.owner.ID, targetIssue, 1); err != nil {
		t.Fatal("error issue should have an empty feedback list", err)
	}
	empty, count, hasMore, err := f.db.IssueFeedback(f.ctx, f.owner.ID, roots[0], 3)
	if err != nil || empty == nil || len(empty) != 0 || count != 27 || hasMore {
		t.Fatal("empty beyond last page", empty, count, hasMore, err)
	}
	groupRequireUnchanged(t, f.ctx, f.db, before)
	if _, err := f.db.UnmergeIssue(f.ctx, f.owner.ID, roots[0], []string{roots[26]}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.FeedbackAssociatedEvent(f.ctx, f.owner.ID, roots[26], fmt.Sprintf("%032x", 1)); !errors.Is(err, ErrNotFound) {
		t.Fatal("detached alias exposed another current owner", err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `DELETE FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, targetID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernErrorWithID(t, targetID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.FeedbackAssociatedEvent(f.ctx, f.owner.ID, roots[0], fmt.Sprintf("%032x", 1)); !errors.Is(err, ErrNotFound) {
		t.Fatal("reader substituted a recreated SDK ID", err)
	}
	page, _, _, err := f.db.IssueFeedback(f.ctx, f.owner.ID, roots[0], 2)
	if err != nil || len(page) != 1 || page[0].AssociationState != "unavailable" || page[0].AssociatedIssueID != nil {
		t.Fatal("deleted target did not become unavailable", page, err)
	}
	if _, _, _, err := f.db.IssueFeedback(f.ctx, f.owner.ID, roots[0], 0); !errors.Is(err, ErrInvalidEventFilter) {
		t.Fatal("invalid page", err)
	}
	if _, err := f.db.FeedbackAssociatedEvent(f.ctx, f.owner.ID, roots[0], "not-an-id"); !errors.Is(err, ErrInvalidEventFilter) {
		t.Fatal("invalid event reference", err)
	}
	if _, err := f.db.pool.Exec(f.ctx, `DELETE FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, f.owner.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.db.IssueFeedback(f.ctx, f.owner.ID, roots[0], 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("removed member read feedback", err)
	}
	if _, err := f.db.FeedbackAssociatedEvent(f.ctx, f.owner.ID, roots[0], fmt.Sprintf("%032x", 1)); !errors.Is(err, ErrNotFound) {
		t.Fatal("removed member read associated error", err)
	}
}

func TestFeedbackDetailAndAssociationUseAuthorizedSnapshot(t *testing.T) {
	for _, associated := range []bool{false, true} {
		t.Run(fmt.Sprint(associated), func(t *testing.T) {
			f := resolutionStore(t)
			const target = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			const feedback = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			if _, _, err := f.db.Ingest(f.ctx, f.project.ID, modernErrorWithID(t, target)); err != nil {
				t.Fatal(err)
			}
			root, _, err := f.db.Ingest(f.ctx, f.project.ID, modernFeedbackEvent(t, feedback, "Snapshot feedback", target))
			if err != nil {
				t.Fatal(err)
			}
			barrier, pid := captureItemsBarrier(t, f)
			table := "feedback_events"
			if associated {
				table = "events"
			}
			if _, err := barrier.Exec(f.ctx, `LOCK TABLE `+table+` IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if associated {
					got, err := f.db.FeedbackAssociatedEvent(f.ctx, f.owner.ID, root, feedback)
					if err == nil && got.EventID != target {
						err = fmt.Errorf("snapshot lost old numeric target: %+v", got)
					}
					done <- err
				} else {
					got, err := f.db.IssueDetail(f.ctx, f.owner.ID, root)
					if err == nil && (got.Issue.Count != 1 || got.FeedbackCount != 1 || len(got.FeedbackItems) != 1 || got.FeedbackItems[0].AssociationState != "linked") {
						err = fmt.Errorf("mixed detail snapshot: %+v", got)
					}
					done <- err
				}
			}()
			groupWaitBlocked(t, f.ctx, barrier, pid, 1)
			if _, err := barrier.Exec(f.ctx, `DELETE FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, target); err != nil {
				t.Fatal(err)
			}
			if err := barrier.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal("authorized reader lost its old receipt snapshot", err)
			}
		})
	}
}
