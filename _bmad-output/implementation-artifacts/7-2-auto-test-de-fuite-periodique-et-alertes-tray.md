# Story 7.2 : Auto-test de fuite périodique et alertes tray

Status: done

## Story

En tant qu'utilisateur,
Je veux que Le Voile vérifie automatiquement l'absence de fuites DNS et IP toutes les 10 minutes et m'alerte via le tray si un problème est détecté,
Afin d'avoir une confiance continue que ma vie privée est protégée sans intervention manuelle.

## Acceptance Criteria

**AC1 – Test périodique automatique toutes les 10 minutes**
**Given** le service est actif et le tunnel est connecté
**When** 10 minutes se sont écoulées depuis le dernier test réussi
**Then** le client exécute automatiquement un test de fuite : il réutilise `leakcheck.WebRTCLeakChecker.RunFullCheck()` et compare l'IP STUN vue avec l'IP publique du relais

**AC2 – Aucune notification si le test passe**
**Given** l'auto-test de fuite est exécuté
**When** l'IP source vue par le serveur STUN correspond à l'IP du relais
**Then** le test passe silencieusement (aucun tooltip, aucun log, aucune notification)

**AC3 – Alerte tray immédiate si fuite détectée**
**Given** l'auto-test de fuite est exécuté
**When** une fuite est détectée (IP réelle visible ou IP STUN ≠ IP relais)
**Then** une alerte tray s'affiche immédiatement via le tooltip : "⚠ Fuite détectée — vérification en cours"

**AC4 – Tentative de rétablissement automatique**
**Given** une fuite est détectée
**When** l'alerte est émise
**Then** le service tente de rétablir la protection via `Reconnector.TriggerReconnect()` et relance le test après 30 secondes

**AC5 – Suspension du test quand kill switch actif**
**Given** le kill switch est actif (tunnel coupé, `KillSwitch.IsActive() == true`)
**When** l'auto-test est planifié
**Then** le test est suspendu jusqu'à ce que `KillSwitch.IsActive() == false` (pas de trafic de test hors tunnel)

**AC6 – Suspension du test quand tunnel déconnecté**
**Given** le tunnel est dans l'état `StateDisconnected` ou `StateConnecting`
**When** l'auto-test est planifié
**Then** le test est ignoré (le scheduler ne lance pas de test hors tunnel actif)

**AC7 – Exposition de l'état via IPC `get_status`**
**Given** le tray interroge le service via `get_status`
**When** le dernier test de fuite a produit un résultat
**Then** la réponse IPC inclut `"leak_status": "pass"|"fail"|"pending"` et `"leak_last_check": "<timestamp RFC3339>"`

## Tasks / Subtasks

- [x] **Task 1 : Créer `internal/leakcheck/scheduler.go`** (AC: 1, 2, 3, 4, 5, 6)
  - [x] 1.1 Définir le type `PeriodicScheduler` avec ses champs : `interval time.Duration`, `checker leakCheckerIface`, `killSwitch KillSwitchQuerier`, `tunnelState TunnelStateQuerier`, `onLeak func(report *FullLeakReport)`, `onRecovery func()`, `mu sync.Mutex`, `running bool`, `cancel context.CancelFunc`, `done chan struct{}`, `lastResult *FullLeakReport`, `lastCheckAt time.Time`, `lastWasLeak bool`
  - [x] 1.2 Définir les interfaces locales `KillSwitchQuerier { IsActive() bool }` et `TunnelStateQuerier { Get() tunnel.ConnState }` pour découplage
  - [x] 1.3 Implémenter `NewPeriodicScheduler(interval time.Duration, checker *WebRTCLeakChecker, ks KillSwitchQuerier, ts TunnelStateQuerier, onLeak func(*FullLeakReport), onRecovery func()) *PeriodicScheduler`
  - [x] 1.4 Implémenter `Start(ctx context.Context) error` — goroutine principale avec ticker, retourne `ErrSchedulerAlreadyRunning` si déjà lancé ; pattern identique à `watchdog.go`
  - [x] 1.5 Implémenter `Stop()` — cancel + drain du channel `done`
  - [x] 1.6 Dans la goroutine principale : `select { case <-ticker.C: p.runCheck(ctx) case <-ctx.Done(): return }`
  - [x] 1.7 Implémenter `runCheck(ctx context.Context)` : skip si kill switch actif ou tunnel ≠ StateConnected ; sinon RunFullCheck avec timeout 25s
  - [x] 1.8 Si `RunFullCheck` retourne `Status == "fail"` → appeler `onLeak(report)` ; si `"pass"` + `lastWasLeak == true` → appeler `onRecovery()`
  - [x] 1.9 Stocker `lastResult`, `lastCheckAt` (protégés par mutex)
  - [x] 1.10 Exposer `LastResult() (*FullLeakReport, time.Time)` pour lecture thread-safe par l'IPC handler

- [x] **Task 2 : Modifier `internal/ipc/messages.go`** (AC: 7)
  - [x] 2.1 Ajouter constante `StatusLeakPending = "pending"` dans la section status constants
  - [x] 2.2 Ajouter champs dans `Response` : `LeakStatus string` et `LeakLastCheck string` (format RFC3339)
  - [x] 2.3 Actions existantes `ActionLeakCheck`, `StatusLeakPass`, `StatusLeakFail` inchangées

- [x] **Task 3 : Modifier `internal/ipchandler/handler.go`** (AC: 7)
  - [x] 3.1 Modifier `handleGetStatus(prg)` pour inclure les champs leak via `prg.LeakScheduler()`
  - [x] 3.2 Si `LeakScheduler() == nil` ou `LastResult() == nil` → `LeakStatus = "pending"`, `LeakLastCheck = ""`
  - [x] 3.3 Si résultat disponible : `LeakStatus = report.Status`, `LeakLastCheck = checkAt.Format(time.RFC3339)`

- [x] **Task 4 : Modifier `internal/service/service.go`** (AC: 1, 4, 5, 6)
  - [x] 4.1 Ajouter champ `leakScheduler *leakcheck.PeriodicScheduler` dans `Program`
  - [x] 4.2 Dans `run()`, après `reconnector.Start` : instancier `PeriodicScheduler` avec `interval = 10 * time.Minute`
  - [x] 4.3 Callback `onLeak` : appel `tunnelClient.Disconnect()` (déclenche reconnexion via Reconnector) + goroutine avec `time.After(30s)` + `TriggerCheck`
  - [x] 4.4 Callback `onRecovery` : no-op
  - [x] 4.5 `go leakScheduler.Start(ctx)` dans `run()`
  - [x] 4.6 Dans shutdown : `leakScheduler.Stop()` avant `reconnector.Stop()`
  - [x] 4.7 Accesseur public `LeakScheduler() *leakcheck.PeriodicScheduler`

- [x] **Task 5 : Modifier `internal/tray/tray.go`** (AC: 3)
  - [x] 5.1 Dans `connectAndPoll` : lire `resp.LeakStatus` après `updateTrayState`
  - [x] 5.2 Si `LeakStatus == "fail"` ET `!leakAlertActive` → `SetTooltip("⚠ Fuite détectée…")` + `restoreTooltipAfter(30s)`
  - [x] 5.3 Si `LeakStatus == "pass"` ET `leakAlertActive` → reset flag (tooltip géré par restoreTooltipAfter)
  - [x] 5.4 Flag `leakAlertActive bool` dans `Tray`

- [x] **Task 6 : Ajouter tests unitaires dans `internal/leakcheck/scheduler_test.go`** (AC: 1, 2, 3, 5, 6)
  - [x] 6.1 `TestPeriodicScheduler_PassSilent`
  - [x] 6.2 `TestPeriodicScheduler_FailCallsOnLeak`
  - [x] 6.3 `TestPeriodicScheduler_SkipsWhenKillSwitchActive`
  - [x] 6.4 `TestPeriodicScheduler_SkipsWhenTunnelDisconnected`
  - [x] 6.5 `TestPeriodicScheduler_RecoveryCallback`
  - [x] 6.6 `TestPeriodicScheduler_StartStop`
  - [x] 6.7 `TestPeriodicScheduler_StartAlreadyRunning`

- [x] **Task 7 : Validation end-to-end**
  - [x] 7.1 `go test ./internal/leakcheck/...` — OK (scheduler + checker existants)
  - [x] 7.2 `go test ./internal/ipc/...` — OK
  - [x] 7.3 `go test ./internal/ipchandler/...` — OK (+ correction bug pré-existant rollback/nil-tunnel)
  - [x] 7.4 `go build ./cmd/client/... ./cmd/tray/...` — OK
  - [x] 7.5 `go test ./internal/service/... ./internal/tray/... ./internal/watchdog/... ./internal/stun/...` — OK (dns/tunnel ont des échecs pré-existants non liés)

## Dev Notes

### Contexte et décision architecturale

**Réutilisation du code existant :** Le package `internal/leakcheck/` contient déjà `WebRTCLeakChecker` qui fait exactement ce qu'il faut — il envoie des requêtes STUN via le tunnel et compare l'IP vue avec l'IP du relais. La Story 7.2 ajoute uniquement un `PeriodicScheduler` qui enveloppe ce checker existant.

**Notification push vs. poll :** Le tray poll déjà le service via `get_status` toutes les 2 secondes. Il suffit d'ajouter les champs `leak_status` et `leak_last_check` dans la réponse. Pas besoin d'un mécanisme push supplémentaire — le délai de notification maximum est 2 secondes, acceptable pour un test de 10 minutes.

**Où vit le scheduler :** Dans `internal/service/service.go` (processus service), PAS dans le tray. Le service tourne en arrière-plan même sans tray actif. Le tray affiche seulement les alertes qu'il détecte au polling.

**Interface vs. type concret :** Le `PeriodicScheduler` dépend d'interfaces (`KillSwitchQuerier`, `TunnelStateQuerier`) et non des types concrets `*dns.KillSwitch` et `*tunnel.StateManager` pour éviter les imports circulaires et faciliter les tests.

### Fichiers à créer

| Fichier | Action |
|---------|--------|
| `internal/leakcheck/scheduler.go` | **Créer** — type `PeriodicScheduler`, `Start`, `Stop`, `runCheck`, `LastResult` |
| `internal/leakcheck/scheduler_test.go` | **Créer** — 7 tests unitaires |

### Fichiers à modifier

| Fichier | Modification |
|---------|-------------|
| `internal/ipc/messages.go` | Ajouter `StatusLeakPending`, champs `LeakStatus` et `LeakLastCheck` dans `Response` |
| `internal/ipchandler/handler.go` | Modifier `handleGetStatus` pour inclure les champs leak |
| `internal/service/service.go` | Ajouter champ `leakScheduler`, démarrage/arrêt dans `run()`, accesseur `LeakScheduler()` |
| `internal/tray/tray.go` | Lire `resp.LeakStatus` dans le polling, afficher alerte si `"fail"` |

### Fichiers à ne PAS toucher

- `internal/leakcheck/checker.go` — `WebRTCLeakChecker` reste tel quel, pas de modification
- `internal/leakcheck/report.go` — `FullLeakReport` reste tel quel (statuts `"pass"`/`"fail"` déjà corrects)
- `internal/dns/proxy.go` — non concerné
- `internal/tunnel/client.go` — non concerné
- `internal/tunnel/reconnect.go` — non concerné (mais vérifier l'API `TriggerReconnect` si elle existe)
- `internal/watchdog/watchdog.go` — non concerné, mais s'en inspirer pour le pattern
- `internal/config/config.go` — aucun nouveau champ TOML (intervalle hardcodé à 10 min, acceptable)
- `internal/crypto/` — non concerné
- `internal/stun/` — non concerné

### Implémentation détaillée

#### `internal/leakcheck/scheduler.go`

```go
package leakcheck

import (
    "context"
    "errors"
    "sync"
    "time"

    "github.com/xxx/levoile/internal/tunnel"
)

var ErrSchedulerAlreadyRunning = errors.New("leakcheck: scheduler already running")

// KillSwitchQuerier abstracts dns.KillSwitch for test isolation.
type KillSwitchQuerier interface {
    IsActive() bool
}

// TunnelStateQuerier abstracts tunnel.StateManager for test isolation.
type TunnelStateQuerier interface {
    Get() tunnel.ConnState
}

type PeriodicScheduler struct {
    interval    time.Duration
    checker     *WebRTCLeakChecker
    killSwitch  KillSwitchQuerier
    tunnelState TunnelStateQuerier
    onLeak      func(report *FullLeakReport)
    onRecovery  func()

    mu           sync.Mutex
    running      bool
    cancel       context.CancelFunc
    done         chan struct{}
    lastResult   *FullLeakReport
    lastCheckAt  time.Time
    lastWasLeak  bool
}

func NewPeriodicScheduler(
    interval time.Duration,
    checker *WebRTCLeakChecker,
    ks KillSwitchQuerier,
    ts TunnelStateQuerier,
    onLeak func(*FullLeakReport),
    onRecovery func(),
) *PeriodicScheduler {
    return &PeriodicScheduler{
        interval:    interval,
        checker:     checker,
        killSwitch:  ks,
        tunnelState: ts,
        onLeak:      onLeak,
        onRecovery:  onRecovery,
    }
}

func (p *PeriodicScheduler) Start(ctx context.Context) error {
    p.mu.Lock()
    if p.running {
        p.mu.Unlock()
        return ErrSchedulerAlreadyRunning
    }
    ctx, p.cancel = context.WithCancel(ctx)
    p.done = make(chan struct{})
    p.running = true
    p.mu.Unlock()

    go func() {
        defer func() {
            p.mu.Lock()
            p.running = false
            p.cancel = nil
            close(p.done)
            p.mu.Unlock()
        }()
        ticker := time.NewTicker(p.interval)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                p.runCheck(ctx)
            }
        }
    }()
    return nil
}

func (p *PeriodicScheduler) Stop() {
    p.mu.Lock()
    cancel := p.cancel
    done := p.done
    p.mu.Unlock()
    if cancel != nil {
        cancel()
    }
    if done != nil {
        <-done
    }
}

func (p *PeriodicScheduler) runCheck(ctx context.Context) {
    // Skip si kill switch actif ou tunnel non connecté
    if p.killSwitch.IsActive() {
        return
    }
    if p.tunnelState.Get() != tunnel.StateConnected {
        return
    }

    checkCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
    defer cancel()

    report, err := p.checker.RunFullCheck(checkCtx)
    if err != nil {
        return // Erreur réseau transiente — skip silencieux
    }

    p.mu.Lock()
    p.lastResult = report
    p.lastCheckAt = time.Now()
    wasLeak := p.lastWasLeak
    p.lastWasLeak = report.Status == StatusLeakFail
    p.mu.Unlock()

    if report.Status == StatusLeakFail {
        if p.onLeak != nil {
            p.onLeak(report)
        }
    } else if wasLeak && p.onRecovery != nil {
        p.onRecovery()
    }
}

// LastResult retourne le dernier résultat et le moment du test (thread-safe).
// Retourne nil, zero si aucun test n'a encore été exécuté.
func (p *PeriodicScheduler) LastResult() (*FullLeakReport, time.Time) {
    p.mu.Lock()
    defer p.mu.Unlock()
    return p.lastResult, p.lastCheckAt
}
```

#### Modification de `internal/ipc/messages.go`

```go
// Ajouter dans la section STATUS CONSTANTS:
const StatusLeakPending = "pending"

// Dans le type Response, ajouter après les champs existants:
type Response struct {
    // ... champs existants ...
    LeakStatus    string `json:"leak_status,omitempty"`
    LeakLastCheck string `json:"leak_last_check,omitempty"`
}
```

#### Modification de `internal/ipchandler/handler.go`

```go
// Dans handleGetStatus, après construction de la Response de base:
func handleGetStatus(prg *svc.Program) ipc.Response {
    // ... code existant ...
    resp := ipc.Response{ /* ... */ }

    // Ajouter état leak test
    if scheduler := prg.LeakScheduler(); scheduler != nil {
        result, checkAt := scheduler.LastResult()
        if result == nil {
            resp.LeakStatus = ipc.StatusLeakPending
        } else {
            resp.LeakStatus = result.Status // "pass" ou "fail"
            resp.LeakLastCheck = checkAt.Format(time.RFC3339)
        }
    }
    return resp
}
```

#### Modification de `internal/tray/tray.go`

```go
// Dans le type Tray, ajouter:
type Tray struct {
    // ... champs existants ...
    leakAlertActive bool // true si alerte fuite déjà affichée
}

// Dans la goroutine de polling (qui lit get_status toutes les 2s):
func (t *Tray) applyStatus(ctx context.Context, resp ipc.Response) {
    // ... code existant ...

    // Gestion alerte de fuite
    if resp.LeakStatus == ipc.StatusLeakFail && !t.leakAlertActive {
        t.leakAlertActive = true
        t.api.SetTooltip("⚠ Fuite détectée — vérification en cours")
        go t.restoreTooltipAfter(ctx, 30*time.Second)
    } else if resp.LeakStatus == ipc.StatusLeakPass && t.leakAlertActive {
        t.leakAlertActive = false
        // restoreTooltipAfter gère déjà la restauration du tooltip normal
    }
}
```

#### Modification de `internal/service/service.go`

```go
// Dans Program, ajouter:
type Program struct {
    // ... champs existants ...
    leakScheduler *leakcheck.PeriodicScheduler
}

// Dans run(), après reconnector.Start:
leakScheduler := leakcheck.NewPeriodicScheduler(
    10 * time.Minute,
    leakcheck.NewWebRTCLeakChecker(func(ctx context.Context) (string, error) {
        return p.tunnelClient.PublicIP(ctx)
    }),
    p.killSwitch,  // implémente KillSwitchQuerier (IsActive() bool)
    p.tunnelClient.State(), // *tunnel.StateManager implémente TunnelStateQuerier
    func(report *leakcheck.FullLeakReport) {
        // Tenter reconnexion
        if p.reconnector != nil {
            p.reconnector.TriggerReconnect() // À vérifier l'API exacte
        }
    },
    func() { /* onRecovery - no-op */ },
)
p.leakScheduler = leakScheduler
go leakScheduler.Start(ctx)

// Dans shutdown (avant reconnector.Stop):
if p.leakScheduler != nil {
    p.leakScheduler.Stop()
}

// Accesseur public:
func (p *Program) LeakScheduler() *leakcheck.PeriodicScheduler {
    return p.leakScheduler
}
```

### Points d'attention critiques

**1. API `TriggerReconnect` du Reconnector :** Vérifier si `tunnel.Reconnector` expose une méthode pour forcer une reconnexion manuelle. Si non, implémenter ou adapter — regarder dans `internal/tunnel/reconnect.go`. Peut-être utiliser `p.reconnector.ForceReconnect()` ou un channel dédié.

**2. API `PublicIP` du TunnelClient :** Vérifier que `tunnel.Client` expose bien `PublicIP(ctx context.Context) (string, error)` ou une fonction équivalente. Le `WebRTCLeakChecker` a besoin d'une `PublicIPFunc` pour connaître l'IP attendue du tunnel.

**3. Implémentation de `TunnelStateQuerier` par `*tunnel.StateManager` :** Vérifier que `StateManager.Get()` est bien accessible publiquement via `client.State()`. L'interface `TunnelStateQuerier { Get() tunnel.ConnState }` doit être satisfaite.

**4. Pas de goroutines orphelines dans `runCheck` :** Le timeout 25s est géré via `context.WithTimeout`, pas via goroutines séparées. Si le contexte parent est annulé (service shutdown), `RunFullCheck` retourne immédiatement.

**5. Comportement au premier démarrage :** Le premier test se lance après le premier tick (10 minutes). `LastResult()` retourne `nil` avant le premier test → `LeakStatus = "pending"` dans l'IPC. Le tray ne montre aucune alerte pour `"pending"`.

### Patterns architecturaux à respecter

**Pattern goroutine lifecycle (copié de watchdog.go) :**
```
Start → Lock → check running → set cancel/done/running → Unlock → goroutine avec defer cleanup
Stop → Lock → read cancel/done → Unlock → cancel() → <-done
```

**Nommage :**
- Fichier : `internal/leakcheck/scheduler.go` (snake_case, dans le package existant)
- Type : `PeriodicScheduler` (PascalCase)
- Constructor : `NewPeriodicScheduler` (New + TypeName)
- Error : `ErrSchedulerAlreadyRunning` (Err + description)

**Erreurs :**
- Préfixe : `leakcheck: ` pour les erreurs du package
- Erreur sentinelle : `ErrSchedulerAlreadyRunning`
- Erreurs réseau dans `runCheck` : skip silencieux (aucun log, aucun retour)

**Concurrence :**
- `sync.Mutex` pour protéger `running`, `lastResult`, `lastCheckAt`, `lastWasLeak`
- Pas de `sync.RWMutex` (les lectures de `LastResult()` sont rares vs les écritures)
- `context.Context` transmis jusqu'à `RunFullCheck`
- Aucune goroutine orpheline dans `runCheck`

**No logging :**
- Aucun `log.Printf` dans scheduler.go (les erreurs transientes sont silencieuses)
- L'alerte tray via `onLeak` est le seul canal de notification utilisateur

**Tests :**
- Go standard `testing` uniquement
- Mocks via interfaces locales (`KillSwitchQuerier`, `TunnelStateQuerier`)
- Ticker instrumenté via `interval` court (ex: 1ms) pour tests rapides
- Table-driven tests si pertinent

### Contexte de la story précédente (7.1)

La story 7.1 a implémenté :
- `internal/crypto/pinning.go` → `VerifyEd25519CertPin`
- `internal/relay/doh_handler.go` → fallback DNS multi-upstream avec goroutine recovery
- `internal/tunnel/client.go` → `VerifyPeerCertificate` + `ErrPinningFailed`

Ces fichiers **ne doivent pas être touchés** dans cette story. Vérifier uniquement que la compilation reste propre après les modifications de `internal/ipc/`, `internal/ipchandler/`, `internal/service/`.

**Leçons de la story 7.1 :**
- La goroutine de recovery du `DoHHandler` utilise `onRecovery` callback + channel pour être déterministe dans les tests → adopter le même pattern pour `PeriodicScheduler`
- `NewDoHHandler` paniq si liste vide → `NewPeriodicScheduler` devrait panicker si `checker == nil` ou `interval == 0`
- Les tests avec goroutines doivent utiliser des channels pour être déterministes (pas de `time.Sleep`)

### Informations techniques récentes

**`leakcheck.WebRTCLeakChecker`** (code existant dans `internal/leakcheck/`) :
- Utilise des serveurs STUN : `stun.l.google.com:19302`, `stun.cloudflare.com:3478`
- `RunFullCheck(ctx context.Context) (*FullLeakReport, error)` avec timeout 5s par serveur STUN
- `FullLeakReport.Status` vaut `"pass"` ou `"fail"` (constantes `StatusLeakPass`, `StatusLeakFail` dans `ipc/messages.go`)
- `NewWebRTCLeakChecker(getPublicIP PublicIPFunc)` où `PublicIPFunc = func(ctx context.Context) (string, error)`

**Polling tray (interval 2s)** : `t.pollInterval` dans `tray.go` — la boucle de polling est déjà en place, ajouter la lecture de `resp.LeakStatus` dans la fonction qui applique la réponse `get_status`.

### Structure du projet (rappel)

```
internal/
  leakcheck/
    checker.go          ← existant (WebRTCLeakChecker)
    report.go           ← existant (FullLeakReport, LeakResult)
    scheduler.go        ← À CRÉER (PeriodicScheduler)
    scheduler_test.go   ← À CRÉER
  ipc/
    messages.go         ← À MODIFIER (StatusLeakPending, champs Response)
  ipchandler/
    handler.go          ← À MODIFIER (handleGetStatus enrichi)
  service/
    service.go          ← À MODIFIER (leakScheduler + LeakScheduler())
  tray/
    tray.go             ← À MODIFIER (lecture leak_status, alerte)
```

### Project Structure Notes

- Le package `internal/leakcheck/` existe déjà et contient `WebRTCLeakChecker` — le `PeriodicScheduler` s'y ajoute naturellement
- Le pattern Start/Stop avec `context.Context` est cohérent avec `watchdog.go` et `reconnect.go`
- Les interfaces locales dans `scheduler.go` évitent les imports circulaires entre `leakcheck` ↔ `dns` ↔ `tunnel`
- Aucun nouveau champ TOML nécessaire (intervalle 10 min hardcodé, suffisant pour MVP)

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 7, Story 7.2]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Concurrency Patterns" : context.Context, goroutines sans orphelins]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Error Handling Patterns" : préfixe package, erreurs sentinelles]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Anti-Patterns : Aucun log côté client"]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "NFR13-16 : watchdog goroutine, kill switch < 100ms"]
- [Source: `internal/watchdog/watchdog.go` — pattern Start/Stop/context identique]
- [Source: `internal/leakcheck/checker.go` — `WebRTCLeakChecker`, `RunFullCheck`, `FullLeakReport`]
- [Source: `internal/ipc/messages.go` — `ActionLeakCheck`, `StatusLeakPass`, `StatusLeakFail`, struct `Response`]
- [Source: `internal/ipchandler/handler.go` — `handleGetStatus`, `handleLeakCheck` existants]
- [Source: `internal/service/service.go` — lifecycle `run()`, champs `reconnector`, `watchdog`]
- [Source: `internal/tray/tray.go` — `pollInterval`, `restoreTooltipAfter`, `leakAlertActive` pattern]
- [Source: `internal/tunnel/reconnect.go` — pattern goroutine lifecycle, `TriggerReconnect` à vérifier]
- [Source: `internal/dns/kill_switch.go` — `IsActive() bool` pour interface `KillSwitchQuerier`]
- [Source: story 7-1 Dev Agent Record — leçons sur goroutines déterministes, callback `onRecovery`]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

Aucun blocage. Note : `Reconnector.TriggerReconnect()` n'existe pas — remplacé par `tunnelClient.Disconnect()` qui émet `StateDisconnected` et déclenche la reconnexion automatique via le Reconnector existant. Interface `leakCheckerIface` interne ajoutée pour permettre le mocking dans les tests sans modifier le type `*WebRTCLeakChecker`.

Correction pré-existante : `handleGetStatus` retournait sans les champs rollback quand `tc == nil` — corrigé pour satisfaire `TestHandle_GetStatus_WithRollback` qui échouait avant cette story.

### Completion Notes List

- **AC1/5/6** — `PeriodicScheduler.runCheck()` : skip si `KillSwitch.IsActive()` ou tunnel ≠ `StateConnected` ; sinon `RunFullCheck` avec timeout 25 s
- **AC2** — Résultat `"pass"` : aucun callback, pas de log
- **AC3** — Résultat `"fail"` : `onLeak` appelé → tray affiche `"⚠ Fuite détectée…"` via `LeakStatus == "fail"` dans le poll
- **AC4** — `onLeak` dans `service.go` : `tunnelClient.Disconnect()` déclenche le Reconnector ; goroutine 30 s + `TriggerCheck`
- **AC7** — `handleGetStatus` expose `leak_status` (`"pending"/"pass"/"fail"`) et `leak_last_check` (RFC3339)
- 7 tests unitaires synchrones (mocks via interfaces locales, pas de ticker réel sauf StartStop/AlreadyRunning)

### File List

internal/leakcheck/scheduler.go (créé)
internal/leakcheck/scheduler_test.go (créé)
internal/ipc/messages.go (modifié)
internal/ipchandler/handler.go (modifié)
internal/service/service.go (modifié)
internal/tray/tray.go (modifié)
_bmad-output/implementation-artifacts/sprint-status.yaml (modifié)
_bmad-output/implementation-artifacts/7-2-auto-test-de-fuite-periodique-et-alertes-tray.md (modifié)

## Senior Developer Review (AI)

**Reviewer :** Akerimus — 2026-03-11
**Résultat :** ✅ Approuvé après corrections

### Corrections appliquées

| Sévérité | Problème | Fichier | Fix |
|----------|----------|---------|-----|
| 🔴 HIGH | Chaînes magiques `"fail"`/`"pass"` sans constante | `leakcheck/webrtc.go`, `scheduler.go` | Constantes `statusPass`/`statusFail` ajoutées dans `webrtc.go`, utilisées dans les 2 fichiers |
| 🔴 HIGH | `leakAlertActive` non réinitialisé lors d'erreur IPC → faux-négatif permanent sur future fuite | `tray/tray.go` | `t.leakAlertActive = false` ajouté dans `handleIPCError` |
| 🟡 MEDIUM | Re-test AC4 (30s) silencieusement ignoré si KillSwitch actif durant reconnexion | `service/service.go` | Goroutine `onLeak` attend désormais que `ks.IsActive() == false` avant `TriggerCheck`, avec timeout 2 minutes |
| 🟡 MEDIUM | Zéro couverture du callback `onLeak` reconnect | `leakcheck/scheduler_test.go`, `tray/tray_test.go`, `ipchandler/handler_test.go` | 7 nouveaux tests ajoutés : `TriggerCheck`, `TriggerCheck_SkipsWhenKSActive`, 3 tests tray alerte, 2 tests handler LeakStatus |
| 🟡 MEDIUM | `handleGetStatus` omettait `LeakStatus` quand `tc == nil` (couplage implicite lifecycle) | `ipchandler/handler.go` | Vérification `LeakScheduler()` ajoutée dans le chemin `tc == nil` |
| 🟢 LOW | `leakAlertActive` sans mutex (incohérence) | `tray/tray.go` | Non modifié — accès en goroutine unique, sûr en l'état |
| 🟢 LOW | Indentation constantes IPC | `ipc/messages.go` | Non modifié — cosmétique uniquement |

### Validation AC post-corrections

| AC | Statut | Note |
|----|--------|------|
| AC1 | ✅ | scheduler.go ticker 10min |
| AC2 | ✅ | pass silencieux |
| AC3 | ✅ | alerte tray + test regression H2 |
| AC4 | ✅ | re-test 30s + wait KS inactive + tests |
| AC5 | ✅ | skip KS actif |
| AC6 | ✅ | skip tunnel déconnecté |
| AC7 | ✅ | IPC get_status + test M3 |

### Revue #2 — 2026-03-12

**Reviewer :** Akerimus — 2026-03-12
**Résultat :** ✅ Approuvé après corrections

| Sévérité | Problème | Fichier | Fix |
|----------|----------|---------|-----|
| 🔴 HIGH | `NewPeriodicScheduler` ne valide pas `ks` et `ts` → panic différé dans `runCheck` au lieu de fail-fast | `leakcheck/scheduler.go` | Ajout panic guards pour `ks == nil` et `ts == nil` + 2 tests |
| 🟡 MEDIUM | Tests `scheduler_test.go` utilisent chaînes `"pass"`/`"fail"` au lieu des constantes `statusPass`/`statusFail` | `leakcheck/scheduler_test.go` | Remplacé par constantes du package |
| 🟡 MEDIUM | `go lkScheduler.Start(ctx)` ignore l'erreur de démarrage | `service/service.go` | Erreur logguée via `serviceStderr` |
| 🟡 MEDIUM | Code dupliqué pour remplir `LeakStatus`/`LeakLastCheck` dans `handleGetStatus` (branches tc==nil et tc!=nil) | `ipchandler/handler.go` | Extrait dans helper `fillLeakStatus()` |
| 🟢 LOW | Couplage implicite leakcheck↔ipc par valeurs de chaînes (`result.Status` vs `StatusLeakPass`) | `ipchandler/handler.go` | Non modifié — couplage intentionnel, constantes identiques |
| 🟢 LOW | `leakAlertActive` : alerte unique par cycle de fuite (pas de rappel si fuite persiste) | `tray/tray.go` | Non modifié — design voulu pour éviter le spam |

## Change Log

- 2026-03-12 : Revue code adversariale #2 (Akerimus) — 1 HIGH + 3 MEDIUM corrigés. Panic guards ajoutés pour `ks`/`ts` nil. Tests corrigés pour utiliser constantes. Erreur `Start()` logguée. Code dupliqué leak status extrait dans `fillLeakStatus()`. 2 nouveaux tests panic.
- 2026-03-11 : Revue code adversariale (Akerimus) — 2 problèmes HIGH + 3 MEDIUM corrigés. Constantes `statusPass`/`statusFail` ajoutées. `leakAlertActive` réinitialisé dans `handleIPCError`. Goroutine AC4 attend fin de reconnexion (KS inactif). `handleGetStatus` expose `LeakStatus` même si `tc == nil`. 7 nouveaux tests.
- 2026-03-11 : Implémentation complète de la Story 7.2 — `PeriodicScheduler` créé, champs IPC ajoutés (`leak_status`/`leak_last_check`), intégration service + tray, 7 tests unitaires. Correction pré-existante : `handleGetStatus` incluait les champs rollback uniquement quand `tc != nil`.
