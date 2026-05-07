package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/config"
	"github.com/stretchr/testify/require"
)

// fakeServer mocks just enough of the control-api surface to drive
// run-package tests.
type fakeServer struct {
	t                 *testing.T
	srv               *httptest.Server
	createCalls       int
	deleteCalls       int
	usageCalls        int
	deleteShouldFail  bool
	createReturnsRoute *client.CreateRouteResponse
	usageReturns       *client.Usage
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{t: t}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/routes":
			f.createCalls++
			resp := f.createReturnsRoute
			if resp == nil {
				resp = &client.CreateRouteResponse{
					ID: "rt_fake1", Token: "rtk_fake1", RouteClass: "direct",
					ExpiresAt: time.Now().Add(2 * time.Minute).UTC(), RouteVersion: 1,
				}
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/routes/"):
			f.deleteCalls++
			if f.deleteShouldFail {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"db_down"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/usage"):
			f.usageCalls++
			u := f.usageReturns
			if u == nil {
				u = &client.Usage{ID: "rt_fake1", Status: "revoked", Bytes: 1234}
			}
			_ = json.NewEncoder(w).Encode(u)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func newCfg(t *testing.T, proxyURL string) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.json"))
	require.NoError(t, err)
	cfg.ProxyURL = proxyURL
	cfg.APIKey = "hn_live_xxxxxxxx"
	return cfg
}

func TestRunCreatesRouteExecsChildAndRevokes(t *testing.T) {
	srv := newFakeServer(t)
	cfg := newCfg(t, "https://proxy.example:443")
	c := client.New(srv.srv.URL, "hn_live_xxxxxxxx")

	var out, errBuf bytes.Buffer
	res, err := Run(context.Background(), c, cfg, Options{
		CreateRequest: &client.CreateRouteRequest{TTLSeconds: 120, RouteClass: "direct", ClientKind: "cli"},
		Argv:          []string{"/usr/bin/env"},
		Stdout:        &out,
		Stderr:        &errBuf,
		Env:           []string{"PATH=/usr/bin:/bin"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	require.True(t, res.Created)
	require.Equal(t, "rt_fake1", res.RouteID)
	require.Equal(t, 1, srv.createCalls)
	require.Equal(t, 1, srv.deleteCalls, "self-created route is revoked")
	require.Equal(t, 1, srv.usageCalls, "receipt is fetched")

	// Child saw the proxy env.
	stdout := out.String()
	require.Contains(t, stdout, "HTTPS_PROXY=https://rt_fake1:rtk_fake1@proxy.example:443")
	require.Contains(t, stdout, "HOPNET_ROUTE_ID=rt_fake1")
	require.Contains(t, stdout, "NO_PROXY=localhost,127.0.0.1,::1")

	// Local cache cleared after revoke.
	_, ok := cfg.GetRoute("rt_fake1")
	require.False(t, ok)
}

func TestRunReusesExistingRouteAndDoesNotRevoke(t *testing.T) {
	srv := newFakeServer(t)
	cfg := newCfg(t, "https://proxy.example:443")
	cfg.PutRoute("rt_keep1", config.Route{
		Token: "rtk_keep1", RouteClass: "direct", RouteVersion: 1,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(5 * time.Minute).UTC(),
	})
	c := client.New(srv.srv.URL, "hn_live_xxxxxxxx")

	res, err := Run(context.Background(), c, cfg, Options{
		RouteID: "rt_keep1",
		Argv:    []string{"/usr/bin/true"},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Env:     []string{"PATH=/usr/bin:/bin"},
	})
	require.NoError(t, err)
	require.False(t, res.Created)
	require.Equal(t, 0, srv.createCalls)
	require.Equal(t, 0, srv.deleteCalls, "caller-supplied route is never auto-revoked")
	require.Equal(t, 1, srv.usageCalls, "receipt still fetched")
	_, ok := cfg.GetRoute("rt_keep1")
	require.True(t, ok, "caller-supplied route stays in cache")
}

func TestRunKeepRouteFlagSkipsRevoke(t *testing.T) {
	srv := newFakeServer(t)
	cfg := newCfg(t, "https://proxy.example:443")
	c := client.New(srv.srv.URL, "hn_live_xxxxxxxx")

	res, err := Run(context.Background(), c, cfg, Options{
		CreateRequest: &client.CreateRouteRequest{TTLSeconds: 60, RouteClass: "direct", ClientKind: "cli"},
		Argv:          []string{"/usr/bin/true"},
		KeepRoute:     true,
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
		Env:           []string{"PATH=/usr/bin:/bin"},
	})
	require.NoError(t, err)
	require.True(t, res.Created)
	require.Equal(t, 0, srv.deleteCalls, "--keep-route suppresses revoke")
	_, ok := cfg.GetRoute(res.RouteID)
	require.True(t, ok, "kept route stays in cache for later use")
}

func TestRunPropagatesNonZeroExitCode(t *testing.T) {
	srv := newFakeServer(t)
	cfg := newCfg(t, "https://proxy.example:443")
	c := client.New(srv.srv.URL, "hn_live_xxxxxxxx")

	res, err := Run(context.Background(), c, cfg, Options{
		CreateRequest: &client.CreateRouteRequest{TTLSeconds: 60, RouteClass: "direct", ClientKind: "cli"},
		Argv:          []string{"/usr/bin/false"},
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
		Env:           []string{"PATH=/usr/bin:/bin"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.ExitCode, "false(1) returns exit 1")
}

func TestRunRevokeFailureDoesNotChangeExitCode(t *testing.T) {
	srv := newFakeServer(t)
	srv.deleteShouldFail = true
	cfg := newCfg(t, "https://proxy.example:443")
	c := client.New(srv.srv.URL, "hn_live_xxxxxxxx")

	res, err := Run(context.Background(), c, cfg, Options{
		CreateRequest: &client.CreateRouteRequest{TTLSeconds: 60, RouteClass: "direct", ClientKind: "cli"},
		Argv:          []string{"/usr/bin/true"},
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
		Env:           []string{"PATH=/usr/bin:/bin"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode, "child success is preserved despite revoke failure")
	require.Error(t, res.RevokeErr)
}

func TestRunCreateRouteFailurePreventsExec(t *testing.T) {
	srvHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "insufficient_credit", "balance_cents": -50})
	}))
	defer srvHTTP.Close()
	cfg := newCfg(t, "https://proxy.example:443")
	c := client.New(srvHTTP.URL, "hn_live_xxxxxxxx")

	res, err := Run(context.Background(), c, cfg, Options{
		CreateRequest: &client.CreateRouteRequest{TTLSeconds: 60, RouteClass: "direct", ClientKind: "cli"},
		Argv:          []string{"/usr/bin/true"},
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
		Env:           []string{"PATH=/usr/bin:/bin"},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, client.ErrInsufficientCredit), "expected ErrInsufficientCredit, got %v", err)
	require.Equal(t, -1, res.ExitCode, "exec did not happen")
}

func TestRunRouteIDNotInCacheReturnsError(t *testing.T) {
	srv := newFakeServer(t)
	cfg := newCfg(t, "https://proxy.example:443")
	c := client.New(srv.srv.URL, "hn_live_xxxxxxxx")

	_, err := Run(context.Background(), c, cfg, Options{
		RouteID: "rt_unknown",
		Argv:    []string{"/usr/bin/true"},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Env:     []string{"PATH=/usr/bin:/bin"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rt_unknown")
}

func TestProxyURLForRouteEscapesUserInfo(t *testing.T) {
	got, err := proxyURLForRoute("https://proxy.hopnet.io:443", "rt_xx", "rtk_yy")
	require.NoError(t, err)
	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, "rt_xx", u.User.Username())
	pw, hasPW := u.User.Password()
	require.True(t, hasPW)
	require.Equal(t, "rtk_yy", pw)
	require.Equal(t, "proxy.hopnet.io:443", u.Host)
}

func TestRunRevokesSelfCreatedRouteOnPreExecError(t *testing.T) {
	srv := newFakeServer(t)
	cfg := newCfg(t, "://invalid-proxy-url")
	c := client.New(srv.srv.URL, "hn_live_xxxxxxxx")

	res, err := Run(context.Background(), c, cfg, Options{
		CreateRequest: &client.CreateRouteRequest{TTLSeconds: 60, RouteClass: "direct", ClientKind: "cli"},
		Argv:          []string{"/usr/bin/true"},
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
		Env:           []string{"PATH=/usr/bin:/bin"},
	})
	require.Error(t, err, "proxyURLForRoute should fail on invalid scheme")
	require.True(t, res.Created, "route was created before the error")
	require.Equal(t, 1, srv.createCalls)
	require.Equal(t, 1, srv.deleteCalls,
		"self-created route must be revoked when exec never starts")
	_, ok := cfg.GetRoute(res.RouteID)
	require.False(t, ok, "route must be removed from local cache too")
}

func TestRunPreExecErrorPreservedWhenKeepRouteSet(t *testing.T) {
	srv := newFakeServer(t)
	cfg := newCfg(t, "://invalid-proxy-url")
	c := client.New(srv.srv.URL, "hn_live_xxxxxxxx")

	_, err := Run(context.Background(), c, cfg, Options{
		CreateRequest: &client.CreateRouteRequest{TTLSeconds: 60, RouteClass: "direct", ClientKind: "cli"},
		Argv:          []string{"/usr/bin/true"},
		KeepRoute:     true,
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
		Env:           []string{"PATH=/usr/bin:/bin"},
	})
	require.Error(t, err)
	require.Equal(t, 0, srv.deleteCalls,
		"--keep-route opts out of revoke even on pre-exec error")
}

func TestBuildEnvDropsCallerProxyVars(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin:/bin",
		"HTTPS_PROXY=http://existing.example",
		"http_proxy=http://existing.example",
		"FOO=bar",
	}
	got := buildEnv(parent, "https://rt:tk@px.example", "rt", "tk")
	joined := strings.Join(got, "\x00")
	require.NotContains(t, joined, "http://existing.example", "pre-existing proxy env must be stripped")
	require.Contains(t, joined, "FOO=bar", "unrelated env preserved")
	require.Contains(t, joined, "HTTPS_PROXY=https://rt:tk@px.example")
	require.Contains(t, joined, "HOPNET_ROUTE_ID=rt")
}
