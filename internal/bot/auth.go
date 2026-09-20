package bot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/chromedp"
)

// loginVerificationTimeoutSeconds bounds how long auth waits for a
// dashboard-only element to appear after submitting credentials, before
// concluding the login itself failed (wrong password, a CAPTCHA, ...)
// rather than the game's own page just being slow. 30s is generous enough
// to tolerate a slow VPN exit, but short enough that a genuine failure is
// reported quickly and clearly -- instead of silently returning success
// here and only surfacing as an opaque "context deadline exceeded" much
// later, once some unrelated later step's own element wait runs out the
// clock on the whole run's timeout_seconds (which can be 180s+).
const loginVerificationTimeoutSeconds = 30

// auth performs authentication on the target website using credentials from the bot configuration.
// Sets b.HasValidCookies to true if the existing session was valid (cookies already
// persisted from a previous run). After a fresh login the field stays false — new cookies
// have been written to Chrome's in-memory store but not yet flushed to disk.
func (b *Bot) auth(ctx context.Context) error {
	b.HasValidCookies = false

	if !utils.IsElementVisible(ctx, model.BUTTON_PLAY_NOW) {
		slog.Debug("already authenticated, skipping auth step")
		b.HasValidCookies = true

		return nil
	}

	slog.Info("performing authentication")
	slog.Debug("auth", "url", b.Conf.Url, "user", utils.MaskUsername(b.Conf.User))

	if err := chromedp.Run(ctx,
		// open login page
		chromedp.Navigate(b.Conf.Url),
		// perform login steps
		chromedp.Click(model.BUTTON_PLAY_NOW, chromedp.ByQuery),
		chromedp.Click(model.BUTTON_LOGIN, chromedp.ByQuery),
		chromedp.WaitReady(model.TEXT_FIELD_LOGIN, chromedp.ByQuery),
		// fill in credentials and submit
		chromedp.SendKeys(model.TEXT_FIELD_LOGIN, b.Conf.User, chromedp.ByQuery),
		chromedp.SendKeys(model.TEXT_FIELD_PASSWORD, b.Conf.GetPassword(), chromedp.ByQuery),
		chromedp.SetAttributeValue(model.CHECKBOX_REMEMBER, "checked", "checked", chromedp.ByQuery),
		chromedp.Click(model.BUTTON_AUTH, chromedp.ByQuery),
		// wait for main page to load
		chromedp.WaitNotVisible(model.OVERLAY_LOADING, chromedp.ByQuery),
		utils.RefreshPage(),
	); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// The loading overlay disappearing above proves the PAGE finished
	// loading, not that the LOGIN succeeded -- a wrong password or a
	// CAPTCHA just re-renders the login page with an error message, and
	// that overlay clears the same way either way. Only a dashboard-only
	// element actually proves we're in -- see money(), the very next
	// step, which already depends on it being there.
	if !utils.IsElementVisible(ctx, model.BUTTON_MAIN_ACCOUNT, loginVerificationTimeoutSeconds) {
		return fmt.Errorf("auth: login failed -- dashboard not visible %ds after submitting credentials "+
			"(wrong password, a CAPTCHA, or an unusually slow connection/VPN)", loginVerificationTimeoutSeconds)
	}

	return nil
}
