//go:build integration

package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// HTTP owns the query-string contract and capture-to-filter wiring. Store
// tests separately own SQL boundaries and mixed legacy/segment aggregation.
func verifyTelemetryWindows(t *testing.T, request func(string, string, string, string) (int, map[string]any), org, project, ingestion string) {
	t.Helper()
	base := "/api/v1/organizations/" + org
	for _, endpoint := range []string{"transactions", "logs"} {
		for _, query := range []string{
			"start=&end=",
			"start=",
			"end=",
			"start=2020-01-01T00:00:00Z",
			"end=2020-01-01T00:00:02Z",
			"start=invalid&end=2020-01-01T00:00:02Z",
			"start=2020-01-01T00:00:00&end=2020-01-01T00:00:02Z",
			"start=2020-01-01T00:00:02Z&end=2020-01-01T00:00:02Z",
			"start=2020-01-02T00:00:00Z&end=2020-01-01T00:00:02Z",
			"start=2020-01-01T00:00:00Z&end=2022-01-01T00:00:00Z",
			"start=2020-01-01T00:00:00.0000001Z&end=2020-01-01T00:00:00.0000009Z",
			"start=2020-01-01T00:00:00%2B24:00&end=2020-01-01T00:00:02Z",
		} {
			if code, body := request("GET", base+"/"+endpoint+"?"+query, "", ""); code != 400 {
				t.Fatalf("%s accepted invalid interval %q: %d %+v", endpoint, query, code, body)
			}
		}
	}
	for _, service := range []string{"%00", "%FF", url.QueryEscape(strings.Repeat("s", 2001))} {
		if code, body := request("GET", base+"/logs?service="+service, "", ""); code != 400 {
			t.Fatalf("invalid service accepted: %d %+v", code, body)
		}
	}
	send := func(kind, body string) {
		t.Helper()
		response, err := http.Post(ingestion, "text/plain", strings.NewReader("{}\n{\"type\":\""+kind+"\"}\n"+body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("window fixture capture=%d", response.StatusCode)
		}
	}
	for i := range 3 {
		send("transaction", fmt.Sprintf(`{"event_id":"00000000000000000000000000000d0%d","type":"transaction","transaction":"HTTP window proof","start_timestamp":%d,"timestamp":%d,"contexts":{"trace":{"trace_id":"44444444444444444444444444444444","span_id":"4444444444444444","op":"window.http"}}}`, i, 1577836799+i, 1577836800+i))
		send("log", fmt.Sprintf(`{"items":[{"timestamp":%d,"level":"info","body":"window-http proof %d","trace_id":"44444444444444444444444444444444","attributes":{"service.name":{"type":"string","value":"payments"},"sentry.environment":{"type":"string","value":"window-http"}}}]}`, 1577836800+i, i))
	}
	send("log", `{"items":[{"timestamp":1577836801,"level":"info","body":"window-http other service","attributes":{"service.name":{"type":"string","value":"orders"},"sentry.environment":{"type":"string","value":"window-http"}}}]}`)
	// Equivalent offset and UTC instants must route to the same half-open range.
	rangeQuery := "start=2019-12-31T18:00:00-06:00&end=2020-01-01T00:00:02Z&project=" + project
	code, body := request("GET", base+"/transactions?"+rangeQuery, "", "")
	if code != 200 || len(body["data"].([]any)) != 1 || body["data"].([]any)[0].(map[string]any)["count"] != float64(2) {
		t.Fatalf("HTTP transaction range=%d %+v", code, body)
	}
	query := rangeQuery + "&service=payments&environment=window-http&query=window-http&level=info&trace=44444444444444444444444444444444"
	code, body = request("GET", base+"/logs?"+query, "", "")
	if code != 200 || len(body["data"].([]any)) != 2 {
		t.Fatalf("HTTP combined log filters=%d %+v", code, body)
	}
	for _, raw := range body["data"].([]any) {
		row := raw.(map[string]any)
		if row["service"] != "payments" || row["body"] == "window-http proof 2" || row["projectId"] != project {
			t.Fatalf("HTTP service/time scope lost: %+v", row)
		}
	}
	code, body = request("GET", base+"/logs?service=Payments&query=window-http", "", "")
	if code != 200 || len(body["data"].([]any)) != 0 {
		t.Fatalf("service must match exactly: %d %+v", code, body)
	}
}
