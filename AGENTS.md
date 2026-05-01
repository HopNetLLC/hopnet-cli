# Agent Guidelines for hopnet-cli

This is the customer-distributed CLI for HopNet. The canonical product strategy, design docs, and working-process rules live in the **planning repo** at `~/projects/hopnet/`. The deployable system (control-api + proxy-gateway + Compose + provisioning) lives in the sibling `hopnet-alpha` repo.

Read these before non-trivial changes:

- `~/projects/hopnet/docs/one-week-mvp-design.md` §`cli` — required commands and their behavior.
- `~/projects/hopnet/docs/one-week-mvp-design.md` §`local-route-bridge` — the in-process bridge that ships inside this binary (P10).
- `~/projects/hopnet/docs/working-process.md` — durable process norms.
- `~/.claude/plans/replicated-squishing-dolphin.md` Phase 8 + Phase 10 — implementation targets.

## Core rules

- **Single static binary.** `CGO_ENABLED=0` always. No glibc, no runtime, no installer. The customer runs `curl -L ... | tar` (or brew/scoop later) and they're done.
- **No user dependencies for runtime.** The binary must work on Alpine + glibc + macOS + Windows without anything else installed.
- **Branch + PR for code repos.** Initial commit on `main` is allowed; everything after is feature branch → PR → review → merge.
- **No `Co-Authored-By` trailers.** Direct, product-oriented commit messages.
- **Stage specific files**, not `git add .` / `-A`.
- **Sanitized logging.** Never log route tokens, API keys, or proxy URLs in any default-verbosity output. Config file at `$XDG_CONFIG_HOME/hopnet/config.json` is `chmod 600` with secrets redacted from any display.
- **Implementation sessions update the planning repo.** Worklog entries and plan updates go back to `~/projects/hopnet/notes/worklog/`.

## Stack and tooling

| Surface | Tooling |
|---|---|
| Language | Go 1.25+ |
| CLI framework | `urfave/cli/v2` |
| Testing | `go test` + `testify/require` (added in P8 if needed) |
| Local route bridge (P10) | stdlib `net/http` + `crypto/tls` |
| Config | JSON file at `$XDG_CONFIG_HOME/hopnet/config.json`, chmod 600 |
| Releases | GoReleaser (P8) → GitHub releases + brew/scoop |

## When in doubt

If this repo's behavior conflicts with the planning repo's docs, the planning repo wins. Update this repo to match, and update the planning repo only if a deliberate revision has been approved.
