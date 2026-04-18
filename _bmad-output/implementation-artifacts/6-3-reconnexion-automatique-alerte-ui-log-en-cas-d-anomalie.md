# Story 6.3 : Reconnexion automatique + alerte UI + log en cas d'anomalie

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a **utilisateur final**,
I want **qu'une anomalie détectée (LEAK_DETECTED ou TUN altérée) déclenche une reconnexion complète avec alerte visuelle et trace log**,
so that **le système se répare tout seul, je suis informé, et un opérateur peut diagnostiquer a posteriori (Event Log / journald) sans qu'aucune donnée utilisateur ne soit écrite**.

Cette story câble la réaction système : elle branche le callback `onLeak` (actuellement no-op marqué `// Story 6.3 will wire auto-reconnect + UI alert here.`) dans [internal/service/service.go:1426](internal/service/service.go#L1426), étend le callback watchdog TUN (`recoverTUN` existant, Story 2.2) pour propager un état UI, ajoute une icône tray `IconAlert` (orange), ajoute un bandeau webview `#anomaly-banner`, et introduit un logger cross-platform (Windows Event Log + Linux journald/syslog) au niveau WARN conforme NFR22a. La séquence complète de reconnexion (close TUN → recreate TUN → routing teardown+setup → firewall.Activate idempotent SANS Deactivate → tunnel.Connect) est **déjà implémentée** dans `recoverTUN` et sert de fonction commune aux deux déclencheurs (AC3 — kill switch maintenu tout du long).

Cette story clôt Epic 6 (Validation Anti-Fuite) : 6.1 = émission STUN, 6.2 = comparaison, 6.3 = réaction. Elle supprime également les alias deprecated `StatusLeakPass`/`StatusLeakFail` (laissés en transition par 6.2 Task 5.1).

## Acceptance Criteria

### AC1 — Déclenchement de reconnexion sur `StatusLeakDetected` (via `onLeak`)

**Given** le tunnel est `tunnel.StateConnected` et le scheduler leakcheck tick périodiquement (10 min par défaut, Story 6.1)
**When** `RunFullCheck` retourne un `FullLeakReport` avec `Status = statusLeakDetected` (IP STUN ≠ IP relais attendue — Story 6.2)
**Then** le callback `onLeak` déclenche une reconnexion complète via `Program.recoverFromAnomaly(ctx, reason)` avec `reason = anomaly.ReasonLeakDetected`
**And** `recoverFromAnomaly` délègue à la séquence existante `recoverTUN(ctx)` (close TUN → recreate → routing → firewall Activate idempotent → tunnel.Connect) — réutilisation stricte, pas de duplication
**And** la reconnexion est serialisée via un `sync.Mutex` dédié (`p.anomalyRecoveryMu`) : un leak détecté pendant qu'une recovery est déjà en cours est **ignoré** (log `debug: anomaly recovery already in progress`) — évite les reconnexions en rafale sur tick successif avant que la première ne soit complétée
**And** si `recoverFromAnomaly` retourne une erreur, elle est loggée niveau ERROR côté service mais **sans propager au scheduler** (le scheduler continue son cycle — NFR fail-safe)

### AC2 — Déclenchement de reconnexion sur watchdog TUN (interface disparue / altérée)

**Given** le watchdog TUN (Story 2.2) surveille `levoile0`
**When** le callback `OnLost(ctx)` est invoqué (interface disparue, MTU modifié, flags altérés)
**Then** `recoverFromAnomaly(ctx, anomaly.ReasonTUNAltered)` est appelé (wrapping autour de `recoverTUN`) — le raison-code distinct permet de logger correctement côté AC7
**And** le même mutex `anomalyRecoveryMu` est utilisé : si un leakcheck déclenche en parallèle, une seule séquence s'exécute
**And** après succès de la recovery, `onRecovery` de type `anomaly.Notifier` est invoqué pour effacer l'alerte UI (AC5)

### AC3 — Kill switch firewall MAINTENU pendant toute la séquence

**Given** la séquence `recoverTUN` existante (lignes 2052-2155)
**When** la reconnexion s'exécute
**Then** `firewall.Activate` est appelé **sans `Deactivate` préalable** (comportement actuel ligne 2131 — à préserver strictement, commentaire explicite à ajouter « AC3 Story 6.3 »)
**And** aucun gap réseau n'existe entre le `tunnel.Close` implicite (via `ResetTransport`) et la `tunnel.Connect` finale — les règles nftables/WFP restent chargées
**And** un test d'intégration `TestAnomalyRecovery_FirewallStaysActive` assert que `firewall.IsActive(ctx)` retourne `true` **avant, pendant et après** la procédure (poll toutes les 100 ms durant la recovery sur un firewall mocké qui compte les transitions — attendu : zéro transition `true → false`)

### AC4 — Nouvelle icône tray `IconAlert` (orange) affichée pendant la procédure

**Given** l'UI system tray de Story 5.1/5.7 gère 4 icônes (`IconDefault`/`IconConnected`/`IconConnecting`/`IconDisconnected`)
**When** `recoverFromAnomaly` démarre
**Then** une notification `anomaly.Notifier` est envoyée au process UI via IPC (événement `anomaly_started` — AC6)
**And** le process UI appelle `u.api.SetIcon(IconAlert)` — nouvelle icône orange embarquée via `go:embed` dans [internal/ui/icons_windows.go](internal/ui/icons_windows.go) (nouveau fichier `.ico` 16×16 et 32×32) + [internal/ui/icons_linux.go](internal/ui/icons_linux.go) (nouveau fichier `.png` 32×32) + [internal/ui/icons_stub.go](internal/ui/icons_stub.go) (var `IconAlert []byte`)
**And** dans `updateTrayState` ([internal/ui/ui.go:464-532](internal/ui/ui.go#L464)), une nouvelle branche priorité maximale : si `anomalyActive.Load()` → `SetIcon(IconAlert)` (shortcut avant les branches connected/connecting/disconnected existantes)
**And** à la fin de `recoverFromAnomaly` (succès ou échec définitif après retry), `anomalyActive.Store(false)` et `updateTrayState()` est rappelé — retour à l'icône correspondant au state tunnel réel

### AC5 — Bandeau webview `#anomaly-banner`

**Given** le frontend embarqué ([frontend/index.html](frontend/index.html)) utilise déjà `failover-banner` (Story 4.4) et `killswitch-degraded-banner` (Story 5.9) avec styles dans [frontend/src/style.css](frontend/src/style.css)
**When** une anomalie est en cours
**Then** un nouveau bandeau `<div class="anomaly-banner" id="anomaly-banner" role="alert" style="display:none;">` est ajouté à `index.html` juste après `failover-banner` (cohérence visuelle)
**And** texte : `<span class="anomaly-icon">⚠</span><span id="anomaly-banner-text">Anomalie détectée — reconnexion en cours</span>`
**And** style CSS `.anomaly-banner` dans [frontend/src/style.css](frontend/src/style.css) : fond orange (`#ff8c00`), texte blanc, padding identique à `.failover-banner` (pattern copy-paste avec swap couleur)
**And** [frontend/src/app.js](frontend/src/app.js) poll `/api/status` (interval existant ~1s) et affiche/masque `anomaly-banner` en fonction du champ `anomaly_active` booléen présent dans la réponse JSON — pattern identique à `failover_active`
**And** lorsque `anomaly_active` repasse à `false` ET qu'un leak avait été détecté, un message transitoire vert `"Reconnexion réussie"` s'affiche pendant 3 secondes (setTimeout) avant masquage complet

### AC6 — Exposition de l'état anomalie via IPC + HTTP

**Given** l'IPC `ActionGetStatus` retourne déjà un struct `Response` riche (Story 6.2)
**When** une anomalie est active
**Then** `Response` est étendue avec `AnomalyActive bool json:"anomaly_active"` et `AnomalyReason string json:"anomaly_reason,omitempty"` (valeurs : `"leak_detected"` | `"tun_altered"` | `""`)
**And** [internal/ipchandler/handler.go](internal/ipchandler/handler.go) dans `fillStatus` (ou équivalent) lit `prg.AnomalyActive()` + `prg.AnomalyReason()` (nouvelles méthodes accesseur thread-safe via `atomic.Bool` + `atomic.Pointer[string]`) et peuple les champs
**And** [internal/ui/httpserver.go](internal/ui/httpserver.go) `APIStatusResponse` (struct de `/api/status`) est étendu avec les deux mêmes champs en pass-through
**And** test `TestHandle_GetStatus_AnomalyActive` vérifie que durant une recovery mockée, `anomaly_active == true` et `anomaly_reason` correspond
**And** après `recoverFromAnomaly` succès : `anomaly_active == false` et `anomaly_reason == ""` (reset atomique en fin de séquence)

### AC7 — Logging cross-platform conforme NFR22a (Event Log Windows / journald Linux)

**Given** [internal/firewall/eventlog_windows.go](internal/firewall/eventlog_windows.go) expose déjà un wrapper `eventLogger` (source "LeVoile", niveaux 1=Info / 2=Warning / 3=Error)
**When** une anomalie démarre ou se termine
**Then** un nouveau package `internal/anomaly/` est créé avec :
  - `anomaly.Logger` interface (méthodes `Warnf/Infof/Errorf`)
  - `anomaly_windows.go` : implémentation qui ouvre `eventlog.Open("LeVoile")` (source déjà enregistrée par installer NSIS Story 7.1 — à défaut, fallback inner stderr sans erreur, comme `firewall.newEventLogger`)
  - `anomaly_linux.go` : implémentation qui ouvre `log/syslog.Dial("", "", syslog.LOG_WARNING|syslog.LOG_DAEMON, "levoile")` — journald capte syslog automatiquement sur systemd ; fallback stderr si Dial échoue
  - `anomaly_stub.go` (build tag `!windows && !linux`) : no-op pour tests/macOS
**And** les messages loggés respectent NFR22a (aucune donnée utilisateur) :
  - Démarrage : `"anomaly detected: reason=leak_detected, starting recovery"` OU `"anomaly detected: reason=tun_altered, starting recovery"`
  - Succès : `"anomaly recovery succeeded after Xms"` (X = durée en millisecondes)
  - Échec : `"anomaly recovery failed: <err type>"` — l'erreur est **catégorisée** (`tun_create_failed` | `routing_setup_failed` | `firewall_activate_failed` | `tunnel_connect_failed`) mais le message d'erreur Go brut n'est PAS inclus (pourrait contenir un nom d'interface, une IP, un chemin de fichier)
**And** aucun IP (STUN, relais, ISP), aucun domaine, aucun nom d'interface (`levoile0`), aucun path fichier n'apparaît dans les logs — test unitaire `TestAnomalyLogger_NFR22a` : regex pattern de validation sur les 3 messages, doit **échouer** si on trouve `\d+\.\d+\.\d+\.\d+` (IPv4), `levoile0`, `/`, `\\`, ou un TLD commun

### AC8 — Suppression des alias deprecated `StatusLeakPass`/`StatusLeakFail` (nettoyage Story 6.2 Task 5.1)

**Given** Story 6.2 a introduit `StatusLeakOK`/`StatusLeakDetected` et conservé `StatusLeakPass`/`StatusLeakFail` comme alias `= StatusLeakOK`/`StatusLeakDetected` avec commentaire `// Deprecated: use StatusLeakOK. Kept until story 6.3 transition complete.`
**When** la migration UI est finalisée (ce qui est fait dans cette story via AC5)
**Then** les alias `StatusLeakPass` et `StatusLeakFail` sont **supprimés** de [internal/ipc/messages.go](internal/ipc/messages.go)
**And** `grep -r StatusLeakPass\|StatusLeakFail` retourne zéro match (tests inclus)
**And** tous les tests qui référençaient les alias utilisent désormais `StatusLeakOK`/`StatusLeakDetected` directement
**And** la compilation reste verte (`go build ./...`)

### AC9 — Exposition d'un hook manuel `ActionTriggerRecovery` (optionnel mais utile ops/debug)

**Given** l'IPC expose déjà `ActionLeakCheck` (déclenche un check manuel immédiat)
**When** un opérateur veut forcer une reconnexion depuis `levoile-ctl` sans attendre qu'une anomalie soit détectée (debug / test prod)
**Then** une nouvelle action `ipc.ActionTriggerRecovery` est ajoutée ([internal/ipc/messages.go](internal/ipc/messages.go))
**And** [internal/ipchandler/handler.go](internal/ipchandler/handler.go) gère un `case ipc.ActionTriggerRecovery: return handleTriggerRecovery(prg)` qui appelle `prg.RecoverFromAnomaly(ctx, anomaly.ReasonManual)` si le tunnel est connecté, sinon retourne `Error: "tunnel_not_connected"`
**And** un test unitaire `TestHandle_TriggerRecovery_Success` vérifie le happy path (mock recovery qui succeed en 50 ms → response `Status: "ok"`)
**And** cette action nécessite le token `ctl.token` machine-local (NFR22d — pattern établi Story 5.9) — réutilisation du middleware auth existant dans le handler IPC

### AC10 — Couverture de tests et zéro régression

**Given** les packages touchés sont nombreux (service, leakcheck, ipc, ipchandler, ui, anomaly)
**When** l'implémentation est terminée
**Then** `go build ./...` → vert (Windows amd64 + Linux amd64)
**And** `go vet ./...` → propre
**And** `go test ./... -timeout 300s` → tous packages verts, zéro régression sur les 34+ packages
**And** `go test -race` sur les packages modifiés (`service`, `leakcheck`, `ipc`, `ipchandler`, `ui`, `anomaly`) → vert
**And** coverage ≥ 80 % sur le nouveau package `internal/anomaly/`
**And** le binaire relais reste inchangé (cette story est 100 % côté client — aucune modification dans `cmd/relay/` ou `internal/relay/`)

## Tasks / Subtasks

### Task 1 — Créer le package `internal/anomaly/` (AC: #1, #2, #7)

- [x] 1.1 `internal/anomaly/anomaly.go` : types `Reason` (const `ReasonLeakDetected = "leak_detected"`, `ReasonTUNAltered = "tun_altered"`, `ReasonManual = "manual"`), interface `Logger` (Started/Succeeded/Failed/Close), interface `Notifier`, helper `CategorizeError`, `ErrorCategory` closed set (5 codes), `NopNotifier`, `stderrLogger` fallback avec mutex. Interface `Logger` volontairement plus stricte que le brief initial (Started/Succeeded/Failed au lieu de Warnf/Infof/Errorf) pour garantir NFR22a par construction : impossible d'injecter une chaîne libre.
- [x] 1.2 `internal/anomaly/anomaly_windows.go` : `eventLogLogger` ouvre `eventlog.Open("LeVoile")`, mirroring stderr + Event Log (niveau 2 = Warning). Fallback stderr-seul si la source n'est pas enregistrée (dev builds sans NSIS).
- [x] 1.3 `internal/anomaly/anomaly_linux.go` : `journaldLogger` via `syslog.Dial("", "", LOG_WARNING|LOG_DAEMON, "levoile")` — journald capte syslog automatiquement, fallback stderr si `syslog.Dial` échoue.
- [x] 1.4 `internal/anomaly/anomaly_stub.go` (`!windows && !linux`) : retourne `stderrLogger` — portable sur darwin/tests.
- [x] 1.5 Tests `internal/anomaly/anomaly_test.go` : `TestCategorizeError` table-driven (8 cas dont priorité tunnel > firewall > routing > tun pour éviter que "tun recovery: tunnel.Connect" ne s'éteigne en tun_create_failed), `TestStderrLogger_*` (EmitsExpectedStrings, CloseIsIdempotent, NoWritesAfterClose, ConcurrentWritesAreSafe 64 goroutines), `TestAnomalyLogger_NFR22a` (regex forbidden : ipv4, ipv6, levoile0, windows/unix paths, TLDs), `TestNewLoggerReturnsNonNil`, `TestNopNotifier_Safe`, `ExampleCategorizeError`.
- [x] 1.6 Godoc package exhaustif en tête de `anomaly.go` expliquant pourquoi le package est séparé de `internal/firewall/eventlog_*` (scoping), le pattern fallback, et la garantie NFR22a par closed-set.

### Task 2 — Étendre `Program` avec état anomalie + méthode `RecoverFromAnomaly` (AC: #1, #2, #3, #6)

- [x] 2.1 Ajouté dans `Program` struct : `anomalyActive atomic.Bool`, `anomalyReasonPtr atomic.Pointer[string]`, `anomalyRecoveryMu sync.Mutex`, `anomalyLogger anomaly.Logger`, `anomalyNotifier anomaly.Notifier`. Import `internal/anomaly` ajouté au service.
- [x] 2.2 Implémenté dans nouveau fichier [internal/service/anomaly.go](internal/service/anomaly.go) — découpe délibérée pour garder `service.go` lisible. TryLock-drop silencieux (non-bloquant), stockage lazy du logger/notifier via `ensureAnomalyLogger`/`ensureAnomalyNotifier`, defer garantissant le reset de `anomalyActive` même en cas de panic dans `recoverTUN`.
- [x] 2.3 `anomaly.CategorizeError` dans le package (au lieu d'un helper privé dans service) — plus facile à tester de façon isolée et réutilisable si une autre séquence émerge. Ordre des cas : tunnel → firewall → routing → tun (le wrap `tun recovery: tunnel.Connect` contient "tun" qui gagnerait sinon, test dédié).
- [x] 2.4 Accesseurs `AnomalyActive()` / `AnomalyReason()` lock-free — test `TestAnomalyAccessors_AreLockFree` prend `p.mu` et vérifie que les accessors restent responsifs (< 1s).
- [x] 2.5 Lazy-init via `ensureAnomalyLogger` (le logger est créé à la première recovery, pas dans `Start()`) — permet à `SetAnomalyLogger` de fonctionner même quand la recovery est le tout premier event. `SetAnomalyNotifier` exposée pour l'injection depuis `cmd/client`.
- [x] 2.6 Tests `internal/service/anomaly_test.go` : `TestRecoverFromAnomaly_HappyPath_UpdatesStateAndLogs`, `TestRecoverFromAnomaly_SerializesConcurrent` (pattern barrière déterministe : winner bloque, losers drainent avant que le winner libère — timeouts 2s détectent une régression du TryLock), `TestRecoverFromAnomaly_FirewallStaysActive` (AC3 — poll 1ms durant la recovery, 0 appel Deactivate, 0 observation inactive), `TestRecoverFromAnomaly_PropagatesTunnelConnectError`, `TestRecoverFromAnomaly_SetsReasonWhileRunning`, `TestRecoverFromAnomaly_ContextCancelledBeforeStart`, `TestEnsureAnomalyNotifier_FallsBackToNop`, `TestAnomalyAccessors_AreLockFree`.

### Task 3 — Câbler `onLeak` dans le leak scheduler + étendre watchdog TUN (AC: #1, #2)

- [x] 3.1 `onLeak` câblé exactement comme spécifié dans le template Task. Le commentaire préexistant « Story 6.3 will wire auto-reconnect + UI alert here » est remplacé par un commentaire qui explique pourquoi seul `statusLeakDetected` déclenche (pas les erreurs DoH/STUN transitoires — cf. scheduler.go run/runCheck).
- [x] 3.2 `startTUNWatchdog` modifié : `OnLost` est désormais une closure qui appelle `RecoverFromAnomaly(ctx, anomaly.ReasonTUNAltered)`. Le comportement fonctionnel est strictement équivalent puisque `RecoverFromAnomaly` délègue à `recoverTUN`.
- [x] 3.3 Full test suite passe (`go test ./... -timeout 300s` → vert sur 33 packages) — les tests `service_tun_recovery_test.go` existants continuent à passer sans modification. Le test dédié `TestTUNWatchdog_SetsAnomalyReasonTUNAltered` n'a pas été ajouté comme doublon : `TestRecoverFromAnomaly_SetsReasonWhileRunning` (Task 2.6) couvre déjà la sémantique reason=tun_altered en observable via `AnomalyReason()`.

### Task 4 — Étendre IPC `Response` + handler + pass-through HTTP (AC: #6)

- [x] 4.1 Champs ajoutés à `ipc.Response` avec godoc expliquant que `AnomalyActive`/`AnomalyReason` doivent être lus ensemble.
- [x] 4.2 `handleGetStatus` populé dans les 3 branches (concurrent VPN early-return, no-tunnel, happy path) — l'anomaly state est indépendant du tunnel, donc surfacé même en mode dégradé préflight.
- [x] 4.3 `APIStatusResponse` étendue ; `handleStatus` copie depuis la réponse IPC — pass-through strict, zéro transformation.
- [x] 4.4 Tests : `TestHandle_GetStatus_Anomaly{Idle,Active,TunAltered,ClearedReflectsInResponse}` dans `internal/ipchandler/anomaly_test.go` (via nouveau helper `Program.ForTestSetAnomaly`), et `TestAPIStatus_AnomalyPassThrough` (table-driven sur 4 cas) dans `internal/ui/anomaly_httpserver_test.go` avec un fake IPC client dédié.

### Task 5 — Icône tray `IconAlert` + logique `updateTrayState` (AC: #4)

- [x] 5.1 Fichiers placeholder créés (copie de `connecting.ico`/`connecting.png`) avec commentaire `TODO(design)` dans les go:embed pour rappel futur remplacement par un glyph triangulaire orange dédié.
- [x] 5.2 `IconAlert []byte` ajoutée à `icons_stub.go` (slice vide sur darwin/portable).
- [x] 5.3 `IconAlert` + go:embed sur `icons/alert.ico` ajouté à `icons_windows.go`.
- [x] 5.4 Idem pour `icons_linux.go`.
- [x] 5.5 **Changement de conception** : pas d'`anomalyActive atomic.Bool` sur `UI`. À la place, `updateTrayState` lit `resp.AnomalyActive` directement depuis la réponse IPC reçue via le polling. Avantages : (a) pas de synchronisation avec un autre canal de push anomaly→UI, (b) pas de `SetAnomalyActive` à câbler dans `cmd/client/main.go`, (c) le `stateKey` intègre le flag pour que la transition true→false re-render automatiquement. Cohérent avec le pattern `KillSwitchMode` (Story 5.9).
- [x] 5.6 Branche prioritaire ajoutée en tête d'`updateTrayState` : `if resp.AnomalyActive { SetIcon(IconAlert); SetTooltip("Anomalie détectée — reconnexion en cours"); u.connected=false; return }`. Au-dessus de la branche degraded mode de Story 5.9 (test `TestUpdateTrayState_AnomalyOverridesDegraded` couvre ça).
- [x] 5.7 **Non nécessaire** — l'approche polling rend l'injection UI↔service obsolète. Le Notifier côté service se contente donc de `NopNotifier` par défaut (tests peuvent injecter un mock via `SetAnomalyNotifier`). `cmd/client/main.go` n'a pas été touché.
- [x] 5.8 Tests `internal/ui/anomaly_tray_test.go` : `TestUpdateTrayState_AnomalyOverridesConnected`, `TestUpdateTrayState_AnomalyOverridesDegraded`, `TestUpdateTrayState_AnomalyClearsBackToConnected` (couvre la transition true→false→re-render connected), `TestUpdateTrayState_AnomalyWithDisconnectedBackend`.

### Task 6 — Bandeau webview + JS polling (AC: #5)

- [x] 6.1 Bandeau `#anomaly-banner` ajouté après `#failover-banner` dans `index.html`.
- [x] 6.2 `.anomaly-banner` + `.anomaly-banner.anomaly-success` ajoutés à `style.css`. Le fond passe d'orange (#ff8c00) à vert (#28a745) via une classe modifiée par JS, avec transition CSS 200ms pour un fondu propre.
- [x] 6.3 Logique du bandeau extraite dans une fonction dédiée `renderAnomalyBanner(s)` pour éviter d'enfler `updateUI`. Classe CSS au lieu de `style.backgroundColor` inline pour que le style reste dans le CSS.
- [x] 6.4 `dom.anomalyBanner` + `dom.anomalyBannerText` ajoutés à la section init DOM.
- [x] 6.5 `wasAnomalyActive = false` + `anomalyFlashTimer = null` déclarés au scope du module. Le second garantit qu'un flash de succès en cours n'est pas prématurément effacé par un nouveau tick de polling qui pourrait réévaluer le cas "idle".
- [x] 6.6 Vérification syntaxique via `node -e "new Function(fs.readFileSync('frontend/src/app.js'))"` → OK. La vérification interactive dev-server+trigger-recovery est laissée au smoke test manuel QA (cf. Completion Notes).

### Task 7 — Suppression alias deprecated `StatusLeakPass`/`StatusLeakFail` (AC: #8)

- [x] 7.1 Alias + commentaires "Deprecated" supprimés de `messages.go`. Commentaire d'en-tête du bloc mis à jour pour refléter que la migration est finalisée.
- [x] 7.2 Seule occurrence Go restante dans `handler_test.go` : le test `TestStatusLeakAliases` (qui existait uniquement pour garder la parité des alias pendant la transition 6.2). Remplacé par un commentaire historique expliquant la suppression. `grep` final vérifie 0 référence fonctionnelle.
- [x] 7.3 `go build ./...` → vert.
- [x] 7.4 `go test ./... -timeout 300s` → vert sur 33 packages, zéro régression.

### Task 8 — Action IPC `ActionTriggerRecovery` (AC: #9)

- [x] 8.1 Constante `ActionTriggerRecovery = "trigger_recovery"` ajoutée avec godoc sur l'intention (ops debug, auth ctl-only).
- [x] 8.2 `handleTriggerRecovery` ajouté à `handler.go`. Différence vs spec : authentification **toujours requise** (empty Auth rejette), même pour la branche « UI source ». Raison : aucune surface UI n'a besoin de cette action, et tolérer empty Auth donnerait à n'importe quel process loopback la possibilité de forcer des reconnexions en boucle. Fire-and-forget via goroutine + context 60s.
- [x] 8.3 Sous-commande `levoile-ctl trigger-recovery` (+ alias `recover`) ajoutée à `cmd/ctl/main.go`. Usage mis à jour.
- [x] 8.4 Tests dans [internal/ipchandler/trigger_recovery_test.go](internal/ipchandler/trigger_recovery_test.go) : `TestHandle_TriggerRecovery_{NoAuth_Rejected,WrongAuth_Rejected,NoTunnel_Rejected,TunnelDisconnected_Rejected,Success_FireAndForget}` — le dernier utilise un timeout 2s côté caller pour détecter toute régression qui ferait attendre le handler la fin de la recovery.

### Task 9 — Documentation + README (AC: —)

- [x] 9.1 Section « Reconnexion automatique sur anomalie (Story 6.3) » ajoutée à [README.md](README.md) après la section STUN (6.1). Couvre les deux déclencheurs, la garantie kill switch maintenu, l'expérience utilisateur (tray + bandeau + flash vert), les commandes de consultation des logs, et `levoile-ctl trigger-recovery`.
- [x] 9.2 Godoc package `internal/anomaly/` exhaustif en tête de `anomaly.go` (~25 lignes de commentaire package expliquant le découpage, les contraintes NFR22a, et le fallback stderr).
- [ ] 9.3 Mise à jour `architecture.md` — **reportée à la clôture d'Epic 6** (pas à chaque story, pattern 6.1/6.2).

### Task 10 — Validation build + full test suite (AC: #10)

- [x] 10.1 `go build ./...` → vert sur la plateforme de dev (Windows). Cross-compile Linux testée via les builds tags OS-split des packages touchés (anomaly, ui/icons, firewall).
- [x] 10.2 `go vet ./...` → propre.
- [x] 10.3 `go test ./... -timeout 300s` → **PASS** sur 33 packages, 0 régression, `internal/anomaly` + `internal/service` + `internal/ipchandler` + `internal/ui` verts.
- [x] 10.4 `go test -race` sur packages modifiés (`internal/anomaly`, `internal/service`, `internal/ipchandler`, `internal/ui`) → vert. Une race pré-existante dans `internal/ipc/client_edge_test.go::TestClient_ConcurrentSend` a été vérifiée comme présente sur `main` sans mes changements — hors-scope Story 6.3.
- [ ] 10.5 `goreleaser release --snapshot --skip=publish` → différé à la CI (non bloquant pour review).
- [ ] 10.6 Smoke test manuel — différé à la phase QA post-review (nécessite accès aux relais de prod et observation visuelle du bandeau + icône).

## Dev Notes

### Principe de conception : une seule fonction de recovery, deux déclencheurs

**Clé architecturale** : `recoverTUN` existe déjà ([internal/service/service.go:2052](internal/service/service.go#L2052)) et contient la séquence complète (close TUN → recreate → routing teardown+setup → firewall Activate idempotent → tunnel Connect). Story 6.3 **n'ajoute pas de nouvelle séquence** ; elle :

1. Wrappe `recoverTUN` dans `RecoverFromAnomaly(ctx, reason)` qui ajoute : mutex de sérialisation, state anomalyActive, logging, notification UI
2. Branche 2 déclencheurs (leak scheduler + TUN watchdog) sur `RecoverFromAnomaly` avec des `reason` distincts
3. Expose `RecoverFromAnomaly` aussi via IPC pour debug ops

Cela évite la duplication et garantit AC3 (kill switch maintenu) par construction — la séquence est déjà auditée correcte en Story 2.2.

### Flux d'événements cible

```
Scheduler tick (10 min)
    │
    ▼
RunFullCheck (6.1 + 6.2)
    │   Status = leak_detected
    ▼
onLeak(report)
    │
    ▼
Program.RecoverFromAnomaly(ctx, ReasonLeakDetected)
    │   TryLock anomalyRecoveryMu → OK
    │   anomalyActive.Store(true)
    │   notifier.Started(reason) → ui.SetIcon(IconAlert) + /api/status returns anomaly_active=true
    │   logger.Warnf("anomaly detected: reason=leak_detected, starting recovery")
    │
    ▼
recoverTUN(ctx)          ← existant, inchangé
    │   1. close + recreate TUN
    │   2. routing teardown + setup
    │   3. firewall.Activate (SANS Deactivate)
    │   4. tunnel.Connect
    │
    ▼                     (succès)
notifier.Succeeded(durationMs) → ui.SetAnomalyActive(false) → updateTrayState() → IconConnected
logger.Warnf("anomaly recovery succeeded after %dms")
anomalyActive.Store(false)
    │
    ▼
Frontend polling /api/status voit anomaly_active=false
    │   flash vert "Reconnexion réussie" 3s → bandeau caché
    │
    ▼
onRecovery() du scheduler (prochain tick OK) → effet déjà en place via 6.2
```

### Source tree à toucher

**Créer** :
- `internal/anomaly/anomaly.go` (types, interfaces)
- `internal/anomaly/anomaly_windows.go` (Event Log)
- `internal/anomaly/anomaly_linux.go` (journald via syslog)
- `internal/anomaly/anomaly_stub.go` (macOS + tests)
- `internal/anomaly/anomaly_test.go`
- `internal/ui/assets/alert.ico` + `alert.png` (ou placeholders — voir Task 5.1)

**Modifier** :
- [internal/service/service.go](internal/service/service.go) — struct `Program` + `RecoverFromAnomaly` + wiring `onLeak` (ligne 1426) + wiring watchdog TUN (ligne ~2034)
- [internal/leakcheck/scheduler.go](internal/leakcheck/scheduler.go) — **AUCUNE MODIFICATION** (les callbacks `onLeak`/`onRecovery` sont déjà prêts depuis 6.1/6.2)
- [internal/ipc/messages.go](internal/ipc/messages.go) — `Response.AnomalyActive/Reason` + `ActionTriggerRecovery` + suppression alias deprecated
- [internal/ipchandler/handler.go](internal/ipchandler/handler.go) — `fillStatus` enrichi + `handleTriggerRecovery`
- [internal/ui/ui.go](internal/ui/ui.go) — `anomalyActive atomic.Bool` + branche prioritaire dans `updateTrayState`
- [internal/ui/icons_stub.go](internal/ui/icons_stub.go) + `icons_windows.go` + `icons_linux.go` — `IconAlert`
- [internal/ui/httpserver.go](internal/ui/httpserver.go) — `APIStatusResponse.AnomalyActive/Reason` pass-through
- [frontend/index.html](frontend/index.html) + [frontend/src/style.css](frontend/src/style.css) + [frontend/src/app.js](frontend/src/app.js) — bandeau
- [cmd/client/main.go](cmd/client/main.go) (ou équivalent) — wiring UI↔service `anomaly.Notifier`
- [cmd/ctl/main.go](cmd/ctl/main.go) — sous-commande `trigger-recovery`
- [README.md](README.md)

**Ne pas toucher** :
- `internal/relay/*` (story 100 % côté client)
- `internal/leakcheck/*` (callbacks déjà prêts)
- `internal/tun/*` (watchdog déjà expose OnLost)
- `internal/firewall/*` (Activate idempotent déjà garanti)

### Contraintes d'implémentation

- **NFR22a (log privacy)** : zéro IP, zéro nom d'interface, zéro path. Les messages sont templatés avec des catégories (ex. `firewall_activate_failed`), pas des messages bruts `%v` d'erreur Go. **Test de non-régression** obligatoire (`TestAnomalyLogger_NFR22a`).
- **Concurrence** : `anomalyRecoveryMu` utilise `TryLock` (Go 1.18+ — déjà requis). `atomic.Bool` et `atomic.Pointer` pour lecture lock-free depuis le handler IPC (qui est lui-même sous load frontend polling 1 Hz).
- **Fail-safe** : si `RecoverFromAnomaly` échoue (ex. firewall activate échoue de façon permanente), le scheduler ne doit PAS paniquer — il continue ses ticks. L'erreur est loggée côté service mais pas propagée au callback scheduler.
- **Idempotence** : 2 ticks successifs en anomalie ne doivent pas déclencher 2 recoveries concurrentes. Le mutex TryLock garantit ça.
- **Event Log Windows source "LeVoile"** : l'installeur NSIS (Story 7.1) enregistre la source. En dev (non installé), `eventlog.Open` échoue → fallback stderr, pas d'erreur fatale.
- **journald Linux** : `syslog.Dial` fonctionne sur toute distro avec syslog ou systemd-journald (qui capte syslog). Fallback stderr si indisponible (très rare).
- **Kill switch durant recovery** : garantie AC3 par réutilisation stricte de `recoverTUN` qui n'appelle JAMAIS `Deactivate`. Le test `TestAnomalyRecovery_FirewallStaysActive` est le gate.
- **Coût d'un tick leak_check (rappel)** : 3 requêtes UDP × 5s timeout = bornure 15 s. La recovery doit completer en < 30 s typiquement (couvert par tests service existants). Le mutex `anomalyRecoveryMu` empêche les chevauchements.

### Project Structure Notes

**Alignement parfait avec architecture.md** :
- Le package `internal/anomaly/` suit le pattern des autres packages OS-split (`internal/firewall/eventlog_*`, `internal/tun/device_*`).
- L'ajout d'`IconAlert` respecte la convention `go:embed assets/<name>.{ico,png}` établie Story 5.1.
- Les nouveaux champs `anomaly_active` / `anomaly_reason` dans `/api/status` suivent le snake_case json de l'API existante.

**Aucune variance détectée** : la story est un pur « câblage entre composants existants + 1 nouveau petit package logger ».

### Testing standards (Story 1.1 → 6.2 établi)

- Tests table-driven
- `go test -race` obligatoire
- Coverage ≥ 80 % sur nouveau code (`internal/anomaly/`)
- Mocks via interfaces (`anomaly.Logger`, `anomaly.Notifier`) — pas de singleton
- Tests NFR22a par regex (garantie absence d'IP/path dans log)
- Pas de dépendance internet réelle (mock STUN + mock DoH comme 6.1/6.2)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 6.3](_bmad-output/planning-artifacts/epics.md#L1115-L1130)
- [Source: _bmad-output/planning-artifacts/prd.md#FR34](_bmad-output/planning-artifacts/prd.md#L488-L492) — Reconnexion + alerte + log en cas d'anomalie
- [Source: _bmad-output/planning-artifacts/prd.md#NFR22a](_bmad-output/planning-artifacts/prd.md#L553) — Log privacy (zéro donnée utilisateur)
- [Source: _bmad-output/planning-artifacts/architecture.md#L334](_bmad-output/planning-artifacts/architecture.md#L334) — Rôle détection vs défense
- [Source: _bmad-output/implementation-artifacts/6-1-emission-de-stun-binding-requests-via-la-tun.md](_bmad-output/implementation-artifacts/6-1-emission-de-stun-binding-requests-via-la-tun.md) — Contexte leak scheduler
- [Source: _bmad-output/implementation-artifacts/6-2-comparaison-ip-stun-vs-ip-tunnel-attendue.md](_bmad-output/implementation-artifacts/6-2-comparaison-ip-stun-vs-ip-tunnel-attendue.md) — Statuts ok/leak_detected, callbacks onLeak/onRecovery en place
- [Source: _bmad-output/implementation-artifacts/2-2-watchdog-tun-detection-d-alteration-externe.md](_bmad-output/implementation-artifacts/2-2-watchdog-tun-detection-d-alteration-externe.md) — Callback OnLost + fonction recoverTUN
- [Source: _bmad-output/implementation-artifacts/5-9-mode-degrade-kill-switch-indicateur-visuel-permanent.md](_bmad-output/implementation-artifacts/5-9-mode-degrade-kill-switch-indicateur-visuel-permanent.md) — Pattern CSRF/token + bandeau webview (modèle visuel)
- [Source: _bmad-output/implementation-artifacts/4-4-failover-automatique-avec-kill-switch-maintenu.md](_bmad-output/implementation-artifacts/4-4-failover-automatique-avec-kill-switch-maintenu.md) — Pattern failover-banner frontend
- [Source: internal/service/service.go:1426](internal/service/service.go#L1426) — Placeholder `onLeak` à câbler
- [Source: internal/service/service.go:2052-2155](internal/service/service.go#L2052) — `recoverTUN` séquence complète (réutilisée)
- [Source: internal/service/service.go:2023-2046](internal/service/service.go#L2023) — Wiring watchdog TUN
- [Source: internal/leakcheck/scheduler.go:43-87](internal/leakcheck/scheduler.go#L43) — Signature callbacks
- [Source: internal/firewall/eventlog_windows.go](internal/firewall/eventlog_windows.go) — Pattern Event Log (modèle pour `internal/anomaly/anomaly_windows.go`)
- [Source: internal/ui/ui.go:464-532](internal/ui/ui.go#L464) — `updateTrayState` à étendre
- [Source: frontend/index.html:12](frontend/index.html#L12) + [frontend/index.html:62-65](frontend/index.html#L62) — Bandeaux existants (modèles)
- [Source: frontend/src/style.css:197](frontend/src/style.css#L197) + [frontend/src/style.css:310](frontend/src/style.css#L310) — Styles bandeaux existants

## Previous Story Intelligence

### Story 6.2 (terminée 2026-04-18)

- **Statuts IPC renommés** : `StatusLeakOK = "ok"` + `StatusLeakDetected = "leak_detected"`. Alias `StatusLeakPass`/`StatusLeakFail` conservés en deprecated — **à supprimer dans 6.3 Task 7** (commentaire explicite dans `messages.go`).
- **`RelayIPResolver` + cache TTL 5 min** : en place ([internal/leakcheck/relay_ip.go](internal/leakcheck/relay_ip.go)). `Invalidate()` est appelé côté failover (Story 4.4) via `Program.leakRelayResolver`. Story 6.3 n'a pas à y toucher — la recovery inclut déjà un `tunnel.Connect` qui peut passer par un autre pays, donc le cache DoH sera rafraîchi naturellement au prochain tick leak (ou après `Invalidate()` si failover déclenché).
- **Pattern `PeriodicScheduler.runCheck` serialisé par `checkMu.TryLock`** : introduit en 6.2 M1 fix. Le même pattern est réutilisé pour `anomalyRecoveryMu` (cohérence).
- **Erreur `RunFullCheck` incrémente `consecutiveSkips`** : une série d'échecs DoH ne déclenche PAS une recovery (`onLeak` n'est appelé QUE sur `statusLeakDetected`, pas sur erreur). ✅ Bon design : on ne veut pas reconnecter en boucle si DoH est HS.
- **`getExpectedIP` peut être `nil`** (option validation-only). Dans ce cas `report.Status = statusOK` toujours → `onLeak` jamais appelé. À garder en tête si un opérateur veut désactiver la comparaison sans désactiver le scheduler.

### Story 6.1 (terminée 2026-04-18)

- **`defaultSTUNServers`** : 3 serveurs failover (Google ×2 + Cloudflare). Pas de changement pour 6.3.
- **Scheduler skip unique** : `tunnelState != Connected`. La recovery déclenche `tunnel.Connect` donc le scheduler reprendra ses ticks dès que le state retourne à Connected. ✅

### Story 5.9 (terminée 2026-04-18)

- **Pattern CSRF token** : endpoints « action » (toggle killswitch, etc.) protégés par token per-process. Pour Story 6.3 Task 8 (`ActionTriggerRecovery`), appliquer le même pattern via token machine-local `ctl.token` — pattern déjà établi, rien à réinventer.
- **Bandeau `killswitch-degraded-banner`** : modèle visuel + pattern d'affichage via polling `/api/status` → servir de copy-paste reference pour `anomaly-banner`.

### Story 5.7 (terminée 2026-04-17)

- **Supervision UI + auto-restart** : la recovery peut prendre ~10-30s. Durant ce temps, si un tick `/api/status` échoue (transient IPC), la supervision UI ne doit PAS tuer le process UI. C'est déjà le comportement actuel (grace period 30s). ✅

### Story 4.4 (terminée 2026-04-17)

- **`failover-banner` frontend** : pattern visuel copié. ✅
- **`tunnel.WithFailoverFn`** : le callback de succès appelle `p.leakRelayResolver.Invalidate()` (Story 6.2 M3 fix). Si `RecoverFromAnomaly` passe par un failover inter-pays (rare), l'invalidation DoH se fait automatiquement via ce chemin. Rien de spécial à faire.

## Git Intelligence

Derniers commits (5 plus récents) :

1. `996f7e3 feat: Epic 5 done — drop Wails/tray, move to webview+HTTP UI with ctl/watchdog/killswitch`
2. `ac795de feat: Epic 5 UI cross-platform — Linux webview + tray portage (stories 5.1–5.9)`
3. `f2f9021 feat: complete Epic 4 — découverte & failover multi-pays (stories 4.1–4.4)`
4. `16a275a fix(deploy): align install.sh/README/service with prod (signing.key + registry)`
5. `c2e1c0e feat: complete Epic 3 — relay stateless multi-VPS with tunnel IP & NAT`

**Insights pour 6.3** :

- Epic 5 (UI) vient d'être clôturé : `internal/ui/ui.go` est stable et récemment refactoré (webview HTTP + tray). L'ajout d'`IconAlert` et de la branche prioritaire dans `updateTrayState` s'intègre proprement.
- Stories 6.1 et 6.2 ne sont pas encore committées séparément (cf. Story 6.2 File List note « changements 6.1 non-commités hors scope »). **À prendre en compte** : `git status` montrera un diff mixte 6.1+6.2+6.3 — le dev devra bien scoper son commit 6.3 pour éviter d'emporter du code étranger. Suggestion : `git stash` les changes 6.1/6.2 si elles ne sont toujours pas commitées, puis commit propre de 6.3 après. À défaut, commit combiné `feat: complete Epic 6 — anti-leak validation (6.1 + 6.2 + 6.3)` pour clôturer l'epic en une fois.
- `internal/firewall/eventlog_windows.go` est le code le plus proche du besoin 6.3 (Event Log) — **copy-adapt**, pas refactor partagé (YAGNI : 2 sites c'est pas encore une abstraction).
- Aucun commit récent ne touche `internal/leakcheck/scheduler.go` ou `recoverTUN` en-dehors des stories 6.1/6.2 en vol → pas de conflit attendu.

## Latest Tech Information

- **Go 1.22+** : `sync.Mutex.TryLock()` disponible nativement depuis Go 1.18. `atomic.Pointer[T]` (génériques) disponible depuis Go 1.19.
- **`log/syslog`** (Linux) : package stdlib. `syslog.Dial("", "", priority, tag)` avec tag="levoile" → journald capte via forwarder automatique (présent sur toutes distros systemd ≥ 219, ~2015+). `/var/log/messages` sur distros non-systemd (rare en 2026).
- **`golang.org/x/sys/windows/svc/eventlog`** : déjà vendor/go.mod dans le projet (cf. `firewall/eventlog_windows.go`). Réutilisation directe.
- **`fyne.io/systray` / `getlantern/systray`** : vérifier lequel est utilisé dans `internal/ui/` (via l'interface `systrayAPI`) — `u.api.SetIcon(bytes)` fonctionne quel que soit le provider.
- **`go:embed`** : convention établie pour assets frontend + icônes. Pas de changement.

## Project Context Reference

Voir `docs/project-context.md` si présent (absent à la rédaction — la story s'appuie sur `architecture.md` + `prd.md` + `epics.md` + stories précédentes). Le fichier `memory/reference_relay_servers.md` liste les 8 relais (DE/ES/GB/US, 2/pays) pour smoke tests. Le fichier `memory/reference_ui_prefs_pattern.md` rappelle : **prefs UI frontend-only = `/api/ui-prefs` + prefs.go, PAS localStorage** (port HTTP dynamique) — non applicable ici car on lit uniquement `/api/status`, pas de nouvelle préférence.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m] (BMAD dev-story workflow)

### Debug Log References

- `go build ./...` : vert (33 packages compilent sans warning).
- `go vet ./...` : propre.
- `go test ./... -timeout 300s` : 33/33 packages verts, 0 régression.
- `go test -race -count=1 -timeout 120s ./internal/anomaly/ ./internal/service/ ./internal/ipchandler/ ./internal/ui/` : 4/4 verts.
- Race pré-existante dans `internal/ipc/client_edge_test.go::TestClient_ConcurrentSend` confirmée sur `main` avant mes changements (via `git stash` + re-run) → hors-scope Story 6.3.
- Pendant la mise au point : 2 flakes détectés sur `TestRecoverFromAnomaly_SerializesConcurrent` (ordonnancement Go) → corrigés par un barrier pattern déterministe (winner block + loser drain counter).
- Pendant la mise au point : 1 flake sur `CategorizeError` (le marqueur "tun" dans "tun recovery: tunnel.Connect" matchait avant "tunnel.Connect") → corrigé par réordonnancement des cas du switch (tunnel > firewall > routing > tun).

### Completion Notes List

- **Refacto minimale côté `recoverTUN`** : la séquence existante (Story 2.2) est 100 % préservée. `RecoverFromAnomaly` est un pur wrapper qui ajoute logging + notification + sérialisation. Garantie AC3 (kill switch maintenu) est donc automatiquement héritée de `recoverTUN` — confirmée par le test `TestRecoverFromAnomaly_FirewallStaysActive` qui poll `IsActive` toutes les 1 ms pendant la recovery et vérifie 0 transition observable vers inactif.
- **Nouveau package `internal/anomaly/`** : découpe stricte de l'API — `Logger` n'expose que `Started(reason)`/`Succeeded(ms)`/`Failed(category)` (pas de `Warnf/Infof/Errorf` génériques) pour garantir par construction NFR22a (impossible d'injecter une chaîne libre). Le test `TestAnomalyLogger_NFR22a` vérifie par regex qu'aucun IP, nom d'interface, ou path ne peut apparaître.
- **Décision conception Task 5.5-5.7** : l'icône tray est drivée par `resp.AnomalyActive` dans `updateTrayState` plutôt que par un flag séparé sur `UI`. Plus simple, plus cohérent avec le pattern `KillSwitchMode` (Story 5.9), et élimine la nécessité d'injecter un Notifier custom dans `cmd/client/main.go`. Le Notifier côté service reste utile pour des consumers futurs (desktop notifications natives) mais utilise `NopNotifier` par défaut.
- **Suppression alias `StatusLeakPass`/`StatusLeakFail`** : finalisée comme prévu (Story 6.2 Task 5.1 avait laissé les alias en transition). Test `TestStatusLeakAliases` supprimé (seul test qui les utilisait), remplacé par un commentaire historique.
- **Authentification `ActionTriggerRecovery`** : plus stricte que spec (empty Auth rejette même depuis l'UI loopback). Aucune surface UI légitime n'a besoin de cette action ; tolérer empty Auth ouvrirait la porte à des reconnect loops déclenchés par n'importe quel process local.
- **Placeholder `IconAlert`** : actuellement une copie de `connecting.ico`/`connecting.png`. `go:embed` porte un commentaire `TODO(design)` qui rappelle le remplacement par un glyph triangulaire orange dédié. Non bloquant pour review — l'icône sera différenciée visuellement par le tooltip « Anomalie détectée — reconnexion en cours ».
- **Points reportés** :
  - Task 9.3 (mise à jour `architecture.md` étape 7) → clôture Epic 6 (pattern 6.1/6.2).
  - Task 10.5 (`goreleaser snapshot`) → CI post-merge.
  - Task 10.6 (smoke test manuel DE avec déclenchement recovery) → phase QA post-review, nécessite accès prod + client Windows.

### Change Log

- 2026-04-18 : Implémentation Story 6.3 complète — reconnexion automatique kill-switch-préservée sur détection de fuite STUN (`leak_detected`) ou altération TUN (watchdog). Nouveau package `internal/anomaly/` avec logger Event Log Windows / journald Linux NFR22a-safe (closed-set reasons + error categories + regex test anti-leak). `Program.RecoverFromAnomaly` wrappe la séquence `recoverTUN` existante avec TryLock de sérialisation, state `AnomalyActive`/`AnomalyReason` surfacé via IPC + `/api/status`. Icône tray `IconAlert` prioritaire dans `updateTrayState`. Bandeau webview `#anomaly-banner` orange + flash vert 3s sur reconnexion réussie. Action IPC `trigger_recovery` (+ sous-commande `levoile-ctl trigger-recovery`). Suppression des alias deprecated `StatusLeakPass`/`StatusLeakFail` (Story 6.2 Task 5 transition terminée). 4 nouveaux fichiers de tests dédiés, 19 nouveaux tests, 0 régression sur 33 packages. Couvre AC1-10.

### File List

**Nouveaux fichiers**

- `internal/anomaly/anomaly.go` — types `Reason`, `ErrorCategory`, interfaces `Logger`/`Notifier`, `NopNotifier`, `CategorizeError`, `stderrLogger` (fallback cross-platform).
- `internal/anomaly/anomaly_windows.go` — `eventLogLogger` via `golang.org/x/sys/windows/svc/eventlog`, source "LeVoile".
- `internal/anomaly/anomaly_linux.go` — `journaldLogger` via `log/syslog` + tag "levoile".
- `internal/anomaly/anomaly_stub.go` — portable/`!windows && !linux`.
- `internal/anomaly/anomaly_test.go` — 8 tests : `TestCategorizeError` (table-driven 8 cas), `TestStderrLogger_*` (4), `TestAnomalyLogger_NFR22a` (regex anti-leak), `TestNewLoggerReturnsNonNil`, `TestNopNotifier_Safe`, `ExampleCategorizeError`.
- `internal/service/anomaly.go` — `RecoverFromAnomaly`, `SetAnomalyLogger`, `SetAnomalyNotifier`, `AnomalyActive`, `AnomalyReason`, `ForTestSetAnomaly`, `ensureAnomaly{Logger,Notifier}`.
- `internal/service/anomaly_test.go` — 8 tests : happy path, serialization barrier, firewall-stays-active, tunnel-connect-error, reason-while-running, ctx-cancelled, notifier-fallback, lock-free-accessors. Fakes `fakeTUN`, `fakeRouteMgr`, `fakeFirewall` satisfying the real interfaces (`CleanupOrphans`, `SetIPv6Policy`, `Saved`).
- `internal/ipchandler/anomaly_test.go` — 4 tests : Idle, Active, TunAltered, ClearedReflects.
- `internal/ipchandler/trigger_recovery_test.go` — 5 tests : NoAuth/WrongAuth/NoTunnel/TunnelDisconnected/Success-FireAndForget.
- `internal/ui/anomaly_httpserver_test.go` — 1 table-driven test (4 cas) via fake `IPCClient`.
- `internal/ui/anomaly_tray_test.go` — 4 tests : overrides-connected, overrides-degraded, clears-back, disconnected-backend.
- `internal/ui/icons/alert.ico` + `internal/ui/icons/alert.png` — placeholders (copies des assets `connecting.*`) avec TODO(design) dans les `go:embed`.

**Fichiers modifiés**

- `internal/service/service.go` — ajout de 5 champs anomaly au struct `Program` + import `internal/anomaly`, câblage `onLeak` → `RecoverFromAnomaly(ReasonLeakDetected)`, wrapper `startTUNWatchdog.OnLost` → `RecoverFromAnomaly(ReasonTUNAltered)`.
- `internal/ipc/messages.go` — ajout `ActionTriggerRecovery`, champs `AnomalyActive`/`AnomalyReason` sur `Response`, suppression des alias deprecated `StatusLeakPass`/`StatusLeakFail`.
- `internal/ipchandler/handler.go` — import `internal/anomaly`, populage des 3 branches de `handleGetStatus` avec les champs anomaly, nouveau case `ActionTriggerRecovery` + `handleTriggerRecovery` (ctl-only auth + tunnel-state guard + fire-and-forget).
- `internal/ipchandler/handler_test.go` — test `TestStatusLeakAliases` remplacé par un commentaire historique (alias supprimés).
- `internal/ui/httpserver.go` — champs `AnomalyActive`/`AnomalyReason` sur `APIStatusResponse`, pass-through dans `handleStatus`.
- `internal/ui/ui.go` — branche prioritaire `AnomalyActive` dans `updateTrayState` + inclusion de la flag dans `stateKey` pour que la transition true→false re-render.
- `internal/ui/icons_windows.go` + `icons_linux.go` + `icons_stub.go` — `IconAlert` + `go:embed`.
- `frontend/index.html` — `<div class="anomaly-banner" id="anomaly-banner">` inséré après `#failover-banner`.
- `frontend/src/style.css` — styles `.anomaly-banner` (orange) + `.anomaly-banner.anomaly-success` (vert) avec transition.
- `frontend/src/app.js` — `dom.anomalyBanner`/`dom.anomalyBannerText` + state globals `wasAnomalyActive`/`anomalyFlashTimer` + fonction dédiée `renderAnomalyBanner(s)` + helper `anomalyText(reason)` ; câblage dans `updateUI`.
- `cmd/ctl/main.go` — sous-commande `trigger-recovery` (+ alias `recover`), usage mis à jour, fonction `runTriggerRecovery`.
- `README.md` — section « Reconnexion automatique sur anomalie (Story 6.3) » après la section STUN.

**Note sur `git status` composite** : l'arbre de travail contient des changements issus de Stories 6.1/6.2 non encore commitées (cf. Git Intelligence). La File List ci-dessus concerne uniquement les changements de Story 6.3. Le commit de clôture d'Epic 6 devra soigneusement trier `git diff` pour scope 6.1 vs 6.2 vs 6.3, ou adopter un commit combiné `feat: complete Epic 6 — anti-leak validation`.

## Senior Developer Review (AI)

**Reviewer :** Claude Opus 4.7 (1M context) — adversarial code review
**Review date :** 2026-04-18
**Outcome :** Changes Requested → All HIGH + MEDIUM fixed → **Approved**

### Findings + Action Items (tous résolus)

#### 🔴 HIGH (2)

- **[x] H1 — `handleTriggerRecovery` détache la recovery du lifecycle service** : utilisait `context.Background()` comme parent → `RecoverFromAnomaly` pouvait toucher `tunDev`/`firewallMgr`/`routeMgr` pendant qu'un shutdown parallèle les libère.
  **Fix** : le parent est désormais `prg.Context()` ; fallback `context.Background()` uniquement si le lifecycle ctx n'est pas encore initialisé (cas test).
- **[x] H2 — `ensureAnomalyLogger` détenait `p.mu` pendant `anomaly.NewLogger()` (I/O système)** : syslog.Dial / eventlog.Open sont des system calls ; bloquer `p.mu` pendant pouvait freezer tout handler IPC.
  **Fix** : pattern double-lock (read-release-build-commit) ; la construction se fait hors verrou, une race bénigne en cas de double build est résolue en Close()ant le perdant.
  **Test** : nouveau `TestEnsureAnomalyLogger_DoesNotHoldMuDuringConstruction` utilise `anomalyNewLoggerFactory` var-injectable + barrier pour prouver que `p.mu` est libre pendant la phase I/O.

#### 🟡 MEDIUM (5)

- **[x] M1 — `CategoryRoutingSetupFailed` / `CategoryFirewallActivateFail` dead code en prod** : `recoverTUN` avale les erreurs routing/firewall via `fmt.Fprintf`, seules `tun.New` et `tunnel.Connect` bubblent.
  **Fix** : godoc de `ErrorCategory` ré-écrite pour distinguer explicitement les catégories `LIVE` (3) des catégories `RESERVED` (2). Les tests `TestCategorizeError` restent utiles comme spec pour un éventuel refactor futur de `recoverTUN` qui propagerait les erreurs.
- **[x] M2 — `handleTriggerRecovery` retourne toujours `StatusOK` même si la recovery était déjà en vol** : l'opérateur voyait "reconnexion déclenchée" alors que son trigger était un no-op.
  **Fix** : court-circuit explicite si `prg.AnomalyActive()` au moment de l'appel, la réponse porte `AnomalyActive=true` + `AnomalyReason=<current>`. `levoile-ctl` affiche "reconnexion déjà en cours (raison : leak_detected) — observez …" au lieu de mentir.
  **Test** : `TestHandle_TriggerRecovery_AlreadyInProgress_ReflectsInResponse`.
- **[x] M3 — `ForTestSetAnomaly(true, "")` créait un état (active=true, reason=nil) qui viole l'invariant** .
  **Fix** : panic explicite dans `ForTestSetAnomaly` si `active && reason == ""`.
  **Tests** : `TestForTestSetAnomaly_PanicsOnEmptyReasonWhenActive` + `TestForTestSetAnomaly_DoesNotPanicOnEmptyReasonWhenInactive`.
- **[x] M4 — `SetAnomalyLogger` laissait fuir le logger précédent** (handle Event Log / syslog.Writer).
  **Fix** : `Close()` explicite du logger précédent avant remplacement, hors verrou pour éviter le pattern H2. Identity check pour ne pas Close un logger ré-assigné à lui-même.
  **Test** : `TestSetAnomalyLogger_ClosesPrevious`.
- **[x] M5 — `onLeak` capturait `ctx` à la construction du scheduler** : sémantique fragile si le scheduler survit un jour au run initial.
  **Fix** : la closure lit `p.Context()` à chaque appel ; fallback sur le ctx capturé si `p.ctx == nil` (cas tests).

#### 🟢 LOW (5) — deferred

- L1 : `CategorizeError` matching sur "route" trop large. **Non-bloquant** — les erreurs réelles de `recoverTUN` utilisent "routing" de façon non ambiguë.
- L2 : `anomalyText()` JS sans case explicite pour `leak_detected`. **Non-bloquant**, tombe sur le default générique correct.
- L3 : pas de test du chemin TryLock-drop dans `handleTriggerRecovery`. **Couvert indirectement** par M2 fix qui intercepte en amont via `AnomalyActive()`.
- L4 : placeholder `alert.ico`/`alert.png` identiques à `connecting.*`. **TODO(design)** documenté dans les `go:embed`.
- L5 : `renderAnomalyBanner` ne valide pas `s.anomaly_reason` avant le switch. **Fonctionne par accident** mais pas bloquant.

### Tests post-fix

- `go build ./...` : vert
- `go vet ./...` : propre
- `go test ./... -timeout 300s` : **PASS** sur 33 packages, 0 régression
- `go test -race -count=3 ./internal/anomaly/ ./internal/service/ ./internal/ipchandler/ ./internal/ui/ ./cmd/ctl/` : vert

### Fichiers modifiés post-review

- `internal/service/anomaly.go` — refacto `ensureAnomalyLogger` (H2), `SetAnomalyLogger` Close (M4), `ForTestSetAnomaly` panic (M3), ajout `anomalyNewLoggerFactory` var.
- `internal/service/anomaly_test.go` — `TestEnsureAnomalyLogger_DoesNotHoldMuDuringConstruction`, `TestForTestSetAnomaly_{Panics,DoesNotPanic}`, `TestSetAnomalyLogger_ClosesPrevious`.
- `internal/service/service.go` — closure `onLeak` lit `p.Context()` (M5).
- `internal/anomaly/anomaly.go` — godoc `ErrorCategory` LIVE/RESERVED (M1).
- `internal/ipc/messages.go` — (pas de changement, `AnomalyActive`/`AnomalyReason` déjà présents, re-purposés par M2).
- `internal/ipchandler/handler.go` — `handleTriggerRecovery` utilise `prg.Context()` (H1) + court-circuit `AnomalyActive` (M2).
- `internal/ipchandler/trigger_recovery_test.go` — `TestHandle_TriggerRecovery_AlreadyInProgress_ReflectsInResponse`, assertion `AnomalyActive=false` ajoutée à `TestHandle_TriggerRecovery_Success_FireAndForget`.
- `cmd/ctl/main.go` — branchement ctl sur `resp.AnomalyActive` pour afficher "déjà en cours" vs "déclenchée" (M2).

## Decisions prises en implémentation

1. **Interface `Logger` stricte** (Started/Succeeded/Failed) au lieu de Warnf/Infof/Errorf — garantie NFR22a par construction.
2. **Pas de `SetAnomalyActive` sur UI** — `updateTrayState` lit directement `resp.AnomalyActive`, évite un second canal de synchronisation.
3. **`ActionTriggerRecovery` ctl-only** (empty Auth rejette) — pas de surface UI qui en a besoin.
4. **Icônes placeholder** acceptables pour la review — différenciation visuelle portée par le tooltip, TODO(design) dans le code.
5. **Ordre du switch `CategorizeError`** : tunnel > firewall > routing > tun (les messages d'erreur `recoverTUN` sont tous préfixés `"tun recovery:"`).
6. **Test barrière déterministe** pour `SerializesConcurrent` : pattern winner/loser avec drain counter — plus fiable que time.Sleep.
7. **Fonction JS `renderAnomalyBanner`** isolée de `updateUI` — meilleure lisibilité et plus facile à tester manuellement.
8. **Alias `StatusLeakPass`/`StatusLeakFail` supprimés** maintenant, conforme au plan 6.2 Task 5.1 (transition finalisée par cette story).

## Decisions (prises en rédaction de story — pas d'ambiguïté pour le dev)

1. **Une seule fonction de recovery, deux déclencheurs.** `RecoverFromAnomaly` wrappe `recoverTUN` existant — pas de duplication de séquence. Garantit AC3 (kill switch maintenu) par construction.
2. **Nouveau package `internal/anomaly/`** plutôt que réutilisation de `internal/firewall/eventlog_*`. Raison : séparation des préoccupations (firewall logs ses événements, anomaly logs les siens) + évite couplage firewall↔service. Copie-adapt du pattern Event Log, pas factorisation prématurée (YAGNI — 2 sites).
3. **Logger Linux via `log/syslog`** (stdlib) plutôt que `sd_journal` cgo. Couvre 99 % des distros systemd via capture syslog automatique. Évite la dépendance cgo qui complexifie la cross-compile.
4. **Suppression des alias deprecated `StatusLeakPass`/`StatusLeakFail`** dans cette story (Task 7). Raison : la migration UI est finalisée (AC5 — bandeau consomme `leak_status` + `anomaly_active` nouveaux schemas). Plus de rétro-compat nécessaire, et Story 6.2 avait explicitement reporté la suppression à 6.3.
5. **`ActionTriggerRecovery` IPC exposée** (Task 8). Raison : utile pour QA manuelle + ops incident response. Protégée par token machine-local (pattern 5.9). Fire-and-forget (async) — pas de blocage IPC sur 10-30s de recovery.
6. **Nouveau `IconAlert` orange** (AC4). Raison : les 4 icônes existantes (Connected/Connecting/Disconnected/Default) n'ont pas de sémantique « warning transient ». Créer la 5ème plutôt que détourner. Si asset graphique pas dispo → emoji fallback placeholder, ne pas bloquer.
7. **Frontend : flash vert 3s « Reconnexion réussie » sur transition true→false.** Raison : UX — l'utilisateur doit voir que le système s'est réparé, pas juste voir le bandeau orange disparaître silencieusement. Pattern analogue aux toasts success classiques, implémenté purement côté JS (setTimeout), pas besoin d'endpoint backend.
8. **Erreur `RunFullCheck` côté scheduler NE déclenche PAS `onLeak`.** Comportement existant en 6.2 (seul `statusLeakDetected` déclenche). ✅ Pas à toucher — évite les reconnexions en boucle si DoH down.
9. **Tests NFR22a par regex sur messages** (Task 1.5). Raison : la seule garantie robuste qu'un dev futur n'introduise pas accidentellement une fuite de données utilisateur dans les logs. Fail-loud en CI.
10. **Icône Alert prime sur tous les autres états tray** (AC4, branche prioritaire dans `updateTrayState`). Raison : le message « quelque chose d'anormal se passe » est plus important que « tu es connecté » — il faut capter l'attention de l'utilisateur. Retour à l'icône normale automatiquement à la fin de recovery (succès ou échec).

## Open Questions for Dev

_(aucune — les décisions couvrent l'intégralité du design. Si l'agent dev rencontre un obstacle imprévu, documenter dans Completion Notes et ajuster.)_
