package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ashokhin/am4bot/internal/store"
)

// vpnRegionResponse is deliberately name-only -- the ovpn file contents
// are the shared VPN account's business, never sent to a browser.
type vpnRegionResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func toVPNRegionResponse(v *store.VPNRegion) vpnRegionResponse {
	return vpnRegionResponse{ID: v.ID, Name: v.Name}
}

// handleListVPNRegions is available to any authenticated user (not just
// admins) -- everyone needs to see the catalog to pick their own region.
func (s *Server) handleListVPNRegions(w http.ResponseWriter, r *http.Request) {
	regions, err := s.store.ListVPNRegions(r.Context())
	if err != nil {
		slog.Error("listing vpn regions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list vpn regions")

		return
	}

	resp := make([]vpnRegionResponse, len(regions))
	for i, v := range regions {
		resp[i] = toVPNRegionResponse(&v)
	}

	writeJSON(w, http.StatusOK, resp)
}

type createVPNRegionRequest struct {
	Name       string `json:"name"`
	OVPNConfig string `json:"ovpn_config"`
}

// handleCreateVPNRegion is admin-only: curating the region catalog is an
// operational decision, not something individual users make.
func (s *Server) handleCreateVPNRegion(w http.ResponseWriter, r *http.Request) {
	var req createVPNRegionRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.Name == "" || req.OVPNConfig == "" {
		writeError(w, http.StatusBadRequest, "name and ovpn_config are required")

		return
	}

	ovpnEnc, err := s.encryptor.Encrypt(req.OVPNConfig)
	if err != nil {
		slog.Error("encrypting vpn region ovpn config", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create vpn region")

		return
	}

	v, err := s.store.CreateVPNRegion(r.Context(), req.Name, ovpnEnc)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "a vpn region with that name already exists")

			return
		}

		slog.Error("creating vpn region", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create vpn region")

		return
	}

	s.audit(r, "create_vpn_region", strconv.FormatInt(v.ID, 10))
	writeJSON(w, http.StatusCreated, toVPNRegionResponse(v))
}

// handleDeleteVPNRegion is admin-only. Any user currently pointing at this
// region falls back to no VPN -- see migrations/0001_init.sql's
// ON DELETE SET NULL.
func (s *Server) handleDeleteVPNRegion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid vpn region id")

		return
	}

	if err := s.store.DeleteVPNRegion(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "vpn region not found")

			return
		}

		slog.Error("deleting vpn region", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete vpn region")

		return
	}

	s.audit(r, "delete_vpn_region", strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}

type vpnProviderStatusResponse struct {
	Configured bool   `json:"configured"`
	Provider   string `json:"provider,omitempty"`
}

// handleGetVPNProviderStatus is admin-only: reports whether the shared VPN
// account has been configured yet, without ever exposing its secrets back
// out (same write-only pattern as a node's game password).
func (s *Server) handleGetVPNProviderStatus(w http.ResponseWriter, r *http.Request) {
	creds, err := s.store.GetVPNProviderCredentials(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, vpnProviderStatusResponse{Configured: false})

			return
		}

		slog.Error("getting vpn provider credentials", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load vpn provider status")

		return
	}

	writeJSON(w, http.StatusOK, vpnProviderStatusResponse{Configured: true, Provider: creds.Provider})
}

type setVPNProviderRequest struct {
	Provider    string `json:"provider"`
	VPNUsername string `json:"vpn_username"`
	VPNPassword string `json:"vpn_password"`
}

// handleSetVPNProviderCredentials is admin-only: (re)configures the one
// shared VPN account every region in the catalog connects through.
func (s *Server) handleSetVPNProviderCredentials(w http.ResponseWriter, r *http.Request) {
	var req setVPNProviderRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.VPNUsername == "" || req.VPNPassword == "" {
		writeError(w, http.StatusBadRequest, "vpn_username and vpn_password are required")

		return
	}

	if req.Provider == "" {
		req.Provider = "custom"
	}

	usernameEnc, err := s.encryptor.Encrypt(req.VPNUsername)
	if err != nil {
		slog.Error("encrypting vpn username", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set vpn provider credentials")

		return
	}

	passwordEnc, err := s.encryptor.Encrypt(req.VPNPassword)
	if err != nil {
		slog.Error("encrypting vpn password", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set vpn provider credentials")

		return
	}

	creds, err := s.store.SetVPNProviderCredentials(r.Context(), req.Provider, usernameEnc, passwordEnc)
	if err != nil {
		slog.Error("setting vpn provider credentials", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set vpn provider credentials")

		return
	}

	s.audit(r, "set_vpn_provider_credentials", "")
	writeJSON(w, http.StatusOK, vpnProviderStatusResponse{Configured: true, Provider: creds.Provider})
}

type setUserVPNRegionRequest struct {
	VPNRegionID *int64 `json:"vpn_region_id"`
}

// handleSetMyVPNRegion is self-service (requireAuth, not requireAdmin): a
// user picks their own exit region, applied to every one of their nodes --
// see vpn_regions.go's doc comment for why this is a per-user field, not
// a per-node one.
func (s *Server) handleSetMyVPNRegion(w http.ResponseWriter, r *http.Request) {
	userID, err := s.currentUserID(r)
	if err != nil {
		slog.Error("resolving current user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set vpn region")

		return
	}

	var req setUserVPNRegionRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.VPNRegionID != nil {
		if _, err := s.store.GetVPNRegionByID(r.Context(), *req.VPNRegionID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusBadRequest, "no such vpn region")

				return
			}

			slog.Error("looking up vpn region", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to set vpn region")

			return
		}
	}

	if err := s.store.SetUserVPNRegion(r.Context(), userID, req.VPNRegionID); err != nil {
		slog.Error("setting user vpn region", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set vpn region")

		return
	}

	s.audit(r, "set_my_vpn_region", strconv.FormatInt(userID, 10))
	w.WriteHeader(http.StatusNoContent)
}
