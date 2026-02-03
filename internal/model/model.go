package model

import (
	"fmt"

	"github.com/chromedp/cdproto/cdp"
)

type MaintenanceType int

const (
	A_CHECK MaintenanceType = iota
	REPAIR
	MODIFY
)

// StaffEntry represents a staff category with associated UI elements for salary and morale management.
type StaffEntry struct {
	Name             string
	TextSalary       string
	TextMorale       string
	ButtonSalaryUp   string
	ButtonSalaryDown string
}

// StaffEntires is a list of all staff categories in the company.
var StaffEntires = []StaffEntry{
	{
		"pilots",
		TEXT_COMPANY_STAFF_PILOT_SALARY,
		TEXT_COMPANY_STAFF_PILOT_MORALE,
		BUTTON_COMPANY_STAFF_PILOT_SALARY_UP,
		BUTTON_COMPANY_STAFF_PILOT_SALARY_DOWN,
	},
	{
		"crew",
		TEXT_COMPANY_STAFF_CREW_SALARY,
		TEXT_COMPANY_STAFF_CREW_MORALE,
		BUTTON_COMPANY_STAFF_CREW_SALARY_UP,
		BUTTON_COMPANY_STAFF_CREW_SALARY_DOWN,
	},
	{
		"engineers",
		TEXT_COMPANY_STAFF_ENGINEER_SALARY,
		TEXT_COMPANY_STAFF_ENGINEER_MORALE,
		BUTTON_COMPANY_STAFF_ENGINEER_SALARY_UP,
		BUTTON_COMPANY_STAFF_ENGINEER_SALARY_DOWN,
	},
	{
		"technicians",
		TEXT_COMPANY_STAFF_TECHNICIAN_SALARY,
		TEXT_COMPANY_STAFF_TECHNICIAN_MORALE,
		BUTTON_COMPANY_STAFF_TECHNICIAN_SALARY_UP,
		BUTTON_COMPANY_STAFF_TECHNICIAN_SALARY_DOWN,
	},
}

// Fuel represents fuel information for an aircraft.
type Fuel struct {
	FuelType string
	Price    float64
	Holding  float64
	Capacity float64
	IsFull   bool
}

// Aircraft represents an aircraft in the fleet.
type Aircraft struct {
	RegNumber   string
	AcType      string
	WearPercent float64
	HoursACheck int
}

// MarketingCompany represents a marketing company with associated UI elements for activation and cost.
type MarketingCompany struct {
	Name               string
	CompanyRow         string
	CompanyOptionValue string
	CompanyCost        string
	CompanyButton      string
	DurationSeconds    int
	IsActive           bool
}

// Hub represents an airport hub with various statistics.
type Hub struct {
	Departures    float64
	Arrivals      float64
	PaxDeparted   float64
	PaxArrived    float64
	HasCatering   bool
	NeedsRepair   bool
	HubCdpNode    *cdp.Node
	LoungeCdpNode *cdp.Node
}

// String returns a string representation of the Hub struct.
func (h Hub) String() string {
	return fmt.Sprint("{Departures:", h.Departures, ", Arrivals:", h.Arrivals,
		", PaxDeparted:", h.PaxDeparted, ", PaxArrived:", h.PaxArrived,
		", HasCatering:", h.HasCatering, ", NeedsRepair:", h.NeedsRepair, "}")
}

// AllianceMember represents a member of an alliance with various statistics.
type AllianceMember struct {
	Name              string
	SharePrice        float64
	ContributedTotal  float64
	ContributedPerDay float64
	ContributedSeason float64
	FlightsTotal      int
}

// Route represents a flight route with various attributes.
type Route struct {
	Name        string
	Distance    int
	Runway      int
	DemandY     int
	DemandJ     int
	DemandF     int
	DemandLarge int
	DemandHeavy int
}
