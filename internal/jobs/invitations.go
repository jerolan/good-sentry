package jobs

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"goodsentry/internal/accountmail"
	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

type InvitationOptions struct {
	Workers, MaxAttempts int
	Timeout              time.Duration
	Domain               string
}

func StartInvitations(parent context.Context, db *store.Store, sender *notify.Sender, box *accountmail.SecretBox, options InvitationOptions) func() {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	for range options.Workers {
		wg.Go(func() { runInvitations(ctx, db, sender, box, options) })
	}
	var once sync.Once
	return func() { once.Do(func() { cancel(); wg.Wait() }) }
}

func runInvitations(ctx context.Context, db *store.Store, sender *notify.Sender, box *accountmail.SecretBox, options InvitationOptions) {
	for ctx.Err() == nil {
		lease, err := db.ClaimInvitation(ctx, options.MaxAttempts, options.Timeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !errors.Is(err, store.ErrNotFound) {
				slog.Error("Invitation could not be claimed", "entryPoint", "invitation_worker", "operation", "claim", "error", err)
			}
			if !waitForWork(ctx) {
				return
			}
			continue
		}
		token, err := box.Open(lease.TokenHash, lease.TokenCiphertext)
		if err != nil {
			_ = db.CompleteInvitation(ctx, lease, notify.Outcome{Disposition: notify.Permanent, Reason: "invalid_payload"}, options.MaxAttempts)
			continue
		}
		inviteURL := strings.TrimRight(options.Domain, "/") + "/?inviteToken=" + url.QueryEscape(token)
		outcome, err := sender.DeliverInvitationMail(ctx, notify.InvitationMail{
			ID: lease.ID, Name: "there", Recipient: lease.Email, Organization: lease.OrganizationName, Inviter: lease.InviterName, Role: lease.Role, InviteURL: inviteURL, ExpiresAt: lease.Invitation.ExpiresAt,
		})
		if err != nil || ctx.Err() != nil {
			return
		}
		if err := db.CompleteInvitation(ctx, lease, outcome, options.MaxAttempts); err != nil && !errors.Is(err, store.ErrConflict) && ctx.Err() == nil {
			slog.Error("Invitation delivery result could not be stored", "entryPoint", "invitation_worker", "operation", "complete", "invitationId", lease.ID, "error", err)
		}
	}
}
