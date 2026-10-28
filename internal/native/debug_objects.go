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
	maxDebugArchiveDepth   = 8
	tarEndMarkerBytes      = 2 * 512
)

type debugArchiveBudget struct {
	entries       int
	expandedBytes uint64
}

func (b *debugArchiveBudget) addEntry(size uint64) error {
	if b.entries >= maxDebugArchiveEntries || size > uint64(maxDebugArchiveBytes)-b.expandedBytes {
		return errors.New("debug archive exceeds processing limits")
	}
	b.entries++
	b.expandedBytes += size
	return nil
}

type DebugObject struct {
	Name      string
	DebugID   string
	Functions []SymbolFunction
	Unwind    *UnwindTable

	// Only a format reader can establish an equivalent identity. A generic
	// 32-character ID could instead be an ELF build ID with no PDB age.
	debugIDAlias string
}

// ParseDebugObjects expands bounded ZIP and TAR debug archives and otherwise
// parses the upload as one object. Unsupported or malformed content yields no
// objects so a project can retain it without making Minidump ingestion fail.
func ParseDebugObjects(name string, data []byte) []DebugObject {
	if isDebugArchive(data) {
		objects, err := parseDebugArchive(data, 1, &debugArchiveBudget{})
		if err != nil {
			return nil
		}
		return objects
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

func parseDebugArchive(data []byte, depth int, budget *debugArchiveBudget) ([]DebugObject, error) {
	if depth < 1 || depth > maxDebugArchiveDepth {
		return nil, errors.New("debug archive nesting exceeds processing limits")
	}
	if isDebugZIP(data) {
		return parseDebugZIPArchive(data, depth, budget)
	}
	if isDebugTAR(data) || isGzip(data) {
		return parseDebugTARArchive(data, depth, budget)
	}
	return nil, errors.New("unsupported debug archive")
}

func parseDebugArchiveEntry(name string, data []byte, depth int, budget *debugArchiveBudget) ([]DebugObject, error) {
	if !isDebugArchive(data) {
		if object, ok := parseDebugObject(name, data); ok {
			return []DebugObject{object}, nil
		}
		return nil, nil
	}
	if depth >= maxDebugArchiveDepth {
		return nil, errors.New("debug archive nesting exceeds processing limits")
	}
	return parseDebugArchive(data, depth+1, budget)
}

func parseDebugTARArchive(data []byte, depth int, budget *debugArchiveBudget) ([]DebugObject, error) {
	var source io.Reader = bytes.NewReader(data)
	var compressed *gzip.Reader
	if isGzip(data) {
		var err error
		compressed, err = gzip.NewReader(source)
		if err != nil {
			return nil, err
		}
		source = compressed
	}
	limited := &io.LimitedReader{R: source, N: maxDebugArchiveBytes + 1}
	counted := &countingReader{Reader: limited}
	objects, err := parseDebugTAR(counted, depth, budget)
	if err == nil {
		err = drainTARPadding(limited)
	}
	if compressed != nil {
		if closeErr := compressed.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil || limited.N == 0 {
		if err == nil {
			err = errors.New("TAR debug archive exceeds processing limits")
		}
		return nil, err
	}
	return objects, nil
}

func parseDebugTAR(source *countingReader, depth int, budget *debugArchiveBudget) ([]DebugObject, error) {
	reader := tar.NewReader(source)
	objects := []DebugObject{}
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
		if header.Size < 0 {
			return nil, errors.New("TAR debug archive has an invalid member size")
		}
		if err := budget.addEntry(uint64(header.Size)); err != nil {
			return nil, err
		}
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
		members, err := parseDebugArchiveEntry(header.Name, content, depth, budget)
		if err != nil {
			return nil, err
		}
		objects = append(objects, members...)
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

func parseDebugZIPArchive(data []byte, depth int, budget *debugArchiveBudget) ([]DebugObject, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) == 0 || len(reader.File) > maxDebugArchiveEntries {
		if err == nil {
			err = errors.New("ZIP debug archive exceeds processing limits")
		}
		return nil, err
	}
	objects := []DebugObject{}
	for _, entry := range reader.File {
		if err := budget.addEntry(entry.UncompressedSize64); err != nil {
			return nil, err
		}
		if entry.FileInfo().IsDir() || entry.UncompressedSize64 == 0 {
			continue
		}
		file, err := entry.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(file, int64(entry.UncompressedSize64)+1))
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if uint64(len(content)) != entry.UncompressedSize64 {
			return nil, io.ErrUnexpectedEOF
		}
		members, err := parseDebugArchiveEntry(entry.Name, content, depth, budget)
		if err != nil {
			return nil, err
		}
		objects = append(objects, members...)
	}
	return objects, nil
}

func parseDebugObject(name string, data []byte) (DebugObject, bool) {
	if isDebugArchive(data) {
		return DebugObject{}, false
	}
	if debugID, functions, ok := parseSymCache(data); ok {
		object := DebugObject{Name: moduleKey(name), DebugID: debugID, Functions: functions}
		// This reader emits 32 characters only for UUID-form DebugIds with
		// appendix0; the equivalent Breakpad representation includes that0.
		if len(debugID) == 32 {
			object.debugIDAlias = debugID + "0"
		}
		return object, true
	}
	if bytes.HasPrefix(data, msf7Magic) || bytes.HasPrefix(data, msf2Magic) {
		debugID, functions, ok := parsePDBSymbols(data)
		if !ok {
			return DebugObject{}, false
		}
		return DebugObject{Name: moduleKey(name), DebugID: debugID, Functions: functions}, true
	}
	key, functions := ParseObjectSymbols(name, data)
	_, unwind := ParseObjectUnwind(name, data)
	if len(functions) != 0 || unwind != nil {
		object := DebugObject{Name: key, DebugID: ObjectDebugID(data), Functions: functions, Unwind: unwind}
		if len(data) >= 4 && isMachOMagic(data[:4]) && len(object.DebugID) == 32 {
			object.debugIDAlias = object.DebugID + "0"
		}
		return object, true
	}
	key, functions = ParseBreakpad(name, data)
	if len(functions) == 0 {
		return DebugObject{}, false
	}
	return DebugObject{Name: key, Functions: functions}, true
}
