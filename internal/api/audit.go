package api

import (
	"log/slog"
	"net/http"
)

// audit logs one mutating action with a consistent field schema --
// actor_role, actor_login, actor_uuid, action, target -- so these lines
// are easy to find/filter later in whatever log aggregation (Elasticsearch,
// Loki, ...) this deployment ends up using, regardless of which handler
// wrote them. Called from every handler that actually CHANGES something
// (create/update/delete/disable/reset/...), both admin AND regular-user
// actions alike -- not just admin ones, and never from a read-only GET/LIST
// handler (nothing to audit there).
//
// action is a short, fixed, snake_case verb (e.g. "disable_user",
// "create_node") -- see each call site for the exact vocabulary in use.
// target identifies WHAT the action was taken on (a uuid, a numeric node
// id as a string, or "" for something that has no id of its own, e.g. the
// one singleton VPN provider account) -- NOT who did it, that's
// actor_uuid/actor_login, always the CALLER, even for an admin action on
// someone else's account (the affected account is the target, not a
// second actor).
//
// Always logged at Info, not Debug -- an audit trail that only shows up
// with verbose logging enabled defeats the point of having one.
func (s *Server) audit(r *http.Request, action, target string) {
	claims := claimsFromContext(r.Context())

	role := "user"
	if claims.IsAdmin {
		role = "admin"
	}

	slog.Info("audit",
		"actor_role", role,
		"actor_login", claims.Login,
		"actor_uuid", claims.UserUUID,
		"action", action,
		"target", target,
	)
}
