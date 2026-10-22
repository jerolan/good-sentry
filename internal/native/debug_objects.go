package native

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
)

const (
	maxDebugArchiveEntries = 256
	maxDebugArchiveBytes   = 128 << 20
	tarEndMarkerBytes      = 2 * 512
)

type DebugObject struct {
	Name      string
	DebugID   string
	Functions []SymbolFunction
	Unwind    *UnwindTable
}

// ParseDebugObjects expands bounded ZIP and TAR debug archives and otherwise
// parses the upload as one object. Unsupported or malformed content yields no
// objects so a project can retain it without making Minidump ingestion fail.
func ParseDebugObjects(name string, data []byte) []DebugObject {
	if isDebugZIP(data) {
		return parseDebugArchive(name, data)
	}
	if isDebugTAR(data) || isGzip(data) {
		return parseDebugTARArchive(data)
	}
	if object, ok := parseDebugObject(name, data); ok {
		return []DebugObject{object}
	}
	return nil
}

func isDebugZIP(data []byte) bool {
	return len(data) >= 4 && bytes.Equal(data[:4], []byte{'P', 'K', 0x03, 0x04})
}

func isDebugArchive(data []byte) bool {
	return isDebugZIP(data) || isDebugTAR(data) || isGzip(data)
}

func isDebugTAR(data []byte) bool {
	return len(data) >= 262 && bytes.Equal(data[257:262], []byte("ustar"))
}

func isGzip(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

func parseDebugTARArchive(data []byte) []DebugObject {
	var source io.Reader = bytes.NewReader(data)
	var compressed *gzip.Reader
	if isGzip(data) {
		var err error
		compressed, err = gzip.NewReader(source)
		if err != nil {
			return nil
		}
		source = compressed
	}
	limited := &io.LimitedReader{R: source, N: maxDebugArchiveBytes + 1}
	counted := &countingReader{Reader: limited}
	objects, err := parseDebugTAR(counted)
	if err == nil {
		err = drainTARPadding(limited)
	}
	if compressed != nil {
		if closeErr := compressed.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil || limited.N == 0 {
		return nil
	}
	return objects
}

func parseDebugTAR(source *countingReader) ([]DebugObject, error) {
	reader := tar.NewReader(source)
	objects := []DebugObject{}
	var expanded int64
	entries := 0
	for {
		beforeNext := source.count
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			// archive/tar also treats missing end markers as a clean end.
			if source.count-beforeNext < tarEndMarkerBytes {
				return nil, errors.New("TAR debug archive is missing its end blocks")
			}
			return objects, nil
		}
		if err != nil && !errors.Is(err, tar.ErrInsecurePath) {
			return nil, err
		}
		if header == nil {
			return nil, errors.New("TAR archive returned an empty member")
		}
		entries++
		if entries > maxDebugArchiveEntries || header.Size < 0 || header.Size > maxDebugArchiveBytes-expanded {
			return nil, errors.New("TAR debug archive exceeds processing limits")
		}
		expanded += header.Size
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			if _, err := io.Copy(io.Discard, reader); err != nil {
				return nil, err
			}
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil || int64(len(content)) != header.Size {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if object, ok := parseDebugObject(header.Name, content); ok {
			objects = append(objects, object)
		}
	}
}

type countingReader struct {
	io.Reader
	count int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.count += int64(n)
	return n, err
}

func drainTARPadding(source io.Reader) error {
	var buffer [32 * 1024]byte
	for {
		n, err := source.Read(buffer[:])
		for _, value := range buffer[:n] {
			if value != 0 {
				return errors.New("TAR debug archive has non-zero data after its end blocks")
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
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
	if isDebugArchive(data) {
		return DebugObject{}, false
	}
	if debugID, functions, ok := parsePDBSymbols(data); ok {
		return DebugObject{Name: moduleKey(name), DebugID: debugID, Functions: functions}, true
	}
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
