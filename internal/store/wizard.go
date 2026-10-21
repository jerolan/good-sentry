package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type WizardTransferRecord struct {
	UserID          string
	OrganizationID  string
	TokenCiphertext []byte
}

type WizardProject struct {
	ID               string
	Slug             string
	OrganizationID   string
	OrganizationSlug string
	OrganizationName string
	PublicKey        string
}

func validWizardTransferInput(hash, secretHash, prefix string, ciphertext []byte, validFor time.Duration) bool {
	return len(hash) == 64 && len(secretHash) == 64 && len(prefix) == 11 && prefix[:3] == "gs_" &&
		len(ciphertext) >= 40 && len(ciphertext) <= 256 && validFor > 0 && validFor <= 24*time.Hour
}

// CreateWizardTransfer atomically issues a one-day organization credential and
// exposes it only through the short-lived wizard transfer row.
func (s *Store) CreateWizardTransfer(ctx context.Context, userID string, authGeneration int64, organizationRef, transferHash, secretHash, prefix string, ciphertext []byte, validFor time.Duration) error {
	if !validWizardTransferInput(transferHash, secretHash, prefix, ciphertext, validFor) {
		return ErrNotFound
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = pinUserGeneration(ctx, tx, userID, authGeneration); err != nil {
		return err
	}
	var organizationID, role string
	err = tx.QueryRow(ctx, `SELECT o.id::text,m.role FROM organizations o JOIN memberships m ON m.organization_id=o.id
		WHERE m.user_id=$1::bigint AND (o.slug=$2 OR o.id=$3::bigint) ORDER BY (o.slug=$2) DESC LIMIT 1 FOR SHARE OF m`,
		userID, organizationRef, referenceID(organizationRef)).Scan(&organizationID, &role)
	if err != nil {
		return dbError(err)
	}
	if role != "owner" && role != "admin" {
		return ErrForbidden
	}
	var apiTokenID string
	if err = tx.QueryRow(ctx, `INSERT INTO api_tokens(organization_id,user_id,name,secret_hash,prefix,scopes,all_projects,expires_at)
		VALUES($1::bigint,$2::bigint,'Sentry Wizard',$3,$4,ARRAY['org:read','project:read','project:releases'],true,now()+interval '1 day')
		RETURNING id::text`, organizationID, userID, secretHash, prefix).Scan(&apiTokenID); err != nil {
		return dbError(err)
	}
	command, err := tx.Exec(ctx, `INSERT INTO wizard_transfers(token_hash,user_id,organization_id,api_token_id,token_ciphertext,expires_at)
		VALUES($1,$2::bigint,$3::bigint,$4::bigint,$5,clock_timestamp()+$6::bigint*interval '1 microsecond') ON CONFLICT(token_hash) DO NOTHING`,
		transferHash, userID, organizationID, apiTokenID, ciphertext, validFor.Microseconds())
	if err != nil {
		return dbError(err)
	}
	if command.RowsAffected() != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

func (s *Store) WizardTransfer(ctx context.Context, transferHash string) (WizardTransferRecord, error) {
	var record WizardTransferRecord
	if len(transferHash) != 64 {
		return record, ErrNotFound
	}
	err := s.pool.QueryRow(ctx, `SELECT w.user_id::text,w.organization_id::text,w.token_ciphertext
		FROM wizard_transfers w
		JOIN memberships m ON m.organization_id=w.organization_id AND m.user_id=w.user_id
		JOIN api_tokens t ON t.id=w.api_token_id AND t.revoked_at IS NULL AND t.expires_at>clock_timestamp()
		WHERE w.token_hash=$1 AND w.expires_at>clock_timestamp()`, transferHash).
		Scan(&record.UserID, &record.OrganizationID, &record.TokenCiphertext)
	return record, dbError(err)
}

func (s *Store) DeleteWizardTransfer(ctx context.Context, transferHash string) error {
	if len(transferHash) != 64 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM wizard_transfers WHERE token_hash=$1`, transferHash)
	return err
}

func (s *Store) WizardProjects(ctx context.Context, userID, organizationID string) ([]WizardProject, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id::text,p.slug,o.id::text,o.slug,o.name,k.public_key
		FROM projects p
		JOIN organizations o ON o.id=p.organization_id
		JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
		JOIN project_keys k ON k.project_id=p.id AND k.enabled
		WHERE p.organization_id=$2::bigint
		ORDER BY p.id,k.created_at,k.public_key`, userID, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []WizardProject{}
	for rows.Next() {
		var project WizardProject
		if err = rows.Scan(&project.ID, &project.Slug, &project.OrganizationID, &project.OrganizationSlug, &project.OrganizationName, &project.PublicKey); err != nil {
			return nil, err
		}
		result = append(result, project)
	}
	return result, rows.Err()
}
