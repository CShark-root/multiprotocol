#!/usr/bin/env bash
# Installs or updates the panel on a Linux server from files copied into
# $1 (default /tmp): panel-linux-amd64, panel.service and the
# install-{xray,hysteria,singbox}.sh scripts.
# Used by scripts/vm/deploy.ps1; the future one-line installer reuses it.
set -euo pipefail
SRC="${1:-/tmp}"
[[ $EUID -eq 0 ]] || { echo "run as root: sudo bash $0" >&2; exit 1; }

for cmd in curl unzip python3; do
  if ! command -v "$cmd" >/dev/null; then
    apt-get update -qq && apt-get install -y -qq curl unzip python3
    break
  fi
done

id panel &>/dev/null || useradd --system --home-dir /var/lib/panel --shell /usr/sbin/nologin panel

install -m 0755 "$SRC/panel-linux-amd64" /usr/local/bin/panel
install -m 0644 "$SRC/panel.service" /etc/systemd/system/panel.service
install -m 0644 "$SRC/panel-amnezia.service" /etc/systemd/system/panel-amnezia.service
[[ -x /usr/local/lib/panel/xray/xray ]] || bash "$SRC/install-xray.sh"
[[ -x /usr/local/lib/panel/hysteria/hysteria ]] || bash "$SRC/install-hysteria.sh"
[[ -x /usr/local/lib/panel/sing-box/sing-box ]] || bash "$SRC/install-singbox.sh"

# The one extra right the panel gets: rebooting the machine from the
# dashboard (logind asks polkit). "multiple-sessions" covers an open SSH login.
if [[ -d /etc/polkit-1/localauthority ]]; then # polkit 0.105 (Ubuntu 22.04)
  mkdir -p /etc/polkit-1/localauthority/50-local.d
  cat > /etc/polkit-1/localauthority/50-local.d/50-panel-reboot.pkla <<'PKLA'
[Let the panel service reboot the server]
Identity=unix-user:panel
Action=org.freedesktop.login1.reboot;org.freedesktop.login1.reboot-multiple-sessions
ResultAny=yes
ResultInactive=yes
ResultActive=yes
PKLA
fi
if [[ -d /etc/polkit-1/rules.d ]]; then # polkit 121+ (newer releases)
  cat > /etc/polkit-1/rules.d/50-panel-reboot.rules <<'RULES'
polkit.addRule(function (action, subject) {
  if (subject.user === "panel" &&
      (action.id === "org.freedesktop.login1.reboot" ||
       action.id === "org.freedesktop.login1.reboot-multiple-sessions")) {
    return polkit.Result.YES;
  }
});
RULES
fi

systemctl daemon-reload
systemctl enable panel panel-amnezia >/dev/null 2>&1
# The Amnezia tab helper only reads Docker; Docker itself is left alone.
systemctl restart panel-amnezia
systemctl restart panel
sleep 2
# Optional: the address clients use, when auto-detection would pick the wrong one.
if [[ -n "${PUBLIC_HOST:-}" ]]; then
  sudo -u panel /usr/local/bin/panel set-host -data-dir /var/lib/panel "$PUBLIC_HOST"
  systemctl restart panel
  sleep 2
fi
systemctl --no-pager --lines=5 status panel || true
echo
sudo -u panel /usr/local/bin/panel show-url -data-dir /var/lib/panel
