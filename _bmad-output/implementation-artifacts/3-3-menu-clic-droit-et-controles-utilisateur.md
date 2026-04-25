# Story 3.3: Menu clic droit et controles utilisateur

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux pouvoir activer/desactiver Le Voile, gerer le demarrage automatique et quitter l'application via le menu du tray,
Afin de controler ma protection simplement.

## Acceptance Criteria

1. **Menu contextuel au clic droit** — Quand l'utilisateur fait un clic droit sur l'icone tray, un menu contextuel s'affiche avec les options : "Activer/Desactiver Le Voile", "Demarrage auto : oui/non", "Quitter".

2. **Desactiver Le Voile** — Quand Le Voile est actif (tunnel connecte) et l'utilisateur clique sur "Desactiver Le Voile", le tray envoie `{"action":"disconnect"}` via IPC, le service arrete le tunnel et restaure le resolver DNS, l'icone passe en rouge et le label du menu devient "Activer Le Voile".

3. **Activer Le Voile** — Quand Le Voile est inactif et l'utilisateur clique sur "Activer Le Voile", le tray envoie `{"action":"connect"}` via IPC, le service retablit le tunnel et la protection DNS, l'icone passe en vert et le label du menu devient "Desactiver Le Voile".

4. **Toggle demarrage automatique** — Quand l'utilisateur clique sur "Demarrage auto : oui", la preference est desactivee dans le fichier config TOML (`auto_start = false`), le label du menu devient "Demarrage auto : non", et le service ne demarrera plus automatiquement au prochain boot. L'inverse est vrai si l'utilisateur clique sur "Demarrage auto : non".

5. **Quitter l'application** — Quand l'utilisateur clique sur "Quitter", le tray envoie `{"action":"disconnect"}` via IPC, le service restaure le resolver DNS a sa valeur originale, le tray UI se ferme, et le service s'arrete proprement.

### Scenarios BDD detailles

**AC1 — Menu contextuel:**
```
Given l'icone tray affichee
When l'utilisateur fait un clic droit
Then un menu contextuel s'affiche avec les options :
  - "Desactiver Le Voile" (si connecte) / "Activer Le Voile" (si deconnecte)
  - "Demarrage auto : oui" (si active) / "Demarrage auto : non" (si desactive)
  - separateur
  - "Quitter"
```

**AC2 — Desactiver Le Voile:**
```
Given Le Voile est actif (tunnel connecte)
When l'utilisateur clique sur "Desactiver Le Voile"
Then le tray envoie {"action":"disconnect"} via IPC
And le service arrete le tunnel et restaure le resolver DNS
And l'icone passe en rouge (disconnected.ico)
And le label du menu devient "Activer Le Voile"
```

**AC3 — Activer Le Voile:**
```
Given Le Voile est inactif
When l'utilisateur clique sur "Activer Le Voile"
Then le tray envoie {"action":"connect"} via IPC
And le service retablit le tunnel et la protection DNS
And l'icone passe en vert (connected.ico)
And le label du menu devient "Desactiver Le Voile"
```

**AC4 — Toggle demarrage auto (desactiver):**
```
Given le demarrage automatique est active (par defaut)
When l'utilisateur clique sur "Demarrage auto : oui"
Then la preference est desactivee dans le fichier config TOML (auto_start = false)
And le label du menu devient "Demarrage auto : non"
And le service ne demarrera plus automatiquement au prochain boot
```

**AC4b — Toggle demarrage auto (activer):**
```
Given le demarrage automatique est desactive
When l'utilisateur clique sur "Demarrage auto : non"
Then la preference est activee dans le fichier config TOML (auto_start = true)
And le label du menu devient "Demarrage auto : oui"
And le service demarrera automatiquement au prochain boot
```

**AC5 — Quitter:**
```
Given le menu affiche
When l'utilisateur clique sur "Quitter"
Then le tray envoie {"action":"quit"} via IPC
And le service deconnecte le tunnel
And le service restaure le resolver DNS a sa valeur originale
And le tray UI se ferme (systray.Quit())
And le service s'arrete proprement
```

## Tasks / Subtasks

- [x] **Task 1 : Implementer le package config — lecture/ecriture TOML** (AC: #4)
  - [x] 1.1 Ajouter `github.com/BurntSushi/toml` v1.5.0 a go.mod : `go get github.com/BurntSushi/toml@v1.5.0`
  - [x] 1.2 Creer `internal/config/config.go` — struct `Config` avec champs : `RelayDomain string`, `RelayPubKeyEd25519 string`, `AutoStart bool`
  - [x] 1.3 Implementer `Load(path string) (*Config, error)` — lit le fichier TOML via `toml.DecodeFile`, retourne config par defaut si le fichier n'existe pas
  - [x] 1.4 Implementer `Save(path string) error` — ecrit le fichier TOML via `toml.NewEncoder`
  - [x] 1.5 Implementer `DefaultPath() (string, error)` — retourne le chemin config selon l'OS : `%AppData%/LeVoile/config.toml` (Windows), `~/.config/levoile/config.toml` (Linux/macOS)
  - [x] 1.6 Creer `internal/config/paths_windows.go` — `//go:build windows`, utilise `os.UserConfigDir()` + `LeVoile`
  - [x] 1.7 Creer `internal/config/paths_unix.go` — `//go:build linux || darwin`, utilise `os.UserConfigDir()` + `levoile`
  - [x] 1.8 Creer `internal/config/config_test.go` — tests : Load fichier inexistant (defauts), Load/Save roundtrip, DefaultPath non-vide
  - [x] 1.9 Supprimer `internal/config/doc.go` — remplace par config.go

- [x] **Task 2 : Etendre le protocole IPC — nouvelles actions** (AC: #4, #5)
  - [x] 2.1 Ajouter dans `internal/ipc/messages.go` : `ActionSetAutoStart = "set_auto_start"`, `ActionQuit = "quit"`
  - [x] 2.2 Ajouter le champ `Value string` a `Request` : `Value string \`json:"value,omitempty"\``
  - [x] 2.3 Mettre a jour le handler IPC dans `cmd/client/main.go` :
    - `set_auto_start` : lire `req.Value` ("true"/"false"), charger config, mettre a jour `auto_start`, sauvegarder, changer le type de demarrage du service OS
    - `quit` : deconnecter le tunnel, restaurer le DNS, arreter le service proprement (appeler `p.cancel()`)
  - [x] 2.4 Pour `set_auto_start` — changer le type de demarrage du service OS :
    - Windows : executer `sc config LeVoile start= auto` ou `sc config LeVoile start= demand` via `exec.Command`
    - Linux : executer `systemctl enable/disable levoile` via `exec.Command`
    - macOS : modifier le plist launchd (post-MVP, documenter)
  - [x] 2.5 **ATTENTION** : Le handler `quit` doit envoyer la reponse IPC AVANT de declencher l'arret du service. Utiliser un `time.AfterFunc(100ms, p.cancel)` pour laisser le temps a la reponse de transiter

- [x] **Task 3 : Ajouter les items de menu au tray** (AC: #1, #2, #3, #4, #5)
  - [x] 3.1 Dans `tray.go`, ajouter les champs au struct `Tray` : `menuToggle *systray.MenuItem`, `menuAutoStart *systray.MenuItem`, `menuQuit *systray.MenuItem`, `connected bool`
  - [x] 3.2 Ajouter une interface `SystrayMenuAPI` pour testabilite (ou etendre `SystrayAPI`) : `AddMenuItem(title, tooltip string) *systray.MenuItem`, `AddMenuItemCheckbox(title, tooltip string, checked bool) *systray.MenuItem`, `AddSeparator()`, `Quit()`
  - [x] 3.3 Dans `onReady()` — creer les items de menu APRES SetIcon/SetTooltip :
    - `t.menuToggle = systray.AddMenuItem("Activer Le Voile", "Activer/Desactiver la protection")`
    - `systray.AddSeparator()`
    - `t.menuAutoStart = systray.AddMenuItemCheckbox("Demarrage auto", "Demarrage automatique au boot", true)` (checked par defaut, lire config)
    - `systray.AddSeparator()`
    - `t.menuQuit = systray.AddMenuItem("Quitter", "Quitter Le Voile")`
  - [x] 3.4 Charger la config TOML au demarrage du tray pour initialiser l'etat du checkbox auto_start
  - [x] 3.5 Lancer une goroutine `menuHandler(ctx)` qui ecoute les channels `ClickedCh` des items de menu

- [x] **Task 4 : Logique du menu toggle Activer/Desactiver** (AC: #2, #3)
  - [x] 4.1 Dans `menuHandler()`, ecouter `t.menuToggle.ClickedCh`
  - [x] 4.2 Si etat courant `connected` → envoyer `{"action":"disconnect"}` via IPC
  - [x] 4.3 Si etat courant `disconnected` → envoyer `{"action":"connect"}` via IPC
  - [x] 4.4 Mettre a jour le label du menu selon la reponse IPC :
    - Apres `disconnect` reussi → `t.menuToggle.SetTitle("Activer Le Voile")`
    - Apres `connect` reussi → `t.menuToggle.SetTitle("Desactiver Le Voile")`
  - [x] 4.5 Le label du menu doit aussi se mettre a jour automatiquement via la boucle de polling existante (dans `updateTrayState`)

- [x] **Task 5 : Logique du menu demarrage automatique** (AC: #4)
  - [x] 5.1 Dans `menuHandler()`, ecouter `t.menuAutoStart.ClickedCh`
  - [x] 5.2 Au clic, lire l'etat courant du checkbox via `t.menuAutoStart.Checked()`
  - [x] 5.3 Envoyer `{"action":"set_auto_start","value":"false"}` si etait coche, `"true"` sinon
  - [x] 5.4 Sur reponse OK → basculer le checkbox : `t.menuAutoStart.Check()` ou `t.menuAutoStart.Uncheck()`
  - [x] 5.5 Mettre a jour le titre : `"Demarrage auto : oui"` ou `"Demarrage auto : non"`

- [x] **Task 6 : Logique du menu Quitter** (AC: #5)
  - [x] 6.1 Dans `menuHandler()`, ecouter `t.menuQuit.ClickedCh`
  - [x] 6.2 Au clic, envoyer `{"action":"quit"}` via IPC
  - [x] 6.3 Appeler `systray.Quit()` pour fermer le tray UI
  - [x] 6.4 **IMPORTANT** : Le quit du tray doit etre gracieux — attendre la reponse IPC (ou timeout 3s) avant de fermer

- [x] **Task 7 : Synchronisation menu ↔ polling** (AC: #2, #3)
  - [x] 7.1 Modifier `updateTrayState()` pour mettre a jour le label du menu toggle en plus de l'icone/tooltip :
    - `connected` → `t.menuToggle.SetTitle("Desactiver Le Voile")`
    - `disconnected`/`error` → `t.menuToggle.SetTitle("Activer Le Voile")`
    - `connecting` → garder le label actuel (ne pas changer pendant la transition)
  - [x] 7.2 Stocker l'etat `connected bool` dans le struct `Tray` (protege par mutex) pour la logique du menu toggle
  - [x] 7.3 **ATTENTION** : Les appels `SetTitle()` sur les `MenuItem` sont thread-safe dans fyne.io/systray — peuvent etre appeles depuis la goroutine de polling

- [x] **Task 8 : Tests** (AC: tous)
  - [x] 8.1 Mettre a jour `internal/tray/tray_test.go` — tester les nouveaux comportements :
    - Test menu toggle : connected → disconnect → label change
    - Test menu toggle : disconnected → connect → label change
    - Test menu auto_start toggle : envoie la bonne action IPC
    - Test menu quit : envoie quit + systray.Quit()
    - Test synchronisation polling → menu label
  - [x] 8.2 Tester `internal/config/config_test.go` — Load/Save/DefaultPath
  - [x] 8.3 Tester le handler IPC `set_auto_start` (mock exec.Command)
  - [x] 8.4 Tester le handler IPC `quit` (verifie que cancel est appele)

- [x] **Task 9 : Validation globale** (AC: tous)
  - [x] 9.1 Executer tous les tests (`go test ./...`) — tray + config + non-regression des 117 tests existants
  - [x] 9.2 Verifier compilation multi-plateforme (`GOOS=windows/linux/darwin`)
  - [x] 9.3 Executer `go vet ./...` — zero warning
  - [ ] 9.4 Verifier que le menu s'affiche au clic droit dans le system tray (test manuel Windows)
  - [ ] 9.5 Verifier les actions connect/disconnect/quit via le menu (test manuel)
  - [ ] 9.6 Verifier le toggle demarrage auto et la persistence TOML (test manuel)

## Dev Notes

### Contraintes architecturales OBLIGATOIRES

- **Pure Go, pas de CGo** — Module path: `github.com/velia-the-veil/le_voile`
- **Jamais de logging client** — Les erreurs sont propagees via IPC au tray, et le tray les affiche dans le tooltip. PAS de `log.Println`
- **Jamais de `panic`** — Toujours retourner `error`
- **`context.Context` obligatoire** — Premier parametre de toutes les fonctions bloquantes ou reseau
- **Wrapping d'erreurs** — Format : `fmt.Errorf("tray: %w", err)` / `fmt.Errorf("config: %w", err)`
- **Tests co-localises** — `tray_test.go` a cote de `tray.go`, `config_test.go` a cote de `config.go`

### Conventions de nommage (OBLIGATOIRES)

- Packages : `tray`, `config` (minuscule, un mot)
- Fichiers : `snake_case.go` (`tray.go`, `config.go`, `paths_windows.go`, `paths_unix.go`)
- Fonctions exportees : `PascalCase` (`Load`, `Save`, `DefaultPath`, `NewTray`)
- Fonctions privees : `camelCase` (`menuHandler`, `handleToggle`, `handleQuit`)
- Constructeurs : `New` + type (`NewTray()`)
- Tests : `TestType_Method` (`TestConfig_LoadDefaults`, `TestConfig_SaveRoundtrip`, `TestTray_MenuToggle`)
- Constantes : `PascalCase` (`ActionSetAutoStart`, `ActionQuit`)

### fyne.io/systray — API Menu (v1.12.0)

**API Menu :**
```go
systray.AddMenuItem(title, tooltip string) *MenuItem    // Ajoute un item
systray.AddMenuItemCheckbox(title, tooltip, checked bool) *MenuItem  // Item avec checkbox
systray.AddSeparator()                                   // Separateur visuel

// MenuItem methods:
item.ClickedCh                     // chan struct{} — recoit quand l'item est clique
item.SetTitle(title string)        // Change le titre
item.SetTooltip(tooltip string)    // Change le tooltip
item.Check()                       // Coche l'item
item.Uncheck()                     // Decoche l'item
item.Checked() bool                // Retourne l'etat coche
item.Disable()                     // Grise l'item
item.Enable()                      // Re-active l'item
```

**CONTRAINTES CRITIQUES :**
1. Les items de menu doivent etre crees dans `onReady()` — APRES que systray soit initialise
2. `ClickedCh` est un unbuffered channel — lire depuis une goroutine dedicee
3. `AddMenuItem` et `AddMenuItemCheckbox` sont thread-safe
4. `SetTitle()` est thread-safe — peut etre appele depuis la goroutine de polling
5. Sur Linux, utiliser `AddMenuItemCheckbox` au lieu de `AddMenuItem` pour le checkbox (sinon le check n'apparait pas)

**Pattern d'ecoute des clics menu :**
```go
func (t *Tray) menuHandler(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.menuToggle.ClickedCh:
            t.handleToggle(ctx)
        case <-t.menuAutoStart.ClickedCh:
            t.handleAutoStartToggle(ctx)
        case <-t.menuQuit.ClickedCh:
            t.handleQuit(ctx)
        }
    }
}
```

### BurntSushi/toml — Config TOML

**Structure config TOML :**
```toml
[relay]
domain = "levoile.dev"
public_key_ed25519 = "base64..."

[client]
auto_start = true
```

**Go struct correspondante :**
```go
type Config struct {
    Relay  RelayConfig  `toml:"relay"`
    Client ClientConfig `toml:"client"`
}

type RelayConfig struct {
    Domain           string `toml:"domain"`
    PublicKeyEd25519 string `toml:"public_key_ed25519"`
}

type ClientConfig struct {
    AutoStart bool `toml:"auto_start"`
}
```

**Lecture/Ecriture :**
```go
// Lecture
var cfg Config
_, err := toml.DecodeFile(path, &cfg)

// Ecriture
f, _ := os.Create(path)
defer f.Close()
toml.NewEncoder(f).Encode(cfg)
```

### Protocole IPC — Extensions requises

**Nouveau champ Request :**
```go
type Request struct {
    Action string `json:"action"`
    Value  string `json:"value,omitempty"`  // NOUVEAU — parametre optionnel
}
```

**Nouvelles actions :**
| Action | Value | Description |
|--------|-------|-------------|
| `set_auto_start` | `"true"` / `"false"` | Change la preference de demarrage auto |
| `quit` | — | Deconnecte, restaure DNS, arrete le service |

**ATTENTION — Compatibilite :** L'ajout du champ `Value` a `Request` est retrocompatible car `omitempty` signifie que les requetes existantes sans `Value` continueront de fonctionner. Le JSON existant `{"action":"get_status"}` sera deserialise avec `Value = ""`.

### Handler IPC `quit` — Implementation critique

Le handler `quit` dans `cmd/client/main.go` doit :
1. Deconnecter le tunnel (comme `disconnect`)
2. Envoyer la reponse IPC au tray AVANT l'arret
3. Declencher l'arret du service avec un leger delai

```go
case ipc.ActionQuit:
    // Disconnect tunnel first
    tc := prg.TunnelClient()
    if tc != nil {
        if r := prg.Reconnector(); r != nil {
            r.Stop()
        }
        tc.Disconnect()
    }
    // Schedule service shutdown after response is sent
    go func() {
        time.Sleep(100 * time.Millisecond)
        prg.Cancel() // triggers full shutdown sequence
    }()
    return ipc.Response{Status: ipc.StatusDisconnected}
```

**IMPORTANT :** `prg.Cancel()` n'existe pas encore — il faudra l'ajouter au type `Program` pour exposer la fonction `cancel()` du context. Ajouter :
```go
func (p *Program) Cancel() {
    if p.cancel != nil {
        p.cancel()
    }
}
```

### Handler IPC `set_auto_start` — Service startup type OS

**Windows (MVP prioritaire) :**
```go
startType := "auto"
if !autoStart {
    startType = "demand"
}
cmd := exec.CommandContext(ctx, "sc", "config", service.ServiceName, "start=", startType)
```

**Linux :**
```go
action := "enable"
if !autoStart {
    action = "disable"
}
cmd := exec.CommandContext(ctx, "systemctl", action, "levoile")
```

**ATTENTION :** Ces commandes necessitent les privileges de l'utilisateur sous lequel le service tourne (generalement SYSTEM sur Windows, root sur Linux). Le service a ces privileges, donc c'est correct.

### APIs existantes a utiliser (NE PAS REINVENTER)

**ipc.Client** (`internal/ipc/client.go`) — DEJA EXISTANT :
- `NewClient() *Client`
- `Connect() error`
- `Close() error`
- `SendContext(ctx context.Context, req Request) (Response, error)`

**ipc.Request / ipc.Response** (`internal/ipc/messages.go`) — A ETENDRE :
- `ActionGetStatus`, `ActionConnect`, `ActionDisconnect` — existants
- `ActionSetAutoStart`, `ActionQuit` — A AJOUTER
- `Request.Value` — A AJOUTER

**Tray existant** (`internal/tray/tray.go`) — A ETENDRE :
- `SystrayAPI` interface — etendre ou creer `SystrayMenuAPI`
- `updateTrayState()` — ajouter mise a jour du label menu
- `connectAndPoll()` — ajouter goroutine `menuHandler` en parallele

### Dependances de cette story

- **Story 3.2** (done) : `tray.Tray`, `SystrayAPI`, `IPCClient`, icones, polling IPC, `updateTrayState`
- **Story 3.1** (done) : `ipc.Client`, `ipc.Server`, `ipc.Request`, `ipc.Response`, service lifecycle
- **Nouvelles dependances externes :**
  - `github.com/BurntSushi/toml` v1.5.0 — bibliotheque TOML pour la config

### NFRs a respecter

| NFR | Exigence | Impact |
|-----|----------|--------|
| NFR5 | Zero fuite DNS | Le disconnect via menu DOIT restaurer le resolver avant de signaler le succes |
| NFR6 | Resolver restaure dans tous les scenarios | Le quit DOIT restaurer le resolver (le service fait deja ca dans shutdown()) |
| NFR11 | RAM < 20MB | Config TOML minimal, pas de cache superflu |
| NFR12 | CPU < 1% | Les goroutines menu handler sont event-driven (channels), pas de polling actif |

### Ce qui est HORS SCOPE (ne PAS implementer)

- Installateur Windows (Epic 4)
- Version portable (Epic 4)
- Demarrage automatique du tray au login (Epic 4 — le tray ne s'auto-lance pas encore)
- Detection IP visible reelle (le service retourne "unknown" au MVP)
- Icones design finales (MVP = icones simples generees dans Story 3.2)
- Config relay domain/pubkey via UI (MVP = fichier TOML edite manuellement)
- macOS launchd auto_start toggle (post-MVP, documenter comme limitation)

### Intelligence de la Story 3.2 (CRUCIAL)

**Lecons directement applicables :**
- `SystrayAPI` interface pour testabilite — etendre le meme pattern pour les menus
- `updateTrayState()` utilise deduplication via `stateKey` — ajouter le label menu a la logique
- Le polling IPC toutes les 2 secondes fonctionne bien — pas besoin de changer la frequence
- La goroutine de polling et les appels `systray.Set*` sont thread-safe — les appels menu `SetTitle` le sont aussi
- Reconnexion IPC avec backoff (1s→10s) — reutiliser pour les actions menu qui echouent
- `fyne.io/systray v1.12.0` deja dans go.mod — pas de mise a jour necessaire
- Icones dans `internal/tray/` — ne pas deplacer

**Fichiers crees dans Story 3.2 (NE PAS RECREER, ETENDRE) :**
- `internal/tray/tray.go` — A MODIFIER : ajouter menu items, menuHandler, handleToggle, handleAutoStartToggle, handleQuit, modifier updateTrayState
- `internal/tray/tray_test.go` — A MODIFIER : ajouter tests menu
- `internal/tray/icons.go` — NE PAS TOUCHER
- `internal/tray/connected.ico` / `connecting.ico` / `disconnected.ico` — NE PAS TOUCHER
- `cmd/tray/main.go` — MODIFIER si necessaire pour passer la config

**Debug log de la Story 3.2 :**
- Cross-compilation Darwin echoue depuis Windows : fyne.io/systray necessite les headers natifs macOS (Objective-C). Compilation OK sur macOS natif. Windows et Linux OK.
- Interface `SystrayAPI` a ete etendue pendant la code review pour inclure `SetTitle` — confirme que l'approche d'extension d'interface fonctionne.

### Intelligence de la Story 3.1 (service/IPC)

**Lecons applicables :**
- Le handler IPC dans `cmd/client/main.go` est un simple switch — ajouter les cas `set_auto_start` et `quit`
- `ipc.NewPlatformListener()` + `ipc.NewServer()` — ne pas toucher, juste etendre les messages
- `prg.Reconnector().Stop()` — DOIT etre appele avant disconnect pour eviter la reconnexion automatique. Deja fait dans le handler `disconnect`, meme pattern pour `quit`
- Scanner buffer 4096 bytes — les nouvelles requetes/reponses sont bien dans cette limite
- Le serveur IPC ferme les connexions actives au shutdown — le client tray recevra une erreur de lecture apres `quit`, ce qui est normal

### Project Structure Notes

**Fichiers a CREER :**
```
internal/config/config.go            # Struct Config, Load, Save
internal/config/config_test.go       # Tests Load/Save/DefaultPath
internal/config/paths_windows.go     # //go:build windows — chemin %AppData%/LeVoile
internal/config/paths_unix.go        # //go:build linux || darwin — chemin ~/.config/levoile
```

**Fichiers a MODIFIER :**
```
internal/ipc/messages.go             # Ajouter ActionSetAutoStart, ActionQuit, Request.Value
internal/tray/tray.go                # Ajouter menu items, menuHandler, modifier updateTrayState
internal/tray/tray_test.go           # Ajouter tests menu
cmd/client/main.go                   # Ajouter handlers IPC set_auto_start et quit
cmd/tray/main.go                     # Charger config au demarrage, passer au Tray
internal/service/service.go          # Ajouter methode Cancel() pour exposer cancel()
go.mod / go.sum                      # Ajouter github.com/BurntSushi/toml
```

**Fichiers a SUPPRIMER :**
```
internal/config/doc.go               # Remplace par config.go
```

**Fichiers a NE PAS TOUCHER :**
```
internal/ipc/server.go               # Serveur IPC stable (Story 3.1)
internal/ipc/client.go               # Client IPC stable (Story 3.1)
internal/ipc/pipe_windows.go         # Transport stable
internal/ipc/pipe_unix.go            # Transport stable
internal/tray/icons.go               # Embed stable (Story 3.2)
internal/tray/*.ico                  # Icones stables (Story 3.2)
internal/tunnel/*                    # APIs stables (Stories 2.x)
internal/dns/*                       # APIs stables (Stories 2.x)
internal/watchdog/*                  # API stable (Story 2.3)
internal/crypto/*                    # Module crypto stable (Story 1.1)
internal/relay/*                     # Cote serveur — pas concerne
cmd/relay/*                          # Cote serveur — pas concerne
```

**Alignement structure projet :**
- `internal/config/config.go` + `paths_*.go` — conforme a l'architecture (prevu dans la structure projet)
- Extension de `internal/tray/tray.go` — conforme, meme fichier que Story 3.2
- Extension de `internal/ipc/messages.go` — ajout retrocompatible
- Extension de `cmd/client/main.go` — ajout de handlers IPC
- Aucun conflit detecte

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Configuration & Donnees] — Config TOML dans %AppData%/LeVoile/config.toml, contenu : cle publique Ed25519, domaine, auto_start
- [Source: _bmad-output/planning-artifacts/architecture.md#Communication & Protocole] — IPC named pipe/unix socket, messages JSON {action, status, ip, error}
- [Source: _bmad-output/planning-artifacts/architecture.md#Architecture Service & Integration OS] — kardianos/service pour gestion service OS
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — internal/config/, internal/tray/, cmd/tray/
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns] — naming, error handling, concurrency patterns
- [Source: _bmad-output/planning-artifacts/architecture.md#Format Patterns] — Config TOML structure, IPC JSON format
- [Source: _bmad-output/planning-artifacts/epics.md#Epic3-Story3.3] — Criteres d'acceptation detailles
- [Source: _bmad-output/planning-artifacts/prd.md#FR11] — Activer/desactiver via menu
- [Source: _bmad-output/planning-artifacts/prd.md#FR12] — Toggle demarrage auto via menu
- [Source: _bmad-output/planning-artifacts/prd.md#FR13] — Quitter via menu
- [Source: _bmad-output/planning-artifacts/prd.md#FR14] — Service demarrage automatique
- [Source: _bmad-output/planning-artifacts/prd.md#FR16] — Service independant du tray
- [Source: _bmad-output/implementation-artifacts/3-2-system-tray-ui-avec-icones-detat-et-tooltip-ip.md] — SystrayAPI interface, updateTrayState, polling IPC, icones, code review fixes
- [Source: _bmad-output/implementation-artifacts/3-1-service-systeme-avec-kardianos-service-et-module-ipc.md] — IPC client/server APIs, service lifecycle, debug logs
- [Source: fyne.io/systray docs] — AddMenuItem, AddMenuItemCheckbox, MenuItem.ClickedCh, SetTitle, Check/Uncheck
- [Source: github.com/BurntSushi/toml] — DecodeFile, NewEncoder, v1.5.0

## Change Log

- 2026-03-09: Implementation complete — package config TOML, extension IPC (set_auto_start, quit), menu tray complet avec toggle, auto-start, quit, et synchronisation polling. 28 nouveaux tests ajoutés (4 config, 20 tray, 4 client handler).
- 2026-03-09: Code review fixes (Claude Opus 4.6) — 4 HIGH + 4 MEDIUM corrigés:
  - H1: go.mod BurntSushi/toml déplacé de indirect à direct
  - H2: Ajout de 3 tests handleAutoStartToggle + 1 test handleToggle error feedback
  - H3/H4: Feedback d'erreur via tooltip dans handleToggle et handleAutoStartToggle
  - M1: Constante StatusOK ajoutée dans messages.go, utilisée côté service et tray
  - M2: TOCTOU corrigé dans config.Load (suppression os.Stat)
  - M3: newWithDeps accepte maintenant autoStart pour testabilité complète
  - M4: setServiceStartupType rendu mockable, test SetAutoStart fiabilisé
  - Tests dupliqués SetsMenuLabel supprimés et remplacés par vrais tests

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Cross-compilation Darwin echoue depuis Windows (connu, Story 3.2) — fyne.io/systray necessite headers natifs macOS. Windows et Linux OK.
- macOS auto_start toggle non implemente (hors scope, post-MVP) — `setServiceStartupType` retourne nil sur darwin.

### Completion Notes List

- Task 1: Package `internal/config` cree avec struct TOML imbriquee (Relay/Client), Load avec defaults, Save avec creation de repertoires, DefaultPath platform-specific via build tags.
- Task 2: IPC etendu — `ActionSetAutoStart`, `ActionQuit` ajoutés, champ `Value` ajouté a `Request` (retrocompatible via omitempty). Handler `quit` utilise goroutine avec 100ms delay pour envoyer la reponse avant shutdown. Handler `set_auto_start` charge/sauvegarde config TOML et change le type de demarrage du service OS via `sc` (Windows) ou `systemctl` (Linux). Methode `Cancel()` ajoutee au type `Program`.
- Tasks 3-7: Menu tray complet — `SystrayMenuAPI` interface creee pour testabilite, items de menu crees dans `onReady()`, goroutine `menuHandler` ecoute les ClickedCh. Toggle connect/disconnect envoie l'action IPC appropriee selon l'etat `connected`. Auto-start toggle lit le checkbox, envoie `set_auto_start` et bascule l'UI sur reponse OK. Quit envoie `quit` avec timeout 3s puis appelle `systray.Quit()`. `updateTrayState` met a jour le label du menu toggle en plus de l'icone/tooltip, avec nil-guard pour les tests.
- Task 8: 28 tests — 4 config (LoadDefaults, SaveRoundtrip, LoadInvalidFile, DefaultPath), 20 tray (existants adaptes + nouveaux: ConnectedStateTracking, ConnectingKeepsState, HandleToggle Connected/Disconnected, HandleQuit, HandleToggle IPCError, menu label tests), 4 client handler (SetAutoStart, Quit nil tunnel, UnknownAction, FormatUptime).
- Task 9: `go test ./...` 100% pass, `go vet ./...` zero warning, cross-compile Windows/Linux OK.

### File List

**Crees:**
- internal/config/config.go
- internal/config/config_test.go
- internal/config/paths_windows.go
- internal/config/paths_unix.go
- cmd/client/main_test.go

**Modifies:**
- internal/ipc/messages.go
- internal/tray/tray.go
- internal/tray/tray_test.go
- cmd/client/main.go
- cmd/tray/main.go
- internal/service/service.go
- go.mod
- go.sum

**Supprimes:**
- internal/config/doc.go
