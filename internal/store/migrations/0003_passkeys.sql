-- WebAuthn passkeys: an additional sign-in method alongside the login
-- password. A user may hold several (one per device/browser they've
-- registered from), each independently deletable from Settings.
CREATE TABLE passkeys (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The WebAuthn credential ID, as the authenticator assigned it.
    -- Globally unique by construction (not just per-user), since a
    -- discoverable/usernameless login looks a credential up by this
    -- alone, before it knows which user it belongs to.
    credential_id BYTEA NOT NULL UNIQUE,
    -- The go-webauthn library's own Credential struct, JSON-marshaled
    -- whole (public key, attestation, authenticator counters/flags, ...)
    -- rather than split into columns, that struct is what every
    -- verification call needs back, byte for byte, and mapping every one
    -- of its fields by hand would just be a second, drifting copy of the
    -- library's own definition.
    data          JSONB NOT NULL,
    -- User-facing label so Settings can list "which one is this",
    -- captured from the browser's User-Agent at registration time (the
    -- user never has to type anything just to add one).
    name          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ
);

CREATE INDEX passkeys_user_id_idx ON passkeys (user_id);

ALTER TABLE users
    -- Whether this user permanently dismissed the "add a passkey?" prompt
    -- (the checkbox), see login_count/passkey_prompt_next_at below for
    -- the rest of that logic.
    ADD COLUMN passkey_prompt_disabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- Successful logins, by any method, counted purely to drive the
    -- prompt's "ask again after 10 more logins" re-ask, not a general
    -- usage metric.
    ADD COLUMN login_count INT NOT NULL DEFAULT 0,
    -- The login_count value at which the prompt is next due. Starts at 1
    -- so a brand new account is prompted on its very first login; a
    -- "Skip" (without the checkbox) sets this to login_count + 10 at the
    -- moment of skipping.
    ADD COLUMN passkey_prompt_next_at INT NOT NULL DEFAULT 1;
