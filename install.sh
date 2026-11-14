#!/bin/sh
# shellcheck disable=SC1091
# Linager installer bootstrap. Downloads a release, verifies it, runs `linager install`.
# Safe variant: curl -fsSLO <url>/install.sh; read it; sh install.sh
set -eu

REPO="bolgov0zero/linager"
# minisign public key of the release signing key (set when the key is created).
PUBKEY="${LINAGER_PUBKEY:-RWQlhVj7fNoehSMCgQigmYrwQMjs/DyxnH1zLVEXkNXINxSTC0HDdCOU}"
TAG="${LINAGER_VERSION:-latest}"

# Clear screen and scrollback so the wizard starts on a clean terminal.
if [ -t 1 ]; then printf '\033[H\033[2J\033[3J'; fi

die() { printf 'Error: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root (sudo sh install.sh)"
[ "$(uname -m)" = "x86_64" ] || die "only amd64 is supported"
[ -r /etc/os-release ] || die "cannot detect the OS"
# os-release is read in a subshell: it defines VERSION, NAME and friends that must not leak into this script
os_id=$(. /etc/os-release && printf '%s' "${ID:-}")
os_ver=$(. /etc/os-release && printf '%s' "${VERSION_ID:-}")
os_name=$(. /etc/os-release && printf '%s' "${PRETTY_NAME:-unknown}")
case "$os_id:$os_ver" in
  debian:12|debian:13|ubuntu:22.04|ubuntu:24.04|ubuntu:26.04) ;;
  *) die "unsupported OS: $os_name (Debian 12/13, Ubuntu 22.04/24.04/26.04)" ;;
esac
apt_quiet() { DEBIAN_FRONTEND=noninteractive apt-get -qq -o Dpkg::Use-Pty=0 "$@" >/dev/null 2>&1; }
if ! command -v curl >/dev/null 2>&1; then
  printf 'Installing curl...\n'
  if ! apt_quiet update || ! apt_quiet install -y curl ca-certificates; then die "cannot install curl"; fi
fi

if [ "$TAG" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$TAG"
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
  if ! command -v minisign >/dev/null 2>&1; then
    printf 'Installing minisign...\n'
    { apt_quiet update; apt_quiet install -y minisign; } || die "cannot install minisign for signature verification"
  fi
  minisign -Vq -P "$PUBKEY" -m SHA256SUMS || die "signature verification failed"
else
  die "release is not signed; refusing to install"
fi

install -m 0755 linager_linux_amd64 /usr/local/bin/linager
exec /usr/local/bin/linager install "$@"
