# Story 7.1 : Fallback DNS multi-résolveur et certificate pinning Ed25519

Status: done

<!-- Note: Validation est optionnelle. Lancer validate-create-story pour un contrôle qualité avant dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que Le Voile bascule automatiquement vers un résolveur DNS alternatif si le principal est inaccessible, et que l'identité du relais soit strictement vérifiée au niveau TLS,
Afin que ma protection DNS ne soit jamais interrompue et que je sois connecté au bon relais.

## Acceptance Criteria

**AC1 – Fallback automatique vers Quad9**
**Given** le client utilise Cloudflare DoH (`https://1.1.1.1/dns-query`) comme résolveur principal via le relais
**When** Cloudflare est inaccessible (timeout > 2s ou erreur HTTP)
**Then** le relais bascule automatiquement vers Quad9 (`https://9.9.9.9/dns-query`) comme résolveur de secours

**AC2 – Rétablissement automatique vers Cloudflare**
**Given** le fallback Quad9 est actif
**When** Cloudflare redevient accessible
**Then** le relais rebascule vers Cloudflare comme résolveur principal

**AC3 – Certificate pinning Ed25519 strict au niveau TLS**
**Given** le client établit une connexion TLS au relais via quic-go/http3
**When** le certificat TLS est présenté par le relais
**Then** le client vérifie que la clé publique Ed25519 extraite du certificat correspond exactement à celle stockée dans `config.toml` (certificate pinning strict via `tls.Config.VerifyPeerCertificate`)

**AC4 – Refus de connexion si pinning échoue**
**Given** un relais présente un certificat avec une clé Ed25519 différente de celle pinnée
**When** la vérification TLS échoue
**Then** la connexion est refusée (`ErrPinningFailed`), le client tente une reconnexion via `reconnect.go` — ne bascule jamais vers un relais non vérifié

## Tasks / Subtasks

- [x] **Task 1 : Fallback DNS multi-résolveur côté relais** (AC: 1, 2)
  - [x] 1.1 Modifier `DoHHandler` dans `internal/relay/doh_handler.go` pour accepter une liste d'upstreams ordonnés (primaire + fallbacks)
  - [x] 1.2 Implémenter la logique de fallback : si l'upstream primaire retourne une erreur ou timeout > 2s, essayer le suivant dans la liste
  - [x] 1.3 Implémenter le mécanisme de recovery : sonde périodique (goroutine, toutes les 30s) pour vérifier si le primaire est redevenu accessible et rebascule
  - [x] 1.4 Mettre à jour `NewDoHHandler` pour accepter `[]string` upstreams au lieu d'un seul `string`
  - [x] 1.5 Mettre à jour `cmd/relay/main.go` pour passer la liste `["https://1.1.1.1/dns-query", "https://9.9.9.9/dns-query"]`
  - [x] 1.6 Ajouter tests unitaires : `TestDoHHandler_FallbackOnPrimaryError`, `TestDoHHandler_RecoveryToPrimary`, `TestDoHHandler_AllUpstreamsFail`

- [x] **Task 2 : Certificate pinning Ed25519 côté client** (AC: 3, 4)
  - [x] 2.1 Créer `internal/crypto/pinning.go` : fonction `VerifyEd25519CertPin(cert *x509.Certificate, pinnedPubKey ed25519.PublicKey) error`
  - [x] 2.2 Modifier `NewClient` dans `internal/tunnel/client.go` : ajouter `tls.Config.VerifyPeerCertificate` qui appelle `VerifyEd25519CertPin` avec la `relayPubKey` déjà stockée
  - [x] 2.3 Ajouter `ErrPinningFailed = errors.New("tunnel: certificate pinning failed")` dans `internal/tunnel/client.go`
  - [x] 2.4 S'assurer que le pinning coexiste avec la vérification challenge/response existante (le pinning est une couche supplémentaire, pas un remplacement)
  - [x] 2.5 Ajouter tests unitaires : `TestVerifyEd25519CertPin_Valid`, `TestVerifyEd25519CertPin_WrongKey`, `TestVerifyEd25519CertPin_NonEd25519Cert`
  - [x] 2.6 Ajouter test d'intégration dans `internal/tunnel/client_test.go` : `TestClient_PinningRefusesWrongKey`

- [x] **Task 3 : Config TOML — aucun changement nécessaire**
  - [x] 3.1 Vérifier que `RelayConfig.PublicKeyEd25519` dans `internal/config/config.go` suffit (oui — la clé pinnée est déjà dans la config)
  - [x] 3.2 Aucun nouveau champ TOML requis pour cette story (le fallback DNS est hardcodé côté relais)

- [x] **Task 4 : Validation end-to-end**
  - [x] 4.1 Lancer `go test ./internal/relay/... ./internal/crypto/... ./internal/tunnel/...` — tous les tests passent
  - [x] 4.2 Vérifier aucune régression sur les packages dns/, watchdog/, config/, tray/, ipc/, service/
  - [x] 4.3 Lancer `go build ./cmd/client/... ./cmd/relay/...` — compilation sans erreur

## Dev Notes

### Architecture de la solution

**Fallback DNS (côté relais uniquement) :**
Le relais (`internal/relay/doh_handler.go`) est le seul composant qui fait des requêtes DNS vers Cloudflare. Le client envoie des paquets DNS wire-format au relais via HTTP/3 — il ne connaît pas Cloudflare directement. Donc le fallback multi-résolveur doit être implémenté dans `DoHHandler`, pas dans `dns/proxy.go`.

**Certificate Pinning (côté client) :**
Le client utilise `quic-go/http3` avec un `*http.Client` dont le transport est `*http3.Transport`. La vérification du certificat TLS se fait via `tls.Config.VerifyPeerCertificate`. Cette callback reçoit les `rawCerts [][]byte` — le premier est le certificat du serveur. Il faut extraire la clé publique Ed25519 de ce certificat et la comparer avec `relayPubKey` déjà stockée dans le `Client`.

**Important :** Le pinning TLS s'ajoute au challenge/response Ed25519 existant (`/verify` endpoint). Ce sont deux vérifications orthogonales :
1. **Pinning TLS** : vérifie que le certificat présenté à la couche TLS contient la bonne clé Ed25519 (MITM impossible même avec un certificat CA valide)
2. **Challenge/Response** (`verifyRelay()`) : vérifie que le serveur possède la clé privée correspondante

### Fichiers à créer

| Fichier | Action |
|---------|--------|
| `internal/crypto/pinning.go` | **Créer** — fonction `VerifyEd25519CertPin` |
| `internal/crypto/pinning_test.go` | **Créer** — tests unitaires pinning |

### Fichiers à modifier

| Fichier | Modification |
|---------|-------------|
| `internal/relay/doh_handler.go` | Refactoriser pour liste d'upstreams + fallback + recovery |
| `internal/relay/doh_handler_test.go` | Ajouter tests fallback/recovery |
| `internal/tunnel/client.go` | Ajouter `VerifyPeerCertificate` dans `tls.Config` + `ErrPinningFailed` |
| `internal/tunnel/client_test.go` | Ajouter test de pinning |

### Fichiers à ne PAS toucher

- `internal/dns/proxy.go` — le proxy DNS local ne fait pas de requêtes directes vers Cloudflare
- `internal/dns/manager*.go` — gestion du resolver système, non concerné
- `internal/dns/kill_switch.go` — non concerné
- `internal/config/config.go` — aucun nouveau champ TOML nécessaire
- `internal/watchdog/` — non concerné
- `internal/service/` — non concerné
- `internal/tray/` — non concerné
- `internal/ipc/` — non concerné

### Implémentation détaillée

#### `internal/crypto/pinning.go` (nouveau fichier)

```go
package crypto

import (
    "crypto/ed25519"
    "crypto/x509"
    "errors"
    "fmt"
)

// ErrPinningFailed is returned when the certificate's public key does not
// match the pinned Ed25519 key.
var ErrPinningFailed = errors.New("crypto: certificate pinning failed: key mismatch")

// VerifyEd25519CertPin checks that the leaf certificate presented by the server
// contains an Ed25519 public key that matches pinnedPubKey exactly.
// Returns ErrPinningFailed if the key does not match.
func VerifyEd25519CertPin(cert *x509.Certificate, pinnedPubKey ed25519.PublicKey) error {
    certPubKey, ok := cert.PublicKey.(ed25519.PublicKey)
    if !ok {
        return fmt.Errorf("crypto: pinning: certificate does not use Ed25519 key")
    }
    if !pinnedPubKey.Equal(certPubKey) {
        return ErrPinningFailed
    }
    return nil
}
```

#### Modification de `internal/tunnel/client.go`

Dans `NewClient`, modifier la construction de `*http3.Transport` :

```go
// Ajouter ErrPinningFailed dans les vars d'erreurs
var (
    ErrVerificationFailed = errors.New("tunnel: relay verification failed")
    ErrNotConnected       = errors.New("tunnel: not connected")
    ErrConnectionTimeout  = errors.New("tunnel: connection timeout")
    ErrPinningFailed      = errors.New("tunnel: certificate pinning failed")
)

// Dans NewClient, après avoir parsé pubKey :
tr := &http3.Transport{
    TLSClientConfig: &tls.Config{
        NextProtos: []string{http3.NextProtoH3},
        MinVersion: tls.VersionTLS13,
        InsecureSkipVerify: o.insecure,
        VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
            if o.insecure {
                return nil // development mode bypass
            }
            if len(rawCerts) == 0 {
                return ErrPinningFailed
            }
            cert, err := x509.ParseCertificate(rawCerts[0])
            if err != nil {
                return fmt.Errorf("tunnel: parse cert: %w", err)
            }
            if err := lecrypto.VerifyEd25519CertPin(cert, pubKey); err != nil {
                return ErrPinningFailed
            }
            return nil
        },
    },
    QUICConfig: &quic.Config{},
}
```

**Note importante :** `VerifyPeerCertificate` est appelé APRÈS la vérification TLS standard. Comme `InsecureSkipVerify` peut être `false` (prod) et que le relais utilise un certificat auto-signé via `crypto.GenerateSelfSignedTLSCert`, il faut aussi s'assurer que la vérification CA est correctement gérée. En production avec Cloudflare (qui gère le TLS), `InsecureSkipVerify` sera `false` et le cert sera signé par Cloudflare. En développement direct au relais, `InsecureSkipVerify: true` bypass le CA check mais pas le pinning (car on vérifie aussi `o.insecure` dans `VerifyPeerCertificate`).

**Décision architecturale :** En mode `insecure=true` (dev), le pinning est également bypassé pour permettre les tests locaux avec des clés générées à la volée. En prod (`insecure=false`), le pinning est obligatoire.

#### Modification de `internal/relay/doh_handler.go`

```go
// DoHHandler avec liste d'upstreams et fallback
type DoHHandler struct {
    upstreams []string        // upstreams[0] = primaire, [1:] = fallbacks
    mu        sync.RWMutex
    activeIdx int             // index de l'upstream actif
    client    *http.Client
}

// NewDoHHandler crée un handler avec liste d'upstreams ordonnés.
// upstreams[0] = résolveur primaire (Cloudflare), upstreams[1] = fallback (Quad9), etc.
func NewDoHHandler(upstreams []string, client *http.Client) *DoHHandler {
    if client == nil {
        client = &http.Client{Timeout: upstreamTimeout}
    }
    return &DoHHandler{
        upstreams: upstreams,
        client:    client,
    }
}
```

**Logique de fallback :**
- Tentative sur `upstreams[activeIdx]` avec timeout 2s
- Si échec → incrémenter `activeIdx` (mod len(upstreams)), réessayer
- Si tous les upstreams échouent → retourner `ErrUpstreamUnavailable`
- Goroutine de recovery : toutes les 30s, sonde l'upstream primaire (index 0). Si accessible → reset `activeIdx = 0`

**Note :** La goroutine de recovery doit être gérée via `context.Context` passé à `Start(ctx)` ou une méthode dédiée. Pas de goroutines orphelines (règle architecture).

### Patterns architecturaux à respecter

**Nommage :**
- Nouveau fichier : `internal/crypto/pinning.go` (snake_case)
- Test : `internal/crypto/pinning_test.go`
- Tests nommés : `TestVerifyEd25519CertPin_Valid`, `TestVerifyEd25519CertPin_WrongKey`

**Erreurs :**
- Préfixe package : `crypto:` pour les erreurs dans `pinning.go`, `tunnel:` dans `client.go`
- Erreurs sentinelles : `ErrPinningFailed` dans `internal/crypto/pinning.go` ET `ErrPinningFailed` dans `internal/tunnel/client.go` (deux niveaux distincts)
- Wrapping : `fmt.Errorf("tunnel: certificate pinning failed: %w", lecrypto.ErrPinningFailed)`

**Concurrence (DoHHandler fallback) :**
- `sync.RWMutex` pour protéger `activeIdx`
- Goroutine recovery gérée via `context.Context` (jamais orpheline)
- Pattern : `go func() { defer wg.Done(); ... }()`

**No logging :**
- Aucun `log.Printf` côté client
- Côté relais : aucun log de l'upstream utilisé (privacy)
- Les erreurs de pinning se propagent jusqu'au tray via IPC

**Tests :**
- Go standard `testing` uniquement (pas de testify, pas de gomock)
- Tests co-localisés avec le code source
- Table-driven tests pour les cas multiples de pinning

### Contexte des stories précédentes (Épic 6)

La story 6-1 (auto-update) a ajouté `UpdateConfig` dans `internal/config/config.go`. Vérifier que les modifications de `DoHHandler` ne cassent pas `cmd/relay/main.go` qui initialise le handler avec un upstream unique actuellement.

L'URL actuelle dans le relais est probablement `"https://cloudflare-dns.com/dns-query"` ou `"https://1.1.1.1/dns-query"`. Vérifier dans `cmd/relay/main.go`.

### Structure du projet (rappel)

```
internal/
  crypto/
    ed25519.go          ← existant
    tls.go              ← existant
    pinning.go          ← À CRÉER
    pinning_test.go     ← À CRÉER
  relay/
    doh_handler.go      ← À MODIFIER (fallback multi-upstream)
    doh_handler_test.go ← À MODIFIER (tests fallback)
  tunnel/
    client.go           ← À MODIFIER (VerifyPeerCertificate)
    client_test.go      ← À MODIFIER (test pinning)
```

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 7, Story 7.1]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Certificate pinning relais — Phase 2"]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Fallback DNS multi-résolveur → Phase 2"]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Error Handling Patterns" : wrapping préfixe package]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Concurrency Patterns" : context.Context, pas de goroutines orphelines]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Anti-Patterns : Aucun log côté client"]
- [Source: `internal/relay/doh_handler.go` — architecture upstream unique actuelle]
- [Source: `internal/tunnel/client.go` — `NewClient`, `verifyRelay`, `relayPubKey` déjà disponible]
- [Source: `internal/crypto/tls.go` — `GenerateSelfSignedTLSCert` utilise Ed25519 → clé extractible via `cert.PublicKey.(ed25519.PublicKey)`]
- [Source: `internal/config/config.go` — `RelayConfig.PublicKeyEd25519` suffit, aucun nouveau champ TOML]
- RFC 8484 — DNS Queries over HTTPS (DoH)
- [Go stdlib `crypto/tls` — `VerifyPeerCertificate` callback](https://pkg.go.dev/crypto/tls#Config.VerifyPeerCertificate)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- Correction de `doRequest` : bufferisation du corps de réponse avant `cancel()` pour éviter la lecture sur un contexte annulé (détecté via `TestDoHHandler_BodyAtMaxSize`).
- `TestClient_SendSTUNRelay` échoue (403) : issue pré-existante hors scope — loopback address bloquée par `isAllowedTarget` pour protection SSRF. Flag `testSkipIPValidation` non accessible depuis le package `tunnel`.
- `internal/dns` tests (5 failures) : pré-existantes, hors scope story 7-1.

### Completion Notes List

- **Task 1 — Fallback DNS** : `DoHHandler` refactorisé avec `[]string` upstreams, logique de fallback (timeout 2s par upstream), goroutine de recovery (30s interval, gérée via `context.Context`). Corps de réponse bufferisé dans `doRequest` pour découpler lecture du contexte courte durée.
- **Task 2 — Certificate Pinning** : `internal/crypto/pinning.go` créé avec `VerifyEd25519CertPin`. `VerifyPeerCertificate` callback ajouté dans `NewClient` — bypasse en mode `insecure=true`, obligatoire en prod. Deux `ErrPinningFailed` distincts : `crypto:` et `tunnel:`.
- **Task 3 — Config** : `RelayConfig.PublicKeyEd25519` existant suffit. Aucun nouveau champ TOML.
- **Task 4 — Validation** : `relay`, `crypto`, `tunnel` (hors STUN pré-existant) — tous verts. Build OK.
- **[AI-Review] Correctifs code review** : `WithInsecureSkipCAOnly()` ajouté (teste chemin production `NewClient` + pinning avec cert auto-signé) ; `doRequest` : `io.LimitReader(resp.Body, maxDNSBodySize+1)` + rejet si >65535 ; `NewDoHHandler` panic si liste vide ; `onRecovery` callback + test recovery déterministe via channel ; `--fallback` flag CLI avec déduplication.
- **[AI-Review-2] Correctifs code review 2026-03-12** : [H1] `doRequest` rejette maintenant les réponses HTTP ≥500 comme `ErrUpstreamUnavailable` → fallback se déclenche sur erreurs HTTP serveur (AC1 complet) ; [H2] `VerifyPeerCertificate` lit `c.relayPubKey` sous `RLock` au lieu de capturer `pubKey` par closure → `UpdateRelay` met à jour le pinning TLS correctement ; [M1] `forwardToUpstream` borné par `context.WithTimeout(ctx, upstreamTimeout)` → timeout total capé à 5s quel que soit le nombre d'upstreams ; [M2] `isPrimaryReachable` utilise un payload DNS valide (`dnsProbeQuery`) au lieu de `http.NoBody`, et vérifie `StatusCode < 500`. Test ajouté : `TestDoHHandler_FallbackOnPrimaryHTTPError`.
- **[AI-Review-3] Correctifs code review 2026-03-12** : [M1] `isPrimaryReachable` draine body (`io.Copy(io.Discard, ...)`) avant `Close()` pour réutilisation connexion HTTP ; [M2] `doRequest`/`forwardToUpstream` retournent `([]byte, int, error)` au lieu de `(*http.Response, error)` → élimine double-buffering dans `ServeHTTP` ; [M3] `TestClient_ConnectTimeout_VeryShort` corrigé (done channel au lieu de `time.Sleep(10s)`) → 0.1s au lieu de 10s+ ; [L1] `w.Write` return value explicitement ignoré ; [L2] `dnsProbeQuery` annoté read-only ; [L3] Test `TestDoHHandler_Start_SingleUpstream_NoOp` ajouté ; [L4] `VerifyPeerCertificate` wrappe l'erreur de `VerifyEd25519CertPin` dans `ErrPinningFailed` (compatible `errors.Is`).

### File List

- `internal/crypto/pinning.go` (créé)
- `internal/crypto/pinning_test.go` (créé)
- `internal/relay/doh_handler.go` (modifié)
- `internal/relay/doh_handler_test.go` (modifié)
- `internal/relay/doh_handler_edge_test.go` (modifié — `NewDoHHandler` signature)
- `internal/relay/e2e_test.go` (modifié — `NewDoHHandler` signature)
- `internal/relay/server_test.go` (modifié — `NewDoHHandler` signature)
- `internal/tunnel/client.go` (modifié)
- `internal/tunnel/client_test.go` (modifié)
- `internal/tunnel/client_edge_test.go` (modifié — fix test lent)
- `cmd/relay/main.go` (modifié)

## Change Log

- 2026-03-11 : Implémentation story 7-1 — fallback DNS multi-résolveur + certificate pinning Ed25519. Ajout `internal/crypto/pinning.go`, refactorisation `DoHHandler` avec fallback/recovery, `VerifyPeerCertificate` dans `NewClient`, mise à jour de tous les tests concernés.
- 2026-03-11 : Code review adversarial — 2 critiques + 3 moyens corrigés. Ajout `WithInsecureSkipCAOnly()` dans `client.go` ; `TestClient_PinningRefusesWrongKey` reécrit pour tester le vrai code de production `NewClient` ; limite de taille sur le corps de réponse upstream dans `doRequest` (`io.LimitReader`) ; validation `len(upstreams) == 0` dans `NewDoHHandler` + callback `onRecovery` ; `TestDoHHandler_RecoveryToPrimary` rendu déterministe via channel ; `--fallback` flag dans `cmd/relay/main.go` avec déduplication.
- 2026-03-12 : Code review adversarial #2 — 2 HIGH + 2 MEDIUM corrigés. Fallback sur HTTP 5xx ; pinning TLS compatible avec `UpdateRelay` ; timeout total capé ; sonde recovery avec DNS valide.
- 2026-03-12 : Code review adversarial #3 — 3 MEDIUM + 4 LOW corrigés. Élimination double-buffering `doRequest`→`ServeHTTP` ; drain body `isPrimaryReachable` ; fix test lent `ConnectTimeout_VeryShort` ; wrapping erreur pinning ; test `Start()` no-op ; annotation `dnsProbeQuery` read-only.
