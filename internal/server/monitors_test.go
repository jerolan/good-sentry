//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// HTTP owns route/CSRF/feature gating, defaults, capability URLs and the check
// response shape. SQL and probe tests cannot detect disconnected endpoints.
// Extend the real authenticated PostgreSQL fixture without a production seam.
func verifyUptimeWorkflow(t *testing.T, request func(string, string, string, string) (int, map[string]any), base, org, project, csrf string) {
	t.Helper()
	collection := "/api/v1/organizations/" + org + "/monitors"
	code, me := request("GET", "/api/v1/me", "", "")
	features, _ := me["features"].(map[string]any)
	if code != 200 || features["uptime"] != true {
		t.Fatalf("uptime availability=%d %+v", code, me)
	}
	input := `{"name":"Web health","type":"GET","url":"https://example.test/health","project":"` + project + `"}`
	if code, _ = request("POST", collection, input, ""); code != 403 {
		t.Fatalf("monitor missing CSRF=%d", code)
	}
	code, monitor := request("POST", collection, input, csrf)
	if code != 201 || monitor["status"] != "unknown" || monitor["intervalSeconds"] != float64(60) || monitor["timeoutSeconds"] != float64(20) || monitor["enabled"] != true {
		t.Fatalf("monitor create/defaults=%d %+v", code, monitor)
	}
	path := collection + "/" + monitor["id"].(string)
	code, list := request("GET", collection, "", "")
	if code != 200 || len(list["data"].([]any)) != 1 {
		t.Fatalf("monitor list=%d %+v", code, list)
	}
	if code, _ = request("POST", collection, strings.Replace(input, `"GET"`, `"SSL"`, 1), csrf); code != 400 {
		t.Fatalf("unimplemented monitor type=%d", code)
	}
	if code, _ = request("POST", collection, strings.Replace(input, `"`+project+`"`, `"foreign-project"`, 1), csrf); code != 404 {
		t.Fatalf("foreign project=%d", code)
	}
	code, checks := request("GET", path+"/checks", "", "")
	if code != 200 || len(checks["data"].([]any)) != 0 || checks["hasMore"] != false {
		t.Fatalf("initial checks=%d %+v", code, checks)
	}
	for _, query := range []string{"?changes=perhaps", "?page=-1", "?page=1000001"} {
		if code, _ = request("GET", path+"/checks"+query, "", ""); code != 400 {
			t.Fatalf("invalid check query %s=%d", query, code)
		}
	}
	monitor["enabled"] = false
	config := func(item map[string]any) string {
		in := map[string]any{}
		for _, key := range []string{"name", "type", "url", "project", "intervalSeconds", "timeoutSeconds", "expectedStatus", "expectedBody", "confirmationThreshold", "enabled"} {
			in[key] = item[key]
		}
		body, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	code, paused := request("PUT", path, config(monitor), csrf)
	if code != 200 || paused["enabled"] != false {
		t.Fatalf("pause=%d %+v", code, paused)
	}
	monitor["type"] = "HEAD"
	if code, _ = request("PUT", path, config(monitor), csrf); code != 409 {
		t.Fatalf("type changed=%d", code)
	}
	code, hb := request("POST", collection, `{"name":"Nightly backup","type":"heartbeat","intervalSeconds":120}`, csrf)
	if code != 201 || !strings.HasPrefix(hb["heartbeatURL"].(string), "http://example.test/api/v1/heartbeat/") {
		t.Fatalf("heartbeat create=%d %+v", code, hb)
	}
	hbURL := base + strings.TrimPrefix(hb["heartbeatURL"].(string), "http://example.test")
	res, err := http.Post(hbURL, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	var check map[string]any
	err = json.NewDecoder(res.Body).Decode(&check)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 || check["up"] != true || check["status"] != "up" || check["isChange"] != true {
		t.Fatalf("anonymous heartbeat=%d %+v %v", res.StatusCode, check, err)
	}
	res, err = http.Get(hbURL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 405 {
		t.Fatalf("heartbeat GET=%d", res.StatusCode)
	}
	res, err = http.Post(hbURL, "text/plain", strings.NewReader(strings.Repeat("x", 1025)))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 413 {
		t.Fatalf("oversized heartbeat=%d", res.StatusCode)
	}
	hbPath := collection + "/" + hb["id"].(string)
	code, checks = request("GET", hbPath+"/checks?changes=false", "", "")
	if code != 200 || len(checks["data"].([]any)) != 1 {
		t.Fatalf("heartbeat history=%d %+v", code, checks)
	}
	hb["enabled"] = false
	if code, _ = request("PUT", hbPath, config(hb), csrf); code != 200 {
		t.Fatalf("pause heartbeat=%d", code)
	}
	res, err = http.Post(hbURL, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("paused heartbeat=%d", res.StatusCode)
	}
	for _, target := range []string{path, hbPath} {
		if code, _ = request("DELETE", target, "", csrf); code != 204 {
			t.Fatalf("monitor delete=%d", code)
		}
		if code, _ = request("GET", target, "", ""); code != 404 {
			t.Fatalf("deleted monitor read=%d", code)
		}
	}
}
