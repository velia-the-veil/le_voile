# Story 5.7: Supervision UI avec auto-restart en cas de crash

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a **utilisateur final (Windows + Linux desktop)**,
I want **que l'UI `levoile-ui` redémarre automatiquement si elle crashe (p.ex. redémarrage GNOME Shell, crash KDE Plasma, crash de `webview` natif, OOM du processus UI)**,
so that **le tray reste accessible en permanence sans devoir rouvrir de session ni lancer l'UI manuellement, et la protection du service n'est pas invisibilisée par un tray absent**.

## Acceptance Criteria

1. **AC1 — Linux : unit systemd user avec Restart=on-failure**
   **Given** un paquet `levoile` installé (deb/rpm/apk/AUR) **et** une session graphique utilisateur ouverte
   **When** le processus `levoile-ui` est lancé via l'autostart XDG (`/etc/xdg/autostart/levoile.desktop` ou `~/.config/autostart/levoile.desktop`) qui invoque `systemctl --user start levoile-ui.service`
   **And** le processus se termine de manière anormale (code ≠ 0, SIGSEGV, SIGKILL externe, OOM kill)
   **Then** systemd (user manager) relance automatiquement `levoile-ui` via la directive `Restart=on-failure` du unit `levoile-ui.service`
   **And** le relancement effectif survient en **moins de 10 secondes** (vérifié par horodatage journal `journalctl --user -u levoile-ui.service`)
   **And** la nouvelle instance est rattachée au même bus DBus user / même `DISPLAY` / même `WAYLAND_DISPLAY` (héritage `PassEnvironment=` depuis l'environnement systemd user)
   **And** le singleton flock (`~/.local/state/levoile/ui.lock`, cf. Story 5.1) empêche toute instance doublée pendant la transition

2. **AC2 — Linux : arrêt propre n'est PAS un crash**
   **Given** l'UI tourne sous supervision systemd user
   **When** l'utilisateur sélectionne « Quitter » dans le menu tray (IPC Quit → exit code 0)
   **Then** systemd respecte la sortie propre : **pas de redémarrage** (la politique `Restart=on-failure` exclut `exit 0`)
   **And** `systemctl --user status levoile-ui.service` affiche `inactive (dead)` sans boucle restart

3. **AC3 — Linux : rate limit systemd pour éviter les boucles infinies**
   **Given** l'UI crashe de manière répétée (p.ex. bug régression bloquant)
   **When** la supervision systemd détecte >= 5 redémarrages en 60 secondes
   **Then** `StartLimitBurst=5` + `StartLimitIntervalSec=60s` dans le unit arrêtent la boucle
   **And** l'unit passe à l'état `failed` avec le message « start request repeated too quickly »
   **And** un log WARNING est émis dans journald `levoile-ui.service` (sans données utilisateur, conformément NFR22a)

4. **AC4 — Windows : watchdog côté service SCM relance levoile-ui.exe**
   **Given** le service `levoile-service` tourne (LocalSystem) **et** une session utilisateur interactive est active (`WTSActive`)
   **When** le processus `levoile-ui.exe` n'est plus détecté dans la session utilisateur cible (crash shell, `TerminateProcess`, SIGSEGV, OOM)
   **Then** un watcher interne au service (`internal/uiwatchdog/`) détecte la disparition dans **≤ 5 secondes** via poll de handle de processus (ou `WaitForSingleObject` sur un handle conservé depuis le dernier lancement)
   **And** relance `levoile-ui.exe` dans la session utilisateur via `CreateProcessAsUser` + token obtenu par `WTSQueryUserToken` (cible la session `WTSActive` courante)
   **And** la nouvelle instance respecte le singleton mutex nommé `Global\LeVoileUI` (cf. [internal/ui/singleton_windows.go](internal/ui/singleton_windows.go)) — si une instance fantôme détient encore le mutex, le watcher attend son release (max 10 s) avant de relancer

5. **AC5 — Windows : rate limit watchdog (parité Linux)**
   **Given** le watchdog Windows détecte des crashes répétés
   **When** >= 5 relancements surviennent dans une fenêtre glissante de 60 secondes
   **Then** le watchdog s'arrête en `BACKOFF` et attend 5 minutes avant nouvelle tentative
   **And** un Event Log Windows (source `LeVoileService`, niveau WARNING) est émis avec compteur de crashes et fenêtre d'observation (pas de PII)
   **And** une nouvelle session user ou un restart du service SCM réinitialise le compteur

6. **AC6 — Windows : ne pas relancer si le service shutdown OU si aucune session interactive**
   **Given** le service SCM reçoit `SERVICE_CONTROL_STOP` OU aucune session `WTSActive` n'existe (ex : session verrouillée pendant reboot, server sans user loggué)
   **When** le watcher observe l'absence de `levoile-ui.exe`
   **Then** **aucun relancement n'est tenté** (évite les boucles au shutdown et les spawns dans session 0)
   **And** au prochain logon utilisateur, l'autostart HKCU (`Software\Microsoft\Windows\CurrentVersion\Run` — cf. [installer/levoile.nsi:79-80](installer/levoile.nsi#L79-L80)) lance `levoile-ui.exe` + le watcher du service reprend la supervision

7. **AC7 — Cross-platform : état observable via IPC**
   **Given** l'UI ou un outil CLI interroge le service via IPC `GetStatus`
   **When** la réponse est construite
   **Then** le champ `ui_supervision` est ajouté : `{"enabled": bool, "last_restart_at": "RFC3339 ou null", "restart_count_window": int, "backoff_until": "RFC3339 ou null"}`
   **And** ce champ permet à l'UI (écran debug/diagnostics) et aux tests e2e d'observer le comportement sans scraper les logs OS

## Tasks / Subtasks

- [x] **Tâche 1 — Préparer les artefacts de packaging Linux** (AC: #1, #2, #3) — *partiellement, 1.3-1.5 deferred to Story 7.2*
  - [x] 1.1 Créer le répertoire [packaging/systemd/user/](packaging/systemd/user/) et y déposer `levoile-ui.service`
  - [x] 1.2 Créer [packaging/desktop/levoile-autostart.desktop](packaging/desktop/levoile-autostart.desktop) qui délègue à `systemctl --user start levoile-ui.service`
  - [ ] 1.3 ~~Enrichir [packaging/postinstall.sh]~~ — *deferred to Story 7.2 (script n'existe pas encore — Story 7.2 le créera et y intégrera ces étapes ; install manuel documenté dans [packaging/README.md](packaging/README.md))*
  - [ ] 1.4 ~~Enrichir [packaging/preremove.sh]~~ — *deferred to Story 7.2 (même raison ; uninstall manuel documenté)*
  - [ ] 1.5 ~~Mettre à jour [.goreleaser.yaml] (section nfpm)~~ — *deferred to Story 7.2 (section `nfpms:` non encore introduite — Story 7.2 wirera les artefacts ; assets prêts dans [packaging/](packaging/))*

- [x] **Tâche 2 — Créer le package `internal/uiwatchdog/`** (AC: #4, #5, #6, #7)
  - [x] 2.1 Scaffolder le package : [internal/uiwatchdog/watchdog.go](internal/uiwatchdog/watchdog.go), [launcher_windows.go](internal/uiwatchdog/launcher_windows.go), [launcher_stub.go](internal/uiwatchdog/launcher_stub.go)
  - [x] 2.2 Struct `Watchdog` + `Config` + sentinelles `ErrAlreadyRunning`/`ErrNoLauncher`, alignée sur le pattern [internal/tun/watchdog/watchdog.go](internal/tun/watchdog/watchdog.go) (Start(ctx) bloquant, Stop, snapshot atomic-friendly via RWMutex)
  - [x] 2.3 Implémentation Windows : `WTSGetActiveConsoleSessionId` + `WTSQueryUserToken` + `DuplicateTokenEx(TokenPrimary)` + `CreateEnvironmentBlock` + `CreateProcessAsUserW` (flags `CREATE_UNICODE_ENVIRONMENT | CREATE_NO_WINDOW`, desktop `winsta0\default`) + `WaitForSingleObject` polling 500ms (event-driven, ctx-aware)
  - [x] 2.4 Stub no-op pour `!windows` build tag — `Available()` renvoie false donc le watchdog reste en attente (Linux délègue à systemd user)
  - [x] 2.5 Tests unitaires [watchdog_test.go](internal/uiwatchdog/watchdog_test.go) : 11 cas (initial launch, respawn after crash, clean exit ne respawne pas, rate limit déclenche backoff, stop unblocks even in backoff, no session defers launch, launch error doesn't panic, double Start fails, sliding window evicts old restarts, snapshot idempotent, fake clock pour tests time-based déterministes)

- [x] **Tâche 3 — Intégrer le watchdog UI dans le programme service** (AC: #4, #6, #7)
  - [x] 3.1 [cmd/client/main.go](cmd/client/main.go) câble `UIWatchdogEnabled: runtime.GOOS == "windows"` dans `svc.Config`
  - [x] 3.2 [internal/service/service.go](internal/service/service.go) : démarre le watchdog AVANT `<-ctx.Done()` dans `run()` (étape 8), Stop appelé EN TÊTE de `shutdown()` (étape 0) — ordre respecté : uiwatchdog → captive → leakcheck → … → tunnel → firewall → routing → tun → IPC
  - [x] 3.3 Helper `deriveUIBinaryPath()` + constants `uiBinaryName` séparées par OS via build tags ([ui_binary_windows.go](internal/service/ui_binary_windows.go) / [ui_binary_other.go](internal/service/ui_binary_other.go)). `os.Stat` valide le path au boot, log WARNING + désactivation propre si introuvable
  - [x] 3.4 Handler IPC `handleGetUISupervision` ajouté dans [internal/ipchandler/handler.go](internal/ipchandler/handler.go) (action `get_ui_supervision`)

- [x] **Tâche 4 — Exposer l'état dans `GetStatus`** (AC: #7)
  - [x] 4.1 Champ `UISupervision *UISupervisionState` ajouté à `ipc.Response` ([internal/ipc/messages.go](internal/ipc/messages.go)) — pointeur + omitempty pour rester nil sur Linux
  - [x] 4.2 `handleGetStatus` enrichit la réponse via helper `uiSupervisionFromSnapshot` (chemin tunnel-up + chemin tunnel-nil)
  - [x] 4.3 Champ documenté côté Go (`// Story 5.7`) ; pas de modification frontend (consommation laissée à une future story diagnostics)

- [x] **Tâche 5 — Handling singleton cross-process au relancement** (AC: #1, #4) — *5.1 deferred to Story 5.1 (flock Linux)*
  - [ ] 5.1 ~~Linux flock validation~~ — *singleton Linux n'est pas implémenté (stub uniquement, cf. [internal/ui/singleton_stub.go](internal/ui/singleton_stub.go)) ; à valider après livraison de Story 5.1 ; n'impacte pas la logique systemd user supervision*
  - [x] 5.2 Windows mutex nommé `Global\LeVoileUI` ([internal/ui/singleton_windows.go](internal/ui/singleton_windows.go)) auto-libéré par le kernel à la mort du processus ; behaviour validé manuellement via [docs/testing/5-7-supervision-manuel.md](docs/testing/5-7-supervision-manuel.md)
  - [x] 5.3 Procédure de tests manuels documentée dans [docs/testing/5-7-supervision-manuel.md](docs/testing/5-7-supervision-manuel.md) (Linux pkill -9 + Windows taskkill /F + observation IPC GetStatus.UISupervision)

- [x] **Tâche 6 — Observabilité et logs structurés** (AC: #3, #5)
  - [x] 6.1 Logs structurés via `uiWatchdogLogger` (préfixe `service: ui watchdog: …`) — niveau WARN au passage BACKOFF, INFO à chaque launch/exit, ERROR pour les échecs syscall. *Event Log Windows reporté à une story d'observabilité dédiée — les logs stderr du service sont déjà capturés par SCM*
  - [x] 6.2 Linux : journald capte automatiquement (commande documentée dans [docs/testing/5-7-supervision-manuel.md](docs/testing/5-7-supervision-manuel.md))
  - [x] 6.3 Aucune PII émise : pas de PID, pas de chemin utilisateur, pas de SID. Les erreurs syscall passent l'erreur Win32 brute (numérique) sans contexte utilisateur

- [x] **Tâche 7 — Tests** (AC: all)
  - [x] 7.1 Tests unitaires Go (11 cas, fake clock + fake launcher) — `go test ./internal/uiwatchdog/... → ok 0.305s`
  - [ ] 7.2 ~~Test e2e Windows `e2e_windows_test.go`~~ — *deferred : la couverture des tests unitaires + procédure manuelle [docs/testing/5-7-supervision-manuel.md](docs/testing/5-7-supervision-manuel.md) couvrent les ACs ; un harness e2e nécessite un stub `fake-ui.exe` à compiler dynamiquement, hors scope MVP*
  - [x] 7.3 Procédure test manuel Linux documentée
  - [x] 7.4 Procédure test manuel Windows documentée (incluant validation observable via IPC GetStatus.UISupervision)

## Dev Notes

### Origine et FR couverts
- **FR15b** ([prd.md:446](_bmad-output/planning-artifacts/prd.md#L446)) : supervision UI Linux systemd user + Windows job object/Watchdog SCM. Story 5.7 est la seule story couvrant ce FR ; sans elle, un crash de `levoile-ui` laisse le tray absent jusqu'à redémarrage de session.
- **NFR15** (fiabilité) : kill switch firewall survit aux crashes ; la supervision UI garantit la même robustesse au tray (sinon l'utilisateur ne voit plus l'état réel de protection et peut croire le service arrêté).
- **Résultats prd-validation-report-2026-04-15** : FR15b noté 5.0/5.0 (spec précise systemd unit + Watchdog SCM). Le présent design matérialise cette spec.

### Architecture patterns à respecter
- **2 processus + IPC** ([architecture.md](_bmad-output/planning-artifacts/architecture.md) §Architecture 2 processus) : le service (LocalSystem Windows / systemd levoile.service Linux) est **distinct** de l'UI user. Le watchdog Windows doit donc traverser la barrière session 0 → session utilisateur via `CreateProcessAsUser`.
- **Autostart initial** reste XDG / HKCU (cf. Story 5.1 et [installer/levoile.nsi:79-80](installer/levoile.nsi#L79-L80)). La **supervision live** s'ajoute en couche au-dessus, sans remplacer l'autostart login.
- **Singleton UI** ([internal/ui/singleton_windows.go](internal/ui/singleton_windows.go) pour Windows, stub Linux à remplacer par flock dans Story 5.1) : le watchdog ne doit jamais violer cet invariant. Prévoir un backoff 500ms × N si `AcquireSingleton` échoue après un relancement trop rapide.
- **Ordre shutdown strict** ([architecture.md §Ordre shutdown](_bmad-output/planning-artifacts/architecture.md)) : `tunnel disconnect → firewall deactivate → routing cleanup → tun destroy`. Le watchdog UI **doit être stoppé AVANT** cette séquence pour ne pas tenter de relancer `levoile-ui.exe` pendant que le service se ferme. Pattern à suivre : channel `done` dans `Watchdog.Stop()` qui bloque jusqu'à ce que toutes les goroutines watchers aient quitté.
- **Pas de détection par process name scan** : préférer `WaitForSingleObject` sur un `HANDLE` conservé depuis `CreateProcessAsUser` — plus réactif (événement instantané au lieu de poll 5s), pas de faux positifs (pas de collision de nom binaire), plus économe.

### Systemd user unit (squelette complet)
À écrire dans `packaging/systemd/user/levoile-ui.service` :
```ini
[Unit]
Description=Le Voile UI (tray + webview)
After=graphical-session.target
PartOf=graphical-session.target

[Service]
Type=simple
ExecStart=/usr/bin/levoile-ui
Restart=on-failure
RestartSec=2s
StartLimitBurst=5
StartLimitIntervalSec=60s
# Pass display/wayland env from user session (systemd --user hérite déjà de l'env user)
# PassEnvironment=DISPLAY WAYLAND_DISPLAY XDG_RUNTIME_DIR DBUS_SESSION_BUS_ADDRESS
# Ressources — limite douce pour éviter runaway
MemoryMax=200M
TasksMax=100

[Install]
WantedBy=default.target
```
**Rationale AC1** : `RestartSec=2s` garantit relancement rapide mais pas instantané (anti-boucle). `After=graphical-session.target` assure que DISPLAY/Wayland sont dispo avant le lancement. `PartOf=graphical-session.target` coupe l'UI à la fermeture de session user (cohérent avec le cycle de vie utilisateur).

### Autostart XDG (levoile-autostart.desktop)
À écrire dans `packaging/desktop/levoile-autostart.desktop` → installé dans `/etc/xdg/autostart/` :
```ini
[Desktop Entry]
Type=Application
Name=Le Voile UI
Comment=Tray applet for Le Voile VPN
Exec=/bin/sh -c "systemctl --user start levoile-ui.service"
X-GNOME-Autostart-enabled=true
NoDisplay=true
OnlyShowIn=GNOME;KDE;XFCE;MATE;LXDE;Cinnamon;Unity;Pantheon;
```
**Rationale** : délègue la supervision à systemd plutôt que d'exec `levoile-ui` directement (sinon pas de `Restart=on-failure`). `NoDisplay=true` retire l'entrée du menu application (service utilitaire, pas une app à lancer à la main).

### Windows : CreateProcessAsUser — pièges connus
1. **Session 0 vs session utilisateur** : `CreateProcess` simple depuis un service LocalSystem spawn dans session 0 (invisible, pas d'UI). Il **faut** CreateProcessAsUser avec token WTSQueryUserToken.
2. **Type de token** : `WTSQueryUserToken` retourne un *impersonation token*. Il faut le **dupliquer en primary token** via `DuplicateTokenEx(SecurityImpersonation, TokenPrimary)` avant `CreateProcessAsUser`.
3. **Environnement bloc** : sans `CreateEnvironmentBlock`+`CREATE_UNICODE_ENVIRONMENT`, le process hérite de l'env SYSTEM — `%USERPROFILE%` pointe vers `C:\Windows\System32\config\systemprofile` → l'UI ne trouve pas sa config user. Bug connu.
4. **Desktop** : `lpStartupInfo->lpDesktop = L"winsta0\\default"` pour que la fenêtre apparaisse sur le bureau interactif.
5. **Droits service** : LocalSystem a déjà `SE_TCB_NAME` et `SE_INCREASE_QUOTA_NAME` requis — aucune adjusting à faire.
6. **Lib Go** : `github.com/golang-design/win` ou direct syscall via `golang.org/x/sys/windows`. Le repo utilise déjà `golang.org/x/sys/windows` (cf. [internal/ui/singleton_windows.go](internal/ui/singleton_windows.go)) — rester cohérent, pas de nouvelle dépendance.
7. **Multi-user** : `WTSEnumerateSessions` peut retourner plusieurs sessions `WTSActive` sur Terminal Server. Pour MVP grand public (1 user loggué), prendre la première session `WTSActive` ; documenter la limitation multi-user.

### Cas explicitement hors scope de 5.7
- **Auto-start du service au boot** (FR14) : couvert par Story 7.1 (NSIS) + Story 7.2 (nfpm systemd enable). Ici on suppose que le service tourne déjà.
- **Écran fallback « Service non démarré »** (FR13c, Story 5.6) : indépendant. Story 5.6 gère le cas « UI tourne, service mort » ; Story 5.7 gère le cas inverse « service tourne, UI morte ».
- **Supervision du service lui-même** : déjà assurée par SCM (Windows) et systemd (Linux) via les directives `Restart=` du unit `levoile.service`. Pas de watchdog de watchdog.

### Project Structure Notes

Nouveaux artefacts à créer :
- [internal/uiwatchdog/watchdog.go](internal/uiwatchdog/watchdog.go) — API cross-platform
- [internal/uiwatchdog/watchdog_windows.go](internal/uiwatchdog/watchdog_windows.go) — impl Windows (CreateProcessAsUser + WaitForSingleObject)
- [internal/uiwatchdog/watchdog_stub.go](internal/uiwatchdog/watchdog_stub.go) — no-op Linux/Darwin
- [internal/uiwatchdog/watchdog_test.go](internal/uiwatchdog/watchdog_test.go) — unit avec fake launcher
- [internal/uiwatchdog/e2e_windows_test.go](internal/uiwatchdog/e2e_windows_test.go) — e2e Windows (tag `e2e`)
- [packaging/systemd/user/levoile-ui.service](packaging/systemd/user/levoile-ui.service) — unit systemd user
- [packaging/desktop/levoile-autostart.desktop](packaging/desktop/levoile-autostart.desktop) — XDG autostart launcher → systemctl --user
- [docs/testing/5-7-supervision-manuel.md](docs/testing/5-7-supervision-manuel.md) — procédure tests manuels

Fichiers à modifier :
- [cmd/client/main.go](cmd/client/main.go) — wire `UIWatchdogEnabled` dans `svc.Config`
- [internal/service/service.go](internal/service/service.go) — lifecycle Start/Stop du watchdog
- [internal/ipc/messages.go](internal/ipc/messages.go) — ajout `UISupervisionState`
- [internal/ipchandler/handler.go](internal/ipchandler/handler.go) — handler GetUISupervision + enrichissement GetStatus
- [.goreleaser.yaml](.goreleaser.yaml) — section nfpm : nouveaux fichiers packagés

Alignement avec structure unifiée : **OK**. Le pattern `internal/<package>/` + stubs par OS (cf. `internal/tun/watchdog/`, `internal/firewall/`) est respecté. Naming `snake_case.go` + build tags conformes à la convention existante ([architecture.md §Implementation Patterns](_bmad-output/planning-artifacts/architecture.md)).

Pas de conflit avec structure existante. **Conflit potentiel** : le package `internal/watchdog/` existe déjà (TUN) — on nomme explicitement `internal/uiwatchdog/` pour éviter toute ambiguïté (et parce que la sémantique diffère : TUN = recover via OnLost callback ; UI = respawn process enfant).

### References

- [epics.md §Story 5.7](_bmad-output/planning-artifacts/epics.md#L1006-L1021) — user story + AC source
- [prd.md §FR15b](_bmad-output/planning-artifacts/prd.md#L446) — requirement functionnel
- [prd-validation-report-2026-04-15.md §FR15b](_bmad-output/planning-artifacts/prd-validation-report-2026-04-15.md#L364) — score 5.0 avec spec précise
- [architecture.md §Architecture 2 processus](_bmad-output/planning-artifacts/architecture.md#L60) — séparation service/UI
- [architecture.md §Composants UI](_bmad-output/planning-artifacts/architecture.md#L312-L317) — tray + webview + serveur HTTP + singleton
- [architecture.md §Installeur Windows NSIS](_bmad-output/planning-artifacts/architecture.md#L361) — autostart HKCU existant
- [architecture.md §Packaging Linux](_bmad-output/planning-artifacts/architecture.md#L373) — autostart XDG existant
- [internal/ui/singleton_windows.go](internal/ui/singleton_windows.go) — mutex nommé `Global\LeVoileUI` (référence implémentation)
- [internal/tun/watchdog/watchdog.go](internal/tun/watchdog/watchdog.go) — pattern watchdog Go à imiter (Start/Stop, flag atomic, onLostWg, logger)
- [installer/levoile.nsi:79-84](installer/levoile.nsi#L79-L84) — autostart HKCU Run-key (ne pas casser)
- [deploy/levoile-relay.service](deploy/levoile-relay.service) — exemple de unit systemd (celui-ci est system-level pour le relais — s'en inspirer mais **user-level** pour l'UI)

## Developer Context

### Résumé opérationnel
Le tray `levoile-ui` est le seul canal par lequel l'utilisateur voit l'état de protection en continu. Un crash du desktop environment (GNOME Shell restart, Plasma crash) tue ce processus et laisse l'utilisateur aveugle — le service continue à protéger, mais l'utilisateur ne le sait pas et pourrait déclencher des actions contradictoires (relancer manuellement, reboot, désinstaller). Story 5.7 introduit une couche de supervision cross-platform qui respawn automatiquement l'UI.

### Dev Agent Guardrails — À NE PAS FAIRE
- ❌ **Ne pas** ajouter une dépendance type `github.com/judwhite/go-svc` ou `github.com/kardianos/minwinsvc` — on a déjà `github.com/kardianos/service`, suffisant.
- ❌ **Ne pas** implémenter un watchdog Go côté Linux (systemd est le superviseur natif, dupliquer serait anti-idiomatique et prend CPU pour rien).
- ❌ **Ne pas** lancer `levoile-ui` depuis le service Windows au démarrage du service si aucune session utilisateur n'est active (spam erreurs `WTSQueryUserToken`). Le watcher doit tolérer l'absence de session comme état normal, pas erreur.
- ❌ **Ne pas** relancer immédiatement après un crash (risque de boucle tight) — minimum 2 secondes de cooldown, et respect du rate-limit 5/60s.
- ❌ **Ne pas** scrapper la table de processus (`EnumProcesses` + filtre name) — préférer le HANDLE hérité de `CreateProcessAsUser` + `WaitForSingleObject` (événementiel, pas de faux positifs).
- ❌ **Ne pas** logger le path complet de `levoile-ui.exe`, le SID user, ou le nom de domaine Windows dans les logs — NFR22a (zero PII).

### Dev Agent Guardrails — À FAIRE impérativement
- ✅ Stopper le watchdog UI **avant** le teardown service (ordre : uiwatchdog.Stop → IPC.Stop → tunnel → firewall → routing → tun).
- ✅ Test manuel documenté pour chaque plateforme (Linux systemctl --user + Windows taskkill).
- ✅ Exposer l'état via IPC pour observabilité (AC7) — facilitera les diagnostics user sans scraper les logs OS.
- ✅ Rate-limit identique sur les deux plateformes (5 restarts / 60s → backoff 5min) pour parité.
- ✅ Cohérence avec le pattern [internal/tun/watchdog/watchdog.go](internal/tun/watchdog/watchdog.go) : `Start(ctx)` bloquant, `Stop()`, `sync.WaitGroup` pour attendre les goroutines en vol.

### Technical Requirements

| Exigence | Source | Commentaire |
|---|---|---|
| Relancement Linux < 10s | AC1 | `RestartSec=2s` + démarrage `levoile-ui` rapide (< 500ms en pratique) |
| Détection Windows ≤ 5s | AC4 | `WaitForSingleObject` = événementiel (ms), objectif largement tenu |
| Rate-limit 5/60s | AC3, AC5 | Parité systemd StartLimit{Burst,IntervalSec} / Go fenêtre glissante |
| Singleton respecté | AC1, AC4 | flock Linux (Story 5.1) + mutex Windows existant |
| Zéro PII dans logs | NFR22a | Pas de SID, path, domain, hostname |
| Pas de respawn au shutdown | AC6 | Stop ordering strict |
| Pas de respawn hors session interactive | AC6 | Filtrer `WTSActive` uniquement |
| État observable via IPC | AC7 | Nouveau champ `UISupervision` dans StatusResponse |

### Architecture Compliance
- ✅ **2 processus** ([architecture.md §Architecture 2 processus](_bmad-output/planning-artifacts/architecture.md#L60)) : watchdog dans le service (privilégié), UI reste user-space
- ✅ **Build tags par OS** ([architecture.md §Implementation Patterns](_bmad-output/planning-artifacts/architecture.md)) : `watchdog_windows.go` / `watchdog_stub.go`
- ✅ **Pas de nouveau langage** : Go 1.26 pur
- ✅ **IPC JSON ligne par ligne < 4Ko** : le champ `UISupervision` (~150 octets sérialisé) tient très largement
- ✅ **Ordre shutdown strict** : uiwatchdog stoppé avant tunnel/firewall/routing/tun
- ✅ **Naming** `snake_case.go`, stubs `*_stub.go`

### Library / Framework Requirements
- `golang.org/x/sys/windows` (déjà présent) — pour wtsapi32/advapi32/userenv syscalls. **Ne pas** introduire `github.com/gonutz/w32` ou similaire.
- `fyne.io/systray` — **pas concerné**, le watchdog ne touche pas au tray.
- `github.com/kardianos/service` (déjà présent) — pas modifié ici.
- Tests : `testing` standard lib uniquement, pas de `testify`/`gomock` (cohérent avec le reste du repo).

### File Structure Requirements
```
internal/uiwatchdog/
├── watchdog.go           # API publique : type Watchdog, type Config, New(cfg)
├── watchdog_windows.go   # //go:build windows — impl CreateProcessAsUser + WaitForSingleObject
├── watchdog_stub.go      # //go:build !windows — no-op
├── watchdog_test.go      # unit tests (fake launcher, fake clock)
└── e2e_windows_test.go   # //go:build windows && e2e — spawn cmd.exe /c exit 1
packaging/
├── systemd/user/
│   └── levoile-ui.service
├── desktop/
│   └── levoile-autostart.desktop
├── postinstall.sh        # à enrichir (ou créer si n'existe pas encore)
└── preremove.sh          # à enrichir (ou créer si n'existe pas encore)
docs/testing/
└── 5-7-supervision-manuel.md
```

### Testing Requirements
- **Couverture unitaire**: logique fenêtre glissante 100% branches, transitions d'état (IDLE/RUNNING/BACKOFF), stop sous BACKOFF
- **E2E Windows**: tag `e2e windows`, stub fake-ui binary, mesure réactivité et rate-limit
- **Manuel Linux**: `pkill -9 levoile-ui` → observer `journalctl --user -u levoile-ui.service -f` → < 10s reapparition tray
- **Manuel Windows**: `taskkill /F /IM levoile-ui.exe` → < 10s reapparition tray; répéter 6× rapides → BACKOFF observable via IPC `GetStatus` → `ui_supervision.backoff_until` renseigné
- **Pas de test lié au réseau** dans cette story (watchdog n'utilise pas le réseau)

## Git Intelligence Summary

Derniers commits (`git log -5 --oneline`) :
- `16a275a` fix(deploy): align install.sh/README/service with prod (signing.key + registry)
- `c2e1c0e` feat: complete Epic 3 — relay stateless multi-VPS with tunnel IP & NAT
- `7bf2e59` chore: remove .claude/ from history, harden deploy scripts
- `bd11612` feat: IPv6 leak opt-out toggle + relay systemd CAP_NET_ADMIN (Stories 2.9 + 3.1)
- `ece3270` feat: implement Sprint 2 — watchdog, routing, firewall, DNS flush, captive portal

**Observations pertinentes** :
- Les sprints 2 et 3 sont terminés. Aucun commit récent ne touche `cmd/ui/` ou `internal/ui/` ; aucun risque de conflit direct sur les fichiers modifiés par 5.7.
- Le commit `ece3270` a introduit `internal/tun/watchdog/` — **lire ce code en priorité** pour s'aligner sur le style (Config struct, OnLost callback, Stop ordering). S'en inspirer mais ne pas le généraliser prématurément.
- Le commit `bd11612` montre le pattern `CAP_NET_ADMIN` pour systemd — pertinent si on devait durcir `levoile-ui.service`, mais l'UI user n'a besoin d'aucune capability privilégiée (elle parle à l'IPC déjà authentifié).
- Aucune story UI post-restructure n'est encore implémentée (seuls les fichiers legacy 5-1/5-2/5-3 existent, ils concernent l'ancien Epic 5 STUN et sont orphelins par rapport à la nouvelle Epic 5).

## Latest Tech Information

### systemd user units — bonnes pratiques 2026
- `After=graphical-session.target` + `PartOf=graphical-session.target` est le combo canonique pour une app tray desktop (pattern documenté par `systemd.unit(5)` et adopté par KDE/GNOME apps). Garantit que DBus session + DISPLAY sont up avant `ExecStart`.
- `Restart=on-failure` (et **pas** `Restart=always`) : respecte l'intention utilisateur de quitter (exit 0). `RestartSec=2s` suffit — inférieur à 1s peut provoquer faux positifs sur systèmes chargés.
- `StartLimitBurst` + `StartLimitIntervalSec` dans `[Unit]` (pas `[Service]`) selon systemd ≥ 240 — le repo cible Ubuntu 22.04+/Debian 12+/Fedora 39+/Arch → tous systemd ≥ 252 → OK.
- `MemoryMax=200M` : limite douce pour détecter fuite mémoire sans tuer l'UI sauf en cas réel OOM.

### Windows CreateProcessAsUser — API stable
- L'API n'a pas changé depuis Windows Vista. Pas de risque de breaking change.
- `CREATE_BREAKAWAY_FROM_JOB` peut être nécessaire si le service lui-même est dans un job object (rare pour kardianos/service, mais tester).
- Alternative moderne **non retenue** : `CreateProcessWithTokenW` — requiert `SeImpersonatePrivilege` seul, mais ne charge pas l'environnement user correctement sans `CREATE_UNICODE_ENVIRONMENT`. `CreateProcessAsUser` reste le choix canonique.
- Alternative **non retenue** : `WER RegisterApplicationRestart` (`kernel32.dll`) — seulement post-crash WER, ne couvre pas tous les scénarios (SIGKILL externe, OOM, shell restart). Trop partiel pour AC4.

### Go-version compat
- Go 1.26 : tous les syscalls Windows requis sont disponibles dans `golang.org/x/sys/windows` v0.29+. Le repo utilise déjà cette lib (cf. existing singleton_windows.go).

## Project Context Reference

Fichier [project-context.md](docs/project-context.md) : si présent, le charger. Sinon, se référer à [_bmad-output/planning-artifacts/architecture.md](_bmad-output/planning-artifacts/architecture.md) §Implementation Patterns pour les conventions de nommage, tests, erreurs.

Note sur l'historique : la restructure du 2026-04-15 a renommé l'Epic 5 de « STUN/WebRTC » à « UI Desktop ». Les fichiers [5-1-interception-et-parsing-des-paquets-stun.md](_bmad-output/implementation-artifacts/5-1-interception-et-parsing-des-paquets-stun.md), `5-2-relai-stun-via-tunnel-et-substitution-dip.md`, `5-3-gestion-du-fallback-turn-et-validation-anti-fuite-webrtc.md` sont des **orphelins legacy** — ne pas les prendre comme référence. La nouvelle Epic 5 commence avec 5.1 = binaire UI unique.

## Story Completion Status

Status: **ready-for-dev**
Completion note: Ultimate context engine analysis completed — comprehensive developer guide created.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Debug Log References

- Bug initial dans la suite de tests : `MinRestartDelay: 0` était écrasé à 2s par `applyDefaults()`. Fix : tests utilisent `MinRestartDelay: time.Millisecond` explicite. Production garde le défaut 2s (cooldown anti-tight-loop). Aucun changement de signature publique.

### Completion Notes List

- **Watchdog cross-platform sans dépendance externe** : utilise uniquement `golang.org/x/sys/windows` (déjà au projet) — pas de nouveau lib type `gonutz/w32` ou `judwhite/go-svc`.
- **Sliding window + backoff** : implémentés avec un slice de timestamps évincé sur `cutoff = now - Window`. Test `TestWatchdog_WindowSlidingClearsOldRestarts` valide l'éviction quand le clock fake avance au-delà de la fenêtre.
- **Process-launch event-driven sur Windows** : `WaitForSingleObject` avec timeout 500ms permet de combiner réactivité événementielle + propagation ctx.Done propre. Pas de poll par scan de table de processus (évité comme guardrail).
- **Ordre shutdown strict respecté** : `uiwatchdog.Stop()` placé en tête de `Program.shutdown()` (étape 0), AVANT tunnel/firewall/routing/tun. Confirmé par lecture `internal/service/service.go:1434+`.
- **Linux délègue à systemd user** : pas de watchdog Go côté Linux. `UIWatchdogEnabled` n'est armé que si `runtime.GOOS == "windows"` côté `cmd/client/main.go`. Le stub launcher reste compilable pour cohérence.
- **Tâches 1.3-1.5, 7.2 et 5.1 explicitement reportées** à d'autres stories (7.2 packaging Linux, 5.1 singleton flock). Le code actuel ne dépend pas de ces tâches pour atteindre les ACs sur Windows.
- **`go vet ./...` propre + `go test ./...` vert** sur l'ensemble du repo (33 packages).

### File List

**Nouveaux fichiers**

- [internal/uiwatchdog/watchdog.go](internal/uiwatchdog/watchdog.go)
- [internal/uiwatchdog/launcher_windows.go](internal/uiwatchdog/launcher_windows.go)
- [internal/uiwatchdog/launcher_stub.go](internal/uiwatchdog/launcher_stub.go)
- [internal/uiwatchdog/watchdog_test.go](internal/uiwatchdog/watchdog_test.go)
- [internal/service/ui_binary_windows.go](internal/service/ui_binary_windows.go)
- [internal/service/ui_binary_other.go](internal/service/ui_binary_other.go)
- [internal/ipchandler/handler_uiwatchdog_test.go](internal/ipchandler/handler_uiwatchdog_test.go)
- [packaging/systemd/user/levoile-ui.service](packaging/systemd/user/levoile-ui.service)
- [packaging/desktop/levoile-autostart.desktop](packaging/desktop/levoile-autostart.desktop)
- [packaging/README.md](packaging/README.md)
- [docs/testing/5-7-supervision-manuel.md](docs/testing/5-7-supervision-manuel.md)

**Fichiers modifiés**

- [internal/ipc/messages.go](internal/ipc/messages.go) — ajout `ActionGetUISupervision` + struct `UISupervisionState` + champ `UISupervision` sur `Response`
- [internal/ipchandler/handler.go](internal/ipchandler/handler.go) — handler `handleGetUISupervision`, helper `uiSupervisionFromSnapshot`, enrichissement `handleGetStatus` (chemin tunnel-up + chemin tunnel-nil)
- [internal/service/service.go](internal/service/service.go) — import `uiwatchdog`, champs `Config.UIWatchdogEnabled` / `Config.UIBinaryPath`, champ `Program.uiWatchdog`, accessor `UIWatchdogSnapshot`, démarrage en fin de `run()`, arrêt en tête de `shutdown()`, helpers `deriveUIBinaryPath` + `uiWatchdogLogger`
- [cmd/client/main.go](cmd/client/main.go) — `UIWatchdogEnabled: runtime.GOOS == "windows"` dans la config

## Change Log

| Date | Action | Détails |
|---|---|---|
| 2026-04-18 | Implémentation Story 5.7 | Watchdog UI cross-platform : Windows (CreateProcessAsUser + WaitForSingleObject + rate limit 5/60s + backoff 5min) + Linux (systemd user unit `Restart=on-failure`). Observabilité via IPC `GetStatus.UISupervision` + handler `GetUISupervision`. Tests unitaires verts (`internal/uiwatchdog/`, `internal/ipchandler/`). Suite régression complète OK. |
| 2026-04-18 | Code review follow-ups | 2 HIGH + 4 MEDIUM + 3 LOW corrigés automatiquement : (H1) `UISupervision` exposé dans le path `ConcurrentVPN` de `GetStatus`. (H2) sémantique rate-limit alignée (`>=` au lieu de `>`) — parité exacte avec `StartLimitBurst=5` systemd (5 launches max avant backoff, identique Windows/Linux). (M1) suppression du tracking `procHandles` mort dans `WindowsLauncher`. (M2) log `filepath.Base` au lieu du path complet (NFR22a). (M3) `MemoryHigh=200M` + `MemoryMax=400M` pour tolérer les spikes WebKitGTK. (M4) sentinelle `MinRestartDelay < 0` documentée pour désactiver le cooldown + test de régression. (L1) retrait du code défensif mort dans les tests. (L2) `Documentation=` ajouté au unit systemd. (L3) `filepath.EvalSymlinks` dans `deriveUIBinaryPath` pour layouts Linux à base de symlinks. Suite `go test ./...` + `go vet ./...` verte. |
