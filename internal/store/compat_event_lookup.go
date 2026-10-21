package store

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"goodsentry/internal/eventview"
)

var ErrAmbiguousEvent = errors.New("event ID matches multiple authorized projects")

type CompatibleEventWithIssue struct {
	Event eventview.Record
	Issue *CompatibleIssueRecord
}

// CompatibleEventLookup identifies a receipt inside the grant before loading
// its payload and parent. The same snapshot owns ambiguity and the association.
func (s *Store) CompatibleEventLookup(ctx context.Context, access ManagementAccess, projectRef, eventRef string) (CompatibleEventWithIssue, error) {
	var result CompatibleEventWithIssue
	reference, valid := compatibleEventReference(eventRef, false)
	if !valid || !utf8.ValidString(projectRef) || utf8.RuneCountInString(projectRef) > 200 || strings.ContainsRune(projectRef, 0) {
		return result, ErrInvalidEventFilter
	}
	tx, err := s.compatibleIssueOrganization(ctx, access)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var projectID int64
	if projectRef != "" {
		projects, err := releaseProjectReferences(ctx, tx, access, []string{projectRef})
		if err != nil {
			return result, err
		}
		projectID = projects[0]
	}
	rows, err := tx.Query(ctx, `SELECT e.id FROM events e JOIN projects p ON p.id=e.project_id
 WHERE p.organization_id=$1::bigint AND ($2 OR p.id=ANY($3::bigint[]))
 AND e.event_id=$4 AND ($5::bigint=0 OR p.id=$5)
 ORDER BY e.project_id,e.id LIMIT 2`, access.OrganizationID, access.AllProjects, access.ProjectIDs, reference, projectID)
	if err != nil {
		return result, err
	}
	var receipts []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		receipts = append(receipts, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(receipts) == 0 {
		return result, ErrNotFound
	}
	if len(receipts) > 1 {
		return result, ErrAmbiguousEvent
	}
	result.Event, _, err = scanCompatibleEvent(tx.QueryRow(ctx, compatibleEventSelect+compatibleEventJoins+` WHERE e.id=$1`, receipts[0]))
	if err != nil {
		return result, dbError(err)
	}
	if result.Event.GroupID != "" {
		issue, err := readCompatibleIssueRecord(ctx, tx, access.UserID, result.Event.GroupID, CompatibleIssueDetailFilter{StatsPeriod: "24h"})
		if err != nil {
			return CompatibleEventWithIssue{}, err
		}
		result.Issue = &issue
	}
	if err := tx.Commit(ctx); err != nil {
		return CompatibleEventWithIssue{}, err
	}
	return result, nil
}
