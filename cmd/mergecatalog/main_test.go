package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ashokhin/am4bot/internal/io"
	"github.com/jmoiron/sqlx"
)

func newCatalog(t *testing.T, dir, name string) *sqlx.DB {
	t.Helper()

	path := filepath.Join(dir, name)

	w, err := io.NewCatalogWriter(path)
	if err != nil {
		t.Fatalf("NewCatalogWriter(%q) error = %v", path, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	db, err := sqlx.Open("sqlite", path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("sqlx.Open(%q) error = %v", path, err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

func TestMergeShardTx(t *testing.T) {
	dir := t.TempDir()

	output := newCatalog(t, dir, "output.db")
	shardA := newCatalog(t, dir, "shardA.db")
	shardB := newCatalog(t, dir, "shardB.db")

	// shardA owns airport 1 (scanned, authoritative) and has an
	// opportunistic (unscanned) sighting of airport 2 with a wrong-looking
	// country — must not survive the merge once shardB's authoritative row
	// for 2 is merged in.
	shardA.MustExec(`INSERT INTO airports (id, name, country, iata, scanned_at) VALUES (1, 'Herat', 'Afghanistan', 'HEA', 100)`)
	shardA.MustExec(`INSERT INTO airports (id, name, country) VALUES (2, 'Bogus', 'Wrongland')`)
	shardA.MustExec(`INSERT INTO routes (from_airport_id, to_airport_id, distance_km, demand_y, demand_j, demand_f) VALUES (1, 2, 500, 100, 20, 5)`)

	// shardB owns airport 2 (scanned, authoritative) and only knows airport
	// 1's runway opportunistically (gap-fill case: output's airport 1
	// already has iata from shardA, must keep it, not overwrite with NULL).
	shardB.MustExec(`INSERT INTO airports (id, name, country, iata, scanned_at) VALUES (2, 'Kabul', 'Afghanistan', 'KBL', 200)`)
	shardB.MustExec(`INSERT INTO airports (id, name, country, runway_ft) VALUES (1, '', '', 9000)`)
	shardB.MustExec(`INSERT INTO routes (from_airport_id, to_airport_id, distance_km, demand_y, demand_j, demand_f) VALUES (2, 1, 500, 90, 15, 3)`)

	for _, shardPath := range []string{filepath.Join(dir, "shardA.db"), filepath.Join(dir, "shardB.db")} {
		if err := mergeShard(output, shardPath); err != nil {
			t.Fatalf("mergeShard(%q) error = %v", shardPath, err)
		}
	}

	var a1, a2 struct {
		Name     string
		Country  string
		IATA     sql.NullString `db:"iata"`
		RunwayFt sql.NullInt64  `db:"runway_ft"`
	}
	if err := output.Get(&a1, `SELECT name, country, iata, runway_ft FROM airports WHERE id = 1`); err != nil {
		t.Fatalf("querying airport 1: %v", err)
	}
	if a1.Name != "Herat" || a1.Country != "Afghanistan" || a1.IATA.String != "HEA" {
		t.Errorf("airport 1 = %+v, want Herat/Afghanistan/HEA preserved", a1)
	}
	if !a1.RunwayFt.Valid || a1.RunwayFt.Int64 != 9000 {
		t.Errorf("airport 1 runway_ft = %+v, want gap filled to 9000 from shardB", a1.RunwayFt)
	}

	if err := output.Get(&a2, `SELECT name, country, iata, runway_ft FROM airports WHERE id = 2`); err != nil {
		t.Fatalf("querying airport 2: %v", err)
	}
	if a2.Name != "Kabul" || a2.Country != "Afghanistan" || a2.IATA.String != "KBL" {
		t.Errorf("airport 2 = %+v, want Kabul/Afghanistan/KBL (authoritative shardB row), not shardA's opportunistic 'Bogus'/'Wrongland'", a2)
	}

	var routeCount int
	if err := output.Get(&routeCount, `SELECT COUNT(*) FROM routes`); err != nil {
		t.Fatalf("counting routes: %v", err)
	}
	if routeCount != 2 {
		t.Errorf("route count = %d, want 2 (one from each shard, both directions kept)", routeCount)
	}
}

func TestMergeShardTx_Idempotent(t *testing.T) {
	dir := t.TempDir()

	output := newCatalog(t, dir, "output.db")
	shard := newCatalog(t, dir, "shard.db")

	shard.MustExec(`INSERT INTO airports (id, name, country, scanned_at) VALUES (1, 'Herat', 'Afghanistan', 100)`)
	shard.MustExec(`INSERT INTO routes (from_airport_id, to_airport_id, distance_km, demand_y, demand_j, demand_f) VALUES (1, 2, 500, 100, 20, 5)`)

	shardPath := filepath.Join(dir, "shard.db")

	if err := mergeShard(output, shardPath); err != nil {
		t.Fatalf("first mergeShard() error = %v", err)
	}
	if err := mergeShard(output, shardPath); err != nil {
		t.Fatalf("second mergeShard() error = %v", err)
	}

	var airportCount, routeCount int
	_ = output.Get(&airportCount, `SELECT COUNT(*) FROM airports`)
	_ = output.Get(&routeCount, `SELECT COUNT(*) FROM routes`)

	if airportCount != 1 || routeCount != 1 {
		t.Errorf("after merging the same shard twice: airports=%d routes=%d, want 1/1 (no duplication)", airportCount, routeCount)
	}
}
