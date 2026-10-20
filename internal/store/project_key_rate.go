package store

import (
	"context"
	"errors"
	"time"
)

// AdmitProjectKey atomically charges a fixed-window telemetry quota. A nil
// rate limit admits without creating usage; an exhausted window returns false
// and the remaining whole seconds before its next window.
func (s *Store) AdmitProjectKey(ctx context.Context, projectID, publicKey string, charge int) (bool, int, error) {
	if charge < 1 || referenceID(projectID) == 0 {
		return false, 0, errors.New("invalid project-key rate charge")
	}
	var keyCount int
	var window, limit *int
	var windowStart *time.Time
	var used *int64
	var retry *int
	err := s.pool.QueryRow(ctx, `
 WITH k AS (
  SELECT api_id,rate_limit_window,rate_limit_count FROM project_keys
  WHERE project_id=$1::bigint AND public_key=$2 AND enabled
 ), w AS (
  SELECT api_id,rate_limit_window,rate_limit_count,
   to_timestamp(floor(extract(epoch FROM clock_timestamp())/rate_limit_window)*rate_limit_window) AS window_start
  FROM k WHERE rate_limit_window IS NOT NULL AND rate_limit_count IS NOT NULL
 ), u AS (
  INSERT INTO project_key_rate_usage(api_id,window_start,count)
  SELECT api_id,window_start,$3::bigint FROM w WHERE $3::bigint<=rate_limit_count
  ON CONFLICT(api_id,window_start) DO UPDATE
  SET count=project_key_rate_usage.count+EXCLUDED.count
  WHERE project_key_rate_usage.count+EXCLUDED.count <= (SELECT rate_limit_count FROM w)
  RETURNING count
 )
 SELECT (SELECT count(*) FROM k), (SELECT rate_limit_window FROM k), (SELECT rate_limit_count FROM k),
  (SELECT window_start FROM w), (SELECT count FROM u),
  COALESCE((SELECT GREATEST(1,CEIL(w.rate_limit_window-EXTRACT(EPOCH FROM (clock_timestamp()-w.window_start))))::int FROM w),0)`,
		projectID, publicKey, charge).Scan(&keyCount, &window, &limit, &windowStart, &used, &retry)
	if err != nil {
		return false, 0, err
	}
	if keyCount == 0 {
		return false, 0, ErrNotFound
	}
	if limit == nil {
		return true, 0, nil
	}
	if used == nil {
		remaining := 0
		if retry != nil {
			remaining = *retry
		}
		return false, max(1, remaining), nil
	}
	return true, 0, nil
}

// CleanupProjectKeyRateUsage bounds historical fixed-window rows. The caller
// must retain at least one prior window beyond the configured maximum window.
func (s *Store) CleanupProjectKeyRateUsage(ctx context.Context, olderThan time.Duration, limit int) (int64, error) {
	seconds := int64(olderThan / time.Second)
	if seconds < 1 || limit < 1 || limit > 1000 {
		return 0, errors.New("invalid project-key rate retention")
	}
	command, err := s.pool.Exec(ctx, `DELETE FROM project_key_rate_usage WHERE ctid IN (
  SELECT ctid FROM project_key_rate_usage
  WHERE window_start < clock_timestamp()-make_interval(secs => $1::double precision)
  ORDER BY window_start LIMIT $2)`, seconds, limit)
	if err != nil {
		return 0, err
	}
	return command.RowsAffected(), nil
}
