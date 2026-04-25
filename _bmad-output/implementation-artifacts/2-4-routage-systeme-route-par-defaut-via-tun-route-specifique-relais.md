# Story 2.4 : Routage système — route par défaut via TUN + route spécifique relais

Status: done

<!-- Note : validation optionnelle — lancer validate-create-story avant dev-story si besoin. -->

## Story

**As a** utilisateur final,
**I want** que tout le trafic IP de ma machine soit routé via `levoile0`, sauf le trafic vers l'IP du relais qui passe par la gateway originale,
**So that** le trafic est tunnelisé sans créer de routing loop.

## Acceptance Criteria

1. **AC1 — Setup Linux** : `Given` l'interface `levoile0` est active et l'IP du relais est résolue, `When` `routing.Setup(tunName, relayIP, origGateway)` est appelé sur Linux, `Then` les commandes suivantes sont exécutées avec succès :
   - `ip route add 0.0.0.0/0 dev levoile0 table 51820`
   - `ip rule add from all lookup 51820 priority 100`
   - `ip route add {relayIP}/32 via {origGateway}`
2. **AC2 — Setup Windows** : `Given` l'interface Wintun `levoile0` est active, `When` `routing.Setup(...)` est appelé sur Windows, `Then` via `winipcfg` une route par défaut via TUN est ajoutée avec metric basse **ET** une route `/32` vers `relayIP` via la gateway originale est ajoutée avec metric haute.
3. **AC3 — Sauvegarde pour restauration** : la gateway originale et la route par défaut système sont capturées en mémoire (struct interne) **avant** toute modification, afin de permettre la restauration (NFR6).
4. **AC4 — Teardown** : `Given` le tunnel est désactivé, `When` `routing.Teardown()` est appelé, `Then` toutes les routes/rules ajoutées par Le Voile sont supprimées, **And** la route par défaut originale est restaurée à son état initial (NFR6), **And** la fonction est idempotente (rappel sans erreur).
5. **AC5 — Ordre d'orchestration** : `routing.Setup()` est appelé **après** `tun.New()` et **avant** `firewall.Activate()` (cf. enforcement architecture). `routing.Teardown()` est appelé **après** `firewall.Deactivate()` et **avant** `tun.Close()`.
6. **AC6 — Robustesse redémarrage** : au démarrage du service, si des routes/rules orphelines `table 51820` / `priority 100` (Linux) ou avec signature Le Voile (Windows) existent, elles sont nettoyées avant toute nouvelle `Setup()` (NFR17).
7. **AC7 — Interface Go & build tags** : package `internal/routing/` expose l'interface `RouteManager` avec `Setup(tunName string, relayIP net.IP, origGateway net.IP) error`, `Teardown() error`, `Cleanup() error`. Implémentations séparées via build tags : `routing_linux.go`, `routing_windows.go`.
8. **AC8 — Erreurs wrappées** : toute erreur OS (commande shell, syscall, WMI) est wrappée avec le préfixe `routing:` et remonte la cause.

## Tasks / Subtasks

- [x] **Task 1 : Créer le package `internal/routing/` avec contrat commun** (AC: #3, #7, #8)
  - [x] Créer `internal/routing/routing.go` : interface `RouteManager`, type `SavedRoutes` (origGateway, origDefaultIface, ...), fonction constructeur `New() RouteManager` switché par build tag
  - [x] Créer `internal/routing/routing_test.go` : tests de l'interface + mock (matrice OS-agnostique)
  - [x] Définir erreurs sentinelles : `ErrAlreadyActive`, `ErrNotActive`, `ErrGatewayResolve`
  - [x] Wrapper toutes les erreurs avec préfixe `routing:` (convention architecture §Conventions)

- [x] **Task 2 : Implémentation Linux via `iproute2`** (AC: #1, #3, #4, #6)
  - [x] Créer `internal/routing/routing_linux.go` (build tag `//go:build linux`)
  - [x] Capturer route par défaut originale via `ip route show default` parsing (gateway + iface)
  - [x] `Setup` : shellout `ip route add 0.0.0.0/0 dev {tunName} table 51820`, `ip rule add from all lookup 51820 priority 100`, `ip route add {relayIP}/32 via {origGateway}` via `exec.Command` (cohérent avec pattern firewall nft shellout — netlink PAS transitif)
  - [x] `Teardown` : `ip rule del priority 100`, `ip route flush table 51820`, `ip route del {relayIP}/32` (idempotent)
  - [x] `Cleanup` (boot) : purge rules/routes orphelines table 51820
  - [x] Créer `internal/routing/routing_linux_test.go` : skip si non-root

- [x] **Task 3 : Implémentation Windows via `netsh` shellout** (AC: #2, #3, #4, #6)
  - [x] Créer `internal/routing/routing_windows.go` (build tag `//go:build windows`)
  - [x] Utiliser `netsh interface ipv4 add/delete route` avec `SysProcAttr{HideWindow: true}` (winipcfg PAS transitif — module séparé de wireguard-go)
  - [x] Capturer gateway originale via `netsh interface ipv4 show route` parsing (Idx + résolution nom via `show interfaces`)
  - [x] `Setup` : route par défaut via TUN metric=5, route /32 relais via gateway originale metric=1
  - [x] `Teardown` : supprimer routes ajoutées (idempotent)
  - [x] `Cleanup` au boot : supprimer route 0.0.0.0/0 résiduelle sur levoile0
  - [x] Créer `internal/routing/routing_windows_test.go` : skip si non-admin

- [x] **Task 4 : Intégration dans `internal/service/`** (AC: #5)
  - [x] Dans `run()` §0g : insérer `routing.Cleanup()` + `routing.Setup()` **après** `tun.New()` et **avant** tunnel connect
  - [x] Dans `shutdown()` §8a : insérer `routing.Teardown()` **après** tunnel disconnect et **avant** `tun.Close()`
  - [x] `tunCleanup` error path inclut `routing.Teardown()` avant `tun.Close()`
  - [x] `routingFactory` injectable pour les tests (pattern tunFactory)

- [x] **Task 5 : Résolution gateway originale** (AC: #3)
  - [x] Fonction `CaptureOriginalGateway() (net.IP, error)` dans chaque impl OS (appelée par service avant Setup)
  - [x] `captureOriginalIface()` interne à chaque impl pour le nom d'interface
  - [x] Stocker dans champ privé `saved SavedRoutes`
  - [x] Gateway non résolue → erreur bloquante (routing non activé, non fatal pour le service)

- [x] **Task 6 : Tests E2E plateforme** (AC: all)
  - [x] `internal/routing/e2e_test.go` (build tag `e2e`) : cycle Setup → vérification → Teardown → vérification restauration
  - [x] `e2e_linux_test.go` + `e2e_windows_test.go` : helpers OS-spécifiques (skip privilege, ensure TUN, verify routes)
  - [x] Test scénario crash : Setup puis Cleanup sans Teardown (NFR17 < 5s)
  - [x] Test scénario concurrent : double Setup → `ErrAlreadyActive`

## Dev Notes

### Developer Context — CRITIQUE

**Ce package n'existe PAS encore** — c'est un nouveau package issu du refactor Epic 2 post-2026-04-15. Ne pas chercher `internal/routing/` dans le code existant, il faut le créer.

**Le code existant utilise `httpproxy` + `tunnel` à un niveau L4 (proxy) — ce refactor passe en L3 (TUN)**. Les stories 2.1 (création TUN) et 2.2 (watchdog TUN) précèdent celle-ci et doivent fournir :
- `tun.Device` avec nom d'interface exposé (`Name() string`) pour récupérer le LUID/iface index
- `tun.New(name, mtu)` retourne une `*Device` utilisable par `routing.Setup`

**Si tu implémentes 2.4 avant que 2.1 ne soit faite** : poser un mock `tun.Device` ou créer une dummy interface via `ip tuntap add` pour les tests Linux. Sur Windows, tester contre une interface Wintun pré-créée manuellement.

### Relevant Architecture Patterns

- **Ordre strict Connect/Disconnect** (architecture.md §Enforcement, ligne 687) :
  - Connect : `elevation → tun → routing → firewall → tunnel`
  - Disconnect : `tunnel → firewall → routing → tun`
  - **Ne jamais désactiver le routing sans désactiver le firewall avant** — sinon fenêtre de fuite.
- **Build tags** : `routing_linux.go` et `routing_windows.go` — pas de fichier cross-platform avec `runtime.GOOS` (convention architecture §Build tags, ligne 440).
- **Interfaces Go dans `internal/routing/`** exposent uniquement `RouteManager` — le service appelle exclusivement via cette interface (pas d'import cross-package de types internes).
- **Pattern shellout vs lib natif** : Linux firewall shellout `nft` (architecture §Firewall). Pour routing Linux, `vishvananda/netlink` est déjà transitif via wireguard — **préférer netlink** pour idempotence et parsing des erreurs. Shellout `ip route` acceptable en fallback mais moins robuste.
- **Pattern wireguard-go/winipcfg** : sur Windows, l'écosystème WireGuard fournit toutes les primitives dont on a besoin (`LUID.AddRoute`, `GetIPForwardTable2`). **Ne pas réimplémenter via P/Invoke direct**.

### Source Tree — fichiers à créer/toucher

```
internal/routing/                        # NOUVEAU package
├── routing.go                           # Interface RouteManager + SavedRoutes
├── routing_test.go                      # Tests unitaires + mock
├── routing_linux.go                     # //go:build linux — netlink + iproute2
├── routing_linux_test.go                # Skip si non-root
├── routing_windows.go                   # //go:build windows — winipcfg
├── routing_windows_test.go              # Skip si non-admin
└── e2e_test.go                          # //go:build e2e

internal/service/
└── service.go                           # MODIFIÉ — orchestration Connect/Disconnect/boot (routing.Setup/Teardown/Cleanup)

go.mod                                   # Ajouter github.com/vishvananda/netlink (Linux) si absent
```

### Testing Standards

- Tests co-localisés (convention architecture §Tests).
- Tests OS-spécifiques via build tags (ne pas polluer les tests cross-platform).
- Tests nécessitant privilèges : `t.Skip()` conditionnel sur `os.Geteuid() != 0` (Linux) / `windows.GetCurrentProcessToken` elevation check.
- E2E derrière `//go:build e2e` — exécuté en CI privilégié seulement.
- Viser couverture ≥ 70% sur le package (convention Go, NFR non-chiffré mais standard projet).

### Project Structure Notes

- Pas de conflit avec structure existante — nouveau package orthogonal.
- `internal/service/` existe déjà et devra être modifié — **ne pas toucher aux autres appels** (tunnel, registry, etc.), uniquement insérer les 3 appels routing aux bons endroits.
- Le package `internal/tun/` (story 2.1) doit exister avant full test E2E — si tu es en avance, créer un stub tun avec juste `Name() string`.

## Previous Story Intelligence

Les stories 2.1/2.2/2.3 du nouveau Epic 2 ne sont pas encore créées. **Pas de learnings antérieurs de ce refactor**.

Les fichiers `2-1-client-tunnel-quic-https-avec-connexion-au-relais.md`, `2-2-gestion-dns-systeme-et-redirection-doh.md`, `2-3-kill-switch-dns-watchdog-et-reconnexion-automatique.md` dans `_bmad-output/implementation-artifacts/` appartiennent à l'**ancien** Epic 2 (pre-restructure 2026-04-15) et ne sont **pas** pertinents — ne pas s'y référer, approche fondamentalement différente (proxy L4 vs capture L3).

## Git Intelligence

Commits récents (contexte ancien Windows-stable) :
- `c1d7c3a` feat: add ES/GB countries, raise quotas — touche registry/quotas, non lié.
- `66469e7` docs: update specs minimize-to-tray — UI, non lié.
- `a1adf3f` fix: hide console windows for netsh/net commands — **PERTINENT pour Linux shellout Windows** : sur Windows, les sous-processus lancés via `exec.Command` doivent avoir `SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}` pour ne pas flasher de console. Si tu shellouts `netsh` / `route` (fallback si winipcfg pose problème), **applique le même pattern** (chercher exemple dans le code existant touché par ce commit).
- `0b5314e` fix: random relay selection, proxy cleanup, MaxConnections 1000 — non lié.
- `8c9938d` fix: minimize-to-tray, webview cold start — non lié.

**Snapshot préservé** : `git tag windows-stable-2026-04-15` + `branch backup/windows-stable` — le refactor peut casser sans peur, le baseline est sauvegardé.

## Latest Tech Information

- **`github.com/vishvananda/netlink`** (Linux) : API stable, `netlink.RouteAdd`, `netlink.RuleAdd`. Idempotence : `RouteReplace` plutôt que `RouteAdd` pour éviter `EEXIST`. Doc : https://pkg.go.dev/github.com/vishvananda/netlink
- **`golang.zx2c4.com/wireguard/windows/conf/winipcfg`** (Windows) : fournit `LUID.AddRoute(dest netip.Prefix, nextHop netip.Addr, metric uint32)` — **netip** (Go 1.18+) préféré à `net.IP` pour les nouvelles API. Convertir via `netip.AddrFromSlice()` si on reçoit `net.IP` depuis l'appelant.
- **Table de routage Linux 51820** : convention WireGuard, libre pour Le Voile. Priorité rule 100 : choisie pour passer avant les rules système par défaut (32766 `from all lookup main`, 32767 `from all lookup default`).

## Project Context Reference

Cf. [architecture.md §Modules internal/routing](../planning-artifacts/architecture.md) lignes 329-332, 596-622, 761-767, 974-978.
Cf. [epics.md §Story 2.4](../planning-artifacts/epics.md) lignes 500-517.
Cf. [prd.md §NFR6, NFR17](../planning-artifacts/prd.md) lignes 510, 536.

## Story Completion Status

- Contexte : analyse exhaustive PRD + architecture + epics + code existant ✓
- Dépendances identifiées : story 2.1 (TUN) prérequise pour E2E complet ✓
- Questions ouvertes :
  1. **Choix netlink vs shellout Linux** : confirmer que `vishvananda/netlink` est acceptable (ajout dépendance go.mod) OU forcer shellout `ip route` pour rester aligné avec firewall (shellout `nft`). **Recommandation dev** : netlink (plus robuste, erreurs structurées).
  2. **Metric Windows** : valeurs `5` (TUN) et `1` (relay /32) arbitraires — vérifier sur Windows 11 que la route /32 avec metric 1 prend bien le pas sur la route par défaut metric 5 (devrait : /32 > /0 par longest-prefix match, metric secondaire).
  3. **IPv6** : story 2.9 traite IPv6 — ici, **scope IPv4 uniquement**. Ne pas ajouter de routes `::/0` — la story 2.9 décidera du traitement IPv6 (bloquer vs router).

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

- netsh `show route 0.0.0.0/0` → "Paramètre incorrect" — corrigé en utilisant `show route` sans filtre puis parsing des lignes 0.0.0.0/0
- `ifIdxToName` retournait "connected Ethernet" au lieu de "Ethernet" — format netsh FR a 5 colonnes (Idx Met MTU État Nom), nom à index [4:] pas [3:]
- `winipcfg` (story recommandation) n'est PAS transitif via wireguard-go — `golang.zx2c4.com/wireguard/windows` est un module Go séparé. Choix : shellout `netsh` (cohérent avec dns/cmd_windows.go, SysProcAttr HideWindow)
- `vishvananda/netlink` PAS transitif non plus — choix shellout `ip route`/`ip rule` pour Linux (cohérent avec firewall nft shellout)

### Completion Notes List

- Story créée 2026-04-15 via `create-story` workflow — nouvelle Epic 2 post-restructure. Epic 1 (ancien) obsolète.
- **Implémenté 2026-04-15** : package `internal/routing/` créé from scratch.
- Linux : shellout `ip route`/`ip rule` via exec.Command (table 51820, priority 100).
- Windows : shellout `netsh interface ipv4 add/delete route` avec HideWindow.
- `CaptureOriginalRoute()` (atomique) exposée — retourne gateway+iface en un seul appel (fix TOCTOU review H1).
- Intégration service : §0g (Cleanup+Setup après TUN), §8a (Teardown avant tun.Close), tunCleanup error path.
- routingFactory injectable pour tests (pattern tunFactory).
- Tests : 10 unit (cross-platform + Windows), 3 E2E (build tag e2e). Aucune régression service (32 tests pass).
- Questions story résolues : netlink→shellout, winipcfg→netsh, IPv6 hors scope (story 2.9).

### File List

- internal/routing/routing.go (NEW)
- internal/routing/routing_test.go (NEW)
- internal/routing/routing_linux.go (NEW)
- internal/routing/routing_linux_test.go (NEW)
- internal/routing/routing_windows.go (NEW)
- internal/routing/routing_windows_test.go (NEW)
- internal/routing/e2e_test.go (NEW)
- internal/routing/e2e_linux_test.go (NEW)
- internal/routing/e2e_windows_test.go (NEW)
- internal/service/service.go (MODIFIED)

## Change Log

- 2026-04-15: Implémentation complète — package internal/routing/ créé (interface RouteManager + Linux shellout iproute2 + Windows shellout netsh), intégré dans service.go (Connect §0g, Disconnect §8a, boot Cleanup), 10 tests unitaires + 3 tests E2E (build tag e2e). Choix netsh au lieu de winipcfg (non transitif), shellout ip au lieu de netlink (non transitif).
- 2026-04-15: Code review adversarial — 7 issues trouvées, toutes fixées : H1 (TOCTOU CaptureOriginalRoute atomique), H2 (findDefaultGateway test fix), M1 (Windows Cleanup /32 orphelines), M2 (ErrNotActive supprimé), M3+M4+L1 (test helpers nettoyés, os hack supprimé, doc fixée). Setup signature étendue à 4 args (origIface). 15 tests routing pass + 32 service pass.
