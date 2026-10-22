package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"goodsentry/internal/uptime"

	"github.com/jackc/pgx/v5"
)

const monitorColumns = `m.id::text,m.name,m.kind,m.url,COALESCE(p.slug,''),m.environment,m.interval_seconds,m.timeout_seconds,
	m.expected_status,m.expected_body,m.confirmation_threshold,m.enabled,m.status,m.consecutive_failures,
	m.last_check,m.last_change,m.next_check,m.created_at,m.heartbeat_key,m.endpoint_id::text,m.organization_id::text`
const monitorJoin = ` FROM monitors m LEFT JOIN projects p ON p.id=m.project_id `
const monitorCheckColumns = `id::text,started_at,completed_at,up,status,previous_status,is_change,response_time_ms,status_code,reason`

func monitorFields(m *uptime.Monitor) []any {
	return []any{&m.ID, &m.Name, &m.Type, &m.URL, &m.Project, &m.Environment, &m.IntervalSeconds, &m.TimeoutSeconds,
		&m.ExpectedStatus, &m.ExpectedBody, &m.ConfirmationThreshold, &m.Enabled, &m.Status, &m.ConsecutiveFailures,
		&m.LastCheck, &m.LastChange, &m.NextCheck, &m.CreatedAt, &m.HeartbeatKey, &m.EndpointID, &m.OrganizationID}
}

func scanMonitor(row scanner) (uptime.Monitor, error) {
	var m uptime.Monitor
	err := row.Scan(monitorFields(&m)...)
	return m, dbError(err)
}

func scanMonitorCheck(row scanner) (uptime.Check, error) {
	var c uptime.Check
	err := row.Scan(&c.ID, &c.StartedAt, &c.CompletedAt, &c.Up, &c.Status, &c.PreviousStatus, &c.IsChange, &c.ResponseTimeMS, &c.StatusCode, &c.Reason)
	return c, dbError(err)
}

func (s *Store) Monitors(ctx context.Context, userID, orgSlug string) ([]uptime.Monitor, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT "+monitorColumns+monitorJoin+" WHERE m.organization_id::text=$1 ORDER BY m.created_at DESC,m.id DESC", org.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []uptime.Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (s *Store) Monitor(ctx context.Context, userID, orgSlug, id string) (uptime.Monitor, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return uptime.Monitor{}, err
	}
	return scanMonitor(s.pool.QueryRow(ctx, "SELECT "+monitorColumns+monitorJoin+" WHERE m.organization_id::text=$1 AND m.id::text=$2", org.ID, id))
}

func (s *Store) monitorWriteOrganization(ctx context.Context, userID, orgSlug string) (Organization, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return Organization{}, err
	}
	if org.Role != "owner" && org.Role != "admin" {
		return Organization{}, ErrForbidden
	}
	return org, nil
}

func monitorProject(ctx context.Context, tx pgx.Tx, orgID, slug string) (any, error) {
	if slug == "" {
		return nil, nil
	}
	var id string
	err := tx.QueryRow(ctx, "SELECT id::text FROM projects WHERE organization_id::text=$1 AND slug=$2", orgID, slug).Scan(&id)
	return id, dbError(err)
}

func monitorNow(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)
	return now, err
}

func (s *Store) CreateMonitor(ctx context.Context, userID, orgSlug string, c uptime.Config) (uptime.Monitor, error) {
	org, err := s.monitorWriteOrganization(ctx, userID, orgSlug)
	if err != nil {
		return uptime.Monitor{}, err
	}
	key := ""
	if c.Type == uptime.Heartbeat {
		key, err = randomKey()
		if err != nil {
			return uptime.Monitor{}, err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uptime.Monitor{}, err
	}
	defer tx.Rollback(ctx)
	project, err := monitorProject(ctx, tx, org.ID, c.Project)
	if err != nil {
		return uptime.Monitor{}, err
	}
	now, err := monitorNow(ctx, tx)
	if err != nil {
		return uptime.Monitor{}, err
	}
	next := now
	if c.Type == uptime.Heartbeat {
		next = now.Add(time.Duration(c.IntervalSeconds) * time.Second)
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO monitors(organization_id,project_id,name,kind,url,interval_seconds,timeout_seconds,
		expected_status,expected_body,confirmation_threshold,enabled,next_check,created_at,heartbeat_key,environment)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id::text`, org.ID, project,
		c.Name, c.Type, c.URL, c.IntervalSeconds, c.TimeoutSeconds, c.ExpectedStatus, c.ExpectedBody,
		c.ConfirmationThreshold, c.Enabled, next, now, key, c.Environment).Scan(&id)
	if err != nil {
		return uptime.Monitor{}, dbError(err)
	}
	m, err := scanMonitor(tx.QueryRow(ctx, "SELECT "+monitorColumns+monitorJoin+" WHERE m.id::text=$1", id))
	if err != nil {
		return uptime.Monitor{}, err
	}
	return m, tx.Commit(ctx)
}

func (s *Store) UpdateMonitor(ctx context.Context, userID, orgSlug, id string, c uptime.Config) (uptime.Monitor, error) {
	org, err := s.monitorWriteOrganization(ctx, userID, orgSlug)
	if err != nil {
		return uptime.Monitor{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uptime.Monitor{}, err
	}
	defer tx.Rollback(ctx)
	current, err := scanMonitor(tx.QueryRow(ctx, "SELECT "+monitorColumns+monitorJoin+" WHERE m.organization_id::text=$1 AND m.id::text=$2 FOR UPDATE OF m", org.ID, id))
	if err != nil {
		return uptime.Monitor{}, err
	}
	if current.Type != c.Type {
		return uptime.Monitor{}, ErrConflict
	}
	project, err := monitorProject(ctx, tx, org.ID, c.Project)
	if err != nil {
		return uptime.Monitor{}, err
	}
	now, err := monitorNow(ctx, tx)
	if err != nil {
		return uptime.Monitor{}, err
	}
	next := now
	if c.Type == uptime.Heartbeat {
		next = now.Add(time.Duration(c.IntervalSeconds) * time.Second)
	}
	_, err = tx.Exec(ctx, `UPDATE monitors SET project_id=$2,name=$3,url=$4,interval_seconds=$5,timeout_seconds=$6,
		expected_status=$7,expected_body=$8,confirmation_threshold=$9,enabled=$10,next_check=$11,
		consecutive_failures=0,lease_token='',lease_started_at=NULL,lease_expires_at=NULL,environment=$12 WHERE id::text=$1`,
		id, project, c.Name, c.URL, c.IntervalSeconds, c.TimeoutSeconds, c.ExpectedStatus, c.ExpectedBody,
		c.ConfirmationThreshold, c.Enabled, next, c.Environment)
	if err != nil {
		return uptime.Monitor{}, err
	}
	m, err := scanMonitor(tx.QueryRow(ctx, "SELECT "+monitorColumns+monitorJoin+" WHERE m.id::text=$1", id))
	if err != nil {
		return uptime.Monitor{}, err
	}
	return m, tx.Commit(ctx)
}

func (s *Store) DeleteMonitor(ctx context.Context, userID, orgSlug, id string) error {
	org, err := s.monitorWriteOrganization(ctx, userID, orgSlug)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, "DELETE FROM monitors WHERE organization_id::text=$1 AND id::text=$2", org.ID, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MonitorChecks(ctx context.Context, userID, orgSlug, id string, changesOnly bool, page int) ([]uptime.Check, bool, error) {
	pageNumber := int64(page)
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageNumber > math.MaxInt64/25 {
		return nil, false, errors.New("monitor history page is outside supported range")
	}
	return s.MonitorChecksPage(ctx, userID, orgSlug, id, changesOnly, int((pageNumber-1)*25), 25)
}

func (s *Store) MonitorChecksPage(ctx context.Context, userID, orgSlug, id string, changesOnly bool, offset, limit int) ([]uptime.Check, bool, error) {
	if offset < 0 || offset > 100000000 || limit < 1 || limit > 100 {
		return nil, false, errors.New("invalid monitor-check page")
	}
	if _, err := s.Monitor(ctx, userID, orgSlug, id); err != nil {
		return nil, false, err
	}
	rows, err := s.pool.Query(ctx, "SELECT "+monitorCheckColumns+` FROM monitor_checks WHERE monitor_id::text=$1
		AND (NOT $2 OR is_change) ORDER BY completed_at DESC,id DESC LIMIT $4 OFFSET $3`, id, changesOnly, offset, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := []uptime.Check{}
	for rows.Next() {
		check, err := scanMonitorCheck(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, check)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	return items, more, nil
}

func (s *Store) ClaimMonitor(ctx context.Context) (uptime.Lease, error) {
	token, err := randomKey()
	if err != nil {
		return uptime.Lease{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uptime.Lease{}, err
	}
	defer tx.Rollback(ctx)
	m, err := scanMonitor(tx.QueryRow(ctx, "SELECT "+monitorColumns+monitorJoin+` WHERE m.enabled AND m.next_check<=clock_timestamp()
		AND (m.lease_expires_at IS NULL OR m.lease_expires_at<=clock_timestamp())
		ORDER BY m.next_check,m.id LIMIT 1 FOR UPDATE OF m SKIP LOCKED`))
	if err != nil {
		return uptime.Lease{}, err
	}
	now, err := monitorNow(ctx, tx)
	if err != nil {
		return uptime.Lease{}, err
	}
	lease := uptime.Lease{Monitor: m, Token: token, StartedAt: now, ExpiresAt: now.Add(time.Duration(m.TimeoutSeconds+30) * time.Second)}
	_, err = tx.Exec(ctx, "UPDATE monitors SET lease_token=$2,lease_started_at=$3,lease_expires_at=$4 WHERE id::text=$1", m.ID, token, lease.StartedAt, lease.ExpiresAt)
	if err != nil {
		return uptime.Lease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uptime.Lease{}, err
	}
	return lease, nil
}

func (s *Store) CompleteMonitor(ctx context.Context, lease uptime.Lease, result uptime.Result) (uptime.Check, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uptime.Check{}, err
	}
	defer tx.Rollback(ctx)
	var m uptime.Monitor
	var token string
	var started, expires *time.Time
	fields := append(monitorFields(&m), &token, &started, &expires)
	err = tx.QueryRow(ctx, "SELECT "+monitorColumns+",m.lease_token,m.lease_started_at,m.lease_expires_at"+
		monitorJoin+" WHERE m.id::text=$1 FOR UPDATE OF m", lease.ID).Scan(fields...)
	if errors.Is(err, pgx.ErrNoRows) {
		return uptime.Check{}, ErrConflict
	}
	if err != nil {
		return uptime.Check{}, err
	}
	now, err := monitorNow(ctx, tx)
	if err != nil {
		return uptime.Check{}, err
	}
	if !m.Enabled || token == "" || token != lease.Token || started == nil || expires == nil || !now.Before(*expires) {
		return uptime.Check{}, ErrConflict
	}
	check, err := completeMonitorCheck(ctx, tx, m, *started, now, result)
	if err != nil {
		return uptime.Check{}, err
	}
	return check, tx.Commit(ctx)
}

func (s *Store) Heartbeat(ctx context.Context, key string) (uptime.Check, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uptime.Check{}, err
	}
	defer tx.Rollback(ctx)
	m, err := scanMonitor(tx.QueryRow(ctx, "SELECT "+monitorColumns+monitorJoin+
		" WHERE m.heartbeat_key=$1 AND m.kind='heartbeat' AND m.enabled FOR UPDATE OF m", key))
	if err != nil {
		return uptime.Check{}, err
	}
	now, err := monitorNow(ctx, tx)
	if err != nil {
		return uptime.Check{}, err
	}
	check, err := completeMonitorCheck(ctx, tx, m, now, now, uptime.Result{Up: true, Reason: "heartbeat_received"})
	if err != nil {
		return uptime.Check{}, err
	}
	return check, tx.Commit(ctx)
}

// CompatibleHeartbeat resolves the public organization/endpoint identity and
// records a heartbeat through the same enabled-monitor transaction.
func (s *Store) CompatibleHeartbeat(ctx context.Context, orgSlug, endpointID string) (uptime.Check, error) {
	if endpointID == "" {
		return uptime.Check{}, ErrNotFound
	}
	var key string
	err := s.pool.QueryRow(ctx, `SELECT m.heartbeat_key FROM monitors m
		JOIN organizations o ON o.id=m.organization_id
		WHERE o.slug=$1 AND m.endpoint_id::text=$2 AND m.kind='heartbeat' AND m.enabled`, orgSlug, endpointID).Scan(&key)
	if err != nil {
		return uptime.Check{}, dbError(err)
	}
	return s.Heartbeat(ctx, key)
}

func completeMonitorCheck(ctx context.Context, tx pgx.Tx, m uptime.Monitor, started, completed time.Time, result uptime.Result) (uptime.Check, error) {
	// Probe workers own Result and supply stable reason codes, never URLs or
	// error messages. Bound them here before adding a durable history record.
	if len(result.Reason) > 64 {
		return uptime.Check{}, fmt.Errorf("monitor result reason exceeds 64 bytes")
	}
	status, failures := m.Status, 0
	if result.Up {
		status = uptime.Up
	} else {
		failures = min(m.ConsecutiveFailures+1, 100)
		if failures >= m.ConfirmationThreshold {
			status = uptime.Down
		}
	}
	changed := status != m.Status
	check, err := scanMonitorCheck(tx.QueryRow(ctx, `INSERT INTO monitor_checks(monitor_id,started_at,completed_at,up,status,
		previous_status,is_change,response_time_ms,status_code,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING `+monitorCheckColumns, m.ID, started, completed, result.Up, status, m.Status, changed, result.ResponseTimeMS, result.StatusCode, result.Reason))
	if err != nil {
		return uptime.Check{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE monitors SET status=$2,consecutive_failures=$3,last_check=$4,
		last_change=CASE WHEN $5 THEN $4 ELSE last_change END,next_check=$6,
		lease_token='',lease_started_at=NULL,lease_expires_at=NULL WHERE id::text=$1`,
		m.ID, status, failures, completed, changed, completed.Add(time.Duration(m.IntervalSeconds)*time.Second))
	if err == nil {
		err = queueMonitorAlerts(ctx, tx, m.ID, check)
	}
	return check, err
}
