package io

import (
	"encoding/csv"
	"log/slog"
	"os"
	"slices"
	"strconv"

	"github.com/ashokhin/am4bot/internal/model"
)

// Writer handles writing route data to a CSV file.
type Writer struct {
	file       *os.File
	writer     *csv.Writer
	routesList []string
}

// NewWriter creates a new Writer instance for the specified file path.
// It initializes the CSV file with the appropriate headers.
func NewWriter(filePath string) (*Writer, error) {
	f, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	w := csv.NewWriter(f)

	// Write CSV header
	header := []string{
		"RouteName",
		"Distance",
		"Runway",
		"DemandY",
		"DemandJ",
		"DemandF",
		"DemandLarge",
		"DemandHeavy",
	}

	if err := w.Write(header); err != nil {
		f.Close()
		return nil, err
	}

	return &Writer{
		file:       f,
		writer:     w,
		routesList: []string{},
	}, nil
}

// WriteRoute writes a route to the CSV file if it hasn't been written before.
func (w *Writer) WriteRoute(routeKey string, route model.Route) error {
	if slices.Contains(w.routesList, routeKey) {
		return nil
	}

	w.routesList = append(w.routesList, routeKey)
	slog.Debug("found route", "route_key", routeKey, "route", route)

	return w.writer.Write([]string{
		route.Name,
		strconv.Itoa(route.Distance),
		strconv.Itoa(route.Runway),
		strconv.Itoa(route.DemandY),
		strconv.Itoa(route.DemandJ),
		strconv.Itoa(route.DemandF),
		strconv.Itoa(route.DemandLarge),
		strconv.Itoa(route.DemandHeavy),
	})
}

// Close flushes the CSV writer and closes the underlying file.
func (w *Writer) Close() error {
	w.writer.Flush()
	return w.file.Close()
}
