package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	cli "github.com/urfave/cli/v2"

	"github.com/HopNetLLC/hopnet-cli/internal/billing"
)

func billingCmd() *cli.Command {
	return &cli.Command{
		Name:  "billing",
		Usage: "Top up credits, view balance, browse ledger history",
		Subcommands: []*cli.Command{
			{
				Name:  "topup",
				Usage: "Open a Stripe Checkout session in the browser and (optionally) wait for the credit to land",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "usd", Usage: "Amount in whole dollars (server enforces $5–$10000)", Required: true},
					&cli.BoolFlag{Name: "no-open", Usage: "Do not auto-launch a browser; only print the URL"},
					&cli.BoolFlag{Name: "no-wait", Usage: "Return immediately after printing the URL; do not poll for balance"},
					&cli.DurationFlag{Name: "timeout", Usage: "Poll-for-balance timeout", Value: 5 * time.Minute},
				},
				Action: billingTopupAction,
			},
			{
				Name:   "balance",
				Usage:  "Show current credit balance plus the 5 most recent ledger entries",
				Action: billingBalanceAction,
			},
			{
				Name:  "history",
				Usage: "Paginated ledger view (newest first)",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "limit", Usage: "Rows per page (server clamps to 200)", Value: 50},
					&cli.StringFlag{Name: "before", Usage: "ISO8601 keyset cursor (created_at upper bound)"},
				},
				Action: billingHistoryAction,
			},
		},
	}
}

func billingTopupAction(c *cli.Context) error {
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	hc, err := newClientForCfg(cfg)
	if err != nil {
		return err
	}
	co, post, err := billing.Topup(c.Context, hc, billing.TopupOptions{
		AmountUSD:   c.Int("usd"),
		Open:        !c.Bool("no-open"),
		Wait:        !c.Bool("no-wait"),
		PollTimeout: c.Duration("timeout"),
		Stdout:      os.Stdout,
	})
	if err != nil {
		return err
	}
	if post != nil {
		fmt.Fprintf(os.Stdout, "balance: %s\n", formatUSD(post.BalanceCents))
		return nil
	}
	// post is nil → either --no-wait was set, or the poll timed out without
	// the balance increasing. Distinguish for the user.
	if c.Bool("no-wait") {
		fmt.Fprintf(os.Stdout, "session: %s (open in browser to complete)\n", co.SessionID)
	} else {
		fmt.Fprintln(os.Stdout, "topup did not land within timeout — re-run `hopnet billing balance` to check later")
	}
	return nil
}

func billingBalanceAction(c *cli.Context) error {
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	hc, err := newClientForCfg(cfg)
	if err != nil {
		return err
	}
	resp, err := hc.GetBilling(c.Context)
	if err != nil {
		return err
	}
	fmt.Printf("balance: %s\n", formatUSD(resp.BalanceCents))
	if len(resp.Recent) == 0 {
		fmt.Println("no ledger entries yet")
		return nil
	}
	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tTYPE\tAMOUNT\tROUTE")
	for _, r := range resp.Recent {
		route := ""
		if r.RouteID != nil {
			route = *r.RouteID
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			r.CreatedAt.Local().Format("2006-01-02 15:04"),
			r.Type, formatSignedUSD(r.AmountCents), route)
	}
	return tw.Flush()
}

func billingHistoryAction(c *cli.Context) error {
	cfg, err := loadCfg(c)
	if err != nil {
		return err
	}
	hc, err := newClientForCfg(cfg)
	if err != nil {
		return err
	}
	resp, err := hc.GetBillingHistory(c.Context, c.Int("limit"), c.String("before"))
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tTYPE\tAMOUNT\tROUTE\tID")
	for _, r := range resp.Rows {
		route := ""
		if r.RouteID != nil {
			route = *r.RouteID
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			r.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			r.Type, formatSignedUSD(r.AmountCents), route, r.ID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if resp.NextBefore != nil {
		fmt.Printf("\nnext page: --before %s\n", *resp.NextBefore)
	}
	return nil
}

// formatUSD formats integer cents as a human-friendly USD string. Always
// renders two decimal places. Negative values are sign-prefixed.
//
// Negation is routed through uint64 to dodge the math.MinInt64 overflow
// case (cents = -cents wraps at the int64 boundary). We never expect
// values that extreme through the API, but corrupted ledger data
// shouldn't produce a garbage formatter output either.
func formatUSD(cents int64) string {
	if cents >= 0 {
		return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
	}
	// Two's-complement absolute via uint64: -(cents+1) is in range
	// for any int64 input including math.MinInt64; +1 lifts it back.
	u := uint64(-(cents + 1)) + 1
	return fmt.Sprintf("-$%d.%02d", u/100, u%100)
}

// formatSignedUSD renders cents with an explicit + or - sign so debits and
// credits are visually distinct in the ledger view.
func formatSignedUSD(cents int64) string {
	switch {
	case cents > 0:
		return "+" + formatUSD(cents)
	case cents < 0:
		// formatUSD already prepends "-" for negatives.
		return formatUSD(cents)
	default:
		return strings.Repeat(" ", 1) + formatUSD(cents)
	}
}
