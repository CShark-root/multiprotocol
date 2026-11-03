#!/usr/bin/env bash
# Installs (or updates) xray-core from the official XTLS/Xray-core GitHub
# release into /usr/local/lib/panel/xray, verifying the release checksum.
#
#   sudo bash scripts/install-xray.sh            # latest release
#   sudo bash scripts/install-xray.sh v25.10.15  # a specific tag
set -euo pipefail

DEST=/usr/local/lib/panel/xray
REPO=XTLS/Xray-core
TAG="${1:-}"

case "$(uname -m)" in
  x86_64|amd64)  ASSET=Xray-linux-64.zip ;;
  aarch64|arm64) ASSET=Xray-linux-arm64-v8a.zip ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

for cmd in curl unzip sha256sum; do
  command -v "$cmd" >/dev/null || { echo "missing: $cmd (apt install $cmd)" >&2; exit 1; }
done

if [[ -z "$TAG" ]]; then
  # Read the whole response first: piping into `grep -m1` would close the
  # pipe early and make curl fail under pipefail.
  RELEASE=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")
  TAG=$(printf '%s\n' "$RELEASE" | grep '"tag_name"' | head -n1 | cut -d'"' -f4)
fi
[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "unexpected tag: '$TAG'" >&2; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
BASE="https://github.com/$REPO/releases/download/$TAG"
echo "Downloading $ASSET $TAG"
curl -fsSL -o "$TMP/$ASSET" "$BASE/$ASSET"
curl -fsSL -o "$TMP/$ASSET.dgst" "$BASE/$ASSET.dgst"

# The .dgst file lists several digests; take the SHA2-256 one.
WANT=$(grep -iE '^SHA2-256' "$TMP/$ASSET.dgst" | head -n1 | awk '{print $NF}')
GOT=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
[[ -n "$WANT" && "$WANT" == "$GOT" ]] || { echo "checksum mismatch: want $WANT got $GOT" >&2; exit 1; }
echo "Checksum OK"

unzip -q -o "$TMP/$ASSET" -d "$TMP/x"
install -d -m 0755 "$DEST"
install -m 0755 "$TMP/x/xray" "$DEST/xray"
install -m 0644 "$TMP/x/geoip.dat" "$TMP/x/geosite.dat" "$DEST/"
echo "Installed: $("$DEST/xray" version | head -n1)"
