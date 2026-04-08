---
title: 'Bypass automatique par volume dans le proxy local'
slug: 'bypass-volume-proxy-local'
created: '2026-03-18'
status: 'completed'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go', 'net/http', 'sync.Map + atomic', 'golang.org/x/net/publicsuffix']
files_to_modify: ['internal/httpproxy/volume_tracker.go (nouveau)', 'internal/httpproxy/volume_tracker_test.go (nouveau)', 'internal/httpproxy/connect_handler.go', 'internal/httpproxy/server.go', 'internal/httpproxy/connect_handler_test.go', 'internal/httpproxy/e2e_test.go']
code_patterns: ['lock-free sync.Map + atomics', 'fenêtre fixe 1h avec reset CAS', 'bypass direct via net.Dial + early return', 'extraction domaine racine PSL', 'sync.Map[domain]*connSet (mutex + map) pour tracking/coupure', 'AddBytes retourne bool pour signal bypass']
test_patterns: ['table-driven tests', 'tests concurrence sync.WaitGroup + atomic', 'package httpproxy (même package)', 'mockTunnelClient dans server_test.go', 'captureTransport/relayTransport pour simuler le relais', 'e2eTunnelClient pour tests E2E (build tag e2e)', 'startE2EProxy helper pour lifecycle proxy']
---

# Tech-Spec: Bypass automatique par volume dans le proxy local

**Created:** 2026-03-18

## Overview

### Problem Statement

Le proxy local (127.0.0.1:50113) route tout le trafic système via le relais VPN sans distinction. Les apps desktop (Steam, Windows Update, Spotify, clients torrent...) peuvent générer des flux massifs qui consomment le quota de 2 Go/jour sans que l'utilisateur s'en rende compte.

### Solution

Moniteur de volume par domaine racine (registrable domain) dans le proxy local Go (`internal/httpproxy/`). Quand le volume cumulé d'un domaine dépasse 500 Mo sur une fenêtre fixe d'1 heure, les nouvelles connexions vers ce domaine sont bypassées (connexion directe, sans passer par le relais) et les connexions relayées existantes vers ce domaine sont coupées. Le flag bypass est posé **avant** la coupure des connexions (atomicité) pour éviter que les reconnexions immédiates repassent par le relais. Le domaine reste en bypass pendant un cooldown de 24h avant de redevenir routé via le relais.

**Trade-off accepté :** les connexions bypassées exposent l'IP réelle de l'utilisateur au serveur destination. C'est le compromis volontaire entre vie privée et économie de quota.

### Scope

**In Scope:**
- Compteur de volume par domaine racine (registrable domain via PSL) dans le proxy local
- Fenêtre fixe d'1 heure, seuil 500 Mo (reset CAS atomique pour éviter les race conditions)
- Bypass automatique : connexion directe (`net.Dial`) pour les domaines flaggés
- Coupure des connexions relayées existantes vers le domaine flaggé
- Cooldown 24h : le domaine reste en bypass pendant 24h
- Compteur uniquement sur les connexions relayées (pas les bypassées)
- Windows et Linux

**Out of Scope:**
- Détection par Content-Type ou liste de domaines
- Notification utilisateur / UI tray
- Quota journalier relais (spec séparée, déjà faite)
- Configuration dynamique du seuil

## Context for Development

### Codebase Patterns

- Le `connectHandler` dans `connect_handler.go` extrait déjà le `host:port` du CONNECT (lignes 36-51)
- Connexion au relais via `POST /connect` avec `h.tunnelClient.HTTPClient()` — le bypass remplace ce chemin par un `net.Dial("tcp", target)` direct
- Boucle bidirectionnelle via `io.Copy` (lignes 153-162) — le comptage de bytes s'insère en wrappant le `relayResp.Body` dans un `io.TeeReader` ou un reader custom qui appelle `AddBytes`
- Le `Server` dans `server.go` crée le `connectHandler` — point d'injection du volume tracker
- `sync.WaitGroup` pour le suivi des connexions hijackées — la coupure utilise le registre `connSet`, pas le WaitGroup
- L'extension navigateur bypass déjà les téléchargements > 50 Mo via Content-Length (`background.js`) — complémentaire
- `golang.org/x/net` déjà dans `go.mod` (v0.43.0, indirect via quic-go) — passer en direct pour `publicsuffix`
- Tests : `mockTunnelClient` dans `server_test.go`, `captureTransport` dans `connect_handler_test.go`, `relayTransport` + `e2eTunnelClient` dans `e2e_test.go`
- Le handler CONNECT utilise `io.Copy` (pas une boucle manuelle Read/Write comme le relay-side) — le comptage se fera via un reader wrapper sur `relayResp.Body`
- Le `connectHandler` est créé dans `server.go:61-64` sans injection du tracker — il faudra ajouter le champ et l'injecter via `NewServer` ou un setter

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `internal/httpproxy/connect_handler.go` | Handler CONNECT client-side — point d'intégration principal pour le bypass et le comptage |
| `internal/httpproxy/server.go` | Serveur proxy local — injection du volume tracker |
| `internal/relay/ip_limiter.go` | Pattern de référence : sync.Map + atomics, cleanup CAS 2-phase |
| `internal/relay/bandwidth_limiter.go` | Tech-spec quota relais — pattern compteur de bytes (complémentaire) |
| `internal/browser/extension_assets/src/background.js` | Bypass navigateur > 50 Mo — mécanisme complémentaire |

### Technical Decisions

| ADR | Décision | Justification |
|-----|----------|---------------|
| Granularité | Domaine racine (registrable domain via `golang.org/x/net/publicsuffix`), **sauf pour les CDN partagés** : si le registrable domain est un CDN connu (`akamaized.net`, `cloudfront.net`, `fastly.net`, `edgecast.net`, `azureedge.net`), utiliser le FQDN complet comme clé | Les CDN font du round-robin sur les sous-domaines — grouper par domaine racine capture tout le volume. Exception CDN partagé : bypasser `akamaized.net` bloquerait tous les sites sur Akamai, pas seulement Spotify |
| Fenêtre | Fixe d'1h avec compteur + timestamp de début. Si `now - windowStart > 1h`, reset du compteur | 10x plus simple qu'une fenêtre glissante à 60 buckets. La précision d'une vraie fenêtre glissante ne justifie pas la complexité — le seuil de 500 Mo est déjà une approximation |
| Seuil | 500 Mo cumulé sur la fenêtre d'1h | Assez haut pour ne pas déclencher sur la navigation web, assez bas pour attraper les flux massifs |
| Bypass | `net.Dial("tcp", target)` direct au lieu de `POST /connect` vers le relais | Connexion directe standard, pas de modification du handshake TLS |
| Coupure | Force-close des connexions relayées existantes vers le domaine flaggé. **Atomicité : flag bypass posé AVANT la coupure** — les nouvelles connexions vérifient le flag en premier | Évite de continuer à consommer le quota relais. L'atomicité empêche la race condition où une reconnexion immédiate repasserait par le relais avant que le flag soit posé |
| Comptage | Uniquement sur les connexions relayées (pas les bypassées) | Les connexions directes ne consomment pas de quota relais — pas besoin de les suivre |
| Cooldown | 24h fixe après le dernier déclenchement du bypass | Assez long pour couvrir une session de jeu/mise à jour typique. Pas de réhabilitation basée sur le volume |
| Tracking connexions | Registre intégré comme champ privé de `VolumeTracker`. Structure : `sync.Map[domain]*connSet` avec `connSet = sync.Mutex + map[int64]net.Conn`. `Register`/`Unregister` publics, `closeAll` privé (appelé par le tracker quand le seuil est atteint). Suppression de l'entrée domaine quand `len(conns) == 0` | `sync.Map` externe pour le lookup rapide par domaine, `sync.Mutex` interne pour la contention rare (seulement pendant `closeAll`). Plus propre que double `sync.Map` pour l'itération et le cleanup |
| Fuite IP | Les connexions bypassées (`net.Dial` direct) exposent l'IP réelle au serveur destination | Trade-off accepté : on sacrifie la vie privée pour les flux massifs afin d'économiser le quota relais. Documenté explicitement |
| Fallback PSL | Si `EffectiveTLDPlusOne` échoue (hostname malformé, IP brute), utiliser le hostname/IP complet comme clé | Évite que des connexions échappent au compteur. Les IPs brutes sont comptées individuellement |
| Fallback bypass | Si `net.Dial` direct échoue (firewall restrictif), retomber sur le chemin relais pour cette connexion. **Timeout court de 3s** sur le dial bypass (au lieu du défaut TCP 30s). **Compteur d'échecs** : après 3 `net.Dial` directs consécutifs échoués pour un domaine, désactiver le bypass pour ce domaine (flag `directFailed`) — pas de retry avant le prochain reset de cooldown | Évite la perte totale de connectivité ET évite de payer 3s × N connexions dans les environnements restrictifs. Le compteur d'échecs empêche les timeouts répétés |
| Vérification flag | Le flag bypass est vérifié comme **première action** du handler CONNECT, avant `EnsureSessionToken` et avant le `POST /connect` | Évite d'initier une connexion relais coûteuse pour un domaine déjà en bypass |
| Désenregistrement | `defer registry.Unregister(domain, connID)` systématique après chaque `Register` | Garantit le cleanup même en cas de panic ou early return — empêche le memory leak du registre |
| Cleanup mémoire | Goroutine périodique supprimant les entrées > 24h, pilotée par `context.Context` et arrêtée proprement au shutdown | Empêche le memory leak de la `sync.Map` et s'intègre dans le lifecycle du serveur |
| Reset CAS | `atomic.CompareAndSwap` sur le timestamp de fenêtre lors du reset du compteur | Un seul goroutine gagne le CAS et reset le compteur — les autres voient le nouveau timestamp. Empêche la perte de bytes |
| API AddBytes | `AddBytes(domain, n) bool` — retourne `true` si le domaine vient d'être bypassé (seuil dépassé). Le déclenchement du bypass (flag + closeAll) est fait inline par le goroutine appelant | Pas de goroutine de surveillance séparée. La boucle relay sort immédiatement si `AddBytes` retourne `true`. Le `closeAll` est le backup — si la boucle ne réagit pas, le `Close()` forcera l'erreur |
| Architecture fichier | Un seul fichier `volume_tracker.go` avec struct unifiée `VolumeTracker` (compteur + registre + bypass flags) | Suit le pattern du projet (un fichier par responsabilité : `ip_limiter.go`, `limiter.go`). Les trois concepts sont intimement liés — les séparer créerait du couplage artificiel |
| Chemin bypass | Early return dans le handler CONNECT avec sa propre boucle relay simplifiée (deux `net.Conn`). Pas de refactoring du chemin relais existant | Les types d'I/O divergent : le chemin relais utilise `io.MultiReader`, `r.Body`, `http.ResponseWriter` ; le chemin bypass utilise deux `net.Conn`. La duplication (~20 lignes) est justifiée |
| Dépendance PSL | `golang.org/x/net/publicsuffix` — quasi-stdlib, maintenu par l'équipe Go | Le projet utilise déjà `golang.org/x/crypto`. Seule solution correcte pour les TLD composés (`co.uk`, `com.au`) |
| Coupure TCP | `Close()` standard sur les connexions coupées (envoie RST quand des données sont en attente). Pas de shutdown gracieux | Le RST a plus de chances de déclencher un retry automatique côté app que le FIN (fermeture propre → stop) |
| HTTP plain-text | Limitation connue : le handler HTTP non-CONNECT (lignes 173-301) n'est pas couvert par le bypass/compteur | < 1% du trafic en 2026 est HTTP non-chiffré. Le quota relais (2 Go/jour) est le filet de sécurité. Évolution future possible |
| Séquence AddBytes | Dans `AddBytes`, quand le seuil est dépassé : (1) `bypassed.Store(true)` + `bypassedAt.Store(now)`, (2) `closeAll(domain)`. **Flag toujours avant coupure** | La fenêtre de race entre détection du dépassement et pose du flag est de quelques nanosecondes — négligeable. Une connexion relais gaspillée puis immédiatement coupée dans le pire cas |
| Domain sharding | Limitation connue : un client torrent utilisant des centaines de domaines (< 500 Mo chacun) échappe au bypass | Le quota relais (2 Go/jour + throttle 5 Mbps) est le filet côté serveur. Le bypass est une optimisation UX, pas une protection |
| Dé-anonymisation | Risque théorique : un attaquant contrôlant un site pourrait forcer 500 Mo de transfert vers son domaine pour déclencher le bypass et exposer l'IP réelle | Requiert 500 Mo de bande passante attaquant + domaine dédié. Peu pratique. Le bypass expose l'IP réelle par design (trade-off accepté) |

## Implementation Plan

### Tasks

- [x] Task 1 : Créer les structs de données du `VolumeTracker`
  - File : `internal/httpproxy/volume_tracker.go`
  - Action : Créer le fichier avec :
    - Constantes : `VolumeThreshold int64 = 500 * 1024 * 1024` (500 Mo), `WindowDuration = 1 * time.Hour`, `BypassCooldown = 24 * time.Hour`, `BypassDialTimeout = 3 * time.Second`, `MaxDirectFailures int32 = 3`, `CleanupInterval = 60 * time.Second`, `CleanupTTL = 24 * time.Hour`
    - Variable de package : `sharedCDNs = map[string]bool{"akamaized.net": true, "cloudfront.net": true, "fastly.net": true, "edgecast.net": true, "azureedge.net": true}` — domaines CDN partagés où le FQDN complet est utilisé comme clé
    - Struct `domainState` avec champs atomiques : `bytesUsed atomic.Int64`, `windowStart atomic.Int64` (Unix timestamp), `bypassed atomic.Bool`, `bypassedAt atomic.Int64` (Unix timestamp), `directFailures atomic.Int32`, `lastSeen atomic.Int64`, `markedForDeletion atomic.Bool`
    - Struct `connSet` avec : `mu sync.Mutex`, `conns map[int64]net.Conn`
    - Struct `VolumeTracker` avec : `domains sync.Map` (map[string]*domainState), `conns sync.Map` (map[string]*connSet), `threshold int64`, `connIDGen atomic.Int64`
    - `NewVolumeTracker(threshold int64) *VolumeTracker`
  - Notes : Le `connIDGen` est un compteur atomique global pour générer des IDs uniques de connexions. Les `domainState` et `connSet` sont séparés dans la `sync.Map` (pas imbriqués) car le cleanup des états de domaine et le cleanup des connexions ont des lifecycles différents.

- [x] Task 2 : Implémenter la résolution de clé de domaine
  - File : `internal/httpproxy/volume_tracker.go`
  - Action : Ajouter la fonction `domainKey(host string) string` :
    1. Extraire le hostname (sans le port) via `net.SplitHostPort`
    2. Si le hostname est une IP (vérifier via `net.ParseIP`), retourner l'IP brute
    3. Appeler `publicsuffix.EffectiveTLDPlusOne(hostname)`
    4. Si erreur, retourner le hostname complet (fallback)
    5. Si le registrable domain est dans `sharedCDNs`, retourner le hostname complet (FQDN)
    6. Sinon, retourner le registrable domain
  - Notes : Import `golang.org/x/net/publicsuffix`. La fonction est privée — appelée par toutes les méthodes publiques.

- [x] Task 3 : Implémenter `IsBypassed` et `AddBytes`
  - File : `internal/httpproxy/volume_tracker.go`
  - Action : Ajouter les méthodes :
    - `IsBypassed(target string) bool` : extrait la clé via `domainKey(target)`, charge le `domainState` depuis la `sync.Map`. Si `bypassed == true` : vérifier le cooldown (`now - bypassedAt > BypassCooldown`). Si cooldown expiré, `bypassed.Store(false)` + reset `directFailures` → retourner `false`. Si `directFailed` (directFailures >= MaxDirectFailures), retourner `false`. Sinon retourner `true`.
    - `AddBytes(target string, n int) bool` : extrait la clé, charge ou crée le `domainState` via `LoadOrStore`. Rescue du `markedForDeletion`. Vérifie la fenêtre : si `now - windowStart > WindowDuration`, CAS pour reset `bytesUsed` à 0 et mettre à jour `windowStart`. Incrémente `bytesUsed.Add(int64(n))`. Met à jour `lastSeen`. Si `bytesUsed > threshold` et `!bypassed` : (1) `bypassed.Store(true)`, `bypassedAt.Store(now)`, (2) appeler `closeAll(key)`. Retourner `true` si le domaine vient d'être bypassé par CET appel, `false` sinon.
  - Notes : Le CAS sur `windowStart` utilise `atomic.CompareAndSwap`. Le CAS sur `bypassed` utilise `CompareAndSwap(false, true)` pour s'assurer qu'un seul goroutine déclenche le bypass.

- [x] Task 4 : Implémenter le registre de connexions et `closeAll`
  - File : `internal/httpproxy/volume_tracker.go`
  - Action : Ajouter les méthodes :
    - `Register(target string, conn net.Conn) int64` : extrait la clé, charge ou crée le `connSet` via `LoadOrStore(&connSet{conns: make(map[int64]net.Conn)})`. Génère un `connID` via `connIDGen.Add(1)`. Lock le mutex du `connSet`, ajoute `conns[connID] = conn`, unlock. Retourne `connID`.
    - `Unregister(target string, connID int64)` : charge le `connSet`, lock, delete `conns[connID]`, si `len(conns) == 0` alors unlock + `conns.Delete(key)`, sinon unlock.
    - `closeAll(key string)` (privée) : charge le `connSet`. Lock le mutex, copie toutes les `net.Conn` dans un slice, clear la map, unlock. Itère le slice et appelle `conn.Close()` sur chaque (ignorer les erreurs — `Close()` est idempotent).
  - Notes : `closeAll` est appelée par `AddBytes` quand le seuil est dépassé. Le lock pendant `closeAll` est bref (copie des pointeurs), les `Close()` sont faits hors du lock.

- [x] Task 5 : Implémenter `RecordDirectFailure` et le cleanup
  - File : `internal/httpproxy/volume_tracker.go`
  - Action : Ajouter :
    - `RecordDirectFailure(target string)` : charge le `domainState`, incrémente `directFailures.Add(1)`.
    - `StartCleanup(ctx context.Context)` : ticker de `CleanupInterval`, appelle `cleanup()`. Arrêt propre via `ctx.Done()`.
    - `cleanup()` : itère `domains` sync.Map. Pour les entrées avec `lastSeen > CleanupTTL` : phase 1 = marquer `markedForDeletion`, phase 2 = supprimer si toujours marqué. Pattern identique à `ip_limiter.go:80-95` avec cutoff de 24h.
  - Notes : Le cleanup ne nettoie pas les `connSet` directement — les connexions fermées se désenregistrent via `Unregister`. Les `connSet` vides sont supprimés par `Unregister`.

- [x] Task 6 : Créer le `countingReader` pour le comptage de bytes
  - File : `internal/httpproxy/volume_tracker.go`
  - Action : Ajouter :
    - Struct `countingReader` avec : `reader io.Reader`, `tracker *VolumeTracker`, `target string`, `stopped atomic.Bool`
    - Méthode `Read(p []byte) (int, error)` : appelle `reader.Read(p)`. Si `n > 0` et `!stopped` : appelle `tracker.AddBytes(target, n)`. Si `AddBytes` retourne `true` (bypass déclenché), set `stopped = true`.
    - Méthode publique `WrapReader(target string, reader io.Reader) *countingReader` sur `VolumeTracker` : retourne `&countingReader{reader, tracker, target, atomic.Bool{}}`.
    - Méthode `Stopped() bool` sur `countingReader` : retourne `stopped.Load()` — permet à la boucle relay de détecter que le bypass a été déclenché.
  - Notes : Le `countingReader` wrap le `relayResp.Body` dans le handler CONNECT. Le `io.Copy(conn, countingReader)` compte les bytes automatiquement. Si `AddBytes` déclenche le bypass, `stopped` est set — le goroutine relay peut vérifier après le `io.Copy` return.

- [x] Task 7 : Injecter le `VolumeTracker` dans le proxy
  - File : `internal/httpproxy/server.go`
  - Action :
    - Ajouter champ `volumeTracker *VolumeTracker` à la struct `Server`
    - Modifier `NewServer` : ajouter paramètre `volumeTracker *VolumeTracker` (peut être `nil` pour backward compat)
    - Passer le `volumeTracker` au `connectHandler` dans `Start()` (ligne 61-64)
    - Lancer `go volumeTracker.StartCleanup(ctx)` dans `Start()` si `volumeTracker != nil`
  - Notes : Le context du serveur (`ctx` de `Start`) pilote le cleanup. Si `volumeTracker` est `nil`, le handler fonctionne comme avant (pas de bypass, pas de comptage).

- [x] Task 8 : Intégrer le bypass dans `handleConnect`
  - File : `internal/httpproxy/connect_handler.go`
  - Action :
    - Ajouter champ `volumeTracker *VolumeTracker` à la struct `connectHandler`
    - **Après le parsing du target (ligne 51), ajouter le check bypass comme première action :**
      ```
      if h.volumeTracker != nil && h.volumeTracker.IsBypassed(target) {
          h.handleBypass(w, r, target)
          return
      }
      ```
    - **Dans la goroutine relay (ligne 130-170), wrapper le `relayResp.Body` :**
      - Si `volumeTracker != nil` : `connID := h.volumeTracker.Register(target, conn)` + `defer h.volumeTracker.Unregister(target, connID)`
      - Wrapper : `cr := h.volumeTracker.WrapReader(target, relayResp.Body)` — remplacer `io.Copy(conn, relayResp.Body)` par `io.Copy(conn, cr)`
      - Si `volumeTracker == nil` : comportement inchangé
  - Notes : Le `Register` est dans la goroutine (après hijack), le `defer Unregister` est immédiatement après. Le `countingReader` wraps le `relayResp.Body` (downstream seulement — bytes du destination vers le browser).

- [x] Task 9 : Implémenter `handleBypass`
  - File : `internal/httpproxy/connect_handler.go`
  - Action : Ajouter la méthode `handleBypass(w http.ResponseWriter, r *http.Request, target string)` :
    1. `net.DialTimeout("tcp", target, BypassDialTimeout)` — timeout 3s
    2. Si erreur : `h.volumeTracker.RecordDirectFailure(target)`, puis fallback sur le chemin relais normal (appeler `h.handleRelayed(w, r, target)` — ou refactorer `handleConnect` pour extraire le chemin relais). **Alternative plus simple** : si le dial échoue, simplement continuer avec le chemin relais inline (pas de méthode séparée) — mais cela duplique le code. **Décision** : si dial échoue, `RecordDirectFailure` + laisser le handler retomber sur le chemin relais en appelant le code existant après le check bypass (le `return` du check bypass n'est pas exécuté).
    3. Si succès : hijack la connexion browser, envoyer `200 Connection Established`, lancer la boucle bidirectionnelle simplifiée (deux `net.Conn`) :
       ```
       done := make(chan struct{}, 2)
       go func() { io.Copy(destConn, conn); done <- struct{}{} }()
       go func() { io.Copy(conn, destConn); done <- struct{}{} }()
       <-done
       destConn.Close()
       conn.Close()
       <-done
       ```
    4. Pas de `Register`/`Unregister` (les connexions bypassées ne sont pas comptées)
    5. Pas de `WaitGroup.Add` (les connexions bypassées n'ont pas besoin d'être drainées au shutdown — elles se ferment avec le `net.Conn.Close`)
  - Notes : Le chemin bypass est intentionnellement simple. Pas de comptage, pas de registre, pas de WaitGroup. Le `BypassDialTimeout` de 3s évite les longs timeouts.

- [x] Task 10 : Restructurer `handleConnect` pour le fallback
  - File : `internal/httpproxy/connect_handler.go`
  - Action : Restructurer le début de `handleConnect` pour gérer le fallback quand le dial direct échoue :
    ```go
    func (h *connectHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
        // ... parse target (existant, lignes 36-51) ...

        if h.volumeTracker != nil && h.volumeTracker.IsBypassed(target) {
            if h.tryDirectBypass(w, r, target) {
                return // bypass réussi
            }
            // dial direct échoué — fallback sur le chemin relais ci-dessous
        }

        // ... chemin relais existant (inchangé, lignes 53-170) ...
    }
    ```
    - `tryDirectBypass` retourne `true` si le bypass a réussi (connexion établie et relayée), `false` si le dial a échoué (fallback nécessaire).
    - Si `false` : `RecordDirectFailure` est appelé dans `tryDirectBypass`, le handler continue normalement sur le chemin relais.
  - Notes : Cette structure évite la duplication du chemin relais pour le fallback. Le chemin relais est exécuté dans tous les cas où le bypass n'est pas applicable ou échoue. Le comptage (`Register`/`WrapReader`) est ajouté dans le chemin relais (Task 8) indépendamment.

- [x] Task 11 : Écrire les tests unitaires du `VolumeTracker`
  - File : `internal/httpproxy/volume_tracker_test.go`
  - Action : Créer les tests suivants (pattern table-driven + concurrence) :
    - `TestDomainKey_RegisteredDomain` — vérifier que `steamcontent.com` est retourné pour `dl1.steamcontent.com:443`
    - `TestDomainKey_SharedCDN` — vérifier que le FQDN complet est retourné pour `audio-ak.akamaized.net:443`
    - `TestDomainKey_IPAddress` — vérifier que l'IP brute est retournée pour `93.184.216.34:443`
    - `TestDomainKey_PSLError` — vérifier que le hostname complet est retourné pour un hostname invalide
    - `TestAddBytes_UnderThreshold` — vérifier que `AddBytes` retourne `false` sous le seuil
    - `TestAddBytes_ExceedsThreshold` — vérifier que `AddBytes` retourne `true` quand le seuil est dépassé et que `IsBypassed` retourne `true`
    - `TestAddBytes_WindowReset` — manipuler `windowStart` pour simuler une fenêtre expirée, vérifier que le compteur est remis à zéro
    - `TestIsBypassed_CooldownExpiry` — manipuler `bypassedAt` pour simuler un cooldown expiré, vérifier que `IsBypassed` retourne `false`
    - `TestIsBypassed_DirectFailed` — vérifier que 3 échecs directs désactivent le bypass pour le domaine
    - `TestRegisterUnregister` — vérifier l'enregistrement/désenregistrement de connexions
    - `TestCloseAll_ClosesRegisteredConns` — vérifier que `closeAll` ferme toutes les connexions enregistrées pour un domaine
    - `TestAddBytes_TriggersCloseAll` — vérifier que le dépassement du seuil ferme les connexions enregistrées
    - `TestCleanup_TwoPhase` — vérifier le marquage en 2 phases (pattern de `TestIPLimiter_CleanupTwoPhase`)
    - `TestCleanup_CASRescue` — vérifier qu'un accès entre les deux phases annule la suppression
    - `TestConcurrent_AddBytes` — 50 goroutines incrémentant simultanément, vérifier la cohérence
    - `TestCountingReader_CountsBytes` — vérifier que le reader compte correctement les bytes
    - `TestCountingReader_StopsOnBypass` — vérifier que `Stopped()` retourne `true` après déclenchement du bypass
    - `TestStartCleanup_RespectsContext` — vérifier l'arrêt propre via context cancellation

- [x] Task 12 : Mettre à jour les tests existants
  - File : `internal/httpproxy/server_test.go`, `internal/httpproxy/connect_handler_test.go`
  - Action :
    - Mettre à jour `NewServer` dans tous les tests existants pour passer le nouveau paramètre `volumeTracker` (`nil` pour ne pas changer le comportement existant)
    - Mettre à jour la création de `connectHandler` dans `connect_handler_test.go` pour ajouter le champ `volumeTracker: nil`
  - Notes : Backward compat — `nil` = pas de bypass, comportement identique à l'existant.

- [x] Task 13 : Ajouter un test E2E du bypass
  - File : `internal/httpproxy/e2e_test.go`
  - Action : Ajouter `TestE2E_VolumeBypass` (build tag `e2e`) :
    1. Créer un `VolumeTracker` avec un seuil bas (1 Ko pour le test)
    2. Créer un target httptest qui renvoie 10 Ko de données
    3. Démarrer le proxy avec le tracker
    4. Envoyer un premier CONNECT — le trafic passe par le relais, les bytes sont comptés, le seuil est dépassé
    5. Vérifier que le domaine est en bypass (`IsBypassed == true`)
    6. Envoyer un deuxième CONNECT vers le même domaine — le trafic passe en direct
    7. Vérifier que la réponse est reçue (le dial direct vers le target httptest fonctionne)
  - Notes : Utiliser le `relayTransport` existant pour simuler le relais. Le seuil bas (1 Ko) permet de déclencher le bypass rapidement en test.

- [x] Task 14 : Passer `golang.org/x/net` en dépendance directe
  - File : `go.mod`
  - Action : `go get golang.org/x/net/publicsuffix` — passe la dépendance de `indirect` à `direct` dans `go.mod`. Exécuter `go mod tidy`.
  - Notes : La dépendance est déjà présente (v0.43.0 via quic-go). Pas de nouveau téléchargement.

### Acceptance Criteria

- [x] AC1 : Given un domaine sous le seuil (< 500 Mo/h), when des connexions CONNECT passent via le relais, then le trafic est relayé normalement et les bytes sont comptés par domaine racine
- [x] AC2 : Given un domaine ayant dépassé 500 Mo de download sur la fenêtre d'1h, when le seuil est franchi, then (1) le flag bypass est posé, (2) les connexions relayées existantes vers ce domaine sont fermées, (3) les nouvelles connexions vers ce domaine passent en direct via `net.Dial`
- [x] AC3 : Given un domaine en bypass, when le cooldown de 24h expire et une nouvelle connexion arrive, then le domaine est re-routé via le relais et le compteur repart de zéro
- [x] AC4 : Given un domaine en bypass, when le `net.Dial` direct échoue, then la connexion retombe sur le chemin relais. Après 3 échecs consécutifs, le bypass est désactivé pour ce domaine
- [x] AC5 : Given un hostname sur un CDN partagé (`*.akamaized.net`), when le volume est compté, then le FQDN complet est utilisé comme clé (pas le registrable domain)
- [x] AC6 : Given une IP brute comme target (pas de hostname), when le volume est compté, then l'IP est utilisée directement comme clé
- [x] AC7 : Given le `VolumeTracker` en fonctionnement, when aucune connexion n'arrive pour un domaine pendant 24h, then l'entrée est nettoyée de la mémoire (cleanup CAS 2-phase)
- [x] AC8 : Given 50 connexions concurrentes vers le même domaine, when elles transfèrent simultanément, then le compteur de bytes reste cohérent (pas de race condition)
- [x] AC9 : Given le `VolumeTracker` passé à `nil`, when une connexion CONNECT est établie, then le proxy fonctionne normalement sans bypass ni comptage (backward compatible)
- [x] AC10 : Given le context du serveur annulé (shutdown), when le cleanup tourne, then la goroutine de cleanup s'arrête proprement
- [x] AC11 : Given la fenêtre d'1h qui expire, when une nouvelle connexion arrive et que deux goroutines tentent le reset simultanément, then un seul goroutine gagne le CAS et le compteur est correctement remis à zéro

## Additional Context

### Dependencies

- `golang.org/x/net/publicsuffix` — déjà dans `go.mod` (indirect via quic-go), passer en direct
- Aucune autre dépendance externe
- Dépend du pattern existant de `ip_limiter.go` pour la cohérence architecturale du cleanup CAS 2-phase

### Testing Strategy

**Tests unitaires** (`volume_tracker_test.go`) :
- Résolution de clé de domaine : registrable domain, CDN partagé, IP brute, fallback PSL
- Compteur de bytes : incrémentation, dépassement seuil, reset fenêtre (CAS)
- Bypass : cooldown expiry, direct failures, atomicité flag
- Registre connexions : register, unregister, closeAll
- Concurrence : 50 goroutines simultanées, cohérence compteur
- countingReader : comptage, arrêt sur bypass
- Cleanup CAS 2-phase : marquage, suppression, rescue
- Context cancellation : arrêt propre du cleanup

**Tests d'intégration** (`connect_handler_test.go`, `e2e_test.go`) :
- Mise à jour des appels existants (backward compat avec `nil`)
- Test E2E : vérifier le bypass complet (relais → dépassement → direct)

**Tests manuels** :
- Lancer le proxy avec un seuil de 50 Mo, télécharger un fichier > 50 Mo via Steam/navigateur, vérifier que le bypass se déclenche
- Vérifier que la navigation web continue de fonctionner (domaines sous le seuil)
- Vérifier le cooldown 24h (manipuler l'horloge système ou attendre)

### Notes

- **Risques principaux** : Le domain sharding (client torrent multi-domaines) échappe au bypass — le quota relais (2 Go/jour) est le filet. La dé-anonymisation via volume forcé (500 Mo) est théoriquement possible mais peu pratique.
- **Limitations connues** : Le handler HTTP plain-text (non-CONNECT) n'est pas couvert par le bypass/compteur — < 1% du trafic. Les connexions bypassées exposent l'IP réelle (trade-off accepté).
- **Évolutions futures** : Whitelist de domaines à ne jamais bypasser (configurable). Notification tray quand un domaine est bypassé. Compteur global (tous domaines) comme protection anti-torrent.

## Review Notes

- Revue adversariale complétée
- Findings : 10 total, 10 résolus (9 corrigés, 1 documenté by-design)
- Approche de résolution : auto-fix (toutes sévérités)
- Corrections clés : perte de données bufferisées après hijack (F3), goroutine bypass non trackée dans WaitGroup (F4), re-bypass immédiat après cooldown (F8), race condition window reset (F1), orphelinage de connSet (F2/F7), liste CDN étendue (F9), CAS sur reset cooldown (F5), stop counting si déjà bypassed (F6), absence de comptage bypass documentée (F10)
