package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"goodsentry/internal/artifact"
)

func (s *Server) sourceMaps(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.ListSourceMaps(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"))
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (s *Server) uploadSourceMap(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	values, data, err := sourceMapUpload(r)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			failure(w, 413, "Source map upload exceeds the configured size limit")
		} else {
			failure(w, 400, err.Error())
		}
		return
	}
	name, release, dist := values["name"], values["release"], values["dist"]
	debugID := strings.ToLower(strings.ReplaceAll(values["debugId"], "-", ""))
	_, idErr := hex.DecodeString(debugID)
	if name == "" || len(name) > 2048 || len(release) > 200 || len(dist) > 200 ||
		(release == "" && debugID == "") || (debugID != "" && (len(debugID) != 32 || idErr != nil)) {
		failure(w, 400, "Provide an artifact name and either a release or a valid debug ID")
		return
	}
	if err := artifact.ValidateSourceMap(name, data); err != nil {
		if errors.Is(err, artifact.ErrSourceMapLimit) {
			failure(w, 413, "Source map exceeds the configured processing limit")
		} else {
			failure(w, 400, "Invalid source map: "+err.Error())
		}
		return
	}
	file, created, err := s.db.SaveSourceMap(r.Context(), current(r).User.ID, r.PathValue("org"), r.PathValue("project"), name, release, dist, debugID, data)
	if err != nil {
		report(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"artifact": file, "created": created})
}

// Parse a bounded multipart body in memory. Client filenames never become paths.
func sourceMapUpload(r *http.Request) (map[string]string, []byte, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, errors.New("Expected a multipart source map upload")
	}
	values := map[string]string{}
	var data []byte
	fileSeen := false
	for count := 0; ; count++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("Read multipart upload: %w", err)
		}
		if count >= 16 {
			return nil, nil, errors.New("Too many upload fields")
		}
		name := part.FormName()
		if name == "file" {
			if fileSeen {
				return nil, nil, errors.New("Expected one source map file")
			}
			fileSeen = true
			data, err = io.ReadAll(part)
		} else {
			if name != "name" && name != "release" && name != "dist" && name != "debugId" {
				return nil, nil, errors.New("Unknown source map upload field")
			}
			if _, duplicate := values[name]; duplicate {
				return nil, nil, errors.New("Duplicate source map upload field")
			}
			var value []byte
			value, err = io.ReadAll(io.LimitReader(part, 4097))
			if len(value) > 4096 {
				return nil, nil, errors.New("Source map metadata is too long")
			}
			if !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 {
				return nil, nil, errors.New("Source map metadata requires UTF-8 text without null bytes")
			}
			values[name] = strings.TrimSpace(string(value))
		}
		if err != nil {
			return nil, nil, fmt.Errorf("Read source map upload: %w", err)
		}
		if err := part.Close(); err != nil {
			return nil, nil, fmt.Errorf("Close upload field: %w", err)
		}
	}
	if !fileSeen || len(data) == 0 {
		return nil, nil, errors.New("Provide a nonempty source map file")
	}
	return values, data, nil
}

func (s *Server) mapEvent(ctx context.Context, projectID string, raw json.RawMessage) (json.RawMessage, error) {
	var metadata struct {
		Release       json.RawMessage `json:"release"`
		Dist          json.RawMessage `json:"dist"`
		DebugMeta     json.RawMessage `json:"debug_meta"`
		Exception     json.RawMessage `json:"exception"`
		Stacktrace    json.RawMessage `json:"stacktrace"`
		RawStacktrace json.RawMessage `json:"raw_stacktrace"`
	}
	// Event normalization owns malformed event errors and optional field shapes.
	if json.Unmarshal(raw, &metadata) != nil {
		return raw, nil
	}
	if len(metadata.Exception) == 0 && len(metadata.Stacktrace) == 0 && len(metadata.RawStacktrace) == 0 {
		return raw, nil
	}
	var release, dist string
	_ = json.Unmarshal(metadata.Release, &release)
	_ = json.Unmarshal(metadata.Dist, &dist)
	var images struct {
		Images []struct {
			Type    string `json:"type"`
			DebugID string `json:"debug_id"`
		} `json:"images"`
	}
	_ = json.Unmarshal(metadata.DebugMeta, &images)
	ids := []string{}
	for _, image := range images.Images {
		if image.Type == "sourcemap" && image.DebugID != "" {
			ids = append(ids, image.DebugID)
		}
	}
	if release == "" && len(ids) == 0 {
		return raw, nil
	}
	maps, err := s.db.SourceMaps(ctx, projectID, release, dist, ids, s.cfg.MaxBodyBytes)
	if err != nil || len(maps) == 0 {
		return raw, err
	}
	return artifact.Symbolicate(raw, maps)
}
