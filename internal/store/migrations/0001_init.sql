-- Initial schema for the multi-tenant control plane.
--
-- Design notes:
--   * Secrets (game account passwords, VPN credentials, per-node config
--     tokens) are stored as AES-256-GCM ciphertext (see internal/secrets)
--     in *_enc columns -- opaque bytes to Postgres, decrypted only in the
--     API server process that holds the master key. This is deliberately
--     different from users.password_hash (bcrypt, one-way): a game
--     password/VPN credential/config token must be recoverable to hand to
--     a container or compare against later, a login password never does.
--   * "Frequently user-edited" node settings (credentials, schedule,
--     jitter, enabled services, timezone) get dedicated typed columns,
--     since the UI reads/writes them directly and they map straight onto
--     ambot's config.CronSchedules/Services/etc.
--   * Everything else the bot's Config struct supports (budget_percent,
--     good_price, aircraft_* thresholds, catering options, ...) lives in
--     extra_config JSONB instead of one column per field. Config gains
--     fields over time (see internal/config/config.go); a JSONB blob lets
--     the UI/API add support for a new one without a migration each time.
--     The API server validates it by unmarshalling into config.Config
--     before rendering a node's config.yaml, so a bad value is still
--     rejected -- just not at the schema level.
--   * services and cron_schedules are TEXT[] (arrays), not JSON -- Postgres
--     arrays are ordered, and neither pgx/lib/pq array scanning into a Go
--     []string nor yaml.v3 marshalling that slice re-sorts it. Execution
--     order of services is meaningful to the user (e.g. "buy fuel and
--     marketing before departing"), so never introduce a query that
--     reorders them (e.g. "SELECT unnest(services) ... ORDER BY ..." for
--     anything other than display) -- always read/write the array as a
--     whole, in the order the UI's drag-and-drop last left it in.
--   * users.uuid is what appears in HAProxy-routed UI URLs and the
--     Prometheus user_uuid label -- never users.id, which is a guessable
--     sequential integer. The label gets attached without touching ambot
--     itself: the orchestrator maintains a Prometheus file_sd targets
--     file, one entry per node, each with labels: {user_uuid: ...,
--     node_id: ...} alongside its scrape target -- Prometheus does the
--     labelling at scrape time, ambot stays unaware of which tenant it
--     belongs to.
--   * users.login (not "email"): there is no signup/registration flow and
--     never will be -- the admin creates every account by hand -- so this
--     is just a login identifier the admin picks, not a real email
--     address.
--   * A node's timezone (not a user's) interprets that node's
--     cron_schedules -- the user sets it themselves while configuring the
--     node's schedule, one zone per node, applied to every cron_schedules
--     entry on it.
--   * VPN model: exactly ONE provider account, admin-configured once
--     (vpn_provider_credentials, a singleton row), never per-user/per-node.
--     The admin also curates a catalog of exit regions (vpn_regions, an
--     .ovpn file per region under that same account). Each user picks ONE
--     region for their whole account (users.vpn_region_id), applied to
--     every one of their nodes uniformly -- never a per-node choice. See
--     internal/store/vpn_regions.go's doc comment for the full rationale.
--   * Every user gets TWO default nodes up front ("player" and
--     "maintenance"), both created disabled -- undeletable (is_default),
--     only disable-able (see DeleteNode). A node can't be persisted with
--     enabled=true unless it has real game credentials, a schedule, and a
--     timezone (see nodeReadyToEnable in internal/store/nodes.go).
--   * Multi-host from day one, in the schema only: nodes.target_host picks
--     which host a node's containers run on, even though everything runs
--     on one machine today. orchestrator currently only ever talks to its
--     own local Docker socket; the field is ready for a real second host
--     later.
--   * Coordination between apiserver and the separate orchestrator service
--     is entirely through the node_operations job queue apiserver writes
--     to and orchestrator polls -- never a direct call between the two
--     processes, a deliberate security boundary (apiserver must never
--     have Docker-socket or shell access).

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid() on Postgres < 15

-- Admin-curated catalog of VPN exit regions, under the one shared provider
-- account (vpn_provider_credentials below). Referenced by users.vpn_region_id.
CREATE TABLE vpn_regions (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE, -- shown to users in the region picker
    ovpn_config_enc TEXT NOT NULL,        -- this region's .ovpn file, encrypted
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Singleton row (id always 1) holding the one shared VPN account every
-- region above connects through.
CREATE TABLE vpn_provider_credentials (
    id               INT PRIMARY KEY CHECK (id = 1),
    provider         TEXT NOT NULL DEFAULT 'custom',
    vpn_username_enc TEXT NOT NULL,
    vpn_password_enc TEXT NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Singleton row (id always 1) holding the Prometheus instance apiserver's
-- metrics endpoint proxies queries to -- admin-configured in the UI and
-- stored here, not a startup flag/env var, so it can be set/changed
-- without a redeploy. No secret here (a URL, not a credential), so unlike
-- vpn_provider_credentials this is plaintext, not AES-encrypted.
CREATE TABLE prometheus_settings (
    id         INT PRIMARY KEY CHECK (id = 1),
    url        TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY, -- internal FK target only; never expose in URLs or labels
    -- external-facing identity: HAProxy-routed UI URLs (/u/<uuid>/...) and
    -- the user_uuid Prometheus label (see above) both use this, not id --
    -- an incrementing integer would let one user enumerate/guess others'.
    uuid          UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    -- login identifier the admin picks when creating the account -- see
    -- the module doc comment on why this isn't "email".
    login         TEXT NOT NULL UNIQUE,
    -- bcrypt hash of this user's OWN login password (not a game account
    -- password) -- how they authenticate to this control plane.
    password_hash TEXT NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    -- purely cosmetic, self-editable label; NULL means "show login
    -- instead". Never used for sign-in.
    display_name  TEXT,
    -- which vpn_regions catalog entry this user's nodes exit VPN traffic
    -- through -- a single per-user choice, not per-node. NULL means no VPN.
    vpn_region_id BIGINT REFERENCES vpn_regions(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at   TIMESTAMPTZ,
    last_login_at TIMESTAMPTZ,
    -- True whenever the current password_hash was set FOR this user
    -- (account creation, an admin's reset) rather than chosen BY them --
    -- the frontend forces a change-password screen while this is true,
    -- since there's no signup flow and every account starts on an
    -- admin-picked password the user never chose themselves. Defaults to
    -- FALSE at the column level; CreateUser and the admin's
    -- reset-password endpoint explicitly set it TRUE, and a self-service
    -- password change explicitly clears it back to FALSE (see
    -- internal/store/users.go's SetUserPasswordHash).
    must_change_password BOOLEAN NOT NULL DEFAULT FALSE
);

-- One row per running ambot container ("node" in the product's language).
CREATE TABLE nodes (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                TEXT NOT NULL, -- e.g. "player", "maintenance"

    game_url            TEXT NOT NULL DEFAULT 'https://www.airlinemanager.com/',
    game_username       TEXT NOT NULL,
    game_password_enc   TEXT NOT NULL,

    -- maps onto config.Config.Services
    services            TEXT[] NOT NULL DEFAULT '{}',
    -- maps onto config.Config.CronSchedules
    cron_schedules      TEXT[] NOT NULL DEFAULT '{}',
    cron_jitter_seconds INT NOT NULL DEFAULT 0,
    timeout_seconds     INT NOT NULL DEFAULT 180,
    -- IANA zone name interpreting every one of this node's cron_schedules
    -- entries -- one zone for the whole node, chosen by the user alongside
    -- the schedule.
    timezone            TEXT NOT NULL DEFAULT 'UTC',

    -- everything else Config supports; merged on top of defaults when
    -- rendering config.yaml. See the module doc comment above.
    extra_config        JSONB NOT NULL DEFAULT '{}',

    -- true once the user has asked for the node to run; the orchestrator
    -- reconciles running containers against this rather than deleting rows
    -- outright, so pausing a node doesn't lose its configuration.
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,

    -- one of the two nodes ("player"/"maintenance") auto-created alongside
    -- a new user. Can be disabled (enabled = false) like any other node,
    -- but the API must refuse to delete it -- every user always has both.
    is_default          BOOLEAN NOT NULL DEFAULT FALSE,

    -- filled in by the orchestrator once it creates the container/publishes
    -- a metrics port; NULL until the node's first (re)conciliation.
    container_name      TEXT,
    prometheus_port     INT,
    -- which host a node's containers run on -- see the module doc comment
    -- on why this exists before multi-host execution does.
    target_host         TEXT,
    -- AES-256-GCM ciphertext of the per-node bearer token ambot presents
    -- to apiserver's internal config-fetch endpoint. Like game_password_enc,
    -- encrypted (recoverable) rather than hashed -- the orchestrator needs
    -- the plaintext back to hand to the container's environment.
    config_token_enc    TEXT,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (user_id, name)
);

CREATE INDEX idx_nodes_user_id ON nodes(user_id);

-- Reserve a metrics port per node so the orchestrator doesn't have to
-- re-derive "which ports are already taken" from container state.
CREATE UNIQUE INDEX idx_nodes_prometheus_port ON nodes(prometheus_port) WHERE prometheus_port IS NOT NULL;

-- The job queue apiserver writes to and the separate orchestrator service
-- polls -- see the module doc comment on why these two processes never
-- call each other directly.
--
-- Design notes:
--   * node_id is nullable with ON DELETE SET NULL, not CASCADE: a hard
--     node delete must not silently vanish the very operation queued to
--     tear that node's containers down. payload snapshots everything the
--     orchestrator needs (container names, target host, ...) at enqueue
--     time, so a since-deleted node_id is fine -- the operation is
--     self-sufficient.
--   * Only two op_types: "reconcile" (create, update, enable, disable all
--     collapse into this -- the orchestrator just makes containers match
--     the node's current row) and "delete" (the one case with no row left
--     to reconcile against). This avoids a queue of stale, superseded
--     "update" operations racing each other -- a node with two pending
--     reconciles just gets reconciled twice, which is idempotent, rather
--     than each op_type meaning something different that has to be
--     ordered correctly.
CREATE TABLE node_operations (
    id            BIGSERIAL PRIMARY KEY,
    node_id       BIGINT REFERENCES nodes(id) ON DELETE SET NULL,
    op_type       TEXT NOT NULL CHECK (op_type IN ('reconcile', 'delete')),
    -- snapshot of whatever the orchestrator needs to carry out this
    -- operation, taken at enqueue time -- see the node_id note above.
    payload       JSONB NOT NULL DEFAULT '{}',
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- the orchestrator's poll query is always "give me pending work"
CREATE INDEX idx_node_operations_pending ON node_operations(created_at) WHERE status = 'pending';
CREATE INDEX idx_node_operations_node_id ON node_operations(node_id);
