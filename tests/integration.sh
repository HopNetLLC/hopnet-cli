#!/usr/bin/env bash
# tests/integration.sh — end-to-end exercise of the hopnet CLI against a
# running hopnet-alpha Compose stack.
#
# Preflight (caller's responsibility):
#   1. Sibling repo checkout: ~/projects/hopnet/code/hopnet-alpha (or
#      override via $HOPNET_ALPHA_DIR).
#   2. Stack up:
#        cd $HOPNET_ALPHA_DIR
#        docker compose --profile all --profile test up -d --build
#      and healthy on $API_URL (default http://127.0.0.1:8080).
#   3. ADMIN_TOK matching the stack's HOPNET_ADMIN_TOKEN. Read from .env
#      in the sibling repo if not exported.
#
# What this script does:
#   - Mints a fresh account + API key + credit grant via the admin API
#     (using curl, just like hopnet-alpha/tests/smoke.sh).
#   - Drives the hopnet CLI: auth login, route create, env, run,
#     receipt, route list, route delete.
#   - Asserts exit codes, stdout shapes, and config-file mode.
#
# CI integration is deferred to P11; this is intended to be run on the
# dev box. Keeps the assertion vocabulary consistent with the alpha
# repo's smoke.sh.

set -euo pipefail

cd "$(dirname "$0")/.."

HOPNET_ALPHA_DIR="${HOPNET_ALPHA_DIR:-$HOME/projects/hopnet/code/hopnet-alpha}"
API_URL="${API_URL:-http://127.0.0.1:8080}"
PROXY_TLS_HOST="${PROXY_TLS_HOST:-127.0.0.1:8443}"
PROXY_URL="${PROXY_URL:-https://${PROXY_TLS_HOST}}"

# Use a throwaway config dir so we don't clobber the caller's real
# ~/.config/hopnet/config.json.
TMP_HOME="$(mktemp -d)"
export XDG_CONFIG_HOME="$TMP_HOME/.config"
trap 'rm -rf "$TMP_HOME"' EXIT
CFG_PATH="$XDG_CONFIG_HOME/hopnet/config.json"

if [[ ! -d "$HOPNET_ALPHA_DIR" ]]; then
  echo "FAIL: HOPNET_ALPHA_DIR=$HOPNET_ALPHA_DIR does not exist." >&2
  echo "      Either check out HopNetLLC/hopnet-alpha there or set HOPNET_ALPHA_DIR." >&2
  exit 1
fi

# Pull ADMIN_TOK from the sibling .env if not already set.
if [[ -z "${ADMIN_TOK:-}" && -f "$HOPNET_ALPHA_DIR/.env" ]]; then
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[[:space:]]*# || -z "${line//[[:space:]]/}" ]] && continue
    if [[ "$line" =~ ^[[:space:]]*HOPNET_ADMIN_TOKEN=(.*)$ ]]; then
      val="${BASH_REMATCH[1]}"
      val="${val%\"}"
      val="${val#\"}"
      ADMIN_TOK="$val"
      break
    fi
  done < "$HOPNET_ALPHA_DIR/.env"
fi
if [[ -z "${ADMIN_TOK:-}" ]]; then
  echo "FAIL: ADMIN_TOK not set and not found in $HOPNET_ALPHA_DIR/.env" >&2
  exit 1
fi

echo "==> waiting for control-api at $API_URL"
for _ in $(seq 1 30); do
  if curl -fsS "$API_URL/healthz" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS "$API_URL/healthz" >/dev/null || {
  echo "FAIL: control-api not responding at $API_URL" >&2; exit 1; }

echo "==> building hopnet binary"
go build -o "$TMP_HOME/hopnet" ./cmd/hopnet
HOPNET="$TMP_HOME/hopnet"

# Seed a direct upstream endpoint so route_class=direct has something to
# select. POST is idempotent on (name) — 201 first time, 409 thereafter.
echo "==> seed direct upstream endpoint (idempotent)"
seed_resp_code=$(curl -sS -o /tmp/cli-int-seed.out -w '%{http_code}' \
  -X POST "$API_URL/v1/admin/upstream-endpoints" \
  -H "X-Admin-Token: $ADMIN_TOK" -H "Content-Type: application/json" \
  -d '{"name":"direct","adapter_type":"direct","route_class":"direct","country":"*","cost_per_gb_cents":0,"priority":1}' || true)
case "$seed_resp_code" in
  201|409)
    echo "  direct upstream: $seed_resp_code" ;;
  500)
    # Duplicate-name 23505 surfaces as 500 today; treat as already-seeded.
    if grep -q '23505' /tmp/cli-int-seed.out; then
      echo "  direct upstream: 500 (duplicate, already seeded)"
    else
      echo "FAIL: seed direct upstream returned 500" >&2
      cat /tmp/cli-int-seed.out >&2; exit 1
    fi ;;
  *)
    echo "FAIL: seed direct upstream returned $seed_resp_code" >&2
    cat /tmp/cli-int-seed.out >&2; exit 1 ;;
esac

echo "==> mint account + api key + credit"
acct_resp=$(curl -fsS -X POST "$API_URL/v1/admin/accounts" \
  -H "X-Admin-Token: $ADMIN_TOK" -H "Content-Type: application/json" \
  -d "{\"email\":\"cli-int-$$@hopnet.dev\"}")
account_id=$(echo "$acct_resp" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
[[ -n "$account_id" ]] || { echo "FAIL: no account id" >&2; exit 1; }

key_resp=$(curl -fsS -X POST "$API_URL/v1/admin/api-keys" \
  -H "X-Admin-Token: $ADMIN_TOK" -H "Content-Type: application/json" \
  -d "{\"account_id\":\"$account_id\",\"name\":\"cli-integration\"}")
api_key=$(echo "$key_resp" | sed -n 's/.*"key":"\([^"]*\)".*/\1/p')
[[ -n "$api_key" ]] || { echo "FAIL: no api key" >&2; exit 1; }

idem="cli-int-$$-$RANDOM"
curl -fsS -X POST "$API_URL/v1/admin/accounts/$account_id/credits" \
  -H "X-Admin-Token: $ADMIN_TOK" -H "Content-Type: application/json" \
  -H "Idempotency-Key: $idem" \
  -d '{"amount_cents":5000,"type":"grant"}' >/dev/null

echo "==> hopnet auth login"
"$HOPNET" auth login --api-key "$api_key" --base-url "$API_URL" --proxy-url "$PROXY_URL"
[[ -f "$CFG_PATH" ]] || { echo "FAIL: config not written at $CFG_PATH" >&2; exit 1; }

mode=$(stat -c '%a' "$CFG_PATH" 2>/dev/null || stat -f '%A' "$CFG_PATH")
[[ "$mode" == "600" ]] || { echo "FAIL: config mode is $mode, expected 600" >&2; exit 1; }
echo "  config mode: $mode (ok)"

echo "==> hopnet route create"
create_out=$("$HOPNET" route create \
  --ttl 2m --max-mb 10 --class direct --allow example.com --label cli-int)
route_id=$(echo "$create_out" | awk '{print $1}')
route_token=$(echo "$create_out" | awk '{print $2}')
[[ "$route_id" == rt_* ]] || { echo "FAIL: route_id=$route_id" >&2; exit 1; }
[[ "$route_token" == rtk_* ]] || { echo "FAIL: route_token shape" >&2; exit 1; }
echo "  route: $route_id"

echo "==> hopnet env $route_id (validate export shape)"
env_out=$("$HOPNET" env "$route_id")
echo "$env_out" | grep -q "export HTTPS_PROXY=" || {
  echo "FAIL: env did not emit HTTPS_PROXY" >&2; exit 1; }
echo "$env_out" | grep -q "$route_id" || {
  echo "FAIL: env did not include route id" >&2; exit 1; }
echo "$env_out" | grep -q "$route_token" || {
  echo "FAIL: env did not include token" >&2; exit 1; }

echo "==> hopnet route list"
list_out=$("$HOPNET" route list)
echo "$list_out" | grep -q "$route_id" || {
  echo "FAIL: route list missing $route_id" >&2; exit 1; }

echo "==> hopnet receipt $route_id"
"$HOPNET" receipt "$route_id" | grep -q "$route_id" || {
  echo "FAIL: receipt missing route id" >&2; exit 1; }

echo "==> hopnet route delete (existing)"
"$HOPNET" route delete "$route_id"

echo "==> hopnet route delete (idempotent → exit 4)"
set +e
"$HOPNET" route delete "$route_id"
del_again_code=$?
set -e
[[ "$del_again_code" == "4" ]] || {
  echo "FAIL: re-delete returned $del_again_code, expected 4 (not_found)" >&2; exit 1; }

echo "==> hopnet bridge stub returns exit 6"
"$HOPNET" route create --ttl 1m --class direct --label bridge-test >/tmp/cli-int-bridge.txt
bridge_route=$(awk '{print $1}' </tmp/cli-int-bridge.txt)
set +e
"$HOPNET" bridge --route "$bridge_route" --listen 127.0.0.1:0
bridge_code=$?
set -e
[[ "$bridge_code" == "6" ]] || {
  echo "FAIL: bridge returned $bridge_code, expected 6 (not implemented)" >&2; exit 1; }
"$HOPNET" route delete "$bridge_route" >/dev/null

# `hopnet run` against the live proxy is gated behind HOPNET_RUN_LIVE=1
# because it requires curl to trust the dev proxy's self-signed cert. The
# default integration runs everything except the actual tunnel; flipping
# the flag exercises the full path against the local stack.
if [[ "${HOPNET_RUN_LIVE:-0}" == "1" ]]; then
  echo "==> hopnet run -- curl https://example.com (live tunnel)"
  set +e
  "$HOPNET" run --ttl 2m --max-mb 10 --class direct --allow example.com -- \
    curl -fsS --proxy-insecure https://example.com >/tmp/cli-int-run.html 2>/tmp/cli-int-run.stderr
  run_code=$?
  set -e
  [[ "$run_code" == "0" ]] || {
    echo "FAIL: run exit=$run_code" >&2
    sed 's/^/    stderr: /' /tmp/cli-int-run.stderr >&2
    exit 1; }
  grep -q "Example Domain" /tmp/cli-int-run.html || {
    echo "FAIL: example.com body not retrieved" >&2; exit 1; }
  grep -q "Route" /tmp/cli-int-run.stderr || {
    echo "FAIL: receipt not on stderr" >&2; exit 1; }
fi

echo "==> hopnet billing balance"
billing_balance_out=$("$HOPNET" billing balance)
echo "$billing_balance_out" | grep -q "balance:" || {
  echo "FAIL: billing balance missing balance line" >&2; exit 1; }

echo "==> hopnet billing history --limit 3"
billing_history_out=$("$HOPNET" billing history --limit 3 || true)
echo "$billing_history_out" | grep -q "WHEN" || {
  echo "FAIL: billing history missing header" >&2; exit 1; }

# topup requires BOTH Stripe creds in the sibling alpha stack. The server
# tightened in P9 review pass 2: POST /v1/billing/checkout returns 503
# stripe_not_configured unless STRIPE_SECRET_KEY_TEST AND
# STRIPE_WEBHOOK_SECRET_TEST are both present (otherwise we'd accept payment
# we can't post). Gate must mirror that check exactly so a half-configured
# .env produces a clean skip, not a hard failure.
stripe_test_key=""
stripe_test_webhook=""
if [[ -f "$HOPNET_ALPHA_DIR/.env" ]]; then
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" =~ ^[[:space:]]*STRIPE_SECRET_KEY_TEST=(.*)$ ]]; then
      stripe_test_key="${BASH_REMATCH[1]}"
    elif [[ "$line" =~ ^[[:space:]]*STRIPE_WEBHOOK_SECRET_TEST=(.*)$ ]]; then
      stripe_test_webhook="${BASH_REMATCH[1]}"
    fi
  done < "$HOPNET_ALPHA_DIR/.env"
fi
if [[ -n "$stripe_test_key" && -n "$stripe_test_webhook" ]]; then
  echo "==> hopnet billing topup --usd 25 --no-open --no-wait (Stripe configured)"
  topup_out=$("$HOPNET" billing topup --usd 25 --no-open --no-wait)
  echo "$topup_out" | grep -q "checkout URL: https://" || {
    echo "FAIL: topup did not print checkout URL" >&2
    echo "$topup_out" >&2; exit 1; }
  echo "$topup_out" | grep -q "session: cs_test_" || {
    echo "FAIL: topup did not print test-mode session id" >&2
    echo "$topup_out" >&2; exit 1; }

  echo "==> hopnet billing topup --usd 1 (insufficient → exit code != 0)"
  set +e
  "$HOPNET" billing topup --usd 1 --no-open --no-wait >/tmp/cli-int-topup-low.out 2>&1
  topup_low_code=$?
  set -e
  [[ "$topup_low_code" -ne 0 ]] || {
    echo "FAIL: topup --usd 1 should have failed (server returns 400)" >&2
    cat /tmp/cli-int-topup-low.out >&2; exit 1; }
else
  echo "==> hopnet billing topup: SKIPPED (STRIPE_SECRET_KEY_TEST + STRIPE_WEBHOOK_SECRET_TEST must both be set in $HOPNET_ALPHA_DIR/.env)"
fi

echo
echo "OK — all CLI integration assertions passed."
