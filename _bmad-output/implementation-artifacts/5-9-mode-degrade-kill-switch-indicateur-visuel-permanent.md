# Story 5.9 : Mode dégradé kill switch + indicateur visuel permanent

Status: done

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

- [x] **Task 1 — Étendre la config et le service** (AC: #2, #4, #6)
  - [x] Aucune nouvelle clé TOML : on réutilise `Firewall.EnableKillSwitch` existante
  - [x] Ajouter `Program.KillSwitchMode() string` (lecture verrouillée de `p.config.FirewallEnabled`)
  - [x] Ajouter `Program.SetKillSwitchMode(ctx, mode, source)` (refus captive, atomicité firewall→config→persist→rollback, audit log RFC3339)
  - [x] Tests unitaires `internal/service/killswitch_test.go` (8 cas : normal↔degraded, refus captive, invalid mode, no-op same-mode, rollback persist, restore-needs-tunnel, MaybeRestore variants, VerifyCtlToken constant-time)
- [x] **Task 2 — Auto-restauration au reconnect** (AC: #4, #6, #8)
  - [x] Nouvelle option `tunnel.WithReconnectSuccessHook(fn)` ajoutée à `internal/tunnel/reconnect.go`, fire après `deactivateKillSwitch` succès, recover() guard contre panics
  - [x] Hook enregistré dans `service.go` Reconnector setup → appelle `Program.MaybeRestoreKillSwitch(ctx, "auto-reconnect")`
  - [x] `MaybeRestoreKillSwitch` no-op si déjà normal / captive — sinon transition gérée par `SetKillSwitchMode` (atomicité + persist)
  - [x] `handleConnect` et `handleSelectCountry` IPC déclenchent aussi `MaybeRestoreKillSwitch` (sources `ipc-connect` / `ipc-select-country`)
  - [x] Tests `internal/tunnel/reconnect_test.go` : 3 cas (fires-on-success, not-on-circuit-breaker, panic-recovered)
- [x] **Task 3 — IPC actions + payload** (AC: #2, #5, #7)
  - [x] Constantes `ipc.ActionSetKillSwitchMode` / `ActionGetKillSwitchMode` + valeurs `KillSwitchModeNormal/Degraded` dans [internal/ipc/messages.go](internal/ipc/messages.go)
  - [x] Champ `Response.KillSwitchMode` ajouté + `Request.Auth` (token ctl)
  - [x] `KillSwitchMode` inclus dans **toutes** les branches `handleGetStatus` (ConcurrentVPN, tunnel-not-ready, connected)
  - [x] `handleSetKillSwitchMode` : auth conditionnelle (UI sans token / ctl avec token verifié constant-time), source attribuée, atomicité déléguée au service
  - [x] `handleGetKillSwitchMode` (lecture seule)
  - [x] Tests `internal/ipchandler/handler_test.go` : 6 cas (Get default, Set invalid, Set UI degraded, Set ctl valid token, Set ctl bad token, Set ctl empty configured, GetStatus includes mode)
- [x] **Task 4 — UI : menu tray + handler** (AC: #1, #3)
  - [x] Entrée `Mode dégradé` ajoutée entre `Ouvrir la fenêtre` et le séparateur dans [internal/ui/ui.go](internal/ui/ui.go) `onReady`
  - [x] `menuHandler` : nouveau case → `handleKillSwitchMenu` (queue event modal + ouvre webview)
  - [x] `updateTrayState` : override degraded — icône `IconDisconnected` + tooltip « Mode dégradé — protection désactivée », `connected=false`, **avant** la logique tunnel-state
  - [x] `stateKey` du debounce inclut `KillSwitchMode` (sinon transitions degraded↔normal seraient avalées)
  - [x] Tests `internal/ui/ui_test.go` : 3 cas (override-while-connected, leave-degraded-restores, TriggerUIEvent read-and-clear)
- [x] **Task 5 — Webview : modale + bandeau permanent** (AC: #1, #3, #4)
  - [x] [frontend/index.html](frontend/index.html) : bandeau sticky-top `#killswitch-banner` (avant titlebar) + modale `#modal-killswitch` avec texte AC1 exact
  - [x] [frontend/src/style.css](frontend/src/style.css) : `.killswitch-degraded-banner` (rouge full-width sticky z-index 200) + `.modal-error` pour affichage erreur inline
  - [x] [frontend/src/app.js](frontend/src/app.js) : `updateUI` toggle bandeau ; `openKillSwitchModal/cancelKillSwitchModal/confirmKillSwitchDegraded` (no-optimistic, garde modale ouverte sur erreur, humanizeKillSwitchError pour captive/auth/tunnel)
  - [x] `startUIEventsPolling` (1 s cadence) consomme `/api/ui-event` et déclenche modale sur `killswitch_modal`
- [x] **Task 6 — UI HTTP server : nouvel endpoint** (AC: #2, #7)
  - [x] [internal/ui/httpserver.go](internal/ui/httpserver.go) : route `/api/settings/killswitch` ajoutée + `handleSetKillSwitch` (decode JSON, validation, proxy IPC sans Auth)
  - [x] `APIStatusResponse.KillSwitchMode` champ ajouté + normalisation `"" → "normal"` (safe rendering quand service unreachable)
  - [x] `/api/settings` inclut `killswitch_mode` pour le panel Paramètres
  - [x] Endpoint `/api/ui-event` (GET) avec `eventSlot` thread-safe pour les one-shot events tray→webview
  - [x] Tests `internal/ui/httpserver_test.go` : 5 cas (status default normal, status surfaces degraded, post degraded, bad mode 400, settings includes mode)
- [x] **Task 7 — Binaire `cmd/ctl/main.go` (CLI)** (AC: #5)
  - [x] [cmd/ctl/main.go](cmd/ctl/main.go) : parser minimaliste, sous-commandes `killswitch off|on`, `status`, `help`
  - [x] Lecture token via `internal/ctlauth.Load(DefaultPath())` — fail-fast français si absent
  - [x] Connexion IPC via `ipc.NewClient` — token transmis dans `Request.Auth`
  - [x] Mapping `killswitch off` → IPC mode degraded, `on` → mode normal, exit codes stables (0/1/2/3)
  - [x] Tests `cmd/ctl/main_test.go` : 13 cas (no args, help variants, unknown, missing verb, invalid verb, off happy, on happy, token missing, auth_failed, captive refusal, dial failure, status happy)
- [x] **Task 8 — Génération + persistance du token machine-local** (AC: #5)
  - [x] Nouveau package `internal/ctlauth` : `Token` API + `LoadOrCreate(path)` + `Load(path)` + `DefaultPath()` + `Hex(raw)` + sentinels `ErrTokenAbsent` / `ErrTokenMalformed`
  - [x] Build tags : `perms_unix.go` (chmod 0600 + atomic write), `perms_windows.go` (parent ACL inheritance documented)
  - [x] `Program.SetCtlToken/VerifyCtlToken/HasCtlToken` (constant-time compare via `crypto/subtle.ConstantTimeCompare`)
  - [x] [cmd/client/main.go](cmd/client/main.go) wire : `LoadOrCreate(DefaultPath())` au boot, injecte sur Program ; persister `firewall.enable_killswitch` via `persistFirewallEnabled` helper
  - [x] Tests `internal/ctlauth/token_test.go` : 7 cas (idempotent, hex persisted, perms 0600 Linux, ErrTokenAbsent, trim newline, malformed, DefaultPath non-empty)
- [x] **Task 9 — Packaging Linux + Windows installer** (AC: #5)
  - [x] [.goreleaser.yaml](.goreleaser.yaml) : 2 nouvelles cibles `ctl-windows` / `ctl-linux` (amd64+arm64), inclues dans archives `windows` et `ui-linux`
  - [x] [installer/levoile.nsi](installer/levoile.nsi) : `CTL_EXE` define + `File /nonfatal "build\${CTL_EXE}"` (copié dans Program Files/LeVoile, parent ACL hérite des perms restrictives pour `%ProgramData%\LeVoile\ctl.token`)
  - **Note** : packaging Linux nfpm/.deb/.rpm/.apk + postinstall systemd reste à compléter dans Epic 7 (Story 7.2/7.3) — la story 5.9 fournit les binaires GoReleaser nécessaires
- [x] **Task 10 — Documentation utilisateur** (AC: tous)
  - [x] [README.md](README.md) : section « Mode dégradé du kill switch » avec procédures UI + CLI, auto-restauration, refus captive
  - [x] [config.example.toml](config.example.toml) : commentaire enrichi sous `[firewall] enable_killswitch` documentant les voies de désactivation runtime + auto-restauration
- [x] **Task 11 — Tests e2e** (AC: #1, #4, #5, #7)
  - [x] Test e2e cross-package : [internal/ipchandler/killswitch_e2e_test.go](internal/ipchandler/killswitch_e2e_test.go) — `TestE2E_KillSwitch_NormalToDegraded_PersistsAndDeactivates` exerce la stack complète (IPC handler → service.SetKillSwitchMode → stub firewall → persister callback → on-disk TOML round-trip)
  - [x] Test refus captive : couvert par `TestSetKillSwitchMode_RefusedDuringCaptive` (service) + handlers IPC
  - [x] Test atomicité rollback : `TestSetKillSwitchMode_PersistFailureRollsBack` (service)
  - **Note** : validation OS-level (`nft list ruleset` Linux + `netsh wfp show filters` Windows + screenshots NFR22) requiert un runner CI privilégié — script de validation manuel à ajouter dans Epic 7 packaging

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

- `go test ./... -timeout 300s` → PASS sur **toute** la suite (35 packages, 0 régression)
- `go test -race ./internal/service/ ./internal/ipchandler/ ./internal/ui/ ./internal/tunnel/ ./internal/ctlauth/ ./cmd/ctl/` → PASS sur tous les packages touchés (race pré-existante dans `TestSTUNActive_AfterStop` due à port 3478 occupé — sans rapport avec la story 5.9)
- `go vet ./...` → propre

### Completion Notes List

- **Architecture clé** : `Firewall.EnableKillSwitch` réutilisée comme source de vérité — déjà annotée « degraded mode, see Story 5.9 » dans le code Sprint 2. Pas de nouvelle clé TOML.
- **Pattern atomicité** repris de Story 2.9 (`handleSetAllowIPv6Leak`) mais **inversé** : le service possède la transition firewall + flag in-memory, et **délègue** la persistence TOML à un callback `SetKillSwitchPersister` injecté par `cmd/client/main.go`. Rollback couvre les deux états (firewall *et* config) si persist échoue.
- **`tunnel.WithReconnectSuccessHook`** : nouvelle option d'extension pure (zéro impact sur les chemins existants), couverte par 3 tests dont un panic-recovery. Hooks fired sur 3 sites : Reconnector success, `handleConnect` post-success, `handleSelectCountry` post-success.
- **CLI `levoile-ctl`** : pure Go, aucun nouveau dépendance externe, exit codes stables (0/1/2/3) pour scripting opérateur.
- **Token machine-local** : nouveau package `internal/ctlauth` partagé service+CLI, build tags pour perms 0600 (Unix) vs ACL héritée (Windows). Comparaison constant-time `crypto/subtle.ConstantTimeCompare` (NFR9c).
- **Indicateur visuel inviolable** : bandeau sticky rouge full-width (z-index 200) sans bouton de fermeture, override tray icon prend la précédence absolue sur la logique tunnel-state-driven.
- **Pas d'optimistic state** : la modale ne se ferme **que** sur réponse IPC `status === 'ok'` (anti-régression Story 2.9 H5). Erreurs surfacées inline avec messages français (captive_portal_active, auth_failed, tunnel_not_connected, generic).
- **Auto-restauration** : couverture des 3 chemins de retour-au-tunnel — Reconnector automatique (success hook), Connect IPC manuel (handler), SelectCountry switch (handler).
- **Couverture tests** : 8 tests service + 3 tests tunnel + 6 tests IPC handler + 1 test e2e cross-package + 5 tests HTTP + 3 tests UI tray + 13 tests CLI + 7 tests ctlauth = **46 nouveaux tests**, tous verts.
- **Open questions résolues** :
  - Q1 (hook auto-restore via channel ou callback) : tranchée pour `WithReconnectSuccessHook` option (callback registered in service.run, fires after kill switch deactivation).
  - Q3 (`killswitch on` sans tunnel) : refusé via `ErrKillSwitchNotConnected` (sentinel) — le user voit `tunnel_not_connected` côté UI/CLI et doit reconnecter.
  - Q4 (séparateur tray) : pas de séparateur additionnel — l'entrée « Mode dégradé » s'insère naturellement avant le séparateur existant + Quitter.
  - Q5 (helper test pour token CI) : `internal/ctlauth.LoadOrCreate(t.TempDir())` est self-contained, utilisable directement par les e2e.

### File List

**Nouveaux fichiers**

- `internal/service/killswitch.go` — méthodes KillSwitchMode/SetKillSwitchMode/MaybeRestoreKillSwitch/Set+Verify+HasCtlToken/InjectFirewallForTest
- `internal/service/killswitch_test.go` — 8 tests
- `internal/ctlauth/token.go` — API publique du package
- `internal/ctlauth/perms_unix.go` — chmod 0600 atomic write (build tag !windows)
- `internal/ctlauth/perms_windows.go` — write avec ACL parent-héritée (build tag windows)
- `internal/ctlauth/token_test.go` — 7 tests
- `cmd/ctl/main.go` — binaire CLI levoile-ctl
- `cmd/ctl/main_test.go` — 13 tests
- `internal/ipchandler/killswitch_e2e_test.go` — test e2e cross-package

**Fichiers modifiés**

- `internal/tunnel/reconnect.go` — `WithReconnectSuccessHook` option + invocation post-success
- `internal/tunnel/reconnect_test.go` — 3 tests pour le hook
- `internal/service/service.go` — champs `killSwitchPersist`/`ctlToken` + wire du `WithReconnectSuccessHook` dans `run()` Reconnector setup
- `internal/ipc/messages.go` — actions `Get/SetKillSwitchMode`, valeurs `KillSwitchMode{Normal,Degraded}`, champs `Response.KillSwitchMode` + `Request.Auth`
- `internal/ipchandler/handler.go` — dispatcher cases + `handleGetKillSwitchMode/handleSetKillSwitchMode` + `MaybeRestoreKillSwitch` calls dans handleConnect/handleSelectCountry + `KillSwitchMode` ajouté dans toutes les branches `handleGetStatus`
- `internal/ipchandler/handler_test.go` — 6 tests killswitch
- `internal/ui/httpserver.go` — endpoint `/api/settings/killswitch` + endpoint `/api/ui-event` + `eventSlot`/`TriggerUIEvent` + `KillSwitchMode` dans `APIStatusResponse` et `/api/settings`
- `internal/ui/httpserver_test.go` — 5 tests
- `internal/ui/ui.go` — champ `menuKillSwitch` + entrée menu + `handleKillSwitchMenu` + override degraded dans `updateTrayState` + `stateKey` enrichi
- `internal/ui/ui_test.go` — 3 tests
- `frontend/index.html` — bandeau permanent + modale destructive
- `frontend/src/style.css` — `.killswitch-degraded-banner` + `.modal-error`
- `frontend/src/app.js` — toggle bandeau, handlers modale, `humanizeKillSwitchError`, `startUIEventsPolling`/`pollUIEvents`
- `cmd/client/main.go` — import `ctlauth`, `SetKillSwitchPersister` wire, token init + helper `persistFirewallEnabled`
- `.goreleaser.yaml` — cibles `ctl-windows`/`ctl-linux` + inclusion archives
- `installer/levoile.nsi` — `CTL_EXE` define + `File` directive
- `config.example.toml` — commentaire enrichi `[firewall] enable_killswitch`
- `README.md` — section « Mode dégradé du kill switch »

**Fichiers modifiés post-review (fixes 8 findings)**

- `internal/config/config.go` — expose `config.Mu sync.Mutex` partagé (H2)
- `internal/ipchandler/handler.go` — `configMu` réécrit comme alias `&config.Mu`, retire import `sync` redondant (H2)
- `cmd/client/main.go` — `persistFirewallEnabled` prend `config.Mu` (H2)
- `internal/ctlauth/perms_windows.go` — applique DACL explicite via `windows.SetNamedSecurityInfo` + `PROTECTED_DACL_SECURITY_INFORMATION` (H1)
- `installer/levoile.nsi` — commentaire mis à jour (H1)
- `internal/service/killswitch.go` — log rollback indéterminé (M1) + `ForceCaptivePortalForTest` (M3)
- `internal/ui/httpserver.go` — CSRF token per-process + endpoint `/api/csrf-token` + `requireCSRF` (M2) + `eventSlot` TTL + `DrainPendingUIEvent` (L3)
- `internal/ui/httpserver_test.go` — 4 nouveaux tests (CSRF: rejected-missing, rejected-wrong, endpoint, retry-stale)
- `frontend/src/app.js` — `getCSRFToken` cache + header X-CSRF-Token + retry-on-403 (M2) + bandeau optimiste (L2)
- `internal/ui/ui.go` — `closedChan` → `neverFiresChan` (L1)
- `internal/ipchandler/handler_test.go` — `TestHandle_SetKillSwitchMode_RefusedDuringCaptive` (M3)

### Change Log

- 2026-04-18 : Implémentation complète Story 5.9 — Mode dégradé kill switch + indicateur visuel permanent + CLI levoile-ctl + token machine-local. 46 nouveaux tests, 0 régression sur la suite complète. Couvre AC1-8.
- 2026-04-18 : Code review adversarial → 8 findings (2 HIGH, 3 MEDIUM, 3 LOW), tous corrigés (+5 tests supplémentaires). Voir « Senior Developer Review (AI) ».

## Senior Developer Review (AI)

**Review Date :** 2026-04-18
**Reviewer :** claude-opus-4-7[1m] (self-review, mode adversarial)
**Outcome :** Approve (after fixes — 8/8 findings résolus)

### Findings + Action Items (tous résolus)

- [x] **[HIGH] H1** : Token Windows lisible par utilisateurs non-admin → `perms_windows.go` applique maintenant un DACL explicite (LocalSystem + Administrators GENERIC_ALL, `PROTECTED_DACL_SECURITY_INFORMATION` bloque l'héritage de `%ProgramData%`). Fichier supprimé sur échec ACL pour ne pas laisser de token largement lisible. NSIS comment-only updated (Go-side hardening suffit).
- [x] **[HIGH] H2** : `persistFirewallEnabled` race avec autres writers → nouveau `config.Mu sync.Mutex` exposé depuis `internal/config`. `cmd/client/main.go` `persistFirewallEnabled` prend `config.Mu` autour de Load+Save. `internal/ipchandler` `configMu` réécrit comme alias `&config.Mu` (zéro diff dans les bodies handlers, mais désormais partagé entre packages).
- [x] **[MED] M1** : Rollback firewall silencieusement ignoré → log explicite `service: killswitch rollback firewall failed (state INDETERMINATE): persist_err=... rollback_err=...` quand l'Activate de rollback échoue.
- [x] **[MED] M2** : `/api/settings/killswitch` sans auth → CSRF token per-process (32 bytes hex, `crypto/rand`) servi par `/api/csrf-token`, requis dans header `X-CSRF-Token`, validé constant-time. Frontend cache token + retry-on-403 pour fenêtres de redémarrage UI. Documenté comme defense-in-depth (vrai isolation = unix socket, deferred Epic 7).
- [x] **[MED] M3** : Pas de test IPC dédié captive → `TestHandle_SetKillSwitchMode_RefusedDuringCaptive` ajouté + helper `Program.ForceCaptivePortalForTest`.
- [x] **[LOW] L1** : `closedChan` mal nommée → renommée `neverFiresChan` avec commentaire explicite « never closed by design ».
- [x] **[LOW] L2** : Gap visuel modal-close → bandeau-apparait → bandeau affiché de manière optimiste dans `confirmKillSwitchDegraded` juste après réponse `ok`, le polling continue à le tenir sync.
- [x] **[LOW] L3** : `pendingUIEvent` orphelin → `eventSlot` stocke maintenant `setAt` et expire les events après `eventSlotTTL = 10 s`. Helper `DrainPendingUIEvent` exposé pour wipe explicite côté lifecycle.

### Tests post-fix

- `go test ./... -timeout 300s` → **PASS** sur 35 packages, 0 régression
- `go test -race` sur les packages touchés → **PASS** (race pré-existante `TestSTUNActive_AfterStop` toujours présente, sans rapport)
- `go vet ./...` Linux + `GOOS=windows go vet ./internal/ctlauth/...` → propres
- **5 nouveaux tests** ajoutés post-review (3 CSRF, 1 captive IPC, autres couverts par tests existants) → **51 tests Story 5.9 au total**.

## Open Questions for Dev

1. **Hook auto-restore (Task 2)** : faut-il observer le state tunnel via channel (`tunnel.State()` expose-t-il un channel ?) ou via callback enregistré sur le `Reconnector` ? À explorer dans le code [internal/tunnel/state.go](internal/tunnel/state.go) en début de tâche. Si aucun channel/callback n'existe, ajouter un mécanisme léger (canal buffered consommé par une goroutine du service) plutôt qu'un polling.
2. **Visibilité du bandeau dans le serveur HTTP local** : si le webview est fermé (tray seul) puis ré-ouvert, le frontend repolll `/api/status` immédiatement et affiche le bandeau. OK. Mais que faire si l'UI crash et redémarre via supervision (Story 5.7) ? L'état est persisté en TOML, le service le retourne au prochain status — auto-récupère. À documenter explicitement dans le README.
3. **`levoile-ctl killswitch on` quand le tunnel n'est pas connecté** : refuser ou exécuter quand même `firewall.Activate` ? Recommandation : refuser avec message « tunnel non connecté — utilisez `levoile-ctl reconnect` d'abord » pour éviter de bloquer internet sans tunnel utilisable. À valider lors du dev.
4. **Ordre tray menu** : la spec dit « Ouvrir / [Mode dégradé] / Quitter ». Faut-il ajouter un séparateur entre « Ouvrir » et « Mode dégradé » pour bien isoler visuellement la commande critique ? Pattern macOS : oui ; pattern Windows : moins courant. Décider avec un screenshot pendant Task 4.
5. **Localisation du token dans CI** : les e2e tests (Task 11) doivent générer un token et l'utiliser pour piloter via `levoile-ctl`. Prévoir un helper de test `testutil.WriteCtlToken(t, dir)` qui crée le token + retourne sa valeur.
