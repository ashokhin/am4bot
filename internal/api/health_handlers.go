package api

import (
	"context"
	"net/http"
	"time"
)

// handleHealthz is a liveness probe: it never touches the database or any
// other dependency, only confirms the process is up and serving requests
// at all. Registered on the PUBLIC listener (see cmd/apiserver's
// --web.listen-address) -- not the internal one -- because that's the
// port both HAProxy's own backend healthcheck and `docker compose`'s own
// `healthcheck:` already reach the apiserver container on. "Internal" here
// means "for infrastructure, not for browsers or the public HAProxy
// frontend's routed paths", not "the /internal/* listener".
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// healthCheckTimeout bounds handleReadyz's own database ping -- a
// readiness probe must fail fast on a hung database, not tie up the
// prober (and pile up concurrent checks) for as long as the caller's own
// timeout allows.
const healthCheckTimeout = 3 * time.Second

// handleReadyz is a readiness probe: unlike handleHealthz, it actually
// pings the database, so a prober can tell "the process is up but can't
// serve real requests" (503) apart from "the process is up and healthy"
// (200). Same public-listener placement as handleHealthz -- see that
// doc comment.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": err.Error()})

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
