package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"goodsentry/internal/artifact"

	"github.com/jackc/pgx/v5"
)

// artifactAccess applies the same current-role boundary to negotiation, reads,
// staging, and final materialization. Read-only transactions use their snapshot.
func artifactAccess(ctx context.Context, tx pgx.Tx, access ManagementAccess, role string) error {
	if access.ReadOnly || role != "owner" && role != "admin" {
		return ErrForbidden
	}
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE organization_id=$1::bigint
        AND ($2 OR id=ANY($3::bigint[])))`, access.OrganizationID, access.AllProjects, access.ProjectIDs).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ArtifactUploadAccess(ctx context.Context, access ManagementAccess) error {
	tx, role, err := s.releaseTransaction(ctx, access, false)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := artifactAccess(ctx, tx, access, role); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SaveArtifactChunks(ctx context.Context, access ManagementAccess, chunks []ArtifactChunk, policy ArtifactStagingPolicy) error {
	tx, role, err := s.releaseTransaction(ctx, access, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := artifactAccess(ctx, tx, access, role); err != nil {
		return err
	}
	if policy.TTL <= 0 || policy.MaxBytes < 0 {
		return ErrArtifactLimit
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "artifact-staging:"+access.OrganizationID); err != nil {
		return err
	}
	// Keep one currently authorized project alive through this write.
	var projectID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE organization_id=$1::bigint
        AND ($2 OR id=ANY($3::bigint[])) ORDER BY id LIMIT 1 FOR SHARE`, access.OrganizationID, access.AllProjects, access.ProjectIDs).Scan(&projectID); err != nil {
		return dbError(err)
	}
	var cutoff time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoff); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM artifact_chunks WHERE organization_id=$1::bigint AND expires_at<=$2`, access.OrganizationID, cutoff); err != nil {
		return err
	}
	var total int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(size),0)::bigint FROM artifact_chunks WHERE organization_id=$1::bigint`, access.OrganizationID).Scan(&total); err != nil {
		return err
	}
	unique := make(map[string][]byte, len(chunks))
	for _, chunk := range chunks {
		if prior, ok := unique[chunk.Checksum]; ok && !bytes.Equal(prior, chunk.Data) {
			return ErrConflict
		}
		unique[chunk.Checksum] = chunk.Data
	}
	keys := make([]string, 0, len(unique))
	for checksum := range unique {
		keys = append(keys, checksum)
	}
	sort.Strings(keys)
	for _, checksum := range keys {
		data := unique[checksum]
		var matches bool
		err := tx.QueryRow(ctx, `SELECT data=$3 FROM artifact_chunks WHERE organization_id=$1::bigint AND checksum=$2`, access.OrganizationID, checksum, data).Scan(&matches)
		if err == nil {
			if !matches {
				return ErrConflict
			}
		} else if errors.Is(err, pgx.ErrNoRows) {
			if int64(len(data)) > policy.MaxBytes-total {
				return ErrArtifactLimit
			}
			total += int64(len(data))
		} else {
			return err
		}
	}
	if total > policy.MaxBytes {
		return ErrArtifactLimit
	}
	for _, checksum := range keys {
		_, err := tx.Exec(ctx, `INSERT INTO artifact_chunks(organization_id,checksum,data,size,expires_at)
            VALUES($1::bigint,$2,$3,$4,$5::timestamptz+make_interval(secs=>$6))
            ON CONFLICT(organization_id,checksum) DO UPDATE SET expires_at=EXCLUDED.expires_at`,
			access.OrganizationID, checksum, unique[checksum], len(unique[checksum]), cutoff, policy.TTL.Seconds())
		if err != nil {
			return dbError(err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ArtifactChunkData(ctx context.Context, access ManagementAccess, projectRefs []string, checksums []string, maxBytes int64) ([]byte, []string, error) {
	tx, role, err := s.releaseTransaction(ctx, access, false)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)
	if err := artifactAccess(ctx, tx, access, role); err != nil {
		return nil, nil, err
	}
	if len(projectRefs) == 0 {
		return nil, nil, ErrNotFound
	}
	if _, err := releaseProjectReferences(ctx, tx, access, projectRefs); err != nil {
		return nil, nil, err
	}
	var cutoff time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoff); err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT checksum,size FROM artifact_chunks WHERE organization_id=$1::bigint
        AND checksum=ANY($2::text[]) AND expires_at>$3`, access.OrganizationID, checksums, cutoff)
	if err != nil {
		return nil, nil, err
	}
	sizes := map[string]int64{}
	for rows.Next() {
		var checksum string
		var size int64
		if err := rows.Scan(&checksum, &size); err != nil {
			rows.Close()
			return nil, nil, err
		}
		sizes[checksum] = size
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	missing := []string{}
	missingSet := map[string]bool{}
	var total int64
	for _, checksum := range checksums {
		size, ok := sizes[checksum]
		if !ok {
			if !missingSet[checksum] {
				missing = append(missing, checksum)
				missingSet[checksum] = true
			}
			continue
		}
		if size > maxBytes-total {
			return nil, nil, ErrArtifactLimit
		}
		total += size
	}
	if len(missing) != 0 {
		return nil, missing, nil
	}
	if total > int64(int(^uint(0)>>1)) {
		return nil, nil, ErrArtifactLimit
	}
	rows, err = tx.Query(ctx, `SELECT checksum,data FROM artifact_chunks WHERE organization_id=$1::bigint
        AND checksum=ANY($2::text[]) AND expires_at>$3`, access.OrganizationID, checksums, cutoff)
	if err != nil {
		return nil, nil, err
	}
	payloads := map[string][]byte{}
	for rows.Next() {
		var checksum string
		var data []byte
		if err := rows.Scan(&checksum, &data); err != nil {
			rows.Close()
			return nil, nil, err
		}
		payloads[checksum] = data
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	data := make([]byte, 0, int(total))
	for _, checksum := range checksums {
		data = append(data, payloads[checksum]...)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return data, missing, nil
}

func (s *Store) SaveBundleSourceMaps(ctx context.Context, access ManagementAccess, projectRefs []string, version ReleaseVersion, dist string, maps []artifact.SourceMap, maxRecords int) error {
	tx, role, err := s.releaseTransaction(ctx, access, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := artifactAccess(ctx, tx, access, role); err != nil {
		return err
	}
	if len(projectRefs) == 0 {
		return ErrNotFound
	}
	ids, err := releaseProjectReferences(ctx, tx, access, projectRefs)
	if err != nil {
		return err
	}
	if maxRecords < 0 || len(ids) > 0 && len(maps) > maxRecords/len(ids) {
		return ErrArtifactLimit
	}
	if version != "" {
		if _, _, err := releaseIdentity(ctx, tx, access.OrganizationID, version, false); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	if err := releaseLockProjects(ctx, tx, access.OrganizationID, ids); err != nil {
		return err
	}
	for _, id := range ids {
		if err := lockReleaseArtifacts(ctx, tx, strconv.FormatInt(id, 10), string(version)); err != nil {
			return err
		}
	}
	sorted := append([]artifact.SourceMap(nil), maps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return normalizeSourceMapDebugID(sorted[i].DebugID) < normalizeSourceMapDebugID(sorted[j].DebugID)
	})
	for _, id := range ids {
		for _, sourceMap := range sorted {
			if _, _, err := saveSourceMapTx(ctx, tx, strconv.FormatInt(id, 10), sourceMap.Name, string(version), dist, sourceMap.DebugID, sourceMap.Data, sourceMap.CodeFiles); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) CleanupArtifactChunks(ctx context.Context) (int64, error) {
	result, err := s.pool.Exec(ctx, `WITH expired AS (
        SELECT organization_id,checksum FROM artifact_chunks WHERE expires_at<=clock_timestamp()
        ORDER BY expires_at,organization_id,checksum LIMIT 1000 FOR UPDATE SKIP LOCKED
    ) DELETE FROM artifact_chunks c USING expired e
        WHERE c.organization_id=e.organization_id AND c.checksum=e.checksum`)
	if err != nil {
		return 0, fmt.Errorf("cleanup artifact chunks: %w", err)
	}
	return result.RowsAffected(), nil
}
