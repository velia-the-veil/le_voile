# Story 6.2: Installation au redemarrage et notification tray

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux etre notifie qu'une mise a jour est prete et qu'elle s'installe au prochain demarrage,
Afin de savoir que ma version sera mise a jour sans surprise ni interruption.

## Acceptance Criteria

1. **Given** un binaire mis a jour verifie et pret en staging
   **When** le tray recoit l'information via IPC (callback `onUpdateReady`)
   **Then** une notification tray s'affiche via tooltip temporaire : "Mise a jour vX.Y.Z prete — appliquee au prochain demarrage"

2. **Given** une mise a jour en attente dans le repertoire staging
   **When** le service redemarre (reboot OS ou redemarrage manuel)
   **Then** l'ancien binaire est sauvegarde (`le_voile.exe.bak`), le nouveau binaire remplace l'ancien, et le service demarre avec la nouvelle version

3. **Given** le menu tray affiche
   **When** une mise a jour est en attente
   **Then** un item de menu informatif indique "Mise a jour vX.Y.Z prete" (visible uniquement quand une MAJ est staged, pas d'action utilisateur requise)

4. **Given** le remplacement du binaire echoue (permissions, fichier verrouille)
   **When** l'installation est tentee au demarrage
   **Then** l'ancien binaire est conserve intact, le service demarre normalement avec l'ancienne version, et l'erreur est signalable via IPC

## Tasks / Subtasks

- [x] Task 1 : Module d'installation du binaire staged (AC: #2, #4)
  - [x] 1.1 Creer `internal/updater/installer.go` — struct `Installer` avec champs : `stagingDir string`, `executablePath string`
  - [x] 1.2 Implementer `NewInstaller(stagingDir string) (*Installer, error)` — resoud le chemin de l'executable courant via `os.Executable()`, resoud les symlinks via `filepath.EvalSymlinks`
  - [x] 1.3 Implementer `HasStagedUpdate() (*StagedUpdate, error)` — verifie si un binaire staged existe dans `stagingDir` correspondant a la plateforme courante (`le_voile_{GOOS}_{GOARCH}[.exe]`). Retourne nil si aucun fichier staged. Parse le fichier `staged_version.txt` pour la version
  - [x] 1.4 Implementer `Install(ctx context.Context, staged *StagedUpdate) error` — sequence atomique :
    1. Re-verifier l'integrite du staged (SHA256 + Ed25519) via `Verifier`
    2. Sauvegarder l'executable courant vers `{executablePath}.bak`
    3. Copier le binaire staged vers `{executablePath}` (copie + rename atomique, pas os.Rename cross-device)
    4. Verifier que le nouveau binaire est executable (`os.Stat` + permissions)
    5. Nettoyer le repertoire staging (supprimer binaire, checksums, signature, staged_version.txt)
    6. Retourner nil si succes
  - [x] 1.5 Implementer `Rollback() error` — si `{executablePath}.bak` existe, restaurer vers `{executablePath}`. Utilise par story 6.3 (rollback automatique)
  - [x] 1.6 Implementer `CleanupBackup() error` — supprimer `{executablePath}.bak` apres confirmation de fonctionnement correct (appele apres demarrage reussi)
  - [x] 1.7 Implementer `writeStagedVersion(stagingDir, version string) error` — ecrit `staged_version.txt` dans le stagingDir avec la version (appele par l'Updater apres verification reussie)
  - [x] 1.8 Ecrire tests pour `HasStagedUpdate` — fichier present, fichier absent, staged_version.txt manquant
  - [x] 1.9 Ecrire tests pour `Install` — installation reussie (verifier backup cree, fichier remplace, staging nettoye), echec copie (backup restaure), echec verification integrite (rien ne change)
  - [x] 1.10 Ecrire tests pour `Rollback` — backup existe et restaure, pas de backup (erreur propre)

- [x] Task 2 : Integration de l'installeur au demarrage du service (AC: #2, #4)
  - [x] 2.1 Dans `internal/service/service.go`, ajouter un champ `installer *updater.Installer` dans `Program`
  - [x] 2.2 Dans `run()`, AVANT le demarrage du tunnel (tout en debut de sequence), appeler `installer.HasStagedUpdate()`. Si une MAJ staged est detectee, appeler `installer.Install(ctx, staged)`
  - [x] 2.3 Si `Install` reussit : loguer "updater: installed vX.Y.Z" dans le status IPC initial, la nouvelle version est maintenant active
  - [x] 2.4 Si `Install` echoue : le service continue normalement avec l'ancienne version. Stocker l'erreur pour exposition via IPC `update_status`
  - [x] 2.5 Apres demarrage reussi du tunnel (confirmation que la nouvelle version fonctionne), appeler `installer.CleanupBackup()` pour supprimer le .bak
  - [x] 2.6 Ajouter methode `Installer() *updater.Installer` sur `Program` pour l'acces depuis les handlers IPC
  - [x] 2.7 Ecrire tests pour le cycle demarrage : pas de MAJ staged → demarrage normal, MAJ staged → installation → demarrage, MAJ staged → echec install → demarrage normal

- [x] Task 3 : Ecriture de `staged_version.txt` par l'Updater (AC: #1)
  - [x] 3.1 Dans `internal/updater/updater.go`, apres verification reussie dans la boucle `Start()` et dans `CheckAndDownload()`, appeler `writeStagedVersion(stagingDir, release.Version)` pour persister la version staged
  - [x] 3.2 Modifier `StagedUpdate` pour inclure un champ `VersionFile string` pointant vers `staged_version.txt`
  - [x] 3.3 Ecrire test : verification que `staged_version.txt` est ecrit apres un cycle check+download+verify reussi

- [x] Task 4 : Notification tray — tooltip temporaire et item de menu (AC: #1, #3)
  - [x] 4.1 Dans `internal/tray/tray.go`, ajouter un champ `menuUpdateReady` de type menu item dans struct `Tray`
  - [x] 4.2 Dans `setupMenu()`, creer l'item "Mise a jour prete" AVANT le premier separateur (entre "Verifier fuite WebRTC" et le separateur). L'item est cache par defaut via `Hide()`
  - [x] 4.3 Implementer methode `NotifyUpdateReady(version string)` sur `Tray` :
    - Mettre a jour le titre du menu item : "Mise a jour v{version} prete"
    - Rendre l'item visible via `Show()`
    - Afficher un tooltip temporaire : "Mise a jour v{version} prete — appliquee au prochain demarrage"
    - Restaurer le tooltip normal apres 10 secondes via `restoreTooltipAfter()`
  - [x] 4.4 Implementer methode `ClearUpdateNotification()` sur `Tray` — cache l'item de menu et remet le titre par defaut (appele apres installation reussie au demarrage suivant)
  - [x] 4.5 Ecrire tests pour `NotifyUpdateReady` — item visible, titre correct, tooltip modifie
  - [x] 4.6 Ecrire tests pour `ClearUpdateNotification` — item cache

- [x] Task 5 : IPC — notification du tray par le service (AC: #1, #3)
  - [x] 5.1 Ajouter constante IPC dans `messages.go` : `ActionNotifyUpdate = "notify_update"` — message pousse du service vers le tray quand une MAJ est prete
  - [x] 5.2 Ajouter constante `StatusInstalled = "installed"` — indique qu'une MAJ vient d'etre installee au demarrage
  - [x] 5.3 Ajouter constante `StatusInstallFailed = "install_failed"` — indique un echec d'installation
  - [x] 5.4 Ajouter champs dans `Response` : `InstalledVersion string` (version qui vient d'etre installee), `InstallError string` (erreur d'installation si applicable)
  - [x] 5.5 Modifier le handler `handleUpdateStatus` dans `ipchandler/handler.go` pour retourner aussi : si une installation vient de se faire → `StatusInstalled` + version, si une installation a echoue → `StatusInstallFailed` + erreur
  - [x] 5.6 Dans `internal/tray/tray.go`, dans la boucle de polling status existante, ajouter la detection du champ `UpdateVersion` dans la reponse IPC. Si `update_version` non vide et `update_status == "update_ready"` → appeler `NotifyUpdateReady(version)`
  - [x] 5.7 Connecter le callback `onUpdateReady` de l'Updater dans `service.go` pour stocker la version prete dans un champ `pendingUpdateVersion` sur `Program` (lu par le handler IPC)
  - [x] 5.8 Ecrire tests pour le handler IPC update_status etendu — cas installed, install_failed, update_ready

- [x] Task 6 : Gestion specifique mode portable vs installe (AC: #2)
  - [x] 6.1 En mode portable (`cmd/portable/main.go`), l'installation est identique : `os.Executable()` pointe vers le portable .exe, le backup et remplacement fonctionnent de la meme maniere
  - [x] 6.2 Verifier dans `Install()` que le binaire cible n'est pas en lecture seule (repertoire portable sur cle USB par exemple). Si lecture seule → retourner erreur specifique `ErrReadOnlyTarget`
  - [x] 6.3 Ajouter erreur sentinelle `ErrReadOnlyTarget = errors.New("updater: install: target is read-only")`
  - [x] 6.4 Ecrire test pour le cas read-only

## Dev Notes

### Contexte Technique Critique

**ARCHITECTURE D'INSTALLATION — SEQUENCE AU DEMARRAGE**

L'installation de la mise a jour s'effectue au TOUT DEBUT du demarrage du service, AVANT le tunnel et tout le reste. Sequence :

```
Service demarre (kardianos/service → Program.run())
  → 1. Charger config
  → 2. installer.HasStagedUpdate() ?
       → OUI : installer.Install(ctx, staged)
           → Re-verifier integrite (SHA256 + Ed25519)
           → Backup : copier current.exe → current.exe.bak
           → Copier staged binary → current.exe (copie + rename, pas rename direct car potentiel cross-device)
           → Nettoyer staging dir
       → NON : continuer normalement
  → 3. Demarrer tunnel, DNS, watchdog, STUN, IPC, tray...
  → 4. Si install reussie : cleanup .bak apres tunnel connecte
```

**DECISIONS CLES :**

1. **Moment d'installation :** Au demarrage du service, pas au shutdown. Raison : au shutdown le binaire peut etre verrouille par le process en cours. Au demarrage suivant, le nouveau process charge directement le nouveau binaire
2. **Copie vs Rename :** Utiliser `io.Copy` + `os.Rename` (temp → cible) plutot que `os.Rename` direct du staging vers l'exe. Raison : staging et exe peuvent etre sur des volumes differents (cross-device rename impossible)
3. **Persistance version staged :** Fichier `staged_version.txt` dans le stagingDir. Le binaire seul ne suffit pas — il faut savoir quelle version est staged pour l'afficher dans le tray et le comparer
4. **Re-verification au demarrage :** Toujours re-verifier SHA256 + Ed25519 avant d'installer. Le fichier staged a pu etre corrompu/modifie entre le download et le redemarrage
5. **Backup .bak :** Conserve jusqu'a confirmation que la nouvelle version fonctionne (tunnel connecte). Story 6.3 utilisera ce backup pour le rollback automatique
6. **Pas de redemarrage force :** Le service ne se redemarrage PAS lui-meme pour appliquer la MAJ. L'installation se fait au prochain demarrage naturel (reboot OS, redemarrage service par l'utilisateur, ou redemarrage par le service manager)

### Copie Atomique du Binaire

```go
func atomicCopyFile(src, dst string) error {
    tmpDst := dst + ".tmp"

    srcFile, err := os.Open(src)
    if err != nil {
        return fmt.Errorf("updater: install: open source: %w", err)
    }
    defer srcFile.Close()

    // Creer fichier temporaire a cote de la destination
    dstFile, err := os.OpenFile(tmpDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
    if err != nil {
        return fmt.Errorf("updater: install: create temp: %w", err)
    }

    if _, err := io.Copy(dstFile, srcFile); err != nil {
        dstFile.Close()
        os.Remove(tmpDst)
        return fmt.Errorf("updater: install: copy: %w", err)
    }

    if err := dstFile.Close(); err != nil {
        os.Remove(tmpDst)
        return fmt.Errorf("updater: install: close temp: %w", err)
    }

    // Rename atomique (meme volume garanti car tmpDst est adjacent a dst)
    if err := os.Rename(tmpDst, dst); err != nil {
        os.Remove(tmpDst)
        return fmt.Errorf("updater: install: rename: %w", err)
    }

    return nil
}
```

**IMPORTANT Windows :** `os.Rename` sur Windows echoue si le fichier destination est verrouille par un autre process. Au demarrage du service, l'ancien binaire n'est pas encore en cours d'execution (c'est le NOUVEAU process qui execute l'installation). Si un antivirus verrouille le fichier temporairement, implementer un retry avec backoff court (3 tentatives, 500ms entre chaque).

### Notification Tray — Integration avec le Polling Existant

Le tray poll le service toutes les X secondes via IPC `get_status`. La detection de MAJ s'integre dans ce flux existant :

```go
// Dans la boucle de polling du tray (tray.go)
resp, err := t.client.SendContext(ctx, ipc.Request{Action: ipc.ActionGetStatus})
if err == nil && resp.UpdateVersion != "" && resp.UpdateStatus == ipc.StatusUpdateReady {
    t.NotifyUpdateReady(resp.UpdateVersion)
}
```

Pas besoin de notification push separee — le polling existant detecte la MAJ disponible. L'action IPC `notify_update` est ajoutee pour completude mais le flux principal passe par le polling `get_status`.

**Modification du handler `get_status` :**
Le handler `handleGetStatus` dans `ipchandler/handler.go` doit aussi retourner `UpdateVersion` et `UpdateStatus` si une MAJ est disponible. Cela evite au tray de faire 2 requetes IPC (get_status + update_status).

### Integration avec l'Updater Existant (Story 6.1)

**Modifications necessaires a l'Updater :**
- Apres `VerifyStagedUpdate` reussi, appeler `writeStagedVersion(stagingDir, version)` pour persister la version
- Le callback `onUpdateReady(version)` reste inchange — deja appele apres verification reussie

**Modifications necessaires au service :**
- Ajouter `installer *updater.Installer` dans `Program`
- Ajouter `pendingUpdateVersion string` dans `Program` (set par callback onUpdateReady, lu par IPC)
- Ajouter `lastInstallError string` dans `Program` (set si Install echoue, lu par IPC)
- Ajouter `installedVersion string` dans `Program` (set si Install reussit, lu par IPC)

### Patterns Go a Respecter

- **Package** : fichiers dans `internal/updater/` (meme package que story 6.1)
- **Nouveau fichier** : `installer.go` + `installer_test.go`
- **Erreurs** : `fmt.Errorf("updater: install: %w", err)`, `fmt.Errorf("updater: install: backup: %w", err)`
- **Erreurs sentinelles** : `ErrNoStagedUpdate`, `ErrReadOnlyTarget`, `ErrBackupFailed`
- **Concurrence** : `sync.Mutex` pour `pendingUpdateVersion`/`lastInstallError`/`installedVersion` sur Program (accede par IPC handler et service)
- **Tests** : table-driven, fichiers temporaires via `t.TempDir()`, noms `TestInstaller_Install_Success`, `TestInstaller_Install_BackupFailure`, `TestInstaller_HasStagedUpdate`
- **Aucun log client** — erreurs propagees via IPC
- **Aucune dependance nouvelle** — tout est standard library

### Intelligence Story 6.1 — Learnings Critiques

**Patterns etablis a reutiliser :**
- `StagedUpdate` struct existante dans `updater` — etendre avec `VersionFile string`
- `Verifier` existant pour re-verification au demarrage — reutiliser `VerifyStagedUpdate()`
- `atomic.Bool` pour `IsDownloading()` — pattern reutilisable pour `IsInstalling()`
- Extension IPC backward-compatible via nouvelles constantes (pattern des stories precedentes)
- Helpers `config.StagingDir()` deja implementes dans story 6.1

**Issues corrigees en code review de 6.1 — NE PAS reproduire :**
- SSRF → la validation URL est dans le Checker, pas dans l'Installer (pas d'URL ici, que des fichiers locaux)
- Fuite de ressource → toujours `defer file.Close()` dans les operations fichier
- Data race → utiliser mutex ou atomic pour tout etat partage entre goroutines

**Debug findings de 6.1 :**
- `golang.org/x/time` a du etre ajoutee explicitement (pas transitive via quic-go) — deja fait, pas d'impact ici
- Tests DNS Windows et STUN relay pre-existants en echec — non lies, ignorer

### Securite — Points Critiques

1. **Re-verification obligatoire** : TOUJOURS re-verifier SHA256 + Ed25519 du binaire staged AVANT installation. Le fichier a pu etre modifie entre le download (story 6.1) et le redemarrage
2. **Permissions du backup** : Le fichier .bak doit avoir les memes permissions que l'original
3. **Ecriture atomique** : Jamais ecrire directement sur l'executable — toujours copier vers .tmp puis rename
4. **Verification post-copie** : Apres installation, `os.Stat` pour confirmer la taille et les permissions du nouveau binaire
5. **Cleanup staging** : Supprimer TOUS les fichiers staged apres installation reussie (binaire, checksums, signature, staged_version.txt)
6. **Pas d'execution du staged** : Ne JAMAIS executer le binaire staged directement. Seule l'installation (copie vers l'emplacement de l'executable courant) est permise

### Project Structure Notes

Nouveaux fichiers a creer :
```
internal/
├── updater/
│   ├── installer.go              # NOUVEAU — Installer, Install(), HasStagedUpdate(), Rollback()
│   └── installer_test.go         # NOUVEAU — Tests installer
```

Fichiers existants modifies :
- `internal/updater/updater.go` — Appel `writeStagedVersion()` apres verification reussie
- `internal/updater/updater_test.go` — Test ecriture staged_version.txt
- `internal/service/service.go` — Ajout champ `installer`, cycle pre-demarrage install, champs `pendingUpdateVersion`/`lastInstallError`/`installedVersion`, accesseur `Installer()`
- `internal/service/service_test.go` — Tests cycle demarrage avec/sans MAJ
- `internal/tray/tray.go` — Ajout `menuUpdateReady`, `NotifyUpdateReady()`, `ClearUpdateNotification()`, detection dans polling
- `internal/tray/tray_test.go` — Tests notification
- `internal/ipc/messages.go` — Ajout `ActionNotifyUpdate`, `StatusInstalled`, `StatusInstallFailed`, champs `InstalledVersion`/`InstallError` dans Response
- `internal/ipchandler/handler.go` — Extension handler `update_status`, enrichissement handler `get_status` avec info MAJ
- `internal/ipchandler/handler_test.go` — Tests handlers etendus

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 6 — Story 6.2]
- [Source: _bmad-output/planning-artifacts/architecture.md#Core Architectural Decisions — Deferred: Auto-update Phase 2]
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns — Error Handling]
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns — Concurrency]
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure & Boundaries]
- [Source: _bmad-output/planning-artifacts/prd.md — Phase 2: Auto-update en arriere-plan, Rollback automatique]
- [Source: _bmad-output/implementation-artifacts/6-1-verification-de-version-et-telechargement-en-arriere-plan.md — Story 6.1 complete]
- [Source: internal/updater/updater.go — Updater, StagedUpdate, onUpdateReady callback]
- [Source: internal/updater/verify.go — Verifier, VerifyStagedUpdate()]
- [Source: internal/service/service.go — Program.run(), shutdown(), lifecycle]
- [Source: internal/tray/tray.go — setupMenu(), restoreTooltipAfter(), polling IPC]
- [Source: internal/ipc/messages.go — Action/Status constants, Request/Response structs]
- [Source: internal/ipchandler/handler.go — handleCheckUpdate, handleUpdateStatus]
- [Source: internal/config/paths_windows.go — StagingDir()]
- [Source: internal/config/paths_unix.go — StagingDir()]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Tous les tests passent sans regression sur les 5 packages modifies
- TestInstaller_Install_ReadOnly skip sur Windows (attendu — permissions POSIX uniquement)

### Completion Notes List

- **Task 1** : Cree `internal/updater/installer.go` avec Installer, Install(), HasStagedUpdate(), Rollback(), CleanupBackup(), writeStagedVersion(), atomicCopyFile(). 12 tests dans `installer_test.go`, tous passent.
- **Task 2** : Integre l'installer au demarrage du service via `tryInstallStagedUpdate()` avant le tunnel. Ajoute champs `installer`, `pendingUpdateVersion`, `installedVersion`, `lastInstallError` avec mutex. Callback `onUpdateReady` connecte. CleanupBackup appele apres connexion tunnel reussie.
- **Task 3** : Ajoute appel `writeStagedVersion()` dans `CheckAndDownload()` apres verification reussie. Champ `VersionFile` ajoute a `StagedUpdate`. Test verifie la creation du fichier.
- **Task 4** : Ajoute `menuUpdateReady` dans le tray, cree entre "Verifier fuite WebRTC" et le separateur, cache par defaut. Methodes `NotifyUpdateReady(version)` et `ClearUpdateNotification()` implementees. 3 tests ajoutees.
- **Task 5** : Constantes IPC `ActionNotifyUpdate`, `StatusInstalled`, `StatusInstallFailed` ajoutees. Champs `InstalledVersion`/`InstallError` dans Response. Handler `handleUpdateStatus` etendu pour retourner les infos d'installation. `handleGetStatus` enrichi avec info MAJ. Detection polling dans tray. 3 tests handler ajoutes.
- **Task 6** : Mode portable compatible par design (os.Executable() fonctionne identiquement). `ErrReadOnlyTarget` et `checkWritable()` implementes dans installer.go.

### Change Log

- 2026-03-11 : Implementation complete story 6.2 — module Installer, integration service, persistence version staged, notification tray, extension IPC, gestion portable
- 2026-03-11 : Code review fixes — H1: verifier obligatoire dans Install() (securite), H2: renameWithRetry pour restauration backup (Windows), H3: separation cle UpdatePubKey/RelayPubKey (separation des secrets), M2: dedup NotifyUpdateReady dans polling, M4: ajout tests contexte annule et nil verifier
- 2026-03-12 : Code review #2 — M1: elimination duplication retry dans atomicCopyFile (utilise renameWithRetry au lieu d'une boucle inline)

### File List

**Nouveaux fichiers :**
- internal/updater/installer.go
- internal/updater/installer_test.go

**Fichiers modifies :**
- internal/updater/downloader.go (ajout champ VersionFile a StagedUpdate)
- internal/updater/updater.go (appel writeStagedVersion apres verification)
- internal/updater/updater_test.go (ajout test WritesStagedVersion + import filepath)
- internal/service/service.go (ajout installer, champs update state, tryInstallStagedUpdate, CleanupBackup, accesseurs, callback onUpdateReady)
- internal/service/service_test.go (ajout tests cycle demarrage, accesseurs, update enabled)
- internal/tray/tray.go (ajout menuUpdateReady, NotifyUpdateReady, ClearUpdateNotification, detection polling)
- internal/tray/tray_test.go (ajout tests NotifyUpdateReady, ClearUpdateNotification)
- internal/ipc/messages.go (ajout ActionNotifyUpdate, StatusInstalled, StatusInstallFailed, champs InstalledVersion/InstallError dans Response)
- internal/ipchandler/handler.go (extension handleUpdateStatus et handleGetStatus)
- internal/ipchandler/handler_test.go (ajout tests handler etendus)
