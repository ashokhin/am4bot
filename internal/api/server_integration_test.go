//go:build integration

// Integration tests against a real Postgres instance. Run with:
//
//	go test -tags=integration ./internal/api/... -v
//
// requiring TEST_DATABASE_URL to point at a scratch database. Skipped by
// default (no build tag => not compiled) so `go test ./...` and CI never
// need a live Postgres -- same convention as internal/store's.
package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/secrets"
	"github.com/ashokhin/am4bot/internal/store"
)

// testServer builds a fresh Server (fresh database, fresh keys) and
// returns an httptest.Server plus a helper for making requests that
// carries cookies between calls like a browser would.
func testServer(t *testing.T) (*httptest.Server, *client) {
	t.Helper()

	srv, c, _, _ := testServerWithDeps(t)

	return srv, c
}

// testServerWithDeps is testServer, but also returns the Store and
// Encryptor backing it -- for tests (like the internal config endpoint's)
// that need to poke state testServer's callers can't reach through the
// HTTP API alone, e.g. minting a node's config token the way the
// orchestrator eventually will.
func testServerWithDeps(t *testing.T) (*httptest.Server, *client, *store.Store, *secrets.Encryptor) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	// pgx's driver is registered (as "pgx") by store's blank import of
	// pgx/v5/stdlib, transitively -- reuse it directly here just to drop
	// the schema before Store re-applies migrations from scratch.
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
		t.Fatalf("re-opening store after reset: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	masterKey, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	keyBytes, err := secrets.KeyFromBase64(masterKey)
	if err != nil {
		t.Fatalf("KeyFromBase64() error = %v", err)
	}
	enc, err := secrets.NewEncryptor(keyBytes)
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}

	tokens, err := auth.NewTokenManager(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}

	// NewServer returns two handlers meant for two separate listeners in
	// production (see its own doc comment) -- tests only need one
	// httptest.Server, so combine them behind a single outer mux instead
	// of standing up two. A minimal uiFS is enough: no test exercises the
	// embedded SPA's actual content, only /api/* and /internal/*, but
	// NewServer reads index.html at construction time (to build the SPA
	// fallback handler, see static.go's rewriteIndexHTML), so it has to
	// exist.
	uiFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><head></head><body></body></html>")},
	}

	publicHandler, internalHandler, err := NewServer(st, tokens, enc, uiFS, ServerOptions{
		CookieSecure:             false,
		OrchestratorToken:        "test-orchestrator-token",
		TargetHosts:              []string{"test-host"},
		PrometheusPortRangeStart: 9200,
		PrometheusPortRangeEnd:   9299,
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	combined := http.NewServeMux()
	combined.Handle("/internal/", internalHandler)
	combined.Handle("/", publicHandler)
	srv := httptest.NewServer(combined)
	t.Cleanup(srv.Close)

	return srv, &client{base: srv.URL, jar: map[string]string{}}, st, enc
}

// client is a minimal cookie-carrying HTTP client, just enough to drive
// these tests without pulling in a full cookie-jar dependency.
type client struct {
	base string
	jar  map[string]string
}

func (c *client) do(t *testing.T, method, path string, body any) (*http.Response, map[string]any) {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	for name, value := range c.jar {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	for _, ck := range resp.Cookies() {
		c.jar[ck.Name] = ck.Value
	}

	var decoded map[string]any
	if resp.StatusCode != http.StatusNoContent {
		_ = json.NewDecoder(resp.Body).Decode(&decoded) // best-effort; some responses are arrays, not objects
	}

	return resp, decoded
}

// doList is like do, but decodes a JSON array response instead of an
// object -- for list endpoints (GET /api/nodes, GET /api/admin/users).
func (c *client) doList(t *testing.T, method, path string) []map[string]any {
	t.Helper()

	req, err := http.NewRequest(method, c.base+path, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}

	for name, value := range c.jar {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decoding list response from %s %s: %v", method, path, err)
	}

	return list
}

func mustCreateAdmin(t *testing.T, st *store.Store, email, password string) {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if _, err := st.CreateUser(context.Background(), email, hash, true); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
}

func TestLoginAndMe(t *testing.T) {
	_, c := testServer(t)
	// re-derive the store from the same DSN to seed an admin directly
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")

	resp, body := c.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	if body["is_admin"] != true {
		t.Fatalf("login response is_admin = %v, want true", body["is_admin"])
	}

	resp, body = c.do(t, "GET", "/api/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me status = %d, want 200", resp.StatusCode)
	}
	if body["login"] != "admin@example.com" {
		t.Fatalf("/api/me email = %v, want admin@example.com", body["login"])
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	_, c := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")

	resp, _ := c.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login with wrong password status = %d, want 401", resp.StatusCode)
	}
}

func TestNonAdminCannotAccessAdminRoutes(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")

	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	resp, _ := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating user status = %d, want 201", resp.StatusCode)
	}

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	resp, _ = friend.do(t, "GET", "/api/admin/users", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin GET /api/admin/users status = %d, want 403", resp.StatusCode)
	}
}

func TestUnauthenticatedRequestRejected(t *testing.T) {
	_, c := testServer(t)

	resp, _ := c.do(t, "GET", "/api/nodes", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/nodes status = %d, want 401", resp.StatusCode)
	}
}

func TestCreatingUserAutoCreatesDefaultNode(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})

	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	nodes := friend.doList(t, "GET", "/api/nodes")

	if len(nodes) != 2 {
		t.Fatalf("new user has %d nodes, want exactly 2 (the auto-created \"player\" and \"maintenance\" defaults)", len(nodes))
	}

	gotNames := map[string]bool{}
	for _, n := range nodes {
		if n["is_default"] != true {
			t.Fatalf("auto-created node is_default = %v, want true: %+v", n["is_default"], n)
		}
		if n["enabled"] != false {
			t.Fatalf("auto-created node enabled = %v, want false (must be configured before it can run)", n["enabled"])
		}
		gotNames[n["name"].(string)] = true
	}
	if !gotNames["player"] || !gotNames["maintenance"] {
		t.Fatalf("auto-created node names = %v, want \"player\" and \"maintenance\"", gotNames)
	}
}

func TestDefaultNodeCannotBeDeleted(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	resp, _ := friend.do(t, "DELETE", "/api/nodes/1", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("deleting default node status = %d, want 409", resp.StatusCode)
	}
}

func TestNodeServiceOrderSurvivesCreateAndUpdate(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	services := []string{"buy_fuel", "marketing", "depart"}

	resp, body := friend.do(t, "POST", "/api/nodes", createNodeRequest{
		Name:         "departure",
		GameUsername: "player1",
		GamePassword: "gamepass1",
		Services:     services,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating node status = %d, want 201", resp.StatusCode)
	}
	if got := toStringSlice(body["services"]); !equalStrings(got, services) {
		t.Fatalf("create response services = %v, want %v", got, services)
	}

	nodeID := fmt.Sprintf("%.0f", body["id"].(float64))

	reordered := []string{"marketing", "buy_fuel", "depart"}
	resp, body = friend.do(t, "PATCH", "/api/nodes/"+nodeID, updateNodeRequest{Services: &reordered})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("updating node status = %d, want 200", resp.StatusCode)
	}
	if got := toStringSlice(body["services"]); !equalStrings(got, reordered) {
		t.Fatalf("update response services = %v, want %v", got, reordered)
	}
}

// TestNodeExtraConfigRejectsNonNumericAllianceIDs covers validateExtraConfig
// (node_handlers.go) -- a defense-in-depth check behind the frontend's own
// input filtering (AdvancedSettingsSection.tsx), since alliance_ids ends
// up directly in a game URL (internal/bot/stats.go's allianceStatsByID)
// and the API can be hit directly, bypassing the UI.
func TestNodeExtraConfigRejectsNonNumericAllianceIDs(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	resp, _ := friend.do(t, "POST", "/api/nodes", createNodeRequest{
		Name:         "departure",
		GameUsername: "player1",
		GamePassword: "gamepass1",
		ExtraConfig:  json.RawMessage(`{"alliance_ids":["12345","not-a-number"]}`),
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("creating a node with a non-numeric alliance id status = %d, want 400", resp.StatusCode)
	}

	resp, body := friend.do(t, "POST", "/api/nodes", createNodeRequest{
		Name:         "departure",
		GameUsername: "player1",
		GamePassword: "gamepass1",
		ExtraConfig:  json.RawMessage(`{"alliance_ids":["12345","67890"]}`),
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating a node with valid numeric alliance ids status = %d, want 201", resp.StatusCode)
	}

	nodeID := fmt.Sprintf("%.0f", body["id"].(float64))

	bad := json.RawMessage(`{"alliance_ids":["55.55"]}`)
	resp, _ = friend.do(t, "PATCH", "/api/nodes/"+nodeID, updateNodeRequest{ExtraConfig: &bad})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("updating a node with a non-numeric alliance id status = %d, want 400", resp.StatusCode)
	}
}

func TestNodeResponseNeverIncludesGamePassword(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	_, body := friend.do(t, "POST", "/api/nodes", createNodeRequest{
		Name: "departure", GameUsername: "player1", GamePassword: "supersecret",
	})

	for _, key := range []string{"game_password", "game_password_enc", "GamePasswordEnc"} {
		if _, present := body[key]; present {
			t.Fatalf("node create response leaked field %q: %v", key, body)
		}
	}
}

func TestVPNRegionCatalogIsAdminManagedButUserReadable(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	// a non-admin cannot curate the catalog.
	resp, _ := friend.do(t, "POST", "/api/admin/vpn-regions", createVPNRegionRequest{Name: "us-east", OVPNConfig: "ovpn-contents"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin creating a vpn region status = %d, want 403", resp.StatusCode)
	}

	resp, body := admin.do(t, "POST", "/api/admin/vpn-regions", createVPNRegionRequest{Name: "us-east", OVPNConfig: "ovpn-contents"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("admin creating a vpn region status = %d, want 201", resp.StatusCode)
	}
	if _, present := body["ovpn_config"]; present {
		t.Fatalf("vpn region create response leaked ovpn_config: %v", body)
	}

	regionID := fmt.Sprintf("%.0f", body["id"].(float64))

	// but any signed-in user can list it, to pick their own region from it.
	list := friend.doList(t, "GET", "/api/vpn-regions")
	if len(list) != 1 || list[0]["name"] != "us-east" {
		t.Fatalf("friend listing vpn regions = %v, want a single \"us-east\" entry", list)
	}

	resp, _ = friend.do(t, "DELETE", "/api/admin/vpn-regions/"+regionID, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin deleting a vpn region status = %d, want 403", resp.StatusCode)
	}

	resp, _ = admin.do(t, "DELETE", "/api/admin/vpn-regions/"+regionID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin deleting a vpn region status = %d, want 204", resp.StatusCode)
	}
}

func TestVPNProviderCredentialsAdminOnly(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	resp, body := admin.do(t, "GET", "/api/admin/vpn-provider", nil)
	if resp.StatusCode != http.StatusOK || body["configured"] != false {
		t.Fatalf("vpn provider status before configuring = %d %v, want 200 configured:false", resp.StatusCode, body)
	}

	resp, _ = friend.do(t, "PUT", "/api/admin/vpn-provider", setVPNProviderRequest{VPNUsername: "u", VPNPassword: "p"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin setting vpn provider credentials status = %d, want 403", resp.StatusCode)
	}

	resp, body = admin.do(t, "PUT", "/api/admin/vpn-provider", setVPNProviderRequest{VPNUsername: "u", VPNPassword: "p"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin setting vpn provider credentials status = %d, want 200", resp.StatusCode)
	}
	for _, key := range []string{"vpn_username", "vpn_password", "vpn_username_enc", "vpn_password_enc"} {
		if _, present := body[key]; present {
			t.Fatalf("vpn provider response leaked field %q: %v", key, body)
		}
	}

	resp, body = admin.do(t, "GET", "/api/admin/vpn-provider", nil)
	if resp.StatusCode != http.StatusOK || body["configured"] != true {
		t.Fatalf("vpn provider status after configuring = %d %v, want 200 configured:true", resp.StatusCode, body)
	}
}

func TestUserSelectsOwnVPNRegionForAllNodes(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	_, region := admin.do(t, "POST", "/api/admin/vpn-regions", createVPNRegionRequest{Name: "us-east", OVPNConfig: "ovpn-contents"})
	regionID := int64(region["id"].(float64))

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	// rejecting a nonexistent region id is a 400, not a 500 (FK violation).
	bogusID := regionID + 999
	resp, _ := friend.do(t, "PUT", "/api/me/vpn-region", setUserVPNRegionRequest{VPNRegionID: &bogusID})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("setting a nonexistent vpn region status = %d, want 400", resp.StatusCode)
	}

	resp, _ = friend.do(t, "PUT", "/api/me/vpn-region", setUserVPNRegionRequest{VPNRegionID: &regionID})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setting own vpn region status = %d, want 204", resp.StatusCode)
	}

	_, me := friend.do(t, "GET", "/api/me", nil)
	if me["vpn_region_id"] == nil || int64(me["vpn_region_id"].(float64)) != regionID {
		t.Fatalf("/api/me vpn_region_id = %v, want %d", me["vpn_region_id"], regionID)
	}

	// clearing it back to no VPN.
	resp, _ = friend.do(t, "PUT", "/api/me/vpn-region", setUserVPNRegionRequest{VPNRegionID: nil})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clearing own vpn region status = %d, want 204", resp.StatusCode)
	}

	_, me = friend.do(t, "GET", "/api/me", nil)
	if me["vpn_region_id"] != nil {
		t.Fatalf("/api/me vpn_region_id after clearing = %v, want nil", me["vpn_region_id"])
	}
}

func TestPrometheusSettingsAdminOnlyAndMetricsEndpoint(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend@example.com", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend@example.com", Password: "friendpass123"})

	// unconfigured: /api/metrics reports so, not an error.
	resp, body := friend.do(t, "GET", "/api/metrics", nil)
	if resp.StatusCode != http.StatusOK || body["configured"] != false {
		t.Fatalf("metrics before configuring prometheus = %d %v, want 200 configured:false", resp.StatusCode, body)
	}

	// a non-admin cannot configure it.
	resp, _ = friend.do(t, "PUT", "/api/admin/prometheus", setPrometheusSettingsRequest{URL: "http://prometheus:9090"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin setting prometheus url status = %d, want 403", resp.StatusCode)
	}

	// a malformed URL is rejected with 400, not stored.
	resp, _ = admin.do(t, "PUT", "/api/admin/prometheus", setPrometheusSettingsRequest{URL: "not a url"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("setting a malformed prometheus url status = %d, want 400", resp.StatusCode)
	}

	resp, body = admin.do(t, "PUT", "/api/admin/prometheus", setPrometheusSettingsRequest{URL: "http://127.0.0.1:1"})
	if resp.StatusCode != http.StatusOK || body["configured"] != true {
		t.Fatalf("admin setting prometheus url status = %d %v, want 200 configured:true", resp.StatusCode, body)
	}

	resp, body = admin.do(t, "GET", "/api/admin/prometheus", nil)
	if resp.StatusCode != http.StatusOK || body["url"] != "http://127.0.0.1:1" {
		t.Fatalf("getting prometheus settings = %d %v, want 200 with the url just set", resp.StatusCode, body)
	}

	// configured but unreachable: /api/metrics fails safely (500, no leaked
	// internals), rather than hanging or crashing the process.
	resp, _ = friend.do(t, "GET", "/api/metrics", nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("metrics with an unreachable prometheus status = %d, want 500", resp.StatusCode)
	}
}

func TestNodeCannotBeEnabledUntilConfigured(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})

	nodes := friend.doList(t, "GET", "/api/nodes")
	nodeID := fmt.Sprintf("%.0f", nodes[0]["id"].(float64))

	// the auto-created default has no game credentials yet -- enabling it
	// must be refused, not silently accepted and left to crash-loop.
	enabledTrue := true
	resp, body := friend.do(t, "PATCH", "/api/nodes/"+nodeID, updateNodeRequest{Enabled: &enabledTrue})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("enabling an unconfigured node status = %d %v, want 400", resp.StatusCode, body)
	}

	// filling in game credentials (schedule and timezone already have
	// defaults from CreateNode) lets it be enabled.
	username := "player1"
	password := "gamepass123"
	resp, body = friend.do(t, "PATCH", "/api/nodes/"+nodeID, updateNodeRequest{
		GameUsername: &username, GamePassword: &password, Enabled: &enabledTrue,
	})
	if resp.StatusCode != http.StatusOK || body["enabled"] != true {
		t.Fatalf("enabling a configured node status = %d %v, want 200 enabled:true", resp.StatusCode, body)
	}
}

func TestAdminCannotDisableOwnAccount(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	_, meBody := admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	adminUUID := meBody["uuid"].(string)

	resp, _ := admin.do(t, "POST", "/api/admin/users/"+adminUUID+"/disable", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin disabling their own account status = %d, want 403", resp.StatusCode)
	}
}

func TestAdminHasNoNodesOfItsOwnButSeesEveryUsersNodes(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin@example.com", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin@example.com", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})

	// requireNonAdminUser rejects the admin's own session on every
	// node/VPN-self-service/metrics route.
	for _, req := range []struct{ method, path string }{
		{"GET", "/api/nodes"},
		{"POST", "/api/nodes"},
		{"GET", "/api/metrics"},
		{"PUT", "/api/me/vpn-region"},
	} {
		resp, _ := admin.do(t, req.method, req.path, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("admin %s %s status = %d, want 403", req.method, req.path, resp.StatusCode)
		}
	}

	// but the read-only admin view sees every user's nodes.
	all := admin.doList(t, "GET", "/api/admin/nodes")
	if len(all) != 2 {
		t.Fatalf("GET /api/admin/nodes = %d nodes, want 2 (friend1's two auto-created defaults)", len(all))
	}
	if all[0]["owner_login"] != "friend1" {
		t.Fatalf("admin node view owner_login = %v, want friend1", all[0]["owner_login"])
	}
}

func TestUserDetailAndListIncludeNodeStatsAndLastLogin(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})

	// list: node stats present, last_login_at nil before friend1 ever logs in.
	list := admin.doList(t, "GET", "/api/admin/users")
	var friendEntry map[string]any
	for _, u := range list {
		if u["login"] == "friend1" {
			friendEntry = u
		}
	}
	if friendEntry == nil {
		t.Fatal("friend1 missing from GET /api/admin/users")
	}
	if friendEntry["total_nodes"] != float64(2) || friendEntry["active_nodes"] != float64(0) {
		t.Fatalf("friend1 list entry node stats = %+v, want total_nodes:2 active_nodes:0", friendEntry)
	}
	if friendEntry["last_login_at"] != nil {
		t.Fatalf("friend1 last_login_at = %v, want nil before first login", friendEntry["last_login_at"])
	}

	friendUUID := friendEntry["uuid"].(string)

	// detail: nodes list present, no vpn region yet.
	resp, detail := admin.do(t, "GET", "/api/admin/users/"+friendUUID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET user detail status = %d, want 200", resp.StatusCode)
	}
	if detail["vpn_region_name"] != nil {
		t.Fatalf("detail vpn_region_name = %v, want nil", detail["vpn_region_name"])
	}
	nodes, ok := detail["nodes"].([]any)
	if !ok || len(nodes) != 2 {
		t.Fatalf("detail nodes = %v, want 2 entries", detail["nodes"])
	}

	// friend1 logs in -- last_login_at now set.
	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})

	_, detail = admin.do(t, "GET", "/api/admin/users/"+friendUUID, nil)
	if detail["last_login_at"] == nil {
		t.Fatal("detail last_login_at still nil after friend1 logged in")
	}
}

func TestAdminResetsUserPassword(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	_, created := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := created["uuid"].(string)

	_, adminSelf := admin.do(t, "GET", "/api/me", nil)
	adminUUID := adminSelf["uuid"].(string)
	resp, _ := admin.do(t, "POST", "/api/admin/users/"+adminUUID+"/reset-password", resetUserPasswordRequest{Password: "whatever123"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin resetting their OWN password via this endpoint status = %d, want 403", resp.StatusCode)
	}

	resp, _ = admin.do(t, "POST", "/api/admin/users/"+friendUUID+"/reset-password", resetUserPasswordRequest{Password: "newpass456"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin reset-password status = %d, want 204", resp.StatusCode)
	}

	friend := &client{base: admin.base, jar: map[string]string{}}
	resp, _ = friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login with the OLD password after reset status = %d, want 401", resp.StatusCode)
	}

	resp, _ = friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "newpass456"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login with the NEW password after reset status = %d, want 200", resp.StatusCode)
	}
}

func TestDisablingUserStopsTheirEnabledNodes(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	_, created := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := created["uuid"].(string)

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})

	nodes := friend.doList(t, "GET", "/api/nodes")
	nodeID := fmt.Sprintf("%.0f", nodes[0]["id"].(float64))
	username, password, enabledTrue := "player1", "gamepass123", true
	resp, body := friend.do(t, "PATCH", "/api/nodes/"+nodeID, updateNodeRequest{
		GameUsername: &username, GamePassword: &password, Enabled: &enabledTrue,
	})
	if resp.StatusCode != http.StatusOK || body["enabled"] != true {
		t.Fatalf("enabling friend1's node status = %d %v, want 200 enabled:true", resp.StatusCode, body)
	}

	resp, _ = admin.do(t, "POST", "/api/admin/users/"+friendUUID+"/disable", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin disabling friend1 status = %d, want 204", resp.StatusCode)
	}

	_, body = friend.do(t, "GET", "/api/nodes/"+nodeID, nil)
	if body["enabled"] != false {
		t.Fatalf("node enabled after owner disabled = %v, want false (disabling a user must stop their nodes)", body["enabled"])
	}
}

// TestAuditLogRecordsAdminAction verifies the structured audit-log fields
// (see audit.go) actually reach the log output with the right shape for a
// representative mutating admin action -- proof that s.audit's fields
// aren't just theoretical, for a design meant to be greppable in an
// external log aggregator (see audit.go's own doc comment).
func TestAuditLogRecordsAdminAction(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	_, created := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := created["uuid"].(string)

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, nil)))
	defer slog.SetDefault(prevLogger)

	resp, _ := admin.do(t, "POST", "/api/admin/users/"+friendUUID+"/disable", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin disabling friend1 status = %d, want 204", resp.StatusCode)
	}

	var line map[string]any
	found := false
	for _, l := range strings.Split(strings.TrimSpace(logBuf.String()), "\n") {
		if l == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(l), &entry); err != nil {
			t.Fatalf("unmarshaling log line %q: %v", l, err)
		}
		if entry["msg"] == "audit" && entry["action"] == "disable_user" {
			line = entry
			found = true

			break
		}
	}
	if !found {
		t.Fatalf("no audit log line for disable_user found in output:\n%s", logBuf.String())
	}

	if line["actor_role"] != "admin" {
		t.Fatalf("audit actor_role = %v, want %q", line["actor_role"], "admin")
	}
	if line["actor_login"] != "admin" {
		t.Fatalf("audit actor_login = %v, want %q", line["actor_login"], "admin")
	}
	if line["target"] != friendUUID {
		t.Fatalf("audit target = %v, want %q", line["target"], friendUUID)
	}
}

// TestDisabledAccountRejectedOnNextWriteNotRead covers requireAuth's
// disabled-account check (internal/api/middleware.go): a session issued
// before the account was disabled remains USABLE FOR READS (no DB round
// trip on every GET, see that middleware's own doc comment), but the very
// next WRITE attempt is refused and force-logs-out the session -- proven
// here by a GET succeeding right after disable, then a PATCH failing, then
// a GET *after that* also failing (the cookie itself was cleared, not just
// that one response).
func TestDisabledAccountRejectedOnNextWriteNotRead(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	_, created := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := created["uuid"].(string)

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})

	resp, _ := admin.do(t, "POST", "/api/admin/users/"+friendUUID+"/disable", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin disabling friend1 status = %d, want 204", resp.StatusCode)
	}

	// The read still works -- the disabled check only runs on mutating
	// methods, see requireAuth's doc comment.
	resp, _ = friend.do(t, "GET", "/api/nodes", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET with a pre-disable session status = %d, want 200 (reads aren't re-checked)", resp.StatusCode)
	}

	// The next WRITE is refused and force-clears the session cookie.
	name := "won't stick"
	resp, _ = friend.do(t, "PUT", "/api/me/display-name", struct {
		DisplayName *string `json:"display_name"`
	}{DisplayName: &name})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("PUT with a disabled account's session status = %d, want 401", resp.StatusCode)
	}

	// The cookie was cleared client-side by that 401 -- a subsequent
	// request (even a GET) now fails too, proving it wasn't just that one
	// response that was refused.
	resp, _ = friend.do(t, "GET", "/api/nodes", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET after the force-logout status = %d, want 401 (session cookie should be gone)", resp.StatusCode)
	}
}

// TestDeletedAccountRejectedOnNextWrite is
// TestDisabledAccountRejectedOnNextWriteNotRead's sibling for the other
// branch of requireAuth's switch (internal/api/middleware.go) -- a
// session issued before the account was DELETED outright.
//
// Unlike disabling, deletion has no "reads still work for a while"
// window: disabling only sets disabled_at on a row that still exists, so
// handlers that don't specifically check it (most of them) keep working
// off the JWT's claims alone; deletion removes that row entirely, so ANY
// handler that resolves the caller's own user record -- which includes
// ordinary reads like handleListNodes's currentUserID lookup -- fails on
// its own, independent of requireAuth. What requireAuth's write-time
// check actually buys here is consistency: without it, a write handler
// hitting the same now-missing-row problem (e.g. handleSetMyDisplayName's
// own currentUserID call) would 500 ("failed to set display name") --
// requireAuth intercepts first and turns that into a clean, uniform 401 +
// force-logout instead, the same outcome a disabled account gets.
func TestDeletedAccountRejectedOnNextWrite(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	_, created := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := created["uuid"].(string)

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})

	// Sanity: the session is good before deletion.
	resp, _ := friend.do(t, "GET", "/api/nodes", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET with a pre-delete session status = %d, want 200", resp.StatusCode)
	}

	resp, _ = admin.do(t, "DELETE", "/api/admin/users/"+friendUUID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin deleting friend1 status = %d, want 204", resp.StatusCode)
	}

	// The very next WRITE gets a clean 401 (not the 500 the handler's own
	// currentUserID lookup would otherwise produce) and force-clears the
	// cookie -- requireAuth catches it before the handler even runs.
	name := "won't stick"
	resp, _ = friend.do(t, "PUT", "/api/me/display-name", struct {
		DisplayName *string `json:"display_name"`
	}{DisplayName: &name})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("PUT with a deleted account's session status = %d, want 401", resp.StatusCode)
	}

	resp, _ = friend.do(t, "GET", "/api/nodes", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET after the force-logout status = %d, want 401 (session cookie should be gone)", resp.StatusCode)
	}
}

func TestAdminDeletesUserAndTheirNodes(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	adminUUID := ""
	{
		_, meBody := admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
		adminUUID = meBody["uuid"].(string)
	}
	_, created := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := created["uuid"].(string)

	// an admin cannot delete their own account.
	resp, _ := admin.do(t, "DELETE", "/api/admin/users/"+adminUUID, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin deleting themselves status = %d, want 403", resp.StatusCode)
	}

	resp, _ = admin.do(t, "DELETE", "/api/admin/users/"+friendUUID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin deleting friend1 status = %d, want 204", resp.StatusCode)
	}

	all := admin.doList(t, "GET", "/api/admin/nodes")
	for _, n := range all {
		if n["owner_login"] == "friend1" {
			t.Fatalf("friend1's node %v still present after their account was deleted", n)
		}
	}

	resp, _ = admin.do(t, "GET", "/api/admin/users/"+friendUUID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted user status = %d, want 404", resp.StatusCode)
	}
}

func TestSelfServicePasswordAndDisplayName(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})

	friend := &client{base: admin.base, jar: map[string]string{}}
	friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})

	resp, _ := friend.do(t, "PUT", "/api/me/password", setPasswordRequest{NewPassword: "newpass456"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("changing own password status = %d, want 204", resp.StatusCode)
	}

	fresh := &client{base: admin.base, jar: map[string]string{}}
	resp, _ = fresh.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "newpass456"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login with the new self-set password status = %d, want 200", resp.StatusCode)
	}

	name := "Friendly One"
	resp, _ = friend.do(t, "PUT", "/api/me/display-name", setDisplayNameRequest{DisplayName: &name})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setting display name status = %d, want 204", resp.StatusCode)
	}

	_, me := friend.do(t, "GET", "/api/me", nil)
	if me["display_name"] != name {
		t.Fatalf("/api/me display_name = %v, want %q", me["display_name"], name)
	}
}

func TestAdminMetricsRequiresUserUUIDAndValidatesIt(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	_, created := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := created["uuid"].(string)

	resp, _ := admin.do(t, "GET", "/api/admin/metrics", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("admin metrics without user_uuid status = %d, want 400", resp.StatusCode)
	}

	resp, _ = admin.do(t, "GET", "/api/admin/metrics?user_uuid=00000000-0000-0000-0000-000000000000", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("admin metrics for a nonexistent user_uuid status = %d, want 404", resp.StatusCode)
	}

	// unconfigured Prometheus -- reports so, not an error.
	resp, body := admin.do(t, "GET", "/api/admin/metrics?user_uuid="+friendUUID, nil)
	if resp.StatusCode != http.StatusOK || body["configured"] != false {
		t.Fatalf("admin metrics before configuring prometheus = %d %v, want 200 configured:false", resp.StatusCode, body)
	}
}

func TestAdminSetsNodeLogLevel(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	_, userBody := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := userBody["uuid"].(string)

	_, detail := admin.do(t, "GET", "/api/admin/users/"+friendUUID, nil)
	nodes := detail["nodes"].([]any)
	nodeID := fmt.Sprintf("%.0f", nodes[0].(map[string]any)["id"].(float64))

	resp, _ := admin.do(t, "PUT", "/api/admin/nodes/"+nodeID+"/log-level", setNodeLogLevelRequest{LogLevel: "not-a-level"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("setting an invalid log level status = %d, want 400", resp.StatusCode)
	}

	resp, _ = admin.do(t, "PUT", "/api/admin/nodes/"+nodeID+"/log-level", setNodeLogLevelRequest{LogLevel: "debug"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setting node log level status = %d, want 204", resp.StatusCode)
	}

	_, node := admin.do(t, "GET", "/api/admin/nodes/"+nodeID, nil)
	extraConfig := node["extra_config"].(map[string]any)
	if extraConfig["log_level"] != "debug" {
		t.Fatalf("extra_config.log_level = %v, want debug", extraConfig["log_level"])
	}

	// clearing it back out removes the key rather than storing an empty string.
	resp, _ = admin.do(t, "PUT", "/api/admin/nodes/"+nodeID+"/log-level", setNodeLogLevelRequest{LogLevel: ""})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clearing node log level status = %d, want 204", resp.StatusCode)
	}

	_, node = admin.do(t, "GET", "/api/admin/nodes/"+nodeID, nil)
	extraConfig = node["extra_config"].(map[string]any)
	if _, present := extraConfig["log_level"]; present {
		t.Fatalf("extra_config still has log_level after clearing it: %v", extraConfig)
	}

	resp, _ = admin.do(t, "PUT", "/api/admin/nodes/999999/log-level", setNodeLogLevelRequest{LogLevel: "warn"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("setting log level on a nonexistent node status = %d, want 404", resp.StatusCode)
	}
}

// TestLoginBruteForceBanAndUnlock exercises LoginGuard end to end: enough
// failed attempts against one login trips a ban (429, not 401), the admin
// can see it on the user detail page and lift it early, and a correct
// password works again immediately after -- see internal/api/login_guard.go.
func TestLoginBruteForceBanAndUnlock(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	_, created := admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})
	friendUUID := created["uuid"].(string)

	friend := &client{base: admin.base, jar: map[string]string{}}

	// maxFailedLoginAttempts (5) wrong-password attempts in a row.
	for i := 0; i < 5; i++ {
		resp, _ := friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "wrong-password"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, resp.StatusCode)
		}
	}

	// The 6th attempt, even with the CORRECT password, must be refused by
	// the ban itself -- proves Check() runs before credentials are ever
	// verified.
	resp, _ := friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("login while banned status = %d, want 429", resp.StatusCode)
	}

	_, detail := admin.do(t, "GET", "/api/admin/users/"+friendUUID, nil)
	ban, ok := detail["ban"].(map[string]any)
	if !ok {
		t.Fatalf("user detail ban = %v, want a non-nil ban object", detail["ban"])
	}
	if ban["reason"] == "" {
		t.Fatal("ban reason is empty")
	}

	activity, ok := detail["login_activity"].([]any)
	if !ok || len(activity) != 5 {
		t.Fatalf("login_activity = %v, want 5 recorded failed attempts", detail["login_activity"])
	}

	resp, _ = admin.do(t, "POST", "/api/admin/users/"+friendUUID+"/unlock", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin unlock status = %d, want 204", resp.StatusCode)
	}

	_, detail = admin.do(t, "GET", "/api/admin/users/"+friendUUID, nil)
	if detail["ban"] != nil {
		t.Fatalf("ban still present after unlock: %v", detail["ban"])
	}

	resp, _ = friend.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "friendpass123"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login with correct password right after unlock status = %d, want 200", resp.StatusCode)
	}
}

// TestLoginGuardSeparatesPasswordGuessingFromLoginGuessing guards against
// a real regression: an earlier LoginGuard version bumped a raw IP
// failure counter on every failed attempt regardless of which login was
// targeted, so repeatedly failing the SAME login from one IP (ordinary
// password-guessing, or just a person mistyping their own password)
// banned that IP too -- locking the tester out of the admin UI with no
// other admin account reachable to undo it. See LoginGuard's doc comment.
func TestLoginGuardSeparatesPasswordGuessingFromLoginGuessing(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "friend1", Password: "friendpass123"})

	// Case 1: maxFailedLoginAttempts wrong passwords against the SAME
	// login, from one IP -- must ban the login, must NOT ban the IP.
	c1 := &client{base: admin.base, jar: map[string]string{}}
	for range maxFailedLoginAttempts {
		c1.do(t, "POST", "/api/auth/login", loginRequest{Login: "friend1", Password: "wrong"})
	}

	if _, err := st.GetActiveLoginBan(context.Background(), banKeyTypeLogin, "friend1"); err != nil {
		t.Fatalf("login ban not created after %d same-login failures: %v", maxFailedLoginAttempts, err)
	}

	if _, err := st.GetActiveLoginBan(context.Background(), banKeyTypeIP, "127.0.0.1"); err == nil {
		t.Fatal("IP got banned from repeated failures against ONE login -- password guessing must not ban the IP")
	}

	if err := st.DeleteLoginBan(context.Background(), banKeyTypeLogin, "friend1"); err != nil {
		t.Fatalf("cleaning up login ban before case 2: %v", err)
	}

	// Case 2: maxFailedLoginAttempts wrong passwords against DIFFERENT
	// logins, from one IP -- must ban the IP, must NOT ban any single
	// login (each of them only failed once).
	c2 := &client{base: admin.base, jar: map[string]string{}}
	for i := range maxFailedLoginAttempts {
		c2.do(t, "POST", "/api/auth/login", loginRequest{Login: fmt.Sprintf("nosuchuser%d", i), Password: "whatever"})
	}

	if _, err := st.GetActiveLoginBan(context.Background(), banKeyTypeIP, "127.0.0.1"); err != nil {
		t.Fatalf("IP ban not created after %d distinct-login failures: %v", maxFailedLoginAttempts, err)
	}

	if _, err := st.GetActiveLoginBan(context.Background(), banKeyTypeLogin, "nosuchuser0"); err == nil {
		t.Fatal("a login got banned from a single failed attempt -- login guessing must not ban individual logins")
	}
}

// TestLoginGuardBansIPForSustainedHammeringOfFewLogins covers the third
// LoginGuard mechanism (maxIPTotalFailedAttempts): alternating between
// just two known logins ("admin", "abc" -- never a 3rd) so the
// distinct-logins-per-IP counter never reaches maxFailedLoginAttempts on
// its own. Each login still bans itself individually at its own 5th
// failure; the raw total across both crosses maxIPTotalFailedAttempts
// (10) right as the second login's own ban fires, and the IP must ALSO
// end up banned at that same moment -- otherwise an attacker who knows
// just 2-3 real logins could keep cycling between them, each self-throttled
// but the source IP itself never penalized.
func TestLoginGuardBansIPForSustainedHammeringOfFewLogins(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	admin.do(t, "POST", "/api/admin/users", createUserRequest{Login: "abc", Password: "abcpass1234"})

	c := &client{base: admin.base, jar: map[string]string{}}

	logins := []string{"admin", "abc"}
	for i := range maxIPTotalFailedAttempts {
		c.do(t, "POST", "/api/auth/login", loginRequest{Login: logins[i%2], Password: "wrong"})
	}

	if _, err := st.GetActiveLoginBan(context.Background(), banKeyTypeLogin, "admin"); err != nil {
		t.Fatalf("login ban for admin not created: %v", err)
	}

	if _, err := st.GetActiveLoginBan(context.Background(), banKeyTypeLogin, "abc"); err != nil {
		t.Fatalf("login ban for abc not created: %v", err)
	}

	ipBan, err := st.GetActiveLoginBan(context.Background(), banKeyTypeIP, "127.0.0.1")
	if err != nil {
		t.Fatalf("IP ban not created after %d total failures against only 2 logins: %v", maxIPTotalFailedAttempts, err)
	}

	if ipBan.Reason != fmt.Sprintf("%d total failed login attempts from this IP", maxIPTotalFailedAttempts) {
		t.Fatalf("IP ban reason = %q, want the total-failures reason (not the distinct-logins one)", ipBan.Reason)
	}
}

// TestAdminCreatesAnotherAdminAndDisablesFirst covers the "promote to
// admin" checkbox on the create-user form (createUserRequest.IsAdmin):
// the new admin gets no default nodes (unlike a regular user), and one
// admin CAN disable another admin -- there is no is_admin check in
// handleSetUserDisabled, only the caller's own uuid is refused. This is
// exactly how an operator replaces the bootstrap "admin"/"admin" account
// with one under their own chosen login.
func TestAdminCreatesAnotherAdminAndDisablesFirst(t *testing.T) {
	_, admin := testServer(t)
	st, _ := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	defer st.Close()
	mustCreateAdmin(t, st, "admin", "adminpass123")
	admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})

	resp, created := admin.do(t, "POST", "/api/admin/users", map[string]any{
		"login":    "second_admin",
		"password": "secondpass123",
		"is_admin": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating second admin status = %d, want 201", resp.StatusCode)
	}
	if isAdmin, _ := created["is_admin"].(bool); !isAdmin {
		t.Fatalf("created user is_admin = %v, want true", created["is_admin"])
	}
	secondAdminUUID := created["uuid"].(string)

	_, detail := admin.do(t, "GET", "/api/admin/users/"+secondAdminUUID, nil)
	if nodes, _ := detail["nodes"].([]any); len(nodes) != 0 {
		t.Fatalf("second admin's nodes = %v, want none (admins have no nodes of their own)", detail["nodes"])
	}

	secondAdmin := &client{base: admin.base, jar: map[string]string{}}
	resp, _ = secondAdmin.do(t, "POST", "/api/auth/login", loginRequest{Login: "second_admin", Password: "secondpass123"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second admin login status = %d, want 200", resp.StatusCode)
	}

	_, firstAdminSelf := admin.do(t, "GET", "/api/me", nil)
	firstAdminUUID := firstAdminSelf["uuid"].(string)

	resp, _ = secondAdmin.do(t, "POST", "/api/admin/users/"+firstAdminUUID+"/disable", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("second admin disabling first admin status = %d, want 204", resp.StatusCode)
	}

	resp, _ = admin.do(t, "POST", "/api/auth/login", loginRequest{Login: "admin", Password: "adminpass123"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled first admin login status = %d, want 401", resp.StatusCode)
	}
}

func toStringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(raw))
	for i, r := range raw {
		out[i], _ = r.(string)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
