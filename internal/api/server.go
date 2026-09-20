// Package api is the HTTP layer for the multi-tenant control plane: the
// backend a React frontend talks to. It never touches Docker or Ansible
// itself -- that's a separate orchestrator service, deliberately kept out
// of this process so a bug or compromise here can't run arbitrary
// commands on the host (see the design discussion this package grew out
// of). This package only reads and writes the database.
package api

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/secrets"
	"github.com/ashokhin/am4bot/internal/store"
)

// Server holds every dependency the HTTP handlers need and wires up the
// routes. It has no other state -- everything durable lives in Postgres,
// except loginGuard's in-memory counters (its bans are also persisted to
// disk, see LoginGuard's doc comment).
type Server struct {
	store      *store.Store
	tokens     *auth.TokenManager
	encryptor  *secrets.Encryptor
	opts       ServerOptions
	httpClient *http.Client
	loginGuard *LoginGuard
}

// ServerOptions carries the deployment-specific settings NewServer needs
// beyond its store/tokens/encryptor dependencies.
type ServerOptions struct {
	// CookieSecure controls the Secure flag on the session cookie. True in
	// production (served over HTTPS via HAProxy); false only for local
	// plain-HTTP development, where a Secure cookie would never be sent
	// back at all.
	CookieSecure bool
	// OrchestratorToken authenticates cmd/orchestrator's calls to the
	// /internal/nodes/{id}/provision endpoint -- a single shared secret
	// both processes are configured with (unlike a node's own config
	// token, which is per-node and minted, not pre-shared). Required.
	OrchestratorToken string
	// TargetHosts is the pool of Ansible inventory hosts a newly
	// provisioned node is assigned to (round-robin by node id). A single
	// entry is fine -- and is exactly today's deployment -- while still
	// letting the schema/assignment logic support more from day one.
	TargetHosts []string
	// PrometheusPortRangeStart/End bound the per-node metrics ports
	// EnsureNodeProvisioned allocates.
	PrometheusPortRangeStart int
	PrometheusPortRangeEnd   int
	// RoutePrefix mounts the PUBLIC listener (UI + /api/*) under this path
	// instead of "/" -- e.g. "/app", so a reverse proxy can serve this UI
	// and something else (Prometheus at /prometheus, say) on the same
	// port/domain with no path-rewriting rules needed, the same
	// route-prefix trick Prometheus/Grafana themselves offer. "" (the
	// default) mounts at the root, unchanged from before this existed.
	// Never affects the INTERNAL listener -- /internal/* is never meant to
	// sit behind a shared reverse proxy at all, see NewServer's own doc
	// comment on why it's a separate listener in the first place.
	// Validated/normalized (must start with "/", must not end with "/")
	// by cmd/apiserver before it ever reaches here.
	RoutePrefix string
}

// NewServer builds a Server and its two http.Handlers -- see
// ServerOptions' field docs for what each setting controls.
//
// Two handlers, not one, because they're meant to listen on two
// different ports/addresses (cmd/apiserver runs two http.Servers):
//   - public: the UI (uiFS, served with SPA fallback) plus every /api/*
//     route. This is the one a reverse proxy points at.
//   - internal: the /internal/* routes a node's own ambot container and
//     the orchestrator call, bearer-token-authenticated rather than
//     cookie-authenticated. Deliberately on a SEPARATE listener so a
//     misconfigured reverse proxy can't accidentally expose it -- the
//     token alone isn't the only thing standing between these endpoints
//     (which hand back decrypted game/VPN passwords) and the public
//     internet; the port itself never being reachable from outside your
//     own infrastructure is a second, independent layer. See
//     docs/multi-tenant-hosting.md's Architecture section.
func NewServer(st *store.Store, tokens *auth.TokenManager, enc *secrets.Encryptor, uiFS fs.FS, opts ServerOptions) (public, internalHandler http.Handler, err error) {
	s := &Server{
		store:      st,
		tokens:     tokens,
		encryptor:  enc,
		opts:       opts,
		httpClient: defaultMetricsHTTPClient(),
		loginGuard: NewLoginGuard(st),
	}

	publicMux := http.NewServeMux()

	publicMux.HandleFunc("POST /api/auth/login", s.handleLogin)
	publicMux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	publicMux.HandleFunc("GET /api/me", s.requireAuth(s.handleMe))

	publicMux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.handleListUsers))
	publicMux.HandleFunc("POST /api/admin/users", s.requireAdmin(s.handleCreateUser))
	publicMux.HandleFunc("GET /api/admin/users/{uuid}", s.requireAdmin(s.handleGetUserDetail))
	publicMux.HandleFunc("DELETE /api/admin/users/{uuid}", s.requireAdmin(s.handleDeleteUser))
	publicMux.HandleFunc("POST /api/admin/users/{uuid}/disable", s.requireAdmin(s.handleSetUserDisabled(true)))
	publicMux.HandleFunc("POST /api/admin/users/{uuid}/enable", s.requireAdmin(s.handleSetUserDisabled(false)))
	publicMux.HandleFunc("POST /api/admin/users/{uuid}/reset-password", s.requireAdmin(s.handleResetUserPassword))
	publicMux.HandleFunc("POST /api/admin/users/{uuid}/unlock", s.requireAdmin(s.handleUnlockUser))

	// Self-service account settings -- every signed-in user, admin
	// included (an admin still has their own login password/display name,
	// even with no nodes/VPN of their own).
	publicMux.HandleFunc("PUT /api/me/password", s.requireAuth(s.handleSetMyPassword))
	publicMux.HandleFunc("PUT /api/me/display-name", s.requireAuth(s.handleSetMyDisplayName))
	publicMux.HandleFunc("GET /api/me/login-activity", s.requireAuth(s.handleMyLoginActivity))

	// Nodes are a regular (non-admin) user's own concern only -- an admin
	// has none of their own, see requireNonAdminUser's doc comment. Admins
	// instead get a read-only view of every user's nodes below.
	publicMux.HandleFunc("GET /api/nodes", s.requireNonAdminUser(s.handleListNodes))
	publicMux.HandleFunc("POST /api/nodes", s.requireNonAdminUser(s.handleCreateNode))
	publicMux.HandleFunc("GET /api/nodes/{id}", s.requireNonAdminUser(s.handleGetNode))
	publicMux.HandleFunc("PATCH /api/nodes/{id}", s.requireNonAdminUser(s.handleUpdateNode))
	publicMux.HandleFunc("DELETE /api/nodes/{id}", s.requireNonAdminUser(s.handleDeleteNode))

	// Admin-only: every node across every user, read-only (no create/edit
	// -- an admin manages the VPN catalog and provider account, not
	// individual users' nodes).
	publicMux.HandleFunc("GET /api/admin/nodes", s.requireAdmin(s.handleListAllNodes))
	publicMux.HandleFunc("GET /api/admin/nodes/{id}", s.requireAdmin(s.handleAdminGetNode))
	publicMux.HandleFunc("PUT /api/admin/nodes/{id}/log-level", s.requireAdmin(s.handleSetNodeLogLevel))

	// Region catalog: readable by any signed-in user (everyone needs to see
	// it to pick their own region), curated (create/delete) by admins only.
	publicMux.HandleFunc("GET /api/vpn-regions", s.requireAuth(s.handleListVPNRegions))
	publicMux.HandleFunc("POST /api/admin/vpn-regions", s.requireAdmin(s.handleCreateVPNRegion))
	publicMux.HandleFunc("DELETE /api/admin/vpn-regions/{id}", s.requireAdmin(s.handleDeleteVPNRegion))
	publicMux.HandleFunc("GET /api/admin/vpn-provider", s.requireAdmin(s.handleGetVPNProviderStatus))
	publicMux.HandleFunc("PUT /api/admin/vpn-provider", s.requireAdmin(s.handleSetVPNProviderCredentials))

	// Self-service: a user picks their own region, applied to every one of
	// their nodes -- see vpn_handlers.go's doc comment. Not for admins --
	// they have no nodes to apply a region to.
	publicMux.HandleFunc("PUT /api/me/vpn-region", s.requireNonAdminUser(s.handleSetMyVPNRegion))

	// Metrics: any signed-in non-admin user sees only their own nodes'
	// metrics (a hard server-side user_uuid filter, never client-supplied
	// -- see metrics_handlers.go); the Prometheus instance itself is
	// admin-only to configure.
	publicMux.HandleFunc("GET /api/metrics", s.requireNonAdminUser(s.handleGetMetrics))
	publicMux.HandleFunc("GET /api/admin/prometheus", s.requireAdmin(s.handleGetPrometheusSettings))
	publicMux.HandleFunc("PUT /api/admin/prometheus", s.requireAdmin(s.handleSetPrometheusSettings))
	// Admin's own metrics view: any user's nodes, picked by uuid -- see
	// handleAdminGetMetrics's doc comment.
	publicMux.HandleFunc("GET /api/admin/metrics", s.requireAdmin(s.handleAdminGetMetrics))

	// The built React SPA -- everything not matched by a route above
	// (react-router's own client-side routes included) falls through to
	// this, which serves static assets or index.html as appropriate. See
	// spaFileServer's doc comment.
	spa, err := spaFileServer(uiFS, opts.RoutePrefix)
	if err != nil {
		return nil, nil, err
	}

	publicMux.Handle("/", spa)

	internalMux := http.NewServeMux()

	// Bearer-token-authenticated, not cookie-authenticated: a node
	// container calls this, not a browser.
	internalMux.HandleFunc("GET /internal/nodes/{id}/config", s.handleInternalGetNodeConfig)

	// Orchestrator-only, guarded by the shared OrchestratorToken rather
	// than a node's own config token -- the caller here is the
	// orchestrator provisioning a node, not that node's own ambot
	// container.
	internalMux.HandleFunc("GET /internal/nodes/{id}/provision", s.requireOrchestrator(s.handleInternalGetNodeProvision))

	wrap := func(h http.Handler) http.Handler {
		return withRequestLogging(withSecurityHeaders(withRecover(h)))
	}

	// http.StripPrefix("", h) is a documented no-op (returns h unchanged),
	// so this is exactly today's behavior when RoutePrefix is unset --
	// none of the route registrations above (or in the /internal/* block
	// below, which is deliberately NEVER prefixed, see RoutePrefix's doc
	// comment) needed to change to support this.
	publicHandler := http.StripPrefix(opts.RoutePrefix, publicMux)

	return wrap(publicHandler), wrap(internalMux), nil
}

// sessionCookieName is the httpOnly cookie the session token travels in.
// httpOnly (not readable by frontend JS) so an XSS bug can't exfiltrate
// it; the frontend never needs to read the token itself, only to have the
// browser send it back automatically.
const sessionCookieName = "am4bot_session"

// cookiePath scopes the session cookie to RoutePrefix when one is set, so
// it doesn't leak to whatever else a reverse proxy serves on sibling
// paths of the same domain (e.g. Prometheus at /prometheus on the same
// host) -- "/" (every path) when there's no prefix, unchanged from
// before RoutePrefix existed.
func (s *Server) cookiePath() string {
	if s.opts.RoutePrefix == "" {
		return "/"
	}

	return s.opts.RoutePrefix
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     s.cookiePath(),
		HttpOnly: true,
		Secure:   s.opts.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.TokenTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     s.cookiePath(),
		HttpOnly: true,
		Secure:   s.opts.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// withRequestLogging logs every request's method, path, status, and
// duration at debug level -- enough to follow along locally without
// drowning the default log level in traffic noise.
func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		logRequest(r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
