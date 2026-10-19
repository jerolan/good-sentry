package store

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/event"
	"goodsentry/internal/eventview"
)

type CompatibleIssueHashFilter struct{ Offset, Limit int }
type CompatibleIssueHash struct {
	ID     string
	Latest *eventview.Record
}

// The project gate precedes this route lock and both issue-row locks. Reset
// holds the exclusive gate, so a committed capture stays on exactly one side.
func activeIssueHashOrigin(ctx context.Context, tx pgx.Tx, projectID string, ev event.Event) (string, *string, error) {
	category := "error"
	if ev.Type == "feedback" {
		category = "feedback"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO issue_hash_routes(project_id,fingerprint) VALUES($1::bigint,$2)
 ON CONFLICT(project_id,fingerprint) DO NOTHING`, projectID, ev.Fingerprint); err != nil {
		return "", nil, err
	}
	var own *string
	if err := tx.QueryRow(ctx, `SELECT origin_issue_id::text FROM issue_hash_routes
 WHERE project_id=$1::bigint AND fingerprint=$2 FOR UPDATE`, projectID, ev.Fingerprint).Scan(&own); err != nil {
		return "", nil, err
	}
	if own == nil {
		var created string
		if err := tx.QueryRow(ctx, `INSERT INTO issues(project_id,fingerprint,title,level,platform,first_seen,last_seen,environment,release,category)
 VALUES($1::bigint,$2,$3,$4,$5,$6,$6,$7,$8,$9) RETURNING id::text`, projectID, ev.Fingerprint, ev.Title, ev.Level, ev.Platform, ev.Timestamp, ev.Environment, ev.Release, category).Scan(&created); err != nil {
			return "", nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE issue_hash_routes SET origin_issue_id=$3::bigint WHERE project_id=$1::bigint AND fingerprint=$2`, projectID, ev.Fingerprint, created); err != nil {
			return "", nil, err
		}
		own = &created
	}
	var target *string
	var existingCategory string
	if err := tx.QueryRow(ctx, `SELECT merged_into::text,category FROM issues WHERE project_id=$1::bigint AND id=$2::bigint FOR UPDATE`, projectID, *own).Scan(&target, &existingCategory); err != nil {
		return "", nil, err
	}
	if existingCategory != category {
		return "", nil, ErrConflict
	}
	return *own, target, nil
}

func (s *Store) CompatibleIssueHashes(ctx context.Context, access ManagementAccess, id string, f CompatibleIssueHashFilter) ([]CompatibleIssueHash, error) {
	if !canonicalIssueID(access.UserID) || !canonicalIssueID(access.OrganizationID) || !canonicalIssueID(id) || f.Offset < 0 || f.Offset > 100000000 || f.Limit < 1 || f.Limit > 100 {
		return nil, ErrInvalidCompatibleIssueFilter
	}
	tx, scope, err := s.compatibleEventTransaction(ctx, access, EventIssueTarget(id))
	if err != nil {
		return nil, compatibleIssueTargetError(err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT replace(h.id::text,'-',''),h.origin_issue_id FROM issue_hash_routes h
 JOIN issues origin ON origin.id=h.origin_issue_id AND origin.project_id=h.project_id
 WHERE h.project_id=$1 AND COALESCE(origin.merged_into,origin.id)=$2
 ORDER BY h.id LIMIT $3 OFFSET $4`, scope.projectID, scope.issueID, f.Limit+1, f.Offset)
	if err != nil {
		return nil, err
	}
	type selectedHash struct {
		value  CompatibleIssueHash
		origin int64
	}
	selected := []selectedHash{}
	for rows.Next() {
		var item selectedHash
		if err = rows.Scan(&item.value.ID, &item.origin); err != nil {
			rows.Close()
			return nil, err
		}
		selected = append(selected, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := make([]CompatibleIssueHash, 0, len(selected))
	for _, item := range selected {
		record, _, err := scanCompatibleEvent(tx.QueryRow(ctx, compatibleEventSelect+compatibleEventJoins+`
 WHERE e.project_id=$1 AND e.issue_id=$2 AND COALESCE(e.original_issue_id,e.issue_id)=$3
 ORDER BY e.timestamp DESC,e.id DESC LIMIT 1`, scope.projectID, scope.issueID, item.origin))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			item.value.Latest = &record
		}
		result = append(result, item.value)
	}
	return result, tx.Commit(ctx)
}

func (s *Store) ResetCompatibleIssueHashes(ctx context.Context, access ManagementAccess, id string, hashIDs []string) error {
	if access.ReadOnly {
		return ErrForbidden
	}
	if !canonicalIssueID(access.UserID) || !canonicalIssueID(access.OrganizationID) || !canonicalIssueID(id) || len(hashIDs) < 1 || len(hashIDs) > 1000 {
		return ErrInvalidCompatibleIssueChange
	}
	ids := make([]string, 0, len(hashIDs))
	for _, value := range hashIDs {
		ref, ok := compatibleEventReference(value, false)
		if !ok {
			return ErrInvalidCompatibleIssueChange
		}
		ids = append(ids, ref)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var project string
	if err = tx.QueryRow(ctx, `SELECT p.id::text FROM issues i JOIN projects p ON p.id=i.project_id
 JOIN memberships m ON m.organization_id=p.organization_id
 WHERE i.id=$1::bigint AND p.organization_id=$2::bigint AND m.user_id=$3::bigint
 AND ($4 OR p.id=ANY($5::bigint[])) FOR SHARE OF m`, id, access.OrganizationID, access.UserID, access.AllProjects, access.ProjectIDs).Scan(&project); err != nil {
		return dbError(err)
	}
	if err = lockProjectIssues(ctx, tx, project, true); err != nil {
		return err
	}
	var merged *string
	if err = tx.QueryRow(ctx, `SELECT merged_into::text FROM issues WHERE id=$1::bigint`, id).Scan(&merged); err != nil {
		return dbError(err)
	}
	if merged != nil {
		return &IssueMergedError{MergedInto: *merged}
	}
	root, _ := strconv.ParseInt(id, 10, 64)
	if _, err = lockIssueGroups(ctx, tx, []int64{root}); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `UPDATE issue_hash_routes h SET origin_issue_id=NULL FROM issues origin
 WHERE h.origin_issue_id=origin.id AND h.project_id=origin.project_id AND h.project_id=$1::bigint
 AND COALESCE(origin.merged_into,origin.id)=$2::bigint
 AND h.id IN(SELECT value::uuid FROM unnest($3::text[]) AS selected(value))
 RETURNING replace(h.id::text,'-','')`, project, id, ids)
	if err != nil {
		return err
	}
	changed, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		slices.Sort(changed)
		if _, err = recordIssueActivity(ctx, tx, id, &access.UserID, "grouping_reset", map[string]any{"hashIds": changed}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
