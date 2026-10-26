package store

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidOrganization = errors.New("invalid organization")

var ErrTwoFactorRequired = errors.New("two-factor authentication must be enabled before requiring it")

func validOrganizationName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 200 && !strings.ContainsFunc(name, unicode.IsControl)
}

func (s *Store) CreateCompatibleOrganization(ctx context.Context, userID, authorizationOrg, name string) (CompatibleOrganizationDetail, error) {
	name = strings.TrimSpace(name)
	if !validOrganizationName(name) {
		return CompatibleOrganizationDetail{}, ErrInvalidOrganization
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CompatibleOrganizationDetail{}, err
	}
	defer tx.Rollback(ctx)
	if authorizationOrg != "" {
		var role string
		if err = tx.QueryRow(ctx, `SELECT m.role FROM memberships m JOIN organizations o ON o.id=m.organization_id
			WHERE m.user_id=$1::bigint AND (o.id=$2::bigint OR o.slug=$3) ORDER BY (o.slug=$3) DESC LIMIT 1 FOR SHARE OF m`,
			userID, referenceID(authorizationOrg), authorizationOrg).Scan(&role); err != nil {
			return CompatibleOrganizationDetail{}, dbError(err)
		}
		if role != "owner" && role != "admin" {
			return CompatibleOrganizationDetail{}, ErrForbidden
		}
	} else {
		var authorized bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1::bigint AND role IN ('owner','admin'))`, userID).Scan(&authorized); err != nil {
			return CompatibleOrganizationDetail{}, err
		}
		if !authorized {
			return CompatibleOrganizationDetail{}, ErrForbidden
		}
	}
	var organizationID string
	if err = tx.QueryRow(ctx, `INSERT INTO organizations(slug,name) VALUES($1,$2) RETURNING id::text`, Slug(name), name).Scan(&organizationID); err != nil {
		return CompatibleOrganizationDetail{}, dbError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,'owner')`, organizationID, userID); err != nil {
		return CompatibleOrganizationDetail{}, err
	}
	var teamID string
	if err = tx.QueryRow(ctx, `INSERT INTO teams(organization_id,slug,name) VALUES($1::bigint,'default','Default') RETURNING id::text`, organizationID).Scan(&teamID); err != nil {
		return CompatibleOrganizationDetail{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO team_memberships(team_id,user_id) VALUES($1::bigint,$2::bigint)`, teamID, userID); err != nil {
		return CompatibleOrganizationDetail{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CompatibleOrganizationDetail{}, err
	}
	return s.CompatibleOrganizationDetail(ctx, userID, organizationID)
}

func (s *Store) UpdateCompatibleOrganization(ctx context.Context, userID, orgRef, name string, require2FA *bool) (CompatibleOrganizationDetail, error) {
	name = strings.TrimSpace(name)
	if (name == "" && require2FA == nil) || (name != "" && !validOrganizationName(name)) {
		return CompatibleOrganizationDetail{}, ErrInvalidOrganization
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CompatibleOrganizationDetail{}, err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, userID, orgRef)
	if err != nil {
		return CompatibleOrganizationDetail{}, err
	}
	if require2FA != nil && *require2FA {
		var confirmed bool
		if err = tx.QueryRow(ctx, `SELECT totp_confirmed_at IS NOT NULL FROM users WHERE id=$1::bigint FOR SHARE`, userID).Scan(&confirmed); err != nil {
			return CompatibleOrganizationDetail{}, dbError(err)
		}
		if !confirmed {
			return CompatibleOrganizationDetail{}, ErrTwoFactorRequired
		}
	}
	if name != "" {
		if _, err = tx.Exec(ctx, `UPDATE organizations SET name=$2 WHERE id=$1::bigint`, orgID, name); err != nil {
			return CompatibleOrganizationDetail{}, err
		}
	}
	if require2FA != nil {
		if _, err = tx.Exec(ctx, `UPDATE organizations SET require_2fa=$2 WHERE id=$1::bigint`, orgID, *require2FA); err != nil {
			return CompatibleOrganizationDetail{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return CompatibleOrganizationDetail{}, err
	}
	return s.CompatibleOrganizationDetail(ctx, userID, orgID)
}

func (s *Store) DeleteCompatibleOrganization(ctx context.Context, userID, orgRef string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orgID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM organizations WHERE slug=$1 OR id=$2 ORDER BY (slug=$1) DESC LIMIT 1 FOR UPDATE`, orgRef, referenceID(orgRef)).Scan(&orgID); err != nil {
		return dbError(err)
	}
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM memberships WHERE organization_id=$1::bigint AND user_id=$2::bigint FOR SHARE`, orgID, userID).Scan(&role); err != nil {
		return dbError(err)
	}
	if role != "owner" {
		return ErrForbidden
	}
	if _, err = tx.Exec(ctx, `DELETE FROM organizations WHERE id=$1::bigint`, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
