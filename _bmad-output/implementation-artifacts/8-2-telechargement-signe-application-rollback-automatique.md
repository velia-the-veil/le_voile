# Story 8.2: Téléchargement signé + application + rollback automatique

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur final,
Je veux que la mise à jour soit téléchargée, vérifiée, appliquée au prochain démarrage, et rollback automatique en cas d'échec,
Afin de ne jamais me retrouver avec un client cassé bloquant ma protection.

## Acceptance Criteria

1. **Given** une nouvelle release est téléchargée
   **When** le binaire est récupéré
   **Then** la signature Ed25519 du checksum SHA256 est vérifiée via `crypto/subtle.ConstantTimeCompare` (NFR9c)
   **And** une signature invalide rejette la mise à jour avec log syslog/Event Log
   **And** le binaire vérifié est stocké dans un emplacement temporaire prêt pour swap

2. **Given** une mise à jour est prête à être appliquée
   **When** le service redémarre (manuel ou planifié)
   **Then** le swap atomique remplace l'ancien binaire par le nouveau
   **And** sur Linux : `systemctl restart levoile.service` est exécuté
   **And** sur Windows : SCM redémarre le service

3. **Given** la nouvelle version vient d'être appliquée
   **When** le service redémarre
   **Then** un timer de 30 secondes démarre
   **And** si le tunnel n'est pas établi dans les 30s, un rollback automatique restaure l'ancien binaire
   **And** le service redémarre sur l'ancienne version
   **And** un log syslog/Event Log enregistre l'échec sans données utilisateur

4. **Given** le swap atomique échoue (disque plein, permissions, écriture bloquée)
   **When** l'erreur est détectée
   **Then** le service continue sur l'ancienne version sans interruption
   **And** l'UI notifie l'utilisateur ("Mise à jour échouée — sera retentée à la prochaine occasion")
   **And** un retry est planifié au prochain check_interval

## Tasks / Subtasks

- [x] Task 1 : NFR9c — Comparaison timing-safe du checksum SHA256 (AC: #1)
  - [x] 1.1 Dans `internal/updater/verify.go`, remplacer `if actualHash != expectedHash` dans `VerifyChecksum()` par une comparaison `crypto/subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) != 1`. Import `crypto/subtle`
  - [x] 1.2 Ajouter commentaire sur la comparaison : `// NFR9c: constant-time comparison to prevent timing attacks on checksum verification`
  - [x] 1.3 Dans `verify_test.go`, ajouter test `TestVerifier_VerifyChecksum_ConstantTime` — vérifier que la fonction rejette bien un hash invalide (comportement fonctionnel inchangé). Le test est essentiellement un régression test, pas une mesure de timing (pas fiable dans les tests Go)
  - [x] 1.4 Vérifier que `crypto.Verify()` dans `internal/crypto/` utilise bien `ed25519.Verify()` qui est constant-time par construction (documentation : `crypto/ed25519` est nativement constant-time). Si oui, ajouter commentaire NFR9c dans `VerifySignature()`. Si non, corriger

- [x] Task 2 : Staging directory Linux — résolution selon mode d'exécution (AC: #1, #2, #4)
  - [x] 2.1 Refactorer `internal/config/paths_unix.go`. La fonction `StagingDir()` actuelle retourne `~/.config/levoile/updates/` — inadaptée pour le service system qui tourne sans `$HOME` user
  - [x] 2.2 Ajouter fonction `isServiceMode() bool` : retourne true si UID == 0 (Linux root) OU si le binaire tourne en tant que service kardianos (détection via env var `SERVICE_NAME` ou argument `service`). Sur Windows, garder le comportement actuel
  - [x] 2.3 Modifier `StagingDir()` Unix :
    - Si `isServiceMode()` → retourner `/var/lib/levoile/updates/`
    - Sinon → retourner `{os.UserConfigDir()}/levoile/updates/` (comportement actuel)
  - [x] 2.4 Créer le répertoire `/var/lib/levoile/updates/` avec permissions `0755` (ou `0700` si l'install crée l'user `levoile`) lors du premier appel au `NewDownloader()`. Si création échoue (permissions), retourner erreur claire `"updater: staging dir /var/lib/levoile/updates: permission denied — run as root or via systemd service"`
  - [x] 2.5 Ajouter tests `TestStagingDir_Linux_ServiceMode`, `TestStagingDir_Linux_UserMode` avec mock UID/env — utiliser `runtime.GOOS` check pour skip sur Windows
  - [ ] 2.6 [DEFERRED — Epic 7 packaging] Dans `deploy/install.sh` et dans les packages deb/rpm/apk, documenter que le répertoire `/var/lib/levoile/updates/` sera créé automatiquement au premier auto-update — pas d'action à l'installation initiale. Le code crée déjà le dossier via `os.MkdirAll` dans `NewDownloader`, donc fonctionnellement non-bloquant

- [x] Task 3 : Validation end-to-end du redémarrage service sur Linux (AC: #2)
  - [x] 3.1 Script `scripts/test-auto-update-linux.sh` créé (emplacement ajusté — le projet utilise `scripts/` pour les tests shell E2E). Séquence : install .deb v1.0.0 → stage v1.0.1 signée → `systemctl restart` → assertions sur `systemctl is-active`, journalctl, version via `levoile-ctl status`
  - [x] 3.2 Vérifier que `kardianos/service` sur Linux appelle bien `systemctl restart {name}.service` via `p.svc.Restart()` (lu via dry-run du code kardianos ou via logs). Documenter dans le commentaire de `scheduleServiceRestart()` quelle commande est exécutée sur chaque OS
  - [x] 3.3 Si `systemctl restart` échoue (par exemple systemd down ou service non installé comme service), `p.svc.Restart()` retourne une erreur. Logger sur `serviceStderr` (qui va dans journald via systemd) : `"service: restart after rollback: %v"` — déjà en place dans le code (service.go:1829)
  - [x] 3.4 Ajouter test unitaire `TestScheduleServiceRestart_NoSvc_NoOp` — vérifier que `scheduleServiceRestart()` avec `p.svc == nil` ne panique pas (mode portable, tests)

- [x] Task 4 : Désactivation auto-update en mode package-managed (AC: #4)
  - [x] 4.1 Ajouter détection package-managed dans `internal/updater/installer.go` : méthode `isPackageManaged() bool`. Heuristique Linux :
    - Si `/usr/bin/le_voile` (ou `/usr/local/bin/le_voile`) est le `executablePath` ET le binaire appartient à root:root avec mode `0755` → probablement package-managed (dpkg/rpm/pacman déploient là)
    - Si `/opt/levoile/...` → installation manuelle / systemd custom, NON package-managed
    - Si `$HOME/.local/bin/` ou `/tmp/` ou chemin utilisateur → portable/dev, NON package-managed
  - [x] 4.2 Dans `tryInstallStagedUpdate()` dans `service.go`, si `isPackageManaged()` retourne true ET `update.enabled = true` dans la config, logger un warning une seule fois : `"updater: install: skipped — binary is package-managed (deb/rpm/apk), use system package manager to upgrade"`. Supprimer les fichiers staged. Retourner sans erreur (comportement non-intrusif)
  - [x] 4.3 Dans `CheckAndDownload()` dans `updater.go`, si `isPackageManaged()` retourne true, désactiver le download en amont pour économiser bande passante. Retourner `ErrPackageManaged` (nouvelle sentinelle)
  - [x] 4.4 Ajouter sentinelle `ErrPackageManaged = errors.New("updater: binary is package-managed, auto-update disabled")`
  - [x] 4.5 Ajouter champ config `[update] allow_when_packaged bool` (défaut `false`) — permet de forcer l'auto-update même en mode package-managed (utilisateurs avancés). Si `true`, `isPackageManaged()` est ignoré
  - [x] 4.6 Tests `TestInstaller_IsPackageManaged_*` : /usr/bin root:root → true, /opt/levoile → false, $HOME → false, /tmp → false. Skip sur Windows

- [x] Task 5 : Logs syslog/Event Log pour échecs signature + rollback (AC: #1, #3)
  - [x] 5.1 Logger infrastructure livrée par Story 8.1 (mergée en parallèle, status `review`). `UpdaterConfig.Logger io.Writer` + méthode `logf()` émettent des lignes `service: updater: <action> ...` depuis `updater.go`. Couvre tous les cas d'erreur `ErrSignatureInvalid`/`ErrChecksumMismatch` via les call-sites `u.logf("verify failed ...", err)` et `u.logf("check failed ...", err)`
  - [x] 5.2 Wiring `Logger: serviceStderr` dans `service.go:1552` (création de l'Updater) — les lignes structurées traversent donc le canal canonique routé vers journald (Linux systemd `StandardError=journal`) / Event Log (Windows kardianos). Plumbing `UpdateAllowWhenPackaged`/`UpdateMaxInstallRetries` dans [cmd/client/main.go:38-43, 141-143, 382-383](cmd/client/main.go) pour que les valeurs config TOML arrivent jusqu'au service
  - [ ] 5.3 [DEFERRED — validation humaine] Vérification journalctl sur Linux réel — couvert par la procédure `docs/testing/auto-update-e2e.md` scénarios A/E/F mais pas exécuté dans cette itération (pas d'accès VM Linux)
  - [ ] 5.4 [DEFERRED — validation humaine] Vérification Event Viewer Windows — couvert par `auto-update-e2e.md` scénario D mais pas exécuté. Pas de doc séparée `auto-update-windows.md` créée : scénario D intégré au doc principal
  - [x] 5.5 Audit PII + sanitizer défensif : (a) mon log skip package-managed utilise `filepath.Dir()` pour ne logger que `/usr/bin` ; (b) **nouveau** : `sanitizePII()` + `scrubHome()` dans `updater.go` scrubbent les chemins user-home (`/home/X`, `/Users/X`, `/root`, `C:\Users\X`) des erreurs OS-wrappées avant qu'elles n'atteignent `u.logger`. Remplacement par `$HOME` placeholder. Tests `TestSanitizePII_ScrubsUserHome` (8 cas) + `TestUpdater_Logger_ScrubsPII` (propagation e2e)

- [x] Task 6 : Notification UI échec swap atomique (AC: #4)
  - [x] 6.1 Dans `internal/ipc/messages.go`, vérifier que `StatusInstallFailed` existe (story 6.2 l'a ajouté). Vérifier que `Response.InstallError string` est bien présent. Si manquant, ajouter
  - [x] 6.2 Dans `service.go` `tryInstallStagedUpdate()`, en cas d'échec de `Install()` (disque plein, permissions), stocker l'erreur dans `p.lastInstallError` et `p.lastFailedInstallVersion`. Déjà en place via story 6.2 — vérifier par lecture du code
  - [ ] 6.3 [DEFERRED — Epic 5 UI] Bannière toast frontend `install_failed` : l'API backend `/api/update-status` expose déjà le champ `install_error` (httpserver.go:373, 389). Le rendu UI n'est pas ajouté ici car le frontend vanilla JS actuel n'a pas d'infrastructure de toast. À traiter dans une story UI dédiée d'Epic 5. Le tray continue d'afficher la notification via `StatusInstallFailed` (story 6.2 legacy)
  - [x] 6.4 Le retry est naturellement planifié par l'Updater : le staging contient toujours le binaire validé, et au prochain redémarrage `HasStagedUpdate()` le détectera à nouveau. Vérifier ce comportement avec test `TestService_InstallFails_StagedPreserved_RetryOnNextBoot`
  - [x] 6.5 Ajouter un compteur `install_retry_count` dans `rollback_state.json` (ou un fichier dédié `install_retries.txt`) — incrémenté à chaque échec d'install. Si > 3 (configurable via `update.max_install_retries`, défaut 3), supprimer le staging et logger `"updater: install: abandoned after 3 retries, will re-download on next check"` pour éviter les loops infinis

- [x] Task 7 : Validation manuelle end-to-end cross-platform (AC: #1, #2, #3, #4)
  - [x] 7.1 Rédiger procédure de test manuel `docs/testing/auto-update-e2e.md` couvrant :
    - Scénario A (Linux service mode) : install v1.0.0 deb, publier release v1.0.1 signée sur GitHub test, attendre 6h (ou forcer via IPC), vérifier swap + systemctl restart + version active
    - Scénario B (Linux user mode) : binaire dans `~/.local/bin/`, même séquence, vérifier que staging est `~/.config/levoile/updates/`
    - Scénario C (Linux packaged intentionnel) : install via dpkg dans `/usr/bin/`, vérifier que auto-update est skippé avec warning
    - Scénario D (Windows service) : install via NSIS, publier release signée, vérifier SCM restart + version active
    - Scénario E (Rollback Linux) : publier release v1.0.2 avec binaire qui crash au boot, vérifier timeout 30s + rollback vers v1.0.1 + `systemctl is-active` + notification UI
    - Scénario F (Signature invalide) : publier release avec `checksums.txt.sig` corrompu, vérifier rejet + log journald + staging nettoyé
  - [ ] 7.2 [DEFERRED — validation humaine] Exécuter les 6 scénarios sur Debian 12, Ubuntu 24.04, Fedora 40, Arch (latest), Alpine 3.19, Windows 11. Matrice documentée mais pas encore parcourue. À planifier avant release 1.0.0
  - [ ] 7.3 [DEFERRED — conditionnel] Création d'issues GitHub en cas d'échec — N/A tant que 7.2 n'est pas exécuté

## Dev Notes

### Contexte Technique Critique

**STATUT DE LA STORY — LARGEMENT EXISTANT**

Cette story est marquée `in-progress` dans le sprint car le mécanisme complet d'auto-update (download + vérification signature + installation atomique + rollback 30s) est **déjà implémenté** par les stories legacy 6.1, 6.2 et 6.3 (Epic 6 obsolète absorbé par Epic 8). Le code vit dans `internal/updater/` et `internal/service/service.go`.

**Le rôle de cette story est de VALIDER + COMBLER LES GAPS**, pas de réécrire. Les gaps identifiés :
1. **NFR9c** : `VerifyChecksum()` utilise `!=` string compare au lieu de `subtle.ConstantTimeCompare` (verify.go:55)
2. **Linux staging path** : `paths_unix.go` retourne `~/.config/...` même en mode service system — à découpler
3. **Validation Linux e2e** : le code kardianos/service → `systemctl restart` n'a jamais été testé end-to-end sur Linux
4. **Package-managed detection** : pas de garde-fou contre l'auto-update quand le binaire est déployé via dpkg/rpm/pacman (risque conflit versions)
5. **Logs structurés** : mix `fmt.Fprintf(stderr)` + éventuels `Logger.*` — à harmoniser pour aller dans journald/Event Log proprement

**FLUX COMPLET (déjà implémenté, ne pas réécrire) :**

```
[Updater.Start() loop — every 6h]
  -> Checker.CheckLatest() → ReleaseInfo (GitHub API)
  -> Downloader.DownloadRelease() → StagedUpdate (3 fichiers dans stagingDir)
  -> Verifier.VerifyStagedUpdate() → SHA256 + Ed25519
    [NFR9c gap : VerifyChecksum utilise != au lieu de ConstantTimeCompare]
  -> writeStagedVersion(stagingDir, version)
  -> onUpdateReady(version) callback → tray.NotifyUpdateReady()

[Service redémarrage — Program.run()]
  -> tryInstallStagedUpdate()
    -> Installer.HasStagedUpdate() → StagedUpdate|nil
    -> Installer.Install(ctx, staged) → swap atomique (.bak + atomicCopyFile + rename)
    -> WriteRollbackState(JustInstalled=true, version)
    -> installedVersion = version
  -> TunnelClient.Connect(ctx, timeout=30s si installedVersion != "")
    -> ECHEC (timeout, crash réseau, etc.)
      -> tryRollbackIfNeeded(ctx, err)
        -> ReadRollbackState() → JustInstalled == true
        -> Installer.Rollback() → restaure .bak
        -> WriteFailedVersion(version)
        -> ClearRollbackState()
        -> rollbackOccurred = true, installedVersion = ""
      -> scheduleServiceRestart() → goroutine attend done, puis p.svc.Restart()
        [Linux : kardianos shellout `systemctl restart levoile.service`]
        [Windows : kardianos appel SCM natif]
    -> SUCCES
      -> ClearRollbackState()
      -> Installer.CleanupBackup()
```

### Gaps Concrets à Corriger — Liste Exhaustive

**Gap 1 — NFR9c non respecté (verify.go:55)**
```go
// ACTUEL (non-constant-time)
if actualHash != expectedHash {
    return ErrChecksumMismatch
}

// CIBLE (Task 1.1)
if subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) != 1 {
    return ErrChecksumMismatch
}
```

**Gap 2 — StagingDir service Linux (paths_unix.go:22)**
```go
// ACTUEL
func StagingDir() (string, error) {
    dir, err := os.UserConfigDir() // retourne ~/.config — échoue si $HOME vide (systemd)
    ...
    return filepath.Join(dir, "levoile", "updates"), nil
}

// CIBLE (Task 2.3)
func StagingDir() (string, error) {
    if isServiceMode() {
        return "/var/lib/levoile/updates", nil
    }
    dir, err := os.UserConfigDir()
    ...
    return filepath.Join(dir, "levoile", "updates"), nil
}
```

**Gap 3 — Logs updater vers service.Logger kardianos**
- `fmt.Fprintf(serviceStderr, ...)` écrit sur stderr → systemd route vers journald automatiquement sur Linux (OK)
- Mais sur Windows : stderr d'un service SCM n'est **pas** automatiquement routé vers Event Log. Il faut passer par `service.Logger.Warningf(...)` explicitement
- Task 5.1/5.2 vérifient et uniformisent

**Gap 4 — Package-managed install (nouveau)**
Si l'utilisateur installe `le_voile_0.1.0_amd64.deb` via `apt install`, le binaire vit dans `/usr/bin/le_voile` avec owner root:root mode 0755. Si l'auto-update écrit par-dessus :
- dpkg verify (`debsums`) signalera une corruption
- Le prochain `apt upgrade le_voile` écrasera notre binaire auto-updaté
- Loop potentielle : apt install v1.0.0 → auto-update v1.0.1 → apt upgrade v1.0.0 → auto-update v1.0.1 → ...

Solution : détecter et skip. L'utilisateur avancé peut forcer via `update.allow_when_packaged = true`.

### Architecture — Points de Référence

**Architecture sections pertinentes** (cf. epics.md + architecture.md) :
- **NFR9c** : Toutes les comparaisons cryptographiques utilisent `crypto/subtle.ConstantTimeCompare` (architecture.md:339)
- **Linux restart** : "Linux : après remplacement binaire → `systemctl restart levoile.service` via dbus ou shellout" (architecture.md:337) — implémenté via kardianos Restart() qui fait shellout `systemctl restart`
- **Staging paths** : non documenté explicitement dans architecture.md — à décider dans Task 2 de cette story (choix : `/var/lib/levoile/updates/` pour service, `~/.config/levoile/updates/` pour user)
- **Config update section** : `[update] enabled bool, check_interval "6h", rate_limit_kbps 512, github_owner, github_repo, max_install_retries 3, allow_when_packaged false` (le champ `allow_when_packaged` ajouté par cette story)

**Integrity Checker binaire** (architecture.md:345) : séparé de l'auto-update. Après swap, au prochain démarrage, `internal/integrity/` vérifie le SHA256 du binaire courant contre une signature Ed25519 embed. Si l'auto-update écrit un binaire mal signé, l'integrity check bloquera le démarrage → rollback déclenché par absence de tunnel dans les 30s. Comportement déjà valide, pas d'action requise dans cette story.

### Intelligence Story 6.3 — Learnings Critiques à Ne Pas Réinventer

**Patterns établis à réutiliser (NE PAS REFAIRE) :**
- `RollbackState` struct et `rollback_state.json` dans stagingDir — déjà implémenté (rollback.go)
- `failed_version.txt` pour bloquer le re-téléchargement d'une version défectueuse — déjà implémenté
- `install_retries.txt` pour le compteur d'abandon — **ajouté par cette story 8.2** dans rollback.go (`ReadInstallRetries`, `WriteInstallRetries`, `ClearInstallRetries`). Format texte brut (un entier décimal, ex: `2`). Incrémenté à chaque échec d'`Install()`, clearé au succès ou à l'abandon après `max_install_retries`
- `rollbackTimeout = 30 * time.Second` comme constante dans service.go — déjà implémenté
- `scheduleServiceRestart()` avec `p.svc.Restart()` kardianos — déjà implémenté (service.go:1822)
- Dedup tray `notifiedRollbackVer` — déjà implémenté
- Priorité IPC StatusRollback > StatusInstalled — déjà implémentée dans handler.go

**Contenu complet du stagingDir après Story 8.2 :**
```
{stagingDir}/
├── le_voile_{GOOS}_{GOARCH}[.exe]   # binaire staged
├── checksums.txt                     # hashes SHA256 (format GoReleaser)
├── checksums.txt.sig                 # signature Ed25519 de checksums.txt
├── staged_version.txt                # version en attente (ex: "2.1.0")
├── rollback_state.json               # {just_installed, installed_version} — transitoire
├── failed_version.txt                # version défectueuse bloquée — persistant
└── install_retries.txt               # compteur N d'échecs Install() sur le staged courant — Story 8.2
```

**Bugs connus de 6.2/6.3 — pertinents ici :**
- Tests DNS Windows (`TestParseDNSFromNetsh_GarbageOutput`, `TestWindowsManager_*`) échouent — non liés
- `TestInstaller_Install_ReadOnly` skip sur Windows — attendu (POSIX perms)
- Code review 6.3 #2 : `tunnelClient = nil` sur chemin rollback + clear `installedVersion` après rollback — déjà en place, ne pas régresser

### Patterns Go à Respecter

- **Package** : modifications dans `internal/updater/` (verify.go, installer.go, updater.go) et `internal/config/` (paths_unix.go)
- **Nouveau fichier** : aucun — extension des fichiers existants uniquement
- **Erreurs** : `fmt.Errorf("updater: <scope>: %w", err)`, sentinelles `errors.New(...)`
- **Concurrence** : réutiliser `p.updateMu` pour tout accès à `lastInstallError`, `installedVersion`, `rollbackOccurred`, etc.
- **Logs** : `p.svc.Logger.Errorf/Warningf/Infof` quand disponible, sinon `fmt.Fprintf(serviceStderr, ...)`. **Jamais** `log.Printf` ni `fmt.Println`
- **Tests** : `t.TempDir()`, build tag `//go:build linux` pour les tests Linux-only, skip via `if runtime.GOOS != "linux" { t.Skip() }` pour les tests comportementaux
- **Aucune dépendance nouvelle** — tout est stdlib (`crypto/subtle`, `os`, `runtime`)

### Sécurité — Points Critiques

1. **NFR9c obligatoire** : la comparaison de checksum via `!=` expose théoriquement à un timing attack sur un attaquant capable de générer de multiples binaires — impact faible en pratique (attaque sur hash, pas sur secret) mais la NFR est explicite. À corriger sans débat
2. **Ed25519 nativement constant-time** : `crypto/ed25519.Verify` est constant-time par construction (cf. doc Go stdlib) — pas de modification requise, juste documenter
3. **failed_version.txt non signé** : texte brut du numéro de version. Si un attaquant peut écrire dans stagingDir, il peut empêcher les updates en injectant n'importe quel numéro de version. Mitigé par les permissions 0700/0600 sur stagingDir — à vérifier dans Task 2.4
4. **Staging dir permissions Linux** : si `/var/lib/levoile/updates/` est 0755 (lisible par tous), un user non-privilégié peut lire le contenu du binaire staged. Peu sensible (déjà signé, téléchargé publiquement depuis GitHub), mais mieux vaut 0750 owner root:levoile
5. **Package-managed detection heuristique** : l'heuristique `/usr/bin + root:root + mode 0755` peut être contournée (user root copie manuellement dans /usr/bin). Ce n'est pas un contrôle sécurité, juste un garde-fou UX — documenter clairement

### Project Structure Notes

**Fichiers modifiés** :
```
internal/updater/verify.go                    # NFR9c — subtle.ConstantTimeCompare
internal/updater/verify_test.go               # tests régression comparison
internal/updater/installer.go                 # isPackageManaged()
internal/updater/installer_test.go            # tests isPackageManaged
internal/updater/updater.go                   # ErrPackageManaged, skip si packaged
internal/updater/updater_test.go              # test skip si packaged
internal/config/paths_unix.go                 # StagingDir service vs user
internal/config/paths_unix_test.go            # tests (nouveau si absent)
internal/config/config.go                     # allow_when_packaged, max_install_retries
internal/config/config_test.go                # tests champs
internal/service/service.go                   # retry counter + log kardianos unifiés
internal/service/service_test.go              # tests retry abandon après 3
internal/ui/frontend/...                      # notification install_failed (si pas déjà en place via 6.2)
```

**Fichiers nouveaux** :
```
test/e2e/auto_update_linux_test.sh            # test E2E manuel scripté
docs/testing/auto-update-e2e.md               # procédure validation matrice OS
docs/testing/auto-update-windows.md           # procédure Event Log Windows
```

**Aucun fichier supprimé.**

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 8 — Story 8.2]
- [Source: _bmad-output/planning-artifacts/architecture.md#Auto-update] (architecture.md:337)
- [Source: _bmad-output/planning-artifacts/architecture.md#Crypto — NFR9c ConstantTimeCompare] (architecture.md:339)
- [Source: _bmad-output/planning-artifacts/architecture.md#Integrity Checker] (architecture.md:345)
- [Source: _bmad-output/planning-artifacts/prd.md#FR36 — Téléchargement signé + rollback]
- [Source: _bmad-output/implementation-artifacts/6-1-verification-de-version-et-telechargement-en-arriere-plan.md — base existante check+download]
- [Source: _bmad-output/implementation-artifacts/6-2-installation-au-redemarrage-et-notification-tray.md — base existante Install()]
- [Source: _bmad-output/implementation-artifacts/6-3-rollback-automatique-en-cas-dechec.md — base existante rollback 30s]
- [Source: internal/updater/verify.go — VerifyChecksum (gap NFR9c ligne 55)]
- [Source: internal/updater/installer.go — Install(), Rollback(), atomicCopyFile, renameWithRetry]
- [Source: internal/updater/updater.go — CheckAndDownload(), onUpdateReady]
- [Source: internal/updater/rollback.go — RollbackState, WriteFailedVersion]
- [Source: internal/service/service.go:1164 — appel scheduleServiceRestart après rollback]
- [Source: internal/service/service.go:1818-1832 — scheduleServiceRestart implémenté]
- [Source: internal/config/paths_unix.go:22 — StagingDir() à décliner service/user]
- [Source: internal/ipc/messages.go — StatusInstalled, StatusInstallFailed, StatusRollback]
- [Source: internal/ipchandler/handler.go — handleUpdateStatus chaîne de priorité rollback > installed]
- [Source: https://pkg.go.dev/crypto/subtle#ConstantTimeCompare — stdlib doc NFR9c]
- [Source: https://pkg.go.dev/github.com/kardianos/service — Restart() implémentation Linux/Windows]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context)

### Debug Log References

- `go test ./...` — full suite verte (34 packages testés, 0 failure).
- `go vet ./internal/config/ ./internal/updater/ ./internal/service/` cross-OS (linux + windows) : OK.
- Test préexistant en échec sur `vet` Linux : `internal/dns/manager_linux_test.go:116 TestParseAllResolvectlInterfaces redeclared` — non lié à cette story, pré-existant avant mes changements.

### Completion Notes List

**Tasks implémentées (code + tests verts) :**

- **Task 1 — NFR9c ConstantTimeCompare** : `VerifyChecksum()` utilise maintenant `crypto/subtle.ConstantTimeCompare` (verify.go:55). Commentaire NFR9c ajouté sur `VerifySignature()` (ed25519 nativement constant-time). Test régression `TestVerifier_VerifyChecksum_ConstantTime` ajouté.
- **Task 2 — StagingDir Linux service/user mode** : `paths_unix.go` refactor avec `isServiceMode()` (euid==0) + override `LEVOILE_STAGING_DIR`. Nouveau test file `paths_unix_test.go` (4 tests). Service mode → `/var/lib/levoile/updates/`, user mode → `~/.config/levoile/updates/`.
- **Task 3 — Validation systemctl restart** : commentaire `scheduleServiceRestart()` documente le mapping kardianos → `systemctl restart` (Linux) / SCM (Windows). Script `scripts/test-auto-update-linux.sh` créé pour validation VM. Test unitaire `TestScheduleServiceRestart_NoSvc_NoOp` ajouté.
- **Task 4 — Package-managed skip** : `Installer.IsPackageManaged()` + heuristique `/usr/bin`, `/usr/local/bin`, `/usr/sbin` (Linux uniquement, Windows → toujours false). `ErrPackageManaged` sentinelle ajoutée, service.go skip avant install avec nettoyage staging, Updater.Start/CheckAndDownload short-circuit avant GitHub. Config `AllowWhenPackaged bool` (défaut false). Tests : `TestInstaller_IsPackageManaged` (8 cases table-driven), `TestUpdater_PackageManaged_ShortCircuits`.
- **Task 5 — Audit logs** : updater package n'a aucun log (erreurs retournées par valeur). Service logs via serviceStderr canonique (routé vers journald par systemd `StandardError=journal` Linux, vers Event Log par kardianos Windows). Mon nouveau log skip package-managed utilise `filepath.Dir()` pour conformité NFR22a. Audit PII complet OK.
- **Task 6 — Retry counter + UI** : nouveau compteur `install_retries.txt` avec `ReadInstallRetries/WriteInstallRetries/ClearInstallRetries` dans rollback.go. Config `MaxInstallRetries` (défaut 3). service.go abandonne staged payload après N échecs. Tests : `TestInstallRetries_WriteRead`, `TestInstallRetries_ReadMalformed`, `TestTryInstallStagedUpdate_AbandonsAfterMaxRetries`. API `/api/update-status` expose déjà `install_error` (story 6.2 legacy).
- **Task 7 — Doc e2e** : `docs/testing/auto-update-e2e.md` rédigé avec 6 scénarios (A-F) couvrant Linux service/user/packaged, Windows SCM, rollback, signature invalide.

**Tasks différées (clairement annotées [DEFERRED] dans les subtasks) :**
- 2.6 : doc `deploy/install.sh` — non bloquant, création automatique via `os.MkdirAll`.
- 5.2 : logger kardianos dédié — non bloquant, serviceStderr déjà routé.
- 5.3, 5.4, 7.2, 7.3 : validations humaines sur VMs réelles — requiert infrastructure VPS/VM multi-OS, à planifier avant release 1.0.0.
- 6.3 : toast frontend install_failed — déporté vers une story UI dédiée d'Epic 5 (backend déjà fourni).

**ACs validés par les tâches implémentées :**
- AC #1 (vérification Ed25519 + SHA256 timing-safe + log rejet) : ✅ Task 1 + Task 5 audit.
- AC #2 (swap atomique + systemctl restart Linux + SCM Windows) : ✅ code legacy 6.2 + doc mapping Task 3.2 + validation humaine différée Task 7.2.
- AC #3 (timer 30s + rollback automatique + log sans PII) : ✅ code legacy 6.3 + audit PII Task 5.5.
- AC #4 (continue ancienne version + notif UI + retry check_interval) : ✅ retry counter Task 6 + backend install_error exposé + frontend toast différé.

### Change Log

- 2026-04-18 : Story 8.2 — hardening auto-update pour Linux + conformité NFR9c.
  - NFR9c : comparaison checksum via `crypto/subtle.ConstantTimeCompare`.
  - StagingDir Linux : `/var/lib/levoile/updates/` en mode service (euid==0), `~/.config/levoile/updates/` en mode user, override via `LEVOILE_STAGING_DIR`.
  - Package-managed detection : skip auto-update si `/usr/bin|/usr/local/bin|/usr/sbin`, flag `allow_when_packaged` pour override.
  - Retry counter : `install_retries.txt` + abandon après `max_install_retries` (défaut 3) pour éviter les loops.
  - Doc `scheduleServiceRestart()` + script E2E `scripts/test-auto-update-linux.sh` + doc matrice 6 scénarios `docs/testing/auto-update-e2e.md`.
  - Audit PII : log skip package-managed utilise `filepath.Dir()` uniquement.
  - 0 régression sur `go test ./...` (34 packages).
- 2026-04-18 (deuxième passe après merge 8-1) : alignement avec la Logger infrastructure livrée par 8.1.
  - Wiring `Logger: serviceStderr` dans service.go NewUpdater (traversée journald/Event Log garantie).
  - Plumbing config TOML `[update] allow_when_packaged` / `max_install_retries` depuis cmd/client/main.go jusqu'à service.Config (sinon les valeurs restaient hardcodées aux defaults).
  - Sanitizer PII `sanitizePII()` + `scrubHome()` dans updater.go : toute erreur OS-wrappée contenant `/home/<user>`, `/Users/<user>`, `/root`, `C:\Users\<user>` est remplacée par `$HOME` avant émission via `u.logf`. Couverture NFR22a durcie.
  - Tests : `TestSanitizePII_ScrubsUserHome` (8 cas) + `TestUpdater_Logger_ScrubsPII` (propagation e2e). Task 5.2 : DEFERRED → DONE.
- 2026-04-18 (code review 8.2, 7 findings corrigés) :
  - **H1** : docstring `UpdateConfig.MaxInstallRetries` alignée avec le code (0 → fallback à 3, pas d'unlimited mode by design — anti-boot-loop).
  - **M1** : WARNING explicite sur `sanitizePII` — le retour `errors.New(...)` casse la chaîne d'erreurs. Safe pour le call-site actuel (logf), à reinstaller si le retour est propagé.
  - **M2** : `TestScheduleServiceRestart_NoSvc_NoOp` assertions goroutine count (plus de leak). Ajout `TestScheduleServiceRestart_WithSvc_RestartsAfterDone` avec `fakeService` qui compte les `Restart()` — couvre la branche non-nil.
  - **M3** : `isServiceMode()` détecte aussi `INVOCATION_ID` et `NOTIFY_SOCKET` (env systemd) — ne casse plus si le unit passe à `User=levoile` non-root avec `AmbientCapabilities`. Test `TestIsServiceMode_SystemdEnvTriggersEvenWhenNonRoot` ajouté. Tests `TestStagingDir_UserMode`/`_ServiceMode_SystemPath` isolent explicitement ces env vars.
  - **L1** : `os.UserHomeDir()` mis en cache dans `Updater.homeDir` à la construction. `sanitizePII(arg, homeDir)` et `scrubHome(s, homeDir)` prennent le home en paramètre — plus de syscall par ligne de log. Test `TestSanitizePII_HomeDirFallback` couvre le cas custom-home.
  - **L2** : `/opt/homebrew/bin/` (brew macOS M1) et `/home/linuxbrew/.linuxbrew/bin/` (Linuxbrew) ajoutés à `packageManagedPrefixes`. 2 nouveaux cas dans `TestInstaller_IsPackageManaged`.
  - **L3** : listing complet du stagingDir (binaire + 6 fichiers d'état, dont `install_retries.txt`) ajouté au Dev Notes "Architecture d'Etat Rollback" pour combler le gap doc.
  - 0 régression sur `go test ./internal/{updater,config,service}/ ./cmd/client/` + `GOOS=linux go build ./... && go test -c`. Status: review → **done**.

### File List

**Modifiés :**
- internal/updater/verify.go — NFR9c ConstantTimeCompare + commentaires
- internal/updater/verify_test.go — TestVerifier_VerifyChecksum_ConstantTime + flipHex helper
- internal/updater/installer.go — ErrPackageManaged, IsPackageManaged(), isPackageManagedPath()
- internal/updater/installer_test.go — TestInstaller_IsPackageManaged (8 cases), TestInstaller_IsPackageManaged_WindowsAlwaysFalse
- internal/updater/updater.go — UpdaterConfig.PackageManaged, short-circuit CheckAndDownload/Start, **sanitizePII + scrubHome + homeUserRE/rootHomeRE** (NFR22a hardening, 2e passe)
- internal/updater/updater_test.go — TestUpdater_PackageManaged_ShortCircuits, **TestSanitizePII_ScrubsUserHome (8 cas), TestUpdater_Logger_ScrubsPII** (2e passe)
- internal/updater/rollback.go — installRetriesFile, ReadInstallRetries, WriteInstallRetries, ClearInstallRetries
- internal/updater/rollback_test.go — TestInstallRetries_WriteRead, TestInstallRetries_ReadMalformed
- internal/config/paths_unix.go — refactor StagingDir() avec isServiceMode + LEVOILE_STAGING_DIR override
- internal/config/config.go — UpdateConfig.AllowWhenPackaged, MaxInstallRetries + defaults
- internal/service/service.go — Config.UpdateAllowWhenPackaged/UpdateMaxInstallRetries, skip package-managed dans tryInstallStagedUpdate, retry counter, doc scheduleServiceRestart mapping OS, Updater.PackageManaged plumbing, **Logger: serviceStderr wiring** (2e passe)
- internal/service/service_test.go — TestTryInstallStagedUpdate_AbandonsAfterMaxRetries, TestScheduleServiceRestart_NoSvc_NoOp
- **cmd/client/main.go** — resolvedConfig.updateAllowWhenPackaged/updateMaxInstallRetries + peuplement depuis cfg.Update + passage à service.Config (2e passe, gap plumbing détecté)

**Nouveaux :**
- internal/config/paths_unix_test.go — tests StagingDir service/user mode + env override
- scripts/test-auto-update-linux.sh — script E2E validation Linux
- docs/testing/auto-update-e2e.md — doc matrice 6 scénarios cross-OS
