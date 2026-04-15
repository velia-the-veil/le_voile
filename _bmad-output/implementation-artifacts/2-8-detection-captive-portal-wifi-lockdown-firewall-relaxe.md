# Story 2.8: Détection captive portal Wi-Fi + lockdown firewall relaxé

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a utilisateur final,
I want que le client détecte les portails captifs Wi-Fi et autorise temporairement l'authentification,
So that je puisse me connecter à un Wi-Fi public puis activer la protection Le Voile.

## Acceptance Criteria

**AC1 — Détection initiale captive portal (probe redirect)**

**Given** le client démarre sur un nouveau réseau Wi-Fi (service Connect demandé ou démarrage auto)
**When** une probe HTTP RFC 7710 est émise (`http://captive.apple.com/hotspot-detect.html` par défaut, fallback `http://connectivitycheck.gstatic.com/generate_204`) **avant** activation du kill switch plein
**Then** si la réponse est un status `30x` (redirect) OU un body différent du contenu attendu (`Success` pour Apple, body vide pour `/generate_204`), le mode `CAPTIVE` est activé
**And** le firewall applique un ruleset `Activate(relayIP, tunName, Mode=Captive)` qui autorise **uniquement** : (a) trafic sortant vers la gateway réseau local (LAN default gateway détectée via `net.InterfaceByName` + routing table lookup), (b) loopback ; tout le reste est `drop` (pas de trafic Internet, pas de tunnel)
**And** un évènement IPC `captive-portal-detected` est émis vers l'UI
**And** l'état tunnel reste `Disconnected` (aucune tentative QUIC vers le relais — gateway-only firewall le bloquerait de toute façon)

**AC2 — Bandeau UI captive portal**

**Given** le mode `CAPTIVE` est actif
**When** l'UI reçoit l'évènement IPC `captive-portal-detected`
**Then** un bandeau d'avertissement s'affiche dans la webview : « Portail Wi-Fi détecté. Authentifiez-vous sur le portail, puis cliquez "Activer la protection". »
**And** le bandeau est persistant (non-dismissable) tant que le mode captive est actif
**And** le bouton `Connect` principal est masqué ou désactivé, remplacé par un bouton `Activer la protection` (ou `Retry`)
**And** l'icône tray passe à un état visuel distinct (orange `captive` — distinct de rouge `kill-switch-failure` et vert `connected`)

**AC3 — Re-probe + transition automatique vers kill switch plein**

**Given** le mode `CAPTIVE` est actif
**When** l'utilisateur clique `Activer la protection` OU qu'un timer périodique de 15s déclenche un re-probe
**Then** la même probe HTTP est ré-émise
**And** si la réponse est un status `200` avec le body attendu (`Success` pour Apple) OU `204 No Content` (pour `/generate_204`), la transition automatique démarre :
  1. Passage firewall `Mode=Captive` → `Mode=Full` (règles full : TUN + relayIP:443 uniquement)
  2. Démarrage séquence Connect normale (registry → TUN → routing → tunnel QUIC → session token)
**And** l'évènement IPC `captive-portal-cleared` est émis, le bandeau UI disparaît
**And** si la probe continue à indiquer un redirect, le mode captive reste actif et un nouveau re-probe est programmé dans 15s

**AC4 — Timeout probe + skip captive**

**Given** la probe HTTP initiale est émise
**When** aucune réponse n'est reçue en 3 secondes (timeout) OU une erreur réseau (pas de DNS, pas de route)
**Then** le mode captive n'est **PAS** activé (pas de redirect observé ≠ portail captif)
**And** le démarrage continue vers la séquence Connect normale
**And** un log niveau `info` trace `captive-probe timeout, assuming no portal`

**AC5 — Désactivation explicite utilisateur**

**Given** l'utilisateur n'utilise jamais de réseau Wi-Fi public (VPS, domicile fibre)
**When** l'utilisateur coche `[ ] Désactiver la détection captive portal` dans Paramètres avancés
**Then** la config TOML persiste `[tunnel] captive_portal_detection = false`
**And** au prochain démarrage, aucune probe n'est émise — séquence Connect directe avec kill switch plein

## Tasks / Subtasks

- [ ] **Task 1 — Package `internal/captive/` : probe HTTP RFC 7710** (AC: 1, 4)
  - [ ] 1.1 Créer `internal/captive/probe.go` avec `Probe(ctx, client *http.Client) (ProbeResult, error)`
  - [ ] 1.2 Types : `ProbeResult` = enum `NoPortal | PortalDetected | ProbeError` + URL tentée + status observé
  - [ ] 1.3 Utiliser `http.Client` avec `Timeout=3s`, `CheckRedirect` retourne `http.ErrUseLastResponse` pour capturer le `Location` header sans suivre (signe captive)
  - [ ] 1.4 URLs par défaut en constantes : `captive.apple.com/hotspot-detect.html` (body attendu `Success`), fallback `connectivitycheck.gstatic.com/generate_204` (body attendu vide + 204)
  - [ ] 1.5 Logique de détection : `30x` → portail ; `200` avec body inattendu → portail ; `200`+body attendu ou `204` vide → pas de portail ; timeout/erreur → `ProbeError`
  - [ ] 1.6 Test unitaire `probe_test.go` avec `httptest.Server` simulant redirect 302, 200+Success, 200+HTML portail, 204, timeout

- [ ] **Task 2 — Mode firewall `Captive` dans `internal/firewall/`** (AC: 1, 3)
  - [ ] 2.1 Étendre l'interface `Firewall` (définie par stories 2.6/2.7) : ajouter param `Mode` à `Activate` → signature devient `Activate(relayIP net.IP, tunName string, mode Mode) error` où `Mode = ModeFull | ModeCaptive | ModeOff`
  - [ ] 2.2 **Linux (nftables)** — ajouter template `ruleset_captive.nft.tmpl` : autorise `ip daddr {lan_gateway}` (CIDR /32) + loopback + ICMP local ; drop tout le reste. La gateway LAN est résolue via `routing.DefaultGateway()` (helper à ajouter si absent)
  - [ ] 2.3 **Windows (WFP)** — filters BLOCK sur toutes interfaces sauf flow sortant vers `{lan_gateway}` (any port) + loopback. Sublayer dédié captive
  - [ ] 2.4 Transition `Captive → Full` : implémenter `Transition(newMode Mode, relayIP, tunName)` idempotent qui flush l'ancien ruleset et applique le nouveau sans fenêtre d'exposition (atomique côté nftables via `nft -f` transaction ; côté WFP via `FwpmTransactionBegin/Commit`)
  - [ ] 2.5 Test unitaire (mocké + e2e Linux) : activation Captive → `nft list ruleset` contient la règle gateway ; transition Full → gateway retirée, TUN+relay ajoutés

- [ ] **Task 3 — Détection gateway LAN dans `internal/routing/`** (AC: 1)
  - [ ] 3.1 Ajouter `DefaultGateway() (net.IP, string, error)` (retour = gateway IP + nom interface)
  - [ ] 3.2 **Linux** : parser `/proc/net/route` (hex) OU `ip route show default` — gateway = ligne `default via X.X.X.X dev ifaceY`
  - [ ] 3.3 **Windows** : `GetBestRoute2` ou `winipcfg.GetIPForwardTable2` pour trouver la route `0.0.0.0/0` native (metric la plus basse parmi interfaces physiques, **exclure `levoile0`** si déjà présente)
  - [ ] 3.4 **Attention** : cette fonction doit être appelable **avant** qu'une TUN existe — donc pas de dépendance circulaire avec `internal/tun/`
  - [ ] 3.5 Test unitaire avec mock de la routing table

- [ ] **Task 4 — Orchestration captive dans `internal/service/service.go`** (AC: 1, 3, 4, 5)
  - [ ] 4.1 Ajouter une étape `captive-check` au tout début de `Connect()`, **avant** création TUN et activation firewall full
  - [ ] 4.2 Si `config.Tunnel.CaptivePortalDetection == false` → skip directement étape suivante
  - [ ] 4.3 Appeler `captive.Probe(ctx, client)` :
    - [ ] `NoPortal` → continue séquence Connect normale
    - [ ] `ProbeError`/timeout → log info, continue séquence Connect normale (fail-open sur erreur — le kill switch full gérera si vraiment pas de connectivité)
    - [ ] `PortalDetected` → activer mode `CAPTIVE` (voir 4.4)
  - [ ] 4.4 Mode captive activation : `firewall.Activate(nil, "", ModeCaptive)` puis émettre IPC event `captive-portal-detected{url, status}` ; démarrer goroutine `captiveWatcher()` avec ticker 15s
  - [ ] 4.5 `captiveWatcher()` : à chaque tick, re-probe ; si `NoPortal` → appeler `transitionFromCaptive()` ; si toujours portail → continuer. Stoppé via `context.Context` à `Disconnect()` ou clic utilisateur
  - [ ] 4.6 `transitionFromCaptive()` : cancel watcher → `firewall.Transition(ModeFull, relayIP, tunName)` → continuer séquence Connect standard (routing, tunnel) → émettre IPC `captive-portal-cleared`

- [ ] **Task 5 — IPC events + Handler UI dans `internal/ipchandler/` + `internal/ipc/`** (AC: 2, 3)
  - [ ] 5.1 Déclarer 2 nouveaux messages IPC server→client : `captive-portal-detected` (payload: `{probeURL, status, lanGateway}`) et `captive-portal-cleared`
  - [ ] 5.2 Ajouter méthode IPC client→server : `retryCaptiveCheck()` (pour bouton "Activer la protection")
  - [ ] 5.3 `retryCaptiveCheck` → déclenche immédiatement un re-probe dans le service (bypass le ticker 15s)

- [ ] **Task 6 — UI : bandeau captive + bouton retry + icône tray** (AC: 2)
  - [ ] 6.1 `frontend/` — composant bandeau captive (HTML+CSS+JS, s'affiche via websocket/SSE depuis `internal/ui/httpserver.go`)
  - [ ] 6.2 Bouton principal passe en mode `Activer la protection` → appel REST `/api/captive/retry` → IPC `retryCaptiveCheck`
  - [ ] 6.3 `internal/ui/tray.go` (ou équivalent fyne.io/systray) — ajouter icône `tray-captive.png` (orange) + setter `SetIconCaptive()` / `SetIconConnected()` / `SetIconDisconnected()`

- [ ] **Task 7 — Config TOML** (AC: 5)
  - [ ] 7.1 `internal/config/` — ajouter `[tunnel] captive_portal_detection = true` (default true)
  - [ ] 7.2 Ajouter `[tunnel] captive_probe_urls = ["http://captive.apple.com/hotspot-detect.html", "http://connectivitycheck.gstatic.com/generate_204"]` (override possible)
  - [ ] 7.3 Vérifier que HMAC integrity (cf. architecture `internal/config/integrity.go`) est recalculé quand ces champs changent

- [ ] **Task 8 — Tests d'intégration bout-en-bout** (AC: 1, 2, 3, 4)
  - [ ] 8.1 Test `captive_e2e_test.go` (Linux, build tag `integration`) : setup un `httptest.Server` qui répond 302 Location pendant N=3 secondes puis 204, vérifier que le service passe CAPTIVE → FULL automatiquement
  - [ ] 8.2 Test timeout : serveur qui ne répond jamais → service skip captive et passe direct en Connect
  - [ ] 8.3 Test config désactivée : `captive_portal_detection=false` → aucun probe émis (vérifier via compteur)

## Dev Notes

### Couverture code existant

**Aucun code existant** couvre cette story. Pas de dossier `internal/captive/` ni de logique probe HTTP RFC 7710 dans le code actuel. Recherche grep `captive|Captive|portal` dans `internal/` = 0 match. À créer de zéro.

### Dépendances fortes sur stories amont

Cette story **DÉPEND** de :
- **Story 2.6** (nftables Linux firewall) — doit fournir l'interface `Firewall` avec signature extensible pour `Mode`
- **Story 2.7** (WFP Windows firewall) — idem côté Windows
- **Story 2.4** (routage) — idéalement fournit déjà `DefaultGateway()` helper. Si absent, cette story l'ajoute dans `internal/routing/` (Task 3)

**⚠️ IMPORTANT pour le dev** : si 2.6/2.7 ne sont pas encore implémentées au moment d'implémenter 2.8, **NE PAS** créer un package `internal/firewall/` minimal en vase clos — remonter au SM pour s'assurer que la signature d'interface `Firewall` est définie de façon à supporter les 3 modes dès 2.6. Ajouter `Mode` en paramètre de `Activate` dès le départ plutôt que de refactorer plus tard.

### Architecture patterns to follow

- **Build tags par OS** — `internal/captive/probe.go` est OS-agnostique (HTTP pur), mais la détection gateway LAN dans `internal/routing/` utilise des build tags Linux/Windows. Pattern déjà appliqué dans `internal/dns/check_linux.go` / `check_windows.go` / `check_darwin.go` — suivre la même convention : un fichier par OS + un fichier `doc.go` partagé.
- **IPC events server→client** — chercher un exemple existant dans `internal/ipchandler/` (p.ex. state tunnel changes) pour le format d'event. Ne pas inventer un nouveau transport.
- **Atomicité nftables** — utiliser `nft -f -` avec `flush table inet levoile; add table inet levoile; …` dans la même transaction plutôt que deux appels séparés (évite une fenêtre de 0-trafic ou pire, de fuite).
- **Atomicité WFP** — `FwpmTransactionBegin0/Commit0` est essentiel pour éviter une fenêtre où aucun filter n'est actif pendant la transition.
- **Fail-open sur probe timeout** (AC4) — choix délibéré : on préfère rater un captive portal (utilisateur verra juste que le tunnel ne monte pas, peut cliquer Connect à nouveau) plutôt que de bloquer un démarrage légitime en absence de captive. Le kill switch full garantit qu'aucune fuite ne se produit entre temps.

### Fichiers source à créer / modifier

| Fichier | Action | Raison |
|---|---|---|
| `internal/captive/probe.go` | **Créer** | Logique de probe HTTP RFC 7710 |
| `internal/captive/probe_test.go` | **Créer** | Tests unitaires probe |
| `internal/captive/doc.go` | **Créer** | Doc package |
| `internal/firewall/firewall.go` | **Modifier** (attend 2.6) | Ajouter `Mode` enum + signature `Activate(relayIP, tunName, mode)` + `Transition(newMode, …)` |
| `internal/firewall/nftables_linux.go` | **Modifier** (attend 2.6) | Template `ruleset_captive.nft.tmpl` |
| `internal/firewall/wfp_windows.go` | **Modifier** (attend 2.7) | Filters gateway-only pour mode captive |
| `internal/routing/gateway_linux.go` | **Créer** | `DefaultGateway()` via `/proc/net/route` |
| `internal/routing/gateway_windows.go` | **Créer** | `DefaultGateway()` via `GetBestRoute2` |
| `internal/service/service.go` | **Modifier** | Orchestration captive-check dans `Connect()`, goroutine `captiveWatcher` |
| `internal/ipchandler/handler.go` | **Modifier** | Events `captive-portal-detected/cleared`, méthode `retryCaptiveCheck` |
| `internal/ui/httpserver.go` | **Modifier** | Endpoint REST `/api/captive/retry` |
| `frontend/index.html` + JS | **Modifier** | Bandeau captive + bouton retry + indicateur |
| `internal/config/config.go` | **Modifier** | Champs `captive_portal_detection`, `captive_probe_urls` |

### Sécurité / Zero-Leak pendant transition

- **Fenêtre de transition CAPTIVE → FULL** : la commande `Transition()` DOIT être atomique côté kernel. Jamais deux étapes : `Deactivate()` puis `Activate(Full)` (crée une fenêtre 0-règle = fuite totale). Utiliser flush+add dans la même transaction nftables ; `FwpmTransactionBegin/Commit` WFP.
- **Aucune requête HTTP probe ne doit passer par la TUN** : au moment du probe initial, la TUN n'existe pas encore (on est avant `Connect()`). Le probe utilise donc le stack réseau normal du système. Aucun risque de fuite car on n'a **pas encore** configuré les routes/firewall.
- **Pendant mode captive**, le firewall bloque tout sauf gateway → donc le **re-probe lui-même** passe par la gateway native. C'est voulu (on est en attente d'authentification Wi-Fi, pas encore protégé).
- Une fois en mode `Full`, aucun probe n'est plus émis (ticker arrêté). Le captive portal ne se re-détecte qu'après `Disconnect()` + nouveau `Connect()`.

### Testing standards

- Unit tests Go standard (`go test ./...`) — `httptest.Server` pour simuler les portails captifs
- Tests d'intégration Linux sous build tag `integration` (requiert `CAP_NET_ADMIN` — exécuter via `sudo -E go test -tags=integration`)
- Pas de tests WFP automatisés dans CI (nécessite VM Windows LocalSystem) — tests manuels documentés dans le runbook
- Couverture cible ≥ 80% sur `internal/captive/` (package nouveau, peu complexe)

### Project Structure Notes

- Package `internal/captive/` est **nouveau** et cohérent avec la liste architecture (~18 packages `internal/`). À ajouter au schéma d'architecture dans `_bmad-output/planning-artifacts/architecture.md` section "Composants architecturaux" après implémentation.
- La logique captive est **volontairement isolée** de `internal/firewall/` : le firewall ne connaît pas la notion de "captive", il connaît seulement un `Mode`. L'orchestration vit dans `internal/service/`.
- Ne pas réinventer un client HTTP — réutiliser `http.DefaultClient` avec timeout custom, ou mieux `crypto/tls` désactivé (probe HTTP plain volontairement, car la plupart des captive portals interceptent HTTP et laissent passer HTTPS sans redirect).

### References

- [architecture.md L31](../planning-artifacts/architecture.md#L31) — Capture L3 (FR5-8 révisés), firewall modes
- [architecture.md L237-239](../planning-artifacts/architecture.md#L237-L239) — Modèle gateway NAT + routage
- [architecture.md L326-331](../planning-artifacts/architecture.md#L326-L331) — Interface `Firewall` + `RouteManager` + build tags
- [epics.md L586-602](../planning-artifacts/epics.md#L586-L602) — Story 2.8 definition + BDD
- [prd.md L424-427](../planning-artifacts/prd.md#L424-L427) — FR8c captive portal spec
- RFC 7710 — Captive-Portal Identification using DHCP/RA (section 2 probe methodology)
- Apple captive probe URL convention : https://en.wikipedia.org/wiki/Captive_portal#Apple
- Android/Chromium convention : `/generate_204` response contract

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

- Story créée 2026-04-15 par `create-story` workflow. Contexte engine analysis complet.
- Dépendance forte sur stories 2.6/2.7 (firewall Linux/Windows) : le dev doit vérifier l'état de ces stories avant de commencer. Si pas implémentées, coordonner avec SM pour définir signature `Firewall.Activate(relayIP, tunName, Mode)` dès 2.6/2.7 afin d'éviter un refactor.
- Probe HTTP (non HTTPS) intentionnel — la plupart des portails captifs n'interceptent pas HTTPS.
- Fail-open sur timeout probe (AC4) : choix sécurité délibéré, documenté ci-dessus.

### Questions pour le PM / Architect

1. **URLs de probe override utilisateur** — faut-il permettre à l'utilisateur de configurer `captive_probe_urls` dans les Paramètres avancés, ou laisser en dur dans le code (moins de surface d'attaque — un attaquant local ne peut pas forger une URL qui retourne 204 pour tromper le client) ?
2. **Ticker re-probe 15s** — intervalle ajustable via config ? Valeur par défaut raisonnable mais certains portails demandent 30s+ de saisie.
3. **Évènement tray "orange captive"** — besoin d'un asset icône dédié. Qui le fournit (UX designer) ? À confirmer avant dev.
4. **Comportement si l'utilisateur `Disconnect` pendant mode captive** — firewall revient en `ModeOff` (trafic libre) ou reste verrouillé ? Hypothèse implémentation : `ModeOff` (cohérent avec disconnect normal).

### File List
