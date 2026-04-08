---
title: 'Camouflage IP — Proxy web tunnelisé via le relay'
slug: 'ip-camouflage-web-proxy'
created: '2026-03-13'
status: 'ready-for-dev'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.22+', 'QUIC/HTTP3 (quic-go)', 'HTTP CONNECT', 'net/http', 'netsh winhttp']
files_to_modify: ['internal/config/config.go', 'installer/config-default.toml', 'internal/ipc/messages.go', 'internal/httpproxy/server.go', 'internal/httpproxy/connect_handler.go', 'internal/httpproxy/sysproxy_windows.go', 'internal/relay/connect_handler.go', 'internal/relay/server.go', 'internal/service/service.go', 'internal/ipchandler/handler.go']
code_patterns: ['Start(ctx)/Ready() channel pattern (dns/proxy.go)', 'mux.Handle + LimitMiddleware (relay/server.go)', 'switch req.Action dispatch (ipchandler/handler.go)', 'mutex-per-component lifecycle (service/service.go)', 'atomic config save temp+rename (config/config.go)']
test_patterns: ['Table-driven tests', '_test.go colocated', '_edge_test.go for edge cases', 'freeUDPAddr/freeTCPAddr helper for port allocation']
---

# Tech-Spec: Camouflage IP — Proxy web tunnelisé via le relay

**Created:** 2026-03-13

## Overview

### Problem Statement

Actuellement Le Voile masque le DNS via DoH tunnelisé mais **pas l'IP du client**. Les sites web voient l'IP française de l'utilisateur. Avec le blocage VPN imminent en France par DPI des FAI, il faut :
1. Que le trafic web passe par le relay islandais (`levoile.dev`) — IP visible = relay
2. Que le tunnel soit indiscernable du HTTPS standard — Cloudflare en frontal (config manuelle par l'utilisateur)

### Solution

Ajouter un **proxy HTTP CONNECT local** côté client (`127.0.0.1`) qui tunnelise le trafic web via le tunnel QUIC/HTTP3 existant vers un **nouveau handler forward proxy** côté relay. Configurer automatiquement le **proxy système Windows** pour router le trafic navigateur sans configuration manuelle. Cloudflare en frontal sur `levoile.dev` assure le camouflage protocolaire (trafic indiscernable du HTTPS vers un CDN).

### Scope

**In Scope:**
- Proxy local côté client (écoute sur `127.0.0.1`)
- Handler forward proxy côté relay (HTTP CONNECT)
- Configuration automatique du proxy système Windows
- Support quelques dizaines d'utilisateurs simultanés
- Trafic web léger uniquement (HTTP/HTTPS, navigation, API)

**Out of Scope:**
- Trafic lourd (streaming, gaming, téléchargement massif)
- Configuration Cloudflare (fait manuellement par Akerimus)
- Support Linux/macOS pour le proxy système (Windows d'abord)
- Interface UI dédiée pour le proxy (intégration tray ultérieure)

## Context for Development

### Codebase Patterns

- **Proxy lifecycle** : `dns/proxy.go` utilise `Start(ctx)` bloquant + `Ready()` channel + error channel pour signaler l'état — le proxy HTTP suivra ce pattern exactement
- **Handler relay** : `relay/server.go` enregistre les handlers via `mux.Handle("/path", LimitMiddleware(limiter, handler))` — le handler CONNECT suivra ce pattern
- **IPC dispatch** : `ipchandler/handler.go` utilise un `switch req.Action` avec pattern toggle identique à blocklist — le toggle proxy HTTP copiera ce pattern
- **Service lifecycle** : `service/service.go` utilise un mutex dédié par composant (`proxyMu`, `stunMu`, etc.) avec `startX(ctx)`/`stopX()` — le proxy HTTP aura son propre mutex et méthodes
- **Config** : structs TOML dans `config/config.go`, save atomique temp+rename — ajouter `HTTPProxyConfig` struct
- **DNS système Windows** : `dns/manager_windows.go` utilise `netsh` pour modifier/restaurer les resolvers — le proxy système utilisera `netsh winhttp` avec le même pattern save/restore
- **Thread safety** : mutex par composant, lock court pour accès champ, context cancellation pour shutdown gracieux

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `internal/tunnel/client.go` | Client HTTP/3 — `httpClient` réutilisable pour tunneliser les requêtes CONNECT |
| `internal/dns/proxy.go` | Pattern `Start(ctx)/Ready()` à reproduire pour le proxy HTTP local |
| `internal/relay/server.go` | Enregistrement handlers + middleware — ajouter `/connect` |
| `internal/relay/doh_handler.go` | Pattern handler relay — modèle pour `connect_handler.go` |
| `internal/relay/middleware.go` | `LimitMiddleware` — appliquer au handler CONNECT |
| `internal/service/service.go` | Lifecycle composants — intégrer proxy HTTP dans start/stop |
| `internal/ipchandler/handler.go` | Dispatch IPC — ajouter toggle proxy HTTP |
| `internal/ipc/messages.go` | Constantes actions IPC — ajouter `ActionSetHTTPProxy` |
| `internal/config/config.go` | Config TOML — ajouter `HTTPProxyConfig` |
| `internal/dns/manager_windows.go` | Pattern `netsh` save/restore — modèle pour proxy système Windows |

### Technical Decisions

- **Menace principale** : FAI français faisant du DPI pour bloquer les VPN
- **Camouflage protocolaire** : Cloudflare en frontal (config externe, pas de code)
- **Utilisateurs** : Quelques dizaines simultanés
- **Performance** : Prioritaire dans les limites du camouflage
- **Trafic** : Web léger uniquement — pas de bande passante lourde
- **Proxy local** : Écoute sur `127.0.0.1:8080` uniquement (sécurité — pas d'écoute externe)
- **CONNECT tunneling** : Requêtes HTTP CONNECT via le tunnel QUIC existant vers le relay
- **Proxy système** : `netsh winhttp set proxy` pour configuration automatique Windows
- **Kill switch** : Le proxy HTTP est intégré aux callbacks pause/resume du kill switch
- **SSRF protection** : Valider les destinations CONNECT côté relay (bloquer loopback, RFC 1918, link-local)
- **Pas de restructuration** : Extension uniquement — aucun code existant modifié en profondeur

## Implementation Plan

### Tasks

- [ ] **Task 1 : Configuration — Ajouter `HTTPProxyConfig` au système de config**
  - File: `internal/config/config.go`
  - Action: Ajouter la struct `HTTPProxyConfig` avec champs `Enabled bool`, `Port int` (défaut 8080). Ajouter le champ `HTTPProxy HTTPProxyConfig` à la struct `Config`.
  - File: `installer/config-default.toml`
  - Action: Ajouter la section `[http_proxy]` avec `enabled = false` et `port = 8080`.
  - Notes: Suivre le pattern exact de `BlocklistConfig`. Le port est configurable pour éviter les conflits.

- [ ] **Task 2 : Messages IPC — Ajouter les constantes proxy HTTP**
  - File: `internal/ipc/messages.go`
  - Action: Ajouter `ActionSetHTTPProxy = "set_http_proxy"` aux constantes d'action. Ajouter `HTTPProxyEnabled bool` au struct `Response`.
  - Notes: Pattern identique à `ActionSetBlocklist` / `BlocklistEnabled`.

- [ ] **Task 3 : Handler relay — Forward proxy CONNECT côté serveur**
  - File: `internal/relay/connect_handler.go` (NOUVEAU)
  - Action: Créer un handler HTTP qui :
    1. Accepte uniquement les requêtes `CONNECT`
    2. Valide l'adresse de destination (rejeter loopback `127.0.0.0/8`, réseaux privés `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, link-local `169.254.0.0/16`, IPv6 loopback `::1`)
    3. Résout le hostname de destination via DNS
    4. Valide l'IP résolue (même checks anti-SSRF — empêcher le DNS rebinding)
    5. Ouvre une connexion TCP vers la destination (`net.DialTimeout` avec timeout 10s)
    6. Répond `200 Connection Established` au client
    7. Hijack la connexion HTTP et relaye bidirectionnellement (`io.Copy` dans deux goroutines)
    8. Ferme proprement à la fin du transfert ou sur timeout idle (30s)
  - Notes: Pattern similaire à `doh_handler.go` pour la structure. Le hijack est nécessaire car HTTP CONNECT transforme la connexion en tunnel TCP brut. Limiter les connexions simultanées par IP source (anti-abus, max 20 par IP).

- [ ] **Task 4 : Enregistrement du handler relay**
  - File: `internal/relay/server.go`
  - Action: Enregistrer le handler CONNECT dans le `ServeMux` : `mux.Handle("/connect", LimitMiddleware(limiter, connectHandler))`. Passer la configuration nécessaire (timeouts, limites).
  - Notes: Le handler doit être protégé par `LimitMiddleware` pour respecter la limite de connexions globale du relay.

- [ ] **Task 5 : Proxy HTTP local côté client — Serveur**
  - File: `internal/httpproxy/server.go` (NOUVEAU)
  - Action: Créer un struct `Server` avec :
    - `listenAddr string` (défaut `127.0.0.1:8080`)
    - `tunnelClient *tunnel.Client` (pour envoyer les requêtes via le tunnel)
    - `ready chan struct{}` (signal de disponibilité)
    - Méthode `Start(ctx context.Context) error` — bloque jusqu'à annulation du context
    - Méthode `Ready() <-chan struct{}` — retourne le channel de disponibilité
    - Écoute TCP sur `listenAddr`, accepte les connexions, les dispatche au handler CONNECT
  - Notes: Suivre exactement le pattern de `dns/proxy.go` pour le lifecycle. Écouter uniquement sur `127.0.0.1` — jamais sur `0.0.0.0`.

- [ ] **Task 6 : Proxy HTTP local côté client — Handler CONNECT**
  - File: `internal/httpproxy/connect_handler.go` (NOUVEAU)
  - Action: Implémenter le handler qui :
    1. Parse la requête CONNECT entrante (extraire host:port)
    2. Construit une requête HTTP CONNECT vers `https://{relayDomain}/connect` via le `httpClient` HTTP/3 du tunnel
    3. Passe le host:port de destination dans le header `X-Connect-Target` (comme le STUN relay utilise `X-Stun-Target`)
    4. Reçoit la réponse 200 du relay
    5. Relaye bidirectionnellement entre la connexion locale et le stream QUIC
  - Notes: Le trafic passe par : Navigateur → proxy local (TCP) → tunnel QUIC/HTTP3 → relay → destination. Le relay fait le vrai CONNECT TCP, pas le client. Le header custom `X-Connect-Target` suit le pattern de `X-Stun-Target` déjà utilisé.

- [ ] **Task 7 : Proxy système Windows — Configuration automatique**
  - File: `internal/httpproxy/sysproxy_windows.go` (NOUVEAU)
  - Action: Créer un manager qui :
    1. `Save()` — sauvegarde le proxy système actuel via `netsh winhttp show proxy`
    2. `Set(addr string)` — configure le proxy via `netsh winhttp set proxy proxy-server="127.0.0.1:8080" bypass-list="localhost;127.0.0.1;*.local"`
    3. `Restore()` — restaure le proxy original ou reset via `netsh winhttp reset proxy`
    4. Rollback automatique si `Set()` échoue partiellement
  - File: `internal/httpproxy/sysproxy_stub.go` (NOUVEAU — build tag `!windows`)
  - Action: Stub no-op pour compilation cross-platform.
  - Notes: Suivre le pattern de `dns/manager_windows.go` pour save/restore. Le bypass-list exclut le trafic local. L'élévation admin est déjà gérée par le service système.

- [ ] **Task 8 : Intégration service lifecycle**
  - File: `internal/service/service.go`
  - Action:
    1. Ajouter les champs : `httpProxyMu sync.Mutex`, `httpProxyCancel context.CancelFunc`, `httpProxyErrCh chan error`, `httpProxy *httpproxy.Server`, `sysProxy *httpproxy.SysProxy`
    2. Créer `startHTTPProxy(ctx context.Context) error` — démarrer le serveur proxy + configurer proxy système
    3. Créer `stopHTTPProxy()` — restaurer proxy système + arrêter le serveur
    4. Insérer `startHTTPProxy` après le démarrage du proxy DNS (step 2) et avant le watchdog (step 4)
    5. Insérer `stopHTTPProxy` dans le shutdown, avant la restauration DNS
    6. Intégrer aux callbacks kill switch : ajouter `stopHTTPProxy`/`startHTTPProxy` aux fonctions `DisableFunc`/`ReconnectFunc`
    7. Ajouter méthodes `EnableHTTPProxy()`/`DisableHTTPProxy()` pour le toggle runtime via IPC
  - Notes: Le proxy HTTP ne démarre que si `config.HTTPProxy.Enabled == true`. Le kill switch doit couper le proxy en cas de déconnexion tunnel pour éviter les fuites.

- [ ] **Task 9 : Handler IPC — Toggle proxy HTTP**
  - File: `internal/ipchandler/handler.go`
  - Action:
    1. Ajouter le case `ipc.ActionSetHTTPProxy` dans le switch dispatch de `Handle()`
    2. Implémenter `handleSetHTTPProxy(prg, req)` qui : valide `req.Value` ("true"/"false"), charge la config, modifie `HTTPProxy.Enabled`, sauvegarde atomiquement, appelle `prg.EnableHTTPProxy()` ou `prg.DisableHTTPProxy()`
    3. Ajouter `HTTPProxyEnabled` à la réponse de `handleGetStatus()`
  - Notes: Copier exactement le pattern de `handleSetBlocklist()`.

- [ ] **Task 10 : Tests unitaires**
  - File: `internal/httpproxy/server_test.go` (NOUVEAU)
  - Action: Tests pour le serveur proxy local : démarrage/arrêt, écoute sur le bon port, refus des connexions externes
  - File: `internal/httpproxy/connect_handler_test.go` (NOUVEAU)
  - Action: Tests pour le handler CONNECT : requêtes valides, destinations interdites (loopback, privées), timeout
  - File: `internal/relay/connect_handler_test.go` (NOUVEAU)
  - Action: Tests pour le handler relay : CONNECT valide, validation SSRF, méthodes non-CONNECT rejetées, timeout
  - File: `internal/httpproxy/sysproxy_windows_test.go` (NOUVEAU)
  - Action: Tests pour save/set/restore du proxy système (mockable via interface `commandRunner` comme `dns/manager_windows.go`)
  - Notes: Suivre le pattern table-driven existant. Utiliser `freeTCPAddr(t)` helper pour les ports libres.

### Acceptance Criteria

- [ ] **AC 1** : Given le service est démarré avec `http_proxy.enabled = true`, when le proxy HTTP démarre, then il écoute sur `127.0.0.1:8080` et le channel `Ready()` est fermé.
- [ ] **AC 2** : Given le proxy local écoute, when un navigateur envoie une requête CONNECT vers `example.com:443`, then la requête est tunnelisée via QUIC/HTTP3 vers le relay, le relay ouvre une connexion TCP vers `example.com:443`, et le trafic est relayé bidirectionnellement.
- [ ] **AC 3** : Given le proxy est actif, when un site web vérifie l'IP du visiteur (ex: `ifconfig.me`), then l'IP affichée est celle du relay islandais (`levoile.dev`), pas celle du client français.
- [ ] **AC 4** : Given le proxy local reçoit une requête CONNECT vers `127.0.0.1:*` ou un réseau privé (`10.x`, `172.16.x`, `192.168.x`), when le relay valide la destination, then la requête est rejetée avec HTTP 403.
- [ ] **AC 5** : Given le proxy est actif sur Windows, when le service démarre, then le proxy système est configuré automatiquement via `netsh winhttp set proxy` avec bypass pour localhost.
- [ ] **AC 6** : Given le proxy système est configuré, when le service s'arrête ou crash, then le proxy système est restauré à sa valeur originale (ou reset).
- [ ] **AC 7** : Given le tunnel se déconnecte (kill switch activé), when le kill switch coupe la connectivité, then le proxy HTTP local est arrêté et le proxy système est restauré — empêchant toute fuite d'IP.
- [ ] **AC 8** : Given un utilisateur envoie `set_http_proxy` avec `value: "true"` via IPC, when le handler traite la requête, then la config est sauvegardée et le proxy démarre au runtime.
- [ ] **AC 9** : Given un utilisateur envoie `set_http_proxy` avec `value: "false"` via IPC, when le handler traite la requête, then le proxy est arrêté, le proxy système restauré, et la config sauvegardée.
- [ ] **AC 10** : Given le relay a 20+ connexions CONNECT actives depuis la même IP, when une nouvelle requête CONNECT arrive de cette IP, then elle est rejetée avec HTTP 429.

## Additional Context

### Dependencies

- **Aucune nouvelle dépendance externe** — tout repose sur `net/http`, `net`, `io`, `quic-go` (déjà présent)
- **Cloudflare** : Configuration manuelle préalable par l'utilisateur sur `levoile.dev` (DNS proxied)
- **Élévation Windows** : `netsh winhttp` nécessite des privilèges admin (déjà géré par le service système kardianos/service)
- **Tunnel actif** : Le proxy HTTP nécessite un tunnel QUIC connecté — il ne démarre qu'après `tunnel.Connect()`

### Testing Strategy

**Tests unitaires :**
- Handler relay CONNECT : validation destination, anti-SSRF, méthodes HTTP, timeout
- Proxy local : lifecycle Start/Stop/Ready, connexion refusée sur interface externe
- Handler CONNECT client : parsing requêtes, construction header `X-Connect-Target`, relay bidirectionnel
- Proxy système Windows : save/set/restore mockés via `commandRunner`

**Tests d'intégration :**
- Proxy local → tunnel → relay → destination en boucle complète (utiliser le serveur HTTP3 de test existant dans `relay/e2e_test.go`)
- Kill switch : vérifier que le proxy est coupé lors de la déconnexion tunnel

**Tests manuels :**
- Navigateur configuré avec proxy `127.0.0.1:8080` → vérifier IP affichée sur `ifconfig.me`
- Vérifier que `netsh winhttp show proxy` est correct après start/stop/crash du service

### Notes

- **Risque : HTTP/3 CONNECT streaming** — Le tunnel QUIC/HTTP3 est orienté requête/réponse. Pour le relay bidirectionnel CONNECT, il faut streamer le body de la réponse HTTP/3 comme un flux continu. Vérifier que `quic-go` supporte le streaming HTTP body (probable via `io.ReadCloser`).
- **Risque : Timeout idle** — Les connexions CONNECT persistantes (keepalive navigateur) pourraient rester ouvertes longtemps. Implémenter un idle timeout de 30s sans activité pour libérer les ressources relay.
- **Futur** : Intégration tray (toggle via menu clic-droit), support macOS/Linux pour le proxy système, liste blanche de domaines à ne pas proxifier.
- Le DNS est déjà masqué via DoH — cette feature ajoute le masquage IP par-dessus.
- Le STUN relay existe déjà pour l'anti-fuite WebRTC — le proxy CONNECT complète la protection.
- Bootstrap : le relay IP est résolu au démarrage AVANT toute redirection — pas de deadlock possible.
