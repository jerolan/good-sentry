package main

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

const demoError = "Intentional demo exception from an app using sentry-go"

var homePage = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GoodSentry SDK demo</title>
  <style>
    body { max-width: 42rem; margin: 4rem auto; padding: 0 1.25rem; color: #20212a; font: 16px/1.6 system-ui, sans-serif; }
    main { padding: 2rem; border: 1px solid #d9dbe4; border-radius: 1rem; }
    button { padding: .75rem 1rem; border: 0; border-radius: .5rem; background: #6259e8; color: white; font: inherit; cursor: pointer; }
    code { color: #5148d6; }
  </style>
</head>
<body>
  <main>
    <h1>GoodSentry SDK demo</h1>
    <p>This Go app uses the official <code>sentry-go</code> SDK. Press the button to send a real exception to the configured DSN.</p>
    <form method="post" action="/capture-error"><button type="submit">Send a demo exception</button></form>
  </main>
</body>
</html>`))

func main() {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		log.Fatal("SENTRY_DSN is required; copy it from a GoodSentry project")
	}

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      envOr("SENTRY_ENVIRONMENT", "demo"),
		Release:          envOr("SENTRY_RELEASE", "goodsentry-sentry-go-demo@1.0.0"),
		AttachStacktrace: true,
	}); err != nil {
		log.Fatalf("initialize Sentry SDK: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := homePage.Execute(w, nil); err != nil {
			log.Printf("render home page: %v", err)
		}
	})
	mux.HandleFunc("POST /capture-error", captureError)

	addr := envOr("ADDR", ":8081")
	log.Printf("Demo app listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func captureError(w http.ResponseWriter, _ *http.Request) {
	eventID := sentry.CaptureException(errors.New(demoError))
	if eventID == nil {
		http.Error(w, "The Sentry SDK did not capture the exception.", http.StatusInternalServerError)
		return
	}
	if !sentry.Flush(3 * time.Second) {
		http.Error(w, "The event has not finished sending yet. Check the GoodSentry project shortly.", http.StatusGatewayTimeout)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, "Sent event %s to GoodSentry. Check the Issues page for: %s\n", *eventID, demoError)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
