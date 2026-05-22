# Agent Guidelines for hopnet-cli

This is the customer-distributed CLI for HopNet. The canonical product strategy, architecture truth, and working-process rules live in the **planning repo** at `~/projects/hopnet/`. Per-service deployable systems live in sibling repos under `HopNetLLC/` (control-api, admin-api, proxy-gateway, web, admin, egress, schema, infra).

Read these before non-trivial changes:

- `~/projects/hopnet/docs/architecture.md` §CLI — current command surface + exit codes + idempotency rules.
- `~/projects/hopnet/docs/working-process.md` — durable process norms (commits, PRs, reviews, cross-repo coordination).
- `~/projects/hopnet/notes/future-work.md` — open backlog items that touch this repo (bridge body, browser launch, fare-scout template, estimated_cost_cents drop, Dockerfile cross-compile refactor).

## Core rules

- **Single static binary.** `CGO_ENABLED=0` always. No glibc, no runtime, no installer. The customer runs `curl -L ... | tar` (or brew/scoop later) and they're done.
- **No user dependencies for runtime.** The binary must work on Alpine + glibc + macOS + Windows without anything else installed.
- **Branch + PR for code repos.** Initial commit on `main` is allowed; everything after is feature branch → PR → review → squash-merge → delete branch.
- **No `Co-Authored-By` trailers.** Direct, product-oriented commit messages.
- **Stage specific files**, not `git add .` / `-A`.
- **Sanitized logging.** Never log route tokens, API keys, or proxy URLs in any default-verbosity output. Config file at `$XDG_CONFIG_HOME/hopnet/config.json` is `chmod 600` with secrets redacted from any display.
- **Server response fields the CLI doesn't read are not errors.** Go unmarshal ignores unknown JSON keys; field additions on the control-api side do not require a CLI release. Field renames or removals DO require coordination — track in the planning-repo backlog.
- **No solo doc-only PRs.** One-line doc fixes ride the next feature PR.

## Stack and tooling

| Surface | Tooling |
|---|---|
| Language | Go 1.25+ |
| CLI framework | `urfave/cli/v2` |
| Testing | `go test ./... -race` + `tests/smoke.sh` (built-binary exercise) |
| Config | JSON file at `$XDG_CONFIG_HOME/hopnet/config.json`, chmod 600 |
| Releases | GoReleaser → GitHub releases (linux/darwin × amd64/arm64) |

## When in doubt

If this repo's behavior conflicts with the planning repo's architecture or working-process docs, the planning repo wins. Update this repo to match, and update the planning repo only if a deliberate revision has been approved.
