# Story 9.1 : Registre de relais et découverte dynamique

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que Le Voile découvre automatiquement les relais disponibles,
Afin de ne pas dépendre d'un seul relais configuré manuellement.

## Acceptance Criteria

**AC1 — Découverte via endpoint registre**
**Given** le client au démarrage
**When** il initie la découverte des relais
**Then** il interroge un endpoint de registre (JSON via HTTPS) qui retourne la liste des relais disponibles avec leurs domaines et clés publiques Ed25519

**AC2 — Vérification chain of trust Ed25519**
**Given** la liste des relais reçue
**When** chaque relais est validé
**Then** le client vérifie que la clé publique Ed25519 de chaque relais est signée par une clé maître de confiance (chain of trust)

**AC3 — Fallback cache local**
**Given** le registre inaccessible
**When** le client ne peut pas récupérer la liste
**Then** il utilise le dernier cache local des relais connus (fichier TOML) et le relais par défaut (levoile.dev)

**AC4 — Rafraîchissement automatique**
**Given** le registre de relais
**When** l'opérateur ajoute un nouveau relais
**Then** les clients le découvrent automatiquement au prochain cycle de rafraîchissement

## Tasks / Subtasks

- [x] **Task 1 : Module `internal/registry/` — Types et parsing** (AC: 1, 2)
  - [x] 1.1 Créer `internal/registry/registry.go` — struct `RelayEntry` : `ID string`, `Domain string`, `PublicKey string` (base64 Ed25519), `Signature string` (base64, signature par clé maître), `Added time.Time`
  - [x] 1.2 Struct `Registry` : `Version int`, `MasterPublicKey string` (base64), `Relays []RelayEntry`, `Updated time.Time`
  - [x] 1.3 Fonction `Parse(data []byte) (*Registry, error)` — décode JSON, valide `Version == 1`, vérifie `len(Relays) > 0`
  - [x] 1.4 Constantes : `CurrentVersion = 1`, `EndpointPath = "/.well-known/relay-registry.json"`

- [x] **Task 2 : Vérification chain of trust Ed25519** (AC: 2)
  - [x] 2.1 Fonction `VerifyRelaySignature(masterPubKey ed25519.PublicKey, entry RelayEntry) error` — décode `entry.PublicKey` base64, préfixe `"relay-key-v1:"` + relayPubKeyBytes, appelle `ed25519.Verify(masterPubKey, prefixedMsg, sigBytes)` ; retourne `ErrInvalidSignature` si échec
  - [x] 2.2 Méthode `(r *Registry) VerifyAll() ([]RelayEntry, error)` — décode `r.MasterPublicKey`, appelle `VerifyRelaySignature` pour chaque relais, retourne uniquement les relais vérifiés + erreur wrappée si aucun relais valide
  - [x] 2.3 Erreurs sentinelles : `ErrInvalidSignature`, `ErrNoValidRelays`, `ErrInvalidMasterKey`, `ErrRegistryEmpty`

- [x] **Task 3 : Client HTTP de découverte** (AC: 1, 4)
  - [x] 3.1 Créer `internal/registry/client.go` — struct `Client` : `registryURL string`, `masterPubKey ed25519.PublicKey`, `httpClient *http.Client`, `refreshInterval time.Duration`
  - [x] 3.2 Constructeur `NewClient(registryURL string, masterPubKeyBase64 string, opts ...ClientOption) (*Client, error)` — décode la clé maître, configure `httpClient` avec timeout 10s, `refreshInterval` par défaut 1h
  - [x] 3.3 Options : `WithHTTPClient(*http.Client)`, `WithRefreshInterval(time.Duration)`
  - [x] 3.4 Méthode `Fetch(ctx context.Context) ([]RelayEntry, error)` — GET `registryURL + EndpointPath`, parse JSON, `VerifyAll()`, retourne relais vérifiés triés par `Added` (plus récent en premier)
  - [x] 3.5 Retourner `fmt.Errorf("registry: fetch: %w", err)` pour toute erreur HTTP (status != 200, timeout, etc.)

- [x] **Task 4 : Cache local TOML** (AC: 3)
  - [x] 4.1 Créer `internal/registry/cache.go` — struct `Cache` : `path string`
  - [x] 4.2 Constructeur `NewCache(path string) *Cache`
  - [x] 4.3 Struct `CachedRegistry` TOML : `MasterPublicKey string`, `Updated time.Time`, `Relays []CachedRelay` où `CachedRelay` = `ID, Domain, PublicKey, Signature string`
  - [x] 4.4 Méthode `Save(entries []RelayEntry, masterPubKey string) error` — sérialise en TOML, écrit atomiquement (écriture dans fichier `.tmp` + rename)
  - [x] 4.5 Méthode `Load() ([]RelayEntry, string, error)` — lit le TOML, retourne les entries + la clé maître ; retourne `ErrCacheNotFound` si fichier absent
  - [x] 4.6 Chemin par défaut : `%AppData%/LeVoile/relay-cache.toml` (Windows), `~/.config/levoile/relay-cache.toml` (Unix) — utiliser `config.DefaultPath()` + `/relay-cache.toml`

- [x] **Task 5 : Découvreur orchestrateur** (AC: 1, 2, 3, 4)
  - [x] 5.1 Créer `internal/registry/discoverer.go` — struct `Discoverer` : `client *Client`, `cache *Cache`, `defaultRelay RelayEntry`, `mu sync.RWMutex`, `relays []RelayEntry`, `stopCh chan struct{}`
  - [x] 5.2 Constructeur `NewDiscoverer(client *Client, cache *Cache, defaultRelay RelayEntry) *Discoverer`
  - [x] 5.3 Méthode `Discover(ctx context.Context) ([]RelayEntry, error)` — tente `client.Fetch()` ; si succès → `cache.Save()` + retourne ; si échec → tente `cache.Load()` + `VerifyAll()` ; si cache absent → retourne `[]RelayEntry{defaultRelay}` (fallback ultime)
  - [x] 5.4 Méthode `Start(ctx context.Context) error` — appelle `Discover()` initial, puis lance goroutine de rafraîchissement périodique (`client.refreshInterval`), met à jour `relays` sous `mu`
  - [x] 5.5 Méthode `Stop()` — ferme `stopCh`, goroutine sort
  - [x] 5.6 Méthode `Relays() []RelayEntry` — retourne copie thread-safe sous `mu.RLock()`
  - [x] 5.7 Méthode `Primary() RelayEntry` — retourne `relays[0]` sous `mu.RLock()` (premier = meilleur ou défaut)

- [x] **Task 6 : Intégration config TOML** (AC: 1, 3)
  - [x] 6.1 Ajouter dans `internal/config/config.go` struct `RegistryConfig` : `Enabled bool`, `URL string`, `MasterPublicKey string`, `RefreshInterval duration` (défaut 1h)
  - [x] 6.2 Ajouter champ `Registry RegistryConfig` dans struct `Config`
  - [x] 6.3 Dans `Load()` : valeurs par défaut — `Enabled: false`, `URL: "https://levoile.dev"`, `RefreshInterval: 1h`
  - [x] 6.4 Pas de migration de config — champ optionnel, l'absence est gérée par les défauts

- [x] **Task 7 : Intégration service** (AC: 1, 3, 4)
  - [x] 7.1 Ajouter dans `internal/service/service.go` struct `Config` : `RegistryEnabled bool`, `RegistryURL string`, `RegistryMasterPubKey string`, `RegistryRefreshInterval time.Duration`
  - [x] 7.2 Ajouter champ `discoverer *registry.Discoverer` dans struct `Program`
  - [x] 7.3 Dans `run()`, **avant** la création du tunnel client (étape 2) : si `RegistryEnabled` → créer `registry.NewClient()` + `registry.NewCache()` + `registry.NewDiscoverer()`, appeler `discoverer.Discover(ctx)`, utiliser `discoverer.Primary()` pour obtenir `relayDomain` et `relayPubKey` au lieu des valeurs config statiques
  - [x] 7.4 Si `RegistryEnabled` et `Discover()` échoue ET cache absent → fallback sur `cfg.RelayDomain` + `cfg.RelayPubKey` (config statique, aucune erreur fatale)
  - [x] 7.5 Après démarrage tunnel : appeler `discoverer.Start(ctx)` pour le rafraîchissement périodique en arrière-plan
  - [x] 7.6 Dans `shutdown()` : appeler `discoverer.Stop()` (avant l'arrêt du tunnel)

- [x] **Task 8 : Intégration `cmd/client/main.go`**
  - [x] 8.1 Dans `resolveConfig()` : lire `cfg.Registry` depuis le TOML et mapper vers `svc.Config` les champs `RegistryEnabled`, `RegistryURL`, `RegistryMasterPubKey`, `RegistryRefreshInterval`

- [x] **Task 9 : Tests** (AC: 1, 2, 3, 4)
  - [x] 9.1 `internal/registry/registry_test.go` :
    - `TestParse_ValidJSON` — JSON valide avec 2 relais → parse OK, Version=1, 2 relais
    - `TestParse_InvalidJSON` — JSON malformé → erreur
    - `TestParse_EmptyRelays` — `"relays": []` → `ErrRegistryEmpty`
    - `TestParse_WrongVersion` — `"version": 2` → erreur
  - [x] 9.2 `internal/registry/registry_test.go` (chain of trust) :
    - `TestVerifyRelaySignature_Valid` — générer master keypair, signer relay pubkey avec préfixe `"relay-key-v1:"`, vérifier → nil
    - `TestVerifyRelaySignature_InvalidSignature` — signature aléatoire → `ErrInvalidSignature`
    - `TestVerifyRelaySignature_WrongMasterKey` — signer avec une clé, vérifier avec une autre → `ErrInvalidSignature`
    - `TestVerifyAll_MixedValidity` — 2 relais valides + 1 invalide → retourne 2 relais vérifiés
    - `TestVerifyAll_NoneValid` — tous invalides → `ErrNoValidRelays`
  - [x] 9.3 `internal/registry/cache_test.go` :
    - `TestCache_SaveAndLoad` — sauvegarder 2 entries, recharger, vérifier égalité
    - `TestCache_LoadNotFound` — fichier absent → `ErrCacheNotFound`
    - `TestCache_AtomicWrite` — vérifier que le fichier `.tmp` n'existe pas après Save réussi
  - [x] 9.4 `internal/registry/client_test.go` :
    - `TestClient_Fetch_Success` — httptest.Server retournant JSON valide signé → relais vérifiés retournés
    - `TestClient_Fetch_HTTPError` — serveur retournant 500 → erreur wrappée
    - `TestClient_Fetch_InvalidSignature` — JSON avec mauvaise signature → `ErrNoValidRelays`
    - `TestClient_Fetch_Timeout` — serveur qui bloque → erreur context
  - [x] 9.5 `internal/registry/discoverer_test.go` :
    - `TestDiscoverer_Discover_OnlineSuccess` — fetch OK → retourne relais, cache écrit
    - `TestDiscoverer_Discover_OfflineFallbackCache` — fetch échoue, cache existe → retourne cache
    - `TestDiscoverer_Discover_OfflineNoCache` — fetch échoue, pas de cache → retourne defaultRelay
    - `TestDiscoverer_Start_PeriodicRefresh` — vérifier que Fetch est appelé après refreshInterval (avec ticker mock ou court intervalle)
    - `TestDiscoverer_Relays_ThreadSafe` — appels concurrents Relays() + Start() sans race condition
  - [x] 9.6 `internal/config/config_test.go` :
    - `TestConfig_RegistryDefaults` — config vide → Registry.Enabled=false, URL="https://levoile.dev", RefreshInterval=1h
  - [x] 9.7 Validation build : `go build ./cmd/client/... ./cmd/tray/... ./cmd/relay/...` — compilation OK

## Dev Notes

### Architecture de la Story 9.1 : Nouveau module `internal/registry/`

Cette story introduit un **nouveau package** `internal/registry/` pour la découverte dynamique des relais. Le module est conçu pour être **opt-in** (`RegistryEnabled: false` par défaut) et **gracefully degradable** — si le registre est inaccessible et le cache absent, le comportement actuel (relais statique) est préservé.

```
internal/registry/
├── registry.go          # Types (Registry, RelayEntry), Parse(), constantes
├── registry_test.go     # Tests parse + chain of trust
├── client.go            # Client HTTP de découverte (Fetch)
├── client_test.go       # Tests client avec httptest
├── cache.go             # Cache TOML local (Save/Load)
├── cache_test.go        # Tests cache
├── discoverer.go        # Orchestrateur (Discover, Start/Stop, rafraîchissement)
└── discoverer_test.go   # Tests orchestrateur
```

### Chain of trust Ed25519 — implémentation exacte

Le registre contient une `master_public_key`. Chaque relais a un `signature` qui est la signature Ed25519 du message `"relay-key-v1:" + relayPublicKeyBytes` par la clé privée maître.

```go
// Côté opérateur (outil de gestion, hors scope story) :
msg := append([]byte("relay-key-v1:"), relayPubKeyBytes...)
sig := ed25519.Sign(masterPrivKey, msg)

// Côté client (vérification) :
msg := append([]byte("relay-key-v1:"), relayPubKeyBytes...)
ok := ed25519.Verify(masterPubKey, msg, sigBytes)
```

Le préfixe `"relay-key-v1:"` prévient les attaques de réutilisation de signature (empêche d'utiliser une signature de nonce `/verify` comme preuve d'autorisation relais).

**ATTENTION** : La clé maître est **différente** de la clé du relais. Le client embarque la clé maître dans le config (`registry.master_public_key`), pas la clé du relais. La clé du relais est découverte dynamiquement via le registre.

### Format JSON du registre

```json
{
  "version": 1,
  "master_public_key": "base64...",
  "relays": [
    {
      "id": "relay-iceland-1",
      "domain": "levoile.dev",
      "public_key": "base64...",
      "signature": "base64...",
      "added": "2026-03-01T00:00:00Z"
    }
  ],
  "updated": "2026-03-12T00:00:00Z"
}
```

Endpoint : `GET https://{registryURL}/.well-known/relay-registry.json`

### Format cache TOML local

```toml
master_public_key = "base64..."
updated = 2026-03-12T00:00:00Z

[[relays]]
id = "relay-iceland-1"
domain = "levoile.dev"
public_key = "base64..."
signature = "base64..."
```

Chemin : `%AppData%/LeVoile/relay-cache.toml` (Windows) / `~/.config/levoile/relay-cache.toml` (Unix)
Écriture atomique : écrire dans `relay-cache.toml.tmp` puis `os.Rename()`.

### Point d'intégration dans `service.go` — étape 2 du startup flow

L'insertion se fait **avant** la création du `tunnel.Client` dans `run()` :

```go
// EXISTANT : étape 2
// client, err := tunnel.NewClient(p.cfg.RelayDomain, p.cfg.RelayPubKey, ...)

// NOUVEAU : si registry activé, résoudre le relais dynamiquement
relayDomain := p.cfg.RelayDomain
relayPubKey := p.cfg.RelayPubKey

if p.cfg.RegistryEnabled {
    regClient, err := registry.NewClient(p.cfg.RegistryURL, p.cfg.RegistryMasterPubKey,
        registry.WithRefreshInterval(p.cfg.RegistryRefreshInterval))
    if err == nil {
        cachePath := filepath.Join(filepath.Dir(cfgPath), "relay-cache.toml")
        cache := registry.NewCache(cachePath)
        defaultRelay := registry.RelayEntry{
            ID: "default", Domain: relayDomain, PublicKey: relayPubKey,
        }
        p.discoverer = registry.NewDiscoverer(regClient, cache, defaultRelay)

        relays, err := p.discoverer.Discover(ctx)
        if err == nil && len(relays) > 0 {
            relayDomain = relays[0].Domain
            relayPubKey = relays[0].PublicKey
        }
        // Si échec : on garde relayDomain/relayPubKey statiques — pas d'erreur fatale
    }
}

client, err := tunnel.NewClient(relayDomain, relayPubKey, ...)
```

**ATTENTION** : Le `discoverer.Start(ctx)` est appelé **après** la connexion tunnel réussie, pour lancer le rafraîchissement périodique en arrière-plan. Le `discoverer.Stop()` est appelé dans `shutdown()` **avant** l'arrêt du tunnel.

### Aucune modification du module `internal/tunnel/`

Le tunnel client accepte déjà `relayDomain` et `relayPubKey` en paramètres du constructeur. La découverte dynamique résout ces valeurs **avant** la création du client. Aucune modification du tunnel n'est nécessaire pour cette story.

La story 9.2 (sélection par latence + failover) ajoutera la logique de changement de relais à runtime.

### Aucune modification du module `internal/relay/`

Le relay server n'est pas modifié. L'endpoint `/health` existant sera utilisé par la story 9.2 pour le health-check. L'endpoint de registre (`/.well-known/relay-registry.json`) est un fichier JSON statique servi séparément (Cloudflare Pages, GitHub Pages, ou nginx) — pas dans le binaire relay.

### Conventions Go à respecter (depuis architecture.md)

- **Nommage packages** : `registry` (minuscule, un mot)
- **Nommage fichiers** : `snake_case.go` — `registry.go`, `client.go`, `cache.go`, `discoverer.go`
- **Fonctions exportées** : `PascalCase` — `NewClient`, `Parse`, `VerifyAll`
- **Erreurs** : wrapping `fmt.Errorf("registry: %s: %w", op, err)`, sentinelles avec `errors.New`
- **Context** : premier paramètre de `Fetch()`, `Discover()`, `Start()`
- **Concurrence** : `sync.RWMutex` pour `relays`, goroutine de rafraîchissement gérée via `stopCh` + context
- **Tests** : co-localisés (`*_test.go`), table-driven quand > 2 cas, `testing` standard uniquement
- **Aucun log côté client** — erreurs retournées, jamais loggées
- **Aucun import circulaire** : `registry` n'importe pas `tunnel`, `service`, ou `config`

### Bibliothèques utilisées

- `crypto/ed25519` (Go standard) — vérification signatures, pas de dépendance externe
- `encoding/json` (Go standard) — parsing registre JSON
- `github.com/BurntSushi/toml v1.5.0` (déjà dans go.mod) — cache TOML
- `net/http` (Go standard) — client HTTP pour fetch registre (pas besoin de HTTP/3 pour le registre)
- `net/http/httptest` (Go standard) — tests client

### Leçons des stories précédentes (à appliquer)

- **Story 8.2** : interfaces locales pour découplage (`BlocklistChecker` dans `dns/proxy.go`) → ici `registry` est autonome, pas d'interface partagée nécessaire
- **Story 8.2** : `sync.RWMutex` pour lectures intensives → `Relays()` et `Primary()` utilisent `RLock`
- **Story 8.1** : écriture atomique fichier (`.tmp` + rename) → appliquée au cache TOML
- **Story 7.1** : `atomic.Bool` pour flags simples → pas nécessaire ici, `mu` suffit
- **Story 8.2** : constantes plutôt que chaînes magiques → `EndpointPath`, `CurrentVersion`

### Fichiers à créer

| Fichier | Description |
|---------|-------------|
| `internal/registry/registry.go` | Types, Parse(), VerifyRelaySignature(), VerifyAll(), constantes, erreurs |
| `internal/registry/registry_test.go` | Tests parse + chain of trust |
| `internal/registry/client.go` | Client HTTP, Fetch() |
| `internal/registry/client_test.go` | Tests client avec httptest |
| `internal/registry/cache.go` | Cache TOML, Save/Load |
| `internal/registry/cache_test.go` | Tests cache |
| `internal/registry/discoverer.go` | Orchestrateur, Discover/Start/Stop |
| `internal/registry/discoverer_test.go` | Tests orchestrateur |

### Fichiers à modifier

| Fichier | Modification |
|---------|-------------|
| `internal/config/config.go` | Ajout `RegistryConfig` struct + champ `Registry` dans `Config` + défauts dans `Load()` |
| `internal/config/config_test.go` | Test des défauts Registry |
| `internal/service/service.go` | Ajout champs `Config` (Registry*), champ `discoverer`, intégration dans `run()` + `shutdown()` |
| `cmd/client/main.go` | Mapping config Registry → service Config dans `resolveConfig()` |

### Fichiers à NE PAS toucher

- `internal/tunnel/` — aucune modification (relayDomain/pubKey résolus avant constructeur)
- `internal/relay/` — aucune modification (registre servi séparément)
- `internal/dns/` — non concerné
- `internal/ipc/` — non concerné (pas d'action IPC pour cette story)
- `internal/tray/` — non concerné (pas de contrôle tray pour cette story)
- `internal/watchdog/` — non concerné
- `internal/blocklist/` — non concerné
- `internal/crypto/` — on utilise `crypto/ed25519` standard directement, pas le wrapper `lecrypto`
- `cmd/relay/main.go` — non modifié
- `cmd/tray/main.go` — non modifié

### Vérification couverture AC

| AC | Couvert par |
|----|------------|
| AC1 (endpoint registre JSON HTTPS) | Task 1 (types), Task 3 (client HTTP Fetch), Task 5 (Discoverer orchestration) |
| AC2 (chain of trust Ed25519) | Task 2 (VerifyRelaySignature, VerifyAll) |
| AC3 (fallback cache local + relais défaut) | Task 4 (cache TOML), Task 5.3 (fallback cascade dans Discover) |
| AC4 (rafraîchissement auto) | Task 3.3 (refreshInterval), Task 5.4 (goroutine périodique) |

### Note sur le registre en production

Le registre `/.well-known/relay-registry.json` est un **fichier JSON statique** maintenu par l'opérateur. Il peut être :
- Servi par Cloudflare Pages sur `levoile.dev`
- Un fichier dans le repo GitHub
- Généré par un outil CLI (hors scope — outil opérateur, pas client)

L'outil de signature des clés relais (génération de la paire maître, signature des relais) est **hors scope** de cette story. Il sera un script CLI utilitaire séparé.

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 9, Story 9.1, AC1-4]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Naming Patterns", "Error Handling Patterns", "Concurrency Patterns", "Project Structure", "Config TOML"]
- [Source: `_bmad-output/planning-artifacts/prd.md` — Phase 2 "Découverte dynamique des relais", FR40]
- [Source: `internal/tunnel/client.go` — NewClient(relayDomain, relayPubKeyBase64), Connect(), SendDoHQuery()]
- [Source: `internal/config/config.go` — RelayConfig struct, Load(), Save(), Config struct]
- [Source: `internal/crypto/ed25519.go` — Verify(), ImportPublicKeyBase64(), ExportPublicKeyBase64()]
- [Source: `internal/service/service.go` — Program struct, run() flow étapes 1-13, shutdown() flow, Config struct]
- [Source: `internal/relay/health.go` — HealthResponse struct, /health endpoint (utilisé par Story 9.2)]
- [Source: `cmd/client/main.go` — resolveConfig(), mapping config → svc.Config]
- [Source: `_bmad-output/implementation-artifacts/8-2-filtrage-dns-local-et-controle-utilisateur-via-tray.md` — patterns interface locale, sync.RWMutex, écriture atomique]
- [Source: `go.mod` — Go 1.26, BurntSushi/toml v1.5.0, crypto/ed25519 standard]
- [Source: RFC 8032 — Ed25519 signature scheme]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

Aucun problème de debug rencontré. Tous les tests passent au premier essai.

### Completion Notes List

- Créé le package `internal/registry/` avec 4 fichiers source et 4 fichiers de test
- Implémenté le parsing JSON du registre avec validation version et contenu
- Implémenté la vérification chain of trust Ed25519 avec préfixe `"relay-key-v1:"` pour isolation des domaines de signature
- Implémenté le client HTTP avec options fonctionnelles (WithHTTPClient, WithRefreshInterval)
- Implémenté le cache TOML local avec écriture atomique (.tmp + rename)
- Implémenté le Discoverer orchestrateur avec cascade : online → cache → default relay
- Intégré dans `internal/config/config.go` : struct `RegistryConfig` avec défauts (Enabled=false, URL="https://levoile.dev", RefreshInterval="1h")
- Intégré dans `internal/service/service.go` : découverte dynamique avant tunnel connect, rafraîchissement périodique après, arrêt dans shutdown
- Intégré dans `cmd/client/main.go` : mapping config Registry → service Config
- 22 tests au total : 21 dans registry (parse, chain of trust, cache, client HTTP, discoverer) + 1 dans config (défauts)
- Compilation validée : `go build ./cmd/client/... ./cmd/tray/... ./cmd/relay/...` OK
- Aucune régression introduite dans les packages modifiés
- Les échecs pré-existants dans `cmd/portable`, `internal/dns`, `internal/tunnel` ne sont pas liés à cette story

### File List

**Fichiers créés :**
- `internal/registry/registry.go` — Types (Registry, RelayEntry), Parse(), VerifyRelaySignature(), VerifyAll(), constantes, erreurs sentinelles
- `internal/registry/client.go` — Client HTTP, NewClient(), Fetch(), options fonctionnelles
- `internal/registry/cache.go` — Cache TOML, NewCache(), Save(), Load(), écriture atomique
- `internal/registry/discoverer.go` — Orchestrateur, NewDiscoverer(), Discover(), Start(), Stop(), Relays(), Primary()
- `internal/registry/registry_test.go` — 9 tests : parse JSON + chain of trust Ed25519
- `internal/registry/client_test.go` — 4 tests : fetch success, HTTP error, invalid signature, timeout
- `internal/registry/cache_test.go` — 3 tests : save/load, not found, atomic write
- `internal/registry/discoverer_test.go` — 5 tests : online success, offline cache fallback, offline no cache, periodic refresh, thread safety

**Fichiers modifiés :**
- `internal/config/config.go` — Ajout RegistryConfig struct, champ Registry dans Config, défauts dans Load()
- `internal/config/config_test.go` — Ajout TestConfig_RegistryDefaults
- `internal/service/service.go` — Ajout import registry, champs Registry* dans Config, champ discoverer dans Program, intégration dans run() et shutdown()
- `cmd/client/main.go` — Ajout champs registry* dans resolvedConfig, mapping dans resolveConfig() et main()

## Change Log

- 2026-03-12 : Implémentation story 9.1 — Registre de relais et découverte dynamique. Nouveau package `internal/registry/` avec client HTTP, cache TOML, vérification chain of trust Ed25519, et orchestrateur de découverte. Intégration opt-in dans config, service et cmd/client.
- 2026-03-12 : Code review adversariale (Claude Opus 4.6) — 9 issues trouvées (3 HIGH, 4 MEDIUM, 2 LOW), toutes corrigées automatiquement :
  - **H1** (CRITICAL) : `Discoverer.Discover()` sauvegardait `registryURL` au lieu de `masterPublicKey` dans le cache → AC3 cassé. Fix : ajout `MasterPublicKeyBase64()` sur Client, utilisation dans Discoverer.
  - **H2** : Double appel `Discover()` au démarrage (explicite + dans `Start()`). Fix : `Start()` skip le discover initial si déjà appelé.
  - **H3** : Race condition sur `stopCh` — panic possible si `Stop()` avant `Start()`. Fix : init `stopCh` dans constructeur, `sync.Once` pour `Stop()`.
  - **M1** : `io.ReadAll` sans limite dans `Fetch()` — DoS potentiel. Fix : `io.LimitReader` (1 MB max).
  - **M2** : Champ `Added` perdu dans le cache TOML. Fix : ajout du champ dans `CachedRelay`.
  - **M3** : Pas de validation URL dans `NewClient`. Fix : `url.Parse()` au constructeur.
  - **L2** : Commentaires shutdown dupliqués `// 1b.` → corrigé en `// 1c.`
- 2026-03-12 : Code review adversariale #2 (Claude Opus 4.6) — 6 issues trouvées (1 CRITICAL, 2 HIGH, 2 MEDIUM, 1 LOW), toutes corrigées :
  - **C1** (CRITICAL) : `Fetch()` vérifiait les signatures avec la clé maître du JSON response au lieu de la clé de confiance du client → chain of trust AC2 cassée. Fix : `reg.MasterPublicKey = c.masterPubKeyBase64` avant `VerifyAll()`.
  - **H1** : `NewClient` validation URL inefficace — `url.Parse()` accepte chaînes vides, chemins relatifs. Fix : validation scheme + host non vides.
  - **H2** : `Parse()` sans validation des champs requis de `RelayEntry` — domaine/ID/clé/signature vides acceptés. Fix : validation loop ajoutée.
  - **M1** : Double-wrapping erreurs dans `Fetch()` — `"registry: fetch: registry: parse: ..."`. Fix : erreurs `Parse()`/`VerifyAll()` retournées sans re-wrapping.
  - **M2** : `Discover()` retournait erreur ET données (anti-pattern Go). Fix : fallback default retourne `nil` error.
  - **L1** : `Added` zero-value accepté silencieusement — documenté, non corrigé (comportement acceptable).
