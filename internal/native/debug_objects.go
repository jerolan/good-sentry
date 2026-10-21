package native

import (
	"archive/zip"
	"bytes"
	"io"
)

const (
	maxDebugArchiveEntries = 256
	maxDebugArchiveBytes   = 128 << 20
)

type DebugObject struct {
	Name      string
	Functions []SymbolFunction
	Unwind    *UnwindTable
}

// ParseDebugObjects expands bounded ZIP debug archives and otherwise parses the
// upload as one object. Unsupported or malformed content yields no objects so a
// project can retain it without making Minidump ingestion fail.
func ParseDebugObjects(name string, data []byte) []DebugObject {
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{'P', 'K', 0x03, 0x04}) {
		return parseDebugArchive(name, data)
	}
	if object, ok := parseDebugObject(name, data); ok {
		return []DebugObject{object}
	}
	return nil
}

func parseDebugArchive(name string, data []byte) []DebugObject {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) == 0 || len(reader.File) > maxDebugArchiveEntries {
		return nil
	}
	objects := []DebugObject{}
	var total uint64
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() || entry.UncompressedSize64 == 0 {
			continue
		}
		if entry.UncompressedSize64 > maxDebugArchiveBytes-total {
			return nil
		}
		total += entry.UncompressedSize64
		file, err := entry.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(io.LimitReader(file, int64(entry.UncompressedSize64)+1))
		file.Close()
		if err != nil || uint64(len(content)) != entry.UncompressedSize64 {
			continue
		}
		if object, ok := parseDebugObject(entry.Name, content); ok {
			objects = append(objects, object)
		}
	}
	return objects
}

func parseDebugObject(name string, data []byte) (DebugObject, bool) {
	key, functions := ParseObjectSymbols(name, data)
	_, unwind := ParseObjectUnwind(name, data)
	if len(functions) != 0 || unwind != nil {
		return DebugObject{Name: key, Functions: functions, Unwind: unwind}, true
	}
	key, functions = ParseBreakpad(name, data)
	if len(functions) == 0 {
		return DebugObject{}, false
	}
	return DebugObject{Name: key, Functions: functions}, true
}
