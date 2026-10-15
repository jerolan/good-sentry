package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/releaseorder"

	"github.com/jackc/pgx/v5"
)

type IssueResolutionInput struct {
	Mode    string
	Version ReleaseVersion
}

type IssueResolutionChange struct{ Policy *IssueResolutionInput }

type IssueResolutionRelease struct {
	Version      ReleaseVersion `json:"version"`
	DateCreated  time.Time      `json:"dateCreated"`
	DateReleased *time.Time     `json:"dateReleased"`
}

type IssueResolution struct {
	Mode       string                  `json:"mode"`
	Comparison string                  `json:"comparison"`
	Release    *IssueResolutionRelease `json:"release"`
	Baseline   *IssueResolutionRelease `json:"baseline"`
}

type IssueReleaseCandidate struct {
	Version      ReleaseVersion `json:"version"`
	Status       string         `json:"status"`
	DateCreated  time.Time      `json:"dateCreated"`
	DateReleased *time.Time     `json:"dateReleased"`
}

const issueResolutionJSON = `(SELECT jsonb_build_object('mode',r.mode,'comparison',r.comparison,
 'release',CASE WHEN r.release_id IS NULL THEN NULL ELSE jsonb_build_object('version',r.release_version,'dateCreated',r.release_created_at,'dateReleased',r.release_released_at) END,
 'baseline',CASE WHEN r.baseline_id IS NULL THEN NULL ELSE jsonb_build_object('version',r.baseline_version,'dateCreated',r.baseline_created_at,'dateReleased',r.baseline_released_at) END)
 FROM issue_resolutions r WHERE r.issue_id=i.id AND i.status='resolved')`

type resolutionRelease struct {
	IssueResolutionRelease
	id int64
}

type issueResolutionPolicy struct {
	mode, comparison  string
	release, baseline *resolutionRelease
	setAt             time.Time
	nextCheckAt       *time.Time
}

func (p *issueResolutionPolicy) public() *IssueResolution {
	if p == nil {
		return nil
	}
	out := &IssueResolution{Mode: p.mode, Comparison: p.comparison}
	if p.release != nil {
		out.Release = &p.release.IssueResolutionRelease
	}
	if p.baseline != nil {
		out.Baseline = &p.baseline.IssueResolutionRelease
	}
	return out
}

func validIssueResolution(change *IssueResolutionChange, status string) bool {
	if change == nil {
		return true
	}
	if status != "resolved" {
		return false
	}
	if change.Policy == nil {
		return true
	}
	p := change.Policy
	if p.Mode == "next_release" {
		return p.Version == ""
	}
	if p.Mode != "release" {
		return false
	}
	_, err := ParseReleaseVersion(string(p.Version))
	return err == nil
}

func issueResolutionRelease(ctx context.Context, tx pgx.Tx, org, project string, version ReleaseVersion) (*resolutionRelease, error) {
	var r resolutionRelease
	err := tx.QueryRow(ctx, `SELECT r.id,r.version,r.created_at,r.date_released FROM releases r
 JOIN release_projects rp ON rp.release_id=r.id WHERE r.organization_id=$1::bigint AND rp.project_id=$2::bigint AND r.version=$3
 FOR SHARE OF r,rp`, org, project, version).Scan(&r.id, &r.Version, &r.DateCreated, &r.DateReleased)
	r.DateCreated = r.DateCreated.UTC()
	r.DateReleased = releaseTimeUTC(r.DateReleased)
	return &r, dbError(err)
}

// New targets are validated under the issue's authorization/serialization locks.
// Equal policies deliberately avoid metadata reads: snapshots survive deletion.
func prepareIssueResolution(ctx context.Context, tx pgx.Tx, i Issue, org string, change IssueUpdate) (*issueResolutionPolicy, bool, error) {
	if change.Status != "resolved" || change.Resolution == nil {
		return nil, false, nil
	}
	wanted := change.Resolution.Policy
	if wanted == nil {
		return nil, i.Resolution != nil, nil
	}
	if i.Status == "resolved" && i.Resolution != nil && i.Resolution.Mode == wanted.Mode &&
		(wanted.Mode == "next_release" || i.Resolution.Release != nil && i.Resolution.Release.Version == wanted.Version) {
		return nil, false, nil
	}
	version := wanted.Version
	if wanted.Mode == "next_release" {
		var err error
		version, err = ParseReleaseVersion(i.Release)
		if err != nil {
			return nil, false, ErrInvalidIssueChange
		}
	}
	anchor, err := issueResolutionRelease(ctx, tx, org, i.ProjectID, version)
	if err != nil {
		if wanted.Mode == "next_release" && errors.Is(err, ErrNotFound) {
			err = ErrInvalidIssueChange
		}
		return nil, false, err
	}
	rows, err := tx.Query(ctx, `SELECT r.version FROM releases r JOIN release_projects rp ON rp.release_id=r.id
 WHERE r.organization_id=$1::bigint AND rp.project_id=$2::bigint ORDER BY r.created_at DESC,r.id DESC LIMIT 10`, org, i.ProjectID)
	if err != nil {
		return nil, false, err
	}
	history, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, false, err
	}
	policy := &issueResolutionPolicy{mode: wanted.Mode, comparison: "created"}
	if _, parseable := releaseorder.Compare(string(version), string(version)); parseable && releaseorder.ProjectUsesSemantic(history) {
		policy.comparison = "semantic"
	}
	if wanted.Mode == "release" {
		policy.release = anchor
	} else {
		policy.baseline = anchor
	}
	return policy, true, nil
}

func replaceIssueResolution(ctx context.Context, tx pgx.Tx, id string, p *issueResolutionPolicy, stamp time.Time) error {
	if _, err := tx.Exec(ctx, "DELETE FROM issue_resolutions WHERE issue_id=$1::bigint", id); err != nil {
		return err
	}
	if p == nil {
		return nil
	}
	var bid, rid *int64
	var bv, rv *ReleaseVersion
	var bc, br, rc, rr *time.Time
	if p.baseline != nil {
		bid, bv, bc, br = &p.baseline.id, &p.baseline.Version, &p.baseline.DateCreated, p.baseline.DateReleased
	}
	if p.release != nil {
		rid, rv, rc, rr = &p.release.id, &p.release.Version, &p.release.DateCreated, p.release.DateReleased
	}
	p.setAt = stamp
	if p.mode == "next_release" && p.release == nil {
		p.nextCheckAt = &stamp
	}
	_, err := tx.Exec(ctx, `INSERT INTO issue_resolutions(issue_id,mode,comparison,set_at,
 baseline_id,baseline_version,baseline_created_at,baseline_released_at,release_id,release_version,release_created_at,release_released_at,next_check_at)
 VALUES($1::bigint,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, id, p.mode, p.comparison, stamp, bid, bv, bc, br, rid, rv, rc, rr, p.nextCheckAt)
	return err
}

// The caller holds the issue lock; this separate statement sees any preceding
// writer's complete policy rather than the snapshot used to acquire that lock.
func loadIssueResolution(ctx context.Context, tx pgx.Tx, id string) (*issueResolutionPolicy, error) {
	var p issueResolutionPolicy
	var bid, rid *int64
	var bv, rv *string
	var bc, br, rc, rr *time.Time
	err := tx.QueryRow(ctx, `SELECT mode,comparison,set_at,baseline_id,baseline_version,baseline_created_at,baseline_released_at,
 release_id,release_version,release_created_at,release_released_at,next_check_at FROM issue_resolutions WHERE issue_id=$1::bigint`, id).
		Scan(&p.mode, &p.comparison, &p.setAt, &bid, &bv, &bc, &br, &rid, &rv, &rc, &rr, &p.nextCheckAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.setAt = p.setAt.UTC()
	p.nextCheckAt = releaseTimeUTC(p.nextCheckAt)
	if bid != nil {
		p.baseline = &resolutionRelease{IssueResolutionRelease: IssueResolutionRelease{Version: ReleaseVersion(*bv), DateCreated: bc.UTC(), DateReleased: releaseTimeUTC(br)}, id: *bid}
	}
	if rid != nil {
		p.release = &resolutionRelease{IssueResolutionRelease: IssueResolutionRelease{Version: ReleaseVersion(*rv), DateCreated: rc.UTC(), DateReleased: releaseTimeUTC(rr)}, id: *rid}
	}
	return &p, nil
}

func (s *Store) IssueReleases(ctx context.Context, userID, id, query string, page int) ([]IssueReleaseCandidate, int, error) {
	offset, err := issuePage(page)
	if err != nil {
		return nil, 0, err
	}
	if len(query) > 200 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
		return nil, 0, ErrInvalidIssueChange
	}
	tx, i, org, _, err := s.issueTransaction(ctx, userID, id, false)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	pattern := "%" + issueSearchPattern(query, false) + "%"
	from := ` FROM releases r JOIN release_projects rp ON rp.release_id=r.id
 WHERE r.organization_id=$1::bigint AND rp.project_id=$2::bigint AND r.version ILIKE $3 ESCAPE E'\\' `
	var total int
	if err = tx.QueryRow(ctx, "SELECT count(*)"+from, org, i.ProjectID, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, "SELECT r.version,r.status,r.created_at,r.date_released"+from+"ORDER BY r.created_at DESC,r.id DESC LIMIT 25 OFFSET $4", org, i.ProjectID, pattern, offset)
	if err != nil {
		return nil, 0, err
	}
	items := []IssueReleaseCandidate{}
	for rows.Next() {
		var r IssueReleaseCandidate
		if err = rows.Scan(&r.Version, &r.Status, &r.DateCreated, &r.DateReleased); err != nil {
			rows.Close()
			return nil, 0, err
		}
		r.DateCreated, r.DateReleased = r.DateCreated.UTC(), releaseTimeUTC(r.DateReleased)
		items = append(items, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return items, total, tx.Commit(ctx)
}
