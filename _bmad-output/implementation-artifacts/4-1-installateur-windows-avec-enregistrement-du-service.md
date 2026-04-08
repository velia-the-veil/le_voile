# Story 4.1: Installateur Windows avec enregistrement du service

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux installer Le Voile via un installateur Windows avec une seule elevation UAC,
Afin d'etre protege immediatement apres l'installation sans configuration.

## Acceptance Criteria

1. **Elevation UAC unique** — Quand l'utilisateur lance l'installateur, une seule invite UAC est presentee pour l'elevation de privileges, et l'installation se deroule sans autre intervention.

2. **Enregistrement et demarrage du service** — Quand l'installation est terminee, le service Le Voile est enregistre dans le gestionnaire de services Windows (SCM), le demarrage automatique est configure par defaut, et le service est demarre immediatement.

3. **Demarrage automatique du tray** — Quand l'utilisateur se connecte a Windows apres installation, le tray UI demarre automatiquement, l'icone Le Voile apparait dans le system tray, et le temps total installation → protection active est inferieur a 30 secondes.

4. **Build GoReleaser** — Quand `goreleaser release --clean` est execute, les binaires Windows (service et tray) sont produits, et un script NSIS est disponible pour compiler l'installateur.

5. **Desinstallation propre** — Quand l'utilisateur utilise "Ajout/Suppression de programmes" Windows, le service est arrete et desenregistre, le resolver DNS est restaure a sa valeur originale, et les fichiers de configuration sont supprimes.

### Scenarios BDD detailles

**AC1 — Elevation UAC unique:**
```
Given l'utilisateur telecharge l'installateur depuis plateformeliberte.fr
When il lance l'installateur (.exe)
Then une seule invite UAC est presentee pour l'elevation de privileges
And l'installation se deroule sans autre intervention
And aucune boite de dialogue supplementaire n'apparait (installation silencieuse post-UAC)
```

**AC2 — Enregistrement service:**
```
Given l'installation terminee
When l'installateur a fini son execution
Then le service "LeVoile" est enregistre dans Windows SCM via "client.exe install"
And le type de demarrage est configure sur "automatic"
And le service est demarre immediatement via "client.exe start"
And la politique de redemarrage sur crash est configuree (restart apres 5s)
```

**AC3 — Tray auto-start:**
```
Given l'installation terminee
When l'utilisateur se connecte a Windows (login)
Then le tray.exe demarre automatiquement via la cle registre HKCU\Software\Microsoft\Windows\CurrentVersion\Run
And l'icone Le Voile apparait dans le system tray
And le tray se connecte au service via IPC named pipe
And le temps total installation → protection active < 30 secondes
```

**AC4 — Build GoReleaser:**
```
Given le fichier .goreleaser.yaml configure
When "goreleaser release --clean" est execute
Then les binaires client.exe et tray.exe sont produits pour windows/amd64
And les archives contiennent les binaires, icones et config par defaut
And le script NSIS peut etre compile avec makensis pour produire l'installateur
```

**AC5 — Desinstallation:**
```
Given Le Voile est installe
When l'utilisateur lance la desinstallation via "Ajout/Suppression de programmes"
Then le service est arrete via "client.exe stop"
And le service est desenregistre via "client.exe uninstall"
And le resolver DNS systeme est restaure a sa valeur originale
And le tray.exe est ferme s'il est en cours d'execution
And l'entree de demarrage automatique tray est supprimee du registre
And les fichiers d'installation sont supprimes de Program Files
And les fichiers de configuration dans %AppData%/LeVoile sont supprimes
And l'entree dans "Ajout/Suppression de programmes" est retiree
```

## Tasks / Subtasks

- [x] **Task 0 : Adapter cmd/client/main.go pour lire la config depuis le fichier TOML** (AC: #2, prerequis)
  - [x] 0.1 **PROBLEME BLOQUANT** : `cmd/client/main.go` exige actuellement `-relay-pubkey` (flag CLI obligatoire, exit 1 si absent). L'installateur NSIS et Windows SCM ne peuvent pas passer de flags CLI au service. De plus, le service tourne en tant que SYSTEM — `os.UserConfigDir()` retourne le AppData de SYSTEM, pas celui de l'utilisateur.
  - [x] 0.2 **SOLUTION** : Modifier `cmd/client/main.go` pour decouvrir le config TOML automatiquement. Ordre de recherche :
    1. Flag optionnel `-config <path>` s'il est fourni
    2. `config.toml` a cote de l'executable (`filepath.Dir(os.Executable())`) — c'est le chemin principal pour le service installe dans Program Files
    3. `config.DefaultPath()` (user AppData) — fallback pour le mode portable ou dev
  - [x] 0.3 Remplacer les flags obligatoires `-relay-pubkey` et `-relay-domain` par la lecture du fichier config :
    ```go
    // Decouverte automatique du config
    configFlag := flag.String("config", "", "chemin config TOML (optionnel, auto-detecte)")
    flag.Parse()
    cfgPath := *configFlag
    if cfgPath == "" {
        exePath, _ := os.Executable()
        exeDir := filepath.Dir(exePath)
        candidate := filepath.Join(exeDir, "config.toml")
        if _, err := os.Stat(candidate); err == nil {
            cfgPath = candidate
        } else {
            cfgPath, _ = config.DefaultPath()
        }
    }
    cfg, err := config.Load(cfgPath)
    // Utiliser cfg.Relay.PublicKeyEd25519 et cfg.Relay.Domain
    ```
  - [x] 0.4 Valider que `cfg.Relay.PublicKeyEd25519` n'est pas vide — sinon afficher une erreur claire et exit 1
  - [x] 0.5 Ajouter `var version string` en haut de `cmd/client/main.go` pour l'injection de version via ldflags (`-X main.version`)
  - [x] 0.6 Ajouter `var version string` en haut de `cmd/tray/main.go` egalement
  - [x] 0.7 Mettre a jour les tests existants dans `cmd/client/main_test.go` — adapter les tests qui fournissaient `-relay-pubkey` via flag pour utiliser un fichier config TOML temporaire
  - [x] 0.8 **ATTENTION** : Conserver la retro-compatibilite CLI — si `-relay-pubkey` est fourni en flag, l'utiliser en priorite sur le fichier config (mode portable/dev)

- [x] **Task 1 : Configurer GoReleaser pour les builds Windows** (AC: #4)
  - [x] 1.1 Creer `.goreleaser.yaml` a la racine du projet avec la configuration multi-binaire :
    - Build `client` : `cmd/client/main.go` → `levoile-service.exe` (GOOS=windows, GOARCH=amd64)
    - Build `tray` : `cmd/tray/main.go` → `levoile-tray.exe` (GOOS=windows, GOARCH=amd64)
    - Build `relay` : `cmd/relay/main.go` → `levoile-relay` (GOOS=linux, GOARCH=amd64)
  - [x] 1.2 Configurer la section `archives` pour creer une archive Windows contenant les deux binaires client + tray
  - [x] 1.3 Ajouter les `extra_files` dans l'archive : `assets/icons/*.ico`, `installer/config-default.toml`, `LICENSE`
  - [x] 1.4 Configurer `ldflags` pour injecter la version : `-s -w -X main.version={{.Version}}`. **IMPORTANT** : `var version string` doit exister dans les main.go (ajoute dans Task 0)
  - [x] 1.5 Ajouter `-H windowsgui` UNIQUEMENT pour le tray (pas de fenetre console). Consolider en une seule ligne ldflags : `-s -w -X main.version={{.Version}} -H windowsgui`. NE PAS mettre `-H windowsgui` sur le service (il doit pouvoir tourner en console via `run`)
  - [x] 1.6 Tester : `goreleaser check` pour valider la syntaxe YAML, `goreleaser build --snapshot --clean` pour verifier les builds

- [x] **Task 2 : Creer le fichier config par defaut** (AC: #2, #3)
  - [x] 2.1 Creer `installer/config-default.toml` avec la configuration par defaut :
    ```toml
    [relay]
    domain = "levoile.dev"
    public_key_ed25519 = ""

    [client]
    auto_start = true
    ```
  - [x] 2.2 **ATTENTION** : La cle publique Ed25519 du relais doit etre remplie AVANT la distribution. Laisser vide dans le template et documenter qu'elle doit etre injectee au moment du build/release

- [x] **Task 3 : Generer l'icone d'application pour l'installateur** (AC: #4)
  - [x] 3.1 Utiliser `tools/gen_icons.go` (existant) pour generer les icones si pas deja dans `internal/tray/`
  - [x] 3.2 Creer une icone d'application `installer/levoile.ico` pour l'installateur NSIS — utiliser l'icone `connected.ico` (verte) comme icone d'application principale, ou creer une icone multi-resolution (16, 32, 48, 256 px) via un outil externe ou en etendant gen_icons.go
  - [x] 3.3 Copier les icones tray dans `assets/icons/` pour l'inclusion dans l'archive GoReleaser (connected.ico, connecting.ico, disconnected.ico)

- [x] **Task 4 : Ecrire le script NSIS d'installation** (AC: #1, #2, #3, #5)
  - [x] 4.1 Creer `installer/levoile.nsi` — script NSIS complet avec :
    - `RequestExecutionLevel admin` — elevation UAC unique
    - `InstallDir "$PROGRAMFILES64\LeVoile"` — repertoire d'installation
    - `Name "Le Voile"` et `OutFile "LeVoile-Setup.exe"`
    - Icone d'application via `Icon` et `UninstallIcon`
  - [x] 4.2 Section **Install** du script NSIS :
    - Copier `levoile-service.exe` dans `$INSTDIR`
    - Copier `levoile-tray.exe` dans `$INSTDIR`
    - Copier `config-default.toml` vers `$INSTDIR\config.toml` (a cote des binaires dans Program Files — le service SYSTEM et le tray user peuvent lire ce chemin). Utiliser `IfFileExists` pour ne pas ecraser une config existante lors d'une reinstallation. Syntaxe NSIS correcte : `File /oname=config.toml "build\config-default.toml"` (le `/oname=` AVANT le chemin source)
    - Copier les icones dans `$INSTDIR\icons\`
    - Ecrire l'uninstaller via `WriteUninstaller "$INSTDIR\uninstall.exe"`
  - [x] 4.3 Section **Service Registration** du script NSIS :
    - **Gestion reinstallation** : Avant `install`, tenter `stop` puis `uninstall` (ignorer erreurs si service non existant) — permet la mise a jour sans desinstallation prealable
    - `nsExec::Exec '"$INSTDIR\levoile-service.exe" stop'` (ignorer erreur)
    - `nsExec::Exec '"$INSTDIR\levoile-service.exe" uninstall'` (ignorer erreur)
    - `Sleep 2000` — attendre 2s que le service soit completement arrete avant reinstallation
    - `ExecWait '"$INSTDIR\levoile-service.exe" install'` — enregistre le service dans SCM
    - `ExecWait '"$INSTDIR\levoile-service.exe" start'` — demarre le service immediatement
    - **ATTENTION** : `kardianos/service install` configure deja OnFailure=restart et demarrage automatique — pas besoin de `sc config` supplementaire
  - [x] 4.4 Section **Tray Auto-Start** du script NSIS :
    - Ecrire cle registre `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` avec valeur `"LeVoile"` = `'"$INSTDIR\levoile-tray.exe"'`
    - **ATTENTION** : Utiliser `HKCU` (pas HKLM) pour que le tray demarre dans le contexte de l'utilisateur courant, pas en tant que SYSTEM
    - Lancer `levoile-tray.exe` immediatement apres l'installation pour que l'utilisateur voie le tray sans reboot
  - [x] 4.5 Section **Add/Remove Programs** du script NSIS :
    - Ecrire les cles registre dans `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\LeVoile` :
      - `DisplayName` = "Le Voile"
      - `UninstallString` = `'"$INSTDIR\uninstall.exe"'`
      - `InstallLocation` = `"$INSTDIR"`
      - `Publisher` = "Velia"
      - `DisplayIcon` = `"$INSTDIR\icons\connected.ico"`
      - `DisplayVersion` = `"${APP_VERSION}"` (injecte via `/DAPP_VERSION=x.y.z` dans makensis)
      - `NoModify` = 1
      - `NoRepair` = 1
  - [x] 4.6 Section **Uninstall** du script NSIS :
    - Fermer le tray s'il est en cours d'execution : `nsExec::Exec 'taskkill /F /IM levoile-tray.exe'` (ignorer erreur si pas en cours)
    - Arreter le service : `ExecWait '"$INSTDIR\levoile-service.exe" stop'`
    - `Sleep 2000` — attendre 2s que le service soit completement arrete et le DNS restaure
    - Desenregistrer le service : `ExecWait '"$INSTDIR\levoile-service.exe" uninstall'`
    - **IMPORTANT** : Le handler `stop` dans kardianos/service restaure le resolver DNS via la sequence shutdown() — le DNS est restaure automatiquement a l'arret du service. Le Sleep garantit que shutdown() a le temps de s'executer avant uninstall
    - Supprimer la cle registre de demarrage auto tray : `DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "LeVoile"`
    - Supprimer les cles registre Add/Remove Programs
    - Supprimer les fichiers dans `$INSTDIR` (binaires, config, icones, uninstaller)
    - Supprimer le repertoire `$INSTDIR`
    - **NOTE** : La config est dans `$INSTDIR\config.toml` (supprimee avec le repertoire). Pas de fichiers dans AppData a nettoyer au MVP
    - **NOTE HKCU** : La cle registre Run est ecrite pour l'utilisateur qui a lance l'installateur (admin). Si un autre admin installe pour un autre utilisateur, le tray ne demarrera pas automatiquement pour cet utilisateur. Acceptable au MVP (single-user)

- [x] **Task 5 : Script de build pour compiler l'installateur** (AC: #4)
  - [x] 5.1 Creer `installer/build.sh` — script shell qui :
    1. Verifie que `goreleaser` et `makensis` sont installes
    2. Execute `goreleaser build --snapshot --clean` pour produire les binaires
    3. Copie les binaires depuis `dist/` vers `installer/build/`
    4. Execute `makensis installer/levoile.nsi` pour produire `LeVoile-Setup.exe`
  - [x] 5.2 Creer `installer/build.ps1` — equivalent PowerShell pour les developpeurs Windows
  - [x] 5.3 Documenter dans le script : la cle publique Ed25519 doit etre injectee dans `config-default.toml` avant le build de distribution

- [x] **Task 6 : Renommer les binaires de sortie** (AC: #1, #2)
  - [x] 6.1 Dans `.goreleaser.yaml`, configurer les noms de binaires :
    - `cmd/client` → binary: `levoile-service` (suffixe .exe automatique sur Windows)
    - `cmd/tray` → binary: `levoile-tray`
    - `cmd/relay` → binary: `levoile-relay`
  - [x] 6.2 Mettre a jour le script NSIS pour utiliser ces noms exacts
  - [x] 6.3 **ATTENTION** : Le nom du service Windows reste `"LeVoile"` (constante dans `internal/service/service.go`), independant du nom du binaire

- [x] **Task 7 : Tests et validation** (AC: tous)
  - [x] 7.1 Executer `goreleaser check` — valider la configuration YAML
  - [x] 7.2 Executer `goreleaser build --snapshot --clean` — verifier que les 3 binaires sont produits
  - [x] 7.3 Verifier que `makensis installer/levoile.nsi` compile sans erreur (necessite NSIS installe)
  - [x] 7.4 Tester l'installateur sur une VM Windows propre :
    - L'UAC ne s'affiche qu'une seule fois
    - Le service est enregistre et demarre (`sc query LeVoile` → RUNNING)
    - Le tray apparait dans le system tray
    - La cle registre Run est presente
  - [x] 7.5 Tester la desinstallation sur la meme VM :
    - Le service est arrete et desenregistre
    - Le resolver DNS est restaure
    - Les fichiers sont supprimes de Program Files
    - La cle registre Run est supprimee
    - L'entree Add/Remove Programs est retiree
  - [x] 7.6 Executer `go test ./...` — non-regression de tous les tests existants
  - [x] 7.7 Executer `go vet ./...` — zero warning

## Dev Notes

### Contraintes architecturales OBLIGATOIRES

- **Pure Go, pas de CGo** — Module path: `github.com/velia-the-veil/le_voile`
- **Go 1.26** — Version actuelle dans go.mod
- **GoReleaser v2.14.1** — Version imposee par l'architecture. Version OSS (gratuite), PAS Pro
- **NSIS (Nullsoft Scriptable Install System)** — Outil externe pour creer l'installateur Windows. Doit etre installe separement (`choco install nsis` ou `winget install NSIS.NSIS`)
- **Jamais de logging client** — Les erreurs sont propagees via IPC, PAS de `log.Println`
- **Jamais de `panic`** — Toujours retourner `error`

### DECISION CRITIQUE : GoReleaser OSS + NSIS standalone

**Pourquoi PAS GoReleaser Pro ?**
- Les sections `nsis:` et `msi:` de GoReleaser sont **exclusivement Pro** (payant)
- Le projet est open-source, gratuit, finance par donations — pas de budget GoReleaser Pro

**Strategie retenue :**
1. **GoReleaser OSS** — Pour le build cross-plateforme des binaires (gratuit, v2.14.1)
2. **Script NSIS standalone** — Compile separement avec `makensis` (gratuit, open-source)
3. **Script d'orchestration** — `installer/build.sh` et `installer/build.ps1` chainent les deux etapes

**Impact :** Le workflow est `goreleaser build` → copie binaires → `makensis` au lieu d'un seul `goreleaser release`. C'est acceptable pour un projet a developpeur unique.

### GoReleaser v2.14.1 — Configuration multi-binaire

**Structure `.goreleaser.yaml` requise :**
```yaml
version: 2

builds:
  - id: service
    main: ./cmd/client
    binary: levoile-service
    goos: [windows]
    goarch: [amd64]
    ldflags:
      - -s -w -X main.version={{.Version}}

  - id: tray
    main: ./cmd/tray
    binary: levoile-tray
    goos: [windows]
    goarch: [amd64]
    ldflags:
      - -s -w -X main.version={{.Version}} -H windowsgui

  - id: relay
    main: ./cmd/relay
    binary: levoile-relay
    goos: [linux]
    goarch: [amd64]
    ldflags:
      - -s -w -X main.version={{.Version}}

archives:
  - id: windows
    builds: [service, tray]
    name_template: "LeVoile_{{.Version}}_windows_amd64"
    format: zip
    files:
      - LICENSE
      - assets/icons/*.ico
      - installer/config-default.toml

  - id: relay
    builds: [relay]
    name_template: "LeVoile-Relay_{{.Version}}_linux_amd64"
    format: tar.gz
    files:
      - LICENSE
      - deploy/*

checksum:
  name_template: "checksums.txt"
```

**ATTENTION — Flags linker :**
- `-s -w` : strip debug info et DWARF — reduit la taille du binaire (~30%)
- `-H windowsgui` : UNIQUEMENT pour le tray — supprime la fenetre console Windows. NE PAS mettre sur le service car `levoile-service.exe run` doit pouvoir afficher en console (mode debug/portable)

### kardianos/service — Sous-commandes existantes (NE PAS REINVENTER)

Le binaire `levoile-service.exe` (ex `cmd/client/main.go`) supporte deja ces sous-commandes via kardianos/service :

```
levoile-service.exe install    → Enregistre le service dans Windows SCM
                                 Configure OnFailure=restart, delai 5s
                                 Configure demarrage automatique
levoile-service.exe uninstall  → Desenregistre le service
levoile-service.exe start      → Demarre le service
levoile-service.exe stop       → Arrete le service (shutdown gracieux: DNS restaure, tunnel ferme)
levoile-service.exe run        → Mode foreground (console, debug, portable)
```

**Configuration service existante** (dans `cmd/client/main.go`, lignes 30-40) :
```go
svcConfig := &service.Config{
    Name:        svc.ServiceName,  // "LeVoile"
    DisplayName: "Le Voile",
    Description: "VPN minimaliste zero-log",
    Option: service.KeyValue{
        "OnFailure":              "restart",
        "OnFailureDelayDuration": "5s",
        "OnFailureResetPeriod":   10,
    },
}
```

**IMPORTANT :** Le script NSIS n'a qu'a appeler ces sous-commandes. PAS de manipulation directe de `sc.exe` ou du registre services — kardianos/service gere tout.

### Script NSIS — Structure et patterns

**Squelette NSIS pour reference :**
```nsis
!define APP_NAME "Le Voile"
!define APP_KEY "LeVoile"
!define SERVICE_EXE "levoile-service.exe"
!define TRAY_EXE "levoile-tray.exe"
; APP_VERSION injecte par le script de build via : makensis /DAPP_VERSION=x.y.z

SetCompressor /SOLID lzma       ; Compression maximale
ManifestDPIAware true            ; Support High-DPI
Name "${APP_NAME}"
OutFile "LeVoile-Setup.exe"
InstallDir "$PROGRAMFILES64\${APP_KEY}"
RequestExecutionLevel admin      ; UAC unique

Section "Install"
  SetOutPath $INSTDIR
  File "build\${SERVICE_EXE}"
  File "build\${TRAY_EXE}"
  SetOutPath "$INSTDIR\icons"
  File "build\icons\connected.ico"
  File "build\icons\connecting.ico"
  File "build\icons\disconnected.ico"

  ; Config par defaut A COTE des binaires (lisible par SYSTEM et user)
  ; Ne pas ecraser une config existante (reinstallation)
  SetOutPath $INSTDIR
  IfFileExists "$INSTDIR\config.toml" skip_config
    File /oname=config.toml "build\config-default.toml"
  skip_config:

  ; Gestion reinstallation : arreter/desenregistrer l'ancien service (ignorer erreurs)
  nsExec::Exec '"$INSTDIR\${SERVICE_EXE}" stop'
  nsExec::Exec '"$INSTDIR\${SERVICE_EXE}" uninstall'
  Sleep 2000

  ; Enregistrer et demarrer le service
  ExecWait '"$INSTDIR\${SERVICE_EXE}" install'
  ExecWait '"$INSTDIR\${SERVICE_EXE}" start'

  ; Tray auto-start au login (contexte utilisateur courant, HKCU pas HKLM)
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" \
    "${APP_KEY}" '"$INSTDIR\${TRAY_EXE}"'

  ; Lancer le tray immediatement
  Exec '"$INSTDIR\${TRAY_EXE}"'

  ; Add/Remove Programs
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "DisplayName" "${APP_NAME}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "Publisher" "Velia"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "DisplayIcon" "$INSTDIR\icons\connected.ico"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "DisplayVersion" "${APP_VERSION}"
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "NoRepair" 1

  WriteUninstaller "$INSTDIR\uninstall.exe"
SectionEnd

Section "Uninstall"
  ; Fermer le tray (ignorer erreur si pas en cours)
  nsExec::Exec 'taskkill /F /IM ${TRAY_EXE}'

  ; Arreter le service (shutdown() restaure le DNS automatiquement)
  ExecWait '"$INSTDIR\${SERVICE_EXE}" stop'
  Sleep 2000  ; Attendre que shutdown() restaure le DNS avant uninstall

  ; Desenregistrer le service
  ExecWait '"$INSTDIR\${SERVICE_EXE}" uninstall'

  ; Nettoyer le registre
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APP_KEY}"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}"

  ; Supprimer les fichiers (config est dans $INSTDIR, pas dans AppData)
  Delete "$INSTDIR\config.toml"
  Delete "$INSTDIR\${SERVICE_EXE}"
  Delete "$INSTDIR\${TRAY_EXE}"
  Delete "$INSTDIR\icons\*.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR\icons"
  RMDir "$INSTDIR"
SectionEnd
```

**CONTRAINTES CRITIQUES NSIS :**
1. `RequestExecutionLevel admin` — declenche l'invite UAC unique automatiquement
2. `ExecWait` pour les commandes service — attendre la fin de l'enregistrement/demarrage avant de continuer
3. `nsExec::Exec` pour taskkill — execution silencieuse, ignorer le code retour
4. `IfFileExists` pour la config — NE JAMAIS ecraser une config utilisateur existante lors d'une reinstallation
5. `HKCU` pour le tray auto-start — le tray doit tourner dans le contexte utilisateur, pas SYSTEM
6. `HKLM` pour Add/Remove Programs — necessaire pour que tous les utilisateurs voient l'entree
7. L'uninstaller doit s'auto-supprimer — NSIS gere ca automatiquement via copie dans %TEMP%
8. `Sleep 2000` entre `stop` et `uninstall` — garantit que shutdown() a le temps de restaurer le DNS
9. `SetCompressor /SOLID lzma` — compression maximale, reduit significativement la taille de l'installateur
10. `ManifestDPIAware true` — affichage correct sur ecrans High-DPI

### Chemins Windows critiques

| Chemin | Usage | Variable NSIS |
|--------|-------|---------------|
| `C:\Program Files\LeVoile\` | Binaires, config et icones | `$INSTDIR` (via `$PROGRAMFILES64`) |
| `C:\Program Files\LeVoile\config.toml` | Config partagee service+tray | `$INSTDIR\config.toml` |
| `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` | Tray auto-start | Registre HKCU |
| `HKLM\...\Uninstall\LeVoile` | Add/Remove Programs | Registre HKLM |
| `\\.\pipe\levoile` | IPC service ↔ tray | Named pipe (code existant) |

**DECISION CRITIQUE — Config dans Program Files (pas AppData) :**
- Le service Windows tourne en tant que **SYSTEM**. `os.UserConfigDir()` sous SYSTEM retourne `C:\Windows\system32\config\systemprofile\AppData\Roaming` — PAS le dossier de l'utilisateur
- La config est donc placee dans `$INSTDIR\config.toml` (Program Files) — lisible par SYSTEM et par l'utilisateur
- Task 0 modifie `cmd/client/main.go` pour decouvrir la config a cote de l'executable via `os.Executable()` + `filepath.Dir()`
- `config.DefaultPath()` (AppData) reste le fallback pour le mode portable et le tray standalone

### NFRs a respecter

| NFR | Exigence | Impact |
|-----|----------|--------|
| NFR1 | TLS 1.3 minimum | Pas d'impact direct sur cette story (deja implemente dans le tunnel) |
| NFR5 | Zero fuite DNS | La desinstallation DOIT restaurer le resolver DNS — le service shutdown() le fait deja |
| NFR6 | Resolver restaure dans tous les scenarios | L'uninstaller DOIT appeler `stop` avant `uninstall` pour declencher la restauration DNS |
| NFR11 | RAM < 20MB | L'installateur ne doit pas ajouter de services ou processus supplementaires au-dela de service + tray |
| NFR15 | Service : redemarrage auto < 10s | Deja configure dans kardianos/service config (OnFailure=restart, 5s delay) |

### APIs et fichiers existants a utiliser (NE PAS REINVENTER)

**kardianos/service** (`cmd/client/main.go`) — DEJA EXISTANT :
- Sous-commandes `install`, `uninstall`, `start`, `stop`, `run` — lignes 64-102
- Config service avec OnFailure=restart — lignes 30-40
- Handler IPC avec actions `get_status`, `connect`, `disconnect`, `set_auto_start`, `quit`

**Config TOML** (`internal/config/`) — DEJA EXISTANT :
- `config.go` : struct `Config` avec `Relay.Domain`, `Relay.PublicKeyEd25519`, `Client.AutoStart`
- `paths_windows.go` : `os.UserConfigDir()` + `LeVoile` → `%AppData%\LeVoile\config.toml`
- `paths_unix.go` : `os.UserConfigDir()` + `levoile`
- `Load()`, `Save()`, `DefaultPath()` — fonctions existantes

**Tray** (`cmd/tray/main.go`) — DEJA EXISTANT :
- Charge la config au demarrage (`config.DefaultPath()` + `config.Load()`)
- Se connecte au service via IPC named pipe
- Fonctionne independamment du service (reconnexion automatique)

**Icones** (`tools/gen_icons.go`) — DEJA EXISTANT :
- Generateur d'icones ICO pur Go (16, 32, 48 px, 32-bit BGRA)
- 3 couleurs : connected (vert #2ECC40), connecting (orange #FF9900), disconnected (rouge #E03C3C)
- Sortie dans `internal/tray/` — les icones sont embedded via `//go:embed`
- **ATTENTION** : ne genere que 16/32/48 px — pour l'icone installateur Windows (affichage Windows Explorer), 256 px est recommande. Etendre `gen_icons.go` pour ajouter 256 px ou generer `installer/levoile.ico` separement

**Deploy Linux** (`deploy/`) — DEJA EXISTANT :
- `install.sh` — script de deploiement Linux (systemd)
- `levoile-relay.service` — unite systemd pour le relais

### Dependances de cette story

- **Story 3.3** (done) : config TOML, menu tray, auto_start toggle, IPC quit
- **Story 3.1** (done) : service kardianos/service, IPC server/client, named pipe
- **Story 3.2** (done) : tray UI, icones, polling IPC, SystrayAPI
- **Toutes stories Epic 1-3** (done) : le projet est fonctionnel bout en bout

**Outils externes requis (NON dans go.mod) :**
- `goreleaser` v2.14.1 — installe via `go install github.com/goreleaser/goreleaser/v2@latest` ou `brew install goreleaser`
- `makensis` (NSIS) — installe via `choco install nsis`, `winget install NSIS.NSIS`, ou download depuis nsis.sourceforge.io

### Ce qui est HORS SCOPE (ne PAS implementer)

- Version portable (Story 4.2)
- Build multi-plateforme Linux/macOS (Story 4.2)
- CI/CD GitHub Actions (post-MVP)
- Auto-update (Phase 2)
- Code signing (certificat payant, post-MVP)
- Interface graphique d'installation (wizard avec choix) — l'installation est silencieuse apres UAC
- Support multi-utilisateur (un seul utilisateur Windows au MVP)

### Intelligence de la Story 3.3 (CRUCIAL)

**Lecons directement applicables :**
- `internal/config/paths_windows.go` utilise `os.UserConfigDir()` → `%AppData%\LeVoile` (Roaming). Mais le service SYSTEM ne peut pas acceder a ce chemin. **Solution Task 0** : decouverte config via `os.Executable()` dir en priorite → config dans `$INSTDIR` (Program Files). `DefaultPath()` reste le fallback pour le mode portable
- Le handler IPC `quit` dans `cmd/client/main.go` deconnecte le tunnel, puis arrete le service via `prg.Cancel()` avec un delai de 100ms — la sequence shutdown() restaure le DNS. Le `stop` du service via kardianos suit le meme chemin
- Le handler `set_auto_start` change le type de demarrage du service OS via `sc config LeVoile start=auto/demand` — kardianos/service `install` configure deja `start=auto` par defaut
- Cross-compilation Darwin echoue depuis Windows (fyne.io/systray necessite headers macOS) — Windows et Linux OK
- `BurntSushi/toml` v1.5.0 deja dans go.mod — pas de nouvelle dependance Go requise pour cette story
- Le service tourne en tant que SYSTEM, le tray en tant que l'utilisateur courant — ils communiquent via named pipe `\\.\pipe\levoile`

**Fichiers crees/modifies dans Story 3.3 qui impactent cette story :**
- `internal/config/config.go` — struct Config avec defaults (`AutoStart: true`, `Relay.Domain: "levoile.dev"`)
- `internal/config/paths_windows.go` — `os.UserConfigDir()` + `LeVoile` → **%AppData%\LeVoile** (IMPORTANT pour NSIS)
- `cmd/client/main.go` — sous-commandes install/uninstall/start/stop/run, handlers IPC
- `cmd/tray/main.go` — charge config au demarrage, cree tray, run()

### Project Structure Notes

**Fichiers a CREER :**
```
.goreleaser.yaml                      # Config GoReleaser multi-binaire
installer/levoile.nsi                 # Script NSIS installateur Windows
installer/config-default.toml         # Config TOML par defaut a distribuer
installer/levoile.ico                 # Icone application pour l'installateur (256px souhaite)
installer/build.sh                    # Script build Unix (goreleaser + makensis)
installer/build.ps1                   # Script build PowerShell Windows
assets/icons/connected.ico            # Copie icone pour archive GoReleaser
assets/icons/connecting.ico           # Copie icone pour archive GoReleaser
assets/icons/disconnected.ico         # Copie icone pour archive GoReleaser
```

**Fichiers a MODIFIER (Task 0) :**
```
cmd/client/main.go                    # Remplacer flags CLI obligatoires par lecture config TOML
                                      # Ajouter auto-decouverte config (exe dir > user AppData)
                                      # Ajouter var version string
cmd/tray/main.go                      # Ajouter var version string
cmd/client/main_test.go               # Adapter tests pour config TOML au lieu de flags CLI
```

**Fichiers a NE PAS TOUCHER :**
```
internal/service/service.go           # Service lifecycle stable
internal/config/config.go             # Config TOML stable
internal/config/paths_windows.go      # Chemins config stables
internal/ipc/*                        # IPC stable
internal/tray/*                       # Tray UI stable
internal/tunnel/*                     # Tunnel stable
internal/dns/*                        # DNS stable
internal/crypto/*                     # Crypto stable
internal/watchdog/*                   # Watchdog stable
internal/relay/*                      # Relais stable
cmd/relay/*                           # Relais stable
deploy/*                              # Deploy Linux stable
```

**Alignement structure projet :**
- `.goreleaser.yaml` a la racine — conforme a l'architecture (prevu dans la structure projet)
- `installer/` nouveau dossier — coherent avec `deploy/` pour Linux
- `assets/icons/` existant mais vide — a remplir avec les icones tray
- `cmd/client/main.go` modifie (Task 0) — necessaire pour la decouverte config par le service SYSTEM
- `cmd/tray/main.go` modifie (Task 0) — ajout `var version string` uniquement
- gen_icons.go ne genere que 16/32/48 px — pour l'icone installateur (256px Windows Explorer), etendre gen_icons.go ou utiliser un outil externe

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Starter Template] — GoReleaser v2.14.1, structure monorepo cmd/ + internal/
- [Source: _bmad-output/planning-artifacts/architecture.md#Infrastructure & Deploiement] — GoReleaser en local, `goreleaser release --clean`
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `.goreleaser.yaml` prevu dans l'arbre projet
- [Source: _bmad-output/planning-artifacts/architecture.md#Architecture Service & Integration OS] — kardianos/service, Windows SCM
- [Source: _bmad-output/planning-artifacts/architecture.md#Configuration & Donnees] — Config TOML dans %AppData%/LeVoile
- [Source: _bmad-output/planning-artifacts/architecture.md#Assets] — Icones tray dans assets/icons/, embedded via //go:embed
- [Source: _bmad-output/planning-artifacts/epics.md#Epic4-Story4.1] — Criteres d'acceptation detailles
- [Source: _bmad-output/planning-artifacts/prd.md#FR20] — Installateur Windows UAC unique
- [Source: _bmad-output/planning-artifacts/prd.md#FR22] — Enregistrement service par installateur
- [Source: _bmad-output/planning-artifacts/prd.md#Distribution & Installation] — Double distribution installateur + portable
- [Source: _bmad-output/implementation-artifacts/3-3-menu-clic-droit-et-controles-utilisateur.md] — Config TOML paths, IPC extensions, service lifecycle, debug logs
- [Source: _bmad-output/implementation-artifacts/3-1-service-systeme-avec-kardianos-service-et-module-ipc.md] — Service install/uninstall/start/stop sub-commands
- [Source: goreleaser.com/customization/nsis/] — NSIS est Pro-only, necessite script standalone
- [Source: goreleaser.com/customization/msi/] — MSI est Pro-only, confirme la strategie NSIS standalone
- [Source: nsis.sourceforge.io] — Documentation NSIS, services plugin, ExecWait patterns

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

Aucun probleme bloquant rencontre durant l'implementation.

### Completion Notes List

- **Task 0** : Refactored `cmd/client/main.go` to auto-discover config TOML via `discoverConfigPath()` and `resolveConfig()`. Priority: `-config` flag > exe dir > AppData. Backward-compatible: `-relay-pubkey` and `-relay-domain` flags still work as overrides. Added `var version string` to both client and tray main.go. 7 new tests added, all passing.
- **Task 1** : Created `.goreleaser.yaml` with 3 builds (service/windows, tray/windows with `-H windowsgui`, relay/linux). Archives configured for Windows zip and Linux tar.gz.
- **Task 2** : Created `installer/config-default.toml` with default relay domain and empty public key (to be injected at build time).
- **Task 3** : Extended `tools/gen_icons.go` to also generate `installer/levoile.ico` (16/32/48/256px) and copy tray icons to `assets/icons/`.
- **Task 4** : Created `installer/levoile.nsi` — NSIS script with single UAC elevation, service registration via kardianos sub-commands, tray auto-start via HKCU Run key, Add/Remove Programs entry, and clean uninstall with DNS restoration.
- **Task 5** : Created `installer/build.sh` and `installer/build.ps1` — orchestration scripts that chain goreleaser build + makensis.
- **Task 6** : Binary names already configured in GoReleaser (levoile-service, levoile-tray, levoile-relay) and referenced consistently in NSIS script.
- **Task 7** : `go test ./...` — all 11 packages pass, `go vet ./...` — zero warnings. GoReleaser/NSIS/VM tests require external tools.

### Change Log

- 2026-03-10: Implemented Story 4.1 — Windows installer with service registration, GoReleaser config, NSIS installer script, build scripts, config auto-discovery.
- 2026-03-10: Code review fixes — [C1] Restructured main() to dispatch install/uninstall/start/stop before config validation (was blocking installation with empty pubkey). [C2] Reordered NSIS script to stop/uninstall service before copying files (was causing file lock on reinstall). [H1] Fixed handleSetAutoStart to use discoverConfigPath instead of config.DefaultPath (wrong path under SYSTEM). [H2] Fixed build.sh CRLF line endings. [M1] Quoted makensis argument in build.ps1. [M3] Added exit code checking for service install/start in NSIS.

### File List

**New files:**
- `.goreleaser.yaml` — GoReleaser multi-binary build configuration
- `installer/levoile.nsi` — NSIS installer script
- `installer/config-default.toml` — Default TOML configuration for distribution
- `installer/levoile.ico` — Application icon for installer (16/32/48/256px)
- `installer/build.sh` — Unix build script (goreleaser + makensis)
- `installer/build.ps1` — PowerShell build script (goreleaser + makensis)
- `assets/icons/connected.ico` — Green tray icon copy for GoReleaser archive
- `assets/icons/connecting.ico` — Orange tray icon copy for GoReleaser archive
- `assets/icons/disconnected.ico` — Red tray icon copy for GoReleaser archive

**Modified files:**
- `cmd/client/main.go` — Config auto-discovery (discoverConfigPath, resolveConfig), var version string, removed mandatory -relay-pubkey flag
- `cmd/client/main_test.go` — 7 new tests for config discovery and resolution
- `cmd/tray/main.go` — Added var version string
- `tools/gen_icons.go` — Extended to generate installer icon (256px) and assets/icons/ copies
