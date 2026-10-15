//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
)

func TestPasskeyCRUDAndOwnership(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u1, err := s.CreateUser(ctx, "user1", "hashed-password", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	u2, err := s.CreateUser(ctx, "user2", "hashed-password", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	if n, err := s.CountPasskeysByUser(ctx, u1.ID); err != nil || n != 0 {
		t.Fatalf("CountPasskeysByUser() = %d, %v, want 0, nil", n, err)
	}

	p, err := s.CreatePasskey(ctx, u1.ID, []byte("cred-1"), []byte(`{"id":"cred-1"}`), "My laptop")
	if err != nil {
		t.Fatalf("CreatePasskey() error = %v", err)
	}

	if n, err := s.CountPasskeysByUser(ctx, u1.ID); err != nil || n != 1 {
		t.Fatalf("CountPasskeysByUser() after create = %d, %v, want 1, nil", n, err)
	}

	// Credential IDs are globally unique - re-registering the same one
	// (even under a different user) must fail, not silently overwrite.
	if _, err := s.CreatePasskey(ctx, u2.ID, []byte("cred-1"), []byte(`{}`), "duplicate"); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreatePasskey() with a duplicate credential id error = %v, want ErrConflict", err)
	}

	byList, err := s.ListPasskeysByUser(ctx, u1.ID)
	if err != nil {
		t.Fatalf("ListPasskeysByUser() error = %v", err)
	}
	if len(byList) != 1 || byList[0].ID != p.ID {
		t.Fatalf("ListPasskeysByUser() = %+v, want just %+v", byList, p)
	}

	byCred, err := s.GetPasskeyByCredentialID(ctx, []byte("cred-1"))
	if err != nil {
		t.Fatalf("GetPasskeyByCredentialID() error = %v", err)
	}
	if byCred.ID != p.ID {
		t.Fatalf("GetPasskeyByCredentialID() ID = %d, want %d", byCred.ID, p.ID)
	}

	if err := s.UpdatePasskeyUsage(ctx, p.ID, []byte(`{"id":"cred-1","signCount":1}`)); err != nil {
		t.Fatalf("UpdatePasskeyUsage() error = %v", err)
	}

	updated, err := s.GetPasskeyByCredentialID(ctx, []byte("cred-1"))
	if err != nil {
		t.Fatalf("GetPasskeyByCredentialID() after update error = %v", err)
	}
	if updated.LastUsedAt == nil {
		t.Fatal("UpdatePasskeyUsage() did not stamp last_used_at")
	}

	// A user can't delete someone else's passkey by guessing its id.
	if err := s.DeletePasskey(ctx, u2.ID, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeletePasskey() by the wrong owner error = %v, want ErrNotFound", err)
	}

	if err := s.DeletePasskey(ctx, u1.ID, p.ID); err != nil {
		t.Fatalf("DeletePasskey() by the real owner error = %v", err)
	}

	if n, err := s.CountPasskeysByUser(ctx, u1.ID); err != nil || n != 0 {
		t.Fatalf("CountPasskeysByUser() after delete = %d, %v, want 0, nil", n, err)
	}
}

func TestPasskeyPromptBookkeeping(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "user1", "hashed-password", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	// passkey_prompt_next_at starts at 1, so the very first login is due.
	due, err := s.IncrementLoginCount(ctx, u.UUID)
	if err != nil {
		t.Fatalf("IncrementLoginCount() error = %v", err)
	}
	if !due {
		t.Fatal("IncrementLoginCount() on the first login = false, want true")
	}

	// Skip without the checkbox: the next 9 logins must NOT re-prompt,
	// the 10th (login_count 1 + 10 = 11) must.
	if err := s.SkipPasskeyPrompt(ctx, u.UUID, false); err != nil {
		t.Fatalf("SkipPasskeyPrompt() error = %v", err)
	}

	for i := 2; i <= 10; i++ {
		due, err := s.IncrementLoginCount(ctx, u.UUID)
		if err != nil {
			t.Fatalf("IncrementLoginCount() login %d error = %v", i, err)
		}
		if due {
			t.Fatalf("IncrementLoginCount() login %d = true, want false (prompt deferred to login 11)", i)
		}
	}

	due, err = s.IncrementLoginCount(ctx, u.UUID)
	if err != nil {
		t.Fatalf("IncrementLoginCount() login 11 error = %v", err)
	}
	if !due {
		t.Fatal("IncrementLoginCount() login 11 = false, want true (the re-ask was due)")
	}

	// "don't show again" must permanently disable it, surviving any number
	// of further logins.
	if err := s.SkipPasskeyPrompt(ctx, u.UUID, true); err != nil {
		t.Fatalf("SkipPasskeyPrompt(dontShowAgain) error = %v", err)
	}

	for i := 0; i < 15; i++ {
		due, err := s.IncrementLoginCount(ctx, u.UUID)
		if err != nil {
			t.Fatalf("IncrementLoginCount() post-disable error = %v", err)
		}
		if due {
			t.Fatal("IncrementLoginCount() fired again after a permanent dismissal")
		}
	}
}
