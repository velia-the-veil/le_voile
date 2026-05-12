#!/bin/bash
# Le Voile - Watchdog d'expiration du cert TLS servi par levoile-relay.
#
# Déclenché par le timer systemd levoile-cert-check.timer (daily).
# Lit le cert réellement servi par le process (chemin extrait de ExecStart),
# puis escalade dans journald selon les jours restants :
#   >30j : silencieux
#   ≤30j : info
#   ≤14j : warning
#   ≤7j  : crit
#
# Consultation : journalctl -t levoile-cert-expiry --since -7d
set -u

CERT=$(systemctl show levoile-relay -p ExecStart --value 2>/dev/null \
       | grep -oE -- '-cert [^ ]+' | awk '{print $2}')
[ -z "$CERT" ] && CERT=/opt/levoile/cert.pem

if [ ! -r "$CERT" ]; then
    echo "cert file unreadable: $CERT" | systemd-cat -t levoile-cert-expiry -p err
    exit 0
fi

END=$(openssl x509 -in "$CERT" -noout -enddate 2>/dev/null | cut -d= -f2)
[ -z "$END" ] && exit 0

NOW_S=$(date +%s)
END_S=$(date -d "$END" +%s 2>/dev/null) || exit 0
DAYS=$(( (END_S - NOW_S) / 86400 ))

HN=$(hostname)
REAL=$(readlink -f "$CERT")
MSG="cert=$CERT real=$REAL expires_in_days=$DAYS host=$HN"

if   [ "$DAYS" -le 7  ]; then echo "CRITICAL: $MSG" | systemd-cat -t levoile-cert-expiry -p crit
elif [ "$DAYS" -le 14 ]; then echo "WARNING: $MSG"  | systemd-cat -t levoile-cert-expiry -p warning
elif [ "$DAYS" -le 30 ]; then echo "INFO: $MSG"     | systemd-cat -t levoile-cert-expiry -p info
fi
