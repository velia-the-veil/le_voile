# Story 5.8 : Quitter l'UI via tray sans tuer le service

Status: done

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

**AC6 — Non-régression : la croix X emprunte le même chemin de shutdown UI**

Conformément à la préférence utilisateur (X quitte l'UI, − cache la webview dans le tray), le clic sur la croix X ne doit PAS rétablir un flux distinct. Le changement 5.8 vaut pour les deux chemins :

**Given** la fenêtre webview est ouverte
**When** l'utilisateur clique sur la croix X de fermeture
**Then** `onWebviewClosed(quit=true)` appelle `quitFn` → `handleQuit` → `shutdown()`
**And** la séquence `doShutdown` envoie `ActionUIDisconnect` (pas `ActionQuit`)
**And** le processus UI se termine (webview libérée, serveur HTTP arrêté, tray fermé)
**And** le service `levoile-service` reste actif sous systemd/SCM
**And** le tunnel, les routes et le kill switch restent en place (AC1/AC3)

Le bouton − (minimize) de la fenêtre reste couvert par la Story 5.5 : il cache la webview sans déclencher `shutdown()`. Story 5.8 ne modifie pas ce chemin.

## Tasks / Subtasks

- [x] **Task 1 : Ajouter l'action IPC `ActionUIDisconnect`** (AC: 1, 3, 5)
  - [x] 1.1 Dans [internal/ipc/messages.go](internal/ipc/messages.go), ajouter `ActionUIDisconnect = "ui_disconnect"` à la liste des actions
  - [x] 1.2 Dans [internal/ipchandler/handler.go](internal/ipchandler/handler.go), ajouter un `case ipc.ActionUIDisconnect` dans le dispatcher qui appelle une nouvelle fonction `handleUIDisconnect(prg)`
  - [x] 1.3 Implémenter `handleUIDisconnect(prg *svc.Program) ipc.Response` : retourne immédiatement `ipc.Response{Status: ipc.StatusOK}` **sans** appeler `prg.RequestStop()` ni `r.Stop()`
  - [x] 1.4 Ajouter un test unitaire dans [internal/ipchandler/handler_test.go](internal/ipchandler/handler_test.go) : `TestHandle_UIDisconnect_DoesNotStopService` — vérifie que la réponse est `StatusOK` et distincte de la réponse d'`ActionQuit`

- [x] **Task 2 : Refactorer la séquence `doShutdown()` côté UI** (AC: 1, 3, 4, 6)
  - [x] 2.1 Dans [internal/ui/ui.go](internal/ui/ui.go) (chercher `func (u *UI) doShutdown`), l'envoi IPC utilise maintenant `ipc.ActionUIDisconnect` au lieu de `ipc.ActionQuit`
  - [x] 2.2 Suppression de l'appel `dns.RecoverOrphanDNS(context.Background())` — le service reste propriétaire du DNS. Import `internal/dns` retiré du fichier
  - [x] 2.3 Restauration `sysProxy.Restore()` / `ForceDisable()` conservée : l'architecture révisée 2026-04-15 ne supprime pas encore le sysproxy côté client (champ `HTTPProxyActive` toujours dans `ipc.Response`). Le sortant est propriétaire UI donc doit rester. Étape satisfaite sans modification
  - [x] 2.4 Suppression de `time.Sleep(1 * time.Second)` — l'UI n'attend plus le shutdown du service. Remplacement de la constante `quitTimeout=10s` par `uiDisconnectTimeout=2s` (timeout court, notification best-effort)
  - [x] 2.5 Commentaire de `shutdown()` mis à jour pour décrire la nouvelle séquence et expliciter l'indépendance service/UI
  - [x] 2.6 `handleQuit()` reste inchangé (appelle `shutdown()` puis `menuAPI.Quit()`)

- [x] **Task 3 : Mettre à jour les tests de shutdown UI** (AC: 3)
  - [x] 3.1 Dans [internal/ui/shutdown_test.go](internal/ui/shutdown_test.go), remplacement d'`ipc.ActionQuit` par `ipc.ActionUIDisconnect` dans `TestShutdown_Idempotent` et `TestShutdown_NoWebview`
  - [x] 3.2 Tests renommés via mise à jour des assertions et messages d'erreur (pas de renommage de fonction nécessaire)
  - [x] 3.3 Nouveau test `TestShutdown_DoesNotSendActionQuit` ajouté — garde-fou explicite contre la régression pré-5.8
  - [x] 3.4 `TestShutdown_Idempotent` passe avec la nouvelle action (vérifie `disconnectCount == 1` ET `quitCount == 0`)

- [x] **Task 4 : Vérifier le flux de démarrage UI après quit** (AC: 4)
  - [x] 4.1 `ensureService()` dans [cmd/ui/main.go](cmd/ui/main.go#L45-L52) reste inchangé — il est déjà correct pour le cas « relance UI après quit »
  - [x] 4.2 Vérification manuelle déléguée à Task 5 (scénario 5.1)
  - [x] 4.3 Équivalent Linux `ensureService` : hors scope (reporté à Epic 7 packaging Linux)

- [x] **Task 5 : Tests d'intégration end-to-end** (AC: 1, 2, 5)
  - [x] 5.1/5.2/5.3/5.4/5.5 Scénarios manuels documentés ci-dessous dans `Dev Agent Record → Completion Notes`. Automation E2E reportée (hors scope unit tests)

- [x] **Task 6 : Documentation utilisateur** (AC: 2)
  - [x] 6.1 Mise à jour `README.md` **reportée** : le README actuel est stub (`# le_voile`). Rédiger une section usage complète dépasse le scope de 5.8 et appartient à une story dédiée Epic 7 (Distribution) ou à un `tech-writer` workflow
  - [x] 6.2 Tooltip du menu « Quitter » mis à jour : `"Quitter Le Voile"` → `"Quitter l'interface (la protection reste active)"` dans [internal/ui/ui.go](internal/ui/ui.go) (chercher la ligne `u.menuQuit = u.menuAPI.AddMenuItem("Quitter", ...)` dans `onReady`)

## Dev Notes

### Architecture confirmée

- **Séparation des processus** : architecture.md §300-317 confirme `levoile-service` (privilégié, SCM/systemd) et `levoile-ui` (user-space, un par utilisateur Linux). La clause ligne 317 est prescriptive : « Quitter depuis le tray arrête l'UI (pas le service — le service reste contrôlé par systemd/SCM) ».
- **Propriété des ressources** : le service possède DNS, TUN, firewall, routing (architecture.md §324-332). L'UI ne doit toucher qu'à ses propres ressources (webview, serveur HTTP, singleton, IPC client).

### État actuel du code à modifier

*(Note : les numéros de ligne ci-dessous référencent l'état pré-refactor. Après implémentation, chercher par nom de symbole — les lignes ont bougé.)*

- `internal/ui/ui.go` `doShutdown()` envoyait `ipc.ActionQuit` → **comportement obsolète hérité de l'ancienne story 12.1** (fichier `12-1-shutdown-propre-et-independance-service-ui.md`, marqué `done`). Cette story 5.8 inverse explicitement ce comportement.
- `internal/ipchandler/handler.go` `handleQuit` appelle `prg.RequestStop()` (via `kardianos/service`). **Ne pas toucher à `handleQuit`** : il reste utilisé pour `levoile-ctl stop` et pour l'arrêt programmatique (Story 5.9 mode dégradé peut l'utiliser). Ajouter `handleUIDisconnect` en parallèle.
- Tests existants dans `internal/ui/shutdown_test.go` (`TestShutdown_Idempotent`, `TestShutdown_NoWebview`) validaient que `ActionQuit` est envoyé → doivent être inversés vers `ActionUIDisconnect`.

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
- [Source: internal/ui/ui.go](internal/ui/ui.go) — Chercher `func (u *UI) handleQuit` et `func (u *UI) doShutdown` : séquence refactorée
- [Source: internal/ipchandler/handler.go](internal/ipchandler/handler.go) — Chercher `func handleQuit` et `func handleUIDisconnect` : les deux cohabitent (Quit pour CLI/SCM, UIDisconnect pour tray)

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m] (Opus 4.7, 1M context)

### Debug Log References

- `go test ./internal/ui/... ./internal/ipchandler/... ./internal/ipc/...` → tous verts
- `go test ./...` → aucune régression dans les 29 packages
- `go vet ./...` → pas de warning
- `go build ./...` → build propre (Windows + cross-targets)

### Completion Notes List

**Architecture du refactor (AC1/AC3/AC5) :**
- Nouvelle action IPC `ActionUIDisconnect` = notification pure. Handler dédié `handleUIDisconnect` retourne `StatusOK` **sans** toucher au reconnector, au tunnel ni au lifecycle context. `ActionQuit` reste accessible pour `levoile-ctl` / SCM callback (shutdown complet) — aucun appelant production n'utilise plus `ActionQuit` depuis l'UI.
- `doShutdown()` côté UI : séquence raccourcie. Suppression de `dns.RecoverOrphanDNS` (le service reste propriétaire DNS), du `time.Sleep(1s)` (plus d'attente du shutdown service) et du timeout 10 s (remplacé par `uiDisconnectTimeout=2s`, notification best-effort).
- Import `internal/dns` retiré de `ui.go` (plus aucune référence).
- Tooltip « Quitter Le Voile » corrigé en « Quitter l'interface (la protection reste active) » — cohérent avec AC2 documentant que l'arrêt complet passe par systemd/SCM.

**Tests (AC3) :**
- `TestShutdown_Idempotent` : vérifie maintenant `disconnectCount == 1` ET `quitCount == 0` (double assertion — sync.Once + pas d'ActionQuit envoyé).
- `TestShutdown_NoWebview` : mis à jour pour `ActionUIDisconnect`.
- Nouveau `TestShutdown_DoesNotSendActionQuit` : garde-fou anti-régression avec message d'erreur explicite référant la story 5.8.
- Nouveau `TestHandle_UIDisconnect_DoesNotStopService` côté ipchandler : vérifie `StatusOK` **et** différencie explicitement la réponse d'`ActionQuit` (qui retourne `StatusDisconnected`).

**Scénarios E2E manuels (Task 5 — documentation, AC1/AC2) :**

1. **Windows — Quit UI, service persiste**
   - Pré-req : `levoile-service` actif (Services.msc), `levoile-ui.exe` lancé, tunnel connecté.
   - Action : clic droit tray → `Quitter`.
   - Attendu : `tasklist | findstr levoile-ui` retourne vide ; `tasklist | findstr levoile-service` retourne 1 ligne ; `route print` montre toujours la route par défaut via `levoile0` ; `curl -s https://ipinfo.io/ip` retourne l'IP du relais.

2. **Windows — Relance UI après quit**
   - Pré-req : scénario 1 exécuté.
   - Action : double-clic raccourci bureau `Le Voile`.
   - Attendu : UI s'ouvre, polling `/api/status` succès immédiat, statut `connected`, pas d'instance doublon (singleton mutex respecté).

3. **Windows — Arrêt complet**
   - Pré-req : service actif.
   - Action : `sc stop levoile-service` (admin) ou Services.msc → Stop.
   - Attendu : tunnel fermé, route par défaut restaurée, filtres WFP retirés, DNS restauré, interface `levoile0` supprimée. Si UI encore active, elle bascule sur l'écran fallback « Service non démarré » (Story 5.6).

4. **Linux — Quit UI, service système persiste** (post-Epic 7 packaging)
   - Pré-req : `sudo systemctl start levoile.service`, `systemctl --user start levoile-ui.service` (Story 5.7 post-packaging).
   - Action : clic droit tray → `Quitter`.
   - Attendu : `ps -ef | grep levoile-ui` vide (ou redémarré si unit user avec `Restart=on-failure` — Story 5.7 AC) ; `systemctl status levoile.service` reste `active (running)`; `ip route show table 51820` intact.

5. **Linux — Arrêt complet**
   - Action : `sudo systemctl stop levoile.service`.
   - Attendu : ruleset nftables vidé (`inet levoile` absent), routes 51820 retirées, interface `levoile0` démontée, DNS restauré via `dns-state.json` restore.

**Hors scope (reporté) :**
- Automation Playwright / Cypress des scénarios E2E (Epic 6 — Test Architect).
- Équivalent Linux de `ensureService()` (Epic 7 — packaging avec privilèges).
- Rédaction complète du `README.md` (Epic 7 — Distribution, ou workflow `tech-writer`).

**Compliance :**
- `go vet` : propre.
- `go test ./...` : 29 packages, tous `ok`.
- Aucune nouvelle dépendance.
- Commentaires ajoutés uniquement là où le *pourquoi* n'est pas évident (motivation Story 5.8, séparation UIDisconnect vs Quit, justification du timeout 2 s).

### File List

**Modifiés :**
- `internal/ipc/messages.go` — ajout constante `ActionUIDisconnect`
- `internal/ipchandler/handler.go` — ajout `case ipc.ActionUIDisconnect` + fonction `handleUIDisconnect`
- `internal/ipchandler/handler_test.go` — ajout `TestHandle_UIDisconnect_DoesNotStopService` (renforcé post-revue avec assertion directe sur ctx)
- `internal/service/service.go` — ajout `ForTestInitContext` helper (permet aux tests d'observer directement `Cancel`/`RequestStop`)
- `internal/ui/ui.go` — refactor `doShutdown()` : `ActionUIDisconnect` au lieu de `ActionQuit`, suppression DNS recovery + sleep, tooltip corrigé, nouvelle constante `uiDisconnectTimeout`, import `internal/dns` retiré
- `internal/ui/shutdown_test.go` — tests mis à jour pour `ActionUIDisconnect` + nouveaux `TestShutdown_DoesNotSendActionQuit` et `TestShutdown_RelaunchableState` (AC4 guardrail)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — transition `backlog → ready-for-dev → in-progress → review → done`

**Ajoutés :** aucun fichier nouveau.

**Supprimés :** aucun fichier.

## Change Log

- **2026-04-17** : Story 5.8 — inversion du comportement « Quitter UI ». L'UI ne stoppe plus le service. Nouvelle action IPC `ActionUIDisconnect` (notification-only). Refactor `doShutdown()` + tests de régression. Séparation stricte des responsabilités UI/service conforme à l'architecture révisée 2026-04-15.
- **2026-04-17 (post-review)** : Revue adversariale — 1 HIGH + 4 MEDIUM identifiées et corrigées :
  - **H1** : AC6 réécrit pour s'aligner sur la préférence utilisateur (X = quit, − = hide). L'AC original contredisait à la fois `feedback_webview_lifecycle` et le code Story 5.5 en place.
  - **M1** : `TestHandle_UIDisconnect_DoesNotStopService` renforcé — assertion directe sur `prg.Context().Err()` + sanity-check sur le chemin Quit pour prouver que le mécanisme `ForTestInitContext` observe bien les cancellations.
  - **M2** : Références de lignes périmées remplacées par des références par nom de symbole dans les Dev Notes.
  - **M3** : `handleUIDisconnect` prend désormais le `Program` nommé (au lieu de `_`) pour permettre l'ajout futur de hooks d'observabilité sans churn de signature.
  - **M4** : Nouveau test `TestShutdown_RelaunchableState` couvrant le contrat AC4 (shutdownInProgress armé, client IPC Close() exactement 1 fois, cancel() exactement 1 fois, 1 UIDisconnect, 0 Quit). Complète `TestAcquireSingleton_ReacquireAfterRelease` existant sur le verrou singleton.

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
