# Story 5.1: Interception et parsing des paquets STUN

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que Le Voile intercepte les requêtes STUN de découverte IP émises par mes navigateurs et applications WebRTC,
Afin que mon IP réelle ne soit pas exposée lors des handshakes WebRTC.

## Acceptance Criteria

1. **Given** le service actif et le tunnel connecté
   **When** une application émet un paquet STUN Binding Request (RFC 5389) sur les ports 3478/5349 UDP
   **Then** le paquet est intercepté par le service avant de quitter le réseau local

2. **Given** un paquet UDP intercepté
   **When** il n'est pas un paquet STUN valide (magic cookie 0x2112A442 absent)
   **Then** le paquet est transmis normalement sans modification

3. **Given** un paquet STUN intercepté
   **When** le type est autre que Binding Request (ex: STUN Indication)
   **Then** le paquet est transmis normalement (seules les requêtes de découverte IP sont traitées)

## Tasks / Subtasks

- [x] Task 1 : Créer le package `internal/stun` avec le parser STUN (AC: #1, #2, #3)
  - [x] 1.1 Définir les constantes STUN : magic cookie `0x2112A442`, type Binding Request `0x0001`, taille header 20 bytes
  - [x] 1.2 Implémenter `IsSTUN(packet []byte) bool` — vérifie taille >= 20 bytes + magic cookie aux octets 4-7
  - [x] 1.3 Implémenter `IsBindingRequest(packet []byte) bool` — vérifie type message = `0x0001` aux octets 0-1
  - [x] 1.4 Implémenter `ParseHeader(packet []byte) (*Header, error)` — extrait type, length, magic cookie, transaction ID (12 bytes)
  - [x] 1.5 Écrire tests unitaires table-driven pour `IsSTUN`, `IsBindingRequest`, `ParseHeader` avec cas valides/invalides/edge cases
- [x] Task 2 : Créer le listener UDP pour l'interception sur ports 3478 et 5349 (AC: #1)
  - [x] 2.1 Implémenter `Interceptor` struct avec `net.UDPConn` listener sur ports 3478 et 5349
  - [x] 2.2 Intégrer `context.Context` pour lifecycle (arrêt propre via cancel)
  - [x] 2.3 Implémenter la boucle de lecture UDP avec `ReadFromUDP()` et dispatch
  - [x] 2.4 Écrire tests unitaires pour le listener (mock UDP, vérifier réception paquets)
- [x] Task 3 : Implémenter la logique de classification et routage des paquets (AC: #1, #2, #3)
  - [x] 3.1 Implémenter `handlePacket(packet []byte, addr *net.UDPAddr)` — classifie et route
  - [x] 3.2 Si pas STUN (`IsSTUN` = false) → forward transparent (passthrough)
  - [x] 3.3 Si STUN mais pas Binding Request → forward transparent (passthrough)
  - [x] 3.4 Si STUN Binding Request → marquer pour interception (sera relayé via tunnel dans story 5.2)
  - [x] 3.5 Écrire tests unitaires pour chaque branche de classification
- [x] Task 4 : Intégrer l'intercepteur au service principal (AC: #1)
  - [x] 4.1 Ajouter démarrage de l'intercepteur dans le lifecycle du service (`internal/service/service.go`)
  - [x] 4.2 Conditionner le démarrage au statut tunnel connecté (pas d'interception sans tunnel)
  - [x] 4.3 Arrêt propre de l'intercepteur lors de la déconnexion/arrêt du service
  - [x] 4.4 Ajouter status IPC pour l'état du proxy STUN (actif/inactif)
  - [x] 4.5 Écrire tests d'intégration pour le cycle démarrage/arrêt

## Dev Notes

### Contexte Technique Critique

**Protocole STUN (RFC 5389) — Format du paquet :**
```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|0 0|     STUN Message Type     |         Message Length        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         Magic Cookie (0x2112A442)             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                                                               |
|                     Transaction ID (96 bits)                  |
|                                                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

- **Header** : 20 bytes fixes (2 type + 2 length + 4 magic cookie + 12 transaction ID)
- **Les 2 premiers bits** du premier octet DOIVENT être `0b00` (distingue STUN de RTP/RTCP)
- **Magic Cookie** : `0x2112A442` aux octets 4-7 (big-endian)
- **Binding Request** : type `0x0001`
- **Binding Response** : type `0x0101` (Success), `0x0111` (Error)
- **RTP/RTCP** : premiers 2 bits != `0b00` → ne JAMAIS intercepter

**Ports STUN standard :**
- **3478** : STUN over UDP (port principal)
- **5349** : STUN over TLS/DTLS
- Certaines applications utilisent aussi des ports non-standard — cette story se limite à 3478/5349

### Architecture d'Interception

**Approche choisie : UDP Proxy local (pas de packet capture raw)**

L'interception se fait via un proxy UDP local qui écoute sur les ports 3478/5349. Le système DNS du client (déjà géré par Epic 2) redirige le trafic STUN vers le proxy local. Cette approche :
- Ne nécessite PAS de privilèges raw socket / packet capture
- Est cohérente avec l'architecture existante (service privilégié qui gère les couches réseau)
- Fonctionne cross-platform (pas de WinDivert ou iptables)

**Diagramme de flux :**
```
App WebRTC → UDP port 3478/5349 → Interceptor (écoute locale)
  → IsSTUN? Non → Forward transparent vers destination originale
  → IsSTUN + Binding Request? Oui → Queue pour relay via tunnel (story 5.2)
  → IsSTUN + autre type? → Forward transparent
```

### Bibliothèque STUN — NE PAS utiliser de dépendance externe

Implémenter le parsing STUN manuellement. Le format est simple (20 bytes header), et les bibliothèques comme `pion/stun` ou `gortc/stun` sont des implémentations complètes client+server dont nous n'avons besoin que du parsing header. Cela reste cohérent avec la philosophie du projet (dépendances minimales).

### Patterns Go à Respecter (Architecture)

- **Package** : `internal/stun` — minuscule, un mot
- **Fichiers** : `stun.go` (types/constantes), `parser.go` (parsing), `interceptor.go` (listener UDP), `interceptor_test.go`, `parser_test.go`
- **Fonctions** : `PascalCase` exportées, `camelCase` internes
- **Erreurs** : `fmt.Errorf("stun: operation: %w", err)` — wrapping avec préfixe package
- **Concurrence** : `context.Context` en premier argument, channels pour communication inter-goroutines
- **Tests** : table-driven pour > 2 cas, nom format `TestParser_IsSTUN`, `TestInterceptor_HandlePacket`
- **Aucun log client** — erreurs propagées via IPC
- **Pas de panic** sauf bug critique

### Intégration Service Existant

Le service (`cmd/client/main.go`) utilise `kardianos/service` avec le pattern lifecycle suivant :
- `Start()` lance tunnel + DNS + watchdog + IPC
- `Stop()` arrête tout proprement avec restauration DNS

L'intercepteur STUN doit :
1. Se démarrer APRÈS la connexion tunnel réussie (pas avant)
2. S'arrêter AVANT la restauration DNS (pendant `Stop()`)
3. Être géré via `context.Context` (cancel pour arrêt)
4. Reporter son état via IPC (nouvel action `stun_status` → `{active: bool}`)

### IPC — Extension Backward-Compatible

Comme établi dans Story 3.3, l'extension IPC se fait via :
- Ajout de nouvelles actions dans `internal/ipc/messages.go`
- Champ `Value string` avec `omitempty` pour backward-compat
- Nouveau message : `{action: "stun_status", status: "active"/"inactive"}`

### Différenciation STUN vs RTP/RTCP (CRITIQUE)

Les paquets RTP et RTCP partagent les mêmes ports que STUN. La RFC 5764 définit le multiplexage :
- **STUN** : premiers 2 bits = `0b00` (octet 0 entre 0-3 en valeur)
- **RTP** : premiers 2 bits = `0b10` (octet 0 entre 128-191)
- **RTCP** : premiers 2 bits = `0b10` (octet 0 entre 128-191, différencié par payload type)

**RÈGLE ABSOLUE** : Seuls les paquets avec premiers 2 bits = `0b00` ET magic cookie valide sont des paquets STUN. Tout le reste passe en transparent. Ne JAMAIS toucher aux flux media RTP/RTCP.

### Project Structure Notes

Nouveau package à créer :
```
internal/
├── stun/                    # NOUVEAU — Proxy STUN transparent
│   ├── stun.go              # Types, constantes, magic cookie
│   ├── parser.go            # IsSTUN, IsBindingRequest, ParseHeader
│   ├── parser_test.go       # Tests table-driven du parser
│   ├── interceptor.go       # UDP listener, classification, routage
│   └── interceptor_test.go  # Tests du listener/intercepteur
```

Fichiers existants modifiés :
- `cmd/client/main.go` — Démarrage/arrêt intercepteur dans lifecycle service
- `internal/ipc/messages.go` — Ajout action `stun_status`

**Aucun conflit** avec la structure existante. Le package `stun` est indépendant et s'intègre au service via le même pattern que `dns`, `tunnel`, `watchdog`.

### Leçons des Stories Précédentes (Intelligence)

**De Story 4.2 (portable) :**
- Le systray DOIT tourner sur le main thread — ne pas bloquer avec l'intercepteur
- `prg.Stop(nil)` doit garantir l'arrêt de l'intercepteur avant exit
- L'intercepteur doit supporter le mode portable (même lifecycle que service)

**De Story 3.3 (menu/IPC) :**
- Extensions IPC via `Value string` + `omitempty` — pattern établi
- Actions IPC : format `snake_case`
- Thread-safety : mutex pour état partagé entre goroutines

**De Story 2.3 (kill switch/watchdog) :**
- Le kill switch bloque TOUT le trafic réseau quand le tunnel est coupé
- L'intercepteur STUN doit aussi cesser de fonctionner quand le kill switch est actif
- Pattern watchdog goroutine applicable pour monitoring de l'intercepteur

### NFRs Impactées

- **NFR5** (Zéro fuite DNS) : L'intercepteur ne doit pas créer de chemin de fuite DNS
- **NFR11** (RAM < 20MB) : Le buffer UDP doit être dimensionné raisonnablement (max 1500 bytes/paquet = MTU standard)
- **NFR17** (Latence STUN < 10ms additionnel) : Le parsing header est O(1) sur 20 bytes — impact négligeable. La latence sera dominée par le relay tunnel (story 5.2)
- **NFR3** (Résistance DPI) : Les paquets STUN relayés via le tunnel QUIC/HTTPS sont indistinguables du trafic web

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 5 — Story 5.1]
- [Source: _bmad-output/planning-artifacts/architecture.md#Patterns de Concurrence]
- [Source: _bmad-output/planning-artifacts/architecture.md#Structure Projet]
- [Source: _bmad-output/planning-artifacts/architecture.md#Conventions Nommage]
- [Source: _bmad-output/planning-artifacts/architecture.md#Error Handling]
- [Source: _bmad-output/implementation-artifacts/4-2-version-portable-et-build-multi-plateforme.md#Dev Notes]
- [Source: _bmad-output/implementation-artifacts/3-3-menu-clic-droit-et-controles-utilisateur.md#Dev Notes]
- [Source: _bmad-output/implementation-artifacts/2-3-kill-switch-dns-watchdog-et-reconnexion-automatique.md]
- [Source: RFC 5389 — Session Traversal Utilities for NAT (STUN)]
- [Source: RFC 5764 — Multiplexing STUN/RTP/RTCP]

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6

### Debug Log References
Aucun problème de debug rencontré.

### Completion Notes List
- Task 1 : Package `internal/stun` créé avec types/constantes STUN (RFC 5389) dans `stun.go`, fonctions de parsing dans `parser.go` (IsSTUN, IsBindingRequest, ParseHeader), et 22 tests table-driven dans `parser_test.go`. Vérification des 2 premiers bits (0b00) pour distinguer STUN de RTP/RTCP.
- Task 2 : Intercepteur UDP (`Interceptor`) écoutant sur 2 ports avec `context.Context` pour lifecycle. Boucle de lecture avec `ReadFromUDP()` et dispatch. 4 tests d'intégration (start/stop, réception, 2 listeners).
- Task 3 : Logique de classification via `handlePacket()` : non-STUN → forward, STUN non-Binding → forward, STUN Binding Request → intercept. 6 cas de test table-driven pour chaque branche.
- Task 4 : Intégration au service principal : `startSTUN()`/`stopSTUN()` dans le lifecycle (après tunnel, avant DNS restore), action IPC `stun_status` (active/inactive), méthode `STUNActive()`. Démarrage best-effort (non-fatal). 3 tests d'intégration.

### Implementation Plan
Approche red-green-refactor : tests écrits avant l'implémentation pour chaque task. L'intercepteur utilise des callbacks (`ForwardFunc`, `InterceptFunc`) pour le routage, permettant un découplage avec la story 5.2 (relay via tunnel). Le démarrage STUN est best-effort pour ne pas bloquer le service si les ports sont occupés.

### Change Log
- 2026-03-10 : Implémentation complète de la story 5.1 — Package stun (parser + intercepteur), intégration service, action IPC stun_status.
- 2026-03-10 : Code review — Corrigé 7 issues (2 HIGH, 5 MEDIUM) : data race STUNActive(), fuite STUN en chemin d'erreur, dispatch asynchrone, copie défensive Addrs(), protection double Start(), constantes IPC, test handleSTUNStatus.

### File List
- internal/stun/stun.go (nouveau) — Types, constantes STUN (Header, MagicCookie, TypeBindingRequest)
- internal/stun/parser.go (nouveau) — IsSTUN, IsBindingRequest, ParseHeader
- internal/stun/parser_test.go (nouveau) — 22 tests table-driven du parser
- internal/stun/interceptor.go (nouveau) — UDP listener, classification asynchrone, routage (ForwardFunc/InterceptFunc)
- internal/stun/interceptor_test.go (nouveau) — 4 tests d'intégration intercepteur
- internal/service/service.go (modifié) — Ajout stunInterceptor, startSTUN/stopSTUN, STUNActive() thread-safe
- internal/service/service_test.go (modifié) — 3 tests STUN lifecycle
- internal/ipc/messages.go (modifié) — Ajout ActionSTUNStatus, StatusSTUNActive, StatusSTUNInactive
- internal/ipchandler/handler.go (modifié) — Ajout handleSTUNStatus()
- internal/ipchandler/handler_test.go (modifié) — Ajout TestHandle_STUNStatus_Inactive
