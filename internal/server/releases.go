package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"goodsentry/internal/store"
)

func (s *Server) registerReleaseRoutes(mux *http.ServeMux) {
	read := []string{"project:read", "project:releases"}
	for _, root := range []string{"/api/0/organizations/{org}/releases/", "/api/0/projects/{org}/{project}/releases/"} {
		mux.Handle("GET "+root+"{$}", s.compatibleAny(read, s.compatibleReleases))
		mux.Handle("POST "+root+"{$}", s.compatible("project:releases", s.createRelease))
		mux.Handle("GET "+root+"{version}/{$}", s.compatibleAny(read, s.release))
		mux.Handle("PUT "+root+"{version}/{$}", s.compatible("project:releases", s.updateRelease))
		mux.Handle("DELETE "+root+"{version}/{$}", s.compatible("project:releases", s.deleteRelease))
		mux.Handle("GET "+root+"{version}/commits/{$}", s.compatibleAny(read, s.compatibleReleaseCommits))
	}
	root := "/api/0/organizations/{org}/releases/{version}/"
	mux.Handle("POST "+root+"commits/{$}", s.compatible("project:releases", s.replaceReleaseCommits))
	mux.Handle("GET "+root+"deploys/{$}", s.compatibleAny(read, s.compatibleReleaseDeploys))
	mux.Handle("POST "+root+"deploys/{$}", s.compatible("project:releases", s.createReleaseDeploy))
	mux.Handle("GET "+root+"previous-with-commits/{$}", s.compatibleAny(read, s.previousReleaseWithCommits))
	mux.Handle("GET /api/0/organizations/{org}/repos/{$}", s.compatible("org:read", s.releaseRepositories))
	mux.Handle("POST /api/0/organizations/{org}/repos/{$}", s.compatible("org:write", s.createReleaseRepository))
	mux.Handle("GET /api/v1/organizations/{org}/releases", s.auth(http.HandlerFunc(s.nativeReleases)))
	mux.Handle("GET /api/v1/organizations/{org}/releases/{version}/commits", s.auth(http.HandlerFunc(s.nativeReleaseCommits)))
	mux.Handle("GET /api/v1/organizations/{org}/releases/{version}/deploys", s.auth(http.HandlerFunc(s.nativeReleaseDeploys)))
}

func reportRelease(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidReleaseInput) {
		failure(w, 400, "Invalid release metadata or deployment dates")
		return
	}
	report(w, err)
}

func releaseVersion(w http.ResponseWriter, r *http.Request) (store.ReleaseVersion, bool) {
	version, err := store.ParseReleaseVersion(r.PathValue("version"))
	if err != nil {
		failure(w, 400, err.Error())
		return "", false
	}
	return version, true
}

func (s *Server) compatibleReleases(w http.ResponseWriter, r *http.Request) {
	s.listReleases(w, r, false)
}

func (s *Server) nativeReleases(w http.ResponseWriter, r *http.Request) {
	s.listReleases(w, r, true)
}

func (s *Server) listReleases(w http.ResponseWriter, r *http.Request, native bool) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	paging, ok := releasePagination(w, r, native)
	if !ok {
		return
	}
	filter, ok := s.releaseListFilter(w, r, access, paging, native)
	if !ok {
		return
	}
	result, err := s.db.Releases(r.Context(), access, filter)
	if err != nil {
		reportRelease(w, err)
		return
	}
	if native {
		writeJSON(w, 200, releaseNativePage(result.Data, paging, result.HasMore))
		return
	}
	s.releaseLinks(w, r, paging, result.HasPrevious, result.HasMore)
	data := make([]releaseResponse, 0, len(result.Data))
	for _, release := range result.Data {
		data = append(data, releaseJSON(release))
	}
	writeJSON(w, 200, data)
}

func (s *Server) release(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	version, ok := releaseVersion(w, r)
	if !ok {
		return
	}
	result, err := s.db.Release(r.Context(), access, version, r.PathValue("project"))
	if err != nil {
		reportRelease(w, err)
		return
	}
	writeJSON(w, 200, releaseJSON(result))
}

func (s *Server) createRelease(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	raw, ok := releaseBody(w, r)
	if !ok {
		return
	}
	fields, err := releaseObject(raw, "version", "projects", "ref", "url", "dateStarted", "dateReleased", "status", "commits")
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	text, err := releaseText(fields["version"], "version", 255, true)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	version, err := store.ParseReleaseVersion(text)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	projectRef := r.PathValue("project")
	var suppliedProjects []json.RawMessage
	metadataOnly := projectRef == "" && fields["status"] != nil && json.Unmarshal(fields["projects"], &suppliedProjects) == nil && suppliedProjects != nil && len(suppliedProjects) == 0
	var projects []string
	if !metadataOnly {
		projects, err = releaseProjectRefs(fields["projects"], projectRef == "")
		if err != nil {
			failure(w, 400, err.Error())
			return
		}
	}
	if projectRef != "" {
		project, err := s.db.ManagementProject(r.Context(), access, projectRef)
		if err != nil {
			reportRelease(w, err)
			return
		}
		if len(projects) > 1 {
			failure(w, 400, "A project release may include only its path project")
			return
		}
		if len(projects) == 1 {
			requested, err := s.db.ManagementProject(r.Context(), access, projects[0])
			if err != nil {
				reportRelease(w, err)
				return
			}
			if requested.ID != project.ID {
				failure(w, 400, "Release projects conflict with the project path")
				return
			}
		}
		projects = []string{project.Slug}
	}
	update, err := decodeReleaseUpdate(fields)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	var result store.Release
	created := false
	if metadataOnly {
		result, err = s.db.UpdateRelease(r.Context(), access, version, "", update)
	} else {
		result, created, err = s.db.CreateRelease(r.Context(), access, store.ReleaseCreate{Version: version, Projects: projects, ReleaseUpdate: update})
	}
	if err != nil {
		reportRelease(w, err)
		return
	}
	status := http.StatusAlreadyReported
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, releaseJSON(result))
}

func (s *Server) updateRelease(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	version, ok := releaseVersion(w, r)
	if !ok {
		return
	}
	raw, ok := releaseBody(w, r)
	if !ok {
		return
	}
	fields, err := releaseObject(raw, "projects", "ref", "url", "dateStarted", "dateReleased", "status", "commits")
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	update, err := decodeReleaseUpdate(fields)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	assertions, err := releaseProjectRefs(fields["projects"], false)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	if projectRef := r.PathValue("project"); projectRef != "" && assertions != nil {
		project, err := s.db.ManagementProject(r.Context(), access, projectRef)
		if err != nil {
			reportRelease(w, err)
			return
		}
		if len(assertions) != 1 {
			failure(w, 400, "Project assertions must identify only the path project")
			return
		}
		asserted, err := s.db.ManagementProject(r.Context(), access, assertions[0])
		if err != nil {
			reportRelease(w, err)
			return
		}
		if asserted.ID != project.ID {
			failure(w, 400, "Project assertions conflict with the project path")
			return
		}
		assertions = []string{project.Slug}
	}
	update.ProjectAssertions = assertions
	result, err := s.db.UpdateRelease(r.Context(), access, version, r.PathValue("project"), update)
	if err != nil {
		reportRelease(w, err)
		return
	}
	writeJSON(w, 200, releaseJSON(result))
}

func (s *Server) deleteRelease(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	version, ok := releaseVersion(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteRelease(r.Context(), access, version, r.PathValue("project")); err != nil {
		reportRelease(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func (s *Server) compatibleReleaseCommits(w http.ResponseWriter, r *http.Request) {
	s.listReleaseCommits(w, r, false)
}

func (s *Server) nativeReleaseCommits(w http.ResponseWriter, r *http.Request) {
	s.listReleaseCommits(w, r, true)
}

func (s *Server) listReleaseCommits(w http.ResponseWriter, r *http.Request, native bool) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	version, ok := releaseVersion(w, r)
	if !ok {
		return
	}
	paging, ok := releasePagination(w, r, native)
	if !ok {
		return
	}
	result, err := s.db.ReleaseCommits(r.Context(), access, version, r.PathValue("project"), paging.offset, paging.limit)
	if err != nil {
		reportRelease(w, err)
		return
	}
	if native {
		writeJSON(w, 200, releaseNativePage(result.Data, paging, result.HasMore))
		return
	}
	s.releaseLinks(w, r, paging, result.HasPrevious, result.HasMore)
	writeJSON(w, 200, result.Data)
}

func (s *Server) replaceReleaseCommits(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	version, ok := releaseVersion(w, r)
	if !ok {
		return
	}
	raw, ok := releaseBody(w, r)
	if !ok {
		return
	}
	commits, err := decodeReleaseCommits(raw)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	result, err := s.db.ReplaceReleaseCommits(r.Context(), access, version, commits)
	if err != nil {
		reportRelease(w, err)
		return
	}
	writeJSON(w, 200, releaseJSON(result))
}

func (s *Server) createReleaseDeploy(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	version, ok := releaseVersion(w, r)
	if !ok {
		return
	}
	raw, ok := releaseBody(w, r)
	if !ok {
		return
	}
	fields, err := releaseObject(raw, "environment", "name", "url", "dateStarted", "dateFinished", "projects")
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	input, err := decodeReleaseDeploy(fields)
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	result, err := s.db.CreateReleaseDeploy(r.Context(), access, version, input)
	if err != nil {
		reportRelease(w, err)
		return
	}
	writeJSON(w, 201, deploymentJSON(result))
}

func (s *Server) compatibleReleaseDeploys(w http.ResponseWriter, r *http.Request) {
	s.listReleaseDeploys(w, r, false)
}

func (s *Server) nativeReleaseDeploys(w http.ResponseWriter, r *http.Request) {
	s.listReleaseDeploys(w, r, true)
}

func (s *Server) listReleaseDeploys(w http.ResponseWriter, r *http.Request, native bool) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	version, ok := releaseVersion(w, r)
	if !ok {
		return
	}
	paging, ok := releasePagination(w, r, native)
	if !ok {
		return
	}
	result, err := s.db.ReleaseDeploys(r.Context(), access, version, paging.offset, paging.limit)
	if err != nil {
		reportRelease(w, err)
		return
	}
	if native {
		writeJSON(w, 200, releaseNativePage(result.Data, paging, result.HasMore))
		return
	}
	s.releaseLinks(w, r, paging, result.HasPrevious, result.HasMore)
	data := make([]deploymentResponse, 0, len(result.Data))
	for _, deploy := range result.Data {
		data = append(data, deploymentJSON(deploy))
	}
	writeJSON(w, 200, data)
}

func (s *Server) previousReleaseWithCommits(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	version, ok := releaseVersion(w, r)
	if !ok {
		return
	}
	result, err := s.db.PreviousReleaseWithCommits(r.Context(), access, version)
	if err != nil {
		reportRelease(w, err)
		return
	}
	writeJSON(w, 200, releaseJSON(result))
}

func (s *Server) releaseRepositories(w http.ResponseWriter, r *http.Request) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return
	}
	paging, ok := releasePagination(w, r, false)
	if !ok {
		return
	}
	query := paging.query.Get("query")
	if !credentialText(query, 200) {
		failure(w, 400, "Query must contain at most 200 characters without controls")
		return
	}
	result, err := s.db.ReleaseRepositories(r.Context(), access, query, paging.offset, paging.limit)
	if err != nil {
		reportRelease(w, err)
		return
	}
	s.releaseLinks(w, r, paging, result.HasPrevious, result.HasMore)
	writeJSON(w, 200, result.Data)
}

func (s *Server) createReleaseRepository(w http.ResponseWriter, r *http.Request) {
	access, valid := s.orgWriteAccess(w, r)
	if !valid {
		return
	}
	var input struct {
		Name     string          `json:"name"`
		URL      string          `json:"url"`
		Provider json.RawMessage `json:"provider"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	repository, err := s.db.CreateReleaseRepository(r.Context(), access, strings.TrimSpace(input.Name), strings.TrimSpace(input.URL))
	if errors.Is(err, store.ErrInvalidRepository) {
		failure(w, 400, "Repository name must contain 1 to 200 characters without controls")
		return
	}
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 201, repository)
}
