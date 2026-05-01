package main

import (
	"fmt"
	"os"

	cli "github.com/urfave/cli/v2"
)

var (
	version = "0.0.1"
	commit  = "dev"
)

func main() {
	app := &cli.App{
		Name:    "hopnet",
		Usage:   "Disposable network routes for agents, CI, browser automation",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		Commands: []*cli.Command{
			{
				Name:  "version",
				Usage: "Print version information",
				Action: func(_ *cli.Context) error {
					fmt.Printf("hopnet %s (%s)\n", version, commit)
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
