//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The public capture-to-outbox boundary owns atomic route wiring, CSRF,
// availability and browser response shapes. SQL tests cannot catch a forgotten
// ingestion hook. This extends the existing real PostgreSQL HTTP fixture and
// never starts an outbound sender or uses an external recipient.
func verifyAlertWorkflow(t *testing.T, request func(string, string, string, string) (int, map[string]any), org, project, csrf, ingestion string) {
	t.Helper()
	collection := "/api/v1/organizations/" + org + "/projects/" + project + "/alerts"
	deliveries := strings.TrimSuffix(collection, "/alerts") + "/alert-deliveries"
	code, me := request("GET", "/api/v1/me", "", "")
	features, _ := me["features"].(map[string]any)
	if code != 200 || features["alerts"] != true || features["alertDelivery"] != false {
		t.Fatalf("alert queue/sender availability=%d %+v", code, features)
	}
	input := `{"name":"Two production errors","quantity":2,"windowMinutes":5,"cooldownSeconds":900,"environments":["alert-http-proof"]}`
	if code, _ := request("POST", collection, input, ""); code != 403 {
		t.Fatalf("alert missing CSRF=%d", code)
	}
	code, rule := request("POST", collection, input, csrf)
	if code != 201 || rule["kind"] != "error_frequency" || rule["enabled"] != true || len(rule["recipients"].([]any)) != 1 {
		t.Fatalf("alert create/defaults=%d %+v", code, rule)
	}
	id := rule["id"].(string)
	path := collection + "/" + id
	code, rules := request("GET", collection, "", "")
	if code != 200 || len(rules["data"].([]any)) != 1 {
		t.Fatalf("alert list=%d %+v", code, rules)
	}
	for _, eventID := range []string{"00000000000000000000000000000b01", "00000000000000000000000000000b01", "00000000000000000000000000000b02"} {
		// An old SDK occurrence must still count as a newly received issue event.
		body := fmt.Sprintf(`{"event_id":%q,"message":"Alert HTTP proof","environment":"alert-http-proof","timestamp":"2001-01-01T00:00:00Z"}`, eventID)
		res, err := http.Post(ingestion, "text/plain", strings.NewReader("{}\n{\"type\":\"event\"}\n"+body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("alert capture=%d", res.StatusCode)
		}
	}
	code, list := request("GET", deliveries+"?rule="+id, "", "")
	if code != 200 || len(list["data"].([]any)) != 1 {
		t.Fatalf("receipt threshold or retry dedup=%d %+v", code, list)
	}
	delivery := list["data"].([]any)[0].(map[string]any)
	payload := delivery["payload"].(map[string]any)
	if delivery["status"] != "pending" || delivery["attempts"] != float64(0) || payload["kind"] != "error_frequency" || payload["count"] != float64(2) || !strings.HasPrefix(payload["url"].(string), "http://example.test/?") {
		t.Fatalf("queued payload=%+v", delivery)
	}
	code, detail := request("GET", deliveries+"/"+delivery["id"].(string), "", "")
	if code != 200 || len(detail["attempts"].([]any)) != 0 {
		t.Fatalf("unstarted attempts=%d %+v", code, detail)
	}
	code, queued := request("POST", path+"/test", "", csrf)
	if code != 202 || queued["queued"] != float64(1) {
		t.Fatalf("durable test queue=%d %+v", code, queued)
	}
	config := map[string]any{}
	for _, key := range []string{"name", "kind", "enabled", "quantity", "windowMinutes", "cooldownSeconds", "environments", "recipients"} {
		config[key] = rule[key]
	}
	config["enabled"] = false
	body, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ = request("PUT", path, string(body), csrf); code != 200 {
		t.Fatalf("pause alert=%d", code)
	}
	if code, _ = request("POST", path+"/test", "", csrf); code != 409 {
		t.Fatalf("disabled alert test=%d", code)
	}
	code, list = request("GET", deliveries+"?rule="+id, "", "")
	if code != 200 || len(list["data"].([]any)) != 2 {
		t.Fatalf("canceled history=%d %+v", code, list)
	}
	for _, item := range list["data"].([]any) {
		if item.(map[string]any)["status"] != "canceled" {
			t.Fatalf("configuration replacement did not cancel pending delivery: %+v", item)
		}
	}
	if code, _ = request("GET", deliveries+"?page=0", "", ""); code != 400 {
		t.Fatalf("invalid delivery page=%d", code)
	}
	if code, _ = request("DELETE", path, "", csrf); code != 204 {
		t.Fatalf("delete alert=%d", code)
	}
	if code, _ = request("GET", path, "", ""); code != 404 {
		t.Fatalf("deleted alert=%d", code)
	}
	code, list = request("GET", deliveries, "", "")
	if code != 200 || len(list["data"].([]any)) != 2 {
		t.Fatalf("deleting alert lost delivery audit=%d %+v", code, list)
	}
}
