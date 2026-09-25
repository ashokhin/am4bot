package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ashokhin/am4bot/internal/api"
	"github.com/ashokhin/am4bot/internal/store"
)

// reconciler holds everything processOperation's two op_type handlers
// need: how to reach apiserver's internal endpoints, and where/how to
// render and run each node's docker-compose.yml.
type reconciler struct {
	store             *store.Store
	apiBaseURL        string
	orchestratorToken string
	// composeDir is the parent directory each node's own
	// composeDir/node-<id>/ subdirectory (compose file + env files) lives
	// under.
	composeDir string
	// ambotImage is the image tag every node's ambot service runs.
	ambotImage string
	// ambotConfigAPIURL is CONFIG_API_URL's value inside the ambot
	// container -- not necessarily the same as apiBaseURL, since this
	// process and the container it configures may reach apiserver over
	// different network paths.
	ambotConfigAPIURL string
	ambotPullPolicy   string
	httpClient        *http.Client
	// prometheusSDFile is the path to regenerate Prometheus' file_sd
	// targets file at after every processed operation -- see
	// prometheus_sd.go. Empty disables this entirely (a no-op, not an
	// error) since it's optional -- see decision #7's Prometheus-labelling
	// design.
	prometheusSDFile string
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// nodeDir returns the directory a node's compose file and env files live
// in. Deterministic from nodeID alone, so deleteNode can find it even
// after the node's own DB row is gone.
func (r *reconciler) nodeDir(nodeID int64) string {
	return filepath.Join(r.composeDir, "node-"+strconv.FormatInt(nodeID, 10))
}

// reconcileNode makes one node's containers match its current desired
// state: fetches the provisioning bundle (placement + whatever secrets
// the VPN container's env unavoidably needs -- see
// api.ProvisionResponse's doc comment on why the ambot container itself
// gets none), renders composeDir/node-<id>/, and runs `docker compose up
// -d` (enabled) or `stop` (disabled, per Node.Enabled -- see its doc
// comment in migrations/0001_init.sql on why that's not a delete).
func (r *reconciler) reconcileNode(ctx context.Context, op store.NodeOperation) error {
	if op.NodeID == nil {
		return errors.New("reconcile operation has no node_id (node may have been deleted before this ran; a delete operation should have been enqueued separately)")
	}

	bundle, err := r.fetchProvisionBundle(ctx, *op.NodeID)
	if err != nil {
		return fmt.Errorf("fetching provisioning bundle: %w", err)
	}

	dir := r.nodeDir(bundle.NodeID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating node directory: %w", err)
	}

	if err := r.writeComposeFiles(dir, bundle); err != nil {
		return fmt.Errorf("rendering compose files: %w", err)
	}

	composePath := filepath.Join(dir, "docker-compose.yml")
	project := "am4bot-node-" + strconv.FormatInt(bundle.NodeID, 10)

	if bundle.Enabled {
		return r.runComposeCommand(ctx, composePath, project, bundleSecrets(bundle), "up", "-d")
	}

	return r.runComposeCommand(ctx, composePath, project, bundleSecrets(bundle), "stop")
}

// deleteNode tears down a node's containers using only the snapshot
// apiserver took before deleting the node row (op.Payload) -- by the time
// this runs there is no node row left to ask apiserver about. nodeDir is
// derived from the node id alone (see its doc comment), so this doesn't
// need the payload to carry a path.
func (r *reconciler) deleteNode(ctx context.Context, op store.NodeOperation) error {
	var payload store.DeleteOperationPayload
	if err := json.Unmarshal(op.Payload, &payload); err != nil {
		return fmt.Errorf("parsing delete operation payload: %w", err)
	}

	nodeID, err := nodeIDFromContainerName(payload.ContainerName)
	if err != nil {
		return fmt.Errorf("determining node directory: %w", err)
	}

	dir := r.nodeDir(nodeID)
	composePath := filepath.Join(dir, "docker-compose.yml")

	if _, err := os.Stat(composePath); errors.Is(err, os.ErrNotExist) {
		// Nothing was ever rendered for this node (it was deleted before
		// its first successful reconcile) -- nothing to tear down.
		return nil
	}

	project := "am4bot-node-" + strconv.FormatInt(nodeID, 10)

	if err := r.runComposeCommand(ctx, composePath, project, nil, "down", "--volumes"); err != nil {
		return err
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing node directory: %w", err)
	}

	return nil
}

// nodeIDFromContainerName recovers a node's id from its container name
// ("ambot-node-<id>-u<uuid-suffix>", assigned by EnsureNodeProvisioned)
// -- the delete payload has no node id of its own (see
// DeleteOperationPayload's doc comment), but does have this. Only the
// digit run right after the prefix is the id; anything from the next '-'
// onward is the owner-uuid suffix appended for `docker ps` readability
// and isn't part of the id.
func nodeIDFromContainerName(containerName string) (int64, error) {
	const prefix = "ambot-node-"

	if len(containerName) <= len(prefix) || containerName[:len(prefix)] != prefix {
		return 0, fmt.Errorf("container name %q doesn't match the expected %q<id>[-u...] shape", containerName, prefix)
	}

	rest := containerName[len(prefix):]

	end := len(rest)
	for i, c := range rest {
		if c < '0' || c > '9' {
			end = i

			break
		}
	}

	return strconv.ParseInt(rest[:end], 10, 64)
}

// fetchProvisionBundle calls apiserver's internal, orchestrator-only
// provisioning endpoint for nodeID.
func (r *reconciler) fetchProvisionBundle(ctx context.Context, nodeID int64) (*api.ProvisionResponse, error) {
	url := r.apiBaseURL + "/internal/nodes/" + strconv.FormatInt(nodeID, 10) + "/provision"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.orchestratorToken)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling apiserver: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var bundle api.ProvisionResponse
	if err := json.Unmarshal(body, &bundle); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &bundle, nil
}

// runComposeCommand runs `docker compose -f composePath -p project
// <args...>`, logging combined output at Debug level (it can include
// secrets that leaked into command output; see writeComposeFiles' doc
// comment on why secrets shouldn't be there in the first place, this is
// just defense in depth).
func (r *reconciler) runComposeCommand(ctx context.Context, composePath, project string, secrets []string, args ...string) error {
	fullArgs := append([]string{"compose", "-f", composePath, "-p", project}, args...)

	cmd := exec.CommandContext(ctx, "docker", fullArgs...)

	output, runErr := cmd.CombinedOutput()

	// Debug, not Info: docker compose can echo back environment values on
	// error (e.g. a misconfigured variable reference), and those values
	// may include what writeComposeFiles wrote to *.env -- keep this out
	// of the default log level rather than relying on it never happening.
	slog.Debug("docker compose output", "project", project, "args", args, "output", string(output))

	if runErr != nil {
		// Without the first line of compose's own message the bare "exit
		// status N" says nothing about why (see composeErrorDetail for how
		// it is kept safe to log at the default level).
		if detail := composeErrorDetail(output, secrets); detail != "" {
			return fmt.Errorf("docker compose (project=%s, args=%v): %w: %s", project, args, runErr, detail)
		}

		return fmt.Errorf("docker compose (project=%s, args=%v): %w", project, args, runErr)
	}

	return nil
}

// maxComposeErrorDetail bounds composeErrorDetail's result.
const maxComposeErrorDetail = 200

// composeErrorDetail returns the first non-empty line of docker compose's
// output, for the error a failed operation is recorded and logged with.
// That output can echo secret values back (a password containing "$" makes
// compose report an interpolation error quoting it, for one), so every
// known secret is replaced with a placeholder first, and only one line,
// capped in length, is ever kept -- the full output stays Debug-only.
func composeErrorDetail(output []byte, secrets []string) string {
	line := ""

	for _, l := range strings.Split(string(output), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l

			break
		}
	}

	for _, secret := range secrets {
		if secret != "" {
			line = strings.ReplaceAll(line, secret, "[redacted]")
		}
	}

	if len(line) > maxComposeErrorDetail {
		line = line[:maxComposeErrorDetail] + "..."
	}

	return line
}

// bundleSecrets lists the secret values a node's compose file contains,
// for composeErrorDetail to scrub.
func bundleSecrets(b *api.ProvisionResponse) []string {
	secrets := []string{b.ConfigToken}

	if b.VPN != nil {
		secrets = append(secrets, b.VPN.Username, b.VPN.Password)
	}

	return secrets
}
