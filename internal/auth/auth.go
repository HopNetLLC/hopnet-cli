// Package auth implements `hopnet auth login`. The flow is:
//
//  1. Resolve the API key from --api-key, then stdin (when piped), then
//     an interactive prompt with no echo (when stdin is a TTY).
//  2. Validate the format: prefix `hn_live_` and length >= 16, matching
//     the server-side check in apps/control-api/src/auth.ts.
//  3. Optionally ping GET /v1/account to verify the key works against the
//     configured base URL. Skipped only with --skip-verify (used by tests).
//  4. Persist {api_key, base_url, proxy_url} to the config file.
//
// On any failure before persistence, the existing config is left untouched.
package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/config"
)

// KeyPrefix is the required leading segment of every API key.
const KeyPrefix = "hn_live_"

// MinKeyLength is the minimum total length of a valid API key (mirrors
// apps/control-api/src/auth.ts).
const MinKeyLength = 16

// ErrNoKey is returned by ResolveKey when no key is provided and stdin
// is not interactive (so we can't prompt) and not piped (so there's
// nothing to read).
var ErrNoKey = errors.New("no api key supplied (use --api-key, pipe via stdin, or run interactively)")

// ResolveKey returns the API key the user wants to use, given the flag
// value and stdin. flagKey wins; otherwise reads one line from stdin
// (whether piped or interactive — caller's responsibility to use a
// no-echo reader for TTYs). Empty result returns ErrNoKey.
func ResolveKey(flagKey string, stdin io.Reader) (string, error) {
	if flagKey != "" {
		return strings.TrimSpace(flagKey), nil
	}
	if stdin == nil {
		return "", ErrNoKey
	}
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return "", ErrNoKey
	}
	key := strings.TrimSpace(scanner.Text())
	if key == "" {
		return "", ErrNoKey
	}
	return key, nil
}

// ValidateFormat checks the key matches the server-side format rule. It
// rejects obvious typos before we hit the network.
func ValidateFormat(key string) error {
	if !strings.HasPrefix(key, KeyPrefix) {
		return fmt.Errorf("api key must start with %q", KeyPrefix)
	}
	if len(key) < MinKeyLength {
		return fmt.Errorf("api key must be at least %d chars (got %d)", MinKeyLength, len(key))
	}
	return nil
}

// LoginOptions controls the Login flow.
type LoginOptions struct {
	APIKey     string
	BaseURL    string
	ProxyURL   string
	SkipVerify bool // skip the /v1/account ping
}

// Login persists the supplied options into cfg after validating the key
// format and (unless SkipVerify) pinging /v1/account. cfg is mutated
// in-place and saved.
func Login(ctx context.Context, cfg *config.Config, opts LoginOptions) (*client.Account, error) {
	if err := ValidateFormat(opts.APIKey); err != nil {
		return nil, err
	}
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}
	proxyURL := opts.ProxyURL
	if proxyURL == "" {
		proxyURL = cfg.ProxyURL
	}

	var account *client.Account
	if !opts.SkipVerify {
		c := client.New(baseURL, opts.APIKey)
		var err error
		account, err = c.GetAccount(ctx)
		if err != nil {
			return nil, fmt.Errorf("verify api key against %s: %w", baseURL, err)
		}
	}

	cfg.APIKey = opts.APIKey
	cfg.BaseURL = baseURL
	cfg.ProxyURL = proxyURL
	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}
	return account, nil
}
