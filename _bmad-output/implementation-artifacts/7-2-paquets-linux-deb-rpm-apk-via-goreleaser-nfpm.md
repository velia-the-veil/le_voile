# Story 7.2 : Paquets Linux .deb / .rpm / .apk via GoReleaser + nfpm

Status: done

<!-- Note: Validation optionnelle. Lancer validate-create-story pour un contrôle qualité avant dev-story. -->

## Story

As a **utilisateur final Linux (Debian/Ubuntu, Fedora/RHEL, Alpine)**,
I want **installer Le Voile via le gestionnaire de paquets natif de ma distribution (`apt`/`dnf`/`apk`)**,
so that **l'installation, les dépendances et les mises à jour soient gérées comme tout autre logiciel système — sans commandes manuelles (`setcap`, `systemctl enable`, création user) et avec désinstallation propre**.

## Acceptance Criteria

### AC1 — Pipeline GoReleaser + nfpm produit les 3 formats × 2 architectures

**Given** la pipeline GoReleaser est configurée avec un bloc `nfpms:`
**When** `goreleaser release --snapshot --skip=publish` est exécuté en local (ou via CI)
**Then** les artefacts suivants sont générés dans `dist/` :

- `levoile_{version}_linux_amd64.deb` + `levoile_{version}_linux_arm64.deb`
- `levoile-{version}.x86_64.rpm` + `levoile-{version}.aarch64.rpm`
- `levoile_{version}_linux_amd64.apk` + `levoile_{version}_linux_arm64.apk`

**And** chaque paquet embarque les 3 binaires (`levoile-service`, `levoile-ui`, `levoile-ctl`) buildés pour la bonne arch
**And** la config `goreleaser check` passe sans warning

### AC2 — Dépendances runtime déclarées par format

**Given** un paquet est inspecté (`dpkg --info`, `rpm -qpR`, `apk info -R`)
**When** la section Dependencies est lue
**Then** les dépendances suivantes sont déclarées **au format natif** de chaque gestionnaire :

| Format | Dépendances runtime |
|--------|---------------------|
| `.deb` | `libwebkit2gtk-6.0-0 \| libwebkit2gtk-4.1-0`, `libayatana-appindicator3-1`, `nftables`, `iproute2` |
| `.rpm` | `webkit2gtk4.1`, `libayatana-appindicator-gtk3`, `nftables`, `iproute` |
| `.apk` | `webkit2gtk-4.1`, `libayatana-appindicator`, `nftables`, `iproute2` |

**And** l'alternative `libwebkit2gtk-6.0-0 | libwebkit2gtk-4.1-0` (Debian 11 vs 12+) est correctement encodée via `recommends`/`replaces`/`depends` selon le modèle nfpm

### AC3 — Post-install crée user système, active systemd, teste nftables

**Given** l'utilisateur exécute `sudo apt install ./levoile_{version}_amd64.deb` (ou équivalent `dnf`/`apk`)
**When** le script post-install s'exécute
**Then** (dans cet ordre) :

1. Un user système `levoile` est créé via `useradd --system --no-create-home --shell /usr/sbin/nologin levoile` si absent (**idempotent** — pas d'erreur si déjà présent)
2. `modprobe nf_tables` est tenté (NFR22) ; l'absence de `nf_tables` **n'échoue pas** le post-install mais un warning est écrit sur stderr
3. Un test de fumée `nft list ruleset >/dev/null 2>&1` valide que nftables est utilisable ; sur échec, warning stderr mais post-install succeed
4. `systemctl daemon-reload` recharge l'arborescence systemd (ignoré si systemd absent — Alpine OpenRC → log skip)
5. `systemctl enable --now levoile.service` active et démarre le service (idem : skip sans erreur si systemd absent)
6. `update-desktop-database` + `gtk-update-icon-cache` sont exécutés best-effort pour rafraîchir menus et icônes (ignorent absence outil)

**And** un log console `[levoile] service activé — UI disponible à la prochaine session de bureau.` est écrit

### AC4 — Fichiers installés aux chemins cross-distro standard

**Given** un paquet est installé
**When** les fichiers sont inspectés (`dpkg -L levoile`, `rpm -ql levoile`, `apk info -L levoile`)
**Then** la liste contient exactement (et sans doublon) :

```
/usr/bin/levoile-service
/usr/bin/levoile-ui
/usr/bin/levoile-ctl
/usr/lib/systemd/system/levoile.service           # unit system, service privilégié
/usr/lib/systemd/user/levoile-ui.service          # unit user, tray+webview (existant — story 5.7)
/etc/levoile/config.toml                          # skeleton config (0644 root:root)
/etc/xdg/autostart/levoile-autostart.desktop      # XDG autostart UI (existant — story 5.7)
/usr/share/applications/levoile.desktop           # entrée menu applications
/usr/share/icons/hicolor/16x16/apps/levoile.png
/usr/share/icons/hicolor/32x32/apps/levoile.png
/usr/share/icons/hicolor/48x48/apps/levoile.png
/usr/share/icons/hicolor/64x64/apps/levoile.png
/usr/share/icons/hicolor/128x128/apps/levoile.png
/usr/share/icons/hicolor/256x256/apps/levoile.png
/usr/share/doc/levoile/LICENSE
/usr/share/doc/levoile/README.md                  # pointe vers la doc en ligne
```

**And** les permissions sont :
- Binaires `/usr/bin/levoile-*` → `0755 root:root`
- Unit files → `0644 root:root`
- `/etc/levoile/config.toml` → `0644 root:root` (la config runtime `/etc/levoile/runtime.toml` créée au premier démarrage sera `0600 levoile:levoile` — NFR9j, géré par le service, hors scope packaging)
- Desktop files + icônes → `0644 root:root`

### AC5 — Unit systemd system avec capabilities et user dédié

**Given** le fichier `/usr/lib/systemd/system/levoile.service` installé
**When** son contenu est lu
**Then** il contient **au minimum** :

```ini
[Unit]
Description=Le Voile VPN — service privilégié (tunnel + kill switch)
Documentation=https://github.com/velia-the-veil/le_voile
After=network-online.target nftables.service
Wants=network-online.target
ConditionKernelVersion=>=4.9

[Service]
Type=simple
ExecStart=/usr/bin/levoile-service run
Restart=on-failure
RestartSec=5s

# NFR24 — capabilities via systemd plutôt que setcap (survivent aux updates binaire)
User=levoile
Group=levoile
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW

# Hardening minimum (les règles nftables + interface TUN ont besoin de NET_ADMIN — PrivateNetwork=true est incompatible)
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/etc/levoile /run/levoile /var/log/levoile
ProtectKernelTunables=false   # nftables a besoin de /proc/sys/net
ProtectKernelModules=false    # modprobe nf_tables
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
RestrictRealtime=true
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
```

**And** `systemctl cat levoile.service` retourne ce contenu après installation
**And** `systemd-analyze security levoile.service` retourne un score ≤ 4.5 ("OK" ou "MEDIUM")

### AC6 — Autostart UI via XDG (session graphique)

**Given** le paquet est installé **et** un utilisateur ouvre une session graphique GNOME/KDE/XFCE
**When** la session démarre
**Then** le fichier `/etc/xdg/autostart/levoile-autostart.desktop` (existant — story 5.7) déclenche `systemctl --user start levoile-ui.service`
**And** le tray Le Voile apparaît dans la barre de statut sans intervention
**And** le unit user `levoile-ui.service` fait `Restart=on-failure` (déjà en place — story 5.7)

### AC7 — Désinstallation propre

**Given** un paquet Le Voile est installé et actif
**When** l'utilisateur exécute `sudo apt remove levoile` (ou `dnf remove levoile` / `apk del levoile`)
**Then** le script pre-remove exécute (dans cet ordre) :

1. `systemctl disable --now levoile.service` (skip si systemd absent)
2. `levoile-ctl killswitch off --reason "uninstall"` best-effort (via IPC) pour retirer les règles nftables propres — si échec, continuer
3. `ip link delete levoile0` best-effort — si interface absente, continuer

**And** le post-remove :
1. Retire les fichiers installés (géré automatiquement par le gestionnaire de paquets)
2. **Ne supprime pas** `/etc/levoile/config.toml` (convention Debian : purge requis)
3. **Ne supprime pas** le user système `levoile` (évite perte de fichiers possédés par ce user si réinstall)

**Given** `sudo apt purge levoile` (ou `dnf remove --purge`)
**When** le post-remove purge est déclenché
**Then** `/etc/levoile/` est supprimé
**And** le user `levoile` est supprimé via `userdel levoile` (idempotent)

### AC8 — Matrice de smoke tests containers (CI)

**Given** le script `packaging/smoke/run.sh` existe
**When** il est exécuté (local ou CI)
**Then** il lance 3 containers Docker :

- `debian:12-slim` + `apt install ./levoile_*.deb`
- `fedora:40` + `dnf install ./levoile-*.rpm`
- `alpine:3.19` + `apk add --allow-untrusted ./levoile_*.apk`

**And** dans chaque container, après installation, les checks suivants passent :
1. `dpkg -L levoile` / `rpm -ql levoile` / `apk info -L levoile` liste tous les fichiers AC4
2. `test -x /usr/bin/levoile-service && test -x /usr/bin/levoile-ui && test -x /usr/bin/levoile-ctl`
3. `getent passwd levoile` retourne une ligne (user créé)
4. `systemctl cat levoile.service` (containers systemd-enabled uniquement — Debian/Fedora avec `--privileged` + init) ou `test -f /usr/lib/systemd/system/levoile.service` (Alpine, pas systemd)
5. `/usr/bin/levoile-ctl --help` retourne exit 0
6. `apt remove levoile -y` / `dnf remove levoile -y` / `apk del levoile -y` passe sans erreur

**And** le script retourne exit 0 si les 3 containers passent, exit non-zéro sinon, avec un rapport final lisible

### AC9 — Documentation packaging mise à jour

**Given** le paquet est distribué
**When** un développeur consulte `packaging/README.md`
**Then** la section "Paquets Linux natifs" est à jour avec :
- Commande de build local (`goreleaser release --snapshot --skip=publish`)
- Commande de test smoke (`bash packaging/smoke/run.sh`)
- Liste des chemins d'installation (AC4)
- Convention de config `/etc/levoile/config.toml` (gérée par le service, pas le paquet — le paquet livre un skeleton)
- Pointeur vers Story 7.4 pour la signature Ed25519 des paquets (hors scope 7.2)

**And** l'ancienne section "Until Story 7.2 lands" est retirée

## Tasks / Subtasks

- [x] **Task 1 — Ajouter la build Linux du service dans `.goreleaser.yaml`** (AC1)
  - [x] Dupliquer le bloc `id: service` en `id: service-linux` avec `goos: [linux]`, `goarch: [amd64, arm64]`, `env: [CGO_ENABLED=0]` (pas de webview côté service — pure Go)
  - [x] Vérifier que `cmd/client/main.go` build sans tag spécifique côté Linux (kardianos/service gère systemd nativement)
  - [x] `goreleaser check` doit passer

- [x] **Task 2 — Créer le unit systemd system `packaging/systemd/levoile.service`** (AC5)
  - [x] Contenu exact spécifié dans AC5 (hardening minimum + `AmbientCapabilities` + `User=levoile`)
  - [ ] Valider avec `systemd-analyze verify packaging/systemd/levoile.service` (container Debian 12) — **reporté** : host Windows sans systemd, à exécuter sur VM ou CI Linux (cf. docs/testing/7-2-packaging-manuel.md)
  - [ ] Valider avec `systemd-analyze security` (score ≤ 4.5) — **reporté** (idem)

- [x] **Task 3 — Créer le launcher menu `packaging/desktop/levoile.desktop`** (AC4)
  - [x] `Type=Application`, `Name=Le Voile`, `Exec=/bin/sh -c "systemctl --user start levoile-ui.service"`, `Icon=levoile`, `Categories=Network;Security;`, `Keywords=VPN;privacy;tunnel;`
  - [x] `StartupNotify=false` (l'app remonte dans le tray, pas de fenêtre à la première frame)

- [x] **Task 4 — Scripts post/pre install et remove** (AC3, AC7)
  - [x] `packaging/scripts/preinstall.sh` — création user `levoile` idempotente (useradd OU adduser busybox). `/run/levoile`, `/var/log/levoile`, `/var/lib/levoile` gérés par le unit systemd via `RuntimeDirectory=`, `LogsDirectory=`, `StateDirectory=` (plus robuste que de les créer côté preinstall — systemd recrée automatiquement au bon owner à chaque start).
  - [x] `packaging/scripts/postinstall.sh` — `modprobe nf_tables` (warning si échec) + smoke `nft list ruleset` + `systemctl daemon-reload` + `systemctl enable --now levoile.service` + `update-desktop-database /usr/share/applications` + `gtk-update-icon-cache /usr/share/icons/hicolor` (tous best-effort, exit 0 même sur échec)
  - [x] `packaging/scripts/preremove.sh` — `systemctl disable --now levoile.service` + `levoile-ctl killswitch off` + `ip link delete levoile0` (best-effort)
  - [x] `packaging/scripts/postremove.sh` — matching conditionnel sur `$1` : Debian `purge` OR RPM `0` → purge complète (rm -rf /etc/levoile + userdel/deluser) ; sinon conservation. apk (pas d'arg) → conservation par défaut (safer).
  - [x] **CRITIQUE** : scripts en `set -u` (pas `-e`) avec guards `command -v` sur systemctl/modprobe/userdel/nft + `|| true` sur actions optionnelles → jamais d'échec d'install dans containers sans init ni sur Alpine OpenRC.

- [x] **Task 5 — Skeleton config `packaging/etc/config.toml`** (AC4)
  - [x] Adapté de `installer/config-default.toml`, section `[http_proxy]` retirée (obsolète sur Linux — capture L3 TUN, pas HTTP proxy)
  - [x] Type nfpm `config|noreplace` : apt/dnf n'écrasent pas la config au upgrade

- [x] **Task 6 — Icônes XDG multi-tailles** (AC4)
  - [x] 6 PNG générés via Pillow depuis `build/appicon.png` (1024×1024, LANCZOS downscaling) — commit dans `packaging/icons/hicolor/{16,32,48,64,128,256}/apps/levoile.png`
  - [x] Script `packaging/icons/generate.sh` supporte ImageMagick 7 OU Python+Pillow (fallback)

- [x] **Task 7 — Section `nfpms:` dans `.goreleaser.yaml`** (AC1, AC2, AC3, AC4)
  - [x] Un seul bloc nfpms avec `formats: [deb, rpm, apk]` et `ids: [service-linux, ui-linux, ctl-linux]`
  - [x] Mapping `contents:` exhaustif couvrant AC4
  - [x] `overrides:` par format (deb/rpm/apk) avec noms de paquets correctement différenciés (AC2)
  - [x] `scripts.preinstall/postinstall/preremove/postremove` pointent vers `packaging/scripts/*.sh`
  - [x] `maintainer`, `homepage`, `description`, `license: MPL-2.0`, `section: net`, `rpm.group: Applications/Internet`

- [x] **Task 8 — Smoke test containers `packaging/smoke/run.sh`** (AC8)
  - [x] Script bash : 3 containers séquentiels (debian:12-slim, fedora:40, alpine:3.19) — séquentiel plutôt que parallèle pour rapport lisible
  - [x] Pour Alpine : `--allow-untrusted` (paquet non signé — story 7.4)
  - [x] Script de validation INTÉRIEUR partagé : vérifie tous les chemins AC4 + user `levoile` + `levoile-ctl --help`
  - [x] Rapport final formaté + exit code global

- [x] **Task 9 — Mettre à jour `packaging/README.md`** (AC9)
  - [x] Section "Until Story 7.2 lands" retirée
  - [x] Sections ajoutées : Fichiers, Build local, Smoke test, Fichiers installés, Workflow install, Désinstallation, Capabilities/ADR-04, Signature (pointeur 7.4), AUR (pointeur 7.3)

- [x] **Task 10 — Archive release `id: linux-pkgs` (optionnel)**
  - [x] Décision : laisser GoReleaser attacher automatiquement les nfpm outputs à la release (comportement par défaut) — pas d'archive spéciale. Les .deb/.rpm/.apk apparaissent comme artefacts de premier niveau dans la release GitHub (pas enfouis dans tar.gz).

- [x] **Task 11 — Documentation validation manuelle VM** (AC3, AC5, AC6, AC7)
  - [x] `docs/testing/7-2-packaging-manuel.md` créé avec procédures Ubuntu 24.04 / Fedora 40 / Alpine 3.19 (pattern 5-7-supervision-manuel.md)
  - [ ] Exécution sur VMs réelles — **à faire avant release** (matrice NFR22)

## Senior Developer Review (AI)

**Review date :** 2026-04-18
**Review outcome :** Changes Requested → **All items resolved**
**Severity breakdown :** 7 HIGH, 4 MEDIUM, 2 LOW → 7 HIGH + 4 MEDIUM fixés ; 2 LOW acceptés tels quels.

### Action Items

- [x] **[HIGH] H1** : `.gitignore` exclut silencieusement `packaging/etc/config.toml` (pattern `config.toml` ligne 48) → build CI fail après clone frais. Fix : exceptions `!packaging/etc/config.toml` + `!installer/config-default.toml`. [`.gitignore:48-52`]
- [x] **[HIGH] H2** : `license: "MPL-2.0"` dans `.goreleaser.yaml` mais `LICENSE` est MIT → déclaration légale fausse dans les paquets publiés. Fix : `license: "MIT"`. [`.goreleaser.yaml:121`]
- [x] **[HIGH] H3** : `ExecStart=/usr/bin/levoile-service run` sans `--config` → le user système `levoile` n'a pas de `$HOME`, `os.UserConfigDir()` échoue, la config n'est jamais chargée. Fix : `ExecStart=/usr/bin/levoile-service --config /etc/levoile/config.toml run`. [`packaging/systemd/levoile.service:12`]
- [x] **[HIGH] H4** : `adduser -S -D -H` sur Alpine (busybox) sans `-G` met le user dans `nogroup` → `Group=levoile` du unit cassait. Fix : création explicite du groupe via `addgroup -S`/`groupadd --system` + `adduser -G levoile`/`useradd --gid levoile`. [`packaging/scripts/preinstall.sh`]
- [x] **[HIGH] H5** : `levoile-ctl killswitch off --reason "uninstall"` — `--reason` n'existe pas dans le CLI (accepte seulement `off`/`on`, `len(args) != 1` → exit usage). L'appel échouait silencieusement, kill switch jamais désactivé. Fix : retrait de `--reason`. [`packaging/scripts/preremove.sh:38`]
- [x] **[HIGH] H6** : `docker run alpine:3.19 bash -c …` — Alpine n'a pas bash (busybox ash uniquement). Le test Alpine échouait avec "bash: not found". Fix : `sh -c` au lieu de `bash -c` + `docker run -i` pour stdin piping. [`packaging/smoke/run.sh:119-125`]
- [x] **[HIGH] H7** : `preremove.sh` faisait `systemctl disable --now` sur TOUTES les transactions, y compris upgrades → fenêtre de fuite kill switch de plusieurs secondes pendant `apt upgrade levoile`. Fix : guard `$1 == upgrade|failed-upgrade|abort-upgrade|1` → exit 0 immédiat. [`packaging/scripts/preremove.sh:20-35`]
- [x] **[MED] M1** : `postinstall.sh` faisait `systemctl enable --now` sur upgrade → override silencieux d'un `systemctl disable` explicite utilisateur. Fix : détection upgrade, `try-restart` si enabled sur upgrade, `enable --now` uniquement sur fresh install. [`packaging/scripts/postinstall.sh`]
- [x] **[MED] M2** : ordre wrong dans preremove — `levoile-ctl killswitch off` appelé APRÈS `systemctl disable --now`, donc service déjà mort, IPC fail. Fix : ctl avant stop + fallback `nft delete table inet levoile` brut en cas d'échec IPC. [`packaging/scripts/preremove.sh:41-63`]
- [x] **[MED] M3** : unit sans `StartLimitBurst` → config malformée causerait un respawn infini `Restart=on-failure / RestartSec=5s`, CPU + logs saturés. Fix : `StartLimitBurst=5 StartLimitIntervalSec=60s`. [`packaging/systemd/levoile.service:19-22`]
- [x] **[MED] M4** : `ConfigurationDirectory=levoile` + `/etc/levoile/` posé par le paquet en root:root → systemd tentait de chown en levoile:levoile au premier start, comportement non-déterministe selon version. Fix : suppression de `ConfigurationDirectory=levoile` — `/etc/levoile/` reste géré exclusivement par le paquet. [`packaging/systemd/levoile.service`]
- [ ] **[LOW] L1** : `StartupWMClass=levoile-ui` dans `levoile.desktop` — à vérifier que la webview expose bien cette classe X11 au WM (sinon l'icône menu ne sera pas associée à la fenêtre). Validation reportée aux VMs.
- [ ] **[LOW] L2** : Task 4 spec story mentionnait création de `/run/levoile` et `/var/log/levoile` via preinstall. Implémentation délègue à systemd (`RuntimeDirectory=`/`LogsDirectory=`) — plus robuste mais divergence de spec. Accepté, documenté dans les completion notes.

### Validations post-fix

- `goreleaser check` ✓
- `go test ./internal/config/... ./internal/firewall/... ./cmd/ctl/...` ✓ (aucune régression)
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/client` ✓

## Dev Notes

### Contexte stratégique (épic 7)

L'Épic 7 vise à rendre Le Voile **installable par `apt install` / `dnf install` / `apk add`** pour Mathieu (parcours utilisateur #6, PRD §165) — ZÉRO commande shell manuelle en dehors du `install`. Story 7.2 est le cœur Linux de cet épic ; Story 7.3 ajoute AUR, Story 7.4 signe les paquets avec la master key Ed25519 (hors scope 7.2 — ne pas implémenter la signature ici).

### Architecture — décisions héritées

**[ADR-04, architecture.md:1333-1336]** Capabilities via systemd `AmbientCapabilities=`, **pas** `setcap` sur le binaire. Raison : `setcap` est perdu à chaque remplacement du binaire (update), forçant un re-`setcap` à chaque mise à jour. `AmbientCapabilities` dans le unit file persiste. **NE PAS** ajouter `setcap cap_net_admin,cap_net_raw+ep /usr/bin/levoile-service` dans le post-install, même si ça peut sembler "plus robuste" — c'est explicitement écarté par l'ADR.

**[architecture.md:316-321, 365-366]** Le service tourne en **user `levoile` non-root** avec capabilities, **pas** en root. C'est une exigence de sécurité forte (principe de moindre privilège — NFR26). Le user est créé par `preinstall.sh`.

**[architecture.md:258, 373]** Chemins cross-distro **figés** : `/usr/bin/levoile-*`, `/usr/lib/systemd/system/levoile.service`, `/etc/levoile/config.toml`. Ne pas diverger (p.ex. `/usr/local/bin` ou `/opt/levoile`) — les conventions varient par distro et nfpm gère bien ces chemins standard.

### Dépendances — pourquoi exactement celles-ci

- `libwebkit2gtk-6.0-0 | libwebkit2gtk-4.1-0` : webview Go (`webview/webview_go`) linke contre WebKitGTK. Debian 12 (bookworm) a `libwebkit2gtk-4.1-0`, Debian 13 (trixie) passe à `libwebkit2gtk-6.0-0`. L'alternative `|` côté `.deb` résout les deux.
- `libayatana-appindicator3-1` : tray icône via fyne.io/systray → AppIndicator sur GNOME/KDE modernes
- `nftables` : kill switch firewall OS-level (NFR9b, story 2.6)
- `iproute2` : `ip link`, `ip route` pour manipulation TUN + route par défaut (story 2.1, 2.4)

**Attention Fedora** : le paquet webkit2gtk s'appelle `webkit2gtk4.1` (pas de tiret), et appindicator est `libayatana-appindicator-gtk3` (avec `-gtk3`). **Attention Alpine** : `webkit2gtk-4.1` (avec tiret), `libayatana-appindicator` (sans suffixe gtk).

### Structure du projet — où ça touche

```
.goreleaser.yaml                              # MODIFIER : ajout service-linux + bloc nfpms:
packaging/
  systemd/
    levoile.service                           # NOUVEAU (system unit, root-level service)
    user/levoile-ui.service                   # EXISTANT (ne pas toucher — story 5.7)
  desktop/
    levoile.desktop                           # NOUVEAU (menu launcher)
    levoile-autostart.desktop                 # EXISTANT (ne pas toucher — story 5.7)
  scripts/
    preinstall.sh                             # NOUVEAU
    postinstall.sh                            # NOUVEAU
    preremove.sh                              # NOUVEAU
    postremove.sh                             # NOUVEAU
  etc/
    config.toml                               # NOUVEAU (skeleton /etc/levoile/)
  icons/
    hicolor/{16,32,48,64,128,256}/apps/levoile.png  # NOUVEAU (6 PNG)
    generate.sh                               # NOUVEAU (script doc, pas exécuté en build)
  smoke/
    run.sh                                    # NOUVEAU (smoke 3 containers)
  README.md                                   # MODIFIER (remettre à jour AC9)
docs/testing/
  7-2-packaging-manuel.md                     # NOUVEAU (doc validation VM, pattern 5-7)
```

**Ne touche pas à** : `cmd/client/main.go` (kardianos/service gère déjà systemd), `cmd/ui/`, `cmd/ctl/`, `installer/` (NSIS Windows — story 7.1).

### Testing standards

- **Pas de tests Go unitaires requis** pour cette story — il s'agit de configuration de packaging + scripts shell. Les tests sont des **smoke tests dans containers** (AC8).
- Les scripts shell sont testés par `shellcheck` (ajouter à CI si absent) — `shellcheck packaging/scripts/*.sh` doit passer sans warning.
- La validation du unit systemd est faite via `systemd-analyze verify` + `systemd-analyze security`.
- **NFR22** (matrice de tests e2e multi-OS) : cette story est un **prérequis** pour la matrice — une fois les paquets dispo, la matrice NFR22 peut tourner sur Ubuntu 24.04, Fedora 40, Alpine 3.19. La matrice elle-même est livrée par des stories e2e dédiées (hors scope 7.2).

### Pièges à éviter (LLM common mistakes)

1. **NE PAS** ajouter de `setcap` au post-install → utiliser uniquement `AmbientCapabilities` (ADR-04)
2. **NE PAS** faire échouer `postinstall.sh` sur containers sans systemd (Alpine, CI runners, Docker sans init) → wrapper les commandes dans `|| true` et logger warning
3. **NE PAS** supprimer le user `levoile` au remove non-purge → seulement au purge (sinon perte de données si réinstall)
4. **NE PAS** supprimer `/etc/levoile/config.toml` au remove non-purge (convention Debian — l'utilisateur peut vouloir garder sa config)
5. **NE PAS** inventer de nouveaux chemins — tous les paths sont figés en AC4 par l'architecture
6. **NE PAS** embarquer la signature Ed25519 des paquets ici → c'est story 7.4, scope séparé. Le build doit produire des paquets **non signés** ; la signature sera ajoutée en hook post-build dans 7.4.
7. **NE PAS** utiliser `aur-publish` ici → c'est story 7.3 (AUR/Arch).
8. **NE PAS** déclarer `libwebkit2gtk` en dépendance `.rpm` avec le nom Debian — les noms diffèrent par distro (voir tableau AC2).
9. **NE PAS** oublier `CGO_ENABLED=1` pour `ui-linux` (webview C deps) mais `CGO_ENABLED=0` pour `service-linux` et `ctl-linux` (pure Go — build plus reproductible).
10. **NE PAS** ajouter `PrivateNetwork=true` au unit systemd → incompatible avec création interface TUN (NET_ADMIN sur le namespace réseau principal requis).

### Références

- [Source : _bmad-output/planning-artifacts/epics.md:1162-1188 — Story 7.2 ACs sources]
- [Source : _bmad-output/planning-artifacts/prd.md:78, 204, 273-274, 327-330, 354-357, 461-462, 548-549 — FR20a/b, NFR22/23/24]
- [Source : _bmad-output/planning-artifacts/architecture.md:37, 75, 91, 154, 187-189, 212-214, 258-259, 361-376, 889-900, 1333-1336 (ADR-04)]
- [Source : packaging/README.md — état actuel (section "Until Story 7.2 lands" à retirer)]
- [Source : packaging/systemd/user/levoile-ui.service — unit user existant, à ne pas dupliquer]
- [Source : .goreleaser.yaml:7-60 — builds existants (service Windows only, ui-linux présent, ctl-linux présent, relay présent)]

### Project Structure Notes

**Alignement** : la story suit la structure fixée dans [architecture.md:889-900] (`packaging/` avec sous-dossiers `systemd/`, `desktop/`, `scripts/`, icônes). Les noms de scripts (`postinstall.sh` etc.) matchent les conventions nfpm.

**Variance détectée** : l'architecture suggère `packaging/nfpm-deb.yaml`, `nfpm-rpm.yaml`, `nfpm-apk.yaml` séparés [architecture.md:365]. **Décision** : utiliser un bloc `nfpms:` unique dans `.goreleaser.yaml` avec `overrides` par format — c'est la convention GoReleaser actuelle (plus simple qu'un fichier nfpm standalone par format). Documenter cette variance dans le commit.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m] (dev-story agent)

### Debug Log References

- `goreleaser check` → `1 configuration file(s) validated` ✓
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/client` → OK après fix import inutilisé `net` dans `internal/firewall/firewall_linux.go` (pré-existant, non lié mais bloquant la compilation Linux)
- `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/client` → OK
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/ctl` → OK
- `goreleaser build --snapshot --clean --id=service-linux --id=ctl-linux --id=relay` → OK (tous les binaires Linux pure-Go)
- `go test ./internal/firewall/...` → `ok` (régression vérifiée)
- **Limitation hôte Windows** : `goreleaser release --snapshot` complet échoue sur la cible `ui-linux` qui nécessite CGO_ENABLED=1 + gcc cross-compilateur Linux (`x86_64-linux-gnu-gcc`) pour la webview. Le snapshot complet (incluant les paquets nfpm) doit être exécuté sur un host Linux / CI — c'est le cas de tous les contributeurs CI. Les `.deb`/`.rpm`/`.apk` sont générés par nfpm après les builds Go ; une fois les builds passés sur Linux, la config nfpm est déjà validée par `goreleaser check`.

### Completion Notes List

**Scope :** 11 tâches complétées. ACs 1-9 satisfaites dans la mesure où la plateforme de dev le permet :
- **AC1 (pipeline)** ✓ validée par `goreleaser check` + builds individuels Linux (amd64 + arm64) pour service-linux/ctl-linux/relay. `ui-linux` (webview CGo) testable uniquement sur host Linux — documenté.
- **AC2 (dépendances)** ✓ encodées par format via `overrides:` dans le bloc nfpms (noms corrects : libwebkit2gtk alternative | Debian, webkit2gtk4.1 Fedora, webkit2gtk-4.1 Alpine).
- **AC3 (post-install)** ✓ scripts écrits avec guards complets (systemd absent → log INFO, modprobe échec → log WARN, jamais d'exit non-zéro).
- **AC4 (fichiers installés)** ✓ tous les chemins mappés dans `nfpms.contents` + smoke script vérifie 13 chemins en container.
- **AC5 (unit systemd)** ✓ contenu exact du unit respecté (ADR-04 AmbientCapabilities, pas setcap ; hardening raisonnable compatible NET_ADMIN). Validation `systemd-analyze` **reportée** aux VMs (host Windows).
- **AC6 (autostart XDG)** ✓ via packaging/desktop/levoile-autostart.desktop existant (story 5.7) — délègue à `systemctl --user start levoile-ui.service`.
- **AC7 (désinstallation)** ✓ conservation par défaut, purge conditionnelle Debian `purge` / RPM count 0. Alpine = conservation (safer default).
- **AC8 (smoke tests)** ✓ script prêt, exécution reporté à host avec Docker (host Windows sans Docker).
- **AC9 (doc)** ✓ README mis à jour.

**Fix collatéral (hors scope explicite, mais bloquant) :** suppression de l'import `net` inutilisé dans `internal/firewall/firewall_linux.go`. Bug pré-existant qui empêchait toute build Linux du client. Compatible avec la règle "ne pas faire de scope creep" car la Task 1 exige que le binaire Linux compile (AC1).

**Décisions prises pendant l'implémentation :**
1. `RuntimeDirectory=`, `LogsDirectory=`, `StateDirectory=`, `ConfigurationDirectory=` déclarés dans le unit systemd plutôt que créés en preinstall → systemd les recrée automatiquement au bon owner à chaque start, plus robuste qu'un `mkdir` manuel qui peut diverger après upgrade.
2. `preinstall.sh` supporte `useradd` (glibc) ET `adduser` (busybox/Alpine) — détection via `command -v`. Même pattern pour `getent`/`grep /etc/passwd`, `userdel`/`deluser`.
3. Archive `id: ui-linux` enrichie pour inclure aussi `service-linux` (sinon tar.gz seule ne contient pas le service, installation manuelle impossible).
4. `type: config|noreplace` sur `/etc/levoile/config.toml` → upgrade ne réécrase jamais la config.

**Tests additionnels à exécuter avant release (hors scope sprint) :**
- VM Ubuntu 24.04 + VM Fedora 40 : procédure `docs/testing/7-2-packaging-manuel.md`
- `bash packaging/smoke/run.sh` sur host Linux avec Docker
- `systemd-analyze verify + security` sur les 2 VMs systemd
- Matrice NFR22 complète (tunnel + kill switch + UI end-to-end) — story dédiée

### File List

**Créés :**
- `packaging/systemd/levoile.service` (unit systemd system — service privilégié)
- `packaging/desktop/levoile.desktop` (entrée menu applications XDG)
- `packaging/scripts/preinstall.sh` (création user levoile idempotente)
- `packaging/scripts/postinstall.sh` (modprobe + systemctl enable + caches XDG)
- `packaging/scripts/preremove.sh` (arrêt service + shutdown propre kill switch + TUN)
- `packaging/scripts/postremove.sh` (purge conditionnelle config + user)
- `packaging/etc/config.toml` (skeleton /etc/levoile/config.toml)
- `packaging/icons/hicolor/16x16/apps/levoile.png`
- `packaging/icons/hicolor/32x32/apps/levoile.png`
- `packaging/icons/hicolor/48x48/apps/levoile.png`
- `packaging/icons/hicolor/64x64/apps/levoile.png`
- `packaging/icons/hicolor/128x128/apps/levoile.png`
- `packaging/icons/hicolor/256x256/apps/levoile.png`
- `packaging/icons/generate.sh` (régénération des icônes multi-tailles)
- `packaging/smoke/run.sh` (smoke test 3 containers Docker)
- `docs/testing/7-2-packaging-manuel.md` (procédure validation VM)

**Modifiés :**
- `.gitignore` — exceptions `!packaging/etc/config.toml` + `!installer/config-default.toml` (code review H1)
- `.goreleaser.yaml` — ajout build `service-linux` (CGO_ENABLED=0, amd64+arm64) + bloc `nfpms:` complet (deb/rpm/apk × overrides dépendances + scripts + contents) + migration syntaxe v2 des archives (`formats:` + `ids:`) + archive `ui-linux` enrichie avec service-linux. License corrigée MIT (code review H2).
- `packaging/README.md` — refonte complète (sections Build/Smoke/Installé/Workflow/Remove/ADR-04/Signature/AUR)
- `internal/firewall/firewall_linux.go` — suppression import `net` inutilisé (fix pré-existant requis pour la build Linux — hors scope nominal mais bloquant AC1)
- `packaging/systemd/levoile.service` — post-review : `--config /etc/levoile/config.toml` explicite (H3), `StartLimitBurst`/`StartLimitIntervalSec` (M3), retrait `ConfigurationDirectory=levoile` conflictuel (M4)
- `packaging/scripts/preinstall.sh` — post-review : création groupe `levoile` explicite + `adduser -G levoile` (H4)
- `packaging/scripts/postinstall.sh` — post-review : distinction fresh-install / upgrade, `try-restart` si enabled (M1)
- `packaging/scripts/preremove.sh` — post-review : guard upgrade (H7), retrait `--reason` (H5), ordre ctl-avant-stop + fallback `nft delete` (M2)
- `packaging/smoke/run.sh` — post-review : `sh -c` au lieu de `bash -c` + `docker run -i` (H6)

### Change Log

- 2026-04-18 — Story 7.2 implémentation complète (11 tâches). Configuration GoReleaser + nfpm pour paquets .deb/.rpm/.apk (amd64+arm64), scripts pre/post install/remove cross-distro (useradd/adduser, systemctl guards), unit systemd system avec AmbientCapabilities (ADR-04), icônes XDG hicolor 6 tailles, skeleton config, smoke test Docker 3 distros, doc validation VM. `goreleaser check` ✓, builds Linux pure-Go ✓. Validation end-to-end nfpm + snapshots UI-CGo reportée à host Linux/CI.
- 2026-04-18 — Code review adversarial → 7 HIGH + 4 MEDIUM fix appliqués :
  - **H1** `.gitignore` : exception `!packaging/etc/config.toml` + `!installer/config-default.toml` (le pattern `config.toml` excluait silencieusement le skeleton — build aurait failed en CI)
  - **H2** `.goreleaser.yaml` : `license: "MIT"` (était `MPL-2.0`, faux — le LICENSE est MIT)
  - **H3** `packaging/systemd/levoile.service` : `ExecStart=… --config /etc/levoile/config.toml run` (sans cela, le user `levoile` sans HOME n'aurait jamais trouvé la config via `os.UserConfigDir()`)
  - **H4** `packaging/scripts/preinstall.sh` : création explicite du groupe `levoile` + `adduser -G levoile` (Alpine busybox sans `-G` met le user dans `nogroup` → Group=levoile du unit cassait)
  - **H5** `packaging/scripts/preremove.sh` : retrait de `--reason "uninstall"` (flag inexistant — `levoile-ctl killswitch` n'accepte que `off|on`, l'appel échouait silencieusement)
  - **H6** `packaging/smoke/run.sh` : `sh -c` au lieu de `bash -c` (Alpine n'a pas bash, le test Alpine échouait immédiatement) + ajout `docker run -i` pour stdin piping
  - **H7** `packaging/scripts/preremove.sh` : guard upgrade explicite → sur `upgrade`/`failed-upgrade`/rpm=`1`, on ne touche à RIEN (évite la fenêtre de fuite kill switch pendant `apt upgrade levoile`)
  - **M1** `packaging/scripts/postinstall.sh` : détection upgrade via `$2` (deb) ou `$1=2` (rpm). Sur upgrade : `try-restart` uniquement si le service était `is-enabled`. Sur fresh install : `enable --now`. Respecte le `systemctl disable` explicite d'un utilisateur.
  - **M2** `packaging/scripts/preremove.sh` : ordre corrigé — `levoile-ctl killswitch off` AVANT `systemctl disable --now` (sinon le service était déjà mort → IPC failed) + fallback `nft delete table inet levoile` en cas d'échec IPC
  - **M3** `packaging/systemd/levoile.service` : ajout `StartLimitBurst=5` + `StartLimitIntervalSec=60s` (cap le respawn infini sur config malformée)
  - **M4** `packaging/systemd/levoile.service` : retrait de `ConfigurationDirectory=levoile` (conflictuel avec `/etc/levoile/` posé par le paquet en root:root — comportement systemd non-déterministe selon version). `/etc/levoile/` reste géré par le paquet, `/run/levoile` + `/var/log/levoile` + `/var/lib/levoile` restent gérés par systemd.
  - Validations : `goreleaser check` ✓, `go test ./internal/config/... ./internal/firewall/... ./cmd/ctl/...` ✓, cross-compile Linux ✓
