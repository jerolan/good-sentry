package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type CompatibleIssueBulkFilter struct {
	IDs, ProjectRefs, Environments []string
	Query, Status, Sort            string
	Start, End                     *time.Time
	Period                         time.Duration
	Limit                          int
}

type CompatibleIssueBulkChange struct {
	Change CompatibleIssueChange
	Merge  bool
}

type CompatibleIssueBulkMerge struct {
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
}

type CompatibleIssueBulkResult struct {
	First *Issue
	Merge *CompatibleIssueBulkMerge
}

func validCompatibleIssueBulkFilter(f CompatibleIssueBulkFilter) bool {
	if len(f.IDs) > 1000 || len(f.ProjectRefs) > 100 || !validEventEnvironments(f.Environments) || f.Limit < 1 || f.Limit > 1000 || f.Period < 0 || (f.Start != nil && f.End != nil && !f.End.After(*f.Start)) {
		return false
	}
	for _, id := range f.IDs {
		if !canonicalIssueID(id) {
			return false
		}
	}
	for _, ref := range f.ProjectRefs {
		if ref == "" || !utf8.ValidString(ref) || strings.ContainsRune(ref, 0) || ref == "-1" && len(f.ProjectRefs) != 1 {
			return false
		}
	}
	switch f.Status {
	case "", "unresolved", "resolved", "ignored", "muted":
	default:
		return false
	}
	_, ok := issueSort(f.Sort)
	return ok
}

func bulkIssueReleaseChange(c CompatibleIssueChange) bool {
	if c.Status == "resolvedInNextRelease" {
		return true
	}
	if c.Policy == nil {
		return false
	}
	return c.Policy.Kind == CompatibleIssueReleasePolicy || c.Policy.Kind == CompatibleIssueLatestReleasePolicy || c.Policy.Kind == CompatibleIssueNextReleasePolicy
}

// Authorization, selection, topology and triage share this transaction. A
// candidate cohort never expands after waiting for another project writer.
func (s *Store) UpdateCompatibleIssues(ctx context.Context, access ManagementAccess, projectRef string, filter CompatibleIssueBulkFilter, command CompatibleIssueBulkChange) (CompatibleIssueBulkResult, error) {
	var result CompatibleIssueBulkResult
	if access.ReadOnly {
		return result, ErrForbidden
	}
	if !canonicalIssueID(access.UserID) || !canonicalIssueID(access.OrganizationID) || !validCompatibleIssueBulkFilter(filter) || !utf8.ValidString(projectRef) || strings.ContainsRune(projectRef, 0) {
		return result, ErrInvalidCompatibleIssueFilter
	}
	change := command.Change
	if !validCompatibleIssueChange(change) {
		return result, ErrInvalidCompatibleIssueChange
	}
	ids := issueNumericIDs(filter.IDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if command.Merge && len(ids) < 2 || len(ids) == 0 && (change.Assignment != nil || change.Policy != nil && change.Status == "") {
		return result, ErrInvalidCompatibleIssueChange
	}
	search, err := ParseEventSearch(filter.Query)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrInvalidCompatibleIssueFilter, err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var member string
	if err = tx.QueryRow(ctx, `SELECT user_id::text FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint FOR SHARE`, access.UserID, access.OrganizationID).Scan(&member); err != nil {
		return result, dbError(err)
	}
	selection, err := prepareBulkIssueSelection(ctx, tx, access, projectRef, filter, search)
	if err != nil {
		return result, err
	}
	if len(ids) > 0 {
		visible, err := selection.contains(ctx, tx, ids)
		if err != nil {
			return result, err
		}
		if !visible {
			return result, tx.Commit(ctx)
		}
	}
	hasChange := change.Status != "" || change.Assignment != nil || change.Policy != nil
	if !hasChange && !command.Merge {
		return result, tx.Commit(ctx)
	}
	candidates, err := selection.roots(ctx, tx, ids)
	if err != nil {
		return result, err
	}
	if len(candidates) == 0 {
		return result, tx.Commit(ctx)
	}
	cohort, projects := []int64{}, []int64{}
	for _, item := range candidates {
		cohort = append(cohort, item.id)
		projects = append(projects, item.project)
	}
	selection.retainExplicitIDs(ids)
	if err = lockBulkIssueProjects(ctx, tx, projects); err != nil {
		return result, err
	}
	if len(ids) > 0 {
		visible, err := selection.contains(ctx, tx, ids)
		if err != nil {
			return result, err
		}
		if !visible {
			return result, tx.Commit(ctx)
		}
	}
	candidates, err = selection.roots(ctx, tx, cohort)
	if err != nil {
		return result, err
	}
	if len(candidates) == 0 {
		return result, tx.Commit(ctx)
	}
	roots := make([]int64, 0, len(candidates))
	projects = projects[:0]
	for _, item := range candidates {
		roots = append(roots, item.id)
		projects = append(projects, item.project)
	}
	slices.Sort(roots)
	roots = slices.Compact(roots)
	slices.Sort(projects)
	projects = slices.Compact(projects)
	members, err := lockIssueGroups(ctx, tx, roots)
	if err != nil {
		return result, err
	}
	// A maintenance worker can expire a policy while this writer waits for a row.
	// Topology is stable under the gates, but status filters need a fresh read.
	candidates, err = selection.roots(ctx, tx, roots)
	if err != nil {
		return result, err
	}
	roots = roots[:0]
	projects = projects[:0]
	for _, item := range candidates {
		roots = append(roots, item.id)
		projects = append(projects, item.project)
	}
	slices.Sort(roots)
	roots = slices.Compact(roots)
	slices.Sort(projects)
	projects = slices.Compact(projects)
	if len(roots) == 0 {
		return result, tx.Commit(ctx)
	}
	if (command.Merge || bulkIssueReleaseChange(change)) && len(projects) > 1 {
		return result, ErrInvalidCompatibleIssueChange
	}
	if command.Merge {
		// The last filter read can only remove roots. Keep the merge's member set
		// restricted to those remaining roots instead of absorbing an excluded group.
		members, err = lockIssueGroups(ctx, tx, roots)
		if err != nil {
			return result, err
		}
		merged, err := applyIssueMerge(ctx, tx, access.UserID, roots, members)
		if err != nil {
			if errors.Is(err, ErrInvalidIssueChange) {
				return result, ErrInvalidCompatibleIssueChange
			}
			return result, err
		}
		children := make([]string, 0, len(roots)-1)
		for _, id := range roots[1:] {
			children = append(children, strconv.FormatInt(id, 10))
		}
		result.Merge = &CompatibleIssueBulkMerge{Parent: merged.Issue.ID, Children: children}
		roots = roots[:1]
	}
	for _, id := range roots {
		issue, err := readGroupIssue(ctx, tx, access.UserID, strconv.FormatInt(id, 10))
		if err != nil {
			return result, err
		}
		if hasChange {
			native, err := compatibleIssueChange(ctx, tx, access.OrganizationID, issue, change)
			if err != nil {
				return result, err
			}
			if !validateIssueUpdate(native) {
				return result, ErrInvalidCompatibleIssueChange
			}
			if change.Policy != nil && change.Policy.Kind == CompatibleIssueLatestReleasePolicy {
				// One command selects one boundary, including when metadata is
				// created concurrently between individual root updates.
				policy := *change.Policy
				policy.Kind = CompatibleIssueReleasePolicy
				policy.Version = native.Resolution.Policy.Version
				change.Policy = &policy
			}
			issue, err = applyIssueUpdate(ctx, tx, access.UserID, access.OrganizationID, issue, native)
			if err != nil {
				return result, err
			}
		}
		if result.First == nil {
			result.First = &issue
		}
	}
	return result, tx.Commit(ctx)
}
