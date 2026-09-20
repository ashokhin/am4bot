package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// apiSource holds the connection details for fetching a node's config
// from apiserver's internal endpoint (see internal/api's
// handleInternalGetNodeConfig) instead of reading a local config.yaml.
// Set only by NewFromAPI; nil for a file-based Config (New).
type apiSource struct {
	baseURL    string
	nodeID     int64
	token      string
	httpClient *http.Client
	// lastRaw is the exact JSON body from the last successful fetch.
	// ReloadConfigIfChanged has no cheap "has this changed" signal for a
	// remote resource the way it has a file's mtime -- comparing raw
	// bytes is the simplest way to tell "fetched again, byte-for-byte
	// identical" apart from an actual change, without needing the server
	// to support something like ETags.
	lastRaw []byte
}

// NewFromAPI creates a Config by fetching it from apiserver's internal
// node-config endpoint instead of reading a local config.yaml. Used only
// for nodes hosted through the multi-tenant control plane; standalone/OSS
// users keep using New. token authenticates as this one node -- callers
// should read it from an environment variable rather than a CLI flag, so
// it doesn't show up in `ps`.
func NewFromAPI(baseURL string, nodeID int64, token string) (*Config, error) {
	c := &Config{
		apiSrc: &apiSource{
			baseURL:    baseURL,
			nodeID:     nodeID,
			token:      token,
			httpClient: &http.Client{Timeout: 15 * time.Second},
		},
	}

	if _, err := c.loadFromAPI(); err != nil {
		return nil, err
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	return c, nil
}

// loadFromAPI fetches the current config from apiserver and, if its raw
// JSON differs from the last successful fetch, replaces *c with the
// freshly parsed value -- the same "build a fresh Config, then swap it
// in" shape loadConfig uses for the file-based path, so PromslogConfig
// and the apiSrc itself survive the swap the same way confFilePath does
// there. Returns whether the config actually changed.
func (c *Config) loadFromAPI() (bool, error) {
	url := fmt.Sprintf("%s/internal/nodes/%d/config", c.apiSrc.baseURL, c.apiSrc.nodeID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiSrc.token)

	resp, err := c.apiSrc.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("fetching config: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("fetching config: unexpected status %d: %s", resp.StatusCode, body)
	}

	if bytes.Equal(body, c.apiSrc.lastRaw) {
		return false, nil
	}

	fresh := new(Config)
	if err := json.Unmarshal(body, fresh); err != nil {
		return false, fmt.Errorf("parsing config JSON: %w", err)
	}

	// preserve internal/runtime fields that aren't part of the JSON body
	fresh.apiSrc = c.apiSrc
	fresh.apiSrc.lastRaw = body
	fresh.PromslogConfig = c.PromslogConfig

	*c = *fresh

	// securely store password, same as the file-based path
	c.safeStorePassword()

	return true, nil
}
