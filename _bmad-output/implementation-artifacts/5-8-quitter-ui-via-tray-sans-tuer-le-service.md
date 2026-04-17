# Story 5.8 : Quitter l'UI via tray sans tuer le service

Status: ready-for-dev

<!-- Épic 5 : Interface Desktop Cross-Platform (Tray + Webview). FRs couverts : FR13b, FR15b, FR16b. Source : _bmad-output/planning-artifacts/epics.md §5.8. -->

## Story

En tant qu'**utilisateur final** de Le Voile,
je veux **pouvoir quitter l'UI tout en gardant ma protection active en arrière-plan**,
afin de **libérer la RAM de l'UI (webview + serveur HTTP local) sans perdre ma protection VPN**.

## Acceptance Criteria

**AC1 — « Quitter » via tray arrête l'UI mais pas le service**

**Given** l'UI est active (tray + webview optionnellement ouvert) et le tunnel est connecté
**When** l'utilisateur sélectionne « Quitter » dans le menu clic droit du tray
**Then** l'UI envoie une notification IPC au service (nouvelle action `ui_disconnect`, pas `quit`)
**And** le service prend acte de la déconnexion de l'UI sans déclencher de shutdown du tunnel
**And** le serveur HTTP local de l'UI (`127.0.0.1:{port}`) s'arrête via `http.Server.Shutdown(ctx)` avec timeout 3 s
**And** la fenêtre webview est détruite si ouverte (`webview.Destroy()`)
**And** le client IPC de l'UI est fermé
**And** `systray.Quit()` termine la boucle tray et le processus UI
**And** le processus `levoile-service` (SCM Windows / systemd Linux) **reste en cours d'exécution**
**And** l'interface TUN `levoile0` reste montée, le tunnel QUIC/HTTP3 reste connecté, le kill switch firewall (nftables/WFP) reste actif
**And** aucun processus UI orphelin ne subsiste (vérification `ps -ef | grep levoile-ui` / `tasklist`)

**AC2 — Arrêt complet via commande système**

**Given** l'utilisateur veut arrêter complètement Le Voile (service + tunnel)
**When** il exécute `sudo systemctl stop levoile.service` (Linux) ou `sc stop levoile-service` (Windows, en admin) ou passe par Services.msc
**Then** le service exécute la séquence Disconnect dans l'ordre : `tunnel.Stop()` → `firewall.Deactivate()` → `routing.Cleanup()` → `tun.Close()`
**And** toutes les ressources système sont libérées (routes, règles firewall, interface TUN)
**And** aucun paquet résiduel ne subsiste (vérif `ip route show table 51820` vide sur Linux, WFP providers retirés sur Windows)
**And** si l'UI était encore active, elle détecte la perte IPC (voir Story 5.6 — écran fallback « Service non démarré »)

**AC3 — Propriété des ressources respectée (pas de restauration côté UI)**

**Given** le design impose que le service soit propriétaire des ressources système (DNS, TUN, firewall, routing)
**When** l'UI quitte via AC1
**Then** l'UI ne tente **PAS** d'appeler `RecoverOrphanDNS()`, `firewall.Deactivate()` ni aucune restauration de ressource système
**And** l'UI ne touche qu'à ses propres ressources : webview, serveur HTTP local, client IPC, singleton lock
**And** les tests `TestShutdown_Idempotent` et assimilés sont mis à jour : la séquence n'envoie plus `ActionQuit`, elle envoie `ActionUIDisconnect` (notification only, pas d'impact lifecycle service)

**AC4 — Idempotence et singleton**

**Given** le processus UI reçoit une commande d'arrêt
**When** un second signal arrive (double clic Quitter, fermeture session OS, Task Manager pendant shutdown)
**Then** la séquence shutdown UI ne s'exécute qu'une seule fois (`sync.Once` préservé)
**And** le singleton (mutex nommé Windows / flock Linux) est relâché via `defer ReleaseSingleton()` dans `cmd/ui/main.go`
**And** relancer l'UI immédiatement après (ex: raccourci bureau) réussit et se reconnecte au service toujours actif

**AC5 — Le service gère la déconnexion UI proprement**

**Given** le service reçoit une requête IPC `ActionUIDisconnect`
**When** le handler la traite
**Then** il répond immédiatement `{status: "ok"}` sans appeler `prg.RequestStop()` ni `prg.Reconnector().Stop()`
**And** si le service tient un compteur de sessions UI connectées (optionnel), il le décrémente
**And** la connexion IPC est refermée côté UI après réception de la réponse
**And** aucune action lifecycle n'est déclenchée côté service (pas de tunnel stop, pas de firewall deactivate)

**AC6 — Non-régression : bouton fermeture fenêtre (croix X)**

**Given** la fenêtre webview est ouverte
**When** l'utilisateur clique sur la croix X de fermeture
**Then** la fenêtre webview est détruite (libération mémoire GPU + WebKit/WebView2)
**And** le tray reste visible
**And** le serveur HTTP local continue d'écouter (réouverture rapide sans redémarrer)
**And** le service, le tunnel et le kill switch restent actifs
**And** le comportement est distinct de « Quitter » (croix ≠ quit, conformément à AC6 de Story 5.5)

## Tasks / Subtasks

- [ ] **Task 1 : Ajouter l'action IPC `ActionUIDisconnect`** (AC: 1, 3, 5)
  - [ ] 1.1 Dans [internal/ipc/messages.go](internal/ipc/messages.go), ajouter `ActionUIDisconnect = "ui_disconnect"` à la liste des actions
  - [ ] 1.2 Dans [internal/ipchandler/handler.go](internal/ipchandler/handler.go), ajouter un `case ipc.ActionUIDisconnect` dans le dispatcher qui appelle une nouvelle fonction `handleUIDisconnect(prg)`
  - [ ] 1.3 Implémenter `handleUIDisconnect(prg *svc.Program) ipc.Response` : retourne immédiatement `ipc.Response{Status: ipc.StatusOK}` **sans** appeler `prg.RequestStop()` ni `r.Stop()`. Optionnel : log une ligne « UI disconnected »
  - [ ] 1.4 Ajouter un test unitaire dans [internal/ipchandler/handler_test.go](internal/ipchandler/handler_test.go) : `TestHandleUIDisconnect_DoesNotStopService` — vérifie que `prg.RequestStop` n'est pas appelé et que le reconnector reste actif

- [ ] **Task 2 : Refactorer la séquence `doShutdown()` côté UI** (AC: 1, 3, 4, 6)
  - [ ] 2.1 Dans [internal/ui/ui.go](internal/ui/ui.go#L220) `doShutdown()`, remplacer l'envoi de `ipc.ActionQuit` (ligne 251) par `ipc.ActionUIDisconnect`
  - [ ] 2.2 Supprimer l'appel à `dns.RecoverOrphanDNS(context.Background())` ligne 258 — le service reste propriétaire du DNS. Supprimer l'import `internal/dns` s'il n'est plus utilisé dans ce fichier
  - [ ] 2.3 Supprimer la restauration WinINET `u.sysProxy.Restore()` / `ForceDisable()` (lignes 235-240) **uniquement** si le design Epic 5 actuel supprime le sysproxy (à vérifier par rapport à l'état de la migration : l'architecture révisée 2026-04-15 supprime les proxy locaux au profit de la capture L3 TUN). Si `SysProxy` est déjà retiré du champ `UI` ou renvoie no-op, cette étape est déjà satisfaite. Sinon, conserver temporairement pour compatibilité
  - [ ] 2.4 Supprimer `time.Sleep(1 * time.Second)` ligne 254 (attente shutdown service — plus nécessaire puisqu'on ne le shutdowne plus)
  - [ ] 2.5 Mettre à jour le commentaire ligne 213-215 décrivant la nouvelle séquence : `shutdownInProgress → destroy webview → shutdown HTTP server → ActionUIDisconnect IPC → cancel ctx → close IPC`
  - [ ] 2.6 Vérifier que `handleQuit()` ligne 206-209 reste inchangé (appelle `shutdown()` puis `menuAPI.Quit()`)

- [ ] **Task 3 : Mettre à jour les tests de shutdown UI** (AC: 3)
  - [ ] 3.1 Dans [internal/ui/shutdown_test.go](internal/ui/shutdown_test.go), remplacer `ipc.ActionQuit` par `ipc.ActionUIDisconnect` aux lignes 64-74 et 133-138
  - [ ] 3.2 Renommer les tests si pertinent (ex: `TestShutdown_SendsActionQuit` → `TestShutdown_SendsActionUIDisconnect`)
  - [ ] 3.3 Ajouter un nouveau test `TestShutdown_DoesNotSendActionQuit` qui vérifie explicitement qu'**aucun** `ActionQuit` n'est envoyé pendant `shutdown()`
  - [ ] 3.4 Vérifier que `TestShutdown_Idempotent` passe toujours avec la nouvelle action

- [ ] **Task 4 : Vérifier le flux de démarrage UI après quit** (AC: 4)
  - [ ] 4.1 Dans [cmd/ui/main.go](cmd/ui/main.go#L45-L52), la fonction `ensureService()` est Windows-spécifique (`levoile-service.exe start`). Cette logique gère le cas où l'UI est lancée après un arrêt complet du service. Aucune modif nécessaire — elle reste correcte
  - [ ] 4.2 Vérifier manuellement : après `Quitter` UI, relancer l'UI via raccourci bureau → l'UI se reconnecte au service qui tourne déjà (pas de double-start tentative destructive)
  - [ ] 4.3 Sur Linux (post-packaging Epic 7), ajouter un équivalent `ensureService()` conditionnel : si `systemctl is-active levoile.service` retourne `inactive`, proposer un message au lieu de tenter un `systemctl start` (privilège root requis — pas pour Story 5.8)

- [ ] **Task 5 : Tests d'intégration end-to-end** (AC: 1, 2, 5)
  - [ ] 5.1 Scénario manuel Windows : service démarré, UI lancée, tunnel connecté → menu tray « Quitter » → vérifier via `tasklist | findstr levoile` que `levoile-service.exe` reste (et que `levoile-ui.exe` a disparu)
  - [ ] 5.2 Vérifier via `route print` que les routes TUN restent en place, et que l'IP visible reste celle du relais (test dans un navigateur vers ipinfo.io ou `/api/status` plus disponible — passer par curl externe)
  - [ ] 5.3 Scénario manuel Linux : `systemctl --user status levoile-ui.service` (si Story 5.7 est implémentée) → `Quitter` → unit UI reste `active` (auto-restart) ou `inactive` selon impl. `systemctl status levoile.service` (system) doit rester `active (running)`
  - [ ] 5.4 Scénario « arrêt complet » : avec service + UI actifs → `sudo systemctl stop levoile.service` → vérifier extinction complète, DNS restauré, routes nettoyées, firewall désactivé
  - [ ] 5.5 Ajouter une note dans `docs/testing/manual-e2e-scenarios.md` (ou équivalent) décrivant les deux scénarios d'arrêt

- [ ] **Task 6 : Documentation utilisateur** (AC: 2)
  - [ ] 6.1 Mettre à jour `README.md` (section usage Windows/Linux) avec la distinction :
    - « Quitter » via tray → arrête l'UI uniquement (la protection reste active en tâche de fond)
    - Arrêt complet : Services.msc / `sc stop levoile-service` (Windows) ou `sudo systemctl stop levoile.service` (Linux)
  - [ ] 6.2 S'assurer que le menu tray lui-même ne prétend pas « Quitter Le Voile » — le tooltip actuel « Quitter Le Voile » dans [internal/ui/ui.go:128](internal/ui/ui.go#L128) est trompeur. Remplacer par « Quitter l'interface (la protection reste active) »

## Dev Notes

### Architecture confirmée

- **Séparation des processus** : architecture.md §300-317 confirme `levoile-service` (privilégié, SCM/systemd) et `levoile-ui` (user-space, un par utilisateur Linux). La clause ligne 317 est prescriptive : « Quitter depuis le tray arrête l'UI (pas le service — le service reste contrôlé par systemd/SCM) ».
- **Propriété des ressources** : le service possède DNS, TUN, firewall, routing (architecture.md §324-332). L'UI ne doit toucher qu'à ses propres ressources (webview, serveur HTTP, singleton, IPC client).

### État actuel du code à modifier

- [internal/ui/ui.go:251](internal/ui/ui.go#L251) envoie actuellement `ipc.ActionQuit` → **comportement obsolète hérité de l'ancienne story 12.1** (fichier `12-1-shutdown-propre-et-independance-service-ui.md`, marqué `done`). Cette story 5.8 inverse explicitement ce comportement.
- [internal/ipchandler/handler.go:292-301](internal/ipchandler/handler.go#L292-L301) — `handleQuit` appelle `prg.RequestStop()` (via `kardianos/service`). **Ne pas toucher à `handleQuit`** : il reste utilisé pour `levoile-ctl stop` et pour l'arrêt programmatique (Story 5.9 mode dégradé peut l'utiliser). Ajouter `handleUIDisconnect` en parallèle.
- Tests existants ([shutdown_test.go:64-74, 133-138](internal/ui/shutdown_test.go)) valident que `ActionQuit` est envoyé → doivent être inversés.

### IPC : nouveau contrat

```
ActionUIDisconnect = "ui_disconnect"
```

- Requête : `{"action": "ui_disconnect"}`
- Réponse : `{"status": "ok"}`
- Sémantique : notification d'arrêt UI, le service ne doit déclencher **aucune** action lifecycle
- Différence avec `ActionQuit` : `ActionQuit` demande l'arrêt du service via `prg.RequestStop()`, `ActionUIDisconnect` ne fait rien lifecycle

### Source tree à toucher

- [internal/ipc/messages.go](internal/ipc/messages.go) — ajouter constante
- [internal/ipchandler/handler.go](internal/ipchandler/handler.go) — ajouter case + handler
- [internal/ipchandler/handler_test.go](internal/ipchandler/handler_test.go) — ajouter test
- [internal/ui/ui.go](internal/ui/ui.go) — refactor `doShutdown()` (lignes 128, 213-265)
- [internal/ui/shutdown_test.go](internal/ui/shutdown_test.go) — mettre à jour tests
- [README.md](README.md) — section usage (optionnel, peut être reporté)

### Standards de test

- Tests unitaires Go : `go test ./internal/ipchandler/... ./internal/ui/...` doit passer
- Pas de mocks de DB (non applicable ici)
- Mocks : `trackingIPCClient`, `mockSystrayAPI`, `mockSystrayMenuAPI` déjà présents dans le package `ui` — les réutiliser
- Intégration : scénarios manuels décrits Task 5 (pas d'automation E2E à ce stade — Story 6.x pour Playwright/Cypress)

### Pièges connus à éviter

1. **Ne PAS** supprimer `handleQuit` de l'ipchandler — d'autres consommateurs existent (CLI `levoile-ctl`, shutdown complet via SCM callback)
2. **Ne PAS** supprimer `ipc.ActionQuit` de `messages.go` — toujours utilisable pour arrêt programmatique complet (futur Story 5.9 peut l'utiliser pour « Arrêter Le Voile complètement »)
3. **Ne PAS** toucher à `handleOpenWebview` / croix X — AC6 vérifie que ce flux est inchangé
4. **Vérifier** que `shutdownInProgress.Store(true)` reste en début de séquence pour bloquer l'orphan recovery IPC (AC7 de l'ancienne story 12.1 — toujours pertinent)
5. **Ne PAS** supprimer la séquence destroy webview / HTTP server shutdown — AC1 les exige

### Project Structure Notes

- Conforme à l'arborescence : `internal/ipc/`, `internal/ipchandler/`, `internal/ui/`, `cmd/ui/` (architecture.md §305-336)
- Aucun nouveau package requis
- Aucune migration TUN/firewall/routing (hors scope — Story 5.8 est purement UI/IPC)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-5.8] — Acceptance criteria originaux
- [Source: _bmad-output/planning-artifacts/architecture.md#L300-L317] — Architecture 2 processus, propriété des ressources
- [Source: _bmad-output/planning-artifacts/architecture.md#L489] — API `POST /api/quit` existante (à garder côté HTTP server, proxy vers `ActionUIDisconnect`)
- [Source: _bmad-output/implementation-artifacts/12-1-shutdown-propre-et-independance-service-ui.md] — Ancienne story dont cette 5.8 corrige le comportement (comportement ancien : UI quit arrête service ; comportement nouveau : UI quit n'arrête pas service)
- [Source: internal/ui/ui.go#L206-L265] — Séquence `handleQuit` + `doShutdown` à refactorer
- [Source: internal/ipchandler/handler.go#L288-L301] — `handleQuit` actuel (à conserver tel quel)

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

---

## Previous Story Intelligence

Aucune story précédente existante dans la **nouvelle** structure Epic 5 (UI cross-platform) — toutes les stories 5.1-5.7 sont `backlog`. Les fichiers `5-1-*.md` à `5-3-*.md` et `10-x-*.md` / `12-x-*.md` présents dans `_bmad-output/implementation-artifacts/` appartiennent à l'**ancienne** structure (STUN/Wails/3 processus) et ne servent qu'au reference tree, pas au contexte de 5.8.

**Exception pertinente** : `12-1-shutdown-propre-et-independance-service-ui.md` (ancienne version) contient la séquence actuelle d'arrêt que Story 5.8 doit **inverser** — elle est citée en références comme contre-exemple.

## Git Intelligence Summary

Commits récents pertinents (git log) :
- `16a275a fix(deploy): align install.sh/README/service with prod (signing.key + registry)` — non pertinent
- `c2e1c0e feat: complete Epic 3 — relay stateless multi-VPS with tunnel IP & NAT` — Epic 3 done, ne touche pas l'UI
- `bd11612 feat: IPv6 leak opt-out toggle + relay systemd CAP_NET_ADMIN (Stories 2.9 + 3.1)` — ajoute `AllowIPv6Leak` dans IPC response (messages.go:112) — contexte IPC à jour
- `ece3270 feat: implement Sprint 2 — watchdog, routing, firewall, DNS flush, captive portal` — Epic 2 done

Le code `internal/ui/ui.go` et `internal/ipchandler/handler.go` sont dans leur état stable post-Epic 2/3. L'Epic 5 UI n'a pas commencé au niveau code — Story 5.8 est un refactor ciblé dans un code stable.

## Project Context Reference

Le projet Le Voile est un VPN Go cross-platform (Windows/Linux) avec architecture 2 processus (service privilégié + UI user-space), communicant via IPC (named pipes Windows / unix socket Linux). Restructuration majeure 2026-04-15 : capture L3 TUN/Wintun machine-wide (remplace proxy DNS local), suppression extension navigateur, Epic 5 réécrite en UI unique systray + webview. Config utilisateur : français, `intermediate` skill. Commits signés `velia-the-veil`.

## Story Completion Status

- **Created** : 2026-04-17
- **Status** : ready-for-dev
- **Epic** : 5 (backlog → transitionne vers in-progress dès première story implémentée)
- **Blocking dependencies** : aucune (refactor isolé dans UI + IPC)
- **Unlocks** : contribue à la complétion Epic 5 ; aucune story n'est strictement bloquée par 5.8
- **Estimated complexity** : faible (≈ 80-150 LoC modifiées, principalement dans `ui.go` + un nouveau handler IPC + mise à jour de 2 tests)

---

*Ultimate context engine analysis completed — comprehensive developer guide created.*
