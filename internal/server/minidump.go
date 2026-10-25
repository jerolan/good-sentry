package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"goodsentry/internal/event"
	"goodsentry/internal/native"
	"goodsentry/internal/protocol"
)

func (s *Server) registerMinidumpRoutes(mux *routeMux) {
	mux.HandleFunc("POST /api/{project}/minidump/", s.minidump)
	mux.HandleFunc("OPTIONS /api/{project}/minidump/", s.preflight)
}

func (s *Server) minidump(w http.ResponseWriter, r *http.Request) {
	ingestCORS(w)
	key, err := protocol.Authenticate(r, nil, r.PathValue("project"))
	if err != nil {
		failure(w, http.StatusForbidden, "Invalid SDK authentication")
		return
	}
	valid, eventThrottleRate, err := s.db.ValidKeyWithEventThrottle(r.Context(), r.PathValue("project"), key)
	if err != nil {
		report(w, err)
		return
	}
	if !valid {
		failure(w, http.StatusForbidden, "Invalid SDK authentication")
		return
	}
	if shouldThrottleProjectEvent(eventThrottleRate) {
		rejectProjectThrottle(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	if err := r.ParseMultipartForm(min(s.cfg.MaxBodyBytes, int64(1<<20))); err != nil {
		failure(w, http.StatusBadRequest, "Malformed minidump request")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("upload_file_minidump")
	if err != nil {
		failure(w, http.StatusBadRequest, "upload_file_minidump is required")
		return
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, s.cfg.MaxBodyBytes+1))
	if err != nil || int64(len(contents)) > s.cfg.MaxBodyBytes {
		failure(w, http.StatusRequestEntityTooLarge, "Minidump exceeds the configured limit")
		return
	}
	crash, err := native.ParseMinidump(contents)
	if err != nil {
		failure(w, http.StatusBadRequest, "Invalid minidump")
		return
	}
	if err := s.symbolizeCrash(r.Context(), r.PathValue("project"), &crash); err != nil {
		report(w, err)
		return
	}
	fields := map[string]string{}
	if r.MultipartForm != nil {
		for name, values := range r.MultipartForm.Value {
			if name != "upload_file_minidump" && len(values) > 0 {
				fields[name] = values[0]
			}
		}
	}
	raw, eventID, err := native.BuildEvent(crash, fields, time.Now())
	if err != nil {
		report(w, err)
		return
	}
	eventValue, err := event.Normalize(json.RawMessage(raw), "", time.Now())
	if err != nil {
		report(w, err)
		return
	}
	if release := strings.TrimSpace(fields["sentry[release]"]); release != "" {
		eventValue.Release = release
	}
	if environment := strings.TrimSpace(fields["sentry[environment]"]); environment != "" {
		eventValue.Environment = environment
	}
	allowed, retry, err := s.db.AdmitProjectKey(r.Context(), r.PathValue("project"), key, 1)
	if err != nil {
		report(w, err)
		return
	}
	if !allowed {
		if retry < 1 {
			retry = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		w.Header().Set("X-Sentry-Rate-Limits", strconv.Itoa(retry)+":event:key:rate_limited")
		failure(w, http.StatusTooManyRequests, "SDK key rate limit exceeded")
		return
	}
	if _, _, err := s.db.Ingest(r.Context(), r.PathValue("project"), eventValue); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": eventID})
}

func (s *Server) symbolizeCrash(ctx context.Context, projectID string, crash *native.Crash) error {
	symbolFiles, err := s.db.DebugSymbolFiles(ctx, projectID)
	if err != nil {
		return err
	}
	files := make([]native.DebugSymbolFile, 0, len(symbolFiles))
	for _, file := range symbolFiles {
		files = append(files, native.DebugSymbolFile{Name: file.Name, DebugID: file.DebugID, Content: file.Content})
	}
	symbols, unwindTables := native.BuildSymbolTables(files)
	if frames := native.UnwindCFI(crash, unwindTables); len(frames) > len(crash.Frames) {
		crash.Frames = frames
	}
	native.Symbolicate(crash, symbols)
	return nil
}
