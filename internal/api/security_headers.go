package api

import "net/http"

// contentSecurityPolicy was written against an actual audit of what
// web/dist's built SPA loads at runtime (grepped the compiled JS/CSS for
// every http(s):// literal): nothing external at all -- no CDN, no
// Google Fonts, no analytics, no third-party script of any kind. Every
// script/style/font/image the page needs is bundled into the same-origin
// JS/CSS files apiserver itself serves. That's what makes a strict
// policy possible here without breaking the app:
//   - script-src 'self', no 'unsafe-inline'/nonce carve-out needed --
//     the one piece of per-request dynamic data the page used to need
//     inline script for (the route-prefix base path) is now a <meta>
//     tag instead, specifically so this could stay strict. See
//     static.go's rewriteIndexHTML.
//   - style-src keeps 'unsafe-inline': Radix UI (Popover, Select,
//     Dialog, ...) sets inline style="" attributes at runtime for
//     dynamic positioning (via Floating UI) -- there's no practical way
//     to nonce those without much deeper surgery, and an inline STYLE
//     attribute can't execute script, so this is a far smaller
//     concession than 'unsafe-inline' on script-src would be.
//   - connect-src 'self': every fetch() this app makes is to its own
//     /api/*.
//   - frame-ancestors 'none': the modern, more consistently-enforced
//     equivalent of X-Frame-Options: DENY below -- kept both since
//     browser support for each differs slightly.
//   - object-src 'none', base-uri 'self', form-action 'self': standard
//     hardening boilerplate this app has no legitimate use for anyway
//     (no <object>/<embed>, no reason to ever repoint <base>, no
//     traditional <form> submissions -- everything goes through fetch).
//
// Re-audit this (repeat the grep in this comment's own first sentence
// against a fresh `npm run build`) before adding any new dependency that
// might load something external -- a forgotten Google Fonts import, an
// analytics snippet, etc. would silently violate this and either break
// or get silently blocked depending on the browser.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// withSecurityHeaders sets a minimal set of response headers that cost
// nothing to apply and close off cheap, generic attack classes an
// internet-facing server otherwise leaves open by default. Deliberately
// NOT doing here: HSTS (belongs at the TLS-terminating reverse proxy, see
// docs/multi-tenant-hosting.md -- this app itself may be plain HTTP behind
// it).
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Stops a browser from sniffing a response's content-type away from
		// what Content-Type actually says (e.g. treating a JSON error body
		// as HTML/script because it looks vaguely like markup).
		h.Set("X-Content-Type-Options", "nosniff")
		// This app is never meant to be framed by another site -- refuses
		// clickjacking-style embedding outright.
		h.Set("X-Frame-Options", "DENY")
		// Don't leak this app's URLs (which can contain uuids/ids) to
		// whatever a user clicks through to next.
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)

		next.ServeHTTP(w, r)
	})
}
