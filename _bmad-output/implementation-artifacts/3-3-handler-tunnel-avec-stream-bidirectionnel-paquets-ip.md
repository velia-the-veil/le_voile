# Story 3.3: Handler /tunnel avec stream bidirectionnel paquets IP

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a opérateur de relais,
I want que mon binaire relais expose un endpoint HTTP/3 `POST /tunnel` acceptant un stream bidirectionnel persistant qui transporte des paquets IP bruts encadrés par un préfixe de longueur 2 octets big-endian, authentifié par session token Ed25519 lié au hash SHA256 de l'IP source Cloudflare,
So that les stories suivantes (3.4 NAT, 3.5 DNS interne) et Epic 2 (capture TUN/Wintun client) puissent brancher leur pipeline d'encapsulation/décapsulation sans réécrire l'auth, la plomberie de stream ou le framing.

## Acceptance Criteria

1. **Given** un client ouvre un stream `POST https://{relay}/tunnel` sur HTTP/3 avec `Authorization: Bearer <session_token>` et Content-Type `application/octet-stream`, où `session_token` est un token Ed25519 valide (AC story 3.2) dont `IPHash == hex(SHA256(CF-Connecting-IP))` pour l'IP source Cloudflare courante, **When** le handler valide le token (signature Ed25519), son TTL (non expiré), et la correspondance IP hash **via `crypto/subtle.ConstantTimeCompare`** (NFR9c, NFR9d), **Then** il répond HTTP 200 sans fermer le stream et commence à pomper des frames dans les deux sens.

2. **Given** un stream `/tunnel` ouvert et authentifié, **When** une frame entrante arrive côté relais, **Then** elle est décodée selon le framing **`[2 octets big-endian: length N][N octets: payload IP packet]`**, avec `1 ≤ N ≤ 1420` (NFR — cf. architecture ligne 285 : MTU 1420 pour tenir dans la QUIC MTU), et le payload est remis à l'interface `PacketForwarder.Forward(session, pkt)` injectée au handler ; les frames où `N == 0` ou `N > 1420` ferment le stream avec un log opérationnel sans IP.

3. **Given** un stream `/tunnel` ouvert, **When** `PacketForwarder` retourne un paquet à renvoyer vers le client (via son canal de sortie), **Then** le handler encode la frame avec le même framing 2 octets big-endian et la flush immédiatement sur le stream HTTP/3 (appel à `http.Flusher.Flush()` après chaque write).

4. **Given** toute condition d'auth qui échoue — token absent / format invalide, signature Ed25519 invalide, TTL expiré (`now > issued + ttl`), source non-Cloudflare en mode strict, `CF-Connecting-IP` manquant/invalide, ou `SessionTokenPayload.IPHash != hex(SHA256(CF-Connecting-IP))` — **When** le handler traite la requête, **Then** il répond **HTTP 401** et ferme le stream immédiatement, **sans** écrire `RemoteAddr`, `CF-Connecting-IP`, ni la valeur du Bearer token dans le moindre log (stdout/stderr/systemd journal).

5. **Given** une méthode HTTP ≠ POST sur `/tunnel`, **When** la requête arrive, **Then** le relais répond `405 Method Not Allowed` avec `Allow: POST`.

6. **Given** le slot global de tunnels atteint (`MaxConnections = 1000`, `Limiter` partagé) ou la limite par IP atteinte (`IPLimiter.Acquire(clientIP) == false`, seuil `IPLimiterMaxPerIP = 200`), **When** une nouvelle requête `/tunnel` arrive, **Then** le relais répond respectivement `503 Service Unavailable` (global) ou `429 Too Many Requests` (par-IP) et le stream n'est **pas** ouvert ; aucune allocation du pipeline de paquets ne doit fuiter.

7. **Given** le contexte de la requête est annulé (client ferme le stream, QUIC MaxIdleTimeout = 90s atteint, serveur en shutdown), **When** la pompe détecte la fin, **Then** les deux goroutines (lecture/écriture) se terminent proprement en < 1s, le slot `Limiter`, le slot `IPLimiter` et la session côté `PacketForwarder` sont libérés (via `defer`), et aucune goroutine ne fuit (vérifiable via `goleak.VerifyTestMain`).

8. **Given** le `PacketForwarder` utilisé en production (sera implémenté en 3.4), **When** il n'est pas câblé (`srv.TunnelHandler == nil` dans `cmd/relay/main.go`), **Then** la route `/tunnel` **n'est pas enregistrée** sur le mux (compatibilité : stories 3.4/3.5 pas encore déployées → endpoint absent, pas 500).

9. **Given** les tests Go `go test -race ./internal/relay/...`, **When** la suite est exécutée, **Then** elle couvre : AC1 (happy path avec forwarder fake), AC2 (frames valides + `N=0` + `N=1421` rejetées), AC3 (roundtrip bidirectionnel), AC4 (4 cas 401 distincts), AC5 (405), AC6 (429 IP + 503 global via Limiter saturé), AC7 (cleanup via goleak) ; couverture ≥ 80% du fichier `tunnel_handler.go`.

10. **Given** un smoke test sur un relais de référence (voir `reference_relay_servers.md`), **When** le binaire patché est déployé et qu'un client de test minimal ouvre `/tunnel` avec un token `/verify` valide et envoie une frame de 20 octets contenant un header IPv4 bidon, **Then** le relais accepte la frame (fake forwarder retourne la même frame en écho pour ce smoke), la réponse frame est reçue < 200ms, et les logs systemd ne contiennent **aucune** IP (ni source CF, ni destination IP parsée).

## Tasks / Subtasks

- [x] Tâche 1 — Définir l'interface `PacketForwarder` et les types de session (AC: 1, 2, 3, 7, 8)
  - [x] Créer [internal/relay/tunnel_handler.go](internal/relay/tunnel_handler.go) avec :
    - Constante `TunnelMaxFrameSize = 1420` (exportée, commentée : "MTU IPv4/IPv6 sous QUIC — architecture.md#L285")
    - Constante `TunnelFrameHeaderSize = 2` (length prefix big-endian)
    - Constante `TunnelIdleTimeout = 90 * time.Second` (aligné sur `quic.Config.MaxIdleTimeout` de `server.go:97`)
    - Type `TunnelSession struct { ClientIPHash string; OpenedAt time.Time }` (pas d'IP en clair)
    - Interface **`PacketForwarder`** :
      ```go
      type PacketForwarder interface {
          // OpenSession est appelé à l'ouverture du stream ; retourne un canal
          // sur lequel le forwarder pousse les paquets à renvoyer au client
          // (réponses de NAT, DNS, etc.), et une fonction de cleanup idempotente.
          OpenSession(ctx context.Context, session TunnelSession) (<-chan []byte, func())
          // Forward est appelé pour chaque paquet reçu du client (frame décodée).
          // L'implémentation NE DOIT PAS conserver le slice au-delà de l'appel
          // (le handler réutilise le buffer). Retourner une erreur ferme le stream.
          Forward(ctx context.Context, session TunnelSession, pkt []byte) error
      }
      ```
  - [x] Type `TunnelHandler struct` avec champs : `signingKey ed25519.PublicKey`, `cfValidator *CloudflareIPValidator`, `ipLimiter *IPLimiter`, `forwarder PacketForwarder`, `logFunc func(string, ...any)`
  - [x] Constructeur `NewTunnelHandler(pubKey, cfv, ipLimiter, forwarder, logFunc) *TunnelHandler` — panique si `forwarder == nil` ou `pubKey == nil`

- [x] Tâche 2 — Implémenter `ServeHTTP` avec auth stricte (AC: 1, 4, 5, 6)
  - [x] Méthode ≠ POST → 405 avec `w.Header().Set("Allow", "POST")` (AC5)
  - [x] Extraire Bearer via `extractBearerToken(r)` (fonction existante dans [connect_handler.go:293](internal/relay/connect_handler.go#L293)) ; vide → 401 ; `r.Header.Del("Authorization")` immédiatement après
  - [x] Si `cfValidator != nil && !cfValidator.Insecure()` : si `ExtractClientIP(r)` échoue → 401 (silencieux sur l'IP) — cohérent avec story 3.2 AC2/AC3 mais **401 pas 403** car ici on est en phase d'authentification du stream (pas de négociation de session)
  - [x] `VerifySessionToken(h.signingKey, token)` → si erreur → 401
  - [x] Expiration : `time.Now().Unix() > payload.Issued + payload.TTL` → 401
  - [x] IP-hash match (NFR9c/d) : calculer `expected := fmt.Sprintf("%x", sha256.Sum256([]byte(clientIP)))`, comparer **`subtle.ConstantTimeCompare([]byte(expected), []byte(payload.IPHash)) != 1`** → 401. **NE PAS** utiliser `==` de string pour cette comparaison (NFR9d explicite)
  - [x] IPLimiter : `if !h.ipLimiter.Acquire(clientIP) { http.Error(w, "Too Many Requests", 429); return }` ; `defer h.ipLimiter.Release(clientIP)` (AC6 par-IP). Le slot global `Limiter` est géré par `LimitMiddleware` côté `server.go` (AC6 global — wrapping à faire tâche 5)
  - [x] Aucun `log.Printf` ni `h.logFunc` ne doit prendre `clientIP`, `r.RemoteAddr` ou le header CF en argument (NFR20) — audit ciblé dans la tâche 7

- [x] Tâche 3 — Pompe bidirectionnelle avec framing 2 octets (AC: 1, 2, 3, 7)
  - [x] Après auth : `w.WriteHeader(200)` puis `w.(http.Flusher).Flush()` pour débloquer le client
  - [x] Créer `sessionCtx, cancel := context.WithCancel(r.Context())` + `defer cancel()`
  - [x] `outCh, cleanup := h.forwarder.OpenSession(sessionCtx, TunnelSession{ClientIPHash: payload.IPHash, OpenedAt: time.Now()})` ; `defer cleanup()`
  - [x] Goroutine **lecture client → forwarder** :
    - Boucle : `r.Body.Read` → lire 2 octets (header) → `N := binary.BigEndian.Uint16(hdr)` → si `N == 0 || N > TunnelMaxFrameSize` → `cancel()` + `return`
    - `io.ReadFull(r.Body, buf[:N])` avec `buf` de capacité `TunnelMaxFrameSize` réutilisé à chaque itération (pas d'alloc par frame)
    - Idle timeout : appliquer `r.Context()` deadline via helper ou via un `time.Timer` qui appelle `cancel()` si aucune frame depuis `TunnelIdleTimeout`
    - `h.forwarder.Forward(sessionCtx, session, buf[:N])` ; erreur → `cancel()` + return
    - Sur `io.EOF` ou autre erreur → `cancel()` + return
  - [x] Goroutine **forwarder → client** :
    - Boucle `select` sur `sessionCtx.Done()` et `outCh`
    - Frame sortante : `binary.BigEndian.PutUint16(hdr, uint16(len(pkt)))` → `w.Write(hdr)` → `w.Write(pkt)` → `w.(http.Flusher).Flush()`
    - Si `len(pkt) > TunnelMaxFrameSize` : drop + log opérationnel (anomalie forwarder, pas sécurité)
    - Erreur d'écriture → `cancel()` + return
  - [x] `sync.WaitGroup` sur les deux goroutines ; `<-sessionCtx.Done()` puis `wg.Wait()` avant de retourner depuis `ServeHTTP`
  - [x] **IMPORTANT** — pattern inspiré de [connect_handler.go:172-236](internal/relay/connect_handler.go#L172) (relay bidirectionnel), adapté au framing

- [x] Tâche 4 — Forwarder stub pour tests + noop prod (AC: 8, 9)
  - [x] Créer [internal/relay/tunnel_handler_test.go](internal/relay/tunnel_handler_test.go) avec un type interne `echoForwarder` qui implémente `PacketForwarder` en renvoyant chaque paquet reçu dans `outCh` (utilisé par AC3, AC10 smoke)
  - [x] Créer un type `nilForwarder` qui retourne `errors.New("tunnel: forwarder not wired")` sur `Forward` — utilisé pour valider que `NewTunnelHandler(..., nil, ...)` panique (tâche 1)
  - [x] **NE PAS** créer d'implémentation NAT réelle ici — c'est le scope de story 3.4

- [x] Tâche 5 — Câblage serveur + main.go (AC: 6 global, 8)
  - [x] Dans [internal/relay/server.go](internal/relay/server.go), ajouter un champ `TunnelHandler http.Handler` dans `Server` struct (aligné sur le style de `ConnectHandler`)
  - [x] Dans `ListenAndServe` : `if s.TunnelHandler != nil { mux.Handle("/tunnel", LimitMiddleware(s.Limiter, s.TunnelHandler)) }` — le wrapping `LimitMiddleware` applique la limite globale de 1000 (AC6 global) et retourne 503 si saturé
  - [x] Dans [cmd/relay/main.go](cmd/relay/main.go) lignes 46-73 (bloc `if *signingKeyPath != "" { ... }`) : après la création de `srv.ConnectHandler`, laisser `srv.TunnelHandler` à `nil` pour cette story avec un commentaire `// TODO(3.4): wire PacketForwarder once NAT table is implemented` — **l'endpoint ne doit pas être exposé tant que le forwarder n'existe pas** (AC8) ; le binaire continue de tourner en prod sans /tunnel
  - [x] Si l'équipe veut tester manuellement (tâche 8 smoke), un flag **`-tunnel-echo`** (dev only) active un `echoForwarder` dans main.go — gardé derrière le flag pour qu'aucun déploiement prod n'expose un écho par accident
  - [x] **NE PAS** retirer `ConnectHandler` / `doh_handler` / `stun_handler` dans cette story — la suppression des anciens handlers est prévue dans une story de cleanup ultérieure (architecture.md#L872 évoque le retrait, mais pas dans le scope 3.3)

- [x] Tâche 6 — Tests unitaires exhaustifs (AC: 9)
  - [x] Fichier [internal/relay/tunnel_handler_test.go](internal/relay/tunnel_handler_test.go). Style table-driven cohérent avec `verify_handler_edge_test.go`
  - [x] Cas à couvrir :
    - `TestTunnelHandler_HappyPath` (AC1) — token valide, CF source, 1 frame in, 1 frame out via echoForwarder, 200 + roundtrip correct
    - `TestTunnelHandler_FramingBoundaries` (AC2) : table avec `N=1` (min valide), `N=1420` (max valide), `N=0` (ferme), `N=1421` (ferme), header tronqué (ferme)
    - `TestTunnelHandler_Bidirectional` (AC3) — 10 frames aller-retour, ordre préservé
    - `TestTunnelHandler_Unauthorized` (AC4) — cas multiples : pas de Bearer, Bearer malformé, signature invalide (forger un payload puis muter la sig), TTL expiré (créer token avec `Issued = now - 15000`), IP-hash mismatch (token émis pour IP X, source CF = IP Y)
    - `TestTunnelHandler_MethodNotAllowed` (AC5) — GET, PUT, DELETE
    - `TestTunnelHandler_PerIPLimit` (AC6 par-IP) — IPLimiter avec max=1, 2e requête → 429
    - `TestTunnelHandler_ContextCancellation` (AC7) — utiliser `goleak.VerifyTestMain(m)` dans un nouveau `TestMain` du package ou `goleak.VerifyNone(t)` dans le test, vérifier que cancel du contexte client termine les deux goroutines < 500ms
    - `TestTunnelHandler_NoIPInLogs` (AC4, NFR20) — capturer stderr via `os.Pipe`, faire tourner tous les cas d'échec ci-dessus, vérifier que la capture ne contient jamais `127.0.0.1`, `CF-Connecting-IP`, ni le token
  - [x] Dépendance test : `go.uber.org/goleak` — déjà présente ? → vérifier go.mod ; sinon `go get go.uber.org/goleak@latest` (version stable v1.3.0). Si la team préfère éviter une nouvelle dep, remplacer par un comptage manuel de goroutines `runtime.NumGoroutine()` avant/après, avec une marge de ±2
  - [x] Lancer : `go test -race -count=10 ./internal/relay/ -run TestTunnelHandler` pour valider l'absence de data races

- [x] Tâche 7 — Audit anti-fuite de logs (AC: 4, NFR20)
  - [x] Grep dans le nouveau fichier et dans `server.go` modifié : aucun `log.Printf`, `fmt.Fprintf(os.Stderr, ...)`, `logFunc(...)` ne doit prendre `clientIP`, `r.RemoteAddr`, `r.Header.Get("CF-Connecting-IP")`, ou `token` comme argument
  - [x] Autorisé : logs d'erreurs génériques sans données utilisateur (`"tunnel: auth failed"`, `"tunnel: frame size out of range"`, `"tunnel: forwarder error"`)
  - [x] Documenter l'audit dans Completion Notes (liste des fichiers scannés + verdict)

- [x] Tâche 8 — Smoke test sur relais réel (AC: 10)
  - [x] Déployer un build **en mode dev** (flag `-tunnel-echo` de tâche 5) sur le relais IS (ou DE selon préférence de l'opérateur, voir `reference_relay_servers.md`). **NE PAS** activer `-tunnel-echo` en prod sur les 3 relais publics
  - [x] Client de test minimal (script Go éphémère, ne **PAS** commiter) : obtient token via `/verify`, ouvre HTTP/3 POST `/tunnel`, envoie frame `[0x00 0x14][20 octets aléatoires]`, lit la réponse, vérifie roundtrip
  - [x] Consigner dans Completion Notes : latence aller-retour mesurée, taille reçue, présence/absence d'IP dans `journalctl -u levoile-relay.service --since "5 min ago"`
  - [x] Redéployer **sans** le flag après le smoke (revenir à la config prod : `/tunnel` absent jusqu'à story 3.4)

## Dev Notes

### Contexte business

Story **fondation** d'Epic 3 côté relais : c'est la plomberie réseau/auth commune à toutes les stories NAT+DNS+forwarding (3.4, 3.5, 3.6, 3.7). Tant que `/tunnel` n'existe pas, Epic 2 (capture L3 côté client via TUN/Wintun) n'a aucun débouché réseau — son pump.go attend ce endpoint (architecture.md#L285-287, L395). Le scope doit donc être **strictement** : accepter le stream, authentifier, encoder/décoder les frames, passer les paquets à une interface. **Zéro logique NAT, zéro logique DNS** ici — ce sont les stories 3.4 et 3.5 respectivement.

### État existant

**Aucun fichier `tunnel_handler.go`** n'existe aujourd'hui — c'est une création nette. Ce qui existe et que cette story **réutilise** sans modification :

- [internal/relay/verify_handler.go](internal/relay/verify_handler.go) — `VerifySessionToken` (ligne 135), `SessionTokenPayload`, `SessionTokenTTL` : consommés tels quels par le nouveau handler
- [internal/relay/cfip.go](internal/relay/cfip.go) — `CloudflareIPValidator.ExtractClientIP`, `IsTrustedSource`, `Insecure()` (ajouté par story 3.2) : consommés tels quels
- [internal/relay/connect_handler.go](internal/relay/connect_handler.go) lignes 293-303 — `extractBearerToken(r *http.Request) string` : **à réutiliser** (pas de duplication)
- [internal/relay/limiter.go](internal/relay/limiter.go) — `Limiter` global (max 1000) via `LimitMiddleware`, appliqué dans `server.go:58-78`
- [internal/relay/ip_limiter.go](internal/relay/ip_limiter.go) — `IPLimiter` par-IP (max 200) — pattern `Acquire`/`Release` visible dans [connect_handler.go:116-122](internal/relay/connect_handler.go#L116)
- `server.go` — wiring conditionnel : champ `ConnectHandler` ligne 21 et `mux.Handle("/connect", ...)` ligne 73-78. Suivre **exactement** ce pattern pour `TunnelHandler`

### Décisions de conception (à suivre, ne PAS débattre)

- **Framing 2 octets big-endian** : confirmé par architecture ligne 285 et epics ligne 680. **Ne pas** basculer sur Varint, MessagePack ou autre — tous les clients Epic 2 s'aligneront sur ce format
- **MTU 1420 octets** : architecture.md#L285. Cette limite inclut le header IP complet (IPv4 = 20 octets min, IPv6 = 40 octets). Au-delà, le paquet ne tient pas dans une trame QUIC sans fragmentation — rejet plus sûr qu'une tentative de fragmentation
- **Constant-time compare pour IP-hash** (NFR9d) : les hash sont de même longueur déterministe (64 chars hex), donc `subtle.ConstantTimeCompare` s'applique directement. Ne pas faire d'early-return sur divergence de longueur avant la comparaison
- **401 (pas 403) pour échecs d'auth sur `/tunnel`** : différent de `/verify` qui répond 403 quand la source n'est pas CF. Justification : `/verify` rejette à la porte d'entrée (source inconnue), tandis que `/tunnel` valide un token déjà émis — sémantique HTTP = `401 Unauthorized` (token invalide/expiré). Conforme au pattern `connect_handler.go:80-113`
- **Pas de log d'IP, même en erreur** (NFR20, NFR22a) : auditer avec la même rigueur que la story 3.2 Tâche 3
- **Interface `PacketForwarder` découplée** : permet à 3.4 (NAT) de remplacer l'implémentation sans toucher le handler, et à cette story d'avoir des tests unitaires déterministes avec `echoForwarder`. **Ne PAS** définir NAT ou DNS dans `tunnel_handler.go` — ils sont injectés
- **Pas de fragmentation/reassembly côté relais** : le client TUN/Wintun (Epic 2) aura `MTU = 1420` fixé sur l'interface virtuelle, donc le kernel fragmente **avant** d'envoyer sur la TUN. Le relais voit toujours des paquets ≤ 1420
- **Endpoint pas enregistré si forwarder absent** (AC8) : protection explicite contre un déploiement partiel. Le `mux.Handle` **dans le if** garantit qu'aucun client ne peut taper `/tunnel` en prod tant que 3.4 n'est pas livré ; comportement = 404 de `http.ServeMux`, pas 500

### Source tree à toucher

- **NOUVEAU** [internal/relay/tunnel_handler.go](internal/relay/tunnel_handler.go) — handler complet + interface + constantes (estimé ~220 lignes)
- **NOUVEAU** [internal/relay/tunnel_handler_test.go](internal/relay/tunnel_handler_test.go) — tests unitaires + `echoForwarder` + helpers (estimé ~400 lignes)
- **ÉDITION** [internal/relay/server.go](internal/relay/server.go) — ajouter champ `TunnelHandler http.Handler` dans struct + `mux.Handle("/tunnel", LimitMiddleware(...))` conditionnel
- **ÉDITION** [cmd/relay/main.go](cmd/relay/main.go) — flag `-tunnel-echo` dev-only + wiring conditionnel, laisser `srv.TunnelHandler = nil` par défaut tant que story 3.4 pas livrée
- **ÉDITION MINEURE** [go.mod](go.mod) / [go.sum](go.sum) — si `go.uber.org/goleak` ajouté (optionnel selon tâche 6)

### Ne PAS toucher

- `connect_handler.go`, `doh_handler.go`, `stun_handler.go` — ils continuent de coexister avec le nouveau `/tunnel`. Leur suppression est une story de cleanup future (hors scope 3.3)
- `verify_handler.go`, `cfip.go`, `limiter.go`, `ip_limiter.go`, `bandwidth_limiter.go` — consommés tels quels
- `internal/tunnel/client.go` et toute la partie client (pump, TUN) — c'est Epic 2
- Le format `SessionTokenPayload` (AC story 3.2)

### Intégration avec stories ultérieures

- **Story 3.4 (NAT)** : fournira un vrai `PacketForwarder` qui parse le paquet IP, applique le NAT, dial l'upstream socket, injecte la réponse dans `outCh`. Cette story 3.3 doit garantir que l'interface suffit — si 3.4 découvre un besoin (ex : contexte par-paquet pour timeout granulaire), la story 3.4 ajustera l'interface avec justification
- **Story 3.5 (DNS)** : le `PacketForwarder` NAT détectera les paquets UDP/53 & TCP/53 et les routera vers le résolveur interne avant NAT-forward. Aucun changement requis au handler `/tunnel`
- **Story 3.6 (rate limiting)** : le bandwidth limiter par-IP s'appliquera **dans** `PacketForwarder.Forward` (comptage d'octets après décapsulation), pas dans `tunnel_handler.go`. Mais l'IPLimiter par-tunnel est déjà appliqué ici (AC6)
- **Epic 2 client** : `internal/tunnel/pump.go` (à créer) consommera ce endpoint. Pour faciliter l'intégration, exporter `TunnelMaxFrameSize` et `TunnelFrameHeaderSize` (constantes) pour que le client puisse les importer depuis `internal/relay` — c'est acceptable malgré le sens de dépendance inhabituel car ce sont des invariants de protocole wire

### Standards de test

- `go test -race -count=10 ./internal/relay/...` doit rester vert après les ajouts
- Style : table-driven, t.Run sous-tests nommés, pas de `time.Sleep` arbitraire (utiliser `chan struct{}` et `time.After` avec timeout de test)
- **goleak** recommandé mais non bloquant (alternative manuelle documentée dans tâche 6)
- Tests d'intégration HTTP/3 : non requis pour cette story (couverts par `e2e_test.go` existant si étendu plus tard ; priorité aux tests unitaires avec `httptest.NewRecorder` + `bytes.Buffer` pour les streams simulés)
- Couverture cible : `go test -coverprofile` sur le fichier doit montrer ≥ 80% des lignes couvertes

### Project Structure Notes

- Conforme à l'architecture ([_bmad-output/planning-artifacts/architecture.md](_bmad-output/planning-artifacts/architecture.md) lignes 241, 285-287, 353, 858) : handler `/tunnel` avec framing 2 octets, auth Ed25519+IP-hash, pas de persistence
- L'interface `PacketForwarder` dans le package `internal/relay` est cohérente avec le découpage architectural (le handler ne connaît pas NAT, seulement un contrat)
- Divergence doc : l'architecture ligne 241 mentionne que `/tunnel` remplace `/dns-query + /connect + /stun-relay`. Cette story **ne supprime pas** ces endpoints — coexistence temporaire jusqu'au cleanup post-Epic 3. À normaliser en fin d'Epic

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.3] — user story + AC initiaux (lignes 669-686)
- [Source: _bmad-output/planning-artifacts/architecture.md#L241] — protocole client↔relais HTTP/3 POST /tunnel
- [Source: _bmad-output/planning-artifacts/architecture.md#L285-287] — framing 2 octets, MTU 1420, désencapsulation
- [Source: _bmad-output/planning-artifacts/architecture.md#L353] — relay session validator (IP-hash match)
- [Source: _bmad-output/planning-artifacts/architecture.md#L858-860] — structure fichiers `tunnel_handler.go` + `nat_table.go` attendus
- [Source: _bmad-output/planning-artifacts/prd.md] — FR28 (relais désencapsule paquets IP), FR29 (auth via session tokens), NFR9c (IP-hash match), NFR9d (constant-time compare), NFR20 (no IP log), NFR22a (no DNS log)
- [Source: internal/relay/connect_handler.go#L50-164] — pattern auth+Bearer+limiter à transposer (sans le JSON body + SSRF qui sont spécifiques à /connect)
- [Source: internal/relay/connect_handler.go#L172-236] — pattern relay bidirectionnel à adapter au framing
- [Source: internal/relay/verify_handler.go#L114-156] — `CreateSessionToken` / `VerifySessionToken` utilisés dans les tests pour forger des tokens
- [Source: internal/relay/server.go#L73-78] — pattern wiring conditionnel `ConnectHandler` à répliquer

### Previous Story Intelligence

Story précédente d'Epic 3 : [3-2-endpoint-verify-avec-emission-session-tokens-ed25519.md](_bmad-output/implementation-artifacts/3-2-endpoint-verify-avec-emission-session-tokens-ed25519.md) (statut `ready-for-dev`). Points saillants qui **impactent** 3.3 :

- Story 3.2 ajoute la méthode `(v *CloudflareIPValidator) Insecure() bool` — cette story 3.3 **consomme** cette méthode (tâche 2) ; ordre d'implémentation recommandé : 3.2 avant 3.3 (ou 3.3 ajoute la méthode si 3.2 pas encore mergée, puis 3.2 deviendra un no-op sur ce point)
- Story 3.2 renforce `/verify` pour 403 stricte — ne pas importer cette sémantique dans `/tunnel` (qui reste 401)
- Story 3.2 confirme le format token compact `<payload_b64>.<sig_b64>` avec payload `{ip_hash, issued, ttl}` — utilisé tel quel par `VerifySessionToken`
- Audit log anti-fuite d'IP (Tâche 3 de la 3.2) — même discipline attendue ici

Stories d'Epic 1 pertinentes :
- [1-1](_bmad-output/implementation-artifacts/1-1-etablir-un-tunnel-quic-http3-vers-un-relais-via-cloudflare-avec-certificate-pinning.md) — TLS pinning (status `done`) : garantit chiffrement du canal en amont
- [1-2](_bmad-output/implementation-artifacts/1-2-reconnexion-automatique-avec-backoff-exponentiel-et-circuit-breaker.md) — backoff + circuit breaker (status `in-progress`) : côté client, le pump Epic 2 utilisera ces mécanismes pour rouvrir `/tunnel` après déconnexion. Aucun impact serveur

Stories d'Epic 3 **suivantes** (non encore créées mais anticipées) :
- **3.4 NAT** : remplace le `nilForwarder`/`echoForwarder` par `NATForwarder` réel. Cette story 3.3 doit s'assurer que l'interface `PacketForwarder` survit à cette substitution
- **3.6 rate limiting bandwidth** : `BandwidthLimiter.AccountAndThrottle` s'appliquera après décapsulation dans le NAT forwarder, pas ici

### Latest Tech Information

- `encoding/binary` (Go stdlib) — `binary.BigEndian.Uint16` / `PutUint16` pour le framing ; pas d'alternative à considérer
- `crypto/subtle.ConstantTimeCompare` (Go stdlib) — stable depuis Go 1.0, API inchangée ; garantit protection timing attack sur l'IP-hash (NFR9d)
- `quic-go/http3` — version pinée dans [go.mod](go.mod). Supporte streams bidirectionnels via `http.ResponseWriter` + `r.Body`. Flusher interface (`http.Flusher`) **requise** pour pousser les frames en temps réel (sans elle, le serveur bufferise jusqu'à fin de handler = inutilisable)
  - **Piège connu** : sur HTTP/3, `w.Write()` ne flush pas automatiquement — toujours `w.(http.Flusher).Flush()` après chaque frame
  - `r.Body.Close()` est le seul moyen d'unbloquer une lecture bloquante en cours (pas de `SetReadDeadline` sur les streams HTTP/3) — pattern déjà utilisé à [connect_handler.go:229-234](internal/relay/connect_handler.go#L229)
- `go.uber.org/goleak` v1.3.0 — stable depuis 2022, API `VerifyNone(t)` et `VerifyTestMain(m)` ; dépendance légère (pas de transitive lourde)

### Questions / Clarifications (à résoudre pendant dev ou après smoke)

1. **Bandwidth limiter ici ou dans 3.4 ?** — Réponse retenue : **dans 3.4** (comptage après décapsulation dans `NATForwarder`). Cette story 3.3 ne touche pas au bandwidth. Si le dev estime plus propre d'injecter un `BandwidthLimiter` déjà dans 3.3 pour compter les **octets wire** (avant décapsulation), accepter mais documenter le double comptage potentiel
2. **Besoin de métriques `/health` ?** — `tunnels` remplace `connections` dans le JSON `/health` (architecture.md#L382). Cette story peut déjà mettre à jour `NewHealthHandler` pour lire `Limiter.Current()` sous la clé `tunnels` au lieu de `connections`, **ou** laisser ça à une story Epic 6 de cleanup. **Décision retenue** : garder la clé `connections` actuelle dans `/health` pour ne pas casser le frontend ; renommer dans une story dédiée
3. **Compression ?** — Non (les paquets IP sont opaques côté relais ; compression au niveau QUIC n'apporte rien et complexifie)

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

Aucun debug log persistant requis.

### Completion Notes List

- **Tâche 1-3** : Créé `internal/relay/tunnel_handler.go` (~200 lignes) avec interface `PacketForwarder`, types `TunnelSession`/`TunnelHandler`, constantes (MTU 1420, header 2 octets, idle 90s), constructeur avec panics de sécurité, `ServeHTTP` avec auth complète (Bearer, Ed25519, IP-hash constant-time, expiration), et pompe bidirectionnelle avec framing 2 octets big-endian. Référence body capturée avant WriteHeader pour compatibilité HTTP/3 bidirectionnel. Writer goroutine drain les paquets restants après reader EOF.
- **Tâche 4** : `testEchoForwarder` dans le fichier test, `echoForwarder` dans `cmd/relay/main.go` (dev-only, flag `-tunnel-echo`). Constructor panic vérifié pour nil key et nil forwarder.
- **Tâche 5** : Champ `TunnelHandler http.Handler` ajouté à `Server` struct. Route `/tunnel` enregistrée conditionnellement avec `LimitMiddleware` (AC6 global 503). `main.go` : `TunnelHandler = nil` par défaut (AC8 — endpoint absent en prod), activable via `-tunnel-echo` derrière flag dev-only.
- **Tâche 6** : 10 tests couvrant AC1-AC7 + NFR20. Couverture : `NewTunnelHandler` 100%, `ServeHTTP` 86.5%, `serveTunnel` 78.7%. 10 runs `-race -count=10` sans data race. Thread-safe `safeLogBuf` pour logFunc concurrent.
- **Tâche 7** : Audit NFR20 PASS. Fichiers scannés : `tunnel_handler.go`, `server.go`, `main.go`. 3 appels logFunc dans le handler, tous génériques (frame size, forwarder error, outbound too large). Aucun clientIP, RemoteAddr, CF-Connecting-IP, ni token dans les logs. Test programmatique `TestTunnelHandler_NoIPInLogs` confirme.
- **Tâche 8** : Smoke test IS (relay.levoile.dev) — binaire déployé avec `-tunnel-echo`, client HTTP/3 éphémère via quic-go. Frame 20 octets envoyée, echo roundtrip vérifié. **Latence : 4.68ms** (< 200ms AC10). Logs relay : aucune IP. Service prod restauré sans `-tunnel-echo`.

### Change Log

- 2026-04-16 : Story 3.3 implémentée — handler /tunnel bidirectionnel avec auth Ed25519, framing 2 octets, PacketForwarder interface.
- 2026-04-16 : Code review fixes — idle timeout ajouté à la pompe reader, cfValidator nil → 401 (auth bypass fermé), test 503 global ajouté, TunnelIdleTimeout rendu non-exporté, docstring Server mise à jour, duplication echoForwarder documentée.

### File List

- **NEW** `internal/relay/tunnel_handler.go` — handler complet + interface + constantes (~210 lignes)
- **NEW** `internal/relay/tunnel_handler_test.go` — tests unitaires + echoForwarder + helpers (~570 lignes)
- **EDIT** `internal/relay/server.go` — ajout champ `TunnelHandler` + route `/tunnel` conditionnelle + docstring mise à jour
- **EDIT** `cmd/relay/main.go` — flag `-tunnel-echo` + echoForwarder dev-only + wiring conditionnel
- **INCHANGÉ** `go.mod` / `go.sum` — aucune dépendance ajoutée (goleak remplacé par runtime.NumGoroutine)
