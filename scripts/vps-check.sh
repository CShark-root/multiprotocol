#!/usr/bin/env bash
# Read-only look at a server before installing MultiProtocol: what is there
# and what could get in the way. It changes nothing.
#
#   curl -fsSL https://raw.githubusercontent.com/CShark-root/multiprotocol/main/scripts/vps-check.sh | sudo bash
set -uo pipefail
say() { printf '\n== %s\n' "$1"; }

say "System"
. /etc/os-release 2>/dev/null && echo "OS: ${PRETTY_NAME:-unknown}"
echo "Kernel: $(uname -r), arch: $(uname -m), CPUs: $(nproc)"
free -m | awk 'NR==2 {printf "RAM: %d MB total, %d MB available\n", $2, $7}'
df -h / | awk 'NR==2 {print "Disk /: " $4 " free of " $2}'
echo "Uptime: $(uptime -p 2>/dev/null)"

say "Addresses"
ip -4 -o addr show scope global | awk '{print "IPv4: " $4 " (" $2 ")"}'
ip -6 -o addr show scope global 2>/dev/null | awk '{print "IPv6: " $4 " (" $2 ")"}'
echo "Default route: $(ip route show default | head -1)"
echo "Public IPv4 as seen outside: $(curl -4 -s -m 8 https://api.ipify.org || echo '?')"

say "Listening ports (who holds them)"
ss -tulpnH 2>/dev/null | awk '{split($5,a,":"); port=a[length(a)]; print $1, port, $7}' | sed 's/users:((//; s/,pid=[0-9]*,fd=[0-9]*//g; s/))//' | sort -u | sort -k2,2n -k1,1

say "Port 443"
ss -tlnpH 'sport = :443' 2>/dev/null | sed 's/^/tcp: /'
ss -ulnpH 'sport = :443' 2>/dev/null | sed 's/^/udp: /'

say "Docker and Amnezia"
if command -v docker >/dev/null; then
  docker --version
  docker ps -a --format '{{.Names}}\t{{.Status}}\t{{.Ports}}' 2>/dev/null | sed 's/^/  /'
else
  echo "Docker: not installed"
fi

say "Other panels and VPN services"
for unit in x-ui 3x-ui marzban xray hysteria-server sing-box panel; do
  state=$(systemctl is-active "$unit" 2>/dev/null)
  [[ $state == active || $state == activating || $state == failed ]] && echo "$unit: $state"
done
[[ -d /usr/local/x-ui ]] && echo "3x-ui files: /usr/local/x-ui"

say "Firewall"
if command -v ufw >/dev/null; then ufw status 2>/dev/null | head -20; else echo "ufw: not installed"; fi
echo "iptables INPUT policy: $(iptables -S INPUT 2>/dev/null | head -1)"
echo "iptables INPUT rules: $(iptables -S INPUT 2>/dev/null | grep -c '^-A')"
command -v nft >/dev/null && echo "nftables tables: $(nft list tables 2>/dev/null | tr '\n' ' ')"

say "Network tuning"
echo "congestion control: $(sysctl -n net.ipv4.tcp_congestion_control), qdisc: $(sysctl -n net.core.default_qdisc)"
echo "available: $(sysctl -n net.ipv4.tcp_available_congestion_control)"
echo "ip_forward: $(sysctl -n net.ipv4.ip_forward)"

say "Time"
timedatectl 2>/dev/null | grep -E 'Time zone|synchronized' | sed 's/^ *//'

echo
echo "Nothing was changed."
