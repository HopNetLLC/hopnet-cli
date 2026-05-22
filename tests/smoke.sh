#!/usr/bin/env bash
# tests/smoke.sh — built-binary exercise for hopnet-cli.
#
# The CLI is a tool, not a service. The scope here is "does the built
# binary produce the right exit codes, write a 0600 config file, and
# preserve the documented error surface?" — no Docker, no network, no
# sibling-service dependency. Cross-service integration (CLI → live
# control-api → proxy-gateway) is a tracked planning-repo backlog
# item; it needs admin-api session bootstrap which Codex moved
# behind Auth.js in P15.
#
# Local: `bash tests/smoke.sh`.
# CI:    `.github/workflows/smoke.yml` runs this on every PR + push.

set -euo pipefail

cd "$(dirname "$0")/.."

TMP="$(mktemp -d)"
export XDG_CONFIG_HOME="$TMP/.config"
trap 'rm -rf "$TMP"' EXIT

HOPNET="$TMP/hopnet"
CFG_DIR="$XDG_CONFIG_HOME/hopnet"
CFG_PATH="$CFG_DIR/config.json"

# Bogus endpoints: the smoke must never hit the live network. Port 1 is
# reliably-closed and produces an immediate connection-refused that maps
# to the CLI's generic exit (1).
DEAD_BASE="http://127.0.0.1:1"
DEAD_PROXY="http://127.0.0.1:2"

# ---------- Build ----------
echo "==> build CGO_ENABLED=0 ./cmd/hopnet"
CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=smoke -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo dev)" \
  -o "$HOPNET" ./cmd/hopnet

# ---------- Phase 1: version + help ----------
echo "==> hopnet version"
"$HOPNET" version | grep -q "^hopnet smoke" || {
  echo "FAIL: version output missing 'hopnet smoke' prefix" >&2; exit 1; }

echo "==> hopnet --help lists every documented subcommand"
help_out=$("$HOPNET" --help)
for sub in auth route env run bridge receipt billing version; do
  echo "$help_out" | grep -qE "^[[:space:]]*$sub" || {
    echo "FAIL: --help missing subcommand: $sub" >&2
    echo "$help_out" >&2; exit 1; }
done

# ---------- Phase 2: auth login --skip-verify writes 0600 config ----------
echo "==> hopnet auth login --skip-verify"
"$HOPNET" auth login \
  --skip-verify \
  --api-key hn_live_smoke_test_key_abcdef \
  --base-url "$DEAD_BASE" \
  --proxy-url "$DEAD_PROXY"

[[ -f "$CFG_PATH" ]] || { echo "FAIL: config not written at $CFG_PATH" >&2; exit 1; }

mode=$(stat -c '%a' "$CFG_PATH" 2>/dev/null || stat -f '%A' "$CFG_PATH")
[[ "$mode" == "600" ]] || { echo "FAIL: config mode is $mode, expected 600" >&2; exit 1; }
echo "    config mode 600 (ok)"

dir_mode=$(stat -c '%a' "$CFG_DIR" 2>/dev/null || stat -f '%A' "$CFG_DIR")
[[ "$dir_mode" == "700" ]] || { echo "FAIL: config dir mode is $dir_mode, expected 700" >&2; exit 1; }
echo "    config dir mode 700 (ok)"

# ---------- Phase 3: bad API-key format is rejected pre-network ----------
echo "==> auth login --api-key short → format-validation error"
set +e
"$HOPNET" auth login --skip-verify --api-key short_key 2>"$TMP"/smoke-badkey.err
bad_key_code=$?
set -e
[[ "$bad_key_code" -ne 0 ]] || {
  echo "FAIL: short api key was accepted (should fail format check)" >&2; exit 1; }
grep -q "hn_live_" "$TMP"/smoke-badkey.err || {
  echo "FAIL: error did not mention required key prefix" >&2
  cat "$TMP"/smoke-badkey.err >&2; exit 1; }

# ---------- Phase 4: hopnet env on uncached route → generic error ----------
echo "==> hopnet env rt_uncached → exit != 0 (not in local cache)"
set +e
"$HOPNET" env rt_nonexistent_route_id 2>"$TMP"/smoke-env.err
env_code=$?
set -e
[[ "$env_code" -ne 0 ]] || {
  echo "FAIL: env returned 0 for uncached route" >&2; exit 1; }
grep -q "local cache" "$TMP"/smoke-env.err || {
  echo "FAIL: env error did not mention local cache" >&2
  cat "$TMP"/smoke-env.err >&2; exit 1; }

# ---------- Phase 5: hopnet bridge with cached route → exit 6 ----------
# Seed the route cache directly so bridge's lookup succeeds and we hit
# the ErrNotImplemented path. This is the documented exit-6 contract.
echo "==> seed route cache and call hopnet bridge → exit 6"
python3 - "$CFG_PATH" <<'PY'
import json, sys
p = sys.argv[1]
with open(p) as f:
    cfg = json.load(f)
cfg.setdefault("routes", {})["rt_smoke_seed_route"] = {
    "token": "rtk_smoke_seed_token_for_bridge_exit_6_check",
    "created_at": "2026-01-01T00:00:00Z",
    "expires_at": "2099-01-01T00:00:00Z",
    "route_class": "direct",
    "route_version": 1,
}
with open(p, "w") as f:
    json.dump(cfg, f, indent=2)
PY

set +e
"$HOPNET" bridge --route rt_smoke_seed_route --listen 127.0.0.1:0 2>"$TMP"/smoke-bridge.err
bridge_code=$?
set -e
[[ "$bridge_code" == "6" ]] || {
  echo "FAIL: bridge returned $bridge_code, expected 6 (not implemented)" >&2
  cat "$TMP"/smoke-bridge.err >&2; exit 1; }
echo "    bridge exit 6 (ok)"

# ---------- Phase 6: route create against dead endpoint → non-zero ----------
echo "==> hopnet route create against dead endpoint → exit != 0"
set +e
"$HOPNET" route create --ttl 1m --class direct >"$TMP"/smoke-create.out 2>&1
create_code=$?
set -e
[[ "$create_code" -ne 0 ]] || {
  echo "FAIL: route create against $DEAD_BASE returned 0" >&2
  cat "$TMP"/smoke-create.out >&2; exit 1; }

# ---------- Phase 7: subcommands match documented surface ----------
# Defense in depth against accidental command drop or rename. The unit
# test TestCommandTreeMatchesSpec checks the same invariant, but only at
# the urfave/cli wiring level — the built binary check catches a missing
# subcommand registration on the entry-point side.
echo "==> hopnet route --help has create/list/usage/delete"
route_help=$("$HOPNET" route --help)
for sub in create list usage delete; do
  echo "$route_help" | grep -qE "^[[:space:]]*$sub" || {
    echo "FAIL: route --help missing: $sub" >&2; exit 1; }
done

echo "==> hopnet billing --help has topup/balance/history"
billing_help=$("$HOPNET" billing --help)
for sub in topup balance history; do
  echo "$billing_help" | grep -qE "^[[:space:]]*$sub" || {
    echo "FAIL: billing --help missing: $sub" >&2; exit 1; }
done

# ---------- Phase 8: completion subcommand emits hopnet-named scripts ----------
echo "==> hopnet completion {bash,zsh,fish} emits non-empty hopnet-named scripts"
for shell in bash zsh fish; do
  out=$("$HOPNET" completion "$shell")
  [ -n "$out" ] || { echo "FAIL: completion $shell empty" >&2; exit 1; }
  echo "$out" | grep -q "hopnet" || {
    echo "FAIL: completion $shell does not reference 'hopnet'" >&2; exit 1; }
done
set +e
"$HOPNET" completion 2>"$TMP"/smoke-completion.err
comp_code=$?
set -e
[ "$comp_code" -ne 0 ] || {
  echo "FAIL: bare 'completion' returned 0; expected usage error" >&2; exit 1; }

echo
echo "OK — all hopnet-cli smoke assertions passed."
