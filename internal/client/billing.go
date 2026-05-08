package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// CreateCheckout is POST /v1/billing/checkout. Returns the Stripe Checkout
// URL the customer should open in a browser. idempotencyKey is required
// (server returns 400 without it); the server forwards it to Stripe so a
// retry — network blip, double-submit — won't create a second session and
// risk duplicate charges. Server-side amount gate: [5, 10000] USD.
func (c *Client) CreateCheckout(ctx context.Context, amountUSD int, idempotencyKey string) (*CheckoutResponse, error) {
	body, err := json.Marshal(&CheckoutRequest{AmountUSD: amountUSD})
	if err != nil {
		return nil, fmt.Errorf("marshal checkout request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/billing/checkout", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /v1/billing/checkout: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out CheckoutResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("decode checkout response: %w", err)
		}
		return &out, nil
	}
	return nil, mapErrorResponse(resp)
}

// GetBilling is GET /v1/account/billing. Returns the current balance plus
// the 5 most recent ledger entries.
func (c *Client) GetBilling(ctx context.Context) (*BillingResponse, error) {
	var out BillingResponse
	if err := c.do(ctx, http.MethodGet, "/v1/account/billing", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetBillingHistory is GET /v1/account/billing/history. limit clamps to
// [1, 200] server-side; before is an optional ISO8601 timestamp cursor.
func (c *Client) GetBillingHistory(ctx context.Context, limit int, before string) (*BillingHistoryResponse, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	if before != "" {
		q.Set("before", before)
	}
	path := "/v1/account/billing/history"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out BillingHistoryResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
