package bot

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/ashokhin/am4bot/internal/config"
	"github.com/ashokhin/am4bot/internal/io"
	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// NewScanner creates a new Bot instance specifically for scanning purposes.
func NewScanner(conf *config.Config) Bot {
	// Setup Chrome options
	opts := setupChromeOptions(conf)

	return Bot{
		Conf:       conf,
		chromeOpts: opts,
	}
}

// RunScanner runs the scanning process using the bot.
// It handles authentication and route scanning.
func (b *Bot) RunScanner(ctx context.Context) error {
	var cdpLogger chromedp.ContextOption

	timeStart := time.Now()

	slog.Debug("create execution context")

	allocatorCtx, cancel := chromedp.NewExecAllocator(ctx, b.chromeOpts...)
	defer cancel()

	if b.Conf.ChromeDebug {
		cdpLogger = chromedp.WithDebugf(log.Printf)
	} else {
		cdpLogger = chromedp.WithLogf(log.Printf)
	}

	taskCtx, cancel := chromedp.NewContext(
		allocatorCtx,
		cdpLogger,
	)
	defer cancel()

	slog.Debug("run bot", "start_time", timeStart.UTC())
	slog.Info("authentication")

	// perform authentication
	if err := b.auth(taskCtx); err != nil {
		slog.Warn("error in Bot.Run > Bot.auth", "error", err)

		return err
	}

	// perform route scanning
	if err := b.ScanRoutes(taskCtx); err != nil {
		slog.Warn("error in Bot.Run > Bot.ScanRoutes", "error", err)

		return err
	}

	// calculate total duration for Prometheus metric and logging
	duration := time.Since(timeStart)

	slog.Info("run complete", "elapsed_time", fmt.Sprint(duration))

	return nil
}

// ScanRoutes scans routes based on the configured hubs and criteria.
func (b *Bot) ScanRoutes(ctx context.Context) error {
	var HubElemList []*cdp.Node
	slog.Info("scanning routes")

	// open fleet window
	utils.DoClickElement(ctx, model.BUTTON_MAIN_FLEET)
	defer utils.DoClickElement(ctx, model.BUTTON_COMMON_CLOSE_POPUP)

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_COMMON_TAB3),
		chromedp.WaitReady(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY, chromedp.ByQuery),
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_DEPARTING_FROM, &HubElemList, chromedp.ByQueryAll),
	); err != nil {
		slog.Warn("error in Bot.ScanRoutes", "error", err)

		return err
	}

	for _, hubElem := range HubElemList {
		hubName := hubElem.Children[0].NodeValue
		nodeValue := hubElem.AttributeValue("value")
		slog.Debug("researching routes", "hub", hubName)

		if slices.Contains(b.Conf.HubsList, hubName) {
			slog.Debug("found hub from config", "hubName", hubName)
			b.Writer, _ = io.NewWriter(fmt.Sprintf("routes_%s.csv", nodeValue))

			if err := chromedp.Run(ctx,
				chromedp.SetValue(model.SELECT_FLEET_RESEARCH_DEPARTING_FROM, nodeValue, chromedp.ByQuery),
			); err != nil {
				slog.Warn("error in Bot.ScanRoutes > set departing from value", "hub", hubName, "error", err)

				return err
			}

			if err := b.searchRoutesForHub(ctx, hubName); err != nil {
				slog.Warn("error in Bot.ScanRoutes > searchRoutesForHub", "hub", hubName, "error", err)
				return err
			}
			b.Writer.Close()
		}
	}

	return nil
}

// searchRoutesForHub searches for routes from a specific hub within the configured distance range.
func (b *Bot) searchRoutesForHub(ctx context.Context, hubName string) error {
	slog.Info("searching routes", "hub", hubName, "max_distance_km", b.Conf.MaxRouteDistanceKm,
		"min_distance_km", b.Conf.MinRouteDistanceKm, "min_runway_length", b.Conf.MinRunwayLength,
		"scan_step_km", b.Conf.ScanStepKm)

	currentDistance := b.Conf.MaxRouteDistanceKm

	for currentDistance >= b.Conf.MinRouteDistanceKm {
		slog.Debug("setting max distance", "hub", hubName, "max_distance_km", currentDistance)

		if err := b.scanDistance(ctx, hubName, currentDistance); err != nil {
			slog.Warn("error in searchRoutesForHub > scanning distance", "hub", hubName, "max_distance_km", currentDistance, "error", err)

			return err
		}

		currentDistance -= b.Conf.ScanStepKm
	}
	return nil
}

// scanDistance scans routes for a specific hub up to the given maximum distance.
func (b *Bot) scanDistance(ctx context.Context, hubName string, maxDistance int) error {

	if err := chromedp.Run(ctx,
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MAX_DISTANCE, strconv.Itoa(maxDistance), chromedp.ByQuery),
		chromedp.SetValue(model.TEXTFIELD_FLEET_RESEARCH_MIN_RUNWAY, strconv.Itoa(b.Conf.MinRunwayLength), chromedp.ByQuery),
		utils.ClickElement(model.BUTTON_FLEET_RESEARCH_SEARCH),
	); err != nil {
		slog.Warn("error in searchRoutesForHub > searching routes", "hub", hubName, "max_distance_km", maxDistance, "error", err)
		return err
	}

	var routesElemList []*cdp.Node

	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, chromedp.ByQuery),
		chromedp.Nodes(model.LIST_FLEET_RESEARCH_SEARCH_RESULTS, &routesElemList, chromedp.ByQueryAll),
	); err != nil {
		slog.Warn("error in searchRoutesForHub > searching routes", "hub", hubName, "max_distance_km", maxDistance, "error", err)
		return err
	}

	if len(routesElemList) == 0 {
		slog.Debug("no routes found", "hub", hubName, "max_distance_km", maxDistance)

		return nil
	} else {
		slog.Debug("found routes", "hub", hubName, "max_distance_km", maxDistance, "routes_found", len(routesElemList))
	}

	if err := b.scanDistanceRoutes(ctx, hubName, routesElemList); err != nil {
		slog.Warn("error in searchRoutesForHub > scanning distance routes", "hub", hubName, "max_distance_km", maxDistance, "error", err)

		return err
	}

	return nil

}

// scanDistanceRoutes processes the list of route elements and writes valid routes to the CSV.
func (b *Bot) scanDistanceRoutes(ctx context.Context, hubName string, routesElemList []*cdp.Node) error {
	for _, routeElem := range routesElemList {
		var (
			from string
			to   string
		)

		if err := chromedp.Run(ctx,
			chromedp.Text(model.TEXT_FLEET_RESEARCH_ROUTE_FROM, &from, chromedp.ByQuery, chromedp.FromNode(routeElem)),
			chromedp.Text(model.TEXT_FLEET_RESEARCH_ROUTE_TO, &to, chromedp.ByQuery, chromedp.FromNode(routeElem)),
		); err != nil {
			slog.Warn("error in searchRoutesForHub > getting route info", "hub", hubName, "error", err)
			return err
		}

		// construct route key
		routeKey := fmt.Sprintf("%s-%s", from, to)
		// create route object
		route := model.Route{
			Name:        fmt.Sprintf("%s-%s", from, to),
			Distance:    utils.AtoiSafe(routeElem.AttributeValue("data-distance")),
			Runway:      utils.AtoiSafe(routeElem.AttributeValue("data-rwy")),
			DemandY:     utils.AtoiSafe(routeElem.AttributeValue("data-yclass")),
			DemandJ:     utils.AtoiSafe(routeElem.AttributeValue("data-jclass")),
			DemandF:     utils.AtoiSafe(routeElem.AttributeValue("data-fclass")),
			DemandLarge: utils.AtoiSafe(routeElem.AttributeValue("data-large")) * 1000,
			DemandHeavy: utils.AtoiSafe(routeElem.AttributeValue("data-heavy")) * 1000,
		}

		// write route to CSV
		if err := b.Writer.WriteRoute(routeKey, route); err != nil {
			slog.Warn("error in searchRoutesForHub > writing route to CSV", "hub", hubName, "route_key", routeKey, "error", err)
			return err
		}
	}

	return nil
}
