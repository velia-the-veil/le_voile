#!/bin/sh
# postinstall — Le Voile VPN
# Exécuté avec root privileges après que les fichiers aient été posés.
# Ne JAMAIS échouer ici (apt refuserait l'install) : toutes les actions sont
# best-effort, guardées `command -v` ou wrappées `|| true`.
#
# Convention args :
#   - deb  : "configure" (fresh install) / "configure <old-version>" (upgrade)
#   - rpm  : 1 (install) / 2 (upgrade) / 0 (autre)
#   - apk  : pas d'arg → traité comme install

set -u

log() { echo "[levoile] $*" >&2; }

ARG="${1:-configure}"
IS_UPGRADE=0
case "$ARG" in
    # deb : `$2` contient la version précédente sur upgrade. En pratique,
    # `configure` seul → fresh install, `configure <x.y.z>` → upgrade. On
    # détecte simplement en regardant si un 2e argument est fourni.
    configure)
        if [ -n "${2-}" ]; then
            IS_UPGRADE=1
        fi
        ;;
    # rpm : 2 = upgrade
    2)
        IS_UPGRADE=1
        ;;
esac

# ------------------------------------------------------------------------
# 1. Charger nf_tables (NFR22). L'absence n'est pas fatale : le service
# échouera au démarrage avec un message clair si nftables est indisponible
# (FR8 — story 2.6).
# ------------------------------------------------------------------------
if command -v modprobe >/dev/null 2>&1; then
    if modprobe nf_tables 2>/dev/null; then
        log "module nf_tables chargé."
    else
        log "WARN : modprobe nf_tables a échoué — le kill switch ne fonctionnera pas tant que nftables n'est pas disponible."
    fi
fi

if command -v nft >/dev/null 2>&1; then
    if nft list ruleset >/dev/null 2>&1; then
        log "nftables opérationnel (nft list ruleset OK)."
    else
        log "WARN : 'nft list ruleset' a échoué — vérifier que le kernel supporte nftables."
    fi
else
    log "WARN : commande 'nft' absente — le paquet 'nftables' devrait avoir été tiré comme dépendance."
fi

# ------------------------------------------------------------------------
# 2. Recharger systemd. Sur fresh install → enable + start. Sur upgrade :
# - si le user avait `systemctl disable`, on respecte (pas de ré-enable)
# - si le service tournait, on le restart pour prendre le nouveau binaire
# ------------------------------------------------------------------------
if command -v systemctl >/dev/null 2>&1; then
    if systemctl daemon-reload 2>/dev/null; then
        log "systemctl daemon-reload OK."

        if [ "$IS_UPGRADE" -eq 1 ]; then
            # Upgrade : respecte l'état préexistant (enabled / disabled).
            if systemctl is-enabled levoile.service >/dev/null 2>&1; then
                systemctl try-restart levoile.service 2>/dev/null \
                    && log "service levoile restart (upgrade, état enabled préservé)." \
                    || log "INFO : restart différé (service stoppé — OK)."
            else
                log "INFO : upgrade — service reste désactivé (état utilisateur respecté)."
            fi
        else
            # Fresh install : enable + start.
            if systemctl enable --now levoile.service 2>/dev/null; then
                log "service levoile activé (fresh install → start + enable)."
            else
                log "WARN : systemctl enable --now a échoué — activer manuellement via 'sudo systemctl enable --now levoile.service'."
            fi
        fi
    else
        log "INFO : systemd non actif dans cet environnement (probable chroot/container sans init) — activation différée."
    fi
else
    log "INFO : systemctl absent (OpenRC/runit/autre) — le service doit être activé via le gestionnaire de services local."
fi

# ------------------------------------------------------------------------
# 3. Rafraîchir les caches XDG (menus + icônes). Best-effort.
# ------------------------------------------------------------------------
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database -q /usr/share/applications 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -qf /usr/share/icons/hicolor 2>/dev/null || true
fi

if [ "$IS_UPGRADE" -eq 1 ]; then
    log "upgrade terminé."
else
    log "installation terminée — UI disponible à la prochaine session de bureau."
fi

exit 0
