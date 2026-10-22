package server

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"goodsentry/internal/store"
)

func (s *Server) registerDebugFileRoutes(mux *routeMux) {
	root := "/api/0/projects/{org}/{project}/files/"
	mux.Handle("GET "+root+"dsyms/{$}", s.compatible("project:releases", s.debugFiles))
	mux.Handle("POST "+root+"dsyms/{$}", s.compatible("project:releases", s.uploadDebugFile))
	mux.Handle("POST "+root+"difs/assemble/{$}", s.compatible("project:releases", s.assembleDebugFiles))
	mux.Handle("POST /api/0/projects/{org}/{project}/reprocessing/{$}", s.compatible("project:releases", s.reprocessingDummy))
}

func (s *Server) debugFiles(w http.ResponseWriter, r *http.Request) {
	access, valid := s.managementAccess(w, r)
	if !valid {
		return
	}
	q, valid := credentialQuery(w, r)
	if !valid || !discoveryQueryKeys(w, q, "limit", "per_page", "cursor") {
		return
	}
	offset, limit, valid := discoveryPage(w, q)
	if !valid {
		return
	}
	files, err := s.db.DebugFiles(r.Context(), access, r.PathValue("project"), offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	next := len(files) > limit
	if next {
		files = files[:limit]
	}
	s.discoveryLinks(w, r, q, offset, limit, next)
	writeJSON(w, 200, files)
}

func (s *Server) uploadDebugFile(w http.ResponseWriter, r *http.Request) {
	access, valid := s.artifactAccess(w, r)
	if !valid {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		failure(w, 400, "Expected a multipart debug file")
		return
	}
	var name, symbolType, debugID, cpuName, objectName string
	var content []byte
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			var limit *http.MaxBytesError
			if errors.As(nextErr, &limit) {
				failure(w, 413, "Debug file exceeds the configured upload limit")
			} else {
				failure(w, 400, "Invalid multipart debug file")
			}
			return
		}
		filename := part.FileName()
		if filename == "" {
			field, readErr := io.ReadAll(io.LimitReader(part, 4096))
			part.Close()
			if readErr != nil {
				failure(w, 400, "Invalid debug-file form")
				return
			}
			value := strings.TrimSpace(string(field))
			switch part.FormName() {
			case "type":
				symbolType = value
			case "debug_id":
				debugID = value
			case "cpu_name":
				cpuName = value
			case "object_name":
				objectName = value
			}
			continue
		}
		if content != nil {
			part.Close()
			failure(w, 400, "Only one debug file may be uploaded")
			return
		}
		name = path.Base(strings.ReplaceAll(filename, "\\", "/"))
		content, err = io.ReadAll(io.LimitReader(part, s.cfg.MaxBodyBytes+1))
		part.Close()
		if err != nil {
			failure(w, 413, "Debug file exceeds the configured upload limit")
			return
		}
		if int64(len(content)) > s.cfg.MaxBodyBytes {
			failure(w, 413, "Debug file exceeds the configured upload limit")
			return
		}
	}
	if content == nil {
		failure(w, 400, "A debug file is required")
		return
	}
	if name == "" || name == "." || name == "/" {
		name = "debug-file"
	}
	if symbolType == "" {
		symbolType = strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	}
	if objectName == "" {
		objectName = name
	}
	hash := sha1.Sum(content)
	file, err := s.db.SaveDebugFile(r.Context(), access, r.PathValue("project"), name, symbolType, debugID, cpuName, objectName, hex.EncodeToString(hash[:]), nil, nil, content)
	if err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, file)
}

func (s *Server) assembleDebugFiles(w http.ResponseWriter, r *http.Request) {
	access, valid := s.artifactAccess(w, r)
	if !valid {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	var entries map[string]struct {
		Name    string   `json:"name"`
		DebugID string   `json:"debug_id"`
		Chunks  []string `json:"chunks"`
	}
	if !bodyJSON(w, r, &entries) || len(entries) == 0 || len(entries) > 100 {
		if entries != nil {
			failure(w, 400, "Provide 1 to 100 debug-file entries")
		}
		return
	}
	maximum := min(int64(64<<20), s.cfg.MaxBodyBytes)
	type result struct {
		State         string           `json:"state"`
		MissingChunks []string         `json:"missingChunks"`
		Detail        string           `json:"detail,omitempty"`
		DIF           *store.DebugFile `json:"dif,omitempty"`
	}
	results := map[string]result{}
	for checksum, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" || len(entry.Name) > 512 || len(entry.DebugID) > 128 || len(entry.Chunks) == 0 || len(entry.Chunks) > 1000 {
			failure(w, 400, "Invalid debug-file assembly metadata")
			return
		}
		if _, valid := artifactChecksum(checksum); !valid {
			failure(w, 400, "Invalid debug-file checksum")
			return
		}
		for i, chunk := range entry.Chunks {
			value, ok := artifactChecksum(chunk)
			if !ok {
				failure(w, 400, "Invalid debug-file chunk checksum")
				return
			}
			entry.Chunks[i] = value
		}
		data, missing, err := s.db.ArtifactChunkData(r.Context(), access, []string{r.PathValue("project")}, entry.Chunks, maximum)
		if err != nil {
			reportArtifact(w, err)
			return
		}
		if len(missing) != 0 {
			results[checksum] = result{State: "not_found", MissingChunks: missing, Detail: "Some chunks are missing"}
			continue
		}
		if expected, _ := hex.DecodeString(checksum); sha1.Sum(data) != [20]byte(expected) {
			failure(w, 400, "Assembled debug-file checksum does not match")
			return
		}
		name := path.Base(strings.ReplaceAll(entry.Name, "\\", "/"))
		symbolType := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
		file, err := s.db.SaveDebugFile(r.Context(), access, r.PathValue("project"), name, symbolType, entry.DebugID, "unknown", name, checksum, nil, nil, data)
		if err != nil {
			report(w, err)
			return
		}
		results[checksum] = result{State: "ok", MissingChunks: []string{}, DIF: &file}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) reprocessingDummy(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.artifactAccess(w, r); !valid {
		return
	}
	writeJSON(w, 200, struct{}{})
}
