package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadMissingFileReturnsZeroConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	c, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, CurrentVersion, c.Version)
	require.Equal(t, DefaultBaseURL, c.BaseURL)
	require.Equal(t, DefaultProxyURL, c.ProxyURL)
	require.Empty(t, c.APIKey)
	require.Empty(t, c.Routes)
}

func TestSaveRoundTripPreservesFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c, err := Load(path)
	require.NoError(t, err)

	c.APIKey = "hn_live_abcdefghij"
	c.BaseURL = "http://127.0.0.1:8080"
	c.ProxyURL = "https://127.0.0.1:8443"
	max := int64(1024 * 1024)
	c.PutRoute("rt_test123", Route{
		Token:        "rtk_test123",
		CreatedAt:    time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		ExpiresAt:    time.Date(2026, 5, 7, 12, 15, 0, 0, time.UTC),
		Label:        "smoke",
		RouteClass:   "direct",
		Country:      "US",
		MaxBytes:     &max,
		RouteVersion: 1,
	})
	require.NoError(t, c.Save())

	c2, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "hn_live_abcdefghij", c2.APIKey)
	require.Equal(t, "http://127.0.0.1:8080", c2.BaseURL)
	require.Equal(t, "https://127.0.0.1:8443", c2.ProxyURL)
	r, ok := c2.GetRoute("rt_test123")
	require.True(t, ok)
	require.Equal(t, "rtk_test123", r.Token)
	require.Equal(t, "direct", r.RouteClass)
	require.NotNil(t, r.MaxBytes)
	require.EqualValues(t, 1024*1024, *r.MaxBytes)
}

func TestSaveEnforcesMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c, err := Load(path)
	require.NoError(t, err)
	c.APIKey = "hn_live_abcdefghij"
	require.NoError(t, c.Save())

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(FileMode), st.Mode().Perm(), "config file should be chmod 600")

	dirSt, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(DirMode), dirSt.Mode().Perm(), "config dir should be chmod 700")
}

func TestSaveTightensExistingLooseMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":1}`), 0o644))

	c, err := Load(path)
	require.NoError(t, err)
	require.Contains(t, c.CheckMode(), "tightening to 0600")

	c.APIKey = "hn_live_abcdefghij"
	require.NoError(t, c.Save())

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(FileMode), st.Mode().Perm())
	require.Empty(t, c.CheckMode(), "after save, mode is clean")
}

func TestLoadMalformedJSONFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse config")
}

func TestPruneDropsExpiredOldEntries(t *testing.T) {
	c := &Config{Routes: map[string]Route{}}
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	c.Routes["rt_active"] = Route{ExpiresAt: now.Add(15 * time.Minute)}
	c.Routes["rt_recent_expired"] = Route{ExpiresAt: now.Add(-1 * time.Hour)}
	c.Routes["rt_old_expired"] = Route{ExpiresAt: now.Add(-48 * time.Hour)}
	c.Routes["rt_no_expiry"] = Route{}

	removed := c.Prune(now)
	require.Equal(t, 1, removed)
	_, hasActive := c.GetRoute("rt_active")
	_, hasRecent := c.GetRoute("rt_recent_expired")
	_, hasOld := c.GetRoute("rt_old_expired")
	_, hasNoExpiry := c.GetRoute("rt_no_expiry")
	require.True(t, hasActive, "still-active route stays")
	require.True(t, hasRecent, "recently-expired route still in window")
	require.False(t, hasOld, "long-expired route gone")
	require.True(t, hasNoExpiry, "zero-time entries are kept (best-effort)")
}

func TestPutGetDeleteRoute(t *testing.T) {
	c := &Config{Routes: map[string]Route{}}
	c.PutRoute("rt_a", Route{Token: "rtk_a"})
	r, ok := c.GetRoute("rt_a")
	require.True(t, ok)
	require.Equal(t, "rtk_a", r.Token)

	c.DeleteRoute("rt_a")
	_, ok = c.GetRoute("rt_a")
	require.False(t, ok)

	c.DeleteRoute("rt_does_not_exist")
}

func TestListRouteIDsSortedDescByCreated(t *testing.T) {
	c := &Config{Routes: map[string]Route{}}
	t0 := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	c.PutRoute("rt_old", Route{CreatedAt: t0})
	c.PutRoute("rt_mid", Route{CreatedAt: t0.Add(1 * time.Hour)})
	c.PutRoute("rt_new", Route{CreatedAt: t0.Add(2 * time.Hour)})
	ids := c.ListRouteIDs()
	require.Equal(t, []string{"rt_new", "rt_mid", "rt_old"}, ids)
}

func TestSaveAtomicWriteNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c, err := Load(path)
	require.NoError(t, err)
	c.APIKey = "hn_live_abcdefghij"
	require.NoError(t, c.Save())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), ".config.json."),
			"temp file %s should have been renamed away", e.Name())
	}
}

func TestSavedJSONShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c, err := Load(path)
	require.NoError(t, err)
	c.APIKey = "hn_live_xxxx0000"
	c.PutRoute("rt_x", Route{Token: "rtk_x", RouteClass: "direct", RouteVersion: 1})
	require.NoError(t, c.Save())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	require.EqualValues(t, 1, raw["version"])
	require.Equal(t, "hn_live_xxxx0000", raw["api_key"])
	require.Contains(t, raw, "routes")
}
