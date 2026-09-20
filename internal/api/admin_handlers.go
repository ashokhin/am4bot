package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/store"
)

// userListResponse is one row of GET /api/admin/users -- userResponse
// plus what the list screen shows beyond a single user's own /api/me
// shape: node counts and last-login, neither of which a user needs about
// themselves on that endpoint.
type userListResponse struct {
	userResponse
	LastLoginAt *time.Time `json:"last_login_at"`
	ActiveNodes int        `json:"active_nodes"`
	TotalNodes  int        `json:"total_nodes"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsersWithNodeStats(r.Context())
	if err != nil {
		slog.Error("listing users", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list users")

		return
	}

	resp := make([]userListResponse, len(users))
	for i, u := range users {
		resp[i] = userListResponse{
			userResponse: toUserResponse(&u.User),
			LastLoginAt:  u.LastLoginAt,
			ActiveNodes:  u.ActiveNodes,
			TotalNodes:   u.TotalNodes,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// userDetailResponse is GET /api/admin/users/{uuid} -- a single user's
// full admin-visible profile plus their nodes, for the user detail
// screen. Nodes are the same shape the admin's "all nodes" list uses
// (adminNodeResponse), minus OwnerLogin/OwnerUUID -- redundant on a page
// that's already scoped to one user.
type userDetailResponse struct {
	userResponse
	LastLoginAt              *time.Time `json:"last_login_at"`
	LastLoginIP              *string    `json:"last_login_ip"`
	LastLoginUserAgent       *string    `json:"last_login_user_agent"`
	LastFailedLoginAt        *time.Time `json:"last_failed_login_at"`
	LastFailedLoginIP        *string    `json:"last_failed_login_ip"`
	LastFailedLoginUserAgent *string    `json:"last_failed_login_user_agent"`
	// Ban is the user's login's CURRENTLY active brute-force ban (see
	// LoginGuard), nil if not banned. Distinct from Disabled (an admin's
	// own manual action) -- a ban is automatic and time-limited, lifted
	// either by handleUnlockUser or by simply waiting out UnbanAt.
	Ban *banResponse `json:"ban"`
	// LoginActivity is this user's last loginActivityLimit login attempts
	// (success and failure both) -- the same shape/limit as
	// GET /api/me/login-activity, just for a user other than the caller.
	LoginActivity []loginActivityEntry `json:"login_activity"`
	CreatedAt     time.Time            `json:"created_at"`
	VPNRegionName *string              `json:"vpn_region_name"`
	Nodes         []nodeResponse       `json:"nodes"`
}

// banResponse is userDetailResponse's Ban field -- "when and why" a login
// is currently locked, for the admin UI to display alongside the unlock
// button.
type banResponse struct {
	Reason   string    `json:"reason"`
	BannedAt time.Time `json:"banned_at"`
	UnbanAt  time.Time `json:"unban_at"`
}

func (s *Server) handleGetUserDetail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("uuid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")

		return
	}

	u, err := s.store.GetUserByUUID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")

			return
		}

		slog.Error("getting user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load user")

		return
	}

	nodes, err := s.store.ListNodesByUser(r.Context(), u.ID)
	if err != nil {
		slog.Error("listing user's nodes", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load user")

		return
	}

	nodeResp := make([]nodeResponse, len(nodes))
	for i, n := range nodes {
		nodeResp[i] = toNodeResponse(&n)
	}

	attempts, err := s.store.ListLoginAttemptsByLogin(r.Context(), u.Login, loginActivityLimit)
	if err != nil {
		slog.Error("listing user's login activity", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load user")

		return
	}

	resp := userDetailResponse{
		userResponse:             toUserResponse(u),
		LastLoginAt:              u.LastLoginAt,
		LastLoginIP:              u.LastLoginIP,
		LastLoginUserAgent:       u.LastLoginUserAgent,
		LastFailedLoginAt:        u.LastFailedLoginAt,
		LastFailedLoginIP:        u.LastFailedLoginIP,
		LastFailedLoginUserAgent: u.LastFailedLoginUserAgent,
		LoginActivity:            toLoginActivityEntries(attempts),
		CreatedAt:                u.CreatedAt,
		Nodes:                    nodeResp,
	}

	if ban, err := s.store.GetActiveLoginBan(r.Context(), "login", u.Login); err == nil {
		resp.Ban = &banResponse{Reason: ban.Reason, BannedAt: ban.BannedAt, UnbanAt: ban.UnbanAt}
	} else if !errors.Is(err, store.ErrNotFound) {
		slog.Error("checking user's login ban", "error", err)
	}

	if u.VPNRegionID != nil {
		region, err := s.store.GetVPNRegionByID(r.Context(), *u.VPNRegionID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			slog.Error("looking up user's vpn region", "error", err)
		} else if err == nil {
			resp.VPNRegionName = &region.Name
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleUnlockUser lifts the CALLING admin's target user's currently
// active login ban (see LoginGuard), if any -- the UI counterpart of
// LoginGuard banning someone after too many failed attempts. Distinct
// from handleSetUserDisabled: a ban is automatic/time-limited (LoginGuard
// created it, not an admin), while disabled_at is a manual, indefinite
// admin action. Unlocking an account that isn't currently banned is not
// an error -- idempotent, same rationale as LoginGuard.UnlockLogin.
func (s *Server) handleUnlockUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("uuid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")

		return
	}

	u, err := s.store.GetUserByUUID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")

			return
		}

		slog.Error("looking up user to unlock", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unlock user")

		return
	}

	claims := claimsFromContext(r.Context())

	admin, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)
	if err != nil {
		slog.Error("looking up admin for unlock audit", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unlock user")

		return
	}

	var lastFailedIP string
	if u.LastFailedLoginIP != nil {
		lastFailedIP = *u.LastFailedLoginIP
	}

	if err := s.loginGuard.UnlockLogin(r.Context(), u.Login, lastFailedIP, admin.Login); err != nil {
		slog.Error("unlocking user", "user_uuid", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unlock user")

		return
	}

	slog.Info("user unlocked by admin", "user_uuid", id, "login", u.Login, "unlocked_by", admin.Login)
	s.audit(r, "unlock_user", id.String())

	w.WriteHeader(http.StatusNoContent)
}

type createUserRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	// IsAdmin promotes the new account to admin instead of a regular
	// (node-owning) user -- how a second/replacement admin gets created,
	// since there's no signup flow and the bootstrap admin
	// (cmd/apiserver's ensureBootstrapAdmin) always uses the fixed
	// "admin"/"admin" login. Lets an operator create an admin account
	// under their own chosen login and then disable the standard
	// bootstrap "admin" account -- see handleSetUserDisabled's doc
	// comment on why disabling one admin from another is allowed.
	IsAdmin bool `json:"is_admin"`
}

// defaultNodeCronSchedule is what both auto-created default nodes start
// with -- "every 5 minutes" -- though neither can actually run yet: see
// handleCreateUser's doc comment, they're created disabled and stay that
// way until the user fills in real game credentials (store.ErrNodeNotReady
// blocks enabling before that).
const defaultNodeCronSchedule = "*/5 * * * *"

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.Login == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "login and password are required")

		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("hashing new user's password", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create user")

		return
	}

	u, err := s.store.CreateUser(r.Context(), req.Login, passwordHash, req.IsAdmin)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "a user with that login already exists")

			return
		}

		slog.Error("creating user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create user")

		return
	}

	// Admin accounts never get default nodes -- they have no nodes of
	// their own at all (requireNonAdminUser rejects them from every
	// node-owning route), so two disabled, permanently unreachable nodes
	// would just sit there orphaned.
	if req.IsAdmin {
		s.audit(r, "create_user", u.UUID.String())
		writeJSON(w, http.StatusCreated, toUserResponse(u))

		return
	}

	// Every user gets two default nodes up front, both DISABLED: "player"
	// (the game bot itself) and "maintenance" (whatever secondary
	// automation they want alongside it). Deliberately disabled, not just
	// "created without credentials" -- store.CreateNode/UpdateNode now
	// refuse to persist enabled=true until a node has real game
	// credentials, a schedule, and a timezone (store.ErrNodeNotReady), so
	// the user configures each one from their own panel FIRST and only
	// then turns it on. Neither is enqueued for reconcile here for the
	// same reason nothing would be enabled to reconcile toward yet --
	// that happens the first time handleUpdateNode successfully enables one.
	for _, name := range []string{"player", "maintenance"} {
		if _, err := s.store.CreateNode(r.Context(), store.NewNodeParams{
			UserID:        u.ID,
			Name:          name,
			CronSchedules: []string{defaultNodeCronSchedule},
			IsDefault:     true,
		}); err != nil {
			// The user row is already committed at this point; log loudly
			// but don't fail the request outright -- the account exists
			// and is usable, it's just missing one of its default nodes,
			// which is visible and fixable rather than a silently
			// half-created account.
			slog.Error("creating default node for new user", "user_uuid", u.UUID, "node_name", name, "error", err)
		}
	}

	s.audit(r, "create_user", u.UUID.String())
	writeJSON(w, http.StatusCreated, toUserResponse(u))
}

// handleSetUserDisabled returns a handler that enables or disables the
// user identified by the {uuid} path value, depending on disabled.
//
// Deliberately allowed: one admin disabling ANOTHER admin account. There
// is no is_admin check here, only the self-uuid one below -- this is what
// lets an operator create a second admin under their own chosen login
// (see createUserRequest.IsAdmin) and then disable the standard bootstrap
// "admin"/"admin" account, or lets two co-admins moderate each other.
// Only disabling your OWN account is refused (see below).
//
// Note: disabling a user doesn't reach into their own browser to clear
// their session cookie -- that's only possible for the browser making the
// current request (see handleLogout), and this request is the ADMIN's
// browser, not theirs. Their already-issued JWT stays cryptographically
// valid, but requireAuth re-checks disabled_at on that account's own next
// WRITE request and force-clears their cookie then -- see requireAuth's
// doc comment for why that check is write-only, not on every read too.
func (s *Server) handleSetUserDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("uuid"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user id")

			return
		}

		// An admin disabling their OWN account would lock themselves out
		// with no other admin able to fix it (no signup flow, no "forgot
		// password") -- refuse it outright rather than let them shoot
		// themselves in the foot.
		if disabled {
			claims := claimsFromContext(r.Context())
			if claims.UserUUID == id {
				writeError(w, http.StatusForbidden, "you cannot disable your own account")

				return
			}
		}

		if err := s.store.SetUserDisabled(r.Context(), id, disabled); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "user not found")

				return
			}

			slog.Error("updating user disabled state", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update user")

			return
		}

		// Disabling a user must actually stop their containers, not just
		// flag the account -- see DisableAllNodesForUser's doc comment.
		// Re-enabling deliberately does NOT restart them: the user comes
		// back to find their nodes as they left them, and turns each back
		// on themselves.
		if disabled {
			s.stopAllNodesForUser(r.Context(), id)
			s.audit(r, "disable_user", id.String())
		} else {
			s.audit(r, "enable_user", id.String())
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// stopAllNodesForUser disables every enabled node belonging to the user
// identified by uuid and enqueues a reconcile for each, so the
// orchestrator actually stops their containers. Used when an admin
// disables or deletes a user. Best-effort/logged-not-surfaced, same
// rationale as enqueueReconcile: the user's disabled state is already
// committed by the time this runs.
func (s *Server) stopAllNodesForUser(ctx context.Context, id uuid.UUID) {
	u, err := s.store.GetUserByUUID(ctx, id)
	if err != nil {
		slog.Error("looking up user to stop their nodes", "user_uuid", id, "error", err)

		return
	}

	nodeIDs, err := s.store.DisableAllNodesForUser(ctx, u.ID)
	if err != nil {
		slog.Error("disabling user's nodes", "user_uuid", id, "error", err)

		return
	}

	for _, nodeID := range nodeIDs {
		s.enqueueReconcile(ctx, nodeID)
	}
}

type resetUserPasswordRequest struct {
	Password string `json:"password"`
}

// handleResetUserPassword sets a new login password for the user
// identified by the {uuid} path value -- the "forgot password" story for
// an account with no self-service recovery (no email, no signup flow):
// the admin sets a new one out of band and tells the user directly.
// Refuses the admin's own account, same as handleDeleteUser: this always
// sets must_change_password = TRUE (see SetUserPasswordHash), so using it
// on yourself would force you through the change-password screen again
// right after logging in with the password you just set -- use Settings'
// own self-service change instead, which correctly clears that flag.
func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("uuid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")

		return
	}

	claims := claimsFromContext(r.Context())
	if claims.UserUUID == id {
		writeError(w, http.StatusForbidden, "you cannot reset your own password this way -- use Settings instead")

		return
	}

	var req resetUserPasswordRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")

		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("hashing reset password", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to reset password")

		return
	}

	if err := s.store.SetUserPasswordHash(r.Context(), id, passwordHash, true); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")

			return
		}

		slog.Error("resetting user password", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to reset password")

		return
	}

	s.audit(r, "reset_user_password", id.String())
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteUser permanently removes a user and every one of their
// nodes. Their containers are torn down first (enqueueNodeDelete per
// node, same as a regular node delete) -- the node ROWS themselves cascade-
// delete along with the user row (nodes.user_id ON DELETE CASCADE, see
// store.DeleteUser's doc comment), but nothing tells the orchestrator to
// stop the actual containers unless this does it explicitly first.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("uuid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")

		return
	}

	claims := claimsFromContext(r.Context())
	if claims.UserUUID == id {
		writeError(w, http.StatusForbidden, "you cannot delete your own account")

		return
	}

	u, err := s.store.GetUserByUUID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")

			return
		}

		slog.Error("looking up user before delete", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete user")

		return
	}

	nodes, err := s.store.ListNodesByUser(r.Context(), u.ID)
	if err != nil {
		slog.Error("listing user's nodes before delete", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete user")

		return
	}

	for _, n := range nodes {
		s.enqueueNodeDelete(r.Context(), &n)
	}

	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")

			return
		}

		slog.Error("deleting user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete user")

		return
	}

	s.audit(r, "delete_user", id.String())
	w.WriteHeader(http.StatusNoContent)
}

// adminNodeResponse is what GET /api/admin/nodes and
// GET /api/admin/nodes/{id} return -- read-only, across every user, for
// the admin's "see everything" view (see requireNonAdminUser's doc
// comment on why admins have no nodes of their own to manage here, only
// visibility).
type adminNodeResponse struct {
	nodeResponse
	OwnerLogin string `json:"owner_login"`
	OwnerUUID  string `json:"owner_uuid"`
}

func toAdminNodeResponse(n *store.NodeWithOwner) adminNodeResponse {
	return adminNodeResponse{
		nodeResponse: toNodeResponse(&n.Node),
		OwnerLogin:   n.OwnerLogin,
		OwnerUUID:    n.OwnerUUID.String(),
	}
}

func (s *Server) handleListAllNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListAllNodesWithOwner(r.Context())
	if err != nil {
		slog.Error("listing all nodes", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list nodes")

		return
	}

	resp := make([]adminNodeResponse, len(nodes))
	for i, n := range nodes {
		resp[i] = toAdminNodeResponse(&n)
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleAdminGetNode is the single-node counterpart of handleListAllNodes
// -- the admin's read-only node detail screen, reachable for any user's
// node (unlike handleGetNode, which is scoped to the caller's own).
func (s *Server) handleAdminGetNode(w http.ResponseWriter, r *http.Request) {
	id, err := nodeIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node id")

		return
	}

	n, err := s.store.GetNodeWithOwner(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")

			return
		}

		slog.Error("getting node", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get node")

		return
	}

	writeJSON(w, http.StatusOK, toAdminNodeResponse(n))
}

// validLogLevels mirrors promslog's own accepted values (see the main
// README's --log.level flag) -- the same four an ambot process itself
// understands via config_.LogLevel/PromslogConfig.Level.Set.
var validLogLevels = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}

type setNodeLogLevelRequest struct {
	// Empty string clears the override, falling back to config.Config's
	// own default ("info") -- not "invalid", so it's accepted alongside
	// the four real levels rather than 400ing.
	LogLevel string `json:"log_level"`
}

// handleSetNodeLogLevel is the one write an admin can make to another
// user's node -- see SetNodeLogLevel's doc comment on why this single
// field is the deliberate exception to "admin only ever looks, never
// touches" (AdminNodeDetailPage, requireNonAdminUser). Everything else
// about a node -- credentials, schedule, services -- stays owner-only.
func (s *Server) handleSetNodeLogLevel(w http.ResponseWriter, r *http.Request) {
	id, err := nodeIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node id")

		return
	}

	var req setNodeLogLevelRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.LogLevel != "" && !validLogLevels[req.LogLevel] {
		writeError(w, http.StatusBadRequest, "log_level must be one of debug, info, warn, error, or empty to clear it")

		return
	}

	if err := s.store.SetNodeLogLevel(r.Context(), id, req.LogLevel); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")

			return
		}

		slog.Error("setting node log level", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set node log level")

		return
	}

	s.audit(r, "set_node_log_level", strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}
