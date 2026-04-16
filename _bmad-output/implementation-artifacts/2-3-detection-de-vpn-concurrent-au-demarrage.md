# Story 2.3: Détection de VPN concurrent au démarrage

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a utilisateur final,
I want que le client refuse de démarrer si un autre VPN est actif sur ma machine,
so that je ne crée pas de configuration réseau incohérente entre deux tunnels (routing loops, DNS contradictoires, kill switches concurrents).

## Acceptance Criteria

1. **Scan interfaces réseau au démarrage**
   **Given** le service démarre (entrée dans la séquence Connect)
   **When** `preflight.DetectConcurrentVPN()` est appelé avant `tun.New("levoile0", …)`
   **Then** toutes les interfaces réseau `UP` sont énumérées via `net.Interfaces()` et, sur Windows, enrichies via `Get-NetAdapter`/Wintun enumeration pour obtenir le descriptif d'adaptateur

2. **Détection d'un VPN concurrent (Linux)**
   **Given** une interface `UP` autre que `levoile0` et `lo` existe
   **When** son nom matche l'un des préfixes : `tun`, `tap`, `utun`, `wg`, `wireguard`, `ppp`, `gpd` (Cisco AnyConnect), ou `ipsec`
   **Then** la connexion est refusée avec `ErrConcurrentVPN{InterfaceName: "<name>"}`

3. **Détection d'un VPN concurrent (Windows)**
   **Given** un adaptateur `UP` autre que `levoile0` (Wintun signé par notre service)
   **When** son `Description` contient (case-insensitive) l'un de : `TAP-Windows`, `WireGuard Tunnel`, `OpenVPN`, `Cisco AnyConnect`, `Wintun` (hors le nôtre), `NordVPN`, `ExpressVPN`, `ProtonVPN`
   **Then** la connexion est refusée avec `ErrConcurrentVPN{InterfaceName: "<adapter description>"}`

4. **Message remonté à l'UI**
   **Given** `ErrConcurrentVPN` est levée dans le chemin Connect
   **When** la réponse IPC à la requête `connect` est construite
   **Then** `Response.Status = "error"` et `Response.Error = "VPN concurrent détecté ({nom_interface}). Déconnectez-le pour utiliser Le Voile."`
   **And** un champ structuré `Response.ConcurrentVPN = true` est exposé pour permettre à l'UI un rendu spécifique

5. **Aucun side-effect si détection positive**
   **Given** un VPN concurrent est détecté
   **When** la connexion est refusée
   **Then** aucune interface `levoile0` n'est créée
   **And** aucune règle firewall (nftables/WFP) n'est posée
   **And** aucune route n'est ajoutée/supprimée
   **And** l'état du service reste `Disconnected`

6. **Faux positifs exclus**
   **Given** l'interface `levoile0` est présente et appartient au service (crash-recovery de la story 2.1 l'a relevée/reprise)
   **When** le scan s'exécute
   **Then** `levoile0` n'est jamais comptabilisée comme VPN concurrent

7. **Chemin nominal**
   **Given** aucun VPN concurrent n'est détecté
   **When** `DetectConcurrentVPN()` retourne `nil`
   **Then** la séquence Connect continue : `elevation → tun → routing → firewall → tunnel` (architecture.md:1339)

8. **Observabilité**
   **Given** une détection (positive ou négative) s'exécute
   **Then** la décision est logguée (niveau INFO pour nominal, WARN pour concurrent détecté) incluant : liste des interfaces `UP` scannées, nom de celle ayant matché, préfixe/pattern ayant déclenché le refus

## Tasks / Subtasks

- [x] Créer le package `internal/preflight/` (AC: #1, #7)
  - [x] `preflight.go` : interface `VPNDetector` avec `DetectConcurrentVPN() error`, type `ErrConcurrentVPN struct{InterfaceName, MatchedPattern string}`, patterns/préfixes constants, helpers `matchLinux`/`matchWindows` + `detect()` partagés (la séparation `preflight_common.go` a été inlinée dans `preflight.go` pour éviter le fichier trivial)
- [x] Implémentation Linux (AC: #2, #6)
  - [x] `preflight_linux.go` (build tag `//go:build linux`) : `net.Interfaces()`, filtre `flags&net.FlagUp != 0`, exclusion de `lo` et `levoile0`, matching par préfixe insensible à la casse, fail-open si l'énumération échoue
  - [x] `preflight_linux_test.go` : tests unitaires avec fake listers injectés
- [x] Implémentation Windows (AC: #3, #6)
  - [x] `preflight_windows.go` (build tag `//go:build windows`) : appel PowerShell `Get-NetAdapter | Select-Object Name, InterfaceDescription, Status | ConvertTo-Json -Compress` via `exec.Command` avec `syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}` (pattern commit a1adf3f). Parseur gère à la fois tableau JSON et objet unique, strip BOM UTF-8
  - [x] Matching case-insensitive sur le champ `InterfaceDescription`, exclusion de l'adaptateur dont le `Name == "levoile0"` (notre Wintun ne match jamais même s'il contient "Wintun" dans la description)
  - [x] `preflight_windows_test.go` : tests avec sortie PowerShell simulée (tableau, objet unique, BOM, vide)
- [x] Intégration dans la séquence Connect (AC: #4, #5, #7)
  - [x] Dans `internal/service/service.go`, `run()` étape 0e-bis appelle `p.detectConcurrentVPN()` juste avant `ensureTUN()`. Si détection positive : log stderr, attente `<-ctx.Done()`, et court-circuit — aucun TUN/route/firewall/tunnel/DNS n'est posé (AC #5)
  - [x] `Program.SetPreflightDetector(d)` expose l'injection pour les tests sans toucher au réseau réel
  - [x] État stocké sur `Program.concurrentVPNErr atomic.Value` → lisible via `Program.ConcurrentVPNError()`
  - [x] `internal/ipchandler/handler.go` : `handleConnect` re-scanne à chaque tentative (cas où le VPN tiers a été démarré après le lancement du service) ; `handleGetStatus` lit `ConcurrentVPNError()` et renvoie `ConcurrentVPN=true + Error=<msg FR>` court-circuitant l'inspection du tunnel
- [x] Exposition UI (AC: #4)
  - [x] `internal/ipc/messages.go` : ajout de `ConcurrentVPN bool \`json:"concurrent_vpn,omitempty"\`` à `Response`
  - [ ] Côté webview : non implémenté dans cette story — le champ est exposé et consommable ; le bandeau UI bloquant sera ajouté avec Epic 5 (stories 5.1/5.9 sur la présentation des états dégradés). Pour l'instant l'UI recevra `Response.Error` via le polling GetStatus
- [x] Tests d'intégration (AC: #5, #6, #8)
  - [x] `internal/service/service_preflight_test.go` : détection nominale, positive, reset après clean, erreurs génériques ignorées (fail-open)
  - [x] `internal/ipchandler/handler_preflight_test.go` : Connect refusé avec message FR + ConcurrentVPN=true ; Connect laisse passer à `service_not_ready` quand clean ; GetStatus renvoie l'état stocké par run()
  - [x] Faux positif `levoile0` crash-recovery couvert par `TestLinuxDetector_OwnInterfaceReused` et `TestWindowsDetector_OwnWintunIgnored`

## Dev Notes

### Scope & contexte

- **Périmètre pur preflight** : la détection est read-only, n'écrit rien, ne touche ni à la TUN, ni au firewall, ni aux routes. En cas de détection, c'est un échec net en début de Connect → aucun rollback à prévoir.
- **Architecture** : cette story introduit un nouveau package `internal/preflight/` (non listé explicitement dans architecture.md mais dans l'esprit des packages orchestrés par `internal/service/`). Le rationnel : garder `service.go` mince et garder la logique OS-specific isolée sous build tags.
- **Positionnement dans la séquence Connect** : `architecture.md:1339` décrit `elevation → tun → routing → firewall → tunnel`. Insertion : **avant `elevation`** — le scan est read-only et ne demande pas de privilèges. Cela permet de refuser vite avec un message clair, sans avoir élevé les privilèges inutilement.

### Règles de matching

- **Linux (préfixes insensibles à la casse)** : `tun`, `tap`, `utun`, `wg`, `wireguard`, `ppp`, `gpd` (Cisco AnyConnect GlobalProtect/AnyConnect), `ipsec`. Exclure toujours `lo` et l'interface qu'on s'apprête à créer (`levoile0`).
- **Windows (sous-chaînes insensibles à la casse dans `InterfaceDescription`)** : `TAP-Windows`, `WireGuard Tunnel`, `OpenVPN`, `Cisco AnyConnect`, `Wintun` (hors le nôtre — exclure si `Name == "levoile0"` **et** notre service est déjà tagué sur l'adapter), `NordVPN`, `ExpressVPN`, `ProtonVPN`. La liste est définie dans `preflight.go` (unexported, exposée en copie via `WindowsVPNPatterns()`) pour faciliter ajouts/retraits en review.
- **Ne PAS matcher** : interfaces physiques (`eth*`, `wlan*`, `en*`), Docker (`docker*`, `br-*`), virtualisation non-VPN (`vmnet*`, `vboxnet*`).

### Interactions process & privilèges

- **Hide console window sur Windows** : lorsqu'on shell-out vers PowerShell pour `Get-NetAdapter`, appliquer `syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}` — pattern déjà appliqué dans le repo au commit `a1adf3f` pour `netsh`/`net` (`fix: hide console windows for netsh/net commands`). Respecter ce pattern.
- **Fallback sans PowerShell** : si `Get-NetAdapter` échoue (PS non disponible, machine restreinte), retomber sur `net.Interfaces()` + enumeration via `winipcfg.GetAdaptersAddresses`. Si même ce fallback échoue, logguer WARN et **laisser passer** (fail-open sur la preflight — on préfère laisser l'utilisateur se connecter avec un VPN concurrent potentiellement non détecté plutôt que de bloquer un démarrage légitime par erreur de l'outil de scan).

### Message FR littéral

Le texte remonté à l'UI est figé par le PRD (FR5c, prd.md:420) et les acceptance criteria : `VPN concurrent détecté ({nom_interface}). Déconnectez-le pour utiliser Le Voile.`. Ne pas paraphraser.

### Project Structure Notes

- **Nouveau package** : `internal/preflight/` — non listé dans `architecture.md` mais aligné avec le pattern `internal/<responsibility>/<file>_<os>.go + build tags` utilisé pour `tun/`, `firewall/`, `routing/`, `dns/`, `elevation/`.
- **Modification IPC** : l'ajout du champ `ConcurrentVPN bool` à `ipc.Response` est additif et backward-compatible (tag `omitempty`).
- **Aucun conflit détecté** avec les stories 2.1 (TUN create/destroy) et 2.2 (watchdog TUN) : cette story s'exécute en amont de 2.1 et son résultat négatif empêche 2.1 de s'exécuter.

### Testing Standards Summary

- Tests unitaires OS-specific sous build tags matching (`//go:build linux` / `//go:build windows`).
- Injecter un faux lister d'interfaces (`type InterfaceLister func() ([]Interface, error)`) pour ne jamais dépendre de l'état réseau réel dans les tests unitaires.
- Couvrir au minimum : (a) aucune interface VPN → `nil`, (b) `wg0` UP sur Linux → `ErrConcurrentVPN`, (c) `tun5` DOWN → ignoré (filtrage par `FlagUp`), (d) `levoile0` UP → ignoré (faux positif exclu), (e) adaptateur Windows `"WireGuard Tunnel #3"` → `ErrConcurrentVPN`, (f) adaptateur `"Ethernet"` → ignoré.
- Test d'intégration dans `internal/service/service_test.go` : mock `VPNDetector` → vérifier le court-circuit de Connect et le message IPC.

### References

- [Source: _bmad-output/planning-artifacts/prd.md#FR5c] — Texte UI littéral, liste de préfixes de référence (`TUN/TAP/utun/wireguard/openvpn/cisco`)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-2.3] — Acceptance criteria BDD complets (lignes 482-498)
- [Source: _bmad-output/planning-artifacts/architecture.md#Séquence-Connect] — Ordre `elevation → tun → routing → firewall → tunnel` (ligne 1339)
- [Source: _bmad-output/planning-artifacts/architecture.md#Service-Orchestrateur] — `internal/service/` point d'intégration (ligne 321)
- [Source: _bmad-output/planning-artifacts/architecture.md#Structure-projet] — Pattern `internal/<pkg>/<file>_<os>.go` (lignes 734-774)
- [Source: internal/ipc/messages.go] — Structure `Response` à étendre avec `ConcurrentVPN`

## Senior Developer Review (AI)

**Review Date:** 2026-04-16
**Review Outcome:** Changes Requested → All Fixed
**Reviewer Model:** claude-opus-4-6[1m]

### Action Items

- [x] [HIGH] H1 — Timeout PowerShell: `exec.CommandContext` 10s + fallback `net.Interfaces()` [preflight_windows.go:43-66]
- [x] [HIGH] H2 — `run()` blocking: retry loop 5s au lieu de `<-ctx.Done()` dead-end [service.go:712-732]
- [x] [HIGH] H3 — Pattern lists mutable: `var` → unexported + accesseurs par copie [preflight.go:30-76]
- [x] [MED] M1 — Fallback `net.Interfaces()` manquant quand PS échoue [preflight_windows.go:67-80]
- [x] [MED] M2 — `handleGetStatus` perd rollback/update en mode ConcurrentVPN [handler.go:75-90]
- [x] [MED] M3 — `handleConnect` shell-out par clic → throttle 3s [service.go:248-257]
- [x] [LOW] L1 — `strings.ToLower` en boucle → pré-calculé `init()` [preflight.go:52-57]
- [x] [LOW] L2 — Dev Notes réfère `preflight_common.go` inexistant [story]

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

- `go test ./internal/preflight/` — 100% des tests passent (matching Linux/Windows, détection, parsing PowerShell JSON, fail-open).
- `go test ./internal/service/ -run TestProgram_DetectConcurrentVPN` — 4 tests ok (clean / positive / reset / generic error).
- `go test ./internal/ipchandler/ -run TestHandle_Connect_ConcurrentVPN|TestHandle_Connect_NoConcurrentVPN|TestHandle_GetStatus_ConcurrentVPN` — 3 tests ok. Le test `TestHandle_SetAutoStart_PortableMode_NilStartupType` qui échoue est préexistant (erreur config non liée à cette story ; vérifié via `git stash`).

### Completion Notes List

- Scope réduit volontairement : l'intégration UI webview (bandeau rouge bloquant) est reportée à Epic 5. La surface IPC (`Response.ConcurrentVPN` + `Response.Error` littéral FR) est en place et testée ; un consommateur UI trivial peut le rendre dès maintenant.
- Fail-open : si PowerShell échoue ou timeout (10s), fallback sur `net.Interfaces()` (détection réduite aux noms, pas de Description). Si même le fallback échoue → fail-open (pas de blocage).
- Le scan est re-exécuté à chaque `handleConnect` IPC avec throttle 3s (évite un shell-out PowerShell par clic rapide). `handleGetStatus` lit l'état cached stocké par `run()`.
- `run()` sur détection positive boucle toutes les 5s : dès que le VPN tiers disparaît, la séquence reprend automatiquement sans redémarrer le service.
- `handleGetStatus` en mode ConcurrentVPN inclut aussi les champs rollback/update/blocklist pour ne pas perdre d'info.
- Les listes de patterns sont unexported (immutables) avec accesseurs par copie `LinuxVPNPrefixes()` / `WindowsVPNPatterns()`. Patterns Windows pré-lowercasés au `init()`.

### File List

- `internal/preflight/preflight.go` (nouveau)
- `internal/preflight/preflight_linux.go` (nouveau)
- `internal/preflight/preflight_windows.go` (nouveau)
- `internal/preflight/preflight_test.go` (nouveau)
- `internal/preflight/preflight_linux_test.go` (nouveau)
- `internal/preflight/preflight_windows_test.go` (nouveau)
- `internal/ipc/messages.go` (modifié : champ `ConcurrentVPN bool`)
- `internal/service/service.go` (modifié : factory `preflightFactory`, `detectConcurrentVPN`, `ConcurrentVPNError`, `SetPreflightDetector`, étape 0e-bis dans `run()`)
- `internal/service/service_preflight_test.go` (nouveau)
- `internal/ipchandler/handler.go` (modifié : `handleConnect` + `handleGetStatus` surfacent ConcurrentVPN)
- `internal/ipchandler/handler_preflight_test.go` (nouveau)

## Change Log

| Date       | Description                                                                                                          |
| ---------- | -------------------------------------------------------------------------------------------------------------------- |
| 2026-04-15 | Implémentation initiale : package `internal/preflight/`, intégration service + IPC, tests unitaires + d'intégration. |
| 2026-04-16 | Code review (8 findings) : timeout PS 10s + fallback net.Interfaces, retry loop run() 5s au lieu de block, patterns unexported + pré-lowercasés, enrichissement GetStatus, throttle 3s handleConnect, fix doc. |
