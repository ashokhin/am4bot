package io

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
// don't already exist, and adds any columns introduced after a database's
// original creation (SQLite has no "ADD COLUMN IF NOT EXISTS", so those are
// applied via best-effort ALTER TABLEs that ignore a "duplicate column" error).
func (w *CatalogWriter) createSchema() error {
	_, err := w.db.Exec(`
		CREATE TABLE IF NOT EXISTS airports (
			id         INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			country    TEXT NOT NULL,
			iata       TEXT,
			icao       TEXT,
			runway_ft  INTEGER,
			scanned_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_airports_country ON airports(country);

		CREATE TABLE IF NOT EXISTS routes (
			from_airport_id INTEGER NOT NULL REFERENCES airports(id),
			to_airport_id   INTEGER NOT NULL REFERENCES airports(id),
			distance_km     INTEGER NOT NULL,
			demand_y        INTEGER NOT NULL,
			demand_j        INTEGER NOT NULL,
			demand_f        INTEGER NOT NULL,
			demand_large    INTEGER NOT NULL DEFAULT 0,
			demand_heavy    INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (from_airport_id, to_airport_id)
		);

		CREATE TABLE IF NOT EXISTS aircraft (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			name              TEXT NOT NULL UNIQUE,
			airframe          TEXT,
			engine            TEXT,
			ac_type           TEXT NOT NULL,
			cruise_speed_kmh  INTEGER NOT NULL,
			max_range_km      INTEGER NOT NULL,
			min_runway_ft     INTEGER NOT NULL,
			capacity          INTEGER NOT NULL
		);
	`)
	if err != nil {
		return err
	}

	for _, ddl := range []string{
		`ALTER TABLE airports ADD COLUMN scan_distance_km INTEGER`,
		`ALTER TABLE airports ADD COLUMN scan_runway_ft INTEGER`,
	} {
		if _, err := w.db.Exec(ddl); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("applying schema migration %q: %w", ddl, err)
		}
	}

	// idx_routes_from used to be created above, but it's redundant: routes'
	// PRIMARY KEY (from_airport_id, to_airport_id) already gives SQLite an
	// index usable for "WHERE from_airport_id = ?" via its leading column
	// (confirmed via EXPLAIN QUERY PLAN — same "SEARCH ... (from_airport_id=?)"
	// either way). On the full imported catalog it cost ~200MB (~25% of the
	// file) for zero query benefit. Drop it if an older database still has it.
	if _, err := w.db.Exec(`DROP INDEX IF EXISTS idx_routes_from`); err != nil {
		return fmt.Errorf("dropping redundant idx_routes_from: %w", err)
	}

	return nil
}

// UpsertAirport inserts an airport or updates its name/country if it already
// exists. Codes and the scanned_at marker are left untouched. Use this only
// for name/country read from an authoritative source (the country dropdown
// selection, in the custom-departure form) — it unconditionally overwrites,
// so a bad read here silently clobbers good data. For name/country observed
// opportunistically (e.g. parsed from a route result row), use
// UpsertAirportIfUnset instead.
func (w *CatalogWriter) UpsertAirport(id int, name, country string) error {
	_, err := w.db.Exec(`
		INSERT INTO airports (id, name, country) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, country = excluded.country
	`, id, name, country)

	return err
}

// GetAirportNameCountry returns the given airport's currently recorded
// name/country. A missing row is treated as empty strings, not an error —
// useful for callers that just want to compare against a freshly-read value
// before overwriting (see UpsertAirport's callers that log on mismatch).
func (w *CatalogWriter) GetAirportNameCountry(id int) (name, country string, err error) {
	if err := w.db.QueryRow(`SELECT name, country FROM airports WHERE id = ?`, id).Scan(&name, &country); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", nil
		}

		return "", "", err
	}

	return name, country, nil
}

// UpsertAirportIfUnset inserts an airport, or — if the row already exists —
// fills in name/country only where they're still empty (e.g. an
// empty-name/country stub left by SetAirportIATA/ICAO/Runway). An
// already-populated name/country is left untouched.
//
// This is the safe choice for name/country parsed from a route result row's
// destination text: unlike the country dropdown (see UpsertAirport), that
// text is read under time pressure from a specific DOM node inside a search
// result, and a wrong selector match or stale read there must not be allowed
// to overwrite a name/country already recorded from the authoritative
// source.
func (w *CatalogWriter) UpsertAirportIfUnset(id int, name, country string) error {
	_, err := w.db.Exec(`
		INSERT INTO airports (id, name, country) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name    = CASE WHEN airports.name = ''    THEN excluded.name    ELSE airports.name    END,
			country = CASE WHEN airports.country = '' THEN excluded.country ELSE airports.country END
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

// MarkAirportScanned records that the given airport's full route sweep has
// completed, and clears any in-progress sweep position (see SetScanProgress) —
// it's no longer needed once the airport is done.
func (w *CatalogWriter) MarkAirportScanned(id int) error {
	_, err := w.db.Exec(`
		UPDATE airports
		SET scanned_at = unixepoch('now'), scan_distance_km = NULL, scan_runway_ft = NULL
		WHERE id = ?
	`, id)

	return err
}

// ScanProgress is an airport's in-progress sweep position — the distance
// window currently being processed and, if a runway sub-sweep is underway for
// that window, the runway threshold within it. A zero value (both fields
// invalid) means the airport's sweep hasn't started yet, or was never
// interrupted mid-window.
type ScanProgress struct {
	DistanceKm sql.NullInt64 `db:"scan_distance_km"`
	RunwayFt   sql.NullInt64 `db:"scan_runway_ft"`
}

// GetScanProgress returns the given airport's saved in-progress sweep
// position, so an interrupted multi-hour sweep can resume from where it left
// off instead of restarting the airport from scratch. A missing airports row
// is treated as "no progress" rather than an error.
func (w *CatalogWriter) GetScanProgress(id int) (ScanProgress, error) {
	var p ScanProgress

	if err := w.db.Get(&p, `SELECT scan_distance_km, scan_runway_ft FROM airports WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ScanProgress{}, nil
		}

		return ScanProgress{}, err
	}

	return p, nil
}

// SetScanProgress records the distance window (and, if mid-runway-sub-sweep,
// the runway threshold) currently being processed for the given airport, so a
// resumed run can pick up from here instead of restarting the airport's
// (potentially hours-long) sweep from its configured maximum distance.
// runwayFt may be sql.NullInt64{} to record "not currently in a runway
// sub-sweep" (i.e. between distance windows).
func (w *CatalogWriter) SetScanProgress(id, distanceKm int, runwayFt sql.NullInt64) error {
	_, err := w.db.Exec(`
		INSERT INTO airports (id, name, country, scan_distance_km, scan_runway_ft) VALUES (?, '', '', ?, ?)
		ON CONFLICT(id) DO UPDATE SET scan_distance_km = excluded.scan_distance_km, scan_runway_ft = excluded.scan_runway_ft
	`, id, distanceKm, runwayFt)

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

// airportHasCode reports whether the airport already has the given code
// recorded. A missing airports row (not yet observed anywhere) is treated as
// "no code" rather than an error.
func (w *CatalogWriter) airportHasCode(id int, column string) (bool, error) {
	var code sql.NullString

	query := fmt.Sprintf(`SELECT %s FROM airports WHERE id = ?`, column) //nolint:gosec // column is one of two hardcoded literals, never user input
	if err := w.db.Get(&code, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	return code.Valid, nil
}

// SetAirportIATA records the given airport's IATA code. Callers should
// UpsertAirport (with the real name/country) first — see SetAirportRunway —
// this only inserts an empty-name/country stub as a last-resort safety net
// so the NOT NULL columns are never violated if that ordering isn't followed.
func (w *CatalogWriter) SetAirportIATA(id int, code string) error {
	_, err := w.db.Exec(`
		INSERT INTO airports (id, name, country, iata) VALUES (?, '', '', ?)
		ON CONFLICT(id) DO UPDATE SET iata = excluded.iata
	`, id, code)

	return err
}

// SetAirportICAO records the given airport's ICAO code. See SetAirportIATA.
func (w *CatalogWriter) SetAirportICAO(id int, code string) error {
	_, err := w.db.Exec(`
		INSERT INTO airports (id, name, country, icao) VALUES (?, '', '', ?)
		ON CONFLICT(id) DO UPDATE SET icao = excluded.icao
	`, id, code)

	return err
}

// SetAirportRunway records an airport's own runway length, learned from a
// route search result row where this airport was the destination (runway
// length is a property of the airport, not of any particular route). The
// first observed value is kept — every route into this airport reports the
// same physical runway, so this is a no-op once set.
//
// Callers should UpsertAirport (with the real name/country, parsed from the
// same result row) first, so the row already exists with real data by the
// time this runs — this only inserts an empty-name/country stub as a
// last-resort safety net so the NOT NULL columns are never violated, and so
// a plain UPDATE can never silently affect zero rows and lose the value.
func (w *CatalogWriter) SetAirportRunway(id, runwayFt int) error {
	_, err := w.db.Exec(`
		INSERT INTO airports (id, name, country, runway_ft) VALUES (?, '', '', ?)
		ON CONFLICT(id) DO UPDATE SET runway_ft = COALESCE(airports.runway_ft, excluded.runway_ft)
	`, id, runwayFt)

	return err
}

// CatalogRoute represents one direction of a scraped route, keyed by the
// game's internal numeric airport IDs. Runway length is not included here —
// it's a property of the destination airport (see SetAirportRunway), not of
// the route.
type CatalogRoute struct {
	FromAirportID int
	ToAirportID   int
	DistanceKm    int
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
			from_airport_id, to_airport_id, distance_km,
			demand_y, demand_j, demand_f, demand_large, demand_heavy
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(from_airport_id, to_airport_id) DO UPDATE SET
			distance_km  = excluded.distance_km,
			demand_y     = excluded.demand_y,
			demand_j     = excluded.demand_j,
			demand_f     = excluded.demand_f,
			demand_large = excluded.demand_large,
			demand_heavy = excluded.demand_heavy
	`, r.FromAirportID, r.ToAirportID, r.DistanceKm,
		r.DemandY, r.DemandJ, r.DemandF, r.DemandLarge, r.DemandHeavy)

	return err
}

// Close closes the underlying database connection.
func (w *CatalogWriter) Close() error {
	return w.db.Close()
}
