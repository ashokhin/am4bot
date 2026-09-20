//go:build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := rawDB.Exec(`DROP TABLE IF EXISTS node_operations, nodes, vpn_regions, vpn_provider_credentials, prometheus_settings, login_ban_events, login_attempts, login_bans, users, schema_migrations CASCADE`); err != nil {
		t.Fatalf("resetting schema: %v", err)
	}
	rawDB.Close()

	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	return st
}

func TestEnsureBootstrapAdminCreatesOneOnAnEmptyDatabase(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if err := ensureBootstrapAdmin(ctx, st); err != nil {
		t.Fatalf("ensureBootstrapAdmin() error = %v", err)
	}

	u, err := st.GetUserByLogin(ctx, bootstrapAdminLogin)
	if err != nil {
		t.Fatalf("GetUserByLogin() error = %v", err)
	}
	if !u.IsAdmin {
		t.Fatal("bootstrap account is not an admin")
	}
	if !u.MustChangePassword {
		t.Fatal("bootstrap account does not require a password change")
	}
	if !auth.VerifyPassword(u.PasswordHash, bootstrapAdminPassword) {
		t.Fatal("bootstrap account's password does not verify against the well-known default")
	}
}

func TestEnsureBootstrapAdminIsANoOpOnceAnyUserExists(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	hash, err := auth.HashPassword("whatever123")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if _, err := st.CreateUser(ctx, "someone", hash, false); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	if err := ensureBootstrapAdmin(ctx, st); err != nil {
		t.Fatalf("ensureBootstrapAdmin() error = %v", err)
	}

	if _, err := st.GetUserByLogin(ctx, bootstrapAdminLogin); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected no bootstrap admin to have been created, GetUserByLogin() error = %v", err)
	}
}
