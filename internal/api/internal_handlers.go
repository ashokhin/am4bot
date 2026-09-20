package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/creasty/defaults"

	"github.com/ashokhin/am4bot/internal/config"
	"github.com/ashokhin/am4bot/internal/secrets"
	"github.com/ashokhin/am4bot/internal/store"
)

// handleInternalGetNodeConfig serves a node's effective config as JSON --
// what a hosted ambot container fetches instead of reading a local
// config.yaml. Authenticated by the node's own bearer token (minted by
// the orchestrator, stored encrypted as nodes.config_token_enc), not a
// user session: the caller is a container, not a browser.
//
// This is deliberately NOT registered under /api/ in server.go's mux --
// keep it there only if it ends up needing the same cookie-based auth as
// everything else; a bearer-token-only route living under a path prefix a
// reverse proxy can route/firewall differently (e.g. only reachable from
// the Docker network the node containers run in, never from the public
// internet HAProxy exposes /api/ on) is the point.
func (s *Server) handleInternalGetNodeConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node id")

		return
	}

	presentedToken, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")

		return
	}

	n, err := s.store.GetNodeByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// same response whether the node doesn't exist or the token
			// is wrong -- don't let a caller distinguish the two
			writeError(w, http.StatusUnauthorized, "invalid node or token")

			return
		}

		slog.Error("looking up node for internal config fetch", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load node")

		return
	}

	if !s.nodeTokenMatches(n, presentedToken) {
		writeError(w, http.StatusUnauthorized, "invalid node or token")

		return
	}

	cfg, err := s.buildNodeConfig(n)
	if err != nil {
		slog.Error("building node config", "node_id", n.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to build config")

		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

// nodeTokenMatches decrypts n's stored config token and compares it to
// presented in constant time (crypto/subtle), so response timing can't
// leak how much of a guessed token was correct. A node with no token
// minted yet (nil ConfigTokenEnc -- not provisioned by the orchestrator
// yet) never matches anything.
func (s *Server) nodeTokenMatches(n *store.Node, presented string) bool {
	if n.ConfigTokenEnc == nil {
		return false
	}

	stored, err := s.encryptor.Decrypt(*n.ConfigTokenEnc)
	if err != nil {
		slog.Error("decrypting node config token", "node_id", n.ID, "error", err)

		return false
	}

	return subtle.ConstantTimeCompare([]byte(stored), []byte(presented)) == 1
}

// buildNodeConfig assembles the config.Config a node's ambot container
// should run with: package defaults, then the node's dedicated columns,
// then its extra_config JSON overlaid on top (only the fields extra_config
// actually sets are changed -- see config.Config's doc comment and
// TestJSONRoundTripAppliesDefaultsThenOverlay).
func (s *Server) buildNodeConfig(n *store.Node) (*config.Config, error) {
	var cfg config.Config
	if err := defaults.Set(&cfg); err != nil {
		return nil, err
	}

	gamePassword, err := s.encryptor.Decrypt(n.GamePasswordEnc)
	if err != nil {
		return nil, err
	}

	cfg.Url = n.GameURL
	cfg.User = n.GameUsername
	cfg.Password = gamePassword
	cfg.Services = []string(n.Services)
	cfg.CronSchedules = []string(n.CronSchedules)
	cfg.CronJitterSeconds = n.CronJitterSeconds
	cfg.TimeoutSeconds = n.TimeoutSeconds

	if len(n.ExtraConfig) > 0 {
		if err := json.Unmarshal(n.ExtraConfig, &cfg); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

// ProvisionResponse is everything the orchestrator needs to reconcile one
// node's containers via Ansible: non-secret placement info plus whatever
// secrets the *VPN* container's env unavoidably needs (gluetun has no
// equivalent of ambot's "fetch your own config" trick -- it needs real
// credentials at container-start). The ambot container itself gets no
// secrets here at all: just ConfigToken, which it uses to fetch its own
// config from /internal/nodes/{id}/config (handleInternalGetNodeConfig).
type ProvisionResponse struct {
	NodeID   int64  `json:"node_id"`
	UserUUID string `json:"user_uuid"`
	Name     string `json:"name"`
	// Enabled mirrors nodes.enabled: whether the orchestrator should have
	// this node's containers running at all right now, vs. stopped (the
	// user paused it) -- see enabled's doc comment in
	// migrations/0001_init.sql.
	Enabled bool `json:"enabled"`
	// Timezone interprets this node's cron_schedules -- rendered by the
	// orchestrator as the ambot container's TZ env var (see
	// cmd/orchestrator/compose.go), which is how Go's cron.New() (called
	// with no options, so it uses the process's local time) ends up
	// interpreting each schedule in the zone the user actually picked.
	Timezone         string  `json:"timezone"`
	TargetHost       string  `json:"target_host"`
	ContainerName    string  `json:"container_name"`
	VPNContainerName string  `json:"vpn_container_name,omitempty"`
	PrometheusPort   int     `json:"prometheus_port"`
	ConfigToken      string  `json:"config_token"`
	VPN              *VPNEnv `json:"vpn,omitempty"`
}

// VPNEnv is what a node's gluetun container's environment needs, in the
// clear -- decrypted here, in apiserver, and handed to the orchestrator
// only for the duration of one provisioning call. The orchestrator writes
// it straight into a short-lived extra-vars file and never persists it.
type VPNEnv struct {
	Provider   string  `json:"provider"`
	Region     *string `json:"region"`
	OVPNConfig string  `json:"ovpn_config"`
	Username   string  `json:"username"`
	Password   string  `json:"password"`
}

func (s *Server) handleInternalGetNodeProvision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node id")

		return
	}

	n, err := s.store.GetNodeByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")

			return
		}

		slog.Error("looking up node for provisioning", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load node")

		return
	}

	user, err := s.store.GetUserByID(r.Context(), n.UserID)
	if err != nil {
		slog.Error("looking up node's user for provisioning", "node_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load node")

		return
	}

	targetHost := s.opts.TargetHosts[int(id)%len(s.opts.TargetHosts)]

	containerName, prometheusPort, err := s.store.EnsureNodeProvisioned(
		r.Context(), id, targetHost, s.opts.PrometheusPortRangeStart, s.opts.PrometheusPortRangeEnd,
	)
	if err != nil {
		slog.Error("ensuring node provisioned", "node_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to assign node placement")

		return
	}

	configToken, err := s.ensureNodeConfigToken(r.Context(), n)
	if err != nil {
		slog.Error("ensuring node config token", "node_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to provision node")

		return
	}

	resp := ProvisionResponse{
		NodeID:         n.ID,
		UserUUID:       user.UUID.String(),
		Name:           n.Name,
		Enabled:        n.Enabled,
		Timezone:       n.Timezone,
		TargetHost:     targetHost,
		ContainerName:  containerName,
		PrometheusPort: prometheusPort,
		ConfigToken:    configToken,
	}

	// The VPN exit is a per-USER choice (user.VPNRegionID), applied to
	// every one of their nodes uniformly -- not a per-node setting. See
	// vpn_regions.go's doc comment for why: a user's game bot and any
	// other of their tooling must share one IP.
	if user.VPNRegionID != nil {
		resp.VPNContainerName = fmt.Sprintf("vpn-node-%d", n.ID)

		region, err := s.store.GetVPNRegionByID(r.Context(), *user.VPNRegionID)
		if err != nil {
			slog.Error("looking up user's vpn region", "node_id", id, "user_id", n.UserID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to load vpn region")

			return
		}

		creds, err := s.store.GetVPNProviderCredentials(r.Context())
		if err != nil {
			slog.Error("looking up vpn provider credentials", "node_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to load vpn provider credentials")

			return
		}

		ovpnConfig, err := s.encryptor.Decrypt(region.OVPNConfigEnc)
		if err != nil {
			slog.Error("decrypting vpn region ovpn config", "node_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to decrypt vpn config")

			return
		}

		username, err := s.encryptor.Decrypt(creds.VPNUsernameEnc)
		if err != nil {
			slog.Error("decrypting vpn username", "node_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to decrypt vpn config")

			return
		}

		password, err := s.encryptor.Decrypt(creds.VPNPasswordEnc)
		if err != nil {
			slog.Error("decrypting vpn password", "node_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to decrypt vpn config")

			return
		}

		resp.VPN = &VPNEnv{
			Provider:   creds.Provider,
			Region:     &region.Name,
			OVPNConfig: ovpnConfig,
			Username:   username,
			Password:   password,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ensureNodeConfigToken returns n's config token in the clear, minting and
// storing a new one (encrypted) if this is the first time n has been
// provisioned. Reusing an already-minted token across reconciles means a
// node's ambot container doesn't need a new token injected every time it's
// recreated, only the first time.
func (s *Server) ensureNodeConfigToken(ctx context.Context, n *store.Node) (string, error) {
	if n.ConfigTokenEnc != nil {
		return s.encryptor.Decrypt(*n.ConfigTokenEnc)
	}

	token, err := secrets.GenerateKey() // any random value works; reuses the same CSPRNG helper
	if err != nil {
		return "", fmt.Errorf("generating config token: %w", err)
	}

	tokenEnc, err := s.encryptor.Encrypt(token)
	if err != nil {
		return "", fmt.Errorf("encrypting config token: %w", err)
	}

	if err := s.store.SetNodeConfigToken(ctx, n.ID, tokenEnc); err != nil {
		return "", fmt.Errorf("storing config token: %w", err)
	}

	return token, nil
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header. ok is false if the header is missing or malformed.
func bearerToken(r *http.Request) (token string, ok bool) {
	const prefix = "Bearer "

	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}

	token = strings.TrimPrefix(h, prefix)

	return token, token != ""
}
