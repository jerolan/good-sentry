package store

import (
	"github.com/jackc/pgx/v5"
	"goodsentry/internal/eventview"
	"time"
)

const compatibleEventSelect = `SELECT e.id,e.event_id,e.project_id,COALESCE(e.issue_id::text,''),e.kind,e.title,e.level,e.environment,e.release,
 e.timestamp,e.created_at,e.data,r.id::text,r.version,r.created_at,r.date_released`
const compatibleEventJoins = ` FROM events e LEFT JOIN issues i ON i.id=e.issue_id
 JOIN projects p ON p.id=e.project_id
 LEFT JOIN releases r ON r.organization_id=p.organization_id AND r.version=e.release
 AND EXISTS(SELECT 1 FROM release_projects rp WHERE rp.release_id=r.id AND rp.project_id=e.project_id)`

func scanCompatibleEvent(row pgx.Row) (eventview.Record, int64, error) {
	var record eventview.Record
	var rowID int64
	var releaseID, version *string
	var created, released *time.Time
	err := row.Scan(&rowID, &record.ID, &record.ProjectID, &record.GroupID, &record.Kind, &record.Title, &record.Level, &record.Environment, &record.Release,
		&record.Timestamp, &record.Received, &record.Data, &releaseID, &version, &created, &released)
	if err != nil {
		return record, 0, err
	}
	if releaseID != nil {
		record.ReleaseInfo = &eventview.ReleaseReference{ID: *releaseID, Version: *version, DateCreated: created.UTC(), DateReleased: releaseTimeUTC(released)}
	}
	return record, rowID, nil
}
