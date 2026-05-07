package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	cli "github.com/urfave/cli/v2"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/config"
)

func routeCmd() *cli.Command {
	return &cli.Command{
		Name:  "route",
		Usage: "Manage HopNet routes",
		Subcommands: []*cli.Command{
			routeCreateCmd(),
			routeListCmd(),
			routeUsageCmd(),
			routeDeleteCmd(),
		},
	}
}

func routeCreateCmd() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "Create a route",
		ArgsUsage: " ",
		Flags: []cli.Flag{
			&cli.DurationFlag{Name: "ttl", Usage: "Route TTL (e.g. 15m, 1h)", Value: 15 * time.Minute},
			&cli.IntFlag{Name: "max-mb", Usage: "Byte cap (MB)"},
			&cli.Int64Flag{Name: "max-cost-cents", Usage: "Cost cap (cents)"},
			&cli.StringFlag{Name: "class", Usage: "Route class (free|direct|datacenter|residential|fast|auto)", Value: "direct"},
			&cli.StringFlag{Name: "country", Usage: "ISO-3166 country code"},
			&cli.IntFlag{Name: "min-mbps", Usage: "Requested minimum throughput (Mbps)"},
			&cli.StringSliceFlag{Name: "allow", Usage: "Hostname allowlist (repeatable)"},
			&cli.StringSliceFlag{Name: "deny", Usage: "Hostname denylist (repeatable)"},
			&cli.StringFlag{Name: "label", Usage: "Free-form label"},
			&cli.StringFlag{Name: "client-kind", Usage: "Client identity (cli|browser|playwright|ci|mcp|api)", Value: "cli"},
			&cli.StringFlag{Name: "template", Usage: "Template name (if using a template)"},
		},
		Action: routeCreateAction,
	}
}

func routeCreateAction(c *cli.Context) error {
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	api, err := newClientForCfg(cfg)
	if err != nil {
		return err
	}

	ttl := c.Duration("ttl")
	if ttl < time.Second {
		return fmt.Errorf("--ttl must be at least 1s")
	}
	if ttl > 24*time.Hour {
		return fmt.Errorf("--ttl cannot exceed 24h")
	}
	req := &client.CreateRouteRequest{
		TTLSeconds: int(ttl.Seconds()),
		RouteClass: c.String("class"),
		ClientKind: c.String("client-kind"),
		Label:      c.String("label"),
		Country:    c.String("country"),
		Allow:      nonEmpty(c.StringSlice("allow")),
		Deny:       nonEmpty(c.StringSlice("deny")),
	}
	if c.IsSet("max-mb") {
		v := c.Int("max-mb")
		req.MaxMB = &v
	}
	if c.IsSet("max-cost-cents") {
		v := c.Int64("max-cost-cents")
		req.MaxCostCents = &v
	}
	if c.IsSet("min-mbps") {
		v := c.Int("min-mbps")
		req.RequestedMinMbps = &v
	}
	if t := c.String("template"); t != "" {
		req.TemplateName = t
	}

	resp, err := api.CreateRoute(c.Context, req)
	if err != nil {
		return err
	}
	cfg.PutRoute(resp.ID, config.Route{
		Token:        resp.Token,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    resp.ExpiresAt,
		Label:        req.Label,
		RouteClass:   resp.RouteClass,
		Country:      resp.Country,
		MaxBytes:     resp.MaxBytes,
		MaxCostCents: resp.MaxCostCents,
		RouteVersion: resp.RouteVersion,
	})
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("persist route: %w", err)
	}
	// Machine-readable single line on stdout: id token expires_at
	fmt.Printf("%s %s %s\n", resp.ID, resp.Token, resp.ExpiresAt.Format(time.RFC3339))
	return nil
}

func routeListCmd() *cli.Command {
	return &cli.Command{
		Name:   "list",
		Usage:  "List routes for the authenticated account",
		Action: routeListAction,
	}
}

func routeListAction(c *cli.Context) error {
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	api, err := newClientForCfg(cfg)
	if err != nil {
		return err
	}
	resp, err := api.ListRoutes(c.Context)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tCLASS\tCOUNTRY\tCREATED\tEXPIRES\tLABEL")
	for _, r := range resp.Routes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.ID, r.Status, r.RouteClass, dashIfEmpty(r.Country),
			r.CreatedAt.Format(time.RFC3339), expiresStr(r.ExpiresAt),
			dashIfEmpty(r.Label),
		)
	}
	return w.Flush()
}

func routeUsageCmd() *cli.Command {
	return &cli.Command{
		Name:      "usage",
		Usage:     "Show usage and per-destination breakdown for a route",
		ArgsUsage: "<route-id>",
		Action:    routeUsageAction,
	}
}

func routeUsageAction(c *cli.Context) error {
	if c.NArg() != 1 {
		return fmt.Errorf("usage requires exactly one argument: <route-id>")
	}
	id := c.Args().Get(0)
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	api, err := newClientForCfg(cfg)
	if err != nil {
		return err
	}
	u, err := api.GetUsage(c.Context, id)
	if err != nil {
		return err
	}
	printUsage(os.Stdout, u)
	return nil
}

func routeDeleteCmd() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Revoke a route",
		ArgsUsage: "<route-id>",
		Action:    routeDeleteAction,
	}
}

func routeDeleteAction(c *cli.Context) error {
	if c.NArg() != 1 {
		return fmt.Errorf("delete requires exactly one argument: <route-id>")
	}
	id := c.Args().Get(0)
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	api, err := newClientForCfg(cfg)
	if err != nil {
		return err
	}
	apiErr := api.DeleteRoute(c.Context, id)
	// Best-effort local cleanup regardless of server result.
	cfg.DeleteRoute(id)
	_ = cfg.Save()
	if apiErr != nil {
		return apiErr
	}
	fmt.Printf("revoked %s\n", id)
	return nil
}

func nonEmpty(s []string) []string {
	out := make([]string, 0, len(s))
	for _, v := range s {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func expiresStr(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

// printUsage renders both `route usage` and the body of `receipt`.
func printUsage(w io.Writer, u *client.Usage) {
	fmt.Fprintf(w, "Route   %s\n", u.ID)
	fmt.Fprintf(w, "Status  %s\n", u.Status)
	fmt.Fprintf(w, "Created %s\n", u.CreatedAt.Format(time.RFC3339))
	if u.ExpiresAt != nil {
		fmt.Fprintf(w, "Expires %s\n", u.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "Bytes   %s (c2u %s, u2c %s)\n",
		formatBytes(u.Bytes),
		formatBytes(u.BytesClientToUpstream),
		formatBytes(u.BytesUpstreamToClient),
	)
	fmt.Fprintf(w, "Cost    %s (estimated)\n", formatCents(u.EstimatedCostCents))
	if u.ObservedAvgMbps != nil {
		fmt.Fprintf(w, "Mbps    %.2f (observed avg)\n", *u.ObservedAvgMbps)
	}
	if len(u.Destinations) > 0 {
		fmt.Fprintln(w, "Destinations")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, d := range u.Destinations {
			fmt.Fprintf(tw, "  %s:%d\t%d tunnels\t%s\n", d.Host, d.Port, d.Tunnels, formatBytes(d.Bytes))
		}
		_ = tw.Flush()
	}
}

func formatBytes(n int64) string {
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
	)
	switch {
	case n >= GiB:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(GiB))
	case n >= MiB:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(MiB))
	case n >= KiB:
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(KiB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
