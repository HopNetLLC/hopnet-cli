package main

import (
	"fmt"
	"os"

	cli "github.com/urfave/cli/v2"
)

func receiptCmd() *cli.Command {
	return &cli.Command{
		Name:      "receipt",
		Usage:     "Print a usage receipt for a route",
		ArgsUsage: "<route-id>",
		Description: `Receipt content is derived from /v1/routes/:id/usage. Works for active,
expired, or revoked routes; the server retains the metering row so
receipts remain available after the TTL.`,
		Action: receiptAction,
	}
}

func receiptAction(c *cli.Context) error {
	if c.NArg() != 1 {
		return fmt.Errorf("receipt requires exactly one argument: <route-id>")
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
