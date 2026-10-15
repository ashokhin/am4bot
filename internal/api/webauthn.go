package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/ashokhin/am4bot/internal/store"
)

// encodeCredential/decodeCredential are Passkey.Data's codec - see that
// field's own doc comment on why the library's whole Credential struct is
// kept together rather than mapped into separate columns.
func encodeCredential(c *webauthn.Credential) ([]byte, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("encoding credential: %w", err)
	}

	return data, nil
}

func decodeCredential(data []byte) (webauthn.Credential, error) {
	var c webauthn.Credential
	if err := json.Unmarshal(data, &c); err != nil {
		return webauthn.Credential{}, fmt.Errorf("decoding credential: %w", err)
	}

	return c, nil
}

// newWebAuthn builds the library's *webauthn.WebAuthn from the deployment's
// public origin (ServerOptions.PublicOrigin, see its own doc comment on
// why that's required config rather than inferred from the request's Host
// header). RPID is the origin's bare hostname - the library itself
// enforces it must be that hostname or a parent domain of it, so deriving
// it from the one configured origin (rather than asking for both
// separately) can't drift into an inconsistent pair.
func newWebAuthn(publicOrigin string) (*webauthn.WebAuthn, error) {
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("--web.public-origin %q is not a valid absolute URL", publicOrigin)
	}

	return webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: "am4bot",
		RPOrigins:     []string{publicOrigin},
	})
}

// webAuthnUser adapts a store.User (plus its passkeys) to the
// webauthn.User interface. UUID, not the internal serial id, is the
// WebAuthn "user handle" - already the one stable, external-facing
// identifier this project uses everywhere else (Prometheus labels, URLs,
// session claims - see migrations/0001_init.sql), so no new column was
// needed just for this.
type webAuthnUser struct {
	user     *store.User
	passkeys []store.Passkey
}

func (w webAuthnUser) WebAuthnID() []byte { return w.user.UUID[:] }
func (w webAuthnUser) WebAuthnName() string {
	return w.user.Login
}

func (w webAuthnUser) WebAuthnDisplayName() string {
	if w.user.DisplayName != nil && *w.user.DisplayName != "" {
		return *w.user.DisplayName
	}

	return w.user.Login
}

// WebAuthnCredentials decodes every stored passkey's JSON blob back into
// the library's own Credential type. A row that fails to decode (should
// never happen barring a manual DB edit or a library upgrade that changes
// the encoding) is skipped with a logged warning rather than failing the
// whole ceremony - one bad row shouldn't lock a user out of every OTHER
// passkey they have.
func (w webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	creds := make([]webauthn.Credential, 0, len(w.passkeys))

	for _, p := range w.passkeys {
		c, err := decodeCredential(p.Data)
		if err != nil {
			slog.Warn("skipping passkey that failed to decode", "passkey_id", p.ID, "error", err)

			continue
		}

		creds = append(creds, c)
	}

	return creds
}

// webAuthnSessionTTL bounds how long a begun-but-not-finished ceremony
// (registration or login) stays valid - generous for a human clicking
// through a passkey prompt, short enough that an abandoned one doesn't
// linger in memory indefinitely.
const webAuthnSessionTTL = 5 * time.Minute

// webAuthnSessions holds in-progress WebAuthn ceremonies between their
// Begin and Finish calls. In-memory, not persisted - same tradeoff as
// LoginGuard's in-memory counters (see its own doc comment): fine for
// this project's single-instance deployment shape, and a lost in-flight
// ceremony on a restart just means the user retries the one click that
// started it.
//
// Registration is keyed by the caller's own user UUID (requireAuth
// already identifies them, and a second concurrent registration attempt
// by the same user simply replaces the first - nothing is lost, the
// stale challenge would've been rejected anyway). Discoverable login has
// no known user yet, so it's keyed by a random opaque flow ID the
// Begin response hands the client and the client echoes back on Finish.
type webAuthnSessions struct {
	mu    sync.Mutex
	byKey map[string]webAuthnSessionEntry
}

type webAuthnSessionEntry struct {
	data    webauthn.SessionData
	expires time.Time
}

func newWebAuthnSessions() *webAuthnSessions {
	return &webAuthnSessions{byKey: make(map[string]webAuthnSessionEntry)}
}

func (s *webAuthnSessions) put(key string, data *webauthn.SessionData) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictExpiredLocked()
	s.byKey[key] = webAuthnSessionEntry{data: *data, expires: time.Now().Add(webAuthnSessionTTL)}
}

// take returns and removes the session for key - a ceremony's Finish step
// is one-shot, successful or not, so there's never a reason to keep a
// session around after the first Finish attempt consumes it.
func (s *webAuthnSessions) take(key string) (webauthn.SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.byKey[key]
	delete(s.byKey, key)

	if !ok || time.Now().After(entry.expires) {
		return webauthn.SessionData{}, false
	}

	return entry.data, true
}

// evictExpiredLocked sweeps stale entries on every put, piggybacking on
// traffic that already holds the lock rather than running a separate
// goroutine/ticker just to garbage-collect what's usually a handful of
// in-flight ceremonies at most.
func (s *webAuthnSessions) evictExpiredLocked() {
	now := time.Now()
	for k, e := range s.byKey {
		if now.After(e.expires) {
			delete(s.byKey, k)
		}
	}
}

// newFlowID returns a random opaque identifier for a discoverable-login
// session - see webAuthnSessions' own doc comment on why that flow (and
// only that one) needs one at all.
func newFlowID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating flow id: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func userUUIDKey(id uuid.UUID) string {
	return "register:" + id.String()
}
