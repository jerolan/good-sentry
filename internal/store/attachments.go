package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Data        []byte `json:"-"`
}

type CaptureAttachment struct {
	Filename, ContentType string
	Data                  []byte
}

type CaptureEnvelopeItem struct {
	Type   string
	Header json.RawMessage
	Data   []byte
}

// CaptureItems belong to the normalized event and share its receipt transaction.
type CaptureItems struct {
	Attachments   []CaptureAttachment
	EnvelopeItems []CaptureEnvelopeItem
}

func saveCaptureItems(ctx context.Context, tx pgx.Tx, projectID, eventID string, parts []CaptureItems) error {
	for _, part := range parts {
		for _, item := range part.Attachments {
			id := fmt.Sprintf("%x", sha256.Sum256(append([]byte(projectID+":"+eventID+":"+item.Filename+":"), item.Data...)))
			if _, err := tx.Exec(ctx, "INSERT INTO attachments(id,project_id,event_id,filename,content_type,data) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO NOTHING", id, projectID, eventID, item.Filename, item.ContentType, item.Data); err != nil {
				return err
			}
		}
		for _, item := range part.EnvelopeItems {
			if err := saveEnvelopeItem(ctx, tx, projectID, eventID, item.Type, item.Header, item.Data); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) SaveEnvelopeItem(ctx context.Context, projectID, eventID, itemType string, header json.RawMessage, data []byte) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockProjectIssues(ctx, tx, projectID, false); err != nil {
		return err
	}
	if err = saveEnvelopeItem(ctx, tx, projectID, eventID, itemType, header, data); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func saveEnvelopeItem(ctx context.Context, tx pgx.Tx, projectID, eventID, itemType string, header json.RawMessage, data []byte) error {
	id := fmt.Sprintf("%x", sha256.Sum256(append(append([]byte(projectID+":"+eventID+":"+itemType+":"), header...), data...)))
	_, err := tx.Exec(ctx, "INSERT INTO envelope_items(id,project_id,event_id,item_type,header,data) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO NOTHING", id, projectID, eventID, itemType, header, data)
	return err
}
