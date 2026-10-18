package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"goodsentry/internal/store"
)

// StartLogRetention drains bounded committed work on startup and after idle
// intervals. The provider coordinates replicas. Cancellation stops SQL and the
// idle timer; the returned stop function is safe to call more than once.
func StartLogRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	wg.Go(func() {
		for ctx.Err() == nil {
			logs, batches, err := db.CleanupLogs(ctx, days)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				// SQL and telemetry may appear in driver errors. Report the failed
				// operation without copying those values into application logs.
				slog.Error("Log retention pass failed", "entryPoint", "log_retention_worker", "operation", "cleanup_logs")
			} else if logs > 0 || batches > 0 {
				slog.Info("Log retention cleanup committed", "entryPoint", "log_retention_worker", "logs", logs, "batches", batches)
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
