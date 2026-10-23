package store

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

type spanArchiveIdentity struct {
	projectID int64
	traceID   string
	spanID    string
}

func lockSpanArchiveIdentities(ctx context.Context, tx pgx.Tx, identities []spanArchiveIdentity) error {
	if len(identities) == 0 {
		return nil
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].projectID != identities[j].projectID {
			return identities[i].projectID < identities[j].projectID
		}
		if identities[i].traceID != identities[j].traceID {
			return identities[i].traceID < identities[j].traceID
		}
		return identities[i].spanID < identities[j].spanID
	})
	batch := &pgx.Batch{}
	for i, identity := range identities {
		if i > 0 && identity == identities[i-1] {
			continue
		}
		lockKey := fmt.Sprintf("goodsentry:span-identity:%d:%s:%s", identity.projectID, identity.traceID, identity.spanID)
		batch.Queue(`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey)
	}
	results := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("lock span identity: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close span identity lock batch: %w", err)
	}
	return nil
}
