#!/usr/bin/env bash
# What does this server show to the outside? Run it on the VPS as root,
# any time (it changes nothing):
#
#   curl -fsSL https://raw.githubusercontent.com/CShark-root/multiprotocol/main/scripts/exposure-check.sh | sudo bash
#
# It lists every port reachable from outside with its owner, checks that
# the panel's own control channels stay on 127.0.0.1 or unix sockets, probes
# the panel port and the Reality port the way an active-probing censor
# would, and checks file permissions. Lines start with OK, WARN or FAIL.
set -uo pipefail
[[ $EUID -eq 0 ]] || { echo "Run as root: sudo bash $0" >&2; exit 1; }
ok() { echo "OK    $*"; }
warn() { echo "WARN  $*"; WARNS=$((WARNS + 1)); }
fail() { echo "FAIL  $*"; FAILS=$((FAILS + 1)); }
WARNS=0 FAILS=0
DATA=/var/lib/panel

echo "== Ports reachable from outside"
# The OS's own DHCP and time clients answer nothing useful to a prober.
expected='panel|xray|hysteria|sing-box|sshd|docker-proxy|systemd-network|systemd-resolve|chronyd|dhclient'
while read -r proto addr proc; do
  name=$(sed -E 's/.*\(\("([^"]+)".*/\1/' <<<"$proc")
  case $addr in 127.*|\[::1\]:*|*%lo:*) continue ;; esac
  if [[ $name =~ ^($expected)$ ]]; then ok "$proto $addr  $name"; else warn "$proto $addr  $name (not part of the panel or Amnezia)"; fi
done < <(ss -tulpnH | awk '{print $1, $5, $7}' | sort -u)

echo
echo "== Control channels stay local"
if pgrep -af '/usr/local/bin/panel' | grep -q -- ' -dev'; then fail "the panel runs with -dev (plain HTTP)"; else ok "panel is not in -dev mode"; fi
for f in "$DATA"/xray/config.json; do
  [[ -f $f ]] || continue
  api=$(python3 -c 'import json,sys; c=json.load(open(sys.argv[1])); print(" ".join(i.get("listen","") for i in c.get("inbounds",[]) if i.get("tag")=="api" or i.get("protocol")=="socks"))' "$f" 2>/dev/null)
  for a in $api; do [[ $a == 127.0.0.1* ]] && ok "xray API/bridge on $a" || fail "xray API or bridge listens on $a"; done
done
for f in "$DATA"/hysteria/*/config.json; do
  [[ -f $f ]] || continue
  s=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("trafficStats",{}).get("listen",""))' "$f" 2>/dev/null)
  [[ -z $s || $s == 127.0.0.1:* ]] && ok "hysteria stats ${s:-off} ($(basename "$(dirname "$f")"))" || fail "hysteria stats on $s"
done
if [[ -f $DATA/sing-box/config.json ]]; then
  c=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("experimental",{}).get("clash_api",{}).get("external_controller",""))' "$DATA/sing-box/config.json" 2>/dev/null)
  [[ -z $c || $c == 127.0.0.1:* ]] && ok "sing-box API ${c:-off}" || fail "sing-box API on $c"
fi
for s in /run/panel-amnezia/helper.sock /run/panel-net/helper.sock; do
  [[ -S $s ]] || continue
  perm=$(stat -c '%a %U:%G' "$s")
  [[ $perm == "660 root:panel" ]] && ok "$s ($perm)" || fail "$s has $perm"
done

echo
echo "== The panel port as a stranger sees it"
port=$(python3 -c 'import json; c=json.load(open("/var/lib/panel/panel.json")); print(c["listen"].rsplit(":",1)[1])' 2>/dev/null)
if [[ -n $port ]]; then
  for p in / /login /admin /api/ /sub/ /robots.txt; do
    code=$(curl -sk -m 5 -o /tmp/ec.body -w '%{http_code}' "https://127.0.0.1:$port$p")
    size=$(stat -c %s /tmp/ec.body 2>/dev/null || echo 0)
    if [[ $code == 404 && $size == 0 ]] || [[ $code == 000 ]]; then ok "GET $p -> ${code/000/no answer} empty"; else fail "GET $p -> $code, $size bytes"; fi
  done
  out=$(curl -s -m 5 "http://127.0.0.1:$port/" -o /dev/null -w '%{http_code}' 2>/dev/null)
  [[ $out == 000 ]] && ok "plain HTTP gets no answer" || fail "plain HTTP answered $out"
  hdr=$(curl -sk -m 5 -D - -o /dev/null "https://127.0.0.1:$port/" | tr -d '\r' | grep -iE '^(server|x-powered-by):')
  [[ -z $hdr ]] && ok "no Server / X-Powered-By header" || warn "reveals: $hdr"
  subj=$(echo | timeout 5 openssl s_client -connect 127.0.0.1:"$port" 2>/dev/null | openssl x509 -noout -subject 2>/dev/null)
  echo "      panel certificate: ${subj:-?}"
  if grep -qiE 'vpn|proxy|panel|xray|hysteria|multiprotocol|amnezia' <<<"$subj"; then
    warn "the certificate name hints at a VPN. Certificate names are public (crt.sh), and the same name is the SNI of Hysteria2/TUIC clients. A neutral name is safer."
  fi
fi

echo
echo "== Reality (443/tcp) as a prober sees it"
if ss -tlnpH | grep -q ':443 .*xray'; then
  real=$(echo | timeout 6 openssl s_client -connect 127.0.0.1:443 -servername "probe-$RANDOM.example.org" 2>/dev/null | openssl x509 -noout -subject 2>/dev/null)
  if [[ -n $real ]]; then ok "an unknown SNI gets the target site's certificate: ${real#subject=}"; else warn "an unknown SNI got no certificate (connection dropped)"; fi
  echo "      Reality borrows a foreign site's TLS. Safest is a target hosted in the same network as this VPS (Inbounds -> scan targets)."
else
  echo "      no Xray on 443/tcp"
fi

echo
echo "== Files"
for f in "$DATA" "$DATA/panel.db" "$DATA/panel.json"; do
  [[ -e $f ]] || continue
  m=$(stat -c '%a' "$f")
  if [[ -d $f && $m == 700 ]] || [[ -f $f && $m == 600 ]]; then ok "$f ($m)"; else warn "$f is $m"; fi
done

echo
echo "== Summary: $FAILS failed, $WARNS warnings"
[[ $FAILS -eq 0 ]]
