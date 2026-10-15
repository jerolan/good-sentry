package store

import (
	"context"
	"errors"
	"time"

	"goodsentry/internal/releaseorder"

	"github.com/jackc/pgx/v5"
)

func (r *resolutionRelease) effective() time.Time {
	if r.DateReleased != nil {
		return *r.DateReleased
	}
	return r.DateCreated
}

func releaseTupleCompare(aTime time.Time, aID int64, bTime time.Time, bID int64) int {
	if aTime.Before(bTime) {
		return -1
	}
	if aTime.After(bTime) {
		return 1
	}
	if aID < bID {
		return -1
	}
	if aID > bID {
		return 1
	}
	return 0
}

// This single statement defines the binding snapshot. Semantic and incomparable
// candidates are separate groups, never a sort with a non-transitive fallback.
func nextIssueRelease(ctx context.Context, tx pgx.Tx, project string, p *issueResolutionPolicy) (*resolutionRelease, error) {
	rows, err := tx.Query(ctx, `SELECT r.id,r.version,r.created_at,r.date_released FROM releases r
 JOIN release_projects rp ON rp.release_id=r.id WHERE rp.project_id=$1::bigint AND r.status='open'
 AND r.version<>$2 AND COALESCE(r.date_released,r.created_at)>$3`, project, p.baseline.Version, p.setAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var best *resolutionRelease
	bestSemantic := false
	for rows.Next() {
		var candidate resolutionRelease
		if err = rows.Scan(&candidate.id, &candidate.Version, &candidate.DateCreated, &candidate.DateReleased); err != nil {
			return nil, err
		}
		candidate.DateCreated = candidate.DateCreated.UTC()
		candidate.DateReleased = releaseTimeUTC(candidate.DateReleased)
		semantic := false
		if p.comparison == "semantic" {
			order, comparable := releaseorder.Compare(string(candidate.Version), string(p.baseline.Version))
			if comparable && order <= 0 {
				continue
			}
			semantic = comparable
		}
		choose := best == nil || semantic && !bestSemantic
		if best != nil && semantic == bestSemantic {
			order := 0
			if semantic {
				order, _ = releaseorder.Compare(string(candidate.Version), string(best.Version))
			}
			choose = order < 0 || order == 0 && releaseTupleCompare(candidate.effective(), candidate.id, best.effective(), best.id) < 0
		}
		if choose {
			copy := candidate
			best = &copy
			bestSemantic = semantic
		}
	}
	return best, rows.Err()
}

func bindLockedIssueRelease(ctx context.Context, tx pgx.Tx, id, project string, p *issueResolutionPolicy) (bool, error) {
	if p == nil || p.mode != "next_release" || p.release != nil {
		return false, nil
	}
	candidate, err := nextIssueRelease(ctx, tx, project, p)
	if err != nil || candidate == nil {
		return false, err
	}
	p.release, p.nextCheckAt = candidate, nil
	if _, err = tx.Exec(ctx, `UPDATE issue_resolutions SET release_id=$2,release_version=$3,
 release_created_at=$4,release_released_at=$5,next_check_at=NULL WHERE issue_id=$1::bigint`, id, candidate.id, candidate.Version, candidate.DateCreated, candidate.DateReleased); err != nil {
		return false, err
	}
	_, err = recordIssueActivity(ctx, tx, id, nil, "resolution_bound", map[string]any{"resolution": p.public()})
	return true, err
}

// Called after fresh-only release observation and before the outbox stage. The
// caller still holds the issue and matching frequency-rule locks.
func settleIssueResolution(ctx context.Context, tx pgx.Tx, id, project, eventID, version string) error {
	var status string
	var resolvedAt *time.Time
	if err := tx.QueryRow(ctx, "SELECT status,resolved_at FROM issues WHERE id=$1::bigint", id).Scan(&status, &resolvedAt); err != nil {
		return err
	}
	if status != "resolved" {
		return nil
	}
	p, err := loadIssueResolution(ctx, tx, id)
	if err != nil {
		return err
	}
	reason := ""
	if p != nil {
		if _, err = bindLockedIssueRelease(ctx, tx, id, project, p); err != nil {
			return err
		}
		parsed, parseErr := ParseReleaseVersion(version)
		if parseErr != nil {
			reason = "missing_release"
		} else if p.release != nil {
			exact := parsed == p.release.Version
			regress := p.mode == "next_release" && exact
			if !exact {
				order, comparable := releaseorder.Compare(version, string(p.release.Version))
				if p.comparison == "semantic" && comparable {
					regress = order > 0
				} else {
					// Observation just inserted/associated this valid identity in this TX.
					var candidate resolutionRelease
					if err = tx.QueryRow(ctx, `SELECT r.id,r.created_at,r.date_released FROM releases r
      JOIN release_projects rp ON rp.release_id=r.id WHERE rp.project_id=$1::bigint AND r.version=$2`, project, parsed).
						Scan(&candidate.id, &candidate.DateCreated, &candidate.DateReleased); err != nil {
						return err
					}
					if p.mode == "release" {
						regress = releaseTupleCompare(candidate.DateCreated, candidate.id, p.release.DateCreated, p.release.id) > 0
					} else {
						regress = releaseTupleCompare(candidate.effective(), candidate.id, p.release.effective(), p.release.id) >= 0
					}
				}
			}
			if regress {
				reason = "newer_release"
				if p.mode == "next_release" {
					reason = "next_release"
				}
			}
		}
		if reason == "" {
			return nil
		}
	}
	data := map[string]any{"eventID": eventID, "resolvedAt": releaseTimeUTC(resolvedAt)}
	if p != nil {
		data["resolution"], data["reason"] = p.public(), reason
	}
	stamp, err := recordIssueActivity(ctx, tx, id, nil, "regressed", data)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM issue_resolutions WHERE issue_id=$1::bigint", id); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE issues SET status='unresolved',resolved_at=NULL,regressed_at=$2 WHERE id=$1::bigint`, id, stamp)
	return err
}

// BindNextIssueRelease reports claim progress. A pending row without a candidate
// is durably deferred, so a restarted or replicated worker cannot spin on it.
func (s *Store) BindNextIssueRelease(ctx context.Context, interval time.Duration) (bool, error) {
	if interval <= 0 {
		return false, errors.New("issue maintenance interval must be positive")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `SELECT i.id::text FROM issues i JOIN issue_resolutions r ON r.issue_id=i.id
 WHERE i.merged_into IS NULL AND i.status='resolved' AND r.mode='next_release' AND r.release_id IS NULL AND r.next_check_at<=clock_timestamp()
 ORDER BY r.next_check_at,i.id LIMIT 1 FOR UPDATE OF i SKIP LOCKED`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	p, err := loadIssueResolution(ctx, tx, id)
	if err != nil {
		return false, err
	}
	var status, project string
	var merged *string
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT status,project_id::text,clock_timestamp(),merged_into::text FROM issues WHERE id=$1::bigint", id).Scan(&status, &project, &now, &merged); err != nil {
		return false, err
	}
	if merged == nil && status == "resolved" && p != nil && p.mode == "next_release" && p.release == nil && p.nextCheckAt != nil && !p.nextCheckAt.After(now) {
		bound, err := bindLockedIssueRelease(ctx, tx, id, project, p)
		if err != nil {
			return false, err
		}
		if !bound {
			now, err = alertNow(ctx, tx)
			if err != nil {
				return false, err
			}
			if _, err = tx.Exec(ctx, "UPDATE issue_resolutions SET next_check_at=$2 WHERE issue_id=$1::bigint", id, now.Add(interval)); err != nil {
				return false, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
