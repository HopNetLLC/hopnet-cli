package billing

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// osStatHelper exists so the file-mode test doesn't have to import os
// twice (once for the test, once for production code via build tags).
func osStatHelper(path string) (os.FileInfo, error) { return os.Stat(path) }

func TestObtainIdempotencyKey_FreshGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topup-pending.json")
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	key, err := obtainIdempotencyKey(path, 50, now)
	require.NoError(t, err)
	require.True(t, len(key) > len("hopnet-cli-"),
		"expected hopnet-cli- prefixed key, got %q", key)

	saved, ok := loadPendingTopup(path)
	require.True(t, ok)
	require.Equal(t, key, saved.IdempotencyKey)
	require.Equal(t, 50, saved.AmountUSD)
	require.True(t, saved.StartedAt.Equal(now))
}

func TestObtainIdempotencyKey_ReusesWithinTTLForSameAmount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topup-pending.json")
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	first, err := obtainIdempotencyKey(path, 50, now)
	require.NoError(t, err)

	// 5 minutes later, same amount → same key.
	later := now.Add(5 * time.Minute)
	second, err := obtainIdempotencyKey(path, 50, later)
	require.NoError(t, err)
	require.Equal(t, first, second,
		"retry within TTL should reuse the stashed key (got %q vs %q)", first, second)
}

func TestObtainIdempotencyKey_RegeneratesAfterTTLExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topup-pending.json")
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	first, err := obtainIdempotencyKey(path, 50, now)
	require.NoError(t, err)

	// Past the 30-min TTL → fresh key.
	later := now.Add(31 * time.Minute)
	second, err := obtainIdempotencyKey(path, 50, later)
	require.NoError(t, err)
	require.NotEqual(t, first, second,
		"expired entry should be replaced with a fresh key")
}

func TestObtainIdempotencyKey_RegeneratesOnAmountChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topup-pending.json")
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	fifty, err := obtainIdempotencyKey(path, 50, now)
	require.NoError(t, err)

	// Different amount within TTL → different key. User asking for $100
	// wants a $100 session, not the stashed $50 one.
	hundred, err := obtainIdempotencyKey(path, 100, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotEqual(t, fifty, hundred,
		"different amount should generate a fresh key")

	saved, ok := loadPendingTopup(path)
	require.True(t, ok)
	require.Equal(t, 100, saved.AmountUSD,
		"pending entry should reflect the latest amount")
}

func TestClearPendingTopup_RemovesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topup-pending.json")
	_, err := obtainIdempotencyKey(path, 50, time.Now())
	require.NoError(t, err)

	require.NoError(t, clearPendingTopup(path))

	_, ok := loadPendingTopup(path)
	require.False(t, ok, "expected pending file to be gone after clear")
}

func TestClearPendingTopup_TolerantOfMissingFile(t *testing.T) {
	require.NoError(t, clearPendingTopup(filepath.Join(t.TempDir(), "nope.json")))
}

func TestSavePendingTopup_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topup-pending.json")
	_, err := obtainIdempotencyKey(path, 50, time.Now())
	require.NoError(t, err)

	info, err := osStatHelper(path)
	require.NoError(t, err)
	mode := info.Mode().Perm()
	require.Equal(t, mode.String(), "-rw-------",
		"pending file must be 0600 to keep the idempotency key off other users' shoulders")
}
