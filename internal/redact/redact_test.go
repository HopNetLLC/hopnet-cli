package redact

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlogReplaceAttrRedactsSensitiveKeys(t *testing.T) {
	for _, key := range []string{"api_key", "ApiKey", "AUTHORIZATION", "bearer", "token", "X-Token"} {
		var buf bytes.Buffer
		h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: SlogReplaceAttr})
		l := slog.New(h)
		l.Info("msg", key, "actual_secret_value")

		var got map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		require.NotContains(t, buf.String(), "actual_secret_value", "key %q leaked", key)
		require.Equal(t, Mask, got[key])
	}
}

func TestSlogReplaceAttrPassesThroughBenign(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: SlogReplaceAttr})
	l := slog.New(h)
	l.Info("msg", "route_id", "rt_xxxx", "host", "example.com")

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, "rt_xxxx", got["route_id"])
	require.Equal(t, "example.com", got["host"])
}

func TestURLStripsUserInfo(t *testing.T) {
	require.Equal(t, "https://proxy.hopnet.io:443", URL("https://rt_x:rtk_y@proxy.hopnet.io:443"))
	require.Equal(t, "http://example.com", URL("http://example.com"))
}

func TestURLOnGarbageReturnsMask(t *testing.T) {
	require.Equal(t, Mask, URL("://not a url at all"))
}

func TestAPIKeyFingerprint(t *testing.T) {
	require.Equal(t, "hn_live_...", APIKey("hn_live_abcdefgh"))
	require.Equal(t, Mask, APIKey("short"))
}
