package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrCredentialsChanged requires a fresh sign-in after account mutation.
	ErrCredentialsChanged = errors.New("credentials changed")
	ErrNotFound           = errors.New("not found")
	ErrConflict           = errors.New("conflict")
	ErrForbidden          = errors.New("forbidden")
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	cfg.MaxConns = 8
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &Store{pool: p}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err = s.migrate(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close()                          { s.pool.Close() }
func (s *Store) Ready(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73042189)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(version integer PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, file := range files {
		prefix, _, ok := strings.Cut(file.Name(), "_")
		version, parseErr := strconv.Atoi(prefix)
		if !ok || parseErr != nil {
			return fmt.Errorf("invalid embedded migration name %q", file.Name())
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(body))
		var existing string
		err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", version).Scan(&existing)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err = tx.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("apply migration %d: %w", version, err)
			}
			if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", version, digest); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if existing != digest {
			return fmt.Errorf("migration %d checksum differs from applied migration", version)
		}
	}
	return tx.Commit(ctx)
}

func dbError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return ErrConflict
	}
	return err
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func Slug(name string) string {
	slug := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-")
	}
	if slug == "" {
		slug = "project"
	}
	return slug
}

func randomKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
