// Package poolsclient is the anonymous control-api client for the
// M3.5 GET /v1/pools customer-facing coverage discovery surface.
//
// Separate from the api-key-authenticated internal/client because
// /v1/pools is the only customer endpoint that takes no auth — a
// CLI user without `hopnet auth login` should be able to run
// `hopnet pools list` against api.hopnet.io. Per
// notes/milestone-3.5/coverage-union/phase-plan.md §6 + §7.
package poolsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultBaseURL points at the production control-api. Override via
// HOPNET_API_BASE_URL for local dev (e.g. http://127.0.0.1:8080).
const DefaultBaseURL = "https://api.hopnet.io"

// Client is a no-auth HTTP client.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New constructs a Client honoring HOPNET_API_BASE_URL when set,
// falling back to DefaultBaseURL. The CLI's auth-bearing config
// (BaseURL field on the on-disk config) is intentionally ignored —
// `hopnet pools list` is anonymous and must work pre-`auth login`.
func New() *Client {
	base := DefaultBaseURL
	if v := strings.TrimSpace(os.Getenv("HOPNET_API_BASE_URL")); v != "" {
		base = v
	}
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// PoolEntry mirrors the @hopnetllc/schema PoolEntry shape (JSON-wire
// — no shared Go types repo yet; Go unmarshal ignores unknown keys
// so additive server-side changes don't require a CLI release).
type PoolEntry struct {
	RouteClass      string   `json:"route_class"`
	Countries       []string `json:"countries"`
	StickyCountries []string `json:"sticky_countries"`
	Status          string   `json:"status"`
}

// PoolsResponse is the GET /v1/pools wire shape.
type PoolsResponse struct {
	Pools       []PoolEntry `json:"pools"`
	GeneratedAt string      `json:"generated_at"`
}

// ListPools returns the customer-facing coverage union.
func (c *Client) ListPools(ctx context.Context) (*PoolsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/pools", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /v1/pools: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET /v1/pools returned status %d", resp.StatusCode)
	}
	var out PoolsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode /v1/pools: %w", err)
	}
	return &out, nil
}
