package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

// prometheusSDTarget is one entry of Prometheus' file_sd_config JSON
// format (https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config).
// user_uuid is the label apiserver's metrics endpoint filters every query
// by -- see internal/api/metrics_handlers.go's doc comment -- so a
// scraped node's metrics are always attributable to exactly one user,
// without ambot itself needing to know its own tenant.
type prometheusSDTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

// writePrometheusSDFile regenerates the whole file_sd targets file from
// the database's current state. Called after every processed operation
// (see main.go) -- cheap full regeneration rather than incremental
// patching, since the number of nodes this hosts is small and Prometheus
// only re-reads the file when its mtime changes, so an unchanged write is
// harmless. A no-op if --prometheus-sd-file wasn't set.
func (r *reconciler) writePrometheusSDFile(ctx context.Context) {
	if r.prometheusSDFile == "" {
		return
	}

	targets, err := r.store.ListProvisionedNodesForScraping(ctx)
	if err != nil {
		slog.Error("listing provisioned nodes for prometheus sd file", "error", err)

		return
	}

	sd := make([]prometheusSDTarget, len(targets))
	for i, t := range targets {
		sd[i] = prometheusSDTarget{
			Targets: []string{fmt.Sprintf("%s:%d", t.TargetHost, t.PrometheusPort)},
			Labels: map[string]string{
				"user_uuid": t.UserUUID.String(),
				"node_id":   strconv.FormatInt(t.NodeID, 10),
			},
		}
	}

	data, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		slog.Error("marshaling prometheus sd file", "error", err)

		return
	}

	// Write to a temp file then rename -- Prometheus polls this path
	// periodically and a half-written file mid-scan would be a transient
	// parse error on its side; rename is atomic on the same filesystem.
	tmp := r.prometheusSDFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Error("writing prometheus sd file", "error", err)

		return
	}

	if err := os.Rename(tmp, r.prometheusSDFile); err != nil {
		slog.Error("renaming prometheus sd file into place", "error", err)
	}
}
