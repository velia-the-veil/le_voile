# Story 3.2: Endpoint /verify avec émission session tokens Ed25519

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a opérateur de relais,
I want que mon relais émette des session tokens Ed25519 signés via `/verify`, liés à l'IP source (hash SHA256 de CF-Connecting-IP) et de TTL 4h,
So that les clients puissent authentifier ensuite leurs requêtes `/tunnel` (Story 3.3) et `/connect` sans que l'IP réelle ne soit jamais loggée.

## Acceptance Criteria

1. **Given** un client émet `POST https://{relay}/verify` avec Content-Type `application/json` et body `{"nonce": "<base64 32 octets>"}`, **When** la source est une IP Cloudflare de confiance et le header `CF-Connecting-IP` contient une IP valide, **Then** la réponse HTTP 200 JSON contient `{signature: <base64 Ed25519 du nonce>, session_token: "<payload_b64>.<sig_b64>"}`, où le payload Ed25519-signé contient exactement `{ip_hash: hex(SHA256(CF-Connecting-IP)), issued: <unix_ts>, ttl: 14400}`.
2. **Given** le validateur Cloudflare est configuré en mode strict (`cf-insecure=false`) et la source réelle `r.RemoteAddr` n'appartient à aucune plage Cloudflare connue, **When** la requête `/verify` arrive, **Then** elle est rejetée avec HTTP 403 sans génération de token et sans écrire l'IP source dans un log.
3. **Given** le validateur Cloudflare est configuré en mode strict et la source est Cloudflare mais `CF-Connecting-IP` est absent ou syntaxiquement invalide, **When** la requête arrive, **Then** elle est rejetée avec HTTP 403 sans génération de token.
4. **Given** le TTL du session token émis, **When** le payload est décodé, **Then** la valeur `ttl` est **exactement** 14400 (4 heures) — via la constante exportée `relay.SessionTokenTTL`.
5. **Given** l'ensemble des chemins d'exécution du handler `/verify` (succès, 400, 403, 405), **When** la requête est traitée, **Then** aucune ligne de log ne contient l'IP source (ni `r.RemoteAddr`, ni `CF-Connecting-IP`) — conforme NFR20.
6. **Given** le test Go `go test ./internal/relay/...`, **When** la suite est exécutée, **Then** les cas AC2 et AC3 sont couverts par au moins un test unitaire neuf (403 non-CF + 403 CF sans CF-Connecting-IP).
7. **Given** le mode développement `cf-insecure=true`, **When** une requête `/verify` arrive depuis n'importe quelle source sans `CF-Connecting-IP`, **Then** le comportement actuel est préservé (200 + token basé sur `RemoteAddr`) — aucune régression du dev-loop.

## Tasks / Subtasks

- [ ] Tâche 1 — Renforcer le handler /verify pour émettre 403 en mode strict (AC: 2, 3, 7)
  - [ ] Dans [internal/relay/verify_handler.go](internal/relay/verify_handler.go), faire que `ServeHTTP` retourne HTTP 403 quand `h.cfValidator != nil` ET `ExtractClientIP(r)` retourne une erreur (actuellement, la réponse 200 est émise sans token — cf. lignes 96-104)
  - [ ] Condition explicite : si le validateur existe ET qu'il n'est pas `insecure` ET que `ExtractClientIP` échoue → `http.Error(w, "Forbidden", http.StatusForbidden)` + `return` avant d'écrire quoi que ce soit d'autre
  - [ ] Si le validateur existe ET qu'il est en mode `insecure` → conserver le fallback actuel (émettre le token basé sur RemoteAddr) pour AC7
  - [ ] Si le validateur est `nil` → conserver le comportement actuel (200 + signature seule, pas de token) — c'est le cas des tests unitaires historiques
  - [ ] Exposer un accesseur lecture seule sur `CloudflareIPValidator` pour savoir s'il est insecure (par ex. méthode `(v *CloudflareIPValidator) Insecure() bool`) afin d'éviter de dupliquer la logique

- [ ] Tâche 2 — Ajouter tests unitaires 403 (AC: 2, 3, 6)
  - [ ] Dans [internal/relay/verify_handler_test.go](internal/relay/verify_handler_test.go) OU [internal/relay/verify_handler_edge_test.go](internal/relay/verify_handler_edge_test.go), ajouter :
    - `TestVerifyHandler_Forbidden_NonCloudflareSource` : validator strict, `req.RemoteAddr = "8.8.8.8:443"`, pas de `CF-Connecting-IP` → attendu 403
    - `TestVerifyHandler_Forbidden_CloudflareSourceMissingCFHeader` : validator strict, `req.RemoteAddr = "104.16.1.1:443"` (plage CF par défaut), pas de `CF-Connecting-IP` → attendu 403
    - `TestVerifyHandler_Forbidden_InvalidCFHeader` : validator strict, source CF, `CF-Connecting-IP = "not-an-ip"` → attendu 403
    - `TestVerifyHandler_InsecureMode_StillIssuesToken` : validator avec `insecure=true`, source arbitraire → attendu 200 + `session_token` non vide (AC7)
  - [ ] Chaque test vérifie également que le body de réponse ne contient NI `RemoteAddr` NI `CF-Connecting-IP` (AC5)

- [ ] Tâche 3 — Audit anti-fuite de logs IP (AC: 5)
  - [ ] Grep ciblé sur les handlers impliqués dans `/verify` : `internal/relay/verify_handler.go`, `internal/relay/cfip.go`, `internal/relay/middleware.go`, `internal/relay/server.go`, `internal/relay/limiter.go`
  - [ ] Vérifier qu'aucun `log.Printf`, `fmt.Fprintf(os.Stderr, ...)`, ni wrapper de middleware n'écrit `r.RemoteAddr` ou `CF-Connecting-IP` dans stdout/stderr (les logs systemd journalisent le process entier)
  - [ ] Note : l'erreur interne `fmt.Errorf("cfip: untrusted source %s", r.RemoteAddr)` à [internal/relay/cfip.go:131](internal/relay/cfip.go#L131) contient l'IP — acceptable tant qu'elle n'est **pas** loggée. Vérifier que les callers (verify_handler, connect_handler) la jettent silencieusement
  - [ ] Vérifier aussi [cmd/relay/main.go](cmd/relay/main.go) : aucune ligne d'accès HTTP ne doit dumper les headers ou RemoteAddr
  - [ ] Si un log problématique est trouvé, le supprimer ou le remplacer par une ligne sans IP (ex : `log.Printf("verify: rejected non-CF source")` sans interpolation)
  - [ ] Consigner le résultat de l'audit dans Completion Notes (liste des fichiers scannés + verdict)

- [ ] Tâche 4 — Valider les invariants AC4 et alignements de doc (AC: 4)
  - [ ] Confirmer que `SessionTokenTTL = 14400` est utilisé par `CreateSessionToken` ([internal/relay/verify_handler.go:119](internal/relay/verify_handler.go#L119)) — pas de magic number ailleurs
  - [ ] Vérifier que le client consomme correctement ce TTL : [internal/tunnel/client.go:629](internal/tunnel/client.go#L629) encode `sessionTokenTTL = 14400` en dur — accepter tel quel (résilient même si serveur renvoie une autre valeur un jour), pas d'action requise

- [ ] Tâche 5 — Smoke test sur relais réel (AC: 1, 2, 3)
  - [ ] Sur un des 3 relais existants (voir `reference_relay_servers.md` en mémoire), après rebuild + redeploy :
    - `curl -k -X POST -H "Content-Type: application/json" -d '{"nonce":"<base64 32B>"}' -H "CF-Connecting-IP: 203.0.113.1" https://<relais>/verify` depuis une source **non** Cloudflare → attendu **403** (en mode `-cf-insecure=false`, config prod)
    - Depuis Cloudflare (via le domaine public, trafic qui transite normalement) → attendu **200** avec `signature` et `session_token` ; décoder le payload (split sur `.`, base64url decode) et vérifier `{ip_hash, issued, ttl: 14400}`
  - [ ] Consigner les deux réponses dans Completion Notes (masquer les IPs dans le rapport)

## Dev Notes

### Contexte business

Story centrale de l'Epic 3 : c'est l'**unique** point d'entrée d'authentification entre client et relais. Un token défectueux = kill switch bloque tout trafic (NFR20). Un token trop permissif = risque de replay par un attaquant hors Cloudflare. Doit être **minimaliste** et **auditable**.

### État existant (TRÈS IMPORTANT — ne PAS réécrire)

Le handler `/verify` est **substantiellement implémenté** et tourne en production sur 3 relais (voir mémoire `reference_relay_servers.md`). Composants existants :

- [internal/relay/verify_handler.go](internal/relay/verify_handler.go) — handler POST `/verify` (1 KB max body), validation Content-Type `application/json`, nonce base64 32 octets, signature Ed25519 via `crypto/subtle`-safe `lecrypto.Sign`, émission token optionnelle si `cfValidator` défini. Constante exportée `SessionTokenTTL = 14400`. Fonctions `CreateSessionToken` et `VerifySessionToken` déjà en place et testées.
- [internal/relay/cfip.go](internal/relay/cfip.go) — `CloudflareIPValidator` avec plages CF hardcodées + refresh périodique depuis `https://www.cloudflare.com/ips-v4|v6`. `ExtractClientIP` retourne l'erreur en mode strict si source non-CF OU CF-Connecting-IP absent/invalide.
- [internal/relay/server.go:62-68](internal/relay/server.go#L62-L68) — wiring du handler avec `SetCFValidator(s.CFIPValidator)` conditionnel.
- [cmd/relay/main.go](cmd/relay/main.go) — flag `-cf-insecure` pour désactiver la validation en dev.
- [internal/tunnel/client.go](internal/tunnel/client.go) lignes 575-633 : `verifyRelay` (POST JSON, décode `{signature, session_token}`, stocke token+TTL 14400). Refresh automatique avec backoff + circuit breaker déjà implémentés (`RefreshSessionToken`, `EnsureSessionToken`).
- Tests existants : [verify_handler_test.go](internal/relay/verify_handler_test.go), [verify_handler_edge_test.go](internal/relay/verify_handler_edge_test.go) — couvrent méthode, content-type, JSON malformé, nonce vide/trop court, body trop gros. **Aucun** test actuellement sur le mode strict 403.

### Gap réel à combler (périmètre de cette story)

1. **403 absent en mode strict** : quand le validateur CF est configuré en mode non-insecure et que `ExtractClientIP` échoue (source non-CF OU CF-Connecting-IP manquant/invalide), le handler **continue** et répond 200 avec la signature du nonce mais sans `session_token`. L'AC2 et AC3 exigent un 403 explicite. C'est la modification de code principale (une dizaine de lignes dans `ServeHTTP`).
2. **Accesseur `Insecure()` manquant** : pour éviter de dupliquer la logique dans le handler, exposer cet accesseur sur `CloudflareIPValidator` (le champ est déjà présent en privé, ligne 66 de cfip.go).
3. **Tests 403 manquants** : trois nouveaux tests unitaires (Tâche 2).
4. **Audit log** : confirmer qu'aucune IP ne fuit dans les logs (NFR20 + AC5).

### Décisions de conception (à suivre, ne PAS débattre à nouveau)

- **Méthode HTTP : POST** (body JSON avec nonce). L'`epics.md#Story-3.2` et l'architecture ligne 236 mentionnent `GET /verify avec challenge dans le body`, mais :
  - HTTP/3 (quic-go/http3) et plusieurs proxys rejettent GET avec body
  - Le client [internal/tunnel/client.go:594](internal/tunnel/client.go#L594) utilise déjà POST en production
  - Tous les tests unitaires existants utilisent POST
  - Modifier vers GET = breaking change sans bénéfice de sécurité
  - **Conséquence** : on garde POST. La divergence doc est mineure et sera normalisée lors d'une passe de tech-writer future. Ne **pas** corriger `epics.md` dans cette story.

- **Format token : compact `<payload_b64>.<sig_b64>`** (JWT-like), le payload décodé est `{ip_hash, issued, ttl}` (3 champs, pas 4). L'epic mentionne un blob avec 4 champs (signature incluse dans le payload) — c'est une erreur de rédaction : la signature est forcément détachée de ce qu'elle signe. Le format compact actuel satisfait AC1 dès lors que le décodage (split `.` + base64url) donne bien le payload JSON avec `{ip_hash, issued, ttl: 14400}`.

- **Clé JSON : `issued`** (pas `issued_at`). Renommer casserait le client déployé. On garde `issued`. Mettre à jour la doc `epics.md` n'est **pas** le scope de cette story.

### Source tree à toucher

- [internal/relay/verify_handler.go](internal/relay/verify_handler.go) — **édition** (Tâche 1) : ajout branche 403 stricte dans `ServeHTTP`
- [internal/relay/cfip.go](internal/relay/cfip.go) — **édition mineure** (Tâche 1) : ajout méthode `Insecure() bool`
- [internal/relay/verify_handler_edge_test.go](internal/relay/verify_handler_edge_test.go) — **édition** (Tâche 2) : 4 nouveaux tests
- [internal/relay/middleware.go](internal/relay/middleware.go), [cmd/relay/main.go](cmd/relay/main.go) — **audit lecture seule** (Tâche 3)
- [internal/tunnel/client.go](internal/tunnel/client.go) — **pas de modification** (client est déjà compatible)

### Ne PAS toucher

- Le handler `/connect` (géré par stories antérieures — déjà audité et en prod)
- Le format wire du session token (briserait le client)
- La structure `SessionTokenPayload` (renommer `Issued` → `IssuedAt` serait breaking côté client/tests)
- Le middleware `LimitMiddleware` (hors scope)
- Le handler `/tunnel` (n'existe pas encore — Story 3.3)
- Le flag `-cf-insecure` côté CLI (comportement couvert par AC7)

### Standards de test

- Couverture Go existante à préserver : `go test ./internal/relay/...` doit rester vert
- Nouveaux tests : style table-driven déjà utilisé dans [verify_handler_edge_test.go](internal/relay/verify_handler_edge_test.go) — s'y conformer
- Pas de test d'intégration HTTP/3 requis (couvert par `e2e_test.go` existant)
- Smoke test VPS manuel (Tâche 5) — pas de script à commiter, observations dans Completion Notes

### Project Structure Notes

- Conforme à l'architecture ([_bmad-output/planning-artifacts/architecture.md](_bmad-output/planning-artifacts/architecture.md) lignes 202-203, 236, 293) : unique handler d'auth, Ed25519 par relais, pas de persistence, pas de log IP
- Zero persistence confirmée : les tokens sont stateless côté serveur (pas de table de sessions) — le relais ne stocke que sa propre clé Ed25519 + plages CF refreshées périodiquement (RAM)
- Déviation assumée doc vs code sur méthode HTTP (POST) et format token (compact) — voir section "Décisions de conception"

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.2] — user story + AC initiaux (lignes 650-667)
- [Source: _bmad-output/planning-artifacts/architecture.md] — session tokens (ligne 203), `/verify` (ligne 236), FR29/NFR20 mappings
- [Source: _bmad-output/planning-artifacts/prd.md] — FR29 (auth /tunnel via session tokens), NFR9d (IP hash match check), NFR20 (no IP log), NFR21 (SHA256 IP hash only)
- [Source: internal/relay/verify_handler.go] — handler existant
- [Source: internal/relay/cfip.go] — validateur CF existant
- [Source: internal/relay/server.go] — wiring serveur HTTP/3
- [Source: internal/tunnel/client.go#L575-L633] — consommation côté client (verifyRelay)
- [Source: cmd/relay/main.go] — flag `-cf-insecure`

### Previous Story Intelligence

Story précédente d'Epic 3 : [3-1-binaire-relais-go-http3-stateless-deployable-via-systemd.md](_bmad-output/implementation-artifacts/3-1-binaire-relais-go-http3-stateless-deployable-via-systemd.md) (statut `ready-for-dev`). Elle ajoute `CAP_NET_ADMIN` au systemd unit et durcit `install.sh` — aucun impact sur ce handler mais le deploy pipeline sera le même (rebuild → `scp relay` → `systemctl restart levoile-relay.service`).

Stories d'Epic 1 pertinentes :
- [1-1](_bmad-output/implementation-artifacts/1-1-etablir-un-tunnel-quic-http3-vers-un-relais-via-cloudflare-avec-certificate-pinning.md) — TLS pinning client (status `done`) : confirme que le canal est chiffré avant même que `/verify` ne soit appelé
- [1-2](_bmad-output/implementation-artifacts/1-2-reconnexion-automatique-avec-backoff-exponentiel-et-circuit-breaker.md) — backoff + circuit breaker (status `in-progress`) : ces mécanismes sont déjà appliqués aux refresh `/verify` (voir `RefreshSessionToken`). À ne pas réécrire ici.

### Latest Tech Information

- `crypto/ed25519` (Go stdlib) — utilisé via wrapper `internal/crypto` ; pas d'update nécessaire
- `crypto/subtle.ConstantTimeCompare` — déjà employé par `VerifySessionToken` (via `lecrypto.Verify`) — garanti par les tests Go stdlib
- Plages CF : refresh 24h via [cfip.go:135-156](internal/relay/cfip.go#L135-L156) avec fallback hardcoded — aucun changement connu au fichier `ips-v4|v6` depuis 2024
- HTTP/3 : `quic-go` version pinée dans [go.mod](go.mod) — POST avec body accepté, pas de restriction

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

### Completion Notes List

### File List
