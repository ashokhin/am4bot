package config

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/creasty/defaults"
	"github.com/prometheus/common/promslog"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// Config holds the configuration settings for the bot.
type Config struct {
	// user-configurable fields
	//
	// Every field below also carries a json tag mirroring its yaml tag.
	// That's for internal/api's node-config endpoint and cmd/ambot's
	// matching alternate config source (a hosted node fetches its config
	// as JSON from apiserver instead of reading a local config.yaml) --
	// plain YAML config files are unaffected either way.
	Url      string `default:"https://www.airlinemanager.com/" yaml:"url" json:"url"`
	User     string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
	LogLevel string `default:"info" yaml:"log_level" json:"log_level"`
	// Parameters for Bot configuration
	BudgetPercent           BudgetType `yaml:"budget_percent" json:"budget_percent"`
	FuelPrice               Price      `yaml:"good_price" json:"good_price"`
	RepairLounges           bool       `default:"true" yaml:"repair_lounges" json:"repair_lounges"`
	BuyCateringIfMissing    bool       `default:"true" yaml:"buy_catering_if_missing" json:"buy_catering_if_missing"`
	CateringDurationHours   string     `default:"168" yaml:"catering_duration_hours" json:"catering_duration_hours"`
	CateringAmountOption    string     `default:"20000" yaml:"catering_amount_option" json:"catering_amount_option"`
	HubsMaintenanceLimit    int        `default:"5" yaml:"hubs_maintenance_limit" json:"hubs_maintenance_limit"`
	FuelCriticalPercent     float64    `default:"20" yaml:"fuel_critical_percent" json:"fuel_critical_percent"`
	AircraftWearPercent     string     `default:"80" yaml:"aircraft_wear_percent" json:"aircraft_wear_percent"`
	AircraftMaxHoursToCheck int        `default:"24" yaml:"aircraft_max_hours_to_check" json:"aircraft_max_hours_to_check"`
	AircraftModifyLimit     int        `default:"3" yaml:"aircraft_modify_limit" json:"aircraft_modify_limit"`
	// Cron schedules for the bot's run trigger. Each entry is a standard
	// 5-field cron expression (with the day-of-week field, this alone
	// covers "different start time on weekdays vs. weekends" — e.g.
	// ["0 8 * * 1-5", "0 10 * * 0,6"] — without any extra scheduling logic).
	// All entries share the same job; overlapping trigger times never run
	// concurrently — a run already in progress makes a newly triggered one
	// skip with a warning instead of racing the same Chrome session.
	CronSchedules []string `default:"[\"*/5 * * * *\"]" yaml:"cron_schedules" json:"cron_schedules"`
	// Upper bound, in seconds, of a random delay applied after each cron
	// trigger and before the run actually starts ("floating start"). Makes
	// login/action timestamps less mechanically regular — a bot that always
	// logs in at exactly 08:00:00 stands out in metrics far more than one
	// that logs in somewhere in 08:00:00-08:04:59. 0 (default) disables it.
	CronJitterSeconds int      `default:"0" yaml:"cron_jitter_seconds" json:"cron_jitter_seconds"`
	TimeoutSeconds    int      `default:"180" yaml:"timeout_seconds" json:"timeout_seconds"`
	Services          []string `default:"[\"company_stats\",\"alliance_stats\",\"staff_morale\",\"hubs\",\"claim_rewards\",\"buy_fuel\",\"marketing\",\"ac_maintenance\",\"depart\"]" yaml:"services" json:"services"`
	AllianceIDs       []string `yaml:"alliance_ids" json:"alliance_ids"`
	PrometheusAddress string   `default:":9150" yaml:"prometheus_address" json:"prometheus_address"`
	// PromslogConfig carries a *slog.LevelVar and other non-serializable
	// runtime state -- excluded from JSON (json:"-"); it's already outside
	// the YAML file too (no yaml tag), wired up separately in cmd/ambot.
	PromslogConfig *promslog.Config `json:"-"`
	// Parameters for Chrome/browser configuration
	ChromeHeadless bool `default:"true" yaml:"chrome_headless" json:"chrome_headless"`
	ChromeDebug    bool `default:"false" yaml:"chrome_debug" json:"chrome_debug"`
	// ChromeStealth switches the browser launch path from the plain
	// chromedp exec-allocator (visible flags, easy to attach a debugger to,
	// good for "where did the bot get stuck" local troubleshooting) to
	// chromedp-undetected (patches navigator.webdriver/CDP fingerprints,
	// launches headless via a real Xvfb display instead of Chrome's own
	// --headless flag). Off by default so a plain local/binary run behaves
	// exactly as before; hosted nodes should set this true.
	ChromeStealth bool `default:"false" yaml:"chrome_stealth" json:"chrome_stealth"`

	// internal fields
	passwordRunes []rune // most safe storage for password in memory
	confFilePath  string
	confModTime   time.Time
	// apiSrc is non-nil only for a Config built by NewFromAPI (a hosted
	// node fetching its config from apiserver instead of reading a local
	// file) -- see api_source.go. nil for every file-based Config (New).
	apiSrc *apiSource
}

// BudgetType holds budget percentage settings for various categories.
type BudgetType struct {
	Maintenance float64 `default:"30" yaml:"maintenance" json:"maintenance"`
	Marketing   float64 `default:"70" yaml:"marketing" json:"marketing"`
	Fuel        float64 `default:"70" yaml:"fuel" json:"fuel"`
}

// Price holds good price settings for fuel and CO2.
type Price struct {
	Fuel float64 `default:"500" yaml:"fuel" json:"fuel"`
	Co2  float64 `default:"120" yaml:"co2" json:"co2"`
}

// String returns a string representation of the Config struct.
func (c Config) String() string {
	return fmt.Sprint("{Url:", c.Url,
		", User:", utils.MaskUsername(c.User),
		", LogLevel:", c.LogLevel,
		", BudgetPercent:", c.BudgetPercent,
		", FuelPrice:", c.FuelPrice,
		", RepairLounges:", c.RepairLounges,
		", BuyCateringIfMissing:", c.BuyCateringIfMissing,
		", CateringDurationHours:", c.CateringDurationHours,
		", CateringAmountOption:", c.CateringAmountOption,
		", HubsMaintenanceLimit:", c.HubsMaintenanceLimit,
		", FuelCriticalPercent:", c.FuelCriticalPercent,
		", AircraftWearPercent:", c.AircraftWearPercent,
		", AircraftMaxHoursToCheck:", c.AircraftMaxHoursToCheck,
		", AircraftModifyLimit:", c.AircraftModifyLimit,
		", CronSchedules:", c.CronSchedules,
		", CronJitterSeconds:", c.CronJitterSeconds,
		", Services:", c.Services,
		", AllianceIDs:", c.AllianceIDs,
		", TimeoutSeconds:", c.TimeoutSeconds,
		", ChromeHeadless:", c.ChromeHeadless,
		", ChromeDebug:", c.ChromeDebug,
		", ChromeStealth:", c.ChromeStealth,
		", PrometheusAddress:", c.PrometheusAddress,
		"}")
}

// safeStorePassword converts password string into array of runes
// and clears the original string to reduce the risk of password leakage in memory.
func (c *Config) safeStorePassword() {
	c.passwordRunes = []rune(c.Password)
	c.Password = ""
}

// GetPassword is the getter for returning password as a string
func (c *Config) GetPassword() string {
	return string(c.passwordRunes)
}

// ReloadConfigIfChanged reloads the configuration from the YAML file
// if it has changed since the last load.
// It returns true if the configuration was reloaded, false otherwise.
func (c *Config) ReloadConfigIfChanged() (bool, error) {
	if c.apiSrc != nil {
		changed, err := c.loadFromAPI()
		if err != nil {
			slog.Debug("error reloading config from API", "error", err)

			return false, err
		}

		if changed {
			c.PromslogConfig.Level.Set(c.LogLevel)
			slog.Debug("config reloaded from API", "log_level", c.LogLevel)
		}

		return changed, nil
	}

	slog.Debug("reloading config file", "file", c.confFilePath)

	info, err := os.Stat(c.confFilePath)
	if err != nil {
		slog.Debug("error stating config file", "error", err)

		return false, err
	}

	newModTime := info.ModTime()

	if !newModTime.After(c.confModTime) {
		slog.Debug("config file unchanged, no reload needed")

		return false, nil
	}

	slog.Debug("config file changed, reloading", "old_mtime", c.confModTime, "new_mtime", newModTime)

	// save previous config before reload
	prevConfig := *c

	// load configuration file
	if err = c.loadConfig(); err != nil {
		// restore previous config in case of error
		*c = prevConfig

		slog.Debug("error reloading config, previous config has been restored", "error", err)

		return false, err
	}

	// set log level from config
	c.PromslogConfig.Level.Set(c.LogLevel)
	// update stored mtime
	c.confModTime = newModTime

	slog.Debug("config reloaded", "file", c.confFilePath, "new_mtime", c.confModTime, "log_level", c.LogLevel)

	return true, nil
}

// loadConfig loads the configuration from the YAML file specified in confFilePath.
// It unmarshals into a fresh Config so that keys removed from the file revert to
// their defaults instead of keeping stale values from a previous load.
func (c *Config) loadConfig() error {
	slog.Info("loading config file", "file", c.confFilePath)

	fresh := new(Config)
	// set default values on the fresh struct
	defaults.Set(fresh)

	// load YAML configuration
	if err := loadYaml(c.confFilePath, fresh); err != nil {
		slog.Debug("error loading config file", "error", err)

		return err
	}

	// preserve internal/runtime fields that are not part of the YAML file
	fresh.confFilePath = c.confFilePath
	fresh.confModTime = c.confModTime
	fresh.PromslogConfig = c.PromslogConfig

	*c = *fresh

	// securely store password
	c.safeStorePassword()

	slog.Debug("configuration loaded successfully", "file", c.confFilePath, "log_level", c.LogLevel)

	return nil
}

// validate checks that required configuration fields are present.
func (c *Config) validate() error {
	if c.Url == "" {
		return fmt.Errorf("config: url is required")
	}

	if c.User == "" {
		return fmt.Errorf("config: username is required")
	}

	if c.GetPassword() == "" {
		return fmt.Errorf("config: password is required")
	}

	if c.CronJitterSeconds < 0 {
		return fmt.Errorf("config: cron_jitter_seconds must be >= 0")
	}

	if len(c.CronSchedules) == 0 {
		return fmt.Errorf("config: cron_schedules must contain at least one entry")
	}

	for _, schedule := range c.CronSchedules {
		if _, err := cron.ParseStandard(schedule); err != nil {
			return fmt.Errorf("config: invalid cron_schedules entry %q: %w", schedule, err)
		}
	}

	return nil
}

// New creates a new Config instance and loading the configuration
// from the specified YAML file.
func New(filePath string) (*Config, error) {
	slog.Debug("creating new Config instance", "file", filePath)

	var err error

	// create new Config instance
	c := new(Config)
	c.confFilePath = filePath

	info, err := os.Stat(filePath)
	if err != nil {
		slog.Debug("error stating config file", "error", err)

		return nil, err
	}

	c.confModTime = info.ModTime()

	// load configuration
	if err := c.loadConfig(); err != nil {
		return nil, err
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	slog.Debug("config loaded", "file", c.confFilePath, "mod_time", c.confModTime)

	return c, nil
}

// loadYaml reads a YAML file from the specified path
// and unmarshals its content into the provided output structure.
func loadYaml(filePath string, out any) error {
	var err error
	var f []byte

	slog.Debug("read file", "file", filePath)

	if f, err = os.ReadFile(filePath); err != nil {
		return err
	}

	slog.Debug("load file as yaml", "file", filePath)

	if err := yaml.Unmarshal(f, out); err != nil {
		return err
	}

	return err
}
