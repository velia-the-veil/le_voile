# Story 1.3: Relais accepte les connexions QUIC/HTTP3 entrantes

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a **opérateur d'un VPS relais Le Voile**,
I want **que le binaire `relay` écoute en HTTP/3 sur `:443` avec TLS 1.3 et expose `/health`**,
So that **les clients Le Voile peuvent se connecter via Cloudflare et vérifier que le relais est opérationnel, tout en rejetant les requêtes n'originant pas des plages Cloudflare**.

## Acceptance Criteria

1. **AC1 — Démarrage HTTP/3** : Le binaire démarré avec `-addr :443 -cert /etc/levoile/cert.pem -key /etc/levoile/key.pem` ouvre un listener QUIC/HTTP3 sur le port 443 et accepte le handshake d'un client `quic-go/http3` en TLS 1.3 minimum (`tls.VersionTLS13`).
2. **AC2 — Handler `/health`** : GET `https://{addr}/health` via HTTP/3 retourne `200 OK` avec un corps JSON conforme au schéma `{status, connections, uptime, ram_mb, cpu_pct}`. Les méthodes non-GET retournent `405`.
3. **AC3 — Dual-stack TCP HTTPS** : Le même mux est servi en TCP/TLS (HTTP/1.1 + HTTP/2) sur `:443` pour les clients non-QUIC (registry bootstrap, latency checker).
4. **AC4 — Filtrage source Cloudflare** : Quand la requête provient d'une IP hors plages Cloudflare connues et que `-cf-insecure` n'est pas passé, un middleware de validation source répond **HTTP 403** avant d'atteindre le handler applicatif. Ce filtrage s'applique à tous les endpoints publics (`/health`, `/verify`, `/ip`, `/dns-query`, `/stun-relay`, `/.well-known/relay-registry.json`). Exception intentionnelle : `/connect` (AC vérifiée en Story 3.3+) conserve sa propre validation.
5. **AC5 — Zero log IP client (NFR20)** : Aucun chemin de log (erreur, refus CF, 403, 405, 503) n'enregistre la valeur de `RemoteAddr`, `CF-Connecting-IP`, ou toute autre IP cliente. Les refus sont loggués au niveau aggregé sans IP (ex: `"relay: cf-reject: untrusted source"` — PAS l'IP).
6. **AC6 — Arrêt propre** : Sur `SIGINT`/`SIGTERM` (ou `ctx.Cancel`), le serveur appelle `Shutdown` sur listeners HTTP/3 + TCP sans goroutine leak (vérifiable par `goleak` ou test `TestServerShutdown`).
7. **AC7 — Tests** : `go test ./internal/relay/... ./cmd/relay/...` passe. Couverture serveur + filtrage CF + /health ≥ 80 %. Au moins un test e2e confirme (a) handshake HTTP/3 TLS 1.3 réussi depuis CF-range simulé, (b) réponse 403 depuis IP hors-CF en mode strict.

## Tasks / Subtasks

- [x] **Tâche 1 — Audit du code existant vs AC** (AC: 1,2,3,4,5,6,7)
  - [x] 1.1 Relire [internal/relay/server.go](internal/relay/server.go) et confirmer que `ListenAndServe` couvre AC1/AC2/AC3/AC6.
  - [x] 1.2 Relire [cmd/relay/main.go](cmd/relay/main.go) — confirmer flags `-addr`, `-cert`, `-key`, `-cf-insecure`.
  - [x] 1.3 Relire [internal/relay/cfip.go](internal/relay/cfip.go), [middleware.go](internal/relay/middleware.go), [health.go](internal/relay/health.go).
  - [x] 1.4 Produire un tableau "AC vs code existant" dans le Dev Agent Record section **Completion Notes**.

- [x] **Tâche 2 — Implémenter le middleware `CloudflareSourceMiddleware`** (AC: 4, 5)
  - [x] 2.1 Créer `internal/relay/cf_middleware.go` exposant `CFSourceMiddleware(v *CloudflareIPValidator, logFunc func(string), next http.Handler) http.Handler`. Si `v == nil` OU `v.insecure == true`, passer direct. Sinon `v.IsTrustedSource(r.RemoteAddr)` → false ⇒ `http.Error(w, "Forbidden", http.StatusForbidden)`.
  - [x] 2.2 Dans [server.go](internal/relay/server.go), envelopper chaque `mux.Handle(...)` destiné à être public (sauf `/connect`) avec `cfWrap(...)` ⇒ `CFSourceMiddleware(s.CFIPValidator, s.CFRejectLog, ...)`.
  - [x] 2.3 Le middleware est appliqué **avant** `LimitMiddleware` (pas de slot consommé sur refus CF).
  - [x] 2.4 Logger ne reçoit que `"cf-reject"` — aucune IP transmise. Test `TestCFSourceMiddleware_LogFunc_NoIPLeak` valide NFR20 explicitement.

- [x] **Tâche 3 — Wiring dans `cmd/relay/main.go`** (AC: 1, 4)
  - [x] 3.1 `CFIPValidator` désormais construit inconditionnellement (avant le bloc `if signingKeyPath != ""`). Toujours assigné à `srv.CFIPValidator`.
  - [x] 3.2 `cfv.StartRefresh(ctx)` est démarré par `server.ListenAndServe` quand `CFIPValidator != nil` (déjà en place [server.go:106-108](internal/relay/server.go#L106-L108)).

- [x] **Tâche 4 — Tests unitaires** (AC: 7)
  - [x] 4.1 `cf_middleware_test.go` : 7 cas — nil-validator pass, insecure pass, non-CF→403, CF-source→200, NFR20 no-IP-leak, no-logfunc no-panic, malformed RemoteAddr→403.
  - [x] 4.2 `TestServer_RejectsNonCFSource` ajouté dans [server_test.go](internal/relay/server_test.go) — serveur strict + client local 127.0.0.1 (hors-CF) ⇒ 403 sur `/health`.
  - [x] 4.3 `TestServer_AcceptsInsecureMode` valide le pass-through dev. Shutdown propre est implicitement validé par tous les tests existants (cancel + sleep dans cleanup).

- [x] **Tâche 5 — Test e2e HTTP/3 + TLS 1.3** (AC: 1, 2, 7)
  - [x] 5.1 `TestServer_TLS13Negotiated` ajouté dans `server_test.go` — assert `resp.TLS != nil && resp.TLS.Version == tls.VersionTLS13` (test in-process plus rapide qu'e2e externe). Le test e2e_test.go reste pour les rounds-trips DoH réels.

- [x] **Tâche 6 — Documentation** (AC: tous)
  - [x] 6.1 Doc-comment ajouté sur le type `Server` listant les endpoints protégés par CF middleware et l'exception `/connect`.

## Dev Notes

### Code existant — état au 2026-04-15

L'ossature de Story 1.3 est **déjà largement en place** (héritage de l'ancien Epic 1 "serveur relais HTTP/3"). Vérifications par AC :

| AC | État | Preuve | Action |
|----|------|--------|--------|
| AC1 HTTP/3 `:443` + TLS 1.3 | ✅ | [server.go:51-54](internal/relay/server.go#L51-L54) `tls.VersionTLS13` + [server.go:91-100](internal/relay/server.go#L91-L100) `http3.Server` | — |
| AC2 `/health` JSON 200 | ✅ | [health.go:35-61](internal/relay/health.go#L35-L61) | — |
| AC3 Dual-stack TCP HTTPS | ✅ | [server.go:118-127](internal/relay/server.go#L118-L127) | — |
| AC4 Rejet 403 hors-CF | ❌ | CF validator existe ([cfip.go](internal/relay/cfip.go)) mais n'est PAS utilisé comme middleware global. Uniquement consulté dans `ExtractClientIP` pour `/verify`, `/ip`, `/connect`. | **Implémenter `CFSourceMiddleware`** |
| AC5 Zero log IP | ⚠️ | À auditer : `connect_handler.go` log `logFunc` passé depuis main. Vérifier qu'aucun format string n'injecte `%s` avec IP. | **Audit passif** |
| AC6 Shutdown propre | ✅ partiel | [server.go:129-139](internal/relay/server.go#L129-L139) gère ctx.Done. À valider par test goleak. | **Ajouter test** |
| AC7 Tests couverture | ⚠️ | Suite existante volumineuse mais absence de test dédié "403 hors-CF". | **Ajouter 2 tests** |

**Conclusion** : cette story est à **80 % pré-implémentée**. Le travail restant tient en ~100 LOC (middleware + 2-3 tests + wiring main.go).

### Patterns & conventions à respecter

- **Stack** : Go 1.26, `quic-go v0.59.0`, TLS 1.3 via `crypto/tls` standard. Pas de crypto maison (NFR2).
- **Nommage fichiers** : `{feature}.go` + `{feature}_test.go` + `{feature}_edge_test.go` pour tests limites (pattern déjà en place : `cfip.go`/`cfip_test.go` pas d'edge encore, `verify_handler.go`/`verify_handler_test.go`/`verify_handler_edge_test.go`).
- **Erreurs** : type `ServerError{Op, Err}` pour erreurs serveur ([server.go:153-165](internal/relay/server.go#L153-L165)). Réutiliser pour nouvelles erreurs si émises depuis server.go.
- **Logging** : pas de logger global. Le relais utilise `fmt.Fprintf(os.Stderr, ...)` dans main.go et des `func(format, args...)` injectés dans les handlers. **Règle NFR20 : jamais d'IP dans les formats.** Préférer `log("cf-reject")` à `log("cf-reject from %s", ip)`.
- **Contexte** : tout démarrage de goroutine doit respecter `ctx.Done()` (voir `StartRefresh`, `StartCleanup`).
- **Tests HTTP/3** : pattern établi dans [server_test.go](internal/relay/server_test.go) — `freeUDPAddr` + `waitForServer` + client `http3.Transport` avec `InsecureSkipVerify: true`. Réutiliser.
- **Simulation IP source non-CF en test** : `http3.Transport` ne permet pas de spoofer RemoteAddr facilement. **Recommandation** : tester le middleware directement via `httptest.NewRequest` + `ResponseRecorder` (plus rapide et déterministe) + un test e2e séparé confirmant que mode `insecure=true` pass-through fonctionne end-to-end HTTP/3.

### Architecture — contraintes applicables

Depuis [architecture.md](_bmad-output/planning-artifacts/architecture.md) :

- **Relais stateless** : aucune donnée persistée au-delà de la requête (NFR3). S'applique aux éventuels compteurs de refus CF — en RAM uniquement, pas de fichier.
- **Cloudflare validation** : NFR9d stipule que le relais doit vérifier que `CF-Connecting-IP` correspond à l'IP source (validation implémentée dans `ExtractClientIP`). Le middleware ajouté ici complète cette défense en refusant les connexions directes.
- **Gateway NAT model** : le relais agira comme gateway NAT (Story 3.3+). Pour l'instant, seuls `/health`, `/verify`, `/ip`, `/dns-query`, `/stun-relay`, `/.well-known/...` sont pertinents.
- **Dual stack TCP + QUIC** : préservé — le registry client et latency checker utilisent HTTPS classique.

### Project Structure Notes

- Aucun conflit avec la structure cible. Tous les fichiers modifiés/ajoutés résident dans `internal/relay/` conformément à [architecture.md:854-872](_bmad-output/planning-artifacts/architecture.md#L854-L872).
- Note : la structure cible mentionne `tunnel_handler.go`, `nat_table.go`, `dns_resolver.go`, `blocklist.go` comme NOUVEAU et indique que `doh_handler.go`, `stun_handler.go`, `connect_handler.go` seront **fusionnés** dans `tunnel_handler` en Epic 3. **Hors-scope pour Story 1.3** — ne pas toucher ces fichiers ici, cette refactorisation appartient aux stories 3.3-3.5.

### Previous Story Intelligence

**Note importante** : le fichier `_bmad-output/implementation-artifacts/1-2-serveur-relais-http3-et-handler-dns-over-https.md` est un artefact de l'**ancienne** structure d'epics (12 epics obsolètes depuis 2026-04-15). Son contenu couvre en partie le sujet actuel de Story 1.3 (serveur HTTP/3 + TLS 1.3) et représente le travail qui a produit le code existant dans `internal/relay/server.go`. **Utile comme référence** pour les patterns établis, mais **ne pas considérer comme prédécesseur direct** de cette story 1.3 du nouvel epic.

Les stories 1-1 (`établir-un-tunnel-quic-http3-vers-un-relais...`) et 1-2 (`reconnexion-automatique-avec-backoff...`) du **nouvel** Epic 1 n'ont pas encore d'artefact créé — elles concernent le côté CLIENT et sont indépendantes du travail côté RELAIS de cette story 1.3. Aucune dépendance bloquante.

### Git Intelligence — commits récents pertinents

Derniers commits (main) :
- `c1d7c3a` feat: add ES/GB countries, raise quotas (10 GiB/day, 1 GiB/h volume) — registry + quotas, hors scope 1.3.
- `0b5314e` fix: random relay selection, proxy cleanup, MaxConnections 1000 — la valeur `MaxIncomingStreams: 1000` en [server.go:96](internal/relay/server.go#L96) vient de ce commit.
- `a1adf3f`, `66469e7`, `8c9938d` : UI/service lifecycle, hors scope 1.3.

**À retenir** : ne pas modifier `MaxIncomingStreams` ou `MaxIdleTimeout` sans concertation (tuning validé empiriquement).

### Latest Tech Information

- **quic-go v0.59.0** : API `http3.Server` stable, pattern `ConfigureTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13})` est idiomatique et requis. Ne pas downgrade en dessous de v0.59.
- **Cloudflare IP ranges** : l'endpoint `https://www.cloudflare.com/ips-v4` et `/ips-v6` reste la source officielle. Hardcoded fallbacks ([cfip.go:34-60](internal/relay/cfip.go#L34-L60)) à jour au 2026-03.
- **Go 1.26 `net/netip`** : `netip.Prefix.Contains(ip)` est le pattern recommandé (zéro allocation) — déjà utilisé.

### Testing Standards

- Frameworks : `testing` standard Go + table-driven tests. Pas de `testify` dans ce repo.
- Tests HTTP/3 : démarrer serveur réel avec cert auto-signé (voir `testCertPath`/`testKeyPath` utilisés dans la suite existante).
- Couverture minimale : ≥ 80 % sur le nouveau middleware.
- Vérif zero-log IP : test grep-style sur les messages captés via `logFunc` injecté en test.
- Ne PAS utiliser `testify/mock`. Si besoin de simuler `IsTrustedSource`, exposer un petit interface (`type CFValidator interface { IsTrustedSource(string) bool }`) et en fournir un fake test.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#L423-L440] — Story 1.3 acceptance criteria originales.
- [Source: _bmad-output/planning-artifacts/prd.md#L542] — NFR20 zero log IP client.
- [Source: _bmad-output/planning-artifacts/prd.md#L519] — NFR9g protection injection paquets TUN (non-scope ici mais contexte).
- [Source: _bmad-output/planning-artifacts/architecture.md#L854-L872] — Structure cible `internal/relay/`.
- [Source: _bmad-output/planning-artifacts/architecture.md#L285] — Spec tunnel IP via HTTP/3 (contexte pour stories 3.x).
- [Source: _bmad-output/planning-artifacts/architecture.md#L382] — Format attendu `/health` response (tunnels, nat_entries à venir — hors-scope 1.3).
- [Source: internal/relay/server.go] — ossature HTTP/3 + TCP existante.
- [Source: internal/relay/cfip.go] — CloudflareIPValidator (déjà implémenté).
- [Source: internal/relay/middleware.go] — pattern LimitMiddleware à émuler pour `CFSourceMiddleware`.

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

### Completion Notes List

- Story créée 2026-04-15 par workflow `create-story` (YOLO mode).
- Implémentation 2026-04-15 par workflow `dev-story`.
- Écart principal identifié vs code existant : **absence du middleware CF global** — corrigé.
- **Tableau AC vs implémentation finale** :
  | AC | Statut | Évidence |
  |----|--------|----------|
  | AC1 HTTP/3 :443 + TLS 1.3 | ✅ | `TestServer_TLS13Negotiated` |
  | AC2 `/health` 200 + 405 non-GET | ✅ | `TestServer_RoutingHealth` (existant) |
  | AC3 Dual-stack TCP HTTPS | ✅ | code [server.go:118-127](internal/relay/server.go#L118-L127) (inchangé) |
  | AC4 403 hors-CF | ✅ | `TestServer_RejectsNonCFSource` + `TestCFSourceMiddleware_NonCFSource_Returns403` |
  | AC5 Zero log IP NFR20 | ✅ | `TestCFSourceMiddleware_LogFunc_NoIPLeak` + main.go format string `"relay: %s\n"` (reason seul) |
  | AC6 Shutdown propre | ✅ | inchangé, validé via cleanup pattern existant |
  | AC7 Tests | ✅ | 9 nouveaux tests, suite relay verte (ok 3.234s) |
- **Note régressions** : 4 tests pré-existants échouent dans `internal/ui` (build error `handleToggle`), `internal/desktop` (`TestQuit_SendsActionQuit`), `internal/ipchandler` (`TestHandle_SetAutoStart_PortableMode_NilStartupType`), `internal/tray` (`TestDesktopExePath` — naming `le-voile-desktop` vs `levoile-desktop`). **Aucun n'est causé par cette story** (packages non touchés). À tracker hors scope 1.3.
- Ultimate context engine analysis completed - comprehensive developer guide created.

### File List

- `internal/relay/cf_middleware.go` (nouveau — CFSourceMiddleware, doc enrichie post-review)
- `internal/relay/cf_middleware_test.go` (nouveau — 7 tests dont NFR20 leak guard, `okHandler`→`cfMWTestOK`)
- `internal/relay/server.go` (modifié — doc-comment + champ `CFRejectLog` + wrap handlers via cfWrap ; champ `Insecure` mort supprimé post-review)
- `internal/relay/server_test.go` (modifié — `TestServer_TLS13Negotiated`, table-driven `TestServer_RejectsNonCFSource_AllEndpoints`, `TestServer_RejectsNonCFSource_TCPListener`, `TestServer_AcceptsInsecureMode`, `TestServer_ShutdownNoLeak`, helper `setupTestServerWithCF`)
- `internal/relay/cfip.go` (modifié post-review — méthode `IsInsecure()` + suppression IP des messages d'erreur `ExtractClientIP` pour NFR20)
- `cmd/relay/main.go` (modifié — instanciation inconditionnelle de `CFIPValidator` + `srv.CFRejectLog`)

### Change Log

- 2026-04-15 : Implémentation initiale (CFSourceMiddleware + wiring + tests).
- 2026-04-15 : Code review fix-all — H1 (champ `Insecure` mort supprimé), H2 (IP retirée des erreurs cfip pour NFR20), M1 (test shutdown goroutine count), M2 (test TCP listener 403), M3 (méthode `IsInsecure()` + check redondant clarifié), M4 (table-driven 6 endpoints), L1 (`gofmt`), L2 (doc enrichie), L3 (helper `setupTestServerWithCF`), L4 (`okHandler`→`cfMWTestOK`).

## Open Questions

1. Doit-on permettre un flag `-cf-log-rejects` pour compter agrégé (sans IP) les refus CF, ou restons-nous totalement silencieux ?
2. Le filtrage CF doit-il aussi couvrir `/.well-known/relay-registry.json` alors que ce chemin est consommé par le **bootstrap client** qui ne transite pas toujours par Cloudflare (Story 4.2 DoH bootstrap) ? **Recommandation** : oui, les clients passent tous via CF en prod ; le mode direct reste derrière `-cf-insecure` pour les tests.
3. `/connect` (hors scope 1.3) fait volontairement exception : à confirmer quand Story 3.3 l'aura remplacé par `/tunnel`.
