package config

import "testing"

func validConfig() *Config {
	c := &Config{
		Url:                      "https://www.airlinemanager.com/",
		User:                     "user@example.com",
		CronSchedules:            []string{"*/5 * * * *"},
		CatalogMinRunwayLengthFt: 1,
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
