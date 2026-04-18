# Story 8.1 : Vérification périodique des releases GitHub

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a **utilisateur final de Le Voile**,
I want **que le client vérifie automatiquement la disponibilité de nouvelles versions sans intervention manuelle, télécharge et vérifie la signature en arrière-plan, et m'avertisse via le tray quand une mise à jour est prête à être appliquée**,
so that **je sois toujours sur la dernière version sécurisée sans avoir à surveiller GitHub moi-même, et que je sache exactement quand redémarrer le service pour bénéficier des correctifs**.

> **Contexte sprint (épic 8 — issu du restructuring 2026-04-15)** : la logique périodique de vérification + téléchargement + vérification signature est **déjà implémentée et testée** dans `internal/updater/` (héritée de l'ancien Epic 6 — Windows-stable). Cette story consolide la vérification de conformité avec les nouveaux ACs (rate limit 500 kbps, owner=`velia-the-veil`, repo=`le_voile`, intervalle 6h), comble le gap UI (le nouveau tray cross-platform `internal/ui/ui.go` n'expose actuellement **aucun** retour `update_ready` vers l'utilisateur), et ajoute la commande `levoile-ctl update check` pour déclencher manuellement une vérification.

## Acceptance Criteria

### AC1 — Configuration `[update]` complète et défauts sains
**Given** un fichier `config.toml` neuf (ou absent) au démarrage du service
**When** `internal/config/config.go` charge la config
**Then** la section `[update]` est défaultée à : `enabled = true`, `check_interval = "6h"`, `rate_limit_kbps = 500`, `github_owner = "velia-the-veil"`, `github_repo = "le_voile"`
**And** la valeur `rate_limit_kbps` exposée dans `config.example.toml` est exactement `500` (alignée avec arch lignes 521-526 + epics ligne 1276) — pas la constante développeur `512` historique de `internal/updater/downloader.go:18`
**And** les valeurs sont surchargeables via TOML utilisateur sans casser les tests existants `internal/config/config_test.go`

### AC2 — Boucle périodique tournée par le service
**Given** `[update] enabled = true`
**When** le service démarre via `cmd/client/main.go` → `service.Program.run()` → `internal/service/service.go:1471`
**Then** un `*updater.Updater` est instancié avec `Owner`/`Repo`/`PubKeyBase64`/`StagingDir`/`CheckInterval`/`RateLimitBytesPerSec` issus de la config
**And** `go upd.Start(ctx)` est lancé → délai initial `1 min` (`updater.initialDelay`) puis cycles `CheckAndDownload` toutes les `6h` (`updater.defaultCheckInterval` ou valeur TOML)
**And** chaque cycle a un timeout dur de `10 min` (`updater.cycleTimeout`)
**And** la boucle est interruptible via cancellation du `ctx` parent (vérifié via `internal/updater/updater_test.go`)
**And** un échec réseau planifie un retry à `30 min` ; un échec d'intégrité (checksum/signature) planifie un retry à `1 h` ; après `3` échecs consécutifs, le cycle revient au pas normal (`6h`)

### AC3 — Requête GitHub conforme et parsing release
**Given** un cycle de check démarre
**When** `Checker.CheckLatest(ctx)` exécute
**Then** une requête `GET https://api.github.com/repos/{owner}/{repo}/releases/latest` est émise avec `Accept: application/vnd.github+json` et `User-Agent: LeVoile/{version}`
**And** la réponse JSON est parsée pour extraire `tag_name` (préfixe `v` strippé), `body`, `published_at` et les assets
**And** seuls les assets nommés `le_voile_{GOOS}_{GOARCH}` (avec `.exe` sur Windows), `checksums.txt` et `checksums.txt.sig` sont retenus comme `DownloadURL`/`ChecksumURL`/`SignatureURL`
**And** une release sans l'asset binaire attendu, `checksums.txt` ou `checksums.txt.sig` retourne une erreur explicite (`parse release: no asset ... found`)
**And** la lecture du body est plafonnée à `2 MB` (`maxResponseSize`) pour éviter l'OOM sur réponse forgée

### AC4 — Comparaison sémantique versions (semver, pre-release < stable)
**Given** la version courante du binaire (`-X internal/updater.Version=X.Y.Z` au build) et la version retournée par GitHub
**When** `compareVersions(current, latest)` est appelé
**Then** le préfixe `v` est strippé des deux côtés (`v1.2.3` == `1.2.3`)
**And** la comparaison est numérique segment par segment (major.minor.patch)
**And** une version pré-release (`1.2.3-beta.1`) est considérée **strictement inférieure** à la stable (`1.2.3`)
**And** entre deux pré-releases, la comparaison est lexicographique sur le suffixe
**And** une chaîne malformée se comporte comme `0.0.0` (jamais de panic, validé par `internal/updater/checker_test.go`)
**And** `CheckLatest` retourne `nil, nil` si `compareVersions(current, latest) >= 0` (déjà à jour, aucun téléchargement)

### AC5 — Téléchargement automatique signé en arrière-plan, rate-limité
**Given** une nouvelle version est détectée par `CheckLatest`
**When** `Updater.CheckAndDownload` poursuit
**Then** `Downloader.DownloadRelease` télécharge séquentiellement le binaire, `checksums.txt`, `checksums.txt.sig` dans `StagingDir`
**And** chaque fichier est écrit en `*.tmp` puis renommé atomiquement (mode `0o600` Unix)
**And** le rate limit appliqué est `cfg.UpdateRateLimit` (issu de `update.rate_limit_kbps × 1024`) — défaut effectif `500 × 1024 = 512_000 B/s`
**And** seul `https://github.com/velia-the-veil/le_voile/releases/...` est autorisé (host + path-prefix), redirects vers `objects.githubusercontent.com` tolérés (HTTPS only, max 10 hops)
**And** le téléchargement est plafonné à `500 MB` (`maxDownloadSize`) ; tout dépassement échoue avant épuisement disque
**And** `Verifier.VerifyStagedUpdate(staged)` vérifie immédiatement la signature Ed25519 du `checksums.txt` avec la clé publique embarquée (`updatePubKey()`) avant tout autre traitement — une signature invalide rejette la mise à jour et déclenche le retry `1h` (`isIntegrityError`)
**And** la version vérifiée est persistée via `writeStagedVersion(stagingDir, version)` pour que l'`Installer` au prochain démarrage la retrouve
**And** le callback `onUpdateReady(version)` est invoqué → le service stocke `pendingUpdateVersion` (sous `p.updateMu`)

### AC6 — Anti-replay des versions ayant déjà rollback
**Given** une version `vX.Y.Z` a déjà été appliquée puis rollback (story 8.2) → `failed_version` persisté dans `StagingDir`
**When** `CheckAndDownload` détecte cette même version comme `latest`
**Then** la mise à jour est silencieusement ignorée (`return nil, nil`) — aucun téléchargement, aucun callback
**And** dès qu'une version **différente** est publiée par GitHub, le marker `failed_version` est effacé via `ClearFailedVersion(stagingDir)` et la mise à jour reprend son cours normal
**And** la concurrence de cycles est sérialisée par `cycleMu` (un seul `CheckAndDownload` à la fois)

### AC7 — IPC `update_status` & HTTP `/api/update-status` exposent l'état
**Given** une mise à jour vient d'être téléchargée + vérifiée → `pendingUpdateVersion = "X.Y.Z"`
**When** l'UI envoie une requête `GET /api/update-status` au serveur HTTP local
**Then** le `HTTPServer` proxie via `ipc.ActionUpdateStatus` → `handleUpdateStatus(prg)`
**And** la réponse JSON est `{"status": "update_ready", "version": "X.Y.Z"}`
**And** quand aucune update n'est prête, la réponse est `{"status": "up_to_date"}` (ou `"downloading"` si `Updater.IsDownloading()`)
**And** quand un rollback vient d'avoir lieu, `{"status": "rollback", "rollback_version": "...", "rollback_reason": "..."}` (story 8.2 — comportement déjà câblé `internal/ipchandler/handler.go:476-518`)
**And** la disponibilité de cette API est validée par `internal/ui/httpserver_test.go::TestHandleUpdateStatus_*` (existant — vérifier qu'aucun test ne casse après les ajouts UI)

### AC8 — Notification tray non-intrusive « Mise à jour disponible : v{version} »
**Given** le polling tray (`internal/ui/ui.go::pollLoop`, intervalle 2s) lit une réponse IPC `get_status` portant `UpdateStatus = "update_ready"` + `UpdateVersion = "X.Y.Z"`
**When** `updateTrayState(resp)` est appelée
**Then** une nouvelle entrée de menu tray apparaît (au-dessus de « Mode dégradé ») : `Mise à jour disponible : v{X.Y.Z}` avec tooltip `Téléchargée et vérifiée — sera appliquée au prochain redémarrage du service`
**And** cette entrée n'est créée **qu'une seule fois par version** (idempotent : si la version annoncée est identique à `lastShownUpdateVersion`, on ne crée rien — pattern `last` déjà utilisé pour l'icône, `internal/ui/ui.go:83`)
**And** un clic sur cette entrée ouvre la fenêtre webview (réveil) et déclenche un événement UI `update_available` consommable par le frontend via `/api/ui-event`
**And** quand l'IPC arrête de retourner `update_ready` (cas rare : version installée, rollback purgé) l'entrée est retirée
**And** aucune popup OS-level (`balloon notification`, libnotify, etc.) n'est utilisée — exigence « non-intrusive » (cf. PRD UX)
**And** sur Linux (build tag `linux`) comme sur Windows (build tag `windows`), l'API `SystrayMenuAPI.AddMenuItem` reste l'unique surface — pas de dépendance OS supplémentaire

### AC9 — Bandeau webview discret « Mise à jour prête » (frontend)
**Given** la fenêtre webview est ouverte et l'utilisateur navigue dans l'UI
**When** le frontend poll `/api/update-status` (toutes les 5s, dans `frontend/src` au même rythme que `/api/status`)
**Then** si `status === "update_ready"`, un bandeau bleu pâle (non-bloquant, hauteur ≤ 32px, classe `.update-ready-banner`) s'affiche en bas de fenêtre : `Mise à jour v{version} prête — redémarrez le service pour l'appliquer`
**And** un lien « Plus tard » masque le bandeau pour la session courante (mémoire en RAM frontend uniquement, **pas** de localStorage — voir `feedback_ui_prefs_pattern.md`)
**And** si `status === "rollback"`, un bandeau orange équivalent apparaît avec `rollback_reason` (préfixe `Mise à jour échouée — `) — comportement déjà spécifié dans story 8.2, mais le composant doit déjà être prêt
**And** aucun appel n'est fait depuis le frontend vers GitHub directement — toujours via `/api/update-status` (sécurité : pas de leak réseau hors tunnel)

### AC10 — Commande `levoile-ctl update check` (déclenchement manuel)
**Given** un utilisateur sur Linux avec `sudo levoile-ctl update check` ou Windows admin avec `levoile-ctl.exe update check`
**When** la commande exécute
**Then** elle se connecte au socket IPC (`/run/levoile/ipc.sock` Linux ou `\\.\pipe\levoile` Windows) avec authentification token (pattern story 5.9 — `crypto/subtle.ConstantTimeCompare`)
**And** envoie `ipc.Request{Action: ipc.ActionCheckUpdate, Auth: <token>}`
**And** le service exécute `Updater.CheckAndDownload(ctx)` synchrone (timeout `cycleTimeout = 10 min`)
**And** la réponse est `{status: "ok", update_status: "update_ready", update_version: "X.Y.Z"}` si une nouvelle version est trouvée, `{status: "ok", update_status: "up_to_date"}` sinon, `{status: "error", error: "<msg>"}` en cas d'échec
**And** la sortie CLI est concise : `mise à jour disponible : v{version} (téléchargée + vérifiée, prête au prochain redémarrage)` ou `déjà à jour (v{current})` ou `erreur : <msg>` ; code retour `0` succès, `1` échec ou `2` updates désactivées
**And** un test dans `cmd/ctl/main_test.go` couvre les trois branches (succès update / déjà à jour / échec)

### AC11 — Logs syslog/Event Log structurés sans données utilisateur
**Given** chaque transition critique de la boucle updater (cycle start, CheckLatest result, download start/end, verify result, rollback)
**When** un évènement survient
**Then** un log structuré est émis vers `serviceStderr` (déjà piped vers syslog/Event Log par `kardianos/service`) au format : `service: updater: <action> [version=<X.Y.Z>] [duration=<ms>] [err=<msg>]`
**And** aucun log ne contient : IP utilisateur, chemin home, nom d'utilisateur, contenu de fichiers téléchargés (NFR22a — observabilité minimale, conformité zero-log)
**And** les niveaux respectent : info pour cycle nominal, warning pour retry, error pour rejet signature/checksum

### AC12 — Conformité « updates désactivées »
**Given** `[update] enabled = false` dans la config
**When** le service démarre
**Then** **aucun** `Updater` n'est instancié (vérifié par garde `if p.config.UpdateEnabled && p.config.UpdateStagingDir != ""` ligne 1471)
**And** `handleCheckUpdate` retourne `{status: "error", error: "updates_disabled"}`
**And** `handleUpdateStatus` retourne le même `updates_disabled` si aucun rollback/installed récent à reporter
**And** la commande `levoile-ctl update check` retourne `code 2` avec message `updates désactivées dans config.toml`
**And** aucun log « updater: cycle start » n'apparaît dans syslog (silence complet)

## Tasks / Subtasks

> **Convention** : chaque task identifie d'abord ce qui **EXISTE DÉJÀ** (juste à valider) vs ce qui est **NOUVEAU** (à coder). Les tâches NOUVELLES sont préfixées `[NEW]`.

### Task 1 — Aligner les défauts de configuration (AC1)
- [x] Vérifier `internal/config/config.go:143-149` : `Update.RateLimitKBps = 512` → **changer à `500`** pour s'aligner avec arch + epics + `config.example.toml`
- [x] Mettre à jour `internal/config/config_test.go` (test des défauts) en conséquence
- [x] Vérifier que `config.example.toml` (à compléter — actuellement absent de la section `[update]`, voir grep ligne 27 fichier exemple) reçoit la section :
  ```toml
  [update]
  enabled = true
  github_owner = "velia-the-veil"
  github_repo = "le_voile"
  check_interval = "6h"
  rate_limit_kbps = 500
  ```
- [x] Diff repo vs prod (`/etc/levoile/config.toml` sur les 8 relais — voir `reference_relay_servers.md`) **AVANT** tout `scp` pour éviter une régression de drift (cf. `feedback_diff_before_deploy.md`) — note : les relais ne portent **pas** la section `[update]` (client-only), donc rien à scp ; valider mentalement que le scope est purement client.

### Task 2 — Valider la boucle périodique existante (AC2, AC3, AC4)
- [x] Lire et valider `internal/updater/updater.go:93-140` (Start loop) — comportement timeouts/retries conforme aux AC2
- [x] Lire et valider `internal/updater/checker.go:48-84` (CheckLatest) — endpoint, headers, parsing conformes AC3
- [x] Lire et valider `internal/updater/checker.go:155-205` (compareVersions) — semver + pre-release conformes AC4
- [x] Run `go test ./internal/updater/... -run TestChecker` et `go test ./internal/updater/... -run TestCompareVersions` — tous verts
- [x] **Aucun nouveau code attendu** — purement vérification

### Task 3 — Valider le téléchargement signé + anti-replay (AC5, AC6)
- [x] Lire et valider `internal/updater/downloader.go:48-181` — rate limit, allowed hosts, redirects, atomic write
- [x] Lire et valider `internal/updater/verify.go` (signature Ed25519) — vérifier emploi de `crypto/subtle.ConstantTimeCompare`
- [x] Lire et valider `internal/updater/updater.go:145-199` (CheckAndDownload) — failed_version + cycleMu sérialisation
- [x] Run `go test ./internal/updater/... -run TestDownloader` et `go test ./internal/updater/... -run TestUpdater_CheckAndDownload` — tous verts
- [x] **Aucun nouveau code attendu** — purement vérification

### Task 4 — Valider l'intégration service + IPC + HTTP (AC7, AC12)
- [x] Lire et valider `internal/service/service.go:1471-1490` (init updater, OnUpdateReady, pendingUpdateVersion)
- [x] Lire et valider `internal/ipchandler/handler.go:96-99,238-241,457-518` (handleCheckUpdate, handleUpdateStatus, get_status enrichi)
- [x] Lire et valider `internal/ui/httpserver.go:96, 351-376` (route + APIUpdateStatusResponse)
- [x] Run `go test ./internal/ipchandler/... -run TestHandle.*Update.*` et `go test ./internal/ui/... -run TestHandleUpdateStatus.*` — tous verts
- [x] Vérifier comportement `updates_disabled` (couvert par `TestHandle_UpdateStatus_NoUpdater` + `TestHandle_CheckUpdate_NoUpdater`)
- [x] **Aucun nouveau code attendu** — purement vérification

### Task 5 — `[NEW]` Notification tray « Mise à jour disponible » (AC8)
- [x] Ajouter dans `internal/ui/ui.go::UI` un champ `menuUpdate updateMenuController` (interface — *systray.MenuItem la satisfait nativement) et `lastShownUpdateVersion string` (sous `mu`)
- [x] Dans `setupMenu()` (autour ligne 160), créer le menu item `menuUpdate` **caché par défaut** (via `systray.MenuItem.Hide()`) ; positionnement : entre `menuOpen` et `menuKillSwitch`
- [x] Dans `updateTrayState(resp ipc.Response)`, ajouter `applyUpdateMenu(resp)` en TOUT premier appel (avant le debounce stateKey) — garantit que la visibilité du menu suit `UpdateStatus/Version` même si tout le reste de l'état est stable
- [x] Dans la goroutine `menuHandler`, ajouter un cas `<-updateCh` (avec sentinel `neverFiresChan` quand `menuUpdateClicked` est nil — pattern story 5.9) qui : (a) ouvre/réveille la webview via `handleOpenWebview()`, (b) appelle `httpServer.TriggerUIEvent("update_available")`
- [x] Ajouter 5 tests `internal/ui/ui_test.go::TestUpdateTrayState_UpdateAvailable_*` couvrant : (i) première détection ; (ii) repolling identique idempotent ; (iii) status clear → masqué + reset ; (iv) bump de version → refresh ; (v) menu nil safe (no-op)
- [x] **Pas de notification OS-level** — exigence non-intrusive
- [x] Pattern `Hide()/Show()` cross-platform — utilisé tel quel par story 5.9 sans problème

### Task 6 — `[NEW]` Bandeau webview « Mise à jour prête » (AC9)
- [x] Dans `frontend/src/app.js`, ajout d'un poller dédié `startUpdateStatusPolling()` (intervalle 5 s, `pollUpdateStatus()`) wired depuis `init()`
- [x] Logique `renderUpdateBanner(data)` : si `status === "update_ready"` ET `version !== sessionDismissedUpdateVersion` → bandeau bleu pâle ; sinon caché
- [x] Lien `#update-dismiss` câblé via `dismissUpdateBanner(event)` qui set `sessionDismissedUpdateVersion = lastSeenUpdateVersion` (variable JS in-memory uniquement — pas de localStorage, pas de POST `/api/ui-prefs`)
- [x] Variante `status === "rollback"` rendue avec couleur orange (`.update-ready-banner.rollback`) et `data.rollback_reason` — composant prêt pour story 8.2
- [x] CSS dans `frontend/src/style.css` (`.update-ready-banner` + `.update-ready-banner.rollback`) avec `position: fixed; bottom: 0` et hauteur ≤ 32 px
- [x] Aucun appel direct vers GitHub depuis le frontend — uniquement `/api/update-status` (verrouillé par contract test `TestAppJSContract_Story81`)
- [x] Tray click `update_available` (Story 8.1 AC8) reset le dismiss session et force `pollUpdateStatus()` immédiat

### Task 7 — `[NEW]` Commande `levoile-ctl update check` (AC10)
- [x] Lecture de `cmd/ctl/main.go` — pattern dispatch (killswitch / status / trigger-recovery) bien établi
- [x] Sous-commande `update` ajoutée avec verbe unique `check` (futurs `status`/`apply` pour story 8.2)
- [x] Implémentée via `sendIPCWithTimeout(ipc.Request{Action: ipc.ActionCheckUpdate, Auth: ctlauth.Hex(token)}, 5*time.Minute, stderr)` — la marge 5 min couvre largement le timeout serveur 2 min de `handleCheckUpdate`
- [x] Codes retour : `0` succès (update_ready ou up_to_date), `1` échec réseau/IPC/signature, `2` (`exitDisabled`) pour `updates_disabled`
- [x] Sortie FR conforme : « mise à jour disponible : vX.Y.Z (téléchargée + vérifiée, prête au prochain redémarrage) » ou « déjà à jour » ou « mises à jour désactivées dans config.toml »
- [x] 6 tests dans `cmd/ctl/main_test.go::TestRun_Update_*` couvrant : missing verb, unknown verb, update ready (avec assert auth + version), up to date, disabled (exit 2), failure (exit 1), missing token
- [x] Usage texte dans `printUsage` mis à jour avec la ligne `update check`

### Task 8 — Logs structurés (AC11)
- [x] Audit : aucun log côté `Updater.Start`/`CheckAndDownload` avant cette story
- [x] Ajout d'un champ `Logger io.Writer` à `UpdaterConfig` (default `io.Discard` quand nil pour préserver compat)
- [x] Helper `logf(format, args...)` émettant `service: updater: <action> ...\n` ; appelé sur cycle start, check failed, up to date, skip rollback-marked, download start/done/failed, verify ok/failed, persist staged-version err, update ready (avec `version=` et `duration_ms=`)
- [x] Service `internal/service/service.go` injecte `serviceStderr` (déjà piped vers syslog/Event Log via kardianos)
- [x] Vérification anti-PII : `internal/updater/logging_test.go::TestUpdater_Logging_NoSensitiveData` regex-check sur 6 patterns interdits (IPv4, /home, /Users, C:\Users, %USERPROFILE%, MAC) → aucun match
- [x] `TestUpdater_Logging_NilLoggerIsSafe` : appel sans Logger ne panique pas

### Task 9 — Cross-platform sanity (Linux + Windows builds)
- [x] `go build ./...` Windows (host) — succès
- [x] `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./internal/updater/... ./internal/config/... ./internal/ipchandler/... ./cmd/ctl/... ./frontend/...` — succès (modules touchés, sans CGO)
- [x] `cmd/ui` reste limité à un host avec libwebkit2gtk pour le build CGO Linux — non bloquant pour 8.1 puisque la stack tray + serveur HTTP UI a été validée par les tests `internal/ui/...`
- [x] Suite complète sur les modules touchés : `config`, `updater`, `ipchandler`, `ui`, `service`, `ctl`, `frontend` — **tous verts**
- [x] 2 échecs Windows-host dans `internal/firewall` (`TestIntegrationWFP_*` — requiert privilèges Admin) et 1 flake `internal/relay/TestServer_RejectsNonCFSource_TCPListener` (passe en réexécution) → **pré-existants, hors scope story 8.1** (aucun fichier de ces packages modifié)

### Task 10 — Mise à jour du sprint status
- [x] Sprint status passé à `in-progress` au démarrage du dev, basculé à `review` à la fin
- [x] Epic 8 reste `in-progress` (story 8.2 est en `review`)

## Dev Notes

### Architecture & emplacements (à respecter strictement)
- **Updater core** : `internal/updater/{updater,checker,downloader,verify,version}.go` — **ne pas relocaliser, ne pas refactor**, héritage Windows-stable consolidé
- **Wiring service** : `internal/service/service.go` (zone `--- 7. Updater start ---`) — patterns : `p.updateMu` pour `pendingUpdateVersion`, `p.updatePubKey()` pour la clé Ed25519 embarquée
- **IPC handlers** : `internal/ipchandler/handler.go` (`handleCheckUpdate` ligne 457, `handleUpdateStatus` ligne 476)
- **HTTP route** : `internal/ui/httpserver.go` (`handleUpdateStatus` ligne 361, registered ligne 96)
- **Tray UI** : `internal/ui/ui.go` (`pollLoop`, `updateTrayState`, `setupMenu`)
- **Frontend** : `frontend/src/` + `frontend/index.html` (charte plateformeliberte.fr, polling existant)
- **CLI ctl** : `cmd/ctl/main.go` — pattern token machine-local (story 5.9)

### Patterns établis à réutiliser
- **Polling tray** (`pollInterval = 2 * time.Second`, `internal/ui/ui.go:17`) : déjà en place, ajouter le check `update_ready` dans `updateTrayState` SANS introduire un nouveau ticker
- **`last` pattern** (`internal/ui/ui.go:83`) pour éviter les SetTitle/SetIcon redondants : appliquer à `lastShownUpdateVersion`
- **Idempotence menu items** : `fyne.io/systray.MenuItem.Show()/Hide()` est l'API canonique — testé avec story 5.9 (`menuKillSwitch`)
- **Sérialisation actions sensibles** : `cycleMu` (updater) et `updateMu` (service) sont déjà en place, ne pas en créer d'autres
- **Auth CLI** : `crypto/subtle.ConstantTimeCompare` + token dans `Request.Auth` (NFR9c) — pattern story 5.9 (`internal/ipchandler/handler.go::handleSetKillSwitchMode`)
- **CSRF token** : déjà géré par `internal/ui/httpserver.go::handleCSRFToken` ; `/api/update-status` est GET donc pas de CSRF nécessaire
- **Logs sans données utilisateur** : NFR22a — préfixer toujours `service: updater:`, utiliser `filepath.Base(path)` pour les chemins

### Pièges à éviter (LLM common mistakes)
1. **Ne pas réinventer un client GitHub** : `Checker.CheckLatest` existe et est testé. Réutiliser `httpClient` 30s timeout, headers `Accept` + `User-Agent`
2. **Ne pas créer de `time.Ticker` parallèle** dans le tray : le polling 2s existant suffit. Un ticker 5s côté frontend (AC9) est OK, c'est un autre processus
3. **Ne pas exposer `POST /api/check-update`** dans cette story : non listé dans les ACs ; arch ligne 488 le mentionne mais c'est explicitement pour story 8.2 ou plus tard. Le déclenchement manuel passe par `levoile-ctl` (AC10)
4. **Ne pas utiliser localStorage côté frontend** pour mémoriser le dismiss du bandeau — voir `feedback_ui_prefs_pattern.md`. Variable JS in-memory uniquement (re-affichée à chaque ouverture de la fenêtre — comportement attendu : l'utilisateur DOIT être rappelé tant qu'il n'a pas redémarré le service)
5. **Ne pas notifier via libnotify/balloon Windows** : exigence « non-intrusive » de la PRD. Le menu tray + le bandeau webview suffisent
6. **Ne pas appeler GitHub depuis le frontend** : tout passe par le service (sécurité + cohérence du tunnel — capture L3 garantit que même un appel frontend GitHub passerait par le tunnel, mais on évite la complexité)
7. **Ne pas changer la valeur `defaultRateLimitBytesPerSec = 512 * 1024` dans `downloader.go`** : c'est le fallback si la config n'est pas lue. Le fix est dans `config.go` (défaut TOML `500`) qui est multiplié par 1024 et passé via `cfg.UpdateRateLimit`
8. **Pas de localisation EN du bandeau ou du menu** — UI 100% FR (PRD)
9. **Pas de modification de la signature publique** : la clé Ed25519 (`updatePubKey()`) est figée par le packaging. Touch zero
10. **Pas de hook nouveau au shutdown** : `Updater.Start` est interruptible via `ctx.Done()`, déjà géré par `service.run`

### Source tree components à toucher
- `internal/config/config.go` (défaut RateLimitKBps : 512 → 500)
- `internal/config/config_test.go` (assertion défauts)
- `config.example.toml` (ajout section `[update]`)
- `internal/ui/ui.go` (menu tray + lastShownUpdateVersion)
- `internal/ui/ui_test.go` (TestUpdateTrayState_*)
- `frontend/src/<fichier polling>` + `frontend/index.html` ou CSS pour le bandeau
- `cmd/ctl/main.go` (sous-commande `update check`)
- `cmd/ctl/main_test.go` (tests update check)
- `internal/updater/updater.go` (option Logger optionnelle, AC11)
- `internal/updater/updater_test.go` (TestUpdaterLogging_NoSensitiveData)

### Standards de test
- **Couverture cible** : updater core ≥ 80% (déjà atteint actuellement), nouveau code UI ≥ 70%
- **Pas de mock du Downloader.Download HTTPS** — utiliser `httptest.NewTLSServer` avec un certif auto-signé et override `AllowedDownloadHost` via build tag de test (pattern existant `checker_test.go`)
- **Tests parallèles OK** sauf pour les tests qui touchent `failed_version` (fichier disque partagé) — utiliser `t.TempDir()` systématiquement
- **Pas de réseau réel** dans les tests — tous fixtures local (`testdata/release.json`, etc.)

### Project Structure Notes

- Alignement total avec `architecture.md` lignes 261, 337, 487-488, 521-526, 851-853 (section updater unchangée + restart systemctl Linux)
- Variance mineure : la **section `[update]` n'est actuellement PAS présente** dans `config.example.toml` — c'est un oubli, pas une décision d'archi. À combler dans Task 1
- Le défaut `RateLimitKBps = 512` côté Go (`config.go:146`) **diverge** des 500 mentionnés dans arch + epics — un alignement vers 500 est sans risque (test downloader passe avec n'importe quelle valeur > 0)
- Le menu tray `Mise à jour disponible` est un ajout **pur** — aucun conflit avec stories 5.x (qui ont introduit `menuOpen`, `menuKillSwitch`, `menuQuit`)
- Aucune modification du protocole IPC requise — toutes les actions/champs nécessaires sont déjà déclarés (`internal/ipc/messages.go:21-23,69-75,94-99`)

### References

- Source AC + user story : [Source: _bmad-output/planning-artifacts/epics.md#Story-8.1] (lignes 1259-1276)
- Architecture updater : [Source: _bmad-output/planning-artifacts/architecture.md#Auto-update] (lignes 261, 337, 487-488, 521-526, 851-853)
- PRD FR35 : [Source: _bmad-output/planning-artifacts/prd.md] (NFR9c signature Ed25519, NFR22a logs sans PII)
- Pattern story précédente (kill-switch tray + CLI) : [Source: _bmad-output/implementation-artifacts/5-9-mode-degrade-kill-switch-indicateur-visuel-permanent.md]
- Config relais (référence diff) : `reference_relay_servers.md` (memory)
- UI prefs anti-pattern : `feedback_ui_prefs_pattern.md` (memory) — pas de localStorage
- Code base critique : [internal/updater/updater.go:93-199](internal/updater/updater.go#L93-L199), [internal/updater/checker.go:48-205](internal/updater/checker.go#L48-L205), [internal/updater/downloader.go:48-181](internal/updater/downloader.go#L48-L181), [internal/service/service.go:1471-1490](internal/service/service.go#L1471-L1490), [internal/ipchandler/handler.go:457-518](internal/ipchandler/handler.go#L457-L518), [internal/ui/httpserver.go:96-376](internal/ui/httpserver.go#L96-L376), [internal/ui/ui.go:160-522](internal/ui/ui.go#L160-L522), [internal/config/config.go:109-149](internal/config/config.go#L109-L149)

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m] (Opus 4.7, 1M context)

### Debug Log References

### Completion Notes List

- **Story créée le 2026-04-18 par workflow `create-story`** — analyse exhaustive du code existant : la logique core updater (checker, downloader, verifier, persistence rollback) est **déjà entièrement implémentée et testée** (héritage Windows-stable, ancien Epic 6). Cette story se concentre sur (i) la validation de conformité avec les nouveaux ACs (rate limit 500 kbps, défauts), (ii) le gap UI nouveau tray cross-platform qui n'expose actuellement aucune indication d'update prête, (iii) l'ajout d'une commande CLI manuelle `levoile-ctl update check`.
- **Implémentation 2026-04-18 (workflow `dev-story`)** :
  - **AC1** ✅ : `internal/config/config.go` défaut `RateLimitKBps` aligné `512 → 500` ; `config.example.toml` reçoit la section `[update]` documentée
  - **AC2-7, AC12** ✅ : validation existant (4 packages, ~30 tests passent, aucun nouveau code requis)
  - **AC8** ✅ : nouveau menu tray « Mise à jour disponible : v{version} » via interface `updateMenuController` (testabilité sans dépendre du runtime systray) + idempotence `lastShownUpdateVersion`. 5 tests UI couvrent first detection, idempotence, hide on clear, version bump, nil-safe.
  - **AC9** ✅ : bandeau webview pâle (bleu pour update_ready, orange pour rollback — composant prêt pour story 8.2) en `position: fixed; bottom: 0`, polling 5 s `/api/update-status`, dismiss in-memory uniquement (jamais localStorage). Contract test `TestAppJSContract_Story81` verrouille les invariants.
  - **AC10** ✅ : `levoile-ctl update check` avec auth token (pattern story 5.9), timeout 5 min, exit codes 0/1/2 (`exitDisabled` constant explicite). 6 tests couvrent succès / up-to-date / disabled / failure / token absent / verbe invalide.
  - **AC11** ✅ : champ `UpdaterConfig.Logger io.Writer` (default `io.Discard`), helper `(*Updater).logf` émet 9 marqueurs (`cycle start`, `check failed`, `up to date`, `skip rollback-marked`, `download start/done/failed`, `verify ok/failed`, `update ready`). Test anti-PII passe sur 6 regex sensibles.
- **Décisions notables vs spec** :
  - Banner positionné en bas (`fixed bottom`) plutôt que sticky-top : strict respect de la consigne « non-bloquant », évite la collision visuelle avec les bandeaux destructifs (killswitch / integrity)
  - `exitDisabled = 2` cohabite avec `exitUsage = 2` (alias sémantique) — préserve la lisibilité des sites d'appel sans introduire un nouveau code retour qui casserait le mapping spec « 2 = disabled »
  - Code logging sans `filepath.Base` parce qu'**aucun** chemin n'est jamais émis (les erreurs core remontent uniquement via `fmt.Errorf("updater: <stage>: %w", err)` sans propager de paths). Test anti-PII regex valide cette propriété.
  - Pas d'env var `LEVOILE_FAKE_UPDATE_READY` (rejetée pendant le dev) — le smoke E2E se fait simplement en pointant `[update]` vers une release réelle ou en injectant un faux `pendingUpdateVersion` dans un test d'intégration ; pas de risque de leak prod.
- **Test résultats** :
  - Suite Windows complète sur les 7 packages touchés : ✅ tous verts
  - 2 échecs `internal/firewall/TestIntegrationWFP_*` (privilèges Admin requis) + 1 flake `internal/relay/TestServer_RejectsNonCFSource_TCPListener` : **pré-existants, hors scope story 8.1** (aucun fichier de ces packages modifié)
  - Linux cross-build (`GOOS=linux CGO_ENABLED=0`) sur les modules touchés (sauf `cmd/ui` qui requiert libwebkit2gtk-dev) : ✅
- **Ultimate context engine analysis completed - comprehensive developer guide created and implemented.**

## Senior Developer Review (AI)

**Reviewer** : claude-opus-4-7[1m] (Opus 4.7, 1M context)
**Review date** : 2026-04-18
**Outcome** : **Approve (after fixes applied)**

Adversarial review identifia 3 HIGH, 5 MEDIUM, 4 LOW. Toutes les HIGH + MEDIUM ont été corrigées dans la même session ; les 4 LOW sont jugées acceptables (tooltip cosmétique, alignement gofmt, contract test class name).

### Action Items résolus (HIGH)

- [x] **H1** — Bandeau `position: fixed; bottom: 0` recouvrait `#btn-connect` → migré en `position: sticky; top: 0` (alignement avec `.killswitch-degraded-banner` et `.integrity-failed-banner`). Z-index abaissé à 180 pour ne jamais empiler sur les bandeaux destructifs. [frontend/src/style.css:367-388](frontend/src/style.css#L367-L388)
- [x] **H2** — File List clarifie que `internal/config/config.go`, `internal/config/config_test.go`, `config.example.toml` étaient déjà alignés par Epic 7 (commit `8be1e8b`) avant le démarrage du dev — AC1 reste satisfait sur le fond, le travail a juste été fait en parallèle.
- [x] **H3** — `runUpdateCheck` retournait `exitOK` sur tout `UpdateStatus` non géré (`downloading`, vide, futurs constants) → désormais retourne `exitGeneric` avec message stderr explicite. 2 nouveaux tests (`TestRun_Update_Check_UnknownStatus_ExitsGeneric`, `TestRun_Update_Check_EmptyStatus_ExitsGeneric`). [cmd/ctl/main.go:230-244](cmd/ctl/main.go#L230-L244)

### Action Items résolus (MEDIUM)

- [x] **M1** — `TestUpdater_Logging_NoSensitiveData` n'exerçait que le happy path → ajout `TestUpdater_Logging_ErrorPathsScrubHome` (3 sous-tests : Linux home, Windows profile, /root) qui valide explicitement que `sanitizePII` collapse `/home/<user>`, `C:\Users\<user>`, `/root` en `$HOME`. [internal/updater/logging_test.go:96-159](internal/updater/logging_test.go#L96-L159)
- [x] **M2** — `dismissUpdateBanner` ne mémorisait que les dismiss de bandeau `update_ready` → ajout `sessionDismissedRollback` + `lastSeenRollbackToken` (clé `version|reason`) pour que le clic « Plus tard » sur un rollback colle pendant la session. Contract test `Story81` verrouille les nouveaux symboles. [frontend/src/app.js:766-820](frontend/src/app.js#L766-L820)
- [x] **M3** — `levoile-ctl update check arg-en-trop` ignorait silencieusement les arguments excédentaires → désormais retourne `exitUsage` avec stderr. Test `TestRun_Update_Check_ExtraArgs_Usage`. [cmd/ctl/main.go:165-173](cmd/ctl/main.go#L165-L173)
- [x] **M4** — `applyUpdateMenu` lock/unlock/lock pattern (TOCTOU théorique sous concurrence future) → refactor en single lock cycle, claim `lastShownUpdateVersion` upfront, calls systray hors lock pour ne pas bloquer le poller sur un menu lent. [internal/ui/ui.go:585-619](internal/ui/ui.go#L585-L619)
- [x] **M5** — Aucun test de transition `update_ready → rollback` (boundary story 8.2) → ajout `TestUpdateTrayState_UpdateAvailable_HidesOnRollbackTransition` qui pin la décision : la story 8.1 cache l'entrée tray en rollback, le webview banner (orange) prend le relais. [internal/ui/ui_test.go:660-702](internal/ui/ui_test.go#L660-L702)

### LOW non corrigées (acceptables)

- **L1** — `TestUpdater_Logging_EmitsCycleEvents` smoke léger (vérifie 2 marqueurs sur 9). Pas critique : les 9 marqueurs sont visibles à l'œil dans le `out` capturé, et un test sur les 9 deviendrait fragile au moindre rewording.
- **L2** — Champ `lastShownUpdateVersion` non aligné gofmt avec les `menu*` voisins (types différents, gofmt n'aligne pas). Cosmétique pure.
- **L3** — Contract test ne vérifie pas la classe CSS `.update-ready-banner.rollback` produite par le JS (juste le string literal). Acceptable : la classe est dans `style.css` et le sélecteur DOM dans `app.js` (`banner.classList.add('rollback')`) — un grep manuel suffit.
- **L4** — Tooltip « sera appliquée au prochain redémarrage du service » sur-promet en l'absence de story 8.2. 8.2 est en `review`, donc consistant aujourd'hui ; un test cross-story ajouterait peu de valeur.

### Résultats post-fix

- 7 packages touchés tous verts en régression : `config`, `updater`, `ipchandler`, `ui`, `service`, `ctl`, `frontend`
- `go build ./...` Windows : succès
- 4 nouveaux tests ajoutés au cours du review (3 ctl + 1 ui) ; 1 nouveau test bloc (logging error paths, 3 sous-tests)
- Note transparente : 2 tests Windows-host (`TestIntegrationWFP_*`) et 1 flake relay restent hors scope story 8.1

**Décision** : story prête à être mergée et marquée `done`.

### File List

**Nouveaux fichiers** :
- `internal/updater/logging_test.go` — tests AC11 + ErrorPathsScrubHome (review M1) — 4 tests

**Fichiers modifiés (effectivement présents dans `git diff`)** :
- `internal/updater/updater.go` — champ `UpdaterConfig.Logger`, helper `logf`, 9 emit lines (AC11)
- `internal/updater/updater_test.go` — helper `newUpdaterWithLogger` + import `io`
- `internal/service/service.go` — `Logger: serviceStderr` injecté (AC11)
- `internal/ui/ui.go` — interface `updateMenuController`, champs `menuUpdate` + `menuUpdateClicked` + `lastShownUpdateVersion`, `applyUpdateMenu` (single lock cycle après review M4), `handleUpdateMenu`, wiring `setupMenu` + `menuHandler` (AC8)
- `internal/ui/ui_test.go` — `mockUpdateMenu` + 6 tests `TestUpdateTrayState_UpdateAvailable_*` (incluant `HidesOnRollbackTransition` après review M5) (AC8)
- `frontend/index.html` — DOM nodes `#update-banner` + `#update-banner-text` + `#update-dismiss` (AC9)
- `frontend/src/style.css` — `.update-ready-banner` (sticky-top après review H1) + `.update-ready-banner.rollback` (AC9)
- `frontend/src/app.js` — `startUpdateStatusPolling`, `pollUpdateStatus`, `renderUpdateBanner`, `dismissUpdateBanner` avec dismiss séparé update_ready/rollback (review M2), hook `update_available` UI event (AC8/AC9)
- `frontend/contract_test.go` — `TestAppJSContract_Story81` (avec rollback dismiss invariant après review M2) + `TestIndexHTMLContract_Story81` (AC9)
- `cmd/ctl/main.go` — constante `exitDisabled`, dispatch `update`, `runUpdate` (rejet args excessifs après review M3), `runUpdateCheck` (default branch exit non-zero après review H3), `sendIPCWithTimeout` factorisé, usage texte mis à jour (AC10)
- `cmd/ctl/main_test.go` — 9 tests `TestRun_Update_*` (incluant `UnknownStatus_ExitsGeneric`, `EmptyStatus_ExitsGeneric`, `ExtraArgs_Usage` après review H3+M3) (AC10)

**Fichiers prévus AC1 mais déjà alignés par Epic 7 (commit 8be1e8b, hors `git diff`)** :
- `internal/config/config.go` — défaut `RateLimitKBps: 500` déjà présent dans HEAD au démarrage du dev 8.1
- `internal/config/config_test.go` — assertion `want := 500` déjà présente
- `config.example.toml` — section `[update]` déjà ajoutée par le packaging Epic 7
> AC1 reste **satisfait** car l'objectif de la story est l'alignement avec arch (rate limit 500 kbps) ; le travail s'est trouvé fait en parallèle par Epic 7. Aucune correction nécessaire — note ajoutée pour transparence (review H2).
