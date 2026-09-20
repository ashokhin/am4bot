// Command orchestrator is the only process allowed to provision Docker
// containers for hosted nodes. It polls node_operations (written only by
// cmd/apiserver, never called directly -- see internal/store's doc
// comment on that table) and, for each pending job, fetches whatever
// secrets it needs transiently from apiserver's internal endpoints,
// renders a per-node docker-compose.yml + env files under --compose-dir,
// and runs `docker compose`. Runs as a user in the "docker" group, not
// root -- see the design discussion this arrangement grew out of for why
// that's still root-equivalent host access, and why it's confined to this
// one process rather than also being apiserver's problem.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/ashokhin/am4bot/internal/store"
)

const appName = "orchestrator"

var (
	databaseURL = kingpin.Flag("database-url", "Postgres connection string.").
			Envar("DATABASE_URL").Required().String()
	apiBaseURL = kingpin.Flag("api-base-url", "Base URL of apiserver's internal endpoints (e.g. http://apiserver:8080).").
			Envar("API_BASE_URL").Required().String()
	orchestratorToken = kingpin.Flag("orchestrator-token", "Shared secret authenticating this process to apiserver's internal endpoints -- must match apiserver's --orchestrator-token.").
				Envar("ORCHESTRATOR_TOKEN").Required().String()
	composeDir = kingpin.Flag("compose-dir", "Directory to render each node's docker-compose.yml and env files under (one subdirectory per node).").
			Envar("COMPOSE_DIR").Required().String()
	ambotImage = kingpin.Flag("ambot-image", "Docker image to run for each node's ambot container.").
			Envar("AMBOT_IMAGE").Default("ashokhin/am4bot:latest").String()
	ambotPullPolicy = kingpin.Flag("ambot-pull-policy", "docker compose pull_policy for the ambot/vpn services -- \"always\" in production; use \"missing\" or \"never\" when --ambot-image is a local-only image (e.g. during development).").
			Envar("AMBOT_PULL_POLICY").Default("always").Enum("always", "missing", "never", "build")
	ambotConfigAPIURL = kingpin.Flag("ambot-config-api-url", "URL ambot containers use to reach apiserver's internal config endpoint -- may differ from --api-base-url if the container reaches apiserver over a different network path than this process does.").
				Envar("AMBOT_CONFIG_API_URL").Required().String()
	pollInterval = kingpin.Flag("poll-interval", "How long to wait between queue polls when there was nothing to do.").
			Default("10s").Duration()
	batchSize = kingpin.Flag("batch-size", "Maximum number of operations to claim per poll.").
			Default("5").Int()
	prometheusSDFile = kingpin.Flag("prometheus-sd-file", "Path to regenerate a Prometheus file_sd_config targets file at after every processed operation, labelling each node's scrape target with its owning user_uuid (see apiserver's /api/metrics endpoint). Unset disables this.").
				Envar("PROMETHEUS_SD_FILE").String()
)

func main() {
	kingpin.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	r := &reconciler{
		store:             st,
		apiBaseURL:        *apiBaseURL,
		orchestratorToken: *orchestratorToken,
		composeDir:        *composeDir,
		ambotImage:        *ambotImage,
		ambotConfigAPIURL: *ambotConfigAPIURL,
		ambotPullPolicy:   *ambotPullPolicy,
		httpClient:        defaultHTTPClient(),
		prometheusSDFile:  *prometheusSDFile,
	}

	slog.Info(appName+" starting", "poll_interval", *pollInterval, "batch_size", *batchSize)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutdown signal received, stopping")

			return nil
		default:
		}

		n := r.pollOnce(ctx, *batchSize)

		if n == 0 {
			select {
			case <-ctx.Done():
				slog.Info("shutdown signal received, stopping")

				return nil
			case <-time.After(*pollInterval):
			}
		}
	}
}

// pollOnce claims and processes one batch of pending operations, returning
// how many it found (0 tells run() it's fine to sleep before polling again).
func (r *reconciler) pollOnce(ctx context.Context, limit int) int {
	ops, err := r.store.ClaimPendingOperations(ctx, limit)
	if err != nil {
		slog.Error("claiming pending operations", "error", err)

		return 0
	}

	for _, op := range ops {
		r.processOperation(ctx, op)
	}

	return len(ops)
}

// processOperation runs one claimed operation to completion and records
// its outcome. Panics inside an op_type handler would otherwise take down
// the whole poll loop for every other node -- not a concern here since
// nothing below panics on bad input, only returns errors, but worth
// calling out as the reason op_type handlers must keep it that way.
func (r *reconciler) processOperation(ctx context.Context, op store.NodeOperation) {
	slog.Info("processing operation", "op_id", op.ID, "op_type", op.OpType, "node_id", op.NodeID)

	var runErr error

	switch op.OpType {
	case store.OpReconcile:
		runErr = r.reconcileNode(ctx, op)
	case store.OpDelete:
		runErr = r.deleteNode(ctx, op)
	default:
		runErr = fmt.Errorf("unknown op_type %q", op.OpType)
	}

	var errMsg *string
	if runErr != nil {
		msg := runErr.Error()
		errMsg = &msg

		slog.Error("operation failed", "op_id", op.ID, "op_type", op.OpType, "error", runErr)
	} else {
		slog.Info("operation succeeded", "op_id", op.ID, "op_type", op.OpType)
	}

	if err := r.store.FinishOperation(ctx, op.ID, runErr == nil, errMsg); err != nil {
		slog.Error("recording operation outcome", "op_id", op.ID, "error", err)
	}

	// Placement (target host, prometheus_port) only ever changes as a
	// side effect of reconcileNode's EnsureNodeProvisioned call, and a
	// delete removes a node from the scrape list entirely -- regenerate
	// after every operation rather than trying to track which ones
	// actually changed placement.
	r.writePrometheusSDFile(ctx)
}
