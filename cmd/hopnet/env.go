package main

import (
	"fmt"
	"net/url"

	cli "github.com/urfave/cli/v2"
)

func envCmd() *cli.Command {
	return &cli.Command{
		Name:      "env",
		Usage:     "Print export statements for a route's proxy credentials",
		ArgsUsage: "<route-id>",
		Description: `Output is suitable for "eval $(hopnet env rt_...)". The route must have
been created via this CLI on this host (the control-api only returns
the route token at creation time).`,
		Action: envAction,
	}
}

func envAction(c *cli.Context) error {
	if c.NArg() != 1 {
		return fmt.Errorf("env requires exactly one argument: <route-id>")
	}
	id := c.Args().Get(0)
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	r, ok := cfg.GetRoute(id)
	if !ok {
		return fmt.Errorf("route %s not found in local cache (only routes created via this CLI on this host are remembered)", id)
	}
	u, err := url.Parse(cfg.ProxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy_url in config: %w", err)
	}
	u.User = url.UserPassword(id, r.Token)
	proxy := u.String()

	fmt.Printf(`export HTTPS_PROXY=%q
export HTTP_PROXY=%q
export ALL_PROXY=%q
export NO_PROXY="localhost,127.0.0.1,::1"
export HOPNET_ROUTE_ID=%q
`, proxy, proxy, proxy, id)
	return nil
}
