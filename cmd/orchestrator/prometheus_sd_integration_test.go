//go:build integration

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ashokhin/am4bot/internal/store"
)

func openTestStoreForSD(t *testing.T) *store.Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := rawDB.Exec(`DROP TABLE IF EXISTS node_operations, nodes, vpn_configs, vpn_regions, vpn_provider_credentials, prometheus_settings, login_ban_events, login_attempts, login_bans, users, schema_migrations CASCADE`); err != nil {
		t.Fatalf("resetting schema: %v", err)
	}
	rawDB.Close()

	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	return st
}

func TestWritePrometheusSDFile(t *testing.T) {
	st := openTestStoreForSD(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "friend@example.com", "hash", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	n, err := st.CreateNode(ctx, store.NewNodeParams{
		UserID: u.ID, Name: "departure", GameURL: "https://www.airlinemanager.com/", GameUsername: "player1",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	if _, _, err := st.EnsureNodeProvisioned(ctx, n.ID, "localhost", 9200, 9299); err != nil {
		t.Fatalf("EnsureNodeProvisioned() error = %v", err)
	}

	sdFile := filepath.Join(t.TempDir(), "targets.json")
	r := &reconciler{store: st, prometheusSDFile: sdFile}

	r.writePrometheusSDFile(ctx)

	data, err := os.ReadFile(sdFile)
	if err != nil {
		t.Fatalf("reading generated sd file: %v", err)
	}

	var targets []prometheusSDTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		t.Fatalf("parsing generated sd file: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("targets = %+v, want exactly 1", targets)
	}
	if targets[0].Labels["user_uuid"] != u.UUID.String() {
		t.Fatalf("target user_uuid label = %q, want %q", targets[0].Labels["user_uuid"], u.UUID.String())
	}
	if len(targets[0].Targets) != 1 || targets[0].Targets[0][:len("localhost:")] != "localhost:" {
		t.Fatalf("target address = %+v, want a localhost:<port> entry", targets[0].Targets)
	}

	// a no-op flag (unset prometheusSDFile) must not touch the filesystem.
	r2 := &reconciler{store: st, prometheusSDFile: ""}
	r2.writePrometheusSDFile(ctx)
}
