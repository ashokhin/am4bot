package main

import (
	"strings"
	"testing"
)

func TestComposeErrorDetail(t *testing.T) {
	long := strings.Repeat("x", 300)

	tests := []struct {
		name    string
		output  string
		secrets []string
		want    string
	}{
		{"empty output", "", nil, ""},
		{"only whitespace", "\n  \n", nil, ""},
		{"first non-empty line only", "\n  service \"a\" is broken  \nsecond line\n", nil, `service "a" is broken`},
		{"secret is redacted", `invalid interpolation format: "pa$$word"`, []string{"pa$$word"}, `invalid interpolation format: "[redacted]"`},
		{"empty secret is ignored, not everywhere", "abc", []string{""}, "abc"},
		{"several secrets", "tok=T1 pw=P2", []string{"T1", "P2"}, "tok=[redacted] pw=[redacted]"},
		{"long line is capped", long, nil, strings.Repeat("x", maxComposeErrorDetail) + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := composeErrorDetail([]byte(tt.output), tt.secrets); got != tt.want {
				t.Fatalf("composeErrorDetail() = %q, want %q", got, tt.want)
			}
		})
	}
}
