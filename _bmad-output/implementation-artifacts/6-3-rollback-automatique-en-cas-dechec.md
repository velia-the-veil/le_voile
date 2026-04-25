# Story 6.3: Rollback automatique en cas d'echec

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que Le Voile revienne automatiquement a la version precedente si une mise a jour echoue,
Afin de ne jamais perdre ma protection a cause d'une mise a jour defectueuse.

## Acceptance Criteria

1. **Given** une nouvelle version installee au demarrage
   **When** le service echoue a demarrer dans les 30 secondes (crash, tunnel non etabli)
   **Then** le rollback automatique restaure le binaire precedent et redemarre le service

2. **Given** un rollback effectue
   **When** le service redemarre avec l'ancienne version
   **Then** la protection est retablie normalement et une notification tray informe "Mise a jour annulee — version precedente restauree"

3. **Given** un rollback effectue
   **When** la version defectueuse est identifiee
   **Then** elle est marquee comme echouee et ne sera pas re-telechargee jusqu'a la prochaine release

## Tasks / Subtasks

- [x] Task 1 : Fichier de persistance d'etat rollback (AC: #1, #3)
  - [x] 1.1 Creer `internal/updater/rollback.go` — struct `RollbackState` avec champs : `JustInstalled bool`, `InstalledVersion string`. Note implementation : `FailedVersion` est stocke dans `failed_version.txt` (fichier separe, pas dans la struct JSON), `RollbackOccurred` et `RollbackReason` sont des champs en memoire sur `Program` proteges par `updateMu` (non persistants — intentionnel, notification one-shot par session de service)
  - [x] 1.2 Implementer `WriteRollbackState(dir string, state *RollbackState) error` — ecrit `rollback_state.json` dans le stagingDir. Format JSON. Prefixe erreurs `"updater: rollback state:"`. Utiliser ecriture atomique (tmp + rename)
  - [x] 1.3 Implementer `ReadRollbackState(dir string) (*RollbackState, error)` — lit `rollback_state.json`. Retourne nil si fichier absent (pas une erreur). Parse JSON
  - [x] 1.4 Implementer `ClearRollbackState(dir string) error` — supprime `rollback_state.json`. Idempotent (pas d'erreur si absent)
  - [x] 1.5 Implementer `WriteFailedVersion(dir string, version string) error` — ecrit `failed_version.txt` dans le stagingDir. Contient la version defectueuse. Empechera le re-telechargement
  - [x] 1.6 Implementer `ReadFailedVersion(dir string) (string, error)` — lit `failed_version.txt`. Retourne "" si absent
  - [x] 1.7 Implementer `ClearFailedVersion(dir string) error` — supprime `failed_version.txt`. Appele quand une NOUVELLE release (version differente) est disponible
  - [x] 1.8 Ecrire tests dans `rollback_test.go` — ecriture/lecture/suppression RollbackState, ecriture/lecture/suppression FailedVersion, fichier absent retourne nil/"", ecriture atomique (pas de corruption)

- [x] Task 2 : Detection d'echec post-installation et rollback automatique (AC: #1)
  - [x] 2.1 Dans `internal/service/service.go`, modifier `tryInstallStagedUpdate()` : apres `Install()` reussi, appeler `WriteRollbackState(stagingDir, &RollbackState{JustInstalled: true, InstalledVersion: staged.Version})` pour marquer qu'un rollback est possible
  - [x] 2.2 Creer methode `tryRollbackIfNeeded(ctx context.Context, tunnelErr error)` sur `Program` — logique de decision rollback :
    1. Lire `RollbackState` depuis stagingDir
    2. Si `JustInstalled == false` ou state nil → retourner (pas de rollback, echec tunnel normal)
    3. Verifier que le backup `.bak` existe via `os.Stat(executablePath + ".bak")`
    4. Appeler `Installer.Rollback()` pour restaurer le binaire precedent
    5. Appeler `WriteFailedVersion(stagingDir, state.InstalledVersion)` pour bloquer le re-telechargement
    6. Appeler `ClearRollbackState(stagingDir)` pour nettoyer l'etat
    7. Mettre a jour les champs `rollbackOccurred`, `rollbackVersion`, `rollbackReason` sur `Program`
    8. Ecrire sur serviceStderr : `"updater: rollback: restored previous version (v%s failed: %v)\n"`
  - [x] 2.3 Dans `run()`, apres l'echec de `TunnelClient.Connect()` (actuellement service log erreur et quitte), ajouter l'appel a `tryRollbackIfNeeded(ctx, err)`. Si rollback reussi, NE PAS quitter — tenter de reconnecter le tunnel avec l'ancien binaire. Si rollback echoue, quitter normalement (comportement actuel)
  - [x] 2.4 Dans `run()`, si tunnel reussit, appeler `ClearRollbackState(stagingDir)` pour confirmer que la nouvelle version fonctionne (avant `CleanupBackup`)
  - [x] 2.5 Ajouter champs sur `Program` : `rollbackOccurred bool`, `rollbackVersion string`, `rollbackReason string` — proteges par `updateMu`
  - [x] 2.6 Ajouter accesseurs thread-safe : `RollbackOccurred() bool`, `RollbackVersion() string`, `RollbackReason() string`
  - [x] 2.7 Ecrire tests : `TestService_TunnelFailure_AfterInstall_TriggersRollback` — mock installer avec staged update, mock tunnel qui echoue, verifier que Rollback() est appele et etat mis a jour. `TestService_TunnelFailure_NoInstall_NoRollback` — tunnel echoue sans installation recente, pas de rollback. `TestService_TunnelSuccess_ClearsRollbackState` — tunnel reussit apres install, rollback state nettoye

- [x] Task 3 : Blocage du re-telechargement de la version defectueuse (AC: #3)
  - [x] 3.1 Dans `internal/updater/updater.go`, modifier `CheckAndDownload()` : avant de telecharger, lire `ReadFailedVersion(stagingDir)`. Si la version disponible sur GitHub == failedVersion, ne PAS telecharger, retourner nil (skip silencieux)
  - [x] 3.2 Dans `CheckAndDownload()`, si la version disponible est DIFFERENTE de failedVersion, appeler `ClearFailedVersion(stagingDir)` avant de telecharger (nouvelle release, on retente)
  - [x] 3.3 Ecrire tests : `TestUpdater_SkipsFailedVersion` — version echouee presente, meme version sur GitHub, skip telecharge. `TestUpdater_ClearsFailedVersion_NewRelease` — version echouee presente, version differente sur GitHub, clear + telecharge

- [x] Task 4 : Extension IPC — statut rollback (AC: #2)
  - [x] 4.1 Ajouter constante IPC dans `messages.go` : `StatusRollback = "rollback"` — indique qu'un rollback vient de se produire
  - [x] 4.2 Ajouter champs dans `Response` : `RollbackVersion string` (version qui a echoue), `RollbackReason string` (raison de l'echec)
  - [x] 4.3 Modifier le handler `handleUpdateStatus` dans `ipchandler/handler.go` : ajouter en priorite la plus haute (avant StatusInstalled) — si `RollbackOccurred()` retourne true → retourner `StatusRollback` avec `RollbackVersion` et `RollbackReason`
  - [x] 4.4 Modifier `handleGetStatus` pour inclure les champs rollback dans la reponse globale (meme pattern que InstalledVersion/InstallError)
  - [x] 4.5 Ecrire tests pour le handler : cas rollback present, cas rollback absent, priorite rollback > installed

- [x] Task 5 : Notification tray — rollback (AC: #2)
  - [x] 5.1 Dans `internal/tray/tray.go`, dans la boucle de polling status, detecter `UpdateStatus == StatusRollback` dans la reponse IPC
  - [x] 5.2 Implementer methode `NotifyRollback(version string)` sur `Tray` :
    - Afficher tooltip temporaire : "Mise a jour v{version} annulee — version precedente restauree"
    - Si `menuUpdateReady` est visible, le masquer (la MAJ n'est plus pertinente)
    - Restaurer le tooltip normal apres 15 secondes via `restoreTooltipAfter()`
  - [x] 5.3 Ajouter deduplication : champ `notifiedRollbackVer string` sur `Tray` (protege par `mu`). Ne notifier qu'une seule fois par version rollback
  - [x] 5.4 Ecrire tests : `TestTray_NotifyRollback` — tooltip modifie, menu update cache. `TestTray_NotifyRollback_Deduplicated` — pas de double notification

- [x] Task 6 : Timeout de demarrage configurable — 30 secondes (AC: #1)
  - [x] 6.1 Dans `run()`, encapsuler l'appel `TunnelClient.Connect()` avec un `context.WithTimeout(ctx, 30*time.Second)` lorsqu'une installation vient d'etre effectuee (`installedVersion != ""`). En mode normal (pas d'installation), garder le comportement actuel (pas de timeout force)
  - [x] 6.2 Si le timeout expire, traiter comme un echec tunnel et appeler `tryRollbackIfNeeded(ctx, context.DeadlineExceeded)`
  - [x] 6.3 Definir la constante `rollbackTimeout = 30 * time.Second` dans service.go (pas configurable en config TOML pour le MVP, juste une constante)
  - [x] 6.4 Ecrire test : `TestService_TunnelTimeout_AfterInstall_TriggersRollback` — tunnel ne repond pas dans les 30s, rollback declenche

## Dev Notes

### Contexte Technique Critique

**MECANISME DE ROLLBACK — FLUX COMPLET**

Le rollback se declenche dans la fenetre entre l'installation du nouveau binaire et la confirmation de fonctionnement (tunnel connecte). Sequence :

```
Service demarre (kardianos/service -> Program.run())
  -> 1. tryInstallStagedUpdate()
       -> Install() reussit
       -> WriteRollbackState(JustInstalled=true, version)  # NOUVEAU
       -> installedVersion = version
  -> 2. TunnelClient.Connect(ctx) avec timeout 30s si JustInstalled
       -> ECHEC (timeout, crash, erreur reseau)
       -> tryRollbackIfNeeded(ctx, err)  # NOUVEAU
           -> ReadRollbackState() -> JustInstalled == true
           -> Stat(.bak) -> backup existe
           -> Installer.Rollback() -> restaure .bak -> exe
           -> WriteFailedVersion(version) -> bloque re-telechargement
           -> ClearRollbackState()
           -> rollbackOccurred = true
       -> Retenter TunnelClient.Connect() avec l'ancien binaire
  -> 3. Si tunnel reussit (ancien ou nouveau binaire)
       -> ClearRollbackState()  # NOUVEAU
       -> CleanupBackup()
       -> Continuer normalement
```

**DECISIONS CLES :**

1. **Fichier d'etat `rollback_state.json`** : Persiste entre les redemarrages potentiels. Si le service crash pendant le rollback lui-meme, l'etat reste sur disque et un prochain demarrage verra qu'aucune installation recente n'a eu lieu (le .bak a ete restaure). Alternative consideree : champ en memoire — rejete car un crash service perd l'etat
2. **Fichier `failed_version.txt`** : Persiste indefiniment jusqu'a ce qu'une NOUVELLE version soit disponible sur GitHub. Evite la boucle : telecharger -> installer -> echouer -> rollback -> telecharger la meme version
3. **Timeout 30 secondes** : Specifique a un demarrage post-installation. En fonctionnement normal, le tunnel utilise son propre mecanisme de reconnexion (backoff exponentiel). Le timeout de 30s est un garde-fou pour detecter un binaire defectueux rapidement
4. **Pas de redemarrage du service** : Le rollback restaure le binaire puis RETENTE la connexion tunnel dans le meme processus. Le prochain redemarrage du service utilisera naturellement l'ancien binaire restaure
5. **Priorite IPC rollback > installed** : Si un rollback vient de se produire dans la meme session, c'est l'information la plus pertinente pour l'utilisateur. Le statut "installed" n'est plus valide puisque l'installation a ete annulee

### Architecture d'Etat Rollback

```
stagingDir/
  rollback_state.json    # Temporaire : present entre Install() et confirmation tunnel
  failed_version.txt     # Persistant : bloque re-telechargement version defectueuse
```

**rollback_state.json** :
```json
{
  "just_installed": true,
  "installed_version": "2.1.0"
}
```

**failed_version.txt** :
```
2.1.0
```

### Integration avec le Code Existant

**Fichiers de Story 6.2 reutilises directement :**
- `Installer.Rollback()` — deja implemente dans `installer.go:173-187`. Restaure `.bak` vers l'executable. NE PAS modifier cette methode
- `Installer.CleanupBackup()` — deja implemente. Continue a etre appele apres confirmation tunnel reussi
- Champs `updateMu`, `installedVersion`, `lastInstallError` sur `Program` — etendre avec champs rollback
- Pattern IPC status priority dans `handleUpdateStatus` — etendre avec cas rollback
- Pattern polling tray dans `tray.go` — etendre avec detection rollback
- Constantes IPC dans `messages.go` — ajouter `StatusRollback`

**Modifications a `Installer.Rollback()` — AUCUNE :**
La methode existante fait exactement ce qu'il faut (rename .bak -> exe). Le nettoyage des fichiers staged et la gestion de `failed_version.txt` sont geres par le code appelant dans `service.go`, pas par l'Installer

**ATTENTION — Ce que tryRollbackIfNeeded() ne doit PAS faire :**
- NE PAS appeler `CleanupBackup()` — le backup vient d'etre restaure, il n'y a plus de .bak
- NE PAS modifier `installedVersion` — il doit rester a la version defectueuse pour le debugging
- NE PAS supprimer les fichiers staged — `Install()` les a deja nettoyes (etape 5 du flux d'installation)

### Patterns Go a Respecter

- **Package** : fichiers dans `internal/updater/` pour `rollback.go` (meme package que `installer.go`)
- **Nouveau fichier** : `rollback.go` + `rollback_test.go` dans `internal/updater/`
- **Erreurs** : `fmt.Errorf("updater: rollback state: %w", err)`, `fmt.Errorf("updater: failed version: %w", err)`
- **JSON** : `encoding/json` standard, champs `snake_case` dans les tags JSON
- **Concurrence** : Champs rollback sur `Program` proteges par `updateMu` existant (meme mutex que `installedVersion`)
- **Tests** : table-driven, fichiers temporaires via `t.TempDir()`, noms `TestRollbackState_WriteRead`, `TestFailedVersion_WriteRead`, `TestService_TunnelFailure_AfterInstall_TriggersRollback`
- **Aucun log client** — erreurs propagees via IPC sauf `serviceStderr` pour le debug critique (rollback est critique)
- **Aucune dependance nouvelle** — tout est `encoding/json`, `os`, `fmt`, standard library

### Intelligence Story 6.2 — Learnings Critiques

**Patterns etablis a reutiliser :**
- `atomicCopyFile()` dans `installer.go` — reutiliser le meme pattern d'ecriture atomique pour `rollback_state.json` (tmp + rename)
- `renameWithRetry()` dans `installer.go` — reutiliser pour les operations de rename sur Windows (antivirus locks)
- Extension IPC backward-compatible via nouvelles constantes — meme approche pour `StatusRollback`
- Pattern de deduplication tray (`notifiedVer`) — reutiliser avec `notifiedRollbackVer`
- Accesseurs thread-safe avec `updateMu.Lock/Unlock` — meme pattern pour `RollbackOccurred()`, `RollbackVersion()`
- Pattern `handleUpdateStatus` priority chain — inserer rollback check en tete

**Issues corrigees en code review de 6.2 — NE PAS reproduire :**
- Verifier obligatoirement dans Install() avant toute operation — deja fait, ne pas supprimer
- `renameWithRetry` pour restauration backup Windows — deja utilise dans Rollback() via os.Rename (simple rename, pas cross-device)
- Separation cle `UpdatePubKey`/`RelayPubKey` — deja fait, pas d'impact ici
- Dedup `NotifyUpdateReady` dans polling — appliquer le meme pattern pour `NotifyRollback`

**Debug findings de 6.2 :**
- Tests DNS Windows et STUN relay pre-existants en echec — non lies, ignorer
- `TestInstaller_Install_ReadOnly` skip sur Windows — attendu (permissions POSIX uniquement)

### Securite — Points Critiques

1. **Ecriture atomique de rollback_state.json** : Toujours ecrire via fichier temporaire + rename pour eviter corruption en cas de crash pendant l'ecriture
2. **Pas d'execution du binaire staged** : Le rollback restaure le .bak, il ne re-execute rien. Le binaire restaure sera charge au prochain demarrage naturel du process
3. **failed_version.txt en texte brut** : Pas de donnees sensibles — juste un numero de version. Pas besoin de chiffrement
4. **Race condition rollback** : Le rollback ne peut se produire que dans `run()` qui est single-threaded par rapport au flux de demarrage. Pas de risque de double rollback

### Project Structure Notes

Nouveaux fichiers a creer :
```
internal/
  updater/
    rollback.go              # NOUVEAU — RollbackState, WriteRollbackState, ReadRollbackState, etc.
    rollback_test.go         # NOUVEAU — Tests rollback state et failed version
```

Fichiers existants modifies :
- `internal/updater/updater.go` — Ajout check `ReadFailedVersion()` avant telechargement, `ClearFailedVersion()` quand nouvelle release
- `internal/updater/updater_test.go` — Tests skip failed version, clear failed version
- `internal/service/service.go` — Ajout `tryRollbackIfNeeded()`, modification `tryInstallStagedUpdate()` (ecrire rollback state), modification `run()` (timeout 30s post-install, appel rollback, retry tunnel), champs `rollbackOccurred`/`rollbackVersion`/`rollbackReason`, accesseurs
- `internal/service/service_test.go` — Tests rollback apres echec tunnel, pas de rollback sans install, clear state apres succes
- `internal/tray/tray.go` — Ajout `NotifyRollback()`, `notifiedRollbackVer`, detection dans polling
- `internal/tray/tray_test.go` — Tests NotifyRollback, deduplication
- `internal/ipc/messages.go` — Ajout `StatusRollback`, champs `RollbackVersion`/`RollbackReason` dans Response
- `internal/ipchandler/handler.go` — Extension `handleUpdateStatus` avec priorite rollback, extension `handleGetStatus` avec champs rollback
- `internal/ipchandler/handler_test.go` — Tests handler rollback

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 6 — Story 6.3]
- [Source: _bmad-output/planning-artifacts/architecture.md#Core Architectural Decisions — Deferred: Auto-update Phase 2]
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns — Error Handling]
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns — Concurrency]
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure & Boundaries]
- [Source: _bmad-output/planning-artifacts/prd.md — Phase 2: Rollback automatique de mise a jour]
- [Source: _bmad-output/implementation-artifacts/6-2-installation-au-redemarrage-et-notification-tray.md — Story 6.2 complete]
- [Source: internal/updater/installer.go — Installer, Install(), Rollback(), CleanupBackup()]
- [Source: internal/updater/updater.go — Updater.CheckAndDownload(), onUpdateReady callback]
- [Source: internal/service/service.go — Program.run(), tryInstallStagedUpdate(), updateMu]
- [Source: internal/tray/tray.go — NotifyUpdateReady(), polling, restoreTooltipAfter()]
- [Source: internal/ipc/messages.go — StatusInstalled, StatusInstallFailed, Response struct]
- [Source: internal/ipchandler/handler.go — handleUpdateStatus priority chain, handleGetStatus]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Tests DNS Windows (TestParseDNSFromNetsh_GarbageOutput, TestWindowsManager_*) pre-existants en echec — non lies a cette story
- TestClient_SendSTUNRelay pre-existant en echec — non lie a cette story
- TestInstaller_Install_ReadOnly skip sur Windows — attendu (permissions POSIX)

### Completion Notes List

- Task 1: Cree `internal/updater/rollback.go` avec RollbackState struct, WriteRollbackState/ReadRollbackState/ClearRollbackState (ecriture atomique tmp+rename via renameWithRetry), WriteFailedVersion/ReadFailedVersion/ClearFailedVersion. 10 tests dans `rollback_test.go` — tous passent.
- Task 2: Modifie `service.go` — ajout champs rollbackOccurred/rollbackVersion/rollbackReason sur Program avec accesseurs thread-safe, WriteRollbackState apres Install() reussi dans tryInstallStagedUpdate(), methode tryRollbackIfNeeded() avec logique complete (read state, verify backup, rollback, write failed version, clear state), modification run() pour appeler rollback apres echec tunnel et ClearRollbackState apres succes. Exporte NewInstallerWithPath et ExecutablePath() sur Installer pour les tests cross-package. 4 tests dans service_test.go.
- Task 3: Modifie `updater.go` — ajout champ stagingDir sur Updater, verification ReadFailedVersion avant telechargement dans CheckAndDownload(), skip silencieux si meme version, ClearFailedVersion si nouvelle release. 2 tests dans updater_test.go.
- Task 4: Modifie `messages.go` — ajout StatusRollback constante, champs RollbackVersion/RollbackReason dans Response. Modifie `handler.go` — priorite rollback > installed dans handleUpdateStatus, inclusion rollback dans handleGetStatus. 3 tests dans handler_test.go.
- Task 5: Modifie `tray.go` — ajout champ notifiedRollbackVer pour deduplication, methode NotifyRollback() avec tooltip + masquage menuUpdateReady + restore apres 15s, detection StatusRollback dans boucle de polling. 4 tests dans tray_test.go.
- Task 6: Modifie `service.go` — constante rollbackTimeout = 30s, context.WithTimeout dans run() quand installedVersion != "" pour detecter binaire defectueux. 2 tests dans service_test.go.
- Code Review (2026-03-11): Corrections apres review adversariale — (1) AC#1 "redemarre le service" : ajout champ svc service.Service sur Program, stockage dans Start(), methode scheduleServiceRestart() qui trigger un vrai redemarrage OS via kardianos/service apres fermeture du canal done, le chemin rollback dans run() appelle maintenant scheduleServiceRestart() puis return (plus de retry inline). (2) Task 1.1 : description mise a jour pour refleter que FailedVersion/RollbackOccurred/RollbackReason ne sont pas dans la struct JSON (design deliberement different de la spec). (3) Tests handler : ajout SetRollbackState()/SetInstalledVersion() sur Program, tests handler rollback corriges (TestHandle_UpdateStatus_RollbackPresent, TestHandle_UpdateStatus_RollbackPriorityOverInstalled reel, TestHandle_GetStatus_WithRollback, TestHandle_UpdateStatus_Installed avec vrai contenu). (4) Ajout TestService_TryRollbackIfNeeded_RollbackFails pour le chemin echec rollback.

### Change Log

- 2026-03-11: Implementation complete de la story 6.3 — rollback automatique en cas d'echec de mise a jour. 6 tasks, 25+ tests ajoutes, 0 regressions sur les packages modifies.
- 2026-03-11: Code review — corrections HIGH/MEDIUM : redemarrage OS reel apres rollback (AC#1), tests handler rollback complets, test echec rollback, correction test UpdateStatus_Installed vide. Status: done.
- 2026-03-12: Code review #2 — corrections MEDIUM : (M1) cleanup tunnelClient=nil sur chemin rollback pour eviter fuite ressources, (M2) clear installedVersion apres rollback reussi pour eviter timeout 30s spurieux en cas de redemarrage in-process. Test mis a jour pour verifier le clear. 0 regressions.

### File List

- internal/updater/rollback.go (NOUVEAU)
- internal/updater/rollback_test.go (NOUVEAU)
- internal/updater/installer.go (MODIFIE — export NewInstallerWithPath, ajout ExecutablePath())
- internal/updater/installer_test.go (MODIFIE — NewInstallerWithPath rename)
- internal/updater/updater.go (MODIFIE — ajout stagingDir, check failed version dans CheckAndDownload)
- internal/updater/updater_test.go (MODIFIE — ajout TestUpdater_SkipsFailedVersion, TestUpdater_ClearsFailedVersion_NewRelease)
- internal/service/service.go (MODIFIE — rollback fields, tryRollbackIfNeeded, timeout 30s, WriteRollbackState, ClearRollbackState, scheduleServiceRestart, svc field, SetRollbackState, SetInstalledVersion)
- internal/service/service_test.go (MODIFIE — ajout tests rollback, accesseurs, timeout, TestService_TryRollbackIfNeeded_RollbackFails)
- internal/ipc/messages.go (MODIFIE — StatusRollback, RollbackVersion, RollbackReason)
- internal/ipchandler/handler.go (MODIFIE — rollback priority dans handleUpdateStatus et handleGetStatus)
- internal/ipchandler/handler_test.go (MODIFIE — tests handler rollback complets : RollbackPresent, RollbackPriorityOverInstalled reel, GetStatus_WithRollback, UpdateStatus_Installed corrige)
- internal/tray/tray.go (MODIFIE — NotifyRollback, notifiedRollbackVer, detection polling)
- internal/tray/tray_test.go (MODIFIE — ajout tests NotifyRollback, deduplication)
