package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreateCheckoutPostsAmountReturnsURL(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/billing/checkout", r.URL.Path)
		require.Equal(t, "Bearer hn_live_testtoken1", r.Header.Get("Authorization"))
		require.Equal(t, "idem-fixed-1", r.Header.Get("Idempotency-Key"))
		body, _ := io.ReadAll(r.Body)
		var got CheckoutRequest
		require.NoError(t, json.Unmarshal(body, &got))
		require.Equal(t, 50, got.AmountUSD)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":   "cs_test_abc",
			"checkout_url": "https://checkout.stripe.com/c/pay/cs_test_abc",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	})
	resp, err := c.CreateCheckout(context.Background(), 50, "idem-fixed-1")
	require.NoError(t, err)
	require.Equal(t, "cs_test_abc", resp.SessionID)
	require.Contains(t, resp.CheckoutURL, "checkout.stripe.com")
	require.NotNil(t, resp.ExpiresAt)
}

func TestCreateCheckoutMapsBadRequestSentinel(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_body"})
	})
	_, err := c.CreateCheckout(context.Background(), 1, "idem-bad")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrBadRequest), "expected ErrBadRequest, got %v", err)
}

func TestGetBillingDecodesRecent(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/account/billing", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"balance_cents": 12500,
			"balance_usd":   125.0,
			"currency":      "usd",
			"recent": []map[string]any{
				{"id": "cl_1", "type": "purchase", "amount_cents": 5000, "route_id": nil, "upstream_endpoint_id": nil, "created_at": time.Now().UTC().Format(time.RFC3339)},
				{"id": "cl_2", "type": "usage", "amount_cents": -42, "route_id": "rt_xx", "upstream_endpoint_id": "ue_yy", "created_at": time.Now().UTC().Format(time.RFC3339)},
			},
		})
	})
	resp, err := c.GetBilling(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 12500, resp.BalanceCents)
	require.Len(t, resp.Recent, 2)
	require.Equal(t, "purchase", resp.Recent[0].Type)
	require.EqualValues(t, -42, resp.Recent[1].AmountCents)
	require.NotNil(t, resp.Recent[1].RouteID)
	require.Equal(t, "rt_xx", *resp.Recent[1].RouteID)
}

func TestGetBillingHistoryPassesQueryParams(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/account/billing/history", r.URL.Path)
		q := r.URL.Query()
		require.Equal(t, "10", q.Get("limit"))
		require.Equal(t, "2026-05-07T00:00:00Z", q.Get("before"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"balance_cents": 0,
			"rows":          []any{},
			"next_before":   nil,
		})
	})
	_, err := c.GetBillingHistory(context.Background(), 10, "2026-05-07T00:00:00Z")
	require.NoError(t, err)
}

func TestGetBillingHistoryOmitsEmptyParams(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, url.Values{}, r.URL.Query())
		_ = json.NewEncoder(w).Encode(map[string]any{"balance_cents": 0, "rows": []any{}, "next_before": nil})
	})
	_, err := c.GetBillingHistory(context.Background(), 0, "")
	require.NoError(t, err)
}
