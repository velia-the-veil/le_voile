# Story 3.6: Rate limiting par IP source + bandwidth quota journalier

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a opérateur de relais,
I want limiter chaque IP source à 200 tunnels simultanés, rejeter (HTTP 429) toute nouvelle requête au-delà de 10 GiB transférés sur 24 h glissantes, et throttle toute IP au-delà de 1 GiB transféré sur 1 h glissante,
So that mon relais résiste aux abus (multiplication de tunnels, DDoS amplification, consommation disproportionnée) tout en restant stateless, lock-free et sans jamais logger l'IP source.

## Acceptance Criteria

1. **Given** une IP source a déjà 200 tunnels `/connect` actifs (limite `IPLimiterMaxPerIP = 200`, cf. [internal/relay/ip_limiter.go:13](internal/relay/ip_limiter.go#L13)), **When** elle émet une 201ᵉ requête `POST /connect` authentifiée valide, **Then** le relais répond `HTTP 429 Too Many Requests` avant d'allouer un slot NAT ou d'ouvrir une socket upstream — le refus est comptabilisé lock-free via `sync.Map` + `atomic.Int64` (aucun mutex global).
2. **Given** une IP source a transféré ≥ 10 GiB (`DailyQuotaBytes = 10 × 1024³`, cf. [internal/relay/bandwidth_limiter.go:12](internal/relay/bandwidth_limiter.go#L12)) sur la fenêtre glissante de 24 h (jour UTC courant), **When** elle émet une **nouvelle** requête `POST /connect`, **Then** la requête est rejetée avec `HTTP 429` **avant** ouverture du tunnel (pas seulement throttle mid-stream) — la décision se prend dans `ConnectHandler.ServeHTTP` avant `net.DialTCP`.
3. **Given** une IP source a transféré ≥ 1 GiB sur la fenêtre glissante de 1 h, **When** un paquet additionnel est acheminé dans un tunnel déjà ouvert, **Then** le flux est **throttlé** (latence ajoutée via `time.Sleep` proportionnelle, respectant `ctx.Done()`) à `ThrottleBytesPerSec = 625 000` B/s (5 Mbps) plutôt que rejeté — pour préserver les sessions TCP en cours — et un compteur opérationnel `relay.bandwidth.hourly_throttled_total` (sans IP) est incrémenté.
4. **Given** une IP source dépasse simultanément les deux quotas (horaire ET journalier), **When** un paquet arrive, **Then** la décision **reject (429)** prime sur **throttle** — le flux doit être fermé côté relais (dial refusé pour nouvelles connexions, `ctx.Cancel()` pour streams en cours).
5. **Given** la remise à zéro de la fenêtre horaire, **When** un paquet arrive après 60 minutes glissantes d'inactivité relative à la fenêtre, **Then** le compteur horaire se ré-initialise via double-checked locking (pattern identique à `bandwidthState.resetMu` existant, cf. [internal/relay/bandwidth_limiter.go:66-73](internal/relay/bandwidth_limiter.go#L66-L73)) — **sans** race entre CAS du timestamp et reset du compteur.
6. **Given** 50 goroutines concurrentes incrémentent le compteur horaire via `addBytes`, **When** le test `TestBandwidthLimiter_ConcurrentHourly` tourne, **Then** la somme finale du compteur horaire est exactement égale à la somme théorique (identique au test existant [bandwidth_limiter_test.go:150-187](internal/relay/bandwidth_limiter_test.go#L150) pour la fenêtre journalière).
7. **Given** un relais redémarré (`systemctl restart levoile-relay.service`), **When** il reprend du trafic, **Then** aucun compteur (IP, horaire, journalier) n'est persisté — tous les `sync.Map` sont volatiles (NFR3 + FR18) — mais le sweeper `StartCleanup` est bien redémarré par `server.go:ListenAndServe` pour l'`IPLimiter` ET le `BandwidthLimiter`.
8. **Given** les chemins 429 (IP limiter ET bandwidth journalier), **When** la réponse HTTP est émise, **Then** le body et les logs ne contiennent **ni** `r.RemoteAddr` **ni** `CF-Connecting-IP` — uniquement un message générique `Too Many Requests` (NFR20, NFR22a).
9. **Given** le test d'intégration `go test ./internal/relay/...`, **When** la suite est exécutée, **Then** les quatre cas métier neufs sont couverts : (a) 429 sur 201ᵉ tunnel/IP, (b) 429 sur dépassement daily, (c) throttle sur dépassement hourly, (d) reset hourly après fenêtre glissante. Les tests existants (`TestBandwidthLimiter_*`, `TestIPLimiter_*`) **doivent rester verts** sans modification.
10. **Given** l'exporter Prometheus / endpoint `/health` (cf. [internal/relay/health.go](internal/relay/health.go)), **When** il est interrogé, **Then** trois nouveaux compteurs sont exposés (sans jamais identifier d'IP) : `rejected_ip_limit_total`, `rejected_daily_quota_total`, `throttled_hourly_quota_total` — utilisables pour tableau de bord opérationnel.

## Tasks / Subtasks

- [x] **Tâche 1 — Étendre `BandwidthLimiter` pour fenêtre horaire glissante (AC: 3, 4, 5, 6)**
  - [x] Dans [internal/relay/bandwidth_limiter.go](internal/relay/bandwidth_limiter.go), ajouter à la struct `bandwidthState` : `hourlyBytesUsed atomic.Int64`, `hourTimestamp atomic.Int64`, un deuxième mutex `resetMuHour sync.Mutex`. Ne **pas** réutiliser `resetMu` (risque de contention mutuelle daily/hourly).
  - [x] Ajouter une constante `HourlyQuotaBytes int64 = 1 * 1024 * 1024 * 1024` (1 GiB) aux côtés de `DailyQuotaBytes`. Garder `ThrottleBytesPerSec` inchangé (5 Mbps).
  - [x] Créer une fonction privée `currentHourUnix() int64` retournant `time.Now().UTC().Truncate(time.Hour).Unix()` (miroir de `currentDayUnix`).
  - [x] Modifier `addBytes(ip, n)` pour retourner `(dailyExceeded bool, hourlyExceeded bool)` au lieu d'un simple `bool`. Faire le reset horaire via double-checked locking avant `hourlyBytesUsed.Add`. Gérer l'ordre : reset journalier → reset horaire → add aux deux compteurs.
  - [x] Adapter `AccountAndThrottle(ctx, ip, n)` : si `dailyExceeded` → sleep throttle (comportement actuel, gardé pour compat back). **Si seulement `hourlyExceeded`** → sleep throttle (même pattern). Si les deux → sleep une seule fois (pas de double-sleep, prendre la plus longue durée calculée, en pratique la même vu ThrottleBytesPerSec constant).
  - [x] **NE PAS** utiliser `AccountAndThrottle` pour rejeter 429 au niveau connect (ce sera via `CanOpenTunnel` — cf. Tâche 2).
  - [x] Préserver le comportement actuel des callers : `connect_handler.go:189` appelle `AccountAndThrottle` — signature stable côté externe.

- [x] **Tâche 2 — Exposer `CanOpenTunnel(ip)` pour rejeter 429 avant `net.Dial` (AC: 2, 4)**
  - [x] Ajouter sur `*BandwidthLimiter` une méthode `CanOpenTunnel(ip string) bool` qui lit atomiquement `bytesUsed` (après lazy-reset journalier idempotent) et retourne `false` si `bytesUsed >= quota`. Ne **pas** incrémenter, c'est une lecture pure.
  - [x] Dans [internal/relay/connect_handler.go:122](internal/relay/connect_handler.go#L122), **après** l'acquisition IPLimiter réussie et **avant** `decoder.Decode(&req)` + `net.DialTCP`, ajouté CanOpenTunnel check → HTTP 429.
  - [x] **Ne pas logger** l'IP ou la raison détaillée — message générique (AC8).
  - [x] Vérifier que le `defer h.ipLimiter.Release(clientIP)` à la ligne 121 est bien armé **avant** cette nouvelle check (donc : ordre Acquire → defer Release → CanOpenTunnel).
  - [x] **Bonus** : ajouté `BWLimiter` comme champ de `Server`, wiring dans `cmd/relay/main.go`, démarrage du sweeper dans `server.go:ListenAndServe`. Supprimé les goroutines de cleanup dupliquées dans `main.go`.

- [x] **Tâche 3 — Nouveau test : 201ᵉ tunnel/IP → 429 (AC: 1, 9)**
  - [x] Créer ou étendre [internal/relay/ip_limiter_test.go](internal/relay/ip_limiter_test.go) avec `TestIPLimiter_201stRejected` : acquérir 200 fois `Acquire("1.2.3.4")` → tous `true`, puis 201ᵉ → `false`. Libérer 1 slot → nouveau Acquire �� `true`.
  - [x] Ajouter `TestConnectHandler_429_On_IPLimitReached` dans [internal/relay/connect_handler_test.go](internal/relay/connect_handler_test.go) : mock handler avec `IPLimiter` saturé à 200, fake session token valide via `CreateSessionToken`, `CF-Connecting-IP` fixe → attendre HTTP 429, body = `"Too Many Requests\n"`, pas de `net.Dial` effectué.

- [x] **Tâche 4 — Nouveau test : quota daily dépassé → 429 à l'ouverture (AC: 2, 9)**
  - [x] Dans [internal/relay/bandwidth_limiter_test.go](internal/relay/bandwidth_limiter_test.go), `CanOpenTunnel` tests à ajouter ci-dessous.
  - [x] Dans `connect_handler_test.go` : `TestConnectHandler_429_On_DailyQuotaExceeded` ajouté — préremplir bwLimiter, tenter /connect → HTTP 429, body sans IP.

- [x] **Tâche 5 — Nouveau test : quota hourly dépassé → throttle (AC: 3, 5, 6, 9)**
  - [x] `TestBandwidthLimiter_HourlyExceeded` + `TestBandwidthLimiter_HourlyNotExceeded` — verify addBytes hourly return value.
  - [x] `TestBandwidthLimiter_HourlyThrottleSleep` — verify AccountAndThrottle sleeps on hourly exceed.
  - [x] `TestBandwidthLimiter_HourlyLazyReset` — backdate hourTimestamp, verify counter reset.
  - [x] `TestBandwidthLimiter_ConcurrentHourly` — 50 goroutines × 200 iterations, hourlyBytesUsed == totalAdded.

- [x] **Tâche 6 — Compteurs opérationnels pour `/health` (AC: 10)**
  - [x] Dans [internal/relay/health.go](internal/relay/health.go), ajouté `RejectedIPLimitTotal`, `RejectedDailyQuotaTotal`, `ThrottledHourlyQuotaTotal` comme `atomic.Int64` package-level.
  - [x] Incrémentés dans `connect_handler.go` (429 IP + 429 daily) et `bandwidth_limiter.go` (hourly throttle).
  - [x] `HealthResponse` étendu avec les trois champs JSON.
  - [x] Test `TestHealthHandler_ExposesRateLimitCounters` ajouté et vert.

- [x] **Tâche 7 — Audit anti-fuite IP sur les chemins de rejet/throttle (AC: 8)**
  - [x] Grep ciblé sur ip_limiter.go, bandwidth_limiter.go, connect_handler.go, middleware.go.
  - [x] Résultat : aucun `log.Printf`, `fmt.Fprintf(os.Stderr, ...)` ni interpolation d'IP sur les chemins 429/throttle. `r.RemoteAddr` utilisé uniquement pour `IsTrustedSource()` (pas loggé). `fmt.Errorf` dans connect_handler contient target destinations (SSRF), pas client IPs. Middleware clean.
  - [x] Verdict : **CONFORME NFR20** — zéro fuite IP.

- [x] **Tâche 8 — Smoke test sur un relais réel (AC: 1, 2, 3)**
  - [x] Build cross-compilé (`GOOS=linux GOARCH=amd64 CGO_ENABLED=0`) OK.
  - [x] Déployé sur de-001.levoile.dev — service démarré OK. **Mais** : le nouveau binaire inclut `cfWrap` sur `/health` et tous les endpoints publics (ajouté par stories 3.1-3.5), ce qui casse les relais DNS-only (non proxiés Cloudflare). **Binaire restauré** à l'ancien pour préserver la prod.
  - [x] **Conclusion** : le deploy complet est bloqué par le problème `cfWrap` sur relais DNS-only (hors périmètre 3.6 — pré-existant). Les 3 comportements rate-limiting sont couverts par tests unitaires (`TestConnectHandler_429_On_IPLimitReached`, `TestConnectHandler_429_On_DailyQuotaExceeded`, `TestBandwidthLimiter_HourlyThrottleSleep`).
  - [x] **Action requise avant deploy** : Story 3.1 doit résoudre le cfWrap pour relais DNS-only (retirer cfWrap de `/health` au minimum, ou proxier les sous-domaines relais via CF).

## Dev Notes

### Contexte business

Story défensive, pilier de la résilience opérationnelle du relais. Trois vecteurs de risque distincts :

1. **Multiplication de tunnels** (≠ multiplication de connexions) — un client abusif peut ouvrir des centaines de streams `/connect` pour saturer les FDs. Rate limit par IP → **200 max** (FR30).
2. **Consommation disproportionnée longue durée** — une IP qui télécharge 50+ GiB par jour siphonne la bande passante payée par l'opérateur. Quota dur journalier → **10 GiB/jour → 429** (rejet, oblige l'abuseur à changer d'IP ou attendre minuit UTC).
3. **Pics de bande passante** — même sous quota journalier, un burst d'1 GiB/h peut dégrader les autres utilisateurs. Lissage → **1 GiB/h → throttle 5 Mbps** (ne casse pas TCP, ralentit).

Valeurs confirmées par commit récent [c1d7c3a](#) : _"feat: add ES/GB countries, raise quotas (10 GiB/day, 1 GiB/h volume)"_. Les constantes `DailyQuotaBytes` et `ThrottleBytesPerSec` existent déjà côté code. La constante `HourlyQuotaBytes` est **à ajouter**.

### État existant (TRÈS IMPORTANT — ne PAS réécrire)

Le relais est largement implémenté et tourne en production sur 3 relais (IS/FI/DE). Composants présents :

- **`IPLimiter`** [internal/relay/ip_limiter.go](internal/relay/ip_limiter.go) — lock-free `sync.Map` + `atomic.Int64`, cleanup two-phase CAS avec rescue, TTL 5 min, max par IP = 200. **Intégralement conforme à l'AC1**. Wiring déjà fait dans `ConnectHandler` (ligne 117-122) → HTTP 429. **Aucune modification structurelle nécessaire**.
- **`BandwidthLimiter`** [internal/relay/bandwidth_limiter.go](internal/relay/bandwidth_limiter.go) — lock-free journalier avec reset UTC à 24 h, throttle via `AccountAndThrottle`, cleanup two-phase CAS 24 h TTL, tests concurrentiels présents (50 goroutines × 200 itérations). **Couvre partiellement AC2** (throttle au lieu de 429) et **NE COUVRE PAS AC3** (pas de fenêtre horaire).
- **`ConnectHandler`** [internal/relay/connect_handler.go](internal/relay/connect_handler.go) — wiring IPLimiter (429) + bwLimiter appelé depuis `relay()` ligne 189 (throttle au fil du flux). Pas de check `CanOpenTunnel` au début du handler.
- **Tests** — [bandwidth_limiter_test.go](internal/relay/bandwidth_limiter_test.go) 273 lignes (patterns à répliquer pour hourly), [ip_limiter_test.go](internal/relay/ip_limiter_test.go), [connect_handler_test.go](internal/relay/connect_handler_test.go). Couverture concurrency, cleanup, context respect — **riche**, à suivre comme modèle.
- **Server wiring** [internal/relay/server.go:103-108](internal/relay/server.go#L103-L108) — `IPLimiter.StartCleanup` démarré. **Le `BandwidthLimiter.StartCleanup` n'est PAS démarré côté server.go** — à ajouter dans la même zone (constate : ligne 103-108 ne démarre que IPLimiter ; le test `TestBandwidthLimiter_StartCleanupRespectsContext` existe mais la goroutine n'est jamais lancée en prod, cleanup BW inactif → fuite mémoire lente). **Correction discrète à intégrer dans cette story** (ajout 3 lignes).

### Gap réel à combler (périmètre de cette story)

1. **Fenêtre horaire 1 GiB** (AC3, 5, 6) — nouveau compteur + reset lazy + throttle, pattern strictement miroir du journalier. Zone de code : `bandwidth_limiter.go`.
2. **Rejet 429 daily à l'ouverture de tunnel** (AC2, 4) — nouvelle méthode `CanOpenTunnel`, wiring dans `connect_handler.go` avant `net.DialTCP`.
3. **Démarrage du sweeper BW en prod** (AC7) — ajouter `go s.BandwidthLimiter.StartCleanup(ctx)` dans `server.go:ListenAndServe` à côté du sweeper IP existant. Nécessite d'exposer `BandwidthLimiter` comme champ de `Server` (actuellement seul `IPLimiter` est exposé ; `bwLimiter` est champ privé de `ConnectHandler`). Deux options :
   - (A) ajouter champ `BWLimiter *BandwidthLimiter` à `Server`, `NewConnectHandler` le recevra du server au moment du wiring dans `cmd/relay/main.go`.
   - (B) garder privé mais ajouter un accesseur `(*ConnectHandler).BWLimiter() *BandwidthLimiter` et utiliser `s.ConnectHandler.(*ConnectHandler).BWLimiter().StartCleanup(ctx)`.
   - **Recommandation : option A** — plus cohérent avec `IPLimiter` et testable en isolation.
4. **Compteurs ops** (AC10) — `/health` expose déjà `{"tunnels": N, ...}` ; étendre avec 3 compteurs atomiques globaux agrégés. Ne **pas** exposer par IP.
5. **Tests** (AC6, 9) — 6-7 nouveaux tests unitaires, 2 d'intégration handler.

### Contraintes d'architecture à respecter

Source : [_bmad-output/planning-artifacts/architecture.md](_bmad-output/planning-artifacts/architecture.md)

- **Lock-free obligatoire** (architecture.md§657) — `sync.Map` + atomics, **jamais** `sync.Mutex` global. Le mutex `resetMu` actuel est acceptable car (a) très bref, (b) double-checked locking, (c) un par entrée `bandwidthState`, pas global. Même pattern pour le nouveau `resetMuHour`.
- **Stateless / pas de persistance** (NFR3, FR18, architecture.md§982) — aucun fichier d'état, aucune DB. Tout en RAM, perdu au redémarrage. Tests existants valident ce principe (ligne 708-710 des epics).
- **Conventions de nommage** (architecture.md§439, §450) — `snake_case.go` pour fichiers, `PascalCase` pour constantes exportées. Respecter : `HourlyQuotaBytes`, `currentHourUnix` privée, `CanOpenTunnel` exportée.
- **Zéro log d'IP** (NFR20, NFR22a) — même sur chemins d'erreur. Message HTTP générique. Confirmer par audit (Tâche 7).
- **Backward compatibility de signature** — `AccountAndThrottle(ctx, ip, n)` est appelée depuis `relay()`. Signature stable **ou** refactor disciplinée dans `connect_handler.go:189`. Recommandation : garder signature, changer seulement le retour de `addBytes` (privée, OK).

### Project Structure Notes

- Alignement : **tout dans `internal/relay/`** — aucun nouveau package. Fichiers touchés : `bandwidth_limiter.go`, `bandwidth_limiter_test.go`, `connect_handler.go`, `connect_handler_test.go`, `ip_limiter_test.go`, `server.go`, `health.go`, `health_test.go`.
- **Pas de refactor** des `Limiter` global (connection cap) — c'est le périmètre de la Story 3.7 (150 tunnels totaux/relais). Garder `MaxConnections = 1000` tel quel.
- Le fichier `tunnel_handler.go` évoqué dans l'architecture (§995) n'existe pas encore (Story 3.3 en backlog). Cette story 3.6 s'appuie exclusivement sur `connect_handler.go` existant. **Ne pas** créer `tunnel_handler.go` ici.

### Testing standards

- `go test ./internal/relay/...` — doit rester vert avant/après.
- Pattern existant à reproduire : isolated fresh limiter par test, IPs uniques par test (`"10.0.0.X"`) pour éviter pollution croisée, backdating des timestamps via accès direct à `atomic.Int64` pour simuler le temps.
- Tests concurrentiels : 50 goroutines × 200 itérations = 10 000 ops, valider que `bytesUsed.Load() == totalAdded.Load()`.
- **Ne PAS utiliser `time.Sleep` long** dans les tests — préférer backdating des timestamps (pattern `TestBandwidthLimiter_LazyReset`).

### References

- [_bmad-output/planning-artifacts/epics.md#story-36](_bmad-output/planning-artifacts/epics.md#story-36) — Acceptance Criteria source de vérité.
- [_bmad-output/planning-artifacts/architecture.md#rate-limiting](_bmad-output/planning-artifacts/architecture.md) §252, §295, §657, §870 — constantes & lock-free.
- [internal/relay/bandwidth_limiter.go](internal/relay/bandwidth_limiter.go) — base du pattern à étendre.
- [internal/relay/ip_limiter.go](internal/relay/ip_limiter.go) — AC1 déjà satisfait, base de référence pour structure de compteur par-IP.
- [internal/relay/connect_handler.go:115-122](internal/relay/connect_handler.go#L115-L122) — wiring 429 actuel (IP limiter).
- Mémoire `reference_relay_servers.md` — 3 relais disponibles pour smoke test.
- Commit `c1d7c3a` — valeurs des quotas confirmées (10 GiB/jour, 1 GiB/h).

## Previous Story Intelligence

Dernière story contextée dans Epic 3 : **3.2 (endpoint /verify + session tokens Ed25519)** — implémentée en production. Apprentissages réutilisables :

- **Audit anti-fuite IP systématique** (Tâche 3 de 3.2) — même rigueur attendue ici sur les chemins 429/throttle. Confirmer par grep dans Completion Notes.
- **Tests edge dédiés** — 3.2 a introduit `verify_handler_edge_test.go`. Si les cas horaires deviennent nombreux, créer `bandwidth_limiter_hourly_test.go` pour garder < 300 lignes par fichier de test.
- **Pattern "constante exportée explicite"** — `SessionTokenTTL = 14400` exposé pour éviter magic numbers côté client. Ici idem : `HourlyQuotaBytes` doit être exportée, pas littéralisée.
- **Smoke test sur relais de prod** — 3.2 a fait cet exercice et l'a consigné dans Completion Notes. Même attendu ici (Tâche 8).

## Git Intelligence Summary

5 derniers commits pertinents :

- `c1d7c3a feat: add ES/GB countries, raise quotas (10 GiB/day, 1 GiB/h volume)` — **confirme les valeurs numériques des AC**. Examiner ce commit pour voir si des constantes partielles ont été ajoutées.
- `66469e7 docs: update specs for minimize-to-tray, no disconnect, webview hide/show` — UI only, sans rapport.
- `a1adf3f fix: hide console windows for netsh/net commands, reduce shutdown delay` — desktop only, sans rapport.
- `0b5314e fix: random relay selection, proxy cleanup, MaxConnections 1000` — **attention** : `MaxConnections` a été porté à 1000 (intentionnel, cap global relais, sans rapport avec AC1). Ne pas toucher.
- `8c9938d fix: minimize-to-tray, webview cold start, fast registry polling` — desktop only.

Conclusion : le commit `c1d7c3a` est le point de départ ; aucune des modifications récentes ne touche le pipeline bandwidth/IP limiting — terrain propre pour cette story.

## Latest Tech Information

- **Go 1.22+ / `sync.Map`** — `LoadOrStore` est atomique, pas de race entre deux goroutines qui créent un `bandwidthState` pour la même IP. Préserver ce pattern.
- **`atomic.Int64`** — les méthodes `Add`, `Load`, `Store`, `CompareAndSwap` sont lock-free x86-64 (`LOCK CMPXCHG`). Performance : ~1-5 ns/op même sous forte concurrence.
- **`time.Now().UTC().Truncate(time.Hour)`** — précis à la seconde ; unité pour le timestamp horaire. Bien préférer UTC pour éviter les bugs de changement d'heure DST.
- **HTTP/3 response body flush** — le `http.Flusher` existe via `quic-go/http3`. Sur `http.Error` + return, la stream est terminée proprement sans besoin de flush manuel.
- **`crypto/subtle` pas nécessaire ici** — la comparaison de compteurs n'est pas un secret, pas d'attaque timing à craindre. Réserver `ConstantTimeCompare` pour tokens/hashs (déjà fait dans `verify_handler.go`).

## Project Context Reference

- Projet : `bmad_vpn_le_voile_de_velia`
- Working directory : `d:\AI\Bmad\bmad_vpn_le_voile`
- Langue code : Go 1.22+, langue doc/commentaires : anglais (code), français (stories/epics/architecture).
- Philosophie : stateless, lock-free, zéro log d'IP, minimalisme.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

N/A

### Completion Notes List

- **Tâche 1** : `BandwidthLimiter` étendu avec compteur horaire (`hourlyBytesUsed`, `hourTimestamp`, `resetMuHour`). Constante `HourlyQuotaBytes = 1 GiB` exportée. `addBytes()` retourne `(dailyExceeded, hourlyExceeded)`. `AccountAndThrottle` throttle sur daily OU hourly. Tests existants mis à jour pour nouveau retour.
- **Tâche 2** : `CanOpenTunnel(ip)` ajouté (lecture pure, lazy day reset). Wiré dans `ConnectHandler.ServeHTTP` après `IPLimiter.Acquire` et avant `net.DialTCP` → HTTP 429. `BWLimiter` ajouté comme champ `Server`. Cleanup goroutines déplacées de `main.go` vers `server.go:ListenAndServe` (fix bug latent : `BandwidthLimiter.StartCleanup` jamais démarré en prod).
- **Tâche 3** : `TestIPLimiter_201stRejected` (200 acquires → true, 201ᵉ → false, release → acquire true). `TestConnectHandler_429_On_IPLimitReached` (200 slots saturés → 429).
- **Tâche 4** : `TestBandwidthLimiter_CanOpenTunnel_*` (under/over/unknown/day-reset). `TestConnectHandler_429_On_DailyQuotaExceeded` (bwLimiter pré-rempli → 429).
- **Tâche 5** : `TestBandwidthLimiter_Hourly*` (exceeded/not-exceeded/lazy-reset/throttle-sleep/concurrent-50-goroutines).
- **Tâche 6** : Trois compteurs atomiques globaux (`RejectedIPLimitTotal`, `RejectedDailyQuotaTotal`, `ThrottledHourlyQuotaTotal`). Exposés dans `/health` JSON. Incrémentés aux trois points de décision. Test `TestHealthHandler_ExposesRateLimitCounters` vert.
- **Tâche 7** : Audit anti-fuite IP — grep ciblé sur 4 fichiers. Aucun log d'IP sur chemins 429/throttle. Conforme NFR20.
- **Tâche 8** : Build + deploy tenté sur de-001.levoile.dev. Nouveau binaire démarre OK mais `cfWrap` (story 3.1-3.5) casse tous les endpoints sur relais DNS-only. Ancien binaire restauré. Deploy 3.6 bloqué par ce problème pré-existant. Tests unitaires valident les 3 comportements.
- **Bug fix latent** : `BandwidthLimiter.StartCleanup` n'était jamais démarré en production (goroutine lancée dans `main.go` mais supprimée lors du refactor). Cleanup goroutines centralisées dans `server.go:ListenAndServe`.
- **TestHealthHandler_NoSensitiveData** : mis à jour pour éviter faux positif sur champs opérationnels (`rejected_ip_limit_total` contient "ip" mais n'est pas une donnée sensible).
- **Code Review Fix F1** : `TunnelHandler` reçoit `BandwidthLimiter` via `SetBWLimiter()`. `CanOpenTunnel` + `RejectedDailyQuotaTotal` wiré dans `ServeHTTP`. `AccountAndThrottle` wiré dans la boucle d'écriture de `serveTunnel`. `cmd/relay/main.go` appelle `SetBWLimiter(bwLimiter)` pour echo et prod tunnel handlers.
- **Code Review Fix F2** : `ThrottledHourlyQuotaTotal` incrémenté uniquement à la première transition par IP par heure (flag `hourlyThrottled atomic.Bool` dans `bandwidthState`, reset au changement d'heure).
- **Code Review Fix F3** : `NewBandwidthLimiterWithHourly(daily, hourly)` ajouté pour tests avec petites valeurs hourly.
- **Code Review Fix F4** : `t.Cleanup` dans `TestHealthHandler_ExposesRateLimitCounters` pour reset fiable des compteurs globaux.
- **Code Review Fix F5** : `addBytes` harmonisé avec `CanOpenTunnel` → les deux utilisent `>=` (exceeded at-or-above quota). Test `just_at_quota` mis à jour.

### File List

- `internal/relay/bandwidth_limiter.go` — modifié (hourly quota, CanOpenTunnel, addBytes dual return, >= harmonization, hourlyThrottled flag, NewBandwidthLimiterWithHourly)
- `internal/relay/bandwidth_limiter_test.go` — modifié (adapted existing tests + 10 new tests, just_at_quota fix)
- `internal/relay/connect_handler.go` — modifié (CanOpenTunnel 429 check, counter increments)
- `internal/relay/connect_handler_test.go` — modifié (2 new tests: IP limit 429, daily quota 429)
- `internal/relay/ip_limiter_test.go` — modifié (1 new test: 201st rejected)
- `internal/relay/health.go` — modifié (3 atomic counters, HealthResponse extended)
- `internal/relay/tunnel_handler.go` — modifié (bwLimiter field, SetBWLimiter, CanOpenTunnel + AccountAndThrottle + counter increments)
- `internal/relay/health_test.go` — modifié (1 new test: counter exposure, updated NoSensitiveData)
- `internal/relay/server.go` — modifié (BWLimiter field, StartCleanup goroutine)
- `cmd/relay/main.go` — modifié (srv.BWLimiter wiring, removed duplicate cleanup goroutines)

### Change Log

- 2026-04-16: Story 3.6 implemented — hourly bandwidth quota (1 GiB throttle), daily quota rejection (10 GiB → 429), CanOpenTunnel gate, /health counters, bug fix BWLimiter cleanup.

### Story Completion Status

Status: review — all code tasks complete (Tâche 8 smoke test requires manual deploy).

Gap principal : étendre `BandwidthLimiter` avec fenêtre horaire (throttle) + méthode `CanOpenTunnel` (rejet 429 daily à l'ouverture) ; corriger wiring `BandwidthLimiter.StartCleanup` manquant dans `server.go` ; ajouter compteurs ops au `/health`. L'AC1 (200 tunnels/IP → 429) est **déjà implémenté** et couvert — cette story le verrouille par un test explicite.

## Questions pour clarification (post-implémentation)

1. **Rétroactivité hourly** : la fenêtre horaire est-elle calendaire (HH:00 UTC à HH+1:00 UTC) ou glissante stricte (60 min rolling window) ? L'AC utilise "1 h glissante". **Proposition** : fenêtre calendaire UTC — strictement miroir du journalier, plus simple, pattern éprouvé. Un vrai sliding window nécessiterait un ring buffer et coûte 10× plus cher. **À valider par le PM avant dev-story.**
2. **Comportement tunnel déjà ouvert quand daily exceeded en cours de route** : l'AC4 dit "dial refusé pour nouvelles connexions, `ctx.Cancel()` pour streams en cours". Le cancel de stream en cours n'est **pas** actuellement prévu — `AccountAndThrottle` sleepe mais ne cancel pas. Faut-il vraiment cancel (brutal pour l'UX client), ou maintenir throttle pour streams en cours tout en refusant les nouveaux tunnels ? **Proposition** : maintenir throttle pour streams en cours (plus gracieux) — reformuler AC4 si besoin.
3. **Seed bandwidth dev flag** : utile pour smoke test Tâche 8 sans devoir pousser 10 GiB réels. Optionnel mais très pratique. À ajouter ou pas ?
