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
	// Minimum runway length "route_scanner" requires when searching routes
	// for the configured HubsList/aircraft.
	HubMinRunwayLengthFt int `default:"9680" yaml:"hub_min_runway_length_ft"`
	ScanStepKm           int `default:"100" yaml:"scan_step_km"`
	// Parameters for "full_catalog_scanner" / "catalog_codes_scanner" scan types
	CatalogDBPath   string `default:"am4_catalog.db" yaml:"catalog_db_path"`
	CatalogCodeType string `default:"iata" yaml:"catalog_code_type"`
	// Lowest "min. runway" value "full_catalog_scanner" queries with — both
	// for its base (unfiltered) query and as the start of the runway sweep
	// (see CatalogRunwayStepFt). Must be >= 1: the game's search form breaks
	// with an empty/0 value. Raise this above 1 only once you've confirmed
	// (e.g. via CatalogDisableRunwaySweep test runs) that no real airport in
	// the game has a shorter runway than the value you pick — it skips real
	// queries, not just empty ones.
	CatalogMinRunwayLengthFt int `default:"1" yaml:"catalog_min_runway_length_ft"`
	// Highest "min. runway" threshold "full_catalog_scanner" will try when a
	// distance window comes back saturated (see CatalogRunwayStepFt).
	CatalogMaxRunwayLengthFt int `default:"20000" yaml:"catalog_max_runway_length_ft"`
	// Step between runway thresholds "full_catalog_scanner" sweeps through
	// (CatalogMinRunwayLengthFt up to CatalogMaxRunwayLengthFt) when a
	// distance window's unfiltered query returns exactly 50 results — the
	// game's per-query cap — since that many results at one distance can
	// itself hide routes behind the cap.
	CatalogRunwayStepFt int `default:"500" yaml:"catalog_runway_step_ft"`
	// Testing/debugging aid: if true, "full_catalog_scanner" never sweeps
	// runway thresholds for a saturated window — useful for comparing route
	// counts with vs. without the runway sweep.
	CatalogDisableRunwaySweep bool `default:"false" yaml:"catalog_disable_runway_sweep"`
	// Testing/debugging aid: if true, "full_catalog_scanner" ignores both
	// early-exit optimizations (distance-level in scanCatalogAirport,
	// runway-level in scanCatalogDistanceByRunway) and always sweeps the
	// full configured range regardless of whether a window came back
	// unsaturated. Used to validate that trusting "unsaturated at X means
	// everything below X is known" doesn't actually lose routes — compare
	// route counts with vs. without this flag on the same airport.
	CatalogDisableEarlyExit bool `default:"false" yaml:"catalog_disable_early_exit"`
	// Testing/debugging aid: if non-empty, "full_catalog_scanner" and
	// "catalog_codes_scanner" only scan these airport IDs (the numeric game
	// ID, e.g. from citySelect/dep/arr) instead of every airport in the game.
	// Airports outside this list are left untouched — a later unrestricted
	// run still covers them normally.
	CatalogAirportIDs []int `yaml:"catalog_airport_ids"`
	// Sharding for running multiple scanner instances in parallel against
	// disjoint slices of the airport ID space, each with its own
	// CatalogDBPath — merge the resulting files afterward. Airport IDs
	// aren't assigned by geography, so a shard still walks every country to
	// find which of its airports fall in [CatalogAirportIDMin,
	// CatalogAirportIDMax], but only actually scans those. 0/0 (the
	// zero-value default) means "no range restriction" — every airport ID
	// is in range. Independent of CatalogAirportIDs; if both are set, an
	// airport is scanned only when it satisfies both.
	CatalogAirportIDMin int `default:"0" yaml:"catalog_airport_id_min"`
	CatalogAirportIDMax int `default:"0" yaml:"catalog_airport_id_max"`
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
	Maintenance float64 `default:"30" yaml:"maintenance"`
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
		", HubMinRunwayLengthFt:", c.HubMinRunwayLengthFt,
		", ScanStepKm:", c.ScanStepKm,
		", CatalogDBPath:", c.CatalogDBPath,
		", CatalogCodeType:", c.CatalogCodeType,
		", CatalogMinRunwayLengthFt:", c.CatalogMinRunwayLengthFt,
		", CatalogMaxRunwayLengthFt:", c.CatalogMaxRunwayLengthFt,
		", CatalogRunwayStepFt:", c.CatalogRunwayStepFt,
		", CatalogDisableRunwaySweep:", c.CatalogDisableRunwaySweep,
		", CatalogDisableEarlyExit:", c.CatalogDisableEarlyExit,
		", CatalogAirportIDs:", c.CatalogAirportIDs,
		", CatalogAirportIDMin:", c.CatalogAirportIDMin,
		", CatalogAirportIDMax:", c.CatalogAirportIDMax,
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

	if c.CatalogMinRunwayLengthFt < 1 {
		return fmt.Errorf("config: catalog_min_runway_length_ft must be >= 1 (0 breaks the game's search form)")
	}

	if c.CatalogAirportIDMax > 0 && c.CatalogAirportIDMin > c.CatalogAirportIDMax {
		return fmt.Errorf("config: catalog_airport_id_min (%d) must be <= catalog_airport_id_max (%d)",
			c.CatalogAirportIDMin, c.CatalogAirportIDMax)
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
