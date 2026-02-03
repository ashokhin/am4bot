package bot

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

const (
	// HUB_WEAR_PERCENT_FOR_REPAIR defines the wear percentage threshold for lounge repair
	HUB_WEAR_PERCENT_FOR_REPAIR float64 = 16.0
)

// hubs checks the status of all hubs, collects statistics, repairs lounges if needed, and buys catering.
func (b *Bot) hubs(ctx context.Context) error {
	var globalNeedRepair bool
	var err error

	slog.Info("check hubs")

	// check Alert icon for lounge on the "Flight Info" menu
	globalNeedRepair = utils.IsElementVisible(ctx, model.ICON_FI_LOUNGE_ALERT)

	slog.Debug("repair status", "need_repair", globalNeedRepair)
	slog.Debug("open pop-up window", "window", "hubs")

	// open hubs window
	if err = chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_MAIN_HUBS),
	); err != nil {
		slog.Warn("error in Bot.hubs > open hubs", "error", err)

		return err
	}

	defer utils.DoClickElement(ctx, model.BUTTON_COMMON_CLOSE_POPUP)

	slog.Debug("get list of hubs webElements")

	var hubsElemList []*cdp.Node

	// get list of Hubs
	if err = chromedp.Run(ctx,
		chromedp.Nodes(model.LIST_HUBS_HUBS, &hubsElemList, chromedp.ByQueryAll),
	); err != nil {
		slog.Warn("error in Bot.hubs > get hubs list", "error", err)

		return err
	}

	var hubsMap map[string]model.Hub
	// collect metrics for all hubs
	if hubsMap, err = b.hubsCollectMetrics(ctx, hubsElemList); err != nil {
		slog.Warn("error in Bot.hubs > Bot.hubsCollectMetrics", "error", err)

		return err
	}

	// repair lounges if needed
	if globalNeedRepair && b.Conf.RepairLounges {
		if err := b.hubsLoungesRepair(ctx, hubsMap); err != nil {
			slog.Warn("error in Bot.hubs > Bot.hubsLoungesRepair", "error", err)

			return err
		}
	}

	// buy catering
	if b.Conf.BuyCateringIfMissing {
		slog.Debug("buy catering for hubs which miss it")

		if err := b.hubsBuyCatering(ctx, hubsMap); err != nil {
			slog.Warn("error in Bot.hubs > Bot.hubsBuyCatering", "error", err)

			return err
		}
	}

	return nil
}

// hubsCollectMetrics collects Prometheus metrics for all available hubs
func (b *Bot) hubsCollectMetrics(ctx context.Context, hubsElemList []*cdp.Node) (map[string]model.Hub, error) {
	hubsMap := make(map[string]model.Hub)
	// get metrics for all hubs in hubsElemList
	for _, hubElem := range hubsElemList {
		var hub model.Hub
		var hubName string

		hub.HubCdpNode = hubElem

		slog.Debug("hubElem", "elem", hub.HubCdpNode)

		// retrieve hub statistics
		if err := chromedp.Run(ctx,
			chromedp.Text(model.TEXT_HUBS_HUB_NAME, &hubName, chromedp.ByQuery, chromedp.FromNode(hub.HubCdpNode)),
			utils.GetFloatFromChildElement(model.TEXT_HUBS_HUB_DEPARTURES, &hub.Departures, hub.HubCdpNode),
			utils.GetFloatFromChildElement(model.TEXT_HUBS_HUB_ARRIVALS, &hub.Arrivals, hub.HubCdpNode),
			utils.GetFloatFromChildElement(model.TEXT_HUBS_HUB_PAX_DEPARTED, &hub.PaxDeparted, hub.HubCdpNode),
			utils.GetFloatFromChildElement(model.TEXT_HUBS_HUB_PAX_ARRIVED, &hub.PaxArrived, hub.HubCdpNode),
		); err != nil {
			slog.Warn("error in Bot.hubsCollectMetrics > get hub info", "error", err)

			return nil, err
		}

		// check if catering is present
		hub.HasCatering = utils.IsSubElementVisible(ctx, model.ICON_HUBS_CATERING, hub.HubCdpNode)

		b.PrometheusMetrics.HubStatsTotal.WithLabelValues(hubName, "departures").Set(hub.Departures)
		b.PrometheusMetrics.HubStatsTotal.WithLabelValues(hubName, "arrivals").Set(hub.Arrivals)
		b.PrometheusMetrics.HubStatsTotal.WithLabelValues(hubName, "paxDeparted").Set(hub.PaxDeparted)
		b.PrometheusMetrics.HubStatsTotal.WithLabelValues(hubName, "paxArrived").Set(hub.PaxArrived)

		hubsMap[hubName] = hub
	}

	return hubsMap, nil
}

// hubsLoungesRepair performs repair operation for limited number of hubs. Limit comes from the
// configuration option "bot.Conf.hubs_maintenance_limit"
func (b *Bot) hubsLoungesRepair(ctx context.Context, hubsMap map[string]model.Hub) error {
	var err error
	loungesRepairCount := 0

	// open lounges maintenance tab
	if err = chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_HUBS_LOUNGES_MAINTENANCE),
	); err != nil {
		slog.Warn("error in Bot.hubsLoungesRepair > open lounges maintenance tab", "error", err)

		return err
	}

	defer utils.DoClickElement(ctx, model.BUTTON_HUBS_LOUNGES_BACK_TO_HUBS)

	// perform repair for the first N ( defined by the config option "bot.Conf.hubs_maintenance_limit")
	// hubs number in hubsMap
	for hubName, hub := range hubsMap {
		if loungesRepairCount >= b.Conf.HubsMaintenanceLimit {
			slog.Info("Maximum lounges limit for repair has been reached for this run", "hubs_maintenance_limit", b.Conf.HubsMaintenanceLimit)

			break
		}

		// collect lounges info and update hub object by reference
		if err = b.collectLoungeInfo(ctx, hubName, &hub); err != nil {
			slog.Warn("error in Bot.hubsLoungesRepair > Bot.collectLoungeInfo", "error", err)

			return err
		}

		// update hub info in hubsMap
		hubsMap[hubName] = hub

		// skip hubs which do not need repair
		if !hub.NeedsRepair {
			slog.Debug("lounge doesn't need repair", "hub", hubName)

			continue
		}

		slog.Info("repair lounge", "hub", hubName)

		if err := b.repairLounge(ctx, &hub); err != nil {
			slog.Warn("error in Bot.hubsLoungesRepair > Bot.repairLounge", "error", err)

			return err
		}

		loungesRepairCount++
	}

	slog.Debug("go back to the 'hubs' window")

	return nil
}

// collectLoungeInfo collects information about hub's lounge and overrides the hub object by reference
func (b *Bot) collectLoungeInfo(ctx context.Context, hubName string, hub *model.Hub) error {
	var err error
	var loungesElemList []*cdp.Node
	// open lounges tab and get list of lounges
	if err = chromedp.Run(ctx,
		chromedp.Nodes(model.LIST_HUBS_LOUNGES, &loungesElemList, chromedp.ByQueryAll),
	); err != nil {
		slog.Warn("error in Bot.collectLoungeInfo > get lounges list", "error", err)

		return err
	}

	// enrich lounges info into hubsMap
	for _, loungeElem := range loungesElemList {
		var loungeName string
		var loungeWearPercent float64
		var needsRepair bool

		// retrieve lounge statistics
		if err := chromedp.Run(ctx,
			chromedp.Text(model.TEXT_HUBS_LOUNGES_LOUNGE_NAME, &loungeName, chromedp.ByQuery, chromedp.FromNode(loungeElem)),
			utils.GetFloatFromChildElement(model.TEXT_HUBS_LOUNGES_LOUNGE_WEAR_PERCENT, &loungeWearPercent, loungeElem),
		); err != nil {
			slog.Warn("error in Bot.collectLoungeInfo > get lounge info", "error", err)

			return err
		}

		// standardize lounge name to upper case for further comparison
		loungeName = strings.ToUpper(loungeName)

		slog.Debug("lounge name", "name", loungeName)
		slog.Debug("lounge wear percent", "wear_percent", loungeWearPercent)

		// check if lounge needs repair
		if loungeWearPercent >= HUB_WEAR_PERCENT_FOR_REPAIR {
			needsRepair = true
		} else {
			needsRepair = false
		}

		slog.Debug("lounge needs repair", "needs_repair", needsRepair)

		// set lounge wear percent and repair status into hubsMap
		if strings.Contains(hubName, loungeName) {
			hub.NeedsRepair = needsRepair
			hub.LoungeCdpNode = loungeElem

			slog.Debug("updated hub info in hubsMap", "hub_name", hubName, "hub_info", hub)

			return nil
		}
	}

	return nil
}

// hubsBuyCatering buys catering for limited number of hubs. Limit comes from the
// configuration option "bot.Conf.hubs_maintenance_limit"
func (b *Bot) hubsBuyCatering(ctx context.Context, hubsMap map[string]model.Hub) error {
	hubsBuyCateringCount := 0
	// perform catering buy for the first N ( defined by the config option "bot.Conf.hubs_maintenance_limit")
	// hubs number in hubsMap
	for hubName, hub := range hubsMap {
		if hubsBuyCateringCount >= b.Conf.HubsMaintenanceLimit {
			slog.Info("Maximum hubs limit for catering has been reached for this run", "hubs_maintenance_limit", b.Conf.HubsMaintenanceLimit)

			break
		}

		// buy catering if not present
		if !hub.HasCatering {
			slog.Info("buy catering for hub", "hub", hubName)

			if err := b.buyCatering(ctx, hub); err != nil {
				slog.Warn("error in Bot.hubsBuyCatering > Bot.buyCatering", "error", err)

				return err
			}

			hubsBuyCateringCount++
		}
	}

	return nil
}

// repairLounge repairs the lounge of a specific hub if the repair cost is within the maintenance budget.
func (b *Bot) repairLounge(ctx context.Context, hub *model.Hub) error {
	slog.Debug("repair lounge function")

	var loungeRepairCost float64

	if utils.IsSubElementVisible(ctx, model.TEXT_HUBS_LOUNGES_LOUNGE_REPAIR_COST, hub.LoungeCdpNode) {
		// get repair cost
		if err := chromedp.Run(ctx,
			utils.GetFloatFromChildElement(model.TEXT_HUBS_LOUNGES_LOUNGE_REPAIR_COST, &loungeRepairCost, hub.LoungeCdpNode),
		); err != nil {
			slog.Warn("error in Bot.repairLounge > get lounge repair cost", "error", err)

			return err
		}
	} else {
		slog.Debug("lounge repair cost element isn't visible, set repair cost to 0.0")

		return nil
	}

	slog.Debug("repair cost", "value", int(loungeRepairCost))

	slog.Debug("available money", "value", int(b.BudgetMoney.Maintenance))

	if loungeRepairCost > b.BudgetMoney.Maintenance {
		slog.Warn("lounge repair is too expensive", "cost", int(loungeRepairCost),
			"budget", int(b.BudgetMoney.Maintenance))

		return nil
	}

	slog.Info("repair lounge", "repairCost", int(loungeRepairCost),
		"BudgetMoney.Maintenance", int(b.BudgetMoney.Maintenance))

	if err := chromedp.Run(ctx,
		chromedp.Click(model.BUTTON_HUBS_LOUNGES_LOUNGE_REPAIR, chromedp.ByQuery, chromedp.FromNode(hub.LoungeCdpNode)),
	); err != nil {
		slog.Warn("error in Bot.repairLounge > click repair", "error", err)

		return err
	}

	// reduce current account money and maintenance budged by repair cost
	b.AccountBalance -= loungeRepairCost
	b.BudgetMoney.Maintenance -= loungeRepairCost

	// after clicking the "repair" button,
	// lounges grid is redrawn, so we need to re-open lounges maintenance tab
	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_HUBS_LOUNGES_BACK_TO_HUBS),
		utils.ClickElement(model.BUTTON_HUBS_LOUNGES_MAINTENANCE),
	); err != nil {
		slog.Warn("error in Bot.repairLounge > reopen lounges tab", "error", err)

		return err
	}

	return nil
}

// buyCatering buys catering for a specific hub if the catering cost is within the maintenance budget.
func (b *Bot) buyCatering(ctx context.Context, hub model.Hub) error {
	slog.Debug("Buy catering function")

	if err := chromedp.Run(ctx,
		chromedp.Click(model.ELEMENT_HUB, chromedp.ByQuery, chromedp.FromNode(hub.HubCdpNode)),
	); err != nil {
		slog.Warn("error in Bot.buyCatering > select hub", "error", err)

		return err
	}

	// return to list of hubs when exiting from function
	defer utils.DoClickElement(ctx, model.BUTTON_HUBS_HUB_MANAGE_BACK)

	if !utils.IsElementVisible(ctx, model.BUTTON_HUBS_ADD_CATERING) {
		slog.Warn("button '+ Add catering' isn't visible")

		return nil
	}

	var cateringCost float64

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_HUBS_ADD_CATERING),
		chromedp.WaitReady(model.ELEM_HUBS_CATERING_OPTION_3, chromedp.ByQuery),
		utils.ClickElement(model.ELEM_HUBS_CATERING_OPTION_3),
		chromedp.SetValue(model.SELECT_HUBS_CATERING_DURATION, b.Conf.CateringDurationHours, chromedp.ByQuery),
		chromedp.SetValue(model.SELECT_HUBS_CATERING_AMOUNT, b.Conf.CateringAmountOption, chromedp.ByQuery),
		utils.GetFloatFromElement(model.TEXT_HUBS_CATERING_COST, &cateringCost),
	); err != nil {
		slog.Warn("error in Bot.buyCatering > select hub", "error", err)

		return err
	}

	if cateringCost > b.BudgetMoney.Maintenance {
		slog.Warn("catering is too expensive", "cost", int(cateringCost),
			"budget", int(b.BudgetMoney.Maintenance))

		return nil
	}

	// buy catering
	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_HUBS_CATERING_BUY),
	); err != nil {
		slog.Warn("error in Bot.buyCatering > buy catering", "error", err)

		return err
	}

	// reduce current account money and maintenance budget by catering cost
	b.AccountBalance -= cateringCost
	b.BudgetMoney.Maintenance -= cateringCost

	return nil
}
