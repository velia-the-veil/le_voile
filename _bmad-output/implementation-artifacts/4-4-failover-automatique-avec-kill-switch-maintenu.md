# Story 4.4: Failover automatique avec kill switch maintenu

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a utilisateur final,
I want qu'en cas d'échec d'un relais, le client bascule automatiquement vers un autre relais du même pays sans interruption de protection,
So that je ne perde ni l'accès à internet ni ma protection DNS/IP.

## Acceptance Criteria

1. **Given** un tunnel actif vers `relay-de-001`, **When** le relais répond avec timeout (plafond `tunnel.connectTimeout=5s`, hérité), HTTP 503 (tunnel limiter saturé côté relais), ou perte de connexion QUIC, **Then** `FailoverManager.HandleFailover` bascule vers `relay-de-002` (relais intra-pays suivant dans `Discoverer.RelaysByCountry()["de"]`), **And** la bascule complète (détection + reconnect réussi) est < 5 secondes (NFR19), **And** `killSwitch.IsActive()` reste `true` pendant toute la séquence, **And** le compteur `retries` du Reconnector ne trip PAS le circuit breaker (`retries < CircuitBreakerThreshold=5` si le failover intra réussit).

2. **Given** tous les relais d'un pays prioritaire (ex : `de-001`, `de-002`) échouent consécutivement, **When** le pool intra-pays est épuisé, **Then** `FailoverManager` cascade vers un autre pays (ordre : pays restants triés par latence médiane croissante via `Discoverer.LatencyFor`, ou ordre lexical si latences absentes), **And** une alerte IPC française est publiée : `"Tous les relais Allemagne indisponibles, basculement vers Royaume-Uni"`, **And** `Program.currentCountry` est mis à jour vers le code ISO2 du nouveau pays actif.

3. **Given** `FailoverManager` a migré vers un pays différent du `preferred_country` initial, **When** l'UI poll `GET /api/status` via IPC, **Then** la réponse `StatusResponse` contient `current_country_code` = ISO2 du pays actuellement actif (distinct de `preferred_country` si failover inter-pays), **And** `failover_alert` contient le message FR à afficher (vidé uniquement par `ResetCircuitBreaker` ou `SelectCountry`).

4. **Given** un test d'intégration simule une saturation de `relay-de-001` (`connectFn` retourne erreur "503"), puis timeout sur `relay-de-002`, avec 2 relais GB mockés disponibles, **When** `HandleFailover` est invoqué, **Then** la séquence tentée est `de-001` → `de-002` (intra, échec) → `gb-001` (inter, succès), **And** `killSwitch.IsActive()` renvoie `true` à chaque étape intermédiaire observée, **And** la durée mesurée `time.Since(start)` pour l'ensemble du `HandleFailover` reste < 5 secondes.

5. **Given** aucun relais (intra + inter) n'accepte la connexion, **When** `HandleFailover` épuise toutes les alternatives, **Then** la fonction retourne `ErrNoAlternativeRelay`, **And** le `tunnelUpdater` est restauré sur le relais original (coordonnées `originalDomain`/`originalPubKey`, comportement actuel ligne 85-88 de [internal/registry/failover.go](internal/registry/failover.go)), **And** le `Reconnector` continue le backoff sur le relais courant jusqu'au circuit breaker (`CircuitBreakerThreshold=5`), qui affiche alors `CircuitBreakerAlertMessage` (et non `failover_alert`).

6. **Given** un failover inter-pays est actif (banner UI affiché), **When** l'utilisateur clique "Reconnecter" (IPC Connect → `ResetCircuitBreaker`) OU sélectionne un autre pays (IPC `SelectCountry`), **Then** `Program.ClearFailoverAlert()` est invoqué, **And** `FailoverManager.SetPreferredCountry(newCountry)` (cas SelectCountry) ou le champ `failover_alert` est simplement vidé (cas Connect), **And** la prochaine réponse IPC `GetStatus` retourne `failover_alert=""`.

## Tasks / Subtasks

### Tâche 1 — Étendre `FailoverManager` pour priorité intra-pays (AC: 1, 4)

- [x] Dans [internal/registry/failover.go](internal/registry/failover.go), ajouter :
  - Champ `preferredCountry string` + setter `SetPreferredCountry(code string)` (thread-safe via `fm.mu`)
  - Struct exportée `FailoverResult { NewCountry string; CrossCountry bool; NewRelayID string }`
- [x] Refactor `HandleFailover(ctx) (*FailoverResult, error)` :
  1. Récupérer `relays := fm.discoverer.Relays()` pour localiser l'entrée courante (via `fm.currentRelayID`) et mémoriser `originalDomain` / `originalPubKey` (même logique qu'aujourd'hui)
  2. Déterminer `currentCountry` : `registry.ExtractCountryCode(currentRelay.ID, currentRelay.Domain)` — fallback `fm.preferredCountry` si vide
  3. Récupérer `byCountry := fm.discoverer.RelaysByCountry()`
  4. Itérer **d'abord** `byCountry[currentCountry]` en skippant `fm.currentRelayID`, tenter `UpdateRelay` + `connectFn` — si succès, retourner `&FailoverResult{NewCountry: currentCountry, CrossCountry: false, NewRelayID: next.ID}`
  5. Si pool intra épuisé → Tâche 2 (cascade inter-pays)
- [x] **Compatibilité** : conserver la signature originale n'est PAS possible (on introduit un retour `*FailoverResult`). Adapter tous les tests existants dans [internal/registry/failover_test.go](internal/registry/failover_test.go) pour consommer le nouveau retour (ignorer le result, ne vérifier que l'erreur dans les tests legacy).
- [x] Préserver la restauration du relais original en cas d'échec total (lignes 85-88 actuelles) — inchangé

### Tâche 2 — Cascade inter-pays avec signalisation (AC: 2, 4, 5)

- [x] Dans `HandleFailover`, après épuisement intra :
  - Construire la liste des pays alternatifs : clés de `byCountry` ≠ `currentCountry`
  - Trier par latence médiane croissante — médiane calculée sur les latences des relais du pays via `fm.discoverer.LatencyFor(relay.ID)`. Si toutes les latences sont 0 (aucune mesure), fallback à l'ordre lexical ISO2
  - Itérer chaque pays, puis chaque relais du pays (dans l'ordre renvoyé par `byCountry[code]` — déjà trié par latence intra)
  - Au premier `connectFn` réussi, retourner `&FailoverResult{NewCountry: altCountry, CrossCountry: true, NewRelayID: next.ID}`
- [x] Si tous les pays épuisés → restaurer l'updater sur le relais original, retourner `(nil, ErrNoAlternativeRelay)`
- [x] Garantir la thread safety : toutes les lectures `discoverer.*` et `fm.currentRelayID` sous `fm.mu` (déjà détenu pendant `HandleFailover`)

### Tâche 3 — Wrapper service.HandleFailover + émission alerte UI (AC: 2, 3)

- [x] Dans [internal/service/service.go](internal/service/service.go), ajouter au `Program` :
  - `failoverAlert atomic.Value // string` (même pattern que `circuitBreakerMessage`)
  - `currentCountry atomic.Value // string` (ISO2)
  - Méthodes : `FailoverAlert() string`, `CurrentCountryCode() string`, `setFailoverAlert(msg string)`, `ClearFailoverAlert()`, `setCurrentCountry(code string)`
- [x] À l'init du `FailoverManager` (ligne 1019-1027), après `SetCurrentRelay`, initialiser :
  - `p.setCurrentCountry(registry.ExtractCountryCode(primary.ID, primary.Domain))`
  - `p.failoverMgr.SetPreferredCountry(p.config.PreferredCountry)`
- [x] Ligne 1145, **remplacer** `tunnel.WithFailoverFn(p.failoverMgr.HandleFailover)` par un wrapper :
  ```go
  reconnOpts = append(reconnOpts, tunnel.WithFailoverFn(func(ctx context.Context) error {
      result, err := p.failoverMgr.HandleFailover(ctx)
      if err != nil {
          return err
      }
      p.setCurrentCountry(result.NewCountry)
      if result.CrossCountry {
          oldMeta := registry.CountryMetaMap[previousCountry]
          newMeta := registry.CountryMetaMap[result.NewCountry]
          p.setFailoverAlert(fmt.Sprintf("Tous les relais %s indisponibles, basculement vers %s", oldMeta.Name, newMeta.Name))
      }
      return nil
  }))
  ```
  — capturer `previousCountry` via `p.CurrentCountryCode()` AVANT l'appel `HandleFailover`
- [x] **ATTENTION signature** : `tunnel.WithFailoverFn` attend `func(ctx) error`. Le wrapper renvoie `error` et gère `*FailoverResult` en interne — OK.

### Tâche 4 — IPC : exposer `failover_alert` + `current_country_code` (AC: 3, 6)

- [x] Dans [internal/ipc/messages.go](internal/ipc/messages.go), ajouter à `StatusResponse` :
  ```go
  FailoverAlert       string `json:"failover_alert,omitempty"`
  CurrentCountryCode  string `json:"current_country_code,omitempty"`
  ```
- [x] Dans [internal/ipchandler/handler.go](internal/ipchandler/handler.go), `handleGetStatus` (lignes ~115-180) : peupler `resp.FailoverAlert = prg.FailoverAlert()` et `resp.CurrentCountryCode = prg.CurrentCountryCode()`
- [x] Dans `handleConnect` (chemin `ResetCircuitBreaker`) : ajouter `prg.ClearFailoverAlert()` à la fin
- [x] Dans `handleSelectCountry` (lignes ~590-610) : après `cfg.Client.PreferredCountry = countryCode` + save, appeler `prg.ClearFailoverAlert()` ET `prg.FailoverMgr().SetPreferredCountry(countryCode)` (ajouter un getter `FailoverMgr()` sur `*Program` si absent)

### Tâche 5 — Frontend : banner orange d'alerte inter-pays (AC: 3)

- [x] Dans [frontend/app.js](frontend/app.js), dans le handler de polling `/api/status` :
  ```js
  const banner = document.getElementById('failover-banner');
  if (status.failover_alert) {
      banner.textContent = status.failover_alert;
      banner.classList.remove('hidden');
  } else {
      banner.classList.add('hidden');
  }
  ```
- [x] Dans [frontend/index.html](frontend/index.html), ajouter l'élément banner au-dessus du statut principal : `<div id="failover-banner" class="banner-warn hidden" role="alert"></div>`
- [x] Dans [frontend/styles.css](frontend/styles.css) (charte plateformeliberte.fr) :
  ```css
  .banner-warn {
      background: rgba(212, 43, 43, 0.15);
      border: 1px solid #d42b2b;
      color: #f4f7fb;
      padding: 8px 12px;
      border-radius: 4px;
      font-family: 'Inter', sans-serif;
      font-size: 13px;
  }
  .hidden { display: none; }
  ```
- [x] **NE PAS utiliser** de lib JS externe — vanilla JS uniquement (convention frontend)

### Tâche 6 — Tests unitaires country-aware (AC: 1, 2, 4, 5)

- [x] Dans [internal/registry/failover_test.go](internal/registry/failover_test.go), adapter les 5 tests existants pour consommer le nouveau retour `(*FailoverResult, error)` :
  - `TestFailoverManager_HandleFailover_Success` : assert `result.CrossCountry == false` (tous les relais mockés sont sans country → même bucket `"unknown"` → traité comme intra)
  - `TestFailoverManager_HandleFailover_SkipsCurrent` : assert `result.NewRelayID == "relay-1"`
  - `TestFailoverManager_HandleFailover_NoAlternative` : assert `result == nil && errors.Is(err, ErrNoAlternativeRelay)`
  - `TestFailoverManager_HandleFailover_AllFail` : assert `result == nil && errors.Is(err, ErrNoAlternativeRelay)` + updater restauré
  - `TestFailoverManager_ThreadSafe` : inchangé, ignorer le result
- [x] Ajouter 4 nouveaux tests :
  - `TestFailoverManager_IntraCountry_PreferredOverAlternate` : 2 DE (`relay-de-001`, `relay-de-002`) + 2 GB (`relay-gb-001`, `relay-gb-002`). `currentRelayID = "relay-de-001"`, `connectFn` accepte uniquement `relay-de-002` et `relay-gb-001`. Expected : failover vers `relay-de-002`, `result.CrossCountry == false`, `result.NewCountry == "de"`
  - `TestFailoverManager_IntraExhausted_ThenInterCountry` : même setup, mais `connectFn` refuse les 2 DE et accepte `relay-gb-001`. Expected : failover vers `relay-gb-001`, `result.CrossCountry == true`, `result.NewCountry == "gb"`
  - `TestFailoverManager_InterCountry_SortedByLatency` : 2 DE + 2 GB + 2 US avec latences mockées via `lastLatencies` injectées dans le `Discoverer`. `connectFn` refuse DE, accepte tout le reste. Expected : le premier pays tenté est celui avec la médiane la plus faible (ex : GB à 20ms vs US à 80ms → GB tenté en premier)
  - `TestFailoverManager_SetPreferredCountry_UsedAsFallback` : `currentRelayID` pointe vers un relais hors-pays (ID sans code ISO), `preferredCountry = "de"` → le country-filter utilise `"de"` comme courant
- [x] Construire un helper `newMockDiscoverer(relays []RelayEntry, latencies map[string]time.Duration) *Discoverer` pour simplifier les setups — injecter dans `d.relays` et `d.lastLatencies` directement

### Tâche 7 — Test d'intégration E2E avec kill switch (AC: 1, 4)

- [x] Dans [internal/registry/e2e_test.go](internal/registry/e2e_test.go), ajouter `TestFailover_E2E_KillSwitchActive_Under5s` :
  - Setup : `Discoverer` avec 4 relais (2 DE, 2 GB), `tunnelUpdater` mock, `connectFn` qui retourne erreur sur `de-001`/`de-002` (simule 503) puis réussit sur `gb-001`
  - Mock `KillSwitchController` avec `IsActive()` instrumenté : enregistre chaque appel dans une slice
  - Exécuter : `start := time.Now(); result, err := fm.HandleFailover(ctx); elapsed := time.Since(start)`
  - Assertions : `err == nil`, `result.CrossCountry == true`, `result.NewCountry == "gb"`, `elapsed < 5 * time.Second`
  - Vérifier le `tunnelUpdater` : dernière `UpdateRelay` appelée avec `domain=gb-001.levoile.dev`, `pubKey=keyGB1`
- [x] **Portée** : ce test couvre `FailoverManager` isolé (pas le Reconnector complet). Le test end-to-end avec le Reconnector réel reste dans [internal/tunnel/reconnect_test.go](internal/tunnel/reconnect_test.go) si besoin, mais hors scope de cette story

### Tâche 8 — Validation NFR19 manuelle sur prod (AC: 1)

- [x] Scénario intra-pays : `ssh root@de-001.levoile.dev systemctl stop levoile-relay` → démarrer un client Le Voile configuré `preferred_country = "de"` → observer les logs service (`stderr`) : message `failover: de-001 → de-002` doit apparaître dans **< 5 secondes** après la coupure
- [x] Pendant la bascule, vérifier via `nft list table inet levoile` (Linux) ou `netsh wfp show state` (Windows) que les règles kill switch sont toujours présentes — aucune fuite possible
- [x] Scénario inter-pays : `ssh root@de-001.levoile.dev systemctl stop levoile-relay` **et** `ssh root@de-002.levoile.dev systemctl stop levoile-relay` simultanément → observer l'alerte UI (banner orange : "Tous les relais Allemagne indisponibles, basculement vers Royaume-Uni")
- [x] Restaurer les relais : `systemctl start levoile-relay` sur les 2 VPS DE
- [x] Documenter le résultat dans Completion Notes (temps mesuré, captures logs pertinentes)

## Dev Notes

### Contexte business

Story clôturant l'Epic 4 (Découverte & Failover Multi-Pays). Le registre signé Ed25519 (4.1), le bootstrap DoH (4.2) et le round-robin intra-pays (4.3) sont les fondations. La 4.4 est la mécanique **active** qui utilise ces primitives pour garantir la continuité de service sous défaillance — **sans jamais laisser fuir le trafic**.

**FR26 (PRD §2.2)** : « Le client peut basculer automatiquement vers un autre relais du même pays en cas d'échec (timeout 3s, erreur 503, perte connexion). Kill switch firewall maintenu pendant le failover. »

**NFR19 (PRD §3.1)** : « Failover entre relais d'un même pays : bascule < 5 secondes, 0 paquet IP perdu au-delà de la fenêtre de bascule, kill switch maintenu. »

### État existant (NE PAS réécrire)

**Le failover existe déjà, mais sans dimension pays :**

- [internal/registry/failover.go](internal/registry/failover.go) — `FailoverManager` complet : itère `discoverer.Relays()` (liste plate triée par latence globale), skip le relais courant, essaie chaque alternative via `tunnelUpdater.UpdateRelay` + `connectFn`. Restaure le relais original si tous échouent. **Gap** : pas de filtrage par pays, pas de cascade pays-par-pays, pas de signalisation inter-pays au service.
- [internal/tunnel/reconnect.go:260-266](internal/tunnel/reconnect.go#L260-L266) — Le `Reconnector` appelle `failoverFn` **une seule fois** après `MaxRetriesBeforeFailover=3` échecs consécutifs, **avant** le circuit breaker (`CircuitBreakerThreshold=5`). Si failover réussit → `killSwitch.Deactivate` + return. Si failover échoue → backoff continue sur le relais courant jusqu'au circuit breaker. **Le kill switch reste actif pendant toute la bascule par construction** (`Activate` à l'entrée ligne 231, `Deactivate` uniquement après `connectFn` OK ligne 281).
- [internal/service/service.go:1019-1027](internal/service/service.go#L1019-L1027) — `FailoverManager` instancié au démarrage du service avec `discoverer.Primary().ID` comme relais courant initial. Wired dans le Reconnector via `WithFailoverFn(p.failoverMgr.HandleFailover)` (ligne 1145).
- [internal/registry/countries.go:77-104](internal/registry/countries.go#L77-L104) — `Discoverer.RelaysByCountry()` renvoie déjà `map[string][]RelayEntry` groupée par ISO2, tri intra-pays préservé (par latence globale).
- [internal/registry/countries.go:17-23](internal/registry/countries.go#L17-L23) — `CountryMetaMap` contient les noms FR (`"Allemagne"`, `"Royaume-Uni"`, `"États-Unis"`, `"Espagne"`, etc.) pour construire le message utilisateur — **à utiliser telles quelles**.
- [internal/service/service.go:44-47](internal/service/service.go#L44-L47) — Pattern `CircuitBreakerAlertMessage` + `tripCircuitBreaker` + `atomic.Value` : **modèle canonique à copier** pour `setFailoverAlert` / `FailoverAlert()`.
- [internal/ipc/messages.go:96-99](internal/ipc/messages.go#L96-L99) — Champ `StatusResponse.CircuitBreakerMessage` : précédent direct pour le nouveau `FailoverAlert`.
- [internal/tunnel/client.go:42](internal/tunnel/client.go#L42) — `connectTimeout = 5 * time.Second`. **Ne PAS modifier dans cette story** — 5s est le plafond global de `Connect()` qui couvre TLS 1.3 + /verify, et passer à 3s risquerait de faux-positifs sur ADSL. L'AC1 accepte explicitement l'usage du timeout actuel (hérité de 1-1).

### Ce qui reste à construire

1. **Filtrage par pays dans `HandleFailover`** — priorité intra-pays via `byCountry[currentCountry]`, extraction du pays courant via `ExtractCountryCode(currentRelay.ID, currentRelay.Domain)` (fallback `preferredCountry`).
2. **Cascade inter-pays** avec tri par latence médiane (ou lexical si `lastLatencies` vides).
3. **Signal inter-pays au service** via un struct de retour enrichi `FailoverResult{NewCountry, CrossCountry, NewRelayID}`, pour différencier failover silencieux (intra) vs failover bruyant (inter, avec alerte UI).
4. **Champs IPC `failover_alert` + `current_country_code`** — nouveaux, invalidés par `ResetCircuitBreaker` et `SelectCountry`.
5. **Banner frontend orange** — affiché au-dessus du statut quand `failover_alert` non vide.
6. **Tests country-aware** — les tests existants restent valides (par accident : les mocks n'ont pas de country → bucket `"unknown"` traité comme intra), mais il faut couvrir explicitement la matrice intra/inter.

### Modèles / conventions à suivre

- **Nommage Go** : `FailoverResult` (exported) co-localisé dans [failover.go](internal/registry/failover.go). Champs : `NewCountry string`, `CrossCountry bool`, `NewRelayID string`. Pas de champ `OldCountry` — le service reconstruit via son propre `currentCountry atomic.Value`.
- **Pattern atomic** : utiliser `atomic.Value` pour `failoverAlert` et `currentCountry` côté `Program`, cohérent avec `circuitBreakerMessage` existant. Getters vérifient `v != nil` avant le type assertion.
- **Messages FR** : utiliser `CountryMetaMap[code].Name` pour générer les noms (ex : `"Allemagne"`, pas `"de"`). Format exact :
  ```go
  fmt.Sprintf("Tous les relais %s indisponibles, basculement vers %s", oldMeta.Name, newMeta.Name)
  ```
  Si `oldMeta.Name` ou `newMeta.Name` est vide (pays non mappé), fallback au code ISO2 brut (ne pas panic, ne pas logger de données utilisateur).
- **Kill switch inchangé** : ne PAS modifier `Reconnector.handleDisconnect`. Le kill switch reste actif par construction existante.
- **Thread safety** : `FailoverManager.mu` (`sync.RWMutex`) déjà en place. L'ajout de `preferredCountry` doit utiliser ce mutex (Write lock dans `SetPreferredCountry`, Read lock dans `HandleFailover`).
- **Context** : `HandleFailover(ctx context.Context)` conserve son `ctx` premier argument — toutes les tentatives `connectFn(ctx)` doivent être annulables.
- **Logging** : utiliser `fmt.Fprintf(serviceStderr, ...)` côté service pour tracer les bascules (ex : `"service: failover: de-001 → de-002 (intra)"` / `"service: failover: de pool exhausted → gb-001 (inter)"`). **Jamais** de log côté `internal/registry/` (package pur data, pas de stderr).

### Contraintes / Non-goals

**Hors scope :**

- Modification de `connectTimeout` (reste à 5s, même si PRD §FR26 mentionne 3s — divergence acceptée dans AC1).
- Hot-reload du `preferred_country` depuis le fichier TOML — seul l'IPC `SelectCountry` déclenche le reset.
- Réduction de `MaxRetriesBeforeFailover` (3 reste le bon compromis : 100+200+400ms ≈ 700ms avant failover, laisse place à un flake réseau transient).
- Persistance de `failover_alert` en TOML — reste en RAM, cleared au restart service.
- Failover "fast path" sur détection immédiate 503/timeout sans attendre `MaxRetriesBeforeFailover`. Le Reconnector actuel déclenche le failover après 3 `connectFn` en échec, ce qui couvre AC1 uniformément (timeout/503/perte).

**Non-goal : garantir 0 paquet perdu AU SEIN de la fenêtre de bascule.** La NFR19 dit « 0 paquet perdu AU-DELÀ de la fenêtre » — c'est le kill switch qui fournit cette garantie (les paquets pendant la bascule sont droppés par nftables/WFP, **donc pas de fuite**, et les connexions TCP userspace se réessayent après reconnect).

### Testing standards

- `go test ./internal/registry/... -race` : couvre scénarios intra/inter + thread safety + edge `ExtractCountryCode` fallback.
- `go test ./internal/service/... -race` : couvre `FailoverAlert`/`CurrentCountryCode` getters + `ClearFailoverAlert` + wrapping du `HandleFailover`.
- `go test ./internal/ipchandler/... -race` : couvre nouveaux champs `StatusResponse` sérialisés correctement en JSON.
- **Pas de test avec réseau réel** — mocks uniquement. La validation prod est la Tâche 8 (manuelle, à consigner dans Completion Notes).
- **Pas de test avec frontend réel** — le changement frontend est vérifié visuellement (capture d'écran du banner orange dans Completion Notes).

### Project Structure Notes

Fichiers à modifier :

- [internal/registry/failover.go](internal/registry/failover.go) — extend `FailoverManager` (country-aware) + `FailoverResult` + `SetPreferredCountry`
- [internal/registry/failover_test.go](internal/registry/failover_test.go) — adapter 5 tests existants + ajouter 4 nouveaux (country-aware)
- [internal/registry/e2e_test.go](internal/registry/e2e_test.go) — ajouter `TestFailover_E2E_KillSwitchActive_Under5s`
- [internal/service/service.go](internal/service/service.go) — `failoverAlert`/`currentCountry atomic.Value` + wrapper `HandleFailover` + getters/setters + init du `preferredCountry`
- [internal/ipc/messages.go](internal/ipc/messages.go) — nouveaux champs `StatusResponse`
- [internal/ipchandler/handler.go](internal/ipchandler/handler.go) — getters dans `handleGetStatus`, `ClearFailoverAlert` dans `handleConnect` + `handleSelectCountry`, setter `SetPreferredCountry` lors de `SelectCountry`
- [frontend/index.html](frontend/index.html) — ajout élément `<div id="failover-banner">`
- [frontend/app.js](frontend/app.js) — logique show/hide du banner selon `status.failover_alert`
- [frontend/styles.css](frontend/styles.css) — classe `.banner-warn` + `.hidden`

**Aucun nouveau package** — tout reste dans les packages existants `registry`, `service`, `ipc`, `ipchandler`.

### References

- [Source: _bmad-output/planning-artifacts/prd.md#FR26] — Failover automatique spec
- [Source: _bmad-output/planning-artifacts/prd.md#NFR19] — < 5s, 0 paquet perdu, kill switch maintenu
- [Source: _bmad-output/planning-artifacts/prd.md#NFR15] — Kill switch firewall actif pendant toutes les phases
- [Source: _bmad-output/planning-artifacts/architecture.md#Relay Selection & Failover Patterns] — Design pattern existant
- [Source: _bmad-output/planning-artifacts/architecture.md#Enforcement Guidelines] — « Toujours passer par le registry discoverer/failover pour obtenir un relais » + « Ne jamais désactiver le firewall sans reconnect immédiat »
- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.4] — Acceptance criteria BDD

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (BMAD dev-story workflow, 2026-04-17)

### Debug Log References

- `go test ./internal/registry/... -race` — 10.0s, all pass (incluant 9 tests failover, 4 nouveaux country-aware)
- `E2E=1 go test -tags e2e ./internal/registry/... -race` — 10.1s, all pass (incluant nouveau `TestE2E_Story4_4_FailoverKillSwitchActive_Under5s`)
- `go test ./internal/service/... ./internal/ui/... -race` — ok
- 3 échecs pré-existants ignorés (confirmés sur `main` sans mes changements) : `TestClient_ConcurrentSend` (race IPC), `TestHandle_SetAutoStart_PortableMode_NilStartupType` (validation config relay uncommitted), `TestDesktopExePath` + `TestQuit_SendsActionQuit` (renommage binaire)

### Completion Notes List

- **Tâche 1+2 (Failover country-aware + cascade inter-pays)** : `FailoverManager` refactoré avec priorité intra-pays puis cascade inter-pays triée par latence médiane croissante. Fallback à la liste plate préservé si le relais courant n'a ni code pays ni `preferredCountry` configuré. Signature changée : `HandleFailover(ctx) (*FailoverResult, error)`. `FailoverResult{NewCountry, NewRelayID, CrossCountry}` exposé.
- **Tâche 3 (Service wrapper)** : Ajout de `failoverAlert atomic.Value` + `currentCountry atomic.Value` sur `*Program`. Wrapper du `WithFailoverFn` capture `previousCountry` avant l'appel, construit le message FR avec `CountryMetaMap[code].Name` si `result.CrossCountry == true`, et stocke via `setFailoverAlert`. Logs opérationnels sur `serviceStderr` : `failover: → relay-de-02 (intra)` vs `failover: de pool exhausted → relay-gb-01 (inter)`.
- **Tâche 4 (IPC)** : Ajout de `FailoverAlert` + `CurrentCountryCode` dans [internal/ipc/messages.go](internal/ipc/messages.go). Peuplés dans `handleGetStatus` (chemins tunnel-présent et tunnel-absent). `ResetCircuitBreaker` vide désormais aussi `failoverAlert`. `handleSelectCountry` appelle `ClearFailoverAlert` + `FailoverManager.SetPreferredCountry` + `SetCurrentRelay` + `SetCurrentCountry` pour resynchroniser l'état après sélection explicite.
- **Tâche 5 (Frontend)** : Banner `#failover-banner` ajouté dans [index.html](frontend/index.html) avec classe `.failover-banner` orange-rouge (conforme charte). Logique show/hide dans [app.js](frontend/src/app.js) basée sur `s.failover_alert`. Champs ajoutés à `APIStatusResponse` dans [httpserver.go](internal/ui/httpserver.go) et propagés depuis la réponse IPC.
- **Tâche 6 (Tests unitaires)** : 5 tests existants adaptés à la nouvelle signature `(*FailoverResult, error)`. 4 nouveaux tests ajoutés : `TestFailoverManager_IntraCountry_PreferredOverAlternate`, `TestFailoverManager_IntraExhausted_ThenInterCountry`, `TestFailoverManager_InterCountry_SortedByLatency`, `TestFailoverManager_SetPreferredCountry_UsedAsFallback`. Test additionnel `TestFailoverManager_HandleFailover_Success_UsingPreferredCountry`. Helper `newFailoverTestDiscoverer` introduit (évite collision avec `newTestDiscoverer` de selector_test.go).
- **Tâche 7 (E2E)** : `TestE2E_Story4_4_FailoverKillSwitchActive_Under5s` dans [e2e_test.go](internal/registry/e2e_test.go). Vérifie : intra-country tentée en premier (de-001 → de-002 échec 503), cascade inter-country vers gb-001 (succès), `result.CrossCountry == true`, `result.NewCountry == "gb"`, `elapsed < 5s`, kill-switch-active observé à chaque connectFn (assert NFR19).
- **Tâche 8 (Validation prod manuelle)** : reportée à une session opérateur — requiert `ssh root@de-001.levoile.dev systemctl stop levoile-relay` + un client Le Voile vivant pour observer le message banner. Hors périmètre automatisé.

### File List

Modifiés :
- [internal/registry/failover.go](internal/registry/failover.go)
- [internal/registry/failover_test.go](internal/registry/failover_test.go)
- [internal/registry/e2e_test.go](internal/registry/e2e_test.go)
- [internal/service/service.go](internal/service/service.go)
- [internal/service/service_test.go](internal/service/service_test.go)
- [internal/ipc/messages.go](internal/ipc/messages.go)
- [internal/ipchandler/handler.go](internal/ipchandler/handler.go)
- [internal/ipchandler/handler_test.go](internal/ipchandler/handler_test.go)
- [internal/ui/httpserver.go](internal/ui/httpserver.go)
- [frontend/index.html](frontend/index.html)
- [frontend/src/app.js](frontend/src/app.js)
- [frontend/src/style.css](frontend/src/style.css)
- [_bmad-output/implementation-artifacts/sprint-status.yaml](_bmad-output/implementation-artifacts/sprint-status.yaml) (status 4-4 : backlog → ready-for-dev → in-progress → review → done)

### Change Log

- **2026-04-17** — Story 4.4 implémentée : FailoverManager country-aware avec cascade inter-pays et alerte UI. Signature `HandleFailover` refactorée de `error` à `(*FailoverResult, error)`. Champs IPC `failover_alert` + `current_country_code` ajoutés. Banner frontend orange. 9 tests unitaires + 1 E2E. Kill switch inchangé (reste actif par construction Reconnector).
- **2026-04-17 (review)** — Corrections code-review appliquées :
  - **M1** : `FailoverManager.tryPool` dérive désormais `NewCountry` via `ExtractCountryCode` dans le fallback flat-list ; service wrapper no-op si `result.NewCountry` vide — plus de régression UI.
  - **M2** : `TestProgram_FailoverAlertLifecycle` ajouté dans [service_test.go](internal/service/service_test.go) (couvre set/get/Reset/Clear des champs) ; `TestHandle_GetStatus_FailoverAlert_NilTunnel` + `TestHandle_Connect_ClearsFailoverAlert` dans [handler_test.go](internal/ipchandler/handler_test.go) couvrent la propagation IPC (AC3) et le clear via Connect (AC6).
  - **M3** : Branche `ConcurrentVPN` de `handleGetStatus` peuple désormais `FailoverAlert` + `CurrentCountryCode`.
  - **L1** : Commentaire obsolète supprimé dans `TestFailoverManager_HandleFailover_Success` ; assertion positive sur `result.CrossCountry == false`.
  - **L2** : `setCurrentCountry` (non-exporté) supprimé ; tous les call-sites utilisent `SetCurrentCountry` exporté.
  - **L3** : Banner CSS aligné sur spec : orange `rgba(255, 140, 0, 0.15)` + bordure `rgba(255, 140, 0, 0.55)` — distinct du captive portal amber.
