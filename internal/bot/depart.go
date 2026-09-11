package bot

import (
	"context"
	"log/slog"
	"math"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// depart handles the departure of all available aircraft from the fleet.
func (b *Bot) depart(ctx context.Context) error {
	slog.Info("depart all available aircraft")

	// get the number of aircraft ready for departure
	aircraftReadyForDepart := b.getReadyForDepart(ctx)
	// calculate maximum retries, because the "Depart" button may process only 20 aircraft at a time
	// also to avoid infinite loops when aircraft has been grounded
	maxRetries := int(math.Round(float64(aircraftReadyForDepart)/20) + 1)

	// loop until all aircraft have departed or maximum retries reached
	for (aircraftReadyForDepart > 0) && (maxRetries > 0) {
		var availableAfterDepart int

		slog.Info("depart available aircraft", "ready to depart", aircraftReadyForDepart, "depart retries", maxRetries)

		// click the "Depart All" button
		utils.DoClickElement(ctx, model.BUTTON_FI_DEPART_ALL)
		// get the number of aircraft still ready for departure
		availableAfterDepart = b.getReadyForDepart(ctx)

		slog.Info("aircraft departed", "count", (aircraftReadyForDepart - availableAfterDepart))

		aircraftReadyForDepart = availableAfterDepart

		maxRetries--

		// try to buy fuel after each depart iteration
		if err := b.fuel(ctx); err != nil {
			slog.Error("failed to refuel during depart iteration", "error", err)
		}
	}

	return nil
}

// getReadyForDepart retrieves the number of aircraft ready for departure from
// the fleet interface. It counts the rows in the "landed" list rather than
// reading the number on the "Depart" button, because that number caps at 20
// even when far more aircraft are ready — which previously made depart() stop
// after only ~40 aircraft on large fleets.
func (b *Bot) getReadyForDepart(ctx context.Context) int {
	var landedRows []*cdp.Node

	if err := chromedp.Run(ctx,
		// AtLeast(0): an empty landed list is a valid state, not an error.
		chromedp.Nodes(model.LIST_FI_LANDED, &landedRows, chromedp.ByQueryAll, chromedp.AtLeast(0)),
	); err != nil {
		slog.Debug("the landed aircraft list not found, assuming 0 ready for depart", "error", err)

		return 0
	}

	return len(landedRows)
}
