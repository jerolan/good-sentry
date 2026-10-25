package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrIssueIdentitySecretUnavailable = errors.New("issue identity secret unavailable")

func (s *Store) issueUserDigest(organizationID, identity string) ([]byte, error) {
	s.issueIdentitySecretMu.RLock()
	secret := append([]byte(nil), s.issueIdentitySecret...)
	s.issueIdentitySecretMu.RUnlock()
	if len(secret) < 32 {
		return nil, ErrIssueIdentitySecretUnavailable
	}

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("goodsentry/issue-user-mute/v1\x00"))
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(organizationID)))
	_, _ = mac.Write(size[:])
	_, _ = mac.Write([]byte(organizationID))
	binary.BigEndian.PutUint64(size[:], uint64(len(identity)))
	_, _ = mac.Write(size[:])
	_, _ = mac.Write([]byte(identity))
	return mac.Sum(nil), nil
}

func issueUserIdentityFromData(ctx context.Context, tx pgx.Tx, data []byte) (string, error) {
	var identity string
	query := "SELECT COALESCE(" + issueUserIdentity + ",'') FROM (SELECT $1::jsonb AS data) e"
	if err := tx.QueryRow(ctx, query, string(data)).Scan(&identity); err != nil {
		return "", err
	}
	return identity, nil
}

func (s *Store) issueOrganization(ctx context.Context, tx pgx.Tx, issueID string) (string, error) {
	var organizationID string
	err := tx.QueryRow(ctx, `SELECT p.organization_id::text FROM issues i JOIN projects p ON p.id=i.project_id WHERE i.id=$1::bigint`, issueID).Scan(&organizationID)
	return organizationID, err
}

func (s *Store) seedIssueIgnoreUserBaseline(ctx context.Context, tx pgx.Tx, issueID string, sourceIDs []int64, organizationID string) error {
	issueIDs := sourceIDs
	if issueIDs == nil {
		id, err := strconv.ParseInt(issueID, 10, 64)
		if err != nil || id < 1 || strconv.FormatInt(id, 10) != issueID {
			return ErrInvalidCompatibleIssueChange
		}
		issueIDs = []int64{id}
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT identity FROM (
 SELECT `+issueUserIdentity+` AS identity FROM events e WHERE e.issue_id=ANY($1::bigint[])
) identities WHERE identity IS NOT NULL ORDER BY identity`, issueIDs)
	if err != nil {
		return err
	}
	var identities []string
	for rows.Next() {
		var identity string
		if err = rows.Scan(&identity); err != nil {
			rows.Close()
			return err
		}
		identities = append(identities, identity)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, identity := range identities {
		digest, digestErr := s.issueUserDigest(organizationID, identity)
		if digestErr != nil {
			return digestErr
		}
		if _, err = tx.Exec(ctx, `INSERT INTO issue_ignore_users(issue_id,identity_digest,baseline)
 VALUES($1::bigint,$2,true) ON CONFLICT(issue_id,identity_digest) DO NOTHING`, issueID, digest); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) recordIssueIgnoreUser(ctx context.Context, tx pgx.Tx, projectID, issueID string, data []byte, receiptAt *time.Time, refresh bool) error {
	identity, err := issueUserIdentityFromData(ctx, tx, data)
	if err != nil || identity == "" {
		return err
	}
	var organizationID string
	err = tx.QueryRow(ctx, `SELECT organization_id::text FROM projects WHERE id=$1::bigint`, projectID).Scan(&organizationID)
	if err != nil {
		return err
	}
	digest, err := s.issueUserDigest(organizationID, identity)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO issue_ignore_users(issue_id,identity_digest,baseline,received_at)
	 VALUES($1::bigint,$2,false,COALESCE($3::timestamptz,clock_timestamp()))
	 ON CONFLICT(issue_id,identity_digest) DO UPDATE SET received_at=EXCLUDED.received_at
	 WHERE $4::boolean AND NOT issue_ignore_users.baseline`, issueID, digest, receiptAt, refresh)
	return err
}

func (s *Store) seedMergedIssueIgnoreUsers(ctx context.Context, tx pgx.Tx, parent string, sourceIDs []int64) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM issue_ignores WHERE issue_id=$1::bigint AND user_count IS NOT NULL)`, parent).Scan(&enabled); err != nil || !enabled {
		return err
	}
	organizationID, err := s.issueOrganization(ctx, tx, parent)
	if err != nil {
		return err
	}
	if err = s.seedIssueIgnoreUserBaseline(ctx, tx, parent, sourceIDs, organizationID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT issue_id::text,identity_digest FROM issue_ignore_users WHERE issue_id=ANY($1::bigint[])`, sourceIDs)
	if err != nil {
		return err
	}
	type digestRow struct {
		issueID string
		digest  []byte
	}
	var digests []digestRow
	for rows.Next() {
		var row digestRow
		if err = rows.Scan(&row.issueID, &row.digest); err != nil {
			rows.Close()
			return err
		}
		digests = append(digests, row)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, row := range digests {
		if _, err = tx.Exec(ctx, `INSERT INTO issue_ignore_users(issue_id,identity_digest,baseline)
 VALUES($1::bigint,$2,true) ON CONFLICT(issue_id,identity_digest) DO NOTHING`, parent, row.digest); err != nil {
			return err
		}
	}
	return nil
}
