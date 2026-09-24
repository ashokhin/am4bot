package api

import (
	"net"
	"net/http"
	"testing"
)

func mustNets(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()

	var nets []*net.IPNet

	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", c, err)
		}

		nets = append(nets, n)
	}

	return nets
}

func TestClientIP(t *testing.T) {
	proxy := mustNets(t, "172.19.0.1/32")
	multi := mustNets(t, "172.19.0.1/32", "10.0.0.0/8")

	tests := []struct {
		name    string
		trusted []*net.IPNet
		remote  string
		xff     []string
		want    string
	}{
		{"no trusted proxies: header ignored", nil, "172.19.0.1:5000", []string{"203.0.113.7"}, "172.19.0.1"},
		{"untrusted peer: forged header ignored", proxy, "198.51.100.9:5000", []string{"203.0.113.7"}, "198.51.100.9"},
		{"trusted peer, no header", proxy, "172.19.0.1:5000", nil, "172.19.0.1"},
		{"trusted peer, real client", proxy, "172.19.0.1:5000", []string{"203.0.113.7"}, "203.0.113.7"},
		{"client-supplied prefix is ignored", proxy, "172.19.0.1:5000", []string{"1.2.3.4, 203.0.113.7"}, "203.0.113.7"},
		{"client-supplied prefix in a separate header line", proxy, "172.19.0.1:5000", []string{"1.2.3.4", "203.0.113.7"}, "203.0.113.7"},
		{"chained trusted proxies are skipped", multi, "172.19.0.1:5000", []string{"203.0.113.7, 10.1.2.3"}, "203.0.113.7"},
		{"all entries trusted: leftmost", multi, "172.19.0.1:5000", []string{"10.9.9.9, 10.1.2.3"}, "10.9.9.9"},
		{"malformed entry falls back to peer", proxy, "172.19.0.1:5000", []string{"not-an-ip"}, "172.19.0.1"},
		{"ipv6 client", proxy, "172.19.0.1:5000", []string{"2001:db8::7"}, "2001:db8::7"},
		{"remote addr without a port", nil, "192.0.2.1", nil, "192.0.2.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{opts: ServerOptions{TrustedProxies: tt.trusted}}
			r := &http.Request{RemoteAddr: tt.remote, Header: http.Header{}}

			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}

			if got := s.clientIP(r); got != tt.want {
				t.Fatalf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
