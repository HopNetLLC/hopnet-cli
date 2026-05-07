package main

import (
	"fmt"
	"os"

	cli "github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/HopNetLLC/hopnet-cli/internal/auth"
	"github.com/HopNetLLC/hopnet-cli/internal/redact"
)

func authCmd() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Authentication commands",
		Subcommands: []*cli.Command{
			{
				Name:  "login",
				Usage: "Persist an API key (and optional base/proxy URLs) to the config file",
				Description: `Read an API key from --api-key, stdin (when piped), or an interactive
prompt with no echo. The key is verified against /v1/account before being
written to disk unless --skip-verify is set.`,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "api-key", Usage: "API key (`hn_live_...`); reads from stdin or prompts when omitted", EnvVars: []string{"HOPNET_API_KEY"}},
					&cli.StringFlag{Name: "base-url", Usage: "Override the API base URL (default: production or current config)"},
					&cli.StringFlag{Name: "proxy-url", Usage: "Override the HopNet proxy URL (default: production or current config)"},
					&cli.BoolFlag{Name: "skip-verify", Usage: "Skip the /v1/account ping (do not use with new keys)"},
				},
				Action: authLoginAction,
			},
		},
	}
}

func authLoginAction(c *cli.Context) error {
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}

	flagKey := c.String("api-key")
	var key string
	switch {
	case flagKey != "":
		key = flagKey
	case !isStdinTTY():
		// Pipe / redirected input.
		key, err = auth.ResolveKey("", os.Stdin)
		if err != nil {
			return err
		}
	default:
		// Interactive: prompt with no echo.
		fmt.Fprint(os.Stderr, "API key: ")
		raw, perr := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if perr != nil {
			return fmt.Errorf("read api key from terminal: %w", perr)
		}
		key, err = auth.ResolveKey(string(raw), nil)
		if err != nil {
			return err
		}
	}

	opts := auth.LoginOptions{
		APIKey:     key,
		BaseURL:    c.String("base-url"),
		ProxyURL:   c.String("proxy-url"),
		SkipVerify: c.Bool("skip-verify"),
	}
	account, err := auth.Login(c.Context, cfg, opts)
	if err != nil {
		return err
	}
	if account != nil {
		fmt.Fprintf(os.Stderr,
			"logged in as %s (account %s, balance %s, key %s)\n",
			account.Email, account.ID, formatCents(account.BalanceCents), redact.APIKey(key),
		)
	} else {
		fmt.Fprintf(os.Stderr, "saved key %s (verification skipped)\n", redact.APIKey(key))
	}
	return nil
}

func isStdinTTY() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func formatCents(c int64) string {
	if c < 0 {
		return fmt.Sprintf("-$%d.%02d", -c/100, -c%100)
	}
	return fmt.Sprintf("$%d.%02d", c/100, c%100)
}

