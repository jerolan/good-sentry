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
	return startRetention(parent, interval, func(ctx context.Context) bool {
		logs, batches, err := db.CleanupLogs(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			// SQL and telemetry may appear in driver errors. Report the failed
			// operation without copying those values into application logs.
			slog.Error("Log retention pass failed", "entryPoint", "log_retention_worker", "operation", "cleanup_logs")
		} else if logs > 0 || batches > 0 {
			slog.Info("Log retention cleanup committed", "entryPoint", "log_retention_worker", "logs", logs, "batches", batches)
			return true
		}
		return false
	})
}

// StartTransactionRetention shares the log worker's backlog and cancellation
// lifecycle. Ingestion feature switches do not disable expiry of stored rows.
func StartTransactionRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		transactions, spans, err := db.CleanupTransactions(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Transaction retention pass failed", "entryPoint", "transaction_retention_worker", "operation", "cleanup_transactions")
		} else if transactions > 0 || spans > 0 {
			slog.Info("Transaction retention cleanup committed", "entryPoint", "transaction_retention_worker", "transactions", transactions, "spans", spans)
			return true
		}
		return false
	})
}

func startRetention(parent context.Context, interval time.Duration, cleanup func(context.Context) bool) func() {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	wg.Go(func() {
		for ctx.Err() == nil {
			progress := cleanup(ctx)
			if ctx.Err() != nil {
				return
			}
			if progress {
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
