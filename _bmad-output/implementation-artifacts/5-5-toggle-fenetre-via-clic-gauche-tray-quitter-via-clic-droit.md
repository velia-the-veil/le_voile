# Story 5.5 : Toggle fenêtre via clic gauche tray + Quitter via clic droit

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur final,
je veux ouvrir/fermer la fenêtre rapidement via un clic gauche sur l'icône tray, et quitter via le menu clic droit,
afin que l'accès soit naturel et conforme aux conventions desktop.

## Acceptance Criteria

1. **AC1 — Clic gauche sur l'icône tray (Windows) → toggle fenêtre webview**
   **Given** l'icône tray `levoile-ui` est visible dans la barre des tâches
   **When** l'utilisateur fait un clic gauche sur l'icône
   **Then** si la fenêtre webview n'existe pas → elle est créée et affichée en position bas-droite (420×540 frameless)
   **And** si la fenêtre existe mais est cachée (minimisée via `─`) → elle est montrée (`ShowWindow SW_SHOW`) et amenée au premier plan (`SetWindowPos HWND_TOP` + focus)
   **And** si la fenêtre est déjà visible au premier plan → elle est cachée (`ShowWindow SW_HIDE`) → le tray persiste, la protection reste active
   **And** le toggle est thread-safe (pas de doublon en cas de clics rapides)

2. **AC2 — Menu contextuel via clic droit (Windows + Linux)**
   **Given** l'icône tray est visible
   **When** l'utilisateur fait un clic droit dessus
   **Then** le menu contextuel fyne.io/systray apparaît avec au minimum les entrées : "Ouvrir la fenêtre" et "Quitter"
   **And** sélectionner "Ouvrir la fenêtre" équivaut exactement à un clic gauche sur une fenêtre cachée/absente (create-or-show, jamais hide)
   **And** sélectionner "Quitter" déclenche la séquence `handleQuit` (shutdown UI propre — scope story 5.8 pour séparation service)

3. **AC3 — Fermeture fenêtre via `✕` → destruction webview uniquement**
   **Given** la fenêtre webview est ouverte
   **When** l'utilisateur clique sur le bouton `✕` de la titlebar custom
   **Then** la fonction JS `__close()` est invoquée
   **And** la webview appelle `w.Terminate()` puis `w.Destroy()` → la mémoire est libérée
   **And** le flag `webviewOpen` repasse à `false`, le tray persiste, le processus `levoile-ui` continue
   **And** le service `levoile-service` n'est PAS arrêté (pas d'IPC `ActionQuit`), le tunnel reste connecté, le kill-switch firewall reste actif
   **And** un clic gauche ultérieur sur le tray recrée une fenêtre saine

4. **AC4 — Linux (libayatana-appindicator) : comportement adapté aux conventions GNOME/KDE**
   **Given** l'UI tourne sur Linux avec `libayatana-appindicator3` (GNOME/KDE/XFCE)
   **When** l'utilisateur interagit avec l'icône tray
   **Then** le clic gauche ET le clic droit ouvrent le menu contextuel (limitation protocolaire StatusNotifierItem ; `systray.SetOnTapped` n'est pas livré fiablement par appindicator)
   **And** l'entrée "Ouvrir la fenêtre" est la première du menu pour rester naturellement accessible
   **And** le code utilise `systray.SetOnTapped(u.handleTrayToggle)` + `systray.SetOnSecondaryTapped(nil)` : actif sur Windows, no-op silencieux sur Linux
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
   **Then** un nouveau test `TestHandleTrayToggle_CreatesWhenAbsent` vérifie : `webviewOpen=false` → `handleOpenWebview` appelé
   **And** `TestHandleTrayToggle_ShowsWhenHidden` vérifie : `webviewOpen=true` + state hidden → signal envoyé sur `showCh`
   **And** `TestHandleTrayToggle_HidesWhenVisible` vérifie : state visible → signal envoyé sur `hideCh` (nouveau canal)
   **And** `TestCloseDoesNotQuitApp` vérifie : `__close` déclenche `Terminate` mais `handleQuit` N'EST PAS appelé
   **And** la suite existante (27 tests actuels) reste verte

## Tasks / Subtasks

- [ ] **Task 1 — Séparer « close webview » de « quit app »** (AC: #3)
  - [ ] 1.1 Dans [internal/ui/webview_cgo.go](internal/ui/webview_cgo.go) : conserver le binding `__close` mais supprimer la sémantique "quit" (supprimer `quitRequested` ou le conserver en local webview)
  - [ ] 1.2 Dans [internal/ui/ui.go:199-203](internal/ui/ui.go#L199-L203) : supprimer le `if quit && !u.shutdownInProgress.Load() { u.handleQuit() }` → la goroutine webview se termine simplement, `webviewOpen.Store(false)` via le `defer` déjà présent
  - [ ] 1.3 Adapter `openWebview` pour ne plus retourner `bool` (quitRequested devient inutile), ou toujours retourner `false`

- [ ] **Task 2 — Toggle clic gauche tray avec 3 états (absent / hidden / visible)** (AC: #1, #5)
  - [ ] 2.1 Dans [internal/ui/webview_cgo.go](internal/ui/webview_cgo.go) : ajouter un canal `hideCh <-chan struct{}` en paramètre de `openWebview`, côté webview une goroutine qui écoute et appelle `procShowWindow.Call(hwnd, swHide)` sur signal
  - [ ] 2.2 Dans [internal/ui/webview_cgo.go](internal/ui/webview_cgo.go) : ajouter un état atomique `isHidden atomic.Bool` dans la fonction — mis à `true` par `__minimize` et par le signal hideCh, remis à `false` par le signal showCh après `SW_SHOW`. Exposer via une fonction callback passée en paramètre (`reportHidden func(bool)`)
  - [ ] 2.3 Dans [internal/ui/ui.go](internal/ui/ui.go) : ajouter champ `webviewHidden atomic.Bool` dans `UI` struct ; capturer l'état via le callback
  - [ ] 2.4 Ajouter champ `hideCh chan struct{}` (buffer 1) dans `UI` struct, créé en même temps que `showCh` dans `handleOpenWebview`
  - [ ] 2.5 Ajouter méthode `u.handleTrayToggle()` :
    ```
    if !u.webviewOpen.Load() → u.handleOpenWebview()  // crée
    else if u.webviewHidden.Load() → send(u.showCh)    // montre
    else → send(u.hideCh)                               // cache
    ```
    Utiliser `select { case ch <- struct{}{}: default: }` pour non-blocking
  - [ ] 2.6 Dans [internal/ui/ui.go:onReady](internal/ui/ui.go#L120) : ajouter `systray.SetOnTapped(u.handleTrayToggle)` après `SetIcon/SetTooltip/SetTitle` et avant l'ajout des menu items

- [ ] **Task 3 — Stub webview no-cgo** (AC: #6 build vert sur CI no-cgo)
  - [ ] 3.1 Dans [internal/ui/webview_nocgo.go](internal/ui/webview_nocgo.go) : adapter la signature de `openWebview` pour accepter les nouveaux paramètres (`hideCh`, `reportHidden`) et conserver le comportement stub existant

- [ ] **Task 4 — Handler menu "Ouvrir la fenêtre"** (AC: #2)
  - [ ] 4.1 Dans [internal/ui/ui.go:menuHandler](internal/ui/ui.go#L156) : garder `u.menuOpen.ClickedCh` → `u.handleOpenWebview()` (pas `handleTrayToggle` car le menu ne doit jamais cacher — AC2 "équivaut à un clic gauche sur fenêtre cachée/absente")
  - [ ] 4.2 Pour les cas où la fenêtre est visible et que l'utilisateur clique "Ouvrir" via clic droit : `handleOpenWebview` détecte `webviewOpen && !webviewHidden` → envoie un signal `showCh` qui re-foreground (no-op si déjà au premier plan)

- [ ] **Task 5 — Tests unitaires Go** (AC: #6)
  - [ ] 5.1 Dans [internal/ui/ui_test.go](internal/ui/ui_test.go) : `TestHandleTrayToggle_CreatesWhenAbsent` — construire `UI` avec `webviewOpen=false`, mocker `handleOpenWebview` via un compteur ou extraire en callback injectable ; vérifier appel
  - [ ] 5.2 `TestHandleTrayToggle_ShowsWhenHidden` — `webviewOpen=true, webviewHidden=true`, `showCh` buffered(1) ; vérifier que `showCh` reçoit et que `hideCh` reste vide
  - [ ] 5.3 `TestHandleTrayToggle_HidesWhenVisible` — `webviewOpen=true, webviewHidden=false` ; vérifier `hideCh` reçoit
  - [ ] 5.4 `TestHandleTrayToggle_DropsWhenChannelFull` — remplir `showCh` ; vérifier que `handleTrayToggle` ne bloque pas (select/default)
  - [ ] 5.5 `TestCloseDoesNotQuitApp` — tester que la goroutine `handleOpenWebview` ne déclenche PAS `handleQuit` quand `openWebview` retourne (simulé avec stub). Mocker `menuAPI.Quit` et vérifier qu'il n'est pas appelé
  - [ ] 5.6 S'assurer que tous les tests existants (27) passent sans régression

- [ ] **Task 6 — Vérification manuelle Windows** (AC: #1, #2, #3, #5)
  - [ ] 6.1 Build : `go build -tags cgo -o levoile-ui.exe ./cmd/ui` (WebView2 + webview_go)
  - [ ] 6.2 Démarrer `levoile-service` puis `levoile-ui.exe` → tray V gris apparaît, webview s'ouvre automatiquement (comportement existant au démarrage)
  - [ ] 6.3 Clic `─` → fenêtre cachée, tray persiste. Clic gauche tray → fenêtre réapparaît au premier plan
  - [ ] 6.4 Clic gauche tray (fenêtre visible) → fenêtre se cache. Re-clic gauche → réapparaît
  - [ ] 6.5 Clic `✕` → fenêtre détruite, tray persiste, `levoile-service` continue (vérifier via `tasklist` / gestionnaire de tâches). Clic gauche tray → nouvelle fenêtre créée
  - [ ] 6.6 Clic droit tray → menu avec "Ouvrir la fenêtre" + "Quitter". "Ouvrir" = équivalent clic gauche sur fenêtre absente
  - [ ] 6.7 Clics rapides (6-8 clics consécutifs) → aucun doublon de fenêtre, aucun panic, le compteur `webviewOpen` reste cohérent

- [ ] **Task 7 — Vérification manuelle Linux (déférable après packaging Epic 7)** (AC: #4)
  - [ ] 7.1 Sur GNOME 45+ ou KDE Plasma 6 : clic gauche ET clic droit ouvrent le menu appindicator → "Ouvrir la fenêtre" présent en premier
  - [ ] 7.2 `✕` ferme la webview, `levoile.service` (systemd) continue (`systemctl status levoile.service`)
  - [ ] 7.3 Si appindicator ne déclenche pas `SetOnTapped` → documenter comme limitation OS attendue dans les Dev Notes (pas un bug)

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

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
