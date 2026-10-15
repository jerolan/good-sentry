package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidIssueChange = errors.New("invalid issue change")

type IssueActorRef struct{ Type, ID string }
type IssueAssignmentChange struct{ Target *IssueActorRef }
type IssueUpdate struct {
	Status     string
	Assignment *IssueAssignmentChange
	Ignore     *IssueIgnoreChange
	Resolution *IssueResolutionChange
}
type IssueActor struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Slug  string `json:"slug,omitempty"`
}
type IssueActivity struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Data        json.RawMessage `json:"data"`
	DateCreated time.Time       `json:"dateCreated"`
	User        *User           `json:"user"`
}

const issueAssignmentJSON = `(SELECT jsonb_strip_nulls(jsonb_build_object('type',CASE WHEN a.user_id IS NOT NULL THEN 'user' ELSE 'team' END,
 'id',COALESCE(a.user_id,a.team_id)::text,'name',COALESCE(u.name,t.name),'email',u.email,'slug',t.slug))
 FROM issue_assignments a LEFT JOIN users u ON u.id=a.user_id LEFT JOIN teams t ON t.id=a.team_id WHERE a.issue_id=i.id)`

// Authorization and mutable state are locked in the same transaction.
func (s *Store) issueTransaction(ctx context.Context, userID, id string, write bool) (pgx.Tx, Issue, string, string, error) {
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if write {
		options = pgx.TxOptions{}
	}
	tx, err := s.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, Issue{}, "", "", err
	}
	var org, role string
	authorize := `SELECT p.organization_id::text,m.role FROM issues i JOIN projects p ON p.id=i.project_id JOIN memberships m ON m.organization_id=p.organization_id WHERE i.id::text=$1 AND m.user_id::text=$2`
	if write {
		authorize += " FOR SHARE OF m"
	}
	if err = tx.QueryRow(ctx, authorize, id, userID).Scan(&org, &role); err != nil {
		_ = tx.Rollback(ctx)
		return nil, Issue{}, "", "", dbError(err)
	}
	if write {
		var locked string
		if err = tx.QueryRow(ctx, `SELECT id::text FROM issues WHERE id::text=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
			_ = tx.Rollback(ctx)
			return nil, Issue{}, "", "", dbError(err)
		}
	}
	// A fresh statement after a possible row-lock wait observes the assignment
	// committed by the preceding writer, including its related table row.
	i, err := scanIssue(tx.QueryRow(ctx, "SELECT "+issueColumns+issueJoins+"WHERE m.user_id::text=$1 AND i.id::text=$2", userID, id))
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, i, "", "", err
	}
	return tx, i, org, role, nil
}

func recordIssueActivity(ctx context.Context, tx pgx.Tx, id string, userID *string, action string, data any) (time.Time, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return time.Time{}, err
	}
	var stamp time.Time
	err = tx.QueryRow(ctx, `INSERT INTO issue_activity(issue_id,user_id,action,data) VALUES($1::bigint,$2::bigint,$3,$4) RETURNING created_at`, id, userID, action, raw).Scan(&stamp)
	return stamp.UTC(), err
}

func canonicalIssueID(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}

func (s *Store) UpdateIssue(ctx context.Context, userID, id string, change IssueUpdate) (Issue, error) {
	if change.Status == "" && change.Assignment == nil || !validIssueIgnore(change.Ignore, change.Status) || !validIssueResolution(change.Resolution, change.Status) {
		return Issue{}, ErrInvalidIssueChange
	}
	if change.Status != "" && change.Status != "unresolved" && change.Status != "resolved" && change.Status != "ignored" {
		return Issue{}, ErrInvalidIssueChange
	}
	if a := change.Assignment; a != nil && a.Target != nil && (!canonicalIssueID(a.Target.ID) || a.Target.Type != "user" && a.Target.Type != "team") {
		return Issue{}, ErrInvalidIssueChange
	}
	tx, i, org, _, err := s.issueTransaction(ctx, userID, id, true)
	if err != nil {
		return i, err
	}
	defer tx.Rollback(ctx)
	var target *IssueActor
	if a := change.Assignment; a != nil && a.Target != nil {
		target = &IssueActor{Type: a.Target.Type, ID: a.Target.ID}
		if target.Type == "user" {
			err = tx.QueryRow(ctx, `SELECT u.name,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1::bigint AND m.user_id=$2::bigint FOR SHARE OF m,u`, org, target.ID).Scan(&target.Name, &target.Email)
		} else {
			err = tx.QueryRow(ctx, `SELECT name,slug FROM teams WHERE organization_id=$1::bigint AND id=$2::bigint FOR SHARE`, org, target.ID).Scan(&target.Name, &target.Slug)
		}
		if err != nil {
			return i, dbError(err)
		}
	}
	resolution, resolutionChanged, err := prepareIssueResolution(ctx, tx, i, org, change)
	if err != nil {
		return i, err
	}
	if i.Status == "ignored" {
		expired, err := expireLockedIssueIgnore(ctx, tx, id, 0, "")
		if err != nil {
			return i, err
		}
		if expired {
			i.Status, i.Ignore, i.ResolvedAt, i.RegressedAt = "unresolved", nil, nil, nil
		}
	}
	var wantedIgnore *IssueIgnoreCondition
	if change.Status == "ignored" {
		if change.Ignore != nil {
			wantedIgnore = change.Ignore.Condition
		} else if i.Status == "ignored" && i.Ignore != nil {
			wantedIgnore = &IssueIgnoreCondition{Minutes: i.Ignore.Minutes, Count: i.Ignore.Count}
		}
	}
	ignoreChanged := change.Status == "ignored" && !sameIssueIgnore(i.Ignore, wantedIgnore)
	if change.Status != "" && (change.Status != i.Status || ignoreChanged || resolutionChanged) {
		var data any = map[string]string{"from": i.Status, "to": change.Status}
		if err := replaceIssueIgnore(ctx, tx, &i, wantedIgnore); err != nil {
			return i, err
		}
		if change.Status == "ignored" {
			data = map[string]any{"from": i.Status, "to": change.Status, "ignore": i.Ignore}
		}
		if change.Status == "resolved" && (resolution != nil || resolutionChanged) {
			data = map[string]any{"from": i.Status, "to": change.Status, "resolution": resolution.public()}
		}
		stamp, e := recordIssueActivity(ctx, tx, id, &userID, change.Status, data)
		if e != nil {
			return i, e
		}
		if err = replaceIssueResolution(ctx, tx, id, resolution, stamp); err != nil {
			return i, err
		}
		i.Resolution = resolution.public()
		i.Status = change.Status
		i.RegressedAt = nil
		i.ResolvedAt = nil
		if change.Status == "resolved" {
			i.ResolvedAt = &stamp
		}
		_, err = tx.Exec(ctx, `UPDATE issues SET status=$2,resolved_at=$3,regressed_at=NULL WHERE id=$1::bigint`, id, i.Status, i.ResolvedAt)
		if err != nil {
			return i, err
		}
	}
	if change.Assignment != nil && !sameIssueActor(i.AssignedTo, target) {
		action := "assigned"
		if target == nil {
			action = "unassigned"
		}
		if _, err = recordIssueActivity(ctx, tx, id, &userID, action, map[string]any{"from": i.AssignedTo, "to": target}); err != nil {
			return i, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM issue_assignments WHERE issue_id=$1::bigint`, id); err != nil {
			return i, err
		}
		if target != nil {
			var user, team *string
			if target.Type == "user" {
				user = &target.ID
			} else {
				team = &target.ID
			}
			_, err = tx.Exec(ctx, `INSERT INTO issue_assignments(issue_id,organization_id,user_id,team_id) VALUES($1::bigint,$2::bigint,$3::bigint,$4::bigint)`, id, org, user, team)
			if err != nil {
				return i, err
			}
		}
		i.AssignedTo = target
	}
	if err = tx.Commit(ctx); err != nil {
		return i, err
	}
	return i, nil
}

func sameIssueActor(a, b *IssueActor) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Type == b.Type && a.ID == b.ID
}

func issuePage(page int) (int, error) {
	if page < 1 || page > 1000000 {
		return 0, fmt.Errorf("%w: page must be between 1 and 1000000", ErrInvalidIssueChange)
	}
	return (page - 1) * 25, nil
}

func (s *Store) IssueAssignees(ctx context.Context, userID, id, query string, page int) ([]IssueActor, int, error) {
	offset, err := issuePage(page)
	if err != nil {
		return nil, 0, err
	}
	if len(query) > 200 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
		return nil, 0, ErrInvalidIssueChange
	}
	tx, _, org, _, err := s.issueTransaction(ctx, userID, id, false)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	pattern := "%" + issueSearchPattern(query, false) + "%"
	from := ` FROM (SELECT 'user' AS type,u.id,u.name,u.email,'' AS slug FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1::bigint
 UNION ALL SELECT 'team',id,name,'',slug FROM teams WHERE organization_id=$1::bigint) a
 WHERE (name ILIKE $2 ESCAPE E'\\' OR email ILIKE $2 ESCAPE E'\\' OR slug ILIKE $2 ESCAPE E'\\') `
	var total int
	if err = tx.QueryRow(ctx, "SELECT count(*)"+from, org, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, "SELECT type,id::text,name,email,slug"+from+"ORDER BY a.type,a.id LIMIT 25 OFFSET $3", org, pattern, offset)
	if err != nil {
		return nil, 0, err
	}
	items := []IssueActor{}
	for rows.Next() {
		var item IssueActor
		if err = rows.Scan(&item.Type, &item.ID, &item.Name, &item.Email, &item.Slug); err != nil {
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
	return items, total, tx.Commit(ctx)
}

func (s *Store) IssueActivities(ctx context.Context, userID, id string, page int) ([]IssueActivity, int, error) {
	offset, err := issuePage(page)
	if err != nil {
		return nil, 0, err
	}
	tx, _, _, _, err := s.issueTransaction(ctx, userID, id, false)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	var total int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM issue_activity WHERE issue_id=$1::bigint`, id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT a.id::text,a.action,a.data,a.created_at,u.id::text,u.name,u.email FROM issue_activity a LEFT JOIN users u ON u.id=a.user_id WHERE issue_id=$1::bigint ORDER BY a.id DESC LIMIT 25 OFFSET $2`, id, offset)
	if err != nil {
		return nil, 0, err
	}
	items := []IssueActivity{}
	for rows.Next() {
		var item IssueActivity
		var uid, name, email *string
		if err = rows.Scan(&item.ID, &item.Type, &item.Data, &item.DateCreated, &uid, &name, &email); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if uid != nil {
			item.User = &User{ID: *uid, Name: *name, Email: *email}
		}
		item.DateCreated = item.DateCreated.UTC()
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return items, total, tx.Commit(ctx)
}
