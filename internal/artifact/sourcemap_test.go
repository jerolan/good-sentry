package artifact_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"goodsentry/internal/artifact"
)

// These v3 mappings were authored for this test. The first segment maps
// generated line 1/column 0 to source line 3/column 4 and the name charge.
const checkoutMap = `{"version":3,"file":"app.js","sources":["../src/checkout.js"],"names":["charge","other"],"mappings":"AAEIA,UACFC","sourcesContent":["export function charge() {\n  const total = 42;\n  throw new Error('declined');\n  return total;\n}"]}`

func TestSymbolicateCodeFileAliasesKeepMapSourceBase(t *testing.T) {
	const data = `{"version":3,"sources":["../src/order.js"],"names":["confirm"],"mappings":"AAAAA","sourcesContent":["throw Error('alias fixture');"]}`
	m := artifact.SourceMap{Name: "https://maps.example/build/special.mapping", CodeFiles: []string{"https://cdn.example/assets/chunk.js", "~/assets/other.js", "relative.js"}, Data: []byte(data)}
	for _, absPath := range []string{"https://cdn.example/assets/chunk.js?v=1#frame", "https://other.example/assets/other.js", "relative.js"} {
		t.Run(absPath, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"stacktrace": map[string]any{"frames": []any{map[string]any{"abs_path": absPath, "lineno": 1, "colno": 1}}}})
			result, err := artifact.Symbolicate(raw, []artifact.SourceMap{m})
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Stacktrace struct {
					Frames []struct {
						Filename, Function string
						Lineno, Colno      int
						ContextLine        string `json:"context_line"`
					}
				}
				RawStacktrace json.RawMessage `json:"raw_stacktrace"`
			}
			if err := json.Unmarshal(result, &event); err != nil {
				t.Fatal(err)
			}
			f := event.Stacktrace.Frames[0]
			if f.Filename != "https://maps.example/src/order.js" || f.Function != "confirm" || f.Lineno != 1 || f.Colno != 1 || f.ContextLine != "throw Error('alias fixture');" || len(event.RawStacktrace) == 0 {
				t.Fatalf("alias failed to map using the map URL as its source base: %s", result)
			}
		})
	}
	const raw = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/chunk.js","lineno":1,"colno":1}]}}`
	for name, maps := range map[string][]artifact.SourceMap{
		"ambiguous alias": {m, {Name: "~/another.map", CodeFiles: m.CodeFiles, Data: []byte(data)}},
		"debug ID only":   {{Name: m.Name, CodeFiles: m.CodeFiles, Data: m.Data, DebugIDOnly: true}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := artifact.Symbolicate(json.RawMessage(raw), maps)
			if err != nil || string(result) != raw {
				t.Fatalf("unsafe alias selected a map: %s, %v", result, err)
			}
		})
	}
	const explicit = `{"debug_meta":{"images":[{"type":"sourcemap","code_file":"https://cdn.example/assets/chunk.js","debug_id":"ffffffffffffffffffffffffffffffff"}]},"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/chunk.js","lineno":1,"colno":1}]}}`
	if result, err := artifact.Symbolicate(json.RawMessage(explicit), []artifact.SourceMap{m}); err != nil || string(result) != explicit {
		t.Fatalf("alias overrode the unavailable explicit build identity: %s, %v", result, err)
	}
}

func TestSymbolicateNodeFilenameFramesKeepOriginalsAndPathPriority(t *testing.T) {
	const filename = "/opt/app/dist/app.js"
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name   string
		abs    json.RawMessage
		mapped bool
	}{
		{"absent abs_path", nil, true},
		{"null abs_path", json.RawMessage(`null`), true},
		{"empty abs_path", json.RawMessage(`""`), true},
		{"authoritative abs_path", json.RawMessage(`"/opt/app/dist/different.js"`), false},
		{"malformed abs_path", json.RawMessage(`42`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame := map[string]any{"filename": filename, "lineno": 1, "colno": 1, "function": "minified"}
			if tc.abs != nil {
				frame["abs_path"] = tc.abs
			}
			raw, _ := json.Marshal(map[string]any{
				"debug_meta": map[string]any{"images": []any{map[string]string{"type": "sourcemap", "code_file": filename, "debug_id": id}}},
				"exception":  map[string]any{"values": []any{map[string]any{"stacktrace": map[string]any{"frames": []any{frame}}}}},
			})
			result, err := artifact.Symbolicate(raw, []artifact.SourceMap{{Name: "https://maps.example/maps/app.js.map", DebugID: id, Data: []byte(checkoutMap)}})
			if err != nil {
				t.Fatal(err)
			}
			if !tc.mapped {
				if string(result) != string(raw) {
					t.Fatalf("filename overrode the authoritative abs_path: %s", result)
				}
				return
			}
			type capturedFrame struct {
				Filename, Function string
				Lineno, Colno      int
				ContextLine        string `json:"context_line"`
			}
			var event struct {
				Exception struct {
					Values []struct {
						Stacktrace    struct{ Frames []capturedFrame }
						RawStacktrace struct{ Frames []capturedFrame } `json:"raw_stacktrace"`
					}
				}
			}
			if err := json.Unmarshal(result, &event); err != nil {
				t.Fatal(err)
			}
			exc := event.Exception.Values[0]
			if len(exc.RawStacktrace.Frames) != 1 {
				t.Fatalf("Node filename frame was not mapped and retained: %s", result)
			}
			mapped, original := exc.Stacktrace.Frames[0], exc.RawStacktrace.Frames[0]
			if mapped.Filename != "https://maps.example/src/checkout.js" || mapped.Function != "charge" || mapped.Lineno != 3 || mapped.Colno != 5 || mapped.ContextLine != "  throw new Error('declined');" || original.Filename != filename || original.Function != "minified" || original.Lineno != 1 || original.Colno != 1 {
				t.Fatalf("Node mapping lost original filename, source base or context: %s", result)
			}
		})
	}
}

func TestSymbolicateTraditionalMapKeepsOriginalsAndSDKMetadata(t *testing.T) {
	const raw = `{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","future":{"number":9007199254740993},"exception":{"values":[{"type":"Error","value":"declined","mechanism":{"type":"custom"},"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/app.js?v=2","filename":"app.js","lineno":1,"colno":1,"function":"a","in_app":true,"vars":{"cart":7}}],"frames_omitted":[10,20]}}]},"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/app.js","lineno":1,"colno":11}]}}`
	result, err := artifact.Symbolicate(json.RawMessage(raw), []artifact.SourceMap{{Name: "~/assets/app.js.map", Data: []byte(checkoutMap)}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Future    json.RawMessage
		Exception struct {
			Values []struct {
				Mechanism  json.RawMessage
				Stacktrace struct {
					Frames []struct {
						Filename, Function string
						ContextLine        string `json:"context_line"`
						Lineno, Colno      int
						InApp              bool     `json:"in_app"`
						PreContext         []string `json:"pre_context"`
						PostContext        []string `json:"post_context"`
						Vars               map[string]int
					}
					FramesOmitted []int `json:"frames_omitted"`
				}
				RawStacktrace json.RawMessage `json:"raw_stacktrace"`
			}
		}
		Stacktrace struct {
			Frames []struct {
				Lineno, Colno int
				Function      string
			}
		}
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Exception.Values) != 1 || len(got.Exception.Values[0].Stacktrace.Frames) != 1 {
		t.Fatalf("lost exception frames: %s", result)
	}
	exc := got.Exception.Values[0]
	f := exc.Stacktrace.Frames[0]
	// A ~ name has no URL host, so the consumer preserves the original path.
	if f.Filename != "../src/checkout.js" || f.Function != "charge" || f.Lineno != 3 || f.Colno != 5 || !f.InApp || f.Vars["cart"] != 7 || f.ContextLine != "  throw new Error('declined');" {
		t.Fatalf("incorrect original frame: %+v; event=%s", f, result)
	}
	if !reflect.DeepEqual(f.PreContext, []string{"export function charge() {", "  const total = 42;"}) || !reflect.DeepEqual(f.PostContext, []string{"  return total;", "}"}) || !reflect.DeepEqual(exc.Stacktrace.FramesOmitted, []int{10, 20}) {
		t.Fatalf("source context/stack metadata missing: %+v", exc.Stacktrace)
	}
	var original struct {
		Frames []struct {
			Filename, Function string
			Lineno, Colno      int
		}
	}
	if err := json.Unmarshal(exc.RawStacktrace, &original); err != nil || len(original.Frames) != 1 || original.Frames[0].Filename != "app.js" || original.Frames[0].Function != "a" || original.Frames[0].Lineno != 1 || original.Frames[0].Colno != 1 {
		t.Fatalf("original stack was changed or lost: %s, %v", exc.RawStacktrace, err)
	}
	if string(got.Future) != `{"number":9007199254740993}` || string(exc.Mechanism) != `{"type":"custom"}` || len(got.Stacktrace.Frames) != 1 || got.Stacktrace.Frames[0].Function != "other" || got.Stacktrace.Frames[0].Lineno != 4 || got.Stacktrace.Frames[0].Colno != 3 {
		t.Fatalf("unknown metadata lost or top-level frame not mapped: %s", result)
	}
}

func TestSymbolicateDebugIDOverridesDuplicateArtifactNames(t *testing.T) {
	const wrong = `{"version":3,"sources":["wrong.js"],"names":["wrong"],"mappings":"AAAAA","sourcesContent":["wrong"]}`
	const correct = `{"version":3,"sources":["right.js"],"names":["right"],"mappings":"AAAAA","sourcesContent":["right source"]}`
	const raw = `{"debug_meta":{"images":[{"type":"sourcemap","code_file":"https://cdn.example/app.js","debug_id":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"}]},"exception":{"values":[{"stacktrace":{"future_stack":"kept","frames":[]},"raw_stacktrace":{"frames":[{"abs_path":"https://cdn.example/app.js","lineno":1,"colno":1,"function":"a"}]}}]}}`
	maps := []artifact.SourceMap{
		{Name: "https://cdn.example/app.js.map", DebugID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Data: []byte(wrong)},
		{Name: "https://cdn.example/app.js.map", DebugID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Data: []byte(correct)},
	}
	result, err := artifact.Symbolicate(json.RawMessage(raw), maps)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Future string `json:"future_stack"`
					Frames []struct {
						Filename, Function string
						Lineno, Colno      int
						ContextLine        string `json:"context_line"`
					}
				}
				RawStacktrace struct{ Frames []struct{ Function string } } `json:"raw_stacktrace"`
			}
		}
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Exception.Values) != 1 || len(got.Exception.Values[0].Stacktrace.Frames) != 1 {
		t.Fatalf("raw-only frame was not symbolicated: %s", result)
	}
	exc := got.Exception.Values[0]
	f := exc.Stacktrace.Frames[0]
	if f.Filename != "https://cdn.example/right.js" || f.Function != "right" || f.Lineno != 1 || f.Colno != 1 || f.ContextLine != "right source" || exc.RawStacktrace.Frames[0].Function != "a" || exc.Stacktrace.Future != "kept" {
		t.Fatalf("wrong map selected or original changed: %s", result)
	}
	missing, err := artifact.Symbolicate(json.RawMessage(raw), maps[:1])
	if err != nil || string(missing) != raw {
		t.Fatalf("missing debug ID fell back to another build: %s, %v", missing, err)
	}
}

func TestSymbolicateBareExceptionArrayPreservesItsShape(t *testing.T) {
	const raw = `{"exception":[{"type":"Error","future_exception":"kept","stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/app.js","filename":"app.js","function":"a","lineno":1,"colno":1}]}}]}`
	result, err := artifact.Symbolicate(json.RawMessage(raw), []artifact.SourceMap{{Name: "~/assets/app.js.map", Data: []byte(checkoutMap)}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Exception []struct {
			Type       string
			Future     string `json:"future_exception"`
			Stacktrace struct {
				Frames []struct {
					Filename, Function string
					Lineno, Colno      int
				}
			}
			RawStacktrace struct {
				Frames []struct {
					Filename, Function string
					Lineno, Colno      int
				}
			} `json:"raw_stacktrace"`
		}
	}
	if err := json.Unmarshal(result, &got); err != nil || len(got.Exception) != 1 || len(got.Exception[0].Stacktrace.Frames) != 1 || len(got.Exception[0].RawStacktrace.Frames) != 1 {
		t.Fatalf("bare-array frame skipped or shape changed: %s, %v", result, err)
	}
	exc := got.Exception[0]
	f, original := exc.Stacktrace.Frames[0], exc.RawStacktrace.Frames[0]
	if exc.Type != "Error" || exc.Future != "kept" || f.Filename != "../src/checkout.js" || f.Function != "charge" || f.Lineno != 3 || f.Colno != 5 || original.Filename != "app.js" || original.Function != "a" || original.Lineno != 1 || original.Colno != 1 {
		t.Fatalf("bare-array mapping or original metadata incorrect: %s", result)
	}
}

func TestSymbolicateNoMatchAndMalformedInput(t *testing.T) {
	maps := []artifact.SourceMap{{Name: "~/assets/app.js.map", Data: []byte(checkoutMap)}}
	for _, raw := range []string{
		`{"message":"no stack","future":{"kept":true}}`,
		`{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/other/app.js","lineno":1,"colno":1}]}}`,
		`{"exception":{"values":[null,42,{"stacktrace":{"frames":[null,"bad",{"abs_path":"https://other.example/absent.js","lineno":1,"colno":1}]}}]}}`,
		`{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/app.js","lineno":"bad","colno":1},{"abs_path":"https://cdn.example/assets/app.js","lineno":0,"colno":-1}]}}`,
	} {
		got, err := artifact.Symbolicate(json.RawMessage(raw), maps)
		if err != nil || string(got) != raw {
			t.Fatalf("unmatched telemetry changed: %s, %v", got, err)
		}
	}
	for _, data := range []string{`broken`, `{}`, `{"version":3,"sources":["a.js"],"names":[],"mappings":"!"}`, `{"version":3,"sources":["a.js"],"names":[],"mappings":"g"}`, `{"version":3,"sources":["a.js"],"names":[],"mappings":"AA"}`} {
		if _, err := artifact.Symbolicate(json.RawMessage(`{"message":"hello"}`), []artifact.SourceMap{{Name: "bad.map", Data: []byte(data)}}); err == nil {
			t.Errorf("accepted malformed source map %s", data)
		}
	}
	if _, err := artifact.Symbolicate(json.RawMessage(`{"broken"`), maps); err == nil {
		t.Error("accepted invalid event JSON")
	}
	const ambiguous = `{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/app.js","lineno":1,"colno":1}]}}`
	got, err := artifact.Symbolicate(json.RawMessage(ambiguous), append(maps, maps[0]))
	if err != nil || string(got) != ambiguous {
		t.Fatalf("ambiguous artifact silently selected: %s, %v", got, err)
	}
	for _, raw := range []string{
		`{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/app.js","lineno":1}]}}`,
		`{"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/app.js","lineno":1,"colno":0}]}}`,
		`{"raw_stacktrace":{},"stacktrace":{"frames":[{"abs_path":"https://cdn.example/assets/app.js","lineno":1,"colno":1}]}}`,
	} {
		result, err := artifact.Symbolicate(json.RawMessage(raw), maps)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Stacktrace struct{ Frames []struct{ Lineno, Colno int } }
		}
		if err := json.Unmarshal(result, &got); err != nil || len(got.Stacktrace.Frames) != 1 || got.Stacktrace.Frames[0].Lineno != 3 || got.Stacktrace.Frames[0].Colno != 5 {
			t.Fatalf("first-column/fallback handling failed: %s, %v", result, err)
		}
	}
}
