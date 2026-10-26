// Run against a disposable GoodSentry instance with a project DSN:
// GOODSENTRY_DSN=<dsn> GOODSENTRY_RUN=<run> GOOS... go run ./errorsgo
//
// The driver uses the pinned official sentry-go client and its default network
// transport. It records the exact event IDs, SDK version and supplied metadata
// so an independent controller can verify the server receipt through API and UI.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

type result struct {
	SDKVersion  string   `json:"sdkVersion"`
	Mode        string   `json:"mode"`
	Run         string   `json:"run"`
	Message     string   `json:"message"`
	Environment string   `json:"environment"`
	Release     string   `json:"release"`
	Tag         string   `json:"tag"`
	UserID      string   `json:"userID"`
	Fingerprint []string `json:"fingerprint,omitempty"`
	EventIDs    []string `json:"eventIDs"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := strings.TrimSpace(os.Getenv("GOODSENTRY_DSN"))
	if dsn == "" {
		return errors.New("GOODSENTRY_DSN is required")
	}
	runID := strings.TrimSpace(os.Getenv("GOODSENTRY_RUN"))
	if runID == "" {
		runID = fmt.Sprint(time.Now().UnixNano())
	}
	mode := env("GOODSENTRY_ERRORS_MODE", "single")
	message := env("GOODSENTRY_ERRORS_MESSAGE", "Go SDK error "+runID)
	environment := "errors-go-acceptance"
	release := "errors-go@1.0.0"
	tag := "errors-go-" + runID
	userID := "errors-go-user-" + runID

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		AttachStacktrace: true,
		Environment:      environment,
		Release:          release,
		TracesSampleRate: 0,
	}); err != nil {
		return fmt.Errorf("initialize official sentry-go: %w", err)
	}
	defer sentry.Flush(10 * time.Second)

	result := result{SDKVersion: sentry.SDKVersion, Mode: mode, Run: runID, Message: message, Environment: environment, Release: release, Tag: tag, UserID: userID}
	switch mode {
	case "single":
		sentry.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetTag("acceptance.run", tag)
			scope.SetTag("component", "errors-go")
			scope.SetContext("acceptance", map[string]any{"run": runID, "kind": "error"})
			scope.SetUser(sentry.User{ID: userID})
		})
		eventID := sentry.CaptureException(errors.New(message))
		if eventID == nil {
			return errors.New("official sentry-go dropped the exception")
		}
		if !sentry.Flush(10 * time.Second) {
			return errors.New("official sentry-go failed to flush the exception")
		}
		result.EventIDs = []string{string(*eventID)}
	case "group":
		count, err := strconv.Atoi(env("GOODSENTRY_ERRORS_COUNT", "3"))
		if err != nil || count < 1 || count > 61 {
			return errors.New("GOODSENTRY_ERRORS_COUNT must be 1..61")
		}
		fingerprint := []string{"errors-go-group", runID}
		result.Fingerprint = fingerprint
		for index := 0; index < count; index++ {
			sentry.ConfigureScope(func(scope *sentry.Scope) {
				scope.SetFingerprint(fingerprint)
				scope.SetTag("acceptance.run", tag)
				scope.SetTag("component", "errors-go")
				scope.SetContext("acceptance", map[string]any{"run": runID, "kind": "error", "occurrence": index})
				scope.SetUser(sentry.User{ID: userID})
			})
			eventID := sentry.CaptureException(errors.New(message))
			if eventID == nil {
				return fmt.Errorf("official sentry-go dropped grouping capture %d", index)
			}
			if !sentry.Flush(10 * time.Second) {
				return fmt.Errorf("official sentry-go failed to flush grouping capture %d", index)
			}
			result.EventIDs = append(result.EventIDs, string(*eventID))
		}
	case "retry":
		fixed := strings.TrimSpace(os.Getenv("GOODSENTRY_ERRORS_EVENT_ID"))
		if len(fixed) != 32 {
			return errors.New("GOODSENTRY_ERRORS_EVENT_ID must be 32 hex characters")
		}
		fingerprint := []string{"errors-go-retry", runID}
		result.Fingerprint = fingerprint
		for attempt := 0; attempt < 2; attempt++ {
			event := sentry.NewEvent()
			event.EventID = sentry.EventID(fixed)
			event.Message = message
			event.Level = sentry.LevelError
			event.Platform = "go"
			event.Timestamp = time.Now().UTC()
			event.Environment = environment
			event.Release = release
			event.Fingerprint = fingerprint
			event.Tags = map[string]string{"acceptance.run": tag, "component": "errors-go"}
			event.Contexts = map[string]sentry.Context{"acceptance": {"run": runID, "kind": "error", "attempt": attempt}}
			event.User = sentry.User{ID: userID}
			event.Exception = []sentry.Exception{{Type: "errors.error", Value: message, Stacktrace: sentry.NewStacktrace()}}
			id := sentry.CaptureEvent(event)
			if id == nil {
				return fmt.Errorf("official sentry-go dropped retry attempt %d", attempt)
			}
			if !sentry.Flush(10 * time.Second) {
				return fmt.Errorf("official sentry-go failed to flush retry attempt %d", attempt)
			}
			result.EventIDs = append(result.EventIDs, string(*id))
		}
	default:
		return fmt.Errorf("unknown GOODSENTRY_ERRORS_MODE %q", mode)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if output := os.Getenv("GOODSENTRY_ERRORS_OUTPUT"); output != "" {
		if err := os.WriteFile(output, encoded, 0o600); err != nil {
			return err
		}
	}
	fmt.Println(string(encoded))
	return nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
