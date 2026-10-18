package store

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/jackc/pgx/v5"
)

const attachmentMetadataColumns = `a.id,left(a.filename,2048),left(a.content_type,256),octet_length(a.data),a.created_at,
 (char_length(a.filename)>2048 OR char_length(a.content_type)>256)`

func scanAttachment(row pgx.Row, withData bool) (Attachment, error) {
	var file Attachment
	fields := []any{&file.ID, &file.Filename, &file.ContentType, &file.Size, &file.DateCreated, &file.MetadataTruncated}
	if withData {
		fields = append(fields, &file.Data)
	}
	err := row.Scan(fields...)
	file.DateCreated = file.DateCreated.UTC()
	return file, dbError(err)
}

// Scope, parent existence and selected bytes share one authorization/topology
// snapshot. A second unscoped blob fetch would break that containment boundary.
func (s *Store) attachmentEventTransaction(ctx context.Context, userID, issueID, eventID string) (pgx.Tx, compatibleEventScope, string, error) {
	ref, valid := compatibleEventReference(eventID, false)
	if !valid {
		return nil, compatibleEventScope{}, "", ErrInvalidEventFilter
	}
	tx, scope, err := s.compatibleEventTransaction(ctx, ManagementAccess{UserID: userID, AllProjects: true, ReadOnly: true}, EventIssueTarget(issueID))
	if err != nil {
		return nil, scope, "", err
	}
	if err := tx.QueryRow(ctx, `SELECT e.event_id FROM events e WHERE e.project_id=$1 AND e.issue_id=$2 AND e.event_id=$3`, scope.projectID, scope.issueID, ref).Scan(&ref); err != nil {
		_ = tx.Rollback(ctx)
		return nil, scope, "", dbError(err)
	}
	return tx, scope, ref, nil
}

func (s *Store) IssueEventAttachments(ctx context.Context, userID, issueID, eventID string, page int) ([]Attachment, bool, error) {
	if page < 1 || page > 1000000 {
		return nil, false, ErrInvalidEventFilter
	}
	tx, scope, ref, err := s.attachmentEventTransaction(ctx, userID, issueID, eventID)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT `+attachmentMetadataColumns+` FROM attachments a
 WHERE a.project_id=$1 AND a.event_id=$2 ORDER BY a.created_at,a.id LIMIT 26 OFFSET $3`, scope.projectID, ref, (page-1)*25)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	files := make([]Attachment, 0, 26)
	for rows.Next() {
		file, err := scanAttachment(rows, false)
		if err != nil {
			return nil, false, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(files) > 25
	if more {
		files = files[:25]
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return files, more, nil
}

func (s *Store) IssueEventAttachment(ctx context.Context, userID, issueID, eventID, attachmentID string, withData bool) (Attachment, error) {
	if len(attachmentID) != 64 {
		return Attachment{}, ErrInvalidEventFilter
	}
	if _, err := hex.DecodeString(attachmentID); err != nil {
		return Attachment{}, ErrInvalidEventFilter
	}
	tx, scope, ref, err := s.attachmentEventTransaction(ctx, userID, issueID, eventID)
	if err != nil {
		return Attachment{}, err
	}
	defer tx.Rollback(ctx)
	columns := attachmentMetadataColumns
	if withData {
		columns += ",a.data"
	}
	file, err := scanAttachment(tx.QueryRow(ctx, "SELECT "+columns+` FROM attachments a
 WHERE a.project_id=$1 AND a.event_id=$2 AND a.id=$3`, scope.projectID, ref, strings.ToLower(attachmentID)), withData)
	if err != nil {
		return Attachment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Attachment{}, err
	}
	return file, nil
}
