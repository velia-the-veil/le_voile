---
title: 'Corrections tech-spec IP Camouflage — Review adversariale'
slug: 'fix-ip-camouflage-spec'
created: '2026-03-13'
status: 'ready-for-dev'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.22+', 'QUIC/HTTP3 (quic-go)', 'HTTP CONNECT', 'net/http', 'WinINET registry (HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings)', 'Ed25519 (internal/crypto)', 'sync.Map (per-IP limiting)']
files_to_modify: ['_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md']
code_patterns: ['verifyRelay: client sends nonce → relay signs with Ed25519 private key → client verifies with public key (tunnel/client.go:305-355)', 'resolveAndValidateTarget: resolve once → return *net.UDPAddr → dial resolved addr directly, no second DNS lookup (relay/stun_handler.go:111-156)', 'emergency DNS restore: defer block in service.go:469-477 with dnsRestored flag, catches panics', 'global Limiter: atomic.Int64 counter, 150 max, no per-IP tracking (relay/limiter.go)', 'verify handler registration: conditional mux.Handle with SigningKey != nil check (relay/server.go:55-57)', 'config TOML: RelayConfig.PublicKeyEd25519 stores base64 public key (config/config.go)']
test_patterns: ['Table-driven tests', '_test.go colocated', '_edge_test.go for edge cases', 'freeUDPAddr/freeTCPAddr helper for port allocation', 'commandRunner interface for mocking netsh (dns/manager_windows.go)']
---

# Tech-Spec: Corrections tech-spec IP Camouflage — Review adversariale

**Created:** 2026-03-13

## Overview

### Problem Statement

La spec `tech-spec-ip-camouflage-web-proxy.md` contient 12 défauts identifiés par review adversariale, dont des bloqueurs architecturaux : le streaming HTTP/3 bidirectionnel n'est pas validé, `netsh winhttp` ne configure pas les navigateurs, le endpoint `/connect` est un proxy ouvert sans authentification, et le header `X-Connect-Target` expose les destinations en clair à Cloudflare.

### Solution

Corriger la spec existante sur les 12 points soulevés par la review, en intégrant les décisions techniques validées avec l'utilisateur : spike HTTP/3 en Task 0, registre WinINET, auth Ed25519, chiffrement de la cible dans le body, port 50113, et réorganisation des tâches pour testabilité précoce.

### Scope

**In Scope:**
1. Task 0 spike — Valider streaming bidirectionnel HTTP/3 avec quic-go
2. WinINET registre — Remplacer `netsh winhttp` par modification registre
3. Auth Ed25519 — Token signé pour `/connect` (pattern `verifyRelay`)
4. Chiffrement cible — Dans le body chiffré, pas en header clair
5. Port 50113 — Nouveau défaut
6. Idle timeout 120s — Remplacer 30s
7. DNS rebinding TOCTOU — Dialer l'IP résolue directement
8. Per-IP rate limiting — Détailler structure, cleanup, sémantique
9. Plain HTTP forwarding — Gérer ou documenter HTTPS-only
10. Crash recovery proxy — Check au démarrage pour proxy orphelin
11. Vertical slice — Réordonner tâches pour testabilité précoce
12. Honnêteté complexité — Reconnaître l'intégration profonde dans service.go

**Out of Scope:**
- Réécriture complète de la spec — corrections ciblées uniquement
- Implémentation du code — spec seulement

## Context for Development

### Codebase Patterns

- **HTTP/3 client** : `tunnel/client.go` utilise `io.ReadAll()` + `io.LimitReader()` — pur request/response, aucun streaming. Le spike doit valider que `http3.Transport` supporte le body streaming persistant pour CONNECT.
- **TOCTOU-safe dialing** : `relay/stun_handler.go` résout le hostname une fois via `resolveAndValidateTarget()` puis dial le `*net.UDPAddr` résolu directement — jamais le hostname. Le handler CONNECT doit suivre ce pattern exactement.
- **Auth Ed25519 (investigation approfondie)** : Le pattern `verifyRelay` est inversé par rapport au besoin. Actuellement : client génère nonce → relay signe → client vérifie. Le client n'a PAS de clé privée (seulement la clé publique relay dans `RelayConfig.PublicKeyEd25519`). **Solution retenue** : lors du `verifyRelay()`, le relay émet un token de session signé Ed25519 (contenant un identifiant client + expiration). Le client attache ce token à chaque requête CONNECT. Le relay vérifie la signature du token — seul le relay peut émettre des tokens valides.
- **Emergency restore** : `dns/manager_windows.go` sauvegarde les DNS originaux en mémoire (`originalDNS` map) et `service.go:469-477` a un defer emergency restore avec flag `dnsRestored`. **Limitation** : ce mécanisme ne survit PAS à un crash process (taskkill /F, BSOD). Pour le proxy WinINET, il faut persister l'état original sur disque avant modification, et vérifier au démarrage si un état orphelin existe.
- **Rate limiting** : `relay/limiter.go` est global-only (`atomic.Int64`, 150 max, pas de tracking IP). Per-IP nécessite une nouvelle structure : `sync.Map[string]*atomic.Int64` avec goroutine de cleanup périodique (TTL 5min) pour éviter la croissance mémoire non bornée.

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md` | Spec à corriger |
| `internal/tunnel/client.go` | HTTP/3 client — confirme absence de streaming, pattern verifyRelay (lignes 305-355) |
| `internal/relay/verify_handler.go` | Handler verify côté relay — signe le nonce avec Ed25519 private key |
| `internal/relay/stun_handler.go` | Pattern TOCTOU-safe : resolve → dial IP résolue (lignes 111-156) |
| `internal/relay/limiter.go` | Limiter global actuel — atomic.Int64, 150 max, pas de per-IP |
| `internal/crypto/ed25519.go` | GenerateKeyPair, Sign, Verify, Import/ExportPublicKeyBase64 |
| `internal/dns/manager_windows.go` | Pattern save/restore DNS + emergency restore |
| `internal/service/service.go` | Lifecycle 14+ composants, 15+ stages, 7 mutex — confirme complexité intégration |
| `internal/config/config.go` | Config TOML — RelayConfig.PublicKeyEd25519 stocke clé publique base64 |
| `cmd/relay/main.go` | Chargement clé privée relay depuis fichier (lignes 55-71) |

### Technical Decisions

- **Spike HTTP/3 obligatoire** : Le tunnel QUIC est orienté request/response (`io.ReadAll`). CONNECT nécessite streaming bidirectionnel persistant. Sans validation, toute la feature est à risque. Task 0 bloquante — aucune autre task ne commence avant que le spike passe.
- **WinINET au lieu de WinHTTP** : `netsh winhttp` configure les clients WinHTTP (Windows Update, BITS), pas les navigateurs. Chrome/Edge/Firefox lisent `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`. Modification registre via `golang.org/x/sys/windows/registry` + broadcast `WM_SETTINGCHANGE` via `SendMessageTimeout` (ou `InternetSetOption`).
- **Auth Ed25519 sur /connect** : Token de session signé par le relay (Ed25519 private key), émis lors de `verifyRelay()`, attaché par le client à chaque requête CONNECT via header `Authorization: Bearer <token>`. Le relay vérifie la signature — seul un client ayant complété la vérification relay peut utiliser `/connect`. Pas de clé privée côté client nécessaire. Token contient : IP client hashée + timestamp émission + TTL (ex: 24h). Le relay vérifie signature + expiration + IP match.
- **Cible chiffrée dans le body** : Au lieu du header `X-Connect-Target` visible par Cloudflare après terminaison TLS, la destination (host:port) est transmise dans le body de la requête POST initiale vers `/connect`. Le body contient un JSON avec le token de session et la cible. Cloudflare voit une requête POST opaque vers `/connect`, pas la destination. Note : le body est déjà protégé par TLS entre client et Cloudflare, puis entre Cloudflare et le relay — la cible n'est visible que par Cloudflare (ce qui est mieux que dans un header, car les headers sont souvent loggués séparément du body par les CDN).
- **Port 50113** : Port haut sans conflit courant (8080 = React, Spring Boot, etc.). Vérification de disponibilité au démarrage avec message d'erreur explicite en cas de conflit.
- **Idle timeout 120s** : Les navigateurs maintiennent des connexions persistantes TLS. 30s tue des connexions actives lors de lecture de page. 120s est le standard proxy HTTP.
- **Crash recovery proxy** : Au démarrage du service, avant toute modification, vérifier si le registre WinINET pointe vers `127.0.0.1:50113`. Si oui et que Le Voile n'était pas en cours d'exécution → proxy orphelin d'un crash précédent → restaurer depuis le fichier de sauvegarde persisté sur disque.
- **Per-IP limiting** : `sync.Map[string]*ipState` où `ipState` contient un compteur atomique de connexions actives. Goroutine de cleanup toutes les 60s supprime les entrées avec compteur à 0 depuis > 5min. Limite = 20 connexions concurrentes par IP source.
- **Plain HTTP** : Le proxy ne gère que CONNECT (HTTPS). Les requêtes GET/POST HTTP plain sont rejetées avec HTTP 405 et un message explicite : "Only HTTPS (CONNECT) is supported". Documenter dans la spec que seul le trafic HTTPS est proxifié.
- **Honnêteté complexité** : L'intégration dans `service.go` (7 sous-actions, mutex, lifecycle, kill switch) est une intégration profonde dans le fichier le plus critique du codebase (14+ composants, 15+ stages, 7 mutex). Documenter un plan de rollback.

## Implementation Plan

### Tasks

Les tâches sont réorganisées pour testabilité précoce : le spike valide d'abord la faisabilité, puis une vertical slice (relay handler + client handler) permet un test d'intégration minimal avant d'élargir.

- [ ] **Task 0 : SPIKE — Valider le streaming bidirectionnel HTTP/3 avec quic-go (BLOQUANTE)**
  - File: `cmd/spike-h3-streaming/main.go` (NOUVEAU, temporaire — ne sera pas mergé)
  - Action: Créer un PoC minimal qui :
    1. Démarre un serveur HTTP/3 avec quic-go qui accepte des requêtes POST sur `/tunnel`
    2. Côté serveur : lit le body de la requête comme un flux (`io.Reader`), ouvre une connexion TCP vers un serveur echo local, relaye bidirectionnellement entre le body/response HTTP/3 et la connexion TCP
    3. Côté client : crée une requête POST vers `/tunnel` via `http3.Transport`, écrit dans le body via `io.Pipe()`, lit la réponse comme un flux (`resp.Body` sans `io.ReadAll`)
    4. Vérifie que les données circulent dans les deux sens en temps réel (pas de buffering complet)
  - Critère de succès : Les données circulent bidirectionnellement en streaming sur un seul stream QUIC HTTP/3. Si `quic-go` ne supporte pas ce mode, documenter l'alternative (ex: raw QUIC streams, WebSocket over HTTP/3, ou fallback HTTP/2).
  - Notes: **AUCUNE autre tâche ne commence avant que ce spike passe.** Si le spike échoue, l'architecture entière doit être repensée. Durée estimée : 2-4h.

- [ ] **Task 1 : Configuration — Ajouter `HTTPProxyConfig` + port 50113**
  - File: `internal/config/config.go`
  - Action: Ajouter la struct `HTTPProxyConfig` avec champs `Enabled bool`, `Port int` (défaut 50113). Ajouter le champ `HTTPProxy HTTPProxyConfig` à la struct `Config`.
  - File: `installer/config-default.toml`
  - Action: Ajouter la section `[http_proxy]` avec `enabled = false` et `port = 50113`.
  - Notes: Suivre le pattern exact de `BlocklistConfig`. Port 50113 au lieu de 8080 pour éviter les conflits avec les serveurs de développement courants.

- [ ] **Task 2 : Messages IPC — Ajouter les constantes proxy HTTP**
  - File: `internal/ipc/messages.go`
  - Action: Ajouter `ActionSetHTTPProxy = "set_http_proxy"` aux constantes d'action. Ajouter `HTTPProxyEnabled bool` au struct `Response`.
  - Notes: Pattern identique à `ActionSetBlocklist` / `BlocklistEnabled`.

- [ ] **Task 3 : Auth — Token de session dans verifyRelay (client + relay)**
  - File: `internal/relay/verify_handler.go`
  - Action: Étendre la réponse `VerifyResponse` pour inclure un `SessionToken string`. Après signature du nonce, générer un token de session : JSON `{"ip_hash": sha256(client_ip), "issued": unix_timestamp, "ttl": 86400}` → signer avec Ed25519 private key → base64-encode. Retourner dans `session_token` du JSON de réponse.
  - File: `internal/tunnel/client.go`
  - Action: Étendre `verifyRelay()` pour extraire et stocker le `sessionToken` reçu. Ajouter un champ `sessionToken string` protégé par le `sync.RWMutex` existant. Ajouter un getter `SessionToken() string`.
  - Notes: Le token contient un hash de l'IP client (pas l'IP en clair) pour que le relay puisse vérifier que le token est utilisé par le bon client. TTL de 24h — le client re-vérifie automatiquement si le token expire (le tunnel se reconnecte déjà périodiquement).

- [ ] **Task 4 : Handler relay — Forward proxy CONNECT côté serveur (avec auth + TOCTOU fix + per-IP limiting)**
  - File: `internal/relay/connect_handler.go` (NOUVEAU)
  - Action: Créer un handler HTTP qui :
    1. Accepte uniquement les requêtes POST (pas CONNECT — le CONNECT est géré côté client local, le relay reçoit un POST avec la cible dans le body)
    2. **Authentification** : Extraire le header `Authorization: Bearer <token>`, décoder le token, vérifier la signature Ed25519 avec la clé relay, vérifier l'expiration (TTL), vérifier que `ip_hash == sha256(request_remote_addr)`. Rejeter avec HTTP 401 si invalide.
    3. **Extraire la cible** : Lire le body JSON `{"target": "host:port"}` — la cible n'est PAS dans un header (invisible aux logs CDN)
    4. **Valider la destination** : Rejeter loopback `127.0.0.0/8`, réseaux privés `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, link-local `169.254.0.0/16`, IPv6 loopback `::1`, IPv6 link-local `fe80::/10`
    5. **Résoudre le hostname** via DNS → obtenir `net.IP`
    6. **Valider l'IP résolue** (même checks anti-SSRF — empêcher le DNS rebinding). **TOCTOU fix** : utiliser l'IP résolue directement pour le dial, ne PAS re-résoudre le hostname. Pattern : `net.DialTCP("tcp", nil, &net.TCPAddr{IP: resolvedIP, Port: port})` — identique au pattern STUN dans `stun_handler.go:174`
    7. Répondre `200 OK` avec headers de streaming
    8. Relayer bidirectionnellement (`io.Copy` dans deux goroutines) entre le body/response HTTP/3 et la connexion TCP destination
    9. Fermer proprement à la fin du transfert ou sur timeout idle (**120s** sans activité)
  - File: `internal/relay/ip_limiter.go` (NOUVEAU)
  - Action: Créer un `IPLimiter` struct :
    - Structure : `sync.Map` mapping `string` (IP) → `*ipState` où `ipState` contient `active atomic.Int64` et `lastSeen atomic.Int64` (unix timestamp)
    - `Acquire(ip string) bool` : incrémente le compteur, retourne false si > 20
    - `Release(ip string)` : décrémente le compteur, met à jour `lastSeen`
    - `StartCleanup(ctx context.Context)` : goroutine qui toutes les 60s supprime les entrées avec `active == 0` et `lastSeen` > 5min
    - Sémantique : connexions **concurrentes** par IP, pas par fenêtre temporelle
  - Notes: Le handler NE fait PAS de hijack HTTP (contrairement à un proxy CONNECT classique) — il utilise le streaming HTTP/3 body pour le tunnel. Le `IPLimiter` est distinct du `Limiter` global existant — les deux s'appliquent (global ET per-IP).

- [ ] **Task 5 : Enregistrement du handler relay + vertical slice test**
  - File: `internal/relay/server.go`
  - Action: Ajouter le champ `ConnectHandler http.Handler` au struct `Server`. Enregistrer conditionnellement : `if s.ConnectHandler != nil { mux.Handle("/connect", LimitMiddleware(s.Limiter, s.ConnectHandler)) }`. Passer `SigningKey` et `IPLimiter` au handler.
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
  - Notes: Ce test + Task 6 forment la **vertical slice** — testable de bout en bout dès que ces deux tâches sont complètes.

- [ ] **Task 6 : Proxy HTTP local côté client — Serveur + Handler CONNECT (vertical slice)**
  - File: `internal/httpproxy/server.go` (NOUVEAU)
  - Action: Créer un struct `Server` avec :
    - `listenAddr string` (défaut `127.0.0.1:50113`)
    - `tunnelClient *tunnel.Client` (pour envoyer les requêtes via le tunnel)
    - `ready chan struct{}` (signal de disponibilité)
    - Méthode `Start(ctx context.Context) error` — bloque jusqu'à annulation du context
    - Méthode `Ready() <-chan struct{}` — retourne le channel de disponibilité
    - Écoute TCP sur `listenAddr`, accepte les connexions, les dispatche au handler CONNECT
    - **Vérification port disponible** : tenter `net.Listen` et si `bind: address already in use`, retourner une erreur explicite : `"port %d already in use — configure a different port in [http_proxy] section"`
  - File: `internal/httpproxy/connect_handler.go` (NOUVEAU)
  - Action: Implémenter le handler qui :
    1. Accepte les requêtes **CONNECT** du navigateur (parse host:port)
    2. **Rejette les requêtes non-CONNECT** (GET, POST plain HTTP) avec HTTP 405 et message : `"Only HTTPS (CONNECT) is supported by this proxy"`
    3. Construit une requête **POST** vers `https://{relayDomain}/connect` via le `httpClient` HTTP/3 du tunnel
    4. Attache le header `Authorization: Bearer <sessionToken>` (obtenu via `tunnelClient.SessionToken()`)
    5. **Passe la cible dans le body JSON** : `{"target": "host:port"}` — PAS dans un header (protection metadata Cloudflare)
    6. Reçoit la réponse 200 du relay
    7. Répond `200 Connection Established` au navigateur
    8. Hijack la connexion TCP locale et relaye bidirectionnellement entre la connexion locale et le stream QUIC HTTP/3 (body request/response)
  - File: `internal/httpproxy/server_test.go` (NOUVEAU)
  - Action: Tests unitaires :
    - Démarrage/arrêt lifecycle (Start/Ready/cancel)
    - Écoute sur le bon port
    - Refus des connexions depuis une interface non-loopback
    - Port déjà occupé → erreur explicite
  - File: `internal/httpproxy/connect_handler_test.go` (NOUVEAU)
  - Action: Tests unitaires :
    - Requête CONNECT valide → tunnel via mock
    - Requête GET plain HTTP → 405
    - Token de session absent → erreur propagée
  - Notes: Avec Task 5 complète, on peut faire un **test d'intégration minimal** : proxy local → tunnel HTTP/3 → relay handler → destination. Pattern lifecycle identique à `dns/proxy.go`. Écouter uniquement sur `127.0.0.1` — jamais sur `0.0.0.0`.

- [ ] **Task 7 : Proxy système Windows — WinINET registre + crash recovery**
  - File: `internal/httpproxy/sysproxy_windows.go` (NOUVEAU)
  - Action: Créer un manager qui :
    1. `Save()` — lit les valeurs actuelles du registre WinINET (`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`) : clés `ProxyEnable` (DWORD), `ProxyServer` (string), `ProxyOverride` (string). **Persiste** ces valeurs dans un fichier JSON `{config_dir}/proxy-original.json` (survit aux crashes).
    2. `Set(addr string)` — écrit dans le registre : `ProxyEnable=1`, `ProxyServer="127.0.0.1:50113"`, `ProxyOverride="localhost;127.0.0.1;*.local;<local>"`. Appelle `SendMessageTimeout(HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Internet Settings")` pour notifier les navigateurs immédiatement (sans redémarrage).
    3. `Restore()` — lit `proxy-original.json`, restaure les valeurs registre, supprime le fichier JSON, broadcast `WM_SETTINGCHANGE`.
    4. `RecoverOrphan()` — **appelé au démarrage du service** : si `proxy-original.json` existe ET le registre pointe vers `127.0.0.1:50113` → crash précédent détecté → appeler `Restore()` automatiquement. Si le fichier existe mais le registre ne pointe pas vers notre proxy → l'utilisateur a changé manuellement → supprimer le fichier sans modifier le registre.
    5. Rollback automatique si `Set()` échoue partiellement (ex: écriture `ProxyEnable` réussit mais `ProxyServer` échoue → restaurer `ProxyEnable`).
  - File: `internal/httpproxy/sysproxy_stub.go` (NOUVEAU — build tag `!windows`)
  - Action: Stub no-op pour compilation cross-platform. Toutes les méthodes retournent `nil`.
  - Notes: Utiliser `golang.org/x/sys/windows/registry` pour l'accès registre (déjà disponible en dépendance transitive via kardianos/service). Le broadcast `WM_SETTINGCHANGE` est critique — sans lui, les navigateurs ne détectent pas le changement de proxy avant redémarrage. L'élévation admin est déjà gérée par le service système.

- [ ] **Task 8 : Intégration service lifecycle (intégration profonde — plan de rollback requis)**
  - File: `internal/service/service.go`
  - Action:
    1. Ajouter les champs : `httpProxyMu sync.Mutex`, `httpProxyCancel context.CancelFunc`, `httpProxyErrCh chan error`, `httpProxy *httpproxy.Server`, `sysProxy *httpproxy.SysProxy`
    2. **Au tout début de `run()`** (avant Stage 0) : appeler `sysProxy.RecoverOrphan()` pour détecter et restaurer un proxy orphelin d'un crash précédent
    3. Créer `startHTTPProxy(ctx context.Context) error` — démarrer le serveur proxy + configurer proxy système WinINET
    4. Créer `stopHTTPProxy()` — restaurer proxy système WinINET + arrêter le serveur
    5. Insérer `startHTTPProxy` après le démarrage du proxy DNS (Stage 2) et avant le watchdog (Stage 4)
    6. Ajouter un **defer emergency proxy restore** (pattern identique au defer DNS lignes 469-477) : `proxyRestored := false; defer func() { if !proxyRestored && p.sysProxy != nil { p.sysProxy.Restore() } }()`
    7. Insérer `stopHTTPProxy` dans le shutdown, avant la restauration DNS. Setter `proxyRestored = true` après restauration réussie.
    8. Intégrer aux callbacks kill switch : ajouter `stopHTTPProxy`/`startHTTPProxy` aux fonctions `DisableFunc`/`ReconnectFunc`
    9. Ajouter méthodes `EnableHTTPProxy()`/`DisableHTTPProxy()` pour le toggle runtime via IPC
  - Notes: **Complexité reconnue** : cette tâche modifie le fichier le plus critique du codebase (14+ composants, 7 mutex, 15+ stages). **Plan de rollback** : si l'intégration déstabilise le service lifecycle, les changements dans `service.go` peuvent être revertés indépendamment des Tasks 4-7 (le proxy et le relay handler sont autonomes). Le proxy HTTP ne démarre que si `config.HTTPProxy.Enabled == true`. Le kill switch doit couper le proxy en cas de déconnexion tunnel pour éviter les fuites IP.

- [ ] **Task 9 : Handler IPC — Toggle proxy HTTP**
  - File: `internal/ipchandler/handler.go`
  - Action:
    1. Ajouter le case `ipc.ActionSetHTTPProxy` dans le switch dispatch de `Handle()`
    2. Implémenter `handleSetHTTPProxy(prg, req)` qui : valide `req.Value` ("true"/"false"), charge la config, modifie `HTTPProxy.Enabled`, sauvegarde atomiquement, appelle `prg.EnableHTTPProxy()` ou `prg.DisableHTTPProxy()`
    3. Ajouter `HTTPProxyEnabled` à la réponse de `handleGetStatus()`
  - Notes: Copier exactement le pattern de `handleSetBlocklist()`.

- [ ] **Task 10 : Tests d'intégration + tests proxy système**
  - File: `internal/httpproxy/sysproxy_windows_test.go` (NOUVEAU)
  - Action: Tests pour save/set/restore/recoverOrphan du proxy système via registre (mockable via interface `registryAccessor` pour les opérations registre, comme `commandRunner` pour netsh dans `dns/manager_windows.go`)
  - File: `internal/relay/ip_limiter_test.go` (NOUVEAU)
  - Action: Tests pour le IPLimiter : acquire/release, limite dépassée, cleanup après TTL
  - File: Tests d'intégration dans `internal/httpproxy/integration_test.go` ou extension de `relay/e2e_test.go`
  - Action: Test end-to-end : proxy local → tunnel HTTP/3 → relay handler → serveur echo HTTP destination. Vérifier que les données circulent bidirectionnellement. Vérifier que le kill switch coupe le proxy. Vérifier l'auth token (rejet sans token, acceptation avec token valide).
  - Notes: Suivre le pattern table-driven existant. Utiliser `freeTCPAddr(t)` helper pour les ports libres.

### Acceptance Criteria

- [ ] **AC 0** : Given le spike HTTP/3 streaming (`cmd/spike-h3-streaming`), when le client envoie des données dans le body d'une requête POST et lit la réponse en streaming, then les données circulent bidirectionnellement en temps réel sur un seul stream QUIC HTTP/3 sans buffering complet.
- [ ] **AC 1** : Given le service est démarré avec `http_proxy.enabled = true`, when le proxy HTTP démarre, then il écoute sur `127.0.0.1:50113` et le channel `Ready()` est fermé.
- [ ] **AC 1b** : Given le port 50113 est déjà occupé par un autre processus, when le proxy HTTP tente de démarrer, then il retourne une erreur explicite mentionnant le port et la section de config `[http_proxy]`.
- [ ] **AC 2** : Given le proxy local écoute, when un navigateur envoie une requête CONNECT vers `example.com:443`, then la requête est tunnelisée via POST HTTP/3 vers le relay `/connect` avec la cible dans le body JSON (pas dans un header), le relay ouvre une connexion TCP vers `example.com:443`, et le trafic est relayé bidirectionnellement en streaming.
- [ ] **AC 3** : Given le proxy est actif et le proxy système WinINET est configuré, when un navigateur (Chrome/Edge/Firefox) accède à `ifconfig.me`, then l'IP affichée est celle du relay islandais (`levoile.dev`), pas celle du client français.
- [ ] **AC 4** : Given le proxy local reçoit une requête CONNECT vers `127.0.0.1:*` ou un réseau privé (`10.x`, `172.16.x`, `192.168.x`), when le relay valide la destination (y compris après résolution DNS — protection DNS rebinding), then la requête est rejetée avec HTTP 403.
- [ ] **AC 4b** : Given un hostname résolvant vers une IP privée (DNS rebinding), when le relay résout le hostname et valide l'IP résolue, then la requête est rejetée avec HTTP 403 ET le relay ne dial PAS le hostname (il utilise l'IP résolue pour le dial, pas le hostname — pas de TOCTOU).
- [ ] **AC 5** : Given le proxy est actif sur Windows, when le service démarre, then le registre WinINET (`HKCU\...\Internet Settings`) est configuré avec `ProxyEnable=1`, `ProxyServer=127.0.0.1:50113`, et `ProxyOverride` incluant localhost. Un broadcast `WM_SETTINGCHANGE` est envoyé pour notifier les navigateurs.
- [ ] **AC 5b** : Given le proxy système WinINET était configuré par Le Voile, when le service a crashé (taskkill /F, BSOD) et redémarre, then `RecoverOrphan()` détecte le proxy orphelin via le fichier `proxy-original.json` persisté sur disque, restaure les valeurs registre originales, et supprime le fichier.
- [ ] **AC 6** : Given le proxy système est configuré, when le service s'arrête proprement, then le registre WinINET est restauré à ses valeurs originales, le fichier `proxy-original.json` est supprimé, et un broadcast `WM_SETTINGCHANGE` est envoyé.
- [ ] **AC 7** : Given le tunnel se déconnecte (kill switch activé), when le kill switch coupe la connectivité, then le proxy HTTP local est arrêté et le proxy système WinINET est restauré — empêchant toute fuite d'IP.
- [ ] **AC 8** : Given un utilisateur envoie `set_http_proxy` avec `value: "true"` via IPC, when le handler traite la requête, then la config est sauvegardée et le proxy démarre au runtime.
- [ ] **AC 9** : Given un utilisateur envoie `set_http_proxy` avec `value: "false"` via IPC, when le handler traite la requête, then le proxy est arrêté, le proxy système WinINET restauré, et la config sauvegardée.
- [ ] **AC 10** : Given le relay a 20 connexions CONNECT actives depuis la même IP, when une 21ème requête CONNECT arrive de cette IP, then elle est rejetée avec HTTP 429. Les entrées IP avec 0 connexions actives sont nettoyées après 5min par la goroutine de cleanup.
- [ ] **AC 11** : Given le proxy local reçoit une requête GET `http://example.com/` (plain HTTP, pas CONNECT), when le handler traite la requête, then elle est rejetée avec HTTP 405 et un message explicite : "Only HTTPS (CONNECT) is supported by this proxy".
- [ ] **AC 12** : Given un client qui n'a pas complété `verifyRelay()` (pas de token de session), when il envoie une requête vers `/connect` sur le relay, then la requête est rejetée avec HTTP 401.
- [ ] **AC 12b** : Given un client avec un token de session expiré ou émis pour une autre IP, when il envoie une requête vers `/connect`, then la requête est rejetée avec HTTP 401.

## Additional Context

### Dependencies

- **`golang.org/x/sys/windows/registry`** — accès registre Windows pour WinINET (disponible en dépendance transitive existante)
- **Cloudflare** : Configuration manuelle préalable par l'utilisateur sur `levoile.dev` (DNS proxied)
- **Élévation Windows** : modification registre HKCU ne nécessite PAS de privilèges admin (contrairement à `netsh winhttp`). Le broadcast `WM_SETTINGCHANGE` non plus.
- **Tunnel actif** : Le proxy HTTP nécessite un tunnel QUIC connecté — il ne démarre qu'après `tunnel.Connect()` + `verifyRelay()` (pour obtenir le token de session)

### Testing Strategy

**Tests unitaires :**
- Handler relay CONNECT : auth token (valide, expiré, IP mismatch, absent), validation destination, anti-SSRF, DNS rebinding TOCTOU, méthodes HTTP, timeout 120s
- IPLimiter : acquire/release, limite 20 par IP, cleanup après 5min
- Proxy local : lifecycle Start/Stop/Ready, port occupé, connexion refusée sur interface non-loopback
- Handler CONNECT client : parsing requêtes CONNECT, rejet GET plain HTTP, construction body JSON avec cible, attachement token Authorization
- Proxy système Windows : save/set/restore/recoverOrphan via mock registre, broadcast WM_SETTINGCHANGE, rollback partiel

**Tests d'intégration :**
- **Vertical slice** (dès Tasks 5+6) : proxy local → tunnel HTTP/3 → relay handler → serveur echo destination
- Kill switch : vérifier que le proxy est coupé lors de la déconnexion tunnel
- Auth : requête sans token → 401, avec token valide → 200 + tunnel

**Tests manuels :**
- Navigateur avec proxy système WinINET → vérifier IP affichée sur `ifconfig.me` (AC 3)
- Vérifier registre `HKCU\...\Internet Settings` après start/stop/crash du service (AC 5, 5b, 6)
- Vérifier que le broadcast `WM_SETTINGCHANGE` prend effet immédiatement dans Chrome/Edge (pas besoin de redémarrer le navigateur)

### Notes

- **Risque résiduel : metadata Cloudflare** — Même avec la cible dans le body au lieu d'un header, Cloudflare termine le TLS et peut théoriquement inspecter le body des requêtes. Le body est protégé par TLS (Cloudflare → relay), mais Cloudflare elle-même y a accès. Pour une protection complète, il faudrait un chiffrement end-to-end client→relay (hors scope v1). Le risque est accepté : Cloudflare est un acteur de confiance raisonnable, et le body est moins systématiquement loggué que les headers par les CDN.
- **Risque : spike HTTP/3 échoue** — Si `quic-go` ne supporte pas le streaming body bidirectionnel, alternatives à évaluer : (a) raw QUIC streams (`quic.Connection.OpenStream()`), (b) WebSocket over HTTP/3, (c) multiplexer plusieurs request/response sur le même tunnel. Ce risque est la raison du Task 0 bloquant.
- **WinINET vs Cloudflare Workers** — Note : `HKCU` modifie le proxy pour l'utilisateur courant uniquement. Si le service tourne sous un autre compte (LocalSystem), il faut accéder au registre du bon user. Le service kardianos s'exécute sous LocalSystem — il faudra peut-être utiliser `RegOpenKeyEx` avec le SID de l'utilisateur connecté. À valider lors de l'implémentation de Task 7.
- **Futur** : Intégration tray (toggle via menu clic-droit), support macOS/Linux pour le proxy système, liste blanche de domaines à ne pas proxifier, chiffrement end-to-end de la cible (bypass Cloudflare).
- Le DNS est déjà masqué via DoH — cette feature ajoute le masquage IP par-dessus.
- Le STUN relay existe déjà pour l'anti-fuite WebRTC — le proxy CONNECT complète la protection.
- Bootstrap : le relay IP est résolu au démarrage AVANT toute redirection — pas de deadlock possible.
- **Plan de rollback** : Si l'intégration `service.go` (Task 8) déstabilise le lifecycle, les changements peuvent être revertés via `git revert` du commit Task 8 uniquement. Les Tasks 4-7 (relay handler, proxy local, proxy système) sont autonomes et testables indépendamment.
