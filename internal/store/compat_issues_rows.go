package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/eventview"
)

func readCompatibleIssueMetrics(ctx context.Context, tx pgx.Tx, records []CompatibleIssueRecord, filter issueOccurrenceFilter, end time.Time, chart string) error {
	ids := make([]string, len(records))
	byID := map[string]*CompatibleIssueRecord{}
	for i := range records {
		ids[i] = records[i].Issue.ID
		byID[ids[i]] = &records[i]
	}
	metrics, err := readIssueMetrics(ctx, tx, ids, filter, end, chart)
	if err != nil {
		return err
	}
	for id, record := range byID {
		item := metrics[id]
		record.UserCount = item.lifetime.UserCount
		record.IsUnhandled = item.lifetime.IsUnhandled
		record.Stats = item.lifetime.Stats
		record.Filtered = item.filtered
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON(e.issue_id) `+compatibleEventSelect[len("SELECT "):]+compatibleEventJoins+` WHERE e.issue_id=ANY($1::bigint[]) ORDER BY e.issue_id,e.timestamp DESC,e.id DESC`, issueNumericIDs(ids))
	if err != nil {
		return err
	}
	for rows.Next() {
		record, _, err := scanCompatibleEvent(rows)
		if err != nil {
			rows.Close()
			return err
		}
		byID[record.GroupID].Latest = &record
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Release references require a retained occurrence and a current real
	// release/project association. Project release candidates alone do not qualify.
	rows, err = tx.Query(ctx, `WITH observed AS (
 SELECT e.issue_id,e.timestamp,e.id,r.id::text release_id,r.version,r.created_at,r.date_released
 FROM events e JOIN projects p ON p.id=e.project_id
 JOIN releases r ON r.organization_id=p.organization_id AND r.version=e.release
 JOIN release_projects rp ON rp.release_id=r.id AND rp.project_id=e.project_id
 WHERE e.issue_id=ANY($1::bigint[]) AND e.release<>''),
 first_release AS(SELECT DISTINCT ON(issue_id) * FROM observed ORDER BY issue_id,timestamp,id),
 last_release AS(SELECT DISTINCT ON(issue_id) * FROM observed ORDER BY issue_id,timestamp DESC,id DESC)
 SELECT issue_id::text,true,release_id,version,created_at,date_released FROM first_release
 UNION ALL SELECT issue_id::text,false,release_id,version,created_at,date_released FROM last_release`, issueNumericIDs(ids))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var first bool
		var release eventview.ReleaseReference
		if err = rows.Scan(&id, &first, &release.ID, &release.Version, &release.DateCreated, &release.DateReleased); err != nil {
			rows.Close()
			return err
		}
		release.DateCreated = release.DateCreated.UTC()
		release.DateReleased = releaseTimeUTC(release.DateReleased)
		if first {
			byID[id].FirstRelease = &release
		} else {
			byID[id].LastRelease = &release
		}
	}
	err = rows.Err()
	rows.Close()
	return err
}
