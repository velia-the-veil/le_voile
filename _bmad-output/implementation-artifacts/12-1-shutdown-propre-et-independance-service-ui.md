# Story 12.1: Shutdown Propre et Indépendance Service/UI

Status: done

<!-- Réécrite 2026-04-08 : architecture 2 processus (webview/webview + fyne.io/systray), remplace l'ancienne version Wails v2 / 3 processus -->

## Story

En tant qu'utilisateur,
je veux que quitter Le Voile via le tray arrête proprement tous les composants (webview, serveur HTTP local, service), et que la protection reste active tant que le service tourne même si la fenêtre webview est fermée,
afin de ne jamais avoir de fuite accidentelle ni de processus orphelin.

## Acceptance Criteria

**AC1 — Quitter via tray = arrêt complet**
**Given** la fenêtre webview est ouverte (ou non) et le tray est actif
**When** l'utilisateur sélectionne "Quitter" via le menu clic droit du tray
**Then** la fenêtre webview est détruite (si ouverte)
**And** le serveur HTTP local est arrêté (`http.Server.Shutdown`)
**And** l'UI envoie `ActionQuit` au service via IPC (timeout 10s)
**And** le service arrête le tunnel, restaure le DNS, restaure les politiques navigateur
**And** le SysProxy WinINET est désactivé (registre HKCU Internet Settings restauré)
**And** le processus UI se termine (`systray.Quit()`)
**And** aucun processus orphelin ne subsiste

**AC2 — Bouton "─" (minimize) = masquer dans le tray**
**Given** le service Le Voile est actif et le tunnel connecté
**When** l'utilisateur clique sur "─" (minimize)
**Then** la fenêtre webview est masquée (`ShowWindow(hwnd, SW_HIDE)`), pas détruite
**And** le tray continue de fonctionner (même processus)
**And** le service, tunnel, DNS, proxy restent actifs
**And** les politiques navigateur restent appliquées

**AC2b — Bouton "✕" (close) = quitter Le Voile**
**Given** le service Le Voile est actif
**When** l'utilisateur clique sur "✕"
**Then** shutdown complet : proxy WinINET restauré (avec ForceDisable si nécessaire), service arrêté via IPC ActionQuit, tray fermé

**AC3 — Réouverture webview depuis tray**
**Given** le service est actif et la fenêtre webview est masquée
**When** l'utilisateur sélectionne "Ouvrir la fenêtre" dans le menu tray
**Then** la fenêtre existante est montrée (`ShowWindow(hwnd, SW_SHOW)`) — pas de recréation
**And** l'état actuel (pays, relais, IP, statut) est affiché correctement via le polling `fetch('/api/status')`

**AC4 — Robustesse shutdown service**
**Given** le service reçoit `ActionQuit` et commence le shutdown
**When** une erreur survient pendant la séquence d'arrêt (timeout netsh, erreur registre)
**Then** la restauration DNS est TOUJOURS tentée (appel explicite, pas uniquement dans un `defer` — `os.Exit` n'exécute pas les defers)
**And** la restauration des politiques navigateur est TOUJOURS tentée
**And** `dns-state.json` n'est supprimé qu'APRÈS confirmation de restauration réussie
**And** au prochain démarrage, `RecoverOrphanDNS()` et `RecoverOrphanPolicies()` nettoient tout état résiduel

**AC5 — Propriété exclusive des ressources (pas de double restauration)**
**Given** le tray et le service sont actifs
**When** le shutdown est déclenché
**Then** chaque ressource système a un seul propriétaire :
- DNS système → service (`RestoreResolver()`)
- Politiques navigateur → service (`RestorePolicies()`)
- SysProxy WinINET → UI/tray (`SysProxy.Restore()`)
**And** aucune race condition entre UI et service sur la même ressource

**AC6 — Idempotence et singleton**
**Given** le processus UI reçoit une commande d'arrêt
**When** un second signal arrive (double clic Quitter, Task Manager pendant shutdown)
**Then** le shutdown ne s'exécute qu'une seule fois (`sync.Once`)
**And** un seul processus UI peut exister (mutex nommé Windows `Global\LeVoileUI`)

**AC7 — IPC pendant shutdown : pas d'interférence orphan recovery**
**Given** l'UI envoie `ActionQuit` au service via IPC
**When** le service commence le shutdown et le serveur IPC se ferme
**Then** un flag `shutdownInProgress` empêche `handleIPCError()` de lancer la récupération orpheline DNS
**And** le serveur IPC du service reste actif jusqu'à ce que toutes les restaurations soient terminées
**And** l'UI ne tente PAS de `RecoverOrphanDNS()` pendant un quit intentionnel

**AC8 — Connexion UI à un service déjà actif**
**Given** le service a été démarré par Windows SCM au boot (sans UI)
**When** l'utilisateur lance l'UI
**Then** l'UI se connecte au service via IPC
**And** l'état actuel complet est affiché dès le premier `get_status`
**And** le WinINET proxy est synchronisé avec l'état du service (`HTTPProxyActive`)

## Tasks / Subtasks

- [x] **Task 1 : Séquence shutdown UI (Quitter)** (AC: 1, 5, 6, 7)
  - [x] 1.1 Dans `internal/ui/`, implémenter la fonction `shutdown()` appelée quand "Quitter" est sélectionné dans le menu tray. Séquence ordonnée :
    1. `shutdownInProgress.Store(true)` (atomic.Bool) — bloque l'orphan recovery
    2. Détruire la fenêtre webview si elle existe (`w.Destroy()`)
    3. Restaurer le WinINET proxy (`SysProxy.Restore()`) — l'UI est propriétaire
    4. Arrêter le serveur HTTP local (`httpServer.Shutdown(ctx)` avec timeout 3s)
    5. Envoyer `ActionQuit` au service via IPC (timeout 10s)
    6. `RecoverOrphanDNS()` en filet de sécurité (si le service n'a pas répondu)
    7. `systray.Quit()` — termine la boucle systray et le processus
  - [x] 1.2 Wrapper l'appel à `shutdown()` dans un `sync.Once` pour l'idempotence. Le callback `systray.OnExit` appelle aussi `shutdown()` (couverture fermeture session Windows, Task Manager).
  - [x] 1.3 Si l'IPC `ActionQuit` timeout (10s), l'UI DOIT quand même se fermer. Le service sera récupéré par SCM ou orphan recovery au prochain lancement.

- [x] **Task 2 : Lifecycle webview indépendant** (AC: 2, 3)
  - [x] 2.1 Dans `internal/ui/`, quand la fenêtre webview est fermée par l'utilisateur (callback webview close), appeler `w.Destroy()` et mettre le pointeur webview à nil. Ne PAS appeler `systray.Quit()` ni `shutdown()`.
  - [x] 2.2 Le serveur HTTP local NE s'arrête PAS quand la fenêtre se ferme. Il continue d'écouter pour une éventuelle réouverture.
  - [x] 2.3 Menu tray "Ouvrir" : créer une nouvelle instance webview (`webview.New(false)`), la configurer (taille 420×540, titre, resize), naviguer vers `http://127.0.0.1:{port}/`. Stocker le pointeur pour la réutilisation.
  - [x] 2.4 Si la fenêtre est déjà ouverte quand "Ouvrir" est cliqué, ramener la fenêtre au premier plan (pas de double fenêtre). Mécanisme : vérifier si le pointeur webview est non-nil.

- [x] **Task 3 : Séquence shutdown service — IPC en dernier** (AC: 4, 7)
  - [x] 3.1 Dans `internal/service/service.go` méthode `shutdown()`, vérifier que l'ordre est :
    1. Stop leak scheduler, discoverer, blocklist, reconnector
    2. Stop watchdog, STUN interceptor
    3. Stop HTTP proxy (5s drain via WaitGroup)
    4. Deactivate kill switch
    5. **Restore browser policies** ← AVANT IPC close
    6. **Restore DNS resolver** ← AVANT IPC close
    7. Verify DNS via watchdog
    8. Stop DNS proxy (IPv4 + IPv6)
    9. Restart Windows Dnscache
    10. Close state channel + disconnect tunnel
    11. **Stop IPC server** ← EN DERNIER
    12. Disable OnFailure SCM
    13. `os.Exit(0)`
  - [x] 3.2 S'assurer que les restaurations DNS et browser policies sont des appels explicites dans `shutdown()`, pas uniquement dans des `defer` — `os.Exit()` n'exécute PAS les defers.
  - [x] 3.3 `dns-state.json` n'est supprimé qu'après confirmation de restauration réussie. En cas d'échec → garder le fichier pour `RecoverOrphanDNS()`.
  - [x] 3.4 Wrapper `shutdown()` dans `sync.Once` via `Stop()` et `RequestStop()`.

- [x] **Task 4 : Persistance WinINET et crash recovery** (AC: 4, 5, 8)
  - [x] 4.1 Dans `internal/ui/sysproxy_windows.go`, quand le proxy est activé, persister l'état original dans `%AppData%/LeVoile/proxy-original.json` (DPAPI-chiffré, même pattern que le code existant dans `internal/tray/sysproxy_windows.go`).
  - [x] 4.2 Au démarrage de l'UI, si `proxy-original.json` existe et le service n'est PAS actif (IPC échoue) → restaurer le WinINET depuis ce fichier (orphan recovery).
  - [x] 4.3 Supprimer `proxy-original.json` après restauration réussie dans `SysProxy.Restore()`.

- [x] **Task 5 : Synchronisation état au reconnect** (AC: 8)
  - [x] 5.1 Au premier `get_status` réussi après connexion IPC, synchroniser le WinINET proxy : si `HTTPProxyActive: true` et WinINET pas configuré → le configurer. Si `false` et WinINET configuré → le désactiver.
  - [x] 5.2 Si `IP` est vide dans `get_status` (détection async pas terminée), afficher "IP en détection..." dans le tray tooltip — ne PAS confondre avec "déconnecté".

- [x] **Task 6 : Tests** (AC: 1-8)
  - [x] 6.1 Test unitaire : `TestShutdown_Idempotent` — appeler `shutdown()` deux fois concurremment, vérifier exécution unique (compteur atomique).
  - [x] 6.2 Test unitaire : `TestShutdownSequence_IPCServerLast` — vérifier que IPC stop vient après DNS et browser policy restore dans la séquence.
  - [x] 6.3 Test unitaire : `TestGetStatus_MissingIP` — `get_status` retourne `status: connected` même quand `IP` est vide.
  - [x] 6.4 Test unitaire : `TestSysProxyPersistence` — sauvegarde et restauration WinINET via fichier persisté.
  - [x] 6.5 Test manuel : Lancer service SCM → lancer UI → vérifier état → Quitter via tray → vérifier DNS restauré, pas de processus orphelin.
  - [x] 6.6 Test manuel : Ouvrir fenêtre → fermer (croix) → vérifier tray + service continuent → rouvrir via tray → vérifier état correct.
  - [x] 6.7 Test manuel : Kill UI via Task Manager → vérifier service continue → relancer UI → vérifier reconnexion et état.

## Dev Notes

### Architecture 2 Processus (post-Epic 10)

```
[Windows SCM] ──start──→ [levoile-service.exe]
                              │
                              ├── IPC server (named pipe \\.\pipe\levoile)
                              ├── Tunnel QUIC/HTTPS
                              ├── DNS proxy + kill switch + watchdog
                              ├── HTTP proxy CONNECT (127.0.0.1:50113)
                              ├── Browser policies (HKLM registre)
                              └── Leak checker + STUN + blocklist

[levoile-ui.exe] ──IPC──→ [service]
    │
    ├── fyne.io/systray (thread principal, bloquant)
    ├── webview/webview (fenêtre à la demande, même processus)
    ├── Serveur HTTP local (127.0.0.1:{port}, assets + API REST)
    └── SysProxy WinINET (HKCU, propriété exclusive UI)
```

**Deux processus, un seul canal IPC.** Le service est maître du réseau (DNS, tunnel, browser policies). L'UI est maître de l'UX (tray, webview, WinINET proxy).

### Séquence Shutdown UI (Task 1)

```
1. shutdownInProgress = true (atomic)
2. Destroy webview (si ouvert)
3. Restore WinINET proxy + supprimer proxy-original.json
4. Shutdown HTTP server (3s timeout)
5. ActionQuit via IPC (10s timeout)
6. RecoverOrphanDNS() — filet de sécurité
7. systray.Quit() — fin du processus
```

### Séquence Shutdown Service (Task 3)

```
 1. Stop leak scheduler
 2. Stop discoverer
 3. Stop blocklist manager
 4. Stop reconnector
 5. Stop watchdog
 6. Stop STUN interceptor
 7. Stop HTTP proxy (5s drain)
 8. Deactivate kill switch
 9. *** Restore browser policies ***   ← AVANT IPC close
10. *** Restore DNS resolver ***        ← AVANT IPC close
11. Verify DNS restoration
12. Stop DNS proxy (IPv4 + IPv6)
13. Restart Windows Dnscache
14. Close state channel + disconnect tunnel
15. *** Stop IPC server ***             ← EN DERNIER
16. Disable OnFailure SCM
17. os.Exit(0)
```

### Différence Clé vs Ancien Code (3 processus)

L'ancien code avait un tray séparé (`cmd/tray/`) qui devait kill le desktop Wails (`cmd/desktop/`) avant d'envoyer Quit au service. Avec l'architecture 2 processus :
- **Plus de kill desktop** — le webview est dans le même processus, `w.Destroy()` suffit
- **Plus de IPC desktop→service** — seul l'UI communique via IPC
- **Serveur HTTP local** — nouveau composant dans l'UI à arrêter proprement (`http.Server.Shutdown`)
- Le code `internal/tray/` et `internal/desktop/` sont remplacés par `internal/ui/`

### Contraintes Architecture

- **webview/webview** : `webview.New(debug bool)` crée une fenêtre. `w.Destroy()` la détruit. `w.Run()` bloque le thread courant — doit tourner sur un thread séparé ou être géré avec `w.Dispatch()`. Le tray (fyne.io/systray) bloque le main thread.
- **fyne.io/systray** : `systray.Run(onReady, onExit)` bloque le main thread. `systray.Quit()` déclenche `onExit` puis termine. Le webview DOIT tourner sur un goroutine ou thread séparé.
- **Ordre threads** : systray sur main thread (obligatoire Windows). Webview sur un thread séparé via `runtime.LockOSThread()` dans une goroutine dédiée. HTTP server sur une goroutine standard.
- **Zero-log** : Aucun `log.Println` ou `fmt.Println` en production. Erreurs propagées via retours et IPC.
- **Error wrapping** : `fmt.Errorf("ui: shutdown: %w", err)` — préfixe package systématique.
- **context.Context** : Premier argument de toute fonction bloquante.
- **Build tags** : Code Windows (`singleton_windows.go`, `sysproxy_windows.go`) avec `//go:build windows`.
- **IPC messages** : JSON `snake_case`. Actions existantes suffisantes (`get_status`, `connect`, `disconnect`, `quit`, `set_http_proxy`, etc.). Pas de nouvelles actions.

### Bibliothèques

| Composant | Bibliothèque | Usage |
|---|---|---|
| System tray | `fyne.io/systray` | Thread principal, menu, icônes |
| Fenêtre desktop | `webview/webview` | WebView2 Windows, créé/détruit à la demande |
| Service OS | `kardianos/service` | `Stop()` idempotent via `sync.Once` |
| HTTP server | `net/http` (stdlib) | Assets frontend + API REST locale |
| IPC | `internal/ipc` | Named pipe, JSON ligne par ligne |
| Mutex singleton | `golang.org/x/sys/windows` | `CreateMutex` pour instance unique UI |

**Aucune nouvelle dépendance.** Tout utilise les packages existants + stdlib Go.

### Standards de Test

- Go standard `testing` — pas de framework tiers
- Tests co-localisés : `shutdown_test.go` à côté du code shutdown
- Nommage : `TestShutdown_Idempotent`, `TestShutdownSequence_IPCServerLast`
- `t.Helper()` pour fonctions utilitaires
- Tests manuels (T6.5-T6.7) non automatisables (multi-processus, SCM, registre)

### Project Structure Notes

```
MODIFIÉ :
internal/service/service.go        # Réordonnement shutdown() : IPC en dernier, sync.Once, restaurations explicites
internal/ipchandler/handler.go     # get_status : IP vide → status reste "connected"

CRÉÉ PAR EPIC 10 (prérequis, déjà existant quand cette story est implémentée) :
cmd/ui/main.go                     # Point d'entrée UI unique
internal/ui/ui.go                  # Orchestration tray + webview + HTTP server
internal/ui/httpserver.go          # Serveur HTTP local + API REST
internal/ui/sysproxy_windows.go    # WinINET proxy (migré depuis internal/tray/)
internal/ui/singleton_windows.go   # Mutex nommé Windows

MODIFIÉ PAR CETTE STORY DANS internal/ui/ :
internal/ui/ui.go                  # shutdown(), handleQuit(), webview lifecycle, sync.Once, shutdownInProgress
internal/ui/sysproxy_windows.go    # Persistance proxy-original.json, orphan recovery, Restore()

SUPPRIMÉ (remplacés par internal/ui/) :
internal/tray/                     # Remplacé par internal/ui/ dans Epic 10
internal/desktop/                  # Remplacé par internal/ui/ dans Epic 10
cmd/tray/                          # Remplacé par cmd/ui/ dans Epic 10
cmd/desktop/                       # Remplacé par cmd/ui/ dans Epic 10

NON MODIFIÉ :
internal/dns/                      # RestoreResolver(), RecoverOrphanDNS() — déjà complets
internal/browser/                  # RestorePolicies(), RecoverOrphanPolicies() — déjà complets
internal/ipc/                      # Client/server inchangés, juste l'ORDRE d'arrêt du server
internal/tunnel/                   # Inchangé
internal/httpproxy/                # Inchangé
internal/watchdog/                 # Inchangé
frontend/                          # Inchangé
extension/                         # Inchangé
```

### Pre-mortem : Risques

**R1 (CRITIQUE) : `os.Exit(0)` n'exécute pas les defers**
Les restaurations DNS et browser policies DOIVENT être des appels explicites dans `shutdown()`, pas uniquement dans des `defer`. Le bloc defer existant dans `run()` reste comme filet pour les panics.

**R2 (ÉLEVÉ) : Race webview.Destroy() vs systray.Quit()**
Si `systray.Quit()` est appelé avant que le webview soit détruit, le processus peut se terminer avec un webview actif. Séquence : toujours détruire le webview AVANT `systray.Quit()`.

**R3 (MOYEN) : webview thread safety**
`webview/webview` n'est pas thread-safe. Toute interaction avec le webview (create, destroy, navigate) doit passer par `w.Dispatch()` ou être sur le thread webview. La destruction depuis le thread tray doit utiliser un canal ou un flag atomique.

**R4 (MOYEN) : HTTP server Shutdown vs active SSE/polling**
Si le frontend a un `fetch('/api/status')` en cours quand `httpServer.Shutdown()` est appelé, la requête sera annulée. Acceptable car l'UI se ferme de toute façon. Timeout 3s suffisant.

### References

- [Source: architecture.md#Architecture 2 Processus & Intégration OS] — Architecture 2 processus, packages internal/ui/
- [Source: architecture.md#Core Architectural Decisions] — Stack webview/webview + fyne.io/systray
- [Source: epics.md#Epic 12 Story 12.1] — Acceptance criteria originaux
- [Source: architecture.md#Communication & Protocole] — IPC named pipes, protocole JSON, actions
- [Source: architecture.md#Composants Communs] — SysProxy (internal/ui/sysproxy_windows.go), Singleton, HTTP server local

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

None — clean implementation, no debug issues.

### Completion Notes List

- **Task 1:** Refactored `shutdownServiceAndRestore()` → `shutdown()` using `sync.Once` for idempotence. Added webview termination (via `w.Terminate()` callback), HTTP server graceful shutdown (3s timeout), and unified cleanup in a single function called by both `handleQuit()` and `onExit()`. Context cancel + IPC close moved into shutdown sequence.
- **Task 2:** Already satisfied by existing architecture. Webview runs in a goroutine, `w.Run()` blocks until user closes window, `webviewOpen` atomic bool prevents double window. Added terminate/clear callbacks for shutdown integration.
- **Task 3:** Service shutdown sequence already correct (IPC stopped last). Fixed `dns-original.json` to only be deleted after successful DNS restoration (was unconditionally deleted before). `sync.Once` already in place via `stopOnce`.
- **Task 4:** Already implemented — `Save()`, `Restore()`, `RecoverOrphan()` with DPAPI encryption and atomic file writes.
- **Task 5:** Added "IP en détection..." tooltip when connected but IP is empty (async detection pending). Service handler already returns `status: "connected"` independent of IP.
- **Task 6:** `TestShutdown_Idempotent` (10 concurrent goroutines, verifies single execution), `TestGetStatus_MissingIP` (connected + empty IP), `TestSysProxyPersistence` (file write/read/cleanup), `TestSysProxyPersistence_AtomicWrite`. Service-side `TestShutdownSequence_IPCServerLast` already existed.

### Change Log

- 2026-04-08: Story 12.1 implemented — shutdown propre, lifecycle webview, DNS state file fix, IP detection tooltip
- 2026-04-09: Code review — 6 findings (1 HIGH, 2 MEDIUM, 3 LOW), all fixed: race condition webview terminate/destroy, singleton mutex rename, shutdown terminate test, t.Helper misuse, HTTPServer.Shutdown sync

### File List

- `internal/ui/ui.go` — Modified: shutdown() via sync.Once, webview terminate callback, HTTP server shutdown, unified cleanup
- `internal/ui/webview_cgo.go` — Modified: openWebview() with atomic alive guard against use-after-free, setTerminate/clearTerminate callbacks
- `internal/ui/webview_nocgo.go` — Modified: signature update to match cgo variant
- `internal/ui/httpserver.go` — Modified: added Shutdown(ctx) with ready channel sync
- `internal/ui/singleton_windows.go` — Modified: mutex name Global\LeVoileTray → Global\LeVoileUI
- `internal/dns/manager_windows.go` — Modified: dns-original.json only removed on successful restore
- `internal/ui/shutdown_test.go` — Created: TestShutdown_Idempotent, TestShutdown_CallsWebviewTerminate, TestShutdown_NoWebview
- `internal/ui/sysproxy_test.go` — Created: TestSysProxyPersistence, TestSysProxyPersistence_AtomicWrite
- `internal/ui/ui_test.go` — Modified: added TestGetStatus_MissingIP

## Senior Developer Review (AI)

**Review Date:** 2026-04-09
**Review Outcome:** Approve (after fixes)
**Reviewer Model:** Claude Opus 4.6

### Findings Summary

| # | Severity | Description | Status |
|---|----------|-------------|--------|
| 1 | HIGH | Race condition: `w.Terminate()` could be called after `w.Destroy()` in webview_cgo.go | ✅ Fixed — atomic alive guard |
| 2 | MEDIUM | Singleton mutex name `Global\LeVoileTray` should be `Global\LeVoileUI` | ✅ Fixed |
| 3 | MEDIUM | No test verifying shutdown calls webview terminate callback | ✅ Fixed — TestShutdown_CallsWebviewTerminate |
| 4 | LOW | Error returns ignored in doShutdown (sysProxy.Restore, httpServer.Shutdown) | ✅ Accepted — best-effort during shutdown |
| 5 | LOW | t.Helper() misused on test functions instead of helper functions | ✅ Fixed |
| 6 | LOW | HTTPServer.Shutdown accesses s.server without sync | ✅ Fixed — waits on ready channel |

### Action Items

- [x] [AI-Review][HIGH] Add atomic alive guard in webview_cgo.go to prevent use-after-free [internal/ui/webview_cgo.go]
- [x] [AI-Review][MEDIUM] Rename mutex Global\LeVoileTray → Global\LeVoileUI [internal/ui/singleton_windows.go:11]
- [x] [AI-Review][MEDIUM] Add TestShutdown_CallsWebviewTerminate [internal/ui/shutdown_test.go]
- [x] [AI-Review][LOW] Remove t.Helper() from test functions [internal/ui/shutdown_test.go, internal/ui/sysproxy_test.go]
- [x] [AI-Review][LOW] Add ready channel sync in HTTPServer.Shutdown [internal/ui/httpserver.go]
