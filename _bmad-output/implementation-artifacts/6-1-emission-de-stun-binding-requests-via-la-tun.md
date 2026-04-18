# Story 6.1 : Émission de STUN Binding Requests via la TUN

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a **utilisateur final**,
I want **que le client émette régulièrement des requêtes STUN Binding via la TUN pour valider que le trafic UDP passe bien par le tunnel**,
so that **je sois alerté si une fuite structurelle apparaît malgré la capture L3 (TUN down, firewall mal configuré, bug de routing)**.

C'est le refactor racine d'Epic 6 (Validation Anti-Fuite). La capture L3 machine-wide (Epic 2) rend les fuites structurellement impossibles : tout le trafic IP — y compris les requêtes STUN — passe par `levoile0` avant de sortir. Cette story supprime l'ancien chemin « relais STUN applicatif » (endpoint `/stun-relay` + `SendSTUNRelay` tunnel client + `internal/stun/{interceptor,relayer}.go` + intercepteur ports 3478/5349) et redéploie la vérification en émettant de simples `net.DialUDP` vers des serveurs STUN publics : l'OS route ces paquets via la TUN, le relais NAT-forward au serveur STUN distant, la réponse revient par le même chemin. La comparaison `IP STUN == IP relais attendue` reste dans le scheduler (Story 6.2 raffinera la sémantique OK/LEAK_DETECTED et la réaction UI vient en 6.3).

## Acceptance Criteria

### AC1 — Scheduler émet une requête STUN Binding (RFC 5389) à intervalle configurable
**Given** le tunnel est connecté (`tunnel.StateConnected`) et la TUN `levoile0` active (routing par défaut via TUN, Story 2.4)
**When** le scheduler `leakcheck.PeriodicScheduler` tick (intervalle `leakcheck.interval`, défaut 10 min, configurable via `[stun] leakcheck_interval`)
**Then** une requête STUN Binding conforme RFC 5389 est émise via `net.DialUDP("udp", nil, stun.l.google.com:19302)` sans passer par un relais applicatif
**And** le paquet UDP est routé par le noyau via `levoile0` (vérifiable par `ip route get 173.194.192.127` qui retourne `dev levoile0`)
**And** encapsulé par `internal/tunnel` dans le stream `/tunnel` du relais, dé-encapsulé par le handler tunnel du VPS, NAT-forwarded vers le serveur STUN cible
**And** la réponse transite inversement jusqu'au socket UDP client
**And** la requête respecte le format 20 octets : type 0x0001, length 0, magic cookie 0x2112A442, transaction ID aléatoire 12 octets (`BuildBindingRequest` de `internal/leakcheck/webrtc.go`, inchangé)

### AC2 — Parsing XOR-MAPPED-ADDRESS + validation transaction ID
**Given** une réponse STUN Binding Success (type 0x0101) arrive sur le socket UDP
**When** le scheduler parse la réponse
**Then** la taille ≥ 20 octets est vérifiée (sinon erreur `stun: response too short from {server}`)
**And** le transaction ID (octets 8-19) est comparé octet-à-octet à celui de la requête (sinon erreur `stun: transaction ID mismatch from {server}` — défense anti-spoofing UDP NFR9c, `crypto/subtle` non requis car non-secret)
**And** l'attribut XOR-MAPPED-ADDRESS (type 0x0020) est extrait via `leakcheck.ParseXORMappedAddress` — famille IPv4 (0x01) ou IPv6 (0x02) supportée, port XOR-démasqué mais ignoré
**And** l'IP retournée est stockée dans `LeakResult{Server, IP}`

### AC3 — Failover sur 3 serveurs STUN (Google x2 + Cloudflare) en cas de non-réponse
**Given** `leakcheck.defaultSTUNServers = [stun.l.google.com:19302, stun1.l.google.com:19302, stun.cloudflare.com:3478]`
**When** `RunFullCheck` s'exécute
**Then** chaque serveur est interrogé séquentiellement avec timeout `stunTimeout = 5s` par serveur (context `WithTimeout`)
**And** si un serveur échoue (timeout, erreur réseau, réponse invalide), l'erreur est appendée dans `report.Results[].Error` et le scheduler continue avec le suivant (failover **dans** le cycle de check — pas de retry cross-cycle)
**And** si les 3 serveurs échouent, `RunFullCheck` retourne `error: leakcheck: all 3 STUN servers unreachable` (sans panic, sans bloquer le scheduler)
**And** le tick suivant (10 min plus tard) retente depuis le serveur #1

### AC4 — Suppression du chemin « relais STUN applicatif » (endpoint /stun-relay + SendSTUNRelay + interceptor/relayer)
**Given** la capture L3 rend le relais applicatif STUN redondant (cf. architecture.md § « Pourquoi plus de politique navigateur »)
**When** la story est implémentée
**Then** le code suivant est **supprimé** (plus de références dans le build) :
  - `internal/stun/interceptor.go` + `interceptor_test.go` + `interceptor_53_test.go` + `interceptor_edge_test.go`
  - `internal/stun/relayer.go` + `relayer_test.go` + `relayer_53_test.go` + `relayer_edge_test.go`
  - `internal/relay/stun_handler.go` + `stun_handler_test.go` + `stun_handler_edge_test.go`
  - Route `mux.Handle("/stun-relay", ...)` dans `internal/relay/server.go:94` + commentaire en-tête ligne 16
  - Méthode `Client.SendSTUNRelay` dans `internal/tunnel/client.go:341-...` + constante `stunRelayTimeout` si orpheline
  - Champs `Program.stunInterceptor`, `stunRelayer`, `stunMu`, `stunCancel`, `stunErrCh` dans `internal/service/service.go:130-174`
  - Méthodes `Program.STUNActive`, `startSTUN`, `stopSTUN`, `setSTUNEnabled` dans `internal/service/service.go` + tous les call sites (kill switch hook, ipchandler)
  - Type `tunnelStateAdapter` si orphelin
  - IPC action `ipc.ActionSTUNStatus` + constantes `ipc.StatusSTUNActive`/`StatusSTUNInactive` + handler `handleSTUNStatus` dans `internal/ipchandler/handler.go:92-94, 402-407`
**And** `internal/stun/parser.go` + `internal/stun/stun.go` (constantes `MagicCookie`, `TypeBindingRequest`, `HeaderSize`, `DefaultPort`, `DefaultTLSPort`, helpers `IsSTUN`/`IsBindingRequest`/`IsTURN`/`ParseHeader`) sont **conservés** — utilisés par `parser_test.go` et disponibles pour usages futurs, mais plus importés par les modules supprimés
**And** le build complet reste vert (`go build ./...`) et aucun import circulaire ou import orphelin ne subsiste (`go vet ./...` propre)

### AC5 — Simplification de `WebRTCLeakChecker` : plus de `STUNRelayFunc`
**Given** le chemin unique est désormais `net.DialUDP` direct (OS route via TUN)
**When** la refacto de `internal/leakcheck/webrtc.go` est appliquée
**Then** le type `STUNRelayFunc` est supprimé
**And** le champ `WebRTCLeakChecker.stunRelay` est supprimé
**And** la méthode `WithSTUNRelay(relay STUNRelayFunc) *WebRTCLeakChecker` est supprimée
**And** `CheckSTUNLeak` utilise systématiquement `stunDirect` (renommé en méthode unique intégrée — `net.DialUDP` avec deadline `ctx.Deadline()` ou `stunTimeout`)
**And** le commentaire godoc « sends STUN through the tunnel relay » est remplacé par « sends STUN via the OS UDP stack — routed through levoile0 by the default route (Story 2.4) »
**And** les tests `webrtc_test.go` + `webrtc_edge_test.go` sont adaptés : le mock `startMockSTUNServer` reste valable (écoute `127.0.0.1:0` — le test ne passe pas par une TUN réelle, il teste la mécanique de parsing/failover), suppression des tests référençant `WithSTUNRelay`/`stunRelayFn`

### AC6 — Câblage service : plus de `stunRelayFn`, plus de `startSTUN/stopSTUN`
**Given** le service orchestrait deux flows (STUN interceptor pour browser + leak scheduler pour validation)
**When** la refacto d'`internal/service/service.go` est appliquée
**Then** dans le bloc `5b. Leak scheduler start` (lignes ~1380-1440) :
  - `getPublicIP` appelle directement `leakcheck.CheckSTUNLeak(ctx, stunServer)` — si le STUN retourne une IP valide, c'est l'IP publique vue (pas de dépendance au tunnel client pour obtenir l'IP)
  - Alternative retenue : `getPublicIP` interroge `/verify` du relais via `tunnelClient` pour connaître l'IP publique attendue (source de vérité pour la comparaison 6.2 — la comparaison réelle est une story 6.2, mais le champ `report.HTTPIP` est déjà peuplé ici pour continuité)
  - Le wiring `NewWebRTCLeakChecker(getPublicIP).WithSTUNRelay(stunRelayFn)` devient `NewWebRTCLeakChecker(getPublicIP)` (un seul argument)
  - Le callback `stunRelayFn` est supprimé
**And** tous les call sites de `startSTUN`/`stopSTUN`/`setSTUNEnabled` sont retirés :
  - `cmd/client/main.go` (le cas échéant)
  - `internal/service/service.go` — suppression des invocations dans `Start`/`Stop` + dans le hook kill switch (`setKillSwitchCallback` ou équivalent) — seul le leak scheduler subsiste
  - `internal/service/killswitch.go` — si `setSTUNEnabled` est appelé depuis le chemin kill switch, retirer (la TUN capture + firewall gère tout)
**And** le scheduler reste piloté par `ctx` parent Service — `Stop()` cancel propage, `<-done` attend la goroutine

### AC7 — Skip condition adaptée : uniquement quand tunnel non connecté
**Given** historiquement `PeriodicScheduler.runCheck` skippait quand `killSwitch.IsActive() == true` (car l'ancien intercepteur bypassait le kill switch — AC5 de Story 5.3)
**When** la nouvelle architecture est en place
**Then** le check `p.killSwitch.IsActive()` est **supprimé** du `runCheck` : en mode normal, kill switch ET TUN sont tous deux actifs, et le trafic STUN doit passer via la TUN (c'est précisément ce qui valide la chaîne)
**And** seule la condition `p.tunnelState.Get() != tunnel.StateConnected` déclenche un skip (incrément `consecutiveSkips`)
**And** `KillSwitchQuerier` est retiré de `NewPeriodicScheduler` (signature change : 4 paramètres obligatoires au lieu de 5)
**And** le champ `killSwitch` est retiré du struct `PeriodicScheduler`
**And** la constante `maxConsecutiveSkips` (actuellement 6 — ~1h de skips d'affilée) et `ConsecutiveSkips()` sont **conservées** : utilisées par 6.3 pour détecter un tunnel coincé
**And** `scheduler_test.go` adapté : suppression des tests `TestScheduler_SkipsWhenKillSwitchActive` et équivalents, mise à jour de la fixture de construction

### AC8 — Config `[stun] leakcheck_interval` + conservation `default_server`
**Given** l'intervalle est codé en dur (`10*time.Minute` dans service.go:1427)
**When** la config TOML est enrichie
**Then** `[stun] leakcheck_interval` (type `time.Duration`) est ajouté à `STUNConfig` dans `internal/config/config.go`, défaut `10m` via `DefaultConfig()`
**And** `[stun] default_server` reste conservé (rétro-compat) mais n'est **plus utilisé par le relais applicatif** (supprimé) — il est utilisé uniquement comme override du premier élément de `defaultSTUNServers` (si présent et non vide, il remplace `stun.l.google.com:19302`)
**And** `[stun] servers` (slice `[]string`) peut être ajouté optionnellement pour override complet de la liste par défaut (feature mineure — si non défini, `defaultSTUNServers` est utilisé)
**And** le TOML d'exemple (`config.example.toml`) est mis à jour avec commentaire : `# Intervalle entre deux checks leak. 10m suffit pour une validation anti-fuite non-intrusive.`

### AC9 — Journalisation conforme NFR22a (aucune donnée utilisateur)
**Given** les contraintes de non-loggage (NFR22a)
**When** le scheduler ou le checker loggent
**Then** les logs restent au niveau opérationnel : `leakcheck: stun server {server}: {err}` — l'IP STUN retournée n'est **jamais** loggée en niveau INFO
**And** en niveau DEBUG (`--debug` CLI), l'IP STUN peut être loggée (utile pour diagnostic) mais jamais le domaine consulté (inapplicable ici : STUN ne résout pas de domaine utilisateur)
**And** les erreurs 3-serveurs-down restent niveau WARN, pas ERROR (évite de polluer le journal en cas de coupure internet brève)

## Tasks / Subtasks

### Task 1 — Refactor `internal/leakcheck/webrtc.go` (AC: #2, #5, #9)
- [x] 1.1 Retirer le type `STUNRelayFunc`, le champ `stunRelay` du struct `WebRTCLeakChecker`, la méthode `WithSTUNRelay`
- [x] 1.2 Simplifier `CheckSTUNLeak` : plus de branche `if c.stunRelay != nil { ... } else { ... }` — une seule branche `stunDirect` (inlinée ou renommée `sendBindingRequest`)
- [x] 1.3 Mettre à jour le godoc package et de la méthode : mention explicite « via OS UDP stack, routed through TUN by default route »
- [x] 1.4 Laisser `BuildBindingRequest`, `ParseXORMappedAddress`, `stunTimeout = 5*time.Second` inchangés
- [x] 1.5 Ajuster `webrtc_test.go` + `webrtc_edge_test.go` : supprimer les tests exerçant `WithSTUNRelay`, vérifier que les tests « mock STUN server écoute 127.0.0.1 » passent toujours (l'OS n'a pas besoin de TUN pour tester la parse/failover logic)
- [x] 1.6 Ajouter test `TestCheckSTUNLeak_TransactionIDMismatch` : mock server renvoie une réponse avec un autre txID → erreur attendue
- [x] 1.7 Ajouter test `TestRunFullCheck_AllServersDown_ReturnsError` : 3 mock servers qui timeout → erreur `all 3 STUN servers unreachable`
- [x] 1.8 `go test ./internal/leakcheck/... -race` → vert

### Task 2 — Adapter `internal/leakcheck/scheduler.go` (AC: #1, #7)
- [x] 2.1 Supprimer le paramètre `ks KillSwitchQuerier` de `NewPeriodicScheduler` (signature passe de 6 à 5 paramètres)
- [x] 2.2 Supprimer le champ `killSwitch KillSwitchQuerier` du struct
- [x] 2.3 Supprimer le check `if p.killSwitch.IsActive() { skip }` dans `runCheck`
- [x] 2.4 Conserver `consecutiveSkips`, `maxConsecutiveSkips`, `TriggerCheck`, `LastResult`
- [x] 2.5 Ajouter test `TestScheduler_DoesNotSkipOnKillSwitchActive` (confirme qu'il n'y a plus de skip sur ce critère — avec double mock avant/après refacto)
- [x] 2.6 Mettre à jour tous les tests `scheduler_test.go` qui construisent avec `ks` → retirer
- [x] 2.7 L'interface `KillSwitchQuerier` peut être supprimée si plus d'usage, sinon gardée pour type-compat

### Task 3 — Supprimer `internal/stun/interceptor.go` + `relayer.go` (AC: #4)
- [x] 3.1 `git rm internal/stun/interceptor.go internal/stun/interceptor_test.go internal/stun/interceptor_53_test.go internal/stun/interceptor_edge_test.go`
- [x] 3.2 `git rm internal/stun/relayer.go internal/stun/relayer_test.go internal/stun/relayer_53_test.go internal/stun/relayer_edge_test.go`
- [x] 3.3 Vérifier que `internal/stun/stun.go` + `parser.go` + `parser_test.go` restent compilables seuls (constantes `MagicCookie`/`TypeBindingRequest`/`HeaderSize`/`TransactionIDSize` + helpers `IsSTUN/IsBindingRequest/IsTURN/ParseHeader` retenus ; `DefaultPort`/`DefaultTLSPort` vivaient dans `interceptor.go` supprimé — plus de consommateurs, donc non réintroduits)
- [x] 3.4 `go build ./internal/stun/...` + `go test ./internal/stun/... -race` → vert

### Task 4 — Supprimer endpoint `/stun-relay` du relais (AC: #4)
- [x] 4.1 Retirer `mux.Handle("/stun-relay", cfWrap(LimitMiddleware(s.Limiter, s.STUNHandler)))` de `internal/relay/server.go:94`
- [x] 4.2 Retirer la mention `/stun-relay` du commentaire d'en-tête ligne 16 + documentation `internal/relay/doc.go` si présente
- [x] 4.3 `git rm internal/relay/stun_handler.go internal/relay/stun_handler_test.go internal/relay/stun_handler_edge_test.go`
- [x] 4.4 Retirer champ `STUNHandler` du struct `Server` s'il existe (grep `STUNHandler`)
- [x] 4.5 `go build ./internal/relay/... ./cmd/relay/...` + `go test ./internal/relay/... -race` → vert
- [x] 4.6 Rebuild du binaire relais : `GOOS=linux GOARCH=amd64 go build -o dist/relay ./cmd/relay` — vérifier que la taille reste raisonnable (< 15 MB)

### Task 5 — Supprimer `Client.SendSTUNRelay` dans `internal/tunnel/client.go` (AC: #4)
- [x] 5.1 Retirer la méthode `SendSTUNRelay(ctx, stunPacket, targetAddr)` (lignes 341-...)
- [x] 5.2 Retirer la constante `stunRelayTimeout` si plus référencée
- [x] 5.3 Retirer les tests `client_test.go` / `client_edge_test.go` qui exercent `SendSTUNRelay`
- [x] 5.4 `go build ./internal/tunnel/...` + `go test ./internal/tunnel/... -race` → vert

### Task 6 — Refactor `internal/service/service.go` : retrait STUN interceptor + wiring leak scheduler (AC: #4, #6)
- [x] 6.1 Retirer imports `internal/stun` si plus utilisés (laisser si `stun.ParseHeader` est référencé ailleurs — peu probable)
- [x] 6.2 Retirer champs `stunInterceptor`, `stunRelayer`, `stunMu`, `stunCancel`, `stunErrCh` du struct `Program`
- [x] 6.3 Retirer méthodes `STUNActive`, `startSTUN`, `stopSTUN`, `setSTUNEnabled`
- [x] 6.4 Retirer les appels à `startSTUN`/`stopSTUN` dans `Program.Start`/`Program.Stop`
- [x] 6.5 Retirer le callback `setSTUNEnabled` du hook kill switch (si présent dans `killswitch.go` ou wiring dans `service.go`)
- [x] 6.6 Retirer le type `tunnelStateAdapter` s'il devient orphelin
- [x] 6.7 Dans le bloc leak scheduler (~ligne 1380) :
  - **DÉCISION (Q1) : `getPublicIP` = premier STUN réussi** — pas d'appel `/verify` : le handler `/verify` retourne un `SessionToken`, pas d'IP (`verifyResponse` dans [internal/tunnel/client.go:57](internal/tunnel/client.go#L57)). La source unique de vérité pour l'IP post-NAT est STUN. Implémentation : `getPublicIP(ctx)` boucle sur `defaultSTUNServers`, retourne la première `ParseXORMappedAddress` réussie. **Conséquence** : `report.STUNIP` et `report.HTTPIP` sont désormais la même valeur sur le premier tick réussi → la comparaison 6.2 se fera en croisant avec l'IP du relais actif depuis `registry` (pas `/verify`). Ajouter une NOTE dans le code : `// Story 6.2 will replace HTTPIP source with active relay IP from registry.`
  - Supprimer la closure `stunRelayFn`
  - Remplacer `NewWebRTCLeakChecker(getPublicIP).WithSTUNRelay(stunRelayFn)` par `NewWebRTCLeakChecker(getPublicIP)`
  - Mettre à jour l'appel `NewPeriodicScheduler` (5 args : interval, checker, tunnelState, onLeak, onRecovery)
  - Interval : lire `p.config.STUN.LeakcheckInterval` (défaut 10m si 0)
- [x] 6.8 `go build ./internal/service/... ./cmd/client/...` + `go test ./internal/service/... -race` → vert
- [x] 6.9 Mettre à jour `service_test.go`, `service_edge_test.go`, `service_preflight_test.go`, `service_53_edge_test.go`, `service_tun_test.go`, `service_tun_recovery_test.go`, `e2e_recovery_test.go` — retirer assertions sur `STUNActive`/`startSTUN` et adapter constructions

### Task 7 — Retirer IPC action `ActionSTUNStatus` (AC: #4)
- [x] 7.1 `internal/ipc/messages.go` : retirer constantes `ActionSTUNStatus`, `StatusSTUNActive`, `StatusSTUNInactive`
- [x] 7.2 `internal/ipchandler/handler.go` : retirer `case ipc.ActionSTUNStatus: return handleSTUNStatus(prg)` (lignes 92-94) et la fonction `handleSTUNStatus` (402-407)
- [x] 7.3 `internal/ipchandler/handler_test.go` : retirer tests `TestHandle_STUNStatus_*`
- [x] 7.4 `internal/tray/tray.go` (ou équivalent UI) : retirer tout appel `IPCClient.GetSTUNStatus()` et affichage associé dans le tooltip/menu
- [x] 7.5 `internal/ui/httpserver.go` : retirer endpoint `/api/stun-status` s'il existe + test associé
- [x] 7.6 Frontend `frontend/src/app.js` + `index.html` : retirer tout rendu `stunStatus` (grep `stun_status`, `stunStatus`)
- [x] 7.7 `go build ./...` + tests frontend (`vitest` ou équivalent s'il existe) → vert

### Task 8 — Enrichir config `[stun] leakcheck_interval` (AC: #8)
- [x] 8.1 `internal/config/config.go` : ajouter `LeakcheckInterval time.Duration toml:"leakcheck_interval"` à `STUNConfig` (défaut `10*time.Minute` dans `DefaultConfig()`)
- [x] 8.2 Option : ajouter `Servers []string toml:"servers"` (défaut `nil` — utilise `defaultSTUNServers`)
- [x] 8.3 Si `STUN.Servers` non vide, l'injecter via `WebRTCLeakChecker.WithSTUNServers()` dans le wiring service
- [x] 8.4 `config.example.toml` : ajouter commentaires
- [x] 8.5 `config_test.go` : test `TestDefaultConfig_STUNLeakcheckInterval10m` + test de parse d'une valeur custom (`5m`, `1h`)
- [x] 8.6 Propager vers `internal/service/service.go` dans le wiring leak scheduler

### Task 9 — Tests unitaires end-to-end : scheduler → checker → dial → parse (AC: #1, #3)
- [x] 9.1 Ajouter test `TestPeriodicScheduler_RunCheck_EmitsSTUNAndParsesResponse` : scheduler lancé avec un checker dont `defaultSTUNServers` pointe vers un mock UDP `127.0.0.1:port` → tick manuel via `TriggerCheck(ctx)` → assert `LastResult()` retourne `status=pass`, `STUNIP` valide
- [x] 9.2 Ajouter test `TestPeriodicScheduler_FailoverAcrossServers` : 3 mock servers, le #1 ne répond pas, le #2 renvoie une réponse valide → assert qu'au moins le #2 a un `Error == ""` dans `report.Results`
- [x] 9.3 Ajouter commentaire dans `internal/leakcheck/scheduler_test.go` : `// End-to-end real-TUN validation is covered by Epic 6 smoke test (Task 11.6), not by unit tests.`
- [x] 9.4 `go test ./internal/leakcheck/... -race -count=3` → vert (triple run pour attraper flakiness UDP)

### Task 10 — Mise à jour documentation + changelog (AC: #9)
- [x] 10.1 `README.md` : section « Anti-Leak Validation » : documenter que STUN passe via TUN (plus de relais applicatif), intervalle 10m, 3 serveurs failover
- [x] 10.2 Package godoc de `internal/leakcheck/webrtc.go` enrichi (en-tête + godoc méthode) — pas de `doc.go` séparé (le commentaire package dans `webrtc.go` couvre tout)
- [x] 10.3 Changelog story 6.1 : voir Change Log plus bas
- [ ] 10.4 `architecture.md` : mise à jour du § « Séquence d'implémentation (révision Linux + TUN) » étape 7 pour marquer comme complétée (**reporté à la clôture d'Epic 6** — pas à chaque story)

### Task 11 — Validation build + full test suite (AC: #4)
- [x] 11.1 `go build ./...` (Windows + Linux) → vert
- [x] 11.2 `go vet ./...` (Windows + Linux) → propre
- [x] 11.3 `go test ./... -timeout 300s` → vert (zéro régression sur 35 packages, tolérance pour flaky tests réseau éventuels sur `leakcheck_test` avec marker `-short`)
- [x] 11.4 `go test -race` sur les packages touchés (`leakcheck`, `service`, `tunnel`, `relay`, `ipchandler`, `config`) → vert
- [ ] 11.5 `goreleaser release --snapshot --skip=publish` → artefacts générés, binaires signés (**différé à la validation CI — pas bloquant pour review**)
- [ ] 11.6 Déploiement smoke test sur 1 relais DE (**différé à la phase QA manuelle post-review** — nécessite accès SSH prod + client Windows en conditions réelles)

## Dev Notes

### Architecture — chemin STUN post-refacto

```
Scheduler (leakcheck) tick 10m
    │
    ▼
leakcheck.CheckSTUNLeak(ctx, "stun.l.google.com:19302")
    │
    ▼
net.DialUDP("udp", nil, stun.l.google.com:19302)
    │   (OS socket : conn.Write(BuildBindingRequest()))
    ▼
OS routing : dst=173.194.X.X → match default route → dev levoile0
    │
    ▼
internal/tun/device.go : ReadPacket() lit le paquet UDP brut
    │
    ▼
internal/tunnel/client.go : pump TX → stream HTTP/3 POST /tunnel
    │
    ▼
Relais : internal/relay/tunnel_handler.go
    │
    ▼
internal/relay/nat_table.go : alloc port NAT, dial UDP vers STUN target
    │
    ▼
Serveur STUN public : répond avec XOR-MAPPED-ADDRESS (= IP publique du relais)
    │
    ▼  (retour inverse par le même chemin)
leakcheck.ParseXORMappedAddress(resp) → net.IP
    │
    ▼
report.STUNIP = IP — prêt pour comparaison (Story 6.2)
```

**Note importante** : dans l'ancienne architecture (pré-Epic-2), l'OS routait le paquet UDP STUN via l'interface physique (wifi/ethernet), d'où la nécessité d'un relais applicatif `/stun-relay` pour le faire passer par le tunnel. Avec la capture L3 (Epic 2, Story 2.4), la route par défaut pointe sur `levoile0`, donc n'importe quel `net.DialUDP` est automatiquement capturé — aucun code applicatif spécifique STUN n'est nécessaire. C'est exactement le principe NFR32 du PRD : « capture L3 rend les fuites structurellement impossibles ».

### Source tree à toucher

**Refactorer** :
- [internal/leakcheck/webrtc.go](internal/leakcheck/webrtc.go) — simplifier `WebRTCLeakChecker` (Task 1)
- [internal/leakcheck/scheduler.go](internal/leakcheck/scheduler.go) — retirer `KillSwitchQuerier` (Task 2)
- [internal/service/service.go](internal/service/service.go) — wiring leak scheduler ligne ~1380, retrait `startSTUN`/`stopSTUN` ligne ~1955 (Task 6)
- [internal/config/config.go](internal/config/config.go) — `STUNConfig.LeakcheckInterval` + `Servers` (Task 8)

**Supprimer** :
- [internal/stun/interceptor.go](internal/stun/interceptor.go) + tests (Task 3)
- [internal/stun/relayer.go](internal/stun/relayer.go) + tests (Task 3)
- [internal/relay/stun_handler.go](internal/relay/stun_handler.go) + tests (Task 4)
- Méthode `SendSTUNRelay` dans [internal/tunnel/client.go](internal/tunnel/client.go#L341) (Task 5)
- `STUNActive`, `startSTUN`, `stopSTUN`, `setSTUNEnabled` dans [internal/service/service.go](internal/service/service.go#L680) (Task 6)
- Route `/stun-relay` dans [internal/relay/server.go:94](internal/relay/server.go#L94) (Task 4)
- IPC `ActionSTUNStatus` dans [internal/ipc/messages.go](internal/ipc/messages.go) + [internal/ipchandler/handler.go:92-94, 402-407](internal/ipchandler/handler.go) (Task 7)

**Conserver** (utilisés par ailleurs) :
- [internal/stun/parser.go](internal/stun/parser.go) + [internal/stun/stun.go](internal/stun/stun.go) — constantes + helpers parsing, indépendants
- [internal/leakcheck/xor_mapped.go](internal/leakcheck/xor_mapped.go) — parser XOR-MAPPED-ADDRESS (utilisé par `webrtc.go`)
- [internal/leakcheck/webrtc.go](internal/leakcheck/webrtc.go) `BuildBindingRequest()`, `stunTimeout`, `defaultSTUNServers`, `RunFullCheck` (sémantique refactorisée), `LeakResult`, `FullLeakReport`

### Contraintes d'implémentation

- **NFR9c (comparaison constante-time)** : inapplicable au txID STUN (non-secret, défense contre mismatch réseau, pas contre attaque timing). Garder `for i := 0; i < 12; i++` simple.
- **NFR22a (log privacy)** : ne JAMAIS logger l'IP STUN retournée au niveau INFO. Uniquement erreurs opérationnelles.
- **FR31 / FR32** : la story valide que la TUN est « étanche » — c'est un diagnostic, pas une protection active. La protection est structurelle (TUN + firewall).
- **Context cancellation** : `RunFullCheck` doit respecter `ctx.Done()` et interrompre proprement si le service s'arrête (ne pas attendre les 5s de timeout par serveur × 3).
- **Concurrency** : `NewPeriodicScheduler` protège son état via `mu`. Le scheduler ne doit pas être appelé concurrent à `Stop()` — le pattern `Start/Stop` est déjà correct.
- **Pattern kardianos/service** : aucune interaction directe avec `service.Service` — le scheduler est démarré depuis `Program.Start`, stoppé depuis `Program.Stop` via context parent cancellation.

### Testing standards (Story 1.1 → 5.9 établi)

- Tests table-driven pour parsing (`BuildBindingRequest`, `ParseXORMappedAddress`, `CheckSTUNLeak`)
- Mock UDP server écoutant `127.0.0.1:0` pour tests `RunFullCheck` (pattern `startMockSTUNServer` déjà en place, réutiliser)
- `go test -race` obligatoire sur `leakcheck` + `service`
- Coverage cible : ≥ 80% sur `internal/leakcheck/` (déjà à ~92% post-5.3, maintenir)
- Pas de test dépendant d'internet réel pour STUN — ci-cd doit rester hermétique

### Project Structure Notes

**Alignement parfait avec architecture.md §838-849** :

```
│   ├── leakcheck/                     # Simplifié — validation TUN uniquement
│   │   ├── webrtc.go                  # STUN Binding Request emitted via OS (passe TUN)
│   │   ├── webrtc_test.go
│   │   ├── xor_mapped.go              # XOR-MAPPED-ADDRESS parsing (conservé)
│   │   └── scheduler.go / scheduler_test.go
│   │
│   ├── stun/                          # Réduit — juste parsing
│   │   ├── stun.go                    # Constants + classification
│   │   ├── parser.go
│   │   └── parser_test.go
│   │   # Supprimés : interceptor.go, relayer.go (plus de relay STUN dédié)
```

**Aucune variance détectée** : la story réalise exactement ce que l'architecture prévoit.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 6.1](_bmad-output/planning-artifacts/epics.md#L1077-L1094)
- [Source: _bmad-output/planning-artifacts/architecture.md#leakcheck-stun](_bmad-output/planning-artifacts/architecture.md#L838-L849) — tree cible
- [Source: _bmad-output/planning-artifacts/architecture.md#sequence-revision](_bmad-output/planning-artifacts/architecture.md#L386-L398) — étape 7 : « Refactor internal/leakcheck/ — validation TUN au lieu de relay STUN. Retrait internal/stun/interceptor.go + relayer.go »
- [Source: _bmad-output/planning-artifacts/architecture.md#pattern-stun](_bmad-output/planning-artifacts/architecture.md#L1125-L1139) — diagramme du flux STUN post-refacto
- [Source: _bmad-output/planning-artifacts/prd.md#FR31-34](_bmad-output/planning-artifacts/prd.md#L483-L492)
- [Source: _bmad-output/planning-artifacts/prd.md#NFR22a](_bmad-output/planning-artifacts/prd.md#L553) — log privacy
- [Source: _bmad-output/implementation-artifacts/5-1-interception-et-parsing-des-paquets-stun.md](_bmad-output/implementation-artifacts/5-1-interception-et-parsing-des-paquets-stun.md) — référence inverse : ce que 5.1 a ajouté et que 6.1 supprime partiellement
- [Source: _bmad-output/implementation-artifacts/5-3-gestion-du-fallback-turn-et-validation-anti-fuite-webrtc.md](_bmad-output/implementation-artifacts/5-3-gestion-du-fallback-turn-et-validation-anti-fuite-webrtc.md) — référence : ancien wiring `WithSTUNRelay`
- [Source: internal/leakcheck/webrtc.go](internal/leakcheck/webrtc.go) — code source à refactorer
- [Source: internal/leakcheck/scheduler.go](internal/leakcheck/scheduler.go) — code source à simplifier
- [Source: internal/service/service.go#L1380-L1440](internal/service/service.go) — wiring leak scheduler actuel
- [Source: internal/service/service.go#L1950-L2100](internal/service/service.go) — `startSTUN/stopSTUN/setSTUNEnabled` à supprimer

## Previous Story Intelligence (5.9)

**Story 5.9 : Mode dégradé kill switch** — dernière story complétée (2026-04-18).

### Learnings importants de 5.9

1. **CSRF token pattern** : `/api/settings/killswitch` protégé par token CSRF per-process (`crypto/rand` 32 bytes), header `X-CSRF-Token`, validé `crypto/subtle.ConstantTimeCompare`. Frontend cache token via endpoint `/api/csrf-token`. → Pour 6.1 : **l'IPC `leak_check` et le scheduler interne** ne sont pas exposés via HTTP frontend, donc CSRF non requis pour cette story. Si on expose un `/api/leak-check-now` (déclenchement manuel), appliquer le pattern CSRF.
2. **`config.Mu` partagé** : 5.9 a introduit `config.Mu sync.Mutex` exporté pour synchroniser les writes TOML multi-writers. → Pour 6.1 : si on écrit `STUNConfig.LeakcheckInterval` depuis un endpoint utilisateur (peu probable en 6.1), utiliser `config.Mu`. Sinon, read-only au démarrage → rien à synchroniser.
3. **Pattern atomicité config-write + action** : 5.9 a établi que `persistFirewallEnabled` écrit TOML **avant** d'appeler l'action, avec rollback si l'action échoue. → Pour 6.1 : pas d'action transactionnelle TOML-first — le scheduler est un side-effect runtime, pas persisté.
4. **Éviter les goroutines orphelines** : 5.9 a corrigé `eventSlot` TTL 10s. → Pour 6.1 : `PeriodicScheduler` a déjà un lifecycle propre (`Start`/`Stop` + `done` channel). Ne pas ajouter de goroutines auxiliaires sans parent context.
5. **Fichier token machine-local** : 5.9 a ajouté `ctl.token` pour `levoile-ctl`. → Pour 6.1 : rien à ajouter (pas de CLI exposée).

### Code patterns établis (1.1 → 5.9)

- **Tests table-driven** systématiques pour parsing/classification
- **Mock server UDP** : pattern `startMockSTUNServer(t, responseIP)` dans `webrtc_test.go` — réutiliser tel quel
- **Contexte hiérarchique** : `ctx := context.Background() → Start(ctx) → runCheck(ctx) → WithTimeout(25s) → CheckSTUNLeak(ctx) → WithTimeout(5s)` — garder cette hiérarchie
- **Build tags par OS** pour TUN/firewall — sinon code pur Go cross-platform
- **`go test -race` obligatoire** avant merge
- **`go vet` + `govulncheck` + `gosec`** comme security gates (NFR22d/e/f)

### Points de vigilance

- Le fichier `internal/leakcheck/webrtc.go` contient actuellement une branche `if c.stunRelay != nil { ... } else { ... }` — la refacto doit **supprimer** les deux branches et garder uniquement le path direct (renommé). Attention à bien retirer tous les call sites pour éviter dead code.
- Les tests `webrtc_edge_test.go` contiennent probablement des cases pour `stunRelay` nil et non-nil — à réduire.
- La race `TestSTUNActive_AfterStop` notée dans la review 5.9 est **obsolète** post-refacto : le test disparaît avec la suppression de `STUNActive`.

## Git Intelligence

Derniers commits (5 plus récents) — indicatifs du contexte code actuel :

1. `996f7e3 feat: Epic 5 done — drop Wails/tray, move to webview+HTTP UI with ctl/watchdog/killswitch`
2. `ac795de feat: Epic 5 UI cross-platform — Linux webview + tray portage (stories 5.1–5.9)`
3. `f2f9021 feat: complete Epic 4 — découverte & failover multi-pays (stories 4.1–4.4)`
4. `16a275a fix(deploy): align install.sh/README/service with prod (signing.key + registry)`
5. `c2e1c0e feat: complete Epic 3 — relay stateless multi-VPS with tunnel IP & NAT`

**Insights pour 6.1** :

- Epic 5 (UI) et Epic 3 (relais stateless) sont récemment clôturés — la base de code relai `internal/relay/` est fraîchement refactorisée (handler `/tunnel` unifié ligne 94 ajouté, sans doute à côté du `/stun-relay` legacy). La suppression de `/stun-relay` s'intègre naturellement dans cette ligne d'évolution.
- Epic 2 (TUN + firewall + routing) est **done** : la capture L3 est en place, donc l'hypothèse AC1 « l'OS route STUN via TUN » est validée par les tests d'intégration Epic 2 (notamment `device_linux_test.go` + `service_tun_test.go`).
- Rien dans l'historique récent ne touche `internal/leakcheck/` — le dernier commit significatif sur ce module remonte à Epic 5.3 (ancien). Donc le refacto n'entrera pas en conflit avec du code en vol.
- La commande `git log --oneline --all -- internal/stun/` avant de supprimer les fichiers pour vérifier si un développeur a ajouté quelque chose récemment qu'on n'aurait pas vu — prudence avant `git rm`.

## Latest Tech Information

- **Go 1.22+** (projet sur Go 1.22 selon `go.mod`) : `context.WithoutCancel` disponible si besoin (inutile ici — on veut la propagation de cancel)
- **`net.DialUDP`** : comportement inchangé depuis Go 1.0. Le deadline via `conn.SetDeadline` reste la méthode canonique (alternative `ctx`-aware via `net.Dialer.DialContext` possible mais la sémantique deadline est équivalente)
- **RFC 5389 (STUN)** : inchangé. RFC 8489 (STUN bis, 2020) étend le format — non requis pour Binding Request simple. Nos serveurs cible (Google, Cloudflare) supportent les deux.
- **`quic-go`** (utilisé par `internal/tunnel/`) : dernière version stable v0.40.x, compatible Go 1.22. Rien à mettre à jour dans cette story (le tunnel client est inchangé).
- **`sync/atomic`** : `atomic.Bool`, `atomic.Int32` préférés à `int32` + `atomic.StoreInt32` (syntaxe plus claire). Déjà utilisés dans le projet.

## Project Context Reference

Voir `docs/project-context.md` si présent (aucune mention trouvée à la racine au moment de la rédaction — la story s'appuie sur `architecture.md` + `prd.md` + `epics.md` comme source de vérité). Le fichier `memory/reference_relay_servers.md` liste les 8 relais (DE/ES/GB/US, 2/pays) disponibles pour smoke tests post-déploiement (Task 11.6).

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m] (BMAD dev-story workflow, execution 2026-04-18)

### Debug Log References

- Race test transient failure initial sur `./internal/tunnel/` (CGO wintun extraction sous -race en parallèle avec autres packages) — résolu au second run en isolation. Non lié à la story 6.1.
- Tests `go test ./... -timeout 300s` : 34 packages verts, 0 régression.
- Tests `go test -race` sur packages touchés (`leakcheck`, `service`, `tunnel`, `relay`, `ipchandler`, `config`, `stun`) : verts.
- Triple run `go test ./internal/leakcheck/... -race -count=3` : vert (anti-flakiness UDP mock).

### Completion Notes List

- **Refacto architecturale majeure** : suppression complète du chemin applicatif STUN (interceptor ports 3478/5349 + relayer via `/stun-relay` + handler côté relais + méthode client `SendSTUNRelay`). Le trafic STUN transite désormais via `net.DialUDP` → OS → TUN `levoile0` → tunnel HTTP/3 → NAT relais → serveur STUN. La capture L3 (Epic 2) rend cette simplification possible.
- **`getPublicIP` simplifié** : boucle sur `defaultSTUNServers` (Google ×2 + Cloudflare), retourne la première réponse XOR-MAPPED-ADDRESS valide. `/verify` ne pouvait pas être utilisé (retourne `SessionToken`, pas d'IP). Story 6.2 raffinera en croisant avec l'IP du registry.
- **Scheduler allégé** : retrait de la dépendance `KillSwitchQuerier` — post-Epic-2 le kill switch firewall et la TUN coexistent, le check STUN doit s'exécuter même sous kill switch actif (c'est précisément ce qui valide la chaîne TUN). Condition de skip unique : `tunnelState != StateConnected`.
- **Config TOML** enrichie : `[stun] leakcheck_interval`, `[stun] servers`, `[stun] default_server` (override premier serveur — conservé pour rétro-compat).
- **IPC** : action `stun_status` + constantes associées retirées (plus d'interceptor à surveiller). Handler IPC `leak_check` refactorisé pour utiliser `net.DialUDP` via `leakcheck.NewWebRTCLeakChecker`.
- **Tests** : 5 nouveaux tests E2E (`TestCheckSTUNLeak_TransactionIDMismatch`, `TestRunFullCheck_AllServersSilent_Story61`, `TestRunFullCheck_FailoverAcrossServers`, `TestPeriodicScheduler_RunCheck_EmitsSTUNAndParsesResponse`, `TestPeriodicScheduler_FailoverAcrossServers_E2E`). 1 test ajouté dans `config_test.go` (`TestConfig_STUN_LeakcheckInterval_Roundtrip`). Suppression de `service_53_edge_test.go` entier (5 tests obsolètes), `TestHandle_STUNStatus_Inactive`, `TestClient_SendSTUNRelay*`, `TestProgram_STUNActive_*`, `TestProgram_StartStopSTUN`, `TestService_KillSwitch_BlocksSTUN`, `TestProgram_StopSTUN_NilSafe`, `TestNewPeriodicScheduler_PanicsOnNilKillSwitch`.
- **Documentation** : godoc `internal/leakcheck/webrtc.go` actualisé, commentaire d'en-tête `internal/relay/server.go` actualisé (`/stun-relay` retiré), README section « Validation anti-fuite STUN », `config.example.toml` section `[stun]` documentée.
- **Points reportés** : Task 10.4 (mise à jour `architecture.md` étape 7) → clôture Epic 6. Tasks 11.5/11.6 (goreleaser + smoke test DE) → phase QA post-review.

### File List

**Modifiés**

- `internal/leakcheck/webrtc.go` — Retrait `STUNRelayFunc`, `WithSTUNRelay`, champ `stunRelay`. Ajout `sendBindingRequest` (chemin unique `net.DialUDP`). Godoc package actualisé.
- `internal/leakcheck/scheduler.go` — Retrait `KillSwitchQuerier`, signature `NewPeriodicScheduler` passée à 5 args. Skip unique sur `tunnelState != Connected`.
- `internal/leakcheck/scheduler_test.go` — Adapté à la nouvelle signature, ajout `TestPeriodicScheduler_DoesNotSkipOnKillSwitchActive`, `TestNewPeriodicScheduler_PanicsOnNilChecker`, `TestNewPeriodicScheduler_PanicsOnZeroInterval`. Retrait des tests KillSwitch.
- `internal/tunnel/client.go` — Retrait méthode `SendSTUNRelay`, constante `stunRelayTimeout`, constante `maxSTUNResponseSize`.
- `internal/tunnel/client_test.go` — Retrait `TestClient_SendSTUNRelay*`, signature `startTestRelay` simplifiée (plus de `stunH`), import `strconv` retiré.
- `internal/tunnel/client_edge_test.go` — Callsites `startTestRelay` adaptés.
- `internal/relay/server.go` — Retrait champ `STUNHandler`, route `/stun-relay`, mention dans commentaire d'en-tête.
- `internal/relay/server_test.go` — Retrait wiring `STUNHandler` dans `setupTestServerWithCF`, retrait endpoint `/stun-relay` du test d'exhaustivité CF filtering.
- `cmd/relay/main.go` — Retrait `srv.STUNHandler = relay.NewSTUNHandler()`.
- `internal/service/service.go` — Retrait import `internal/stun`, champs `stunInterceptor`/`stunRelayer`/`stunMu`/`stunCancel`/`stunErrCh`, méthodes `STUNActive`/`startSTUN`/`setSTUNEnabled`/`stopSTUN`, type `tunnelStateAdapter`. Refactor bloc leak scheduler (5b) : `getPublicIP` simplifié via `net.DialUDP`, wiring `NewWebRTCLeakChecker` sans `WithSTUNRelay`, `NewPeriodicScheduler` 5 args, lecture interval depuis `config.STUN.LeakcheckInterval`. Kill switch hook simplifié (retrait `setSTUNEnabled`). Ajout `Config.STUNServers` + `Config.STUNLeakcheckInterval`.
- `internal/service/service_test.go` — Retrait `TestProgram_STUNActive_NilBeforeStart`, `TestProgram_StartStopSTUN`, `TestService_KillSwitch_BlocksSTUN`, `TestProgram_StopSTUN_NilSafe`.
- `internal/ipc/messages.go` — Retrait `ActionSTUNStatus`, `StatusSTUNActive`, `StatusSTUNInactive`.
- `internal/ipchandler/handler.go` — Retrait `case ipc.ActionSTUNStatus` + fonction `handleSTUNStatus`. Refactor `handleLeakCheck` pour utiliser `net.DialUDP` via `leakcheck.NewWebRTCLeakChecker` avec liste STUN + retrait dépendance `tc.SendSTUNRelay`.
- `internal/ipchandler/handler_test.go` — Retrait `testKSQuerier`, signature `newTestScheduler` adaptée (5 args), retrait `TestHandle_STUNStatus_Inactive`.
- `internal/config/config.go` — Enrichissement `STUNConfig` : champs `Servers []string`, `LeakcheckInterval string`. Godoc section actualisé (plus de « relay STUN »).
- `internal/config/config_test.go` — Ajout `TestConfig_STUN_LeakcheckInterval_Roundtrip` + assertions sur valeurs par défaut.
- `README.md` — Ajout section « Validation anti-fuite STUN (Story 6.1) ».
- `config.example.toml` — Ajout section `[stun]` documentée.

**Ajoutés**

- `internal/leakcheck/e2e_story6_1_test.go` — 5 tests E2E Story 6.1 (txID mismatch, 3-servers-down, failover 2-sur-3, scheduler+checker+dial+parse, scheduler failover).

**Supprimés**

- `internal/stun/interceptor.go`, `internal/stun/interceptor_test.go`, `internal/stun/interceptor_53_test.go`, `internal/stun/interceptor_edge_test.go`
- `internal/stun/relayer.go`, `internal/stun/relayer_test.go`, `internal/stun/relayer_53_test.go`, `internal/stun/relayer_edge_test.go`
- `internal/relay/stun_handler.go`, `internal/relay/stun_handler_test.go`, `internal/relay/stun_handler_edge_test.go`
- `internal/service/service_53_edge_test.go` (5 tests tous obsolètes post-refacto)

### Change Log

- 2026-04-18 : Implémentation complète Story 6.1 — Émission de STUN Binding Requests via la TUN. Refacto architecturale : suppression du chemin applicatif STUN (`/stun-relay` endpoint + `stun.Interceptor` + `stun.Relayer` + `Client.SendSTUNRelay` + `startSTUN/stopSTUN/setSTUNEnabled`). STUN Binding Requests émis via `net.DialUDP` → OS → TUN → relais NAT → serveur STUN. 3 serveurs failover (Google ×2 + Cloudflare). Intervalle config `[stun] leakcheck_interval` (défaut 10m). Scheduler allégé (retrait `KillSwitchQuerier`). 5 nouveaux tests E2E, 0 régression sur 34 packages. Couvre AC1-9.
- 2026-04-18 : Code review adversarial → 10 findings (1 HIGH, 3 MEDIUM, 6 LOW), tous corrigés (+1 helper `resolveSTUNServers`, +1 export `DefaultSTUNServers()`, wiring TOML→svc.Config complété). Assertions E2E failover renforcées. Voir « Senior Developer Review (AI) ».

## Senior Developer Review (AI)

**Review Date :** 2026-04-18
**Reviewer :** claude-opus-4-7[1m] (self-review, mode adversarial)
**Outcome :** Approve (after fixes — 10/10 findings résolus)

### Findings + Action Items (tous résolus)

- [x] **[HIGH] H1** : `cfg.STUN.LeakcheckInterval` et `cfg.STUN.Servers` non propagées vers `svc.Config` dans [cmd/client/main.go](cmd/client/main.go) → AC8 PARTIAL. **Fix** : ajout `stunServers` + `stunLeakcheckInterval` à `resolvedConfig`, parse `time.ParseDuration` avec erreur claire si invalide, propagation dans `svc.Config{STUNServers, STUNLeakcheckInterval}`.
- [x] **[MED] M1** : requêtes STUN dupliquées par cycle (4+ au lieu de 3) à cause de `getPublicIP` qui hitait STUN puis `RunFullCheck` qui re-hitait → comparaison tautologique. **Fix** : `getPublicIP` accepte maintenant `nil` dans `WebRTCLeakChecker` ; `RunFullCheck` saute la comparaison quand `c.getPublicIP == nil`. Service + handler IPC passent désormais `nil` → exactement 3 requêtes STUN par cycle. Story 6.2 injectera l'IP du registry.
- [x] **[MED] M2** : `TestPeriodicScheduler_FailoverAcrossServers_E2E` asserait seulement `successful >= 1`. **Fix** : assertions renforcées — `Results[0].Error != ""` (silent #1), `Results[1..2].Error == ""` + `IP.Equal(vpsIP)` (answering #2, #3), `len(Results) == 3`.
- [x] **[MED] M3** : champs `svc.Config.STUNServers` / `STUNLeakcheckInterval` zombies. **Fix** : conséquence de H1 — propagation effective depuis `cmd/client/main.go`.
- [x] **[LOW] L1** : dead code `stunServers = nil` quand déjà nil. **Fix** : bloc entièrement supprimé via le refacto `resolveSTUNServers`.
- [x] **[LOW] L2** : liste STUN hard-codée en double. **Fix** : export `leakcheck.DefaultSTUNServers()` + helper `service.resolveSTUNServers(servers, defaultServer)` qui centralise la logique de priorité (full override > first-entry override > built-in).
- [x] **[LOW] L3** : `_ = tc` code smell dans `handleLeakCheck`. **Fix** : branche simplifiée — `tc` sert uniquement à `State().Get() != Connected`, plus utilisé ensuite, variable conservée implicitement via l'early return.
- [x] **[LOW] L4** : `contains`/`indexOf` custom dans `e2e_story6_1_test.go`. **Fix** : remplacés par `strings.Contains` (import `strings` ajouté), 14 lignes en moins.
- [x] **[LOW] L5** : claim story inexacte « `stun.DefaultPort`/`DefaultTLSPort` retenues ». **Fix** : Task 3.3 reformulée pour refléter que ces constantes vivaient dans l'`interceptor.go` supprimé et n'ont plus de consommateur.
- [x] **[LOW] L6** : bruit git CRLF sur 3 fichiers tunnel (`reconnect_test.go`, `reconnect_edge_test.go`, `state_test.go`). **Fix** : `git checkout --` sur les 3 fichiers, plus d'apparition dans `git diff --name-only`.

### Tests post-fix

- `go build ./...` → vert (34 packages compilables)
- `go vet ./...` → propre
- `go test ./... -timeout 300s` → **PASS** sur 34 packages, 0 régression
- `go test -race` sur `leakcheck` + `service` + `ipchandler` + `config` → **PASS**
- Assertions E2E failover : Results[0].Error peuplé (silent), Results[1..2].IP == vpsIP ✅

### Fichiers modifiés post-review

- `cmd/client/main.go` — `resolvedConfig.stunServers` + `stunLeakcheckInterval`, parse `time.ParseDuration` avec erreur claire, propagation dans `svc.Config{}`.
- `internal/leakcheck/webrtc.go` — export `DefaultSTUNServers() []string`, `RunFullCheck` accepte `getPublicIP == nil` (skip comparison + report.Status reste pass + STUNIP peuplé depuis premier STUN succès).
- `internal/service/service.go` — nouvelle fonction `resolveSTUNServers`, wiring leak scheduler simplifié (`NewWebRTCLeakChecker(nil)` + `WithSTUNServers(resolveSTUNServers(...))`), ~60 lignes en moins.
- `internal/ipchandler/handler.go` — `handleLeakCheck` simplifié (14 lignes → 4), retrait import `net`, utilise `leakcheck.DefaultSTUNServers()`.
- `internal/leakcheck/e2e_story6_1_test.go` — `strings.Contains` au lieu de helpers custom, assertions E2E failover renforcées.

## Decisions (prises en rédaction de story — pas d'ambiguïté pour le dev)

1. **`getPublicIP` = STUN direct, premier réussi.** `/verify` retourne un `SessionToken` uniquement (vérifié dans [internal/tunnel/client.go:57](internal/tunnel/client.go#L57)) — il ne peut pas servir de source d'IP. La boucle `getPublicIP` tente `defaultSTUNServers[0..n]` et retourne la première réponse valide. La comparaison 6.2 se fera avec l'IP du relais actif issue du registry (pas `/verify`).
2. **`STUNConfig.DefaultServer` conservé.** Override optionnel du premier serveur de la liste. Rétro-compatibilité avec les configs existantes. Pas de migration TOML nécessaire.
3. **Tests d'intégration TUN réelle : reportés au smoke test manuel (Task 11.6).** Unit tests : mock UDP `127.0.0.1` suffisent pour valider parsing + failover + transaction ID. Validation bout-en-bout « le paquet passe bien par levoile0 » = smoke test sur un relai DE via `scp` + `journalctl` (cf. `memory/reference_relay_servers.md`). Task 9.1 est **supprimée** du plan, Task 9.2 (mock unit) devient Task 9.
4. **`internal/stun/parser.go` + `stun.go` conservés.** Aucun consommateur restant après la refacto (seul `internal/service/service.go` importait pour `NewInterceptor`/`NewRelayer`, qui disparaissent). La library reste disponible pour usages futurs (inspection TUN, diagnostic). Suppression reportée si jamais inutilisée dans 2 epics.
5. **Package `leakcheck` gardé tel quel.** La sémantique « vérification anti-fuite » reste pertinente.

## Open Questions for Dev

_(aucune — les décisions ci-dessus couvrent l'intégralité du design. Si l'agent dev rencontre un obstacle imprévu, documenter dans Completion Notes et ajuster.)_
