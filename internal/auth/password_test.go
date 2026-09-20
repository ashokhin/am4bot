package auth

import "testing"

func TestHashAndVerifyPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("VerifyPassword() = false for the correct password, want true")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if VerifyPassword(hash, "wrong password") {
		t.Fatal("VerifyPassword() = true for the wrong password, want false")
	}
}

func TestHashPasswordIsNotDeterministic(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	b, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if a == b {
		t.Fatal("HashPassword() produced identical hashes for two calls; salt not applied")
	}
}
