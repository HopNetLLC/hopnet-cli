// Package run implements `hopnet run -- <cmd>`. It either creates a
// short-lived route on demand or reuses a route the caller already
// created, builds proxy environment variables, exec's the child process,
// forwards stdin/stdout/stderr and signals, and revokes the route when
// the child exits (unless --keep-route).
//
// The receipt is fetched from /v1/routes/:id/usage after revoke and
// printed to stderr — never stdout, so child-process pipes stay clean.
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HopNetLLC/hopnet-cli/internal/client"
	"github.com/HopNetLLC/hopnet-cli/internal/config"
)

// Options controls a single Run invocation.
type Options struct {
	// RouteID, if non-empty, reuses the supplied route. Otherwise a new
	// route is created from the Create* fields.
	RouteID string

	// Inputs for ad-hoc route creation when RouteID is empty.
	CreateRequest *client.CreateRouteRequest

	// Command + arguments to exec. Argv[0] is the program; Argv[1:] are
	// passed positionally.
	Argv []string

	// KeepRoute, when true, leaves the route alive after the child exits.
	// Has no effect on routes that were not created by this invocation.
	KeepRoute bool

	// Stdin/Stdout/Stderr default to the parent process's standard
	// streams when nil (production). Tests override them.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Env is the parent environment to inherit. If nil, os.Environ() is
	// used. Tests override to keep PATH minimal.
	Env []string
}

// Result captures what happened.
type Result struct {
	RouteID    string
	Token      string
	Created    bool // we created the route in this Run (so we own revocation)
	ExitCode   int  // child exit code (or 128+signum on signal); -1 if exec never started
	Receipt    *client.Usage
	RevokeErr  error // non-nil if revoke failed; informational only
	ReceiptErr error // non-nil if receipt fetch failed; informational only
}

// proxyURLForRoute builds a credentials-embedded proxy URL.
// proxyBase is something like "https://proxy.hopnet.io:443"; we splice
// the route id + token into the userinfo of that URL.
func proxyURLForRoute(proxyBase, routeID, token string) (string, error) {
	u, err := url.Parse(proxyBase)
	if err != nil {
		return "", fmt.Errorf("parse proxy_url: %w", err)
	}
	u.User = url.UserPassword(routeID, token)
	return u.String(), nil
}

// buildEnv returns the child env: parent env minus any HOPNET_ROUTE_*
// previously set, plus our injected proxy + bookkeeping vars.
func buildEnv(parent []string, proxyURL, routeID, token string) []string {
	out := make([]string, 0, len(parent)+8)
	suppressed := map[string]bool{
		"HTTPS_PROXY":        true,
		"HTTP_PROXY":         true,
		"ALL_PROXY":          true,
		"NO_PROXY":           true,
		"HOPNET_ROUTE_ID":    true,
		"HOPNET_ROUTE_TOKEN": true,
		"https_proxy":        true,
		"http_proxy":         true,
		"all_proxy":          true,
		"no_proxy":           true,
	}
	for _, kv := range parent {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if suppressed[kv[:eq]] {
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"HTTPS_PROXY="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"ALL_PROXY="+proxyURL,
		"NO_PROXY=localhost,127.0.0.1,::1",
		"HOPNET_ROUTE_ID="+routeID,
		"HOPNET_ROUTE_TOKEN="+token,
	)
	return out
}

// Run executes the requested command and revokes the route afterwards
// (unless KeepRoute is set on a route Run created itself, OR the caller
// passed a RouteID — in which case the route's lifecycle is the
// caller's, not ours).
func Run(ctx context.Context, c *client.Client, cfg *config.Config, opts Options) (*Result, error) {
	if len(opts.Argv) == 0 {
		return nil, errors.New("no command supplied (use `hopnet run -- <cmd>`)")
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Env == nil {
		opts.Env = os.Environ()
	}

	res := &Result{ExitCode: -1}

	// Either reuse the route or create one.
	if opts.RouteID != "" {
		r, ok := cfg.GetRoute(opts.RouteID)
		if !ok {
			return res, fmt.Errorf("route %s not found in local cache (was it created by this CLI on this host?)", opts.RouteID)
		}
		res.RouteID = opts.RouteID
		res.Token = r.Token
	} else {
		if opts.CreateRequest == nil {
			return res, errors.New("internal: CreateRequest is nil and RouteID is empty")
		}
		created, err := c.CreateRoute(ctx, opts.CreateRequest)
		if err != nil {
			return res, fmt.Errorf("create route: %w", err)
		}
		res.RouteID = created.ID
		res.Token = created.Token
		res.Created = true
		// Persist immediately so a crash mid-run still leaves a usable
		// cache entry for receipt/revoke.
		cfg.PutRoute(created.ID, config.Route{
			Token:        created.Token,
			CreatedAt:    time.Now().UTC(),
			ExpiresAt:    created.ExpiresAt,
			Label:        opts.CreateRequest.Label,
			RouteClass:   created.RouteClass,
			Country:      created.Country,
			MaxBytes:     created.MaxBytes,
			RouteVersion: created.RouteVersion,
		})
		if err := cfg.Save(); err != nil {
			return res, fmt.Errorf("save config after route create: %w", err)
		}
	}

	// Track whether we've started the child. Until exec begins, any
	// error path that affects a self-created route must trigger a
	// best-effort revoke so we don't leak a billable route on a setup
	// failure (e.g. proxyURLForRoute below).
	execStarted := false
	defer func() {
		if execStarted || !res.Created || opts.KeepRoute {
			return
		}
		revokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.DeleteRoute(revokeCtx, res.RouteID); err != nil &&
			!errors.Is(err, client.ErrNotFound) {
			res.RevokeErr = err
		} else {
			cfg.DeleteRoute(res.RouteID)
			_ = cfg.Save()
		}
	}()

	proxyURL, err := proxyURLForRoute(cfg.ProxyURL, res.RouteID, res.Token)
	if err != nil {
		return res, err
	}

	// Exec the child.
	execStarted = true
	res.ExitCode = execChild(opts, buildEnv(opts.Env, proxyURL, res.RouteID, res.Token))

	// Revoke unless we're explicitly keeping the route AND we created it.
	// If the user supplied --route, the route belongs to them; we never
	// revoke routes the caller didn't ask us to create.
	if res.Created && !opts.KeepRoute {
		// Use a fresh detached context with a short timeout so we
		// still revoke when the parent ctx was cancelled by SIGINT.
		revokeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.DeleteRoute(revokeCtx, res.RouteID); err != nil &&
			!errors.Is(err, client.ErrNotFound) {
			res.RevokeErr = err
		} else {
			cfg.DeleteRoute(res.RouteID)
			_ = cfg.Save()
		}
	}

	// Best-effort receipt. Use the same detached context.
	receiptCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if usage, err := c.GetUsage(receiptCtx, res.RouteID); err == nil {
		res.Receipt = usage
	} else {
		res.ReceiptErr = err
	}
	return res, nil
}

// execChild starts the child with the supplied env, forwards signals, and
// returns the exit code. If the child is killed by a signal, the
// returned code is 128+signum (Unix convention).
//
// Note: exec.Command is used here, NOT exec.CommandContext. The parent's
// ctx is canceled by SIGINT (signal.NotifyContext in main), and Go's
// CommandContext default-kills the child with SIGKILL on cancel — which
// would race with our explicit forwarder and produce exit 137 instead
// of 128+SIGINT. We drive child termination only through the explicit
// signal forwarder below.
func execChild(opts Options, env []string) int {
	cmd := exec.Command(opts.Argv[0], opts.Argv[1:]...)
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Env = env

	// Put the child in its own process group so the terminal's SIGINT
	// (delivered to our PG only) doesn't double-hit the child; we
	// forward it explicitly below so the child's whole subtree sees it.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(opts.Stderr, "hopnet: failed to start %s: %v\n", opts.Argv[0], err)
		return 127
	}

	// Forward SIGINT/SIGTERM/SIGHUP. Second signal escalates to SIGKILL.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	// `exited` is set BEFORE doneCh receives so the signal handler can
	// short-circuit kills against a freshly-reaped PID/PGID. The race
	// window between `cmd.Wait()` returning and `exited.Store(true)` is
	// a few CPU instructions; far smaller than the unguarded window.
	var exited atomic.Bool
	doneCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		exited.Store(true)
		doneCh <- err
	}()

	signalsReceived := 0
	for {
		select {
		case sig := <-sigCh:
			if exited.Load() {
				continue
			}
			signalsReceived++
			if signalsReceived >= 2 {
				if !exited.Load() {
					_ = cmd.Process.Kill()
				}
				continue
			}
			// Forward via process group so child + descendants see it.
			// Re-check `exited` between Getpgid and Kill to keep the
			// PID-reuse window as small as possible.
			pgid, err := syscall.Getpgid(cmd.Process.Pid)
			if err == nil && !exited.Load() {
				_ = syscall.Kill(-pgid, sig.(syscall.Signal))
			} else if err != nil && !exited.Load() {
				_ = cmd.Process.Signal(sig)
			}
		case err := <-doneCh:
			return mapExitCode(err)
		}
	}
}

func mapExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		ws, ok := ee.Sys().(syscall.WaitStatus)
		if ok {
			if ws.Signaled() {
				return 128 + int(ws.Signal())
			}
			return ws.ExitStatus()
		}
		return ee.ExitCode()
	}
	return 1
}
