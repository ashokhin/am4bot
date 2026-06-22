package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// money checks account balances and updates the bot's budget allocations accordingly.
func (b *Bot) money(ctx context.Context) error {
	var accElemList []*cdp.Node

	slog.Info("check account money")
	slog.Debug("get accounts list")

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_MAIN_ACCOUNT),
		chromedp.Nodes(model.LIST_ACCOUNT_ACCOUNTS, &accElemList, chromedp.ByQueryAll),
	); err != nil {
		return fmt.Errorf("money: get accounts list: %w", err)
	}

	defer utils.DoClickElement(ctx, model.BUTTON_COMMON_CLOSE_POPUP)

	for idx, accountElem := range accElemList {
		var accountName string
		var accountBalance float64

		slog.Debug("check account", "index", idx)

		if err := chromedp.Run(ctx,
			chromedp.Text(model.TEXT_ACCOUNT_ACCOUNT_NAME, &accountName, chromedp.ByQuery, chromedp.FromNode(accountElem)),
			utils.GetFloatFromChildElement(model.TEXT_ACCOUNT_ACCOUNT_BALANCE, &accountBalance, accountElem),
		); err != nil {
			return fmt.Errorf("money: get account info: %w", err)
		}

		accountName = strings.TrimSpace(accountName)

		slog.Debug("account balance", "name", accountName, "value", accountBalance)

		b.PrometheusMetrics.CompanyMoney.WithLabelValues(accountName).Set(accountBalance)

		if accountName == "Airline account" {
			b.AccountBalance = accountBalance
		}
	}

	b.calcBudget()

	return nil
}

// calcBudget calculates the budget allocations for maintenance, marketing
// and fuel based on the total account balance and configured percentages.
func (b *Bot) calcBudget() {
	slog.Debug("calculate budgets")

	b.BudgetMoney.Maintenance = (b.AccountBalance * (b.Conf.BudgetPercent.Maintenance * 0.01))
	b.BudgetMoney.Marketing = (b.AccountBalance * (b.Conf.BudgetPercent.Marketing * 0.01))
	b.BudgetMoney.Fuel = (b.AccountBalance * (b.Conf.BudgetPercent.Fuel * 0.01))

	slog.Debug("calculated budget",
		"maintenancePercent", b.Conf.BudgetPercent.Maintenance,
		"maintenanceBudget", int(b.BudgetMoney.Maintenance),
		"marketingPercent", b.Conf.BudgetPercent.Marketing,
		"marketingBudget", int(b.BudgetMoney.Marketing),
		"fuelPercent", b.Conf.BudgetPercent.Fuel,
		"fuelBudget", int(b.BudgetMoney.Fuel))
}
