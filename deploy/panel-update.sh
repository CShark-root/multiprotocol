#!/usr/bin/env bash
# Runs as root from panel-update.service when the panel (which has no root
# rights) asks for an update by creating /var/lib/panel/update-request. It
# runs the same one-line installer an admin would: the latest release,
# checksum-verified. The request file holds options, one per line; only
# "cores=1" is understood, anything else is ignored.
set -uo pipefail
REQ=/var/lib/panel/update-request
LOG=/var/lib/panel/update.log
[[ -f $REQ ]] || exit 0
cores=0
grep -qx 'cores=1' "$REQ" && cores=1
rm -f "$REQ"
{
  echo "== $(date -Is) update started (cores: $cores)"
  if UPDATE_CORES=$cores bash /usr/local/lib/panel/install.sh; then
    echo "== $(date -Is) done"
  else
    echo "== $(date -Is) failed"
  fi
} >"$LOG.new" 2>&1
mv "$LOG.new" "$LOG"
chown panel:panel "$LOG"
chmod 0640 "$LOG"
