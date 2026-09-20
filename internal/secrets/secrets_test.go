package secrets

import "testing"

func testEncryptor(t *testing.T) *Encryptor {
	t.Helper()

	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}

	e, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}

	return e
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	e := testEncryptor(t)

	const plaintext = "hunter2"

	ciphertext, err := e.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	got, err := e.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	if got != plaintext {
		t.Fatalf("Decrypt() = %q, want %q", got, plaintext)
	}
}

func TestEncryptIsNotDeterministic(t *testing.T) {
	e := testEncryptor(t)

	a, err := e.Encrypt("same input")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	b, err := e.Encrypt("same input")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	if a == b {
		t.Fatal("Encrypt() produced identical ciphertext for two calls with the same plaintext; nonce reuse")
	}
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
	e := testEncryptor(t)

	ciphertext, err := e.Encrypt("secret value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	wrongKey := make([]byte, KeySize)
	for i := range wrongKey {
		wrongKey[i] = byte(255 - i)
	}

	wrong, err := NewEncryptor(wrongKey)
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}

	if _, err := wrong.Decrypt(ciphertext); err == nil {
		t.Fatal("Decrypt() with the wrong key succeeded, want error")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	e := testEncryptor(t)

	ciphertext, err := e.Encrypt("secret value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	tampered := []byte(ciphertext)
	// flip a byte well past the nonce, inside the sealed payload
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := e.Decrypt(string(tampered)); err == nil {
		t.Fatal("Decrypt() of tampered ciphertext succeeded, want error")
	}
}

func TestDecryptRejectsTruncatedCiphertext(t *testing.T) {
	e := testEncryptor(t)

	if _, err := e.Decrypt("dG9vc2hvcnQ="); err == nil { // base64("tooshort")
		t.Fatal("Decrypt() of a too-short ciphertext succeeded, want error")
	}
}

func TestNewEncryptorRejectsWrongKeySize(t *testing.T) {
	if _, err := NewEncryptor([]byte("too short")); err == nil {
		t.Fatal("NewEncryptor() with a short key succeeded, want error")
	}
}

func TestGenerateKeyRoundTrip(t *testing.T) {
	encoded, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	key, err := KeyFromBase64(encoded)
	if err != nil {
		t.Fatalf("KeyFromBase64() error = %v", err)
	}

	if len(key) != KeySize {
		t.Fatalf("KeyFromBase64() returned %d bytes, want %d", len(key), KeySize)
	}

	if _, err := NewEncryptor(key); err != nil {
		t.Fatalf("NewEncryptor() with a generated key error = %v", err)
	}
}

func TestGenerateKeyIsRandom(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	b, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	if a == b {
		t.Fatal("GenerateKey() produced identical keys on two calls")
	}
}
