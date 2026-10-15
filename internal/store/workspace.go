package store

import (
	"context"
	"time"
)

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash []byte `json:"-"`
}
type Organization struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type Project struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Platform    string    `json:"platform"`
	PublicKey   string    `json:"publicKey,omitempty"`
	DSN         string    `json:"dsn"`
	DateCreated time.Time `json:"dateCreated"`
}

func (s *Store) SetupRequired(ctx context.Context) (bool, error) {
	var needed bool
	err := s.pool.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM users)").Scan(&needed)
	return needed, err
}

func (s *Store) Bootstrap(ctx context.Context, email string, password []byte, name, organization string) (User, Organization, error) {
	var u User
	var o Organization
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return u, o, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73042190)"); err != nil {
		return u, o, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users)").Scan(&exists); err != nil {
		return u, o, err
	}
	if exists {
		return u, o, ErrConflict
	}
	err = tx.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text,email,name", email, password, name).Scan(&u.ID, &u.Email, &u.Name)
	if err != nil {
		return u, o, dbError(err)
	}
	err = tx.QueryRow(ctx, "INSERT INTO organizations(slug,name) VALUES($1,$2) RETURNING id::text,slug,name", Slug(organization), organization).Scan(&o.ID, &o.Slug, &o.Name)
	if err != nil {
		return u, o, dbError(err)
	}
	o.Role = "owner"
	if _, err = tx.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')", o.ID, u.ID); err != nil {
		return u, o, err
	}
	var team string
	if err = tx.QueryRow(ctx, "INSERT INTO teams(organization_id,slug,name) VALUES($1,'default','Default') RETURNING id::text", o.ID).Scan(&team); err != nil {
		return u, o, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO team_memberships(team_id,user_id) VALUES($1,$2)", team, u.ID); err != nil {
		return u, o, err
	}
	return u, o, tx.Commit(ctx)
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, "SELECT id::text,email,name,password_hash FROM users WHERE email=$1", email).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash)
	return u, dbError(err)
}

func (s *Store) Organizations(ctx context.Context, userID string) ([]Organization, error) {
	rows, err := s.pool.Query(ctx, "SELECT o.id::text,o.slug,o.name,m.role FROM organizations o JOIN memberships m ON m.organization_id=o.id WHERE m.user_id::text=$1 ORDER BY o.id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Organization{}
	for rows.Next() {
		var o Organization
		if err = rows.Scan(&o.ID, &o.Slug, &o.Name, &o.Role); err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

func (s *Store) Organization(ctx context.Context, userID, slug string) (Organization, error) {
	var o Organization
	err := s.pool.QueryRow(ctx, "SELECT o.id::text,o.slug,o.name,m.role FROM organizations o JOIN memberships m ON m.organization_id=o.id WHERE m.user_id::text=$1 AND o.slug=$2", userID, slug).Scan(&o.ID, &o.Slug, &o.Name, &o.Role)
	return o, dbError(err)
}

func (s *Store) CreateSession(ctx context.Context, userID, tokenHash, csrf string, expires time.Time) error {
	_, err := s.pool.Exec(ctx, "INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,$4)", tokenHash, userID, csrf, expires)
	return err
}
func (s *Store) Session(ctx context.Context, tokenHash string) (User, string, error) {
	var u User
	var csrf string
	err := s.pool.QueryRow(ctx, "SELECT u.id::text,u.email,u.name,s.csrf_token FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()", tokenHash).Scan(&u.ID, &u.Email, &u.Name, &csrf)
	return u, csrf, dbError(err)
}
func (s *Store) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", tokenHash)
	return err
}

func (s *Store) Projects(ctx context.Context, userID, orgSlug string) ([]Project, error) {
	if _, err := s.Organization(ctx, userID, orgSlug); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT p.id::text,p.slug,p.name,p.platform,p.created_at,COALESCE((SELECT k.public_key FROM project_keys k WHERE k.project_id=p.id AND k.enabled ORDER BY k.created_at LIMIT 1),'') FROM projects p JOIN organizations o ON o.id=p.organization_id WHERE o.slug=$1 ORDER BY p.name`, orgSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Project{}
	for rows.Next() {
		var p Project
		if err = rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Platform, &p.DateCreated, &p.PublicKey); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (s *Store) CreateProject(ctx context.Context, userID, orgSlug, name, platform string) (Project, error) {
	var p Project
	o, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return p, err
	}
	if o.Role == "member" {
		return p, ErrForbidden
	}
	key, err := randomKey()
	if err != nil {
		return p, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, "INSERT INTO projects(organization_id,name,slug,platform) VALUES($1,$2,$3,$4) RETURNING id::text,name,slug,platform,created_at", o.ID, name, Slug(name), platform).Scan(&p.ID, &p.Name, &p.Slug, &p.Platform, &p.DateCreated)
	if err != nil {
		return p, dbError(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO project_keys(public_key,project_id) VALUES($1,$2)", key, p.ID); err != nil {
		return p, err
	}
	p.PublicKey = key
	return p, tx.Commit(ctx)
}

func (s *Store) ValidKey(ctx context.Context, projectID, key string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM project_keys WHERE project_id::text=$1 AND public_key=$2 AND enabled)", projectID, key).Scan(&ok)
	return ok, err
}
