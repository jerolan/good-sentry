package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type APITokenInput struct {
	Name          string
	Scopes        []string
	ProjectIDs    []int64
	ExpiresInDays int
}

type APITokenProject struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type APIToken struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Prefix      string            `json:"prefix"`
	Scopes      []string          `json:"scopes"`
	AllProjects bool              `json:"allProjects"`
	Projects    []APITokenProject `json:"projects"`
	CreatedBy   string            `json:"createdBy"`
	DateCreated time.Time         `json:"dateCreated"`
	ExpiresAt   time.Time         `json:"expiresAt"`
	RevokedAt   *time.Time        `json:"revokedAt"`
}

type APITokensPage struct {
	Data     []APIToken `json:"data"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
	HasMore  bool       `json:"hasMore"`
}

type APITokenGrant struct {
	User           User
	OrganizationID string
	AllProjects    bool
	ProjectIDs     []int64
	Scopes         []string
}

const apiTokenColumns = `t.id::text,t.name,t.prefix,t.scopes,t.all_projects,t.user_id::text,
	t.created_at,t.expires_at,t.revoked_at,COALESCE((SELECT jsonb_agg(jsonb_build_object(
	'id',p.id::text,'slug',p.slug,'name',p.name) ORDER BY p.id)
	FROM api_token_projects tp JOIN projects p ON p.id=tp.project_id
	WHERE tp.token_id=t.id AND p.organization_id=t.organization_id),'[]'::jsonb)`

func scanAPIToken(row interface{ Scan(...any) error }) (APIToken, error) {
	var t APIToken
	var projects []byte
	err := row.Scan(&t.ID, &t.Name, &t.Prefix, &t.Scopes, &t.AllProjects, &t.CreatedBy,
		&t.DateCreated, &t.ExpiresAt, &t.RevokedAt, &projects)
	if err != nil {
		return t, dbError(err)
	}
	err = json.Unmarshal(projects, &t.Projects)
	return t, err
}

func tokenAdministrator(ctx context.Context, tx pgx.Tx, userID, orgSlug string, lock bool) (string, error) {
	query := `SELECT o.id::text,m.role FROM organizations o JOIN memberships m ON m.organization_id=o.id
		WHERE m.user_id=$1::bigint AND o.slug=$2`
	if lock {
		query += " FOR SHARE OF m"
	}
	var orgID, role string
	if err := tx.QueryRow(ctx, query, userID, orgSlug).Scan(&orgID, &role); err != nil {
		return "", dbError(err)
	}
	if role != "owner" && role != "admin" {
		return "", ErrForbidden
	}
	return orgID, nil
}

func (s *Store) CreateAPIToken(ctx context.Context, userID, orgSlug string, input APITokenInput, secretHash, prefix string) (APIToken, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return APIToken{}, err
	}
	defer tx.Rollback(ctx)
	orgID, err := tokenAdministrator(ctx, tx, userID, orgSlug, true)
	if err != nil {
		return APIToken{}, err
	}
	// Lock the selected projects until the token and all grants commit together.
	rows, err := tx.Query(ctx, `SELECT id FROM projects WHERE organization_id=$1::bigint AND id=ANY($2::bigint[]) ORDER BY id FOR SHARE`, orgID, input.ProjectIDs)
	if err != nil {
		return APIToken{}, err
	}
	selected, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return APIToken{}, err
	}
	requested := make(map[int64]bool, len(input.ProjectIDs))
	for _, id := range input.ProjectIDs {
		requested[id] = true
	}
	if len(selected) != len(requested) {
		return APIToken{}, ErrNotFound
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO api_tokens(organization_id,user_id,name,secret_hash,prefix,scopes,all_projects,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,now()+$8::integer*interval '1 day') RETURNING id::text`,
		orgID, userID, input.Name, secretHash, prefix, input.Scopes, len(input.ProjectIDs) == 0, input.ExpiresInDays).Scan(&id)
	if err != nil {
		return APIToken{}, dbError(err)
	}
	if len(selected) > 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO api_token_projects(token_id,project_id) SELECT $1::bigint,unnest($2::bigint[])`, id, selected); err != nil {
			return APIToken{}, err
		}
	}
	result, err := scanAPIToken(tx.QueryRow(ctx, "SELECT "+apiTokenColumns+" FROM api_tokens t WHERE t.id=$1::bigint", id))
	if err != nil {
		return APIToken{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return APIToken{}, err
	}
	return result, nil
}

func (s *Store) APITokens(ctx context.Context, userID, orgSlug string, page int) (APITokensPage, error) {
	result := APITokensPage{Data: []APIToken{}, Page: page, PageSize: 25}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	orgID, err := tokenAdministrator(ctx, tx, userID, orgSlug, false)
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, "SELECT "+apiTokenColumns+` FROM api_tokens t WHERE t.organization_id=$1::bigint
		ORDER BY t.created_at DESC,t.id DESC LIMIT 26 OFFSET $2`, orgID, int64(page-1)*25)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return result, err
		}
		result.Data = append(result.Data, t)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Data) > 25 {
		result.HasMore = true
		result.Data = result.Data[:25]
	}
	return result, tx.Commit(ctx)
}

func (s *Store) RevokeAPIToken(ctx context.Context, userID, orgSlug, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	orgID, err := tokenAdministrator(ctx, tx, userID, orgSlug, true)
	if err != nil {
		return err
	}
	var found string
	err = tx.QueryRow(ctx, `UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,now())
		WHERE organization_id=$1::bigint AND id=$2::bigint RETURNING id::text`, orgID, id).Scan(&found)
	if err != nil {
		return dbError(err)
	}
	return tx.Commit(ctx)
}

func (s *Store) AuthenticateAPIToken(ctx context.Context, secretHash string) (APITokenGrant, error) {
	var grant APITokenGrant
	err := s.pool.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,t.organization_id::text,t.all_projects,t.scopes,
		ARRAY(SELECT p.id FROM api_token_projects tp JOIN projects p ON p.id=tp.project_id
		WHERE tp.token_id=t.id AND p.organization_id=t.organization_id ORDER BY p.id)
		FROM api_tokens t JOIN users u ON u.id=t.user_id
		JOIN memberships m ON m.organization_id=t.organization_id AND m.user_id=t.user_id
		WHERE t.secret_hash=$1 AND t.revoked_at IS NULL AND t.expires_at>now()`, secretHash).
		Scan(&grant.User.ID, &grant.User.Email, &grant.User.Name, &grant.OrganizationID, &grant.AllProjects, &grant.Scopes, &grant.ProjectIDs)
	return grant, dbError(err)
}
