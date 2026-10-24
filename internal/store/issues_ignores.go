package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type IssueIgnoreCondition struct {
	Minutes   *int32 `json:"minutes"`
	Count     *int32 `json:"count"`
	UserCount *int32 `json:"userCount"`
}

type IssueIgnoreChange struct{ Condition *IssueIgnoreCondition }

type IssueIgnore struct {
	Minutes       *int32     `json:"minutes"`
	Count         *int32     `json:"count"`
	UserCount     *int32     `json:"userCount"`
	Until         *time.Time `json:"until"`
	Remaining     *int64     `json:"remaining"`
	UserRemaining *int64     `json:"userRemaining"`
}

const issueIgnoreJSON = `(SELECT jsonb_build_object('minutes',g.minutes,'count',g.count,'userCount',g.user_count,'until',g.until,
 'remaining',CASE WHEN g.count IS NULL THEN NULL ELSE GREATEST(0,g.count::bigint-GREATEST(0,i.receipt_count-g.baseline)) END,
 'userRemaining',CASE WHEN g.user_count IS NULL THEN NULL ELSE GREATEST(0,g.user_count::bigint-(SELECT count(*) FROM issue_ignore_users u WHERE u.issue_id=i.id AND NOT u.baseline)) END)
 FROM issue_ignores g WHERE g.issue_id=i.id AND i.status='ignored')`

func validIssueIgnore(change *IssueIgnoreChange, status string) bool {
	if change == nil {
		return true
	}
	if status != "ignored" {
		return false
	}
	c := change.Condition
	return c == nil || (c.Minutes != nil || c.Count != nil || c.UserCount != nil) && (c.Minutes == nil || *c.Minutes > 0) && (c.Count == nil || *c.Count > 0) && (c.UserCount == nil || *c.UserCount > 0)
}

func sameIssueIgnore(current *IssueIgnore, wanted *IssueIgnoreCondition) bool {
	if current == nil || wanted == nil {
		return current == nil && wanted == nil
	}
	return sameIgnoreNumber(current.Minutes, wanted.Minutes) && sameIgnoreNumber(current.Count, wanted.Count) && sameIgnoreNumber(current.UserCount, wanted.UserCount)
}

func sameIgnoreNumber(a, b *int32) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (s *Store) replaceIssueIgnore(ctx context.Context, tx pgx.Tx, i *Issue, organizationID string, condition *IssueIgnoreCondition) error {
	if condition == nil {
		if _, err := tx.Exec(ctx, "DELETE FROM issue_ignore_users WHERE issue_id=$1::bigint", i.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "DELETE FROM issue_ignores WHERE issue_id=$1::bigint", i.ID)
		i.Ignore = nil
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM issue_ignore_users WHERE issue_id=$1::bigint", i.ID); err != nil {
		return err
	}
	if condition.UserCount != nil {
		if organizationID == "" {
			var err error
			organizationID, err = s.issueOrganization(ctx, tx, i.ID)
			if err != nil {
				return err
			}
		}
		if err := s.seedIssueIgnoreUserBaseline(ctx, tx, i.ID, nil, organizationID); err != nil {
			return err
		}
	}
	var until *time.Time
	if condition.Minutes != nil {
		var now time.Time
		if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return err
		}
		// Positive int32 minutes exceed time.Duration's range. These are elapsed
		// seconds, so Unix arithmetic also avoids civil-time and DST adjustments.
		stamp := time.Unix(now.Unix()+int64(*condition.Minutes)*60, int64(now.Nanosecond())).UTC()
		until = &stamp
	}
	_, err := tx.Exec(ctx, `INSERT INTO issue_ignores(issue_id,minutes,count,user_count,until,baseline)
  SELECT id,$2,$3,$4,$5,receipt_count FROM issues WHERE id=$1::bigint
  ON CONFLICT(issue_id) DO UPDATE SET minutes=EXCLUDED.minutes,count=EXCLUDED.count,user_count=EXCLUDED.user_count,until=EXCLUDED.until,baseline=EXCLUDED.baseline`, i.ID, condition.Minutes, condition.Count, condition.UserCount, until)
	if err != nil {
		return err
	}
	i.Ignore = &IssueIgnore{Minutes: condition.Minutes, Count: condition.Count, UserCount: condition.UserCount, Until: until}
	if condition.Count != nil {
		remaining := int64(*condition.Count)
		i.Ignore.Remaining = &remaining
	}
	if condition.UserCount != nil {
		remaining := int64(*condition.UserCount)
		i.Ignore.UserRemaining = &remaining
	}
	return nil
}

// The caller holds the issue row lock. Read the related policy in a fresh
// statement: a preceding writer may have replaced it while that lock was awaited.
func expireLockedIssueIgnore(ctx context.Context, tx pgx.Tx, id string, additional int64, eventID string) (bool, error) {
	var status string
	var receipts int64
	var minutes, count, userCount *int32
	var until *time.Time
	var baseline *int64
	var merged *string
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT i.status,i.receipt_count,g.minutes,g.count,g.user_count,g.until,g.baseline,clock_timestamp(),i.merged_into::text
  FROM issues i LEFT JOIN issue_ignores g ON g.issue_id=i.id WHERE i.id=$1::bigint`, id).Scan(&status, &receipts, &minutes, &count, &userCount, &until, &baseline, &now, &merged)
	if err != nil {
		return false, err
	}
	if merged != nil || status != "ignored" || baseline == nil {
		return false, nil
	}
	previous := IssueIgnore{Minutes: minutes, Count: count, UserCount: userCount, Until: until}
	if until != nil {
		stamp := until.UTC()
		previous.Until = &stamp
	}
	if count != nil {
		// A capture is admitted exactly once before this check. Include it in the
		// previous-policy snapshot, so the Nth fresh capture records remaining=0.
		remaining := max(int64(0), int64(*count)-max(int64(0), receipts-*baseline)-additional)
		previous.Remaining = &remaining
	}
	if userCount != nil {
		var seen int64
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM issue_ignore_users WHERE issue_id=$1::bigint AND NOT baseline`, id).Scan(&seen); err != nil {
			return false, err
		}
		remaining := max(int64(0), int64(*userCount)-seen)
		previous.UserRemaining = &remaining
	}
	reason := ""
	if until != nil && !now.Before(*until) {
		reason = "duration"
	} else if previous.Remaining != nil && *previous.Remaining == 0 {
		reason = "count"
	} else if previous.UserRemaining != nil && *previous.UserRemaining == 0 {
		reason = "user_count"
	}
	if reason == "" {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE issues SET status='unresolved',resolved_at=NULL,regressed_at=NULL WHERE id=$1::bigint`, id); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM issue_ignores WHERE issue_id=$1::bigint", id); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM issue_ignore_users WHERE issue_id=$1::bigint", id); err != nil {
		return false, err
	}
	data := map[string]any{"reason": reason, "ignore": previous}
	if eventID != "" {
		data["eventID"] = eventID
	}
	if _, err = recordIssueActivity(ctx, tx, id, nil, "unignored", data); err != nil {
		return false, err
	}
	return true, nil
}

// ExpireIssueIgnore processes at most one due candidate. The bool reports claim
// progress, including a candidate renewed since the join snapshot was read.
// Only an empty claim permits the worker to idle. SKIP LOCKED lets replicas
// drain separate issues without waiting on capture or native mutation locks.
func (s *Store) ExpireIssueIgnore(ctx context.Context) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `SELECT i.id::text FROM issues i JOIN issue_ignores g ON g.issue_id=i.id
  WHERE i.merged_into IS NULL AND i.status='ignored' AND g.until<=clock_timestamp() ORDER BY g.until,i.id LIMIT 1 FOR UPDATE OF i SKIP LOCKED`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = expireLockedIssueIgnore(ctx, tx, id, 0, "")
	if err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
