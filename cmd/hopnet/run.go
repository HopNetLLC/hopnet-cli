package main

import (
	"fmt"
	"os"
	"time"

	cli "github.com/urfave/cli/v2"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/run"
)

func runCmd() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Run a command with proxy env injected; revoke route on exit",
		ArgsUsage: "-- <command> [args...]",
		Description: `Creates a route (or reuses --route) and execs the given command with
HTTPS_PROXY/HTTP_PROXY/ALL_PROXY/NO_PROXY pointing at the HopNet proxy.
The route is revoked when the command exits unless --keep-route is set
(only effective when the route was created by this run; --route is
caller-owned and never auto-revoked).`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "route", Usage: "Reuse an existing route id (must be in local cache)"},
			&cli.DurationFlag{Name: "ttl", Usage: "Route TTL when creating ad-hoc", Value: 15 * time.Minute},
			&cli.IntFlag{Name: "max-mb", Usage: "Byte cap (MB) when creating ad-hoc"},
			&cli.StringFlag{Name: "class", Usage: "Route class when creating ad-hoc", Value: "direct"},
			&cli.StringFlag{Name: "country", Usage: "Country code when creating ad-hoc"},
			&cli.StringSliceFlag{Name: "allow", Usage: "Allow host (repeatable)"},
			&cli.StringSliceFlag{Name: "deny", Usage: "Deny host (repeatable)"},
			&cli.StringFlag{Name: "label", Usage: "Route label", Value: "hopnet-run"},
			&cli.BoolFlag{Name: "keep-route", Usage: "Do not revoke a self-created route after exit"},
		},
		Action: runAction,
	}
}

func runAction(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("usage: hopnet run [flags] -- <command> [args...]")
	}
	argv := c.Args().Slice()

	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	api, err := newClientForCfg(cfg)
	if err != nil {
		return err
	}

	opts := run.Options{
		Argv:      argv,
		KeepRoute: c.Bool("keep-route"),
	}
	if rid := c.String("route"); rid != "" {
		opts.RouteID = rid
	} else {
		ttl := c.Duration("ttl")
		if ttl < time.Second || ttl > 24*time.Hour {
			return fmt.Errorf("--ttl must be between 1s and 24h")
		}
		req := &client.CreateRouteRequest{
			TTLSeconds: int(ttl.Seconds()),
			RouteClass: c.String("class"),
			ClientKind: "cli",
			Label:      c.String("label"),
			Country:    c.String("country"),
			Allow:      nonEmpty(c.StringSlice("allow")),
			Deny:       nonEmpty(c.StringSlice("deny")),
		}
		if c.IsSet("max-mb") {
			v := c.Int("max-mb")
			req.MaxMB = &v
		}
		opts.CreateRequest = req
	}

	res, err := run.Run(c.Context, api, cfg, opts)
	if err != nil {
		return err
	}

	// Receipt to stderr so child stdout pipes stay clean.
	if res.Receipt != nil {
		fmt.Fprintln(os.Stderr, "")
		printUsage(os.Stderr, res.Receipt)
	} else if res.ReceiptErr != nil {
		fmt.Fprintf(os.Stderr, "hopnet: receipt unavailable: %v\n", res.ReceiptErr)
	}
	if res.RevokeErr != nil {
		fmt.Fprintf(os.Stderr, "hopnet: revoke failed (route may auto-expire): %v\n", res.RevokeErr)
	}

	// Exit with the child's status; cli.Exit wraps it without printing.
	if res.ExitCode != 0 {
		return cli.Exit("", res.ExitCode)
	}
	return nil
}
