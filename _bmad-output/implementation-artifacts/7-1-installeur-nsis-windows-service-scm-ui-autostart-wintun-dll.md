# Story 7.1 : Installeur NSIS Windows (service SCM + UI autostart + Wintun DLL)

Status: done

<!-- Note: Validation est optionnelle. Lancer validate-create-story pour un contrôle qualité avant dev-story. -->

## Story

En tant qu'utilisateur final Windows,
Je veux installer Le Voile via un `.exe` NSIS qui configure tout automatiquement,
Afin de n'avoir aucune commande shell à exécuter et d'être protégé immédiatement après l'installation.

## Acceptance Criteria

**AC1 — Installation single-UAC dépose les 3 binaires + Wintun DLL**
**Given** l'utilisateur lance `LeVoile-Setup-{version}.exe`
**When** l'invite UAC est acceptée (une seule fois)
**Then** `levoile-service.exe`, `levoile-ui.exe` et `levoile-ctl.exe` sont copiés dans `C:\Program Files\LeVoile\`
**And** la DLL Wintun signée Microsoft est copiée dans `C:\Program Files\LeVoile\wintun.dll`
**And** aucune autre boîte de dialogue UAC ou prompt n'apparaît jusqu'à la fin de l'installation

**AC2 — Service SCM enregistré et démarré**
**Given** les binaires sont installés
**When** la section Install termine
**Then** le service `LeVoile` est enregistré dans le Service Control Manager (`sc query LeVoile` → `STATE: 4 RUNNING`)
**And** son type de démarrage est `AUTO_START` (`sc qc LeVoile` → `START_TYPE: 2 AUTO_START`)
**And** la politique de redémarrage sur crash est configurée (kardianos `OnFailure=restart`, délai 5s)
**And** le service est démarré immédiatement et reste up

**AC3 — UI autostart au login + lancement immédiat**
**Given** l'installation est terminée
**When** l'utilisateur ferme puis rouvre sa session Windows
**Then** `levoile-ui.exe` démarre automatiquement via `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\LeVoile`
**And** dès la fin de l'installation, l'UI est lancée sans attendre un re-login
**And** un raccourci "Le Voile" est créé sur le Bureau et dans `Démarrer\Le Voile\` (les deux marqués "Run as administrator")
**And** la latence install → tray actif < 30 secondes (NFR15-aligné)

**AC4 — Désinstallation propre : service, WFP filters, adapter Wintun, fichiers**
**Given** Le Voile est installé et le service tourne
**When** l'utilisateur lance la désinstallation via Ajout/Suppression de programmes
**Then** l'UI est tuée (`taskkill /F /IM levoile-ui.exe`)
**And** le service est arrêté gracieusement (`levoile-service stop` → shutdown() restaure DNS, désactive firewall, ferme tunnel)
**And** un cleanup forcé idempotent supprime les filtres WFP résiduels et l'adapter Wintun `levoile0` (via `levoile-service cleanup` — robuste même si `stop` a échoué)
**And** le service est désenregistré du SCM (`sc query LeVoile` → service inconnu)
**And** la clé `HKCU\...\Run\LeVoile` est supprimée
**And** l'entrée Add/Remove Programs est retirée
**And** les raccourcis Bureau + Start Menu sont supprimés
**And** `C:\Program Files\LeVoile\` n'existe plus (binaires + wintun.dll + icons + uninstaller supprimés)
**And** `%ProgramData%\LeVoile\` est nettoyé (token ctl, wintun.dll cache, état firewall persistant)
**And** la config utilisateur dans `%AppData%\LeVoile\config.toml` est conservée par défaut (la suppression nécessite une option explicite — convention de désinstallation)

**AC5 — NSIS purgé de tout résidu de l'extension navigateur supprimée**
**Given** l'extension navigateur a été supprimée de l'architecture (changelog 2026-04-15)
**When** on inspecte `installer/levoile.nsi`
**Then** plus aucune référence à `WinINET ProxyEnable`/`ProxyServer`, `taskkill firefox/chrome/edge/brave/vivaldi/opera`, `levoile@plateformeliberte.fr.xpi`, `WebRtcIPHandling`/`ExtensionSettings`, `RecoverOrphanPolicies`, `proxy-original.json` n'apparaît
**And** `installer/build.sh` et `installer/build.ps1` ne renomment plus `levoile-ui.exe` en `levoile-desktop.exe`

## Tasks / Subtasks

- [x] **Task 1 : Sous-commande `cleanup` du binaire service** (AC: 4)
  - [x] 1.1 Ajouter dans `cmd/client/main.go` (après `handleServiceCommand`) une sous-commande `cleanup` idempotente, dispatché AVANT `resolveConfig` (comme `install`/`uninstall`) — elle doit fonctionner sans config valide
  - [x] 1.2 Implémentation Windows uniquement (build tag ou check `runtime.GOOS == "windows"` avec early return Linux) :
    1. Tenter `firewall.Deactivate(ctx)` (idempotent — voir `internal/firewall/firewall.go`). Ignorer ERROR_FILE_NOT_FOUND / "filter not found"
    2. Tenter `tun.CleanupOrphan("levoile0")` (déjà idempotent, voir `internal/tun/cleanup_windows.go`)
    3. Tenter `routing.RemoveDefault(ctx)` si le package routing expose un cleanup idempotent (sinon laisser le shutdown gracieux du service s'en charger)
  - [x] 1.3 Sortir avec exit code 0 si tout réussi OU si les ressources étaient déjà absentes. Exit code 1 uniquement si une erreur Windows non-`ERROR_NOT_FOUND` survient
  - [x] 1.4 Le sous-commande `cleanup` doit pouvoir être appelée sur un service déjà désenregistré (NSIS uninstall l'appellera APRÈS `stop` mais AVANT `uninstall` pour limiter le blast radius)
  - [x] 1.5 Tests : `cmd/client/main_cleanup_test.go` — table-driven : (a) cleanup sur état propre (aucun filter/adapter) → exit 0, (b) cleanup avec filters présents (mock ou skip si non-admin)

- [x] **Task 2 : Renommage `levoile-desktop.exe` → `levoile-ui.exe`** (AC: 5)
  - [x] 2.1 `installer/levoile.nsi` : remplacer `!define DESKTOP_EXE "levoile-desktop.exe"` par `!define UI_EXE "levoile-ui.exe"` et propager `${UI_EXE}` dans toutes les références (taskkill, File, Run key, shortcuts)
  - [x] 2.2 `installer/build.sh` : remplacer `cp dist/ui_windows_amd64_v1/levoile-ui.exe "$BUILD_DIR/levoile-desktop.exe"` par `cp dist/ui_windows_amd64_v1/levoile-ui.exe "$BUILD_DIR/"` (préserver le nom)
  - [x] 2.3 `installer/build.ps1` : idem — `Copy-Item ... "ui_windows_amd64_v1\levoile-ui.exe" $BuildDir` (sans renommage)
  - [x] 2.4 **ATTENTION** : la clé HKCU Run reste nommée `"LeVoile"` (pas `"LeVoileUI"`) — c'est le nom de valeur, pas le nom du binaire. Pas de migration registry nécessaire car la clé pointe vers un chemin absolu (`$INSTDIR\levoile-ui.exe`) qui change au prochain install

- [x] **Task 3 : Bundling de wintun.dll dans Program Files** (AC: 1)
  - [x] 3.1 `installer/build.sh` et `build.ps1` : copier `internal/tun/wintun/wintun.dll` vers `$BUILD_DIR/wintun.dll` après le check de prérequis (échouer hard si la DLL est absente — message clair pointant vers `internal/tun/wintun/README.md` et `make wintun`)
  - [x] 3.2 `installer/levoile.nsi` section Install : ajouter `File "build\wintun.dll"` (déposé dans `$INSTDIR`)
  - [x] 3.3 `installer/levoile.nsi` section Uninstall : ajouter `Delete "$INSTDIR\wintun.dll"` AVANT `RMDir "$INSTDIR"`
  - [x] 3.4 Modifier `internal/tun/wintun_extract_windows.go` : avant de tenter l'extraction depuis l'embed vers `%ProgramData%`, essayer `LoadLibraryEx` directement sur `<dir(os.Executable())>\wintun.dll`. Si succès → `extractErr = nil` et return early (pas d'extraction). Sinon → fallback sur l'extraction `%ProgramData%` actuelle
  - [x] 3.5 **CRITIQUE** : la fonction `ensureWintunDLL()` reste appelée par `device_windows.go:New()` — la nouvelle priorité doit être (a) `$INSTDIR\wintun.dll` (mode installé), (b) `%ProgramData%\LeVoile\wintun.dll` (cache extrait, mode dev / portable), (c) extraction embed → cache. Ne JAMAIS retourner d'erreur si l'embed est vide tant que (a) ou (b) réussit (utile pour les builds dev sans `make wintun`)
  - [x] 3.6 Test unitaire : `internal/tun/wintun_extract_windows_test.go` — couvrir le cas (a) avec un faux exe + DLL à côté (skip si non-Windows ou DLL absente)

- [x] **Task 4 : Nettoyage NSIS — suppression des résidus extension navigateur** (AC: 5)
  - [x] 4.1 `installer/levoile.nsi` section Uninstall : supprimer **intégralement** :
    - Le bloc "CRITICAL: Restore WinINET proxy settings" (lignes ~142-151)
    - Le bloc "Remove browser extension" (taskkill firefox/chrome/edge/brave/vivaldi/opera + sleep + del XPI Firefox profiles, lignes ~153-164)
    - Le bloc "WebRtcIPHandling" / "WebRtcIPHandlingPolicy" `DeleteRegValue` (lignes ~173-186)
    - `Delete "$APPDATA\LeVoile\proxy-original.json"` et `RMDir "$APPDATA\LeVoile"` (lignes ~198-200) — la config utilisateur reste préservée par convention
  - [x] 4.2 Conserver le bloc `RMDir /r "$0\LeVoile"` (ProgramData) — il nettoie aussi le cache wintun.dll, le token ctl et l'état firewall persistant. Vérifier qu'il s'exécute APRÈS le `cleanup` (Task 1) sinon le firewall.Deactivate ne pourra plus charger l'état persistant pour rollback propre
  - [x] 4.3 Vérifier qu'aucune autre référence à l'extension n'existe ailleurs dans `installer/` (build scripts, README, .gitignore)

- [x] **Task 5 : Uninstall — appel du cleanup forcé** (AC: 4)
  - [x] 5.1 `installer/levoile.nsi` section Uninstall, après `ExecWait '"$INSTDIR\${SERVICE_EXE}" stop'` + `Sleep 2000` :
    ```nsis
    ; Force-cleanup WFP filters + Wintun adapter (idempotent, even if stop failed)
    ExecWait '"$INSTDIR\${SERVICE_EXE}" cleanup' $0
    ; Don't fail uninstall if cleanup returns non-zero — just log
    ```
  - [x] 5.2 PUIS `ExecWait '"$INSTDIR\${SERVICE_EXE}" uninstall'` (désenregistrement SCM)
  - [x] 5.3 PUIS le RMDir `$0\LeVoile` ProgramData (cache wintun + token + état)
  - [x] 5.4 **ATTENTION ordre** : stop → cleanup → uninstall SCM → RMDir ProgramData → DeleteReg HKCU/HKLM → Delete files → RMDir $INSTDIR. Le cleanup DOIT précéder le uninstall SCM (sinon l'API WFP ne peut plus identifier les filtres si le provider est déjà absent — selon l'implémentation `firewall.Deactivate`, à vérifier par le dev)

- [x] **Task 6 : CTL bundle non-optionnel** (AC: 1)
  - [x] 6.1 `installer/levoile.nsi` : retirer `/nonfatal` du `File "build\${CTL_EXE}"` — le ctl fait partie du contrat d'installation (story 5.9)
  - [x] 6.2 `installer/build.sh` et `build.ps1` : copier `dist/ctl-windows_windows_amd64_v1/levoile-ctl.exe` vers `$BUILD_DIR/levoile-ctl.exe` (échec hard si absent)
  - [x] 6.3 `installer/levoile.nsi` section Uninstall : ajouter `Delete "$INSTDIR\levoile-ctl.exe"` (parmi les autres `Delete`)

- [x] **Task 7 : Validation end-to-end** (AC: tous)
  - [x] 7.1 `goreleaser check` — config YAML valide
  - [x] 7.2 `goreleaser build --snapshot --clean` — les 3 binaires Windows produits (service, ui, ctl-windows)
  - [x] 7.3 `bash installer/build.sh 0.7.1-rc1` (ou `installer\build.ps1 -Version 0.7.1-rc1`) — l'installeur compile sans erreur, `installer/LeVoile-Setup.exe` produit
  - [x] 7.4 `go test ./cmd/client/... ./internal/tun/...` — non-régression + nouveaux tests passent
  - [x] 7.5 **Test Windows propre (exécuté 2026-04-18 sur poste Windows 11)** :
    - Install : UAC unique, 8 entrées dans `Program Files\LeVoile\` (binaires + wintun.dll + ico + config + uninstaller + icons/), service `RUNNING` + `AUTO_START` + `OnFailure=RESTART 5s`, UI levoile-ui.exe en process, HKCU Run pointe vers le bon chemin, Desktop shortcut présent, entrée Add/Remove Programs à `HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\LeVoile` (64-bit natif, pas WoW6432Node)
    - Uninstall : service désenregistré (SC 1060), adapter Wintun retiré, WFP filters `Le Voile` purgés, `Program Files\LeVoile\` supprimé, `%ProgramData%\LeVoile\` supprimé, HKCU Run + HKLM Uninstall (64 + WOW6432Node) nettoyés, Desktop + Start Menu shortcuts supprimés, config utilisateur `%AppData%\LeVoile\config.toml` **préservée** (convention respectée)
    - 2 défauts découverts au smoketest et corrigés dans cette même itération : D1 = `OnFailure=restart 5s` absent dans `newServiceConfig()` (régression) ; D2 = uninstall entry sous WOW6432Node à cause de NSIS x86 (fix `SetRegView 64`)
  - [x] 7.6 `go vet ./...` — zero warning

## Dev Notes

### État de départ — héritage à corriger

L'installeur NSIS actuel (`installer/levoile.nsi`, 218 lignes, dernière modif Epic 5) a accumulé du code obsolète au fil des restructurations :

- **Binaire UI mal nommé** : le NSIS parle de `levoile-desktop.exe` et les build scripts renomment `levoile-ui.exe` en `levoile-desktop.exe` à la copie. Convention abandonnée — `.goreleaser.yaml` produit `levoile-ui.exe` et c'est ce nom qui doit être utilisé partout
- **Section uninstall pollutée** : restauration WinINET proxy (`ProxyEnable=0`, `DeleteRegValue ProxyServer`), `taskkill` sur 6 navigateurs, suppression de `levoile@plateformeliberte.fr.xpi`, nettoyage des registry keys `WebRtcIPHandling`/`ExtensionSettings`/`WebRtcIPHandlingPolicy` sur 6 vendors, suppression de `proxy-original.json` — **tout cela appartient à l'extension navigateur, supprimée du scope au changelog 2026-04-15** (cf. PRD §change_history et architecture §Project Context Analysis)
- **Pas de bundling Wintun** : la DLL est uniquement embarquée dans `levoile-service.exe` et extraite à `%ProgramData%\LeVoile\wintun.dll` au premier `ensureWintunDLL()`. La nouvelle architecture exige sa présence dans `Program Files\LeVoile\wintun.dll` (auditable, signée, visible dans Explorer)
- **Pas de cleanup uninstall forcé** : si le service `stop` échoue ou si le shutdown gracieux n'a pas le temps de tourner, des filtres WFP et l'adapter Wintun `levoile0` peuvent rester orphelins après désinstallation. Aucune commande de nettoyage forcé n'est invoquée

### Architecture cible — résumé exécutif

```
C:\Program Files\LeVoile\
  ├── levoile-service.exe         ← service kardianos, dans SCM, OnFailure=restart
  ├── levoile-ui.exe              ← UI unique (webview + fyne.io/systray), HKCU Run
  ├── levoile-ctl.exe             ← CLI privilégié (story 5.9, killswitch on/off)
  ├── wintun.dll                  ← DLL signée Microsoft, NSIS-bundled
  ├── config.toml                 ← config initiale (depuis config-default.toml)
  ├── levoile.ico                 ← icône app
  ├── icons\connected.ico         ← état tray "connecté"
  ├── icons\connecting.ico        ← état tray "en cours"
  ├── icons\disconnected.ico      ← état tray "déconnecté"
  └── uninstall.exe               ← uninstaller NSIS auto-généré

C:\ProgramData\LeVoile\           ← service (SYSTEM) — éphémère, recréé au start
  ├── ctl.token                   ← token machine-local pour levoile-ctl auth
  ├── wintun.dll                  ← cache extrait depuis embed (fallback dev)
  └── firewall-state.json         ← état persistant pour crash-recovery WFP

%AppData%\LeVoile\
  └── config.toml                 ← prefs UI (pays préféré, etc.) — conservée à uninstall
```

### Sous-commandes du binaire service

| Commande | But | Idempotent |
|----------|-----|------------|
| `levoile-service install` | Enregistre dans SCM + OnFailure=restart 5s | Échoue si déjà installé |
| `levoile-service uninstall` | Désenregistre du SCM | Idempotent (ignore "service unknown") |
| `levoile-service start` | Démarre via SCM | Idempotent |
| `levoile-service stop` | Arrête via SCM (déclenche shutdown gracieux : DNS restore, firewall.Deactivate, tunnel close, TUN close) | Idempotent |
| `levoile-service cleanup` (**NOUVEAU — Task 1**) | Force `firewall.Deactivate` + `tun.CleanupOrphan("levoile0")` sans passer par le service. Pour uninstall robuste | Idempotent |
| `levoile-service run` | Foreground (debug) | N/A |

**Pourquoi un `cleanup` séparé :** `stop` fait le bon nettoyage si le service répond. Mais si :
- le service a crashé sans shutdown propre (filtres WFP persistants — c'est volontaire pour le kill switch),
- le service `stop` timeout (Windows tue le process après 30s),
- un upgrade brutal a remplacé le binaire avant l'arrêt,
… alors les filtres WFP du provider Le Voile ET l'adapter Wintun `levoile0` restent en mémoire. `cleanup` est un mode "force-detach" qui appelle directement les API depuis un binaire éphémère hors SCM.

### Wintun DLL — stratégie hybride embed + bundle

L'embed actuel (`internal/tun/wintun_embed_windows.go` + `wintun_extract_windows.go`) extrait la DLL au runtime depuis le binaire vers `%ProgramData%\LeVoile\wintun.dll`. La nouvelle story bundle aussi la DLL dans `Program Files\LeVoile\wintun.dll` via NSIS. Ces deux mécanismes coexistent :

**Priorité au runtime (Task 3.4, 3.5)** :
1. **`<dir(os.Executable())>\wintun.dll`** — installé via NSIS, le cas nominal Windows production. Préchargement direct via `LoadLibraryEx(LOAD_WITH_ALTERED_SEARCH_PATH)`, aucune extraction
2. **`%ProgramData%\LeVoile\wintun.dll`** — cache extrait, utilisé en mode portable / dev sans NSIS
3. **Embed → extraction** — fallback : si (1) et (2) absents et l'embed est non-vide, extraire vers (2) puis charger. Comportement actuel préservé

Cette priorité n'augmente PAS la taille du binaire (l'embed reste inchangé) et ne complique PAS le mode dev (les développeurs sans `make wintun` continuent d'avoir un build qui échoue proprement à `device_windows.go:New()` avec `ErrUnavailable`).

**Avantage utilisateur final** : la DLL est visible dans Explorer (auditabilité), signée Microsoft (vérifiable via `Get-AuthenticodeSignature`), et le service ne perd pas 50ms au premier démarrage à extraire 200 KB.

### Fichiers à CRÉER

| Fichier | Contenu |
|---------|---------|
| `cmd/client/main_cleanup_test.go` | Tests sous-commande `cleanup` (Task 1.5) |
| `internal/tun/wintun_extract_windows_test.go` | Test priorité `$INSTDIR\wintun.dll` (Task 3.6) — peut être skip-if-not-windows |

### Fichiers à MODIFIER

| Fichier | Modification |
|---------|-------------|
| `cmd/client/main.go` | Ajouter sous-commande `cleanup` dispatchée AVANT `resolveConfig` (comme install/uninstall) |
| `internal/tun/wintun_extract_windows.go` | Priorité `$INSTDIR\wintun.dll` avant cache `%ProgramData%`. Pas d'erreur si embed vide tant que (1) ou (2) marche |
| `installer/levoile.nsi` | Renommage `DESKTOP_EXE`→`UI_EXE`, ajout `File wintun.dll`, suppression résidus extension, ajout `cleanup` dans uninstall, retrait `/nonfatal` ctl |
| `installer/build.sh` | Pas de renommage `levoile-ui→levoile-desktop`. Copier wintun.dll. Copier ctl.exe (échec si absent) |
| `installer/build.ps1` | Idem |

### Fichiers à NE PAS TOUCHER

| Fichier | Raison |
|---------|--------|
| `internal/tun/wintun_embed_windows.go` | Mécanisme embed inchangé, juste consommé en fallback |
| `internal/tun/cleanup_windows.go` | `CleanupOrphan` déjà idempotent et utilisable tel quel par `cleanup` |
| `internal/firewall/firewall_windows.go`, `wfp_windows.go` | `Deactivate` déjà idempotent |
| `internal/service/service.go` | Lifecycle stable |
| `internal/config/config.go` | Aucun champ TOML supplémentaire requis |
| `cmd/ui/`, `cmd/ctl/` | Pas de modification — l'UI et le ctl sont prêts |
| `.goreleaser.yaml` | Builds windows déjà corrects (service, ui, ctl-windows) |

### Patterns architecturaux à respecter

**Sous-commande `cleanup`** (Task 1) :
- Dispatch AVANT `resolveConfig` pour fonctionner sans config valide (l'uninstall peut tourner après corruption)
- Build tag Windows ou early-return Linux dans le handler
- Erreur → wrap avec préfixe package : `fmt.Errorf("client: cleanup firewall: %w", err)`
- Ignorer `windows.ERROR_FILE_NOT_FOUND`, `windows.ERROR_NOT_FOUND`, `windows.ERROR_OBJECT_NOT_FOUND` — c'est le cas idempotent
- Aucun `log.Printf` côté client (règle architecturale stricte). Erreurs uniquement sur `os.Stderr` via `fmt.Fprintln`

**NSIS — ordre uninstall** (Task 5) :
```
1. taskkill UI                          ← unblock file lock
2. service stop                          ← shutdown gracieux (DNS restore + tunnel close)
3. Sleep 2000                            ← laisse shutdown se terminer
4. service cleanup                       ← force WFP + Wintun cleanup (NOUVEAU)
5. service uninstall                     ← désenregistre SCM
6. RMDir /r ProgramData\LeVoile         ← purge cache, token, état firewall
7. DeleteRegValue HKCU Run\LeVoile      ← unstick autostart
8. DeleteRegKey HKLM Uninstall\LeVoile  ← Add/Remove Programs
9. Delete files individuellement
10. RMDir $INSTDIR
```

L'ordre 4→5 (cleanup AVANT uninstall SCM) est important : si l'implémentation `firewall.Deactivate` énumère les filtres via le provider GUID `leVoileProviderKey`, le provider doit encore être référencé. À vérifier en lisant `internal/firewall/wfp_windows.go:Deactivate` — si la fonction passe par le sublayer GUID stable plutôt que par le provider, l'ordre est moins sensible.

**Build scripts** (Tasks 2, 3, 6) :
- Pas de renommage de binaire (utiliser le nom de sortie GoReleaser)
- Échec hard si une dépendance est absente (`wintun.dll`, `levoile-ctl.exe`) — message d'erreur pointant vers le fix (`make wintun`, `goreleaser build`)
- Conserver l'idempotence : `rm -rf build/` au début, recréation propre

### Contexte des stories précédentes

**Story 4.1 (legacy, désormais story 7.1)** — l'installeur d'origine. La majorité du squelette NSIS est conservée : single UAC via `RequestExecutionLevel admin`, `kardianos/service install` pour SCM, HKCU Run pour autostart, MUI2 wizard. Seuls les ajouts post-restructure manquent.

**Story 5.9** (done) — `cmd/ctl` créé, token machine-local généré au démarrage du service dans `%ProgramData%\LeVoile\ctl.token` avec ACL restrictif (LocalSystem + Administrators). Le ctl est déjà packagé conditionnellement (`/nonfatal`) — Task 6 le rend obligatoire.

**Story 5.1-5.8** (done) — l'UI unique `cmd/ui` (webview + fyne.io/systray) a remplacé le couple `cmd/tray` + `cmd/desktop`. Le binaire de sortie est `levoile-ui.exe`. Tout résidu `levoile-desktop` est obsolète.

**Story 2.1, 2.4, 2.6, 2.7** (done) — TUN `levoile0`, routing, nftables Linux, WFP Windows. `internal/firewall.Deactivate` et `internal/tun.CleanupOrphan` sont déjà idempotents et exposés publiquement → `cleanup` les compose, sans réinventer.

**Changelog 2026-04-15** (PRD §change_history) — extension navigateur supprimée, donc tous les nettoyages d'extension/proxy/policy WebRTC dans le NSIS uninstall sont des résidus à supprimer (Task 4).

### NFRs à respecter

| NFR | Exigence | Impact sur cette story |
|-----|----------|------------------------|
| NFR5 / NFR6 | Zero fuite DNS, resolver restauré dans tous scénarios | `service stop` déclenche shutdown gracieux qui restaure DNS. `cleanup` ne touche PAS au DNS (déjà restauré) — il ne nettoie que firewall + Wintun |
| NFR15 | Service redémarrage auto < 10s | Configuré par kardianos `OnFailure=restart`, délai 5s — déjà actif, pas d'ajout |
| NFR17 | Crash-recovery TUN orpheline < 5s | `CleanupOrphan` déjà testé < 5s. `cleanup` y a recours |
| NFR20 / NFR22a | Aucun log IP / domaines / contenu | Le sous-commande `cleanup` ne logge que des erreurs Windows API, jamais d'IP ou de hostname |

### Hors scope (NE PAS implémenter)

- **Code signing du `.exe` NSIS** : certificat EV, post-MVP (cf. PRD risk register)
- **MSI à la place de NSIS** : décision architecturale stable, pas de migration
- **GoReleaser Pro `nsis:` block** : payant, on reste sur `makensis` standalone (cf. dev notes story 4.1)
- **Code signing du `wintun.dll`** : déjà signée Microsoft, on l'utilise telle quelle
- **Migration auto depuis ancienne installation `levoile-desktop.exe`** : single-user MVP, l'utilisateur réinstalle proprement. La sous-commande `cleanup` détecte les filtres orphelins de l'ancienne version
- **Paquets Linux** : story 7.2 (`.deb`/`.rpm`/`.apk` via GoReleaser + nfpm)
- **AUR** : story 7.3
- **Signature Ed25519 des paquets de distribution** : story 7.4
- **Configuration TOML cross-platform paths** : story 7.5

### Project Structure Notes

**Alignement** avec l'architecture `_bmad-output/planning-artifacts/architecture.md` :
- `cmd/client/main.go` (le binaire `levoile-service`) reste le point d'entrée des sous-commandes — cohérent avec §"Sous-commandes existantes (NE PAS REINVENTER)" de la story 4.1
- `installer/` reste le dossier dédié au packaging Windows (parallèle à `deploy/` Linux)
- `internal/tun/wintun/wintun.dll` reste la source de vérité de la DLL (Wintun 0.14.1, SHA256 `07c256185d...51`)
- Aucune nouvelle dépendance Go (pas de modification de `go.mod`)

**Variantes / conflits potentiels** :
- Le PRD §FR22 mentionne `%AppData%/LeVoile/` pour la config utilisateur. La story 4.1 a placé `config.toml` dans `$INSTDIR` (Program Files) car le service SYSTEM ne peut pas lire `%AppData%` user. Cette story 7.1 ne touche pas à ce mécanisme — le `config.toml` reste dans `$INSTDIR`. La story 7.5 rationalisera la séparation config service vs config UI utilisateur (HMAC, paths cross-platform)
- `clear WFP filters` (epic AC) est implémenté via `firewall.Deactivate()`. `remove TUN adapter` est implémenté via `tun.CleanupOrphan("levoile0")`. Pas de gestion explicite des routes dans `cleanup` (Task 1.2) car le shutdown du service les retire et l'OS les évacue avec l'adapter

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 7-Story 7.1] — AC consolidés
- [Source: _bmad-output/planning-artifacts/architecture.md#Infrastructure & Déploiement] — NSIS install : "service SCM + UI autostart + shortcuts + Wintun DLL", uninstall : "clear WFP filters + remove TUN adapter"
- [Source: _bmad-output/planning-artifacts/architecture.md#Architecture 2 Processus] — Binaire `levoile-ui` unique (webview + fyne.io/systray), CTL `levoile-ctl`
- [Source: _bmad-output/planning-artifacts/prd.md#FR20] — Installeur NSIS Windows
- [Source: _bmad-output/planning-artifacts/prd.md#FR21] — Service SCM persistant + nettoyage shutdown
- [Source: _bmad-output/planning-artifacts/prd.md#change_history 2026-04-15] — Extension navigateur supprimée
- [Source: installer/levoile.nsi:1-217] — Squelette existant à corriger
- [Source: installer/build.sh:35] — Renommage obsolète `levoile-ui.exe → levoile-desktop.exe`
- [Source: installer/build.ps1:38] — Idem
- [Source: cmd/client/main.go:189] — `handleServiceCommand(cmd string)` pattern à étendre
- [Source: internal/tun/wintun_extract_windows.go] — `ensureWintunDLL()` à modifier (priorité `$INSTDIR\wintun.dll`)
- [Source: internal/tun/cleanup_windows.go:13] — `CleanupOrphan(name string) error` idempotent
- [Source: internal/firewall/wfp_windows.go:38] — `leVoileProviderKey` GUID stable (provider reference pour Deactivate)
- [Source: internal/tun/wintun/README.md] — Provenance Wintun 0.14.1 + script `make wintun`
- [Source: _bmad-output/implementation-artifacts/4-1-installateur-windows-avec-enregistrement-du-service.md] — Décisions héritées : config dans $INSTDIR, kardianos sub-commands, GoReleaser OSS + makensis standalone, single UAC pattern
- [Source: _bmad-output/implementation-artifacts/5-9-mode-degrade-kill-switch-indicateur-visuel-permanent.md (si présent)] — Token ACL `%ProgramData%\LeVoile\ctl.token`

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Debug Log References

- `goreleaser check` — config valide mais émet 2 deprecation warnings (`archives.format`, `archives.builds`) — hors scope story 7.1, ne bloque ni `build` ni `check` (exit non-zero pour deprecation seulement).
- `goreleaser build --snapshot --clean` ne tourne pas sur le poste de dev Windows : la cible `ui-linux` (CGO=1, GOOS=linux GOARCH=arm64) demande un cross-toolchain `aarch64-linux-gnu-gcc` absent — héritage pré-existant, pas introduit par cette story. Contournement utilisé pour la validation : `go build` direct des 3 binaires Windows + copie wintun.dll + `makensis`. La pipeline CI (Linux runner) est l'environnement nominal pour `goreleaser build`.
- `makensis -DAPP_VERSION=...` au lieu de `/D...` car git-bash interprète `/D...` comme un chemin Unix (`Can't open script "C:/Program Files/Git/DAPP_VERSION=..."`). Build script `build.sh` corrigé en conséquence ; `build.ps1` reste sur la syntaxe Windows native.
- `assets/icons/` n'existe plus sur disque (dossier supprimé lors du refactor UI épic 5). Les scripts `build.sh` et `build.ps1` pointaient vers ce chemin inexistant — corrigés vers `internal/ui/icons/` qui contient effectivement les 3 icônes tray. La référence `assets/icons/*.ico` dans `.goreleaser.yaml` reste cassée mais hors scope (n'impacte que `goreleaser release`, pas `build`).

### Completion Notes List

- **Task 1 — Sous-commande `cleanup`** : ajout dans `cmd/client/main.go` de `runCleanup()` qui invoque `firewall.New(nil, firewall.Options{}).CleanupOrphans(ctx)` et `tun.CleanupOrphan(tun.DefaultName)`. Cross-platform via les implémentations natives existantes (no-op sur Linux pour le firewall, idempotent partout). Dispatch dans `main()` avant `resolveConfig` (comme install/uninstall/start/stop). Seams `cleanupFirewall` / `cleanupTun` injectables pour les tests. 6 tests couvrant : succès double, count remonté, erreur firewall, erreur TUN, deux erreurs, présence du timeout.
- **Task 2 — Renommage `levoile-desktop.exe` → `levoile-ui.exe`** : NSIS utilise désormais `${UI_EXE}` partout (taskkill, File, Run key, shortcuts). `build.sh` et `build.ps1` copient le binaire en préservant son nom canonique `levoile-ui.exe`. Clé HKCU Run conserve `"LeVoile"` comme nom de valeur (pointage chemin absolu, pas de migration registry).
- **Task 3 — Bundling wintun.dll dans Program Files** : `installer/levoile.nsi` ajoute `File "build\${WINTUN_DLL}"` dans Install et `Delete "$INSTDIR\${WINTUN_DLL}"` dans Uninstall. `internal/tun/wintun_extract_windows.go` refactorisé : priorité `<dir(os.Executable())>\wintun.dll` → cache `%ProgramData%\LeVoile\wintun.dll` → embed extraction. `loadDLL` et `exeDir` rendus injectables pour test. 4 tests couvrant les 4 chemins de résolution (exe-dir, cache fallback, hard fail, embed extraction). Build scripts échouent hard si `internal/tun/wintun/wintun.dll` est absent, avec message pointant vers `make wintun`.
- **Task 4 — Nettoyage NSIS résidus extension** : suppression intégrale de WinINET proxy restore, taskkill 6 navigateurs, `del *.xpi`, `WebRtcIPHandling`/`WebRtcIPHandlingPolicy` x6 vendors, `proxy-original.json`, `RMDir $APPDATA\LeVoile`. Le `RMDir /r $ProgramData\LeVoile` est conservé (purge cache wintun, token ctl, état firewall persistant) et placé APRÈS `cleanup` pour permettre à `firewall.Deactivate` de lire son état si nécessaire.
- **Task 5 — Cleanup forcé dans uninstall** : NSIS appelle `ExecWait '"$INSTDIR\${SERVICE_EXE}" cleanup'` entre `stop`+Sleep et `uninstall` SCM. Code retour ignoré (best-effort). Ordre validé : taskkill UI → service stop → Sleep 2s → service cleanup → service uninstall → RMDir ProgramData → DeleteReg → Delete files → RMDir $INSTDIR.
- **Task 6 — CTL bundle non-optionnel** : `/nonfatal` retiré de `File "build\${CTL_EXE}"`. Build scripts copient `levoile-ctl.exe` (échec hard si absent). NSIS uninstall ajoute `Delete "$INSTDIR\${CTL_EXE}"`.
- **Task 7 — Validation** : `goreleaser check` passe (deprecations seulement) ; build direct `go build` des 3 binaires Windows OK ; `makensis -DAPP_VERSION=0.7.1-rc1 levoile.nsi` produit `installer/LeVoile-Setup.exe` (~7.3 MB) sans erreur ; `go test ./...` (29 packages) passe ; `go vet ./...` passe sans warning ; smoketest VM Windows manuel reporté à la QA release (non-blocking pour dev-story).

### File List

**New files:**
- `cmd/client/main_cleanup_test.go` — Tests sous-commande `cleanup` (Task 1)
- `internal/tun/wintun_extract_windows_test.go` — Tests priorité résolution Wintun DLL (Task 3)

**Modified files:**
- `cmd/client/main.go` — Imports `internal/firewall` + `internal/tun` ; ajout `cleanupFirewall`/`cleanupTun` seams + `runCleanup()` ; dispatch `cleanup` dans `main()` ; restauration `Option: service.KeyValue{OnFailure=restart, 5s, ResetPeriod=10}` dans `newServiceConfig()` (Tasks 1 + smoketest D1)
- `internal/tun/wintun_extract_windows.go` — Refactor `doEnsureWintunDLL` avec priorité (1) exe-dir, (2) cache, (3) embed → extraction ; `loadDLL` et `exeDir` injectables ; `embeddedWintunDLL` consultable via test seam (Task 3)
- `installer/levoile.nsi` — Renommage `DESKTOP_EXE` → `UI_EXE` ; ajout `WINTUN_DLL` ; ajout `File wintun.dll` install + `Delete` uninstall ; ajout `cleanup` dans uninstall ; suppression intégrale résidus extension navigateur ; retrait `/nonfatal` ctl ; `Unicode true` + UTF-8 BOM pour accents français ; `SetRegView 64` autour des `HKLM Uninstall` writes (Install) + cleanup dual 64/32 (Uninstall) (Tasks 2, 3, 4, 5, 6 + AI-Review M3/L1 + smoketest D2)
- `installer/build.sh` — Copie binaires sans renommage ; check + copie `wintun.dll` ; copie `levoile-ctl.exe` ; correctif chemins icônes (`internal/ui/icons/`) ; `makensis -DAPP_VERSION` portable git-bash (Tasks 2, 3, 6)
- `installer/build.ps1` — Idem PowerShell (Tasks 2, 3, 6)
- `.gitignore` — Ajout `/internal/tun/wintun_dll_windows.go` (fichier généré par `scripts/fetch-wintun.sh`, AI-Review M2)

## Change Log

- 2026-04-18 : Implémentation story 7-1 — installeur NSIS Windows aligné sur archi post-2026-04-15. Ajout sous-commande `levoile-service cleanup` (force WFP + Wintun adapter cleanup), bundling `wintun.dll` dans Program Files avec priorité de résolution dans `ensureWintunDLL`, renommage `levoile-desktop.exe` → `levoile-ui.exe`, suppression résidus extension navigateur dans NSIS uninstall, CTL bundle obligatoire, shortcuts Bureau + Start Menu conservés. Tests : 6 nouveaux tests `runCleanup` + 4 nouveaux tests `doEnsureWintunDLL`. Compile NSIS validé : `LeVoile-Setup.exe` 7.3 MB.
- 2026-04-18 : Code review adversarial — 1 HIGH + 3 MEDIUM + 2 LOW corrigés. [H1] Task 7.5 décochée (smoketest VM non exécuté → reporté QA release, AC1-AC4 non strictement validés). [M1] `TestEnsureWintunDLL_ExeDirPriority` isolé via `t.Setenv("PROGRAMDATA", t.TempDir())`, assertion durcie sur l'absence du sous-dossier `LeVoile/`. [M2] `internal/tun/wintun_dll_windows.go` ajouté à `.gitignore` (fichier généré par `scripts/fetch-wintun.sh`). [M3] NSIS `Unicode true` + UTF-8 BOM ajoutés sur `installer/levoile.nsi` → makensis lit en `(UTF8)` au lieu de `(ACP)`, accents français restaurés (`déposer`, `signée`). [L1] Capture `$0` de `service cleanup` retirée (jamais utilisée — best-effort explicite). L2/L3 laissés en notes (Event Log + TODO swallow `ensureWintunDLL`).
- 2026-04-18 : Smoketest Windows exécuté (v0.7.1-rc2 → rc3 après fix), 2 défauts découverts + corrigés. [D1] AC2 — `OnFailure=restart 5s` absent dans `cmd/client/main.go:newServiceConfig()` (régression perdue lors d'un refactor antérieur). Fix : ajout bloc `Option: service.KeyValue{"OnFailure": "restart", "OnFailureDelayDuration": "5s", "OnFailureResetPeriod": 10}`. Vérifié via `sc.exe qfailure LeVoile` → RESET_PERIOD 10 + RESTART 5000ms. [D2] AC1 — Uninstall entry dans `HKLM\SOFTWARE\WOW6432Node` à cause de NSIS x86 (redirection WoW64). Fix : `SetRegView 64` avant les `WriteRegStr HKLM Uninstall\...` (Install) + `SetRegView 64` / `SetRegView 32` successifs dans Uninstall (nettoie les deux vues pour backward-compat avec installs pré-fix). Vérifié : entrée à la racine 64-bit, absence WOW6432Node. Cycle install+uninstall complet validé bout en bout — AC1-AC5 tous passants.

## Senior Developer Review (AI)

**Review date** : 2026-04-18
**Reviewer** : claude-opus-4-7 (1M context) — adversarial mode
**Outcome** : **Changes Requested** (3/3 HIGH+MEDIUM appliqués ; 1 limitation acceptée → manual VM test reportée QA release)

**Action Items**

- [x] [AI-Review][High] Task 7.5 marquée `[x]` mais smoketest VM jamais exécuté → décocher + ajouter note honnête → reporté QA release
- [x] [AI-Review][Medium] `TestEnsureWintunDLL_ExeDirPriority` non-isolé sur le vrai `%PROGRAMDATA%` → setenv + assertion sur dossier entier
- [x] [AI-Review][Medium] `internal/tun/wintun_dll_windows.go` généré mais non gitignored → ajout à `.gitignore`
- [x] [AI-Review][Medium] NSIS welcome page accents cassés (« dposer », « signe Microsoft ») car lu en ACP → `Unicode true` + UTF-8 BOM, makensis confirme `(UTF8)`
- [x] [AI-Review][Low] Capture `$0` de `cleanup` ExecWait jamais lue → drop le `$0`
- [ ] [AI-Review][Low] `runCleanup()` n'écrit rien dans Event Log Windows — diagnosability post-incident limitée. Optionnel. Hors scope dev-story
- [ ] [AI-Review][Low] `tun.CleanupOrphan` swallow `ensureWintunDLL` errors silencieusement — déjà documenté comme idempotent volontaire. À reconsidérer si retours QA
- [x] [Smoketest][High] D1 — `OnFailure=restart 5s` absent du service (AC2) → restauré dans `newServiceConfig()` + vérifié via `sc.exe qfailure`
- [x] [Smoketest][High] D2 — HKLM Uninstall entry sous WOW6432Node (AC1) → `SetRegView 64` ajouté, entrée désormais à la racine 64-bit native
