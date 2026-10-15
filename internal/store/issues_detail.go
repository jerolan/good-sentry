package store

import (
	"context"
	"encoding/json"
)

type IssueDetail struct {
	Issue        Issue           `json:"issue"`
	Event        json.RawMessage `json:"event"`
	Events       []EventSummary  `json:"events"`
	Comments     []Comment       `json:"comments"`
	CommentTotal int             `json:"commentTotal"`
}

// IssueDetail reads authorization, issue state, events and comments from the
// same repeatable-read snapshot, including when capture commits between queries.
func (s *Store) IssueDetail(ctx context.Context, userID, id string) (IssueDetail, error) {
	tx, issue, _, role, err := s.issueTransaction(ctx, userID, id, false)
	if err != nil {
		return IssueDetail{}, err
	}
	defer tx.Rollback(ctx)
	detail := IssueDetail{Issue: issue, Event: json.RawMessage(`{}`), Events: []EventSummary{}}
	rows, err := tx.Query(ctx, `SELECT event_id,timestamp,data FROM events WHERE issue_id=$1::bigint ORDER BY timestamp DESC,id DESC LIMIT 50`, issue.ID)
	if err != nil {
		return detail, err
	}
	for rows.Next() {
		var item EventSummary
		if err = rows.Scan(&item.ID, &item.DateCreated, &item.Data); err != nil {
			rows.Close()
			return detail, err
		}
		detail.Events = append(detail.Events, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return detail, err
	}
	if len(detail.Events) > 0 {
		detail.Event = detail.Events[0].Data
	}
	detail.Comments, detail.CommentTotal, err = readIssueComments(ctx, tx, userID, role, issue.ID, 0)
	if err != nil {
		return detail, err
	}
	return detail, tx.Commit(ctx)
}
