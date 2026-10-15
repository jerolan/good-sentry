// Package jobs runs durable work inside the application process. PostgreSQL
// leases coordinate replicas; workers never hold a row lock during network I/O.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"goodsentry/internal/store"
	"goodsentry/internal/uptime"
)

// StartMonitors starts the configured worker count and returns an idempotent
// stop-and-wait function. The caller must stop workers before closing db.
func StartMonitors(parent context.Context, db *store.Store, workers int, allowPrivate bool) func() {
	ctx, cancel := context.WithCancel(parent)
	probe := uptime.NewProber(allowPrivate)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runMonitors(ctx, db, probe)
		}()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
			probe.Close()
		})
	}
}

func runMonitors(ctx context.Context, db *store.Store, probe *uptime.Prober) {
	for ctx.Err() == nil {
		lease, err := db.ClaimMonitor(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !errors.Is(err, store.ErrNotFound) {
				slog.Error("Uptime work could not be claimed", "entryPoint", "monitor_worker", "operation", "claim", "error", err)
			}
			if !waitForWork(ctx) {
				return
			}
			continue
		}
		result := uptime.Result{Reason: "heartbeat_missed"}
		if lease.Type != uptime.Heartbeat {
			result, err = probe.Check(ctx, lease.Config)
			if err != nil {
				// Caller cancellation aborts work without declaring an outage. Its
				// lease will expire and another process can reclaim the monitor.
				if ctx.Err() != nil {
					return
				}
				slog.Error("Uptime probe was interrupted", "entryPoint", "monitor_worker", "operation", "probe", "monitorId", lease.ID)
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}
		if _, err = db.CompleteMonitor(ctx, lease, result); err != nil && !errors.Is(err, store.ErrConflict) && ctx.Err() == nil {
			slog.Error("Uptime result could not be stored", "entryPoint", "monitor_worker", "operation", "complete", "monitorId", lease.ID, "error", err)
		}
	}
}

func waitForWork(ctx context.Context) bool {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
