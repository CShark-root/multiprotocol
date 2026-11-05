#!/usr/bin/env bash
# Runs as root from panel-update.service when the panel (which has no root
# rights) asks for an update by creating /var/lib/panel/update-request. It
# runs the same one-line installer an admin would: the latest release,
# checksum-verified. The request file holds options, one per line; only
# "cores=1" is understood, anything else is ignored.
#
# The log is written as it goes (the panel shows it live); "==> step" lines
# mark the stages.
set -uo pipefail
REQ=/var/lib/panel/update-request
LOG=/var/lib/panel/update.log
[[ -f $REQ ]] || exit 0
cores=0 only=0
grep -qx 'cores=1' "$REQ" && cores=1
# cores-only=1: только ядра, без переустановки панели (ночная автопроверка).
grep -qx 'cores-only=1' "$REQ" && only=1
rm -f "$REQ"
: >"$LOG"
chown panel:panel "$LOG"
chmod 0640 "$LOG"
if ((only)); then
  {
    echo "== $(date -Is) core update started"
    if bash /usr/local/lib/panel/update-cores.sh; then echo "== $(date -Is) done"; else echo "== $(date -Is) failed"; fi
  } >>"$LOG" 2>&1
  exit 0
fi
{
  echo "== $(date -Is) update started (cores: $cores)"
  if UPDATE_CORES=$cores bash /usr/local/lib/panel/install.sh; then
    echo "== $(date -Is) done"
  else
    echo "== $(date -Is) failed"
  fi
} >>"$LOG" 2>&1
