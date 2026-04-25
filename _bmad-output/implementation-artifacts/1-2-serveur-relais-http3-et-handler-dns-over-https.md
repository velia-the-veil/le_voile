# Story 1.2: Serveur relais HTTP/3 et handler DNS-over-HTTPS

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'opérateur,
Je veux un relais HTTP/3 qui reçoit les requêtes DoH et les relaie vers les résolveurs publics,
Afin que les clients puissent résoudre le DNS de façon chiffrée via le relais.

## Acceptance Criteria

1. **Given** le binaire relais démarré sur un port configuré
   **When** un client envoie un POST HTTPS sur `/dns-query` avec un corps DNS wire-format (RFC 8484, content-type `application/dns-message`)
   **Then** le relais forward la requête vers Cloudflare DNS (1.1.1.1) et retourne la réponse DNS au client
   **And** aucune donnée de la requête n'est persistée (stateless)

2. **Given** le relais en fonctionnement
   **When** un client envoie une requête DoH pour `example.com` type A
   **Then** la réponse contient une adresse IP valide
   **And** le content-type de la réponse est `application/dns-message`

3. **Given** le relais en fonctionnement
   **When** une requête arrive sur un path autre que `/dns-query` ou `/health`
   **Then** le relais retourne HTTP 404

4. **Given** le serveur HTTP/3
   **When** la connexion TLS est établie
   **Then** le protocole utilisé est TLS 1.3 minimum
   **And** le certificat TLS du relais est compatible Ed25519

## Tasks / Subtasks

- [x] Task 1 — Implémenter le serveur HTTP/3 avec quic-go (AC: #4)
  - [x] 1.0 Exécuter `go get github.com/quic-go/quic-go@v0.59.0` pour ajouter la dépendance au go.mod (actuellement vide après go mod tidy de la Story 1.1)
  - [x] 1.1 Créer `internal/relay/server.go` — struct `Server` avec `Addr`, `CertFile`, `KeyFile`, `Handler http.Handler`
  - [x] 1.2 Implémenter `NewServer(addr, certFile, keyFile string) *Server` — constructeur avec configuration TLS 1.3 Ed25519
  - [x] 1.3 Implémenter `(s *Server) ListenAndServe(ctx context.Context) error` — démarrage HTTP/3 via `http3.Server` + `http3.ConfigureTLSConfig`
  - [x] 1.4 Implémenter `(s *Server) Shutdown(ctx context.Context) error` — arrêt propre du serveur
  - [x] 1.5 Configurer le routage : `/dns-query` → doh_handler, `/health` → placeholder 200 OK, `*` → 404
  - [x] 1.6 Créer `internal/relay/server_test.go` — tests du routage (404 sur paths inconnus, routing correct)

- [x] Task 2 — Implémenter le handler DNS-over-HTTPS (AC: #1, #2)
  - [x] 2.1 Créer `internal/relay/doh_handler.go` — struct `DoHHandler` implémentant `http.Handler`, avec champ `client *http.Client` injectable (permet de mocker l'upstream dans les tests via `httptest.NewServer`)
  - [x] 2.2 Implémenter `NewDoHHandler(upstream string, client *http.Client) *DoHHandler` — constructeur avec URL résolveur upstream (défaut: `https://1.1.1.1/dns-query`) et client HTTP configurable (si nil, créer un `http.Client` avec timeout 5s)
  - [x] 2.3 Implémenter `ServeHTTP(w, r)` — validation : méthode POST uniquement, content-type `application/dns-message`
  - [x] 2.4 Lire le corps de la requête (DNS wire-format), forward vers le résolveur upstream via HTTPS POST
  - [x] 2.5 Retourner la réponse DNS au client avec content-type `application/dns-message`
  - [x] 2.6 Gérer les erreurs : upstream timeout → 502, body trop grand (>512 bytes pour UDP-compat, ou >65535) → 400, méthode non-POST → 405
  - [x] 2.7 Définir les erreurs sentinelles : `var ErrUpstreamUnavailable = errors.New("relay: upstream unavailable")`, `var ErrBodyTooLarge = errors.New("relay: body too large")`, `var ErrEmptyBody = errors.New("relay: empty body")`
  - [x] 2.8 S'assurer qu'aucune donnée de requête n'est persistée (stateless — pas de log, pas de cache, pas de variable globale)

- [x] Task 3 — Implémenter le endpoint /health placeholder (AC: #3)
  - [x] 3.1 Créer `internal/relay/health.go` — `HealthHandler` retournant `{"status":"ok"}` en JSON
  - [x] 3.2 Créer `internal/relay/health_test.go` — `TestHealthHandler_ReturnsOK` vérifiant status 200 et body `{"status":"ok"}` avec content-type `application/json`
  - [x] 3.3 Ce handler sera enrichi dans la Story 1.3 (connexions, uptime, RAM, CPU)

- [x] Task 4 — Générer le certificat TLS Ed25519 auto-signé pour le dev (AC: #4)
  - [x] 4.1 Créer `internal/crypto/tls.go` — fonction `GenerateSelfSignedTLSCert(privKey ed25519.PrivateKey) (certPEM, keyPEM []byte, error)`
  - [x] 4.2 Utiliser `crypto/x509` + `crypto/ed25519` pour un certificat X.509 auto-signé Ed25519
  - [x] 4.3 Créer `internal/crypto/tls_test.go` — test round-trip : générer cert → charger via `tls.X509KeyPair` → vérifier Ed25519

- [x] Task 5 — Câbler le point d'entrée relay (AC: #1, #2, #3, #4)
  - [x] 5.1 Mettre à jour `cmd/relay/main.go` — charger config (port, cert), initialiser Server + DoHHandler + HealthHandler
  - [x] 5.2 Implémenter graceful shutdown via `signal.NotifyContext` (SIGINT, SIGTERM)
  - [x] 5.3 Utiliser `context.Context` pour le lifecycle du serveur

- [x] Task 6 — Tests d'intégration du handler DoH (AC: #1, #2)
  - [x] 6.1 Créer `internal/relay/doh_handler_test.go`
  - [x] 6.2 Test `TestDoHHandler_ValidQuery` — requête DNS wire-format pour `example.com` type A → réponse valide
  - [x] 6.3 Test `TestDoHHandler_WrongMethod` — GET sur /dns-query → 405
  - [x] 6.4 Test `TestDoHHandler_WrongContentType` — POST avec mauvais content-type → 400
  - [x] 6.5 Test `TestDoHHandler_EmptyBody` — POST sans body → 400
  - [x] 6.6 Test `TestDoHHandler_UpstreamError` — upstream indisponible → 502

- [x] Task 7 — Validation globale (AC: #1, #2, #3, #4)
  - [x] 7.1 Exécuter `go build ./cmd/relay/` — compilation sans erreur
  - [x] 7.2 Exécuter `go test ./internal/relay/... ./internal/crypto/...` — tous les tests passent
  - [x] 7.3 Exécuter `go vet ./...` — aucun warning
  - [x] 7.4 Test d'intégration end-to-end : test Go utilisant `http3.Transport` pour envoyer une requête DoH au serveur relais local et vérifier la réponse DNS

## Dev Notes

### Contraintes architecturales critiques

- **Langage :** Go pur, aucun CGo. Module path : `github.com/velia-the-veil/le_voile`
- **Aucun log côté relais** — Les erreurs sont gérées silencieusement. Seul `/health` expose l'état. NE PAS ajouter `log.Println`, `log.Fatal`, `fmt.Println` pour du debug ou du logging de requêtes
- **Aucune donnée utilisateur persistée** — Ni IP client, ni contenu DNS, ni headers. Le relais est stateless par design (NFR3)
- **Aucun `panic`** — Toujours retourner `error`, jamais `panic`
- **`context.Context`** en premier argument de toute fonction bloquante/réseau

### Conventions de nommage Go (OBLIGATOIRES)

- **Packages :** minuscules, un mot : `relay`, `crypto`
- **Fichiers :** `snake_case.go` : `doh_handler.go`, `server.go`, `health.go`
- **Fonctions exportées :** `PascalCase` : `NewServer`, `NewDoHHandler`
- **Fonctions privées :** `camelCase`
- **Constructeurs :** Pattern `New` + type : `NewServer()`, `NewDoHHandler()`
- **Tests :** `TestNomType_NomMethode` : `TestDoHHandler_ValidQuery`, `TestServer_Routing`
- **Table-driven tests** quand > 2 cas

### Error handling (OBLIGATOIRE)

- Wrapping systématique : `fmt.Errorf("relay: serve: %w", err)`
- Préfixe = nom du package : `relay:`, `crypto:`
- Erreurs sentinelles pour cas récupérables : `var ErrUpstreamTimeout = errors.New("relay: upstream timeout")`

### API quic-go HTTP/3 — Pattern serveur (v0.59.0)

```go
import (
    "github.com/quic-go/quic-go/http3"
    "crypto/tls"
)

// Pattern recommandé :
mux := http.NewServeMux()
mux.Handle("/dns-query", dohHandler)
mux.Handle("/health", healthHandler)
// Tout autre path → 404 automatique par http.ServeMux

server := http3.Server{
    Handler:   mux,
    Addr:      "0.0.0.0:443",
    TLSConfig: http3.ConfigureTLSConfig(&tls.Config{
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS13,
    }),
}
err := server.ListenAndServe()
```

**ATTENTION :** `http3.ConfigureTLSConfig()` est OBLIGATOIRE — il configure le ALPN correct pour HTTP/3. Ne pas passer un `tls.Config` brut.

**ATTENTION :** quic-go v0.53.0+ a remplacé l'interface `Connection` par la struct `Conn`. Le code v0.59.0 utilise la nouvelle API.

### Protocole DoH — RFC 8484 (détails d'implémentation)

Le handler DoH est un simple proxy HTTP transparent pour le DNS wire-format :

1. **Réception :** POST HTTPS avec `Content-Type: application/dns-message`, body = DNS wire-format (RFC 1035 §4.2.1)
2. **Forwarding :** POST HTTPS vers `https://1.1.1.1/dns-query` avec le même body et content-type
3. **Réponse :** Retourner le body de la réponse upstream au client avec `Content-Type: application/dns-message`

```go
// Pseudo-code du handler DoH
func (h *DoHHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "", http.StatusMethodNotAllowed)
        return
    }
    if r.Header.Get("Content-Type") != "application/dns-message" {
        http.Error(w, "", http.StatusBadRequest)
        return
    }

    body, err := io.ReadAll(io.LimitReader(r.Body, 65535))
    // ... forward vers upstream ...
    // ... retourner la réponse ...
}
```

**Taille max du body :** Les messages DNS UDP font max 512 bytes (standard) ou ~4096 (EDNS0). Limiter `io.ReadAll` à 65535 bytes (max DNS over TCP). Rejeter les body vides.

**Upstream résolveur :** Cloudflare DNS `https://1.1.1.1/dns-query` — utiliser un `http.Client` standard Go (pas HTTP/3 côté upstream, HTTPS/2 standard suffit). Le `http.Client` doit être un champ de la struct `DoHHandler` pour permettre l'injection dans les tests (via `httptest.NewServer` comme mock upstream).

**Timeout upstream :** Configurer un `http.Client` avec timeout de 5 secondes. En cas de timeout ou erreur upstream → HTTP 502 Bad Gateway.

### Certificat TLS Ed25519

Le relais a besoin d'un certificat TLS pour le serveur HTTP/3. En production, Cloudflare termine le TLS côté CDN et se connecte au relais en origin pull. Pour le développement et les tests :

- Générer un certificat X.509 auto-signé avec clé Ed25519 via `crypto/x509` + `crypto/ed25519`
- Le module `internal/crypto/` contient déjà `GenerateKeyPair()` — réutiliser pour générer la clé privée du certificat
- Stocker la fonction de génération du cert dans `internal/crypto/tls.go` (même package, accès aux fonctions existantes)

```go
// Pattern pour certificat Ed25519 auto-signé
template := &x509.Certificate{
    SerialNumber: big.NewInt(1),
    Subject:      pkix.Name{CommonName: "levoile.dev"},
    NotBefore:    time.Now(),
    NotAfter:     time.Now().Add(365 * 24 * time.Hour),
    KeyUsage:     x509.KeyUsageDigitalSignature,
    ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
    DNSNames:     []string{"levoile.dev", "localhost"},
}
certDER, _ := x509.CreateCertificate(rand.Reader, template, template, pubKey, privKey)
```

### Apprentissages de la Story 1.1 (OBLIGATOIRE à respecter)

- **go.mod sans dépendances externes** est normal tant que le code ne les importe pas. `go mod tidy` les retirera si aucun import. Pour cette story, `quic-go` sera importé et restera dans go.mod
- **Sign() retourne `([]byte, error)`** — suivre ce pattern avec validation nil pour toutes les nouvelles fonctions crypto
- **Tests nommés `TestType_Method`** — convention établie dans la Story 1.1, la suivre
- **Artéfacts build** — Ne pas committer de binaires `.exe` dans la racine. Ajouter au `.gitignore` si nécessaire
- **Licence MIT** choisie par l'opérateur

### Structure des fichiers à créer/modifier

```
internal/
├── crypto/
│   ├── ed25519.go          # EXISTANT — NE PAS MODIFIER
│   ├── ed25519_test.go     # EXISTANT — NE PAS MODIFIER
│   ├── tls.go              # NOUVEAU — GenerateSelfSignedTLSCert()
│   └── tls_test.go         # NOUVEAU — Tests certificat Ed25519
├── relay/
│   ├── doc.go              # EXISTANT — Peut être supprimé ou conservé
│   ├── server.go           # NOUVEAU — Struct Server, HTTP/3, routage
│   ├── server_test.go      # NOUVEAU — Tests routage
│   ├── doh_handler.go      # NOUVEAU — Handler DoH RFC 8484
│   ├── doh_handler_test.go # NOUVEAU — Tests handler DoH
│   ├── health.go           # NOUVEAU — Endpoint /health placeholder
│   └── health_test.go      # NOUVEAU — Test /health
cmd/
└── relay/
    └── main.go             # MODIFIER — Câblage serveur + handlers + shutdown
```

### NE PAS implémenter (hors scope Story 1.2)

- Limiteur de connexions (Story 1.3)
- Métriques /health complètes — connections, uptime, ram_mb, cpu_pct (Story 1.3)
- Déploiement systemd et build linux/amd64 (Story 1.3)
- Configuration TOML (Epic 2+)
- Le module `internal/config/` — utiliser des flags CLI ou des constantes pour cette story

### Test d'intégration end-to-end (AC #1, #2)

Écrire un test Go d'intégration utilisant `http3.RoundTripper` de quic-go pour envoyer une requête DoH au serveur relais local. Pattern :

```go
// Construire une requête DNS wire-format pour example.com type A
dnsQuery := buildDNSQuery("example.com", dns.TypeA) // RFC 1035 §4.2.1

// Client HTTP/3 avec TLS InsecureSkipVerify (cert auto-signé en dev)
client := &http.Client{
    Transport: &http3.RoundTripper{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
    },
}

resp, err := client.Post("https://localhost:8443/dns-query",
    "application/dns-message", bytes.NewReader(dnsQuery))
// Vérifier resp.StatusCode == 200
// Vérifier resp.Header.Get("Content-Type") == "application/dns-message"
// Vérifier que le body contient une réponse DNS valide
```

**Note :** Ce test nécessite un réseau fonctionnel (forward vers 1.1.1.1). Préféré au curl car curl HTTP/3 n'est pas disponible en standard.

### Project Structure Notes

- Tous les fichiers relay dans `internal/relay/` — conformité architecture planifiée
- Le package `crypto` est étendu avec `tls.go` — cohérent car il gère la cryptographie Ed25519
- Aucun conflit détecté avec la structure existante de la Story 1.1

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Core Architectural Decisions] — HTTP/3 DoH RFC 8484, TLS 1.3
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns & Consistency Rules] — Naming, error handling, concurrence, anti-patterns
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure & Boundaries] — Structure relay/, crypto/
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.2] — Acceptance criteria BDD
- [Source: _bmad-output/planning-artifacts/prd.md#Non-Functional Requirements] — NFR1 (TLS 1.3), NFR3 (stateless), NFR8 (latence DNS)
- [Source: _bmad-output/implementation-artifacts/1-1-initialisation-du-projet-et-module-cryptographique-ed25519.md] — Apprentissages Story 1.1, API crypto existante
- [Source: quic-go.net/docs/http3/server/] — API HTTP/3 server quic-go
- [Source: datatracker.ietf.org/doc/html/rfc8484] — RFC 8484 DNS Queries over HTTPS

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

Aucun problème rencontré durant l'implémentation initiale.

### Completion Notes List

- **Task 1:** Serveur HTTP/3 créé avec quic-go v0.59.0. Struct `Server` avec `ListenAndServe`/`Shutdown`, TLS 1.3 via `http3.ConfigureTLSConfig`, routage via `http.ServeMux`. Type d'erreur `ServerError` avec wrapping.
- **Task 2:** Handler DoH conforme RFC 8484 — validation POST + content-type, forwarding upstream avec `http.Client` injectable, erreurs sentinelles (`ErrUpstreamUnavailable`, `ErrBodyTooLarge`, `ErrEmptyBody`), limite 65535 bytes, aucune donnée persistée (stateless).
- **Task 3:** `HealthHandler` retourne `{"status":"ok"}` avec content-type JSON. Test unitaire validé. (Placeholder conforme au scope Story 1.2 — sera enrichi avec métriques dans Story 1.3)
- **Task 4:** `GenerateSelfSignedTLSCert()` dans `internal/crypto/tls.go` — certificat X.509 Ed25519 auto-signé avec serial number aléatoire, DNS names levoile.dev + localhost, validity 1 an. Tests round-trip, nil key, invalid key, DNS names.
- **Task 5:** `cmd/relay/main.go` câblé avec flags CLI (`-addr`, `-cert`, `-key`, `-upstream`), graceful shutdown via `signal.NotifyContext` (SIGINT/SIGTERM).
- **Task 6:** 5 tests d'intégration DoH avec mock upstream (httptest.NewServer) : valid query, wrong method (table-driven 4 méthodes), wrong content-type (table-driven 3 cas), empty body, upstream error.
- **Task 7:** Build OK, 22 tests PASS (9 relay + 13 crypto dont 4 nouveaux TLS) + 1 e2e skippé, go vet clean. Test e2e HTTP/3 round-trip vers Cloudflare DNS 1.1.1.1 (nécessite E2E=1).

### File List

- `internal/relay/server.go` — NOUVEAU — Serveur HTTP/3 avec routage
- `internal/relay/server_test.go` — NOUVEAU — Tests routage HTTP/3 (404, /health, /dns-query) + helpers (freeUDPAddr, waitForServer)
- `internal/relay/doh_handler.go` — NOUVEAU — Handler DoH RFC 8484
- `internal/relay/doh_handler_test.go` — NOUVEAU — Tests unitaires handler DoH (5 tests)
- `internal/relay/health.go` — NOUVEAU — Endpoint /health placeholder `{"status":"ok"}`
- `internal/relay/health_test.go` — NOUVEAU — Test /health (status, body, content-type)
- `internal/relay/e2e_test.go` — NOUVEAU — Test e2e HTTP/3 DoH round-trip
- `internal/crypto/tls.go` — NOUVEAU — GenerateSelfSignedTLSCert()
- `internal/crypto/tls_test.go` — NOUVEAU — Tests certificat TLS Ed25519 (4 tests)
- `cmd/relay/main.go` — MODIFIÉ — Câblage serveur + handlers + shutdown
- `go.mod` — MODIFIÉ — Ajout quic-go v0.59.0 + dépendances transitives
- `go.sum` — NOUVEAU — Checksums des dépendances

## Change Log

- 2026-03-08: Implémentation complète Story 1.2 — Serveur relais HTTP/3 avec handler DoH RFC 8484, endpoint /health, certificat TLS Ed25519 auto-signé, et test e2e. 22 tests passent, build et vet clean.
- 2026-03-09: Code review — Retrait du scope creep Story 1.3 (limiter.go, middleware.go, health enrichi). Health revert vers placeholder `{"status":"ok"}`. Server simplifié sans Limiter/StartTime. Tests améliorés : ports UDP dynamiques + polling readiness au lieu de time.Sleep. 22 tests PASS (9 relay + 13 crypto) + 1 e2e skippé, build et vet clean.
