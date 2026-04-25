# Story 10.3: Bouton Connect/Disconnect et Intégration Tray ↔ Wails

Status: deprecated — remplacé par 10-3-bouton-connect-disconnect-et-integration-tray-webview.md (architecture webview/webview + fyne.io/systray). Le bouton Disconnect a été supprimé — l'utilisateur quitte via X ou tray > Quitter.

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux connecter/déconnecter Le Voile depuis la fenêtre ou le tray, et ouvrir la fenêtre depuis le tray,
Afin de contrôler ma protection depuis n'importe quel point d'accès.

## Acceptance Criteria

**AC1 — Bouton Connect/Disconnect dans la fenêtre desktop**
**Given** la fenêtre desktop est ouverte et le tunnel est connecté
**When** l'utilisateur clique sur le bouton "Déconnecter"
**Then** le tunnel se désactive via l'IPC existant (`ActionDisconnect`)
**And** l'indicateur de statut passe à rouge ("Déconnecté")
**And** l'icône tray se met à jour simultanément (via son propre polling 2s)
**And** le bouton change pour afficher "Connecter" (vert)

**Given** la fenêtre desktop est ouverte et le tunnel est déconnecté
**When** l'utilisateur clique sur le bouton "Connecter"
**Then** le tunnel s'active via l'IPC existant (`ActionConnect`)
**And** l'indicateur passe à orange ("Reconnexion en cours...") puis vert
**And** le bouton change pour afficher "Déconnecter"

**AC2 — Menu tray "Ouvrir la fenêtre" lance/affiche le desktop Wails**
**Given** le tray icon est visible dans la barre des tâches
**When** l'utilisateur fait un clic droit et sélectionne "Ouvrir la fenêtre"
**Then** si le processus desktop n'est pas lancé → le tray lance `cmd/desktop/` en sous-processus et la fenêtre s'affiche
**And** si le processus desktop est déjà lancé → la fenêtre existante passe au premier plan

**AC3 — Fermeture fenêtre = masquer (pas détruire)**
**Given** la fenêtre Wails est ouverte
**When** l'utilisateur clique sur ✕ (fermer) dans la titlebar
**Then** un modal d'avertissement s'affiche (sauf si "Ne plus montrer" est coché)
**And** le modal affiche : titre "Quitter Le Voile ?", texte explicatif ─ vs ✕, checkbox "Ne plus montrer", boutons Annuler/Confirmer
**And** si l'utilisateur confirme : le tunnel se déconnecte, le DNS et les configs sont restaurés, le tray et la fenêtre se ferment
**And** si l'utilisateur annule : le modal se ferme, retour à l'application
**And** si "Ne plus montrer" était déjà coché : le ✕ exécute directement la fermeture/déconnexion sans modal

**Given** l'utilisateur clique sur ─ (minimiser)
**When** la fenêtre se masque
**Then** le service et le tray continuent de fonctionner
**And** la protection reste active

**AC4 — Menu clic droit tray synchronisé avec la fenêtre Wails**
**Given** le menu clic droit du tray contient "Connecter/Déconnecter"
**When** l'utilisateur sélectionne cette option depuis le tray
**Then** le tunnel s'active/se désactive
**And** la fenêtre Wails (si ouverte) reflète le changement d'état via le polling 2s existant

**Given** le menu clic droit du tray contient "Ouvrir"
**When** l'utilisateur sélectionne "Ouvrir"
**Then** la fenêtre Wails s'affiche (même comportement que clic gauche)

## Tasks / Subtasks

- [x] **Task 1 : Méthodes Connect/Disconnect dans `internal/desktop/app.go`** (AC: 1)
  - [x] 1.1 Ajouter la méthode `Connect() StatusResponse` :
    ```go
    func (a *App) Connect() StatusResponse {
        ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
        defer cancel()
        resp, err := a.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionConnect})
        if err != nil {
            a.reconnectIPC()
            return StatusResponse{Status: "disconnected", Message: "Déconnecté"}
        }
        return a.mapResponse(resp)
    }
    ```
  - [x] 1.2 Ajouter la méthode `Disconnect() StatusResponse` :
    ```go
    func (a *App) Disconnect() StatusResponse {
        ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
        defer cancel()
        resp, err := a.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionDisconnect})
        if err != nil {
            a.reconnectIPC()
            return StatusResponse{Status: "disconnected", Message: "Déconnecté"}
        }
        return a.mapResponse(resp)
    }
    ```
  - [x] 1.3 Ajouter la méthode `Quit()` pour le shutdown propre depuis le frontend :
    ```go
    func (a *App) Quit() {
        ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
        defer cancel()
        // Attendre la réponse IPC avant de quitter Wails —
        // le handler envoie la réponse AVANT le RequestStop (100ms délai côté service)
        a.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionQuit})
        // Le service s'arrêtera après le délai de 100ms.
        // Le tray détectera la perte de service via son polling 2s et se fermera seul.
        wailsRuntime.Quit(a.ctx)
    }
    ```
    **NOTE (F2)** : Le tray a sa propre logique `shutdownServiceAndRestore()` qui peut tenter un double cleanup DNS. Ce n'est pas un problème car : (1) le service restaure le DNS dans son shutdown, (2) le tray vérifie l'état avant de restaurer — si déjà restauré, c'est un no-op. Le tray se fermera de lui-même quand il détectera que le service ne répond plus.
  - [x] 1.4 Extraire `reconnectIPC()` en méthode privée (pattern déjà utilisé dans GetStatus, GetRegistry, SelectCountry — factoriser)

- [x] **Task 2 : Préférence "Ne plus montrer" — persistance config** (AC: 3)
  - [x] 2.1 Ajouter à `ClientConfig` dans `internal/config/config.go` :
    ```go
    type ClientConfig struct {
        AutoStart        bool   `toml:"auto_start"`
        PreferredCountry string `toml:"preferred_country"`
        SkipQuitModal    bool   `toml:"skip_quit_modal"` // NOUVEAU
    }
    ```
  - [x] 2.2 Ajouter un champ `skipQuitModal bool` dans la struct `App` de `internal/desktop/app.go`, initialisé au `Startup()` depuis la config :
    ```go
    type App struct {
        ctx            context.Context
        ipcClient      IPCClient
        relayDomain    string
        skipQuitModal  bool  // NOUVEAU — caché au startup, pas de I/O disque au runtime
    }
    ```
  - [x] 2.3 Ajouter méthode `GetSkipQuitModal() bool` et `SetSkipQuitModal(skip bool)` dans `internal/desktop/app.go` :
    - `GetSkipQuitModal()` : Retourne `a.skipQuitModal` (pas d'I/O disque)
    - `SetSkipQuitModal(skip)` : Met à jour `a.skipQuitModal` ET sauvegarde via `config.Save()` (I/O uniquement à l'écriture)
  - [x] 2.3 Ces méthodes sont exposées en Wails bindings pour appel depuis le frontend

- [x] **Task 3 : Menu tray "Ouvrir la fenêtre" + lancement subprocess desktop** (AC: 2, 4)
  - [x] 3.1 **DÉCISION ARCHITECTURALE (ADR-001)** : Le tray et le desktop sont **deux processus séparés**. Le tray lance le desktop en sous-processus via `exec.Command`. Pas de named event, pas de fusion processus, pas de migration systray. Le menu clic droit "Ouvrir la fenêtre" est le mécanisme retenu.
  - [x] 3.2 Ajouter "Ouvrir la fenêtre" comme **premier** item du menu tray (clic droit) dans `internal/tray/tray.go` :
    ```go
    menuOpen = systray.AddMenuItem("Ouvrir la fenêtre", "")
    ```
  - [x] 3.3 Ajouter un champ `desktopCmd *exec.Cmd` dans la struct `Tray` pour tracker le sous-processus desktop
  - [x] 3.4 Implémenter `handleOpenDesktop()` dans `internal/tray/tray.go` :
    ```go
    func (t *Tray) handleOpenDesktop() {
        // Si le processus est déjà lancé et actif → ne rien faire (la fenêtre poll déjà)
        if t.desktopCmd != nil && t.desktopCmd.Process != nil {
            // Vérifier si le processus est encore vivant
            // Si mort → relancer
        }
        // Chercher le binaire desktop à côté du binaire tray
        exePath := desktopExePath() // même dossier que le tray, "le-voile-desktop.exe"
        t.desktopCmd = exec.Command(exePath)
        t.desktopCmd.Start() // non-bloquant
    }
    ```
  - [x] 3.5 Dans `menuHandler()`, ajouter le handler pour `menuOpen.ClickedCh` → appelle `handleOpenDesktop()`
  - [x] 3.6 Dans `handleQuit()`, **ne PAS tuer le subprocess desktop**. Le desktop détectera le service arrêté via son polling 2s et affichera "Déconnecté". L'utilisateur ferme la fenêtre manuellement — comportement inoffensif. `Process.Kill()` empêcherait le callback Wails `OnShutdown` de s'exécuter proprement
  - [x] 3.7 Le menu tray existant "Activer/Désactiver Le Voile" (`handleToggle()`) reste inchangé — il envoie déjà `ActionConnect`/`ActionDisconnect` via IPC. La fenêtre Wails reflètera le changement d'état via son propre polling 2s
  - [x] 3.8 Vérifier que `updateTrayState()` met déjà à jour le label du menu toggle ("Connecter"/"Déconnecter") — si pas le cas, l'ajouter

- [x] **Task 5 : Bouton Connect/Disconnect — HTML** (AC: 1)
  - [x] 5.1 Ajouter le bouton dans `frontend/index.html`, dans le `.status-panel` après `status-uptime` et avant `test-link` :
    ```html
    <button class="btn-connect" id="btn-connect" onclick="toggleConnect()">Connecter</button>
    ```
  - [x] 5.2 Le bouton est **masqué** pendant la transition (orange/connecting) — pas de clic pendant la reconnexion

- [x] **Task 6 : Bouton Connect/Disconnect — CSS** (AC: 1)
  - [x] 6.1 Styles du bouton "Connecter" (action positive) dans `frontend/src/style.css` :
    ```css
    .btn-connect {
      display: block;
      width: 100%;
      max-width: 200px;
      padding: 0.625rem 1.5rem;
      margin: 0.75rem auto;
      border: none;
      border-radius: 6px;
      font-family: 'Rajdhani', sans-serif;
      font-weight: 600;
      font-size: 0.875rem;
      cursor: pointer;
      transition: background 0.15s, box-shadow 0.15s, opacity 0.15s;
    }
    /* État Connecter (vert — action positive) */
    .btn-connect.connect {
      background: var(--status-secure);
      color: var(--bg-primary);
    }
    .btn-connect.connect:hover {
      box-shadow: 0 0 16px rgba(74, 222, 128, 0.3);
    }
    /* État Déconnecter (rouge — action destructive) */
    .btn-connect.disconnect {
      background: transparent;
      color: var(--accent-red);
      border: 1px solid rgba(212, 43, 43, 0.3);
    }
    .btn-connect.disconnect:hover {
      background: rgba(212, 43, 43, 0.1);
      border-color: var(--accent-red);
    }
    /* Masqué pendant transition */
    .btn-connect.hidden {
      display: none;
    }
    .btn-connect:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    ```

- [x] **Task 7 : Bouton Connect/Disconnect — JavaScript** (AC: 1)
  - [x] 7.1 Ajouter le DOM ref dans `init()` :
    ```javascript
    dom.btnConnect = document.getElementById('btn-connect');
    ```
  - [x] 7.2 Implémenter `toggleConnect()` :
    ```javascript
    async function toggleConnect() {
      const btn = dom.btnConnect;
      btn.disabled = true;
      try {
        if (btn.classList.contains('disconnect')) {
          await window.go.desktop.App.Disconnect();
        } else {
          await window.go.desktop.App.Connect();
        }
      } catch (e) {
        // Erreur silencieuse — le polling mettra à jour
      }
      // Ne pas re-enable ici — le polling updateUI gère l'état
    }
    ```
  - [x] 7.3 Mettre à jour `updateUI(status)` pour gérer le bouton :
    ```javascript
    // Dans updateUI():
    const btn = dom.btnConnect;
    if (status.status === 'connected') {
      btn.className = 'btn-connect disconnect';
      btn.textContent = 'Déconnecter';
      btn.disabled = false;
    } else if (status.status === 'disconnected' || status.status === 'error') {
      btn.className = 'btn-connect connect';
      btn.textContent = 'Connecter';
      btn.disabled = false;
    } else {
      // connecting — masquer le bouton
      btn.className = 'btn-connect hidden';
    }
    ```

- [x] **Task 8 : Modal Quitter — HTML** (AC: 3)
  - [x] 8.1 Ajouter le modal dans `frontend/index.html`, après le `</main>` :
    ```html
    <div class="modal-overlay hidden" id="modal-overlay">
      <div class="modal-card" role="dialog" aria-modal="true" aria-labelledby="modal-title">
        <h2 class="modal-title" id="modal-title">Quitter Le Voile ?</h2>
        <p class="modal-text">
          Utilisez <strong>─</strong> pour réduire la fenêtre en icône tray.
          <strong>✕</strong> déconnecte le VPN et quitte Le Voile.
          Votre connexion ne sera plus protégée.
        </p>
        <label class="modal-checkbox">
          <input type="checkbox" id="modal-skip-checkbox">
          <span>Ne plus montrer</span>
        </label>
        <div class="modal-actions">
          <button class="btn-cancel" id="btn-modal-cancel" onclick="closeQuitModal()">Annuler</button>
          <button class="btn-confirm" id="btn-modal-confirm" onclick="confirmQuit()">Confirmer</button>
        </div>
      </div>
    </div>
    ```

- [x] **Task 9 : Modal Quitter — CSS** (AC: 3)
  - [x] 9.1 Styles du modal dans `frontend/src/style.css` :
    ```css
    .modal-overlay {
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.85);
      backdrop-filter: blur(4px);
      display: flex;
      align-items: center;
      justify-content: center;
      z-index: 100;
    }
    .modal-overlay.hidden { display: none; }
    .modal-card {
      background: var(--bg-secondary);
      border: 1px solid rgba(138, 155, 184, 0.15);
      border-radius: 8px;
      padding: 1.5rem;
      max-width: 300px;
      width: 90%;
    }
    .modal-title {
      font-family: 'Bebas Neue', sans-serif;
      font-size: 1.25rem;
      color: var(--text-primary);
      margin: 0 0 0.75rem;
    }
    .modal-text {
      font-family: 'Inter', sans-serif;
      font-size: 0.8125rem;
      color: var(--text-secondary);
      line-height: 1.5;
      margin: 0 0 1rem;
    }
    .modal-text strong {
      color: var(--text-primary);
      font-weight: 700;
    }
    .modal-checkbox {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      font-family: 'Rajdhani', sans-serif;
      font-size: 0.8125rem;
      color: var(--text-secondary);
      margin-bottom: 1rem;
      cursor: pointer;
    }
    .modal-checkbox input[type="checkbox"] {
      accent-color: var(--accent-glow);
    }
    .modal-actions {
      display: flex;
      gap: 0.75rem;
      justify-content: flex-end;
    }
    .btn-cancel {
      background: transparent;
      color: var(--text-secondary);
      border: 1px solid rgba(42, 141, 255, 0.2);
      border-radius: 6px;
      padding: 0.5rem 1rem;
      font-family: 'Rajdhani', sans-serif;
      font-weight: 600;
      font-size: 0.8125rem;
      cursor: pointer;
      transition: border-color 0.15s, color 0.15s;
    }
    .btn-cancel:hover {
      border-color: var(--accent-glow);
      color: var(--text-primary);
    }
    .btn-confirm {
      background: transparent;
      color: var(--accent-red);
      border: 1px solid rgba(212, 43, 43, 0.3);
      border-radius: 6px;
      padding: 0.5rem 1rem;
      font-family: 'Rajdhani', sans-serif;
      font-weight: 600;
      font-size: 0.8125rem;
      cursor: pointer;
      transition: background 0.15s, border-color 0.15s;
    }
    .btn-confirm:hover {
      background: rgba(212, 43, 43, 0.1);
      border-color: var(--accent-red);
    }
    ```

- [x] **Task 10 : Modal Quitter — JavaScript** (AC: 3)
  - [x] 10.1 Ajouter les DOM refs dans `init()` :
    ```javascript
    dom.modalOverlay = document.getElementById('modal-overlay');
    dom.modalSkipCheckbox = document.getElementById('modal-skip-checkbox');
    ```
  - [x] 10.2 Modifier `closeWindow()` pour afficher le modal au lieu de masquer directement :
    ```javascript
    async function closeWindow() {
      // Vérifier la préférence "ne plus montrer"
      try {
        const skip = await window.go.desktop.App.GetSkipQuitModal();
        if (skip) {
          await confirmQuit();
          return;
        }
      } catch (e) {
        // Si erreur, afficher le modal par défaut
      }
      dom.modalOverlay.classList.remove('hidden');
      document.getElementById('btn-modal-cancel').focus();
    }
    ```
  - [x] 10.3 Implémenter `closeQuitModal()` et `confirmQuit()` :
    ```javascript
    function closeQuitModal() {
      dom.modalOverlay.classList.add('hidden');
    }

    async function confirmQuit() {
      // Sauvegarder "ne plus montrer" si coché
      if (dom.modalSkipCheckbox && dom.modalSkipCheckbox.checked) {
        try {
          await window.go.desktop.App.SetSkipQuitModal(true);
        } catch (e) { /* best effort */ }
      }
      // Quit via Go — déconnexion + fermeture
      try {
        await window.go.desktop.App.Quit();
      } catch (e) {
        window.runtime.Quit();
      }
    }
    ```
  - [x] 10.4 Ajouter gestion Escape pour fermer le modal :
    ```javascript
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !dom.modalOverlay.classList.contains('hidden')) {
        closeQuitModal();
      }
    });
    ```

- [x] **Task 11 : Tests** (AC: 1-4)
  - [x] 11.1 `internal/desktop/app_test.go` — nouveaux tests :
    - `TestConnect_Success` — mock IPC retourne StatusConnecting → StatusResponse correcte
    - `TestConnect_IPCError` — IPC indisponible → retourne Déconnecté gracieusement
    - `TestDisconnect_Success` — mock IPC retourne StatusDisconnected → StatusResponse correcte
    - `TestDisconnect_IPCError` — IPC indisponible → retourne Déconnecté gracieusement
    - `TestQuit_SendsActionQuit` — vérifie que Quit envoie ActionQuit (mock sans wailsRuntime.Quit)
    - `TestGetSkipQuitModal` — mock config retourne true/false
    - `TestSetSkipQuitModal` — mock config est appelé avec la valeur correcte
  - [x] 11.2 `internal/config/config_test.go` :
    - `TestConfig_SkipQuitModal` — vérifie que le champ est persisté en TOML
  - [x] 11.3 `internal/tray/` — test basique :
    - `TestDesktopExePath` — vérifie que le chemin retourné est dans le même dossier que l'exécutable, suffixé `-desktop.exe`
  - [x] 11.4 Vérification build : `go build ./cmd/desktop/...` — compilation OK
  - [x] 11.5 Vérification build : `go build ./cmd/tray/...` — compilation OK (tray modifié pour "Ouvrir la fenêtre")
  - [x] 11.6 Vérification manuelle : `wails dev` → bouton connect/disconnect fonctionnel, modal quit

## Dev Notes

### Architecture de la Story 10.3 : Connect/Disconnect + Tray ↔ Wails

Cette story **étend** le package `internal/desktop/` et le frontend Wails créés en 10.1 et enrichis en 10.2. Elle ajoute aussi une interaction entre le tray et le desktop (deux processus séparés).

```
MODIFIÉ :
internal/desktop/app.go          # Connect(), Disconnect(), Quit(), GetSkipQuitModal(), SetSkipQuitModal()
internal/desktop/app_test.go     # Nouveaux tests Connect, Disconnect, Quit, SkipQuitModal
internal/config/config.go        # ClientConfig.SkipQuitModal
internal/config/config_test.go   # Test SkipQuitModal TOML
internal/tray/tray.go            # Item "Ouvrir la fenêtre" dans le menu + lancement processus desktop
frontend/index.html              # Bouton connect/disconnect, modal quitter
frontend/src/style.css           # Styles bouton, modal, btn-cancel, btn-confirm
frontend/src/app.js              # toggleConnect(), closeWindow() modal, confirmQuit()

POTENTIELLEMENT MODIFIÉ :
cmd/tray/main.go                 # Si des changements sont nécessaires pour le path du binaire desktop

NON MODIFIÉ :
internal/ipc/messages.go         # ActionConnect, ActionDisconnect, ActionQuit existent déjà
internal/ipchandler/handler.go   # handleConnect(), handleDisconnect(), handleQuit() existent déjà
internal/service/service.go      # Pas de changement — le service gère déjà connect/disconnect/quit
internal/relay/                  # Pas concerné
internal/registry/               # Pas concerné
internal/tunnel/                 # Pas concerné — Connect(), Disconnect() existent déjà
```

### Communication Desktop ↔ Service pour Connect/Disconnect

```
Frontend JS (clic bouton "Connecter")
  └── window.go.desktop.App.Connect()
        └── ipc.Client.SendContext(ActionConnect)
              └── Named Pipe \\.\pipe\levoile
                    └── ipchandler.handleConnect()
                          └── tunnel.Client.Connect(ctx) + reconnector restart

Frontend JS (clic bouton "Déconnecter")
  └── window.go.desktop.App.Disconnect()
        └── ipc.Client.SendContext(ActionDisconnect)
              └── Named Pipe \\.\pipe\levoile
                    └── ipchandler.handleDisconnect()
                          └── reconnector stop + tunnel.Client.Disconnect()

Frontend JS (clic "Confirmer" dans modal quitter)
  └── window.go.desktop.App.Quit()
        └── ipc.Client.SendContext(ActionQuit)
              └── Named Pipe \\.\pipe\levoile
                    └── ipchandler.handleQuit()
                          └── reconnector stop + SCM RequestStop()
        └── wailsRuntime.Quit(ctx)
```

### Intégration Tray ↔ Desktop : deux processus séparés (ADR-001)

**Problème** : Le tray (`cmd/tray/`) et le desktop (`cmd/desktop/`) sont deux processus Go indépendants. Le tray doit pouvoir ouvrir/afficher la fenêtre Wails.

**Décision** : Menu clic droit "Ouvrir la fenêtre" + lancement subprocess. Pas de named event Windows, pas de fusion processus, pas de migration systray.

**Rationale (ADR évalué)** :
- Named Event Windows : complexité disproportionnée, Windows-only
- Fusion tray + desktop : conflit `systray.Run()` vs `wails.Run()` sur le main thread — breaking change
- Second named pipe : over-engineering pour un simple "ouvre la fenêtre"
- Menu clic droit : **zéro risque, zéro nouvelle dépendance**, satisfait le besoin fonctionnel

```
cmd/tray/ (clic droit → "Ouvrir la fenêtre")
  └── handleOpenDesktop()
        └── Si processus desktop pas lancé → exec.Command("le-voile-desktop.exe").Start()
        └── Si processus desktop déjà lancé → ne rien faire (la fenêtre est déjà visible ou poll le service)
```

**Détection du processus desktop** : Le tray stocke `*exec.Cmd` et vérifie si le processus est encore vivant via `cmd.Process` + `cmd.ProcessState`. Si le processus est mort → relancer.

**Localisation du binaire** : Le binaire desktop (`le-voile-desktop.exe`) est dans le même dossier que le binaire tray. Utiliser `os.Executable()` pour trouver le chemin du tray, puis remplacer le nom de fichier.

**Quit propre** : Quand le tray reçoit "Quitter", il **ne tue PAS** le subprocess desktop (F3 — `Process.Kill()` empêcherait le callback Wails `OnShutdown`). Le desktop détectera le service arrêté via son polling 2s et affichera "Déconnecté". L'utilisateur ferme la fenêtre manuellement — comportement inoffensif.

**Double cleanup DNS (F2)** : Quand le desktop envoie `ActionQuit`, le service restaure le DNS dans son shutdown. Le tray, détectant la perte de service au prochain poll, pourrait tenter un second cleanup via `shutdownServiceAndRestore()`. Ce n'est pas un problème : la restauration DNS est idempotente — si le DNS est déjà restauré, c'est un no-op.

### Sélection pays — comportement 10.2 conservé

Le comportement auto-connexion de la story 10.2 (`selectCountry()` → `SelectCountry(code)` → reconnexion immédiate) est **conservé tel quel**. Le UX spec suggère un mode "preview" en 2 clics, mais les AC de l'epic 10.2 définissent l'auto-connexion, qui est déjà implémentée et fonctionnelle.

Le bouton Connect/Disconnect de cette story 10.3 couvre uniquement :
- **Connecté** → bouton "Déconnecter" (rouge, destructif) → `Disconnect()`
- **Déconnecté** → bouton "Connecter" (vert, positif) → `Connect()` (reconnexion au dernier pays)
- **Connecting** → bouton masqué (pas de clic pendant la transition)

Le changement de pays reste géré par le clic sidebar (auto-connexion 10.2).

### Modal Quitter — flux détaillé

```
closeWindow() (clic ✕)
  ├── await GetSkipQuitModal()
  │     ├── true → confirmQuit() directement
  │     └── false → afficher modal overlay
  │
  └── Modal affiché :
      ├── Annuler → closeQuitModal() → retour app
      ├── Confirmer → confirmQuit()
      │     ├── Si checkbox cochée → SetSkipQuitModal(true) → config.Save()
      │     └── Quit() → ActionQuit IPC → wailsRuntime.Quit()
      └── Escape → closeQuitModal()
```

**Focus trap** : Le focus initial est sur "Annuler" (action sûre). Tab circule entre checkbox, Annuler, Confirmer. Escape = Annuler.

**Overlay** : 85% opacité noir + backdrop-filter: blur(4px). Contenu derrière atténué.

### Synchronisation tray ↔ desktop

Le tray et le desktop **ne communiquent PAS directement** pour synchroniser l'état de connexion. Chacun poll le service indépendamment via IPC (2s). C'est suffisant car :

1. Le tray envoie `ActionConnect`/`ActionDisconnect` → service met à jour son état
2. Le desktop poll `ActionGetStatus` 2s plus tard → détecte le nouvel état
3. Latence max de synchronisation : 2 secondes — acceptable pour l'UX

Aucun mécanisme de push ou de callback inter-process n'est nécessaire pour les changements d'état ou le toggle fenêtre.

### Conventions Go — identiques à 10.1/10.2

- **Nommage fichiers** : `snake_case.go`
- **Package** : `desktop` (dans `internal/desktop/`)
- **Erreurs** : wrapping `fmt.Errorf("desktop: %w", err)`
- **Context** : timeout 10s pour Connect (opération longue), 5s pour Disconnect/Quit
- **Tests** : table-driven, `testing` standard, mock IPC via interface `IPCClient`
- **Aucun log côté client** — zero-log architecture (commit `b640d2d`)
- **Aucun import circulaire** : `desktop` → `ipc` + `config`, `tray` → `ipc`

### Bibliothèques

| Bibliothèque | Version | Usage dans 10.3 |
|-------------|---------|-----------------|
| `github.com/wailsapp/wails/v2` | v2.11.0 | Bindings Connect(), Disconnect(), Quit(), Get/SetSkipQuitModal() |
| `github.com/wailsapp/wails/v2/pkg/runtime` | — | WindowShow(), WindowHide(), Quit() |
| `fyne.io/systray` | existant | Menu "Ouvrir la fenêtre" (clic droit) |
| `internal/ipc` | existant | ActionConnect, ActionDisconnect, ActionQuit (déjà définis) |
| `internal/config` | existant | SkipQuitModal field |

**Aucune nouvelle dépendance externe.**

### Informations techniques récentes (mars 2026)

- **Wails v2** : v2.11.0 stable. `runtime.Quit(ctx)` ferme proprement la fenêtre et le processus Wails. `runtime.WindowShow(ctx)`/`runtime.WindowHide(ctx)` contrôlent la visibilité.
- **Wails v3** : en alpha — NE PAS migrer. Rester sur v2.
- **fyne.io/systray** : Pas de callback clic gauche. Le menu clic droit est le seul point d'interaction. Le menu "Ouvrir la fenêtre" est la solution retenue (ADR-001).
- **Backdrop-filter** : `backdrop-filter: blur(4px)` est supporté nativement par WebView2/Edge Chromium. Pas besoin de polyfill.

### Leçons des Stories 10.1 et 10.2

- **IPC reconnexion** : Close()+Connect() après erreur pipe — déjà implémenté, même pattern pour Connect/Disconnect
- **Binding path Wails** : Confirmé `window.go.desktop.App` — les nouvelles méthodes `Connect()`, `Disconnect()`, `Quit()`, `GetSkipQuitModal()`, `SetSkipQuitModal()` seront auto-générées sous le même chemin
- **Zero-log** : Aucun `log.Println` ou `fmt.Println` — erreurs retournées via IPC
- **Build check** : Vérifier `go build ./cmd/desktop/...` et `go build ./cmd/tray/...` après chaque modification
- **XSS** : Utiliser `textContent` au lieu de `innerHTML` (fix review 10.2)
- **setInterval cleanup** : Appliquer `startPolling()` pattern avec `clearInterval` (fix review 10.1)
- **test-link** : Masquer le lien test quand déconnecté (fix review 10.2) — même logique pour le bouton

### Lien avec Story 12.1 (Epic 12)

Story 12.1 traitera le shutdown propre avec indépendance service/UI de manière plus approfondie. Story 10.3 implémente le **cas simple** : ✕ dans la fenêtre + Quitter depuis le tray = arrêt de tous les composants. Le scénario "service continue après fermeture UI" est couvert par le design existant (fermer fenêtre via ─ = masquer, service continue).

### Ce qui n'est PAS dans le scope

- Panneau Paramètres / toggle WebRTC → hors scope Epic 10 (UX C6, J5)
- Étoile favori sur les pays → hors scope (UX C4)
- Cloche notifications → hors scope (UX C11)
- Fusion tray + desktop en un processus → hors scope (breaking change)
- Extension navigateur → Epic 11
- Shutdown propre avancé (service indépendant) → Epic 12 Story 12.1

### Project Structure Notes

- Le bouton suit la hiérarchie UX : vert positif (Connecter) / rouge destructif (Déconnecter)
- Le modal quitter est le **seul modal** de toute l'app — pas de confirmation pour les autres actions
- La préférence "Ne plus montrer" est persistée en TOML dans la config client existante
- L'intégration tray → desktop via menu clic droit + subprocess est la solution la plus simple et fiable (ADR-001)
- La synchronisation d'état repose sur le polling IPC existant (2s) — pas de push inter-process

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 10, Story 10.3, AC en BDD]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "IPC Named Pipe", "Tray", "Wails v2", "kardianos/service"]
- [Source: `_bmad-output/planning-artifacts/ux-design-specification.md` — C5 (Status Panel avec bouton), C8 (Quit Modal), C12 (Connect Button), J2 (changement pays), J4 (quitter vs minimiser), Button Hierarchy, Sidebar Logic, Modal Pattern]
- [Source: `internal/tray/tray.go` — handleToggle(), handleQuit(), menuHandler(), connectAndPoll(), shutdownServiceAndRestore()]
- [Source: `internal/desktop/app.go` — App struct, GetStatus(), GetRegistry(), SelectCountry(), mapResponse(), reconnectIPC pattern]
- [Source: `internal/ipc/messages.go` — ActionConnect, ActionDisconnect, ActionQuit (déjà définis)]
- [Source: `internal/ipchandler/handler.go` — handleConnect(), handleDisconnect(), handleQuit() (déjà implémentés)]
- [Source: `internal/service/service.go` — RequestStop(), TunnelClient(), shutdown()]
- [Source: `frontend/index.html` — Layout Direction F (sidebar + main panel), titlebar custom]
- [Source: `frontend/src/app.js` — Polling pattern, updateUI(), closeWindow() = WindowHide()]
- [Source: `frontend/src/style.css` — Design tokens, button styles, status-dot]
- [Source: `10-1-initialisation-wails-v2.md` — Dev Notes, Wails config, titlebar controls]
- [Source: `10-2-selecteur-de-pays-et-affichage-ip-visible.md` — Dev Notes, selectCountry(), review fixes]
- [Source: Wails v2 docs — Runtime API WindowShow/WindowHide/Quit]

### Vérification couverture AC

| AC | Couvert par |
|----|------------|
| AC1 (bouton connect/disconnect fenêtre) | Task 1 (Go methods), Task 5-7 (HTML/CSS/JS) |
| AC2 (menu tray "Ouvrir la fenêtre" + subprocess) | Task 3 (menu item + exec.Command subprocess) |
| AC3 (fermeture fenêtre masquer + modal) | Task 2 (config SkipQuitModal), Task 8-10 (HTML/CSS/JS modal) |
| AC4 (menu tray synchronisé) | Task 3.7-3.8 (toggle existant inchangé), polling 2s existant pour synchro |

### Couverture FRs

| FR | Couvert par AC |
|----|---------------|
| FR12 (connect/disconnect via fenêtre ou menu tray) | AC1, AC4 |
| FR13 (toggle fenêtre via clic gauche tray) | AC2 |

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

Aucun problème de debug rencontré.

### Completion Notes List

- Task 1 : Ajouté Connect(), Disconnect(), Quit() dans app.go. Factorisé reconnectIPC() en méthode privée, remplaçant le pattern Close()+Connect() dupliqué dans GetStatus, GetRegistry, SelectCountry. Quit() utilise runtimeQuit (injectable pour tests).
- Task 2 : Ajouté SkipQuitModal dans ClientConfig (TOML). App struct étendu avec skipQuitModal, configPath. Méthodes GetSkipQuitModal()/SetSkipQuitModal() exposées en Wails bindings. SetSkipQuitModal persiste via config.Load+Save.
- Task 3 : Menu "Ouvrir la fenêtre" ajouté comme premier item du tray. handleOpenDesktop() lance le desktop en subprocess via exec.Command. desktopExePath() localise le binaire dans le même dossier. Le tray ne tue PAS le desktop au quit (ADR-001).
- Tasks 5-7 : Bouton connect/disconnect avec états vert (Connecter) / rouge (Déconnecter) / masqué (connecting). toggleConnect() désactive le bouton pendant l'action, le polling le réactive.
- Tasks 8-10 : Modal "Quitter Le Voile ?" avec overlay backdrop-blur. closeWindow() vérifie GetSkipQuitModal avant d'afficher. confirmQuit() sauvegarde la préférence et appelle Quit(). Escape ferme le modal.
- Task 11 : 9 nouveaux tests desktop (Connect/Disconnect/Quit success+error, SkipQuitModal get/set/persist), 2 tests config (SkipQuitModal roundtrip + default), 1 test tray (desktopExePath). Builds desktop et tray OK. Suite complète : 0 régression.
- Mise à jour main.go racine et cmd/desktop/main.go pour nouveau signature NewApp(client, domain, cfgPath, skipQuitModal).

### Change Log

- 2026-03-17 : Implémentation story 10.3 — bouton connect/disconnect, modal quitter, menu tray "Ouvrir la fenêtre", intégration tray↔desktop via subprocess
- 2026-03-17 : Code review — 5 fixes appliqués : (H1) bindings Wails régénérés avec 5 nouvelles méthodes, (H2) dimensions fenêtre synchronisées main.go↔cmd/desktop/main.go à 420×540, (M1) SetSkipQuitModal revert en mémoire si Save échoue, (M2) handleOpenDesktop affiche tooltip d'erreur si Start() échoue, (M3) race condition ProcessState remplacée par flag desktopRunning protégé par mutex

### File List

- internal/desktop/app.go (modifié)
- internal/desktop/app_test.go (modifié)
- internal/config/config.go (modifié)
- internal/config/config_test.go (modifié)
- internal/tray/tray.go (modifié)
- internal/tray/tray_test.go (modifié)
- frontend/index.html (modifié)
- frontend/src/app.js (modifié)
- frontend/src/style.css (modifié)
- frontend/wailsjs/go/desktop/App.d.ts (modifié — review fix H1)
- frontend/wailsjs/go/desktop/App.js (modifié — review fix H1)
- cmd/desktop/main.go (modifié)
- main.go (modifié — review fix H2)
