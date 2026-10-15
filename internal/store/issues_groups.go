package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type IssueMergedError struct{ MergedInto string }

func (e *IssueMergedError) Error() string { return "issue belongs to another current group" }

type IssueMergeResult struct {
	Issue   Issue `json:"issue"`
	Changed bool  `json:"changed"`
}

type IssueUnmergeResult struct {
	Issue            Issue    `json:"issue"`
	Changed          bool     `json:"changed"`
	DetachedIssueIDs []string `json:"detachedIssueIds"`
	RemainingIssueID *string  `json:"remainingIssueId"`
}

type IssueOrigin struct {
	ID          string     `json:"id"`
	Fingerprint string     `json:"fingerprint"`
	Title       string     `json:"title"`
	Count       int64      `json:"count"`
	FirstSeen   *time.Time `json:"firstSeen"`
	LastSeen    *time.Time `json:"lastSeen"`
	IsRoot      bool       `json:"isRoot"`
}

type IssueOriginPage struct {
	Data     []IssueOrigin `json:"data"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
	IssueID  string        `json:"issueId"`
}

// Membership precedes the project gate; every issue row lock follows it. Workers
// only hold one issue row and never acquire this gate in the opposite order.
func lockProjectIssues(ctx context.Context, tx pgx.Tx, projectID string, exclusive bool) error {
	function := "pg_advisory_xact_lock_shared"
	if exclusive {
		function = "pg_advisory_xact_lock"
	}
	_, err := tx.Exec(ctx, "SELECT "+function+"(hashtextextended(current_schema()||':issue-group:'||$1::text,0))", projectID)
	return err
}

func issueGroupIDs(ids []string, minimum int) ([]int64, error) {
	if len(ids) < minimum || len(ids) > 100 {
		return nil, fmt.Errorf("%w: select %d to 100 distinct issues", ErrInvalidIssueChange, minimum)
	}
	values := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if !canonicalIssueID(id) {
			return nil, ErrInvalidIssueChange
		}
		n, _ := strconv.ParseInt(id, 10, 64)
		if seen[n] {
			return nil, ErrInvalidIssueChange
		}
		seen[n] = true
		values = append(values, n)
	}
	slices.Sort(values)
	return values, nil
}

type issueGroupMember struct {
	id      int64
	project string
	root    int64
}

func readIssueSelection(ctx context.Context, tx pgx.Tx, org string, ids []int64) ([]issueGroupMember, error) {
	rows, err := tx.Query(ctx, `SELECT i.id,i.project_id::text,COALESCE(i.merged_into,i.id) FROM issues i JOIN projects p ON p.id=i.project_id WHERE p.organization_id=$1::bigint AND i.id=ANY($2::bigint[]) ORDER BY i.id`, org, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []issueGroupMember{}
	for rows.Next() {
		var item issueGroupMember
		if err = rows.Scan(&item.id, &item.project, &item.root); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) != len(ids) {
		return nil, ErrNotFound
	}
	for _, item := range items {
		if item.project != items[0].project {
			return nil, fmt.Errorf("%w: issues must belong to one project", ErrInvalidIssueChange)
		}
	}
	return items, nil
}

func lockIssueGroups(ctx context.Context, tx pgx.Tx, roots []int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM issues WHERE id=ANY($1::bigint[]) OR merged_into=ANY($1::bigint[]) ORDER BY id DESC FOR UPDATE`, roots)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, rows.Err()
}

func readGroupIssue(ctx context.Context, tx pgx.Tx, userID, id string) (Issue, error) {
	return scanIssue(tx.QueryRow(ctx, "SELECT "+issueColumns+issueJoins+"WHERE m.user_id::text=$1 AND i.id::text=$2", userID, id))
}

// Recount the current partition, without advancing its fresh-capture progress.
func recountIssueGroups(ctx context.Context, tx pgx.Tx, roots []int64) error {
	_, err := tx.Exec(ctx, `WITH wanted AS (SELECT unnest($1::bigint[]) AS id),
 totals AS (SELECT w.id,count(e.id) AS n,min(e.timestamp) AS first_seen,max(e.timestamp) AS last_seen
 FROM wanted w LEFT JOIN events e ON e.issue_id=w.id GROUP BY w.id),
 latest AS (SELECT DISTINCT ON (issue_id) issue_id,title,environment,release FROM events
 WHERE issue_id=ANY($1::bigint[]) ORDER BY issue_id,timestamp DESC,id DESC)
 UPDATE issues i SET count=t.n,first_seen=COALESCE(t.first_seen,i.first_seen),last_seen=COALESCE(t.last_seen,i.last_seen),
 title=COALESCE(l.title,i.title),environment=COALESCE(l.environment,i.environment),release=COALESCE(l.release,i.release)
 FROM totals t LEFT JOIN latest l ON l.issue_id=t.id WHERE i.id=t.id`, roots)
	return err
}

func (s *Store) MergeIssues(ctx context.Context, userID, orgSlug string, ids []string) (IssueMergeResult, error) {
	var result IssueMergeResult
	selected, err := issueGroupIDs(ids, 2)
	if err != nil {
		return result, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var org string
	if err = tx.QueryRow(ctx, `SELECT o.id::text FROM organizations o JOIN memberships m ON m.organization_id=o.id WHERE o.slug=$1 AND m.user_id::text=$2 FOR SHARE OF m`, orgSlug, userID).Scan(&org); err != nil {
		return result, dbError(err)
	}
	selection, err := readIssueSelection(ctx, tx, org, selected)
	if err != nil {
		return result, err
	}
	if err = lockProjectIssues(ctx, tx, selection[0].project, true); err != nil {
		return result, err
	}
	selection, err = readIssueSelection(ctx, tx, org, selected)
	if err != nil {
		return result, err
	}
	roots := []int64{}
	for _, item := range selection {
		roots = append(roots, item.root)
	}
	slices.Sort(roots)
	roots = slices.Compact(roots)
	members, err := lockIssueGroups(ctx, tx, roots)
	if err != nil {
		return result, err
	}
	result, err = applyIssueMerge(ctx, tx, userID, roots, members)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func applyIssueMerge(ctx context.Context, tx pgx.Tx, userID string, roots, members []int64) (IssueMergeResult, error) {
	var result IssueMergeResult
	var err error
	parent := strconv.FormatInt(roots[0], 10)
	if len(roots) > 1 {
		sources := make([]Issue, 0, len(roots)-1)
		sourceIDs := make([]string, 0, len(roots)-1)
		for _, id := range roots[1:] {
			source, err := readGroupIssue(ctx, tx, userID, strconv.FormatInt(id, 10))
			if err != nil {
				return result, err
			}
			sources = append(sources, source)
			sourceIDs = append(sourceIDs, source.ID)
		}
		if _, err = tx.Exec(ctx, `UPDATE events SET original_issue_id=issue_id WHERE issue_id=ANY($1::bigint[]) AND original_issue_id IS NULL`, roots); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `UPDATE events SET issue_id=$1::bigint WHERE issue_id=ANY($2::bigint[])`, parent, roots); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `UPDATE issues SET merged_into=CASE WHEN id=$1::bigint THEN NULL ELSE $1::bigint END WHERE id=ANY($2::bigint[])`, parent, members); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `UPDATE issues SET status='unresolved',resolved_at=NULL,regressed_at=NULL WHERE id=ANY($1::bigint[])`, roots[1:]); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM issue_ignores WHERE issue_id=ANY($1::bigint[])`, roots[1:]); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM issue_resolutions WHERE issue_id=ANY($1::bigint[])`, roots[1:]); err != nil {
			return result, err
		}
		if err = recountIssueGroups(ctx, tx, roots[:1]); err != nil {
			return result, err
		}
		result.Issue, err = readGroupIssue(ctx, tx, userID, parent)
		if err != nil {
			return result, err
		}
		if _, err = recordIssueActivity(ctx, tx, parent, &userID, "merged", map[string]any{"sourceIssueIds": sourceIDs, "count": result.Issue.Count}); err != nil {
			return result, err
		}
		for _, source := range sources {
			previous := map[string]any{"status": source.Status, "count": source.Count, "assignedTo": source.AssignedTo, "resolvedAt": source.ResolvedAt, "regressedAt": source.RegressedAt, "ignore": source.Ignore, "resolution": source.Resolution}
			if _, err = recordIssueActivity(ctx, tx, source.ID, &userID, "merged", map[string]any{"sourceIssueId": source.ID, "mergedInto": parent, "previous": previous}); err != nil {
				return result, err
			}
		}
		result.Changed = true
	} else {
		result.Issue, err = readGroupIssue(ctx, tx, userID, parent)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
