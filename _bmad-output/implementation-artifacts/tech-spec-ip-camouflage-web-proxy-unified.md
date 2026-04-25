---
title: 'Camouflage IP — Proxy web tunnelisé via le relay'
slug: 'ip-camouflage-web-proxy'
created: '2026-03-13'
status: 'ready-for-dev'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.22+', 'QUIC/HTTP3 (quic-go)', 'HTTP CONNECT', 'net/http', 'WinINET registry (HKCU via tray)', 'Ed25519 (internal/crypto)', 'sync.Map + CompareAndDelete (Go 1.20+)', 'Cloudflare CF-Connecting-IP', 'DPAPI (CryptProtectData/CryptUnprotectData)']
files_to_modify: ['internal/config/config.go', 'installer/config-default.toml', 'internal/ipc/messages.go', 'internal/httpproxy/server.go', 'internal/httpproxy/connect_handler.go', 'internal/tray/sysproxy_windows.go', 'internal/relay/connect_handler.go', 'internal/relay/ip_limiter.go', 'internal/relay/cfip.go', 'internal/relay/server.go', 'internal/relay/verify_handler.go', 'internal/tunnel/client.go', 'internal/service/service.go', 'internal/ipchandler/handler.go', 'internal/tray/tray.go']
code_patterns: ['Start(ctx)/Ready() channel pattern (dns/proxy.go)', 'mux.Handle + LimitMiddleware (relay/server.go)', 'switch req.Action dispatch (ipchandler/handler.go)', 'mutex-per-component lifecycle (service/service.go)', 'atomic config save temp+rename (config/config.go)', 'startProxy/stopProxy pattern: mutex + context + errCh + Ready() channel (service.go:870-951)', 'Kill switch DisableFunc/ReconnectFunc (service.go:486-495)', 'Shutdown séquentiel inverse (service.go:641-716)', 'IPC polling proxy state (tray/tray.go)', 'Tray gère HKCU directement (tourne sous utilisateur, pas LocalSystem)', 'verifyRelay: nonce→sign→verify (client.go:305-355)', 'Limiter lock-free atomic.Int64 avec Acquire/Release (relay/limiter.go)', 'CF-Connecting-IP extraction with Cloudflare IP whitelist', 'CAS flag markedForDeletion pour IPLimiter cleanup', 'idleTimeoutConn io.Reader/Writer wrapper avec time.Timer reset', 'TOCTOU-safe dialing: resolve → dial IP résolue (relay/stun_handler.go:111-156)']
test_patterns: ['Table-driven tests avec assertions directes (pas testify)', '_test.go colocated dans même package', '_edge_test.go for edge cases', 'sync.WaitGroup pour tests concurrence', 'freeUDPAddr(t)/freeTCPAddr(t) helpers pour ports éphémères', 'testing.T.TempDir() pour isolation', 'commandRunner interface for mocking netsh (dns/manager_windows.go)']
---

# Tech-Spec: Camouflage IP — Proxy web tunnelisé via le relay

**Created:** 2026-03-13

## Overview

### Problem Statement

Actuellement Le Voile masque le DNS via DoH tunnelisé mais **pas l'IP du client**. Les sites web voient l'IP française de l'utilisateur. Avec le blocage VPN imminent en France par DPI des FAI, il faut :
1. Que le trafic web passe par le relay islandais (`levoile.dev`) — IP visible = relay
2. Que le tunnel soit indiscernable du HTTPS standard — Cloudflare en frontal (config manuelle par l'utilisateur)

### Solution

Ajouter un **proxy HTTP CONNECT local** côté client (`127.0.0.1:50113`) qui tunnelise le trafic web via le tunnel QUIC/HTTP3 existant vers un **nouveau handler forward proxy** côté relay. Le **proxy système Windows (WinINET)** est configuré automatiquement par le **tray client** (qui tourne sous l'utilisateur connecté — le service LocalSystem ne peut pas accéder au HKCU de l'utilisateur). Cloudflare en frontal sur `levoile.dev` assure le camouflage protocolaire (trafic indiscernable du HTTPS vers un CDN). L'authentification utilise un **token de session Ed25519** émis lors de `verifyRelay()`, et l'IP client réelle est extraite via **CF-Connecting-IP** avec whitelist des ranges Cloudflare.

### Scope

**In Scope:**
- Proxy local côté client (écoute sur `127.0.0.1:50113`)
- Handler forward proxy côté relay (POST avec cible dans le body JSON)
- Auth Ed25519 session token sur `/connect`
- CF-Connecting-IP extraction avec validation IP Cloudflare
- Configuration automatique du proxy système Windows (WinINET registre via le tray)
- Crash recovery proxy (DPAPI + `RecoverOrphan()` au démarrage tray)
- Connection draining 5s avec firewall activé avant draining
- Support quelques dizaines d'utilisateurs simultanés
- Trafic web léger uniquement (HTTP/HTTPS, navigation, API)

**Out of Scope:**
- Trafic lourd (streaming, gaming, téléchargement massif)
- Configuration Cloudflare (fait manuellement par Akerimus)
- Support Linux/macOS pour le proxy système (Windows d'abord)
- Chiffrement end-to-end client→relay (bypass CDN) — futur

## Context for Development

### Codebase Patterns

- **Proxy lifecycle** : `dns/proxy.go` utilise `Start(ctx)` bloquant + `Ready()` channel + error channel pour signaler l'état — le proxy HTTP suivra ce pattern exactement
- **Handler relay** : `relay/server.go` enregistre les handlers via `mux.Handle("/path", LimitMiddleware(limiter, handler))` — le handler CONNECT suivra ce pattern
- **IPC dispatch** : `ipchandler/handler.go` utilise un `switch req.Action` avec pattern toggle identique à blocklist — le toggle proxy HTTP copiera ce pattern
- **Service lifecycle** : `service/service.go` utilise un mutex dédié par composant (`proxyMu`, `stunMu`, etc.) avec `startX(ctx)`/`stopX()`. Le service a actuellement **6 mutex** (`mu`, `blMu`, `toggleMu`, `updateMu`, `proxyMu`, `stunMu`) — cette implémentation ajoutera le 7ème (`httpProxyMu`)
- **startProxy/stopProxy pattern** : `service.go:870-951`. Mutex-protected, context + cancel, errCh, goroutine Start(), wait Ready() signal. Le `startHTTPProxy` suit ce pattern exactement
- **Kill switch** : `service.go:486-495`. `DisableFunc` : stop STUN + stop DNS proxy. `ReconnectFunc` : start DNS proxy + enable STUN. HTTP proxy stop/start s'insère ici
- **Shutdown séquence** : `service.go:641-716`. Ordre inverse : IPC → leak/discoverer/blocklist/reconnector → watchdog → STUN → kill switch → DNS restore → verify → DNS proxy stop → tunnel disconnect. HTTP proxy stop s'insère entre STUN stop (ligne 677) et kill switch deactivate (ligne 679)
- **Config** : structs TOML dans `config/config.go`, save atomique temp+rename — ajouter `HTTPProxyConfig` struct
- **Proxy système Windows** : Le service tourne sous LocalSystem (`HKCU` ≠ utilisateur). La config WinINET est gérée par le **tray client** (qui tourne sous l'utilisateur connecté) via modification directe du registre `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`. Le service signale l'état proxy au tray via les champs `HTTPProxyActive`, `HTTPProxyAddr` et `HTTPProxySeq` dans la réponse IPC `get_status`. Pattern : tray poll 2s → détecte changement via sequence number → configure/restaure registre
- **IPC Architecture** : Request-response synchrone via named pipe Windows (`\\.\pipe\levoile`). Le tray poll toutes les 2s via `get_status`. Pas de push du service vers le tray. Pattern : `ipc.Client.SendContext()` → `ipchandler.Handle()` → `ipc.Response`
- **HTTP/3 client** : `tunnel/client.go` utilise `io.ReadAll()` + `io.LimitReader()` — pur request/response, aucun streaming. Le spike (Task 0) doit valider que `http3.Transport` supporte le body streaming persistant pour CONNECT
- **TOCTOU-safe dialing** : `relay/stun_handler.go` résout le hostname une fois via `resolveAndValidateTarget()` puis dial le `*net.UDPAddr` résolu directement — jamais le hostname. Le handler CONNECT suit ce pattern exactement
- **Auth Ed25519** : Le pattern `verifyRelay()` (`client.go:305-355`) : client génère nonce → relay signe avec Ed25519 private key → client vérifie avec public key. Le client n'a PAS de clé privée (seulement la clé publique relay dans `RelayConfig.PublicKeyEd25519`). **Solution** : lors du `verifyRelay()`, le relay émet un token de session signé Ed25519 (contenant un identifiant client + expiration). Le client attache ce token à chaque requête CONNECT. Le relay vérifie la signature — seul le relay peut émettre des tokens valides
- **Pas de CF-Connecting-IP** : Le relay (`internal/relay/server.go`) reçoit les requêtes via `http.Request.RemoteAddr` standard. Derrière Cloudflare, RemoteAddr = IP edge Cloudflare, pas le client réel. Le connect handler extrait l'IP réelle depuis `CF-Connecting-IP` avec validation des ranges Cloudflare
- **Rate limiting** : `relay/limiter.go` est global-only (`atomic.Int64`, 150 max, pas de tracking IP). Per-IP nécessite une nouvelle structure : `sync.Map[string]*ipState` avec CAS pattern `markedForDeletion` pour cleanup safe
- **Emergency DNS restore** : In-memory uniquement (`originalDNS` map dans `dns/manager_windows.go`), ne survit pas aux crashes. Le proxy WinINET persiste sur disque via DPAPI dans `%AppData%\LeVoile\proxy-original.json` — le tray gère sa propre persistance
- **Token refresh** : `verifyRelay()` dans `tunnel/client.go:305-355` — aucun mécanisme de retry interne. Les retries sont gérés par le `Reconnector`. Le token refresh est un mécanisme nouveau et séparé
- **Thread safety** : mutex par composant, lock court pour accès champ, context cancellation pour shutdown gracieux

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `internal/tunnel/client.go` | Client HTTP/3 — `httpClient` réutilisable, verifyRelay() (305-355), stockage sessionToken |
| `internal/dns/proxy.go` | Pattern `Start(ctx)/Ready()` à reproduire pour le proxy HTTP local |
| `internal/relay/server.go` | Enregistrement handlers + middleware — ajouter `/connect`, IPLimiter, CFIPValidator |
| `internal/relay/doh_handler.go` | Pattern handler relay — modèle pour `connect_handler.go` |
| `internal/relay/stun_handler.go` | Pattern TOCTOU-safe : resolve → dial IP résolue (lignes 111-156) |
| `internal/relay/middleware.go` | `LimitMiddleware` — appliquer au handler CONNECT |
| `internal/relay/limiter.go` | Limiter global atomic — template pour per-IP IPLimiter |
| `internal/relay/verify_handler.go` | Handler verify côté relay — signe le nonce, émettra le session token |
| `internal/crypto/ed25519.go` | GenerateKeyPair, Sign, Verify, Import/ExportPublicKeyBase64 |
| `internal/service/service.go` | Lifecycle composants — 6 mutex, kill switch, shutdown, startProxy pattern |
| `internal/ipchandler/handler.go` | Dispatch IPC — ajouter toggle proxy HTTP, pattern handleSetBlocklist (320-348) |
| `internal/ipc/messages.go` | Constantes actions IPC — ajouter `ActionSetHTTPProxy` |
| `internal/config/config.go` | Config TOML — ajouter `HTTPProxyConfig`, RelayConfig.PublicKeyEd25519 |
| `internal/config/paths_windows.go` | `%AppData%\LeVoile\` — répertoire pour proxy-original.json |
| `internal/tray/tray.go` | Systray UI (599 lignes), polling 2s, handleBlocklistToggle pattern |
| `internal/dns/manager_windows.go` | Emergency restore in-memory (pattern à ne PAS reproduire — persister sur disque) |
| `cmd/client/main.go` | kardianos/service LocalSystem, IPC server registration |
| `cmd/relay/main.go` | Chargement clé privée relay depuis fichier |

### Technical Decisions

- **Menace principale** : FAI français faisant du DPI pour bloquer les VPN
- **Camouflage protocolaire** : Cloudflare en frontal (config externe, pas de code)
- **Utilisateurs** : Quelques dizaines simultanés
- **Performance** : Prioritaire dans les limites du camouflage
- **Trafic** : Web léger uniquement — pas de bande passante lourde
- **Spike HTTP/3 obligatoire** : Le tunnel QUIC est orienté request/response (`io.ReadAll`). CONNECT nécessite streaming bidirectionnel persistant. Sans validation, toute la feature est à risque. Task 0 bloquante — aucune autre task ne commence avant que le spike passe. Le spike inclut un test à travers un reverse proxy TLS (Caddy) pour simuler le comportement Cloudflare
- **Proxy local** : Écoute sur `127.0.0.1:50113` uniquement (sécurité — pas d'écoute externe). Port 50113 au lieu de 8080 pour éviter les conflits avec les serveurs de développement courants (React, Spring Boot, etc.). Vérification de disponibilité au démarrage avec message d'erreur explicite
- **CONNECT tunneling** : Requêtes HTTP CONNECT reçues localement, transmises en POST avec body JSON vers le relay via le tunnel QUIC existant
- **Cible dans le body JSON** : La destination (`host:port`) est transmise dans le body JSON d'une requête POST vers `/connect` (pas dans un header `X-Connect-Target`). Cloudflare voit une requête POST opaque — les CDN loguent les headers séparément du body, ce qui réduit l'exposition de la destination dans les logs automatisés
- **Proxy système WinINET géré par le tray (pas le service)** : Le service LocalSystem ne peut pas accéder au HKCU de l'utilisateur connecté. Architecture : (1) le service gère le proxy HTTP local (listener `127.0.0.1:50113`), (2) le service expose l'état via IPC (`HTTPProxyActive` + `HTTPProxyAddr` + `HTTPProxySeq` — sequence number monotone), (3) le tray détecte le changement lors du polling 2s en comparant le sequence number et configure/restaure WinINET. Détection crash service : si le pipe IPC est cassé (`SendContext()` error), le tray restaure WinINET immédiatement. Multi-utilisateurs : chaque instance tray gère son propre HKCU — résolu nativement
- **Auth Ed25519 sur /connect** : Token de session signé par le relay (Ed25519 private key), émis lors de `verifyRelay()`, attaché par le client à chaque requête CONNECT via header `Authorization: Bearer <token>`. Le relay vérifie la signature. Token contient : `ip_hash` (sha256 de l'IP client réelle via CF-Connecting-IP) + `issued` (timestamp) + `ttl` (14400s = 4h). Le relay vérifie signature + expiration + IP match. L'`ip_hash` est defense-in-depth, pas le mécanisme d'auth principal — l'auth repose sur la signature Ed25519 du nonce (impossible à forger sans la clé privée)
- **CF-Connecting-IP avec whitelist Cloudflare** : Le relay extrait l'IP client depuis `CF-Connecting-IP` au lieu de `RemoteAddr`. Vérification que `RemoteAddr` appartient aux ranges IP Cloudflare (embarquées en dur + refresh 24h via goroutine ticker). Si `RemoteAddr` ∉ Cloudflare IPs et mode `!insecure` → drop TCP silencieux (pas de réponse HTTP). Le mode `insecure` ne doit être activable que par flag CLI + variable d'environnement, jamais par config fichier seul. Note production : le firewall du relay doit bloquer tout trafic sauf les ranges Cloudflare (defense-in-depth réseau)
- **Token TTL 4h (14400s)** : Compromis entre sécurité (fenêtre de vol limitée) et résilience (tolère des interruptions relay de plusieurs heures). Le client refresh proactivement 5min avant expiration
- **Token refresh** : Vérification proactive du TTL avant chaque CONNECT. Refresh protégé par `sync.Mutex` (single-flight). Backoff exponentiel 1s→2s→4s...→60s max. Circuit-breaker après 5 échecs consécutifs (pause 60s). Le circuit-breaker ne bloque que les tentatives de refresh — si le token actuel est encore valide, l'utiliser normalement. Si circuit-breaker ouvert ET token expiré → erreur explicite (pas de blocage silencieux)
- **Per-IP limiting avec CAS** : `sync.Map[string]*ipState` où `ipState` contient `active atomic.Int64`, `lastSeen atomic.Int64`, `markedForDeletion atomic.Bool`. Goroutine de cleanup toutes les 60s. CAS pattern : cleanup set `markedForDeletion = true` si `active == 0` et `lastSeen > 5min`. `Acquire()` vérifie et reset le flag si true (entrée survit). Prochain cycle : si flag toujours true → `sync.Map.Delete()` safe. Limite = 20 connexions concurrentes par IP source
- **Idle timeout 120s** : Activité = octets transférés (`n > 0`) dans l'une ou l'autre direction via wrapper `idleTimeoutConn` qui reset un `time.Timer(120s)` à chaque `Read()`/`Write()` réussi. TCP keepalives ne comptent pas. Timer expiré → `conn.Close()` bidirectionnel. 120s est le standard proxy HTTP
- **Plain HTTP** : TCP RST silencieux (`conn.Close()` sans réponse HTTP) pour les requêtes non-CONNECT. Ne confirme pas l'existence d'un proxy à un scanner. Cohérent avec la posture privacy
- **Connection draining 5s** : Séquence critique : (1) kill switch active le firewall Windows (bloque tout trafic sortant), (2) proxy arrête d'accepter de nouvelles connexions, (3) signale `context.Cancel()` aux goroutines CONNECT actives, (4) attend `sync.WaitGroup` avec `time.After(5s)`, (5) force close si timeout. Le firewall est activé AVANT le draining — les connexions en draining sont déjà bloquées, aucune fuite IP pendant la grace period
- **Crash recovery** : `proxy-original.json` chiffré via DPAPI (`CryptProtectData`), écrit atomiquement (temp file → `os.Rename()`). `RecoverOrphan()` au démarrage du tray vérifie si le fichier existe et si le registre pointe vers `127.0.0.1:50113`. DPAPI lie les données au credential de l'utilisateur — résistant à la lecture par d'autres processus. Interface `DataProtector` pour testabilité (ADR-1)
- **ProxyOverride inclut le relay domain** : `"localhost;127.0.0.1;*.local;<local>;{relay_domain}"` — le domaine relay est lu depuis la config (hostname, pas IP). ADR-3 : hostname préféré à l'IP résolue car failover-safe (le Reconnector change de relay → DNS re-résout). Le risque fingerprint registre est mineur
- **Authorization sanitized dans les logs** : Le header `Authorization` ne doit JAMAIS apparaître dans les logs relay (ni debug, ni panic recovery). Remplacé par `[REDACTED]`
- **CF ranges refresh** : Ranges embarquées en dur comme fallback + refresh obligatoire toutes les 24h via goroutine ticker. Fetch `cloudflare.com/ips-v4` + `/ips-v6` → parse → valider ≥10 ranges IPv4 CIDR → remplacer atomiquement via `atomic.Pointer[[]netip.Prefix]` (ADR-4). Si fetch échoue → garder ranges actuelles. Si >30 jours sans refresh → log warning
- **Kill switch** : Le proxy HTTP est intégré aux callbacks pause/resume du kill switch
- **SSRF protection** : Valider les destinations côté relay (bloquer loopback, RFC 1918, link-local). TOCTOU fix : résoudre une fois → dialer l'IP résolue directement
- **Pas de restructuration** : Extension uniquement — aucun code existant modifié en profondeur

## Implementation Plan

### Tasks

Les tâches sont ordonnées pour testabilité précoce : le spike valide d'abord la faisabilité, puis une vertical slice (relay handler + client handler) permet un test d'intégration minimal avant d'élargir.

---

- [ ] **Task 0 : SPIKE — Valider le streaming bidirectionnel HTTP/3 avec quic-go (BLOQUANTE)**
  - File: `cmd/spike-h3-streaming/main.go` (NOUVEAU, temporaire — ne sera pas mergé)
  - Action: Créer un PoC minimal qui :
    1. Démarre un serveur HTTP/3 avec quic-go qui accepte des requêtes POST sur `/tunnel`
    2. Côté serveur : lit le body de la requête comme un flux (`io.Reader`), ouvre une connexion TCP vers un serveur echo local, relaye bidirectionnellement entre le body/response HTTP/3 et la connexion TCP
    3. Côté client : crée une requête POST vers `/tunnel` via `http3.Transport`, écrit dans le body via `io.Pipe()`, lit la réponse comme un flux (`resp.Body` sans `io.ReadAll`)
    4. Vérifie que les données circulent dans les deux sens en temps réel (pas de buffering complet)
    5. **Étape Cloudflare simulation** : Configurer Caddy en reverse proxy TLS devant le serveur HTTP/3 de test. Vérifier que le streaming bidirectionnel survit à la terminaison TLS + re-encryption du reverse proxy. Si le buffering Caddy casse le streaming, documenter le comportement et évaluer les alternatives (chunked transfer, SSE, WebSocket upgrade)
  - Critère de succès : Les données circulent bidirectionnellement en streaming sur un seul stream QUIC HTTP/3 **à travers un reverse proxy TLS** (simulation CDN). Si `quic-go` ne supporte pas ce mode, documenter l'alternative (ex: raw QUIC streams, WebSocket over HTTP/3, ou fallback HTTP/2).
  - Notes: **AUCUNE autre tâche ne commence avant que ce spike passe.** Si le spike échoue, l'architecture entière doit être repensée. Durée estimée : 2-4h.

---

- [ ] **Task 1 : Configuration — Ajouter `HTTPProxyConfig` + port 50113**
  - File: `internal/config/config.go`
  - Action: Ajouter la struct `HTTPProxyConfig` avec champs `Enabled bool`, `Port int` (défaut 50113). Ajouter le champ `HTTPProxy HTTPProxyConfig` à la struct `Config`.
  - File: `installer/config-default.toml`
  - Action: Ajouter la section `[http_proxy]` avec `enabled = false` et `port = 50113`.
  - Notes: Suivre le pattern exact de `BlocklistConfig`. Port 50113 au lieu de 8080 pour éviter les conflits avec les serveurs de développement courants.

---

- [ ] **Task 2 : Messages IPC — Ajouter les constantes proxy HTTP**
  - File: `internal/ipc/messages.go`
  - Action:
    1. Ajouter `ActionSetHTTPProxy = "set_http_proxy"` aux constantes d'action
    2. Ajouter `HTTPProxyActive bool`, `HTTPProxyAddr string`, et `HTTPProxySeq uint64` au struct `Response`. Le sequence number est un compteur monotone incrémenté à chaque changement d'état du proxy (start/stop). Le tray compare `seq` entre deux polls : si identique, pas de changement → pas de modification WinINET
  - Notes: Pattern identique à `ActionSetBlocklist` / `BlocklistEnabled`.

---

- [ ] **Task 3 : Auth — Token de session dans verifyRelay (client + relay) + CF-Connecting-IP**
  - File: `internal/relay/verify_handler.go`
  - Action: Étendre la réponse `VerifyResponse` pour inclure un `SessionToken string`. Après signature du nonce, générer un token de session : JSON `{"ip_hash": sha256(client_real_ip), "issued": unix_timestamp, "ttl": 14400}` → signer avec Ed25519 private key → base64-encode. Retourner dans `session_token` du JSON de réponse. `client_real_ip` est extrait via `CloudflareIPValidator.ExtractClientIP(r)`.
  - File: `internal/tunnel/client.go`
  - Action: Étendre `verifyRelay()` pour extraire et stocker le `sessionToken` reçu. Ajouter un champ `sessionToken string` protégé par le `sync.RWMutex` existant. Ajouter un getter `SessionToken() string`. Ajouter le mécanisme de **token refresh** : si `issued + ttl - 300 < now`, appeler `refreshToken()` protégé par `sync.Mutex` (single-flight). Backoff exponentiel 1s→2s→4s→...→60s max. Circuit-breaker après 5 échecs consécutifs (pause 60s). Le circuit-breaker ne bloque que les tentatives de refresh — si le token actuel est encore valide (non expiré), l'utiliser normalement.
  - File: `internal/relay/cfip.go` (NOUVEAU)
  - Action: Créer le struct `CloudflareIPValidator` avec :
    - `IsTrustedSource(remoteAddr string) bool` — vérifie que l'IP source appartient aux ranges Cloudflare
    - `ExtractClientIP(r *http.Request) (string, error)` — extrait l'IP depuis `CF-Connecting-IP` si source trusted, sinon `RemoteAddr`
    - Ranges IP embarquées en dur comme fallback (~15 IPv4, ~6 IPv6)
    - Refresh via goroutine ticker 24h : fetch `cloudflare.com/ips-v4` + `/ips-v6` → parse → valider ≥10 ranges IPv4 CIDR → remplacer atomiquement via `atomic.Pointer[[]netip.Prefix]` (ADR-4)
    - Si fetch échoue → garder ranges actuelles. Si >30 jours → log warning
    - Si `RemoteAddr` ∉ Cloudflare IPs et mode `!insecure` → drop TCP silencieux (pas de réponse HTTP)
    - Mode `insecure` activable uniquement par flag CLI + variable d'environnement (jamais config fichier)
  - Notes: Le `ip_hash` utilise l'IP réelle du client (via CF-Connecting-IP), pas l'IP edge Cloudflare. L'`ip_hash` est defense-in-depth — l'auth repose sur le nonce Ed25519 signé. Un attaquant avec la même IP source (NAT) ne peut pas forger le nonce sans la clé privée.

---

- [ ] **Task 4 : Handler relay — Forward proxy côté serveur**
  - File: `internal/relay/connect_handler.go` (NOUVEAU)
  - Action: Créer un handler HTTP qui :
    1. Accepte uniquement les requêtes **POST** (pas CONNECT — le CONNECT est géré côté client local, le relay reçoit un POST avec la cible dans le body)
    2. **Authentification** : Extraire le header `Authorization: Bearer <token>`, décoder le token, vérifier la signature Ed25519 avec la clé relay, vérifier l'expiration (TTL), vérifier que `ip_hash == sha256(client_real_ip)` où `client_real_ip` vient de `CloudflareIPValidator.ExtractClientIP(r)`. Rejeter avec HTTP 401 si invalide. **Sanitization logs** : le header `Authorization` est remplacé par `[REDACTED]` dans tous les logs et le panic recovery middleware
    3. **Extraire la cible** : Lire le body JSON `{"target": "host:port"}` — la cible n'est PAS dans un header (invisible aux logs CDN)
    4. **Valider la destination** : Rejeter loopback `127.0.0.0/8`, réseaux privés `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, link-local `169.254.0.0/16`, IPv6 loopback `::1`, IPv6 link-local `fe80::/10`
    5. **Résoudre le hostname** via DNS → obtenir `net.IP`
    6. **Valider l'IP résolue** (même checks anti-SSRF — empêcher le DNS rebinding). **TOCTOU fix** : utiliser l'IP résolue directement pour le dial, ne PAS re-résoudre le hostname. Pattern : `net.DialTCP("tcp", nil, &net.TCPAddr{IP: resolvedIP, Port: port})` — identique au pattern STUN dans `stun_handler.go:174`
    7. Répondre `200 OK` avec headers de streaming
    8. Relayer bidirectionnellement (`io.Copy` dans deux goroutines) entre le body/response HTTP/3 et la connexion TCP destination, avec wrapper `idleTimeoutConn` qui reset un `time.Timer(120s)` à chaque `Read()`/`Write()` avec `n > 0`. Timer expiré → `conn.Close()` bidirectionnel
    9. **Connection draining** : `http.Server.Shutdown(ctx)` avec deadline 5s fonctionne nativement côté relay car les connexions restent trackées par le serveur HTTP (pas de `Hijack()` — streaming body HTTP/3). ADR-5
  - File: `internal/relay/ip_limiter.go` (NOUVEAU)
  - Action: Créer un `IPLimiter` struct :
    - Structure : `sync.Map` mapping `string` (IP) → `*ipState` où `ipState` contient `active atomic.Int64`, `lastSeen atomic.Int64` (unix timestamp), `markedForDeletion atomic.Bool`
    - `Acquire(ip string) bool` : incrémente le compteur, retourne false si > 20. Si `markedForDeletion == true`, reset le flag et continuer (CAS pattern — l'entrée survit)
    - `Release(ip string)` : décrémente le compteur, met à jour `lastSeen`
    - `StartCleanup(ctx context.Context)` : goroutine qui toutes les 60s : si `active == 0` et `lastSeen > 5min`, set `markedForDeletion = true`. Prochain cycle : si flag toujours true → `sync.Map.Delete()` safe (aucun `Acquire()` concurrent n'a touché l'entrée)
    - Sémantique : connexions **concurrentes** par IP, pas par fenêtre temporelle
  - Notes: Le handler NE fait PAS de hijack HTTP — il utilise le streaming HTTP/3 body pour le tunnel. Le `IPLimiter` est distinct du `Limiter` global existant — les deux s'appliquent (global ET per-IP).

---

- [ ] **Task 5 : Enregistrement du handler relay + vertical slice test**
  - File: `internal/relay/server.go`
  - Action:
    1. Ajouter les champs `ConnectHandler http.Handler`, `IPLimiter *IPLimiter`, `CFIPValidator *CloudflareIPValidator` au struct `Server`
    2. Enregistrer conditionnellement : `if s.ConnectHandler != nil { mux.Handle("/connect", LimitMiddleware(s.Limiter, s.ConnectHandler)) }`
    3. Passer `SigningKey`, `IPLimiter`, et `CFIPValidator` au handler
    4. Appeler `IPLimiter.StartCleanup(ctx)` dans `Server.Serve()`
    5. Lancer le goroutine ticker 24h pour le refresh CF ranges dans `Server.Serve(ctx)` (ADR-4)
  - File: `internal/relay/connect_handler_test.go` (NOUVEAU)
  - Action: Tests unitaires du handler relay :
    - Token valide + destination valide → 200 + streaming
    - Token absent → 401
    - Token expiré → 401
    - Token IP mismatch → 401
    - Destination loopback/privée → 403
    - Destination avec DNS rebinding (hostname résout vers IP privée) → 403
    - Per-IP limit dépassée → 429
    - Requête non-POST → 405
    - Idle timeout → connexion fermée après 120s d'inactivité
    - Connexion directe non-Cloudflare → drop TCP silencieux
    - Header `Authorization` sanitized dans logs (panic recovery ne contient pas le token)
    - Token TTL = 14400s
  - Notes: Ce test + Task 6 forment la **vertical slice** — testable de bout en bout dès que ces deux tâches sont complètes.

---

- [ ] **Task 6 : Proxy HTTP local côté client — Serveur + Handler CONNECT (vertical slice)**
  - File: `internal/httpproxy/server.go` (NOUVEAU)
  - Action: Créer un struct `Server` avec :
    - `listenAddr string` (défaut `127.0.0.1:50113`)
    - `tunnelClient *tunnel.Client` (pour envoyer les requêtes via le tunnel)
    - `ready chan struct{}` (signal de disponibilité)
    - `wg sync.WaitGroup` (tracking des connexions hijackées pour draining)
    - Méthode `Start(ctx context.Context) error` — bloque jusqu'à annulation du context
    - Méthode `Ready() <-chan struct{}` — retourne le channel de disponibilité
    - Écoute TCP sur `listenAddr`, accepte les connexions, les dispatche au handler CONNECT
    - **Vérification port disponible** : tenter `net.Listen` et si `bind: address already in use`, retourner une erreur explicite : `"port %d already in use — configure a different port in [http_proxy] section"`
    - Écouter uniquement sur `127.0.0.1` — jamais sur `0.0.0.0`
  - File: `internal/httpproxy/connect_handler.go` (NOUVEAU)
  - Action: Implémenter le handler qui :
    1. Accepte les requêtes **CONNECT** du navigateur (parse host:port)
    2. **Requêtes non-CONNECT** (GET, POST plain HTTP) : TCP RST silencieux — `conn.Close()` sans écrire de réponse HTTP. Ne confirme pas l'existence d'un proxy
    3. Construit une requête **POST** vers `https://{relayDomain}/connect` via le `httpClient` HTTP/3 du tunnel
    4. Attache le header `Authorization: Bearer <sessionToken>` (obtenu via `tunnelClient.SessionToken()`)
    5. **Token refresh proactif** : avant chaque CONNECT, vérifier `token.IssuedAt + token.TTL - 5min < now`. Si expiré/proche, appeler `refreshToken()` (single-flight mutex, backoff 1s→60s, circuit-breaker 5 échecs). Si circuit-breaker ouvert ET token expiré → erreur explicite vers le client local
    6. **Passe la cible dans le body JSON** : `{"target": "host:port"}` — PAS dans un header
    7. Reçoit la réponse 200 du relay
    8. Répond `200 Connection Established` au navigateur
    9. Hijack la connexion TCP locale (`wg.Add(1)` au hijack, `wg.Done()` à la fin) et relaye bidirectionnellement entre la connexion locale et le stream QUIC HTTP/3 (body request/response)
  - File: `internal/httpproxy/server_test.go` (NOUVEAU)
  - Action: Tests unitaires :
    - Démarrage/arrêt lifecycle (Start/Ready/cancel)
    - Écoute sur le bon port
    - Refus des connexions depuis une interface non-loopback
    - Port déjà occupé → erreur explicite
  - File: `internal/httpproxy/connect_handler_test.go` (NOUVEAU)
  - Action: Tests unitaires :
    - Requête CONNECT valide → tunnel via mock
    - Requête GET plain HTTP → connexion fermée (pas de réponse HTTP)
    - Token de session absent → erreur propagée
    - Token expiré → refresh single-flight
  - Notes: Avec Task 5 complète, on peut faire un **test d'intégration minimal** : proxy local → tunnel HTTP/3 → relay handler → destination. Pattern lifecycle identique à `dns/proxy.go`.

---

- [ ] **Task 7 : Proxy système Windows — WinINET registre dans le TRAY + DPAPI + crash recovery**
  - File: `internal/tray/sysproxy_windows.go` (NOUVEAU)
  - Action: Créer un manager qui :
    1. `Save()` — lit les valeurs actuelles du registre WinINET (`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`) : clés `ProxyEnable` (DWORD), `ProxyServer` (string), `ProxyOverride` (string). Chiffre les données via DPAPI (`CryptProtectData`) et persiste dans `%AppData%\LeVoile\proxy-original.json` via **atomic write** (temp file → `os.Rename()`, pattern identique à `config.go:98-124`). Si crash entre write et rename → pas de fichier corrompu
    2. `Set(addr string)` — écrit dans le registre : `ProxyEnable=1`, `ProxyServer="127.0.0.1:50113"`, `ProxyOverride="localhost;127.0.0.1;*.local;<local>;{relay_domain}"` (relay domain depuis la config, hostname pas IP — failover-safe, ADR-3). Broadcast `WM_SETTINGCHANGE` via `SendMessageTimeout(HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Internet Settings")` pour notifier les navigateurs immédiatement
    3. `Restore()` — lit `proxy-original.json`, déchiffre via DPAPI (`CryptUnprotectData`), restaure les valeurs registre, supprime le fichier JSON, broadcast `WM_SETTINGCHANGE`
    4. `RecoverOrphan()` — appelé au **démarrage du tray** : si `proxy-original.json` existe ET le registre pointe vers `127.0.0.1:50113` → crash précédent détecté → appeler `Restore()`. Si le fichier existe mais le registre ne pointe pas vers notre proxy → l'utilisateur a changé manuellement → supprimer le fichier sans modifier le registre. **Validation anti-empoisonnement** : avant restauration, valider le contenu — (1) `ProxyEnable` ∈ {0, 1}, (2) `ProxyServer` vide ou format `host:port` valide, (3) vérifier l'intégrité via DPAPI — si le déchiffrement échoue (fichier corrompu, créé par un autre utilisateur, forgé) → supprimer sans restaurer + log warning
    5. Rollback atomique si `Set()` échoue partiellement (ex: écriture `ProxyEnable` réussit mais `ProxyServer` échoue → restaurer `ProxyEnable`)
    6. **ADR-1 : Interface `DataProtector`** pour testabilité DPAPI. Interface : `Protect(data []byte) ([]byte, error)` + `Unprotect(data []byte) ([]byte, error)`. Implémentation Windows : `dpapi_windows.go` utilise `CryptProtectData`/`CryptUnprotectData`. Implémentation test/stub : `dpapi_stub.go` (build tag `!windows`) retourne les données en clair. `SysProxy` reçoit un `DataProtector` par injection de dépendance
  - File: `internal/tray/sysproxy_stub.go` (NOUVEAU — build tag `!windows`)
  - Action: Stub no-op pour compilation cross-platform. Toutes les méthodes retournent `nil`.
  - Notes: Utiliser `golang.org/x/sys/windows/registry` pour l'accès registre (disponible en dépendance transitive via kardianos/service). Le broadcast `WM_SETTINGCHANGE` est critique — sans lui, les navigateurs ne détectent pas le changement de proxy avant redémarrage.

---

- [ ] **Task 8 : Intégration service lifecycle (simplifiée — le tray gère WinINET)**
  - File: `internal/service/service.go`
  - Action:
    1. Ajouter les champs : `httpProxyMu sync.Mutex` (7ème mutex, après les 6 existants), `httpProxyCancel context.CancelFunc`, `httpProxyErrCh chan error`, `httpProxy *httpproxy.Server`
    2. Créer `startHTTPProxy(ctx context.Context) error` — suivre le pattern exact de `startProxy()` (service.go:870-927). Mutex lock → créer serveur → context+cancel → goroutine Start → wait Ready(). Si le listener échoue (port occupé), retourner erreur explicite. Pas de config WinINET — le tray détecte l'état via polling
    3. Créer `stopHTTPProxy()` — suivre le pattern exact de `stopProxy()` (service.go:930-951). **ADR-5 : Connection draining via `sync.WaitGroup` custom** — le proxy local utilise `Hijack()` pour les connexions CONNECT, donc `http.Server.Shutdown()` ne track pas ces connexions. Séquence : chaque goroutine CONNECT fait `wg.Add(1)` au hijack et `wg.Done()` à la fin. `stopHTTPProxy()` : cancel context → `select { case <-wgDone(): ok | case <-time.After(5s): force close }`. **Quand appelé depuis le kill switch** : le firewall Windows est activé AVANT `stopHTTPProxy()` — aucune fuite IP possible pendant la grace period
    4. Insérer `startHTTPProxy` après le démarrage du proxy DNS (Stage 2) et avant le watchdog (Stage 4)
    5. **Shutdown** : insérer `stopHTTPProxy()` entre STUN stop (ligne 677) et kill switch deactivate (ligne 679)
    6. **Kill switch** : ajouter `p.stopHTTPProxy()` dans `DisableFunc` (après `p.stopProxy()` ligne 488). Ajouter `p.startHTTPProxy(reconnCtx)` dans `ReconnectFunc` (après `p.startProxy()` ligne 490)
    7. **Defer emergency proxy stop** : ajouter un defer dans `run()` qui appelle `stopHTTPProxy()` si le proxy est actif. Pas de restore registre (le tray gère) — le service arrête juste le listener
    8. Ajouter méthodes `EnableHTTPProxy()`/`DisableHTTPProxy()` pour le toggle runtime via IPC
    9. Ajouter méthodes `HTTPProxyActive() bool`, `HTTPProxyAddr() string`, `HTTPProxySeq() uint64` sur Program, utilisées par `handleGetStatus()`
  - Notes: **Complexité reconnue** : cette tâche modifie le fichier le plus critique du codebase (14+ composants, 6 mutex existants). L'intégration est **simplifiée** par rapport à l'architecture originale car le service ne touche plus au registre. **Plan de rollback** : si l'intégration déstabilise le service lifecycle, les changements dans `service.go` peuvent être revertés via `git revert` indépendamment des Tasks 4-7 (le proxy et le relay handler sont autonomes). Le proxy HTTP ne démarre que si `config.HTTPProxy.Enabled == true`.

---

- [ ] **Task 9 : Handler IPC — Toggle proxy HTTP + intégration tray**
  - File: `internal/ipchandler/handler.go`
  - Action:
    1. Ajouter le case `ipc.ActionSetHTTPProxy` dans le switch dispatch de `Handle()`
    2. Implémenter `handleSetHTTPProxy(prg, req)` qui : valide `req.Value` ("true"/"false"), charge la config, modifie `HTTPProxy.Enabled`, sauvegarde atomiquement, appelle `prg.EnableHTTPProxy()` ou `prg.DisableHTTPProxy()`
    3. Dans `handleGetStatus()` : populer `HTTPProxyActive`, `HTTPProxyAddr`, et `HTTPProxySeq` depuis `prg.HTTPProxyActive()`, `prg.HTTPProxyAddr()`, et `prg.HTTPProxySeq()`
  - File: `internal/tray/tray.go`
  - Action:
    1. Dans `updateTrayState(resp)` : détecter le changement via **comparaison du `HTTPProxySeq`** (pas juste `HTTPProxyActive`). Si `resp.HTTPProxyActive` change de `false` → `true` : appeler `sysProxy.Save()` puis `sysProxy.Set(resp.HTTPProxyAddr)`. Si `true` → `false` : appeler `sysProxy.Restore()`
    2. **Détection crash service** : si `ipc.Client.SendContext()` retourne une erreur pipe (service inaccessible), restaurer WinINET immédiatement sans attendre le prochain poll 2s
    3. Au démarrage tray : appeler `sysProxy.RecoverOrphan()` avant le polling loop
    4. Ajouter un menu item toggle proxy dans le tray (pattern identique à blocklist toggle)
  - Notes: Copier le pattern de `handleSetBlocklist()`. Le toggle fonctionne : tray envoie `set_http_proxy` → service start/stop listener → tray détecte changement via seq → tray configure/restaure registre.

---

- [ ] **Task 10 : Tests (unitaires + intégration + spike validation)**
  - File: `internal/relay/ip_limiter_test.go` (NOUVEAU)
  - Action: Tests pour le IPLimiter : acquire/release, limite 20 par IP, CAS flag race (goroutines concurrentes Acquire pendant cleanup), cleanup après TTL, entrée non perdue pendant cleanup
  - File: `internal/relay/cfip_test.go` (NOUVEAU)
  - Action: Tests pour CloudflareIPValidator :
    - IsTrustedSource avec IPs Cloudflare valides/invalides
    - ExtractClientIP avec/sans header, mode insecure
    - Refresh ranges (fetch mock → remplacement atomique)
    - Ranges stales >30j → warning log
    - Fetch fail → fallback ranges embarquées
    - Goroutine ticker refresh (mock ticker → verify atomic replace)
    - `atomic.Pointer` swap (concurrent read pendant swap → pas de race)
  - File: `internal/tray/sysproxy_windows_test.go` (NOUVEAU)
  - Action: Tests pour save/set/restore/recoverOrphan via mock `registryAccessor` interface :
    - RecoverOrphan avec fichier empoisonné (DPAPI déchiffrement échoué → suppression)
    - RecoverOrphan avec ProxyServer malveillant (valeur suspecte → suppression)
    - Détection pipe cassé → restauration immédiate
    - Save() atomic write (simuler crash entre write et rename → pas de fichier corrompu)
    - ProxyOverride contient relay domain depuis config (ADR-3)
    - DataProtector interface mock (ADR-1)
    - Broadcast WM_SETTINGCHANGE, rollback partiel
  - File: `internal/httpproxy/idle_timeout_test.go` (NOUVEAU)
  - Action: Tests pour `idleTimeoutConn` : timer reset sur transfert, timeout après inactivité 120s, pas de timeout pendant transfert lent continu
  - File: Tests d'intégration dans `internal/httpproxy/integration_test.go` ou extension de `relay/e2e_test.go`
  - Action:
    - Test end-to-end : proxy local → tunnel HTTP/3 → relay handler → serveur echo HTTP destination. Vérifier que les données circulent bidirectionnellement
    - Kill switch : vérifier que le proxy est coupé lors de la déconnexion tunnel
    - Auth : requête sans token → 401, avec token valide → 200 + tunnel
    - Token refresh : token expiré → refresh single-flight → requête réussie
    - Connection draining : connexions actives ont 5s grace period sur shutdown
  - Notes: Suivre le pattern table-driven existant. Utiliser `freeTCPAddr(t)` helper pour les ports libres. Les tests CAS IPLimiter sont critiques — ils doivent prouver qu'aucune entrée n'est perdue lors d'un Acquire concurrent au cleanup.

---

### Acceptance Criteria

- [ ] **AC 0** : Given le spike HTTP/3 streaming avec Caddy en reverse proxy TLS, when le client envoie des données en streaming, then les données circulent bidirectionnellement **à travers le reverse proxy** sans buffering complet.
- [ ] **AC 1** : Given le service est démarré avec `http_proxy.enabled = true`, when le proxy HTTP démarre, then il écoute sur `127.0.0.1:50113` et le channel `Ready()` est fermé.
- [ ] **AC 2** : Given le port 50113 est déjà occupé par un autre processus, when le proxy HTTP tente de démarrer, then il retourne une erreur explicite mentionnant le port et la section de config `[http_proxy]`.
- [ ] **AC 3** : Given le proxy local écoute, when un navigateur envoie une requête CONNECT vers `example.com:443`, then la requête est tunnelisée via POST HTTP/3 vers le relay `/connect` avec la cible dans le body JSON (pas dans un header), le relay ouvre une connexion TCP vers `example.com:443`, et le trafic est relayé bidirectionnellement en streaming.
- [ ] **AC 4** : Given le proxy est actif et le proxy système WinINET est configuré, when un navigateur (Chrome/Edge/Firefox) accède à `ifconfig.me`, then l'IP affichée est celle du relay islandais (`levoile.dev`), pas celle du client français.
- [ ] **AC 5** : Given le proxy local reçoit une requête CONNECT vers `127.0.0.1:*` ou un réseau privé (`10.x`, `172.16.x`, `192.168.x`), when le relay valide la destination (y compris après résolution DNS — protection DNS rebinding), then la requête est rejetée avec HTTP 403.
- [ ] **AC 6** : Given un hostname résolvant vers une IP privée (DNS rebinding), when le relay résout le hostname et valide l'IP résolue, then la requête est rejetée avec HTTP 403 ET le relay ne dial PAS le hostname — il utilise l'IP résolue pour le dial (TOCTOU fix).
- [ ] **AC 7** : Given le proxy est actif sur Windows, when le service démarre et le tray détecte l'état proxy via IPC, then le tray configure le registre WinINET (`HKCU\...\Internet Settings`) avec `ProxyEnable=1`, `ProxyServer=127.0.0.1:50113`, et `ProxyOverride` incluant localhost et le relay domain. Un broadcast `WM_SETTINGCHANGE` est envoyé.
- [ ] **AC 8** : Given le proxy système WinINET était configuré par Le Voile, when le tray a crashé et redémarre, then `RecoverOrphan()` détecte le proxy orphelin via `proxy-original.json` (chiffré DPAPI) persisté sur disque, restaure les valeurs registre, et supprime le fichier.
- [ ] **AC 9** : Given le proxy système est configuré, when le service s'arrête proprement, then le tray détecte le changement d'état via IPC, restaure le registre WinINET, supprime `proxy-original.json`, et broadcast `WM_SETTINGCHANGE`.
- [ ] **AC 10** : Given le tunnel se déconnecte (kill switch activé), when le kill switch coupe la connectivité, then le firewall est activé AVANT le draining, le proxy HTTP local est arrêté avec grace period 5s pour les connexions actives, et le tray restaure le proxy système — aucune fuite d'IP possible pendant la grace period.
- [ ] **AC 11** : Given un utilisateur envoie `set_http_proxy` avec `value: "true"` via IPC, when le handler traite la requête, then la config est sauvegardée et le proxy démarre au runtime.
- [ ] **AC 12** : Given un utilisateur envoie `set_http_proxy` avec `value: "false"` via IPC, when le handler traite la requête, then le proxy est arrêté, le tray détecte le changement et restaure le proxy système WinINET, et la config est sauvegardée.
- [ ] **AC 13** : Given le relay a 20 connexions CONNECT actives depuis la même IP, when une 21ème requête arrive de cette IP, then elle est rejetée avec HTTP 429. Les entrées IP avec 0 connexions actives sont nettoyées après 5min par la goroutine de cleanup (CAS pattern).
- [ ] **AC 14** : Given le proxy local reçoit une requête GET `http://example.com/` (plain HTTP, pas CONNECT), when le handler traite la requête, then la connexion TCP est fermée silencieusement sans réponse HTTP (TCP RST).
- [ ] **AC 15** : Given un client qui n'a pas complété `verifyRelay()` (pas de token de session), when il envoie une requête vers `/connect` sur le relay, then la requête est rejetée avec HTTP 401.
- [ ] **AC 16** : Given un client avec un token de session expiré ou émis pour une autre IP, when il envoie une requête vers `/connect`, then la requête est rejetée avec HTTP 401.
- [ ] **AC 17** : Given le relay derrière Cloudflare, when le connect handler extrait l'IP client, then il utilise `CF-Connecting-IP` (pas `RemoteAddr`) et vérifie que `RemoteAddr` ∈ ranges IP Cloudflare. Si connexion directe (non-CF) en mode `!insecure` → drop TCP silencieux.
- [ ] **AC 18** : Given le client avec un token proche de l'expiration (< 5min TTL restant), when une requête CONNECT arrive, then le client refresh le token via `verifyRelay()` (single-flight mutex) avant d'envoyer la requête. Si refresh échoue, backoff exponentiel. Si circuit-breaker ouvert ET token encore valide → utiliser le token existant.

## Additional Context

### Dependencies

- **`golang.org/x/sys/windows/registry`** — Pour le tray : accès registre WinINET. Disponible en dépendance transitive existante via kardianos/service
- **DPAPI** (`CryptProtectData`/`CryptUnprotectData`) — Protection de `proxy-original.json` dans le tray. Disponible nativement via `syscall` ou `golang.org/x/sys/windows`. Lie les données au credential store de l'utilisateur
- **Cloudflare IP ranges** : `https://cloudflare.com/ips-v4` et `/ips-v6`. Embarquées en dur dans `internal/relay/cfip.go` comme fallback + refresh obligatoire toutes les 24h. ~15 ranges IPv4, ~6 ranges IPv6 — taille négligeable. Validation : ≥10 ranges IPv4 en format CIDR valide
- **Caddy** (spike uniquement) : Reverse proxy TLS pour simuler le comportement CDN dans le spike HTTP/3. Dépendance dev uniquement
- **IPC named pipe existant** : Le tray et le service communiquent déjà via `\\.\pipe\levoile`. Pas de nouvelle dépendance transport
- **Cloudflare** : Configuration manuelle préalable par l'utilisateur sur `levoile.dev` (DNS proxied)
- **Tunnel actif** : Le proxy HTTP nécessite un tunnel QUIC connecté — il ne démarre qu'après `tunnel.Connect()` + `verifyRelay()` (pour obtenir le token de session)

### Testing Strategy

**Tests unitaires :**
- Handler relay CONNECT : auth token (valide, expiré, IP mismatch CF, absent), validation destination, anti-SSRF, DNS rebinding TOCTOU, méthodes HTTP, timeout 120s, connexion directe non-CF → drop TCP silencieux, header Authorization sanitized dans logs, token TTL = 14400s
- IPLimiter : acquire/release, limite 20 par IP, CAS race test (goroutines Acquire concurrentes au cleanup), cleanup après TTL, entrée non perdue pendant cleanup
- CloudflareIPValidator : IsTrustedSource (IPs CF valides/invalides/edge cases), ExtractClientIP (avec/sans header, mode insecure), refresh ranges (fetch mock → remplacement atomique), ranges stales >30j → warning log, goroutine ticker refresh (mock ticker → atomic replace), atomic.Pointer swap concurrent
- Proxy local : lifecycle Start/Stop/Ready, port occupé, connexion refusée sur interface non-loopback
- Handler CONNECT client : parsing requêtes CONNECT, rejet plain HTTP → conn closed (TCP RST), construction body JSON avec cible, attachement token Authorization, token expiré → refresh single-flight
- Idle timeout : timer reset sur transfert, timeout après inactivité 120s, pas de timeout pendant transfert lent continu
- Connection draining : connexions actives ont 5s grace period sur shutdown
- Proxy système Windows (tray) : save/set/restore/recoverOrphan via mock `registryAccessor`, broadcast WM_SETTINGCHANGE, rollback partiel, RecoverOrphan avec fichier empoisonné (DPAPI échoué → suppression), RecoverOrphan avec ProxyServer malveillant → suppression, détection pipe cassé → restauration immédiate, Save() atomic write (crash résistant), ProxyOverride contient relay domain, DataProtector interface mock (ADR-1)

**Tests d'intégration :**
- **Spike HTTP/3 + reverse proxy TLS** : streaming bidirectionnel à travers Caddy
- **Vertical slice** (dès Tasks 5+6) : proxy local → tunnel HTTP/3 → relay handler → serveur echo destination, avec connection draining
- **Kill switch** : proxy arrêté + connexions drainées 5s sur déconnexion tunnel
- **Auth** : requête sans token → 401, avec token valide → 200 + tunnel
- **Token refresh** : token expiré → refresh single-flight → requête réussie

**Tests manuels :**
- Navigateur avec proxy système WinINET → vérifier IP affichée sur `ifconfig.me`
- Vérifier registre `HKCU\...\Internet Settings` après start/stop/crash du tray
- Fast user switching : vérifier que chaque utilisateur a son propre état proxy
- Broadcast `WM_SETTINGCHANGE` : vérifier prise d'effet immédiate dans Chrome/Edge (pas besoin de redémarrer le navigateur)

### Threat Model

**Attaquants considérés :**
- **Attaquant réseau distant** (ISP, réseau Wi-Fi hostile, CDN) : PROTEGE — tunnel QUIC chiffré, auth Ed25519, CF-Connecting-IP validation, token session avec ip_hash, SSRF/TOCTOU protection, kill switch firewall
- **Opérateur CDN (Cloudflare)** : PARTIELLEMENT PROTEGE — le body JSON contenant la cible `host:port` est accessible après terminaison TLS. Defense-in-depth (cible dans le body, pas dans les headers). Chiffrement e2e client→relay = hors scope v1
- **Attaquant local (malware, process co-résident)** : LIMITE — un process avec les mêmes privilèges utilisateur peut lire le registre WinINET (voit le relay domain dans ProxyOverride — risque fingerprint accepté, voir ADR-3), peut potentiellement interagir avec le named pipe IPC. DPAPI protège `proxy-original.json` au niveau credential (pas au niveau process). Un malware avec accès au credential store de l'utilisateur a déjà un accès complet à sa session — hors périmètre réaliste
- **Attaquant multi-utilisateur (session RDP, fast user switch)** : PROTEGE — chaque tray gère son propre HKCU, DPAPI lie les données au credential du user spécifique

**Limitations acceptées et documentées :**
- **Timing oracle TCP RST** : un scanner réseau peut distinguer le port 50113 (accept TCP → parse → RST) d'un port fermé (RST immédiat kernel). Limitation acceptée : le comportement est indiscernable de milliers de services qui rejettent les connexions non authentifiées. La présence du service Windows `LeVoile` dans le SCM est déjà un indicateur plus fiable pour un attaquant local
- **Token replay même IP (NAT)** : un attaquant sur le même réseau (même IP sortante via CF) partage le même `CF-Connecting-IP`. Le `ip_hash` ne protège pas dans ce cas. Accepté : l'auth repose sur le nonce Ed25519 signé (impossible à forger sans la clé privée), l'`ip_hash` est defense-in-depth contre le vol de token cross-IP

### Notes

- **Honnêteté Cloudflare** — Zero protection cryptographique contre l'opérateur CDN. Cloudflare termine le TLS et a accès complet au body des requêtes POST vers `/connect`, y compris la cible `host:port`. Le placement de la cible dans le body (au lieu d'un header) est uniquement defense-in-depth contre le log scraping automatisé des headers par les CDN — pas une garantie architecturale. Pour une protection complète contre le CDN, il faudrait un chiffrement end-to-end client→relay (hors scope v1).
- **Idle timeout 120s défini** — Activité = octets transférés (`n > 0`) dans l'une ou l'autre direction via `idleTimeoutConn` wrapper. TCP keepalives ne comptent pas. Downloads lents avec pauses < 120s survivent. 120s est le standard proxy HTTP — 30s tuait les connexions actives lors de lecture de page.
- **Multi-utilisateurs** — Chaque instance tray gère son propre HKCU. Fast user switching et sessions RDP supportés nativement — chaque utilisateur connecté a son tray qui configure/restaure son propre proxy.
- **Plan de rollback** — Si l'intégration `service.go` (Task 8) déstabilise le lifecycle, les changements peuvent être revertés via `git revert` du commit correspondant. Les Tasks 4-7 (relay handler, proxy local, tray proxy) sont autonomes et testables indépendamment.
- **ADRs (Architecture Decision Records)** :
  - **ADR-1** : DPAPI confirmé + interface `DataProtector` pour testabilité cross-platform
  - **ADR-2** : TTL 4h (14400s) — compromis sécurité (fenêtre de vol limitée) / résilience (tolère interruptions relay)
  - **ADR-3** : Hostname relay préféré à IP résolue dans ProxyOverride pour failover-safety (Reconnector change de relay → DNS re-résout)
  - **ADR-4** : Goroutine ticker 24h + `atomic.Pointer[[]netip.Prefix]` pour refresh CF ranges sans latence hot path
  - **ADR-5** : `sync.WaitGroup` custom pour proxy local (hijacked CONNECT) vs `http.Server.Shutdown()` pour relay HTTP/3 (pas de hijack)
- Le DNS est déjà masqué via DoH — cette feature ajoute le masquage IP par-dessus.
- Le STUN relay existe déjà pour l'anti-fuite WebRTC — le proxy CONNECT complète la protection.
- Bootstrap : le relay IP est résolu au démarrage AVANT toute redirection — pas de deadlock possible.
- Futur : support macOS/Linux pour le proxy système, liste blanche de domaines à ne pas proxifier, chiffrement end-to-end de la cible (bypass Cloudflare).
