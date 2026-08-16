// Command importparquet builds a full catalog SQLite database directly from
// the "am4" project's (github.com/abc8747/am4) pre-scraped static game-data
// release assets — airports.parquet, routes.parquet and aircrafts.parquet —
// instead of running full_catalog_scanner against the live game.
//
// routes.parquet is not keyed by airport ID: it's a flattened strictly-upper-
// triangular matrix over every airport pair (see abc8747/am4's
// am4/src/route/db.rs, StrictlyUpperTriangularMatrix), one row per unordered
// pair (origin_idx, dest_idx) with origin_idx < dest_idx, in the exact order
// produced by iterating origin_idx from 0..N-1 and, for each, dest_idx from
// origin_idx+1..N-1. airports.parquet's row order (0-indexed) is that same
// idx. So this tool reads routes.parquet sequentially and pairs each row with
// the matching (i, j) from that same double loop — no join key needed, only
// matching iteration order.
//
// aircrafts.parquet has one row per (airframe, engine) combination — the same
// airframe (e.g. "B747-400") appears once per selectable engine, each with its
// own cruise speed. Every row is imported as a distinct catalog aircraft named
// "<airframe> (<engine>)", so every real in-game speed variant is available —
// this is what lets the calculator drop its "add a custom aircraft" workaround
// for engine choices not in a short hardcoded list.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"time"

	amio "github.com/ashokhin/am4bot/internal/io"
	"github.com/jmoiron/sqlx"
	"github.com/parquet-go/parquet-go"
	_ "modernc.org/sqlite"
)

// airportRow mirrors the columns of airports.parquet we need. Row order in
// the file is the "idx" used to address routes.parquet's implicit matrix.
type airportRow struct {
	ID      uint16 `parquet:"id"`
	Name    string `parquet:"name"`
	Country string `parquet:"country"`
	IATA    string `parquet:"iata"`
	ICAO    string `parquet:"icao"`
	RWY     uint16 `parquet:"rwy"`
}

// routeRow mirrors the columns of routes.parquet: pax demand and distance for
// one unordered airport pair, keyed implicitly by row order (see package doc).
type routeRow struct {
	YD uint16  `parquet:"yd"`
	JD uint16  `parquet:"jd"`
	FD uint16  `parquet:"fd"`
	D  float64 `parquet:"d"`
}

// aircraftRow mirrors the columns of aircrafts.parquet we need. One row per
// (airframe, engine) combination — see package doc.
type aircraftRow struct {
	Name     string  `parquet:"name"`
	Type     uint8   `parquet:"type"`
	EName    string  `parquet:"ename"`
	Speed    float32 `parquet:"speed"`
	Capacity uint32  `parquet:"capacity"`
	RWY      uint16  `parquet:"rwy"`
	Range    uint16  `parquet:"range"`
}

// aircraftTypeNames maps aircrafts.parquet's numeric "type" column to the
// calculator's ac_type strings — confirmed against known aircraft (A320-VIP
// etc. are type 2, cargo-only airframes like the A400M are type 1).
var aircraftTypeNames = map[uint8]string{
	0: "PAX",
	1: "CARGO",
	2: "VIP",
}

// batchSize bounds how many parquet rows are read (and how many SQL
// statements are issued) per transaction commit.
const batchSize = 50_000

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintf(os.Stderr, "usage: %s <airports.parquet> <routes.parquet> <aircrafts.parquet> <output.db>\n", os.Args[0])
		os.Exit(1)
	}

	airportsPath, routesPath, aircraftsPath, outputPath := os.Args[1], os.Args[2], os.Args[3], os.Args[4]

	if err := run(airportsPath, routesPath, aircraftsPath, outputPath); err != nil {
		slog.Error("import failed", "error", err)
		os.Exit(1)
	}
}

func run(airportsPath, routesPath, aircraftsPath, outputPath string) error {
	airports, err := readAirports(airportsPath)
	if err != nil {
		return fmt.Errorf("reading airports parquet: %w", err)
	}

	slog.Info("read airports", "count", len(airports))

	aircrafts, err := readAircraft(aircraftsPath)
	if err != nil {
		return fmt.Errorf("reading aircrafts parquet: %w", err)
	}

	slog.Info("read aircraft", "count", len(aircrafts))

	// NewCatalogWriter ensures the output file has the full current schema
	// before we bulk-load into it via a plain sqlx connection — mirrors
	// cmd/mergecatalog's approach, since CatalogWriter's row-at-a-time upsert
	// API isn't built for tens of millions of inserts.
	writer, err := amio.NewCatalogWriter(outputPath)
	if err != nil {
		return fmt.Errorf("creating output catalog schema: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("closing output catalog after schema setup: %w", err)
	}

	db, err := sqlx.Open("sqlite", outputPath+"?_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("opening output catalog: %w", err)
	}
	defer db.Close()

	if err := importAirports(db, airports); err != nil {
		return fmt.Errorf("importing airports: %w", err)
	}

	if err := importRoutes(db, routesPath, airports); err != nil {
		return fmt.Errorf("importing routes: %w", err)
	}

	if err := importAircraft(db, aircrafts); err != nil {
		return fmt.Errorf("importing aircraft: %w", err)
	}

	var airportCount, routeCount, aircraftCount int
	_ = db.Get(&airportCount, `SELECT COUNT(*) FROM airports`)
	_ = db.Get(&routeCount, `SELECT COUNT(*) FROM routes`)
	_ = db.Get(&aircraftCount, `SELECT COUNT(*) FROM aircraft`)
	slog.Info("import complete", "output", outputPath, "airports", airportCount, "routes", routeCount, "aircraft", aircraftCount)

	return nil
}

func readAirports(path string) ([]airportRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := parquet.NewGenericReader[airportRow](f)
	defer reader.Close()

	airports := make([]airportRow, 0, reader.NumRows())
	buf := make([]airportRow, 1024)

	for {
		n, err := reader.Read(buf)
		airports = append(airports, buf[:n]...)

		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	return airports, nil
}

// importAirports writes every airport as an authoritative, already-scanned
// row: this is a complete, trusted snapshot of the game's static data, not a
// partial/opportunistic sighting, so scanned_at is set immediately (nothing
// about these airports needs the live scanner to revisit them).
func importAirports(db *sqlx.DB, airports []airportRow) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO airports (id, name, country, iata, icao, runway_ft, scanned_at)
		VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name       = excluded.name,
			country    = excluded.country,
			iata       = excluded.iata,
			icao       = excluded.icao,
			runway_ft  = excluded.runway_ft,
			scanned_at = excluded.scanned_at
	`)
	if err != nil {
		_ = tx.Rollback()

		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()

	for _, a := range airports {
		if _, err := stmt.Exec(a.ID, a.Name, a.Country, a.IATA, a.ICAO, a.RWY, now); err != nil {
			_ = tx.Rollback()

			return fmt.Errorf("airport id=%d: %w", a.ID, err)
		}
	}

	return tx.Commit()
}

// importRoutes streams routes.parquet sequentially, pairing each row with the
// (i, j) airport-index pair produced by the same nested-loop order used to
// build the file upstream (see package doc), and writes both directions since
// the catalog's RoutesFrom query only looks up by from_airport_id.
func importRoutes(db *sqlx.DB, path string, airports []airportRow) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := parquet.NewGenericReader[routeRow](f)
	defer reader.Close()

	n := len(airports)
	i, j := 0, 1

	total := reader.NumRows()
	slog.Info("importing routes", "pairs", total)

	buf := make([]routeRow, batchSize)
	imported := int64(0)

	for {
		count, readErr := reader.Read(buf)
		if count > 0 {
			if err := importRouteBatch(db, buf[:count], airports, &i, &j, n); err != nil {
				return err
			}

			imported += int64(count)
			slog.Info("import progress", "done", imported, "total", total)
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	if i != n-1 || j != n {
		return fmt.Errorf("route/airport count mismatch: iteration ended at i=%d j=%d, expected i=%d j=%d (N=%d) — routes.parquet and airports.parquet don't match", i, j, n-1, n, n)
	}

	return nil
}

func importRouteBatch(db *sqlx.DB, rows []routeRow, airports []airportRow, i, j *int, n int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO routes (from_airport_id, to_airport_id, distance_km, demand_y, demand_j, demand_f, demand_large, demand_heavy)
		VALUES (?, ?, ?, ?, ?, ?, 0, 0)
		ON CONFLICT(from_airport_id, to_airport_id) DO UPDATE SET
			distance_km = excluded.distance_km,
			demand_y    = excluded.demand_y,
			demand_j    = excluded.demand_j,
			demand_f    = excluded.demand_f
	`)
	if err != nil {
		_ = tx.Rollback()

		return err
	}
	defer stmt.Close()

	for _, r := range rows {
		if *j >= n {
			_ = tx.Rollback()

			return fmt.Errorf("more route rows than airport pairs (N=%d)", n)
		}

		fromID, toID := airports[*i].ID, airports[*j].ID
		distanceKm := int(r.D + 0.5)

		if _, err := stmt.Exec(fromID, toID, distanceKm, r.YD, r.JD, r.FD); err != nil {
			_ = tx.Rollback()

			return fmt.Errorf("route %d->%d: %w", fromID, toID, err)
		}
		if _, err := stmt.Exec(toID, fromID, distanceKm, r.YD, r.JD, r.FD); err != nil {
			_ = tx.Rollback()

			return fmt.Errorf("route %d->%d: %w", toID, fromID, err)
		}

		*j++
		if *j == n {
			*i++
			*j = *i + 1
		}
	}

	return tx.Commit()
}

func readAircraft(path string) ([]aircraftRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := parquet.NewGenericReader[aircraftRow](f)
	defer reader.Close()

	aircrafts := make([]aircraftRow, 0, reader.NumRows())
	buf := make([]aircraftRow, 256)

	for {
		n, err := reader.Read(buf)
		aircrafts = append(aircrafts, buf[:n]...)

		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	return aircrafts, nil
}

// importAircraft writes every (airframe, engine) row as a distinct catalog
// aircraft — see package doc for why this must not collapse to one row per
// airframe.
func importAircraft(db *sqlx.DB, aircrafts []aircraftRow) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO aircraft (name, airframe, engine, ac_type, cruise_speed_kmh, max_range_km, min_runway_ft, capacity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			airframe         = excluded.airframe,
			engine           = excluded.engine,
			ac_type          = excluded.ac_type,
			cruise_speed_kmh = excluded.cruise_speed_kmh,
			max_range_km     = excluded.max_range_km,
			min_runway_ft    = excluded.min_runway_ft,
			capacity         = excluded.capacity
	`)
	if err != nil {
		_ = tx.Rollback()

		return err
	}
	defer stmt.Close()

	for _, a := range aircrafts {
		acType, ok := aircraftTypeNames[a.Type]
		if !ok {
			_ = tx.Rollback()

			return fmt.Errorf("aircraft %q: unknown type code %d", a.Name, a.Type)
		}

		// name stays the unique lookup key exactly as before (composite
		// "<airframe> (<engine>)") — the calculator's calculate request,
		// download filename, and GetAircraft lookup all key off it unchanged.
		// airframe/engine are separate columns purely so the catalog can sort
		// engine variants correctly (by speed, not by engine name — see
		// ListAircraft) and the UI can compose the display label from clean
		// parts instead of parsing the composite string.
		name := fmt.Sprintf("%s (%s)", a.Name, a.EName)
		speedKmh := int(math.Round(float64(a.Speed)))

		if _, err := stmt.Exec(name, a.Name, a.EName, acType, speedKmh, a.Range, a.RWY, a.Capacity); err != nil {
			_ = tx.Rollback()

			return fmt.Errorf("aircraft %q: %w", name, err)
		}
	}

	return tx.Commit()
}
