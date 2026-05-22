#!/usr/bin/env bash
# install.sh — one-shot installer for the hopnet CLI.
#
# Usage (note `| bash`, NOT `| sh` — the script uses bash-only options like
# `set -o pipefail`, and a piped shebang is ignored so /bin/sh would run it):
#   curl -fsSL https://raw.githubusercontent.com/HopNetLLC/hopnet-cli/main/install.sh | bash
#
# Environment overrides:
#   HOPNET_VERSION       — install a specific version (e.g. v0.1.1).
#                          Default: latest GitHub release.
#                          CI users should pin this — the unauthenticated
#                          GitHub releases API is rate-limited at 60/hr/IP.
#   HOPNET_INSTALL_DIR   — install directory.
#                          Default: $HOME/.local/bin (no sudo required).

set -euo pipefail

INSTALL_DIR="${HOPNET_INSTALL_DIR:-$HOME/.local/bin}"
REPO="HopNetLLC/hopnet-cli"

err()  { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }

# ---------- detect OS + arch ----------
os_raw="$(uname -s)"
arch_raw="$(uname -m)"

case "$os_raw" in
  Linux)  os=Linux ;;
  Darwin) os=Darwin ;;
  *)      err "unsupported OS: $os_raw (Linux + Darwin only). For Windows, build from source." ;;
esac

case "$arch_raw" in
  x86_64|amd64) arch=x86_64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported arch: $arch_raw" ;;
esac

# ---------- resolve version ----------
if [ -n "${HOPNET_VERSION:-}" ]; then
  tag="$HOPNET_VERSION"
  info "using pinned version: $tag"
else
  info "resolving latest release from github.com/$REPO"
  # `releases/latest` redirects to the actual tag URL; -I + -L follows redirects
  # and reveals the final tag in the URL — no auth required, no jq required.
  tag="$(curl -fsSL -o /dev/null -w '%{url_effective}' \
           "https://github.com/$REPO/releases/latest" \
         | sed -E 's|.*/tag/||')"
  [ -n "$tag" ] || err "failed to resolve latest release tag"
  info "latest release: $tag"
fi

# Strip leading 'v' from tag → archive filename includes the bare version.
version="${tag#v}"
archive="hopnet_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"

# ---------- checksum tool ----------
if command -v sha256sum >/dev/null 2>&1; then
  sha_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  sha_cmd="shasum -a 256"
else
  err "neither sha256sum nor shasum found. Install coreutils (Linux) or use a system that has \`shasum\` (macOS)."
fi

# ---------- download ----------
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

info "downloading $archive"
curl -fsSL -o "$tmpdir/$archive"        "$base/$archive"
curl -fsSL -o "$tmpdir/checksums.txt"   "$base/checksums.txt"

# ---------- verify ----------
info "verifying sha256"
(
  cd "$tmpdir"
  expected="$(grep "  $archive$" checksums.txt | awk '{print $1}')"
  [ -n "$expected" ] || err "no checksum for $archive in checksums.txt"
  actual="$($sha_cmd "$archive" | awk '{print $1}')"
  [ "$expected" = "$actual" ] || err "checksum mismatch: expected $expected got $actual"
)

# ---------- extract + install ----------
info "extracting"
tar -xzf "$tmpdir/$archive" -C "$tmpdir"
[ -f "$tmpdir/hopnet" ] || err "tarball did not contain a 'hopnet' binary"

mkdir -p "$INSTALL_DIR"

# Two-step move: temp path first, then atomic rename. Avoids ETXTBSY if a
# previous hopnet is currently running, and the rename is atomic on the
# same filesystem so a concurrent invocation never sees a partial binary.
staged="$INSTALL_DIR/.hopnet.$$"
mv "$tmpdir/hopnet" "$staged"
chmod 0755 "$staged"
mv -f "$staged" "$INSTALL_DIR/hopnet"

info "installed: $INSTALL_DIR/hopnet"

# ---------- PATH hint ----------
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    cat <<EOF

NOTE: $INSTALL_DIR is not on your PATH.

To use the binary, add this to your shell rc (~/.bashrc, ~/.zshrc, etc.):

    export PATH="$INSTALL_DIR:\$PATH"

Or invoke it directly:

    $INSTALL_DIR/hopnet version

EOF
    ;;
esac

# ---------- verify ----------
"$INSTALL_DIR/hopnet" version
