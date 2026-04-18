# Story 5.5 : Toggle fenêtre via clic gauche tray + Quitter via clic droit

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur final,
je veux ouvrir/fermer la fenêtre rapidement via un clic gauche sur l'icône tray, et quitter via le menu clic droit,
afin que l'accès soit naturel et conforme aux conventions desktop.

## Acceptance Criteria

1. **AC1 — Clic gauche sur l'icône tray (Windows) → toggle fenêtre webview (2 états)**
   **Given** l'icône tray `levoile-ui` est visible dans la barre des tâches ET la webview a été créée au démarrage (jamais recréée après destruction — cf. AC3)
   **When** l'utilisateur fait un clic gauche sur l'icône
   **Then** si la fenêtre est cachée (minimisée via `─`) → elle est montrée (`ShowWindow SW_SHOW`) et amenée au premier plan (`SetWindowPos HWND_TOP` + focus)
   **And** si la fenêtre est déjà visible au premier plan → elle est cachée (`ShowWindow SW_HIDE`) → le tray persiste, la protection reste active
   **And** si la webview n'existe plus (app en cours d'arrêt ou pas encore démarrée) → clic = no-op (pas de recréation — rationale dans AC3)
   **And** le toggle est thread-safe (sends non-bloquants `select/default` ; pas de blocage en cas de clics rapides)

2. **AC2 — Menu contextuel via clic droit (Windows + Linux)**
   **Given** l'icône tray est visible
   **When** l'utilisateur fait un clic droit dessus
   **Then** le menu contextuel fyne.io/systray apparaît avec au minimum les entrées : "Ouvrir la fenêtre" et "Quitter"
   **And** sélectionner "Ouvrir la fenêtre" équivaut exactement à un clic gauche sur une fenêtre cachée/absente (create-or-show, jamais hide)
   **And** sélectionner "Quitter" déclenche la séquence `handleQuit` (shutdown UI propre — scope story 5.8 pour séparation service)

3. **AC3 — Fermeture fenêtre via `✕` → quitter l'application avec confirmation**
   **Given** la fenêtre webview est ouverte
   **When** l'utilisateur clique sur le bouton `✕` de la titlebar custom
   **Then** une modal "Voulez-vous quitter Le Voile ?" s'affiche avec deux boutons "Annuler" / "Quitter" et une case à cocher "Ne plus montrer"
   **And** "Annuler" ferme la modal et laisse la webview intacte
   **And** "Quitter" appelle le binding Go `__close()` → `w.Terminate()` → la goroutine retourne avec `quitRequested=true` → `onWebviewClosed(true)` → `u.handleQuit()` → shutdown complet (service `levoile-service`, tunnel, firewall, tray, webview)
   **And** si "Ne plus montrer" était cochée, la préférence est persistée **côté serveur** via `POST /api/ui-prefs {quit_prompt_enabled: false}` → fichier JSON dans `%APPDATA%\LeVoile\ui-prefs.json` (Windows) ou `$XDG_CONFIG_HOME/levoile/ui-prefs.json` (Linux). **Note critique** : `localStorage` webview n'aurait PAS fonctionné car l'HTTP server bind sur `127.0.0.1:0` (port dynamique) — chaque lancement = origin différente = prefs perdues (review H3)
   **And** au prochain démarrage de l'app, le frontend appelle `GET /api/ui-prefs` à l'init → si `quit_prompt_enabled: false`, `✕` quitte directement sans afficher la modal
   **And** la webview n'est **jamais recréée** après destruction (le process n'existe plus). Rationale : la recréation a été testée et produit une fenêtre blanche sur WebView2 (feedback validé 2026-04-17)

4. **AC4 — Linux (libayatana-appindicator) : comportement adapté aux conventions GNOME/KDE**
   **Given** l'UI tourne sur Linux avec `libayatana-appindicator3` (GNOME/KDE/XFCE)
   **When** l'utilisateur interagit avec l'icône tray
   **Then** le clic gauche ET le clic droit ouvrent le menu contextuel (comportement natif appindicator)
   **And** l'entrée "Ouvrir la fenêtre" est la première du menu pour rester naturellement accessible
   **And** le code gate `systray.SetOnTapped(u.handleTrayToggle)` derrière `if runtime.GOOS == "windows"` : fyne.io/systray met `ItemIsMenu=false` dès qu'un tapped callback est défini (systray_unix.go:368), ce qui casse le comportement natif du menu sur appindicator. Gating Windows-only évite ce side-effect (review M1)
   **And** le comportement de `✕` (AC3) et de "Quitter" (AC2) est identique sur les deux OS

5. **AC5 — Raccourci `─` (minimize) reste cohérent avec AC1**
   **Given** la fenêtre webview est ouverte
   **When** l'utilisateur clique sur `─` dans la titlebar
   **Then** la fonction `__minimize()` existante appelle `ShowWindow SW_HIDE` → la fenêtre est cachée sans destruction
   **And** un clic gauche ultérieur sur le tray la remontre (branche "hidden → show" de AC1)
   **And** la webview reste en RAM (pas de recréation), le state frontend est préservé

6. **AC6 — Tests unitaires Go**
   **Given** le package `internal/ui`
   **When** la suite `go test ./internal/ui/...` est exécutée
   **Then** `TestHandleTrayToggle_NoopWhenAbsent` vérifie : `webviewOpen=false` → no-op pur (pas de panic nil-channel, pas de création)
   **And** `TestHandleTrayToggle_ShowsWhenHidden` vérifie : state hidden → signal envoyé sur `showCh`, rien sur `hideCh`
   **And** `TestHandleTrayToggle_HidesWhenVisible` vérifie : state visible → signal envoyé sur `hideCh`, rien sur `showCh`
   **And** `TestHandleTrayToggle_DropsWhenChannelFull` vérifie : canal pré-rempli → `handleTrayToggle` ne bloque pas (select/default)
   **And** `TestOnWebviewClosed_QuitsOnTrueQuit` vérifie : `quit=true` + pas de shutdown en cours → `quitFn` appelé exactement 1 fois (AC3 happy path)
   **And** `TestOnWebviewClosed_NoopOnFalseQuit` vérifie : `quit=false` → `quitFn` jamais appelé
   **And** `TestOnWebviewClosed_NoopWhenShutdownInProgress` vérifie : garde de ré-entrée (clic menu "Quitter" avant `✕`) → `quitFn` non rappelé
   **And** `TestPrefsStore_*` (5 tests) + `TestUIPrefs_*` (4 tests) valident persistance prefs côté serveur (chargement défauts / roundtrip save-load / création dossier / écriture atomique / JSON corrompu / GET défauts / POST persiste + GET relit / POST bad JSON / méthode HTTP non autorisée)
   **And** la suite existante (27+ tests pré-existants) reste verte

## Tasks / Subtasks

- [x] **Task 1 — `✕` → quitter l'application avec confirmation frontend** (AC: #3)
  - [x] 1.1 [internal/ui/webview_cgo_windows.go](internal/ui/webview_cgo_windows.go) + [internal/ui/webview_cgo_linux.go](internal/ui/webview_cgo_linux.go) : binding `__close` → `quitRequested.Store(true)` + `w.Terminate()` ; `openWebview` retourne `quitRequested.Load()` (signature `...) bool`)
  - [x] 1.2 [internal/ui/ui.go](internal/ui/ui.go) `handleOpenWebview` goroutine : après retour de `openWebview`, appel à nouvelle méthode `u.onWebviewClosed(quit)` — gate garde `!shutdownInProgress` et invoque `u.quitFn()` (défaut `handleQuit`) ; extrait en méthode pour testabilité
  - [x] 1.3 Nouveau champ `quitFn func()` dans `UI` struct ; initialisé à `u.handleQuit` par `New`/`newWithDeps` ; overridable pour les tests

- [x] **Task 2 — Toggle clic gauche tray avec 2 états (hidden / visible)** (AC: #1, #5)
  - [x] 2.1 [webview_cgo_windows.go](internal/ui/webview_cgo_windows.go) : nouveau paramètre `hideCh <-chan struct{}` + goroutine dédiée qui appelle `procShowWindow.Call(hwnd, swHide)` sur signal. Boucle `select { <-done / <-hideCh }` fermée via `defer close(done)` — corrige au passage la fuite de goroutine pré-existante
  - [x] 2.2 Callback `reportHidden func(bool)` passé en paramètre — invoqué par `__minimize` (→ true), par le handler `hideCh` (→ true), par le handler `showCh` après `SW_SHOW` (→ false)
  - [x] 2.3 Champ `webviewHidden atomic.Bool` ajouté au `UI` struct ; remis à `false` à chaque `handleOpenWebview` et via `defer` quand la goroutine webview se termine
  - [x] 2.4 Champ `hideCh chan struct{}` ajouté au `UI` struct, créé en paire avec `showCh` dans `handleOpenWebview`, stocké sous `u.mu` ; libéré (nil) par `defer` à la sortie de la goroutine webview
  - [x] 2.5 Méthode `u.handleTrayToggle()` : 2 branches seulement — `webviewOpen=false` → return no-op ; hidden → send `showCh` ; visible → send `hideCh`. Send non-bloquant via `select/default`
  - [x] 2.6 `systray.SetOnTapped(u.handleTrayToggle)` appelé dans [ui.go:onReady](internal/ui/ui.go) **gated par `if runtime.GOOS == "windows"`** pour éviter le side-effect `ItemIsMenu=false` sur appindicator Linux (review M1)
  - [x] 2.7 (bonus correctness) `handleOpenWebview` utilise `CompareAndSwap(false, true)` pour fermer la fenêtre de course entre menu "Ouvrir" et clic gauche tray simultanés

- [x] **Task 3 — Stub webview no-cgo** (build vert sur CI no-cgo)
  - [x] 3.1 [internal/ui/webview_nocgo.go](internal/ui/webview_nocgo.go) : signature alignée sur les 6 paramètres, retour `bool`

- [x] **Task 4 — Handler menu "Ouvrir la fenêtre"** (AC: #2)
  - [x] 4.1 [ui.go:menuHandler](internal/ui/ui.go) : `u.menuOpen.ClickedCh` → `u.handleOpenWebview()` inchangé (ne cache jamais, conforme AC2)
  - [x] 4.2 `handleOpenWebview` branche "déjà ouverte" envoie sur `showCh` : SW_SHOW est un no-op si déjà visible, et amène au premier plan via `SetWindowPos` dans la goroutine webview — comportement correct que la fenêtre soit cachée ou visible

- [x] **Task 5 — Modal de confirmation frontend** (AC: #3)
  - [x] 5.1 [frontend/index.html](frontend/index.html) : `✕` change `onclick="__close()"` → `onclick="confirmQuit()"` ; nouveau `<div class="modal-overlay" id="modal-quit">` avec `modal-text`, `modal-hint`, `modal-checkbox` (input `quit-dont-ask`), boutons Annuler/Quitter
  - [x] 5.2 [frontend/src/app.js](frontend/src/app.js) : fonctions `confirmQuit()` (lit cache `uiPrefs`, affiche modal ou quitte direct), `cancelQuitModal()`, `doQuit()` (async — attend POST prefs avant `__close()` pour éviter race)
  - [x] 5.3 [frontend/src/app.js](frontend/src/app.js) : `loadUIPrefs()` fetch `/api/ui-prefs` à l'init (via `init()`), met à jour le cache `uiPrefs = { quit_prompt_enabled: true }` par défaut ; `saveUIPrefs(partial)` POST la valeur mise à jour
  - [x] 5.4 [frontend/src/style.css](frontend/src/style.css) : styles `.modal-hint` + `.modal-checkbox` avec commentaire pattern documentant la réutilisation

- [x] **Task 6 — Persistance serveur-side des prefs UI** (AC: #3, review H3)
  - [x] 6.1 Nouveau fichier [internal/ui/prefs.go](internal/ui/prefs.go) : `UIPrefs{QuitPromptEnabled bool}`, `PrefsStore` thread-safe avec Load/Save, chemin `%APPDATA%\LeVoile\ui-prefs.json` (Windows) / `$XDG_CONFIG_HOME/levoile/ui-prefs.json` (Linux) via `os.UserConfigDir()`
  - [x] 6.2 Save atomique : write .tmp + rename ; mkdir parent 0755 ; fichier 0600
  - [x] 6.3 Load tolérant : fichier absent → défauts sans erreur ; JSON corrompu → erreur + défauts (safe)
  - [x] 6.4 Endpoint `/api/ui-prefs` dans [internal/ui/httpserver.go](internal/ui/httpserver.go) : GET → retourne JSON ; POST → lit `UIPrefs` JSON (max 1 KiB), persiste, renvoie la valeur
  - [x] 6.5 Tests [internal/ui/prefs_test.go](internal/ui/prefs_test.go) : 5 tests (défauts / roundtrip / création dir / atomic / corrupt)
  - [x] 6.6 Tests [internal/ui/httpserver_test.go](internal/ui/httpserver_test.go) : 4 tests (`TestUIPrefs_GetReturnsDefaultsOnFirstRun`, `TestUIPrefs_PostPersistsAndGetRetrieves`, `TestUIPrefs_PostBadJSON`, `TestUIPrefs_MethodNotAllowed`) via helper `newTestHTTPServerWithPrefs(t)` redirigeant le path vers `t.TempDir()`

- [x] **Task 7 — Tests unitaires Go pour le lifecycle** (AC: #6)
  - [x] 7.1 `TestHandleTrayToggle_NoopWhenAbsent` : `UI` nu (pas de channels, `webviewOpen=false`) → appel ne panique pas et ne signale rien
  - [x] 7.2 `TestHandleTrayToggle_ShowsWhenHidden` : state hidden → signal sur `showCh`, rien sur `hideCh`
  - [x] 7.3 `TestHandleTrayToggle_HidesWhenVisible` : state visible → signal sur `hideCh`, rien sur `showCh`
  - [x] 7.4 `TestHandleTrayToggle_DropsWhenChannelFull` : `hideCh` pré-rempli ; appel dans goroutine avec timeout 200ms — ne doit pas bloquer
  - [x] 7.5 `TestOnWebviewClosed_QuitsOnTrueQuit` : `quitFn` injecté comme compteur → exactement 1 appel quand `quit=true`
  - [x] 7.6 `TestOnWebviewClosed_NoopOnFalseQuit` : `quit=false` → 0 appel
  - [x] 7.7 `TestOnWebviewClosed_NoopWhenShutdownInProgress` : `shutdownInProgress=true` → 0 appel (garde de ré-entrée)
  - [x] 7.8 Suite complète `go test ./...` verte sans régression

- [ ] **Task 8 — Vérification manuelle Windows** (AC: #1, #2, #3, #5) — _reste à exécuter par {user_name}_
  - [ ] 8.1 Build : `go build -tags cgo -o levoile-ui.exe ./cmd/ui`
  - [ ] 8.2 Démarrer `levoile-service` puis `levoile-ui.exe` → tray apparaît, webview s'ouvre automatiquement
  - [ ] 8.3 Clic `─` → fenêtre cachée, tray persiste. Clic gauche tray → fenêtre réapparaît au premier plan
  - [ ] 8.4 Clic gauche tray (fenêtre visible) → fenêtre se cache. Re-clic gauche → réapparaît
  - [ ] 8.5 Clic `✕` 1ère fois → modal "Voulez-vous quitter Le Voile ?" avec Annuler/Quitter + case "Ne plus montrer". Annuler → modal se ferme, webview intacte
  - [ ] 8.6 `✕` → cocher "Ne plus montrer" + Quitter → shutdown complet. Redémarrer l'app → `✕` → quitte directement sans modal (preuve persistance via `%APPDATA%\LeVoile\ui-prefs.json`)
  - [ ] 8.7 Clic droit tray → menu avec "Ouvrir la fenêtre" + "Quitter". "Quitter" → shutdown sans modal de confirmation (comportement différent de `✕` par design, review L1)
  - [ ] 8.8 Clics rapides sur tray (6-8 consécutifs) → aucun doublon, aucun panic

- [ ] **Task 9 — Vérification manuelle Linux (déférable après packaging Epic 7)** (AC: #4)
  - [ ] 9.1 Sur GNOME 45+ ou KDE Plasma 6 : le `systray.SetOnTapped` n'est pas appelé (gaté `runtime.GOOS=="windows"`) → clic gauche ouvre le menu natif appindicator
  - [ ] 9.2 Menu appindicator → "Ouvrir la fenêtre" + "Quitter" présents
  - [ ] 9.3 `✕` ouvre la modal de confirmation ; persistance prefs dans `$XDG_CONFIG_HOME/levoile/ui-prefs.json`
  - [ ] 9.4 `levoile.service` (systemd) s'arrête proprement via `handleQuit` quand l'utilisateur confirme Quitter

## Dev Notes

### Architecture actuelle (état 2026-04-17, branche `main`)

Le binaire `levoile-ui` (un seul processus) combine :
- [internal/ui/ui.go](internal/ui/ui.go) — fyne.io/systray v1.12.0 sur main thread via `systray.Run(onReady, onExit)`
- [internal/ui/webview_cgo.go](internal/ui/webview_cgo.go) — `webview/webview_go` (WebView2 Windows, WebKitGTK Linux) créée à la demande dans une goroutine
- [internal/ui/httpserver.go](internal/ui/httpserver.go) — serveur HTTP local sur `127.0.0.1:<port>` qui sert le frontend embarqué et proxy `/api/*` vers le service via IPC
- [internal/ui/singleton_windows.go](internal/ui/singleton_windows.go) / [internal/ui/singleton_stub.go](internal/ui/singleton_stub.go) — mutex nommé (Windows) / flock (Linux à venir)

### Point central à corriger : le sens du `✕`

**Comportement actuel** (hérité story 10-3, pré-Epic 5 cross-platform) :
- `✕` → binding JS `__close()` → `w.Terminate()` → goroutine retourne avec `quit=true` → [ui.go:200-202](internal/ui/ui.go#L200-L202) appelle `u.handleQuit()` → **full shutdown incluant `ipc.ActionQuit` envoyé au service**.
- Résultat : cliquer `✕` **tue aussi le service, détruit le tunnel, restaure le proxy**. Le kill-switch firewall est retiré. La protection disparaît.

**Comportement attendu (story 5.5 AC3 + story 5.8)** :
- `✕` → `__close()` → webview `Terminate()` → goroutine retourne → `webviewOpen=false`, **rien d'autre**.
- Le service reste vivant, le tunnel reste connecté, le firewall reste actif, le tray reste visible.
- L'utilisateur doit explicitement cliquer "Quitter" dans le menu tray (clic droit) pour arrêter l'UI. Et même dans ce cas, story 5.8 précisera que le service survit (pas de `ActionQuit` — simple notification IPC). Mais **5.5 ne touche PAS à la sémantique de `handleQuit`** — c'est 5.8 qui la redéfinira. Il suffit à 5.5 de déconnecter `✕` du chemin `handleQuit`.

Ce changement corrige aussi l'UX spec lignes 488-491 (`ux-design-specification.md` rev 2026-03-16) qui disait : _"✕ déconnecte le VPN et quitte Le Voile"_. L'Epic 5 refondu (2026-04-15) **redéfinit** explicitement cette sémantique : `✕` = ferme la fenêtre uniquement. Le texte du dialogue de confirmation _"Votre connexion ne sera plus protégée"_ est **obsolète et doit être retiré** s'il existe encore dans `frontend/` (grep `confirm.*d[eé]connecte` lors de l'implémentation).

### API fyne.io/systray v1.12.0 — support clic gauche/droit

Vérifié dans `C:\Users\Akerimus\go\pkg\mod\fyne.io\systray@v1.12.0\systray.go` :

```go
func SetOnTapped(f func())          // clic gauche — ligne 152
func SetOnSecondaryTapped(f func()) // clic droit/middle — ligne 156
```

- **Windows** ([systray_windows.go:329-332](file:///c/Users/Akerimus/go/pkg/mod/fyne.io/systray@v1.12.0/systray_windows.go)) : `WM_LBUTTONUP` → appelle `tappedLeft` si défini, sinon affiche le menu. `WM_RBUTTONUP` → `tappedRight` ou menu par défaut. ⇒ `SetOnTapped(u.handleTrayToggle)` fonctionne.
- **Linux (appindicator/StatusNotifierItem)** ([systray_unix.go:368](file:///c/Users/Akerimus/go/pkg/mod/fyne.io/systray@v1.12.0/systray_unix.go)) : la propriété `ItemIsMenu` passe à `false` si `tappedLeft != nil || tappedRight != nil`. MAIS `libayatana-appindicator` et KDE n'appellent pas toujours le signal `Activate` de façon fiable — le comportement dépend du shell. ⇒ Ne PAS s'appuyer dessus pour Linux. Documenter en AC4 comme limitation protocolaire.
- **macOS** : différé post-MVP.

**Thread-safety** : `SetOnTapped` affecte une variable de package (`tappedLeft`). L'appeler AVANT `systray.Run()` garantit la cohérence, mais `onReady` s'exécute dans la goroutine `systray.Run` — acceptable pour ce cas d'usage (callback affecté une seule fois).

### État webview — 3 états à distinguer

| `webviewOpen` | `webviewHidden` | Signification | Action du clic gauche tray |
|---|---|---|---|
| `false` | — | Fenêtre détruite (après `✕` ou jamais créée) | `handleOpenWebview()` crée |
| `true` | `true` | Minimisée via `─` ou cachée via toggle | `showCh <- {}` + SetFocus |
| `true` | `false` | Visible au premier plan | `hideCh <- {}` (nouveau) |

Le callback `reportHidden` (passé à `openWebview`) met à jour `webviewHidden`. Il DOIT être appelé :
- À `true` quand `__minimize` invoque `SW_HIDE`
- À `true` quand un signal `hideCh` est consommé (AC1 branche hide)
- À `false` quand un signal `showCh` est consommé et `SW_SHOW` réussit
- À `false` à l'initialisation (fenêtre créée et affichée)

### Pattern de canaux (anti-blocage)

```go
func (u *UI) handleTrayToggle() {
    if !u.webviewOpen.Load() {
        u.handleOpenWebview()
        return
    }
    var ch chan struct{}
    if u.webviewHidden.Load() {
        ch = u.showCh
    } else {
        ch = u.hideCh
    }
    select {
    case ch <- struct{}{}:
    default: // canal déjà rempli — coalesce
    }
}
```

**Invariant** : `showCh` et `hideCh` sont buffered(1), créés à chaque `handleOpenWebview` et fermés/remplacés à la destruction. La goroutine webview consomme jusqu'à fermeture de la goroutine parente.

### Fichiers concernés (cartographie)

| Fichier | Action | Motif |
|---|---|---|
| [internal/ui/ui.go](internal/ui/ui.go) | MODIFIER | `onReady` : ajouter `SetOnTapped` ; retirer le `handleQuit` sur close webview ; ajouter `handleTrayToggle`, `hideCh`, `webviewHidden` |
| [internal/ui/webview_cgo.go](internal/ui/webview_cgo.go) | MODIFIER | Signature `openWebview` : accepter `hideCh` + `reportHidden` ; ne plus retourner `quitRequested` (toujours false) ; goroutine d'écoute `hideCh` |
| [internal/ui/webview_nocgo.go](internal/ui/webview_nocgo.go) | MODIFIER | Aligner la signature stub |
| [internal/ui/ui_test.go](internal/ui/ui_test.go) | MODIFIER | +5 tests (Task 5) |
| [frontend/index.html](frontend/index.html) | INCHANGÉ | Les bindings `__minimize`/`__close` existent déjà |
| [frontend/src/app.js](frontend/src/app.js) | INCHANGÉ | Aucun appel front-side à modifier |
| [internal/ui/httpserver.go](internal/ui/httpserver.go) | INCHANGÉ | Pas d'API REST impactée |
| [internal/ipchandler/handler.go](internal/ipchandler/handler.go) | INCHANGÉ | Aucun nouveau message IPC |

### Fichiers à NE PAS toucher (scope story 5.5)

- `internal/service/`, `internal/tunnel/`, `internal/firewall/`, `internal/tun/` — aucune impact service
- `internal/ipc/messages.go` — pas de nouvelle action (la notification « UI quit » relève de story 5.8)
- `internal/ui/sysproxy_*.go` — la restauration proxy reste liée au `handleQuit` pour l'instant (5.8 redéfinira)

### Threading — rappel critique

- `systray.Run` bloque le main thread (obligatoire sur Linux X11 et recommandé partout)
- `webview.Run` DOIT être appelé sur le thread Go qui a appelé `webview.New` (pin par `runtime.LockOSThread` via la lib) — actuellement lancé dans une goroutine dans `handleOpenWebview`, ce qui fonctionne sur Windows ; sur Linux GTK le comportement doit être vérifié manuellement (Task 7)
- `SetOnTapped` (systray) est appelé par le message loop natif (WindowProc sur Windows) — **NE PAS** appeler de fonction bloquante dedans. `handleTrayToggle` fait uniquement `Load` atomique + send non-bloquant sur canal : ✅ sûr
- `handleOpenWebview` lance une goroutine puis retourne immédiatement : ✅ sûr

### Ce que la story NE fait PAS (scope boundary)

- Ne touche pas à la séparation UI-quit / service-quit → **Story 5.8**
- Ne modifie pas le fallback "Service non démarré" → **Story 5.6**
- Ne touche pas au supervision auto-restart → **Story 5.7**
- Ne modifie pas le bouton Connect/Disconnect ou le polling frontend → couvert par 5.4 / 5.1
- Ne porte pas Linux appindicator si `libayatana-appindicator3` est absent (ex. Wayland/sway sans xdg-desktop-portal) — fallback diagnostic noté comme "limitation OS", hors scope

### Previous Story Intelligence — Story 10-3 (ancien Epic 10, DONE)

La story 10-3 est la source directe du code tray+webview actuel. Points à retenir :

1. **Pattern de polling indépendant** : tray poll IPC (2s) + frontend poll `/api/status` (2s), pas de canal direct. Cette story 5.5 N'INTRODUIT pas de canal direct — seulement des canaux webview-internes (show/hide) pour le toggle.
2. **Bouton fermeture X = full shutdown** : c'est exactement ce que 5.5 corrige (cf. section "Point central à corriger").
3. **Learnings 10-2 à respecter** :
   - Zero-log Go (aucun `log.Println`/`fmt.Println` dans internal/ui)
   - Erreurs wrappées : `fmt.Errorf("ui: %w", err)`
   - `const` vs `var` respecté en JS (pas de JS nouveau ici)
   - Zone cliquable tactile ≥ 44×44px (pas de nouveau bouton UI ici — les `─`/`✕` existants sont stylés en story 10-3)
4. **Context timeout 5s** pour les appels IPC — pas de nouvel IPC ici, mais respecter le pattern si introduit
5. **Tests existants** : 27/27 verts dans `internal/ui` (commit `4d9b1cb`). Les 5 nouveaux tests doivent s'ajouter sans casser les existants.

### Git Intelligence (5 derniers commits pertinents UI)

```
b855d2a fix: minimize-to-tray, webview cold start, fast registry polling  ← pose les fondations __minimize
61ac539 docs: update specs for minimize-to-tray, no disconnect, webview hide/show
4b3e3a2 feat: refonte UI desktop, fix WebRTC Chrome 146, registre relay, icones
e4e6048 refactor: remove disconnect button, delete dead cmd/tray and cmd/desktop, cleanup
4d9b1cb feat(ui): story 10-3 connect/disconnect button + tray-webview integration  ← ajoute handleQuit sur close
```

Le commit `b855d2a` a introduit `__minimize` (hide-to-tray) mais N'A PAS aligné `__close`. La présente story 5.5 achève la refonte initiée par `b855d2a`.

### Bibliothèques et versions

| Bibliothèque | Version | Rôle dans cette story |
|---|---|---|
| `fyne.io/systray` | v1.12.0 | `SetOnTapped` / menu items — API vérifiée dans le module cache |
| `webview/webview_go` | v0.0.0-20240831120633 | Webview Windows/Linux — binding `__close`/`__minimize` et API `Terminate`/`Destroy` |
| `golang.org/x/sys/windows` | (déjà importé) | `ShowWindow SW_HIDE/SW_SHOW`, `SetWindowPos` pour toggle visibilité |
| Go `testing` | stdlib | Tests unitaires |
| Go `sync/atomic` | stdlib | `atomic.Bool` pour `webviewOpen`, `webviewHidden`, `shutdownInProgress` |

### Latest Tech Information

- **WebView2 Runtime** (Windows 10/11) : présent par défaut sur Windows 10 22H2+ et Windows 11. Pas de changement depuis le cut-off 2026-01. `webview/webview_go` dernière release 20240831 reste compatible.
- **WebKitGTK 6.0** (Linux GNOME 45+) : API stable depuis 2023, pas de breaking change connu. Packaging `webkit2gtk-6.0` via `.deb`/`.rpm` couvert par story 7.2.
- **libayatana-appindicator3** : widely packaged on Debian 12, Ubuntu 24.04, Fedora 40, Arch. Fallback si absent = systray invisible → erreur attendue, scope packaging (story 7.2).
- Aucun CVE pertinent publié sur fyne.io/systray depuis v1.11.0 (vérification rapide : https://pkg.go.dev/fyne.io/systray?tab=versions).

### Conflits détectés avec la documentation existante

1. **UX spec lignes 488-491 vs Story 5.5 AC3** : l'UX spec (rev 2026-03-16) disait _"✕ déconnecte le VPN et quitte Le Voile"_. L'Epic 5 refondu (sprint-status 2026-04-15 — `chore: remove .claude/ from history, harden deploy scripts` + restructure) **redéfinit** : `✕` = ferme la fenêtre. **Résolution** : l'Epic et la story 5.5 font autorité (plus récents + sprint-status révisé). Mettre à jour `ux-design-specification.md` sera une tâche de clean-up post-Epic 5 (hors scope story).
2. **Story 10-3 AC4** (done, archive) : _"X = quitter Le Voile (shutdown complet)"_. Cette story 5.5 **renverse** explicitement ce AC. Pas d'action requise sur 10-3 (statut `done`), mais le grep `quitRequested` dans le code doit revenir vide après implémentation de la Task 1.

### Project Structure Notes

- Alignement structure unifiée : tous les fichiers modifiés sont dans `internal/ui/` (package unique) — conforme à la directive archi _"Fusion internal/tray/ + internal/desktop/ → internal/ui/"_ (architecture.md ligne 1285)
- Nommage Go : `snake_case.go` pour les fichiers (`webview_cgo.go`, `webview_nocgo.go` ✅). Erreurs wrappées `fmt.Errorf("ui: %w", err)` (architecture.md §Enforcement Guidelines). Pas de nouveau fichier nécessaire.
- Pas de conflit détecté avec la structure arborescente projet.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#L961-L984 — Epic 5, Story 5.5]
- [Source: _bmad-output/planning-artifacts/architecture.md#L624-L645 — UI Patterns webview/systray, Lifecycle]
- [Source: _bmad-output/planning-artifacts/architecture.md#L311-L317 — levoile-ui 2 processus Tray+Webview]
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#L337-L338 — Clic gauche/droit tray]
- [Source: _bmad-output/planning-artifacts/prd.md — FR13 (toggle fenêtre clic gauche), FR13b (quitter clic droit)]
- [Source: _bmad-output/implementation-artifacts/10-3-bouton-connect-disconnect-et-integration-tray-webview.md — previous story intelligence]
- [Source: internal/ui/ui.go#L120-L167 — onReady, menuHandler, handleOpenWebview, handleQuit actuels]
- [Source: internal/ui/webview_cgo.go#L100-L174 — openWebview, bindings __close/__minimize, showCh]
- [Source: fyne.io/systray@v1.12.0/systray.go#L152-L158 — SetOnTapped, SetOnSecondaryTapped]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context)

### Debug Log References

Aucun blocage rencontré. `go vet ./...` vert. `go test ./...` : toutes les suites passent, y compris les 5 nouveaux tests de `internal/ui`.

### Implementation Plan

Trois axes orthogonaux, implémentés dans cet ordre :

1. **Découpler `✕` de l'arrêt app** — retirer le retour `bool` de `openWebview` et le `handleQuit` consécutif dans `handleOpenWebview`. Le binding JS `__close` se limite désormais à `w.Terminate()`.
2. **Étendre `openWebview` pour supporter le toggle 3-états** — nouveaux paramètres `hideCh <-chan struct{}` + `reportHidden func(bool)` ; goroutines d'écoute `showCh`/`hideCh` nettoyées via `defer close(done)` (corrige au passage la fuite de goroutine que la version précédente avait à chaque cycle open/close).
3. **Ajouter `handleTrayToggle`** — trois branches (absent/hidden/visible), sends non-bloquants via `select/default`, et `systray.SetOnTapped(u.handleTrayToggle)` branché dans `onReady`. Injection de `trayOpenFn` pour pouvoir tester la branche "absent" sans dépendance au serveur HTTP.

Détail bonus : `handleOpenWebview` passe à `CompareAndSwap(false, true)` pour éliminer la course entre un clic gauche tray et un clic menu "Ouvrir" simultanés.

### Completion Notes List

- **Tasks 1–5 complètes.** Les 5 nouveaux tests `TestHandleTrayToggle_*` + `TestCloseDoesNotQuitApp` passent. La suite complète du projet (`go test ./...`) reste verte, y compris les 27 tests pré-existants de `internal/ui`.
- **Tasks 6 et 7 (vérification manuelle) restent ouvertes** car elles exigent un environnement desktop réel (Windows WebView2 + tray Win32 pour Task 6, GNOME/KDE + libayatana-appindicator pour Task 7). Les tests unitaires valident la logique de toggle et la décorrélation close/quit ; la validation visuelle WinAPI / appindicator est à exécuter par {user_name}.
- **Bonus qualité** : correction de la fuite de goroutine qui existait dans le `for range showCh` de la version précédente (une goroutine orpheline par cycle open/close). La version refactorée utilise `defer close(done)` pour terminer proprement les deux listeners show/hide.
- **Build vert multi-target** : `go build ./internal/ui/... ./cmd/ui/...` + `go vet ./...` sans warning. Signature de `openWebview` harmonisée entre `webview_cgo_windows.go`, `webview_cgo_linux.go` et `webview_nocgo.go`.
- **Aucun secret, aucune dépendance ajoutée.** Les seules lib utilisées sont déjà présentes dans `go.mod` (`fyne.io/systray v1.12.0`, `webview/webview_go`, `golang.org/x/sys/windows`).

### File List

**Backend Go :**

- `internal/ui/ui.go` — MODIFIÉ — import `runtime` ; champs `webviewHidden atomic.Bool`, `hideCh chan struct{}`, `quitFn func()` ajoutés au `UI` struct ; `New`/`newWithDeps` initialisent `quitFn = u.handleQuit` ; `onReady` binde `systray.SetOnTapped(u.handleTrayToggle)` gaté `runtime.GOOS=="windows"` ; `handleOpenWebview` utilise `CompareAndSwap`, crée `showCh`+`hideCh` en paire sous `u.mu`, `defer` nettoie channels à `nil`, `defer` passe `reportHidden` ; nouvelle méthode `handleTrayToggle` (2 branches) ; nouvelle méthode `onWebviewClosed(quit bool)` extraite pour testabilité (garde `shutdownInProgress` + invoque `quitFn`)
- `internal/ui/webview_cgo_windows.go` — MODIFIÉ — signature `openWebview(...) bool` restaurée ; `__close` → `quitRequested.Store(true) + w.Terminate()` ; `__minimize` wrappé dans `w.Dispatch(...)` pour cohérence avec les goroutines show/hide ; nouveaux paramètres `hideCh <-chan struct{}`, `reportHidden func(bool)` ; 2 goroutines listener show/hide nettoyées via `defer close(done)` ; retour `quitRequested.Load()`
- `internal/ui/webview_cgo_linux.go` — MODIFIÉ — signature alignée avec retour `bool` ; `showCh`/`hideCh`/`reportHidden` ignorés intentionnellement (limitation OS documentée AC4) ; `quitRequested` restauré
- `internal/ui/webview_nocgo.go` — MODIFIÉ — stub signature 6 paramètres + retour `bool` (false)
- `internal/ui/prefs.go` — **NOUVEAU** — `UIPrefs{QuitPromptEnabled bool}` + `PrefsStore` thread-safe (Load/Save atomique via `.tmp`+rename, mkdir parent, fallback défauts sur fichier absent/corrompu). Chemin via `os.UserConfigDir() + "LeVoile/ui-prefs.json"`
- `internal/ui/httpserver.go` — MODIFIÉ — champ `prefs *PrefsStore` ajouté à `HTTPServer` ; `NewHTTPServer` l'initialise ; nouveau handler `handleUIPrefs` enregistré sur `/api/ui-prefs` (GET renvoie prefs, POST valide+persiste, 405 sur autre méthode)

**Tests Go :**

- `internal/ui/ui_test.go` — MODIFIÉ — 4 tests `TestHandleTrayToggle_*` (NoopWhenAbsent, ShowsWhenHidden, HidesWhenVisible, DropsWhenChannelFull) + 3 tests `TestOnWebviewClosed_*` (QuitsOnTrueQuit, NoopOnFalseQuit, NoopWhenShutdownInProgress) ; imports nettoyés (pas de `sync/atomic` ni `fyne.io/systray` supplémentaires)
- `internal/ui/prefs_test.go` — **NOUVEAU** — 5 tests (`LoadMissingReturnsDefaults`, `SaveThenLoadRoundtrip`, `SaveCreatesMissingDir`, `SaveAtomic`, `LoadCorruptReturnsDefaults`)
- `internal/ui/httpserver_test.go` — MODIFIÉ — 4 tests `TestUIPrefs_*` (Get défauts, POST persiste+GET relit, bad JSON, method not allowed) + helper `newTestHTTPServerWithPrefs(t)` redirigeant `prefs.path` vers `t.TempDir()`

**Frontend :**

- `frontend/index.html` — MODIFIÉ — `✕` : `onclick="__close()"` → `onclick="confirmQuit()"` ; nouveau `<div class="modal-overlay" id="modal-quit">` avec titre, hint "Votre protection sera interrompue", case `<input type="checkbox" id="quit-dont-ask">`, boutons Annuler/Quitter
- `frontend/src/app.js` — MODIFIÉ — cache `uiPrefs = { quit_prompt_enabled: true }` ; `loadUIPrefs()` fetch `/api/ui-prefs` à l'init ; `saveUIPrefs(partial)` POST la valeur ; `confirmQuit()` consulte le cache (pas localStorage — review H3) ; `cancelQuitModal()`, `doQuit()` async qui attend le POST avant `__close()` pour éviter la race write→exit
- `frontend/src/style.css` — MODIFIÉ — nouveaux styles `.modal-hint` + `.modal-checkbox` avec commentaire pattern réutilisable

**Artefacts projet :**

- `_bmad-output/implementation-artifacts/sprint-status.yaml` — MODIFIÉ — `5-5-...-clic-droit` : `ready-for-dev` → `in-progress` → `review`

### Change Log

- 2026-04-17 (initial) — Story 5.5 première implémentation : toggle 3-états (absent/hidden/visible) sur clic gauche tray via `systray.SetOnTapped` ; décorrélation `✕` / arrêt app ; callback `reportHidden` pour synchroniser l'état webview→UI ; goroutines listener nettoyées via `defer close(done)`. +5 tests unitaires, suite verte sans régression.
- 2026-04-17 (revision post-test user) — **Renversement sémantique `✕`** : test manuel sur Windows a montré que recréer la webview après destruction produit une fenêtre blanche (bug WebView2). Comportement corrigé : `✕` → quitter l'app (avec modal de confirmation "Voulez-vous quitter Le Voile ?" + case "Ne plus montrer") ; `handleTrayToggle` simplifié à 2 branches (pas de recréation après destroy) ; feedback persisté en mémoire `feedback_webview_lifecycle.md`.
- 2026-04-17 (revision post-code-review) — **10 findings adressés** (1 critique, 3 hauts, 2 moyens, 4 bas) :
  - **C1/H1/H2** — Story réalignée : AC1 (2 états au lieu de 3), AC6 (tests mis à jour), File List régénérée avec les 3 fichiers frontend et les 2 nouveaux fichiers Go (`prefs.go`, `prefs_test.go`), tasks réécrites pour refléter le code réel.
  - **H3** — Persistance des prefs déplacée de `localStorage` (perdue à chaque relance car port HTTP dynamique) vers `%APPDATA%\LeVoile\ui-prefs.json` via nouveau endpoint `/api/ui-prefs` (GET+POST). Nouveau fichier `internal/ui/prefs.go` + 9 tests.
  - **M1** — `systray.SetOnTapped` gaté `runtime.GOOS=="windows"` pour éviter le side-effect `ItemIsMenu=false` cassant le menu appindicator sur Linux.
  - **M2** — Méthode `onWebviewClosed(quit bool)` extraite + champ `quitFn` injectable ; 3 nouveaux tests couvrant la chaîne `✕ → quit` (happy path + 2 gardes).
  - **L1** — Inconsistence intentionnelle documentée : "Quitter" menu clic droit → direct, `✕` → modal (réduit la friction répétée sur la sortie explicite).
  - **L2** — `__minimize` wrappé dans `w.Dispatch(...)` pour cohérence avec les goroutines show/hide.
  - **L3** — Canaux `showCh`/`hideCh` mis à `nil` sous `u.mu` au `defer` final de la goroutine webview (libération mémoire + noop naturel pour `handleTrayToggle` après quit).
  - **L4** — Commentaire CSS au-dessus de `.modal-hint`/`.modal-checkbox` documentant le pattern réutilisable pour les futures modales.
  - **Suite tests** : 27+ pré-existants + 7 nouveaux Go (`internal/ui`) = tout vert, aucune régression.

