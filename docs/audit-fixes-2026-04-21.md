# Audit fixes — 2026-04-21 (client / UI, hors relais)

Suite à l'audit approfondi du code Le Voile côté client / UI / service
(excluant le code server-side des relais), quatre fix majeurs ont été
livrés. Ce document récapitule le problème, la solution, les fichiers
touchés et les impacts de compatibilité.

Validation globale : `go build ./...`, `go vet ./...`, `go test ./...`
verts.

---

## Fix #1 — Coordination de shutdown des goroutines long-running

**Problème constaté**

Plusieurs composants étaient lancés via un pattern
`go func() { component.Start(ctx) }()` sans aucune synchronisation
permettant à `shutdown()` de confirmer la fin de la goroutine avant le
`os.Exit(0)`. Cas critique : `internal/updater.Updater.Start(ctx)` qui
est une vraie boucle long-running et n'a pas de `.Stop()` dédié.
Conséquence : `shutdown()` pouvait retourner alors que l'updater était
encore en vol (peu grave en pratique mais rend le raisonnement sur
l'ordre des libérations de ressources impossible).

**Solution**

- Ajout d'un champ `bgWG sync.WaitGroup` au `Program`.
- Les 5 goroutines lanceuses (ipcStart, leakScheduler, blocklistMgr,
  updater, uiWatchdog, tunWatchdog) sont maintenant encadrées par
  `p.bgWG.Add(1)` / `defer p.bgWG.Done()`.
- `shutdown()` attend `bgWG.Wait()` avec un timeout borné
  (`bgWaitTimeout = 1500ms`) ; au-delà, un log stderr acte la situation
  et on sort quand même — on ne bloque jamais le SCM plus longtemps
  que nécessaire.
- L'erreur de l'updater est désormais loguée (hors `context.Canceled` /
  `context.DeadlineExceeded` qui sont la terminaison normale).

**Fichiers touchés**

- [internal/service/service.go](../internal/service/service.go)

**Compatibilité** : transparent, pas de changement d'API publique.

---

## Fix #2 — Timeouts dédiés sur chaque étape de shutdown

**Problème constaté**

La séquence de `shutdown()` utilisait `context.Background()` sans
timeout pour cinq étapes critiques : kill switch deactivate, browser
policies restore, DNS restore, watchdog verify, firewall deactivate.
Un seul hang (WFP engine bloqué, registry lent, resolver système figé)
swallowait tout le budget `shutdownTimeout` (10 s) et laissait les
étapes suivantes non exécutées → règles firewall / DNS orphelines à la
sortie brutale par SCM.

**Solution**

Chaque étape passe désormais par un `context.WithTimeout` dédié
(`shutdownStepTimeout = 2 * time.Second`). Sizing : 5 × 2 s = 10 s ≤
`shutdownTimeout`, donc même le pire cas de timeouts consécutifs
reste dans le budget SCM. Une étape qui hang ne bloque plus les
suivantes, et chaque restore de ressource système garde sa chance
de s'exécuter.

**Fichiers touchés**

- [internal/service/service.go](../internal/service/service.go) —
  `shutdown()` lignes ~1694–1763

**Compatibilité** : transparent. Nouvelles constantes internes
`shutdownStepTimeout`, `bgWaitTimeout`.

---

## Fix #3 — IPC strict-auth par défaut + UI envoie le token ctlauth

**Problème constaté**

Le gate d'authentification IPC était **opt-in** : par défaut, toute
action mutante avec `req.Auth == ""` était acceptée. Le flag
`LEVOILE_IPC_STRICT_AUTH=1` permettait de durcir, mais personne ne le
positionnait en production. Conséquence documentée dans SECURITY.md §C3 :
n'importe quel processus du même user OS pouvait piloter le service
(allumer / éteindre kill-switch, disconnect, etc.) sans authentifier.

Le rollout bloquait sur une pré-condition qui n'avait jamais été faite :
la UI n'envoyait pas de token ctl. Fix pré-requis côté UI avant de
pouvoir flipper le défaut.

**Solution (deux parties)**

### 3a. UI charge le token ctlauth et l'envoie sur chaque IPC

- [cmd/ui/main.go](../cmd/ui/main.go) : charge le token via
  `ctlauth.Load(ctlauth.DefaultPath())` au boot. Best-effort : si le
  fichier n'existe pas encore (service pas bootstrappé), l'UI démarre
  sans token et retombe dans le rejet strict — l'utilisateur relance
  l'UI une fois le service démarré.
- [internal/ui/ui.go](../internal/ui/ui.go) : `Config.AuthToken`
  ajouté ; `handleQuit` envoie maintenant `req.Auth = u.config.AuthToken`.
- [internal/ui/httpserver.go](../internal/ui/httpserver.go) :
  `NewHTTPServer(...)` prend un paramètre `authToken string` ;
  `sendIPC()` l'injecte dans chaque `ipc.Request{}`. Centralisé en
  un seul endroit → tous les handlers `/api/*` mutants le propagent
  automatiquement.

### 3b. Flip du défaut strict-by-default

- [internal/ipchandler/handler.go](../internal/ipchandler/handler.go) :
  - `strictIPCAuthRequired()` remplacé par `legacyEmptyAuthAllowed()`.
  - Sémantique inversée : par défaut **strict** (rejette `Auth == ""`),
    `LEVOILE_IPC_LEGACY_AUTH=1` réactive le comportement pré-2026-04
    (pour hosts bloqués sur une vieille UI le temps d'un upgrade).
  - L'ancienne variable `LEVOILE_IPC_STRICT_AUTH` devient no-op (un
    résidu `=1` d'un ancien déploiement ne casse rien).
  - Le gate d'integrity a été **réordonné AVANT** le gate d'auth :
    une installation dont le config a été altéré renvoie maintenant
    `integrity_failed` (actionnable par l'UI → bannière recovery) au
    lieu d'un `auth_required` générique qui masquait la vraie cause.

### Tests

- [internal/ipchandler/testmain_test.go](../internal/ipchandler/testmain_test.go) :
  nouveau — `TestMain` positionne `LEVOILE_IPC_LEGACY_AUTH=1` pour que
  les tests hérités (qui envoyaient `Auth=""`) continuent de passer.
  Les tests qui valident explicitement le gate strict (`strict_auth_test.go`)
  scopent l'override avec `t.Setenv`.
- [internal/ipchandler/strict_auth_test.go](../internal/ipchandler/strict_auth_test.go) :
  renommé et refactoré — `TestLegacyAuth_Default_Strict`,
  `TestLegacyAuth_Opt_In`.
- [internal/ipchandler/killswitch_e2e_test.go](../internal/ipchandler/killswitch_e2e_test.go),
  [internal/ipchandler/trigger_recovery_test.go](../internal/ipchandler/trigger_recovery_test.go) :
  ajustés pour la nouvelle sémantique.

**Fichiers touchés** : voir ci-dessus + [SECURITY.md](../SECURITY.md) §C3
mis à jour.

**Compatibilité — breaking en production**

Scénario de rollout recommandé :
1. Déployer d'abord la nouvelle version du **service** (qui accepte
   `LEVOILE_IPC_LEGACY_AUTH=1` pour la transition).
2. Déployer ensuite la nouvelle UI (qui charge et envoie le token).
3. Si une flotte mixte existe temporairement, set
   `LEVOILE_IPC_LEGACY_AUTH=1` sur les hosts avec UI ancienne, puis le
   retirer après upgrade.

Concrètement pour un déploiement normal (service + UI packagés
ensemble), la mise à jour est transparente : le service écrit le token
via `ctlauth.LoadOrCreate` au boot, l'UI le lit au démarrage suivant.

---

## Fix #4 — Rollback post-install : classifier network-slow vs binary-bad

**Problème constaté**

Après une fresh install, le premier `tunnel.Connect()` était borné par
un timeout de 30 s. À l'expiration, **toute** erreur (y compris
`context.DeadlineExceeded` sur une ADSL lente ou un DNS flake)
déclenchait un rollback vers la version précédente + restart service.
Conséquence : les utilisateurs sur uplink lent se retrouvaient bloqués
sur l'ancienne version jusqu'au prochain cycle d'update qui re-testait
la nouvelle version.

**Solution**

- `rollbackTimeout` passé de 30 s à **90 s** pour tolérer un handshake
  QUIC sur ADSL.
- Nouveau classifier `shouldRollbackOnConnectErr(err)` :
  - `tunnel.ErrVerificationFailed`, `tunnel.ErrPinningFailed`,
    erreur inconnue → **rollback** (signal fort que le binaire est le
    fautif : crypto / cert pinning / comportement inattendu).
  - `context.DeadlineExceeded`, `context.Canceled`,
    `net.Error.Timeout()`, `*net.DNSError` → **pas de rollback**
    (transient, le reconnect loop normal doit retry).
- `run()` gate l'appel à `tryRollbackIfNeeded` par ce classifier.
  Sur une erreur transient post-install, une ligne stderr trace la
  décision (`skipping rollback`) pour diagnostic.

**Fichiers touchés**

- [internal/service/service.go](../internal/service/service.go) —
  constante `rollbackTimeout`, nouvelle fonction
  `shouldRollbackOnConnectErr`, gate dans `run()`.
- [internal/service/service_test.go](../internal/service/service_test.go) —
  nouveau test de table `TestShouldRollbackOnConnectErr` + mise à jour
  de `TestService_RollbackTimeout_Constant` (90 s).

**Compatibilité** : transparent pour les updates qui fonctionnent. Les
updates qui produisent un vrai binaire cassé continuent de rollback
comme avant.

---

## Faux positifs de l'audit initial (écartés après vérification)

Documentés ici pour ne pas ré-analyser ces points à la prochaine passe :

- **Race `webviewHidden` ↔ `handleTrayToggle`** : l'agent a signalé un
  signal show/hide perdu. Vérification ligne
  [internal/ui/ui.go:370-389](../internal/ui/ui.go) : `u.mu` protège
  bien `showCh`/`hideCh`, et `webviewHidden.Load()` ne détermine que
  la direction. Pas de perte de signal, au pire un flip cosmétique
  rattrapé par le polling.
- **WebView2 jamais recréée = bug de recovery** : c'est un workaround
  documenté d'un bug WebView2 où la seconde instance s'affiche blanche.
  Conforme à la mémoire `feedback_webview_lifecycle` — *pas* un
  problème.
- **`startTUNReader` erreur ignorée** : la fonction retourne `void` et
  vérifie `tunDev == nil` ; la goroutine interne gère les EOF via
  `close(out)`. Pas de path d'échec non capturé.
- **Loopback IPv6 Allow rule permissive** : rule WFP conditionnée sur
  `FWP_CONDITION_FLAG_IS_LOOPBACK` — pas exploitable pour sortir vers
  Internet.

---

## Faits saillants pour un futur auditeur

- Le gate IPC strict-by-default **NE clôt PAS** la limitation
  architecturale du même-user OS sur Windows (pipe DACL permissif,
  UI et malware indistinguables). SECURITY.md §C3 documente le plan
  long-terme (`ui.token` séparé, DPAPI-bound).
- Aucune commande CLI / UI / IPC n'a été introduite pour reset /
  bypasser un mécanisme de sécurité (cohérent avec la feedback
  `feedback_no_reset_endpoints` — la recovery reste exclusivement
  out-of-band : stop service, supprime config, restart).
- Les zones non couvertes par cette passe (à investiguer plus tard) :
  `internal/config/integrity.go` en profondeur, atomicité staging / swap
  dans `internal/updater/`, persistance `anomalyActive` si user
  disconnect pendant recovery, TOCTOU `internal/elevation/`.
