package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// UserReports resolves current membership, project grants and merged aliases
// in the same snapshot as rows and totals. The extra row proves the next page.
func (s *Store) UserReports(ctx context.Context, access ManagementAccess, target CompatibleEventTarget, offset, limit int) ([]UserReport, int64, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000000 {
		return nil, 0, ErrInvalidEventFilter
	}
	tx, scope, err := s.compatibleEventTransaction(ctx, access, target)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	reports, total, err := readUserReports(ctx, tx, scope, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return reports, total, nil
}

func readUserReports(ctx context.Context, tx pgx.Tx, scope compatibleEventScope, offset, limit int) ([]UserReport, int64, error) {
	args := []any{scope.projectID}
	where := ` FROM user_reports report LEFT JOIN events e ON e.id=report.event_receipt_id WHERE report.project_id=$1`
	if scope.issueID != 0 {
		args = append(args, scope.issueID)
		where += ` AND e.issue_id=$2`
	}
	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit+1, offset)
	rows, err := tx.Query(ctx, `SELECT report.id::text,report.event_id,report.created_at,report.name,report.email,report.comments`+where+
		fmt.Sprintf(` ORDER BY report.created_at DESC,report.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	reports, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (UserReport, error) { return scanUserReport(row) })
	return reports, total, err
}
