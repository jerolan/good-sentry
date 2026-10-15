package store

import (
	"context"
	"strings"
	"unicode/utf8"
)

func (s *Store) IssueOrigins(ctx context.Context, userID, id, query string, page int) (IssueOriginPage, error) {
	result := IssueOriginPage{Data: []IssueOrigin{}, Page: page, PageSize: 25}
	offset, err := issuePage(page)
	if err != nil {
		return result, err
	}
	if len(query) > 200 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
		return result, ErrInvalidIssueChange
	}
	tx, issue, _, _, err := s.issueTransaction(ctx, userID, id, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	result.IssueID = issue.ID
	// A NULL legacy origin still belongs to its current owner until the first
	// reparenting saves that owner. Completely unowned rows never match a member.
	from := ` FROM issues origin
 LEFT JOIN LATERAL (SELECT count(*) AS n,min(timestamp) AS first_seen,max(timestamp) AS last_seen
 FROM events WHERE original_issue_id=origin.id OR (original_issue_id IS NULL AND issue_id=origin.id)) totals ON true
 LEFT JOIN LATERAL (SELECT title FROM events WHERE original_issue_id=origin.id OR (original_issue_id IS NULL AND issue_id=origin.id)
 ORDER BY timestamp DESC,id DESC LIMIT 1) latest ON true
 WHERE (origin.id=$1::bigint OR origin.merged_into=$1::bigint)
 AND (origin.id::text ILIKE $2 ESCAPE E'\\' OR origin.fingerprint ILIKE $2 ESCAPE E'\\' OR COALESCE(latest.title,origin.title) ILIKE $2 ESCAPE E'\\') `
	pattern := "%" + issueSearchPattern(query, false) + "%"
	if err = tx.QueryRow(ctx, "SELECT count(*)"+from, issue.ID, pattern).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT origin.id::text,origin.fingerprint,COALESCE(latest.title,origin.title),totals.n,totals.first_seen,totals.last_seen,origin.id=$1::bigint,EXISTS(SELECT 1 FROM issue_hash_routes h WHERE h.project_id=origin.project_id AND h.origin_issue_id=origin.id)`+from+` ORDER BY origin.id LIMIT 25 OFFSET $3`, issue.ID, pattern, offset)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var item IssueOrigin
		if err = rows.Scan(&item.ID, &item.Fingerprint, &item.Title, &item.Count, &item.FirstSeen, &item.LastSeen, &item.IsRoot, &item.ReceivesNewEvents); err != nil {
			rows.Close()
			return result, err
		}
		item.FirstSeen, item.LastSeen = releaseTimeUTC(item.FirstSeen), releaseTimeUTC(item.LastSeen)
		result.Data = append(result.Data, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
