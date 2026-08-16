// Command mergecatalog combines catalog SQLite files produced by several
// parallel, ID-range-sharded full_catalog_scanner runs (see
// Config.CatalogAirportIDMin/Max) into one output file.
//
// Merge trust model mirrors io.CatalogWriter's own two-tier rule
// (UpsertAirport vs UpsertAirportIfUnset): within each shard, an airport row
// with scanned_at set is that shard's authoritative result for an airport it
// actually owned (its ID fell in that shard's range); a row with scanned_at
// NULL is just an opportunistic sighting (e.g. as someone else's route
// destination) and must never be allowed to overwrite another shard's
// authoritative data for the same airport ID. So this merges in two passes
// across every shard: authoritative rows first (unconditional overwrite),
// then opportunistic rows (fill gaps only). Routes are shard-exclusive by
// construction (only the owning shard ever searches FROM a given airport),
// so they're merged with a plain insert-if-missing.
package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/ashokhin/am4bot/internal/io"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <output.db> <shard1.db> [shard2.db ...]\n", os.Args[0])
		os.Exit(1)
	}

	outputPath := os.Args[1]
	shardPaths := os.Args[2:]

	// NewCatalogWriter ensures the output file has the full current schema
	// (including migrations) before we merge into it, same as a fresh scan
	// would produce. Closed immediately after — the merge below uses its own
	// connection, and two open handles to the same file would just risk
	// avoidable lock contention.
	writer, err := io.NewCatalogWriter(outputPath)
	if err != nil {
		slog.Error("opening output catalog", "path", outputPath, "error", err)
		os.Exit(1)
	}
	if err := writer.Close(); err != nil {
		slog.Error("closing output catalog after schema setup", "error", err)
		os.Exit(1)
	}

	db, err := sqlx.Open("sqlite", outputPath+"?_busy_timeout=5000")
	if err != nil {
		slog.Error("opening output catalog for merge", "path", outputPath, "error", err)
		os.Exit(1)
	}
	defer db.Close()

	for _, shardPath := range shardPaths {
		slog.Info("merging shard", "path", shardPath)

		if err := mergeShard(db, shardPath); err != nil {
			slog.Error("merging shard failed", "path", shardPath, "error", err)
			os.Exit(1)
		}
	}

	var airportCount, routeCount int
	_ = db.Get(&airportCount, `SELECT COUNT(*) FROM airports`)
	_ = db.Get(&routeCount, `SELECT COUNT(*) FROM routes`)

	slog.Info("merge complete", "output", outputPath, "shards_merged", len(shardPaths),
		"total_airports", airportCount, "total_routes", routeCount)
}

func mergeShard(db *sqlx.DB, shardPath string) error {
	if _, err := db.Exec(`ATTACH DATABASE ? AS shard`, shardPath); err != nil {
		return fmt.Errorf("attaching: %w", err)
	}
	defer func() {
		if _, err := db.Exec(`DETACH DATABASE shard`); err != nil {
			slog.Warn("detaching shard database", "path", shardPath, "error", err)
		}
	}()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if err := mergeShardTx(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			slog.Warn("rolling back failed merge", "error", rbErr)
		}

		return err
	}

	return tx.Commit()
}

func mergeShardTx(tx *sql.Tx) error {
	// Pass 1: authoritative rows (scanned_at set) — this shard's own
	// airports, unconditionally overwrite whatever's already in the output.
	if _, err := tx.Exec(`
		INSERT INTO airports (id, name, country, iata, icao, runway_ft, scanned_at)
		SELECT id, name, country, iata, icao, runway_ft, scanned_at
		FROM shard.airports WHERE scanned_at IS NOT NULL
		ON CONFLICT(id) DO UPDATE SET
			name       = excluded.name,
			country    = excluded.country,
			iata       = COALESCE(excluded.iata, airports.iata),
			icao       = COALESCE(excluded.icao, airports.icao),
			runway_ft  = COALESCE(excluded.runway_ft, airports.runway_ft),
			scanned_at = excluded.scanned_at
	`); err != nil {
		return fmt.Errorf("merging authoritative airports: %w", err)
	}

	// Pass 2: opportunistic rows (scanned_at still NULL in the shard) — fill
	// gaps only, never overwrite a name/country/code/runway already present
	// (whether from this merge's pass 1, an earlier shard, or an
	// already-authoritative row in the output).
	if _, err := tx.Exec(`
		INSERT INTO airports (id, name, country, iata, icao, runway_ft)
		SELECT id, name, country, iata, icao, runway_ft
		FROM shard.airports WHERE scanned_at IS NULL
		ON CONFLICT(id) DO UPDATE SET
			name      = CASE WHEN airports.name = ''    THEN excluded.name    ELSE airports.name    END,
			country   = CASE WHEN airports.country = '' THEN excluded.country ELSE airports.country END,
			iata      = COALESCE(airports.iata, excluded.iata),
			icao      = COALESCE(airports.icao, excluded.icao),
			runway_ft = COALESCE(airports.runway_ft, excluded.runway_ft)
	`); err != nil {
		return fmt.Errorf("merging opportunistic airports: %w", err)
	}

	// Routes are shard-exclusive by construction (only the shard owning
	// from_airport_id ever searches from it) — insert whatever's missing,
	// leave any existing row alone.
	//
	// INSERT OR IGNORE, not "ON CONFLICT ... DO NOTHING": the latter hits a
	// modernc.org/sqlite parser bug ("near \"DO\": syntax error") specific
	// to INSERT...SELECT across an ATTACHed database — confirmed it doesn't
	// happen without ATTACH, and doesn't happen with DO UPDATE (used above),
	// only DO NOTHING combined with ATTACH. INSERT OR IGNORE gives the same
	// "skip on conflict" semantics without tripping it.
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO routes (from_airport_id, to_airport_id, distance_km, demand_y, demand_j, demand_f, demand_large, demand_heavy)
		SELECT from_airport_id, to_airport_id, distance_km, demand_y, demand_j, demand_f, demand_large, demand_heavy
		FROM shard.routes
	`); err != nil {
		return fmt.Errorf("merging routes: %w", err)
	}

	return nil
}
