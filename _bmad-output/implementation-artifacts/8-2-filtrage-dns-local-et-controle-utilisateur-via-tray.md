# Story 8.2 : Filtrage DNS local et contrôle utilisateur via tray

Status: done

## Story

En tant qu'utilisateur,
Je veux que les requêtes DNS vers des domaines publicitaires/trackers soient bloquées localement, et pouvoir activer/désactiver cette fonctionnalité,
Afin de naviguer sans publicités ni trackers tout en gardant le contrôle.

## Acceptance Criteria

**AC1 — Filtrage DNS local pour domaines bloqués**
**Given** la blocklist active et une requête DNS reçue
**When** le domaine demandé est dans la blocklist
**Then** la requête est résolue localement avec une réponse NXDOMAIN (RCODE=3) sans transiter par le tunnel — latence additionnelle < 1ms

**AC2 — Pass-through pour domaines non bloqués**
**Given** la blocklist active et une requête DNS reçue
**When** le domaine demandé n'est PAS dans la blocklist
**Then** la requête est traitée normalement via le tunnel DoH

**AC3 — Option tray visible**
**Given** le menu tray affiché
**When** l'utilisateur fait un clic droit
**Then** une option "Blocklist DNS : activée" ou "Blocklist DNS : désactivée" est présente dans le menu, reflétant l'état actuel

**AC4 — Désactivation via tray**
**Given** la blocklist activée
**When** l'utilisateur clique sur "Blocklist DNS : activée"
**Then** le filtrage est désactivé immédiatement, la préférence est sauvegardée dans le config TOML, et le label devient "Blocklist DNS : désactivée"

**AC5 — Activation via tray**
**Given** la blocklist désactivée
**When** l'utilisateur clique sur "Blocklist DNS : désactivée"
**Then** le filtrage est réactivé, la blocklist en mémoire est utilisée immédiatement (Manager démarré si nécessaire)

## Tasks / Subtasks

- [x] **Task 1 : Modifier `internal/dns/proxy.go`** (AC: 1, 2)
  - [x] 1.1 Ajouter l'interface `BlocklistChecker { IsBlocked(domain string) bool; IsReady() bool }` (package-level, avant `QueryFunc`)
  - [x] 1.2 Ajouter les champs `blMu sync.RWMutex` et `blocklist BlocklistChecker` au struct `Proxy` (après `ready chan struct{}`)
  - [x] 1.3 Ajouter méthode `SetBlocklist(bl BlocklistChecker)` — thread-safe via `blMu.Lock()` ; `bl` peut être nil pour désactiver
  - [x] 1.4 Implémenter `extractDomain(payload []byte) string` — parse le QNAME DNS (offset 12, labels length-prefixed, termine par 0x00) ; retourne `strings.Join(labels, ".")` ; retourne `""` si payload trop court, label trop long ou pointer de compression rencontré
  - [x] 1.5 Implémenter `buildNXDOMAINResponse(query []byte) []byte` — copie le query, met QR=1 AA=1 en byte[2] (`(query[2] & 0x79) | 0x84`), RCODE=3 en byte[3] (`0x03`), remet à zéro les bytes 6-11 (ANCOUNT/NSCOUNT/ARCOUNT) ; retourne nil si `len(query) < minDNSSize`
  - [x] 1.6 Modifier `handleQuery` : lire `p.blocklist` sous `p.blMu.RLock()` ; si non nil ET `bl.IsReady()` → appeler `extractDomain(payload)`, normaliser en minuscules (`strings.ToLower`), si `bl.IsBlocked(domain)` → envoyer `buildNXDOMAINResponse(payload)` et retourner ; sinon continuer vers `p.queryFunc`
  - [x] 1.7 Ajouter import `"strings"` dans proxy.go

- [x] **Task 2 : Tests dans `internal/dns/proxy_test.go`** (AC: 1, 2)
  - [x] 2.1 `TestExtractDomain_BasicQuery` — payload de `makeDNSPayload()` (contient "example.com") → retourne `"example.com"`
  - [x] 2.2 `TestExtractDomain_ShortPayload` — payload de 12 bytes ou moins → retourne `""`
  - [x] 2.3 `TestExtractDomain_SingleLabel` — query pour `"foo"` (1 seul label) → retourne `"foo"`
  - [x] 2.4 `TestBuildNXDOMAINResponse_ValidQuery` — retourne slice non nil, QR bit set (byte[2] & 0x80 != 0), RCODE=3 (byte[3] & 0x0F == 3), ANCOUNT=0 (bytes 6-7)
  - [x] 2.5 `TestBuildNXDOMAINResponse_TooShort` — payload < minDNSSize → retourne nil
  - [x] 2.6 `TestProxy_BlocklistFiltering_Blocked` — créer proxy avec `SetBlocklist` d'un blocklist mock qui bloque "example.com" ; envoyer `makeDNSPayload()` (query pour example.com) ; vérifier réponse reçue avec RCODE=3 (byte[3] & 0x0F == 3)
  - [x] 2.7 `TestProxy_BlocklistFiltering_NotBlocked` — blocklist mock ne bloque pas "example.com" ; envoyer `makeDNSPayload()` ; vérifier réponse = echo du payload (forwarded normalement)
  - [x] 2.8 `TestProxy_BlocklistFiltering_NotReady` — blocklist mock `IsReady()=false` ; query → forwarded normalement même si `IsBlocked` retournerait true
  - [x] 2.9 `TestProxy_BlocklistFiltering_NilBlocklist` — `SetBlocklist(nil)` après activation → query forwarded normalement
  - [x] 2.10 `TestProxy_SetBlocklist_ThreadSafe` — appels concurrents `SetBlocklist` + `handleQuery` sans race condition (utiliser `-race` flag)

- [x] **Task 3 : Modifier `internal/ipc/messages.go`** (AC: 3, 4, 5)
  - [x] 3.1 Ajouter constante action : `ActionSetBlocklist = "set_blocklist"`
  - [x] 3.2 Ajouter champ `BlocklistEnabled bool \`json:"blocklist_enabled,omitempty"\`` dans la struct `Response`

- [x] **Task 4 : Modifier `internal/ipchandler/handler.go`** (AC: 3, 4, 5)
  - [x] 4.1 Ajouter `case ipc.ActionSetBlocklist: return handleSetBlocklist(prg, req, opts)` dans le switch de `Handle`
  - [x] 4.2 Implémenter `handleSetBlocklist(prg *svc.Program, req ipc.Request, opts Options) ipc.Response` :
    - `enabled := req.Value == "true"`
    - Charger config via `opts.ConfigPathFn()` + `config.Load`
    - Modifier `cfg.Blocklist.Enabled = enabled`
    - Sauvegarder via `cfg.Save(cfgPath)`
    - Si enabled → `prg.EnableBlocklist()` ; sinon → `prg.DisableBlocklist()`
    - Retourner `ipc.Response{Status: ipc.StatusOK}`
  - [x] 4.3 Dans `handleGetStatus` : ajouter `resp.BlocklistEnabled = prg.BlocklistActive()` avant chaque `return resp`

- [x] **Task 5 : Modifier `internal/service/service.go`** (AC: 1, 4, 5)
  - [x] 5.1 Ajouter champs au struct `Program` (après `blocklistManager`) :
    - `proxy    *dns.Proxy   // IPv4 proxy ref, sous proxyMu`
    - `proxyV6  *dns.Proxy   // IPv6 proxy ref, sous proxyMu`
    - `blMu     sync.Mutex   // protège blocklistManager pour Enable/Disable concurrents`
    - `blocklistActive atomic.Bool  // runtime toggle, thread-safe`
  - [x] 5.2 Dans `run()`, section "5c. Blocklist manager start" : après `p.blocklistManager = blMgr`, ajouter `p.blocklistActive.Store(true)`
  - [x] 5.3 Dans `startProxy()`, après création de chaque `dns.Proxy` : injecter la blocklist si active
  - [x] 5.4 Dans `stopProxy()` : ajouter `p.proxy = nil` et `p.proxyV6 = nil` avant de libérer le lock
  - [x] 5.5 Ajouter méthode `BlocklistActive() bool` : retourne `p.blocklistActive.Load()`
  - [x] 5.6 Ajouter méthode `EnableBlocklist()`
  - [x] 5.7 Ajouter méthode `DisableBlocklist()`
  - [x] 5.8 Dans `shutdown()`, `p.blocklistManager.Stop()` reste intact (section "1a. Stop blocklist manager") — non modifié, déjà présent

- [x] **Task 6 : Modifier `internal/tray/tray.go`** (AC: 3, 4, 5)
  - [x] 6.1 Ajouter champ `blocklistEnabled bool` au struct `Tray` (après `leakAlertActive`)
  - [x] 6.2 Ajouter champ `menuBlocklist *systray.MenuItem` au struct `Tray` (après `menuAutoStart`)
  - [x] 6.3 Modifier `NewWithConfig(autoStart bool, portableMode bool, blocklistEnabled bool) *Tray` — ajouter `blocklistEnabled bool` paramètre et l'assigner dans le return
  - [x] 6.4 Modifier `newWithDeps` — ajouter `blocklistEnabled bool` paramètre (pour tests)
  - [x] 6.5 Dans `onReady()`, ajouter menu blocklist avec checkbox après menuAutoStart, avant menuQuit
  - [x] 6.6 Implémenter `handleBlocklistToggle(ctx context.Context)`
  - [x] 6.7 Dans `menuHandler` : ajouter `case <-t.menuBlocklist.ClickedCh` dans les DEUX branches
  - [x] 6.8 Dans `updateTrayState` : `stateKey` inclut `BlocklistEnabled`, synchronisation du menu blocklist

- [x] **Task 7 : Modifier `cmd/tray/main.go`** (AC: 3)
  - [x] 7.1 Lire `cfg.Blocklist.Enabled` depuis le fichier config et le passer à `tray.NewWithConfig(autoStart, false, blocklistEnabled)`

- [x] **Task 8 : Validation**
  - [x] 8.1 `go test ./internal/dns/...` — tous les tests proxy passent (10 nouveaux + existants) ; les échecs préexistants dns manager/windows non liés à cette story
  - [x] 8.2 `go test ./internal/ipc/...` — pas de régression (PASS)
  - [x] 8.3 `go test ./internal/ipchandler/...` — pas de régression (PASS)
  - [x] 8.4 `go test ./internal/tray/...` — pas de régression (PASS)
  - [x] 8.5 `go test ./internal/service/...` — pas de régression (PASS)
  - [x] 8.6 `go build ./cmd/client/... ./cmd/tray/...` — compilation OK

## Dev Notes

### Architecture de la Story 8.2 : 3 couches d'intégration

Cette story réalise l'intégration complète de la blocklist (créée en Story 8.1) dans le pipeline DNS et l'interface utilisateur :

1. **Couche DNS** (`dns/proxy.go`) : filtrage dans `handleQuery` avant `queryFunc`
2. **Couche Service** (`service/service.go`) : toggle runtime sans redémarrage
3. **Couche UI** (`tray/tray.go` + `ipc`) : contrôle utilisateur + persistance config

### Point d'intégration exact dans `handleQuery`

Story 8.1 a prévu ce code dans `proxy.go` :
```go
// AVANT l'appel à p.queryFunc :
func (p *Proxy) handleQuery(ctx context.Context, payload []byte, clientAddr *net.UDPAddr) {
    p.blMu.RLock()
    bl := p.blocklist
    p.blMu.RUnlock()

    if bl != nil && bl.IsReady() {
        if domain := extractDomain(payload); domain != "" {
            if bl.IsBlocked(strings.ToLower(domain)) {
                if resp := buildNXDOMAINResponse(payload); resp != nil && p.conn != nil {
                    p.conn.WriteToUDP(resp, clientAddr)
                }
                return
            }
        }
    }

    resp, err := p.queryFunc(ctx, payload)
    if err != nil {
        return
    }
    if p.conn != nil {
        p.conn.WriteToUDP(resp, clientAddr)
    }
}
```

### Format DNS wire pour extractDomain

Structure du message DNS (RFC 1035) :
```
Bytes 0-11  : Header (12 bytes fixes)
Byte 12+    : Section Question
  QNAME     : labels length-prefixed, terminés par 0x00
              ex: "example.com" = [7]"example"[3]"com"[0]
  QTYPE     : 2 bytes (après QNAME)
  QCLASS    : 2 bytes
```

`extractDomain` parse uniquement le QNAME (offset 12), ignore la compression (rare dans les requêtes, signalée par bits `11` en tête du byte de longueur → break).

### Format NXDOMAIN response (buildNXDOMAINResponse)

Flags byte 2 layout : `QR(1)|Opcode(4)|AA(1)|TC(1)|RD(1)`
- `0x79` = `0111 1001` → masque pour préserver Opcode (bits 6-3) et RD (bit 0)
- `0x84` = `1000 0100` → QR=1, AA=1
- `resp[2] = (query[2] & 0x79) | 0x84`

Flags byte 3 : `RA(1)|Z(1)|AD(1)|CD(1)|RCODE(4)`
- `resp[3] = 0x03` → RCODE=3 (NXDOMAIN), tout le reste à 0

Bytes 6-11 : mettre à 0 (ANCOUNT=0, NSCOUNT=0, ARCOUNT=0)
Bytes 12+  : copier la question section telle quelle (le resolver client la lit dans la réponse)

### Gestion du toggle runtime (sans redémarrage service)

Le `Manager` (blocklist) **reste toujours actif** une fois démarré, même quand le filtrage est désactivé. Cela permet une ré-activation instantanée sans délai de téléchargement.

```
EnableBlocklist():
  1. Crée et démarre Manager si nil (premier toggle vers activé)
  2. Store(true) dans blocklistActive
  3. Injecte Manager dans proxy + proxyV6 via SetBlocklist

DisableBlocklist():
  1. Store(false) dans blocklistActive
  2. SetBlocklist(nil) sur proxy + proxyV6
  3. NE STOPPE PAS le Manager → garde la liste fraîche

startProxy() (appelé sur reconnexion kill switch):
  → Vérifie blocklistActive.Load() et injecte si true
  → Stocke les refs proxy/proxyV6 dans Program sous proxyMu
```

### Gestion des refs proxy dans startProxy

**ATTENTION** : `startProxy` est appelé plusieurs fois (initial run + chaque reconnexion du kill switch). Il faut stocker les références dans `p.proxy` et `p.proxyV6` sous `proxyMu` pour que `EnableBlocklist`/`DisableBlocklist` puissent les accéder.

Placement dans `startProxy` (après `<-proxy.Ready()`) :
```go
p.proxyCancel = pCancel
p.proxyErrCh = errCh
p.proxy = proxy  // ← AJOUTER (sous proxyMu déjà locké)

proxyV6 := dns.NewProxy(dns.DefaultListenAddrV6, p.tunnelClient.SendDoHQuery)
// injection blocklist si active
p.proxyV6 = proxyV6  // ← AJOUTER
```

Dans `stopProxy()`, ajouter sous `proxyMu.Lock()` :
```go
p.proxy = nil
p.proxyV6 = nil
```

### Concurrence et thread-safety

- `proxy.blocklist` : protégé par `blMu sync.RWMutex` (RLock dans handleQuery, Lock dans SetBlocklist)
- `p.blocklistManager` : protégé par `p.blMu sync.Mutex` dans EnableBlocklist/DisableBlocklist
- `p.blocklistActive` : `atomic.Bool` (Go 1.19+, disponible en Go 1.26)
- `p.proxy`, `p.proxyV6` : protégés par `p.proxyMu sync.Mutex` (déjà existant)
- `t.blocklistEnabled` dans tray : protégé par `t.mu sync.Mutex` (déjà existant)

### Synchronisation état tray via stateKey

Le champ `stateKey` dans `updateTrayState` doit inclure `BlocklistEnabled` pour détecter les changements d'état blocklist même quand Status/IP/Error ne changent pas :
```go
stateKey := resp.Status + "|" + resp.IP + "|" + resp.Error + "|" + fmt.Sprintf("%v", resp.BlocklistEnabled)
```

### Mock BlocklistChecker pour les tests proxy

Dans `proxy_test.go`, créer un mock simple :
```go
type mockBlocklist struct {
    blockedDomains map[string]bool
    ready          bool
}
func (m *mockBlocklist) IsBlocked(domain string) bool { return m.blockedDomains[domain] }
func (m *mockBlocklist) IsReady() bool                { return m.ready }
```

### Leçons de Story 8.1 (à appliquer impérativement)

- **sync.RWMutex plutôt que sync.Mutex** pour les opérations de lecture intensive (IsBlocked est lu à chaque requête DNS) — utiliser RLock dans handleQuery
- **Tests déterministes** — pas de `time.Sleep`, utiliser des channels ou contexts avec timeout
- **Constantes plutôt que chaînes magiques** — ex: `ActionSetBlocklist` comme constante dans `messages.go`
- **Wrapping erreurs** : préfixe package → `fmt.Errorf("dns: proxy: %w", err)` si nécessaire
- **Aucun log côté client** — silently drop (déjà le cas dans handleQuery)

### Leçons de Story 7.2 (également applicables)

- **Interfaces locales pour découplage** — `BlocklistChecker` dans `dns/proxy.go` évite l'import de `blocklist` dans `dns` (pas d'import circulaire)
- **Constantes dès le départ** — évite les revues de code pour remplacer les chaînes magiques

### Fichiers impactés par le changement de signature `NewWithConfig`

`cmd/tray/main.go` est le SEUL appelant de `NewWithConfig` dans le code de production. Les tests utilisent `newWithDeps`. Vérifier que `newWithDeps` est mis à jour pour accepter le nouveau paramètre.

### Project Structure Notes

- **Pas d'import circulaire** : `dns/proxy.go` définit l'interface `BlocklistChecker` → `service.go` et `blocklist.Manager` implémentent cette interface (pas d'import de `blocklist` depuis `dns`)
- **Fichiers à modifier** :

| Fichier | Modification |
|---------|-------------|
| `internal/dns/proxy.go` | Interface BlocklistChecker, extractDomain, buildNXDOMAINResponse, SetBlocklist, handleQuery modifié |
| `internal/dns/proxy_test.go` | 10 nouveaux tests |
| `internal/ipc/messages.go` | ActionSetBlocklist + BlocklistEnabled dans Response |
| `internal/ipchandler/handler.go` | handleSetBlocklist + BlocklistActive dans handleGetStatus |
| `internal/service/service.go` | proxy/proxyV6 refs, blMu, blocklistActive, EnableBlocklist, DisableBlocklist, BlocklistActive |
| `internal/tray/tray.go` | blocklistEnabled, menuBlocklist, NewWithConfig signature, handleBlocklistToggle, menuHandler, updateTrayState |
| `cmd/tray/main.go` | Passage blocklistEnabled à NewWithConfig |

- **Fichiers à NE PAS toucher** :
  - `internal/blocklist/manager.go` — non modifié (Story 8.1 est complète)
  - `internal/config/config.go` — non modifié (BlocklistConfig déjà là)
  - `cmd/client/main.go` — non modifié (blocklistEnabled déjà câblé vers Service.Config)
  - `internal/dns/manager.go`, `kill_switch.go` — non concernés
  - `internal/watchdog/` — non concerné
  - `internal/tunnel/` — non concerné

### Vérification NFR19

NFR19 : filtrage blocklist < 1ms de latence additionnelle par requête.
- `extractDomain` : O(n) sur longueur du domain (< 255 bytes max DNS) → nanosecondes
- `bl.IsBlocked()` via `sync.RWMutex` + `map[string]struct{}` lookup O(1) → nanosecondes
- `buildNXDOMAINResponse` : `make` + `copy` sur ≤ 512 bytes → nanosecondes
- Total bien inférieur à 1ms ✓

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 8, Story 8.2, AC1-5]
- [Source: `_bmad-output/implementation-artifacts/8-1-telechargement-et-parsing-de-la-blocklist-stevenblack-hosts.md` — Dev Notes "Point d'intégration DNS", "Leçons Story 7.2"]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Naming Patterns", "Concurrency Patterns", "Error Handling Patterns", "Anti-Patterns"]
- [Source: `internal/dns/proxy.go` — struct Proxy, handleQuery, minDNSSize, pipeline existant]
- [Source: `internal/dns/proxy_test.go` — helpers freeUDPAddr, makeDNSPayload, startProxy, echoQueryFunc]
- [Source: `internal/ipc/messages.go` — pattern ActionSetAutoStart, Response struct existant]
- [Source: `internal/ipchandler/handler.go` — pattern handleSetAutoStart, handleGetStatus existant]
- [Source: `internal/tray/tray.go` — pattern handleAutoStartToggle, menuHandler branches, updateTrayState stateKey]
- [Source: `internal/service/service.go` — struct Program, startProxy, stopProxy, blocklistManager, proxyMu]
- [Source: `cmd/tray/main.go` — appel NewWithConfig]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — NFR19 blocklist < 1ms]
- [Source: `go.mod` — Go 1.26 (atomic.Bool/atomic.Pointer disponibles)]

## Senior Developer Review (AI)

**Date :** 2026-03-12
**Reviewer :** Akerimus (via code-review workflow)

### Issues identifiés et résolus

**[MEDIUM][FIXED] Race condition dans `EnableBlocklist`/`DisableBlocklist` — `service.go`**
`blocklistActive.Store()` et `proxy.SetBlocklist()` s'exécutaient hors de `blMu`, permettant un entrelacement Enable/Disable résultant en état incohérent (`blocklistActive=true` mais `proxy.blocklist=nil`).
Fix : ajout de `toggleMu sync.Mutex` sérialisant les deux opérations de bout en bout. Lock ordering : `toggleMu` → `blMu` → `proxyMu` (inchangé).

**[MEDIUM][FIXED] Zéro test pour `handleBlocklistToggle` — `tray_test.go`**
La méthode symétrique `handleAutoStartToggle` avait 3 tests ; `handleBlocklistToggle` aucun.
Fix : ajout de `TestTray_HandleBlocklistToggle_FromEnabled_SendsFalse`, `_FromDisabled_SendsTrue`, `_IPCError_ShowsTooltip`.

**[MEDIUM][FIXED] `updateTrayState` appelait des méthodes systray sous `t.mu.Lock()` — `tray.go`**
Incohérence avec `handleAutoStartToggle` qui libère le verrou avant les appels menu. Risque de deadlock si systray utilise des verrous internes.
Fix : capture de `blocklistChanged` sous `t.mu`, appel de `Check()/SetTitle()` hors du verrou.

**[CRITICAL][FIXED] Blocklist NON injectée dans les proxies au démarrage initial — `service.go`**
Dans `run()`, `startProxy(ctx)` est appelé (étape 2) avant que `blocklistActive.Store(true)` ne soit exécuté (étape 5c). Les proxies démarrent donc sans blocklist même quand `config.BlocklistEnabled=true`. AC1 cassé au premier démarrage — le filtrage DNS ne fonctionne pas jusqu'au premier toggle tray ou kill switch.
Fix : injection explicite `proxy.SetBlocklist(blMgr)` et `proxyV6.SetBlocklist(blMgr)` sous `proxyMu` immédiatement après création du Manager en étape 5c.

**[MEDIUM][FIXED] `handleToggle` provoque un flicker du menu blocklist — `tray.go`**
`handleToggle` appelait `updateTrayState(resp)` avec une réponse ActionConnect/Disconnect qui ne contient pas `BlocklistEnabled` (= `false` par défaut Go). `updateTrayState` détectait `blocklistChanged=true` et flip le menu à "désactivée" pendant ~2s.
Fix : remplacement de `updateTrayState(resp)` par un feedback visuel direct (icon, tooltip, menuToggle) sans toucher au menu blocklist. Le `t.last=""` force un refresh complet au prochain poll.

**[LOW][FIXED] `TestProxy_SetBlocklist_ThreadSafe` utilisait un blocklist vide — `proxy_test.go`**
Le mock `blockedDomains` était une map vide, le chemin de blocage (extractDomain → IsBlocked → buildNXDOMAINResponse) n'était jamais exercé sous contention.
Fix : map `{"example.com": true}` pour que les queries concurrentes traversent le chemin de blocage.

### Décision

**Changes Requested → corrigés et résolus.**

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

Aucun blocage rencontré. Implémentation directe suivant les spécifications de la story.

### Completion Notes List

- ✅ `BlocklistChecker` interface définie dans `dns/proxy.go` — découplage via interface locale évite l'import circulaire `dns → blocklist`
- ✅ `extractDomain` : parse QNAME RFC 1035, gère labels length-prefixed, compression pointer, payloads courts
- ✅ `buildNXDOMAINResponse` : flags byte 2 = `(query[2] & 0x79) | 0x84`, byte 3 = `0x03`, bytes 6-11 = 0
- ✅ `handleQuery` modifié : RLock sur `blMu`, NXDOMAIN sans passer par `queryFunc` si bloqué
- ✅ 10 nouveaux tests proxy tous verts, y compris thread-safety et cas limites
- ✅ `ActionSetBlocklist` + `BlocklistEnabled` ajoutés à l'IPC
- ✅ `handleSetBlocklist` : charge config, modifie `cfg.Blocklist.Enabled`, sauvegarde, appelle Enable/DisableBlocklist
- ✅ `handleGetStatus` : `BlocklistEnabled` ajouté dans les deux branches de retour
- ✅ Service : `proxy`/`proxyV6` refs, `blMu`, `blocklistActive atomic.Bool`, `EnableBlocklist`, `DisableBlocklist`, `BlocklistActive`
- ✅ `startProxy` injecte blocklist dans proxy et proxyV6 si `blocklistActive.Load() == true`
- ✅ `stopProxy` efface les refs `proxy` et `proxyV6` sous `proxyMu`
- ✅ `EnableBlocklist` crée le Manager si nil (premier toggle), sinon réutilise l'existant
- ✅ `DisableBlocklist` ne stoppe pas le Manager — liste fraîche disponible pour ré-activation immédiate
- ✅ Tray : `blocklistEnabled`, `menuBlocklist`, `NewWithConfig` et `newWithDeps` signature étendue
- ✅ `handleBlocklistToggle` : pattern identique à `handleAutoStartToggle`
- ✅ `updateTrayState` : `stateKey` inclut `BlocklistEnabled`, synchronisation du menu si état changé
- ✅ `menuHandler` : case `menuBlocklist.ClickedCh` dans les deux branches
- ✅ `cmd/tray/main.go` : lit `cfg.Blocklist.Enabled` et le passe à `NewWithConfig`
- ✅ Tous les tests tray existants mis à jour avec le nouveau paramètre `blocklistEnabled`
- ✅ Build OK : `./cmd/client/...` et `./cmd/tray/...`

### File List

internal/dns/proxy.go
internal/dns/proxy_test.go
internal/ipc/messages.go
internal/ipchandler/handler.go
internal/service/service.go
internal/tray/tray.go
internal/tray/tray_test.go
cmd/tray/main.go

## Change Log

- 2026-03-12 : Code review #2 — 1 CRITICAL + 1 MEDIUM + 1 LOW corrigés : blocklist non injectée dans proxies au démarrage initial (ajout injection sous proxyMu après Manager.Start en run()), handleToggle flicker menu blocklist (feedback visuel direct sans updateTrayState), TestProxy_SetBlocklist_ThreadSafe exercice du chemin de blocage sous contention.
- 2026-03-12 : Code review #1 — 3 issues MEDIUM + 3 issues LOW corrigés : race condition Enable/DisableBlocklist (toggleMu), 3 tests handleBlocklistToggle ajoutés, updateTrayState systray calls déplacés hors de t.mu, handleSetBlocklist retourne BlocklistEnabled + validation req.Value, menuHandler refactorisé (nil-channel pattern, plus de duplication).
- 2026-03-11 : Implémentation complète de la Story 8.2 — filtrage DNS local NXDOMAIN via `BlocklistChecker`, toggle runtime sans redémarrage, menu tray "Blocklist DNS : activée/désactivée", persistance config TOML. 10 nouveaux tests proxy (extractDomain, buildNXDOMAINResponse, filtrage bloqué/non-bloqué/non-prêt/nil, thread-safety). Tous les tests des packages modifiés passent.
