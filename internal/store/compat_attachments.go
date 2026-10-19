package store

import (
	"context"
	"crypto/sha1"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type CompatibleAttachment struct {
	ID                string            `json:"id"`
	EventID           string            `json:"event_id"`
	Type              string            `json:"type"`
	Name              string            `json:"name"`
	Mimetype          string            `json:"mimetype"`
	DateCreated       time.Time         `json:"dateCreated"`
	Size              int               `json:"size"`
	Headers           map[string]string `json:"headers"`
	SHA1              string            `json:"sha1"`
	MetadataTruncated bool              `json:"metadataTruncated,omitempty"`
	Data              []byte            `json:"-"`
}

type AttachmentFilter struct {
	Query         string
	Offset, Limit int
}

// Authorize the current project and select its retained event in the same
// snapshot as the attachment. A public SDK key never grants file access.
func (s *Store) compatibleAttachmentTransaction(ctx context.Context, access ManagementAccess, project, eventID string) (pgx.Tx, compatibleEventScope, string, error) {
	ref, valid := compatibleEventReference(eventID, false)
	if !valid {
		return nil, compatibleEventScope{}, "", ErrInvalidEventFilter
	}
	tx, scope, err := s.compatibleEventTransaction(ctx, access, EventProjectTarget(project))
	if err != nil {
		return nil, scope, "", err
	}
	if err = tx.QueryRow(ctx, `SELECT event_id FROM events WHERE project_id=$1 AND event_id=$2`, scope.projectID, ref).Scan(&ref); err != nil {
		_ = tx.Rollback(ctx)
		return nil, scope, "", dbError(err)
	}
	return tx, scope, ref, nil
}

const compatibleAttachmentColumns = `a.api_id::text,a.event_id,left(a.attachment_type,128),left(a.filename,2048),left(a.content_type,256),a.created_at,octet_length(a.data),a.checksum_sha1,
 (char_length(a.attachment_type)>128 OR char_length(a.filename)>2048 OR char_length(a.content_type)>256)`

func scanCompatibleAttachment(row pgx.Row) (CompatibleAttachment, error) {
	var file CompatibleAttachment
	var raw []byte
	if err := row.Scan(&file.ID, &file.EventID, &file.Type, &file.Name, &file.Mimetype, &file.DateCreated, &file.Size, &file.SHA1, &file.MetadataTruncated, &raw); err != nil {
		return file, dbError(err)
	}
	if file.SHA1 == "" {
		file.SHA1 = fmt.Sprintf("%x", sha1.Sum(raw))
	}
	file.Data = raw
	file.Headers = map[string]string{"Content-Type": file.Mimetype}
	file.DateCreated = file.DateCreated.UTC()
	return file, nil
}

func (s *Store) CompatibleAttachments(ctx context.Context, access ManagementAccess, project, eventID string, filter AttachmentFilter) ([]CompatibleAttachment, error) {
	if filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 || filter.Offset > 100000000 || !utf8.ValidString(filter.Query) || utf8.RuneCountInString(filter.Query) > 200 || strings.ContainsRune(filter.Query, 0) {
		return nil, ErrInvalidEventFilter
	}
	tx, scope, ref, err := s.compatibleAttachmentTransaction(ctx, access, project, eventID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Historical files lack a persisted SHA-1. Stream only those selected bytes
	// one row at a time; new files return metadata without transferring blobs.
	rows, err := tx.Query(ctx, `SELECT `+compatibleAttachmentColumns+`,CASE WHEN a.checksum_sha1='' THEN a.data END
 FROM attachments a WHERE a.project_id=$1 AND a.event_id=$2
 AND ($3='' OR ($3='is:screenshot' AND a.attachment_type='event.attachment' AND lower(a.filename)='screenshot.png')
 OR ($3<>'is:screenshot' AND strpos(lower(a.filename),lower($3))>0))
 ORDER BY a.api_id LIMIT $4 OFFSET $5`, scope.projectID, ref, filter.Query, filter.Limit+1, filter.Offset)
	if err != nil {
		return nil, err
	}
	files := make([]CompatibleAttachment, 0, filter.Limit+1)
	for rows.Next() {
		file, err := scanCompatibleAttachment(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		file.Data = nil
		files = append(files, file)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return files, nil
}

func (s *Store) CompatibleAttachment(ctx context.Context, access ManagementAccess, project, eventID, attachmentID string, withData bool) (CompatibleAttachment, error) {
	if referenceID(attachmentID) == 0 {
		return CompatibleAttachment{}, ErrInvalidEventFilter
	}
	tx, scope, ref, err := s.compatibleAttachmentTransaction(ctx, access, project, eventID)
	if err != nil {
		return CompatibleAttachment{}, err
	}
	defer tx.Rollback(ctx)
	file, err := scanCompatibleAttachment(tx.QueryRow(ctx, `SELECT `+compatibleAttachmentColumns+`,CASE WHEN $4 OR a.checksum_sha1='' THEN a.data END
 FROM attachments a WHERE a.project_id=$1 AND a.event_id=$2 AND a.api_id=$3`, scope.projectID, ref, attachmentID, withData))
	if err != nil {
		return CompatibleAttachment{}, err
	}
	if !withData {
		file.Data = nil
	}
	if err = tx.Commit(ctx); err != nil {
		return CompatibleAttachment{}, err
	}
	return file, nil
}
