#!/usr/bin/env bash
# Installs (or updates) the official Hysteria 2 server from the
# apernet/hysteria GitHub release into /usr/local/lib/panel/hysteria,
# verifying the release checksum.
#
#   sudo bash scripts/install-hysteria.sh               # latest release
#   sudo bash scripts/install-hysteria.sh app/v2.6.5    # a specific tag
set -euo pipefail

DEST=/usr/local/lib/panel/hysteria
REPO=apernet/hysteria
TAG="${1:-}"

case "$(uname -m)" in
  x86_64|amd64)  ASSET=hysteria-linux-amd64 ;;
  aarch64|arm64) ASSET=hysteria-linux-arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

for cmd in curl sha256sum; do
  command -v "$cmd" >/dev/null || { echo "missing: $cmd" >&2; exit 1; }
done

if [[ -z "$TAG" ]]; then
  # Read the whole response first: piping into a reader that stops early
  # would make curl fail under pipefail.
  RELEASE=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")
  TAG=$(printf '%s\n' "$RELEASE" | grep '"tag_name"' | head -n1 | cut -d'"' -f4)
fi
[[ "$TAG" =~ ^app/v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "unexpected tag: '$TAG'" >&2; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
BASE="https://github.com/$REPO/releases/download/$TAG"
echo "Downloading $ASSET $TAG"
curl -fsSL -o "$TMP/$ASSET" "$BASE/$ASSET"
curl -fsSL -o "$TMP/hashes.txt" "$BASE/hashes.txt"

# hashes.txt lists "<sha256>  <file>" lines; match the exact asset name.
WANT=$(awk -v f="$ASSET" '{n=$NF; sub(/^.*\//, "", n); if (n == f) {print $1; exit}}' "$TMP/hashes.txt")
GOT=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
[[ -n "$WANT" && "$WANT" == "$GOT" ]] || { echo "checksum mismatch: want '$WANT' got '$GOT'" >&2; exit 1; }
echo "Checksum OK"

install -d -m 0755 "$DEST"
install -m 0755 "$TMP/$ASSET" "$DEST/hysteria"
echo "Installed: $("$DEST/hysteria" version | grep -m1 -i '^version')"
