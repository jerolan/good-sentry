package store

import (
	"context"
	"strconv"
	"time"
)

type ManagementAccess struct {
	UserID         string
	OrganizationID string
	AllProjects    bool
	ProjectIDs     []int64
}

type ManagementProject struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Platform    string    `json:"platform"`
	DateCreated time.Time `json:"dateCreated"`
}

func referenceID(value string) int64 {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0
	}
	return id
}

func (s *Store) ManagementOrganization(ctx context.Context, userID, reference string) (Organization, error) {
	var org Organization
	err := s.pool.QueryRow(ctx, `SELECT o.id::text,o.slug,o.name,m.role FROM organizations o
		JOIN memberships m ON m.organization_id=o.id WHERE m.user_id=$1::bigint
		AND (o.slug=$2 OR o.id=$3) ORDER BY (o.slug=$2) DESC LIMIT 1`, userID, reference, referenceID(reference)).
		Scan(&org.ID, &org.Slug, &org.Name, &org.Role)
	return org, dbError(err)
}

func (s *Store) ManagementProjects(ctx context.Context, access ManagementAccess, query string, offset, limit int) ([]ManagementProject, error) {
	var member bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint)`, access.UserID, access.OrganizationID).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT p.id::text,p.name,p.slug,p.platform,p.created_at FROM projects p
		JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
		WHERE p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[]))
		AND ($5='' OR strpos(lower(p.name),lower($5))>0 OR strpos(lower(p.slug),lower($5))>0)
		ORDER BY p.id LIMIT $6 OFFSET $7`, access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs, query, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ManagementProject{}
	for rows.Next() {
		var p ManagementProject
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Platform, &p.DateCreated); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) ManagementProject(ctx context.Context, access ManagementAccess, project string) (ManagementProject, error) {
	var p ManagementProject
	err := s.pool.QueryRow(ctx, `SELECT p.id::text,p.name,p.slug,p.platform,p.created_at FROM projects p
		JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
		WHERE p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[]))
		AND (p.slug=$5 OR p.id=$6) ORDER BY (p.slug=$5) DESC LIMIT 1`,
		access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs, project, referenceID(project)).
		Scan(&p.ID, &p.Name, &p.Slug, &p.Platform, &p.DateCreated)
	return p, dbError(err)
}
