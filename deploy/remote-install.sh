#!/usr/bin/env bash
# Installs or updates the panel on a Linux server from files copied into
# $1 (default /tmp): panel-linux-amd64, panel.service, install-xray.sh.
# Used by scripts/vm/deploy.ps1; the future one-line installer reuses it.
set -euo pipefail
SRC="${1:-/tmp}"
[[ $EUID -eq 0 ]] || { echo "run as root: sudo bash $0" >&2; exit 1; }

for cmd in curl unzip; do
  if ! command -v "$cmd" >/dev/null; then
    apt-get update -qq && apt-get install -y -qq curl unzip
    break
  fi
done

id panel &>/dev/null || useradd --system --home-dir /var/lib/panel --shell /usr/sbin/nologin panel

install -m 0755 "$SRC/panel-linux-amd64" /usr/local/bin/panel
install -m 0644 "$SRC/panel.service" /etc/systemd/system/panel.service
[[ -x /usr/local/lib/panel/xray/xray ]] || bash "$SRC/install-xray.sh"

systemctl daemon-reload
systemctl enable panel >/dev/null 2>&1
systemctl restart panel
sleep 2
systemctl --no-pager --lines=5 status panel || true
echo
sudo -u panel /usr/local/bin/panel show-url -data-dir /var/lib/panel
