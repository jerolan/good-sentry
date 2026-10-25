package jobs

import (
	"context"
	"log/slog"
	"time"

	"goodsentry/internal/native"
	"goodsentry/internal/store"
)

// StartNativeReprocessing drains durable project-scoped event symbolication
// work on startup and after idle intervals. PostgreSQL coordinates replicas.
func StartNativeReprocessing(parent context.Context, db *store.Store, interval time.Duration) func() {
	var cachedProjectID string
	var cachedGeneration int64
	var cachedSymbols native.SymbolSet
	return startRetention(parent, interval, func(ctx context.Context) bool {
		processed, err := db.ProcessNextNativeReprocessingBatch(ctx, cachedProjectID, cachedGeneration, func(projectID string, generation int64, cached bool, stored []store.DebugSymbolFile, events []store.NativeReprocessingEvent) (map[int64][]byte, error) {
			if !cached {
				files := make([]native.DebugSymbolFile, 0, len(stored))
				for _, file := range stored {
					files = append(files, native.DebugSymbolFile{Name: file.Name, DebugID: file.DebugID, Content: file.Content})
				}
				cachedSymbols, _ = native.BuildSymbolTables(files)
				cachedProjectID, cachedGeneration = projectID, generation
			}
			updates := make(map[int64][]byte)
			for _, event := range events {
				updated, changed, err := native.ResymbolicateEvent(event.Data, cachedSymbols)
				if err != nil {
					return nil, err
				}
				if changed {
					updates[event.ID] = updated
				}
			}
			return updates, nil
		})
		if ctx.Err() != nil {
			return false
		}
		if err == nil && !processed {
			cachedProjectID, cachedGeneration, cachedSymbols = "", 0, nil
		}
		if err != nil {
			slog.Error("Native event reprocessing pass failed", "entryPoint", "native_reprocessing_worker", "operation", "process_batch")
			return false
		}
		return processed
	})
}
