# Auto-update — Procédure de validation end-to-end cross-OS (Story 8.2)

Ce document décrit la matrice de validation manuelle pour le mécanisme d'auto-update couvert par l'[Story 8.2](../../_bmad-output/implementation-artifacts/8-2-telechargement-signe-application-rollback-automatique.md). Les tests unitaires Go couvrent la logique interne (NFR9c, package-managed, retry counter). Les scénarios ci-dessous couvrent les chemins OS qui ne sont pas testables en CI.

## Prérequis communs

- Release GitHub test `v1.0.0` et `v1.0.1` publiées sur un fork ou repo de test, signées avec une paire Ed25519 dédiée (pas la clé de production).
- `checksums.txt` et `checksums.txt.sig` générés par GoReleaser.
- Config TOML client pointant vers `github_owner`/`github_repo` du repo de test et contenant la clé publique Ed25519 correspondante dans `[relay].public_key_ed25519` (ou champ dédié `[update].pub_key` si introduit).
- Accès SSH/console aux VMs de test listées dans la matrice.

## Matrice de scénarios

| ID | Scénario | Plateforme(s) | Mécanisme clé | Résultat attendu |
|---|---|---|---|---|
| A | Linux service mode, swap + systemctl restart | Debian 12, Ubuntu 24.04, Fedora 40 | `/var/lib/levoile/updates/` + kardianos/systemd | v1.0.0 → v1.0.1 sans downtime, `systemctl is-active` = active, journalctl contient `updater: installed v1.0.1` |
| B | Linux user mode, staging `~/.config` | Arch (latest), Alpine 3.19 | `~/.config/levoile/updates/` + relaunch manuel | Binaire utilisateur swap ok, staging dans `$XDG_CONFIG_HOME` |
| C | Linux packaged (deb/rpm/pacman) — skip | Debian 12, Fedora 40, Arch | `IsPackageManaged()` + `UpdateAllowWhenPackaged=false` | Auto-update skippé, log `updater: install: skipped — binary is package-managed (/usr/bin)`, staging wipé |
| D | Windows service SCM | Windows 11 | NSIS + kardianos/SCM | v1.0.0 → v1.0.1 via SCM Stop+Start, Event Viewer contient ligne `updater: installed` |
| E | Rollback Linux (binaire qui crash au boot) | Debian 12 | Timer 30s + `Installer.Rollback()` | Tunnel ne se lève pas, backup `.bak` restauré, `systemctl restart` déclenché via `scheduleServiceRestart`, service actif sur v1.0.1 (précédent) |
| F | Signature invalide (checksums.txt.sig corrompu) | Debian 12 | `VerifySignature()` + nettoyage staging | Payload rejeté, fichiers staged supprimés, `journalctl` contient `updater: signature invalid` |

## Scénario A — Linux service mode

```bash
# VM : Debian 12 fraîche, root
apt install -y ./levoile_1.0.0_amd64.deb
systemctl is-active levoile.service  # expect: active

# Simuler la release v1.0.1 via le script de test
./scripts/test-auto-update-linux.sh \
    ./levoile_1.0.0_amd64.deb \
    ./staging-v1.0.1/

# Assertions automatiques dans le script :
# - service active après swap
# - journalctl contient "updater: installed v1.0.1" ou "updater: install:"
# - levoile-ctl status retourne v1.0.1
```

Si `levoile-ctl status` n'expose pas encore la version, l'attestation alternative est `journalctl -u levoile.service --since "2 minutes ago" | grep 'updater: installed'`.

## Scénario B — Linux user mode

```bash
# VM : Arch Linux user non-root
tar -xzf levoile_1.0.0_linux_amd64.tar.gz -C ~/.local/bin/
~/.local/bin/le_voile &  # run as user
# vérifier staging dir utilisateur
ls -la ~/.config/levoile/updates/
```

Pour déclencher un update sans attendre 6h : placer les fichiers v1.0.1 dans `~/.config/levoile/updates/` manuellement puis `kill -HUP <pid>` + relaunch. Vérifier que le nouveau binaire est en place.

## Scénario C — Linux packaged (skip)

```bash
# Debian 12, install .deb
apt install -y ./levoile_1.0.0_amd64.deb
# Forcer un staging depuis /var/lib/levoile/updates/
cp -v ./staging-v1.0.1/* /var/lib/levoile/updates/
# Redémarrer — ne doit PAS appliquer
systemctl restart levoile.service
sleep 5
# Vérifier log skip
journalctl -u levoile.service --since "1 minute ago" | grep "package-managed"
# Staging doit être wipé
ls /var/lib/levoile/updates/ | grep le_voile || echo "OK - staging wiped"
# Version doit rester v1.0.0
levoile-ctl status | grep -i version
```

Pour forcer l'auto-update malgré l'install packagée, éditer `/etc/levoile/config.toml` :
```toml
[update]
allow_when_packaged = true
```

## Scénario D — Windows service SCM

1. Installer `levoile-installer-1.0.0.exe` (NSIS), laisser l'UAC élever.
2. Vérifier dans Services.msc que "Le Voile VPN Service" est "Running".
3. Copier `staging-v1.0.1\*` vers `%ProgramData%\LeVoile\updates\` (mode admin).
4. `sc.exe stop LeVoile && sc.exe start LeVoile` OU redémarrer la machine.
5. Ouvrir Event Viewer → Windows Logs → Application, filtrer Source = "LeVoile" → attendre l'entrée `updater: installed v1.0.1`.
6. Vérifier via `"C:\Program Files\LeVoile\levoile-ctl.exe" status` que la version affichée est v1.0.1.

## Scénario E — Rollback Linux

Préparer une v1.0.2 défectueuse : binaire qui `exit 1` immédiatement ou qui échoue `TunnelClient.Connect()`. Release signée normalement.

```bash
# On v1.0.1 actif, stager v1.0.2 défectueuse
cp -v ./staging-v1.0.2-broken/* /var/lib/levoile/updates/
systemctl restart levoile.service
# Attendre 35s (timeout 30s + marge)
sleep 35
# Vérifier rollback
journalctl -u levoile.service --since "1 minute ago" | grep 'updater: rollback: restored previous version'
# Service doit être actif sur v1.0.1
systemctl is-active levoile.service  # expect: active
levoile-ctl status | grep 'v1\.0\.1'
# failed_version.txt doit contenir v1.0.2
cat /var/lib/levoile/updates/failed_version.txt  # expect: 1.0.2
```

## Scénario F — Signature invalide

```bash
# Stager v1.0.1 avec signature corrompue
cp ./staging-v1.0.1/le_voile_linux_amd64 /var/lib/levoile/updates/
cp ./staging-v1.0.1/checksums.txt /var/lib/levoile/updates/
# Injecter un payload signé avec une autre clé Ed25519
cp ./bad-key-signature/checksums.txt.sig /var/lib/levoile/updates/
systemctl restart levoile.service
sleep 5
# Vérifier rejet
journalctl -u levoile.service --since "1 minute ago" | grep -E 'updater: (signature invalid|install update)'
# Staging nettoyé par VerifyStagedUpdate()
ls /var/lib/levoile/updates/ | grep le_voile && echo "FAIL: staged files should be removed" || echo "OK: staged files removed"
# Version reste v1.0.0
levoile-ctl status | grep version
```

## Critères de sortie

Tous les scénarios A→F doivent passer sans régression sur :
- Debian 12 ✓
- Ubuntu 24.04 ✓
- Fedora 40 ✓
- Arch Linux (rolling) ✓
- Alpine 3.19 ✓
- Windows 11 ✓

Si un scénario échoue sur un OS donné, créer une issue GitHub référençant ce document + le scénario + les logs journalctl/Event Viewer, et mentionner l'échec dans la rétrospective Epic 8.

## Logs à capturer en cas d'échec

- Linux : `journalctl -u levoile.service --since "15 minutes ago" > /tmp/levoile-debug.log`
- Windows : Event Viewer → Filter Current Log → Source = LeVoile → Save All Events As → `levoile-debug.evtx`
- Contenu du staging : `ls -la /var/lib/levoile/updates/` (Linux) / `dir %ProgramData%\LeVoile\updates` (Windows)
- Config effective : `cat /etc/levoile/config.toml` (Linux) / `type %ProgramData%\LeVoile\config.toml` (Windows)
