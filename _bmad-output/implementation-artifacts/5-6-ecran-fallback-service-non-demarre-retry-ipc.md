# Story 5.6 : Écran fallback « Service non démarré » + retry IPC

Status: ready-for-dev

<!-- Note : Validation optionnelle. Lancer `validate-create-story` pour un QC avant `dev-story`. -->

## Story

As a **utilisateur final**,
I want **voir un écran clair et non-effrayant si le service Le Voile n'est pas démarré, avec la commande shell exacte (adaptée à mon OS) pour le lancer moi-même, pendant que l'UI retente la connexion IPC en arrière-plan**,
so that **je puisse résoudre la situation sans contacter le support et que l'UI revienne automatiquement à son état normal dès que le service est actif**.

Contexte FR : FR13c du PRD — « Si l'UI ne peut pas joindre l'IPC du service, écran fixe avec titre "Service Le Voile non démarré" et commande shell selon OS détecté (systemctl/sc). Retry IPC toutes les 5s ».

## Acceptance Criteria

**AC1 — IPC injoignable au démarrage → écran fallback affiché**

- **Given** l'UI (`levoile-ui`) démarre alors que le service n'est pas joignable (service arrêté, crashé, container sans systemd, named pipe absent)
- **When** la première tentative de connexion IPC échoue (timeout ou erreur de transport)
- **Then** l'UI n'affiche ni le panneau Statut normal, ni la zone Paramètres
- **And** un écran fallback dédié est affiché dans la webview avec le titre **« Service Le Voile non démarré »** (h1/h2, charte plateformeliberte.fr respectée)
- **And** un court message explique la situation en français non technique

**AC2 — Commande OS-spécifique visible**

- **Given** l'écran fallback est affiché
- **When** l'OS hôte est détecté par le binaire UI via `runtime.GOOS`
- **Then** sur **Linux**, le texte affiche : « Le service Le Voile n'est pas démarré. Ouvrez un terminal et lancez : » suivi du bloc monospace : `sudo systemctl start levoile.service`
- **And** sur **Windows**, le texte affiche : « Le service Le Voile n'est pas démarré. Ouvrez Services.msc et démarrez "Le Voile Service", ou utilisez `sc start levoile-service` en tant qu'administrateur »
- **And** la commande est sélectionnable (user-select: text) afin que l'utilisateur puisse la copier/coller

**AC3 — Retry IPC toutes les 5 secondes en arrière-plan**

- **Given** l'écran fallback est affiché
- **When** le temps passe
- **Then** l'UI tente une reconnexion IPC **toutes les 5 secondes** (cadence fixe, pas d'exponential backoff)
- **And** chaque tentative échouée ne modifie pas l'écran visible (pas de flash, pas de compteur visible — discret)
- **And** l'icône system tray reste en état « Service indisponible » (icône disconnected + tooltip « Service indisponible »)

**AC4 — Retour automatique à l'UI normale quand le service démarre**

- **Given** l'écran fallback est affiché et retry IPC en cours
- **When** entre deux retries, l'utilisateur démarre le service (via la commande affichée ou autrement) et la connexion IPC réussit
- **Then** l'écran fallback disparaît sans intervention utilisateur
- **And** l'UI normale (panneau Statut) s'affiche avec l'état courant issu de `/api/status`
- **And** le polling 2s du statut reprend normalement
- **And** l'icône tray reflète l'état réel (connecté/en cours/déconnecté)

**AC5 — IPC qui tombe pendant l'utilisation → fallback affiché**

- **Given** l'UI est opérationnelle, la fenêtre webview est ouverte, le service répondait jusque-là
- **When** le service est arrêté manuellement (ex : `sudo systemctl stop levoile.service`) ou crashe en cours de session
- **Then** après la première erreur IPC détectée par le polling (≤ 2s de polling + 5s de retry IPC), l'écran fallback remplace le panneau Statut
- **And** l'icône tray passe à « Service indisponible »
- **And** les retries 5s reprennent jusqu'à rétablissement

**AC6 — Pas d'impact sur le lifecycle UI**

- **Given** l'écran fallback est affiché
- **When** l'utilisateur clique sur « Quitter » dans le tray OU ferme la fenêtre webview
- **Then** la séquence de shutdown UI s'exécute sans bloquer sur l'IPC (timeout court, pas de `panic`)
- **And** le processus UI termine proprement
- **And** aucune ressource système (proxy WinINET, webview, serveur HTTP local, mutex singleton) n'est laissée orpheline

## Tasks / Subtasks

- [ ] **T1 — API : distinguer « service unreachable » de « tunnel disconnected » (AC1, AC3, AC5)**
  - [ ] T1.1 Ajouter dans `APIStatusResponse` (internal/ui/httpserver.go) deux champs : `ServiceReachable bool` (`json:"service_reachable"`) et `ServiceStartCommand string` (`json:"service_start_command,omitempty"`)
  - [ ] T1.2 Modifier `sendIPC` (internal/ui/httpserver.go) pour propager l'erreur IPC jusqu'à `handleStatus` au lieu d'absorber avec `{Status: StatusDisconnected}` (introduire un retour `(ipc.Response, error)` ou un booléen de reachability)
  - [ ] T1.3 Dans `handleStatus` : si l'erreur IPC persiste, renvoyer `{ServiceReachable: false, ServiceStartCommand: serviceStartCommand()}` et HTTP 200 (garder le JSON parsable)
  - [ ] T1.4 Pour tous les autres endpoints (`/api/connect`, `/api/country`, `/api/settings*`, `/api/captive/retry`) : en cas d'IPC unreachable, renvoyer HTTP 503 + JSON `{"error":"service_unreachable"}` pour que le frontend sache que l'action est inopérante

- [ ] **T2 — Détection OS et commande de démarrage (AC2)**
  - [ ] T2.1 Créer `internal/ui/service_command.go` avec la fonction `ServiceStartCommand() string` retournant :
    - Linux : `"sudo systemctl start levoile.service"`
    - Windows : `"sc start levoile-service"` (la phrase complète « Ouvrez Services.msc ... » est construite côté frontend à partir de `navigator.platform` OU retournée sous forme structurée — voir T2.2)
  - [ ] T2.2 Préférer une structure : ajouter dans `APIStatusResponse` un champ `ServiceStartHint struct { OS string; Command string; HumanMessage string }` sérialisé en JSON `service_start_hint` — `OS` = `"linux"` ou `"windows"` ; `HumanMessage` pré-traduit en français selon OS
  - [ ] T2.3 Utiliser `runtime.GOOS` côté Go (pas la détection JS) pour garantir la cohérence avec l'OS du service qu'on essaie de démarrer (le service tourne sur la même machine que l'UI par design — architecture 2 processus)

- [ ] **T3 — Boucle de retry IPC fixée à 5s (AC3)**
  - [ ] T3.1 Dans `internal/ui/ui.go`, introduire une constante `ipcRetryInterval = 5 * time.Second` dédiée au retry post-connexion (à distinguer du backoff initial)
  - [ ] T3.2 Remplacer la boucle `reconnectIPC` exponentielle (1s→10s) par un ticker 5s fixe après le premier échec — conserver un essai immédiat au démarrage pour ne pas retarder inutilement le chemin nominal
  - [ ] T3.3 Documenter par un commentaire ciblé **« Cadence fixe FR13c — ne pas ré-introduire un backoff »** (une ligne, pas plus)
  - [ ] T3.4 Préserver l'annulation via `ctx.Done()` pour un shutdown propre (AC6)

- [ ] **T4 — Frontend : panneau fallback dédié (AC1, AC2, AC4, AC5)**
  - [ ] T4.1 Dans `frontend/index.html`, ajouter un nouveau `<div class="panel" id="panel-service-down">` à la racine de `.panel-area` (pas dans la sidebar — écran plein, sans onglet sélectionnable)
  - [ ] T4.2 Structure HTML :
    - Titre h2 : « Service Le Voile non démarré »
    - Paragraphe explicatif (utiliser `service_start_hint.human_message` rendu côté JS)
    - Bloc `<pre class="shell-cmd">` contenant `service_start_hint.command` (sélectionnable, monospace)
    - Petit indicateur discret « Nouvelle tentative dans 5s... » (spinner CSS léger, pas intrusif)
  - [ ] T4.3 Dans `frontend/src/app.js`, modifier `updateUI(s)` :
    - Si `s.service_reachable === false` → `showFallback(s.service_start_hint)` (cache tous les autres panels, affiche `panel-service-down`, désactive les onglets sidebar)
    - Si `s.service_reachable === true` → restaure le panel courant (`showPanel(currentPanel)`) si on sortait du fallback
  - [ ] T4.4 Dans `frontend/src/style.css`, styliser `#panel-service-down` et `.shell-cmd` selon charte (fond `#0b1526`, accent `#d42b2b` subtil pour le titre, police Inter pour le texte, police monospace système pour la commande, bordure arrondie 8px, padding 16px)
  - [ ] T4.5 S'assurer que le bouton titlebar « Quitter » (`__close()`) reste fonctionnel depuis le panneau fallback (AC6)

- [ ] **T5 — Tray cohérent avec l'état service-unreachable (AC3, AC4)**
  - [ ] T5.1 Dans `handleIPCError` (internal/ui/ui.go), conserver le comportement actuel : `SetIcon(IconDisconnected)` + `SetTooltip("Service indisponible")` — aucune régression
  - [ ] T5.2 Dans `updateTrayState`, lors du retour à un état nominal (`StatusConnected/StatusConnecting/StatusDisconnected`), invalider `u.last` pour forcer la mise à jour immédiate du tooltip qui peut avoir été figé à « Service indisponible »
  - [ ] T5.3 Ne PAS ajouter de nouveau `StatusServiceDown` dans `internal/ipc/messages.go` — le service ne peut par définition pas émettre ce statut (il est le service). Le flag vit uniquement côté UI/HTTP API.

- [ ] **T6 — Tests unitaires et d'intégration (prévention régression FR13c)**
  - [ ] T6.1 `internal/ui/httpserver_test.go` — ajouter un test : IPC client qui retourne erreur persistante → `/api/status` renvoie `{service_reachable:false, service_start_hint:{...}}`, HTTP 200, JSON parsable
  - [ ] T6.2 `internal/ui/httpserver_test.go` — test : après que IPC client revient disponible, `/api/status` renvoie `service_reachable:true` (mock qui bascule)
  - [ ] T6.3 `internal/ui/service_command_test.go` (nouveau) — table-test GOOS via `runtime.GOOS` (test unitaire ne runtime-mock pas GOOS → utiliser une indirection `osDetector func() string` avec default `runtime.GOOS`, override en test)
  - [ ] T6.4 `internal/ui/ui_test.go` — vérifier que la cadence de retry est 5s (injecter un clock mock ou vérifier la constante exportée / interface horloge)
  - [ ] T6.5 Test d'intégration manuel documenté dans la section « Test manuel » ci-dessous (pas de test e2e automatisé pour la webview — contrainte CGo/WebView2)

- [ ] **T7 — Build tags et cross-platform**
  - [ ] T7.1 Vérifier que `service_command.go` compile sous les deux tags (`GOOS=linux` et `GOOS=windows`) sans build tag spécifique (simple switch `runtime.GOOS`)
  - [ ] T7.2 Lancer `go vet ./...` et `gofmt -l ./internal/ui ./frontend` — zero warning

## Dev Notes

### Architecture et patterns à suivre

- **Source : _bmad-output/planning-artifacts/architecture.md** — sections `UI Patterns`, `IPC`, `Architecture 2 processus`, `APIs and Integrations`
- **Source : _bmad-output/planning-artifacts/prd.md** — FR13c, NFR (reconnexion / résilience)
- **Source : _bmad-output/planning-artifacts/epics.md** — Epic 5 Story 5.6 (ligne 986+)

### Frontière IPC respectée

- Aucun import direct `internal/service` depuis `internal/ui` — strictement via IPC (`internal/ipc`) [Source: architecture.md#Frontière IPC]
- Le flag `ServiceReachable` n'est PAS un statut IPC (le service ne l'émet pas). Il est calculé côté `httpserver.go` au niveau client à partir de l'erreur de transport.
- Ne pas ajouter de constante `StatusServiceDown` dans `internal/ipc/messages.go` — pollution conceptuelle [Source: architecture.md#IPC Actions]

### Code structure cible (inchangé)

```
cmd/ui/main.go                        # point d'entrée UI (inchangé)
internal/ui/
  ui.go                               # MODIFIÉ : reconnectIPC cadence 5s fixe (T3)
  httpserver.go                       # MODIFIÉ : APIStatusResponse étendu (T1)
  service_command.go                  # NOUVEAU : ServiceStartCommand() + StartHint (T2)
  service_command_test.go             # NOUVEAU : table tests OS (T6.3)
  httpserver_test.go                  # MODIFIÉ : nouveaux cas service-unreachable (T6.1, T6.2)
  ui_test.go                          # MODIFIÉ : test cadence 5s (T6.4)
frontend/
  index.html                          # MODIFIÉ : panneau fallback (T4.1, T4.2)
  src/app.js                          # MODIFIÉ : bascule panel + polling (T4.3)
  src/style.css                       # MODIFIÉ : styles fallback (T4.4)
```

### Éléments déjà en place (à réutiliser, ne pas réinventer)

- `handleIPCError` dans `internal/ui/ui.go:306` gère déjà le tooltip « Service indisponible » → garder, ne pas dupliquer la logique de détection
- `reconnectIPC` dans `internal/ui/ui.go:288` → MODIFIER cadence (pas réécrire la structure)
- `sendIPC` dans `internal/ui/httpserver.go:201` → point central pour détecter reachability
- `SafeIPCClient` (internal/ui/ipc_safe.go) → ne pas bypasser, toujours passer par lui pour l'accès IPC concurrent
- Frontend : pattern `showPanel(name)` dans `frontend/src/app.js:36` → étendre (ne pas forker)
- Charte plateformeliberte.fr déjà appliquée (`frontend/src/style.css`) → réutiliser les variables couleur existantes

### Pièges LLM à éviter

- **NE PAS** introduire un nouveau champ `Status = "service-unreachable"` dans les constantes IPC. C'est un état UI, pas un état du service. Garder la clean separation.
- **NE PAS** utiliser `navigator.userAgent` pour détecter l'OS côté JS : le frontend est embarqué, la webview de Windows peut rapporter un UA non-Windows selon WebView2. Utiliser `runtime.GOOS` côté Go (T2.3).
- **NE PAS** afficher d'instructions qui demandent à l'utilisateur de relancer l'UI (« redémarrez Le Voile »). Le retry 5s doit gérer la reconnexion — c'est tout l'intérêt de FR13c.
- **NE PAS** ré-introduire un exponential backoff « parce que c'est plus élégant ». L'AC3 impose 5s fixe ; la simplicité est intentionnelle (comportement prévisible pour l'utilisateur).
- **NE PAS** casser le chemin nominal : si le service est déjà up au démarrage, aucun écran fallback ne doit apparaître, même une fraction de seconde (pas de flash).
- **NE PAS** bloquer le shutdown sur le retry IPC (AC6). Le `context.Context` du `Run()` doit propager l'annulation — vérifier `select { case <-ctx.Done(): return ... }` dans la boucle de retry.
- **NE PAS** modifier le schéma du `/api/status` de façon cassante : les champs `ServiceReachable` / `ServiceStartHint` sont additifs. Les anciens consommateurs frontend continuent à lire `status`, `ip`, etc.
- **NE PAS** créer une nouvelle route `/api/service-status` séparée : un seul polling `/api/status` doit suffire (minimise la charge CPU de la webview et la latence de retour à l'UI normale).

### Test manuel (reproductible pendant dev-story)

**Linux :**
```bash
# 1. Arrêter le service avant de lancer l'UI
sudo systemctl stop levoile.service
./levoile-ui &
# → l'écran fallback doit apparaître avec "sudo systemctl start levoile.service"
# 2. Démarrer le service en laissant l'UI tourner
sudo systemctl start levoile.service
# → dans les 5s, l'UI normale doit revenir sans intervention
# 3. Arrêter à nouveau le service
sudo systemctl stop levoile.service
# → après ≤ 7s (2s polling + 5s retry), l'écran fallback doit réapparaître
```

**Windows (cmd admin) :**
```cmd
sc stop levoile-service
levoile-ui.exe
REM → écran fallback avec "sc start levoile-service"
sc start levoile-service
REM → retour automatique à l'UI normale
```

### Testing standards

- Tests Go : framework standard `testing`, pas de mock framework externe (aligné sur le repo) [Source: architecture.md#Testing Standards]
- Couvrir au moins : le flag `service_reachable`, la commande OS-dépendante, la cadence 5s
- Pas de test e2e automatisé de la webview — limitation technique acceptée
- Exécuter `go test ./internal/ui/...` avant commit

### Project Structure Notes

- Alignement complet avec l'architecture 2 processus (service/UI) [Source: architecture.md#Component Architecture]
- Aucun chevauchement avec l'Epic 2 (kill switch) ni l'Epic 3 (relais) — changements isolés au binaire UI
- Pas de modification de la couche IPC (`internal/ipc/`) — respecte la règle « IPC protocol stable »

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 5.6] — BDD acceptance criteria d'origine
- [Source: _bmad-output/planning-artifacts/prd.md#FR13c] — exigence fonctionnelle
- [Source: _bmad-output/planning-artifacts/architecture.md#Architecture 2 processus] — contexte IPC
- [Source: _bmad-output/planning-artifacts/architecture.md#UI Patterns] — systray + webview + HTTP local
- [Source: internal/ui/ui.go:288-322] — reconnectIPC + handleIPCError existants
- [Source: internal/ui/httpserver.go:116-145] — handleStatus à étendre
- [Source: frontend/src/app.js:56-64] — pollStatus à enrichir
- [Source: frontend/index.html:37-57] — structure panels existante

## Dev Agent Record

### Context Reference

- Epic 5 « Interface Desktop Cross-Platform » — backlog (première story créée sous la nouvelle structure)
- Previous stories : aucune story 5-* (nouvelle structure) n'existait au moment de la création. Les fichiers `5-1-interception-*`, `5-2-relai-stun-*`, `5-3-gestion-du-fallback-turn-*` sont des vestiges de l'ancienne structure (Epic 5 STUN) et doivent être ignorés pour le contexte de cette story.
- Git context : derniers commits sur Epic 3 (relais stateless), Epic 2 (kill switch TUN/firewall). Pas de travail récent sur `internal/ui/` côté fallback. Pattern de test existant : voir `internal/ui/ui_test.go` et `internal/ui/httpserver_test.go` (table tests + IPC client mockables).

### Agent Model Used

(à renseigner par dev-story)

### Debug Log References

### Completion Notes List

- Story générée par create-story 2026-04-17 (Epic 5 passé en in-progress à cette occasion).
- Ultimate context engine : analyse complète PRD/Architecture/epics + code UI existant.

### File List

(à renseigner par dev-story)
