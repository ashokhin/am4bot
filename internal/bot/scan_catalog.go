package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ashokhin/am4bot/internal/io"
	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

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
	defer cancel()

	countryElemList, err := b.openCustomDepartureForm(taskCtx)
	if err != nil {
		return err
	}

	for _, countryElem := range countryElemList {
		countryName := countryElem.AttributeValue("value")
		if countryName == "" {
			continue
		}

		slog.Info("scanning country", "country", countryName)

		airportElemList, err := b.selectCountryAndListAirports(taskCtx, countryName)
		if err != nil {
			slog.Warn("error listing airports for country", "country", countryName, "error", err)

			continue
		}

		for _, airportElem := range airportElemList {
			b.scanCatalogAirportEntry(taskCtx, writer, airportElem, countryName)
		}
	}

	return nil
}

// scanCatalogAirportEntry upserts a single airport and, if its route sweep
// hasn't already completed, performs the full distance sweep for it.
func (b *Bot) scanCatalogAirportEntry(ctx context.Context, writer *io.CatalogWriter, airportElem *cdp.Node, countryName string) {
	airportID, airportValue, airportName, ok := parseAirportOption(airportElem)
	if !ok {
		return
	}

	if err := writer.UpsertAirport(airportID, airportName, countryName); err != nil {
		slog.Warn("error upserting airport", "airport_id", airportID, "error", err)

		return
	}

	scanned, err := writer.AirportScanned(airportID)
	if err != nil {
		slog.Warn("error checking scanned state", "airport_id", airportID, "error", err)

		return
	}

	if scanned {
		slog.Debug("airport already scanned, skipping", "airport_id", airportID, "name", airportName)

		return
	}

	slog.Info("scanning airport", "airport_id", airportID, "name", airportName, "country", countryName)

	if err := b.scanCatalogAirport(ctx, writer, airportID, airportValue); err != nil {
		slog.Warn("error scanning airport routes", "airport_id", airportID, "name", airportName, "error", err)

		return
	}

	if err := writer.MarkAirportScanned(airportID); err != nil {
		slog.Warn("error marking airport scanned", "airport_id", airportID, "error", err)
	}
}

// scanCatalogAirport selects airportValue as the custom-departure origin and
// sweeps the full configured distance range (b.Conf.MaxRouteDistanceKm down to
// MinRouteDistanceKm, in ScanStepKm steps — the same window-narrowing approach
// ScanRoutes uses to work around the game's 50-results-per-query cap), writing
// every discovered route to the catalog.
func (b *Bot) scanCatalogAirport(ctx context.Context, writer *io.CatalogWriter, airportID int, airportValue string) error {
	if err := chromedp.Run(ctx,
		chromedp.SetValue(model.SELECT_FLEET_RESEARCH_AIRPORT_SELECTOR, airportValue, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("selecting airport: %w", err)
	}

	currentDistance := b.Conf.MaxRouteDistanceKm

	for currentDistance >= b.Conf.MinRouteDistanceKm {
		if err := b.scanCatalogDistance(ctx, writer, airportID, currentDistance); err != nil {
			return err
		}

		currentDistance -= b.Conf.ScanStepKm

		select {
		case b.ProgressChan <- struct{}{}:
		default:
		}
	}

	return nil
}

// scanCatalogDistance searches routes from the already-selected origin up to
// maxDistance and writes every result row to the catalog.
func (b *Bot) scanCatalogDistance(ctx context.Context, writer *io.CatalogWriter, fromID, maxDistance int) error {
	if err := chromedp.Run(ctx,
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MAX_DISTANCE, strconv.Itoa(maxDistance), chromedp.ByQuery),
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY, strconv.Itoa(b.Conf.MinRunwayLength), chromedp.ByQuery),
		utils.ClickElement(model.BUTTON_FLEET_RESEARCH_SEARCH),
	); err != nil {
		return fmt.Errorf("searching routes: %w", err)
	}

	var routesElemList []*cdp.Node

	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, chromedp.ByQuery),
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, &routesElemList, chromedp.ByQueryAll),
	); err != nil {
		return fmt.Errorf("reading results: %w", err)
	}

	for _, routeElem := range routesElemList {
		depID, arrID, err := utils.ParseDepArr(routeElem.AttributeValue("onclick"))
		if err != nil {
			slog.Warn("error parsing dep/arr from result row", "error", err)

			continue
		}

		if depID != fromID {
			slog.Warn("unexpected dep ID in result row", "expected", fromID, "got", depID)
		}

		route := io.CatalogRoute{
			FromAirportID: fromID,
			ToAirportID:   arrID,
			DistanceKm:    utils.AtoiSafe(routeElem.AttributeValue("data-distance")),
			RunwayFt:      utils.AtoiSafe(routeElem.AttributeValue("data-rwy")),
			DemandY:       utils.AtoiSafe(routeElem.AttributeValue("data-yclass")),
			DemandJ:       utils.AtoiSafe(routeElem.AttributeValue("data-jclass")),
			DemandF:       utils.AtoiSafe(routeElem.AttributeValue("data-fclass")),
			DemandLarge:   utils.AtoiSafe(routeElem.AttributeValue("data-large")) * 1000,
			DemandHeavy:   utils.AtoiSafe(routeElem.AttributeValue("data-heavy")) * 1000,
		}

		if err := writer.WriteRoute(route); err != nil {
			return fmt.Errorf("writing route: %w", err)
		}
	}

	return nil
}

// ScanAirportCodes performs a lightweight scan — one minimal search per
// airport, instead of a full distance sweep — to capture each airport's IATA
// or ICAO code, whichever the account's code-display setting currently shows.
// The caller must set the desired display mode in-game before running this;
// it is a single account-wide toggle, not something this scan can change.
// Airports that already have the requested code recorded are skipped, so this
// can be safely resumed too.
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
	defer cancel()

	countryElemList, err := b.openCustomDepartureForm(taskCtx)
	if err != nil {
		return err
	}

	for _, countryElem := range countryElemList {
		countryName := countryElem.AttributeValue("value")
		if countryName == "" {
			continue
		}

		airportElemList, err := b.selectCountryAndListAirports(taskCtx, countryName)
		if err != nil {
			slog.Warn("error listing airports for country", "country", countryName, "error", err)

			continue
		}

		for _, airportElem := range airportElemList {
			b.scanAirportCodeEntry(taskCtx, writer, airportElem, codeType)
		}
	}

	return nil
}

func (b *Bot) scanAirportCodeEntry(ctx context.Context, writer *io.CatalogWriter, airportElem *cdp.Node, codeType string) {
	airportID, airportValue, airportName, ok := parseAirportOption(airportElem)
	if !ok {
		return
	}

	hasCode, err := airportHasCode(writer, airportID, codeType)
	if err != nil {
		slog.Warn("error checking existing code", "airport_id", airportID, "error", err)

		return
	}

	if hasCode {
		return
	}

	code, err := b.fetchAirportCode(ctx, airportValue)
	if err != nil {
		slog.Warn("error fetching airport code", "airport_id", airportID, "name", airportName, "error", err)

		return
	}

	if code == "" {
		slog.Debug("airport has no outgoing routes, cannot determine code", "airport_id", airportID, "name", airportName)

		return
	}

	if err := setAirportCode(writer, airportID, codeType, code); err != nil {
		slog.Warn("error saving airport code", "airport_id", airportID, "error", err)

		return
	}

	slog.Debug("captured airport code", "airport_id", airportID, "name", airportName, "code_type", codeType, "code", code)
}

// fetchAirportCode selects airportValue as the custom-departure origin, runs
// one broad search, and returns the "from" code shown on the first result row
// — that airport's own code under the currently active IATA/ICAO display mode.
// Returns an empty string (not an error) if the airport has no routes.
func (b *Bot) fetchAirportCode(ctx context.Context, airportValue string) (string, error) {
	if err := chromedp.Run(ctx,
		chromedp.SetValue(model.SELECT_FLEET_RESEARCH_AIRPORT_SELECTOR, airportValue, chromedp.ByQuery),
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MAX_DISTANCE, strconv.Itoa(b.Conf.MaxRouteDistanceKm), chromedp.ByQuery),
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY, "0", chromedp.ByQuery),
		utils.ClickElement(model.BUTTON_FLEET_RESEARCH_SEARCH),
	); err != nil {
		return "", fmt.Errorf("searching routes: %w", err)
	}

	// Bounded wait: an airport with zero routes never renders a result row, so
	// an unbounded WaitVisible would hang forever. A short timeout here treats
	// "no results appeared in time" as "no routes" rather than a scan failure.
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()

	if err := chromedp.Run(waitCtx, chromedp.WaitVisible(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, chromedp.ByQuery)); err != nil {
		return "", nil //nolint:nilerr // timeout here means "no routes", not a scan failure
	}

	var routesElemList []*cdp.Node
	if err := chromedp.Run(ctx,
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, &routesElemList, chromedp.ByQueryAll),
	); err != nil {
		return "", fmt.Errorf("reading results: %w", err)
	}

	if len(routesElemList) == 0 {
		return "", nil
	}

	var code string
	if err := chromedp.Run(ctx,
		chromedp.Text(model.TEXT_FLEET_RESEARCH_ROUTE_FROM, &code, chromedp.ByQuery, chromedp.FromNode(routesElemList[0])),
	); err != nil {
		return "", fmt.Errorf("reading airport code: %w", err)
	}

	return code, nil
}

// openCustomDepartureForm opens the fleet "Research" tab and switches to the
// "Custom departure" panel, returning the list of country option nodes.
func (b *Bot) openCustomDepartureForm(ctx context.Context) ([]*cdp.Node, error) {
	utils.DoClickElement(ctx, model.BUTTON_MAIN_FLEET)

	var countryElemList []*cdp.Node

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_COMMON_TAB3),
		chromedp.WaitReady(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY, chromedp.ByQuery),
		utils.ClickElement(model.LINK_FLEET_RESEARCH_CUSTOM_DEPARTURE),
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_COUNTRY_OPTIONS, &countryElemList, chromedp.ByQueryAll),
	); err != nil {
		return nil, fmt.Errorf("opening custom departure form: %w", err)
	}

	return countryElemList, nil
}

// selectCountryAndListAirports selects countryName in the country selector and
// returns the resulting list of airport option nodes.
func (b *Bot) selectCountryAndListAirports(ctx context.Context, countryName string) ([]*cdp.Node, error) {
	var airportElemList []*cdp.Node

	if err := chromedp.Run(ctx,
		chromedp.SetValue(model.SELECT_FLEET_RESEARCH_COUNTRY_SELECTOR, countryName, chromedp.ByQuery),
		chromedp.WaitReady(model.SELECT_FLEET_RESEARCH_AIRPORT_SELECTOR, chromedp.ByQuery),
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_AIRPORT_OPTIONS, &airportElemList, chromedp.ByQueryAll),
	); err != nil {
		return nil, err
	}

	return airportElemList, nil
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
