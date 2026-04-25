# Story 2.1: Client tunnel QUIC/HTTPS avec connexion au relais

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que le client établisse automatiquement un tunnel chiffré QUIC/HTTPS vers le relais via Cloudflare,
Afin que mes communications transitent par le relais de façon sécurisée et indiscernable du trafic web normal.

## Acceptance Criteria

1. **Given** le client démarre avec une configuration valide (domaine levoile.dev, clé publique Ed25519 du relais)
   **When** le client initie la connexion au relais
   **Then** un tunnel QUIC/HTTPS est établi via Cloudflare en moins de 3 secondes
   **And** le protocole TLS 1.3 minimum est utilisé

2. **Given** le client en cours de connexion
   **When** le certificat TLS du relais est présenté
   **Then** le client vérifie l'identité du relais via la clé publique Ed25519 embarquée
   **And** la connexion est refusée si la vérification échoue

3. **Given** le tunnel établi
   **When** l'état de la connexion change
   **Then** la machine d'état transite correctement entre `connected`, `connecting` et `disconnected`
   **And** l'état est exposé via un channel pour les composants dépendants

4. **Given** le trafic du tunnel capturé via Wireshark
   **When** on analyse les paquets
   **Then** le trafic est indiscernable de requêtes HTTPS standard vers Cloudflare
   **And** aucune signature protocolaire VPN n'est détectable

## Tasks / Subtasks

- [x] Task 1 — Implémenter la machine d'état du tunnel (AC: #3)
  - [x] 1.1 Créer `internal/tunnel/state.go` — type `ConnState` (string) avec constantes `StateConnected`, `StateConnecting`, `StateDisconnected`
  - [x] 1.2 Implémenter struct `StateManager` avec champ `current ConnState` protégé par `sync.RWMutex` et channel `updates chan ConnState`
  - [x] 1.3 Implémenter `NewStateManager() *StateManager` — état initial `StateDisconnected`, channel bufferisé (capacité 1)
  - [x] 1.4 Implémenter `(sm *StateManager) Set(state ConnState)` — met à jour l'état et envoie sur le channel (non-bloquant via select/default)
  - [x] 1.5 Implémenter `(sm *StateManager) Get() ConnState` — lecture thread-safe de l'état courant
  - [x] 1.6 Implémenter `(sm *StateManager) Updates() <-chan ConnState` — retourne le channel en lecture seule
  - [x] 1.7 Créer `internal/tunnel/state_test.go` :
    - [x] `TestStateManager_InitialState` — état initial = disconnected
    - [x] `TestStateManager_SetGet` — transitions valides
    - [x] `TestStateManager_Updates` — le channel reçoit les mises à jour
    - [x] `TestStateManager_Concurrent` — accès concurrent sûr (goroutines parallèles)

- [x] Task 2 — Implémenter le endpoint de vérification Ed25519 sur le relais (AC: #2)
  - [x] 2.1 Créer `internal/relay/verify_handler.go` — struct `VerifyHandler` avec champ `signingKey ed25519.PrivateKey`
  - [x] 2.2 Implémenter `NewVerifyHandler(signingKey ed25519.PrivateKey) *VerifyHandler`
  - [x] 2.3 Implémenter `ServeHTTP(w, r)` :
    - POST uniquement (405 sinon)
    - Content-Type `application/json` requis
    - Lire le body JSON : `{"nonce":"<base64_32_bytes>"}`
    - Valider le nonce : exactement 32 bytes après décodage base64
    - Signer le nonce avec `crypto.Sign(signingKey, nonceBytes)` (réutiliser `internal/crypto`)
    - Répondre JSON : `{"signature":"<base64_signature>"}`
    - Aucune donnée persistée (stateless)
  - [x] 2.4 Créer `internal/relay/verify_handler_test.go` :
    - [x] `TestVerifyHandler_ValidNonce` — nonce 32 bytes → signature vérifiable
    - [x] `TestVerifyHandler_InvalidMethod` — GET → 405
    - [x] `TestVerifyHandler_InvalidNonce` — nonce trop court → 400
    - [x] `TestVerifyHandler_EmptyBody` — body vide → 400
  - [x] 2.5 Modifier `internal/relay/server.go` — ajouter champ `SigningKey ed25519.PrivateKey` à la struct `Server`
  - [x] 2.6 Modifier le routage dans `NewServer()` : ajouter `mux.Handle("/verify", verifyHandler)` — SANS middleware de limitation (même pattern que `/health`)
  - [x] 2.7 Modifier `cmd/relay/main.go` — ajouter flag `-signing-key <path>` pour charger la clé privée Ed25519 (fichier base64). Si le flag n'est pas fourni, le endpoint `/verify` n'est pas enregistré (rétrocompatibilité)

- [x] Task 3 — Implémenter le client tunnel QUIC/HTTPS (AC: #1, #2, #4)
  - [x] 3.1 Créer `internal/tunnel/client.go` — struct `Client` avec champs :
    ```go
    type Client struct {
        relayDomain   string              // "levoile.dev"
        relayPubKey   ed25519.PublicKey    // Clé publique Ed25519 du relais
        httpClient    *http.Client        // Client HTTP/3 via http3.Transport
        transport     *http3.Transport    // Pour Close()
        state         *StateManager       // Machine d'état
    }
    ```
  - [x] 3.2 Implémenter `NewClient(relayDomain string, relayPubKeyBase64 string) (*Client, error)` :
    - Importer la clé publique via `crypto.ImportPublicKeyBase64()`
    - Créer `http3.Transport` avec TLS 1.3 :
      ```go
      tr := &http3.Transport{
          TLSClientConfig: &tls.Config{
              NextProtos: []string{http3.NextProtoH3},
              MinVersion: tls.VersionTLS13,
          },
          QUICConfig: &quic.Config{},
      }
      ```
    - Créer `http.Client{Transport: tr}`
    - Initialiser `StateManager`
  - [x] 3.3 Implémenter `(c *Client) Connect(ctx context.Context) error` :
    - Passer l'état à `connecting`
    - Vérifier l'identité du relais via `/verify` :
      1. Générer 32 bytes aléatoires (`crypto/rand`)
      2. POST `https://{relayDomain}/verify` avec `{"nonce":"<base64>"}`
      3. Lire la réponse `{"signature":"<base64>"}`
      4. Vérifier la signature via `crypto.Verify(relayPubKey, nonce, signature)`
      5. Si échec → état `disconnected`, retourner erreur
    - Si vérification OK → état `connected`
    - Timeout global de 3 secondes via `context.WithTimeout` (NFR9)
  - [x] 3.4 Implémenter `(c *Client) SendDoHQuery(ctx context.Context, dnsPayload []byte) ([]byte, error)` :
    - Vérifier que l'état est `connected`
    - POST `https://{relayDomain}/dns-query` avec content-type `application/dns-message`
    - Lire et retourner le body de la réponse
    - Timeout 5 secondes
  - [x] 3.5 Implémenter `(c *Client) Disconnect() error` :
    - Passer l'état à `disconnected`
    - Fermer le transport : `c.transport.Close()`
  - [x] 3.6 Implémenter `(c *Client) State() *StateManager` — accesseur pour la machine d'état
  - [x] 3.7 Implémenter `(c *Client) HTTPClient() *http.Client` — accesseur pour injection dans les tests

- [x] Task 4 — Tests du client tunnel (AC: #1, #2, #3)
  - [x] 4.1 Créer `internal/tunnel/client_test.go`
  - [x] 4.2 Helper de test : démarrer un serveur HTTP/3 local (réutiliser le pattern du relay) avec endpoint `/verify` et `/dns-query`
  - [x] 4.3 `TestClient_NewClient_ValidKey` — constructeur avec clé valide
  - [x] 4.4 `TestClient_NewClient_InvalidKey` — clé invalide → erreur
  - [x] 4.5 `TestClient_Connect_VerificationSuccess` — connexion + vérification Ed25519 → state connected
  - [x] 4.6 `TestClient_Connect_VerificationFailed` — fausse clé publique → state disconnected + erreur
  - [x] 4.7 `TestClient_Connect_Timeout` — serveur lent → timeout 3s
  - [x] 4.8 `TestClient_SendDoHQuery` — envoi requête DNS wire-format → réponse valide
  - [x] 4.9 `TestClient_SendDoHQuery_NotConnected` — envoi sans connexion → erreur
  - [x] 4.10 `TestClient_Disconnect` — déconnexion → state disconnected
  - [x] 4.11 `TestClient_StateTransitions` — vérifier la séquence connecting → connected → disconnected

- [x] Task 5 — Câbler le point d'entrée client (AC: #1, #2, #3)
  - [x] 5.1 Mettre à jour `cmd/client/main.go` :
    - Flags CLI : `-relay-domain` (défaut: "levoile.dev"), `-relay-pubkey` (base64, requis)
    - Signal handling : `signal.NotifyContext(ctx, SIGINT, SIGTERM)` (même pattern que le relay)
    - Créer `tunnel.NewClient(domain, pubkey)`
    - Appeler `client.Connect(ctx)`
    - Attendre le signal d'arrêt
    - Appeler `client.Disconnect()`
  - [x] 5.2 Afficher les transitions d'état via le channel `StateManager.Updates()` (temporaire pour debug — sera remplacé par IPC dans Epic 3). Utiliser `fmt.Printf` côté client pour le MVP uniquement.

- [x] Task 6 — Validation globale (AC: #1, #2, #3, #4)
  - [x] 6.1 `go build ./cmd/client/` — compilation sans erreur
  - [x] 6.2 `go build ./cmd/relay/` — compilation sans erreur (non-régression)
  - [x] 6.3 `go test ./internal/tunnel/...` — tous les tests passent
  - [x] 6.4 `go test ./internal/relay/...` — tous les tests passent (non-régression)
  - [x] 6.5 `go vet ./...` — aucun warning
  - [x] 6.6 Vérifier que le trafic HTTP/3 vers Cloudflare est du HTTPS standard (AC: #4 — propriété architecturale, pas de code spécial nécessaire)

## Dev Notes

### Contraintes architecturales critiques

- **Langage :** Go pur, aucun CGo. Module path : `github.com/velia-the-veil/le_voile`
- **Aucun log côté client** — Les erreurs sont propagées vers le tray via IPC (Epic 3). Pour cette story, les erreurs sont retournées. Le SEUL logging autorisé est le `fmt.Printf` temporaire dans `cmd/client/main.go` pour afficher les transitions d'état (sera retiré en Epic 3)
- **Aucun `panic`** — Toujours retourner `error`, jamais `panic`
- **`context.Context`** en premier argument de toute fonction bloquante/réseau
- **Error wrapping** : `fmt.Errorf("tunnel: connect: %w", err)` — préfixe `tunnel:` obligatoire

### Conventions de nommage Go (OBLIGATOIRES)

- **Packages :** minuscules, un mot : `tunnel`
- **Fichiers :** `snake_case.go` : `client.go`, `state.go`, `verify_handler.go`
- **Fonctions exportées :** `PascalCase` : `NewClient`, `Connect`, `SendDoHQuery`, `NewStateManager`
- **Fonctions privées :** `camelCase` : `verifyRelay`
- **Constructeurs :** Pattern `New` + type : `NewClient()`, `NewStateManager()`
- **Constantes exportées :** `PascalCase` : `StateConnected`, `StateConnecting`, `StateDisconnected`
- **Tests :** `TestNomType_NomMethode` : `TestClient_Connect_VerificationSuccess`, `TestStateManager_SetGet`
- **Table-driven tests** quand > 2 cas
- **Erreurs sentinelles** : `var ErrVerificationFailed = errors.New("tunnel: relay verification failed")`

### Error handling (OBLIGATOIRE)

- Wrapping systématique : `fmt.Errorf("tunnel: connect: %w", err)`
- Préfixe = nom du package : `tunnel:`
- Erreurs sentinelles :
  ```go
  var ErrVerificationFailed = errors.New("tunnel: relay verification failed")
  var ErrNotConnected = errors.New("tunnel: not connected")
  var ErrConnectionTimeout = errors.New("tunnel: connection timeout")
  ```
- Jamais de `panic` — toujours retourner `error`

### API quic-go HTTP/3 — Pattern client (v0.59.0)

```go
import (
    "github.com/quic-go/quic-go"
    "github.com/quic-go/quic-go/http3"
    "crypto/tls"
)

// Pattern recommandé pour le client HTTP/3 :
tr := &http3.Transport{
    TLSClientConfig: &tls.Config{
        NextProtos: []string{http3.NextProtoH3},
        MinVersion: tls.VersionTLS13,
    },
    QUICConfig: &quic.Config{},
}
defer tr.Close()

client := &http.Client{Transport: tr}
resp, err := client.Post("https://levoile.dev/dns-query",
    "application/dns-message", bytes.NewReader(dnsPayload))
```

**ATTENTION :** Utiliser `http3.Transport` (pas `http3.RoundTripper` qui est l'ancien nom). Le `Transport` gère le pooling de connexions et le réutilisation automatique.

**ATTENTION :** `tr.Close()` est OBLIGATOIRE pour libérer les ressources QUIC. Toujours appeler dans `Disconnect()`.

**0-RTT (optionnel, recommandé pour la performance) :**
```go
tr := &http3.Transport{
    TLSClientConfig: &tls.Config{
        ClientSessionCache: tls.NewLRUClientSessionCache(100),
        NextProtos:         []string{http3.NextProtoH3},
    },
}
```
Le session cache permet la reprise TLS 0-RTT pour les connexions ultérieures (< 3s NFR9).

### Protocole de vérification Ed25519 (application-layer)

**Pourquoi application-layer ?** En production, Cloudflare termine TLS côté CDN. Le client voit le certificat Cloudflare, pas celui du relais. La vérification Ed25519 du relais doit donc se faire au niveau applicatif, pas au niveau TLS.

**Protocole :**
1. Client génère 32 bytes aléatoires via `crypto/rand.Read()`
2. Client envoie POST `https://{domain}/verify` avec body JSON : `{"nonce":"<base64_nonce>"}`
3. Relais signe les bytes du nonce avec sa clé privée Ed25519 via `crypto.Sign()`
4. Relais répond JSON : `{"signature":"<base64_signature>"}`
5. Client vérifie via `crypto.Verify(relayPubKey, nonceBytes, signatureBytes)`
6. Si OK → tunnel vérifié. Si KO → erreur, déconnexion.

**Structs JSON :**
```go
// Côté relay (verify_handler.go)
type VerifyRequest struct {
    Nonce string `json:"nonce"` // base64-encoded 32 bytes
}

type VerifyResponse struct {
    Signature string `json:"signature"` // base64-encoded Ed25519 signature
}

// Côté client (client.go)
// Utilise les mêmes structs via un package partagé ou inline
```

**Sécurité :**
- Le nonce empêche les attaques par rejeu
- 32 bytes = 256 bits d'entropie — largement suffisant
- Le relais ne persiste rien (stateless) — il signe et oublie
- La signature Ed25519 est de 64 bytes

### Machine d'état du tunnel

```
                 Connect()
 disconnected ──────────────► connecting
       ▲                           │
       │                           │ Vérification OK
       │  Erreur ou                ▼
       │  Disconnect()         connected
       └───────────────────────────┘
```

**Channel Updates :** Bufferisé capacité 1, envoi non-bloquant (select/default). Si le consommateur est lent, la dernière mise à jour est perdue mais l'état courant reste lisible via `Get()`. Ce design évite les goroutines bloquées.

```go
func (sm *StateManager) Set(state ConnState) {
    sm.mu.Lock()
    sm.current = state
    sm.mu.Unlock()

    // Non-bloquant : si personne n'écoute, on skip
    select {
    case sm.updates <- state:
    default:
    }
}
```

### Modification du relais (Task 2) — Scope minimal

La Task 2 ajoute un endpoint `/verify` au relais existant. C'est une extension nécessaire pour la vérification d'identité côté client. **Modifications minimales :**

1. **Nouveau fichier** : `verify_handler.go` + `verify_handler_test.go`
2. **Modification `server.go`** : ajouter champ `SigningKey`, enregistrer `/verify` dans le routage
3. **Modification `cmd/relay/main.go`** : nouveau flag `-signing-key`

**IMPORTANT :** Le endpoint `/verify` est optionnel au démarrage du relais (flag non fourni → endpoint non enregistré). Cela garantit la rétrocompatibilité totale avec l'Epic 1. Les tests existants du relais NE DOIVENT PAS être cassés.

**IMPORTANT :** `/verify` NE DOIT PAS être limité par le `LimitMiddleware` (même pattern que `/health`).

### Résistance DPI (AC: #4) — Propriété architecturale

Le trafic est indiscernable du HTTPS standard **par design** :
- Le client se connecte à `levoile.dev` via Cloudflare CDN
- Cloudflare termine TLS — le trafic sortant du client est du QUIC/HTTPS standard vers une IP Cloudflare
- Les requêtes DoH sont des POST HTTPS normaux avec content-type `application/dns-message`
- Pour un observateur réseau (FAI, DPI), le trafic ressemble à du trafic web vers un site Cloudflare

**Aucun code spécial n'est nécessaire** pour l'AC #4. C'est une propriété de l'architecture (HTTP/3 via Cloudflare CDN). La validation se fait manuellement via capture Wireshark.

### Apprentissages des Stories 1.1, 1.2 et 1.3 (OBLIGATOIRE à respecter)

**De la Story 1.1 :**
- `go mod tidy` retire les dépendances non importées — c'est normal
- `Sign()` retourne `([]byte, error)` — suivre ce pattern
- Tests nommés `TestType_Method` — convention établie
- Ne pas committer de binaires
- Licence MIT

**De la Story 1.2 :**
- `http3.ConfigureTLSConfig()` est OBLIGATOIRE côté serveur — côté client, utiliser `http3.Transport` avec `TLSClientConfig`
- Le DoH handler utilise un `http.Client` injectable pour les tests — même pattern pour le client tunnel
- Le routage utilise `http.NewServeMux()` — ajouter `/verify` au mux existant
- quic-go v0.59.0 — API `Conn` (pas `Connection`)
- `httptest.NewServer` pour les mocks upstream — réutiliser ce pattern pour les tests client
- Le serveur HTTP/3 de test utilise des ports UDP dynamiques + polling readiness

**De la Story 1.3 :**
- `atomic.Int64` pour compteurs thread-safe
- Middleware pattern : wrapper uniquement les handlers concernés
- `/health` et `/verify` ne sont PAS limités par le middleware
- La struct `Server` a déjà les champs `Limiter *Limiter` et `StartTime time.Time`
- Le `LimitMiddleware` wraps uniquement `/dns-query`

### Tests — Pattern serveur HTTP/3 local pour les tests client

Le test du client tunnel nécessite un serveur HTTP/3 local. Réutiliser le pattern des tests du relais :

```go
func startTestRelay(t *testing.T, signingKey ed25519.PrivateKey) (addr string, cleanup func()) {
    t.Helper()

    // Générer cert TLS Ed25519
    _, privKey, _ := crypto.GenerateKeyPair()
    certPEM, keyPEM, _ := crypto.GenerateSelfSignedTLSCert(privKey)
    cert, _ := tls.X509KeyPair(certPEM, keyPEM)

    // Trouver un port UDP libre
    // ... (pattern freeUDPAddr de la Story 1.2)

    // Démarrer le serveur relay avec signing key
    // ...
}
```

**ATTENTION :** Les tests client utilisent `InsecureSkipVerify: true` dans le `TLSClientConfig` car le serveur de test utilise un certificat auto-signé. La vérification TLS du certificat est celle de Cloudflare en production ; la vérification Ed25519 de l'identité du relais est applicative (via `/verify`).

### Structure des fichiers à créer/modifier

```
internal/
├── tunnel/
│   ├── doc.go              # EXISTANT — Stub package, peut être supprimé si client.go existe
│   ├── state.go            # NOUVEAU — StateManager, ConnState, channel updates
│   ├── state_test.go       # NOUVEAU — Tests machine d'état
│   ├── client.go           # NOUVEAU — Client tunnel HTTP/3, Connect, SendDoHQuery, Disconnect
│   └── client_test.go      # NOUVEAU — Tests client tunnel avec mock relay
├── relay/
│   ├── server.go           # MODIFIER — Ajouter champ SigningKey, route /verify
│   ├── verify_handler.go   # NOUVEAU — VerifyHandler, signature nonce Ed25519
│   ├── verify_handler_test.go # NOUVEAU — Tests verification endpoint
│   └── ... (existants — NE PAS MODIFIER sauf server.go)
cmd/
├── client/
│   └── main.go             # MODIFIER — Câblage tunnel client + flags + shutdown
└── relay/
    └── main.go             # MODIFIER — Ajouter flag -signing-key
```

### NE PAS implémenter (hors scope Story 2.1)

- **Reconnexion automatique** (Story 2.3) — si le tunnel tombe, l'état passe à `disconnected` et c'est tout
- **Gestion DNS système** (Story 2.2) — pas de modification du resolver DNS
- **Kill switch DNS** (Story 2.3) — pas de blocage DNS
- **Watchdog** (Story 2.3) — pas de surveillance
- **Module config TOML** (`internal/config/`) — utiliser des flags CLI pour cette story
- **IPC** (Epic 3) — pas de communication inter-processus
- **Tray UI** (Epic 3) — pas d'interface graphique
- **Backoff exponentiel** (Story 2.3) — pas de logique de retry

### Dépendances — Aucune nouvelle

Cette story n'ajoute aucune nouvelle dépendance Go. Tout est réalisable avec :
- `github.com/quic-go/quic-go/http3` — déjà dans go.mod (v0.59.0)
- `crypto/ed25519` — bibliothèque standard Go
- `crypto/rand` — génération nonce
- `crypto/tls` — configuration TLS client
- `encoding/json` — messages vérification
- `encoding/base64` — encodage nonce/signature
- `sync` — RWMutex pour StateManager
- `internal/crypto` — réutilisation des fonctions existantes (Verify, ImportPublicKeyBase64, Sign, GenerateSelfSignedTLSCert)

### NFRs couverts par cette story

- **NFR1** : Communications client-relais chiffrées via QUIC/HTTPS (TLS 1.3 minimum) — `http3.Transport` avec `MinVersion: tls.VersionTLS13`
- **NFR2** : Authentification client-relais exclusivement Ed25519 — vérification via `/verify` endpoint
- **NFR4** : Trafic non identifiable comme VPN par DPI — HTTP/3 via Cloudflare CDN, indiscernable du trafic web
- **NFR9** : Établissement tunnel initial < 3 secondes — `context.WithTimeout(ctx, 3*time.Second)`

### Project Structure Notes

- Le package `internal/tunnel/` est actuellement un stub (`doc.go` uniquement) — `client.go` et `state.go` le remplissent
- Le `doc.go` peut être supprimé si le package contient d'autres fichiers Go
- L'ajout de `/verify` au relais est rétrocompatible (optionnel via flag)
- Aucun conflit détecté avec la structure existante des Stories 1.1, 1.2 et 1.3

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Core Architectural Decisions] — HTTP/3 DoH RFC 8484, TLS 1.3, Ed25519 auth unidirectionnelle
- [Source: _bmad-output/planning-artifacts/architecture.md#Communication & Protocole] — Client ↔ Relais via HTTPS POST DoH, Cloudflare CDN
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns & Consistency Rules] — Naming, error handling, concurrence, anti-patterns
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure & Boundaries] — Structure tunnel/, crypto/, relay/
- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.1] — Acceptance criteria BDD complets
- [Source: _bmad-output/planning-artifacts/prd.md#Tunnel & Connexion Réseau] — FR1 (tunnel QUIC/HTTPS), FR3 (auth Ed25519)
- [Source: _bmad-output/planning-artifacts/prd.md#Non-Functional Requirements] — NFR1 (TLS 1.3), NFR2 (Ed25519), NFR4 (résistance DPI), NFR9 (< 3s)
- [Source: _bmad-output/implementation-artifacts/1-1-initialisation-du-projet-et-module-cryptographique-ed25519.md] — API crypto : GenerateKeyPair, Sign, Verify, ImportPublicKeyBase64
- [Source: _bmad-output/implementation-artifacts/1-2-serveur-relais-http3-et-handler-dns-over-https.md] — Pattern serveur HTTP/3, DoH handler, http3.ConfigureTLSConfig, test e2e
- [Source: _bmad-output/implementation-artifacts/1-3-limiteur-de-connexions-monitoring-et-deploiement.md] — Middleware pattern, server struct avec Limiter/StartTime
- [Source: quic-go.net/docs/http3/client/] — API http3.Transport client v0.59.0
- [Source: pkg.go.dev/github.com/quic-go/quic-go/http3] — Documentation Go du package http3

## Change Log

- 2026-03-09: Implémentation complète de la Story 2.1 — Client tunnel QUIC/HTTPS avec vérification Ed25519 du relais, machine d'état, endpoint /verify côté relais, câblage CLI client et relay.
- 2026-03-09: Code review adversarial — 7 issues corrigées (1 CRITICAL, 2 HIGH, 4 MEDIUM). Test manquant ajouté (TestClient_SendDoHQuery), validation status HTTP dans SendDoHQuery, validation Content-Type dans VerifyHandler, gestion erreur encodage JSON, suppression doc.go stub, ajout Close() au StateManager, ajout tests routage /verify côté serveur.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Tous les tests passent au premier run sans correction nécessaire
- `go vet ./...` propre, zéro warning
- Builds client et relay OK

### Completion Notes List

- **Task 1:** Machine d'état `StateManager` avec `ConnState` (connected/connecting/disconnected), channel bufferisé non-bloquant, thread-safe via `sync.RWMutex`. 4 tests unitaires couvrant état initial, transitions, channel updates et accès concurrent.
- **Task 2:** Endpoint `/verify` ajouté au relais — `VerifyHandler` signe un nonce 32 bytes avec Ed25519. Non limité par le middleware (même pattern que `/health`). Flag `-signing-key` optionnel dans `cmd/relay/main.go` pour rétrocompatibilité. 4 tests unitaires (nonce valide, méthode invalide, nonce invalide, body vide). Zéro régression sur les tests relay existants.
- **Task 3:** Client tunnel `Client` avec `NewClient`, `Connect` (vérification Ed25519 applicative via `/verify`), `SendDoHQuery`, `Disconnect`. HTTP/3 via `http3.Transport` avec TLS 1.3 minimum. Timeout 3s pour connexion (NFR9), 5s pour DoH. Erreurs sentinelles (`ErrVerificationFailed`, `ErrNotConnected`, `ErrConnectionTimeout`).
- **Task 4:** 9 tests client tunnel avec serveur HTTP/3 de test local réutilisant le pattern relay. Couvre : constructeur valide/invalide, vérification succès/échec, timeout, DoH happy-path, DoH non connecté, déconnexion, transitions d'état.
- **Task 5:** `cmd/client/main.go` câblé avec flags `-relay-domain` et `-relay-pubkey`, signal handling SIGINT/SIGTERM, affichage temporaire des transitions d'état via `fmt.Printf`.
- **Task 6:** Validation globale — builds OK, tous tests passent (tunnel + relay + crypto), `go vet` propre. AC #4 (résistance DPI) est une propriété architecturale (HTTP/3 via Cloudflare CDN).

### File List

- `internal/tunnel/state.go` — NOUVEAU — Machine d'état du tunnel (ConnState, StateManager)
- `internal/tunnel/state_test.go` — NOUVEAU — Tests machine d'état (4 tests)
- `internal/tunnel/client.go` — NOUVEAU — Client tunnel HTTP/3 (Connect, SendDoHQuery, Disconnect)
- `internal/tunnel/client_test.go` — NOUVEAU — Tests client tunnel avec mock relay HTTP/3 (9 tests)
- `internal/relay/verify_handler.go` — NOUVEAU — Handler /verify (signature Ed25519 de nonce)
- `internal/relay/verify_handler_test.go` — NOUVEAU — Tests verify handler (4 tests)
- `internal/relay/server.go` — MODIFIÉ — Ajout champ SigningKey, route /verify conditionnelle
- `cmd/relay/main.go` — MODIFIÉ — Ajout flag -signing-key, chargement clé Ed25519
- `internal/relay/server_test.go` — MODIFIÉ — Ajout tests routage /verify (activé/désactivé)
- `internal/tunnel/doc.go` — SUPPRIMÉ — Stub package remplacé par doc comment dans state.go
- `cmd/client/main.go` — MODIFIÉ — Câblage tunnel client avec flags, signal handling, affichage état, Close() StateManager
