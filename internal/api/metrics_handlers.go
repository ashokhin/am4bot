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
	"strings"
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

// deltaMetrics is the metrics worth showing as a "how much changed over
// this window" delta -- currently just am4_flights_departed_total, a
// real Counter this node's own depart service increments locally (see
// internal/bot/depart.go and internal/metrics/prometheus.go's doc
// comment on it). Deliberately NOT am4_stats_flights_operated_total (an
// early version of this widget used that instead): it's the whole
// airline account's lifetime flights, read off a game page, identical
// across every node logged into the same account regardless of which
// services that node runs -- so a node with only e.g. a maintenance
// service enabled, no depart at all, showed a nonzero "flights
// dispatched" delta simply because ANOTHER node on the same account had
// depart enabled. am4_flights_departed_total has no such cross-node
// leakage: it only ever grows when THIS node's own depart() dispatches
// aircraft, and reads 0/no-data for a node that never runs depart.
var deltaMetrics = []string{
	"am4_flights_departed_total",
}

// deltaPeriods is the fixed set of lookback windows both the delta
// widget's and the balance chart's period buttons offer -- validated
// against, never passed through client-supplied text straight into a
// PromQL range vector or a query_range start time, even though a plain
// duration string couldn't inject anything unsafe on its own; keeping
// this list authoritative also means the UI can't ask for a window the
// backend didn't intend to support.
var deltaPeriods = map[string]bool{
	"24h": true,
	"3d":  true,
	"7d":  true,
	"14d": true,
	"30d": true,
}

// rangeStepFor is query_range's resolution for each of deltaPeriods --
// chosen to keep every period at roughly 150-350 points, plenty smooth
// for a line chart without asking Prometheus (or the browser) to push
// thousands of points for a 30-day window.
var rangeStepFor = map[string]string{
	"24h": "5m",
	"3d":  "20m",
	"7d":  "1h",
	"14d": "2h",
	"30d": "4h",
}

// balanceMetric is the one gauge the balance-over-time chart plots --
// am4_company_money is a GaugeVec keyed by in-game account name (see
// internal/bot/money.go), and "Airline account" is the company's main
// balance, the same figure a player watches in-game; the bot's other
// tracked accounts (maintenance/marketing/training reserves, ...) aren't
// exposed here.
const balanceMetric = "am4_company_money"
const balanceAccountType = "Airline account"

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

// parseDeltaPeriod reads and validates the "period" query parameter
// against deltaPeriods -- shared by handleGetMetricsDelta and
// handleAdminGetMetricsDelta.
func parseDeltaPeriod(r *http.Request) (string, error) {
	period := r.URL.Query().Get("period")
	if !deltaPeriods[period] {
		return "", fmt.Errorf("period must be one of 24h, 3d, 7d, 14d, 30d")
	}

	return period, nil
}

func (s *Server) handleGetMetricsDelta(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	period, err := parseDeltaPeriod(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

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

	series, err := s.queryAllMetricsDelta(r.Context(), settings.URL, claims.UserUUID.String(), period)
	if err != nil {
		slog.Error("querying prometheus", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to query metrics")

		return
	}

	writeJSON(w, http.StatusOK, metricsResponse{Configured: true, Series: series})
}

// handleAdminGetMetricsDelta is the admin counterpart of
// handleGetMetricsDelta, mirroring handleAdminGetMetrics' user_uuid
// query parameter.
func (s *Server) handleAdminGetMetricsDelta(w http.ResponseWriter, r *http.Request) {
	userUUID := r.URL.Query().Get("user_uuid")
	if userUUID == "" {
		writeError(w, http.StatusBadRequest, "user_uuid query parameter is required")

		return
	}

	parsedUUID, err := uuid.Parse(userUUID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_uuid")

		return
	}

	period, err := parseDeltaPeriod(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

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

	series, err := s.queryAllMetricsDelta(r.Context(), settings.URL, userUUID, period)
	if err != nil {
		slog.Error("querying prometheus", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to query metrics")

		return
	}

	writeJSON(w, http.StatusOK, metricsResponse{Configured: true, Series: series})
}

// handleGetMetricsBalance backs the balance-over-time chart: one
// query_range call for balanceMetric across the whole selected period,
// per node. Unlike the scalar/delta endpoints above (one point per
// metric per node), each metricSeries entry in the response is one POINT
// of a node's line -- the frontend groups by node_id itself.
func (s *Server) handleGetMetricsBalance(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	period, err := parseDeltaPeriod(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

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

	series, err := s.queryBalanceRange(r.Context(), settings.URL, claims.UserUUID.String(), period)
	if err != nil {
		slog.Error("querying prometheus", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to query metrics")

		return
	}

	writeJSON(w, http.StatusOK, metricsResponse{Configured: true, Series: series})
}

// handleAdminGetMetricsBalance is the admin counterpart of
// handleGetMetricsBalance, mirroring handleAdminGetMetrics' user_uuid
// query parameter.
func (s *Server) handleAdminGetMetricsBalance(w http.ResponseWriter, r *http.Request) {
	userUUID := r.URL.Query().Get("user_uuid")
	if userUUID == "" {
		writeError(w, http.StatusBadRequest, "user_uuid query parameter is required")

		return
	}

	parsedUUID, err := uuid.Parse(userUUID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_uuid")

		return
	}

	period, err := parseDeltaPeriod(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

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

	series, err := s.queryBalanceRange(r.Context(), settings.URL, userUUID, period)
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

// queryAllMetricsDelta is queryAllMetrics' counterpart for deltaMetrics --
// one delta(...) instant query per entry, concurrently. period must
// already be validated against deltaPeriods by the caller.
func (s *Server) queryAllMetricsDelta(ctx context.Context, prometheusURL, userUUID, period string) ([]metricSeries, error) {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		result   = []metricSeries{}
		firstErr error
	)

	for _, metric := range deltaMetrics {
		wg.Add(1)

		go func(metric string) {
			defer wg.Done()

			series, err := s.queryOneMetricDelta(ctx, prometheusURL, metric, userUUID, period)

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

// queryBalanceRange runs one query_range call for balanceMetric, scoped
// to the caller's user_uuid and balanceAccountType, over [now-period, now]
// at rangeStepFor[period]'s resolution. period must already be validated
// against deltaPeriods by the caller.
func (s *Server) queryBalanceRange(ctx context.Context, prometheusURL, userUUID, period string) ([]metricSeries, error) {
	query := fmt.Sprintf(`%s{user_uuid=%q,type=%q}`, balanceMetric, userUUID, balanceAccountType)

	parsed, err := s.runRangeQuery(ctx, prometheusURL, query, period)
	if err != nil {
		return nil, err
	}

	return toMetricSeriesRange(parsed, balanceMetric), nil
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

	parsed, err := s.runInstantQuery(ctx, prometheusURL, query)
	if err != nil {
		return nil, err
	}

	return toMetricSeries(parsed, metric), nil
}

// queryOneMetricDelta is queryOneMetric's counterpart for the "how much
// changed over this window" widget -- current value minus its own value
// `period` ago. period must already be validated against deltaPeriods by
// the caller.
//
// Deliberately current - (current offset period), NOT PromQL's
// delta()/increase() over a [period] range vector: those EXTRAPOLATE
// beyond whatever data actually exists in the window, which blows up for
// any node younger than the selected period. A brand-new ambot node's
// first scrape captures the account's real lifetime total (e.g.
// "276252" flights already flown before the bot ever ran, synced from
// the game on day one), so if only ~4h of real samples exist inside a
// requested 7d window, delta() stretches that one jump across the full
// 7 days and reports a number bigger than the metric's own current
// value. The offset form has no such failure mode: if the metric didn't
// exist `period` ago, the two sides simply don't match and Prometheus
// returns no data for that series (rendered as "--" by the widget)
// instead of a fabricated number -- verified against this project's own
// live Prometheus instance while building this feature.
func (s *Server) queryOneMetricDelta(ctx context.Context, prometheusURL, metric, userUUID, period string) ([]metricSeries, error) {
	selector := fmt.Sprintf(`%s{user_uuid=%q}`, metric, userUUID)
	query := fmt.Sprintf(`%s - %s offset %s`, selector, selector, period)

	parsed, err := s.runInstantQuery(ctx, prometheusURL, query)
	if err != nil {
		return nil, err
	}

	return toMetricSeries(parsed, metric), nil
}

// runInstantQuery is the shared HTTP/JSON plumbing behind an instant
// PromQL query (https://prometheus.io/docs/prometheus/latest/querying/api/#instant-queries)
// -- query is either a bare metric selector or a full PromQL expression
// (e.g. wrapped in delta(...)); either way Prometheus' /api/v1/query
// handles it the same way.
func (s *Server) runInstantQuery(ctx context.Context, prometheusURL, query string) (promQueryResponse, error) {
	reqURL := prometheusURL + "/api/v1/query?" + url.Values{"query": {query}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return promQueryResponse{}, fmt.Errorf("building prometheus request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return promQueryResponse{}, fmt.Errorf("calling prometheus: %w", err)
	}
	defer resp.Body.Close()

	var parsed promQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return promQueryResponse{}, fmt.Errorf("decoding prometheus response: %w", err)
	}

	if parsed.Status != "success" {
		return promQueryResponse{}, fmt.Errorf("prometheus query %q failed: %s", query, parsed.Error)
	}

	return parsed, nil
}

// toMetricSeries maps a parsed instant-query response onto our own
// metricSeries shape, tagging every point with the metric name the
// caller queried under (a delta(...)-wrapped query's result labels don't
// carry a __name__, so this can't be recovered from parsed itself).
func toMetricSeries(parsed promQueryResponse, metric string) []metricSeries {
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

	return series
}

// promRangeQueryResponse is the subset of Prometheus' range-query
// response (https://prometheus.io/docs/prometheus/latest/querying/api/#range-queries)
// this cares about -- Values (plural, a whole series of [timestamp,
// value] pairs) in place of instant-query's single Value.
type promRangeQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

// runRangeQuery is runInstantQuery's counterpart for /api/v1/query_range,
// covering [now-period, now] at rangeStepFor[period]'s resolution. period
// must already be validated against deltaPeriods (and therefore
// rangeStepFor) by the caller.
func (s *Server) runRangeQuery(ctx context.Context, prometheusURL, query, period string) (promRangeQueryResponse, error) {
	now := time.Now()

	dur, err := parsePromDuration(period)
	if err != nil {
		return promRangeQueryResponse{}, fmt.Errorf("parsing period %q: %w", period, err)
	}

	params := url.Values{
		"query": {query},
		"start": {strconv.FormatInt(now.Add(-dur).Unix(), 10)},
		"end":   {strconv.FormatInt(now.Unix(), 10)},
		"step":  {rangeStepFor[period]},
	}

	reqURL := prometheusURL + "/api/v1/query_range?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return promRangeQueryResponse{}, fmt.Errorf("building prometheus request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return promRangeQueryResponse{}, fmt.Errorf("calling prometheus: %w", err)
	}
	defer resp.Body.Close()

	var parsed promRangeQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return promRangeQueryResponse{}, fmt.Errorf("decoding prometheus response: %w", err)
	}

	if parsed.Status != "success" {
		return promRangeQueryResponse{}, fmt.Errorf("prometheus query %q failed: %s", query, parsed.Error)
	}

	return parsed, nil
}

// parsePromDuration parses one of deltaPeriods' fixed strings ("24h",
// "3d", ...) as a Go time.Duration -- time.ParseDuration itself doesn't
// accept a "d" (day) unit, so the day-based periods get expanded to
// hours first. Only ever called with an already-validated period, so an
// error here would be a programming error (a new deltaPeriods entry
// added without a matching case), not a runtime condition.
func parsePromDuration(period string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(period, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, err
		}

		return time.Duration(n) * 24 * time.Hour, nil
	}

	return time.ParseDuration(period)
}

// toMetricSeriesRange is toMetricSeries' counterpart for a range-query
// response: one metricSeries entry per [timestamp, value] point (not one
// per series), tagged with the metric name and node_id the same way.
func toMetricSeriesRange(parsed promRangeQueryResponse, metric string) []metricSeries {
	series := []metricSeries{}

	for _, r := range parsed.Data.Result {
		var nodeID *int64

		if nodeIDStr, ok := r.Metric["node_id"]; ok {
			if id, err := strconv.ParseInt(nodeIDStr, 10, 64); err == nil {
				nodeID = &id
			}
		}

		for _, v := range r.Values {
			ts, _ := v[0].(float64)

			valStr, _ := v[1].(string)

			val, err := strconv.ParseFloat(valStr, 64)
			if err != nil {
				continue
			}

			series = append(series, metricSeries{Metric: metric, NodeID: nodeID, Value: val, Timestamp: ts})
		}
	}

	return series
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
