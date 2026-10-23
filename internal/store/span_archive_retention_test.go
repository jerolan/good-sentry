//go:build integration

package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goodsentry/internal/spanarchive"
)

func TestSpanArchivePreservesReceiptLifetimeAndDeletedProjectCleanup(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	projectID, err := strconv.ParseInt(project.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	exception := groupEvent(t, 91, "archive-independent", "Preserved error", time.Now())
	if _, _, err := db.Ingest(ctx, project.ID, exception); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IngestLogs(ctx, project.ID, []byte(`{"items":[{"timestamp":1,"trace_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","level":"info","body":"independent log","attributes":{}}]}`)); err != nil {
		t.Fatal(err)
	}
	archiveRows := []map[string]any{
		spanFixture("0000000000000001", true, 1_700_000_000, 1_700_000_000.01),
		spanFixture("0000000000000002", false, 1_700_000_001, 1_700_000_001.02),
		spanFixture("0000000000000003", false, 1_700_000_002, 1_700_000_002.03),
		spanFixture("0000000000000004", false, 1_700_000_003, 1_700_000_003.04),
	}
	if count, err := db.IngestSpans(ctx, project.ID, spanBatch(t, archiveRows...), len(archiveRows)); err != nil || count != len(archiveRows) {
		t.Fatalf("insert spans=%d err=%v", count, err)
	}
	ages := map[string]int{"0000000000000001": 15, "0000000000000002": 10, "0000000000000003": 2, "0000000000000004": 31}
	for spanID, days := range ages {
		if _, err := db.pool.Exec(ctx, "UPDATE spans SET created_at=now()-($1::integer*interval '24 hours') WHERE span_id=$2", days, spanID); err != nil {
			t.Fatal(err)
		}
	}
	var expiredID string
	if err := db.pool.QueryRow(ctx, `SELECT id::text FROM spans WHERE span_id='0000000000000004'`).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	archivedIDs := spanIDs(t, db, "0000000000000001", "0000000000000002")
	objects, err := spanarchive.NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	transactions, moved, removedObjects, err := db.CleanupTransactionsWithArchive(ctx, 30, SpanArchiveOptions{
		Backend: "local", AfterDays: 5, Objects: objects,
	})
	if err != nil || transactions != 0 || moved != 3 || removedObjects != 0 {
		t.Fatalf("archive pass transactions/spans/objects=%d/%d/%d err=%v", transactions, moved, removedObjects, err)
	}
	var hot, manifests, rowCount int
	var projectRef int64
	var key string
	var objectSize int64
	var indexComplete bool
	var catalogRows int
	var minCreated, maxCreated time.Time
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM spans),(SELECT count(*) FROM span_archive_objects),
 (SELECT row_count FROM span_archive_objects),(SELECT project_id FROM span_archive_objects),
 (SELECT object_key FROM span_archive_objects),(SELECT object_size_bytes FROM span_archive_objects),
	 (SELECT min_created_at FROM span_archive_objects),(SELECT max_created_at FROM span_archive_objects),
	 (SELECT index_complete FROM span_archive_objects),(SELECT count(*) FROM span_archive_rows)`).Scan(
		&hot, &manifests, &rowCount, &projectRef, &key, &objectSize, &minCreated, &maxCreated, &indexComplete, &catalogRows); err != nil {
		t.Fatal(err)
	}
	if hot != 1 || manifests != 1 || rowCount != 2 || projectRef != projectID || objectSize <= 0 || !indexComplete || catalogRows != rowCount {
		t.Fatalf("hot/manifests/rows/project/size/index/catalog=%d/%d/%d/%v/%d/%v/%d", hot, manifests, rowCount, projectRef, objectSize, indexComplete, catalogRows)
	}
	var expiredPresent int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM spans WHERE id::text=$1", expiredID).Scan(&expiredPresent); err != nil || expiredPresent != 0 {
		t.Fatalf("expired span count=%d err=%v", expiredPresent, err)
	}
	if time.Since(minCreated) < 14*24*time.Hour || time.Since(maxCreated) < 9*24*time.Hour || time.Since(maxCreated) > 11*24*time.Hour {
		t.Fatalf("manifest receipt bounds=%v..%v", minCreated, maxCreated)
	}
	var errorsAfterArchive, logsAfterArchive int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM events WHERE kind='error'),
	 (SELECT count(*) FROM logs)`).Scan(&errorsAfterArchive, &logsAfterArchive); err != nil || errorsAfterArchive != 1 || logsAfterArchive != 1 {
		t.Fatalf("independent errors/logs=%d/%d err=%v", errorsAfterArchive, logsAfterArchive, err)
	}
	wantKey, err := spanarchive.CohortKey(projectID, archivedIDs)
	if err != nil || key != wantKey {
		t.Fatalf("manifest key=%q want=%q err=%v", key, wantKey, err)
	}
	body, err := objects.Get(ctx, key)
	if err != nil || int64(len(body)) != objectSize {
		t.Fatalf("object size=%d manifest=%d err=%v", len(body), objectSize, err)
	}
	decoded, err := spanarchive.Decode(body)
	if err != nil || len(decoded) != 2 || decoded[0].ProjectID != projectID || decoded[0].TraceID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		!bytes.Contains(decoded[0].Data, []byte(`"future"`)) || !bytes.Contains(decoded[0].Data, []byte(`"kept"`)) ||
		!bytes.Contains(decoded[0].Attributes, []byte(`"attempt"`)) {
		t.Fatalf("decoded archive rows=%+v err=%v", decoded, err)
	}
	_, moved, removedObjects, err = db.CleanupTransactionsWithArchive(ctx, 12, SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects})
	if err != nil || moved != 0 || removedObjects != 0 {
		t.Fatalf("mixed-age expiry moved spans/objects=%d/%d err=%v", moved, removedObjects, err)
	}
	var partialRows, partialManifests int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM span_archive_rows),(SELECT count(*) FROM span_archive_objects)`).Scan(&partialRows, &partialManifests); err != nil {
		t.Fatal(err)
	}
	if partialRows != 1 || partialManifests != 1 {
		t.Fatalf("mixed-age catalog/manifests=%d/%d; want one live row in one object", partialRows, partialManifests)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE span_archive_objects SET min_created_at=now()-interval '32 days',
	 max_created_at=now()-interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	_, _, removedObjects, err = db.CleanupTransactionsWithArchive(ctx, 30, SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects})
	if err != nil || removedObjects != 1 {
		t.Fatalf("total receipt expiry removed=%d err=%v", removedObjects, err)
	}
	if _, err := objects.Get(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired object remained readable: %v", err)
	}
	if count, err := db.IngestSpans(ctx, project.ID, spanBatch(t, spanFixture("0000000000000005", false, 1_700_000_004, 1_700_000_005)), 1); err != nil || count != 1 {
		t.Fatalf("insert project-cleanup span=%d err=%v", count, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE spans SET created_at=now()-interval '10 days' WHERE span_id='0000000000000005'"); err != nil {
		t.Fatal(err)
	}
	_, moved, _, err = db.CleanupTransactionsWithArchive(ctx, 30, SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects})
	if err != nil || moved != 1 {
		t.Fatalf("project cleanup archive moved=%d err=%v", moved, err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT object_key FROM span_archive_objects").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "DELETE FROM projects WHERE id=$1", projectID); err != nil {
		t.Fatal(err)
	}
	var orphaned bool
	if err := db.pool.QueryRow(ctx, "SELECT project_id IS NULL FROM span_archive_objects").Scan(&orphaned); err != nil || !orphaned {
		t.Fatalf("project deletion did not orphan the manifest: %v %v", orphaned, err)
	}
	_, _, removedObjects, err = db.CleanupTransactionsWithArchive(ctx, 30, SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects})
	if err != nil || removedObjects != 1 {
		t.Fatalf("orphan cleanup objects=%d err=%v", removedObjects, err)
	}
	if _, err := objects.Get(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan object remained readable: %v", err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM span_archive_objects").Scan(&manifests); err != nil || manifests != 0 {
		t.Fatalf("manifest cleanup count=%d err=%v", manifests, err)
	}
}

func TestSpanArchiveFailuresRollbackAndRetryTheSameObject(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	projectID, err := strconv.ParseInt(project.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := db.IngestSpans(ctx, project.ID, spanBatch(t, spanFixture("0000000000000011", true, 1_700_000_000, 1_700_000_001)), 1); err != nil || count != 1 {
		t.Fatalf("insert span=%d err=%v", count, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE spans SET created_at=now()-interval '10 days'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO events(project_id,event_id,kind,title,level,timestamp,data,created_at)
	VALUES($1,'archive-rollback','transaction','rollback','info',now(),'{}',now()-interval '40 days')`, projectID); err != nil {
		t.Fatal(err)
	}
	rowIDs := spanIDs(t, db, "0000000000000011")
	objects, err := spanarchive.NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options := SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: failSpanArchivePut{Objects: objects}}
	if transactions, moved, manifests, err := db.CleanupTransactionsWithArchive(ctx, 30, options); err == nil || transactions != 0 || moved != 0 || manifests != 0 {
		t.Fatalf("failed put acknowledged=%d/%d/%d err=%v", transactions, moved, manifests, err)
	}
	assertSpanArchiveState(t, ctx, db, rowIDs[0], true, 0)
	options.Objects = objects
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION fail_span_archive_manifest() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN RAISE EXCEPTION 'synthetic manifest failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `CREATE TRIGGER fail_span_archive_manifest BEFORE INSERT ON span_archive_objects
	FOR EACH ROW EXECUTE FUNCTION fail_span_archive_manifest()`); err != nil {
		t.Fatal(err)
	}
	if transactions, moved, manifests, err := db.CleanupTransactionsWithArchive(ctx, 30, options); err == nil || transactions != 0 || moved != 0 || manifests != 0 {
		t.Fatalf("failed manifest acknowledged=%d/%d/%d err=%v", transactions, moved, manifests, err)
	}
	assertSpanArchiveState(t, ctx, db, rowIDs[0], true, 0)
	key, err := spanarchive.CohortKey(projectID, rowIDs)
	if err != nil {
		t.Fatal(err)
	}
	firstUpload, err := objects.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "DROP TRIGGER fail_span_archive_manifest ON span_archive_objects"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "DROP FUNCTION fail_span_archive_manifest()"); err != nil {
		t.Fatal(err)
	}
	transactions, moved, manifests, err := db.CleanupTransactionsWithArchive(ctx, 30, options)
	if err != nil || transactions != 1 || moved != 1 || manifests != 0 {
		t.Fatalf("retry result=%d/%d/%d err=%v", transactions, moved, manifests, err)
	}
	secondUpload, err := objects.Get(ctx, key)
	if err != nil || !bytes.Equal(firstUpload, secondUpload) {
		t.Fatalf("deterministic retry changed object bytes: err=%v", err)
	}
	assertSpanArchiveState(t, ctx, db, rowIDs[0], false, 1)
}

func TestIngestSpansPreservesAStillRetainedArchivedIdentity(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	objects, err := spanarchive.NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options := SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects}
	if err := db.EnableSpanArchive(ctx, options, 30); err != nil {
		t.Fatal(err)
	}
	payload := spanBatch(t, spanFixture("0000000000000021", true, 1_700_000_000, 1_700_000_001))
	if count, err := db.IngestSpans(ctx, project.ID, payload, 1); err != nil || count != 1 {
		t.Fatalf("initial span ingest=%d err=%v", count, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE spans SET created_at=now()-interval '10 days' WHERE span_id='0000000000000021'"); err != nil {
		t.Fatal(err)
	}
	_, moved, _, err := db.CleanupTransactionsWithArchive(ctx, 30, options)
	if err != nil || moved != 1 {
		t.Fatalf("archive eligible span moved=%d err=%v", moved, err)
	}
	if count, err := db.IngestSpans(ctx, project.ID, payload, 1); err != nil || count != 0 {
		t.Fatalf("duplicate cold identity ingest=%d err=%v", count, err)
	}
	var hot, cold int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM spans WHERE span_id='0000000000000021'),
	 (SELECT count(*) FROM span_archive_rows WHERE span_id='0000000000000021')`).Scan(&hot, &cold); err != nil {
		t.Fatal(err)
	}
	if hot != 0 || cold != 1 {
		t.Fatalf("duplicate created hot/cold rows=%d/%d", hot, cold)
	}
}

func TestSpanArchiveCleanupRecoversAfterObjectOrManifestDeleteFailure(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	objects, err := spanarchive.NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options := SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects}
	if count, err := db.IngestSpans(ctx, project.ID, spanBatch(t, spanFixture("0000000000000031", true, 1_700_000_000, 1_700_000_001)), 1); err != nil || count != 1 {
		t.Fatalf("insert span=%d err=%v", count, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE spans SET created_at=now()-interval '10 days' WHERE span_id='0000000000000031'"); err != nil {
		t.Fatal(err)
	}
	_, moved, _, err := db.CleanupTransactionsWithArchive(ctx, 30, options)
	if err != nil || moved != 1 {
		t.Fatalf("archive eligible span moved=%d err=%v", moved, err)
	}
	var key string
	if err := db.pool.QueryRow(ctx, "SELECT object_key FROM span_archive_objects").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE span_archive_objects SET min_created_at=now()-interval '32 days',
	 max_created_at=now()-interval '31 days';
	 UPDATE span_archive_rows SET created_at=now()-interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	failingDelete := failDeleteSpanArchive{Objects: objects}
	if _, _, _, err := db.CleanupTransactionsWithArchive(ctx, 30, SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: failingDelete}); err == nil {
		t.Fatal("cleanup acknowledged a failed external object delete")
	}
	var state string
	if err := db.pool.QueryRow(ctx, "SELECT state FROM span_archive_objects WHERE object_key=$1", key).Scan(&state); err != nil || state != "deleting" {
		t.Fatalf("external delete failure state=%q err=%v", state, err)
	}
	if _, err := objects.Get(ctx, key); err != nil {
		t.Fatalf("failed external delete removed the object: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION fail_span_archive_manifest_delete() RETURNS trigger LANGUAGE plpgsql AS $$
	 BEGIN RAISE EXCEPTION 'synthetic manifest delete failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `CREATE TRIGGER fail_span_archive_manifest_delete BEFORE DELETE ON span_archive_objects
	 FOR EACH ROW EXECUTE FUNCTION fail_span_archive_manifest_delete()`); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := db.CleanupTransactionsWithArchive(ctx, 30, options); err == nil {
		t.Fatal("cleanup acknowledged a failed manifest delete")
	}
	if _, err := objects.Get(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("object delete should precede the failed manifest commit: %v", err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT state FROM span_archive_objects WHERE object_key=$1", key).Scan(&state); err != nil || state != "deleting" {
		t.Fatalf("manifest failure lost the durable deleting state=%q err=%v", state, err)
	}
	if _, err := db.pool.Exec(ctx, `DROP TRIGGER fail_span_archive_manifest_delete ON span_archive_objects;
	 DROP FUNCTION fail_span_archive_manifest_delete()`); err != nil {
		t.Fatal(err)
	}
	_, _, removed, err := db.CleanupTransactionsWithArchive(ctx, 30, options)
	if err != nil || removed != 1 {
		t.Fatalf("retry cleanup removed=%d err=%v", removed, err)
	}
	var manifests, rows int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM span_archive_objects),(SELECT count(*) FROM span_archive_rows)`).Scan(&manifests, &rows); err != nil {
		t.Fatal(err)
	}
	if manifests != 0 || rows != 0 {
		t.Fatalf("retry left deleting rows manifests/catalog=%d/%d", manifests, rows)
	}
}

func TestSpanArchiveSkipsLockedRowsCancelsAndSerializesMaintainers(t *testing.T) {
	ctx, db, _, _, project := telemetryStore(t)
	ids := []string{"0000000000000021", "0000000000000022"}
	items := []map[string]any{
		spanFixture(ids[0], true, 1_700_000_000, 1_700_000_001),
		spanFixture(ids[1], false, 1_700_000_002, 1_700_000_003),
	}
	if count, err := db.IngestSpans(ctx, project.ID, spanBatch(t, items...), len(items)); err != nil || count != len(items) {
		t.Fatalf("insert spans=%d err=%v", count, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE spans SET created_at=CASE span_id
 WHEN $1 THEN now()-interval '12 days' ELSE now()-interval '10 days' END`, ids[0]); err != nil {
		t.Fatal(err)
	}
	lockedID := spanIDs(t, db, ids[0])[0]
	blocker, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, "SELECT id FROM spans WHERE id=$1 FOR UPDATE", lockedID); err != nil {
		t.Fatal(err)
	}
	objects, err := spanarchive.NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options := SpanArchiveOptions{Backend: "local", AfterDays: 5, Objects: objects}
	_, moved, _, err := db.CleanupTransactionsWithArchive(ctx, 30, options)
	if err != nil || moved != 1 {
		t.Fatalf("locked cohort pass moved=%d err=%v", moved, err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	remaining := spanIDs(t, db, ids[0])
	if len(remaining) != 1 || remaining[0] != lockedID {
		t.Fatalf("locked source row changed: %v", remaining)
	}
	blockingObjects := &blockingSpanArchivePut{started: make(chan struct{})}
	options.Objects = blockingObjects
	canceled, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		_, _, _, err := db.CleanupTransactionsWithArchive(canceled, 30, options)
		result <- err
	}()
	<-blockingObjects.started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled archive error=%v", err)
	}
	if got := spanIDs(t, db, ids[0]); !reflect.DeepEqual(got, []int64{lockedID}) {
		t.Fatalf("canceled source rows=%v", got)
	}
	other, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	sharedObjects, err := spanarchive.NewLocalObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options.Objects = sharedObjects
	start := make(chan struct{})
	type passResult struct {
		moved int64
		err   error
	}
	results := make(chan passResult, 2)
	var wg sync.WaitGroup
	for _, cleaner := range []*Store{db, other} {
		wg.Add(1)
		go func(cleaner *Store) {
			defer wg.Done()
			<-start
			_, moved, _, err := cleaner.CleanupTransactionsWithArchive(ctx, 30, options)
			results <- passResult{moved: moved, err: err}
		}(cleaner)
	}
	close(start)
	wg.Wait()
	close(results)
	var totalMoved int64
	for pass := range results {
		if pass.err != nil {
			t.Fatal(pass.err)
		}
		totalMoved += pass.moved
	}
	if totalMoved != 1 || len(spanIDs(t, db, ids[0])) != 0 {
		t.Fatalf("overlapping maintainers moved=%d remaining=%v", totalMoved, spanIDs(t, db, ids[0]))
	}
}

type failSpanArchivePut struct {
	spanarchive.Objects
}

func (failSpanArchivePut) Put(context.Context, string, []byte) error {
	return errors.New("synthetic object upload failure")
}

type failDeleteSpanArchive struct {
	spanarchive.Objects
}

func (failDeleteSpanArchive) Delete(context.Context, string) error {
	return errors.New("synthetic object delete failure")
}

type blockingSpanArchivePut struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingSpanArchivePut) Put(ctx context.Context, _ string, _ []byte) error {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return ctx.Err()
}

func (b *blockingSpanArchivePut) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("unexpected get")
}
func (b *blockingSpanArchivePut) Delete(context.Context, string) error {
	return errors.New("unexpected delete")
}

func assertSpanArchiveState(t *testing.T, ctx context.Context, db *Store, spanID int64, wantHot bool, wantManifests int) {
	t.Helper()
	var hot, transactions, manifests int
	if err := db.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM spans WHERE id=$1),
 (SELECT count(*) FROM events WHERE event_id='archive-rollback'),
 (SELECT count(*) FROM span_archive_objects)`, spanID).Scan(&hot, &transactions, &manifests); err != nil {
		t.Fatal(err)
	}
	want := 0
	if wantHot {
		want = 1
	}
	if hot != want || transactions != want || manifests != wantManifests {
		t.Fatalf("rollback hot/transaction/manifest=%d/%d/%d want hot=%d manifest=%d", hot, transactions, manifests, want, wantManifests)
	}
}

func spanIDs(t *testing.T, db *Store, values ...string) []int64 {
	t.Helper()
	rows, err := db.pool.Query(context.Background(), "SELECT id FROM spans WHERE span_id=ANY($1::text[]) ORDER BY id", values)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatal(err)
	}
	return ids
}
