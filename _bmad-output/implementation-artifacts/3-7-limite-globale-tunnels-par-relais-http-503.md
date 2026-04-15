# Story 3.7: Limite globale tunnels par relais (HTTP 503)

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a opérateur de relais,
I want limiter le nombre total de tunnels `/tunnel` simultanés à 150 par relais et exposer ce compteur dans `/health`,
So that la capacité de chaque VPS reste bornée (RAM/CPU prévisibles) et que les clients basculent vers un autre relais via le failover (Epic 4) plutôt que de saturer un seul nœud.

## Acceptance Criteria

1. **Given** une constante exportée `relay.MaxTunnels int64 = 150` est définie, **When** le code du package `relay` est compilé, **Then** cette constante est l'unique source de vérité pour le plafond global de tunnels (aucun magic number `150` ailleurs dans `internal/relay/` ni `cmd/relay/`).
2. **Given** un `relay.TunnelLimiter` (instance de `*relay.Limiter` dédiée aux tunnels) est instancié avec `MaxTunnels = 150` et 150 tunnels sont actifs (counter = 150), **When** un client tente d'ouvrir un 151ème tunnel via `POST /tunnel` (handler livré en Story 3.3), **Then** le serveur répond **HTTP 503 Service Unavailable** sans ouvrir de stream et sans incrémenter le compteur (cohérence atomic.Int64 préservée — cf. limiter.go:24-26).
3. **Given** le `TunnelLimiter` exposé dans `Server` est distinct de l'actuel `Server.Limiter` (utilisé pour `/dns-query`, `/health`, `/verify`, `/ip`, `/stun-relay`), **When** des requêtes courtes saturent `Server.Limiter` (1000 max — inchangé), **Then** elles n'ont aucun impact sur le compteur de tunnels et inversement.
4. **Given** l'endpoint `GET /health` répond, **When** la réponse JSON est décodée, **Then** elle contient le champ `"tunnels": <int64>` reflétant `TunnelLimiter.Current()` exactement (aux côtés de `status`, `connections`, `uptime`, `ram_mb`, `cpu_pct` ; ajout `nat_entries` hors scope — Story 3.4).
5. **Given** un test unitaire `go test ./internal/relay/...`, **When** la suite est exécutée, **Then** au moins ces deux tests neufs passent : (a) `TestLimitMiddleware_Returns503WhenTunnelLimiterSaturated` qui sature un `*Limiter` à `MaxTunnels` puis vérifie que le wrapper `LimitMiddleware` renvoie 503 sur la requête excédentaire, (b) `TestHealthHandler_ExposesTunnelsField` qui vérifie que le JSON contient `tunnels` et que la valeur reflète le `TunnelLimiter` injecté.
6. **Given** la constante `MaxTunnels = 150` et le test `TestLimiter_MaxReached` adapté, **When** la suite tourne, **Then** la régression de [internal/relay/limiter_test.go:22-39](internal/relay/limiter_test.go#L22-L39) reste verte (le test actuel utilise `MaxConnections = 1000` ; il continue d'utiliser `MaxConnections`, on n'y touche pas — voir Tâche 4).
7. **Given** le binaire relais est rebuild et redéployé sur un des 3 VPS (voir mémoire `reference_relay_servers.md`), **When** on appelle `curl -k https://<relais>/health`, **Then** la réponse JSON contient bien `"tunnels": 0` (relais frais) et le déploiement n'a brisé aucun endpoint existant (`/verify`, `/ip`, `/.well-known/relay-registry.json` répondent comme avant).

## Tasks / Subtasks

- [ ] Tâche 1 — Définir `MaxTunnels` et instancier `TunnelLimiter` (AC: 1, 3)
  - [ ] Dans [internal/relay/limiter.go](internal/relay/limiter.go), **ajouter** (ne PAS toucher à `MaxConnections = 1000`) :
    ```go
    // MaxTunnels is the global cap on concurrent /tunnel streams per relay.
    // Per architecture.md L243, L295: 150 tunnels simultanés (clients distincts)
    // par relais. Excess returns HTTP 503 → client failover (Epic 4).
    const MaxTunnels int64 = 150
    ```
  - [ ] Dans [internal/relay/server.go](internal/relay/server.go), ajouter un champ `TunnelLimiter *Limiter` à `Server` (lignes 15-30) à côté de `Limiter`
  - [ ] Initialiser dans `NewServer` : `TunnelLimiter: NewLimiter(MaxTunnels)` (lignes 33-41)
  - [ ] **Ne PAS** wirer `/tunnel` dans `ListenAndServe` — le handler `/tunnel` est créé par Story 3.3. Cette story livre uniquement le limiter et le câble de `/health`. Story 3.3 enroulera son handler avec `LimitMiddleware(s.TunnelLimiter, tunnelHandler)`.

- [ ] Tâche 2 — Étendre `/health` avec le champ `tunnels` (AC: 4)
  - [ ] Dans [internal/relay/health.go](internal/relay/health.go), ajouter `Tunnels int64 \`json:"tunnels"\`` à `HealthResponse` (entre `Connections` et `Uptime`)
  - [ ] Modifier `HealthHandler` pour stocker un second `*Limiter` :
    ```go
    type HealthHandler struct {
        limiter       *Limiter // legacy /dns-query etc. (rendered as "connections")
        tunnelLimiter *Limiter // /tunnel global cap (rendered as "tunnels")
        startTime     time.Time
    }
    ```
  - [ ] Adapter la signature de `NewHealthHandler` : `func NewHealthHandler(limiter, tunnelLimiter *Limiter, startTime time.Time) *HealthHandler` (rupture API privée acceptable — un seul caller dans server.go)
  - [ ] Dans `ServeHTTP`, peupler `resp.Tunnels = h.tunnelLimiter.Current()`. Si `h.tunnelLimiter == nil` (sécurité), renvoyer `0` (pas de panic)
  - [ ] Mettre à jour le caller [internal/relay/server.go:60](internal/relay/server.go#L60) : `mux.Handle("/health", LimitMiddleware(s.Limiter, NewHealthHandler(s.Limiter, s.TunnelLimiter, s.StartTime)))`

- [ ] Tâche 3 — Tests unitaires neufs (AC: 5)
  - [ ] Dans [internal/relay/middleware_test.go](internal/relay/middleware_test.go), ajouter `TestLimitMiddleware_Returns503WhenTunnelLimiterSaturated` :
    - Crée `l := NewLimiter(MaxTunnels)`, appelle `Acquire()` 150 fois (saturer)
    - Wrap un handler dummy avec `LimitMiddleware(l, dummy)`
    - Émet une requête → vérifier `rec.Code == http.StatusServiceUnavailable` et que le body contient "Service Unavailable"
    - Vérifier `l.Current() == 150` (pas d'incrément parasite — protection limiter.go:24-26)
  - [ ] Dans [internal/relay/health_test.go](internal/relay/health_test.go), ajouter `TestHealthHandler_ExposesTunnelsField` :
    - Crée `legacy := NewLimiter(MaxConnections)` et `tun := NewLimiter(MaxTunnels)`
    - `tun.Acquire()` 3 fois
    - `handler := NewHealthHandler(legacy, tun, time.Now())`
    - Émet GET, décode `HealthResponse`, vérifier `hr.Tunnels == 3`
    - Vérifier que le JSON brut contient bien `"tunnels":3` (string match) — protège contre un rename de tag JSON
  - [ ] Adapter les **5 tests existants** dans health_test.go qui appellent `NewHealthHandler(limiter, time.Now())` (lignes 15, 48, 83, 104, 118) pour passer désormais `NewHealthHandler(limiter, nil, time.Now())` — `nil` est sûr par Tâche 2
  - [ ] Vérifier que `TestHealthHandler_NoSensitiveData` (health_test.go:81-100) reste vert : "tunnels" n'est pas dans la liste des forbidden tokens → OK

- [ ] Tâche 4 — Vérifications de non-régression (AC: 6, 3)
  - [ ] Confirmer que [internal/relay/limiter_test.go](internal/relay/limiter_test.go) reste **inchangé** : il utilise `MaxConnections` partout (1000), pas `MaxTunnels`. Aucun test ne devrait référencer 150 en dur.
  - [ ] `go vet ./internal/relay/... && go test ./internal/relay/...` doit passer entièrement.
  - [ ] Vérifier qu'aucun appel à `NewHealthHandler` n'a été oublié : grep `NewHealthHandler\(` dans tout le repo → seul caller hors tests = `internal/relay/server.go`.
  - [ ] Compilation `go build ./...` doit passer (binaire relais + binaire desktop, le second n'utilise pas le package relay côté serveur mais s'appuie sur des types partagés rares).

- [ ] Tâche 5 — Smoke test sur relais réel (AC: 7)
  - [ ] Sur un seul des 3 relais (voir mémoire `reference_relay_servers.md`), rebuild le binaire `cmd/relay` (cross-compile linux/amd64 depuis Windows ou compile sur le VPS si Go installé), scp + `systemctl restart levoile-relay`
  - [ ] `curl -k https://<relais>/health` → vérifier que le JSON contient `"tunnels":0` ET les champs préexistants `status`, `connections`, `uptime`, `ram_mb`, `cpu_pct`
  - [ ] `curl -k https://<relais>/verify -X POST -H "Content-Type: application/json" -d '{"nonce":"<base64 32B>"}'` → vérifier réponse non-corrompue (200 ou 403 selon CF, peu importe — on contrôle juste l'absence de régression)
  - [ ] `curl -k https://<relais>/.well-known/relay-registry.json` → vérifier 200 + JSON registry intact
  - [ ] Consigner les outputs dans Completion Notes (masquer toute IP)
  - [ ] **Ne PAS** redéployer sur les 3 relais — un seul suffit pour valider, les 2 autres bénéficieront du déploiement coordonné après merge de Story 3.3

## Dev Notes

### Contexte business

Story de **garde-fou capacitaire** dans Epic 3. Le relais est stateless mais une explosion de tunnels concurrents épuiserait : (a) la table NAT en RAM (Story 3.4), (b) les sockets userspace, (c) la bande passante du VPS. À 150 tunnels × ~500 flux NAT chacun (architecture.md L243), on tient la limite RAM cible (NFR12 : <250 MB côté relais). Au-delà, le client doit failover vers un autre VPS du même pays (Epic 4) plutôt que de dégrader tout le monde.

C'est **délibérément 150 et pas 200 (la limite par IP)** : la limite par IP (Story 3.6) protège contre un abuseur unique ; la limite globale (cette story) protège la capacité du nœud face à du trafic légitime distribué sur de nombreuses IP.

### Pourquoi cette story est PETITE et préparatoire

Le handler `/tunnel` n'existe **pas encore** — il est livré par Story 3.3 ([epics.md ligne 670+](_bmad-output/planning-artifacts/epics.md), `handler /tunnel avec stream bidirectionnel paquets IP`). Story 3.7 livre :

1. La **constante `MaxTunnels = 150`** (source de vérité unique)
2. Le **`TunnelLimiter`** (instance dédiée de `*Limiter`, séparée de l'existant `Server.Limiter` qui sert les endpoints courts)
3. L'**exposition `tunnels`** dans `/health` (visible immédiatement, utile pour le monitoring même avant que `/tunnel` soit câblé)

Story 3.3 wirera ensuite `mux.Handle("/tunnel", LimitMiddleware(s.TunnelLimiter, tunnelHandler))`. Le 503 fonctionnera automatiquement grâce au middleware existant ([middleware.go:9-13](internal/relay/middleware.go#L9-L13) — pas de modification nécessaire à `LimitMiddleware`, qui renvoie déjà 503).

**Conséquence acceptée** : entre le merge de cette story et celui de Story 3.3, `/health` rapportera `"tunnels": 0` constant. C'est correct (zéro tunnel actif puisque `/tunnel` n'existe pas) et permet de découpler les deux stories.

### État existant (à ne PAS toucher)

- [internal/relay/limiter.go](internal/relay/limiter.go) — `Limiter` (atomic.Int64) générique, déjà utilisé. Constante `MaxConnections = 1000` levée par commit `0b5314e` parce que les anciens `/connect` (HTTP CONNECT proxy) sont des streams longs qui saturaient les 150 slots du `Server.Limiter`. **Conserver `MaxConnections = 1000`** : `/connect` est encore en place le temps que `/tunnel` (Story 3.3) le remplace, et les autres endpoints courts en bénéficient sans coût.
- [internal/relay/middleware.go](internal/relay/middleware.go) — `LimitMiddleware` renvoie déjà `http.StatusServiceUnavailable` (ligne 10) et gère bien le double-release. **Réutilisé tel quel.**
- [internal/relay/server.go](internal/relay/server.go) — `Server.Limiter` continue d'envelopper `/dns-query`, `/health`, `/verify`, `/ip`, `/stun-relay`. **`/tunnel` aura son propre limiter** (`TunnelLimiter`), wiring effectif Story 3.3.
- [internal/relay/health.go](internal/relay/health.go) — `HealthResponse.Connections` reste : c'est le compteur des endpoints courts, distinct de `Tunnels`. Le client desktop ne lit pas encore ces champs ([internal/registry/](internal/registry/) ping seulement le statut HTTP 200).

### Décisions de conception (à suivre, ne PAS rouvrir)

- **Deux limiters séparés** (`Limiter` 1000 + `TunnelLimiter` 150) plutôt qu'un seul à 150. Justification : (a) les endpoints courts ne doivent pas concurrencer les tunnels (et inversement), (b) `MaxConnections` est garanti monter à 1000 par la révision récente (commit `0b5314e`), (c) la sémantique est claire dans `/health` (`connections` vs `tunnels`).
- **HTTP 503, pas 429.** Story 3.6 utilise 429 pour le rate-limiting *par IP* (excès individuel = comportement anormal). Story 3.7 utilise 503 pour le plafond *global* (capacité du serveur saturée — sémantique RFC 7231 §6.6.4). Les clients réagissent différemment : 429 = backoff sur ce relais, 503 = failover immédiat vers un autre VPS (Epic 4).
- **Le handler `/health` reste public, sans rate limit dédié, mais traverse `Server.Limiter`.** Inchangé. Le coût d'un GET /health est négligeable et le limiter à 1000 absorbe largement les sondes Cloudflare + le ping client toutes les 6h.
- **Pas d'event log par 503.** Conformément NFR20 (zero-log IP) et la politique générale du relais, on ne loggue pas le rejet 503. Si un opérateur veut auditer la saturation, le compteur Prometheus est ajouté plus tard (hors scope MVP).

### Source tree à toucher

- [internal/relay/limiter.go](internal/relay/limiter.go) — **édition mineure** (Tâche 1) : ajout constante `MaxTunnels = 150`
- [internal/relay/server.go](internal/relay/server.go) — **édition** (Tâche 1, 2) : champ `TunnelLimiter`, init dans `NewServer`, mise à jour signature `NewHealthHandler`
- [internal/relay/health.go](internal/relay/health.go) — **édition** (Tâche 2) : champ `Tunnels`, signature `NewHealthHandler` étendue
- [internal/relay/health_test.go](internal/relay/health_test.go) — **édition** (Tâche 3) : 5 callers à mettre à jour + 1 test neuf
- [internal/relay/middleware_test.go](internal/relay/middleware_test.go) — **édition** (Tâche 3) : 1 test neuf
- [cmd/relay/main.go](cmd/relay/main.go) — **lecture seule** : confirmer qu'il appelle `relay.NewServer(...)` puis configure les handlers — pas de changement requis (le `TunnelLimiter` est créé automatiquement dans `NewServer`)

### Project Structure Notes

- Conformité naming : `MaxTunnels` (PascalCase, exporté) ✅ — architecture.md L450 cite `MaxConnections` comme exemple, `MaxTunnels` suit la même règle
- Pas de nouveau fichier — extension de fichiers existants seulement
- Pas de dépendance externe ajoutée (`atomic.Int64` déjà utilisé)

### References

- Story 3.7 epic source : [epics.md:755-767](_bmad-output/planning-artifacts/epics.md#L755-L767)
- FR30b (limite globale) : [epics.md:88](_bmad-output/planning-artifacts/epics.md#L88), [epics.md:327](_bmad-output/planning-artifacts/epics.md#L327)
- Architecture, plafond 150 + champ `tunnels` /health :
  - [architecture.md:243](_bmad-output/planning-artifacts/architecture.md#L243) (`Limite connexions par relais : 150 tunnels simultanés`)
  - [architecture.md:260](_bmad-output/planning-artifacts/architecture.md#L260) (`/health endpoint... connections renommé tunnels`)
  - [architecture.md:295](_bmad-output/planning-artifacts/architecture.md#L295) (`Limite par relais : 150 tunnels simultanés`)
  - [architecture.md:382](_bmad-output/planning-artifacts/architecture.md#L382) (exemple JSON `/health` avec `tunnels`)
  - [architecture.md:450](_bmad-output/planning-artifacts/architecture.md#L450) (convention naming `MaxConnections = 150` — adaptée en `MaxTunnels` car `MaxConnections` existe déjà à 1000)
  - [architecture.md:573](_bmad-output/planning-artifacts/architecture.md#L573) (forme JSON `/health`)
- Sémantique 503 vs 429 : RFC 7231 §6.5.10 (429) et §6.6.4 (503) — appliquée différenciée Story 3.6 vs 3.7
- Code de référence (pattern à suivre) : Story 3.2 livrée [3-2-endpoint-verify-avec-emission-session-tokens-ed25519.md](_bmad-output/implementation-artifacts/3-2-endpoint-verify-avec-emission-session-tokens-ed25519.md) — même style de gardrails + smoke test relais réel

### Dépendances et ordonnancement

- **Bloque** : Story 3.3 (`/tunnel handler`) — qui consommera `Server.TunnelLimiter` via `LimitMiddleware`
- **Indépendant de** : Stories 3.4 (NAT), 3.5 (DNS resolver), 3.6 (rate limit par IP), 3.8 (organisation pays)
- **Recommandé d'implémenter avant 3.3** : pour que 3.3 trouve `TunnelLimiter` déjà câblé et ne se contente pas de redéfinir une constante locale

### Previous Story Intelligence

Story 3.2 (la dernière story 3.x livrée, status `ready-for-dev`) a établi des patterns réutilisés ici :
- **Tests `Edge`** : préférer un fichier `*_edge_test.go` quand on ajoute des cas négatifs (ici on étend les fichiers `_test.go` existants car les ajouts sont mineurs et thématiquement liés)
- **Smoke test relais réel obligatoire** : la mémoire `reference_relay_servers.md` documente les 3 VPS — utiliser **un seul** pour valider, ne pas rebuild les 3 inutilement
- **Audit anti-fuite logs IP** : pas applicable ici (handler ne touche pas aux IP source — seulement à un compteur atomic)
- **Communication française** : conserver le ton FR (story, AC, dev notes) — le code et les noms de symboles restent EN

## Dev Agent Record

### Agent Model Used

claude-opus-4-6 (1M context)

### Debug Log References

### Completion Notes List

- Ultimate context engine analysis completed - comprehensive developer guide created
- Story découplée de 3.3 par design : livre uniquement le limiter + l'exposition `/health`, le wiring effectif `/tunnel` arrive avec Story 3.3
- Constante `MaxTunnels = 150` distincte de `MaxConnections = 1000` (rationale dans Dev Notes)

### File List
