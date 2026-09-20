package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// withRecover catches a panic anywhere downstream (a nil deref, an
// index-out-of-range on malformed input, ...) and turns it into a logged
// 500 instead of an unhandled panic. net/http's own per-connection recovery
// would otherwise just close the connection and dump the stack to raw
// stderr, bypassing this app's structured logging entirely -- this makes
// the failure visible the same way every other error already is (slog),
// and gives the caller a normal JSON error body instead of a dropped
// connection.
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in handler",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)

				// Best-effort -- if the handler already wrote a status
				// code/body before panicking, WriteHeader here is a no-op
				// net/http logs and ignores, not a second response.
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
