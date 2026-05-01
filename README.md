# hopnet CLI

Single Go binary that drives HopNet from the developer's machine. Wraps the control API and includes the in-process local route bridge for browser sessions.

The CLI is the customer-distributed artifact. The deployable system (control-api + proxy-gateway + Compose + provisioning) lives in the sibling [`hopnet-alpha`](../hopnet-alpha/) repo. Strategy and design docs live in the planning repo at `~/projects/hopnet/`.

## Tech Stack

- **Go 1.25+** — single statically-linked binary, no runtime dependencies.
- `urfave/cli/v2` — command surface.
- Cross-compiled to `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- Distroless container image for CI/Docker users (`gcr.io/distroless/static-debian12`).

Phase 1 ships only `hopnet version`. Phase 8 of the implementation plan (`~/.claude/plans/replicated-squishing-dolphin.md`) lands the full surface (`auth login`, `route create/list/usage/delete`, `env`, `run`, `bridge`, `receipt`). Phase 10 adds `browser launch` + `template fare-scout`.

## Quick Start

```bash
go run ./cmd/hopnet
go run ./cmd/hopnet version
```

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
cmd/hopnet/      Binary entry point (main package)
internal/        (added in P8: auth, config, bridge, run helpers)
.github/         CI + release workflows
```

## Telemetry

The CLI emits anonymous tool-usage telemetry to the control-api when configured. Sanitized: never includes route tokens, API keys, or payload data. Disabled by default until the user explicitly opts in via `hopnet auth login`.

## Testing

Unit tests cover flag parsing, config-file handling, and pure-Go logic. Integration tests against a running `hopnet-alpha` stack are in the sibling repo's `tests/smoke.sh` (P8 will add CLI-specific assertions there).

## License

MIT — see `LICENSE`.
