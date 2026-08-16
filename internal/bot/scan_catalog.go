package bot

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ashokhin/am4bot/internal/io"
	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// restartBrowserEveryNAirports bounds how many airports get a full distance
// sweep before the browser session is recycled (closed and re-authenticated
// from scratch). A single long-lived Chrome tab, doing thousands of route
// searches back to back over a multi-day/multi-week scan, leaks memory that
// never gets reclaimed without a fresh navigation — confirmed live: one run
// hit 21.5GB RSS after ~15 hours (about 13 airports) and started dragging
// the whole host toward OOM (2.1GB free, swapping). Restarting after every
// single airport keeps that bounded to "worst case, one airport's worth of
// leak" — cheap relative to a scan that already takes 1+ hour per airport.
const restartBrowserEveryNAirports = 1

// ScanFullCatalog scans every airport in the game (grouped by country, via the
// "Custom departure" form) and writes the full route catalog — airports and
// routes, keyed by the game's internal numeric airport IDs — to a SQLite
// database at dbPath. Airports already marked as fully scanned are skipped, so
// an interrupted run can be resumed by running it again.
func (b *Bot) ScanFullCatalog(ctx context.Context, dbPath string) error {
	slog.Info("scanning full route catalog", "db_path", dbPath)

	writer, err := io.NewCatalogWriter(dbPath)
	if err != nil {
		return fmt.Errorf("open catalog database: %w", err)
	}
	defer writer.Close()

	taskCtx, cancel, err := b.startScanner(ctx)
	if err != nil {
		return fmt.Errorf("starting scanner: %w", err)
	}
	defer func() { cancel() }()

	// Country names are read once, up front, as plain strings — not kept as
	// DOM node references, which would go stale the moment the browser
	// session that produced them gets recycled below.
	countryElemList, err := b.openCustomDepartureForm(taskCtx)
	if err != nil {
		return err
	}

	countryNames := make([]string, 0, len(countryElemList))
	for _, countryElem := range countryElemList {
		if name := countryElem.AttributeValue("value"); name != "" {
			countryNames = append(countryNames, name)
		}
	}

	airportsSinceRestart := 0

	for _, countryName := range countryNames {
		slog.Info("scanning country", "country", countryName)

		// Looping rather than a single pass: if the browser gets recycled
		// partway through this country's airport list, that list's remaining
		// *cdp.Node entries are now invalid (they belong to the closed
		// session) — re-selecting the country gets a fresh, valid list.
		// Airports already scanned before the restart are re-encountered
		// but skip instantly (AirportScanned is a cheap DB check), so this
		// costs nothing beyond that check.
		for {
			airportElemList, err := b.selectCountryAndListAirports(taskCtx, countryName)
			if err != nil {
				return fmt.Errorf("listing airports for country %q: %w", countryName, err)
			}

			restarted := false

			for _, airportElem := range airportElemList {
				scanned, err := b.scanCatalogAirportEntry(taskCtx, writer, airportElem, countryName)
				if err != nil {
					return err
				}

				if !scanned {
					continue
				}

				airportsSinceRestart++
				if airportsSinceRestart < restartBrowserEveryNAirports {
					continue
				}

				slog.Info("recycling browser session to bound memory growth over the long-running scan",
					"airports_since_restart", airportsSinceRestart)

				cancel()

				taskCtx, cancel, err = b.startScanner(ctx)
				if err != nil {
					return fmt.Errorf("restarting scanner: %w", err)
				}

				// startScanner only authenticates — it doesn't land on the
				// route-search "Custom departure" panel, so without
				// re-opening it here, the very next selectCountryAndListAirports
				// call fails (its selectors don't exist yet on whatever page
				// login landed on).
				if _, err := b.openCustomDepartureForm(taskCtx); err != nil {
					return fmt.Errorf("reopening custom departure form after browser restart: %w", err)
				}

				airportsSinceRestart = 0
				restarted = true

				break
			}

			if !restarted {
				break
			}
		}
	}

	return nil
}

// scanCatalogAirportEntry upserts a single airport and, if its route sweep
// hasn't already completed, performs the full distance sweep for it.
//
// Any error here — including a browser step that stayed stuck through all of
// runCatalogStep's retries — aborts the entire scan rather than skipping just
// this airport. That's intentional: a genuinely wedged browser tab won't
// un-wedge itself for the next airport either, so continuing would just fail
// the same way repeatedly. The caller is expected to exit the process on
// error so its container's restart policy brings up a fresh browser session;
// the scan-progress persisted along the way (SetScanProgress,
// MarkAirportScanned) means the resumed run picks back up close to where
// this one stopped, instead of restarting from scratch.
//
// Returns scanned=true only when it actually performed a distance sweep for
// this airport (not when skipped as out-of-range, already-scanned, or an
// invalid option) — the caller uses this to count real work done, for the
// browser-recycle threshold in ScanFullCatalog (see
// restartBrowserEveryNAirports).
func (b *Bot) scanCatalogAirportEntry(ctx context.Context, writer *io.CatalogWriter, airportElem *cdp.Node, countryName string) (scanned bool, err error) {
	airportID, airportValue, airportName, ok := parseAirportOption(airportElem)
	if !ok {
		return false, nil
	}

	if !b.shouldScanCatalogAirport(airportID) {
		return false, nil
	}

	if err := writer.UpsertAirport(airportID, airportName, countryName); err != nil {
		return false, fmt.Errorf("upserting airport %d: %w", airportID, err)
	}

	alreadyScanned, err := writer.AirportScanned(airportID)
	if err != nil {
		return false, fmt.Errorf("checking scanned state for airport %d: %w", airportID, err)
	}

	if alreadyScanned {
		slog.Debug("airport already scanned, skipping", "airport_id", airportID, "name", airportName)

		return false, nil
	}

	slog.Info("scanning airport", "airport_id", airportID, "name", airportName, "country", countryName)

	if err := b.scanCatalogAirport(ctx, writer, airportID, airportValue); err != nil {
		return false, fmt.Errorf("scanning routes for airport %d (%s): %w", airportID, airportName, err)
	}

	if err := writer.MarkAirportScanned(airportID); err != nil {
		return false, fmt.Errorf("marking airport %d scanned: %w", airportID, err)
	}

	return true, nil
}

const (
	// catalogResultsCap is the number of rows the game's route search returns
	// per query. A query returning exactly this many rows is treated as
	// saturated — there may be more matching routes hidden past the cap.
	catalogResultsCap = 50

	// catalogSearchTimeout bounds how long a single search query waits for
	// results to render. Many distance/runway windows legitimately match zero
	// routes, and the results panel never appears for those — without a
	// bound, WaitVisible hangs forever instead of treating that as "0 results".
	// This is a distinct, smaller timeout from catalogStepTimeout below: here,
	// timing out is an expected outcome (empty result set), not a failure, so
	// it isn't retried.
	catalogSearchTimeout = 10 * time.Second

	// catalogStepTimeout bounds every other browser interaction in the catalog
	// scan (setting form fields, clicking search, reading result nodes) — none
	// of these have a legitimate reason to take long, so a hang here means the
	// browser tab has gone unresponsive.
	catalogStepTimeout = 15 * time.Second

	// catalogStepMaxRetries is how many times a stuck step is retried (in case
	// it's a transient CDP hiccup) before runCatalogStep gives up and returns
	// an error. That error is treated as fatal by every caller in this file —
	// the whole scan aborts, the process exits non-zero, and the container's
	// restart policy brings up a fresh browser session. Thanks to the
	// per-airport scan-progress persisted in SetScanProgress, the resumed run
	// picks back up from where it was, instead of silently skipping whatever
	// airport happened to be mid-scan when the tab wedged.
	catalogStepMaxRetries = 3
)

// runCatalogStep runs actions with a bounded per-attempt timeout, retrying up
// to catalogStepMaxRetries times before giving up. description is used only
// for logging/error context.
func runCatalogStep(ctx context.Context, description string, actions ...chromedp.Action) error {
	var lastErr error

	for attempt := 1; attempt <= catalogStepMaxRetries; attempt++ {
		stepCtx, cancel := context.WithTimeout(ctx, catalogStepTimeout)
		lastErr = chromedp.Run(stepCtx, actions...)
		cancel()

		if lastErr == nil {
			return nil
		}

		slog.Warn("catalog scan step failed, retrying", "step", description,
			"attempt", attempt, "max_attempts", catalogStepMaxRetries, "error", lastErr)
	}

	return fmt.Errorf("%s: failed after %d attempts: %w", description, catalogStepMaxRetries, lastErr)
}

// dispatchChangeEvent fires native "input" and "change" events on the
// element matching sel. chromedp.SetValue only sets the DOM .value property
// — it does not dispatch any event — but the game tracks state like the
// selected departure airport via a jQuery `.on('change', ...)` handler
// (confirmed from a live HTML dump: `$('#citySelect').on('change',
// function(){ depId = $(this).val(); })`), and jQuery's event binding
// responds to a plain native dispatchEvent just as it would to real user
// interaction. Without firing this, that handler never runs and the game's
// own notion of the field's value never updates to match what's visibly in
// the DOM.
//
// Both "input" and "change" are fired (not just whichever a given field
// happens to listen for) since that's cheap and one-size-fits-all: e.g. a
// text field capturing "min. runway" is exactly the kind of control that
// commonly wires up "input" instead of "change" for a live-updating value —
// confirmed necessary live: without firing anything at all here, every
// runway sub-sweep query silently kept re-querying the same first threshold
// no matter how high startRunway climbed (identical "still saturated"
// results at every single step, for every distance window, which is not
// physically possible — the game's own catalogued max runway is ~18045ft,
// yet windows stayed saturated all the way past 19000ft).
func dispatchChangeEvent(sel string) chromedp.Action {
	js := fmt.Sprintf(`(function(){
		var el = document.querySelector(%s);
		if (!el) { return; }
		el.dispatchEvent(new Event('input', {bubbles: true}));
		el.dispatchEvent(new Event('change', {bubbles: true}));
	})()`, strconv.Quote(sel))

	return chromedp.Evaluate(js, nil)
}

// selectAirportVerified selects airportValue as the custom-departure origin —
// setting the dropdown's value *and* dispatching a change event (see
// dispatchChangeEvent) so the game's own "selected departure airport" state
// actually updates, not just the DOM's visible value — then reads the value
// back to confirm the selection took, retrying the whole cycle up to
// catalogStepMaxRetries times if it didn't.
//
// Confirmed live: a full sweep "for airport 356" had every single result
// row's actual dep ID read back as 1427 — the search never left airport
// 1427, because chromedp.SetValue alone (no change event) never updated the
// game's `depId` JS variable that the search actually keys off of. Without
// this fix, every one of those routes would have been written to the
// catalog attributed to airport 356 instead.
func selectAirportVerified(ctx context.Context, airportID int, airportValue string) error {
	var lastErr error

	for attempt := 1; attempt <= catalogStepMaxRetries; attempt++ {
		if err := runCatalogStep(ctx, "selecting airport",
			chromedp.SetValue(model.SELECT_FLEET_RESEARCH_AIRPORT_SELECTOR, airportValue, chromedp.ByQuery),
			dispatchChangeEvent(model.SELECT_FLEET_RESEARCH_AIRPORT_SELECTOR),
		); err != nil {
			return err
		}

		var got string
		if err := runCatalogStep(ctx, "verifying airport selection",
			chromedp.Value(model.SELECT_FLEET_RESEARCH_AIRPORT_SELECTOR, &got, chromedp.ByQuery),
		); err != nil {
			return err
		}

		if got == airportValue {
			return nil
		}

		lastErr = fmt.Errorf("airport selection did not take: wanted %q, dropdown reads %q", airportValue, got)

		slog.Warn("airport selection verification failed, retrying", "airport_id", airportID,
			"attempt", attempt, "max_attempts", catalogStepMaxRetries, "wanted", airportValue, "got", got)
	}

	return fmt.Errorf("selecting airport %d: failed after %d attempts: %w", airportID, catalogStepMaxRetries, lastErr)
}

// scanCatalogAirport selects airportValue as the custom-departure origin and
// sweeps the full configured distance range (b.Conf.MaxRouteDistanceKm down to
// MinRouteDistanceKm, in ScanStepKm steps — the same window-narrowing approach
// ScanRoutes uses to work around the game's 50-results-per-query cap), writing
// every discovered route to the catalog.
//
// Distance is not the only axis the 50-results cap can hide routes behind:
// a single distance window can itself contain more than 50 destinations
// (e.g. a dense hub region), in which case varying "min. runway" — the game's
// other search filter — is needed to split that window further. This is done
// adaptively: only windows that come back saturated (exactly catalogResultsCap
// rows) get a runway sub-sweep, so airports with few routes stay cheap.
func (b *Bot) scanCatalogAirport(ctx context.Context, writer *io.CatalogWriter, airportID int, airportValue string) error {
	if err := selectAirportVerified(ctx, airportID, airportValue); err != nil {
		return err
	}

	currentDistance := b.Conf.MaxRouteDistanceKm

	progress, err := writer.GetScanProgress(airportID)
	if err != nil {
		return fmt.Errorf("reading scan progress: %w", err)
	}

	if progress.DistanceKm.Valid {
		currentDistance = int(progress.DistanceKm.Int64)

		slog.Info("resuming airport sweep from saved progress", "airport_id", airportID, "max_distance_km", currentDistance)
	}

	for currentDistance >= b.Conf.MinRouteDistanceKm {
		// resumedRunway is set only when this is the very distance window a
		// prior (interrupted) run last saved progress for, and that progress
		// includes a runway threshold — meaning the base query for this
		// distance was already confirmed saturated before the interruption,
		// and a runway sub-sweep was underway. Skip straight back into it
		// instead of re-running the (redundant, already-known-saturated)
		// base query.
		resumedRunway := 0
		if progress.DistanceKm.Valid && progress.RunwayFt.Valid && currentDistance == int(progress.DistanceKm.Int64) {
			resumedRunway = int(progress.RunwayFt.Int64)

			slog.Info("resuming distance window's runway sub-sweep from saved progress",
				"airport_id", airportID, "max_distance_km", currentDistance, "min_runway_ft", resumedRunway)
		}

		if resumedRunway > 0 {
			if err := b.scanCatalogDistanceByRunway(ctx, writer, airportID, currentDistance, resumedRunway); err != nil {
				return err
			}
		} else {
			if err := writer.SetScanProgress(airportID, currentDistance, sql.NullInt64{}); err != nil {
				return fmt.Errorf("saving scan progress: %w", err)
			}

			found, err := b.scanCatalogDistance(ctx, writer, airportID, currentDistance, b.Conf.CatalogMinRunwayLengthFt)
			if err != nil {
				return err
			}

			if found == catalogResultsCap && b.Conf.CatalogDisableRunwaySweep {
				slog.Debug("distance window saturated but runway sweep is disabled, some routes may be missing",
					"airport_id", airportID, "max_distance_km", currentDistance)
			} else if found == catalogResultsCap {
				slog.Debug("distance window saturated, sweeping by runway to find hidden routes",
					"airport_id", airportID, "max_distance_km", currentDistance)

				if err := b.scanCatalogDistanceByRunway(ctx, writer, airportID, currentDistance, b.Conf.CatalogMinRunwayLengthFt); err != nil {
					return err
				}
			} else if b.Conf.CatalogDisableEarlyExit {
				// See CatalogDisableEarlyExit's doc comment — validating that
				// this early-exit doesn't actually lose routes.
				slog.Debug("distance window unsaturated but early-exit is disabled, continuing sweep anyway",
					"airport_id", airportID, "max_distance_km", currentDistance, "routes_found", found)
			} else {
				// Unsaturated: this query returned every route with
				// distance <= currentDistance, so every route in
				// [MinRouteDistanceKm, currentDistance] is now fully captured —
				// further shrinking distance can only re-query an
				// already-fully-known subset. Nothing left to find.
				slog.Debug("distance window unsaturated, every route within range is captured — stopping sweep for this airport",
					"airport_id", airportID, "max_distance_km", currentDistance, "routes_found", found)

				break
			}
		}

		currentDistance -= b.Conf.ScanStepKm

		select {
		case b.ProgressChan <- struct{}{}:
		default:
		}
	}

	return nil
}

// scanCatalogDistanceByRunway re-queries a saturated distance window across
// increasing "min. runway" thresholds (startRunway up to
// CatalogMaxRunwayLengthFt, in CatalogRunwayStepFt steps — startRunway is
// CatalogMinRunwayLengthFt on a fresh sweep, redundantly re-querying the
// caller's already-saturated base query, or a saved mid-sub-sweep threshold
// when resuming). Each threshold is a strict superset of the next, so this
// surfaces any route whose runway length pushed it past the first,
// effectively-unfiltered query's cap. WriteRoute's upsert makes the overlap
// between runway bands (and the caller's base query) harmless.
func (b *Bot) scanCatalogDistanceByRunway(ctx context.Context, writer *io.CatalogWriter, fromID, maxDistance, startRunway int) error {
	sawUnsaturated := false

	for currentRunway := startRunway; currentRunway <= b.Conf.CatalogMaxRunwayLengthFt; currentRunway += b.Conf.CatalogRunwayStepFt {
		if err := writer.SetScanProgress(fromID, maxDistance, sql.NullInt64{Int64: int64(currentRunway), Valid: true}); err != nil {
			return fmt.Errorf("saving scan progress: %w", err)
		}

		found, err := b.scanCatalogDistance(ctx, writer, fromID, maxDistance, currentRunway)
		if err != nil {
			return err
		}

		if found < catalogResultsCap {
			if !b.Conf.CatalogDisableEarlyExit {
				// Unsaturated: this query captured every remaining route with
				// runway >= currentRunway — higher thresholds are a strict
				// subset and can't reveal anything new. Stop here. See
				// CatalogDisableEarlyExit's doc comment for how this gets
				// validated.
				return nil
			}

			slog.Debug("distance+runway window unsaturated but early-exit is disabled, continuing sweep anyway",
				"airport_id", fromID, "max_distance_km", maxDistance, "min_runway_ft", currentRunway, "routes_found", found)

			sawUnsaturated = true

			continue
		}

		sawUnsaturated = false

		slog.Warn("distance+runway window still saturated; continuing to a higher runway threshold",
			"airport_id", fromID, "max_distance_km", maxDistance, "min_runway_ft", currentRunway)
	}

	// Only genuinely a "may be incomplete" situation if the *last* threshold
	// tested was still saturated — with CatalogDisableEarlyExit set, the loop
	// runs to completion regardless, so reaching the end doesn't by itself
	// mean anything was missed (it may well have gone unsaturated long
	// before CatalogMaxRunwayLengthFt and just kept sweeping past that point
	// to validate the early-exit assumption).
	if !sawUnsaturated {
		slog.Warn("reached max_runway_length_ft while still saturated for this window — results may be incomplete; consider raising max_runway_length_ft or lowering runway_step_ft",
			"airport_id", fromID, "max_distance_km", maxDistance)
	}

	return nil
}

// scanCatalogDistance searches routes from the already-selected origin up to
// maxDistance and at least minRunway, and writes every result row to the
// catalog. Returns the number of result rows found, so the caller can detect
// a saturated (likely-truncated) query.
//
// Timing for each phase (search, DOM scan/read, DB save) is logged at debug
// level, to make any scan-speed degradation over a long run (browser tab
// growing sluggish, DB writes slowing down, etc.) visible in the logs instead
// of only showing up as a vague "it's taking longer than it used to".
func (b *Bot) scanCatalogDistance(ctx context.Context, writer *io.CatalogWriter, fromID, maxDistance, minRunway int) (int, error) {
	queryStart := time.Now()

	if err := runCatalogStep(ctx, "searching routes",
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MAX_DISTANCE, strconv.Itoa(maxDistance), chromedp.ByQuery),
		dispatchChangeEvent(model.TEXTFIELD_FLEET_RESEARCH_MAX_DISTANCE),
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY, strconv.Itoa(minRunway), chromedp.ByQuery),
		dispatchChangeEvent(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY),
		utils.ClickElement(model.BUTTON_FLEET_RESEARCH_SEARCH),
	); err != nil {
		return 0, err
	}

	// Bounded wait: many distance/runway windows legitimately match zero
	// routes, and the results panel never renders for those — an unbounded
	// WaitVisible would hang forever instead of treating that as "0 results".
	waitCtx, waitCancel := context.WithTimeout(ctx, catalogSearchTimeout)
	defer waitCancel()

	if err := chromedp.Run(waitCtx, chromedp.WaitVisible(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, chromedp.ByQuery)); err != nil {
		searchDur := time.Since(queryStart)

		slog.Debug("catalog query timing", "airport_id", fromID, "max_distance_km", maxDistance, "min_runway_ft", minRunway,
			"routes_found", 0, "search_ms", searchDur.Milliseconds(), "scan_ms", 0, "save_ms", 0)

		return 0, nil //nolint:nilerr // timeout here means "no routes in this window", not a scan failure
	}

	searchDur := time.Since(queryStart)

	scanStart := time.Now()

	var routesElemList []*cdp.Node

	if err := runCatalogStep(ctx, "reading results",
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, &routesElemList, chromedp.ByQueryAll),
	); err != nil {
		return 0, err
	}

	// Despite selectAirportVerified's confirmation before this query started,
	// cross-check the first row's own dep ID against fromID before trusting
	// any of these results. Confirmed live: a whole sweep "for airport 356"
	// had every row read back dep=1427 — the search silently never left the
	// previous airport. If that happens, the entire result set belongs to
	// the wrong origin, not just a stray row, so this aborts the query
	// outright (fatal — see the package doc on why callers treat this as a
	// crash-and-resume condition) rather than writing any of it under fromID.
	if len(routesElemList) > 0 {
		if depID, _, err := utils.ParseDepArr(routesElemList[0].AttributeValue("onclick")); err == nil && depID != fromID {
			return 0, fmt.Errorf("search results are for airport %d, not the selected %d — refusing to record them", depID, fromID)
		}
	}

	var saveDur time.Duration

	for _, routeElem := range routesElemList {
		depID, arrID, err := utils.ParseDepArr(routeElem.AttributeValue("onclick"))
		if err != nil {
			slog.Warn("error parsing dep/arr from result row", "error", err)

			continue
		}

		if depID != fromID {
			slog.Warn("unexpected dep ID in result row", "expected", fromID, "got", depID)

			continue
		}

		route := io.CatalogRoute{
			FromAirportID: fromID,
			ToAirportID:   arrID,
			DistanceKm:    utils.AtoiSafe(routeElem.AttributeValue("data-distance")),
			DemandY:       utils.AtoiSafe(routeElem.AttributeValue("data-yclass")),
			DemandJ:       utils.AtoiSafe(routeElem.AttributeValue("data-jclass")),
			DemandF:       utils.AtoiSafe(routeElem.AttributeValue("data-fclass")),
			DemandLarge:   utils.AtoiSafe(routeElem.AttributeValue("data-large")) * 1000,
			DemandHeavy:   utils.AtoiSafe(routeElem.AttributeValue("data-heavy")) * 1000,
		}

		saveStart := time.Now()
		writeErr := writer.WriteRoute(route)
		saveDur += time.Since(saveStart)

		if writeErr != nil {
			return 0, fmt.Errorf("writing route: %w", writeErr)
		}

		// The result row also names the destination airport's own country and
		// name (e.g. "India, Mumbai" — the game has no separate "city"
		// concept, just an airport name that often matches or contains one)
		// — upsert it with real data now, instead of waiting for the scanner
		// to eventually reach that airport's own country/airport listing.
		// Without this, SetAirportRunway below would be the first write for
		// a not-yet-seen destination and would fall back to an
		// empty-name/country stub.
		//
		// UpsertAirportIfUnset, not UpsertAirport: this text is read from a
		// specific DOM node inside a search result row under time pressure —
		// a wrong selector match here must not be allowed to clobber a
		// name/country already recorded from the authoritative source (the
		// country dropdown, in scanCatalogAirportEntry).
		var toLocation string
		if err := runCatalogStep(ctx, "reading destination airport location",
			chromedp.Text(model.TEXT_FLEET_RESEARCH_ROUTE_TO_LOCATION, &toLocation, chromedp.ByQuery, chromedp.FromNode(routeElem)),
		); err != nil {
			slog.Warn("error reading destination airport location", "airport_id", arrID, "error", err)
		} else if country, airportName, ok := parseCountryAirportName(toLocation); ok {
			saveStart := time.Now()
			upsertErr := writer.UpsertAirportIfUnset(arrID, airportName, country)
			saveDur += time.Since(saveStart)

			if upsertErr != nil {
				return 0, fmt.Errorf("upserting destination airport: %w", upsertErr)
			}
		} else {
			slog.Warn("unexpected destination location text", "airport_id", arrID, "text", toLocation)
		}

		// Runway length is a property of the destination airport, not of this
		// route — record it there instead of on the route.
		saveStart = time.Now()
		runwayErr := writer.SetAirportRunway(arrID, utils.AtoiSafe(routeElem.AttributeValue("data-rwy")))
		saveDur += time.Since(saveStart)

		if runwayErr != nil {
			return 0, fmt.Errorf("writing destination airport runway: %w", runwayErr)
		}
	}

	scanDur := time.Since(scanStart) - saveDur

	slog.Debug("catalog query timing", "airport_id", fromID, "max_distance_km", maxDistance, "min_runway_ft", minRunway,
		"routes_found", len(routesElemList), "search_ms", searchDur.Milliseconds(), "scan_ms", scanDur.Milliseconds(), "save_ms", saveDur.Milliseconds())

	return len(routesElemList), nil
}

// ScanAirportCodes performs a lightweight scan — one minimal search per
// airport, instead of a full distance sweep — to capture each airport's IATA
// or ICAO code, whichever the account's code-display setting currently shows.
// The caller must set the desired display mode in-game before running this;
// it is a single account-wide toggle, not something this scan can change.
//
// Since this walks every airport via the same authoritative country dropdown
// as ScanFullCatalog, it also re-asserts each visited airport's name/country
// from that source — repairing any airport whose name/country was clobbered
// by a bad opportunistic read during the full catalog scan (see
// UpsertAirportIfUnset's doc comment). This repair runs for every airport
// this scan visits, regardless of whether it already has the requested code,
// so a single codes pass over the whole game also doubles as a full
// name/country repair pass.
//
// Airports that already have the requested code recorded skip the (more
// expensive) code lookup, so this can be safely resumed too.
// restartBrowserEveryNCodeLookups is ScanAirportCodes' equivalent of
// restartBrowserEveryNAirports — much larger, since each code lookup is one
// lightweight search rather than a full distance/runway sweep, so memory
// grows far slower per airport here. Still recycled periodically rather than
// never, since this scan also runs for a long time over thousands of
// airports on the same never-navigated tab.
const restartBrowserEveryNCodeLookups = 200

func (b *Bot) ScanAirportCodes(ctx context.Context, dbPath string, codeType string) error {
	if codeType != "iata" && codeType != "icao" {
		return fmt.Errorf(`unknown code type %q, expected "iata" or "icao"`, codeType)
	}

	slog.Info("scanning airport codes", "db_path", dbPath, "code_type", codeType)

	writer, err := io.NewCatalogWriter(dbPath)
	if err != nil {
		return fmt.Errorf("open catalog database: %w", err)
	}
	defer writer.Close()

	taskCtx, cancel, err := b.startScanner(ctx)
	if err != nil {
		return fmt.Errorf("starting scanner: %w", err)
	}
	defer func() { cancel() }()

	countryElemList, err := b.openCustomDepartureForm(taskCtx)
	if err != nil {
		return err
	}

	countryNames := make([]string, 0, len(countryElemList))
	for _, countryElem := range countryElemList {
		if name := countryElem.AttributeValue("value"); name != "" {
			countryNames = append(countryNames, name)
		}
	}

	lookupsSinceRestart := 0

	for _, countryName := range countryNames {
		// See ScanFullCatalog's identical loop shape for why this re-selects
		// the country on every restart instead of a single pass.
		for {
			airportElemList, err := b.selectCountryAndListAirports(taskCtx, countryName)
			if err != nil {
				return fmt.Errorf("listing airports for country %q: %w", countryName, err)
			}

			restarted := false

			for _, airportElem := range airportElemList {
				scanned, err := b.scanAirportCodeEntry(taskCtx, writer, airportElem, countryName, codeType)
				if err != nil {
					return err
				}

				if !scanned {
					continue
				}

				lookupsSinceRestart++
				if lookupsSinceRestart < restartBrowserEveryNCodeLookups {
					continue
				}

				slog.Info("recycling browser session to bound memory growth over the long-running scan",
					"lookups_since_restart", lookupsSinceRestart)

				cancel()

				taskCtx, cancel, err = b.startScanner(ctx)
				if err != nil {
					return fmt.Errorf("restarting scanner: %w", err)
				}

				// See ScanFullCatalog's identical call for why this is needed
				// — startScanner only authenticates, it doesn't reopen the
				// route-search panel.
				if _, err := b.openCustomDepartureForm(taskCtx); err != nil {
					return fmt.Errorf("reopening custom departure form after browser restart: %w", err)
				}

				lookupsSinceRestart = 0
				restarted = true

				break
			}

			if !restarted {
				break
			}
		}
	}

	return nil
}

// scanAirportCodeEntry mirrors scanCatalogAirportEntry's fatal-on-error
// policy — see its doc comment for why.
func (b *Bot) scanAirportCodeEntry(ctx context.Context, writer *io.CatalogWriter, airportElem *cdp.Node, countryName, codeType string) (scanned bool, err error) {
	airportID, airportValue, airportName, ok := parseAirportOption(airportElem)
	if !ok {
		return false, nil
	}

	if !b.shouldScanCatalogAirport(airportID) {
		return false, nil
	}

	// Repair name/country from the authoritative source before the
	// hasCode-skip below — that skip is about avoiding a redundant *code*
	// lookup, not about skipping this repair, so it must run for every
	// airport this scan visits, coded or not. See ScanAirportCodes' doc
	// comment.
	//
	// TODO(temporary diagnostic, remove after the next catalog_codes_scanner
	// run): the GetAirportNameCountry call + if-block immediately below exist
	// only to log a count of how many airports the now-fixed
	// UpsertAirport-clobbering bug actually corrupted (grep scanner logs for
	// "repaired mismatched airport name/country" to get the number). Once
	// that number has been seen and recorded, delete the
	// "prevName, prevCountry, err := ..." line and the "if prevCountry != ..."
	// block right below it. Leave the writer.UpsertAirport(...) repair call
	// (and its own "if err := ..." a few lines down) exactly as-is — it's
	// self-contained and needs no change; only the diagnostic comparison
	// and log line above it go.
	prevName, prevCountry, err := writer.GetAirportNameCountry(airportID)
	if err != nil {
		return false, fmt.Errorf("reading current name/country for airport %d: %w", airportID, err)
	}

	if prevCountry != "" && (prevCountry != countryName || prevName != airportName) {
		slog.Info("repaired mismatched airport name/country", "airport_id", airportID,
			"prev_name", prevName, "prev_country", prevCountry, "name", airportName, "country", countryName)
	}

	if err := writer.UpsertAirport(airportID, airportName, countryName); err != nil {
		return false, fmt.Errorf("repairing name/country for airport %d: %w", airportID, err)
	}

	hasCode, err := airportHasCode(writer, airportID, codeType)
	if err != nil {
		return false, fmt.Errorf("checking existing code for airport %d: %w", airportID, err)
	}

	if hasCode {
		return false, nil
	}

	code, err := b.fetchAirportCode(ctx, airportID, airportValue)
	if err != nil {
		return false, fmt.Errorf("fetching code for airport %d (%s): %w", airportID, airportName, err)
	}

	if code == "" {
		slog.Debug("airport has no outgoing routes, cannot determine code", "airport_id", airportID, "name", airportName)

		return true, nil
	}

	if err := setAirportCode(writer, airportID, codeType, code); err != nil {
		return false, fmt.Errorf("saving code for airport %d: %w", airportID, err)
	}

	slog.Debug("captured airport code", "airport_id", airportID, "name", airportName, "code_type", codeType, "code", code)

	return true, nil
}

// fetchAirportCode selects airportValue as the custom-departure origin, runs
// one broad search, and returns the "from" code shown on the first result row
// — that airport's own code under the currently active IATA/ICAO display mode.
// Returns an empty string (not an error) if the airport has no routes.
func (b *Bot) fetchAirportCode(ctx context.Context, airportID int, airportValue string) (string, error) {
	if err := selectAirportVerified(ctx, airportID, airportValue); err != nil {
		return "", err
	}

	if err := runCatalogStep(ctx, "searching routes",
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MAX_DISTANCE, strconv.Itoa(b.Conf.MaxRouteDistanceKm), chromedp.ByQuery),
		dispatchChangeEvent(model.TEXTFIELD_FLEET_RESEARCH_MAX_DISTANCE),
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY, strconv.Itoa(b.Conf.CatalogMinRunwayLengthFt), chromedp.ByQuery),
		dispatchChangeEvent(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY),
		utils.ClickElement(model.BUTTON_FLEET_RESEARCH_SEARCH),
	); err != nil {
		return "", err
	}

	// Bounded wait: an airport with zero routes never renders a result row, so
	// an unbounded WaitVisible would hang forever. A short timeout here treats
	// "no results appeared in time" as "no routes" rather than a scan failure.
	waitCtx, waitCancel := context.WithTimeout(ctx, catalogSearchTimeout)
	defer waitCancel()

	if err := chromedp.Run(waitCtx, chromedp.WaitVisible(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, chromedp.ByQuery)); err != nil {
		return "", nil //nolint:nilerr // timeout here means "no routes", not a scan failure
	}

	var routesElemList []*cdp.Node
	if err := runCatalogStep(ctx, "reading results",
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, &routesElemList, chromedp.ByQueryAll),
	); err != nil {
		return "", err
	}

	if len(routesElemList) == 0 {
		return "", nil
	}

	// Even with selectAirportVerified's confirmation above, cross-check the
	// result row's own dep ID before trusting its code — belt-and-suspenders
	// against the same "search silently ran for a different airport" failure
	// mode that corrupted route data (see selectAirportVerified's doc
	// comment). A code read from the wrong airport would otherwise get
	// written under this airportID with no warning at all.
	if depID, _, err := utils.ParseDepArr(routesElemList[0].AttributeValue("onclick")); err == nil && depID != airportID {
		return "", fmt.Errorf("result row is for airport %d, not the selected %d — refusing to record its code", depID, airportID)
	}

	var code string
	if err := runCatalogStep(ctx, "reading airport code",
		chromedp.Text(model.TEXT_FLEET_RESEARCH_ROUTE_FROM, &code, chromedp.ByQuery, chromedp.FromNode(routesElemList[0])),
	); err != nil {
		return "", err
	}

	return code, nil
}

// openCustomDepartureForm opens the fleet "Research" tab and switches to the
// "Custom departure" panel, returning the list of country option nodes.
func (b *Bot) openCustomDepartureForm(ctx context.Context) ([]*cdp.Node, error) {
	utils.DoClickElement(ctx, model.BUTTON_MAIN_FLEET)

	var countryElemList []*cdp.Node

	if err := runCatalogStep(ctx, "opening custom departure form",
		utils.ClickElement(model.BUTTON_COMMON_TAB3),
		chromedp.WaitReady(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY, chromedp.ByQuery),
		utils.ClickElement(model.LINK_FLEET_RESEARCH_CUSTOM_DEPARTURE),
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_COUNTRY_OPTIONS, &countryElemList, chromedp.ByQueryAll),
	); err != nil {
		return nil, err
	}

	return countryElemList, nil
}

// selectCountryAndListAirports selects countryName in the country selector and
// returns the resulting list of airport option nodes.
func (b *Bot) selectCountryAndListAirports(ctx context.Context, countryName string) ([]*cdp.Node, error) {
	// The game replaces the airport <select> with a brand-new element (fresh
	// HTML including a re-bound change handler — see dispatchChangeEvent's
	// doc comment) via AJAX when the country selector's change event fires.
	// Grab the *current* element's identity first, so we can tell once that
	// replacement has actually happened, rather than just checking that
	// *some* <select> matching the selector exists — one always does,
	// including the stale one left over from the previous country.
	var prevNode []*cdp.Node
	// AtLeast(0): on the very first call there's no previous country, so no
	// pre-existing airport select to compare against — that's fine, not an error.
	if err := chromedp.Run(ctx, chromedp.Nodes(model.SELECT_FLEET_RESEARCH_AIRPORT_SELECTOR, &prevNode, chromedp.ByQuery, chromedp.AtLeast(0))); err != nil {
		return nil, fmt.Errorf("reading current airport select node: %w", err)
	}

	var prevNodeID cdp.NodeID
	if len(prevNode) > 0 {
		prevNodeID = prevNode[0].NodeID
	}

	if err := runCatalogStep(ctx, "selecting country",
		chromedp.SetValue(model.SELECT_FLEET_RESEARCH_COUNTRY_SELECTOR, countryName, chromedp.ByQuery),
		dispatchChangeEvent(model.SELECT_FLEET_RESEARCH_COUNTRY_SELECTOR),
	); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(catalogStepTimeout)

	var airportElemList []*cdp.Node

	for {
		if err := chromedp.Run(ctx, chromedp.Nodes(model.SELECT_FLEET_RESEARCH_AIRPORT_SELECTOR, &airportElemList, chromedp.ByQuery, chromedp.AtLeast(0))); err != nil {
			return nil, fmt.Errorf("waiting for airport list to refresh for country %q: %w", countryName, err)
		}

		if len(airportElemList) > 0 && airportElemList[0].NodeID != prevNodeID {
			break
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("airport list for country %q never refreshed (still showing the previous country's list after %s) — "+
				"this is what caused airports to be scanned under the wrong country before", countryName, catalogStepTimeout)
		}

		time.Sleep(200 * time.Millisecond)
	}

	if err := runCatalogStep(ctx, "reading airport options",
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_AIRPORT_OPTIONS, &airportElemList, chromedp.ByQueryAll),
	); err != nil {
		return nil, err
	}

	return airportElemList, nil
}

// shouldScanCatalogAirport reports whether airportID should be scanned,
// given Conf.CatalogAirportIDs (an explicit allow-list) and
// Conf.CatalogAirportIDMin/Max (a range, for sharding a scan across multiple
// parallel instances — see their doc comment in internal/config). Both are
// optional; an airport must satisfy whichever of the two are actually set.
// Empty/zero on both sides means "scan everything".
func (b *Bot) shouldScanCatalogAirport(airportID int) bool {
	if len(b.Conf.CatalogAirportIDs) > 0 && !slices.Contains(b.Conf.CatalogAirportIDs, airportID) {
		return false
	}

	if b.Conf.CatalogAirportIDMax > 0 && (airportID < b.Conf.CatalogAirportIDMin || airportID > b.Conf.CatalogAirportIDMax) {
		return false
	}

	return true
}

// parseAirportOption extracts the numeric ID, raw option value, and display
// name from an airport <option> node. ok is false for the placeholder
// "- Select airport" option (whose value is not a positive integer).
func parseAirportOption(airportElem *cdp.Node) (id int, value string, name string, ok bool) {
	value = airportElem.AttributeValue("value")

	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, "", "", false
	}

	if len(airportElem.Children) > 0 {
		name = airportElem.Children[0].NodeValue
	}

	return id, value, name, true
}

// parseCountryAirportName splits a route result row's location text (e.g.
// "Afghanistan, Herat") into country and airport name — the game has no
// separate "city" concept, just an airport name that often matches or
// contains one. ok is false if the text doesn't contain the expected ", "
// separator.
func parseCountryAirportName(location string) (country, airportName string, ok bool) {
	parts := strings.SplitN(location, ", ", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return parts[0], parts[1], true
}

func airportHasCode(w *io.CatalogWriter, id int, codeType string) (bool, error) {
	if codeType == "iata" {
		return w.AirportHasIATA(id)
	}

	return w.AirportHasICAO(id)
}

func setAirportCode(w *io.CatalogWriter, id int, codeType, code string) error {
	if codeType == "iata" {
		return w.SetAirportIATA(id, code)
	}

	return w.SetAirportICAO(id, code)
}
