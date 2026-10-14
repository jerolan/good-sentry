package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodsentry/internal/artifact"

	"github.com/jackc/pgx/v5"
)

var ErrArtifactLimit = errors.New("source-map load exceeds artifact limit")

type SourceMapFile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Release     string    `json:"release"`
	Dist        string    `json:"dist"`
	DebugID     string    `json:"debugId"`
	Checksum    string    `json:"checksum"`
	Size        int64     `json:"size"`
	DateCreated time.Time `json:"dateCreated"`
}

const sourceMapColumns = "id::text,name,release,dist,debug_id,checksum,size,created_at"

func (s *Store) SaveSourceMap(ctx context.Context, userID, orgSlug, projectID, name, release, dist, debugID string, data []byte) (SourceMapFile, bool, error) {
	org, err := s.sourceMapProject(ctx, userID, orgSlug, projectID)
	if err != nil {
		return SourceMapFile{}, false, err
	}
	if org.Role != "owner" && org.Role != "admin" {
		return SourceMapFile{}, false, ErrForbidden
	}
	debugID = normalizeSourceMapDebugID(debugID)
	digest := sha256.Sum256(data)
	checksum := hex.EncodeToString(digest[:])
	meta, err := scanSourceMap(s.pool.QueryRow(ctx, `INSERT INTO source_maps(project_id,name,release,dist,debug_id,checksum,size,data)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(project_id,name,release,dist,debug_id) DO NOTHING
		RETURNING `+sourceMapColumns, projectID, name, release, dist, debugID, checksum, int64(len(data)), data))
	if err == nil {
		return meta, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SourceMapFile{}, false, err
	}
	// This is a separate statement so a competing upload that won the unique
	// key is visible after INSERT waited for that upload's commit.
	meta, err = scanSourceMap(s.pool.QueryRow(ctx, "SELECT "+sourceMapColumns+
		" FROM source_maps WHERE project_id::text=$1 AND name=$2 AND release=$3 AND dist=$4 AND debug_id=$5", projectID, name, release, dist, debugID))
	if err != nil {
		return SourceMapFile{}, false, err
	}
	if meta.Checksum != checksum {
		return meta, false, ErrConflict
	}
	return meta, false, nil
}

func (s *Store) ListSourceMaps(ctx context.Context, userID, orgSlug, projectID string) ([]SourceMapFile, error) {
	if _, err := s.sourceMapProject(ctx, userID, orgSlug, projectID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT "+sourceMapColumns+
		" FROM source_maps WHERE project_id::text=$1 ORDER BY name,release,dist,debug_id,id", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourceMapFile{}
	for rows.Next() {
		meta, err := scanSourceMap(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, meta)
	}
	return items, rows.Err()
}

func (s *Store) sourceMapProject(ctx context.Context, userID, orgSlug, projectID string) (Organization, error) {
	org, err := s.Organization(ctx, userID, orgSlug)
	if err != nil {
		return Organization{}, err
	}
	var belongs bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id::text=$1 AND organization_id::text=$2)", projectID, org.ID).Scan(&belongs); err != nil {
		return Organization{}, err
	}
	if !belongs {
		return Organization{}, ErrNotFound
	}
	return org, nil
}

func scanSourceMap(row scanner) (SourceMapFile, error) {
	var meta SourceMapFile
	err := row.Scan(&meta.ID, &meta.Name, &meta.Release, &meta.Dist, &meta.DebugID, &meta.Checksum, &meta.Size, &meta.DateCreated)
	return meta, dbError(err)
}

// SourceMaps is an internal ingestion operation. Authentication has already
// selected the project; maps may cross releases only through an exact debug ID.
func (s *Store) SourceMaps(ctx context.Context, projectID, release, dist string, debugIDs []string, maxBytes int64) ([]artifact.SourceMap, error) {
	ids := make([]string, 0, len(debugIDs))
	for _, id := range debugIDs {
		if normalized := normalizeSourceMapDebugID(id); normalized != "" {
			ids = append(ids, normalized)
		}
	}
	if release == "" && len(ids) == 0 {
		return []artifact.SourceMap{}, nil
	}
	// The count/size preflight and data read share one snapshot. Concurrent
	// uploads cannot make the second read exceed the checked in-memory budget.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	const where = ` FROM source_maps WHERE project_id::text=$1 AND
		((debug_id<>'' AND debug_id=ANY($2::text[])) OR ($3<>'' AND release=$3 AND dist=$4))`
	args := []any{projectID, ids, release, dist}
	var count, total int64
	if err := tx.QueryRow(ctx, "SELECT count(*),COALESCE(sum(size),0)::bigint"+where, args...).Scan(&count, &total); err != nil {
		return nil, err
	}
	if count > 100 || total > maxBytes {
		return nil, fmt.Errorf("%w: %d maps, %d bytes", ErrArtifactLimit, count, total)
	}
	rows, err := tx.Query(ctx, "SELECT name,debug_id,data,NOT($3<>'' AND release=$3 AND dist=$4)"+where+" ORDER BY name,release,dist,debug_id,id", args...)
	if err != nil {
		return nil, err
	}
	items := make([]artifact.SourceMap, 0, count)
	for rows.Next() {
		var sourceMap artifact.SourceMap
		if err := rows.Scan(&sourceMap.Name, &sourceMap.DebugID, &sourceMap.Data, &sourceMap.DebugIDOnly); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, sourceMap)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}

func normalizeSourceMapDebugID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", ""))
}
