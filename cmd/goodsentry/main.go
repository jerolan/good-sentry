package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/notify"
	"goodsentry/internal/server"
	"goodsentry/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("GoodSentry stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.MCP || cfg.DuckDB || cfg.Admin || cfg.OpenAPI || cfg.ValkeyURL != "" {
		return errors.New("this increment does not yet provide MCP, DuckDB, admin, OpenAPI or Valkey; disable those options")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	db, err := store.Open(startup, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()
	stopArtifactChunks := jobs.StartArtifactChunks(ctx, db, time.Duration(cfg.ArtifactChunkTTLHours)*time.Hour)
	defer stopArtifactChunks()
	stopTransactionSpans := jobs.StartTransactionSpans(ctx, db)
	defer stopTransactionSpans()
	stopIssueMaintenance := jobs.StartIssueMaintenance(ctx, db, time.Duration(cfg.IssueMaintenanceIntervalSeconds)*time.Second)
	defer stopIssueMaintenance()
	stopLogRetention := jobs.StartLogRetention(ctx, db, cfg.LogRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopLogRetention()
	stopTransactionRetention := jobs.StartTransactionRetention(ctx, db, cfg.TransactionRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopTransactionRetention()
	stopEventRetention := jobs.StartEventRetention(ctx, db, cfg.EventRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopEventRetention()
	stopUptimeRetention := jobs.StartUptimeRetention(ctx, db, cfg.UptimeRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopUptimeRetention()
	stopFileRetention := jobs.StartFileRetention(ctx, db, cfg.FileRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopFileRetention()
	stopReleaseRetention := jobs.StartReleaseRetention(ctx, db, cfg.ReleaseRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopReleaseRetention()
	stopPendingUserReports := jobs.StartPendingUserReports(ctx, db, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopPendingUserReports()
	stopFeedbackAssociations := jobs.StartFeedbackAssociations(ctx, db, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopFeedbackAssociations()
	alertTimeout := time.Duration(cfg.AlertTimeoutSeconds) * time.Second
	sender, err := notify.NewSender(notify.Options{
		Domain: cfg.Domain, EmailURL: cfg.EmailURL, FromEmail: cfg.FromEmail,
		Timeout: alertTimeout, AllowPrivate: cfg.WebhookAllowPrivate, Console: os.Stdout,
	})
	if err != nil {
		return err
	}
	defer sender.Close()
	stopAlerts := jobs.StartAlerts(ctx, db, sender, jobs.AlertOptions{
		Workers: cfg.AlertWorkers, MaxAttempts: cfg.AlertMaxAttempts, Timeout: alertTimeout,
	})
	defer stopAlerts()
	cfg.AlertDelivery = true
	if cfg.Uptime {
		stopWorkers := jobs.StartMonitors(ctx, db, cfg.MonitorWorkers, cfg.UptimeAllowPrivate)
		defer stopWorkers()
	}
	srv := &http.Server{Addr: cfg.Addr, Handler: server.New(cfg, db), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() {
		slog.Info("GoodSentry listening", "address", cfg.Addr, "domain", cfg.Domain)
		done <- srv.ListenAndServe()
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
