package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	cli "github.com/urfave/cli/v2"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/config"
	"github.com/HopNetLLC/hopnet-cli/internal/redact"
)

// Build-time injected via -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "0.0.1"
	commit  = "dev"
)

// Exit codes (kept in sync with the P8 plan).
const (
	exitOK              = 0
	exitGeneric         = 1
	exitAuth            = 2
	exitInsufficientCC  = 3
	exitNotFound        = 4
	exitServer          = 5
	exitBridgeNotImpl   = 6
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:       logLevel(),
		ReplaceAttr: redact.SlogReplaceAttr,
	}))
	slog.SetDefault(logger)

	app := buildApp()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := app.RunContext(ctx, os.Args); err != nil {
		os.Exit(mapErrorToExitCode(err))
	}
}

func logLevel() slog.Level {
	if v := os.Getenv("HOPNET_DEBUG"); v != "" && v != "0" && v != "false" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func buildApp() *cli.App {
	return &cli.App{
		Name:                  "hopnet",
		Usage:                 "Disposable network routes for agents, CI, browser automation",
		Version:               fmt.Sprintf("%s (%s)", version, commit),
		EnableBashCompletion:  true,
		HideHelpCommand:       true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "Path to config file (default $XDG_CONFIG_HOME/hopnet/config.json)",
				EnvVars: []string{"HOPNET_CONFIG"},
			},
		},
		Commands: []*cli.Command{
			versionCmd(),
			authCmd(),
			routeCmd(),
			envCmd(),
			runCmd(),
			bridgeCmd(),
			receiptCmd(),
			billingCmd(),
			completionCmd(),
			doctorCmd(),
		},
	}
}

func versionCmd() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print version information",
		Action: func(_ *cli.Context) error {
			fmt.Printf("hopnet %s (%s)\n", version, commit)
			return nil
		},
	}
}

// resolveConfigPath reads --config (set on the parent app), falling back
// to DefaultPath().
func resolveConfigPath(c *cli.Context) (string, error) {
	if p := c.String("config"); p != "" {
		return p, nil
	}
	return config.DefaultPath()
}

// loadCfg loads the config and warns on loose mode.
func loadCfg(c *cli.Context) (*config.Config, error) {
	p, err := resolveConfigPath(c)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(p)
	if err != nil {
		return nil, err
	}
	if w := cfg.CheckMode(); w != "" {
		slog.Warn(w)
	}
	cfg.Prune(time.Now().UTC())
	return cfg, nil
}

// mapErrorToExitCode walks the error chain and selects an exit code.
// Falls back to exitGeneric on anything we don't recognize.
func mapErrorToExitCode(err error) int {
	if err == nil {
		return exitOK
	}
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		return exitAuth
	case errors.Is(err, client.ErrInsufficientCredit):
		return exitInsufficientCC
	case errors.Is(err, client.ErrNotFound):
		return exitNotFound
	case errors.Is(err, client.ErrServer):
		return exitServer
	}
	// urfave/cli wraps user-facing errors via cli.Exit; honor that exit code.
	var exitCoder cli.ExitCoder
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	fmt.Fprintln(os.Stderr, err)
	return exitGeneric
}

// newClientForCfg builds an HTTP client wired to the config.
func newClientForCfg(cfg *config.Config) (*client.Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("not logged in — run `hopnet auth login --api-key hn_live_...` first")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("config has no base_url; run `hopnet auth login --base-url ...`")
	}
	return client.New(cfg.BaseURL, cfg.APIKey), nil
}
