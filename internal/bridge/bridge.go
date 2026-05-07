// Package bridge will (in P10) host an in-process local-route-bridge
// that translates loopback HTTP/HTTPS requests into authenticated
// CONNECT calls against the upstream HopNet proxy. P8 ships only the
// command-line plumbing — the bridge body itself is deferred.
package bridge

import (
	"errors"
	"fmt"

	"github.com/HopNetLLC/hopnet-cli/internal/config"
)

// ErrNotImplemented is returned by Run until the body lands in P10.
var ErrNotImplemented = errors.New("bridge not yet implemented (lands in Phase 10)")

// Options is the parsed flag set for `hopnet bridge`. The fields here
// are what P10 will need; we capture them now so flag parsing and
// route lookup are real even though the listener is not.
type Options struct {
	RouteID string
	Listen  string // e.g. "127.0.0.1:0"
}

// Run does the route lookup so the user gets a clear error if their
// route id is unknown, then returns ErrNotImplemented. The lookup is
// real plumbing that P10 will keep.
func Run(cfg *config.Config, opts Options) error {
	if opts.RouteID == "" {
		return fmt.Errorf("--route is required")
	}
	if opts.Listen == "" {
		return fmt.Errorf("--listen is required (e.g. 127.0.0.1:0)")
	}
	if _, ok := cfg.GetRoute(opts.RouteID); !ok {
		return fmt.Errorf("route %s not found in local cache", opts.RouteID)
	}
	return ErrNotImplemented
}
