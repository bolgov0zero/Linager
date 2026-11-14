#!/bin/sh
# Linager installer bootstrap. Downloads a release, verifies it, runs `linager install`.
# Safe variant: curl -fsSLO <url>/install.sh; read it; sh install.sh
set -eu

REPO="bolgov0zero/linager"
# minisign public key of the release signing key (set when the key is created).
PUBKEY="${LINAGER_PUBKEY:-REPLACE_WITH_MINISIGN_PUBLIC_KEY}"
VERSION="${LINAGER_VERSION:-latest}"

# Clear screen and scrollback so the wizard starts on a clean terminal.
if [ -t 1 ]; then printf '\033[H\033[2J\033[3J'; fi

die() { printf 'Error: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root (sudo sh install.sh)"
[ "$(uname -m)" = "x86_64" ] || die "only amd64 is supported"
[ -r /etc/os-release ] || die "cannot detect the OS"
# shellcheck disable=SC1091
. /etc/os-release
case "${ID:-}:${VERSION_ID:-}" in
  debian:12|debian:13|ubuntu:22.04|ubuntu:24.04|ubuntu:26.04) ;;
  *) die "unsupported OS: ${PRETTY_NAME:-unknown} (Debian 12/13, Ubuntu 22.04/24.04/26.04)" ;;
esac
command -v curl >/dev/null 2>&1 || { apt-get update -qq && apt-get install -y -qq curl ca-certificates; }

if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
cd "$tmp"

printf 'Downloading Linager...\n'
curl -fsSL -o linager_linux_amd64 "$base/linager_linux_amd64" || die "download failed"
curl -fsSL -o SHA256SUMS "$base/SHA256SUMS" || die "checksums download failed"

grep ' linager_linux_amd64$' SHA256SUMS | sha256sum -c --status - || die "checksum mismatch"

if curl -fsSL -o SHA256SUMS.minisig "$base/SHA256SUMS.minisig" 2>/dev/null; then
  case "$PUBKEY" in REPLACE_*) die "signing key is not configured in this script" ;; esac
  command -v minisign >/dev/null 2>&1 || apt-get install -y -qq minisign >/dev/null 2>&1 \
    || die "cannot install minisign for signature verification"
  minisign -Vq -P "$PUBKEY" -m SHA256SUMS || die "signature verification failed"
else
  die "release is not signed; refusing to install"
fi

install -m 0755 linager_linux_amd64 /usr/local/bin/linager
exec /usr/local/bin/linager install "$@"
