# Story 5.3: Gestion du fallback TURN et validation anti-fuite WebRTC

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que les appels video/audio fonctionnent correctement malgre la substitution STUN, et pouvoir verifier l'absence de fuite,
Afin d'avoir confiance que ma vie privee est protegee sans casser mes appels.

## Acceptance Criteria

1. **Given** la substitution IP STUN active
   **When** une connexion P2P WebRTC echoue (l'IP substituee n'est pas routable pour le pair)
   **Then** WebRTC bascule automatiquement sur les serveurs TURN du service (Google Meet, Discord, etc.) de maniere transparente

2. **Given** un appel video en cours via TURN fallback
   **When** le flux audio/video transite
   **Then** la qualite est maintenue (le flux media passe par TURN, pas par le tunnel Le Voile)

3. **Given** le proxy STUN actif
   **When** l'utilisateur visite browserleaks.com ou ipleak.net
   **Then** aucune IP locale ou publique reelle n'apparait dans les resultats WebRTC — seule l'IP du relais est visible

4. **Given** le proxy STUN actif
   **When** le tunnel Le Voile est coupe (kill switch actif)
   **Then** les requetes STUN sont egalement bloquees (pas de fuite WebRTC pendant la coupure)

## Tasks / Subtasks

- [x] Task 1 : Valider le passthrough TURN transparent (AC: #1, #2)
  - [x] 1.1 Ajouter des constantes TURN dans `internal/stun/stun.go` : `TypeAllocateRequest = 0x0003`, `TypeCreatePermission = 0x0008`, `TypeChannelBind = 0x0009`, `TypeSendIndication = 0x0016`, `TypeDataIndication = 0x0017` — documenter qu'ils NE sont PAS interceptes
  - [x] 1.2 Ajouter `IsTURN(packet []byte) bool` dans `parser.go` — retourne true si le type message est un type TURN (Allocate, CreatePermission, ChannelBind, Send, Data)
  - [x] 1.3 Ecrire tests table-driven pour `IsTURN` : paquets Allocate Request, CreatePermission, ChannelBind, Binding Request (false), non-STUN (false)
  - [x] 1.4 Ecrire test explicite `TestInterceptor_TURNPassthrough` : envoyer un paquet TURN Allocate Request a l'intercepteur → verifier qu'il est forwarde en transparent (ForwardFunc appelee), PAS intercepte
  - [x] 1.5 Ecrire test `TestInterceptor_RTPNotTouched` : envoyer un paquet RTP (premiers 2 bits = 0b10) → verifier qu'il passe en transparent via ForwardFunc

- [x] Task 2 : Garantir le blocage STUN pendant le kill switch (AC: #4)
  - [x] 2.1 Ajouter methode `SetEnabled(enabled bool)` sur `Relayer` — quand disabled, `HandleIntercept` drop tous les paquets sans verifier le tunnel
  - [x] 2.2 Dans `service.go`, appeler `relayer.SetEnabled(false)` quand le kill switch s'active et `relayer.SetEnabled(true)` quand il se desactive — s'abonner au state channel du tunnel
  - [x] 2.3 Ajouter methode `SetEnabled(enabled bool)` sur `Interceptor` — quand disabled, `handlePacket` drop tous les paquets (ni forward ni intercept)
  - [x] 2.4 Dans `service.go`, connecter `interceptor.SetEnabled(false/true)` au meme signal kill switch que le relayer
  - [x] 2.5 Ecrire test `TestRelayer_Disabled_DropsAll` : relayer disabled → paquet STUN envoye → verifier que le tunnel n'est PAS appele
  - [x] 2.6 Ecrire test `TestInterceptor_Disabled_DropsAll` : intercepteur disabled → paquet envoye → ni ForwardFunc ni InterceptFunc appeles
  - [x] 2.7 Ecrire test d'integration `TestService_KillSwitch_BlocksSTUN` : simuler deconnexion tunnel → verifier intercepteur + relayer desactives → simuler reconnexion → verifier reactives

- [x] Task 3 : Outil de validation anti-fuite WebRTC (AC: #3)
  - [x] 3.1 Creer `internal/leakcheck/webrtc.go` — package `leakcheck` avec struct `WebRTCLeakChecker`
  - [x] 3.2 Implementer `CheckSTUNLeak(ctx context.Context, stunServer string) (*LeakResult, error)` — envoie un STUN Binding Request UDP direct au serveur STUN specifie, parse la reponse XOR-MAPPED-ADDRESS et retourne l'IP decouverte
  - [x] 3.3 Implementer `parseXORMappedAddress(response []byte) (net.IP, error)` — parse l'attribut XOR-MAPPED-ADDRESS (type 0x0020) de la reponse STUN Binding Response selon RFC 5389 section 15.2
  - [x] 3.4 Implementer `RunFullCheck(ctx context.Context) (*FullLeakReport, error)` — execute CheckSTUNLeak sur 3 serveurs (stun.l.google.com:19302, stun1.l.google.com:19302, stun.cloudflare.com:3478), compare les IPs retournees avec l'IP HTTP publique (via tunnel), retourne un rapport pass/fail
  - [x] 3.5 Ajouter action IPC `leak_check` dans `messages.go` — le tray peut declencher un test de fuite
  - [x] 3.6 Implementer le handler IPC `handleLeakCheck()` dans `ipchandler/handler.go` — execute RunFullCheck en goroutine, repond avec le resultat JSON (`{status: "pass"/"fail", stun_ip: "x.x.x.x", http_ip: "y.y.y.y"}`)
  - [x] 3.7 Ecrire tests unitaires pour `parseXORMappedAddress` avec reponses STUN valides/invalides
  - [x] 3.8 Ecrire test pour `CheckSTUNLeak` avec mock UDP serveur STUN

- [x] Task 4 : Integration menu tray et reporting (AC: #3)
  - [x] 4.1 Ajouter item menu tray "Verifier fuite WebRTC" dans `internal/tray/tray.go` — visible uniquement quand le tunnel est connecte
  - [x] 4.2 Au clic, envoyer `{action: "leak_check"}` via IPC et afficher le resultat dans le tooltip (temporaire 10s) : "Aucune fuite detectee" ou "FUITE DETECTEE — IP reelle visible"
  - [x] 4.3 Ecrire test pour l'integration IPC du leak check dans le tray

- [x] Task 5 : Tests d'integration et documentation (AC: #1, #2, #3, #4)
  - [x] 5.1 Ecrire test d'integration `TestSTUNProxy_TURNFallback` : simuler un scenario ICE complet — Binding Request intercepte (srflx = VPS IP), Allocate Request passe en transparent (relay candidate), verifier que seul Binding est intercepte
  - [x] 5.2 Ecrire test `TestLeakCheck_WithProxy` : demarrer intercepteur + relayer + mock tunnel → executer leak check → verifier que l'IP retournee est celle du VPS
  - [x] 5.3 Documenter la procedure de test manuel dans les Dev Notes (browserleaks.com, ipleak.net) pour la validation humaine

## Dev Notes

### Contexte Technique Critique

**DECOUVERTE CLE : Le TURN n'a PAS besoin d'interception.**

Le protocole TURN (RFC 5766) cache inheremment l'IP du client. Quand un relay candidate TURN est utilise, le pair distant voit uniquement l'IP du serveur TURN, pas celle du client. Le Voile ne doit donc intercepter que les STUN Binding Requests (deja fait en stories 5.1/5.2).

**Comportement ICE et fallback TURN :**
```
ICE Candidate Gathering (parallele dans le navigateur) :
1. Host candidates      → IPs locales (mDNS obfusque depuis Chrome 74+/Firefox 78+)
2. Server-reflexive     → IP decouverte via STUN (= IP du VPS grace a l'interception)
3. Relay candidates     → IP du serveur TURN (inheremment safe)

Connectivity Checks (par priorite) :
  host > srflx > relay

Si srflx echoue (IP VPS non routable pour le pair) :
  → ICE bascule sur relay (TURN) automatiquement
  → Latence d'etablissement +2-5s (timeout srflx check)
  → Qualite audio/video maintenue
```

**Le Voile n'interfere PAS avec les flux media :**
- RTP/RTCP : premiers 2 bits = `0b10` → `IsSTUN()` retourne false → passthrough
- TURN Data/Send : types STUN mais PAS Binding Request → `IsBindingRequest()` retourne false → passthrough
- Les flux media TURN transitent directement entre le client et le serveur TURN, JAMAIS par le tunnel Le Voile

### Mecanisme Kill Switch et STUN

**Etat actuel du blocage STUN quand le tunnel est coupe :**

1. Le kill switch arrete le proxy DNS → `127.0.0.1:53` refuse les connexions → DNS bloque
2. Le Relayer verifie `tunnel.State() == StateConnected` → si deconnecte, drop silencieux
3. ForwardFunc est `nil` → les paquets non-STUN sur 3478/5349 sont deja droppes

**Gap identifie :** L'intercepteur continue de LISTEN sur 3478/5349 meme pendant le kill switch. Les paquets arrivent mais sont droppes par le Relayer. Cependant, si un attaquant ou une app envoie des paquets craftes, le Relayer les traite inutilement avant de les dropper.

**Solution story 5.3 :** Ajouter `SetEnabled(false)` sur l'Interceptor ET le Relayer — court-circuit total quand le kill switch est actif. Aucun processing de paquet, drop immediat dans `handlePacket()` avant meme la classification.

### Architecture de l'Outil de Validation Anti-Fuite

**Approche : STUN Binding Request direct depuis le client Go**

L'outil de leak check fait exactement ce que font browserleaks.com et ipleak.net :
1. Envoie un STUN Binding Request UDP a un serveur STUN public
2. Parse la reponse STUN Binding Response
3. Extrait l'attribut XOR-MAPPED-ADDRESS (type `0x0020`, RFC 5389 §15.2)
4. Compare l'IP obtenue avec l'IP HTTP visible (obtenue via le tunnel)

**XOR-MAPPED-ADDRESS Parsing (RFC 5389 §15.2) :**
```
Attribut : type 0x0020, length variable
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|x x x x x x x x|    Family     |         X-Port                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                X-Address (32 bits IPv4 / 128 bits IPv6)        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

Family: 0x01 = IPv4, 0x02 = IPv6
X-Port: port XOR avec les 16 bits MSB du Magic Cookie (0x2112)
X-Address IPv4: IP XOR avec Magic Cookie (0x2112A442)
X-Address IPv6: IP XOR avec Magic Cookie + Transaction ID (16 bytes)
```

Le parsing STUN header existe deja dans `internal/stun/parser.go`. Le parsing d'attribut XOR-MAPPED-ADDRESS est NOUVEAU (story 5.3).

**Flux du leak check :**
```
LeakChecker.RunFullCheck()
  → CheckSTUNLeak("stun.l.google.com:19302")  // UDP direct
  → CheckSTUNLeak("stun1.l.google.com:19302") // UDP direct
  → CheckSTUNLeak("stun.cloudflare.com:3478") // UDP direct
  → Chaque requete passe par l'intercepteur local (ports 3478/19302)
  → L'intercepteur relaie via tunnel → VPS execute le STUN → retourne IP VPS
  → Compare IP STUN avec IP HTTP publique (obtenue via tunnel DoH)
  → Si toutes identiques = PASS, sinon = FAIL
```

**ATTENTION :** Le leak check envoie des paquets UDP aux serveurs STUN sur le port 19302. L'intercepteur ecoute sur 3478 et 5349. Les paquets vers le port 19302 NE passent PAS par l'intercepteur. Le leak check simule le scenario REEL : test si le STUN direct (sans passer par l'intercepteur) revele l'IP reelle. C'est le TEST CORRECT — il detecte les fuites que l'intercepteur ne couvre pas.

### Integration avec le Code Existant

**`internal/stun/interceptor.go` — Ajout SetEnabled :**
```go
type Interceptor struct {
    // existant...
    enabled atomic.Bool  // NOUVEAU — defaut true
}

func (i *Interceptor) SetEnabled(enabled bool) {
    i.enabled.Store(enabled)
}

// Dans handlePacket() — court-circuit en premier :
func (i *Interceptor) handlePacket(packet []byte, src *net.UDPAddr) {
    if !i.enabled.Load() {
        return // drop immediat
    }
    // ... classification existante ...
}
```

**`internal/stun/relayer.go` — Ajout SetEnabled :**
```go
type Relayer struct {
    // existant...
    enabled atomic.Bool  // NOUVEAU — defaut true
}

func (r *Relayer) SetEnabled(enabled bool) {
    r.enabled.Store(enabled)
}

// Dans HandleIntercept() — court-circuit en premier :
func (r *Relayer) HandleIntercept(packet []byte, src *net.UDPAddr, hdr *Header) {
    if !r.enabled.Load() {
        return // drop immediat
    }
    // ... relai existant ...
}
```

**`internal/service/service.go` — Connexion au kill switch :**
Le service observe deja l'etat du tunnel via le state channel. Quand le tunnel passe en `disconnected` :
- Le kill switch s'active (existant)
- Appeler `stunInterceptor.SetEnabled(false)` et `stunRelayer.SetEnabled(false)` (NOUVEAU)

Quand le tunnel repasse en `connected` :
- Le kill switch se desactive (existant)
- Appeler `stunInterceptor.SetEnabled(true)` et `stunRelayer.SetEnabled(true)` (NOUVEAU)

**`internal/ipc/messages.go` — Nouvelles constantes :**
```go
ActionLeakCheck    = "leak_check"      // Declenche un test de fuite WebRTC
StatusLeakPass     = "pass"            // Aucune fuite detectee
StatusLeakFail     = "fail"            // Fuite detectee
```

**`internal/leakcheck/webrtc.go` — NOUVEAU package :**
```go
package leakcheck

type LeakResult struct {
    Server    string `json:"server"`
    IP        net.IP `json:"ip"`
    Leaked    bool   `json:"leaked"`
}

type FullLeakReport struct {
    Status    string        `json:"status"`   // "pass" ou "fail"
    STUNIP    string        `json:"stun_ip"`  // IP vue par les serveurs STUN
    HTTPIP    string        `json:"http_ip"`  // IP HTTP publique via tunnel
    Results   []LeakResult  `json:"results"`
}
```

### Patterns Go a Respecter

- **Package** : `internal/leakcheck` — minuscule, deux mots accoles (pattern Go idiomatique)
- **Fichiers** : `webrtc.go` (checker), `xor_mapped.go` (parsing attribut), `webrtc_test.go`, `xor_mapped_test.go`
- **Erreurs** : `fmt.Errorf("leakcheck: stun: %w", err)`, `fmt.Errorf("stun: turn: %w", err)`
- **Concurrence** : `context.Context` premier argument, `atomic.Bool` pour `enabled` (pas de mutex — operation simple load/store)
- **Tests** : table-driven, noms `TestIsTURN`, `TestInterceptor_TURNPassthrough`, `TestLeakChecker_RunFullCheck`
- **Aucun log client** — erreurs propagees via IPC
- **Copie defensive** du paquet avant dispatch (pattern story 5.1)

### Intelligence Stories Precedentes (5.1 et 5.2)

**Patterns etablis a reutiliser :**
- `atomic.Bool` pour flags thread-safe (story 5.1 fix data race `STUNActive()`)
- Copie defensive du paquet avant goroutine (story 5.1 fix)
- Semaphore channel pour limiter la concurrence (20 max dans relayer)
- ForwardFunc/InterceptFunc callbacks pour decouplage
- Extension IPC backward-compatible via nouvelles actions/status
- Protection double `Start()` via flag atomique
- Drop silencieux en cas d'erreur (aucun log client)

**Issues corrigees en code review 5.1/5.2 — NE PAS reproduire :**
- Data race → utiliser `atomic.Bool` pour `enabled` (pas `sync.Mutex`)
- Fuite STUN en chemin d'erreur → SetEnabled coupe AVANT tout processing
- SSRF dans STUNHandler → allowlist ports STUN deja en place (story 5.2 fix H1)
- Validation paquet STUN cote relay → `isValidSTUNPacket()` deja en place (story 5.2 fix H2)

**Fichiers modifies en stories 5.1/5.2 — ATTENTION aux conflits :**
- `internal/stun/stun.go` — ajouter constantes TURN (pas de conflit)
- `internal/stun/parser.go` — ajouter `IsTURN()` (pas de conflit)
- `internal/stun/interceptor.go` — ajouter `enabled` atomic.Bool + check dans `handlePacket` (modifier existant)
- `internal/stun/relayer.go` — ajouter `enabled` atomic.Bool + check dans `HandleIntercept` (modifier existant)
- `internal/service/service.go` — ajouter connexion kill switch ↔ STUN enable/disable (modifier `startSTUN`)
- `internal/ipc/messages.go` — ajouter constantes leak check (pas de conflit)
- `internal/ipchandler/handler.go` — ajouter handler leak check (pas de conflit)

### Procedure de Test Manuel (browserleaks.com / ipleak.net)

**Verification post-implementation :**
1. Demarrer Le Voile (tunnel connecte, icone verte)
2. Ouvrir https://browserleaks.com/webrtc dans le navigateur
3. Verifier : section "WebRTC Leak Test" ne montre PAS l'IP reelle
4. Les candidates srflx doivent afficher l'IP du VPS islandais
5. Les candidates host doivent etre obfusques par mDNS (format `.local`)
6. Ouvrir https://ipleak.net et verifier la section WebRTC
7. Tester aussi avec le kill switch actif (deconnecter le tunnel) : aucun candidate ne doit etre genere

### NFRs Impactees

- **NFR17** (Latence STUN < 10ms) : Le fallback TURN ajoute 2-5s au premier etablissement ICE (timeout srflx check). Ce n'est PAS un overhead du proxy STUN mais un comportement normal d'ICE quand l'IP srflx n'est pas routable. La latence media via TURN est independante de Le Voile.
- **NFR5** (Zero fuite DNS/IP) : Le leak check automatise via IPC permet une verification continue. Le blocage STUN via `SetEnabled(false)` garantit zero fuite pendant le kill switch.
- **NFR11** (RAM < 20MB) : Le package `leakcheck` est leger — 3 UDP sockets temporaires, parsing minimal. Impact negligeable.

### Project Structure Notes

Nouveaux fichiers a creer :
```
internal/
├── leakcheck/                  # NOUVEAU — Validation anti-fuite WebRTC
│   ├── webrtc.go               # WebRTCLeakChecker, RunFullCheck, CheckSTUNLeak
│   ├── webrtc_test.go          # Tests du checker (mock STUN server)
│   ├── xor_mapped.go           # parseXORMappedAddress (RFC 5389 §15.2)
│   └── xor_mapped_test.go      # Tests du parsing XOR-MAPPED-ADDRESS
```

Fichiers existants modifies :
- `internal/stun/stun.go` — Ajout constantes types TURN
- `internal/stun/parser.go` — Ajout `IsTURN()`
- `internal/stun/parser_test.go` — Tests `IsTURN`
- `internal/stun/interceptor.go` — Ajout `enabled` atomic.Bool + `SetEnabled()` + check dans `handlePacket`
- `internal/stun/interceptor_test.go` — Tests TURN passthrough, RTP passthrough, disabled drops
- `internal/stun/relayer.go` — Ajout `enabled` atomic.Bool + `SetEnabled()` + check dans `HandleIntercept`
- `internal/stun/relayer_test.go` — Test disabled drops
- `internal/service/service.go` — Connexion kill switch ↔ STUN enable/disable, reference relayer pour SetEnabled
- `internal/service/service_test.go` — Test integration kill switch + STUN
- `internal/ipc/messages.go` — Ajout ActionLeakCheck, StatusLeakPass, StatusLeakFail
- `internal/ipchandler/handler.go` — Ajout handleLeakCheck()
- `internal/ipchandler/handler_test.go` — Test handleLeakCheck
- `internal/tray/tray.go` — Ajout item menu "Verifier fuite WebRTC"

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 5 — Story 5.3]
- [Source: _bmad-output/planning-artifacts/architecture.md#Communication & Protocole]
- [Source: _bmad-output/planning-artifacts/architecture.md#Patterns de Concurrence]
- [Source: _bmad-output/planning-artifacts/architecture.md#Error Handling]
- [Source: _bmad-output/planning-artifacts/architecture.md#Structure Projet]
- [Source: _bmad-output/implementation-artifacts/5-1-interception-et-parsing-des-paquets-stun.md#Dev Notes]
- [Source: _bmad-output/implementation-artifacts/5-1-interception-et-parsing-des-paquets-stun.md#Completion Notes]
- [Source: _bmad-output/implementation-artifacts/5-2-relai-stun-via-tunnel-et-substitution-dip.md#Dev Notes]
- [Source: _bmad-output/implementation-artifacts/5-2-relai-stun-via-tunnel-et-substitution-dip.md#Completion Notes]
- [Source: internal/stun/interceptor.go — InterceptFunc/ForwardFunc, handlePacket]
- [Source: internal/stun/relayer.go — HandleIntercept, TunnelStateChecker]
- [Source: internal/dns/kill_switch.go — Activate/Deactivate, stopProxy/startProxy]
- [Source: internal/service/service.go — startSTUN/stopSTUN, shutdown sequence]
- [Source: RFC 5389 — Session Traversal Utilities for NAT (STUN)]
- [Source: RFC 5766 — Traversal Using Relays around NAT (TURN)]
- [Source: RFC 5764 — Multiplexing STUN/RTP/RTCP]
- [Source: RFC 8828 — WebRTC IP Address Handling Requirements]
- [Source: browserleaks.com/webrtc — WebRTC Leak Test methodology]

## Change Log

- 2026-03-10: Implementation complete de la story 5.3 — TURN passthrough, kill switch STUN blocking, leak check WebRTC, integration tray, tests d'integration
- 2026-03-10: Code review — 7 issues trouvees (2 HIGH, 3 MEDIUM, 2 LOW). Fixes appliques: H1 (leak check reference IP via tunnel relay), H2 (erreur si tous STUN echouent), M1 (relay timeout 10s), M2 (test handleLeakCheck ipchandler), M3 (stunServers comme champ struct). Fonctions ParseXORMappedAddress et BuildBindingRequest exportees.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

Aucun probleme de debug rencontre.

### Completion Notes List

- **Task 1 (TURN passthrough):** Ajout de 5 constantes TURN (RFC 5766) dans stun.go, fonction IsTURN() dans parser.go avec 9 tests table-driven. Tests TestInterceptor_TURNPassthrough et TestInterceptor_RTPNotTouched confirment le comportement passthrough.
- **Task 2 (Kill switch STUN):** Ajout de atomic.Bool enabled + SetEnabled() sur Interceptor et Relayer. Court-circuit immediat dans handlePacket/HandleIntercept quand disabled. Integration dans service.go via setSTUNEnabled() connecte aux callbacks du KillSwitch (stopProxy/startProxy wrappees). Tests unitaires et integration confirment le blocage.
- **Task 3 (Leak check WebRTC):** Nouveau package internal/leakcheck avec WebRTCLeakChecker, parseXORMappedAddress (RFC 5389 §15.2), RunFullCheck multi-serveur. IPC ActionLeakCheck + handler dans ipchandler. Tests avec mock STUN server UDP.
- **Task 4 (Tray integration):** Menu item "Verifier fuite WebRTC" visible uniquement quand connecte. Clic envoie leak_check IPC, tooltip temporaire 10s avec resultat. 3 tests tray (pass/fail/error).
- **Task 5 (Integration tests):** TestSTUNProxy_TURNFallback simule scenario ICE complet. TestLeakCheck_WithProxy valide le flux end-to-end. Procedure de test manuel documentee dans Dev Notes.
- **Note:** Les echecs de tests preexistants dans internal/dns (Windows-specific) et internal/tunnel (flaky 403) ne sont PAS lies a cette story.

### File List

Nouveaux fichiers :
- internal/leakcheck/webrtc.go
- internal/leakcheck/webrtc_test.go
- internal/leakcheck/xor_mapped.go
- internal/leakcheck/xor_mapped_test.go

Fichiers modifies :
- internal/stun/stun.go
- internal/stun/parser.go
- internal/stun/parser_test.go
- internal/stun/interceptor.go
- internal/stun/interceptor_test.go
- internal/stun/relayer.go
- internal/stun/relayer_test.go
- internal/service/service.go
- internal/service/service_test.go
- internal/ipc/messages.go
- internal/ipchandler/handler.go
- internal/ipchandler/handler_test.go
- internal/tray/tray.go
- internal/tray/tray_test.go
- _bmad-output/implementation-artifacts/sprint-status.yaml
