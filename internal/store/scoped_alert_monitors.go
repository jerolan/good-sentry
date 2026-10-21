package store

import (
	"context"

	"goodsentry/internal/notify"
	"goodsentry/internal/uptime"
)

// ScopedAlerts preserves the native owner/admin alert boundary while limiting
// project rows to the fixed organization and token selection in SQL.
func (s *Store) ScopedAlerts(ctx context.Context, access ManagementAccess, project string, offset, limit int) ([]notify.Rule, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || offset < 0 || offset > 100000000 || limit < 1 || limit > 100 {
		return nil, ErrNotFound
	}
	var role string
	if err := s.pool.QueryRow(ctx, `SELECT role FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint`, access.UserID, access.OrganizationID).Scan(&role); err != nil {
		return nil, dbError(err)
	}
	if role != "owner" && role != "admin" {
		return nil, ErrForbidden
	}
	var projectID int64
	if project != "" {
		selected, err := s.ManagementProject(ctx, access, project)
		if err != nil {
			return nil, err
		}
		projectID = referenceID(selected.ID)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+alertColumns+` FROM alert_rules
 WHERE project_id IN (SELECT p.id FROM projects p
 JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
 WHERE p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[]))
 AND ($5::bigint=0 OR p.id=$5))
 ORDER BY id LIMIT $6 OFFSET $7`, access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs, projectID, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []notify.Rule{}
	for rows.Next() {
		item, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ScopedMonitors includes organization-level monitors and only selected
// project-linked monitors for the current member.
func (s *Store) ScopedMonitors(ctx context.Context, access ManagementAccess, offset, limit int) ([]uptime.Monitor, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || offset < 0 || offset > 100000000 || limit < 1 || limit > 100 {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT `+monitorColumns+` FROM monitors m
 LEFT JOIN projects p ON p.id=m.project_id
 JOIN memberships mb ON mb.organization_id=m.organization_id AND mb.user_id=$1::bigint
 WHERE m.organization_id=$2::bigint
 AND (m.project_id IS NULL OR $3 OR m.project_id=ANY($4::bigint[]))
 ORDER BY m.created_at DESC,m.id DESC LIMIT $5 OFFSET $6`, access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []uptime.Monitor{}
	for rows.Next() {
		item, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
