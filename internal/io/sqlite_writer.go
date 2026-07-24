package io

import (
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// CatalogWriter persists the full-game route catalog (airports and routes,
// keyed by the game's internal numeric airport IDs) to a SQLite file.
type CatalogWriter struct {
	db *sqlx.DB
}

// NewCatalogWriter opens (creating if necessary) the catalog SQLite database at
// dbPath and ensures its schema exists.
func NewCatalogWriter(dbPath string) (*CatalogWriter, error) {
	db, err := sqlx.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	w := &CatalogWriter{db: db}

	if err := w.createSchema(); err != nil {
		return nil, err
	}

	return w, nil
}

// createSchema creates the airports/routes tables and their indexes if they
// don't already exist.
func (w *CatalogWriter) createSchema() error {
	_, err := w.db.Exec(`
		CREATE TABLE IF NOT EXISTS airports (
			id         INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			country    TEXT NOT NULL,
			iata       TEXT,
			icao       TEXT,
			scanned_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_airports_country ON airports(country);

		CREATE TABLE IF NOT EXISTS routes (
			from_airport_id INTEGER NOT NULL REFERENCES airports(id),
			to_airport_id   INTEGER NOT NULL REFERENCES airports(id),
			distance_km     INTEGER NOT NULL,
			runway_ft       INTEGER NOT NULL,
			demand_y        INTEGER NOT NULL,
			demand_j        INTEGER NOT NULL,
			demand_f        INTEGER NOT NULL,
			demand_large    INTEGER NOT NULL DEFAULT 0,
			demand_heavy    INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (from_airport_id, to_airport_id)
		);
		CREATE INDEX IF NOT EXISTS idx_routes_from ON routes(from_airport_id);
	`)

	return err
}

// UpsertAirport inserts an airport or updates its name/country if it already
// exists. Codes and the scanned_at marker are left untouched.
func (w *CatalogWriter) UpsertAirport(id int, name, country string) error {
	_, err := w.db.Exec(`
		INSERT INTO airports (id, name, country) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, country = excluded.country
	`, id, name, country)

	return err
}

// AirportScanned reports whether the given airport's full route sweep has
// already completed, so the caller can skip it on a resumed run.
func (w *CatalogWriter) AirportScanned(id int) (bool, error) {
	var scannedAt sql.NullInt64
	if err := w.db.Get(&scannedAt, `SELECT scanned_at FROM airports WHERE id = ?`, id); err != nil {
		return false, err
	}

	return scannedAt.Valid, nil
}

// MarkAirportScanned records that the given airport's full route sweep has completed.
func (w *CatalogWriter) MarkAirportScanned(id int) error {
	_, err := w.db.Exec(`UPDATE airports SET scanned_at = unixepoch('now') WHERE id = ?`, id)

	return err
}

// AirportHasIATA reports whether the airport already has an IATA code recorded.
func (w *CatalogWriter) AirportHasIATA(id int) (bool, error) {
	return w.airportHasCode(id, "iata")
}

// AirportHasICAO reports whether the airport already has an ICAO code recorded.
func (w *CatalogWriter) AirportHasICAO(id int) (bool, error) {
	return w.airportHasCode(id, "icao")
}

func (w *CatalogWriter) airportHasCode(id int, column string) (bool, error) {
	var code sql.NullString

	query := fmt.Sprintf(`SELECT %s FROM airports WHERE id = ?`, column) //nolint:gosec // column is one of two hardcoded literals, never user input
	if err := w.db.Get(&code, query, id); err != nil {
		return false, err
	}

	return code.Valid, nil
}

// SetAirportIATA records the given airport's IATA code.
func (w *CatalogWriter) SetAirportIATA(id int, code string) error {
	_, err := w.db.Exec(`UPDATE airports SET iata = ? WHERE id = ?`, code, id)

	return err
}

// SetAirportICAO records the given airport's ICAO code.
func (w *CatalogWriter) SetAirportICAO(id int, code string) error {
	_, err := w.db.Exec(`UPDATE airports SET icao = ? WHERE id = ?`, code, id)

	return err
}

// CatalogRoute represents one direction of a scraped route, keyed by the
// game's internal numeric airport IDs.
type CatalogRoute struct {
	FromAirportID int
	ToAirportID   int
	DistanceKm    int
	RunwayFt      int
	DemandY       int
	DemandJ       int
	DemandF       int
	DemandLarge   int
	DemandHeavy   int
}

// WriteRoute upserts a route into the catalog, keyed by (from, to) airport IDs.
func (w *CatalogWriter) WriteRoute(r CatalogRoute) error {
	_, err := w.db.Exec(`
		INSERT INTO routes (
			from_airport_id, to_airport_id, distance_km, runway_ft,
			demand_y, demand_j, demand_f, demand_large, demand_heavy
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(from_airport_id, to_airport_id) DO UPDATE SET
			distance_km  = excluded.distance_km,
			runway_ft    = excluded.runway_ft,
			demand_y     = excluded.demand_y,
			demand_j     = excluded.demand_j,
			demand_f     = excluded.demand_f,
			demand_large = excluded.demand_large,
			demand_heavy = excluded.demand_heavy
	`, r.FromAirportID, r.ToAirportID, r.DistanceKm, r.RunwayFt,
		r.DemandY, r.DemandJ, r.DemandF, r.DemandLarge, r.DemandHeavy)

	return err
}

// Close closes the underlying database connection.
func (w *CatalogWriter) Close() error {
	return w.db.Close()
}
