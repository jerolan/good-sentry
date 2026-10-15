package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/eventview"
)

var ErrInvalidCompatibleIssueFilter = errors.New("invalid compatible issue filter")

type IssueAnalytics struct {
	UserCount int64                 `json:"userCount"`
	Stats     map[string][][2]int64 `json:"stats"`
}

type CompatibleIssueMetrics struct {
	Count, UserCount                     int64
	FirstSeen, LastSeen                  *time.Time
	MatchingEventID, MatchingEnvironment *string
	IsUnhandled                          bool
	Stats                                map[string][][2]int64
}

type CompatibleIssueRecord struct {
	Issue                             Issue
	OrganizationSlug, ProjectPlatform string
	NumComments, UserCount            int64
	IsUnhandled                       bool
	Latest                            *eventview.Record
	Filtered                          *CompatibleIssueMetrics
	Stats                             map[string][][2]int64
	FirstRelease, LastRelease         *eventview.ReleaseReference
}

type CompatibleIssueFilter struct {
	IDs, ProjectRefs, Environments []string
	Query, Sort, StatsPeriod       string
	Start, End                     *time.Time
	Period                         time.Duration
	Offset, Limit                  int
	ShortIDLookup                  bool
}

type CompatibleIssueDetailFilter struct {
	Environments []string
	StatsPeriod  string
}

type CompatibleIssueStats struct {
	ID                  string
	Count, UserCount    int64
	FirstSeen, LastSeen time.Time
	IsUnhandled         bool
	Stats               map[string][][2]int64
}

func validIssueIDs(ids []string) bool {
	if len(ids) > 100 {
		return false
	}
	for _, id := range ids {
		if referenceID(id) == 0 {
			return false
		}
	}
	return true
}

func issueSort(value string) (string, bool) {
	switch value {
	case "", "date":
		return "i.last_seen DESC,i.id DESC", true
	case "new":
		return "i.first_seen DESC,i.id DESC", true
	case "freq":
		return "i.count DESC,i.id DESC", true
	case "user":
		return "(SELECT count(DISTINCT " + issueUserIdentity + ") FROM events e WHERE e.issue_id=i.id) DESC,i.id DESC", true
	}
	direction := " ASC"
	if strings.HasPrefix(value, "-") {
		direction, value = " DESC", value[1:]
	}
	column := map[string]string{"last_seen": "i.last_seen", "first_seen": "i.first_seen", "count": "i.count", "priority": `CASE i.level WHEN 'sample' THEN 0 WHEN 'debug' THEN 1 WHEN 'info' THEN 2 WHEN 'warning' THEN 3 WHEN 'error' THEN 4 WHEN 'fatal' THEN 5 ELSE -1 END`}[value]
	return column + direction + ",i.id" + direction, column != ""
}

// Organization membership and project grants are evaluated in the same snapshot
// as issue ownership and metrics. Project/detail targets share the event reader's boundary.
func (s *Store) compatibleIssueOrganization(ctx context.Context, access ManagementAccess) (pgx.Tx, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 {
		return nil, ErrInvalidCompatibleIssueFilter
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	var member bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint)`, access.UserID, access.OrganizationID).Scan(&member)
	if err != nil || !member {
		_ = tx.Rollback(ctx)
		if err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	return tx, nil
}

func compatibleIssueTargetError(err error) error {
	if errors.Is(err, ErrInvalidEventFilter) {
		return ErrInvalidCompatibleIssueFilter
	}
	return err
}

const compatibleIssueExtras = `,o.slug,p.platform,(SELECT count(*) FROM comments c WHERE c.issue_id=i.id OR c.issue_id IN(SELECT member.id FROM issues member WHERE member.merged_into=i.id))`

func scanCompatibleIssue(row scanner) (CompatibleIssueRecord, error) {
	var record CompatibleIssueRecord
	var err error
	record.Issue, err = scanIssue(row, &record.OrganizationSlug, &record.ProjectPlatform, &record.NumComments)
	return record, err
}

func (s *Store) CompatibleIssues(ctx context.Context, access ManagementAccess, projectRef string, filter CompatibleIssueFilter) ([]CompatibleIssueRecord, error) {
	order, validSort := issueSort(filter.Sort)
	if !validSort || filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 || filter.Offset > 100000000 || filter.Period < 0 || !validIssueIDs(filter.IDs) || len(filter.ProjectRefs) > 100 || !validEventEnvironments(filter.Environments) || !validIssueChart(filter.StatsPeriod, true) || (filter.Start != nil && filter.End != nil && !filter.End.After(*filter.Start)) {
		return nil, ErrInvalidCompatibleIssueFilter
	}
	for _, ref := range filter.ProjectRefs {
		if ref == "" || !utf8.ValidString(ref) || strings.ContainsRune(ref, 0) || (ref == "-1" && len(filter.ProjectRefs) != 1) {
			return nil, ErrInvalidCompatibleIssueFilter
		}
	}
	query, shortProject, shortID := filter.Query, "", int64(0)
	if filter.ShortIDLookup {
		if pos := strings.LastIndexByte(query, '-'); pos > 0 && !strings.ContainsAny(query, " \t\r\n") && referenceID(query[pos+1:]) > 0 {
			shortProject, shortID, query = query[:pos], referenceID(query[pos+1:]), ""
		}
	}
	search, err := ParseEventSearch(query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCompatibleIssueFilter, err)
	}
	var tx pgx.Tx
	var scope compatibleEventScope
	if projectRef != "" {
		tx, scope, err = s.compatibleEventTransaction(ctx, access, EventProjectTarget(projectRef))
	} else {
		tx, err = s.compatibleIssueOrganization(ctx, access)
	}
	if err != nil {
		return nil, compatibleIssueTargetError(err)
	}
	defer tx.Rollback(ctx)
	occurrence, end, chart, err := prepareIssueMetrics(ctx, tx, filter, search)
	if err != nil {
		return nil, err
	}
	args := []any{access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs}
	where := ` WHERE m.user_id=$1::bigint AND o.id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[])) AND i.merged_into IS NULL`
	bind := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if scope.projectID > 0 {
		where += " AND p.id=" + bind(scope.projectID)
	}
	if len(filter.ProjectRefs) > 0 && filter.ProjectRefs[0] != "-1" {
		projects, err := releaseProjectReferences(ctx, tx, access, filter.ProjectRefs)
		if err != nil {
			return nil, err
		}
		where += " AND p.id=ANY(" + bind(projects) + "::bigint[])"
	}
	if len(filter.IDs) > 0 {
		where += ` AND i.id IN(SELECT COALESCE(alias.merged_into,alias.id) FROM issues alias WHERE alias.project_id=i.project_id AND alias.id=ANY(` + bind(issueNumericIDs(filter.IDs)) + `::bigint[]))`
	}
	if shortID > 0 {
		where += ` AND upper(p.slug)=upper(` + bind(shortProject) + `) AND i.id IN(SELECT COALESCE(alias.merged_into,alias.id) FROM issues alias WHERE alias.project_id=i.project_id AND alias.id=` + bind(shortID) + `)`
	}
	issueTerms, _, err := issueSearchPredicates(issueStatusSearch(search), &args)
	if err != nil {
		return nil, err
	}
	for _, term := range issueTerms {
		where += " AND " + term
	}
	terms, err := occurrence.predicates(&args)
	if err != nil {
		return nil, err
	}
	if len(terms) > 0 {
		where += ` AND EXISTS(SELECT 1 FROM events e WHERE e.issue_id=i.id AND ` + strings.Join(terms, " AND ") + `)`
	}
	limit, offset := bind(filter.Limit+1), bind(filter.Offset)
	rows, err := tx.Query(ctx, "SELECT "+issueColumns+compatibleIssueExtras+issueJoins+where+" ORDER BY "+order+" LIMIT "+limit+" OFFSET "+offset, args...)
	if err != nil {
		return nil, err
	}
	records := []CompatibleIssueRecord{}
	for rows.Next() {
		record, err := scanCompatibleIssue(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		records = append(records, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = readCompatibleIssueMetrics(ctx, tx, records, occurrence, end, chart); err != nil {
		return nil, err
	}
	return records, tx.Commit(ctx)
}

func (s *Store) CompatibleIssue(ctx context.Context, access ManagementAccess, issueID string, filter CompatibleIssueDetailFilter) (CompatibleIssueRecord, error) {
	var record CompatibleIssueRecord
	if !validEventEnvironments(filter.Environments) || !validIssueChart(filter.StatsPeriod, false) {
		return record, ErrInvalidCompatibleIssueFilter
	}
	tx, scope, err := s.compatibleEventTransaction(ctx, access, EventIssueTarget(issueID))
	if err != nil {
		return record, compatibleIssueTargetError(err)
	}
	defer tx.Rollback(ctx)
	record, err = readCompatibleIssueRecord(ctx, tx, access.UserID, fmt.Sprint(scope.issueID), filter)
	if err != nil {
		return record, err
	}
	return record, tx.Commit(ctx)
}

func readCompatibleIssueRecord(ctx context.Context, tx pgx.Tx, userID, issueID string, filter CompatibleIssueDetailFilter) (CompatibleIssueRecord, error) {
	record, err := scanCompatibleIssue(tx.QueryRow(ctx, "SELECT "+issueColumns+compatibleIssueExtras+issueJoins+` WHERE m.user_id=$1::bigint AND i.id=$2::bigint`, userID, issueID))
	if err != nil {
		return record, err
	}
	occurrence, end, chart, err := prepareIssueMetrics(ctx, tx, CompatibleIssueFilter{Environments: filter.Environments, StatsPeriod: filter.StatsPeriod}, IssueSearch{})
	if err != nil {
		return record, err
	}
	records := []CompatibleIssueRecord{record}
	if err = readCompatibleIssueMetrics(ctx, tx, records, occurrence, end, chart); err != nil {
		return record, err
	}
	return records[0], nil
}

func (s *Store) CompatibleIssueStats(ctx context.Context, access ManagementAccess, ids []string, period string) ([]CompatibleIssueStats, error) {
	if len(ids) == 0 || !validIssueIDs(ids) || (period != "24h" && period != "14d") {
		return nil, ErrInvalidCompatibleIssueFilter
	}
	tx, err := s.compatibleIssueOrganization(ctx, access)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON(COALESCE(alias.merged_into,alias.id)) COALESCE(alias.merged_into,alias.id)::text,root.count,root.first_seen,root.last_seen
 FROM unnest($1::bigint[]) WITH ORDINALITY requested(id,position) JOIN issues alias ON alias.id=requested.id
 JOIN issues root ON root.id=COALESCE(alias.merged_into,alias.id) JOIN projects p ON p.id=root.project_id
 WHERE p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[]))
	 ORDER BY COALESCE(alias.merged_into,alias.id),requested.position`, issueNumericIDs(ids), access.OrganizationID, access.AllProjects, access.ProjectIDs)
	if err != nil {
		return nil, err
	}
	byID := map[string]CompatibleIssueStats{}
	for rows.Next() {
		var item CompatibleIssueStats
		if err = rows.Scan(&item.ID, &item.Count, &item.FirstSeen, &item.LastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		byID[item.ID] = item
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// The bounded requested-order mapping includes aliases while never exposing hidden roots.
	orderRows, err := tx.Query(ctx, `SELECT COALESCE(i.merged_into,i.id)::text FROM unnest($1::bigint[]) WITH ORDINALITY requested(id,position) JOIN issues i ON i.id=requested.id ORDER BY requested.position`, issueNumericIDs(ids))
	if err != nil {
		return nil, err
	}
	ordered := []string{}
	seen := map[string]bool{}
	for orderRows.Next() {
		var id string
		if err = orderRows.Scan(&id); err != nil {
			orderRows.Close()
			return nil, err
		}
		if _, ok := byID[id]; ok && !seen[id] {
			ordered = append(ordered, id)
			seen[id] = true
		}
	}
	err = orderRows.Err()
	orderRows.Close()
	if err != nil {
		return nil, err
	}
	_, end, _, err := prepareIssueMetrics(ctx, tx, CompatibleIssueFilter{StatsPeriod: period}, IssueSearch{})
	if err != nil {
		return nil, err
	}
	metrics, err := readIssueMetrics(ctx, tx, ordered, issueOccurrenceFilter{}, end, period)
	if err != nil {
		return nil, err
	}
	result := make([]CompatibleIssueStats, 0, len(ordered))
	for _, id := range ordered {
		item := byID[id]
		item.FirstSeen = item.FirstSeen.UTC()
		item.LastSeen = item.LastSeen.UTC()
		item.UserCount = metrics[id].lifetime.UserCount
		item.IsUnhandled = metrics[id].lifetime.IsUnhandled
		item.Stats = metrics[id].lifetime.Stats
		result = append(result, item)
	}
	return result, tx.Commit(ctx)
}
