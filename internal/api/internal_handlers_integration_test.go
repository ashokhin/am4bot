//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/ashokhin/am4bot/internal/config"
	"github.com/ashokhin/am4bot/internal/secrets"
	"github.com/ashokhin/am4bot/internal/store"
)

// mustCreateNodeWithToken creates a user + node directly through the store
// (bypassing HTTP, since this test is about the internal endpoint, not the
// public API) and mints a config token for it the way the orchestrator
// eventually will: generate a random value, encrypt it, store it, hand
// back the plaintext for the test to present as a bearer token.
func mustCreateNodeWithToken(t *testing.T, st *store.Store, enc *secrets.Encryptor, plainToken string) *store.Node {
	t.Helper()

	ctx := context.Background()

	u, err := st.CreateUser(ctx, "friend@example.com", "hash", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	gamePasswordEnc, err := enc.Encrypt("gamepass1")
	if err != nil {
		t.Fatalf("Encrypt(game password) error = %v", err)
	}

	n, err := st.CreateNode(ctx, store.NewNodeParams{
		UserID:          u.ID,
		Name:            "departure",
		GameURL:         "https://www.airlinemanager.com/",
		GameUsername:    "player1",
		GamePasswordEnc: gamePasswordEnc,
		Services:        []string{"buy_fuel", "marketing", "depart"},
		CronSchedules:   []string{"0 8 * * 1-5"},
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	tokenEnc, err := enc.Encrypt(plainToken)
	if err != nil {
		t.Fatalf("Encrypt(token) error = %v", err)
	}

	if err := st.SetNodeConfigToken(ctx, n.ID, tokenEnc); err != nil {
		t.Fatalf("SetNodeConfigToken() error = %v", err)
	}

	n.ConfigTokenEnc = &tokenEnc

	return n
}

func getInternalConfigByID(t *testing.T, baseURL string, nodeID int64, token string) *http.Response {
	t.Helper()

	req, err := http.NewRequest("GET", baseURL+"/internal/nodes/"+strconv.FormatInt(nodeID, 10)+"/config", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /internal/nodes/%d/config: %v", nodeID, err)
	}

	return resp
}

func TestInternalConfigEndpointReturnsUsableConfig(t *testing.T) {
	srv, _, st, enc := testServerWithDeps(t)

	n := mustCreateNodeWithToken(t, st, enc, "node-secret-token")

	resp := getInternalConfigByID(t, srv.URL, n.ID, "node-secret-token")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var cfg config.Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decoding config: %v", err)
	}

	if cfg.User != "player1" {
		t.Fatalf("User = %q, want player1", cfg.User)
	}
	if cfg.Password != "gamepass1" {
		t.Fatalf("Password = %q, want the decrypted game password", cfg.Password)
	}
	if len(cfg.Services) != 3 || cfg.Services[0] != "buy_fuel" || cfg.Services[2] != "depart" {
		t.Fatalf("Services = %v, want order preserved [buy_fuel marketing depart]", cfg.Services)
	}
	// a field never set on the node, must come from config's own defaults
	if cfg.TimeoutSeconds != 180 {
		t.Fatalf("TimeoutSeconds = %d, want the package default 180", cfg.TimeoutSeconds)
	}
}

func TestInternalConfigEndpointRejectsWrongToken(t *testing.T) {
	srv, _, st, enc := testServerWithDeps(t)

	n := mustCreateNodeWithToken(t, st, enc, "node-secret-token")

	resp := getInternalConfigByID(t, srv.URL, n.ID, "wrong-token")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestInternalConfigEndpointRejectsMissingToken(t *testing.T) {
	srv, _, st, enc := testServerWithDeps(t)

	n := mustCreateNodeWithToken(t, st, enc, "node-secret-token")

	resp := getInternalConfigByID(t, srv.URL, n.ID, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestInternalConfigEndpointRejectsNodeWithNoToken(t *testing.T) {
	srv, _, st, _ := testServerWithDeps(t)

	ctx := context.Background()
	u, err := st.CreateUser(ctx, "friend@example.com", "hash", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	n, err := st.CreateNode(ctx, store.NewNodeParams{
		UserID:       u.ID,
		Name:         "departure",
		GameURL:      "https://www.airlinemanager.com/",
		GameUsername: "player1",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	// no SetNodeConfigToken call: ConfigTokenEnc stays nil, as if the
	// orchestrator has never provisioned this node yet.
	resp := getInternalConfigByID(t, srv.URL, n.ID, "anything")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
