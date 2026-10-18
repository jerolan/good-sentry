package store

import (
	"context"
	"errors"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidFeedback = errors.New("invalid feedback")

// All receipt keys precede the project gate and any issue/event row locks.
// Sorting the actual signed keys also orders collisions between requests.
func lockCaptureReceipts(ctx context.Context, tx pgx.Tx, projectID string, ev event.Event) error {
	identities := []string{projectID + ":" + ev.ID}
	if ev.Feedback != nil && ev.Feedback.AssociatedEventID != "" {
		identities = append(identities, projectID+":"+ev.Feedback.AssociatedEventID)
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT hashtextextended(identity,0) AS key
 FROM unnest($1::text[]) identity ORDER BY key`, identities)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return err
		}
	}
	return nil
}

func saveFeedbackAssociation(ctx context.Context, tx pgx.Tx, projectID string, ev event.Event, receipt int64) error {
	var associatedID *string
	var target *int64
	pending := false
	if ev.Feedback.AssociatedEventID != "" {
		associatedID = &ev.Feedback.AssociatedEventID
		var kind string
		err := tx.QueryRow(ctx, `SELECT id,kind FROM events WHERE project_id=$1::bigint AND event_id=$2 FOR KEY SHARE`, projectID, *associatedID).Scan(&target, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			pending = true
		} else if err != nil {
			return err
		} else if kind != "error" && kind != "message" {
			target = nil
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO feedback_events(event_receipt_id,project_id,event_id,associated_event_id,associated_event_receipt_id,association_pending)
 VALUES($1,$2::bigint,$3,$4,$5,$6)`, receipt, projectID, ev.ID, associatedID, target, pending)
	return err
}

// Error capture already owns its receipt and project gates. It never waits on
// a feedback receipt gate or row; skipped/large cohorts have a real catch-up.
func bindFeedbackForError(ctx context.Context, tx pgx.Tx, projectID, eventID string, receipt int64) (int64, error) {
	result, err := tx.Exec(ctx, `WITH candidates AS (
 SELECT event_receipt_id FROM feedback_events WHERE project_id=$1::bigint AND associated_event_id=$2 AND association_pending
 ORDER BY event_receipt_id LIMIT 250 FOR UPDATE SKIP LOCKED)
 UPDATE feedback_events f SET associated_event_receipt_id=$3,association_pending=false FROM candidates c
 WHERE f.event_receipt_id=c.event_receipt_id AND f.association_pending`, projectID, eventID, receipt)
	return result.RowsAffected(), err
}

type feedbackAssociationPair struct{ feedback, project, target int64 }

// BindPendingFeedbackAssociations freezes exact numeric pairs before gate
// waits. A deleted target is skipped, never resolved to a replacement receipt.
func (s *Store) BindPendingFeedbackAssociations(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':feedback-associations',0))`).Scan(&acquired); err != nil {
		return 0, err
	}
	if !acquired {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT f.event_receipt_id,f.project_id,e.id FROM feedback_events f
 JOIN events e ON e.project_id=f.project_id AND e.event_id=f.associated_event_id AND e.kind IN ('error','message')
 WHERE f.association_pending ORDER BY f.event_receipt_id LIMIT 250`)
	if err != nil {
		return 0, err
	}
	pairs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (feedbackAssociationPair, error) {
		var pair feedbackAssociationPair
		err := row.Scan(&pair.feedback, &pair.project, &pair.target)
		return pair, err
	})
	if err != nil {
		return 0, err
	}
	projects := make([]int64, 0, len(pairs))
	for _, pair := range pairs {
		projects = append(projects, pair.project)
	}
	if err := lockFeedbackProjects(ctx, tx, projects); err != nil {
		return 0, err
	}
	var bound int64
	for _, pair := range pairs {
		var target int64
		err := tx.QueryRow(ctx, `SELECT id FROM events WHERE id=$1 AND project_id=$2 AND kind IN ('error','message') FOR KEY SHARE SKIP LOCKED`, pair.target, pair.project).Scan(&target)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		result, err := tx.Exec(ctx, `WITH available AS (
 SELECT f.event_receipt_id FROM feedback_events f JOIN events e ON e.project_id=f.project_id AND e.event_id=f.associated_event_id
 WHERE f.event_receipt_id=$1 AND f.project_id=$2 AND f.association_pending AND e.id=$3
 FOR UPDATE OF f SKIP LOCKED)
 UPDATE feedback_events f SET associated_event_receipt_id=$3,association_pending=false
 FROM available a WHERE f.event_receipt_id=a.event_receipt_id`, pair.feedback, pair.project, target)
		if err != nil {
			return 0, err
		}
		bound += result.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return bound, nil
}

func lockFeedbackProjects(ctx context.Context, tx pgx.Tx, projects []int64) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT hashtextextended(current_schema()||':issue-group:'||project_id::text,0) AS key
 FROM unnest($1::bigint[]) project_id ORDER BY key`, projects)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, key); err != nil {
			return err
		}
	}
	return nil
}
