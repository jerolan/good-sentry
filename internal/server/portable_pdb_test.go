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
