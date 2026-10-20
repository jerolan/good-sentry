package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type DebugFile struct {
	ID          string            `json:"id"`
	UUID        *string           `json:"uuid"`
	DebugID     *string           `json:"debugId"`
	CPUName     string            `json:"cpuName"`
	ObjectName  string            `json:"objectName"`
	SymbolType  *string           `json:"symbolType"`
	Size        int64             `json:"size"`
	SHA1        string            `json:"sha1"`
	DateCreated time.Time         `json:"dateCreated"`
	Headers     map[string]string `json:"headers"`
	Data        map[string]any    `json:"data"`
}

const debugFileColumns = `id::text,symbol_type,debug_id,cpu_name,object_name,size,sha1,created_at,headers,data`

func scanDebugFile(row interface{ Scan(...any) error }) (DebugFile, error) {
	var file DebugFile
	var headers, data []byte
	err := row.Scan(&file.ID, &file.SymbolType, &file.DebugID, &file.CPUName, &file.ObjectName, &file.Size, &file.SHA1, &file.DateCreated, &headers, &data)
	if err != nil {
		return file, dbError(err)
	}
	file.UUID = &file.ID
	file.DateCreated = file.DateCreated.UTC()
	if json.Unmarshal(headers, &file.Headers) != nil || file.Headers == nil {
		file.Headers = map[string]string{}
	}
	if json.Unmarshal(data, &file.Data) != nil || file.Data == nil {
		file.Data = map[string]any{}
	}
	return file, nil
}

func validDebugFileText(value string, max int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsFunc(value, unicode.IsControl)
}

func validDebugFileSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (s *Store) DebugFiles(ctx context.Context, access ManagementAccess, projectRef string, offset, limit int) ([]DebugFile, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, errors.New("invalid debug-file page")
	}
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+debugFileColumns+` FROM debug_files
		WHERE project_id=$1::bigint ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, referenceID(project.ID), limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DebugFile{}
	for rows.Next() {
		file, err := scanDebugFile(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, file)
	}
	return result, rows.Err()
}

func (s *Store) SaveDebugFile(ctx context.Context, access ManagementAccess, projectRef, name, symbolType, debugID, cpuName, objectName, sha1Hash string, headers map[string]string, data map[string]any, content []byte) (DebugFile, error) {
	name = strings.TrimSpace(name)
	objectName = strings.TrimSpace(objectName)
	if !validDebugFileText(name, 512) || !validDebugFileText(objectName, 512) || len(content) == 0 || len(content) > 64<<20 || !validDebugFileSHA1(sha1Hash) {
		return DebugFile{}, errors.New("invalid debug file")
	}
	if symbolType != "" && !validDebugFileText(symbolType, 64) || debugID != "" && !validDebugFileText(debugID, 128) || cpuName != "" && !validDebugFileText(cpuName, 64) {
		return DebugFile{}, errors.New("invalid debug-file metadata")
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if data == nil {
		data = map[string]any{}
	}
	project, err := s.ManagementProject(ctx, access, projectRef)
	if err != nil {
		return DebugFile{}, err
	}
	var symbol, debug *string
	if symbolType != "" {
		symbol = &symbolType
	}
	if debugID != "" {
		debug = &debugID
	}
	if cpuName == "" {
		cpuName = "unknown"
	}
	headersJSON, _ := json.Marshal(headers)
	dataJSON, _ := json.Marshal(data)
	file, err := scanDebugFile(s.pool.QueryRow(ctx, `
  INSERT INTO debug_files(project_id,name,symbol_type,debug_id,cpu_name,object_name,size,sha1,headers,data,content)
  VALUES($1::bigint,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11)
  ON CONFLICT(project_id,sha1) DO NOTHING RETURNING `+debugFileColumns,
		referenceID(project.ID), name, symbol, debug, cpuName, objectName, len(content), sha1Hash, headersJSON, dataJSON, content))
	if !errors.Is(err, ErrNotFound) {
		return file, err
	}
	return scanDebugFile(s.pool.QueryRow(ctx, `SELECT `+debugFileColumns+` FROM debug_files WHERE project_id=$1::bigint AND sha1=$2`, referenceID(project.ID), sha1Hash))
}
