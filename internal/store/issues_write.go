package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Both native and compatible callers lock current membership before the project
// gate and issue row. The fresh read after waiting observes the preceding writer.
func (s *Store) issueWriteTransaction(ctx context.Context, access ManagementAccess, id string) (pgx.Tx, Issue, string, string, error) {
	if access.ReadOnly {
		return nil, Issue{}, "", "", ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, Issue{}, "", "", err
	}
	fail := func(err error) (pgx.Tx, Issue, string, string, error) {
		_ = tx.Rollback(ctx)
		return nil, Issue{}, "", "", dbError(err)
	}
	var org, role, project string
	err = tx.QueryRow(ctx, `SELECT p.organization_id::text,m.role,p.id::text FROM issues i
 JOIN projects p ON p.id=i.project_id JOIN memberships m ON m.organization_id=p.organization_id
 WHERE i.id::text=$1 AND m.user_id::text=$2 AND ($3::bigint=0 OR p.organization_id=$3)
 AND ($4 OR p.id=ANY($5::bigint[])) FOR SHARE OF m`, id, access.UserID, referenceID(access.OrganizationID), access.AllProjects, access.ProjectIDs).Scan(&org, &role, &project)
	if err != nil {
		return fail(err)
	}
	if err = lockProjectIssues(ctx, tx, project, false); err != nil {
		return fail(err)
	}
	var merged *string
	if err = tx.QueryRow(ctx, `SELECT merged_into::text FROM issues WHERE id::text=$1 FOR UPDATE`, id).Scan(&merged); err != nil {
		return fail(err)
	}
	if merged != nil {
		return fail(&IssueMergedError{MergedInto: *merged})
	}
	i, err := scanIssue(tx.QueryRow(ctx, "SELECT "+issueColumns+issueJoins+` WHERE m.user_id::text=$1 AND i.id::text=$2`, access.UserID, id))
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, i, "", "", err
	}
	return tx, i, org, role, nil
}
