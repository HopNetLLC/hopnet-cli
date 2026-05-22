# hopnet CLI

Single Go binary that drives HopNet from the developer's machine. Wraps the control API and includes the in-process local route bridge for browser sessions.

The CLI is the customer-distributed artifact. The deployable system (control-api, admin-api, proxy-gateway, customer + admin web) lives in per-service sibling repos under `HopNetLLC/`. Strategy, architecture truth, and forward-looking design live in the planning repo at `~/projects/hopnet/`.

## Tech Stack

- **Go 1.25+** — single statically-linked binary, no runtime dependencies.
- `urfave/cli/v2` — command surface.
- Cross-compiled to `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- Distroless container image for CI/Docker users (`gcr.io/distroless/static-debian12`).

## Quick Start

### Install

```bash
# Linux + macOS — no sudo, installs to $HOME/.local/bin
curl -fsSL https://raw.githubusercontent.com/HopNetLLC/hopnet-cli/main/install.sh | sh

# Pin a specific version (recommended for CI to avoid the unauthenticated
# 60/hr/IP GitHub releases API rate limit)
curl -fsSL https://raw.githubusercontent.com/HopNetLLC/hopnet-cli/main/install.sh | HOPNET_VERSION=v0.1.0 sh

# Custom install directory
curl -fsSL https://raw.githubusercontent.com/HopNetLLC/hopnet-cli/main/install.sh | HOPNET_INSTALL_DIR=/usr/local/bin sh
```

Or build from source:

```bash
go build -o ./bin/hopnet ./cmd/hopnet
```

### Use

```bash
# Authenticate against production (default base/proxy URLs)
hopnet auth login --api-key hn_live_...

# Or stash via 1Password
op read 'op://Private/HopNet API/key' | hopnet auth login

# Create a route and run a command through it
hopnet route create --ttl 5m --max-mb 50 --class direct --allow example.com
hopnet run --ttl 5m --class direct --allow example.com -- curl https://example.com

# List, inspect, revoke
hopnet route list
hopnet route usage rt_xxxxxxxx
hopnet receipt rt_xxxxxxxx
hopnet route delete rt_xxxxxxxx
```

Default endpoints are production:
- `base_url`  = `https://api.hopnet.io`
- `proxy_url` = `https://proxy.hopnet.io:443`

Override per-host via `auth login --base-url ... --proxy-url ...` for staging or local stacks.

## Configuration

State lives at `$XDG_CONFIG_HOME/hopnet/config.json` (default `~/.config/hopnet/config.json`), `chmod 600`. The file holds:

- `api_key` — the `hn_live_...` token.
- `base_url` — control-api endpoint.
- `proxy_url` — HopNet TLS-CONNECT proxy endpoint.
- `routes` — local cache of route ids → tokens. Required so `hopnet env` and `hopnet run --route` can reconstruct proxy credentials after the fact (the API only returns a route token at creation time).

Override the path with `--config <file>` or `HOPNET_CONFIG`. Set `HOPNET_DEBUG=1` for slog debug output (sensitive fields are redacted).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic CLI error (bad flag, malformed response) |
| 2 | Auth failure (401) |
| 3 | Insufficient credit (402) |
| 4 | Not found (404) |
| 5 | Server error (5xx) |
| 6 | `bridge` not yet implemented |
| _child_ | `hopnet run --` propagates the child's exit code (or 128+signum on signal) |

## Build

```bash
go build -o ./bin/hopnet ./cmd/hopnet
./bin/hopnet version
```

Cross-compile matrix:

```bash
GOOS=linux  GOARCH=amd64 go build -o ./bin/hopnet-linux-amd64  ./cmd/hopnet
GOOS=linux  GOARCH=arm64 go build -o ./bin/hopnet-linux-arm64  ./cmd/hopnet
GOOS=darwin GOARCH=amd64 go build -o ./bin/hopnet-darwin-amd64 ./cmd/hopnet
GOOS=darwin GOARCH=arm64 go build -o ./bin/hopnet-darwin-arm64 ./cmd/hopnet
```

## Scripts

```bash
go vet ./...
go test ./... -race -count=1
```

## Docker

```bash
docker build -t ghcr.io/hopnetllc/hopnet-cli .
docker run --rm ghcr.io/hopnetllc/hopnet-cli version
```

## Project Structure

```
cmd/hopnet/        Binary entry point (urfave/cli/v2 wiring + per-command files)
internal/auth/     auth login flow + key validation
internal/client/   typed HTTP client for control-api
internal/config/   $XDG_CONFIG_HOME/hopnet/config.json round-trip + route cache
internal/run/      child-process exec + signal forwarding + receipt
internal/bridge/   stub package — see `hopnet bridge` exit code 6
internal/billing/  Stripe Checkout topup orchestration + idempotency-key stash
internal/redact/   slog ReplaceAttr masker for tokens/keys/passwords
tests/             smoke.sh (built-binary exercise; no network deps)
.github/           CI + release workflows
```

## Telemetry

`log/slog` JSON to stderr, `LevelInfo` by default and `LevelDebug` when `HOPNET_DEBUG=1`. Sensitive attributes (any key matching `api_key`, `token`, `authorization`, `bearer`, `password`, `secret`, `pepper`) are masked by `internal/redact`.

## Testing

```bash
go vet ./...
go test ./... -race -count=1
bash tests/smoke.sh
```

`tests/smoke.sh` is a network-free built-binary exercise: builds the binary, verifies the command surface, error codes, config-file mode, and the bridge stub. Cross-service integration against a live control-api stack is a tracked future-work item in the planning repo.

## Releases

Tagged releases are built via [GoReleaser](https://goreleaser.com) on push of a `v*` tag (`.github/workflows/release.yml`). Local snapshot validation:

```bash
goreleaser release --snapshot --clean
```
