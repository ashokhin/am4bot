package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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
)

func main() {
	var err error
	var conf *config.Config

	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print(APP_NAME))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promslog.New(promslogConfig)
	slog.SetDefault(logger)

	slog.Info(fmt.Sprintf("starting application %s", APP_NAME), "version", version.Info())
	slog.Info("build context", "build_context", version.BuildContext())

	// load configuration
	confPath, _ := filepath.Abs(*configFile)

	if conf, err = config.New(confPath); err != nil {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	// create cron job with schedule from configuration
	c.AddFunc(bot.Conf.CronSchedule, func() {
		slog.Info("start job", "start_time", time.Now().UTC())

		if err := bot.Run(ctx); err != nil {
			// failed run increases counter
			restoreAttemptsCount++
			bot.PrometheusMetrics.Up.Set(0)

			slog.Error("error in Bot.Run", "restore_attempts_count", restoreAttemptsCount, "max_attempts", MAX_RESTORE_ATTEMPTS, "error", err)

			if restoreAttemptsCount >= MAX_RESTORE_ATTEMPTS {
				slog.Error("max restore attempts count has been reached. Exit.")

				os.Exit(1)
			}

			slog.Error("job has been failed", "end_time", time.Now().UTC(), "next_run", c.Entry(1).Next.UTC())
		} else {
			// successful run resets counter
			restoreAttemptsCount = 0
			bot.PrometheusMetrics.Up.Set(1)

			slog.Info("job has been done", "end_time", time.Now().UTC(), "next_run", c.Entry(1).Next.UTC())
		}

	})

	// start cron object, schedule jobs
	c.Start()

	slog.Info("job scheduled", "next_run", c.Entry(1).Next.UTC())

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

	// start HTTP server for Prometheus scraping
	if err := http.ListenAndServe(*webAddr, nil); err != nil {
		slog.Error("error in http server", "error", err)

		os.Exit(1)
	}
}
