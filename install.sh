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

# Prints the architecture of the release binary for this machine (amd64 or arm64).
# Returns 1 for any other CPU and 2 when a 64-bit kernel runs a 32-bit system (Raspberry Pi OS 32-bit
# can): the packages of such a system are armhf, so the arm64 binary would not match them.
detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) a=amd64 ;;
    aarch64 | arm64) a=arm64 ;;
    *) return 1 ;;
  esac
  if command -v dpkg >/dev/null 2>&1 && [ "$(dpkg --print-architecture 2>/dev/null)" != "$a" ]; then
    return 2
  fi
  printf '%s' "$a"
}

[ "$(id -u)" -eq 0 ] || die "run as root (sudo sh install.sh)"
rc=0
arch=$(detect_arch) || rc=$?
case "$rc" in
  0) ;;
  2) die "this is a 32-bit system on a 64-bit CPU; install a 64-bit OS (Raspberry Pi OS 64-bit, Debian arm64)" ;;
  *) die "unsupported CPU: $(uname -m) (amd64 and arm64 only; on a Raspberry Pi use a 64-bit OS)" ;;
esac
bin="linager_linux_$arch"
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
curl -fsSL -o "$bin" "$base/$bin" || die "download failed"
curl -fsSL -o SHA256SUMS "$base/SHA256SUMS" || die "checksums download failed"

grep " $bin\$" SHA256SUMS | sha256sum -c --status - || die "checksum mismatch"

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

install -m 0755 "$bin" /usr/local/bin/linager

# The downloads are no longer needed; `exec` below would skip the EXIT trap.
cd /
rm -rf "$tmp"
trap - EXIT INT TERM

# With `curl ... | sh` stdin is the pipe, not the keyboard: give the wizard the terminal back.
# Without a terminal (automation) the wizard runs non-interactively and needs its flags.
rc=0
if [ ! -t 0 ] && [ -t 1 ] && (: </dev/tty) 2>/dev/null; then
  /usr/local/bin/linager install "$@" </dev/tty || rc=$?
else
  /usr/local/bin/linager install "$@" || rc=$?
fi
[ "$rc" -eq 0 ] || exit "$rc"

# A person at a terminal gets the menu next, as if they had typed `linager`. The wizard has printed its summary
# (addresses, the administrator password): the menu waits for Enter, so the summary can be read first.
# Automation (--no-tui) and runs without a terminal end here.
case " $* " in *" --no-tui "*) exit 0 ;; esac
if [ -t 1 ] && (: </dev/tty) 2>/dev/null; then
  case "${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}" in
    ru*) hint='Готово. Нажмите Enter, чтобы открыть меню Linager (то же самое откроет команда linager)...' ;;
    *) hint='Done. Press Enter to open the Linager menu (the same as typing linager)...' ;;
  esac
  printf '\n%s ' "$hint"
  read -r _ </dev/tty || exit 0
  exec /usr/local/bin/linager </dev/tty
fi
