// Package config manages the on-disk CLI state at $XDG_CONFIG_HOME/hopnet/config.json.
//
// The file holds the API key, server URLs, and a cache of route tokens that
// were created via this CLI on this host. The control-api only returns the
// route token at creation time, so `hopnet env` and `hopnet run --route` rely
// on this cache to reconstruct proxy credentials after the fact.
//
// File mode is 0600 and the parent directory is 0700. Writes are atomic
// (temp file + rename). Reads tolerate a missing file (returning a zero
// Config) but fail on malformed JSON so the user notices corruption rather
// than silently losing routes.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// CurrentVersion is bumped when the on-disk schema changes in a way
	// that needs migration. v1 covers all of P8.
	CurrentVersion = 1

	// DirMode is the mode for the config directory.
	DirMode = 0o700
	// FileMode is the mode for the config file.
	FileMode = 0o600

	// DefaultBaseURL points at the production control-api. Integration
	// tests override via `auth login --base-url`.
	DefaultBaseURL = "https://api.hopnet.io"
	// DefaultProxyURL points at the production HTTPS-CONNECT proxy.
	// Integration tests override via `auth login --proxy-url`.
	DefaultProxyURL = "https://proxy.hopnet.io:443"

	// PruneCutoff is how long expired routes stick around in the cache
	// after their expires_at. Long enough for a `receipt` lookup after a
	// run, short enough to not accumulate forever.
	PruneCutoff = 24 * time.Hour
)

// Route is the locally-cached subset of a control-api route response. The
// Token field is what makes this file sensitive — it grants proxy access
// for the route's TTL.
type Route struct {
	Token        string    `json:"token"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Label        string    `json:"label,omitempty"`
	RouteClass   string    `json:"route_class"`
	Country      string    `json:"country,omitempty"`
	MaxBytes     *int64    `json:"max_bytes,omitempty"`
	RouteVersion int       `json:"route_version"`
}

// Config is the top-level on-disk shape.
type Config struct {
	Version  int              `json:"version"`
	APIKey   string           `json:"api_key,omitempty"`
	BaseURL  string           `json:"base_url,omitempty"`
	ProxyURL string           `json:"proxy_url,omitempty"`
	Routes   map[string]Route `json:"routes,omitempty"`

	path string `json:"-"`
	mu   sync.Mutex
}

// Path returns the absolute path of the file backing this config.
func (c *Config) Path() string { return c.path }

// DefaultPath resolves the config path: $XDG_CONFIG_HOME/hopnet/config.json
// or, if XDG_CONFIG_HOME is unset, $HOME/.config/hopnet/config.json.
func DefaultPath() (string, error) {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "hopnet", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "hopnet", "config.json"), nil
}

// Load reads the config from path. If the file does not exist a zero Config
// (with default URLs and v1) is returned so first-run callers can immediately
// PutRoute / set APIKey and Save. Mode mismatches log a warning via the
// returned WarnFn but do not fail.
func Load(path string) (*Config, error) {
	c := &Config{
		Version:  CurrentVersion,
		BaseURL:  DefaultBaseURL,
		ProxyURL: DefaultProxyURL,
		Routes:   map[string]Route{},
		path:     path,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if len(data) == 0 {
		return c, nil
	}
	// Reset to zero values before unmarshal so the file's empty fields
	// override defaults (don't silently re-introduce DefaultBaseURL when
	// the user explicitly cleared it).
	c.Version = 0
	c.BaseURL = ""
	c.ProxyURL = ""
	c.Routes = nil
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if c.Routes == nil {
		c.Routes = map[string]Route{}
	}
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.ProxyURL == "" {
		c.ProxyURL = DefaultProxyURL
	}
	return c, nil
}

// LoadDefault loads from DefaultPath().
func LoadDefault() (*Config, error) {
	p, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Load(p)
}

// Save writes the config atomically with mode 0600. Parent dir is created
// with mode 0700 if absent.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		return errors.New("config path is empty")
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// Tighten dir mode in case it pre-existed with looser permissions.
	if err := os.Chmod(dir, DirMode); err != nil && !errors.Is(err, fs.ErrPermission) {
		// Best effort; don't fail Save on a permission tweak we can't make.
		_ = err
	}
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	if c.Routes == nil {
		c.Routes = map[string]Route{}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config.json.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(FileMode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, c.path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// CheckMode returns a human-readable warning if the config file mode is
// looser than 0600. Empty string means OK or file does not exist.
func (c *Config) CheckMode() string {
	st, err := os.Stat(c.path)
	if err != nil {
		return ""
	}
	if st.Mode().Perm() != FileMode {
		return fmt.Sprintf("config %s has mode %o; tightening to 0600 on next write", c.path, st.Mode().Perm())
	}
	return ""
}

// PutRoute inserts/updates a route in the cache.
func (c *Config) PutRoute(id string, r Route) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Routes == nil {
		c.Routes = map[string]Route{}
	}
	c.Routes[id] = r
}

// GetRoute returns a route by ID. ok=false when not present.
func (c *Config) GetRoute(id string) (Route, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.Routes[id]
	return r, ok
}

// DeleteRoute removes a route from the cache. Idempotent.
func (c *Config) DeleteRoute(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Routes, id)
}

// ListRouteIDs returns the cached route IDs sorted by created_at descending.
func (c *Config) ListRouteIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, 0, len(c.Routes))
	for id := range c.Routes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return c.Routes[ids[i]].CreatedAt.After(c.Routes[ids[j]].CreatedAt)
	})
	return ids
}

// Prune drops cache entries whose ExpiresAt is older than now-PruneCutoff.
// Returns the count removed.
func (c *Config) Prune(now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := now.Add(-PruneCutoff)
	removed := 0
	for id, r := range c.Routes {
		if !r.ExpiresAt.IsZero() && r.ExpiresAt.Before(cutoff) {
			delete(c.Routes, id)
			removed++
		}
	}
	return removed
}
