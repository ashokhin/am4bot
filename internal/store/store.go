// Package store is the Postgres-backed data layer for the multi-tenant
// control plane (users, their VPN identities, and their nodes). It is used
// by cmd/apiserver; cmd/ambot never imports it directly — it stays
// single-tenant, reading a plain config.yaml as before.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"

	// registers the "pgx" driver with database/sql, which sqlx.Open uses below
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the database connection and every repository method for the
// control plane's tables.
type Store struct {
	db *sqlx.DB
}

// Open connects to Postgres at dsn and applies any migration files under
// migrations/ that haven't run yet (tracked in a schema_migrations table).
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sqlx.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: opening database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()

		return nil, fmt.Errorf("store: connecting to database: %w", err)
	}

	s := &Store{db: db}

	if err := s.migrate(ctx); err != nil {
		db.Close()

		return nil, fmt.Errorf("store: applying migrations: %w", err)
	}

	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate applies every migrations/*.sql file, in filename order, that
// isn't already recorded in schema_migrations. Each file runs inside its
// own transaction, so a failure partway through a file rolls that file
// back rather than leaving the schema half-migrated.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}

	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := s.db.GetContext(ctx, &applied,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)`, name,
		); err != nil {
			return fmt.Errorf("checking migration state for %s: %w", name, err)
		}

		if applied {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		slog.Info("applying migration", "file", name)

		tx, err := s.db.BeginTxx(ctx, nil)
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			tx.Rollback()

			return fmt.Errorf("applying migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`, name,
		); err != nil {
			tx.Rollback()

			return fmt.Errorf("recording migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", name, err)
		}
	}

	return nil
}
