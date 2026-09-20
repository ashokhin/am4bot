package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/store"
)

type setPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// handleSetMyPassword is self-service for ANY signed-in user, admin
// included -- covers both the forced first-password-change flow (right
// after a login that already proved they know their current password,
// see User.MustChangePassword's doc comment) and a voluntary change from
// Settings. No current-password check: the session cookie itself is
// already proof of who's asking, and re-asking for a password the caller
// either just typed at login or already knows is redundant friction for
// this small, trusted-users deployment. Unlike handleResetUserPassword
// (admin-only, for when a user is locked out and can't authenticate at
// all), this always acts on the CALLER's own account.
func (s *Server) handleSetMyPassword(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	var req setPasswordRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "new_password is required")

		return
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		slog.Error("hashing new password", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to change password")

		return
	}

	if err := s.store.SetUserPasswordHash(r.Context(), claims.UserUUID, newHash, false); err != nil {
		slog.Error("setting new password", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to change password")

		return
	}

	s.audit(r, "change_own_password", claims.UserUUID.String())
	w.WriteHeader(http.StatusNoContent)
}

type setDisplayNameRequest struct {
	DisplayName *string `json:"display_name"`
}

// handleSetMyDisplayName is self-service for any signed-in user -- a
// purely cosmetic label, see store.User.DisplayName's doc comment.
func (s *Server) handleSetMyDisplayName(w http.ResponseWriter, r *http.Request) {
	userID, err := s.currentUserID(r)
	if err != nil {
		slog.Error("resolving current user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set display name")

		return
	}

	var req setDisplayNameRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.DisplayName != nil && *req.DisplayName == "" {
		req.DisplayName = nil
	}

	if err := s.store.SetUserDisplayName(r.Context(), userID, req.DisplayName); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")

			return
		}

		slog.Error("setting display name", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set display name")

		return
	}

	s.audit(r, "set_display_name", strconv.FormatInt(userID, 10))
	w.WriteHeader(http.StatusNoContent)
}

// handleMyLoginActivity returns the caller's own last loginActivityLimit
// login attempts (success and failure both) -- the profile page's "recent
// activity" panel. Separate from /api/me itself (called on every page
// load as the auth check) so that endpoint doesn't grow this extra query
// for a use case only the Settings/profile page actually needs.
func (s *Server) handleMyLoginActivity(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	u, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)
	if err != nil {
		slog.Error("looking up current user for login activity", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load login activity")

		return
	}

	attempts, err := s.store.ListLoginAttemptsByLogin(r.Context(), u.Login, loginActivityLimit)
	if err != nil {
		slog.Error("listing own login activity", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load login activity")

		return
	}

	writeJSON(w, http.StatusOK, toLoginActivityEntries(attempts))
}
