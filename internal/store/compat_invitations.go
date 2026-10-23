package store

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// CompatibleInvitation is a pending organization invitation resolved together
// with the organization metadata used by the compatible acceptance route.
type CompatibleInvitation struct {
	Invitation
	Organization CompatibleOrganization
	Teams        []string
}

func validCompatibleInvitationID(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// CompatibleInvitation validates a current sent, unexpired invitation by its
// public identity and token hash. A mismatched pair is indistinguishable from
// an expired or canceled invitation.
func (s *Store) CompatibleInvitation(ctx context.Context, invitationID, tokenHash string) (CompatibleInvitation, error) {
	var result CompatibleInvitation
	if !validCompatibleInvitationID(invitationID) || len(tokenHash) != 64 {
		return result, ErrInvalidInvitation
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	result.Invitation, err = invitationFrom(tx, ctx,
		"i.id=$1 AND i.token_hash=$2 AND i.status='sent' AND i.expires_at>clock_timestamp()", invitationID, tokenHash)
	if errors.Is(err, ErrNotFound) {
		return result, ErrInvalidInvitation
	}
	if err != nil {
		return result, err
	}
	result.Teams = []string{}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(array_agg(t.slug ORDER BY lower(t.slug),t.id)
		FILTER (WHERE t.id IS NOT NULL),ARRAY[]::text[]) FROM organization_invitation_teams iit
		JOIN teams t ON t.id=iit.team_id AND t.organization_id=$2::bigint
		WHERE iit.invitation_id=$1`, invitationID, result.OrganizationID).Scan(&result.Teams); err != nil {
		return result, err
	}
	var slug, name string
	var created time.Time
	if err = tx.QueryRow(ctx, "SELECT slug,name,created_at FROM organizations WHERE id=$1::bigint", result.OrganizationID).
		Scan(&slug, &name, &created); err != nil {
		return result, dbError(err)
	}
	result.Organization = discoveryOrganization(result.OrganizationID, slug, name, created)
	return result, tx.Commit(ctx)
}
