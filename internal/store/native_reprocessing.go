package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

const nativeReprocessingBatchSize = 10

type NativeReprocessingEvent struct {
	ID   int64
	Data json.RawMessage
}

type NativeReprocessingTransform func(projectID string, generation int64, cached bool, files []DebugSymbolFile, events []NativeReprocessingEvent) (map[int64][]byte, error)

// EnqueueNativeReprocessing snapshots the current project event range and
// resets its durable cursor. Repeated requests coalesce into one project row.
func (s *Store) EnqueueNativeReprocessing(ctx context.Context, access ManagementAccess, project string) error {
	tx, role, err := s.releaseTransaction(ctx, access, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := artifactAccess(ctx, tx, access, role); err != nil {
		return err
	}
	projectIDs, err := releaseProjectReferences(ctx, tx, access, []string{project})
	if err != nil {
		return err
	}
	if len(projectIDs) != 1 {
		return ErrNotFound
	}
	projectID := projectIDs[0]
	var through int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM events
		WHERE project_id=$1 AND kind='error' AND data->>'platform'='native'`, projectID).Scan(&through); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_native_reprocessing(project_id,cursor_event_id,through_event_id)
		VALUES($1,0,$2) ON CONFLICT(project_id) DO UPDATE SET
		cursor_event_id=0,through_event_id=EXCLUDED.through_event_id,
		generation=EXCLUDED.generation,requested_at=clock_timestamp()`, projectID, through); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ProcessNextNativeReprocessingBatch locks one project queue row and commits
// event updates with cursor progress so failed work is retried after restart.
func (s *Store) ProcessNextNativeReprocessingBatch(ctx context.Context, cachedProjectID string, cachedGeneration int64, transform NativeReprocessingTransform) (bool, error) {
	if transform == nil {
		return false, errors.New("native reprocessing transform is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var projectID string
	var cursor, through, generation int64
	err = tx.QueryRow(ctx, `SELECT project_id::text,cursor_event_id,through_event_id,generation
		FROM project_native_reprocessing ORDER BY requested_at,project_id
		FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&projectID, &cursor, &through, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT id,data FROM events
		WHERE project_id=$1::bigint AND id>$2 AND id<=$3 AND kind='error' AND data->>'platform'='native'
		ORDER BY id LIMIT $4 FOR UPDATE`, projectID, cursor, through, nativeReprocessingBatchSize)
	if err != nil {
		return false, err
	}
	events := make([]NativeReprocessingEvent, 0, nativeReprocessingBatchSize)
	for rows.Next() {
		var event NativeReprocessingEvent
		if err := rows.Scan(&event.ID, &event.Data); err != nil {
			rows.Close()
			return false, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	if len(events) == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM project_native_reprocessing WHERE project_id=$1::bigint`, projectID); err != nil {
			return false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return true, nil
	}
	cached := projectID == cachedProjectID && generation == cachedGeneration
	var files []DebugSymbolFile
	if !cached {
		files, err = nativeReprocessingFiles(ctx, tx, projectID)
		if err != nil {
			return false, err
		}
	}
	updates, err := transform(projectID, generation, cached, files, events)
	if err != nil {
		return false, err
	}
	allowed := make(map[int64]bool, len(events))
	for _, event := range events {
		allowed[event.ID] = true
	}
	for id, data := range updates {
		if !allowed[id] || !json.Valid(data) {
			return false, errors.New("native reprocessing returned an invalid event update")
		}
		if _, err := tx.Exec(ctx, `UPDATE events SET data=$2::jsonb WHERE project_id=$1::bigint AND id=$3`, projectID, data, id); err != nil {
			return false, err
		}
	}
	lastID := events[len(events)-1].ID
	if lastID >= through {
		if _, err := tx.Exec(ctx, `DELETE FROM project_native_reprocessing WHERE project_id=$1::bigint`, projectID); err != nil {
			return false, err
		}
	} else if _, err := tx.Exec(ctx, `UPDATE project_native_reprocessing SET cursor_event_id=$2,
		requested_at=GREATEST(clock_timestamp(),requested_at+interval '1 microsecond') WHERE project_id=$1::bigint`, projectID, lastID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func nativeReprocessingFiles(ctx context.Context, tx pgx.Tx, projectID string) ([]DebugSymbolFile, error) {
	rows, err := tx.Query(ctx, `SELECT object_name,coalesce(debug_id,''),content FROM (
		SELECT object_name,debug_id,content,size,created_at,id,
			sum(size) OVER(ORDER BY created_at DESC,id DESC) AS running
		FROM debug_files WHERE project_id=$1::bigint
	) bounded WHERE running<=67108864 ORDER BY created_at DESC,id DESC LIMIT 25`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]DebugSymbolFile, 0, 25)
	for rows.Next() {
		var file DebugSymbolFile
		if err := rows.Scan(&file.Name, &file.DebugID, &file.Content); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}
