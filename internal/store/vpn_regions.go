package store

import (
	"context"
	"fmt"
	"time"
)

// This file implements the actual VPN model: there is exactly ONE VPN
// provider account, shared by every user, whose credentials the admin
// configures once (VPNProviderCredentials, a singleton row). The admin
// also curates a catalog of exit regions (VPNRegion) -- each just an
// .ovpn file naming a different server/location for that same account.
// A user picks ONE region from that catalog (users.vpn_region_id) and
// every one of their nodes
// exits through it -- there is deliberately no per-node VPN choice, so a
// user's game bot and any other of their tooling always share one IP.

// VPNRegion is one entry in the admin-curated catalog of VPN exit
// locations, all under the one shared provider account.
type VPNRegion struct {
	ID            int64     `db:"id"`
	Name          string    `db:"name"`
	OVPNConfigEnc string    `db:"ovpn_config_enc"`
	CreatedAt     time.Time `db:"created_at"`
}

// CreateVPNRegion adds a new region to the catalog. ovpnConfigEnc must
// already be ciphertext (internal/secrets.Encrypt).
func (s *Store) CreateVPNRegion(ctx context.Context, name, ovpnConfigEnc string) (*VPNRegion, error) {
	var v VPNRegion

	err := s.db.GetContext(ctx, &v, `
		INSERT INTO vpn_regions (name, ovpn_config_enc)
		VALUES ($1, $2)
		RETURNING id, name, ovpn_config_enc, created_at
	`, name, ovpnConfigEnc)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a vpn region named %q already exists", ErrConflict, name)
		}

		return nil, fmt.Errorf("creating vpn region: %w", err)
	}

	return &v, nil
}

// ListVPNRegions returns the whole catalog, alphabetically -- there's no
// per-user scoping, every user picks from the same list.
func (s *Store) ListVPNRegions(ctx context.Context) ([]VPNRegion, error) {
	var regions []VPNRegion

	if err := s.db.SelectContext(ctx, &regions, `
		SELECT id, name, ovpn_config_enc, created_at FROM vpn_regions ORDER BY name
	`); err != nil {
		return nil, fmt.Errorf("listing vpn regions: %w", err)
	}

	return regions, nil
}

// GetVPNRegionByID looks up one region by id -- used both to validate a
// user's region choice and by the provisioning flow to fetch its ovpn file.
func (s *Store) GetVPNRegionByID(ctx context.Context, id int64) (*VPNRegion, error) {
	var v VPNRegion

	err := s.db.GetContext(ctx, &v, `
		SELECT id, name, ovpn_config_enc, created_at FROM vpn_regions WHERE id = $1
	`, id)
	if err != nil {
		return nil, wrapNotFound(err, "vpn region")
	}

	return &v, nil
}

// DeleteVPNRegion removes a region from the catalog. Any user currently
// pointing at it falls back to no VPN (users.vpn_region_id references this
// ON DELETE SET NULL) rather than being blocked -- see
// migrations/0001_init.sql.
func (s *Store) DeleteVPNRegion(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM vpn_regions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting vpn region: %w", err)
	}

	return checkRowsAffected(res, "vpn region")
}

// VPNProviderCredentials is the single shared VPN account every region in
// the catalog above connects through -- one admin-entered username/
// password, reused for every user and every region. Modeled as a
// singleton row (id always 1) rather than a config-file setting so it can
// be rotated from the admin UI without a redeploy.
type VPNProviderCredentials struct {
	Provider       string    `db:"provider"`
	VPNUsernameEnc string    `db:"vpn_username_enc"`
	VPNPasswordEnc string    `db:"vpn_password_enc"`
	UpdatedAt      time.Time `db:"updated_at"`
}

// GetVPNProviderCredentials returns the configured provider account.
// Returns ErrNotFound if the admin hasn't set one up yet.
func (s *Store) GetVPNProviderCredentials(ctx context.Context) (*VPNProviderCredentials, error) {
	var c VPNProviderCredentials

	err := s.db.GetContext(ctx, &c, `
		SELECT provider, vpn_username_enc, vpn_password_enc, updated_at FROM vpn_provider_credentials WHERE id = 1
	`)
	if err != nil {
		return nil, wrapNotFound(err, "vpn provider credentials")
	}

	return &c, nil
}

// SetVPNProviderCredentials creates or replaces the single shared VPN
// account. usernameEnc/passwordEnc must already be ciphertext.
func (s *Store) SetVPNProviderCredentials(ctx context.Context, provider, usernameEnc, passwordEnc string) (*VPNProviderCredentials, error) {
	var c VPNProviderCredentials

	err := s.db.GetContext(ctx, &c, `
		INSERT INTO vpn_provider_credentials (id, provider, vpn_username_enc, vpn_password_enc, updated_at)
		VALUES (1, $1, $2, $3, now())
		ON CONFLICT (id) DO UPDATE SET
			provider = EXCLUDED.provider,
			vpn_username_enc = EXCLUDED.vpn_username_enc,
			vpn_password_enc = EXCLUDED.vpn_password_enc,
			updated_at = now()
		RETURNING provider, vpn_username_enc, vpn_password_enc, updated_at
	`, provider, usernameEnc, passwordEnc)
	if err != nil {
		return nil, fmt.Errorf("setting vpn provider credentials: %w", err)
	}

	return &c, nil
}
