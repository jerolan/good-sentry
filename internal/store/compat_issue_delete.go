package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func (s *Store) DeleteCompatibleIssue(ctx context.Context, access ManagementAccess, id string) error {
	return s.deleteCompatibleIssue(ctx, access, id, false)
}

func (s *Store) DiscardCompatibleIssue(ctx context.Context, access ManagementAccess, id string) error {
	return s.deleteCompatibleIssue(ctx, access, id, true)
}

func (s *Store) deleteCompatibleIssue(ctx context.Context, access ManagementAccess, id string, discard bool) error {
	if access.ReadOnly {
		return ErrForbidden
	}
	if !canonicalIssueID(id) || !canonicalIssueID(access.UserID) || access.OrganizationID != "" && !canonicalIssueID(access.OrganizationID) {
		return ErrInvalidCompatibleIssueChange
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var project string
	if err = tx.QueryRow(ctx, `SELECT p.id::text FROM issues i
 JOIN projects p ON p.id=i.project_id JOIN memberships m ON m.organization_id=p.organization_id
 WHERE i.id=$1::bigint AND m.user_id=$2::bigint AND ($3::bigint=0 OR p.organization_id=$3)
 AND ($4 OR p.id=ANY($5::bigint[])) FOR SHARE OF m`, id, access.UserID, referenceID(access.OrganizationID), access.AllProjects, access.ProjectIDs).Scan(&project); err != nil {
		return dbError(err)
	}
	if err = lockProjectIssues(ctx, tx, project, true); err != nil {
		return err
	}
	var merged *string
	var category string
	if err = tx.QueryRow(ctx, `SELECT merged_into::text,category FROM issues WHERE id=$1::bigint`, id).Scan(&merged, &category); err != nil {
		return dbError(err)
	}
	if merged != nil {
		return &IssueMergedError{MergedInto: *merged}
	}
	if discard && category != "error" {
		return ErrInvalidCompatibleIssueChange
	}
	root, _ := strconv.ParseInt(id, 10, 64)
	roots := []int64{root}
	if _, err = lockIssueGroups(ctx, tx, roots); err != nil {
		return err
	}
	if discard {
		if _, err = tx.Exec(ctx, `INSERT INTO discarded_issue_hashes(project_id,fingerprint)
 SELECT route.project_id,route.fingerprint FROM issue_hash_routes route
 JOIN issues origin ON origin.id=route.origin_issue_id AND origin.project_id=route.project_id
 WHERE route.project_id=$1::bigint AND (origin.id=$2::bigint OR origin.merged_into=$2::bigint)
 AND origin.category='error' ON CONFLICT(project_id,fingerprint) DO NOTHING`, project, root); err != nil {
			return err
		}
	}
	if err = deleteCompatibleIssueGroups(ctx, tx, roots); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func compatibleIssueDeletionFilter(f CompatibleIssueBulkFilter) (CompatibleIssueBulkFilter, error) {
	if len(f.IDs) > 0 {
		if len(f.ProjectRefs) > 100 || len(f.Environments) > 100 {
			return f, ErrInvalidCompatibleIssueFilter
		}
		// Explicit IDs ignore known filter values, but their framing remains
		// valid even when the ignored value is not understood by a selector.
		for _, values := range [][]string{f.ProjectRefs, f.Environments, {f.Query, f.Status, f.Sort}} {
			for _, value := range values {
				if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
					return f, ErrInvalidCompatibleIssueFilter
				}
			}
		}
		f = CompatibleIssueBulkFilter{IDs: f.IDs, Limit: f.Limit}
	}
	if !validCompatibleIssueBulkFilter(f) {
		return f, ErrInvalidCompatibleIssueFilter
	}
	return f, nil
}

// Like bulk PUT, the candidate cohort cannot expand after waiting for another
// project writer. Complete explicit-ID admission is repeated after those waits.
func (s *Store) DeleteCompatibleIssues(ctx context.Context, access ManagementAccess, projectRef string, filter CompatibleIssueBulkFilter) error {
	if access.ReadOnly {
		return ErrForbidden
	}
	if !canonicalIssueID(access.UserID) || !canonicalIssueID(access.OrganizationID) || !utf8.ValidString(projectRef) || strings.ContainsRune(projectRef, 0) {
		return ErrInvalidCompatibleIssueFilter
	}
	filter, err := compatibleIssueDeletionFilter(filter)
	if err != nil {
		return err
	}
	search, err := ParseEventSearch(filter.Query)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCompatibleIssueFilter, err)
	}
	ids := issueNumericIDs(filter.IDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var member string
	if err = tx.QueryRow(ctx, `SELECT user_id::text FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint FOR SHARE`, access.UserID, access.OrganizationID).Scan(&member); err != nil {
		return dbError(err)
	}
	selection, err := prepareBulkIssueSelection(ctx, tx, access, projectRef, filter, search)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		visible, err := selection.contains(ctx, tx, ids)
		if err != nil {
			return err
		}
		if !visible {
			return tx.Commit(ctx)
		}
	}
	candidates, err := selection.roots(ctx, tx, ids)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return tx.Commit(ctx)
	}
	cohort, projects := []int64{}, []int64{}
	for _, item := range candidates {
		cohort = append(cohort, item.id)
		projects = append(projects, item.project)
	}
	selection.retainExplicitIDs(ids)
	if err = lockBulkIssueProjects(ctx, tx, projects); err != nil {
		return err
	}
	if len(ids) > 0 {
		visible, err := selection.contains(ctx, tx, ids)
		if err != nil {
			return err
		}
		if !visible {
			return tx.Commit(ctx)
		}
	}
	candidates, err = selection.roots(ctx, tx, cohort)
	if err != nil {
		return err
	}
	roots := make([]int64, 0, len(candidates))
	for _, item := range candidates {
		roots = append(roots, item.id)
	}
	if len(roots) == 0 {
		return tx.Commit(ctx)
	}
	if _, err = lockIssueGroups(ctx, tx, roots); err != nil {
		return err
	}
	// Row-only maintenance workers may have changed a filter while we waited.
	// Only remove surviving members of the frozen cohort; never backfill.
	candidates, err = selection.roots(ctx, tx, roots)
	if err != nil {
		return err
	}
	roots = roots[:0]
	for _, item := range candidates {
		roots = append(roots, item.id)
	}
	if len(roots) > 0 {
		if err = deleteCompatibleIssueGroups(ctx, tx, roots); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// The caller holds exclusive project gates and all group rows in descending
// order. Route UUIDs and independent notification snapshots outlive the group.
func deleteCompatibleIssueGroups(ctx context.Context, tx pgx.Tx, roots []int64) error {
	if _, err := tx.Exec(ctx, `UPDATE issue_hash_routes SET origin_issue_id=NULL
 WHERE origin_issue_id IN(SELECT id FROM issues WHERE id=ANY($1::bigint[]) OR merged_into=ANY($1::bigint[]))`, roots); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM envelope_items item USING events e
 WHERE e.issue_id=ANY($1::bigint[]) AND e.kind<>'transaction' AND e.event_id<>''
 AND item.project_id=e.project_id AND item.event_id=e.event_id`, roots); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM issues WHERE id=ANY($1::bigint[]) OR merged_into=ANY($1::bigint[])`, roots)
	return err
}
