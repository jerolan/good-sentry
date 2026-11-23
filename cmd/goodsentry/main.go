// Command goodsentry runs the GoodSentry portal or migrates its PostgreSQL database.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jerolan/good-sentry/internal/portal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	command := "serve"
	if len(args) == 1 {
		command = args[0]
	}
	if len(args) > 1 || (command != "serve" && command != "migrate") {
		return errors.New("usage: goodsentry [serve|migrate]")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("invalid PostgreSQL configuration")
	}
	defer db.Close()

	if command == "migrate" {
		migrationCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		if err := portal.Migrate(migrationCtx, db); err != nil {
			return errors.New("PostgreSQL migration failed")
		}
		log.Print("PostgreSQL migrations applied")
		return nil
	}

	publicURL, err := url.Parse(os.Getenv("PUBLIC_URL"))
	if err != nil || publicURL.Hostname() == "" || publicURL.User != nil ||
		(publicURL.Scheme != "http" && publicURL.Scheme != "https") ||
		strings.TrimSuffix(publicURL.Path, "/") != "" || publicURL.RawQuery != "" ||
		publicURL.ForceQuery || publicURL.Fragment != "" {
		return errors.New("PUBLIC_URL must be an absolute HTTP or HTTPS origin")
	}
	user, password := os.Getenv("PORTAL_USER"), os.Getenv("PORTAL_PASSWORD")
	if user == "" || strings.Contains(user, ":") || password == "" {
		return errors.New("PORTAL_USER without a colon and PORTAL_PASSWORD are required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return errors.New("PORT must be between 1 and 65535")
	}

	startupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = portal.CheckSchema(startupCtx, db)
	cancel()
	if err != nil {
		return errors.New("PostgreSQL schema is unavailable; check DATABASE_URL and run goodsentry migrate")
	}

	server := &http.Server{
		Addr:              net.JoinHostPort("", port),
		Handler:           portal.New(db, *publicURL, user, password),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       time.Minute,
	}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()

	select {
	case err := <-done:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
