package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type bulkIssueCandidate struct{ id, project int64 }

type bulkIssueSelection struct {
	scope      string
	scopeCount int
	args       []any
	terms      []string
	order      string
	limit      int
}

func prepareBulkIssueSelection(ctx context.Context, tx pgx.Tx, access ManagementAccess, projectRef string, f CompatibleIssueBulkFilter, search IssueSearch) (bulkIssueSelection, error) {
	s := bulkIssueSelection{scope: `p.organization_id=$1::bigint AND ($2 OR p.id=ANY($3::bigint[]))`, args: []any{access.OrganizationID, access.AllProjects, access.ProjectIDs}, limit: f.Limit}
	bind := func(v any) string { s.args = append(s.args, v); return fmt.Sprintf("$%d", len(s.args)) }
	if projectRef != "" {
		ids, err := releaseProjectReferences(ctx, tx, access, []string{projectRef})
		if err != nil {
			return s, err
		}
		s.scope += " AND p.id=" + bind(ids[0])
	}
	if len(f.ProjectRefs) > 0 && f.ProjectRefs[0] != "-1" {
		ids, err := releaseProjectReferences(ctx, tx, access, f.ProjectRefs)
		if err != nil {
			return s, err
		}
		s.scope += " AND p.id=ANY(" + bind(ids) + "::bigint[])"
	}
	// Scope parameters are kept separate from occurrence/status parameters so all
	// explicit IDs are admitted before query filtering or result limiting.
	s.scopeCount = len(s.args)
	s.order, _ = issueSort(f.Sort)
	status := f.Status
	if status == "muted" {
		status = "ignored"
	}
	if status != "" {
		s.terms = append(s.terms, "i.status="+bind(status))
	}
	issueTerms, _, err := issueSearchPredicates(issueStatusSearch(search), &s.args)
	if err != nil {
		return s, err
	}
	s.terms = append(s.terms, issueTerms...)
	occurrence, _, _, err := prepareIssueMetrics(ctx, tx, CompatibleIssueFilter{Environments: f.Environments, Start: f.Start, End: f.End, Period: f.Period}, search)
	if err != nil {
		return s, err
	}
	terms, err := occurrence.predicates(&s.args)
	if err != nil {
		return s, err
	}
	if len(terms) > 0 {
		s.terms = append(s.terms, `EXISTS(SELECT 1 FROM events e WHERE e.issue_id=i.id AND `+strings.Join(terms, " AND ")+`)`)
	}
	return s, nil
}

func (s bulkIssueSelection) contains(ctx context.Context, tx pgx.Tx, ids []int64) (bool, error) {
	args := append([]any(nil), s.args[:s.scopeCount]...)
	args = append(args, ids)
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM issues i JOIN projects p ON p.id=i.project_id WHERE `+s.scope+fmt.Sprintf(" AND i.id=ANY($%d::bigint[])", len(args)), args...).Scan(&n)
	return n == len(ids), err
}

// Freezing roots must not lose the caller's selected identities. A native split
// can detach the only selected alias before the project gate becomes available.
func (s *bulkIssueSelection) retainExplicitIDs(ids []int64) {
	if len(ids) == 0 {
		return
	}
	s.args = append(s.args, ids)
	s.terms = append(s.terms, fmt.Sprintf(`i.id IN(SELECT COALESCE(selected.merged_into,selected.id) FROM issues selected
 WHERE selected.project_id=i.project_id AND selected.id=ANY($%d::bigint[]))`, len(s.args)))
}

func (s bulkIssueSelection) roots(ctx context.Context, tx pgx.Tx, ids []int64) ([]bulkIssueCandidate, error) {
	args := append([]any(nil), s.args...)
	where := s.scope + ` AND i.merged_into IS NULL`
	if len(ids) > 0 {
		args = append(args, ids)
		where += fmt.Sprintf(` AND i.id IN(SELECT COALESCE(origin.merged_into,origin.id) FROM issues origin WHERE origin.project_id=i.project_id AND origin.id=ANY($%d::bigint[]))`, len(args))
	}
	if len(s.terms) > 0 {
		where += " AND " + strings.Join(s.terms, " AND ")
	}
	args = append(args, s.limit)
	rows, err := tx.Query(ctx, `SELECT i.id,p.id FROM issues i JOIN projects p ON p.id=i.project_id WHERE `+where+" ORDER BY "+s.order+fmt.Sprintf(" LIMIT $%d", len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []bulkIssueCandidate{}
	for rows.Next() {
		var v bulkIssueCandidate
		if err = rows.Scan(&v.id, &v.project); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// Gates are acquired by their actual signed keys. A collision cannot reverse
// the order of keys across requests that selected different project sets.
func lockBulkIssueProjects(ctx context.Context, tx pgx.Tx, projects []int64) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT hashtextextended(current_schema()||':issue-group:'||project_id::text,0) AS key FROM unnest($1::bigint[]) AS project_id ORDER BY key`, projects)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return err
		}
	}
	return nil
}
