package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"goodsentry/internal/notify"

	"github.com/jackc/pgx/v5"
)

type Invitation struct {
	ID               string     `json:"id"`
	Email            string     `json:"email"`
	Role             string     `json:"role"`
	InviterID        string     `json:"inviterId"`
	InviterName      string     `json:"inviterName"`
	OrganizationID   string     `json:"-"`
	OrganizationName string     `json:"-"`
	OrganizationSlug string     `json:"-"`
	TokenHash        string     `json:"-"`
	TokenCiphertext  []byte     `json:"-"`
	ExpiresAt        time.Time  `json:"expiresAt"`
	AcceptedAt       *time.Time `json:"acceptedAt"`
	Status           string     `json:"status"`
	Attempts         int        `json:"-"`
	NextAttempt      time.Time  `json:"-"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"-"`
	SentAt           *time.Time `json:"sentAt"`
	LastReason       string     `json:"lastReason"`
}

type InvitationLease struct {
	Invitation
	Token          string
	LeaseExpiresAt time.Time
}

type InvitationInspection struct {
	Email            string `json:"email"`
	OrganizationName string `json:"organizationName"`
	OrganizationSlug string `json:"organizationSlug"`
	Role             string `json:"role"`
	ExistingAccount  bool   `json:"existingAccount"`
}

const invitationColumns = "i.id,i.email,i.role,i.inviter_user_id::text,u.name,i.organization_id::text,o.name,o.slug,i.token_hash,i.token_ciphertext,i.expires_at,i.accepted_at,i.status,i.attempts,i.next_attempt,i.created_at,i.updated_at,i.sent_at,i.last_reason"

func scanInvitation(row interface{ Scan(...any) error }) (Invitation, error) {
	var item Invitation
	err := row.Scan(&item.ID, &item.Email, &item.Role, &item.InviterID, &item.InviterName, &item.OrganizationID, &item.OrganizationName, &item.OrganizationSlug,
		&item.TokenHash, &item.TokenCiphertext, &item.ExpiresAt, &item.AcceptedAt, &item.Status, &item.Attempts, &item.NextAttempt, &item.CreatedAt, &item.UpdatedAt, &item.SentAt, &item.LastReason)
	return item, dbError(err)
}

func scanInvitationLease(row interface{ Scan(...any) error }) (Invitation, string, *time.Time, error) {
	var item Invitation
	var token string
	var leaseExpires *time.Time
	err := row.Scan(&item.ID, &item.Email, &item.Role, &item.InviterID, &item.InviterName, &item.OrganizationID, &item.OrganizationName, &item.OrganizationSlug,
		&item.TokenHash, &item.TokenCiphertext, &item.ExpiresAt, &item.AcceptedAt, &item.Status, &item.Attempts, &item.NextAttempt, &item.CreatedAt, &item.UpdatedAt, &item.SentAt, &item.LastReason,
		&token, &leaseExpires)
	return item, token, leaseExpires, dbError(err)
}

func invitationFrom(tx pgx.Tx, ctx context.Context, where string, args ...any) (Invitation, error) {
	return scanInvitation(tx.QueryRow(ctx, "SELECT "+invitationColumns+" FROM organization_invitations i JOIN organizations o ON o.id=i.organization_id JOIN users u ON u.id=i.inviter_user_id WHERE "+where, args...))
}

func (s *Store) InviteMember(ctx context.Context, actorID, orgSlug, email, role, tokenHash string, ciphertext []byte, lifetime time.Duration) (Invitation, error) {
	return s.InviteMemberWithTeams(ctx, actorID, orgSlug, email, role, nil, tokenHash, ciphertext, lifetime)
}

func (s *Store) InviteMemberWithTeams(ctx context.Context, actorID, orgSlug, email, role string, teamSlugs []string, tokenHash string, ciphertext []byte, lifetime time.Duration) (Invitation, error) {
	var result Invitation
	if !validResetInput(tokenHash, ciphertext) || lifetime != 7*24*time.Hour || role != "owner" && role != "manager" && role != "admin" && role != "member" {
		return result, errors.New("invalid organization invitation")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	orgID, actorRole, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return result, err
	}
	if role == "owner" && actorRole != "owner" || role == "manager" && actorRole != "owner" && actorRole != "manager" {
		return result, ErrForbidden
	}
	assignedTeams := []string{}
	if len(teamSlugs) > 0 {
		rows, queryErr := tx.Query(ctx, `SELECT t.id::text,EXISTS(SELECT 1 FROM team_memberships tm
			WHERE tm.team_id=t.id AND tm.user_id=$3::bigint) FROM teams t
			WHERE t.organization_id=$1::bigint AND t.slug=ANY($2::text[]) ORDER BY t.id`, orgID, teamSlugs, actorID)
		if queryErr != nil {
			return result, queryErr
		}
		for rows.Next() {
			var teamID string
			var actorOnTeam bool
			if queryErr = rows.Scan(&teamID, &actorOnTeam); queryErr != nil {
				rows.Close()
				return result, queryErr
			}
			if actorRole == "admin" && !actorOnTeam {
				rows.Close()
				return result, ErrForbidden
			}
			assignedTeams = append(assignedTeams, teamID)
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			return result, queryErr
		}
		rows.Close()
	}
	var member bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN memberships m ON m.user_id=u.id
		WHERE m.organization_id=$1::bigint AND (lower(u.email)=lower($2) OR EXISTS(
		 SELECT 1 FROM user_emails ue WHERE ue.user_id=u.id AND ue.is_verified AND lower(ue.email)=lower($2))))`, orgID, email).Scan(&member); err != nil {
		return result, err
	}
	if member {
		return result, ErrConflict
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return result, err
	}
	id, err := randomKey()
	if err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO organization_invitations(id,organization_id,email,role,inviter_user_id,token_hash,token_ciphertext,expires_at,status,attempts,next_attempt,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'pending',0,$9,$9,$9)", id, orgID, email, role, actorID, tokenHash, ciphertext, now.Add(lifetime), now); err != nil {
		return result, dbError(err)
	}
	result, err = invitationFrom(tx, ctx, "i.id=$1", id)
	if err != nil {
		return result, err
	}
	for _, teamID := range assignedTeams {
		if _, err = tx.Exec(ctx, `INSERT INTO organization_invitation_teams(invitation_id,team_id) VALUES($1,$2::bigint)`, id, teamID); err != nil {
			return result, err
		}
	}
	return result, tx.Commit(ctx)
}

func (s *Store) Invitations(ctx context.Context, userID, orgSlug string) ([]Invitation, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	orgID, role, err := organizationMember(ctx, tx, userID, orgSlug, false, false)
	if err != nil {
		return nil, err
	}
	if role != "owner" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	rows, err := tx.Query(ctx, "SELECT "+invitationColumns+" FROM organization_invitations i JOIN organizations o ON o.id=i.organization_id JOIN users u ON u.id=i.inviter_user_id WHERE i.organization_id=$1::bigint AND i.status IN ('pending','processing','sent','failed') ORDER BY i.created_at DESC,i.id DESC", orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Invitation{}
	for rows.Next() {
		item, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, tx.Commit(ctx)
}

func (s *Store) CancelInvitation(ctx context.Context, actorID, orgSlug, invitationID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	orgID, _, err := manageableOrganization(ctx, tx, actorID, orgSlug)
	if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "UPDATE organization_invitations SET status='canceled',updated_at=$3,lease_token='',lease_expires_at=NULL,last_reason='canceled' WHERE organization_id=$1::bigint AND id=$2 AND status IN ('pending','processing','sent','failed')", orgID, invitationID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *Store) ClaimInvitation(ctx context.Context, maxAttempts int, timeout time.Duration) (InvitationLease, error) {
	if maxAttempts < 1 || maxAttempts > 20 || timeout <= 0 {
		return InvitationLease{}, errors.New("invalid invitation delivery configuration")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return InvitationLease{}, err
	}
	defer tx.Rollback(ctx)
	for range 128 {
		item, err := scanInvitation(tx.QueryRow(ctx, "SELECT "+invitationColumns+" FROM organization_invitations i JOIN organizations o ON o.id=i.organization_id JOIN users u ON u.id=i.inviter_user_id WHERE (i.status='pending' AND i.next_attempt<=clock_timestamp()) OR (i.status='processing' AND i.lease_expires_at<=clock_timestamp()) ORDER BY i.next_attempt,i.created_at,i.id LIMIT 1 FOR UPDATE OF i SKIP LOCKED"))
		if errors.Is(err, ErrNotFound) {
			if err = tx.Commit(ctx); err != nil {
				return InvitationLease{}, err
			}
			return InvitationLease{}, ErrNotFound
		}
		if err != nil {
			return InvitationLease{}, err
		}
		var now time.Time
		if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return InvitationLease{}, err
		}
		if !now.Before(item.ExpiresAt) || item.Attempts >= maxAttempts {
			reason := "expired"
			if item.Attempts >= maxAttempts {
				reason = "attempts_exhausted"
			}
			if _, err = tx.Exec(ctx, "UPDATE organization_invitations SET status='failed',last_reason=$2,updated_at=$3,lease_token='',lease_expires_at=NULL WHERE id=$1", item.ID, reason, now); err != nil {
				return InvitationLease{}, err
			}
			continue
		}
		leaseToken, err := randomKey()
		if err != nil {
			return InvitationLease{}, err
		}
		item.Attempts++
		leaseExpires := now.Add(timeout + 30*time.Second)
		reason := item.LastReason
		if item.Status == "processing" {
			reason = "lease_expired"
		}
		if _, err = tx.Exec(ctx, "UPDATE organization_invitations SET status='processing',attempts=$2,updated_at=$3,last_reason=$4,lease_token=$5,lease_expires_at=$6 WHERE id=$1", item.ID, item.Attempts, now, reason, leaseToken, leaseExpires); err != nil {
			return InvitationLease{}, err
		}
		item.Status, item.UpdatedAt, item.LastReason = "processing", now, reason
		if err = tx.Commit(ctx); err != nil {
			return InvitationLease{}, err
		}
		return InvitationLease{Invitation: item, Token: leaseToken, LeaseExpiresAt: leaseExpires}, nil
	}
	if err = tx.Commit(ctx); err != nil {
		return InvitationLease{}, err
	}
	return InvitationLease{}, ErrNotFound
}

func (s *Store) CompleteInvitation(ctx context.Context, lease InvitationLease, outcome notify.Outcome, maxAttempts int) error {
	if maxAttempts < 1 || maxAttempts > 20 || outcome.ElapsedMS < 0 || len(outcome.Reason) > 64 || outcome.RetryAfter != 0 {
		return errors.New("invalid invitation delivery outcome")
	}
	if outcome.Disposition != notify.Delivered && outcome.Disposition != notify.Retry && outcome.Disposition != notify.Permanent {
		return errors.New("invalid invitation delivery disposition")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	item, token, leaseExpires, err := scanInvitationLease(tx.QueryRow(ctx, "SELECT "+invitationColumns+",i.lease_token,i.lease_expires_at FROM organization_invitations i JOIN organizations o ON o.id=i.organization_id JOIN users u ON u.id=i.inviter_user_id WHERE i.id=$1 FOR UPDATE OF i", lease.ID))
	if errors.Is(err, ErrNotFound) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	if item.Status != "processing" || token != lease.Token || leaseExpires == nil || !now.Before(*leaseExpires) {
		return ErrConflict
	}
	status, reason := "failed", outcome.Reason
	next, sentAt := item.NextAttempt, (*time.Time)(nil)
	if !now.Before(item.ExpiresAt) {
		status, reason = "failed", "expired"
	} else if outcome.Disposition == notify.Delivered {
		status, sentAt = "sent", &now
	} else if outcome.Disposition == notify.Retry && item.Attempts < maxAttempts {
		status = "pending"
		next = now.Add(notify.RetryDelay(item.Attempts, 0))
	}
	if _, err = tx.Exec(ctx, "UPDATE organization_invitations SET status=$2,next_attempt=$3,updated_at=$4,sent_at=$5,last_reason=$6,lease_token='',lease_expires_at=NULL WHERE id=$1", item.ID, status, next, now, sentAt, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) InspectInvitation(ctx context.Context, tokenHash string) (InvitationInspection, error) {
	var result InvitationInspection
	if len(tokenHash) != 64 {
		return result, ErrInvalidInvitation
	}
	err := s.pool.QueryRow(ctx, `SELECT i.email,o.name,o.slug,i.role,EXISTS(
		SELECT 1 FROM users u WHERE lower(u.email)=lower(i.email) OR EXISTS(
		 SELECT 1 FROM user_emails ue WHERE ue.user_id=u.id AND ue.is_verified AND lower(ue.email)=lower(i.email)))
		FROM organization_invitations i JOIN organizations o ON o.id=i.organization_id
		WHERE i.token_hash=$1 AND i.status='sent' AND i.expires_at>clock_timestamp()`, tokenHash).Scan(&result.Email, &result.OrganizationName, &result.OrganizationSlug, &result.Role, &result.ExistingAccount)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrInvalidInvitation
	}
	return result, err
}

func (s *Store) AcceptInvitation(ctx context.Context, tokenHash, existingUserID string, authGeneration int64, newName string, passwordHash []byte) (User, Organization, error) {
	var user User
	var org Organization
	if len(tokenHash) != 64 || existingUserID == "" && (newName == "" || len(passwordHash) == 0) {
		return user, org, ErrInvalidInvitation
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return user, org, err
	}
	defer tx.Rollback(ctx)
	if existingUserID == "" {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73042190)`); err != nil {
			return user, org, err
		}
	}
	var invitationID, orgID, email, role string
	if err = tx.QueryRow(ctx, "SELECT id,organization_id::text,email,role FROM organization_invitations WHERE token_hash=$1", tokenHash).Scan(&invitationID, &orgID, &email, &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user, org, ErrInvalidInvitation
		}
		return user, org, err
	}
	if existingUserID != "" {
		if err = pinUserGeneration(ctx, tx, existingUserID, authGeneration); err != nil {
			return user, org, ErrInvalidInvitation
		}
	}
	if err = tx.QueryRow(ctx, "SELECT id::text,slug,name FROM organizations WHERE id=$1::bigint FOR UPDATE", orgID).Scan(&org.ID, &org.Slug, &org.Name); err != nil {
		return user, org, ErrInvalidInvitation
	}
	var status string
	var expires time.Time
	var accepted *time.Time
	if err = tx.QueryRow(ctx, "SELECT status,expires_at,accepted_at FROM organization_invitations WHERE token_hash=$1 FOR UPDATE", tokenHash).Scan(&status, &expires, &accepted); err != nil {
		return user, org, ErrInvalidInvitation
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return user, org, err
	}
	if status != "sent" || accepted != nil || !now.Before(expires) {
		return user, org, ErrInvalidInvitation
	}
	if existingUserID != "" {
		if err = tx.QueryRow(ctx, "SELECT id::text,email,name,auth_generation,is_instance_admin FROM users WHERE id=$1::bigint AND is_active", existingUserID).Scan(&user.ID, &user.Email, &user.Name, &user.AuthGeneration, &user.IsInstanceAdmin); err != nil {
			return user, org, ErrInvalidInvitation
		}
		if !strings.EqualFold(user.Email, email) {
			var verified bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_emails
				WHERE user_id=$1::bigint AND is_verified AND lower(email)=lower($2))`, existingUserID, email).Scan(&verified); err != nil {
				return user, org, err
			}
			if !verified {
				return user, org, ErrInvalidInvitation
			}
		}
	} else {
		var matchingAccount string
		// User deletion locks the account before its organizations; this transaction
		// already holds the organization, so duplicate detection must remain a read.
		err = tx.QueryRow(ctx, `SELECT u.id::text FROM users u
			WHERE lower(u.email)=lower($1) OR EXISTS(SELECT 1 FROM user_emails ue WHERE ue.user_id=u.id AND ue.is_verified AND lower(ue.email)=lower($1))
			ORDER BY (lower(u.email)=lower($1)) DESC LIMIT 1`, email).Scan(&matchingAccount)
		if err == nil {
			return user, org, ErrInvalidInvitation
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return user, org, err
		}
		if err = tx.QueryRow(ctx, "INSERT INTO users(email,password_hash,name) VALUES($1,$2,$3) RETURNING id::text,email,name,auth_generation,is_instance_admin", email, passwordHash, newName).Scan(&user.ID, &user.Email, &user.Name, &user.AuthGeneration, &user.IsInstanceAdmin); err != nil {
			return user, org, dbError(err)
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO memberships(organization_id,user_id,role) VALUES($1::bigint,$2::bigint,$3)", orgID, user.ID, role); err != nil {
		return user, org, dbError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO team_memberships(team_id,user_id)
		SELECT iit.team_id,$2::bigint FROM organization_invitation_teams iit
		JOIN teams t ON t.id=iit.team_id AND t.organization_id=$3::bigint
		WHERE iit.invitation_id=$1 ON CONFLICT DO NOTHING`, invitationID, user.ID, orgID); err != nil {
		return user, org, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM organization_invitation_teams WHERE invitation_id=$1`, invitationID); err != nil {
		return user, org, err
	}
	org.Role = role
	if _, err = tx.Exec(ctx, "UPDATE organization_invitations SET status='accepted',accepted_at=clock_timestamp(),updated_at=clock_timestamp(),lease_token='',lease_expires_at=NULL WHERE token_hash=$1", tokenHash); err != nil {
		return user, org, err
	}
	if _, err = tx.Exec(ctx, "UPDATE organization_invitations SET status='canceled',updated_at=clock_timestamp(),lease_token='',lease_expires_at=NULL,last_reason='accepted_other' WHERE organization_id=$1::bigint AND lower(email)=lower($2) AND status IN ('pending','processing','sent','failed')", orgID, email); err != nil {
		return user, org, err
	}
	return user, org, tx.Commit(ctx)
}
