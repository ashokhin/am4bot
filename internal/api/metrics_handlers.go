// Package api's metrics endpoint proxies a fixed set of PromQL queries to
// the admin-configured Prometheus instance (store.PrometheusSettings),
// always with a server-side `{user_uuid="<caller's own uuid>"}` selector
// -- never a client-supplied one -- so a signed-in user can only ever see
// their own nodes' metrics. Every node's metrics carry that label because
// cmd/orchestrator's file_sd targets file tags each scrape target with
// its owning user's uuid (see cmd/orchestrator/prometheus_sd.go); ambot
// itself never needs to know its own tenant.
//
// Only a fixed, hardcoded list of metric names is ever queried -- the
// caller supplies no PromQL of their own -- which is also what makes the
// server-side label injection safe without needing a real PromQL parser:
// every query this builds is `<our own fixed metric name>{user_uuid="<a
// uuid.UUID's own .String() form>"}`, never user-controlled text.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ashokhin/am4bot/internal/store"
)

// exposedMetrics is every scalar (non-vector) gauge internal/metrics
// publishes that's useful on a per-user dashboard -- see
// internal/metrics/prometheus.go. GaugeVec metrics (labeled by aircraft,
// currency, etc.) are deliberately left out of this first version; they
// need a richer widget than a single stat tile.
var exposedMetrics = []string{
	"up",
	"am4_duration_seconds",
	"am4_last_run_timestamp_seconds",
	"am4_next_scheduled_run_timestamp_seconds",
	"am4_company_rank",
	"am4_company_training_points",
	"am4_ac_fleet_size",
	"am4_ac_routes",
	"am4_company_hubs",
	"am4_ac_hangar_capacity",
	"am4_company_share_value",
	"am4_stats_flights_operated_total",
	"am4_alliance_contributed_total",
	"am4_alliance_contributed_per_day",
	"am4_alliance_flights_total",
	"am4_alliance_season_money",
}

type metricsResponse struct {
	Configured bool           `json:"configured"`
	Series     []metricSeries `json:"series"`
}

type metricSeries struct {
	Metric    string  `json:"metric"`
	NodeID    *int64  `json:"node_id,omitempty"`
	Value     float64 `json:"value"`
	Timestamp float64 `json:"timestamp"`
}

func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	settings, err := s.store.GetPrometheusSettings(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, metricsResponse{Configured: false})

			return
		}

		slog.Error("getting prometheus settings", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load metrics")

		return
	}

	series, err := s.queryAllMetrics(r.Context(), settings.URL, claims.UserUUID.String())
	if err != nil {
		slog.Error("querying prometheus", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to query metrics")

		return
	}

	writeJSON(w, http.StatusOK, metricsResponse{Configured: true, Series: series})
}

// handleAdminGetMetrics is the admin counterpart of handleGetMetrics: an
// admin has no nodes of their own (see requireNonAdminUser's doc
// comment), but can look up any user's metrics by uuid -- still always a
// hard server-side label filter, just parameterized by an admin-supplied
// user_uuid instead of the caller's own.
func (s *Server) handleAdminGetMetrics(w http.ResponseWriter, r *http.Request) {
	userUUID := r.URL.Query().Get("user_uuid")
	if userUUID == "" {
		writeError(w, http.StatusBadRequest, "user_uuid query parameter is required")

		return
	}

	// Validate it's a real user's uuid -- not because an invalid one would
	// be unsafe to embed in the PromQL selector (see this file's own doc
	// comment on why that's already safe), just so a typo returns a clear
	// 404 instead of a silently-empty series list.
	parsedUUID, err := uuid.Parse(userUUID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_uuid")

		return
	}

	if _, err := s.store.GetUserByUUID(r.Context(), parsedUUID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")

			return
		}

		slog.Error("looking up user for admin metrics", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load metrics")

		return
	}

	settings, err := s.store.GetPrometheusSettings(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, metricsResponse{Configured: false})

			return
		}

		slog.Error("getting prometheus settings", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load metrics")

		return
	}

	series, err := s.queryAllMetrics(r.Context(), settings.URL, userUUID)
	if err != nil {
		slog.Error("querying prometheus", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to query metrics")

		return
	}

	writeJSON(w, http.StatusOK, metricsResponse{Configured: true, Series: series})
}

// queryAllMetrics runs one instant PromQL query per entry in
// exposedMetrics, concurrently (there are over a dozen, and each is a
// separate HTTP round trip to Prometheus), filtered by userUUID.
func (s *Server) queryAllMetrics(ctx context.Context, prometheusURL, userUUID string) ([]metricSeries, error) {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
		// Never nil: a user with no matching series (e.g. no nodes
		// provisioned yet) must get back `"series": []`, not `null` --
		// the frontend range-iterates this directly.
		result   = []metricSeries{}
		firstErr error
	)

	for _, metric := range exposedMetrics {
		wg.Add(1)

		go func(metric string) {
			defer wg.Done()

			series, err := s.queryOneMetric(ctx, prometheusURL, metric, userUUID)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				if firstErr == nil {
					firstErr = err
				}

				return
			}

			result = append(result, series...)
		}(metric)
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return result, nil
}

// promQueryResponse is the subset of Prometheus' instant-query response
// (https://prometheus.io/docs/prometheus/latest/querying/api/#instant-queries)
// this cares about.
type promQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"` // [unix_timestamp(float64), value(string)]
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

func (s *Server) queryOneMetric(ctx context.Context, prometheusURL, metric, userUUID string) ([]metricSeries, error) {
	query := fmt.Sprintf(`%s{user_uuid=%q}`, metric, userUUID)

	reqURL := prometheusURL + "/api/v1/query?" + url.Values{"query": {query}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building prometheus request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling prometheus: %w", err)
	}
	defer resp.Body.Close()

	var parsed promQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding prometheus response: %w", err)
	}

	if parsed.Status != "success" {
		return nil, fmt.Errorf("prometheus query %q failed: %s", query, parsed.Error)
	}

	series := make([]metricSeries, 0, len(parsed.Data.Result))

	for _, r := range parsed.Data.Result {
		ts, _ := r.Value[0].(float64)

		valStr, _ := r.Value[1].(string)

		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}

		point := metricSeries{Metric: metric, Value: val, Timestamp: ts}

		if nodeIDStr, ok := r.Metric["node_id"]; ok {
			if nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64); err == nil {
				point.NodeID = &nodeID
			}
		}

		series = append(series, point)
	}

	return series, nil
}

type prometheusSettingsResponse struct {
	Configured bool   `json:"configured"`
	URL        string `json:"url,omitempty"`
}

// handleGetPrometheusSettings is admin-only.
func (s *Server) handleGetPrometheusSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetPrometheusSettings(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, prometheusSettingsResponse{Configured: false})

			return
		}

		slog.Error("getting prometheus settings", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load prometheus settings")

		return
	}

	writeJSON(w, http.StatusOK, prometheusSettingsResponse{Configured: true, URL: settings.URL})
}

type setPrometheusSettingsRequest struct {
	URL string `json:"url"`
}

// handleSetPrometheusSettings is admin-only.
func (s *Server) handleSetPrometheusSettings(w http.ResponseWriter, r *http.Request) {
	var req setPrometheusSettingsRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")

		return
	}

	if _, err := url.ParseRequestURI(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, "url must be an absolute URL")

		return
	}

	settings, err := s.store.SetPrometheusSettings(r.Context(), req.URL)
	if err != nil {
		slog.Error("setting prometheus settings", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set prometheus settings")

		return
	}

	s.audit(r, "set_prometheus_settings", settings.URL)
	writeJSON(w, http.StatusOK, prometheusSettingsResponse{Configured: true, URL: settings.URL})
}

// defaultMetricsHTTPClient is a short-timeout client for the Prometheus
// round trips above -- a hung or unreachable Prometheus must not hang the
// request indefinitely.
func defaultMetricsHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}
