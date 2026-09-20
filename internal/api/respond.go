package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

func logRequest(method, path string, status int, dur time.Duration) {
	slog.Debug("request", "method", method, "path", path, "status", status, "duration", dur)
}

// writeJSON encodes v as the response body with the given status code.
// Errors encoding v are logged but otherwise unrecoverable at this point
// (headers are already sent), matching the standard net/http tradeoff.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encoding JSON response", "error", err)
	}
}

// apiError is the JSON shape returned for every non-2xx response.
type apiError struct {
	Error string `json:"error"`
}

// writeError writes a JSON error body. msg is shown to the caller, so it
// must never contain anything sensitive (a raw DB error, a stack trace,
// credential material) -- callers pass a fixed, safe string here and log
// the real error themselves beforehand if it's worth keeping.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// maxRequestBodyBytes bounds every JSON request body readJSON decodes.
// Generous for anything this API legitimately receives (the largest is an
// OVPN config file upload, well under a megabyte) -- this exists purely to
// stop an internet-facing endpoint from being handed a multi-gigabyte body
// and decoding it into memory unbounded.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// readJSON decodes the request body into v, rejecting unknown fields so a
// typo or an out-of-date client silently dropping a field is a visible
// error instead of quietly ignored input. The body is capped at
// maxRequestBodyBytes via http.MaxBytesReader -- a decode that hits the
// limit fails with an error readJSON's callers already treat as a normal
// "invalid request body" 400.
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	return dec.Decode(v)
}
