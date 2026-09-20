-- Login hardening: brute-force bans and per-user login activity, backing
-- LoginGuard (internal/api/login_guard.go) and the admin "last activity"/
-- unlock UI. Previously drafted as a JSON file on disk; moved into
-- Postgres instead so it's queryable per-user, atomically updated, and
-- doesn't need its own volume mount to survive a container restart.

-- One row per currently-banned key. A key is either an IP address
-- (key_type='ip') or a login string (key_type='login') -- see
-- LoginGuard's doc comment on why both are tracked independently. Expired
-- rows are deleted rather than kept around, so this table only ever holds
-- active bans.
CREATE TABLE login_bans (
    key_type   TEXT NOT NULL CHECK (key_type IN ('ip', 'login')),
    key        TEXT NOT NULL,
    reason     TEXT NOT NULL,
    banned_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    unban_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (key_type, key)
);

-- Per-user login activity, shown on the admin user detail page. Successful
-- and failed attempts are tracked separately since they answer different
-- questions ("when did they last actually get in" vs "is someone
-- currently trying to guess this account's password"). These two are a
-- denormalized "most recent" summary for a quick glance; the full history
-- of every attempt lives in login_attempts below.
ALTER TABLE users
    ADD COLUMN last_login_ip         TEXT,
    ADD COLUMN last_login_user_agent TEXT,
    ADD COLUMN last_failed_login_at         TIMESTAMPTZ,
    ADD COLUMN last_failed_login_ip         TEXT,
    ADD COLUMN last_failed_login_user_agent TEXT;

-- Append-only log of every login attempt, successful or not, against any
-- login string (including ones that don't correspond to a real account --
-- that's itself useful signal). Grows without bound; nothing prunes it
-- yet, acceptable at this project's scale (a handful of users) -- revisit
-- with a retention policy before this matters.
CREATE TABLE login_attempts (
    id         BIGSERIAL PRIMARY KEY,
    login      TEXT NOT NULL,
    success    BOOLEAN NOT NULL,
    ip         TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX login_attempts_login_idx ON login_attempts (login, created_at DESC);

-- Append-only history of every ban LoginGuard has created, independent of
-- login_bans above (which only holds CURRENTLY active bans and loses the
-- row once one expires or an admin unlocks it). unlocked_at/
-- unlocked_by_login are set only when an admin manually unlocks it before
-- its natural unban_at -- both stay NULL for a ban that simply expired on
-- its own. unlocked_by_login is a snapshot string, not a foreign key: this
-- is an audit trail, and should keep reading correctly even if that admin
-- account is later deleted.
CREATE TABLE login_ban_events (
    id                 BIGSERIAL PRIMARY KEY,
    key_type           TEXT NOT NULL CHECK (key_type IN ('ip', 'login')),
    key                TEXT NOT NULL,
    reason             TEXT NOT NULL,
    banned_at          TIMESTAMPTZ NOT NULL,
    unban_at           TIMESTAMPTZ NOT NULL,
    unlocked_at        TIMESTAMPTZ,
    unlocked_by_login  TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX login_ban_events_key_idx ON login_ban_events (key_type, key, created_at DESC);
