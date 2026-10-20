package server

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
)

func (s *Server) registerDebugFileRoutes(mux *http.ServeMux) {
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
	if _, valid := s.artifactAccess(w, r); !valid {
		return
	}
	failure(w, http.StatusNotImplemented, "Chunked debug-file assembly is not implemented")
}

func (s *Server) reprocessingDummy(w http.ResponseWriter, r *http.Request) {
	if _, valid := s.artifactAccess(w, r); !valid {
		return
	}
	writeJSON(w, 200, struct{}{})
}
