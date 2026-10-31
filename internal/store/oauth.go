package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrOAuthGrant = errors.New("invalid OAuth grant")

type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	CreatedAt    time.Time
}

type OAuthCodeInput struct {
	CodeHash, ClientID, UserID string
	AuthGeneration             int64
	OrganizationID             string
	ProjectIDs                 []int64
	AllProjects                bool
	Scopes                     []string
	RedirectURI, Challenge     string
	ExpiresAt                  time.Time
}

type OAuthExchangeInput struct {
	CodeHash, ClientID, RedirectURI             string
	AccessSecretHash, RefreshSecretHash, Prefix string
	AccessExpiresAt, RefreshExpiresAt           time.Time
}

type OAuthRotationInput struct {
	RefreshSecretHash, ClientID                    string
	AccessSecretHash, NewRefreshSecretHash, Prefix string
	AccessExpiresAt, RefreshExpiresAt              time.Time
}

type OAuthIssued struct {
	AccessTokenID string
	Scopes        []string
}

func (s *Store) CreateOAuthClient(ctx context.Context, client OAuthClient) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_clients(client_id,client_name,redirect_uris,token_endpoint_auth_method)
 VALUES($1,$2,$3,'none')`, client.ID, client.Name, client.RedirectURIs)
	return dbError(err)
}

func (s *Store) OAuthClient(ctx context.Context, id string) (OAuthClient, error) {
	var client OAuthClient
	err := s.pool.QueryRow(ctx, `SELECT client_id,client_name,redirect_uris,created_at FROM oauth_clients WHERE client_id=$1`, id).
		Scan(&client.ID, &client.Name, &client.RedirectURIs, &client.CreatedAt)
	return client, dbError(err)
}

func (s *Store) OAuthCodeChallenge(ctx context.Context, clientID, codeHash string) string {
	var challenge string
	if err := s.pool.QueryRow(ctx, `SELECT code_challenge FROM oauth_authorization_codes WHERE code_hash=$1 AND client_id=$2`, codeHash, clientID).Scan(&challenge); err != nil {
		return ""
	}
	return challenge
}

func oauthGrant(ctx context.Context, tx pgx.Tx, userID, authGeneration, organizationID string, allProjects bool, projectIDs []int64) error {
	if err := pinUserGeneration(ctx, tx, userID, parseInt64(authGeneration)); err != nil {
		return err
	}
	var member bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1::bigint AND organization_id=$2::bigint)`, userID, organizationID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return ErrOAuthGrant
	}
	if allProjects {
		return nil
	}
	if len(projectIDs) == 0 {
		return ErrOAuthGrant
	}
	seen := map[int64]bool{}
	for _, id := range projectIDs {
		if id <= 0 || seen[id] {
			return ErrOAuthGrant
		}
		seen[id] = true
	}
	rows, err := tx.Query(ctx, `SELECT p.id FROM projects p JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
 WHERE p.organization_id=$2::bigint AND p.id=ANY($3::bigint[]) FOR SHARE OF p`, userID, organizationID, projectIDs)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		count++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(seen) {
		return ErrOAuthGrant
	}
	return nil
}

func parseInt64(value string) int64 {
	return referenceID(value)
}

func (s *Store) CreateOAuthCode(ctx context.Context, input OAuthCodeInput) error {
	if input.AllProjects && input.ProjectIDs == nil {
		input.ProjectIDs = []int64{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM oauth_clients WHERE client_id=$1)`, input.ClientID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrOAuthGrant
	}
	if err := oauthGrant(ctx, tx, input.UserID, formatInt(input.AuthGeneration), input.OrganizationID, input.AllProjects, input.ProjectIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_authorization_codes(code_hash,client_id,user_id,organization_id,project_ids,all_projects,scopes,redirect_uri,code_challenge,auth_generation,expires_at)
 VALUES($1,$2,$3::bigint,$4::bigint,$5,$6,$7,$8,$9,$10,$11)`, input.CodeHash, input.ClientID, input.UserID, input.OrganizationID,
		input.ProjectIDs, input.AllProjects, input.Scopes, input.RedirectURI, input.Challenge, input.AuthGeneration, input.ExpiresAt); err != nil {
		return dbError(err)
	}
	return tx.Commit(ctx)
}

func issuedAccessToken(ctx context.Context, tx pgx.Tx, name, userID, authGeneration, organizationID string, allProjects bool, projectIDs []int64, scopes []string, secretHash, prefix string, expires time.Time) (string, error) {
	if err := pinUserGeneration(ctx, tx, userID, parseInt64(authGeneration)); err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO api_tokens(organization_id,user_id,name,secret_hash,prefix,scopes,all_projects,expires_at)
 VALUES($1::bigint,$2::bigint,$3,$4,$5,$6,$7,$8) RETURNING id::text`, organizationID, userID, name, secretHash, prefix, scopes, allProjects, expires).Scan(&id); err != nil {
		return "", dbError(err)
	}
	if !allProjects {
		if _, err := tx.Exec(ctx, `INSERT INTO api_token_projects(token_id,project_id) SELECT $1::bigint,unnest($2::bigint[])`, id, projectIDs); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (s *Store) ExchangeOAuthCode(ctx context.Context, input OAuthExchangeInput) (OAuthIssued, error) {
	var issued OAuthIssued
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return issued, err
	}
	defer tx.Rollback(ctx)
	var userID, orgID, redirect, challenge, clientName, authGeneration string
	var projectIDs []int64
	var allProjects bool
	var scopes []string
	var created, expires time.Time
	var consumed *time.Time
	err = tx.QueryRow(ctx, `SELECT c.user_id::text,c.organization_id::text,c.project_ids,c.all_projects,c.scopes,c.redirect_uri,c.code_challenge,c.auth_generation::text,
 c.created_at,c.expires_at,c.consumed_at,cl.client_name FROM oauth_authorization_codes c JOIN oauth_clients cl ON cl.client_id=c.client_id
 WHERE c.code_hash=$1 AND c.client_id=$2 FOR UPDATE OF c`, input.CodeHash, input.ClientID).Scan(&userID, &orgID, &projectIDs, &allProjects, &scopes, &redirect, &challenge, &authGeneration, &created, &expires, &consumed, &clientName)
	if err != nil || consumed != nil || redirect != input.RedirectURI || !expires.After(created) {
		return issued, ErrOAuthGrant
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return issued, err
	}
	if !expires.After(now) {
		return issued, ErrOAuthGrant
	}
	if err := oauthGrant(ctx, tx, userID, authGeneration, orgID, allProjects, projectIDs); err != nil {
		return issued, ErrOAuthGrant
	}
	name := "OAuth " + clientName
	if len([]rune(name)) > 100 {
		name = string([]rune(name)[:100])
	}
	issued.AccessTokenID, err = issuedAccessToken(ctx, tx, name, userID, authGeneration, orgID, allProjects, projectIDs, scopes, input.AccessSecretHash, input.Prefix, input.AccessExpiresAt)
	if err != nil {
		return OAuthIssued{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_refresh_tokens(secret_hash,client_id,access_token_id,user_id,organization_id,project_ids,all_projects,scopes,auth_generation,expires_at)
 VALUES($1,$2,$3::bigint,$4::bigint,$5,$6,$7,$8,$9,$10)`, input.RefreshSecretHash, input.ClientID, issued.AccessTokenID, userID, orgID, projectIDs, allProjects, scopes, parseInt64(authGeneration), input.RefreshExpiresAt); err != nil {
		return OAuthIssued{}, err
	}
	if result, err := tx.Exec(ctx, `UPDATE oauth_authorization_codes SET consumed_at=$2 WHERE code_hash=$1 AND consumed_at IS NULL`, input.CodeHash, now); err != nil || result.RowsAffected() != 1 {
		if err != nil {
			return OAuthIssued{}, err
		}
		return OAuthIssued{}, ErrOAuthGrant
	}
	issued.Scopes = scopes
	return issued, tx.Commit(ctx)
}

func (s *Store) RotateOAuthRefresh(ctx context.Context, input OAuthRotationInput) (OAuthIssued, error) {
	var issued OAuthIssued
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return issued, err
	}
	defer tx.Rollback(ctx)
	var observedAccessID, observedUserID, observedGeneration string
	if err = tx.QueryRow(ctx, `SELECT access_token_id::text,user_id::text,auth_generation::text FROM oauth_refresh_tokens WHERE secret_hash=$1 AND client_id=$2`, input.RefreshSecretHash, input.ClientID).
		Scan(&observedAccessID, &observedUserID, &observedGeneration); err != nil {
		return issued, ErrOAuthGrant
	}
	if err = pinUserGeneration(ctx, tx, observedUserID, parseInt64(observedGeneration)); err != nil {
		return issued, ErrOAuthGrant
	}
	var accessRevoked *time.Time
	if err = tx.QueryRow(ctx, `SELECT revoked_at FROM api_tokens WHERE id=$1::bigint AND user_id=$2::bigint FOR UPDATE`, observedAccessID, observedUserID).Scan(&accessRevoked); err != nil || accessRevoked != nil {
		return issued, ErrOAuthGrant
	}
	var accessID, userID, orgID, authGeneration, clientName string
	var projectIDs []int64
	var allProjects bool
	var scopes []string
	var expires time.Time
	var revoked *time.Time
	err = tx.QueryRow(ctx, `SELECT r.access_token_id::text,r.user_id::text,r.organization_id::text,r.auth_generation::text,r.project_ids,r.all_projects,r.scopes,
 r.expires_at,r.revoked_at,cl.client_name FROM oauth_refresh_tokens r JOIN oauth_clients cl ON cl.client_id=r.client_id
 WHERE r.secret_hash=$1 AND r.client_id=$2 FOR UPDATE OF r`, input.RefreshSecretHash, input.ClientID).
		Scan(&accessID, &userID, &orgID, &authGeneration, &projectIDs, &allProjects, &scopes, &expires, &revoked, &clientName)
	if err != nil || revoked != nil {
		return issued, ErrOAuthGrant
	}
	if accessID != observedAccessID || userID != observedUserID || authGeneration != observedGeneration {
		return issued, ErrOAuthGrant
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return issued, err
	}
	if !expires.After(now) {
		return issued, ErrOAuthGrant
	}
	if err := oauthGrant(ctx, tx, userID, authGeneration, orgID, allProjects, projectIDs); err != nil {
		return issued, ErrOAuthGrant
	}
	if _, err := tx.Exec(ctx, `UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,$2) WHERE id=$1::bigint`, accessID, now); err != nil {
		return issued, err
	}
	name := "OAuth " + clientName
	if len([]rune(name)) > 100 {
		name = string([]rune(name)[:100])
	}
	issued.AccessTokenID, err = issuedAccessToken(ctx, tx, name, userID, authGeneration, orgID, allProjects, projectIDs, scopes, input.AccessSecretHash, input.Prefix, input.AccessExpiresAt)
	if err != nil {
		return OAuthIssued{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_refresh_tokens(secret_hash,client_id,access_token_id,user_id,organization_id,project_ids,all_projects,scopes,auth_generation,expires_at)
 VALUES($1,$2,$3::bigint,$4::bigint,$5,$6,$7,$8,$9,$10)`, input.NewRefreshSecretHash, input.ClientID, issued.AccessTokenID, userID, orgID, projectIDs, allProjects, scopes, parseInt64(authGeneration), input.RefreshExpiresAt); err != nil {
		return OAuthIssued{}, err
	}
	result, err := tx.Exec(ctx, `UPDATE oauth_refresh_tokens SET revoked_at=$3,rotated_to=$4 WHERE secret_hash=$1 AND client_id=$2 AND revoked_at IS NULL`, input.RefreshSecretHash, input.ClientID, now, input.NewRefreshSecretHash)
	if err != nil || result.RowsAffected() != 1 {
		if err != nil {
			return OAuthIssued{}, err
		}
		return OAuthIssued{}, ErrOAuthGrant
	}
	issued.Scopes = scopes
	return issued, tx.Commit(ctx)
}

func (s *Store) RevokeOAuthArtifact(ctx context.Context, clientID, secretHash string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID, accessID string
	err = tx.QueryRow(ctx, `SELECT r.user_id::text,r.access_token_id::text FROM oauth_refresh_tokens r
		JOIN api_tokens t ON t.id=r.access_token_id
		WHERE r.client_id=$2 AND (r.secret_hash=$1 OR t.secret_hash=$1) LIMIT 1`, secretHash, clientID).Scan(&userID, &accessID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	var lockedUser string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id=$1::bigint FOR UPDATE`, userID).Scan(&lockedUser); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tx.Commit(ctx)
		}
		return err
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE api_tokens SET revoked_at=COALESCE(revoked_at,$2) WHERE id=$1::bigint`, accessID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_refresh_tokens SET revoked_at=COALESCE(revoked_at,$3)
		WHERE access_token_id=$1::bigint AND client_id=$2`, accessID, clientID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CleanupOAuthSecrets(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	codes, err := tx.Exec(ctx, `DELETE FROM oauth_authorization_codes
 WHERE expires_at<now()-interval '30 days' OR consumed_at<now()-interval '30 days'`)
	if err != nil {
		return 0, err
	}
	refresh, err := tx.Exec(ctx, `DELETE FROM oauth_refresh_tokens
 WHERE expires_at<now()-interval '30 days' OR revoked_at<now()-interval '30 days'`)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return codes.RowsAffected() + refresh.RowsAffected(), nil
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
