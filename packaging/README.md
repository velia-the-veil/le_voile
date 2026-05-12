# Packaging Linux — Le Voile

Assets de packaging Linux natif. Les paquets `.deb` / `.rpm` / `.apk` sont
générés par GoReleaser + nfpm (story 7.2) et installent un service systemd
privilégié (`levoile-service`), l'UI tray+webview (`levoile-ui`), le CLI
opérateur (`levoile-ctl`) plus les métadonnées XDG nécessaires.

## Fichiers

```
packaging/
├── systemd/
│   ├── levoile.service                 # unit system (service privilégié)
│   └── user/
│       └── levoile-ui.service          # unit user (tray + webview) — story 5.7
├── desktop/
│   ├── levoile.desktop                 # entrée menu applications
│   └── levoile-autostart.desktop       # XDG autostart — story 5.7
├── scripts/
│   ├── preinstall.sh                   # crée user levoile (idempotent)
│   ├── postinstall.sh                  # modprobe nf_tables, systemctl enable, caches XDG
│   ├── preremove.sh                    # systemctl disable, shutdown kill switch
│   └── postremove.sh                   # purge conditionnelle user + /etc/levoile
├── etc/
│   └── config.toml                     # skeleton /etc/levoile/config.toml
├── icons/
│   ├── generate.sh                     # régénère depuis build/appicon.png
│   └── hicolor/
│       ├── 16x16/apps/levoile.png
│       ├── 32x32/apps/levoile.png
│       ├── 48x48/apps/levoile.png
│       ├── 64x64/apps/levoile.png
│       ├── 128x128/apps/levoile.png
│       └── 256x256/apps/levoile.png
└── smoke/
    └── run.sh                          # smoke test 3 containers Docker
```

## Build local

```bash
# Build snapshot (sans publication, version `0.0.0-next`).
# Produit dans dist/ : les binaires + archives + .deb/.rpm/.apk × amd64/arm64.
goreleaser release --snapshot --skip=publish

# Vérifier la config sans build :
goreleaser check
```

## Smoke test containers

```bash
# Prérequis : Docker daemon actif + dist/ peuplé via la commande ci-dessus.
bash packaging/smoke/run.sh
```

Lance 3 containers (debian:12-slim, fedora:40, alpine:3.19), installe le paquet
natif correspondant dans chacun et vérifie tous les chemins d'installation
attendus. Rapport final : exit 0 si tout passe, sinon échec explicite.

## Fichiers installés par les paquets

| Chemin | Permissions | Rôle |
|---|---|---|
| `/usr/bin/levoile-service` | 0755 root:root | Service privilégié (tunnel + kill switch) |
| `/usr/bin/levoile-ui` | 0755 root:root | UI tray + webview |
| `/usr/bin/levoile-ctl` | 0755 root:root | CLI opérateur (killswitch, status) |
| `/usr/lib/systemd/system/levoile.service` | 0644 root:root | Unit systemd system |
| `/usr/lib/systemd/user/levoile-ui.service` | 0644 root:root | Unit systemd user (supervision UI) |
| `/etc/xdg/autostart/levoile-autostart.desktop` | 0644 root:root | XDG autostart → `systemctl --user start levoile-ui.service` |
| `/usr/share/applications/levoile.desktop` | 0644 root:root | Entrée menu applications |
| `/etc/levoile/config.toml` | 0644 root:root | Skeleton config (type `config|noreplace`) |
| `/usr/share/icons/hicolor/{16..256}/apps/levoile.png` | 0644 root:root | Icônes XDG hicolor |
| `/usr/share/doc/levoile/{LICENSE,README.md}` | 0644 root:root | Documentation FHS |

## Workflow d'installation (ce que font les scripts)

1. **preinstall** — crée le user système `levoile` (`useradd --system` ou
   `adduser -S` sur Alpine), idempotent.
2. **Fichiers posés** — par apt/dnf/apk.
3. **postinstall** — `modprobe nf_tables` best-effort, `nft list ruleset` smoke,
   `systemctl daemon-reload` + `systemctl enable --now levoile.service`,
   rafraîchissement des caches XDG (`update-desktop-database`,
   `gtk-update-icon-cache`). Toutes les étapes sont guardées et n'échouent
   jamais l'installation (containers Docker sans init → log INFO, pas d'exit non-zéro).

## Désinstallation

```bash
sudo apt remove levoile            # Debian/Ubuntu — conserve /etc/levoile/
sudo apt purge levoile             # Debian/Ubuntu — nettoie tout (user + config)
sudo dnf remove levoile            # Fedora/RHEL  — nettoie tout (convention RPM)
sudo apk del levoile               # Alpine        — conserve par défaut
```

Le **preremove** stoppe le service, désactive le kill switch via `levoile-ctl`,
supprime l'interface `levoile0`. Le **postremove** conserve par défaut
`/etc/levoile/` et le user `levoile` (évite la perte de config en cas de
réinstall) ; le mode purge (Debian `purge`, RPM instance count = 0) supprime
tout.

## Capabilities — pourquoi systemd `AmbientCapabilities=` et pas `setcap`

[ADR-04, architecture.md §1333-1336] — les capabilities POSIX (`CAP_NET_ADMIN`,
`CAP_NET_RAW`) sont attribuées via le unit systemd, **pas** via `setcap` sur le
binaire. `setcap` est perdu à chaque remplacement du binaire (update apt/dnf),
ce qui forcerait une ré-application post-upgrade. `AmbientCapabilities` dans le
unit persiste et s'applique au démarrage du service.

## Signature Ed25519 des paquets

**Hors scope story 7.2.** Story 7.4 ajoute la signature Ed25519 détachée de
chaque `.deb`/`.rpm`/`.apk`/`.exe` via un hook post-build GoReleaser (master key
air-gapped / YubiKey — NFR22g). Les paquets générés par la présente story
sont **non signés** ; `apk add --allow-untrusted` est donc requis en attendant.

## Publication AUR

Le paquet AUR `levoile` est publié automatiquement à chaque release GitHub
via [.github/workflows/aur-publish.yml](../.github/workflows/aur-publish.yml)
(story 7.3). Le PKGBUILD extrait directement le `.deb` upstream → strictement
les mêmes fichiers et chemins que les paquets Debian/Fedora/Alpine.

Procédure mainteneur (premier setup + secrets GitHub + rollback) :
[docs/aur-release.md](../docs/aur-release.md).

```bash
# Utilisateur final :
yay -S levoile
```

Fichiers sources : [arch/PKGBUILD](arch/PKGBUILD),
[arch/levoile.install](arch/levoile.install), [arch/.SRCINFO](arch/.SRCINFO).

## Références

- [Story 7.2 complète](../_bmad-output/implementation-artifacts/7-2-paquets-linux-deb-rpm-apk-via-goreleaser-nfpm.md)
- [Architecture — ADR-04 capabilities systemd](../_bmad-output/planning-artifacts/architecture.md#L1333)
- [PRD — NFR22/23/24 matrice tests + dépendances](../_bmad-output/planning-artifacts/prd.md#L547)
- [Validation manuelle VM — docs/testing/7-2-packaging-manuel.md](../docs/testing/7-2-packaging-manuel.md)
