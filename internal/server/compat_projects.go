package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxProjectOffset = 100000000

func projectCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("offset:" + strconv.Itoa(offset)))
}

func readProjectCursor(value string) (int, bool) {
	if value == "" {
		return 0, true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return 0, false
	}
	text, found := strings.CutPrefix(string(decoded), "offset:")
	n, err := strconv.ParseInt(text, 10, 64)
	return int(n), found && err == nil && n >= 0 && n <= maxProjectOffset && strconv.FormatInt(n, 10) == text
}

func (s *Server) compatibleProjects(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	q, valid := credentialQuery(w, r)
	if !valid {
		return
	}
	query := q.Get("query")
	if !credentialText(query, 200) {
		failure(w, 400, "Query must contain at most 200 characters without controls")
		return
	}
	if len(q["limit"]) > 1 || len(q["per_page"]) > 1 {
		failure(w, 400, "Pagination parameters must not repeat")
		return
	}
	limit := int64(100)
	if q.Has("limit") {
		limit, valid = occurrenceNumber(q.Get("limit"), 100)
		if !valid {
			failure(w, 400, "limit must be a positive decimal integer up to 100")
			return
		}
	}
	if q.Has("per_page") {
		perPage, valid := occurrenceNumber(q.Get("per_page"), 100)
		if !valid {
			failure(w, 400, "per_page must be a positive decimal integer up to 100")
			return
		}
		if q.Has("limit") && perPage != limit {
			failure(w, 400, "limit and per_page must agree")
			return
		}
		limit = perPage
	}
	offset, valid := readProjectCursor(q.Get("cursor"))
	if !valid {
		failure(w, 400, "Cursor is invalid")
		return
	}
	projects, err := s.db.ManagementProjects(r.Context(), access, query, offset, int(limit))
	if err != nil {
		report(w, err)
		return
	}
	next := len(projects) > int(limit)
	if next {
		projects = projects[:int(limit)]
	}
	nextOffset := offset + int(limit)
	next = next && nextOffset <= maxProjectOffset
	previous := max(0, offset-int(limit))
	hasPrevious := false
	if offset > 0 {
		before, err := s.db.ManagementProjects(r.Context(), access, query, previous, 1)
		if err != nil {
			report(w, err)
			return
		}
		hasPrevious = len(before) > 0
	}
	links := make([]string, 0, 2)
	pageSizeParameter := "per_page"
	if q.Has("limit") {
		pageSizeParameter = "limit"
	}
	for _, link := range []struct {
		relation string
		offset   int
		results  bool
	}{{"previous", previous, hasPrevious}, {"next", min(maxProjectOffset, nextOffset), next}} {
		cursor := projectCursor(link.offset)
		q.Set("cursor", cursor)
		q.Set(pageSizeParameter, strconv.FormatInt(limit, 10))
		public := strings.TrimRight(s.cfg.Domain, "/") + "/api/0/organizations/" + url.PathEscape(r.PathValue("org")) + "/projects/?" + q.Encode()
		links = append(links, fmt.Sprintf(`<%s>; rel="%s"; results="%t"; cursor="%s"`, public, link.relation, link.results, cursor))
	}
	w.Header().Set("Link", strings.Join(links, ", "))
	writeJSON(w, 200, projects)
}

func (s *Server) compatibleProject(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	project, err := s.db.ManagementProject(r.Context(), access, r.PathValue("project"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, project)
}
