package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"goodsentry/internal/uptime"

	"github.com/jackc/pgx/v5"
)

type StatusPage struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Slug      string           `json:"slug"`
	IsPublic  bool             `json:"isPublic"`
	Monitors  []uptime.Monitor `json:"monitors"`
	CreatedAt time.Time        `json:"createdAt"`
}

func validStatusPageName(name string) bool {
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 200 && !strings.ContainsFunc(name, unicode.IsControl)
}

func (s *Store) StatusPages(ctx context.Context, userID, orgSlug string) ([]StatusPage, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return nil, err
	}
	return s.statusPages(ctx, org.ID)
}

func (s *Store) PublicStatusPage(ctx context.Context, slug string) (StatusPage, error) {
	var page StatusPage
	err := s.pool.QueryRow(ctx, `SELECT id::text,name,slug,is_public,created_at
FROM status_pages WHERE slug=$1 AND is_public`, slug).
		Scan(&page.ID, &page.Name, &page.Slug, &page.IsPublic, &page.CreatedAt)
	if err != nil {
		return page, dbError(err)
	}
	page.Monitors, err = s.statusPageMonitors(ctx, page.ID)
	return page, err
}

func (s *Store) CreateStatusPage(ctx context.Context, userID, orgSlug, name string, isPublic bool, monitorIDs []string) (StatusPage, error) {
	org, err := s.monitorWriteOrganization(ctx, userID, orgSlug)
	if err != nil {
		return StatusPage{}, err
	}
	name = strings.TrimSpace(name)
	if !validStatusPageName(name) {
		return StatusPage{}, ErrInvalidStatusPage
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return StatusPage{}, err
	}
	defer tx.Rollback(ctx)
	if err = statusPageMonitorsBelong(ctx, tx, org.ID, monitorIDs); err != nil {
		return StatusPage{}, err
	}
	slug, err := statusPageSlug(ctx, tx, name)
	if err != nil {
		return StatusPage{}, err
	}
	var id string
	if err = tx.QueryRow(ctx, `INSERT INTO status_pages(organization_id,name,slug,is_public)
VALUES($1::bigint,$2,$3,$4) RETURNING id::text`, org.ID, name, slug, isPublic).Scan(&id); err != nil {
		return StatusPage{}, dbError(err)
	}
	if err = replaceStatusPageMonitors(ctx, tx, id, monitorIDs); err != nil {
		return StatusPage{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return StatusPage{}, err
	}
	return s.statusPageByID(ctx, id)
}

func (s *Store) UpdateStatusPage(ctx context.Context, userID, orgSlug, ref, name string, isPublic bool, monitorIDs []string) (StatusPage, error) {
	org, err := s.monitorWriteOrganization(ctx, userID, orgSlug)
	if err != nil {
		return StatusPage{}, err
	}
	name = strings.TrimSpace(name)
	if !validStatusPageName(name) {
		return StatusPage{}, ErrInvalidStatusPage
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return StatusPage{}, err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM status_pages
WHERE organization_id::text=$1 AND (id::text=$2 OR slug=$2) FOR UPDATE`, org.ID, ref).Scan(&id)
	if err != nil {
		return StatusPage{}, dbError(err)
	}
	if err = statusPageMonitorsBelong(ctx, tx, org.ID, monitorIDs); err != nil {
		return StatusPage{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE status_pages SET name=$2,is_public=$3,updated_at=now() WHERE id::text=$1`,
		id, name, isPublic); err != nil {
		return StatusPage{}, err
	}
	if err = replaceStatusPageMonitors(ctx, tx, id, monitorIDs); err != nil {
		return StatusPage{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return StatusPage{}, err
	}
	return s.statusPageByID(ctx, id)
}

func (s *Store) DeleteStatusPage(ctx context.Context, userID, orgSlug, ref string) error {
	org, err := s.monitorWriteOrganization(ctx, userID, orgSlug)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM status_pages WHERE organization_id::text=$1 AND (id::text=$2 OR slug=$2)`, org.ID, ref)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) statusPages(ctx context.Context, orgID string) ([]StatusPage, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,name,slug,is_public,created_at
FROM status_pages WHERE organization_id::text=$1 ORDER BY created_at DESC,id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pages := []StatusPage{}
	for rows.Next() {
		var page StatusPage
		if err := rows.Scan(&page.ID, &page.Name, &page.Slug, &page.IsPublic, &page.CreatedAt); err != nil {
			return nil, err
		}
		page.Monitors, err = s.statusPageMonitors(ctx, page.ID)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, rows.Err()
}

func (s *Store) statusPageByID(ctx context.Context, id string) (StatusPage, error) {
	var page StatusPage
	err := s.pool.QueryRow(ctx, `SELECT id::text,name,slug,is_public,created_at FROM status_pages WHERE id::text=$1`, id).
		Scan(&page.ID, &page.Name, &page.Slug, &page.IsPublic, &page.CreatedAt)
	if err != nil {
		return page, dbError(err)
	}
	page.Monitors, err = s.statusPageMonitors(ctx, id)
	return page, err
}

func (s *Store) statusPageMonitors(ctx context.Context, pageID string) ([]uptime.Monitor, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+monitorColumns+monitorJoin+`
JOIN status_page_monitors spm ON spm.monitor_id=m.id
WHERE spm.status_page_id::text=$1 ORDER BY m.name,m.id`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []uptime.Monitor{}
	for rows.Next() {
		monitor, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, monitor)
	}
	return items, rows.Err()
}

func statusPageMonitorsBelong(ctx context.Context, tx pgx.Tx, orgID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM monitors WHERE organization_id::text=$1 AND id::text=ANY($2::text[])`, orgID, ids).Scan(&count)
	if err != nil {
		return err
	}
	unique := map[string]bool{}
	for _, id := range ids {
		if !canonicalIssueID(id) {
			return ErrNotFound
		}
		unique[id] = true
	}
	if count != len(unique) {
		return ErrNotFound
	}
	return nil
}

func replaceStatusPageMonitors(ctx context.Context, tx pgx.Tx, pageID string, ids []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM status_page_monitors WHERE status_page_id::text=$1`, pageID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO status_page_monitors(status_page_id,monitor_id)
VALUES($1::bigint,$2::bigint) ON CONFLICT DO NOTHING`, pageID, id); err != nil {
			return err
		}
	}
	return nil
}

func statusPageSlug(ctx context.Context, tx pgx.Tx, name string) (string, error) {
	base := Slug(name)
	if base == "" {
		base = "status"
	}
	for attempt := 0; attempt < 5; attempt++ {
		candidate := base
		if attempt > 0 {
			key, err := randomKey()
			if err != nil {
				return "", err
			}
			candidate = base
			if len(candidate) > 190 {
				candidate = candidate[:190]
			}
			candidate += "-" + key[:8]
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM status_pages WHERE slug=$1)`, candidate).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", errors.New("could not allocate status-page slug")
}
