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

		// DoClickElement's chromedp.Click waits for the button with no
		// timeout of its own -- if it's ever not there (a slow-rendering
		// popup, an unexpected page state), that wait doesn't end on its
		// own and eats the whole run's timeout instead of just this one
		// service failing fast. Check first, with a short bound.
		if !utils.IsElementVisible(ctx, model.BUTTON_FI_DEPART_ALL) {
			slog.Warn("depart: \"Depart All\" button not visible, stopping this iteration", "ready to depart", aircraftReadyForDepart)

			break
		}

		// click the "Depart All" button
		utils.DoClickElement(ctx, model.BUTTON_FI_DEPART_ALL)
		// get the number of aircraft still ready for departure
		availableAfterDepart = b.getReadyForDepart(ctx)

		departedThisIteration := aircraftReadyForDepart - availableAfterDepart

		slog.Info("aircraft departed", "count", departedThisIteration)

		// FlightsDepartedTotal counts what THIS node's depart service
		// actually dispatched, unlike am4_stats_flights_operated_total
		// (the whole airline account's lifetime total, read off a game
		// page -- see that gauge's own doc comment in
		// internal/metrics/prometheus.go) -- only add a positive count;
		// a stuck/grounded aircraft can leave availableAfterDepart >=
		// aircraftReadyForDepart, and a Counter must never go backwards.
		if departedThisIteration > 0 {
			b.PrometheusMetrics.FlightsDepartedTotal.Add(float64(departedThisIteration))
		}

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
//
// A grounded aircraft (maintenance overdue, audit, etc.) sits in this same
// landed list but can't actually depart -- LIST_FI_LANDED's own selector
// excludes it (see model/css.go). Counting it here used to make depart()
// believe an aircraft the "Depart All" button won't move was still ready,
// clicking that inert button every iteration until the whole run timed out.
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
