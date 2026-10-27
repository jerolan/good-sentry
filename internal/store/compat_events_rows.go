package store

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/eventview"
)

const compatibleEventSelect = `SELECT e.id,e.event_id,e.project_id,COALESCE(e.issue_id::text,''),e.kind,e.title,e.level,e.environment,e.release,
 e.timestamp,e.created_at,e.data,r.id::text,r.version,r.created_at,r.date_released,
 report.id::text,report.created_at,report.name,report.email,report.comments`
const compatibleEventJoins = ` FROM events_with_archive e LEFT JOIN issues i ON i.id=e.issue_id
 JOIN projects p ON p.id=e.project_id
 LEFT JOIN releases r ON r.organization_id=p.organization_id AND r.version=e.release
 AND EXISTS(SELECT 1 FROM release_projects rp WHERE rp.release_id=r.id AND rp.project_id=e.project_id)
 LEFT JOIN user_reports report ON report.event_receipt_id=e.id`

func scanCompatibleEvent(row pgx.Row) (eventview.Record, int64, error) {
	var record eventview.Record
	var rowID int64
	var releaseID, version *string
	var created, released *time.Time
	var reportID, name, email, comments *string
	var reported *time.Time
	err := row.Scan(&rowID, &record.ID, &record.ProjectID, &record.GroupID, &record.Kind, &record.Title, &record.Level, &record.Environment, &record.Release,
		&record.Timestamp, &record.Received, &record.Data, &releaseID, &version, &created, &released,
		&reportID, &reported, &name, &email, &comments)
	if err != nil {
		return record, 0, err
	}
	if releaseID != nil {
		record.ReleaseInfo = &eventview.ReleaseReference{ID: *releaseID, Version: *version, DateCreated: created.UTC(), DateReleased: releaseTimeUTC(released)}
	}
	if reportID != nil {
		record.UserReport, err = json.Marshal(UserReport{ID: *reportID, EventID: record.ID, Event: UserReportEvent{EventID: record.ID}, DateCreated: reported.UTC(), Name: *name, Email: *email, Comments: *comments})
	}
	return record, rowID, err
}
