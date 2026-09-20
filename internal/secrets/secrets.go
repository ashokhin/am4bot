// Package secrets encrypts and decrypts sensitive values (game account
// passwords, VPN credentials) at rest in the multi-tenant database, using
// AES-256-GCM with a single master key held outside the database — in the
// API server's environment, never in a config file or a DB column.
//
// This is deliberately a single, server-held key rather than a per-user
// passphrase: there is no user-facing "unlock" step, since the bot needs to
// read credentials on its own on a schedule, with no human present to type
// a passphrase in. Losing the master key means every stored secret becomes
// unrecoverable — treat it like any other production secret (a secrets
// manager, or at minimum a securely-stored environment variable), and back
// it up separately from the database.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeySize is the required length, in bytes, of the master key passed to
// NewEncryptor (AES-256).
const KeySize = 32

// Encryptor encrypts and decrypts strings with a fixed master key.
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor creates an Encryptor from a raw 32-byte AES-256 key. Generate
// one with GenerateKey; store it outside the database (e.g. an environment
// variable or secrets manager), not alongside the ciphertexts it protects.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", KeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: creating AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: creating GCM mode: %w", err)
	}

	return &Encryptor{gcm: gcm}, nil
}

// GenerateKey returns a new random 32-byte AES-256 key, base64-encoded for
// storage in an environment variable. Run this once per deployment; losing
// the result makes every value already encrypted with it unrecoverable.
func GenerateKey() (string, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("secrets: generating key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

// KeyFromBase64 decodes a master key produced by GenerateKey.
func KeyFromBase64(s string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("secrets: decoding key: %w", err)
	}

	if len(key) != KeySize {
		return nil, fmt.Errorf("secrets: key must decode to %d bytes, got %d", KeySize, len(key))
	}

	return key, nil
}

// Encrypt returns plaintext encrypted under the Encryptor's key, as a
// base64 string safe to store in a text column. Each call uses a fresh
// random nonce (prepended to the ciphertext), so encrypting the same
// plaintext twice yields different output — this is expected and required
// for GCM's security guarantees, not a bug to work around.
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secrets: generating nonce: %w", err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt reverses Encrypt. It fails if the master key is wrong or the
// stored value has been tampered with or truncated (GCM authenticates the
// ciphertext, so corruption is detected rather than silently misdecrypted).
func (e *Encryptor) Decrypt(encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secrets: decoding base64: %w", err)
	}

	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("secrets: ciphertext too short")
	}

	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := e.gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("secrets: decrypting (wrong key or corrupted data): %w", err)
	}

	return string(plaintext), nil
}
