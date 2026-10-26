// Run against a disposable GoodSentry instance: GOODSENTRY_DSN=<project dsn> go run ./logssdk
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

func main() {
	dsn := os.Getenv("GOODSENTRY_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "GOODSENTRY_DSN is required")
		os.Exit(2)
	}
	run := os.Getenv("GOODSENTRY_RUN")
	if run == "" {
		run = fmt.Sprint(time.Now().UnixNano())
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      "logs-go-acceptance",
		Release:          "logs-go@1.0.0",
		TracesSampleRate: 1,
		Debug:            false,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sentry.Flush(10 * time.Second)

	hub := sentry.CurrentHub().Clone()
	ctx := sentry.SetHubOnContext(context.Background(), hub)
	tx := sentry.StartTransaction(ctx, "logs-go-acceptance", sentry.WithTransactionSource(sentry.SourceTask))
	ctx = tx.Context()
	span := tx.StartChild("op.logs.emit")

	logger := sentry.NewLogger(ctx)
	logger.SetAttributes(
		attribute.String("service.name", "logs-go-service"),
		attribute.String("acceptance.run", run),
	)
	logger.Info().
		String("component", "logs-go").
		Int64("extreme", 9223372036854775807).
		Bool("healthy", false).
		Float64("ratio", 1.5).
		Emit("Go SDK info log " + run)
	logger.Warn().
		String("reason", "filter-target").
		Emit("Go SDK warn log " + run)
	logger.Error().
		Int("attempt", 3).
		Emit("Go SDK error log " + run)

	span.Finish()
	tx.Finish()
	fmt.Println("trace", tx.TraceID.String(), "span", span.SpanID.String())
}
