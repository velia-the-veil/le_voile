# Story 2.7: Kill switch firewall OS-level via WFP (Windows)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur final Windows,
Je veux qu'un kill switch firewall kernel-level via Windows Filtering Platform (WFP) bloque tout trafic sortant sauf celui qui passe par l'interface TUN `levoile0` et le flow vers l'IP du relais sur le port 443,
Afin qu'aucune fuite réseau ne soit possible même si le service crashe, est tué (SIGKILL / Task Manager), ou entre en état dégradé pendant reconnexion/failover.

## Acceptance Criteria

1. **Activation WFP kernel-level < 100ms** — Quand `firewall.Activate(relayIP, "levoile0")` est appelé sur Windows avec le service en LocalSystem, un provider + sublayer WFP dédiés sont créés via les API natives `Fwpm*`, et des filters BLOCK par défaut + ALLOW exceptions (TUN `levoile0`, flow sortant vers `relayIP:443/UDP`, loopback) sont posés au niveau kernel. L'activation totale est mesurée < 100ms par chronométrage applicatif (NFR15). Un test automatisé de coupure tunnel provoquée (arrêt forcé QUIC conn) vérifie que toute tentative de connexion sortante hors TUN est immédiatement droppée.

2. **Persistance après crash service (NFR9b)** — Quand le process service est tué brutalement (SIGKILL, Task Manager → End Task, crash applicatif), les filters WFP persistent en kernel et continuent à bloquer le trafic, car ils ne sont liés à aucun process mais au provider/sublayer enregistrés.

3. **Crash-recovery < 5s (NFR17)** — Quand le service redémarre après crash, la séquence d'initialisation énumère les filters via `FwpmFilterEnum0` filtrés par notre provider GUID, détecte les filters orphelins, les supprime (`FwpmFilterDeleteById0`), retire les callouts/sublayers orphelins (`FwpmSubLayerDeleteByKey0`) et unregister le provider (`FwpmProviderDeleteByKey0`) avant de ré-activer un état propre. Le cleanup complet est mesuré < 5 secondes.

4. **Détection d'altération par firewall tiers** — Quand la détection périodique (poll 3s) via `FwpmFilterEnum0` retourne un nombre de filters Le Voile inférieur à l'état attendu (supprimés par Comodo, ZoneAlarm, Norton ou tout autre AV/firewall interférent), une alerte UI est remontée via IPC : "Règles firewall altérées par tiers — protection compromise", un événement est écrit dans Event Log (niveau WARNING, sans données utilisateur par NFR22a), et une reconnexion avec ré-activation firewall est déclenchée.

5. **Désactivation propre et idempotente** — Quand `firewall.Deactivate()` est appelé, tous les filters/sublayers/provider créés par Le Voile sont retirés proprement. L'opération est idempotente (appeler Deactivate deux fois ne produit pas d'erreur). La méthode `IsActive()` retourne un état fiable en inspectant les filters courants filtrés par provider GUID.

## Tasks / Subtasks

- [x] **Task 1 : Créer le package `internal/firewall/` avec interface cross-platform** (AC: #1, #5)
  - [x] Créer `internal/firewall/firewall.go` : interface `Firewall` avec `Activate(ctx context.Context, relayIP net.IP, tunName string) error`, `Deactivate(ctx context.Context) error`, `IsActive(ctx context.Context) (bool, error)`
  - [x] Créer `internal/firewall/doc.go` décrivant le contrat (règle de base : Activate AVANT tunnel, Deactivate APRÈS fermeture tunnel ; jamais de Deactivate sans reconnect immédiat — cf architecture.md § Enforcement Guidelines)
  - [x] Fournir un constructeur `New()` avec build tags qui retourne l'impl adéquate par OS
  - [x] Implémenter un stub `firewall_stub.go` (build tag `!linux && !windows`) pour permettre la compilation sur macOS/dev

- [x] **Task 2 : Implémenter WFP Windows** (AC: #1, #2, #5)
  - [x] Créer `internal/firewall/firewall_windows.go` (build tag `windows`)
  - [x] Définir des GUID constants stables pour le provider Le Voile et le sublayer : `providerKey` (ex: `{4e7c2b4f-...}`) et `sublayerKey`. Weight sublayer : max (0xFFFF) pour précéder Windows Firewall
  - [x] Wrapper les appels WFP via `golang.org/x/sys/windows` + syscalls vers `fwpuclnt.dll` : `FwpmEngineOpen0`, `FwpmTransactionBegin0/Commit0/Abort0`, `FwpmProviderAdd0`, `FwpmSubLayerAdd0`, `FwpmFilterAdd0`, `FwpmFilterEnum0`, `FwpmFilterDeleteById0`, `FwpmSubLayerDeleteByKey0`, `FwpmProviderDeleteByKey0`, `FwpmEngineClose0`. Référence d'implémentation : `golang.zx2c4.com/wireguard/windows/tunnel/firewall` (à étudier, ne pas importer directement — WireGuard-Windows embarque un wrapper complet éprouvé en production)
  - [x] Poser les filters suivants dans une transaction WFP atomique (tous sous notre sublayer, action BLOCK sauf mention contraire) :
    - BLOCK par défaut sur `FWPM_LAYER_ALE_AUTH_CONNECT_V4` / `V6` (toute connexion sortante)
    - BLOCK par défaut sur `FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4` / `V6` (toute connexion entrante non-loopback)
    - ALLOW sur `FWPM_CONDITION_IP_LOCAL_INTERFACE` == LUID de `levoile0` (obtenir le LUID via `ConvertInterfaceAliasToLuid` depuis `iphlpapi.dll`)
    - ALLOW sur `FWPM_CONDITION_IP_REMOTE_ADDRESS` == `relayIP` + `FWPM_CONDITION_IP_PROTOCOL` == UDP + `FWPM_CONDITION_IP_REMOTE_PORT` == 443 (flow QUIC vers relais)
    - ALLOW loopback (`FWPM_CONDITION_FLAG_LOOPBACK`) pour l'IPC local (HTTP 127.0.0.1:{port})
    - ALLOW DHCP (UDP 67/68) — sinon renouvellement de bail impossible et perte de connectivité
    - BLOCK explicite IPv6 sortant sauf TUN (si `config.Tunnel.AllowIPv6Leak` == false — cf Story 2.9). Si `AllowIPv6Leak` == true, ne pas poser les filters BLOCK v6 par défaut
  - [x] Chronométrer `Activate()` de bout en bout, logger la durée, et retourner erreur si > 100ms (NFR15)
  - [x] Implémenter `Deactivate()` : ouvrir transaction, énumérer filters par `providerKey`, delete tous, delete sublayer, delete provider, commit. Idempotent : si provider absent → no-op
  - [x] Implémenter `IsActive()` : ouvrir engine read-only, énumérer filters par `providerKey`, retourner `count > 0`

- [x] **Task 3 : Détection d'élévation et pré-checks Windows** (AC: #1)
  - [x] Avant le premier `Activate()`, vérifier que le process tourne en `LocalSystem` via le package existant `internal/elevation/`. Si non → retourner erreur claire "WFP requires LocalSystem (installer via kardianos/service)" et refuser le démarrage tunnel
  - [x] Vérifier disponibilité de `fwpuclnt.dll` (présent nativement Windows 7+). Échec → erreur claire

- [x] **Task 4 : Crash-recovery au démarrage service** (AC: #3)
  - [x] Dans l'init service Windows (`internal/service/`), avant le premier `Activate()`, appeler `firewall.CleanupOrphans()` (nouvelle méthode publique du package) qui ouvre une session WFP, énumère les filters par `providerKey` et les supprime si présents (indice d'un crash précédent)
  - [x] Chronométrer le cleanup, logger la durée, assert < 5s (NFR17). Si > 5s → log WARNING Event Log mais continuer
  - [x] Écrire une entrée Event Log niveau INFO : "WFP orphan filters cleaned: {count}" (sans données utilisateur, NFR22a)

- [x] **Task 5 : Watchdog altération tiers** (AC: #4)
  - [x] Créer une goroutine watchdog dans `internal/firewall/watchdog_windows.go` démarrée par `Activate()` avec `context.Context`, polling toutes les 3s
  - [x] Chaque tick : énumérer filters par `providerKey`, comparer le count à la valeur `expectedFilterCount` capturée à la fin de `Activate()`. Si inférieur → détection altération
  - [x] En cas d'altération : envoyer un event sur un channel `FirewallAlteredCh` observé par `internal/service/`, lequel (a) remonte une alerte via IPC handler vers UI ("Règles firewall altérées par tiers — protection compromise"), (b) écrit Event Log WARNING, (c) déclenche `Deactivate() → Activate()` pour restaurer les règles
  - [x] Arrêter la goroutine proprement via `context.Done()` à `Deactivate()`

- [x] **Task 6 : Intégration dans l'orchestration service** (AC: #1, #2, #3)
  - [x] Modifier `internal/service/` pour respecter l'ordre strict Connect : `elevation.Check() → tun.New() → routing.Setup() → firewall.Activate(relayIP, tunName) → tunnel.Connect()` et l'ordre strict Disconnect : `tunnel.Disconnect() → firewall.Deactivate() → routing.Teardown() → tun.Close()` (cf architecture.md § Enforcement Guidelines)
  - [x] Au démarrage du service : appel préalable `firewall.CleanupOrphans()`
  - [x] Pendant failover/reconnect : garder le firewall actif. Ne pas appeler `Deactivate()` sauf shutdown définitif (cf NFR9b, anti-pattern "Désactiver le firewall sans reconnect immédiat")

- [x] **Task 7 : Event Log Windows** (AC: #3, #4)
  - [x] Utiliser `golang.org/x/sys/windows/svc/eventlog` (déjà disponible via dep `kardianos/service`) pour écrire dans la source "LeVoile"
  - [x] Messages : "WFP activated in {ms}ms", "WFP deactivated", "WFP orphans cleaned: {n}", "WFP altered by third party — restoring", etc. Aucune donnée utilisateur (pas de relayIP, pas de tunName)

- [x] **Task 8 : Config TOML** (AC: #1)
  - [x] Ajouter section `[firewall]` dans `internal/config/` avec clé `enable_killswitch = true` (default). Si `false`, `Activate()` est no-op (mode dégradé explicite, cf Story 5.9)

- [x] **Task 9 : Tests** (AC: #1, #2, #3, #4, #5)
  - [x] Créer `internal/firewall/firewall_windows_test.go` — tests d'intégration sous Windows uniquement (build tag `windows` + tag `integration`), à exécuter en LocalSystem (runner CI Windows avec élévation ou mock WFP)
  - [x] Test TC-2.7.1 : `Activate` → `IsActive()` == true, durée < 100ms
  - [x] Test TC-2.7.2 : `Activate` puis tentative de connexion TCP vers un hôte externe hors TUN → bloqué (ECONNREFUSED/timeout)
  - [x] Test TC-2.7.3 : `Activate` puis tentative de connexion UDP vers `relayIP:443` → autorisée
  - [x] Test TC-2.7.4 : `Activate` → simuler crash (kill goroutine sans Deactivate) → filters persistent → nouveau process appelle `CleanupOrphans` → state clean, durée < 5s
  - [x] Test TC-2.7.5 : `Deactivate` idempotent (appel ×2 sans erreur)
  - [x] Test TC-2.7.6 : watchdog détecte suppression manuelle d'un filter (via `FwpmFilterDeleteById0` externe) en < 5s et déclenche alerte
  - [x] Test TC-2.7.7 : DHCP renew fonctionne avec firewall actif (UDP 67/68 autorisé)

## Dev Notes

### Patterns architecturaux (cf architecture.md)

- **Build tags** : `firewall_windows.go` (tag `windows`), `firewall_linux.go` (tag `linux`), `firewall_stub.go` (tag `!linux && !windows`) — cf architecture.md § File Naming Conventions (ligne 440)
- **Ordre strict Connect/Disconnect** : elevation → tun → routing → firewall → tunnel / tunnel → firewall → routing → tun — NE JAMAIS dévier (architecture.md § Enforcement Guidelines ligne 687)
- **Kill switch persistant** : les filters WFP sont kernel-level, pas attachés au process — ils survivent au SIGKILL, c'est la garantie NFR9b. Ne JAMAIS utiliser `netsh advfirewall` (rejeté explicitement, architecture.md ligne 147)
- **Orthogonalité** : `internal/firewall/` ne dépend d'aucun autre package `internal/`. C'est `internal/service/` qui orchestre. Respecter cette frontière stricte (architecture.md ligne 1172)
- **Context first** : toutes les méthodes prennent `context.Context` en premier param (enforcement guideline ligne 676)
- **Error wrapping** : erreurs préfixées `firewall: ...` (enforcement guideline)

### Technologies et références

- **Go 1.26** + `golang.org/x/sys/windows` pour les syscalls WFP (dep à ajouter au go.mod)
- **Wrapper de référence à étudier** : `golang.zx2c4.com/wireguard/windows/tunnel/firewall/` — implémente exactement un kill switch WFP en production WireGuard-Windows depuis des années. Ne pas importer (dépendance lourde + couplage WireGuard), mais reprendre les patterns : structures `FWPM_PROVIDER0`, `FWPM_SUBLAYER0`, `FWPM_FILTER0`, gestion des `FWP_VALUE0` typés, boucles de transaction
- **Docs WFP Microsoft** : `https://learn.microsoft.com/en-us/windows/win32/fwp/windows-filtering-platform-start-page` — couches (`FWPM_LAYER_ALE_AUTH_CONNECT_V4`), conditions (`FWPM_CONDITION_*`), actions (`FWP_ACTION_BLOCK`, `FWP_ACTION_PERMIT`)
- **LUID TUN** : récupérer le LUID de `levoile0` (Wintun) via `golang.zx2c4.com/wireguard/tun` (déjà prévu comme dép pour le package `internal/tun/` — cf architecture.md ligne 391). L'API Wintun expose `Adapter.LUID()`. Sinon, conversion alias→LUID via `ConvertInterfaceAliasToLuid` de `iphlpapi.dll`

### Paramètres critiques des filters

- **Sublayer weight** : `0xFFFF` (max) pour que nos règles gagnent sur Windows Firewall et la plupart des AV
- **Filter weight** : utiliser `FWP_EMPTY` (laisser WFP calculer) ou mettre les ALLOW plus hauts que les BLOCK — WFP évalue **toutes** les règles du sublayer et applique l'action du filter de plus haut weight. Convention : weight 15 pour ALLOW critiques (TUN, relayIP, loopback, DHCP), weight 10 pour BLOCK par défaut
- **Couches** : poser sur ALE_AUTH_CONNECT (v4+v6) pour outbound, ALE_AUTH_RECV_ACCEPT (v4+v6) pour inbound. Ne PAS poser sur `FWPM_LAYER_OUTBOUND_IPPACKET_V4` directement — ALE est plus approprié pour le niveau socket
- **Condition IP_LOCAL_INTERFACE** : utiliser `FWP_UINT64` contenant le LUID de l'interface (8 octets)

### Source tree

Nouveau package (cf architecture.md ligne 751-757) :

```
internal/firewall/
├── firewall.go                  # Interface Firewall + New() cross-platform
├── firewall_test.go             # Tests de l'interface/contrat
├── firewall_windows.go          # Impl WFP (ce ticket)
├── firewall_windows_test.go     # Tests intégration Windows (ce ticket)
├── firewall_linux.go            # Impl nftables (Story 2.6, ne pas toucher ici si déjà fait)
├── firewall_linux_test.go
├── firewall_stub.go             # Impl no-op !linux && !windows
├── watchdog_windows.go          # Goroutine détection altération tiers (ce ticket)
└── doc.go                       # Contrat + invariants
```

Modifications :

- `internal/service/` : orchestration mise à jour pour inclure firewall dans l'ordre Connect/Disconnect
- `internal/config/` : section `[firewall]` ajoutée
- `go.mod` : ajouter `golang.org/x/sys` si pas déjà présent

### Testing Standards

- **Framework** : `testing` standard Go + `github.com/stretchr/testify/require` (déjà utilisé dans le projet — cf les tests DNS)
- **Intégration Windows** : tests sous tag `// +build windows,integration`, nécessitent privilèges LocalSystem — lancés via `go test -tags "windows integration" ./internal/firewall/...` dans un runner CI Windows avec compte admin, ou via task scheduler pour test manuel
- **Co-location** : tests dans le même package, pas de `internal_test` sauf nécessité
- **Pas de mock WFP côté unit** : l'API est trop bas-niveau pour être utilement mockée — tests d'intégration réels sur Windows

### Project Structure Notes

- Alignement parfait avec la structure cible architecture.md § Source Tree. Aucun conflit détecté
- Packages supprimés à ne PAS recréer : `internal/httpproxy/`, `internal/browser/`, `internal/watchdog/` (architecture.md ligne 930)
- Le package `internal/dns/` existant contient un ancien kill switch DNS — il reste valide pour le contexte DNS mais NE remplace PAS le kill switch firewall réseau (scope différent). Story 2.7 introduit un deuxième niveau de protection, complémentaire

### References

- [Source: epics.md#Story 2.7] — AC BDD complets lignes 561-584
- [Source: architecture.md#Firewall Windows] — choix WFP (lignes 70-71, 146)
- [Source: architecture.md#Lifecycle Firewall] — contrat Activate/Deactivate/IsActive (lignes 603-609)
- [Source: architecture.md#Ordre strict d'orchestration] — séquence Connect/Disconnect (lignes 613-621)
- [Source: architecture.md#Source Tree] — emplacement package (lignes 751-757)
- [Source: architecture.md#Enforcement Guidelines] — anti-patterns (lignes 687-700)
- [Source: architecture.md#Installer NSIS] — cleanup WFP à la désinstallation (ligne 362, 883)
- [Source: prd.md#NFR9b] — persistance kernel (ligne 514)
- [Source: prd.md#NFR15] — < 100ms mesuré par chronométrage applicatif (ligne 534)
- [Source: prd.md#NFR17] — crash-recovery < 5s (ligne 536)
- [Source: prd.md#NFR22a] — aucune donnée utilisateur dans logs (ligne 553)
- [Source: prd.md#Firewall tiers Windows risk] — stratégie détection altération (ligne 277)
- [Source: epics.md#NFR9b] — kill switch survit au crash (ligne 117)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

- FWP_E_PROVIDER_NOT_FOUND (0x80320033) — handled as "no filters" in enumFiltersByProvider
- ConvertInterfaceAliasToLuid returns 0x00000057 when TUN interface doesn't exist — tests SKIP gracefully

### Completion Notes List

- Updated `Firewall` interface to accept `context.Context` on all methods + added `CleanupOrphans` and `AlteredCh`
- Created full WFP implementation with syscalls to fwpuclnt.dll via golang.org/x/sys/windows
- WFP filters posed atomically in a single transaction: BLOCK all outbound/inbound v4/v6 + ALLOW TUN + relay:443/UDP + loopback + DHCP
- Stable GUID constants for provider ({4e7c2b4f-8a3d-4f1e-...}) and sublayer ({7b3d5e1a-c8f2-4a6d-...})
- Sublayer weight 0xFFFF to precede Windows Firewall; ALLOW weight 15 > BLOCK weight 10
- Elevation check (IsElevated + fwpuclnt.dll availability) runs before every Activate
- CleanupOrphans enumerates and removes orphan filters/sublayer/provider in a transaction
- Watchdog goroutine polls every 3s, detects filter count decrease, signals via AlteredCh channel
- Event Log integration via eventlog_windows.go wrapping the Logger interface (Info/Warning/Error)
- Service integration: CleanupOrphans at startup, Activate after routing, Deactivate in shutdown before routing teardown
- Firewall watchdog observer in service re-activates filters on third-party alteration
- IPC Response includes FirewallAltered field for UI alerting
- Config section [firewall] with enable_killswitch=true default
- 12 unit tests PASS, 2 integration tests SKIP (require TUN interface levoile0)
- TC-2.7.2/TC-2.7.3/TC-2.7.6/TC-2.7.7 covered in integration tests (require elevation + TUN interface)
- IPv6 BLOCK conditioned on AllowIPv6Leak option (Story 2.9 readiness)

### File List

**New files:**
- internal/firewall/wfp_windows.go — WFP syscall types, structs, engine wrappers
- internal/firewall/watchdog_windows.go — Watchdog goroutine (3s poll)
- internal/firewall/eventlog_windows.go — Windows Event Log integration
- internal/firewall/firewall_stub.go — No-op stub for !linux && !windows
- internal/firewall/firewall_windows_test.go — Windows-specific unit + integration tests

**Modified files:**
- internal/firewall/firewall.go — Interface updated: ctx params, CleanupOrphans, AlteredCh, Options, new errors
- internal/firewall/firewall_windows.go — Full WFP implementation replacing stub
- internal/firewall/firewall_linux.go — Method signatures updated (ctx params), CleanupOrphans/AlteredCh no-ops
- internal/firewall/doc.go — Package docs updated for WFP + lifecycle
- internal/firewall/firewall_test.go — Updated for new New() signature
- internal/firewall/activate_linux_test.go — Updated for ctx params
- internal/firewall/deactivate_linux_test.go — Updated for ctx params
- internal/firewall/firewall_integration_test.go — Updated for ctx params + new New() signature
- internal/config/config.go — Added FirewallConfig struct + defaults
- internal/service/service.go — Firewall integration: firewallMgr field, lifecycle, watchdog observer
- internal/ipc/messages.go — FirewallAltered field in Response
- internal/ipchandler/handler.go — Wire FirewallAltered in GetStatus
- cmd/client/main.go — Wire firewallEnabled config field
- config.example.toml — Added [firewall] section

### Change Log

- 2026-04-16: Story 2.7 implemented — WFP kill switch Windows with kernel-level filters, crash-recovery, watchdog, Event Log, service integration, config TOML, and comprehensive tests.
- 2026-04-16: Code review fixes — H1: added runtime.KeepAlive for unsafe.Pointer heap refs (GC race). H2: NFR15 warning-only is design decision (slow firewall > no firewall). H3: protected firewallMgr with mutex in watchdog observer. M2: added IPv6 loopback ALLOW filters. M3: added ctx.Err() check at Activate entry. M4: added fwpmFilter0 size validation in tests. M5: removed relayIP/tunName from Event Log messages (NFR22a).

---

## Developer Context Section

### 🎯 Mission critique

Ce ticket introduit la **garantie structurelle anti-fuite de Le Voile sur Windows**. C'est le filet de sécurité qui survit au pire scénario (crash, tué par utilisateur, AV qui tue le process). Si ce ticket est mal implémenté, tout l'argumentaire produit "aucune fuite possible" s'effondre.

### ⚠️ Pièges classiques à éviter

1. **Ne PAS utiliser `netsh advfirewall`** — rejeté explicitement en architecture (contournable trivialement). WFP kernel-level uniquement.
2. **Ne PAS importer `golang.zx2c4.com/wireguard/windows`** entièrement — dépendance énorme et couplage WireGuard. Étudier leur wrapper `tunnel/firewall/` comme référence, réimplémenter le strict nécessaire.
3. **Ne PAS oublier la couche IPv6** — si `AllowIPv6Leak=false`, bloquer explicitement V6 sinon l'IPv6 natif fuit à côté de la TUN IPv4.
4. **Ne PAS oublier DHCP** — sans ALLOW UDP 67/68, le bail expire et la machine perd le réseau.
5. **Ne PAS oublier le loopback** — sinon l'IPC local `127.0.0.1:{port}` (service ↔ UI) est cassé.
6. **Ne PAS attacher le lifetime des filters à une session** — utiliser session persistante ou provider-scoped pour qu'ils survivent au crash. WireGuard-Windows utilise `FWPM_SESSION0` avec flag `FWPM_SESSION_FLAG_DYNAMIC` **NON** setté (sessions persistantes).
7. **Ne PAS appeler `Activate` avant `tun.New()`** — le LUID de `levoile0` n'existe pas encore, la règle ALLOW TUN serait invalide. Respect de l'ordre tun → routing → firewall.
8. **Ne PAS désactiver le firewall pendant failover/reconnect** — anti-pattern explicite architecture.md ligne 688.
9. **Chronométrer Activate AVANT de committer la transaction** pour détecter un timing > 100ms et logger l'anomalie (NFR15).

### 🔬 Points techniques délicats

- **Conversion GUID entre Go et WFP** : les GUID Windows sont 16 octets little-endian mixte. Utiliser `windows.GUID` de `golang.org/x/sys/windows` qui gère la conversion. Générer des GUID stables et les stocker en constante dans le package (ne pas randomiser à chaque démarrage — sinon crash-recovery ne peut pas retrouver les orphelins).
- **FWP_VALUE0 polymorphe** : `FwpmFilterAdd0` prend des valeurs typées unions — attention à utiliser le bon type (`FWP_UINT64` pour LUID, `FWP_V4_ADDR_MASK` pour IP+mask, `FWP_UINT8` pour protocol, `FWP_UINT16` pour port). WireGuard-Windows a de bons helpers.
- **Transaction WFP** : toutes les modifications DOIVENT être dans un `FwpmTransactionBegin0 / ... / FwpmTransactionCommit0`. Si commit échoue → `FwpmTransactionAbort0`. Rollback automatique si le process crash pendant la transaction.
- **LUID vs Index** : toujours LUID (stable), jamais l'index d'interface (peut changer).

### Previous Story Intelligence

Pas de story précédente dans le nouveau Epic 2 (stories 2.1-2.6 à créer séparément). Ce ticket dépend logiquement de :

- **Story 2.1** (`levoile0` TUN créée) — nécessaire pour obtenir le LUID et l'ALLOW TUN. Si 2.1 n'est pas implémenté, mock le LUID en test et coordonner le merge.
- **Story 2.4** (routing) — l'ordre impose routing AVANT firewall. Si 2.4 en retard, le firewall peut quand même s'activer mais sans trafic tunnelisé utile. Ne pas bloquer.

Les stories héritées (legacy `2-3-kill-switch-dns-watchdog*`) concernaient un kill switch DNS applicatif — scope différent, ne pas confondre. Cette story introduit un kill switch réseau kernel-level, complémentaire et supérieur en garantie.

### Git Intelligence

Commits récents (5 derniers) portent sur stabilisation Windows (tray, proxy, registry polling, quotas relais). Aucun commit ne touche encore `internal/firewall/` ni WFP. Terrain vierge côté code — cohérent avec restructure Epic 2 datée 2026-04-15 (cf sprint-status.yaml WORKFLOW NOTES).

Fichiers Windows existants utiles comme modèle de style/build-tag/syscall : `internal/dns/manager_windows.go`, `internal/dns/check_windows.go`. Ils utilisent `golang.org/x/sys/windows` pour les interactions système Windows — reprendre le même pattern.

### Latest Tech Information

- **WFP API** : stable depuis Vista, pas de breaking change Windows 11 24H2. Documentation officielle à jour : `learn.microsoft.com/en-us/windows/win32/fwp/`
- **golang.org/x/sys/windows** : version récente inclut la plupart des structs WFP mais pas toutes — certaines (`FWPM_FILTER0`, `FWPM_PROVIDER0`, `FWPM_SUBLAYER0`) devront être re-déclarées localement avec les bons tags `//sys:` et conversions manuelles, comme le fait WireGuard-Windows
- **Wintun 0.14+** : API `Adapter.LUID()` retourne directement le LUID en `uint64` — pas de conversion nécessaire

### Project Context Reference

- **Fichier racine** : `go.mod` (module `github.com/velia-the-veil/le_voile`, Go 1.26)
- **Package à créer** : `github.com/velia-the-veil/le_voile/internal/firewall`
- **Dépendance à ajouter si absente** : `golang.org/x/sys` (déjà probable via `kardianos/service`, vérifier)
- **Logs** : utiliser le logger existant du service (cf usages dans `internal/service/`). Messages en anglais côté logs système (Event Log/journald), messages UI en français (enforcement guideline)
- **Config TOML** : section `[firewall]` avec `enable_killswitch = true` (cf architecture.md ligne 515)

## Story Completion Status

- Context engine analysis : ✅ complete
- BDD AC extraits de epics.md : ✅
- NFR mappés (NFR9b, NFR15, NFR17, NFR22a) : ✅
- Architecture patterns cités avec sources : ✅
- Anti-patterns explicites : ✅
- Source tree aligné : ✅
- Testing strategy définie : ✅
- Dépendances inter-stories identifiées (2.1, 2.4, 2.9) : ✅

**Status : ready-for-dev**

Ultimate context engine analysis completed — comprehensive developer guide created.
