package main

import (
	"sort"
	"testing"

	cli "github.com/urfave/cli/v2"
	"github.com/stretchr/testify/require"
)

func TestVersionDefault(t *testing.T) {
	if version == "" {
		t.Error("version should not be empty")
	}
	if commit == "" {
		t.Error("commit should not be empty")
	}
}

func TestCommandTreeMatchesSpec(t *testing.T) {
	app := buildApp()
	got := commandNames(app.Commands)
	sort.Strings(got)
	require.Equal(t, []string{"auth", "billing", "bridge", "completion", "doctor", "env", "pools", "receipt", "route", "run", "version"}, got)

	auth := findCmd(t, app.Commands, "auth")
	require.Equal(t, []string{"login"}, commandNames(auth.Subcommands))

	route := findCmd(t, app.Commands, "route")
	routeSubs := commandNames(route.Subcommands)
	sort.Strings(routeSubs)
	require.Equal(t, []string{"connects", "create", "delete", "list", "usage"}, routeSubs)

	billing := findCmd(t, app.Commands, "billing")
	billingSubs := commandNames(billing.Subcommands)
	sort.Strings(billingSubs)
	require.Equal(t, []string{"balance", "history", "topup"}, billingSubs)

	pools := findCmd(t, app.Commands, "pools")
	require.Equal(t, []string{"list"}, commandNames(pools.Subcommands))
}

func commandNames(cmds []*cli.Command) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Name)
	}
	return out
}

func findCmd(t *testing.T, cmds []*cli.Command, name string) *cli.Command {
	t.Helper()
	for _, c := range cmds {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("command %q not found", name)
	return nil
}
