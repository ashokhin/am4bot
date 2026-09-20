package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/ashokhin/am4bot/internal/bot"
	"github.com/ashokhin/am4bot/internal/config"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	versionCollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/robfig/cron/v3"
)

const (
	APP_NAME             string = "ambot"
	EXPORTER_NAME        string = "ambot_exporter"
	EXPORTER_NAMESPACE   string = "am4"
	MAX_RESTORE_ATTEMPTS int    = 5
)

var (
	configFile   = kingpin.Flag("app.config", "YAML file with configuration.").Short('c').Default("config.yaml").String()
	webAddr      = kingpin.Flag("web.listen-address", "Addresses on which to expose metrics and web interface.").Default(":9150").String()
	webTelemetry = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()

	// Alternate config source for nodes hosted through the multi-tenant
	// control plane: fetch config from apiserver's internal endpoint
	// instead of reading --app.config. Standalone/OSS users leave these
	// unset and nothing changes. All three are required together when any
	// one is set (checked in main, since kingpin has no "these three go
	// together" constraint).
	configAPIURL = kingpin.Flag("config-api-url", "Base URL of apiserver's internal config endpoint (e.g. http://apiserver:8080). If set, config is fetched from there (using --node-id and --node-token) instead of --app.config.").
			Envar("CONFIG_API_URL").String()
	nodeID = kingpin.Flag("node-id", "This node's id in the control plane, used with --config-api-url.").
		Envar("NODE_ID").Int64()
	nodeToken = kingpin.Flag("node-token", "Bearer token authenticating as --node-id. Prefer the NODE_TOKEN environment variable over this flag so it doesn't appear in `ps`.").
			Envar("NODE_TOKEN").String()
)

func main() {
	var err error
	var conf *config.Config

	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print(APP_NAME))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	// Log to stdout (promslog defaults to stderr). Under the journald log
	// driver, stderr is recorded at PRIORITY=err, which makes journalctl
	// render every ordinary "level=info" line as an error. stdout lands at
	// PRIORITY=info instead.
	promslogConfig.Writer = os.Stdout

	logger := promslog.New(promslogConfig)
	slog.SetDefault(logger)

	slog.Info(fmt.Sprintf("starting application %s", APP_NAME), "version", version.Info())
	slog.Info("build context", "build_context", version.BuildContext())

	// load configuration: from apiserver if --config-api-url (and its
	// companions) are set, otherwise the usual local YAML file.
	switch {
	case *configAPIURL != "":
		if *nodeID == 0 || *nodeToken == "" {
			slog.Error("config loading error", "error", "--node-id and --node-token are required together with --config-api-url")

			return
		}

		conf, err = config.NewFromAPI(*configAPIURL, *nodeID, *nodeToken)
	default:
		var confPath string
		confPath, err = filepath.Abs(*configFile)

		if err == nil {
			conf, err = config.New(confPath)
		}
	}

	if err != nil {
		slog.Error("config loading error", "error", err)

		return
	}

	conf.PromslogConfig = promslogConfig

	// The CLI's "log.level" and config's "log_level" by default are both "info"
	// if they are not -- check further
	if conf.PromslogConfig.Level.String() != conf.LogLevel {
		// If CLI's "log.level" is not default (info) then prioritize CLI's value
		if conf.PromslogConfig.Level.String() != "info" {
			slog.Info("set log level from CLI", "log.level", conf.PromslogConfig.Level.String())

		} else { // else - set "log_level" from config
			slog.Info("set log level from config", "log_level", conf.LogLevel)

			conf.PromslogConfig.Level.Set(conf.LogLevel)
		}
	}

	// The CLI's "web.listen-address" and config's "prometheus_address" by default are both ":9150"
	// if they are not -- check further
	if *webAddr != conf.PrometheusAddress {
		// If CLI's "web.listen-address" is not default (:9150) then prioritize CLI's value
		if *webAddr != ":9150" {
			slog.Info("set Prometheus address from CLI", "address", *webAddr)

		} else { // else - set "prometheus_address" from config
			slog.Info("set Prometheus address from config", "address", conf.PrometheusAddress)

			*webAddr = conf.PrometheusAddress
		}
	}

	// create Prometheus registry
	prometheusRegistry := prometheus.NewRegistry()
	prometheusRegistry.MustRegister(versionCollector.NewCollector(APP_NAME))
	prometheusRegistry.MustRegister(collectors.NewGoCollector())

	// create Bot object with loaded configuration
	bot := bot.New(conf, prometheusRegistry)

	// cancel the context on SIGINT/SIGTERM for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// start it once in the blocking mode (not inside a goroutine)
	// for collecting initial Prometheus metrics
	if err := bot.Run(ctx); err != nil {
		slog.Warn("error in Bot.Run", "error", err)

		bot.PrometheusMetrics.Up.Set(0)
	} else {
		bot.PrometheusMetrics.Up.Set(1)
	}

	// now start it inside "cronjob" (goroutine with schedule)
	// create cron object
	c := cron.New()
	// add counter for restore attempts after error
	restoreAttemptsCount := 0
	// runMu serializes actual bot runs: with multiple schedules and/or
	// jitter, two triggers can land close enough to overlap. A run already
	// in progress makes a newly triggered one skip rather than race the
	// same Chrome session.
	var runMu sync.Mutex
	// entryIDs holds one entry per configured schedule, used below to log
	// the earliest upcoming run across all of them.
	entryIDs := make([]cron.EntryID, 0, len(bot.Conf.CronSchedules))

	// runJob is shared by every schedule entry. It applies the configured
	// "floating start" jitter, then runs the bot -- skipping (rather than
	// blocking) if a previous run is still in progress.
	runJob := func() {
		if bot.Conf.CronJitterSeconds > 0 {
			delay := time.Duration(rand.IntN(bot.Conf.CronJitterSeconds+1)) * time.Second
			slog.Info("floating start delay", "delay", delay)

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}

		if !runMu.TryLock() {
			slog.Warn("previous run still in progress, skipping this trigger")

			return
		}
		defer runMu.Unlock()

		slog.Info("start job", "start_time", time.Now().UTC())

		nextRun := nextScheduledRun(c, entryIDs)
		bot.PrometheusMetrics.NextScheduledRunTimestampSeconds.Set(float64(nextRun.Unix()))

		if err := bot.Run(ctx); err != nil {
			// failed run increases counter
			restoreAttemptsCount++
			bot.PrometheusMetrics.Up.Set(0)

			slog.Error("error in Bot.Run", "restore_attempts_count", restoreAttemptsCount, "max_attempts", MAX_RESTORE_ATTEMPTS, "error", err)

			if restoreAttemptsCount >= MAX_RESTORE_ATTEMPTS {
				slog.Error("max restore attempts count has been reached. Exit.")

				os.Exit(1)
			}

			slog.Error("job has been failed", "end_time", time.Now().UTC(), "next_run", nextRun)
		} else {
			// successful run resets counter
			restoreAttemptsCount = 0
			bot.PrometheusMetrics.Up.Set(1)

			slog.Info("job has been done", "end_time", time.Now().UTC(), "next_run", nextRun)
		}
	}

	// register the shared job against every configured schedule
	for _, schedule := range bot.Conf.CronSchedules {
		entryID, err := c.AddFunc(schedule, runJob)
		if err != nil {
			slog.Error("error scheduling cron job", "schedule", schedule, "error", err)

			os.Exit(1)
		}

		entryIDs = append(entryIDs, entryID)
	}

	// start cron object, schedule jobs
	c.Start()

	// Set once here for the window before the FIRST trigger fires --
	// runJob itself keeps it current on every trigger after that (see
	// above). Without this, the metric would read 0 (an unset Gauge's
	// zero value) until the first scheduled run, which a naive alert
	// rule could misread as "already overdue".
	initialNextRun := nextScheduledRun(c, entryIDs)
	bot.PrometheusMetrics.NextScheduledRunTimestampSeconds.Set(float64(initialNextRun.Unix()))

	slog.Info("job scheduled", "schedules", bot.Conf.CronSchedules, "jitter_seconds", bot.Conf.CronJitterSeconds,
		"next_run", initialNextRun)

	// create and register handler for the webTelemetry page
	handler := promhttp.HandlerFor(
		prometheusRegistry,
		promhttp.HandlerOpts{
			Registry: prometheusRegistry,
		})

	http.Handle(*webTelemetry, handler)

	// create and register handler for the root page
	// for displaying version and redirecting to the webTelemetry page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>AM4Bot Exporter</title></head>
			<body>
			<h1>AM4Bot Exporter</h1>
			<p>` + version.Info() + `</p>
			<p>` + version.BuildContext() + `</p>
			<p>For Prometheus scraping use the metrics endpoint:</p>
			<p><a href="` + *webTelemetry + `">` + *webTelemetry + `</a></p>
			</body>
			</html>`))
	})

	slog.Info(fmt.Sprintf("starting Prometheus exporter %s", EXPORTER_NAME), "address", *webAddr, "location", *webTelemetry)

	// configure HTTP server with sane timeouts for Prometheus scraping
	srv := &http.Server{
		Addr:              *webAddr,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// start HTTP server in a goroutine so we can wait for shutdown signals
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("error in http server", "error", err)

			os.Exit(1)
		}
	}()

	// block until a shutdown signal cancels the context
	<-ctx.Done()
	slog.Info("shutdown signal received, stopping")

	// stop the cron scheduler from starting new jobs
	cronCtx := c.Stop()
	<-cronCtx.Done()

	// gracefully shut down the HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("error during HTTP server shutdown", "error", err)
	}

	slog.Info("shutdown complete")
}

// nextScheduledRun returns the earliest upcoming run time across all given
// cron entries (relevant once multiple schedules are configured).
func nextScheduledRun(c *cron.Cron, entryIDs []cron.EntryID) time.Time {
	var next time.Time

	for _, id := range entryIDs {
		entryNext := c.Entry(id).Next
		if entryNext.IsZero() {
			continue
		}

		if next.IsZero() || entryNext.Before(next) {
			next = entryNext
		}
	}

	return next
}
