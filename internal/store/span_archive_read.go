package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"goodsentry/internal/spanarchive"
)

const spanArchiveReadAttempts = 3

type spanArchiveSelector struct {
	OrganizationID     int64
	ProjectID          int64
	ProjectIDs         []int64
	RestrictProjectIDs bool
	TraceID            string
	SpanRowID          int64
	Release            string
	Environment        string
	Environments       []string
	RequireEnvironment bool
	TimeColumn         string
	Start              *time.Time
	End                *time.Time
	IsSegment          *bool
}

type spanArchiveRead struct {
	tx          pgx.Tx
	enabled     bool
	options     SpanArchiveOptions
	retention   int
	cutoff      time.Time
	verified    bool
	hotRelation string
}

func (r *spanArchiveRead) organizationID(ctx context.Context, userID, orgSlug string) (int64, error) {
	var id int64
	err := r.tx.QueryRow(ctx, `SELECT o.id FROM organizations o
		JOIN memberships m ON m.organization_id=o.id
		WHERE m.user_id::text=$1 AND o.slug=$2`, userID, orgSlug).Scan(&id)
	return id, dbError(err)
}

func (r *spanArchiveRead) managementProjectID(ctx context.Context, access ManagementAccess, reference string) (int64, error) {
	var id int64
	err := r.tx.QueryRow(ctx, `SELECT p.id FROM projects p
		JOIN memberships m ON m.organization_id=p.organization_id AND m.user_id=$1::bigint
		WHERE p.organization_id=$2::bigint AND ($3 OR p.id=ANY($4::bigint[]))
		AND (p.slug=$5 OR p.id=$6) ORDER BY (p.slug=$5) DESC LIMIT 1`,
		access.UserID, access.OrganizationID, access.AllProjects, access.ProjectIDs, reference, referenceID(reference)).Scan(&id)
	return id, dbError(err)
}

func (r *spanArchiveRead) loadAccessSpans(ctx context.Context, access ManagementAccess, selector spanArchiveSelector) error {
	if !r.enabled {
		return nil
	}
	selector.OrganizationID = referenceID(access.OrganizationID)
	if selector.OrganizationID <= 0 {
		return errors.New("span archive access requires an organization")
	}
	if !access.AllProjects {
		if selector.RestrictProjectIDs {
			permitted := make(map[int64]struct{}, len(access.ProjectIDs))
			for _, id := range access.ProjectIDs {
				permitted[id] = struct{}{}
			}
			intersection := make([]int64, 0, min(len(selector.ProjectIDs), len(access.ProjectIDs)))
			for _, id := range selector.ProjectIDs {
				if _, ok := permitted[id]; ok {
					intersection = append(intersection, id)
				}
			}
			selector.ProjectIDs = intersection
		} else {
			selector.ProjectIDs = access.ProjectIDs
		}
		selector.RestrictProjectIDs = true
	}
	var member bool
	if err := r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships
		WHERE organization_id=$1 AND user_id=$2::bigint)`, selector.OrganizationID, access.UserID).Scan(&member); err != nil {
		return fmt.Errorf("verify span archive organization access: %w", err)
	}
	if !member {
		selector.RestrictProjectIDs = true
		selector.ProjectIDs = nil
	}
	return r.Load(ctx, selector)
}

func (r *spanArchiveRead) windowBounds(ctx context.Context, window *TimeWindow, fallback time.Duration) (time.Time, time.Time, error) {
	if window != nil {
		return window.Start, window.End, nil
	}
	var start, end time.Time
	err := r.tx.QueryRow(ctx, `SELECT transaction_timestamp()-($1::bigint*interval '1 microsecond'),
		transaction_timestamp()+interval '1 microsecond'`, int64(fallback/time.Microsecond)).Scan(&start, &end)
	return start, end, err
}

func withSpanArchiveRead[T any](ctx context.Context, s *Store, read func(*spanArchiveRead) (T, error)) (T, error) {
	var zero T
	for attempt := range spanArchiveReadAttempts {
		reader, err := s.beginSpanArchiveRead(ctx)
		if err == nil {
			var result T
			result, err = read(reader)
			if err == nil {
				err = reader.commit(ctx)
			} else {
				_ = reader.tx.Rollback(ctx)
			}
			if err == nil {
				return result, nil
			}
		}
		if !isSpanArchiveSerializationFailure(err) || attempt+1 == spanArchiveReadAttempts || ctx.Err() != nil {
			return zero, err
		}
		if err := waitSpanArchiveRetry(ctx, time.Duration(attempt+1)*25*time.Millisecond); err != nil {
			return zero, err
		}
	}
	return zero, errors.New("span archive read retry limit reached")
}

func (s *Store) beginSpanArchiveRead(ctx context.Context) (*spanArchiveRead, error) {
	reader := &spanArchiveRead{enabled: s.spanArchive != nil}
	mode := pgx.ReadOnly
	if reader.enabled {
		mode = pgx.ReadWrite
		reader.options = *s.spanArchive
		reader.retention = s.spanArchiveRetentionDays
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: mode})
	if err != nil {
		return nil, err
	}
	reader.tx = tx
	if !reader.enabled {
		return reader, nil
	}
	var schema, name string
	if err := tx.QueryRow(ctx, `SELECT n.nspname,c.relname FROM pg_class c
	 JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=to_regclass('spans')`).Scan(&schema, &name); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("resolve hot spans relation from search_path: %w", err)
	}
	reader.hotRelation = pgx.Identifier{schema, name}.Sanitize()
	if err := tx.QueryRow(ctx, "SELECT transaction_timestamp()-($1::integer*interval '24 hours')", reader.retention).Scan(&reader.cutoff); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("resolve cold span retention cutoff: %w", err)
	}
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE pg_temp.archived_spans (LIKE "+reader.hotRelation+" INCLUDING DEFAULTS) ON COMMIT DROP"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("create temporary cold span relation: %w", err)
	}
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE pg_temp.span_archive_loaded (object_id bigint PRIMARY KEY) ON COMMIT DROP"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("create temporary archive object ledger: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE pg_temp.span_archive_trace_seeds (
	 project_id bigint NOT NULL, trace_id text NOT NULL, PRIMARY KEY(project_id,trace_id)) ON COMMIT DROP`); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("create temporary archive trace seeds: %w", err)
	}
	if _, err := tx.Exec(ctx, "CREATE TEMP VIEW pg_temp.spans AS SELECT * FROM "+reader.hotRelation+" UNION ALL SELECT * FROM pg_temp.archived_spans"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("create transaction-local spans view: %w", err)
	}
	return reader, nil
}

func (r *spanArchiveRead) commit(ctx context.Context) error {
	if r.enabled {
		if _, err := r.tx.Exec(ctx, "DROP VIEW pg_temp.spans"); err != nil {
			_ = r.tx.Rollback(ctx)
			return fmt.Errorf("drop transaction-local spans view: %w", err)
		}
		if _, err := r.tx.Exec(ctx, "DROP TABLE pg_temp.span_archive_trace_seeds, pg_temp.span_archive_loaded, pg_temp.archived_spans"); err != nil {
			_ = r.tx.Rollback(ctx)
			return fmt.Errorf("drop transaction-local span tables: %w", err)
		}
	}
	return r.tx.Commit(ctx)
}

func (r *spanArchiveRead) Load(ctx context.Context, selector spanArchiveSelector) error {
	if !r.enabled {
		return nil
	}
	where, args, err := r.selectorPredicate(selector)
	if err != nil {
		return err
	}
	return r.loadWhere(ctx, where, args)
}

// AddTraceSeeds accepts a fixed store-owned SELECT returning project_id and
// trace_id. The insert is scoped by the caller's existing authorization and
// seed predicates before unbounded trace children are loaded.
func (r *spanArchiveRead) AddTraceSeeds(ctx context.Context, selectSQL string, args ...any) error {
	if !r.enabled {
		return nil
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(selectSQL)), "SELECT") {
		return errors.New("archive trace seeds must come from a SELECT")
	}
	if _, err := r.tx.Exec(ctx, "INSERT INTO pg_temp.span_archive_trace_seeds(project_id,trace_id) "+selectSQL+" ON CONFLICT DO NOTHING", args...); err != nil {
		return fmt.Errorf("select cold span trace seeds: %w", err)
	}
	return nil
}

func (r *spanArchiveRead) LoadSeededTraces(ctx context.Context) error {
	if !r.enabled {
		return nil
	}
	where := `r.created_at >= transaction_timestamp()-($1::integer*interval '24 hours')
	 AND EXISTS (SELECT 1 FROM pg_temp.span_archive_trace_seeds seed
	  WHERE seed.project_id=r.project_id AND seed.trace_id=r.trace_id)`
	return r.loadWhere(ctx, where, []any{r.retention})
}

func (r *spanArchiveRead) selectorPredicate(selector spanArchiveSelector) (string, []any, error) {
	args := []any{r.retention}
	conditions := []string{"r.created_at>=transaction_timestamp()-($1::integer*interval '24 hours')"}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if selector.OrganizationID > 0 {
		add("r.project_id IN (SELECT p.id FROM projects p WHERE p.organization_id=$%d::bigint)", selector.OrganizationID)
	}
	if selector.ProjectID > 0 {
		add("r.project_id=$%d::bigint", selector.ProjectID)
	}
	if selector.RestrictProjectIDs {
		if len(selector.ProjectIDs) == 0 {
			conditions = append(conditions, "false")
		} else {
			add("r.project_id=ANY($%d::bigint[])", selector.ProjectIDs)
		}
	}
	if selector.TraceID != "" {
		add("r.trace_id=$%d", selector.TraceID)
	}
	if selector.SpanRowID > 0 {
		add("r.span_row_id=$%d::bigint", selector.SpanRowID)
	}
	if selector.Release != "" {
		add("r.release=$%d", selector.Release)
	}
	if selector.Environment != "" {
		add("r.environment=$%d", selector.Environment)
	}
	if len(selector.Environments) > 0 {
		add("r.environment=ANY($%d::text[])", selector.Environments)
	}
	if selector.RequireEnvironment {
		conditions = append(conditions, "r.environment<>''")
	}
	if selector.Start != nil || selector.End != nil {
		column := ""
		switch selector.TimeColumn {
		case "start_timestamp":
			column = "r.start_timestamp"
		case "end_timestamp":
			column = "r.end_timestamp"
		default:
			return "", nil, errors.New("archive time selector requires a supported timestamp column")
		}
		if selector.Start != nil {
			add(column+">=$%d", *selector.Start)
		}
		if selector.End != nil {
			add(column+"<$%d", *selector.End)
		}
	}
	if selector.IsSegment != nil {
		add("r.is_segment=$%d", *selector.IsSegment)
	}
	if selector.OrganizationID <= 0 && selector.ProjectID <= 0 && !selector.RestrictProjectIDs &&
		selector.TraceID == "" && selector.SpanRowID <= 0 {
		return "", nil, errors.New("archive selector must have a project, organization, trace, or row scope")
	}
	return strings.Join(conditions, " AND "), args, nil
}

func (r *spanArchiveRead) loadWhere(ctx context.Context, rowPredicate string, rowArgs []any) error {
	if err := r.verifyCatalog(ctx); err != nil {
		return err
	}
	lastID := int64(0)
	const batchSize = 50
	for {
		args := append(append([]any(nil), rowArgs...), r.options.Backend, lastID, batchSize)
		backendArg, lastIDArg, limitArg := len(rowArgs)+1, len(rowArgs)+2, len(rowArgs)+3
		query := fmt.Sprintf(`SELECT o.id,o.project_id,o.object_key,o.row_count,o.object_size_bytes
		 FROM span_archive_objects o
		 WHERE o.backend=$%d AND o.state='ready' AND o.index_complete AND o.id>$%d
		 AND EXISTS(SELECT 1 FROM span_archive_rows r WHERE r.archive_object_id=o.id AND %s)
		 AND NOT EXISTS(SELECT 1 FROM pg_temp.span_archive_loaded l WHERE l.object_id=o.id)
		 ORDER BY o.id LIMIT $%d FOR SHARE OF o`, backendArg, lastIDArg, rowPredicate, limitArg)
		rows, err := r.tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("select cold span archive objects: %w", err)
		}
		type manifest struct {
			id        int64
			projectID pgtype.Int8
			key       string
			rows      int32
			size      int64
		}
		batch, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (manifest, error) {
			var object manifest
			err := row.Scan(&object.id, &object.projectID, &object.key, &object.rows, &object.size)
			return object, err
		})
		if err != nil {
			return fmt.Errorf("read cold span archive manifests: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for _, object := range batch {
			lastID = object.id
			if !object.projectID.Valid {
				return fmt.Errorf("ready archive object %d has no project", object.id)
			}
			if err := r.loadObject(ctx, object.id, object.projectID.Int64, object.key, int(object.rows), object.size); err != nil {
				return err
			}
		}
	}
}

func (r *spanArchiveRead) verifyCatalog(ctx context.Context) error {
	if r.verified {
		return nil
	}
	var incomplete bool
	if err := r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM span_archive_objects
	 WHERE backend=$1 AND state='ready' AND NOT index_complete)`, r.options.Backend).Scan(&incomplete); err != nil {
		return fmt.Errorf("verify span archive catalog readiness: %w", err)
	}
	if incomplete {
		return errors.New("span archive catalog is incomplete; finish startup backfill before reading")
	}
	r.verified = true
	return nil
}

func (r *spanArchiveRead) loadObject(ctx context.Context, id, projectID int64, key string, rowCount int, objectSize int64) error {
	body, err := r.options.Objects.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read span archive object %d: %w", id, err)
	}
	if int64(len(body)) != objectSize {
		return fmt.Errorf("span archive object %d size does not match its manifest", id)
	}
	spans, err := spanarchive.Decode(body)
	if err != nil {
		return fmt.Errorf("decode span archive object %d: %w", id, err)
	}
	if len(spans) != rowCount {
		return fmt.Errorf("span archive object %d row count does not match its manifest", id)
	}
	columns := []string{"id", "project_id", "trace_id", "span_id", "parent_span_id", "name", "status", "is_segment",
		"start_timestamp", "end_timestamp", "duration", "op", "environment", "release", "attributes", "data", "created_at"}
	values := make([][]any, 0, len(spans))
	for _, span := range spans {
		if span.ProjectID != projectID {
			return fmt.Errorf("span archive object %d contains a row from another project", id)
		}
		created := time.UnixMicro(span.CreatedAt).UTC()
		if !created.Before(r.cutoff) {
			values = append(values, []any{span.ID, span.ProjectID, span.TraceID, span.SpanID, span.ParentSpanID, span.Name,
				span.Status, span.IsSegment, time.UnixMicro(span.StartTimestamp).UTC(), time.UnixMicro(span.EndTimestamp).UTC(),
				span.Duration, span.Op, span.Environment, span.Release, span.Attributes, span.Data, created})
		}
	}
	if len(values) > 0 {
		inserted, err := r.tx.CopyFrom(ctx, pgx.Identifier{"pg_temp", "archived_spans"}, columns, pgx.CopyFromRows(values))
		if err != nil {
			return fmt.Errorf("load rows from archive object %d: %w", id, err)
		}
		if inserted != int64(len(values)) {
			return fmt.Errorf("loaded %d rows from archive object %d; expected %d", inserted, id, len(values))
		}
	}
	if _, err := r.tx.Exec(ctx, "INSERT INTO pg_temp.span_archive_loaded(object_id) VALUES($1)", id); err != nil {
		return fmt.Errorf("record loaded span archive object %d: %w", id, err)
	}
	return nil
}

func isSpanArchiveSerializationFailure(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "40001"
}

func waitSpanArchiveRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
