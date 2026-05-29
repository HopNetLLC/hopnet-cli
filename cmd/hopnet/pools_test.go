package main

import (
	"strings"
	"testing"

	"github.com/HopNetLLC/hopnet-cli/internal/poolsclient"
)

func TestFormatCoverageCell(t *testing.T) {
	cases := []struct {
		name      string
		countries []string
		status    string
		showAll   bool
		want      string
	}{
		{
			name:      "available with short list",
			countries: []string{"US", "GB", "DE"},
			status:    "available",
			want:      "US, GB, DE",
		},
		{
			name: "available with long list elided",
			countries: []string{
				"AU", "BR", "CA", "DE", "ES", "FR", "GB", "IN",
				"IT", "JP", "MX", "NL", "PL", "SG", "US",
			},
			status:  "available",
			showAll: false,
			want:    "AU, BR, CA, DE, ES, FR, GB, IN, IT, JP, MX, NL, ... (+3 more)",
		},
		{
			name: "available with --all does not elide",
			countries: []string{
				"AU", "BR", "CA", "DE", "ES", "FR", "GB", "IN",
				"IT", "JP", "MX", "NL", "PL", "SG", "US",
			},
			status:  "available",
			showAll: true,
			want:    "AU, BR, CA, DE, ES, FR, GB, IN, IT, JP, MX, NL, PL, SG, US",
		},
		{
			name:      "not_refreshed empty list",
			countries: []string{},
			status:    "not_refreshed",
			want:      "(coverage not yet refreshed)",
		},
		{
			name:      "unavailable empty list",
			countries: []string{},
			status:    "unavailable",
			want:      "(no upstream provisioned)",
		},
		{
			name:      "unknown status empty list",
			countries: []string{},
			status:    "something_else",
			want:      "(no coverage data)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatCoverageCell(tc.countries, tc.status, tc.showAll)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatStickyCell(t *testing.T) {
	if got := formatStickyCell(nil); got != "(none)" {
		t.Errorf("nil sticky: got %q, want (none)", got)
	}
	if got := formatStickyCell([]string{}); got != "(none)" {
		t.Errorf("empty sticky: got %q, want (none)", got)
	}
	if got := formatStickyCell([]string{"US", "GB"}); got != "US, GB" {
		t.Errorf("sticky list: got %q, want US, GB", got)
	}
}

func TestContainsString(t *testing.T) {
	if !containsString([]string{"US", "GB"}, "US") {
		t.Error("expected containsString to find US")
	}
	if containsString([]string{"US", "GB"}, "DE") {
		t.Error("expected containsString to NOT find DE")
	}
	if containsString(nil, "US") {
		t.Error("expected containsString on nil to be false")
	}
}

// Smoke that the renderer doesn't panic on the empty + available shapes
// and includes the status word in the output.
func TestRenderPoolsTableSmoke(t *testing.T) {
	var pools []poolsclient.PoolEntry = []poolsclient.PoolEntry{
		{
			RouteClass:      "residential",
			Countries:       []string{"US", "GB"},
			StickyCountries: []string{"US"},
			Status:          "available",
		},
		{
			RouteClass:      "isp",
			Countries:       []string{},
			StickyCountries: []string{},
			Status:          "unavailable",
		},
	}
	var buf strings.Builder
	renderPoolsTable(&buf, pools, false)
	out := buf.String()
	if !strings.Contains(out, "residential") {
		t.Errorf("expected residential in output, got %q", out)
	}
	if !strings.Contains(out, "available") {
		t.Errorf("expected status 'available' in output, got %q", out)
	}
	if !strings.Contains(out, "(no upstream provisioned)") {
		t.Errorf("expected unavailable copy in output, got %q", out)
	}
}
