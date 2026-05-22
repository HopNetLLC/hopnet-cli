# Getting started with HopNet

This guide takes you from zero to running real traffic through a HopNet route in under five minutes.

## What HopNet is

HopNet gives you **disposable, task-scoped network routes**. A route is a short-lived proxy credential pinned to your choice of country, route class, byte cap, time-to-live, and destination allow/deny list. You route your traffic through `proxy.hopnet.io:443`, and when the route expires or you revoke it, the credentials stop working and you get a receipt.

Use cases:
- Give an AI agent a 15-minute route with a 50 MB cap that can only reach `example.com`.
- Pin a CI job to a single country code so flaky geo-tests reproduce.
- Spin up a per-task browser session with its own egress.
- Run a curl/wget/scraper through a route and read the receipt afterward.

The **route**, not your account, is the unit of pricing, policy, and audit.

## 1. Install the CLI

Pick whichever you prefer. Both install the same single static binary.

```bash
# Shell installer — no sudo, installs to $HOME/.local/bin
curl -fsSL https://raw.githubusercontent.com/HopNetLLC/hopnet-cli/main/install.sh | bash
```

```bash
# Homebrew (macOS + Linux)
brew install HopNetLLC/hopnet/hopnet
```

If `$HOME/.local/bin` isn't on your `PATH`, the installer prints the exact `export PATH=...` line to add to your shell rc.

Verify:

```bash
hopnet version
# hopnet 0.1.2 (...)
```

### CI pinning

CI users should pin the version to avoid the unauthenticated GitHub releases API rate limit (60 requests per hour per IP):

```bash
curl -fsSL https://raw.githubusercontent.com/HopNetLLC/hopnet-cli/main/install.sh | HOPNET_VERSION=v0.1.2 bash
```

## 2. Get an API key

1. Sign in at **https://app.hopnet.io** (email magic-link or Google).
2. Open the **API Keys** section of the dashboard.
3. Create a new key and copy it. It looks like `hn_live_...` and is shown to you exactly once — store it in a password manager.

Tip: keys are rate-limit-attributed to your account. Mint a separate key per machine or per integration so revoking one doesn't break the others.

## 3. Log in from the CLI

```bash
hopnet auth login --api-key hn_live_...
```

This stores your key (mode 0600) at `$XDG_CONFIG_HOME/hopnet/config.json` (default `~/.config/hopnet/config.json`) and pings `/v1/account` to verify it works. You'll see a confirmation line with your account email + current credit balance.

For 1Password users:

```bash
op read 'op://Private/HopNet API/key' | hopnet auth login
```

## 4. Verify everything is working

```bash
hopnet doctor
```

Runs four checks and exits non-zero on any failure:

```
[ok]    config       /home/you/.config/hopnet/config.json mode 0600
[ok]    api-key      format valid
[ok]    control-api  https://api.hopnet.io · you@example.com · balance $5.00
[ok]    proxy        tcp proxy.hopnet.io:443 reachable
```

Use it any time a route operation surprises you — it's faster than reading errors.

## 5. Create your first route

```bash
hopnet route create \
  --ttl 5m \
  --max-mb 50 \
  --class direct \
  --allow example.com
```

Output is a single machine-readable line:

```
rt_01jck3a5q7v8n0z8h0m1bm5x9w  rtk_4yk5x... 2026-05-22T15:05:00Z
```

That's `<route-id> <route-token> <expires-at>`. The CLI also caches the token to your local config (mode 0600) so `hopnet env <route-id>`, `hopnet run --route <route-id>`, and `hopnet route delete <route-id>` all keep working without you copying anything by hand.

If you do need the token in a script that doesn't have access to the CLI's config — for example, embedding it in CI secrets — capture it from the `route create` output line at creation time. The server returns the token **exactly once over the wire**; if you delete the local config and didn't capture the token in some other way, the route still exists but you can't reuse its credentials from a different machine. You'd create a new route in that case.

Treat `$XDG_CONFIG_HOME/hopnet/config.json` as sensitive — it holds your API key plus every active route token.

### Route flags

No flag is strictly required — `hopnet route create` with no arguments produces a 15-minute `direct`-class route with no byte/cost cap and no destination filter.

| Flag | Default | Required | What it does |
|---|---|---|---|
| `--ttl 15m` | `15m` | no | How long the route lives (max 24h) |
| `--max-mb 100` | _(no cap)_ | no | Hard byte cap; route auto-terminates when hit (max 100 GB) |
| `--max-cost-cents 50` | _(no cap)_ | no | Hard cost cap, in cents |
| `--class direct` | `direct` | no | Route class: `direct`, `datacenter`, `residential`, `free`, `fast`, `auto` |
| `--country US` | _(any)_ | no | ISO-3166 country code (depends on the class supporting it) |
| `--min-mbps 5` | _(unrestricted)_ | no | Requested minimum throughput |
| `--allow example.com` | _(all hosts allowed)_ | no | Destination allowlist (repeatable; presence enables strict filtering) |
| `--deny tracker.com` | _(none denied)_ | no | Destination denylist (repeatable) |
| `--label run-123` | _(empty)_ | no | Free-form label for your own bookkeeping |

## 6. Use the route

Three patterns:

### Pattern A — one-shot command

`hopnet run` creates a route (or reuses one with `--route rt_...`), injects `HTTPS_PROXY`/`HTTP_PROXY`/`ALL_PROXY`/`NO_PROXY` into the child process, executes the command, and revokes the route on exit (unless `--keep-route`).

```bash
hopnet run --ttl 5m --max-mb 10 --class direct --allow example.com -- \
  curl -fsSL https://example.com
```

The child's exit code propagates back. A receipt prints to stderr so child stdout pipes stay clean.

### Pattern B — eval into your shell

`hopnet env rt_...` prints `export` lines you can eval to put the proxy in your current shell:

```bash
eval $(hopnet env rt_xxxxxxxx)
curl https://example.com
playwright test --proxy-server=$HTTPS_PROXY
```

Any process started in that shell uses the route until you `unset HTTPS_PROXY` (or close the shell).

### Pattern C — point a long-lived tool at it

```bash
HTTPS_PROXY=$(hopnet env rt_xxx | grep HTTPS_PROXY | cut -d= -f2-) \
  docker run --rm -e HTTPS_PROXY ...
```

Most tools that read `HTTPS_PROXY` (curl, wget, Playwright, Puppeteer, npm, pip, go modules, etc.) work without further config.

## 7. Inspect and clean up

```bash
hopnet route list         # everything you've created
hopnet route usage rt_... # bytes, cost (estimated), per-destination breakdown
hopnet receipt rt_...     # post-revoke receipt (works after the TTL too)
hopnet route delete rt_...# revoke; established tunnels see the change + close
```

Routes auto-expire at their TTL. Receipts stay queryable after expiry.

## 8. Billing

```bash
hopnet billing balance       # current credit + last 5 ledger entries
hopnet billing history       # full ledger (paginated)
hopnet billing topup --usd 25   # opens Stripe Checkout in your browser
```

`billing topup` opens a Stripe Checkout session, optionally polls for the balance to land (default 5-minute wait), and reuses an idempotency key on retry so a network drop doesn't double-charge you. Use `--no-open` for headless boxes; `--no-wait` to fire-and-forget.

## 9. Tab completion

```bash
# bash — add to ~/.bashrc
source <(hopnet completion bash)

# zsh — add to ~/.zshrc
source <(hopnet completion zsh)

# fish — add to ~/.config/fish/config.fish
hopnet completion fish | source
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic CLI error (bad flag, malformed response) |
| 2 | Auth failure (401 — bad or revoked key) |
| 3 | Insufficient credit (402 — top up via `billing topup`) |
| 4 | Not found (404 — wrong route id, or route already revoked) |
| 5 | Server error (5xx — retry or check status) |
| 6 | `bridge` subcommand not yet implemented |
| _child_ | `hopnet run --` propagates the child's exit code (or 128+signum on signal) |

Scripts can branch on these directly — exit codes are part of the public contract.

## Troubleshooting

**"unauthorized" / exit 2** — your key is wrong or revoked. Re-mint at https://app.hopnet.io and `hopnet auth login --api-key ...` again.

**"insufficient credit" / exit 3** — top up: `hopnet billing topup --usd 25`.

**`hopnet env rt_xxx` says "not in local cache"** — the route token cache is per-host. If you created the route on a different machine, the token is gone (the server only returns tokens at creation). Create a new route on this machine, or re-run `hopnet run` with the same flags.

**`route create` hangs or times out** — run `hopnet doctor` to localize the failure. If the proxy check fails, your network is blocking outbound 443 to `proxy.hopnet.io`.

**Stripe Checkout doesn't open a browser** — pass `--no-open` and copy the URL manually. Common on headless boxes / WSL.

**Need verbose logs** — set `HOPNET_DEBUG=1`. Output goes to stderr; sensitive fields are masked.

## Where things live

| What | Where |
|---|---|
| Config + cached route tokens | `$XDG_CONFIG_HOME/hopnet/config.json` (mode 0600) |
| In-flight billing idempotency key | `$XDG_CONFIG_HOME/hopnet/topup-pending.json` (mode 0600, 30-min TTL) |
| Logs | stderr (JSON when `HOPNET_DEBUG=1`) |
| Source + releases | https://github.com/HopNetLLC/hopnet-cli |
| Homebrew formula | https://github.com/HopNetLLC/homebrew-hopnet |

## What's next

- **Help with a specific integration** (Playwright, Puppeteer, GitHub Actions, MCP server): file an issue at https://github.com/HopNetLLC/hopnet-cli/issues.
- **Source code, releases, changelog**: same repo.
- **Architecture deep dive**: read the README at the repo root.
