//go:build integration

package server

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"goodsentry/internal/store"
)

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
