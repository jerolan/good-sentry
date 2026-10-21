package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"time"

	"goodsentry/internal/notify"
	"goodsentry/internal/uptime"

	"github.com/jackc/pgx/v5"
)

func triggerProject(ctx context.Context, tx pgx.Tx, projectID string) (alertProjectInfo, error) {
	return scanAlertProject(tx.QueryRow(ctx, `SELECT p.id::text,p.slug,p.name,o.slug FROM projects p JOIN organizations o ON o.id=p.organization_id WHERE p.id=$1::bigint`, projectID))
}

func lockedAlertRules(ctx context.Context, tx pgx.Tx, projectID string, kind notify.Kind, environment string) ([]notify.Rule, error) {
	rows, err := tx.Query(ctx, "SELECT "+alertColumns+` FROM alert_rules WHERE project_id=$1::bigint AND kind=$2 AND enabled
		AND (cardinality(environments)=0 OR $3=ANY(environments)) ORDER BY id FOR UPDATE`, projectID, kind, environment)
	if err != nil {
		return nil, err
	}
	rules := []notify.Rule{}
	for rows.Next() {
		r, err := scanAlert(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		rules = append(rules, r)
	}
	err = rows.Err()
	rows.Close()
	return rules, err
}

// Error capture locks rules before observing release metadata. Enqueuing runs
// after resolution settles, retaining the same rule snapshot and lock order.
type errorAlertCapture struct {
	title, environment string
	received           time.Time
	rules              []notify.Rule
	project            alertProjectInfo
}

func prepareErrorAlerts(ctx context.Context, tx pgx.Tx, projectID, eventID, issueID string) (*errorAlertCapture, error) {
	c := &errorAlertCapture{}
	var kind, status string
	if err := tx.QueryRow(ctx, `SELECT kind,title,environment,created_at FROM events WHERE project_id=$1::bigint AND event_id=$2`, projectID, eventID).
		Scan(&kind, &c.title, &c.environment, &c.received); err != nil {
		return nil, err
	}
	if kind == "transaction" || kind == "feedback" || issueID == "" {
		return nil, nil
	}
	if err := tx.QueryRow(ctx, "SELECT status FROM issues WHERE id=$1::bigint", issueID).Scan(&status); err != nil {
		return nil, err
	}
	if status == "ignored" {
		return nil, nil
	}
	var err error
	c.rules, err = lockedAlertRules(ctx, tx, projectID, notify.Frequency, c.environment)
	if err != nil || len(c.rules) == 0 {
		return nil, err
	}
	c.project, err = triggerProject(ctx, tx, projectID)
	return c, err
}

func (c *errorAlertCapture) enqueue(ctx context.Context, tx pgx.Tx, eventID, issueID string) error {
	if c == nil {
		return nil
	}
	var status string
	if err := tx.QueryRow(ctx, "SELECT status FROM issues WHERE id=$1::bigint", issueID).Scan(&status); err != nil {
		return err
	}
	if status != "unresolved" {
		return nil
	}
	// Read the database clock after both rule and release waits.
	now, err := alertNow(ctx, tx)
	if err != nil {
		return err
	}
	for _, r := range c.rules {
		start := now.Add(-time.Duration(r.WindowMinutes) * time.Minute)
		if !c.received.After(start) || c.received.After(now) {
			continue
		}
		if r.LastTriggered != nil && now.Before(r.LastTriggered.Add(time.Duration(r.CooldownSeconds)*time.Second)) {
			continue
		}
		var before int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM events WHERE project_id=$1::bigint AND event_id<>$2
			AND kind NOT IN ('transaction','feedback') AND created_at>$3 AND created_at<=$4 AND (cardinality($5::text[])=0 OR environment=ANY($5::text[])) LIMIT $6) counted`,
			c.project.ID, eventID, start, now, r.Environments, r.Quantity).Scan(&before)
		if err != nil {
			return err
		}
		if before != r.Quantity-1 {
			continue
		}
		payload := alertPayload(r, c.project, "error_frequency", c.received)
		payload.Issue = &notify.Resource{ID: issueID, Name: boundedAlertText(c.title, 200)}
		payload.EventID, payload.Count, payload.WindowMinutes, payload.Environment = eventID, r.Quantity, r.WindowMinutes, boundedAlertText(c.environment, 128)
		payload.URL = alertResourceURL("issues", c.project.Organization, "issue", issueID)
		n, err := enqueueAlert(ctx, tx, r, c.project, "event:"+eventID, payload, now)
		if err != nil {
			return err
		}
		if n > 0 {
			if _, err := tx.Exec(ctx, "UPDATE alert_rules SET last_triggered=$2 WHERE id::text=$1", r.ID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func queueMonitorAlerts(ctx context.Context, tx pgx.Tx, monitorID string, check uptime.Check) error {
	if !check.IsChange {
		return nil
	}
	kind := ""
	if check.Status == uptime.Down {
		kind = "uptime_down"
	} else if check.PreviousStatus == uptime.Down && check.Status == uptime.Up {
		kind = "uptime_recovered"
	}
	if kind == "" {
		return nil
	}
	var projectID *string
	var name, environment string
	if err := tx.QueryRow(ctx, "SELECT project_id::text,name,environment FROM monitors WHERE id::text=$1", monitorID).Scan(&projectID, &name, &environment); err != nil {
		return err
	}
	if projectID == nil {
		return nil
	}
	rules, err := lockedAlertRules(ctx, tx, *projectID, notify.Uptime, environment)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	p, err := triggerProject(ctx, tx, *projectID)
	if err != nil {
		return err
	}
	now, err := alertNow(ctx, tx)
	if err != nil {
		return err
	}
	for _, r := range rules {
		payload := alertPayload(r, p, kind, check.CompletedAt)
		payload.Monitor = &notify.Monitor{Resource: notify.Resource{ID: monitorID, Name: boundedAlertText(name, 200)}, Status: string(check.Status), PreviousStatus: string(check.PreviousStatus)}
		payload.Environment = boundedAlertText(environment, 128)
		payload.URL = alertResourceURL("monitors", p.Organization, "monitor", monitorID)
		n, err := enqueueAlert(ctx, tx, r, p, "check:"+check.ID, payload, now)
		if err != nil {
			return err
		}
		if n > 0 {
			if _, err := tx.Exec(ctx, "UPDATE alert_rules SET last_triggered=$2 WHERE id::text=$1", r.ID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func alertPayload(r notify.Rule, p alertProjectInfo, kind string, occurred time.Time) notify.Payload {
	return notify.Payload{Version: 1, Kind: kind, OccurredAt: occurred, Rule: notify.Resource{ID: r.ID, Name: boundedAlertText(r.Name, 200)},
		Organization: p.Organization, Project: notify.Project{Resource: notify.Resource{ID: p.ID, Name: boundedAlertText(p.Name, 200)}, Slug: p.Slug}}
}

func alertResourceURL(view, org, key, id string) string {
	link := "/?view=" + url.QueryEscape(view) + "&org=" + url.QueryEscape(org)
	if key != "" {
		link += "&" + key + "=" + url.QueryEscape(id)
	}
	return link
}

func boundedAlertText(value string, limit int) string {
	count := 0
	for i := range value {
		if count == limit {
			return strings.Clone(value[:i])
		}
		count++
	}
	return value
}

func resolvedAlertRecipients(ctx context.Context, tx pgx.Tx, projectID string, configured []notify.Recipient) ([]notify.Recipient, error) {
	unique := map[notify.Recipient]bool{}
	for _, recipient := range configured {
		if recipient.Type != "team_email" {
			unique[recipient] = true
			continue
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT u.email FROM projects p
			JOIN LATERAL (
			 SELECT pt.team_id FROM project_teams pt WHERE pt.project_id=p.id
			 UNION
			 SELECT d.id FROM teams d WHERE d.organization_id=p.organization_id AND d.slug='default'
			  AND NOT EXISTS(SELECT 1 FROM project_teams pt WHERE pt.project_id=p.id)
			) selected ON true
			JOIN teams t ON t.id=selected.team_id AND t.organization_id=p.organization_id
			JOIN team_memberships tm ON tm.team_id=t.id JOIN users u ON u.id=tm.user_id
			JOIN memberships m ON m.user_id=u.id AND m.organization_id=p.organization_id WHERE p.id=$1::bigint ORDER BY u.email`, projectID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var email string
			if err := rows.Scan(&email); err != nil {
				rows.Close()
				return nil, err
			}
			unique[notify.Recipient{Type: "email", Destination: email}] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	recipients := make([]notify.Recipient, 0, len(unique))
	for recipient := range unique {
		recipients = append(recipients, recipient)
	}
	sort.Slice(recipients, func(i, j int) bool {
		if recipients[i].Type != recipients[j].Type {
			return recipients[i].Type < recipients[j].Type
		}
		return recipients[i].Destination < recipients[j].Destination
	})
	return recipients, nil
}

func enqueueAlert(ctx context.Context, tx pgx.Tx, r notify.Rule, p alertProjectInfo, trigger string, payload notify.Payload, now time.Time) (int, error) {
	recipients, err := resolvedAlertRecipients(ctx, tx, p.ID, r.Recipients)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, recipient := range recipients {
		id, err := randomKey()
		if err != nil {
			return 0, err
		}
		payload.ID = id
		digest := sha256.Sum256([]byte(recipient.Destination))
		result, err := tx.Exec(ctx, `INSERT INTO alert_deliveries(id,project_id,rule_id,rule_name,trigger_key,recipient_type,destination,destination_hash,payload,status,next_attempt,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending',$10,$10,$10) ON CONFLICT(rule_id,trigger_key,recipient_type,destination_hash) DO NOTHING`,
			id, p.ID, r.ID, boundedAlertText(r.Name, 200), trigger, recipient.Type, recipient.Destination, hex.EncodeToString(digest[:]), payload, now)
		if err != nil {
			return 0, err
		}
		count += int(result.RowsAffected())
	}
	return count, nil
}
