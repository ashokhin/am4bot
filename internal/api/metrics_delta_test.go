package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakePrometheus answers each instant query with a canned vector chosen by
// what the query looks like, so queryOneMetricDelta's two-query flow can be
// exercised without a real Prometheus. It also records the queries it saw.
func fakePrometheus(t *testing.T, increase, since map[string]string, seen *[]string) *httptest.Server {
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
		*seen = append(*seen, q)

		var body string

		switch {
		case strings.HasPrefix(q, "min_over_time(timestamp("):
			body = vector(since)
		case strings.HasPrefix(q, "increase("):
			body = vector(increase)
		default:
			t.Errorf("unexpected query %q", q)
		}

		_, _ = w.Write([]byte(body))
	}))
}

func TestQueryOneMetricDelta(t *testing.T) {
	now := time.Now()
	unix := func(d time.Duration) string { return strconv.FormatInt(now.Add(d).Unix(), 10) }

	tests := []struct {
		name      string
		increase  map[string]string
		since     map[string]string
		want      map[int64]float64
		wantSince map[int64]bool
	}{
		{
			name:      "history covers the whole 24h window: no Since",
			increase:  map[string]string{"1": "601.1"},
			since:     map[string]string{"1": unix(-24 * time.Hour)},
			want:      map[int64]float64{1: 601.1},
			wantSince: map[int64]bool{1: false},
		},
		{
			name:      "history starts within tolerance of the window start: no Since",
			increase:  map[string]string{"1": "5"},
			since:     map[string]string{"1": unix(-24*time.Hour + time.Minute)},
			want:      map[int64]float64{1: 5},
			wantSince: map[int64]bool{1: false},
		},
		{
			name:      "history shorter than the window: Since is set",
			increase:  map[string]string{"1": "44"},
			since:     map[string]string{"1": unix(-4 * time.Hour)},
			want:      map[int64]float64{1: 44},
			wantSince: map[int64]bool{1: true},
		},
		{
			name:      "each node is judged on its own history",
			increase:  map[string]string{"1": "10", "2": "0"},
			since:     map[string]string{"1": unix(-48 * time.Hour), "2": unix(-2 * time.Hour)},
			want:      map[int64]float64{1: 10, 2: 0},
			wantSince: map[int64]bool{1: false, 2: true},
		},
		{
			name:      "no since data: value still returned, no Since",
			increase:  map[string]string{"1": "3"},
			want:      map[int64]float64{1: 3},
			wantSince: map[int64]bool{1: false},
		},
		{
			name: "no data at all: empty",
			want: map[int64]float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []string

			srv := fakePrometheus(t, tt.increase, tt.since, &seen)
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

			// The value must come from increase() (reset-aware), never from
			// subtracting an offset sample, which goes negative across a
			// counter reset.
			if len(seen) == 0 || !strings.HasPrefix(seen[0], `increase(am4_flights_departed_total{user_uuid="u"}[24h])`) {
				t.Fatalf("first query = %v, want an increase() over the 24h window", seen)
			}

			for _, q := range seen {
				if strings.Contains(q, " offset ") {
					t.Fatalf("query %q subtracts an offset sample, which breaks across counter resets", q)
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
