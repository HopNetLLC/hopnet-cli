package main

import (
	"errors"
	"fmt"
	"os"

	cli "github.com/urfave/cli/v2"

	"github.com/HopNetLLC/hopnet-cli/internal/bridge"
)

func bridgeCmd() *cli.Command {
	return &cli.Command{
		Name:  "bridge",
		Usage: "Run a local proxy bridge for a route (Phase 10)",
		Description: `Phase 8 ships only the flag scaffolding. The full bridge body lands in
Phase 10. Calling this command exits ` + fmt.Sprintf("%d", exitBridgeNotImpl) + ` with an explanation.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "route", Required: true, Usage: "Route id (must be in local cache)"},
			&cli.StringFlag{Name: "listen", Value: "127.0.0.1:0", Usage: "Local listen address"},
		},
		Action: bridgeAction,
	}
}

func bridgeAction(c *cli.Context) error {
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	err = bridge.Run(cfg, bridge.Options{
		RouteID: c.String("route"),
		Listen:  c.String("listen"),
	})
	if errors.Is(err, bridge.ErrNotImplemented) {
		fmt.Fprintln(os.Stderr, "hopnet: "+err.Error())
		return cli.Exit("", exitBridgeNotImpl)
	}
	return err
}
