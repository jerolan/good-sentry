package store

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// ReleaseFile is the compatible view of a release-scoped source map. The
// underlying source_maps row remains authoritative for the bytes and SHA-256
// deduplication checksum.
type ReleaseFile struct {
	ID          string
	Name        string
	SHA1        string
	Size        int64
	DateCreated time.Time
}

const releaseFileColumns = "sm.id::text,sm.name,sm.size,sm.created_at,sm.data"

func scanReleaseFile(row interface{ Scan(...any) error }) (ReleaseFile, error) {
	var file ReleaseFile
	var data []byte
	if err := row.Scan(&file.ID, &file.Name, &file.Size, &file.DateCreated, &data); err != nil {
		return file, dbError(err)
	}
	sum := sha1.Sum(data)
	file.SHA1 = hex.EncodeToString(sum[:])
	file.DateCreated = file.DateCreated.UTC()
	return file, nil
}

func (s *Store) releaseFileProject(ctx context.Context, access ManagementAccess, projectRef string) (string, error) {
	if projectRef == "" {
		if access.OrganizationID == "" {
			return "", ErrNotFound
		}
		return "", nil
	}
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return "", err
	}
	return project.ID, nil
}

func (s *Store) ReleaseFiles(ctx context.Context, access ManagementAccess, orgRef, projectRef, version string, offset, limit int) ([]ReleaseFile, bool, error) {
	if _, err := ParseReleaseVersion(version); err != nil {
		return nil, false, ErrInvalidReleaseInput
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, false, ErrInvalidReleaseInput
	}
	projectID, err := s.releaseFileProject(ctx, access, projectRef)
	if err != nil {
		return nil, false, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+releaseFileColumns+` FROM source_maps sm
		JOIN projects p ON p.id=sm.project_id
		WHERE p.organization_id=$1::bigint AND sm.release=$2
		AND ($3 OR p.id=ANY($4::bigint[]))
		AND ($5::bigint=0 OR p.id=$5::bigint)
		ORDER BY sm.created_at DESC,sm.id DESC LIMIT $6 OFFSET $7`,
		access.OrganizationID, version, access.AllProjects, access.ProjectIDs, referenceID(projectID), limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	files := []ReleaseFile{}
	for rows.Next() {
		file, err := scanReleaseFile(rows)
		if err != nil {
			return nil, false, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := false
	if len(files) > limit {
		more = true
		files = files[:limit]
	}
	return files, more, nil
}

func (s *Store) ReleaseFile(ctx context.Context, access ManagementAccess, orgRef, projectRef, version, fileID string) (ReleaseFile, error) {
	if _, err := ParseReleaseVersion(version); err != nil {
		return ReleaseFile{}, ErrInvalidReleaseInput
	}
	id := referenceID(fileID)
	if id == 0 {
		return ReleaseFile{}, ErrNotFound
	}
	projectID, err := s.releaseFileProject(ctx, access, projectRef)
	if err != nil {
		return ReleaseFile{}, err
	}
	file, err := scanReleaseFile(s.pool.QueryRow(ctx, `SELECT `+releaseFileColumns+` FROM source_maps sm
		JOIN projects p ON p.id=sm.project_id
		WHERE p.organization_id=$1::bigint AND sm.release=$2 AND sm.id=$3::bigint
		AND ($4 OR p.id=ANY($5::bigint[]))
		AND ($6::bigint=0 OR p.id=$6::bigint)`,
		access.OrganizationID, version, id, access.AllProjects, access.ProjectIDs, referenceID(projectID)))
	return file, err
}

func (s *Store) DeleteReleaseFile(ctx context.Context, access ManagementAccess, orgRef, projectRef, version, fileID string) error {
	if _, err := ParseReleaseVersion(version); err != nil {
		return ErrInvalidReleaseInput
	}
	id := referenceID(fileID)
	if id == 0 {
		return ErrNotFound
	}
	projectID, err := s.releaseFileProject(ctx, access, projectRef)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM source_maps sm USING projects p
		WHERE sm.project_id=p.id AND p.organization_id=$1::bigint AND sm.release=$2 AND sm.id=$3::bigint
		AND ($4 OR p.id=ANY($5::bigint[]))
		AND ($6::bigint=0 OR p.id=$6::bigint)`,
		access.OrganizationID, version, id, access.AllProjects, access.ProjectIDs, referenceID(projectID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
