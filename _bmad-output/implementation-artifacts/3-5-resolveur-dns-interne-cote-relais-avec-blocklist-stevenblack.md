# Story 3.5: Résolveur DNS interne côté relais avec blocklist StevenBlack

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a opérateur de relais,
I want que mon relais résolve en interne les requêtes DNS (UDP/TCP 53) capturées par la TUN côté client avec validation DNSSEC et application d'une blocklist StevenBlack/hosts (chargée en mémoire, refresh 24h), retournant NXDOMAIN sur domaines bloqués et SERVFAIL en cas d'échec amont,
So that les clients bénéficient d'une protection anti-malware et anti-tracking uniforme sans que mon relais ne logge aucun nom de domaine résolu (NFR22a) et sans dépendre d'un proxy DNS local côté client (supprimé par la révision 2026-04-15).

## Acceptance Criteria

1. **Given** un paquet UDP port 53 OU TCP port 53 arrivant via le handler `/tunnel` (Story 3.3) au relais, **When** le dispatcher IP détecte que c'est une requête DNS avant d'atteindre le NAT, **Then** la requête est détournée vers `internal/relay/dns_resolver.go` au lieu d'être NAT-forwardée, et la réponse DNS est ré-encapsulée dans un paquet UDP/TCP avec source = IP+port du serveur DNS attendu (ex: `1.1.1.1:53` côté client) pour que la stack cliente reconnaisse la réponse.

2. **Given** une requête DNS wire-format valide (query unique) parvient au resolver interne, **When** `Resolve(ctx, query)` est appelé, **Then** la résolution est faite via `https://1.1.1.1/dns-query` (Cloudflare, primaire, DoH application/dns-message) avec failover automatique vers `https://9.9.9.9/dns-query` (Quad9) après erreur réseau OU réponse non-DNSSEC-validable OU timeout upstream (timeout primaire 2s, timeout fallback 2s, contexte global ≤ 3s pour respecter NFR sur latence DNS).

3. **Given** une réponse upstream arrive, **When** le resolver traite la réponse, **Then** la validation DNSSEC est exécutée (NFR9f) : les flags AD (Authenticated Data) et CD (Checking Disabled) sont vérifiés ; si la réponse upstream ne porte pas AD=1 pour une zone DNSSEC-signée (détection via présence de RRSIG dans la requête DO=1 ou via ce que l'upstream renvoie), le resolver bascule sur Quad9 ; si les deux upstreams échouent la validation, la réponse au client est SERVFAIL (rcode=2) et un compteur `dns_dnssec_failures_total` est incrémenté **sans** nom de domaine.

4. **Given** la blocklist StevenBlack est chargée en mémoire sous forme `map[string]struct{}` (clé = FQDN lowercase sans point final), **When** la requête arrive, **Then** le domaine demandé est cherché dans la blocklist avant tout appel upstream ; si présent, la réponse est un paquet DNS NXDOMAIN (rcode=3, ANCOUNT=0) synthétisé localement et aucun appel réseau upstream n'est émis (FR8b). La correspondance doit couvrir aussi les sous-domaines : lookup en remontant les labels (`foo.bar.baz.com` → essai `foo.bar.baz.com`, `bar.baz.com`, `baz.com`).

5. **Given** la blocklist est initialisée au démarrage du binaire `cmd/relay`, **When** l'initialisation démarre, **Then** le loader télécharge `https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts` (liste unifiée standard) via `http.Client` avec timeout 30s, parse les lignes `0.0.0.0 domain.tld` (ignore commentaires `#` et lignes vides), et peuple un `*Blocklist` atomique (swap pointeur via `atomic.Pointer[map[string]struct{}]`). Si le téléchargement échoue, le relais démarre quand même avec blocklist vide et log opérationnel (sans interrompre le service).

6. **Given** le relais tourne depuis > 24h, **When** le refresh timer (`time.NewTicker(24 * time.Hour)`) se déclenche, **Then** un nouveau téléchargement est tenté ; en cas de succès la map est swappée atomiquement (lecteurs concurrents non bloqués) ; en cas d'échec l'ancienne map est conservée et un log opérationnel est émis (sans arrêter le ticker).

7. **Given** n'importe quel chemin d'exécution (résolution réussie, NXDOMAIN blocklist, SERVFAIL upstream, SERVFAIL DNSSEC, timeout), **When** le resolver exécute quoi que ce soit, **Then** aucune ligne de log ne contient de nom de domaine (NFR22a) : les logs opérationnels n'exposent que des compteurs (`dns_queries_total`, `dns_blocked_total`, `dns_upstream_failures_total`, `dns_dnssec_failures_total`) — vérifiable par grep sur les sources et les tests. Les tests doivent vérifier que `bytes.Buffer` capturant `log.SetOutput` ne contient AUCUN nom de domaine après un cycle de requêtes.

8. **Given** les tests Go `go test ./internal/relay/...`, **When** la suite est exécutée, **Then** `dns_resolver_test.go` et `blocklist_test.go` couvrent : (a) résolution nominale avec upstream mock, (b) bascule Cloudflare → Quad9 sur timeout, (c) NXDOMAIN sur domaine blocklisté, (d) matching sous-domaines blocklist, (e) SERVFAIL sur échec double upstream, (f) swap atomique de la blocklist sans race (`go test -race`), (g) parsing StevenBlack avec lignes commentées / malformées / 0.0.0.0 et 127.0.0.1, (h) aucun log contenant de FQDN. Aucun test ne doit accéder au réseau réel (tous les upstreams sont mockés via `httptest.Server`).

## Tasks / Subtasks

- [ ] Tâche 1 — Créer le module blocklist (AC: 4, 5, 6, 7)
  - [ ] Créer [internal/relay/blocklist.go](internal/relay/blocklist.go) avec :
    - Type `Blocklist` encapsulant `entries *atomic.Pointer[map[string]struct{}]` + `upstreamURL string` + `client *http.Client` + `refreshInterval time.Duration`
    - Constructeur `NewBlocklist(url string, client *http.Client, refreshInterval time.Duration) *Blocklist` (client nil → default 30s timeout ; refresh 0 → 24h par défaut)
    - Méthode `Load(ctx context.Context) error` : télécharge, parse, swap atomiquement. Format parse : ligne `0.0.0.0 domain.tld` OU `127.0.0.1 domain.tld`, trim leading/trailing whitespace, ignore lignes commençant par `#` et lignes vides, lowercase du domaine, strip point final éventuel
    - Méthode `IsBlocked(fqdn string) bool` : lookup direct + remontée des labels (`a.b.c.d` → `a.b.c.d`, `b.c.d`, `c.d`, `d` — s'arrête au premier match)
    - Méthode `Start(ctx context.Context)` : démarre goroutine avec ticker 24h, loop `Load(ctx)`. S'arrête proprement sur `ctx.Done()`. Premier `Load` synchrone avant de lancer le ticker (pour que les tests puissent attendre le load initial)
    - Pas de log de domaine ; logs uniquement compteurs / erreurs réseau (sans URL complète contenant secret)
  - [ ] Créer [internal/relay/blocklist_test.go](internal/relay/blocklist_test.go) couvrant AC 4, 5, 6, 7 (points g et h de AC8) :
    - `TestBlocklist_ParseStevenBlackFormat` : table-driven (lignes valides `0.0.0.0 foo.com`, `127.0.0.1 bar.com`, commentaires, lignes vides, IPs bizarres → ignorées, majuscules → normalisées)
    - `TestBlocklist_IsBlocked_ExactAndSubdomain` : blocklist `{"tracker.com"}` → `IsBlocked("tracker.com")` = true, `IsBlocked("ads.tracker.com")` = true, `IsBlocked("notrackercom")` = false
    - `TestBlocklist_AtomicSwap_NoRace` : run under `-race`, goroutines concurrentes `IsBlocked` + `Load` via `httptest.Server`
    - `TestBlocklist_LoadFailure_PreservesOldMap` : premier Load succès, deuxième échec (server renvoie 500) → map inchangée
    - `TestBlocklist_NoDomainInLogs` : capture `log.SetOutput(&buf)`, exécute Load + lookups sur 10 domaines → `buf.String()` ne contient aucun des 10 FQDNs

- [ ] Tâche 2 — Créer le resolver DNS interne (AC: 2, 3, 5, 7, 8)
  - [ ] Créer [internal/relay/dns_resolver.go](internal/relay/dns_resolver.go) avec :
    - Dépendance `github.com/miekg/dns` pour parse/synthèse des messages DNS wire-format (déjà utilisé par l'écosystème Go mainstream ; vérifier `go.mod` et ajouter si absent — cf. Latest Tech Information)
    - Type `DNSResolver` encapsulant `upstreams []*DoHResolver` (slice ordonné : Cloudflare puis Quad9), `blocklist *Blocklist`, `client *http.Client` (partagé), compteurs `atomic.Uint64` (queriesTotal, blockedTotal, upstreamFailuresTotal, dnssecFailuresTotal)
    - Constructeur `NewDNSResolver(upstreams []string, blocklist *Blocklist, client *http.Client) *DNSResolver` (upstreams par défaut : `["https://1.1.1.1/dns-query", "https://9.9.9.9/dns-query"]`). Panic si `upstreams` vide. Si `client` nil → timeout total 3s.
    - Méthode `Resolve(ctx context.Context, query []byte) ([]byte, error)` : parse le message DNS (1 question attendue, sinon SERVFAIL), extrait le FQDN de la question (lowercase, strip trailing dot), check blocklist → NXDOMAIN synthétisé si match (incrémente blockedTotal) ; sinon appelle `doHPost(ctx, upstreams[0], query)` avec timeout 2s, puis `upstreams[1]` en cas d'erreur ou DNSSEC invalide (incrémente upstreamFailuresTotal / dnssecFailuresTotal) ; retourne SERVFAIL synthétisé si double échec. Toujours incrémente queriesTotal.
    - Helper `synthesizeNXDOMAIN(query []byte) []byte` : reprend le QID, flip QR=1, RCODE=3, ANCOUNT=0, recopie la Question section
    - Helper `synthesizeSERVFAIL(query []byte) []byte` : QR=1, RCODE=2
    - Helper `validateDNSSEC(resp *dns.Msg) bool` : retourne `resp.AuthenticatedData` (flag AD). Si l'upstream est accédé via DoH Cloudflare/Quad9 avec `cd=0` (par défaut) et DNSSEC validation activée côté upstream, AD=1 est la preuve que le upstream a validé. Si AD=0, considérer échec DNSSEC.
    - Méthode `Metrics() DNSMetrics` : snapshot des 4 compteurs (exposable par `/health` éventuellement en Story 3.7)
  - [ ] Créer [internal/relay/dns_resolver_test.go](internal/relay/dns_resolver_test.go) couvrant AC 2, 3, 7, 8 :
    - `TestDNSResolver_NominalCloudflare` : `httptest.Server` retourne réponse DNSSEC valide (AD=1) → résolution OK, queriesTotal=1
    - `TestDNSResolver_FailoverToQuad9_OnTimeout` : premier server timeout (sleep > 2s), second OK → réponse du second, upstreamFailuresTotal=1
    - `TestDNSResolver_FailoverToQuad9_OnDNSSECInvalid` : premier AD=0, second AD=1 → réponse du second, dnssecFailuresTotal=1
    - `TestDNSResolver_SERVFAIL_BothFail` : deux servers 500 → réponse synthétisée RCODE=2
    - `TestDNSResolver_BlocklistNXDOMAIN` : blocklist `{"evil.com"}`, query `evil.com` → NXDOMAIN, aucun appel HTTP upstream (vérifier via `server.handlerCalled` flag)
    - `TestDNSResolver_NoDomainInLogs` : comme pour blocklist
  - [ ] Ajout `github.com/miekg/dns` dans [go.mod](go.mod) si absent (`go get github.com/miekg/dns@latest`)

- [ ] Tâche 3 — Intégration au dispatcher du handler /tunnel (AC: 1)
  - [ ] **Dépendance** : cette tâche suppose Story 3.3 (handler /tunnel) déjà implémentée — si non, la Tâche 3 est **déferrée** et une note est ajoutée à Completion Notes. L'implémentation des Tâches 1 et 2 est néanmoins livrable indépendamment (resolver + blocklist en tant que composants réutilisables).
  - [ ] Dans `internal/relay/tunnel_handler.go` (à créer en Story 3.3), dans la fonction de dispatch du paquet IP : détecter `IPv4/IPv6 + UDP dport=53` OU `IPv4/IPv6 + TCP dport=53` ; si match, extraire le payload DNS (UDP : à partir de l'offset header UDP ; TCP : 2 octets length prefix + payload) et appeler `resolver.Resolve(ctx, payload)`. Ré-encapsuler la réponse dans un paquet IP/UDP ou IP/TCP avec source = dst original (spoof du serveur DNS), destination = src original, et push vers le stream client. **Ne pas** NAT-forwarder le paquet DNS.
  - [ ] Wiring dans [cmd/relay/main.go](cmd/relay/main.go) : créer `blocklist := relay.NewBlocklist(...)`, `blocklist.Start(ctx)`, `resolver := relay.NewDNSResolver(nil, blocklist, nil)`, passer `resolver` au server HTTP/3 lors de son instanciation. Flag CLI `-dns-blocklist-url` pour override (défaut StevenBlack).
  - [ ] Si Story 3.3 pas encore mergée, cette tâche reste ouverte avec TODO explicite référençant ce fichier. Sinon, les tests `e2e_test.go` existants ne doivent pas régresser.

- [ ] Tâche 4 — Configuration relais (AC: 5)
  - [ ] Ajouter section `[relay.dns]` dans `config.example.toml` (côté relais uniquement) :
    ```toml
    [relay.dns]
    upstreams = ["https://1.1.1.1/dns-query", "https://9.9.9.9/dns-query"]
    blocklist_url = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
    blocklist_refresh_hours = 24
    ```
  - [ ] Wiring dans [internal/config/config.go](internal/config/config.go) : ajouter struct `RelayDNSConfig` dans `RelayConfig` (la section `[relay]` existe — vérifier et étendre). Valeurs défaut alignées sur la config exemple. Aucune variable d'env requise.
  - [ ] Les valeurs doivent être lues par `cmd/relay/main.go` et passées au constructeur du resolver / blocklist

- [ ] Tâche 5 — Audit anti-fuite logs (AC: 7)
  - [ ] Grep ciblé sur [internal/relay/dns_resolver.go](internal/relay/dns_resolver.go), [internal/relay/blocklist.go](internal/relay/blocklist.go), [cmd/relay/main.go](cmd/relay/main.go) : aucun `log.Printf`, `fmt.Fprintf(os.Stderr, ...)`, `slog`, ou `http.ResponseWriter.Write` ne doit contenir une interpolation de FQDN
  - [ ] Les erreurs retournées en interne PEUVENT contenir des FQDNs pour debug (ex. `fmt.Errorf("parse question: %s", q.Name)`), MAIS les callers (handler /tunnel, main) doivent les jeter silencieusement sans logger. Vérifier chaque caller.
  - [ ] Consigner le résultat de l'audit dans Completion Notes (fichiers scannés + verdict)

- [ ] Tâche 6 — Smoke test VPS (AC: 1, 2, 3, 4)
  - [ ] Après rebuild + redeploy sur un relais (voir `reference_relay_servers.md` en mémoire, AFTER Story 3.3 est live) :
    - `dig @<relais-public-ip> example.com` via le tunnel levoile (depuis client avec TUN up) → attendu réponse A valide, AD=1 si resolver retourne le flag
    - `dig @<relais-public-ip> doubleclick.net` via tunnel → attendu NXDOMAIN (domaine présent dans StevenBlack unified hosts)
    - `journalctl -u levoile-relay.service --since "5 min ago" | grep -iE "example\.com|doubleclick\.net"` → attendu **zéro** match (AC7)
    - Vérifier endpoint `/health` (Story 3.7) expose `dns_queries_total` incrémenté
  - [ ] Consigner les réponses dans Completion Notes (masquer les IPs si visibles)

## Dev Notes

### Contexte business

C'est la pièce qui rend Le Voile **strictement équivalent à un VPN DNS-safe** : sans ce resolver côté relais, les requêtes DNS clientes seraient NAT-forwardées vers les DNS du FAI du relais (et donc leakées à l'upstream local du VPS). L'architecture post-2026-04-15 a **retiré** le proxy DNS local client (`internal/dns/proxy.go`, `kill_switch.go`, `blocklist` côté client) : tout est désormais du ressort du relais. Cette story matérialise cette promesse. FR8b (blocklist) + NFR9f (DNSSEC) + NFR22a (zero log domaine) sont les trois invariants non négociables.

### État existant (à NE PAS confondre)

- [internal/relay/doh_handler.go](internal/relay/doh_handler.go) — **différent**. C'est l'ancien handler `/dns-query` côté relais qui acceptait des requêtes DoH client et les forwardait. Il est en cours de **suppression** par la révision (plus utilisé par le client qui passe désormais par TUN + `/tunnel`). Son code de failover upstream (`activeIdx`, `recoveryInterval`, `httpClient`) est néanmoins une bonne **référence architecturale** pour le pattern failover à répliquer dans `dns_resolver.go`. Lire mais **ne pas réutiliser en l'état** — il traite du HTTP, pas du DNS wire.
- [internal/dns/*](internal/dns/) côté client — **obsolète**. Toutes les fonctions `proxy.go`, `kill_switch.go`, `dnscache_*.go`, `reuseaddr_*.go` sont en cours de retrait (voir architecture ligne 402). Ne **pas** s'en inspirer pour le resolver relais.
- Aucun code de blocklist n'existe pour l'instant (le module client `internal/blocklist/` a déjà été retiré par le refactor).
- DNSSEC : jamais implémenté. Dépendance à ajouter : `github.com/miekg/dns` (package Go DNS canonique, sans dépendance réseau, utilisé par CoreDNS).

### Gap à combler (périmètre de cette story)

1. **Créer `blocklist.go`** : parser StevenBlack + map atomique + refresh ticker.
2. **Créer `dns_resolver.go`** : Resolve(query) avec DoH upstream (Cloudflare → Quad9), DNSSEC via flag AD, blocklist, synthèse NXDOMAIN/SERVFAIL.
3. **Câbler** dans `cmd/relay/main.go` et dans le futur handler `/tunnel` (Story 3.3).
4. **Tests** sans réseau réel.
5. **Config** via section `[relay.dns]`.

### Décisions de conception (à suivre, ne PAS débattre à nouveau)

- **DoH (application/dns-message) plutôt que DNS plain UDP/TCP vers 1.1.1.1:53** : (a) meilleure résistance au filtrage côté hébergeur du VPS, (b) chiffrement upstream — le VPS provider ne voit pas les queries, (c) Cloudflare et Quad9 supportent tous deux DoH HTTPS/2 de façon stable, (d) un seul `http.Client` partagé pour tout le relais.
- **DNSSEC validation = lecture du flag AD de l'upstream** (pas de validation cryptographique locale) : Cloudflare 1.1.1.1 et Quad9 9.9.9.9 activent DNSSEC en interne et renvoient AD=1 quand la réponse est authentifiée. Faire une validation cryptographique locale (en-dessous des upstreams) ajouterait des milliers de lignes de code (RRSIG parsing, anchor trust `.` , NSEC/NSEC3) sans bénéfice réel : on fait déjà confiance à TLS 1.3 vers Cloudflare/Quad9. **Conséquence** : AD=0 → bascule fallback, AD=0 pour les deux → SERVFAIL. Suffisant pour NFR9f.
- **Blocklist = StevenBlack/hosts "unified"** (liste par défaut, sans extensions fakenews/gambling/porn/social) : compromis privacy-vs-fonctionnalité raisonnable pour MVP. Les variantes sont configurables via URL dans `[relay.dns]`.
- **Matching sous-domaine par remontée des labels** : conforme à la sémantique DNS (un domaine bloqué bloque tous ses sous-domaines — sinon `ads.google.com` bloqué mais pas `foo.ads.google.com` serait trivial à contourner).
- **Synthèse des paquets DNS de réponse NXDOMAIN/SERVFAIL** : on ne réutilise **pas** la query pour le DNS body — on recrée un message DNS avec `dns.Msg{}`, on copie QID, on met les flags, et on encode. `github.com/miekg/dns` a `m.SetRcode(request, dns.RcodeNameError)` qui fait ça en une ligne.
- **Pas de cache DNS local côté relais au MVP** : (a) Cloudflare/Quad9 cachent déjà, (b) un cache local avec TTL invaliderait le modèle "zero state" du relais (FR18), (c) on privilégie simplicité + conformité NFR3 (stateless). Si perf insuffisante (NFR10 : latence DNS < 50ms), on re-ouvrira dans une story future.
- **StevenBlack = ~150k-200k entrées** : ~5-10 MB en map Go, acceptable côté relais (RAM budget relais ≥ 256 MB typique).

### Source tree à toucher

- [internal/relay/blocklist.go](internal/relay/blocklist.go) — **CRÉATION**
- [internal/relay/blocklist_test.go](internal/relay/blocklist_test.go) — **CRÉATION**
- [internal/relay/dns_resolver.go](internal/relay/dns_resolver.go) — **CRÉATION**
- [internal/relay/dns_resolver_test.go](internal/relay/dns_resolver_test.go) — **CRÉATION**
- [cmd/relay/main.go](cmd/relay/main.go) — **édition** (Tâche 3) : wiring resolver + blocklist, flag CLI
- [internal/config/config.go](internal/config/config.go) — **édition** (Tâche 4) : section `[relay.dns]`
- [config.example.toml](config.example.toml) — **édition** (Tâche 4)
- [go.mod](go.mod) / [go.sum](go.sum) — **édition** : ajout `github.com/miekg/dns`
- `internal/relay/tunnel_handler.go` — **à créer par Story 3.3** : appel resolver.Resolve au dispatch

### Ne PAS toucher

- Handler `/verify` ([internal/relay/verify_handler.go](internal/relay/verify_handler.go)) — scope Story 3.2
- Handler `/connect` ([internal/relay/connect_handler.go](internal/relay/connect_handler.go)) — en cours de retrait par la révision, hors scope ici (laisser mourir dans son coin)
- Handler `/dns-query` (DoH) ([internal/relay/doh_handler.go](internal/relay/doh_handler.go)) — en cours de retrait. **Ne PAS supprimer dans cette story** (perturberait la compatibilité backward du fleet tant que les clients 2026-04-08 et antérieurs tournent encore). Suppression prévue par une story d'épic 7 après bascule clients complète.
- NAT table (`internal/relay/nat_table.go`) — scope Story 3.4
- `internal/blocklist/` côté client — **n'existe plus** (déjà retiré par le refactor)

### Standards de test

- Couverture Go : `go test ./internal/relay/...` doit rester vert ; `go test -race ./internal/relay/...` doit passer (critique pour `atomic.Pointer`)
- Tests tous offline : `httptest.Server` pour upstreams, `httptest.Server` pour blocklist download. Aucun test ne tape `1.1.1.1` ou `raw.githubusercontent.com` réels (CI flaky + fuite IP du runner).
- Style table-driven déjà utilisé dans [verify_handler_edge_test.go](internal/relay/verify_handler_edge_test.go) — s'y conformer
- Test "no domain in logs" : utiliser `log.SetOutput(&bytes.Buffer{})` + `t.Cleanup(func(){ log.SetOutput(os.Stderr) })`, puis `assert.NotContains` sur chaque FQDN utilisé dans le test

### Project Structure Notes

- Conforme à l'architecture : [_bmad-output/planning-artifacts/architecture.md](_bmad-output/planning-artifacts/architecture.md) lignes 242, 254, 287, 352, 862-865, 989, 1031, 1181 référencent `internal/relay/dns_resolver.go` et `internal/relay/blocklist.go` comme modules attendus
- Conforme ADR-06 DNS relais (voir prd-validation-report-2026-04-15.md#79)
- Invariant zero-persistence (NFR3, FR18) : blocklist est en RAM, refresh périodique, aucune écriture disque ; compteurs DNS sont `atomic.Uint64` en RAM uniquement

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.5] — user story + AC initiaux (lignes 712-730)
- [Source: _bmad-output/planning-artifacts/architecture.md#L242] — "DNS : Résolution côté relais (upstream Cloudflare 1.1.1.1 / Quad9 9.9.9.9, blocklist StevenBlack)"
- [Source: _bmad-output/planning-artifacts/architecture.md#L254] — "Blocklist DNS : Téléchargement StevenBlack/hosts côté relais"
- [Source: _bmad-output/planning-artifacts/architecture.md#L287] — "DNS : Les requêtes DNS (UDP 53 / TCP 53) capturées par la TUN arrivent au relais comme tout autre paquet IP"
- [Source: _bmad-output/planning-artifacts/architecture.md#L352] — "Relay DNSSEC Validator (`internal/relay/dns_resolver.go`)"
- [Source: _bmad-output/planning-artifacts/architecture.md#L862-865] — emplacement `dns_resolver.go` + `blocklist.go` dans l'arbo
- [Source: _bmad-output/planning-artifacts/architecture.md#L1031] — flux paquet IP → dispatch DNS → dns_resolver + blocklist
- [Source: _bmad-output/planning-artifacts/prd.md#NFR9f] — DNSSEC requis
- [Source: _bmad-output/planning-artifacts/prd.md#NFR22a] — zero log de nom de domaine
- [Source: _bmad-output/planning-artifacts/prd.md#FR8b] — blocklist StevenBlack côté relais
- [Source: _bmad-output/planning-artifacts/prd.md#L281] — risque DNS poisoning upstream → mitigation DNSSEC + fallback Quad9
- [Source: internal/relay/doh_handler.go] — pattern failover upstream à s'inspirer (sans réutiliser)

### Previous Story Intelligence

- [3-2-endpoint-verify-avec-emission-session-tokens-ed25519.md](_bmad-output/implementation-artifacts/3-2-endpoint-verify-avec-emission-session-tokens-ed25519.md) (ready-for-dev) : établit le pattern de session tokens qu'utilisera Story 3.3 (handler `/tunnel`). Ce resolver n'a **pas** à vérifier le session token lui-même — c'est le handler `/tunnel` en amont qui filtre. Reprendre la convention d'audit anti-fuite logs (AC5 / Tâche 3 de 3.2) pour les logs DNS ici (AC7 / Tâche 5).
- [3-1-binaire-relais-go-http3-stateless-deployable-via-systemd.md](_bmad-output/implementation-artifacts/3-1-binaire-relais-go-http3-stateless-deployable-via-systemd.md) (ready-for-dev) : durcit `install.sh`. Le wiring resolver + blocklist dans `cmd/relay/main.go` doit rester compatible avec `CAP_NET_ADMIN` et le user `levoile-relay` (pas d'écriture disque).
- Stories 3.3 et 3.4 (handler /tunnel et NAT) sont en **backlog** : la Tâche 3 de cette story dépend de 3.3. Stratégie : livrer Tâches 1-2-4-5 de manière autonome ; la Tâche 3 sera finalisée lorsque 3.3 mergera. Cela n'empêche **pas** le merge de cette story (les composants sont utilisables dès leur existence).
- Epic 1 / Story 1-1 (TLS pinning, done) : canal client↔relais chiffré, donc le payload DNS entre client et relais est déjà confidentiel. Le resolver côté relais n'a pas à se soucier de MITM côté client.

### Latest Tech Information

- **github.com/miekg/dns** : package Go DNS canonique (dernière version stable ~v1.1.63 / 2025). Utilisé par CoreDNS, dnscrypt-proxy, etc. API stable. Fournit `dns.Msg.Pack()`, `dns.Msg.Unpack()`, `dns.Msg.SetRcode()`, flags AD/CD, parsing RR. Ajouter via `go get github.com/miekg/dns@latest`. Aucune dépendance transitive problématique (pure Go).
- **Cloudflare DoH** : `https://1.1.1.1/dns-query` — Content-Type `application/dns-message` en POST (ou GET avec `?dns=<base64url(wire)>`). Préférer POST (plus simple, pas de limite de taille base64). DNSSEC validation activée par défaut (AD=1 pour zones signées).
- **Quad9 DoH** : `https://9.9.9.9/dns-query` — mêmes conventions. DNSSEC idem. Quad9 applique aussi son propre filtrage malware (bonus).
- **StevenBlack/hosts unified** : `https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts` — ~150k-200k entrées, format classique `0.0.0.0 domain.tld # comment`. Refresh typique 24-48h upstream. ETag supporté (optimisation possible mais pas requise au MVP).
- **atomic.Pointer[T]** : Go 1.19+. Vérifier la version Go dans [go.mod](go.mod) (probablement 1.22+ déjà). Zero race sur swap + read concurrents.
- **HTTP/2 client pool** : un seul `http.Client` partagé entre les deux upstreams suffit (multiplexing HTTP/2 par défaut). Timeout par requête via `context.WithTimeout`, pas via `http.Client.Timeout` (bloque tout le client).

### Project Context

Voir [project-context.md](**/project-context.md) si présent, sinon [CLAUDE.md](CLAUDE.md) à la racine pour les conventions Go + structure des tests du projet.

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

### Completion Notes List

- Ultimate context engine analysis completed — comprehensive developer guide created

### File List
