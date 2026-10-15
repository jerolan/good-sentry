package artifact_test

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"goodsentry/internal/artifact"
)

const bundleMap = `{"version":3,"sources":["src/app.js"],"names":[],"mappings":"AAAA","sourcesContent":["throw Error('fixture');"]}`

func TestParseBundlePreservesFilesAndSeparatesTheirDebugIDs(t *testing.T) {
	manifest := `{"debug_id":"bundle-id","release":" Release / 1 ","dist":"web","future":{"kept":true},"files":{"files/z.js":{"type":"minified_source","url":"~/app.js","headers":{"debug-id":"file-id","sourcemap":"app.js.map"},"future":42},"files/a.js.map":{"type":"source_map","url":"~/app.js.map","headers":{}},"files/future.bin":{"type":"future_blob","url":"~/payload.bin"}}}`
	data := makeBundle(t, manifest, []bundleMember{
		{name: "files/z.js", data: "throw Error('fixture');"},
		{name: "files/future.bin", data: "\x00\n\xff"},
		{name: "files/a.js.map", data: bundleMap},
	})
	got, err := artifact.ParseBundle(data, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := artifact.Bundle{DebugID: "bundle-id", Release: " Release / 1 ", Dist: "web", Files: []artifact.BundleFile{
		{Name: "files/a.js.map", Type: "source_map", URL: "~/app.js.map", Headers: map[string]string{}, Data: []byte(bundleMap)},
		{Name: "files/future.bin", Type: "future_blob", URL: "~/payload.bin", Data: []byte("\x00\n\xff")},
		{Name: "files/z.js", Type: "minified_source", URL: "~/app.js", DebugID: "file-id", Headers: map[string]string{"debug-id": "file-id", "sourcemap": "app.js.map"}, Data: []byte("throw Error('fixture');")},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded bundle = %#v; want %#v", got, want)
	}
}

func TestParseBundleRejectsMalformedInputAndManifest(t *testing.T) {
	valid := makeBundle(t, `{"files":{}}`, nil)
	version := bytes.Clone(valid)
	binary.LittleEndian.PutUint32(version[4:8], 3)
	cases := map[string][]byte{
		"truncated header": []byte("SYSB"),
		"wrong magic":      append([]byte("NOPE"), valid[4:]...),
		"wrong version":    version,
		"invalid zip":      append([]byte("SYSB\x02\x00\x00\x00"), []byte("not a ZIP")...),
		"missing manifest": makeBundle(t, "", []bundleMember{{name: "files/a", data: "data"}}),
	}
	for name, manifest := range map[string]string{
		"invalid JSON":           `{"files":`,
		"non-object manifest":    `[]`,
		"null manifest":          `null`,
		"missing files":          `{}`,
		"null files":             `{"files":null}`,
		"files array":            `{"files":[]}`,
		"non-string bundle ID":   `{"debug_id":12,"files":{}}`,
		"null bundle ID":         `{"debug_id":null,"files":{}}`,
		"non-object file":        `{"files":{"files/a":null}}`,
		"missing type":           `{"files":{"files/a":{"url":"~/a"}}}`,
		"non-string type":        `{"files":{"files/a":{"type":12,"url":"~/a"}}}`,
		"empty type":             `{"files":{"files/a":{"type":"","url":"~/a"}}}`,
		"missing URL":            `{"files":{"files/a":{"type":"source"}}}`,
		"null URL":               `{"files":{"files/a":{"type":"source","url":null}}}`,
		"non-string URL":         `{"files":{"files/a":{"type":"source","url":false}}}`,
		"empty URL":              `{"files":{"files/a":{"type":"source","url":""}}}`,
		"non-object headers":     `{"files":{"files/a":{"type":"source","url":"~/a","headers":[]}}}`,
		"null headers":           `{"files":{"files/a":{"type":"source","url":"~/a","headers":null}}}`,
		"non-string header":      `{"files":{"files/a":{"type":"source","url":"~/a","headers":{"debug-id":12}}}}`,
		"null header":            `{"files":{"files/a":{"type":"source","url":"~/a","headers":{"debug-id":null}}}}`,
		"missing member":         `{"files":{"files/missing":{"type":"source","url":"~/a"}}}`,
		"invalid source map":     `{"files":{"files/a":{"type":"source_map","url":"~/a.map"}}}`,
		"unsafe manifest member": `{"files":{"../a":{"type":"source","url":"~/a"}}}`,
	} {
		cases[name] = makeBundle(t, manifest, []bundleMember{{name: "files/a", data: "not a source map"}})
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := artifact.ParseBundle(data, 1<<20); err == nil {
				t.Fatal("accepted malformed bundle")
			}
		})
	}
}

func TestParseBundleRejectsUnsafeAndDuplicateArchiveMembers(t *testing.T) {
	for _, name := range []string{"../escape", "files/../escape", "/absolute", "files\\escape", "C:/absolute", "files/./alias", "files//alias", ""} {
		t.Run(name, func(t *testing.T) {
			data := makeBundle(t, `{"files":{}}`, []bundleMember{{name: name, data: "x"}})
			if _, err := artifact.ParseBundle(data, 1<<20); err == nil {
				t.Fatalf("accepted unsafe archive member %q", name)
			}
		})
	}
	for name, members := range map[string][]bundleMember{
		"duplicate file":     {{name: "files/a", data: "a"}, {name: "files/a", data: "b"}},
		"duplicate manifest": {{name: "manifest.json", data: `{"files":{}}`}},
		"directory":          {{name: "files/", mode: fs.ModeDir | 0o755}},
		"symlink":            {{name: "files/link", data: "../outside", mode: fs.ModeSymlink | 0o777}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := artifact.ParseBundle(makeBundle(t, `{"files":{}}`, members), 1<<20); err == nil {
				t.Fatal("accepted duplicate or non-regular archive member")
			}
		})
	}
}

func TestParseBundleEnforcesInputAndExpansionLimits(t *testing.T) {
	t.Run("input", func(t *testing.T) {
		data := makeBundle(t, `{"files":{}}`, nil)
		for _, limit := range []int64{-1, 0, int64(len(data) - 1)} {
			if _, err := artifact.ParseBundle(data, limit); !errors.Is(err, artifact.ErrBundleLimit) {
				t.Fatalf("input budget must return ErrBundleLimit: %v", err)
			}
		}
	})
	for name, members := range map[string][]bundleMember{
		"member expansion": {{name: "files/a", data: strings.Repeat("a", 6000)}},
		"total expansion":  {{name: "files/a", data: strings.Repeat("a", 3000)}, {name: "files/b", data: strings.Repeat("b", 3000)}},
	} {
		t.Run(name, func(t *testing.T) {
			data := makeBundle(t, `{"files":{}}`, members)
			if len(data) >= 5000 {
				t.Fatal("fixture must pass the compressed input limit")
			}
			if _, err := artifact.ParseBundle(data, 5000); !errors.Is(err, artifact.ErrBundleLimit) {
				t.Fatalf("expanded budget must return ErrBundleLimit: %v", err)
			}
		})
	}
	t.Run("manifest expansion", func(t *testing.T) {
		manifest := `{"files":{},"future":"` + strings.Repeat("a", 1<<20) + `"}`
		data := makeBundle(t, manifest, nil)
		if _, err := artifact.ParseBundle(data, 2<<20); !errors.Is(err, artifact.ErrBundleLimit) {
			t.Fatalf("manifest budget must return ErrBundleLimit: %v", err)
		}
	})
	t.Run("entry count", func(t *testing.T) {
		members := make([]bundleMember, 1000)
		for i := range members {
			members[i] = bundleMember{name: fmt.Sprintf("files/%04d", i)}
		}
		if _, err := artifact.ParseBundle(makeBundle(t, `{"files":{}}`, members), 1<<20); !errors.Is(err, artifact.ErrBundleLimit) {
			t.Fatalf("entry budget must return ErrBundleLimit: %v", err)
		}
	})
}

type bundleMember struct {
	name string
	data string
	mode fs.FileMode
}

func makeBundle(t *testing.T, manifest string, members []bundleMember) []byte {
	t.Helper()
	var data bytes.Buffer
	data.WriteString("SYSB\x02\x00\x00\x00")
	writer := zip.NewWriter(&data)
	if manifest != "" {
		members = append([]bundleMember{{name: "manifest.json", data: manifest}}, members...)
	}
	for _, member := range members {
		header := &zip.FileHeader{Name: member.name, Method: zip.Deflate}
		mode := member.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(member.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
