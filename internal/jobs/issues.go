package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"goodsentry/internal/store"
)

// StartIssueMaintenance drains durable mute deadlines on startup and after each
// idle interval. Issue row locks coordinate replicas without a separate service.
func StartIssueMaintenance(parent context.Context, db *store.Store, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	wg.Go(func() {
		for ctx.Err() == nil {
			processed, err := db.ExpireIssueIgnore(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				slog.Error("Issue mute could not be expired", "entryPoint", "issue_maintenance_worker", "operation", "expire", "error", err)
			}
			if err == nil && processed {
				continue
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})
	var once sync.Once
	return func() { once.Do(func() { cancel(); wg.Wait() }) }
}
