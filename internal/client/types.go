// Package client is a typed wrapper over the HopNet control API.
//
// Field naming and types here mirror the server's wire shapes exactly.
// When the server changes a field, this file changes; do not invent fields.
package client

import "time"

// Account is the response from GET /v1/account.
type Account struct {
	ID                   string `json:"id"`
	Email                string `json:"email"`
	Status               string `json:"status"`
	BalanceCents         int64  `json:"balance_cents"`
	AllowNegativeBalance bool   `json:"allow_negative_balance"`
}

// CreateRouteRequest mirrors the server's route-create schema.
// Pointer fields are omitempty so unset values are not sent (the server
// applies its own defaults).
type CreateRouteRequest struct {
	Label            string   `json:"label,omitempty"`
	TTLSeconds       int      `json:"ttl_seconds"`
	MaxMB            *int     `json:"max_mb,omitempty"`
	MaxCostCents     *int64   `json:"max_cost_cents,omitempty"`
	RouteClass       string   `json:"route_class"`
	Country          string   `json:"country,omitempty"`
	RequestedMinMbps *int     `json:"requested_min_mbps,omitempty"`
	Allow            []string `json:"allow,omitempty"`
	Deny             []string `json:"deny,omitempty"`
	ClientKind       string   `json:"client_kind"`
	TemplateName     string   `json:"template_name,omitempty"`
}

// CreateRouteResponse is the 201 body from POST /v1/routes. Token is
// returned exactly once.
type CreateRouteResponse struct {
	ID           string    `json:"id"`
	Token        string    `json:"token"`
	RouteClass   string    `json:"route_class"`
	Country      string    `json:"country,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	MaxBytes     *int64    `json:"max_bytes"`
	MaxCostCents *int64    `json:"max_cost_cents"`
	RouteVersion int       `json:"route_version"`
}

// Route mirrors api.ts publicRoute(). Used by GET /v1/routes/:id and the
// list response. allow_hosts / deny_hosts come back as raw JSON; we don't
// inspect them for any P8 use case so RawMessage-equivalent (any) is fine.
type Route struct {
	ID                 string     `json:"id"`
	Label              string     `json:"label"`
	Status             string     `json:"status"`
	RouteClass         string     `json:"route_class"`
	Country            string     `json:"country"`
	UpstreamEndpointID string     `json:"upstream_endpoint_id"`
	RequestedMinMbps   *int       `json:"requested_min_mbps"`
	AllowHosts         any        `json:"allow_hosts"`
	DenyHosts          any        `json:"deny_hosts"`
	MaxBytes           *int64     `json:"max_bytes"`
	MaxCostCents       *int64     `json:"max_cost_cents"`
	RouteVersion       int        `json:"route_version"`
	ClientKind         string     `json:"client_kind"`
	TemplateName       string     `json:"template_name"`
	CreatedAt          time.Time  `json:"created_at"`
	ExpiresAt          *time.Time `json:"expires_at"`
	RevokedAt          *time.Time `json:"revoked_at"`
}

// ListRoutesResponse is GET /v1/routes.
type ListRoutesResponse struct {
	Routes []Route `json:"routes"`
}

// Destination is one row in the per-destination breakdown of a usage
// response.
type Destination struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Tunnels int    `json:"tunnels"`
	Bytes   int64  `json:"bytes"`
}

// Usage is GET /v1/routes/:id/usage. Bytes is the sum of the c2u + u2c
// columns; both directions are also exposed individually.
type Usage struct {
	ID                    string        `json:"id"`
	Status                string        `json:"status"`
	Bytes                 int64         `json:"bytes"`
	BytesClientToUpstream int64         `json:"bytes_client_to_upstream"`
	BytesUpstreamToClient int64         `json:"bytes_upstream_to_client"`
	EstimatedCostCents    int64         `json:"estimated_cost_cents"`
	ObservedAvgMbps       *float64      `json:"observed_avg_mbps"`
	LastFlushedAt         *time.Time    `json:"last_flushed_at"`
	CreatedAt             time.Time     `json:"created_at"`
	ExpiresAt             *time.Time    `json:"expires_at"`
	Destinations          []Destination `json:"destinations"`
}

// errorBody is the shape the server uses for non-2xx responses. Not all
// fields are always set; we read what's present.
type errorBody struct {
	Error        string `json:"error"`
	BalanceCents *int64 `json:"balance_cents,omitempty"`
}

// CheckoutRequest is the body for POST /v1/billing/checkout (P9).
type CheckoutRequest struct {
	AmountUSD int `json:"amount_usd"`
}

// CheckoutResponse is the 200 body from POST /v1/billing/checkout. ExpiresAt
// is server-side ISO8601; absent for sessions without an expiry.
type CheckoutResponse struct {
	SessionID   string     `json:"session_id"`
	CheckoutURL string     `json:"checkout_url"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

// LedgerRow is one entry in the credit_ledger view exposed via
// /v1/account/billing and /v1/account/billing/history (P9).
type LedgerRow struct {
	ID                 string    `json:"id"`
	Type               string    `json:"type"`
	AmountCents        int64     `json:"amount_cents"`
	RouteID            *string   `json:"route_id"`
	UpstreamEndpointID *string   `json:"upstream_endpoint_id"`
	CreatedAt          time.Time `json:"created_at"`
}

// BillingResponse is GET /v1/account/billing. Recent is capped at 5 rows.
type BillingResponse struct {
	BalanceCents int64       `json:"balance_cents"`
	BalanceUSD   float64     `json:"balance_usd"`
	Currency     string      `json:"currency"`
	Recent       []LedgerRow `json:"recent"`
}

// BillingHistoryResponse is GET /v1/account/billing/history. NextBefore is
// the keyset cursor for the next page; null when fewer than `limit` rows
// were returned.
type BillingHistoryResponse struct {
	BalanceCents int64       `json:"balance_cents"`
	Rows         []LedgerRow `json:"rows"`
	NextBefore   *string     `json:"next_before"`
}
