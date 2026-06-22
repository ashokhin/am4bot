package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// marketingCompanies checks and activates marketing companies based on budget and status.
func (b *Bot) marketingCompanies(ctx context.Context) error {
	slog.Info("check marketing companies")

	marketingCompaniesMap := map[string]model.MarketingCompany{
		"AirlineReputation": {
			Name:               "Airline reputation",
			CompanyRow:         model.ELEM_FINANCE_MARKETING_INC_AIRLINE_REP,
			CompanyOptionValue: model.OPTION_FINANCE_MARKETING_INC_AIRLINE_REP_24H_VALUE,
			CompanyCost:        model.TEXT_FINANCE_MARKETING_INC_AIRLINE_REP_COST,
			CompanyButton:      model.BUTTON_FINANCE_MARKETING_INC_AIRLINE_REP_BUY,
		},
		"CargoReputation": {
			Name:               "Cargo reputation",
			CompanyRow:         model.ELEM_FINANCE_MARKETING_INC_CARGO_REP,
			CompanyOptionValue: model.OPTION_FINANCE_MARKETING_INC_CARGO_REP_24H_VALUE,
			CompanyCost:        model.TEXT_FINANCE_MARKETING_INC_CARGO_REP_COST,
			CompanyButton:      model.BUTTON_FINANCE_MARKETING_INC_CARGO_REP_BUY,
		},
		"EcoFriendly": {
			Name:               "Eco friendly",
			CompanyRow:         model.ELEM_FINANCE_MARKETING_ECO_FRIENDLY,
			CompanyOptionValue: "",
			CompanyCost:        model.TEXT_FINANCE_MARKETING_ECO_FRIENDLY_COST,
			CompanyButton:      model.BUTTON_FINANCE_MARKETING_ECO_FRIENDLY_BUY,
		},
	}

	// open finance pop-up
	utils.DoClickElement(ctx, model.BUTTON_MAIN_FINANCE)
	defer utils.DoClickElement(ctx, model.BUTTON_COMMON_CLOSE_POPUP)
	// open the "+ New campaign" section
	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_COMMON_TAB2),
		utils.ClickElement(model.BUTTON_FINANCE_MARKETING_NEW_COMPANY),
	); err != nil {
		return fmt.Errorf("marketingCompanies: open marketing companies window: %w", err)
	}

	// check marketing companies status
	for markCompName, markComp := range marketingCompaniesMap {
		if err := b.checkMarketingCompanyStatus(ctx, &markComp); err != nil {
			return fmt.Errorf("marketingCompanies: checkMarketingCompanyStatus %s: %w", markComp.Name, err)
		}

		slog.Debug("marketing company status", "company", markComp.Name, "isActive", markComp.IsActive)
		// update map entry
		marketingCompaniesMap[markCompName] = markComp
	}

	// activate marketing companies if not active
	// and collect marketing companies duration if active
	for markCompName, markComp := range marketingCompaniesMap {
		if !markComp.IsActive {
			if err := b.activateMarketingCompany(ctx, &markComp); err != nil {
				slog.Warn("error in Bot.marketingCompanies > Bot.activateMarketingCompany", "company", markComp.Name, "error", err)
			} else {
				// update map entry
				marketingCompaniesMap[markCompName] = markComp
			}

		}

		// collect duration if active
		if markComp.IsActive {
			if err := b.collectMarketingCompanyDuration(ctx, &markComp); err != nil {
				slog.Warn("error in Bot.marketingCompanies > Bot.collectMarketingCompanyDuration", "company", markComp.Name, "error", err)

				continue
			}
			// update map entry
			marketingCompaniesMap[markCompName] = markComp

			// update Prometheus metrics
			b.PrometheusMetrics.MarketingCompanyDurationSeconds.WithLabelValues(markComp.Name).Set(float64(markComp.DurationSeconds))
		}
	}

	return nil
}

// checkMarketingCompanyStatus checks if a marketing company is currently active.
func (b *Bot) checkMarketingCompanyStatus(ctx context.Context, mc *model.MarketingCompany) error {
	var marketingCompanyElemAttributes map[string]string

	slog.Debug("Check marketing company by the 'class' attribute", "company", mc.Name)

	// search marketingCompany element attributes
	if err := chromedp.Run(ctx,
		chromedp.Attributes(mc.CompanyRow, &marketingCompanyElemAttributes, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("checkMarketingCompanyStatus %s: %w", mc.Name, err)
	}

	slog.Debug("attributes found", "company", mc.Name, "attributes", marketingCompanyElemAttributes)

	// check if marketing company element has "not-active" class (means company is active)
	if marketingCompanyElemAttributes["class"] == "not-active" {
		slog.Debug("marketing company is active", "company", mc.Name)

		mc.IsActive = true
	} else {
		slog.Debug("marketing company is not active", "company", mc.Name)

		mc.IsActive = false
	}

	return nil
}

// activateMarketingCompany activates a specific marketing company if it is affordable and not already active.
func (b *Bot) activateMarketingCompany(ctx context.Context, mc *model.MarketingCompany) error {
	slog.Debug("activate marketing company", "company", mc.Name)

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_COMMON_TAB2),
		utils.ClickElement(model.BUTTON_FINANCE_MARKETING_NEW_COMPANY),
		utils.ClickElement(mc.CompanyRow),
	); err != nil {
		return fmt.Errorf("activateMarketingCompany %s: click company row: %w", mc.Name, err)
	}

	var marketingCompanyCost float64
	// get marketing company cost
	switch mc.Name {
	// in case of "Eco friendly" marketing company we skip "select option" actions
	case "Eco friendly":
		if err := chromedp.Run(ctx,
			utils.GetFloatFromElement(mc.CompanyCost, &marketingCompanyCost),
		); err != nil {
			return fmt.Errorf("activateMarketingCompany %s: get company cost: %w", mc.Name, err)
		}
	default:
		if err := chromedp.Run(ctx,
			chromedp.SetValue(model.SELECT_FINANCE_MARKETING_COMPANY_DURATION, mc.CompanyOptionValue, chromedp.ByQuery),
			utils.GetFloatFromElement(mc.CompanyCost, &marketingCompanyCost),
		); err != nil {
			return fmt.Errorf("activateMarketingCompany %s: get company cost: %w", mc.Name, err)
		}
	}

	slog.Debug("company cost", "company", mc.Name, "cost", int(marketingCompanyCost))

	if marketingCompanyCost > b.BudgetMoney.Marketing {
		slog.Warn("marketing company is too expensive", "company", mc.Name,
			"cost", int(marketingCompanyCost), "budget", int(b.BudgetMoney.Marketing))

		return nil
	}

	// buy marketing company
	if err := chromedp.Run(ctx,
		utils.ClickElement(mc.CompanyButton),
	); err != nil {
		return fmt.Errorf("activateMarketingCompany %s: buy company: %w", mc.Name, err)
	}

	// update budgets and account balance
	b.deductBudget(marketingCompanyCost, &b.BudgetMoney.Marketing)
	mc.IsActive = true

	slog.Info("marketing company activated", "company", mc.Name,
		"cost", int(marketingCompanyCost),
		"new marketing budget", int(b.BudgetMoney.Marketing),
		"new account balance", int(b.AccountBalance),
	)

	return nil
}

// collectMarketingCompanyDuration searches and collects the remaining duration of an active marketing company.
// The search is performed because of the list of marketing is dynamic.
func (b *Bot) collectMarketingCompanyDuration(ctx context.Context, mc *model.MarketingCompany) error {
	slog.Debug("collect marketing company duration", "company", mc.Name)

	var durationStr string

	// find marketing companies array
	var marketingCompaniesList []*cdp.Node

	if err := chromedp.Run(ctx,
		utils.ClickElement(model.BUTTON_COMMON_TAB2),
		chromedp.Nodes(model.LIST_FINANCE_MARKETING_COMPANIES, &marketingCompaniesList, chromedp.ByQueryAll),
	); err != nil {
		return fmt.Errorf("collectMarketingCompanyDuration: get companies list: %w", err)
	}
	// get marketing company duration string
	for _, companyElem := range marketingCompaniesList {
		var companyName string

		if err := chromedp.Run(ctx,
			chromedp.Text(model.TEXT_MARKETING_COMPANY_NAME, &companyName, chromedp.ByQuery, chromedp.FromNode(companyElem)),
		); err != nil {
			return fmt.Errorf("collectMarketingCompanyDuration: get company name: %w", err)
		}

		companyName = strings.TrimSpace(companyName)

		slog.Debug("parsed marketing company name from list", "company", companyName)

		if companyName == mc.Name {
			slog.Debug("found marketing company", "company", mc.Name)

			// add several attempts to get duration string due to possible UI updates
			for i := 0; i < 5; i++ {
				if err := chromedp.Run(ctx,
					chromedp.Text(model.TEXT_MARKETING_COMPANY_DURATION, &durationStr, chromedp.ByQuery, chromedp.FromNode(companyElem)),
				); err != nil {
					return fmt.Errorf("collectMarketingCompanyDuration %s: get duration: %w", mc.Name, err)
				}

				if durationStr != "" {
					break
				}

				slog.Debug("duration string not found", "company", mc.Name, "attempt", i+1)

				time.Sleep(100 * time.Millisecond)
			}

			slog.Debug("found marketing company duration", "company", mc.Name, "durationStr", durationStr)

			break
		}
	}

	// parse duration string to seconds
	durationSeconds, err := utils.ParseDurationStringToSeconds(durationStr)
	if err != nil {
		return fmt.Errorf("collectMarketingCompanyDuration %s: parse duration: %w", mc.Name, err)
	}

	slog.Debug("marketing company duration collected", "company", mc.Name, "durationStr", durationStr, "durationSeconds", durationSeconds)

	mc.DurationSeconds = durationSeconds

	return nil
}
