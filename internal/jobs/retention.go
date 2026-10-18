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

// StartEventRetention expires receipts without discarding their issue history.
func StartEventRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		events, err := db.CleanupEvents(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Event retention pass failed", "entryPoint", "event_retention_worker", "operation", "cleanup_events")
		} else if events > 0 {
			slog.Info("Event retention cleanup committed", "entryPoint", "event_retention_worker", "events", events)
			return true
		}
		return false
	})
}

// StartUptimeRetention expires completed history even when probing is disabled.
func StartUptimeRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		checks, err := db.CleanupMonitorChecks(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Uptime retention pass failed", "entryPoint", "uptime_retention_worker", "operation", "cleanup_monitor_checks")
		} else if checks > 0 {
			slog.Info("Uptime retention cleanup committed", "entryPoint", "uptime_retention_worker", "checks", checks)
			return true
		}
		return false
	})
}

// StartFileRetention expires durable bytes independently of ingestion switches.
func StartFileRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		maps, attachments, items, err := db.CleanupFiles(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("File retention pass failed", "entryPoint", "file_retention_worker", "operation", "cleanup_files")
		} else if maps > 0 || attachments > 0 || items > 0 {
			slog.Info("File retention cleanup committed", "entryPoint", "file_retention_worker", "sourceMaps", maps, "attachments", attachments, "items", items)
			return true
		}
		return false
	})
}

// StartReleaseRetention shares the in-process receipt maintenance lifecycle.
func StartReleaseRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		releases, err := db.CleanupReleases(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Release retention pass failed", "entryPoint", "release_retention_worker", "operation", "cleanup_releases")
		} else if releases > 0 {
			slog.Info("Release retention cleanup committed", "entryPoint", "release_retention_worker", "removedReleases", releases)
			return true
		}
		return false
	})
}

// StartPendingUserReports reclaims reports whose matching error never arrived.
// Its fixed pending lifetime is independent of all event/file retention days.
func StartPendingUserReports(parent context.Context, db *store.Store, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		removed, err := db.CleanupPendingUserReports(ctx)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Pending user report cleanup failed", "entryPoint", "pending_user_reports_worker", "operation", "cleanup_pending_user_reports")
		} else if removed > 0 {
			slog.Info("Pending user report cleanup committed", "entryPoint", "pending_user_reports_worker", "removedPendingReports", removed)
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
