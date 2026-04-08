# Story 11.3: Installation Automatique et Cohabitation SysProxy

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que l'extension s'installe automatiquement dans mes navigateurs et cohabite avec le SysProxy existant,
Afin d'être protégé sans aucune action manuelle, dans les navigateurs comme dans les autres applications.

## Acceptance Criteria

**AC1 — Installation automatique de l'extension via politiques navigateur**
**Given** Le Voile est installé et le service démarre
**When** les politiques navigateur sont appliquées (via `internal/browser/`)
**Then** l'extension est installée automatiquement dans Chromium (registre Windows `ExtensionSettings`) et Firefox (registre Windows `ExtensionSettings`)
**And** les fichiers extension (CRX pré-signé, XPI pré-signé AMO, updates.xml) sont déployés sur disque (`C:\ProgramData\LeVoile\extension\`)
**And** aucune action utilisateur n'est requise
**And** les politiques d'entreprise pré-existantes ne sont PAS écrasées (merge JSON obligatoire)

**AC2 — Cohabitation extension + SysProxy sans conflit**
**Given** l'extension est active dans le navigateur ET le SysProxy WinINET est configuré
**When** l'utilisateur navigue dans le navigateur
**Then** l'extension gère le routage navigateur (politique d'entreprise > SysProxy)
**And** le SysProxy reste actif pour les applications hors navigateur (Electron, curl, Windows native apps)
**And** les deux pointent vers le même proxy local 127.0.0.1:50113

**AC3 — Compatibilité Chrome MV3 (policy-installed)**
**Given** l'extension est force-installée dans Chrome via `ExtensionSettings`
**When** l'extension est chargée
**Then** elle respecte les contraintes Manifest V3 (service worker, pas de background page persistante)
**And** `webRequestBlocking` est disponible (extension policy-installed)

**AC4 — Compatibilité Firefox (policy-installed)**
**Given** l'extension est force-installée dans Firefox via `ExtensionSettings`
**When** l'extension est chargée
**Then** elle utilise l'API `proxy.onRequest` (MV2) compatible Firefox
**And** le XPI est signé AMO (Firefox refuse les XPI non-signés même via politique)

**AC5 — Restauration propre à l'arrêt et au désinstall**
**Given** Le Voile est arrêté (Quitter via tray) ou désinstallé
**When** le shutdown s'exécute
**Then** les politiques navigateur sont restaurées à leur état original (service: `RestorePolicies()`)
**And** le SysProxy WinINET est restauré (tray: `SysProxy.Restore()`)
**And** les fichiers extension déployés sont supprimés
**And** aucune politique orpheline ne subsiste

**AC6 — Récupération crash (orphan recovery)**
**Given** Le Voile a crashé sans restaurer les politiques/proxy
**When** le service redémarre
**Then** `RecoverOrphanPolicies()` détecte l'état orphelin et restaure les politiques
**And** `SysProxy.RecoverOrphan()` détecte le proxy orphelin et restaure les paramètres WinINET
**And** un re-apply automatique est effectué après la récupération

## Tasks / Subtasks

- [ ] **Task 1 : Vérifier et adapter `internal/browser/extension_embed.go` — déploiement fichiers extension** (AC: 1, 3, 4)
  - [ ] 1.1 Vérifier que `//go:embed all:extension_assets` embarque correctement les assets CRX/XPI pré-signés depuis `internal/browser/extension_assets/build/`
  - [ ] 1.2 Vérifier que `deployExtensionFiles()` extrait vers `C:\ProgramData\LeVoile\extension\` :
    - `chrome/levoile.crx` (CRX v3 pré-signé)
    - `levoile.xpi` (XPI signé AMO)
    - `chrome/updates.xml` (généré dynamiquement au runtime avec chemin `file:///` et version)
  - [ ] 1.3 Vérifier que `extractExtensionIDFromCRX()` dérive correctement l'ID extension depuis le header CRX protobuf (SHA256 de la clé publique → hex lowercase)
  - [ ] 1.4 Vérifier que la version est lue depuis le `manifest.json` embarqué pour le `updates.xml`
  - [ ] 1.5 Adapter si nécessaire pour la nouvelle architecture 2-processus (service appelle `ApplyPolicies` → `deployExtensionFiles`)

- [ ] **Task 2 : Vérifier et adapter `internal/browser/manager_windows.go` — politiques extension** (AC: 1, 3, 4, 5)
  - [ ] 2.1 Vérifier `applyExtensionChromium()` : écriture `ExtensionSettings` JSON dans `HKLM\SOFTWARE\Policies\{vendor}` pour chaque navigateur Chromium détecté (Chrome, Edge, Brave, Vivaldi, Opera). **MERGE obligatoire** : lire la valeur existante, parser JSON, ajouter l'entrée Le Voile, réécrire le JSON fusionné. Sauvegarder la valeur originale dans `policyPersistedState`
  - [ ] 2.2 Vérifier `applyExtensionFirefox()` : écriture `ExtensionSettings` dans `HKLM\SOFTWARE\Policies\Mozilla\Firefox`. Gecko ID = `levoile@plateformeliberte.fr`. **MERGE obligatoire** idem. `install_url` = `file:///C:/ProgramData/LeVoile/extension/levoile.xpi`
  - [ ] 2.3 Vérifier que `mergeExtensionSettings()` gère le guard E4 : si `json.Unmarshal()` échoue sur le JSON pré-existant (corrompu), ne PAS écraser — remonter l'erreur, sauvegarder la valeur brute, abandonner proprement
  - [ ] 2.4 Vérifier la restauration dans `restoreOne()` : restaurer `ExtensionSettings` original ou supprimer la clé si elle n'existait pas avant
  - [ ] 2.5 Vérifier `cleanupExtensionFiles()` dans `RestorePolicies()` : supprime `C:\ProgramData\LeVoile\extension\`
  - [ ] 2.6 Vérifier que `ApplyPolicies()` appelle `deployExtensionFiles()` AVANT d'écrire les politiques registre
  - [ ] 2.7 Vérifier la vérification post-apply : relire les valeurs registre pour confirmer qu'elles ont bien été écrites

- [ ] **Task 3 : Vérifier et adapter la cohabitation SysProxy + Extension** (AC: 2)
  - [ ] 3.1 Vérifier que `internal/tray/sysproxy_windows.go` configure le proxy WinINET sur `127.0.0.1:50113` (même port que le proxy HTTP CONNECT local)
  - [ ] 3.2 Vérifier que `SysProxy.Set()` configure le `ProxyOverride` (bypass list) incluant : localhost, 127.0.0.1, `*.local`, `<local>`, relay domain, OCSP/CRL servers, Windows Update, CDNs vidéo
  - [ ] 3.3 Vérifier que le flux tray est correct :
    - `syncSysProxy(true, addr)` → `Save()` + `Set(addr)` quand le proxy HTTP est activé
    - `syncSysProxy(false, "")` → `Restore()` quand le proxy est désactivé ou à l'arrêt
  - [ ] 3.4 Vérifier que le commentaire de cohabitation est présent dans `extension_embed.go` : l'extension a priorité dans les navigateurs (politique d'entreprise > SysProxy), le SysProxy gère les apps hors navigateur
  - [ ] 3.5 Vérifier qu'aucun conflit n'existe entre les clés registre WebRTC (`WebRtcIPHandlingPolicy`) et extension (`ExtensionSettings`) — clés distinctes, pas d'interférence

- [ ] **Task 4 : Vérifier et adapter le service pour l'orchestration des politiques** (AC: 1, 5, 6)
  - [ ] 4.1 Vérifier `internal/service/service.go` : `RecoverOrphanPolicies()` appelé au démarrage (ligne ~461)
  - [ ] 4.2 Vérifier que si `BrowserPoliciesEnabled` : `NewPolicyManager()` → `ApplyPolicies(ctx)` (ligne ~601-612)
  - [ ] 4.3 Vérifier le shutdown : `RestorePolicies()` appelé dans `shutdown()` et dans le `defer` emergency (lignes ~632-641, ~848-857)
  - [ ] 4.4 Vérifier que `SysProxy.RecoverOrphan()` est appelé au démarrage du tray (si le proxy était actif avant un crash)

- [ ] **Task 5 : Vérifier et adapter l'installeur NSIS** (AC: 1, 5)
  - [ ] 5.1 Vérifier la section Install de `installer/levoile.nsi` : le service est démarré après installation → `ApplyPolicies()` se déclenche automatiquement → extension déployée
  - [ ] 5.2 Vérifier la section Uninstall :
    - Fermeture des navigateurs (nécessaire pour débloquer les XPI)
    - Suppression des XPI Firefox dans `%APPDATA%\Mozilla\Firefox\Profiles\*\extensions\`
    - Suppression des clés registre `ExtensionSettings` (Chrome, Edge, Firefox)
    - Suppression du dossier `C:\ProgramData\LeVoile\extension\`
    - Restauration du proxy WinINET (`ProxyEnable=0`, suppression `ProxyServer`/`ProxyOverride`)
    - Broadcast `WM_SETTINGCHANGE` via `rundll32 wininet.dll,InternetSetOptionW 39`

- [ ] **Task 6 : Tests** (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 6.1 Vérifier les tests unitaires `internal/browser/manager_windows_test.go` : `applyExtensionChromium()`, `applyExtensionFirefox()`, merge avec JSON pré-existant, merge avec JSON corrompu (guard E4), restauration
  - [ ] 6.2 Vérifier les tests `internal/browser/manager_test.go` : persistance d'état extension dans `policyPersistedState`
  - [ ] 6.3 Vérifier les tests `internal/browser/e2e_windows_test.go` : cycle complet Apply → verify → Restore
  - [ ] 6.4 Vérifier les tests `internal/tray/sysproxy_windows_test.go` : Save/Set/Restore, orphan recovery
  - [ ] 6.5 Test manuel Chrome : extension force-installée visible dans `chrome://extensions`, routage proxy actif, `chrome://policy` montre l'extension sans `[BLOCKED]`
  - [ ] 6.6 Test manuel Firefox : extension installée automatiquement, routage `proxy.onRequest` actif
  - [ ] 6.7 Test manuel cohabitation : navigateur utilise l'extension (vérifier via `chrome://net-internals`), app hors navigateur (curl) utilise le SysProxy WinINET, les deux montrent l'IP du relais
  - [ ] 6.8 Test shutdown : Quitter via tray → vérifier que les politiques registre sont supprimées et le proxy WinINET restauré
  - [ ] 6.9 Faire passer `go test ./internal/browser/... ./internal/tray/... ./internal/service/...` sans régression

## Dev Notes

### Architecture Story 11.3 : Installation Automatique & Cohabitation SysProxy

Cette story vérifie et adapte l'installation automatique de l'extension navigateur et la cohabitation SysProxy pour l'architecture révisée (webview/webview + fyne.io/systray, 2 processus : service + UI/tray).

**Séparation des responsabilités (architecture 2 processus) :**

| Composant | Responsable | Processus |
|---|---|---|
| Browser policies (WebRTC + Extension) | `internal/browser/` | **Service** (`levoile-service.exe`) |
| SysProxy WinINET | `internal/tray/sysproxy_windows.go` | **UI/Tray** (`levoile-ui.exe`) |
| HTTP proxy CONNECT local | `internal/httpproxy/` | **Service** |
| Extension source (background.js) | `extension/` | **Navigateur** |

**Flux de démarrage :**
```
[Service démarre (SCM)]
  → RecoverOrphanPolicies() (crash recovery)
  → startHTTPProxy() → écoute 127.0.0.1:50113
  → ApplyPolicies() → deployExtensionFiles() + écriture registre ExtensionSettings + WebRTC

[UI/Tray démarre (autostart HKCU)]
  → SysProxy.RecoverOrphan() (crash recovery)
  → poll IPC status → détecte HTTPProxyActive=true
  → syncSysProxy(true, "127.0.0.1:50113") → Save() + Set()

[Navigateur détecte la politique]
  → Chrome/Firefox charge l'extension force-installed
  → background.js configure le routage proxy (PAC/onRequest)
  → health check 5s → proxy actif → routing via 127.0.0.1:50113
```

**Flux de shutdown (Quitter via tray) :**
```
[Tray: handleQuit()]
  → shutdownServiceAndRestore()
    → syncSysProxy(false, "") → Restore() WinINET
    → IPC ActionQuit au service (timeout 10s)
  → menuAPI.Quit()

[Service: shutdown()]
  → stopHTTPProxy() (drain 5s)
  → RestorePolicies() → restaure registre + cleanupExtensionFiles()
  → restoreDNS()
```

### Fichiers existants à vérifier/adapter

```
VÉRIFIER (déjà implémentés, adapter si nécessaire) :
internal/browser/extension_embed.go      # //go:embed CRX/XPI, deploy, updates.xml, ID extraction
internal/browser/manager_windows.go      # ApplyPolicies, applyExtensionChromium/Firefox, merge, restore
internal/browser/manager.go              # PolicyManager interface, policyPersistedState, orphan recovery
internal/browser/detect_windows.go       # DetectBrowsers (Chrome, Edge, Brave, Vivaldi, Opera, Firefox)
internal/browser/detect.go               # Constants, policy paths, BrowserFamily
internal/browser/lock_windows.go         # Advisory file lock C:\ProgramData\LeVoile\browser-policies.lock
internal/tray/sysproxy_windows.go        # SysProxy: Save/Set/Restore, DPAPI, ProxyOverride, orphan recovery
internal/tray/tray.go                    # syncSysProxy(), shutdownServiceAndRestore()
internal/service/service.go              # ApplyPolicies au start, RestorePolicies au shutdown, RecoverOrphan
installer/levoile.nsi                    # Install: service start → policies auto. Uninstall: cleanup complet

INCHANGÉ (ne pas toucher) :
internal/httpproxy/                      # Proxy CONNECT local — indépendant
extension/background.js                  # Routage proxy — déjà implémenté story 11.1 + 11.2
extension/manifest.json                  # Chrome MV3 — déjà correct
extension/manifest_firefox.json          # Firefox MV2 — déjà correct
```

### Chemins de déploiement extension

| Artefact | Chemin Windows |
|---|---|
| CRX Chrome pré-signé | `C:\ProgramData\LeVoile\extension\chrome\levoile.crx` |
| updates.xml Chrome | `C:\ProgramData\LeVoile\extension\chrome\updates.xml` |
| XPI Firefox signé AMO | `C:\ProgramData\LeVoile\extension\levoile.xpi` |
| État persisté policies | `C:\ProgramData\LeVoile\browser-policies-original.json` |
| Lock file policies | `C:\ProgramData\LeVoile\browser-policies.lock` |
| État persisté SysProxy | `%AppData%\LeVoile\proxy-original.json` (DPAPI) |

### Politiques registre extension

**Chromium (Chrome, Edge, Brave, Vivaldi, Opera) :**
```
HKLM\SOFTWARE\Policies\{vendor}\ExtensionSettings (REG_SZ)
→ JSON: {"<extension_id>": {"installation_mode": "force_installed", "update_url": "file:///C:/ProgramData/LeVoile/extension/chrome/updates.xml"}}
```

**Firefox :**
```
HKLM\SOFTWARE\Policies\Mozilla\Firefox\ExtensionSettings (REG_SZ)
→ JSON: {"levoile@plateformeliberte.fr": {"installation_mode": "force_installed", "install_url": "file:///C:/ProgramData/LeVoile/extension/levoile.xpi"}}
```

**Extension ID Chrome :** Dérivé de la clé publique du CRX (SHA256 → hex a-p). Fixe pour une même clé PEM de build.
**Extension ID Firefox :** `levoile@plateformeliberte.fr` (gecko ID dans manifest_firefox.json).

### Cohabitation SysProxy + Extension — mécanisme

L'extension navigateur et le SysProxy WinINET coexistent sans conflit :

1. **Extension** : installée via politique d'entreprise (force_installed). Les politiques d'entreprise ont priorité sur les paramètres proxy système dans les navigateurs. L'extension configure le routage via PAC (Chrome) ou `proxy.onRequest` (Firefox) vers `127.0.0.1:50113`.

2. **SysProxy WinINET** : configure `HKCU\...\Internet Settings\ProxyServer=127.0.0.1:50113`. Affecte les applications Windows qui respectent WinINET (Electron, curl, PowerShell, etc.) mais PAS les navigateurs qui ont une politique d'extension active.

3. **Même destination** : les deux routent vers le même proxy HTTP CONNECT local (`127.0.0.1:50113`). Pas de conflit de port ni de double-proxy.

4. **Bypass lists** : le SysProxy a un `ProxyOverride` exhaustif (loopback, OCSP/CRL, Windows Update, CDNs vidéo, relay domain). L'extension a son propre bypass (loopback dans le PAC, gros fichiers > 50 Mo via story 11.2).

### Risques connus (story 11-3 originale)

| Risque | Statut | Mitigation |
|---|---|---|
| E1: Chrome bloque CRX auto-signés sur postes non-managed | **Résolu** | `update_url` configurable via `ChromeStoreUpdateURL`. Publication CWS unlisted pour prod |
| E2: Firefox refuse XPI non-signés | **Résolu** | XPI signé AMO (self-distribution unlisted). Embarqué pré-signé dans le binaire |
| E4: Corruption JSON ExtensionSettings pré-existant | **Géré** | Guard dans `mergeExtensionSettings()` — si unmarshal échoue, ne pas écraser, remonter erreur |
| Politiques d'entreprise tierces | **Géré** | Merge JSON obligatoire — l'entrée Le Voile est ajoutée au dict existant, jamais écrasement |

### Intelligence Story 11.2 (précédente)

Leçons de l'implémentation 11.2 applicables à 11.3 :
- **Pas de `console.log`** — architecture zero-log, aucun logging dans l'extension
- **Un seul `background.js`** détecte Chrome vs Firefox au runtime — ne PAS créer de fichiers séparés
- **try/catch autour de `new URL()`** — toujours encapsuler les parsings d'URL
- **Code review a nettoyé le code mort** — ne pas laisser de variables/permissions inutilisées
- **Loopback exclusion critique** dans PAC et `proxy.onRequest` — ne pas casser
- **`webRequestBlocking` garanti** sur Chrome MV3 car extension policy-installed
- **Guard anti-boucle infinie** dans `onHeadersReceived` — `isAlreadyBypassed()` check
- **`saveAs: false`** dans `downloads.download()` pour UX transparente

### Intelligence Git (commits récents)

```
b10febd chore: add manifest v2 backup for browser extension
94abe13 fix(extension): auto-fallback to DIRECT when proxy is down
d5a6025 fix(installer): remove Firefox extension and browser policies on uninstall
1640da5 fix: desktop shortcuts, tray icon, clean shutdown, remove dead code
340b6f6 fix: adversarial code review fixes, installer proxy cleanup, and full shutdown
0561c38 feat(httpproxy): add per-domain volume bypass in local proxy
017255c feat(browser): force-install extension via enterprise policies with embedded CRX/XPI
```

Patterns observés :
- Le commit `d5a6025` a corrigé le nettoyage Firefox dans l'installeur — vérifier que ces corrections sont toujours présentes
- Le commit `94abe13` a ajouté le fallback DIRECT quand le proxy est down — déjà en place dans background.js
- Le commit `017255c` est l'implémentation originale des browser policies — base de travail existante

### Project Structure Notes

- `internal/browser/` est le package central de cette story (16 fichiers Go)
- `internal/tray/sysproxy_windows.go` gère le SysProxy WinINET (propriété du processus tray)
- `internal/service/service.go` orchestre les browser policies (propriété du processus service)
- La séparation service/tray est critique : le service gère les politiques registre (admin), le tray gère le proxy WinINET (user-level HKCU)
- Les assets CRX/XPI pré-signés sont dans `internal/browser/extension_assets/build/` (embarqués via `//go:embed`)
- La clé PEM de build est dans `extension/levoile.pem` (gitignored)

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 11, Story 11.3, AC en BDD]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Extension Navigateur (FR37-40)", cohabitation SysProxy + extension, déploiement CRX/XPI, politiques navigateur]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — Arbre source `extension/`, `internal/browser/`, `internal/tray/sysproxy_windows.go`]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — Séquence d'implémentation: 12=browser policies, 17=UI sysproxy]
- [Source: `_bmad-output/planning-artifacts/ux-design-specification.md` — "Zero-config absolu : aucun écran de configuration à l'installation"]
- [Source: `_bmad-output/implementation-artifacts/11-2-bypass-intelligent-des-telechargements-volumineux.md` — Intelligence story précédente: zero-log, fichier unique background.js, guard anti-boucle, loopback exclusion]
- [Source: `internal/browser/extension_embed.go` — deployExtensionFiles(), extractExtensionIDFromCRX(), updates.xml template, commentaire cohabitation SysProxy]
- [Source: `internal/browser/manager_windows.go` — ApplyPolicies(), applyExtensionChromium/Firefox(), mergeExtensionSettings(), RestorePolicies()]
- [Source: `internal/tray/sysproxy_windows.go` — SysProxy.Save/Set/Restore(), DPAPI, ProxyOverride bypass list]
- [Source: `internal/tray/tray.go` — syncSysProxy(), shutdownServiceAndRestore(), handleQuit()]
- [Source: `internal/service/service.go` — lignes 601-612 ApplyPolicies, 632-641 emergency restore, 848-857 shutdown restore]
- [Source: `installer/levoile.nsi` — sections Install (service start → auto policies) et Uninstall (cleanup complet)]
- [ADR: Extension toujours policy-installed (dev+prod) → webRequestBlocking garanti Chrome+Firefox]
- [ADR: SysProxy propriété du tray (HKCU user-level), browser policies propriété du service (HKLM admin-level)]
- [ADR: Merge JSON obligatoire pour ExtensionSettings — ne jamais écraser les politiques d'entreprise tierces]

### Couverture AC

| AC | Couvert par |
|----|------------|
| AC1 (installation auto extension) | Task 1 (deploy fichiers), Task 2 (politiques registre), Task 4 (orchestration service) |
| AC2 (cohabitation SysProxy + extension) | Task 3 (vérification flux tray + même port + bypass lists) |
| AC3 (Chrome MV3 policy-installed) | Task 2.1 (ExtensionSettings Chromium), Task 1.3 (ID CRX) |
| AC4 (Firefox policy-installed) | Task 2.2 (ExtensionSettings Firefox), Task 1.2 (XPI signé AMO) |
| AC5 (restauration propre) | Task 2.4-2.5 (RestorePolicies), Task 3.3 (SysProxy.Restore), Task 5.2 (uninstaller) |
| AC6 (orphan recovery) | Task 4.1 (RecoverOrphanPolicies), Task 4.4 (SysProxy.RecoverOrphan) |

### Couverture FRs

| FR | Couvert par AC |
|----|---------------|
| FR39 (installation auto extension via politiques navigateur) | AC1, AC3, AC4 |
| FR40 (cohabitation SysProxy + extension) | AC2, AC5 |

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
