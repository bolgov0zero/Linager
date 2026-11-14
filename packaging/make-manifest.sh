#!/bin/sh
# Writes bin/manifest.json for a tagged release: version, channel, checksum, size, DB migrations, notes.
set -eu
VERSION="${1:?usage: make-manifest.sh vX.Y.Z}"
BIN=bin/linager_linux_amd64
CHANNEL=stable
case "$VERSION" in *-*) CHANNEL=beta ;; esac
SHA=$(sha256sum "$BIN" | cut -d' ' -f1)
SIZE=$(stat -c %s "$BIN")
MIGRATIONS=$(find migrations -name '*.sql' | wc -l | tr -d ' ')
PREV=$(git describe --tags --abbrev=0 "${VERSION}^" 2>/dev/null || true)
RANGE=${PREV:+$PREV..}$VERSION
NOTES=$(git log --no-merges --pretty='%s' "$RANGE" | head -15 | jq -R . | jq -s .)
# Agents older than this cannot be updated through the panel and are shown as unsupported.
MIN_AGENT="${MIN_AGENT:-v0.1.0}"
jq -n --arg v "$VERSION" --arg c "$CHANNEL" --arg m "$MIN_AGENT" --arg sha "$SHA" --argjson size "$SIZE" \
  --argjson mig "$MIGRATIONS" --argjson notes "$NOTES" --arg t "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{version:$v, channel:$c, min_agent:$m, sha256:$sha, size:$size, migrations:$mig, notes:$notes, built_at:$t}' > bin/manifest.json
