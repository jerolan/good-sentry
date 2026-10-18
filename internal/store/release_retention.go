package store

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// CleanupReleases expires metadata independently of retained file bytes.
func (s *Store) CleanupReleases(ctx context.Context, days int) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':release-retention',0))`).Scan(&acquired); err != nil {
		return 0, err
	}
	if !acquired {
		return 0, nil
	}
	var cutoff time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()-($1::integer*interval '24 hours')`, days).Scan(&cutoff); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id,organization_id,version FROM releases
 WHERE created_at<$1 ORDER BY created_at,id LIMIT 250`, cutoff)
	if err != nil {
		return 0, err
	}
	type identity struct {
		id, organization int64
		version          string
	}
	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (identity, error) {
		var release identity
		err := row.Scan(&release.id, &release.organization, &release.version)
		return release, err
	})
	if err != nil {
		return 0, err
	}
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(`+releaseIdentityLockKeySQL+`)`, strconv.FormatInt(candidate.organization, 10), candidate.version).Scan(&acquired); err != nil {
			return 0, err
		}
		if acquired {
			ids = append(ids, candidate.id)
		}
	}
	removed, err := tx.Exec(ctx, `WITH expired AS (
 SELECT id FROM releases WHERE id=ANY($1::bigint[]) AND created_at<$2
 ORDER BY created_at,id FOR UPDATE SKIP LOCKED
) DELETE FROM releases r USING expired e WHERE r.id=e.id`, ids, cutoff)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return removed.RowsAffected(), nil
}
