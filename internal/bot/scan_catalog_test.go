package bot

import (
	"testing"

	"github.com/ashokhin/am4bot/internal/config"
)

func TestParseCountryAirportName(t *testing.T) {
	testCases := map[string]struct {
		location        string
		wantCountry     string
		wantAirportName string
		wantOK          bool
	}{
		"simple":                  {"Afghanistan, Herat", "Afghanistan", "Herat", true},
		"multi-word airport name": {"United States, New York JFK", "United States", "New York JFK", true},
		"comma in airport name":   {"India, Mumbai, Chhatrapati Shivaji", "India", "Mumbai, Chhatrapati Shivaji", true},
		"no separator":            {"Nowhereland", "", "", false},
		"empty":                   {"", "", "", false},
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			country, airportName, ok := parseCountryAirportName(tt.location)
			if ok != tt.wantOK {
				t.Fatalf("parseCountryAirportName(%q) ok = %v, want %v", tt.location, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if country != tt.wantCountry || airportName != tt.wantAirportName {
				t.Errorf("parseCountryAirportName(%q) = (%q, %q), want (%q, %q)",
					tt.location, country, airportName, tt.wantCountry, tt.wantAirportName)
			}
		})
	}
}

func TestShouldScanCatalogAirport(t *testing.T) {
	testCases := map[string]struct {
		conf      config.Config
		airportID int
		want      bool
	}{
		"no restriction":       {config.Config{}, 42, true},
		"id list: included":    {config.Config{CatalogAirportIDs: []int{10, 42, 99}}, 42, true},
		"id list: excluded":    {config.Config{CatalogAirportIDs: []int{10, 99}}, 42, false},
		"range: inside":        {config.Config{CatalogAirportIDMin: 100, CatalogAirportIDMax: 2000}, 500, true},
		"range: below min":     {config.Config{CatalogAirportIDMin: 100, CatalogAirportIDMax: 2000}, 50, false},
		"range: above max":     {config.Config{CatalogAirportIDMin: 100, CatalogAirportIDMax: 2000}, 2001, false},
		"range: at boundaries": {config.Config{CatalogAirportIDMin: 100, CatalogAirportIDMax: 2000}, 100, true},
		"both: satisfies both": {config.Config{CatalogAirportIDs: []int{500}, CatalogAirportIDMin: 100, CatalogAirportIDMax: 2000}, 500, true},
		"both: fails range":    {config.Config{CatalogAirportIDs: []int{2500}, CatalogAirportIDMin: 100, CatalogAirportIDMax: 2000}, 2500, false},
		"both: fails id list":  {config.Config{CatalogAirportIDs: []int{999}, CatalogAirportIDMin: 100, CatalogAirportIDMax: 2000}, 500, false},
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			b := &Bot{Conf: &tt.conf}
			if got := b.shouldScanCatalogAirport(tt.airportID); got != tt.want {
				t.Errorf("shouldScanCatalogAirport(%d) = %v, want %v", tt.airportID, got, tt.want)
			}
		})
	}
}
