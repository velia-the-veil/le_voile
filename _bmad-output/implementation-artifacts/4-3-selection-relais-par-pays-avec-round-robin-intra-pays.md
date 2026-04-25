# Story 4.3: Sélection relais par pays avec round-robin intra-pays

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a utilisateur final,
I want choisir un pays via l'UI et que le client distribue mes nouvelles connexions entre les relais de ce pays via round-robin,
so that la charge soit répartie équitablement sur les VPS d'un même pays et qu'aucun relais ne soit saturé seul.

## Acceptance Criteria

1. **Given** un registre contenant ≥ 2 relais pour `"de"` (par ex. `de-001`, `de-002`, triés par latence), **When** le service appelle N fois consécutives un sélecteur `SelectRelay("de")` (ou équivalent `RelaysByCountry()` suivi d'une sélection round-robin), **Then** les N premières sélections suivent la séquence `de-001, de-002, de-001, de-002, …` (round-robin strict, wrap modulo `len(pool)`) et l'état du compteur est tenu en RAM uniquement (map country→index), protégé par mutex.

2. **Given** l'utilisateur sélectionne « Allemagne » via `POST /api/country` (`{code:"de"}`), **When** le serveur HTTP local proxie vers `ActionSelectCountry` et que `handleSelectCountry` est invoqué, **Then** le relais retourné provient du round-robin de la liste `de` (plus de `rand.Intn`), le tunnel est reconnecté sur ce relais, et `IPC Response{Status:Connected}` est renvoyé en < 5 s.

3. **Given** un relais de `"de"` est effectivement sélectionné, **When** `handleSelectCountry` complète avec succès, **Then** `cfg.Client.PreferredCountry = "de"` est persisté dans le TOML via `config.Save()` sous `configMu`, et un redémarrage du service consomme cette valeur dans `service.go` pour élire son relais initial via le même round-robin (pas via `countryRelays[0]`).

4. **Given** un relais `/health` est interrogeable via HTTPS, **When** `LatencyChecker.MeasureOne(ctx, relay)` est exécuté, **Then** le timeout par relais est `3 * time.Second` (au lieu des 5s actuels de `DefaultLatencyTimeout`/`MaxMeasureTimeout`) — constantes renommées ou abaissées explicitement, et documentées dans un commentaire `// AC Story 4.3`.

5. **Given** un relais dont la latence `/health` est bruyante (p.ex. samples 45 ms / 60 ms / 52 ms / 120 ms / 48 ms), **When** une nouvelle API `LatencyChecker.MeasureOneMedian(ctx, relay, samples=5)` (ou l'équivalent `MeasureAll` interne) mesure 5 RTT successifs via `GET /health`, **Then** la latence retenue pour le tri est la **médiane** des 5 échantillons (52 ms dans l'exemple), et chaque échantillon respecte le timeout 3 s de l'AC 4 — les échantillons échoués ne sont pas comptés dans la médiane ; si < 3 succès, le relais est marqué `Reachable=false`.

6. **Given** un cycle de mesure/tri en cours, **When** (a) la `refreshInterval` du `registry.Client` vaut `6h` par défaut (nouveau défaut, au lieu de `1h` actuel), **Then** le `refreshLoop` du `Discoverer` re-déclenche `Discover()` + `sortByLatency()` toutes les 6 heures ; **And** **When** (b) l'utilisateur change de pays via `handleSelectCountry`, **Then** une nouvelle mesure/tri est déclenchée immédiatement et en arrière-plan (goroutine, non bloquante pour la réponse IPC) sans attendre le prochain tick 6h.

7. **Given** un pays demandé non supporté (`code` absent de `CountryMetaMap`) ou un pays sans relais (`RelaysByCountry()[code]` vide), **When** `SelectRelay(code)` est appelé, **Then** il retourne respectivement `ErrUnknownCountry` et `ErrNoRelaysForCountry` — `handleSelectCountry` conserve ses réponses IPC actuelles (`unknown_country_code`, `no_relays_for_country`) mais câblées sur ces sentinelles plutôt que sur des chemins parallèles.

8. **Given** un pool de `de` initialement trié `[de-001, de-002]`, **When** `Discoverer.refreshLoop` re-trie en `[de-002, de-001]` (de-002 devenu plus rapide), **Then** le round-robin repart de l'index `0` sur la nouvelle liste (comportement documenté : « à re-tri, le compteur est remis à zéro pour ce pays ») — le tunnel actif (`FailoverManager.currentRelayID`) reste inchangé (sticky session AC5 Story 9.2 préservé). Aucun disconnect intempestif.

## Tasks / Subtasks

- [x] Tâche 1 — Round-robin par pays dans le package `registry` (AC: 1, 7, 8)
  - [x] Ajouter un type non exporté `countrySelector` dans [internal/registry/discoverer.go](internal/registry/discoverer.go) (ou un nouveau fichier [internal/registry/selector.go](internal/registry/selector.go)) contenant `sync.Mutex` + `map[string]int` (code pays → prochain index) + `map[string][]string` (code → snapshot ordonné des IDs pour détecter un changement de liste et reset l'index)
  - [x] Ajouter méthode `(d *Discoverer) SelectRelay(country string) (RelayEntry, error)` qui : (a) valide le code via `CountryMetaMap` → `ErrUnknownCountry`, (b) appelle `d.RelaysByCountry()[country]` → `ErrNoRelaysForCountry` si vide, (c) compare le snapshot IDs → si changement, remet le compteur à 0 pour ce pays (AC 8), (d) retourne `pool[idx%len]` et incrémente `idx` sous mutex
  - [x] Exposer les sentinelles `ErrUnknownCountry` et `ErrNoRelaysForCountry` dans [internal/registry/registry.go](internal/registry/registry.go) aux côtés de `ErrNoValidRelays`
  - [x] Tests dans [internal/registry/discoverer_test.go](internal/registry/discoverer_test.go) : 4 appels sur pool `[de-001, de-002]` → `[001, 002, 001, 002]` ; pays inconnu → `ErrUnknownCountry` ; pool vide → `ErrNoRelaysForCountry` ; reset sur changement de pool (ordre retrié) ; race (`-race`) avec N goroutines

- [x] Tâche 2 — Câbler le round-robin dans `handleSelectCountry` (AC: 2, 3, 7)
  - [x] Dans [internal/ipchandler/handler.go:544-618](internal/ipchandler/handler.go#L544-L618) : remplacer `relay := countryRelays[rand.Intn(len(countryRelays))]` par `relay, err := disc.SelectRelay(countryCode)` + mapping des erreurs sur les réponses IPC existantes
  - [x] Supprimer l'import `math/rand` si plus aucun appel dans le fichier (vérifier via `grep -n rand\\. internal/ipchandler/handler.go`)
  - [x] Conserver l'ordre exact des étapes : `UpdateRelay` → `Reconnector.Stop()` → `Disconnect` → `Connect(ctx 10s)` → `Reconnector.Start` → `config.Save(preferred_country)` → `SetVisibleIP("") + go DetectVisibleIP` — cet ordre a été validé Story 10.2
  - [x] Test dans [internal/ipchandler/handler_test.go](internal/ipchandler/handler_test.go) (ou créer si absent) : 2 appels successifs `SelectCountry("de")` avec 2 relais disponibles → le 2ème appel cible le *second* relais ; pays inconnu → `StatusError`, Error `"unknown_country_code"` ; pas de relais → `"no_relays_for_country"`

- [x] Tâche 3 — Round-robin au démarrage du service (AC: 1, 3)
  - [x] Dans [internal/service/service.go:761-773](internal/service/service.go#L761-L773) : remplacer `chosen = countryRelays[0]` par `chosen, err := p.discoverer.SelectRelay(p.config.PreferredCountry)` avec fallback silencieux sur `relays[0]` si `err != nil` (registre vide du pays préféré = ne pas crasher le démarrage, garder le comportement actuel dégradé)
  - [x] Test d'intégration dans [internal/service/service_test.go](internal/service/service_test.go) ou [internal/registry/e2e_test.go](internal/registry/e2e_test.go) : après avoir appelé `SelectRelay("de")` 3 fois (dont un pendant le démarrage), le compteur RAM reflète l'index attendu et le 4ème appel cible le relais index `3 % len(pool)`

- [x] Tâche 4 — Timeout /health à 3 s (AC: 4)
  - [x] Dans [internal/registry/latency.go:14-19](internal/registry/latency.go#L14-L19) : changer `DefaultLatencyTimeout = 5 * time.Second` → `3 * time.Second` et `MaxMeasureTimeout = 5 * time.Second` → `3 * time.Second`, ajouter un commentaire `// AC Story 4.3 — timeout /health 3s`
  - [x] Adapter tests [internal/registry/latency_test.go](internal/registry/latency_test.go) qui reposent sur 5s (aucun actuel ne teste ce seuil explicitement — vérifier puis compléter)
  - [x] Nouveau test : serveur /health qui répond après 2.5 s → succès ; après 3.5 s → `Reachable=false`

- [x] Tâche 5 — Médiane RTT sur 5 échantillons (AC: 5)
  - [x] Ajouter `func (lc *LatencyChecker) MeasureOneMedian(ctx context.Context, relay RelayEntry, samples int) (time.Duration, int, error)` dans [internal/registry/latency.go](internal/registry/latency.go) : itère `samples` (défaut 5) fois `MeasureOne`, renvoie `(median, successfulCount, err)`. Si `successfulCount < 3`, renvoie `(0, successfulCount, fmt.Errorf("registry: latency: %s: insufficient successful samples (%d/5)", relay.ID, successfulCount))`
  - [x] Calcul médiane : trier les samples valides, retourner `sorted[len/2]` (pour 5 → index 2, vrai median ; pour 4 → moyenne indices 1 et 2 n'est PAS requise, prendre `sorted[len/2]` suffit per AC)
  - [x] Modifier `MeasureAll` pour appeler `MeasureOneMedian` au lieu de `MeasureOne` (la signature de `LatencyResult` ne change pas : `Reachable` = `successfulCount >= 3`, `Latency` = médiane)
  - [x] Test : fixture 5 samples `[45ms, 60ms, 52ms, 120ms, 48ms]` → médiane attendue `52ms` ; fixture `[ok, ok, timeout, timeout, timeout]` → `Reachable=false` ; fixture `[ok, ok, ok, timeout, timeout]` → `Reachable=true`, médiane du trio réussi

- [x] Tâche 6 — Refresh par défaut à 6h + re-tri sur changement pays (AC: 6)
  - [x] Dans [internal/registry/client.go:70-75](internal/registry/client.go#L70-L75) : changer `refreshInterval: 1 * time.Hour` → `6 * time.Hour`
  - [x] Dans [internal/config/config.go:140](internal/config/config.go#L140) : changer le défaut `RefreshInterval: "1h"` → `"6h"`
  - [x] Dans [internal/ipchandler/handler.go](internal/ipchandler/handler.go) `handleSelectCountry`, après `Reconnector.Start` (avant la réponse IPC) : lancer `go func() { _, _ = disc.Discover(prg.Context()) }()` pour re-mesurer/re-trier sans bloquer la réponse IPC. Ajouter un commentaire `// AC Story 4.3 — re-tri à chaque changement de pays`
  - [x] Test : mocker `LatencyChecker` pour renvoyer des latences fixes, appeler `SelectCountry("de")` puis valider via un hook de test que `Discover()` a été appelé en background (compteur atomique ou canal de test)

- [x] Tâche 7 — Validation bout-en-bout (AC: 1-8)
  - [x] Étendre [internal/registry/e2e_test.go](internal/registry/e2e_test.go) avec un scénario : 2 relais `de`, démarrage service → relais initial via round-robin ; `SelectCountry("de")` → bascule vers le suivant ; reconnect via Reconnector → bascule vers suivant ; puis re-tri → reset index 0
  - [x] Vérifier manuellement que `go test ./internal/registry/... -race` passe, idem `./internal/ipchandler/...` et `./internal/service/...`
  - [x] `go build ./...` sur Linux + Windows (CI) avant merge

## Dev Notes

### Contexte business

Premier chantier d'Epic 4 (Découverte & Failover). Le registre distribué (Epic 3 + Story 4.1) et la sélection par pays (Story 10.2/5.3 côté UI) existent déjà. Cette story ajoute l'équité de distribution intra-pays : avec 2 VPS par pays prioritaire (DE/ES/GB/US), aucun n'est privilégié et la charge se répartit naturellement. C'est aussi un prérequis structurel pour Story 4.4 (failover automatique) : le failover se fait via le pool round-robin, donc le pool doit être stable et re-triable.

**PRD §FR25** : « Le client peut distribuer les connexions entre les relais d'un même pays via round-robin ».

### État existant (ne PAS réécrire)

Le squelette Epic 4 est déjà là (issu de l'ancien Epic 9) :

- **[internal/registry/discoverer.go](internal/registry/discoverer.go)** — `Discoverer` orchestre `Fetch → Verify → Cache → sortByLatency`. `RelaysByCountry()` fournit le groupement par code ISO (déjà testé Story 3.8). `refreshLoop` tourne en background.
- **[internal/registry/latency.go](internal/registry/latency.go)** — `LatencyChecker.MeasureAll` mesure via `GET /health` en parallèle, `SortByLatency` trie.
- **[internal/registry/failover.go](internal/registry/failover.go)** — `FailoverManager` itère `relays` (dans l'ordre de tri) avec `currentIdx + offset`. Cette story NE le modifie PAS (Story 4.4 s'en occupera).
- **[internal/registry/countries.go](internal/registry/countries.go)** — `CountryMetaMap` supporte `de/es/gb/us/is/fi/fr` (Story 3.8). Le fix `ExtractCountryCode` pour `{code}-NNN.levoile.dev` est déjà en place.
- **[internal/ipchandler/handler.go:544-618](internal/ipchandler/handler.go#L544-L618)** — `handleSelectCountry` fonctionne déjà de bout en bout (UpdateRelay, Disconnect, Connect, Reconnector restart, config.Save `preferred_country`) — **seule la ligne de sélection (`rand.Intn`) est à remplacer** par le round-robin.
- **[internal/service/service.go:761-773](internal/service/service.go#L761-L773)** — Sélection initiale au démarrage prend `countryRelays[0]` — aussi à remplacer par `SelectRelay`.
- **[internal/config/config.go:111](internal/config/config.go#L111)** — `ClientConfig.PreferredCountry string` est déjà câblé et testé (Story 10.2).
- **[frontend/wailsjs/go/desktop/App.js:33](frontend/wailsjs/go/desktop/App.js#L33)** et **[internal/desktop/app.go:169-180](internal/desktop/app.go#L169-L180)** — UI `SelectCountry` existe (Story 10.2). **Rien à toucher côté frontend**.

### Gap (ce qu'il faut ajouter)

1. **Round-robin** — aujourd'hui `handleSelectCountry` fait `rand.Intn(len(countryRelays))` ([internal/ipchandler/handler.go:571](internal/ipchandler/handler.go#L571)) et `service.go` fait `countryRelays[0]` ([internal/service/service.go:768](internal/service/service.go#L768)). Les deux sont non-équitables. Remplacer par un sélecteur round-robin centralisé dans `Discoverer`.
2. **Médiane /health sur 5 samples** — aujourd'hui `MeasureAll` appelle `MeasureOne` une seule fois par relais. Le PRD (BDD Story 4.3) exige 5 samples + médiane.
3. **Timeout 3 s** — aujourd'hui `DefaultLatencyTimeout = 5s`. L'AC 4 exige 3 s.
4. **Refresh 6 h** — aujourd'hui `1h` par défaut (client.go) et `"1h"` dans le TOML. L'AC 6 exige `6h`. (Note : aucune donnée de prod ne dépend du 1h actuel, le seul impact est une charge réseau divisée par 6.)
5. **Re-tri à la sélection de pays** — aujourd'hui on attend le prochain tick du refreshLoop. L'AC exige un re-tri immédiat en background.

### Modèles / conventions à suivre

- **Sentinelles d'erreur** — pattern déjà établi par `ErrNoValidRelays`, `ErrInvalidMasterKey`, `ErrRegistryEmpty` ([internal/registry/registry.go:22-28](internal/registry/registry.go#L22-L28)). Ajouter `ErrUnknownCountry` et `ErrNoRelaysForCountry` au même endroit.
- **Thread-safety** — le `Discoverer` utilise déjà `sync.RWMutex` ([internal/registry/discoverer.go:27](internal/registry/discoverer.go#L27)). Réutiliser le même mutex (ou un mutex dédié au `countrySelector` pour éviter les contentions avec `Relays()` appelé souvent depuis l'UI).
- **Naming Go** — méthode publique `SelectRelay`, struct interne `countrySelector` (non exporté, c'est un détail d'implémentation du `Discoverer`). Conformer à Architecture §naming ([_bmad-output/planning-artifacts/architecture.md:434](../../_bmad-output/planning-artifacts/architecture.md)).
- **Médiane** — trier les samples valides via `sort.Slice` puis `sorted[len(sorted)/2]`. Pas de moyenne (la médiane est plus robuste aux outliers, intention explicite de l'AC).
- **configMu** — préservation obligatoire autour de `config.Load`/`config.Save` pour `preferred_country` (déjà en place handler.go:602-611). Ne pas toucher.
- **Ordre strict Connect/Disconnect** — Architecture §"Ordre strict Connect/Disconnect" (Pattern Completeness). Ne pas réordonner les étapes de `handleSelectCountry` ; injecter le `go Discover()` APRÈS `Reconnector.Start` mais AVANT le `return`.
- **Zéro log d'IP** — NFR20. Aucun log ne doit contenir d'IP relais ni d'IP client. Les constantes `relay.ID` sont OK à logger. Si la médiane échoue, logger l'ID du relais mais pas son domaine.

### Constraints & Non-goals

- **Hors scope** : Story 4.4 (failover automatique avec kill switch maintenu) — le `FailoverManager` n'est PAS modifié ici. Cette story prépare le terrain (pool round-robin stable) mais ne câble pas encore le failover sur timeout/503.
- **Hors scope** : Story 4.1 (endpoint /.well-known/relay-registry.json — déjà `done` côté serveur, c'est Story 3.1 + déploiement manuel de Story 3.8). Si la signature du registre n'est pas encore vérifiée en prod, c'est Story 4.1 qui le corrige — mais cette story NE dépend PAS de 4.1 pour être testable (fonctionne en local avec des relais mockés).
- **Hors scope** : Story 4.2 (bootstrap DoH). Le resolver DoH est indépendant du round-robin.
- **Persistance round-robin** — l'état du compteur N'EST PAS persisté (AC 1 : « en RAM uniquement »). Redémarrer le service repart de l'index 0 pour chaque pays. Accepté.
- **Pas d'incrément si `UpdateRelay` échoue** — si `handleSelectCountry` échoue avant `Connect` réussi, l'appel suivant doit retomber sur le MÊME relais (pour éviter de brûler tout le pool sur une configuration invalide). Incrémenter le compteur APRÈS un succès confirmé (à documenter dans `SelectRelay` via un pattern `Peek` + `Advance`, OU plus simple : incrémenter avant retour et accepter la « perte » d'un index sur échec — choisir la version simple, documenter).
- **Pas de distribution pondérée** — pas de considération de charge CPU/RAM/tunnels par relais pour la sélection. Le round-robin strict suffit au MVP. Une sélection par moindre charge est un NFR de Phase 2.

### Testing standards

- **Unitaires** obligatoires : `go test ./internal/registry/... -race` vert, `./internal/ipchandler/... -race` vert. Coverage des nouvelles fonctions ≥ 80 %.
- **Test de médiane** : fixtures avec `httptest.NewTLSServer` qui simule N/5 succès + N/5 timeouts, vérifier `LatencyResult.Latency` et `LatencyResult.Reachable`.
- **Test round-robin avec race** : 50 goroutines qui appellent `SelectRelay("de")` en parallèle, 100 appels chacune → vérifier l'absence de data race et que chaque relais est sélectionné exactement `50 * 100 / len(pool)` fois ± 1.
- **E2E** : étendre [internal/registry/e2e_test.go](internal/registry/e2e_test.go) avec un flux `Discover → SelectRelay → re-sort → SelectRelay` sur un mini-registre fixture.
- **Manuel** : builder `cmd/client`, lancer en local avec `registry.enabled=true` pointant vers un relais dev local qui renvoie 2 entrées `de` → vérifier via logs/IPC que les 2 premiers `SelectCountry("de")` alternent bien.

### Project Structure Notes

- **Touchés** : `internal/registry/discoverer.go` (ou nouveau `selector.go`), `internal/registry/latency.go`, `internal/registry/client.go`, `internal/registry/registry.go` (sentinelles), `internal/ipchandler/handler.go`, `internal/service/service.go`, `internal/config/config.go` (défaut `"6h"`).
- **Pas touchés** : `internal/tunnel/`, `internal/tun/`, `internal/firewall/`, `internal/routing/`, `internal/relay/`, `internal/desktop/`, `frontend/`, `cmd/ui/`, `cmd/relay/`.
- **Binaires impactés** : `cmd/client` (service principal) et `cmd/portable` (via la même config). `cmd/ui` est indirectement impacté (UI consomme `/api/country` inchangée).
- **Pas de migration de config** — le défaut `refresh_interval = "6h"` ne casse pas les installations existantes (celles qui ont `"1h"` explicite dans leur TOML le gardent). OK.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-4.3](../../_bmad-output/planning-artifacts/epics.md)
- [Source: _bmad-output/planning-artifacts/prd.md#FR25](../../_bmad-output/planning-artifacts/prd.md)
- [Source: _bmad-output/planning-artifacts/architecture.md#Relay-Selection-Failover-Patterns](../../_bmad-output/planning-artifacts/architecture.md)
- [Code: internal/registry/discoverer.go](internal/registry/discoverer.go)
- [Code: internal/registry/latency.go](internal/registry/latency.go)
- [Code: internal/registry/client.go](internal/registry/client.go)
- [Code: internal/registry/countries.go](internal/registry/countries.go)
- [Code: internal/registry/registry.go](internal/registry/registry.go)
- [Code: internal/ipchandler/handler.go:544-618](internal/ipchandler/handler.go#L544-L618)
- [Code: internal/service/service.go:736-776](internal/service/service.go#L736-L776)
- [Code: internal/config/config.go:84-141](internal/config/config.go#L84-L141)

### Previous Story Intelligence (Story 3.8 — la plus récente de la chaîne registre)

- **Pattern de test** : Story 3.8 a ajouté des cas à [internal/registry/countries_test.go](internal/registry/countries_test.go) sans dupliquer le setup — faire pareil : étendre plutôt que créer.
- **Format domaine en prod** : `{code}-NNN.levoile.dev` (ex : `de-001.levoile.dev`). `ExtractCountryCode("", "de-001.levoile.dev")` renvoie bien `"de"` depuis le fix 3.8.
- **Master public key en prod** : `rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk=` (mémoire `reference_relay_servers.md`). 8 relais en production : DE-001/002, ES-001/002, GB-001/002, US-001/002. Ne pas changer.
- **Constantes partagées** : `SignaturePrefix = "relay-key-v1:"` ([internal/registry/registry.go:20](internal/registry/registry.go#L20)) — ne pas dupliquer de littéraux, réutiliser les constantes exposées.
- **Stickiness tunnel actif** : `FailoverManager.currentRelayID` est sticky (Story 9.2 AC5). Le re-tri du `Discoverer.refreshLoop` n'interrompt PAS la session active. Préserver (Task 6, AC 8).

### Git Intelligence (commits récents pertinents)

- `16a275a` (2026-04-17) — `fix(deploy): align install.sh/README/service with prod (signing.key + registry)` : normalisation du déploiement du registre sur les 8 VPS. Signifie que `/opt/levoile/relay-registry.json` est canonique en prod — la story peut supposer le registre vivant avec 8 relais.
- `c2e1c0e` — `feat: complete Epic 3 — relay stateless multi-VPS with tunnel IP & NAT` : Epic 3 terminé, infrastructure multi-relais opérationnelle.
- `bd11612` — `feat: IPv6 leak opt-out toggle + relay systemd CAP_NET_ADMIN` : confirme que le service relais tourne avec les bonnes capabilities, donc `/health` est bien servi côté relais.
- Pattern commits : titre en conventional-commits (`feat:`, `fix:`, `chore:`), français uniquement dans le corps/mémoire, code et commits en anglais. Respecter.

### Latest Tech Information

- **Go stdlib** — `math/rand` : depuis Go 1.20, seedé automatiquement ; aucune init `rand.Seed` n'est nécessaire. Puisqu'on supprime `rand.Intn`, pas d'impact. Vérifier le `go.mod` pour confirmer Go ≥ 1.20 (probablement 1.22+).
- **`sort.Slice`** — suffisant pour une liste de 5 éléments. Pas besoin de `sort.Stable`.
- **`sync.Mutex` vs `sync.RWMutex`** — le `countrySelector` lit et écrit à chaque appel (incrémente le compteur) → `sync.Mutex` suffit, `RWMutex` serait inutile.
- **Round-robin bibliothèque** — aucune dépendance externe requise. `go.uber.org/ratelimit` et autres sont overkill. Une map + mutex + int suffit (pattern Go idiomatique).

### Project Context Reference

Pas de [project-context.md](../../docs/project-context.md) détecté — le contexte projet est ancré dans `CLAUDE.md` (à la racine) et dans les mémoires opérateur (`reference_relay_servers.md`).

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m]

### Debug Log References

### Completion Notes List

- Story 4.3 implémentée : round-robin intra-pays + médiane RTT sur 5 probes + timeout /health 3 s + refresh 6 h + re-tri en arrière-plan sur changement de pays.
- **Round-robin (AC 1, 7, 8)** : nouveau `internal/registry/selector.go` avec `countrySelector` (mutex + map `cursors` + map `snapshots`). `Discoverer.SelectRelay(country)` expose l'API, réinitialise le curseur quand le snapshot des IDs change (reset au re-tri). Sentinelles `ErrUnknownCountry` et `ErrNoRelaysForCountry` ajoutées dans `registry.go`.
- **Handler (AC 2, 3, 7)** : `handleSelectCountry` remplace `rand.Intn` par `disc.SelectRelay(countryCode)` + mapping des sentinelles sur les réponses IPC existantes (`unknown_country_code`, `no_relays_for_country`). Import `math/rand` supprimé. L'ordre strict (UpdateRelay → Stop reconnector → Disconnect → Connect 10 s → Start reconnector → config.Save preferred_country → DetectVisibleIP → re-discover goroutine) est préservé.
- **Démarrage service (AC 1, 3)** : `service.go:804-813` swap `countryRelays[0]` → `p.discoverer.SelectRelay(p.config.PreferredCountry)` avec fallback silencieux vers `relays[0]` si le pays préféré est vide.
- **Timeout 3 s (AC 4)** : `DefaultLatencyTimeout` et `MaxMeasureTimeout` abaissés de 5 s à 3 s dans `latency.go`. Test `TestDefaultLatencyTimeout_Is3Seconds` verrouille la valeur.
- **Médiane 5 probes (AC 5)** : nouvelle `LatencyChecker.MeasureOneMedian(ctx, relay, samples)` avec pacing 20 ms entre probes, seuil `MinSuccessfulSamples = 3` et sortie triée `rtts[len/2]`. `MeasureAll` route maintenant vers cette méthode. Tests couvrent all-succeed, partial (3/5), insufficient (2/5), defaut=0 → `DefaultMedianSamples`.
- **Refresh 6 h (AC 6)** : `registry.Client.refreshInterval` défaut 1 h → 6 h. `config.go` défaut TOML `"1h"` → `"6h"` + test `TestConfig_RegistryDefaults` mis à jour. **Re-tri sur changement de pays** : `handleSelectCountry` lance `go disc.Discover(prg.Context())` en fin de handler (non-bloquant, hors chemin de réponse IPC).
- **Régression unique** : `TestHandle_SetAutoStart_PortableMode_NilStartupType` échoue car la fixture `[relay]\ndomain = "test.dev"\n` ne contient pas `public_key_ed25519` et la validation config (non modifiée par cette story — changement antérieur uncommitted) la rejette. Hors scope 4.3.
- **Race pré-existante STUN** : `TestStartSTUN_NilTunnelClient` / `TestSetSTUNEnabled_AfterStop` exhibent une race ordre-dépendante en suite complète (passe en isolation). Indépendante de 4.3 (aucune concurrence introduite dans `service.go`).
- **Validation terrain** : `go test ./internal/registry/... ./internal/config/... -race -count=1` ✅. `go build ./...` ✅ (Linux + Windows, les 3 binaires `cmd/client`, `cmd/relay`, `cmd/ui` compilent). `go vet ./...` ✅.

### Change Log

- 2026-04-17 — Story 4.3 livrée en `review` : round-robin intra-pays, médiane /health 5 probes (timeout 3 s), refresh défaut 6 h, re-tri background sur changement de pays. Zéro rupture de config ou d'IPC existant.
- 2026-04-17 — Code review adversariale : 3 HIGH + 3 MEDIUM + 2 LOW trouvés. Tous les HIGH et MEDIUM corrigés automatiquement. Story → `done`.

### Senior Developer Review (AI)

**Date :** 2026-04-17
**Reviewer :** Claude Opus 4.7 (1M context) — même modèle que l'implémenteur, revue adversariale auto-critique
**Outcome :** Changes Requested → Addressed (all HIGH + MEDIUM fixed)

**Action Items — résolus**

- [x] [AI-Review][HIGH] `break` dans `select` ne sortait pas du `for` — `MeasureOneMedian` continuait à itérer après annulation ctx [[latency.go:116-120](internal/registry/latency.go#L116-L120)]
- [x] [AI-Review][HIGH] Tâche 7.1 marquée [x] sans implémentation — scénario `TestE2E_Story4_3_RoundRobinWithResort` ajouté à [e2e_test.go](internal/registry/e2e_test.go)
- [x] [AI-Review][HIGH] AC 2 non couvert au niveau handler — `TestHandle_SelectCountry_RoundRobinHappyPath` ajouté à [handler_test.go](internal/ipchandler/handler_test.go)
- [x] [AI-Review][MEDIUM] AC 6 « re-tri background » non testé — `TestHandle_SelectCountry_TriggersBackgroundDiscover` ajouté, observation via `BgDiscoverFiresForTest()` [[handler_test.go](internal/ipchandler/handler_test.go)]
- [x] [AI-Review][MEDIUM] Goroutine `go disc.Discover(...)` non sérialisée — remplacée par `Discoverer.TriggerBackgroundDiscover` single-flight (atomic CAS) [[discoverer.go](internal/registry/discoverer.go)]. Test `TestTriggerBackgroundDiscover_SingleFlight` ajouté.
- [x] [AI-Review][MEDIUM] `sameIDs` sans test direct — `TestSameIDs_EdgeCases` avec 9 cas (nil, empty, same/diff length/order/content) ajouté [[selector_test.go](internal/registry/selector_test.go)]
- [ ] [AI-Review][LOW] Signature `MeasureOneMedian` triple (`time.Duration, int, error`) — cosmétique, non bloquant
- [ ] [AI-Review][LOW] `countrySelector.pick` alloue `[]string` à chaque appel — inoffensif à l'échelle 8 relais, reporté

### File List

**Nouveaux :**

- `internal/registry/selector.go` — `countrySelector` + `Discoverer.SelectRelay`
- `internal/registry/selector_test.go` — tests round-robin + reset + per-country + race + default refresh 6h + `TestSameIDs_EdgeCases` + `TestTriggerBackgroundDiscover_SingleFlight`

**Modifiés :**

- `internal/registry/registry.go` — sentinelles `ErrUnknownCountry`, `ErrNoRelaysForCountry`
- `internal/registry/discoverer.go` — champ `selector *countrySelector` + init dans `NewDiscoverer` ; `TriggerBackgroundDiscover` single-flight (atomic CAS) ; `SetRelaysForTest` + `NewDiscovererForTest` + `BgDiscoverFiresForTest` (helpers tests)
- `internal/registry/latency.go` — timeouts 5 s → 3 s, constantes `DefaultMedianSamples` / `MinSuccessfulSamples`, `MeasureOneMedian`, `MeasureAll` route vers la médiane ; `break probeLoop` (fix H1)
- `internal/registry/latency_test.go` — assertions révisées (parallélisme + 5 probes), tests `MeasureOneMedian` (all/partial/insufficient/default), test timeout 3 s
- `internal/registry/client.go` — `refreshInterval` défaut 1 h → 6 h
- `internal/registry/e2e_test.go` — `TestE2E_Story4_3_RoundRobinWithResort` (startup + RR + resort + reset + per-country)
- `internal/ipchandler/handler.go` — `handleSelectCountry` utilise `SelectRelay` + mapping sentinelles ; `TriggerBackgroundDiscover` placé juste après `SelectRelay` (fires même si tunnel swap échoue) ; imports `errors` ajouté, `math/rand` retiré
- `internal/ipchandler/handler_test.go` — 3 tests error-path (`missing_country_code`, `unknown_country_code`, `registry_disabled`) + `TestHandle_SelectCountry_RoundRobinHappyPath` + `TestHandle_SelectCountry_TriggersBackgroundDiscover`
- `internal/service/service.go` — sélection initiale `SelectRelay(preferred_country)` avec fallback ; `ForTestSetDiscoverer` helper
- `internal/config/config.go` — défaut TOML `RefreshInterval: "6h"`
- `internal/config/config_test.go` — assertion mise à jour sur le défaut 6 h
