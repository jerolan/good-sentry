package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"goodsentry/internal/spanarchive"
	"goodsentry/internal/store"
)

// StartLogRetention drains bounded committed work on startup and after idle
// intervals. The provider coordinates replicas. Cancellation stops SQL and the
// idle timer; the returned stop function is safe to call more than once.
func StartLogRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	return StartLogRetentionWithArchive(parent, db, days, 0, interval)
}

// StartLogRetentionWithArchive copies retained logs into the local archive
// before the ordinary expiry pass. The threshold is capped by the log lifetime.
func StartLogRetentionWithArchive(parent context.Context, db *store.Store, days, archiveAfterDays int, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		archived := int64(0)
		if archiveAfterDays > 0 {
			archiveDays := min(archiveAfterDays, days)
			cutoff := time.Now().UTC().Add(-time.Duration(archiveDays) * 24 * time.Hour)
			var err error
			archived, err = db.ArchiveLogs(ctx, cutoff, 250)
			if ctx.Err() != nil {
				return false
			}
			if err != nil {
				slog.Error("Log archive pass failed", "entryPoint", "log_retention_worker", "operation", "archive_logs")
			}
			removedArchive, cleanupErr := db.CleanupLogArchive(ctx, time.Now().UTC().Add(-time.Duration(days)*24*time.Hour))
			if ctx.Err() != nil {
				return false
			}
			if cleanupErr != nil {
				slog.Error("Log archive retention failed", "entryPoint", "log_retention_worker", "operation", "cleanup_log_archive")
			} else if removedArchive > 0 {
				archived += removedArchive
			}
		}
		logs, batches, err := db.CleanupLogs(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			// SQL and telemetry may appear in driver errors. Report the failed
			// operation without copying those values into application logs.
			slog.Error("Log retention pass failed", "entryPoint", "log_retention_worker", "operation", "cleanup_logs")
		} else if logs > 0 || batches > 0 || archived > 0 {
			slog.Info("Log retention cleanup committed", "entryPoint", "log_retention_worker", "logs", logs, "batches", batches, "archived", archived)
			return true
		}
		return false
	})
}

// StartLogRetentionWithS3Archive uses the S3-compatible object backend for
// logs. Upload precedes catalog/local materialization; expiry removes object,
// catalog and cache at the same receipt cutoff.
func StartLogRetentionWithS3Archive(parent context.Context, db *store.Store, days, archiveAfterDays int, objects spanarchive.Objects, prefix string, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		archived := int64(0)
		if archiveAfterDays > 0 {
			archiveDays := min(archiveAfterDays, days)
			cutoff := time.Now().UTC().Add(-time.Duration(archiveDays) * 24 * time.Hour)
			var err error
			archived, err = db.ArchiveLogsS3(ctx, cutoff, 250, objects, prefix)
			if ctx.Err() != nil {
				return false
			}
			if err != nil {
				slog.Error("Log S3 archive pass failed", "entryPoint", "log_retention_worker", "operation", "archive_logs_s3")
			}
			expired := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
			removedObjects, cleanupErr := db.CleanupLogArchiveS3(ctx, expired, objects, 250)
			if ctx.Err() != nil {
				return false
			}
			if cleanupErr != nil {
				slog.Error("Log S3 archive retention failed", "entryPoint", "log_retention_worker", "operation", "cleanup_log_archive_s3")
			} else if removedObjects > 0 {
				archived += removedObjects
			}
			removedLocal, localErr := db.CleanupLogArchive(ctx, expired)
			if localErr != nil {
				slog.Error("Log archive cache retention failed", "entryPoint", "log_retention_worker", "operation", "cleanup_log_archive")
			} else if removedLocal > 0 {
				archived += removedLocal
			}
		}
		logs, batches, err := db.CleanupLogs(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Log retention pass failed", "entryPoint", "log_retention_worker", "operation", "cleanup_logs")
		} else if logs > 0 || batches > 0 || archived > 0 {
			slog.Info("Log retention cleanup committed", "entryPoint", "log_retention_worker", "logs", logs, "batches", batches, "archived", archived)
			return true
		}
		return false
	})
}

// StartTransactionRetention shares the log worker's backlog and cancellation
// lifecycle. Ingestion feature switches do not disable expiry of stored rows.
func StartTransactionRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	return startTransactionRetention(parent, db, days, interval, nil)
}

func StartTransactionRetentionWithArchive(parent context.Context, db *store.Store, days int, interval time.Duration, archive store.SpanArchiveOptions) func() {
	return startTransactionRetention(parent, db, days, interval, &archive)
}

func startTransactionRetention(parent context.Context, db *store.Store, days int, interval time.Duration, archive *store.SpanArchiveOptions) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		var transactions, spans, objects int64
		var err error
		archivedTransactions := int64(0)
		if archive != nil && archive.Backend == "local" && archive.AfterDays > 0 {
			archiveDays := min(archive.AfterDays, days)
			cutoff := time.Now().UTC().Add(-time.Duration(archiveDays) * 24 * time.Hour)
			if n, archiveErr := db.ArchiveTransactions(ctx, cutoff, 250); archiveErr != nil {
				slog.Error("Transaction archive pass failed", "entryPoint", "transaction_retention_worker", "operation", "archive_transactions")
			} else {
				archivedTransactions += n
			}
			expired := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
			if n, cleanupErr := db.CleanupTransactionArchive(ctx, expired); cleanupErr != nil {
				slog.Error("Transaction archive retention failed", "entryPoint", "transaction_retention_worker", "operation", "cleanup_transaction_archive")
			} else {
				archivedTransactions += n
			}
		}
		if archive != nil && archive.Backend == "s3" && archive.AfterDays > 0 {
			archiveDays := min(archive.AfterDays, days)
			cutoff := time.Now().UTC().Add(-time.Duration(archiveDays) * 24 * time.Hour)
			if n, archiveErr := db.ArchiveTransactionsS3(ctx, cutoff, 250, archive.Objects, archive.S3Prefix); archiveErr != nil {
				slog.Error("Transaction S3 archive pass failed", "entryPoint", "transaction_retention_worker", "operation", "archive_transactions_s3")
			} else {
				archivedTransactions += n
			}
			expired := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
			if n, cleanupErr := db.CleanupTransactionArchiveS3(ctx, expired, archive.Objects, 250); cleanupErr != nil {
				slog.Error("Transaction S3 archive retention failed", "entryPoint", "transaction_retention_worker", "operation", "cleanup_transaction_archive_s3")
			} else {
				archivedTransactions += n
			}
		}
		if archive == nil {
			transactions, spans, err = db.CleanupTransactions(ctx, days)
		} else {
			transactions, spans, objects, err = db.CleanupTransactionsWithArchive(ctx, days, *archive)
		}
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Transaction retention pass failed", "entryPoint", "transaction_retention_worker", "operation", "cleanup_transactions")
		} else if transactions > 0 || spans > 0 || objects > 0 || archivedTransactions > 0 {
			slog.Info("Transaction retention cleanup committed", "entryPoint", "transaction_retention_worker", "transactions", transactions, "spans", spans, "archiveObjects", objects, "archivedTransactions", archivedTransactions)
			return true
		}
		return false
	})
}

// StartEventRetention expires receipts without discarding their issue history.
func StartEventRetention(parent context.Context, db *store.Store, days int, interval time.Duration) func() {
	return StartEventRetentionWithArchive(parent, db, days, 0, interval)
}

// StartEventRetentionWithArchive copies retained error/message receipts into
// the local durable archive before the ordinary expiry pass. archiveAfterDays
// is capped by the event lifetime, so an archive never outlives its source.
func StartEventRetentionWithArchive(parent context.Context, db *store.Store, days, archiveAfterDays int, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		archived := int64(0)
		if archiveAfterDays > 0 {
			archiveDays := min(archiveAfterDays, days)
			cutoff := time.Now().UTC().Add(-time.Duration(archiveDays) * 24 * time.Hour)
			var err error
			archived, err = db.ArchiveEvents(ctx, cutoff, 250)
			if ctx.Err() != nil {
				return false
			}
			if err != nil {
				slog.Error("Event archive pass failed", "entryPoint", "event_retention_worker", "operation", "archive_events")
			}
			removedArchive, cleanupErr := db.CleanupEventArchive(ctx, time.Now().UTC().Add(-time.Duration(days)*24*time.Hour))
			if ctx.Err() != nil {
				return false
			}
			if cleanupErr != nil {
				slog.Error("Event archive retention failed", "entryPoint", "event_retention_worker", "operation", "cleanup_event_archive")
			} else if removedArchive > 0 {
				archived += removedArchive
			}
		}
		events, err := db.CleanupEvents(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Event retention pass failed", "entryPoint", "event_retention_worker", "operation", "cleanup_events")
		} else if events > 0 || archived > 0 {
			slog.Info("Event retention cleanup committed", "entryPoint", "event_retention_worker", "events", events, "archived", archived)
			return true
		}
		return false
	})
}

// StartEventRetentionWithS3Archive uses the S3-compatible object backend for
// errors. Objects are uploaded before their catalog/local read rows; expiry
// removes objects, catalog rows and local copies at the same receipt cutoff.
func StartEventRetentionWithS3Archive(parent context.Context, db *store.Store, days, archiveAfterDays int, objects spanarchive.Objects, prefix string, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		archived := int64(0)
		if archiveAfterDays > 0 {
			archiveDays := min(archiveAfterDays, days)
			cutoff := time.Now().UTC().Add(-time.Duration(archiveDays) * 24 * time.Hour)
			var err error
			archived, err = db.ArchiveEventsS3(ctx, cutoff, 250, objects, prefix)
			if ctx.Err() != nil {
				return false
			}
			if err != nil {
				slog.Error("Event S3 archive pass failed", "entryPoint", "event_retention_worker", "operation", "archive_events_s3")
			}
			expired := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
			removedObjects, cleanupErr := db.CleanupEventArchiveS3(ctx, expired, objects, 250)
			if ctx.Err() != nil {
				return false
			}
			if cleanupErr != nil {
				slog.Error("Event S3 archive retention failed", "entryPoint", "event_retention_worker", "operation", "cleanup_event_archive_s3")
			} else if removedObjects > 0 {
				archived += removedObjects
			}
			removedLocal, localErr := db.CleanupEventArchive(ctx, expired)
			if localErr != nil {
				slog.Error("Event archive cache retention failed", "entryPoint", "event_retention_worker", "operation", "cleanup_event_archive")
			} else if removedLocal > 0 {
				archived += removedLocal
			}
		}
		events, err := db.CleanupEvents(ctx, days)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Event retention pass failed", "entryPoint", "event_retention_worker", "operation", "cleanup_events")
		} else if events > 0 || archived > 0 {
			slog.Info("Event retention cleanup committed", "entryPoint", "event_retention_worker", "events", events, "archived", archived)
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

// OAuth secret retention removes only expired or revoked credential metadata.
func StartOAuthSecretRetention(parent context.Context, db *store.Store, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		removed, err := db.CleanupOAuthSecrets(ctx)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("OAuth secret retention pass failed", "entryPoint", "oauth_retention_worker", "operation", "cleanup_oauth_secrets")
		} else if removed > 0 {
			slog.Info("OAuth secret retention cleanup committed", "entryPoint", "oauth_retention_worker", "secrets", removed)
			return true
		}
		return false
	})
}

// StartProjectKeyRateCleanup bounds fixed-window quota history independently of
// ingestion feature switches and application restarts.
func StartProjectKeyRateCleanup(parent context.Context, db *store.Store, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		removed, err := db.CleanupProjectKeyRateUsage(ctx, 14*24*time.Hour, 250)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Project-key rate cleanup failed", "entryPoint", "project_key_rate_worker", "operation", "cleanup_project_key_rate")
		} else if removed > 0 {
			slog.Info("Project-key rate cleanup committed", "entryPoint", "project_key_rate_worker", "removedWindows", removed)
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

// StartFeedbackAssociations catches up associations skipped by bounded capture.
// It binds retained receipts and never deletes feedback or error history.
func StartFeedbackAssociations(parent context.Context, db *store.Store, interval time.Duration) func() {
	return startRetention(parent, interval, func(ctx context.Context) bool {
		bound, err := db.BindPendingFeedbackAssociations(ctx)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			slog.Error("Feedback association pass failed", "entryPoint", "feedback_associations_worker", "operation", "bind_pending_feedback_associations")
		} else if bound > 0 {
			slog.Info("Feedback associations committed", "entryPoint", "feedback_associations_worker", "boundFeedback", bound)
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
