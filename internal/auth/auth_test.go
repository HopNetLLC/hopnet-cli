package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/config"
	"github.com/stretchr/testify/require"
)

func TestResolveKeyPrefersFlag(t *testing.T) {
	got, err := ResolveKey("hn_live_fromflag", strings.NewReader("hn_live_fromstdin"))
	require.NoError(t, err)
	require.Equal(t, "hn_live_fromflag", got)
}

func TestResolveKeyFallsBackToStdin(t *testing.T) {
	got, err := ResolveKey("", strings.NewReader("hn_live_piped\n"))
	require.NoError(t, err)
	require.Equal(t, "hn_live_piped", got)
}

func TestResolveKeyStdinTrimsWhitespace(t *testing.T) {
	got, err := ResolveKey("", strings.NewReader("  hn_live_padded   \n"))
	require.NoError(t, err)
	require.Equal(t, "hn_live_padded", got)
}

func TestResolveKeyNoneAvailable(t *testing.T) {
	_, err := ResolveKey("", strings.NewReader(""))
	require.ErrorIs(t, err, ErrNoKey)

	_, err = ResolveKey("", nil)
	require.ErrorIs(t, err, ErrNoKey)
}

func TestValidateFormatRejectsBadPrefix(t *testing.T) {
	require.Error(t, ValidateFormat("sk_test_xxxxxxxx"))
	require.Error(t, ValidateFormat(""))
}

func TestValidateFormatRejectsTooShort(t *testing.T) {
	require.Error(t, ValidateFormat("hn_live_1"))
}

func TestValidateFormatAcceptsValidKey(t *testing.T) {
	require.NoError(t, ValidateFormat("hn_live_abcdefghij"))
}

func TestLoginPersistsAfterSuccessfulVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/account", r.URL.Path)
		require.Equal(t, "Bearer hn_live_validkey1", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(client.Account{ID: "acc_1", Email: "a@b.c", Status: "active", BalanceCents: 5000})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	require.NoError(t, err)

	acc, err := Login(context.Background(), cfg, LoginOptions{
		APIKey: "hn_live_validkey1", BaseURL: srv.URL, ProxyURL: "https://proxy.example.com:443",
	})
	require.NoError(t, err)
	require.Equal(t, "acc_1", acc.ID)

	reloaded, err := config.Load(cfg.Path())
	require.NoError(t, err)
	require.Equal(t, "hn_live_validkey1", reloaded.APIKey)
	require.Equal(t, srv.URL, reloaded.BaseURL)
	require.Equal(t, "https://proxy.example.com:443", reloaded.ProxyURL)
}

func TestLoginRejectsInvalidKeyBeforeNetwork(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	require.NoError(t, err)

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	defer srv.Close()

	_, err = Login(context.Background(), cfg, LoginOptions{APIKey: "wrong_prefix_key", BaseURL: srv.URL})
	require.Error(t, err)
	require.False(t, called, "server should not be called for malformed keys")
}

func TestLoginDoesNotPersistOnVerifyFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_key"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	cfg.APIKey = "hn_live_priorvalid"
	require.NoError(t, cfg.Save())

	_, err = Login(context.Background(), cfg, LoginOptions{APIKey: "hn_live_newinvalid", BaseURL: srv.URL})
	require.Error(t, err)
	require.True(t, errors.Is(err, client.ErrUnauthorized))

	reloaded, err := config.Load(cfg.Path())
	require.NoError(t, err)
	require.Equal(t, "hn_live_priorvalid", reloaded.APIKey, "prior key must survive failed login")
}

func TestLoginSkipVerifyWritesWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	require.NoError(t, err)

	_, err = Login(context.Background(), cfg, LoginOptions{
		APIKey: "hn_live_offlinekey1", BaseURL: "http://does.not.exist", SkipVerify: true,
	})
	require.NoError(t, err)
	reloaded, err := config.Load(cfg.Path())
	require.NoError(t, err)
	require.Equal(t, "hn_live_offlinekey1", reloaded.APIKey)
}
