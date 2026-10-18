package store

import (
	"context"
	"strings"
	"unicode/utf8"
)

// SubmitManagedUserReport pins current membership and the granted project in
// the same transaction that acknowledges feedback. Ordinary members may submit.
func (s *Store) SubmitManagedUserReport(ctx context.Context, access ManagementAccess, projectRef string, input UserReportInput) (UserReport, error) {
	if access.ReadOnly {
		return UserReport{}, ErrForbidden
	}
	input, err := NormalizeUserReport(input)
	if err != nil || !validEventTarget(access, EventProjectTarget(projectRef)) {
		return UserReport{}, ErrInvalidUserReport
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserReport{}, err
	}
	defer tx.Rollback(ctx)
	var projectID string
	err = tx.QueryRow(ctx, `SELECT p.id::text FROM projects p
 JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
 WHERE p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[]))
 AND (p.slug=$5 OR p.id=$6) ORDER BY (p.slug=$5) DESC LIMIT 1
 FOR SHARE OF m FOR KEY SHARE OF p`, access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs, projectRef, referenceID(projectRef)).Scan(&projectID)
	if err != nil {
		return UserReport{}, dbError(err)
	}
	return commitUserReport(ctx, tx, projectID, input)
}

// SubmitDSNUserReport binds the path and the enabled key to one project before
// any write. Key revocation waits for this already authorized transaction.
func (s *Store) SubmitDSNUserReport(ctx context.Context, orgRef, projectRef, key string, input UserReportInput) (UserReport, error) {
	for _, value := range []string{orgRef, projectRef, key} {
		if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return UserReport{}, ErrNotFound
		}
	}
	input, err := NormalizeUserReport(input)
	if err != nil {
		return UserReport{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserReport{}, err
	}
	defer tx.Rollback(ctx)
	var projectID string
	err = tx.QueryRow(ctx, `SELECT p.id::text FROM project_keys k
 JOIN projects p ON p.id=k.project_id JOIN organizations o ON o.id=p.organization_id
 WHERE k.public_key=$1 AND k.enabled AND (o.slug=$2 OR o.id=$3)
 AND (p.slug=$4 OR p.id=$5) FOR SHARE OF k FOR KEY SHARE OF p`, key, orgRef, referenceID(orgRef), projectRef, referenceID(projectRef)).Scan(&projectID)
	if err != nil {
		return UserReport{}, dbError(err)
	}
	return commitUserReport(ctx, tx, projectID, input)
}
