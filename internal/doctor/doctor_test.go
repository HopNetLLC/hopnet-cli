package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/config"
)

// happyConfig writes a valid config at a tmp path and returns a loaded
// *config.Config. The control-api ping and proxy URL get overridden by
// each test as needed.
func happyConfig(t *testing.T, baseURL, proxyURL string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.APIKey = "hn_live_doctorkey1"
	cfg.BaseURL = baseURL
	cfg.ProxyURL = proxyURL
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Reload to refresh mode metadata that Save just set on disk.
	cfg2, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return cfg2
}

func TestRun_AllPass(t *testing.T) {
	// Stub control-api answers /v1/account with a real-looking row.
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                     "acct_123",
			"email":                  "doctor@example.com",
			"status":                 "active",
			"balance_cents":          1234,
			"allow_negative_balance": false,
		})
	}))
	defer apiSrv.Close()

	// Listen on a free localhost port for the proxy dial check.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	proxyURL := "https://" + ln.Addr().String()

	cfg := happyConfig(t, apiSrv.URL, proxyURL)
	results := Run(context.Background(), Options{
		Config: cfg,
		AccountClientFor: func(c *config.Config) AccountClient {
			return client.New(c.BaseURL, c.APIKey)
		},
		ProxyDialTimeout: 2 * time.Second,
	})

	if !AllOK(results) {
		for _, r := range results {
			t.Logf("  [%s] %s — %s", r.Status, r.Name, r.Detail)
		}
		t.Fatalf("expected all-OK, got %d results not OK", countNonOK(results))
	}
	if got := names(results); strings.Join(got, ",") != "config,api-key,control-api,proxy" {
		t.Fatalf("result order/names = %v", got)
	}
	// balance rendering should not garble cents.
	for _, r := range results {
		if r.Name == "control-api" && !strings.Contains(r.Detail, "$12.34") {
			t.Fatalf("control-api detail did not render balance correctly: %q", r.Detail)
		}
	}
}

func TestRun_FreshInstall(t *testing.T) {
	// Realistic "user just installed, never logged in" state: config file
	// doesn't exist, Load returns a zero Config with defaults. Doctor
	// should fail the config check (file missing), fail the api-key
	// check (empty), skip control-api (depends on key), and fail proxy
	// (dial against the stubbed endpoint).
	dir := t.TempDir()
	path := filepath.Join(dir, "no-such-file.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Force proxy_url at a closed local port so the dial is fast and
	// doesn't escape the test host.
	cfg.ProxyURL = "http://127.0.0.1:1"

	dialCalled := false
	results := Run(context.Background(), Options{
		Config: cfg,
		AccountClientFor: func(c *config.Config) AccountClient {
			return errClient{err: errors.New("should not be called when no key")}
		},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialCalled = true
			return nil, errors.New("dial refused")
		},
		ProxyDialTimeout: 500 * time.Millisecond,
	})

	want := map[string]Status{
		"config":      StatusFail, // file missing
		"api-key":     StatusFail, // empty
		"control-api": StatusSkip, // no key
		"proxy":       StatusFail, // dial stub returns error
	}
	for _, r := range results {
		if r.Status != want[r.Name] {
			t.Errorf("%s: status %s, want %s (detail: %s)", r.Name, r.Status, want[r.Name], r.Detail)
		}
	}
	if !dialCalled {
		t.Error("proxy dial should have been attempted")
	}
}

func TestRun_BadAPIKeyFormat(t *testing.T) {
	cfg := happyConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:2")
	cfg.APIKey = "wrong_prefix"
	results := Run(context.Background(), Options{Config: cfg})
	for _, r := range results {
		if r.Name == "api-key" {
			if r.Status != StatusFail {
				t.Errorf("expected api-key fail; got %s", r.Status)
			}
			if !strings.Contains(r.Detail, "hn_live_") {
				t.Errorf("expected detail to mention required prefix; got %q", r.Detail)
			}
			return
		}
	}
	t.Fatal("api-key result not found")
}

func TestRun_ControlAPIDown(t *testing.T) {
	// Point base URL at port 1 (always refused). Proxy URL also dead so
	// we don't accidentally hit something else; that check failing
	// independently is expected.
	cfg := happyConfig(t, "http://127.0.0.1:1", "http://127.0.0.1:2")
	results := Run(context.Background(), Options{Config: cfg, ProxyDialTimeout: 500 * time.Millisecond})
	got := map[string]Status{}
	for _, r := range results {
		got[r.Name] = r.Status
	}
	if got["control-api"] != StatusFail {
		t.Errorf("expected control-api fail; got %s", got["control-api"])
	}
	if got["proxy"] != StatusFail {
		t.Errorf("expected proxy fail; got %s", got["proxy"])
	}
}

func TestRun_ProxyMissingPort_HTTPSDefaults(t *testing.T) {
	// Listen on 443? Not permitted as non-root. Use a sentinel URL we
	// know will fail, but verify the parse path falls through to default
	// port logic without erroring before the dial.
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "x", "email": "x@x", "status": "active", "balance_cents": 0})
	}))
	defer apiSrv.Close()
	cfg := happyConfig(t, apiSrv.URL, "https://127.0.0.1") // no port → defaults to 443
	called := false
	results := Run(context.Background(), Options{
		Config: cfg,
		AccountClientFor: func(c *config.Config) AccountClient {
			return client.New(c.BaseURL, c.APIKey)
		},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			called = true
			// Verify the port came out as 443.
			_, port, _ := net.SplitHostPort(address)
			if port != "443" {
				t.Errorf("expected default port 443, got %q", port)
			}
			return nil, errors.New("dial intentionally refused")
		},
	})
	if !called {
		t.Fatal("proxy dial was never attempted")
	}
	for _, r := range results {
		if r.Name == "proxy" && r.Status != StatusFail {
			t.Errorf("expected proxy fail (dial returned error); got %s", r.Status)
		}
	}
}

func TestFormatBalance(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "$0.00"},
		{1234, "$12.34"},
		{100, "$1.00"},
		// Codex r1 P3 regression: -50 cents must render with a minus
		// sign. Plain `cents/100` truncates toward zero for small
		// negatives, dropping the sign.
		{-50, "-$0.50"},
		{-1, "-$0.01"},
		{-1234, "-$12.34"},
	}
	for _, tc := range cases {
		if got := formatBalance(tc.cents); got != tc.want {
			t.Errorf("formatBalance(%d) = %q, want %q", tc.cents, got, tc.want)
		}
	}
}

func TestAllOK(t *testing.T) {
	if !AllOK([]Result{{Status: StatusOK}, {Status: StatusOK}}) {
		t.Fatal("expected true for all OK")
	}
	if AllOK([]Result{{Status: StatusOK}, {Status: StatusFail}}) {
		t.Fatal("expected false when any fail")
	}
	if AllOK([]Result{{Status: StatusOK}, {Status: StatusSkip}}) {
		t.Fatal("skip should not count as OK")
	}
}

// errClient implements AccountClient and always returns the configured error.
type errClient struct{ err error }

func (e errClient) GetAccount(ctx context.Context) (*client.Account, error) {
	return nil, e.err
}

// helpers
func names(results []Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Name
	}
	return out
}

func countNonOK(results []Result) int {
	n := 0
	for _, r := range results {
		if r.Status != StatusOK {
			n++
		}
	}
	return n
}

// Sanity: keep a build-time check that *client.Client implements the
// interface we hand into Options.AccountClientFor — guards against a
// future client.go refactor breaking the doctor.
var _ AccountClient = (*client.Client)(nil)

// Sanity: net.Dialer.DialContext should satisfy DialFunc.
var _ DialFunc = (&net.Dialer{}).DialContext

// Sanity: url.Parse with an empty scheme is the boundary the proxy check
// guards. This isn't a runtime test, just a compile-time reminder for
// the maintainer that scheme presence matters.
var _ = url.Parse
