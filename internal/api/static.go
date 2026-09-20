package api

import (
	"bytes"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// spaFileServer serves the built React SPA out of distFS (see
// internal/webui.DistFS), falling back to index.html for any path that
// isn't a real file -- react-router's client-side routes (e.g. /nodes,
// /admin/users/<uuid>) only exist in the browser, so a hard refresh or a
// direct link to one has to still get index.html, not a 404, and let the
// SPA's own router take it from there.
//
// basePath is --web.route-prefix/WEB_ROUTE_PREFIX (see cmd/apiserver's
// flag), "" by default. When set, this app is mounted under that path
// (e.g. a reverse proxy serving Prometheus at /prometheus and this UI at
// /app on the same port/domain, Prometheus/Grafana-style route-prefix --
// no path rewriting needed on the proxy side). NewServer already strips
// basePath from every incoming request before it reaches this handler (or
// any /api/* route) via http.StripPrefix, so nothing here or in
// server.go's route registrations needs to know about it -- the ONE place
// that does is index.html's own asset references (`src="/assets/..."`),
// since those are absolute paths the BROWSER re-requests verbatim; without
// rewriting them to `src="<basePath>/assets/..."` here, the browser would
// ask for them at the untranslated root path and get a 404. See
// rewriteIndexHTML.
//
// Registered on "/" on the PUBLIC mux only -- api routes registered on
// more specific patterns (e.g. "GET /api/nodes") take precedence in
// Go's ServeMux, so this never shadows them; it only ever handles
// requests nothing more specific matched.
func spaFileServer(distFS fs.FS, basePath string) (http.Handler, error) {
	fileServer := http.FileServer(http.FS(distFS))

	indexHTML, err := rewriteIndexHTML(distFS, basePath)
	if err != nil {
		return nil, fmt.Errorf("rewriting index.html for base path %q: %w", basePath, err)
	}

	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(indexHTML))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" || name == "index.html" {
			serveIndex(w, r)

			return
		}

		if _, err := fs.Stat(distFS, name); err != nil {
			// Not a real file in the build output -- serve index.html
			// (already rewritten) instead of 404ing, same SPA-fallback
			// reasoning as above.
			serveIndex(w, r)

			return
		}

		fileServer.ServeHTTP(w, r)
	}), nil
}

// rewriteIndexHTML reads index.html out of distFS once at startup and, if
// basePath is non-empty, rewrites its absolute asset references
// (`src="/..."`, `href="/..."`) to be prefixed with basePath, and injects
// a <meta name="am4bot-base-path" content="..."> tag -- the frontend's own
// runtime source of truth for this (see web/src/api/client.ts and
// web/src/main.tsx), so the same build works mounted at any path without a
// rebuild, matching Prometheus/Grafana's own route-prefix behavior.
//
// A <meta> tag, not the inline <script> an earlier version of this used --
// deliberately, so a strict script-src 'self' CSP (see
// security_headers.go) doesn't need a nonce or 'unsafe-inline' carved out
// just for this one value; a <meta> tag isn't script execution at all, so
// CSP's script-src has no opinion on it.
func rewriteIndexHTML(distFS fs.FS, basePath string) ([]byte, error) {
	raw, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("reading index.html: %w", err)
	}

	content := string(raw)

	if basePath != "" {
		content = strings.ReplaceAll(content, ` src="/`, ` src="`+basePath+`/`)
		content = strings.ReplaceAll(content, ` href="/`, ` href="`+basePath+`/`)
	}

	// html.EscapeString even though basePath is operator-controlled (a
	// CLI flag/env var, never end-user input) and validateRoutePrefix
	// already constrains its shape -- cheap defense-in-depth against
	// breaking out of the attribute if that validation is ever loosened.
	meta := fmt.Sprintf(`<meta name="am4bot-base-path" content="%s">`, html.EscapeString(basePath))
	if idx := strings.Index(content, "<head>"); idx != -1 {
		insertAt := idx + len("<head>")
		content = content[:insertAt] + "\n  " + meta + content[insertAt:]
	} else {
		// No <head> tag found (a hand-edited or unusual index.html) --
		// still inject it, just at the very top, rather than silently
		// skipping it and leaving the frontend to guess.
		content = meta + content
	}

	return []byte(content), nil
}
