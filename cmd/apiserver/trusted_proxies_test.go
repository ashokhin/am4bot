package main

import "testing"

func TestParseTrustedProxies(t *testing.T) {
	good := map[string]int{
		"":                          0,
		"  ":                        0,
		"172.19.0.1":                1,
		"10.0.0.0/8":                1,
		"172.19.0.1, 10.0.0.0/8":    2,
		"2001:db8::1,2001:db8::/32": 2,
		"172.19.0.1,,":              1,
	}

	for in, want := range good {
		got, err := parseTrustedProxies(in)
		if err != nil {
			t.Fatalf("parseTrustedProxies(%q) unexpected error: %v", in, err)
		}

		if len(got) != want {
			t.Fatalf("parseTrustedProxies(%q) = %d networks, want %d", in, len(got), want)
		}
	}

	for _, in := range []string{"proxy.local", "10.0.0.0/33", "172.19.0.1;10.0.0.1"} {
		if _, err := parseTrustedProxies(in); err == nil {
			t.Fatalf("parseTrustedProxies(%q) = nil error, want an error", in)
		}
	}
}
