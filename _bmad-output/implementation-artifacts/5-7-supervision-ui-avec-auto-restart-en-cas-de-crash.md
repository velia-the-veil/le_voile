# Story 5.7: Supervision UI avec auto-restart en cas de crash

Status: ready-for-dev

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

- [ ] **Tâche 1 — Préparer les artefacts de packaging Linux** (AC: #1, #2, #3)
  - [ ] 1.1 Créer le répertoire [packaging/systemd/user/](packaging/systemd/user/) et y déposer `levoile-ui.service` (voir squelette en Dev Notes → §Systemd user unit)
  - [ ] 1.2 Créer [packaging/desktop/levoile-autostart.desktop](packaging/desktop/levoile-autostart.desktop) qui exécute `systemctl --user start levoile-ui.service` (évite de lancer `levoile-ui` en direct — délègue la supervision à systemd)
  - [ ] 1.3 Enrichir [packaging/postinstall.sh](packaging/postinstall.sh) : recharger la config user systemd (`systemctl --user daemon-reload` via loginctl pour chaque utilisateur actif si pratique — sinon documenter que le reload est automatique au prochain login user)
  - [ ] 1.4 Enrichir [packaging/preremove.sh](packaging/preremove.sh) : `systemctl --user stop levoile-ui.service` + `systemctl --user disable levoile-ui.service` pour tout utilisateur actif détecté (loginctl list-users → runuser)
  - [ ] 1.5 Mettre à jour [.goreleaser.yaml](.goreleaser.yaml) (section nfpm) pour inclure les nouveaux fichiers packagés :
    - `packaging/systemd/user/levoile-ui.service` → `/usr/lib/systemd/user/levoile-ui.service` (mode 0644)
    - `packaging/desktop/levoile-autostart.desktop` → `/etc/xdg/autostart/levoile-autostart.desktop` (mode 0644)

- [ ] **Tâche 2 — Créer le package `internal/uiwatchdog/` (service Windows)** (AC: #4, #5, #6, #7)
  - [ ] 2.1 Scaffolder le package : `internal/uiwatchdog/watchdog.go` (API cross-platform), `watchdog_windows.go` (impl), `watchdog_stub.go` (no-op non-Windows, build tag `!windows`)
  - [ ] 2.2 Définir la struct `Watchdog` + `Config{BinaryPath string; PollInterval time.Duration; MaxRestartsInWindow int; Window time.Duration; BackoffDuration time.Duration; Logger Logger}` (s'aligner sur le pattern [internal/tun/watchdog/watchdog.go](internal/tun/watchdog/watchdog.go) : Start(ctx)/Stop, flag atomic recovering, onLostWg)
  - [ ] 2.3 Implémenter `watchdog_windows.go` :
    - Obtenir session interactive via `wtsapi32.WTSEnumerateSessions` + filtrer `WTSActive`
    - Obtenir le token user via `wtsapi32.WTSQueryUserToken(sessionID)`
    - Dupliquer en primary token via `DuplicateTokenEx(TokenPrimary, TOKEN_ALL_ACCESS)`
    - Charger l'environnement user via `userenv.CreateEnvironmentBlock`
    - Lancer le process via `advapi32.CreateProcessAsUserW` avec `CREATE_UNICODE_ENVIRONMENT | CREATE_NO_WINDOW` + `lpDesktop="winsta0\\default"`
    - Conserver le `PROCESS_INFORMATION.hProcess` pour `WaitForSingleObject` en goroutine dédiée (plus réactif qu'un poll)
    - Sur `WAIT_OBJECT_0` (exit) : incrémenter compteur fenêtre glissante, tester rate limit, décider BACKOFF ou relance
  - [ ] 2.4 Stub `watchdog_stub.go` pour Linux/Darwin : `Start` retourne nil immédiatement, `Stop` no-op. Linux utilise systemd user (pas de watchdog Go)
  - [ ] 2.5 Tests unitaires `watchdog_test.go` : injecter un faux `processLauncher` (interface) pour tester la logique fenêtre glissante + backoff sans dépendre de Windows. Tests Windows-only avec `//go:build windows` pour un e2e qui spawn `cmd.exe /c exit 1` en boucle et vérifie rate limit

- [ ] **Tâche 3 — Intégrer le watchdog UI dans le programme service** (AC: #4, #6, #7)
  - [ ] 3.1 Dans [cmd/client/main.go](cmd/client/main.go), ajouter un champ `UIWatchdogEnabled bool` (default true Windows, false Linux) à `svc.Config`
  - [ ] 3.2 Dans [internal/service/service.go](internal/service/service.go) → méthode `Start(s service.Service) error` : construire `uiwatchdog.New(uiwatchdog.Config{BinaryPath: deriveUIBinaryPath(), ...})` après le démarrage IPC, appeler `Start(ctx)` dans une goroutine. Sur `Stop()`, appeler `Stop()` du watchdog **avant** le teardown tunnel/firewall (ordre : uiwatchdog → IPC → tunnel → firewall → routing → tun)
  - [ ] 3.3 Helper `deriveUIBinaryPath()` : filepath.Join(filepath.Dir(servicePath), "levoile-ui.exe") sous Windows, stub ailleurs. Valider via `os.Stat` au boot service et logger WARNING + désactiver watchdog si introuvable
  - [ ] 3.4 Ajouter un handler IPC `GetUISupervision` (action `ui.supervision.get`) qui retourne le snapshot `{enabled, last_restart_at, restart_count_window, backoff_until}` (cf. AC7). Câbler dans [internal/ipchandler/handler.go](internal/ipchandler/handler.go)

- [ ] **Tâche 4 — Exposer l'état dans `GetStatus`** (AC: #7)
  - [ ] 4.1 Étendre la structure `ipc.StatusResponse` dans [internal/ipc/messages.go](internal/ipc/messages.go) : champ `UISupervision *UISupervisionState` (pointeur, nil sur Linux où l'info vient de systemd)
  - [ ] 4.2 Backend : le handler `GetStatus` lit le snapshot du watchdog et le joint à la réponse
  - [ ] 4.3 Frontend (sans UI dédiée dans cette story) : aucun changement — le champ est exposé pour future observabilité. Ajouter un commentaire `// Consumed by diagnostics panel (future story)` dans le type Go

- [ ] **Tâche 5 — Handling singleton cross-process au relancement** (AC: #1, #4)
  - [ ] 5.1 Linux : vérifier que le flock `~/.local/state/levoile/ui.lock` est bien non-bloquant + auto-release à la mort du processus (`flock` natif Linux libère automatiquement, mais valider avec test qui SIGKILL l'ancien puis lance le nouveau < 1s plus tard)
  - [ ] 5.2 Windows : le mutex nommé `Global\LeVoileUI` est détenu par le handle du processus ; kernel le release à la mort du process. Valider : tuer `levoile-ui.exe` via Task Manager, attendre 2s, le watchdog doit relancer et l'`AcquireSingleton` réussit (pas de `ERROR_ALREADY_EXISTS`). Dans le cas contraire (course), le watchdog réessaye après 10s max
  - [ ] 5.3 Ajouter un test manuel documenté dans [docs/testing/](docs/testing/) (fichier `5-7-supervision-manuel.md`) : étapes reproductibles pour valider AC1+AC4 (commande `kill -9`, commande `taskkill /F /IM levoile-ui.exe`, timing attendu, commande de vérification)

- [ ] **Tâche 6 — Observabilité et logs structurés** (AC: #3, #5)
  - [ ] 6.1 Windows : émettre Event Log via `eventlog` package (installé au `service install`) — source `LeVoileService` déjà existante ; niveau WARNING au passage en BACKOFF, INFO à chaque relancement réussi. Format : `"levoile-ui respawned (count=N/window=60s)"` — aucun PID, aucun path utilisateur
  - [ ] 6.2 Linux : systemd journald capte automatiquement la sortie `levoile-ui` — aucun travail côté Go. Documenter la commande utile : `journalctl --user -u levoile-ui.service -n 50`
  - [ ] 6.3 Toutes les erreurs du watchdog (token acquisition failed, CreateProcessAsUser failed) sont loggées niveau ERROR avec code Win32 numérique (`GetLastError`), jamais le nom utilisateur ni le chemin complet (NFR22a)

- [ ] **Tâche 7 — Tests** (AC: all)
  - [ ] 7.1 Tests unitaires Go : logique rate-limit fenêtre glissante (fake clock), transitions IDLE → RUNNING → BACKOFF → IDLE, stop propre en cours de BACKOFF
  - [ ] 7.2 Test e2e Windows `internal/uiwatchdog/e2e_windows_test.go` (build tag `windows && e2e`) : lance un stub `fake-ui.exe` qui exit 1, valide rate-limit après 5 crashes
  - [ ] 7.3 Test manuel Linux documenté : lancer `systemctl --user start levoile-ui.service`, `pkill -9 levoile-ui`, chrono < 10s, ré-émergence du tray
  - [ ] 7.4 Test manuel Windows : logon user + service démarré, `taskkill /F /IM levoile-ui.exe`, chrono < 10s, icône tray revient. Répéter 6 fois rapide → le 6e doit déclencher BACKOFF (tray disparaît 5 min)

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

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
