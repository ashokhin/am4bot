package config

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/creasty/defaults"
	"github.com/prometheus/common/promslog"
	"gopkg.in/yaml.v3"
)

// Config holds the configuration settings for the bot.
type Config struct {
	// user-configurable fields
	Url      string `default:"https://www.airlinemanager.com/" yaml:"url"`
	User     string `yaml:"username"`
	Password string `yaml:"password"`
	LogLevel string `default:"info" yaml:"log_level"`
	// Parameters for Bot configuration
	BudgetPercent           BudgetType `yaml:"budget_percent"`
	FuelPrice               Price      `yaml:"good_price"`
	RepairLounges           bool       `default:"true" yaml:"repair_lounges"`
	BuyCateringIfMissing    bool       `default:"true" yaml:"buy_catering_if_missing"`
	CateringDurationHours   string     `default:"168" yaml:"catering_duration_hours"`
	CateringAmountOption    string     `default:"20000" yaml:"catering_amount_option"`
	HubsMaintenanceLimit    int        `default:"5" yaml:"hubs_maintenance_limit"`
	FuelCriticalPercent     float64    `default:"20" yaml:"fuel_critical_percent"`
	AircraftWearPercent     string     `default:"80" yaml:"aircraft_wear_percent"`
	AircraftMaxHoursToCheck int        `default:"24" yaml:"aircraft_max_hours_to_check"`
	AircraftModifyLimit     int        `default:"3" yaml:"aircraft_modify_limit"`
	CronSchedule            string     `default:"*/5 * * * *" yaml:"cron_schedule"`
	TimeoutSeconds          int        `default:"180" yaml:"timeout_seconds"`
	Services                []string   `default:"[\"company_stats\",\"alliance_stats\",\"staff_morale\",\"hubs\",\"claim_rewards\",\"buy_fuel\",\"marketing\",\"ac_maintenance\",\"depart\"]" yaml:"services"`
	AllianceIDs             []string   `yaml:"alliance_ids"`
	PrometheusAddress       string     `default:":9150" yaml:"prometheus_address"`
	PromslogConfig          *promslog.Config
	// Parameters for Scanner configuration
	ScanType           string   `default:"route_scanner" yaml:"scan_type"`
	HubsList           []string `yaml:"hubs_list"`
	MaxRouteDistanceKm int      `default:"14500" yaml:"max_route_range_km"`
	MinRouteDistanceKm int      `default:"6500" yaml:"min_route_range_km"`
	MinRunwayLength    int      `default:"9680" yaml:"min_runway_length"`
	ScanStepKm         int      `default:"100" yaml:"scan_step_km"`
	// Parameters for "full_catalog_scanner" / "catalog_codes_scanner" scan types
	CatalogDBPath   string `default:"am4_catalog.db" yaml:"catalog_db_path"`
	CatalogCodeType string `default:"iata" yaml:"catalog_code_type"`
	// Parameters for both Bot and Scanner configuration
	ChromeHeadless bool `default:"true" yaml:"chrome_headless"`
	ChromeDebug    bool `default:"false" yaml:"chrome_debug"`

	// internal fields
	passwordRunes []rune // most safe storage for password in memory
	confFilePath  string
	confModTime   time.Time
}

// BudgetType holds budget percentage settings for various categories.
type BudgetType struct {
	Maintenance float64 `default:"50" yaml:"maintenance"`
	Marketing   float64 `default:"70" yaml:"marketing"`
	Fuel        float64 `default:"70" yaml:"fuel"`
}

// Price holds good price settings for fuel and CO2.
type Price struct {
	Fuel float64 `default:"500" yaml:"fuel"`
	Co2  float64 `default:"120" yaml:"co2"`
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
		", CronSchedule:", c.CronSchedule,
		", Services:", c.Services,
		", AllianceIDs:", c.AllianceIDs,
		", TimeoutSeconds:", c.TimeoutSeconds,
		", ChromeHeadless:", c.ChromeHeadless,
		", ChromeDebug:", c.ChromeDebug,
		", PrometheusAddress:", c.PrometheusAddress,
		", ScanType:", c.ScanType,
		", HubsList:", c.HubsList,
		", MaxRouteDistanceKm:", c.MaxRouteDistanceKm,
		", MinRouteDistanceKm:", c.MinRouteDistanceKm,
		", MinRunwayLength:", c.MinRunwayLength,
		", ScanStepKm:", c.ScanStepKm,
		", CatalogDBPath:", c.CatalogDBPath,
		", CatalogCodeType:", c.CatalogCodeType,
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

	slog.Debug("config reloaded", "config", c)

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

	slog.Debug("configuration loaded successfully", "config", c)

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

	slog.Debug("config loaded", "config", c)

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
