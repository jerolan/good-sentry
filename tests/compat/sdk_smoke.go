// Run against a disposable GoodSentry instance with:
// GOODSENTRY_URL=http://127.0.0.1:18000 go run .
// COMPAT_EMAIL and COMPAT_PASSWORD select the account on an initialized server.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

const exceptionMessage = "GoodSentry SDK acceptance"

type apiClient struct {
	base string
	http *http.Client
	csrf string
}

type meResponse struct {
	Organizations []struct {
		Slug string `json:"slug"`
	} `json:"organizations"`
	CSRFToken string `json:"csrfToken"`
}

type project struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	DSN  string `json:"dsn"`
}

type issue struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type issueDetail struct {
	Issue issue `json:"issue"`
	Event struct {
		EventID   string `json:"event_id"`
		Exception []struct {
			Value string `json:"value"`
		} `json:"exception"`
	} `json:"event"`
	Events []struct {
		EventID string `json:"eventID"`
	} `json:"events"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func run() error {
	base, err := url.Parse(env("GOODSENTRY_URL", "http://127.0.0.1:18000"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return errors.New("GOODSENTRY_URL must be an absolute HTTP or HTTPS URL")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := apiClient{
		base: strings.TrimRight(base.String(), "/"),
		http: &http.Client{Jar: jar, Timeout: 15 * time.Second},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var setup struct {
		Required bool `json:"required"`
	}
	if err := client.json(ctx, http.MethodGet, "/api/v1/setup", nil, http.StatusOK, &setup); err != nil {
		return err
	}
	credentials := map[string]string{
		"email":    env("COMPAT_EMAIL", "compat@example.test"),
		"password": env("COMPAT_PASSWORD", "long test pw"),
	}
	var me meResponse
	if setup.Required {
		credentials["name"] = "SDK Compatibility"
		credentials["organization"] = "SDK Compatibility"
		err = client.json(ctx, http.MethodPost, "/api/v1/setup", credentials, http.StatusCreated, &me)
	} else {
		err = client.json(ctx, http.MethodPost, "/api/v1/login", credentials, http.StatusOK, &me)
	}
	if err != nil {
		return err
	}
	if len(me.Organizations) == 0 || me.Organizations[0].Slug == "" || me.CSRFToken == "" {
		return errors.New("authentication response omitted organization or CSRF token")
	}
	client.csrf = me.CSRFToken
	orgPath := "/api/v1/organizations/" + url.PathEscape(me.Organizations[0].Slug)
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	runID := hex.EncodeToString(nonce[:])
	var created project
	if err := client.json(ctx, http.MethodPost, orgPath+"/projects", map[string]string{
		"name": "Go SDK " + runID, "platform": "go",
	}, http.StatusCreated, &created); err != nil {
		return err
	}
	if created.ID == "" || created.Slug == "" || created.DSN == "" {
		return errors.New("project response omitted ID, slug or DSN")
	}
	options := sentry.ClientOptions{
		Dsn: created.DSN, AttachStacktrace: true,
		EnableTracing: true, TracesSampleRate: 1,
		Environment: "sdk-compat", Release: "goodsentry-compat@1",
	}
	if os.Getenv("COMPAT_DEBUG") != "" {
		options.HTTPTransport = sdkDebugTransport{base: http.DefaultTransport}
	}
	if err := sentry.Init(options); err != nil {
		return fmt.Errorf("initialize official Sentry Go SDK: %w", err)
	}
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("compat.run", runID)
	})
	eventID := sentry.CaptureException(errors.New(exceptionMessage))
	if eventID == nil {
		return errors.New("SDK dropped the captured exception")
	}
	transactionName := "GoodSentry SDK transaction " + runID
	transaction := sentry.StartTransaction(ctx, transactionName, sentry.WithOpName("compat.sdk"))
	child := transaction.StartChild("compat.child")
	child.Description = "SDK acceptance child"
	child.SetData("acceptance", true)
	logMessage := "GoodSentry SDK log " + runID
	sentry.NewLogger(transaction.Context()).Info().
		String("compat.run", runID).
		Int("compat.attempt", 7).
		Bool("compat.ready", true).
		Float64("compat.ratio", 1.5).
		Emit(logMessage)
	child.Finish()
	transaction.Finish()
	if !sentry.Flush(10 * time.Second) {
		return errors.New("official SDK failed to flush within 10 seconds")
	}

	if err := client.verifyException(ctx, orgPath, string(*eventID)); err != nil {
		return err
	}
	fmt.Printf("PASS: official sentry-go v0.50.0 exception %s persisted and visible in project %s\n", *eventID, created.Slug)
	if err := client.verifyTransaction(ctx, orgPath, created.ID, transactionName, transaction, child); err != nil {
		return err
	}
	fmt.Printf("PASS: SDK transaction %q aggregated and event occurrence retained root/child timing, parent and raw context for trace %s\n", transactionName, transaction.TraceID.String())
	if err := client.verifyLog(ctx, orgPath, logMessage, transaction.TraceID.String(), runID); err != nil {
		return err
	}
	fmt.Printf("PASS: SDK log %q retained trace %s and string/integer/boolean/double attributes\n", logMessage, transaction.TraceID.String())
	return nil
}

func (c *apiClient) verifyException(ctx context.Context, orgPath, eventID string) error {
	for attempt := 0; attempt < 25; attempt++ {
		var listed struct {
			Data []issue `json:"data"`
		}
		if err := c.json(ctx, http.MethodGet, orgPath+"/issues?query="+url.QueryEscape(exceptionMessage), nil, http.StatusOK, &listed); err != nil {
			return err
		}
		for _, candidate := range listed.Data {
			var detail issueDetail
			if err := c.json(ctx, http.MethodGet, "/api/v1/issues/"+url.PathEscape(candidate.ID), nil, http.StatusOK, &detail); err != nil {
				return err
			}
			if detail.Event.EventID != eventID {
				continue
			}
			if !strings.Contains(detail.Issue.Title, exceptionMessage) {
				return fmt.Errorf("event %s has unexpected issue title %q", eventID, detail.Issue.Title)
			}
			hasException := false
			for _, exception := range detail.Event.Exception {
				hasException = hasException || exception.Value == exceptionMessage
			}
			if !hasException {
				return fmt.Errorf("event %s lost the SDK exception value", eventID)
			}
			for _, event := range detail.Events {
				if event.EventID == eventID {
					return nil
				}
			}
			return fmt.Errorf("event %s is missing from the issue's event list", eventID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("captured event %s was not visible through the authenticated issue API", eventID)
}

type transactionReference struct {
	Source    string `json:"source"`
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

func (c *apiClient) verifyTransaction(ctx context.Context, orgPath, projectID, name string, root, child *sentry.Span) error {
	traceID := root.TraceID.String()
	for attempt := 0; attempt < 25; attempt++ {
		var listed struct {
			Data []struct {
				Transaction   string               `json:"transaction"`
				Op            string               `json:"op"`
				Count         int                  `json:"count"`
				ErrorCount    int                  `json:"errorCount"`
				AvgDuration   float64              `json:"avgDuration"`
				P50           float64              `json:"p50"`
				P95           float64              `json:"p95"`
				LatestTraceID string               `json:"latestTraceId"`
				LatestSample  transactionReference `json:"latestSample"`
			} `json:"data"`
		}
		if err := c.json(ctx, http.MethodGet, orgPath+"/transactions", nil, http.StatusOK, &listed); err != nil {
			return err
		}
		for _, transaction := range listed.Data {
			if transaction.Transaction != name {
				continue
			}
			if transaction.LatestTraceID != traceID || transaction.Op != "compat.sdk" || transaction.Count != 1 || transaction.ErrorCount != 0 {
				return fmt.Errorf("SDK transaction aggregate lost identity or counts: %+v", transaction)
			}
			if transaction.AvgDuration <= 0 || transaction.P50 <= 0 || transaction.P95 < transaction.P50 {
				return fmt.Errorf("SDK transaction aggregate has invalid duration metrics: %+v", transaction)
			}
			ref := transaction.LatestSample
			id, err := strconv.ParseInt(ref.ID, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != ref.ID || ref.Source != "event" || ref.ProjectID != projectID {
				return fmt.Errorf("Go aggregate has invalid event occurrence reference: %+v", ref)
			}
			var detail struct {
				Sample         transactionReference `json:"sample"`
				EventID        string               `json:"eventId"`
				TraceID        string               `json:"traceId"`
				SpanID         string               `json:"spanId"`
				ParentSpanID   string               `json:"parentSpanId"`
				Name           string               `json:"name"`
				Op             string               `json:"op"`
				Environment    string               `json:"environment"`
				Release        string               `json:"release"`
				Duration       float64              `json:"duration"`
				StartTimestamp *time.Time           `json:"startTimestamp"`
				EndTimestamp   time.Time            `json:"endTimestamp"`
				SpanScope      string               `json:"spanScope"`
				SpanCount      int                  `json:"spanCount"`
				Page           int                  `json:"page"`
				PageSize       int                  `json:"pageSize"`
				HasMore        bool                 `json:"hasMore"`
				Data           struct {
					EventID     string `json:"event_id"`
					Transaction string `json:"transaction"`
					SDK         struct {
						Name    string `json:"name"`
						Version string `json:"version"`
					} `json:"sdk"`
					Contexts struct {
						Trace struct {
							TraceID string `json:"trace_id"`
							SpanID  string `json:"span_id"`
							Op      string `json:"op"`
						} `json:"trace"`
					} `json:"contexts"`
				} `json:"data"`
				Spans []struct {
					ID             string     `json:"id"`
					SpanID         string     `json:"spanId"`
					ParentSpanID   string     `json:"parentSpanId"`
					Name           string     `json:"name"`
					Op             string     `json:"op"`
					StartTimestamp *time.Time `json:"startTimestamp"`
					EndTimestamp   *time.Time `json:"endTimestamp"`
					Duration       *float64   `json:"duration"`
					Data           struct {
						SpanID       string `json:"span_id"`
						ParentSpanID string `json:"parent_span_id"`
						Description  string `json:"description"`
						Op           string `json:"op"`
						Data         struct {
							Acceptance bool `json:"acceptance"`
						} `json:"data"`
					} `json:"data"`
				} `json:"spans"`
			}
			if err := c.json(ctx, http.MethodGet, orgPath+"/transactions/"+ref.Source+"/"+ref.ID+"?page=1", nil, http.StatusOK, &detail); err != nil {
				return err
			}
			if detail.Sample != ref || detail.TraceID != traceID || detail.SpanID != root.SpanID.String() || detail.ParentSpanID != "" || detail.Name != name || detail.Op != "compat.sdk" || detail.Environment != "sdk-compat" || detail.Release != "goodsentry-compat@1" {
				return fmt.Errorf("Go occurrence lost its project/root identity or context: %+v", detail)
			}
			if detail.StartTimestamp == nil || math.Abs(detail.StartTimestamp.Sub(root.StartTime).Seconds()) > 0.000002 || math.Abs(detail.EndTimestamp.Sub(root.EndTime).Seconds()) > 0.000002 || detail.Duration <= 0 || math.Abs(detail.Duration-root.EndTime.Sub(root.StartTime).Seconds()*1000) > 0.002 || detail.Duration != transaction.AvgDuration {
				return fmt.Errorf("Go occurrence lost SDK root timing: duration=%v start=%v end=%v", detail.Duration, detail.StartTimestamp, detail.EndTimestamp)
			}
			if detail.SpanScope != "transaction" || detail.SpanCount != 1 || len(detail.Spans) != 1 || detail.Page != 1 || detail.PageSize != 50 || detail.HasMore {
				return fmt.Errorf("Go occurrence child scope/pagination changed: scope=%s count=%d rows=%d", detail.SpanScope, detail.SpanCount, len(detail.Spans))
			}
			got := detail.Spans[0]
			if got.ID != "1" || got.SpanID != child.SpanID.String() || got.ParentSpanID != root.SpanID.String() || got.Name != "SDK acceptance child" || got.Op != "compat.child" || got.StartTimestamp == nil || got.EndTimestamp == nil || got.Duration == nil || *got.Duration < 0 || math.Abs(*got.Duration-child.EndTime.Sub(child.StartTime).Seconds()*1000) > 0.002 {
				return fmt.Errorf("Go occurrence lost SDK child name/parent/timing: %+v", got)
			}
			if detail.EventID == "" || detail.EventID != detail.Data.EventID || detail.Data.Transaction != name || detail.Data.SDK.Name != "sentry.go" || detail.Data.SDK.Version != "0.50.0" || detail.Data.Contexts.Trace.TraceID != traceID || detail.Data.Contexts.Trace.SpanID != root.SpanID.String() || detail.Data.Contexts.Trace.Op != "compat.sdk" || !got.Data.Data.Acceptance || got.Data.SpanID != child.SpanID.String() || got.Data.ParentSpanID != root.SpanID.String() || got.Data.Description != "SDK acceptance child" || got.Data.Op != "compat.child" {
				return errors.New("Go occurrence lost raw SDK/trace/child context")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("SDK transaction %q was not visible through the authenticated telemetry API", name)
}

func (c *apiClient) verifyLog(ctx context.Context, orgPath, body, traceID, runID string) error {
	type attribute struct {
		Type  string `json:"type"`
		Value any    `json:"value"`
	}
	wanted := map[string]attribute{
		"compat.run":     {Type: "string", Value: runID},
		"compat.attempt": {Type: "integer", Value: float64(7)},
		"compat.ready":   {Type: "boolean", Value: true},
		"compat.ratio":   {Type: "double", Value: 1.5},
	}
	for attempt := 0; attempt < 25; attempt++ {
		var listed struct {
			Data []struct {
				Body       string               `json:"body"`
				TraceID    string               `json:"traceId"`
				Level      string               `json:"level"`
				Attributes map[string]attribute `json:"attributes"`
			} `json:"data"`
		}
		if err := c.json(ctx, http.MethodGet, orgPath+"/logs?query="+url.QueryEscape(body), nil, http.StatusOK, &listed); err != nil {
			return err
		}
		for _, log := range listed.Data {
			if log.Body != body {
				continue
			}
			if log.TraceID != traceID || log.Level != "info" {
				return fmt.Errorf("SDK log lost trace correlation or severity: %+v", log)
			}
			for key, expected := range wanted {
				actual := log.Attributes[key]
				if actual.Type != expected.Type || !reflect.DeepEqual(actual.Value, expected.Value) {
					return fmt.Errorf("SDK log attribute %s changed: got %+v, want %+v", key, actual, expected)
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("SDK log %q was not visible through the authenticated logs API", body)
}

type apiStatusError struct {
	status  int
	message string
}

func (e *apiStatusError) Error() string { return e.message }

func (c *apiClient) json(ctx context.Context, method, path string, input any, expected int, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && c.csrf != "" {
		request.Header.Set("X-CSRF-Token", c.csrf)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != expected {
		return &apiStatusError{status: response.StatusCode, message: fmt.Sprintf("%s %s: got HTTP %d, want %d: %s", method, path, response.StatusCode, expected, strings.TrimSpace(string(payload)))}
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return fmt.Errorf("%s %s: invalid JSON response: %w", method, path, err)
	}
	return nil
}

// sdkDebugTransport observes only SDK ingestion traffic, never account credentials.
// It preserves both bodies so diagnostics do not change what the SDK sends or reads.
type sdkDebugTransport struct{ base http.RoundTripper }

func (t sdkDebugTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var sent []byte
	if request.Body != nil {
		var err error
		sent, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(sent))
	}
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return response, err
	}
	fmt.Fprintf(os.Stderr, "SDK %s HTTP %d %s\n", request.URL.Path, response.StatusCode, strings.TrimSpace(string(body)))
	if response.StatusCode >= http.StatusBadRequest && len(sent) > 0 {
		if err := os.WriteFile("/tmp/goodsentry-last-sdk-error.json", sent, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "SDK diagnostic capture failed: %v\n", err)
		}
	}
	return response, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
