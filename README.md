# hopnet CLI

Single Go binary that drives HopNet from the developer's machine. Wraps the control API and includes the in-process local route bridge for browser sessions.

The CLI is the customer-distributed artifact. The deployable system (control-api + proxy-gateway + Compose + provisioning) lives in the sibling [`hopnet-alpha`](../hopnet-alpha/) repo. Strategy and design docs live in the planning repo at `~/projects/hopnet/`.

## Tech Stack

- **Go 1.25+** — single statically-linked binary, no runtime dependencies.
- `urfave/cli/v2` — command surface.
- Cross-compiled to `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- Distroless container image for CI/Docker users (`gcr.io/distroless/static-debian12`).

Phase 8 ships the full command surface: `auth login`, `route create/list/usage/delete`, `env`, `run`, `bridge` (P10 stub), and `receipt`. Phase 10 will add `browser launch` and `template fare-scout`.

## Quick Start

```bash
# Build
go build -o ./bin/hopnet ./cmd/hopnet

# Authenticate (key minted out-of-band by an admin)
./bin/hopnet auth login --api-key hn_live_...

# Or, stash it via 1Password
op read 'op://Private/HopNet API/key' | ./bin/hopnet auth login

# Create a route and run a command through it
./bin/hopnet route create --ttl 5m --max-mb 50 --class direct --allow example.com
./bin/hopnet run --ttl 5m --class direct --allow example.com -- curl https://example.com

# List, inspect, revoke
./bin/hopnet route list
./bin/hopnet route usage rt_xxxxxxxx
./bin/hopnet receipt rt_xxxxxxxx
./bin/hopnet route delete rt_xxxxxxxx
```

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
| 6 | `bridge` not yet implemented (P10) |
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
internal/bridge/   P8 stub; full body in P10
internal/redact/   slog ReplaceAttr masker for tokens/keys/passwords
tests/             integration.sh (runs against sibling hopnet-alpha stack)
.github/           CI + release workflows
```

## Telemetry

`log/slog` JSON to stderr, `LevelInfo` by default and `LevelDebug` when `HOPNET_DEBUG=1`. Sensitive attributes (any key matching `api_key`, `token`, `authorization`, `bearer`, `password`, `secret`, `pepper`) are masked by `internal/redact`. Server-pushed metrics emission is deferred to P11/P12.

## Testing

```bash
go vet ./...
go test ./... -race -count=1
```

End-to-end integration runs against a live `hopnet-alpha` Compose stack:

```bash
# In hopnet-alpha:
docker compose --profile all --profile test up -d --build

# In hopnet-cli:
bash tests/integration.sh
# or, exercise the live tunnel (requires curl with --proxy-insecure for the dev cert):
HOPNET_RUN_LIVE=1 bash tests/integration.sh
```

## Releases

Tagged releases are built via [GoReleaser](https://goreleaser.com). Local snapshot validation:

```bash
goreleaser release --snapshot --clean
```

## License

MIT — see `LICENSE`.
