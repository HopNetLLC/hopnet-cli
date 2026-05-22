package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	cli "github.com/urfave/cli/v2"

	"github.com/HopNetLLC/hopnet-cli/internal/doctor"
)

func doctorCmd() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Diagnose config, auth, and connectivity",
		Description: `Checks (in order):
  1. config       — file exists at $XDG_CONFIG_HOME/hopnet/config.json, mode 0600
  2. api-key      — present + format passes the hn_live_… prefix + length rule
  3. control-api  — GET <base_url>/v1/account returns 200
  4. proxy        — TCP dial to <proxy_url> host:port succeeds within 3s

Exits 0 if every check passes, 1 if any fails.`,
		Action: doctorAction,
	}
}

func doctorAction(c *cli.Context) error {
	// loadCfg returns an error only if the config file is malformed
	// (missing file is treated as a fresh zero Config). Pass the load
	// error through to doctor so the config check can surface the
	// actual parse failure instead of a generic "could not load".
	cfg, loadErr := loadCfg(c)
	results := doctor.Run(c.Context, doctor.Options{
		Config:        cfg,
		ConfigLoadErr: loadErr,
	})

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, r := range results {
		fmt.Fprintf(tw, "[%s]\t%s\t%s\n", r.Status, r.Name, r.Detail)
	}
	_ = tw.Flush()

	if !doctor.AllOK(results) {
		return cli.Exit("", 1)
	}
	return nil
}
