#!/usr/bin/env bash
# Installs (or updates) sing-box from the official SagerNet/sing-box GitHub
# release into /usr/local/lib/panel/sing-box. The release has no checksum
# file, so the SHA-256 digest GitHub publishes for each release asset (REST
# API "digest" field) is verified instead.
#
#   sudo bash scripts/install-singbox.sh            # latest release
#   sudo bash scripts/install-singbox.sh v1.12.12   # a specific tag
set -euo pipefail

DEST=/usr/local/lib/panel/sing-box
REPO=SagerNet/sing-box
TAG="${1:-}"

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

for cmd in curl tar sha256sum python3; do
  command -v "$cmd" >/dev/null || { echo "missing: $cmd" >&2; exit 1; }
done

if [[ -z "$TAG" ]]; then
  API="https://api.github.com/repos/$REPO/releases/latest"
else
  API="https://api.github.com/repos/$REPO/releases/tags/$TAG"
fi
RELEASE=$(curl -fsSL "$API")

# Pick the plain linux-<arch> tarball (not -glibc / -musl / -purego variants)
# and its digest.
read -r TAG ASSET URL WANT < <(printf '%s' "$RELEASE" | python3 -c '
import json, re, sys
arch = sys.argv[1]
r = json.load(sys.stdin)
for a in r["assets"]:
    if re.fullmatch(r"sing-box-[0-9.]+-linux-" + arch + r"\.tar\.gz", a["name"]):
        print(r["tag_name"], a["name"], a["browser_download_url"], (a.get("digest") or "").removeprefix("sha256:"))
        break
' "$ARCH")
[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "no stable release asset found (tag '$TAG')" >&2; exit 1; }
[[ -n "${WANT:-}" ]] || { echo "GitHub did not publish a digest for $ASSET; refusing to install unverified" >&2; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
echo "Downloading $ASSET $TAG"
curl -fsSL -o "$TMP/$ASSET" "$URL"
GOT=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
[[ "$WANT" == "$GOT" ]] || { echo "checksum mismatch: want $WANT got $GOT" >&2; exit 1; }
echo "Checksum OK"

tar -xzf "$TMP/$ASSET" -C "$TMP"
install -d -m 0755 "$DEST"
install -m 0755 "$TMP"/sing-box-*/sing-box "$DEST/sing-box"
echo "Installed: $("$DEST/sing-box" version | head -n1)"
