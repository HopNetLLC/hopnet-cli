// Package billing wraps the topup-and-poll orchestration so cmd/hopnet's
// billing subcommand stays thin (testable via a fake Client).
package billing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
)

// pendingTopupTTL is how long a stashed idempotency key stays valid for
// retry-reuse. Longer than the default poll timeout (5 min) so a network
// drop during polling, an interrupted CLI, or a slow customer paying via
// browser still maps a re-run back to the same Stripe session. Shorter
// than Stripe's 24h idempotency-key validity so two intentional topups
// for the same amount on the same day don't accidentally collapse.
const pendingTopupTTL = 30 * time.Minute

// Clienter is the subset of *client.Client topup needs. Tests pass a fake.
type Clienter interface {
	CreateCheckout(ctx context.Context, amountUSD int, idempotencyKey string) (*client.CheckoutResponse, error)
	GetBilling(ctx context.Context) (*client.BillingResponse, error)
}

// TopupOptions controls UX behavior. Open=true opens the Checkout URL in the
// platform browser; Wait=true polls /v1/account/billing for a balance bump
// up to PollTimeout. Defaults: Open=true, Wait=true, PollTimeout=5min,
// PollInterval=3s.
//
// PendingPath overrides the on-disk location of the in-flight idempotency
// key (used for retry deduplication). When empty, defaults to
// $XDG_CONFIG_HOME/hopnet/topup-pending.json (same dir as the main config).
// Tests inject a tmp path; callers in cmd/hopnet leave it empty.
type TopupOptions struct {
	AmountUSD    int
	Open         bool
	Wait         bool
	PollInterval time.Duration
	PollTimeout  time.Duration
	Stdout       io.Writer
	PendingPath  string
}

// pendingTopup is the on-disk record of an in-flight topup. Holds the
// idempotency key sent to the server so a re-run within the TTL window
// reuses the key — this is the load-bearing dedup for the network-drop
// retry case (server accepted POST, client never saw the response).
type pendingTopup struct {
	IdempotencyKey string    `json:"idempotency_key"`
	AmountUSD      int       `json:"amount_usd"`
	StartedAt      time.Time `json:"started_at"`
}

// Topup creates a Stripe Checkout session, optionally opens the URL, and
// optionally polls for balance increase. Returns:
//   - (CheckoutResponse, BillingResponse, nil) when polling observed the
//     balance increase by ≥ AmountUSD — caller renders the new balance.
//   - (CheckoutResponse, nil, nil) when Wait=false or the poll timed out
//     without the balance increasing — caller renders an "open in browser
//     to complete" / "didn't land in time" hint. Polling timeout is a soft
//     failure: the session may simply not have been completed yet, so we
//     don't surface it as an error.
//   - (nil, nil, err) on hard failures (auth, network, etc.).
func Topup(ctx context.Context, c Clienter, opts TopupOptions) (*client.CheckoutResponse, *client.BillingResponse, error) {
	if opts.AmountUSD <= 0 {
		return nil, nil, fmt.Errorf("amount must be positive USD")
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 3 * time.Second
	}
	if opts.PollTimeout == 0 {
		opts.PollTimeout = 5 * time.Minute
	}

	// Pre-topup balance is only used as the floor for the post-poll check;
	// skipping it under --no-wait removes a network round-trip and a failure
	// point we don't need (the GetBilling endpoint failing shouldn't block
	// a user who explicitly asked us not to wait).
	var pre *client.BillingResponse
	if opts.Wait {
		got, err := c.GetBilling(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("read pre-topup balance: %w", err)
		}
		pre = got
	}

	// Stable idempotency key across retries. The classic scenario the key
	// is meant to protect is: server accepts POST, response drops on the
	// wire, user re-runs the command. With a fresh-random key per call,
	// the second run creates a second Stripe session and the customer can
	// pay both — a duplicate charge. So we stash the key on disk, scoped
	// by amount, with a short TTL; same amount within the window reuses
	// the same key, after which Stripe-side dedup kicks in.
	pendingPath := opts.PendingPath
	if pendingPath == "" {
		p, err := defaultPendingPath()
		if err != nil {
			return nil, nil, fmt.Errorf("resolve pending-topup path: %w", err)
		}
		pendingPath = p
	}

	idemKey, err := obtainIdempotencyKey(pendingPath, opts.AmountUSD, time.Now())
	if err != nil {
		return nil, nil, err
	}

	co, err := c.CreateCheckout(ctx, opts.AmountUSD, idemKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create checkout session: %w", err)
	}

	if opts.Stdout != nil {
		fmt.Fprintf(opts.Stdout, "checkout URL: %s\n", co.CheckoutURL)
	}

	if opts.Open {
		// Best-effort browser launch. Failures here don't propagate — the
		// URL was already printed to stdout, so the user can open it
		// manually if the OS has no default browser.
		_ = openInBrowser(co.CheckoutURL)
	}

	if !opts.Wait {
		// Fire-and-forget: the user explicitly opted out of waiting, so
		// they're not in a position to retry on network drop anyway. Clear
		// the pending entry so a subsequent intentional same-amount topup
		// within the TTL doesn't unintentionally reuse this key (Stripe
		// would return the existing session, blocking the second charge).
		_ = clearPendingTopup(pendingPath)
		return co, nil, nil
	}

	// pre is non-nil here because Wait is true (checked above).
	deadline := time.Now().Add(opts.PollTimeout)
	expected := pre.BalanceCents + int64(opts.AmountUSD*100)
	for {
		select {
		case <-ctx.Done():
			return co, nil, ctx.Err()
		case <-time.After(opts.PollInterval):
		}
		now, err := c.GetBilling(ctx)
		if err != nil {
			return co, nil, fmt.Errorf("poll balance: %w", err)
		}
		if now.BalanceCents >= expected {
			// Topup landed; clear the pending entry so the next topup of
			// the same amount creates a fresh session. Best-effort: a
			// failure here doesn't fail the topup (worst case, a second
			// topup within the TTL would dedupe — but the user just saw
			// the first one succeed so they know to wait or use a
			// different amount).
			_ = clearPendingTopup(pendingPath)
			return co, now, nil
		}
		if time.Now().After(deadline) {
			// Soft failure: session may simply not have been completed yet.
			// Returning a non-nil BillingResponse here would let callers
			// mistake the unchanged balance for a successful topup, so
			// signal timeout by returning nil for the post-balance.
			//
			// Concurrent-usage mitigation: if the balance moved at all
			// (now > pre), the topup almost certainly landed but ongoing
			// usage debits offset the increase below `expected`. Clear
			// the pending entry in that case so a legitimate retry isn't
			// blocked for the full TTL. If balance stayed flat, keep the
			// entry so a network-drop retry still dedupes.
			if now.BalanceCents > pre.BalanceCents {
				_ = clearPendingTopup(pendingPath)
			}
			return co, nil, nil
		}
	}
}

// obtainIdempotencyKey returns a stable idempotency key for this topup
// attempt. If a non-expired pending entry exists for the same amount,
// reuses its key; otherwise generates a fresh one and stashes it.
//
// `now` is injected so tests can simulate TTL expiry without time.Sleep.
func obtainIdempotencyKey(path string, amountUSD int, now time.Time) (string, error) {
	if existing, ok := loadPendingTopup(path); ok {
		if existing.AmountUSD == amountUSD && now.Sub(existing.StartedAt) < pendingTopupTTL {
			return existing.IdempotencyKey, nil
		}
		// Either amount changed or TTL expired; the existing entry is
		// stale. Overwrite below. (We don't try to dedupe across amounts —
		// a different amount means the user wants a different topup.)
	}
	idemBytes := make([]byte, 16)
	if _, err := rand.Read(idemBytes); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	key := "hopnet-cli-" + hex.EncodeToString(idemBytes)
	if err := savePendingTopup(path, &pendingTopup{
		IdempotencyKey: key, AmountUSD: amountUSD, StartedAt: now,
	}); err != nil {
		return "", fmt.Errorf("save pending topup: %w", err)
	}
	return key, nil
}

func defaultPendingPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hopnet", "topup-pending.json"), nil
}

func loadPendingTopup(path string) (*pendingTopup, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		// Missing file is the common case (no in-flight topup); both
		// missing and unreadable degrade to "generate a fresh key" so
		// the user is never blocked.
		return nil, false
	}
	var p pendingTopup
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, false
	}
	if p.IdempotencyKey == "" {
		return nil, false
	}
	return &p, true
}

func savePendingTopup(path string, p *pendingTopup) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// Atomic write: tmp + rename. Same pattern as internal/config.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".topup-pending.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func clearPendingTopup(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// openInBrowser dispatches the platform-appropriate "open this URL" command.
// Errors are silenced by the caller — if it fails, the URL was already printed.
func openInBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	return exec.Command(cmd, args...).Start()
}
