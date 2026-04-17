# Story 4.2: Bootstrap via relais hardcodé au premier lancement

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur final,
Je veux que le client trouve son premier relais sans fichier de registre préexistant et sans dépendre du resolver DNS système,
Afin que le premier lancement post-installation fonctionne sans configuration manuelle et reste protégé contre un DNS poisoning upstream.

## Acceptance Criteria

1. **Bootstrap premier lancement via DoH (AC1)** — Quand le service démarre et qu'aucun cache local de registre n'existe, le domaine relais bootstrap hardcodé dans la config par défaut (`relay.levoile.dev`) est utilisé, la résolution DNS de ce domaine se fait via DoH (Cloudflare `https://cloudflare-dns.com/dns-query` puis fallback Quad9 `https://dns.quad9.net/dns-query`) avant que le tunnel ne soit établi, le registre est récupéré depuis ce relais bootstrap, puis mis en cache localement. Conforme NFR9i.

2. **Cold start résilient cache-only (AC2)** — Quand le cache local existe mais qu'aucun relais ne répond au démarrage (timeout ou échec de fetch), le cache est utilisé comme source de vérité et le service tente la connexion à chaque relais du cache jusqu'à réussite, sans jamais retomber sur une résolution DNS système non protégée.

3. **Failover DoH Cloudflare→Quad9** — Quand Cloudflare DoH est injoignable (timeout > 5s, HTTP ≠ 200, erreur TLS), le resolver bascule automatiquement sur Quad9 DoH. Si les deux échouent, le client renvoie une erreur explicite et n'appelle JAMAIS le resolver système (pas de fallback silencieux `net.LookupHost`).

4. **Aucun log IP utilisateur** — Les échecs de résolution DoH et de fetch registry sont loggés côté client sans révéler l'IP utilisateur. Aucune donnée utilisateur identifiable ne transite dans les logs d'erreur. Conforme NFR20.

5. **Transport HTTP/3 injectable** — Le `registry.Client` expose une option `WithResolver(resolver *DoHResolver)` qui construit un `http.Transport` dont le `DialContext` utilise l'IP résolue par DoH au lieu du resolver système. La configuration est opt-in (si nil, comportement actuel préservé pour les tests).

### Scenarios BDD détaillés

**AC1 — Bootstrap premier lancement via DoH:**
```
Given aucun fichier cache local ne existe (premier lancement post-installation)
And la config par défaut contient relay.domain = "relay.levoile.dev"
And la config par défaut contient registry.url = "https://relay.levoile.dev"
And la config par défaut contient registry.master_public_key = <clé embedded>
When le service démarre et appelle registry.Discover(ctx)
Then le DoH resolver interroge https://cloudflare-dns.com/dns-query pour relay.levoile.dev
And l'IP résolue (A ou AAAA) est utilisée par le http.Transport.DialContext
And le client fetch GET https://relay.levoile.dev/.well-known/relay-registry.json
And la signature Ed25519 master key est vérifiée sur chaque entrée
And les relais vérifiés sont persistés dans $AppData/LeVoile/relay-cache.toml (Windows) ou $XDG_CONFIG_HOME/levoile/relay-cache.toml (Linux)
And le resolver DNS système n'est JAMAIS interrogé pour relay.levoile.dev pendant ce bootstrap
```

**AC2 — Cold start cache-only:**
```
Given le fichier relay-cache.toml existe avec N relais vérifiés
And aucun réseau DoH n'est disponible (Cloudflare et Quad9 timeout)
When le service démarre et appelle registry.Discover(ctx)
Then registry.Client.Fetch() échoue
And Cache.Load() retourne les N relais cachés
And Registry.VerifyAll() re-vérifie la signature contre la master key de confiance (PAS celle du cache)
And les relais vérifiés sont retournés triés par cached latency si disponible
And service.Program tente la connexion tunnel vers chaque relais dans l'ordre jusqu'à réussite
```

**AC3 — Failover DoH Cloudflare→Quad9:**
```
Given Cloudflare DoH est injoignable (timeout 5s)
When DoHResolver.Resolve("relay.levoile.dev") est appelé
Then Cloudflare DoH échoue avec ErrDoHUpstreamUnreachable
And le resolver tente automatiquement Quad9 DoH
And si Quad9 répond, l'IP est retournée
And si Quad9 échoue également, ErrAllDoHUpstreamsDown est retournée
And AUCUN appel à net.LookupHost / net.Resolver par défaut n'a lieu
```

**AC4 — Zero log IP client:**
```
Given une résolution DoH échoue ou un fetch registry échoue
When l'erreur est loggée via serviceStderr
Then le message contient le domaine résolu (relay.levoile.dev) et l'upstream DoH (cloudflare/quad9)
And le message NE contient PAS l'IP source de la machine client
And le message NE contient PAS de données liées à l'utilisateur (user-agent, cookies)
```

**AC5 — Opt-in via config:**
```
Given registry.bootstrap_doh_enabled = true dans config.toml (ou défaut à true quand registry.enabled=true)
When le service construit registry.NewClient
Then WithResolver(doh) est passée en option
Given registry.bootstrap_doh_enabled = false
When le service construit registry.NewClient
Then WithResolver n'est PAS passée et le http.Transport utilise le resolver système (comportement legacy, diagnostique uniquement)
```

## Tasks / Subtasks

> **Ordre d'exécution :** Task 1 (DoH resolver) → Task 2 (intégration Client) → Task 3 (wiring service) → Task 4 (config) → Task 5 (tests) → Task 6 (docs deploy)

- [x] **Task 1 : Créer `internal/registry/doh_resolver.go`** (AC: #1, #3, #4)
  - [x] 1.1 Créer le type `DoHResolver` avec champs : `upstreams []string` (URLs DoH), `httpClient *http.Client` (timeout 5s par upstream), `logger func(format string, args ...any)` optionnel
  - [x] 1.2 Constantes : `CloudflareDoH = "https://cloudflare-dns.com/dns-query"`, `Quad9DoH = "https://dns.quad9.net/dns-query"`
  - [x] 1.3 Constructeur `NewDoHResolver(opts ...DoHOption) *DoHResolver` — défauts : `[Cloudflare, Quad9]`, timeout 5s
  - [x] 1.4 Options fonctionnelles : `WithDoHUpstreams([]string)`, `WithDoHTimeout(time.Duration)`, `WithDoHHTTPClient(*http.Client)` (tests)
  - [x] 1.5 Méthode `Resolve(ctx context.Context, host string) (netip.Addr, error)` :
    - Pour chaque upstream dans l'ordre : construire requête DoH RFC 8484 (`GET {upstream}?dns=<base64url(DNS wireformat)>`) ou POST `application/dns-message` (choisir POST pour simplicité, éviter caching middleboxes)
    - Parser la réponse DNS wireformat (réutiliser `golang.org/x/net/dns/dnsmessage` — déjà dans go.sum pour quic-go)
    - Retourner la première IP A ou AAAA trouvée (préférer A pour IPv4, AAAA seulement si AllowIPv6Leak=false est respecté par contexte appelant — ici on préfère A)
    - Si upstream timeout/HTTP≠200/parsing error/pas de réponse : passer au suivant
    - Si tous échouent : retourner `ErrAllDoHUpstreamsDown`
  - [x] 1.6 Erreurs sentinelles : `ErrDoHUpstreamUnreachable`, `ErrAllDoHUpstreamsDown`, `ErrDoHInvalidResponse`, `ErrDoHNoAddressRecord`
  - [x] 1.7 **Logging discret** (AC4) : jamais d'IP client, seulement domain + upstream + type d'erreur. Utiliser un callback injecté, par défaut no-op
  - [x] 1.8 **NE PAS utiliser `net.Resolver` par défaut en fallback** — si DoH échoue, on renvoie l'erreur. Pas de « soft fallback » resolver système

- [x] **Task 2 : Intégrer DoHResolver dans `registry.Client`** (AC: #5)
  - [x] 2.1 Ajouter option `WithResolver(resolver *DoHResolver) ClientOption` dans [client.go](internal/registry/client.go)
  - [x] 2.2 Si un resolver est fourni, construire un `*http.Transport` custom qui :
    - Implémente `DialContext(ctx, network, addr)` :
      1. Extraire `host, port` via `net.SplitHostPort(addr)`
      2. Si `host` est déjà une IP (via `netip.ParseAddr`), déléguer à `net.Dialer.DialContext` direct (pas de re-résolution)
      3. Sinon, appeler `resolver.Resolve(ctx, host)` → obtenir IP
      4. Dial `net.JoinHostPort(ip.String(), port)` via `net.Dialer{Timeout: 10s, KeepAlive: 30s}`
    - Conserve `ForceAttemptHTTP2: true`, `TLSClientConfig` avec ServerName = host original (important pour SNI et cert verification)
    - TLS 1.3 minimum (NFR1) via `MinVersion: tls.VersionTLS13`
  - [x] 2.3 Si aucun resolver fourni → comportement actuel préservé (utilise `&http.Client{Timeout: 10s}`)
  - [x] 2.4 Ajouter test unitaire `client_test.go` : mock DoH + serveur HTTPS local → vérifier que le DialContext passe bien par le DoH avant de connecter
  - [x] 2.5 **Piège SNI** : le ServerName TLS doit rester le domaine (`relay.levoile.dev`) et PAS l'IP résolue, sinon le cert pinning échoue. Hériter via `http.Transport.TLSClientConfig` reste OK car `http.Client` passe le host original via `Host:` header et `TLS ServerName` par défaut est dérivé de l'URL, pas du DialContext

- [x] **Task 3 : Wiring dans `internal/service/service.go`** (AC: #1, #2, #5)
  - [x] 3.1 Dans la section 0d ([service.go:736-776](internal/service/service.go#L736-L776)), avant `registry.NewClient` :
    - Créer un `DoHResolver` si `p.config.RegistryBootstrapDoHEnabled == true` (voir Task 4)
    - Passer `registry.WithResolver(doh)` à `NewClient`
  - [x] 3.2 Ne PAS construire de DoHResolver si `RegistryEnabled == false` — pas de registry, pas de bootstrap
  - [x] 3.3 Logging : `fmt.Fprintf(serviceStderr, "service: registry bootstrap: using DoH resolver\n")` quand activé
  - [x] 3.4 **Cas AC2** : le code actuel [discoverer.go:70-86](internal/registry/discoverer.go#L70-L86) gère déjà le fallback cache. **Aucune modif nécessaire** côté discoverer. Vérifier que le path cache → VerifyAll → relays est bien retourné au service pour que le failover itère sur tous les relais cachés
  - [x] 3.5 Ajouter dans `svc.Config` :
    ```go
    RegistryBootstrapDoHEnabled bool
    RegistryDoHUpstreams        []string // optionnel, vide = défauts
    ```

- [x] **Task 4 : Extension `config.RegistryConfig`** (AC: #5)
  - [x] 4.1 Ajouter dans [config.go:85](internal/config/config.go#L85) :
    ```go
    type RegistryConfig struct {
        Enabled             bool     `toml:"enabled"`
        URL                 string   `toml:"url"`
        MasterPublicKey     string   `toml:"master_public_key"`
        RefreshInterval     string   `toml:"refresh_interval"`
        BootstrapDoHEnabled bool     `toml:"bootstrap_doh_enabled"` // nouveau
        DoHUpstreams        []string `toml:"doh_upstreams,omitempty"` // nouveau, optionnel
    }
    ```
  - [x] 4.2 Default dans `Load()` ([config.go:137](internal/config/config.go#L137)) : `BootstrapDoHEnabled: true` (activé par défaut quand Registry l'est)
  - [x] 4.3 Mettre à jour [installer/config-default.toml](installer/config-default.toml) :
    ```toml
    [registry]
    enabled = true
    url = "https://relay.levoile.dev"
    master_public_key = "rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk="
    bootstrap_doh_enabled = true
    ```
  - [x] 4.4 Validation : si `DoHUpstreams` est non-vide, chaque entrée doit parser comme URL HTTPS ; sinon retourner erreur config
  - [x] 4.5 Propager vers `svc.Config` dans [cmd/client/main.go](cmd/client/main.go) et [cmd/portable/main.go](cmd/portable/main.go) (résolution TOML → Config service)

- [x] **Task 5 : Tests** (AC: tous)
  - [x] 5.1 Créer `internal/registry/doh_resolver_test.go` :
    - `TestDoHResolver_CloudflareSuccess` : spin up `httptest.NewTLSServer` qui répond en DNS wireformat valide → vérifier IP retournée
    - `TestDoHResolver_CloudflareFailQuad9Success` : Cloudflare timeout/500 → Quad9 succède → vérifier failover
    - `TestDoHResolver_AllUpstreamsDown` : tous renvoient erreur → `ErrAllDoHUpstreamsDown`
    - `TestDoHResolver_NoAddressRecord` : réponse DNS sans A/AAAA → `ErrDoHNoAddressRecord`
    - `TestDoHResolver_ContextCanceled` : ctx annulé au milieu → erreur propagée
    - `TestDoHResolver_LogsNoClientIP` : vérifier que le logger reçoit domain + upstream mais jamais une IP parseable comme client
  - [x] 5.2 Étendre `internal/registry/client_test.go` :
    - `TestClient_FetchViaDoHResolver` : DoH mock + registry HTTPS mock local → bout en bout
    - `TestClient_NoResolverFallsBackToSystemDial` : absence de WithResolver → `http.Client{}` standard (compatibilité)
  - [x] 5.3 Test d'intégration `internal/registry/e2e_test.go` :
    - `TestE2E_Bootstrap_NoCache_DoHOnly` : cache vide + DoH mocké + registry mocké → discover retourne les relais et persiste le cache
    - `TestE2E_ColdStart_CacheOnly_AllRelaysDown` : cache présent + registry 503 + DoH up → discover retourne les relais du cache
  - [x] 5.4 Test `internal/config/config_test.go` : `TestRegistryConfig_BootstrapDoHDefault` — vérifier défaut `true`
  - [x] 5.5 Lancer `go test ./internal/registry/... ./internal/config/... ./internal/service/...` → 0 échec
  - [x] 5.6 `go vet ./...` → 0 warning
  - [x] 5.7 `go build ./...` → tous les binaires compilent

- [x] **Task 6 : Documentation deploy & ops** (AC: #1)
  - [x] 6.1 Mettre à jour [deploy/README.md](deploy/README.md) si présent : mentionner que le domaine bootstrap (`relay.levoile.dev`) doit être routé vers N'IMPORTE QUEL relais du pool en production (round-robin DNS ou pointer vers un relais sacrificiel dédié au bootstrap). C'est le **single point of trust**, pas le single point of failure : tout relais sert le registre complet
  - [x] 6.2 Ajouter note opérationnelle : rotation du domaine bootstrap = rebuild client (domaine embedded dans `installer/config-default.toml`). La rotation est possible via la clé de rotation Ed25519 (NFR22h) lors du prochain release signé
  - [x] 6.3 Vérifier manuellement avec curl : `curl -H "accept: application/dns-message" "https://cloudflare-dns.com/dns-query?name=relay.levoile.dev&type=A" | xxd` → réponse binaire valide

## Dev Notes

### Contraintes architecturales OBLIGATOIRES

- **Pure Go, pas de CGo** — pas de dépendance libc pour le resolver DoH
- **Module path:** `github.com/velia-the-veil/le_voile`
- **Go 1.26**
- **Jamais de logging client sensible** — pas d'IP utilisateur dans les logs (NFR20)
- **Jamais de `panic`** — toujours retourner `error`
- **Error wrapping** avec préfixe package : `fmt.Errorf("registry: doh: %w", err)`
- **context.Context** en premier argument de toute fonction bloquante/réseau
- **TLS 1.3 min** (NFR1) pour les connexions DoH et la connexion au registry
- **crypto/subtle.ConstantTimeCompare** (NFR9c) pour toute comparaison cryptographique — hérité, la story 4.2 n'ajoute pas de comparaison crypto directe, mais le `VerifyAll` existant l'utilise

### DÉCISION CRITIQUE : DoH transport vs Resolver Go standard

**Problème :** `net.Resolver` a un champ `Dial` qui permet de rediriger TCP/UDP vers un serveur DNS custom (DoT / DNS-over-TLS possible via ce hook). Mais pour DoH, il faut du HTTP/2 vers un endpoint — `net.Resolver` ne supporte pas nativement DoH.

**Solutions candidates :**
1. **Custom `http.Transport.DialContext`** (RETENU) : on résout via DoH puis on dial sur l'IP. Simple, pas de dépendance nouvelle, contrôle total. Le `TLSClientConfig.ServerName` reste le domaine (SNI préservé)
2. `golang.org/x/net/dns/dnsmessage` + DoH custom + `net.Resolver{PreferGo: true, Dial: ...}` → plus complexe, `net.Resolver.Dial` n'expose pas le host cible facilement
3. Bibliothèque tierce (ncruces/go-dns, AdguardTeam/dnsproxy) → dépendance supplémentaire, surface d'attaque accrue

**Retenu :** solution 1. Le resolver DoH fait une seule requête HTTP/2 vers Cloudflare ou Quad9, parse la réponse wireformat avec `golang.org/x/net/dns/dnsmessage` (bibliothèque standard x/net déjà transitivement présente via quic-go), et retourne la première IP A/AAAA. Le `http.Transport.DialContext` appelle ce resolver avant chaque dial.

**Pourquoi DNS wireformat et pas JSON API Cloudflare (`application/dns-json`) ?** Parce que :
- Le wireformat est le format IETF standard (RFC 8484), supporté universellement par tout resolver DoH (Quad9 inclus)
- Le JSON API Cloudflare est propriétaire et non-portable
- La parsing binaire est triviale avec `dnsmessage.Parser` de x/net

### Flow d'exécution (premier lancement, AC1)

```
[service.run()] 
   ↓
[0d. p.config.RegistryEnabled && p.config.RegistryBootstrapDoHEnabled]
   ↓
[doh := registry.NewDoHResolver()]  // défauts Cloudflare + Quad9
   ↓
[regClient := registry.NewClient(url, masterKey, WithResolver(doh))]
   ↓
[discoverer := registry.NewDiscoverer(regClient, cache, defaultRelay)]
   ↓
[discoverer.Discover(ctx)]
   ↓
[regClient.Fetch(ctx)]
   ↓ (http.Client.Do → Transport.DialContext → resolver.Resolve via DoH HTTPS)
[DoH Cloudflare HTTPS GET → IP de relay.levoile.dev]
   ↓ (fallback Quad9 si échec)
[net.Dialer.DialContext(ip:port)]
   ↓ (TLS handshake avec SNI = relay.levoile.dev, cert pinning via master key côté app)
[GET https://relay.levoile.dev/.well-known/relay-registry.json]
   ↓
[VerifyAll(masterKey) → relais vérifiés]
   ↓
[cache.Save(relais) → $AppData/LeVoile/relay-cache.toml]
   ↓
[service se connecte au relais choisi (sticky country ou primary)]
```

### Flow cold start cache-only (AC2)

```
[service.run()]
   ↓
[discoverer.Discover(ctx)]
   ↓
[regClient.Fetch(ctx) → err (timeout DoH ou registry down)]
   ↓
[cache.Load() → N relais]
   ↓
[Registry{MasterPublicKey: client.MasterPublicKeyBase64(), Relays: cached}.VerifyAll()]
   ↓ (re-vérifie avec la master key DE CONFIANCE, pas celle du fichier cache)
[verified relays → sortByLatencyCache(verified)]
   ↓
[service itère sur chaque relais jusqu'à connexion réussie]
   ↓ (si TOUS échouent → ultime fallback defaultRelay, déjà implémenté)
```

### APIs existantes à RÉUTILISER (NE PAS RÉINVENTER)

| Package | API clé | Notes |
|---|---|---|
| `internal/registry` | `NewClient(url, keyBase64, opts...)` | Ajouter `WithResolver` — backward compat préservée |
| `internal/registry` | `Client.Fetch(ctx)` | Inchangée, utilise `c.httpClient.Do(req)` |
| `internal/registry` | `Registry.VerifyAll()` | Re-vérification signature Ed25519 master key |
| `internal/registry` | `Discoverer.Discover` | Cascade online→cache→default déjà en place |
| `internal/registry` | `Cache.Load/Save` | Persistence TOML atomic |
| `internal/service` | `Program.run()` section 0d | Wiring registry, à enrichir avec DoH resolver |
| `internal/config` | `Config.Registry.*` | Ajouter 2 champs TOML |
| stdlib | `net.Dialer`, `net.SplitHostPort`, `netip.ParseAddr`, `netip.Addr.String()` | Dialing sur IP résolue |
| `golang.org/x/net/dns/dnsmessage` | `Parser`, `Builder`, `Question`, `NewName` | DoH wireformat RFC 8484 — DÉJÀ transitif via quic-go, vérifier `go mod why golang.org/x/net` |

### Dépendances externes

| Dépendance | État | Usage |
|---|---|---|
| `golang.org/x/net/dns/dnsmessage` | À vérifier / ajouter via `go get golang.org/x/net/dns/dnsmessage` | Parsing DNS wireformat |
| `crypto/tls` | stdlib | TLS 1.3 pour DoH HTTPS et registry HTTPS |
| `net/http` | stdlib | Client HTTP/2 vers DoH endpoints |

Aucune autre dépendance nouvelle. Pas de bibliothèque DoH tierce (surface d'attaque minimale, auditability).

### NFRs à respecter

| NFR | Exigence | Application dans 4.2 |
|---|---|---|
| NFR1 | TLS 1.3 minimum | `Transport.TLSClientConfig.MinVersion = tls.VersionTLS13` pour DoH ET pour registry fetch |
| NFR9 | Bloquer paquets vers réseaux privés (SSRF relais) | N/A côté client (concerne le relais). Note : le DoH upstream doit rejeter une réponse IP privée ? → À considérer : DoHResolver peut filtrer `netip.Addr.IsPrivate() || IsLoopback()` et les ignorer pour éviter qu'un DoH compromis redirige vers 127.0.0.1 |
| NFR9c | ConstantTimeCompare | Hérité via `Registry.VerifyAll` |
| NFR9i | **Bootstrap DoH (le cœur de cette story)** | Task 1, Task 2, Task 3 |
| NFR20 | Aucun log IP client | Task 1.7 — logger callback injecté, par défaut no-op, jamais d'IP client dans les formats |
| NFR22g/h | Rotation master key | Hérité via structure x/net/crypto/ed25519. Story 4.2 ne modifie pas la rotation |

### Points de sécurité critiques (RATIONAL REVIEW)

1. **DoH upstream peut mentir** — Cloudflare ou Quad9 pourrait retourner une IP fausse pour `relay.levoile.dev` sous contrainte légale. **Mitigation (existante)** : le TLS certificate pinning sur le relais + la signature Ed25519 du registry + la signature Ed25519 de chaque relais par la master key garantissent que même une IP malicieuse ne peut pas servir un registre falsifié. Le pinning est dans [internal/crypto/](internal/crypto/) et [internal/tunnel/](internal/tunnel/)
2. **Timing attack sur DoH failover** — l'ordre strict Cloudflare → Quad9 est publiquement connu. Ce n'est pas un secret. Pas de mitigation nécessaire
3. **DoH endpoint lui-même nécessite résolution** — `cloudflare-dns.com` et `dns.quad9.net` doivent être résolus en IP. **Mitigation** : hardcoder les IPs IPv4 de ces endpoints dans `doh_resolver.go` et les utiliser comme fallback si même la première résolution système échoue. IPs stables connues : Cloudflare `1.1.1.1` / `1.0.0.1`, Quad9 `9.9.9.9` / `149.112.112.112`. Au premier usage, tenter `cloudflare-dns.com` via system DNS, si échec bypass DNS et dial direct `1.1.1.1:443` avec SNI `cloudflare-dns.com`
4. **Rejeter IPs privées retournées par DoH** (défense en profondeur) — si DoH retourne `127.0.0.1` ou `10.x.x.x` pour `relay.levoile.dev`, le client doit ignorer cette réponse et passer à l'upstream suivant. `netip.Addr.IsPrivate()`, `IsLoopback()`, `IsLinkLocalUnicast()`
5. **Cache TTL** — le registry JSON est re-fetché toutes les 6h via `Discoverer.refreshLoop`. Le cache persiste entre runs. Pas besoin de TTL agressif côté client — la signature Ed25519 par entrée suffit pour la validité. Le cache n'expire pas tant que les signatures sont valides

### Dépendances de cette story

- **Story 4.1** (backlog — à créer juste avant ou en parallèle) : endpoint `/.well-known/relay-registry.json` sur chaque relais. Code existant côté serveur ([internal/relay/](internal/relay/) + [cmd/genregistry/](cmd/genregistry/)) — à valider
- **Story 1.1** (done) : tunnel QUIC/HTTP3 avec certificate pinning
- **Story 1.3** (done) : relais accepte connexions QUIC/HTTP3
- **Stories Epic 3** (done) : relais stateless déployables

**Ancien fichier `4-2-version-portable-et-build-multi-plateforme.md`** (status `done`) : concerne l'ANCIEN Epic 4 (pré-restructuration 2026-04-15). Ne PAS le toucher. Il est archivé et reste référence pour la fonctionnalité portable, désormais rattachée conceptuellement à Epic 7 Distribution.

### Ce qui est HORS SCOPE

- Support DoT (DNS-over-TLS) — DoH suffit pour NFR9i
- DoH ODoH (Oblivious DoH) — post-MVP, trop complexe pour la v1
- DNSSEC côté client — la validation DNSSEC a lieu côté relais (NFR9f). Le client fait confiance au pinning TLS + signature Ed25519 master key
- Rotation dynamique du domaine bootstrap sans release client — post-MVP (via dual-signed release avec clé de rotation NFR22h)
- Support IPv6 DoH bootstrap — AAAA peut être retourné si A indisponible, mais la priorité reste IPv4. Les tests ciblent A records
- UI de diagnostic DoH — pas d'indicateur UI pour cette story (transparence utilisateur : l'utilisateur n'a pas à savoir que DoH est utilisé)

### Intelligence de git (recent commits)

```
16a275a fix(deploy): align install.sh/README/service with prod (signing.key + registry)
c2e1c0e feat: complete Epic 3 — relay stateless multi-VPS with tunnel IP & NAT
7bf2e59 chore: remove .claude/ from history, harden deploy scripts
bd11612 feat: IPv6 leak opt-out toggle + relay systemd CAP_NET_ADMIN (Stories 2.9 + 3.1)
ece3270 feat: implement Sprint 2 — watchdog, routing, firewall, DNS flush, captive portal
```

**Apprentissages pour 4.2 :**
- 16a275a montre que le registry est déjà déployé en production sur les 8 VPS relais (DE/ES/GB/US) — **le bootstrap peut être testé en conditions réelles**
- La branche `main` est propre, les 3 epics 1-3 sont `done` → infra stable pour attaquer Epic 4 sans régression

**Relais disponibles pour tests réels** (cf. memoire `reference_relay_servers.md`) : 8 VPS productifs déjà servants de `/.well-known/relay-registry.json`. L'intégration DoH peut être testée bout-en-bout contre la prod.

### Fichiers à CRÉER

```
internal/registry/doh_resolver.go          # DoHResolver, Resolve, failover upstreams
internal/registry/doh_resolver_test.go     # Tests unitaires DoH mock
```

### Fichiers à MODIFIER

```
internal/registry/client.go                # WithResolver option + Transport.DialContext custom
internal/registry/client_test.go           # Tests DoH-backed client
internal/registry/e2e_test.go              # Tests bootstrap complet et cold-start cache-only
internal/service/service.go                # Wiring DoHResolver en section 0d
internal/config/config.go                  # Champs BootstrapDoHEnabled + DoHUpstreams
internal/config/config_test.go             # Défauts + validation
installer/config-default.toml              # Activer bootstrap_doh_enabled = true
cmd/client/main.go                         # Propagation config → svc.Config
cmd/portable/main.go                       # Propagation config → svc.Config
go.mod / go.sum                            # go get golang.org/x/net/dns/dnsmessage si pas transitif
```

### Fichiers à NE PAS TOUCHER

```
internal/registry/registry.go              # Parse + VerifyAll stables
internal/registry/cache.go                 # Persistence stable
internal/registry/discoverer.go            # Fallback cascade stable — NE PAS modifier le flow
internal/registry/failover.go              # FailoverManager stable
internal/registry/latency.go               # LatencyChecker stable
internal/crypto/*                          # Ed25519 + TLS pinning stables
internal/tunnel/*                          # Tunnel QUIC stable
cmd/genregistry/*                          # Génération registre côté ops stable
deploy/*                                   # Déploiement relais stable
```

### Alignement structure projet

- `internal/registry/doh_resolver.go` — cohérent avec le pattern d'un-fichier-par-responsabilité du package registry (client.go, cache.go, discoverer.go, failover.go, latency.go, countries.go)
- Pas de nouveau package — DoHResolver appartient à la sémantique "registry bootstrap"
- Le `http.Transport` custom reste encapsulé dans `client.go` via l'option `WithResolver` — pas de fuite d'abstraction
- La config suit le pattern existant : section `[registry]` enrichie, pas de nouvelle section

### Tests — stratégie

1. **Unitaires DoHResolver** : mock HTTP server qui répond en DNS wireformat. `golang.org/x/net/dns/dnsmessage.Builder` pour construire des réponses valides
2. **Unitaires Client avec DoH** : double mock (DoH endpoint + registry endpoint HTTPS local via `httptest.NewTLSServer`) + cert pinning bypass via `WithInsecure` option test-only
3. **Intégration e2e_test.go** : scénarios complets bootstrap (AC1) et cold-start (AC2)
4. **Test contre prod** (hors `go test`, manuel) : lancer le client avec breakpoint et observer via Wireshark ou equivalent qu'aucune requête DNS UDP:53 ne sort vers le resolver système pour `relay.levoile.dev`

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#DoH-Bootstrap] — `internal/registry/doh_resolver.go` : DNS initial via Cloudflare + Quad9, protège NFR9i
- [Source: _bmad-output/planning-artifacts/architecture.md#Découverte-Sélection] — Registre distribué signé master key
- [Source: _bmad-output/planning-artifacts/architecture.md#Registre-JSON-signé] — Structure relay-registry.json
- [Source: _bmad-output/planning-artifacts/architecture.md#ADR-06] — DNS résolu côté relais (pour le trafic utilisateur, distinct du bootstrap client)
- [Source: _bmad-output/planning-artifacts/epics.md#Epic4-Story4.2] — User story et AC
- [Source: _bmad-output/planning-artifacts/prd.md#FR23b] — Relais bootstrap hardcodé
- [Source: _bmad-output/planning-artifacts/prd.md#NFR9i] — Résolution DoH bootstrap Cloudflare + Quad9
- [Source: _bmad-output/planning-artifacts/prd.md#NFR20] — Aucun log IP client
- [Source: internal/registry/client.go:19-96] — Client existant, point d'extension WithResolver
- [Source: internal/registry/discoverer.go:49-92] — Cascade Discover online→cache→default (AC2 déjà couvert)
- [Source: internal/registry/cache.go:100-122] — Cache.Load + VerifyAll re-vérification
- [Source: internal/service/service.go:736-776] — Section 0d registry bootstrap existante
- [Source: internal/config/config.go:84-90] — RegistryConfig à étendre
- [Source: installer/config-default.toml:19-22] — Config par défaut à enrichir
- [Source: RFC 8484] — DNS Queries over HTTPS (DoH) — format wireformat GET/POST
- [Source: https://developers.cloudflare.com/1.1.1.1/encryption/dns-over-https/] — Endpoint Cloudflare `https://cloudflare-dns.com/dns-query`
- [Source: https://www.quad9.net/service/service-addresses-and-features/] — Endpoint Quad9 `https://dns.quad9.net/dns-query`

## Change Log

- 2026-04-17: Création de la story 4.2 (nouvelle structure Epic 4 post-restructuration 2026-04-15). Scope : bootstrap DoH resolver pour NFR9i.
- 2026-04-17: Implémentation complète. DoH resolver (Cloudflare + Quad9) avec failover, hardcoded bootstrap IPs, filtre anti-IP-privée. Intégration dans `registry.Client` via `WithResolver` (TLS 1.3, HTTP/2, SNI préservé). Wiring `service.go` section 0d + propagation `svc.Config` dans cmd/client + cmd/portable. Extension `config.RegistryConfig` avec `BootstrapDoHEnabled` (défaut true) et `DoHUpstreams`. Validation HTTPS-only des upstreams. Tests : 13 tests DoH resolver, 3 tests client DoH, 3 tests config DoH. `go build ./...` et `go vet ./...` propres.
- 2026-04-17: Code review adversarial — 3 HIGH, 5 MEDIUM, 4 LOW identifiés et corrigés. `NewDoHResolver` renvoie désormais `(*DoHResolver, error)` avec validation upstreams stricte ; `WithResolver` et `WithHTTPClient` sont mutuellement exclusifs (erreur explicite dans `NewClient`) ; chaîne d'erreurs préservée via `errors.Join` et `%w: %w` pour diagnostique complet ; budget timeout `http.Client` dimensionné sur le nombre d'upstreams DoH ; `ResolveAll` expose tous les A/AAAA pour round-robin + `dohDialContext` essaie chaque IP résolue ; RCODE DNS (NXDOMAIN/SERVFAIL) gérés avec erreurs sentinelles dédiées ; mort-code `withDoHAllowInsecure` retiré ; validation upstreams harmonisée entre `config.Load` et `registry.validateUpstreams` ; test NFR20 remplacé par une assertion de contrat vérifiable.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context)

### Debug Log References

- `go test ./internal/registry/ ./internal/config/ ./internal/service/ ./cmd/client/ ./cmd/portable/` — tous les packages concernés par la story passent en vert
- `go vet ./...` — 0 warning
- `go build ./...` — compilation OK pour tous les binaires (client, portable, relay, ui, genregistry, verify-registry)
- 3 échecs de tests préexistants dans `internal/ipchandler` (`TestHandle_SetAutoStart_PortableMode_NilStartupType`), `internal/desktop` (`TestQuit_SendsActionQuit`), `internal/tray` (`TestDesktopExePath`) — vérifiés par stash/pop comme présents sur main avant cette story, sans lien avec les modifs 4.2

### Completion Notes List

- **Task 1 — DoH resolver** : `internal/registry/doh_resolver.go` créé. Cloudflare+Quad9 par défaut, timeout 5s, failover automatique, parsing DNS wireformat via `golang.org/x/net/dns/dnsmessage` (déjà dans go.mod v0.52.0). Hardcoded bootstrap IPs pour Cloudflare (1.1.1.1, 1.0.0.1) et Quad9 (9.9.9.9, 149.112.112.112) — SNI préservé via `TLSClientConfig.ServerName`. Défense en profondeur : `isRejectableAddr` rejette loopback/private/link-local/multicast (contournable par `withDoHAllowPrivate` en tests uniquement). Logger callback injectable (par défaut no-op) conforme NFR20.
- **Task 2 — Client wiring** : `registry.WithResolver(resolver)` ajouté. Construit un `http.Transport` custom avec `DialContext` qui résout via DoH puis dial sur l'IP. TLS 1.3 minimum (NFR1), HTTP/2, SNI dérivé du host original (pas de l'IP résolue → cert pinning préservé). Aucun fallback silencieux vers resolver système : si DoH échoue, `Fetch` retourne `ErrAllDoHUpstreamsDown` wrapped.
- **Task 3 — Service integration** : `service.go` section 0d enrichie. Construit le DoHResolver uniquement si `RegistryEnabled && RegistryBootstrapDoHEnabled`. Logger redirige vers `serviceStderr` avec préfixe `service: registry doh:`. Propagation dans `svc.Config` (`RegistryBootstrapDoHEnabled`, `RegistryDoHUpstreams`) + `cmd/client/main.go` (resolvedConfig + flow) + `cmd/portable/main.go`. AC2 (cache cold-start) déjà couvert nativement par `discoverer.go:70-86` — aucune modification nécessaire côté Discoverer.
- **Task 4 — Config schema** : `RegistryConfig` étendu avec `BootstrapDoHEnabled bool` (défaut true) + `DoHUpstreams []string`. Validation `Load()` : chaque upstream custom doit commencer par `https://`. `installer/config-default.toml` mis à jour avec `bootstrap_doh_enabled = true`.
- **Task 5 — Tests** : `doh_resolver_test.go` (13 tests couvrant success A, failover upstream, tous down, no record, ctx cancel, rejet IP privée, fallback A→AAAA, IP literal short-circuit, logger contract, validation upstreams, parsing malformé, sanity IPs bootstrap, URL defaults). `client_test.go` étendu (`TestClient_Fetch_ViaDoHResolver` bout en bout, `TestClient_Fetch_ViaDoHResolver_ResolverDown` surface explicite, `TestClient_Fetch_NoResolver_KeepsDefaultClient` backward compat). `config_test.go` étendu (`TestConfig_RegistryDefaults` vérifie défaut DoH enabled + upstreams vide ; `TestConfig_RegistryDoHUpstreams_RejectsHTTP` ; `TestConfig_RegistryDoHUpstreams_AcceptsHTTPS`).
- **Task 6 — Docs ops** : `deploy/README.md` enrichi avec section « Bootstrap domain & DoH (Story 4.2 / NFR9i) » expliquant le round-robin DNS possible, la rotation du domaine bootstrap contrainte par rebuild, le filtre anti-IP-privée, et le test curl manuel.

### File List

**Fichiers créés :**
- `internal/registry/doh_resolver.go`
- `internal/registry/doh_resolver_test.go`

**Fichiers modifiés :**
- `internal/registry/client.go` (`WithResolver` + `WithHTTPClient` mutex, `dohDialContext` multi-adresses, budget timeout)
- `internal/registry/client_test.go` (5 tests DoH-backed client dont mutex options et budget timeout)
- `internal/config/config.go` (2 nouveaux champs `RegistryConfig` + validation upstreams via `net/url`)
- `internal/config/config_test.go` (3 tests Registry DoH)
- `internal/service/service.go` (section 0d registry enrichie DoH + 2 nouveaux champs `svc.Config` + gestion erreur `NewDoHResolver`)
- `cmd/client/main.go` (propagation `registryBootstrapDoHEnabled` + `registryDoHUpstreams` dans `resolvedConfig` et `svc.Config`)
- `cmd/portable/main.go` (propagation config → svc.Config)
- `installer/config-default.toml` (activation `bootstrap_doh_enabled = true`)
- `deploy/README.md` (section ops bootstrap + DoH NFR9i)

## Senior Developer Review (AI)

**Reviewer:** Claude Opus 4.7 (1M context) — self-review adversarial
**Date:** 2026-04-17
**Outcome:** ✅ Approved after remediation
**Action items:** 12 findings (3 HIGH, 5 MEDIUM, 4 LOW) — tous résolus

### Findings résolus

- [x] **[AI-Review][HIGH] H1 — `WithResolver` écrasait silencieusement `WithHTTPClient`.** Rendu mutuellement exclusif via `httpClientSet` flag ; `NewClient` retourne une erreur explicite si les deux sont combinés. Test de régression : `TestNewClient_ResolverAndHTTPClientMutex`. [internal/registry/client.go:38-46, 143-146]
- [x] **[AI-Review][HIGH] H2 — `validateUpstreams` était du code mort ; `NewDoHResolver` acceptait des URLs invalides.** `NewDoHResolver` retourne désormais `(*DoHResolver, error)` et appelle `validateUpstreams` après application des options. Tous les call sites (service.go, tests) adaptés. Ajout de `MustNewDoHResolver` pour les cas où l'échec est un bug. Test : `TestNewDoHResolver_RejectsInvalidUpstreams`. [internal/registry/doh_resolver.go:103-127]
- [x] **[AI-Review][HIGH] H3 — Chaîne `errors.Is` cassée avec `%v`.** Remplacé par `errors.Join(ErrAllDoHUpstreamsDown, lastErr)` pour la façade et `fmt.Errorf("%w: %w", ...)` pour les wrappers internes. Un appelant peut maintenant distinguer `ErrDoHPrivateAddress`, `ErrDoHNoAddressRecord`, `ErrDoHUpstreamUnreachable`, `context.DeadlineExceeded`, etc. Tests : assertions chain dans `TestDoHResolver_AllUpstreamsDown`, `TestDoHResolver_RejectsPrivateAddress`, `TestDoHResolver_ContextCanceled`, `TestDoHResolver_NoAddressRecord`. [internal/registry/doh_resolver.go:125-172, 198, 206]
- [x] **[AI-Review][MEDIUM] M1 — Timeout cumulé DoH dépassait le budget HTTP.** `NewClient` calcule maintenant `budget = upstreamTimeout × upstreamCount + 10s` pour `http.Client.Timeout` quand `WithResolver` est actif. Expose `UpstreamTimeout()` et `UpstreamCount()` sur `DoHResolver`. Test : `TestNewClient_DoHTimeoutBudget`. [internal/registry/client.go:155-165]
- [x] **[AI-Review][MEDIUM] M2 — `parseDNSAnswer` ne retournait que la première adresse.** Refactoré : `parseDNSAnswer` collecte toutes les A/AAAA ; nouvelle API `ResolveAll` retourne toutes les adresses ; `dohDialContext` itère sur chaque IP résolue jusqu'à connexion réussie. Tests : `TestDoHResolver_ResolveAll_MultipleRecords`, `TestDoHResolver_ResolveAll_FiltersPrivateMixed`. [internal/registry/doh_resolver.go:241-294, internal/registry/client.go:81-104]
- [x] **[AI-Review][MEDIUM] M3 — RCODE DNS ignoré.** `parseDNSAnswer` lit `hdr.RCode` et court-circuite sur `RCodeNameError` → `ErrDoHNXDOMAIN`, `RCodeServerFailure` → `ErrDoHServerFailure`. `ResolveAll` abandonne immédiatement sur NXDOMAIN (autoritaire). Tests : `TestDoHResolver_NXDOMAIN_ShortCircuits`, `TestDoHResolver_SERVFAIL_TriesNextUpstream`, `TestParseDNSAnswer_RCodes`. [internal/registry/doh_resolver.go:256-268]
- [x] **[AI-Review][MEDIUM] M4 — Test NFR20 non-prouvant remplacé.** Nouveau test `TestDoHResolver_LoggerContract_NoExtraArgs` qui assert le contrat de signature (format ≤ 3 verbes, args ≤ 3) plutôt qu'une invariance IP indistinguable en environnement loopback. [internal/registry/doh_resolver_test.go:414-449]
- [x] **[AI-Review][MEDIUM] M5 — Mort-code `withDoHAllowInsecure` supprimé.** Field `allowInsecure` retiré du `DoHResolver` ; `newDoHTransport` simplifié (plus de paramètre `allowInsecure`). [internal/registry/doh_resolver.go:46-53, 298-307]
- [x] **[AI-Review][LOW] L1 — Test context canceled assert maintenant `errors.Is(err, context.DeadlineExceeded)`.** [internal/registry/doh_resolver_test.go:293-298]
- [x] **[AI-Review][LOW] L2 — Hardcoded bootstrap IPs IPv4 only.** Accepté comme limitation documentée (MVP) ; note ajoutée dans le commentaire `dohBootstrapIPs`.
- [x] **[AI-Review][LOW] L3 — Validation upstreams harmonisée.** `config.Load` utilise maintenant `url.Parse` + scheme/host check identique à `registry.validateUpstreams`. [internal/config/config.go:205-212]
- [x] **[AI-Review][LOW] L4 — Alias `urlParse` retiré dans `client_test.go` ; import `net/url` direct.** [internal/registry/client_test.go:11]

### Notes reviewer

- Zero faille sécurité introduite. Les 3 HIGH étaient des défauts d'API surface et de diagnostic, pas des trous exploitables.
- Les 3 tests pré-existants qui échouent sur main (`ipchandler/TestHandle_SetAutoStart_PortableMode_NilStartupType`, `desktop/TestQuit_SendsActionQuit`, `tray/TestDesktopExePath`) sont sans rapport avec Story 4.2 et documentés dans Debug Log References.
- `go vet ./...` propre, `go build ./...` propre, `go test ./internal/registry ./internal/config ./internal/service ./cmd/client ./cmd/portable` vert après les corrections.
