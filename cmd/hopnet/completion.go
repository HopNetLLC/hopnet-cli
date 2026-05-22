package main

import (
	_ "embed"
	"fmt"

	cli "github.com/urfave/cli/v2"
)

//go:embed completions/bash.sh
var completionBash string

//go:embed completions/zsh.sh
var completionZsh string

//go:embed completions/fish.fish
var completionFish string

func completionCmd() *cli.Command {
	return &cli.Command{
		Name:      "completion",
		Usage:     "Print shell completion script (bash, zsh, fish)",
		ArgsUsage: "<bash|zsh|fish>",
		Description: `Emits the completion wrapper for the named shell. Source it from your
shell rc to enable tab completion:

    # bash
    source <(hopnet completion bash)

    # zsh
    source <(hopnet completion zsh)

    # fish
    hopnet completion fish | source`,
		Action: completionAction,
	}
}

func completionAction(c *cli.Context) error {
	if c.NArg() != 1 {
		return fmt.Errorf("completion requires exactly one argument: bash | zsh | fish")
	}
	switch c.Args().Get(0) {
	case "bash":
		fmt.Print(completionBash)
	case "zsh":
		fmt.Print(completionZsh)
	case "fish":
		fmt.Print(completionFish)
	default:
		return fmt.Errorf("unsupported shell %q (supported: bash, zsh, fish)", c.Args().Get(0))
	}
	return nil
}
