package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ashokhin/am4bot/internal/store"
)

// defaultCronSchedules matches config.Config's own package default
// (internal/config's `default:"[\"*/5 * * * *\"]"` tag) -- used when a
// create request doesn't specify any, so a node is never silently stored
// with zero schedules (which config.Config.validate() rejects, meaning
// its ambot container would crash-loop forever on every start otherwise).
var defaultCronSchedules = []string{"*/5 * * * *"}

// validateCronSchedules mirrors config.Config.validate()'s own checks for
// these two fields, at the API layer, so a bad value is rejected here --
// with a helpful 400 -- rather than being accepted, stored, and only
// discovered when the node's ambot container fails to start.
func validateCronSchedules(schedules []string, jitterSeconds int) error {
	if len(schedules) == 0 {
		return errors.New("cron_schedules must contain at least one entry")
	}

	for _, s := range schedules {
		if _, err := cron.ParseStandard(s); err != nil {
			return fmt.Errorf("invalid cron_schedules entry %q: %w", s, err)
		}
	}

	if jitterSeconds < 0 {
		return errors.New("cron_jitter_seconds must be >= 0")
	}

	return nil
}

// validateTimezone mirrors admin_handlers.go's old per-user check, now
// applied per-node: a node's timezone interprets every one of its
// CronSchedules entries (one zone for the whole node, not per entry).
func validateTimezone(tz string) error {
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("unknown timezone %q", tz)
	}

	return nil
}

// allianceIDPattern matches a single alliance ID -- these are purely
// numeric (see internal/bot/stats.go's allianceStatsByID, which embeds
// one directly into a game URL as ?id=<value>), so anything else is
// rejected here rather than silently stored and only discovered as a
// broken navigation once a node with alliance_stats enabled actually runs.
var allianceIDPattern = regexp.MustCompile(`^\d+$`)

// validateExtraConfig checks the handful of nodes.extra_config fields
// that need more than "is it valid JSON" -- everything else in there is
// opaque to apiserver (see internal_handlers.go's doc comment: it's
// merged on top of config.Config's defaults only when ambot itself fetches
// it), so this deliberately only unmarshals the one field it validates,
// via a minimal local struct, rather than needing to know
// config.Config's entire shape. A raw of nil/empty is valid (no
// extra_config at all).
func validateExtraConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}

	var parsed struct {
		AllianceIDs []string `json:"alliance_ids"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("invalid extra_config: %w", err)
	}

	for _, id := range parsed.AllianceIDs {
		if !allianceIDPattern.MatchString(id) {
			return fmt.Errorf("extra_config.alliance_ids entry %q is not a numeric alliance ID", id)
		}
	}

	return nil
}

// nodeResponse is the node shape returned to the frontend -- notably
// missing GamePasswordEnc, which never leaves the store package. There is
// deliberately no way to read a node's game password back out via the
// API, encrypted or not; the frontend only ever writes a new one.
type nodeResponse struct {
	ID                int64           `json:"id"`
	Name              string          `json:"name"`
	GameURL           string          `json:"game_url"`
	GameUsername      string          `json:"game_username"`
	Services          []string        `json:"services"`
	CronSchedules     []string        `json:"cron_schedules"`
	CronJitterSeconds int             `json:"cron_jitter_seconds"`
	TimeoutSeconds    int             `json:"timeout_seconds"`
	ExtraConfig       json.RawMessage `json:"extra_config"`
	Timezone          string          `json:"timezone"`
	Enabled           bool            `json:"enabled"`
	IsDefault         bool            `json:"is_default"`
	// HasGamePassword tells the frontend whether there's already a real
	// password to fall back to if the form's password field is left blank
	// -- true for any node that's been given one, false for a freshly
	// auto-created default node that never has (see the two default nodes
	// created alongside a new user in handleCreateUser). Without this the
	// edit form's "leave blank to keep the current password" placeholder
	// would lie on a node that was never actually configured yet.
	HasGamePassword bool `json:"has_game_password"`
}

func toNodeResponse(n *store.Node) nodeResponse {
	return nodeResponse{
		ID:                n.ID,
		Name:              n.Name,
		GameURL:           n.GameURL,
		GameUsername:      n.GameUsername,
		Services:          []string(n.Services),
		CronSchedules:     []string(n.CronSchedules),
		CronJitterSeconds: n.CronJitterSeconds,
		TimeoutSeconds:    n.TimeoutSeconds,
		ExtraConfig:       n.ExtraConfig,
		Timezone:          n.Timezone,
		Enabled:           n.Enabled,
		IsDefault:         n.IsDefault,
		HasGamePassword:   n.GamePasswordEnc != "",
	}
}

// currentUserID resolves the calling user's internal store id (nodes are
// keyed by that, not by uuid) from the session claims already verified by
// requireAuth. Every node handler needs this first, to scope its query.
func (s *Server) currentUserID(r *http.Request) (int64, error) {
	claims := claimsFromContext(r.Context())

	u, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)
	if err != nil {
		return 0, err
	}

	return u.ID, nil
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	userID, err := s.currentUserID(r)
	if err != nil {
		slog.Error("resolving current user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list nodes")

		return
	}

	nodes, err := s.store.ListNodesByUser(r.Context(), userID)
	if err != nil {
		slog.Error("listing nodes", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list nodes")

		return
	}

	resp := make([]nodeResponse, len(nodes))
	for i, n := range nodes {
		resp[i] = toNodeResponse(&n)
	}

	writeJSON(w, http.StatusOK, resp)
}

type createNodeRequest struct {
	Name              string          `json:"name"`
	GameURL           string          `json:"game_url"`
	GameUsername      string          `json:"game_username"`
	GamePassword      string          `json:"game_password"`
	Services          []string        `json:"services"`
	CronSchedules     []string        `json:"cron_schedules"`
	CronJitterSeconds int             `json:"cron_jitter_seconds"`
	TimeoutSeconds    int             `json:"timeout_seconds"`
	ExtraConfig       json.RawMessage `json:"extra_config"`
	Timezone          string          `json:"timezone"`
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	userID, err := s.currentUserID(r)
	if err != nil {
		slog.Error("resolving current user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create node")

		return
	}

	var req createNodeRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.Name == "" || req.GameUsername == "" || req.GamePassword == "" {
		writeError(w, http.StatusBadRequest, "name, game_username and game_password are required")

		return
	}

	if req.GameURL == "" {
		req.GameURL = "https://www.airlinemanager.com/"
	}
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = 180
	}
	if len(req.CronSchedules) == 0 {
		req.CronSchedules = defaultCronSchedules
	}

	if err := validateCronSchedules(req.CronSchedules, req.CronJitterSeconds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	if req.Timezone == "" {
		req.Timezone = "UTC"
	} else if err := validateTimezone(req.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	if err := validateExtraConfig(req.ExtraConfig); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	gamePasswordEnc, err := s.encryptor.Encrypt(req.GamePassword)
	if err != nil {
		slog.Error("encrypting game password", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create node")

		return
	}

	n, err := s.store.CreateNode(r.Context(), store.NewNodeParams{
		UserID:            userID,
		Name:              req.Name,
		GameURL:           req.GameURL,
		GameUsername:      req.GameUsername,
		GamePasswordEnc:   gamePasswordEnc,
		Services:          req.Services,
		CronSchedules:     req.CronSchedules,
		CronJitterSeconds: req.CronJitterSeconds,
		TimeoutSeconds:    req.TimeoutSeconds,
		ExtraConfig:       req.ExtraConfig,
		Timezone:          req.Timezone,
		Enabled:           true,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "a node with that name already exists")

			return
		}
		if errors.Is(err, store.ErrNodeNotReady) {
			writeError(w, http.StatusBadRequest, err.Error())

			return
		}

		slog.Error("creating node", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create node")

		return
	}

	s.enqueueReconcile(r.Context(), n.ID)

	s.audit(r, "create_node", strconv.FormatInt(n.ID, 10))
	writeJSON(w, http.StatusCreated, toNodeResponse(n))
}

// enqueueReconcile queues a "make this node's containers match its
// current row" job for the orchestrator (see node_operations' doc
// comment in migrations/0001_init.sql). Logged, not surfaced to
// the caller, on failure: the node row itself is already committed by the
// time this runs, so a queueing failure shouldn't turn into a failed
// request -- it just means provisioning is delayed until the next change
// to the node prompts another enqueue.
func (s *Server) enqueueReconcile(ctx context.Context, nodeID int64) {
	if _, err := s.store.EnqueueOperation(ctx, nodeID, store.OpReconcile, nil); err != nil {
		slog.Error("enqueueing reconcile operation", "node_id", nodeID, "error", err)
	}
}

// enqueueNodeDelete snapshots what the orchestrator needs to tear down
// n's containers with and enqueues the delete operation -- shared by
// handleDeleteNode and the admin's delete-user flow (admin_handlers.go),
// which tears down every one of a deleted user's nodes the same way.
// Must be called BEFORE the node row itself is deleted:
// node_operations.node_id references nodes(id) and the row must still
// exist for this insert to succeed; it becomes harmlessly NULL afterward
// via ON DELETE SET NULL (see migrations/0001_init.sql) -- the
// operation survives on its payload snapshot alone from that point on.
// A no-op if n was never provisioned (n.ContainerName == nil) -- nothing
// exists to tear down.
func (s *Server) enqueueNodeDelete(ctx context.Context, n *store.Node) {
	if n.ContainerName == nil {
		return
	}

	payload, err := json.Marshal(store.DeleteOperationPayload{
		ContainerName:    *n.ContainerName,
		VPNContainerName: fmt.Sprintf("vpn-node-%d", n.ID),
		TargetHost:       n.TargetHost,
	})
	if err != nil {
		slog.Error("marshaling delete operation payload", "node_id", n.ID, "error", err)

		return
	}

	if _, err := s.store.EnqueueOperation(ctx, n.ID, store.OpDelete, payload); err != nil {
		slog.Error("enqueueing delete operation", "node_id", n.ID, "error", err)
	}
}

// nodeIDFromPath parses the {id} path value shared by every /api/nodes/{id}
// route.
func nodeIDFromPath(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	userID, err := s.currentUserID(r)
	if err != nil {
		slog.Error("resolving current user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get node")

		return
	}

	id, err := nodeIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node id")

		return
	}

	n, err := s.store.GetNode(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")

			return
		}

		slog.Error("getting node", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get node")

		return
	}

	writeJSON(w, http.StatusOK, toNodeResponse(n))
}

// updateNodeRequest mirrors createNodeRequest but every field is optional
// (a pointer/nil-able slice): only fields present in the request body are
// changed. Services/CronSchedules, when present, replace the array
// wholesale -- see store.UpdateNodeParams's doc comment for why there is
// no partial-array update.
type updateNodeRequest struct {
	Name              *string          `json:"name,omitempty"`
	GameURL           *string          `json:"game_url,omitempty"`
	GameUsername      *string          `json:"game_username,omitempty"`
	GamePassword      *string          `json:"game_password,omitempty"`
	Services          *[]string        `json:"services,omitempty"`
	CronSchedules     *[]string        `json:"cron_schedules,omitempty"`
	CronJitterSeconds *int             `json:"cron_jitter_seconds,omitempty"`
	TimeoutSeconds    *int             `json:"timeout_seconds,omitempty"`
	ExtraConfig       *json.RawMessage `json:"extra_config,omitempty"`
	Timezone          *string          `json:"timezone,omitempty"`
	Enabled           *bool            `json:"enabled,omitempty"`
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	userID, err := s.currentUserID(r)
	if err != nil {
		slog.Error("resolving current user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update node")

		return
	}

	id, err := nodeIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node id")

		return
	}

	var req updateNodeRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	// Only validate the fields this request actually touches -- an update
	// that doesn't mention cron_schedules/cron_jitter_seconds must not be
	// rejected over the node's existing, already-valid values.
	if req.CronSchedules != nil {
		jitter := 0
		if req.CronJitterSeconds != nil {
			jitter = *req.CronJitterSeconds
		}

		if err := validateCronSchedules(*req.CronSchedules, jitter); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())

			return
		}
	} else if req.CronJitterSeconds != nil && *req.CronJitterSeconds < 0 {
		writeError(w, http.StatusBadRequest, "cron_jitter_seconds must be >= 0")

		return
	}

	if req.Timezone != nil {
		if err := validateTimezone(*req.Timezone); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())

			return
		}
	}

	if req.ExtraConfig != nil {
		if err := validateExtraConfig(*req.ExtraConfig); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())

			return
		}
	}

	params := store.UpdateNodeParams{
		Name:              req.Name,
		GameURL:           req.GameURL,
		GameUsername:      req.GameUsername,
		Services:          req.Services,
		CronSchedules:     req.CronSchedules,
		CronJitterSeconds: req.CronJitterSeconds,
		TimeoutSeconds:    req.TimeoutSeconds,
		ExtraConfig:       req.ExtraConfig,
		Timezone:          req.Timezone,
		Enabled:           req.Enabled,
	}

	if req.GamePassword != nil {
		enc, err := s.encryptor.Encrypt(*req.GamePassword)
		if err != nil {
			slog.Error("encrypting game password", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update node")

			return
		}

		params.GamePasswordEnc = &enc
	}

	n, err := s.store.UpdateNode(r.Context(), userID, id, params)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")

			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "a node with that name already exists")

			return
		}
		if errors.Is(err, store.ErrNodeNotReady) {
			writeError(w, http.StatusBadRequest, err.Error())

			return
		}

		slog.Error("updating node", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update node")

		return
	}

	s.enqueueReconcile(r.Context(), n.ID)

	s.audit(r, "update_node", strconv.FormatInt(n.ID, 10))
	writeJSON(w, http.StatusOK, toNodeResponse(n))
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	userID, err := s.currentUserID(r)
	if err != nil {
		slog.Error("resolving current user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete node")

		return
	}

	id, err := nodeIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node id")

		return
	}

	// Snapshot what the orchestrator needs to tear down containers with,
	// and enqueue the delete operation, BEFORE actually deleting the node
	// row below -- node_operations.node_id references nodes(id) and the
	// row must still exist for that insert to succeed. It becomes
	// harmlessly NULL afterward via ON DELETE SET NULL (see
	// migrations/0001_init.sql); the operation survives on its
	// payload snapshot alone from that point on.
	n, err := s.store.GetNode(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")

			return
		}

		slog.Error("looking up node before delete", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete node")

		return
	}

	s.enqueueNodeDelete(r.Context(), n)

	if err := s.store.DeleteNode(r.Context(), userID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")

			return
		}
		if errors.Is(err, store.ErrDefaultNodeNotDeletable) {
			writeError(w, http.StatusConflict, "the default node cannot be deleted, only disabled")

			return
		}

		slog.Error("deleting node", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete node")

		return
	}

	s.audit(r, "delete_node", strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}
