# Story 2.2 : Watchdog TUN — détection d'altération externe

Status: ready-for-dev

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

- [ ] **T1. Créer le package `internal/tun/watchdog/`** (AC1, AC4, AC5, AC6)
  - [ ] T1.1 — Définir `type Watchdog struct` (interval 3 s, dépendances `tun.Device` checker + callback `OnLost func(ctx) error`).
  - [ ] T1.2 — Constructeur `NewWatchdog(checker InterfaceChecker, onLost func(ctx context.Context) error, opts ...Option)`.
  - [ ] T1.3 — Méthodes `Start(ctx) error` (bloquante, retourne quand ctx cancel), `Stop()` (idempotente), `Wait()` (sync).
  - [ ] T1.4 — Boucle interne avec `time.Ticker(3*time.Second)`, sentinelle `ErrWatchdogAlreadyRunning` calquée sur le pattern existant `internal/watchdog/watchdog.go`.
  - [ ] T1.5 — Anti-flapping : `atomic.Bool recovering` ; si vrai, skip déclenchement OnLost.

- [ ] **T2. Implémenter `InterfaceChecker` cross-platform** (AC1, AC4)
  - [ ] T2.1 — Interface Go `InterfaceChecker interface { Check(ctx) (Status, error) }` avec `Status` enum (`StatusOK`, `StatusMissing`, `StatusInvalid`).
  - [ ] T2.2 — `checker_linux.go` : utiliser `net.InterfaceByName("levoile0")` + vérification flags `FlagUp` + MTU == 1420. Si `errors.Is(err, syscall.ENODEV)` → `StatusMissing`.
  - [ ] T2.3 — `checker_windows.go` : interroger via `golang.zx2c4.com/wireguard/tun` (méthode `Device.Name()` ou `Device.MTU()` qui erreure si adapter détruit) ; fallback `net.InterfaceByName`.
  - [ ] T2.4 — Build tags appropriés (`//go:build linux` / `//go:build windows`).

- [ ] **T3. Câbler le callback `OnLost` dans `internal/service/`** (AC2, AC3)
  - [ ] T3.1 — Méthode `(*Program).recoverTUN(ctx)` qui exécute la séquence stricte : `tun.New(levoile0, 1420)` → `routing.Setup(...)` → `firewall.Activate(relayIP, tunName)` → `tunnel.Connect()`.
  - [ ] T3.2 — Avant la séquence : **NE PAS** appeler `firewall.Deactivate()` — le firewall doit rester actif pendant toute la procédure (AC3, NFR15).
  - [ ] T3.3 — Logging structuré à chaque étape (`INFO tun.recovery: step=X duration=Yms`).
  - [ ] T3.4 — En cas d'échec d'une étape : log `ERROR`, retry exponentiel borné (3 tentatives max), maintien firewall actif.

- [ ] **T4. Intégrer le watchdog au lifecycle de service** (AC5)
  - [ ] T4.1 — Démarrage : après `tunnel.Connect()` réussi initial, lancer `go watchdog.Start(ctx)`.
  - [ ] T4.2 — Shutdown : appeler `watchdog.Stop()` AVANT `tunnel.Disconnect()` pour éviter de retrigger un reconnect pendant l'arrêt.
  - [ ] T4.3 — Retirer la dépendance à l'ancien `internal/watchdog/` (DNS resolver) — il sera supprimé en story 2.5 quand le proxy DNS local sera retiré. Ne PAS le supprimer dans cette story.

- [ ] **T5. Tests** (AC7)
  - [ ] T5.1 — `watchdog_test.go` : checker mocké retournant `StatusMissing` après N cycles → vérifier appel `OnLost` exactement 1 fois (anti-flapping).
  - [ ] T5.2 — `watchdog_test.go` : ctx cancel → `Start()` retourne en < 3 s, `goleak.VerifyNone` OK.
  - [ ] T5.3 — `checker_linux_test.go` (build tag `integration`) : créer une vraie interface dummy via `netlink`, vérifier `StatusOK`, supprimer, vérifier `StatusMissing`.
  - [ ] T5.4 — `service/recovery_test.go` : mocker tun/firewall/routing/tunnel, vérifier l'ordre d'invocation strict de `recoverTUN`.

- [ ] **T6. Documentation inline et configuration**
  - [ ] T6.1 — Ajouter section `[watchdog]` dans `config.example.toml` : `tun_poll_interval = "3s"`.
  - [ ] T6.2 — Godoc complet sur les exports (suit la convention du code existant `internal/watchdog/watchdog.go`).

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

(à compléter par l'agent dev)

### Debug Log References

### Completion Notes List

### File List

---

**Notes de complétion (create-story)** : Ultimate context engine analysis completed — comprehensive developer guide created. Story créée le 2026-04-15.

**Questions / clarifications soulevées pendant l'analyse** :
1. La story 2.1 (création TUN) n'est pas encore matérialisée en story file. La 2.2 nécessite a minima un squelette `internal/tun/device.go` avec interface `Device` pour compiler. Décision recommandée : créer la story 2.1 avant de lancer dev-story sur 2.2, OU autoriser le dev de 2.2 à créer le squelette de l'interface en plus.
2. La valeur MTU (1420) doit-elle être strictement validée par le watchdog (AC4), ou tolérer une plage ? Recommandation : strict == 1420 pour MVP, paramétrable plus tard si besoin.
3. Anti-flapping : faut-il un seuil de N détections consécutives avant déclenchement (debounce) ? Recommandation MVP : non (1 détection = trigger, NFR16 demande < 5 s). À réviser si flapping observé en prod.
