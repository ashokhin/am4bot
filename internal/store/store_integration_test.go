//go:build integration

// Integration tests against a real Postgres instance. Run with:
//
//	go test -tags=integration ./internal/store/... -v
//
// requiring TEST_DATABASE_URL to point at a scratch database -- these
// tests create/drop tables freely and must never run against anything
// that matters. Skipped by default (no build tag => not compiled) so
// `go test ./...` and CI never need a live Postgres.
package store

import (
	"context"
	"errors"
	"os"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	s, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	t.Cleanup(func() { s.Close() })

	// start each test from a clean slate
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS node_operations, nodes, vpn_configs, vpn_regions, vpn_provider_credentials, prometheus_settings, login_ban_events, login_attempts, login_bans, users, schema_migrations CASCADE`); err != nil {
		t.Fatalf("resetting schema: %v", err)
	}

	s2, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("re-opening Store after reset: %v", err)
	}

	t.Cleanup(func() { s2.Close() })

	return s2
}

func TestUserCreateAndLookup(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "user1", "hashed-password", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	if u.UUID.String() == "" || u.ID == 0 {
		t.Fatalf("CreateUser() returned zero-value id/uuid: %+v", u)
	}

	byLogin, err := s.GetUserByLogin(ctx, "user1")
	if err != nil {
		t.Fatalf("GetUserByLogin() error = %v", err)
	}
	if byLogin.UUID != u.UUID {
		t.Fatalf("GetUserByLogin() UUID = %v, want %v", byLogin.UUID, u.UUID)
	}

	byUUID, err := s.GetUserByUUID(ctx, u.UUID)
	if err != nil {
		t.Fatalf("GetUserByUUID() error = %v", err)
	}
	if byUUID.Login != u.Login {
		t.Fatalf("GetUserByUUID() Login = %q, want %q", byUUID.Login, u.Login)
	}

	if _, err := s.CreateUser(ctx, "user1", "x", false); err == nil {
		t.Fatal("CreateUser() with a duplicate login succeeded, want ErrConflict")
	}
}

func TestNodeServicesAndSchedulesPreserveOrder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "user@example.com", "hashed-password", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	// deliberately NOT alphabetical -- this is the whole point of the test
	services := []string{"buy_fuel", "marketing", "depart", "ac_maintenance"}
	schedules := []string{"0 10 * * 0,6", "0 8 * * 1-5"}

	n, err := s.CreateNode(ctx, NewNodeParams{
		UserID:        u.ID,
		Name:          "departure",
		GameURL:       "https://www.airlinemanager.com/",
		GameUsername:  "player1",
		Services:      services,
		CronSchedules: schedules,
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	if got := []string(n.Services); !equal(got, services) {
		t.Fatalf("CreateNode() Services = %v, want %v (order not preserved)", got, services)
	}
	if got := []string(n.CronSchedules); !equal(got, schedules) {
		t.Fatalf("CreateNode() CronSchedules = %v, want %v (order not preserved)", got, schedules)
	}

	// re-read from a fresh query to rule out a round-trip-only artifact
	got, err := s.GetNode(ctx, u.ID, n.ID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}

	if gotServices := []string(got.Services); !equal(gotServices, services) {
		t.Fatalf("GetNode() Services = %v, want %v (order not preserved across a fresh read)", gotServices, services)
	}
}

func TestDeleteDefaultNodeRefused(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "user1", "hashed-password", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	n, err := s.CreateNode(ctx, NewNodeParams{
		UserID:       u.ID,
		Name:         "maintenance",
		GameURL:      "https://www.airlinemanager.com/",
		GameUsername: "player1",
		IsDefault:    true,
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	if err := s.DeleteNode(ctx, u.ID, n.ID); !errors.Is(err, ErrDefaultNodeNotDeletable) {
		t.Fatalf("DeleteNode() on default node error = %v, want ErrDefaultNodeNotDeletable", err)
	}

	// a user now gets TWO default nodes ("player" + "maintenance", see
	// admin_handlers.go's handleCreateUser) -- more than one is_default
	// row per user is expected, not an error.
	n2, err := s.CreateNode(ctx, NewNodeParams{
		UserID:       u.ID,
		Name:         "player",
		GameURL:      "https://www.airlinemanager.com/",
		GameUsername: "player1",
		IsDefault:    true,
	})
	if err != nil {
		t.Fatalf("CreateNode() with a second default node error = %v, want success", err)
	}

	if err := s.DeleteNode(ctx, u.ID, n2.ID); !errors.Is(err, ErrDefaultNodeNotDeletable) {
		t.Fatalf("DeleteNode() on second default node error = %v, want ErrDefaultNodeNotDeletable", err)
	}
}

func TestVPNRegionCatalogAndUserSelection(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "user@example.com", "hashed-password", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if u.VPNRegionID != nil {
		t.Fatalf("new user VPNRegionID = %v, want nil (no VPN by default)", u.VPNRegionID)
	}

	region, err := s.CreateVPNRegion(ctx, "us-east", "enc-ovpn-us-east")
	if err != nil {
		t.Fatalf("CreateVPNRegion() error = %v", err)
	}

	if _, err := s.CreateVPNRegion(ctx, "us-east", "enc-ovpn-dup"); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateVPNRegion() with a duplicate name error = %v, want ErrConflict", err)
	}

	regions, err := s.ListVPNRegions(ctx)
	if err != nil {
		t.Fatalf("ListVPNRegions() error = %v", err)
	}
	if len(regions) != 1 || regions[0].ID != region.ID {
		t.Fatalf("ListVPNRegions() = %+v, want a single entry with id %d", regions, region.ID)
	}

	if err := s.SetUserVPNRegion(ctx, u.ID, &region.ID); err != nil {
		t.Fatalf("SetUserVPNRegion() error = %v", err)
	}

	got, err := s.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if got.VPNRegionID == nil || *got.VPNRegionID != region.ID {
		t.Fatalf("GetUserByID() VPNRegionID = %v, want %d", got.VPNRegionID, region.ID)
	}

	// deleting the region a user has selected must not fail -- it falls
	// back to no VPN (ON DELETE SET NULL), not ErrConflict.
	if err := s.DeleteVPNRegion(ctx, region.ID); err != nil {
		t.Fatalf("DeleteVPNRegion() while selected by a user error = %v, want nil (SET NULL)", err)
	}

	got, err = s.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID() after region deletion error = %v", err)
	}
	if got.VPNRegionID != nil {
		t.Fatalf("GetUserByID() VPNRegionID after its region was deleted = %v, want nil", got.VPNRegionID)
	}
}

func TestVPNProviderCredentials(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.GetVPNProviderCredentials(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetVPNProviderCredentials() before any are set error = %v, want ErrNotFound", err)
	}

	if _, err := s.SetVPNProviderCredentials(ctx, "custom", "enc-user", "enc-pass"); err != nil {
		t.Fatalf("SetVPNProviderCredentials() error = %v", err)
	}

	got, err := s.GetVPNProviderCredentials(ctx)
	if err != nil {
		t.Fatalf("GetVPNProviderCredentials() error = %v", err)
	}
	if got.VPNUsernameEnc != "enc-user" || got.VPNPasswordEnc != "enc-pass" {
		t.Fatalf("GetVPNProviderCredentials() = %+v, want enc-user/enc-pass", got)
	}

	// setting again must replace, not duplicate, the singleton row.
	if _, err := s.SetVPNProviderCredentials(ctx, "custom", "enc-user2", "enc-pass2"); err != nil {
		t.Fatalf("SetVPNProviderCredentials() (second call) error = %v", err)
	}

	got, err = s.GetVPNProviderCredentials(ctx)
	if err != nil {
		t.Fatalf("GetVPNProviderCredentials() (after replace) error = %v", err)
	}
	if got.VPNUsernameEnc != "enc-user2" {
		t.Fatalf("GetVPNProviderCredentials() VPNUsernameEnc = %q, want enc-user2 (replaced, not duplicated)", got.VPNUsernameEnc)
	}
}

func TestPrometheusSettings(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.GetPrometheusSettings(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPrometheusSettings() before any is set error = %v, want ErrNotFound", err)
	}

	if _, err := s.SetPrometheusSettings(ctx, "http://prometheus:9090"); err != nil {
		t.Fatalf("SetPrometheusSettings() error = %v", err)
	}

	got, err := s.GetPrometheusSettings(ctx)
	if err != nil {
		t.Fatalf("GetPrometheusSettings() error = %v", err)
	}
	if got.URL != "http://prometheus:9090" {
		t.Fatalf("GetPrometheusSettings() URL = %q, want http://prometheus:9090", got.URL)
	}

	// setting again must replace, not duplicate, the singleton row.
	if _, err := s.SetPrometheusSettings(ctx, "http://prometheus2:9090"); err != nil {
		t.Fatalf("SetPrometheusSettings() (second call) error = %v", err)
	}

	got, err = s.GetPrometheusSettings(ctx)
	if err != nil {
		t.Fatalf("GetPrometheusSettings() (after replace) error = %v", err)
	}
	if got.URL != "http://prometheus2:9090" {
		t.Fatalf("GetPrometheusSettings() URL = %q, want http://prometheus2:9090 (replaced, not duplicated)", got.URL)
	}
}

func TestListProvisionedNodesForScraping(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "user@example.com", "hashed-password", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	n, err := s.CreateNode(ctx, NewNodeParams{
		UserID: u.ID, Name: "departure", GameURL: "https://www.airlinemanager.com/", GameUsername: "player1",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	// not provisioned yet -- must not appear in the scrape list.
	targets, err := s.ListProvisionedNodesForScraping(ctx)
	if err != nil {
		t.Fatalf("ListProvisionedNodesForScraping() error = %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("ListProvisionedNodesForScraping() before provisioning = %+v, want empty", targets)
	}

	if _, _, err := s.EnsureNodeProvisioned(ctx, n.ID, "test-host", 9200, 9299); err != nil {
		t.Fatalf("EnsureNodeProvisioned() error = %v", err)
	}

	targets, err = s.ListProvisionedNodesForScraping(ctx)
	if err != nil {
		t.Fatalf("ListProvisionedNodesForScraping() error = %v", err)
	}
	if len(targets) != 1 || targets[0].NodeID != n.ID || targets[0].UserUUID != u.UUID || targets[0].TargetHost != "test-host" {
		t.Fatalf("ListProvisionedNodesForScraping() = %+v, want a single entry for node %d/user %s/host test-host", targets, n.ID, u.UUID)
	}
}

func equal(a, b []string) bool {
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
