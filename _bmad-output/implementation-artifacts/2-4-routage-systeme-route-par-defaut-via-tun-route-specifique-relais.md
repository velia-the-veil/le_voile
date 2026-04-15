# Story 2.4 : Routage système — route par défaut via TUN + route spécifique relais

Status: ready-for-dev

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

- [ ] **Task 1 : Créer le package `internal/routing/` avec contrat commun** (AC: #3, #7, #8)
  - [ ] Créer `internal/routing/routing.go` : interface `RouteManager`, type `SavedRoutes` (origGateway, origDefaultIface, ...), fonction constructeur `New() RouteManager` switché par build tag
  - [ ] Créer `internal/routing/routing_test.go` : tests de l'interface + mock (matrice OS-agnostique)
  - [ ] Définir erreurs sentinelles : `ErrAlreadyActive`, `ErrNotActive`, `ErrGatewayResolve`
  - [ ] Wrapper toutes les erreurs avec préfixe `routing:` (convention architecture §Conventions)

- [ ] **Task 2 : Implémentation Linux via `iproute2`** (AC: #1, #3, #4, #6)
  - [ ] Créer `internal/routing/routing_linux.go` (build tag `//go:build linux`)
  - [ ] Capturer route par défaut originale via `ip route show default` parsing (gateway + iface)
  - [ ] `Setup` : exécuter `ip route add 0.0.0.0/0 dev {tunName} table 51820`, `ip rule add from all lookup 51820 priority 100`, `ip route add {relayIP}/32 via {origGateway}` — préférer `github.com/vishvananda/netlink` si déjà transitif, sinon shellout via `exec.Command` (cohérent avec pattern `firewall_linux.go` qui shellout `nft`)
  - [ ] `Teardown` : exécuter `ip rule del priority 100`, `ip route flush table 51820`, `ip route del {relayIP}/32` (idempotent : ignorer `RTNETLINK answers: No such file or directory`)
  - [ ] `Cleanup` (appelé au boot) : si `ip rule list | grep "lookup 51820"` retourne qqch → purger. Si `ip route show table 51820` non vide → flush
  - [ ] Créer `internal/routing/routing_linux_test.go` : tests réels nécessitent `CAP_NET_ADMIN` → marquer `t.Skip()` si `os.Geteuid() != 0`, sinon tester cycle Setup/Teardown réel sur interface dummy

- [ ] **Task 3 : Implémentation Windows via `winipcfg`** (AC: #2, #3, #4, #6)
  - [ ] Créer `internal/routing/routing_windows.go` (build tag `//go:build windows`)
  - [ ] Utiliser `golang.zx2c4.com/wireguard/windows/conf/winipcfg` (déjà transitive via wireguard-go) pour API `LUID.AddRoute`, `LUID.DeleteRoute`, `GetIPForwardTable2`, `GetBestInterfaceEx`
  - [ ] Capturer gateway originale via `GetIPForwardTable2` — prendre la default route (`DestinationPrefix == 0.0.0.0/0`) avec la plus petite metric sur interface non-TUN
  - [ ] `Setup` : récupérer `LUID` du TUN via nom → `luid.AddRoute(0.0.0.0/0, nextHop=0.0.0.0, metric=5)` + `AddRoute({relayIP}/32, nextHop=origGateway, metric=1)` sur LUID interface originale
  - [ ] `Teardown` : supprimer les routes ajoutées (tracker en mémoire dans la struct), la route par défaut originale reste intacte (pas été supprimée, juste masquée par metric basse)
  - [ ] `Cleanup` au boot : scanner `GetIPForwardTable2`, supprimer les routes `/32` orphelines dont la `NextHop` pointe vers l'IP d'un ancien relais connu **ou** les routes sur un adapter Wintun `levoile0` résiduel
  - [ ] Créer `internal/routing/routing_windows_test.go` : skip si non-admin

- [ ] **Task 4 : Intégration dans `internal/service/`** (AC: #5)
  - [ ] Dans `service.Connect()` : insérer `routing.Setup()` **après** `tun.New()` et **avant** `firewall.Activate()`
  - [ ] Dans `service.Disconnect()` : insérer `routing.Teardown()` **après** `firewall.Deactivate()` et **avant** `tun.Close()`
  - [ ] Dans `service.Start()` (boot) : appeler `routing.Cleanup()` **après** `firewall.Cleanup()` et **avant** première tentative Connect (NFR17)
  - [ ] Gestion rollback : si `routing.Setup()` échoue → `tun.Close()` puis retour erreur propagée à l'UI via IPC

- [ ] **Task 5 : Résolution gateway originale** (AC: #3)
  - [ ] Fonction helper `captureOriginalGateway() (net.IP, error)` dans chaque impl OS — doit être **atomique** (appelée avant toute mutation)
  - [ ] Stocker dans champ privé de la struct `routing_<os>.go` : `saved SavedRoutes`
  - [ ] Si la gateway ne peut être résolue → erreur bloquante (pas de fallback : sans elle, pas de route `/32` relais = routing loop garanti)

- [ ] **Task 6 : Tests E2E plateforme** (AC: all)
  - [ ] `internal/routing/e2e_test.go` (build tag `e2e`) : cycle complet Setup → vérifier routes présentes (`ip route show table 51820` / `GetIPForwardTable2`) → Teardown → vérifier restauration complète
  - [ ] Test scénario crash : Setup, puis kill process, puis Cleanup au redémarrage → doit purger les résidus < 5s (NFR17)
  - [ ] Test scénario concurrent : deux Setup successifs sans Teardown intermédiaire → second doit retourner `ErrAlreadyActive`

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

### Completion Notes List

- Story créée 2026-04-15 via `create-story` workflow — nouvelle Epic 2 post-restructure. Epic 1 (ancien) obsolète.
- **Attention** : package `internal/routing/` à créer from scratch — absent du code baseline.
- Alignement orchestration service strict : `tun → routing → firewall → tunnel` (Connect) / inverse (Disconnect).

### File List
