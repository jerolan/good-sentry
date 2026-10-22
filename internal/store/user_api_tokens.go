package store

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// UserAPIToken is one account credential presented in the compatible token
// shape. The secret is stored sealed and only opened for the owner's list and
// the creation response.
type UserAPIToken struct {
	ID         int64
	Label      string
	ScopesMask int64
	Sealed     []byte
	CreatedAt  time.Time
}

// UserAPITokenCursor bounds one list page. Reverse pages walk newer rows and
// are returned in the same newest-first order as the forward pages.
type UserAPITokenCursor struct {
	Boundary time.Time
	Reverse  bool
}

const userAPITokenColumns = "id,label,scopes_mask,secret,created_at"

func scanUserAPIToken(row interface{ Scan(...any) error }) (UserAPIToken, error) {
	var t UserAPIToken
	if err := row.Scan(&t.ID, &t.Label, &t.ScopesMask, &t.Sealed, &t.CreatedAt); err != nil {
		return UserAPIToken{}, dbError(err)
	}
	return t, nil
}

// CreateUserAPIToken stores one account credential for its owner.
func (s *Store) CreateUserAPIToken(ctx context.Context, userID, label string, mask int64, secretHash string, sealed []byte) (UserAPIToken, error) {
	return scanUserAPIToken(s.pool.QueryRow(ctx, `INSERT INTO user_api_tokens(user_id,label,scopes_mask,secret_hash,secret)
		VALUES($1::bigint,$2,$3,$4,$5) RETURNING `+userAPITokenColumns,
		userID, label, mask, secretHash, sealed))
}

// UserAPITokens lists one owner's credentials newest first. A limit of zero
// returns every entry; more reports whether entries exist beyond the page in
// the cursor's direction.
func (s *Store) UserAPITokens(ctx context.Context, userID string, limit int, cursor *UserAPITokenCursor) ([]UserAPIToken, bool, error) {
	query := "SELECT " + userAPITokenColumns + " FROM user_api_tokens WHERE user_id=$1::bigint"
	args := []any{userID}
	order := "DESC"
	if cursor != nil {
		operator := "<"
		if cursor.Reverse {
			operator, order = ">", "ASC"
		}
		query += " AND created_at " + operator + " $2"
		args = append(args, cursor.Boundary)
	}
	query += " ORDER BY created_at " + order + ",id " + order
	if limit > 0 {
		query += " LIMIT " + strconv.Itoa(limit+1)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	tokens := []UserAPIToken{}
	for rows.Next() {
		t, err := scanUserAPIToken(rows)
		if err != nil {
			return nil, false, err
		}
		tokens = append(tokens, t)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := false
	if limit > 0 && len(tokens) > limit {
		more = true
		tokens = tokens[:limit]
	}
	if cursor != nil && cursor.Reverse {
		slices.Reverse(tokens)
	}
	return tokens, more, nil
}

// DeleteUserAPIToken removes one credential from its owner. Another owner's
// identity answers ErrNotFound.
func (s *Store) DeleteUserAPIToken(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM user_api_tokens WHERE user_id=$1::bigint AND id=$2::bigint", userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AuthenticateUserAPIToken resolves a presented secret. The grant carries no
// organization so compatible routes resolve one through the owner's membership.
func (s *Store) AuthenticateUserAPIToken(ctx context.Context, secretHash string) (APITokenGrant, error) {
	var grant APITokenGrant
	err := s.pool.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.auth_generation,t.scopes_mask
		FROM user_api_tokens t JOIN users u ON u.id=t.user_id
		WHERE t.secret_hash=$1`, secretHash).
		Scan(&grant.User.ID, &grant.User.Email, &grant.User.Name, &grant.User.AuthGeneration, &grant.ScopesMask)
	if err != nil {
		return grant, dbError(err)
	}
	grant.AllProjects = true
	grant.Scopes = UserTokenScopeNames(grant.ScopesMask)
	return grant, nil
}

var userTokenScopes = []string{
	"project:read", "project:write", "project:admin", "project:releases",
	"team:read", "team:write", "team:admin",
	"event:read", "event:write", "event:admin",
	"org:read", "org:write", "org:admin",
	"member:read", "member:write", "member:admin",
}

// UserTokenScopeNames renders a mask as scope names in ascending bit order.
// Bits outside the published sixteen grant nothing.
func UserTokenScopeNames(mask int64) []string {
	names := []string{}
	for bit, name := range userTokenScopes {
		if mask&(1<<bit) != 0 {
			names = append(names, name)
		}
	}
	return names
}

// UserTokenMask combines published scope names into one mask.
func UserTokenMask(names []string) int64 {
	var mask int64
	for bit, name := range userTokenScopes {
		for _, candidate := range names {
			if candidate == name {
				mask |= 1 << bit
				break
			}
		}
	}
	return mask
}

// UserTokenMaskChecked combines published scope names and reports whether all
// of them are known. Unknown names must be rejected instead of silently
// dropping authority.
func UserTokenMaskChecked(names []string) (int64, bool) {
	var mask int64
	for _, candidate := range names {
		known := false
		for bit, name := range userTokenScopes {
			if candidate == name {
				mask |= 1 << bit
				known = true
				break
			}
		}
		if !known {
			return 0, false
		}
	}
	return mask, true
}

var (
	// ErrNoRecoveryCodes reports that no staged set exists to consume.
	ErrNoRecoveryCodes = errors.New("no recovery codes set")
	// ErrInvalidRecoveryCode reports a submitted code outside the staged set.
	ErrInvalidRecoveryCode = errors.New("invalid recovery code")
)

// StageCompatRecoveryCodes stages a fresh single-use set for one account,
// independent of the native two-factor backup codes.
func (s *Store) StageCompatRecoveryCodes(ctx context.Context, userID string, hashes []string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO compat_recovery_codes(user_id,code_hashes) VALUES($1::bigint,$2::text[])
		ON CONFLICT (user_id) DO UPDATE SET code_hashes=EXCLUDED.code_hashes,created_at=now()`, userID, hashes)
	return err
}

// ConsumeRecoveryCode removes the staged set after a matching code. A wrong
// code leaves the set in place; an absent set is reported separately.
func (s *Store) ConsumeCompatRecoveryCode(ctx context.Context, userID, codeHash string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var hashes []string
	if err = tx.QueryRow(ctx, "SELECT code_hashes FROM compat_recovery_codes WHERE user_id=$1::bigint FOR UPDATE", userID).Scan(&hashes); err != nil {
		if errors.Is(dbError(err), ErrNotFound) {
			return ErrNoRecoveryCodes
		}
		return err
	}
	matched := false
	for _, hash := range hashes {
		if hash == codeHash {
			matched = true
			break
		}
	}
	if !matched {
		return ErrInvalidRecoveryCode
	}
	if _, err = tx.Exec(ctx, "DELETE FROM compat_recovery_codes WHERE user_id=$1::bigint", userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
