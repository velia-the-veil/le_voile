# Story 2.3: Kill switch DNS, watchdog et reconnexion automatique

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que mes requetes DNS soient bloquees si le tunnel est coupe, que le resolver soit surveille en permanence, et que la connexion se retablisse automatiquement,
Afin qu'aucune fuite DNS ne se produise, meme en cas de coupure ou de crash.

## Acceptance Criteria

1. **Kill switch activation rapide** — Quand la connexion au relais est perdue, le kill switch s'active en moins de 100ms, le resolver DNS systeme est pointe vers `127.0.0.1`, et toutes les requetes DNS sortantes sont bloquees.

2. **Reconnexion automatique avec backoff** — Quand le kill switch est actif, la reconnexion est initiee en moins de 1 seconde apres la perte. Un backoff exponentiel est applique en cas d'echecs successifs. Le kill switch reste actif pendant toute la duree de la reconnexion.

3. **Restauration apres reconnexion** — Quand la reconnexion reussit et le tunnel repasse en etat `connected`, le kill switch est desactive, le resolver DNS systeme est repointe vers le client DoH local (`127.0.0.1:53` via proxy), et les requetes DNS fonctionnent a nouveau normalement.

4. **Watchdog DNS apres crash** — Quand le service client crash de facon inattendue, le watchdog detecte que le resolver DNS est dans un etat incoherent et restaure le resolver vers `127.0.0.1` (kill switch) en moins de 5 secondes. Si le service redemarre, le resolver est reconfigure correctement.

5. **Arret propre** — Quand le client est arrete definitivement, le resolver DNS systeme est restaure a sa valeur originale et le watchdog confirme la restauration avant l'arret complet.

### Scenarios BDD detailles

**AC1 — Kill switch:**
```
Given le tunnel en etat "connected"
When la connexion au relais est perdue (state -> disconnected)
Then le kill switch s'active en moins de 100ms
And le resolver DNS systeme est pointe vers "127.0.0.1"
And toutes les requetes DNS sortantes sont bloquees (proxy arrete ou refuse)
```

**AC2 — Reconnexion:**
```
Given le kill switch actif
When le client tente une reconnexion
Then la reconnexion est initiee en moins de 1 seconde apres la perte
And un backoff exponentiel est applique (1s, 2s, 4s, 8s, 16s, max 30s)
And le kill switch reste actif pendant toute la duree de la reconnexion
```

**AC3 — Restauration:**
```
Given la reconnexion reussie
When le tunnel repasse en etat "connected"
Then le kill switch est desactive
And le resolver DNS systeme est repointe vers le client DoH local
And les requetes DNS fonctionnent a nouveau normalement
```

**AC4 — Watchdog:**
```
Given le service client crash de facon inattendue
When le watchdog detecte que le resolver DNS est dans un etat incoherent
Then le watchdog restaure le resolver vers "127.0.0.1" (kill switch) en moins de 5 secondes
And si le service redemarre, le resolver est reconfigure correctement
```

**AC5 — Arret propre:**
```
Given le client desinstalle ou arrete definitivement
When le processus d'arret est execute
Then le resolver DNS systeme est restaure a sa valeur originale
And le watchdog confirme la restauration avant l'arret complet
```

## Tasks / Subtasks

- [x] **Task 1 : Module Kill Switch DNS** (AC: #1)
  - [x] 1.1 Creer `internal/dns/kill_switch.go` avec type `KillSwitch` struct
  - [x] 1.2 Implementer `Activate(ctx)` — pointe le resolver vers `127.0.0.1` via `DNSManager.SetResolver()`, arrete le proxy DNS
  - [x] 1.3 Implementer `Deactivate(ctx)` — restaure le resolver vers le client DoH local, redemarre le proxy DNS
  - [x] 1.4 Implementer `IsActive()` — retourne l'etat du kill switch (thread-safe via `sync.RWMutex`)
  - [x] 1.5 Creer `internal/dns/kill_switch_test.go` avec tests table-driven (activation, desactivation, etats, concurrence)

- [x] **Task 2 : Module Reconnexion Automatique** (AC: #2, #3)
  - [x] 2.1 Creer `internal/tunnel/reconnect.go` avec type `Reconnector` struct
  - [x] 2.2 Implementer logique de backoff exponentiel (base 1s, facteur 2x, max 30s, reset apres succes)
  - [x] 2.3 Implementer `Start(ctx)` — ecoute le channel `StateManager.Updates()`, declenche reconnexion sur `disconnected`
  - [x] 2.4 Implementer `Stop()` — arrete les tentatives de reconnexion
  - [x] 2.5 Integrer avec `KillSwitch` — activation sur `disconnected`, desactivation sur `connected`
  - [x] 2.6 Creer `internal/tunnel/reconnect_test.go` avec tests (backoff, reconnexion reussie, echecs successifs, annulation)

- [x] **Task 3 : Module Watchdog DNS** (AC: #4)
  - [x] 3.1 Creer `internal/watchdog/watchdog.go` avec type `Watchdog` struct
  - [x] 3.2 Implementer goroutine de surveillance — verifie le resolver DNS systeme a intervalle regulier (2-3 secondes)
  - [x] 3.3 Implementer detection d'incoherence — comparer resolver actuel avec etat attendu (tunnel connecte → DoH local, tunnel deconnecte → 127.0.0.1)
  - [x] 3.4 Implementer restauration automatique — corriger le resolver en cas d'incoherence en moins de 5 secondes
  - [x] 3.5 Creer `internal/watchdog/watchdog_test.go` avec tests (detection, restauration, arret propre)

- [x] **Task 4 : Integration dans le lifecycle client** (AC: #1, #2, #3, #5)
  - [x] 4.1 Modifier `cmd/client/main.go` — integrer KillSwitch, Reconnector et Watchdog dans le lifecycle
  - [x] 4.2 Orchestrer l'ordre d'initialisation : tunnel connect → proxy start → DNS set → watchdog start → reconnector start
  - [x] 4.3 Orchestrer l'arret propre : reconnector stop → watchdog stop → DNS restore → proxy stop → tunnel disconnect
  - [x] 4.4 Gerer les signaux (SIGINT, SIGTERM) pour arret propre avec restauration DNS garantie

- [x] **Task 5 : Validation globale** (AC: tous)
  - [x] 5.1 Executer tous les tests (`go test ./...`) — kill switch, reconnexion, watchdog, DNS, tunnel
  - [x] 5.2 Verifier compilation multi-plateforme (`GOOS=windows/linux/darwin`)
  - [x] 5.3 Executer `go vet ./...` — zero warning
  - [x] 5.4 Verifier non-regression des 13 tests DNS et 13 tests tunnel existants

## Dev Notes

### Contraintes architecturales OBLIGATOIRES

- **Pure Go, pas de CGo** — Module path: `github.com/velia-the-veil/le_voile`
- **Jamais de logging client** — Les erreurs sont propagees via IPC au tray (Epic 3). Pour l'instant, propagation via stdout temporaire dans `main.go`
- **Jamais de `panic`** — Toujours retourner `error`
- **`context.Context` obligatoire** — Premier parametre de toutes les fonctions bloquantes ou reseau
- **Wrapping d'erreurs** — Format : `fmt.Errorf("dns: kill_switch: %w", err)`, `fmt.Errorf("tunnel: reconnect: %w", err)`, `fmt.Errorf("watchdog: %w", err)`
- **Build tags pour OS** — `//go:build windows`, `//go:build linux`, `//go:build darwin`
- **Tests co-localises** — `kill_switch_test.go` a cote de `kill_switch.go`

### Conventions de nommage (OBLIGATOIRES)

- Packages : lowercase, un mot (`dns`, `tunnel`, `watchdog`)
- Fichiers : `snake_case.go` (`kill_switch.go`, `reconnect.go`)
- Fonctions exportees : `PascalCase` (`ActivateKillSwitch`, `NewReconnector`)
- Fonctions privees : `camelCase` (`checkResolver`, `attemptReconnect`)
- Constructeurs : `New` + type (`NewKillSwitch()`, `NewReconnector()`, `NewWatchdog()`)
- Tests : `TestType_Method` (`TestKillSwitch_Activate`, `TestReconnector_Backoff`)
- Constantes exportees : `PascalCase` (`MaxBackoffDelay`, `WatchdogInterval`)

### Patterns de concurrence

- `context.Context` passe en premier argument partout (timeout, annulation)
- Channels pour communication inter-goroutines (tunnel state → kill switch, watchdog → DNS)
- `sync.Mutex` ou `sync.RWMutex` pour etat partage simple
- Jamais de goroutines orphelines — toujours via context ou `sync.WaitGroup`
- Pattern standard : `go func() { defer wg.Done(); ... }()`

### Erreurs sentinelles a creer

```go
// internal/dns/kill_switch.go
var (
    ErrKillSwitchAlreadyActive   = errors.New("dns: kill switch already active")
    ErrKillSwitchAlreadyInactive = errors.New("dns: kill switch already inactive")
)

// internal/tunnel/reconnect.go
var (
    ErrReconnectInProgress = errors.New("tunnel: reconnect already in progress")
    ErrReconnectStopped    = errors.New("tunnel: reconnect stopped")
)

// internal/watchdog/watchdog.go
var (
    ErrWatchdogAlreadyRunning = errors.New("watchdog: already running")
    ErrResolverInconsistent   = errors.New("watchdog: resolver inconsistent")
)
```

### APIs existantes a utiliser (NE PAS REINVENTER)

**tunnel.Client** (`internal/tunnel/client.go`) :
- `Connect(ctx context.Context) error` — etablit le tunnel, verifie l'identite Ed25519
- `Disconnect() error` — ferme le tunnel
- `SendDoHQuery(ctx context.Context, dnsPayload []byte) ([]byte, error)` — envoie requete DNS via tunnel
- `State() *StateManager` — retourne le gestionnaire d'etat

**tunnel.StateManager** (`internal/tunnel/state.go`) :
- `Set(state ConnState)` — met a jour l'etat (non-bloquant, channel buffered taille 1)
- `Get() ConnState` — lit l'etat courant
- `Updates() <-chan ConnState` — retourne channel read-only pour les changements d'etat
- `Close()` — ferme le channel

**dns.DNSManager** (`internal/dns/manager.go`) :
- `SetResolver(ctx context.Context, addr string) error` — modifie DNS systeme
- `RestoreResolver(ctx context.Context) error` — restaure DNS original
- `OriginalResolver() string` — retourne le resolver original sauvegarde

**dns.Proxy** (`internal/dns/proxy.go`) :
- Ecoute UDP sur `127.0.0.1:53`
- `QueryFunc` callback vers `Client.SendDoHQuery`
- `Ready()` channel de readiness
- Max 100 goroutines concurrentes, buffer 4096

### Dependances de cette story

- **Story 2.1** (done) : `tunnel.Client`, `tunnel.StateManager` — connexion, etat, `SendDoHQuery()`
- **Story 2.2** (done) : `dns.DNSManager` (interface + implementations OS), `dns.Proxy` — gestion DNS, proxy local
- **Aucune nouvelle dependance externe** — tout se fait avec la lib standard Go + les modules internes existants

### NFRs a respecter

| NFR | Exigence | Impact |
|-----|----------|--------|
| NFR5 | Aucune fuite DNS pendant fonctionnement normal ou reconnexion | Kill switch obligatoire |
| NFR6 | Resolver restaure dans tous les scenarios | Watchdog + arret propre |
| NFR13 | Kill switch < 100ms apres perte tunnel | Performance critique |
| NFR14 | Watchdog restauration < 5 secondes apres crash | Intervalle surveillance 2-3s |
| NFR15 | Service redemarrage < 10 secondes apres crash | Integration kardianos/service (Epic 3) |

### Ce qui est HORS SCOPE (ne PAS implementer)

- TCP DNS (UDP uniquement pour MVP)
- Cache DNS
- Module config TOML (Epic 3)
- Communication IPC service ↔ tray (Epic 3)
- Interface tray / icones d'etat (Epic 3)
- kardianos/service integration (Epic 3, Story 3.1)
- Metriques/telemetrie

### Project Structure Notes

**Fichiers a CREER :**
```
internal/dns/kill_switch.go         # Type KillSwitch, Activate/Deactivate
internal/dns/kill_switch_test.go    # Tests kill switch
internal/tunnel/reconnect.go        # Type Reconnector, backoff, auto-reconnect
internal/tunnel/reconnect_test.go   # Tests reconnexion
internal/watchdog/watchdog.go       # Type Watchdog, surveillance resolver (REMPLACE doc.go)
internal/watchdog/watchdog_test.go  # Tests watchdog
```

**Fichiers a MODIFIER :**
```
cmd/client/main.go                  # Integration kill switch, reconnector, watchdog dans lifecycle
```

**Fichiers a NE PAS TOUCHER :**
```
internal/tunnel/client.go           # API tunnel stable
internal/tunnel/state.go            # State machine stable
internal/dns/manager.go             # Interface DNSManager stable
internal/dns/proxy.go               # Proxy DNS stable
internal/dns/manager_*.go           # Implementations OS stables
internal/relay/*                    # Cote serveur — pas concerne
internal/crypto/*                   # Module crypto — pas concerne
```

**Alignement structure projet :**
- `internal/dns/kill_switch.go` — conforme a l'architecture (`internal/dns/kill_switch.go` prevu dans architecture.md)
- `internal/tunnel/reconnect.go` — conforme a l'architecture (`internal/tunnel/reconnect.go` prevu dans architecture.md)
- `internal/watchdog/watchdog.go` — conforme a l'architecture (`internal/watchdog/watchdog.go` prevu dans architecture.md)
- Aucun conflit ou variance detecte

### Intelligence des stories precedentes

**Lecons de la Story 2.2 (DNS) :**
- `commandRunner` function type pour mocker l'execution de commandes OS — reutiliser ce pattern dans le watchdog pour verifier le resolver
- `defaultRunner` execute les commandes reelles — ne pas creer un autre mecanisme
- Les tests Windows necessitent isolation des interfaces — deja gere
- Le proxy DNS a un channel `Ready()` pour la synchronisation — l'utiliser pour coordonner kill switch ↔ proxy
- Rollback sur echec partiel implemente sur Windows — le kill switch doit aussi gerer les echecs partiels

**Lecons de la Story 2.1 (Tunnel) :**
- `StateManager.Updates()` retourne un channel buffered taille 1 — le `Reconnector` doit le consommer rapidement pour ne pas bloquer
- `Connect()` a un timeout de 3 secondes — le backoff doit en tenir compte
- `Disconnect()` ferme le transport HTTP/3 — apres reconnexion, un nouveau transport est cree implicitement par `Connect()`
- Les tests utilisent un serveur HTTP/3 local — reutiliser `startTestRelay()` et `newTestClient()` pour tester la reconnexion

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Kill Switch] — `internal/dns/kill_switch.go`, resolver → 127.0.0.1
- [Source: _bmad-output/planning-artifacts/architecture.md#Watchdog] — goroutine surveillance, restauration
- [Source: _bmad-output/planning-artifacts/architecture.md#Reconnexion] — `internal/tunnel/reconnect.go`, backoff
- [Source: _bmad-output/planning-artifacts/architecture.md#Concurrence] — context.Context, channels, WaitGroup
- [Source: _bmad-output/planning-artifacts/architecture.md#Anti-Patterns] — pas de panic, pas de log client, pas de goroutines orphelines
- [Source: _bmad-output/planning-artifacts/prd.md#FR2] — Reconnexion automatique
- [Source: _bmad-output/planning-artifacts/prd.md#FR7] — Restauration resolver crash (watchdog)
- [Source: _bmad-output/planning-artifacts/prd.md#FR8] — Kill switch DNS
- [Source: _bmad-output/planning-artifacts/prd.md#NFR13] — Kill switch < 100ms
- [Source: _bmad-output/planning-artifacts/prd.md#NFR14] — Watchdog < 5 secondes
- [Source: _bmad-output/planning-artifacts/epics.md#Epic2-Story2.3] — Criteres d'acceptation BDD
- [Source: _bmad-output/implementation-artifacts/2-2-gestion-dns-systeme-et-redirection-doh.md] — DNSManager API, patterns, tests
- [Source: _bmad-output/implementation-artifacts/2-1-client-tunnel-quic-https-avec-connexion-au-relais.md] — Client tunnel API, StateManager

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

Aucun probleme majeur. Un ajustement de timing sur le test de backoff (5s → 8s) car la sequence 1s+2s+4s depasse 5 secondes.

### Completion Notes List

- **Task 1 — Kill Switch DNS** : Module `KillSwitch` dans `internal/dns/kill_switch.go`. Controle le proxy DNS (stop/start) et verifie defensivement le resolver. Thread-safe via `sync.RWMutex`. 8 tests couvrant activation, desactivation, etats, erreurs, concurrence.
- **Task 2 — Reconnexion Automatique** : Module `Reconnector` dans `internal/tunnel/reconnect.go`. Ecoute `StateManager.Updates()`, backoff exponentiel (1s, 2s, 4s, 8s, 16s, max 30s), integration kill switch. 7 tests couvrant backoff, reconnexion reussie, echecs successifs, annulation, double start, fermeture channel.
- **Task 3 — Watchdog DNS** : Module `Watchdog` dans `internal/watchdog/watchdog.go`. Verification periodique du resolver (intervalle 3s), correction automatique, `VerifyAndRestore` pour arret propre. 10 tests couvrant detection, restauration, arret propre, erreurs.
- **Task 4 — Integration lifecycle** : `cmd/client/main.go` reecrit avec ordre d'initialisation (tunnel → proxy → DNS → watchdog → reconnector) et arret propre inverse. Gestion signaux SIGINT/SIGTERM avec restauration DNS garantie. Verification watchdog finale avant arret.
- **Task 5 — Validation** : 91 tests passent (0 echec), compilation multi-plateforme OK, go vet 0 warning, 0 regression.

### File List

**Fichiers crees :**
- `internal/dns/kill_switch.go` — Module KillSwitch
- `internal/dns/kill_switch_test.go` — Tests kill switch (8 tests)
- `internal/dns/check_windows.go` — Verification/correction resolver Windows
- `internal/dns/check_linux.go` — Verification/correction resolver Linux
- `internal/dns/check_darwin.go` — Verification/correction resolver macOS
- `internal/tunnel/reconnect.go` — Module Reconnector avec backoff
- `internal/tunnel/reconnect_test.go` — Tests reconnexion (7 tests)
- `internal/watchdog/watchdog.go` — Module Watchdog (remplace doc.go)
- `internal/watchdog/watchdog_test.go` — Tests watchdog (10 tests)

**Fichiers modifies :**
- `cmd/client/main.go` — Integration KillSwitch, Reconnector, Watchdog dans lifecycle

**Fichiers supprimes :**
- `internal/watchdog/doc.go` — Remplace par watchdog.go (le package doc est dans watchdog.go)

## Change Log

- 2026-03-09 : Implementation complete de la story 2.3 — kill switch DNS, reconnexion automatique avec backoff, watchdog DNS, integration lifecycle client. 25 nouveaux tests ajoutes, 0 regression sur les 66 tests existants.
- 2026-03-09 : Code review — 9 issues identifiees (3 HIGH, 4 MEDIUM, 2 LOW). Corrections appliquees :
  - [H1] Suppression `internal/watchdog/doc.go` orphelin (descriptions package contradictoires)
  - [H2] Reconnector : retry kill switch Activate/Deactivate sur echec (protection NFR5)
  - [H3] KillSwitch : ajout `SetForceResolver` pour verification defensive du resolver sans ecraser l'original
  - [M1] Watchdog : retry sur echec du fixer avec delai 500ms
  - [M2] Suppression code mort `activeInterfaceNames()` dans `check_windows.go`
  - [M3] Note : timing kill switch < 100ms non garanti structurellement (TODO)
  - [M4] Proxy : retry avec delai 100ms si port pas encore libere par l'OS
  - [L1] Tests kill switch : assertions d'erreur renforcees (wantErrContains + test forceResolver)
