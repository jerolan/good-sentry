package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidUserReport = errors.New("invalid user report")

type UserReportInput struct {
	EventID  string `json:"event_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Comments string `json:"comments"`
}

type UserReportEvent struct {
	EventID string  `json:"eventID"`
	ID      *string `json:"id"`
}

type UserReport struct {
	ID          string          `json:"id"`
	EventID     string          `json:"eventID"`
	Event       UserReportEvent `json:"event"`
	DateCreated time.Time       `json:"dateCreated"`
	Name        string          `json:"name"`
	Email       string          `json:"email"`
	Comments    string          `json:"comments"`
	User        *string         `json:"user"`
	Issue       *string         `json:"issue"`
}

func normalizeUserReport(input UserReportInput) (UserReportInput, error) {
	id, ok := compatibleEventReference(input.EventID, false)
	if !ok || strings.TrimSpace(input.Comments) == "" {
		return input, ErrInvalidUserReport
	}
	for _, field := range []struct {
		text  string
		limit int
	}{{input.Name, 128}, {input.Email, 254}, {input.Comments, 10000}} {
		if !utf8.ValidString(field.text) || strings.ContainsRune(field.text, 0) || utf8.RuneCountInString(field.text) > field.limit {
			return input, ErrInvalidUserReport
		}
	}
	input.EventID = id
	return input, nil
}

const userReportColumns = `id::text,event_id,created_at,name,email,comments`

func scanUserReport(row pgx.Row) (UserReport, error) {
	var report UserReport
	err := row.Scan(&report.ID, &report.EventID, &report.DateCreated, &report.Name, &report.Email, &report.Comments)
	report.Event.EventID = report.EventID
	report.DateCreated = report.DateCreated.UTC()
	return report, err
}

// SubmitUserReport is an SDK write. Its caller authorizes the project key;
// management callers additionally recheck membership/grants in their transaction.
func (s *Store) SubmitUserReport(ctx context.Context, projectID string, input UserReportInput) (UserReport, error) {
	input, err := normalizeUserReport(input)
	if err != nil || !canonicalIssueID(projectID) {
		return UserReport{}, ErrInvalidUserReport
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserReport{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, projectID+":"+input.EventID); err != nil {
		return UserReport{}, err
	}
	if err := lockProjectIssues(ctx, tx, projectID, false); err != nil {
		return UserReport{}, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1::bigint)`, projectID).Scan(&exists); err != nil {
		return UserReport{}, err
	}
	if !exists {
		return UserReport{}, ErrNotFound
	}
	report, err := saveUserReport(ctx, tx, projectID, input)
	if err != nil {
		return UserReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserReport{}, err
	}
	return report, nil
}

// The caller holds receipt then shared project gates. Reports are serialized
// with error insertion/deletion without adding a reverse event/issue lock order.
func saveUserReport(ctx context.Context, tx pgx.Tx, projectID string, input UserReportInput) (UserReport, error) {
	input, err := normalizeUserReport(input)
	if err != nil {
		return UserReport{}, err
	}
	var receipt *int64
	var kind string
	var received time.Time
	err = tx.QueryRow(ctx, `SELECT id,kind,created_at FROM events WHERE project_id=$1::bigint AND event_id=$2 FOR KEY SHARE`, projectID, input.EventID).Scan(&receipt, &kind, &received)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return UserReport{}, err
	}
	if receipt != nil && kind != "error" && kind != "message" {
		return UserReport{}, ErrInvalidUserReport
	}
	var first time.Time
	err = tx.QueryRow(ctx, `SELECT created_at FROM user_reports WHERE project_id=$1::bigint AND event_id=$2 FOR UPDATE`, projectID, input.EventID).Scan(&first)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return UserReport{}, err
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return UserReport{}, err
	}
	if (receipt != nil && now.After(received.Add(30*time.Minute))) || (!first.IsZero() && now.After(first.Add(5*time.Minute))) {
		return UserReport{}, ErrConflict
	}
	return scanUserReport(tx.QueryRow(ctx, `INSERT INTO user_reports(project_id,event_id,event_receipt_id,name,email,comments)
 VALUES($1::bigint,$2,$3,$4,$5,$6) ON CONFLICT(project_id,event_id) DO UPDATE
 SET name=EXCLUDED.name,email=EXCLUDED.email,comments=EXCLUDED.comments,updated_at=clock_timestamp()
 RETURNING `+userReportColumns, projectID, input.EventID, receipt, input.Name, input.Email, input.Comments))
}

// A pending report can join only this newly inserted numeric receipt. A report
// whose pending deadline passed is removed before same-envelope children run.
func bindPendingUserReport(ctx context.Context, tx pgx.Tx, projectID, eventID string, receipt int64) error {
	var first time.Time
	err := tx.QueryRow(ctx, `SELECT created_at FROM user_reports WHERE project_id=$1::bigint AND event_id=$2 AND event_receipt_id IS NULL FOR UPDATE`, projectID, eventID).Scan(&first)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if now.After(first.Add(30 * time.Minute)) {
		_, err = tx.Exec(ctx, `DELETE FROM user_reports WHERE project_id=$1::bigint AND event_id=$2 AND event_receipt_id IS NULL`, projectID, eventID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE user_reports SET event_receipt_id=$3 WHERE project_id=$1::bigint AND event_id=$2 AND event_receipt_id IS NULL`, projectID, eventID, receipt)
	}
	return err
}

// CleanupPendingUserReports bounds orphan work independently of error lifetime.
func (s *Store) CleanupPendingUserReports(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':pending-user-reports',0))`).Scan(&acquired); err != nil {
		return 0, err
	}
	if !acquired {
		return 0, nil
	}
	removed, err := tx.Exec(ctx, `DELETE FROM user_reports WHERE id IN(
 SELECT id FROM user_reports WHERE event_receipt_id IS NULL AND created_at<transaction_timestamp()-interval '30 minutes'
 ORDER BY created_at,id LIMIT 250 FOR UPDATE SKIP LOCKED)`)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return removed.RowsAffected(), nil
}
