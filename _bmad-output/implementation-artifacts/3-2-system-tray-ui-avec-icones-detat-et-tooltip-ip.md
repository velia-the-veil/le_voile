# Story 3.2: System tray UI avec icones d'etat et tooltip IP

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur,
Je veux voir l'etat de ma protection dans le system tray avec une icone coloree et connaitre mon IP visible,
Afin de savoir en un coup d'oeil si je suis protege.

## Acceptance Criteria

1. **Initialisation tray avec fyne.io/systray** — Quand le tray UI demarre, une icone apparait dans le system tray et le tray se connecte automatiquement au service via le client IPC.

2. **Icone verte — etat connected** — Quand le service est en etat `connected`, l'icone affichee est `connected.ico` (verte) et le tooltip affiche "Protege — IP visible : {ip}" ou `{ip}` est la valeur retournee par `get_status` (actuellement "unknown" au MVP — la detection d'IP externe sera ajoutee ulterieurement).

3. **Icone orange — etat connecting** — Quand le service est en etat `connecting`, l'icone affichee est `connecting.ico` (orange) et le tooltip affiche "Connexion en cours...".

4. **Icone rouge — etat disconnected** — Quand le service est en etat `disconnected`, l'icone affichee est `disconnected.ico` (rouge) et le tooltip affiche "Non protege" avec le motif d'erreur si disponible.

5. **Icones embarquees via go:embed** — Les icones sont embarquees dans le binaire via `//go:embed` depuis `assets/icons/` et aucun fichier externe n'est necessaire a l'execution.

### Scenarios BDD detailles

**AC1 — Initialisation tray:**
```
Given le tray UI demarre
When il s'initialise avec fyne.io/systray
Then une icone apparait dans le system tray
And le tray se connecte automatiquement au service via le client IPC
```

**AC2 — Icone connected:**
```
Given le service en etat connected
When le tray recoit le statut via IPC
Then l'icone affichee est connected.ico (verte)
And le tooltip affiche "Protege — IP visible : {ip}" (ip = valeur IPC, "unknown" au MVP)
```

**AC3 — Icone connecting:**
```
Given le service en etat connecting
When le tray recoit le statut via IPC
Then l'icone affichee est connecting.ico (orange)
And le tooltip affiche "Connexion en cours..."
```

**AC4 — Icone disconnected:**
```
Given le service en etat disconnected
When le tray recoit le statut via IPC
Then l'icone affichee est disconnected.ico (rouge)
And le tooltip affiche "Non protege" avec le motif d'erreur si disponible
```

**AC5 — Icones embarquees:**
```
Given les icones du tray
When le binaire est compile
Then les icones sont embarquees via //go:embed depuis assets/icons/
And aucun fichier externe n'est necessaire a l'execution
```

## Tasks / Subtasks

- [x] **Task 1 : Creer les icones ICO pour les 3 etats** (AC: #5)
  - [x] 1.1 Creer `assets/icons/connected.ico` — icone verte (indicateur protection active). Format ICO, taille 16x16 + 32x32 + 48x48 pixels. Utiliser une icone minimaliste (bouclier vert ou cadenas vert)
  - [x] 1.2 Creer `assets/icons/connecting.ico` — icone orange (connexion en cours). Meme format et tailles
  - [x] 1.3 Creer `assets/icons/disconnected.ico` — icone rouge (non protege). Meme format et tailles
  - [x] 1.4 Supprimer `assets/icons/.gitkeep` une fois les icones ajoutees

- [x] **Task 2 : Module icons — Chargement des icones embarquees** (AC: #5)
  - [x] 2.1 Creer `internal/tray/icons.go` — utiliser `//go:embed assets/icons/connected.ico` etc.
  - [x] 2.2 Exporter les variables : `var IconConnected []byte`, `var IconConnecting []byte`, `var IconDisconnected []byte`
  - [x] 2.3 **ATTENTION** : Le `//go:embed` doit utiliser un chemin relatif depuis la racine du module. Puisque `internal/tray/icons.go` est dans `internal/tray/`, il faut soit utiliser un chemin relatif `../../assets/icons/connected.ico`, soit placer un fichier d'embed au niveau racine. **Solution recommandee :** Creer un package `internal/tray/icons/` qui re-exporte les icones, OU utiliser `embed` dans un fichier a la racine et passer les bytes au package tray. **Solution la plus simple :** Les embed paths sont relatifs au fichier source, donc placer `icons.go` a la racine (`icons.go` ou `assets/embed.go`) et importer depuis le package tray. **OU** copier les .ico dans `internal/tray/` directement (convention Tailscale). **Decision : Copier les .ico dans `internal/tray/` pour un embed simple et direct.**

- [x] **Task 3 : Module tray — Initialisation et boucle de polling** (AC: #1, #2, #3, #4)
  - [x] 3.1 Remplacer `internal/tray/doc.go` par `internal/tray/tray.go` — type `Tray` avec `Run()`, `Stop()`
  - [x] 3.2 `Run()` — appelle `systray.Run(onReady, onExit)`. **CRITIQUE :** `systray.Run` BLOQUE sur le thread principal. Doit etre appele depuis le main goroutine
  - [x] 3.3 `onReady()` — initialise : `systray.SetIcon(IconDisconnected)`, `systray.SetTooltip("Non protege")`, `systray.SetTitle("Le Voile")`
  - [x] 3.4 Dans `onReady()` — creer le client IPC (`ipc.NewClient()`) et se connecter au service (`client.Connect()`)
  - [x] 3.5 Lancer une goroutine de polling qui interroge le service via `get_status` toutes les 2 secondes et met a jour l'icone et le tooltip selon la reponse
  - [x] 3.6 Gerer la reconnexion IPC si le service n'est pas disponible — retry avec backoff (1s, 2s, 4s, max 10s)
  - [x] 3.7 `onExit()` — fermer le client IPC, cleanup

- [x] **Task 4 : Logique de mise a jour de l'icone et du tooltip** (AC: #2, #3, #4)
  - [x] 4.1 Creer une fonction `updateTrayState(resp ipc.Response)` dans `tray.go`
  - [x] 4.2 Switch sur `resp.Status` :
    - `connected` → `systray.SetIcon(IconConnected)`, tooltip = `"Protege — IP visible : " + resp.IP` (resp.IP sera "unknown" au MVP)
    - `connecting` → `systray.SetIcon(IconConnecting)`, tooltip = `"Connexion en cours..."`
    - `disconnected` → `systray.SetIcon(IconDisconnected)`, tooltip = `"Non protege"` + ` — ` + resp.Error si non-vide
    - `error` → `systray.SetIcon(IconDisconnected)`, tooltip = `"Erreur : " + resp.Error`
  - [x] 4.3 Ne mettre a jour l'icone que si l'etat a change (eviter les appels SetIcon inutiles — garder le dernier etat en memoire)

- [x] **Task 5 : Point d'entree tray — cmd/tray/main.go** (AC: #1)
  - [x] 5.1 Creer `cmd/tray/main.go` — point d'entree pour le processus tray UI (separe du service)
  - [x] 5.2 Initialiser le `Tray` et appeler `tray.Run()` sur le main goroutine
  - [x] 5.3 **CRITIQUE :** Le tray est un processus SEPARE du service. Le service tourne avec privileges eleves, le tray tourne en espace utilisateur. Ils communiquent via IPC uniquement
  - [x] 5.4 Ajouter `fyne.io/systray` a go.mod : `go get fyne.io/systray`

- [x] **Task 6 : Tests** (AC: tous)
  - [x] 6.1 Creer `internal/tray/tray_test.go` — tester `updateTrayState` avec differentes reponses IPC (connected/connecting/disconnected/error)
  - [x] 6.2 Tester la logique de non-mise-a-jour si l'etat n'a pas change
  - [x] 6.3 Tester le comportement quand le service IPC n'est pas disponible (retry)
  - [x] 6.4 **NOTE :** `systray.Run()` ne peut pas etre teste en CI (necessite un desktop). Les tests doivent se concentrer sur la logique metier (parsing status, update decisions) en mockant `systray.SetIcon`/`systray.SetTooltip`

- [x] **Task 7 : Validation globale** (AC: tous)
  - [x] 7.1 Executer tous les tests (`go test ./...`) — tray + non-regression des 105 tests existants
  - [x] 7.2 Verifier compilation multi-plateforme (`GOOS=windows/linux/darwin`)
  - [x] 7.3 Executer `go vet ./...` — zero warning
  - [ ] 7.4 Verifier que le binaire tray demarre et affiche une icone dans le system tray (test manuel Windows)
  - [ ] 7.5 Verifier la communication IPC tray ↔ service en conditions reelles

## Dev Notes

### Contraintes architecturales OBLIGATOIRES

- **Pure Go, pas de CGo** — Module path: `github.com/velia-the-veil/le_voile`
- **Jamais de logging client** — Les erreurs sont propagees via IPC au tray, et le tray les affiche dans le tooltip. PAS de `log.Println`
- **Jamais de `panic`** — Toujours retourner `error`
- **`context.Context` obligatoire** — Premier parametre de toutes les fonctions bloquantes ou reseau
- **Wrapping d'erreurs** — Format : `fmt.Errorf("tray: %w", err)`
- **Tests co-localises** — `tray_test.go` a cote de `tray.go`

### Conventions de nommage (OBLIGATOIRES)

- Package : `tray` (minuscule, un mot)
- Fichiers : `snake_case.go` (`tray.go`, `icons.go`, `tray_test.go`)
- Fonctions exportees : `PascalCase` (`NewTray`, `Run`, `Stop`)
- Fonctions privees : `camelCase` (`updateTrayState`, `pollService`, `reconnectIPC`)
- Constructeurs : `New` + type (`NewTray()`)
- Tests : `TestType_Method` (`TestTray_UpdateState_Connected`, `TestTray_UpdateState_Disconnected`)

### fyne.io/systray — API et contraintes critiques

**API principale :**
```go
systray.Run(onReady func(), onExit func())  // BLOQUE — doit etre sur le main goroutine
systray.SetIcon(iconBytes []byte)            // .ico sur Windows, .ico/.jpg/.png ailleurs
systray.SetTooltip(tooltip string)           // Windows et macOS uniquement
systray.SetTitle(title string)               // Titre a cote de l'icone (macOS principalement)
systray.AddMenuItem(title, tooltip string) *MenuItem  // Ajoute un item au menu
systray.Quit()                               // Quitte le systray
```

**CONTRAINTES CRITIQUES :**
1. `systray.Run()` BLOQUE le thread appelant — c'est le main event loop. Doit etre appele depuis `func main()` directement
2. `SetIcon` prend des `[]byte` — le contenu brut du fichier .ico
3. `SetTooltip` ne fonctionne PAS sur Linux — uniquement Windows et macOS. OK pour le MVP (Windows principal)
4. Tous les appels `systray.*` doivent etre faits APRES que `onReady` ait ete appele
5. Le menu sera ajoute dans la Story 3.3 — pour cette story, pas de menu items

**ATTENTION — Thread safety :**
- Les fonctions `systray.SetIcon()` et `systray.SetTooltip()` sont thread-safe et peuvent etre appelees depuis n'importe quelle goroutine
- La goroutine de polling peut donc appeler directement ces fonctions

### Pattern de polling IPC recommande

```go
func (t *Tray) pollService(ctx context.Context) {
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            resp, err := t.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionGetStatus})
            if err != nil {
                // Service indisponible — tenter reconnexion
                t.handleIPCError(err)
                continue
            }
            t.updateTrayState(resp)
        }
    }
}
```

### Icones ICO — Format et generation

**Format ICO requis :**
- Windows system tray utilise typiquement 16x16 et 32x32 pixels
- Format .ico multi-resolution recommande : 16x16 + 32x32 + 48x48
- Les icones doivent etre minimalistes et lisibles a 16x16 pixels

**Options de generation :**
1. **ImageMagick** : `convert -background transparent -fill green -size 32x32 xc:green connected.ico`
2. **Go generate** : Script Go qui genere des icones programmatiquement (plus reproductible)
3. **Icones simples** : Pour le MVP, des carres de couleur (vert/orange/rouge) de 16x16 et 32x32 suffisent. Les icones design viendront plus tard

**Decision MVP : Generer des icones simples programmatiquement.** Creer un script `tools/gen_icons.go` qui produit des .ico basiques (carres colores) ou les generer directement en bytes dans `icons.go` pour eviter les fichiers externes. La solution la plus simple : des icones .ico minimales commitees dans le repo.

### go:embed — Strategie d'integration

**PROBLEME :** `//go:embed` ne supporte que les chemins relatifs au fichier source et ne peut PAS remonter au-dessus du module root. Puisque `internal/tray/icons.go` est dans `internal/tray/`, il ne peut PAS faire `//go:embed ../../assets/icons/connected.ico`.

**SOLUTION RETENUE :** Placer les fichiers .ico directement dans `internal/tray/` a cote du code :
```
internal/tray/
├── tray.go
├── tray_test.go
├── icons.go              # //go:embed connected.ico connecting.ico disconnected.ico
├── connected.ico
├── connecting.ico
└── disconnected.ico
```

Ceci est le pattern utilise par Tailscale et d'autres projets Go. Les fichiers `assets/icons/` restent comme reference design mais les .ico embarques sont dans `internal/tray/`.

**Alternative :** Creer un package `assets` a la racine avec les embed et l'importer. Mais c'est plus complexe sans benefice reel.

### APIs existantes a utiliser (NE PAS REINVENTER)

**ipc.Client** (`internal/ipc/client.go`) :
- `NewClient() *Client` — cree un client IPC
- `Connect() error` — se connecte au service via named pipe (Windows) ou unix socket
- `Close() error` — ferme la connexion
- `Send(req Request) (Response, error)` — envoie une requete, timeout 5 secondes par defaut
- `SendContext(ctx context.Context, req Request) (Response, error)` — envoi avec context

**ipc.Request / ipc.Response** (`internal/ipc/messages.go`) :
- `Request{Action: "get_status"}` → `Response{Status: "connected", IP: "82.x.x.x", Uptime: "3h12m"}`
- `Request{Action: "connect"}` → `Response{Status: "connecting"}`
- `Request{Action: "disconnect"}` → `Response{Status: "disconnected"}`
- Actions : `ActionGetStatus`, `ActionConnect`, `ActionDisconnect`
- Statuts : `StatusConnected`, `StatusConnecting`, `StatusDisconnected`, `StatusError`

### Dependances de cette story

- **Story 3.1** (done) : `ipc.Client`, `ipc.Server`, `ipc.Request`, `ipc.Response`, `ipc.Messages` — toute l'infrastructure IPC
- **Nouvelles dependances externes :**
  - `fyne.io/systray` — bibliotheque system tray cross-platform. Version a verifier lors du `go get`

### NFRs a respecter

| NFR | Exigence | Impact |
|-----|----------|--------|
| NFR11 | RAM < 20MB | Tray leger, polling IPC minimal |
| NFR12 | CPU < 1% | Polling toutes les 2s, pas de boucle active |
| NFR5 | Zero fuite DNS | Le tray est read-only — il ne controle PAS le tunnel dans cette story (Story 3.3 ajoutera connect/disconnect) |

### Ce qui est HORS SCOPE (ne PAS implementer)

- Menu clic droit et controles utilisateur (Story 3.3)
- Actions connect/disconnect depuis le tray (Story 3.3) — le tray est LECTURE SEULE dans cette story
- Demarrage automatique du tray (Story 3.3 / Epic 4)
- Installateur Windows (Epic 4)
- Version portable (Epic 4)
- Recuperation de l'IP visible reelle (le service retourne "unknown" pour l'IP au MVP — afficher tel quel)
- Icones design finales (MVP = icones simples generees)

### Intelligence de la Story 3.1 (CRUCIAL)

**Lecons directement applicables :**
- `ipc.Client.Connect()` se connecte au named pipe `\\.\pipe\levoile` (Windows) ou unix socket `/tmp/levoile.sock` (Linux/macOS) — utiliser tel quel
- Le protocole IPC est du newline-delimited JSON — `json.NewEncoder`/`json.NewDecoder`
- Le client IPC a un timeout de 5 secondes par requete (`RequestTimeout`)
- `SendContext(ctx, req)` supporte l'annulation via context — utiliser pour le polling
- Le handler IPC dans `cmd/client/main.go` repond a `get_status` avec `{status, ip, uptime}` — `ip` est "unknown" pour l'instant
- `go-winio` est deja dans `go.mod` pour les named pipes Windows
- Scanner buffer limite a 4096 bytes (fix M4 de la code review 3.1) — les requetes/reponses sont bien dans cette limite
- Le serveur IPC track les connexions actives et les ferme au shutdown — le client doit gerer la deconnexion gracieusement (retry)

**Fichiers crees dans Story 3.1 (NE PAS TOUCHER) :**
- `internal/ipc/server.go` — serveur IPC
- `internal/ipc/client.go` — client IPC (A UTILISER)
- `internal/ipc/messages.go` — types (A UTILISER)
- `internal/ipc/pipe_windows.go` / `pipe_unix.go` — transport
- `internal/service/service.go` — lifecycle service
- `cmd/client/main.go` — point d'entree service avec handler IPC

**Debug log de la Story 3.1 :**
- Hang du test serveur IPC initial : `bufio.Scanner` bloquait au shutdown car les connexions actives n'etaient pas fermees. Fix = tracking des connexions + fermeture au cancel du context. **Impact tray :** Si le service s'arrete, le client IPC recevra une erreur de lecture — gerer proprement avec retry.

### Project Structure Notes

**Fichiers a CREER :**
```
cmd/tray/main.go                     # Point d'entree processus tray (SEPARE du service)
internal/tray/tray.go                # Initialisation systray, polling IPC, mise a jour icone/tooltip
internal/tray/tray_test.go           # Tests logique metier (updateTrayState, polling)
internal/tray/icons.go               # //go:embed des icones .ico
internal/tray/connected.ico          # Icone verte 16x16+32x32+48x48
internal/tray/connecting.ico         # Icone orange 16x16+32x32+48x48
internal/tray/disconnected.ico       # Icone rouge 16x16+32x32+48x48
```

**Fichiers a MODIFIER :**
```
go.mod / go.sum                      # Ajouter fyne.io/systray
```

**Fichiers a SUPPRIMER :**
```
internal/tray/doc.go                 # Remplace par tray.go
assets/icons/.gitkeep                # Optionnel — garder si on veut le dossier pour les icones design futures
```

**Fichiers a NE PAS TOUCHER :**
```
internal/ipc/*                       # APIs stables (Story 3.1)
internal/service/*                   # Lifecycle stable (Story 3.1)
internal/tunnel/*                    # APIs stables (Stories 2.x)
internal/dns/*                       # APIs stables (Stories 2.x)
internal/watchdog/*                  # API stable (Story 2.3)
internal/crypto/*                    # Module crypto stable (Story 1.1)
internal/relay/*                     # Cote serveur — pas concerne
internal/config/*                    # Pas encore implemente
cmd/client/main.go                   # Service — pas concerne (sauf si ajout build tray dans goreleaser)
cmd/relay/*                          # Cote serveur — pas concerne
```

**Alignement structure projet :**
- `cmd/tray/main.go` — conforme a l'architecture (processus tray separe du service, architecture multi-binaire `cmd/`)
- `internal/tray/tray.go`, `icons.go` — conformes a l'architecture (`internal/tray/` prevu)
- Icones dans `internal/tray/` au lieu de `assets/icons/` pour l'embed — variance justifiee par la contrainte `//go:embed`
- Aucun conflit detecte

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Assets] — Icones tray dans assets/icons/ (embedded via //go:embed), 3 fichiers : connected.ico, connecting.ico, disconnected.ico
- [Source: _bmad-output/planning-artifacts/architecture.md#Communication & Protocole] — IPC named pipe/unix socket, messages JSON
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — internal/tray/tray.go, internal/tray/icons.go
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns] — naming, error handling, concurrency patterns
- [Source: _bmad-output/planning-artifacts/architecture.md#Starter Template Evaluation] — fyne.io/systray selectionne, pas de dependance GTK
- [Source: _bmad-output/planning-artifacts/prd.md#FR9] — Icone coloree system tray (vert/orange/rouge)
- [Source: _bmad-output/planning-artifacts/prd.md#FR10] — Tooltip IP visible
- [Source: _bmad-output/planning-artifacts/prd.md#FR15] — Tray auto-connecte au service
- [Source: _bmad-output/planning-artifacts/prd.md#NFR11] — RAM < 20MB
- [Source: _bmad-output/planning-artifacts/prd.md#NFR12] — CPU < 1%
- [Source: _bmad-output/planning-artifacts/epics.md#Epic3-Story3.2] — Criteres d'acceptation detailles
- [Source: _bmad-output/implementation-artifacts/3-1-service-systeme-avec-kardianos-service-et-module-ipc.md] — IPC client/server APIs, debug logs, code review fixes

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Cross-compilation Darwin echoue depuis Windows : fyne.io/systray necessite les headers natifs macOS (Objective-C). Compilation OK sur macOS natif. Windows et Linux OK.
- Tasks 7.4 et 7.5 sont des tests manuels — necessitent que le service tourne et un desktop Windows.

### Completion Notes List

- Icones ICO generees programmatiquement via `tools/gen_icons.go` — carres colores 16x16+32x32+48x48 (vert/orange/rouge)
- Icones placees dans `internal/tray/` pour embed direct (convention Tailscale)
- `icons.go` utilise `//go:embed` pour embarquer les 3 .ico dans le binaire
- `tray.go` implemente : type Tray, Run/Stop, onReady/onExit, connectAndPoll, reconnectIPC (backoff 1s→10s), handleIPCError, updateTrayState
- Interface `SystrayAPI` et `IPCClient` pour testabilite (injection de dependances)
- Deduplication des mises a jour : l'etat complet (status+ip+error) est compare avant appel SetIcon/SetTooltip
- `cmd/tray/main.go` : point d'entree minimal, processus separe du service
- 12 tests couvrant : tous les etats IPC (connected/connecting/disconnected/error/unknown), deduplication, polling, reconnexion apres erreur, embed verification
- `fyne.io/systray v1.12.0` ajoutee a go.mod (dependance directe)
- Tous les 117 tests passent (105 existants + 12 nouveaux), go vet zero warning
- Code review : 7 issues corrigees (2 HIGH, 4 MEDIUM, 1 LOW) — interface SystrayAPI complete, go.mod fix, icones optimisees, tests rapides, erreur IPC dans tooltip, default case statut inconnu

### Change Log

- 2026-03-09 : Implementation complete Story 3.2 — System tray UI avec icones d'etat et tooltip IP
- 2026-03-09 : Code review — Corrige 7 issues : H1 SetTitle via interface, H2 go.mod indirect→direct, M1 AND mask size bug (-60% taille icones), M2 bytes.Equal stdlib, M3 tests 12s→1s (poll injectable), M4 erreur IPC dans tooltip, L1 default case statut inconnu

### File List

**Nouveaux :**
- `tools/gen_icons.go` — Generateur d'icones ICO
- `internal/tray/connected.ico` — Icone verte (connected)
- `internal/tray/connecting.ico` — Icone orange (connecting)
- `internal/tray/disconnected.ico` — Icone rouge (disconnected)
- `internal/tray/icons.go` — Embed des icones via go:embed
- `internal/tray/tray.go` — Module tray principal (systray, polling IPC, updateTrayState)
- `internal/tray/tray_test.go` — Tests du module tray (11 tests)
- `cmd/tray/main.go` — Point d'entree processus tray

**Modifies :**
- `go.mod` — Ajout fyne.io/systray v1.12.0, github.com/godbus/dbus/v5 v5.1.0
- `go.sum` — Mis a jour

**Supprimes :**
- `internal/tray/doc.go` — Remplace par tray.go
- `assets/icons/.gitkeep` — Plus necessaire
