package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// signingKeySize is the byte length GenerateSigningKey produces -- more
// than the 32-byte minimum NewTokenManager requires, since HMAC keys don't
// benefit from being trimmed to the hash's block size the way some other
// primitives do.
const signingKeySize = 64

// GenerateSigningKey returns a new random signing key, base64-encoded for
// storage in an environment variable. Run this once per deployment.
func GenerateSigningKey() (string, error) {
	key := make([]byte, signingKeySize)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("auth: generating signing key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

// TokenTTL is how long an issued session token stays valid. Short-lived by
// session-token standards, deliberately: there is no refresh-token flow
// yet, so a user simply logs in again once it expires.
const TokenTTL = 24 * time.Hour

// ErrInvalidToken covers every way a token can fail to parse/verify: bad
// signature, wrong issuer, expired, malformed. Callers don't need to
// distinguish these -- they all mean "make the user log in again".
var ErrInvalidToken = errors.New("auth: invalid or expired token")

// Claims is what a session token asserts about the caller. UserUUID (not
// the internal serial id) identifies the user -- consistent with it being
// the only user identifier that ever leaves the database (see
// migrations/0001_init.sql). Login is cached here (not just fetched fresh
// from the DB on demand) specifically so structured audit-log lines (see
// internal/api's audit.go) can include a human-readable actor without an
// extra query on every single mutating request -- safe to cache because
// login is immutable once a user is created (no rename-login endpoint
// exists anywhere in this codebase).
type Claims struct {
	UserUUID uuid.UUID `json:"user_uuid"`
	Login    string    `json:"login"`
	IsAdmin  bool      `json:"is_admin"`
	jwt.RegisteredClaims
}

// TokenManager issues and verifies session tokens (JWT, HS256) with a
// single server-held signing key -- there is no per-session state to look
// up, so revoking one SPECIFIC already-issued token before it expires
// isn't possible here; that would need a server-side denylist/session
// store, deliberately left out of this first pass. What DOES exist is
// coarser: internal/api's requireAuth separately re-checks the account's
// disabled_at on every write, which effectively revokes every token for a
// disabled account (not a single token) -- see that middleware's own doc
// comment.
type TokenManager struct {
	key []byte
}

// NewTokenManager creates a TokenManager from a signing key. Generate one
// with GenerateSigningKey; store it outside the database (an environment
// variable, alongside the internal/secrets master key), not in code.
func NewTokenManager(key []byte) (*TokenManager, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("auth: signing key must be at least 32 bytes, got %d", len(key))
	}

	return &TokenManager{key: key}, nil
}

// Issue returns a signed session token asserting the given user identity.
func (m *TokenManager) Issue(userUUID uuid.UUID, login string, isAdmin bool) (string, error) {
	now := time.Now()

	claims := Claims{
		UserUUID: userUUID,
		Login:    login,
		IsAdmin:  isAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(m.key)
	if err != nil {
		return "", fmt.Errorf("auth: signing token: %w", err)
	}

	return signed, nil
}

// Verify parses and validates a session token, returning its claims.
// Returns ErrInvalidToken for anything wrong with it -- see the doc
// comment on that variable for why callers don't get more detail than
// that back.
func (m *TokenManager) Verify(tokenString string) (*Claims, error) {
	var claims Claims

	_, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Method)
		}

		return m.key, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return &claims, nil
}
