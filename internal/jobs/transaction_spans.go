package jobs

import (
	"context"
	"log/slog"
	"sync"

	"goodsentry/internal/store"
)

// StartTransactionSpans projects historical transaction children inside the
// application. Row locks coordinate replicas and each event commits atomically.
func StartTransactionSpans(parent context.Context, db *store.Store) func() {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	wg.Go(func() {
		for ctx.Err() == nil {
			processed, err := db.ProjectTransactionSpans(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				slog.Error("Historical transaction spans could not be indexed", "entryPoint", "transaction_span_worker", "operation", "project", "error", err)
			}
			if (err != nil || !processed) && !waitForWork(ctx) {
				return
			}
		}
	})
	var once sync.Once
	return func() { once.Do(func() { cancel(); wg.Wait() }) }
}
