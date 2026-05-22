// Package doctor runs the diagnostics that back `hopnet doctor`. Each
// check returns a Result; the caller renders all results and aggregates
// the overall pass/fail.
package doctor

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/HopNetLLC/hopnet-cli/internal/auth"
	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/config"
)

// Status is the outcome of a single check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Result is one row in the doctor output table.
type Result struct {
	Name   string
	Status Status
	Detail string // human-readable single-line explanation
}

// AllOK reports whether every result is StatusOK.
func AllOK(results []Result) bool {
	for _, r := range results {
		if r.Status != StatusOK {
			return false
		}
	}
	return true
}

// AccountClient is the subset of *client.Client the doctor needs.
// Tests pass a fake to avoid spinning up an httptest server when they
// just want to assert formatting behavior.
type AccountClient interface {
	GetAccount(ctx context.Context) (*client.Account, error)
}

// DialFunc opens a TCP connection. Signature matches net.Dialer.DialContext
// so the production default is a direct method-value assignment. Tests
// inject a fake to avoid binding real sockets during unit testing.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Options injects the cfg + transport seams. Production callers leave
// AccountClientFor and Dial nil; tests fill them in.
type Options struct {
	Config *config.Config

	// AccountClientFor constructs the API client for the control-api check.
	// If nil, the default is client.New(cfg.BaseURL, cfg.APIKey).
	AccountClientFor func(cfg *config.Config) AccountClient

	// Dial defaults to net.Dialer{Timeout: 3s}.DialContext.
	Dial DialFunc

	// ProxyDialTimeout is the per-check timeout for the proxy TCP dial.
	// Default 3s.
	ProxyDialTimeout time.Duration

	// Now is the clock; tests override to make deadlines deterministic.
	Now func() time.Time
}

// Run executes every check in order and returns the result rows.
func Run(ctx context.Context, opts Options) []Result {
	if opts.ProxyDialTimeout == 0 {
		opts.ProxyDialTimeout = 3 * time.Second
	}
	if opts.Dial == nil {
		opts.Dial = (&net.Dialer{Timeout: opts.ProxyDialTimeout}).DialContext
	}
	if opts.AccountClientFor == nil {
		opts.AccountClientFor = func(cfg *config.Config) AccountClient {
			return client.New(cfg.BaseURL, cfg.APIKey)
		}
	}

	cfg := opts.Config
	var results []Result

	// 1. config
	results = append(results, checkConfig(cfg))

	// 2. api-key (depends on the config check having loaded cfg; we accept
	// a nil cfg in case of catastrophic loader failure)
	if cfg == nil {
		results = append(results, Result{Name: "api-key", Status: StatusSkip, Detail: "config unreadable"})
	} else {
		results = append(results, checkAPIKey(cfg))
	}

	// 3. control-api ping — skip if no key, since /v1/account requires auth
	if cfg == nil || cfg.APIKey == "" {
		results = append(results, Result{Name: "control-api", Status: StatusSkip, Detail: "no api key configured"})
	} else {
		results = append(results, checkControlAPI(ctx, cfg, opts.AccountClientFor))
	}

	// 4. proxy TCP dial — independent of api-key
	if cfg == nil || cfg.ProxyURL == "" {
		results = append(results, Result{Name: "proxy", Status: StatusSkip, Detail: "no proxy_url configured"})
	} else {
		results = append(results, checkProxy(ctx, cfg.ProxyURL, opts.Dial, opts.ProxyDialTimeout))
	}

	return results
}

func checkConfig(cfg *config.Config) Result {
	if cfg == nil {
		return Result{Name: "config", Status: StatusFail, Detail: "could not load config"}
	}
	p := cfg.Path()
	st, err := os.Stat(p)
	if err != nil {
		// File doesn't exist yet (fresh install, never logged in) is a
		// known state worth surfacing as a fail with a clear next-step
		// hint — the rest of the checks will skip cleanly.
		if os.IsNotExist(err) {
			return Result{
				Name:   "config",
				Status: StatusFail,
				Detail: fmt.Sprintf("not found at %s — run `hopnet auth login` first", p),
			}
		}
		return Result{Name: "config", Status: StatusFail, Detail: fmt.Sprintf("stat %s: %v", p, err)}
	}
	mode := st.Mode().Perm()
	if mode != config.FileMode {
		return Result{
			Name:   "config",
			Status: StatusFail,
			Detail: fmt.Sprintf("mode %o at %s (want %o)", mode, p, config.FileMode),
		}
	}
	return Result{Name: "config", Status: StatusOK, Detail: fmt.Sprintf("%s mode 0600", p)}
}

func checkAPIKey(cfg *config.Config) Result {
	if cfg.APIKey == "" {
		return Result{Name: "api-key", Status: StatusFail, Detail: "not set — run `hopnet auth login`"}
	}
	if err := auth.ValidateFormat(cfg.APIKey); err != nil {
		// auth.ValidateFormat returns errors describing the prefix +
		// length requirements; never the key value itself, so it's safe
		// to surface as-is.
		return Result{Name: "api-key", Status: StatusFail, Detail: err.Error()}
	}
	return Result{Name: "api-key", Status: StatusOK, Detail: "format valid"}
}

func checkControlAPI(ctx context.Context, cfg *config.Config, mkClient func(*config.Config) AccountClient) Result {
	api := mkClient(cfg)
	account, err := api.GetAccount(ctx)
	if err != nil {
		return Result{
			Name:   "control-api",
			Status: StatusFail,
			Detail: fmt.Sprintf("GET %s/v1/account: %v", cfg.BaseURL, err),
		}
	}
	// Surface email + balance so doctor doubles as a "who am I?" check.
	return Result{
		Name:   "control-api",
		Status: StatusOK,
		Detail: fmt.Sprintf("%s · %s · balance $%d.%02d", cfg.BaseURL, account.Email,
			account.BalanceCents/100, absInt64(account.BalanceCents)%100),
	}
}

func checkProxy(ctx context.Context, proxyURL string, dial DialFunc, timeout time.Duration) Result {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return Result{Name: "proxy", Status: StatusFail, Detail: fmt.Sprintf("parse proxy_url: %v", err)}
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		// Default 443 for https schemes (production proxy.hopnet.io:443
		// always has an explicit port today, but be defensive).
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			return Result{Name: "proxy", Status: StatusFail, Detail: fmt.Sprintf("no port in proxy_url %q", proxyURL)}
		}
	}
	address := net.JoinHostPort(host, port)

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dial(dialCtx, "tcp", address)
	if err != nil {
		return Result{Name: "proxy", Status: StatusFail, Detail: fmt.Sprintf("tcp dial %s: %v", address, err)}
	}
	_ = conn.Close()
	return Result{Name: "proxy", Status: StatusOK, Detail: fmt.Sprintf("tcp %s reachable", address)}
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
