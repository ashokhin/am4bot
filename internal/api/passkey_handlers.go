package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/ashokhin/am4bot/internal/store"
)

// requirePasskeysEnabled answers 503 and returns false when this server has
// no PublicOrigin configured (s.webAuthn is nil) - see
// ServerOptions.PublicOrigin's doc comment on why passkeys are simply
// unavailable rather than guessed at in that case.
func (s *Server) requirePasskeysEnabled(w http.ResponseWriter) bool {
	if s.webAuthn == nil {
		writeError(w, http.StatusServiceUnavailable, "passkeys are not configured on this server")

		return false
	}

	return true
}

func (s *Server) loadWebAuthnUser(r *http.Request, u *store.User) (webAuthnUser, error) {
	passkeys, err := s.store.ListPasskeysByUser(r.Context(), u.ID)
	if err != nil {
		return webAuthnUser{}, err
	}

	return webAuthnUser{user: u, passkeys: passkeys}, nil
}

// handlePasskeyRegisterBegin starts a registration ceremony for the
// already-authenticated caller - used both by the post-login "add a
// passkey?" prompt and by Settings' "add a passkey" button.
func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !s.requirePasskeysEnabled(w) {
		return
	}

	claims := claimsFromContext(r.Context())

	u, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)
	if err != nil {
		slog.Error("loading user for passkey registration", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start registration")

		return
	}

	wu, err := s.loadWebAuthnUser(r, u)
	if err != nil {
		slog.Error("loading passkeys for registration", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start registration")

		return
	}

	creation, session, err := s.webAuthn.BeginRegistration(wu)
	if err != nil {
		slog.Error("beginning passkey registration", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start registration")

		return
	}

	s.webAuthnSess.put(userUUIDKey(u.UUID), session)
	writeJSON(w, http.StatusOK, creation)
}

// handlePasskeyRegisterFinish completes the ceremony begun above and
// stores the resulting credential. The browser's attestation response
// itself travels as the request body, same shape the library's
// FinishRegistration expects to parse straight from *http.Request - the
// caller-supplied name rides along as a query parameter since the body is
// consumed by the library's own parser.
func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !s.requirePasskeysEnabled(w) {
		return
	}

	claims := claimsFromContext(r.Context())

	u, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)
	if err != nil {
		slog.Error("loading user for passkey registration", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to finish registration")

		return
	}

	session, ok := s.webAuthnSess.take(userUUIDKey(u.UUID))
	if !ok {
		writeError(w, http.StatusBadRequest, "registration session expired or not found, please retry")

		return
	}

	wu, err := s.loadWebAuthnUser(r, u)
	if err != nil {
		slog.Error("loading passkeys for registration", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to finish registration")

		return
	}

	cred, err := s.webAuthn.FinishRegistration(wu, session, r)
	if err != nil {
		slog.Warn("finishing passkey registration", "user_uuid", u.UUID, "error", err)
		writeError(w, http.StatusBadRequest, "could not verify the new passkey")

		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "Passkey"
	}

	data, err := encodeCredential(cred)
	if err != nil {
		slog.Error("encoding new passkey", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save the new passkey")

		return
	}

	if _, err := s.store.CreatePasskey(r.Context(), u.ID, cred.ID, data, name); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "this passkey is already registered")

			return
		}

		slog.Error("saving new passkey", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save the new passkey")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type passkeyResponse struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (s *Server) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	u, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)
	if err != nil {
		slog.Error("loading user for passkey list", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list passkeys")

		return
	}

	passkeys, err := s.store.ListPasskeysByUser(r.Context(), u.ID)
	if err != nil {
		slog.Error("listing passkeys", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list passkeys")

		return
	}

	resp := make([]passkeyResponse, 0, len(passkeys))
	for _, p := range passkeys {
		resp = append(resp, passkeyResponse{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	u, err := s.store.GetUserByUUID(r.Context(), claims.UserUUID)
	if err != nil {
		slog.Error("loading user for passkey delete", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete passkey")

		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid passkey id")

		return
	}

	if err := s.store.DeletePasskey(r.Context(), u.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "passkey not found")

			return
		}

		slog.Error("deleting passkey", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete passkey")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type skipPasskeyPromptRequest struct {
	DontShowAgain bool `json:"dont_show_again"`
}

func (s *Server) handleSkipPasskeyPrompt(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r.Context())

	var req skipPasskeyPromptRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if err := s.store.SkipPasskeyPrompt(r.Context(), claims.UserUUID, req.DontShowAgain); err != nil {
		slog.Error("recording passkey prompt skip", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record your choice")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handlePasskeyLoginBegin starts a discoverable (usernameless) login
// ceremony - the "Войти с passkey" button on the login page, no login
// typed first. Keyed by a random flow id (see webAuthnSessions' own doc
// comment on why this, unlike registration, can't be keyed by user).
func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !s.requirePasskeysEnabled(w) {
		return
	}

	assertion, session, err := s.webAuthn.BeginDiscoverableLogin()
	if err != nil {
		slog.Error("beginning passkey login", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start passkey login")

		return
	}

	flowID, err := newFlowID()
	if err != nil {
		slog.Error("generating passkey login flow id", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start passkey login")

		return
	}

	s.webAuthnSess.put(flowID, session)
	writeJSON(w, http.StatusOK, struct {
		FlowID    string                        `json:"flow_id"`
		Assertion *protocol.CredentialAssertion `json:"assertion"`
	}{FlowID: flowID, Assertion: assertion})
}

func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !s.requirePasskeysEnabled(w) {
		return
	}

	flowID := r.URL.Query().Get("flow_id")
	if flowID == "" {
		writeError(w, http.StatusBadRequest, "missing flow_id")

		return
	}

	session, ok := s.webAuthnSess.take(flowID)
	if !ok {
		writeError(w, http.StatusBadRequest, "login session expired or not found, please retry")

		return
	}

	var resolved *store.User

	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		id, err := uuid.FromBytes(userHandle)
		if err != nil {
			return nil, err
		}

		u, err := s.store.GetUserByUUID(r.Context(), id)
		if err != nil {
			return nil, err
		}

		resolved = u

		wu, err := s.loadWebAuthnUser(r, u)
		if err != nil {
			return nil, err
		}

		return wu, nil
	}

	_, cred, err := s.webAuthn.FinishPasskeyLogin(handler, session, r)
	if err != nil {
		slog.Warn("finishing passkey login", "error", err)
		writeError(w, http.StatusUnauthorized, "passkey login failed")

		return
	}

	if resolved == nil || resolved.DisabledAt != nil {
		writeError(w, http.StatusUnauthorized, "passkey login failed")

		return
	}

	pk, err := s.store.GetPasskeyByCredentialID(r.Context(), cred.ID)
	if err != nil {
		slog.Error("loading used passkey", "error", err)
	} else {
		data, err := encodeCredential(cred)
		if err != nil {
			slog.Error("encoding updated passkey", "error", err)
		} else if err := s.store.UpdatePasskeyUsage(r.Context(), pk.ID, data); err != nil {
			slog.Error("updating passkey usage", "error", err)
		}
	}

	token, err := s.tokens.Issue(resolved.UUID, resolved.Login, resolved.IsAdmin)
	if err != nil {
		slog.Error("issuing session token", "error", err)
		writeError(w, http.StatusInternalServerError, "login failed")

		return
	}

	ip := s.clientIP(r)
	ua := r.UserAgent()

	if err := s.store.RecordLoginAttempt(r.Context(), resolved.Login, true, ip, ua); err != nil {
		slog.Error("recording login attempt", "error", err)
	}

	if err := s.store.SetUserLastLogin(r.Context(), resolved.UUID, ip, ua); err != nil {
		slog.Error("recording last login", "user_uuid", resolved.UUID, "error", err)
	}

	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, toUserResponse(resolved, s.recordSuccessfulLogin(r, resolved)))
}
