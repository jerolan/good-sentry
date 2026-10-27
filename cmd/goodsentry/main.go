package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
	"goodsentry/internal/accountmail"
	"goodsentry/internal/config"
	"goodsentry/internal/jobs"
	"goodsentry/internal/notify"
	"goodsentry/internal/server"
	"goodsentry/internal/spanarchive"
	"goodsentry/internal/store"
)

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		err = runAdmin(os.Args[2:])
	} else {
		err = run()
	}
	if err != nil {
		slog.Error("GoodSentry stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateRuntimeAvailability(); err != nil {
		return err
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
	archiveOptions, err := configureSpanArchive(ctx, db, cfg)
	if err != nil {
		return err
	}
	var defaultUser *store.User
	if cfg.AuthenticationDisabled() {
		password := cfg.DefaultPassword
		if password == "" {
			password, err = accountmail.NewToken()
			if err != nil {
				return err
			}
		}
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			return err
		}
		user, _, err := db.EnsureDefaultOwner(ctx, cfg.DefaultEmail, passwordHash, cfg.DefaultName, cfg.DefaultOrganization)
		if err != nil {
			return err
		}
		defaultUser = &user
		slog.Info("Authentication disabled; using implicit owner", "email", user.Email, "organization", cfg.DefaultOrganization)
	}
	stopArtifactChunks := jobs.StartArtifactChunks(ctx, db, time.Duration(cfg.ArtifactChunkTTLHours)*time.Hour)
	defer stopArtifactChunks()
	stopTransactionSpans := jobs.StartTransactionSpans(ctx, db)
	defer stopTransactionSpans()
	stopIssueMaintenance := jobs.StartIssueMaintenance(ctx, db, time.Duration(cfg.IssueMaintenanceIntervalSeconds)*time.Second)
	defer stopIssueMaintenance()
	stopNativeReprocessing := jobs.StartNativeReprocessing(ctx, db, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopNativeReprocessing()
	var stopLogRetention func()
	if archiveOptions != nil && archiveOptions.Backend == "local" {
		stopLogRetention = jobs.StartLogRetentionWithArchive(ctx, db, cfg.LogRetentionDays, cfg.ArchiveAfterDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	} else {
		stopLogRetention = jobs.StartLogRetention(ctx, db, cfg.LogRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	}
	defer stopLogRetention()
	var stopTransactionRetention func()
	if archiveOptions == nil {
		stopTransactionRetention = jobs.StartTransactionRetention(ctx, db, cfg.TransactionRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	} else {
		stopTransactionRetention = jobs.StartTransactionRetentionWithArchive(ctx, db, cfg.TransactionRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second, *archiveOptions)
	}
	defer stopTransactionRetention()
	var stopEventRetention func()
	if archiveOptions == nil {
		stopEventRetention = jobs.StartEventRetention(ctx, db, cfg.EventRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	} else if archiveOptions.Backend == "s3" {
		stopEventRetention = jobs.StartEventRetentionWithS3Archive(ctx, db, cfg.EventRetentionDays, cfg.ArchiveAfterDays, archiveOptions.Objects, archiveOptions.S3Prefix, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	} else {
		stopEventRetention = jobs.StartEventRetentionWithArchive(ctx, db, cfg.EventRetentionDays, cfg.ArchiveAfterDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	}
	defer stopEventRetention()
	stopUptimeRetention := jobs.StartUptimeRetention(ctx, db, cfg.UptimeRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopUptimeRetention()
	stopFileRetention := jobs.StartFileRetention(ctx, db, cfg.FileRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopFileRetention()
	stopReleaseRetention := jobs.StartReleaseRetention(ctx, db, cfg.ReleaseRetentionDays, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopReleaseRetention()
	stopOAuthSecrets := jobs.StartOAuthSecretRetention(ctx, db, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopOAuthSecrets()
	stopProjectKeyRate := jobs.StartProjectKeyRateCleanup(ctx, db, time.Duration(cfg.RetentionIntervalSeconds)*time.Second)
	defer stopProjectKeyRate()
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
	accountBox, err := accountmail.NewSecretBox(cfg.Secret)
	if err != nil {
		return err
	}
	stopAccountMail := jobs.StartAccountMail(ctx, db, sender, accountBox, jobs.AccountMailOptions{
		Workers: cfg.AlertWorkers, MaxAttempts: cfg.AlertMaxAttempts, Timeout: alertTimeout, Domain: cfg.Domain,
	})
	defer stopAccountMail()
	stopInvitations := jobs.StartInvitations(ctx, db, sender, accountBox, jobs.InvitationOptions{
		Workers: cfg.AlertWorkers, MaxAttempts: cfg.AlertMaxAttempts, Timeout: alertTimeout, Domain: cfg.Domain,
	})
	defer stopInvitations()
	stopAlerts := jobs.StartAlerts(ctx, db, sender, jobs.AlertOptions{
		Workers: cfg.AlertWorkers, MaxAttempts: cfg.AlertMaxAttempts, Timeout: alertTimeout,
	})
	defer stopAlerts()
	cfg.AlertDelivery = true
	if cfg.Uptime {
		stopWorkers := jobs.StartMonitors(ctx, db, cfg.MonitorWorkers, cfg.UptimeAllowPrivate)
		defer stopWorkers()
	}
	srv := &http.Server{Addr: cfg.Addr, Handler: server.NewWithAccountMailAndDefaultUser(cfg, db, sender, accountBox, defaultUser), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
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

func configureSpanArchive(ctx context.Context, db *store.Store, cfg config.Config) (*store.SpanArchiveOptions, error) {
	if !cfg.ArchiveEnabled {
		if err := db.RequireSpanArchiveDisabled(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	var objects spanarchive.Objects
	switch cfg.ArchiveBackend {
	case "local":
		local, err := spanarchive.NewLocalObjects(cfg.ArchiveDir)
		if err != nil {
			return nil, fmt.Errorf("configure local span archive: %w", err)
		}
		objects = local
	case "s3":
		remote, err := spanarchive.NewS3Objects(ctx, cfg.ArchiveS3Region, cfg.ArchiveS3Bucket, cfg.ArchiveS3Endpoint)
		if err != nil {
			return nil, fmt.Errorf("configure S3 span archive: %w", err)
		}
		objects = remote
	default:
		return nil, fmt.Errorf("unsupported span archive backend %q", cfg.ArchiveBackend)
	}
	options := store.SpanArchiveOptions{
		Backend: cfg.ArchiveBackend, AfterDays: cfg.ArchiveAfterDays,
		S3Prefix: cfg.ArchiveS3Prefix, Objects: objects,
	}
	if err := db.EnableSpanArchive(ctx, options, cfg.TransactionRetentionDays); err != nil {
		return nil, fmt.Errorf("initialize span archive before serving: %w", err)
	}
	if cfg.ArchiveBackend == "s3" {
		for batch := 0; batch < 20; batch++ {
			restored, err := db.RestoreEventArchiveS3(ctx, objects, 250)
			if err != nil {
				return nil, fmt.Errorf("restore S3 event archive before serving: %w", err)
			}
			if restored < 250 {
				break
			}
		}
	}
	return &options, nil
}
