package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

type CompatibleAvatar struct {
	Type string  `json:"avatarType"`
	UUID *string `json:"avatarUuid"`
}

type CompatibleStatus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CompatibleOrganization struct {
	ID                string           `json:"id"`
	Slug              string           `json:"slug"`
	Name              string           `json:"name"`
	Role              string           `json:"-"`
	DateCreated       time.Time        `json:"dateCreated"`
	Status            CompatibleStatus `json:"status"`
	Avatar            CompatibleAvatar `json:"avatar"`
	IsEarlyAdopter    bool             `json:"isEarlyAdopter"`
	Require2FA        bool             `json:"require2FA"`
	IsAcceptingEvents bool             `json:"isAcceptingEvents"`
	EventThrottleRate int              `json:"eventThrottleRate"`
}

func discoveryOrganization(id, slug, name string, created time.Time) CompatibleOrganization {
	return CompatibleOrganization{
		ID: id, Slug: slug, Name: name, DateCreated: created.UTC(),
		Status: CompatibleStatus{ID: "active", Name: "active"},
		Avatar: CompatibleAvatar{}, IsAcceptingEvents: true,
	}
}

type CompatibleProject struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Slug              string                 `json:"slug"`
	Platform          string                 `json:"platform"`
	DateCreated       time.Time              `json:"dateCreated"`
	Organization      CompatibleOrganization `json:"organization"`
	Avatar            CompatibleAvatar       `json:"avatar"`
	Color             string                 `json:"color"`
	Features          []string               `json:"features"`
	HasAccess         bool                   `json:"hasAccess"`
	IsBookmarked      bool                   `json:"isBookmarked"`
	IsInternal        bool                   `json:"isInternal"`
	IsMember          bool                   `json:"isMember"`
	IsPublic          bool                   `json:"isPublic"`
	ScrubIPAddresses  bool                   `json:"scrubIPAddresses"`
	FirstEvent        *time.Time             `json:"firstEvent"`
	EventThrottleRate int                    `json:"eventThrottleRate"`
}

type CompatibleUserOptions struct {
	Timezone        *string `json:"timezone"`
	StacktraceOrder *int    `json:"stacktraceOrder"`
	Language        *string `json:"language"`
	Clock24Hours    *bool   `json:"clock24Hours"`
	PreferredTheme  *string `json:"preferredTheme"`
}

type CompatibleUser struct {
	ID                 string                `json:"id"`
	Username           string                `json:"username"`
	Email              string                `json:"email"`
	Name               string                `json:"name"`
	DateJoined         time.Time             `json:"dateJoined"`
	Options            CompatibleUserOptions `json:"options"`
	HasPasswordAuth    bool                  `json:"hasPasswordAuth"`
	Identities         []any                 `json:"identities"`
	LastLogin          *time.Time            `json:"lastLogin"`
	IsSuperuser        bool                  `json:"isSuperuser"`
	IsActive           bool                  `json:"isActive"`
	SubscribeByDefault bool                  `json:"-"`
}

// CompatibleOrganizations returns only current memberships. The handler owns
// pagination trimming and Link construction; the provider returns one probe row.
func (s *Store) CompatibleOrganizations(ctx context.Context, userID, organizationID, query string, ownerOnly bool, sortBy string, offset, limit int) ([]CompatibleOrganization, error) {
	order := "lower(o.name),o.id"
	switch sortBy {
	case "", "name":
	case "-name":
		order = "lower(o.name) DESC,o.id DESC"
	case "slug":
		order = "lower(o.slug),o.id"
	case "-slug":
		order = "lower(o.slug) DESC,o.id DESC"
	case "dateCreated":
		order = "o.created_at,o.id"
	case "-dateCreated":
		order = "o.created_at DESC,o.id DESC"
	default:
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT o.id::text,o.slug,o.name,o.created_at,o.require_2fa,m.role FROM organizations o
		JOIN memberships m ON m.organization_id=o.id AND m.user_id=$1::bigint
		WHERE ($2='' OR o.id::text=$2)
		AND ($3='' OR strpos(lower(o.name),lower($3))>0 OR strpos(lower(o.slug),lower($3))>0)
		AND (NOT $4 OR m.role='owner') ORDER BY `+order+` LIMIT $5 OFFSET $6`, userID, organizationID, query, ownerOnly, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CompatibleOrganization{}
	for rows.Next() {
		var id, slug, name string
		var created time.Time
		var role string
		var require2FA bool
		if err := rows.Scan(&id, &slug, &name, &created, &require2FA, &role); err != nil {
			return nil, err
		}
		organization := discoveryOrganization(id, slug, name, created)
		organization.Role = role
		organization.Require2FA = require2FA
		result = append(result, organization)
	}
	return result, rows.Err()
}

func (s *Store) CompatibleProjectsAcrossOrganizations(ctx context.Context, userID, organizationID string, allProjects bool, projectIDs []int64, offset, limit int) ([]CompatibleProject, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id::text,p.name,p.slug,p.platform,p.created_at,p.event_throttle_rate,
		o.id::text,o.slug,o.name,o.created_at,o.require_2fa
		FROM projects p JOIN organizations o ON o.id=p.organization_id
		JOIN memberships m ON m.organization_id=o.id AND m.user_id=$1::bigint
		WHERE ($2='' OR o.id::text=$2) AND ($3 OR p.id=ANY($4::bigint[]))
		ORDER BY p.id LIMIT $5 OFFSET $6`, userID, organizationID, allProjects, projectIDs, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CompatibleProject{}
	for rows.Next() {
		var projectID, name, slug, platform string
		var created time.Time
		var orgID, orgSlug, orgName string
		var orgCreated time.Time
		var eventThrottleRate int
		var require2FA bool
		if err := rows.Scan(&projectID, &name, &slug, &platform, &created, &eventThrottleRate, &orgID, &orgSlug, &orgName, &orgCreated, &require2FA); err != nil {
			return nil, err
		}
		organization := discoveryOrganization(orgID, orgSlug, orgName, orgCreated)
		organization.Require2FA = require2FA
		result = append(result, CompatibleProject{
			ID: projectID, Name: name, Slug: slug, Platform: platform, DateCreated: created.UTC(), EventThrottleRate: eventThrottleRate,
			Organization: organization,
			Features:     []string{}, HasAccess: true, IsMember: true,
		})
	}
	return result, rows.Err()
}

func (s *Store) CompatibleUser(ctx context.Context, userID string) (CompatibleUser, error) {
	if referenceID(userID) == 0 {
		return CompatibleUser{}, ErrNotFound
	}
	var user CompatibleUser
	var options []byte
	err := s.pool.QueryRow(ctx, `SELECT id::text,email,name,created_at,options,notification_subscribe_by_default
FROM users WHERE id=$1::bigint`, userID).
		Scan(&user.ID, &user.Email, &user.Name, &user.DateJoined, &options, &user.SubscribeByDefault)
	if err != nil {
		return CompatibleUser{}, dbError(err)
	}
	user.Username = user.Email
	user.DateJoined = user.DateJoined.UTC()
	user.HasPasswordAuth = true
	user.Identities = []any{}
	if json.Unmarshal(options, &user.Options) != nil {
		user.Options = CompatibleUserOptions{}
	}
	user.IsActive = true
	return user, nil
}

func (s *Store) UpdateCompatibleUser(ctx context.Context, userID, name string, options CompatibleUserOptions) (CompatibleUser, error) {
	if referenceID(userID) == 0 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 255 {
		return CompatibleUser{}, ErrNotFound
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return CompatibleUser{}, err
	}
	result, err := s.pool.Exec(ctx, `UPDATE users SET name=$2,options=$3::jsonb WHERE id::text=$1`, userID, name, encoded)
	if err != nil {
		return CompatibleUser{}, err
	}
	if result.RowsAffected() == 0 {
		return CompatibleUser{}, ErrNotFound
	}
	return s.CompatibleUser(ctx, userID)
}

func (s *Store) UpdateUserNotifications(ctx context.Context, userID string, subscribe bool) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE users SET notification_subscribe_by_default=$2 WHERE id::text=$1`, userID, subscribe)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 0 {
		return false, ErrNotFound
	}
	return subscribe, nil
}
