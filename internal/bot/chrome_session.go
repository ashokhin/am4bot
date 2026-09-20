package bot

import (
	"context"

	"github.com/ashokhin/am4bot/internal/config"

	cu "github.com/Davincible/chromedp-undetected"
	"github.com/chromedp/chromedp"
)

// newChromeContext builds a chromedp browser context. It has two paths:
//
//   - Plain (conf.ChromeStealth == false): the existing exec-allocator path
//     using chromeOpts (setupChromeOptions) -- an ordinary, unmodified
//     Chrome/Chromium with visible flags. Useful for local troubleshooting
//     ("where did the bot get stuck") with a real Chrome window
//     (chrome_headless: false) and an attachable debugger.
//   - Stealth (conf.ChromeStealth == true): chromedp-undetected, which
//     patches navigator.webdriver/CDP fingerprints and, when headless,
//     launches Chrome inside a real Xvfb display rather than passing
//     Chrome's own --headless flag -- see internal/config.Config's
//     ChromeStealth doc comment and docs/multi-tenant-hosting.md.
//
// The returned cancel func tears down everything the chosen path started
// (allocator, browser context, and -- in the stealth path -- the Xvfb
// frame buffer).
func newChromeContext(ctx context.Context, conf *config.Config, chromeOpts []chromedp.ExecAllocatorOption) (context.Context, context.CancelFunc, error) {
	if !conf.ChromeStealth {
		allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, chromeOpts...)
		taskCtx, cancelTask := chromedp.NewContext(allocatorCtx, cdpLoggerOption(conf.ChromeDebug))

		cancel := func() {
			cancelTask()
			cancelAllocator()
		}

		return taskCtx, cancel, nil
	}

	cuOpts := []cu.Option{
		cu.WithContext(ctx),
		cu.WithUserDataDir(getChromedpUserDataDir("am4bot-stealth")),
		cu.WithNoSandbox(true),
	}

	if conf.ChromeHeadless {
		cuOpts = append(cuOpts, cu.WithHeadless())
	}

	cfg := cu.NewConfig(cuOpts...)
	cfg.ContextOptions = []chromedp.ContextOption{cdpLoggerOption(conf.ChromeDebug)}

	taskCtx, cancel, err := cu.New(cfg)
	if err != nil {
		return nil, func() {}, err
	}

	return taskCtx, cancel, nil
}
