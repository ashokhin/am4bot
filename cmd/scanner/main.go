package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ashokhin/am4bot/internal/bot"
	"github.com/ashokhin/am4bot/internal/config"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/common/version"
	"github.com/schollz/progressbar/v3"
)

const (
	APP_NAME string = "scanner"
)

var (
	configFile = kingpin.Flag("app.config", "YAML file with configuration.").
			Short('c').
			Default("config.yaml").
			String()
	logLevel = kingpin.Flag("log.level", "Only log messages with the given severity or above. One of: [debug, info, warn, error]").
			Default("info").
			Enum("debug", "info", "warn", "error")
)

func SetLogLevel() slog.Level {

	switch strings.ToLower(*logLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func calcValuesForProgress(config *config.Config) int {
	var totalValues int
	scansPerHub := ((config.MaxRouteDistanceKm - config.MinRouteDistanceKm) / config.ScanStepKm)
	totalValues = scansPerHub * len(config.HubsList)

	return totalValues
}

func main() {
	var err error
	var conf *config.Config

	kingpin.Version(version.Print(APP_NAME))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	handlerOpts := &slog.HandlerOptions{
		Level:     SetLogLevel(),
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				// Get the source struct
				source, ok := a.Value.Any().(*slog.Source)
				if ok && source != nil {
					// Replace the full path with the base filename
					source.File = filepath.Base(source.File)
				}
			}
			return a
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, handlerOpts))
	slog.SetDefault(logger)

	// load configuration
	confPath, _ := filepath.Abs(*configFile)

	if conf, err = config.New(confPath); err != nil {
		slog.Error("config loading error", "error", err)

		return
	}

	bot := bot.NewScanner(conf)
	ctx, cancel := context.WithCancel(context.Background())
	timeStart := time.Now()

	switch conf.ScanType {
	case "route_scanner":
		if err := scanRoutes(ctx, bot); err != nil {
			slog.Error("error in main > scanRoutes", "error", err)

			return
		}
	case "airport_scanner":
		if err := scanAirports(ctx, bot); err != nil {
			slog.Error("error in main > scanAirports", "error", err)

			return
		}
	default:
		slog.Warn("invalid scan type specified in config", "scan_type", conf.ScanType)
	}

	cancel()
	duration := time.Since(timeStart)

	slog.Info("run complete", "elapsed_time", fmt.Sprint(duration))
	slog.Info("application finished")
}

func scanAirports(ctx context.Context, bot bot.Bot) error {
	if err := bot.ScanAirports(ctx); err != nil {
		slog.Warn("error in main > bot.ScanAirports", "error", err)

		return err
	}

	return nil
}

func scanRoutes(ctx context.Context, bot bot.Bot) error {
	// calc values for progress bar
	totalValues := calcValuesForProgress(bot.Conf)
	slog.Debug("calc progress values", "totalPrCount", totalValues)
	bar := progressbar.NewOptions(totalValues,
		progressbar.OptionSetWidth(40),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionShowElapsedTimeOnFinish(),
	)

	go func() {
		curPrCount := 0
		for range bot.ProgressChan {
			bar.Add(1)
			curPrCount++
			slog.Debug("curPrCount increased", "current_value", curPrCount)
		}
		bar.Finish()
	}()

	defer close(bot.ProgressChan)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := bot.ScanRoutes(ctx); err != nil {
		slog.Error("bot run error", "error", err)

		return err
	}

	return nil
}
