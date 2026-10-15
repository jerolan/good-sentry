package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"goodsentry/internal/notify"
	"goodsentry/internal/store"
)

type AlertOptions struct {
	Workers, MaxAttempts int
	Timeout              time.Duration
}

// StartAlerts runs the persisted outbox. The caller owns the sender and closes
// it after stopping workers; no database transaction spans external I/O.
func StartAlerts(parent context.Context, db *store.Store, sender *notify.Sender, options AlertOptions) func() {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	for range options.Workers {
		wg.Go(func() { runAlerts(ctx, db, sender, options) })
	}
	var once sync.Once
	return func() { once.Do(func() { cancel(); wg.Wait() }) }
}

func runAlerts(ctx context.Context, db *store.Store, sender *notify.Sender, options AlertOptions) {
	for ctx.Err() == nil {
		lease, err := db.ClaimAlertDelivery(ctx, options.MaxAttempts, options.Timeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !errors.Is(err, store.ErrNotFound) {
				slog.Error("Alert delivery could not be claimed", "entryPoint", "alert_worker", "operation", "claim", "error", err)
			}
			if !waitForWork(ctx) {
				return
			}
			continue
		}
		outcome, err := sender.Deliver(ctx, lease.Delivery)
		if err != nil || ctx.Err() != nil {
			return
		}
		if err := db.CompleteAlertDelivery(ctx, lease, outcome, options.MaxAttempts); err != nil && !errors.Is(err, store.ErrConflict) && ctx.Err() == nil {
			slog.Error("Alert delivery result could not be stored", "entryPoint", "alert_worker", "operation", "complete", "deliveryId", lease.ID, "error", err)
		}
	}
}
