# Story 2.3: Détection de VPN concurrent au démarrage

Status: ready-for-dev

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

- [ ] Créer le package `internal/preflight/` (AC: #1, #7)
  - [ ] `preflight.go` : interface `VPNDetector` avec `DetectConcurrentVPN() error`, type `ErrConcurrentVPN struct{InterfaceName, MatchedPattern string}`
  - [ ] `preflight_common.go` : logique de matching de noms/descriptions partagée, liste de préfixes/patterns en constantes exportées pour tests et reuse
- [ ] Implémentation Linux (AC: #2, #6)
  - [ ] `preflight_linux.go` (build tag `//go:build linux`) : `net.Interfaces()`, filtre `flags&net.FlagUp != 0`, exclusion de `lo` et `levoile0`, matching par préfixe insensible à la casse
  - [ ] `preflight_linux_test.go` : tests unitaires avec fake listers injectés (ne pas dépendre de l'état réseau réel dans les tests unitaires)
- [ ] Implémentation Windows (AC: #3, #6)
  - [ ] `preflight_windows.go` (build tag `//go:build windows`) : appel PowerShell `Get-NetAdapter | Where-Object Status -eq 'Up'` via `exec.Command` avec `syscall.SysProcAttr{HideWindow: true}` (voir commit a1adf3f), parsing JSON via `-OutputFormat Json`. Fallback : `net.Interfaces()` + `wireguard/windows/conf/winipcfg.GetAdaptersAddresses` pour enrichir la description
  - [ ] Matching case-insensitive sur le champ `InterfaceDescription`, exclusion des adaptateurs dont le hardware ID matche notre Wintun (`Wintun` + nom `levoile0`)
  - [ ] `preflight_windows_test.go` : tests avec sortie PowerShell simulée
- [ ] Intégration dans la séquence Connect (AC: #4, #5, #7)
  - [ ] Dans `internal/service/service.go`, appeler `preflight.DetectConcurrentVPN()` au tout début de la méthode `Connect` (avant `tun.New`, avant élévation de privilèges — c'est purement read-only donc ne nécessite pas root/Admin pour le scan Linux, et est bénin sur Windows)
  - [ ] Si erreur `ErrConcurrentVPN` : retourner immédiatement sans créer d'interface/route/firewall
  - [ ] Propager via IPC : étendre `ipc.Response` avec `ConcurrentVPN bool` et alimenter `Error` avec le message FR exact (voir AC #4)
- [ ] Exposition UI (AC: #4)
  - [ ] `internal/ipc/messages.go` : ajouter `ConcurrentVPN bool \`json:"concurrent_vpn,omitempty"\`` à `Response`
  - [ ] Côté UI (`internal/ui/` + webview) : afficher un bandeau d'erreur bloquant avec le texte `Response.Error` si `ConcurrentVPN == true`. Le bouton Connect reste actif pour un retry après déconnexion manuelle du VPN tiers
- [ ] Tests d'intégration (AC: #5, #6, #8)
  - [ ] `internal/service/service_test.go` : mock `VPNDetector` → `ErrConcurrentVPN` → vérifier que ni TUN ni firewall ni routing ne sont appelés, et que la réponse IPC contient le message FR attendu
  - [ ] Vérifier que `levoile0` pré-existant (crash-recovery) n'est pas flaggé

## Dev Notes

### Scope & contexte

- **Périmètre pur preflight** : la détection est read-only, n'écrit rien, ne touche ni à la TUN, ni au firewall, ni aux routes. En cas de détection, c'est un échec net en début de Connect → aucun rollback à prévoir.
- **Architecture** : cette story introduit un nouveau package `internal/preflight/` (non listé explicitement dans architecture.md mais dans l'esprit des packages orchestrés par `internal/service/`). Le rationnel : garder `service.go` mince et garder la logique OS-specific isolée sous build tags.
- **Positionnement dans la séquence Connect** : `architecture.md:1339` décrit `elevation → tun → routing → firewall → tunnel`. Insertion : **avant `elevation`** — le scan est read-only et ne demande pas de privilèges. Cela permet de refuser vite avec un message clair, sans avoir élevé les privilèges inutilement.

### Règles de matching

- **Linux (préfixes insensibles à la casse)** : `tun`, `tap`, `utun`, `wg`, `wireguard`, `ppp`, `gpd` (Cisco AnyConnect GlobalProtect/AnyConnect), `ipsec`. Exclure toujours `lo` et l'interface qu'on s'apprête à créer (`levoile0`).
- **Windows (sous-chaînes insensibles à la casse dans `InterfaceDescription`)** : `TAP-Windows`, `WireGuard Tunnel`, `OpenVPN`, `Cisco AnyConnect`, `Wintun` (hors le nôtre — exclure si `Name == "levoile0"` **et** notre service est déjà tagué sur l'adapter), `NordVPN`, `ExpressVPN`, `ProtonVPN`. La liste est en constante exportée dans `preflight_common.go` pour faciliter ajouts/retraits en review.
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

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
