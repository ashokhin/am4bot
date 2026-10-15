package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/store"
)

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// userResponse is the user shape returned to the frontend - notably
// missing PasswordHash, which never leaves the store package.
type userResponse struct {
	UUID               string  `json:"uuid"`
	Login              string  `json:"login"`
	DisplayName        *string `json:"display_name"`
	IsAdmin            bool    `json:"is_admin"`
	Disabled           bool    `json:"disabled"`
	VPNRegionID        *int64  `json:"vpn_region_id"`
	MustChangePassword bool    `json:"must_change_password"`
	// ShowPasskeyPrompt tells the frontend to show the post-login "add a
	// passkey?" modal this time - see recordSuccessfulLogin's doc comment
	// for when this is true.
	ShowPasskeyPrompt bool `json:"show_passkey_prompt"`
}

func toUserResponse(u *store.User, showPasskeyPrompt bool) userResponse {
	return userResponse{
		UUID:               u.UUID.String(),
		Login:              u.Login,
		DisplayName:        u.DisplayName,
		IsAdmin:            u.IsAdmin,
		Disabled:           u.DisabledAt != nil,
		VPNRegionID:        u.VPNRegionID,
		MustChangePassword: u.MustChangePassword,
		ShowPasskeyPrompt:  showPasskeyPrompt,
	}
}

// recordSuccessfulLogin is the bookkeeping every successful sign-in needs
// to do regardless of method (password, here, or passkey - see
// handlePasskeyLoginFinish): bump the login counter and report whether the
// add-a-passkey prompt is due. The prompt is never due for a user who
// already has one - IncrementLoginCount's own counter has no way to know
// that, so it's checked here instead.
func (s *Server) recordSuccessfulLogin(r *http.Request, u *store.User) bool {
	promptDue, err := s.store.IncrementLoginCount(r.Context(), u.UUID)
	if err != nil {
		slog.Error("incrementing login count", "user_uuid", u.UUID, "error", err)

		return false
	}

	if !promptDue {
		return false
	}

	n, err := s.store.CountPasskeysByUser(r.Context(), u.ID)
	if err != nil {
		slog.Error("counting passkeys", "user_uuid", u.UUID, "error", err)

		return false
	}

	return n == 0
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	ua := r.UserAgent()

	var req loginRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	// Checked BEFORE the user lookup/bcrypt: a banned caller shouldn't get
	// to spend the server's own CPU on credentials it'll reject anyway.
	// See LoginGuard's doc comment for why this is two independent checks
	// (by IP and by login).
	if ban, banned := s.loginGuard.Check(r.Context(), ip, req.Login); banned {
		slog.Warn("login refused: banned", "login", req.Login, "ip", maskIP(ip), "user_agent", ua,
			"ban_key_type", ban.KeyType, "reason", ban.Reason, "unban_at", ban.UnbanAt)
		w.Header().Set("Retry-After", fmt.Sprint(int(time.Until(ban.UnbanAt).Seconds())))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts -- try again later")

		return
	}

	// Deliberately identical error for "no such user" and "wrong
	// password", and no early-return time difference worth exploiting
	// beyond what bcrypt's own constant-time compare already avoids --
	// never reveal whether a login is registered.
	const badCreds = "invalid login or password"

	fail := func() {
		slog.Warn("failed login attempt", "login", req.Login, "ip", maskIP(ip), "user_agent", ua)
		s.loginGuard.RecordFailure(r.Context(), ip, req.Login)

		if err := s.store.RecordLoginAttempt(r.Context(), req.Login, false, ip, ua); err != nil {
			slog.Error("recording login attempt", "error", err)
		}

		// Best-effort - see SetUserLastFailedLogin's doc comment: a
		// failure here must not fail the (already-failed) login response.
		if err := s.store.SetUserLastFailedLogin(r.Context(), req.Login, ip, ua); err != nil {
			slog.Error("recording last failed login", "login", req.Login, "error", err)
		}

		writeError(w, http.StatusUnauthorized, badCreds)
	}

	u, err := s.store.GetUserByLogin(r.Context(), req.Login)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("looking up user for login", "error", err)
		}

		fail()

		return
	}

	if u.DisabledAt != nil || !auth.VerifyPassword(u.PasswordHash, req.Password) {
		fail()

		return
	}

	token, err := s.tokens.Issue(u.UUID, u.Login, u.IsAdmin)
	if err != nil {
		slog.Error("issuing session token", "error", err)
		writeError(w, http.StatusInternalServerError, "login failed")

		return
	}

	s.loginGuard.RecordSuccess(ip, req.Login)

	if err := s.store.RecordLoginAttempt(r.Context(), req.Login, true, ip, ua); err != nil {
		slog.Error("recording login attempt", "error", err)
	}

	// Best-effort - see SetUserLastLogin's doc comment: a failure here
	// must not fail the login itself.
	if err := s.store.SetUserLastLogin(r.Context(), u.UUID, ip, ua); err != nil {
		slog.Error("recording last login", "user_uuid", u.UUID, "error", err)
	}

	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, toUserResponse(u, s.recordSuccessfulLogin(r, u)))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	u, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)
	if err != nil {
		// A valid, unexpired token whose user has vanished (e.g. deleted
		// directly in the database) - treat it the same as "not logged
		// in" rather than a server error.
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "not authenticated")

			return
		}

		slog.Error("looking up current user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load user")

		return
	}

	// Not a login event (just the frontend re-checking its session), so no
	// bookkeeping/prompt here - that only happens in handleLogin and
	// handlePasskeyLoginFinish.
	writeJSON(w, http.StatusOK, toUserResponse(u, false))
}
