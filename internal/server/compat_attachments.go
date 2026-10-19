package server

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"goodsentry/internal/store"
)

func (s *Server) registerCompatibleAttachmentRoutes(mux *http.ServeMux) {
	root := "/api/0/projects/{org}/{project}/events/{event}/attachments/"
	scopes := []string{"project:read", "project:write"}
	mux.Handle("GET "+root+"{$}", s.compatibleAny(scopes, s.compatibleAttachments))
	mux.Handle("GET "+root+"{attachment}/{$}", s.compatibleAny(scopes, s.compatibleAttachment))
}

func (s *Server) compatibleAttachments(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	query, ok := credentialQuery(w, r)
	if !ok {
		return
	}
	for key, values := range query {
		if (key != "query" && key != "cursor" && key != "per_page") || len(values) != 1 {
			failure(w, 400, "Unknown or repeated attachment query parameter")
			return
		}
	}
	filter := store.AttachmentFilter{Query: query.Get("query"), Limit: 100}
	if !credentialText(filter.Query, 200) {
		failure(w, 400, "Attachment query must contain at most 200 characters without controls")
		return
	}
	if query.Has("per_page") {
		limit, valid := occurrenceNumber(query.Get("per_page"), 100)
		if !valid {
			failure(w, 400, "per_page must be a decimal integer from 1 to 100")
			return
		}
		filter.Limit = int(limit)
	}
	filter.Offset, ok = readProjectCursor(query.Get("cursor"))
	if !ok {
		failure(w, 400, "Cursor is invalid")
		return
	}
	files, err := s.db.CompatibleAttachments(r.Context(), access, r.PathValue("project"), r.PathValue("event"), filter)
	if err != nil {
		reportCompatibleEvent(w, err)
		return
	}
	more := len(files) > filter.Limit
	if more {
		files = files[:filter.Limit]
	}
	s.compatibleAttachmentLinks(w, r, query, filter.Offset, filter.Limit, more)
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, files)
}

func (s *Server) compatibleAttachmentLinks(w http.ResponseWriter, r *http.Request, query url.Values, offset, limit int, more bool) {
	links := make([]string, 0, 2)
	for _, link := range []struct {
		relation string
		offset   int
		results  bool
	}{{"previous", max(0, offset-limit), offset > 0}, {"next", min(maxProjectOffset, offset+limit), more && offset+limit <= maxProjectOffset}} {
		cursor := projectCursor(link.offset)
		query.Set("cursor", cursor)
		query.Set("per_page", strconv.Itoa(limit))
		public := strings.TrimRight(s.cfg.Domain, "/") + r.URL.EscapedPath() + "?" + query.Encode()
		links = append(links, fmt.Sprintf(`<%s>; rel="%s"; results="%t"; cursor="%s"`, public, link.relation, link.results, cursor))
	}
	w.Header().Set("Link", strings.Join(links, ", "))
}

func (s *Server) compatibleAttachment(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	query, ok := credentialQuery(w, r)
	if !ok {
		return
	}
	for key, values := range query {
		if key != "download" || len(values) != 1 {
			failure(w, 400, "Only one download parameter is supported")
			return
		}
	}
	download := query.Has("download")
	file, err := s.db.CompatibleAttachment(r.Context(), access, r.PathValue("project"), r.PathValue("event"), r.PathValue("attachment"), download && r.Method != http.MethodHead)
	if err != nil {
		reportCompatibleEvent(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if !download {
		writeJSON(w, 200, file)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachmentDownloadName(file.Name)}))
	w.Header().Set("Content-Length", strconv.Itoa(file.Size))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(file.Data)
	}
}
