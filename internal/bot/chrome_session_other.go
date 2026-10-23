//go:build !linux

package bot

import (
	"fmt"

	"github.com/chromedp/chromedp"
)

// headlessViaXvfb: Xvfb is Linux-only, so chrome_stealth + chrome_headless
// together aren't supported on other platforms - see chrome_session.go's
// newChromeContext doc comment. chrome_stealth without chrome_headless
// (a visible stealth-flagged window) still works everywhere, since that
// path never calls this.
func headlessViaXvfb() ([]chromedp.ExecAllocatorOption, func(), error) {
	return nil, func() {}, fmt.Errorf("chrome_stealth with chrome_headless requires Linux (Xvfb)")
}
