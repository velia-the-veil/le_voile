# Story 9.2 : Sélection par latence et failover automatique

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux être connecté automatiquement au relais le plus rapide, et basculer vers un autre si mon relais tombe,
Afin d'avoir la meilleure performance et une protection sans interruption.

## Acceptance Criteria

**AC1 — Mesure de latence et sélection optimale**
**Given** la liste des relais découverts (via `Discoverer.Discover()` story 9.1)
**When** le client mesure la latence de chaque relais (GET HTTPS vers `/health`)
**Then** le relais avec la latence la plus basse est sélectionné — sélection complète en < 5 secondes

**AC2 — Cache du classement de latence**
**Given** le relais optimal sélectionné
**When** le tunnel est établi
**Then** le client stocke le classement de latence en cache pour accélérer les démarrages suivants

**AC3 — Failover automatique**
**Given** le tunnel connecté à un relais
**When** le relais devient indisponible (timeout, erreur réseau)
**Then** le client bascule automatiquement vers le relais suivant dans le classement de latence

**AC4 — Kill switch pendant failover**
**Given** un failover vers un autre relais
**When** le basculement s'effectue
**Then** le kill switch DNS reste actif pendant la transition et la reconnexion utilise le même mécanisme qu'Epic 2 (backoff exponentiel)

**AC5 — Sticky session**
**Given** le relais original redevient disponible
**When** sa latence est meilleure que le relais actuel
**Then** le client ne bascule PAS automatiquement (sticky session) — le changement ne se fait qu'au prochain redémarrage ou failover

## Tasks / Subtasks

- [x] **Task 1 : Module `internal/registry/latency.go` — Mesure de latence** (AC: 1)
  - [x] 1.1 Créer `internal/registry/latency.go` — struct `LatencyChecker` : `httpClient *http.Client` (timeout 5s, pas de HTTP/3 — HTTP/1.1 suffit pour le health check)
  - [x] 1.2 Constructeur `NewLatencyChecker(opts ...LatencyOption) *LatencyChecker` — option `WithLatencyHTTPClient(*http.Client)`
  - [x] 1.3 Méthode `MeasureOne(ctx context.Context, relay RelayEntry) (time.Duration, error)` — GET `https://{relay.Domain}/health`, mesure le temps aller-retour, retourne la durée ; si erreur/timeout → retourne `(0, err)`. Utiliser `time.Now()` avant/après le HTTP GET
  - [x] 1.4 Méthode `MeasureAll(ctx context.Context, relays []RelayEntry) []LatencyResult` — mesure TOUTES les latences en **parallèle** (une goroutine par relais), timeout global 5s via context. Struct `LatencyResult` : `Relay RelayEntry`, `Latency time.Duration`, `Reachable bool`, `Error error`
  - [x] 1.5 Méthode `SortByLatency(results []LatencyResult) []RelayEntry` — trie par latence croissante, relais injoignables en fin de liste ; retourne uniquement les `RelayEntry` triés
  - [x] 1.6 Constantes : `HealthEndpoint = "/health"`, `DefaultLatencyTimeout = 5 * time.Second`, `MaxMeasureTimeout = 5 * time.Second`

- [x] **Task 2 : Cache de latence dans `internal/registry/cache.go`** (AC: 2)
  - [x] 2.1 Étendre `CachedRegistry` TOML avec section `[[latency_rankings]]` — struct `CachedLatency` : `RelayID string`, `Latency string` (duration sérialisée `"42ms"`), `MeasuredAt time.Time`
  - [x] 2.2 Méthode `SaveLatencies(rankings []LatencyResult) error` — lit le cache existant, ajoute/met à jour la section latency_rankings, écriture atomique
  - [x] 2.3 Méthode `LoadLatencies() ([]CachedLatency, error)` — retourne les rankings sauvegardés ; si absents → retourne slice vide (pas d'erreur)
  - [x] 2.4 Dans `Discoverer.Discover()` : si les mesures de latence échouent (réseau), utiliser le cache de latence pour trier les relais au lieu du tri par défaut (Added DESC)

- [x] **Task 3 : Extension `Discoverer` pour la sélection par latence** (AC: 1, 2)
  - [x] 3.1 Ajouter champ `latencyChecker *LatencyChecker` dans struct `Discoverer`
  - [x] 3.2 Ajouter option constructeur `WithLatencyChecker(checker *LatencyChecker)` comme `DiscovererOption`
  - [x] 3.3 Modifier `Discover(ctx context.Context)` : après fetch/cache → si `latencyChecker != nil` ET `len(relays) > 1` → appeler `latencyChecker.MeasureAll(ctx, relays)` → `SortByLatency()` → `cache.SaveLatencies()` → `setRelays(sortedRelays)`. Si MeasureAll échoue → essayer `cache.LoadLatencies()` pour trier. Si aucun ranking → garder l'ordre fetch (Added DESC)
  - [x] 3.4 Modifier `refreshLoop()` : à chaque rafraîchissement, re-mesurer les latences et réordonner (mais NE PAS changer le relais actif — sticky session AC5). Logger le nouveau classement dans les relays internes sans déclencher de failover

- [x] **Task 4 : Méthode `UpdateRelay` sur `tunnel.Client`** (AC: 3, 4)
  - [x] 4.1 Ajouter méthode `UpdateRelay(relayDomain string, relayPubKeyBase64 string) error` dans `internal/tunnel/client.go` — met à jour `c.relayDomain` et `c.relayPubKey` de manière thread-safe (sous `sync.Mutex`). NE PAS appeler `Connect()` — le caller est responsable de la reconnexion
  - [x] 4.2 Ajouter `mu sync.Mutex` dans struct `Client` pour protéger `relayDomain` et `relayPubKey`
  - [x] 4.3 Modifier `Connect()` et `verifyRelay()` pour lire `relayDomain` et `relayPubKey` sous `mu.RLock()` (lectures thread-safe)
  - [x] 4.4 Ajouter méthode `RelayDomain() string` — getter thread-safe pour le domaine actuel

- [x] **Task 5 : Module failover `internal/registry/failover.go`** (AC: 3, 4, 5)
  - [x] 5.1 Créer `internal/registry/failover.go` — struct `FailoverManager` : `discoverer *Discoverer`, `tunnelUpdater RelayUpdater`, `connectFn func(ctx context.Context) error`, `currentRelayID string`, `mu sync.Mutex`, `stopCh chan struct{}`, `once sync.Once`
  - [x] 5.2 Interface `RelayUpdater` : `UpdateRelay(domain string, pubKeyBase64 string) error` — implémentée par `tunnel.Client`
  - [x] 5.3 Constructeur `NewFailoverManager(discoverer *Discoverer, updater RelayUpdater, connectFn func(ctx context.Context) error) *FailoverManager`
  - [x] 5.4 Méthode `HandleFailover(ctx context.Context) error` — appelée quand le relais actuel échoue :
    1. Verrouiller `mu`
    2. Appeler `discoverer.Relays()` pour obtenir le classement actuel
    3. Trouver le relais **suivant** après `currentRelayID` dans le classement
    4. Si trouvé → `tunnelUpdater.UpdateRelay(next.Domain, next.PublicKey)` → `connectFn(ctx)` → mettre à jour `currentRelayID`
    5. Si aucun relais suivant → retourner `ErrNoAlternativeRelay`
    6. Déverrouiller `mu`
  - [x] 5.5 Méthode `SetCurrentRelay(relayID string)` — définit le relais actif actuel
  - [x] 5.6 Méthode `CurrentRelayID() string` — getter thread-safe
  - [x] 5.7 Erreurs sentinelles : `ErrNoAlternativeRelay = errors.New("registry: failover: no alternative relay available")`

- [x] **Task 6 : Extension `Reconnector` pour failover** (AC: 3, 4)
  - [x] 6.1 Ajouter champ optionnel `failoverFn func(ctx context.Context) error` dans struct `Reconnector`
  - [x] 6.2 Ajouter option constructeur `WithFailoverFn(fn func(ctx context.Context) error)` comme `ReconnectorOption`
  - [x] 6.3 Modifier `handleDisconnect()` : après échec de `connectFn(ctx)` avec backoff maximal atteint (3 tentatives échouées sur le relais actuel) → si `failoverFn != nil` → appeler `failoverFn(ctx)`. Si failover réussit → reset le backoff. Si failover échoue → continuer le backoff normal sur le relais actuel
  - [x] 6.4 Ajouter constante `MaxRetriesBeforeFailover = 3` — nombre de tentatives avant de déclencher le failover

- [x] **Task 7 : Intégration service `internal/service/service.go`** (AC: 1, 2, 3, 4, 5)
  - [x] 7.1 Ajouter champ `failoverMgr *registry.FailoverManager` dans struct `Program`
  - [x] 7.2 Dans `run()`, **après** la découverte (étape 0b), si `RegistryEnabled` ET `len(relays) > 1` :
    - Créer `registry.NewLatencyChecker()` → le passer au Discoverer via `WithLatencyChecker()`
    - Re-appeler `discoverer.Discover(ctx)` avec le latency checker pour obtenir les relais triés par latence
    - Utiliser `discoverer.Primary()` pour le relais optimal
  - [x] 7.3 Dans `run()`, **après** la connexion tunnel réussie (après étape 1), si `RegistryEnabled` ET `len(relays) > 1` :
    - Créer `registry.NewFailoverManager(discoverer, tunnelClient, tunnelClient.Connect)`
    - Appeler `failoverMgr.SetCurrentRelay(relays[0].ID)`
    - Passer `failoverMgr.HandleFailover` comme `WithFailoverFn()` au Reconnector
  - [x] 7.4 Dans `shutdown()` : rien de spécial — le FailoverManager n'a pas de goroutine à arrêter

- [x] **Task 8 : Tests** (AC: 1, 2, 3, 4, 5)
  - [x] 8.1 `internal/registry/latency_test.go` :
    - `TestMeasureOne_Success` — httptest.Server répondant JSON `{"status":"ok"}` → latence > 0, pas d'erreur
    - `TestMeasureOne_Timeout` — serveur qui bloque → erreur context deadline
    - `TestMeasureOne_HTTPError` — serveur retournant 500 → erreur
    - `TestMeasureAll_Parallel` — 3 serveurs avec délais différents (0ms, 50ms, 100ms simulés) → résultats corrects, vérifie que MeasureAll complète en < 200ms (parallèle, pas séquentiel)
    - `TestMeasureAll_PartialFailure` — 2 OK + 1 timeout → 2 Reachable + 1 pas Reachable
    - `TestSortByLatency_Order` — résultats mélangés (100ms, 20ms, 50ms, unreachable) → triés [20ms, 50ms, 100ms, unreachable]
    - `TestSortByLatency_AllUnreachable` — tous en erreur → liste vide
  - [x] 8.2 `internal/registry/cache_test.go` (extensions) :
    - `TestCache_SaveAndLoadLatencies` — sauvegarder 3 rankings, recharger, vérifier égalité
    - `TestCache_LoadLatencies_NotFound` — pas de section latency → retourne slice vide sans erreur
  - [x] 8.3 `internal/registry/discoverer_test.go` (extensions) :
    - `TestDiscoverer_WithLatencyChecker_SortsRelays` — 3 relais, latencies [100ms, 20ms, 50ms] → Primary() retourne relais 20ms
    - `TestDiscoverer_LatencyFallbackToCache` — latency checker échoue, cache de latence existe → utilise rankings cache
    - `TestDiscoverer_RefreshLoop_NoRelaySwitch` — vérifier que refreshLoop re-mesure mais NE change PAS le relais actif si déjà connecté (sticky)
  - [x] 8.4 `internal/tunnel/client_test.go` (extension) :
    - `TestClient_UpdateRelay_ThreadSafe` — appels concurrents UpdateRelay + Connect sans race condition
    - `TestClient_RelayDomain_AfterUpdate` — UpdateRelay("new.domain", key) → RelayDomain() == "new.domain"
  - [x] 8.5 `internal/registry/failover_test.go` :
    - `TestFailoverManager_HandleFailover_Success` — 3 relais, current = relais[0], failover → utilise relais[1], connectFn réussit
    - `TestFailoverManager_HandleFailover_SkipsCurrent` — ne retente pas le relais actuel
    - `TestFailoverManager_HandleFailover_NoAlternative` — un seul relais → `ErrNoAlternativeRelay`
    - `TestFailoverManager_HandleFailover_AllFail` — connectFn échoue sur tous les relais alternatifs → `ErrNoAlternativeRelay`
    - `TestFailoverManager_ThreadSafe` — appels concurrents HandleFailover sans race
  - [x] 8.6 `internal/tunnel/reconnect_test.go` (extension) :
    - `TestReconnector_FailoverAfterMaxRetries` — connectFn échoue 3 fois → failoverFn appelé
    - `TestReconnector_NoFailoverIfConnectSucceeds` — connectFn réussit à la 2e tentative → failoverFn jamais appelé
    - `TestReconnector_FailoverFnNil_ContinuesBackoff` — pas de failoverFn → comportement actuel inchangé
  - [x] 8.7 Validation build : `go build ./cmd/client/... ./cmd/tray/... ./cmd/relay/...` — compilation OK

## Dev Notes

### Architecture de la Story 9.2 : Extension du module `internal/registry/` + failover

Cette story **étend** le package `internal/registry/` créé en 9.1 et ajoute un mécanisme de failover. Pas de nouveau package — tout s'intègre dans l'existant.

```
internal/registry/
├── registry.go          # Types (INCHANGÉ)
├── registry_test.go     # Tests parse + chain of trust (INCHANGÉ)
├── client.go            # Client HTTP (INCHANGÉ)
├── client_test.go       # Tests client (INCHANGÉ)
├── cache.go             # Cache TOML — ÉTENDU (latency_rankings)
├── cache_test.go        # Tests cache — ÉTENDUS
├── discoverer.go        # Orchestrateur — ÉTENDU (latency checker, options)
├── discoverer_test.go   # Tests orchestrateur — ÉTENDUS
├── latency.go           # NOUVEAU — Mesure de latence HTTP
├── latency_test.go      # NOUVEAU — Tests latence
├── failover.go          # NOUVEAU — Gestionnaire de failover
└── failover_test.go     # NOUVEAU — Tests failover
```

### Mesure de latence — implémentation exacte

Le latency checker mesure la latence via un simple GET HTTP/1.1 vers l'endpoint `/health` de chaque relais. **Pas de HTTP/3** pour la mesure — on veut mesurer la latence réseau pure, pas la latence du tunnel QUIC.

```go
// Mesure d'un relais
func (lc *LatencyChecker) MeasureOne(ctx context.Context, relay RelayEntry) (time.Duration, error) {
    url := "https://" + relay.Domain + HealthEndpoint
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    start := time.Now()
    resp, err := lc.httpClient.Do(req)
    elapsed := time.Since(start)
    if err != nil {
        return 0, fmt.Errorf("registry: latency: %s: %w", relay.ID, err)
    }
    defer resp.Body.Close()
    io.Copy(io.Discard, resp.Body) // drain body
    if resp.StatusCode != http.StatusOK {
        return 0, fmt.Errorf("registry: latency: %s: status %d", relay.ID, resp.StatusCode)
    }
    return elapsed, nil
}
```

**ATTENTION** : Le `httpClient` du LatencyChecker est **différent** de celui du registry Client. Le registry Client utilise un client standard avec timeout 10s. Le LatencyChecker utilise un client avec timeout 5s car la mesure doit être rapide.

### Mesure parallèle — toutes les latences en < 5 secondes (AC1)

```go
func (lc *LatencyChecker) MeasureAll(ctx context.Context, relays []RelayEntry) []LatencyResult {
    ctx, cancel := context.WithTimeout(ctx, MaxMeasureTimeout)
    defer cancel()

    results := make([]LatencyResult, len(relays))
    var wg sync.WaitGroup
    for i, relay := range relays {
        wg.Add(1)
        go func(idx int, r RelayEntry) {
            defer wg.Done()
            latency, err := lc.MeasureOne(ctx, r)
            results[idx] = LatencyResult{
                Relay:     r,
                Latency:   latency,
                Reachable: err == nil,
                Error:     err,
            }
        }(i, relay)
    }
    wg.Wait()
    return results
}
```

### Tri par latence

```go
func SortByLatency(results []LatencyResult) []RelayEntry {
    // Séparer reachable et unreachable
    var reachable []LatencyResult
    for _, r := range results {
        if r.Reachable {
            reachable = append(reachable, r)
        }
    }
    // Trier reachable par latence croissante
    sort.Slice(reachable, func(i, j int) bool {
        return reachable[i].Latency < reachable[j].Latency
    })
    sorted := make([]RelayEntry, 0, len(reachable))
    for _, r := range reachable {
        sorted = append(sorted, r.Relay)
    }
    return sorted
}
```

### Cache de latence — format TOML étendu

```toml
master_public_key = "base64..."
updated = 2026-03-12T00:00:00Z

[[relays]]
id = "relay-iceland-1"
domain = "levoile.dev"
public_key = "base64..."
signature = "base64..."

[[relays]]
id = "relay-finland-1"
domain = "fi.levoile.dev"
public_key = "base64..."
signature = "base64..."

[[latency_rankings]]
relay_id = "relay-iceland-1"
latency = "42ms"
measured_at = 2026-03-12T10:30:00Z

[[latency_rankings]]
relay_id = "relay-finland-1"
latency = "78ms"
measured_at = 2026-03-12T10:30:00Z
```

### Failover — logique de basculement

Le failover est déclenché par le `Reconnector` après `MaxRetriesBeforeFailover` (3) échecs de reconnexion consécutifs sur le relais actuel.

```
Reconnector.handleDisconnect()
├── killSwitch.Activate()
├── Tentative 1 : connectFn(ctx) sur relais actuel → échec
├── Backoff 1s
├── Tentative 2 : connectFn(ctx) sur relais actuel → échec
├── Backoff 2s
├── Tentative 3 : connectFn(ctx) sur relais actuel → échec
├── MaxRetriesBeforeFailover atteint !
├── failoverFn(ctx) :
│   ├── FailoverManager.HandleFailover(ctx)
│   ├── Cherche relais suivant dans discoverer.Relays()
│   ├── tunnelClient.UpdateRelay(next.Domain, next.PublicKey)
│   ├── connectFn(ctx) sur nouveau relais → succès
│   └── Met à jour currentRelayID
├── killSwitch.Deactivate()
└── Reconnexion réussie sur nouveau relais
```

**ATTENTION** : Le kill switch DNS reste **actif** pendant TOUT le failover (AC4). Il n'est désactivé qu'après la reconnexion réussie au nouveau relais. C'est le comportement existant du Reconnector — `killSwitch.Activate()` au début, `killSwitch.Deactivate()` seulement après `connectFn()` réussit.

### Sticky session — aucun basculement spontané (AC5)

Quand le `refreshLoop()` du Discoverer re-mesure les latences et constate que l'ancien relais (maintenant rétabli) est plus rapide que le relais actuel, il **ne déclenche PAS de failover**. Les relais internes sont réordonnés, mais le `currentRelayID` du FailoverManager ne change pas. Le basculement vers un meilleur relais ne se fait qu'au :
- Prochain redémarrage du service
- Prochain failover (si le relais actuel tombe)

### Point d'intégration dans `service.go` — extension de l'étape 0b

```go
// EXISTANT story 9.1 (étape 0b) :
if p.cfg.RegistryEnabled {
    regClient, err := registry.NewClient(...)
    cache := registry.NewCache(cachePath)
    defaultRelay := registry.RelayEntry{...}
    p.discoverer = registry.NewDiscoverer(regClient, cache, defaultRelay)
    relays, err := p.discoverer.Discover(ctx)
    if err == nil && len(relays) > 0 {
        relayDomain = relays[0].Domain
        relayPubKey = relays[0].PublicKey
    }
}

// NOUVEAU story 9.2 — AVANT p.discoverer.Discover() :
// Créer le LatencyChecker et le passer au Discoverer
if p.cfg.RegistryEnabled {
    regClient, err := registry.NewClient(...)
    cache := registry.NewCache(cachePath)
    defaultRelay := registry.RelayEntry{...}

    latencyChecker := registry.NewLatencyChecker() // NOUVEAU
    p.discoverer = registry.NewDiscoverer(regClient, cache, defaultRelay,
        registry.WithLatencyChecker(latencyChecker)) // NOUVEAU option

    relays, err := p.discoverer.Discover(ctx)
    // relays maintenant triés par latence (meilleur en premier)
    if err == nil && len(relays) > 0 {
        relayDomain = relays[0].Domain
        relayPubKey = relays[0].PublicKey
    }
}

// NOUVEAU — après connexion tunnel réussie (après étape 1) :
if p.discoverer != nil && len(p.discoverer.Relays()) > 1 {
    p.failoverMgr = registry.NewFailoverManager(
        p.discoverer,
        p.tunnelClient, // implémente RelayUpdater
        p.tunnelClient.Connect,
    )
    p.failoverMgr.SetCurrentRelay(p.discoverer.Primary().ID)
}

// MODIFIÉ — création Reconnector (étape 5) avec failover :
var reconnOpts []tunnel.ReconnectorOption
if p.failoverMgr != nil {
    reconnOpts = append(reconnOpts, tunnel.WithFailoverFn(p.failoverMgr.HandleFailover))
}
p.reconnector = tunnel.NewReconnector(
    p.tunnelClient.State().Updates(),
    p.tunnelClient.Connect,
    p.killSwitch,
    reconnOpts...,
)
```

### Modification du Reconnector — compteur de tentatives

Le `Reconnector` actuel utilise un backoff exponentiel sans limite de tentatives. Story 9.2 ajoute un compteur : après `MaxRetriesBeforeFailover` (3) échecs consécutifs, si `failoverFn` est défini, il est appelé.

```go
// MODIFIÉ dans handleDisconnect() :
func (r *Reconnector) handleDisconnect(ctx context.Context) {
    r.killSwitch.Activate(ctx)
    backoff := InitialBackoff
    retries := 0
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }
        err := r.connectFn(ctx)
        if err == nil {
            r.killSwitch.Deactivate(ctx)
            return
        }
        retries++
        // NOUVEAU : failover après MaxRetriesBeforeFailover
        if retries >= MaxRetriesBeforeFailover && r.failoverFn != nil {
            if failErr := r.failoverFn(ctx); failErr == nil {
                // Failover a changé le relais et reconnecté
                r.killSwitch.Deactivate(ctx)
                return
            }
            // Failover échoué — continuer backoff normal
        }
        time.Sleep(backoff)
        backoff = min(backoff*BackoffFactor, MaxBackoff)
    }
}
```

**ATTENTION** : Le `failoverFn` (FailoverManager.HandleFailover) appelle `connectFn` en interne après avoir changé le relais. Si le failover réussit, `handleDisconnect` sort immédiatement. Si le failover échoue, on continue le backoff normal.

### Aucune modification du module `internal/relay/`

Le relais server n'est pas modifié. L'endpoint `/health` existant est utilisé tel quel par le LatencyChecker. Aucune modification nécessaire.

### Aucune modification de `internal/dns/`, `internal/ipc/`, `internal/tray/`

- **DNS** : Le kill switch fonctionne comme avant — activé/désactivé par le Reconnector
- **IPC** : Pas de nouvelle action IPC pour le failover (pas de contrôle tray)
- **Tray** : Pas de nouvelle entrée de menu pour la sélection de relais

### Conventions Go à respecter (depuis architecture.md et story 9.1)

- **Nommage fichiers** : `snake_case.go` — `latency.go`, `failover.go`
- **Fonctions exportées** : `PascalCase` — `NewLatencyChecker`, `MeasureAll`, `HandleFailover`
- **Erreurs** : wrapping `fmt.Errorf("registry: latency: %w", err)` ou `fmt.Errorf("registry: failover: %w", err)`
- **Context** : premier paramètre de `MeasureOne()`, `MeasureAll()`, `HandleFailover()`
- **Concurrence** : `sync.WaitGroup` pour MeasureAll parallèle, `sync.Mutex` pour FailoverManager et Client.UpdateRelay
- **Tests** : co-localisés (`*_test.go`), table-driven quand > 2 cas, `testing` standard uniquement
- **Aucun log côté client** — erreurs retournées, jamais loggées
- **Aucun import circulaire** : `registry` n'importe pas `tunnel`, `service`, ou `config`. L'interface `RelayUpdater` dans `registry/failover.go` découple de `tunnel.Client`

### Bibliothèques utilisées

- `net/http` (Go standard) — client HTTP pour mesure latence (HTTP/1.1, pas besoin de HTTP/3)
- `sort` (Go standard) — tri des résultats de latence
- `sync` (Go standard) — WaitGroup pour parallélisme, Mutex pour thread-safety
- `time` (Go standard) — mesure de durée
- `github.com/BurntSushi/toml v1.5.0` (déjà dans go.mod) — extension cache TOML
- **Aucune nouvelle dépendance** — tout est dans la stdlib Go

### Leçons de la Story 9.1 (à appliquer)

- **Écriture atomique** : Le cache TOML utilise `.tmp` + rename — continuer ce pattern pour SaveLatencies
- **Fallback cascade** : online → cache → default. Story 9.2 ajoute un niveau : latence live → cache latence → ordre par défaut (Added DESC)
- **Code review 9.1 corrections** : `io.LimitReader` pour les body HTTP (appliquer dans MeasureOne), validation URL dans constructeur
- **`sync.Once` pour Stop()** : Le Discoverer utilise sync.Once pour éviter les panics de double close — le FailoverManager n'a pas de goroutine, donc pas besoin
- **`io.LimitReader(resp.Body, 1<<20)`** : Limiter la lecture du body dans MeasureOne comme dans client.Fetch (protection DoS)
- **Erreurs sentinelles** : utiliser `errors.New` pour `ErrNoAlternativeRelay`

### Fichiers à créer

| Fichier | Description |
|---------|-------------|
| `internal/registry/latency.go` | LatencyChecker, MeasureOne, MeasureAll, SortByLatency, types |
| `internal/registry/latency_test.go` | Tests mesure de latence avec httptest |
| `internal/registry/failover.go` | FailoverManager, HandleFailover, RelayUpdater interface |
| `internal/registry/failover_test.go` | Tests failover |

### Fichiers à modifier

| Fichier | Modification |
|---------|-------------|
| `internal/registry/cache.go` | Ajout `CachedLatency` struct, `SaveLatencies()`, `LoadLatencies()`, extension `CachedRegistry` |
| `internal/registry/cache_test.go` | Tests cache latence |
| `internal/registry/discoverer.go` | Ajout `latencyChecker` champ, `WithLatencyChecker()` option, modification `Discover()` et `refreshLoop()` |
| `internal/registry/discoverer_test.go` | Tests discoverer avec latency checker |
| `internal/tunnel/client.go` | Ajout `mu sync.Mutex`, `UpdateRelay()`, `RelayDomain()`, modification `Connect()`/`verifyRelay()` pour lecture thread-safe |
| `internal/tunnel/client_test.go` | Tests UpdateRelay thread-safe |
| `internal/tunnel/reconnect.go` | Ajout `failoverFn`, `WithFailoverFn()`, `MaxRetriesBeforeFailover`, modification `handleDisconnect()` |
| `internal/tunnel/reconnect_test.go` | Tests failover dans reconnector |
| `internal/service/service.go` | Ajout `failoverMgr`, intégration LatencyChecker + FailoverManager dans `run()` |

### Fichiers à NE PAS toucher

- `internal/registry/registry.go` — types inchangés
- `internal/registry/client.go` — client HTTP inchangé
- `internal/relay/` — aucune modification (endpoint /health utilisé tel quel)
- `internal/dns/` — non concerné
- `internal/ipc/` — non concerné
- `internal/tray/` — non concerné
- `internal/watchdog/` — non concerné
- `internal/blocklist/` — non concerné
- `internal/crypto/` — non concerné
- `internal/config/config.go` — pas de nouveau champ config (latency checker est activé implicitement quand registry est activé)
- `cmd/client/main.go` — pas de modification (la config existante suffit)
- `cmd/relay/main.go` — non modifié
- `cmd/tray/main.go` — non modifié

### Vérification couverture AC

| AC | Couvert par |
|----|------------|
| AC1 (mesure latence + sélection < 5s) | Task 1 (LatencyChecker, MeasureAll parallèle avec timeout 5s), Task 3 (intégration Discoverer) |
| AC2 (cache classement latence) | Task 2 (SaveLatencies/LoadLatencies dans cache TOML), Task 3.3 (sauvegarde après mesure) |
| AC3 (failover automatique) | Task 5 (FailoverManager.HandleFailover), Task 6 (Reconnector + failoverFn), Task 4 (UpdateRelay) |
| AC4 (kill switch pendant transition) | Task 6 (killSwitch.Activate avant, Deactivate après — comportement Reconnector existant préservé) |
| AC5 (sticky session) | Task 3.4 (refreshLoop réordonne sans changer relais actif), Task 5 (failover ne revient pas au relais original) |

### Note sur la mesure de latence en production

La mesure de latence utilise l'endpoint `/health` des relais. En production, les relais sont derrière Cloudflare — la latence mesurée inclut le temps Cloudflare CDN → VPS. C'est représentatif car le tunnel DNS passe aussi par Cloudflare.

Si un seul relais est disponible (cas actuel), le LatencyChecker est quand même appelé mais le tri ne change rien. Le failover ne peut pas fonctionner avec un seul relais — `ErrNoAlternativeRelay` est retourné.

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 9, Story 9.2, AC1-5]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Naming Patterns", "Error Handling Patterns", "Concurrency Patterns", "Project Structure"]
- [Source: `_bmad-output/planning-artifacts/prd.md` — Phase 2-3 "Sélection automatique du relais optimal (latence)"]
- [Source: `_bmad-output/implementation-artifacts/9-1-registre-de-relais-et-decouverte-dynamique.md` — Architecture registry, patterns, leçons]
- [Source: `internal/registry/discoverer.go` — Discoverer struct, Discover(), Primary(), refreshLoop(), setRelays()]
- [Source: `internal/registry/client.go` — Client struct, Fetch(), RefreshInterval(), MasterPublicKeyBase64()]
- [Source: `internal/registry/cache.go` — Cache struct, Save(), Load(), CachedRegistry, écriture atomique]
- [Source: `internal/registry/registry.go` — RelayEntry struct (ID, Domain, PublicKey, Signature, Added)]
- [Source: `internal/tunnel/client.go` — Client struct, Connect(), relayDomain, relayPubKey, State()]
- [Source: `internal/tunnel/reconnect.go` — Reconnector struct, handleDisconnect(), InitialBackoff, BackoffFactor, MaxBackoff, KillSwitchController]
- [Source: `internal/tunnel/state.go` — StateManager, ConnState, StateDisconnected]
- [Source: `internal/relay/health.go` — HealthHandler, HealthResponse, /health endpoint]
- [Source: `internal/service/service.go` — Program struct, run() flow étapes 0b-5, shutdown(), Config struct, discoverer field]
- [Source: `go.mod` — Go 1.26, BurntSushi/toml v1.5.0]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- ✅ Task 1: Created `internal/registry/latency.go` with `LatencyChecker`, `MeasureOne`, `MeasureAll` (parallel with goroutines), `SortByLatency`, `LatencyResult` type, and constants. Applied `io.LimitReader` for DoS protection per story 9.1 lessons.
- ✅ Task 2: Extended `internal/registry/cache.go` with `CachedLatency` struct, `SaveLatencies()` (atomic write pattern), `LoadLatencies()`. Extended `CachedRegistry` TOML struct with `LatencyRankings` field.
- ✅ Task 3: Extended `internal/registry/discoverer.go` with `DiscovererOption` pattern, `WithLatencyChecker()`, `sortByLatency()` and `sortByLatencyCache()` fallback methods. `Discover()` now sorts by latency when checker configured. `refreshLoop()` re-measures latencies without triggering relay switch (sticky session AC5).
- ✅ Task 4: Added `sync.RWMutex` to `tunnel.Client`, `UpdateRelay()` method with key import validation, `RelayDomain()` getter. Modified `verifyRelay()`, `SendDoHQuery()`, `SendSTUNRelay()` to read relay coordinates under `RLock`.
- ✅ Task 5: Created `internal/registry/failover.go` with `FailoverManager`, `RelayUpdater` interface, `HandleFailover()` (tries all alternative relays in ranking order), `ErrNoAlternativeRelay` sentinel error.
- ✅ Task 6: Extended `internal/tunnel/reconnect.go` with `ReconnectorOption`, `WithFailoverFn()`, `MaxRetriesBeforeFailover = 3`. Modified `handleDisconnect()` to trigger failover after 3 consecutive failures. Extracted `deactivateKillSwitch()` helper.
- ✅ Task 7: Integrated in `internal/service/service.go`: `failoverMgr` field on `Program`, `LatencyChecker` creation in step 0b, `FailoverManager` setup in new step 1d after tunnel connect, `WithFailoverFn` passed to `Reconnector` in step 5.
- ✅ Task 8: All tests written and passing (36 registry tests, tunnel tests including failover). No regressions. Build validates for all three commands.

### Change Log

- 2026-03-12: Story 9.2 implementation complete — latency-based relay selection, latency caching, automatic failover, kill switch preservation during failover, sticky session behavior
- 2026-03-12: Code review fixes (5 issues) — [C1] Added missing TestDiscoverer_RefreshLoop_NoRelaySwitch test for AC5 sticky session; [H1] Fixed TestDiscoverer_WithLatencyChecker_SortsRelays to use TLS servers and assert sort order; [H2] Fixed TestDiscoverer_LatencyFallbackToCache with real assertions; [H3] Fixed handleDisconnect failover called repeatedly (added failoverAttempted flag); [M1] Fixed cache.Save() to preserve latency_rankings section; [M2] Fixed HandleFailover loop bound for currentIdx=-1 edge case
- 2026-03-12: Code review fixes #2 (4 issues) — [H1] SortByLatency now keeps unreachable relays at end of list instead of dropping them (preserves failover options, fixes Task 1.5 spec); discoverer.sortByLatency checks hasReachable before deciding cache fallback; [M1] Removed dead cert-merging loop in TestMeasureAll_Parallel; [M2] sortByLatencyCache uses sort.Slice instead of bubble sort; [M3] FailoverManager.CurrentRelayID() uses RWMutex.RLock() instead of exclusive Lock()
- 2026-03-12: Code review fixes #3 (4 issues) — [H1] HandleFailover restores original relay coordinates on tunnel client after complete failover failure (Reconnector was retrying on wrong relay); [M1] TestHandleFailover_AllFail now verifies updater restored to original relay; [M2] MeasureOne validates non-empty domain before URL construction; [L1] Discover() doc comment clarifies error return is always nil (fallback cascade guarantees success)

### File List

**New files:**
- `internal/registry/latency.go` — LatencyChecker, MeasureOne, MeasureAll, SortByLatency, LatencyResult type
- `internal/registry/latency_test.go` — 7 tests for latency measurement
- `internal/registry/failover.go` — FailoverManager, RelayUpdater interface, HandleFailover
- `internal/registry/failover_test.go` — 5 tests for failover manager

**Modified files:**
- `internal/registry/cache.go` — Added CachedLatency struct, SaveLatencies(), LoadLatencies(), extended CachedRegistry
- `internal/registry/cache_test.go` — Added 2 tests for latency cache
- `internal/registry/discoverer.go` — Added DiscovererOption, WithLatencyChecker, sortByLatency/sortByLatencyCache methods, modified Discover() and refreshLoop()
- `internal/registry/discoverer_test.go` — Added 2 tests for latency integration
- `internal/tunnel/client.go` — Added sync.RWMutex, UpdateRelay(), RelayDomain(), thread-safe reads in verifyRelay/SendDoHQuery/SendSTUNRelay
- `internal/tunnel/client_test.go` — Added 2 tests for UpdateRelay thread-safety
- `internal/tunnel/reconnect.go` — Added ReconnectorOption, WithFailoverFn, MaxRetriesBeforeFailover, modified handleDisconnect() for failover, extracted deactivateKillSwitch()
- `internal/tunnel/reconnect_test.go` — Added 3 tests for failover in reconnector
- `internal/service/service.go` — Added failoverMgr field, LatencyChecker integration in step 0b, FailoverManager setup in step 1d, WithFailoverFn in Reconnector creation
