package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/event"

	"github.com/jackc/pgx/v5"
)

type Issue struct {
	ID          string           `json:"id"`
	ProjectID   string           `json:"projectId"`
	Project     Project          `json:"project"`
	Title       string           `json:"title"`
	Level       string           `json:"level"`
	Platform    string           `json:"platform"`
	Status      string           `json:"status"`
	Count       int64            `json:"count"`
	GroupSize   int              `json:"groupSize"`
	FirstSeen   time.Time        `json:"firstSeen"`
	LastSeen    time.Time        `json:"lastSeen"`
	Environment string           `json:"environment"`
	Release     string           `json:"release"`
	ShortID     string           `json:"shortId"`
	AssignedTo  *IssueActor      `json:"assignedTo"`
	ResolvedAt  *time.Time       `json:"resolvedAt"`
	RegressedAt *time.Time       `json:"regressedAt"`
	Ignore      *IssueIgnore     `json:"ignore"`
	Resolution  *IssueResolution `json:"resolution"`
}
type IssueFilter struct {
	Project, Query, Status, Environment, Release, Sort string
	Page                                               int
}
type EventSummary struct {
	ID          string          `json:"eventID"`
	DateCreated time.Time       `json:"dateCreated"`
	Data        json.RawMessage `json:"-"`
}
type Comment struct {
	ID          string     `json:"id"`
	Text        string     `json:"text"`
	DateCreated time.Time  `json:"dateCreated"`
	User        User       `json:"user"`
	UpdatedAt   *time.Time `json:"updatedAt"`
	CanEdit     bool       `json:"canEdit"`
}

func (s *Store) Ingest(ctx context.Context, projectID string, ev event.Event) (string, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	// Serialize only a retry identity. Different events group under row locks.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", projectID+":"+ev.ID); err != nil {
		return "", false, err
	}
	var present bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM events WHERE project_id::text=$1 AND event_id=$2)", projectID, ev.ID).Scan(&present); err != nil {
		return "", false, err
	}
	if present {
		var id string
		err = tx.QueryRow(ctx, "SELECT COALESCE(issue_id::text,'') FROM events WHERE project_id::text=$1 AND event_id=$2", projectID, ev.ID).Scan(&id)
		return id, false, err
	}
	var issueID any
	var originID any
	if ev.Type != "transaction" {
		if err = lockProjectIssues(ctx, tx, projectID, false); err != nil {
			return "", false, err
		}
		own, target, err := activeIssueHashOrigin(ctx, tx, projectID, ev)
		if err != nil {
			return "", false, err
		}
		originID = own
		issueID = own
		if target != nil {
			issueID = *target
		}
		var status string
		var receipts int64
		if err = tx.QueryRow(ctx, `SELECT status,receipt_count FROM issues WHERE id=$1 FOR UPDATE`, issueID).Scan(&status, &receipts); err != nil {
			return "", false, err
		}
		if status == "ignored" {
			if _, err = expireLockedIssueIgnore(ctx, tx, issueID.(string), 1, ev.ID); err != nil {
				return "", false, err
			}
		}
		if receipts == 0 {
			if _, err = recordIssueActivity(ctx, tx, issueID.(string), nil, "first_seen", map[string]string{"eventID": ev.ID}); err != nil {
				return "", false, err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE issues SET count=count+1,receipt_count=receipt_count+1,first_seen=CASE WHEN count=0 THEN $2 ELSE LEAST(first_seen,$2) END,last_seen=CASE WHEN count=0 THEN $2 ELSE GREATEST(last_seen,$2) END,title=CASE WHEN count=0 OR $2>=last_seen THEN $3 ELSE title END,environment=CASE WHEN count=0 OR $2>=last_seen THEN $4 ELSE environment END,release=CASE WHEN count=0 OR $2>=last_seen THEN $5 ELSE release END WHERE id=$1`, issueID, ev.Timestamp, ev.Title, ev.Environment, ev.Release)
		if err != nil {
			return "", false, err
		}
	}
	var eventRowID int64
	err = tx.QueryRow(ctx, `INSERT INTO events(project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, projectID, ev.ID, issueID, originID, ev.Type, ev.Title, ev.Level, ev.Timestamp, ev.Environment, ev.Release, ev.TraceID, ev.Transaction, ev.Duration, ev.Data).Scan(&eventRowID)
	if err != nil {
		return "", false, err
	}
	if ev.Type == "transaction" {
		if err = projectTransactionSpans(ctx, tx, eventRowID, ev.Data); err != nil {
			return "", false, err
		}
	}
	var alerts *errorAlertCapture
	if issueID != nil {
		alerts, err = prepareErrorAlerts(ctx, tx, projectID, ev.ID, issueID.(string))
		if err != nil {
			return "", false, err
		}
	}
	if err = observeRelease(ctx, tx, projectID, ev.Release); err != nil {
		return "", false, err
	}
	if issueID != nil {
		if err = settleIssueResolution(ctx, tx, issueID.(string), projectID, ev.ID, ev.Release); err != nil {
			return "", false, err
		}
		if err = alerts.enqueue(ctx, tx, ev.ID, issueID.(string)); err != nil {
			return "", false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", false, err
	}
	if issueID == nil {
		return "", true, nil
	}
	return issueID.(string), true, nil
}

const issueColumns = `i.id::text,p.id::text,p.slug,p.name,i.title,i.level,i.platform,i.status,i.count,i.first_seen,i.last_seen,i.environment,i.release,i.resolved_at,i.regressed_at,` + issueAssignmentJSON + "," + issueIgnoreJSON + "," + issueResolutionJSON + `,1+(SELECT count(*) FROM issues group_member WHERE group_member.merged_into=i.id)`
const issueJoins = ` FROM issues i JOIN projects p ON p.id=i.project_id JOIN organizations o ON o.id=p.organization_id JOIN memberships m ON m.organization_id=o.id `

type scanner interface{ Scan(...any) error }

func scanIssue(row scanner, additional ...any) (Issue, error) {
	var i Issue
	var assigned, ignored, resolution json.RawMessage
	destinations := []any{&i.ID, &i.ProjectID, &i.Project.Slug, &i.Project.Name, &i.Title, &i.Level, &i.Platform, &i.Status, &i.Count, &i.FirstSeen, &i.LastSeen, &i.Environment, &i.Release, &i.ResolvedAt, &i.RegressedAt, &assigned, &ignored, &resolution, &i.GroupSize}
	err := row.Scan(append(destinations, additional...)...)
	if err == nil && len(assigned) > 0 {
		err = json.Unmarshal(assigned, &i.AssignedTo)
	}
	if err == nil && len(ignored) > 0 {
		err = json.Unmarshal(ignored, &i.Ignore)
	}
	if err == nil && len(resolution) > 0 {
		err = json.Unmarshal(resolution, &i.Resolution)
	}
	if i.Resolution != nil {
		for _, reference := range []*IssueResolutionRelease{i.Resolution.Release, i.Resolution.Baseline} {
			if reference != nil {
				reference.DateCreated = reference.DateCreated.UTC()
				reference.DateReleased = releaseTimeUTC(reference.DateReleased)
			}
		}
	}
	if i.Ignore != nil && i.Ignore.Until != nil {
		stamp := i.Ignore.Until.UTC()
		i.Ignore.Until = &stamp
	}
	if i.ResolvedAt != nil {
		stamp := i.ResolvedAt.UTC()
		i.ResolvedAt = &stamp
	}
	if i.RegressedAt != nil {
		stamp := i.RegressedAt.UTC()
		i.RegressedAt = &stamp
	}
	i.Project.ID = i.ProjectID
	i.ShortID = strings.ToUpper(i.Project.Slug) + "-" + i.ID
	return i, dbError(err)
}

func (s *Store) Issues(ctx context.Context, userID, orgSlug string, f IssueFilter) ([]Issue, int, error) {
	search, err := validateIssueFilter(f)
	if err != nil {
		return nil, 0, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	var orgID string
	if err := tx.QueryRow(ctx, `SELECT o.id::text FROM organizations o JOIN memberships m ON m.organization_id=o.id
     WHERE m.user_id::text=$1 AND o.slug=$2`, userID, orgSlug).Scan(&orgID); err != nil {
		return nil, 0, dbError(err)
	}
	if f.Project != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1::bigint AND organization_id=$2::bigint)`, f.Project, orgID).Scan(&exists); err != nil {
			return nil, 0, err
		}
		if !exists {
			return nil, 0, ErrNotFound
		}
	}
	args := []any{userID, orgSlug}
	where := `WHERE m.user_id::text=$1 AND o.slug=$2 AND i.merged_into IS NULL `
	bind := func(value string) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if f.Project != "" {
		where += "AND p.id=" + bind(f.Project) + "::bigint "
	}
	if f.Status != "" && f.Status != "all" {
		where += "AND i.status=" + bind(f.Status) + " "
	}
	// Every event predicate is conjoined within one occurrence. Lifetime issue
	// summaries remain independent of which historical occurrence matched.
	eventPredicates := []string{}
	if f.Environment != "" {
		eventPredicates = append(eventPredicates, "e.environment="+bind(f.Environment))
	}
	if f.Release != "" {
		eventPredicates = append(eventPredicates, "e.release="+bind(f.Release))
	}
	issuePredicates, searchedEvents, err := issueSearchPredicates(search, &args)
	if err != nil {
		return nil, 0, err
	}
	for _, predicate := range issuePredicates {
		where += "AND " + predicate + " "
	}
	eventPredicates = append(eventPredicates, searchedEvents...)
	if len(eventPredicates) > 0 {
		where += "AND EXISTS(SELECT 1 FROM events e WHERE e.issue_id=i.id AND " + strings.Join(eventPredicates, " AND ") + ") "
	}
	var total int
	if err := tx.QueryRow(ctx, "SELECT count(*)"+issueJoins+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "i.last_seen DESC,i.id DESC"
	switch f.Sort {
	case "freq":
		order = "i.count DESC,i.id DESC"
	case "new":
		order = "i.first_seen DESC,i.id DESC"
	}
	page := f.Page
	if page == 0 {
		page = 1
	}
	args = append(args, (page-1)*25)
	rows, err := tx.Query(ctx, "SELECT "+issueColumns+issueJoins+where+"ORDER BY "+order+fmt.Sprintf(" LIMIT 25 OFFSET $%d", len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	items := []Issue{}
	for rows.Next() {
		item, err := scanIssue(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func validateIssueFilter(f IssueFilter) (IssueSearch, error) {
	for _, value := range []string{f.Project, f.Query, f.Status, f.Environment, f.Release, f.Sort} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return IssueSearch{}, fmt.Errorf("%w: filters must be valid UTF-8 without NUL", ErrInvalidIssueFilter)
		}
	}
	if f.Project != "" {
		id, err := strconv.ParseInt(f.Project, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != f.Project {
			return IssueSearch{}, fmt.Errorf("%w: project must be a canonical positive int64 ID", ErrInvalidIssueFilter)
		}
	}
	switch f.Status {
	case "", "all", "unresolved", "resolved", "ignored":
	default:
		return IssueSearch{}, fmt.Errorf("%w: invalid issue status", ErrInvalidIssueFilter)
	}
	switch f.Sort {
	case "", "date", "freq", "new":
	default:
		return IssueSearch{}, fmt.Errorf("%w: invalid issue sort", ErrInvalidIssueFilter)
	}
	if f.Page < 0 || f.Page > 1000000 {
		return IssueSearch{}, fmt.Errorf("%w: page must be between 1 and 1000000", ErrInvalidIssueFilter)
	}
	if len(f.Query) > 4096 || utf8.RuneCountInString(f.Environment) > 128 || utf8.RuneCountInString(f.Release) > 255 {
		return IssueSearch{}, fmt.Errorf("%w: issue filter is too long", ErrInvalidIssueFilter)
	}
	return ParseIssueSearch(f.Query)
}

func issueSearchValues(expression string, values []IssueSearchValue, args *[]any) string {
	predicates := make([]string, 0, len(values))
	for _, value := range values {
		operator := "="
		input := value.Value
		if value.Wildcard {
			operator = "LIKE"
			input = issueSearchPattern(input, true)
		}
		*args = append(*args, input)
		predicate := fmt.Sprintf("%s %s $%d", expression, operator, len(*args))
		if value.Wildcard {
			predicate += ` ESCAPE E'\\'`
		}
		predicates = append(predicates, predicate)
	}
	return "(" + strings.Join(predicates, " OR ") + ")"
}

func issueSearchPattern(value string, wildcard bool) string {
	value = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(value)
	if wildcard {
		value = strings.ReplaceAll(value, "*", "%")
	}
	return value
}
