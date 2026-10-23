//go:build integration

package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/config"
	"goodsentry/internal/spanarchive"
	"goodsentry/internal/store"
)

func TestConfigureSpanArchiveRejectsDisabledManifestAndBackfillsBeforeRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	baseURL, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil || baseURL.Scheme == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	admin, err := pgx.Connect(ctx, baseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	schema := fmt.Sprintf("goodsentry_startup_archive_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	query := baseURL.Query()
	query.Set("search_path", schema)
	baseURL.RawQuery = query.Encode()
	schemaDB, err := pgx.Connect(ctx, baseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = schemaDB.Close(ctx) })
	db, err := store.Open(ctx, baseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	user, org, err := db.Bootstrap(ctx, "startup@example.test", []byte("already-hashed"), "Startup Owner", "Startup")
	if err != nil {
		t.Fatal(err)
	}
	project, err := db.CreateProject(ctx, user.ID, org.Slug, "Archived project", "go")
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := strconv.ParseInt(project.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	archiveDir := t.TempDir()
	objects, err := spanarchive.NewLocalObjects(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Microsecond)
	span := spanarchive.Span{
		ID: 900001, ProjectID: projectID, TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SpanID: "0000000000000081",
		Name: "startup archive span", Status: "ok", IsSegment: true,
		StartTimestamp: created.UnixMicro(), EndTimestamp: created.Add(time.Second).UnixMicro(), Duration: 1000,
		Op: "http.server", Environment: "production", Release: "startup@1",
		Attributes: []byte(`{"sentry":{"segment":{"name":"startup archive span"}}}`), Data: []byte(`{"source":"startup"}`),
		CreatedAt: created.UnixMicro(),
	}
	body, err := spanarchive.Encode([]spanarchive.Span{span})
	if err != nil {
		t.Fatal(err)
	}
	key := "projects/" + project.ID + "/spans/startup.parquet"
	if err := objects.Put(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	if _, err := schemaDB.Exec(ctx, `INSERT INTO span_archive_objects(project_id,backend,object_key,row_count,object_size_bytes,
 min_created_at,max_created_at,min_start_timestamp,max_start_timestamp,min_end_timestamp,max_end_timestamp)
 VALUES($1,'local',$2,1,$3,$4,$4,$5,$5,$6,$6)`, projectID, key, len(body), created,
		created, created.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	common := config.Config{ArchiveBackend: "local", ArchiveDir: archiveDir, ArchiveAfterDays: 5, TransactionRetentionDays: 30}
	if _, err := configureSpanArchive(ctx, db, common); err == nil {
		t.Fatal("startup allowed disabling archival while a manifest still exists")
	}
	common.ArchiveEnabled = true
	options, err := configureSpanArchive(ctx, db, common)
	if err != nil || options == nil || options.Backend != "local" {
		t.Fatalf("configure local archive options=%+v err=%v", options, err)
	}
	var complete bool
	if err := schemaDB.QueryRow(ctx, `SELECT index_complete FROM span_archive_objects WHERE object_key=$1`, key).Scan(&complete); err != nil || !complete {
		t.Fatalf("startup backfill complete=%v err=%v", complete, err)
	}
	spans, err := db.Spans(ctx, user.ID, org.Slug, project.ID, span.TraceID)
	if err != nil || len(spans) != 1 || spans[0].SpanID != span.SpanID {
		t.Fatalf("archived span was queryable immediately after startup configuration: %+v err=%v", spans, err)
	}
}
