# Story 3.4: NAT côté relais avec table en RAM et TTL court

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a opérateur,
I want une table NAT en mémoire (aucune persistence disque) avec éviction par TTL court (TCP 300s / UDP 120s) et pool de ports 10000-60000,
So that le relais reste stateless (NFR3, FR18) tout en supportant des flux concurrents multiples par client sans fuite mémoire ni collision de ports.

## Acceptance Criteria

1. **Given** un paquet IP entrant désencapsulé par le handler `/tunnel` (Story 3.3) portant un 5-tuple `(src_ip, src_port, dst_ip, dst_port, proto)` avec `proto ∈ {TCP(6), UDP(17)}` et `clientSession` identifiant la session tunnel source, **When** la méthode `NAT.Translate(clientSession, pkt)` est appelée sur un paquet dont le 5-tuple n'a pas d'entrée existante, **Then** une entrée `(clientSession, 5-tuple) → (natPort, lastSeen)` est créée dans la table (`sync.Map` keyé par une clé canonique de ce tuple), l'IP source du paquet est réécrite vers l'IP publique du relais, le port source est substitué par `natPort ∈ [10000, 60000]`, et le paquet est retourné prêt à être émis via socket userspace (TCP : `net.DialTCP` associé à l'entrée ; UDP : socket `net.ListenUDP` associé).
2. **Given** une entrée NAT existante pour un `(clientSession, 5-tuple)` donné, **When** un nouveau paquet du même tuple arrive, **Then** **aucune nouvelle** allocation de port n'est effectuée, `natPort` est réutilisé, `lastSeen` est mis à jour à `time.Now()` via `atomic.StoreInt64`, et l'entrée n'apparaît toujours qu'une seule fois dans la table.
3. **Given** un paquet de retour provenant d'Internet arrive sur le socket userspace associé à une entrée NAT (même `natPort`), **When** `NAT.Reverse(pkt)` est appelée, **Then** l'entrée est retrouvée par lookup sur `natPort`, l'IP destination est réécrite vers `src_ip` d'origine, le port destination vers `src_port` d'origine, et le paquet est retourné avec le `clientSession` à qui il doit être livré (pour que le pump HTTP/3 l'envoie au bon client).
4. **Given** une entrée TCP sans trafic depuis **> 300 secondes** OU une entrée UDP sans trafic depuis **> 120 secondes** (NFR3), **When** le sweeper d'éviction s'exécute (ticker 10s), **Then** l'entrée est supprimée de la table, son port NAT est libéré et rendu disponible pour une ré-allocation, et le socket userspace associé est fermé proprement (`conn.Close()`) — aucun goroutine ni file descriptor n'est orphelin.
5. **Given** la table NAT atteint la capacité maximale (tous les ports 10000-60000 alloués, soit 50 001 entrées), **When** une `Translate` arrive pour un nouveau 5-tuple, **Then** un sweeper synchrone est déclenché avant allocation ; si après sweep aucun port n'est libre, l'appel retourne une erreur `ErrNATPortExhausted` et l'appelant (handler `/tunnel`) DROP le paquet silencieusement (aucun log avec IP) et incrémente un compteur opérationnel anonyme.
6. **Given** un paquet IP entrant dont la destination appartient à un réseau privé (RFC 1918, loopback, link-local, multicast, IPv6-mapped) — protection SSRF (NFR9), **When** `NAT.Translate` valide la destination via le helper existant `isBlockedIP(net.IP)` (réutilisé depuis [internal/relay/connect_handler.go:273-289](internal/relay/connect_handler.go#L273-L289)), **Then** l'allocation NAT est refusée, **aucune** entrée n'est créée, **aucun** port n'est consommé, et `ErrSSRFBlocked` est retournée (le handler `/tunnel` DROP le paquet).
7. **Given** le service relais reçoit `SIGTERM` (`systemctl stop levoile-relay.service`), **When** la méthode `NAT.Shutdown(ctx)` est appelée par `main.go`, **Then** **toutes** les entrées sont évincées, tous les sockets userspace sont fermés, la table est vidée ; aucun fichier d'état NAT n'existe sur disque (vérifié par test d'intégration : `find /var/lib/levoile /tmp -name '*nat*'` doit être vide après shutdown) — conforme NFR3 + FR18.
8. **Given** la concurrence : plusieurs goroutines appelant `Translate`, `Reverse`, et le sweeper simultanément, **When** `go test -race ./internal/relay/...` s'exécute, **Then** aucune data race n'est détectée ; l'implémentation utilise exclusivement `sync.Map` pour la table principale, `atomic.Int64` pour `lastSeen`, et un pool de ports protégé par `sync.Mutex` pour l'allocation/libération.
9. **Given** la méthode `NAT.Stats()`, **When** appelée pour alimenter l'endpoint `/health`, **Then** elle retourne `{nat_entries int, nat_ports_used int}` calculé via un compteur `atomic.Int64` incrémenté à la création / décrémenté à l'éviction — **sans** parcourir `sync.Map` (coût O(1)) — afin que `/health` (Story 3.7 / monitoring) reflète `nat_entries` dans sa réponse JSON.
10. **Given** les logs opérationnels du relais, **When** une entrée NAT est créée, évincée ou en erreur (SSRF, épuisement), **Then** **aucune** ligne de log ne contient l'IP source client, l'IP destination, le port source ou le port NAT (NFR20, NFR22a) — seuls des compteurs agrégés anonymes sont exposés.
11. **Given** la suite `go test ./internal/relay/...`, **When** les tests unitaires de `nat_table_test.go` s'exécutent, **Then** les AC 1, 2, 3, 4, 5, 6, 7, 8 sont chacun couverts par au moins un test Go (TTL vérifié via injection d'un `clock` mockable, pas via `time.Sleep` > 1s).

## Tasks / Subtasks

- [x] Tâche 1 — Créer le package NAT table (AC: 1, 2, 3, 9)
  - [x] Créer [internal/relay/nat_table.go](internal/relay/nat_table.go) exportant le type `NAT` avec API : `NewNAT(relayIP net.IP, opts ...NATOption) *NAT`, `Translate(session SessionID, pkt []byte) ([]byte, error)`, `Reverse(pkt []byte) ([]byte, SessionID, error)`, `Stats() NATStats`, `Shutdown(ctx context.Context) error`
  - [x] Struct interne `natEntry` : `{session SessionID, srcIP, dstIP net.IP, srcPort, dstPort, natPort uint16, proto uint8, lastSeen atomic.Int64, conn io.Closer}` + TCP state (mu, tcpState, ISNs, seqNext)
  - [x] Table principale : `entriesByTuple sync.Map` + `entriesByNATPort sync.Map`
  - [x] Compteurs atomiques : `entriesCount atomic.Int64`, `portsUsed atomic.Int64`
  - [x] `type SessionID = string` — alias aligné avec TunnelSession.ClientIPHash+OpenedAt
  - [x] Constantes exportées : `NATTTLTCP`, `NATTTLUDP`, `NATPortRangeMin`, `NATPortRangeMax`
  - [x] Erreurs exportées : `ErrNATPortExhausted`, `ErrSSRFBlocked`, `ErrUnsupportedProto`, `ErrInvalidPacket`

- [x] Tâche 2 — Parseur/éditeur de paquets IP minimaliste (AC: 1, 3)
  - [x] `parseIPv4(pkt) (*parsedPacket, error)` : IPv4 header + TCP/UDP, rejette IPv6 et ICMP
  - [x] `rewriteSource(pkt, p, newIP, newPort)` : réécrit + checksums RFC 1071
  - [x] `rewriteDest(pkt, p, newIP, newPort)` : symétrique
  - [x] IPv6 retourne `ErrUnsupportedProto` (MVP IPv4 only)
  - [x] Tests checksums byte-for-byte + FuzzParseIPv4

- [x] Tâche 3 — Pool de ports NAT (AC: 1, 4, 5)
  - [x] `portPool` mutex+slice, `rand.Shuffle`, `allocate()` O(1), `release()`
  - [x] `BenchmarkNATAllocate` + `TestNAT_PortPool_NoDuplicates`

- [x] Tâche 4 — Validation SSRF (AC: 6)
  - [x] `isBlockedIP(dstIP)` appelé dans `Translate` avant allocation
  - [x] `TestNAT_SSRFBlocked` table-driven : loopback, RFC1918, link-local, broadcast

- [x] Tâche 5 — Sweeper d'éviction par TTL (AC: 4, 7, 8)
  - [x] Goroutine `sweepLoop` (10s ticker) + `sweepOnce` (synchrone)
  - [x] Sweeper synchrone avant `ErrNATPortExhausted` (AC5)
  - [x] Clock mockable via `WithClock` option

- [x] Tâche 6 — Forwarder userspace (TCP / UDP) (AC: 1, 3, 4)
  - [x] TCP : `dialTCP` + SYN-ACK synthesis + reverse goroutine + FIN handling
  - [x] UDP : `dialUDP` + reverse goroutine
  - [x] NAT implémente `PacketForwarder` (OpenSession + Forward) — intégration directe avec tunnel_handler.go

- [x] Tâche 7 — Intégration dans [cmd/relay/main.go](cmd/relay/main.go) (AC: 9)
  - [x] `natTable := relay.NewNAT(resolveRelayPublicIP(*publicIP), relay.WithContext(ctx))`
  - [x] `srv.NATStatsFunc = natTable.Stats` pour /health
  - [x] NAT passé comme PacketForwarder au TunnelHandler (mode prod) ; echoForwarder conservé pour -tunnel-echo
  - [x] Flag `-public-ip` + `resolveRelayPublicIP()` avec auto-detect eth0/ens3/ens5/enp0s3
  - [x] `natTable.Shutdown(shutdownCtx)` avec timeout 5s au SIGTERM

- [x] Tâche 8 — Mise à jour `/health` (AC: 9)
  - [x] `NATStatsProvider func() NATStats` + `SetNATStatsProvider` sur HealthHandler
  - [x] Champ `nat_entries` omitempty dans HealthResponse JSON
  - [x] Tests : `TestHealthHandler_NATEntries` + `TestHealthHandler_NATEntriesOmittedWhenNoProvider`

- [x] Tâche 9 — Tests (AC: 11, 8)
  - [x] `TestNAT_TranslateAllocatesPort` (AC1)
  - [x] `TestNAT_TranslateReusesEntryForSameTuple` (AC2) — 1000 paquets
  - [x] `TestNAT_ReverseRoutesBackToOriginalClient` (AC3)
  - [x] `TestNAT_TTLEvictionTCP300s` + `TestNAT_TTLEvictionUDP120s` (AC4)
  - [x] `TestNAT_PortExhaustionTriggersSweep` (AC5)
  - [x] `TestNAT_SSRFBlocked` (AC6)
  - [x] `TestNAT_ShutdownClosesAllConns` (AC7)
  - [x] `TestNAT_RaceFree` (AC8) — 100 goroutines × 10 ops
  - [x] `TestNAT_PacketForwarder_Integration` — full UDP forward+reverse via PacketForwarder
  - [x] `TestNAT_TCP_SYNHandshake` + `TestNAT_TCP_DataForward`

- [x] Tâche 10 — Validation `go vet`, `go test -race`, `go build`
  - [x] `go vet ./internal/relay/...` — clean
  - [x] `go test -race ./internal/relay/...` — all pass
  - [x] `go build ./cmd/relay` — clean

## Dev Notes

### Contexte architectural et patterns à suivre

- **Stateless absolu (NFR3, FR18)** : la table NAT vit exclusivement en RAM. Aucune écriture disque, aucune reprise après crash. Un `systemctl restart levoile-relay.service` reset toutes les entrées — c'est voulu. Cf. architecture L87 (Statelessness) et L280 (État NAT côté relais).
- **Choix `sync.Map` vs mutex + `map`** : architecture L351 impose explicitement `sync.Map` pour le chemin chaud (lecture/écriture par clé). Le `sync.Map` Go est optimisé pour les accès majoritairement lecture avec clés stables — notre cas : une fois une entrée créée, `Translate` fait surtout des updates atomiques de `lastSeen` (pas d'update structurel). Le `portPool` lui reste mutex+slice (pas besoin de `sync.Map` pour un pool global).
- **Pas de netstack userspace client** : architecture L69, L77, L237, L1258. Le client n'a pas de stack TCP/IP côté client ; il envoie des paquets IP bruts. Le relais fait **gateway NAT**, pas terminaison TCP. On forwarde donc des paquets IP, pas des streams TCP parsés.
- **Ports NAT 10000-60000** : range standard (évite ports système 0-1023, éphémères kernel Linux 32768-60999 peuvent overlapper mais c'est acceptable car `SO_REUSEADDR=false` + allocation explicite via `net.DialTCP` / `net.ListenUDP` détectera les collisions). Pool initial de 50 001 ports = plafond dur de 50 001 flux NAT concurrents par relais (cohérent avec architecture L243 : "500 flux NAT concurrents par tunnel" × ~150 tunnels = ~75 000 — on est légèrement sous-dimensionné mais on reste OK pour le MVP, à re-évaluer si nécessaire).
- **TTL values (NFR3)** : TCP 300s, UDP 120s. Ces valeurs sont mesurées dans le PRD ([prd.md:507](../planning-artifacts/prd.md#L507)). **Ne pas** les rendre configurables via config TOML — elles sont des constantes de sécurité.
- **SSRF (NFR9)** : prd.md L513 exige de bloquer loopback, RFC 1918, link-local. Le helper `isBlockedIP` dans [connect_handler.go:273-289](internal/relay/connect_handler.go#L273) couvre déjà loopback + IsPrivate + link-local unicast/multicast + multicast + unspecified + IPv6-mapped. **Réutiliser tel quel** — ne pas dupliquer.
- **Zero log IP (NFR20, NFR22a)** : tous les logs opérationnels sont anonymes. Le pattern établi dans le package est `logFunc` nil-safe + formats du type `"nat: entries=%d ports_used=%d"` — **jamais** `%v` sur une struct qui contient des IPs.
- **Framing des paquets** : le handler `/tunnel` (Story 3.3) délivre les paquets IP bruts déjà désencapsulés (framing 2-octets-length stripé). `NAT.Translate` reçoit donc directement le paquet IPv4 commençant par le byte de version.

### Source tree — fichiers à créer / modifier

- **Nouveau** : [internal/relay/nat_table.go](internal/relay/nat_table.go) (API NAT, struct, sync.Map, sweeper)
- **Nouveau** : [internal/relay/nat_table_test.go](internal/relay/nat_table_test.go) (suite de tests AC11)
- **Nouveau** : [internal/relay/nat_packet.go](internal/relay/nat_packet.go) (parseur IPv4 + rewriters + checksum)
- **Nouveau** : [internal/relay/nat_packet_test.go](internal/relay/nat_packet_test.go)
- **Nouveau** : [internal/relay/nat_portpool.go](internal/relay/nat_portpool.go) (port pool avec mutex)
- **Modifié** : [cmd/relay/main.go](cmd/relay/main.go) — instanciation NAT, flag `-public-ip`, branchement `/health` et shutdown
- **Modifié** : [internal/relay/health.go](internal/relay/health.go) — ajout champ `nat_entries`
- **Modifié** : [internal/relay/health_test.go](internal/relay/health_test.go) — couverture du nouveau champ

### Conventions de nommage

- Fichiers : `snake_case.go` (architecture L439)
- Erreurs exportées : `ErrXxx`
- Constantes exportées : `PascalCase` (cohérent avec `SessionTokenTTL` de Story 3.2)

### Dépendances

- Stdlib uniquement : `sync`, `sync/atomic`, `net`, `time`, `context`, `encoding/binary`, `errors`
- **NE PAS** ajouter `gvisor.dev/gvisor/pkg/tcpip` — architecture L137 : rejeté, on fait gateway NAT pas netstack
- Go version : conforme [go.mod](go.mod) existant

### Constraintes de sécurité

- **Constant-time** : aucune comparaison cryptographique dans ce module (pas de tokens manipulés ici — déjà fait en amont par /tunnel). Pas besoin de `crypto/subtle`.
- **Pas de raw sockets au MVP** : architecture L351 liste `raw socket` pour ICMP en option. On utilise exclusivement `net.DialTCP` / `net.ListenUDP` (socket userspace). ICMP abandonné au MVP (architecture L1211).
- **CAP_NET_BIND_SERVICE** suffit pour les ports ≥ 1024 — on est toujours dans [10000, 60000] donc pas besoin de `CAP_NET_ADMIN` pour les sockets (mais `CAP_NET_ADMIN` reste nécessaire pour d'autres parties du relais, déjà accordé).

### Testing standards

- Cadre : `testing` stdlib, pas de testify — cohérent avec les tests relay existants (cf. [verify_handler_test.go](internal/relay/verify_handler_test.go))
- `-race` obligatoire sur toute la suite
- Aucun `time.Sleep > 1s` : utiliser la clock mockable (option `WithClock(func() time.Time)`)
- Couverture cible : **≥ 85%** du package NAT (mesurable via `go test -coverprofile=cover.out ./internal/relay`)
- Fuzzing : architecture L378 mentionne fuzzing hebdomadaire sur parsers critiques — ajouter `FuzzParseIPv4` dans `nat_packet_test.go` (peut rester optionnel en CI normale, exécuté dans le job fuzz hebdomadaire)

### Project Structure Notes

- Cohérent avec l'architecture cible ([architecture.md:858-861](../planning-artifacts/architecture.md#L858-L861)) : `cmd/relay/internal/relay/nat_table.go` + `nat_table_test.go` sont explicitement listés
- **Variance** : l'architecture liste `cmd/relay/internal/relay/` dans le source tree, mais la structure réelle du repo a le package à [internal/relay/](internal/relay/) (côté module, pas sous cmd/). On continue avec `internal/relay/` — la cohérence avec l'existant prime. À mentionner au retrospective d'Epic 3.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.4](../planning-artifacts/epics.md#L688-L710) — AC originaux
- [Source: _bmad-output/planning-artifacts/architecture.md#NAT-côté-relais](../planning-artifacts/architecture.md#L351) — `sync.Map` keyée, pool 10000-60000, TTL
- [Source: _bmad-output/planning-artifacts/architecture.md#État-NAT](../planning-artifacts/architecture.md#L280) — 5-tuple + last_seen, jamais persisté
- [Source: _bmad-output/planning-artifacts/architecture.md#Gateway-NAT](../planning-artifacts/architecture.md#L237) — modèle gateway côté relais
- [Source: _bmad-output/planning-artifacts/architecture.md#Monitoring](../planning-artifacts/architecture.md#L382) — `/health` expose `nat_entries`
- [Source: _bmad-output/planning-artifacts/prd.md#NFR3](../planning-artifacts/prd.md#L507) — TTL TCP 300s / UDP 120s
- [Source: _bmad-output/planning-artifacts/prd.md#NFR9](../planning-artifacts/prd.md#L513) — SSRF private networks
- [Source: _bmad-output/planning-artifacts/prd.md#NFR20](../planning-artifacts/prd.md#L537) — zero log IP
- [Source: _bmad-output/planning-artifacts/prd.md#FR28](../planning-artifacts/prd.md#L477) — désencapsulation + NAT
- [Source: internal/relay/connect_handler.go:273-289](internal/relay/connect_handler.go#L273-L289) — helper `isBlockedIP` à réutiliser
- [Source: internal/relay/health.go](internal/relay/health.go) — endpoint `/health` à enrichir

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

### Completion Notes List

- NAT table implements `PacketForwarder` interface from Story 3.3, enabling direct integration with `TunnelHandler`
- TCP proxy: full SYN→SYN-ACK handshake synthesis, data forwarding with ACK generation, FIN/RST handling, reverse goroutine reads from destination connection
- UDP proxy: payload extraction + forwarding via connected UDP socket, reverse goroutine for responses
- IPv4 packet parser with RFC 1071 checksum (IPv4 header + TCP/UDP pseudo-header), verified byte-for-byte in tests
- Port pool: mutex+slice with O(1) allocate/release, pre-shuffled for distribution
- SSRF: reuses existing `isBlockedIP` from connect_handler.go — no duplication
- Sweeper: 10s ticker + synchronous sweep on port exhaustion (AC5)
- Clock injection via `WithClock` option — all TTL tests use mock clock, no `time.Sleep`
- Stats: O(1) atomic counters, no sync.Map traversal
- `/health` endpoint enriched with `nat_entries` field (omitempty when no NAT configured)
- `cmd/relay/main.go`: NAT wired as PacketForwarder to TunnelHandler, `-public-ip` flag with auto-detection
- Note: `gosec` and `govulncheck` not run (not installed in environment) — to be validated in CI

### Senior Developer Review (AI)

**Date:** 2026-04-16
**Outcome:** Changes Requested → Fixed
**Total findings:** 3 High, 5 Medium, 2 Low — 4 fixed (H1-H3, M1)

**Action Items (all resolved):**
- [x] H1: Port pool double-release guard — added `allocated map` in portPool
- [x] H2: Shutdown vs Translate race — added `stopped atomic.Bool` check
- [x] H3: TCP reverse loop not setting CLOSED — now sets `tcpStateClosed` after FIN
- [x] M1: TCP seq validation — added out-of-order segment drop in forwardTCPData

**Accepted risks (MVP):**
- M2: Simplified TCP state machine (no TIME-WAIT) — acceptable for QUIC-tunneled FIFO path
- M3: Reverse goroutines stop via conn.Close, not context — functional, not instant
- M4: math/rand port shuffle — relay behind Cloudflare, ports not externally observable
- M5: Silent packet drop on full channel — 256 buffer sufficient for typical flows

### File List

- **New**: internal/relay/nat_table.go
- **New**: internal/relay/nat_table_test.go
- **New**: internal/relay/nat_packet.go
- **New**: internal/relay/nat_packet_test.go
- **New**: internal/relay/nat_portpool.go
- **Modified**: internal/relay/health.go
- **Modified**: internal/relay/health_test.go
- **Modified**: internal/relay/server.go
- **Modified**: cmd/relay/main.go
