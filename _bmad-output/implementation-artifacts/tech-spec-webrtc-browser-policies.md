---
title: 'Protection WebRTC via policies navigateur'
slug: 'webrtc-browser-policies'
created: '2026-03-15'
status: 'completed'
stepsCompleted: [1, 2, 3, 4]
tech_stack: [Go, Windows Registry API, Linux filesystem, TOML config, JSON policies]
files_to_modify:
  - 'internal/browser/manager.go (NEW - interface, types, logique transversale)'
  - 'internal/browser/manager_windows.go (NEW - implémentation Windows registry)'
  - 'internal/browser/manager_linux.go (NEW - implémentation Linux policies files)'
  - 'internal/browser/manager_darwin.go (NEW - stub macOS no-op)'
  - 'internal/browser/detect.go (NEW - types et constantes navigateurs)'
  - 'internal/browser/detect_windows.go (NEW - détection Windows registry)'
  - 'internal/browser/detect_linux.go (NEW - détection Linux filesystem)'
  - 'internal/browser/detect_darwin.go (NEW - stub macOS no-op)'
  - 'internal/browser/lock_windows.go (NEW - advisory lock via LockFileEx)'
  - 'internal/browser/lock_linux.go (NEW - advisory lock via syscall.Flock)'
  - 'internal/browser/lock_darwin.go (NEW - stub no-op lock)'
  - 'internal/service/service.go (MODIFY - intégration lifecycle + RecoverOrphanPolicies au startup)'
  - 'internal/config/config.go (MODIFY - ajout BrowserPolicies config)'
  - 'internal/ipc/messages.go (MODIFY - ajout champs browser policy dans Response)'
  - 'internal/ipchandler/handler.go (MODIFY - ajout champs browser policy dans handleGetStatus)'
  - 'internal/tray/tray.go (MODIFY - tooltip browser policies + champ notifiedBrowserPolicies + stateKey update)'
  - 'cmd/client/main.go (MODIFY - mapping config BrowserPolicies → service.Config)'
  - 'installer/levoile.nsi (MODIFY - note portable sur completion page)'
code_patterns:
  - 'Interface abstraite OS-specific (dns.DNSManager pattern)'
  - 'Persistance incrémentale JSON (temp file → rename — pattern NOUVEAU, le DNS existant utilise os.WriteFile direct)'
  - 'Recovery orphelins appelé depuis le SERVICE au startup (contrairement au DNS qui est depuis le tray — HKLM et /etc/ nécessitent les droits SYSTEM/root du service)'
  - 'Build tags pour séparation OS (_windows.go, _linux.go, _darwin.go)'
  - 'IPC request-response : tray poll via get_status, service répond avec Response struct'
  - 'sync.Mutex pour état partagé'
  - 'Erreur wrapping avec préfixe package'
  - 'Deep merge chirurgical pour Firefox policies.json'
test_patterns:
  - 'Mock commandRunner pour simuler commandes OS'
  - 'Mock interface lister'
  - 'Tests de concurrence avec goroutines'
  - 'Roundtrip tests pour config Save/Load'
  - 'Fichiers temp dans t.TempDir()'
---

# Tech-Spec: Protection WebRTC via policies navigateur

**Created:** 2026-03-15

## Overview

### Problem Statement

Les navigateurs peuvent fuiter l'IP réelle de l'utilisateur via WebRTC (STUN Binding Requests directs), contournant le tunnel VPN. L'interception STUN existante couvre le réseau, mais les navigateurs peuvent initier des requêtes WebRTC qui exposent l'IP locale ou publique réelle avant qu'elles n'atteignent la couche réseau interceptée.

### Solution

Le service Le Voile applique les policies d'entreprise WebRTC aux navigateurs standard installés sur le système — via registry HKLM (Windows) ou fichiers policies système (Linux). Le même pattern que le DNS est suivi : sauvegarde de l'état original, application au connect, restauration au disconnect, persistance atomique sur disque et recovery d'orphelins. Le tray détecte l'état browser policies via le polling `get_status` existant et affiche une notification demandant à l'utilisateur de redémarrer son navigateur.

### Scope

**In Scope:**

- Support Windows + Linux day one
- Détection dynamique des navigateurs standard installés (Chromium-based + Firefox)
- Registry policies HKLM (Windows) / fichiers policies système `/etc/*/policies/` (Linux)
- Sauvegarde/restauration de l'état original, persistance atomique incrémentale, recovery d'orphelins (restauration inconditionnelle)
- Notification tray demandant à l'utilisateur de redémarrer son navigateur (via polling `get_status` existant, pas de push)
- Intégration au lifecycle du service (startup/shutdown) et recovery au startup du tray
- Note sur l'écran de fin d'installation NSIS : les navigateurs portables ne sont pas protégés
- Deep merge chirurgical pour Firefox `policies.json` (ne toucher qu'à la clé spécifique)

**Out of Scope:**

- macOS (futur — stubs no-op fournis pour compilation)
- Toggle UI pour activer/désactiver la feature (toujours active)
- Navigateurs non-Chromium et non-Firefox (Safari, etc.)
- **Navigateurs portables** — non supportés, avertissement à l'installation
- **Redémarrage automatique des navigateurs** — remplacé par notification utilisateur
- **IPC push mechanism** — inexistant dans le codebase, on utilise le polling existant

## Context for Development

### Codebase Patterns

- **Interface abstraite OS-specific** : `dns.DNSManager` interface (`internal/dns/manager.go`) avec implémentations `windowsManager` / `linuxManager`. Le browser policy manager suivra le même pattern.
- **Persistance JSON** : le DNS existant (`persistState()` dans `manager_windows.go`) utilise un simple `os.WriteFile()` direct — PAS de temp→rename atomique. Le package `browser` introduira un pattern plus robuste avec write atomique (temp → rename) pour la persistance incrémentale, car les données sont plus complexes (multi-navigateurs).
- **Recovery d'orphelins — ATTENTION : pattern différent du DNS** : `RecoverOrphanDNS()` est appelé depuis le **tray** (`tray.go` L239 dans `shutdownServiceAndRestore`, L658 dans `startDNSRecoveryTimer`) car le DNS peut être restauré via `netsh` au niveau utilisateur. Pour les browser policies, la recovery nécessite les droits **SYSTEM** (HKLM Windows) ou **root** (`/etc/` Linux) — donc `RecoverOrphanPolicies()` doit être appelé depuis le **service** au startup, PAS depuis le tray.
- **IPC strictement request-response** : le tray envoie une `Request` via named pipe, le service répond avec une `Response` (`internal/ipc/messages.go` L49-72). Il n'existe AUCUN mécanisme push du service vers le tray. Le tray poll via `ActionGetStatus`. La notification browser policies sera transmise en ajoutant des champs à la `Response` struct existante.
- **Build tags OS** : fichiers séparés `_windows.go`, `_linux.go`, `_darwin.go` avec `//go:build` tags. Pas d'imports croisés.
- **Registry Windows** : `golang.org/x/sys/windows/registry` est importé uniquement dans `internal/tray/sysproxy_windows.go`. Le nouveau package `internal/browser` importera cette dépendance directement — elle est déjà dans `go.mod` mais n'a jamais été utilisée dans le service. Le code registry du nouveau package sera construit from scratch en s'inspirant du pattern `sysproxy_windows.go`.
- **Config mapping** : `cmd/client/main.go` L217-236 construit `svc.Config{}` avec un mapping 1-to-1 depuis la config TOML résolue. Le nouveau champ `BrowserPoliciesEnabled` sera ajouté à ce mapping.
- **Concurrence** : `sync.Mutex` pour protéger les maps d'état, `sync.RWMutex` pour le kill switch.
- **Zero-log strict** : aucun `log.*` call dans le code client. Erreurs propagées via retour.
- **Error wrapping** : `fmt.Errorf("package: context: %w", err)`.

### Files to Reference

| File | Purpose | Notes |
| ---- | ------- | ----- |
| `internal/dns/manager.go` | Interface abstraite `DNSManager` + `commandRunner` type | Pattern à suivre pour l'interface `PolicyManager` |
| `internal/dns/manager_windows.go` | Pattern save/restore/persist/recover Windows | `RecoverOrphanDNS()` = fonction package-level, pas méthode d'interface |
| `internal/dns/manager_linux.go` | Pattern Linux : resolvectl + resolv.conf fallback | `RecoverOrphanDNS()` = fonction package-level |
| `internal/tray/sysproxy_windows.go` | Manipulation registry Windows (seul usage de `x/sys/windows/registry`) | Pattern registry à adapter dans `internal/browser` |
| `internal/tray/tray.go` | `RecoverOrphanDNS()` (L239 shutdown, L658 IPC error), `SysProxy.RecoverOrphan()` (L191 onReady), tooltip via `SetTooltip()`, dedup fields `notifiedVer`/`notifiedRollbackVer` (L80-81), `updateTrayState` stateKey (L674) | Browser policy recovery is in SERVICE (not tray like DNS) due to HKLM/`/etc/` permissions |
| `internal/service/service.go` | Service lifecycle : `run()` L367, `SetResolver` L514, `RestoreResolver` L738 | Points d'intégration pour Apply/Restore |
| `internal/config/config.go` | Config TOML avec Load/Save atomique | Ajout `BrowserPoliciesConfig` |
| `internal/ipc/messages.go` | `Request` L49-52, `Response` L55-72 — strictement request-response | Ajout champs browser policy à `Response` |
| `cmd/client/main.go` | Config mapping L217-236 : resolved config → `svc.Config{}` | Ajout mapping `BrowserPoliciesEnabled` |
| `installer/levoile.nsi` | Installateur NSIS Windows | Ajout note portable sur completion page |

### Technical Decisions

- **HKLM pour les policies Windows** : le service tourne en SYSTEM, accès garanti. Les policies d'entreprise sont le mécanisme officiel supporté par Chrome, Edge, Brave et Firefox.
- **Policies Chromium** : clé `HKLM\SOFTWARE\Policies\{vendor}\{browser}` avec valeur string `WebRtcIPHandlingPolicy` = `"disable_non_proxied_udp"`. Sur Linux : fichier `/etc/{vendor}/policies/managed/levoile-webrtc.json` contenant `{"WebRtcIPHandlingPolicy": "disable_non_proxied_udp"}`.
- **Policies Firefox** : clé `HKLM\SOFTWARE\Policies\Mozilla\Firefox\Preferences` avec valeur DWORD `media.peerconnection.ice.default_address_only` = `1` (true). Sur Linux : deep merge chirurgical dans `{distribution_path}/policies.json`. Structure JSON exacte Firefox : `{"policies": {"Preferences": {"media.peerconnection.ice.default_address_only": true}}}`. **Attention** : `media.peerconnection.ice.default_address_only` est un **nom de clé plat avec des points**, PAS un chemin JSON imbriqué.
- **Détection dynamique** : scanner le registry Windows (`App Paths` + `Uninstall`) et le filesystem Linux (`/usr/bin`, `/usr/local/bin`, `/snap/bin`, `/opt`) pour les navigateurs standard installés.
- **Pas de support portables** : les navigateurs portables ne sont pas couverts. Un avertissement est affiché sur l'écran de fin d'installation NSIS.
- **Notification via tooltip polling** : pas de push IPC (le mécanisme n'existe pas). Pas de toast/notify-send (aucune infrastructure existante dans le tray). Le service stocke l'`ApplyResult` en mémoire et l'expose via de nouveaux champs dans la `Response` du `get_status` existant. Le tray met à jour le **tooltip** (via `systray.SetTooltip()`, déjà utilisé pour afficher l'IP) pour inclure le statut browser policies. Le tooltip est le seul mécanisme de notification existant — pas besoin d'ajouter une dépendance toast.
- **Recovery orphelins depuis le service** : `RecoverOrphanPolicies()` est une **fonction package-level** (pas une méthode d'interface), appelée depuis le **service** au tout début de `run()`, AVANT le tunnel connect. Contrairement à `RecoverOrphanDNS()` (appelé depuis le tray car `netsh` fonctionne au niveau utilisateur), les browser policies nécessitent les droits SYSTEM/root (HKLM sur Windows, `/etc/` sur Linux) — seul le service a ces droits.
- **Recovery inconditionnelle** : si le fichier de persistance `browser-policies-original.json` existe, restaurer inconditionnellement sans vérifier l'état actuel. Idempotent.
- **Pas de kill switch pour les browser policies** : les policies restent en place pendant les pertes de tunnel.
- **Intégration service** : `ApplyPolicies()` AVANT `SetResolver()` L514 (fermer la fenêtre de race condition WebRTC), `RestorePolicies()` avant `RestoreResolver()` L738 dans shutdown.
- **Vérification post-apply** : après écriture des policies, relire la valeur pour confirmer qu'elle est bien en place (détection GPO ou permission refusée). La vérification est ponctuelle — les GPO peuvent écraser les policies plus tard (refresh toutes les ~90 min). Accepté comme limitation.
- **Nettoyage fichiers temp orphelins** : au startup, nettoyer les fichiers `browser-policies-*.tmp`. Si un temp existe sans fichier final, l'utiliser pour la recovery.
- **Validation JSON stricte pour Firefox** : `json.Unmarshal` strict. Si le parse échoue, ne PAS toucher au fichier.
- **Persistance incrémentale** : write atomique complet (JSON marshal du state entier → temp → rename) après chaque navigateur modifié. Quelques millisecondes par navigateur.
- **Rollback si persistance échoue** : rollback via l'état **en mémoire** (pas le fichier persisté). Si le rollback lui-même échoue (ex: permissions révoquées), collecter l'erreur et la retourner — le fichier de persistance partiel permettra la recovery au prochain startup.
- **Reverse merge chirurgical au restore (Linux Firefox)** : relire le fichier actuel, retirer uniquement notre clé, préserver les modifications tierces.
- **Lock advisory OS** : utiliser `syscall.Flock` (Linux, dans `lock_linux.go`) et `LockFileEx` (Windows, dans `lock_windows.go`) — fichiers séparés avec build tags car les syscalls sont OS-specific. Si le processus crashe, le lock est automatiquement libéré par l'OS. Le lock est tenu pendant toute la session VPN. `RecoverOrphanPolicies()` n'acquiert PAS le lock car elle s'exécute au startup du service avant toute instance de `PolicyManager`.
- **Default `Enabled: false`** : cohérent avec les autres features optionnelles (Blocklist, Registry, HTTPProxy). L'utilisateur doit opt-in explicitement dans `config.toml`. Évite de modifier le registry système silencieusement à la mise à jour.
- **Langue des notifications** : les strings de notification tray sont en **anglais**, cohérent avec le reste du codebase. Pas de système de localisation.

## Implementation Plan

### Tasks

- [x] **Task 1 : Types et constantes navigateurs** (`internal/browser/detect.go`)
  - File : `internal/browser/detect.go`
  - Action : Créer le fichier avec les types partagés cross-platform
  - Détails :
    - Type `BrowserFamily` : `Chromium` | `Firefox`
    - Type `BrowserInfo` struct : `Name string`, `Family BrowserFamily`, `PolicyPath string`
    - Constantes pour les noms de navigateurs connus : Chrome, Edge, Brave, Vivaldi, Opera, Chromium, Firefox
    - Map `chromiumVendors` : associe chaque navigateur à son vendor registry path (Windows) et policy dir (Linux)
    - Fichier sans build tags (types partagés)

- [x] **Task 2 : Détection navigateurs Windows** (`internal/browser/detect_windows.go`)
  - File : `internal/browser/detect_windows.go`
  - Action : Implémenter la détection des navigateurs standard installés via le registry Windows
  - Détails :
    - Build tag `//go:build windows`
    - Fonction `DetectBrowsers() ([]BrowserInfo, error)`
    - Scanner `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths` pour les exécutables navigateurs connus (chrome.exe, msedge.exe, brave.exe, firefox.exe, vivaldi.exe, opera.exe)
    - Scanner `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall` et `HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall` pour les navigateurs avec `DisplayName` + `InstallLocation`
    - Pour chaque navigateur trouvé, remplir `BrowserInfo` avec le chemin de policy registry approprié
    - Dédupliquer les résultats (même navigateur trouvé via plusieurs sources)

- [x] **Task 3 : Détection navigateurs Linux** (`internal/browser/detect_linux.go`)
  - File : `internal/browser/detect_linux.go`
  - Action : Implémenter la détection des navigateurs standard installés via le filesystem Linux
  - Détails :
    - Build tag `//go:build linux`
    - Fonction `DetectBrowsers() ([]BrowserInfo, error)`
    - Chercher les exécutables dans : `/usr/bin`, `/usr/local/bin`, `/snap/bin`, `/opt`
    - Noms à chercher : `google-chrome`, `google-chrome-stable`, `chromium`, `chromium-browser`, `microsoft-edge`, `brave-browser`, `vivaldi`, `opera`, `firefox`
    - **Chemins policies Chromium Linux** :
      - Chrome : `/etc/opt/chrome/policies/managed/`
      - Chromium : `/etc/chromium/policies/managed/` ou `/etc/chromium-browser/policies/managed/`
      - Edge : `/etc/opt/edge/policies/managed/`
      - Brave : `/etc/brave/policies/managed/`
      - Vivaldi : `/etc/vivaldi/policies/managed/`
      - Opera : `/etc/opera/policies/managed/`
    - **Chemins policies Firefox Linux** :
      - Standard : `/usr/lib/firefox/distribution/policies.json`
      - Snap : `/etc/firefox/policies/policies.json`

- [x] **Task 4 : Interface abstraite PolicyManager + logique transversale** (`internal/browser/manager.go`)
  - File : `internal/browser/manager.go`
  - Action : Définir l'interface, les types partagés, et la logique transversale
  - Détails :
    - Type `ApplyResult` struct :
      ```go
      type ApplyResult struct {
          Applied []string        // browsers with policy successfully applied
          Failed  []BrowserError  // browsers that failed + reason
      }
      type BrowserError struct {
          Name   string
          Reason string
      }
      ```
    - Interface `PolicyManager` :
      ```go
      type PolicyManager interface {
          ApplyPolicies(ctx context.Context) (*ApplyResult, error)
          RestorePolicies(ctx context.Context) error
      }
      ```
    - **Fonction package-level** (PAS méthode d'interface, pattern `RecoverOrphanDNS`) :
      ```go
      func RecoverOrphanPolicies(ctx context.Context) error
      ```
      Signature avec `context.Context` (cohérent avec `RecoverOrphanDNS(ctx)`). Appelée depuis le **service** au startup de `run()`, avant le tunnel connect. Nécessite les droits SYSTEM/root (HKLM/`/etc/`). Elle lit le fichier de persistance, restaure, et nettoie.
    - Fonction constructeur `NewPolicyManager() PolicyManager` (appelle l'implémentation OS-specific)
    - Type `policyPersistedState` struct : `Browsers []browserSavedState` contenant le nom, la famille, le chemin de policy, et les valeurs originales sauvegardées
    - Constantes : `policyStateFile = "browser-policies-original.json"`
    - Fonctions utilitaires partagées (dans `manager.go`, pas de build tag) :
      - `policyStatePath() string` (Windows : `%ProgramData%\LeVoile\`, Linux : `/var/lib/levoile/`)
      - `persistStateIncremental(state *policyPersistedState) error` — write atomique complet (JSON marshal → temp → rename) après chaque navigateur modifié
      - `loadPersistedState() (*policyPersistedState, error)` — lire le fichier de persistance ou fichier temp orphelin
      - `cleanOrphanTemps()` — nettoyer les `browser-policies-*.tmp`, promouvoir un temp en fichier recovery si final absent
    - Fonctions OS-specific pour le lock advisory (fichiers séparés nécessaires car syscalls OS-specific) :
      - `internal/browser/lock_windows.go` : `acquireLock() (io.Closer, error)` via `LockFileEx`
      - `internal/browser/lock_linux.go` : `acquireLock() (io.Closer, error)` via `syscall.Flock`
      - `internal/browser/lock_darwin.go` : stub no-op retournant un closer vide
    - **Note technique** : la persistance incrémentale utilise un write atomique (temp → rename) — pattern NOUVEAU dans ce codebase (le DNS existant utilise `os.WriteFile` direct). Le lock advisory est libéré automatiquement par l'OS si le processus crashe. Le lock est acquis au début d'`ApplyPolicies` et libéré dans `RestorePolicies`. La recovery (`RecoverOrphanPolicies`) n'a PAS besoin du lock car elle s'exécute au startup du service avant que `ApplyPolicies` ne soit appelé — le lock n'existe pas encore.

- [x] **Task 5 : Implémentation Windows** (`internal/browser/manager_windows.go`)
  - File : `internal/browser/manager_windows.go`
  - Action : Implémenter le policy manager Windows via registry HKLM
  - Détails :
    - Build tag `//go:build windows`
    - Import `golang.org/x/sys/windows/registry` (déjà dans `go.mod`, mais première utilisation hors tray)
    - Struct `windowsPolicyManager` avec `mu sync.Mutex`, `savedState *policyPersistedState`, `lockCloser io.Closer`
    - **ApplyPolicies(ctx)** :
      1. Acquérir le lock advisory via `acquireLock()`. Si échec, retourner immédiatement avec `ApplyResult` vide.
      2. Appeler `DetectBrowsers()` pour obtenir la liste des navigateurs standard installés
      3. Pour chaque navigateur Chromium : ouvrir/créer `HKLM\SOFTWARE\Policies\{vendor}\{browser}`, lire la valeur actuelle de `WebRtcIPHandlingPolicy` (sauvegarder si existante), écrire string `"disable_non_proxied_udp"`. **Persistance incrémentale** après chaque navigateur.
      4. Pour chaque Firefox : ouvrir/créer `HKLM\SOFTWARE\Policies\Mozilla\Firefox\Preferences`, lire la valeur actuelle de `media.peerconnection.ice.default_address_only` (sauvegarder si existante), écrire DWORD `1`. Persistance incrémentale.
      5. **Vérification post-apply** : relire chaque valeur écrite pour confirmer. Si la valeur ne correspond pas (GPO, permissions), exclure ce navigateur de Applied, ajouter à Failed.
      6. **Si la persistance incrémentale a échoué** : rollback via l'état **en mémoire** (pas le fichier). Si le rollback échoue aussi, retourner l'erreur — le fichier de persistance partiel permettra la recovery au prochain startup tray.
      7. Retourner `*ApplyResult` avec Applied et Failed
    - **Note** : la restauration Windows est chirurgicale — on ne supprime/restaure que les clés individuelles que Le Voile a créées/modifiées.
    - **RestorePolicies(ctx)** :
      1. Pour chaque navigateur dans `savedState` : restaurer la valeur originale ou supprimer la clé si elle n'existait pas
      2. Supprimer le fichier de persistance
      3. Libérer le lock advisory via `lockCloser.Close()`
    - **Clés registry Chromium** (map vendor → path) :
      - Chrome : `SOFTWARE\Policies\Google\Chrome`
      - Edge : `SOFTWARE\Policies\Microsoft\Edge`
      - Brave : `SOFTWARE\Policies\BraveSoftware\Brave`
      - Vivaldi : `SOFTWARE\Policies\Vivaldi`
      - Opera : `SOFTWARE\Policies\Opera Software\Opera`
      - Chromium : `SOFTWARE\Policies\Chromium`
    - **Clé registry Firefox** : `SOFTWARE\Policies\Mozilla\Firefox\Preferences`
    - **`RecoverOrphanPolicies()` Windows** (dans ce fichier, appelé par la fonction package-level) :
      1. `cleanOrphanTemps()`
      2. Si `policyStateFile` existe : lire, restaurer inconditionnellement, nettoyer

- [x] **Task 6 : Implémentation Linux** (`internal/browser/manager_linux.go`)
  - File : `internal/browser/manager_linux.go`
  - Action : Implémenter le policy manager Linux via fichiers policies système
  - Détails :
    - Build tag `//go:build linux`
    - Struct `linuxPolicyManager` avec `mu sync.Mutex`, `savedState *policyPersistedState`, `lockCloser io.Closer`
    - **ApplyPolicies(ctx)** :
      1. Acquérir le lock advisory via `acquireLock()`. Si échec, retourner immédiatement.
      2. Appeler `DetectBrowsers()` pour obtenir la liste
      3. Pour chaque navigateur Chromium : créer le répertoire de policies si nécessaire, sauvegarder `levoile-webrtc.json` si présent, écrire `{"WebRtcIPHandlingPolicy": "disable_non_proxied_udp"}`. Persistance incrémentale.
      4. Pour chaque Firefox : **deep merge chirurgical** dans `{distribution_path}/policies.json` — `json.Unmarshal` strict. Si parse échoue → ne PAS toucher, ajouter à Failed. Si OK : modifier uniquement la clé plat `media.peerconnection.ice.default_address_only` dans `policies.Preferences` (structure exacte : `{"policies": {"Preferences": {"media.peerconnection.ice.default_address_only": true}}}`). Valider le JSON résultant. Écriture atomique. Sauvegarder le contenu original complet. Persistance incrémentale.
      5. Si persistance échoue : rollback via état **en mémoire**.
      6. Retourner `*ApplyResult`
    - **RestorePolicies(ctx)** :
      1. Chromium : supprimer `levoile-webrtc.json` ou restaurer le contenu original
      2. Firefox : **reverse merge chirurgical** — relire le `policies.json` actuel, retirer uniquement la clé `media.peerconnection.ice.default_address_only` de `policies.Preferences`, restaurer sa valeur originale si elle existait. Écriture atomique. Si le fichier n'existait pas avant Le Voile, le supprimer.
      3. Supprimer le fichier de persistance
      4. Libérer le lock advisory
    - **`RecoverOrphanPolicies()` Linux** : `cleanOrphanTemps()`, puis restaurer inconditionnellement si fichier persistance existe.

- [x] **Task 7 : Stubs macOS** (`internal/browser/manager_darwin.go`, `internal/browser/detect_darwin.go`)
  - Files : `internal/browser/manager_darwin.go`, `internal/browser/detect_darwin.go`
  - Action : Créer des stubs no-op pour compilation cross-platform
  - Détails :
    - `detect_darwin.go` : build tag `//go:build darwin`, `DetectBrowsers()` retourne `nil, nil`
    - `manager_darwin.go` : build tag `//go:build darwin`, `NewPolicyManager()` retourne stub no-op, `RecoverOrphanPolicies()` retourne `nil`

- [x] **Task 8 : Notification tray via tooltip polling** (`internal/ipc/messages.go` + `internal/ipchandler/handler.go` + `internal/tray/tray.go`)
  - Files : `internal/ipc/messages.go`, `internal/ipchandler/handler.go`, `internal/tray/tray.go`
  - Action : Transmettre l'état browser policies via le `get_status` existant et l'afficher dans le tooltip
  - Détails :
    - **`messages.go`** : ajouter des champs optionnels à la struct `Response` (L55-72) :
      ```go
      BrowserPoliciesApplied []string `json:"browser_policies_applied,omitempty"`
      BrowserPoliciesFailed  []string `json:"browser_policies_failed,omitempty"`
      ```
    - **`service.go`** : ajouter une méthode accessor sur le struct `Program` :
      ```go
      func (p *Program) BrowserPolicyApplied() []string
      func (p *Program) BrowserPolicyFailed() []string
      ```
      Ces méthodes lisent `p.browserPolicyResult` (protégé par mutex si nécessaire).
    - **`ipchandler/handler.go`** : dans `handleGetStatus` (L58-102), ajouter :
      ```go
      resp.BrowserPoliciesApplied = prg.BrowserPolicyApplied()
      resp.BrowserPoliciesFailed = prg.BrowserPolicyFailed()
      ```
    - **`tray.go`** :
      - Ajouter un champ `notifiedBrowserPolicies bool` au struct `Tray` (pattern existant : `notifiedVer`, `notifiedRollbackVer` L80-81)
      - Dans `updateTrayState` (L674) : ajouter les champs browser policies au `stateKey` pour éviter que le dedup ne supprime la mise à jour :
        ```go
        stateKey := resp.Status + "|" + resp.IP + "|" + resp.Error + "|" +
            fmt.Sprintf("%v|%d|%v", resp.BlocklistEnabled, resp.HTTPProxySeq,
                len(resp.BrowserPoliciesApplied))
        ```
      - Après le stateKey check, si `resp.BrowserPoliciesApplied` non-vide et `!t.notifiedBrowserPolicies` :
        - Mettre à jour le **tooltip** via `systray.SetTooltip()` pour inclure le statut browser policies (ex: append `" | WebRTC: Chrome, Firefox — restart browsers"`)
        - Marquer `t.notifiedBrowserPolicies = true`
      - **Pas de toast/notify-send** — aucune infrastructure existante dans le tray. Le tooltip est le seul mécanisme disponible.
      - **Pas de notification au shutdown** — le tray est probablement déjà fermé
    - **Note** : la recovery orphelins n'est PAS dans le tray (contrairement au DNS). Elle est dans le service (Task 10).

- [x] **Task 9 : Configuration TOML** (`internal/config/config.go`)
  - File : `internal/config/config.go`
  - Action : Ajouter la config `BrowserPolicies` au struct `Config`
  - Détails :
    - Ajouter `BrowserPolicies BrowserPoliciesConfig \`toml:"browser_policies"\`` au struct Config (après HTTPProxy)
    - Struct `BrowserPoliciesConfig` : `Enabled bool \`toml:"enabled"\`` (défaut `false` — cohérent avec les autres features optionnelles, opt-in explicite)
    - Pas d'autres champs nécessaires — la détection est automatique

- [x] **Task 10 : Intégration service + config mapping** (`internal/service/service.go` + `cmd/client/main.go`)
  - Files : `internal/service/service.go`, `cmd/client/main.go`
  - Action : Intégrer le PolicyManager dans le lifecycle du service et mapper la config
  - Détails :
    - **`cmd/client/main.go`** (L217-236) : ajouter `BrowserPoliciesEnabled: rc.browserPoliciesEnabled,` dans le struct `svc.Config{}`. Ajouter `browserPoliciesEnabled` dans le resolved config struct, mappé depuis `cfg.BrowserPolicies.Enabled`.
    - **`service.go`** : ajouter les champs au struct `Program` :
      - `browserPolicyMgr browser.PolicyManager`
      - `browserPolicyResult *browser.ApplyResult` (stocké pour le polling tray)
    - Ajouter `BrowserPoliciesEnabled bool` au struct `Config` du service
    - **Startup** dans `run()` :
      1. **Au tout début** (avant tunnel connect, ~L380) : appeler `browser.RecoverOrphanPolicies(ctx)`. Non-fatal si erreur. Ceci restaure les policies orphelines d'un crash précédent. Nécessite les droits SYSTEM/root que le service possède. Note : contrairement à `RecoverOrphanDNS` (appelé depuis le tray), la recovery browser policies est dans le service car HKLM et `/etc/` nécessitent les droits élevés.
      2. **AVANT `SetResolver()` (L514)** — c'est-à-dire après tunnel connect (L427), proxy start (L491), HTTP proxy (L501), STUN start (L509), mais AVANT le DNS. Si le tunnel ne se connecte pas, les policies ne sont pas appliquées (trade-off accepté : pas de protection WebRTC sans VPN actif).
      3. Si `config.BrowserPoliciesEnabled` : créer `browser.NewPolicyManager()`, appeler `ApplyPolicies(ctx)`
      4. Stocker le `*ApplyResult` sur `p.browserPolicyResult` pour le polling tray
      5. En cas d'erreur critique : non-fatal, continuer sans browser policies
    - **Shutdown** dans `shutdown()` — AVANT `RestoreResolver()` (L738) :
      1. Si `browserPolicyMgr != nil` : appeler `RestorePolicies(ctx)` avec `context.Background()`
    - **Safety net defer** : ajouter un defer similaire au DNS emergency restore (L526-540) pour restaurer les policies en cas de panic/crash du service

- [x] **Task 11 : Note installateur NSIS** (`installer/levoile.nsi`)
  - File : `installer/levoile.nsi`
  - Action : Ajouter une note sur la completion page
  - Détails :
    - Le fichier NSIS existe à `installer/levoile.nsi`
    - Ajouter un `DetailPrint` ou `MessageBox` sur la completion page :
      `"Note: If you enable WebRTC browser protection (browser_policies.enabled in config.toml), portable browsers will not be protected. Only standard-installed browsers benefit from automatic protection."`
    - Syntaxe NSIS : utiliser `!insertmacro MUI_FINISHPAGE_TEXT` ou un `MessageBox MB_OK` dans la section finish
    - Le message mentionne explicitement que la feature est opt-in via config (cohérent avec `Enabled: false` par défaut)

- [x] **Task 12 : Tests unitaires**
  - Files : `internal/browser/manager_test.go`, `internal/browser/detect_test.go`
  - Action : Écrire les tests unitaires pour le package browser
  - Détails :
    - **detect_test.go** : tester la logique de parsing/déduplication avec des données mockées
    - **manager_test.go** :
      - Mock du registry (Windows) ou du filesystem (Linux) pour tester ApplyPolicies/RestorePolicies
      - Test persistState/removePersistedState avec `t.TempDir()`
      - Test RecoverOrphanPolicies (fonction package-level) : vérifier restauration inconditionnelle si fichier persistance existe
      - Test de concurrence (pattern kill_switch_test.go)
      - Test roundtrip : Apply → vérifier état → Restore → vérifier état restauré
      - Test deep merge Firefox avec structure exacte `{"policies": {"Preferences": {"media.peerconnection.ice.default_address_only": true}}}` : vérifier que les autres policies sont préservées
      - Test deep merge Firefox avec JSON invalide : vérifier fichier non modifié
      - Test vérification post-apply : simuler GPO → navigateur exclu de Applied, ajouté à Failed
      - Test recovery avec fichier temp orphelin
      - Test ApplyResult contient les bonnes listes Applied et Failed
      - Test persistance incrémentale : vérifier fichier mis à jour après chaque navigateur
      - Test rollback si persistance échoue : rollback via état mémoire, pas fichier
      - Test rollback qui échoue : vérifier que l'erreur est retournée et le fichier partiel reste pour recovery
      - Test reverse merge Firefox : modifier policies.json pendant session, vérifier préservation au restore
      - Test advisory lock : vérifier que la deuxième acquisition échoue
      - Test advisory lock libéré après Restore et RecoverOrphan

### Acceptance Criteria

- [x] **AC 1** : Given a Windows system with Chrome and Firefox installed and `BrowserPolicies.Enabled = true` in config, when the Le Voile service starts and connects the tunnel, then WebRTC policies are applied in the registry for both browsers, and the tray tooltip includes the browser policy status on the next `get_status` poll.

- [x] **AC 2** : Given a Linux system with Chromium and Firefox installed and `BrowserPolicies.Enabled = true` in config, when the Le Voile service starts and connects the tunnel, then policy files are created/modified (`levoile-webrtc.json` for Chromium, deep merge in `policies.json` for Firefox with exact structure `{"policies": {"Preferences": {"media.peerconnection.ice.default_address_only": true}}}`), and the tray tooltip includes the browser policy status.

- [x] **AC 3** : Given WebRTC policies applied by Le Voile, when the service shuts down normally, then original policies are restored silently (registry keys removed/restored, files removed/restored), the persistence file is cleaned up, and the advisory lock is released. No tray notification.

- [x] **AC 4** : Given the Le Voile service that crashes with policies applied, when the service restarts, then `RecoverOrphanPolicies()` (called from service startup, before tunnel connect) detects the persistence file and unconditionally restores original policies. Note: unlike `RecoverOrphanDNS` (tray), browser policy recovery runs in the service because HKLM/`/etc/` require elevated privileges.

- [x] **AC 5** : Given a Firefox with an existing `policies.json` containing other enterprise policies, when `ApplyPolicies()` is called, then only the flat key `media.peerconnection.ice.default_address_only` is added/modified in `policies.Preferences`, all other policies are preserved.

- [x] **AC 6** : Given no standard browser installed on the system, when `ApplyPolicies()` is called, then the function returns an empty `ApplyResult` with no error.

- [x] **AC 7** : Given `BrowserPolicies.Enabled = false` in the config (default), when the service starts, then no browser policy is applied.

- [x] **AC 8** : Given an error applying policies to one browser (permissions, GPO, SELinux), when `ApplyPolicies()` is called, then the error is non-fatal, policies are applied to other accessible browsers, `ApplyResult.Failed` contains the failed browser with reason, and the service continues normally.

- [x] **AC 9** : Given the Le Voile NSIS installer completion page, when the user finishes installation, then a warning message indicates that portable browsers are not protected against WebRTC leaks.

- [x] **AC 10** : Given a service crash during atomic persistence write (temp file exists, final file absent), when the service restarts, then `RecoverOrphanPolicies()` promotes the temp file and uses it for recovery.

- [x] **AC 11** : Given a Firefox with invalid JSON in `policies.json` (BOM, trailing comma, etc.), when `ApplyPolicies()` attempts the deep merge, then the file is NOT modified, the browser is added to `ApplyResult.Failed`, and other browsers continue.

- [x] **AC 12** : Given a service crash after applying Chrome's policy but before Firefox, when the service restarts, then `RecoverOrphanPolicies()` uses the incremental persistence file to restore only Chrome.

- [x] **AC 13** : Given a full disk during `persistStateIncremental()`, when persistence fails, then all policies already applied are rolled back using in-memory state and `ApplyPolicies()` returns an error.

- [x] **AC 14** : Given an admin who modifies Firefox `policies.json` while Le Voile is running, when `RestorePolicies()` is called at shutdown, then the reverse merge removes only the Le Voile key and preserves the admin's additions.

- [x] **AC 15** : Given two processes attempting to apply policies simultaneously, when the second process calls `ApplyPolicies()`, then it fails to acquire the advisory lock and returns immediately with an empty `ApplyResult`.

## Additional Context

### Dependencies

- **`golang.org/x/sys/windows/registry`** : already in `go.mod` (used by `internal/tray/sysproxy_windows.go`). First usage outside the tray package — `internal/browser/manager_windows.go` will import it directly.
- **No new external Go module dependency**.
- **Prerequisite** : service runs as SYSTEM (Windows) or root (Linux) for HKLM / `/etc/` write access.

### Testing Strategy

- **Unit tests** : mock registry for Windows, temp filesystem for Linux. Pattern from `dns/manager_windows_test.go`.
- **Integration tests** : manual verification on dev machine that policies are written/read/restored for Chrome and Firefox.
- **Recovery test** : simulate crash (kill -9 service), verify tray startup restores via `RecoverOrphanPolicies`.
- **Regression test** : verify service startup/shutdown works normally with no browsers installed.
- **Deep merge Firefox test** : create `policies.json` with third-party policies, apply + restore, verify third-party integrity.
- **WebRTC end-to-end test** : after policy application and manual browser restart, open `chrome://webrtc-internals` or a WebRTC test site to verify real IP is not exposed.

### Notes

- **Firefox `policies.json` exact structure** — The key `media.peerconnection.ice.default_address_only` is a **flat key name with dots**, not a nested JSON path. Correct structure: `{"policies": {"Preferences": {"media.peerconnection.ice.default_address_only": true}}}`. Do NOT deep-nest as `media → peerconnection → ice → ...`.
- **Portable browsers not supported** — Portables would require `user.js` + process detection (fragile). Warning shown at installation. Users can configure manually.
- **No automatic browser restart** — Forced restart risks cutting video calls, losing form data. Tray notification is more respectful. Policies take effect on next browser launch.
- **Recovery from service, not tray** — Unlike `RecoverOrphanDNS()` (called from tray because `netsh` works at user level), `RecoverOrphanPolicies()` is called from `service.go` at `run()` startup because HKLM registry writes and `/etc/` file writes require SYSTEM/root privileges that only the service has.
- **Advisory lock via OS syscalls** — Uses `syscall.Flock` (Linux) / `LockFileEx` (Windows) in separate build-tagged files (`lock_linux.go`, `lock_windows.go`). Automatically released on process crash. The lock is held for the entire VPN session. Recovery does NOT acquire the lock (runs at startup before `ApplyPolicies`).
- **Incremental persistence is a NEW pattern** — The existing DNS `persistState()` uses simple `os.WriteFile()`. The browser package introduces atomic persistence (temp → rename) as a more robust approach for multi-browser state. This is intentionally different from the DNS pattern.
- **Browser install/uninstall during session** — `DetectBrowsers()` runs once at `ApplyPolicies` time. Browsers installed mid-session are not protected. Browsers uninstalled mid-session: `RestorePolicies` will attempt to restore and silently ignore errors for missing browsers (same error handling as any individual browser failure).
- **Tooltip, not toast** — The tray has no toast/notification infrastructure. Browser policy status is shown via `systray.SetTooltip()` (same mechanism as IP display). No new notification library needed.
- **Rollback uses in-memory state** — If persistence fails, rollback iterates `savedState` in memory (the accurate source), not the on-disk file (which may be partial/absent).
- **Reverse merge (Linux Firefox)** — At restore, re-read current `policies.json`, remove only our key, preserve third-party changes made during session.
- **Default `Enabled: false`** — Consistent with other optional features (Blocklist, HTTPProxy). Avoids silently modifying system registry on upgrade. User must opt-in via `config.toml`.
- **GPO timing limitation** — Post-apply verification catches existing GPO conflicts. GPO applied later (~90 min refresh) may overwrite. Accepted as minor limitation.
- **macOS stubs** — No-op stubs for cross-platform compilation. Future implementation with `.plist` in `/Library/Managed Preferences/`.
- **Performance** — Browser detection and policy writing are one-shot operations (startup/shutdown only). No steady-state performance impact.
