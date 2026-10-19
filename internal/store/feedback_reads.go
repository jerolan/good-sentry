package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type FeedbackItem struct {
	EventID           string    `json:"eventID"`
	DateCreated       time.Time `json:"dateCreated"`
	Message           string    `json:"message"`
	Name              string    `json:"name"`
	Email             string    `json:"email"`
	URL               string    `json:"url"`
	AssociatedEventID *string   `json:"associatedEventID"`
	AssociationState  string    `json:"associationState"`
	AssociatedIssueID *string   `json:"associatedIssueID"`
}

type FeedbackAssociatedEvent struct {
	EventID string          `json:"eventID"`
	IssueID string          `json:"issueID"`
	Event   json.RawMessage `json:"event"`
}

// IssueFeedback uses current membership and canonical group ownership in the
// same read-only snapshot as receipt paging, counts and association state.
func (s *Store) IssueFeedback(ctx context.Context, userID, issueID string, page int) ([]FeedbackItem, int64, bool, error) {
	if page < 1 || page > 1000000 {
		return nil, 0, false, ErrInvalidEventFilter
	}
	tx, issue, _, _, err := s.issueTransaction(ctx, userID, issueID, false)
	if err != nil {
		return nil, 0, false, err
	}
	defer tx.Rollback(ctx)
	items, total, err := readFeedbackItems(ctx, tx, issue.ID, (page-1)*25)
	if err != nil {
		return nil, 0, false, err
	}
	more := len(items) > 25
	if more {
		items = items[:25]
	}
	return items, total, more, tx.Commit(ctx)
}

func readFeedbackItems(ctx context.Context, tx pgx.Tx, issueID string, offset int) ([]FeedbackItem, int64, error) {
	const from = ` FROM events e JOIN feedback_events f ON f.event_receipt_id=e.id
 LEFT JOIN events target ON target.id=f.associated_event_receipt_id AND target.project_id=e.project_id AND target.kind IN ('error','message')
 WHERE e.issue_id=$1::bigint AND e.kind='feedback'`
	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*)`+from, issueID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT e.event_id,e.created_at,
 COALESCE(e.data#>>'{contexts,feedback,message}',''),COALESCE(e.data#>>'{contexts,feedback,name}',''),
 COALESCE(e.data#>>'{contexts,feedback,contact_email}',''),COALESCE(e.data#>>'{contexts,feedback,url}',''),f.associated_event_id,
 CASE WHEN f.associated_event_id IS NULL THEN 'absent' WHEN target.id IS NOT NULL THEN 'linked'
 WHEN f.association_pending THEN 'pending' ELSE 'unavailable' END,target.issue_id::text`+from+
		` ORDER BY e.created_at DESC,e.id DESC LIMIT 26 OFFSET $2`, issueID, offset)
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FeedbackItem, error) {
		var item FeedbackItem
		err := row.Scan(&item.EventID, &item.DateCreated, &item.Message, &item.Name, &item.Email, &item.URL,
			&item.AssociatedEventID, &item.AssociationState, &item.AssociatedIssueID)
		item.DateCreated = item.DateCreated.UTC()
		return item, err
	})
	return items, total, err
}

// FeedbackAssociatedEvent joins the established numeric receipt. A target that
// disappeared cannot be replaced by a new event reusing its external SDK ID.
func (s *Store) FeedbackAssociatedEvent(ctx context.Context, userID, issueID, feedbackEventID string) (FeedbackAssociatedEvent, error) {
	var result FeedbackAssociatedEvent
	ref, valid := compatibleEventReference(feedbackEventID, false)
	if !valid {
		return result, ErrInvalidEventFilter
	}
	tx, issue, _, _, err := s.issueTransaction(ctx, userID, issueID, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT target.event_id,target.issue_id::text,target.data
 FROM events own JOIN feedback_events f ON f.event_receipt_id=own.id
 JOIN events target ON target.id=f.associated_event_receipt_id AND target.project_id=own.project_id
 JOIN issues current_issue ON current_issue.id=target.issue_id
 WHERE own.issue_id=$1::bigint AND own.event_id=$2 AND own.kind='feedback' AND target.kind IN ('error','message')`, issue.ID, ref).
		Scan(&result.EventID, &result.IssueID, &result.Event)
	if err != nil {
		return result, dbError(err)
	}
	return result, tx.Commit(ctx)
}
