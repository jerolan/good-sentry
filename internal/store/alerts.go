package store

import (
	"context"
	"errors"
	"math"
	"time"

	"goodsentry/internal/notify"

	"github.com/jackc/pgx/v5"
)

var ErrNoRecipients = errors.New("alert has no resolved recipients")

const alertColumns = `id::text,project_id::text,name,kind,enabled,quantity,window_minutes,cooldown_seconds,
	environments,recipients,created_at,updated_at,last_triggered`
const deliveryColumns = `id,COALESCE(rule_id::text,''),rule_name,project_id::text,recipient_type,destination,payload,
	status,attempts,next_attempt,created_at,updated_at,sent_at,last_reason,last_status_code`

type alertProjectInfo struct{ ID, Slug, Name, Organization string }

func scanAlertProject(row scanner) (alertProjectInfo, error) {
	var p alertProjectInfo
	err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.Organization)
	return p, dbError(err)
}

func (s *Store) alertProject(ctx context.Context, userID, orgSlug, projectID string) (alertProjectInfo, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return alertProjectInfo{}, err
	}
	if org.Role != "owner" && org.Role != "admin" {
		return alertProjectInfo{}, ErrForbidden
	}
	return scanAlertProject(s.pool.QueryRow(ctx, `SELECT p.id::text,p.slug,p.name,o.slug FROM projects p JOIN organizations o ON o.id=p.organization_id WHERE p.id::text=$1 AND o.id::text=$2`, projectID, org.ID))
}

func scanAlert(row scanner) (notify.Rule, error) {
	var r notify.Rule
	err := row.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Kind, &r.Enabled, &r.Quantity, &r.WindowMinutes, &r.CooldownSeconds, &r.Environments, &r.Recipients, &r.CreatedAt, &r.UpdatedAt, &r.LastTriggered)
	return r, dbError(err)
}

func deliveryFields(d *notify.Delivery) []any {
	return []any{&d.ID, &d.RuleID, &d.RuleName, &d.ProjectID, &d.Recipient.Type, &d.Recipient.Destination, &d.Payload,
		&d.Status, &d.Attempts, &d.NextAttempt, &d.CreatedAt, &d.UpdatedAt, &d.SentAt, &d.LastReason, &d.LastStatusCode}
}

func scanDelivery(row scanner) (notify.Delivery, error) {
	var d notify.Delivery
	err := row.Scan(deliveryFields(&d)...)
	return d, dbError(err)
}

func alertNow(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)
	return now, err
}

func (s *Store) Alerts(ctx context.Context, userID, orgSlug, projectID string) ([]notify.Rule, error) {
	if _, err := s.alertProject(ctx, userID, orgSlug, projectID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT "+alertColumns+" FROM alert_rules WHERE project_id::text=$1 ORDER BY id", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []notify.Rule{}
	for rows.Next() {
		r, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

func (s *Store) Alert(ctx context.Context, userID, orgSlug, projectID, id string) (notify.Rule, error) {
	if _, err := s.alertProject(ctx, userID, orgSlug, projectID); err != nil {
		return notify.Rule{}, err
	}
	return scanAlert(s.pool.QueryRow(ctx, "SELECT "+alertColumns+" FROM alert_rules WHERE project_id::text=$1 AND id::text=$2", projectID, id))
}

func (s *Store) CreateAlert(ctx context.Context, userID, orgSlug, projectID string, c notify.Config) (notify.Rule, error) {
	if _, err := s.alertProject(ctx, userID, orgSlug, projectID); err != nil {
		return notify.Rule{}, err
	}
	if c.Environments == nil {
		c.Environments = []string{}
	}
	return scanAlert(s.pool.QueryRow(ctx, `INSERT INTO alert_rules(project_id,name,kind,enabled,quantity,window_minutes,cooldown_seconds,environments,recipients,created_at,updated_at)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,t,t FROM (SELECT clock_timestamp() t) n RETURNING `+alertColumns,
		projectID, c.Name, c.Kind, c.Enabled, c.Quantity, c.WindowMinutes, c.CooldownSeconds, c.Environments, c.Recipients))
}

func (s *Store) UpdateAlert(ctx context.Context, userID, orgSlug, projectID, id string, c notify.Config) (notify.Rule, error) {
	if _, err := s.alertProject(ctx, userID, orgSlug, projectID); err != nil {
		return notify.Rule{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return notify.Rule{}, err
	}
	defer tx.Rollback(ctx)
	current, err := scanAlert(tx.QueryRow(ctx, "SELECT "+alertColumns+" FROM alert_rules WHERE project_id::text=$1 AND id::text=$2 FOR UPDATE", projectID, id))
	if err != nil {
		return notify.Rule{}, err
	}
	if current.Kind != c.Kind {
		return notify.Rule{}, ErrConflict
	}
	now, err := alertNow(ctx, tx)
	if err != nil {
		return notify.Rule{}, err
	}
	if err := cancelAlertWork(ctx, tx, id, now, "rule_updated"); err != nil {
		return notify.Rule{}, err
	}
	if c.Environments == nil {
		c.Environments = []string{}
	}
	r, err := scanAlert(tx.QueryRow(ctx, `UPDATE alert_rules SET name=$3,enabled=$4,quantity=$5,window_minutes=$6,cooldown_seconds=$7,environments=$8,recipients=$9,updated_at=$10 WHERE project_id::text=$1 AND id::text=$2 RETURNING `+alertColumns,
		projectID, id, c.Name, c.Enabled, c.Quantity, c.WindowMinutes, c.CooldownSeconds, c.Environments, c.Recipients, now))
	if err != nil {
		return notify.Rule{}, err
	}
	return r, tx.Commit(ctx)
}

func (s *Store) DeleteAlert(ctx context.Context, userID, orgSlug, projectID, id string) error {
	if _, err := s.alertProject(ctx, userID, orgSlug, projectID); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists string
	if err := tx.QueryRow(ctx, "SELECT id::text FROM alert_rules WHERE project_id::text=$1 AND id::text=$2 FOR UPDATE", projectID, id).Scan(&exists); err != nil {
		return dbError(err)
	}
	now, err := alertNow(ctx, tx)
	if err != nil {
		return err
	}
	if err := cancelAlertWork(ctx, tx, id, now, "rule_deleted"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM alert_rules WHERE id::text=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func cancelAlertWork(ctx context.Context, tx pgx.Tx, id string, now time.Time, reason string) error {
	_, err := tx.Exec(ctx, `WITH canceled AS (
  UPDATE alert_deliveries SET status='canceled',lease_token='',lease_expires_at=NULL,updated_at=$2,last_reason=$3
  WHERE rule_id::text=$1 AND status IN ('pending','processing') RETURNING id
 ) UPDATE alert_attempts SET status='canceled',completed_at=$2,reason=$3
 WHERE delivery_id IN (SELECT id FROM canceled) AND status='processing'`, id, now, reason)
	return err
}

func (s *Store) AlertDeliveries(ctx context.Context, userID, orgSlug, projectID, ruleID string, page int) ([]notify.Delivery, bool, error) {
	if _, err := s.alertProject(ctx, userID, orgSlug, projectID); err != nil {
		return nil, false, err
	}
	p := int64(page)
	if p < 1 {
		p = 1
	}
	if p > math.MaxInt64/25 {
		return nil, false, errors.New("alert delivery page is outside supported range")
	}
	rows, err := s.pool.Query(ctx, "SELECT "+deliveryColumns+` FROM alert_deliveries WHERE project_id::text=$1 AND ($2='' OR COALESCE(rule_id::text,payload#>>'{rule,id}')=$2) ORDER BY created_at DESC,id DESC LIMIT 26 OFFSET $3`, projectID, ruleID, (p-1)*25)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := []notify.Delivery{}
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(items) > 25
	if more {
		items = items[:25]
	}
	return items, more, nil
}

func (s *Store) AlertDelivery(ctx context.Context, userID, orgSlug, projectID, id string) (notify.Delivery, []notify.Attempt, error) {
	if _, err := s.alertProject(ctx, userID, orgSlug, projectID); err != nil {
		return notify.Delivery{}, nil, err
	}
	d, err := scanDelivery(s.pool.QueryRow(ctx, "SELECT "+deliveryColumns+" FROM alert_deliveries WHERE project_id::text=$1 AND id=$2", projectID, id))
	if err != nil {
		return notify.Delivery{}, nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT number,started_at,completed_at,status,reason,status_code,elapsed_ms FROM alert_attempts WHERE delivery_id=$1 ORDER BY number`, id)
	if err != nil {
		return notify.Delivery{}, nil, err
	}
	defer rows.Close()
	attempts := []notify.Attempt{}
	for rows.Next() {
		var a notify.Attempt
		if err := rows.Scan(&a.Number, &a.StartedAt, &a.CompletedAt, &a.Status, &a.Reason, &a.StatusCode, &a.ElapsedMS); err != nil {
			return notify.Delivery{}, nil, err
		}
		attempts = append(attempts, a)
	}
	return d, attempts, rows.Err()
}

func (s *Store) TestAlert(ctx context.Context, userID, orgSlug, projectID, id string) (int, error) {
	p, err := s.alertProject(ctx, userID, orgSlug, projectID)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	r, err := scanAlert(tx.QueryRow(ctx, "SELECT "+alertColumns+" FROM alert_rules WHERE project_id::text=$1 AND id::text=$2 FOR UPDATE", projectID, id))
	if err != nil {
		return 0, err
	}
	if !r.Enabled {
		return 0, ErrConflict
	}
	trigger, err := randomKey()
	if err != nil {
		return 0, err
	}
	now, err := alertNow(ctx, tx)
	if err != nil {
		return 0, err
	}
	payload := alertPayload(r, p, "test", now)
	payload.URL = alertResourceURL("issues", p.Organization, "", "")
	n, err := enqueueAlert(ctx, tx, r, p, "test:"+trigger, payload, now)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrNoRecipients
	}
	return n, tx.Commit(ctx)
}
