package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakePrometheus answers each instant query with a canned vector chosen by
// what the query looks like, so queryOneMetricDelta's three-query flow can
// be exercised without a real Prometheus.
func fakePrometheus(t *testing.T, primary, partial, since map[string]string) *httptest.Server {
	t.Helper()

	vector := func(byNode map[string]string) string {
		var parts []string

		for node, v := range byNode {
			parts = append(parts, fmt.Sprintf(`{"metric":{"node_id":%q},"value":[1790000000,%q]}`, node, v))
		}

		return `{"status":"success","data":{"result":[` + strings.Join(parts, ",") + `]}}`
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")

		var body string

		switch {
		case strings.HasPrefix(q, "min_over_time(timestamp("):
			body = vector(since)
		case strings.Contains(q, "min_over_time("):
			body = vector(partial)
		case strings.Contains(q, " offset "):
			body = vector(primary)
		default:
			t.Errorf("unexpected query %q", q)
		}

		_, _ = w.Write([]byte(body))
	}))
}

func TestQueryOneMetricDelta(t *testing.T) {
	tests := []struct {
		name    string
		primary map[string]string
		partial map[string]string
		since   map[string]string
		// node id -> want value; wantSince: node id -> whether Since is set
		want      map[int64]float64
		wantSince map[int64]bool
	}{
		{
			name:      "full history: primary wins, no Since",
			primary:   map[string]string{"1": "10"},
			partial:   map[string]string{"1": "7"},
			since:     map[string]string{"1": "1789990000"},
			want:      map[int64]float64{1: 10},
			wantSince: map[int64]bool{1: false},
		},
		{
			name:      "no history for the period: fallback with Since",
			partial:   map[string]string{"1": "44"},
			since:     map[string]string{"1": "1789990000"},
			want:      map[int64]float64{1: 44},
			wantSince: map[int64]bool{1: true},
		},
		{
			name:      "mixed: each node uses its own source",
			primary:   map[string]string{"1": "10"},
			partial:   map[string]string{"1": "7", "2": "0"},
			since:     map[string]string{"1": "1789990000", "2": "1789995000"},
			want:      map[int64]float64{1: 10, 2: 0},
			wantSince: map[int64]bool{1: false, 2: true},
		},
		{
			name: "no data at all: empty",
			want: map[int64]float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := fakePrometheus(t, tt.primary, tt.partial, tt.since)
			defer srv.Close()

			s := &Server{httpClient: srv.Client()}

			got, err := s.queryOneMetricDelta(t.Context(), srv.URL, "am4_flights_departed_total", "u", "24h")
			if err != nil {
				t.Fatalf("queryOneMetricDelta() error: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d series, want %d: %+v", len(got), len(tt.want), got)
			}

			for _, p := range got {
				want, ok := tt.want[*p.NodeID]
				if !ok || p.Value != want {
					t.Fatalf("node %d = %v, want %v (present=%v)", *p.NodeID, p.Value, want, ok)
				}

				if (p.Since != nil) != tt.wantSince[*p.NodeID] {
					t.Fatalf("node %d Since set = %v, want %v", *p.NodeID, p.Since != nil, tt.wantSince[*p.NodeID])
				}
			}
		})
	}
}

func TestParseDeltaPeriodAcceptsShortPeriods(t *testing.T) {
	for _, p := range []string{"1h", "6h", "12h", "24h", "3d", "7d", "14d", "30d"} {
		r := httptest.NewRequest(http.MethodGet, "/x?period="+p, nil)

		if got, err := parseDeltaPeriod(r); err != nil || got != p {
			t.Fatalf("parseDeltaPeriod(%q) = %q, %v", p, got, err)
		}

		if rangeStepFor[p] == "" {
			t.Fatalf("period %q has no query_range step", p)
		}

		if _, err := parsePromDuration(p); err != nil {
			t.Fatalf("parsePromDuration(%q): %v", p, err)
		}
	}

	if _, err := parseDeltaPeriod(httptest.NewRequest(http.MethodGet, "/x?period=99h", nil)); err == nil {
		t.Fatal("parseDeltaPeriod(99h) = nil error, want an error")
	}
}
