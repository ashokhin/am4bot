package config

import (
	"encoding/json"
	"testing"

	"github.com/creasty/defaults"
)

func validConfig() *Config {
	c := &Config{
		Url:           "https://www.airlinemanager.com/",
		User:          "user@example.com",
		CronSchedules: []string{"*/5 * * * *"},
	}
	c.passwordRunes = []rune("secret")

	return c
}

func TestValidateRejectsBadCronSchedulesEntry(t *testing.T) {
	c := validConfig()
	c.CronSchedules = []string{"0 8 * * 1-5", "not a cron expression"}

	if err := c.validate(); err == nil {
		t.Fatal("validate() = nil, want error for invalid cron_schedules entry")
	}
}

func TestValidateAcceptsWeekdayWeekendSchedules(t *testing.T) {
	c := validConfig()
	c.CronSchedules = []string{"0 8 * * 1-5", "0 10 * * 0,6"}

	if err := c.validate(); err != nil {
		t.Fatalf("validate() error = %v, want nil", err)
	}
}

func TestValidateRejectsNegativeJitter(t *testing.T) {
	c := validConfig()
	c.CronJitterSeconds = -1

	if err := c.validate(); err == nil {
		t.Fatal("validate() = nil, want error for negative cron_jitter_seconds")
	}
}

func TestValidateRejectsEmptyCronSchedules(t *testing.T) {
	c := validConfig()
	c.CronSchedules = nil

	if err := c.validate(); err == nil {
		t.Fatal("validate() = nil, want error for empty cron_schedules")
	}
}

// TestJSONRoundTrip locks in the contract internal/api's node-config
// endpoint and cmd/ambot's matching alternate config source depend on:
// a Config built directly in Go (not via New()/loadConfig(), so
// safeStorePassword never runs and Password stays populated) marshals to
// JSON with every field intact, and unmarshals back losslessly. Password
// staying present is deliberate here -- the whole point of the endpoint
// is to hand ambot a usable, populated Config.
func TestJSONRoundTrip(t *testing.T) {
	c := Config{
		Url:               "https://www.airlinemanager.com/",
		User:              "player1",
		Password:          "supersecret",
		CronSchedules:     []string{"0 8 * * 1-5", "0 10 * * 0,6"},
		CronJitterSeconds: 300,
		Services:          []string{"buy_fuel", "marketing", "depart"},
		BudgetPercent:     BudgetType{Maintenance: 40, Marketing: 60, Fuel: 80},
		FuelPrice:         Price{Fuel: 700, Co2: 130},
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.Password != c.Password {
		t.Fatalf("Password = %q after round-trip, want %q", got.Password, c.Password)
	}
	if len(got.Services) != 3 || got.Services[0] != "buy_fuel" || got.Services[1] != "marketing" || got.Services[2] != "depart" {
		t.Fatalf("Services = %v after round-trip, want order preserved: %v", got.Services, c.Services)
	}
	if len(got.CronSchedules) != 2 || got.CronSchedules[0] != c.CronSchedules[0] {
		t.Fatalf("CronSchedules = %v after round-trip, want %v", got.CronSchedules, c.CronSchedules)
	}
	if got.CronJitterSeconds != c.CronJitterSeconds {
		t.Fatalf("CronJitterSeconds = %d, want %d", got.CronJitterSeconds, c.CronJitterSeconds)
	}
	if got.BudgetPercent != c.BudgetPercent {
		t.Fatalf("BudgetPercent = %+v, want %+v", got.BudgetPercent, c.BudgetPercent)
	}
	if got.FuelPrice != c.FuelPrice {
		t.Fatalf("FuelPrice = %+v, want %+v", got.FuelPrice, c.FuelPrice)
	}
}

// TestJSONRoundTripAppliesDefaultsThenOverlay mirrors exactly how
// internal/api will build a node's effective config: start from
// defaults.Set, override the node's dedicated columns, then unmarshal
// extra_config JSON on top so it only overrides what it explicitly sets.
func TestJSONRoundTripAppliesDefaultsThenOverlay(t *testing.T) {
	var c Config
	if err := defaults.Set(&c); err != nil {
		t.Fatalf("defaults.Set() error = %v", err)
	}

	c.Url = "https://www.airlinemanager.com/"
	c.User = "player1"
	c.Password = "supersecret"
	c.CronSchedules = []string{"*/10 * * * *"}

	// extra_config only mentions fuel price -- everything else must
	// survive from the defaults/dedicated-column pass untouched.
	extraConfig := []byte(`{"good_price":{"fuel":700}}`)
	if err := json.Unmarshal(extraConfig, &c); err != nil {
		t.Fatalf("Unmarshal(extra_config) error = %v", err)
	}

	if c.FuelPrice.Fuel != 700 {
		t.Fatalf("FuelPrice.Fuel = %v after overlay, want 700", c.FuelPrice.Fuel)
	}
	if c.FuelPrice.Co2 != 120 {
		t.Fatalf("FuelPrice.Co2 = %v after overlay, want untouched default 120", c.FuelPrice.Co2)
	}
	if c.User != "player1" {
		t.Fatalf("User = %q after overlay, want untouched %q", c.User, "player1")
	}
	if c.HubsMaintenanceLimit != 5 {
		t.Fatalf("HubsMaintenanceLimit = %d after overlay, want untouched default 5", c.HubsMaintenanceLimit)
	}
}
