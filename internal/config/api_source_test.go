package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/common/promslog"
)

func testConfigJSON(t *testing.T) []byte {
	t.Helper()

	var cfg Config
	cfg.Url = "https://www.airlinemanager.com/"
	cfg.User = "player1"
	cfg.Password = "gamepass1"
	cfg.CronSchedules = []string{"*/10 * * * *"}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshaling test config: %v", err)
	}

	return data
}

func TestNewFromAPI(t *testing.T) {
	body := testConfigJSON(t)

	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")

		if r.URL.Path != "/internal/nodes/42/config" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	c, err := NewFromAPI(srv.URL, 42, "my-node-token")
	if err != nil {
		t.Fatalf("NewFromAPI() error = %v", err)
	}

	if gotAuth != "Bearer my-node-token" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer my-node-token")
	}
	if c.User != "player1" {
		t.Fatalf("User = %q, want player1", c.User)
	}
	if c.GetPassword() != "gamepass1" {
		t.Fatalf("GetPassword() = %q, want gamepass1", c.GetPassword())
	}
	if c.Password != "" {
		t.Fatalf("Password = %q, want empty (safeStorePassword should have cleared it)", c.Password)
	}
}

func TestNewFromAPIRejectsNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid node or token"}`))
	}))
	defer srv.Close()

	if _, err := NewFromAPI(srv.URL, 42, "wrong-token"); err == nil {
		t.Fatal("NewFromAPI() with a 401 response succeeded, want error")
	}
}

func TestReloadConfigIfChangedFromAPIDetectsNoChange(t *testing.T) {
	body := testConfigJSON(t)
	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(body)
	}))
	defer srv.Close()

	c, err := NewFromAPI(srv.URL, 1, "token")
	if err != nil {
		t.Fatalf("NewFromAPI() error = %v", err)
	}
	c.PromslogConfig = &promslog.Config{Level: promslog.NewLevel()}

	changed, err := c.ReloadConfigIfChanged()
	if err != nil {
		t.Fatalf("ReloadConfigIfChanged() error = %v", err)
	}
	if changed {
		t.Fatal("ReloadConfigIfChanged() = true for byte-identical config, want false")
	}
	if requests != 2 { // 1 from NewFromAPI, 1 from ReloadConfigIfChanged
		t.Fatalf("server received %d requests, want 2", requests)
	}
}

func TestReloadConfigIfChangedFromAPIDetectsChange(t *testing.T) {
	first := testConfigJSON(t)

	var second Config
	if err := json.Unmarshal(first, &second); err != nil {
		t.Fatalf("unmarshaling test config: %v", err)
	}
	second.CronJitterSeconds = 999
	secondBody, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshaling second config: %v", err)
	}

	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			w.Write(first)
		} else {
			w.Write(secondBody)
		}
	}))
	defer srv.Close()

	c, err := NewFromAPI(srv.URL, 1, "token")
	if err != nil {
		t.Fatalf("NewFromAPI() error = %v", err)
	}
	c.PromslogConfig = &promslog.Config{Level: promslog.NewLevel()}

	changed, err := c.ReloadConfigIfChanged()
	if err != nil {
		t.Fatalf("ReloadConfigIfChanged() error = %v", err)
	}
	if !changed {
		t.Fatal("ReloadConfigIfChanged() = false after the served config changed, want true")
	}
	if c.CronJitterSeconds != 999 {
		t.Fatalf("CronJitterSeconds = %d after reload, want 999", c.CronJitterSeconds)
	}
}
