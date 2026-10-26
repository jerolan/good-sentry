package artifact

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// BundleSourceMaps connects file identities and script URLs without fetching
// either. Source map URLs remain the base for resolving original sources.
func BundleSourceMaps(bundle Bundle) ([]SourceMap, error) {
	maps := make([]SourceMap, 0)
	for _, file := range bundle.Files {
		if !bundleSourceMapFile(file) {
			continue
		}
		if err := bundleArtifactURL(file.URL); err != nil {
			return nil, err
		}
		id, err := bundleFileDebugID(file)
		if err != nil {
			return nil, err
		}
		maps = append(maps, SourceMap{Name: file.URL, DebugID: id, Data: file.Data})
	}
	type scriptLink struct {
		url, id string
		header  []int
	}
	links := make([]scriptLink, 0)
	for _, file := range bundle.Files {
		if file.Type != "minified_source" {
			continue
		}
		if bundleSourceMapFile(file) {
			continue
		}
		id, err := bundleFileDebugID(file)
		if err != nil {
			return nil, err
		}
		link := scriptLink{url: file.URL, id: id}
		if reference := file.Headers["sourcemap"]; reference != "" {
			if resolved, ok := bundleMapReference(file.URL, reference); ok {
				for i := range maps {
					if maps[i].Name == resolved {
						link.header = append(link.header, i)
					}
				}
			}
		}
		links = append(links, link)
	}
	matchingIDs := func(id string) []int {
		var found []int
		for i := range maps {
			if id != "" && maps[i].DebugID == id {
				found = append(found, i)
			}
		}
		return found
	}
	// Establish inherited identities before linking scripts that carry only an ID.
	for _, link := range links {
		if len(link.header) != 1 || link.id == "" {
			continue
		}
		i := link.header[0]
		ids := matchingIDs(link.id)
		if maps[i].DebugID != "" && maps[i].DebugID != link.id || len(ids) == 1 && ids[0] != i {
			return nil, errors.New("source-map header and file debug IDs disagree")
		}
		if len(ids) <= 1 {
			maps[i].DebugID = link.id
		}
	}
	for _, link := range links {
		ids := matchingIDs(link.id)
		if len(link.header) > 1 || len(ids) > 1 {
			continue
		}
		i := -1
		if len(link.header) == 1 {
			i = link.header[0]
		}
		if len(ids) == 1 {
			if i != -1 && i != ids[0] {
				return nil, errors.New("source-map header and file debug IDs disagree")
			}
			i = ids[0]
		}
		if i != -1 {
			if err := bundleArtifactURL(link.url); err != nil {
				return nil, err
			}
			maps[i].CodeFiles = append(maps[i].CodeFiles, link.url)
		}
	}
	for i := range maps {
		slices.Sort(maps[i].CodeFiles)
		maps[i].CodeFiles = slices.Compact(maps[i].CodeFiles)
	}
	return maps, nil
}

// bundleSourceMapFile accepts the explicit type and the emitted CLI form, which
// labels a bundled .map entry as a minified source.
func bundleSourceMapFile(file BundleFile) bool {
	if file.Type == "source_map" {
		return true
	}
	return file.Type == "minified_source" && strings.HasSuffix(strings.ToLower(file.URL), ".map")
}

func bundleArtifactURL(value string) error {
	if value == "" || len(value) > 2048 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return errors.New("artifact URL must be valid UTF-8, at most 2048 bytes and free of NUL")
	}
	return nil
}

func bundleFileDebugID(file BundleFile) (string, error) {
	values := []string{file.DebugID, file.Headers["debug-id"]}
	if file.Type == "source_map" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(file.Data, &fields); err != nil || fields == nil {
			return "", errors.New("source map must be a JSON object")
		}
		for _, field := range []string{"debug_id", "debugId"} {
			if raw, exists := fields[field]; exists {
				var value *string
				if err := json.Unmarshal(raw, &value); err != nil {
					return "", fmt.Errorf("source map %s must be a string or null", field)
				}
				if value != nil {
					values = append(values, *value)
				}
			}
		}
	}
	var id string
	for _, value := range values {
		if value == "" {
			continue
		}
		normalized := normalizeDebugID(value)
		if len(normalized) != 32 {
			return "", errors.New("file debug ID must contain 32 hexadecimal digits")
		}
		if _, err := hex.DecodeString(normalized); err != nil {
			return "", errors.New("file debug ID must contain 32 hexadecimal digits")
		}
		if id != "" && id != normalized {
			return "", errors.New("source-map file debug IDs disagree")
		}
		id = normalized
	}
	return id, nil
}

func bundleMapReference(script, reference string) (string, bool) {
	if strings.HasPrefix(reference, "~/") {
		return reference, true
	}
	base, err := url.Parse(script)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(reference)
	if err != nil {
		return "", false
	}
	alias := strings.HasPrefix(script, "~/")
	if alias {
		base.Path = strings.TrimPrefix(base.Path, "~")
		base.RawPath = strings.TrimPrefix(base.RawPath, "~")
	}
	resolved := base.ResolveReference(ref)
	if alias && !ref.IsAbs() && ref.Host == "" {
		resolved.Path = "~" + resolved.Path
		if resolved.RawPath != "" {
			resolved.RawPath = "~" + resolved.RawPath
		}
	} else if !base.IsAbs() && base.Host == "" && !strings.HasPrefix(base.Path, "/") && !ref.IsAbs() && ref.Host == "" && !strings.HasPrefix(ref.Path, "/") {
		resolved.Path = strings.TrimPrefix(resolved.Path, "/")
		resolved.RawPath = strings.TrimPrefix(resolved.RawPath, "/")
	}
	return resolved.String(), true
}
