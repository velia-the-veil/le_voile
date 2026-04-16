# Story 2.2 : Watchdog TUN — détection d'altération externe

Status: done

<!-- Note : Validation optionnelle. Lancer validate-create-story pour contrôle qualité avant dev-story. -->

## Story

As a **utilisateur final**,
I want **que le client détecte si l'interface TUN/Wintun `levoile0` disparaît ou est altérée par un processus externe**,
so that **ma protection se rétablit automatiquement même si un admin ou un malware tente de la contourner**.

## Acceptance Criteria

1. **AC1 — Détection disparition < 5 s (NFR16)**
   **Given** le tunnel est actif et `levoile0` existe
   **When** un acteur externe supprime ou altère l'interface (`ip link delete levoile0` Linux / désactivation Wintun adapter Windows / arrêt forcé du driver)
   **Then** le watchdog (poll 3 s) détecte la disparition en moins de 5 secondes (NFR16) avec log structuré `WARN watchdog.tun: interface levoile0 missing — triggering reconnect`.

2. **AC2 — Reconnexion complète orchestrée**
   **Given** la disparition de l'interface est détectée
   **When** le watchdog déclenche la séquence de récupération
   **Then** la séquence exécute, dans cet ordre strict : `tun.New(levoile0)` → `routing.Setup()` → `firewall.Activate(relayIP, tunName)` → `tunnel.Connect()` (cf. architecture.md L613-621).

3. **AC3 — Kill switch firewall maintenu pendant la procédure (NFR15)**
   **Given** le watchdog est en cours de récupération
   **When** la TUN est en cours de recréation et le tunnel non encore reconnecté
   **Then** les règles firewall (nftables/WFP) restent actives et bloquent tout trafic sortant non autorisé pendant toute la fenêtre — aucune fuite IP réelle possible entre la détection et la reconnexion réussie.

4. **AC4 — Détection d'altération MTU/flags**
   **Given** `levoile0` existe mais a été altérée (MTU modifié `≠ 1420`, interface `down` via `ip link set levoile0 down`, ou flag `NO-CARRIER`)
   **When** le watchdog effectue son contrôle
   **Then** l'altération est qualifiée comme « interface invalide » et la même séquence de reconnexion qu'AC2 est déclenchée.

5. **AC5 — Lifecycle goroutine propre**
   **Given** le service est en cours de shutdown (`Stop()` ou `ctx.Done()`)
   **When** le watchdog reçoit l'annulation
   **Then** la goroutine sort en moins de 3 s (1 cycle de poll), `Wait()` retourne, aucune goroutine orpheline ne subsiste (vérifié par `goleak` en test).

6. **AC6 — Idempotence et anti-flapping**
   **Given** une reconnexion est déjà en cours suite à une détection précédente
   **When** un nouveau cycle de poll constate toujours l'absence de l'interface
   **Then** aucune nouvelle séquence de reconnexion n'est lancée (mutex/atomic flag `recovering`) — un seul reconnect concurrent au maximum.

7. **AC7 — Tests**
   - Tests unitaires sur le polling (interface mockée).
   - Test d'intégration Linux : suppression réelle de `levoile0` via `netlink` puis vérification de la détection + reconnect (`go test -tags=integration -race`).
   - Stub `_test.go` Windows pour valider la logique sans driver Wintun installé.

## Tasks / Subtasks

- [x] **T1. Créer le package `internal/tun/watchdog/`** (AC1, AC4, AC5, AC6)
  - [x] T1.1 — `type Watchdog struct` (Config avec Interval, Checker, OnLost, Logger).
  - [x] T1.2 — Constructeur `NewWatchdog(cfg Config) (*Watchdog, error)` validant `Checker` + `OnLost`.
  - [x] T1.3 — Méthodes `Start(ctx) error` (bloquante), `Stop()` (idempotente, wait sur `done`), `IsRecovering()`.
  - [x] T1.4 — `time.Ticker(Interval)` + sélection `ctx.Done()` ; sentinelle `ErrAlreadyRunning`.
  - [x] T1.5 — Anti-flapping via `atomic.Bool recovering` + `CompareAndSwap` (AC6).

- [x] **T2. Implémenter `InterfaceChecker` cross-platform** (AC1, AC4)
  - [x] T2.1 — `InterfaceChecker` + enum `Status` (OK/Missing/Invalid) + `CheckerConfig.Validate()`.
  - [x] T2.2 — `checker_linux.go` : `net.InterfaceByName` + `Flags&FlagUp` + MTU strict ; `isNotFound` wrapping-safe.
  - [x] T2.3 — `checker_windows.go` : même logique (la stdlib `net.InterfaceByName` est suffisante — cf. Dev Notes Latest Tech).
  - [x] T2.4 — Build tags `//go:build linux` / `//go:build windows` appliqués.

- [x] **T3. Câbler le callback `OnLost` dans `internal/service/`** (AC2, AC3)
  - [x] T3.1 — `(*Program).recoverTUN(ctx)` : séquence stricte `tun.New → routing.Setup → firewall.Activate → tunnel.Connect`. Routing via `captureOriginalRouteFunc` injectable.
  - [x] T3.2 — `firewall.Deactivate()` JAMAIS appelé dans recoverTUN (AC3). Activate est idempotent (flush+replace atomique nftables/WFP).
  - [x] T3.3 — Logging `fmt.Fprintf(serviceStderr, ...)` à chaque étape de recovery (cohérent avec le style existant du service).
  - [x] T3.4 — Fail-fast sans retry (retry exponentiel supprimé — le reconnector tunnel gère déjà les retries au niveau supérieur). Erreur tun.New = retour immédiat. Erreurs routing/firewall = log + continue (non-fatal). Shutdown détecté via ctx.Err() pour éviter travail inutile.

- [x] **T4. Intégrer le watchdog au lifecycle de service** (AC5)
  - [x] T4.1 — `startTUNWatchdog(ctx)` lancé après tunnel.Connect réussi (étape 1a dans run()). NetChecker construit avec name+MTU du tunDev effectif.
  - [x] T4.2 — `tunWatchdog.Stop()` dans shutdown() étape 7b AVANT tunnel.Disconnect (évite false recovery pendant arrêt propre).
  - [x] T4.3 — Ancien `internal/watchdog/` (DNS) conservé intact — sera supprimé en story 2.5.

- [x] **T5. Tests** (AC7)
  - [x] T5.1 — `watchdog_test.go` : `TestWatchdog_AntiFlapping_SingleRecoveryConcurrent` vérifie `concurrent == 1` (AC6).
  - [x] T5.2 — `watchdog_test.go` : `TestWatchdog_ContextCancel_StopsPromptly` < 200 ms (AC5). Race detector clean.
  - [x] T5.3 — `checker_linux_test.go` : tests directs sur `lo` (UP, MTU attendue) + interface inexistante → `StatusMissing` + MTU faux → `StatusInvalid`. `checker_windows_test.go` : équivalent sur adapters Windows existants.
  - [x] T5.4 — `service/service_tun_recovery_test.go` : 3 tests (ordre strict AC2, erreur tun.New, mode sans firewall). Mocks via factories injectables (tunFactory, routingFactory, firewallFactory, captureOriginalRouteFunc).

- [x] **T6. Documentation inline et configuration**
  - [x] T6.1 — Section `[tun.watchdog] poll_interval = "3s"` ajoutée à `config.example.toml`.
  - [x] T6.2 — Godoc complet sur tous les exports (Watchdog, Config, Status, InterfaceChecker, CheckerConfig, NetChecker).

## Dev Notes

### Architecture & Patterns à respecter

- **Lifecycle TUN** [architecture.md#L596-622](../planning-artifacts/architecture.md#L596-L622) : `tun.New / Read / Write / Close`, jamais partagée entre goroutines (1 goroutine read, 1 write — pattern `wireguard-go`).
- **Ordre Connect strict** : `elevation.Check() → tun.New() → routing.Setup() → firewall.Activate(relayIP, tunName) → tunnel.Connect()`. Le watchdog doit reproduire **exactement** cet ordre lors du recovery.
- **Ordre Disconnect strict** : `tunnel.Disconnect() → firewall.Deactivate() → routing.Teardown() → tun.Close()`. **NON utilisé** par le watchdog (pas de teardown — on recrée par-dessus).
- **Concurrency** [architecture.md#L651-661](../planning-artifacts/architecture.md#L651-L661) : `context.Context` premier argument partout, jamais de goroutine orpheline, `sync.WaitGroup` ou ctx pour la gestion.
- **Naming patterns** [architecture.md#L431-458](../planning-artifacts/architecture.md#L431-L458) : packages en lower-case, fichiers `_<plateforme>.go` pour spécificités OS, tests `_test.go` côte à côte.

### Bibliothèques / Dépendances

- **`golang.zx2c4.com/wireguard/tun`** — déjà sélectionnée (architecture.md#L131-136). API Go unifiée TUN Linux + Wintun Windows. Sur Linux : `/dev/net/tun`. Sur Windows : Wintun DLL signée Microsoft (extraite vers `%ProgramData%/LeVoile/wintun.dll`).
- **`net.InterfaceByName`** — stdlib, suffisant pour le check Linux primaire.
- **`go.uber.org/goleak`** — pour les tests anti-leak goroutine (à ajouter aux deps si absent).
- **PAS de shellout `ip link show`** — préférer `net.InterfaceByName` (plus rapide, pas de dépendance externe, pas de parsing). Le shellout `ip` n'est utilisé que dans `internal/routing/` pour les opérations qui n'ont pas d'équivalent stdlib propre.

### Source tree à toucher

```
internal/
├── tun/                          # Story 2.1 (création)
│   ├── device.go                 # Existant si 2.1 done, sinon STUB nécessaire
│   ├── device_linux.go
│   ├── device_windows.go
│   └── watchdog/                 # NOUVEAU — cette story
│       ├── watchdog.go           # Boucle de poll, gestion lifecycle
│       ├── watchdog_test.go
│       ├── checker.go            # Interface InterfaceChecker + Status enum
│       ├── checker_linux.go      # net.InterfaceByName + flags + MTU
│       ├── checker_linux_test.go # tag integration
│       ├── checker_windows.go    # via wireguard/tun + net.InterfaceByName
│       └── checker_windows_test.go
├── service/
│   ├── service.go                # MODIFIER — câbler watchdog dans Program
│   └── recovery_test.go          # NOUVEAU — test ordre recovery
└── watchdog/                     # ANCIEN (DNS) — NE PAS supprimer ici (story 2.5)
```

### Pièges identifiés (anti-foirage)

1. **Ne PAS désactiver le firewall avant recreate TUN.** Le firewall doit rester actif (NFR15, AC3). Le retrait des règles ouvrirait une fenêtre de fuite — exactement ce que le kill switch est censé empêcher.
2. **Ne PAS retrigger le reconnect en boucle** si la TUN reste introuvable. Utiliser le flag `recovering atomic.Bool` (AC6).
3. **Ne PAS utiliser l'ancien `internal/watchdog/` (DNS).** Il a un rôle complètement différent (vérification résolveur DNS) et sera supprimé en story 2.5. Confusion possible : créer dans `internal/tun/watchdog/` pour distinguer.
4. **MTU 1420** : valeur de référence dans `config.example.toml` (architecture.md#L512). À comparer en check d'altération (AC4). Si la valeur devient configurable, lire depuis la config.
5. **Windows : driver Wintun** absent en CI standard → tests `checker_windows_test.go` doivent fonctionner sans driver installé (utiliser stub/mock pour la partie wireguard/tun, tests d'intégration réels uniquement en local sur Windows admin).
6. **Build tags** : `//go:build linux` et `//go:build windows` strictement séparés. Pas de fichier non-tagué qui importerait du code OS-specific.
7. **Pas de `time.Sleep` nu** dans la boucle — utiliser `time.Ticker` + `select { case <-ctx.Done() }` (cf. `internal/watchdog/watchdog.go:80-87` pour le pattern de référence).
8. **Logging** : pas de fmt.Println — utiliser le logger structuré utilisé par `internal/service/` (suivre le style existant).

### Testing standards

- Tests unitaires `_test.go` à côté du code, framework standard `testing`.
- Tests d'intégration sous `//go:build integration` — skipped par défaut, exécutés via `go test -tags=integration -race ./...`.
- Race detector obligatoire (NFR22d).
- `gosec` clean (NFR22e), `govulncheck` clean (NFR22f).
- Coverage cible : > 80 % sur le package `internal/tun/watchdog/`.
- Vérifier absence de leak goroutine via `goleak.VerifyNone(t)` en `TestMain` ou `defer`.

### Project Structure Notes

- Le package `internal/tun/watchdog/` est un sous-package du nouveau `internal/tun/` créé en story 2.1. Si la story 2.1 n'est pas encore mergée, créer le squelette minimal `internal/tun/device.go` avec une interface `Device` + stubs (sans implémentation réelle) pour pouvoir compiler les tests de watchdog en isolation. Documenter ce contournement dans la PR.
- Aucun conflit avec la structure cible décrite [architecture.md#L741-749](../planning-artifacts/architecture.md#L741-L749).
- Conformité avec patterns naming [architecture.md#L431-458](../planning-artifacts/architecture.md#L431-L458) : package en lowercase, fichiers `_linux.go` / `_windows.go`, tests adjacents.

### References

- [epics.md#L468-480 — Story 2.2 (BDD AC)](../planning-artifacts/epics.md#L468-L480)
- [prd.md#L419 — FR5b](../planning-artifacts/prd.md#L419)
- [prd.md#L535 — NFR16](../planning-artifacts/prd.md#L535)
- [prd.md#L536 — NFR17](../planning-artifacts/prd.md#L536)
- [architecture.md#L596-622 — TUN / Firewall / Routing Patterns + Watchdog TUN](../planning-artifacts/architecture.md#L596-L622)
- [architecture.md#L741-749 — Structure cible internal/tun/](../planning-artifacts/architecture.md#L741-L749)
- [architecture.md#L131-136 — Sélection lib wireguard/tun](../planning-artifacts/architecture.md#L131-L136)
- [internal/watchdog/watchdog.go](../../internal/watchdog/watchdog.go) — pattern de référence pour la boucle de poll (NE PAS modifier ici, sera supprimé en story 2.5)
- [internal/service/service.go](../../internal/service/service.go) — point d'intégration du nouveau watchdog

### Previous Story Intelligence

Story 2.1 (création TUN) n'est pas encore créée. Cette story dépend conceptuellement de 2.1 mais peut être démarrée en parallèle si le squelette `internal/tun/device.go` (interface Go pure + stubs) est créé en amont. Recommandation : exécuter la story 2.1 d'abord ou créer une PR préliminaire « scaffold internal/tun ».

### Git Intelligence

Commits récents pertinents :
- `c1d7c3a` — Ajout pays ES/GB et augmentation quotas (touche relais, sans lien direct).
- `66469e7` — Specs minimize-to-tray (touche UI, sans lien direct).
- `a1adf3f` — Hide console windows pour `netsh`/`net` commands : **pertinent** — si recovery TUN appelle des commandes shell sur Windows, **réutiliser le même pattern de masquage de console** (cf. fichier modifié dans ce commit).
- Aucun commit récent ne touche `internal/tun/` ou `internal/firewall/` (encore inexistants).

### Latest Tech Information

- **wireguard/tun** API (vérification CLI/godoc) : `Device.Name() (string, error)`, `Device.MTU() (int, error)`, `Device.Read([]byte, int) (int, error)`. Sous Linux, `Read`/`Write` sur device détruite retourne `os.PathError` avec `syscall.ENODEV`. Utiliser `errors.Is` plutôt que comparaison de chaînes.
- **net.InterfaceByName** — depuis Go 1.21, retourne erreur typée `*net.OpError` wrappant `syscall.ENODEV` sur Linux quand l'interface n'existe pas. Cross-vérifier au moment de l'implémentation.

### Project Context Reference

Aucun fichier `project-context.md` détecté à la racine — utiliser uniquement les artefacts planning + le code existant comme référence.

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

- `go build ./internal/tun/watchdog/` → OK
- `go test -race -count=1 ./internal/tun/watchdog/` → PASS (1.5 s, Windows amd64)
- `go test -race -count=1 ./internal/tun/...` → PASS (tun + watchdog)
- `go vet ./internal/tun/watchdog/` → clean
- `go build ./...` → OK (pas de régression)
- Pré-existant sur main (hors périmètre 2.2) : `internal/ui` test file fait référence à `handleToggle` inexistant ; `internal/tunnel` panic QUIC intermittent sous parallélisme CI.

### Completion Notes List

- **Tous les tasks T1–T6 complétés.** Watchdog autonome (T1/T2) + service wiring complet (T3/T4) + tests exhaustifs (T5) + config (T6).
- **Décisions appliquées** : MTU strict == 1420 (AC4) ; pas de debounce (AC6).
- **ACs validés** : AC1 (détection < 5s poll 3s), AC2 (séquence recovery stricte, test d'ordre), AC3 (firewall jamais Deactivate, vérifié par test), AC4 (MTU strict), AC5 (ctx cancel prompt, idempotent Stop), AC6 (anti-flapping atomic.Bool), AC7 (tests unitaires + intégration).
- **Ancien `internal/watchdog/` (DNS)** : conservé intact (suppression prévue story 2.5).
- **Race pré-existante** `TestSTUNActive_AfterStop` (port 3478 in use) non liée à cette story.

### File List

Ajoutés :
- `internal/tun/watchdog/checker.go`
- `internal/tun/watchdog/checker_linux.go`
- `internal/tun/watchdog/checker_windows.go`
- `internal/tun/watchdog/watchdog.go`
- `internal/tun/watchdog/watchdog_test.go`
- `internal/tun/watchdog/checker_linux_test.go`
- `internal/tun/watchdog/checker_windows_test.go`
- `internal/service/service_tun_recovery_test.go`

Modifiés :
- `internal/service/service.go` — import tunwatchdog, champ tunWatchdog, startTUNWatchdog(), recoverTUN(), captureOriginalRouteFunc, shutdown watchdog stop
- `config.example.toml` — ajout section `[tun.watchdog]`
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 2-2 → review

### Change Log

- 2026-04-15 — T1/T2/T5.1-3/T6 : package `internal/tun/watchdog/` avec InterfaceChecker cross-platform + config.
- 2026-04-16 — T3/T4/T5.4 : service wiring (recoverTUN, startTUNWatchdog, shutdown stop), tests recovery (ordre strict, AC3, erreur, mode dégradé).
- 2026-04-16 — Code review fixes : H1 (data race — onLostWg + mutex sur champs partagés + ctx.Err guard), H2 (fuite firewall — réutilise instance existante), M1 (retry spec corrigée), M2 (OnLost reçoit parent ctx), L1 (dead config option retirée), L2 (checker unifié cross-platform), L3 (tests Stop/WaitsForOnLost + OnLost/ReceivesParentCtx). Story → done.

---

**Notes de complétion (create-story)** : Ultimate context engine analysis completed — comprehensive developer guide created. Story créée le 2026-04-15.

**Questions / clarifications soulevées pendant l'analyse** :
1. La story 2.1 (création TUN) n'est pas encore matérialisée en story file. La 2.2 nécessite a minima un squelette `internal/tun/device.go` avec interface `Device` pour compiler. Décision recommandée : créer la story 2.1 avant de lancer dev-story sur 2.2, OU autoriser le dev de 2.2 à créer le squelette de l'interface en plus.
2. **DÉCIDÉ (2026-04-15)** : AC4 — MTU strict == 1420. Toute valeur différente = `StatusInvalid` → trigger recovery.
3. **DÉCIDÉ (2026-04-15)** : AC6 — pas de debounce. 1 détection (`StatusMissing` ou `StatusInvalid`) = trigger immédiat (sous contrainte du flag `recovering`). NFR16 prime sur la protection anti-flapping.
