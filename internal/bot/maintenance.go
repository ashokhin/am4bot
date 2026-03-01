package bot

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// maintenance performs maintenance operations on aircraft, including A-Checks, repairs, and modifications.
func (b *Bot) maintenance(ctx context.Context) error {
	slog.Info("start aircraft maintenance")

	slog.Debug("open pop-up window", "window", "maintenance")
	// open the "Maintenance" pop-up
	utils.DoClickElement(ctx, model.BUTTON_MAIN_MAINTENANCE)

	defer utils.DoClickElement(ctx, model.BUTTON_COMMON_CLOSE_POPUP)

	// perform the 'A-Check' operation on all eligible aircraft
	if err := b.aCheckAllAircraft(ctx); err != nil {
		slog.Warn("error in Bot.maintenance > Bot.aCheckAllAircraft", "error", err)

		return err
	}

	// perform the 'Repair' operation on all eligible aircraft
	if err := b.repairAllAircraft(ctx); err != nil {
		slog.Warn("error in Bot.maintenance > Bot.repairAllAircraft", "error", err)

		return err
	}

	// perform the 'Modify' operation on all eligible aircraft
	if err := b.modifyAllAircraft(ctx); err != nil {
		slog.Warn("error in Bot.maintenance > Bot.modifyAllAircraft", "error", err)

		return err
	}

	return nil
}

// aCheckAllAircraft performs A-Check maintenance on all eligible aircraft.
func (b *Bot) aCheckAllAircraft(ctx context.Context) error {
	var aircraftNeedACheck int
	var aircraftElemList []*cdp.Node

	slog.Info("search aircraft which need A-Check")
	slog.Debug("get list of aircraftElements")

	if err := chromedp.Run(ctx,
		// open "Plan +" tab
		utils.ClickElement(model.BUTTON_COMMON_TAB2),
		// click on "Bulk A-Check" button
		utils.ClickElement(model.BUTTON_MAINTENANCE_BULK_ACHECK),
		// search all "aircraft" rows
		chromedp.Nodes(model.LIST_MAINTENANCE_BULK_ACHECK_AC_LIST, &aircraftElemList, chromedp.ByQueryAll),
	); err != nil {
		slog.Warn("error in Bot.aCheckAllAircraft > get aircraftElements list", "error", err)

		return err
	}

	// Select all eligible aircraft for A-Check maintenance and count total A-Check cost
	for _, aircraftElem := range aircraftElemList {
		var acACheckHours int

		if err := chromedp.Run(ctx,
			utils.GetIntFromChildElement(model.TEXT_MAINTENANCE_BULK_ACHECK_HOURS, &acACheckHours, aircraftElem),
		); err != nil {
			slog.Warn("error in Bot.aCheckAllAircraft > utils.GetIntFromChildElement", "error", err)

			continue
		}

		if acACheckHours > b.Conf.AircraftMaxHoursToCheck {
			slog.Debug("skip aircraft", "a-check hours", acACheckHours)

			continue
		}

		slog.Debug("add aircraft for a-check", "a-check hours", acACheckHours)

		if err := chromedp.Run(ctx,
			chromedp.Click(model.TEXT_MAINTENANCE_BULK_ACHECK_HOURS, chromedp.ByQuery, chromedp.FromNode(aircraftElem)),
		); err != nil {
			slog.Warn("error in Bot.aCheckAllAircraft > click 'Plan' button for aircraft", "error", err)

			continue
		}

		aircraftNeedACheck++
	}

	if aircraftNeedACheck == 0 {
		slog.Info("no aircraft need A-Check")

		return nil
	}

	// Get total A-Check cost for all selected aircraft
	var totalACheckCost float64

	if err := chromedp.Run(ctx,
		utils.GetFloatFromElement(model.TEXT_MAINTENANCE_BULK_ACHECK_COST, &totalACheckCost),
	); err != nil {
		slog.Warn("error in Bot.aCheckAllAircraft > get total A-Check cost", "error", err)

		return err
	}

	slog.Info("found aircraft for a-check", "count", aircraftNeedACheck, "totalCost", int(totalACheckCost))

	if totalACheckCost > b.BudgetMoney.Maintenance {
		slog.Warn("total A-Check maintenance cost is too expensive", "cost", int(totalACheckCost),
			"budget", int(b.BudgetMoney.Maintenance), "operation", "a-check")

		return nil
	}

	slog.Info("plan A-Check maintenance for selected aircraft", "count", aircraftNeedACheck, "totalCost", int(totalACheckCost))

	// Click the "Plan bulk check" button to schedule A-Check maintenance for all selected aircraft
	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_MAINTENANCE_BULK_ACHECK_PLAN),
	); err != nil {
		slog.Warn("error in Bot.aCheckAllAircraft > plan A-Check maintenance for selected aircraft", "error", err)

		return err
	}

	// update budget and account balance
	b.BudgetMoney.Maintenance -= totalACheckCost
	b.AccountBalance -= totalACheckCost

	return nil
}

// repairAllAircraft performs repair maintenance on all eligible aircraft.
func (b *Bot) repairAllAircraft(ctx context.Context) error {
	slog.Info("search aircraft which need repair")
	slog.Debug("get list of aircraftElements")

	if err := chromedp.Run(ctx,
		// open "Plan +" tab
		utils.ClickElement(model.BUTTON_COMMON_TAB2),
		// open "Bulk Repair" menu
		utils.ClickElement(model.BUTTON_MAINTENANCE_BULK_REPAIR),
		// set "Repair %" filter
		chromedp.SetValue(model.SELECT_MAINTENANCE_BULK_REPAIR_PERCENT, b.Conf.AircraftWearPercent, chromedp.ByQuery),
	); err != nil {
		slog.Warn("error in Bot.repairAllAircraft > set repair value filter", "error", err)

		return err
	}

	// Check if repair cost is visible after setting the filter, if not - then no aircraft need repair
	if !utils.IsElementVisible(ctx, model.TEXT_MAINTENANCE_BULK_REPAIR_COST) {
		slog.Info("no aircraft need repair")

		return nil
	}

	var totalRepairCost float64

	if err := chromedp.Run(ctx,
		utils.GetFloatFromElement(model.TEXT_MAINTENANCE_BULK_REPAIR_COST, &totalRepairCost),
	); err != nil {
		slog.Warn("error in Bot.repairAllAircraft > get total repair cost", "error", err)

		return err
	}

	slog.Info("found aircraft for repair", "totalCost", int(totalRepairCost))

	if totalRepairCost > b.BudgetMoney.Maintenance {
		slog.Warn("total repair maintenance cost is too expensive", "cost", int(totalRepairCost),
			"budget", int(b.BudgetMoney.Maintenance), "operation", "repair")

		return nil
	}

	slog.Info("plan repair maintenance for selected aircraft", "totalCost", int(totalRepairCost))

	time.Sleep(10 * time.Minute)

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_MAINTENANCE_BULK_REPAIR_PLAN),
	); err != nil {
		slog.Warn("error in Bot.repairAllAircraft > plan repair maintenance for selected aircraft", "error", err)

		return err
	}

	// update budget and account balance
	b.BudgetMoney.Maintenance -= totalRepairCost
	b.AccountBalance -= totalRepairCost

	return nil
}

// repairAllAircraft performs repair maintenance on all eligible aircraft.
func (b *Bot) modifyAllAircraft(ctx context.Context) error {
	var aircraftPlaned int
	var aircraftNeedModify []model.Aircraft
	var aircraftElemList []*cdp.Node

	slog.Info("search aircraft which need modify")
	slog.Debug("get list of aircraftElements")

	if err := chromedp.Run(ctx,
		// open "Plan +" tab
		utils.ClickElement(model.BUTTON_COMMON_TAB2),
		// click on "Base only" button
		utils.ClickElement(model.BUTTON_MAINTENANCE_BASE_ONLY),
		// search all "aircraft" rows
		chromedp.Nodes(model.LIST_MAINTENANCE_AC_LIST, &aircraftElemList, chromedp.ByQueryAll),
	); err != nil {
		slog.Warn("error in Bot.modifyAllAircraft > get aircraftElements list", "error", err)

		return err
	}

	// the "Maintenance list" element is dynamic, it means that we have to search
	// every aircraft individually by it's reg.number, inside the Bot.maintenanceAcByType function

	// create "aircraft" list
	for _, aircraftElem := range aircraftElemList {

		var aircraft model.Aircraft

		aircraft.RegNumber = aircraftElem.AttributeValue(model.TEXT_MAINTENANCE_AC_REG_NUMBER)
		aircraft.AcType = aircraftElem.AttributeValue(model.TEXT_MAINTENANCE_AC_TYPE)

		slog.Debug("add aircraft for modify check", "aircraft", aircraft.RegNumber)

		aircraftNeedModify = append(aircraftNeedModify, aircraft)
	}

	slog.Debug("sort and slice aircraft for modify list", "slice_limit", b.Conf.AircraftModifyLimit)

	// sort "aircraft" list by reg.number and get only last "Conf.AircraftModifyLimit" number of aircraft
	// Note: Sorting is lexicographical; registration numbers with mixed formats or without zero-padding may not sort numerically.
	// If numerical sorting is required, normalize RegNumber before sorting.
	sort.Slice(aircraftNeedModify, func(i, j int) bool {
		return aircraftNeedModify[i].RegNumber < aircraftNeedModify[j].RegNumber
	})

	if len(aircraftNeedModify) >= b.Conf.AircraftModifyLimit {
		aircraftNeedModify = aircraftNeedModify[len(aircraftNeedModify)-b.Conf.AircraftModifyLimit:]
	}

	slog.Debug("sorted and sliced aircraft for modify list", "list_length", len(aircraftNeedModify), "list", aircraftNeedModify)

	for _, aircraft := range aircraftNeedModify {
		slog.Debug("try to modify aircraft", "aircraft", aircraft.RegNumber)

		if mntOperationPerformed, err := b.modifyAc(ctx, aircraft); err != nil {
			slog.Warn("error in Bot.modifyAllAircraft > Bot.maintenanceAcByType", "error", err)

			return err
		} else if mntOperationPerformed {
			aircraftPlaned++
		}
	}

	if aircraftPlaned > 0 {
		slog.Info("aircraft modify planed", "count", aircraftPlaned)
	} else {
		slog.Info("no aircraft need modification")
	}

	return nil
}

// modifyAc performs a specific maintenance operation (A-Check, Repair, Modify) on a given aircraft.
func (b *Bot) modifyAc(ctx context.Context, ac model.Aircraft) (bool, error) {
	var mntOperationCost float64
	var acWebElemNode *cdp.Node

	slog.Debug("modify aircraft", "reg.number", strings.ToUpper(ac.RegNumber))
	slog.Debug("get aircraft rows")

	var aircraftElemList []*cdp.Node

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_COMMON_TAB2),
		chromedp.Nodes(model.LIST_MAINTENANCE_AC_LIST, &aircraftElemList, chromedp.ByQueryAll),
	); err != nil {
		slog.Warn("error in Bot.modifyAc > get aircraftElements list", "error", err)

		return false, err
	}

	slog.Debug("search aircraft row")

	for _, acElem := range aircraftElemList {
		if ac.RegNumber == acElem.AttributeValue(model.TEXT_MAINTENANCE_AC_REG_NUMBER) {
			slog.Debug("row found")

			acWebElemNode = acElem

			break
		}
	}

	if acWebElemNode == nil {
		slog.Warn("aircraft row not found", "reg.number", strings.ToUpper(ac.RegNumber))

		return false, nil
	}

	slog.Debug("get cost for aircraft modification", "reg.number", strings.ToUpper(ac.RegNumber))

	// open modification window
	if err := chromedp.Run(ctx,
		chromedp.Click(model.BUTTON_MAINTENANCE_MODIFY, chromedp.ByQuery, chromedp.FromNode(acWebElemNode)),
	); err != nil {
		slog.Warn("error in Bot.modifyAc > open modification window", "error", err)

		return false, err
	}

	// select all available modification options
	if err := chromedp.Run(ctx,
		chromedp.Click(model.CHECKBOX_MAINTENANCE_MODIFY_MOD1, chromedp.ByQuery),
		chromedp.Click(model.CHECKBOX_MAINTENANCE_MODIFY_MOD2, chromedp.ByQuery),
		chromedp.Click(model.CHECKBOX_MAINTENANCE_MODIFY_MOD3, chromedp.ByQuery),
	); err != nil {
		slog.Warn("error in Bot.modifyAc > flag 'modify' options", "error", err)

		return false, err
	}

	// get final cost for maintenance operation
	if err := chromedp.Run(ctx,
		utils.GetFloatFromElement(model.TEXT_MAINTENANCE_MODIFY_TOTAL_COST, &mntOperationCost),
	); err != nil {
		slog.Warn("error in Bot.modifyAc > get operation cost", "error", err)

		return false, err
	}

	slog.Debug("modification cost", "cost", int(mntOperationCost), "reg.number", strings.ToUpper(ac.RegNumber))

	if mntOperationCost == 0 {
		slog.Debug("modification cost is $0")

		return false, nil
	}

	if mntOperationCost > b.BudgetMoney.Maintenance {
		slog.Warn("modification is too expensive", "cost", int(mntOperationCost),
			"budget", int(b.BudgetMoney.Maintenance), "operation", "modify",
			"reg.number", strings.ToUpper(ac.RegNumber))

		return false, nil
	}

	slog.Info("plan modification", "reg.number", strings.ToUpper(ac.RegNumber))

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_MAINTENANCE_PLAN_MODIFY),
	); err != nil {
		slog.Warn("error in Bot.modifyAc > plan modification operation", "error", err)

		return false, err
	}

	// update budget and account balance
	b.BudgetMoney.Maintenance -= mntOperationCost
	b.AccountBalance -= mntOperationCost

	return true, nil
}
