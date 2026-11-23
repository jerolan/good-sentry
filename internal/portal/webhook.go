package portal

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func validWebhookURL(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 2048 || strings.Contains(value, "#") {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.Hostname() != "" && u.User == nil && len(u.Opaque) == 0
}

var webhookClient = &http.Client{
	Timeout:       3 * time.Second,
	Transport:     &http.Transport{Proxy: nil, IdleConnTimeout: 90 * time.Second},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func sendWebhook(destination string, projectID, issueID int64, eventID string) {
	payload, err := json.Marshal(struct {
		ProjectID int64  `json:"project_id"`
		IssueID   int64  `json:"issue_id"`
		EventID   string `json:"event_id"`
	}{projectID, issueID, eventID})
	if err != nil {
		log.Printf("webhook delivery failed: project_id=%d issue_id=%d event_id=%s", projectID, issueID, eventID)
		return
	}
	req, err := http.NewRequest(http.MethodPost, destination, bytes.NewReader(payload))
	if err != nil {
		log.Printf("webhook delivery failed: project_id=%d issue_id=%d event_id=%s", projectID, issueID, eventID)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := webhookClient.Do(req)
	if err != nil {
		log.Printf("webhook delivery failed: project_id=%d issue_id=%d event_id=%s", projectID, issueID, eventID)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("webhook delivery failed: project_id=%d issue_id=%d event_id=%s status=%d", projectID, issueID, eventID, resp.StatusCode)
	}
}
