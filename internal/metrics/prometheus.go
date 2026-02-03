package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "am4"
)

// Metrics holds all Prometheus metrics used in the application.
type Metrics struct {
	Up                              prometheus.Gauge
	StartTimeSeconds                prometheus.Gauge
	DurationSeconds                 prometheus.Gauge
	CompanyRank                     prometheus.Gauge
	CompanyTrainingPoints           prometheus.Gauge
	CompanyFleetSize                prometheus.Gauge
	AircraftRoutesNumber            prometheus.Gauge
	HubsNumber                      prometheus.Gauge
	HangarCapacity                  prometheus.Gauge
	SharePrice                      prometheus.Gauge
	FlightsOperatedTotal            prometheus.Gauge
	AllianceContributedTotal        prometheus.Gauge
	AllianceContributedPerDay       prometheus.Gauge
	AllianceFlightsTotal            prometheus.Gauge
	AllianceSeasonMoney             prometheus.Gauge
	PassengersTransportedTotal      *prometheus.GaugeVec
	CargoTransportedTotal           *prometheus.GaugeVec
	AircraftStatus                  *prometheus.GaugeVec
	CompanyReputation               *prometheus.GaugeVec
	MarketingCompanyDurationSeconds *prometheus.GaugeVec
	CompanyMoney                    *prometheus.GaugeVec
	HubStatsTotal                   *prometheus.GaugeVec
	StaffSalary                     *prometheus.GaugeVec
	FuelHolding                     *prometheus.GaugeVec
	FuelLimit                       *prometheus.GaugeVec
	FuelPrice                       *prometheus.GaugeVec
	AllianceMemberSharePrice        *prometheus.GaugeVec
	AllianceMemberContributedTotal  *prometheus.GaugeVec
	AllianceMemberContributedPerDay *prometheus.GaugeVec
	AllianceMemberContributedSeason *prometheus.GaugeVec
	AllianceMemberFlightsTotal      *prometheus.GaugeVec
}

// New initializes and returns a new Metrics instance with all Prometheus metrics defined.
func New() *Metrics {
	return &Metrics{
		Up: prometheus.NewGauge(
			prometheus.GaugeOpts{
				// Namespace: namespace,
				Name: "up",
				Help: "Was the last execution successful.",
			},
		),
		StartTimeSeconds: prometheus.NewGauge(
			prometheus.GaugeOpts{
				// Namespace: namespace,
				Name: "process_start_time_seconds",
				Help: "Start time of the process since unix epoch in seconds.",
			},
		),
		DurationSeconds: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "duration_seconds",
				Help:      "Duration of execution in seconds.",
			},
		),

		CompanyRank: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_rank",
				Help:      "Company rank value.",
			},
		),
		CompanyTrainingPoints: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_training_points",
				Help:      "Company training points value.",
			},
		),
		CompanyFleetSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "ac_fleet_size",
				Help:      "Company fleet size value.",
			},
		),
		AircraftRoutesNumber: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "ac_routes",
				Help:      "Company routes number value.",
			},
		),
		HubsNumber: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_hubs",
				Help:      "Company hubs number value.",
			},
		),
		HangarCapacity: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "ac_hangar_capacity",
				Help:      "Company hangar capacity value.",
			},
		),
		SharePrice: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_share_value",
				Help:      "Company share price value.",
			},
		),
		FlightsOperatedTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "stats_flights_operated_total",
				Help:      "Company flights operated value.",
			},
		),
		AllianceContributedTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_contributed_total",
				Help:      "Alliance contributed total value.",
			},
		),
		AllianceContributedPerDay: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_contributed_per_day",
				Help:      "Alliance contributed per day value.",
			},
		),
		AllianceFlightsTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_flights_total",
				Help:      "Alliance flights value.",
			},
		),
		AllianceSeasonMoney: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_season_money",
				Help:      "Alliance season money value.",
			},
		),
		PassengersTransportedTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "stats_passengers_transported_total",
				Help:      "Passengers transported by type.",
			},
			[]string{"type"},
		),
		CargoTransportedTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "stats_cargo_transported_total",
				Help:      "Cargo transported by type.",
			},
			[]string{"type"},
		),
		AircraftStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "ac_status",
				Help:      "Aircraft status by type.",
			},
			[]string{"type"},
		),
		CompanyReputation: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_reputation",
				Help:      "Company reputation by company type.",
			},
			[]string{"type"},
		),
		MarketingCompanyDurationSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "marketing_company_duration_seconds",
				Help:      "Marketing company duration in seconds by company type.",
			},
			[]string{"type"},
		),
		CompanyMoney: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_money",
				Help:      "Company money by account type.",
			},
			[]string{"type"},
		),
		HubStatsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "hub_stats_total",
				Help:      "Company hub info by hub name and stat type.",
			},
			[]string{"name", "type"},
		),
		StaffSalary: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_staff_salary",
				Help:      "Company staff salary by staff type.",
			},
			[]string{"type"},
		),
		FuelHolding: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_fuel_holding",
				Help:      "Fuel amount holding by fuel type.",
			},
			[]string{"type"},
		),
		FuelLimit: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "company_fuel_limit",
				Help:      "Fuel amount limit by fuel type.",
			},
			[]string{"type"},
		),
		FuelPrice: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "market_fuel_price",
				Help:      "Fuel amount price by fuel type.",
			},
			[]string{"type"},
		),
		AllianceMemberSharePrice: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_member_share_price",
				Help:      "Share price for alliance member.",
			},
			[]string{"uid", "name"},
		),
		AllianceMemberContributedTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_member_contributed_total",
				Help:      "Alliance member contributed total value.",
			},
			[]string{"uid", "name"},
		),
		AllianceMemberContributedPerDay: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_member_contributed_per_day",
				Help:      "Alliance member contributed total value.",
			},
			[]string{"uid", "name"},
		),
		AllianceMemberContributedSeason: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_member_season_money",
				Help:      "Alliance member season money value.",
			},
			[]string{"uid", "name"},
		),
		AllianceMemberFlightsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "alliance_member_flights_total",
				Help:      "Alliance member flights value.",
			},
			[]string{"uid", "name"},
		),
	}
}

// RegisterMetrics registers all Prometheus metrics with the provided registry.
func (m *Metrics) RegisterMetrics(registry *prometheus.Registry) {
	registry.MustRegister(
		m.Up,
		m.StartTimeSeconds,
		m.DurationSeconds,
		m.CompanyRank,
		m.CompanyTrainingPoints,
		m.CompanyFleetSize,
		m.AircraftRoutesNumber,
		m.HubsNumber,
		m.HangarCapacity,
		m.SharePrice,
		m.FlightsOperatedTotal,
		m.AllianceContributedTotal,
		m.AllianceContributedPerDay,
		m.AllianceFlightsTotal,
		m.AllianceSeasonMoney,
		m.PassengersTransportedTotal,
		m.CargoTransportedTotal,
		m.AircraftStatus,
		m.CompanyReputation,
		m.MarketingCompanyDurationSeconds,
		m.CompanyMoney,
		m.HubStatsTotal,
		m.StaffSalary,
		m.FuelHolding,
		m.FuelLimit,
		m.FuelPrice,
		m.AllianceMemberSharePrice,
		m.AllianceMemberContributedTotal,
		m.AllianceMemberContributedPerDay,
		m.AllianceMemberContributedSeason,
		m.AllianceMemberFlightsTotal,
	)
}
