# Story 12.2: Validation Bout en Bout et Tests d'Intégration

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que l'ensemble des composants (service + UI tray/webview + extension navigateur + proxy + tunnel) fonctionne de maniere coherente et sans fuite,
Afin d'avoir confiance que ma protection est reelle et complete.

## Acceptance Criteria

**AC1 — Chaine complete : zero fuite DNS**
**Given** Le Voile est installe avec tous les composants (service, UI tray+webview, extension navigateur)
**When** l'utilisateur lance Le Voile et le tunnel se connecte
**Then** un test DNS leak (dnsleaktest.com ou equivalent programmatique) retourne zero fuite
**And** toutes les requetes DNS passent par le tunnel DoH (resolver systeme = 127.0.0.1)
**And** le kill switch est actif (requetes DNS hors tunnel bloquees)

**AC2 — Chaine complete : zero fuite IP**
**Given** Le Voile est connecte via un relais d'un pays selectionne
**When** l'utilisateur visite https://plateformeliberte.fr/test-protection.html
**Then** l'IP affichee sur la page correspond a l'IP du relais actif (pas l'IP reelle de l'utilisateur)
**And** l'IP affichee dans la fenetre webview correspond a l'IP vue par le site
**And** cette verification passe immediatement apres connexion ET apres un failover

**AC3 — Chaine complete : zero fuite WebRTC**
**Given** Le Voile est connecte et les politiques navigateur sont appliquees
**When** un test WebRTC est execute (browserleaks.com/webrtc ou equivalent)
**Then** aucun ICE candidate ne contient l'IP reelle ou l'IP LAN de l'utilisateur
**And** les politiques Chromium (registre) et Firefox (policies.json) sont verifiees comme appliquees

**AC4 — Failover avec synchronisation UI**
**Given** Le Voile est connecte via un pays selectionne (ex: Islande)
**When** le relais actif tombe (timeout, erreur 503, perte connexion)
**Then** le failover bascule vers un autre relais du MEME pays (< 5s)
**And** le kill switch DNS protege pendant la bascule (< 100ms)
**And** la fenetre webview (si ouverte) affiche "Reconnexion..." puis le nouveau relais
**And** le tray met a jour l'icone pendant la transition (V orange puis V vert)
**And** l'IP visible post-failover correspond au pays selectionne

**AC5 — Changement de pays avec mise a jour coherente de tous les composants**
**Given** Le Voile est connecte a l'Islande, la fenetre webview est ouverte
**When** l'utilisateur selectionne un nouveau pays (ex: France) via la fenetre webview
**Then** le tunnel se reconnecte via un relais du nouveau pays (< 5s)
**And** l'IP visible dans la fenetre webview se met a jour et correspond au pays France
**And** le tray reflete le nouveau pays dans son tooltip en < 2s
**And** l'extension navigateur continue de router via le proxy (pas d'interruption de routage)
**And** le pays prefere est sauvegarde dans la config TOML

**AC6 — Resilience post-shutdown : navigateur et DNS fonctionnels**
**Given** Le Voile etait connecte avec extension active et SysProxy configure
**When** l'utilisateur quitte Le Voile via le menu tray "Quitter"
**Then** le DNS systeme est restaure (resolver original fonctionnel)
**And** le SysProxy WinINET est desactive (registre Internet Settings restaure)
**And** le navigateur fonctionne normalement (l'extension passe en fallback direct, SysProxy desactive)
**And** aucun processus orphelin ne subsiste (service et UI tous arretes)

**AC7 — Recovery apres crash : restauration automatique**
**Given** Le Voile est connecte et un crash est simule (kill processus service)
**When** le service redemarre automatiquement (Windows SCM, < 10s)
**Then** `RecoverOrphanDNS()` restaure le resolver si necessaire
**And** `RecoverOrphanPolicies()` restaure les politiques navigateur si necessaire
**And** le tray se reconnecte au service et affiche l'etat correct
**And** la protection reprend sans intervention utilisateur

**AC8 — Proxy CONNECT resilient : pas de hang quand tunnel down**
**Given** Le Voile est connecte et le HTTP proxy CONNECT est actif (127.0.0.1:50113)
**When** le tunnel se deconnecte (relais down)
**Then** les requetes CONNECT en cours echouent proprement (pas de hang infini, timeout < 10s)
**And** le kill switch DNS bloque les resolutions
**And** l'extension detecte le proxy inactif et passe en fallback direct

**AC9 — Coherence de demarrage complet (cold start)**
**Given** la machine vient de demarrer (boot), le service demarre via SCM
**When** l'UI se lance (auto-start HKCU)
**Then** le tray se connecte au service via IPC et affiche l'etat complet des le premier `get_status`
**And** si le tunnel est encore en cours de connexion, le tray affiche l'icone orange (pas rouge)
**And** la fenetre webview (si ouverte) affiche le meme etat via le polling API /api/status
**And** l'extension est chargee et route via le proxy des que le service est pret

## Tasks / Subtasks

- [ ] **Task 1 : Revue et mise a jour des tests E2E existants pour l'architecture 2 processus** (AC: 1, 2, 3, 4, 5, 6, 7, 8, 9)
  - [ ] 1.1 Auditer tous les fichiers `e2e_test.go` existants (`internal/dns/`, `internal/httpproxy/`, `internal/ipc/`, `internal/registry/`, `internal/service/`, `internal/browser/`) — verifier qu'aucun ne reference Wails v2, `cmd/desktop/`, `cmd/tray/`, `cmd/portable/`, `internal/desktop/`, `internal/tray/`. Corriger toute reference obsolete vers la nouvelle architecture (`cmd/ui/`, `internal/ui/`).
  - [ ] 1.2 Dans `internal/service/e2e_recovery_test.go`, verifier que les tests de crash recovery (`TestE2E_CleanShutdown_*`, `TestE2E_CrashRecovery_*`) sont compatibles avec le nouveau modele 2 processus. Le shutdown sequence est : UI envoie Quit via IPC -> service shutdown (tunnel, DNS, browser policies, IPC en dernier) -> UI se ferme.
  - [ ] 1.3 Verifier que le build tag et la gate `E2E=1` sont presents sur TOUS les fichiers e2e. Pattern : `//go:build e2e` en premiere ligne + `if os.Getenv("E2E") != "1" { t.Skip("E2E=1 required") }` dans chaque test.
  - [ ] 1.4 Executer `E2E=1 go test -tags e2e -run TestE2E ./... -v -timeout 5m` et corriger tout echec lie a l'architecture revisee.

- [ ] **Task 2 : Tests E2E DNS proxy + Kill Switch** (AC: 1, 4)
  - [ ] 2.1 Verifier l'existence et le bon fonctionnement de `internal/dns/e2e_test.go` : `TestE2E_DNSProxyResolution`, `TestE2E_KillSwitchActivation`, `TestE2E_DNSRestoredAfterShutdown`. Ces tests existent deja (commit deb24df) — s'assurer qu'ils passent avec le code actuel.
  - [ ] 2.2 Verifier `TestE2E_KillSwitchActivation` : le kill switch doit activer en < 100ms et 0 resolution DNS ne doit passer apres la notification de perte tunnel (burst de 50 resolutions paralleles).
  - [ ] 2.3 Verifier `internal/dns/e2e_windows_test.go` : `TestE2E_DNSIPv6Resolver`, `TestE2E_DNSPort53Real` — tests Windows-only avec skip automatique si pas admin ou port 53 occupe.

- [ ] **Task 3 : Tests E2E Proxy CONNECT + Mock Relay** (AC: 2, 8)
  - [ ] 3.1 Verifier l'existence et le bon fonctionnement de `internal/httpproxy/e2e_test.go` : `TestE2E_IPCamouflage`, `TestE2E_ProxyCONNECT_TunnelDown`, `TestE2E_SessionTokenRefresh`. Ces tests existent deja (commit deb24df).
  - [ ] 3.2 S'assurer que le mock relay local retourne une IP fixe sur `/ip` et forward correctement le trafic CONNECT. Le proxy local doit utiliser le mock comme backend tunnel.
  - [ ] 3.3 Verifier que `TestE2E_ProxyCONNECT_TunnelDown` confirme un timeout < 10s (pas de hang infini) quand le relay est arrete.

- [ ] **Task 4 : Tests E2E IPC multi-client et coherence** (AC: 5, 9)
  - [ ] 4.1 Verifier `internal/ipc/e2e_test.go` : `TestE2E_IPCMultiClient`, `TestE2E_IPCStatusDuringReconnect`, `TestE2E_IPCPipeBroken`, `TestE2E_IPCCountryChange`, `TestE2E_IPCConcurrentCountryChange`. Verifier compatibilite avec le nouveau protocole IPC si des changements ont ete faits pour Epic 10.
  - [ ] 4.2 `TestE2E_IPCCountryChange` doit verifier que SelectCountry -> GetStatus retourne le nouveau pays et que l'IP visible correspond.
  - [ ] 4.3 `TestE2E_IPCConcurrentCountryChange` doit verifier la coherence quand deux changements de pays arrivent rapidement (le dernier gagne, pas de corruption d'etat).

- [ ] **Task 5 : Tests E2E Failover et Registry** (AC: 4)
  - [ ] 5.1 Verifier `internal/registry/e2e_test.go` : `TestE2E_FailoverSameCountry`, `TestE2E_FailoverIPConsistency`, `TestE2E_FailoverKillSwitchProtection`, `TestE2E_ReconnectInitiation`.
  - [ ] 5.2 `TestE2E_FailoverSameCountry` : demarrer 2+ mock relays pour le meme pays. Arreter le relay actif. Verifier que le failover bascule vers l'autre relay du meme pays en < 5s.
  - [ ] 5.3 `TestE2E_FailoverKillSwitchProtection` : pendant le failover, verifier qu'aucune resolution DNS ne passe en clair (kill switch actif pendant toute la bascule).

- [ ] **Task 6 : Tests E2E Browser Policies** (AC: 3, 6)
  - [ ] 6.1 Verifier `internal/browser/e2e_test.go` et `internal/browser/e2e_windows_test.go` : `TestE2E_ChromiumPoliciesApplied`, `TestE2E_WebRTCPoliciesApplied_Chromium`, `TestE2E_FirefoxPoliciesApplied`, `TestE2E_WebRTCPoliciesApplied_Firefox`.
  - [ ] 6.2 Verifier que les tests Windows verifient les cles registre Chromium (`HKLM\SOFTWARE\Policies\Google\Chrome`, `HKLM\SOFTWARE\Policies\Microsoft\Edge`) et que les tests Firefox verifient `policies.json`.
  - [ ] 6.3 `TestE2E_CleanShutdown_BrowserPoliciesRestored` : apres shutdown du service, verifier que les politiques navigateur sont supprimees/restaurees.

- [ ] **Task 7 : Checklist de validation manuelle bout en bout** (AC: 1-9)
  - [ ] 7.1 **Demarrage cold start** : reboot machine -> service demarre via SCM -> UI demarre via autostart HKCU -> tray V orange -> tunnel connecte -> tray V vert, fenetre webview affiche etat correct via polling /api/status.
  - [ ] 7.2 **Zero fuite DNS** : Le Voile connecte -> ouvrir https://dnsleaktest.com -> Extended test -> verifier que seuls les serveurs DNS du relais apparaissent (pas ceux du FAI).
  - [ ] 7.3 **Zero fuite IP** : Le Voile connecte -> ouvrir https://whatismyip.com -> verifier que l'IP correspond a l'IP affichee dans la fenetre webview. Repeter apres un failover.
  - [ ] 7.4 **Zero fuite WebRTC** : Le Voile connecte -> ouvrir https://browserleaks.com/webrtc -> verifier qu'aucun ICE candidate ne contient l'IP reelle ou l'IP LAN. Tester dans Chrome ET Firefox.
  - [ ] 7.5 **Changement de pays** : fenetre webview ouverte -> selectionner un nouveau pays dans la sidebar -> cliquer Connecter -> verifier IP changee dans la fenetre + tray mis a jour + whatismyip.com confirme.
  - [ ] 7.6 **Failover** : identifier le relais actif -> couper manuellement le relais (ou simuler un timeout) -> verifier : tray passe en V orange, fenetre affiche "Reconnexion...", bascule vers un autre relais < 5s, V vert restaure, IP verifiee.
  - [ ] 7.7 **Shutdown propre** : Le Voile connecte -> menu tray "Quitter" -> verifier : processus service et UI tous arretes (tasklist), DNS systeme restaure (`nslookup google.com` fonctionne), SysProxy WinINET desactive (Internet Settings restaure), navigateur fonctionne normalement.
  - [ ] 7.8 **Crash recovery** : Le Voile connecte -> `taskkill /F /IM levoile-service.exe` -> verifier : SCM redemarre le service < 10s, `RecoverOrphanDNS()` restaure si necessaire, tray se reconnecte, protection reprend.
  - [ ] 7.9 **Extension navigateur fallback** : Le Voile connecte -> quitter Le Voile -> verifier que l'extension detecte le proxy down et route en DIRECT (navigation fonctionne). Relancer Le Voile -> l'extension re-route via le proxy automatiquement.
  - [ ] 7.10 **Fermeture webview independante** : Le Voile connecte, fenetre webview ouverte -> fermer la fenetre (croix ou bouton minimiser) -> verifier : tray reste actif, service continue, protection maintenue. Ré-ouvrir via menu tray "Ouvrir" -> fenetre affiche l'etat actuel correctement.
  - [ ] 7.11 **Proxy CONNECT resilient** : Le Voile connecte -> lancer un telechargement HTTP -> couper le tunnel (kill service) -> verifier que le telechargement echoue proprement (pas de hang infini du navigateur).
  - [ ] 7.12 **WinINET recovery** : Le Voile connecte -> `taskkill /F /IM levoile-ui.exe` -> relancer l'UI -> verifier que le WinINET proxy est synchronise avec l'etat du service (restore orpheline si necessaire).
  - [ ] 7.13 **Extension bypass gros fichiers** : Le Voile connecte -> telecharger un fichier > 50 Mo (ex: ISO Linux) -> verifier dans la console extension que le bypass direct s'active apres detection Content-Length > 50 Mo.
  - [ ] 7.14 **Blocklist DNS** : Le Voile connecte, blocklist activee -> `nslookup` un domaine connu comme bloque (ex: `ads.example.com` dans la liste StevenBlack) -> verifier NXDOMAIN. Desactiver la blocklist via la fenetre -> meme requete -> resolution reussie.

- [ ] **Task 8 : Documentation validation-e2e.md** (AC: 1-9)
  - [ ] 8.1 Mettre a jour `docs/validation-e2e.md` pour refleter l'architecture 2 processus (service + UI unique). Supprimer toute reference a Wails v2, `cmd/desktop/`, `cmd/tray/`, `cmd/portable/`.
  - [ ] 8.2 Mettre a jour la table de couverture AC <-> tests E2E avec les noms de tests actuels.
  - [ ] 8.3 Ajouter la section checklist manuelle (Tasks 7.1-7.14) comme reference.

## Dev Notes

### Architecture 2 Processus (Post-Epic 10)

```
[Windows SCM] ──start──> [levoile-service.exe (service)]
                              |
                              +-- IPC server (named pipe \\.\pipe\levoile)
                              +-- Tunnel QUIC/HTTPS
                              +-- DNS proxy + kill switch + watchdog
                              +-- HTTP proxy CONNECT (127.0.0.1:50113)
                              +-- Browser policies (HKLM registre)
                              +-- Leak checker + STUN + blocklist

[levoile-ui.exe] ──IPC──> [service]
    |
    +-- fyne.io/systray (thread principal)
    +-- webview/webview (fenetre 420x540, ouverte/fermee a la demande)
    +-- Serveur HTTP local (127.0.0.1:{port}) : assets + API REST JSON
    +-- WinINET proxy (registre)
    +-- Singleton mutex Windows
```

**CRITIQUE : 2 processus, pas 3.** L'ancienne architecture Wails v2 avec `cmd/desktop/`, `cmd/tray/`, `cmd/portable/` est remplacee par `cmd/ui/` (binaire unique systray + webview + HTTP local). Le mode portable est supprime.

### Propriete des ressources systeme (pas de double restauration)

| Ressource | Proprietaire | Restauration |
|-----------|-------------|--------------|
| DNS systeme | service | `RestoreResolver()` dans `shutdown()` |
| Politiques navigateur | service | `RestorePolicies()` dans `shutdown()` |
| SysProxy WinINET | UI (tray) | `restoreWinINETProxy()` dans shutdown UI |
| Tunnel QUIC | service | `disconnect()` dans `shutdown()` |
| Kill switch DNS | service | `deactivate()` dans `shutdown()` |

### Sequence shutdown (Story 12.1 confirmee)

```
UI cote :
1. shutdownInProgress = true (atomic)
2. restoreWinINETProxy() + supprimer proxy-original.json
3. ActionQuit via IPC (timeout 10s)
4. Detruire fenetre webview si ouverte
5. systray.Quit()

Service cote :
1.  Stop leak scheduler
2.  Stop discoverer
3.  Stop blocklist manager
4.  Stop reconnector
5.  Stop watchdog
6.  Stop STUN interceptor
7.  Stop HTTP proxy (5s drain)
8.  Deactivate kill switch
9.  Restore browser policies    <-- AVANT IPC close
10. Restore DNS resolver         <-- AVANT IPC close
11. Verify DNS via watchdog
12. Stop DNS proxy
13. Restart Windows Dnscache
14. Close state channel + disconnect tunnel
15. Stop IPC server              <-- EN DERNIER
16. Disable OnFailure (sc failure)
17. os.Exit(0)
```

### Tests E2E existants (commit deb24df)

Les tests E2E suivants existent deja et testent les sous-systemes internes (independants de l'architecture UI) :

| Fichier | Tests | AC |
|---------|-------|-----|
| `internal/dns/e2e_test.go` | DNSProxyResolution, KillSwitchActivation, DNSRestoredAfterShutdown | AC1, AC4, AC6 |
| `internal/dns/e2e_windows_test.go` | DNSIPv6Resolver, DNSPort53Real | AC1 |
| `internal/httpproxy/e2e_test.go` | IPCamouflage, ProxyCONNECT_TunnelDown, SessionTokenRefresh | AC2, AC8 |
| `internal/ipc/e2e_test.go` | IPCMultiClient, IPCStatusDuringReconnect, IPCPipeBroken, IPCCountryChange, IPCConcurrentCountryChange | AC5, AC9 |
| `internal/registry/e2e_test.go` | FailoverSameCountry, FailoverIPConsistency, FailoverKillSwitchProtection, ReconnectInitiation | AC4 |
| `internal/service/e2e_recovery_test.go` | CleanShutdown_DNSRestored, CleanShutdown_BrowserPoliciesRestored, CrashRecovery_OrphanDNS, CrashRecovery_OrphanPolicies, WinINETRecovery | AC6, AC7 |
| `internal/browser/e2e_test.go` | FirefoxPoliciesApplied, WebRTCPoliciesApplied_Firefox | AC3 |
| `internal/browser/e2e_windows_test.go` | ChromiumPoliciesApplied, WebRTCPoliciesApplied_Chromium | AC3 |

**Action principale : verifier que ces tests passent toujours avec le code actuel et corriger les references obsoletes (Wails, 3 processus).**

### Commandes de test

```bash
# Tous les tests E2E
E2E=1 go test -tags e2e -run TestE2E ./... -v -timeout 5m

# Par sous-systeme
E2E=1 go test -tags e2e -run TestE2E ./internal/dns/ -v
E2E=1 go test -tags e2e -run TestE2E ./internal/httpproxy/ -v
E2E=1 go test -tags e2e -run TestE2E ./internal/ipc/ -v
E2E=1 go test -tags e2e -run TestE2E ./internal/registry/ -v
E2E=1 go test -tags e2e -run TestE2E ./internal/service/ -v
E2E=1 go test -tags e2e -run TestE2E ./internal/browser/ -v

# Tests unitaires standard (regression)
go test ./... -timeout 3m
```

### Contraintes architecture a respecter

- **Zero-log :** Aucun `log.Println`, `fmt.Println` dans le code de production. Erreurs propagees via retours d'erreur et IPC.
- **Error wrapping :** `fmt.Errorf("package: context: %w", err)` — prefixe package systematique.
- **Build tags :** Code Windows-specific (`//go:build windows`), tests E2E (`//go:build e2e`).
- **Go standard `testing` :** Pas de framework tiers. Table-driven tests quand > 2 cas. `t.Helper()`, `t.Cleanup()`.
- **Tests co-localises :** `e2e_test.go` a cote du code source, pas dans un dossier separe.
- **Gate E2E :** Chaque test E2E DOIT verifier `os.Getenv("E2E") == "1"` et `t.Skip()` sinon.
- **Skip conditionnel :** Tests Windows-only skippent sur Linux/macOS. Tests port 53 ou admin skippent si non disponibles.
- **Nommage tests :** `TestE2E_NomDescriptif` pour les E2E, `TestPackage_Methode_Scenario` pour les unitaires.

### Bibliotheques et frameworks

| Composant | Bibliotheque | Version | Usage |
|-----------|-------------|---------|-------|
| Service OS | `kardianos/service` | v1.2.4 | SCM Windows, restart auto |
| System tray | `fyne.io/systray` | v1.12.0 | Icone tray, menu contextuel |
| Fenetre desktop | `webview/webview` | nouvelle | WebView2 Windows, ouverte/fermee a la demande |
| IPC | `internal/ipc` | custom | Named pipes Windows, JSON ligne par ligne |
| Tunnel | `quic-go` | v0.59.0 | HTTP/3 + TLS 1.3 |
| Crypto | `crypto/ed25519` | stdlib | Session tokens, cert pinning, registre signe |
| Config | `BurntSushi/toml` | v1.5.0 | Configuration TOML |
| HTTP proxy | `internal/httpproxy` | custom | CONNECT proxy 127.0.0.1:50113 |
| Extension | JS WebExtension | Chrome MV3 / Firefox MV2 | Routage intelligent + bypass gros fichiers |

### Patterns de test E2E a respecter

**Mock relay local :** `httptest.Server` qui expose `/ip` (IP fixe), `/dns-query` (DoH mock), `/connect` (CONNECT proxy), `/verify` (session token). Reutiliser le pattern de `internal/relay/server_test.go`.

**DNS proxy sur port ephemere :** Jamais tester sur port 53 par defaut — utiliser un port libre. Le test `TestE2E_DNSPort53Real` est conditionnel (admin + port libre).

**Cleanup obligatoire :** `t.Cleanup()` pour restaurer le resolver DNS, les politiques navigateur, le WinINET proxy. Ne JAMAIS laisser l'etat systeme modifie apres un test.

**Parallelisme :** Les tests E2E qui modifient l'etat systeme (DNS, registre, politiques) ne DOIVENT PAS etre paralleles. Les tests sur port ephemere ou avec mock local PEUVENT etre paralleles.

### Intelligence Story 12.1 (story precedente)

**Lecons critiques :**
- `os.Exit(0)` n'execute PAS les defers — appeler les restaurations EXPLICITEMENT dans `shutdown()`
- IPC server arrete EN DERNIER (pas en premier) pour que l'UI recoit la confirmation
- `shutdownInProgress` flag empecheAJOUTER `handleIPCError()` de lancer la recovery orpheline pendant un quit intentionnel
- `sync.Once` dans `Stop()` pour idempotence
- Singleton mutex Windows (`Global\LeVoileTray`) pour empecher instances multiples
- `proxy-original.json` DPAPI-encrypted pour WinINET recovery
- "IP en detection..." quand IP vide (pas "deconnecte")
- `httpProxySeq` reset a 0 dans `handleIPCError` pour forcer sync WinINET au reconnect

**Fichiers modifies par 12.1 :**
- `internal/service/service.go` — reordonnement shutdown, sync.Once
- `internal/ui/ui.go` (anciennement `internal/tray/tray.go`) — shutdownInProgress, handleIPCError guard
- `cmd/ui/main.go` (anciennement `cmd/tray/main.go`) — singleton mutex

### Intelligence Git — Commits recents

```
b10febd chore: add manifest v2 backup for browser extension
94abe13 fix(extension): auto-fallback to DIRECT when proxy is down
d5a6025 fix(installer): remove Firefox extension and browser policies on uninstall
1640da5 fix: desktop shortcuts, tray icon, clean shutdown, remove dead code
340b6f6 fix: adversarial code review fixes, installer proxy cleanup, and full shutdown
0561c38 feat(httpproxy): add per-domain volume bypass in local proxy
deb24df test: add E2E integration tests and adversarial review fixes (story 12.2)
```

**Observations cles :**
- `94abe13` : L'extension auto-fallback DIRECT quand proxy down — directement teste par AC8 / Task 7.9
- `deb24df` : Les tests E2E ont deja ete ecrits — cette story valide et met a jour ces tests
- `1640da5` : Clean shutdown deja itere — cette story valide que ca fonctionne bout en bout

### Project Structure Notes

```
A VERIFIER / METTRE A JOUR :
internal/dns/e2e_test.go                # Existant — verifier references architecture
internal/dns/e2e_windows_test.go        # Existant — verifier references architecture
internal/httpproxy/e2e_test.go          # Existant — verifier references architecture
internal/ipc/e2e_test.go               # Existant — verifier references architecture
internal/registry/e2e_test.go           # Existant — verifier references architecture
internal/service/e2e_recovery_test.go   # Existant — verifier references architecture
internal/browser/e2e_test.go            # Existant — verifier references architecture
internal/browser/e2e_windows_test.go    # Existant — verifier references architecture
docs/validation-e2e.md                  # Existant — mettre a jour pour 2 processus

NON MODIFIE :
internal/dns/                           # Sous-systeme — pas de changement
internal/httpproxy/                     # Sous-systeme — pas de changement
internal/ipc/                           # Sous-systeme — pas de changement
internal/registry/                      # Sous-systeme — pas de changement
internal/service/                       # Sous-systeme — pas de changement
internal/browser/                       # Sous-systeme — pas de changement
frontend/                               # Pas de changement (polling fetch/api)
extension/                              # Pas de changement
```

**Cette story est principalement une story de VALIDATION et VERIFICATION — pas de nouvelle fonctionnalite.** L'essentiel du travail est : (1) verifier que les tests E2E existants passent, (2) corriger les references obsoletes, (3) executer la checklist manuelle complete.

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 12, Story 12.2, AC en BDD]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — Architecture 2 processus, webview/webview + fyne.io/systray, IPC named pipe]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — Testing Framework : Go standard `testing`, tests co-localises, e2e_test.go, build tags]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — Data flows : DNS, CONNECT, failover, extension, shutdown]
- [Source: `_bmad-output/planning-artifacts/ux-design-specification.md` — Etats visuels (V vert/orange/rouge), feedback patterns, gestion d'erreur]
- [Source: `_bmad-output/implementation-artifacts/12-1-shutdown-propre-et-independance-service-ui.md` — Lecons shutdown, IPC order, sync.Once, singleton]
- [Source: `docs/validation-e2e.md` — Guide E2E existant, commandes, prerequisites, couverture AC]
- [Source: git log — deb24df test: add E2E integration tests (tests existants)]
- [Source: sprint-status.yaml — 2026-04-08: Architecture revisee, Epics 10/12 reecrites]

### Couverture AC

| AC | Couvert par |
|----|------------|
| AC1 (zero fuite DNS) | Task 2 (E2E DNS), Task 7.2 (manuel) |
| AC2 (zero fuite IP) | Task 3 (E2E proxy), Task 7.3 (manuel) |
| AC3 (zero fuite WebRTC) | Task 6 (E2E browser), Task 7.4 (manuel) |
| AC4 (failover + UI sync) | Task 5 (E2E registry), Task 7.6 (manuel) |
| AC5 (changement pays) | Task 4 (E2E IPC), Task 7.5 (manuel) |
| AC6 (post-shutdown) | Task 6 (E2E browser), Task 7.7 (manuel) |
| AC7 (crash recovery) | Task 1.2 (E2E service), Task 7.8 (manuel) |
| AC8 (CONNECT resilient) | Task 3 (E2E proxy), Task 7.11 (manuel) |
| AC9 (cold start) | Task 4 (E2E IPC), Task 7.1 (manuel) |

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
