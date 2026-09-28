package api

import "testing"

func TestMaskIP(t *testing.T) {
	tests := map[string]string{
		"203.0.113.77":         "203.0.113.x",
		"192.168.1.65":         "192.168.1.x",
		"::ffff:203.0.113.7":   "203.0.113.x",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1::/48",
		"fe80::1%eth0":         "fe80::/48",
		"":                     "invalid",
		"not-an-ip":            "invalid",
		"1.2.3.4\nforged":      "invalid",
		"203.0.113.7:5000":     "invalid",
	}

	for in, want := range tests {
		if got := maskIP(in); got != want {
			t.Errorf("maskIP(%q) = %q, want %q", in, got, want)
		}
	}
}
