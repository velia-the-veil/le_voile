# Story 10.1 : Initialisation Wails v2 et Fenêtre Desktop avec Statut de Connexion

Status: deprecated — remplacé par 10-1-binaire-ui-unique-systray-webview-serveur-http-local-avec-statut-de-connexion.md (architecture webview/webview + fyne.io/systray)

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux voir une fenêtre desktop affichant mon statut de protection (connecté/en cours/déconnecté) avec un indicateur visuel coloré,
Afin de savoir immédiatement si je suis protégé.

## Acceptance Criteria

**AC1 — Indicateur visuel de statut en temps réel**
**Given** le service Le Voile est démarré
**When** la fenêtre desktop Wails s'ouvre
**Then** l'indicateur de statut affiche l'état actuel :
- Vert (`#4ade80`) = connecté
- Orange (`#fb923c`) = en cours / reconnexion
- Rouge (`#ff3c3c`) = déconnecté

**AC2 — Charte visuelle plateformeliberte.fr**
**Given** la fenêtre desktop est ouverte
**When** l'interface est rendue
**Then** elle reproduit la charte plateformeliberte.fr :
- Fond sombre `#0b1526`
- Bleus accent `#1a6fc4` / `#2a8dff`
- Polices Bebas Neue (titres), Rajdhani (UI), Inter (corps)
- Fenêtre 400×500px, non-redimensionnable, frameless

**AC3 — Mise à jour temps réel du statut**
**Given** le tunnel change d'état (connexion, perte, reconnexion)
**When** l'état du tunnel est mis à jour
**Then** l'indicateur visuel change de couleur en temps réel

**AC4 — Messages français non-techniques**
**Given** la fenêtre affiche un statut
**When** l'état change
**Then** un message français non-technique est affiché :
- "Connecté — Islande" (avec pays du relais actif)
- "Reconnexion en cours..."
- "Déconnecté"

**AC5 — Initialisation Wails v2 dans le monorepo Go**
**Given** le projet existant (monorepo Go, `cmd/`, `internal/`)
**When** Wails v2 est initialisé
**Then** le dossier `frontend/` est créé avec HTML/CSS/JS vanilla (pas de framework)

**AC6 — Bindings Wails Go → JS**
**Given** le frontend Wails est initialisé
**When** les bindings sont configurés
**Then** le frontend peut appeler les fonctions Go existantes via IPC pour obtenir le statut (`GetStatus()`)

**AC7 — Design tokens CSS extraits de plateformeliberte.fr**
**Given** le fichier CSS est créé
**When** les variables CSS sont définies
**Then** les design tokens suivants sont présents et utilisés :
```css
--bg-primary: #0b1526;
--bg-secondary: #0e1e38;
--accent-blue: #1a6fc4;
--accent-glow: #2a8dff;
--accent-red: #d42b2b;
--status-secure: #4ade80;
--status-warning: #fb923c;
--status-risk: #ff3c3c;
--text-primary: #f0f4ff;
--text-secondary: #8a9bb8;
```

## Tasks / Subtasks

- [x] **Task 1 : Initialisation Wails v2 dans le monorepo** (AC: 5)
  - [x] 1.1 Ajouter la dépendance `github.com/wailsapp/wails/v2` dans `go.mod` (v2.11.0+)
  - [x] 1.2 Créer `frontend/` avec structure :
    ```
    frontend/
    ├── index.html          # Point d'entrée
    ├── src/
    │   ├── style.css       # Charte plateformeliberte.fr + design tokens
    │   └── app.js          # Logique UI : statut, polling, bindings
    └── assets/
        └── fonts/          # Bebas Neue, Rajdhani, Inter (fichiers .woff2)
    ```
  - [x] 1.3 Créer `frontend/embed.go` avec `//go:embed` pour l'FS embarqué :
    ```go
    package frontend
    import "embed"
    //go:embed all:index.html all:src all:assets
    var Assets embed.FS
    ```

- [x] **Task 2 : Struct App Wails et bindings Go** (AC: 6)
  - [x] 2.1 Créer `internal/desktop/app.go` — struct `App` :
    ```go
    type App struct {
        ctx       context.Context
        ipcClient *ipc.Client
    }
    ```
  - [x] 2.2 Méthode `startup(ctx context.Context)` — callback Wails `OnStartup`, connecte le client IPC au service
  - [x] 2.3 Méthode `GetStatus() StatusResponse` — appelle `ipc.ActionGetStatus` et retourne le résultat formaté :
    ```go
    type StatusResponse struct {
        Status    string `json:"status"`    // "connected", "connecting", "disconnected"
        IP        string `json:"ip"`        // IP visible ou ""
        Country   string `json:"country"`   // Pays du relais ou ""
        RelayID   string `json:"relay_id"`  // ID relais actif ou ""
        Uptime    string `json:"uptime"`    // Durée formatée ou ""
        Message   string `json:"message"`   // Message FR non-technique
    }
    ```
  - [x] 2.4 Méthode `shutdown(ctx context.Context)` — callback Wails `OnShutdown`, ferme le client IPC

- [x] **Task 3 : Point d'entrée Wails `cmd/desktop/main.go`** (AC: 5, 6)
  - [x] 3.1 Créer `cmd/desktop/main.go` — nouveau binaire desktop :
    ```go
    err := wails.Run(&options.App{
        Title:            "Le Voile",
        Width:            400,
        Height:           500,
        MinWidth:         400,
        MinHeight:        500,
        MaxWidth:         400,
        MaxHeight:        500,
        DisableResize:    true,
        Frameless:        true,
        StartHidden:      false,
        CSSDragProperty:  "--wails-draggable",
        CSSDragValue:     "drag",
        BackgroundColour: &options.RGBA{R: 11, G: 21, B: 38, A: 255}, // #0b1526
        AssetServer:      &assetserver.Options{Assets: frontend.Assets},
        OnStartup:        app.startup,
        OnShutdown:       app.shutdown,
        Bind:             []interface{}{app},
    })
    ```
  - [x] 3.2 Charger la config via `config.DiscoverPath("")` (même logique que `cmd/tray/`)
  - [x] 3.3 Créer l'instance `desktop.App` avec le client IPC configuré

- [x] **Task 4 : Frontend HTML — `frontend/index.html`** (AC: 2, 4)
  - [x] 4.1 Structure HTML sémantique :
    ```html
    <div class="titlebar" style="--wails-draggable:drag">
      <div class="titlebar-logo"><!-- V logo SVG, couleur = statut --></div>
      <span class="titlebar-text">Le Voile</span>
      <div class="titlebar-controls" style="--wails-draggable:no-drag">
        <button class="btn-minimize" onclick="minimizeWindow()">─</button>
        <button class="btn-close" onclick="closeWindow()">✕</button>
      </div>
    </div>
    <main>
      <div class="status-panel">
        <div class="status-dot"></div>
        <div class="status-text"></div>
        <div class="status-ip"></div>
        <div class="status-uptime"></div>
      </div>
    </main>
    ```
  - [x] 4.2 Charger les polices via `@font-face` depuis `assets/fonts/`
  - [x] 4.3 Inclure `src/app.js` en `type="module"` ou script classique

- [x] **Task 5 : CSS — Design tokens et charte visuelle** (AC: 2, 7)
  - [x] 5.1 Créer `frontend/src/style.css` avec toutes les variables CSS design tokens
  - [x] 5.2 Styles titlebar custom : `height: 32px`, fond `--bg-secondary`, `--wails-draggable:drag`
  - [x] 5.3 Styles panneau de statut : dot animé (glow pour connecté, pulse pour reconnexion), texte centré
  - [x] 5.4 États visuels :
    - `.connected` : dot vert + glow, texte "Connecté — {pays}"
    - `.connecting` : dot orange + pulse CSS, texte "Reconnexion en cours..."
    - `.disconnected` : dot rouge, texte "Déconnecté"
  - [x] 5.5 Responsive DPI : utiliser `rem`/`em`, pas de `px` fixes pour le texte

- [x] **Task 6 : JavaScript — Polling et mise à jour UI** (AC: 1, 3, 4)
  - [x] 6.1 Créer `frontend/src/app.js` — appelle `window.go.desktop.App.GetStatus()` toutes les 2 secondes
  - [x] 6.2 Fonction `updateUI(status)` — met à jour le DOM :
    - Couleur du dot de statut selon `status.status`
    - Texte du message selon `status.message`
    - IP visible si connecté
    - Durée uptime si connecté
    - Couleur du logo V dans la titlebar
  - [x] 6.3 Fonctions titlebar :
    - `minimizeWindow()` → `window.runtime.WindowMinimise()` (masque en tray)
    - `closeWindow()` → `window.runtime.WindowHide()` (ne détruit PAS la fenêtre)
  - [x] 6.4 Gestion état initial : appeler `GetStatus()` immédiatement au chargement

- [x] **Task 7 : Mapping pays depuis relais** (AC: 4)
  - [x] 7.1 Dans `internal/desktop/app.go`, mapper l'ID du relais vers le nom de pays en français :
    ```go
    var relayCountryMap = map[string]string{
        "relay-iceland":  "Islande",
        "relay-finland":  "Finlande",
        "relay-germany":  "Allemagne",
        "relay-france":   "France",
        "relay-usa":      "États-Unis",
    }
    ```
  - [x] 7.2 Si le relais n'est pas dans la map → utiliser l'ID du relais comme fallback
  - [x] 7.3 Construire le message français : `"Connecté — " + countryName`

- [x] **Task 8 : Tests** (AC: 1-7)
  - [x] 8.1 `internal/desktop/app_test.go` :
    - `TestGetStatus_Connected` — mock IPC retourne StatusConnected → StatusResponse correcte avec message FR
    - `TestGetStatus_Connecting` — mock IPC retourne StatusConnecting → message "Reconnexion en cours..."
    - `TestGetStatus_Disconnected` — mock IPC retourne StatusDisconnected → message "Déconnecté"
    - `TestGetStatus_IPCError` — IPC indisponible → retourne StatusDisconnected gracieusement
    - `TestRelayCountryMapping` — relay IDs connus → noms français corrects
  - [x] 8.2 Validation build : `go build ./cmd/desktop/...` — compilation OK
  - [ ] 8.3 Vérification manuelle : lancer `wails dev` et confirmer que la fenêtre s'affiche avec le statut

- **Review Follow-ups (AI)**
  - [x] [AI-Review][HIGH] Reconnexion IPC après erreur pipe — ajout Close()+Connect() dans GetStatus() [internal/desktop/app.go:71-74]
  - [x] [AI-Review][HIGH] Erreur wails.Run() ignorée — ajout os.Exit(1) [cmd/desktop/main.go:25-48]
  - [x] [AI-Review][MED] GetStatus() utilise context.Background() au lieu de a.ctx — corrigé [internal/desktop/app.go:67]
  - [x] [AI-Review][MED] wails.json absent — créé avec config minimale [wails.json]
  - [x] [AI-Review][MED] setInterval sans cleanup — ajout startPolling() avec clearInterval [frontend/src/app.js:14-23]
  - [x] [AI-Review][MED] Chemin binding Wails confirmé — `window.go.desktop.App` correct (vérifié via `wails build`)
  - [x] [AI-Review][MED] Polices .woff2 fournies par l'utilisateur — 5 fichiers placés dans frontend/assets/fonts/
  - [x] [AI-Review][LOW] var → const dans app.js [frontend/src/app.js:27]
  - [x] [AI-Review][LOW] RelayID renseigné même quand déconnecté — conditionné à connected/connecting [internal/desktop/app.go:87-89]

## Dev Notes

### Architecture de la Story 10.1 : Nouveau package `internal/desktop/` + frontend Wails

Cette story crée un **nouveau binaire** `cmd/desktop/` et un **nouveau package** `internal/desktop/` pour la fenêtre Wails. Le binaire desktop est **séparé** du tray et du service — il communique avec le service via IPC exactement comme le tray.

```
NOUVEAU :
cmd/desktop/main.go          # Point d'entrée Wails
internal/desktop/app.go      # Struct App, bindings Go, GetStatus()
internal/desktop/app_test.go # Tests
frontend/
├── embed.go                 # //go:embed FS
├── index.html               # UI
├── src/
│   ├── style.css            # Charte + design tokens
│   └── app.js               # Logique UI
└── assets/
    └── fonts/               # Bebas Neue, Rajdhani, Inter (.woff2)

EXISTANT (NON MODIFIÉ) :
cmd/tray/main.go             # Tray reste inchangé
cmd/client/main.go           # Service reste inchangé
internal/tray/               # Tray package inchangé
internal/ipc/                # IPC inchangé — réutilisé par desktop
internal/ipchandler/         # Handler inchangé
internal/service/            # Service inchangé
internal/config/             # Config inchangée
```

### Communication Desktop ↔ Service

Le desktop utilise le **même protocole IPC** que le tray :

```
cmd/desktop/main.go
  └── internal/desktop/app.go
        └── ipc.Client.SendContext(ActionGetStatus)
              └── Named Pipe \\.\pipe\levoile (Windows)
                    └── internal/ipc/server.go
                          └── internal/ipchandler/handler.go
                                └── handleGetStatus()
```

Le desktop ne parle **jamais** directement au tunnel, DNS, ou autre composant. Tout passe par IPC. C'est le même pattern que `internal/tray/tray.go` ligne `connectAndPoll()`.

### Wails v2 — Configuration critique

**Dépendance Go :**
```
github.com/wailsapp/wails/v2 v2.11.0
```

**Fenêtre frameless avec titlebar custom :**
- `Frameless: true` dans les options Wails
- `CSSDragProperty: "--wails-draggable"`, `CSSDragValue: "drag"` pour le drag HTML
- Attribut `style="--wails-draggable:drag"` sur la div titlebar
- Attribut `style="--wails-draggable:no-drag"` sur les boutons minimiser/fermer
- Le runtime Wails injecte automatiquement `/wails/ipc.js` et `/wails/runtime.js` dans `index.html`

**Bindings auto-générés :**
- Wails génère automatiquement `frontend/wailsjs/go/desktop/App.js` à partir de la struct `App`
- Appel JS : `window.go.desktop.App.GetStatus()` retourne une Promise<StatusResponse>
- Le runtime Wails est disponible via `window.runtime` (WindowMinimise, WindowHide, etc.)

**ATTENTION :** Le package Go pour les bindings doit correspondre au chemin d'import. Si `App` est dans `internal/desktop`, les bindings seront sous `window.go.internal.desktop.App`. Pour simplifier, envisager un alias ou vérifier le chemin exact généré par Wails.

### Polices web — fichiers nécessaires

Télécharger en `.woff2` (format le plus compact) :
- **Bebas Neue** — Google Fonts, licence OFL (titres)
- **Rajdhani** — Google Fonts, licence OFL (UI/labels)
- **Inter** — Google Fonts, licence OFL (corps de texte)

Placer dans `frontend/assets/fonts/` et déclarer via `@font-face` dans `style.css`.

### Pattern de polling — identique au tray

Le tray poll le service toutes les 2 secondes via `ActionGetStatus`. Le desktop fait la même chose côté JS :

```javascript
// frontend/src/app.js
async function pollStatus() {
    try {
        const status = await window.go.desktop.App.GetStatus();
        updateUI(status);
    } catch (e) {
        updateUI({ status: "disconnected", message: "Déconnecté" });
    }
}
setInterval(pollStatus, 2000);
pollStatus(); // Appel immédiat au chargement
```

Le Go `GetStatus()` côté `internal/desktop/app.go` crée un nouveau client IPC à chaque appel (ou maintient une connexion persistante avec reconnexion). Le tray maintient une connexion persistante — le desktop devrait faire pareil pour l'efficacité.

### Fenêtre titlebar — boutons minimiser/fermer

**Minimiser (─)** → `window.runtime.WindowMinimise()` — masque la fenêtre, le service et le tray continuent.

**Fermer (✕)** → `window.runtime.WindowHide()` — masque la fenêtre sans la détruire. La protection reste active. PAS de `window.runtime.Quit()` car cela tuerait le processus desktop.

**IMPORTANT pour Story 10.3 :** Le tray devra pouvoir réafficher la fenêtre (`WindowShow()`) au clic gauche. Ce n'est PAS dans le scope de 10.1 — ne pas implémenter l'intégration tray ↔ Wails.

### Statut IPC — mapping vers messages français

Le service retourne des statuts anglais via IPC. Le desktop les traduit :

| IPC Status | Couleur | Message FR |
|-----------|---------|------------|
| `connected` | `#4ade80` (vert) | "Connecté — {Pays}" |
| `connecting` | `#fb923c` (orange) | "Reconnexion en cours..." |
| `disconnected` | `#ff3c3c` (rouge) | "Déconnecté" |
| `error` | `#ff3c3c` (rouge) | "Erreur — {détail}" |

### Ce qui n'est PAS dans le scope de 10.1

- **Sélecteur de pays** → Story 10.2
- **Bouton connect/disconnect** → Story 10.3
- **Intégration tray ↔ Wails** (clic gauche tray ouvre fenêtre) → Story 10.3
- **Sidebar navigation** → Story 10.2
- **Panneau paramètres** → Story 10.2 ou 10.3
- **Modal de quit** → Story 10.3
- **Notification de mise à jour** → hors scope Epic 10

### Conventions Go à respecter

- **Nommage fichiers** : `snake_case.go` — `app.go`, `app_test.go`
- **Package** : `desktop` (dans `internal/desktop/`)
- **Erreurs** : wrapping `fmt.Errorf("desktop: %w", err)`
- **Context** : `startup(ctx)` reçoit le contexte Wails, `GetStatus()` utilise un context avec timeout 5s
- **Tests** : table-driven, `testing` standard, mock IPC via interface
- **Aucun log côté client** — erreurs retournées ou gérées silencieusement
- **Aucun import circulaire** : `desktop` importe `ipc`, jamais `service`, `tray`, `tunnel`

### Bibliothèques

| Bibliothèque | Version | Usage |
|-------------|---------|-------|
| `github.com/wailsapp/wails/v2` | v2.11.0 | Framework desktop (fenêtre, bindings, runtime) |
| `github.com/wailsapp/wails/v2/pkg/options` | — | Options de configuration fenêtre |
| `github.com/wailsapp/wails/v2/pkg/runtime` | — | Contrôle fenêtre depuis Go (WindowHide, etc.) |
| `internal/ipc` | existant | Client IPC vers le service |
| `internal/config` | existant | Chargement config TOML |

**Aucune nouvelle dépendance externe** autre que Wails v2 lui-même.

### Leçons des stories précédentes (Epics 1-9)

- **Client IPC** : `ipc.NewClient()` + `client.Connect()` peut échouer si le service n'est pas démarré. Gérer gracieusement → afficher "Déconnecté" au lieu de crasher
- **Polling 2s** : le tray poll toutes les 2s — le desktop doit faire pareil pour la cohérence
- **Timeout IPC** : le client IPC a un timeout de 5s par défaut. Pour `GetStatus()` qui est rapide, c'est suffisant
- **Zero-log** : le projet suit une architecture zero-log côté client (commit `b640d2d`). Ne pas ajouter de `log.Println` ou `fmt.Println`
- **Shutdown propre** : le callback `OnShutdown` de Wails doit fermer le client IPC proprement
- **Pas de proxy system** : le desktop n'a PAS besoin de gérer le proxy Windows (c'est le tray qui s'en charge)

### Fichiers à créer

| Fichier | Description |
|---------|-------------|
| `cmd/desktop/main.go` | Point d'entrée Wails v2 |
| `internal/desktop/app.go` | Struct App, bindings, GetStatus(), mapping pays |
| `internal/desktop/app_test.go` | Tests unitaires |
| `frontend/embed.go` | `//go:embed` pour les assets |
| `frontend/index.html` | Structure HTML sémantique |
| `frontend/src/style.css` | Charte visuelle + design tokens |
| `frontend/src/app.js` | Polling, updateUI, titlebar controls |
| `frontend/assets/fonts/*.woff2` | Polices Bebas Neue, Rajdhani, Inter |

### Fichiers à NE PAS toucher

- `cmd/client/main.go` — service inchangé
- `cmd/tray/main.go` — tray inchangé (intégration dans Story 10.3)
- `cmd/portable/main.go` — portable inchangé
- `internal/tray/` — non concerné
- `internal/ipc/` — réutilisé tel quel
- `internal/ipchandler/` — non modifié
- `internal/service/` — non modifié
- `internal/config/` — non modifié
- `internal/tunnel/` — non concerné
- `internal/dns/` — non concerné

### Vérification couverture AC

| AC | Couvert par |
|----|------------|
| AC1 (indicateur visuel statut temps réel) | Task 5 (CSS dot animé), Task 6 (polling + updateUI) |
| AC2 (charte plateformeliberte.fr) | Task 4 (HTML), Task 5 (CSS design tokens) |
| AC3 (mise à jour temps réel) | Task 6 (polling 2s, updateUI) |
| AC4 (messages français) | Task 7 (mapping pays), Task 6 (updateUI avec messages) |
| AC5 (initialisation Wails monorepo) | Task 1 (frontend/), Task 3 (cmd/desktop/) |
| AC6 (bindings Go → JS) | Task 2 (App struct, GetStatus), Task 3 (wails.Run + Bind) |
| AC7 (design tokens CSS) | Task 5 (variables CSS) |

### Project Structure Notes

- Le binaire desktop est un **processus séparé** du tray et du service — 3 processus au total (service + tray + desktop)
- Pour Story 10.3, le tray et desktop devront être fusionnés ou coordonnés (le tray lance/affiche la fenêtre desktop)
- Le `frontend/` est à la racine du projet (pas dans `internal/`) car Wails nécessite un `embed.FS` accessible depuis `cmd/desktop/`
- Le package `frontend` exporte uniquement `Assets embed.FS` — pas de logique Go

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 10, Story 10.1, AC1-7]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "UI Framework Wails v2", "IPC Named Pipe", "Design Tokens", "Frontend Structure"]
- [Source: `_bmad-output/planning-artifacts/prd.md` — FR9, FR10, FR13, FR15, Journey Camille]
- [Source: `_bmad-output/planning-artifacts/ux-design-specification.md` — Design system, component catalog C1/C5/C9, status states, titlebar controls, visual specs]
- [Source: `internal/ipc/messages.go` — ActionGetStatus, Response struct]
- [Source: `internal/ipc/client.go` — NewClient(), Connect(), SendContext()]
- [Source: `internal/tray/tray.go` — connectAndPoll() pattern, polling 2s]
- [Source: `internal/config/discover.go` — DiscoverPath()]
- [Source: `go.mod` — dépendances actuelles]
- [Source: Wails v2 docs — Frameless Applications, Options, Frontend guide]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

- Pre-existing test failures in `internal/ipc` (pipe access denied) and `internal/service` (port 3478 conflict) — not related to this story.
- Task 8.3 (manual wails dev verification) left unchecked — requires Wails CLI installation.

### Completion Notes List

- Created new `internal/desktop` package with `App` struct, `GetStatus()` binding, and `StatusResponse` type
- `App` uses `IPCClient` interface for testability (same pattern as tray's `IPCClient`)
- `GetStatus()` queries service via IPC, maps status to French messages, maps relay domain to country name
- `countryFromDomain()` uses prefix matching on `relayCountryMap` with domain fallback
- Created `cmd/desktop/main.go` — Wails v2 entry point with 400x500 frameless window, `#0b1526` background
- Config loaded via `config.DiscoverPath("")` (same as tray), extracts `Relay.Domain` for country mapping
- Frontend: vanilla HTML/CSS/JS, no framework — `index.html`, `style.css`, `app.js`
- CSS design tokens match AC7 spec exactly, all sizes in `rem`/`em` for DPI scaling
- JS polls `GetStatus()` every 2s via Wails bindings, updates dot color/glow/pulse, text, IP, uptime, titlebar logo
- `minimizeWindow()` → `WindowMinimise()`, `closeWindow()` → `WindowHide()` (no process kill)
- 10 unit tests: connected/connecting/disconnected/error states, IPC failure, relay mapping (7 domains), startup/shutdown lifecycle
- `go build ./cmd/desktop/...` compiles successfully
- Zero new logs added (zero-log architecture preserved)
- No existing files modified (all changes are new files)

### Implementation Plan

Architecture follows story spec: new `cmd/desktop/` binary + `internal/desktop/` package + `frontend/` at root. Desktop communicates with service via IPC only (same as tray). IPCClient interface enables mock-based testing.

### File List

| Action | File |
|--------|------|
| Added | `cmd/desktop/main.go` |
| Added | `internal/desktop/app.go` |
| Added | `internal/desktop/app_test.go` |
| Added | `frontend/embed.go` |
| Added | `frontend/index.html` |
| Added | `frontend/src/style.css` |
| Added | `frontend/src/app.js` |
| Added | `frontend/assets/fonts/README` |
| Added | `wails.json` |
| Added | `main.go` (root — required by Wails CLI) |
| Added | `frontend/assets/fonts/*.woff2` (5 font files) |
| Added | `frontend/wailsjs/` (auto-generated bindings) |
| Modified | `go.mod` |
| Modified | `go.sum` |

### Change Log

- **2026-03-16**: Story 10.1 implementation — Wails v2 desktop window with real-time connection status indicator, French messages, design tokens from plateformeliberte.fr charter, IPC polling, and frameless titlebar with custom controls.
- **2026-03-16**: Code review fixes — IPC reconnection on pipe failure, wails.Run() error handling, Wails context propagation, wails.json config, JS interval cleanup, var→const, conditional RelayID. Root main.go added for Wails CLI. Font casing fix. All 9/9 findings resolved. `wails build` successful → `build/bin/le-voile-desktop.exe`.

## Senior Developer Review (AI)

**Review Date:** 2026-03-16
**Reviewer:** Claude Opus 4.6 (code-review workflow)
**Outcome:** Approved (all findings resolved)

### Findings Summary

| Severity | Count | Fixed | Remaining |
|----------|-------|-------|-----------|
| HIGH | 2 | 2 | 0 |
| MEDIUM | 5 | 3 | 2 |
| LOW | 2 | 2 | 0 |
| **Total** | **9** | **7** | **2** |

### Action Items

- [x] H1: IPC reconnection logic — Close()+Connect() on SendContext failure
- [x] H2: wails.Run() error handling — os.Exit(1) on failure
- [x] M1: GetStatus() context — use a.ctx instead of context.Background()
- [x] M2: wails.json — created with minimal config for wails dev/build
- [x] M3: Wails binding path — confirmed `window.go.desktop.App` correct via `wails build`
- [x] M4: setInterval cleanup — startPolling() with clearInterval guard
- [x] M5: Font .woff2 files — provided by user, casing fix applied (rajdhani-semibold.woff2)
- [x] L1: var → const in app.js
- [x] L2: RelayID conditional on connected/connecting status
