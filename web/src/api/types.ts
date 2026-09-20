// Mirrors internal/api's JSON response/request shapes (Go's userResponse,
// createUserRequest, etc.). Keep field names in sync with the Go structs'
// json tags -- there's no shared schema generation between the two yet.

export interface User {
  uuid: string
  // Just a login identifier the admin picks when creating the account --
  // there's no signup flow, so this was never an email address (see
  // internal/store/migrations/0005's doc comment).
  login: string
  // Purely cosmetic, self-editable label -- null means "show login
  // instead". Never used for sign-in.
  display_name: string | null
  is_admin: boolean
  disabled: boolean
  // Which vpn_regions catalog entry ALL of this user's nodes exit VPN
  // traffic through -- a single per-user choice, not per-node. Null means
  // no VPN. See internal/store/vpn_regions.go's doc comment for the full
  // model: one admin-configured provider account shared by everyone, with
  // only the exit region selectable per user. Always null for an admin.
  vpn_region_id: number | null
  // True whenever the current password was set FOR this user (account
  // creation, an admin's reset) rather than chosen BY them -- forces the
  // change-password screen until they pick their own. See
  // internal/store/migrations/0002's doc comment.
  must_change_password: boolean
}

export interface LoginRequest {
  login: string
  password: string
}

export interface CreateUserRequest {
  login: string
  password: string
  // Promotes the new account to admin instead of a regular (node-owning)
  // user -- see internal/api/admin_handlers.go's createUserRequest doc
  // comment for why this exists (creating a non-default-login admin so
  // the bootstrap "admin"/"admin" account can be disabled).
  is_admin: boolean
}

// GET /api/admin/users row -- User plus what the admin list screen shows
// beyond a single user's own /api/me shape.
export interface AdminUserListEntry extends User {
  last_login_at: string | null
  active_nodes: number
  total_nodes: number
}

// One entry of GET /api/me/login-activity or the login_activity field on
// AdminUserDetail -- one attempt, success or failure.
export interface LoginActivityEntry {
  success: boolean
  ip: string
  user_agent: string
  created_at: string
}

// A currently-active brute-force login ban (see internal/api/login_guard.go)
// -- present on AdminUserDetail.ban only while the account is locked.
export interface LoginBan {
  reason: string
  banned_at: string
  unban_at: string
}

// GET /api/admin/users/{uuid} -- a user's full admin-visible profile plus
// their nodes, for the user detail screen.
export interface AdminUserDetail extends User {
  last_login_at: string | null
  last_login_ip: string | null
  last_login_user_agent: string | null
  last_failed_login_at: string | null
  last_failed_login_ip: string | null
  last_failed_login_user_agent: string | null
  // Non-null only while a brute-force ban is currently active -- see
  // LoginBan. Distinct from `disabled` (a manual, indefinite admin
  // action): this is automatic and time-limited, lifted by
  // adminApi.unlockUser or by simply waiting out unban_at.
  ban: LoginBan | null
  login_activity: LoginActivityEntry[]
  created_at: string
  vpn_region_name: string | null
  nodes: Node[]
}

export interface ResetUserPasswordRequest {
  password: string
}

export interface SetMyPasswordRequest {
  new_password: string
}

export interface SetMyDisplayNameRequest {
  display_name: string | null
}

export interface ApiErrorBody {
  error: string
}

// The full list of services ambot understands, in no particular order --
// see internal/bot/bot.go's own service-name map. What order a node
// actually runs them in is entirely `Node.services`' own array order;
// this list is just "what's available to add".
export const ALL_SERVICES = [
  'company_stats',
  'alliance_stats',
  'claim_rewards',
  'staff_morale',
  'hubs',
  'buy_fuel',
  'marketing',
  'ac_maintenance',
  'depart',
] as const

export type ServiceName = (typeof ALL_SERVICES)[number]

export interface Node {
  id: number
  name: string
  game_url: string
  game_username: string
  // services/cron_schedules are ORDERED -- see migrations/0001_init.sql's
  // design note. Never sort these; only ever send back the exact order
  // the user last arranged them in (e.g. via drag-and-drop).
  services: string[]
  cron_schedules: string[]
  cron_jitter_seconds: number
  timeout_seconds: number
  extra_config: Record<string, unknown>
  // IANA zone name interpreting every one of this node's cron_schedules
  // entries -- one zone for the whole node, chosen by the user alongside
  // the schedule, not by the admin.
  timezone: string
  enabled: boolean
  is_default: boolean
  // Whether a real game password has ever been set on this node -- a
  // freshly auto-created default node hasn't, even though it already has
  // an id (so it's reached via the edit form like any configured node).
  // Drives whether the edit form's password field can be left blank.
  has_game_password: boolean
}

// Mirrors internal/config.Config's own fields that a node's extra_config
// JSONB can override (see internal/api/internal_handlers.go's doc comment
// on the merge: config.Config defaults, then extra_config overlaid on
// top). Every field here is optional -- an unset one just falls through
// to config.Config's own package default (see the main README's
// Configuration table). "log_level" is deliberately NOT here: a regular
// user never sets it, only an admin can (via a separate endpoint) -- see
// AdvancedSettingsSection's doc comment.
export interface NodeExtraConfig {
  budget_percent?: {
    fuel?: number
    maintenance?: number
    marketing?: number
  }
  good_price?: {
    fuel?: number
    co2?: number
  }
  fuel_critical_percent?: number
  hubs_maintenance_limit?: number
  repair_lounges?: boolean
  buy_catering_if_missing?: boolean
  catering_duration_hours?: string
  catering_amount_option?: string
  aircraft_wear_percent?: string
  aircraft_max_hours_to_check?: number
  aircraft_modify_limit?: number
  alliance_ids?: string[]
}

export interface CreateNodeRequest {
  name: string
  game_url?: string
  game_username: string
  game_password: string
  services?: string[]
  cron_schedules?: string[]
  cron_jitter_seconds?: number
  timeout_seconds?: number
  timezone?: string
  extra_config?: NodeExtraConfig
}

// Every field optional: only what's present gets changed (mirrors Go's
// **updateNodeRequest** -- omitting a field there means "leave it alone",
// distinct from sending an empty value).
export interface UpdateNodeRequest {
  name?: string
  game_url?: string
  game_username?: string
  game_password?: string
  services?: string[]
  cron_schedules?: string[]
  cron_jitter_seconds?: number
  timeout_seconds?: number
  timezone?: string
  enabled?: boolean
  extra_config?: NodeExtraConfig
}

// Admin-only, read-only view -- see internal/api/admin_handlers.go's
// adminNodeResponse. An admin never creates/edits a node, only sees whose
// it is -- with the one exception of log_level, see SetNodeLogLevelRequest.
export interface AdminNodeView extends Node {
  owner_login: string
  owner_uuid: string
}

// PUT /api/admin/nodes/{id}/log-level -- the one field of another user's
// node an admin can actually change (dialing in debug/error logging on a
// misbehaving node without touching anything else about it). Empty string
// clears the override back to config.Config's own default ("info").
export interface SetNodeLogLevelRequest {
  log_level: '' | 'debug' | 'info' | 'warn' | 'error'
}

// VPNRegion is name-only -- the ovpn file lives behind the one shared
// provider account, never sent to a browser. See internal/api/vpn_handlers.go.
export interface VPNRegion {
  id: number
  name: string
}

export interface CreateVPNRegionRequest {
  name: string
  ovpn_config: string
}

export interface VPNProviderStatus {
  configured: boolean
  provider?: string
}

export interface SetVPNProviderRequest {
  provider?: string
  vpn_username: string
  vpn_password: string
}

export interface SetUserVPNRegionRequest {
  vpn_region_id: number | null
}

// Mirrors internal/api/metrics_handlers.go's metricSeries -- one data
// point (Prometheus' instant-query result) for one metric on one node.
export interface MetricSeries {
  metric: string
  node_id?: number
  value: number
  timestamp: number
}

export interface MetricsResponse {
  configured: boolean
  series: MetricSeries[]
}

export interface PrometheusSettingsStatus {
  configured: boolean
  url?: string
}

export interface SetPrometheusSettingsRequest {
  url: string
}
