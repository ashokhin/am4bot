package bot

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ashokhin/am4bot/internal/config"
	"github.com/ashokhin/am4bot/internal/io"
	"github.com/ashokhin/am4bot/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// Bot represents the automation bot with its configuration and state.
type Bot struct {
	Conf              *config.Config
	chromeOpts        []chromedp.ExecAllocatorOption
	AccountBalance    float64
	BudgetMoney       BudgetType
	PrometheusMetrics metrics.Metrics
	Writer            *io.Writer
	ProgressChan      chan struct{}
	HasValidCookies   bool
}

// Budget defines the budget allocations for different categories.
type BudgetType struct {
	Maintenance float64
	Marketing   float64
	Fuel        float64
}

// New creates a new Bot instance with the provided configuration and Prometheus registry.
func New(conf *config.Config, registry *prometheus.Registry) Bot {
	metrics := metrics.New()
	metrics.RegisterMetrics(registry)
	metrics.StartTimeSeconds.SetToCurrentTime()

	// Setup Chrome options
	opts := setupChromeOptions(conf)

	return Bot{
		Conf:              conf,
		chromeOpts:        opts,
		PrometheusMetrics: *metrics,
	}
}

// deductBudget subtracts amount from both AccountBalance and the given budget category.
func (b *Bot) deductBudget(amount float64, category *float64) {
	b.AccountBalance -= amount
	*category -= amount
}

// ReloadBotConfig reloads the bot's configuration and updates relevant settings.
func (b *Bot) ReloadBotConfig() error {

	slog.Info("reloading Bot configuration")
	// Setup Chrome options
	b.chromeOpts = setupChromeOptions(b.Conf)

	return nil
}

// Run executes the bot's main workflow, including authentication and service tasks.
func (b *Bot) Run(ctx context.Context) error {
	// reload config if changed
	confChanged, err := b.Conf.ReloadConfigIfChanged()
	if err != nil {
		slog.Error("error reloading config", "error", err)
	}

	// if config changed, update Chrome options
	if confChanged {
		slog.Info("updating Bot options due to config change")
		b.ReloadBotConfig()
	}

	timeStart := time.Now()

	slog.Debug("create context with timeout", "timeout_seconds", b.Conf.TimeoutSeconds)

	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, time.Duration(b.Conf.TimeoutSeconds)*time.Second)
	defer cancelTimeout()

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(timeoutCtx, b.chromeOpts...)
	defer cancelAllocator()

	taskCtx, cancelTask := chromedp.NewContext(
		allocatorCtx,
		cdpLoggerOption(b.Conf.ChromeDebug),
	)
	defer cancelTask()

	slog.Debug("run bot", "start_time", timeStart.UTC())
	slog.Info("start session")
	slog.Debug("navigate", "url", b.Conf.Url)

	if err := chromedp.Run(taskCtx,
		// open URL to initialize session and cookies before authentication
		chromedp.Navigate(b.Conf.Url),
	); err != nil {
		slog.Error("navigate failed", "error", err)

		return err
	}

	// perform authentication
	if err := b.auth(taskCtx); err != nil {
		slog.Error("auth failed", "error", err)

		return err
	}

	// perform money check
	if err := b.money(taskCtx); err != nil {
		slog.Error("money check failed", "error", err)

		return err
	}

	// service registry — built here because handlers need the `b` receiver
	services := map[string]func(context.Context) error{
		"company_stats":  b.companyStats,
		"alliance_stats": b.allianceStats,
		"claim_rewards":  b.claimRewards,
		"staff_morale":   b.staffMorale,
		"hubs":           b.hubs,
		"buy_fuel":       b.fuel,
		"marketing":      b.marketingCompanies,
		"ac_maintenance": b.maintenance,
		"depart":         b.depart,
	}

	availableServices := []string{
		"company_stats", "alliance_stats", "claim_rewards", "staff_morale",
		"hubs", "buy_fuel", "marketing", "ac_maintenance", "depart",
	}

	for _, serviceName := range b.Conf.Services {
		handler, ok := services[serviceName]
		if !ok {
			slog.Warn("unknown service", "service", serviceName, "available_services", availableServices)

			continue
		}

		if err := handler(taskCtx); err != nil {
			slog.Error("service failed", "service", serviceName, "error", err)

			return err
		}
	}

	// Chrome's SQLite cookie store flushes via a background timer (~30s after startup).
	// --no-sandbox (required in Docker) changes Chrome's shutdown path so the cookie store
	// is not flushed on exit. We only need to wait when authentication was just performed
	// (new cookies written); if the session was already valid, cookies are unchanged.
	if !b.HasValidCookies {
		if elapsed := time.Since(timeStart); elapsed < 35*time.Second {
			wait := 35*time.Second - elapsed
			slog.Debug("waiting for Chrome cookie flush timer", "elapsed", elapsed, "wait", wait)
			time.Sleep(wait)
		}
	}

	// Explicitly close Chrome via CDP while the connection is still live.
	// defer cancelAllocator() signals Chrome asynchronously and may not complete
	// a graceful shutdown before the process is killed. Browser.close is best-effort:
	// the connection drops immediately on receipt so an error is expected.
	if err := chromedp.Run(taskCtx, browser.Close()); err != nil {
		slog.Debug("browser.Close", "error", err)
	}

	// calculate total duration for Prometheus metric and logging
	duration := time.Since(timeStart)

	slog.Info("run complete", "elapsed_time", fmt.Sprint(duration))

	b.PrometheusMetrics.DurationSeconds.Set(duration.Seconds())

	return nil
}

// cdpLoggerOption returns the chromedp logging option based on the debug flag.
func cdpLoggerOption(debug bool) chromedp.ContextOption {
	if debug {
		return chromedp.WithDebugf(log.Printf)
	}

	return chromedp.WithLogf(log.Printf)
}

func getChromedpUserDataDir(appName string) string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		// fallback: next to binary or in temp dir if current directory is not writable
		cacheDir = os.TempDir()
	}

	dir := filepath.Join(cacheDir, appName, "chromedp-user-data")

	if err := os.MkdirAll(dir, 0700); err != nil {
		slog.Warn("failed to create user data dir", "error", err)
		return ""
	}

	// Remove stale Chrome singleton lock files left behind after a crash or hard kill.
	// If these files exist on startup, Chrome refuses to start with "File exists" error.
	for _, lockFile := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		path := filepath.Join(dir, lockFile)
		if err := os.Remove(path); err == nil {
			slog.Warn("removed stale Chrome lock file", "file", path)
		}
	}

	slog.Info("chrome user data dir", "dir", dir)

	return dir
}

// setupChromeOptions configures Chrome options based on the provided configuration.
func setupChromeOptions(conf *config.Config) []chromedp.ExecAllocatorOption {
	// Setup Chrome options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		// set the 'chrome_headless: false' config for displaying Chrome window
		chromedp.Flag("headless", conf.ChromeHeadless),
		chromedp.Flag("start-maximized", true),
		chromedp.WindowSize(1920, 1080),
		// disable GPU: Alpine Docker containers have no GPU drivers; without this
		// Chrome logs GPU init errors and may fall back to software rasterizer unreliably
		chromedp.Flag("disable-gpu", true),
		// Both flags are required when running as non-root inside Docker:
		// --no-sandbox disables the renderer sandbox;
		// --disable-setuid-sandbox stops the zygote from trying to create
		// Linux namespaces, which the kernel denies to unprivileged processes.
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.UserDataDir(getChromedpUserDataDir("am4bot")),
	)

	return opts
}
