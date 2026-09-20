// Command apiserver is the HTTP backend for the multi-tenant control
// plane's React frontend. It owns the Postgres database (users, VPN
// identities, nodes) and authentication; it never touches Docker or
// Ansible directly -- provisioning nodes is a separate orchestrator
// service's job, kept out of this process on purpose.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/ashokhin/am4bot/internal/api"
	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/secrets"
	"github.com/ashokhin/am4bot/internal/store"
	"github.com/ashokhin/am4bot/internal/webui"
)

const appName = "apiserver"

var (
	listenAddr = kingpin.Flag("web.listen-address", "Address the PUBLIC listener (UI + /api/*) listens on -- what your reverse proxy points at.").
			Default(":8080").String()
	internalListenAddr = kingpin.Flag("internal.listen-address", "Address the INTERNAL listener (/internal/*, used by ambot containers and orchestrator) listens on. Deliberately a separate port from --web.listen-address -- never point a public reverse proxy at it, see docs/multi-tenant-hosting.md's Architecture section.").
				Envar("INTERNAL_LISTEN_ADDRESS").Default(":8081").String()
	webRoutePrefix = kingpin.Flag("web.route-prefix", "Mount the PUBLIC listener (UI + /api/*) under this path instead of \"/\", e.g. \"/app\" -- same idea as Prometheus'/Grafana's own route-prefix: lets a reverse proxy serve this UI and other services on the same port/domain with no path-rewriting rules. Never affects the INTERNAL listener. Must start with \"/\" and not end with one.").
			Envar("WEB_ROUTE_PREFIX").Default("").String()
	databaseURL = kingpin.Flag("database-url", "Postgres connection string.").
			Envar("DATABASE_URL").Required().String()
	secretsMasterKey = kingpin.Flag("secrets-master-key", "Base64 AES-256 key for encrypting node/VPN credentials at rest (generate with internal/secrets.GenerateKey).").
				Envar("SECRETS_MASTER_KEY").Required().String()
	jwtSigningKey = kingpin.Flag("jwt-signing-key", "Base64 key for signing session tokens (generate with internal/auth.GenerateSigningKey).").
			Envar("JWT_SIGNING_KEY").Required().String()
	cookieSecure = kingpin.Flag("cookie-secure", "Mark the session cookie Secure (HTTPS-only). Disable only for local plain-HTTP development.").
			Default("true").Bool()
	orchestratorToken = kingpin.Flag("orchestrator-token", "Shared secret cmd/orchestrator authenticates its provisioning requests with.").
				Envar("ORCHESTRATOR_TOKEN").Required().String()
	targetHosts = kingpin.Flag("target-host", "An Ansible inventory host newly provisioned nodes may be assigned to. Repeatable; assignment is round-robin by node id.").
			Envar("TARGET_HOSTS").Required().Strings()
	prometheusPortRangeStart = kingpin.Flag("prometheus-port-range-start", "Start of the port range allocated to nodes' Prometheus endpoints.").
					Default("9200").Int()
	prometheusPortRangeEnd = kingpin.Flag("prometheus-port-range-end", "End (inclusive) of the port range allocated to nodes' Prometheus endpoints.").
				Default("9299").Int()
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
	masterKey, err := secrets.KeyFromBase64(*secretsMasterKey)
	if err != nil {
		return fmt.Errorf("secrets master key: %w", err)
	}

	encryptor, err := secrets.NewEncryptor(masterKey)
	if err != nil {
		return fmt.Errorf("creating encryptor: %w", err)
	}

	// Same "base64 key in an env var" pattern as the secrets master key
	// above, but a different size (see auth.GenerateSigningKey) -- decoded
	// directly rather than via secrets.KeyFromBase64, which enforces
	// secrets.KeySize specifically.
	signingKey, err := base64.StdEncoding.DecodeString(*jwtSigningKey)
	if err != nil {
		return fmt.Errorf("jwt signing key: decoding base64: %w", err)
	}

	tokens, err := auth.NewTokenManager(signingKey)
	if err != nil {
		return fmt.Errorf("creating token manager: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer st.Close()

	if err := ensureBootstrapAdmin(ctx, st); err != nil {
		return fmt.Errorf("ensuring bootstrap admin: %w", err)
	}

	if err := validateRoutePrefix(*webRoutePrefix); err != nil {
		return fmt.Errorf("--web.route-prefix: %w", err)
	}

	// The DistFS root has one extra "dist" path segment (see
	// internal/webui.embed.go's own doc comment on why go:embed can't
	// embed web/dist directly) -- fs.Sub strips it so the SPA file server
	// sees index.html etc. at the FS root, matching how they'll actually
	// be requested.
	uiFS, err := fs.Sub(webui.DistFS, "dist")
	if err != nil {
		return fmt.Errorf("preparing embedded UI filesystem: %w", err)
	}

	publicHandler, internalHandler, err := api.NewServer(st, tokens, encryptor, uiFS, api.ServerOptions{
		CookieSecure:             *cookieSecure,
		OrchestratorToken:        *orchestratorToken,
		TargetHosts:              *targetHosts,
		PrometheusPortRangeStart: *prometheusPortRangeStart,
		PrometheusPortRangeEnd:   *prometheusPortRangeEnd,
		RoutePrefix:              *webRoutePrefix,
	})
	if err != nil {
		return fmt.Errorf("building server: %w", err)
	}

	// WriteTimeout/IdleTimeout/MaxHeaderBytes, on top of ReadHeaderTimeout,
	// close off slowloris-style resource exhaustion: a client that reads
	// its response slowly, or that just holds an idle keep-alive
	// connection open, would otherwise occupy a connection indefinitely
	// once past ReadHeaderTimeout's own window.
	publicSrv := &http.Server{
		Addr:              *listenAddr,
		Handler:           publicHandler,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}
	internalSrv := &http.Server{
		Addr:              *internalListenAddr,
		Handler:           internalHandler,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutdown signal received, stopping")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var wg sync.WaitGroup

		for _, srv := range []*http.Server{publicSrv, internalSrv} {
			wg.Add(1)

			go func(srv *http.Server) {
				defer wg.Done()

				if err := srv.Shutdown(shutdownCtx); err != nil {
					slog.Error("error during HTTP server shutdown", "address", srv.Addr, "error", err)
				}
			}(srv)
		}

		wg.Wait()
	}()

	serveErrs := make(chan error, 2)

	go func() {
		slog.Info(fmt.Sprintf("starting %s (public)", appName), "address", *listenAddr)

		if err := publicSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrs <- fmt.Errorf("public http server: %w", err)

			return
		}

		serveErrs <- nil
	}()

	go func() {
		slog.Info(fmt.Sprintf("starting %s (internal)", appName), "address", *internalListenAddr)

		if err := internalSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrs <- fmt.Errorf("internal http server: %w", err)

			return
		}

		serveErrs <- nil
	}()

	var firstErr error
	for range 2 {
		if err := <-serveErrs; err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return firstErr
	}

	slog.Info("shutdown complete")

	return nil
}

// bootstrapAdminLogin/Password are the well-known first-run credentials --
// deliberately weak, since there's no signup flow to fall back to if
// nobody knows any login at all yet. Safe only because CreateUser always
// starts a new account with must_change_password = true (see its doc
// comment): the very first thing anyone can do with this account is
// change the password, before touching anything else in the UI (see
// ProtectedRoute's forced redirect to /change-password on the frontend).
const (
	bootstrapAdminLogin    = "admin"
	bootstrapAdminPassword = "admin"
)

// validateRoutePrefix enforces --web.route-prefix's documented shape
// before it ever reaches api.NewServer -- "" (disabled) is always fine;
// anything else must start with "/" and not end with one, matching how
// http.StripPrefix and the index.html asset-path rewrite (see
// internal/api/static.go's rewriteIndexHTML) both expect to concatenate
// it directly onto a path that already starts with "/".
func validateRoutePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}

	if !strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("must start with \"/\", got %q", prefix)
	}

	if strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("must not end with \"/\", got %q", prefix)
	}

	return nil
}

// ensureBootstrapAdmin creates the first-run admin account (login/password
// "admin"/"admin", forced to change it on first login) if and only if no
// account exists yet -- so a freshly stood-up stack always has a way in
// without a manual seeding step, but restarting apiserver on an
// already-used database never re-creates or resets it.
func ensureBootstrapAdmin(ctx context.Context, st *store.Store) error {
	count, err := st.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("counting users: %w", err)
	}

	if count > 0 {
		return nil
	}

	hash, err := auth.HashPassword(bootstrapAdminPassword)
	if err != nil {
		return fmt.Errorf("hashing bootstrap admin password: %w", err)
	}

	if _, err := st.CreateUser(ctx, bootstrapAdminLogin, hash, true); err != nil {
		return fmt.Errorf("creating bootstrap admin: %w", err)
	}

	slog.Warn("no users existed yet -- created a bootstrap admin account; log in and change its password immediately",
		"login", bootstrapAdminLogin)

	return nil
}
