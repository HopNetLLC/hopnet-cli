package main

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatUSD(t *testing.T) {
	cases := []struct {
		name  string
		cents int64
		want  string
	}{
		{"zero", 0, "$0.00"},
		{"one cent", 1, "$0.01"},
		{"one dollar", 100, "$1.00"},
		{"fifty bucks", 5000, "$50.00"},
		{"big number", 999_999_99, "$999999.99"},
		{"negative one cent", -1, "-$0.01"},
		{"negative one dollar", -100, "-$1.00"},
		{"negative fifty bucks", -5000, "-$50.00"},
		// Cosmic but real: corrupted ledger data could surface MinInt64.
		// `cents = -cents` would wrap to itself; the uint64-routed absolute
		// must produce a deterministic non-garbage string instead.
		{"math.MinInt64", math.MinInt64, "-$92233720368547758.08"},
		// One step less extreme — the previous-implementation overflow
		// path crossed exactly at MinInt64; off-by-one should still be fine.
		{"math.MinInt64+1", math.MinInt64 + 1, "-$92233720368547758.07"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, formatUSD(tc.cents))
		})
	}
}

func TestFormatSignedUSD(t *testing.T) {
	require.Equal(t, "+$50.00", formatSignedUSD(5000))
	require.Equal(t, "-$0.42", formatSignedUSD(-42))
	// Zero gets a leading space so columns align with signed values above.
	require.Equal(t, " $0.00", formatSignedUSD(0))
}
