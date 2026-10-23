package bot

import (
	"context"

	"github.com/ashokhin/am4bot/internal/config"
	"github.com/chromedp/chromedp"
)

// newChromeContext builds a chromedp browser context. It has two paths:
//
//   - Plain (conf.ChromeStealth == false): the ordinary exec-allocator path
//     using chromeOpts (setupChromeOptions) - passes Chrome's own --headless
//     flag when conf.ChromeHeadless is set. Useful for local troubleshooting
//     ("where did the bot get stuck") with a real Chrome window
//     (chrome_headless: false) and an attachable debugger.
//   - Stealth (conf.ChromeStealth == true): never passes --headless at all -
//     when conf.ChromeHeadless is set, it instead launches a real Xvfb
//     virtual display and starts Chrome against it (stealthExecAllocatorOptions,
//     chrome_session_linux.go), since a browser genuinely running without
//     that flag is harder for a site to fingerprint as automated than one
//     that announces itself via --headless. Xvfb is Linux-only, so this
//     combination (chrome_stealth + chrome_headless) isn't available on
//     other platforms - see chrome_session_other.go. See
//     internal/config.Config's ChromeStealth doc comment and
//     docs/multi-tenant-hosting.md. This used to be delegated to
//     github.com/Davincible/chromedp-undetected, replaced by this file once
//     that library stopped tracking chromedp's own API (effectively
//     unmaintained since 2024) - the flags/Xvfb approach reproduces exactly
//     what it did, since that's all it ever did: no JS-level
//     anti-fingerprinting of its own.
//
// The returned cancel func tears down everything the chosen path started
// (allocator, browser context, and - in the stealth+headless path - the
// Xvfb frame buffer).
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

	opts, closeFrameBuffer, err := stealthExecAllocatorOptions(conf)
	if err != nil {
		return nil, func() {}, err
	}

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, opts...)
	taskCtx, cancelTask := chromedp.NewContext(allocatorCtx, cdpLoggerOption(conf.ChromeDebug))

	cancel := func() {
		cancelTask()
		cancelAllocator()
		closeFrameBuffer()
	}

	return taskCtx, cancel, nil
}

// stealthExecAllocatorOptions builds the flag set for the stealth path:
// the same hygiene flags (suppressed first-run UI, no-sandbox for Docker,
// a fixed window size) as the plain path, minus --headless itself - see
// headlessViaXvfb (chrome_session_linux.go/chrome_session_other.go) for why,
// when conf.ChromeHeadless is set.
func stealthExecAllocatorOptions(conf *config.Config) (opts []chromedp.ExecAllocatorOption, closeFrameBuffer func(), err error) {
	closeFrameBuffer = func() {}

	opts = append(opts,
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("test-type", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("lang", "en-US"),
		chromedp.UserDataDir(getChromedpUserDataDir("am4bot-stealth")),
	)

	if conf.ChromeHeadless {
		headlessOpts, closeFB, err := headlessViaXvfb()
		if err != nil {
			return nil, func() {}, err
		}

		opts = append(opts, chromedp.Flag("window-size", "1920,1080"), chromedp.Flag("start-maximized", true))
		opts = append(opts, headlessOpts...)
		closeFrameBuffer = closeFB
	}

	return opts, closeFrameBuffer, nil
}
