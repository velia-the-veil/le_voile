# Story 5.9 : Mode dégradé kill switch + indicateur visuel permanent

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a **utilisateur final en mobilité (Wi-Fi public instable)**,
I want **pouvoir désactiver temporairement le kill switch firewall pour accéder à internet en clair**,
so that **je puisse envoyer un email urgent quand le tunnel ne se rétablit pas, en assumant le risque, sans rester totalement coincée**.

Cette story implémente la sortie de secours « Camille » (User Journey #8 du PRD) : un toggle volontaire et explicite, accompagné d'un indicateur visuel permanent qui empêche l'utilisateur d'oublier qu'il n'est plus protégé, et une auto-restauration silencieuse dès que le tunnel revient.

## Acceptance Criteria

### AC1 — Activation depuis le menu tray (modale destructive)
**Given** le service tourne, le tunnel ne parvient pas à se rétablir, et le kill switch firewall bloque internet
**When** l'utilisateur fait clic droit sur l'icône tray puis sélectionne « Mode dégradé »
**Then** le serveur HTTP local de l'UI ouvre/réveille la fenêtre webview
**And** une modale bloquante s'affiche avec le titre exact « Mode dégradé »
**And** le corps contient le texte exact : « Voulez-vous désactiver la protection temporairement ? Votre trafic ne sera PAS chiffré. L'icône tray deviendra rouge jusqu'à rétablissement du tunnel. »
**And** deux boutons : `Annuler` (par défaut, focus initial) et `Continuer` (rouge `#d42b2b`, classe destructive — **pas** de pré-validation optimiste)
**And** tant que la modale n'a pas été validée, aucun appel IPC n'est émis et le firewall reste actif

### AC2 — Désactivation du kill switch après confirmation
**Given** la modale est affichée, kill switch actif (`enable_killswitch = true` effectif)
**When** l'utilisateur clique `Continuer`
**Then** l'UI envoie `POST /api/settings/killswitch` avec `{"mode": "degraded"}` au serveur HTTP local
**And** le serveur proxie via IPC `set_killswitch_mode` value=`degraded`
**And** le service appelle `firewall.Deactivate(ctx)` (idempotent — succès si déjà absent)
**And** la config TOML est persistée avec `[firewall] enable_killswitch = false`
**And** la séquence config-write puis firewall-action est atomique : si le firewall échoue, la valeur TOML est restaurée à `true` (pattern Story 2.9 — voir `handleSetAllowIPv6Leak`)
**And** la réponse IPC retourne `{status: "ok", killswitch_mode: "degraded"}`
**And** la modale se ferme uniquement après réponse `ok` (pas d'état optimiste)

### AC3 — Indicateur visuel permanent (tray rouge + bandeau webview)
**Given** le mode dégradé vient d'être activé
**When** le polling status tray (toutes les 2s, `connectAndPoll`) lit la nouvelle valeur
**Then** l'icône system tray passe à `IconDisconnected` (rouge) et **reste** rouge même si le tunnel est `Connected`, tant que `killswitch_mode = "degraded"`
**And** le tooltip tray devient « Mode dégradé — protection désactivée »
**And** la fenêtre webview affiche en haut un bandeau rouge full-width permanent : `⚠ Mode dégradé — protection désactivée`
**And** le bandeau reste visible quel que soit l'onglet (Status / Sélecteur pays / Paramètres)
**And** la classe CSS du bandeau est `.killswitch-degraded-banner` avec fond `#d42b2b`, texte blanc

### AC4 — Auto-restauration au retour du tunnel
**Given** le mode dégradé est actif, l'utilisateur ne touche à rien
**When** le tunnel transite vers l'état `Connected` (reconnexion automatique réussie via le `Reconnector`)
**Then** le service ré-active automatiquement le kill switch via `firewall.Activate(ctx, ModeFull, relayIP, tunName)`
**And** la config TOML est mise à jour avec `enable_killswitch = true`
**And** la transition est journalisée : `service: killswitch auto-restored after tunnel reconnect`
**And** au prochain poll de status, l'icône tray retrouve sa couleur correspondant à l'état tunnel (vert connecté), le tooltip redevient « Connecté — {pays} », et le bandeau rouge disparaît du webview

### AC5 — CLI `levoile-ctl killswitch off|on` avec authentification machine-local
**Given** l'utilisateur préfère la CLI (Linux ou Windows admin)
**When** il exécute `sudo levoile-ctl killswitch off` (Linux) ou `levoile-ctl.exe killswitch off` (Windows admin)
**Then** le binaire lit le token machine-local depuis `/etc/levoile/ctl.token` (Linux, perms 0600 user `levoile`) ou `%ProgramData%\LeVoile\ctl.token` (Windows, ACL LocalSystem+Administrators uniquement)
**And** se connecte au socket IPC (Unix socket `/run/levoile/ipc.sock` ou named pipe `\\.\pipe\levoile`)
**And** envoie `set_killswitch_mode` value=`degraded` avec le token dans le champ `Auth` de la `Request`
**And** le service vérifie le token via `crypto/subtle.ConstantTimeCompare` (NFR9c) avant d'exécuter — refus avec `auth_failed` si invalide
**And** affiche en sortie : `kill switch désactivé — protection désactivée jusqu'à la prochaine connexion réussie`
**And** le code retour est `0` en succès, `1` en échec (token absent / IPC refusé / firewall échoue)
**And** `levoile-ctl killswitch on` ré-active manuellement (force `Activate` avec params courants), code retour `0` si succès

### AC6 — Persistance et restauration au redémarrage du service
**Given** mode dégradé actif, l'utilisateur reboot la machine ou exécute `systemctl restart levoile.service`
**When** le service redémarre et lit `enable_killswitch = false` depuis le TOML
**Then** le firewall n'est PAS activé au boot (séquence existante `cmd/client/main.go:165` respectée)
**And** au premier poll status, l'UI affiche immédiatement le bandeau rouge + tray rouge (status retourne `killswitch_mode = "degraded"`)
**And** dès que le tunnel atteint `Connected`, l'auto-restauration AC4 se déclenche

### AC7 — Refus de désactivation en mode captive portal
**Given** le service est en mode captive portal (`captive_portal = true`, firewall en `ModeCaptive`)
**When** l'utilisateur sélectionne « Mode dégradé » dans le tray
**Then** la modale **n'est pas** affichée
**And** une notification (toast tray ou alert webview) explique : « Mode dégradé indisponible : portail captif actif. Authentifiez-vous d'abord via "Activer la protection". »
**And** l'IPC `set_killswitch_mode` retourne `{status: "error", error: "captive_portal_active"}` si forcé via CLI

### AC8 — Audit log côté service
**Given** mode dégradé activé/désactivé via UI ou CLI
**When** la transition est appliquée
**Then** le service écrit dans `serviceStderr` (qui pipe vers Event Log Windows / journald Linux) une ligne structurée :
- Activation : `service: killswitch_mode=degraded source={ui|ctl} ts={RFC3339}`
- Restauration : `service: killswitch_mode=normal source={auto-reconnect|ui|ctl} ts={RFC3339}`
**And** aucun token d'authentification ni IP utilisateur n'est journalisé (NFR1 zéro-log utilisateur)

## Tasks / Subtasks

- [ ] **Task 1 — Étendre la config et le service** (AC: #2, #4, #6)
  - [ ] Aucune nouvelle clé TOML : on réutilise `Firewall.EnableKillSwitch` existante ([config.go:35](internal/config/config.go#L35))
  - [ ] Ajouter `Program.KillSwitchMode() string` (retourne `"normal"` / `"degraded"` selon `p.config.FirewallEnabled`)
  - [ ] Ajouter `Program.SetKillSwitchMode(ctx, mode string) error` calqué sur `SetAllowIPv6Leak` ([service.go:2035](internal/service/service.go#L2035)) :
    - Si `mode == "degraded"` : `firewall.Deactivate(ctx)` puis `p.config.FirewallEnabled = false`
    - Si `mode == "normal"` : reconstruire `firewallFactory` puis `Activate(ctx, ModeFull, p.resolvedRelayIP(), p.tunDev.Name())` puis `p.config.FirewallEnabled = true`
    - Refuser si `p.captivePortal.Load() == true` → `errors.New("captive_portal_active")`
    - Logger : `service: killswitch_mode={mode} source=ipc ts={time.RFC3339}`
  - [ ] Tests unitaires `service_killswitch_test.go` (table-driven : transitions normal↔degraded, captive refuse, idempotence)
- [ ] **Task 2 — Auto-restauration au reconnect** (AC: #4, #6, #8)
  - [ ] Dans `internal/service`, repérer la transition `Disconnected/Connecting → Connected` (probablement dans le `Reconnector` ou via un channel d'état tunnel)
  - [ ] Hook : si `p.config.FirewallEnabled == false` ET le tunnel passe à `Connected` ET on n'est pas en captive, appeler `p.SetKillSwitchMode(ctx, "normal")` avec `source=auto-reconnect`
  - [ ] Persister la valeur restaurée en config TOML via `configMu` (ipchandler) — réutiliser un helper si possible
  - [ ] Tests : simulation tunnel state machine, vérifier qu'un seul restore se déclenche par transition (debounce via `atomic.Bool`)
- [ ] **Task 3 — IPC actions + payload** (AC: #2, #5, #7)
  - [ ] Ajouter constantes `ipc.ActionSetKillSwitchMode = "set_killswitch_mode"` et `ipc.ActionGetKillSwitchMode = "get_killswitch_mode"` dans [internal/ipc/messages.go](internal/ipc/messages.go)
  - [ ] Ajouter champ `KillSwitchMode string \`json:"killswitch_mode,omitempty"\`` dans `ipc.Response` (valeurs : `"normal"` / `"degraded"`)
  - [ ] Ajouter champ `Auth string \`json:"auth,omitempty"\`` dans `ipc.Request` (utilisé uniquement par `levoile-ctl`)
  - [ ] Inclure `KillSwitchMode` dans **toutes** les branches `handleGetStatus` ([handler.go:76](internal/ipchandler/handler.go#L76)) — y compris ConcurrentVPN et tunnel-not-ready, comme pour `AllowIPv6Leak`
  - [ ] Implémenter `handleSetKillSwitchMode` sur le pattern `handleSetAllowIPv6Leak` ([handler.go:632](internal/ipchandler/handler.go#L632)) :
    - Validation `req.Value ∈ {"normal", "degraded"}` sinon `invalid_value`
    - Si `req.Auth != ""` : vérifier token (cf Task 6) — sinon source = UI (autorisée sans token)
    - Atomicité : config-first sous `configMu`, firewall ensuite, rollback config si firewall échoue
  - [ ] Tests IPC : roundtrip, refus captive, refus invalid_value, rollback sur échec firewall
- [ ] **Task 4 — UI : menu tray + handler** (AC: #1, #3)
  - [ ] Dans [internal/ui/ui.go:126-128](internal/ui/ui.go#L126), ajouter `u.menuKillSwitch = u.menuAPI.AddMenuItem("Mode dégradé", "Désactiver temporairement la protection")` entre « Ouvrir la fenêtre » et le séparateur
  - [ ] Ajouter le case dans `menuHandler` : `case <-u.menuKillSwitch.ClickedCh:` → ouvrir/réveiller webview puis émettre un signal `showKillSwitchModal` (par ex via un nouvel endpoint local `POST /api/ui/show-killswitch-modal` ou un flag dans `/api/status`) — préférer le flag dans status pour simplicité (pattern `pending_modal`)
  - [ ] Adapter `connectAndPoll` : si `resp.KillSwitchMode == "degraded"`, forcer `IconDisconnected` + tooltip « Mode dégradé — protection désactivée », **avant** la logique normale tunnel-state-driven
  - [ ] Tests UI : vérifier que l'icône reste rouge même si `resp.Status == "connected"` quand `KillSwitchMode == "degraded"`
- [ ] **Task 5 — Webview : modale + bandeau permanent** (AC: #1, #3, #4)
  - [ ] [frontend/index.html](frontend/index.html) : ajouter `<div id="killswitch-banner" class="killswitch-degraded-banner hidden">⚠ Mode dégradé — protection désactivée</div>` en haut du `<body>`, avant le main panel
  - [ ] [frontend/index.html](frontend/index.html) : ajouter modale `#killswitch-modal` réutilisant le pattern de la modale IPv6 (Story 2.9) — texte exact d'AC1, bouton `Continuer` rouge avec classe `.btn-destructive`, `Annuler` focus par défaut
  - [ ] [frontend/src/style.css](frontend/src/style.css) : `.killswitch-degraded-banner { background: #d42b2b; color: #fff; padding: 8px 16px; text-align: center; font-weight: 600; position: sticky; top: 0; z-index: 100; }` + `.hidden { display: none; }`
  - [ ] [frontend/src/app.js](frontend/src/app.js) :
    - Dans le polling `/api/status` : `document.getElementById('killswitch-banner').classList.toggle('hidden', data.killswitch_mode !== 'degraded')`
    - Si la fenêtre est ouverte avec un flag `pending_killswitch_modal` (passé par exemple via query string `?modal=killswitch` quand le tray déclenche l'ouverture), afficher la modale automatiquement
    - Handler bouton `Continuer` : `fetch('/api/settings/killswitch', {method:'POST', body: JSON.stringify({mode:'degraded'})})`. Fermer la modale **uniquement** si `response.status === 'ok'`. Pas d'état optimiste.
    - Handler `Annuler` : ferme la modale, aucun appel IPC
  - [ ] Tests frontend (smoke en navigateur via le serveur HTTP local) : bandeau apparaît/disparaît selon status, modale ne se ferme pas tant que IPC n'a pas répondu
- [ ] **Task 6 — UI HTTP server : nouvel endpoint** (AC: #2, #7)
  - [ ] [internal/ui/httpserver.go](internal/ui/httpserver.go) : ajouter `s.mux.HandleFunc("/api/settings/killswitch", s.handleSetKillSwitch)` après la ligne 64
  - [ ] `handleSetKillSwitch` : décoder `{mode: "degraded"|"normal"}`, proxier vers IPC `set_killswitch_mode` value=mode (sans `Auth` → source UI), retourner JSON `{status, killswitch_mode, error}`
  - [ ] Inclure `killswitch_mode` dans la réponse `/api/status` (ligne 228) et `/api/settings`
- [ ] **Task 7 — Binaire `cmd/ctl/main.go` (CLI)** (AC: #5)
  - [ ] Créer `cmd/ctl/main.go` avec parsing flag minimaliste (pas de Cobra) : sous-commandes `killswitch off`, `killswitch on`, `status`, `--help`
  - [ ] Lire le token depuis `/etc/levoile/ctl.token` (Linux) ou `%ProgramData%\LeVoile\ctl.token` (Windows) — fail-fast avec message clair si absent ou perms incorrectes
  - [ ] Connexion IPC via le client existant `internal/ipc.NewClient()` — ajouter le token dans `Request.Auth`
  - [ ] Mapping commandes → IPC : `killswitch off` → `set_killswitch_mode` value=`degraded` ; `killswitch on` → `set_killswitch_mode` value=`normal` ; `status` → `get_status`
  - [ ] Code retour : `0` succès, `1` erreur ; messages stderr en français ; messages stdout courts
  - [ ] Tests : `cmd/ctl/main_test.go` (parsing flags, code retour, formatage messages — IPC mockable)
- [ ] **Task 8 — Génération + persistance du token machine-local** (AC: #5)
  - [ ] À la première initialisation du service (post-install ou premier `Start()`), si `ctl.token` n'existe pas, générer 32 octets aléatoires via `crypto/rand`, encoder en hex, écrire dans le fichier avec perms `0600` (Linux) / ACL `LocalSystem+Administrators` (Windows)
  - [ ] Charger le token en mémoire au démarrage (`Program.ctlToken []byte`)
  - [ ] Helper `Program.VerifyCtlToken(provided string) bool` utilisant `subtle.ConstantTimeCompare` (NFR9c)
  - [ ] Tests : génération idempotente, perms vérifiées (Linux uniquement via `os.Stat`), comparaison constant-time
- [ ] **Task 9 — Packaging Linux + Windows installer** (AC: #5)
  - [ ] [packaging/postinstall.sh](packaging/postinstall.sh) : créer `/etc/levoile/` avec perms `0750`, déclencher la génération du token au premier `systemctl start`
  - [ ] [packaging/nfpm-deb.yaml](packaging/nfpm-deb.yaml) / `nfpm-rpm.yaml` / `nfpm-apk.yaml` : déclarer le binaire `/usr/bin/levoile-ctl` (cible build GoReleaser)
  - [ ] [.goreleaser.yaml](.goreleaser.yaml) : ajouter une cible build `cmd/ctl` produisant `levoile-ctl` (Linux + Windows)
  - [ ] [installer/levoile.nsi](installer/levoile.nsi) : copier `levoile-ctl.exe` dans `Program Files/LeVoile/`, ajouter au PATH système ou créer un raccourci CMD admin
- [ ] **Task 10 — Documentation utilisateur** (AC: tous)
  - [ ] [README.md](README.md) section « Mode dégradé » : expliquer le scenario Camille (Wi-Fi public instable), les commandes `levoile-ctl killswitch off|on`, l'auto-restauration, et l'avertissement de sécurité
  - [ ] Ajouter une note dans [config.example.toml](config.example.toml) sous `[firewall] enable_killswitch` : « Désactivable temporairement via l'UI (« Mode dégradé ») ou `levoile-ctl killswitch off`. Auto-restauré à la prochaine connexion tunnel réussie. »
- [ ] **Task 11 — Tests e2e** (AC: #1, #4, #5, #7)
  - [ ] Linux (Ubuntu 24.04) : démarrer service avec firewall actif, désactiver via CLI, vérifier `nft list ruleset` vide, vérifier `ping 1.1.1.1` passe ; rétablir la connexion tunnel manuellement (ou simulée), vérifier que `nft list ruleset` revient
  - [ ] Windows 11 : équivalent avec `netsh wfp show filters` + `Test-NetConnection`
  - [ ] Test refus captive : forcer `captivePortal=true`, appeler IPC `set_killswitch_mode` value=`degraded`, vérifier réponse `error: captive_portal_active`
  - [ ] Test atomicité : injecter une erreur dans `firewall.Deactivate`, vérifier que le TOML reste à `enable_killswitch=true` (rollback)
  - [ ] Test indicateur visuel : screenshot bandeau rouge + tray rouge en mode dégradé (matrice NFR22)

## Dev Notes

### Relevant architecture patterns and constraints

- **Pattern atomicité config + firewall** ([handler.go:632](internal/ipchandler/handler.go#L632) — `handleSetAllowIPv6Leak`) : config-write **dans** `configMu`, puis firewall-action, rollback config en cas d'échec firewall, le tout sous le même verrou. À reproduire à l'identique pour `handleSetKillSwitchMode` — ne PAS inverser l'ordre (config doit être source de vérité avant tout effet de bord OS).
- **Réutilisation de `Firewall.EnableKillSwitch`** ([config.go:35](internal/config/config.go#L35)) : la config existe déjà avec un commentaire explicite « When false, Activate() is a no-op (degraded mode, see Story 5.9) ». Pas de nouvelle clé TOML — on bascule cette valeur. Le service consomme déjà `p.config.FirewallEnabled` ([service.go:929](internal/service/service.go#L929)) : si `false` au boot, le firewall n'est pas activé. Cette story rend cette valeur **runtime-mutable**.
- **`firewall.Activate` est idempotent** : il flushe et remplace atomiquement (cf [firewall_linux.go:42](internal/firewall/firewall_linux.go#L42)). On peut le rappeler sans `Deactivate` préalable. `Deactivate` est aussi idempotent (gère « table absente » en succès, [firewall_linux.go:96](internal/firewall/firewall_linux.go#L96)).
- **Mode captive prioritaire sur mode dégradé** : `ModeCaptive` est un état contraint (firewall lockdown relaxé pour authentifier le portail). Permettre la désactivation totale par-dessus créerait un état incohérent. AC7 le refuse explicitement. Le check se fait via `p.captivePortal.Load()` ([service.go:2053](internal/service/service.go#L2053)).
- **Auto-restauration AC4 = comportement « safe-by-default »** : l'utilisateur active explicitement le mode dégradé (modale destructive), mais la sortie est automatique. C'est cohérent avec l'esprit du PRD (User Journey #8 « transitoire, automatiquement réversible »). Ne pas exiger de re-confirmation au retour — l'utilisateur est *implicitement* d'accord avec « repasser en mode protégé dès que possible ».
- **Indicateur visuel inviolable** : tray rouge + bandeau webview ne sont **pas** dismissable par l'utilisateur. Pas de croix « fermer le bandeau ». L'objectif (PRD ligne 197) est précisément d'empêcher l'oubli. Tester explicitement dans Task 5 que le bandeau persiste à travers les changements d'onglet/sidebar.
- **CLI `levoile-ctl` = nouveau binaire mais infra IPC existante** : ne pas créer un nouveau transport. Réutiliser `internal/ipc.NewClient()` qui ouvre déjà named pipe (Windows) ou Unix socket (Linux). Le seul ajout est le champ `Auth` dans la `Request` JSON.
- **Token machine-local ≠ password utilisateur** : c'est une valeur dérivée d'`os.Random`, persistée à perms restrictives, comparée constant-time. Elle protège contre l'usage **non-privilégié** de `levoile-ctl` (malware user-space sans root). Avec root/admin, le token est lisible — c'est acceptable car l'attaquant root contrôle déjà le firewall directement.
- **Build tags pour génération token** : la création du fichier avec ACL Windows nécessite `golang.org/x/sys/windows`. Linux utilise `os.Chmod(0600)` + `os.Chown` au user `levoile`. Séparer dans `cmd/ctl/token_linux.go` / `token_windows.go`.
- **Pas de modale optimiste** (anti-régression Story 2.9 H5) : le state UI **suit** le résultat IPC, pas l'inverse. Si l'IPC échoue, la modale doit afficher l'erreur et **rester ouverte**.

### Source tree components to touch

**Packages existants à modifier :**
- [internal/config/config.go](internal/config/config.go) — clé existante, juste vérifier qu'aucune validation n'empêche `EnableKillSwitch=false` à runtime (le commentaire suggère que c'est déjà supporté)
- [internal/firewall/firewall.go](internal/firewall/firewall.go) — pas de changement d'interface ; `Activate`/`Deactivate` existants suffisent
- [internal/ipc/messages.go](internal/ipc/messages.go) — ajout 2 actions, 1 champ Response (`KillSwitchMode`), 1 champ Request (`Auth`)
- [internal/ipchandler/handler.go](internal/ipchandler/handler.go) — ajout `handleSetKillSwitchMode`, `handleGetKillSwitchMode`, inclure `KillSwitchMode` dans tous les status retour
- [internal/service/service.go](internal/service/service.go) — méthodes `KillSwitchMode()` / `SetKillSwitchMode()` / `VerifyCtlToken()`, hook auto-restore dans la transition tunnel
- [internal/ui/ui.go](internal/ui/ui.go) — entrée menu tray « Mode dégradé », logique tray rouge si `KillSwitchMode == "degraded"`
- [internal/ui/httpserver.go](internal/ui/httpserver.go) — endpoint `/api/settings/killswitch`, exposition `killswitch_mode`
- [frontend/index.html](frontend/index.html) — bandeau permanent + modale destructive
- [frontend/src/app.js](frontend/src/app.js) — toggle bandeau, handler modale, polling
- [frontend/src/style.css](frontend/src/style.css) — `.killswitch-degraded-banner`, `.btn-destructive` (probablement déjà défini par 2.9)
- [cmd/client/main.go](cmd/client/main.go) — initialisation token au boot (Task 8)

**Nouveaux fichiers :**
- [cmd/ctl/main.go](cmd/ctl/main.go) + `cmd/ctl/token_linux.go` + `cmd/ctl/token_windows.go` + `cmd/ctl/main_test.go`
- [internal/service/killswitch.go](internal/service/killswitch.go) (méthodes `KillSwitchMode/SetKillSwitchMode/auto-restore hook`)
- [internal/service/killswitch_test.go](internal/service/killswitch_test.go)
- [packaging/](packaging/) ajustements packaging Linux (postinstall)

### Testing standards summary

- **Unit tests Go** co-localisés (Story `_test.go` à côté de chaque fichier touché). Coverage cible : transitions état, refus captive, atomicité config/firewall, génération token, parsing CLI.
- **Integration tests** sous build tags `linux` / `windows` pour vérifier l'effet réel `nft`/`WFP` après IPC. Utiliser le pattern existant `firewall_integration_test.go`.
- **E2E** : scripts `scripts/e2e/killswitch-degraded.{sh,ps1}` exerçant cycle complet (UI activation → ping clair → reconnexion auto → ping via tunnel). Validation finale matrice NFR22 (Ubuntu 24.04 + Windows 11 obligatoires).
- **Race detector** obligatoire (`go test -race`) sur `internal/service` et `internal/ipchandler` — la mutation runtime de `p.config.FirewallEnabled` croise plusieurs verrous (`p.mu`, `configMu`, `f.mu`).
- **Pas de mocks de DB** (irrelevant ici) ; mocker le firewall via une interface `firewallFactory` déjà en place dans `service_tun_recovery_test.go:41` (réutiliser `stubFirewall`).

### Project Structure Notes

- **Aucun nouveau package** côté `internal/` — toutes les modifications sont dans des packages existants (config, firewall, ipc, ipchandler, service, ui). Cohérent avec architecture.md — pas de création de package non listé.
- **Nouveau binaire `cmd/ctl/`** — déjà prévu dans architecture ([architecture.md:728](_bmad-output/planning-artifacts/architecture.md#L728)) : « cmd/ctl/ NOUVEAU — CLI opérationnel (levoile-ctl) ». Cette story matérialise cette ligne.
- **Aucun conflit avec structure existante** : le menu tray actuel a juste « Ouvrir » et « Quitter » ; insérer « Mode dégradé » entre les deux respecte la convention « actions principales avant séparateur, Quitter après ». À valider visuellement dans la matrice NFR22.
- **Frontend** : pattern modale destructive déjà introduit par Story 2.9 (cf [frontend/src/style.css](frontend/src/style.css) à inspecter — la classe `.btn-destructive` doit déjà exister avec le rouge `#d42b2b`). Réutiliser pour cohérence visuelle.
- **`Reconnector` hook** : la story 1.2 (« reconnexion automatique avec backoff exponentiel et circuit breaker ») a déjà mis en place le mécanisme. Identifier le point exact de la transition `Connecting → Connected` (probablement dans `tunnel/reconnect.go` ou via channel `state.go`). À explorer en début de Task 2 pour choisir le bon hook (callback ou channel observer).

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 5.9 (lignes 1045-1069)](_bmad-output/planning-artifacts/epics.md#L1045) — User story + AC originaux
- [Source: _bmad-output/planning-artifacts/prd.md#FR16b (ligne 448)](_bmad-output/planning-artifacts/prd.md#L448) — Requirement fonctionnel mode dégradé
- [Source: _bmad-output/planning-artifacts/prd.md#User Journey 8 Camille (lignes 188-220)](_bmad-output/planning-artifacts/prd.md#L188) — Scénario utilisateur mobile/Wi-Fi public
- [Source: _bmad-output/planning-artifacts/architecture.md#IPC Actions (ligne 294)](_bmad-output/planning-artifacts/architecture.md#L294) — `GetKillSwitchMode, SetKillSwitchMode, ForceReconnect` listés
- [Source: _bmad-output/planning-artifacts/architecture.md#CLI Control Tool (ligne 346)](_bmad-output/planning-artifacts/architecture.md#L346) — Spec `levoile-ctl killswitch off/on`, token machine-local
- [Source: _bmad-output/planning-artifacts/architecture.md#Firewall (lignes 326-328)](_bmad-output/planning-artifacts/architecture.md#L326) — Interface `firewall.Firewall`, build tags
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure (ligne 728)](_bmad-output/planning-artifacts/architecture.md#L728) — `cmd/ctl/` planifié
- [Source: _bmad-output/implementation-artifacts/2-9-option-ipv6-hors-tunnel-avec-avertissement-explicite.md](_bmad-output/implementation-artifacts/2-9-option-ipv6-hors-tunnel-avec-avertissement-explicite.md) — **Pattern de référence** : atomicité config+firewall, modale destructive, indicateur visuel permanent, rollback en cas d'échec
- [Source: internal/ipchandler/handler.go#L632 — handleSetAllowIPv6Leak](internal/ipchandler/handler.go#L632) — Code pattern atomicité à dupliquer
- [Source: internal/service/service.go#L2035 — SetAllowIPv6Leak](internal/service/service.go#L2035) — Code pattern méthode runtime à dupliquer
- [Source: internal/firewall/firewall.go#L70-103 — Firewall interface](internal/firewall/firewall.go#L70) — API existante `Activate`/`Deactivate`/`IsActive`
- [Source: internal/ui/ui.go#L120-167 — onReady + menuHandler](internal/ui/ui.go#L120) — Point d'insertion menu tray

### Previous Story Intelligence (Story 2.9 — IPv6 leak opt-out)

Story 2.9 est le précédent direct touchant le même périmètre (firewall + UI + config + IPC). Apprentissages à reproduire :

- **Pattern d'atomicité éprouvé** ([handler.go:632](internal/ipchandler/handler.go#L632)) : config-first sous `configMu`, firewall ensuite, rollback config si firewall échoue. Le review adversarial a remonté un MED M1 quand le rollback était hors lock — corrigé. À NE PAS réintroduire ici.
- **Race conditions sur les getters** : H3 du review 2.9 a montré que `AllowIPv6Leak()` lisait sans verrou. Pour `KillSwitchMode()`, prendre `p.mu.Lock()` ; pour `SetKillSwitchMode()`, `defer p.mu.Unlock()` autour de toute la séquence (cf [service.go:2036](internal/service/service.go#L2036)).
- **Modale destructive — anti-pattern optimiste** : H5 du review 2.9 → checkbox basculait avant réponse IPC. Pour la modale 5.9, la fermeture **ne doit dépendre que** de `response.status === 'ok'` — pas d'optimisme.
- **XSS** : M2 du review 2.9 → utilisation de `innerHTML` pour des IPs. Pour le bandeau « Mode dégradé », le texte est statique : utiliser `textContent` malgré tout par cohérence/discipline.
- **Texte exact des modales** : H4 du review 2.9 → accents oubliés (« protege » au lieu de « protégé »). AC1 spécifie le texte mot-à-mot — copier-coller depuis ce fichier sans retaper.
- **Indicateur visuel permanent** : Story 2.9 a mis en place le pattern « badge orange titlebar + ligne d'état panel ». Pour 5.9, intensifier : bandeau full-width rouge (criticité supérieure — la protection est totalement coupée, pas juste IPv6).
- **Couverture tests modifiés sans régression** : la note de complétion 2.9 mentionne « pre-existing failures in ui/desktop/tray/ipchandler unrelated ». Vérifier en début de Task 4-5 que ces tests existants passent toujours après nos modifs UI.

### Git Intelligence Summary

5 derniers commits pertinents :

1. `bd11612 feat: IPv6 leak opt-out toggle + relay systemd CAP_NET_ADMIN (Stories 2.9 + 3.1)` — référence directe pour le pattern firewall + UI + IPC. Inspecter le diff pour copier la structure des handlers et le style des tests.
2. `ece3270 feat: implement Sprint 2 — watchdog, routing, firewall, DNS flush, captive portal` — fondation du firewall. La logique `ModeCaptive` introduite ici justifie AC7 (refus mode dégradé en captive).
3. `c2e1c0e feat: complete Epic 3 — relay stateless multi-VPS with tunnel IP & NAT` — relais finalisé. Pas d'impact direct sur 5.9 mais signifie que le tunnel IP est end-to-end fonctionnel pour les tests e2e.
4. `7bf2e59 chore: remove .claude/ from history, harden deploy scripts` — non pertinent.
5. `16a275a fix(deploy): align install.sh/README/service with prod (signing.key + registry)` — déploiement relais. Note pour Task 9 : aligner les scripts post-install client/Linux avec le même style.

### Latest Tech Information

- **`fyne.io/systray` v1.12.0** — `AddMenuItem` et `*MenuItem.ClickedCh` sont stables ; aucun changement breaking récent. Pattern d'extension du menu = ajout en place dans `onReady` (pas de menu dynamique runtime nécessaire ici).
- **`crypto/subtle.ConstantTimeCompare`** (Go 1.26) — toujours la primitive recommandée pour comparer des secrets de longueur arbitraire en temps constant. Préférer aux `bytes.Equal` ou `==`.
- **`crypto/rand.Read`** — `crypto/rand.Read(buf)` (32 octets pour le token machine-local) ne peut échouer en pratique sur Linux/Windows ; gérer l'erreur quand même (logger + halt boot).
- **`encoding/hex.EncodeToString`** vs base64 — préférer hex pour la persistance disque du token : 64 caractères ASCII, lisible à l'œil pour debug, pas de caractères spéciaux dans les chemins/argv.
- **`golang.org/x/sys/windows` ACL** — pour Task 8 Windows : `windows.SetNamedSecurityInfo` avec un DACL granté à `S-1-5-18` (LocalSystem) et `S-1-5-32-544` (Administrators). Pas de groupe « Users ». Référencer la doc MSDN pour les SID.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m]

### Debug Log References

### Completion Notes List

### File List

## Open Questions for Dev

1. **Hook auto-restore (Task 2)** : faut-il observer le state tunnel via channel (`tunnel.State()` expose-t-il un channel ?) ou via callback enregistré sur le `Reconnector` ? À explorer dans le code [internal/tunnel/state.go](internal/tunnel/state.go) en début de tâche. Si aucun channel/callback n'existe, ajouter un mécanisme léger (canal buffered consommé par une goroutine du service) plutôt qu'un polling.
2. **Visibilité du bandeau dans le serveur HTTP local** : si le webview est fermé (tray seul) puis ré-ouvert, le frontend repolll `/api/status` immédiatement et affiche le bandeau. OK. Mais que faire si l'UI crash et redémarre via supervision (Story 5.7) ? L'état est persisté en TOML, le service le retourne au prochain status — auto-récupère. À documenter explicitement dans le README.
3. **`levoile-ctl killswitch on` quand le tunnel n'est pas connecté** : refuser ou exécuter quand même `firewall.Activate` ? Recommandation : refuser avec message « tunnel non connecté — utilisez `levoile-ctl reconnect` d'abord » pour éviter de bloquer internet sans tunnel utilisable. À valider lors du dev.
4. **Ordre tray menu** : la spec dit « Ouvrir / [Mode dégradé] / Quitter ». Faut-il ajouter un séparateur entre « Ouvrir » et « Mode dégradé » pour bien isoler visuellement la commande critique ? Pattern macOS : oui ; pattern Windows : moins courant. Décider avec un screenshot pendant Task 4.
5. **Localisation du token dans CI** : les e2e tests (Task 11) doivent générer un token et l'utiliser pour piloter via `levoile-ctl`. Prévoir un helper de test `testutil.WriteCtlToken(t, dir)` qui crée le token + retourne sa valeur.
