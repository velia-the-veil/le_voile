#!/bin/sh
# preremove — Le Voile VPN
# Exécuté AVANT que les fichiers ne soient retirés. Différencie entre :
#  - UPGRADE en cours : on NE touche à RIEN (le nouveau paquet restart en
#    postinstall). Couper le kill switch ici ouvrirait une fenêtre de leak
#    pendant les quelques secondes où les fichiers sont remplacés.
#  - REMOVE / PURGE : on arrête le service via IPC (killswitch off — enlève
#    nftables + interface via la logique service), PUIS systemctl disable.
#    L'ordre est important : le service doit tourner quand levoile-ctl lui
#    parle, sinon l'IPC échoue et on laisse nftables orphelin.
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

# 1. Désactivation propre du kill switch via IPC — tant que le service tourne
# encore. `levoile-ctl killswitch off` ne prend que 1 argument (off|on), pas
# de --reason. L'appel échoue silencieusement si le service est déjà arrêté
# ou si le token n'existe pas → on tombe alors sur le fallback nft brut.
if [ -x /usr/bin/levoile-ctl ]; then
    /usr/bin/levoile-ctl killswitch off >/dev/null 2>&1 || true
fi

# 2. Arrêt + désactivation du service systemd.
if command -v systemctl >/dev/null 2>&1; then
    if systemctl disable --now levoile.service 2>/dev/null; then
        log "service levoile arrêté et désactivé."
    else
        log "INFO : systemctl disable --now levoile.service — pas d'action (service déjà arrêté ou systemd inactif)."
    fi
fi

# 3. Fallback : retirer les règles nftables directement si le ctl n'a pas pu.
# `nft delete table inet levoile` est idempotent-sur-absence (exit non-zéro),
# on ignore l'erreur. La table levoile est créée par le service (story 2.6).
if command -v nft >/dev/null 2>&1; then
    nft delete table inet levoile 2>/dev/null || true
fi

# 4. Retirer l'interface TUN si elle traîne.
if command -v ip >/dev/null 2>&1; then
    if ip link show levoile0 >/dev/null 2>&1; then
        ip link delete levoile0 2>/dev/null || true
        log "interface levoile0 supprimée."
    fi
fi

exit 0
