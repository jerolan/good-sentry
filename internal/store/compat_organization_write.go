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

func validOrganizationName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 200 && !strings.ContainsFunc(name, unicode.IsControl)
}

func (s *Store) UpdateCompatibleOrganization(ctx context.Context, userID, orgRef, name string) (CompatibleOrganizationDetail, error) {
	name = strings.TrimSpace(name)
	if !validOrganizationName(name) {
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
	if _, err = tx.Exec(ctx, `UPDATE organizations SET name=$2 WHERE id=$1::bigint`, orgID, name); err != nil {
		return CompatibleOrganizationDetail{}, err
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
