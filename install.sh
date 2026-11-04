#!/usr/bin/env bash
# MultiProtocol: install or update with one command.
#
#   curl -fsSL https://raw.githubusercontent.com/CShark-root/multiprotocol/main/install.sh | sudo bash
#
# Downloads the latest release (or MP_VERSION=v1.2.3) for this CPU, checks
# its SHA-256, installs the panel, the cores (Xray, Hysteria2, sing-box) and
# the systemd units, and starts everything. Running it again updates the
# panel; the data in /var/lib/panel is kept. UPDATE_CORES=1 also updates the
# cores. Docker and Amnezia are never touched.
set -euo pipefail

REPO="${MP_REPO:-CShark-root/multiprotocol}"
VERSION="${MP_VERSION:-latest}"

[[ $EUID -eq 0 ]] || { echo "Run as root: curl -fsSL … | sudo bash" >&2; exit 1; }
for cmd in curl tar sha256sum; do
  command -v "$cmd" >/dev/null || { apt-get update -qq && apt-get install -y -qq curl tar coreutils; break; }
done

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [[ $VERSION == latest ]]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi
ASSET="multiprotocol-linux-$ARCH.tar.gz"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
echo "Downloading $ASSET ($VERSION) from github.com/$REPO"
curl -fsSL --retry 3 -o "$TMP/$ASSET" "$BASE/$ASSET"
curl -fsSL --retry 3 -o "$TMP/$ASSET.sha256" "$BASE/$ASSET.sha256"
(cd "$TMP" && sha256sum -c "$ASSET.sha256" >/dev/null) || { echo "checksum mismatch, aborting" >&2; exit 1; }

mkdir "$TMP/pkg"
tar -xzf "$TMP/$ASSET" -C "$TMP/pkg"
bash "$TMP/pkg/remote-install.sh" "$TMP/pkg"
