package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxBundleEntries       = 1000
	maxBundleManifestBytes = 1 << 20
)

// ErrBundleLimit identifies resource budgets separately from invalid content.
var ErrBundleLimit = errors.New("artifact bundle processing limit exceeded")

type BundleFile struct {
	Name    string
	Type    string
	URL     string
	DebugID string
	Headers map[string]string
	Data    []byte
}

type Bundle struct {
	DebugID string
	Release string
	Dist    string
	Files   []BundleFile
}

// ParseBundle reads the observed SYSB v2 artifact format entirely in memory.
// File debug IDs come from their manifest headers, independently of the bundle ID.
func ParseBundle(data []byte, maxExpandedBytes int64) (Bundle, error) {
	if maxExpandedBytes <= 0 || int64(len(data)) > maxExpandedBytes {
		return Bundle{}, fmt.Errorf("%w: input exceeds size limit", ErrBundleLimit)
	}
	if len(data) < 8 || string(data[:4]) != "SYSB" {
		return Bundle{}, errors.New("artifact bundle requires a SYSB header")
	}
	if binary.LittleEndian.Uint32(data[4:8]) != 2 {
		return Bundle{}, errors.New("unsupported artifact bundle version")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Bundle{}, fmt.Errorf("read artifact bundle ZIP: %w", err)
	}
	if len(archive.File) > maxBundleEntries {
		return Bundle{}, fmt.Errorf("%w: entry limit exceeded", ErrBundleLimit)
	}
	members := make(map[string][]byte, len(archive.File))
	var expanded int64
	for _, member := range archive.File {
		if !safeBundlePath(member.Name) || !member.Mode().IsRegular() {
			return Bundle{}, fmt.Errorf("unsafe artifact bundle member %q", member.Name)
		}
		if _, duplicate := members[member.Name]; duplicate {
			return Bundle{}, fmt.Errorf("duplicate artifact bundle member %q", member.Name)
		}
		limit := maxExpandedBytes - expanded
		if member.Name == "manifest.json" && limit > maxBundleManifestBytes {
			limit = maxBundleManifestBytes
		}
		payload, err := readBundleMember(member, limit)
		if err != nil {
			return Bundle{}, err
		}
		expanded += int64(len(payload))
		members[member.Name] = payload
	}
	manifest, exists := members["manifest.json"]
	if !exists {
		return Bundle{}, errors.New("artifact bundle is missing manifest.json")
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &metadata); err != nil || metadata == nil {
		return Bundle{}, errors.New("artifact bundle manifest must be a JSON object")
	}
	var files map[string]json.RawMessage
	if err := json.Unmarshal(metadata["files"], &files); err != nil || files == nil {
		return Bundle{}, errors.New("artifact bundle manifest requires a files object")
	}
	result := Bundle{Files: make([]BundleFile, 0, len(files))}
	if raw, exists := metadata["debug_id"]; exists {
		result.DebugID, err = bundleString(raw)
		if err != nil {
			return Bundle{}, fmt.Errorf("artifact bundle manifest debug_id: %w", err)
		}
	}
	for _, field := range []struct {
		name  string
		value *string
	}{{"release", &result.Release}, {"dist", &result.Dist}} {
		if raw, exists := metadata[field.name]; exists {
			var value *string
			if err := json.Unmarshal(raw, &value); err != nil {
				return Bundle{}, fmt.Errorf("artifact bundle manifest %s: expected a JSON string or null", field.name)
			}
			if value != nil {
				*field.value = *value
			}
		}
	}
	for name, raw := range files {
		if !safeBundlePath(name) {
			return Bundle{}, fmt.Errorf("unsafe artifact bundle manifest member %q", name)
		}
		payload, exists := members[name]
		if !exists {
			return Bundle{}, fmt.Errorf("artifact bundle manifest references missing member %q", name)
		}
		file, err := parseBundleFile(name, raw, payload)
		if err != nil {
			return Bundle{}, fmt.Errorf("artifact bundle manifest member %q: %w", name, err)
		}
		result.Files = append(result.Files, file)
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Name < result.Files[j].Name })
	return result, nil
}

func safeBundlePath(name string) bool {
	if !fs.ValidPath(name) || name == "." || strings.ContainsRune(name, '\\') {
		return false
	}
	return !(len(name) >= 2 && name[1] == ':' && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')))
}

func readBundleMember(member *zip.File, limit int64) ([]byte, error) {
	if limit < 0 || member.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%w: member %q exceeds expanded size limit", ErrBundleLimit, member.Name)
	}
	reader, err := member.Open()
	if err != nil {
		return nil, fmt.Errorf("open artifact bundle member %q: %w", member.Name, err)
	}
	readLimit := limit
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	payload, readErr := io.ReadAll(io.LimitReader(reader, readLimit))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read artifact bundle member %q: %w", member.Name, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close artifact bundle member %q: %w", member.Name, closeErr)
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("%w: member %q exceeds expanded size limit", ErrBundleLimit, member.Name)
	}
	return payload, nil
}

func parseBundleFile(name string, raw json.RawMessage, data []byte) (BundleFile, error) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		return BundleFile{}, errors.New("file metadata must be a JSON object")
	}
	file := BundleFile{Name: name, Data: data}
	var err error
	file.Type, err = bundleString(metadata["type"])
	if err != nil || file.Type == "" {
		return BundleFile{}, errors.New("file type must be a nonempty string")
	}
	if !utf8.Valid(metadata["url"]) {
		return BundleFile{}, errors.New("file URL must be valid UTF-8")
	}
	file.URL, err = bundleString(metadata["url"])
	if err != nil || file.URL == "" {
		return BundleFile{}, errors.New("file URL must be a nonempty string")
	}
	if rawHeaders, exists := metadata["headers"]; exists {
		var headers map[string]json.RawMessage
		if err := json.Unmarshal(rawHeaders, &headers); err != nil || headers == nil {
			return BundleFile{}, errors.New("file headers must be an object of strings")
		}
		file.Headers = make(map[string]string, len(headers))
		for name, raw := range headers {
			value, err := bundleString(raw)
			if err != nil {
				return BundleFile{}, fmt.Errorf("file header %q must be a string", name)
			}
			file.Headers[name] = value
		}
		file.DebugID = file.Headers["debug-id"]
	}
	if file.Type == "source_map" {
		if err := validateMap(data); err != nil {
			return BundleFile{}, fmt.Errorf("invalid source map: %w", err)
		}
	}
	return file, nil
}

func bundleString(raw json.RawMessage) (string, error) {
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "", errors.New("expected a JSON string")
	}
	return *value, nil
}
