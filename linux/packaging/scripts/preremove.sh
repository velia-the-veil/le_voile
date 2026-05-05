#!/bin/sh
# preremove — Le Voile VPN
# Exécuté AVANT que les fichiers ne soient retirés. Différencie entre :
#  - UPGRADE en cours : on NE touche à RIEN (le nouveau paquet restart en
#    postinstall). Couper le kill switch ici ouvrirait une fenêtre de leak
#    pendant les quelques secondes où les fichiers sont remplacés.
#  - REMOVE / PURGE : on arrête le service via systemctl, qui exécute son
#    propre cleanup (firewall down + TUN down) atomiquement. Audit fix Pk2
#    (2026-05-04) : on a retiré l'appel `levoile-ctl killswitch off`
#    préalable, qui dégradait le firewall avant l'arrêt du service →
#    fenêtre de fuite de quelques secondes pendant qu'un connect actif
#    pouvait sortir par la gateway physique. Le fallback `nft delete
#    table inet levoile` couvre les cas où le service crash sans cleanup.
#
# Conventions d'args :
#   - deb  : "upgrade" / "remove" / "purge" / "failed-upgrade" ...
#   - rpm  : 0 (final removal) / 1 (upgrade)
#   - apk  : pas d'arg → traité comme remove

set -u

log() { echo "[levoile] $*" >&2; }

ARG="${1:-remove}"
IS_UPGRADE=0
case "$ARG" in
    upgrade|failed-upgrade|abort-upgrade|1)
        IS_UPGRADE=1
        ;;
esac

if [ "$IS_UPGRADE" -eq 1 ]; then
    log "upgrade en cours — service laissé actif pour préserver le tunnel et le kill switch pendant le swap de fichiers."
    exit 0
fi

# --- REMOVE / PURGE ---

# 1. Arrêt + désactivation du service systemd. Le service shutdown handler
# (cmd/client/main.go shutdown sequence) tear down kill-switch + TUN
# atomiquement avant de rendre la main, donc pas de fenêtre de leak.
# Étape `levoile-ctl killswitch off` retirée (audit fix Pk2, 2026-05-04).
if command -v systemctl >/dev/null 2>&1; then
    if systemctl disable --now levoile.service 2>/dev/null; then
        log "service levoile arrêté et désactivé."
    else
        log "INFO : systemctl disable --now levoile.service — pas d'action (service déjà arrêté ou systemd inactif)."
    fi
fi

# 2. Fallback : retirer les règles nftables directement si le service crash.
# `nft delete table inet levoile` est idempotent-sur-absence (exit non-zéro),
# on ignore l'erreur. La table levoile est créée par le service (story 2.6).
if command -v nft >/dev/null 2>&1; then
    nft delete table inet levoile 2>/dev/null || true
fi

# 3. Retirer l'interface TUN si elle traîne.
if command -v ip >/dev/null 2>&1; then
    if ip link show levoile0 >/dev/null 2>&1; then
        ip link delete levoile0 2>/dev/null || true
        log "interface levoile0 supprimée."
    fi
fi

exit 0
