# Story 8.1 : Téléchargement et parsing de la blocklist StevenBlack/hosts

Status: done

## Story

En tant qu'utilisateur,
Je veux que Le Voile télécharge et maintienne à jour la blocklist communautaire StevenBlack/hosts,
Afin de disposer d'une liste de filtrage fiable et à jour.

## Acceptance Criteria

**AC1 — Téléchargement initial au démarrage si activé**
**Given** le service actif et la blocklist activée (`blocklist.enabled = true` dans config TOML)
**When** la blocklist est activée pour la première fois ou n'existe pas en mémoire
**Then** le client télécharge la blocklist depuis `https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts` et la parse au format hosts (lignes `0.0.0.0 domaine`)

**AC2 — Stockage en structure mémoire optimisée**
**Given** la blocklist téléchargée
**When** elle est parsée
**Then** les domaines sont stockés dans un `map[string]struct{}` pour une recherche O(1) — accessible via `IsBlocked(domain string) bool`

**AC3 — Mise à jour automatique toutes les 24 heures**
**Given** la blocklist active
**When** 24 heures se sont écoulées depuis la dernière mise à jour réussie
**Then** le client télécharge la nouvelle version en arrière-plan sans interrompre le filtrage actif (la liste existante reste en mémoire pendant le téléchargement)

**AC4 — Remplacement atomique de la liste**
**Given** une mise à jour de blocklist téléchargée
**When** le parsing réussit
**Then** la nouvelle liste remplace l'ancienne en mémoire de manière atomique (pas d'interruption de filtrage, pas de fenêtre sans liste)

**AC5 — Fallback sur liste existante en cas d'erreur**
**Given** le téléchargement de la blocklist échoue
**When** le réseau ou GitHub est inaccessible (erreur HTTP, timeout, erreur réseau)
**Then** la liste existante en mémoire reste active et une nouvelle tentative est planifiée au prochain tick du scheduler

## Tasks / Subtasks

- [x] **Task 1 : Créer `internal/blocklist/parser.go`** (AC: 1, 2)
  - [x] 1.1 Implémenter `parse(data []byte) map[string]struct{}` — itère ligne par ligne, extrait les domaines des lignes `0.0.0.0 <domaine>`, ignore commentaires (`#`), lignes vides, entrées `localhost`/`broadcasthost`
  - [x] 1.2 Trimmer les espaces, gérer `\r\n` et `\n`, ignorer les lignes où le domaine est vide ou `0.0.0.0` lui-même
  - [x] 1.3 Retourner `map[string]struct{}` — pas d'erreur retournée (la fonction est tolérant aux lignes mal formées, elle les ignore)

- [x] **Task 2 : Créer `internal/blocklist/parser_test.go`** (AC: 2)
  - [x] 2.1 `TestParse_BasicEntries` — lignes `0.0.0.0 ads.example.com`, `0.0.0.0 tracker.io`
  - [x] 2.2 `TestParse_IgnoresComments` — `# commentaire`, `0.0.0.0 # pas de domaine`
  - [x] 2.3 `TestParse_IgnoresLocalhost` — `127.0.0.1 localhost`, `0.0.0.0 0.0.0.0`
  - [x] 2.4 `TestParse_EmptyInput` — slice vide → map vide
  - [x] 2.5 `TestParse_CRLFLineEndings` — fichier Windows `\r\n`
  - [x] 2.6 `TestParse_RealWorldSample` — extrait de 10 lignes du vrai format StevenBlack

- [x] **Task 3 : Créer `internal/blocklist/downloader.go`** (AC: 1, 5)
  - [x] 3.1 Définir constante `blocklistURL = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"`
  - [x] 3.2 Implémenter `download(ctx context.Context, client *http.Client) ([]byte, error)` — GET avec contexte, timeout 30s, retourne erreur wrappée `blocklist: download: %w` si HTTP != 200
  - [x] 3.3 Lire le body avec `io.ReadAll` — le fichier fait ~800KB, pas de streaming nécessaire

- [x] **Task 4 : Créer `internal/blocklist/downloader_test.go`** (AC: 1, 5)
  - [x] 4.1 `TestDownload_Success` — httptest.Server retourne 200 + contenu hosts, vérifie contenu retourné
  - [x] 4.2 `TestDownload_HTTPError` — serveur retourne 404, vérifie erreur contenant "blocklist: download:"
  - [x] 4.3 `TestDownload_ContextCancelled` — contexte annulé avant réponse, vérifie erreur

- [x] **Task 5 : Créer `internal/blocklist/manager.go`** (AC: 1, 2, 3, 4, 5)
  - [x] 5.1 Définir type `Manager` avec champs : `httpClient *http.Client`, `mu sync.Mutex`, `domains map[string]struct{}` (liste active), `lastUpdate time.Time`, `interval time.Duration`, `running bool`, `cancel context.CancelFunc`, `done chan struct{}`
  - [x] 5.2 Implémenter `NewManager(interval time.Duration) *Manager` — construit avec `http.Client{Timeout: 30 * time.Second}`, interval par défaut `24 * time.Hour`
  - [x] 5.3 Implémenter `Start(ctx context.Context) error` — goroutine principale : télécharge immédiatement, puis ticker 24h ; pattern identique à `leakcheck/scheduler.go` ; retourne `ErrManagerAlreadyRunning` si déjà démarré
  - [x] 5.4 Implémenter goroutine interne : `select { case <-ticker.C: m.refresh(ctx) case <-ctx.Done(): return }`
  - [x] 5.5 Implémenter `refresh(ctx context.Context)` — télécharge via `download()`, parse via `parse()`, si succès → swap atomique via `m.mu.Lock(); m.domains = newDomains; m.lastUpdate = time.Now(); m.mu.Unlock()` ; si erreur → garde ancienne liste, silencieux
  - [x] 5.6 Implémenter `Stop()` — cancel + `<-done` (même pattern que `watchdog.go` et `scheduler.go`)
  - [x] 5.7 Implémenter `IsBlocked(domain string) bool` — lecture thread-safe : `m.mu.Lock(); defer m.mu.Unlock(); _, ok := m.domains[domain]; return ok`
  - [x] 5.8 Implémenter `IsReady() bool` — retourne `len(m.domains) > 0` (thread-safe) — utile pour Story 8.2
  - [x] 5.9 Définir `var ErrManagerAlreadyRunning = errors.New("blocklist: manager already running")`

- [x] **Task 6 : Créer `internal/blocklist/manager_test.go`** (AC: 1, 2, 3, 4, 5)
  - [x] 6.1 `TestManager_InitialDownload` — httptest.Server, Start → IsBlocked retourne true pour domaine présent
  - [x] 6.2 `TestManager_IsBlocked_NotInList` — domaine absent → false
  - [x] 6.3 `TestManager_AtomicSwap` — premier download charge liste A, deuxième download (ticker court) charge liste B → swap sans interruption
  - [x] 6.4 `TestManager_FallbackOnError` — premier download OK, deuxième download 500 → ancienne liste toujours active
  - [x] 6.5 `TestManager_StartStop` — Start puis Stop → goroutine bien terminée
  - [x] 6.6 `TestManager_StartAlreadyRunning` — Start deux fois → `ErrManagerAlreadyRunning`
  - [x] 6.7 `TestManager_IsReady_FalseBeforeDownload` — IsReady() == false avant premier download
  - [x] 6.8 `TestManager_ContextCancelled` — contexte annulé → Stop propre, pas de goroutine orpheline

- [x] **Task 7 : Modifier `internal/config/config.go`** (support AC1)
  - [x] 7.1 Ajouter struct `BlocklistConfig { Enabled bool \`toml:"enabled"\`; UpdateInterval string \`toml:"update_interval"\` }`
  - [x] 7.2 Ajouter champ `Blocklist BlocklistConfig \`toml:"blocklist"\`` dans `Config`
  - [x] 7.3 Dans `Load()`, default : `Blocklist: BlocklistConfig{Enabled: false, UpdateInterval: "24h"}` — **désactivé par défaut** (opt-in, Story 8.2 gère l'activation via tray)

- [x] **Task 8 : Intégration dans `internal/service/service.go`** (AC: 1, 3)
  - [x] 8.1 Ajouter champ `blocklistManager *blocklist.Manager` dans `Program`
  - [x] 8.2 Dans `run()` : si `cfg.Blocklist.Enabled` → créer `blocklist.NewManager(24*time.Hour)` et `go blocklistManager.Start(ctx)` ; stocker dans `p.blocklistManager`
  - [x] 8.3 Dans shutdown : si `p.blocklistManager != nil` → `p.blocklistManager.Stop()`
  - [x] 8.4 Exposer accesseur public `BlocklistManager() *blocklist.Manager` — requis par Story 8.2 pour injecter dans le DNS proxy

- [x] **Task 9 : Validation**
  - [x] 9.1 `go test ./internal/blocklist/...` — tous les tests passent
  - [x] 9.2 `go test ./internal/config/...` — pas de régression
  - [x] 9.3 `go test ./internal/service/...` — pas de régression
  - [x] 9.4 `go build ./cmd/client/... ./cmd/tray/...` — compilation OK

## Dev Notes

### Décision architecturale : Package `internal/blocklist/` séparé

Le filtrage DNS est **déjà en place** dans `internal/dns/proxy.go` via le pipeline `handleQuery → queryFunc`. La blocklist sera injectée dans ce pipeline en Story 8.2. Story 8.1 crée uniquement la fondation : téléchargement + parsing + gestion mémoire.

**Pourquoi `internal/blocklist/` et non `internal/dns/` ?**
- Évite d'alourdir le package `dns` qui gère déjà la résolution, le kill switch et le watchdog
- Permet d'injecter le `Manager` via interface en Story 8.2 sans import circulaire
- Cohérent avec le pattern du projet : `updater/`, `leakcheck/` sont des packages séparés pour des responsabilités distinctes

### Point d'intégration DNS (pour Story 8.2)

Le `Proxy` dans `internal/dns/proxy.go` a une méthode `handleQuery`. En Story 8.2, le filtrage sera ajouté **avant** l'appel à `p.queryFunc` :

```go
// Futur code Story 8.2 dans handleQuery :
if p.blocklist != nil && p.blocklist.IsBlocked(extractDomain(payload)) {
    // Retourner NXDOMAIN ou 0.0.0.0 localement, sans queryFunc
}
resp, err := p.queryFunc(ctx, payload)
```

**Story 8.1 ne touche PAS `proxy.go`** — uniquement la fondation téléchargement/parsing.

### Format de la blocklist StevenBlack/hosts

URL : `https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts`
Taille : ~800KB, ~260 000 domaines bloqués

Format typique :
```
# This hosts file is a merged collection of hosts from reputable sources,
# with a dash of crowd sourcing via Github

127.0.0.1 localhost
127.0.0.1 localhost.localdomain
...

# Start StevenBlack

0.0.0.0 0.0.0.0
0.0.0.0 ad.doubleclick.net
0.0.0.0 ads.google.com
0.0.0.0 tracker.io
```

**Règles de parsing :**
- Lignes commençant par `#` → ignorer
- Lignes vides → ignorer
- Lignes `0.0.0.0 <domaine>` → extraire le domaine (2ème champ)
- Ignorer si domaine == `0.0.0.0`, `localhost`, `broadcasthost`
- Supprimer commentaires inline (tout après `#` sur la même ligne)
- Gérer `\r\n` (fichier Windows) et `\n`

### Structure mémoire : `map[string]struct{}`

Choix justifié :
- `map[string]struct{}` → O(1) lookup, ~48 bytes overhead par entrée, ~12MB total pour 260K domaines
- Répond à NFR19 : latence additionnelle < 1ms (lookup map en Go : ~100-200ns)
- Un trie serait plus efficace mémoire pour les sous-domaines mais ajoute de la complexité inutile pour MVP
- **Pas de bibliothèque externe** — Go standard uniquement

### Remplacement atomique sans `sync/atomic.Pointer`

Utiliser `sync.Mutex` (déjà utilisé dans le projet) plutôt que `atomic.Pointer[map]` :
```go
// Atomic swap :
m.mu.Lock()
m.domains = newDomains  // pointer swap, instantané
m.lastUpdate = time.Now()
m.mu.Unlock()
```
La fenêtre de lock est nanosecondes — aucun impact sur la latence DNS.

### Pattern goroutine lifecycle (identique à `watchdog.go` et `scheduler.go`)

```go
// Start :
m.mu.Lock()
if m.running { m.mu.Unlock(); return ErrManagerAlreadyRunning }
ctx, m.cancel = context.WithCancel(ctx)
m.done = make(chan struct{})
m.running = true
m.mu.Unlock()

go func() {
    defer func() {
        m.mu.Lock()
        m.running = false
        m.cancel = nil
        close(m.done)
        m.mu.Unlock()
    }()
    m.refresh(ctx) // téléchargement initial immédiat
    ticker := time.NewTicker(m.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C: m.refresh(ctx)
        }
    }
}()

// Stop :
m.mu.Lock(); cancel := m.cancel; done := m.done; m.mu.Unlock()
if cancel != nil { cancel() }
if done != nil { <-done }
```

### Conventions de nommage (architecture.md)

- Package : `blocklist` (minuscule, un mot)
- Fichiers : `manager.go`, `parser.go`, `downloader.go` (snake_case)
- Types exportés : `Manager` (PascalCase)
- Constructeur : `NewManager` (New + TypeName)
- Erreur sentinelle : `ErrManagerAlreadyRunning`
- Méthodes exportées : `Start`, `Stop`, `IsBlocked`, `IsReady`
- Wrapping erreurs : `fmt.Errorf("blocklist: %w", err)`

### Config TOML après modification

```toml
[blocklist]
enabled = false
update_interval = "24h"
```

`enabled = false` par défaut — la fonctionnalité s'active via le menu tray en Story 8.2.

### Fichiers à créer

| Fichier | Action |
|---------|--------|
| `internal/blocklist/manager.go` | **Créer** — `Manager`, `Start`, `Stop`, `IsBlocked`, `IsReady` |
| `internal/blocklist/manager_test.go` | **Créer** — 8 tests unitaires |
| `internal/blocklist/parser.go` | **Créer** — `parse(data []byte) map[string]struct{}` |
| `internal/blocklist/parser_test.go` | **Créer** — 6 tests unitaires |
| `internal/blocklist/downloader.go` | **Créer** — `download(ctx, client) ([]byte, error)` |
| `internal/blocklist/downloader_test.go` | **Créer** — 3 tests unitaires |

### Fichiers à modifier

| Fichier | Modification |
|---------|-------------|
| `internal/config/config.go` | Ajouter `BlocklistConfig` + champ `Blocklist` dans `Config`, default `enabled=false` |
| `internal/service/service.go` | Ajouter champ `blocklistManager`, démarrage conditionnel dans `run()`, accesseur `BlocklistManager()` |

### Fichiers à NE PAS toucher

- `internal/dns/proxy.go` — l'intégration de filtrage est Story 8.2
- `internal/dns/manager.go` — non concerné
- `internal/dns/kill_switch.go` — non concerné
- `internal/tray/tray.go` — le toggle tray est Story 8.2
- `internal/ipc/messages.go` — aucun nouveau message IPC dans cette story
- `internal/ipchandler/handler.go` — non concerné
- `internal/tunnel/` — non concerné
- `internal/watchdog/` — non concerné
- `internal/updater/` — non concerné
- `internal/leakcheck/` — non concerné

### Contexte de la story précédente (7.2)

La story 7.2 a établi :
- Le pattern `PeriodicScheduler` Start/Stop avec interfaces — **adopter exactement ce pattern** pour `Manager`
- Les interfaces locales pour découplage (`KillSwitchQuerier`, `TunnelStateQuerier`) — même approche si nécessaire en Story 8.2
- Les corrections post-review (constantes au lieu de chaînes magiques) — **utiliser des constantes dès le départ**
- `go leakScheduler.Start(ctx)` dans `service.go` — même pattern pour `blocklistManager.Start(ctx)`

**Leçons appliquées :**
- Constantes pour les URLs et valeurs répétées (pas de chaînes magiques)
- Tests déterministes avec `interval` court (1ms/10ms) pour les tests de scheduler
- Ne pas utiliser `time.Sleep` dans les tests — utiliser des channels ou intervalles courts

### Informations techniques

**`net/http` standard** — aucune bibliothèque externe nécessaire pour le téléchargement HTTP.

**`http.Client` avec timeout** :
```go
httpClient: &http.Client{Timeout: 30 * time.Second}
```

**Gestion du context dans `download()`** :
```go
req, err := http.NewRequestWithContext(ctx, http.MethodGet, blocklistURL, nil)
```

**Taille attendue** : ~800KB, 260 000+ domaines, ~12MB en mémoire map.

**Vérification de la réponse HTTP** :
```go
if resp.StatusCode != http.StatusOK {
    return nil, fmt.Errorf("blocklist: download: HTTP %d", resp.StatusCode)
}
```

### Project Structure Notes

- `internal/blocklist/` — nouveau package, aucun conflit avec packages existants
- Pas d'import circulaire : `blocklist` n'importe pas `dns`, `tunnel`, `ipc` — sens unique (service → blocklist)
- Le `Manager` sera accessible depuis `service.go` via `p.blocklistManager` et exposé par `BlocklistManager()` pour injection en Story 8.2

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 8, Story 8.1]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Code Organization" : structure `internal/`]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Naming Patterns" : packages, fichiers, types]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Concurrency Patterns" : context.Context, sync.Mutex, goroutines sans orphelins]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Error Handling Patterns" : wrapping, préfixe package]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Anti-Patterns : Aucun log côté client"]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "NFR19 : blocklist DNS < 1ms latence additionnelle"]
- [Source: `internal/dns/proxy.go` — pipeline handleQuery, point d'intégration futur Story 8.2]
- [Source: `internal/config/config.go` — pattern ajout config TOML avec defaults dans Load()]
- [Source: `internal/service/service.go` — pattern champ + Start dans run() + accesseur public]
- [Source: `internal/leakcheck/scheduler.go` — pattern Start/Stop/done/cancel]
- [Source: `internal/watchdog/watchdog.go` — pattern goroutine lifecycle identique]
- [Source: story 7-2 Dev Notes — leçons : constantes, tests déterministes, pattern interfaces locales]
- [Source: `go.mod` — modules disponibles : stdlib net/http, sync, io — aucune lib externe à ajouter]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

Aucun blocage rencontré. Implémentation directe suivant les spécifications de la story.

### Completion Notes List

- Créé package `internal/blocklist/` avec 3 fichiers source et 3 fichiers de tests (17 tests au total, tous verts).
- `parser.go` : fonction `parse()` tolérante aux lignes malformées, gestion `\r\n`/`\n`, filtrage localhost/broadcasthost/0.0.0.0.
- `downloader.go` : `download()` + `downloadFrom()` (testabilité), wrapping d'erreur `blocklist: download: %w`, limite `io.LimitReader` 10MB.
- `manager.go` : pattern goroutine lifecycle identique à `leakcheck/scheduler.go` — `Start`/`Stop`/`done`/`cancel`, swap atomique via `sync.RWMutex`, `IsBlocked` O(1) avec `RLock`, `IsReady()` pour Story 8.2.
- `config.go` : ajout `BlocklistConfig` avec `enabled=false` par défaut (opt-in).
- `service.go` : champ `blocklistManager`, démarrage conditionnel dans `run()`, arrêt dans `shutdown()`, accesseur `BlocklistManager()` pour injection Story 8.2.
- `cmd/client/main.go` : câblage `BlocklistEnabled`/`BlocklistInterval` depuis config TOML vers `svc.Config`.
- Aucune bibliothèque externe ajoutée — stdlib Go uniquement.

**Corrections post-revue (code-review) :**
- ✅ CRIT : `cmd/client/main.go` — câblage blocklist config ajouté (était manquant, feature toujours désactivée sinon)
- ✅ MED : `manager.go` — `sync.Mutex` → `sync.RWMutex` pour `IsBlocked`/`IsReady` (lectures concurrentes sans contention)
- ✅ MED : `downloader.go` — `io.LimitReader(resp.Body, 10MB)` ajouté contre OOM
- ✅ MED : `manager_test.go` — `time.Sleep(200ms)` remplacé par channel synchronization dans `TestManager_FallbackOnError`
- ✅ MED : `newManagerWithURL` déplacée de `manager.go` vers `manager_test.go`

**Corrections post-revue #2 (code-review 2026-03-12) :**
- ✅ HIGH : `manager.go` — `refresh()` protégé contre swap atomique avec map vide (contenu invalide du serveur → garde l'ancienne liste, conforme AC5)
- ✅ HIGH : `downloader.go` — suppression de `download()` (code mort, seul `downloadFrom()` était utilisé)
- ✅ HIGH : `manager.go` — `IsReady()` utilise `!m.lastUpdate.IsZero()` au lieu de `len(m.domains) > 0` (correct après download réussi d'une liste vide)
- ✅ MED : `manager_test.go` — `TestManager_AtomicSwap` vérifie maintenant que l'ancienne liste est bien remplacée (pas mergée)
- ✅ MED : `downloader.go` — détection réponse tronquée si `len(data) >= maxBodyBytes` (10MB)
- ✅ MED : `service.go` — erreur de `blMgr.Start()` capturée dans goroutine (cohérent avec leakScheduler)
- ✅ LOW : `downloader.go` — body HTTP drainé avant close sur réponse non-200 (réutilisation connexion TCP)
- ✅ LOW : `manager_test.go` — helpers `waitReady`/`waitBlocked` extraits pour éliminer la duplication de polling

### File List

- `internal/blocklist/parser.go` — **Créé**
- `internal/blocklist/parser_test.go` — **Créé**
- `internal/blocklist/downloader.go` — **Créé**
- `internal/blocklist/downloader_test.go` — **Créé**
- `internal/blocklist/manager.go` — **Créé**
- `internal/blocklist/manager_test.go` — **Créé**
- `internal/config/config.go` — **Modifié** (ajout `BlocklistConfig`)
- `internal/service/service.go` — **Modifié** (intégration `blocklistManager`)
- `cmd/client/main.go` — **Modifié** (câblage `BlocklistEnabled`/`BlocklistInterval`)
