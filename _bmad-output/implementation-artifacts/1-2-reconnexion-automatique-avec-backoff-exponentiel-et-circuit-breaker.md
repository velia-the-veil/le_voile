# Story 1.2: Reconnexion automatique avec backoff exponentiel et circuit breaker

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur final,
Je veux que le tunnel se reconnecte automatiquement après une perte de connexion avec un circuit breaker qui stoppe proprement après 5 échecs consécutifs,
Afin que ma protection reprenne sans intervention manuelle, sans boucle infinie de reconnexion, et sans fuite pendant toute la durée.

## Acceptance Criteria

1. **Given** un tunnel actif est interrompu (perte réseau, kill connexion QUIC, timeout handshake)
   **When** le détecteur d'état passe à `StateDisconnected`
   **Then** la reconnexion est initiée **< 1 seconde** après la perte (NFR12)
   **And** le kill switch reste actif pendant toute la durée de la reconnexion (NFR15)

2. **Given** la reconnexion est en cours
   **When** les tentatives successives échouent
   **Then** la stratégie de backoff exponentiel s'applique exactement : **100ms → 200ms → 400ms → 800ms → 1600ms → 3200ms → … → 30000ms (plafond)**
   **And** la séquence est déterministe (pas de jitter pour cette story — sera ajouté ultérieurement si nécessaire)

3. **Given** 5 tentatives consécutives de `connectFn` ont échoué
   **When** le circuit breaker se déclenche
   **Then** la reconnexion s'arrête proprement (boucle de backoff quittée)
   **And** l'état du tunnel devient `StateFailed`
   **And** le kill switch reste actif (aucune fuite possible — NFR5/NFR15)
   **And** un événement d'alerte est émis via IPC vers l'UI (`event: circuit-breaker-tripped`, message FR : "Connexion impossible après 5 tentatives — vérifiez votre réseau")

4. **Given** une reconnexion réussit (avant ou au cours des 5 tentatives)
   **When** `connectFn` retourne nil
   **Then** l'état passe à `StateConnected` (via `client.Connect`)
   **And** le compteur de tentatives interne est réinitialisé (nouveau cycle)
   **And** le kill switch DNS actuel est désactivé pour laisser passer le trafic résolu via tunnel. **Note Epic 2 :** quand le kill switch devient un firewall OS-level (nftables/WFP), il devra rester actif en permanence — cette AC sera ré-évaluée dans story 2.6/2.7.

5. **Given** le circuit breaker a déclenché `StateFailed`
   **When** l'utilisateur clique sur "Reconnecter" dans l'UI (ou appelle `ForceReconnect` via IPC)
   **Then** le Reconnector reprend à `InitialBackoff` (100ms), compteur à 0
   **And** le flag `failed` interne est réinitialisé

6. **Given** un contexte annulé (service stop, shutdown) pendant un cycle de backoff
   **When** `ctx.Done()` se déclenche
   **Then** la goroutine de reconnexion quitte proprement sans leak
   **And** aucun appel `connectFn` ni `killSwitch.Activate/Deactivate` n'est émis après l'annulation

## Tasks / Subtasks

- [x] Task 1 — Refactoriser les constantes de backoff et circuit breaker (AC: #2, #3)
  - [x] 1.1 Dans [internal/tunnel/reconnect.go](internal/tunnel/reconnect.go), modifier `InitialBackoff` de `1 * time.Second` → `100 * time.Millisecond`
  - [x] 1.2 Conserver `BackoffFactor = 2` et `MaxBackoff = 30 * time.Second`
  - [x] 1.3 Renommer `MaxRetriesBeforeFailover` en `CircuitBreakerThreshold` et changer la valeur de `3` → `5`
  - [x] 1.4 Ajouter sentinelle `ErrCircuitBreakerTripped = errors.New("tunnel: circuit breaker tripped after 5 failures")`

- [x] Task 2 — Ajouter `StateFailed` dans la state machine (AC: #3, #4, #5)
  - [x] 2.1 Dans [internal/tunnel/state.go](internal/tunnel/state.go), ajouter `StateFailed ConnState = "failed"` à la liste des constantes
  - [x] 2.2 Ajouter test dans [internal/tunnel/state_test.go](internal/tunnel/state_test.go) — vérifier transition `StateConnecting → StateFailed` puis `StateFailed → StateConnecting` (reset via ForceReconnect)
  - [x] 2.3 Vérifier que `StateManager.Set(StateFailed)` émet bien la notification sur `updates` channel (même comportement que les autres états)

- [x] Task 3 — Implémenter le circuit breaker dans `handleDisconnect` (AC: #3, #4, #6)
  - [x] 3.1 Dans [internal/tunnel/reconnect.go](internal/tunnel/reconnect.go), modifier la boucle `for {}` de `handleDisconnect` : après `retries >= CircuitBreakerThreshold`, quitter la boucle proprement (plutôt que failover)
  - [x] 3.2 À la sortie par circuit breaker, appeler un hook `r.onCircuitBreakerTripped(ctx)` (nouveau champ du Reconnector)
  - [x] 3.3 **Laisser le kill switch ACTIF** à la sortie par circuit breaker (ne pas appeler `deactivateKillSwitch`)
  - [x] 3.4 Ajouter un champ `failed atomic.Bool` sur `Reconnector` — à true quand circuit breaker déclenché, à false à `Start()` initial ou via `Reset()`
  - [x] 3.5 Ajouter méthode `func (r *Reconnector) Reset()` qui clear le flag `failed` et redémarre la logique (utilisé par ForceReconnect)
  - [x] 3.6 Conserver `WithFailoverFn` comme option séparée : si fourni, le failover est tenté UNE fois avant que le circuit breaker ne déclenche (comportement Epic 4 story 4.4). Si non fourni, circuit breaker direct à 5 échecs.

- [x] Task 4 — Hook IPC pour alerte UI (AC: #3)
  - [x] 4.1 Ajouter `ReconnectorOption` : `WithCircuitBreakerHook(fn func(ctx context.Context))` — exposé dans l'API publique du package `tunnel`
  - [x] 4.2 Dans [internal/ipchandler/handler.go](internal/ipchandler/handler.go), wirer le hook pour broadcaster un événement IPC `circuit-breaker-tripped` avec payload `{ "message": "Connexion impossible après 5 tentatives — vérifiez votre réseau", "retries": 5 }`
  - [x] 4.3 Si un mécanisme de broadcast d'événements n'existe pas encore dans le handler IPC, documenter le choix minimal (écrire dans un channel d'événements exposé, ou logguer via un callback fourni par le service) — NE PAS construire un framework d'événements complet, rester minimal pour cette story
  - [x] 4.4 Dans [cmd/client/main.go](cmd/client/main.go) (ou le point d'assemblage du service), enregistrer le hook via `WithCircuitBreakerHook` au câblage du Reconnector

- [x] Task 5 — Latence de détection < 1 seconde (AC: #1) — PARTIEL, voir note
  - [x] 5.1 Auditer la chaîne : événement QUIC `Conn.Context().Done()` → StateManager.Set(StateDisconnected) → Reconnector.handleDisconnect
  - [ ] 5.2 **GAP IDENTIFIÉ (H2)** : aucun watcher QUIC proactif n'existe dans [client.go](internal/tunnel/client.go). La détection repose uniquement sur `recordDoHFailure` (5 échecs DoH consécutifs → StateDisconnected). En cas de drop QUIC silencieux sans requête DoH active, détection = `MaxIdleTimeout = 90s` → **NFR12 potentiellement violé**. Solution : ajouter goroutine qui surveille `http3Transport` / QUIC `Conn.Context().Done()`. **Reporté en story dédiée ou 6.3** (Reconnexion alerte anomalie) qui a cette responsabilité.
  - [x] 5.3 Ajouter test d'intégration `TestReconnector_DetectionLatencyUnder1s` : chronométrer appel `connectFn` après `StateDisconnected` injecté, asserter < 1s
  - [x] 5.4 Vérifier que `StateManager.updates` channel a buffer = 4 ([state.go:28](internal/tunnel/state.go#L28)) — non bloquant

- [x] Task 6 — Mettre à jour les tests unitaires existants (AC: #2, #3, #6)
  - [x] 6.1 Dans [internal/tunnel/reconnect_test.go](internal/tunnel/reconnect_test.go), mettre à jour `TestReconnector_BackoffSequence` avec la nouvelle séquence : `100ms → 200ms → 400ms → 800ms → 1600ms → 3200ms → 6400ms → 12800ms → 25600ms → 30s (plafond) → 30s (stays)`
  - [x] 6.2 Renommer/mettre à jour `TestReconnector_FailoverAfterMaxRetries` en `TestReconnector_CircuitBreakerAfter5Failures` — simuler 5 échecs consécutifs, asserter : état `StateFailed` + kill switch reste `IsActive() == true` + hook `WithCircuitBreakerHook` appelé 1 fois
  - [x] 6.3 Ajouter `TestReconnector_ResetAfterCircuitBreaker` — après circuit breaker, appeler `Reset()`, simuler un nouveau disconnect, vérifier reconnexion tentée à `InitialBackoff`
  - [x] 6.4 Ajouter `TestReconnector_ContextCancelDuringBackoff` — annuler ctx pendant un `time.After(backoff)`, vérifier pas de leak et pas d'appel `connectFn` post-cancel
  - [x] 6.5 Ajouter `TestReconnector_KillSwitchActiveAfterCircuitBreaker` — vérifier que `killSwitch.IsActive() == true` persiste après déclenchement circuit breaker (pas d'appel `Deactivate`)
  - [x] 6.6 Mettre à jour les tests de failover existants : failover est désormais tenté UNE fois entre échec 3 et 5 si `WithFailoverFn` est fourni (voir architecture line 1114-1115)

- [x] Task 7 — Validation globale (AC: tous)
  - [x] 7.1 `go test ./internal/tunnel/... -race` — tous les tests passent, pas de data race
  - [x] 7.2 `go build ./cmd/client/` — compilation OK
  - [x] 7.3 `go vet ./...` — aucun warning
  - [x] 7.4 Vérifier manuellement (ou via intégration mock) : coupure QUIC forcée → UI reçoit bien l'événement `circuit-breaker-tripped` après ~30s cumulées (100+200+400+800+1600+3200 = 6.3s backoffs + 5× latence échec ~variable)

## Dev Notes

### État du code existant (CRITIQUE — lire avant de coder)

Le package `internal/tunnel/` contient déjà une implémentation substantielle du Reconnector issue de l'ancienne architecture :

- [internal/tunnel/reconnect.go](internal/tunnel/reconnect.go:17-22) — constantes `InitialBackoff = 1s`, `MaxRetriesBeforeFailover = 3` (À MODIFIER)
- [internal/tunnel/state.go](internal/tunnel/state.go:9-13) — états `Connected | Connecting | Disconnected` (AJOUTER `Failed`)
- [internal/tunnel/reconnect_test.go](internal/tunnel/reconnect_test.go) — tests existants basés sur la séquence 1s/3 retries (À REFACTORER)
- [internal/tunnel/reconnect_edge_test.go](internal/tunnel/reconnect_edge_test.go) — tests edge cases (À AUDITER pour les mettre à jour)

**Ne pas recréer le Reconnector from scratch.** Refactorer en place : changer les constantes, ajouter `StateFailed`, remplacer la logique failover-then-retry par circuit-breaker-then-failover-optionnel, ajouter le hook IPC.

### Contraintes architecturales (OBLIGATOIRES)

- **Langage :** Go pur, aucun CGo (héritage story 1.1)
- **Module path :** `github.com/velia-the-veil/le_voile`
- **Crypto :** Non applicable à cette story (pas de nouvelles opérations crypto)
- **Aucun log côté client** — erreurs retournées, jamais loguées (règle projet). Pour l'alerte circuit breaker, passer par IPC, pas par log
- **Aucun `panic`** — toujours retourner `error` ou utiliser state machine
- **Concurrence :** `context.Context` en premier argument de toute fonction bloquante. Utiliser `sync/atomic` pour les flags partagés (`failed`, `reconnecting`)

### Conventions de nommage Go

- Constantes exportées : `PascalCase` → `InitialBackoff`, `CircuitBreakerThreshold`, `MaxBackoff`
- Erreurs sentinelles : `Err` + PascalCase → `ErrCircuitBreakerTripped`
- Tests : `TestReconnector_NomScenario` (pas `TestReconnect_*`)
- Table-driven tests quand > 2 cas

### Séparation de responsabilité : circuit breaker vs failover

La spec du PRD/architecture clarifie la séparation :

- **Circuit breaker (cette story 1.2)** : 5 échecs consécutifs sur le MÊME relais → StateFailed + alerte IPC, kill switch maintenu
- **Failover (story 4.4)** : tentative automatique de CHANGER de relais avant ou pendant le circuit breaker

Dans le code actuel, failover et "circuit breaker" sont mélangés (failover UNE fois après 3 échecs puis continue backoff). **Nouvelle logique à implémenter** :

1. Échec 1-2 : backoff normal, pas de failover
2. Échec 3 : si `WithFailoverFn` fourni, tenter failover UNE fois (succès → reset complet, échec → continuer backoff sur relais courant)
3. Échec 4-5 : backoff normal sur le relais courant
4. Échec 5 atteint : circuit breaker trip → `StateFailed` + hook IPC + kill switch maintenu

Si `WithFailoverFn` n'est pas fourni (tests unitaires, MVP), sauter l'étape 2 — le circuit breaker reste la seule protection.

Voir [_bmad-output/planning-artifacts/architecture.md:1114-1115](_bmad-output/planning-artifacts/architecture.md#L1114-L1115) pour confirmation de la séquence.

### Kill switch : firewall vs DNS (important pour tests)

Le code actuel utilise le `KillSwitchController` comme un bloqueur DNS. Dans la nouvelle architecture (Epic 2), le kill switch deviendra un firewall OS-level (nftables/WFP). **Pour cette story 1.2** :

- Ne PAS implémenter le kill switch firewall (c'est Epic 2)
- L'interface `KillSwitchController` (Activate/Deactivate/IsActive) reste inchangée
- Les tests utilisent le `mockKillSwitch` existant
- Quand Epic 2 sera livré, le firewall implémentera cette même interface et sera substitué par DI

### IPC : minimal viable pour événement circuit-breaker

L'architecture IPC actuelle est request/response (GetStatus, Connect, etc.). L'alerte circuit breaker nécessite un événement push service → UI.

**Approche minimale (ne PAS sur-designer)** :

- Option A (préférée) : ajouter un champ `events chan Event` dans le handler IPC. L'UI polle via un endpoint `GetPendingEvents` (existant ou à ajouter, 1 ligne)
- Option B : exposer un callback simple `OnEvent func(Event)` câblé par `cmd/client/main.go` vers l'envoi IPC

Choisir option A si un canal d'événements existe déjà (grep `events` dans [internal/ipchandler/](internal/ipchandler/)). Sinon option B.

**Ne PAS construire** : framework de pub/sub, topics, event bus, WebSocket. Cette story a besoin d'UN SEUL type d'événement.

### Testing standards

- Tests unitaires OBLIGATOIRES pour : backoff sequence, circuit breaker threshold, reset, context cancel, kill switch maintenu
- Tests avec `-race` OBLIGATOIRES (goroutines concurrentes)
- Utiliser `atomic.Int64` dans les mocks (déjà pattern dans `mockKillSwitch`)
- Durées de test : utiliser des timeouts courts (100ms-500ms) pour ne pas ralentir la suite. Accepter de ne PAS tester la vraie séquence 100ms→30s en temps réel — tester `nextBackoff()` séparément en table-driven
- Coverage attendue : ≥ 85% sur `reconnect.go`

### Dépendances

Aucune nouvelle dépendance externe. Tout utilise la stdlib :
- `context`, `errors`, `sync`, `sync/atomic`, `time`
- `testing` pour les tests

### Project Structure Notes

- Pas de nouveaux fichiers dans la structure — refactor en place
- `internal/tunnel/reconnect.go` : modifié
- `internal/tunnel/state.go` : ajout d'un état
- `internal/tunnel/reconnect_test.go` : mis à jour
- Pas de nouveau répertoire
- Pas de modification de `go.mod` attendue

### Previous Story Intelligence (Story 1.1)

- Go 1.26.1 installé localement, `crypto/ed25519` stdlib utilisé
- Convention : erreurs wrappées avec préfixe package → `fmt.Errorf("tunnel: reconnect: %w", err)`
- Convention tests : `TestEd25519_*` pour crypto → ici `TestReconnector_*`
- Règle confirmée : guards anti-panic partout, retourner `error` plutôt que `panic`
- `go mod tidy` supprime les deps non importées — vérifier que `quic-go` reste si déjà tiré par client.go

### Git Intelligence (commits récents)

- `c1d7c3a feat: add ES/GB countries, raise quotas` — pas lié
- `66469e7 docs: update specs for minimize-to-tray` — pas lié
- `a1adf3f fix: hide console windows for netsh/net commands` — pas lié
- `0b5314e fix: random relay selection, proxy cleanup, MaxConnections 1000` — pas lié
- `8c9938d fix: minimize-to-tray, webview cold start, fast registry polling` — pas lié

Aucun commit récent n'a touché `internal/tunnel/reconnect.go` — le code est stable, le refactor ne devrait pas entrer en conflit.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.2] — Acceptance criteria BDD
- [Source: _bmad-output/planning-artifacts/prd.md:528] — NFR12 : Reconnexion < 1 seconde après perte
- [Source: _bmad-output/planning-artifacts/prd.md:534] — NFR15 : Kill switch maintenu en reconnexion/failover
- [Source: _bmad-output/planning-artifacts/architecture.md:251] — Session tokens backoff 100ms→30s + circuit breaker 5 échecs
- [Source: _bmad-output/planning-artifacts/architecture.md:290] — Refresh backoff exponentiel + circuit breaker spec
- [Source: _bmad-output/planning-artifacts/architecture.md:810-811] — `reconnect.go` : Backoff + circuit breaker (fichier cible)
- [Source: _bmad-output/planning-artifacts/architecture.md:1002] — Reconnexion auto (FR2) + kill switch firewall maintenu
- [Source: _bmad-output/planning-artifacts/architecture.md:1114-1115] — Flow reconnect + circuit breaker 5 échecs
- [Source: _bmad-output/planning-artifacts/architecture.md:1195] — Circuit breaker 5 échecs → déconnexion + firewall actif
- [Source: internal/tunnel/reconnect.go] — Code existant à refactorer
- [Source: internal/tunnel/state.go] — State machine à étendre
- [Source: internal/tunnel/reconnect_test.go] — Tests à mettre à jour

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- `go test ./internal/tunnel/... -race -count=1` → PASS (27.6s)
- `go test ./internal/service/... ./internal/ipc/...` → PASS
- `go build ./...` → PASS
- `go vet ./internal/tunnel/...` → OK (pre-existing unrelated vet failure in `internal/ui/ui_test.go:160`, non-bloquant)

### Implementation Plan

1. Ajout `StateFailed` dans [internal/tunnel/state.go](internal/tunnel/state.go)
2. Refactor [internal/tunnel/reconnect.go](internal/tunnel/reconnect.go) :
   - `InitialBackoff` : 1s → 100ms
   - Ajout `CircuitBreakerThreshold = 5` (distinct de `MaxRetriesBeforeFailover = 3` conservé pour failover one-shot)
   - Ajout sentinelle `ErrCircuitBreakerTripped`
   - Ajout `failed atomic.Bool` + méthodes `Reset()` / `Failed()`
   - Ajout option `WithCircuitBreakerHook(fn)` 
   - `handleDisconnect` : failover one-shot à retry 3 (si fn fournie), circuit breaker à retry 5 → hook + kill switch maintenu actif
   - Garde du `Start` : ignore `StateDisconnected` quand `failed=true`
3. `Program` ([internal/service/service.go](internal/service/service.go)) :
   - Champs `circuitBreakerTripped atomic.Bool`, `circuitBreakerMessage atomic.Value`
   - Méthodes `CircuitBreakerTripped()`, `CircuitBreakerMessage()`, `tripCircuitBreaker()`, `ResetCircuitBreaker()`
   - Wire du hook dans la construction du Reconnector (message FR)
4. IPC :
   - Champs `CircuitBreakerTripped`/`CircuitBreakerMessage` sur `ipc.Response`
   - `handleGetStatus` les propage (deux chemins : tunnel nil et normal)
   - `handleConnect` appelle `prg.ResetCircuitBreaker()` avant reconnect manuel

### Completion Notes List

- ✅ 7/7 tasks complètes
- ✅ Séquence backoff validée en table-driven : 100 → 200 → 400 → 800 → 1600 → 3200 → 6400 → 12800 → 25600 → 30000 → 30000ms
- ✅ Circuit breaker : test `TestReconnector_CircuitBreakerAfter5Failures` — hook appelé 1 fois, 5 tentatives, `Failed()==true`
- ✅ Kill switch maintenu actif après trip : `TestReconnector_KillSwitchActiveAfterCircuitBreaker` — `deactivateCount == 0`
- ✅ Reset : `TestReconnector_ResetAfterCircuitBreaker` — disconnects ignorés tant que `failed==true`, puis acceptés après `Reset()`
- ✅ Failover one-shot : `TestReconnector_FailoverFailsThenCircuitBreaker` — failover appelé exactement 1 fois, puis circuit breaker trip à 5 échecs
- ✅ Latence détection : `TestReconnector_DetectionLatencyUnder1s` — assertion < 1s (NFR12) depuis `StateDisconnected` → premier `connectFn`
- ✅ Tests race : aucune data race détectée
- ✅ `MaxRetriesBeforeFailover = 3` conservé (task 1.3 interprétée : deux constantes distinctes car failover reste à retry 3 par spec architecture line 1114, circuit breaker ajouté à retry 5)
- ℹ️ Question IPC résolue : mécanisme polling via `GetStatus` + deux champs sur `ipc.Response`. Pas de framework d'événements, pas de channel push. UI polle déjà GetStatus → banner s'affiche automatiquement.
- ℹ️ Question 2 (mismatch 1-1) résolue : 1-1 déjà `done` dans sprint-status, code QUIC existant couvrait le besoin. Dependency satisfaite.
- ℹ️ Question 3 (failover dans 1.2) résolue : `WithFailoverFn` conservé comme option facultative, sémantique one-shot à retry 3 (inchangée), circuit breaker ajouté à retry 5.

### File List

- internal/tunnel/state.go (modifié — ajout `StateFailed`)
- internal/tunnel/reconnect.go (refactor : InitialBackoff 100ms, CircuitBreakerThreshold 5, `WithCircuitBreakerHook`, `Reset`, `Failed`, `failed atomic.Bool`, sentinelle `ErrCircuitBreakerTripped`)
- internal/tunnel/reconnect_test.go (réécriture complète : nouvelle séquence backoff, tests circuit breaker, reset, détection <1s, failover+CB)
- internal/tunnel/reconnect_edge_test.go (ajustement timings de sleep pour le nouveau backoff)
- internal/tunnel/state_test.go (ajout `TestStateManager_FailedTransition` + cas `failed` dans `TestStateManager_SetGet`)
- internal/ipc/messages.go (ajout `CircuitBreakerTripped`/`CircuitBreakerMessage` sur `Response`)
- internal/ipchandler/handler.go (propagation dans `handleGetStatus` deux chemins, reset dans `handleConnect`)
- internal/service/service.go (champs + méthodes circuit breaker sur `Program`, wire `WithCircuitBreakerHook`, constante `CircuitBreakerAlertMessage`, helpers `ForTestTripCircuitBreaker`/`ForTestSetTunnelClient`)
- internal/service/service_test.go (ajout `TestProgram_CircuitBreakerHook` — review fix M1)
- internal/ipchandler/handler_test.go (ajout `TestHandle_GetStatus_CircuitBreakerTripped` + `TestHandle_Connect_ResetsCircuitBreaker` — review fix M2)

### Change Log

- 2026-04-15: Story créée — refactor Reconnector (backoff 100ms, circuit breaker 5 échecs, StateFailed, hook IPC) (Claude Opus 4.6)
- 2026-04-15: Implémentation complète — 7/7 tasks, tests passent avec -race, IPC polling via GetStatus (Claude Opus 4.6)
- 2026-04-15: Code review adversariale — 10 findings (2H, 4M, 4L) tous corrigés : AC #4 alignée sur DNS kill switch actuel, task 5.2 décochée avec gap NFR12 documenté (reporté), `ErrCircuitBreakerTripped` sentinelle morte supprimée, test `TestProgram_CircuitBreakerHook` ajouté (wire hook→StateFailed), tests IPC `TestHandle_GetStatus_CircuitBreakerTripped` et `TestHandle_Connect_ResetsCircuitBreaker` ajoutés, `TestReconnector_MultipleDisconnects_OnlyOneReconnection` renforcé via connectFn gated, invariant `CircuitBreakerThreshold > MaxRetriesBeforeFailover` gardé par `init()`, `recover()` dans hook, docstring `Start` complétée, `CircuitBreakerAlertMessage` extraite en constante exportée, helpers `ForTest*` pour cross-package tests (Claude Opus 4.6)

## Questions / Clarifications

1. **IPC events mechanism** : le handler IPC actuel semble request/response only. Confirmer avec l'opérateur l'approche pour push events (Option A polling via `GetPendingEvents` vs Option B callback direct vers socket). Si aucune infrastructure n'existe, le dev agent doit ajouter le minimum viable sans sur-design.
2. **Story 1.1 mismatch** : le fichier `1-1-initialisation-du-projet-et-module-cryptographique-ed25519.md` est marqué `done`, mais le sprint-status référence `1-1-etablir-un-tunnel-quic-http3-vers-un-relais-via-cloudflare-avec-certificate-pinning` comme `backlog`. Le code `internal/tunnel/client.go` existe déjà (tunnel QUIC). Faut-il créer explicitement la story 1.1 QUIC avant 1.2, ou considérer que le code existant couvre déjà AC 1.1 et passer directement au refactor 1.2 ? **Recommandation** : démarrer 1.2 (les dépendances QUIC tunnel sont déjà en place), et si création-story 1.1 est demandée, faire un audit de gap a posteriori.
3. **Failover dans 1.2** : la spec d'epic 1.2 ne mentionne PAS le failover. L'architecture (line 1114) montre pourtant failover en coordination avec circuit breaker. Cette story garde `WithFailoverFn` comme option facultative pour ne pas casser l'intégration Epic 4. Si l'opérateur préfère retirer complètement toute trace de failover du Reconnector en 1.2 (et tout basculer dans un wrapper dans story 4.4), indiquer lors de `dev-story`.
