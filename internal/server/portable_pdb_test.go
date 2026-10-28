//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

// The fixture was emitted by Sentry .NET 6.12.1 with Probe.pdb absent at
// runtime. The optional envelope path also exercises the complete SDK capture.
func TestPortablePDBRealDotnetSDKCapture(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	pdb, err := os.ReadFile("testdata/portable-sdk/Probe.pdb")
	if err != nil {
		t.Fatal(err)
	}
	event, err := os.ReadFile("testdata/portable-sdk/event.json")
	if err != nil {
		t.Fatal(err)
	}
	var original struct {
		ID string `json:"event_id"`
	}
	if err = json.Unmarshal(event, &original); err != nil || original.ID == "" {
		t.Fatalf("invalid SDK fixture: %v", err)
	}
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	digest := sha1.Sum(pdb)
	const debugID = "f47611dd-a29f-4fba-a19a-94054bf3fca4-d34b6eb7"
	if _, err = f.db.SaveDebugFile(f.ctx, access, f.project.ID, "Probe.pdb", "portablepdb", debugID, "unknown", "Probe.pdb", hex.EncodeToString(digest[:]), nil, nil, pdb); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err = json.Compact(&compact, event); err != nil {
		t.Fatal(err)
	}
	body := append([]byte("{}\n{\"type\":\"event\"}\n"), compact.Bytes()...)
	if envelopePath := os.Getenv("GOODSENTRY_DOTNET_ENVELOPE"); envelopePath != "" {
		body, err = os.ReadFile(envelopePath)
		if err != nil {
			t.Fatal(err)
		}
	}
	endpoint := f.server.URL + "/good/api/" + f.project.ID + "/envelope/?sentry_version=7&sentry_key=" + f.project.PublicKey
	resp, err := http.Post(endpoint, "text/plain", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SDK capture=%d", resp.StatusCode)
	}
	var stored []byte
	if err = f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, original.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []struct {
						Function, Filename string
						Line               int `json:"lineno"`
					} `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
	}
	if err = json.Unmarshal(stored, &result); err != nil {
		t.Fatal(err)
	}
	for _, value := range result.Exception.Values {
		for _, frame := range value.Stacktrace.Frames {
			if strings.Contains(frame.Function, "ThrowProbe") {
				if !strings.HasSuffix(frame.Filename, "/Program.cs") || frame.Line != 18 {
					t.Fatalf("SDK frame: %+v", frame)
				}
				return
			}
		}
	}
	t.Fatal("SDK probe frame missing")
}

func TestPortablePDBManagedEnvelopeProjectIsolation(t *testing.T) {
	data, err := os.ReadFile("../native/testdata/portable-dotnet10.pdb")
	if err != nil {
		t.Fatal(err)
	}
	const id = "c6f075cc-4407-4e01-aeee-5d0399a7d7d1-b08a78f8"
	for _, test := range []struct {
		name                  string
		other, mismatch, want bool
	}{
		{"same project", false, false, true}, {"other project", true, false, false}, {"metadata mismatch", false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			projectID := f.project.ID
			if test.other {
				other, err := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Portable other project", "native")
				if err != nil {
					t.Fatal(err)
				}
				projectID = other.ID
			}
			metadataID := id
			if test.mismatch {
				metadataID = "000000000000000000000000000000001"
			}
			digest := sha1.Sum(data)
			if _, err = f.db.SaveDebugFile(f.ctx, access, projectID, "renamed.pdb", "portablepdb", metadataID, "unknown", "renamed.pdb", hex.EncodeToString(digest[:]), nil, nil, data); err != nil {
				t.Fatal(err)
			}
			const eventID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbc"
			payload := `{"event_id":"` + eventID + `","platform":"csharp","extra":{"keep":7},"debug_meta":{"images":[{"type":"pe_dotnet","debug_id":"` + id + `"}]},"exception":{"values":[{"type":"Example","stacktrace":{"frames":[{"function":"Compute","addr_mode":"rel:0","function_id":"0x1","instruction_addr":"0x5"}]}}]}}`
			endpoint := f.server.URL + "/good/api/" + f.project.ID + "/envelope/?sentry_version=7&sentry_key=" + f.project.PublicKey
			resp, err := http.Post(endpoint, "text/plain", strings.NewReader("{}\n{\"type\":\"event\"}\n"+payload))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("capture=%d", resp.StatusCode)
			}
			var stored []byte
			if err = f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, eventID).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			var event struct {
				Extra     map[string]int `json:"extra"`
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []struct {
								Function string `json:"function"`
								Filename string `json:"filename"`
								Line     int    `json:"lineno"`
							} `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if err = json.Unmarshal(stored, &event); err != nil {
				t.Fatal(err)
			}
			frame := event.Exception.Values[0].Stacktrace.Frames[0]
			if event.Extra["keep"] != 7 || frame.Function != "Compute" {
				t.Fatalf("event fields changed: %+v", frame)
			}
			if test.want {
				if !strings.HasSuffix(frame.Filename, "/alpha.cs") || frame.Line != 100 {
					t.Fatalf("managed frame=%+v", frame)
				}
			} else if frame.Filename != "" || frame.Line != 0 {
				t.Fatalf("unmatched PDB changed frame=%+v", frame)
			}
		})
	}
}

func TestEmbeddedPortablePDBManagedEnvelopeIsolation(t *testing.T) {
	pe, err := os.ReadFile("../native/testdata/portable-embedded/Probe.dll")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../native/testdata/portable-embedded/event.json")
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err = json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	frames := event["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
	for _, item := range frames {
		f := item.(map[string]any)
		delete(f, "filename")
		delete(f, "abs_path")
		delete(f, "lineno")
	}
	missing, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	const id = "6755b4cd-d3c5-4986-89d6-490d7bd5545c-8e698d33"
	for _, tc := range []struct {
		name            string
		other, original bool
	}{{"same project", false, false}, {"other project", true, false}, {"original SDK fields", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			project := f.project.ID
			if tc.other {
				other, e := f.db.CreateProject(f.ctx, f.owner.ID, f.org.Slug, "Embedded other", "native")
				if e != nil {
					t.Fatal(e)
				}
				project = other.ID
			}
			digest := sha1.Sum(pe)
			if _, e := f.db.SaveDebugFile(f.ctx, access, project, "Probe.dll", "pe", id, "unknown", "Probe.dll", hex.EncodeToString(digest[:]), nil, nil, pe); e != nil {
				t.Fatal(e)
			}
			payload := missing
			if tc.original {
				var compact bytes.Buffer
				if e := json.Compact(&compact, raw); e != nil {
					t.Fatal(e)
				}
				payload = compact.Bytes()
			}
			body := append([]byte("{}\n{\"type\":\"event\"}\n"), payload...)
			if path := os.Getenv("GOODSENTRY_EMBEDDED_DOTNET_ENVELOPE"); tc.original && path != "" {
				envelope, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				body = envelope
			}
			endpoint := f.server.URL + "/good/api/" + f.project.ID + "/envelope/?sentry_version=7&sentry_key=" + f.project.PublicKey
			resp, e := http.Post(endpoint, "text/plain", bytes.NewReader(body))
			if e != nil {
				t.Fatal(e)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("capture=%d", resp.StatusCode)
			}
			var stored []byte
			if e = f.sql.QueryRow(f.ctx, `SELECT data FROM events WHERE project_id=$1::bigint AND event_id=$2`, f.project.ID, event["event_id"]).Scan(&stored); e != nil {
				t.Fatal(e)
			}
			var out map[string]any
			if e = json.Unmarshal(stored, &out); e != nil {
				t.Fatal(e)
			}
			result := out["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
			for _, item := range result {
				frame := item.(map[string]any)
				if strings.Contains(frame["function"].(string), "ThrowProbe") {
					name, _ := frame["filename"].(string)
					if tc.other {
						if name != "" || frame["lineno"] != nil {
							t.Fatalf("cross-project frame=%v", frame)
						}
					} else if !strings.HasSuffix(name, "Program.cs") || frame["lineno"] != float64(18) {
						t.Fatalf("frame=%v", frame)
					}
					if tc.original && (name != "Program.cs" || frame["abs_path"] != "/tmp/goodsentry-embedded-sdk/app/Program.cs" || frame["colno"] != float64(33)) {
						t.Fatalf("SDK source fields changed: %v", frame)
					}
					return
				}
			}
			t.Fatal("SDK frame missing")
		})
	}
}
