---
title: 'Corrections review adversariale — tech-spec IP Camouflage'
slug: 'fix-ip-camouflage-adversarial'
created: '2026-03-13'
status: 'ready-for-dev'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'QUIC/HTTP3 (quic-go)', 'WinINET registry (HKCU)', 'Ed25519 (internal/crypto)', 'sync.Map + CompareAndDelete (Go 1.20+)', 'Cloudflare CF-Connecting-IP', 'kardianos/service (LocalSystem)', 'IPC named pipe (\\.\pipe\levoile)', 'systray (internal/tray)', 'TOML config (BurntSushi/toml)', 'DPAPI (CryptProtectData/CryptUnprotectData)']
files_to_modify: ['_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md']
code_patterns: ['startProxy/stopProxy pattern: mutex + context + errCh + Ready() channel (service.go:870-951)', 'Kill switch DisableFunc/ReconnectFunc (service.go:486-495)', 'Shutdown séquentiel inverse (service.go:641-716)', 'IPC request-response polling 2s — pas de push (tray/tray.go:433)', 'Tray gère HKCU directement (tourne sous utilisateur, pas LocalSystem)', 'verifyRelay: nonce→sign→verify, pas de token session (client.go:305-355)', 'Limiter lock-free atomic.Int64 avec Acquire/Release (relay/limiter.go)', 'Handler registration: mux.Handle + LimitMiddleware (relay/server.go:50-60)', 'Config atomic save: temp file → rename (config/config.go:98-124)', 'Config dir Windows: %AppData%\LeVoile\ (config/paths_windows.go)']
test_patterns: ['Table-driven tests avec assertions directes (pas testify)', '_test.go colocated dans même package', 'sync.WaitGroup pour tests concurrence', 'freeUDPAddr(t)/freeTCPAddr(t) helpers pour ports éphémères', 'testing.T.TempDir() pour isolation', 'HTTP/3 client avec InsecureSkipVerify pour tests']
---

# Tech-Spec: Corrections review adversariale — tech-spec IP Camouflage

**Created:** 2026-03-13

## Overview

### Problem Statement

La spec `tech-spec-ip-camouflage-web-proxy.md` a été soumise à une review adversariale qui a identifié 12 défauts, dont des bloqueurs architecturaux : le service LocalSystem ne peut pas accéder au HKCU de l'utilisateur connecté (Task 7 et 8 morts), le binding IP du token de session est cassé derrière Cloudflare (schéma d'auth inutilisable en production), le relay domain manque dans ProxyOverride (deadlock potentiel), et plusieurs sous-spécifications critiques (race condition IPLimiter, pas de connection draining, idle timeout non défini, partial failure non gérée).

### Solution

Corriger la spec existante sur les 12 points, avec les décisions techniques suivantes : déplacer la configuration proxy WinINET dans le client tray (résout LocalSystem + multi-utilisateurs), trust `CF-Connecting-IP` avec whitelist IP Cloudflare (résout binding IP), et fixes ciblés pour chaque finding restant.

### Scope

**In Scope:**
1. **BLOQUEUR** — Déplacer config proxy WinINET du service vers le tray client (findings 1 & 9)
2. **BLOQUEUR** — Fix IP hash derrière Cloudflare : trust CF-Connecting-IP depuis IPs CF uniquement (finding 2)
3. Ajouter relay domain (`levoile.dev`) à ProxyOverride (finding 3)
4. Fix race condition IPLimiter avec CAS pattern (finding 4)
5. Spike HTTP/3 : ajouter test à travers reverse proxy TLS (finding 5)
6. Corriger "7 mutex" → "6 mutex" (finding 6)
7. Connection draining 5s sur kill switch / shutdown (finding 7)
8. Token refresh : sync.Once pattern + backoff exponentiel (finding 8)
9. Plain HTTP : TCP RST silencieux au lieu de 405 avec message (finding 10)
10. Idle timeout : définir comme octets transférés + timer reset wrapper (finding 11)
11. Partial failure startHTTPProxy : atomic (les deux réussissent ou aucun) (finding 12)
12. Honnêteté doc : renforcer note sur body JSON vs Cloudflare (finding 13)

**Out of Scope:**
- Réécriture complète de la spec — corrections ciblées uniquement
- Implémentation du code — spec seulement
- Chiffrement end-to-end client→relay (bypass CDN) — futur

## Context for Development

### Codebase Patterns

- **Service LocalSystem** : `cmd/client/main.go` utilise kardianos/service sans configuration explicite d'utilisateur — tourne sous LocalSystem par défaut. HKCU de LocalSystem ≠ HKCU de l'utilisateur connecté. La spec actuelle assume à tort que le service peut écrire dans le registre WinINET de l'utilisateur.
- **IPC Architecture** : Request-response synchrone via named pipe Windows (`\\.\pipe\levoile`). Le tray poll toutes les 2s via `get_status`. Pas de push du service vers le tray. Le tray tourne sous l'utilisateur connecté → accès direct à son propre HKCU. Pattern : `ipc.Client.SendContext()` → `ipchandler.Handle()` → `ipc.Response`.
- **Tray systray** : `internal/tray/tray.go` (599 lignes). Menus : toggle connexion, leak check, auto-start, blocklist, quit. Pattern toggle : `handleBlocklistToggle()` → `ActionSetBlocklist` → handler modifie config + appelle `prg.EnableBlocklist()`. Le proxy WinINET suivra le même pattern mais **côté tray** (pas côté service).
- **Pas de CF-Connecting-IP** : Le relay (`internal/relay/server.go`) reçoit les requêtes via `http.Request.RemoteAddr` standard — aucun parsing de headers CDN. Derrière Cloudflare, RemoteAddr = IP edge Cloudflare, pas le client réel.
- **Tunnel QUIC/UDP** : Le tunnel (`internal/tunnel/client.go`) utilise QUIC (UDP), résout l'IP relay au bootstrap (`NewClient()`) et la cache dans `relayIP`. WinINET proxy ne capture que HTTP/HTTPS sur TCP — le tunnel ne serait pas affecté, mais ajouter le relay domain à ProxyOverride est du defense-in-depth.
- **startProxy/stopProxy pattern** : `service.go:870-951`. Mutex-protected, context + cancel, errCh, goroutine Start(), wait Ready() signal. **Le startHTTPProxy doit suivre ce pattern exactement.**
- **Kill switch** : `service.go:486-495`. `DisableFunc` : stop STUN + stop DNS proxy. `ReconnectFunc` : start DNS proxy + enable STUN. HTTP proxy stop/start s'insère ici.
- **Shutdown séquence** : `service.go:641-716`. Ordre inverse : IPC → leak/discoverer/blocklist/reconnector → watchdog → STUN → kill switch → DNS restore → verify → DNS proxy stop → tunnel disconnect. HTTP proxy stop s'insère entre STUN stop (ligne 677) et kill switch deactivate (ligne 679).
- **IPLimiter proposé** : `sync.Map` + `atomic.Int64` — mais `sync.Map.Delete()` n'est pas atomique avec `Load()`. Go 1.26 disponible → utiliser flag CAS `markedForDeletion` dans `ipState`.
- **6 mutex dans service.go** : `mu`, `blMu`, `toggleMu`, `updateMu`, `proxyMu`, `stunMu`. Pas 7. Le 7ème (`httpProxyMu`) sera ajouté par l'implémentation.
- **Token refresh** : `verifyRelay()` dans `tunnel/client.go:305-355` — aucun mécanisme de retry interne, backoff, ou protection contre appels concurrents. Les retries sont gérés par le `Reconnector` (1s→30s backoff, max 3 avant failover).
- **Limiter global** : `relay/limiter.go` — lock-free `atomic.Int64`, max 150. API : `Acquire() bool`, `Release()`, `Current() int64`. Pattern pour per-IP IPLimiter.
- **Config** : TOML, atomic save (temp file → rename), `%AppData%\LeVoile\config.toml`. Pour persister `proxy-original.json` : même répertoire `%AppData%\LeVoile\`.
- **Emergency DNS restore** : In-memory uniquement (`originalDNS` map dans `dns/manager_windows.go`), ne survit pas aux crashes. Le proxy WinINET persiste sur disque (le tray gère sa propre persistance dans `%AppData%\LeVoile\proxy-original.json`).

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md` | Spec à corriger — document cible |
| `internal/service/service.go` | 6 mutex, kill switch (486-495), shutdown (641-716), startProxy pattern (870-951) |
| `internal/tunnel/client.go` | verifyRelay() (305-355), pas de token session, reconnector gère retries |
| `internal/relay/server.go` | Handler registration mux.Handle (50-60), LimitMiddleware, pas de CF-Connecting-IP |
| `internal/relay/verify_handler.go` | VerifyResponse struct (19-22) — seulement Signature, pas de session token |
| `internal/relay/limiter.go` | Limiter global atomic — template pour per-IP IPLimiter |
| `internal/relay/limiter_test.go` | Pattern test : assertions directes, WaitGroup concurrence |
| `internal/ipc/messages.go` | Actions IPC, Request/Response structs — à étendre pour proxy state |
| `internal/ipchandler/handler.go` | Dispatch switch (28-54), pattern handleSetBlocklist (320-348) |
| `internal/tray/tray.go` | Systray UI (599 lignes), polling 2s, handleBlocklistToggle pattern |
| `internal/config/config.go` | Config struct TOML, Load/Save atomic, pas encore de HTTPProxyConfig |
| `internal/config/paths_windows.go` | %AppData%\LeVoile\ — répertoire pour proxy-original.json |
| `cmd/client/main.go` | kardianos/service LocalSystem, IPC server registration (229-242) |
| `internal/dns/manager_windows.go` | Emergency restore in-memory (pattern à ne PAS reproduire — persister sur disque) |

### Technical Decisions

- **Proxy WinINET géré par le tray (pas le service)** : Le service LocalSystem ne peut pas accéder au HKCU de l'utilisateur connecté. Architecture révisée : (1) le service gère le proxy HTTP local (listener `127.0.0.1:50113`), (2) le service expose l'état proxy dans la réponse `get_status` (nouveau champ `HTTPProxyActive bool` + `HTTPProxyAddr string` + **`HTTPProxySeq uint64`** — sequence number monotone incrémenté à chaque changement d'état), (3) le tray détecte le changement d'état lors du polling 2s **en comparant le sequence number** et configure/restaure WinINET dans son propre HKCU. **Détection crash service** : si le pipe IPC est cassé (erreur `SendContext()`), le tray restaure WinINET immédiatement sans attendre le prochain poll — évite une fenêtre où le proxy WinINET pointe vers un listener mort. Crash recovery tray : le tray vérifie au démarrage si `proxy-original.json` existe dans `%AppData%\LeVoile\` et si le registre pointe vers `127.0.0.1:50113` → restauration automatique. Multi-utilisateurs : chaque instance tray gère son propre HKCU — résolu nativement.
- **CF-Connecting-IP avec whitelist Cloudflare** : Le relay extrait l'IP client depuis `CF-Connecting-IP` au lieu de `RemoteAddr`. Vérification que `RemoteAddr` appartient aux ranges IP Cloudflare (publiées sur `cloudflare.com/ips/`). **Si `RemoteAddr` ∉ Cloudflare IPs ET mode `!insecure` → drop TCP silencieux (pas de réponse HTTP)** — empêche le spoofing de `CF-Connecting-IP` par connexion directe au relay. Ranges embarquées en dur dans le binaire comme fallback + **refresh obligatoire toutes les 24h** (fetch `cloudflare.com/ips-v4` + `/ips-v6` → parse → valider ≥10 ranges IPv4 CIDR → remplacer atomiquement). Si fetch échoue → garder ranges embarquées. Si ranges non rafraîchies depuis >30 jours → log warning opérationnel. Le mode `insecure` ne doit être activable que par **flag CLI + variable d'environnement**, jamais par config fichier seul. Appliqué dans le connect handler et le verify handler (pour le token session ip_hash). **Note production** : le firewall du relay doit bloquer tout trafic entrant sauf les ranges Cloudflare (defense-in-depth réseau en plus de la validation applicative).
- **CAS pour IPLimiter cleanup** : La goroutine de cleanup utilise un flag `markedForDeletion atomic.Bool` dans `ipState`. Séquence : (1) cleanup lit `active == 0` et `lastSeen > 5min` → set `markedForDeletion = true`, (2) `Acquire()` vérifie `markedForDeletion` : si true, reset le flag et continue normalement — l'entrée survit. (3) Au prochain cycle cleanup, si `markedForDeletion` est toujours true → `sync.Map.Delete()` est safe car aucun `Acquire()` concurrent n'a touché l'entrée.
- **Connection draining 5s (post-kill switch)** : **Séquence critique** : (1) kill switch active le firewall Windows (bloque tout trafic sortant) — c'est le kill switch existant, (2) **ensuite** le proxy HTTP arrête d'accepter de nouvelles connexions (`listener.Close()`), (3) signale un `context.Cancel()` aux goroutines CONNECT actives, (4) attend `sync.WaitGroup` avec `time.After(5s)`, (5) force close si timeout. **Le firewall est activé AVANT le draining** — les connexions en draining sont déjà bloquées par le firewall, donc aucune fuite IP n'est possible pendant la grace period de 5s. Le draining est une courtoisie pour les réponses en vol, pas une fenêtre de vulnérabilité. Pattern : `http.Server.Shutdown(ctx)` avec deadline 5s.
- **Token refresh sync.Once + backoff** : Nouveau mécanisme dans le client : (1) avant chaque requête CONNECT, vérifier `token.IssuedAt + token.TTL - 5min < now`, (2) si expiré/proche, appel `refreshToken()` protégé par `sync.Mutex` + flag `refreshing` — les requêtes concurrentes bloquent et attendent le résultat, (3) `refreshToken()` appelle `verifyRelay()`, (4) si échec : backoff exponentiel 1s→2s→4s...→60s max, circuit-breaker après 5 échecs consécutifs (pause 60s). **Important : le circuit-breaker ne bloque que les tentatives de refresh, pas les requêtes avec un token encore valide.** Si le token actuel n'est pas expiré, l'utiliser même si le refresh a échoué. Si le circuit-breaker est ouvert ET le token est expiré → les requêtes CONNECT échouent avec erreur explicite (pas de blocage silencieux), le tunnel QUIC reste actif. Le Reconnector existant gère la reconnexion tunnel — le token refresh est un mécanisme séparé et complémentaire.
- **TCP RST silencieux pour plain HTTP** : Le proxy local ferme la connexion TCP immédiatement (`conn.Close()`) sans écrire de réponse HTTP pour les requêtes non-CONNECT. Cohérent avec la posture privacy — ne confirme pas l'existence d'un proxy à un scanner.
- **Idle timeout = octets transférés** : `idleTimeoutConn` struct wrappant `io.Reader`/`io.Writer` qui reset un `time.Timer(120s)` à chaque `Read()`/`Write()` réussi avec `n > 0`. Les TCP keepalives ne passent pas par `io.Copy` donc ne comptent pas. Timer expiré → `conn.Close()` des deux côtés. Les downloads lents avec pauses < 120s survivent car chaque chunk reçu reset le timer.
- **Atomic startHTTPProxy** : Séquence révisée pour architecture tray : (1) service démarre proxy listener → (2) service set `httpProxyActive = true` → (3) tray détecte au prochain poll → (4) tray configure WinINET. Si (1) échoue, pas de changement d'état. Si le tray ne configure pas WinINET (tray non lancé), le proxy écoute mais aucun trafic n'arrive — état safe (pas de fuite). Le rollback est géré par le tray indépendamment.
- **ProxyOverride += relay domain (depuis config)** : `ProxyOverride = "localhost;127.0.0.1;*.local;<local>;{relay_domain}"` — le domaine relay est lu depuis la config (pas hardcodé). **ADR-3** : le hostname a été préféré à l'IP résolue car (1) le Reconnector fait du failover entre relays → si l'IP change, l'ancien IP dans ProxyOverride est stale et peut causer une boucle, (2) le hostname est failover-safe (DNS re-résout), (3) un domaine dans ProxyOverride n'est pas suspect — les entreprises mettent leur domaine interne dans ProxyOverride systématiquement. Le risque fingerprint est accepté comme mineur comparé au risque de casser le failover. Defense-in-depth — le tunnel QUIC/UDP ne serait pas affecté par WinINET, mais protège contre un futur fallback TCP.
- **Spike avec reverse proxy TLS** : Ajouter au spike HTTP/3 une étape testant le streaming à travers Caddy configuré en reverse proxy TLS devant le serveur HTTP/3, pour simuler le buffering/proxying de Cloudflare.
- **Honnêteté Cloudflare renforcée** : Note explicite : "Zero protection cryptographique contre l'opérateur CDN. Le body est accessible à Cloudflare après terminaison TLS. L'avantage est uniquement defense-in-depth contre le log scraping automatisé des headers — pas une garantie architecturale."
- **Correction factuelle** : "7 mutex" → "6 mutex" partout dans la spec. Le 7ème (`httpProxyMu`) n'existe que dans l'état futur post-implémentation.

## Implementation Plan

### Tasks

Les tâches sont ordonnées par section de la spec cible. Chaque tâche décrit les modifications exactes à apporter au document `tech-spec-ip-camouflage-web-proxy.md`.

- [ ] **Task 1 : Frontmatter + tech_stack — Corrections factuelles**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action:
    1. Dans le frontmatter `tech_stack` (ligne 7) : remplacer `'netsh winhttp'` par `'WinINET registry (HKCU via tray)', 'Cloudflare CF-Connecting-IP'`
    2. Dans `files_to_modify` (ligne 8) : ajouter `'internal/tray/tray.go'`, `'internal/relay/ip_limiter.go'`, `'internal/relay/cfip.go'`. Retirer `'internal/httpproxy/sysproxy_windows.go'` du service (il migre vers le tray, voir Task 5)
    3. Dans `code_patterns` (ligne 9) : ajouter `'IPC polling proxy state (tray/tray.go)', 'CF-Connecting-IP extraction with Cloudflare IP whitelist', 'CAS flag markedForDeletion pour IPLimiter cleanup', 'idleTimeoutConn io.Reader/Writer wrapper avec time.Timer reset'`
  - Notes: Le frontmatter doit refléter les nouvelles dépendances techniques et l'architecture révisée.

- [ ] **Task 2 : Section "Codebase Patterns" — Correction netsh → WinINET + tray**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action:
    1. Ligne 53 : remplacer le pattern `netsh` par : `- **Proxy système Windows** : Le service tourne sous LocalSystem (HKCU ≠ utilisateur). La config WinINET est gérée par le tray client (qui tourne sous l'utilisateur connecté) via modification directe du registre \`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings\`. Le service signale l'état proxy au tray via le champ \`HTTPProxyActive\` dans la réponse IPC \`get_status\`. Pattern : tray poll → détecte changement → configure/restaure registre.`
    2. Ligne 51 (service lifecycle) : corriger "mutex dédié par composant" — ajouter `httpProxyMu` à la liste et préciser que le service a actuellement **6** mutex (pas 7), le 7ème ajouté par cette implémentation.
  - Notes: L'erreur "7 mutex" apparaît aussi dans les lignes 68, 83, et 214 de la spec corrigée précédente. Corriger partout.

- [ ] **Task 3 : Section "Technical Decisions" — Correction port, netsh, header**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action:
    1. Ligne 78 : remplacer `127.0.0.1:8080` par `127.0.0.1:50113` (port sans conflit)
    2. Ligne 80 : remplacer `netsh winhttp set proxy` par : `WinINET registre via le tray client — le service LocalSystem ne peut pas accéder au HKCU de l'utilisateur. Le tray (qui tourne sous l'utilisateur) configure \`HKCU\\...\\Internet Settings\` (ProxyEnable, ProxyServer, ProxyOverride) et broadcast \`WM_SETTINGCHANGE\`.`
    3. Ajouter décisions techniques manquantes :
       - **CF-Connecting-IP** : Le relay extrait l'IP client depuis `CF-Connecting-IP` (pas `RemoteAddr` qui est l'IP edge Cloudflare). Vérification que `RemoteAddr` ∈ ranges IP Cloudflare. Mode insecure/dev : `RemoteAddr` direct.
       - **Connection draining** : Grace period 5s sur kill switch et shutdown. `http.Server.Shutdown(ctx)` avec deadline 5s. Les CONNECT actifs ont 5s pour terminer, puis force close.
       - **Token refresh** : Vérification proactive du TTL avant chaque CONNECT. Refresh protégé par mutex (single-flight). Backoff exponentiel 1s→60s max. Circuit-breaker après 5 échecs (pause 60s).
       - **Idle timeout sémantique** : 120s = octets transférés dans l'une ou l'autre direction. Wrapper `idleTimeoutConn` qui reset un `time.Timer` à chaque `Read()`/`Write()` avec `n > 0`. TCP keepalives ne comptent pas.
       - **Plain HTTP** : TCP RST silencieux (`conn.Close()` sans réponse HTTP) pour les requêtes non-CONNECT. Pas de 405 avec message — ne confirme pas l'existence du proxy.
  - Notes: Ces décisions comblent les findings 2, 7, 8, 10, 11 de la review adversariale.

- [ ] **Task 4 : Task 0 (spike) — Ajouter test reverse proxy TLS**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Dans la Task 0 (spike HTTP/3, lignes 91-99 de la spec corrigée), ajouter une étape :
    ```
    5. **Étape Cloudflare simulation** : Configurer Caddy en reverse proxy TLS devant le serveur HTTP/3 de test. Vérifier que le streaming bidirectionnel survit à la terminaison TLS + re-encryption du reverse proxy. Si le buffering Caddy casse le streaming, documenter le comportement et évaluer les alternatives (chunked transfer, SSE, WebSocket upgrade).
    ```
    Modifier le critère de succès pour inclure : "Les données circulent bidirectionnellement **à travers un reverse proxy TLS** (simulation CDN)."
  - Notes: Sans ce test, le spike peut passer en direct mais échouer en production derrière Cloudflare.

- [ ] **Task 5 : Task 1 (config) — Corriger port 8080 → 50113**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action:
    1. Lignes 91-94 : remplacer `port = 8080` par `port = 50113` dans la description de `HTTPProxyConfig` et dans `config-default.toml`
    2. Ajouter dans les Notes : "Port 50113 au lieu de 8080 pour éviter les conflits avec les serveurs de développement courants (React, Spring Boot, etc.). Vérification de disponibilité au démarrage avec message d'erreur explicite."
  - Notes: Correction simple, déjà identifiée dans la spec corrigée précédente.

- [ ] **Task 6 : Task 3 (handler relay) — Auth + CF-Connecting-IP + IPLimiter CAS + idle timeout + draining**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Réécrire la Task 3 (lignes 101-112) pour intégrer :
    1. **Auth Ed25519** : Le handler extrait `Authorization: Bearer <token>` du header, vérifie la signature Ed25519, vérifie l'expiration TTL, vérifie que `ip_hash == sha256(client_real_ip)`. **Sanitization logs** : le header `Authorization` ne doit JAMAIS apparaître dans les logs relay (ni en mode debug, ni dans le panic recovery). Ajouter une règle de sanitization dans le logger et le recovery middleware : tout header `Authorization` est remplacé par `[REDACTED]`. Côté Cloudflare : configurer le Logpush pour exclure le header `Authorization` (Logpush Filter → field exclusion) — sinon le token session est visible dans les logs CDN.
    2. **CF-Connecting-IP** : `client_real_ip` est extrait depuis `CF-Connecting-IP` header (pas `RemoteAddr`). Vérification préalable : `RemoteAddr` doit appartenir aux ranges IP Cloudflare (embarquées en dur + refresh optionnel). **Si `RemoteAddr` ∉ Cloudflare IPs ET mode `!insecure` → drop TCP silencieux** (pas de réponse HTTP — empêche le spoofing CF-Connecting-IP par connexion directe). Si mode `insecure` (flag CLI + env var uniquement, jamais config fichier) : utiliser `RemoteAddr` directement. Créer `internal/relay/cfip.go` : struct `CloudflareIPValidator` avec `IsTrustedSource(remoteAddr string) bool` et `ExtractClientIP(r *http.Request) (string, error)`.
    3. **Cible dans le body** : POST avec body JSON `{"target": "host:port"}` au lieu de CONNECT avec header `X-Connect-Target`.
    4. **TOCTOU fix** : Résoudre le hostname → obtenir `net.IP` → valider anti-SSRF → `net.DialTCP("tcp", nil, &net.TCPAddr{IP: resolvedIP, Port: port})` — jamais re-résoudre le hostname.
    5. **IPLimiter avec CAS** : `internal/relay/ip_limiter.go` — struct `ipState` avec `active atomic.Int64`, `lastSeen atomic.Int64`, `markedForDeletion atomic.Bool`. `Acquire()` : si `markedForDeletion == true`, reset flag et continuer. Cleanup goroutine : si `active == 0` et `lastSeen > 5min`, set `markedForDeletion = true`. Prochain cycle : si flag toujours true → `sync.Map.Delete()` safe.
    6. **Idle timeout 120s** : Wrapper `idleTimeoutConn` sur les `io.Reader`/`io.Writer` des deux côtés du relay. Reset `time.Timer(120s)` à chaque `Read()`/`Write()` avec `n > 0`. Timer expiré → `conn.Close()` bidirectionnel.
    7. **Connection draining (relay)** : **ADR-5** : Le relay utilise le streaming body HTTP/3 (pas de `Hijack()`) → `http.Server.Shutdown(ctx)` avec deadline 5s fonctionne nativement car les connexions restent trackées par le serveur HTTP. Pas besoin de `sync.WaitGroup` custom côté relay — c'est le proxy local (Task 9/11) qui utilise `Hijack()` et nécessite un WaitGroup.
    8. Remplacer la réponse HTTP `200 Connection Established` par `200 OK` avec headers de streaming (le relay ne fait pas de hijack HTTP classique — il utilise le streaming body HTTP/3).
  - Notes: Cette task est la plus dense — elle consolide les findings 2, 4, 7, 11 dans le handler relay.

- [ ] **Task 7 : Task 4 (enregistrement relay) — Ajouter IPLimiter + cfip validator**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Modifier la Task 4 (lignes 114-117) :
    1. Ajouter le champ `IPLimiter *IPLimiter` et `CFIPValidator *CloudflareIPValidator` au struct `Server`.
    2. Passer ces deux dépendances au connect handler lors de l'enregistrement.
    3. Ajouter `StartCleanup(ctx)` call pour la goroutine de cleanup IPLimiter dans `Server.Serve()`.
    4. **ADR-4 : Refresh CF ranges via goroutine ticker** : Lancer dans `Server.Serve(ctx)` un `time.NewTicker(24h)` qui fetch `cloudflare.com/ips-v4` + `/ips-v6`, parse, valide (≥10 ranges IPv4 CIDR), et remplace atomiquement via `atomic.Pointer[[]netip.Prefix]`. Arrêt via `ctx.Done()`. Le fetch est async — aucune latence sur le hot path des requêtes. Si fetch échoue → garder ranges actuelles. Si >30 jours → log warning.

- [ ] **Task 8 : Task 5 (proxy local serveur) — Corriger port + port check**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Modifier la Task 5 (lignes 119-128) :
    1. Remplacer `127.0.0.1:8080` par `127.0.0.1:50113`
    2. Ajouter vérification port disponible : tenter `net.Listen` — si `bind: address already in use`, retourner erreur explicite : `"port %d already in use — configure a different port in [http_proxy] section"`

- [ ] **Task 9 : Task 6 (handler CONNECT client) — Body JSON + auth token + RST silencieux**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Réécrire la Task 6 (lignes 130-138) :
    1. Remplacer le header `X-Connect-Target` par body JSON `{"target": "host:port"}` dans la requête POST vers `/connect`.
    2. Ajouter header `Authorization: Bearer <sessionToken>` (obtenu via `tunnelClient.SessionToken()`).
    3. **Plain HTTP : TCP RST silencieux** — remplacer "Rejette les requêtes non-CONNECT avec HTTP 405 et message" par "Ferme la connexion TCP immédiatement (`conn.Close()`) sans écrire de réponse HTTP pour les requêtes non-CONNECT. Ne confirme pas l'existence d'un proxy."
    4. Ajouter **token refresh proactif** : avant chaque CONNECT, vérifier `token.IssuedAt + token.TTL - 5min < now`. Si expiré/proche, appeler `refreshToken()` protégé par `sync.Mutex` + flag (single-flight). Backoff exponentiel 1s→2s→4s...→60s. Circuit-breaker après 5 échecs : pause 60s. **Le circuit-breaker ne bloque que les tentatives de refresh** — si le token actuel est encore valide (non expiré), l'utiliser normalement même si le circuit-breaker est ouvert. Si circuit-breaker ouvert ET token expiré → erreur explicite vers le client local (pas de blocage silencieux).
  - Notes: Le RST silencieux (finding 10) et le token refresh (finding 8) s'intègrent dans ce handler.

- [ ] **Task 10 : Task 7 (sysproxy) — Migration service → tray**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Réécrire complètement la Task 7 (lignes 140-149). L'architecture change fondamentalement :
    1. **Supprimer** `internal/httpproxy/sysproxy_windows.go` du service.
    2. **Créer** `internal/tray/sysproxy_windows.go` (NOUVEAU) dans le tray avec :
       - `Save()` — lit les valeurs actuelles du registre WinINET (`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`) : clés `ProxyEnable` (DWORD), `ProxyServer` (string), `ProxyOverride` (string). Chiffre les données via DPAPI (`CryptProtectData`) et persiste dans `%AppData%\LeVoile\proxy-original.json` via **atomic write** (temp file → `os.Rename()`, pattern identique à `config.go:98-124`). Si crash entre write et rename → pas de fichier corrompu.
       - `Set(addr string)` — écrit dans le registre : `ProxyEnable=1`, `ProxyServer="127.0.0.1:50113"`, `ProxyOverride="localhost;127.0.0.1;*.local;<local>;{relay_domain}"` (relay domain depuis la config). Broadcast `WM_SETTINGCHANGE` via `SendMessageTimeout(HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Internet Settings")`.
       - `Restore()` — lit `proxy-original.json`, restaure les valeurs registre, supprime le fichier JSON, broadcast `WM_SETTINGCHANGE`.
       - `RecoverOrphan()` — appelé au **démarrage du tray** : si `proxy-original.json` existe ET le registre pointe vers `127.0.0.1:50113` → crash précédent détecté → appeler `Restore()`. Si le fichier existe mais le registre ne pointe pas vers notre proxy → l'utilisateur a changé manuellement → supprimer le fichier. **Validation anti-empoisonnement** : avant restauration, valider le contenu de `proxy-original.json` — (1) `ProxyEnable` ∈ {0, 1} (DWORD), (2) `ProxyServer` doit être vide ou format `host:port` avec host ∈ {vide, localhost, IP privée RFC1918, IP publique connue} — rejeter les valeurs suspectes (ex: proxy malveillant injecté), (3) vérifier l'intégrité via **DPAPI** (`CryptProtectData`/`CryptUnprotectData`) — le fichier `proxy-original.json` est chiffré avec DPAPI lors de `Save()`, qui lie les données au credential de l'utilisateur Windows courant. `RecoverOrphan()` appelle `CryptUnprotectData` : si le déchiffrement échoue (fichier corrompu, créé par un autre utilisateur, ou forgé par un malware sans accès au credential store) → supprimer sans restaurer + log warning. DPAPI est supérieur à HMAC-SID car le SID est public (lisible par tout process du même user), tandis que DPAPI utilise le master key du credential store protégé par le password utilisateur.
       - Rollback atomique si `Set()` échoue partiellement.
       - **ADR-1 : Interface `DataProtector`** pour testabilité DPAPI. Interface : `Protect(data []byte) ([]byte, error)` + `Unprotect(data []byte) ([]byte, error)`. Implémentation Windows : `dpapi_windows.go` utilise `CryptProtectData`/`CryptUnprotectData`. Implémentation test/stub : `dpapi_stub.go` (build tag `!windows`) retourne les données en clair (acceptable car le fichier n'existe que sur Windows). `SysProxy` reçoit un `DataProtector` par injection de dépendance → les tests unitaires utilisent le stub.
    3. **Créer** `internal/tray/sysproxy_stub.go` (build tag `!windows`) — stubs no-op.
    4. **Intégration tray** : dans `tray.go`, ajouter la logique de polling proxy state :
       - Dans `updateTrayState(resp)` : si `resp.HTTPProxyActive` change de `false` → `true` : appeler `sysProxy.Save()` puis `sysProxy.Set(resp.HTTPProxyAddr)`.
       - Si `resp.HTTPProxyActive` change de `true` → `false` : appeler `sysProxy.Restore()`.
       - Au démarrage tray : appeler `sysProxy.RecoverOrphan()` avant le polling loop.
    5. **ProxyOverride inclut le relay domain** : `"localhost;127.0.0.1;*.local;<local>;{relay_domain}"` — le domaine relay est lu depuis la config. **ADR-3** : hostname préféré à l'IP résolue car failover-safe (le Reconnector change de relay → DNS re-résout, pas besoin de mettre à jour ProxyOverride). Le risque fingerprint registre est mineur — les domaines dans ProxyOverride sont courants. Defense-in-depth — le tunnel QUIC/UDP ne passe pas par WinINET.
  - Notes: Ce changement architectural résout les findings 1 (LocalSystem), 3 (ProxyOverride relay domain), et 9 (multi-utilisateurs). Utiliser `golang.org/x/sys/windows/registry` pour l'accès registre. Le broadcast `WM_SETTINGCHANGE` est critique — sans lui, les navigateurs ne détectent pas le changement.

- [ ] **Task 11 : Task 8 (intégration service) — Simplification + draining + partial failure**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Réécrire la Task 8 (lignes 151-161) :
    1. **Supprimer** `sysProxy *httpproxy.SysProxy` du service — migré dans le tray (Task 10).
    2. **Supprimer** `sysProxy.RecoverOrphan()` du service — migré dans le tray.
    3. **Conserver** les champs service : `httpProxyMu sync.Mutex`, `httpProxyCancel context.CancelFunc`, `httpProxyErrCh chan error`, `httpProxy *httpproxy.Server`.
    4. `startHTTPProxy(ctx)` : suivre le pattern exact de `startProxy()` (service.go:870-927). Mutex lock → créer serveur → context+cancel → goroutine Start → wait Ready(). **Si le listener échoue** (port occupé), retourner erreur explicite. Pas de config WinINET — le tray détecte l'état via polling.
    5. `stopHTTPProxy()` : suivre le pattern exact de `stopProxy()` (service.go:930-951). **ADR-5 : Connection draining via `sync.WaitGroup` custom** — le proxy local utilise `Hijack()` pour les connexions CONNECT, donc `http.Server.Shutdown()` ne track pas ces connexions. Séquence : chaque goroutine CONNECT fait `wg.Add(1)` au hijack et `wg.Done()` à la fin. `stopHTTPProxy()` : cancel context → `select { case <-wg.Wait(): ok | case <-time.After(5s): force close }`. **Quand appelé depuis le kill switch** : le firewall Windows est activé AVANT `stopHTTPProxy()` — les connexions en draining sont déjà bloquées au niveau réseau, donc aucune fuite IP possible pendant la grace period.
    6. **Shutdown** : insérer `stopHTTPProxy()` entre STUN stop (ligne 677) et kill switch deactivate (ligne 679).
    7. **Kill switch** : ajouter `p.stopHTTPProxy()` dans `DisableFunc` (après `p.stopProxy()` ligne 488). Ajouter `p.startHTTPProxy(reconnCtx)` dans `ReconnectFunc` (après `p.startProxy()` ligne 490).
    8. **Defer emergency proxy stop** : ajouter un defer dans `run()` qui appelle `stopHTTPProxy()` si le proxy est actif. Pas de restore registre (c'est le tray qui gère) — le service arrête juste le listener.
    9. **État proxy exposé** : ajouter méthode `HTTPProxyActive() bool` et `HTTPProxyAddr() string` sur Program, utilisées par `handleGetStatus()`.
    10. Corriger "7 mutex" → "6 mutex actuellement, 7 après ajout de httpProxyMu" dans les notes Task 8.
  - Notes: L'intégration est **simplifiée** par rapport à la spec originale car le service ne touche plus au registre. Le plan de rollback reste valide : revert Task 8 si déstabilisation, Tasks 4-7 restent autonomes.

- [ ] **Task 12 : Task 9 (IPC handler) — Étendre get_status + proxy state côté tray**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Modifier la Task 9 (lignes 163-169) :
    1. Ajouter `HTTPProxyActive bool`, `HTTPProxyAddr string`, et **`HTTPProxySeq uint64`** au struct `ipc.Response` (messages.go). Le sequence number est un compteur monotone incrémenté à chaque changement d'état du proxy (start/stop). Le tray compare `seq` entre deux polls : si identique, pas de changement → pas de modification WinINET. Empêche les toggles rapides start→stop entre deux polls de laisser WinINET dans un état incohérent.
    2. Dans `handleGetStatus()` : populer `HTTPProxyActive`, `HTTPProxyAddr`, et `HTTPProxySeq` depuis `prg.HTTPProxyActive()`, `prg.HTTPProxyAddr()`, et `prg.HTTPProxySeq()`.
    3. Le handler `handleSetHTTPProxy()` reste similaire mais ne touche plus au registre — il appelle `prg.EnableHTTPProxy()` / `prg.DisableHTTPProxy()` (service gère le listener uniquement).
    4. **Côté tray** (nouveau) : dans `updateTrayState()`, détecter le changement via **comparaison du `HTTPProxySeq`** (pas juste `HTTPProxyActive`) et appeler `sysProxy.Set()`/`sysProxy.Restore()` en réaction. **Détection crash service** : si `ipc.Client.SendContext()` retourne une erreur pipe (service inaccessible), restaurer WinINET immédiatement — ne pas attendre que le service revienne. Ajouter un menu item toggle proxy dans le tray (pattern identique à blocklist toggle).
  - Notes: La Task 9 originale est étendue pour couvrir le côté tray. Le toggle fonctionne : tray envoie `set_http_proxy` → service start/stop listener → tray détecte changement → tray configure/restaure registre.

- [ ] **Task 13 : Task 3 (auth) — Ajouter session token à verifyRelay**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Ajouter ou modifier la description de la Task 3 (auth token) déjà présente dans la spec corrigée :
    1. **Côté relay** (`verify_handler.go`) : après signature du nonce, générer un session token : JSON `{"ip_hash": sha256(client_real_ip), "issued": unix_timestamp, "ttl": 14400}` → signer Ed25519 → base64. `client_real_ip` extrait via `CloudflareIPValidator.ExtractClientIP(r)`. Retourner dans `session_token` de `VerifyResponse`.
    2. **Côté client** (`tunnel/client.go`) : extraire et stocker `sessionToken` de la réponse verify. Getter `SessionToken() string`. **Token refresh** : si `issued + ttl - 300 < now`, appeler `refreshToken()` (single-flight avec mutex, backoff 1s→60s, circuit-breaker 5 échecs → pause 60s).
  - Notes: Le `ip_hash` utilise l'IP réelle du client (via CF-Connecting-IP), pas l'IP edge Cloudflare. **Clarification sécurité** : l'`ip_hash` est une couche de **defense-in-depth**, pas le mécanisme d'authentification principal. L'auth repose sur la signature Ed25519 du nonce — un attaquant avec la même IP source (NAT, Wi-Fi public) ne peut pas forger le nonce sans la clé privée du client. L'`ip_hash` empêche uniquement la réutilisation d'un token volé depuis une IP différente. Le token refresh est un mécanisme nouveau qui n'existe pas dans le codebase actuel.

- [ ] **Task 14 : Tests — Ajouter tests pour les nouveaux composants**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Modifier la Task 10 (tests, lignes 171-180) pour ajouter :
    1. `internal/relay/ip_limiter_test.go` : acquire/release, limite 20 par IP, CAS flag race (goroutines concurrentes Acquire pendant cleanup), cleanup après TTL.
    2. `internal/relay/cfip_test.go` : IsTrustedSource avec IPs Cloudflare valides/invalides, ExtractClientIP avec/sans header, mode insecure.
    3. `internal/tray/sysproxy_windows_test.go` : save/set/restore/recoverOrphan via mock `registryAccessor` interface.
    4. `internal/httpproxy/connect_handler_test.go` : ajouter tests token absent → erreur propagée, token expiré → refresh, plain HTTP → connexion fermée (pas de réponse HTTP).
    5. `internal/httpproxy/idle_timeout_test.go` : vérifier que le timer reset sur transfert, timeout après inactivité, pas de timeout pendant transfert lent continu.
    6. **Spike** : ajouter test à travers reverse proxy TLS Caddy dans les critères du spike.
    7. **Connection draining test** : vérifier que les connexions actives ont 5s grace period sur shutdown.
  - Notes: Les tests CAS IPLimiter sont critiques — ils doivent prouver qu'aucune entrée n'est perdue lors d'un Acquire concurrent au cleanup.

- [ ] **Task 15 : Acceptance Criteria — Ajouter les ACs manquants**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action: Modifier les ACs (lignes 182-193) :
    1. **AC 0** (NOUVEAU) : Given le spike HTTP/3 streaming avec Caddy en reverse proxy TLS, when le client envoie des données en streaming, then les données circulent bidirectionnellement **à travers le reverse proxy** sans buffering complet.
    2. **AC 1b** (NOUVEAU) : Given le port 50113 déjà occupé, when le proxy tente de démarrer, then erreur explicite mentionnant le port et la section config `[http_proxy]`.
    3. **AC 4b** (NOUVEAU) : Given un hostname résolvant vers IP privée (DNS rebinding), when le relay résout et valide, then rejet 403 ET le relay dial l'IP résolue (pas le hostname — TOCTOU fix).
    4. **AC 5** : Remplacer `netsh winhttp` par "le tray configure le registre WinINET (`HKCU\...\Internet Settings`) avec `ProxyEnable=1`, `ProxyServer=127.0.0.1:50113`, et `ProxyOverride` incluant localhost et le relay domain. Broadcast `WM_SETTINGCHANGE`."
    5. **AC 5b** (NOUVEAU) : Given crash du tray, when le tray redémarre, then `RecoverOrphan()` détecte le proxy orphelin via `proxy-original.json`, restaure les valeurs registre, supprime le fichier.
    6. **AC 7** : Ajouter "Les connexions CONNECT actives ont un grace period de 5s avant force close."
    7. **AC 11** : Remplacer "HTTP 405 et message" par "connexion TCP fermée silencieusement sans réponse HTTP."
    8. **AC 12** (NOUVEAU) : Given un client sans token, when requête vers `/connect`, then rejet 401.
    9. **AC 12b** (NOUVEAU) : Given un token expiré ou IP mismatch, when requête vers `/connect`, then rejet 401.
    10. **AC 13** (NOUVEAU) : Given le relay derrière Cloudflare, when le connect handler extrait l'IP client, then il utilise `CF-Connecting-IP` (pas `RemoteAddr`) et vérifie que `RemoteAddr` ∈ ranges IP Cloudflare.
    11. **AC 14** (NOUVEAU) : Given le client avec un token proche de l'expiration (< 5min TTL), when une requête CONNECT arrive, then le client refresh le token via `verifyRelay()` (single-flight) avant d'envoyer la requête. Si refresh échoue, backoff exponentiel.
  - Notes: Les ACs couvrent tous les 12 findings de la review adversariale.

- [ ] **Task 16 : Section "Notes" — Honnêteté Cloudflare + corrections**
  - File: `_bmad-output/implementation-artifacts/tech-spec-ip-camouflage-web-proxy.md`
  - Action:
    1. Remplacer la note risque "HTTP/3 CONNECT streaming" (ligne 222) par : "**Risque résiduel : metadata Cloudflare** — Zero protection cryptographique contre l'opérateur CDN. Cloudflare termine le TLS et a accès complet au body des requêtes POST vers `/connect`, y compris la cible `host:port`. Le placement de la cible dans le body (au lieu d'un header) est uniquement defense-in-depth contre le log scraping automatisé des headers par les CDN — pas une garantie architecturale. Pour une protection complète contre le CDN, il faudrait un chiffrement end-to-end client→relay (hors scope v1)."
    2. Remplacer la note "Timeout idle 30s" (ligne 223) par : "**Idle timeout 120s défini** — Activité = octets transférés (`n > 0`) dans l'une ou l'autre direction via `idleTimeoutConn` wrapper. TCP keepalives ne comptent pas. Downloads lents avec pauses < 120s survivent. 120s est le standard proxy HTTP — 30s tuait les connexions actives lors de lecture de page."
    3. Supprimer "Futur : Intégration tray" (ligne 224) — c'est maintenant dans le scope (Task 10/12).
    4. Ajouter note : "**Multi-utilisateurs** : Chaque instance tray gère son propre HKCU. Fast user switching et sessions RDP supportés nativement — chaque utilisateur connecté a son tray qui configure/restaure son propre proxy."
    5. Ajouter note : "**Plan de rollback** : Si l'intégration `service.go` (Task 11) déstabilise le lifecycle, les changements peuvent être revertés via `git revert` du commit correspondant. Les Tasks 6-10 (relay handler, proxy local, tray proxy) sont autonomes et testables indépendamment."

### Acceptance Criteria

- [ ] **AC 1** : Given la spec corrigée, when un dev agent lit la Task 3 (handler relay), then il trouve les instructions complètes pour l'extraction CF-Connecting-IP, la validation Cloudflare IP whitelist, l'auth Ed25519 token, le TOCTOU fix, le CAS IPLimiter, l'idle timeout 120s avec wrapper, et le connection draining 5s — sans ambiguïté ni placeholder.
- [ ] **AC 2** : Given la spec corrigée, when un dev agent lit la Task 7 (sysproxy), then il comprend que le code va dans le tray (pas le service), utilise le registre WinINET (pas netsh), inclut le relay domain dans ProxyOverride, et gère le crash recovery via fichier persisté + RecoverOrphan au démarrage tray.
- [ ] **AC 3** : Given la spec corrigée, when on cherche "netsh winhttp" dans le document, then aucune occurrence n'est trouvée — toutes les références sont remplacées par WinINET registre + tray.
- [ ] **AC 4** : Given la spec corrigée, when on cherche "7 mutex", then aucune occurrence incorrecte n'est trouvée — le document dit "6 mutex actuellement" avec mention du 7ème ajouté par l'implémentation.
- [ ] **AC 5** : Given la spec corrigée, when on cherche "8080" dans le document, then aucune occurrence n'est trouvée — toutes les références sont remplacées par 50113.
- [ ] **AC 6** : Given la spec corrigée, when on cherche "X-Connect-Target" dans le document, then aucune occurrence n'est trouvée — la cible est dans le body JSON.
- [ ] **AC 7** : Given la spec corrigée, when on lit la section Notes, then on trouve une note explicite sur le risque Cloudflare (zero protection crypto contre CDN), la sémantique idle timeout (octets transférés), le support multi-utilisateurs, et le plan de rollback.
- [ ] **AC 8** : Given la spec corrigée, when on compte les ACs, then chacun des 12 findings de la review adversariale est couvert par au moins un AC.
- [ ] **AC 9 (Red Team)** : Given le service qui crash pendant que le proxy WinINET est actif, when le tray détecte l'erreur pipe IPC, then le tray restaure WinINET immédiatement sans attendre le prochain poll 2s.
- [ ] **AC 10 (Red Team)** : Given un client qui se connecte directement au relay (bypass Cloudflare) avec un header `CF-Connecting-IP` spoofé, when `RemoteAddr ∉ Cloudflare IPs` et mode `!insecure`, then le relay drop la connexion TCP silencieusement.
- [ ] **AC 11 (Red Team)** : Given un `proxy-original.json` non déchiffrable par DPAPI ou avec des valeurs suspectes (ex: ProxyServer pointant vers un proxy malveillant), when `RecoverOrphan()` le lit, then le fichier est supprimé sans restauration + log warning.
- [ ] **AC 12 (Red Team)** : Given le kill switch activé avec des connexions CONNECT en cours, when le draining 5s s'exécute, then le firewall Windows est déjà actif AVANT le draining — aucune donnée ne peut fuiter pendant la grace period.
- [ ] **AC 13 (Red Team)** : Given un circuit-breaker ouvert sur le token refresh ET un token encore valide, when une requête CONNECT arrive, then le token existant est utilisé normalement — le circuit-breaker ne bloque pas les requêtes avec token valide.
- [ ] **AC 14 (ADR-3)** : Given le ProxyOverride dans le registre WinINET, when on lit la valeur, then elle contient le relay domain depuis la config (failover-safe). Le hostname est préféré à l'IP résolue pour éviter de casser le failover Reconnector.
- [ ] **AC 15 (Audit)** : Given le relay en production, when le header `Authorization` apparaît dans une requête qui cause un panic ou un log, then le header est remplacé par `[REDACTED]` dans tous les outputs.
- [ ] **AC 16 (Audit)** : Given le relay démarré depuis >24h, when les ranges CF n'ont pas été rafraîchies, then un refresh est tenté automatiquement. Si fetch échoue, les ranges embarquées sont utilisées. Si >30 jours sans refresh → warning log.
- [ ] **AC 17 (ADR-2)** : Given un token session, when on vérifie son TTL, then le TTL est de 14400s (4h) — compromis entre sécurité (fenêtre de vol limitée) et résilience (tolère des interruptions relay de plusieurs heures).
- [ ] **AC 18 (Audit)** : Given le fichier `proxy-original.json`, when il est sauvegardé par `Save()`, then il est chiffré via DPAPI et écrit atomiquement (temp file → rename). Un crash pendant l'écriture ne produit pas de fichier corrompu.

## Additional Context

### Dependencies

- **IPC named pipe existant** : Le tray et le service communiquent déjà via `\\.\pipe\levoile`. Pas de nouvelle dépendance transport.
- **`golang.org/x/sys/windows/registry`** : Pour le tray — accès registre WinINET. Disponible en dépendance transitive existante via kardianos/service.
- **Cloudflare IP ranges** : `https://cloudflare.com/ips-v4` et `/ips-v6`. Embarquées en dur dans `internal/relay/cfip.go` comme fallback + **refresh obligatoire toutes les 24h**. ~15 ranges IPv4, ~6 ranges IPv6 — taille négligeable. Validation : ≥10 ranges IPv4 en format CIDR valide. Warning si >30 jours sans refresh réussi.
- **DPAPI** (`CryptProtectData`/`CryptUnprotectData`) : Pour la protection de `proxy-original.json` dans le tray. Disponible nativement via `syscall` ou `golang.org/x/sys/windows`. Lie les données au credential store de l'utilisateur Windows — résistant à la lecture par d'autres processus ou utilisateurs.
- **Caddy** (spike uniquement) : Reverse proxy TLS pour simuler le comportement CDN dans le spike HTTP/3. Dépendance dev uniquement, pas de production.
- **Tunnel QUIC actif** : Le proxy HTTP nécessite un tunnel connecté — il ne démarre qu'après `Connect()` + `verifyRelay()` (pour obtenir le token session).

### Testing Strategy

**Tests unitaires (ajoutés/modifiés par cette spec) :**
- `relay/cfip_test.go` : IsTrustedSource (IPs CF valides/invalides/edge cases), ExtractClientIP (avec/sans header, mode insecure)
- `relay/ip_limiter_test.go` : acquire/release, limite 20/IP, **CAS race test** (goroutines Acquire concurrentes au cleanup), cleanup après TTL, entrée non perdue pendant cleanup
- `tray/sysproxy_windows_test.go` : save/set/restore/recoverOrphan via mock `registryAccessor`, broadcast WM_SETTINGCHANGE, rollback partiel
- `httpproxy/connect_handler_test.go` : token absent → erreur, token expiré → refresh single-flight, plain HTTP → conn closed (pas de réponse)
- `httpproxy/idle_timeout_test.go` : timer reset sur transfert, timeout après inactivité 120s, pas de timeout pendant transfert lent
- `relay/connect_handler_test.go` : auth token (valide, expiré, IP mismatch CF, absent), CF-Connecting-IP extraction, draining 5s, **connexion directe non-CF → drop TCP silencieux**
- `tray/sysproxy_windows_test.go` (addendum Red Team + Audit) : **RecoverOrphan avec fichier empoisonné** (DPAPI déchiffrement échoué → suppression), **RecoverOrphan avec ProxyServer malveillant** (valeur suspecte → suppression), **détection pipe cassé → restauration immédiate**, **Save() atomic write** (simuler crash entre write et rename → pas de fichier corrompu), **ProxyOverride contient relay domain depuis config** (ADR-3), **DataProtector interface mock** (ADR-1)
- `relay/cfip_test.go` (addendum Audit) : **refresh ranges** (fetch mock → remplacement atomique), **ranges stales >30j → warning log**, **fetch fail → fallback ranges embarquées**
- `relay/connect_handler_test.go` (addendum Audit) : **header Authorization sanitized dans logs** (panic recovery ne contient pas le token), **token TTL = 14400s** (ADR-2)
- `relay/cfip_test.go` (addendum ADR-4) : **goroutine ticker refresh** (mock ticker → verify atomic replace), **atomic.Pointer swap** (concurrent read pendant swap → pas de race)

**Tests d'intégration :**
- **Spike HTTP/3 + reverse proxy TLS** : streaming bidirectionnel à travers Caddy
- **Vertical slice** (Tasks 5+6 spec originale) : proxy local → tunnel → relay → destination, avec connection draining
- **Kill switch** : proxy arrêté + connexions drainées 5s sur déconnexion tunnel
- **Token refresh** : token expiré → refresh single-flight → requête réussie

**Tests manuels :**
- Navigateur avec proxy système WinINET → vérifier IP sur `ifconfig.me`
- Vérifier registre `HKCU\...\Internet Settings` après start/stop/crash du tray
- Fast user switching : vérifier que chaque utilisateur a son propre état proxy
- Broadcast `WM_SETTINGCHANGE` : vérifier prise d'effet immédiate dans Chrome/Edge

### Threat Model

**Attaquants considérés :**
- **Attaquant réseau distant** (ISP, réseau Wi-Fi hostile, CDN) : PROTÉGÉ — tunnel QUIC chiffré, auth Ed25519, CF-Connecting-IP validation, token session avec ip_hash, SSRF/TOCTOU protection, kill switch firewall.
- **Opérateur CDN (Cloudflare)** : PARTIELLEMENT PROTÉGÉ — le body JSON contenant la cible `host:port` est accessible après terminaison TLS. Defense-in-depth (cible dans le body, pas dans les headers). Chiffrement e2e client→relay = hors scope v1.
- **Attaquant local (malware, process co-résident)** : LIMITÉ — un process avec les mêmes privilèges utilisateur peut lire le registre WinINET (voit le relay domain dans ProxyOverride — risque fingerprint accepté, voir ADR-3), peut potentiellement interagir avec le named pipe IPC. DPAPI protège `proxy-original.json` au niveau credential (pas au niveau process), testable via interface `DataProtector` (ADR-1). Un malware avec accès au credential store de l'utilisateur a déjà un accès complet à sa session — hors périmètre réaliste.
- **Attaquant multi-utilisateur (session RDP, fast user switch)** : PROTÉGÉ — chaque tray gère son propre HKCU, DPAPI lie les données au credential du user spécifique.

**Limitations acceptées et documentées :**
- **Timing oracle TCP RST** : un scanner réseau peut distinguer le port 50113 (accept TCP → parse → RST) d'un port fermé (RST immédiat kernel). Limitation acceptée : le comportement est indiscernable de milliers de services qui rejettent les connexions non authentifiées. La présence du service Windows `LeVoile` dans le SCM est déjà un indicateur plus fiable pour un attaquant local.
- **Token replay même IP (NAT)** : un attaquant sur le même réseau (même IP sortante via CF) partage le même `CF-Connecting-IP`. Le `ip_hash` ne protège pas dans ce cas. Accepté : l'auth repose sur le nonce Ed25519 signé (impossible à forger sans la clé privée), l'`ip_hash` est defense-in-depth contre le vol de token cross-IP.

### Notes

- Ce tech-spec corrige la spec existante `tech-spec-ip-camouflage-web-proxy.md` — il ne la remplace pas. Chaque task correspond à une section/ligne à modifier dans le document cible.
- Le changement architectural majeur (proxy WinINET dans le tray) a un impact transversal sur les Tasks 7, 8, 9, et 10 de la spec originale.
- L'architecture tray-managed est supérieure au service-managed : résout LocalSystem (finding 1), multi-utilisateurs (finding 9), et simplifie le service. Le coût est une dépendance sur le tray étant actif — mais si le tray n'est pas actif, le proxy écoute sans que le navigateur le sache, ce qui est safe (pas de fuite IP).
- Les 12 findings sont traités : 2 bloqueurs architecturaux (tray + CF-IP), 1 correction factuelle (mutex), 9 améliorations techniques (spike, draining, token refresh, CAS, RST, idle timeout, partial failure, ProxyOverride, honnêteté doc).
- **Durcissements Red Team/Blue Team (6 findings additionnels)** : (1) sequence number IPC + détection pipe cassé pour la race tray/service, (2) drop TCP silencieux pour connexions non-Cloudflare directes au relay, (3) clarification nonce Ed25519 = auth principal vs ip_hash = defense-in-depth, (4) validation DPAPI + whitelist sur proxy-original.json contre empoisonnement local, (5) firewall kill switch activé AVANT draining pour éliminer la fenêtre de fuite 5s, (6) circuit-breaker ne bloque que le refresh, pas les requêtes avec token valide.
- **Durcissements Security Audit (9 findings additionnels)** : (1) ProxyOverride relay domain depuis config (ADR-3 a revert l'IP résolue pour failover-safety), (2) DPAPI remplace HMAC-SID avec interface `DataProtector` (ADR-1), (3) sanitization `Authorization` header dans logs relay + config CF Logpush, (4) atomic write `proxy-original.json` (temp→rename, anti-corruption crash), (5) refresh Cloudflare IP ranges obligatoire 24h via goroutine ticker + `atomic.Pointer` (ADR-4), (6) TTL token réduit de 24h à 4h/14400s (ADR-2), (7) section Threat Model explicite, (8) timing oracle TCP RST documenté comme limitation acceptée, (9) token replay même IP documenté comme limitation acceptée.
- **ADRs Architecture Decision Records (5 décisions)** : (ADR-1) DPAPI confirmé + interface `DataProtector` pour testabilité cross-platform, (ADR-2) TTL 4h compromis sécurité/résilience, (ADR-3) hostname relay préféré à IP résolue pour failover-safety, (ADR-4) goroutine ticker 24h + `atomic.Pointer[[]netip.Prefix]` pour refresh CF ranges sans latence hot path, (ADR-5) `sync.WaitGroup` custom pour proxy local (hijacked CONNECT) vs `http.Server.Shutdown()` pour relay HTTP/3 (pas de hijack).
