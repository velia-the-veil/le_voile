# Story 2.2: Gestion DNS systeme et redirection DoH

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que toutes mes requetes DNS soient automatiquement redirigees via le tunnel chiffre,
Afin que mes requetes DNS soient invisibles pour mon FAI et mon reseau local.

## Acceptance Criteria

1. **Given** le tunnel est en etat `connected`
   **When** le client active la protection DNS
   **Then** le resolver DNS systeme est modifie pour pointer vers le client DoH local (127.0.0.1)
   **And** la modification utilise `netsh` sur Windows, `resolvectl`/`resolv.conf` sur Linux, `networksetup` sur macOS

2. **Given** le resolver DNS systeme modifie
   **When** une application effectue une requete DNS (ex: `nslookup example.com`)
   **Then** la requete est envoyee en POST HTTPS au format RFC 8484 via le tunnel vers `/dns-query` du relais
   **And** la reponse DNS est retournee a l'application en moins de 50ms de latence additionnelle

3. **Given** le client desactive normalement (arret volontaire)
   **When** la protection DNS est desactivee
   **Then** le resolver DNS systeme est restaure a sa valeur originale
   **And** aucun residu de configuration ne subsiste

4. **Given** l'interface DNSManager
   **When** le code est compile avec des build tags differents
   **Then** l'implementation Windows (`manager_windows.go`) utilise `netsh`
   **And** l'implementation Linux (`manager_linux.go`) utilise `resolvectl` ou `resolv.conf`
   **And** l'implementation macOS (`manager_darwin.go`) utilise `networksetup`

## Tasks / Subtasks

- [x] Task 1 — Definir l'interface DNSManager et le proxy DNS local (AC: #1, #2, #4)
  - [x] 1.1 Creer `internal/dns/manager.go` — Interface `DNSManager` avec methodes `SetResolver`, `RestoreResolver`, `OriginalResolver`
  - [x] 1.2 Definir les types et erreurs sentinelles dans `manager.go`
  - [x] 1.3 Creer `internal/dns/proxy.go` — Serveur DNS UDP local sur 127.0.0.1:53, forwarding via tunnel DoH
  - [x] 1.4 Creer `internal/dns/proxy_test.go` — Tests du proxy DNS local

- [x] Task 2 — Implementation Windows du DNSManager (AC: #1, #3, #4)
  - [x] 2.1 Creer `internal/dns/manager_windows.go` — Implementation via `netsh`
  - [x] 2.2 Creer `internal/dns/manager_windows_test.go` — Tests unitaires (mocks d'execution de commandes)

- [x] Task 3 — Implementation Linux du DNSManager (AC: #1, #3, #4)
  - [x] 3.1 Creer `internal/dns/manager_linux.go` — Implementation via `resolvectl` et fallback `resolv.conf`
  - [x] 3.2 Creer `internal/dns/manager_linux_test.go` — Tests unitaires

- [x] Task 4 — Implementation macOS du DNSManager (AC: #1, #3, #4)
  - [x] 4.1 Creer `internal/dns/manager_darwin.go` — Implementation via `networksetup`
  - [x] 4.2 Creer `internal/dns/manager_darwin_test.go` — Tests unitaires

- [x] Task 5 — Cablage dans le point d'entree client (AC: #1, #2, #3)
  - [x] 5.1 Modifier `cmd/client/main.go` — Integrer DNSManager et proxy DNS dans le lifecycle
  - [x] 5.2 Ajouter l'activation DNS apres connexion tunnel et desactivation avant deconnexion

- [x] Task 6 — Validation globale (AC: #1, #2, #3, #4)
  - [x] 6.1 `go build ./cmd/client/` — compilation sans erreur sur toutes les plateformes
  - [x] 6.2 `go test ./internal/dns/...` — tous les tests passent
  - [x] 6.3 `go test ./internal/tunnel/...` — non-regression
  - [x] 6.4 `go vet ./...` — aucun warning

## Dev Notes

### Contraintes architecturales critiques

- **Langage :** Go pur, aucun CGo. Module path : `github.com/velia-the-veil/le_voile`
- **Aucun log cote client** — Les erreurs sont propagees vers le tray via IPC (Epic 3). Pour cette story, les erreurs sont retournees. Le SEUL logging autorise est le `fmt.Printf` temporaire dans `cmd/client/main.go`
- **Aucun `panic`** — Toujours retourner `error`, jamais `panic`
- **`context.Context`** en premier argument de toute fonction bloquante/reseau
- **Error wrapping** : `fmt.Errorf("dns: ...: %w", err)` — prefixe `dns:` obligatoire
- **Build tags** : `//go:build windows`, `//go:build linux`, `//go:build darwin` pour code specifique OS

### Conventions de nommage Go (OBLIGATOIRES)

- **Packages :** minuscules, un mot : `dns`
- **Fichiers :** `snake_case.go` : `manager.go`, `proxy.go`, `manager_windows.go`
- **Fonctions exportees :** `PascalCase` : `NewManager`, `SetResolver`, `RestoreResolver`
- **Fonctions privees :** `camelCase` : `runNetsh`, `parseResolvConf`
- **Constructeurs :** Pattern `New` + type : `NewManager()`, `NewProxy()`
- **Constantes exportees :** `PascalCase` : `DefaultListenAddr`
- **Tests :** `TestType_Method` : `TestProxy_ForwardQuery`, `TestWindowsManager_SetResolver`
- **Table-driven tests** quand > 2 cas
- **Erreurs sentinelles** : `var ErrResolverNotSet = errors.New("dns: resolver not set")`

### Architecture du proxy DNS local

**Flux complet des requetes DNS :**

```
[Application]
     | requete DNS standard (UDP port 53)
     v
[Proxy DNS local (127.0.0.1:53)]
     | extrait wire-format DNS du paquet UDP
     | envoie via tunnel.Client.SendDoHQuery()
     v
[Tunnel QUIC/HTTPS → Cloudflare → Relais VPS]
     | POST https://levoile.dev/dns-query
     | Content-Type: application/dns-message
     v
[Relais → Cloudflare DNS 1.1.1.1]
     | reponse DNS wire-format
     v
[Proxy DNS local]
     | retourne la reponse UDP a l'application
     v
[Application recoit la reponse DNS]
```

**Design du proxy DNS local (`internal/dns/proxy.go`) :**

```go
type Proxy struct {
    listenAddr string              // "127.0.0.1:53"
    conn       *net.UDPConn       // Listener UDP
    queryFunc  func(ctx context.Context, payload []byte) ([]byte, error)
    // queryFunc sera tunnel.Client.SendDoHQuery
}
```

- Ecoute sur UDP `127.0.0.1:53`
- Chaque requete DNS recue est un paquet UDP contenant le wire-format DNS
- Le wire-format est passe directement a `SendDoHQuery` (deja le format RFC 8484)
- La reponse est renvoyee au client DNS original
- Pas besoin de parser le contenu DNS — on proxie les bytes bruts
- Buffer de lecture : 4096 bytes (EDNS0 supporte des paquets jusqu'a 4096)
- **context.Context** pour le lifecycle (shutdown graceful)
- Goroutine par requete LIMITEE : pool de goroutines via semaphore (max 100 concurrentes)

**ATTENTION Port 53 :** Ecouter sur le port 53 necessite des privileges eleves (admin/root). C'est coherent avec le design service systeme (kardianos/service, Epic 3). Pour le MVP, le client tourne avec elevation (`cmd/client/main.go` temporaire).

### Interface DNSManager

```go
// DNSManager gere la configuration du resolver DNS systeme.
type DNSManager interface {
    // SetResolver modifie le resolver DNS de toutes les interfaces actives
    // pour pointer vers l'adresse specifiee (ex: "127.0.0.1").
    SetResolver(ctx context.Context, addr string) error

    // RestoreResolver restaure le resolver DNS a sa valeur originale
    // sauvegardee lors du dernier appel a SetResolver.
    RestoreResolver(ctx context.Context) error

    // OriginalResolver retourne l'adresse du resolver original sauvegardee,
    // ou une chaine vide si SetResolver n'a pas ete appele.
    OriginalResolver() string
}
```

**Pattern d'implementation :**
- Chaque implementation OS sauvegarde le resolver original AVANT de le modifier
- `RestoreResolver()` utilise la valeur sauvegardee pour restaurer
- Si `SetResolver()` n'a jamais ete appele, `RestoreResolver()` est un no-op
- Les commandes OS sont executees via `exec.CommandContext()` pour respecter le contexte

### Implementation Windows (`manager_windows.go`)

```go
//go:build windows

type windowsManager struct {
    originalDNS map[string]string // interface name → original DNS
    mu          sync.Mutex
}
```

**Commandes netsh :**
```bash
# Lister les interfaces actives
netsh interface ip show config

# Obtenir le DNS actuel d'une interface
netsh interface ip show dns "Ethernet"

# Modifier le resolver DNS
netsh interface ip set dns name="Ethernet" static 127.0.0.1

# Restaurer (DHCP)
netsh interface ip set dns name="Ethernet" dhcp
```

**ATTENTION :** Windows peut avoir plusieurs interfaces reseau actives (Ethernet, Wi-Fi, VPN tiers). Il faut modifier le DNS de TOUTES les interfaces actives et sauvegarder l'etat original de chacune.

**Detection des interfaces actives :** Utiliser `net.Interfaces()` de Go standard pour lister les interfaces UP et non-loopback, puis appliquer netsh sur chacune.

### Implementation Linux (`manager_linux.go`)

```go
//go:build linux

type linuxManager struct {
    originalDNS string // contenu original de /etc/resolv.conf
    useResolvectl bool
    mu            sync.Mutex
}
```

**Strategy : Detection au demarrage**
1. Verifier si `resolvectl` est disponible → utiliser `resolvectl`
2. Sinon → manipuler `/etc/resolv.conf` directement

**Commandes resolvectl :**
```bash
# Obtenir le DNS actuel
resolvectl dns

# Modifier le resolver DNS
resolvectl dns <interface> 127.0.0.1

# Restaurer
resolvectl revert <interface>
```

**Methode resolv.conf :**
```bash
# Sauvegarder
cp /etc/resolv.conf /etc/resolv.conf.levoile.bak

# Modifier
echo "nameserver 127.0.0.1" > /etc/resolv.conf

# Restaurer
cp /etc/resolv.conf.levoile.bak /etc/resolv.conf
rm /etc/resolv.conf.levoile.bak
```

### Implementation macOS (`manager_darwin.go`)

```go
//go:build darwin

type darwinManager struct {
    originalDNS map[string][]string // service name → original DNS servers
    mu          sync.Mutex
}
```

**Commandes networksetup :**
```bash
# Lister les services reseau
networksetup -listallnetworkservices

# Obtenir le DNS actuel
networksetup -getdnsservers "Wi-Fi"

# Modifier le resolver
networksetup -setdnsservers "Wi-Fi" 127.0.0.1

# Restaurer (vider = DHCP)
networksetup -setdnsservers "Wi-Fi" Empty
```

### Injection de dependance pour les tests

**Pattern d'execution de commandes testable :**

```go
// commandRunner permet de mocker l'execution de commandes OS dans les tests
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultRunner execute reellement la commande
func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
    return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
```

Chaque implementation OS accepte un `commandRunner` optionnel (pattern injection de dependance). En production, `defaultRunner` est utilise. En test, un mock retourne les sorties attendues.

### Apprentissages de la Story 2.1 (OBLIGATOIRE a respecter)

**De la Story 2.1 :**
- Machine d'etat `StateManager` avec `connected`/`connecting`/`disconnected`
- `SendDoHQuery()` existe deja dans `tunnel.Client` — prend `[]byte` DNS wire-format, retourne `[]byte` reponse
- `SendDoHQuery()` verifie que l'etat est `connected` avant d'envoyer
- Timeout 5s pour les requetes DoH
- Le client HTTP/3 est deja configure avec TLS 1.3
- Error wrapping avec prefixe `tunnel:`
- Tests utilisent un serveur HTTP/3 local

**De la Story 1.2 :**
- Le DoH handler relay accepte `application/dns-message` (POST)
- Body min 1 byte, max 65535 bytes
- Le relay forward vers upstream (1.1.1.1) et retourne la reponse

**De la Story 1.1 :**
- `go mod tidy` pour nettoyer les dependances
- Module path : `github.com/velia-the-veil/le_voile`
- Conventions de nommage Go strictement suivies

### NE PAS implementer (hors scope Story 2.2)

- **Kill switch DNS** (Story 2.3) — pas de blocage DNS quand le tunnel tombe
- **Watchdog DNS** (Story 2.3) — pas de surveillance du resolver
- **Reconnexion automatique** (Story 2.3) — pas de retry si le tunnel tombe
- **Module config TOML** (`internal/config/`) — utiliser des flags CLI ou hardcoded pour cette story
- **IPC** (Epic 3) — pas de communication inter-processus
- **Tray UI** (Epic 3) — pas d'interface graphique
- **Backoff exponentiel** (Story 2.3)
- **TCP DNS** — UDP uniquement pour le MVP (la grande majorite des requetes DNS sont UDP)
- **DNS caching** — pas de cache local, chaque requete est forwardee

### Dependances — Aucune nouvelle

Cette story n'ajoute aucune nouvelle dependance Go. Tout est realisable avec :
- `net` — UDP listener pour le proxy DNS
- `os/exec` — Execution de commandes systeme (netsh, resolvectl, networksetup)
- `context` — Lifecycle et timeouts
- `sync` — Mutex pour etat DNSManager
- `runtime` — Detection OS (GOOS) si necessaire
- `golang.org/x/sys` — deja dans go.mod (v0.35.0), utile pour syscalls si necessaire
- `internal/tunnel` — Reutilisation de `Client.SendDoHQuery()` pour le forwarding DoH

### NFRs couverts par cette story

- **NFR5** : Aucune fuite DNS pendant le fonctionnement normal — toutes les requetes DNS passent par le tunnel
- **NFR6** : Resolver DNS systeme restaure (desactivation) — `RestoreResolver()` appele au shutdown
- **NFR8** : Resolution DNS via tunnel < 50ms de latence additionnelle — proxy UDP local + DoH via HTTP/3 (connexion persistante)

### Structure des fichiers a creer/modifier

```
internal/
├── dns/
│   ├── doc.go              # EXISTANT — Stub package, sera supprime si d'autres fichiers Go existent
│   ├── manager.go          # NOUVEAU — Interface DNSManager + types + erreurs sentinelles
│   ├── manager_windows.go  # NOUVEAU — //go:build windows — implementation via netsh
│   ├── manager_windows_test.go # NOUVEAU — Tests Windows (mock commandes)
│   ├── manager_linux.go    # NOUVEAU — //go:build linux — implementation via resolvectl/resolv.conf
│   ├── manager_linux_test.go   # NOUVEAU — Tests Linux (mock commandes)
│   ├── manager_darwin.go   # NOUVEAU — //go:build darwin — implementation via networksetup
│   ├── manager_darwin_test.go  # NOUVEAU — Tests macOS (mock commandes)
│   ├── proxy.go            # NOUVEAU — Proxy DNS UDP local (127.0.0.1:53)
│   └── proxy_test.go       # NOUVEAU — Tests proxy DNS
cmd/
├── client/
│   └── main.go             # MODIFIER — Integrer DNS proxy + DNS manager dans le lifecycle
```

### Project Structure Notes

- Le package `internal/dns/` est actuellement un stub (`doc.go` uniquement) — cette story le remplit
- Le `doc.go` peut etre supprime quand les fichiers d'implementation sont ajoutes
- Le proxy DNS ecoute sur 127.0.0.1:53 — meme adresse que le kill switch (Story 2.3)
- Le `tunnel.Client.SendDoHQuery()` est le point d'integration principal avec la Story 2.1
- Aucun conflit detecte avec la structure existante

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Architecture Service & Integration OS] — Interface DNSManager, build tags, netsh/resolvectl/networksetup
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns & Consistency Rules] — Naming, error handling, concurrence, anti-patterns
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure & Boundaries] — Structure dns/, fichiers manager_*.go, kill_switch.go, doh.go
- [Source: _bmad-output/planning-artifacts/architecture.md#Data Flow] — Flux DNS complet utilisateur → tunnel → relais → Cloudflare DNS
- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.2] — Acceptance criteria BDD complets
- [Source: _bmad-output/planning-artifacts/prd.md#Protection DNS] — FR5 (redirection DNS), FR6 (modification resolver), FR7 (restauration resolver)
- [Source: _bmad-output/planning-artifacts/prd.md#Non-Functional Requirements] — NFR5 (zero fuite DNS), NFR6 (restauration resolver), NFR8 (latence < 50ms)
- [Source: _bmad-output/implementation-artifacts/2-1-client-tunnel-quic-https-avec-connexion-au-relais.md] — API tunnel.Client : SendDoHQuery, StateManager, patterns etablis
- [Source: _bmad-output/implementation-artifacts/1-2-serveur-relais-http3-et-handler-dns-over-https.md] — DoH handler format wire-format, RFC 8484

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Corrige parseDNSFromNetsh: les lignes contenant "DNS" etaient exclues a tort, empechant l'extraction des IP dans la sortie netsh.

### Completion Notes List

- Task 1: Interface DNSManager avec SetResolver/RestoreResolver/OriginalResolver, types QueryFunc, erreurs sentinelles (ErrResolverNotSet, ErrSetResolverFailed, ErrRestoreFailed, ErrNoActiveInterface, ErrProxyNotRunning), constante DefaultListenAddr. Proxy DNS UDP local avec semaphore (max 100 goroutines concurrentes), buffer 4096 bytes EDNS0, shutdown graceful via context. 6 tests proxy: NewProxy, StartStop, ForwardQuery, QueryFuncError, ConcurrentQueries, EmptyPayload.
- Task 2: Implementation Windows via netsh — windowsManager sauvegarde DNS de toutes les interfaces actives (net.Interfaces), restauration DHCP ou static. 7 tests dont table-driven parseDNSFromNetsh.
- Task 3: Implementation Linux via resolvectl (priorite) avec fallback resolv.conf — detection automatique au demarrage, backup /etc/resolv.conf.levoile.bak. Tests avec mock commandRunner.
- Task 4: Implementation macOS via networksetup — darwinManager gere multiples services reseau, restauration Empty (DHCP) ou liste de serveurs. Tests avec mock + table-driven parseNetworkServices/parseDNSServers.
- Task 5: Cablage dans cmd/client/main.go — DNS proxy demarre apres connexion tunnel, DNSManager.SetResolver active la protection, RestoreResolver appele avant deconnexion au shutdown.
- Task 6: Compilation OK, 13 tests dns passent, 13 tests tunnel passent (non-regression), go vet clean.

### Change Log

- 2026-03-09: Implementation complete de la gestion DNS systeme et redirection DoH — proxy UDP local, interface DNSManager multi-OS (Windows/Linux/macOS), cablage lifecycle client.
- 2026-03-09: Code review — Corrige 7 issues (3 HIGH, 4 MEDIUM): rollback Windows sur echec partiel, Linux resolvectl multi-interface, tests Windows isoles, readiness channel proxy, permissions backup resolv.conf, defaultRunner centralise, validation taille minimum DNS.

### File List

- internal/dns/manager.go (NEW) — Interface DNSManager, types, erreurs sentinelles, commandRunner, defaultRunner
- internal/dns/proxy.go (NEW) — Proxy DNS UDP local avec semaphore et readiness channel
- internal/dns/proxy_test.go (NEW) — 7 tests du proxy DNS (readiness, taille minimum, concurrence)
- internal/dns/manager_windows.go (NEW) — Implementation Windows via netsh avec rollback sur echec partiel
- internal/dns/manager_windows_test.go (NEW) — 9 tests Windows (mocks isoles avec interfaceLister)
- internal/dns/manager_linux.go (NEW) — Implementation Linux via resolvectl multi-interface/resolv.conf
- internal/dns/manager_linux_test.go (NEW) — 5 tests Linux (mocks, multi-interface)
- internal/dns/manager_darwin.go (NEW) — Implementation macOS via networksetup
- internal/dns/manager_darwin_test.go (NEW) — 7 tests macOS (mocks)
- internal/dns/doc.go (DELETED) — Stub package supprime
- cmd/client/main.go (MODIFIED) — Integration DNS proxy + DNSManager avec readiness dans le lifecycle
