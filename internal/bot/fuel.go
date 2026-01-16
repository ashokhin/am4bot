package bot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/chromedp"
)

// FUEL_MINIMUM_AMOUNT defines the minimum amount of fuel that system allows to be purchased
const FUEL_MINIMUM_AMOUNT float64 = 1000.00

// fuel is checking fuel levels and buying them if price is good
// or levels are critical
func (b *Bot) fuel(ctx context.Context) error {
	slog.Info("check fuel")

	fuelList := []model.Fuel{
		{
			FuelType: "fuel",
		},
		{
			FuelType: "co2",
		},
	}

	// open fuel window
	utils.DoClickElement(ctx, model.BUTTON_MAIN_FUEL)
	defer utils.DoClickElement(ctx, model.BUTTON_COMMON_CLOSE_POPUP)

	// iterate over fuel types
	for _, fuelEntry := range fuelList {
		slog.Debug("processing fuel type", "type", fuelEntry.FuelType)

		if err := b.checkFuelType(ctx, &fuelEntry); err != nil {
			slog.Warn("error from Bot.fuel > checkFuelType", "type",
				fuelEntry.FuelType, "error", err)

			return err
		}

		slog.Debug("fuel collected", "type", fuelEntry.FuelType, "fuel", fuelEntry)

		// skip full fuel type if already full
		if fuelEntry.IsFull {
			slog.Debug("fuel is full", "type", fuelEntry.FuelType)

			continue
		}

		// try to buy fuel
		if err := b.buyFuelType(ctx, &fuelEntry); err != nil {
			slog.Warn("error from Bot.fuel > buyFuelType", "type",
				fuelEntry.FuelType, "error", err)

			return err
		}
	}

	return nil
}

// checkFuelType retrieves fuel information for a specific fuel type
func (b *Bot) checkFuelType(ctx context.Context, fuelStruct *model.Fuel) error {
	slog.Debug("check fuel type", "type", fuelStruct.FuelType)

	// select fuel tab depending on fuel type
	switch fuelStruct.FuelType {
	case "fuel":
		utils.DoClickElement(ctx, model.BUTTON_COMMON_TAB1)
	case "co2":
		utils.DoClickElement(ctx, model.BUTTON_COMMON_TAB2)
	}

	// retrieve fuel information
	if err := chromedp.Run(ctx,
		utils.GetFloatFromElement(model.TEXT_FUEL_FUEL_PRICE, &fuelStruct.Price),
		utils.GetFloatFromElement(model.TEXT_FUEL_FUEL_HOLDING, &fuelStruct.Holding),
		utils.GetFloatFromElement(model.TEXT_FUEL_FUEL_CAPACITY, &fuelStruct.Capacity),
	); err != nil {
		slog.Warn("error in Bot.checkFuelType", "type", fuelStruct.FuelType, "error", err)

		return err
	}

	slog.Debug("set prometheus metrics", "type", fuelStruct.FuelType, "fuel", *fuelStruct)

	b.PrometheusMetrics.FuelHolding.WithLabelValues(fuelStruct.FuelType).Set(fuelStruct.Holding)
	b.PrometheusMetrics.FuelLimit.WithLabelValues(fuelStruct.FuelType).Set(fuelStruct.Capacity)
	b.PrometheusMetrics.FuelPrice.WithLabelValues(fuelStruct.FuelType).Set(fuelStruct.Price)

	// Set IsFull using helper for clarity.
	fuelStruct.IsFull = isFuelFull(fuelStruct.Capacity, fuelStruct.Holding)

	return nil
}

// isFuelFull determines if the fuel tank is considered full based on the minimum amount threshold.
func isFuelFull(capacity, holding float64) bool {
	needAmount := capacity - holding
	return needAmount < FUEL_MINIMUM_AMOUNT
}

// buyFuelType attempts to purchase fuel of a specific type based on budget and price conditions
func (b *Bot) buyFuelType(ctx context.Context, fuelStruct *model.Fuel) error {
	var fuelExpectedPrice float64

	slog.Debug("buy fuel type", "type", fuelStruct.FuelType)

	// select fuel tab depending on fuel type and set expected price
	switch fuelStruct.FuelType {
	case "fuel":
		utils.DoClickElement(ctx, model.BUTTON_COMMON_TAB1)
		fuelExpectedPrice = b.Conf.FuelPrice.Fuel
	case "co2":
		utils.DoClickElement(ctx, model.BUTTON_COMMON_TAB2)
		fuelExpectedPrice = b.Conf.FuelPrice.Co2
	}

	// calculate fuel need amount and price
	fuelNeedAmount := fuelStruct.Capacity - fuelStruct.Holding
	fuelKeepAmountPercent := (fuelStruct.Holding / fuelStruct.Capacity) * 100
	// price per 1000 Lbs/Quotas
	amountPrice := (fuelNeedAmount * fuelStruct.Price) / 1000

	// if fuel less than critical_percent then buy fuel anyway
	if fuelKeepAmountPercent <= b.Conf.FuelCriticalPercent {
		slog.Info("not enough fuel (less than fuel_critical_percent)", "type", fuelStruct.FuelType,
			"keepPercent", int(fuelKeepAmountPercent),
			"critical_percent", int(b.Conf.FuelCriticalPercent))
	} else if fuelStruct.Price > fuelExpectedPrice { // else if fuelPrice more that expectedPrice then exit
		slog.Info("fuel is too expensive", "type", fuelStruct.FuelType, "price", int(fuelStruct.Price),
			"expected", int(fuelExpectedPrice))

		return nil
	} else if amountPrice > b.BudgetMoney.Fuel { // else if amountPrice more than budget then exit
		slog.Info("not enough money for buying fuel", "type", fuelStruct.FuelType, "need", int(amountPrice),
			"budget", int(b.BudgetMoney.Fuel))

		return nil
	}
	// define fuel amount string for input field
	fuelNeedAmountString := fmt.Sprintf("%d", int(fuelNeedAmount))

	slog.Debug("buying fuel", "type", fuelStruct.FuelType, "amount", fuelNeedAmountString, "price", int(amountPrice))

	// perform buy fuel action
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(model.TEXT_FIELD_FUEL_AMOUNT, fuelNeedAmountString, chromedp.ByQuery),
		utils.ClickElement(model.BUTTON_FUEL_BUY),
	); err != nil {
		slog.Warn("error in Bot.buyFuelType", "type", fuelStruct.FuelType, "error", err)

		return err
	}

	slog.Debug("money before", "AccountBalance", int(b.AccountBalance), "fuelBudget", int(b.BudgetMoney.Fuel))
	// update bot money values after purchase
	b.AccountBalance -= amountPrice
	b.BudgetMoney.Fuel -= amountPrice
	slog.Debug("money after", "AccountBalance", int(b.AccountBalance), "fuelBudget", int(b.BudgetMoney.Fuel))

	return nil
}
