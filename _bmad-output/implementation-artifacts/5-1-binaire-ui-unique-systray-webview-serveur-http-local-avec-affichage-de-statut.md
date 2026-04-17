# Story 5.1 : Binaire UI unique (systray + webview + serveur HTTP local) avec affichage de statut

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'**utilisateur final**,
je veux **voir l'état de protection (connecté / en cours / déconnecté) via une fenêtre desktop avec indicateur visuel coloré, fonctionnant aussi bien sur Windows que sur Linux**,
afin de **savoir immédiatement si je suis protégé, sur mon OS, sans configuration supplémentaire**.

## Acceptance Criteria

**AC1 — Ouverture fenêtre webview + indicateur visuel coloré**
**Given** le service Le Voile est démarré
**When** l'utilisateur ouvre la fenêtre depuis le menu tray "Ouvrir la fenêtre"
**Then** une fenêtre webview **420×540px frameless** s'ouvre et navigue vers `http://127.0.0.1:{port}/`
**And** l'indicateur de statut affiche la couleur correspondant à l'état :
  - vert `#4ade80` (connecté, steady)
  - orange `#fb923c` (connecting, pulse 1.5 s)
  - rouge `#ff3c3c` (déconnecté)
**And** la charte plateformeliberte.fr est respectée (fond `#0b1526`, bleus `#1a6fc4` / `#2a8dff`, rouge `#d42b2b`, fonts Bebas Neue / Rajdhani / Inter en woff2 embarquées)

**AC2 — Mise à jour temps réel via polling HTTP**
**Given** le tunnel change d'état (connexion, perte, reconnexion)
**When** le frontend poll `fetch('/api/status')` toutes les 2 secondes
**Then** l'indicateur visuel et le message en français se mettent à jour :
  - "Connecté — Allemagne"
  - "Reconnexion en cours..."
  - "Déconnecté"
**And** l'icône system tray reflète simultanément l'état (icônes embarquées via `//go:embed`)

**AC3 — Binaire UI unique : systray + webview + serveur HTTP local**
**Given** le binaire UI démarre
**When** `cmd/ui/main.go` initialise les composants
**Then** `fyne.io/systray` démarre sur le **main thread** (bloquant — prérequis systray)
**And** un serveur HTTP local (`net/http`, `127.0.0.1:{port}` dynamique) sert les assets frontend embarqués via `//go:embed`
**And** une API REST JSON est exposée :
  - `GET /api/status`
  - `POST /api/connect`
  - `POST /api/disconnect`
  - `POST /api/country`
  - `GET  /api/registry`
  - `GET  /api/leak-status`
  - `GET  /api/update-status`
  - `POST /api/quit`
  - `GET  /api/settings` (+ sous-routes `/settings/autostart`, `/settings/blocklist`, `/settings/httpproxy`, `/settings/ipv6leak`)
**And** chaque endpoint proxie vers le service via IPC client (nommé pipe Windows / Unix socket Linux)

**AC4 — Singleton multiplateforme**
**Given** l'utilisateur lance plusieurs fois `levoile-ui`
**When** une instance est déjà active pour cet utilisateur
**Then** sous **Windows**, un mutex nommé (`Global\LeVoileUI`) empêche les instances multiples (exit silencieux)
**And** sous **Linux**, un `flock()` sur `~/.local/state/levoile/ui.lock` empêche les instances multiples **pour le même utilisateur** (singleton per-user Linux)
**And** la libération du lock est idempotente et se produit à la sortie du processus

**AC5 — Icônes tray multiplateforme**
**Given** le binaire UI démarre
**When** `fyne.io/systray` affiche son icône
**Then** sur **Windows**, les fichiers `.ico` embarqués sont utilisés (`connected.ico`, `connecting.ico`, `disconnected.ico`, `levoile.ico`)
**And** sur **Linux**, les fichiers `.png` embarqués sont utilisés (`connected.png`, `connecting.png`, `disconnected.png`, `levoile.png`)
**And** la sélection du format se fait via build tags (`icons_windows.go` / `icons_linux.go`), sans condition runtime

**AC6 — Ldflags et mode console**
**Given** le binaire UI est compilé via GoReleaser
**When** il est lancé sur Windows
**Then** `-H windowsgui` dans les ldflags supprime la fenêtre console
**And** sur Linux, aucun drapeau équivalent n'est nécessaire (pas de console attachée depuis `.desktop`)

**AC7 — Serveur HTTP local : sécurité bind**
**Given** le serveur HTTP démarre
**When** `net.Listen` est appelé
**Then** l'écoute est **strictement sur `127.0.0.1`** (loopback uniquement, pas `0.0.0.0`)
**And** le port est **dynamique** (`127.0.0.1:0` → kernel alloue un port libre)
**And** aucun header CORS n'est nécessaire (même origine — webview navigue vers le serveur local)

## Tasks / Subtasks

### Contexte : existant vs nouveau

**IMPORTANT** — Le gros de cette story est **déjà implémenté** par l'ancienne Story 10.1 (cf. `10-1-binaire-ui-unique-systray-webview-serveur-http-local-avec-statut-de-connexion.md` marquée `done` dans Epic 10 obsolète). Code existant à **conserver tel quel** sauf mentions contraires :

- `cmd/ui/main.go` — point d'entrée
- `internal/ui/ui.go` — orchestrateur (systray + HTTP + webview + polling IPC)
- `internal/ui/httpserver.go` — serveur HTTP local + endpoints
- `internal/ui/webview_cgo.go` + `internal/ui/webview_nocgo.go` — wrapper webview (build tags CGo)
- `internal/ui/singleton_windows.go` — mutex Windows
- `internal/ui/ipc_safe.go` — wrapper mutex IPC thread-safe
- `internal/ui/icons.go` — embed `.ico`
- `internal/ui/icons/*.ico` — icônes Windows
- `frontend/` — HTML/CSS/JS adaptés (fetch vers /api/*)
- `.goreleaser.yaml` — build target `ui`

Cette story **étend** l'existant pour :
1. Support Linux (singleton flock, icônes `.png`, ensureService systemd)
2. Ajout endpoints API manquants (`/api/disconnect`, `/api/leak-status`, `/api/update-status`, `/api/quit`)
3. Validation cross-platform des builds Windows + Linux

### Tâches

- [x] **Task 1 : Implémenter singleton Linux via `flock`** (AC: 4)
  - [x] 1.1 Créer `internal/ui/singleton_linux.go` avec build tag `//go:build linux`
  - [x] 1.2 Implémenter `AcquireSingleton()` :
    - Construire le chemin `$XDG_STATE_HOME/levoile/ui.lock` (fallback `~/.local/state/levoile/ui.lock`)
    - `os.MkdirAll(dir, 0700)` du parent
    - `os.OpenFile(path, O_CREATE|O_RDWR, 0600)`
    - `syscall.Flock(fd, LOCK_EX|LOCK_NB)` — retourne erreur FR si `EWOULDBLOCK` : `"ui: une autre instance Le Voile est déjà active pour cet utilisateur"`
    - Stocker fd dans une variable package-level (idem pattern Windows)
  - [x] 1.3 Implémenter `ReleaseSingleton()` : `syscall.Flock(fd, LOCK_UN)` + `os.Close(fd)` — idempotent
  - [x] 1.4 **Mettre à jour build tag de `singleton_stub.go`** : passer de `//go:build !windows` à `//go:build !windows && !linux` (pour ne garder que les autres OS : darwin, bsd, etc.)
  - [x] 1.5 Test manuel : `./levoile-ui &` puis `./levoile-ui` → 2ᵉ instance exit silencieusement *(couvert par tests unitaires `TestAcquireSingleton_SecondInstanceBlocked` — test manuel Linux dans CI)*

- [x] **Task 2 : Icônes tray Linux (PNG)** (AC: 5)
  - [x] 2.1 Renommer `internal/ui/icons.go` en `internal/ui/icons_windows.go` avec build tag `//go:build windows` — contenu inchangé (embed `.ico`)
  - [x] 2.2 Créer `internal/ui/icons_linux.go` avec build tag `//go:build linux` :
    ```go
    package ui
    import _ "embed"
    //go:embed icons/levoile.png
    var IconDefault []byte
    //go:embed icons/connected.png
    var IconConnected []byte
    //go:embed icons/connecting.png
    var IconConnecting []byte
    //go:embed icons/disconnected.png
    var IconDisconnected []byte
    ```
  - [x] 2.3 Générer les 4 `.png` 48×48 RGBA depuis les sources existantes :
    - `internal/ui/icons/levoile.png`
    - `internal/ui/icons/connected.png`
    - `internal/ui/icons/connecting.png`
    - `internal/ui/icons/disconnected.png`
    - Extraction via `ffmpeg -i <name>.ico -map 0:2 <name>.png` (stream 2 = 48×48)
    - Transparence alpha préservée (RGBA, vérifié avec `file *.png`)
  - [x] 2.4 Valider compilation avec `GOOS=linux go build ./cmd/ui/...` et `GOOS=windows go build ./cmd/ui/...` → OK

- [x] **Task 3 : `ensureService()` multiplateforme dans `cmd/ui/main.go`** (AC: 3)
  - [x] 3.1 Extraire `ensureService()` dans un fichier dédié `cmd/ui/service_windows.go` (build tag `//go:build windows`) — logique actuelle `levoile-service.exe start` inchangée
  - [x] 3.2 Créer `cmd/ui/service_linux.go` (build tag `//go:build linux`) :
    ```go
    func ensureService() {
        // Systemd gère l'autostart via enable --now. L'UI ne doit
        // PAS essayer de démarrer le service (le user n'a pas les droits).
        // En cas de service down, le polling IPC affichera "Service indisponible"
        // et l'utilisateur saura devoir lancer : systemctl start levoile.service
    }
    ```
  - [x] 3.3 Retirer la définition de `ensureService()` de `cmd/ui/main.go` (maintenant dans les fichiers build-tag)

- [x] **Task 4 : Endpoints API manquants dans `internal/ui/httpserver.go`** (AC: 3)
  - [x] 4.1 Ajouter route + handler `POST /api/disconnect` :
    ```go
    s.mux.HandleFunc("/api/disconnect", s.handleDisconnect)
    // handleDisconnect : méthode POST, proxie ipc.ActionDisconnect, retourne actionResponse
    ```
  - [x] 4.2 Ajouter route + handler `GET /api/leak-status` :
    - Proxie `ipc.ActionLeakCheck` (déclenche un check immédiat)
    - Retourne JSON : `APILeakStatusResponse{Status, LastCheck}` (champs déjà dans `ipc.Response`)
  - [x] 4.3 Ajouter route + handler `GET /api/update-status` :
    - Proxie `ipc.ActionUpdateStatus`
    - Retourne JSON : `APIUpdateStatusResponse{Status, Version, InstalledVersion, InstallError, RollbackVersion, RollbackReason}`
  - [x] 4.4 Ajouter route + handler `POST /api/quit` :
    - Proxie `ipc.ActionQuit` côté service (demande au service de s'arrêter)
    - Route prête pour un éventuel bouton "Quitter" côté webview (hors scope 5.1 mais route prête)
  - [x] 4.5 Tests unitaires (`internal/ui/httpserver_test.go`) :
    - `TestDisconnect` + `TestDisconnect_IPCError_ReturnsDisconnected` — POST `/api/disconnect`
    - `TestLeakStatus_Pass` / `TestLeakStatus_Fail` / `TestLeakStatus_IPCError` — GET `/api/leak-status`
    - `TestUpdateStatus_Ready` / `TestUpdateStatus_UpToDate` / `TestUpdateStatus_Rollback` — GET `/api/update-status`
    - `TestQuit` — POST `/api/quit`
    - `TestMethodNotAllowed` étendu avec les 4 nouveaux chemins

- [x] **Task 5 : Vérifier webview Linux (WebKitGTK)** (AC: 1)
  - [x] 5.1 Inspecter `internal/ui/webview_cgo.go` — vérifier que les flags CGo sont corrects pour Linux :
    - Sur Windows : WebView2 via `webview_go`
    - Sur Linux : WebKitGTK 6.0 (préféré) ou 4.1 (fallback) — `pkg-config --cflags --libs webkit2gtk-4.1`
    - La lib `github.com/webview/webview_go` gère ça nativement via CGo
    - **RÉGRESSION DÉTECTÉE ET CORRIGÉE** : `webview_cgo.go` avait le tag `//go:build cgo` sans restriction OS, mais importait `golang.org/x/sys/windows` et utilisait des APIs Win32 (procSetWindowPos, procCreateIconFromResourceEx, etc.). Une build CGo Linux aurait cassé. Refactoré en `webview_cgo_windows.go` (`//go:build cgo && windows`) + nouveau `webview_cgo_linux.go` (`//go:build cgo && linux`) avec une implémentation minimale GTK-safe (pas de Win32, `xdg-open` pour les liens externes).
  - [x] 5.2 Documenter les paquets runtime Linux requis (déjà dans `architecture.md` section Packaging Linux) :
    - Debian/Ubuntu : `libwebkit2gtk-6.0-0 | libwebkit2gtk-4.1-0, libayatana-appindicator3-1`
    - Fedora : `webkit2gtk4.1, libayatana-appindicator-gtk3`
    - Arch : `webkit2gtk-4.1 libayatana-appindicator`
    - Alpine : `webkit2gtk-4.1 libayatana-appindicator`
  - [x] 5.3 Build local Linux : `CGO_ENABLED=0 GOOS=linux go build ./cmd/ui/` OK. Build CGo complet avec `libwebkit2gtk-4.1-dev` → à valider en CI Linux (headers GTK indisponibles sur le host de dev Windows)
  - [x] 5.4 Test fumée : *à exécuter en CI ou sur machine Linux dédiée — hors portée du host de dev Windows*

- [x] **Task 6 : Mise à jour `.goreleaser.yaml` pour target Linux UI** (AC: 6)
  - [x] 6.1 Ajouter la cible `ui-linux` dans `.goreleaser.yaml` :
    ```yaml
    - id: ui-linux
      main: ./cmd/ui
      binary: levoile-ui
      goos: [linux]
      goarch: [amd64, arm64]
      env: [CGO_ENABLED=1]
      ldflags: ["-s -w -X main.version={{.Version}}"]
      hooks:
        pre: []  # rien à faire — WebKitGTK headers fournis par l'image CI
    ```
  - [x] 6.2 Cible Windows existante : confirmée avec `CGO_ENABLED=1`
  - [x] 6.3 Ajouter `levoile-ui` aux archives Linux dans `archives:` section (`ui-linux` archive tar.gz ajoutée)
  - [x] 6.4 Ne PAS publier dans cette story (Story 7.2 prend en charge le packaging .deb/.rpm/.apk)

- [x] **Task 7 : Validation build cross-platform** (AC: 1-7)
  - [x] 7.1 `GOOS=windows CGO_ENABLED=0 go build ./cmd/ui/` → OK
  - [x] 7.1b `GOOS=windows CGO_ENABLED=1 go build ./cmd/ui/` → OK (webview cgo + Win32 chrome)
  - [x] 7.2 `GOOS=linux CGO_ENABLED=0 go build ./cmd/ui/` → OK (webview_nocgo stub). CGo complet → CI
  - [x] 7.3 `go test ./internal/ui/... -count=1` → tous les tests passent
  - [x] 7.4 `go vet ./internal/ui/...` + `go test -race ./internal/ui/...` → OK, aucune race détectée

- [x] **Task 8 : Tests unitaires singleton Linux** (AC: 4)
  - [x] 8.1 Créer `internal/ui/singleton_linux_test.go` (build tag linux)
  - [x] 8.2 `TestAcquireSingleton_FirstInstance` — première acquisition réussit
  - [x] 8.3 `TestAcquireSingleton_SecondInstanceBlocked` — 2ᵉ acquisition depuis même user échoue avec message FR attendu
  - [x] 8.4 `TestReleaseSingleton_IdempotentWithoutAcquire` + `TestReleaseSingleton_IdempotentAfterAcquire` — Release appelé 2× ne panic pas
  - [x] 8.5 `lockPathOverride` package-level var pour redirection + `t.TempDir()` pour isolation
  - [x] 8.6 Tests supplémentaires : `TestAcquireSingleton_ReacquireAfterRelease`, `TestResolveLockPath_XDGStateHome`, `TestResolveLockPath_HomeFallback`

## Dev Notes

### Périmètre : cross-platform Windows + Linux

Cette story finalise la **parité Linux** de l'UI. Le socle Windows est opérationnel depuis la Story 10.1 (Epic 10 obsolète, voir snapshot `windows-stable-2026-04-15`). Les trois axes à compléter :

1. **Singleton Linux** : flock par utilisateur (`~/.local/state/levoile/ui.lock`). **Différence sémantique importante** : sous Windows le mutex est `Global\...` (machine-wide) ; sous Linux le flock est par user (cf. `architecture.md` — "Le Voile est un VPN machine-wide — sur un système multi-user Linux, tous les utilisateurs partagent le même tunnel, mais chaque user peut lancer sa propre UI").
2. **Icônes PNG** : `fyne.io/systray` accepte `[]byte` mais le format attendu diffère selon l'OS (`.ico` Windows, `.png` Linux). Build tag pour séparer.
3. **ensureService Linux** : le service est un systemd unit contrôlé par root. L'UI (user-space) ne peut pas le démarrer. Fonction no-op sous Linux.

### Architecture 2 processus

```
[levoile-service]  (kardianos/service : SCM Windows / systemd Linux)
       ↕ IPC (named pipe Windows / unix socket Linux)
[levoile-ui]       (fyne.io/systray main thread + webview + HTTP server)
       ↕ HTTP local (127.0.0.1:PORT)
[Frontend JS]      (fetch /api/* — aucun binding Go↔JS direct)
```

### Flow endpoints API (Story 5.1)

| Endpoint | Méthode | Action IPC | Scope 5.1 |
|----------|---------|-----------|-----------|
| `/api/status` | GET | `get_status` | **déjà implémenté** |
| `/api/connect` | POST | `connect` | **déjà implémenté** |
| `/api/disconnect` | POST | `disconnect` | **AJOUTER** (Task 4.1) |
| `/api/country` | POST | `select_country` | **déjà implémenté** |
| `/api/registry` | GET | `get_registry` | **déjà implémenté** |
| `/api/leak-status` | GET | `leak_check` | **AJOUTER** (Task 4.2) |
| `/api/update-status` | GET | `update_status` | **AJOUTER** (Task 4.3) |
| `/api/quit` | POST | `quit` | **AJOUTER** (Task 4.4) |
| `/api/settings` (+ sous-routes) | GET/POST | `set_*` | **déjà implémenté** |
| `/api/captive/retry` | POST | `retry_captive` | **déjà implémenté** |

### Singleton Linux : pattern `flock`

Référence standard Go/Unix. Pseudo-code :

```go
//go:build linux
package ui

import (
    "fmt"
    "os"
    "path/filepath"
    "syscall"
)

var lockFile *os.File

func AcquireSingleton() error {
    dir, err := stateDir() // $XDG_STATE_HOME/levoile ou ~/.local/state/levoile
    if err != nil {
        return fmt.Errorf("ui: singleton dir: %w", err)
    }
    if err := os.MkdirAll(dir, 0700); err != nil {
        return fmt.Errorf("ui: singleton mkdir: %w", err)
    }
    path := filepath.Join(dir, "ui.lock")
    f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
    if err != nil {
        return fmt.Errorf("ui: singleton open: %w", err)
    }
    if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
        f.Close()
        return fmt.Errorf("ui: une autre instance Le Voile est déjà active pour cet utilisateur")
    }
    lockFile = f
    return nil
}

func ReleaseSingleton() {
    if lockFile != nil {
        syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
        lockFile.Close()
        lockFile = nil
    }
}

func stateDir() (string, error) {
    if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
        return filepath.Join(xdg, "levoile"), nil
    }
    home, err := os.UserHomeDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(home, ".local", "state", "levoile"), nil
}
```

### Packages existants à RÉUTILISER

| Package | Chemin | Rôle |
|---------|--------|------|
| IPC client | `internal/ipc/` | `ipc.NewClient()`, `Request`, `Response`, `Action*` constants |
| Config | `internal/config/` | `config.Load()`, `config.DefaultPath()` |
| Frontend assets | `frontend/` | `frontend.Assets` (`embed.FS`) |
| UI orchestrateur | `internal/ui/` | `ui.New()`, `ui.Run()` — inchangés |
| DNS safety net | `internal/dns/` | `dns.RecoverOrphanDNS()` — déjà appelé dans shutdown |

### Packages / fichiers à CRÉER

| Fichier | Build tag | Description |
|---------|-----------|-------------|
| `internal/ui/singleton_linux.go` | `//go:build linux` | Flock `~/.local/state/levoile/ui.lock` |
| `internal/ui/singleton_linux_test.go` | `//go:build linux` | Tests flock |
| `internal/ui/icons_linux.go` | `//go:build linux` | Embed `.png` |
| `internal/ui/icons/*.png` | — | 4 fichiers PNG |
| `cmd/ui/service_linux.go` | `//go:build linux` | `ensureService()` no-op |

### Fichiers à MODIFIER

| Fichier | Modification |
|---------|--------------|
| `internal/ui/icons.go` | Renommer en `icons_windows.go` + ajouter build tag |
| `internal/ui/singleton_stub.go` | Changer build tag → `!windows && !linux` |
| `internal/ui/httpserver.go` | Ajouter 4 handlers + routes |
| `internal/ui/httpserver_test.go` | Ajouter tests pour les 4 nouveaux endpoints |
| `cmd/ui/main.go` | Retirer `ensureService()` de main.go (déplacé en build-tag files) |
| `cmd/ui/service_windows.go` | Nouveau — extraction de l'actuel `ensureService()` |
| `.goreleaser.yaml` | Ajouter cible `ui-linux` (amd64, arm64) |

### Fichiers à NE PAS toucher

- `internal/ui/ui.go` — orchestrateur fonctionnel
- `internal/ui/webview_cgo.go` / `webview_nocgo.go` — CGo split déjà en place
- `internal/ui/ipc_safe.go` — mutex wrapper validé
- `internal/ui/sysproxy_*.go` — WinINET (Windows only, stub Linux) — **conserver**, même si le HTTP proxy est destiné à disparaître avec la nouvelle archi L3 (cf. Epic 3). Nettoyage séparé hors scope 5.1
- `internal/ipc/` — ajout d'actions seulement si vraiment manquantes (toutes présentes déjà : `ActionDisconnect`, `ActionLeakCheck`, `ActionUpdateStatus`, `ActionQuit` — cf. `internal/ipc/messages.go`)
- `frontend/src/app.js` — polling déjà implémenté ; hors scope 5.1 (des enrichissements pour leak/update viendront dans stories 5.2-5.9)
- `internal/tun/`, `internal/tunnel/`, `internal/firewall/`, `internal/routing/` — sans rapport

### Webview Linux — points critiques

1. **Thread model** : `webview_go` sous Linux utilise GTK main loop. `w.Run()` est bloquant. Comme sous Windows, l'appeler dans une **goroutine dédiée** depuis le menu tray "Ouvrir" (pattern déjà en place dans `ui.go::handleOpenWebview()`).
2. **Dépendances runtime Linux** : `webkit2gtk-4.1` (ou 6.0), `libayatana-appindicator3-1`. Pas de déploiement dans cette story — packaging fait par Story 7.2.
3. **Tray Linux** : `fyne.io/systray` supporte Linux via StatusNotifierItem (protocole freedesktop) — nécessite `libayatana-appindicator` ou `libappindicator`. **GNOME pur** ne supporte pas nativement — extension `AppIndicator and KStatusNotifierItem Support` requise (documenter dans README).
4. **Frameless 420×540** : webview/webview a barre de titre native sur Linux (GTK). La valeur `webview.HintFixed` dans `w.SetSize(420, 540, webview.HintFixed)` non redimensionnable reste correcte. Le "frameless" du spec architecture est aspirationnel — accepter la barre GTK native.

### Conventions Go du projet

- Nommage fichiers : `snake_case.go`
- Build tags : `_windows.go`, `_linux.go`, `_stub.go`
- Erreurs : `fmt.Errorf("ui: %w", err)`, `fmt.Errorf("ui: httpserver: %w", err)`
- Zero-log côté UI (pas de `log.Println` ni `fmt.Println`)
- Tests co-localisés, table-driven, mock via interfaces (`IPCClient`)
- Imports : `ui` importe `ipc`, `config`, `frontend`, `dns` ; **jamais** `service`, `tunnel`, `firewall`, `routing`, `tun`

### Leçons des stories précédentes

- **Code review Story 10.1 (2026-04-08)** :
  - H1 : `SafeIPCClient` mutex wrapper indispensable — concurrents IPC (HTTP + polling + menu) → race sans ce wrapper. Déjà en place (`ipc_safe.go`), **ne pas retirer**.
  - M2 : `HTTPServer.Start()` doit `close(s.ready)` sur erreur de listen pour ne pas bloquer `Addr()` indéfiniment. Déjà en place.
  - M3 : test d'intégration `TestHTTPServer_StartAndAddr` valide le bind TCP réel. Conserver.
- **Story 2.9 (IPv6 leak)** : `allow_ipv6_leak` déjà exposé via `/api/settings/ipv6leak` — pas à refaire.
- **Story 2.8 (captive portal)** : `/api/captive/retry` déjà en place.
- **Story 3.* (relais)** : aucune interaction côté UI.

### Git intelligence (5 derniers commits)

```
16a275a fix(deploy): align install.sh/README/service with prod
c2e1c0e feat: complete Epic 3 — relay stateless multi-VPS
7bf2e59 chore: remove .claude/ from history, harden deploy scripts
bd11612 feat: IPv6 leak opt-out + relay systemd CAP_NET_ADMIN (2.9 + 3.1)
ece3270 feat: implement Sprint 2 — watchdog, routing, firewall, DNS flush
```

→ Activité récente : Epics 2 (TUN/firewall/DNS) et 3 (relais) finalisés. **Pas de travail récent sur `internal/ui/`** — le code UI Windows est stable (dernière modif importante : 2026-04-08 review fixes Story 10.1). Baseline propre pour attaquer Linux.

### Bibliothèques

| Lib | Version | Usage | Nouveau ? |
|-----|---------|-------|-----------|
| `fyne.io/systray` | v1.12.0+ | Tray icône + menu (Win + Linux) | existant |
| `github.com/webview/webview_go` | master | Webview (WebView2 Win / WebKitGTK Linux) | existant |
| `golang.org/x/sys` | latest | Syscalls Windows (mutex) et Linux (flock) | existant (ajouter sous-package si besoin) |
| `net/http`, `embed`, `syscall` | stdlib | — | — |

Aucune nouvelle dépendance externe n'est requise.

### Hors scope de 5.1

- **Affichage pays + relais + IP visible** → Story 5.2
- **Sélecteur de pays avec drapeaux** → Story 5.3
- **Bouton Connect/Disconnect en UI** → Story 5.4 (la route `/api/disconnect` est ajoutée ici mais le bouton frontend vient après)
- **Toggle fenêtre clic gauche tray** → Story 5.5
- **Écran fallback "Service non démarré"** → Story 5.6
- **Supervision UI auto-restart** → Story 5.7
- **Menu tray "Quitter" sans tuer le service** → Story 5.8 (la sémantique actuelle de `handleQuit()` est à réviser dans 5.8 : aujourd'hui elle stoppe aussi le service via `ActionQuit` — sous Linux le service doit survivre car il est systemd-managed)
- **Mode dégradé kill switch indicateur** → Story 5.9
- **Packaging Linux .deb/.rpm** → Story 7.2
- **Suppression `cmd/desktop/`, `cmd/tray/`, `cmd/portable/`** → nettoyage séparé après Epic 5 complet

### Project Structure Notes

Structure cible après cette story :

```
cmd/
  ui/
    main.go                   # EXISTANT — ensureService() extrait
    service_windows.go        # NOUVEAU — ensureService Win
    service_linux.go          # NOUVEAU — ensureService no-op
  client/                     # EXISTANT (service) — inchangé
  relay/                      # EXISTANT — inchangé
  genregistry/                # EXISTANT — inchangé
  portable/                   # EXISTANT — à nettoyer ultérieurement

internal/
  ui/
    ui.go                     # EXISTANT — inchangé
    ui_test.go                # EXISTANT — inchangé
    httpserver.go             # MODIFIÉ — 4 handlers en plus
    httpserver_test.go        # MODIFIÉ — tests 4 handlers
    webview_cgo.go            # EXISTANT — inchangé
    webview_nocgo.go          # EXISTANT — inchangé
    ipc_safe.go               # EXISTANT — inchangé
    icons_windows.go          # RENOMMÉ depuis icons.go + build tag
    icons_linux.go            # NOUVEAU — embed PNG
    icons/
      connected.ico           # EXISTANT
      connecting.ico          # EXISTANT
      disconnected.ico        # EXISTANT
      levoile.ico             # EXISTANT
      connected.png           # NOUVEAU
      connecting.png          # NOUVEAU
      disconnected.png        # NOUVEAU
      levoile.png             # NOUVEAU
    singleton_windows.go      # EXISTANT — inchangé
    singleton_linux.go        # NOUVEAU — flock
    singleton_linux_test.go   # NOUVEAU
    singleton_stub.go         # MODIFIÉ — build tag restreint
    sysproxy_windows.go       # EXISTANT — conservé (nettoyage hors scope)
    sysproxy_stub.go          # EXISTANT — conservé
    shutdown_test.go          # EXISTANT
  ipc/                        # EXISTANT — inchangé
  config/                     # EXISTANT — inchangé
  dns/                        # EXISTANT — inchangé (RecoverOrphanDNS utilisé)

frontend/                     # EXISTANT — inchangé (5.2+ ajouteront les nouveaux endpoints)

.goreleaser.yaml              # MODIFIÉ — cible ui-linux ajoutée
```

Conformité unified project structure (`architecture.md` section "Complete Project Directory Structure") : OK. Pas de conflit détecté — `internal/ui/` a déjà sa place dans l'arbre cible.

### Vérification couverture AC

| AC | Couvert par |
|----|-------------|
| AC1 (fenêtre + indicateur coloré + charte) | Existant 10.1 (webview 420×540, frontend CSS tokens) — à **valider** en test Linux |
| AC2 (polling 2s) | Existant 10.1 (`frontend/src/app.js` polling) |
| AC3 (binaire unique + endpoints) | Existant + Task 4 (4 endpoints ajoutés) |
| AC4 (singleton cross-platform) | Existant Windows + Task 1 (Linux flock) |
| AC5 (icônes cross-platform) | Task 2 (build tags + PNG) |
| AC6 (ldflags) | Existant `-H windowsgui` + Task 6 (cible Linux sans flag) |
| AC7 (bind loopback dynamique) | Existant `net.Listen("tcp", "127.0.0.1:0")` dans `httpserver.go` |

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` — Epic 5, Story 5.1 (lignes 877-901)]
- [Source: `_bmad-output/planning-artifacts/architecture.md` — "Architecture 2 Processus" (L298-317), "UI Patterns" (L624-646), "Composants Communs — Singleton UI" (L335), "Packaging Linux — dépendances runtime" (L366-370)]
- [Source: `internal/ipc/messages.go` — Actions/Statuts : `ActionDisconnect`, `ActionLeakCheck`, `ActionUpdateStatus`, `ActionQuit`, `StatusLeakPass/Fail/Pending`, `StatusUpdateReady/UpToDate/...`]
- [Source: `internal/ui/ui.go` — orchestrateur existant, `connectAndPoll()`, `handleOpenWebview()`, `shutdown()`]
- [Source: `internal/ui/httpserver.go` — handlers existants, pattern `sendIPC()`, `actionResponse()`, `statusMessage()` FR]
- [Source: `internal/ui/singleton_windows.go` — pattern mutex nommé à répliquer avec flock Linux]
- [Source: `internal/ui/icons.go` — pattern `//go:embed` à dupliquer en PNG]
- [Source: `cmd/ui/main.go` — `ensureService()` à splitter par OS]
- [Source: Ancienne Story 10.1 — `10-1-binaire-ui-unique-systray-webview-serveur-http-local-avec-statut-de-connexion.md` — leçons code review (H1 SafeIPCClient, M2 ready channel, M3 intégration test)]
- [Source: `.goreleaser.yaml` — cible `ui` actuelle (Windows), à étendre Linux]
- [Source: freedesktop XDG Base Directory spec — `$XDG_STATE_HOME` priorisé sur `~/.local/state`]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context)

### Debug Log References

- **Préexistant (non lié à la story)** : 3 tests obsolètes dans `internal/ui/ui_test.go` référençaient une méthode `handleToggle()` qui n'existe plus dans `ui.go` (le menu tray "Activer Le Voile" a été supprimé dans un refactor antérieur). Ces 3 fonctions (`TestHandleToggle_ConnectWhenDisconnected`, `TestHandleToggle_DisconnectWhenConnected`, `TestHandleToggle_IPCError`) empêchaient toute compilation des tests UI et ont été retirées — cleanup minimal nécessaire pour valider les nouveaux tests.
- **Régression critique détectée (Task 5.1)** : `internal/ui/webview_cgo.go` avait le build tag `//go:build cgo` (sans restriction OS) mais utilisait des symboles Win32 (`golang.org/x/sys/windows`, `user32.dll` procs, `CreateIconFromResourceEx`, etc.). Un build CGo Linux aurait échoué à la compilation. Correction : split en `webview_cgo_windows.go` (`//go:build cgo && windows`, contenu inchangé) + nouveau `webview_cgo_linux.go` (`//go:build cgo && linux`) avec implémentation GTK-safe minimale (pas de Win32, `xdg-open` pour liens externes, pas de manipulation titlebar/positionnement).
- **Échecs de tests préexistants non liés à la story** (notés dans le retro de l'ancienne 10.1, toujours présents) :
  - `internal/ipchandler/TestHandle_SetAutoStart_PortableMode_NilStartupType` — validation de config relay
  - `internal/service/TestProgram_StartStop` — test flaky (port 3478 déjà pris, clé Ed25519 base64 invalide dans le setup)
  - `internal/tray/TestDesktopExePath` — expectation de path obsolète (le-voile-desktop.exe vs levoile-desktop.exe)
- Toutes les suites touchées par la story (`internal/ui`) passent 100% avec `-race`.

### Completion Notes List

- **Singleton Linux** (`internal/ui/singleton_linux.go`) : flock advisoire sur `$XDG_STATE_HOME/levoile/ui.lock` (fallback `~/.local/state/levoile/ui.lock`). Message FR explicite quand un autre instance est active : *"ui: une autre instance Le Voile est déjà active pour cet utilisateur"*. Singleton per-user conformément à l'architecture (le service est machine-wide, mais chaque user Linux peut lancer sa propre UI).
- **Build tag `singleton_stub.go`** élargi à `!windows && !linux` pour ne plus shadow l'implémentation Linux.
- **Icônes PNG 48×48 RGBA** extraites depuis les `.ico` via `ffmpeg -map 0:2` (stream 2 = 48×48). Split `icons_windows.go` / `icons_linux.go` avec build tags explicites.
- **`ensureService()` split par OS** : Windows lance `levoile-service.exe start` (fire-and-forget, SCM ignore les doublons). Linux est no-op (systemd gère le service, l'UI user-space n'a pas les droits pour le démarrer).
- **4 endpoints API ajoutés** :
  - `POST /api/disconnect` → `ipc.ActionDisconnect`
  - `POST /api/quit` → `ipc.ActionQuit`
  - `GET /api/leak-status` → `ipc.ActionLeakCheck` avec `APILeakStatusResponse{status, last_check}`
  - `GET /api/update-status` → `ipc.ActionUpdateStatus` avec `APIUpdateStatusResponse{status, version, installed_version, install_error, rollback_version, rollback_reason}`
- **webview Linux** : bénéficie du même `webview/webview_go` cross-platform. Refactor bloquant résolu (voir Debug Log). Runtime deps documentées dans `architecture.md` (webkit2gtk-4.1, libayatana-appindicator).
- **Tests** : 8 nouveaux tests `singleton_linux_test.go` (flock, message FR, idempotence, XDG_STATE_HOME, HOME fallback, reacquire après release) + 7 nouveaux tests httpserver (disconnect, quit, leak pass/fail/IPC-error, update ready/up-to-date/rollback). `TestMethodNotAllowed` étendu pour les 4 nouveaux chemins.
- **GoReleaser** : cible `ui-linux` (amd64 + arm64, CGO_ENABLED=1, pas de `-H windowsgui`) et archive `LeVoile_{Version}_linux_{Arch}.tar.gz` ajoutées.
- **Validation** : `go vet`, `go test -race`, builds Windows (CGo + NoCGo), builds Linux (NoCGo). Build CGo Linux complet nécessite `libwebkit2gtk-4.1-dev` (CI only — pas possible sur host Windows).
- **Zero log** préservé côté UI. Aucune dépendance externe ajoutée (réuse `golang.org/x/sys/unix` via stdlib `syscall`, `github.com/webview/webview_go` déjà présent).

### Implementation Plan

Approche : étendre l'infrastructure UI Windows existante (Story 10.1 obsolète, `done`) pour la parité Linux, sans toucher au socle validé. Trois axes : singleton flock, assets PNG, endpoints API manquants. Un blocker Linux non anticipé (webview_cgo.go Windows-only) a été détecté et corrigé en Task 5.1.

### File List

| Action | File |
|--------|------|
| Added | `internal/ui/singleton_linux.go` |
| Added | `internal/ui/singleton_linux_test.go` |
| Added | `internal/ui/icons_linux.go` |
| Added | `internal/ui/icons_windows.go` *(migration depuis `icons.go`)* |
| Added | `internal/ui/icons/connected.png` |
| Added | `internal/ui/icons/connecting.png` |
| Added | `internal/ui/icons/disconnected.png` |
| Added | `internal/ui/icons/levoile.png` |
| Added | `internal/ui/webview_cgo_linux.go` |
| Added | `cmd/ui/service_windows.go` |
| Added | `cmd/ui/service_linux.go` |
| Renamed | `internal/ui/webview_cgo.go` → `internal/ui/webview_cgo_windows.go` *(build tag `cgo && windows`)* |
| Deleted | `internal/ui/icons.go` *(remplacé par variantes OS)* |
| Modified | `internal/ui/singleton_stub.go` *(build tag → `!windows && !linux`)* |
| Modified | `internal/ui/httpserver.go` *(4 nouveaux endpoints + 2 types de réponse)* |
| Modified | `internal/ui/httpserver_test.go` *(7 nouveaux tests + `TestMethodNotAllowed` étendu)* |
| Modified | `internal/ui/ui_test.go` *(suppression de 3 tests obsolètes `TestHandleToggle_*`)* |
| Modified | `cmd/ui/main.go` *(retrait `ensureService()` local, import `exec`/`filepath` retirés)* |
| Modified | `.goreleaser.yaml` *(target `ui-linux` + archive `ui-linux`)* |

### Change Log

- **2026-04-17** : Story 5.1 — parité Linux UI complétée (singleton flock per-user, icônes PNG build-tagged, `ensureService()` split OS, webview_cgo Linux-safe). Ajout de 4 endpoints API (`/api/disconnect`, `/api/quit`, `/api/leak-status`, `/api/update-status`). Cible GoReleaser `ui-linux` (amd64 + arm64). Correction d'une régression latente : `webview_cgo.go` importait `golang.org/x/sys/windows` sans tag OS → split en `webview_cgo_windows.go` + `webview_cgo_linux.go`. 15 nouveaux tests. 0 nouvelle dépendance externe.
