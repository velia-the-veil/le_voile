---
title: 'Isolation cross-platform — séparation windows/ / linux/ / android/'
slug: 'isolation-cross-platform-windows-linux-android'
created: '2026-04-26'
status: 'ready-for-dev'
stepsCompleted: [1, 2, 3, 4]
tech_stack:
  - 'Go 1.21+ (module unique, pas de cgo dans le code applicatif)'
  - 'Wails v2 (frontend embarqué via go:embed)'
  - 'WebView2 (Windows) / WebKit2GTK (Linux) — pas de C-binding direct (shellout uniquement)'
  - 'NSIS (Windows installer) + makensis'
  - 'goreleaser + nfpm (Linux deb/rpm/apk + AUR PKGBUILD)'
  - 'systemd (Linux services + user units), polkit'
  - 'WFP (Windows Filtering Platform) via netsh + APIs Win32'
  - 'nftables (Linux kill-switch)'
  - 'wintun.dll 0.14.1 (embarquée via go:embed Windows uniquement)'
  - 'kardianos/service (cross-OS service wrapper, abstrait SCM/systemd)'
  - 'Ed25519 (signature releases + registry)'
files_to_modify:
  - '353 fichiers Go (cmd/ + internal/) — déplacement majoritaire vers windows/ et linux/'
  - '~115 fichiers non-Go (frontend, installer, packaging, build, scripts, tools, deploy, configs racine)'
  - '.goreleaser.yaml (8 builds, 4 archives, 2 nfpm) + .goreleaser-linux-only.yaml (4 builds, 1 archive, 1 nfpm)'
  - 'Makefile (cibles wintun, clean, tun-test, release-*)'
  - '.github/workflows/release.yml + aur-publish.yml'
  - 'installer/build.sh + build.ps1 + levoile.nsi (paths internes)'
  - 'scripts/fetch-wintun.sh (TARGET_DIR + EMBED_GO path)'
code_patterns:
  - 'Build tags par suffixe de fichier (*_windows.go, *_linux.go, *_darwin.go, *_other.go) — pattern à abandonner pour les packages OS-spécifiques au profit de packages physiquement séparés par OS'
  - 'Stubs no-op cross-OS (firewall_stub.go, anomaly_stub.go, flush_other.go, icons_stub.go, singleton_stub.go) — à supprimer ou conserver selon la racine OS cible'
  - 'go:embed : 14 directives, toutes en chemin relatif au package source — safe si déplacement en bloc'
  - 'Aucun import "C" ou cgo dans le code applicatif (shellout via os/exec uniquement)'
  - 'Aucun chemin de ressource hard-codé en Go production (1 seul en test, négligeable)'
  - 'Tous imports OS-specifique convergent vers internal/service (point de collecte unique)'
  - 'Module Go unique (go.mod racine) — à conserver, sous-arbres windows/linux/android sont juste des chemins d''import'
test_patterns:
  - 'Tests unitaires par OS via suffixe _test.go + suffixe OS (*_windows_test.go, *_linux_test.go) — colocalisés avec le code testé, à déplacer ensemble'
  - 'Tests e2e opt-in via build tag //go:build e2e (combiné parfois avec && windows / && linux)'
  - 'Tests integration Linux via //go:build linux && integration'
  - 'Tests utilisent t.TempDir() — pas de chemins fragiles (préservation safe)'
  - 'Commandes shell e2e détectées au runtime (ipconfig, resolvectl, nft, ip route, netsh) — pas de hard-code de paths /usr/bin/X'
---

# Tech-Spec: Isolation cross-platform — séparation windows/ / linux/ / android/

**Created:** 2026-04-26

## Overview

### Problem Statement

Le code spécifique à un OS cohabite actuellement dans les **mêmes packages Go** (convention build-tags `*_windows.go` / `*_linux.go` / `*_darwin.go` / `*_other.go`) et dans des **dossiers racine partagés** (`frontend/`, `scripts/`, `tools/`). Cette cohabitation a déjà causé une régression cross-OS : modifier le code d'un OS a involontairement impacté l'autre. L'arrivée prochaine d'Android amplifie le risque, et l'absence d'isolation rend la maintenance multi-OS fragile (un import incorrect, une fonction renommée à la légère, et le binaire de l'autre OS casse silencieusement).

### Solution

Restructurer le repo en **trois racines OS** : `windows/`, `linux/`, `android/`. Chacune contient son propre `cmd/`, `internal/`, `frontend/`, installeur/packaging, scripts, build assets. Le partage est **explicite** : seul ce qui est strictement OS-agnostique reste à la racine (relais server-side, primitives 100 % portables, config repo, CI/release).

Migration **incrémentale**, un OS à la fois : Windows d'abord (le plus complexe — NSIS, WFP, wintun, .syso, scheduled task), puis Linux (nftables, systemd, polkit, AUR/deb/rpm), puis squelette Android. Tests verts (`go test ./...`, `go build ./...`, build d'installeur pour l'OS courant) à chaque jalon avant d'enchaîner.

Stratégie Go : pour chaque package `internal/<feature>/` qui contient des fichiers OS-specific, on crée `windows/internal/<feature>/` et `linux/internal/<feature>/` qui ne contiennent **que les fichiers de leur OS** (plus les fichiers communs du package, dupliqués si nécessaire). Les imports sont mis à jour vers la racine OS appropriée. Les binaires `cmd/client`, `cmd/ui`, `cmd/ctl` deviennent eux aussi par-OS (deux mains.go distincts, qui importent depuis leur racine OS respective).

### Scope

**In Scope:**
- Déplacement de tout fichier Go suffixé `_windows.go` / `_windows_test.go` / `*.syso` / `*.rc` vers `windows/internal/...` et `windows/cmd/...`
- Déplacement de tout fichier Go suffixé `_linux.go` / `_linux_test.go` / `*_other.go` / `*_darwin.go` (si darwin n'est pas une cible séparée — à confirmer) vers `linux/internal/...` et `linux/cmd/...`
- Duplication des fichiers Go communs (sans suffixe OS) dans `windows/internal/<feature>/` et `linux/internal/<feature>/` quand le package devient indivisible (cf. décisions techniques §)
- `installer/` → `windows/installer/` (NSIS, ico, bmp, build.ps1, build.sh)
- `packaging/` → `linux/packaging/` (deb/rpm/aur/systemd/polkit/desktop/icons)
- `build/windows/` → `windows/build/`
- `frontend/` → dupliqué en `windows/frontend/` et `linux/frontend/` (assets, index.html, src/, embed.go, contract_test.go)
- Scripts : `.ps1` → `windows/scripts/` ; `.sh` client-side → `linux/scripts/` ; scripts relais (`deploy/*.sh`) restent en `deploy/`
- `tools/wfp_repro/` → `windows/tools/` ; `tools/ipc_send/` → trier (Win-spécifique probablement) ; `tools/gen_icons.go` → trier
- Création de `android/` avec arborescence stub (`android/cmd/`, `android/internal/`, `android/frontend/`, README placeholder)
- Mise à jour des chemins dans `.goreleaser.yaml`, `.goreleaser-linux-only.yaml`, `Makefile`, `.github/workflows/*.yml`
- Mise à jour des `//go:embed` paths (frontend, wintun, ruleset.nft.tmpl)
- Mise à jour des chemins dans tests d'intégration et e2e
- Validation : `go build ./windows/...`, `go build ./linux/...`, `go test ./windows/...`, `go test ./linux/...`, build installeur Windows + paquet Linux complets à chaque jalon

**Out of Scope:**
- Implémentation effective Android (juste structure de dossiers + stub README)
- Refactor fonctionnel, renommage d'API publiques, changement de symboles Go
- Modification du protocole IPC, format config TOML, contrats relais, schéma blocklist
- Modification du code relais : `cmd/relay`, `cmd/genkey`, `cmd/genregistry`, `cmd/signpkg`, `cmd/verifypkg`, `cmd/verify-registry`, `internal/relay`, `internal/registry`, `deploy/` restent à la racine (server-side, OS-agnostique)
- Déduplication ultérieure du code partagé entre OS (la duplication est volontaire au début ; une étape 2 future pourra extraire `shared/internal/...` si pertinent)
- Mise à jour de la documentation produit (`README.md`, `SECURITY.md`, `docs/`) au-delà de la mention de la nouvelle structure

## Context for Development

### Codebase Patterns

- **Build tags Go par suffixe** (`*_windows.go`, `*_linux.go`, `*_darwin.go`, `*_other.go`) : pattern abandonné pour les packages OS-spécifiques au profit de packages physiquement séparés. Conservé pour les packages qui restent à la racine.
- **Stubs no-op cross-OS** : `firewall_stub.go`, `anomaly_stub.go`, `flush_other.go`, `icons_stub.go`, `singleton_stub.go`, `dnscache_other.go`, `sysproxy_stub.go`. Sortie : on **supprime** ces stubs des packages racine OS-spécifiques (ils ne servent qu'à compiler sur Darwin/autres). Si Darwin n'est plus une cible (à confirmer), suppression nette.
- **`//go:embed` (14 directives)** : tous chemins relatifs au package source. Préservés tant que les sous-dossiers d'assets sont déplacés en bloc avec leur fichier `.go` parent. Listing complet ci-dessous (§ Anchor points).
- **Aucun cgo** dans le code applicatif. Webview "cgo" en réalité = wrappers Go pur (le suffixe `_cgo` dans `webview_cgo_linux.go` est trompeur — vérifier en step 3 si vraiment pas de `import "C"`). Pas de bibliothèques C liées.
- **Aucun chemin de ressource hard-codé en Go prod.** Une seule string `./le_voile` dans un test (`internal/updater/installer_test.go:32`).
- **Tous les imports OS-spécifiques convergent vers `internal/service`** : c'est le point d'entrée unique du daemon (`cmd/client`) qui agrège firewall, tun, dns, routing, preflight, ipc, config. Conséquence : si on splitte ces packages, `cmd/client/main.go` doit aussi être splitté en `windows/cmd/client` et `linux/cmd/client`.
- **Module Go unique** (`go.mod` racine, `github.com/velia-the-veil/le_voile` ou similaire) : conservé. Sous-arbres `windows/`, `linux/`, `android/` sont des chemins d'import internes au module.
- **Goreleaser dual** (`.goreleaser.yaml` + `.goreleaser-linux-only.yaml`) : tous les `main:` paths à recâbler vers `windows/cmd/...` ou `linux/cmd/...` ou laissés à la racine selon la classification du binaire.

### Anchor Points — Inventaire Consolidé

#### A. Statistiques Go (cmd/ + internal/, 353 fichiers)

| Classification | Nombre | Sort |
|---|---|---|
| `shared` (compile partout) | 101 | Reste à la racine OU dupliqué selon le package |
| `test-shared` | 128 | Suit son code |
| `windows-only` | 36 | → `windows/internal/...` ou `windows/cmd/...` |
| `test-windows-only` | 19 | → `windows/...` |
| `linux-only` | 21 | → `linux/internal/...` ou `linux/cmd/...` |
| `test-linux-only` | 16 | → `linux/...` |
| `darwin-only` | 5 | **À trancher** : on cible Darwin ou on supprime ? |
| `test-darwin-only` | 2 | idem |
| `unix-shared` (Linux+Darwin via `!windows`) | 8 | → `linux/...` si Darwin abandonné, sinon dupliqué |
| `cross-os-stub` (no-op Darwin/other) | 5 | À supprimer si Darwin abandonné |
| `test-e2e` (cross-OS) | 7 | Avec leur package |
| Autres | 5 | Détail en step 3 |

#### B. Binaires `cmd/` — classification

| Binaire | Type | Action |
|---|---|---|
| `cmd/relay` | OS-agnostique (server-side) | **Reste à la racine** |
| `cmd/genkey`, `cmd/genregistry`, `cmd/signpkg`, `cmd/verifypkg`, `cmd/verify-registry` | OS-agnostiques (utilitaires crypto/registry) | **Restent à la racine** |
| `cmd/client` | Binaire daemon — importe `internal/service` qui lui-même tire firewall/tun/dns/routing/ipc/preflight/config (tous OS-spécifiques) | **Splitté** : `windows/cmd/client/` + `linux/cmd/client/` |
| `cmd/ui` | Binaire UI Wails — `service_windows.go`, `service_linux.go`, `app.rc`, `app_windows.syso`, importe `internal/ui` (OS-spécifique) | **Splitté** : `windows/cmd/ui/` + `linux/cmd/ui/` |
| `cmd/ctl` | CLI opérateur — importe `internal/ipc` (OS-spécifique : pipe Win vs socket Unix) | **Splitté** : `windows/cmd/ctl/` + `linux/cmd/ctl/` |

#### C. Packages `internal/` — destination

| Package | Statut | Destination |
|---|---|---|
| `internal/relay` | OS-agnostique pur | **Reste à la racine** |
| `internal/registry` | OS-agnostique pur | **Reste à la racine** |
| `internal/crypto` | OS-agnostique pur | **Reste à la racine** |
| `internal/blocklist` | OS-agnostique (à reconfirmer en step 3 — agent Explore l'a noté agnostique) | **Reste à la racine** |
| `internal/firewall` | OS-spécifique (WFP / nftables) | **Dédoublé** : `windows/internal/firewall/` + `linux/internal/firewall/` |
| `internal/tun` | OS-spécifique + wintun.dll embarquée | **Dédoublé** + DLL → `windows/internal/tun/wintun/` |
| `internal/dns` | OS-spécifique (cmd_windows.go, manager_*.go, flush_*.go, kill_switch*.go) | **Dédoublé** |
| `internal/routing` | OS-spécifique | **Dédoublé** |
| `internal/ui` | OS-spécifique (icons embarqués, webview, singleton, sysproxy, service_command) | **Dédoublé** + assets icons → `windows/internal/ui/icons/` (`.ico`) et `linux/internal/ui/icons/` (`.png`) |
| `internal/service` | OS-spécifique (kardianos wrapping SCM Win / systemd Linux) | **Dédoublé** |
| `internal/config` | OS-spécifique (chemins) | **Dédoublé** OU conservé à la racine avec build tags si la divergence est minime — à trancher en step 3 |
| `internal/ipc` | OS-spécifique (named pipe vs Unix socket) | **Dédoublé** |
| `internal/preflight` | OS-spécifique | **Dédoublé** |
| `internal/captive`, `internal/anomaly`, `internal/leakcheck`, `internal/uiwatchdog`, `internal/watchdog`, `internal/elevation`, `internal/browser`, `internal/httpproxy`, `internal/ipchandler`, `internal/ctlauth`, `internal/stun`, `internal/tunnel`, `internal/updater` | À classer en step 3 (lecture rapide des fichiers du package) |

#### D. Directives `//go:embed` (14)

| Source | Embed | Destination après refacto |
|---|---|---|
| `internal/ui/icons_windows.go` | `icons/levoile.ico`, `connected.ico`, `connecting.ico`, `disconnected.ico`, `alert.ico` | → `windows/internal/ui/icons/` (5 .ico) |
| `internal/ui/icons_linux.go` | `icons/levoile.png`, `connected.png`, `connecting.png`, `disconnected.png`, `alert.png` | → `linux/internal/ui/icons/` (5 .png) |
| `frontend/embed.go` | `all:index.html all:src all:assets` | **Dupliqué** : `windows/frontend/embed.go` + `linux/frontend/embed.go`, chacun avec sa copie d'assets |
| `internal/config/embed.go` | `config.example.toml` | Si `internal/config` reste racine : statu quo. Sinon dédoublé. |
| `internal/firewall/ruleset_linux.go` | `ruleset.nft.tmpl` | → `linux/internal/firewall/ruleset.nft.tmpl` |
| `internal/tun/wintun_embed_windows.go` (commenté) + `internal/tun/wintun_dll_windows.go` (généré) | `wintun/wintun.dll` | → `windows/internal/tun/wintun/wintun.dll`. **Le script `scripts/fetch-wintun.sh` doit voir son `TARGET_DIR` mis à jour.** |

#### E. Fichiers ambigus tranchés

| Fichier | Décision |
|---|---|
| `tools/gen_icons.go` (`//go:build ignore`) | → `windows/tools/gen_icons.go` (génère uniquement des `.ico`) |
| `tools/ipc_send/main.go` (`//go:build ipc_diagnostic`, importe `go-winio`) | → `windows/tools/ipc_send/` (Windows-only) |
| `tools/wfp_repro/` (`//go:build wfp_diagnostic`, WFP) | → `windows/tools/wfp_repro/` (Windows-only) |
| `internal/ui/prefs_perms_test.go` (`//go:build !windows`) | → `linux/internal/ui/prefs_perms_test.go` (POSIX-only ; si Darwin abandonné, le tag `!windows` peut devenir `linux`) |
| `build/chrome-cws.zip` | Aucune référence Go ou config build trouvée. **À questionner Akerimus** : extension Chrome partagée Win/Linux ? Reste racine ou à dupliquer ? |
| `build/appicon.png` | Master icon Wails — racine probablement (sert aux deux OS pour Wails build). À reconfirmer step 3. |
| `filters.xml` (612 KB) | Manifestement Windows (WFP). À déplacer en `windows/` mais à confirmer son utilité (à reconfirmer step 3 — pas de référence trouvée par les agents). |

#### F. Configs build à mettre à jour (synthèse)

| Fichier | Nb occurrences à patcher |
|---|---|
| `.goreleaser.yaml` | ~25 paths (8 builds `main:`, 4 archives `files:`, 2 nfpm avec ~18 entries chacune, release `extra_files:`) |
| `.goreleaser-linux-only.yaml` | ~15 paths (4 builds, 1 archive, 1 nfpm) |
| `Makefile` | ~6 paths (cibles `wintun`, `tun-test`, `clean`) |
| `installer/build.sh` + `build.ps1` | ~10 paths chacun (icons, dist/, wintun, NSIS, config) |
| `installer/levoile.nsi` | ~8 `File` directives (binaries, dll, icons, config) |
| `scripts/fetch-wintun.sh` | 2 paths (TARGET_DIR + EMBED_GO) |
| `scripts/release-sign.sh` + `test-release-signing.sh` | ~5 paths chacun (`./cmd/...` patterns, `cmd/genkey`, `cmd/signpkg`, `cmd/verifypkg`) |
| `.github/workflows/release.yml` | 2 paths (`./cmd/...`, `./internal/...`) |
| `.github/workflows/aur-publish.yml` | 3 paths (`packaging/arch/*`) |
| `scripts/diag-blank-window.ps1`, `scripts/test-aur-install.sh`, `scripts/test-auto-update-linux.sh`, `scripts/backup-signing-key.sh` | À auditer en step 3 (peu d'occurrences chacune) |

#### G. Restent à la racine (synthèse)

- `cmd/relay`, `cmd/genkey`, `cmd/genregistry`, `cmd/signpkg`, `cmd/verifypkg`, `cmd/verify-registry`
- `internal/relay`, `internal/registry`, `internal/crypto`, `internal/blocklist` (à reconfirmer en step 3 pour les packages `internal/` non encore audités en détail)
- `deploy/` (relais server-side complet)
- `_bmad/`, `_bmad-output/`, `.github/`
- `go.mod`, `go.sum`, `LICENSE`, `README.md`, `SECURITY.md`, `.gitattributes`, `.gitignore`, `.goreleaser.yaml`, `.goreleaser-linux-only.yaml`, `Makefile`, `config.example.toml`
- `docs/keys/levoile-release.pub`, `docs/keys/levoile-release.pub.pem` (clé publique distribution end-users — agnostique)
- Reste de `docs/` (documentation produit)

### Files to Reference

Inventaire condensé. Le tableau exhaustif (chemin source → chemin cible, ~470 entrées) sera produit en step 3 sous forme de manifeste de migration.

| File / Dir | Purpose | Destination |
| ---- | ------- | ---- |
| `cmd/client/` (3 fichiers) | Daemon entry — splitté | `windows/cmd/client/`, `linux/cmd/client/` |
| `cmd/ui/` (5 fichiers : main.go, service_windows.go, service_linux.go, app.rc, app_windows.syso) | UI Wails entry — splitté | `windows/cmd/ui/`, `linux/cmd/ui/` |
| `cmd/ctl/` | CLI ops — splitté | `windows/cmd/ctl/`, `linux/cmd/ctl/` |
| `cmd/relay/`, `cmd/gen*/`, `cmd/sign*/`, `cmd/verify*/` | Server-side / utilitaires | **Restent racine** |
| `internal/firewall/` (20 fichiers) | Kill-switch | Splitté Win/Linux |
| `internal/tun/` (21 fichiers + `wintun/wintun.dll`) | TUN device + Wintun DLL | Splitté Win/Linux |
| `internal/dns/` (33 fichiers) | DNS manager + kill-switch DNS | Splitté Win/Linux |
| `internal/routing/` (9 fichiers) | Routes système | Splitté Win/Linux |
| `internal/ui/` (28 fichiers + `icons/`) | UI HTTP server, prefs, webview, tray | Splitté Win/Linux |
| `internal/service/` (14 fichiers) | Service wrapper | Splitté Win/Linux |
| `internal/ipc/` | IPC named pipe / Unix socket | Splitté Win/Linux |
| `internal/preflight/` | Démarrage checks | Splitté Win/Linux |
| `internal/config/` | Config + paths OS | À trancher step 3 (split ou racine + build tags) |
| `internal/captive/`, `internal/anomaly/`, `internal/leakcheck/`, `internal/uiwatchdog/`, `internal/watchdog/`, `internal/elevation/`, `internal/browser/`, `internal/httpproxy/`, `internal/ipchandler/`, `internal/ctlauth/`, `internal/stun/`, `internal/tunnel/`, `internal/updater/` | Divers | À classer step 3 (rapide audit fichiers) |
| `internal/relay/`, `internal/registry/`, `internal/crypto/`, `internal/blocklist/` | OS-agnostique | **Restent racine** |
| `frontend/` (9 fichiers) | UI assets | Dupliqué : `windows/frontend/`, `linux/frontend/` |
| `installer/` (~13 fichiers + sous-dossiers) | Installer Windows | → `windows/installer/` |
| `packaging/` (~25 fichiers) | Packaging Linux | → `linux/packaging/` |
| `build/windows/` (3 fichiers) | Build assets Wails Windows | → `windows/build/` |
| `build/appicon.png`, `build/bin/`, `build/chrome-cws.zip` | À trancher | step 3 |
| `scripts/diag-blank-window.ps1` | Diagnostic UI Win | → `windows/scripts/` |
| `scripts/fetch-wintun.sh` | Fetch wintun DLL | → `windows/scripts/` (logique : ne sert qu'à Windows) |
| `scripts/test-aur-install.sh`, `scripts/test-auto-update-linux.sh` | Tests Linux | → `linux/scripts/` |
| `scripts/release-sign.sh`, `scripts/test-release-signing.sh`, `scripts/backup-signing-key.sh` | Release signing (cross-OS exec mais touche tous les paths) | **Restent racine** (utilitaires release maintainer) |
| `tools/gen_icons.go`, `tools/wfp_repro/`, `tools/ipc_send/` | Tools Windows | → `windows/tools/` |
| `filters.xml` | WFP filters dump (capturé 2026-04-21) | → `windows/` (à confirmer son rôle step 3) |
| `.goreleaser.yaml`, `.goreleaser-linux-only.yaml`, `Makefile` | Configs release | **Restent racine** (paths internes patchés) |
| `.github/workflows/*` | CI | **Restent dans `.github/`** (paths internes patchés) |
| `deploy/` (entier) | Relais server-side | **Reste racine** |
| `docs/`, `LICENSE`, `README.md`, `SECURITY.md`, `config.example.toml`, `go.mod`, `go.sum`, `.gitattributes`, `.gitignore` | Méta | **Restent racine** |
| `android/` | (nouveau) | Stub : `android/cmd/`, `android/internal/`, `android/frontend/` + `README.md` placeholder |

### Technical Decisions

1. **Module Go unique conservé.** Confirmé après investigation : un seul `go.mod` racine. Les sous-arbres `windows/`, `linux/`, `android/` sont des chemins d'import. Pas de modules séparés.

2. **Pas de `shared/` ni `common/` introduits dans cette refacto.** Le code commun reste à sa place actuelle (racine `internal/<feature>/`) ou est dupliqué. Une étape 2 future pourra extraire des abstractions communes — avec recul.

3. **Frontend dupliqué.** `frontend/` → `windows/frontend/` + `linux/frontend/` (mêmes fichiers initialement). Conforme à la directive utilisateur "on isole frontend pour tous".

4. **Restent à la racine** : `cmd/relay`, `cmd/genkey`, `cmd/genregistry`, `cmd/signpkg`, `cmd/verifypkg`, `cmd/verify-registry` (binaires server/utility OS-agnostiques) ; `internal/relay`, `internal/registry`, `internal/crypto`, `internal/blocklist` (packages OS-agnostiques confirmés sans suffixes OS) ; `deploy/` (relais infra) ; `docs/`, configs racine, `.goreleaser*`, `Makefile`, `.github/`.

5. **Splittés** : `cmd/client`, `cmd/ui`, `cmd/ctl` (ils dépendent transitivement de packages OS-spécifiques via `internal/service` ou `internal/ipc`).

6. **Stratégie Darwin** : `darwin-only` (5 fichiers prod + 2 tests) et `cross-os-stub` (5 fichiers no-op pour Darwin) — **à trancher** : si Darwin n'est pas une cible client, on **supprime** ces fichiers et stubs lors de la refacto. Si Darwin reste cible (futur), on les déplace dans une racine `darwin/` (mais l'utilisateur n'a mentionné que Win/Linux/Android).

7. **Ordre de migration imposé** :
   1. **Préparation** : audit final des packages restants (`internal/captive`, etc.), création des arborescences vides (commit dédié).
   2. **Windows** : déplacement complet `cmd/ui` Win, `cmd/client` Win, `cmd/ctl` Win, packages `internal/*` Win, `frontend/` → `windows/frontend/`, `installer/` → `windows/installer/`, `build/windows/` → `windows/build/`, `tools/wfp_repro` + `tools/ipc_send` + `tools/gen_icons.go` → `windows/tools/`, scripts `.ps1` + `fetch-wintun.sh` → `windows/scripts/`, `filters.xml` → `windows/`. Mise à jour `.goreleaser*.yaml`, `Makefile`, `installer/levoile.nsi`, `scripts/fetch-wintun.sh`. Tests : `go build ./windows/...` + `go vet` + tests unitaires Windows + build NSIS + smoke launch UI.
   3. **Linux** : symétrique. `cmd/ui` Linux, `cmd/client` Linux, `cmd/ctl` Linux, packages `internal/*` Linux, `frontend/` → `linux/frontend/`, `packaging/` → `linux/packaging/`, scripts `.sh` Linux client → `linux/scripts/`. Mise à jour configs. Tests : `go build ./linux/...` + tests unitaires Linux + build deb (nfpm) + smoke launch UI.
   4. **Stratégie Darwin** : suppression des fichiers `_darwin.go`, `_darwin_test.go`, et stubs cross-OS (sauf si l'utilisateur veut conserver). Tag `!windows` → `linux`.
   5. **Android** : création arborescence `android/cmd/`, `android/internal/`, `android/frontend/` + `android/README.md` placeholder. Pas d'implémentation. Confirmation que tous les builds/tests Win et Linux restent verts.
   6. **Cleanup final** : suppression des `cmd/client`, `cmd/ui`, `cmd/ctl`, packages `internal/*` OS-spécifiques, `frontend/`, `installer/`, `packaging/`, `build/windows/`, `tools/wfp_repro`, etc. à la racine (devenus vides ou non référencés).

8. **Critère de "vert" entre étapes (par OS) :**
   - `go build ./...` (tout le module)
   - `go vet ./...`
   - `go test ./windows/...` (sur runner Windows pour étape Windows) — `go test ./linux/...` (sur runner Linux pour étape Linux)
   - Build de l'installeur de l'OS courant (NSIS Win / `goreleaser build --snapshot --single-target` Linux deb)
   - Smoke launch : lancer `ui.exe` (Win) ou `levoile-ui` (Linux) — vérifier tray + webview démarrent
   - Pas de régression dans `_bmad-output/implementation-artifacts/sprint-status.yaml`

9. **Migration incrémentale = 1 commit par groupe logique de fichiers** (ex : `internal/firewall` Windows en un commit avec `go build` vert), pas un seul méga-commit. Chaque commit doit laisser le repo buildable. Le commit final supprime les originaux racine.

10. **Pas de modification d'API ou de symbole Go.** Les imports passent de `module/internal/firewall` à `module/windows/internal/firewall` ou `module/linux/internal/firewall`. Les noms de package Go (`firewall`, `tun`, `dns`, etc.) restent inchangés. Les noms de symboles exportés inchangés.

11. **`go.mod` unique** : pas de `go.work`, pas de modules séparés. Si Akerimus veut isoler aussi le `go.mod` (pour garantir 100 % d'étanchéité côté dépendances), c'est une étape 2 future — beaucoup plus lourde (multi-module workspace).

12. **Décisions Akerimus (2026-04-26) :**
   - **Darwin supprimé** (5 prod + 2 tests + 5 stubs cross-OS).
   - **`internal/config/` splitté** Win/Linux.
   - **`build/chrome-cws.zip` supprimé** (extension Chrome obsolète depuis le changement d'architecture).
   - **`filters.xml` supprimé** (dump WFP diagnostic 2026-04-21, non utilisé en prod).
   - **`internal/crypto/` reste racine** (24 importeurs cross-tier — drift = trou sécurité).
   - **`internal/registry/` reste racine** (partagé client+relay strict).
   - **`internal/blocklist/` DUPLIQUÉ** : `windows/internal/blocklist/` + `linux/internal/blocklist/` (futur : `android/internal/blocklist/`).
   - **Outils maintainer crypto regroupés** dans `tools/` racine : `tools/genkey/`, `tools/signpkg/`, `tools/verifypkg/` (OS-agnostiques, utilisés sur poste maintainer).
   - **Spécifiques relais** → `relay/` : `relay/cmd/{relay,genregistry,verify-registry}/`, `relay/internal/relay/`, `relay/deploy/`, `relay/Makefile`, `relay/.goreleaser.yaml`.
   - **Goreleaser et Makefile isolés strict par OS** : `windows/.goreleaser.yaml` + `windows/Makefile`, `linux/.goreleaser.yaml` + `linux/Makefile`. Aucun fichier de build à la racine. **Pas d'orchestrateur multi-OS** (cross-compilation refusée par Akerimus en pratique). Une release par OS, indépendante. Tags Git séparés (ex : `windows-v1.2.0`, `linux-v1.2.0`, `relay-v1.2.0`).
   - **Scripts release** : `scripts/release-sign.sh` + `test-release-signing.sh` splittés par OS (Linux .sh dans `linux/scripts/`, Windows à porter en `.ps1` dans `windows/scripts/`, équivalent dans `relay/scripts/` ou `relay/Makefile`).
   - **`scripts/backup-signing-key.sh`** → `linux/scripts/` (bash, exécuté sur poste maintainer Linux).
   - **`internal/tunnel/client_test.go`** importe `internal/relay` → ce test devra importer depuis `relay/internal/relay/` après refacto. Si `internal/tunnel/` est splitté Win/Linux, le test suit son code.

### Structure cible finale

```
/  (racine)
├── _bmad/, _bmad-output/, .git/, .github/, .vscode/, .claude/, docs/
├── tools/
│   ├── genkey/  signpkg/  verifypkg/
├── internal/
│   ├── crypto/   registry/
├── relay/
│   ├── cmd/{relay,genregistry,verify-registry}/
│   ├── internal/relay/
│   ├── deploy/
│   ├── scripts/  (release-sign + smoke + cert-check côté relay)
│   ├── Makefile  .goreleaser.yaml
├── windows/
│   ├── cmd/{client,ui,ctl}/
│   ├── internal/{firewall,tun,dns,routing,ui,service,ipc,preflight,config,blocklist,...}/
│   ├── frontend/  installer/  build/  scripts/
│   ├── tools/{wfp_repro,ipc_send,gen_icons.go}/
│   ├── Makefile  .goreleaser.yaml
├── linux/
│   ├── cmd/{client,ui,ctl}/
│   ├── internal/{firewall,tun,dns,routing,ui,service,ipc,preflight,config,blocklist,...}/
│   ├── frontend/  packaging/  scripts/
│   ├── Makefile  .goreleaser.yaml
├── android/  (stub futur)
│   ├── cmd/  internal/  frontend/  README.md
├── go.mod  go.sum  README.md  SECURITY.md  LICENSE  .gitignore  .gitattributes
└── config.example.toml
```

### Technical Decisions

1. **Module Go unique conservé.** `go.mod` reste à la racine. Pas de modules séparés par OS — gérer trois `go.mod` triplerait la dette de mise à jour des dépendances. Les sous-arbres `windows/`, `linux/`, `android/` sont de simples chemins d'import dans le module principal.

2. **Pas de packages "shared"/"common" introduits dans cette refacto.** Le code commun reste là où il est aujourd'hui (à la racine de `internal/<feature>/`) ou est dupliqué dans chaque racine OS si le package devient OS-spécifique. Création d'un `shared/` deviendra une étape 2 quand on aura recul sur ce qui est réellement partageable.

3. **Frontend dupliqué assumé.** Même fichiers HTML/JS/CSS dans `windows/frontend/` et `linux/frontend/` initialement. Aucune tentative de partage côté frontend. Permet d'évoluer indépendamment (ex : assets icons OS, comportements webview spécifiques) sans cross-impact.

4. **`internal/relay`, `internal/registry`, `internal/crypto`, `cmd/relay`, `cmd/gen*`, `cmd/sign*`, `cmd/verify*`, `deploy/` restent à la racine.** Ils sont server-side ou utilitaires de build OS-agnostiques. Vérification à confirmer en Step 2 (qu'ils ne référencent pas de code client-side OS-specific).

5. **Ordre de migration imposé : Windows → Linux → Android (stub).** Windows en premier car c'est la plate-forme la plus complexe (NSIS, WFP, wintun DLL embarquée, .syso, scheduled task, élévation UAC) et où les régressions sont les plus coûteuses à diagnostiquer. Une fois Windows isolé et vert, Linux suit le même playbook avec moins de surprises. Android est en dernier — juste l'arborescence + un README de placeholder.

6. **Critère de "vert" entre étapes :**
   - `go build ./...` (l'ensemble du module)
   - `go test ./windows/...` (sur runner Windows) — ou `go test ./linux/...` (sur runner Linux)
   - `go vet ./...`
   - Build de l'installeur de l'OS courant (NSIS pour Win ; deb minimum pour Linux) — sanity check
   - Test fumée : lancer `ui.exe` (Win) ou `levoile-ui` (Linux) et vérifier que le tray + webview démarrent
   - Pas de régression visible dans `_bmad-output/implementation-artifacts/sprint-status.yaml` ni dans les TODOs ouverts

7. **Migration incrémentale signifie aussi que la racine actuelle (`internal/`, `cmd/client/`, `cmd/ui/`, `frontend/`, `installer/`, `packaging/`) reste partiellement peuplée pendant la transition.** On ne supprime un fichier de la racine qu'après l'avoir copié dans la racine OS et avoir validé que rien ne le réfère plus. À chaque jalon "OS terminé", on fait un commit propre qui supprime les originaux à la racine.

8. **Pas de modification d'API ou de symbol Go.** Les déplacements préservent les noms de package (sauf le path d'import). Les imports passent de `…/internal/firewall` à `…/windows/internal/firewall`. Les noms de symbole exportés restent identiques.

## Implementation Plan

### Stratégie générale — flow "copy-build-cleanup"

La migration suit un flow en **trois temps** par OS pour garantir que `go build ./...` reste vert à CHAQUE commit :

1. **Copy** — création de `windows/...` (puis `linux/...`) en **copiant** (pas en déplaçant) les packages OS-spécifiques depuis la racine. L'ancien `internal/...` racine reste intact pendant cette phase. Les imports dans `windows/cmd/*` pointent vers `windows/internal/*` ; ceux dans `cmd/*` racine continuent de pointer vers `internal/*` racine. Les deux arbres compilent en parallèle.
2. **Build & verify** — pour chaque OS, après avoir copié toute son arborescence, `go build ./windows/...` et `go test ./windows/...` doivent être verts. Smoke test : lancer le binaire UI, vérifier tray + webview.
3. **Cleanup** — une fois les deux OS migrés et verts, suppression de l'ancien `cmd/*` racine et `internal/*` OS-spécifique racine, en commits dédiés (par paquets logiques).

Cette approche évite la cascade "déplacer le package X casse Y qui casse Z" parce que pendant la phase Copy, l'ancien et le nouveau coexistent.

**Conséquence** : duplication temporaire (le repo grossit pendant la migration). Le commit final cleanup retire le surplus.

### Tasks

#### PHASE 0 — PRÉPARATION (1 commit)

- [ ] **Task 0.1: Créer les arborescences vides + sanity baseline**
  - Action: Créer les dossiers `windows/`, `linux/`, `android/`, `relay/`, `tools/` (avec un `.gitkeep` placeholder dans chacun)
  - Action: Créer `android/README.md` avec un placeholder ("Stub directory — Android implementation not yet started. Mirror windows/ or linux/ structure.")
  - Action: `git add -A && git commit -m "chore(refacto): create empty target directories for OS isolation"`
  - Verify: `go build ./...` vert (baseline). `git status` propre.

#### PHASE 1 — NETTOYAGE PRÉ-REFACTO (2 commits)

- [ ] **Task 1.1: Supprimer la cible Darwin et les stubs cross-OS no-op**
  - Files (suppression):
    - `internal/dns/check_darwin.go`, `internal/dns/check_darwin_test.go`
    - `internal/dns/manager_darwin.go`, `internal/dns/manager_darwin_test.go`
    - `internal/dns/flush_other.go`, `internal/dns/dnscache_other.go`
    - `internal/browser/detect_darwin.go`, `internal/browser/manager_darwin.go`, `internal/browser/lock_darwin.go`
    - `internal/firewall/firewall_stub.go`
    - `internal/anomaly/anomaly_stub.go`
    - `internal/ui/icons_stub.go`, `internal/ui/singleton_stub.go`, `internal/ui/sysproxy_stub.go`
    - `internal/uiwatchdog/launcher_stub.go`
  - Action: vérifier qu'aucun fichier `*.go` non-stub ne référence un symbole défini dans les stubs supprimés. Si oui, fixer l'usage côté Win+Linux (la fonction stub no-op n'est plus appelable sur les OS supportés).
  - Action: ajuster les tags `//go:build !windows` qui voulaient dire "linux+darwin" en `//go:build linux` purs (ex : `internal/ui/prefs_perms_test.go`, `internal/elevation/elevation_unix.go` → renommer en `_linux.go` ou conserver le tag `!windows` qui devient équivalent à `linux`).
  - Verify: `go build ./...` vert sur Windows ET Linux. `go test ./...` vert.
  - Commit: `chore(refacto): drop Darwin target and cross-OS no-op stubs`

- [ ] **Task 1.2: Supprimer assets obsolètes**
  - Files (suppression): `build/chrome-cws.zip`, `filters.xml`
  - Verify: aucun référence dans le code/configs (déjà confirmé en step 2). `go build ./...` vert.
  - Commit: `chore(refacto): drop obsolete chrome extension and WFP filter dump`

#### PHASE 2 — RACINE PARTAGÉE `tools/` (1 commit)

- [ ] **Task 2.1: Déplacer outils maintainer crypto vers `tools/`**
  - Action: `git mv cmd/genkey tools/genkey && git mv cmd/signpkg tools/signpkg && git mv cmd/verifypkg tools/verifypkg`
  - Files à patcher (paths internes) :
    - `.goreleaser.yaml` : `dir: cmd/signpkg` → `dir: tools/signpkg` (et idem verifypkg dans les builds `verify-windows`, `verify-linux`)
    - `Makefile` : toute référence `./cmd/genkey`, `./cmd/signpkg`, `./cmd/verifypkg` → `./tools/...`
    - `scripts/release-sign.sh` : `go build ./cmd/signpkg` → `go build ./tools/signpkg` (et toute référence `./cmd/genkey`, `./cmd/verifypkg`)
    - `scripts/test-release-signing.sh` : idem
  - Verify: `go build ./tools/...` vert. `go vet ./tools/...` vert. Smoke test : `make release-verify-smoke` (snapshot signing) si possible localement.
  - Commit: `refactor(tools): move maintainer crypto tools (genkey,signpkg,verifypkg) to tools/`

#### PHASE 3 — RELAIS `relay/` (3 commits)

- [ ] **Task 3.1: Déplacer code relais vers `relay/`**
  - Action:
    - `git mv cmd/relay relay/cmd/relay`
    - `git mv cmd/genregistry relay/cmd/genregistry`
    - `git mv cmd/verify-registry relay/cmd/verify-registry`
    - `git mv internal/relay relay/internal/relay`
    - `git mv deploy relay/deploy`
  - Files à patcher (imports Go) :
    - `internal/tunnel/client_test.go` : `import ".../internal/relay"` → `import ".../relay/internal/relay"` (l'unique importeur cross-tier détecté en step 2)
    - Tout autre import de `internal/relay` détecté (re-grep avant commit pour confirmer)
  - Files à patcher (configs) :
    - `.goreleaser.yaml` : `main: ./cmd/relay` → `main: ./relay/cmd/relay`, `extra_files: deploy/install.sh` → `extra_files: relay/deploy/install.sh`, `before.hooks` qui signe `deploy/install.sh` → patcher path
    - `.github/workflows/release.yml` (si refs)
    - `scripts/test-release-signing.sh` : refs `deploy/install.sh` et `deploy/install.sh.sig` → `relay/deploy/install.sh*`
    - `scripts/test-aur-install.sh` : ne touche pas relay normalement, à vérifier
  - Verify: `go build ./relay/...` vert. `go test ./relay/...` vert (les tests `e2e` requièrent `-tags=e2e`). `go build ./...` global vert.
  - Commit: `refactor(relay): move relay code, registry tools and deploy scripts to relay/`

- [ ] **Task 3.2: Créer `relay/Makefile` et `relay/.goreleaser.yaml`**
  - Action: extraire du `Makefile` racine les cibles relay-spécifiques (s'il y en a) → `relay/Makefile`. Au minimum : `make build`, `make test`, `make release` qui invoquent `go build ./cmd/relay`, `goreleaser release --config .goreleaser.yaml`.
  - Action: extraire du `.goreleaser.yaml` racine les sections `builds` (id `relay`), `archives` (id `relay`), `signs`, `nfpms` non-applicables au relais (à laisser racine ou virer), → `relay/.goreleaser.yaml` autonome qui ne build QUE le binaire relay + ses extras.
  - L'ancien `.goreleaser.yaml` racine perd ses sections relay (le bloc `id: relay` du tableau `builds:`, le bloc archive relay, et `extra_files` deploy/).
  - Tester depuis `relay/` : `cd relay && goreleaser release --snapshot --clean --config .goreleaser.yaml`
  - Verify: archive relay produite avec `levoile-relay`, install.sh, install.sh.sig.
  - Commit: `feat(relay): add autonomous Makefile and goreleaser config under relay/`

- [ ] **Task 3.3: Splitter scripts release relay**
  - Files (déplacement) :
    - `git mv scripts/cert-expiry-check.sh relay/scripts/cert-expiry-check.sh` (si applicable côté relais ; sinon il est déjà sous deploy/)
    - Note : `deploy/cert-expiry-check.sh`, `deploy/renewal-hook-restart-relay.sh`, `deploy/smoke_registry.sh`, `deploy/install*.sh` ont déjà été déplacés en 3.1 sous `relay/deploy/`.
  - Action: créer `relay/scripts/release-sign.sh` extrait de `scripts/release-sign.sh` racine (uniquement la portion qui release le relais).
  - Verify: `relay/scripts/release-sign.sh` exécutable.
  - Commit: `feat(relay): split relay-specific release scripts under relay/scripts/`

#### PHASE 4 — MIGRATION WINDOWS — PHASE COPY (≈12 commits)

À chaque commit de cette phase, **`go build ./...` doit rester vert sur Windows ET Linux**. L'ancien `internal/*` et `cmd/*` racine restent intacts. On crée `windows/*` en parallèle.

- [ ] **Task 4.1: Créer `windows/internal/blocklist/` (cas dupliqué)**
  - Action: `cp -r internal/blocklist windows/internal/blocklist` (copie, l'original reste pour Linux)
  - Note: aucun import à patcher pour l'instant — `windows/cmd/*` n'existe pas encore, et l'ancien `cmd/*` racine continue d'importer `internal/blocklist`.
  - Verify: `go build ./windows/internal/blocklist/...` vert.
  - Commit: `refactor(windows): seed windows/internal/blocklist as a duplicated package`

- [ ] **Task 4.2: Créer `windows/internal/firewall/`**
  - Action:
    - `cp internal/firewall/firewall.go windows/internal/firewall/` (shared)
    - `cp internal/firewall/doc.go windows/internal/firewall/` (shared)
    - `git mv internal/firewall/firewall_windows.go windows/internal/firewall/`
    - `git mv internal/firewall/wfp_windows.go windows/internal/firewall/`
    - `git mv internal/firewall/eventlog_windows.go windows/internal/firewall/`
    - `git mv internal/firewall/watchdog_windows.go windows/internal/firewall/`
    - `git mv internal/firewall/firewall_windows_test.go windows/internal/firewall/`
  - **Risque** : après suppression des fichiers `_windows.go` de l'ancien `internal/firewall/` racine, le build Windows de `cmd/client` (racine) qui importe `internal/service` qui importe `internal/firewall` va casser. L'ancien `internal/firewall/firewall.go` (shared) référence des fonctions définies dans `_windows.go`. → Le repo va casser sur Windows pendant cette phase si `cmd/client` racine n'a pas été splitté avant.
  - **Solution** : faire ce commit **après** Task 4.8 (création `windows/cmd/client`), OU conserver une copie temporaire de `firewall_windows.go` à la racine jusqu'au cleanup. Choix : **conserver la copie racine** — utiliser `cp` au lieu de `git mv` pour les fichiers `_windows.go`. La double copie sera nettoyée en Phase 7.
  - Action révisée: `cp -r internal/firewall windows/internal/firewall` (tout copier ; le clean racine arrive en Phase 7)
  - Verify: `go build ./windows/internal/firewall/...` (Windows) vert. `go build ./...` (Windows + Linux) toujours vert.
  - Commit: `refactor(windows): seed windows/internal/firewall (copy of OS-specific package)`

- [ ] **Task 4.3: Créer `windows/internal/tun/` + wintun DLL**
  - Action:
    - `cp -r internal/tun windows/internal/tun` (copie complète : device.go, device_windows.go, wintun_*.go, watchdog/, wintun/wintun.dll, etc.)
    - **Important** : `internal/tun/wintun_dll_windows.go` est généré par `scripts/fetch-wintun.sh` ; vérifier que la copie inclut le fichier généré. Le path `//go:embed wintun/wintun.dll` reste valide car le sous-dossier `wintun/` est copié avec.
    - Adapter `windows/internal/tun/cleanup_linux.go` ? Non — il sera supprimé en Phase 7 puisque windows/ ne build que Windows. **Action complémentaire** : retirer les fichiers `_linux.go` de `windows/internal/tun/` immédiatement (ils n'ont aucune raison d'y être). De même pour tous les packages copiés sous `windows/`.
  - Action générique: après chaque `cp -r`, supprimer dans la copie `windows/...` les fichiers `_linux.go`, `_linux_test.go`, et reciproquement pour `linux/...` les fichiers `_windows.go`.
  - Verify: `go build ./windows/internal/tun/...` (Windows) vert. `windows/internal/tun/wintun/wintun.dll` présent.
  - Commit: `refactor(windows): seed windows/internal/tun including wintun DLL`

- [ ] **Task 4.4: Créer `windows/internal/dns/`**
  - Action: `cp -r internal/dns windows/internal/dns`, puis dans la copie supprimer tous les `*_linux*.go` et garder uniquement `*_windows*.go` + shared (`flush.go`, `kill_switch.go`, `manager.go`, `proxy.go`, `reuseaddr_other.go` ? — non, `_other.go` a été supprimé en Phase 1 donc `reuseaddr_other.go` n'existe plus → garder `reuseaddr_windows.go`).
  - Verify: `go build ./windows/internal/dns/...` (Windows) vert.
  - Commit: `refactor(windows): seed windows/internal/dns`

- [ ] **Task 4.5: Créer `windows/internal/routing/`**
  - Action: `cp -r internal/routing windows/internal/routing`, supprimer `*_linux*.go`.
  - Verify: `go build ./windows/internal/routing/...` (Windows) vert.
  - Commit: `refactor(windows): seed windows/internal/routing`

- [ ] **Task 4.6: Créer `windows/internal/ui/` + assets icons**
  - Action: `cp -r internal/ui windows/internal/ui`, supprimer `*_linux*.go`, `icons/*.png` (garder `icons/*.ico`), `singleton_linux*.go`, `webview_cgo_linux.go`, `webview_nocgo.go` (à vérifier — peut-être nécessaire en fallback).
  - Note: `prefs_perms_test.go` (`!windows`) → exclure de la copie windows ; il vivra sous `linux/internal/ui/`.
  - Verify: `go build ./windows/internal/ui/...` vert. Embed des `.ico` fonctionne.
  - Commit: `refactor(windows): seed windows/internal/ui with Windows icons and webview`

- [ ] **Task 4.7: Créer le reste des packages Windows OS-spécifiques**
  - Action (un commit unique pour les petits packages, ou un par package selon préférence) :
    - `cp -r internal/service windows/internal/service` puis supprimer `*_linux*.go`
    - `cp -r internal/ipc windows/internal/ipc` puis supprimer `*_linux*.go`
    - `cp -r internal/preflight windows/internal/preflight` puis supprimer `*_linux*.go`
    - `cp -r internal/config windows/internal/config` puis supprimer `*_linux*.go`
    - `cp -r internal/anomaly windows/internal/anomaly` puis supprimer `*_linux*.go`
    - `cp -r internal/uiwatchdog windows/internal/uiwatchdog` puis supprimer `*_linux*.go` ou `*_unix.go`
    - `cp -r internal/elevation windows/internal/elevation` puis supprimer `elevation_unix.go`
    - `cp -r internal/browser windows/internal/browser` puis supprimer `*_linux*.go`, `*_darwin*.go` (déjà supprimé en 1.1)
    - `cp -r internal/ctlauth windows/internal/ctlauth` puis supprimer `perms_unix.go`
  - Verify: `go build ./windows/internal/...` vert pour Windows.
  - Commit: `refactor(windows): seed remaining windows/internal/* OS-specific packages`

- [ ] **Task 4.8: Créer `windows/cmd/{client,ui,ctl}/`**
  - Action:
    - `cp -r cmd/client windows/cmd/client`
    - `cp -r cmd/ui windows/cmd/ui`, supprimer `service_linux.go`
    - `cp -r cmd/ctl windows/cmd/ctl` (vérifier s'il y a des fichiers `_linux.go` dedans, supprimer si oui)
  - Action: **mettre à jour les imports** dans les copies `windows/cmd/*/` :
    - Tout `import "<module>/internal/firewall"` → `import "<module>/windows/internal/firewall"`
    - Idem pour `tun`, `dns`, `routing`, `ui`, `service`, `ipc`, `preflight`, `config`, `anomaly`, `uiwatchdog`, `elevation`, `browser`, `ctlauth`, `blocklist`
    - Imports vers packages racine inchangés (`internal/crypto`, `internal/registry`, `internal/captive`, `internal/leakcheck`, `internal/watchdog`, `internal/httpproxy`, `internal/ipchandler`, `internal/stun`, `internal/tunnel`, `internal/updater`)
    - Imports relay : `internal/relay` n'est plus importé par client (déjà déplacé sous relay/) ; si encore référencé dans un commentaire ou test, patcher.
  - Important : `internal/service` est splitté en `windows/internal/service`. Mais `internal/service/service.go` lui-même importe `internal/firewall`, `internal/tun`, etc. Donc dans `windows/internal/service/service.go`, ces imports doivent aussi pointer vers `windows/internal/...`. Action complémentaire : **dans tous les fichiers de `windows/internal/`, mettre à jour les imports cross-internal vers leurs équivalents `windows/internal/`**.
  - Verify: `go build ./windows/cmd/...` vert (Windows). `go build ./windows/...` vert global.
  - Commit: `refactor(windows): seed windows/cmd/{client,ui,ctl} with updated imports`

- [ ] **Task 4.9: Créer `windows/frontend/`**
  - Action: `cp -r frontend windows/frontend` (incluant `embed.go`, `contract_test.go`, `index.html`, `src/`, `assets/`)
  - Action: si `windows/cmd/ui/main.go` importe `frontend` (le package), patcher l'import → `windows/frontend`. Idem dans `windows/internal/ui/...` si applicable.
  - Verify: `go build ./windows/frontend/...` vert. Embed des assets fonctionne.
  - Commit: `refactor(windows): seed windows/frontend with embedded UI assets`

- [ ] **Task 4.10: Créer `windows/installer/` + `windows/build/` + `windows/tools/` + `windows/scripts/`**
  - Action:
    - `git mv installer windows/installer` (les fichiers ne sont utilisés que pour les builds Windows)
    - `git mv build/windows windows/build` (ou `mkdir windows/build && git mv build/windows/* windows/build/`)
    - `git mv build/appicon.png windows/build/appicon.png` (master Wails icon — uniquement Windows utilise Wails). Si l'utilisateur pense que `appicon.png` sert aussi à Linux (improbable car Linux utilise PNG icons depuis `packaging/icons/`), à clarifier sinon copier dans linux/build/.
    - `git mv build/bin/levoile-desktop.exe windows/build/bin/levoile-desktop.exe` si encore présent (legacy)
    - `git mv tools/wfp_repro windows/tools/wfp_repro`
    - `git mv tools/ipc_send windows/tools/ipc_send`
    - `git mv tools/gen_icons.go windows/tools/gen_icons.go`
    - `git mv scripts/diag-blank-window.ps1 windows/scripts/diag-blank-window.ps1`
    - `git mv scripts/fetch-wintun.sh windows/scripts/fetch-wintun.sh`
  - Files à patcher :
    - `windows/scripts/fetch-wintun.sh` : `TARGET_DIR="internal/tun/wintun"` → `TARGET_DIR="windows/internal/tun/wintun"` ; `EMBED_GO="internal/tun/wintun_dll_windows.go"` → `EMBED_GO="windows/internal/tun/wintun_dll_windows.go"`. Adapter aussi les chemins de log.
    - `windows/installer/build.ps1` + `build.sh` : tous les paths `internal/tun/wintun/wintun.dll` → `windows/internal/tun/wintun/wintun.dll` ; `internal/ui/icons/connected.ico` → `windows/internal/ui/icons/connected.ico` (etc) ; `dist/service_windows_amd64_v1/levoile-service.exe` → restera `dist/...` car goreleaser produit toujours dans `dist/` (voir Task 4.11).
    - `windows/installer/levoile.nsi` : aucune patch nécessaire si les `File` directives sont en chemins relatifs au répertoire de build (ex: `build/levoile-service.exe`) — vérifier en lisant le fichier en step 4.
  - Verify: `go build ./windows/tools/...` vert (avec tags `wfp_diagnostic`, `ipc_diagnostic`). `windows/installer/build.ps1` exécutable.
  - Commit: `refactor(windows): move installer, build assets, tools and scripts under windows/`

- [ ] **Task 4.11: Créer `windows/Makefile` + `windows/.goreleaser.yaml`**
  - Action:
    - Extraire du `Makefile` racine les cibles Windows-only (`wintun`, `installer-win`, `release-win`) → `windows/Makefile`. Adapter les paths internes.
    - Extraire du `.goreleaser.yaml` racine les sections Windows : `builds` `id: service` (windows), `id: ui` (windows), `id: ctl-windows`, `id: verify-windows`, archive `id: windows`, signs Windows, nfpms n'existe pas pour Windows — donc rien à extraire de ce côté. Construire `windows/.goreleaser.yaml` autonome.
    - Adapter tous les paths : `cmd/client` → `cmd/client` (relatif depuis `windows/`), `assets/icons/*.ico` → vérifier le chemin réel ; `installer/config-default.toml` → `installer/config-default.toml` (relatif depuis `windows/`) ; `docs/keys/levoile-release.pub` → `../docs/keys/levoile-release.pub` (chemin remontant à la racine pour la clé publique partagée).
    - Créer `windows/scripts/release-sign.ps1` (port PowerShell de l'ancien `scripts/release-sign.sh`) ou laisser un `windows/scripts/release-sign.sh` qui exécute via Git Bash.
    - Créer `windows/scripts/test-release-signing.sh` ou .ps1 (idem).
  - L'ancien `.goreleaser.yaml` racine perd ses sections Windows.
  - Verify: depuis `windows/`, `goreleaser release --snapshot --clean --config .goreleaser.yaml --single-target` produit `levoile-service.exe`, `levoile-ui.exe`, `levoile-ctl.exe`, archive Windows, signatures.
  - Commit: `feat(windows): add autonomous Makefile, goreleaser config and release scripts`

- [ ] **Task 4.12: Smoke test Windows**
  - Action manuelle (Akerimus sur poste Windows):
    - `cd windows && make` → build vert
    - `cd windows && make test` → tests verts
    - `cd windows && go vet ./...` → propre
    - `cd windows/installer && bash build.sh` (ou `pwsh build.ps1`) → produit `LeVoile-Setup.exe`
    - Lancer `dist/.../levoile-ui.exe` → vérifier tray + webview démarrent
    - Vérifier que la wintun.dll est bien embarquée et que le binaire ne dépend pas de fichiers à la racine du repo
  - Si KO : retour aux commits précédents pour fix.
  - Verify: smoke test OK manuel.
  - Commit (post-fix éventuel): `chore(windows): smoke test pass — Windows release pipeline isolated`

#### PHASE 5 — MIGRATION LINUX — PHASE COPY (≈12 commits, symétrique à Phase 4)

- [ ] **Task 5.1 → 5.12** : symétrique à 4.1 → 4.12, avec :
  - `linux/internal/blocklist/` (copie pour duplication)
  - `linux/internal/{firewall,tun,dns,routing,ui,service,ipc,preflight,config,anomaly,uiwatchdog,elevation,browser,ctlauth}/` (copies, suppression des `_windows*.go`)
  - `linux/cmd/{client,ui,ctl}/` (avec imports patchés vers `linux/internal/*` + racine partagée)
  - `linux/frontend/`, `linux/packaging/` (depuis `packaging/`), `linux/scripts/`
  - `linux/Makefile` + `linux/.goreleaser.yaml`
  - Smoke test : `cd linux && make`, `make test`, `goreleaser build --snapshot --single-target`, lancer `dist/.../levoile-ui` → tray + webview, build `.deb` via nfpm
- [ ] **Task 5.10 spécifique Linux** : `git mv packaging linux/packaging` ; mettre à jour `linux/.goreleaser.yaml` `nfpms.contents.src` paths : `packaging/systemd/levoile.service` → `packaging/systemd/levoile.service` (relatif depuis `linux/`). Adapter `linux/packaging/scripts/postinstall.sh` etc. si paths absolus du target system changent (normalement non — ils restent `/usr/bin/levoile-service`).
- [ ] **Task 5.10 spécifique aussi** : `git mv scripts/test-aur-install.sh linux/scripts/`, `git mv scripts/test-auto-update-linux.sh linux/scripts/`, `git mv scripts/backup-signing-key.sh linux/scripts/`. Adapter `.github/workflows/aur-publish.yml` paths `packaging/arch/*` → `linux/packaging/arch/*`.
- [ ] **Task 5.11 spécifique** : `linux/scripts/release-sign.sh` (port de l'ancien `scripts/release-sign.sh` adapté pour Linux uniquement) ; `linux/scripts/test-release-signing.sh` idem.
- [ ] **Task 5.12 (smoke Linux)** : `cd linux && make`, build deb, install dans VM/container, vérifier service systemd démarre, UI tray fonctionne.

#### PHASE 6 — STUB ANDROID (1 commit)

- [ ] **Task 6.1: Créer le squelette Android**
  - Action:
    - `mkdir -p android/cmd android/internal android/frontend`
    - Créer `android/.gitkeep` placeholders dans les sous-dossiers
    - Compléter `android/README.md` avec : (1) explication de la structure attendue (cmd/, internal/, frontend/), (2) liens vers la doc gomobile et Wails Android (s'il existe), (3) note "implémentation reportée — voir epic <futur>"
    - Si déjà décidé : créer `android/internal/blocklist/` comme 3e copie du package (cohérent avec la décision "dupliqué"). Sinon laisser pour la phase d'implémentation Android future.
  - Verify: `go build ./...` toujours vert (Android est juste un placeholder, pas de code Go fonctionnel).
  - Commit: `feat(android): scaffold android/ directory as future implementation placeholder`

#### PHASE 7 — CLEANUP RACINE (4 commits)

À ce point, `windows/`, `linux/`, `relay/`, `tools/` contiennent tout le code nécessaire. L'ancien `cmd/*` et `internal/*` racine OS-spécifique sont **redondants**. On les supprime par paquets logiques.

- [ ] **Task 7.1: Supprimer ancien `cmd/{client,ui,ctl}/` racine**
  - Files: `cmd/client/`, `cmd/ui/`, `cmd/ctl/` (entiers)
  - Verify: `go build ./...` vert (le code racine `cmd/{client,ui,ctl}` n'existe plus, mais `windows/cmd/...` et `linux/cmd/...` couvrent le besoin).
  - Commit: `refactor: remove root cmd/{client,ui,ctl} (migrated to windows/ and linux/)`

- [ ] **Task 7.2: Supprimer ancien `internal/*` OS-spécifique racine**
  - Files (suppression complète) : `internal/firewall/`, `internal/tun/`, `internal/dns/`, `internal/routing/`, `internal/ui/`, `internal/service/`, `internal/ipc/`, `internal/preflight/`, `internal/config/`, `internal/anomaly/`, `internal/uiwatchdog/`, `internal/elevation/`, `internal/browser/`, `internal/ctlauth/`, `internal/blocklist/` (le pkg dupliqué — l'original racine est supprimé, les copies windows/+linux/ subsistent)
  - **Restent à la racine `internal/`** : `internal/captive/`, `internal/leakcheck/`, `internal/watchdog/`, `internal/httpproxy/`, `internal/ipchandler/`, `internal/stun/` (à supprimer si confirmé dead code), `internal/tunnel/`, `internal/updater/`, `internal/crypto/`, `internal/registry/`
  - Verify: `go build ./windows/...` + `go build ./linux/...` + `go build ./relay/...` + `go build ./tools/...` verts. `go build ./internal/...` (les agnostiques) vert. `go test ./...` vert.
  - Commit: `refactor: remove root internal/* OS-specific packages (migrated to windows/ and linux/)`

- [ ] **Task 7.3: Supprimer ancien `frontend/`, `build/`, racine restantes**
  - Files: `frontend/` (entier), `build/` (entier — windows/ et appicon.png déplacés en 4.10 mais le dossier `build/` racine peut être devenu vide ou contient encore `bin/`)
  - Verify: `go build ./...` vert.
  - Commit: `refactor: remove root frontend/ and build/ (migrated to per-OS roots)`

- [ ] **Task 7.4: Supprimer ancien `Makefile`, `.goreleaser*.yaml`, `scripts/`**
  - Files: `Makefile`, `.goreleaser.yaml`, `.goreleaser-linux-only.yaml`, `scripts/` (entier — tout son contenu a été splitté en `windows/scripts/`, `linux/scripts/`, `relay/scripts/`, `tools/`).
  - Verify: `go build ./...` vert. `git status` propre. Le repo n'a plus de chaîne de build à la racine.
  - Commit: `refactor: remove root Makefile, goreleaser configs and scripts (split per OS)`

#### PHASE 8 — VALIDATION FINALE (1 commit, optionnel)

- [ ] **Task 8.1: Audit final + documentation README**
  - Action: mettre à jour `README.md` racine avec la nouvelle structure (1 paragraphe + arborescence). Indiquer les commandes par OS : `cd windows && make`, `cd linux && make`, `cd relay && make`.
  - Action: chacun de `windows/README.md`, `linux/README.md`, `relay/README.md` (s'il existe) avec instructions de build/test/release pour cet OS.
  - Action: vérifier qu'aucun fichier `.gitignore` ne référence des paths obsolètes (`internal/tun/wintun_dll_windows.go` etc.) → adapter pour les nouveaux paths.
  - Action: `grep -r "cmd/client\|cmd/ui\|cmd/ctl\|internal/firewall\|internal/tun" .github/ docs/` pour s'assurer qu'aucune doc/CI ne référence les anciens paths.
  - Verify: `go build ./...`, `go test ./...`, `go vet ./...` tous verts. Build NSIS Win + nfpm Linux + relay snapshot OK.
  - Commit: `docs: update README with new per-OS structure`

### Acceptance Criteria

- [ ] **AC1 (Isolation Windows ↔ Linux)** : Given le repo après migration complète, when je modifie un fichier `windows/internal/firewall/firewall_windows.go`, then aucun fichier sous `linux/` n'est affecté et `go build ./linux/...` reste vert sans rebuild.

- [ ] **AC2 (Isolation Linux ↔ Windows)** : Given le repo après migration complète, when je modifie un fichier `linux/internal/firewall/firewall_linux.go`, then aucun fichier sous `windows/` n'est affecté et `go build ./windows/...` reste vert sans rebuild.

- [ ] **AC3 (Build Windows complet)** : Given le repo migré et un poste Windows, when je lance `cd windows && make`, then les binaires `levoile-service.exe`, `levoile-ui.exe`, `levoile-ctl.exe` sont produits dans `windows/dist/` et l'installeur NSIS `windows/installer/LeVoile-Setup.exe` est généré sans erreur.

- [ ] **AC4 (Build Linux complet)** : Given le repo migré et un poste Linux, when je lance `cd linux && make`, then les binaires `levoile-service`, `levoile-ui`, `levoile-ctl` sont produits dans `linux/dist/` et le paquet `.deb` (et `.rpm`/`.apk` si configuré) est généré via nfpm sans erreur.

- [ ] **AC5 (Build relay autonome)** : Given le repo migré, when je lance `cd relay && make` ou `cd relay && goreleaser release --snapshot --clean`, then le binaire `levoile-relay`, `deploy/install.sh` (signé) et `deploy/install.sh.sig` sont produits sans dépendance vers `windows/` ou `linux/`.

- [ ] **AC6 (Cross-compile interdit)** : Given le repo migré, when je tente `cd linux && GOOS=windows go build ./...`, then le build échoue (comportement attendu — le code Linux ne contient aucune définition Windows).

- [ ] **AC7 (UI Windows démarre)** : Given une installation Windows propre via `LeVoile-Setup.exe`, when je lance `levoile-ui.exe`, then le tray apparaît et la webview se charge avec l'UI Wails sans message d'erreur.

- [ ] **AC8 (UI Linux démarre)** : Given une installation Linux propre via `dpkg -i levoile_*.deb`, when je lance `levoile-ui`, then le tray apparaît et la webview se charge avec l'UI sans message d'erreur.

- [ ] **AC9 (Wintun DLL embarquée)** : Given le binaire `windows/dist/.../levoile-service.exe` produit, when je l'examine, then `wintun.dll` est embarquée (vérifiable par hash d'un fichier extrait au runtime ou inspection du binaire).

- [ ] **AC10 (Aucun reste obsolète)** : Given le repo après Phase 7, when je liste la racine, then les dossiers `cmd/{client,ui,ctl}/`, `internal/{firewall,tun,dns,routing,ui,service,ipc,preflight,config,anomaly,uiwatchdog,elevation,browser,ctlauth,blocklist}/`, `frontend/`, `installer/`, `packaging/`, `build/` et les fichiers `Makefile`, `.goreleaser.yaml`, `.goreleaser-linux-only.yaml` racine **n'existent plus**.

- [ ] **AC11 (Outils maintainer fonctionnels)** : Given le repo migré, when je lance `go run ./tools/genkey` ou `go run ./tools/signpkg ...`, then les outils s'exécutent depuis n'importe quel OS sans dépendance à `windows/` ou `linux/`.

- [ ] **AC12 (Tests verts par OS)** : Given le repo migré, when je lance `cd windows && go test ./...` (sur Windows) puis `cd linux && go test ./...` (sur Linux), then tous les tests unitaires et d'intégration passent (sauf ceux explicitement marqués `e2e` qui nécessitent flag dédié).

- [ ] **AC13 (Tag Git par OS)** : Given une release post-migration, when je fais `git tag windows-v1.2.0` et `cd windows && goreleaser release`, then la release GitHub est publiée avec uniquement les artefacts Windows, sans artefacts Linux ni relay.

- [ ] **AC14 (Modification cross-tier safe)** : Given le repo migré, when je modifie `internal/crypto/sign.go` (package shared racine), then les builds Windows ET Linux ET relay reflètent la modification (comportement attendu — `internal/crypto` reste partagé).

- [ ] **AC15 (Blocklist drift contrôlé)** : Given `internal/blocklist` dupliqué Win/Linux, when je modifie `windows/internal/blocklist/manager.go`, then `linux/internal/blocklist/manager.go` n'est pas modifié (drift assumé — décision Akerimus). Une procédure manuelle de synchronisation est documentée pour les deux copies.

## Additional Context

### Dependencies

**Internes au repo (pré-existant)** :
- Module Go unique (`go.mod` racine, conservé)
- Wails v2 (frontend embarqué via `//go:embed`)
- Wintun DLL 0.14.1 (Windows, embarquée via `//go:embed`, fetch-wintun.sh téléchargement)
- NSIS + makensis (Windows installer)
- goreleaser + nfpm (Linux paquets deb/rpm/apk)
- WebView2 (Windows runtime utilisateur)
- WebKit2GTK (Linux dépendance système)
- kardianos/service (cross-OS service wrapper)

**Pré-requis pour l'exécution de la migration** :
- Poste Windows avec Go, NSIS, goreleaser, Git Bash (pour les `.sh`) ou PowerShell, accès au binaire Wintun
- Poste Linux (ou VM/container) avec Go, goreleaser, nfpm, dpkg-deb, makepkg (pour les tests AUR)
- Accès à un environnement de test pour smoke test des binaires (NSIS install + tray Win, deb install + systemd Linux)
- Branche dédiée `refacto/cross-platform-isolation` (suggestion) — la migration touche ~470 fichiers, mieux vaut isoler du tronc principal

**Aucune dépendance externe nouvelle** introduite par cette refacto. Pas de nouveau module Go, pas de nouvelle bibliothèque, pas de changement de version d'outillage.

### Testing Strategy

#### Tests automatisés requis à chaque commit

| Niveau | Commande | Quand |
|---|---|---|
| Sanity build | `go build ./...` | À CHAQUE commit (cross-OS, depuis Windows ou Linux) |
| Vet | `go vet ./...` | À chaque commit |
| Tests unitaires | `go test ./...` (sans tags e2e) | À chaque commit, sur Windows ET sur Linux |
| Tests OS-spécifiques | `go test ./windows/...` (Win) / `go test ./linux/...` (Linux) | À partir de Phase 4.12 / 5.12 |
| Tests e2e opt-in | `go test -tags=e2e ./...` | À chaque jalon (fin Phase 4, 5, 7) |
| Tests integration Linux | `go test -tags=integration ./linux/internal/firewall/...` | À chaque jalon Linux |

#### Tests manuels par jalon

**Fin Phase 1 (nettoyage)** :
- `go build ./...` sur Windows ET Linux : verts
- Aucun stub no-op orphelin résiduel

**Fin Phase 3 (relay)** :
- `cd relay && goreleaser release --snapshot --clean` produit `levoile-relay`, `deploy/install.sh`, signature
- Lancer `relay/dist/.../levoile-relay --version` → output correct
- Le binaire relay reste fonctionnel (smoke test optionnel : déploiement sur VPS de test)

**Fin Phase 4 (Windows COPY)** :
- `cd windows && make` : build vert
- `cd windows && make test` : tests unitaires verts
- `cd windows && go test -tags=e2e ./...` : tests e2e Windows verts
- `cd windows/installer && pwsh build.ps1` : produit `LeVoile-Setup.exe`
- Installation NSIS sur Windows propre : binaires posés correctement
- Lancement `levoile-ui.exe` : tray apparaît, webview charge l'UI sans erreur (regarder `%APPDATA%\LeVoile\ui-service-start.log`)
- Vérifier que les `*.ico` embarqués sont bien lus depuis `windows/internal/ui/icons/`
- Vérifier que `wintun.dll` est extraite au démarrage et que la TUN se monte

**Fin Phase 5 (Linux COPY)** :
- `cd linux && make` : build vert
- `cd linux && make test` : tests verts
- `cd linux && go test -tags=e2e ./...` : verts (peut nécessiter root + nft + iproute2)
- `cd linux && goreleaser release --snapshot --clean` : produit `.deb`, `.rpm`, `.apk`
- Installation `.deb` sur VM Debian propre : `systemctl start levoile.service` OK, `systemctl --user start levoile-ui.service` OK
- Lancement de l'UI : tray apparaît, webview charge sans erreur
- Test AUR : `linux/scripts/test-aur-install.sh` → vert

**Fin Phase 6 (Android stub)** :
- `go build ./...` : vert (le stub `android/` n'introduit aucune compilation)
- Vérifier que `android/README.md` documente clairement le statut "scaffold only"

**Fin Phase 7 (cleanup)** :
- `go build ./...` : vert sur Windows ET Linux
- `go test ./...` : vert
- `git status` : propre
- Aucun fichier `cmd/*/main.go`, `internal/firewall/*`, `Makefile` racine, `.goreleaser*.yaml` racine, `installer/`, `packaging/`, `frontend/`, `build/` racine ne doit subsister
- Re-test des smoke tests Phase 4.12 et 5.12 → toujours OK
- Re-test smoke relay Phase 3 → toujours OK

**Fin Phase 8 (validation)** :
- README racine : clair sur la nouvelle structure
- Aucune référence aux anciens paths dans `.github/`, `docs/`, `_bmad-output/sprint-status.yaml` (sauf historique sprint passé)
- Tag Git de release par OS testé (au moins une release snapshot de chaque)

#### Critère "vert" non négociable entre étapes

À CHAQUE commit (sauf le commit de cleanup Phase 7 qui supprime du code mort) :
1. `go build ./...` sur Windows
2. `go build ./...` sur Linux
3. `go vet ./...` propre

Si une étape ne peut pas tenir cette règle (cas exceptionnel), le commit doit être marqué `WIP` dans son message et le suivant doit revenir vert immédiatement. **Pas de série de commits cassés**.

### Notes

#### Risques et mitigations

| Risque | Probabilité | Impact | Mitigation |
|---|---|---|---|
| Cascade d'imports cassée pendant Copy (Phase 4/5) | Moyen | Casse build temporaire | Ne PAS supprimer l'ancien `internal/*` racine pendant Phase 4/5 ; la suppression est concentrée en Phase 7 |
| Embed paths cassés (icons, wintun, frontend) | Élevé | Build OK mais runtime KO | Vérifier après chaque commit Copy : `find windows/ -name '*.ico'` confirme la présence ; lancer un build et inspecter le binaire (icons/wintun extraits) |
| Drift `internal/blocklist` Win vs Linux | Faible (au début) | Bug fonctionnel divergent | AC15 documenté ; ajouter à la doc DEVOPS un check trimestriel `diff -r windows/internal/blocklist linux/internal/blocklist` |
| Outil oublié à classer sous `windows/tools/` ou `linux/scripts/` | Moyen | Build OK mais script orphelin | Phase 8.1 fait un grep final `cmd/`, `internal/`, `tools/` dans toutes les CI/scripts/docs |
| Cross-compilation tentée par habitude | Faible | Erreur de release | AC6 acte que le cross-compile est *interdit* (et échouera) ; documenter dans README |
| `internal/stun` est dead code | Très faible | Aucun (juste résidu) | Vérifier en Phase 7 : `grep -r "internal/stun" .` après cleanup ; si zéro importeur, supprimer le package en commit dédié |
| `webview_nocgo.go` (`internal/ui/`) — fallback non-cgo | Inconnu | Build casse si retiré à tort | Avant Phase 4.6 : confirmer qu'il sert (sinon le supprimer ; sinon le copier dans Win + Linux) |
| `cmd/ui/app_windows.syso` (binary resource) | Faible | Build sans icône Win | Le `.syso` est un objet linker — vérifier qu'il est bien copié (texte binaire, le `git mv` peut surprendre s'il n'est pas tracké en LFS — vérifier `.gitattributes`) |
| Wintun DLL non re-générée après déplacement | Élevé | Build Win KO | Phase 4.10 patch `windows/scripts/fetch-wintun.sh` ; s'assurer que `windows/Makefile` lance `bash scripts/fetch-wintun.sh` avant `goreleaser build` |
| Tests e2e cassés par paths absolus | Faible | CI rouge | Step 2 a confirmé : aucun chemin absolu dans les tests (utilisent `t.TempDir()`). Vérifier après cleanup que les commandes shell (nft, ipconfig, resolvectl) sont toujours détectées au runtime |
| Sprint en cours interrompu par la refacto | Élevé | Conflit commits | Branche dédiée `refacto/cross-platform-isolation`. Merger après stabilisation. Ne pas mélanger avec un autre refacto en parallèle |
| `_bmad-output/test-artifacts/gen-*.go` référence `internal/relay` | Faible | Génération de tests cassée | Step 2 a noté ces fichiers (`gen-backend-tunnel-client.go`, `gen-api-verify-handler.go`). Patcher leurs imports en Phase 3.1 |

#### Limitations connues

- **Pas d'orchestration multi-OS automatisée** : décision Akerimus. Une release par OS, manuellement déclenchée. Si le besoin d'orchestrer émerge plus tard, ce sera une étape 2 (workflow GitHub Actions multi-job, ou script orchestrateur racine).
- **`internal/blocklist` dupliqué** : le drift est possible (et même probable à long terme). À surveiller via diff périodique.
- **Pas de Darwin** : décision actée. Si macOS devient cible plus tard, créer `darwin/` au modèle Win/Linux. Re-introduire les `_darwin.go` supprimés implique de les ressortir du Git history.
- **Pas de `shared/`** : volontairement, pour ne pas introduire une 4e racine pendant cette refacto. Une future étape 2 pourra extraire des abstractions communes si pertinent.
- **Module Go unique conservé** : si Akerimus veut un jour des `go.mod` séparés par OS (multi-module workspace), c'est une refacto distincte (étape 2/3 future), beaucoup plus lourde.

#### Considérations futures (out of scope, mais à noter)

- **Implémentation Android effective** : `android/` est un stub. L'implémentation viendra dans un epic dédié, probablement après stabilisation Win/Linux.
- **Documentation publique** (`README.md`, `docs/`) : adapter le wording pour expliquer la structure aux contributeurs externes. Pas dans le scope de cette refacto, mais à prévoir.
- **CI matrix** : actuellement `release.yml` build sur ubuntu-latest. Avec l'isolation, on peut introduire des jobs séparés `windows-build` (sur `windows-latest`) et `linux-build` (sur `ubuntu-latest`). Suggestion pour étape 2.
- **Re-test du `update-checker`** : `internal/updater` reste à la racine (OS-agnostique), mais il valide les binaires téléchargés. Vérifier qu'il fonctionne avec les nouvelles releases per-OS (qui auront des noms d'archives différents : `levoile-windows-v1.2.0.zip` vs `levoile-linux-v1.2.0.tar.gz` au lieu d'un goreleaser unifié).
- **Audit `internal/stun`** : suspecter de dead code (aucun importeur trouvé en step 2). Si confirmé en Phase 7, supprimer en commit dédié.
- **`scripts/test-auto-update-linux.sh`** : utilise des paths système absolus (`/var/lib/levoile/updates/`) qui ne changent pas — pas de patch path nécessaire. Mais à re-tester après migration pour valider le pipeline auto-update.
- **`internal/tunnel`** : a une dépendance `internal/relay` via tests. Si la dépendance test devient inacceptable (pour isoler les tiers client/serveur), on pourra extraire les types partagés dans `internal/registry` ou `internal/crypto`. Pas dans le scope de cette refacto.

#### Décisions à reconfirmer en Phase 4 (au moment d'agir)

- `webview_nocgo.go` — utilité réelle ?
- `cmd/ui/app.rc` — est-il consommé par le compiler Go ou seulement par `go build` Windows pour générer le `.syso` ? Vérifier le pipeline.
- `build/appicon.png` — utilisé par Wails côté Windows ET Linux ? À copier dans windows/build/ ET linux/build/ si oui.
- `build/bin/levoile-desktop.exe` (legacy) — toujours présent ? À supprimer si oui.
- Fichiers `internal/ui/anomaly_*` (`anomaly_httpserver_test.go`, `anomaly_tray_test.go`) — vraiment dans le package `ui` ou doivent migrer vers `internal/anomaly` ? Vérifier l'organisation actuelle.

### Notes

- Inventaire complet produit en step 2 (353 fichiers Go, ~115 fichiers non-Go).
- Plan de migration ordonné (commit par commit) à détailler en step 3.
- La validation complète (lancement du binaire + smoke UI) nécessite exécution manuelle par Akerimus à chaque jalon (Windows local + machine Linux pour les phases respectives).
- **Décisions à confirmer en step 3 ou par Akerimus avant step 3** :
  - Stratégie Darwin : abandon (suppression de 5 fichiers `_darwin.go` + 5 stubs cross-OS) ou conservation (création racine `darwin/`) ?
  - `internal/config/` : split en `windows/internal/config` + `linux/internal/config`, ou conservation racine avec build tags ?
  - Statut de `build/chrome-cws.zip` (extension Chrome — partagée ou par-OS ?)
  - Statut de `filters.xml` (612 KB, dump WFP du 2026-04-21 — utilité réelle dans l'arbre ou à supprimer/déplacer dans `_bmad-output/` ?)
  - Audit rapide en step 3 des packages `internal/*` non encore classés : `captive`, `anomaly`, `leakcheck`, `uiwatchdog`, `watchdog`, `elevation`, `browser`, `httpproxy`, `ipchandler`, `ctlauth`, `stun`, `tunnel`, `updater`.
