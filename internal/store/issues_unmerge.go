package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"
)

func (s *Store) UnmergeIssue(ctx context.Context, userID, id string, originIDs []string) (IssueUnmergeResult, error) {
	result := IssueUnmergeResult{DetachedIssueIDs: []string{}}
	selected, err := issueGroupIDs(originIDs, 1)
	if err != nil {
		return result, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var org, project string
	if err = tx.QueryRow(ctx, `SELECT p.organization_id::text,p.id::text FROM issues i JOIN projects p ON p.id=i.project_id JOIN memberships m ON m.organization_id=p.organization_id WHERE i.id::text=$1 AND m.user_id::text=$2 FOR SHARE OF m`, id, userID).Scan(&org, &project); err != nil {
		return result, dbError(err)
	}
	if err = lockProjectIssues(ctx, tx, project, true); err != nil {
		return result, err
	}
	var merged *string
	if err = tx.QueryRow(ctx, `SELECT merged_into::text FROM issues WHERE id::text=$1`, id).Scan(&merged); err != nil {
		return result, dbError(err)
	}
	if merged != nil {
		return result, &IssueMergedError{MergedInto: *merged}
	}
	selection, err := readIssueSelection(ctx, tx, org, selected)
	if err != nil {
		return result, err
	}
	root, _ := strconv.ParseInt(id, 10, 64)
	for _, item := range selection {
		if item.project != project || item.root != root && item.root != item.id {
			return result, fmt.Errorf("%w: origin belongs to another group or project", ErrInvalidIssueChange)
		}
	}
	members, err := lockIssueGroups(ctx, tx, []int64{root})
	if err != nil {
		return result, err
	}
	detached, remaining := []int64{}, []int64{}
	for _, member := range members {
		if _, wanted := slices.BinarySearch(selected, member); wanted {
			detached = append(detached, member)
		} else {
			remaining = append(remaining, member)
		}
	}
	if len(remaining) > 0 {
		value := strconv.FormatInt(remaining[0], 10)
		result.RemainingIssueID = &value
	}
	if len(members) == 1 || len(detached) == 0 {
		result.Issue, err = readGroupIssue(ctx, tx, userID, id)
		if err != nil {
			return result, err
		}
		return result, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE events SET original_issue_id=issue_id WHERE issue_id=$1::bigint AND original_issue_id IS NULL`, id); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE events SET issue_id=CASE WHEN original_issue_id=ANY($2::bigint[]) THEN original_issue_id ELSE $3::bigint END WHERE issue_id=$1::bigint`, id, detached, result.RemainingIssueID); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE issues SET merged_into=CASE WHEN id=ANY($2::bigint[]) OR id=$3::bigint THEN NULL ELSE $3::bigint END WHERE id=ANY($1::bigint[])`, members, detached, result.RemainingIssueID); err != nil {
		return result, err
	}
	roots := slices.Clone(detached)
	if len(remaining) > 0 {
		roots = append(roots, remaining[0])
	}
	if err = recountIssueGroups(ctx, tx, roots); err != nil {
		return result, err
	}
	result.Issue, err = readGroupIssue(ctx, tx, userID, id)
	if err != nil {
		return result, err
	}
	for _, origin := range detached {
		result.DetachedIssueIDs = append(result.DetachedIssueIDs, strconv.FormatInt(origin, 10))
	}
	if _, err = recordIssueActivity(ctx, tx, id, &userID, "unmerged", map[string]any{"originIds": result.DetachedIssueIDs, "remainingIssueId": result.RemainingIssueID, "count": result.Issue.Count}); err != nil {
		return result, err
	}
	for _, next := range roots {
		if next == root {
			continue
		}
		nextID := strconv.FormatInt(next, 10)
		var count int64
		if err = tx.QueryRow(ctx, `SELECT count FROM issues WHERE id=$1::bigint`, nextID).Scan(&count); err != nil {
			return result, err
		}
		data := map[string]any{"sourceIssueId": id, "remainingIssueId": result.RemainingIssueID, "count": count}
		if len(remaining) > 0 && next == remaining[0] {
			data["originIds"] = result.DetachedIssueIDs
		}
		if _, err = recordIssueActivity(ctx, tx, nextID, &userID, "unmerged", data); err != nil {
			return result, err
		}
	}
	result.Changed = true
	return result, tx.Commit(ctx)
}
