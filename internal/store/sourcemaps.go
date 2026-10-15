package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
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
	CodeFiles   []string  `json:"codeFiles,omitempty"`
}

const sourceMapColumns = "id::text,name,release,dist,debug_id,checksum,size,created_at,code_files"

func (s *Store) SaveSourceMap(ctx context.Context, userID, orgSlug, projectID, name, release, dist, debugID string, data []byte) (SourceMapFile, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SourceMapFile{}, false, err
	}
	defer tx.Rollback(ctx)
	var role string
	err = tx.QueryRow(ctx, `SELECT m.role FROM organizations o
		JOIN memberships m ON m.organization_id=o.id AND m.user_id=$1::bigint
		JOIN projects p ON p.organization_id=o.id
		WHERE o.slug=$2 AND p.id::text=$3 FOR SHARE OF m,p`, userID, orgSlug, projectID).Scan(&role)
	if err != nil {
		return SourceMapFile{}, false, dbError(err)
	}
	if role != "owner" && role != "admin" {
		return SourceMapFile{}, false, ErrForbidden
	}
	if release != "" {
		if _, err := ParseReleaseVersion(release); err != nil {
			return SourceMapFile{}, false, ErrInvalidReleaseInput
		}
		if err := observeRelease(ctx, tx, projectID, release); err != nil {
			return SourceMapFile{}, false, err
		}
		if err := lockReleaseArtifacts(ctx, tx, projectID, release); err != nil {
			return SourceMapFile{}, false, err
		}
	}
	meta, created, err := saveSourceMapTx(ctx, tx, projectID, name, release, dist, debugID, data, nil)
	if err != nil {
		return meta, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceMapFile{}, false, err
	}
	return meta, created, nil
}

func saveSourceMapTx(ctx context.Context, tx pgx.Tx, projectID, name, release, dist, debugID string, data []byte, aliases []string) (SourceMapFile, bool, error) {
	debugID = normalizeSourceMapDebugID(debugID)
	digest := sha256.Sum256(data)
	checksum := hex.EncodeToString(digest[:])
	aliases = sourceMapAliases(nil, aliases)
	meta, err := scanSourceMap(tx.QueryRow(ctx, `INSERT INTO source_maps(project_id,name,release,dist,debug_id,checksum,size,data,code_files)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(project_id,name,release,dist,debug_id) DO NOTHING
		RETURNING `+sourceMapColumns, projectID, name, release, dist, debugID, checksum, int64(len(data)), data, aliases))
	if err == nil {
		return meta, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SourceMapFile{}, false, err
	}
	// A new statement sees a competing upload after INSERT waits for commit.
	// The row lock also serializes alias union with concurrent native uploads.
	meta, err = scanSourceMap(tx.QueryRow(ctx, "SELECT "+sourceMapColumns+
		" FROM source_maps WHERE project_id::text=$1 AND name=$2 AND release=$3 AND dist=$4 AND debug_id=$5 FOR UPDATE", projectID, name, release, dist, debugID))
	if err != nil {
		return SourceMapFile{}, false, err
	}
	if meta.Checksum != checksum {
		return meta, false, ErrConflict
	}
	merged := sourceMapAliases(meta.CodeFiles, aliases)
	if len(merged) != len(meta.CodeFiles) {
		if _, err := tx.Exec(ctx, "UPDATE source_maps SET code_files=$2 WHERE id=$1::bigint", meta.ID, merged); err != nil {
			return SourceMapFile{}, false, err
		}
		meta.CodeFiles = merged
	}
	return meta, false, nil
}

func sourceMapAliases(existing, incoming []string) []string {
	seen := make(map[string]bool, len(existing)+len(incoming))
	for _, name := range existing {
		seen[name] = true
	}
	for _, name := range incoming {
		seen[name] = true
	}
	merged := make([]string, 0, len(seen))
	for name := range seen {
		merged = append(merged, name)
	}
	sort.Strings(merged)
	return merged
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
	err := row.Scan(&meta.ID, &meta.Name, &meta.Release, &meta.Dist, &meta.DebugID, &meta.Checksum, &meta.Size, &meta.DateCreated, &meta.CodeFiles)
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
	// Equivalent immutable payloads can have several release/dist selector rows.
	// Group metadata before the budget check, then fetch one representative's
	// bytes and the selected rows' complete alias union from the same snapshot.
	const selected = `WITH matching AS (
		SELECT id,name,lower(replace(debug_id,'-','')) AS debug_id,checksum,size,
			NOT($3<>'' AND release=$3 AND dist=$4) AS debug_id_only
		FROM source_maps WHERE project_id::text=$1 AND
			((debug_id<>'' AND debug_id=ANY($2::text[])) OR ($3<>'' AND release=$3 AND dist=$4))
	), logical AS (
		SELECT name,debug_id,checksum,min(id) AS id,max(size) AS size,
			bool_and(debug_id_only) AS debug_id_only,array_agg(id) AS ids
		FROM matching GROUP BY name,debug_id,checksum
	) `
	args := []any{projectID, ids, release, dist}
	var count, total int64
	if err := tx.QueryRow(ctx, selected+"SELECT count(*),COALESCE(sum(size),0)::bigint FROM logical", args...).Scan(&count, &total); err != nil {
		return nil, err
	}
	if count > 100 || total > maxBytes {
		return nil, fmt.Errorf("%w: %d maps, %d bytes", ErrArtifactLimit, count, total)
	}
	rows, err := tx.Query(ctx, selected+`SELECT l.name,l.debug_id,m.data,
		ARRAY(SELECT DISTINCT aliases.code_file FROM source_maps a
			CROSS JOIN LATERAL unnest(a.code_files) AS aliases(code_file)
			WHERE a.id=ANY(l.ids) ORDER BY aliases.code_file),l.debug_id_only
		FROM logical l JOIN source_maps m ON m.id=l.id
		ORDER BY l.name,l.debug_id,l.checksum,l.id`, args...)
	if err != nil {
		return nil, err
	}
	items := make([]artifact.SourceMap, 0, count)
	for rows.Next() {
		var sourceMap artifact.SourceMap
		if err := rows.Scan(&sourceMap.Name, &sourceMap.DebugID, &sourceMap.Data, &sourceMap.CodeFiles, &sourceMap.DebugIDOnly); err != nil {
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
