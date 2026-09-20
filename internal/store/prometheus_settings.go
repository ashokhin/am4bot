package store

import (
	"context"
	"fmt"
	"time"
)

// PrometheusSettings is the single admin-configured Prometheus instance
// apiserver's metrics endpoint proxies queries to. Modeled as a singleton
// row (id always 1), same pattern as VPNProviderCredentials, so it's
// editable from the admin UI without a redeploy.
type PrometheusSettings struct {
	URL       string    `db:"url"`
	UpdatedAt time.Time `db:"updated_at"`
}

// GetPrometheusSettings returns the configured Prometheus URL. Returns
// ErrNotFound if the admin hasn't set one up yet.
func (s *Store) GetPrometheusSettings(ctx context.Context) (*PrometheusSettings, error) {
	var p PrometheusSettings

	err := s.db.GetContext(ctx, &p, `SELECT url, updated_at FROM prometheus_settings WHERE id = 1`)
	if err != nil {
		return nil, wrapNotFound(err, "prometheus settings")
	}

	return &p, nil
}

// SetPrometheusSettings creates or replaces the configured Prometheus URL.
func (s *Store) SetPrometheusSettings(ctx context.Context, url string) (*PrometheusSettings, error) {
	var p PrometheusSettings

	err := s.db.GetContext(ctx, &p, `
		INSERT INTO prometheus_settings (id, url, updated_at)
		VALUES (1, $1, now())
		ON CONFLICT (id) DO UPDATE SET url = EXCLUDED.url, updated_at = now()
		RETURNING url, updated_at
	`, url)
	if err != nil {
		return nil, fmt.Errorf("setting prometheus settings: %w", err)
	}

	return &p, nil
}
