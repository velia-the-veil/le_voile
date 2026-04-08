# Story 11.1: Extension WebExtension — Routage par Défaut via Proxy Le Voile

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux que tout mon trafic navigateur soit automatiquement routé via Le Voile sans aucune action de ma part,
Afin que mon IP soit masquée pour toute navigation.

## Acceptance Criteria

**AC1 — Routage par défaut Chrome MV3**
**Given** l'extension est installée dans Chrome (Manifest V3)
**When** l'utilisateur navigue sur n'importe quel site
**Then** le trafic est routé via le proxy Le Voile (127.0.0.1:50113) via `chrome.proxy.settings.set()` (PAC script inline)
**And** aucune UI/popup n'est affichée dans l'extension

**AC2 — Routage par défaut Firefox MV2**
**Given** l'extension est installée dans Firefox
**When** l'utilisateur navigue sur n'importe quel site
**Then** le trafic est routé via le proxy Le Voile (127.0.0.1:50113) via `browser.proxy.onRequest`
**And** aucune UI/popup n'est affichée dans l'extension

**AC3 — Configuration automatique au chargement**
**Given** le proxy Le Voile est actif (service connecté)
**When** l'extension est chargée (service worker Chrome / event page Firefox)
**Then** l'extension configure automatiquement le routage proxy
**And** tout le trafic HTTP/HTTPS passe par le proxy local

**AC4 — Fallback gracieux quand proxy inactif**
**Given** le proxy Le Voile est inactif (service déconnecté)
**When** l'extension détecte l'absence du proxy (health check échoue)
**Then** le trafic passe en connexion directe (fallback gracieux)
**And** aucune erreur n'est affichée à l'utilisateur
**And** quand le proxy redevient disponible, le routage reprend automatiquement

## Tasks / Subtasks

- [ ] **Task 1 : Vérifier et mettre à jour les manifests** (AC: 1, 2, 3)
  - [ ] 1.1 Vérifier `extension/manifest.json` (Chrome MV3) — permissions `proxy`, `webRequest`, `downloads`, host_permissions `<all_urls>`, background service_worker
  - [ ] 1.2 Vérifier `extension/manifest_firefox.json` (Firefox MV2) — permissions `proxy`, `webRequest`, `webRequestBlocking`, `downloads`, `<all_urls>`, gecko ID `levoile@plateformeliberte.fr`, strict_min_version `142.0`
  - [ ] 1.3 Supprimer `extension/manifest_chrome.json` si c'est un doublon de `manifest.json` (éviter la confusion)
  - [ ] 1.4 Vérifier que les icônes dans `extension/icons/` (16, 48, 128px) sont présentes et valides

- [ ] **Task 2 : Vérifier le routage proxy dans background.js** (AC: 1, 2, 3, 4)
  - [ ] 2.1 Vérifier le routage Chrome via PAC script dynamique (`generatePacScript()` → `chrome.proxy.settings.set()`) :
    - PAC inclut exclusion loopback (`127.0.0.1`, `localhost`, `::1`)
    - PAC inclut fallback `; DIRECT` natif
    - PAC inclut bypass set dynamique pour les gros fichiers (scope 11.2 mais déjà implémenté)
  - [ ] 2.2 Vérifier le routage Firefox via `browser.proxy.onRequest` :
    - Exclusion loopback avec try/catch sur `new URL()`
    - `failoverTimeout: 3` pour fallback natif
    - `proxy.onError` listener pour détecter proxy down
    - Bypass set check pour les URLs marquées
  - [ ] 2.3 Vérifier le health check proxy périodique (5s) :
    - `fetch()` vers `http://127.0.0.1:50113/` en mode `no-cors`
    - Si success → `proxyAlive = true`
    - Si erreur → `proxyAlive = false` + mise à jour PAC Chrome
    - Auto-recovery quand le proxy revient
  - [ ] 2.4 Vérifier le point d'entrée : détection `isFirefox` → appel `setupFirefoxProxy()` ou `setupChromeProxy()` + `setupBypassDetection()`

- [ ] **Task 3 : Synchroniser extension/ ↔ internal/browser/extension_assets/src/** (AC: 1, 2)
  - [ ] 3.1 Vérifier que `extension/background.js` et `internal/browser/extension_assets/src/background.js` sont identiques
  - [ ] 3.2 Vérifier que les manifests source dans `extension_assets/src/` matchent ceux de `extension/`
  - [ ] 3.3 Vérifier que `go:generate` dans `extension_embed.go` synchronise correctement les assets (`-sync-assets` flag)

- [ ] **Task 4 : Vérifier la compatibilité avec l'architecture 2 processus** (AC: 3, 4)
  - [ ] 4.1 Confirmer que l'extension ne dépend que du proxy HTTP CONNECT sur 127.0.0.1:50113 (aucune dépendance directe sur le service ou l'UI)
  - [ ] 4.2 Confirmer que le proxy est démarré par le service (`internal/httpproxy/server.go`) indépendamment de l'UI
  - [ ] 4.3 Tester le scénario : service actif + UI fermée → extension doit continuer à router via proxy
  - [ ] 4.4 Tester le scénario : service arrêté → extension doit fallback DIRECT sans erreur visible

- [ ] **Task 5 : Tests manuels** (AC: 1-4)
  - [ ] 5.1 Chrome : charger extension non empaquetée (`chrome://extensions` → `extension/`). Naviguer sur whatismyip.com → IP du relais affichée
  - [ ] 5.2 Chrome : vérifier `chrome://settings` → "L'extension Le Voile contrôle ce paramètre" pour le proxy
  - [ ] 5.3 Firefox : copier `manifest_firefox.json` → `manifest.json`, charger via `about:debugging`. Vérifier routage
  - [ ] 5.4 Arrêter le service → vérifier navigation continue en direct (fallback). Redémarrer → routage reprend automatiquement
  - [ ] 5.5 Vérifier qu'aucune UI/popup n'apparaît dans l'extension

## Dev Notes

### Contexte critique : code existant à vérifier, pas à réécrire

L'extension est **déjà entièrement implémentée** dans le codebase. Cette story est une **vérification et mise en conformité** avec l'architecture révisée (2026-04-08 : Wails v2 → webview/webview + fyne.io/systray, 2 processus). Le changement d'architecture n'affecte PAS directement le code extension — elle communique uniquement avec le proxy HTTP CONNECT sur `127.0.0.1:50113` qui est démarré par le service, pas par l'UI.

**NE PAS réécrire le code existant sauf si un bug ou une incompatibilité est détecté.**

### Fichiers existants à vérifier (NE PAS recréer)

```
EXISTANT — VÉRIFIER :
extension/background.js              # Script unifié Chrome/Firefox (182 lignes)
extension/manifest.json              # Chrome MV3 manifest
extension/manifest_firefox.json      # Firefox MV2 manifest
extension/manifest_chrome.json       # Doublon ? À clarifier/supprimer
extension/manifest_bak.json          # Backup — peut être supprimé si redondant
extension/levoile.pem                # Clé RSA privée pour signature CRX
extension/icons/icon-{16,48,128}.png # Icônes extension
extension/.amo-upload-uuid           # Tracking upload Firefox AMO

internal/browser/extension_assets/src/manifest.json          # Copie embarquée Chrome
internal/browser/extension_assets/src/manifest_firefox.json   # Copie embarquée Firefox
internal/browser/extension_assets/src/manifest_bak.json       # Backup embarqué
internal/browser/extension_assets/src/background.js           # Copie embarquée du script
internal/browser/extension_assets/src/icons/                  # Copie embarquée des icônes
internal/browser/extension_assets/build/levoile.crx           # CRX pré-signé
internal/browser/extension_assets/build/levoile.xpi           # XPI pré-signé (AMO)

NE PAS MODIFIER :
internal/httpproxy/server.go         # Proxy CONNECT existant sur 127.0.0.1:50113
internal/browser/extension_embed.go  # Embedding CRX/XPI + dérivation Extension ID
internal/browser/manager.go          # PolicyManager (scope story 11.3)
internal/browser/manager_windows.go  # Policies registre Windows (scope story 11.3)
```

### Différence API Chrome vs Firefox

| Aspect | Chrome MV3 | Firefox MV2 |
|--------|------------|-------------|
| API proxy | `chrome.proxy.settings.set()` (PAC inline dynamique) | `browser.proxy.onRequest` (listener per-request) |
| Background | Service worker (idle 5min, réactivé par événements) | Script persistant |
| Fallback direct | `; DIRECT` dans la chaîne PAC + health check → PAC full DIRECT | `failoverTimeout: 3` + `proxy.onError` + health check |
| `webRequestBlocking` | Policy-installed uniquement (MV3) | Disponible nativement (MV2) |
| Bypass gros fichiers | PAC dynamique avec bypass set + `chrome.downloads.download()` | `bypassUrls.has(details.url)` → `{ type: 'direct' }` |
| Permissions | `proxy`, `webRequest`, `downloads` + host `<all_urls>` | `proxy`, `webRequest`, `webRequestBlocking`, `downloads`, `<all_urls>` |

### Health check proxy — mécanisme de fallback actif

Le code implémente un health check au-delà du fallback natif PAC/failoverTimeout :
- `fetch('http://127.0.0.1:50113/', { mode: 'no-cors' })` toutes les 5 secondes
- Si échec → `proxyAlive = false` → Chrome : regénère PAC avec `return 'DIRECT'` pour tout / Firefox : `proxy.onRequest` retourne `{ type: 'direct' }`
- Si succès après échec → `proxyAlive = true` → routage reprend automatiquement
- Ce mécanisme est plus réactif que le failover natif (5s max vs timeout TCP)

### Cohabitation SysProxy + Extension (contexte pour 11.3)

L'extension a priorité dans les navigateurs (enterprise policy > SysProxy). Le SysProxy (`internal/ui/sysproxy_windows.go` → WinINET `ProxyServer` registre) reste actif pour les apps hors navigateur (Electron, curl, Windows Store apps). Les deux routent vers le même proxy `127.0.0.1:50113` — pas de conflit.

### Note : bypass gros fichiers déjà implémenté

Le code `background.js` actuel inclut déjà la logique de bypass > 50 Mo (scope story 11.2) :
- `webRequest.onHeadersReceived` vérifie `Content-Length > 52428800`
- Ajoute l'URL/hostname au bypass set (TTL 120s)
- Cancel la requête + re-download via `downloads.download()` en direct
- Chrome : regénère le PAC avec les exclusions / Firefox : check `bypassUrls` dans `proxy.onRequest`

Cette logique existe dans le code mais sera formellement validée en story 11.2. Ne pas la supprimer.

### Pipeline de build extension

```
extension/                          # Sources de développement (chargement manuel)
    ↓ go:generate (extension_embed.go)
internal/browser/extension_assets/src/   # Copie synchronisée des sources
    ↓ tools/crxgen (clé RSA levoile.pem)
internal/browser/extension_assets/build/levoile.crx  # Chrome CRX signé
    ↓ AMO upload manuel (levoile@plateformeliberte.fr)
internal/browser/extension_assets/build/levoile.xpi  # Firefox XPI signé
    ↓ //go:embed all:extension_assets
internal/browser/extension_embed.go      # Embarqué dans le binaire Go
    ↓ deployExtensionFiles()
%ProgramData%/LeVoile/extension/         # Déployé sur le système au runtime
```

### Flux de données extension → proxy → relais

```
[Chrome/Firefox avec extension Le Voile]
    ↓ (toute requête HTTP/HTTPS)
[background.js → proxyAlive? → bypass set?]
    ↓ (si proxy alive et pas bypass)
[PROXY 127.0.0.1:50113]
    ↓ (HTTP CONNECT)
[internal/httpproxy/connect_handler.go]
    ↓ (volume check → bypass si domaine > 500 Mo)
[internal/tunnel/client.go → POST /connect + session token Ed25519]
    ↓ (QUIC/HTTP3 via Cloudflare CDN)
[Relais VPS du pays sélectionné]
    ↓ (relay bidirectionnel TCP)
[Serveur destination]
```

### Project Structure Notes

- Le dossier `extension/` est à la racine du projet, au même niveau que `frontend/`, `cmd/`, `internal/`
- L'extension est 100% statique (JS/JSON/PNG) — pas de build step, pas de bundler, pas de transpilation
- Le fichier `background.js` est unique et détecte Chrome vs Firefox au runtime via `typeof browser`
- Deux manifests séparés obligatoires (Chrome MV3 ≠ Firefox MV2 : service_worker vs scripts, permissions)
- La copie embarquée dans `internal/browser/extension_assets/` est synchronisée via `go:generate`

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 11, Story 11.1, AC en BDD, FR37]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Extension Navigateur Patterns", flux routage intelligent, structure `extension/`]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "FR37-40 (Extension Navigateur) → extension/, internal/browser/"]
- [Source: `_bmad-output/planning-artifacts/ux-design-specification.md` — "Extension navigateur — installée automatiquement, aucune action utilisateur"]
- [Source: `extension/background.js` — Script unifié Chrome/Firefox, PAC dynamique, proxy.onRequest, health check 5s, bypass detection]
- [Source: `extension/manifest.json` — Chrome MV3, permissions proxy+webRequest+downloads, service_worker]
- [Source: `extension/manifest_firefox.json` — Firefox MV2, gecko ID levoile@plateformeliberte.fr, strict_min_version 142.0]
- [Source: `internal/httpproxy/server.go` — Proxy CONNECT existant sur 127.0.0.1:50113]
- [Source: `internal/browser/extension_embed.go` — go:generate crxgen, //go:embed, CRX ID derivation, deployExtensionFiles()]
- [Source: `internal/browser/manager_windows.go` — ExtensionSettings registry policies (scope 11.3)]

### Couverture AC → Tasks

| AC | Couvert par |
|----|-------------|
| AC1 (routage Chrome MV3) | Task 1.1, Task 2.1, Task 3.2, Task 5.1-5.2 |
| AC2 (routage Firefox MV2) | Task 1.2, Task 2.2, Task 3.2, Task 5.3 |
| AC3 (config auto au chargement) | Task 2.4, Task 4.1-4.2 |
| AC4 (fallback gracieux + auto-recovery) | Task 2.3, Task 4.3-4.4, Task 5.4 |

### Couverture FRs

| FR | Couvert par AC |
|----|---------------|
| FR37 (extension route par défaut via proxy Le Voile 127.0.0.1:50113) | AC1, AC2, AC3, AC4 |

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
