package artifact_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"goodsentry/internal/artifact"
)

func TestSourceMapUnmappedSegmentsDoNotBorrowPreviousSources(t *testing.T) {
	for _, tc := range []struct {
		mappings, sources string
		column            int
	}{
		{"AAAAA,K", `["original.js"]`, 6},
		{"A", `[]`, 1},
		{"", `[]`, 1},
	} {
		raw := json.RawMessage(fmt.Sprintf(`{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","filename":"app.js","function":"a","lineno":1,"colno":%d}]}}`, tc.column))
		data := []byte(fmt.Sprintf(`{"version":3,"sources":%s,"names":["mapped"],"mappings":%q}`, tc.sources, tc.mappings))
		if err := artifact.ValidateSourceMap("https://cdn.example/app.js.map", data); err != nil {
			t.Fatalf("valid unmapped map rejected: %s, %v", data, err)
		}
		got, err := artifact.Symbolicate(raw, []artifact.SourceMap{{Name: "https://cdn.example/app.js.map", Data: data}})
		if err != nil || string(got) != string(raw) {
			t.Fatalf("unmapped location borrowed a source: %s, %v", got, err)
		}
	}
}

func TestSourceMapRejectsInvalidIndicesAndPositionsAtUpload(t *testing.T) {
	for _, mappings := range []string{
		"ACAA", "ADAA", "AAAAC", "AAAAD", "AADA", "AAAD", "DAAA", "BAAA",
		"ggggggEAAA", "+/////DAAA", "8/////DAAA,CAAA", "AA+/////DA", "AAA+/////D",
		"AAAA,ACAA", "AAAAAA", "AAAA,", "AAgg",
	} {
		data := []byte(fmt.Sprintf(`{"version":3,"sources":["original.js"],"names":["named"],"mappings":%q}`, mappings))
		if err := artifact.ValidateSourceMap("https://cdn.example/app.js.map", data); err == nil {
			t.Errorf("accepted invalid source-map semantics %q", mappings)
		}
	}
	for _, data := range []string{
		`{"version":3,"sources":null,"mappings":"A"}`,
		`{"version":3,"sources":[42],"mappings":"AAAA"}`,
		`{"version":3,"sources":["a"],"names":[null],"mappings":"AAAAA"}`,
		`{"version":3,"sections":null}`,
		`{"version":3,"sections":[{"offset":{"line":0},"map":{"version":3,"sources":[],"mappings":""}}]}`,
		`{"version":3,"sections":[{"offset":{"line":-1,"column":0},"map":{"version":3,"sources":[],"mappings":""}}]}`,
		`{"version":3,"sections":[{"offset":{"line":0,"column":0},"url":"https://unreachable.invalid/map"}]}`,
	} {
		if err := artifact.ValidateSourceMap("app.js.map", []byte(data)); err == nil {
			t.Errorf("accepted malformed source-map shape: %s", data)
		}
	}
}

func TestSourceMapCarriesIndicesAcrossLinesAndKeepsUnmappedLines(t *testing.T) {
	const data = `{"version":3,"sources":["first.js","second.js"],"names":["one","two"],"mappings":"CCIEC;ADCDD;"}`
	const raw = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","lineno":1,"colno":2},{"abs_path":"https://cdn.example/app.js","lineno":2,"colno":1},{"abs_path":"https://cdn.example/app.js","filename":"unmapped.js","lineno":3,"colno":1},{"abs_path":"https://cdn.example/app.js","filename":"before-first.js","lineno":1,"colno":1}]}}`
	frames := symbolicatedFrames(t, raw, data)
	if len(frames) != 4 || frames[0].Filename != "https://cdn.example/second.js" || frames[0].Function != "two" || frames[0].Lineno != 5 || frames[0].Colno != 3 || frames[1].Filename != "https://cdn.example/first.js" || frames[1].Function != "one" || frames[1].Lineno != 6 || frames[1].Colno != 2 || frames[2].Filename != "unmapped.js" || frames[3].Filename != "before-first.js" {
		t.Fatalf("line/index carry or same-line search incorrect: %+v", frames)
	}
}

func TestSourceMapNullSourceAndSourceRootContext(t *testing.T) {
	const data = `{"version":3,"sourceRoot":"../src","sources":[null,"checkout.js"],"sourcesContent":[null,"first\nsecond\nthird"],"names":["charge"],"mappings":"AAAAA,UCCAA"}`
	const raw = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","filename":"unknown.js","lineno":1,"colno":1},{"abs_path":"https://cdn.example/app.js","lineno":1,"colno":11}]}}`
	frames := symbolicatedFrames(t, raw, data)
	if len(frames) != 2 || frames[0].Filename != "unknown.js" || frames[1].Filename != "https://cdn.example/src/checkout.js" || frames[1].Lineno != 2 || frames[1].Colno != 1 || frames[1].Context != "second" {
		t.Fatalf("null source/sourceRoot mapping incorrect: %+v", frames)
	}
	emptyRoot := strings.Replace(data, `"sourceRoot":"../src"`, `"sourceRoot":""`, 1)
	frames = symbolicatedFrames(t, raw, emptyRoot)
	if frames[1].Filename != "https://cdn.example/checkout.js" {
		t.Fatalf("empty sourceRoot changed source base: %+v", frames)
	}
}

func TestSourceMapIndexedOffsetsApplyColumnOnlyToFirstLine(t *testing.T) {
	const data = `{"version":3,"sections":[{"offset":{"line":2,"column":5},"map":{"version":3,"sources":["first.js"],"names":["first"],"mappings":"AAAAA;AACA"}},{"offset":{"line":5,"column":10},"map":{"version":3,"sections":[{"offset":{"line":0,"column":3},"map":{"version":3,"sources":["nested.js"],"names":["nested"],"mappings":"AAAAA;AACA"}}]}}]}`
	const raw = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","lineno":3,"colno":6},{"abs_path":"https://cdn.example/app.js","lineno":4,"colno":1},{"abs_path":"https://cdn.example/app.js","lineno":6,"colno":14},{"abs_path":"https://cdn.example/app.js","lineno":7,"colno":1},{"abs_path":"https://cdn.example/app.js","filename":"before.js","lineno":3,"colno":5}]}}`
	frames := symbolicatedFrames(t, raw, data)
	if len(frames) != 5 || frames[0].Filename != "https://cdn.example/first.js" || frames[0].Lineno != 1 || frames[1].Lineno != 2 || frames[2].Filename != "https://cdn.example/nested.js" || frames[2].Lineno != 1 || frames[3].Filename != "https://cdn.example/nested.js" || frames[3].Lineno != 2 || frames[4].Filename != "before.js" {
		t.Fatalf("indexed offsets incorrect: %+v", frames)
	}
}

func TestSourceMapSectionBoundariesStopPreviousMappings(t *testing.T) {
	const data = `{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"version":3,"sources":["first.js"],"names":[],"mappings":"AAAA"}},{"offset":{"line":0,"column":5},"map":{"version":3,"sources":[],"mappings":""}},{"offset":{"line":0,"column":10},"map":{"version":3,"sources":["later.js"],"mappings":"EAAA"}}]}`
	const raw = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","filename":"empty-section.js","lineno":1,"colno":6},{"abs_path":"https://cdn.example/app.js","filename":"leading-gap.js","lineno":1,"colno":11},{"abs_path":"https://cdn.example/app.js","lineno":1,"colno":13}]}}`
	frames := symbolicatedFrames(t, raw, data)
	if len(frames) != 3 || frames[0].Filename != "empty-section.js" || frames[1].Filename != "leading-gap.js" || frames[2].Filename != "https://cdn.example/later.js" {
		t.Fatalf("indexed section borrowed preceding source: %+v", frames)
	}
	for _, data := range []string{
		`{"version":3,"sections":[{"offset":{"line":1,"column":0},"map":{"version":3,"sources":[],"mappings":""}},{"offset":{"line":0,"column":0},"map":{"version":3,"sources":[],"mappings":""}}]}`,
		`{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"version":3,"sources":[],"mappings":"U"}},{"offset":{"line":0,"column":5},"map":{"version":3,"sources":[],"mappings":""}}]}`,
	} {
		if err := artifact.ValidateSourceMap("app.js.map", []byte(data)); err == nil {
			t.Errorf("accepted unordered/overlapping sections: %s", data)
		}
	}
}

func TestIndexedSourceMapContextFollowsSelectedSourceRecord(t *testing.T) {
	const data = `{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"version":3,"sources":["shared.js"],"sourcesContent":["first section content"],"names":["first"],"mappings":"AAAAA"}},{"offset":{"line":2,"column":0},"map":{"version":3,"sources":["shared.js"],"sourcesContent":["second section content"],"names":["second"],"mappings":"AAAAA"}}]}`
	const raw = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","lineno":3,"colno":1}]}}`
	frames := symbolicatedFrames(t, raw, data)
	if len(frames) != 1 || frames[0].Function != "second" || frames[0].Context != "second section content" {
		t.Fatalf("context came from another section with the same URL: %+v", frames)
	}
}

func TestSourceMapParserBudgetsAreEnforced(t *testing.T) {
	nested := `{"version":3,"sources":[],"mappings":""}`
	for range 32 {
		nested = `{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":` + nested + `}]}`
	}
	if err := artifact.ValidateSourceMap("app.js.map", []byte(nested)); err != nil {
		t.Fatalf("depth 32 rejected: %v", err)
	}
	tooDeep := `{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":` + nested + `}]}`
	if err := artifact.ValidateSourceMap("app.js.map", []byte(tooDeep)); !errors.Is(err, artifact.ErrSourceMapLimit) {
		t.Fatalf("depth limit missing: %v", err)
	}
	mappings := strings.Repeat("A,", 999_999) + "A"
	data := []byte(`{"version":3,"sources":[],"mappings":"` + mappings + `"}`)
	if err := artifact.ValidateSourceMap("app.js.map", data); err != nil {
		t.Fatalf("one-million mapping boundary rejected: %v", err)
	}
	tooMany := []byte(`{"version":3,"sources":[],"mappings":"` + mappings + `,A"}`)
	if err := artifact.ValidateSourceMap("app.js.map", tooMany); !errors.Is(err, artifact.ErrSourceMapLimit) {
		t.Fatalf("mapping limit missing: %v", err)
	}
	indexed := []byte(`{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":` + string(data) + `}]}`)
	if err := artifact.ValidateSourceMap("app.js.map", indexed); !errors.Is(err, artifact.ErrSourceMapLimit) {
		t.Fatalf("section boundary did not consume the mapping budget: %v", err)
	}
	// Individually valid uploads share the event's processing budget.
	part := []byte(`{"version":3,"sources":[],"mappings":"` + strings.Repeat("A,", 599_999) + `A"}`)
	_, err := artifact.Symbolicate(json.RawMessage(`{}`), []artifact.SourceMap{
		{Name: "one.js.map", Data: part}, {Name: "two.js.map", Data: part},
	})
	if !errors.Is(err, artifact.ErrSourceMapLimit) {
		t.Fatalf("selected maps bypassed shared mapping budget: %v", err)
	}
}

func TestSourceMapAcceptsBoundedNegativeGeneratedDeltas(t *testing.T) {
	// U=+10 and L=-5 put the second encoded segment before the first.
	// Consumers sort positions after decoding; deltas need not be positive.
	const data = `{"version":3,"sources":["first.js","second.js"],"names":[],"mappings":"UAAA,LCAA"}`
	const raw = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","lineno":1,"colno":6},{"abs_path":"https://cdn.example/app.js","lineno":1,"colno":11}]}}`
	frames := symbolicatedFrames(t, raw, data)
	if len(frames) != 2 || frames[0].Filename != "https://cdn.example/second.js" || frames[1].Filename != "https://cdn.example/first.js" {
		t.Fatalf("valid negative generated delta rejected or unsorted: %+v", frames)
	}
}

func FuzzSourceMapPublicBoundaries(f *testing.F) {
	f.Add([]byte(`{"version":3,"sources":["original.js"],"names":["charge"],"mappings":"AAAAA,K"}`))
	f.Add([]byte(`{"version":3,"sources":[],"mappings":"A"}`))
	f.Add([]byte(`{"version":3,"sources":[],"mappings":"ACAA"}`))
	f.Add([]byte(`{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"version":3,"sources":[null],"mappings":"AAAA"}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		const name = "https://cdn.example/app.js.map"
		if err := artifact.ValidateSourceMap(name, data); err != nil {
			return
		}
		const raw = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","lineno":1,"colno":1},{"abs_path":"https://cdn.example/app.js","lineno":2,"colno":6}]}}`
		result, err := artifact.Symbolicate(json.RawMessage(raw), []artifact.SourceMap{{Name: name, Data: data}})
		if err != nil || !json.Valid(result) {
			t.Fatalf("validated map could not safely process telemetry: %s, %v", result, err)
		}
	})
}

type mappedFrame struct {
	Filename, Function string
	Lineno, Colno      int
	Context            string `json:"context_line"`
}

func symbolicatedFrames(t *testing.T, raw, data string) []mappedFrame {
	t.Helper()
	result, err := artifact.Symbolicate(json.RawMessage(raw), []artifact.SourceMap{{Name: "https://cdn.example/app.js.map", Data: []byte(data)}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Stacktrace struct{ Frames []mappedFrame }
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	return got.Stacktrace.Frames
}
