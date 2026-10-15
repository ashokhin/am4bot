package store

import (
	"context"
	"fmt"
	"time"
)

// Passkey is one WebAuthn credential a user has registered. Data holds the
// go-webauthn library's own Credential struct (see CredentialJSON/SetCredential),
// stored as opaque JSON rather than mapped column by column.
type Passkey struct {
	ID           int64      `db:"id"`
	UserID       int64      `db:"user_id"`
	CredentialID []byte     `db:"credential_id"`
	Data         []byte     `db:"data"`
	Name         string     `db:"name"`
	CreatedAt    time.Time  `db:"created_at"`
	LastUsedAt   *time.Time `db:"last_used_at"`
}

// CreatePasskey stores a newly-registered credential. credentialID and
// data are the caller's webauthn.Credential, split into its ID (for the
// unique/lookup column) and its full JSON encoding (everything
// verification needs back later), see Passkey.Data's own doc comment on
// why the whole struct is kept together rather than split into columns.
func (s *Store) CreatePasskey(ctx context.Context, userID int64, credentialID, data []byte, name string) (*Passkey, error) {
	var p Passkey

	err := s.db.GetContext(ctx, &p, `
		INSERT INTO passkeys (user_id, credential_id, data, name)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, credential_id, data, name, created_at, last_used_at
	`, userID, credentialID, data, name)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: this credential is already registered", ErrConflict)
		}

		return nil, fmt.Errorf("creating passkey: %w", err)
	}

	return &p, nil
}

// ListPasskeysByUser returns a user's own passkeys, oldest first, for
// the Settings page's "manage your passkeys" list.
func (s *Store) ListPasskeysByUser(ctx context.Context, userID int64) ([]Passkey, error) {
	var passkeys []Passkey

	if err := s.db.SelectContext(ctx, &passkeys, `
		SELECT id, user_id, credential_id, data, name, created_at, last_used_at
		FROM passkeys WHERE user_id = $1 ORDER BY created_at
	`, userID); err != nil {
		return nil, fmt.Errorf("listing passkeys: %w", err)
	}

	return passkeys, nil
}

// CountPasskeysByUser is a lighter-weight check than ListPasskeysByUser for
// "does this user have at least one passkey", which is all the
// add-a-passkey prompt and the discoverable-login button need to know.
func (s *Store) CountPasskeysByUser(ctx context.Context, userID int64) (int, error) {
	var n int

	if err := s.db.GetContext(ctx, &n, `SELECT count(*) FROM passkeys WHERE user_id = $1`, userID); err != nil {
		return 0, fmt.Errorf("counting passkeys: %w", err)
	}

	return n, nil
}

// GetPasskeyByCredentialID looks a credential up by the ID the
// authenticator itself reports, the only thing a discoverable/
// usernameless login has to go on before it knows which user is signing
// in. credential_id is globally unique (see the migration's own comment),
// so this never needs a user_id to disambiguate.
func (s *Store) GetPasskeyByCredentialID(ctx context.Context, credentialID []byte) (*Passkey, error) {
	var p Passkey

	if err := s.db.GetContext(ctx, &p, `
		SELECT id, user_id, credential_id, data, name, created_at, last_used_at
		FROM passkeys WHERE credential_id = $1
	`, credentialID); err != nil {
		return nil, wrapNotFound(err, "passkey")
	}

	return &p, nil
}

// UpdatePasskeyUsage persists the authenticator's post-ceremony state
// (notably its signature counter, which the library bumps on every
// successful login to detect a cloned authenticator) and the fact that it
// was just used. Called after every successful passkey login, never after
// registration (there's nothing to update yet).
func (s *Store) UpdatePasskeyUsage(ctx context.Context, id int64, data []byte) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE passkeys SET data = $2, last_used_at = now() WHERE id = $1
	`, id, data); err != nil {
		return fmt.Errorf("updating passkey usage: %w", err)
	}

	return nil
}

// DeletePasskey removes one of userID's OWN passkeys, scoped by both id
// and user_id in one statement so a user can never delete someone else's
// by guessing an id, with no separate ownership check needed first.
// Returns ErrNotFound if id doesn't exist or isn't userID's.
func (s *Store) DeletePasskey(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("deleting passkey: %w", err)
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}

	return nil
}
