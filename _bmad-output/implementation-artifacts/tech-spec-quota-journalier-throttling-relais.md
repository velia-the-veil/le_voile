---
title: 'Quota journalier et throttling par IP sur le relais'
slug: 'quota-journalier-throttling-relais'
created: '2026-03-18'
status: 'completed'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go', 'HTTP/3 (quic-go)', 'sync.Map + atomic.Int64', 'net/http']
files_to_modify: ['internal/relay/bandwidth_limiter.go (nouveau)', 'internal/relay/bandwidth_limiter_test.go (nouveau)', 'internal/relay/connect_handler.go', 'cmd/relay/main.go']
code_patterns: ['lock-free sync.Map + atomics', 'CAS 2-phase cleanup', 'injection via constructeur', 'boucle manuelle Read/Write 32Ko']
test_patterns: ['table-driven tests', 'tests concurrence sync.WaitGroup + atomic', 'manipulation timestamps cleanup', 'package relay (même package)']
---

# Tech-Spec: Quota journalier et throttling par IP sur le relais

**Created:** 2026-03-18

## Overview

### Problem Statement

Le relais n'a aucune limite de volume par utilisateur. Une seule IP peut consommer toute la bande passante disponible en streamant ou téléchargeant massivement via le tunnel CONNECT, dégradant le service pour tous les autres utilisateurs.

### Solution

Ajouter un compteur de volume transféré par IP sur le relais. Au-delà de 2 Go/jour, le débit est limité à 5 Mbps (au lieu de couper la connexion), permettant une navigation dégradée mais fonctionnelle. Reset quotidien à minuit UTC.

### Scope

**In Scope:**
- Compteur de bytes transférés par IP (**download uniquement** — bytes reçus du serveur destination) dans le relais
- Seuil : 2 Go par jour par IP
- Action au dépassement : throttle à 5 Mbps (pas de coupure)
- Reset : minuit UTC (lazy reset, `time.Now().UTC()` obligatoire)
- Intégration dans le handler CONNECT existant
- Cleanup des entrées inactives > 24h (pattern 2-phase CAS existant)

**Out of Scope:**
- Bypass côté client (spec séparée)
- UI / notification côté desktop
- Configuration dynamique du seuil
- Persistance du compteur entre redémarrages du relais

## Context for Development

### Codebase Patterns

- Pattern lock-free avec `sync.Map` + `atomic.Int64` (cf. `ip_limiter.go`)
- Nettoyage CAS 2-phase pour les entrées inactives (cf. `ip_limiter.go:80-95`)
- Relay bidirectionnel via **boucle manuelle Read/Write** avec buffers 32 Ko (cf. `connect_handler.go:178-217`) — PAS `io.Copy`
- IP source extraite via `cfValidator.ExtractClientIP(r)` dans `ServeHTTP` (cf. `connect_handler.go:74-81`)
- Injection de dépendance via constructeur : `NewConnectHandler(pubKey, cfv, ipLimiter, logFunc)`
- L'IPLimiter est actuellement **désactivé** (`nil`) dans `cmd/relay/main.go:63` — dette technique à corriger : le VPN est maintenant multi-utilisateur, l'IPLimiter doit être réactivé en même temps que l'injection du BandwidthLimiter
- La fonction `relay()` ne reçoit PAS l'IP client — il faudra ajouter le paramètre
- Tests dans le même package (`package relay`), table-driven, tests concurrence avec `sync.WaitGroup` + `atomic.Int64`

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `internal/relay/ip_limiter.go` | Pattern existant de compteur atomique par IP (connexions concurrentes) — modèle pour la struct atomique et le cleanup CAS 2-phase |
| `internal/relay/connect_handler.go` | Handler CONNECT relay-side avec relay bidirectionnel — point d'intégration pour `WrapReader` |
| `internal/relay/limiter.go` | Limiteur global de connexions DoH — pattern de référence |
| `internal/relay/bandwidth_limiter.go` | **Nouveau fichier** — struct unifiée `BandwidthLimiter` (compteur + throttle par IP) |

### Technical Decisions

| ADR | Décision | Justification |
|-----|----------|---------------|
| Architecture | **Struct unifiée `BandwidthLimiter`** dans `bandwidth_limiter.go` — compteur et throttle sont deux facettes du même objet | Évite la séparation artificielle quota/throttle. Une seule struct, une seule responsabilité |
| API d'intégration | `limiter.WrapReader(ip, reader) io.Reader` — le handler CONNECT n'a pas besoin de savoir si l'IP est throttlée | Encapsulation propre, le handler appelle `WrapReader` et utilise le reader retourné — transparent |
| Mécanisme de throttle | Rate-limited `io.Copy` via `ThrottledReader` wrappant `io.Reader` avec `time.Sleep` | Minimal, sans dépendance externe, TCP absorbe naturellement la backpressure. Précision ±10% acceptable |
| Compteur par IP | `sync.Map` + struct avec `atomic.Int64` pour bytes et timestamp de reset | Lock-free, cohérent avec le pattern de `ip_limiter.go`, performant sous charge |
| Quoi compter | **Download uniquement** (bytes lus depuis le serveur destination) | Évite le double comptage upload+download dans le relay bidirectionnel |
| Reset quotidien | Lazy reset — chaque accès vérifie si le jour a changé (comparaison atomique du timestamp), reset individuel via CAS | Pas de goroutine supplémentaire, pas de spike de contention, distribué dans le temps |
| Timezone | `time.Now().UTC()` obligatoire partout | Évite les bugs de décalage si le serveur est configuré en heure locale |
| Granularité throttle | Budget global partagé de 5 Mbps par IP (pas par connexion) | Un seul rate limiter par IP partagé entre toutes les connexions CONNECT concurrentes |
| Cleanup mémoire | Goroutine périodique (60s) supprimant les entrées inactives > 24h, pattern CAS 2-phase | Évite le memory leak de la `sync.Map` et empêche la suppression d'entrées actives |
| Constante débit | `ThrottleBytesPerSec = 625_000` (5 Mbps = 5×10⁶/8) | Évite la confusion bits/bytes — constante explicite |
| Extraction IP | `CF-Connecting-IP` avec fallback sur `r.RemoteAddr` | Fonctionne en production (Cloudflare) et en dev/test local |
| Reset CAS | Boucle CAS : `CompareAndSwap(oldDay, newDay)`, retry si échec | Gère la race condition quand deux goroutines tentent le reset simultanément |

## Implementation Plan

### Tasks

- [x] Task 1 : Créer la struct `bandwidthState` et le `BandwidthLimiter`
  - File : `internal/relay/bandwidth_limiter.go`
  - Action : Créer le fichier avec :
    - Constantes : `DailyQuotaBytes = 2 * 1024 * 1024 * 1024` (2 Go), `ThrottleBytesPerSec = 625_000` (5 Mbps), `BandwidthCleanupInterval = 60 * time.Second`, `BandwidthInactivityTTL = 24 * time.Hour`
    - Struct `bandwidthState` avec champs atomiques : `bytesUsed atomic.Int64`, `dayTimestamp atomic.Int64` (Unix du début du jour UTC courant), `lastSeen atomic.Int64`, `markedForDeletion atomic.Bool`
    - Struct `BandwidthLimiter` avec `ips sync.Map` (map[string]*bandwidthState) et `quota int64`
    - `NewBandwidthLimiter(quota int64) *BandwidthLimiter`
    - Méthode `addBytes(ip string, n int) bool` — incrémente le compteur, effectue le lazy reset si le jour a changé (boucle CAS), retourne `true` si le quota est dépassé
  - Notes : Le lazy reset calcule le jour courant via `time.Now().UTC().Truncate(24 * time.Hour).Unix()` et compare avec `dayTimestamp`. Si différent, CAS pour reset `bytesUsed` à 0 et mettre à jour `dayTimestamp`.

- [x] Task 2 : Implémenter le `ThrottledReader`
  - File : `internal/relay/bandwidth_limiter.go`
  - Action : Ajouter dans le même fichier :
    - Struct `ThrottledReader` avec champs : `reader io.Reader`, `limiter *BandwidthLimiter`, `ip string`, `bytesPerSec int64`
    - Méthode `Read(p []byte) (int, error)` : appelle `reader.Read(p)`, puis `limiter.addBytes(ip, n)`. Si quota dépassé, calcule `sleepDuration = time.Duration(n) * time.Second / time.Duration(bytesPerSec)` et fait `time.Sleep(sleepDuration)`
    - Méthode publique `WrapReader(ip string, reader io.Reader) io.Reader` sur `BandwidthLimiter` : retourne un `&ThrottledReader{reader, limiter, ip, ThrottleBytesPerSec}`
  - Notes : Le sleep n'est appliqué QUE quand le quota est dépassé. Sous le quota, le reader passe les données sans délai. Le compteur est toujours incrémenté.

- [x] Task 3 : Implémenter le cleanup CAS 2-phase
  - File : `internal/relay/bandwidth_limiter.go`
  - Action : Ajouter :
    - Méthode `StartCleanup(ctx context.Context)` — ticker de 60s, appelle `cleanup()`. Pattern identique à `IPLimiter.StartCleanup`.
    - Méthode `cleanup()` — itère `sync.Map`, pour les entrées avec `lastSeen` > 24h : phase 1 = marquer `markedForDeletion`, phase 2 = supprimer si toujours marqué. Les entrées récemment accédées via `addBytes` annulent le marquage.
  - Notes : Copier le pattern exact de `ip_limiter.go:80-95` en adaptant le cutoff de 5 min → 24h.

- [x] Task 4 : Intégrer dans `ConnectHandler`
  - File : `internal/relay/connect_handler.go`
  - Action :
    - Ajouter champ `bwLimiter *BandwidthLimiter` à la struct `ConnectHandler`
    - Modifier `NewConnectHandler` : ajouter paramètre `bwLimiter *BandwidthLimiter`
    - Modifier l'appel à `relay()` dans `ServeHTTP` (ligne 161) : passer `clientIP` et `h.bwLimiter` en paramètres supplémentaires
    - Modifier la signature de `relay()` : ajouter `clientIP string, bwLimiter *BandwidthLimiter`
    - Dans la goroutine `dest → client` (lignes 178-197) : après chaque `dest.Read(buf)`, appeler `bwLimiter.addBytes(clientIP, n)`. Si quota dépassé, calculer et appliquer le sleep avant le `clientWriter.Write`
  - Notes : NE PAS utiliser `WrapReader` ici car le relay utilise une boucle manuelle Read/Write (pas `io.Copy`). Intégrer le throttle directement dans la boucle. Si `bwLimiter` est `nil`, pas de throttle (backward compatible).

- [x] Task 5 : Réactiver l'IPLimiter et injecter le BandwidthLimiter dans `main.go`
  - File : `cmd/relay/main.go`
  - Action :
    - Créer l'IPLimiter : `ipLimiter := relay.NewIPLimiter(relay.IPLimiterMaxPerIP)`
    - Créer le BandwidthLimiter : `bwLimiter := relay.NewBandwidthLimiter(relay.DailyQuotaBytes)`
    - Lancer les goroutines de cleanup : `go ipLimiter.StartCleanup(ctx)` et `go bwLimiter.StartCleanup(ctx)`
    - Passer `ipLimiter` et `bwLimiter` à `NewConnectHandler` (au lieu de `nil`)
    - Supprimer le commentaire "IPLimiter disabled: single-user relay"
    - Mettre à jour le `buildTag`
  - Notes : Les deux limiters sont indépendants et orthogonaux.

- [x] Task 6 : Écrire les tests unitaires du `BandwidthLimiter`
  - File : `internal/relay/bandwidth_limiter_test.go`
  - Action : Créer les tests suivants (pattern table-driven + concurrence comme `ip_limiter_test.go`) :
    - `TestBandwidthLimiter_AddBytesUnderQuota` — vérifier que `addBytes` retourne `false` (pas throttlé) sous le quota
    - `TestBandwidthLimiter_AddBytesExceedsQuota` — vérifier que `addBytes` retourne `true` quand le quota est dépassé
    - `TestBandwidthLimiter_LazyReset` — manipuler `dayTimestamp` pour simuler un changement de jour, vérifier que le compteur est remis à zéro
    - `TestBandwidthLimiter_CleanupTwoPhase` — vérifier le marquage en 2 phases (copier le pattern de `TestIPLimiter_CleanupTwoPhase`)
    - `TestBandwidthLimiter_CleanupCASRescue` — vérifier qu'un accès entre les deux phases annule la suppression
    - `TestBandwidthLimiter_ConcurrentAddBytes` — 50 goroutines incrémentant simultanément, vérifier la cohérence du compteur
    - `TestThrottledReader_NoThrottleUnderQuota` — vérifier que le reader ne dort pas sous le quota
    - `TestThrottledReader_ThrottlesOverQuota` — vérifier que le reader insère un délai au-delà du quota
    - `TestBandwidthLimiter_StartCleanupRespectsContext` — vérifier l'arrêt propre via context cancellation

- [x] Task 7 : Mettre à jour les tests existants du `ConnectHandler`
  - File : `internal/relay/connect_handler_test.go`
  - Action :
    - Mettre à jour les appels à `NewConnectHandler` pour passer le nouveau paramètre `bwLimiter` (passer `nil` dans les tests existants pour ne pas changer leur comportement)
    - Ajouter un test d'intégration : vérifier qu'une connexion CONNECT avec un `BandwidthLimiter` compte bien les bytes transférés

### Acceptance Criteria

- [x] AC1 : Given une IP sous le quota (< 2 Go), when elle transfère des données via CONNECT, then le débit n'est pas limité et les bytes sont comptés
- [x] AC2 : Given une IP ayant dépassé 2 Go de download dans la journée, when elle transfère des données via CONNECT, then le débit est limité à ~5 Mbps (±10%)
- [x] AC3 : Given une IP throttlée, when minuit UTC passe et une nouvelle requête arrive, then le compteur est remis à zéro et le débit n'est plus limité
- [x] AC4 : Given le `BandwidthLimiter` en fonctionnement, when aucune requête n'arrive pour une IP pendant 24h, then l'entrée est nettoyée de la mémoire (cleanup CAS 2-phase)
- [x] AC5 : Given 50 connexions concurrentes depuis la même IP, when elles transfèrent simultanément, then le compteur de bytes reste cohérent (pas de race condition) et le budget de 5 Mbps est partagé
- [x] AC6 : Given le `BandwidthLimiter` passé à `nil`, when une connexion CONNECT est établie, then le relay fonctionne normalement sans throttle (backward compatible)
- [x] AC7 : Given le serveur qui démarre, when l'IPLimiter et le BandwidthLimiter sont injectés, then les deux limiters sont actifs et leurs goroutines de cleanup tournent
- [x] AC8 : Given le context du serveur annulé (shutdown), when le cleanup tourne, then la goroutine de cleanup s'arrête proprement

## Additional Context

### Dependencies

- Aucune dépendance externe — uniquement la stdlib Go (`sync`, `sync/atomic`, `time`, `io`)
- Dépend du pattern existant de `ip_limiter.go` pour la cohérence architecturale
- L'IPLimiter doit être réactivé dans `main.go` en même temps (dette technique)

### Testing Strategy

**Tests unitaires** (`bandwidth_limiter_test.go`) :
- Compteur de bytes : incrémentation, dépassement quota, reset lazy
- ThrottledReader : pas de throttle sous quota, throttle actif au-delà
- Cleanup CAS 2-phase : marquage, suppression, rescue
- Concurrence : 50 goroutines simultanées, cohérence du compteur
- Context cancellation : arrêt propre du cleanup

**Tests d'intégration** (`connect_handler_test.go`) :
- Mise à jour des appels `NewConnectHandler` existants (backward compat avec `nil`)
- Nouveau test : vérifier le comptage de bytes à travers une connexion CONNECT complète

**Tests manuels** :
- Déployer sur le relais de dev, transférer > 2 Go, vérifier que le throttle s'active
- Vérifier le reset à minuit UTC
- Vérifier que la navigation web reste fonctionnelle à 5 Mbps

### Limitations connues

- **IP partagée (NAT)** : Les utilisateurs derrière un même NAT partagent le même quota. Acceptable pour un VPN personnel — les cas de NAT partagé sont rares. Évolution future possible : quoter par session token Ed25519.

### Design Notes

- Le système de quota (`bandwidth_limiter.go`) et le système de connexions concurrentes (`ip_limiter.go`) sont **orthogonaux** — ils protègent des ressources différentes (bande passante vs sockets). Pas de chevauchement.
- Le quota fixe (2 Go) est un proxy acceptable pour la V1. Un modèle "fair share dynamique" serait plus optimal mais exponentiellement plus complexe.
- La précision du throttle à ±10% est parfaitement acceptable — TCP, latence et slow start font que le débit réel est souvent inférieur au plafond configuré.

## Review Notes

- Revue adversariale complétée le 2026-03-19
- Findings : 8 total, 4 corrigés, 4 skippés
- Résolution : auto-fix des findings réels
- **F1** (High) corrigé : race CAS day-reset → double-checked locking avec `sync.Mutex` par entrée
- **F2** (Medium) corrigé : `ThrottledReader`/`WrapReader` supprimés (code mort), remplacés par `AccountAndThrottle`
- **F4** (Medium) corrigé : `time.Sleep` remplacé par `select` context-aware dans `AccountAndThrottle`
- **F8** (Low) corrigé : tests ajoutés pour `AccountAndThrottle` (chemin de production)
- F3 skip : upload non compté — intentionnel (spec « download uniquement »)
- F5 skip : limitation IP-based — design documenté
- F6 skip : contention CAS bénigne au premier accès
- F7 skip : race cleanup `Delete` vs `LoadOrStore` — pattern existant `ip_limiter.go`, fenêtre négligeable
