package server

import (
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"goodsentry/internal/store"
)

type compatibleReleaseFile struct {
	ID          string            `json:"id"`
	DateCreated time.Time         `json:"dateCreated"`
	SHA1        string            `json:"sha1"`
	Headers     map[string]string `json:"headers"`
	Name        string            `json:"name"`
	Size        int64             `json:"size"`
}

func compatibleReleaseFileView(file store.ReleaseFile) compatibleReleaseFile {
	return compatibleReleaseFile{
		ID: file.ID, DateCreated: file.DateCreated.UTC(), SHA1: file.SHA1,
		Name: file.Name, Size: file.Size,
	}
}

func releaseFilePage(w http.ResponseWriter, query url.Values) (int, int, bool) {
	limit := int64(50)
	if query.Has("limit") || query.Has("per_page") {
		value := query.Get("limit")
		if query.Has("per_page") {
			if query.Has("limit") && query.Get("limit") != query.Get("per_page") {
				failure(w, 400, "limit and per_page must agree")
				return 0, 0, false
			}
			value = query.Get("per_page")
		}
		parsed, valid := occurrenceNumber(value, 100)
		if !valid {
			failure(w, 400, "limit must be a positive decimal integer up to 100")
			return 0, 0, false
		}
		limit = parsed
	}
	offset, valid := readProjectCursor(query.Get("cursor"))
	if !valid {
		failure(w, 400, "Cursor is invalid")
		return 0, 0, false
	}
	return offset, int(limit), true
}

func (s *Server) compatibleReleaseFiles(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	query, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, query, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, valid := releaseFilePage(w, query)
	if !valid {
		return
	}
	version, valid := releaseVersion(w, r)
	if !valid {
		return
	}
	files, more, err := s.db.ReleaseFiles(r.Context(), access, r.PathValue("org"), r.PathValue("project"), string(version), offset, limit)
	if err != nil {
		reportRelease(w, err)
		return
	}
	result := make([]compatibleReleaseFile, 0, len(files))
	for _, file := range files {
		result = append(result, compatibleReleaseFileView(file))
	}
	s.discoveryLinks(w, r, query, offset, limit, more)
	writeJSON(w, 200, result)
}

func (s *Server) compatibleReleaseFile(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	if r.URL.RawQuery != "" {
		failure(w, 400, "Release file detail accepts no query parameters")
		return
	}
	version, valid := releaseVersion(w, r)
	if !valid {
		return
	}
	fileID, valid := occurrenceNumber(r.PathValue("file"), math.MaxInt64)
	if !valid {
		failure(w, 400, "File ID must be a positive decimal integer")
		return
	}
	file, err := s.db.ReleaseFile(r.Context(), access, r.PathValue("org"), r.PathValue("project"), string(version), strconv.FormatInt(fileID, 10))
	if err != nil {
		reportRelease(w, err)
		return
	}
	writeJSON(w, 200, compatibleReleaseFileView(file))
}

func (s *Server) compatibleDeleteReleaseFile(w http.ResponseWriter, r *http.Request) {
	access, valid := s.projectWriteAccess(w, r)
	if !valid {
		return
	}
	if r.URL.RawQuery != "" {
		failure(w, 400, "Release file deletion accepts no query parameters")
		return
	}
	version, valid := releaseVersion(w, r)
	if !valid {
		return
	}
	fileID, valid := occurrenceNumber(r.PathValue("file"), math.MaxInt64)
	if !valid {
		failure(w, 400, "File ID must be a positive decimal integer")
		return
	}
	if err := s.db.DeleteReleaseFile(r.Context(), access, r.PathValue("org"), r.PathValue("project"), string(version), strconv.FormatInt(fileID, 10)); err != nil {
		reportRelease(w, err)
		return
	}
	writeJSON(w, 204, nil)
}
