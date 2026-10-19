package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/eventview"
)

var ErrInvalidEventFilter = errors.New("invalid event filter")

// CompatibleEventTarget has exactly one boundary: a project reference or an issue ID.
type CompatibleEventTarget struct{ kind, reference string }

func EventProjectTarget(reference string) CompatibleEventTarget {
	return CompatibleEventTarget{"project", reference}
}
func EventIssueTarget(id string) CompatibleEventTarget { return CompatibleEventTarget{"issue", id} }

type CompatibleEventFilter struct {
	Environments  []string
	Query         string
	Start, End    *time.Time
	Period        time.Duration
	Offset, Limit int
	Sample        bool
}

type compatibleEventScope struct{ projectID, organizationID, issueID int64 }

func validEventEnvironments(values []string) bool {
	if len(values) > 100 {
		return false
	}
	for _, value := range values {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || utf8.RuneCountInString(value) > 200 {
			return false
		}
	}
	return true
}

func validEventTarget(access ManagementAccess, target CompatibleEventTarget) bool {
	if referenceID(access.UserID) == 0 || (access.OrganizationID != "" && referenceID(access.OrganizationID) == 0) {
		return false
	}
	switch target.kind {
	case "project":
		return access.OrganizationID != "" && target.reference != "" && utf8.ValidString(target.reference) && !strings.ContainsRune(target.reference, 0)
	case "issue":
		return referenceID(target.reference) > 0
	}
	return false
}

// compatibleEventTransaction resolves membership, grants and canonical issue ownership
// within the same repeatable-read snapshot used for records and siblings.
func (s *Store) compatibleEventTransaction(ctx context.Context, access ManagementAccess, target CompatibleEventTarget) (pgx.Tx, compatibleEventScope, error) {
	var scope compatibleEventScope
	if !validEventTarget(access, target) {
		return nil, scope, ErrInvalidEventFilter
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, scope, err
	}
	if target.kind == "project" {
		err = tx.QueryRow(ctx, `SELECT p.id,p.organization_id FROM projects p
   JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
   WHERE p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[]))
   AND (p.slug=$5 OR p.id=$6) ORDER BY (p.slug=$5) DESC LIMIT 1`, access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs, target.reference, referenceID(target.reference)).Scan(&scope.projectID, &scope.organizationID)
	} else {
		err = tx.QueryRow(ctx, `SELECT p.id,p.organization_id,COALESCE(i.merged_into,i.id) FROM issues i
   JOIN projects p ON p.id=i.project_id
   JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
   WHERE i.id=$2::bigint AND ($3::bigint=0 OR p.organization_id=$3)
   AND ($4 OR p.id=ANY($5::bigint[]))`, access.UserID, target.reference, referenceID(access.OrganizationID), access.AllProjects, access.ProjectIDs).Scan(&scope.projectID, &scope.organizationID, &scope.issueID)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, scope, dbError(err)
	}
	return tx, scope, nil
}

// CompatibleEvents returns at most Limit+1 rows; the extra row proves a next page.
func (s *Store) CompatibleEvents(ctx context.Context, access ManagementAccess, target CompatibleEventTarget, filter CompatibleEventFilter) ([]eventview.Record, error) {
	if filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 || filter.Offset > 100000000 || filter.Period < 0 || !validEventEnvironments(filter.Environments) || (filter.Start != nil && filter.End != nil && !filter.End.After(*filter.Start)) {
		return nil, ErrInvalidEventFilter
	}
	search, err := ParseEventSearch(filter.Query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEventFilter, err)
	}
	tx, scope, err := s.compatibleEventTransaction(ctx, access, target)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if filter.Period > 0 {
		var now time.Time
		if err = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&now); err != nil {
			return nil, err
		}
		start := now.Add(-filter.Period)
		filter.Start, filter.End = &start, &now
	}
	args, where := compatibleEventWhere(scope, filter.Environments)
	if filter.Start != nil {
		args = append(args, *filter.Start)
		where += fmt.Sprintf(" AND e.timestamp >= $%d", len(args))
	}
	if filter.End != nil {
		args = append(args, *filter.End)
		where += fmt.Sprintf(" AND e.timestamp < $%d", len(args))
	}
	issueTerms, eventTerms, err := issueSearchPredicates(search, &args)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEventFilter, err)
	}
	for _, predicate := range append(issueTerms, eventTerms...) {
		where += " AND " + predicate
	}
	where += " AND e.kind<>'transaction'"
	if target.kind == "project" {
		where += " AND e.kind<>'feedback'"
	}
	order := "e.timestamp DESC,e.id DESC"
	if filter.Sample {
		order = "md5(e.project_id::text||':'||e.event_id),e.id"
	}
	args = append(args, filter.Limit+1, filter.Offset)
	query := compatibleEventSelect + compatibleEventJoins + where + fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, len(args)-1, len(args))
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	result := []eventview.Record{}
	for rows.Next() {
		record, _, scanErr := scanCompatibleEvent(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		result = append(result, record)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func compatibleEventWhere(scope compatibleEventScope, environments []string) ([]any, string) {
	args := []any{scope.projectID}
	where := " WHERE e.project_id=$1"
	if scope.issueID != 0 {
		args = append(args, scope.issueID)
		where += fmt.Sprintf(" AND e.issue_id=$%d", len(args))
	}
	if len(environments) > 0 {
		args = append(args, environments)
		where += fmt.Sprintf(" AND e.environment=ANY($%d::text[])", len(args))
	}
	return args, where
}

func compatibleEventReference(value string, issue bool) (string, bool) {
	if issue && (value == "latest" || value == "oldest" || value == "recommended") {
		return value, true
	}
	if len(value) == 36 {
		for _, index := range []int{8, 13, 18, 23} {
			if value[index] != '-' {
				return "", false
			}
		}
		value = value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	}
	if len(value) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	return strings.ToLower(value), true
}

// CompatibleEvent also reads current release association and adjacent events
// before committing its authorization/topology snapshot. Project siblings include
// all event kinds; issue siblings belong to the resolved root.
func (s *Store) CompatibleEvent(ctx context.Context, access ManagementAccess, target CompatibleEventTarget, eventRef string, environments []string) (eventview.Record, error) {
	var empty eventview.Record
	ref, valid := compatibleEventReference(eventRef, target.kind == "issue")
	if !valid || !validEventEnvironments(environments) {
		return empty, ErrInvalidEventFilter
	}
	tx, scope, err := s.compatibleEventTransaction(ctx, access, target)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	args, where := compatibleEventWhere(scope, environments)
	order := "e.timestamp DESC,e.id DESC"
	switch ref {
	case "latest", "recommended":
	case "oldest":
		order = "e.timestamp,e.id"
	default:
		args = append(args, ref)
		where += fmt.Sprintf(" AND e.event_id=$%d", len(args))
	}
	record, rowID, err := scanCompatibleEvent(tx.QueryRow(ctx, compatibleEventSelect+compatibleEventJoins+where+" ORDER BY "+order+" LIMIT 1", args...))
	if err != nil {
		return empty, dbError(err)
	}
	record.SiblingsKnown = true
	args, where = compatibleEventWhere(scope, environments)
	args = append(args, record.Timestamp, rowID)
	for _, sibling := range []struct {
		destination       **string
		comparison, order string
	}{
		{&record.PreviousID, "<", "e.timestamp DESC,e.id DESC"},
		{&record.NextID, ">", "e.timestamp,e.id"},
	} {
		var id string
		err = tx.QueryRow(ctx, "SELECT e.event_id FROM events e"+where+fmt.Sprintf(" AND (e.timestamp,e.id)%s($%d::timestamptz,$%d::bigint) ORDER BY %s LIMIT 1", sibling.comparison, len(args)-1, len(args), sibling.order), args...).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return empty, err
		}
		if err == nil {
			*sibling.destination = &id
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return record, nil
}
