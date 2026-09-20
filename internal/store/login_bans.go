package store

import (
	"context"
	"fmt"
	"time"
)

// LoginBan is one active brute-force ban -- see internal/api/login_guard.go
// for the counting/threshold logic that creates these; this file only
// covers reading/writing the row itself.
type LoginBan struct {
	KeyType  string    `db:"key_type"`
	Key      string    `db:"key"`
	Reason   string    `db:"reason"`
	BannedAt time.Time `db:"banned_at"`
	UnbanAt  time.Time `db:"unban_at"`
}

// UpsertLoginBan creates or refreshes a ban for (keyType, key), active
// until unbanAt. ON CONFLICT so a key that's already banned (e.g. it kept
// failing after the ban's threshold-crossing attempt, which shouldn't
// normally happen once LoginGuard.Check starts refusing it, but a race
// between two concurrent requests is possible) just gets its ban extended
// rather than erroring.
func (s *Store) UpsertLoginBan(ctx context.Context, keyType, key, reason string, unbanAt time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO login_bans (key_type, key, reason, unban_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key_type, key) DO UPDATE
			SET reason = EXCLUDED.reason, banned_at = now(), unban_at = EXCLUDED.unban_at
	`, keyType, key, reason, unbanAt); err != nil {
		return fmt.Errorf("upserting login ban: %w", err)
	}

	return nil
}

// GetActiveLoginBan returns the ban for (keyType, key) if one exists and
// hasn't expired yet. Returns ErrNotFound if there's no row, or the row is
// there but already past unban_at (an expired ban that just hasn't been
// cleaned up yet reads the same as no ban at all).
func (s *Store) GetActiveLoginBan(ctx context.Context, keyType, key string) (*LoginBan, error) {
	var b LoginBan

	err := s.db.GetContext(ctx, &b, `
		SELECT key_type, key, reason, banned_at, unban_at
		FROM login_bans
		WHERE key_type = $1 AND key = $2 AND unban_at > now()
	`, keyType, key)
	if err != nil {
		return nil, wrapNotFound(err, "login ban")
	}

	return &b, nil
}

// DeleteLoginBan removes a ban outright -- used both for the admin
// "unlock" action and to clean up an expired row the next time it's
// touched. Deleting a ban that doesn't exist is not an error (idempotent):
// admin unlock and expiry cleanup both want "make sure it's gone", not
// "assert it was there".
func (s *Store) DeleteLoginBan(ctx context.Context, keyType, key string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM login_bans WHERE key_type = $1 AND key = $2`, keyType, key,
	); err != nil {
		return fmt.Errorf("deleting login ban: %w", err)
	}

	return nil
}

// LoginAttempt is one row of the append-only login_attempts audit log --
// see migrations/0002's doc comment.
type LoginAttempt struct {
	ID        int64     `db:"id"`
	Login     string    `db:"login"`
	Success   bool      `db:"success"`
	IP        string    `db:"ip"`
	UserAgent string    `db:"user_agent"`
	CreatedAt time.Time `db:"created_at"`
}

// RecordLoginAttempt appends one row to login_attempts -- called for
// EVERY login attempt (success or failure), including ones against a
// login that doesn't correspond to any account.
func (s *Store) RecordLoginAttempt(ctx context.Context, login string, success bool, ip, userAgent string) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO login_attempts (login, success, ip, user_agent)
		VALUES ($1, $2, $3, $4)
	`, login, success, ip, userAgent); err != nil {
		return fmt.Errorf("recording login attempt: %w", err)
	}

	return nil
}

// ListLoginAttemptsByLogin returns the most recent attempts against login
// (success and failure both), newest first, capped at limit -- for the
// admin user detail page's activity history.
func (s *Store) ListLoginAttemptsByLogin(ctx context.Context, login string, limit int) ([]LoginAttempt, error) {
	var attempts []LoginAttempt

	if err := s.db.SelectContext(ctx, &attempts, `
		SELECT id, login, success, ip, user_agent, created_at
		FROM login_attempts
		WHERE login = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, login, limit); err != nil {
		return nil, fmt.Errorf("listing login attempts: %w", err)
	}

	return attempts, nil
}

// LoginBanEvent is one row of the append-only login_ban_events audit log
// -- see migrations/0002's doc comment. Unlike LoginBan, a row here is
// never deleted: it's the permanent record that a ban happened, even
// after the ban itself expires or is lifted.
type LoginBanEvent struct {
	ID              int64      `db:"id"`
	KeyType         string     `db:"key_type"`
	Key             string     `db:"key"`
	Reason          string     `db:"reason"`
	BannedAt        time.Time  `db:"banned_at"`
	UnbanAt         time.Time  `db:"unban_at"`
	UnlockedAt      *time.Time `db:"unlocked_at"`
	UnlockedByLogin *string    `db:"unlocked_by_login"`
	CreatedAt       time.Time  `db:"created_at"`
}

// RecordLoginBanEvent appends a row recording that a ban was just created
// -- called alongside UpsertLoginBan, never on its own.
func (s *Store) RecordLoginBanEvent(ctx context.Context, keyType, key, reason string, bannedAt, unbanAt time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO login_ban_events (key_type, key, reason, banned_at, unban_at)
		VALUES ($1, $2, $3, $4, $5)
	`, keyType, key, reason, bannedAt, unbanAt); err != nil {
		return fmt.Errorf("recording login ban event: %w", err)
	}

	return nil
}

// MarkLoginBanEventUnlocked stamps unlocked_at/unlocked_by_login on the
// most recent still-open ban event for (keyType, key) -- called when an
// admin manually unlocks an account before its ban would have expired
// naturally. A ban that simply expires on its own never gets this call,
// so unlocked_at staying NULL means exactly "ran out the clock", not
// "nobody ever checked".
func (s *Store) MarkLoginBanEventUnlocked(ctx context.Context, keyType, key, unlockedByLogin string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE login_ban_events SET unlocked_at = now(), unlocked_by_login = $3
		WHERE id = (
			SELECT id FROM login_ban_events
			WHERE key_type = $1 AND key = $2 AND unlocked_at IS NULL
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, keyType, key, unlockedByLogin); err != nil {
		return fmt.Errorf("marking login ban event unlocked: %w", err)
	}

	return nil
}

// ListLoginBanEventsByKey returns the ban history for (keyType, key),
// newest first, capped at limit -- for the admin user detail page's "when
// and why was this account locked" history.
func (s *Store) ListLoginBanEventsByKey(ctx context.Context, keyType, key string, limit int) ([]LoginBanEvent, error) {
	var events []LoginBanEvent

	if err := s.db.SelectContext(ctx, &events, `
		SELECT id, key_type, key, reason, banned_at, unban_at, unlocked_at, unlocked_by_login, created_at
		FROM login_ban_events
		WHERE key_type = $1 AND key = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, keyType, key, limit); err != nil {
		return nil, fmt.Errorf("listing login ban events: %w", err)
	}

	return events, nil
}
