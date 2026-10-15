package artifact_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"goodsentry/internal/artifact"
)

func TestBundleSourceMapsResolvesFileIdentitiesAndScriptURLs(t *testing.T) {
	const id = "77e1d1e3b43b565e94f9cbe7240e8671"
	const body = `{"version":3,"sources":["src/app.js"],"names":[],"mappings":"AAAA","debug_id":"77E1D1E3-B43B-565E-94F9-CBE7240E8671","debugId":"77e1d1e3b43b565e94f9cbe7240e8671"}`
	for _, fixture := range []struct{ script, reference, mapURL string }{
		{"https://cdn.example/assets/app.js", "../maps/output.mapping", "https://cdn.example/maps/output.mapping"},
		{"~/assets/app.js", "../maps/output.mapping", "~/maps/output.mapping"},
		{"assets/app.js", "../maps/output.mapping", "maps/output.mapping"},
		{"https://cdn.example/assets/app.js", "https://maps.example/output.mapping", "https://maps.example/output.mapping"},
		{"~/assets/app.js", "../maps/output.mapping?build=A#map", "~/maps/output.mapping?build=A#map"},
	} {
		t.Run(fixture.script+fixture.mapURL, func(t *testing.T) {
			bundle := artifact.Bundle{DebugID: "unrelated-bundle-identity", Files: []artifact.BundleFile{
				{Name: "files/map", Type: "source_map", URL: fixture.mapURL, Data: []byte(body), Headers: map[string]string{"debug-id": id}},
				{Name: "files/script", Type: "minified_source", URL: fixture.script, Headers: map[string]string{"sourcemap": fixture.reference}},
				{Name: "files/other", Type: "minified_source", URL: "~/other.js", DebugID: id},
				{Name: "future", Type: "future_blob", URL: "future", DebugID: "future-identity", Data: []byte{0xff}},
			}}
			maps, err := artifact.BundleSourceMaps(bundle)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{fixture.script, "~/other.js"}
			if len(maps) != 1 || maps[0].Name != fixture.mapURL || maps[0].DebugID != id || string(maps[0].Data) != body || !reflect.DeepEqual(maps[0].CodeFiles, want) {
				t.Fatalf("wrong file identity, source base, or script links: %+v", maps)
			}
		})
	}
}

func TestBundleSourceMapsHeaderInheritanceAndAmbiguity(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	base := artifact.Bundle{DebugID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Files: []artifact.BundleFile{
		{Type: "source_map", URL: "~/map.mapping", Data: []byte(bundleMap)},
		{Type: "minified_source", URL: "~/app.js", DebugID: id, Headers: map[string]string{"sourcemap": "map.mapping"}},
	}}
	maps, err := artifact.BundleSourceMaps(base)
	if err != nil || len(maps) != 1 || maps[0].DebugID != id || !reflect.DeepEqual(maps[0].CodeFiles, []string{"~/app.js"}) {
		t.Fatalf("unambiguous file identity was not inherited: %+v, %v", maps, err)
	}
	for _, ambiguity := range []string{"URL", "DebugID"} {
		t.Run(ambiguity, func(t *testing.T) {
			bundle := artifact.Bundle{Files: []artifact.BundleFile{
				{Type: "source_map", URL: "~/one.map", DebugID: id, Data: []byte(bundleMap)},
				{Type: "source_map", URL: "~/two.map", DebugID: id, Data: []byte(bundleMap)},
				{Type: "minified_source", URL: "~/app.js", DebugID: id},
			}}
			if ambiguity == "URL" {
				bundle.Files[1].URL = bundle.Files[0].URL
				bundle.Files[0].DebugID, bundle.Files[1].DebugID, bundle.Files[2].DebugID = "", "", ""
				bundle.Files[2].Headers = map[string]string{"sourcemap": "one.map"}
			}
			maps, err := artifact.BundleSourceMaps(bundle)
			if err != nil || len(maps) != 2 || len(maps[0].CodeFiles) != 0 || len(maps[1].CodeFiles) != 0 {
				t.Fatalf("ambiguous association invented a script alias: %+v, %v", maps, err)
			}
		})
	}
	base.Files[1].DebugID = ""
	maps, err = artifact.BundleSourceMaps(base)
	if err != nil || maps[0].DebugID != "" {
		t.Fatalf("bundle identity substituted for absent file identity: %+v, %v", maps, err)
	}
}

func TestBundleSourceMapsHeaderIdentityKeepsQueryAndFragment(t *testing.T) {
	bundle := artifact.Bundle{Files: []artifact.BundleFile{
		{Type: "source_map", URL: "~/output.map?build=A#first", Data: []byte(bundleMap)},
		{Type: "minified_source", URL: "~/script.js", Headers: map[string]string{"sourcemap": "output.map?build=B#second"}},
	}}
	maps, err := artifact.BundleSourceMaps(bundle)
	if err != nil || len(maps) != 1 || len(maps[0].CodeFiles) != 0 {
		t.Fatalf("different resolved header identity linked another map: %+v, %v", maps, err)
	}
}

func TestBundleSourceMapsRejectsContradictoryIdentitiesAndInvalidMapURLs(t *testing.T) {
	const a = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const b = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for name, mapFile := range map[string]artifact.BundleFile{
		"header body conflict": {URL: "~/a.map", DebugID: a, Data: []byte(`{"debugId":"` + b + `"}`)},
		"body alias conflict":  {URL: "~/a.map", Data: []byte(`{"debugId":"` + a + `","debug_id":"` + b + `"}`)},
		"invalid identity":     {URL: "~/a.map", DebugID: "not-a-debug-id", Data: []byte(bundleMap)},
		"nonstring identity":   {URL: "~/a.map", Data: []byte(`{"debugId":42}`)},
		"long map URL":         {URL: strings.Repeat("x", 2049), Data: []byte(bundleMap)},
		"NUL map URL":          {URL: "~/a\x00.map", Data: []byte(bundleMap)},
	} {
		t.Run(name, func(t *testing.T) {
			mapFile.Type = "source_map"
			if _, err := artifact.BundleSourceMaps(artifact.Bundle{Files: []artifact.BundleFile{mapFile}}); err == nil {
				t.Fatal("accepted invalid map metadata")
			}
		})
	}
	for _, other := range []artifact.BundleFile{
		{Type: "source_map", URL: "~/b.map", DebugID: b, Data: []byte(bundleMap)},
		{Type: "minified_source", URL: "~/second.js", DebugID: b, Headers: map[string]string{"sourcemap": "a.map"}},
	} {
		bundle := artifact.Bundle{Files: []artifact.BundleFile{
			{Type: "source_map", URL: "~/a.map", Data: []byte(bundleMap)},
			{Type: "minified_source", URL: "~/first.js", DebugID: a, Headers: map[string]string{"sourcemap": "a.map"}}, other,
		}}
		if other.Type == "source_map" {
			bundle.Files[1].DebugID = b
		}
		if _, err := artifact.BundleSourceMaps(bundle); err == nil {
			t.Fatal("accepted contradictory header and file identities")
		}
	}
	if maps, err := artifact.BundleSourceMaps(artifact.Bundle{Files: []artifact.BundleFile{{Type: "source_map", URL: strings.Repeat("界", 682) + "ab", Data: []byte(bundleMap)}}}); err != nil || len(maps) != 1 {
		t.Fatalf("rejected a valid 2048-byte map URL: %v", err)
	}
}

func TestBundleSourceMapsRejectsInvalidUTF8BeforeJSONReplacement(t *testing.T) {
	manifest := "{\"files\":{\"files/a.map\":{\"type\":\"source_map\",\"url\":\"~/\xff.map\"}}}"
	bundle, err := artifact.ParseBundle(makeBundle(t, manifest, []bundleMember{{name: "files/a.map", data: bundleMap}}), 1<<20)
	if err == nil {
		_, err = artifact.BundleSourceMaps(bundle)
	}
	if err == nil {
		t.Fatal("invalid UTF-8 URL became an accepted replacement-character URL")
	}
}

func TestParseBundleOptionalReleaseAndDist(t *testing.T) {
	for _, field := range []string{"release", "dist"} {
		for _, value := range []any{nil, "", " value "} {
			manifest, _ := json.Marshal(map[string]any{"files": map[string]any{}, field: value})
			bundle, err := artifact.ParseBundle(makeBundle(t, string(manifest), nil), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			got := bundle.Release
			if field == "dist" {
				got = bundle.Dist
			}
			if value != nil && got != value || value == nil && got != "" {
				t.Fatalf("manifest %s=%v became %q", field, value, got)
			}
		}
		for _, value := range []any{1, true, []string{}, map[string]string{}} {
			manifest, _ := json.Marshal(map[string]any{"files": map[string]any{}, field: value})
			if _, err := artifact.ParseBundle(makeBundle(t, string(manifest), nil), 1<<20); err == nil {
				t.Fatalf("accepted nonstring manifest %s=%v", field, value)
			}
		}
	}
}
