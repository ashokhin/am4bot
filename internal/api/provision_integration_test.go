//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/ashokhin/am4bot/internal/store"
)

func getProvision(t *testing.T, baseURL string, nodeID int64, token string) *http.Response {
	t.Helper()

	req, err := http.NewRequest("GET", baseURL+"/internal/nodes/"+strconv.FormatInt(nodeID, 10)+"/provision", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /internal/nodes/%d/provision: %v", nodeID, err)
	}

	return resp
}

func TestProvisionEndpointAssignsPlacementAndMintsToken(t *testing.T) {
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

	resp := getProvision(t, srv.URL, n.ID, "test-orchestrator-token")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got ProvisionResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if got.TargetHost != "test-host" {
		t.Fatalf("TargetHost = %q, want test-host", got.TargetHost)
	}
	if got.ContainerName == "" {
		t.Fatal("ContainerName is empty, want an assigned name")
	}
	if got.PrometheusPort < 9200 || got.PrometheusPort > 9299 {
		t.Fatalf("PrometheusPort = %d, want in [9200,9299]", got.PrometheusPort)
	}
	if got.ConfigToken == "" {
		t.Fatal("ConfigToken is empty, want a minted token")
	}
	if got.VPN != nil {
		t.Fatalf("VPN = %+v, want nil (node has no vpn_config_id)", got.VPN)
	}

	// re-provisioning the same node must be idempotent: same placement,
	// same token (not a fresh one each time).
	resp2 := getProvision(t, srv.URL, n.ID, "test-orchestrator-token")
	defer resp2.Body.Close()

	var got2 ProvisionResponse
	if err := json.NewDecoder(resp2.Body).Decode(&got2); err != nil {
		t.Fatalf("decoding second response: %v", err)
	}

	if got2.ContainerName != got.ContainerName || got2.PrometheusPort != got.PrometheusPort {
		t.Fatalf("second provision reassigned placement: got %+v, first was %+v", got2, got)
	}
	if got2.ConfigToken != got.ConfigToken {
		t.Fatal("second provision minted a different config token, want the same one reused")
	}
}

func TestProvisionEndpointIncludesDecryptedVPNBundle(t *testing.T) {
	srv, _, st, enc := testServerWithDeps(t)

	ctx := context.Background()
	u, err := st.CreateUser(ctx, "friend@example.com", "hash", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	ovpnEnc, _ := enc.Encrypt("client\ndev tun\n...")
	userEnc, _ := enc.Encrypt("vpn-user")
	passEnc, _ := enc.Encrypt("vpn-pass")

	region, err := st.CreateVPNRegion(ctx, "us-east", ovpnEnc)
	if err != nil {
		t.Fatalf("CreateVPNRegion() error = %v", err)
	}
	if _, err := st.SetVPNProviderCredentials(ctx, "custom", userEnc, passEnc); err != nil {
		t.Fatalf("SetVPNProviderCredentials() error = %v", err)
	}
	if err := st.SetUserVPNRegion(ctx, u.ID, &region.ID); err != nil {
		t.Fatalf("SetUserVPNRegion() error = %v", err)
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

	resp := getProvision(t, srv.URL, n.ID, "test-orchestrator-token")
	defer resp.Body.Close()

	var got ProvisionResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if got.VPN == nil {
		t.Fatal("VPN is nil, want the decrypted bundle")
	}
	if got.VPN.Username != "vpn-user" || got.VPN.Password != "vpn-pass" {
		t.Fatalf("VPN credentials = %+v, want decrypted vpn-user/vpn-pass", got.VPN)
	}
	if got.VPN.OVPNConfig != "client\ndev tun\n..." {
		t.Fatalf("VPN.OVPNConfig = %q, want the decrypted file contents", got.VPN.OVPNConfig)
	}
	if got.VPN.Region == nil || *got.VPN.Region != "us-east" {
		t.Fatalf("VPN.Region = %v, want us-east", got.VPN.Region)
	}
}

func TestProvisionEndpointRejectsWrongOrchestratorToken(t *testing.T) {
	srv, _, st, _ := testServerWithDeps(t)

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

	resp := getProvision(t, srv.URL, n.ID, "wrong-token")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
