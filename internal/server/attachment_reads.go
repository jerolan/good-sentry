package server

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"

	"goodsentry/internal/store"
)

type nativeAttachment struct {
	store.Attachment
	DownloadName string `json:"downloadName"`
}

// Filenames remain metadata. Only a bounded basename becomes a MIME parameter;
// it is never a server path or an unescaped response header fragment.
func attachmentDownloadName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if name == "" || name == "." || name == ".." || name == "/" {
		return "attachment"
	}
	runes := []rune(name)
	if len(runes) > 255 {
		name = string(runes[:255])
	}
	return name
}

func reportAttachmentRead(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidEventFilter) {
		failure(w, 400, "Invalid attachment reference or page")
		return
	}
	report(w, err)
}

func (s *Server) issueEventAttachments(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Invalid attachment query")
		return
	}
	page := 1
	for name, values := range query {
		if name != "page" || len(values) != 1 {
			failure(w, 400, "Only one attachment page parameter is supported")
			return
		}
		page, err = strconv.Atoi(values[0])
		if err != nil || page < 1 || page > 1000000 || strconv.Itoa(page) != values[0] {
			failure(w, 400, "Attachment page must be a canonical integer from 1 to 1000000")
			return
		}
	}
	files, more, err := s.db.IssueEventAttachments(r.Context(), current(r).User.ID, r.PathValue("issue"), r.PathValue("event"), page)
	if err != nil {
		reportAttachmentRead(w, err)
		return
	}
	data := make([]nativeAttachment, 0, len(files))
	for _, file := range files {
		data = append(data, nativeAttachment{Attachment: file, DownloadName: attachmentDownloadName(file.Filename)})
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, map[string]any{"data": data, "hasMore": more, "pageSize": 25})
}

func (s *Server) issueEventAttachment(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		failure(w, 400, "Attachment downloads accept no query parameters")
		return
	}
	file, err := s.db.IssueEventAttachment(r.Context(), current(r).User.ID, r.PathValue("issue"), r.PathValue("event"), r.PathValue("attachment"), r.Method != http.MethodHead)
	if err != nil {
		reportAttachmentRead(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachmentDownloadName(file.Filename)}))
	w.Header().Set("Content-Length", strconv.Itoa(file.Size))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(file.Data)
	}
}
