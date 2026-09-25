package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/ashokhin/am4bot/internal/api"
)

// composeFile is the small subset of the Compose Spec this project
// needs. Rendered via yaml.v3 (not string templating, and not via
// env_file -- see writeComposeFiles' doc comment) so secret values are
// always correctly YAML-escaped, whatever characters they contain.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image         string            `yaml:"image,omitempty"`
	ContainerName string            `yaml:"container_name,omitempty"`
	PullPolicy    string            `yaml:"pull_policy,omitempty"`
	Restart       string            `yaml:"restart,omitempty"`
	NetworkMode   string            `yaml:"network_mode,omitempty"`
	Environment   map[string]string `yaml:"environment,omitempty"`
	Ports         []string          `yaml:"ports,omitempty"`
	Volumes       []string          `yaml:"volumes,omitempty"`
	CapAdd        []string          `yaml:"cap_add,omitempty"`
	Devices       []string          `yaml:"devices,omitempty"`
	ExtraHosts    []string          `yaml:"extra_hosts,omitempty"`
}

// vpnServiceName is the compose service key the VPN container is defined
// under. ambot's network_mode: "service:<name>" must use THIS name, not the
// container_name (e.g. "vpn-node-1"): compose resolves it against service
// keys, so the container name yields "depends on undefined service".
const vpnServiceName = "vpn"

// ovpnFileName is the filename the vpn service's OpenVPN config is
// written under, inside the node's own directory.
const ovpnFileName = "expressvpn.ovpn"

// writeComposeFiles renders dir/docker-compose.yml (see nodeDir) plus the
// vpn service's *.ovpn file, if any.
//
// Secrets (the config token, VPN credentials) go directly into the
// compose YAML's own "environment:" maps rather than a separate env_file
// -- docker compose's env_file format does its own, simpler parsing that
// does NOT reliably support quoting/escaping (unlike environment:, which
// yaml.v3 marshals with correct YAML string escaping regardless of what
// characters a credential contains). The whole file is written 0600 to
// compensate for secrets living in it; there's no "safe to inspect"
// structure-only copy. Unlike the transient extra-vars file an earlier
// Ansible-based design used, this lives on disk for as long as the node
// exists, since docker compose needs it on every up/stop/restart, not
// just once at creation.
func (r *reconciler) writeComposeFiles(dir string, bundle *api.ProvisionResponse) error {
	compose := composeFile{Services: map[string]composeService{}}

	ambotSvc := composeService{
		Image:         r.ambotImage,
		ContainerName: bundle.ContainerName,
		PullPolicy:    r.ambotPullPolicy,
		Restart:       "unless-stopped",
		Environment: map[string]string{
			"CONFIG_API_URL": r.ambotConfigAPIURL,
			"NODE_ID":        strconv.FormatInt(bundle.NodeID, 10),
			"NODE_TOKEN":     bundle.ConfigToken,
			// cron.New() (cmd/ambot/main.go) uses the process's local
			// time with no explicit location option, so this env var --
			// not any app-level config -- is what makes a node's
			// schedule run in the timezone the user actually picked
			// (see ProvisionResponse.Timezone's doc comment).
			"TZ": bundle.Timezone,
		},
		// lets AMBOT_CONFIG_API_URL be "http://host.docker.internal:<port>"
		// when apiserver runs directly on the host (its current systemd-
		// managed deployment, not itself in a compose project on a shared
		// network) -- harmless if ambotConfigAPIURL points somewhere else.
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}

	if bundle.VPN != nil {
		if err := r.addVPNService(&compose, dir, bundle); err != nil {
			return err
		}

		// ambot shares the vpn container's network stack instead of
		// publishing its own port; the vpn service publishes
		// prometheus_port instead (see addVPNService).
		ambotSvc.NetworkMode = "service:" + vpnServiceName

		// Docker rejects extra_hosts on a container that joins another's
		// network ("conflicting options: custom host-to-IP mapping and
		// the network mode"). The vpn service owns the shared network
		// namespace, so the host.docker.internal mapping goes there
		// instead (see addVPNService) and ambot sees it through it.
		ambotSvc.ExtraHosts = nil
	} else {
		ambotSvc.Ports = []string{fmt.Sprintf("%d:9150", bundle.PrometheusPort)}
	}

	compose.Services["ambot"] = ambotSvc

	data, err := yaml.Marshal(compose)
	if err != nil {
		return fmt.Errorf("marshaling compose file: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), data, 0o600); err != nil {
		return fmt.Errorf("writing docker-compose.yml: %w", err)
	}

	return nil
}

// addVPNService adds the vpn (gluetun) service to compose and writes its
// .ovpn file. Only called when bundle.VPN is non-nil.
func (r *reconciler) addVPNService(compose *composeFile, dir string, bundle *api.ProvisionResponse) error {
	vpn := bundle.VPN

	if err := os.WriteFile(filepath.Join(dir, ovpnFileName), []byte(vpn.OVPNConfig), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", ovpnFileName, err)
	}

	environment := map[string]string{
		"VPN_SERVICE_PROVIDER":  vpn.Provider,
		"VPN_TYPE":              "openvpn",
		"OPENVPN_CUSTOM_CONFIG": "/gluetun/" + ovpnFileName,
		"OPENVPN_USER":          vpn.Username,
		"OPENVPN_PASSWORD":      vpn.Password,
	}
	// SERVER_COUNTRIES only makes sense for gluetun's built-in providers;
	// a "custom" provider (our current, and so far only, real deployment)
	// picks its exit region via the .ovpn file's own server address
	// instead -- Region is stored for that case too, but as metadata,
	// not something gluetun consumes.
	if vpn.Region != nil && vpn.Provider != "custom" {
		environment["SERVER_COUNTRIES"] = *vpn.Region
	}

	compose.Services[vpnServiceName] = composeService{
		Image:         "qmcgaw/gluetun",
		ContainerName: bundle.VPNContainerName,
		PullPolicy:    r.ambotPullPolicy,
		Restart:       "unless-stopped",
		CapAdd:        []string{"NET_ADMIN"},
		Devices:       []string{"/dev/net/tun:/dev/net/tun"},
		Environment:   environment,
		Ports:         []string{fmt.Sprintf("%d:9150", bundle.PrometheusPort)},
		Volumes:       []string{"./" + ovpnFileName + ":/gluetun/" + ovpnFileName + ":ro"},
		// Owns the network namespace ambot joins, so this is where the
		// host.docker.internal mapping for reaching apiserver has to live.
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}

	return nil
}
