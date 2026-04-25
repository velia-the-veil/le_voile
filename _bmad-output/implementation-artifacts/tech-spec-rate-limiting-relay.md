---
title: 'Rate Limiting Relay Multi-Utilisateur'
slug: 'rate-limiting-relay'
created: '2026-03-16'
status: 'ready-for-dev'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['go 1.26', 'quic-go v0.59.0', 'golang.org/x/time v0.15.0 (déjà en go.mod)']
files_to_modify: ['cmd/relay/main.go', 'internal/relay/ip_limiter.go', 'internal/relay/server.go', 'internal/relay/connect_handler.go', 'internal/relay/doh_handler.go']
files_to_create: ['internal/relay/rate_limiter.go', 'internal/relay/rate_limiter_test.go']
code_patterns: ['atomic counters (Limiter, IPLimiter)', 'sync.Map per-IP state avec cleanup CAS two-phase', 'nil-check optional dependencies', 'LimitMiddleware wrapper Acquire/defer Release', 'table-driven tests avec helpers (testKeys, testToken, newHandler)', 'cfValidator.ExtractClientIP via SplitHostPort pour IP sans port']
test_patterns: ['table-driven tests', 'httptest.NewRequest + httptest.NewRecorder', 'helpers partagés (testKeys, testToken, newHandler)', 'concurrency tests avec sync.WaitGroup + atomic', 'cleanup tests avec backdating lastSeen']
---

# Tech-Spec: Rate Limiting Relay Multi-Utilisateur

**Created:** 2026-03-16

## Overview

### Problem Statement

Le relay est actuellement mono-utilisateur. Pour l'ouvrir au public, il faut empêcher les abus de ressources (streams, DNS flood, bande passante) sans authentification, sur un serveur contraint : 1 CPU, 1 Go RAM, 1 To transfert/mois. Cible : ~10-20 utilisateurs actifs quotidiens.

### Solution

Activer l'IPLimiter existant avec limite à 50 streams CONNECT/IP, ajouter un rate limiter token bucket DoH (~100 req/min/IP), un bandwidth limiter par IP (3 Mbps total toutes connexions confondues), et ajuster les limites QUIC pour supporter ~20 utilisateurs simultanés.

### Scope

**In Scope:**

- Activer l'IPLimiter existant avec limite réduite à 50 streams CONNECT simultanés par IP
- Rate limiting DoH par IP via token bucket (`golang.org/x/time/rate`) — ~100 req/min, burst 20
- Bandwidth limiting par IP — throttle à 3 Mbps total par IP sur les streams CONNECT, burst calé sur la taille du buffer (32 Ko)
- Réduire `MaxIncomingStreams` QUIC de 1000 à 500 (per-connection, pas global — 500 est largement suffisant par client)
- Extraction de l'IP client pour le DoH handler (via `RemoteAddr`)
- Protection anti-slowloris : réduire `connectIdleTimeout` à 30s pour fermer les streams inactifs plus vite
- Tout hardcodé en constantes (pas de flags CLI)

**Out of Scope:**

- Authentification / comptes utilisateur
- Extension navigateur (spec séparée)
- Bannissement temporaire (on refuse juste les nouvelles connexions/requêtes)
- Flags CLI configurables pour les limites

## Context for Development

### Codebase Patterns

- **Atomic counters** : `IPLimiter` et `Limiter` utilisent `atomic.Int64` pour le lock-free counting
- **sync.Map** : `IPLimiter` utilise `sync.Map` pour le state per-IP avec cleanup CAS two-phase
- **Nil-check pattern** : le `ConnectHandler` vérifie `if h.ipLimiter != nil` avant d'appeler Acquire — le limiter est optionnel
- **Middleware pattern** : `LimitMiddleware` wraps handlers pour le global limiter — pattern réutilisable pour le DoH rate limiter
- **IP extraction** : `ConnectHandler` utilise `cfValidator.ExtractClientIP(r)` pour l'IP ; le DoH handler n'a aucune notion d'IP actuellement
- **Helper IP unifié** : Nécessaire pour normaliser l'extraction d'IP (sans port) entre tous les handlers — éviter les clés incohérentes dans les limiters

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `cmd/relay/main.go` | Point d'entrée — instancie les handlers, passe `nil` pour IPLimiter actuellement |
| `internal/relay/ip_limiter.go` | IPLimiter existant — limite connexions concurrentes par IP, cleanup périodique |
| `internal/relay/connect_handler.go` | Handler CONNECT — auth session token, relay bidirectionnel, supporte déjà IPLimiter |
| `internal/relay/doh_handler.go` | Handler DoH — forwarding DNS, fallback multi-upstream, aucun rate limiting |
| `internal/relay/server.go` | Server HTTP/3 — mux, config QUIC, `MaxIncomingStreams: 1000` |
| `internal/relay/limiter.go` | Global limiter — 150 connexions max, utilisé via `LimitMiddleware` |
| `internal/relay/middleware.go` | `LimitMiddleware` — wrapper Acquire/defer Release, pattern à réutiliser |
| `internal/relay/cfip.go` | `CloudflareIPValidator` — `ExtractClientIP` fait `SplitHostPort` pour retirer le port |
| `internal/relay/ip_limiter_test.go` | Tests complets IPLimiter : concurrence, cleanup, CAS rescue, double release |
| `internal/relay/connect_handler_test.go` | Tests ConnectHandler : auth pipeline, rate limit 429, SSRF, table-driven |

### Technical Decisions

- **Token bucket pour DoH** : `golang.org/x/time/rate` — permet burst court (navigateur ouvrant plusieurs onglets) tout en imposant un débit moyen. Plus adapté qu'un simple compteur sliding window. Burst à 20 pour absorber le cold start d'un navigateur (~15-20 requêtes DNS simultanées à l'ouverture).
- **Bandwidth via io.Reader wrapper** : Wrapper `rate.Limiter` sur le stream entier (pas par direction séparée). 3 Mbps = 375 Ko/s partagés entre toutes les connexions CONNECT d'une même IP. Burst calé à 32 Ko (taille du buffer de copie) pour éviter les burst initiaux abusifs sur connexions multiples.
- **IPLimiter à 50** : Un navigateur ouvre ~6-10 connexions parallèles par domaine. 50 donne de la marge pour un usage normal tout en bloquant l'abus.
- **MaxIncomingStreams à 500 (per-connection)** : `MaxIncomingStreams` dans quic-go est par connexion QUIC, pas global serveur. Chaque client a sa propre connexion. 500 streams par client est largement suffisant (~50 CONNECT + DoH/health) et sert de limite dure naturelle par client.
- **Global limiter inchangé à 150** : Gère uniquement les requêtes courtes (DoH ~5ms). Même à 20 users × 100 req/min, la concurrence réelle est ~33 requêtes. 150 est largement suffisant.
- **Anti-slowloris** : Réduire `connectIdleTimeout` de 120s à 30s. 120s × 50 streams = slots bloqués trop longtemps. 30s reste confortable pour la navigation web (les requêtes actives renouvellent le deadline).
- **Burst bandwidth contrôlé** : Burst du token bucket = 32 Ko (taille buffer), pas 625 Ko (1s de débit). Empêche 50 streams × 625 Ko = 30 Mo de burst instantané.

## Implementation Plan

### Tasks

- [ ] Task 1 : Helper d'extraction d'IP normalisé
  - File : `internal/relay/rate_limiter.go`
  - Action : Créer la fonction `ExtractIP(remoteAddr string) string` qui :
    - Appelle `net.SplitHostPort` pour retirer le port
    - Parse avec `netip.ParseAddr` et appelle `.Unmap()` pour canonicaliser IPv4-mapped IPv6
    - Retourne la string IP pure (ex: `"1.2.3.4"`)
  - Notes : Utilisé comme clé dans toutes les maps per-IP. Le `ConnectHandler` continue d'utiliser `cfValidator.ExtractClientIP()` pour le hash token — ce helper est pour les limiters uniquement.

- [ ] Task 2 : Structure per-IP unifiée `RateLimiter`
  - File : `internal/relay/rate_limiter.go`
  - Action : Créer la struct `RateLimiter` qui centralise le state per-IP :
    - Constantes : `DoHRateLimit = 100.0/60.0` (~1.67 req/s), `DoHBurst = 20`, `BandwidthRate = 375_000` (375 Ko/s = 3 Mbps), `BandwidthBurst = 32_768` (32 Ko), `MaxIPEntries = 10_000`
    - Struct interne `ipRateState` : champ `doh *rate.Limiter`, champ `bw *rate.Limiter`, champ `lastSeen atomic.Int64`, champ `markedForDeletion atomic.Bool`
    - Champ `ips sync.Map` (map[string]*ipRateState)
    - Champ `count atomic.Int64` pour tracker le nombre d'entrées (cap dur)
    - `NewRateLimiter() *RateLimiter`
    - `getOrCreate(ip string) (*ipRateState, bool)` — retourne l'état et `false` si le cap est atteint. Crée le `rate.Limiter` DoH et bandwidth lazily à la première utilisation.
  - Notes : Pattern similaire à `IPLimiter` avec `sync.Map` + atomics. Les `rate.Limiter` sont créés une seule fois par IP et réutilisés.

- [ ] Task 3 : Méthode `AllowDoH` sur `RateLimiter`
  - File : `internal/relay/rate_limiter.go`
  - Action : `AllowDoH(ip string) bool` — vérifie le rate limit DoH pour l'IP :
    - Appelle `getOrCreate(ip)`, retourne `false` si cap atteint
    - Appelle `state.doh.Allow()` (non-bloquant)
    - Met à jour `lastSeen`
  - Notes : Utilisé dans le middleware DoH. Non-bloquant pour ne pas ralentir les requêtes DNS.

- [ ] Task 4 : Méthode `BandwidthReader` et `BandwidthWriter` sur `RateLimiter`
  - File : `internal/relay/rate_limiter.go`
  - Action : Créer deux types :
    - `BandwidthReader` : struct wrappant un `io.Reader` + `*rate.Limiter` partagé. La méthode `Read(p []byte) (int, error)` appelle le reader interne puis `limiter.WaitN(ctx, n)` pour throttle après lecture.
    - `BandwidthWriter` : struct wrappant un `io.Writer` + `*rate.Limiter` partagé. La méthode `Write(p []byte) (int, error)` appelle le writer interne puis `limiter.WaitN(ctx, n)` pour throttle après écriture.
    - `RateLimiter.WrapReader(ctx context.Context, ip string, r io.Reader) io.Reader` — retourne un `BandwidthReader` avec le limiter partagé de l'IP
    - `RateLimiter.WrapWriter(ctx context.Context, ip string, w io.Writer) io.Writer` — retourne un `BandwidthWriter` avec le limiter partagé de l'IP
  - Notes : Le même `rate.Limiter` bandwidth est partagé entre toutes les connexions CONNECT d'une même IP (upload + download). `WaitN` bloque la goroutine (sleep) — pas de busy-wait, impact CPU négligeable. Si `getOrCreate` retourne cap atteint, retourne le reader/writer original sans throttle (fail-open pour les streams déjà authentifiés).

- [ ] Task 5 : Cleanup périodique dans `RateLimiter`
  - File : `internal/relay/rate_limiter.go`
  - Action : `StartCleanup(ctx context.Context)` — goroutine de cleanup périodique (60s) :
    - Même pattern CAS two-phase que `IPLimiter.cleanup()` : 1er cycle marque, 2e cycle supprime
    - Condition : `lastSeen` > 5 min ET pas d'activité récente
    - Décrémente `count` à chaque suppression
  - Notes : Doit démarrer **avant** que le serveur accepte des connexions.

- [ ] Task 6 : Modifier `IPLimiter` — constante à 50
  - File : `internal/relay/ip_limiter.go`
  - Action : Changer `IPLimiterMaxPerIP` de `200` à `50`
  - Notes : Changement d'une seule ligne. Les tests existants qui utilisent `IPLimiterMaxPerIP` s'adaptent automatiquement sauf `TestIPLimiter_AcquireUpToLimit` qui utilise `const limit int64 = 20` (indépendant).

- [ ] Task 7 : Middleware DoH rate limiting
  - File : `internal/relay/middleware.go`
  - Action : Ajouter `DoHRateLimitMiddleware(rl *RateLimiter, next http.Handler) http.Handler` :
    - Extrait l'IP avec `ExtractIP(r.RemoteAddr)`
    - Appelle `rl.AllowDoH(ip)`
    - Si refusé : `http.Error(w, "Too Many Requests", 429)` + return
    - Sinon : `next.ServeHTTP(w, r)`
  - Notes : Pattern identique à `LimitMiddleware` mais per-IP.

- [ ] Task 8 : Intégrer le rate limiter DoH dans le mux
  - File : `internal/relay/server.go`
  - Action :
    - Ajouter champ `RateLimiter *RateLimiter` sur la struct `Server`
    - Modifier le mux pour `/dns-query` : `DoHRateLimitMiddleware(s.RateLimiter, LimitMiddleware(s.Limiter, s.Handler))` — rate limit per-IP **avant** le global limiter
    - Démarrer `s.RateLimiter.StartCleanup(ctx)` **avant** `s.h3.ListenAndServe()` (avant accept)
    - Réduire `MaxIncomingStreams` de `1000` à `500`
  - Notes : L'ordre des middlewares est critique : per-IP d'abord pour ne pas consommer de slot global sur les requêtes rejetées. Le cleanup démarre avant le serveur pour éviter les races au startup.

- [ ] Task 9 : Intégrer le bandwidth limiter dans le relay CONNECT
  - File : `internal/relay/connect_handler.go`
  - Action :
    - Ajouter champ `rateLimiter *RateLimiter` sur `ConnectHandler`
    - Modifier `NewConnectHandler` pour accepter un `*RateLimiter`
    - Dans `ServeHTTP`, après l'authentification et avant le relay bidirectionnel, wrapper les streams :
      - `clientReader` → `rl.WrapReader(ctx, clientIP, clientReader)` (upload throttle)
      - `clientWriter` (le `http.ResponseWriter`) ne peut pas être wrappé directement → wrapper `dest.Read` côté write avec `rl.WrapWriter(ctx, clientIP, destWriter)` dans la fonction `relay()`
    - Modifier la fonction `relay()` pour accepter les wrappers throttlés
    - Réduire `connectIdleTimeout` de `120s` à `30s`
  - Notes : Le `rate.Limiter` bandwidth est partagé entre tous les streams de la même IP. Le `ConnectHandler` continue d'utiliser `cfValidator.ExtractClientIP()` pour le hash token, mais utilise la même IP (via `clientIP` déjà extrait) comme clé pour le bandwidth limiter.

- [ ] Task 10 : Wiring dans `main.go`
  - File : `cmd/relay/main.go`
  - Action :
    - Créer `rl := relay.NewRateLimiter()`
    - Créer `ipLimiter := relay.NewIPLimiter(relay.IPLimiterMaxPerIP)`
    - Passer `ipLimiter` au `NewConnectHandler` (au lieu de `nil`)
    - Passer `rl` au `NewConnectHandler`
    - Assigner `srv.RateLimiter = rl`
    - Mettre à jour le `buildTag`
  - Notes : Le `IPLimiter` (streams concurrents) et le `RateLimiter` (DoH + bandwidth) sont deux structures séparées avec des responsabilités distinctes.

- [ ] Task 11 : Tests unitaires `RateLimiter`
  - File : `internal/relay/rate_limiter_test.go`
  - Action : Écrire les tests suivants :
    - `TestExtractIP` : table-driven — `"1.2.3.4:1234"` → `"1.2.3.4"`, `"[::ffff:1.2.3.4]:1234"` → `"1.2.3.4"`, `"[::1]:80"` → `"::1"`, `"1.2.3.4"` (sans port) → `"1.2.3.4"`
    - `TestRateLimiter_AllowDoH` : vérifier que les 20 premières requêtes passent (burst), puis les suivantes sont rejetées
    - `TestRateLimiter_AllowDoH_DifferentIPs` : vérifier que les limites sont indépendantes par IP
    - `TestRateLimiter_CapDur` : remplir 10 000 IPs, vérifier que la 10 001e retourne `false`
    - `TestRateLimiter_Cleanup` : créer des entrées, backdater `lastSeen`, vérifier la suppression two-phase
    - `TestRateLimiter_CleanupDecrementsCount` : vérifier que le compteur global diminue après cleanup
    - `TestBandwidthReader_Throttle` : vérifier qu'un reader throttlé à 375 Ko/s met au moins ~100ms pour lire 37.5 Ko
    - `TestBandwidthWriter_Throttle` : même test pour le writer
    - `TestRateLimiter_ConcurrentAllowDoH` : goroutines concurrentes, vérifier pas de race (avec `-race`)
  - Notes : Suivre les patterns existants : table-driven, helpers, `sync.WaitGroup` pour la concurrence.

- [ ] Task 12 : Mettre à jour les tests existants
  - File : `internal/relay/connect_handler_test.go`
  - Action :
    - Mettre à jour `NewConnectHandler` calls dans les tests pour passer le nouveau paramètre `*RateLimiter` (passer `nil` dans les tests existants pour ne pas casser)
    - Ajouter un test `TestConnectHandler_BandwidthThrottle` qui vérifie que le throughput est limité quand un `RateLimiter` est fourni

### Acceptance Criteria

- [ ] AC 1 : Given une IP qui ouvre 50 streams CONNECT, when elle tente un 51e stream, then le relay répond 429 Too Many Requests
- [ ] AC 2 : Given une IP qui envoie 20 requêtes DoH en burst, when elle envoie la 21e requête immédiatement après, then le relay répond 429 Too Many Requests
- [ ] AC 3 : Given une IP avec un stream CONNECT actif transférant des données, when le débit dépasse 3 Mbps, then le relay throttle le transfert à ~375 Ko/s
- [ ] AC 4 : Given une IP avec 3 streams CONNECT actifs, when les 3 streams transfèrent en parallèle, then le débit total combiné est limité à ~375 Ko/s (limiter partagé)
- [ ] AC 5 : Given `r.RemoteAddr` = `"[::ffff:1.2.3.4]:1234"`, when `ExtractIP` est appelé, then il retourne `"1.2.3.4"`
- [ ] AC 6 : Given 10 000 IPs distinctes ayant été vues, when une 10 001e IP tente une requête DoH, then le relay répond 429
- [ ] AC 7 : Given des entrées per-IP inactives depuis > 5 minutes, when le cleanup s'exécute deux fois (two-phase CAS), then les entrées sont supprimées et le compteur global est décrémenté
- [ ] AC 8 : Given un stream CONNECT sans transfert de données, when 30 secondes s'écoulent, then le stream est fermé (idle timeout)
- [ ] AC 9 : Given une requête DoH rejetée par le rate limiter per-IP, when le global limiter a des slots disponibles, then aucun slot global n'est consommé (ordre des middlewares correct)
- [ ] AC 10 : Given le relay démarré, when le cleanup `RateLimiter` démarre, then il est actif **avant** que le serveur HTTP/3 accepte des connexions
- [ ] AC 11 : Given `MaxIncomingStreams` QUIC à 500, when un client tente d'ouvrir 501 streams sur sa connexion, then le stream est refusé par QUIC
- [ ] AC 12 : Given tous les tests existants du package `relay`, when `go test ./internal/relay/...` est exécuté, then tous les tests passent (pas de régression)

## Additional Context

### Dependencies

- `golang.org/x/time v0.15.0` — déjà présent dans `go.mod`, pas de `go get` nécessaire

### Testing Strategy

**Tests unitaires (obligatoires) :**

- `rate_limiter_test.go` : tous les tests listés dans la Task 11 — `ExtractIP`, `AllowDoH`, cap dur, cleanup, bandwidth throttle, concurrence
- Mise à jour `connect_handler_test.go` : adapter les appels `NewConnectHandler` au nouveau paramètre, ajouter test bandwidth
- Exécuter `go test -race ./internal/relay/...` pour détecter les data races

**Tests manuels (validation en dev) :**

- Déployer sur le relay de test, ouvrir un navigateur configuré avec le proxy
- Vérifier que la navigation fonctionne normalement (< 50 streams, < 100 DoH/min)
- Ouvrir un speed test et vérifier que le débit est capé à ~3 Mbps
- Simuler un flood DoH avec `curl` en boucle et vérifier les 429 après burst

**Pas de tests d'intégration E2E automatisés** — les tests unitaires couvrent la logique, le test manuel valide le wiring en conditions réelles

### Notes

- Contrainte serveur : 1 CPU, 1 Go RAM, 1 To transfert/mois
- Le bandwidth limiter doit être partagé entre toutes les connexions CONNECT d'une même IP (pas par stream)
- Le bandwidth limiter doit wrapper les deux directions (upload ET download) avec le même `rate.Limiter` par IP
- L'IP client pour DoH peut être extraite de `r.RemoteAddr` (pas de Cloudflare sur DoH pour l'instant)
- Structure per-IP unifiée avec cleanup partagé pour DoH rate limiter + bandwidth limiter (éviter memory leak)
- Ordre des middlewares DoH : rate limiter per-IP **avant** le global limiter (éviter de consommer des slots globaux pour des requêtes qui seront rejetées)
- Helper d'extraction d'IP unique et normalisé (sans port) utilisé par tous les handlers
- Le helper IP doit canonicaliser IPv4-mapped IPv6 (`::ffff:1.2.3.4`) → IPv4 pure (`1.2.3.4`) pour éviter les clés incohérentes
- Cap dur sur le nombre d'entrées dans la map per-IP (~10 000). Au-delà, rejeter avec 429 Too Many Requests
- Le cleanup per-IP doit démarrer **avant** que le serveur accepte des connexions (pas de race au startup)
- Le `ConnectHandler` continue d'utiliser `cfValidator.ExtractClientIP()` pour le token IP hash — le helper normalisé est pour les limiters uniquement
- `golang.org/x/time v0.15.0` déjà dans `go.mod` — pas de `go get` nécessaire

### Pre-mortem Findings (élicitation)

| # | Scénario d'échec | Priorité | Mitigation |
|---|------------------|----------|------------|
| 1 | OOM par accumulation de limiters per-IP sans cleanup | Haute | Cap dur ~10 000 entries + cleanup démarre avant accept |
| 2 | Monopolisation bandwidth par gros flux (YouTube) | Info | Trade-off accepté — fair-queuing par stream hors scope |
| 3 | Quota 1 To épuisé trop vite | Haute | Réduit à 3 Mbps/IP |
| 4 | Dépendance `x/time/rate` manquante au build | Moyenne | `go get` explicite dans les tâches |
| 5 | IPv4-mapped IPv6 casse le hash IP token | Haute | Helper canonicalise IPv4-mapped → IPv4 + test unitaire |

### Red Team Findings (élicitation)

| # | Vecteur | Sévérité | Mitigation |
|---|---------|----------|------------|
| 1 | Rotation d'IP multi-source | Moyenne | Hors scope — `MaxIncomingStreams` per-connection (500) sert de limite dure par client |
| 2 | Slowloris CONNECT (slots bloqués sans trafic) | Haute | Réduire `connectIdleTimeout` à 30s |
| 3 | Flood DoH multi-IP saturant le global limiter | Faible | Le global limiter à 150 couvre déjà — besoin de > 150 IPs simultanées |
| 4 | Burst bandwidth abusif sur connexions multiples | Moyenne | Burst du token bucket calé à 32 Ko (taille buffer) |
| 5 | Quota global 1 To non protégé | Haute | Réduit à 3 Mbps/IP — budget ~1.3 To pour 20 users à 3h/jour. Marge acceptable. |
