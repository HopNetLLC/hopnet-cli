package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(srv.URL, "hn_live_testtoken1")
	c.HTTP.Timeout = 5 * time.Second
	return c, srv
}

func TestGetAccountSendsBearerAndDecodes(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer hn_live_testtoken1", r.Header.Get("Authorization"))
		require.Equal(t, "/v1/account", r.URL.Path)
		require.Equal(t, http.MethodGet, r.Method)
		_ = json.NewEncoder(w).Encode(Account{ID: "acc_1", Email: "a@b.c", Status: "active", BalanceCents: 5000})
	})
	a, err := c.GetAccount(context.Background())
	require.NoError(t, err)
	require.Equal(t, "acc_1", a.ID)
	require.EqualValues(t, 5000, a.BalanceCents)
}

func TestCreateRoutePostsJSONReturnsToken(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/routes", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		var got CreateRouteRequest
		require.NoError(t, json.Unmarshal(body, &got))
		require.Equal(t, 120, got.TTLSeconds)
		require.Equal(t, "direct", got.RouteClass)
		require.Equal(t, []string{"example.com"}, got.Allow)

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":            "rt_xxxx",
			"token":         "rtk_yyyy",
			"route_class":   "direct",
			"expires_at":    time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339),
			"max_bytes":     int64(10 * 1024 * 1024),
			"route_version": 1,
		})
	})
	max := 10
	resp, err := c.CreateRoute(context.Background(), &CreateRouteRequest{
		TTLSeconds: 120, MaxMB: &max, RouteClass: "direct", ClientKind: "cli", Allow: []string{"example.com"},
	})
	require.NoError(t, err)
	require.Equal(t, "rt_xxxx", resp.ID)
	require.Equal(t, "rtk_yyyy", resp.Token)
}

func TestCreateRouteInsufficientCredit(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "insufficient_credit", "balance_cents": -100})
	})
	_, err := c.CreateRoute(context.Background(), &CreateRouteRequest{TTLSeconds: 60, RouteClass: "direct", ClientKind: "cli"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInsufficientCredit))
	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, "insufficient_credit", apiErr.Code)
	require.NotNil(t, apiErr.BalanceCents)
	require.EqualValues(t, -100, *apiErr.BalanceCents)
}

func TestUnauthorizedMapsToErrUnauthorized(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_key"})
	})
	_, err := c.GetAccount(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnauthorized))
}

func TestDeleteRouteAcceptsNoContent(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/v1/routes/rt_x", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})
	require.NoError(t, c.DeleteRoute(context.Background(), "rt_x"))
}

func TestDeleteRouteNotFoundMapsToErrNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found_or_already_terminal"})
	})
	err := c.DeleteRoute(context.Background(), "rt_missing")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNotFound))
}

func TestServerErrorMapsToErrServer(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("kaboom"))
	})
	_, err := c.GetAccount(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrServer))
	require.Contains(t, err.Error(), "500")
}

func TestListRoutesRoundTrip(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/routes", r.URL.Path)
		_ = json.NewEncoder(w).Encode(ListRoutesResponse{
			Routes: []Route{{ID: "rt_a", Status: "active", RouteClass: "direct", CreatedAt: time.Now().UTC()}},
		})
	})
	resp, err := c.ListRoutes(context.Background())
	require.NoError(t, err)
	require.Len(t, resp.Routes, 1)
	require.Equal(t, "rt_a", resp.Routes[0].ID)
}

func TestGetUsageDecodesDestinations(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/routes/rt_a/usage", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                       "rt_a",
			"status":                   "active",
			"bytes":                    1234,
			"bytes_client_to_upstream": 1000,
			"bytes_upstream_to_client": 234,
			"estimated_cost_cents":     1,
			"observed_avg_mbps":        nil,
			"last_flushed_at":          nil,
			"created_at":               time.Now().UTC().Format(time.RFC3339),
			"expires_at":               nil,
			"destinations": []map[string]any{
				{"host": "example.com", "port": 443, "tunnels": 1, "bytes": 1234},
			},
		})
	})
	u, err := c.GetUsage(context.Background(), "rt_a")
	require.NoError(t, err)
	require.EqualValues(t, 1234, u.Bytes)
	require.Len(t, u.Destinations, 1)
	require.Equal(t, "example.com", u.Destinations[0].Host)
}

func TestBaseURLTrailingSlashTrimmed(t *testing.T) {
	c := New("https://api.example.com/", "")
	require.Equal(t, "https://api.example.com", c.BaseURL)
}

func TestErrorMessageFallsBackToBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream gone"))
	})
	_, err := c.GetAccount(context.Background())
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "502"))
}
