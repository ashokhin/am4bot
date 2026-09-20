// Package auth handles login-password hashing and session tokens for the
// multi-tenant control plane's own accounts -- unrelated to the game
// account credentials stored (encrypted) on each node, which is
// internal/secrets' job instead.
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword returns a bcrypt hash of plaintext, suitable for storing in
// users.password_hash. bcrypt's own per-hash random salt means hashing the
// same password twice yields different output -- expected, not a bug.
func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hashing password: %w", err)
	}

	return string(hash), nil
}

// VerifyPassword reports whether plaintext matches the given bcrypt hash.
// It never returns a useful error for a mismatch -- only bad input (e.g. a
// corrupted hash) does -- so callers should treat any error as "login
// failed" without echoing it back to the user.
func VerifyPassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}
