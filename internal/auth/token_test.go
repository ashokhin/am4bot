package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func testTokenManager(t *testing.T) *TokenManager {
	t.Helper()

	m, err := NewTokenManager(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}

	return m
}

func TestIssueVerifyRoundTrip(t *testing.T) {
	m := testTokenManager(t)
	userUUID := uuid.New()

	token, err := m.Issue(userUUID, "admin", true)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	claims, err := m.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if claims.UserUUID != userUUID {
		t.Fatalf("Verify() UserUUID = %v, want %v", claims.UserUUID, userUUID)
	}
	if claims.Login != "admin" {
		t.Fatalf("Verify() Login = %q, want %q", claims.Login, "admin")
	}
	if !claims.IsAdmin {
		t.Fatal("Verify() IsAdmin = false, want true")
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	a := testTokenManager(t)

	bKey := make([]byte, 32)
	for i := range bKey {
		bKey[i] = 1
	}
	b, err := NewTokenManager(bKey)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}

	token, err := a.Issue(uuid.New(), "user1", false)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	if _, err := b.Verify(token); err == nil {
		t.Fatal("Verify() with the wrong key succeeded, want ErrInvalidToken")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	m := testTokenManager(t)

	claims := Claims{
		UserUUID: uuid.New(),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * TokenTTL)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-TokenTTL)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(m.key)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	if _, err := m.Verify(signed); err == nil {
		t.Fatal("Verify() of an expired token succeeded, want ErrInvalidToken")
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	m := testTokenManager(t)

	token, err := m.Issue(uuid.New(), "user1", false)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	// Flip a bit in the decoded signature bytes, not the base64 text: the
	// last base64url character sometimes encodes only a couple of
	// significant bits, so mutating it as text can occasionally decode to
	// the *same* bytes and make this test flaky. A bit flip on the actual
	// signature bytes always changes them.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3 (header.payload.signature)", len(parts))
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}

	sig[0] ^= 0xFF
	parts[2] = base64.RawURLEncoding.EncodeToString(sig)
	tampered := strings.Join(parts, ".")

	if _, err := m.Verify(tampered); err == nil {
		t.Fatal("Verify() of a tampered token succeeded, want ErrInvalidToken")
	}
}

func TestNewTokenManagerRejectsShortKey(t *testing.T) {
	if _, err := NewTokenManager(make([]byte, 16)); err == nil {
		t.Fatal("NewTokenManager() with a short key succeeded, want error")
	}
}

func TestGenerateSigningKeyRoundTrip(t *testing.T) {
	encoded, err := GenerateSigningKey()
	if err != nil {
		t.Fatalf("GenerateSigningKey() error = %v", err)
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decoding GenerateSigningKey() output: %v", err)
	}

	if _, err := NewTokenManager(key); err != nil {
		t.Fatalf("NewTokenManager() with a generated key error = %v", err)
	}
}
