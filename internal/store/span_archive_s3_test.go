//go:build integration

package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"goodsentry/internal/spanarchive"
)

func TestSpanArchiveS3QueryRestartAndExpiry(t *testing.T) {
	ctx, db, owner, org, project := telemetryStore(t)
	rows := []map[string]any{
		spanFixture("0000000000000011", true, 1_700_000_000, 1_700_000_000.1),
		spanFixture("0000000000000012", false, 1_700_000_001, 1_700_000_001.1),
	}
	if count, err := db.IngestSpans(ctx, project.ID, spanBatch(t, rows...), len(rows)); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE spans SET created_at=now()-(CASE WHEN span_id='0000000000000011' THEN 10 ELSE 1 END)*interval '24 hours' WHERE span_id IN ('0000000000000011','0000000000000012')`); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	objectsByPath := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			objectsByPath[r.URL.Path] = data
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, ok := objectsByPath[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		case http.MethodDelete:
			delete(objectsByPath, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	objects, err := spanarchive.NewS3Objects(ctx, "us-east-1", "archive-bucket", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnableSpanArchive(ctx, SpanArchiveOptions{Backend: "s3", AfterDays: 5, Objects: objects}, 30); err != nil {
		t.Fatal(err)
	}
	_, moved, _, err := db.CleanupTransactionsWithArchive(ctx, 30, SpanArchiveOptions{Backend: "s3", AfterDays: 5, Objects: objects})
	if err != nil || moved != 1 {
		t.Fatalf("moved=%d err=%v", moved, err)
	}
	mu.Lock()
	if len(objectsByPath) != 1 {
		t.Fatalf("S3 objects=%d", len(objectsByPath))
	}
	mu.Unlock()
	assertSpansS3(t, ctx, db, owner.ID, org.Slug, project.ID, 2)
	reopened, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.EnableSpanArchive(ctx, SpanArchiveOptions{Backend: "s3", AfterDays: 5, Objects: objects}, 30); err != nil {
		t.Fatal(err)
	}
	assertSpansS3(t, ctx, reopened, owner.ID, org.Slug, project.ID, 2)
	if _, _, removed, err := reopened.CleanupTransactionsWithArchive(ctx, 5, SpanArchiveOptions{Backend: "s3", AfterDays: 5, Objects: objects}); err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	mu.Lock()
	if len(objectsByPath) != 0 {
		t.Fatalf("S3 objects after expiry=%d", len(objectsByPath))
	}
	mu.Unlock()
	assertSpansS3(t, ctx, reopened, owner.ID, org.Slug, project.ID, 1)
}

func assertSpansS3(t *testing.T, ctx context.Context, db *Store, userID, orgSlug, projectID string, want int) {
	t.Helper()
	count, err := withSpanArchiveRead(ctx, db, func(read *spanArchiveRead) (int, error) {
		var orgID int64
		if err := read.tx.QueryRow(ctx, `SELECT o.id FROM organizations o JOIN memberships m ON m.organization_id=o.id WHERE m.user_id::text=$1 AND o.slug=$2`, userID, orgSlug).Scan(&orgID); err != nil {
			return 0, err
		}
		if err := read.Load(ctx, spanArchiveSelector{OrganizationID: orgID, TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err != nil {
			return 0, err
		}
		var n int
		err := read.tx.QueryRow(ctx, `SELECT count(*) FROM pg_temp.spans`).Scan(&n)
		return n, err
	})
	if err != nil || count != want {
		t.Fatalf("spans=%d want=%d err=%v", count, want, err)
	}
}
