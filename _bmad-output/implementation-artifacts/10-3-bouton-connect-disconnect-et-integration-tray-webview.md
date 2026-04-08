# Story 10.3 : Bouton Connect/Disconnect et Intégration Tray ↔ Webview

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
je veux connecter/déconnecter Le Voile depuis la fenêtre ou le tray, et ouvrir la fenêtre depuis le tray,
afin de contrôler ma protection depuis n'importe quel point d'accès.

## Acceptance Criteria

1. **AC1 — Bouton Connect/Disconnect dans la fenêtre webview**
   **Given** la fenêtre webview est ouverte
   **When** l'utilisateur clique sur le bouton Connect/Disconnect
   **Then** le frontend appelle `fetch('/api/connect', {method:'POST'})` ou `fetch('/api/disconnect', {method:'POST'})`
   **And** le serveur HTTP local proxie la commande vers le service via IPC (`ActionConnect` / `ActionDisconnect`)
   **And** l'indicateur de statut et l'icône tray se mettent à jour simultanément

2. **AC2 — Menu tray "Ouvrir" crée/montre la fenêtre webview**
   **Given** le tray icon est visible dans la barre des tâches
   **When** l'utilisateur sélectionne "Ouvrir" dans le menu tray
   **Then** une fenêtre webview (420×540px, frameless) est créée et navigue vers `http://127.0.0.1:{port}/`
   **And** si une fenêtre existe déjà, elle est mise au premier plan

3. **AC3 — Fermeture webview → destruction, tray persiste**
   **Given** la fenêtre webview est ouverte
   **When** l'utilisateur ferme la fenêtre (croix ✕ ou via le modal quitter)
   **Then** la fenêtre webview est détruite (ressources libérées)
   **And** le tray et le service continuent de fonctionner
   **And** la protection reste active (tunnel, DNS, proxy)
   **And** une réouverture depuis le tray crée une nouvelle fenêtre webview

4. **AC4 — Menu tray "Connecter/Déconnecter" synchronisé**
   **Given** le menu clic droit du tray contient "Connecter" ou "Déconnecter" (selon l'état)
   **When** l'utilisateur sélectionne cette option
   **Then** le tunnel s'active/se désactive via IPC
   **And** l'icône tray change (vert/orange/rouge)
   **And** la fenêtre webview (si ouverte) reflète le changement d'état via le polling /api/status

5. **AC5 — États du bouton et feedback visuel**
   **Given** l'état du tunnel change
   **When** le frontend poll `/api/status` (toutes les 2s)
   **Then** le bouton s'adapte selon le tableau :
   | État | Action visible |
   |---|---|
   | Connecté | Bouton "Déconnecter" (rouge transparent, bordure rouge) |
   | Transition/Reconnexion | Aucun bouton (masqué, disabled) |
   | Déconnecté | Bouton "Connecter" (fond vert `#4ade80`) |
   | Pays sélectionné ≠ pays connecté | Bouton "Connecter" (fond vert) |
   | Erreur (aucun relais) | Aucun bouton |

## Tasks / Subtasks

- [x] Task 1 — Endpoints HTTP `/api/connect` et `/api/disconnect` (AC: #1)
  - [x] 1.1 Ajouter handlers POST dans `internal/ui/httpserver.go`
  - [x] 1.2 Chaque handler envoie `ipc.ActionConnect` / `ipc.ActionDisconnect` via `ipc.Client` au service
  - [x] 1.3 Retourner la `StatusResponse` JSON au frontend
  - [x] 1.4 Tests unitaires handlers (mock IPC client)

- [x] Task 2 — Bouton Connect/Disconnect frontend (AC: #1, #5)
  - [x] 2.1 Ajouter le bouton HTML dans `frontend/index.html` (zone panel principal, après le lien "Tester ma protection")
  - [x] 2.2 Implémenter `toggleConnect()` dans `frontend/src/app.js` — appelle `fetch('/api/connect')` ou `fetch('/api/disconnect')` selon l'état
  - [x] 2.3 Mettre à jour `updateUI()` pour gérer la visibilité et le style du bouton selon le tableau AC5
  - [x] 2.4 Styling CSS dans `frontend/src/style.css` :
    - Bouton "Connecter" : fond `#4ade80`, texte `#0b1526`, Rajdhani 600, hover glow vert
    - Bouton "Déconnecter" : transparent, bordure rouge `#d42b2b` 30% opacité, texte rouge, hover fond rouge 10%
    - Disabled : `opacity: 0.5`, `pointer-events: none`
    - Zone cliquable minimum : 44×44px
  - [x] 2.5 Gestion du cas "pays sélectionné ≠ pays connecté" → afficher "Connecter" au lieu de "Déconnecter"

- [x] Task 3 — Menu tray "Connecter/Déconnecter" (AC: #4)
  - [x] 3.1 Ajouter un item `menuToggle` dans le menu systray (`internal/ui/ui.go`)
  - [x] 3.2 Le libellé alterne entre "Connecter" et "Déconnecter" selon l'état du tunnel (mis à jour au polling 2s)
  - [x] 3.3 Le clic envoie `ActionConnect` ou `ActionDisconnect` via `ipc.Client`
  - [x] 3.4 L'icône tray se met à jour immédiatement (vert/orange/rouge)

- [x] Task 4 — Menu tray "Ouvrir" et lifecycle webview (AC: #2, #3)
  - [x] 4.1 Ajouter un item `menuOpen` dans le menu systray
  - [x] 4.2 Au clic "Ouvrir" : créer une fenêtre webview via `webview.New()` (420×540, frameless, titre "Le Voile"), naviguer vers `http://127.0.0.1:{port}/`
  - [x] 4.3 Tracker l'instance webview dans une variable (`var currentWebview *webview.WebView`) — nil quand fermée
  - [x] 4.4 Si fenêtre déjà ouverte et "Ouvrir" cliqué → ne pas créer de doublon (focus ou no-op)
  - [x] 4.5 Quand la fenêtre se ferme → `currentWebview.Destroy()`, set `currentWebview = nil`
  - [x] 4.6 Le tray persiste — `systray.Run()` reste bloquant sur le main thread
  - [x] 4.7 Webview `.Run()` exécuté dans une goroutine dédiée (ATTENTION : webview/webview requiert le main thread sur certaines plateformes — vérifier la doc pour Windows)

- [x] Task 5 — Synchronisation état tray ↔ webview (AC: #1, #4, #5)
  - [x] 5.1 Le polling IPC existant (2s) dans le tray met à jour : icône tray + libellé menuToggle
  - [x] 5.2 Le polling frontend (2s) via `fetch('/api/status')` met à jour : bouton + indicateur + texte
  - [x] 5.3 Pas de canal direct tray→webview — la synchronisation passe par le polling indépendant de chacun
  - [x] 5.4 Vérifier que les deux sources (tray et webview) peuvent déclencher connect/disconnect sans conflit (le service gère l'idempotence)

- [x] Task 6 — Tests et validation (AC: tous)
  - [x] 6.1 Tests unitaires Go : handlers `/api/connect`, `/api/disconnect` dans `internal/ui/httpserver_test.go`
  - [x] 6.2 Test tray menu toggle : mock IPC, vérifier les appels `ActionConnect`/`ActionDisconnect`
  - [x] 6.3 Build vérifié : `go build ./cmd/ui/...` + `go build ./cmd/client/...`
  - [x] 6.4 Vérification manuelle : bouton webview → connect/disconnect, menu tray → connect/disconnect, fermer webview → tray persiste, rouvrir → état correct

## Dev Notes

### ALERTE ARCHITECTURE — Migration Wails v2 → webview/webview + fyne.io/systray

**Le codebase actuel utilise encore l'ancienne architecture Wails v2 (3 processus).** Les stories 10-1 à 10-3 doivent migrer vers la nouvelle architecture 2 processus. Cette story 10-3 s'appuie sur les fondations posées par 10-1 (binaire UI, systray, webview, HTTP server, statut) et 10-2 (sélecteur pays, IP visible, registre).

**Ancienne architecture (à ne PAS suivre) :**
- `cmd/desktop/main.go` → binaire Wails v2 `levoile-desktop.exe`
- `cmd/tray/main.go` → binaire tray séparé `levoile-tray.exe`
- `internal/desktop/app.go` → Wails bindings `window.go.desktop.App.Method()`
- `internal/tray/tray.go` → processus tray indépendant, lance desktop en subprocess

**Nouvelle architecture (à suivre) :**
- `cmd/ui/main.go` → binaire unique `levoile-ui.exe`
- `internal/ui/ui.go` → fyne.io/systray (main thread) + webview/webview (à la demande) + serveur HTTP local
- `internal/ui/httpserver.go` → API REST JSON (`/api/connect`, `/api/disconnect`, etc.) + assets embarqués
- `internal/ui/embed.go` → `//go:embed frontend`
- Frontend : `fetch('/api/...')` au lieu de `window.go.desktop.App.Method()`

### Pattern de communication (nouvelle archi)

```
[Utilisateur clique bouton dans webview]
        ↓
[frontend/src/app.js → fetch('/api/connect', {method:'POST'})]
        ↓ (HTTP local 127.0.0.1:{port})
[internal/ui/httpserver.go → handler → ipc.Client.Send(ActionConnect)]
        ↓ (named pipe \\.\pipe\levoile)
[internal/ipchandler/handler.go → Handle(prg, req)]
        ↓
[service → Start tunnel, DNS, proxy]
        ↓ (IPC response)
[httpserver → JSON response → frontend updateUI()]
```

### Handlers IPC existants — NE PAS RECRÉER

`internal/ipchandler/handler.go` contient déjà `handleConnect` et `handleDisconnect` (fonctionnels). Les actions IPC sont définies dans `internal/ipc/messages.go` :
- `ActionConnect = "connect"`
- `ActionDisconnect = "disconnect"`
- `ActionGetStatus = "get_status"` (pour le polling)

Le serveur HTTP local (`httpserver.go`) doit simplement **proxier** les requêtes vers ces actions IPC existantes.

### Lifecycle webview — Points critiques

1. **fyne.io/systray v1.12.0** requiert le main thread (appel bloquant `systray.Run(onReady, onExit)`)
2. **webview/webview** sur Windows utilise WebView2 — vérifier si `.Run()` doit aussi être sur le main thread
3. **Pattern recommandé :** systray sur main thread, webview créé/détruit dans une goroutine ou via `systray.Run` callback — consulter la doc webview/webview pour le threading Windows
4. La fenêtre webview navigue vers `http://127.0.0.1:{port}/` — le serveur HTTP local doit être démarré AVANT la première création de webview
5. Fermer la fenêtre = `webview.Destroy()` + set nil — PAS de quitter l'application

### Tray polling et synchronisation

- Le tray poll le service via IPC toutes les 2 secondes → met à jour icône + libellé menuToggle
- Le frontend poll `/api/status` toutes les 2 secondes → met à jour bouton + indicateur
- **Pas de canal direct** entre tray et webview — chacun poll indépendamment
- Si l'utilisateur connecte via le tray, la webview le verra au prochain poll (max 2s de délai)
- Si l'utilisateur connecte via la webview, le tray le verra au prochain poll (max 2s de délai)
- Le service est **idempotent** : appeler Connect quand déjà connecté = no-op

### UX — Bouton Connect/Disconnect

**Bouton "Connecter" (état déconnecté) :**
- Fond : `#4ade80` (vert)
- Texte : `#0b1526` (navy), Rajdhani 600
- Hover : vert lumineux + `box-shadow: 0 0 16px rgba(74, 222, 128, 0.3)`
- `aria-label: "Se connecter à [pays]"`

**Bouton "Déconnecter" (état connecté) :**
- Fond : transparent
- Bordure : `#d42b2b` à 30% opacité
- Texte : `#d42b2b` (rouge), Rajdhani 600
- Hover : fond rouge 10% opacité, bordure rouge pleine

**Pendant transition :** bouton masqué (`display: none` ou classe `hidden`), `opacity: 0.5` si affiché en disabled

**Cas spécial pays ≠ connecté :** Si l'utilisateur a sélectionné un pays différent du pays actuellement connecté → afficher "Connecter" (vert) au lieu de "Déconnecter". Le clic déclenchera la reconnexion au nouveau pays.

### Icônes tray

Fichiers dans `internal/ui/icons/` embarqués via `//go:embed` dans `internal/ui/icons.go` :
- `connected.ico` → V vert `#4ade80`
- `connecting.ico` → V orange `#fb923c`
- `disconnected.ico` → V rouge `#ff3c3c`
- `levoile.ico` → icône par défaut

### Menu tray (items complets)

1. **Ouvrir** — crée/montre la fenêtre webview
2. **Connecter / Déconnecter** — toggle via IPC, libellé dynamique
3. **Quitter** — arrêt propre (story 12-1, pas dans le scope de cette story)

### Project Structure Notes

**Fichiers à créer/modifier dans cette story :**

| Fichier | Action | Description |
|---|---|---|
| `internal/ui/httpserver.go` | MODIFIER | Ajouter handlers POST `/api/connect` et `/api/disconnect` |
| `internal/ui/httpserver_test.go` | MODIFIER | Tests unitaires nouveaux handlers |
| `internal/ui/ui.go` | MODIFIER | Ajouter menuToggle + menuOpen dans le menu systray, lifecycle webview |
| `internal/ui/ui_test.go` | MODIFIER | Tests tray toggle |
| `frontend/index.html` | MODIFIER | Ajouter bouton connect/disconnect dans le panel principal |
| `frontend/src/app.js` | MODIFIER | `toggleConnect()`, mise à jour `updateUI()` pour état bouton |
| `frontend/src/style.css` | MODIFIER | Styles bouton connect (vert) et disconnect (rouge) |

**Fichiers existants à NE PAS toucher :**

| Fichier | Raison |
|---|---|
| `internal/ipc/messages.go` | Actions déjà définies |
| `internal/ipchandler/handler.go` | Handlers déjà fonctionnels |
| `internal/service/service.go` | Logique tunnel inchangée |
| `internal/tunnel/` | Pas de changement requis |
| `internal/dns/` | Pas de changement requis |

### Learnings de la story 10-2 (code review)

1. **XSS** — Ne jamais utiliser `innerHTML` pour du contenu dynamique. Utiliser `createElement` + `textContent`
2. **Erreurs IPC** — Toujours retourner les erreurs dans la réponse JSON, ne pas les ignorer silencieusement
3. **Réinitialisation état** — Quand le pays change, réinitialiser `visibleIP` à `""` avant de relancer la détection
4. **Tri déterministe** — Trier les listes (pays, relais) avec `sort.Slice` pour un affichage stable
5. **`const` vs `var`** — Utiliser `const` en JS pour les valeurs non réassignées
6. **Hover visible** — Ajouter `background: rgba(255,255,255,0.05)` sur les items hover
7. **Zero-log** — Aucun `log.Println` ni `fmt.Println` — erreurs uniquement via IPC/JSON
8. **Context timeout** — 5s pour tous les appels IPC
9. **Nommage** — Fichiers `snake_case.go`, erreurs `fmt.Errorf("ui: %w", err)`
10. **Imports** — Aucun import circulaire, aucune nouvelle dépendance externe

### Git Intelligence

Derniers commits pertinents :
- `b10febd` — manifest v2 backup extension navigateur
- `94abe13` — auto-fallback DIRECT quand proxy down (extension)
- `1640da5` — desktop shortcuts, tray icon, clean shutdown (patterns tray/shutdown existants)
- `340b6f6` — adversarial code review fixes, installer proxy cleanup

Pattern observé : les commits récents sont des fixes/polish. L'architecture tray actuelle (`internal/tray/tray.go`) sera remplacée par `internal/ui/ui.go`. Les patterns de shutdown propre dans `1640da5` sont un bon modèle pour le lifecycle webview.

### Bibliothèques et versions

| Bibliothèque | Version | Utilisation dans cette story |
|---|---|---|
| `webview/webview` | latest | Fenêtre desktop WebView2, création/destruction |
| `fyne.io/systray` | v1.12.0 | Menu tray (Ouvrir, Connecter/Déconnecter) |
| `microsoft/go-winio` | v0.6.2 | IPC named pipe (via `internal/ipc/client.go`) |
| Go `net/http` | stdlib | Serveur HTTP local + handlers API |
| Go `testing` | stdlib | Tests unitaires |

### References

- [Source: _bmad-output/planning-artifacts/architecture.md — §11 Interface Desktop, §IPC, §API endpoints]
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md — §C12 Bouton Connecter, §C9 Status Dot, §Menu tray, §Layout Direction F]
- [Source: _bmad-output/planning-artifacts/epics.md — §Epic 10 Story 10.3]
- [Source: _bmad-output/planning-artifacts/prd.md — FR9, FR10, FR11, FR12, FR13, FR15]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

Aucun blocage rencontré.

### Completion Notes List

- **Task 1 (handlers connect/disconnect)** : Handlers déjà implémentés par story 10-1. Amélioré avec `actionResponse()` helper qui inclut le champ `error` dans la réponse JSON quand `resp.Error` est non vide (learning 10-2). 3 nouveaux tests ajoutés : IPC error connect, IPC error disconnect, error field inclus.
- **Task 2 (bouton frontend)** : Bouton, toggleConnect(), updateUI(), CSS déjà implémentés par stories 10-1/10-2. Ajouté la gestion du country mismatch (AC5) : variable `selectedCountryName` trackée dans app.js, comparée avec `status.country` dans updateUI(). État `error` masque le bouton (au lieu d'afficher "Connecter"). Synchro initiale du selectedCountryName depuis le registre actif.
- **Task 3 (tray toggle)** : Entièrement implémenté par story 10-1. menuToggle avec label dynamique, handleToggle() via IPC, icônes mise à jour.
- **Task 4 (webview lifecycle)** : Entièrement implémenté par story 10-1. menuOpen, handleOpenWebview() avec guard atomic.Bool, openWebview() dans goroutine avec defer Destroy.
- **Task 5 (sync tray↔webview)** : Polling indépendant 2s confirmé pour tray (IPC) et frontend (HTTP). Pas de canal direct. Service idempotent.
- **Task 6 (tests)** : 27/27 tests passent dans internal/ui. 3 nouveaux tests connect/disconnect + 3 tests handleToggle ajoutés. Builds cmd/ui et cmd/client OK. Échecs pré-existants dans packages obsolètes (desktop, tray, browser) non liés à cette story.

### Implementation Plan

La majorité des fonctionnalités étaient déjà en place (stories 10-1 et 10-2). Les modifications se concentrent sur :
1. Enrichissement des réponses JSON connect/disconnect avec champ error
2. Gestion du country mismatch dans le frontend (AC5)
3. Correction de l'état error pour masquer le bouton
4. Tests unitaires supplémentaires (handlers + tray toggle)

### File List

- `internal/ui/httpserver.go` — MODIFIÉ — Ajout helper `actionResponse()`, handlers connect/disconnect utilisent ce helper
- `internal/ui/httpserver_test.go` — MODIFIÉ — +3 tests (IPC error connect, IPC error disconnect, error field inclus)
- `internal/ui/ui_test.go` — MODIFIÉ — +3 tests (handleToggle connect, disconnect, IPC error)
- `frontend/src/app.js` — MODIFIÉ — Variable `selectedCountryName`, country mismatch dans updateUI(), synchro initiale registre, paramètre name dans selectCountry()
- `frontend/src/app.js` — MODIFIÉ (review) — aria-label dynamique, var→const, toggleConnect affiche erreurs, sync failover selectedCountryName
- `frontend/src/style.css` — MODIFIÉ — Ajout `--bg-tertiary`, `.country-item.active` background, `min-height: 44px` bouton, `pointer-events: none` disabled
- `frontend/index.html` — INCHANGÉ (bouton déjà en place)
- `internal/ui/httpserver.go` — MODIFIÉ (review) — handleCountry utilise actionResponse()
- `internal/ui/ui.go` — INCHANGÉ (tray toggle et webview lifecycle déjà implémentés)

### Change Log

- 2026-04-08 : Implémentation story 10-3 — bouton connect/disconnect, intégration tray↔webview. Handlers enrichis avec champ error. Frontend : gestion country mismatch (AC5), état error masque le bouton. +6 tests unitaires (handlers + tray toggle).
- 2026-04-08 : Code review adversarial — 9 issues trouvées (3H, 4M, 2L). 8 corrigées automatiquement : handleCountry actionResponse (H2), aria-label dynamique (H3), File List corrigée (M1), min-height 44px (M2), var→const (M3), feedback erreur toggleConnect (M4), sync failover selectedCountryName (L1), pointer-events:none (L2). H1 (task 6.4 vérification manuelle) reste à faire par l'utilisateur.

## Senior Developer Review (AI)

**Date :** 2026-04-08
**Reviewer :** Claude Opus 4.6 (code-review adversarial)
**Outcome :** Changes Requested → Fixed (8/9)

### Action Items

- [x] [H2] `handleCountry` n'utilisait pas `actionResponse()` — erreurs silencieuses → corrigé `httpserver.go`
- [x] [H3] `aria-label` absent sur bouton connect — accessibilité → ajouté dans `app.js` updateUI()
- [x] [M1] `style.css` marqué "INCHANGÉ" dans File List mais modifié dans git → File List corrigée
- [x] [M2] Hauteur bouton < 44px — cible tactile insuffisante → ajouté `min-height: 44px` dans CSS
- [x] [M3] `var` au lieu de `const` dans fonctions JS (learning 10-2 #5) → remplacé par `const`/`let`
- [x] [M4] Frontend ignore champ `error` des réponses connect/disconnect → `toggleConnect()` affiche `data.error`
- [x] [L1] Country mismatch fantôme sur failover → `renderCountryList` sync toujours le pays actif
- [x] [L2] `pointer-events: none` manquant sur `.btn-connect:disabled` → ajouté dans CSS
- [x] [H1] Task 6.4 (vérification manuelle) — validée par l'utilisateur
