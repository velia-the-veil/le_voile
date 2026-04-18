# Story 7.2 — paquets Linux : tests manuels (VM)

Les smoke tests Docker (`packaging/smoke/run.sh`) vérifient l'installation
dans des environnements minimaux sans init. Les **VMs complètes** ci-dessous
valident le chemin utilisateur final : service qui démarre vraiment, tray qui
apparaît au login, désinstallation propre. À exécuter avant chaque release
touchant au packaging.

## Pré-requis

- VMs : Ubuntu 24.04 LTS, Fedora 40, Alpine 3.19 (openrc ou test-utilisateur
  uniquement puisque Alpine n'a pas systemd par défaut).
- `goreleaser release --snapshot --skip=publish` exécuté localement, paquets
  dans `dist/`.

## Ubuntu 24.04 — .deb (AC3, AC5, AC6, AC7)

```bash
# 1) Installation
sudo apt install ./levoile_*_linux_amd64.deb

# 2) Vérifier le service
systemctl status levoile.service
# attendu : Active: active (running), User=levoile, CAP_NET_ADMIN présente

# 3) Vérifier les capabilities (processus)
systemd-analyze security levoile.service
# attendu : score ≤ 4.5 ("OK" ou "MEDIUM")

sudo cat /proc/$(pgrep -f levoile-service)/status | grep ^Cap
# attendu : CapEff: 0000000000003000 (CAP_NET_ADMIN + CAP_NET_RAW)

# 4) Vérifier le user
getent passwd levoile
# attendu : levoile:x:UID:GID:Le Voile VPN service:/:/usr/sbin/nologin

# 5) Se déconnecter / reconnecter : l'UI Le Voile doit apparaître dans le tray
#    (autostart XDG → systemctl --user start levoile-ui.service).

# 6) Vérifier le menu applications : Le Voile listé dans Réseau / Sécurité.

# 7) Désinstallation propre (conserve la config)
sudo apt remove levoile
systemctl status levoile.service
# attendu : Unit levoile.service could not be found.
ls /etc/levoile/
# attendu : config.toml encore présent

# 8) Purge
sudo apt purge levoile
ls /etc/levoile/ 2>/dev/null
# attendu : No such file or directory
getent passwd levoile
# attendu : (rien)
```

## Fedora 40 — .rpm (AC3, AC5, AC7)

```bash
# 1) Installation
sudo dnf install ./levoile-*.x86_64.rpm

# 2) Identique à Ubuntu :
systemctl status levoile.service
systemd-analyze security levoile.service
getent passwd levoile

# 3) SELinux — le service doit tourner malgré les contextes. Si denials :
sudo ausearch -m AVC -ts recent | grep levoile
# attendu : aucun (les capabilities systemd sont compatibles avec SELinux default)

# 4) Désinstallation
sudo dnf remove levoile
# NB : RPM retire tout par défaut (pas d'équivalent "purge"). Vérifier :
ls /etc/levoile/ 2>/dev/null  # attendu : vide
getent passwd levoile          # attendu : (rien)
```

## Alpine 3.19 — .apk (AC4, AC7)

Alpine n'a pas systemd par défaut → pas d'activation automatique du service.
Valide uniquement la présence des fichiers et l'intégrité du paquet.

```bash
# 1) Installation (paquet non signé → --allow-untrusted en attendant story 7.4)
sudo apk add --allow-untrusted ./levoile_*_linux_amd64.apk

# 2) Vérifier les fichiers (pas le service, il ne tourne pas)
apk info -L levoile | head -20
test -x /usr/bin/levoile-service && echo OK
test -x /usr/bin/levoile-ui && echo OK
test -x /usr/bin/levoile-ctl && echo OK
test -f /usr/lib/systemd/system/levoile.service && echo OK  # présent mais non utilisé

# 3) User système
grep ^levoile: /etc/passwd
# attendu : levoile:x:UID:GID:Le Voile VPN service:/:/sbin/nologin (ou /bin/false)

# 4) Désinstallation
sudo apk del levoile
grep ^levoile: /etc/passwd 2>/dev/null  # attendu : (rien)
```

## Critères de validation release

- Ubuntu 24.04 .deb : tous les checks ci-dessus passent, cycle install→usage→purge clean
- Fedora 40 .rpm : idem
- Alpine 3.19 .apk : install + désinstall OK (service non démarré = attendu)
- `systemd-analyze security levoile.service` ≤ 4.5 sur les deux VMs systemd
- Aucun denial SELinux sur Fedora

## Notes

- Le paquet **non signé** nécessite `--allow-untrusted` sur Alpine et
  `--force-signatures-ignore` équivalent sur dnf/apt si les repos sont signés.
  Story 7.4 ajoute la signature Ed25519 et la clé publique côté repo.
- Les logs du service sont dans `journalctl -u levoile.service` (systemd) ou
  `/var/log/levoile/` (LogsDirectory=levoile dans le unit). **Aucune URL ni
  nom de domaine ne doit apparaître** (NFR22a).
