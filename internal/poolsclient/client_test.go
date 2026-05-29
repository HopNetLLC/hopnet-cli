package poolsclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNew_HonorsEnvOverride(t *testing.T) {
	t.Setenv("HOPNET_API_BASE_URL", "http://127.0.0.1:8080")
	c := New()
	if c.BaseURL != "http://127.0.0.1:8080" {
		t.Errorf("expected env override; got %q", c.BaseURL)
	}
}

func TestNew_FallsBackToDefault(t *testing.T) {
	_ = os.Unsetenv("HOPNET_API_BASE_URL")
	c := New()
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("expected default; got %q", c.BaseURL)
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	t.Setenv("HOPNET_API_BASE_URL", "https://api.example.com/")
	c := New()
	if c.BaseURL != "https://api.example.com" {
		t.Errorf("expected trimmed; got %q", c.BaseURL)
	}
}

func TestListPools_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pools" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(PoolsResponse{
			Pools: []PoolEntry{
				{
					RouteClass:      "residential",
					Countries:       []string{"GB", "US"},
					StickyCountries: []string{"US"},
					Status:          "available",
				},
			},
			GeneratedAt: "2026-05-29T00:00:00Z",
		})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	resp, err := c.ListPools(context.Background())
	if err != nil {
		t.Fatalf("ListPools: %v", err)
	}
	if len(resp.Pools) != 1 || resp.Pools[0].RouteClass != "residential" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestListPools_PropagatesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.ListPools(context.Background()); err == nil {
		t.Error("expected error on 500")
	}
}
