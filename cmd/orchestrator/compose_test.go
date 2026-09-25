package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ashokhin/am4bot/internal/api"
)

func renderCompose(t *testing.T, bundle *api.ProvisionResponse) composeFile {
	t.Helper()

	dir := t.TempDir()
	r := &reconciler{ambotImage: "example/ambot:latest", ambotPullPolicy: "always", ambotConfigAPIURL: "http://host.docker.internal:8081"}

	if err := r.writeComposeFiles(dir, bundle); err != nil {
		t.Fatalf("writeComposeFiles: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}

	var c composeFile
	if err := yaml.Unmarshal(data, &c); err != nil {
		t.Fatalf("rendered compose isn't valid YAML: %v", err)
	}

	return c
}

// A node with a VPN region: ambot joins the vpn service's network. That has
// to reference a service that actually exists, and Docker forbids
// extra_hosts on a container using another's network.
func TestComposeVPNNodeIsValid(t *testing.T) {
	c := renderCompose(t, &api.ProvisionResponse{
		NodeID: 1, ContainerName: "ambot-node-1", VPNContainerName: "vpn-node-1", PrometheusPort: 9201, Timezone: "UTC",
		VPN: &api.VPNEnv{Provider: "custom", OVPNConfig: "client", Username: "u", Password: "p"},
	})

	ambot, ok := c.Services["ambot"]
	if !ok {
		t.Fatal("no ambot service")
	}

	target, found := strings.CutPrefix(ambot.NetworkMode, "service:")
	if !found {
		t.Fatalf("ambot network_mode = %q, want service:<vpn service>", ambot.NetworkMode)
	}

	vpn, ok := c.Services[target]
	if !ok {
		t.Fatalf("ambot network_mode references service %q, which is not defined (services: %v)", target, c.Services)
	}

	if len(ambot.ExtraHosts) != 0 {
		t.Fatalf("ambot must not set extra_hosts with network_mode service:, got %v", ambot.ExtraHosts)
	}

	if len(vpn.ExtraHosts) == 0 {
		t.Fatal("the vpn service should carry the host.docker.internal mapping ambot relies on")
	}

	if len(ambot.Ports) != 0 {
		t.Fatalf("ambot must not publish ports when it shares the vpn network, got %v", ambot.Ports)
	}

	if vpn.ContainerName != "vpn-node-1" {
		t.Fatalf("vpn container_name = %q, want vpn-node-1", vpn.ContainerName)
	}
}

func TestComposeNoVPNNode(t *testing.T) {
	c := renderCompose(t, &api.ProvisionResponse{NodeID: 2, ContainerName: "ambot-node-2", PrometheusPort: 9200, Timezone: "UTC"})

	if _, ok := c.Services[vpnServiceName]; ok {
		t.Fatal("no vpn service expected without a VPN region")
	}

	ambot := c.Services["ambot"]
	if ambot.NetworkMode != "" || len(ambot.Ports) != 1 || len(ambot.ExtraHosts) == 0 {
		t.Fatalf("unexpected ambot service without VPN: %+v", ambot)
	}
}
