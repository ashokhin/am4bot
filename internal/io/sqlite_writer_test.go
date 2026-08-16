package io

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestUpsertAirportIfUnset(t *testing.T) {
	w, err := NewCatalogWriter(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("NewCatalogWriter() error = %v", err)
	}
	defer w.Close()

	// Authoritative write (e.g. from the country dropdown) sets the real data.
	if err := w.UpsertAirport(1, "Herat", "Afghanistan"); err != nil {
		t.Fatalf("UpsertAirport() error = %v", err)
	}

	// A later, opportunistic write (e.g. mis-parsed route-result text) must
	// not clobber it.
	if err := w.UpsertAirportIfUnset(1, "Gyumri", "Armenia"); err != nil {
		t.Fatalf("UpsertAirportIfUnset() error = %v", err)
	}

	var gotName, gotCountry string
	if err := w.db.QueryRow(`SELECT name, country FROM airports WHERE id = ?`, 1).Scan(&gotName, &gotCountry); err != nil {
		t.Fatalf("querying airport: %v", err)
	}
	if gotName != "Herat" || gotCountry != "Afghanistan" {
		t.Fatalf("UpsertAirportIfUnset() overwrote authoritative data: got name=%q country=%q, want Herat/Afghanistan", gotName, gotCountry)
	}

	// On a brand-new (or stub) row, it should still fill in the data.
	if err := w.UpsertAirportIfUnset(2, "Gyumri", "Armenia"); err != nil {
		t.Fatalf("UpsertAirportIfUnset() on new row error = %v", err)
	}

	if err := w.db.QueryRow(`SELECT name, country FROM airports WHERE id = ?`, 2).Scan(&gotName, &gotCountry); err != nil {
		t.Fatalf("querying airport: %v", err)
	}
	if gotName != "Gyumri" || gotCountry != "Armenia" {
		t.Fatalf("UpsertAirportIfUnset() on new row = name=%q country=%q, want Gyumri/Armenia", gotName, gotCountry)
	}
}

func TestScanProgress_RoundTrip(t *testing.T) {
	w, err := NewCatalogWriter(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("NewCatalogWriter() error = %v", err)
	}
	defer w.Close()

	if p, err := w.GetScanProgress(1); err != nil {
		t.Fatalf("GetScanProgress() error = %v", err)
	} else if p.DistanceKm.Valid || p.RunwayFt.Valid {
		t.Fatalf("GetScanProgress() on unseen airport = %+v, want zero value", p)
	}

	if err := w.SetScanProgress(1, 5000, sql.NullInt64{}); err != nil {
		t.Fatalf("SetScanProgress() error = %v", err)
	}

	p, err := w.GetScanProgress(1)
	if err != nil {
		t.Fatalf("GetScanProgress() error = %v", err)
	}
	if !p.DistanceKm.Valid || p.DistanceKm.Int64 != 5000 || p.RunwayFt.Valid {
		t.Fatalf("GetScanProgress() = %+v, want DistanceKm=5000, RunwayFt=NULL", p)
	}

	if err := w.SetScanProgress(1, 5000, sql.NullInt64{Int64: 6500, Valid: true}); err != nil {
		t.Fatalf("SetScanProgress() error = %v", err)
	}

	p, err = w.GetScanProgress(1)
	if err != nil {
		t.Fatalf("GetScanProgress() error = %v", err)
	}
	if !p.RunwayFt.Valid || p.RunwayFt.Int64 != 6500 {
		t.Fatalf("GetScanProgress() = %+v, want RunwayFt=6500", p)
	}

	if err := w.MarkAirportScanned(1); err != nil {
		t.Fatalf("MarkAirportScanned() error = %v", err)
	}

	p, err = w.GetScanProgress(1)
	if err != nil {
		t.Fatalf("GetScanProgress() error = %v", err)
	}
	if p.DistanceKm.Valid || p.RunwayFt.Valid {
		t.Fatalf("GetScanProgress() after MarkAirportScanned = %+v, want cleared", p)
	}
}

func TestCreateSchema_MigrationIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "catalog.db")

	w1, err := NewCatalogWriter(dbPath)
	if err != nil {
		t.Fatalf("NewCatalogWriter() error = %v", err)
	}
	if err := w1.UpsertAirport(1, "Herat", "Afghanistan"); err != nil {
		t.Fatalf("UpsertAirport() error = %v", err)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Reopening an existing database re-runs createSchema's ALTER TABLEs —
	// this must not fail with "duplicate column name" the second time around.
	w2, err := NewCatalogWriter(dbPath)
	if err != nil {
		t.Fatalf("NewCatalogWriter() on existing db error = %v", err)
	}
	defer w2.Close()

	if err := w2.SetScanProgress(1, 1000, sql.NullInt64{}); err != nil {
		t.Fatalf("SetScanProgress() on reopened db error = %v", err)
	}
}
