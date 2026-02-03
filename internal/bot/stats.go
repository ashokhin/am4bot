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

// companyStats retrieves and updates various statistics about the company.
func (b *Bot) companyStats(ctx context.Context) error {
	var (
		airlineReputation             float64
		cargoReputation               float64
		fleetSize                     float64
		acInflight                    float64
		acPendingMaintenance          float64
		acPendingDelivery             float64
		routes                        float64
		hubs                          float64
		hangarCapacity                float64
		sharePrice                    float64
		flightsOperated               float64
		passengersEconomyTransported  float64
		passengersBusinessTransported float64
		passengersFirstTransported    float64
		cargoTransportedLarge         float64
		cargoTransportedHeavy         float64
	)

	slog.Info("check company stats")
	slog.Debug("open pop-up window", "window", "overview")

	if err := chromedp.Run(ctx,
		chromedp.Click(model.BUTTON_FI_OVERVIEW, chromedp.ByQuery),
		chromedp.WaitReady(model.TEXT_OVERVIEW_AIRLINE_REPUTATION, chromedp.ByQuery),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_AIRLINE_REPUTATION, &airlineReputation),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_CARGO_REPUTATION, &cargoReputation),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_FLEET_SIZE, &fleetSize),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_AC_PENDING_DELIVERY, &acPendingDelivery),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_ROUTES, &routes),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_HUBS, &hubs),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_AC_PENDING_MAINTENANCE, &acPendingMaintenance),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_HANGAR_CAPACITY, &hangarCapacity),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_AC_INFLIGHT, &acInflight),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_SHARE_PRICE, &sharePrice),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_FLIGHTS_OPERATED, &flightsOperated),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_PASSENGERS_ECONOMY_TRANSPORTED, &passengersEconomyTransported),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_PASSENGERS_BUSINESS_TRANSPORTED, &passengersBusinessTransported),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_PASSENGERS_FIRST_TRANSPORTED, &passengersFirstTransported),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_CARGO_TRANSPORTED_LARGE, &cargoTransportedLarge),
		utils.GetFloatFromElement(model.TEXT_OVERVIEW_CARGO_TRANSPORTED_HEAVY, &cargoTransportedHeavy),
		chromedp.Click(model.BUTTON_COMMON_CLOSE_POPUP, chromedp.ByQuery),
		//utils.Screenshot(),
	); err != nil {
		slog.Debug("error in Bot.companyStats", "error", err)

		return err
	}

	acWithoutRoute := (fleetSize - (acPendingDelivery + routes))

	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.CompanyReputation.WithLabelValues("airline"), airlineReputation)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.CompanyReputation.WithLabelValues("cargo"), cargoReputation)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.CompanyFleetSize, fleetSize)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AircraftStatus.WithLabelValues("in_flight"), acInflight)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AircraftStatus.WithLabelValues("pending_delivery"), acPendingDelivery)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AircraftStatus.WithLabelValues("pending_maintenance"), acPendingMaintenance)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AircraftStatus.WithLabelValues("wo_route"), acWithoutRoute)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AircraftRoutesNumber, routes)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.HubsNumber, hubs)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.HangarCapacity, hangarCapacity)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.SharePrice, sharePrice)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.FlightsOperatedTotal, flightsOperated)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.PassengersTransportedTotal.WithLabelValues("economy"), passengersEconomyTransported)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.PassengersTransportedTotal.WithLabelValues("business"), passengersBusinessTransported)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.PassengersTransportedTotal.WithLabelValues("first"), passengersFirstTransported)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.CargoTransportedTotal.WithLabelValues("large"), cargoTransportedLarge)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.CargoTransportedTotal.WithLabelValues("heavy"), cargoTransportedHeavy)

	return nil
}

// allianceStats retrieves and updates various statistics about the alliance.
func (b *Bot) allianceStats(ctx context.Context) error {
	var (
		contributedTotal  float64
		contributedPerDay float64
		allianceFlights   float64
		seasonMoney       float64
	)

	slog.Info("check alliance stats")
	slog.Debug("open pop-up window", "window", "alliance_overview")

	if err := chromedp.Run(ctx,
		chromedp.Click(model.BUTTON_ALLIANCE_INFO, chromedp.ByQuery),
	); err != nil {
		slog.Debug("error in Bot.allianceStats", "error", err)

		return err
	}

	defer utils.DoClickElement(ctx, model.BUTTON_COMMON_CLOSE_POPUP)

	if !utils.IsElementVisible(ctx, model.TEXT_ALLIANCE_CONTRIBUTED_TOTAL) {
		slog.Warn("no alliance stats available")

		return nil
	}

	// collect stats for all alliance members
	if err := b.wholeAllianceStats(ctx); err != nil {
		slog.Debug("error in Bot.allianceStats > Bot.wholeAllianceStats", "error", err)
	}

	if err := chromedp.Run(ctx,
		utils.GetFloatFromElement(model.TEXT_ALLIANCE_CONTRIBUTED_TOTAL, &contributedTotal),
		utils.GetFloatFromElement(model.TEXT_ALLIANCE_CONTRIBUTED_PER_DAY, &contributedPerDay),
		utils.GetFloatFromElement(model.TEXT_ALLIANCE_FLIGHTS, &allianceFlights),
		utils.GetFloatFromElement(model.TEXT_ALLIANCE_SEASON_MONEY, &seasonMoney),
	); err != nil {
		slog.Debug("error in Bot.allianceStats", "error", err)

		return err
	}

	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AllianceContributedTotal, contributedTotal)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AllianceContributedPerDay, contributedPerDay)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AllianceFlightsTotal, allianceFlights)
	utils.SetPromGaugeNonNeg(b.PrometheusMetrics.AllianceSeasonMoney, seasonMoney)

	return nil
}

// wholeAllianceStats collects statistics about all alliance members
func (b *Bot) wholeAllianceStats(ctx context.Context) error {
	var err error

	slog.Debug("check whole alliance stats")

	allianceMembersElemList := make([]*cdp.Node, 0)

	slog.Debug("get list of all alliance members")

	// get list of all alliance members
	if err = chromedp.Run(ctx,
		chromedp.Nodes(model.LIST_ALLIANCE_MEMBERS, &allianceMembersElemList, chromedp.ByQueryAll),
	); err != nil {
		slog.Warn("error in Bot.wholeAllianceStats > get hubs list", "error", err)

		return err
	}

	allianceMembersMap := make(map[string]model.AllianceMember)

	for _, memberElem := range allianceMembersElemList {
		var (
			allianceMember model.AllianceMember
			uid            string
		)

		uid = memberElem.AttributeValue(model.TEXT_ALLIANCE_MEMBER_ID)
		uid = strings.ReplaceAll(uid, "al-list-", "")

		slog.Debug("member id", "id", uid)

		if err = chromedp.Run(ctx,
			chromedp.Text(model.TEXT_ALLIANCE_MEMBER_NAME, &allianceMember.Name, chromedp.ByQuery, chromedp.FromNode(memberElem)),
			utils.GetFloatFromChildElement(model.TEXT_ALLIANCE_MEMBER_CONTRIBUTED_TOTAL, &allianceMember.ContributedTotal, memberElem),
			utils.GetFloatFromChildElement(model.TEXT_ALLIANCE_MEMBER_CONTRIBUTED_PER_DAY, &allianceMember.ContributedPerDay, memberElem),
			utils.GetIntFromChildElement(model.TEXT_ALLIANCE_MEMBER_FLIGHTS, &allianceMember.FlightsTotal, memberElem),
			utils.GetFloatFromChildElement(model.TEXT_ALLIANCE_MEMBER_SEASON_MONEY, &allianceMember.ContributedSeason, memberElem),
		); err != nil {
			slog.Warn("error in Bot.wholeAllianceStats > get member data", "error", err)
		}

		// collect share price separately
		// bc. it could be the "N/A" string
		if err = chromedp.Run(ctx,
			utils.GetFloatFromChildElement(model.TEXT_ALLIANCE_MEMBER_SHARE_PRICE, &allianceMember.SharePrice, memberElem),
		); err != nil {
			slog.Warn("error in Bot.wholeAllianceStats > get member share price", "allianceMember.Name", allianceMember.Name, "error", err)

			allianceMember.SharePrice = -1.0

		}

		allianceMembersMap[uid] = allianceMember

	}

	// reset labels in vectors bc. list of alliance members are dynamic
	b.PrometheusMetrics.AllianceMemberSharePrice.Reset()
	b.PrometheusMetrics.AllianceMemberContributedTotal.Reset()
	b.PrometheusMetrics.AllianceMemberContributedPerDay.Reset()
	b.PrometheusMetrics.AllianceMemberContributedSeason.Reset()
	b.PrometheusMetrics.AllianceMemberFlightsTotal.Reset()

	for uid, member := range allianceMembersMap {
		slog.Debug("set alliance member metrics", "uid", uid, "member", member)

		b.PrometheusMetrics.AllianceMemberSharePrice.WithLabelValues(uid, member.Name).Set(member.SharePrice)
		b.PrometheusMetrics.AllianceMemberContributedTotal.WithLabelValues(uid, member.Name).Set(member.ContributedTotal)
		b.PrometheusMetrics.AllianceMemberContributedPerDay.WithLabelValues(uid, member.Name).Set(member.ContributedPerDay)
		b.PrometheusMetrics.AllianceMemberContributedSeason.WithLabelValues(uid, member.Name).Set(member.ContributedSeason)
		b.PrometheusMetrics.AllianceMemberFlightsTotal.WithLabelValues(uid, member.Name).Set(float64(member.FlightsTotal))
	}

	return nil
}
