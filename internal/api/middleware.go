package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/store"
)

type contextKey int

const claimsContextKey contextKey = iota

// requireAuth verifies the session cookie and, on success, calls next with
// the request's context carrying the token's claims (retrievable with
// claimsFromContext). Responds 401 without calling next otherwise.
//
// A disabled/deleted account's already-issued JWT is otherwise still
// cryptographically valid until it expires (TokenTTL, 24h) -- there's no
// way to reach into that account's OWN browser and clear its cookie the
// moment an admin disables them (unlike handleLogout, which clears the
// cookie of the SAME browser making that exact request); the server has
// to catch it on that account's own NEXT request instead. This only
// checks on MUTATING requests (POST/PUT/PATCH/DELETE), not GET -- a read
// can't do anything a disabled account wasn't already allowed to have
// seen, so paying a DB round trip on every read (including the 20s
// metrics poll, see web/src/hooks/usePolling.ts) to guard against that
// isn't worth it; catching it the moment they try to actually change
// anything is enough.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			slog.Debug("rejected request: no session cookie", "path", r.URL.Path, "ip", clientIP(r), "user_agent", r.UserAgent())
			writeError(w, http.StatusUnauthorized, "not authenticated")

			return
		}

		claims, err := s.tokens.Verify(cookie.Value)
		if err != nil {
			slog.Warn("rejected request: invalid or expired session token", "path", r.URL.Path, "ip", clientIP(r), "user_agent", r.UserAgent(), "error", err)
			writeError(w, http.StatusUnauthorized, "not authenticated")

			return
		}

		if isMutatingMethod(r.Method) {
			u, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)

			switch {
			case err != nil && errors.Is(err, store.ErrNotFound):
				// The account was deleted since this token was issued.
				slog.Warn("force-logging-out a deleted account", "user_uuid", claims.UserUUID)
				s.clearSessionCookie(w)
				writeError(w, http.StatusUnauthorized, "not authenticated")

				return
			case err != nil:
				// A genuine DB error, not a "this account is gone" one --
				// fail closed on THIS request (don't let an unverifiable
				// write through), but don't force-logout over what might
				// be a transient hiccup: that would punish an otherwise
				// legitimate, still-active user for a server-side problem
				// that has nothing to do with their account.
				slog.Error("checking account status", "user_uuid", claims.UserUUID, "error", err)
				writeError(w, http.StatusInternalServerError, "internal server error")

				return
			case u.DisabledAt != nil:
				slog.Warn("force-logging-out a disabled account", "user_uuid", claims.UserUUID)
				s.clearSessionCookie(w)
				writeError(w, http.StatusUnauthorized, "not authenticated")

				return
			}
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next(w, r.WithContext(ctx))
	}
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// requireAdmin is requireAuth plus an is_admin check. Responds 403 (not
// 404) on a non-admin caller -- these endpoints' existence isn't a secret,
// only their data is.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromContext(r.Context())

		if !claims.IsAdmin {
			writeError(w, http.StatusForbidden, "admin access required")

			return
		}

		next(w, r)
	})
}

// requireNonAdminUser is requireAuth plus a NOT-admin check -- the mirror
// of requireAdmin. Admin accounts have no nodes and no VPN region of
// their own (see decision in the project notes: an admin only curates
// the VPN region catalog/provider account and can see every user's
// nodes, never their own) -- routes gated by this reject an admin caller
// the same way requireAdmin rejects a non-admin one.
func (s *Server) requireNonAdminUser(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromContext(r.Context())

		if claims.IsAdmin {
			writeError(w, http.StatusForbidden, "admin accounts don't have their own nodes or VPN region")

			return
		}

		next(w, r)
	})
}

// claimsFromContext retrieves the claims requireAuth stored. Panics if
// called outside a requireAuth-wrapped handler -- that's a programming
// error (a route registered without the middleware), not a runtime
// condition to handle gracefully.
func claimsFromContext(ctx context.Context) *auth.Claims {
	return ctx.Value(claimsContextKey).(*auth.Claims)
}

// requireOrchestrator guards a route with the single shared
// ServerOptions.OrchestratorToken instead of a user session or a node's
// own per-node config token -- for the one caller that is the
// orchestrator service itself, acting on behalf of the whole system
// rather than one user or one node.
func (s *Server) requireOrchestrator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presented, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing bearer token")

			return
		}

		if subtle.ConstantTimeCompare([]byte(presented), []byte(s.opts.OrchestratorToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid token")

			return
		}

		next(w, r)
	}
}
