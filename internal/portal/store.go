package portal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type project struct {
	ID         int64
	Name       string
	Key        string
	WebhookURL string
}

type issue struct {
	ID    int64
	Title string
	Count int64
}

type event struct {
	ID         string
	Type       string
	Message    string
	ReceivedAt time.Time
	Frames     []frame
}

type frame struct {
	Function string `json:"function"`
	Filename string `json:"filename"`
	Line     int    `json:"lineno"`
}

func (p *portal) createProject(ctx context.Context, name string) (int64, error) {
	var id int64
	key := hex.EncodeToString(randomBytes(16))
	err := p.db.QueryRow(ctx, "INSERT INTO projects (name, ingest_key) VALUES ($1, $2) RETURNING id", name, key).Scan(&id)
	return id, err
}

func randomBytes(size int) []byte {
	key := make([]byte, size)
	rand.Read(key)
	return key
}

func (p *portal) projects(ctx context.Context) ([]project, error) {
	rows, err := p.db.Query(ctx, "SELECT id, name FROM projects ORDER BY id")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (project, error) {
		var item project
		err := row.Scan(&item.ID, &item.Name)
		return item, err
	})
}

func (p *portal) project(ctx context.Context, projectID int64) (project, error) {
	result := project{ID: projectID}
	err := p.db.QueryRow(ctx, "SELECT name, ingest_key, webhook_url FROM projects WHERE id = $1", projectID).Scan(&result.Name, &result.Key, &result.WebhookURL)
	return result, err
}

func (p *portal) setWebhook(ctx context.Context, projectID int64, webhookURL string) error {
	var id int64
	return p.db.QueryRow(ctx, "UPDATE projects SET webhook_url = $2 WHERE id = $1 RETURNING id", projectID, webhookURL).Scan(&id)
}

func (p *portal) issues(ctx context.Context, projectID int64) ([]issue, error) {
	rows, err := p.db.Query(ctx, `
		SELECT i.id, i.title, count(*)
		FROM issues i JOIN events e ON (i.project_id, i.id) = (e.project_id, e.issue_id)
		WHERE i.project_id = $1
		GROUP BY i.id, i.title
		ORDER BY max(e.received_at) DESC, i.id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[issue])
}

func (p *portal) issue(ctx context.Context, projectID, issueID int64) (issue, error) {
	result := issue{ID: issueID}
	err := p.db.QueryRow(ctx, `
		SELECT i.title, count(*)
		FROM issues i JOIN events e ON (i.project_id, i.id) = (e.project_id, e.issue_id)
		WHERE i.project_id = $1 AND i.id = $2
		GROUP BY i.id, i.title`, projectID, issueID).Scan(&result.Title, &result.Count)
	return result, err
}

func (p *portal) events(ctx context.Context, projectID, issueID int64) ([]event, error) {
	rows, err := p.db.Query(ctx, `
		SELECT id, message, received_at FROM events
		WHERE project_id = $1 AND issue_id = $2
		ORDER BY received_at DESC, id`, projectID, issueID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (event, error) {
		var item event
		err := row.Scan(&item.ID, &item.Message, &item.ReceivedAt)
		return item, err
	})
}

func (p *portal) event(ctx context.Context, projectID, issueID int64, id string) (event, error) {
	result := event{ID: id}
	err := p.db.QueryRow(ctx, `
		SELECT message, exception_type, frames, received_at FROM events
		WHERE project_id = $1 AND issue_id = $2 AND id = $3`, projectID, issueID, id).
		Scan(&result.Message, &result.Type, &result.Frames, &result.ReceivedAt)
	return result, err
}

var errDuplicateEvent = errors.New("duplicate event")

func (p *portal) saveEvent(ctx context.Context, projectID int64, item event) (int64, error) {
	var top frame
	if len(item.Frames) > 0 {
		top = item.Frames[len(item.Frames)-1]
	}
	identity, err := json.Marshal([4]string{item.Type, item.Message, top.Function, top.Filename})
	if err != nil {
		return 0, err
	}
	fingerprint := sha256.Sum256(identity)
	var issueID int64

	err = pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO issues (project_id, fingerprint, title) VALUES ($1, $2, $3)
			ON CONFLICT (project_id, fingerprint) DO UPDATE SET title = EXCLUDED.title
			RETURNING id`, projectID, fingerprint[:], item.Message).Scan(&issueID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO events (project_id, issue_id, id, message, exception_type, frames)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (project_id, id) DO NOTHING`, projectID, issueID, item.ID, item.Message, item.Type, item.Frames)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// Roll back the issue upsert as well, including a new issue on a conflicting retry.
			return errDuplicateEvent
		}
		return nil
	})
	if errors.Is(err, errDuplicateEvent) {
		return 0, nil
	}
	return issueID, err
}
