package api

import (
	"net"
	"net/http"
	"strings"
)

// clientIP returns the address this request should be attributed to --
// what login activity shows and what the per-IP login ban keys on.
//
// By default that's just the direct peer (r.RemoteAddr). Behind a reverse
// proxy that is the proxy's own address for every request, so when the
// direct peer is in ServerOptions.TrustedProxies its X-Forwarded-For
// header is honored instead: walking the list from the right (each proxy
// appends the address it received the request from, so the rightmost
// entries are the ones our own proxies wrote), the first address that is
// not itself a trusted proxy is the client. Anything to the left of that
// was supplied by the client and is never used, so a forged header can't
// pick the address. The header is ignored entirely when the peer isn't a
// trusted proxy, and a malformed entry falls back to the direct peer.
func (s *Server) clientIP(r *http.Request) string {
	peer := peerIP(r)

	if len(s.opts.TrustedProxies) == 0 || !s.isTrustedProxy(peer) {
		return peer
	}

	var entries []string

	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, e := range strings.Split(v, ",") {
			if e = strings.TrimSpace(e); e != "" {
				entries = append(entries, e)
			}
		}
	}

	for i := len(entries) - 1; i >= 0; i-- {
		if net.ParseIP(entries[i]) == nil {
			return peer
		}

		if i == 0 || !s.isTrustedProxy(entries[i]) {
			return entries[i]
		}
	}

	return peer
}

// peerIP extracts the connection's remote address from r.RemoteAddr
// (host:port), falling back to the raw value if it isn't in that form.
func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

func (s *Server) isTrustedProxy(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}

	for _, n := range s.opts.TrustedProxies {
		if n.Contains(ip) {
			return true
		}
	}

	return false
}
