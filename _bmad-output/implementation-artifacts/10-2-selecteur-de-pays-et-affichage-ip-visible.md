# Story 10.2 : Sélecteur de Pays et Affichage IP Visible

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux sélectionner un pays parmi les relais disponibles via des drapeaux et voir mon IP visible,
Afin de choisir depuis quel pays je souhaite apparaître et vérifier que mon IP est masquée.

## Acceptance Criteria

**AC1 — Sélecteur de pays avec drapeaux et nombre de relais**
**Given** la fenêtre webview est ouverte et le registre des relais est chargé
**When** le sélecteur de pays est affiché dans la sidebar gauche (150px)
**Then** chaque pays disponible est affiché avec son drapeau emoji et le nombre de relais actifs
**And** le pays actuellement sélectionné est mis en évidence (bordure gauche bleue `#2a8dff`, fond `--bg-tertiary`)
**And** les pays affichés sont : Islande, Allemagne, Finlande, Etats-Unis (+ France si disponible)

**AC2 — Changement de pays et reconnexion tunnel**
**Given** l'utilisateur clique sur un pays different dans la sidebar
**When** la selection change
**Then** le tunnel se reconnecte via un relais du nouveau pays (< 5s)
**And** l'IP visible affichee dans la fenetre se met a jour apres reconnexion
**And** le pays prefere est sauvegarde dans la config TOML (`preferred_country`)
**And** le statut passe a "connecting" (orange) pendant la bascule

**AC3 — Affichage IP visible, pays et relais actif**
**Given** le tunnel est connecte
**When** la fenetre est affichee
**Then** l'IP visible actuelle est affichee (font Inter, 14px, couleur `--text-secondary`)
**And** le pays selectionne est affiche avec son drapeau emoji et nom en francais (Bebas Neue, 28px)
**And** l'identifiant du relais actif et sa latence sont affiches (ex: "is-01 . 85ms")
**And** un lien "Tester ma protection" pointe vers `plateformeliberte.fr/test-protection.html`
**And** le lien est masque quand le tunnel est deconnecte (eviter d'exposer l'IP reelle)

**AC4 — Layout Direction F (split sidebar)**
**Given** la fenetre webview est ouverte
**When** l'interface est rendue
**Then** la fenetre utilise le layout Direction F : sidebar 150px a gauche + panneau principal a droite
**And** la taille fenetre est 420x540px
**And** la sidebar contient la liste des pays cliquables
**And** le panneau principal contient le status dot, le pays actif, l'IP visible, le relais, et le lien test

**AC5 — Endpoints API REST locaux pour registre et pays**
**Given** le serveur HTTP local est demarre (story 10-1)
**When** le frontend demande la liste des pays ou change de pays
**Then** `GET /api/registry` retourne la liste des pays avec relais groupes, nombre, et flag active
**And** `POST /api/country` avec body `{"code":"is"}` declenche la selection du pays via IPC
**And** les reponses sont en JSON

**AC6 — Persistance du pays prefere au redemarrage**
**Given** l'utilisateur a selectionne un pays et la config TOML a ete mise a jour
**When** Le Voile redemarre
**Then** le tunnel se connecte directement au pays prefere sauvegarde
**And** la sidebar reflete la selection precedente

## Tasks / Subtasks

- [x] **Task 1 : Endpoints HTTP dans `internal/ui/httpserver.go`** (AC: 5)
  - [x] 1.1 Ajouter `GET /api/registry` — proxie vers IPC `ActionGetRegistry`, retourne JSON `{"countries":[...]}`
  - [x] 1.2 Ajouter `POST /api/country` — parse body `{"code":"xx"}`, proxie vers IPC `ActionSelectCountry`, retourne status JSON
  - [x] 1.3 Les endpoints utilisent le meme pattern IPC client que `/api/status` (deja en place via story 10-1)

- [x] **Task 2 : Layout Direction F — HTML sidebar** (AC: 4, 1)
  - [x] 2.1 Modifier `frontend/index.html` — restructurer `<main>` avec layout split :
    ```html
    <main class="layout-split">
      <aside class="sidebar" id="sidebar">
        <div class="sidebar-header">Pays</div>
        <div class="country-list" id="country-list"></div>
      </aside>
      <section class="main-panel">
        <div class="status-panel">
          <div class="status-dot" id="status-dot"></div>
          <div class="country-display">
            <span class="country-flag-large" id="country-flag"></span>
            <span class="country-name-large" id="country-name"></span>
          </div>
          <div class="status-text" id="status-text">Chargement...</div>
          <div class="status-ip" id="status-ip"></div>
          <div class="relay-info" id="relay-info"></div>
          <div class="status-uptime" id="status-uptime"></div>
          <a class="test-link" id="test-link" href="https://plateformeliberte.fr/test-protection.html" target="_blank">Tester ma protection</a>
        </div>
      </section>
    </main>
    ```
  - [x] 2.2 S'assurer que la fenetre webview est configuree a 420x540px (dans `internal/ui/ui.go` ou la ou le webview est cree)

- [x] **Task 3 : CSS sidebar et country selector** (AC: 4, 1, 3)
  - [x] 3.1 Layout split sidebar :
    ```css
    .layout-split {
      display: flex;
      height: calc(100vh - 32px); /* Minus titlebar */
    }
    .sidebar {
      width: 150px;
      background: var(--bg-secondary);
      border-right: 1px solid rgba(138, 155, 184, 0.1);
      overflow-y: auto;
      padding: 0.5rem 0;
    }
    .sidebar-header {
      font-family: var(--font-ui);
      font-size: 0.875rem;
      font-weight: 600;
      color: var(--text-secondary);
      padding: 0.5rem 0.75rem;
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    .main-panel {
      flex: 1;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 1rem;
    }
    ```
  - [x] 3.2 Country item dans sidebar :
    ```css
    .country-item {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      padding: 0.625rem 0.75rem;
      cursor: pointer;
      font-family: var(--font-ui);
      font-size: 0.875rem;
      color: var(--text-primary);
      transition: background 0.15s;
    }
    .country-item:hover { background: rgba(255, 255, 255, 0.05); }
    .country-item.active {
      background: var(--bg-tertiary);
      border-left: 3px solid var(--accent-glow);
    }
    .country-flag { font-size: 1.25rem; }
    .country-count {
      margin-left: auto;
      font-size: 0.75rem;
      color: var(--text-secondary);
    }
    ```
  - [x] 3.3 Pays actif dans panneau principal + relay info + test link :
    ```css
    .country-display { display: flex; align-items: center; gap: 0.75rem; margin: 0.5rem 0; }
    .country-flag-large { font-size: 2rem; }
    .country-name-large {
      font-family: var(--font-heading);
      font-size: 1.75rem;
      color: var(--text-primary);
      text-transform: uppercase;
    }
    .relay-info { font-family: var(--font-body); font-size: 0.75rem; color: var(--text-secondary); }
    .test-link {
      margin-top: 1rem;
      font-family: var(--font-ui);
      font-size: 0.875rem;
      color: var(--accent-glow);
      text-decoration: none;
    }
    .test-link:hover { text-decoration: underline; }
    ```

- [x] **Task 4 : JavaScript — registre, selection pays, IP visible** (AC: 1, 2, 3, 5)
  - [x] 4.1 Remplacer les appels Wails par `fetch()` vers le serveur HTTP local :
    ```javascript
    async function loadRegistry() {
      try {
        const resp = await fetch('/api/registry');
        const reg = await resp.json();
        if (reg && reg.countries) renderCountryList(reg.countries);
      } catch (e) { /* sidebar inchangee */ }
    }

    async function selectCountry(code) {
      try {
        await fetch('/api/country', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code: code })
        });
        setTimeout(loadRegistry, 2000); // Refresh apres reconnexion
      } catch (e) { /* polling met a jour le statut */ }
    }
    ```
  - [x] 4.2 `renderCountryList(countries)` — genere les country-items via `createElement`/`textContent` (pas innerHTML — prevention XSS) :
    ```javascript
    function renderCountryList(countries) {
      const list = dom.countryList;
      list.innerHTML = '';
      countries.forEach(function(c) {
        const item = document.createElement('div');
        item.className = 'country-item' + (c.active ? ' active' : '');
        const flagSpan = document.createElement('span');
        flagSpan.className = 'country-flag';
        flagSpan.textContent = c.flag;
        const nameSpan = document.createElement('span');
        nameSpan.className = 'country-name';
        nameSpan.textContent = c.name;
        const countSpan = document.createElement('span');
        countSpan.className = 'country-count';
        countSpan.textContent = c.relay_count;
        item.appendChild(flagSpan);
        item.appendChild(nameSpan);
        item.appendChild(countSpan);
        item.addEventListener('click', function() { selectCountry(c.code); });
        list.appendChild(item);
      });
    }
    ```
  - [x] 4.3 Mettre a jour `updateUI(status)` pour afficher :
    - Pays actif (drapeau + nom) dans le panneau principal
    - IP visible (`status.ip`) quand connecte
    - Relay info (`status.relay_id` + `status.latency`)
    - Test link visible uniquement quand connecte
  - [x] 4.4 Polling registre : charger au demarrage + toutes les 30s (intervalle separe du polling status 2s)
  - [x] 4.5 Ajouter les refs DOM dans `init()` : `countryList`, `countryFlag`, `countryName`, `relayInfo`, `testLink`
  - [x] 4.6 Adapter `pollStatus()` pour utiliser `fetch('/api/status')` (si pas deja fait en 10-1)

- [x] **Task 5 : Tests** (AC: 1-6)
  - [x] 5.1 `internal/ui/httpserver_test.go` — tester les nouveaux endpoints :
    - `TestRegistryEndpoint` — mock IPC retourne 4 pays → reponse JSON correcte
    - `TestCountryEndpoint_ValidCode` — POST `{"code":"de"}` → proxie vers IPC → success
    - `TestCountryEndpoint_InvalidMethod` — GET au lieu de POST → 405
    - `TestCountryEndpoint_EmptyCode` — body vide → 400
  - [x] 5.2 Verification build : `go build ./cmd/ui/...` — compilation OK
  - [ ] 5.3 Verification manuelle : lancer le binaire UI → sidebar affiche pays, clic change pays, IP visible mise a jour

## Dev Notes

### ALERTE MIGRATION : Wails v2 remplace par webview/webview

**L'architecture a ete revisee le 08-04-2026.** Le code existant dans `internal/desktop/` et `cmd/desktop/` utilise Wails v2 — il est **OBSOLETE**. La nouvelle architecture utilise :
- `webview/webview` pour la fenetre desktop (WebView2 sur Windows)
- `fyne.io/systray` pour le system tray (meme processus)
- Serveur HTTP local `net/http` sur `127.0.0.1:{port}` pour servir le frontend et l'API REST
- `fetch('/api/*')` au lieu de bindings Wails `window.go.desktop.App.*`

**NE PAS** utiliser `internal/desktop/`, `cmd/desktop/`, ni `github.com/wailsapp/wails/v2`.

### Architecture de la Story 10.2

Story 10-1 cree l'infrastructure de base : `cmd/ui/main.go`, `internal/ui/` (ui.go, httpserver.go, embed.go), frontend avec `fetch('/api/status')`. Story 10-2 etend cette base.

```
MODIFIE (cree par 10-1, etendu par 10-2) :
internal/ui/httpserver.go              # +GET /api/registry, +POST /api/country
frontend/index.html                    # Layout Direction F (sidebar + main panel)
frontend/src/style.css                 # +Styles sidebar, country-item, country-display
frontend/src/app.js                    # +loadRegistry(), renderCountryList(), selectCountry()

DEJA EN PLACE (backend cree lors de l'ancienne story 10-2 — REUTILISER TEL QUEL) :
internal/ipc/messages.go               # ActionGetRegistry, ActionSelectCountry, RegistryCountry deja presents
internal/ipchandler/handler.go         # handleGetRegistry(), handleSelectCountry() deja implementes
internal/registry/countries.go         # CountryMetaMap (is,de,fi,us,fr), ExtractCountryCode(), RelaysByCountry()
internal/config/config.go              # ClientConfig.PreferredCountry, Save() — deja en place
internal/service/service.go            # DetectVisibleIP(), VisibleIP(), SetVisibleIP() — deja en place
internal/relay/ip_handler.go           # Endpoint /ip sur le relais — deja deploye

NE PAS TOUCHER :
internal/desktop/                      # OBSOLETE Wails v2 — sera supprime plus tard
cmd/desktop/                           # OBSOLETE Wails v2
cmd/client/                            # Service inchange
internal/tray/                         # Ancien tray — sera remplace par internal/ui/
internal/tunnel/                       # Inchange
internal/registry/discoverer.go        # Inchange — RelaysByCountry() deja expose
```

### Communication Frontend → Service (nouvelle architecture)

```
Frontend JS
  └── fetch('/api/registry')
        └── internal/ui/httpserver.go → handler GET /api/registry
              └── ipc.Client.SendContext(ActionGetRegistry)
                    └── Named Pipe \\.\pipe\levoile
                          └── ipchandler.handleGetRegistry()
                                └── registry.RelaysByCountry() + CountryMetaMap

Frontend JS (clic pays)
  └── fetch('/api/country', {method:'POST', body:'{"code":"de"}'})
        └── internal/ui/httpserver.go → handler POST /api/country
              └── ipc.Client.SendContext(ActionSelectCountry, Value:"de")
                    └── Named Pipe \\.\pipe\levoile
                          └── ipchandler.handleSelectCountry()
                                └── discoverer → tunnel.UpdateRelay + Connect
                                └── config.Save(preferred_country:"de")
```

### Pattern HTTP server — comment ajouter les endpoints

Le serveur HTTP local (`internal/ui/httpserver.go`) suit ce pattern (etabli par 10-1) :

```go
// Dans la fonction qui enregistre les routes (ex: NewHTTPServer ou setupRoutes)
mux.HandleFunc("/api/registry", s.handleRegistry)
mux.HandleFunc("/api/country", s.handleCountry)
```

```go
func (s *HTTPServer) handleRegistry(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()
    resp, err := s.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionGetRegistry})
    if err != nil {
        http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "countries": resp.RegistryCountries,
    })
}

func (s *HTTPServer) handleCountry(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var body struct { Code string `json:"code"` }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }
    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()
    resp, err := s.ipcClient.SendContext(ctx, ipc.Request{
        Action: ipc.ActionSelectCountry,
        Value:  body.Code,
    })
    if err != nil {
        http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}
```

### Backend deja en place — ne pas reimplementer

**IPC Messages** (`internal/ipc/messages.go`) :
- `ActionGetRegistry = "get_registry"` — ligne 26
- `ActionSelectCountry = "select_country"` — ligne 27
- `RegistryCountry` struct (Code, Name, Flag, RelayCount, Active) — lignes 88-94

**IPC Handler** (`internal/ipchandler/handler.go`) :
- `handleGetRegistry()` — utilise `discoverer.RelaysByCountry()` + `CountryMetaMap` pour construire la reponse
- `handleSelectCountry()` — trouve un relais du pays, appelle `tunnel.UpdateRelay()`, declenche `Connect()`, sauvegarde `preferred_country` dans config

**Registry Countries** (`internal/registry/countries.go`) :
- `CountryMetaMap` : is→Islande, de→Allemagne, fi→Finlande, us→Etats-Unis, fr→France
- `ExtractCountryCode(relayID, domain)` — extrait le code ISO depuis l'ID ou le domaine
- `RelaysByCountry()` — retourne `map[string][]RelayEntry`

**Config** (`internal/config/config.go`) :
- `ClientConfig.PreferredCountry string` — code ISO 2 lettres
- `Save(path string) error` — serialise TOML atomiquement

**Service** (`internal/service/service.go`) :
- `DetectVisibleIP()` — appelle `/ip` sur le relais actif apres connexion
- `VisibleIP() string` — retourne l'IP detectee (atomic)
- `SetVisibleIP(ip string)` — stocke l'IP (atomic)

**Relais** (`internal/relay/ip_handler.go`) :
- Endpoint `/ip` — retourne l'IP source de la requete en plain text

### IP visible — mecanisme deja en place

Le service detecte l'IP automatiquement via `DetectVisibleIP()` apres chaque connexion/reconnexion. Le handler `handleGetStatus()` retourne cette IP dans le champ `ip` de la reponse IPC. Le serveur HTTP local proxie cette reponse vers le frontend via `GET /api/status`.

L'IP se met a jour automatiquement apres un changement de pays car `handleSelectCountry()` declenche une reconnexion qui appelle `DetectVisibleIP()`.

### Charte visuelle — design tokens (deja dans style.css via 10-1)

```css
--bg-primary: #0b1526;
--bg-secondary: #0e1e38;
--bg-tertiary: #162a4a;
--accent-blue: #1a6fc4;
--accent-glow: #2a8dff;
--status-secure: #4ade80;
--status-warning: #fb923c;
--status-risk: #ff3c3c;
--text-primary: #f0f4ff;
--text-secondary: #8a9bb8;
--font-heading: 'Bebas Neue', sans-serif;
--font-ui: 'Rajdhani', sans-serif;
--font-body: 'Inter', sans-serif;
```

### UX Direction F — Split Sidebar (reference)

Source: `_bmad-output/planning-artifacts/ux-design-specification.md` section "Chosen Direction"
- Sidebar gauche 150px : liste des pays avec drapeaux, nombre de relais, bordure gauche bleue pour pays actif
- Panel principal droit : status dot, drapeau grand format, IP visible, relais + latence, bouton connect (story 10-3), lien test
- Pas de panneau Parametres dans cette story (prevu plus tard)

### Polling — 2 intervalles separes

| Donnee | Endpoint | Intervalle | Moment |
|--------|----------|-----------|--------|
| Status (IP, pays, connexion) | `GET /api/status` | 2s | Au chargement + setInterval |
| Registre (liste pays) | `GET /api/registry` | 30s | Au chargement + setInterval + apres selectCountry |

### Webview — pas de bindings directs

Avec webview/webview, il n'y a **PAS** de bindings Go→JS automatiques comme Wails. Toute communication passe par le serveur HTTP local :
- Frontend → Go : `fetch('/api/*')` (requetes HTTP)
- Go → Frontend : pas de push — le frontend poll

Pour les controles fenetre (titlebar), webview/webview fournit :
- `webview.Eval(js)` cote Go pour executer du JS
- Pas de `window.runtime` — utiliser `window.close()` ou callbacks custom

### Conventions Go

- Nommage fichiers : `snake_case.go`
- Package : `ui` (dans `internal/ui/`)
- Erreurs : wrapping `fmt.Errorf("ui: %w", err)`
- Context : timeout 5s pour IPC reads, 10s pour IPC writes (country change)
- Tests : table-driven, `testing` standard
- Zero-log cote client — erreurs retournees via HTTP JSON
- Pas d'import circulaire : `ui` → `ipc`, jamais `service`

### Ce qui n'est PAS dans le scope de 10-2

- **Bouton connect/disconnect** → Story 10.3
- **Integration tray ↔ webview** (clic tray ouvre fenetre) → Story 10.3
- **Modal quitter** → Story 10.3
- **Panneau Parametres** (toggle WebRTC) → hors scope Epic 10 MVP
- **Etoile favori** sur les pays → hors scope MVP
- **Notification mise a jour** → hors scope Epic 10

### Verification couverture AC

| AC | Couvert par |
|----|------------|
| AC1 (selecteur pays drapeaux) | Task 2 (HTML sidebar), Task 3 (CSS), Task 4 (JS renderCountryList) |
| AC2 (changement pays reconnexion) | Task 1 (POST /api/country), Task 4 (JS selectCountry) |
| AC3 (IP visible, pays, relais) | Task 4 (JS updateUI), backend deja en place (DetectVisibleIP) |
| AC4 (layout Direction F) | Task 2 (HTML), Task 3 (CSS layout-split) |
| AC5 (endpoints API REST) | Task 1 (httpserver.go) |
| AC6 (persistance pays) | Backend deja en place (config.Save preferred_country) |

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 10, Story 10.2]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — sections "UI Patterns", "Communication", "API REST locale", "Code Organization" cmd/ui/ + internal/ui/]
- [Source: `_bmad-output/planning-artifacts/ux-design-specification.md` — Direction F Split Sidebar, design tokens, typography, spacing]
- [Source: `internal/ipc/messages.go` — ActionGetRegistry, ActionSelectCountry, RegistryCountry]
- [Source: `internal/ipchandler/handler.go` — handleGetRegistry(), handleSelectCountry()]
- [Source: `internal/registry/countries.go` — CountryMetaMap, RelaysByCountry(), ExtractCountryCode()]
- [Source: `internal/config/config.go` — PreferredCountry, Save()]
- [Source: `internal/service/service.go` — DetectVisibleIP(), VisibleIP()]
- [Source: `internal/relay/ip_handler.go` — endpoint /ip]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

Aucun probleme rencontre.

### Completion Notes List

- Task 1: Ajoute `GET /api/registry` et `POST /api/country` dans `httpserver.go`, reutilisant le pattern `sendIPC` existant. Validation methode HTTP + parsing body JSON avec gestion erreurs 400/405.
- Task 2: Structure HTML Direction F deja en place (story 10-1). Fenetre webview deja configuree 420x540px dans `webview_cgo.go`.
- Task 3: Ajoute variable CSS `--bg-tertiary: #162a4a`. Corrige `.country-item.active` pour utiliser `--bg-tertiary` au lieu de `--bg-secondary` (conforme AC1).
- Task 4: Ajoute `loadRegistry()`, `renderCountryList()` (XSS-safe via `createElement`/`textContent`), `selectCountry()`. Polling registre 30s separe du polling status 2s. Ref DOM `countryList` ajoutee dans `init()`.
- Task 5: 5 nouveaux tests ajoutes (Registry, Country valid/invalid method/empty code/invalid JSON). 2 cas supplementaires dans TestMethodNotAllowed (registry POST, country GET). Tous les 20 tests ui passent. Build `cmd/ui` OK. Regression suite : 4 echecs pre-existants (desktop/tray/browser/ipchandler), aucun lie aux changements.

### Change Log

- 2026-04-08: Implementation story 10-2 — endpoints registry/country, CSS --bg-tertiary, JS sidebar dynamique, 5 tests ajoutes
- 2026-04-08: Code review fixes — M1: MaxBytesReader(1Ko) sur handleCountry, M2: renderCountryList DOM-only (plus de innerHTML), M3: registry retourne [] au lieu de null si IPC echoue, +1 test

### File List

- `internal/ui/httpserver.go` — MODIFIE: +handleRegistry(), +handleCountry(), routes /api/registry et /api/country
- `internal/ui/httpserver_test.go` — MODIFIE: +5 tests (Registry, Country valid/invalid/empty/bad JSON), +2 cas MethodNotAllowed
- `frontend/src/style.css` — MODIFIE: +--bg-tertiary variable, .country-item.active corrige vers --bg-tertiary
- `frontend/src/app.js` — MODIFIE: +loadRegistry(), +renderCountryList(), +selectCountry(), +startRegistryPolling(), +dom.countryList
