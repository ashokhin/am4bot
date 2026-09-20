package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ErrDefaultNodeNotDeletable is returned by DeleteNode when asked to delete
// a user's default (auto-created) node -- disable it instead.
var ErrDefaultNodeNotDeletable = errors.New("store: the default node cannot be deleted, only disabled")

// ErrNodeNotReady is returned by CreateNode/UpdateNode when asked to
// enable a node that isn't configured enough to actually run yet -- see
// nodeReadyToEnable's doc comment.
var ErrNodeNotReady = errors.New("store: node needs game credentials, a schedule, and a timezone before it can be enabled")

// nodeReadyToEnable reports whether n has the minimum configuration
// ambot needs to not crash-loop immediately: real game credentials, at
// least one cron schedule, and a timezone. Every node starts disabled
// (see the two auto-created defaults in admin_handlers.go) so a user
// configures it FIRST and only then turns it on -- this is what enforces
// that at the data layer, not just in the UI.
func nodeReadyToEnable(n *Node) bool {
	return n.GameUsername != "" && n.GamePasswordEnc != "" && len(n.CronSchedules) > 0 && n.Timezone != ""
}

// defaultTimeoutSeconds mirrors config.Config's own default for the same
// field (internal/config's TimeoutSeconds `default:"180"` tag) -- kept as
// a literal here rather than importing internal/config, to avoid the
// store package depending on it for one constant.
const defaultTimeoutSeconds = 180

// defaultGameURL is the one URL every node uses -- the UI deliberately
// has no field for it (see NodeFormPage's removal of Game URL: "он для
// всех один"), so this is the only place it's ever set from.
const defaultGameURL = "https://www.airlinemanager.com/"

// Node is one ambot container's worth of configuration -- one row per node
// in the product's language (e.g. a user's "departure" or "maintenance"
// node). Services and CronSchedules are ordered: see the design note atop
// migrations/0001_init.sql for why they're Postgres arrays rather than
// JSON, and why nothing here may re-sort them.
type Node struct {
	ID                int64           `db:"id"`
	UserID            int64           `db:"user_id"`
	Name              string          `db:"name"`
	GameURL           string          `db:"game_url"`
	GameUsername      string          `db:"game_username"`
	GamePasswordEnc   string          `db:"game_password_enc"`
	Services          pq.StringArray  `db:"services"`
	CronSchedules     pq.StringArray  `db:"cron_schedules"`
	CronJitterSeconds int             `db:"cron_jitter_seconds"`
	TimeoutSeconds    int             `db:"timeout_seconds"`
	ExtraConfig       json.RawMessage `db:"extra_config"`
	// Timezone interprets every one of this node's CronSchedules entries --
	// one zone for the whole node, not per schedule entry. IANA zone name,
	// validated at the API layer via time.LoadLocation.
	Timezone       string  `db:"timezone"`
	Enabled        bool    `db:"enabled"`
	IsDefault      bool    `db:"is_default"`
	ContainerName  *string `db:"container_name"`
	PrometheusPort *int    `db:"prometheus_port"`
	// TargetHost is the Ansible inventory host the orchestrator provisions
	// this node's containers on. Nil until first assigned (see
	// AssignNodeTarget), which apiserver does right after creating the row.
	TargetHost *string `db:"target_host"`
	// ConfigTokenEnc is AES-256-GCM ciphertext (see the migration's doc
	// comment for why it's encrypted, not hashed): the bearer credential
	// ambot presents to apiserver's internal config endpoint. Nil until
	// the orchestrator's first reconcile mints one (see
	// SetNodeConfigToken).
	ConfigTokenEnc *string   `db:"config_token_enc"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

// NewNodeParams groups the fields a caller supplies when creating a node;
// the rest (id, timestamps, container assignment) are set by the store or
// the orchestrator.
type NewNodeParams struct {
	UserID            int64
	Name              string
	GameURL           string
	GameUsername      string
	GamePasswordEnc   string
	Services          []string
	CronSchedules     []string
	CronJitterSeconds int
	TimeoutSeconds    int
	ExtraConfig       json.RawMessage
	Timezone          string
	// Enabled defaults to false (the zero value) if unset -- a node must
	// be explicitly asked to start enabled, and nodeReadyToEnable still
	// has to hold if it is. This is the opposite of the old behavior
	// (the enabled column's own DEFAULT TRUE), deliberately: see
	// ErrNodeNotReady's doc comment.
	Enabled   bool
	IsDefault bool
}

// CreateNode inserts a new node. GamePasswordEnc must already be
// ciphertext (internal/secrets.Encrypt) -- the store never sees a
// plaintext game password.
func (s *Store) CreateNode(ctx context.Context, p NewNodeParams) (*Node, error) {
	if p.ExtraConfig == nil {
		p.ExtraConfig = json.RawMessage(`{}`)
	}
	p.Services = nonNilSlice(p.Services)
	p.CronSchedules = nonNilSlice(p.CronSchedules)
	// The nodes.timeout_seconds column has its own DEFAULT 180, but since
	// this INSERT always supplies the value explicitly, an unset (zero-value)
	// p.TimeoutSeconds would silently store 0 rather than falling through to
	// that column default -- normalize here instead of relying on every
	// caller (an HTTP handler today, admin tooling or a test tomorrow) to
	// remember to set it themselves.
	if p.TimeoutSeconds == 0 {
		p.TimeoutSeconds = defaultTimeoutSeconds
	}
	if p.Timezone == "" {
		p.Timezone = "UTC"
	}
	// Same story as TimeoutSeconds above: the game_url column has its own
	// DEFAULT, but this INSERT always supplies a value explicitly, so an
	// unset p.GameURL would store "" rather than falling through to it --
	// normalize here rather than trusting every caller to remember. This
	// is exactly what bit the two auto-created default nodes
	// (handleCreateUser's "player"/"maintenance" loop never set GameURL
	// at all): they silently got "" until manually patched, and ambot
	// crash-looped on "config: url is required" the first time either was
	// actually enabled.
	if p.GameURL == "" {
		p.GameURL = defaultGameURL
	}

	if p.Enabled && !nodeReadyToEnable(&Node{
		GameUsername: p.GameUsername, GamePasswordEnc: p.GamePasswordEnc,
		CronSchedules: p.CronSchedules, Timezone: p.Timezone,
	}) {
		return nil, ErrNodeNotReady
	}

	var n Node

	err := s.db.GetContext(ctx, &n, `
		INSERT INTO nodes (
			user_id, name, game_url, game_username, game_password_enc,
			services, cron_schedules, cron_jitter_seconds, timeout_seconds, extra_config, timezone, enabled, is_default
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+nodeColumns+`
	`, p.UserID, p.Name, p.GameURL, p.GameUsername, p.GamePasswordEnc,
		pq.StringArray(p.Services), pq.StringArray(p.CronSchedules), p.CronJitterSeconds, p.TimeoutSeconds,
		p.ExtraConfig, p.Timezone, p.Enabled, p.IsDefault)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a node named %q already exists for this user", ErrConflict, p.Name)
		}

		return nil, fmt.Errorf("creating node: %w", err)
	}

	return &n, nil
}

const nodeColumns = `
	id, user_id, name, game_url, game_username, game_password_enc,
	services, cron_schedules, cron_jitter_seconds, timeout_seconds, extra_config, timezone,
	enabled, is_default, container_name, prometheus_port, target_host, config_token_enc,
	created_at, updated_at
`

// nodeColumnsPrefixed is nodeColumns with an explicit "n." table
// qualifier on every column -- for queries (like ListAllNodesWithOwner)
// that join nodes against another table also having an "id" column,
// where the bare list would be ambiguous.
const nodeColumnsPrefixed = `
	n.id, n.user_id, n.name, n.game_url, n.game_username, n.game_password_enc,
	n.services, n.cron_schedules, n.cron_jitter_seconds, n.timeout_seconds, n.extra_config, n.timezone,
	n.enabled, n.is_default, n.container_name, n.prometheus_port, n.target_host, n.config_token_enc,
	n.created_at, n.updated_at
`

// ListNodesByUser returns every node belonging to userID, oldest first (so
// the auto-created default node -- always created first -- sorts to the
// top of a user's node list).
func (s *Store) ListNodesByUser(ctx context.Context, userID int64) ([]Node, error) {
	var nodes []Node

	if err := s.db.SelectContext(ctx, &nodes,
		`SELECT `+nodeColumns+` FROM nodes WHERE user_id = $1 ORDER BY created_at`, userID,
	); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	return nodes, nil
}

// DisableAllNodesForUser turns off every currently-enabled node belonging
// to userID and returns their ids -- for when an admin disables (or
// deletes) a user: their containers must actually stop, not just the
// account. The caller enqueues a reconcile operation for each returned id
// (see admin_handlers.go) -- this only flips the DB flag, it has no way
// to reach the orchestrator itself.
func (s *Store) DisableAllNodesForUser(ctx context.Context, userID int64) ([]int64, error) {
	var ids []int64

	if err := s.db.SelectContext(ctx, &ids, `
		UPDATE nodes SET enabled = false, updated_at = now()
		WHERE user_id = $1 AND enabled = true
		RETURNING id
	`, userID); err != nil {
		return nil, fmt.Errorf("disabling nodes for user: %w", err)
	}

	return ids, nil
}

// ScrapeTarget is one row of what the orchestrator needs to maintain
// Prometheus' file_sd targets file -- see
// cmd/orchestrator/prometheus_sd.go. UserUUID is what tags every metric
// scraped from this node with its owner, so apiserver's metrics endpoint
// can filter on it -- see decision #7 in the project's design notes.
type ScrapeTarget struct {
	NodeID         int64     `db:"id"`
	TargetHost     string    `db:"target_host"`
	PrometheusPort int       `db:"prometheus_port"`
	UserUUID       uuid.UUID `db:"user_uuid"`
}

// ListProvisionedNodesForScraping returns every node that has been
// assigned a placement (target host + Prometheus port) -- i.e. has gone
// through EnsureNodeProvisioned at least once -- regardless of which user
// owns it or whether it's currently enabled (a disabled node's container
// is stopped, not scrape-worthy, but the orchestrator's own EnsureNodeProvisioned/
// docker compose state, not this list, is what actually decides whether
// its container is running; Prometheus scraping a stopped target just
// times out, same as any other down target).
func (s *Store) ListProvisionedNodesForScraping(ctx context.Context) ([]ScrapeTarget, error) {
	var targets []ScrapeTarget

	if err := s.db.SelectContext(ctx, &targets, `
		SELECT n.id, n.target_host, n.prometheus_port, u.uuid AS user_uuid
		FROM nodes n
		JOIN users u ON u.id = n.user_id
		WHERE n.target_host IS NOT NULL AND n.prometheus_port IS NOT NULL
		ORDER BY n.id
	`); err != nil {
		return nil, fmt.Errorf("listing provisioned nodes for scraping: %w", err)
	}

	return targets, nil
}

// NodeWithOwner is one row of the admin-only "every node, every user"
// view -- see ListAllNodesWithOwner.
type NodeWithOwner struct {
	Node
	OwnerLogin string    `db:"owner_login"`
	OwnerUUID  uuid.UUID `db:"owner_uuid"`
}

// ListAllNodesWithOwner returns every node across every user, newest
// first, with its owner's login/uuid attached. Admin-only (see
// requireNonAdminUser's doc comment: an admin has no nodes of their own,
// only visibility into everyone else's) -- never scope a regular user's
// request through this, use ListNodesByUser instead.
func (s *Store) ListAllNodesWithOwner(ctx context.Context) ([]NodeWithOwner, error) {
	var nodes []NodeWithOwner

	if err := s.db.SelectContext(ctx, &nodes, `
		SELECT `+nodeColumnsPrefixed+`, u.login AS owner_login, u.uuid AS owner_uuid
		FROM nodes n
		JOIN users u ON u.id = n.user_id
		ORDER BY n.created_at DESC
	`); err != nil {
		return nil, fmt.Errorf("listing all nodes: %w", err)
	}

	return nodes, nil
}

// GetNodeWithOwner is the single-row form of ListAllNodesWithOwner -- for
// the admin's read-only node detail screen. Admin-only, same caveat as
// ListAllNodesWithOwner: never scope a regular user's request through
// this, use GetNode instead.
func (s *Store) GetNodeWithOwner(ctx context.Context, id int64) (*NodeWithOwner, error) {
	var n NodeWithOwner

	err := s.db.GetContext(ctx, &n, `
		SELECT `+nodeColumnsPrefixed+`, u.login AS owner_login, u.uuid AS owner_uuid
		FROM nodes n
		JOIN users u ON u.id = n.user_id
		WHERE n.id = $1
	`, id)
	if err != nil {
		return nil, wrapNotFound(err, "node")
	}

	return &n, nil
}

// GetNode looks up a single node by id, scoped to userID so a user can
// never reference (or discover the existence of) another user's node.
func (s *Store) GetNode(ctx context.Context, userID, id int64) (*Node, error) {
	var n Node

	err := s.db.GetContext(ctx, &n,
		`SELECT `+nodeColumns+` FROM nodes WHERE id = $1 AND user_id = $2`, id, userID,
	)
	if err != nil {
		return nil, wrapNotFound(err, "node")
	}

	return &n, nil
}

// GetNodeByID looks up a node by its internal id alone, with no user
// scoping. For the two callers that legitimately need cross-user access:
// apiserver's internal config endpoint (authenticating the caller via the
// node's own config token instead of a user session) and the
// orchestrator (which acts on behalf of the whole system, not one user).
// Never call this from a user-facing handler -- use GetNode instead.
func (s *Store) GetNodeByID(ctx context.Context, id int64) (*Node, error) {
	var n Node

	err := s.db.GetContext(ctx, &n, `SELECT `+nodeColumns+` FROM nodes WHERE id = $1`, id)
	if err != nil {
		return nil, wrapNotFound(err, "node")
	}

	return &n, nil
}

// AssignNodeTarget sets the Ansible inventory host a node's containers
// should be provisioned on. Called once by apiserver right after
// CreateNode (see Node.TargetHost's doc comment).
func (s *Store) AssignNodeTarget(ctx context.Context, id int64, host string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE nodes SET target_host = $2 WHERE id = $1`, id, host)
	if err != nil {
		return fmt.Errorf("assigning node target host: %w", err)
	}

	return checkRowsAffected(res, "node")
}

// SetNodeConfigToken stores tokenEnc (already AES-256-GCM ciphertext, see
// Node.ConfigTokenEnc's doc comment) as the node's config-fetch
// credential, replacing any previous one.
func (s *Store) SetNodeConfigToken(ctx context.Context, id int64, tokenEnc string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE nodes SET config_token_enc = $2 WHERE id = $1`, id, tokenEnc)
	if err != nil {
		return fmt.Errorf("setting node config token: %w", err)
	}

	return checkRowsAffected(res, "node")
}

// EnsureNodeProvisioned assigns whatever a node is still missing to be
// provisionable -- target host, container name, Prometheus port -- and is
// a no-op for anything already set (so calling it on every reconcile is
// fine; it only fills gaps, never reassigns). Returns the node's
// (possibly just-assigned) container name and Prometheus port.
//
// portRangeStart/End bound the search for a free port: candidates are
// tried in order and skipped on a unique-constraint conflict (see
// idx_nodes_prometheus_port), so concurrent provisioning of two nodes
// can't both grab the same port.
func (s *Store) EnsureNodeProvisioned(ctx context.Context, id int64, targetHost string, portRangeStart, portRangeEnd int) (containerName string, prometheusPort int, err error) {
	n, err := s.GetNodeByID(ctx, id)
	if err != nil {
		return "", 0, err
	}

	if n.TargetHost == nil {
		if err := s.AssignNodeTarget(ctx, id, targetHost); err != nil {
			return "", 0, err
		}
	}

	if n.ContainerName == nil {
		owner, err := s.GetUserByID(ctx, n.UserID)
		if err != nil {
			return "", 0, fmt.Errorf("looking up node owner for container name: %w", err)
		}

		// The last 6 hex chars of the owner's uuid, not the id alone --
		// `docker ps` output is otherwise indistinguishable between users'
		// nodes; this is enough to spot at a glance which user a container
		// belongs to and cross-reference against the uuid shown in the
		// admin UI, without the full 36-char uuid cluttering every line.
		uuidSuffix := owner.UUID.String()
		uuidSuffix = uuidSuffix[len(uuidSuffix)-6:]
		name := fmt.Sprintf("ambot-node-%d-u%s", id, uuidSuffix)

		res, err := s.db.ExecContext(ctx, `UPDATE nodes SET container_name = $2 WHERE id = $1`, id, name)
		if err != nil {
			return "", 0, fmt.Errorf("assigning node container name: %w", err)
		}
		if err := checkRowsAffected(res, "node"); err != nil {
			return "", 0, err
		}

		n.ContainerName = &name
	}

	if n.PrometheusPort == nil {
		port, err := s.allocatePrometheusPort(ctx, id, portRangeStart, portRangeEnd)
		if err != nil {
			return "", 0, err
		}

		n.PrometheusPort = &port
	}

	return *n.ContainerName, *n.PrometheusPort, nil
}

// allocatePrometheusPort finds a free port in [start, end] for id and
// stores it, retrying on a unique-constraint conflict with the next
// candidate (see idx_nodes_prometheus_port).
func (s *Store) allocatePrometheusPort(ctx context.Context, id int64, start, end int) (int, error) {
	for port := start; port <= end; port++ {
		res, err := s.db.ExecContext(ctx,
			`UPDATE nodes SET prometheus_port = $2 WHERE id = $1`, id, port,
		)
		if err != nil {
			if isUniqueViolation(err) {
				continue // this port is already taken by another node
			}

			return 0, fmt.Errorf("allocating prometheus port: %w", err)
		}

		if err := checkRowsAffected(res, "node"); err != nil {
			return 0, err
		}

		return port, nil
	}

	return 0, fmt.Errorf("no free prometheus port in range %d-%d", start, end)
}

// UpdateNodeParams groups the fields UpdateNode can change. A nil pointer
// leaves that field untouched; Services and CronSchedules are replaced
// wholesale when non-nil (there is no partial-array update -- the caller,
// e.g. after a drag-and-drop reorder in the UI, always has the full
// desired order in hand already).
type UpdateNodeParams struct {
	Name              *string
	GameURL           *string
	GameUsername      *string
	GamePasswordEnc   *string
	Services          *[]string
	CronSchedules     *[]string
	CronJitterSeconds *int
	TimeoutSeconds    *int
	ExtraConfig       *json.RawMessage
	Timezone          *string
	Enabled           *bool
}

// UpdateNode applies a partial update to one node and returns the row as
// it stands afterward. Returns ErrNotFound if no node with that id exists
// for userID.
func (s *Store) UpdateNode(ctx context.Context, userID, id int64, p UpdateNodeParams) (*Node, error) {
	current, err := s.GetNode(ctx, userID, id)
	if err != nil {
		return nil, err
	}

	if p.Name != nil {
		current.Name = *p.Name
	}
	if p.GameURL != nil {
		current.GameURL = *p.GameURL
	}
	if p.GameUsername != nil {
		current.GameUsername = *p.GameUsername
	}
	if p.GamePasswordEnc != nil {
		current.GamePasswordEnc = *p.GamePasswordEnc
	}
	if p.Services != nil {
		current.Services = pq.StringArray(nonNilSlice(*p.Services))
	}
	if p.CronSchedules != nil {
		current.CronSchedules = pq.StringArray(nonNilSlice(*p.CronSchedules))
	}
	if p.CronJitterSeconds != nil {
		current.CronJitterSeconds = *p.CronJitterSeconds
	}
	if p.TimeoutSeconds != nil {
		current.TimeoutSeconds = *p.TimeoutSeconds
	}
	if p.ExtraConfig != nil {
		current.ExtraConfig = *p.ExtraConfig
	}
	if p.Timezone != nil {
		current.Timezone = *p.Timezone
	}
	if p.Enabled != nil {
		current.Enabled = *p.Enabled
	}

	if current.Enabled && !nodeReadyToEnable(current) {
		return nil, ErrNodeNotReady
	}

	var n Node

	err = s.db.GetContext(ctx, &n, `
		UPDATE nodes SET
			name = $3, game_url = $4, game_username = $5, game_password_enc = $6,
			services = $7, cron_schedules = $8, cron_jitter_seconds = $9, timeout_seconds = $10,
			extra_config = $11, timezone = $12, enabled = $13, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING `+nodeColumns, id, userID,
		current.Name, current.GameURL, current.GameUsername, current.GamePasswordEnc,
		current.Services, current.CronSchedules, current.CronJitterSeconds, current.TimeoutSeconds,
		current.ExtraConfig, current.Timezone, current.Enabled)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a node named %q already exists for this user", ErrConflict, current.Name)
		}

		return nil, fmt.Errorf("updating node: %w", err)
	}

	return &n, nil
}

// nonNilSlice returns s, or an empty (non-nil) slice if s is nil.
// pq.StringArray(nil) serializes as SQL NULL, which the NOT NULL
// constraint on the services/cron_schedules columns rejects.
func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// DeleteNode removes a node, refusing if it's the user's default node
// (ErrDefaultNodeNotDeletable) -- disable it via UpdateNode instead.
func (s *Store) DeleteNode(ctx context.Context, userID, id int64) error {
	n, err := s.GetNode(ctx, userID, id)
	if err != nil {
		return err
	}

	if n.IsDefault {
		return ErrDefaultNodeNotDeletable
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("deleting node: %w", err)
	}

	return checkRowsAffected(res, "node")
}

// SetNodeLogLevel sets or clears (level == "") the "log_level" key inside
// a node's extra_config JSONB -- the one field of a node's config an
// admin can set on ANY user's node, unscoped by owning user, unlike
// every other node field (see requireNonAdminUser's and
// AdminNodeDetailPage's doc comments on why an admin otherwise only
// looks, never touches). A regular user never sees or edits this
// themselves -- it's meant for an admin to dial in debug/error logging
// on a misbehaving node without needing game credentials or any other
// access to it. The jsonb `||` merge operator leaves every other
// extra_config key untouched and needs no read-modify-write round trip.
func (s *Store) SetNodeLogLevel(ctx context.Context, id int64, level string) error {
	var res sql.Result

	var err error

	if level == "" {
		res, err = s.db.ExecContext(ctx,
			`UPDATE nodes SET extra_config = extra_config - 'log_level' WHERE id = $1`, id)
	} else {
		res, err = s.db.ExecContext(ctx,
			`UPDATE nodes SET extra_config = extra_config || jsonb_build_object('log_level', $2::text) WHERE id = $1`,
			id, level)
	}

	if err != nil {
		return fmt.Errorf("setting node log level: %w", err)
	}

	return checkRowsAffected(res, "node")
}
