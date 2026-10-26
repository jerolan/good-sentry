package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"goodsentry/internal/store"
)

// StartArtifactChunks reclaims expired upload staging. PostgreSQL row locks
// divide each bounded batch between replicas; completed maps are unaffected.
func StartArtifactChunks(parent context.Context, db *store.Store, ttl time.Duration) func() {
	ctx, cancel := context.WithCancel(parent)
	interval := artifactCleanupInterval(ttl)
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for ctx.Err() == nil {
			removed, err := db.CleanupArtifactChunks(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				slog.Error("Expired artifact chunks could not be removed", "entryPoint", "artifact_chunk_cleanup", "operation", "delete", "error", err)
			}
			if err == nil && removed > 0 {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	var once sync.Once
	return func() { once.Do(func() { cancel(); wg.Wait() }) }
}

// artifactCleanupInterval clamps the TTL-derived sweep interval so replicas
// converge without sweeping storage continuously.
func artifactCleanupInterval(ttl time.Duration) time.Duration {
	return min(time.Hour, max(time.Minute, ttl/24))
}
