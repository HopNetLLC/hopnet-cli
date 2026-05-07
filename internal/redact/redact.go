// Package redact provides a slog ReplaceAttr function that masks sensitive
// fields (api keys, route tokens, authorization headers) from any structured
// log output, plus helpers for the rare strings that have to be displayed
// in human-readable error messages.
package redact

import (
	"log/slog"
	"net/url"
	"strings"
)

// sensitiveKeys are slog attribute keys whose values should never appear
// in logs. Match is case-insensitive and substring-based: e.g. "api_key",
// "ApiKey", "AUTHORIZATION_HEADER" all redact.
var sensitiveKeys = []string{
	"api_key",
	"apikey",
	"token",
	"authorization",
	"bearer",
	"password",
	"secret",
	"pepper",
}

// Mask is the constant returned in place of redacted values.
const Mask = "<redacted>"

// SlogReplaceAttr is the slog HandlerOptions ReplaceAttr hook.
func SlogReplaceAttr(_ []string, a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, Mask)
	}
	return a
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// URL returns u with any userinfo (user:pass) stripped. Used for logging
// proxy URLs that legitimately have credentials embedded but must never
// appear in logs.
func URL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		// If we can't parse, redact the whole thing rather than risk
		// leaking embedded credentials.
		return Mask
	}
	if parsed.User != nil {
		parsed.User = nil
	}
	return parsed.String()
}

// APIKey returns a short fingerprint suitable for logging: first 8 chars of
// the key (the prefix is fixed at "hn_live_" so this is safe to show).
// Anything shorter than 12 chars is fully masked.
func APIKey(key string) string {
	if len(key) < 12 {
		return Mask
	}
	return key[:8] + "..."
}
