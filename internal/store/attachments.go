package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Data        []byte `json:"-"`
}

func (s *Store) SaveAttachment(ctx context.Context, projectID, eventID, name, contentType string, data []byte) error {
	id := fmt.Sprintf("%x", sha256.Sum256(append([]byte(projectID+":"+eventID+":"+name+":"), data...)))
	_, err := s.pool.Exec(ctx, "INSERT INTO attachments(id,project_id,event_id,filename,content_type,data) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO NOTHING", id, projectID, eventID, name, contentType, data)
	return err
}
func (s *Store) SaveEnvelopeItem(ctx context.Context, projectID, eventID, itemType string, header json.RawMessage, data []byte) error {
	id := fmt.Sprintf("%x", sha256.Sum256(append(append([]byte(projectID+":"+eventID+":"+itemType+":"), header...), data...)))
	_, err := s.pool.Exec(ctx, "INSERT INTO envelope_items(id,project_id,event_id,item_type,header,data) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO NOTHING", id, projectID, eventID, itemType, header, data)
	return err
}
