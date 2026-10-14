package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodsentry/internal/event"
)

type Issue struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Project     Project   `json:"project"`
	Title       string    `json:"title"`
	Level       string    `json:"level"`
	Platform    string    `json:"platform"`
	Status      string    `json:"status"`
	Count       int64     `json:"count"`
	FirstSeen   time.Time `json:"firstSeen"`
	LastSeen    time.Time `json:"lastSeen"`
	Environment string    `json:"environment"`
	Release     string    `json:"release"`
	ShortID     string    `json:"shortId"`
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
	ID          string    `json:"id"`
	Text        string    `json:"text"`
	DateCreated time.Time `json:"dateCreated"`
	User        User      `json:"user"`
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
		_, err = tx.Exec(ctx, `INSERT INTO issues(project_id,fingerprint,title,level,platform,first_seen,last_seen,environment,release) VALUES($1,$2,$3,$4,$5,$6,$6,$7,$8) ON CONFLICT(project_id,fingerprint) DO NOTHING`, projectID, ev.Fingerprint, ev.Title, ev.Level, ev.Platform, ev.Timestamp, ev.Environment, ev.Release)
		if err != nil {
			return "", false, err
		}
		var own string
		var target *string
		err = tx.QueryRow(ctx, "SELECT id::text,merged_into::text FROM issues WHERE project_id::text=$1 AND fingerprint=$2 FOR UPDATE", projectID, ev.Fingerprint).Scan(&own, &target)
		if err != nil {
			return "", false, err
		}
		originID = own
		issueID = own
		if target != nil {
			issueID = *target
		}
		_, err = tx.Exec(ctx, `UPDATE issues SET count=count+1,first_seen=LEAST(first_seen,$2),last_seen=GREATEST(last_seen,$2),status=CASE WHEN status='resolved' THEN 'unresolved' ELSE status END,resolved_at=CASE WHEN status='resolved' THEN NULL ELSE resolved_at END,title=CASE WHEN $2>=last_seen THEN $3 ELSE title END,environment=CASE WHEN $2>=last_seen THEN $4 ELSE environment END,release=CASE WHEN $2>=last_seen THEN $5 ELSE release END WHERE id=$1`, issueID, ev.Timestamp, ev.Title, ev.Environment, ev.Release)
		if err != nil {
			return "", false, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO events(project_id,event_id,issue_id,original_issue_id,kind,title,level,timestamp,environment,release,trace_id,transaction_name,duration,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, projectID, ev.ID, issueID, originID, ev.Type, ev.Title, ev.Level, ev.Timestamp, ev.Environment, ev.Release, ev.TraceID, ev.Transaction, ev.Duration, ev.Data)
	if err != nil {
		return "", false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", false, err
	}
	if issueID == nil {
		return "", true, nil
	}
	return issueID.(string), true, nil
}

const issueColumns = `i.id::text,p.id::text,p.slug,p.name,i.title,i.level,i.platform,i.status,i.count,i.first_seen,i.last_seen,i.environment,i.release`
const issueJoins = ` FROM issues i JOIN projects p ON p.id=i.project_id JOIN organizations o ON o.id=p.organization_id JOIN memberships m ON m.organization_id=o.id `

type scanner interface{ Scan(...any) error }

func scanIssue(row scanner) (Issue, error) {
	var i Issue
	err := row.Scan(&i.ID, &i.ProjectID, &i.Project.Slug, &i.Project.Name, &i.Title, &i.Level, &i.Platform, &i.Status, &i.Count, &i.FirstSeen, &i.LastSeen, &i.Environment, &i.Release)
	i.Project.ID = i.ProjectID
	i.ShortID = strings.ToUpper(i.Project.Slug) + "-" + i.ID
	return i, dbError(err)
}

func (s *Store) Issues(ctx context.Context, userID, orgSlug string, f IssueFilter) ([]Issue, int, error) {
	if _, err := s.Organization(ctx, userID, orgSlug); err != nil {
		return nil, 0, err
	}
	args := []any{userID, orgSlug}
	where := `WHERE m.user_id::text=$1 AND o.slug=$2 AND i.merged_into IS NULL `
	for _, filter := range []struct{ value, sql string }{{f.Project, "p.id::text"}, {f.Status, "i.status"}, {f.Environment, "i.environment"}, {f.Release, "i.release"}} {
		if filter.value != "" {
			args = append(args, filter.value)
			where += fmt.Sprintf("AND %s=$%d ", filter.sql, len(args))
		}
	}
	if f.Query != "" {
		args = append(args, "%"+f.Query+"%")
		where += fmt.Sprintf("AND (i.title ILIKE $%d OR EXISTS(SELECT 1 FROM events e WHERE e.issue_id=i.id AND e.data::text ILIKE $%d)) ", len(args), len(args))
	}
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*)"+issueJoins+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "i.last_seen DESC,i.id DESC"
	if f.Sort == "freq" {
		order = "i.count DESC,i.id DESC"
	}
	page := f.Page
	if page < 1 {
		page = 1
	}
	args = append(args, (page-1)*25)
	rows, err := s.pool.Query(ctx, "SELECT "+issueColumns+issueJoins+where+"ORDER BY "+order+fmt.Sprintf(" LIMIT 25 OFFSET $%d", len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, i)
	}
	return items, total, rows.Err()
}

func (s *Store) Issue(ctx context.Context, userID, id string) (Issue, error) {
	return scanIssue(s.pool.QueryRow(ctx, "SELECT "+issueColumns+issueJoins+"WHERE m.user_id::text=$1 AND i.id::text=$2", userID, id))
}

func (s *Store) Events(ctx context.Context, userID, issueID string) ([]EventSummary, error) {
	if _, err := s.Issue(ctx, userID, issueID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT event_id,timestamp,data FROM events WHERE issue_id::text=$1 ORDER BY timestamp DESC,id DESC LIMIT 50", issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []EventSummary{}
	for rows.Next() {
		var e EventSummary
		if err = rows.Scan(&e.ID, &e.DateCreated, &e.Data); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

func (s *Store) SetIssueStatus(ctx context.Context, userID, id, status string) (Issue, error) {
	i, err := s.Issue(ctx, userID, id)
	if err != nil {
		return i, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return i, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE issues SET status=$2,resolved_at=CASE WHEN $2='resolved' THEN now() ELSE NULL END WHERE id::text=$1", id, status); err != nil {
		return i, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO issue_activity(issue_id,user_id,action) VALUES($1,$2,$3)", id, userID, status); err != nil {
		return i, err
	}
	if err = tx.Commit(ctx); err != nil {
		return i, err
	}
	i.Status = status
	return i, nil
}

func (s *Store) Comments(ctx context.Context, userID, issueID string) ([]Comment, error) {
	if _, err := s.Issue(ctx, userID, issueID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT c.id::text,c.text,c.created_at,u.id::text,u.name,u.email FROM comments c JOIN users u ON u.id=c.user_id WHERE c.issue_id::text=$1 ORDER BY c.id", issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Comment{}
	for rows.Next() {
		var c Comment
		if err = rows.Scan(&c.ID, &c.Text, &c.DateCreated, &c.User.ID, &c.User.Name, &c.User.Email); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (s *Store) AddComment(ctx context.Context, userID, issueID, text string) (Comment, error) {
	var c Comment
	if _, err := s.Issue(ctx, userID, issueID); err != nil {
		return c, err
	}
	err := s.pool.QueryRow(ctx, "WITH added AS (INSERT INTO comments(issue_id,user_id,text) VALUES($1,$2,$3) RETURNING *) SELECT c.id::text,c.text,c.created_at,u.id::text,u.name,u.email FROM added c JOIN users u ON u.id=c.user_id", issueID, userID, text).Scan(&c.ID, &c.Text, &c.DateCreated, &c.User.ID, &c.User.Name, &c.User.Email)
	return c, err
}
