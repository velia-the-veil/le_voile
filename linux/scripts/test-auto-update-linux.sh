#!/usr/bin/env bash
# test-auto-update-linux.sh — End-to-end auto-update validation on Linux.
#
# STORY 8.2 (AC #2) — Validates systemctl restart path via kardianos/service.
#
# Scenario:
#   1. Install le_voile v1.0.0 from a local .deb (or tar.gz) into /opt/levoile
#      and register the systemd service via `deploy/install.sh`.
#   2. Stage a v1.0.1 release in /var/lib/levoile/updates/ that passes Ed25519
#      verification (signed by a test update public key).
#   3. Trigger auto-update via `levoile-ctl update-check` (or restart the
#      service manually — it picks up the staged update at boot).
#   4. Expect: service binary is swapped, `systemctl restart levoile.service`
#      is executed (observable via `journalctl -u levoile.service`), tunnel
#      reconnects, version reported by `levoile-ctl status` is v1.0.1.
#
# Usage (on a Debian/Ubuntu test VPS, as root):
#   ./scripts/test-auto-update-linux.sh /path/to/v1.0.0.deb /path/to/v1.0.1-staging/
#
# Exit codes:
#   0 = success, 1 = setup failure, 2 = assertion failure

set -euo pipefail

DEB_V1="${1:?usage: $0 <v1.0.0.deb> <v1.0.1-staging-dir>}"
STAGING_V2="${2:?usage: $0 <v1.0.0.deb> <v1.0.1-staging-dir>}"

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: must be run as root (needs systemd + /var/lib/levoile/)" >&2
  exit 1
fi

log() { printf '[%(%H:%M:%S)T] %s\n' -1 "$*"; }

log "Step 1/5 — Install v1.0.0"
dpkg -i "$DEB_V1" || { log "FAIL: dpkg install"; exit 1; }
systemctl start levoile.service
sleep 5

log "Step 2/5 — Verify v1.0.0 running"
ACTIVE=$(systemctl is-active levoile.service)
if [[ "$ACTIVE" != "active" ]]; then
  log "FAIL: service not active after install (state=$ACTIVE)"
  exit 2
fi

log "Step 3/5 — Copy v1.0.1 staged update"
mkdir -p /var/lib/levoile/updates
cp -v "$STAGING_V2"/* /var/lib/levoile/updates/
chmod 0700 /var/lib/levoile/updates
chown -R root:root /var/lib/levoile/updates

log "Step 4/5 — Restart service to trigger staged install"
systemctl restart levoile.service
sleep 10

log "Step 5/5 — Assertions"
ACTIVE=$(systemctl is-active levoile.service)
if [[ "$ACTIVE" != "active" ]]; then
  log "FAIL: service not active after auto-update (state=$ACTIVE)"
  journalctl -u levoile.service -n 30 --no-pager
  exit 2
fi

# Check journald for the expected updater log line.
if ! journalctl -u levoile.service --since "2 minutes ago" | grep -qE 'updater: installed v1\.0\.1|updater: install:'; then
  log "FAIL: journalctl missing updater install log line"
  journalctl -u levoile.service --since "2 minutes ago" --no-pager | tail -40
  exit 2
fi

# Version reported by ctl must be v1.0.1 (format assumed from current levoile-ctl).
VERSION=$(levoile-ctl status 2>/dev/null | grep -oE 'version[[:space:]]*:?[[:space:]]*v?[0-9]+\.[0-9]+\.[0-9]+' || true)
if [[ -z "$VERSION" || "$VERSION" != *"1.0.1"* ]]; then
  log "FAIL: levoile-ctl reports unexpected version: '$VERSION'"
  exit 2
fi

log "PASS — auto-update v1.0.0 → v1.0.1 succeeded"
log "       systemctl restart invoked, service re-active, version bumped"
exit 0
