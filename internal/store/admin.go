package store

import "context"

// AdminOrganization is the non-secret operational summary used by the
// opt-in administration surface. It is limited to organizations where the
// caller currently has owner or admin authority.
type AdminOrganization struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Role     string `json:"role"`
	Projects int64  `json:"projects"`
	Members  int64  `json:"members"`
	Issues   int64  `json:"issues"`
	Events   int64  `json:"events"`
	Logs     int64  `json:"logs"`
}

func (s *Store) AdminOrganizations(ctx context.Context, userID string) ([]AdminOrganization, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id::text,o.name,o.slug,m.role,
		(SELECT count(*) FROM projects p WHERE p.organization_id=o.id),
		(SELECT count(*) FROM memberships mm WHERE mm.organization_id=o.id),
		(SELECT count(*) FROM issues i JOIN projects p ON p.id=i.project_id WHERE p.organization_id=o.id),
		(SELECT count(*) FROM events e JOIN projects p ON p.id=e.project_id WHERE p.organization_id=o.id),
		(SELECT count(*) FROM logs l JOIN projects p ON p.id=l.project_id WHERE p.organization_id=o.id)
		FROM organizations o JOIN memberships m ON m.organization_id=o.id
		WHERE m.user_id=$1::bigint AND m.role IN ('owner','admin')
		ORDER BY lower(o.name),o.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AdminOrganization{}
	for rows.Next() {
		var organization AdminOrganization
		if err := rows.Scan(&organization.ID, &organization.Name, &organization.Slug, &organization.Role, &organization.Projects, &organization.Members, &organization.Issues, &organization.Events, &organization.Logs); err != nil {
			return nil, err
		}
		result = append(result, organization)
	}
	return result, rows.Err()
}
