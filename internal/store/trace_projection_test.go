//go:build integration

package store

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/event"
)

// This database boundary owns the version-one upgrade. Existing projection
// tests own fresh ingestion/duplicate accounting; trace-reader tests own graph
// semantics. A real advisory wait lets readers observe the committed old
// statistics while replacement metadata is still uncommitted, then cancel it.
func TestTraceProjectionUpgradePreservesStatisticsDuringCancellation(t *testing.T) {
	parent, db, user, org, project := telemetryStore(t)
	ctx, stop := context.WithTimeout(parent, 30*time.Second)
	defer stop()
	raw := []byte(`{"event_id":"abcdef0123456789abcdef0123456789","type":"transaction","transaction":"Projection upgrade","start_timestamp":"2026-10-14T18:00:00.123456999-06:00","timestamp":"2026-10-15T00:00:02Z","contexts":{"trace":{"trace_id":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","span_id":"BBBBBBBBBBBBBBBB","parent_span_id":"cccccccccccccccc","op":"http.server","status":"ok"}},"spans":[{"span_id":"DDDDDDDDDDDDDDDD","parent_span_id":"BBBBBBBBBBBBBBBB","op":"db.query","status":"ok","description":"Read cart","start_timestamp":"2026-10-14T18:00:00,2500009-06:00","timestamp":"2026-10-14T18:00:00.7500009-06:00"},null]}`)
	ev, err := event.Normalize(raw, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id := detailEventID(t, ctx, db, project.ID, ev)
	var initialRaw []byte
	assertMetadata := func() {
		t.Helper()
		var version int
		var span, parent, operation, status, childSpan, childParent, childStatus string
		var start, childStart, childEnd time.Time
		var duration float64
		var stored []byte
		err := db.pool.QueryRow(ctx, `SELECT e.span_projection_version,e.trace_span_id,e.trace_parent_span_id,
			e.trace_op,e.trace_status,e.trace_start_timestamp,c.span_id,c.parent_span_id,c.status,
			c.start_timestamp,c.end_timestamp,c.duration,e.data FROM events e
			JOIN transaction_span_operations c ON c.event_id=e.id AND c.ordinal=1 WHERE e.id::text=$1`, id).Scan(
			&version, &span, &parent, &operation, &status, &start, &childSpan, &childParent, &childStatus, &childStart, &childEnd, &duration, &stored)
		if err != nil {
			t.Fatal(err)
		}
		if version != 2 || span != strings.Repeat("B", 16) || parent != strings.Repeat("c", 16) || operation != "http.server" || status != "ok" || childSpan != strings.Repeat("D", 16) || childParent != span || childStatus != "ok" || duration != 500 || start.UTC().Format(time.RFC3339Nano) != "2026-10-15T00:00:00.123456Z" || childStart.UTC().Format(time.RFC3339Nano) != "2026-10-15T00:00:00.25Z" || childEnd.UTC().Format(time.RFC3339Nano) != "2026-10-15T00:00:00.75Z" {
			t.Fatalf("version-two projection lost exact IDs/timing: version=%d root=%s parent=%s op=%s status=%s start=%s child=%s/%s status=%s interval=%s/%s duration=%v", version, span, parent, operation, status, start, childSpan, childParent, childStatus, childStart, childEnd, duration)
		}
		if initialRaw == nil {
			initialRaw = stored
		} else if !bytes.Equal(initialRaw, stored) {
			t.Fatal("metadata upgrade changed the original event JSON")
		}
	}
	assertMetadata()
	// Arrange the state produced by migration008 over a version-one event.
	if _, err := db.pool.Exec(ctx, `UPDATE events SET span_projection_version=1,trace_span_id='',trace_parent_span_id='',trace_op='',trace_status='',trace_start_timestamp=NULL;
		UPDATE transaction_span_operations SET span_id='',parent_span_id='',status='',start_timestamp=NULL,end_timestamp=NULL`); err != nil {
		t.Fatal(err)
	}
	window := &TimeWindow{Start: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)}
	before, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", id, "", window, 1)
	if err != nil || before.SpanCount != 2 || before.TimedSpanCount != 1 {
		t.Fatalf("version-one statistics unavailable before upgrade: %+v, %v", before, err)
	}
	control, err := pgx.Connect(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	if _, err := control.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended(current_schema(),8401))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION hold_trace_projection() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(TG_TABLE_SCHEMA,8401)); RETURN NEW; END $$;
		CREATE TRIGGER hold_trace_projection BEFORE UPDATE ON events FOR EACH ROW
		WHEN (OLD.span_projection_version=1 AND NEW.span_projection_version=2) EXECUTE FUNCTION hold_trace_projection()`); err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		processed, err := db.ProjectTransactionSpans(work)
		if processed {
			done <- errors.New("blocked metadata was reported as committed")
			return
		}
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted
			AND classid::bigint=((hashtextextended(current_schema(),8401)>>32)&4294967295)
			AND objid::bigint=(hashtextextended(current_schema(),8401)&4294967295))`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("upgrade never reached the held metadata update")
		}
		select {
		case err := <-done:
			t.Fatalf("upgrade completed before reaching its blocked metadata update: %v", err)
		case <-time.After(5 * time.Millisecond):
		}
	}
	during, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", id, "", window, 1)
	if err != nil || !reflect.DeepEqual(before, during) {
		t.Fatalf("uncommitted replacement hid/changed version-one statistics: %+v, %v", during, err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled upgrade did not roll back: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade ignored cancellation")
	}
	var version int
	var rows int
	if err := db.pool.QueryRow(ctx, `SELECT span_projection_version,(SELECT count(*) FROM transaction_span_operations WHERE event_id=events.id) FROM events WHERE id::text=$1`, id).Scan(&version, &rows); err != nil || version != 1 || rows != 2 {
		t.Fatalf("canceled upgrade lost committed version-one data: version=%d rows=%d, %v", version, rows, err)
	}
	if _, err := control.Exec(ctx, "SELECT pg_advisory_unlock(hashtextextended(current_schema(),8401))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "DROP TRIGGER hold_trace_projection ON events"); err != nil {
		t.Fatal(err)
	}
	databaseURL := db.pool.Config().ConnString()
	db.Close()
	db, err = Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if processed, err := db.ProjectTransactionSpans(ctx); err != nil || !processed {
		t.Fatalf("reopen failed to resume version-one upgrade: %v, %v", processed, err)
	}
	assertMetadata()
	after, err := db.TransactionSpanGroups(ctx, user.ID, org.Slug, "event", id, "", window, 1)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("completed upgrade changed operation statistics: %+v, %v", after, err)
	}
}
