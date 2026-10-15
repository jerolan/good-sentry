package store

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func normalizedComment(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 10000 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", fmt.Errorf("%w: comment must contain 1 to 10000 UTF-8 bytes without NUL", ErrInvalidIssueChange)
	}
	return text, nil
}

func scanComment(row scanner, userID, role string) (Comment, error) {
	var c Comment
	err := row.Scan(&c.ID, &c.Text, &c.DateCreated, &c.UpdatedAt, &c.User.ID, &c.User.Name, &c.User.Email)
	c.CanEdit = c.User.ID == userID || role == "owner" || role == "admin"
	c.DateCreated = c.DateCreated.UTC()
	if c.UpdatedAt != nil {
		stamp := c.UpdatedAt.UTC()
		c.UpdatedAt = &stamp
	}
	return c, dbError(err)
}

const commentColumns = `c.id::text,c.text,c.created_at,c.updated_at,u.id::text,u.name,u.email`

func (s *Store) IssueComments(ctx context.Context, userID, id string, page int) ([]Comment, int, error) {
	offset, err := issuePage(page)
	if err != nil {
		return nil, 0, err
	}
	tx, issue, _, role, err := s.issueTransaction(ctx, userID, id, false)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	items, total, err := readIssueComments(ctx, tx, userID, role, issue.ID, offset)
	if err != nil {
		return nil, 0, err
	}
	return items, total, tx.Commit(ctx)
}

func readIssueComments(ctx context.Context, tx pgx.Tx, userID, role, id string, offset int) ([]Comment, int, error) {
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM comments WHERE issue_id IN (SELECT id FROM issues WHERE id=$1::bigint OR merged_into=$1::bigint)`, id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, "SELECT "+commentColumns+` FROM comments c JOIN users u ON u.id=c.user_id WHERE c.issue_id IN (SELECT id FROM issues WHERE id=$1::bigint OR merged_into=$1::bigint) ORDER BY c.id DESC LIMIT 25 OFFSET $2`, id, offset)
	if err != nil {
		return nil, 0, err
	}
	items := []Comment{}
	for rows.Next() {
		item, e := scanComment(rows, userID, role)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) AddComment(ctx context.Context, userID, id, text string) (Comment, error) {
	text, err := normalizedComment(text)
	if err != nil {
		return Comment{}, err
	}
	tx, _, _, role, err := s.issueTransaction(ctx, userID, id, true)
	if err != nil {
		return Comment{}, err
	}
	defer tx.Rollback(ctx)
	c, err := scanComment(tx.QueryRow(ctx, `WITH added AS (INSERT INTO comments(issue_id,user_id,text) VALUES($1::bigint,$2::bigint,$3) RETURNING *) SELECT `+commentColumns+` FROM added c JOIN users u ON u.id=c.user_id`, id, userID, text), userID, role)
	if err != nil {
		return c, err
	}
	if _, err = recordIssueActivity(ctx, tx, id, &userID, "comment_added", map[string]string{"commentID": c.ID}); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

func (s *Store) EditComment(ctx context.Context, userID, id, commentID, text string) (Comment, error) {
	text, err := normalizedComment(text)
	if err != nil {
		return Comment{}, err
	}
	tx, _, _, role, err := s.issueTransaction(ctx, userID, id, true)
	if err != nil {
		return Comment{}, err
	}
	defer tx.Rollback(ctx)
	c, err := scanComment(tx.QueryRow(ctx, "SELECT "+commentColumns+` FROM comments c JOIN users u ON u.id=c.user_id WHERE c.issue_id IN (SELECT id FROM issues WHERE id=$1::bigint OR merged_into=$1::bigint) AND c.id::text=$2 FOR UPDATE OF c`, id, commentID), userID, role)
	if err != nil {
		return c, err
	}
	if !c.CanEdit {
		return c, ErrForbidden
	}
	if c.Text != text {
		stamp, e := recordIssueActivity(ctx, tx, id, &userID, "comment_edited", map[string]string{"commentID": c.ID})
		if e != nil {
			return c, e
		}
		if _, err = tx.Exec(ctx, `UPDATE comments SET text=$2,updated_at=$3 WHERE id=$1::bigint`, c.ID, text, stamp); err != nil {
			return c, err
		}
		c.Text = text
		c.UpdatedAt = &stamp
	}
	return c, tx.Commit(ctx)
}

func (s *Store) DeleteComment(ctx context.Context, userID, id, commentID string) error {
	tx, _, _, role, err := s.issueTransaction(ctx, userID, id, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := scanComment(tx.QueryRow(ctx, "SELECT "+commentColumns+` FROM comments c JOIN users u ON u.id=c.user_id WHERE c.issue_id IN (SELECT id FROM issues WHERE id=$1::bigint OR merged_into=$1::bigint) AND c.id::text=$2 FOR UPDATE OF c`, id, commentID), userID, role)
	if err != nil {
		return err
	}
	if !c.CanEdit {
		return ErrForbidden
	}
	if _, err = tx.Exec(ctx, `DELETE FROM comments WHERE id=$1::bigint`, c.ID); err != nil {
		return err
	}
	if _, err = recordIssueActivity(ctx, tx, id, &userID, "comment_deleted", map[string]string{"commentID": c.ID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
