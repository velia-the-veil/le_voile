# Story 3.1: Service systeme avec kardianos/service et module IPC

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que Le Voile fonctionne comme un service systeme qui maintient la protection meme si l'interface tray est fermee,
Afin que ma protection soit permanente et survive aux fermetures d'interface ou aux redemarrages.

## Acceptance Criteria

1. **Enregistrement service OS natif** — Le binaire client compile avec kardianos/service s'enregistre aupres du gestionnaire de services natif (Windows SCM, systemd, launchd) et demarre automatiquement au boot par defaut.

2. **Redemarrage automatique apres crash** — Quand le service crash, le gestionnaire de services le redemarre automatiquement en moins de 10 secondes.

3. **Independance service/tray** — Quand l'utilisateur ferme le tray UI, le service continue de fonctionner et le tunnel + protection DNS restent actifs.

4. **Serveur IPC operationnel** — Le module IPC serveur accepte les connexions via named pipe (`\\.\pipe\levoile` Windows) ou unix socket (`/tmp/levoile.sock` Linux/macOS) et attend des messages JSON.

5. **Commande get_status** — Quand un client IPC envoie `{"action":"get_status"}`, le service repond avec `{"status":"connected","ip":"82.x.x.x","uptime":"3h12m"}` ou l'etat courant. Les champs JSON utilisent le format `snake_case`.

6. **Commandes connect/disconnect** — Quand un client IPC envoie `{"action":"connect"}` ou `{"action":"disconnect"}`, le service execute l'action demandee et repond avec le nouveau statut.

### Scenarios BDD detailles

**AC1 — Enregistrement service:**
```
Given le binaire client compile avec kardianos/service
When le service est installe sur l'OS
Then il s'enregistre aupres du gestionnaire de services natif (Windows SCM, systemd, launchd)
And il demarre automatiquement au boot par defaut
```

**AC2 — Redemarrage crash:**
```
Given le service en cours d'execution
When le service crash
Then le gestionnaire de services le redemarre automatiquement en moins de 10 secondes
```

**AC3 — Independance tray:**
```
Given le service en cours d'execution
When l'utilisateur ferme le tray UI
Then le service continue de fonctionner
And le tunnel et la protection DNS restent actifs
```

**AC4 — Serveur IPC:**
```
Given le module IPC serveur initialise
When un client IPC se connecte via named pipe (\\.\pipe\levoile Windows) ou unix socket (/tmp/levoile.sock Linux/macOS)
Then le serveur accepte la connexion et attend des messages JSON
```

**AC5 — get_status:**
```
Given un client IPC connecte
When il envoie {"action":"get_status"}
Then le service repond avec {"status":"connected","ip":"82.x.x.x","uptime":"3h12m"} ou l'etat courant
And les champs JSON utilisent le format snake_case
```

**AC6 — connect/disconnect:**
```
Given un client IPC connecte
When il envoie {"action":"connect"} ou {"action":"disconnect"}
Then le service execute l'action demandee et repond avec le nouveau statut
```

## Tasks / Subtasks

- [x] **Task 1 : Ajouter kardianos/service et refactorer le lifecycle** (AC: #1, #2, #3)
  - [x] 1.1 `go get github.com/kardianos/service` — ajouter la dependance
  - [x] 1.2 Creer `internal/service/service.go` — type `Program` implementant `service.Interface` (Start/Stop)
  - [x] 1.3 Migrer la logique d'orchestration de `cmd/client/main.go` vers `Program.Start()` et `Program.Stop()`
  - [x] 1.4 `Program.Start()` — lance une goroutine qui execute la sequence : tunnel connect → proxy start → DNS set → watchdog start → reconnector start
  - [x] 1.5 `Program.Stop()` — execute l'arret propre inverse : reconnector stop → watchdog stop → kill switch deactivate → DNS restore → watchdog verify → proxy stop → state close → tunnel disconnect
  - [x] 1.6 Modifier `cmd/client/main.go` — configurer `service.Config{Name: "LeVoile", DisplayName: "Le Voile", Description: "VPN minimaliste zero-log"}`, gerer les sous-commandes (install, uninstall, start, stop, run)
  - [x] 1.7 Creer `internal/service/service_test.go` — tests du lifecycle Start/Stop avec mocks

- [x] **Task 2 : Module IPC — Types de messages JSON** (AC: #5, #6)
  - [x] 2.1 Creer `internal/ipc/messages.go` — definir les types `Request` (Action string) et `Response` (Status, IP, Uptime, Error string)
  - [x] 2.2 Actions supportees : `get_status`, `connect`, `disconnect`
  - [x] 2.3 Statuts possibles : `connected`, `connecting`, `disconnected`, `error`
  - [x] 2.4 Serialisation/deserialisation JSON avec `encoding/json` — champs `snake_case` via struct tags

- [x] **Task 3 : Module IPC — Transport platform-specific** (AC: #4)
  - [x] 3.1 Creer `internal/ipc/pipe_windows.go` — listener sur named pipe `\\.\pipe\levoile` via go-winio
  - [x] 3.2 Creer `internal/ipc/pipe_unix.go` — listener sur unix socket `/tmp/levoile.sock` via `net.Listen("unix", "/tmp/levoile.sock")`
  - [x] 3.3 Definir interface `Listener` avec `Listen() (net.Listener, error)` et `Cleanup() error` pour abstraction cross-platform

- [x] **Task 4 : Module IPC — Serveur** (AC: #4, #5, #6)
  - [x] 4.1 Creer `internal/ipc/server.go` — type `Server` avec `Start(ctx)`, `Stop()`, `SetHandler(func(Request) Response)`
  - [x] 4.2 Accepter les connexions, lire les messages JSON ligne par ligne (un JSON par ligne), decoder et dispatcher vers le handler
  - [x] 4.3 Le handler recoit l'action et retourne la reponse — la logique metier reste dans le service, pas dans l'IPC
  - [x] 4.4 Gerer les deconnexions proprement, cleanup du socket/pipe a l'arret
  - [x] 4.5 Creer `internal/ipc/server_test.go` — tests avec client local (get_status, connect, disconnect, connexion invalide)

- [x] **Task 5 : Module IPC — Client** (AC: #5, #6)
  - [x] 5.1 Creer `internal/ipc/client.go` — type `Client` avec `Connect()`, `Close()`, `Send(Request) (Response, error)`
  - [x] 5.2 Connexion au named pipe (Windows) ou unix socket (Linux/macOS)
  - [x] 5.3 Envoi/reception JSON, timeout de 5 secondes par requete
  - [x] 5.4 Creer `internal/ipc/client_test.go` — tests d'integration client-serveur

- [x] **Task 6 : Integration IPC dans le service** (AC: #5, #6)
  - [x] 6.1 Dans `Program.Start()` — demarrer le serveur IPC apres l'initialisation du tunnel
  - [x] 6.2 Implementer le handler IPC dans le service : `get_status` retourne l'etat courant du StateManager + IP + uptime, `connect` appelle tunnel.Connect, `disconnect` appelle le shutdown propre
  - [x] 6.3 Dans `Program.Stop()` — arreter le serveur IPC avant le reste du shutdown

- [x] **Task 7 : Validation globale** (AC: tous)
  - [x] 7.1 Executer tous les tests (`go test ./...`) — service, IPC, + non-regression
  - [x] 7.2 Verifier compilation multi-plateforme (`GOOS=windows/linux/darwin`)
  - [x] 7.3 Executer `go vet ./...` — zero warning
  - [x] 7.4 Tester `le_voile install`, `le_voile start`, `le_voile stop`, `le_voile uninstall` en mode interactif
  - [x] 7.5 Verifier non-regression des 91 tests existants

## Dev Notes

### Contraintes architecturales OBLIGATOIRES

- **Pure Go, pas de CGo** — Module path: `github.com/velia-the-veil/le_voile`
- **Jamais de logging client** — Les erreurs sont propagees via IPC au tray. Pour l'instant `fmt.Fprintf(os.Stderr, ...)` dans main.go uniquement
- **Jamais de `panic`** — Toujours retourner `error`
- **`context.Context` obligatoire** — Premier parametre de toutes les fonctions bloquantes ou reseau
- **Wrapping d'erreurs** — Format : `fmt.Errorf("service: %w", err)`, `fmt.Errorf("ipc: server: %w", err)`, `fmt.Errorf("ipc: client: %w", err)`
- **Build tags pour OS** — `//go:build windows`, `//go:build linux || darwin`
- **Tests co-localises** — `service_test.go` a cote de `service.go`

### Conventions de nommage (OBLIGATOIRES)

- Packages : lowercase, un mot (`service`, `ipc`)
- Fichiers : `snake_case.go` (`service.go`, `messages.go`, `pipe_windows.go`, `pipe_unix.go`)
- Fonctions exportees : `PascalCase` (`NewServer`, `NewClient`, `NewProgram`)
- Fonctions privees : `camelCase` (`handleConnection`, `readMessage`, `writeResponse`)
- Constructeurs : `New` + type (`NewServer()`, `NewClient()`, `NewProgram()`)
- Tests : `TestType_Method` (`TestServer_HandleGetStatus`, `TestClient_Connect`)
- Constantes exportees : `PascalCase` (`PipeName`, `SocketPath`)

### Patterns de concurrence

- `context.Context` passe en premier argument partout (timeout, annulation)
- Channels pour communication inter-goroutines
- `sync.Mutex` ou `sync.RWMutex` pour etat partage simple
- Jamais de goroutines orphelines — toujours via context ou `sync.WaitGroup`
- Pattern standard : `go func() { defer wg.Done(); ... }()`

### kardianos/service — Pattern d'implementation

```go
// internal/service/service.go
type Program struct {
    // Tous les composants du lifecycle
    tunnelClient *tunnel.Client
    dnsManager   dns.DNSManager
    proxy        *dns.Proxy
    killSwitch   *dns.KillSwitch
    reconnector  *tunnel.Reconnector
    watchdog     *watchdog.Watchdog
    ipcServer    *ipc.Server
    startTime    time.Time
    // Contexte et annulation
    ctx    context.Context
    cancel context.CancelFunc
    done   chan struct{}
}

func (p *Program) Start(s service.Service) error {
    // Lance la goroutine principale — NE BLOQUE PAS
    go p.run()
    return nil
}

func (p *Program) Stop(s service.Service) error {
    // Arret propre — BLOQUE jusqu'a completion
    p.cancel()
    <-p.done
    return nil
}
```

**CRITIQUE :** `Start()` NE DOIT PAS bloquer — lancer une goroutine pour le travail principal. `Stop()` DOIT bloquer jusqu'a l'arret complet.

### cmd/client/main.go — Pattern avec sous-commandes

```go
func main() {
    svcConfig := &service.Config{
        Name:        "LeVoile",
        DisplayName: "Le Voile",
        Description: "VPN minimaliste zero-log",
    }

    prg := NewProgram(/* config params */)
    s, err := service.New(prg, svcConfig)
    // Gerer os.Args pour install/uninstall/start/stop/run
    // Par defaut : s.Run() (mode service ou interactif selon contexte)
}
```

### IPC — Format des messages JSON

**Requete (client → service) :**
```json
{"action":"get_status"}
{"action":"connect"}
{"action":"disconnect"}
```

**Reponse (service → client) :**
```json
{"status":"connected","ip":"82.x.x.x","uptime":"3h12m"}
{"status":"disconnected","error":"tunnel_timeout"}
{"status":"connecting"}
```

**Protocole :** Un message JSON par ligne (newline-delimited JSON). Chaque message termine par `\n`. Utiliser `json.NewEncoder`/`json.NewDecoder` avec le stream.

### IPC — Transport cross-platform

**Windows — Named Pipe :**
- Chemin : `\\.\pipe\levoile`
- Option 1 : `github.com/Microsoft/go-winio` — `winio.ListenPipe(pipePath, nil)` retourne un `net.Listener`
- Option 2 : Si on veut eviter une dependance externe, utiliser `net.Listen("unix", ...)` avec un chemin dans le dossier AppData (Windows 10+ supporte AF_UNIX)
- **Decision recommandee :** Utiliser `go-winio` pour les named pipes Windows — c'est le standard (utilise par Docker, containerd). La dependance est legere et pure Go.

**Linux/macOS — Unix Socket :**
- Chemin : `/tmp/levoile.sock`
- `net.Listen("unix", "/tmp/levoile.sock")` — standard Go, aucune dependance
- Cleanup : `os.Remove(socketPath)` avant Listen (supprime le socket orphelin)
- Permissions : le socket herite des permissions du processus

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
- Types : `ConnState` = `string`, valeurs : `StateConnected`, `StateConnecting`, `StateDisconnected`

**dns.DNSManager** (`internal/dns/manager.go`) :
- `SetResolver(ctx context.Context, addr string) error` — modifie DNS systeme
- `RestoreResolver(ctx context.Context) error` — restaure DNS original
- `OriginalResolver() string` — retourne le resolver original sauvegarde

**dns.Proxy** (`internal/dns/proxy.go`) :
- Ecoute UDP sur `127.0.0.1:53`
- `QueryFunc` callback vers `Client.SendDoHQuery`
- `Ready()` channel de readiness
- Max 100 goroutines concurrentes, buffer 4096

**dns.KillSwitch** (`internal/dns/kill_switch.go`) :
- `Activate(ctx)` — pointe resolver vers 127.0.0.1, arrete proxy
- `Deactivate(ctx)` — restaure resolver vers DoH local, redemarre proxy
- `IsActive()` — etat thread-safe

**tunnel.Reconnector** (`internal/tunnel/reconnect.go`) :
- `Start(ctx)` — ecoute Updates(), reconnecte sur disconnected, active/desactive kill switch
- `Stop()` — arrete les tentatives

**watchdog.Watchdog** (`internal/watchdog/watchdog.go`) :
- `Start(ctx)` — verifie resolver toutes les 3 secondes
- `Stop()` — arrete la surveillance
- `VerifyAndRestore(ctx)` — verification finale pour arret propre

### Dependances de cette story

- **Story 2.1** (done) : `tunnel.Client`, `tunnel.StateManager`
- **Story 2.2** (done) : `dns.DNSManager`, `dns.Proxy`
- **Story 2.3** (done) : `dns.KillSwitch`, `tunnel.Reconnector`, `watchdog.Watchdog`
- **Nouvelles dependances externes :**
  - `github.com/kardianos/service` — gestion service OS multi-plateforme
  - `github.com/Microsoft/go-winio` — named pipes Windows (optionnel si AF_UNIX Windows suffisant)

### NFRs a respecter

| NFR | Exigence | Impact |
|-----|----------|--------|
| NFR15 | Service redemarrage < 10 secondes apres crash | kardianos/service config |
| NFR11 | RAM < 20MB | IPC serveur leger, pas de buffer excessif |
| NFR12 | CPU < 1% | IPC idle = 0 CPU, listener bloquant |
| NFR5 | Zero fuite DNS | Le service maintient la protection meme sans tray |
| NFR6 | Resolver restaure dans tous les scenarios | Stop() doit TOUJOURS restaurer |

### Ce qui est HORS SCOPE (ne PAS implementer)

- Interface tray / icones d'etat (Story 3.2)
- Menu clic droit et controles utilisateur (Story 3.3)
- Module config TOML complet (sera cree quand necessaire, utiliser les flags en attendant)
- Installateur Windows / version portable (Epic 4)
- Metriques/telemetrie
- Recuperation de l'IP visible externe (le `get_status` retourne "unknown" pour l'IP au MVP — l'appel HTTP externe pour detecter l'IP sera ajoute ulterieurement)

### Intelligence des stories precedentes

**Lecons de la Story 2.3 (Kill Switch/Watchdog/Reconnexion) :**
- L'orchestration lifecycle est dans `cmd/client/main.go` — la refactorer vers `internal/service/service.go`
- Ordre d'initialisation : tunnel → proxy → DNS → watchdog → reconnector (NE PAS CHANGER L'ORDRE)
- Ordre d'arret (inverse) : reconnector → watchdog → kill switch → DNS restore → watchdog verify → proxy → state → tunnel
- Le watchdog fait une `VerifyAndRestore(ctx)` finale avant l'arret — CONSERVER ce comportement
- Gestion signaux SIGINT/SIGTERM deja implementee dans main.go — kardianos/service gere ca nativement, SUPPRIMER la gestion manuelle des signaux
- Le proxy DNS a un retry avec delai 100ms si le port n'est pas encore libere — CONSERVER ce mecanisme

**Lecons de la Story 2.2 (DNS) :**
- `commandRunner` function type pour mocker l'execution de commandes OS — reutiliser ce pattern si besoin
- Le proxy DNS a un channel `Ready()` pour la synchronisation — l'utiliser dans le service lifecycle
- Rollback sur echec partiel implemente sur Windows — le service doit aussi gerer les echecs partiels

**Lecons de la Story 2.1 (Tunnel) :**
- `StateManager.Updates()` retourne un channel buffered taille 1 — consommer rapidement
- `Connect()` a un timeout de 3 secondes
- Apres reconnexion, un nouveau transport HTTP/3 est cree implicitement par `Connect()`

### Project Structure Notes

**Fichiers a CREER :**
```
internal/service/service.go          # Type Program implementant service.Interface
internal/service/service_test.go     # Tests lifecycle
internal/ipc/messages.go             # Types Request, Response, constantes actions/statuts
internal/ipc/server.go               # Serveur IPC, accept connexions, dispatch handler
internal/ipc/server_test.go          # Tests serveur
internal/ipc/client.go               # Client IPC, connexion, envoi/reception
internal/ipc/client_test.go          # Tests client
internal/ipc/pipe_windows.go         # //go:build windows — named pipe listener
internal/ipc/pipe_unix.go            # //go:build linux || darwin — unix socket listener
```

**Fichiers a MODIFIER :**
```
cmd/client/main.go                   # Refactorer : deleguer lifecycle au Program, ajouter sous-commandes service
go.mod / go.sum                      # Ajouter kardianos/service (+ go-winio si named pipe)
```

**Fichiers a SUPPRIMER :**
```
internal/service/doc.go              # Remplace par service.go
internal/ipc/doc.go                  # Remplace par messages.go
```

**Fichiers a NE PAS TOUCHER :**
```
internal/tunnel/*                    # APIs stables
internal/dns/*                       # APIs stables
internal/watchdog/*                  # API stable
internal/crypto/*                    # Module crypto stable
internal/relay/*                     # Cote serveur — pas concerne
internal/tray/*                      # Story 3.2
internal/config/*                    # Pas encore implemente
cmd/relay/*                          # Cote serveur — pas concerne
```

**Alignement structure projet :**
- `internal/service/service.go` — conforme a l'architecture (`internal/service/service.go` prevu)
- `internal/ipc/server.go`, `client.go`, `messages.go` — conformes a l'architecture
- `internal/ipc/pipe_windows.go`, `pipe_unix.go` — conformes aux build tags documentes
- Aucun conflit ou variance detecte

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Architecture Service & Integration OS] — kardianos/service, DNSManager, service lifecycle
- [Source: _bmad-output/planning-artifacts/architecture.md#Communication & Protocole] — IPC named pipe/unix socket, messages JSON
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns] — naming, error handling, concurrency
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — fichiers service.go, ipc/*.go
- [Source: _bmad-output/planning-artifacts/prd.md#FR14] — Service demarrage automatique
- [Source: _bmad-output/planning-artifacts/prd.md#FR15] — Tray auto-connecte au service
- [Source: _bmad-output/planning-artifacts/prd.md#FR16] — Service independant du tray
- [Source: _bmad-output/planning-artifacts/prd.md#NFR15] — Redemarrage < 10 secondes
- [Source: _bmad-output/planning-artifacts/epics.md#Epic3-Story3.1] — Criteres d'acceptation
- [Source: _bmad-output/implementation-artifacts/2-3-kill-switch-dns-watchdog-et-reconnexion-automatique.md] — Lifecycle, KillSwitch, Reconnector, Watchdog APIs

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Server IPC test initial hang: `bufio.Scanner` on TCP connections blocked on shutdown because active connections were not closed. Fixed by tracking active connections and closing them all when context is cancelled.

### Completion Notes List

- Migrated full lifecycle orchestration from `cmd/client/main.go` into `internal/service/service.go` (`Program.run()` and `Program.shutdown()`).
- `Program.Start()` does NOT block (launches goroutine), `Program.Stop()` BLOCKS until complete — as required by kardianos/service.
- Shutdown order preserved exactly: reconnector → watchdog → kill switch → DNS restore → watchdog verify → proxy → state → tunnel.
- IPC server uses newline-delimited JSON protocol over named pipe (Windows) or unix socket (Linux/macOS).
- IPC handler in `cmd/client/main.go` bridges `get_status`, `connect`, `disconnect` actions to service components.
- `get_status` returns IP as "unknown" (external IP detection out of scope for MVP).
- Added `go-winio` for Windows named pipes and `kardianos/service` for OS service management.
- 19 new tests added (5 service + 7 server + 7 client), 105 total tests pass (91 pre-existing preserved).

### Code Review Fixes (AI)

- **[H1]** Added `service.Config.Option` with `OnFailure: restart`, `OnFailureDelayDuration: 5s`, `OnFailureResetPeriod: 10` for Windows SCM crash recovery (AC2).
- **[H2]** IPC `disconnect` now stops the reconnector before disconnecting, preventing automatic reconnection after user-initiated disconnect.
- **[H3]** Fixed `go.mod`: moved `kardianos/service` and `go-winio` from indirect to direct require block.
- **[M1]** Fixed `TestProgram_SetIPCServer`: replaced dead `_ = called` with actual callback invocation assertions. Added `TestProgram_Reconnector_NilBeforeStart` and `TestProgram_Context_NilBeforeStart`.
- **[M2]** IPC `connect` now stops the reconnector before connecting and restarts it after, preventing race conditions.
- **[M3]** IPC server Start() now launched in a goroutine in `Program.run()`. Simplified `Cleanup()` in pipe_unix.go and pipe_windows.go to avoid double-close of listener.
- **[M4]** Added `scanner.Buffer(4096, 4096)` limit on IPC server to prevent resource exhaustion from oversized messages.

### File List

**New files:**
- `internal/service/service.go` — Program type implementing service.Interface (lifecycle)
- `internal/service/service_test.go` — Lifecycle tests
- `internal/ipc/messages.go` — Request/Response types, Listener interface, action/status constants
- `internal/ipc/server.go` — IPC server with connection tracking and graceful shutdown
- `internal/ipc/server_test.go` — Server tests (7 tests)
- `internal/ipc/client.go` — IPC client with timeout support
- `internal/ipc/client_test.go` — Client integration tests (7 tests)
- `internal/ipc/pipe_windows.go` — Windows named pipe transport via go-winio
- `internal/ipc/pipe_unix.go` — Unix socket transport

**Modified files:**
- `cmd/client/main.go` — Refactored to use kardianos/service, added IPC handler and sub-commands
- `go.mod` — Added kardianos/service v1.2.4, Microsoft/go-winio v0.6.2
- `go.sum` — Updated checksums

**Deleted files:**
- `internal/service/doc.go` — Replaced by service.go
- `internal/ipc/doc.go` — Replaced by messages.go

## Change Log

- 2026-03-09: Story 3.1 implementation complete — Service lifecycle via kardianos/service, IPC module (server/client/transport), integration dans cmd/client/main.go. 105 tests passent, compilation cross-platform OK, go vet OK.
- 2026-03-09: Code review — 3 HIGH, 4 MEDIUM, 1 LOW trouvés. Corrigés : crash recovery config (H1), disconnect/reconnector coordination (H2), go.mod indirect fix (H3), tests améliorés (M1), connect/reconnector race (M2), IPC goroutine + cleanup (M3), scanner buffer limit (M4).
