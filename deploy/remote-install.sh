#!/usr/bin/env bash
# Installs or updates the panel on a Linux server from files in $1 (default
# /tmp): panel-linux-<arch>, the systemd units and the
# install-{xray,hysteria,singbox}.sh scripts. Used by install.sh (one-line
# install from a GitHub release) and by scripts/vm/deploy.ps1. Data in
# /var/lib/panel is never touched. UPDATE_CORES=1 also reinstalls the cores
# at their latest versions.
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

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
echo "==> install"
install -m 0755 "$SRC/panel-linux-$ARCH" /usr/local/bin/panel
install -m 0644 "$SRC/panel.service" /etc/systemd/system/panel.service
install -m 0644 "$SRC/panel-amnezia.service" /etc/systemd/system/panel-amnezia.service
install -m 0644 "$SRC/panel-net.service" /etc/systemd/system/panel-net.service
# Port hopping uses nftables (the kernel has it; this adds the nft tool).
# nftables.service stays disabled: it would load /etc/nftables.conf at boot.
command -v nft >/dev/null || apt-get install -y -qq nftables
# Updates from the panel's own "Update" button: the panel only drops a request
# file, systemd runs the checksum-verified installer as root.
install -d -m 0755 /usr/local/lib/panel
install -m 0755 "$SRC/panel-update.sh" /usr/local/lib/panel/panel-update.sh
install -m 0644 "$SRC/panel-update.service" /etc/systemd/system/panel-update.service
install -m 0644 "$SRC/panel-update.path" /etc/systemd/system/panel-update.path
[[ -f $SRC/install.sh ]] && install -m 0755 "$SRC/install.sh" /usr/local/lib/panel/install.sh
# Автообновление ядер: скрипт и установщики ядер лежат рядом.
[[ -f $SRC/update-cores.sh ]] && install -m 0755 "$SRC/update-cores.sh" /usr/local/lib/panel/update-cores.sh
for s in install-xray.sh install-hysteria.sh install-singbox.sh; do
  install -m 0644 "$SRC/$s" /usr/local/lib/panel/$s
done
echo "==> cores"
for core in xray hysteria sing-box; do
  bin=/usr/local/lib/panel/$core/$core
  if [[ ! -x $bin || "${UPDATE_CORES:-0}" == 1 ]]; then
    bash "$SRC/install-${core//-/}.sh"
  fi
done

# Network tuning in a file of our own: BBR for TCP (Xray) and larger socket
# buffers for QUIC (Hysteria2, TUIC). Nothing Docker or Amnezia rely on
# (ip_forward and the like) is touched.
# Имя файла сортируется после 99-sysctl.conf: у части хостеров (Hostkey) в
# /etc/sysctl.conf свой алгоритм (hybla), и он иначе перебил бы наш BBR.
modprobe tcp_bbr 2>/dev/null || true
echo tcp_bbr > /etc/modules-load.d/panel-bbr.conf
rm -f /etc/sysctl.d/90-panel.conf
cat > /etc/sysctl.d/99-zz-panel.conf <<'SYSCTL'
# Written by the panel installer.
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
SYSCTL
sysctl -q -p /etc/sysctl.d/99-zz-panel.conf || echo "note: some network settings were not applied" >&2

# Узел: на этом сервере только агент, конфиги он берёт у главной панели.
# NODE_JOIN — код из вкладки «Серверы»; agent.json — узел уже подключён
# (повторный запуск обновляет агента).
if [[ -n ${NODE_JOIN:-} || -f /var/lib/panel/agent.json ]]; then
  if [[ -f /var/lib/panel/panel.db ]]; then
    echo "На этом сервере уже работает главная панель — узлом его сделать нельзя." >&2
    exit 1
  fi
  install -d -m 0700 -o panel -g panel /var/lib/panel
  install -m 0644 "$SRC/panel-agent.service" /etc/systemd/system/panel-agent.service
  if [[ -n ${NODE_JOIN:-} ]]; then
    sudo -u panel /usr/local/bin/panel agent-join -data-dir /var/lib/panel "$NODE_JOIN"
  fi
  echo "==> restart"
  systemctl daemon-reload
  systemctl enable panel-agent >/dev/null 2>&1
  systemctl restart panel-agent
  sleep 2
  systemctl --no-pager --lines=5 status panel-agent || true
  echo
  echo "Узел подключён. Он появится во вкладке «Серверы» главной панели через несколько секунд."
  exit 0
fi

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

# Earlier versions created the database world-readable (the directory is
# 0700, so it was never reachable, but keep it tight anyway).
chmod -f 600 /var/lib/panel/panel.db /var/lib/panel/panel.db-wal /var/lib/panel/panel.db-shm 2>/dev/null || true
echo "==> restart"
systemctl daemon-reload
systemctl enable panel panel-amnezia panel-net panel-update.path >/dev/null 2>&1
systemctl restart panel-net
systemctl start panel-update.path
# The Amnezia helper never changes Docker itself.
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
