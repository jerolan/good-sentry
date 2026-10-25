package native

import (
	"encoding/json"
	"testing"
)

func TestResymbolicateEventFillsOnlyMissingNativeFrames(t *testing.T) {
	symbols, _ := BuildSymbolTables([]DebugSymbolFile{{
		Name: "test_app.sym",
		Content: []byte("MODULE windows x86_64 TEST test_app.pdb\nFILE 1 C:\\src\\crash.cc\n" +
			"FUNC 1010 20 0 CrashFunction\n1010 20 42 1\n"),
	}})
	original := []byte(`{"event_id":"event-1","platform":"native","timestamp":"2026-10-25T12:00:00Z","extra":{"keep":"yes"},"debug_meta":{"images":[{"code_file":"test_app.pdb","image_addr":"0x100000","image_size":8192,"debug_id":"5A9832E5287241C1838ED98914E9B7FF1"}]},"exception":{"values":[{"type":"Crash","stacktrace":{"frames":[{"instruction_addr":"0x101010","package":"test_app.pdb","unknown":{"keep":true}},{"instruction_addr":"0x101011","package":"test_app.pdb","function":"ExistingInline","lineno":8},{"instruction_addr":"0x900000","package":"test_app.pdb","function":""}]}}]}}`)
	updated, changed, err := ResymbolicateEvent(original, symbols)
	if err != nil || !changed {
		t.Fatalf("resymbolicate changed=%v err=%v", changed, err)
	}
	var event struct {
		ID        string         `json:"event_id"`
		Timestamp string         `json:"timestamp"`
		Extra     map[string]any `json:"extra"`
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []map[string]json.RawMessage `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
	}
	if err := json.Unmarshal(updated, &event); err != nil {
		t.Fatal(err)
	}
	frames := event.Exception.Values[0].Stacktrace.Frames
	var function, filename string
	var line int
	if err := json.Unmarshal(frames[0]["function"], &function); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frames[0]["filename"], &filename); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frames[0]["lineno"], &line); err != nil {
		t.Fatal(err)
	}
	if event.ID != "event-1" || event.Timestamp != "2026-10-25T12:00:00Z" || event.Extra["keep"] != "yes" || len(frames) != 3 || function != "CrashFunction" || filename != `C:\src\crash.cc` || line != 42 {
		t.Fatalf("event identity/context or symbols changed unexpectedly: %+v frame=%+v", event, frames[0])
	}
	var unknown map[string]bool
	if err := json.Unmarshal(frames[0]["unknown"], &unknown); err != nil || !unknown["keep"] {
		t.Fatalf("frame extension was not preserved: %s %v", frames[0]["unknown"], err)
	}
	var existing string
	if err := json.Unmarshal(frames[1]["function"], &existing); err != nil || existing != "ExistingInline" {
		t.Fatalf("existing frame was replaced: %s %v", frames[1]["function"], err)
	}
	var unresolved string
	if err := json.Unmarshal(frames[2]["function"], &unresolved); err != nil || unresolved != "" {
		t.Fatalf("unmatched frame changed: %s %v", frames[2]["function"], err)
	}
	second, changed, err := ResymbolicateEvent(updated, symbols)
	if err != nil || changed || string(second) != string(updated) {
		t.Fatalf("repeat was not idempotent: changed=%v err=%v", changed, err)
	}
}

func TestResymbolicateEventLeavesNonNativePayloadUntouched(t *testing.T) {
	original := []byte(`{"event_id":"event-2","platform":"go","stacktrace":{"frames":[]}}`)
	updated, changed, err := ResymbolicateEvent(original, SymbolSet{"test_app.pdb": {{Start: 0, End: 100, Name: "fn"}}})
	if err != nil || changed || string(updated) != string(original) {
		t.Fatalf("non-native event changed: %s changed=%v err=%v", updated, changed, err)
	}
}
