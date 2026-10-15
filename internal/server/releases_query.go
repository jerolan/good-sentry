package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"goodsentry/internal/store"
)

type releasePaging struct {
	query               url.Values
	offset, limit, page int
}

func releasePagination(w http.ResponseWriter, r *http.Request, native bool) (releasePaging, bool) {
	q, valid := credentialQuery(w, r)
	if !valid {
		return releasePaging{}, false
	}
	for _, field := range []string{"page", "per_page", "limit", "cursor", "query"} {
		if len(q[field]) > 1 {
			failure(w, 400, field+" must be supplied once")
			return releasePaging{}, false
		}
	}
	paging := releasePaging{query: q, limit: 100, page: 1}
	if native {
		paging.limit = 25
		if q.Has("page") {
			page, ok := occurrenceNumber(q.Get("page"), 1000000)
			if !ok {
				failure(w, 400, "Page must be a positive decimal integer up to 1000000")
				return paging, false
			}
			paging.page = int(page)
		}
		paging.offset = (paging.page - 1) * paging.limit
		return paging, true
	}
	for _, key := range []string{"per_page", "limit"} {
		if q.Has(key) {
			value, ok := occurrenceNumber(q.Get(key), 100)
			if !ok {
				failure(w, 400, key+" must be a positive decimal integer up to 100")
				return paging, false
			}
			paging.limit = int(value)
		}
	}
	if q.Has("per_page") && q.Has("limit") && q.Get("per_page") != q.Get("limit") {
		failure(w, 400, "per_page and limit must agree")
		return paging, false
	}
	paging.offset, valid = readProjectCursor(q.Get("cursor"))
	if !valid {
		failure(w, 400, "Cursor is invalid")
	}
	return paging, valid
}

func (s *Server) releaseListFilter(w http.ResponseWriter, r *http.Request, access store.ManagementAccess, paging releasePaging, native bool) (store.ReleaseFilter, bool) {
	q := paging.query
	filter := store.ReleaseFilter{Query: q.Get("query"), Offset: paging.offset, Limit: paging.limit}
	if !credentialText(filter.Query, 200) {
		failure(w, 400, "Query must contain at most 200 characters without controls")
		return filter, false
	}
	if native && (len(q["project"]) > 1 || len(q["environment"]) > 1) {
		failure(w, 400, "Native project and environment filters must be supplied once")
		return filter, false
	}
	filter.Environments = q["environment"]
	for _, environment := range filter.Environments {
		if environment == "" || !credentialText(environment, 64) {
			failure(w, 400, "Environment must contain 1 to 64 characters without controls")
			return filter, false
		}
	}
	refs := q["project"]
	for _, ref := range refs {
		if ref == "-1" {
			if len(refs) != 1 || r.PathValue("project") != "" {
				failure(w, 400, "The all-project filter must be used alone on an organization list")
				return filter, false
			}
			refs = nil
			break
		}
	}
	var fixed *store.ManagementProject
	if ref := r.PathValue("project"); ref != "" {
		project, err := s.db.ManagementProject(r.Context(), access, ref)
		if err != nil {
			report(w, err)
			return filter, false
		}
		fixed = &project
	}
	for _, ref := range refs {
		if ref == "" || !credentialText(ref, 200) {
			failure(w, 400, "Project filters must be nonempty slugs or IDs")
			return filter, false
		}
		project, err := s.db.ManagementProject(r.Context(), access, ref)
		if err != nil {
			report(w, err)
			return filter, false
		}
		if fixed != nil && project.ID != fixed.ID {
			failure(w, 400, "Project filter conflicts with the project path")
			return filter, false
		}
		filter.ProjectRefs = append(filter.ProjectRefs, project.Slug)
	}
	if fixed != nil {
		filter.ProjectRefs = []string{fixed.Slug}
	}
	return filter, true
}

func (s *Server) releaseLinks(w http.ResponseWriter, r *http.Request, paging releasePaging, previous, next bool) {
	q := paging.query
	q.Set("per_page", strconv.Itoa(paging.limit))
	links := make([]string, 0, 2)
	for _, link := range []struct {
		relation string
		offset   int
		results  bool
	}{{"previous", max(0, paging.offset-paging.limit), previous}, {"next", min(maxProjectOffset, paging.offset+paging.limit), next && paging.offset+paging.limit <= maxProjectOffset}} {
		cursor := projectCursor(link.offset)
		q.Set("cursor", cursor)
		public := strings.TrimRight(s.cfg.Domain, "/") + r.URL.EscapedPath() + "?" + q.Encode()
		links = append(links, fmt.Sprintf(`<%s>; rel="%s"; results="%t"; cursor="%s"`, public, link.relation, link.results, cursor))
	}
	w.Header().Set("Link", strings.Join(links, ", "))
}

func releaseNativePage(data any, paging releasePaging, more bool) any {
	return struct {
		Data     any  `json:"data"`
		Page     int  `json:"page"`
		PageSize int  `json:"pageSize"`
		HasMore  bool `json:"hasMore"`
	}{data, paging.page, 25, more}
}
