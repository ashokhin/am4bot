package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned by Get* methods when no matching row exists.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned when a write would violate a uniqueness
// constraint (duplicate login, ...).
var ErrConflict = errors.New("store: conflict")

// userColumns is every users column except password_hash's raw SQL
// fragment doesn't change per query, so it's centralized here -- every
// Get*/List below must return exactly this shape into a User.
const userColumns = `
	id, uuid, login, password_hash, is_admin, vpn_region_id,
	display_name, last_login_at, last_login_ip, last_login_user_agent,
	last_failed_login_at, last_failed_login_ip, last_failed_login_user_agent,
	created_at, disabled_at, must_change_password
`

// User is a control-plane account: one of the admin's friends, or the
// admin themself (IsAdmin true). PasswordHash is a bcrypt hash of this
// user's OWN login password -- unrelated to any game account password,
// which lives on Node instead. Login is just an identifier the admin
// picks when creating the account (there is no signup flow, so it never
// needed to be a real email address -- see migrations/0005's doc comment).
type User struct {
	ID           int64     `db:"id"`
	UUID         uuid.UUID `db:"uuid"`
	Login        string    `db:"login"`
	PasswordHash string    `db:"password_hash"`
	IsAdmin      bool      `db:"is_admin"`
	// VPNRegionID picks which vpn_regions catalog entry ALL of this user's
	// nodes exit VPN traffic
	// through -- a single per-user choice, not a per-node one. Nil means no
	// VPN (direct connection). See vpn_regions.go's doc comment for the
	// full model: one admin-configured VPN provider account, shared by
	// every user, with only the exit region selectable per user. Always
	// nil for an admin account -- see requireNonAdminUser's doc comment.
	VPNRegionID *int64 `db:"vpn_region_id"`
	// DisplayName is a purely cosmetic, self-editable label -- nil means
	// "show Login instead", not "unset". Never used for sign-in or
	// uniqueness, unlike Login.
	DisplayName *string `db:"display_name"`
	// LastLoginAt/LastLoginIP/LastLoginUserAgent are set together on every
	// successful password login (see SetUserLastLogin) -- nil LastLoginAt
	// means the account has never signed in.
	LastLoginAt        *time.Time `db:"last_login_at"`
	LastLoginIP        *string    `db:"last_login_ip"`
	LastLoginUserAgent *string    `db:"last_login_user_agent"`
	// LastFailedLoginAt/IP/UserAgent are set together on every failed
	// password attempt against this login (see SetUserLastFailedLogin) --
	// shown on the admin user detail page alongside the ban status from
	// LoginGuard, so an admin can see "is someone currently trying to get
	// into this account" independent of whether it's currently banned.
	LastFailedLoginAt        *time.Time `db:"last_failed_login_at"`
	LastFailedLoginIP        *string    `db:"last_failed_login_ip"`
	LastFailedLoginUserAgent *string    `db:"last_failed_login_user_agent"`
	CreatedAt                time.Time  `db:"created_at"`
	DisabledAt               *time.Time `db:"disabled_at"`
	// MustChangePassword is true whenever the current password_hash was
	// set FOR this user (account creation, an admin's reset) rather than
	// chosen BY them -- the frontend forces a change-password screen
	// while it's true. See SetUserPasswordHash and migrations/0002's doc
	// comment.
	MustChangePassword bool `db:"must_change_password"`
}

// CreateUser inserts a new user and returns the full row (including the
// generated id/uuid/created_at). passwordHash must already be hashed
// (see internal/auth) -- store never hashes or validates passwords itself.
// Always starts with must_change_password = true: the admin picked this
// password, not the user themselves (there's no signup flow).
func (s *Store) CreateUser(ctx context.Context, login, passwordHash string, isAdmin bool) (*User, error) {
	var u User

	err := s.db.GetContext(ctx, &u, `
		INSERT INTO users (login, password_hash, is_admin, must_change_password)
		VALUES ($1, $2, $3, TRUE)
		RETURNING `+userColumns, login, passwordHash, isAdmin)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: login %q already exists", ErrConflict, login)
		}

		return nil, fmt.Errorf("creating user: %w", err)
	}

	return &u, nil
}

// GetUserByLogin looks up a user for login. Returns ErrNotFound if no
// account has that login, including disabled ones -- callers that need to
// distinguish "no such account" from "disabled" should check DisabledAt.
func (s *Store) GetUserByLogin(ctx context.Context, login string) (*User, error) {
	var u User

	err := s.db.GetContext(ctx, &u, `SELECT `+userColumns+` FROM users WHERE login = $1`, login)
	if err != nil {
		return nil, wrapNotFound(err, "user")
	}

	return &u, nil
}

// GetUserByUUID looks up a user by their external-facing identifier (the
// one used in URLs and Prometheus labels -- see migrations/0001_init.sql).
// GetUserByID looks up a user by their internal serial id -- for
// resolving a node's owning user (nodes.user_id) internally, never for
// anything user-facing (use GetUserByUUID there instead).
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	var u User

	err := s.db.GetContext(ctx, &u, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	if err != nil {
		return nil, wrapNotFound(err, "user")
	}

	return &u, nil
}

func (s *Store) GetUserByUUID(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User

	err := s.db.GetContext(ctx, &u, `SELECT `+userColumns+` FROM users WHERE uuid = $1`, id)
	if err != nil {
		return nil, wrapNotFound(err, "user")
	}

	return &u, nil
}

// ListUsers returns every user, most recently created first. For the admin
// panel's user list -- there is no per-user "list other users" use case.
// CountUsers returns how many accounts exist -- cheaper than ListUsers
// when the caller only needs to know "is there anyone at all yet"
// (see cmd/apiserver's bootstrap-admin startup check).
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int

	if err := s.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM users`); err != nil {
		return 0, fmt.Errorf("counting users: %w", err)
	}

	return count, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	var users []User

	if err := s.db.SelectContext(ctx, &users, `
		SELECT `+userColumns+` FROM users ORDER BY created_at DESC
	`); err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}

	return users, nil
}

// UserWithNodeStats is one row of ListUsersWithNodeStats -- the admin
// user list's "active/total nodes" columns, computed here rather than by
// the caller issuing a second query per user.
type UserWithNodeStats struct {
	User
	ActiveNodes int `db:"active_nodes"`
	TotalNodes  int `db:"total_nodes"`
}

// ListUsersWithNodeStats is ListUsers plus each user's node counts, for
// the admin user list.
func (s *Store) ListUsersWithNodeStats(ctx context.Context) ([]UserWithNodeStats, error) {
	var users []UserWithNodeStats

	if err := s.db.SelectContext(ctx, &users, `
		SELECT `+userColumnsPrefixed+`,
			COUNT(n.id) FILTER (WHERE n.enabled) AS active_nodes,
			COUNT(n.id) AS total_nodes
		FROM users u
		LEFT JOIN nodes n ON n.user_id = u.id
		GROUP BY u.id
		ORDER BY u.created_at DESC
	`); err != nil {
		return nil, fmt.Errorf("listing users with node stats: %w", err)
	}

	return users, nil
}

// userColumnsPrefixed is userColumns with an explicit "u." table
// qualifier -- for queries (like ListUsersWithNodeStats) that join users
// against another table also having an "id" column.
const userColumnsPrefixed = `
	u.id, u.uuid, u.login, u.password_hash, u.is_admin, u.vpn_region_id,
	u.display_name, u.last_login_at, u.last_login_ip, u.last_login_user_agent,
	u.last_failed_login_at, u.last_failed_login_ip, u.last_failed_login_user_agent,
	u.created_at, u.disabled_at, u.must_change_password
`

// SetUserDisabled sets or clears a user's disabled_at timestamp. Disabling
// a user is a separate, reversible step from deleting.
//
// Refusing to let an admin disable their OWN account is deliberately NOT
// enforced here -- that's a caller-identity concern the store layer has no
// way to check (it only ever sees the target uuid), so it belongs in
// internal/api/admin_handlers.go's handler, which has both.
func (s *Store) SetUserDisabled(ctx context.Context, id uuid.UUID, disabled bool) error {
	var disabledAt any
	if disabled {
		disabledAt = time.Now().UTC()
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET disabled_at = $2 WHERE uuid = $1`, id, disabledAt,
	)
	if err != nil {
		return fmt.Errorf("updating user disabled state: %w", err)
	}

	return checkRowsAffected(res, "user")
}

// DeleteUser removes a user row outright -- their nodes cascade-delete
// with it (nodes.user_id REFERENCES users(id) ON DELETE CASCADE, see
// migrations/0001_init.sql). The caller (admin_handlers.go) is
// responsible for enqueueing orchestrator teardown for each of the
// user's provisioned nodes BEFORE calling this, the same way
// handleDeleteNode does for one node -- this only removes the database
// rows, it has no way to reach the orchestrator itself.
func (s *Store) DeleteUser(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE uuid = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}

	return checkRowsAffected(res, "user")
}

// SetUserPasswordHash replaces a user's login password hash -- used both
// for admin-initiated resets (by uuid) and self-service changes.
// mustChangePassword should be true for an admin-initiated reset (the
// user didn't pick this password, so they're forced to change it before
// doing anything else -- see migrations/0002's doc comment) and false for
// a self-service change (they just proved they know the current password
// and picked this new one themselves).
func (s *Store) SetUserPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string, mustChangePassword bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = $2, must_change_password = $3 WHERE uuid = $1`,
		id, passwordHash, mustChangePassword)
	if err != nil {
		return fmt.Errorf("setting user password: %w", err)
	}

	return checkRowsAffected(res, "user")
}

// SetUserDisplayName sets or clears (name == nil) a user's own display
// name. Self-service, scoped by the user's own id.
func (s *Store) SetUserDisplayName(ctx context.Context, userID int64, name *string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET display_name = $2 WHERE id = $1`, userID, name)
	if err != nil {
		return fmt.Errorf("setting user display name: %w", err)
	}

	return checkRowsAffected(res, "user")
}

// SetUserLastLogin stamps a user's last_login_at/ip/user_agent -- called
// once per successful password login (handleLogin). Best-effort from the
// caller's point of view: a failure here shouldn't fail the login itself.
func (s *Store) SetUserLastLogin(ctx context.Context, id uuid.UUID, ip, userAgent string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = now(), last_login_ip = $2, last_login_user_agent = $3 WHERE uuid = $1`,
		id, ip, userAgent)
	if err != nil {
		return fmt.Errorf("setting user last login: %w", err)
	}

	return checkRowsAffected(res, "user")
}

// SetUserLastFailedLogin stamps last_failed_login_at/ip/user_agent for the
// user with this login -- called on every failed password attempt
// (handleLogin), including attempts against a login that turns out not to
// exist, in which case this is simply a no-op (0 rows affected is not
// treated as an error here, unlike SetUserLastLogin, since the caller
// can't know in advance whether the login is real).
func (s *Store) SetUserLastFailedLogin(ctx context.Context, login, ip, userAgent string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET last_failed_login_at = now(), last_failed_login_ip = $2, last_failed_login_user_agent = $3
		WHERE login = $1
	`, login, ip, userAgent); err != nil {
		return fmt.Errorf("setting user last failed login: %w", err)
	}

	return nil
}

// SetUserVPNRegion sets or clears (regionID == nil) the vpn_regions entry a
// user's nodes exit VPN traffic through. Self-service -- called from the
// authenticated user's own settings, not an admin action -- so it's scoped
// by the user's own id, not a target uuid the way SetUserDisabled is.
func (s *Store) SetUserVPNRegion(ctx context.Context, userID int64, regionID *int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET vpn_region_id = $2 WHERE id = $1`, userID, regionID)
	if err != nil {
		return fmt.Errorf("setting user vpn region: %w", err)
	}

	return checkRowsAffected(res, "user")
}

// isUniqueViolation reports whether err is a Postgres unique_violation
// (SQLSTATE 23505), regardless of which constraint/index triggered it.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
