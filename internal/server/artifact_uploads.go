package server

import (
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"goodsentry/internal/artifact"
	"goodsentry/internal/store"
)

func (s *Server) registerArtifactRoutes(mux *routeMux) {
	root := "/api/0/organizations/{org}/"
	mux.Handle("GET "+root+"chunk-upload/{$}", s.compatible("project:releases", s.artifactUploadInfo))
	mux.Handle("POST "+root+"chunk-upload/{$}", s.compatible("project:releases", s.uploadArtifactChunks))
	mux.Handle("POST "+root+"artifactbundle/assemble/{$}", s.compatible("project:releases", s.assembleArtifactBundle))
}

func (s *Server) artifactAccess(w http.ResponseWriter, r *http.Request) (store.ManagementAccess, bool) {
	access, ok := s.managementAccess(w, r)
	if !ok {
		return access, false
	}
	if err := s.db.ArtifactUploadAccess(r.Context(), access); err != nil {
		reportArtifact(w, err)
		return access, false
	}
	return access, true
}

func (s *Server) artifactChunkLimits() (int64, int) {
	size := min(int64(1<<20), s.cfg.MaxBodyBytes/2)
	parts := min(int64(16), max(int64(1), s.cfg.MaxBodyBytes/(size+512)))
	return size, int(parts)
}

func (s *Server) artifactUploadInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.artifactAccess(w, r); !ok {
		return
	}
	size, parts := s.artifactChunkLimits()
	writeJSON(w, 200, map[string]any{
		"url":           s.cfg.Domain + "/api/0/organizations/" + url.PathEscape(r.PathValue("org")) + "/chunk-upload/",
		"hashAlgorithm": "sha1", "chunkSize": size, "chunksPerRequest": parts,
		"maxRequestSize": s.cfg.MaxBodyBytes, "maxFileSize": int64(64 << 20), "concurrency": 2,
		"compression": []string{"gzip"}, "accept": []string{"debug_files", "artifact_bundles"},
	})
}

func (s *Server) uploadArtifactChunks(w http.ResponseWriter, r *http.Request) {
	access, ok := s.artifactAccess(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	size, parts := s.artifactChunkLimits()
	chunks, err := readArtifactChunks(r, size, parts, s.cfg.MaxBodyBytes)
	if err != nil {
		var bodyLimit *http.MaxBytesError
		if errors.As(err, &bodyLimit) || errors.Is(err, store.ErrArtifactLimit) {
			failure(w, 413, "Artifact chunks exceed the configured upload budget")
		} else {
			failure(w, 400, err.Error())
		}
		return
	}
	policy := store.ArtifactStagingPolicy{MaxBytes: s.cfg.ArtifactStagingMaxBytes, TTL: time.Duration(s.cfg.ArtifactChunkTTLHours) * time.Hour}
	if err := s.db.SaveArtifactChunks(r.Context(), access, chunks, policy); err != nil {
		reportArtifact(w, err)
		return
	}
	writeJSON(w, 200, struct{}{})
}

// No part filename becomes a filesystem path. Check the original MIME parameter
// rather than Part.FileName, which removes client path components.
func readArtifactChunks(r *http.Request, maxChunk int64, maxParts int, maxInflated int64) ([]store.ArtifactChunk, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, errors.New("Expected multipart artifact chunks")
	}
	var chunks []store.ArtifactChunk
	var total int64
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Read multipart artifact chunks: %w", err)
		}
		if len(chunks) >= maxParts {
			return nil, store.ErrArtifactLimit
		}
		field := part.FormName()
		if field != "file" && field != "file_gzip" {
			return nil, errors.New("Artifact chunks must use file or file_gzip parts")
		}
		_, parameters, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if err != nil {
			return nil, errors.New("Invalid artifact chunk disposition")
		}
		checksum, ok := artifactChecksum(parameters["filename"])
		if !ok {
			return nil, errors.New("Artifact chunk filenames must be SHA-1 digests")
		}
		var source io.Reader = part
		var compressed *gzip.Reader
		if field == "file_gzip" {
			compressed, err = gzip.NewReader(part)
			if err != nil {
				return nil, errors.New("Invalid gzip artifact chunk")
			}
			source = compressed
		}
		data, readErr := io.ReadAll(io.LimitReader(source, maxChunk+1))
		if compressed != nil {
			_ = compressed.Close()
		}
		if readErr != nil {
			return nil, fmt.Errorf("Read artifact chunk: %w", readErr)
		}
		total += int64(len(data))
		if int64(len(data)) > maxChunk || total > maxInflated {
			return nil, store.ErrArtifactLimit
		}
		if len(data) == 0 || fmt.Sprintf("%x", sha1.Sum(data)) != checksum {
			return nil, errors.New("Artifact chunk digest does not match its content")
		}
		chunks = append(chunks, store.ArtifactChunk{Checksum: checksum, Data: data})
	}
	if len(chunks) == 0 {
		return nil, errors.New("Provide at least one artifact chunk")
	}
	return chunks, nil
}

func artifactChecksum(value string) (string, bool) {
	if len(value) != sha1.Size*2 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	return strings.ToLower(value), true
}

func (s *Server) assembleArtifactBundle(w http.ResponseWriter, r *http.Request) {
	access, ok := s.artifactAccess(w, r)
	if !ok {
		return
	}
	var input struct {
		Checksum *string   `json:"checksum"`
		Chunks   *[]string `json:"chunks"`
		Projects *[]string `json:"projects"`
		Version  *string   `json:"version"`
		Dist     *string   `json:"dist"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	if input.Checksum == nil || input.Chunks == nil || input.Projects == nil || len(*input.Chunks) == 0 || len(*input.Chunks) > 1000 || len(*input.Projects) == 0 || len(*input.Projects) > 1000 {
		failure(w, 400, "Provide a checksum, 1 to 1000 ordered chunks and 1 to 1000 projects")
		return
	}
	checksum, valid := artifactChecksum(*input.Checksum)
	if !valid {
		failure(w, 400, "Invalid artifact bundle checksum")
		return
	}
	for i, value := range *input.Chunks {
		digest, ok := artifactChecksum(value)
		if !ok {
			failure(w, 400, "Invalid artifact chunk checksum")
			return
		}
		(*input.Chunks)[i] = digest
	}
	for _, project := range *input.Projects {
		if project == "" || len(project) > 200 || !utf8.ValidString(project) || strings.ContainsRune(project, 0) {
			failure(w, 400, "Invalid artifact project reference")
			return
		}
	}
	if input.Version != nil {
		if _, err := store.ParseReleaseVersion(*input.Version); err != nil {
			failure(w, 400, err.Error())
			return
		}
	}
	if input.Dist != nil && !validArtifactDist(*input.Dist) {
		failure(w, 400, "Invalid artifact distribution")
		return
	}
	data, missing, err := s.db.ArtifactChunkData(r.Context(), access, *input.Projects, *input.Chunks, s.cfg.MaxBodyBytes)
	if err != nil {
		reportArtifact(w, err)
		return
	}
	if len(missing) > 0 {
		writeJSON(w, 200, map[string]any{"state": "not_found", "missingChunks": missing})
		return
	}
	if fmt.Sprintf("%x", sha1.Sum(data)) != checksum {
		artifactAssemblyError(w, errors.New("Artifact bundle checksum does not match its ordered chunks"))
		return
	}
	bundle, err := artifact.ParseBundle(data, s.cfg.MaxBodyBytes)
	if err != nil {
		reportBundle(w, err)
		return
	}
	if (input.Version != nil && bundle.Release != "" && *input.Version != bundle.Release) || (input.Dist != nil && bundle.Dist != "" && *input.Dist != bundle.Dist) {
		artifactAssemblyError(w, errors.New("Artifact bundle and request release/distribution must agree"))
		return
	}
	version, dist := bundle.Release, bundle.Dist
	if input.Version != nil {
		version = *input.Version
	}
	if input.Dist != nil {
		dist = *input.Dist
	}
	if version != "" {
		if _, err := store.ParseReleaseVersion(version); err != nil {
			artifactAssemblyError(w, err)
			return
		}
	}
	if !validArtifactDist(dist) {
		artifactAssemblyError(w, errors.New("Invalid artifact bundle distribution"))
		return
	}
	maps, err := artifact.BundleSourceMaps(bundle)
	if err != nil {
		reportBundle(w, err)
		return
	}
	for _, sourceMap := range maps {
		if version == "" && sourceMap.DebugID == "" {
			artifactAssemblyError(w, errors.New("Each source map needs a file DebugID or release"))
			return
		}
	}
	if err := s.db.SaveBundleSourceMaps(r.Context(), access, *input.Projects, store.ReleaseVersion(version), dist, maps, s.cfg.ArtifactMaxMapRecords); err != nil {
		reportArtifact(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"state": "ok", "missingChunks": []string{}})
}

func validArtifactDist(value string) bool {
	return len(value) <= 200 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func artifactAssemblyError(w http.ResponseWriter, err error) {
	detail := err.Error()
	if len(detail) > 512 {
		detail = detail[:512]
	}
	writeJSON(w, 200, map[string]any{"state": "error", "missingChunks": []string{}, "detail": detail})
}

func reportBundle(w http.ResponseWriter, err error) {
	if errors.Is(err, artifact.ErrBundleLimit) || errors.Is(err, artifact.ErrSourceMapLimit) {
		failure(w, 413, "Artifact bundle exceeds the configured processing budget")
		return
	}
	artifactAssemblyError(w, err)
}

func reportArtifact(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrArtifactLimit) {
		failure(w, 413, "Artifact upload exceeds the configured storage or processing budget")
		return
	}
	if errors.Is(err, store.ErrInvalidReleaseInput) {
		failure(w, 400, "Invalid artifact metadata")
		return
	}
	report(w, err)
}
