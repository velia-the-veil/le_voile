#!/bin/sh
# postremove — Le Voile VPN
# Exécuté APRÈS que les fichiers aient été retirés. Deux modes :
#  - remove (retrait simple) : on laisse /etc/levoile/ et le user `levoile`
#    intacts (convention Debian — réinstall reprend la config).
#  - purge (Debian) / upgrade-cleanup (RPM $1 == "0") : on nettoie tout.
#
# Conventions des 3 formats (référence : nfpm + distrib docs) :
#   - deb  : $1 vaut "remove" ou "purge" (ou "upgrade"/"failed-upgrade"/...)
#   - rpm  : $1 vaut "0" (dernière instance retirée) ou "1" (upgrade en cours)
#   - apk  : pas d'argument standard → on traite comme "remove" par défaut
#
# Politique : on ne supprime jamais user + config si on n'est PAS CERTAIN que
# c'est une purge. Cela protège contre un upgrade cassé qui perdrait la config.

set -u

log() { echo "[levoile] $*" >&2; }

PURGE=0
case "${1:-remove}" in
    purge)
        # Debian purge
        PURGE=1
        ;;
    0)
        # RPM final removal (dernière instance retirée, pas un upgrade)
        PURGE=1
        ;;
    remove|upgrade|failed-upgrade|abort-install|abort-upgrade|disappear)
        # Debian remove simple ou upgrades en cours → on conserve tout
        PURGE=0
        ;;
    1|2)
        # RPM upgrade → on conserve
        PURGE=0
        ;;
    *)
        # apk ou cas inconnu → conservation par défaut (safer default)
        PURGE=0
        ;;
esac

if [ "$PURGE" -eq 0 ]; then
    log "retrait simple — /etc/levoile/ et user 'levoile' conservés (utiliser 'apt purge' ou 'rm -rf /etc/levoile && userdel levoile' pour nettoyer)."
    # Rafraîchir les caches XDG après retrait des fichiers.
    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database -q /usr/share/applications 2>/dev/null || true
    fi
    if command -v gtk-update-icon-cache >/dev/null 2>&1; then
        gtk-update-icon-cache -qf /usr/share/icons/hicolor 2>/dev/null || true
    fi
    exit 0
fi

# Mode purge : nettoyage complet.
log "purge — suppression de /etc/levoile/ et du user 'levoile'."

# 1. Supprimer /etc/levoile/ (config marquée `type: config|noreplace` dans le
# nfpm n'est pas retirée automatiquement par le gestionnaire de paquets, il
# faut le faire explicitement ici).
if [ -d /etc/levoile ]; then
    rm -rf /etc/levoile
fi

# 2. Supprimer le user système.
if command -v userdel >/dev/null 2>&1; then
    if id levoile >/dev/null 2>&1; then
        userdel levoile 2>/dev/null || log "WARN : userdel levoile a échoué (user a peut-être des fichiers résiduels)."
    fi
elif command -v deluser >/dev/null 2>&1; then
    # busybox deluser (Alpine)
    if id levoile >/dev/null 2>&1; then
        deluser levoile 2>/dev/null || true
    fi
fi

# 3. Supprimer les répertoires d'état si encore présents (systemd devrait les
# avoir nettoyés au stop via RuntimeDirectory mais on double-check).
rm -rf /run/levoile /var/lib/levoile /var/log/levoile 2>/dev/null || true

# 4. Rafraîchir les caches XDG.
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database -q /usr/share/applications 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -qf /usr/share/icons/hicolor 2>/dev/null || true
fi

exit 0
