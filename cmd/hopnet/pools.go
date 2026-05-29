package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	cli "github.com/urfave/cli/v2"

	"github.com/HopNetLLC/hopnet-cli/internal/poolsclient"
)

// poolsCmd registers `hopnet pools` per M3.5 coverage-union phase plan §7.
// `direct` route_class is excluded from /v1/pools (no upstream, no
// country concept); --help documents this.
func poolsCmd() *cli.Command {
	return &cli.Command{
		Name:  "pools",
		Usage: "Inspect customer-facing pool / coverage discovery",
		Description: "List the (route_class, country) pairs HopNet can route to.\n" +
			"Anonymous — no API key required. Reads HOPNET_API_BASE_URL.\n\n" +
			"Note: 'direct' is excluded from this list. Direct routes egress\n" +
			"from the proxy node itself with no upstream geo selection.",
		Subcommands: []*cli.Command{
			poolsListCmd(),
		},
	}
}

const poolsEllipsisAt = 12

func poolsListCmd() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List pools (route classes + countries)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "class",
				Usage: "Filter to a single route class (e.g. residential)",
			},
			&cli.StringFlag{
				Name:  "country",
				Usage: "Show only classes that cover this ISO-3166 alpha-2 country (e.g. US)",
			},
			&cli.BoolFlag{
				Name:  "all",
				Usage: "Show full country list (default elides at 12 entries)",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Emit the raw API response as JSON",
			},
		},
		Action: poolsListAction,
	}
}

func poolsListAction(c *cli.Context) error {
	api := poolsclient.New()
	resp, err := api.ListPools(c.Context)
	if err != nil {
		return err
	}

	// Codex r1 P3 fix: validate --class existence against the UNFILTERED
	// response BEFORE applying --country. Otherwise a valid class with
	// no coverage for the requested country (e.g. `--class datacenter
	// --country DE` when datacenter is US-only) would surface as
	// "no pool found for --class datacenter" — misleading: the class
	// exists, it just doesn't cover that country.
	if class := strings.TrimSpace(c.String("class")); class != "" {
		classExists := false
		for _, p := range resp.Pools {
			if p.RouteClass == class {
				classExists = true
				break
			}
		}
		if !classExists {
			return fmt.Errorf("no pool found for --class %q", class)
		}
		filtered := make([]poolsclient.PoolEntry, 0, 1)
		for _, p := range resp.Pools {
			if p.RouteClass == class {
				filtered = append(filtered, p)
			}
		}
		resp.Pools = filtered
	}

	// Country filter runs AFTER class so an unmatched (class, country)
	// pair renders as a discovery-style empty result instead of an
	// error. Country normalized to uppercase to match the wire format.
	if country := strings.ToUpper(strings.TrimSpace(c.String("country"))); country != "" {
		filtered := make([]poolsclient.PoolEntry, 0, len(resp.Pools))
		for _, p := range resp.Pools {
			if containsString(p.Countries, country) {
				filtered = append(filtered, p)
			}
		}
		resp.Pools = filtered
	}

	if c.Bool("json") {
		// Round-trip the (possibly filtered) response so jq pipelines
		// stay stable across CLI versions.
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	renderPoolsTable(os.Stdout, resp.Pools, c.Bool("all"))
	return nil
}

// renderPoolsTable writes a tabwriter table mirroring `hopnet route list`
// style. Empty class shows actionable copy keyed on status; the
// `direct` class is excluded server-side, not here.
func renderPoolsTable(w io.Writer, pools []poolsclient.PoolEntry, showAll bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ROUTE CLASS\tSTATUS\tCOUNTRIES\tSTICKY")
	for _, p := range pools {
		// Defensive sort so an old server returning unsorted lists
		// renders deterministically. The wire schema specifies sorted
		// ascending; cost is O(N log N) on a small N.
		countries := append([]string(nil), p.Countries...)
		sort.Strings(countries)
		sticky := append([]string(nil), p.StickyCountries...)
		sort.Strings(sticky)

		countriesCell := formatCoverageCell(countries, p.Status, showAll)
		stickyCell := formatStickyCell(sticky)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			p.RouteClass, p.Status, countriesCell, stickyCell,
		)
	}
	_ = tw.Flush()
}

// formatCoverageCell renders the COUNTRIES column. Per-status copy:
//
//	available     → comma-separated countries (elided unless --all)
//	not_refreshed → (coverage not yet refreshed)
//	unavailable   → (no upstream provisioned)
//	other / empty → (no coverage data)
func formatCoverageCell(countries []string, status string, showAll bool) string {
	if len(countries) == 0 {
		switch status {
		case "not_refreshed":
			return "(coverage not yet refreshed)"
		case "unavailable":
			return "(no upstream provisioned)"
		default:
			return "(no coverage data)"
		}
	}
	if !showAll && len(countries) > poolsEllipsisAt {
		head := countries[:poolsEllipsisAt]
		extra := len(countries) - poolsEllipsisAt
		return fmt.Sprintf("%s, ... (+%d more)", strings.Join(head, ", "), extra)
	}
	return strings.Join(countries, ", ")
}

func formatStickyCell(sticky []string) string {
	if len(sticky) == 0 {
		return "(none)"
	}
	return strings.Join(sticky, ", ")
}

func containsString(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
