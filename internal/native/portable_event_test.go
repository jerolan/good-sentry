package native

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSymbolicatePortableEvent(t *testing.T) {
	data, err := os.ReadFile("testdata/portable-dotnet10.pdb")
	if err != nil {
		t.Fatal(err)
	}
	const id = "c6f075cc-4407-4e01-aeee-5d0399a7d7d1-b08a78f8"
	archive := func(contents []byte, broken bool) []byte {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		f, err := w.Create("inside/symbols.pdb")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(contents); err != nil {
			t.Fatal(err)
		}
		if broken {
			f, err = w.Create("bad.zip")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.Write([]byte{'P', 'K', 3, 4, 0}); err != nil {
				t.Fatal(err)
			}
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	raw := []byte(`{"platform":"csharp","event_id":"abc","extra":{"keep":17},"debug_meta":{"images":[{"type":"pe_dotnet","debug_id":"` + id + `"}]},"exception":{"values":[{"type":"Example","stacktrace":{"frames":[{"function":"Compute","addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x5"},{"function":"Main","addr_mode":"rel:0","function_id":"0x2","instruction_addr":"0x0"},{"function":"Conflict","filename":"unrelated.cs","addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x9"}]}}]},"stacktrace":{"frames":[{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x9"}]},"threads":{"values":[{"id":9,"stacktrace":{"frames":[{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x1"}]}}]}}`)
	pair := func(payload []byte, meta string) []DebugSymbolFile {
		return []DebugSymbolFile{{Name: "renamed.pdb", DebugID: meta, Content: payload}}
	}
	conflict := append(append([]byte(nil), data...), 0)
	for _, test := range []struct {
		name  string
		files []DebugSymbolFile
		want  bool
	}{
		{"standalone", pair(data, ""), true}, {"matching metadata", pair(data, id), true},
		{"zip", pair(archive(data, false), ""), true}, {"identical duplicate", append(pair(data, ""), pair(data, "")...), true},
		{"missing file", nil, false}, {"mismatched metadata", pair(data, "000000000000000000000000000000001"), false},
		{"conflicting same identity", append(pair(data, ""), pair(conflict, "")...), false},
		{"malformed nested archive", pair(archive(data, true), ""), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, changed, err := SymbolicatePortableEvent(raw, test.files)
			if err != nil {
				t.Fatal(err)
			}
			if changed != test.want {
				t.Fatalf("changed=%t, want %t", changed, test.want)
			}
			if !test.want {
				if !bytes.Equal(got, raw) {
					t.Fatal("unmatched event changed")
				}
				return
			}
			var event struct {
				EventID   string         `json:"event_id"`
				Extra     map[string]int `json:"extra"`
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []struct {
								Function, Filename, AbsPath string
								Line                        int `json:"lineno"`
							} `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
				Stacktrace struct {
					Frames []struct {
						Filename string
						Line     int `json:"lineno"`
					} `json:"frames"`
				} `json:"stacktrace"`
				Threads struct {
					Values []struct {
						ID         int
						Stacktrace struct {
							Frames []struct {
								Filename string
								Line     int `json:"lineno"`
							} `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"threads"`
			}
			if err = json.Unmarshal(got, &event); err != nil {
				t.Fatal(err)
			}
			frames := event.Exception.Values[0].Stacktrace.Frames
			if event.EventID != "abc" || event.Extra["keep"] != 17 || frames[0].Function != "Compute" || !strings.HasSuffix(frames[0].Filename, "/alpha.cs") || frames[0].Line != 100 || frames[1].Line != 14 || frames[2].Filename != "unrelated.cs" || frames[2].Line != 0 {
				t.Fatalf("unexpected exception frames: %+v", frames)
			}
			if event.Stacktrace.Frames[0].Line != 200 || !strings.HasSuffix(event.Stacktrace.Frames[0].Filename, "/beta.cs") || event.Threads.Values[0].ID != 9 || event.Threads.Values[0].Stacktrace.Frames[0].Line != 100 {
				t.Fatal("top-level or thread source lost")
			}
		})
	}
}

func TestPortableEventPreservesExistingAndInvalidFrameData(t *testing.T) {
	data, err := os.ReadFile("testdata/portable-dotnet10.pdb")
	if err != nil {
		t.Fatal(err)
	}
	const id = "c6f075cc-4407-4e01-aeee-5d0399a7d7d1-b08a78f8"
	files := []DebugSymbolFile{{Name: "symbols.pdb", Content: data}}
	for _, frame := range []string{
		`{"addr_mode":"rel:1","function_id":"0x1","instruction_addr":"0x5"}`,
		`{"addr_mode":"rel:0","function_id":"0x0","instruction_addr":"0x5"}`,
		`{"addr_mode":"rel:0","function_id":"0x1000000","instruction_addr":"0x5"}`,
		`{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x100000000"}`,
		`{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x5","abs_path":"other.cs"}`,
		`{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x5","filename":17}`,
	} {
		raw := []byte(`{"debug_meta":{"images":[{"type":"pe_dotnet","debug_id":"` + id + `"}]},"stacktrace":{"frames":[` + frame + `]}}`)
		got, changed, err := SymbolicatePortableEvent(raw, files)
		if err != nil || changed || !bytes.Equal(got, raw) {
			t.Fatalf("frame %s: changed=%v, err=%v, got=%s", frame, changed, err, got)
		}
	}
	raw := []byte(`{"debug_meta":{"images":[{"type":"pe_dotnet","debug_id":"` + id + `"}]},"exception":[{"stacktrace":{"frames":[{"addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x5","filename":"alpha.cs","lineno":123,"function":"Already known"}]}}]}`)
	got, changed, err := SymbolicatePortableEvent(raw, files)
	if err != nil || !changed {
		t.Fatalf("existing fields: changed=%v, err=%v", changed, err)
	}
	var event struct {
		Exception []struct {
			Stacktrace struct {
				Frames []map[string]any `json:"frames"`
			} `json:"stacktrace"`
		} `json:"exception"`
	}
	if err := json.Unmarshal(got, &event); err != nil {
		t.Fatal(err)
	}
	frame := event.Exception[0].Stacktrace.Frames[0]
	if frame["filename"] != "alpha.cs" || frame["lineno"] != float64(123) || frame["function"] != "Already known" || !strings.HasSuffix(frame["abs_path"].(string), "/alpha.cs") {
		t.Fatalf("existing source fields changed: %v", frame)
	}
}
