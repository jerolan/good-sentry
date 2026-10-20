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

type AccountMailOptions struct {
	Workers, MaxAttempts int
	Timeout              time.Duration
	Domain               string
}

func StartAccountMail(parent context.Context, db *store.Store, sender *notify.Sender, box *accountmail.SecretBox, options AccountMailOptions) func() {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	for range options.Workers {
		wg.Go(func() { runAccountMail(ctx, db, sender, box, options) })
	}
	var once sync.Once
	return func() { once.Do(func() { cancel(); wg.Wait() }) }
}

func runAccountMail(ctx context.Context, db *store.Store, sender *notify.Sender, box *accountmail.SecretBox, options AccountMailOptions) {
	for ctx.Err() == nil {
		lease, err := db.ClaimPasswordReset(ctx, options.MaxAttempts, options.Timeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !errors.Is(err, store.ErrNotFound) {
				slog.Error("Password reset could not be claimed", "entryPoint", "account_mail_worker", "operation", "claim", "error", err)
			}
			if !waitForWork(ctx) {
				return
			}
			continue
		}
		token, err := box.Open(lease.TokenHash, lease.TokenCiphertext)
		if err != nil {
			_ = db.CompletePasswordReset(ctx, lease, notify.Outcome{Disposition: notify.Permanent, Reason: "invalid_payload"}, options.MaxAttempts)
			continue
		}
		resetURL := strings.TrimRight(options.Domain, "/") + "/?resetToken=" + url.QueryEscape(token)
		outcome, err := sender.DeliverAccountMail(ctx, notify.AccountMail{
			ID: lease.ID, Name: lease.UserName, Recipient: lease.Recipient, ResetURL: resetURL, ExpiresAt: lease.PasswordReset.ExpiresAt,
		})
		if err != nil || ctx.Err() != nil {
			return
		}
		if err := db.CompletePasswordReset(ctx, lease, outcome, options.MaxAttempts); err != nil && !errors.Is(err, store.ErrConflict) && ctx.Err() == nil {
			slog.Error("Password reset result could not be stored", "entryPoint", "account_mail_worker", "operation", "complete", "resetId", lease.ID, "error", err)
		}
	}
}
