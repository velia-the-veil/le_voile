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
# 2. Permissions /etc/levoile/. Le service tourne en `User=levoile` et écrit
# trois sidecars dans ce répertoire :
#   - config.integrity.key (LoadOrCreateKey, premier démarrage)
#   - config.toml.hmac     (Sign / SaveAndSign, à chaque mutation)
#   - config.toml          (SaveAndSign via IPC handler — écriture atomique
#                            par rename, donc dépend du mode du *dossier*)
# Le paquet pose le dossier en root:root 0755 → impossible pour `levoile`
# d'y créer/renommer. On chgrp + chmod 2770 (setgid) pour que :
#   - le user `levoile` puisse créer/renommer dedans (group write)
#   - les nouveaux fichiers héritent du groupe `levoile` (setgid bit)
#   - la racine reste protégée des autres users (no world perms)
# Idempotent → safe sur upgrade comme sur fresh install.
# Fait AVANT le start systemd : sinon le service crash en boucle au premier
# enable --now et `systemctl start` retourne en erreur.
# ------------------------------------------------------------------------
if [ -d /etc/levoile ]; then
    chgrp levoile /etc/levoile 2>/dev/null \
        && chmod 2770 /etc/levoile 2>/dev/null \
        && log "permissions /etc/levoile/ ajustées (root:levoile 2770)." \
        || log "WARN : chgrp/chmod /etc/levoile/ a échoué — le service ne pourra pas écrire ses sidecars (config.integrity.key, *.hmac)."
fi

# ------------------------------------------------------------------------
# 2-bis. Préparer /etc/firefox/policies/ pour les browser policies WebRTC.
# Mozilla supporte deux paths sur Linux pour policies.json :
#   - /usr/lib/firefox/distribution/policies.json (root-owned 755 par défaut)
#   - /etc/firefox/policies/policies.json         (path standard sysadmin)
# Le service tourne en User=levoile et ne peut pas écrire dans /usr/lib (root).
# On crée /etc/firefox/policies/ en root:levoile mode 2770 (même schéma que
# /etc/levoile/) pour que ApplyPolicies puisse y déposer le fichier sans
# privilege escalation. Sans ça, le code retombait sur /usr/lib/firefox/
# distribution/, le write échouait silencieusement (EACCES non surfacé) et
# la fuite WebRTC restait active malgré BrowserPoliciesEnabled=true.
# Idempotent → safe sur upgrade.
# ------------------------------------------------------------------------
mkdir -p /etc/firefox/policies 2>/dev/null \
    && chgrp levoile /etc/firefox/policies 2>/dev/null \
    && chmod 2770 /etc/firefox/policies 2>/dev/null \
    && log "/etc/firefox/policies/ créé (root:levoile 2770) pour Firefox WebRTC policies." \
    || log "WARN : impossible de préparer /etc/firefox/policies/ — la fuite WebRTC restera active sur Firefox."

# ------------------------------------------------------------------------
# 3. Recharger systemd. Sur fresh install → enable + start. Sur upgrade :
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
# 4. Ajouter le user qui invoque sudo au groupe `levoile`.
# L'UI tourne en tant qu'utilisateur de bureau (User=akerimus) et doit pouvoir :
#   - lire le socket IPC dans /run/levoile/ (mode 0750 levoile:levoile)
#   - lire ctl.token dans /etc/levoile/ (mode 2770 root:levoile via étape 2)
#   - lire le state dans /var/lib/levoile/ (mode 0750 levoile:levoile)
# Le seul moyen propre = membership du groupe `levoile`. Pattern standard
# (cf. groupes `docker`, `libvirt`, `wireshark`).
#
# Note : la nouvelle membership n'est active qu'après logout/login (la session
# de bureau capture les groupes au PAM-login). On le signale à la fin.
# ------------------------------------------------------------------------
TARGET_USER="${SUDO_USER:-}"
if [ -n "$TARGET_USER" ] && [ "$TARGET_USER" != "root" ]; then
    if id -nG "$TARGET_USER" 2>/dev/null | tr ' ' '\n' | grep -qx levoile; then
        log "user '$TARGET_USER' déjà membre du groupe levoile."
    else
        if command -v usermod >/dev/null 2>&1; then
            if usermod -aG levoile "$TARGET_USER" 2>/dev/null; then
                log "user '$TARGET_USER' ajouté au groupe levoile (effet à la prochaine session de bureau)."
            else
                log "WARN : usermod -aG levoile '$TARGET_USER' a échoué — ajouter manuellement."
            fi
        elif command -v adduser >/dev/null 2>&1; then
            # busybox/Alpine : adduser <user> <group>
            adduser "$TARGET_USER" levoile 2>/dev/null \
                && log "user '$TARGET_USER' ajouté au groupe levoile (effet à la prochaine session de bureau)." \
                || log "WARN : adduser '$TARGET_USER' levoile a échoué — ajouter manuellement."
        else
            log "WARN : ni usermod ni adduser trouvé — ajouter '$TARGET_USER' au groupe levoile manuellement."
        fi
    fi
else
    log "INFO : SUDO_USER vide (install par root direct) — ajouter manuellement les utilisateurs de bureau au groupe levoile : 'sudo usermod -aG levoile <user>'"
fi

# ------------------------------------------------------------------------
# 4-bis. Restaurer les contextes SELinux des binaires installés (audit fix
# U3, 2026-05-04). Sur RHEL/Fedora/CentOS Stream avec SELinux enforcing,
# les fichiers extraits par dpkg/rpm peuvent recevoir un mauvais contexte
# (typiquement bin_t vs un type custom local) — un restorecon réapplique
# la policy par défaut et garantit que systemd peut exécuter le binaire
# dans le bon domaine. Best-effort : pas de SELinux → no-op silencieux.
# Idempotent (restorecon est designed for ça) — safe sur upgrade.
# ------------------------------------------------------------------------
if command -v restorecon >/dev/null 2>&1; then
    for f in /usr/bin/levoile-service /usr/bin/levoile-ctl /usr/bin/levoile-ui; do
        [ -e "$f" ] && restorecon -F "$f" 2>/dev/null || true
    done
    [ -d /etc/levoile ] && restorecon -RF /etc/levoile 2>/dev/null || true
    log "contextes SELinux restaurés (best-effort)."
fi

# ------------------------------------------------------------------------
# 5. Rafraîchir les caches XDG (menus + icônes). Best-effort.
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
    log "installation terminée."
    log ""
    log "⚠ DÉCONNEXION/RECONNEXION REQUISE pour que l'UI accède au service :"
    log "   l'utilisateur de bureau doit avoir activé son appartenance au"
    log "   groupe 'levoile', ce qui ne se fait qu'au PAM-login."
fi

exit 0
