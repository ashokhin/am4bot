package bot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/ashokhin/am4bot/internal/utils"

	"github.com/chromedp/chromedp"
)

// staffMorale checks and adjusts the morale of various staff members in the company.
func (b *Bot) staffMorale(ctx context.Context) error {
	var rank, trainingPoints float64

	slog.Info("check staff morale")
	slog.Debug("open pop-up window", "window", "company")

	if err := chromedp.Run(ctx,
		chromedp.Click(model.BUTTON_MAIN_COMPANY, chromedp.ByQuery),
		chromedp.WaitReady(model.TEXT_COMPANY_RANK, chromedp.ByQuery),
		utils.GetFloatFromElement(model.TEXT_COMPANY_RANK, &rank),
		chromedp.Click(model.BUTTON_COMMON_TAB2, chromedp.ByQuery),
		utils.GetFloatFromElement(model.TEXT_COMPANY_STAFF_TRAINING_POINTS, &trainingPoints),
	); err != nil {
		slog.Debug("error in Bot.staffMorale", "error", err)

		return err
	}

	defer utils.DoClickElement(ctx, model.BUTTON_COMMON_CLOSE_POPUP)

	slog.Debug("rank", "value", rank)

	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.CompanyRank, rank)

	slog.Debug("staff training points", "value", trainingPoints)

	b.PrometheusMetrics.CompanyTrainingPoints.Set(trainingPoints)

	for _, staffEntry := range model.StaffEntires {
		slog.Debug("check staff morale", "entry", staffEntry.Name)

		if err := b.checkStaffEntry(ctx, staffEntry); err != nil {
			return fmt.Errorf("staffMorale: checkStaffEntry %s: %w", staffEntry.Name, err)
		}
	}

	slog.Debug("close pop-up window", "window", "company")

	return nil
}

// checkStaffEntry checks and adjusts the morale for a specific staff entry.
func (b *Bot) checkStaffEntry(ctx context.Context, e model.StaffEntry) error {
	var moralePercent int

	if err := chromedp.Run(ctx,
		utils.GetIntFromElement(e.TextMorale, &moralePercent),
	); err != nil {
		return fmt.Errorf("checkStaffEntry %s: get morale: %w", e.Name, err)
	}
	startSalary := 0.0

	slog.Debug("check salary", "entry", e.Name)

	if err := chromedp.Run(ctx,
		utils.GetFloatFromElement(e.TextSalary, &startSalary),
	); err != nil {
		return err
	}

	// copy value
	newSalary := startSalary

	slog.Debug("start salary", "entry", e.Name, "value", startSalary)

	b.PrometheusMetrics.StaffSalary.WithLabelValues(e.Name).Set(startSalary)

	if moralePercent < 100 {
		// three clicks Up and three clicks Down before the first comparison
		if err := chromedp.Run(ctx,
			utils.ClickElement(e.ButtonSalaryUp),
			utils.ClickElement(e.ButtonSalaryUp),
			utils.ClickElement(e.ButtonSalaryUp),
			utils.ClickElement(e.ButtonSalaryDown),
			utils.ClickElement(e.ButtonSalaryDown),
			utils.ClickElement(e.ButtonSalaryDown),
		); err != nil {
			return err
		}
	}

	for moralePercent < 100 {
		slog.Debug("align morale", "entry", e.Name, "moralePercent", moralePercent,
			"newSalary", newSalary)

		if err := chromedp.Run(ctx,
			utils.ClickElement(e.ButtonSalaryUp),
			// check morale
			utils.GetIntFromElement(e.TextMorale, &moralePercent),
			// check salary
			utils.GetFloatFromElement(e.TextSalary, &newSalary),
		); err != nil {
			return fmt.Errorf("checkStaffEntry %s: align morale: %w", e.Name, err)
		}

		// Keep salary equal or lower than startSalary
		maxAttempts := 10
		for newSalary > startSalary {
			maxAttempts--
			slog.Debug("align salary. newSalary > startSalary", "entry", e.Name,
				"newSalary", newSalary, "startSalary", startSalary, "attemptsLeft", maxAttempts)

			if err := chromedp.Run(ctx,
				utils.ClickElement(e.ButtonSalaryDown),
				utils.ClickElement(e.ButtonSalaryUp),
				// check morale
				utils.GetIntFromElement(e.TextMorale, &moralePercent),
				// check salary
				utils.GetFloatFromElement(e.TextSalary, &newSalary),
			); err != nil {
				return fmt.Errorf("checkStaffEntry %s: align salary: %w", e.Name, err)
			}

			slog.Warn("aligned salary", "entry", e.Name, "morale", moralePercent, "salary", newSalary)

			if maxAttempts == 0 {
				slog.Debug("failed to align salary. Return to morale")

				break
			}
		}
	}

	slog.Debug("morale aligned", "entry", e.Name, "morale", moralePercent, "salary", newSalary)

	b.PrometheusMetrics.StaffSalary.WithLabelValues(e.Name).Set(newSalary)

	return nil
}
