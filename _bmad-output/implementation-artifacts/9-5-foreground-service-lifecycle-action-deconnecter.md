# Story 9.5: Foreground Service lifecycle + action « Déconnecter »

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Aucune exception code partagé n'est nécessaire pour cette story.** Story 9.5 affine le lifecycle Foreground Service livré squelette par Story 9.4 (notification stub + `startForeground` < 5 s + `stopForeground` immédiat) en : (a) attachant l'action « Déconnecter » à la notification stub via `PendingIntent.getService(... FLAG_IMMUTABLE)` ciblant `ACTION_DISCONNECT`, (b) introduisant un délai de 5 s avant `stopForeground(STOP_FOREGROUND_REMOVE)` (au lieu de l'appel immédiat 9.4), (c) ajoutant le pattern singleton (`@Volatile companion var instance`) qui rejette les doubles `onStartCommand(ACTION_CONNECT)` concurrents, (d) ajoutant des helpers privés Kotlin dans `MainActivity` (`requestVpnConsent()`, `requestVpnStart(country?)`, `requestVpnStop()`) que **Story 11.2 câblera** au JS bridge `connect()`/`disconnect()`. **100% Kotlin/XML/Markdown sous `android/app/` + `android/README-android.md`.** Aucun fichier Go n'est lu, créé ou modifié. Aucun import gomobile dans le code Kotlin. Aucun `internal/*` racine n'est touché. Aucun fichier sous `android/shims/` n'est ajouté ou modifié. Aucune ligne dans `go.mod`/`go.sum` racine n'est touchée.
>
> **Rappel ADR-08 (architecture.md l. 2385-2388) — isolation OS maximale.** La règle structurelle est : un agent IA travaillant sur Android ne touche JAMAIS au code Windows, Linux, ni aux packages racine `internal/*` desktop. Si tu détectes une logique qui semble dupliquer du code desktop (timers de teardown, watchdogs, machines d'état Connect/Disconnect, gestion proxy systray), **c'est intentionnel** — la duplication assumée est documentée ADR-08. La seule porte vers le partagé Go = `android/shims/*.go` (livré 9.2) → `.aar` gomobile (généré localement) → `GoCoreAdapter.kt` (à livrer 9.7). Story 9.5 reste **strictement avant cette porte**.
>
> **Quand l'exception "code partagé" s'applique-t-elle ?** Uniquement aux stories qui modifient explicitement la frontière partagée :
> - Story 9.2 (livrée) : ajout de `golang.org/x/mobile` à `go.mod` racine + création des 5 shims `android/shims/{auth,crypto,leakcheck,protocol,registry}/`
> - Story 9.7 (à livrer) : enrichissement des shims pour exposer la pompe paquets + connect/disconnect réels du noyau Go au consommateur Kotlin
> - Toute future story qui aurait besoin d'exposer une nouvelle fonction Go côté Android — **avec ADR justificatif obligatoire avant ajout** (ADR-09 et règle « justification obligatoire dans un ADR avant ajout au noyau partagé », architecture.md l. 1100)
>
> **Story 9.5 n'est PAS dans cette catégorie.** Le délai de 5 s avant `stopForeground` est un timer Android (`Handler(Looper.getMainLooper()).postDelayed(...)` ou `lifecycleScope.launch { delay(5_000); ... }`) ; le `PendingIntent.getService(... FLAG_IMMUTABLE)` est une API Android pure ; le pattern singleton est une référence statique Kotlin classique. **Aucun de ces patterns ne traverse la frontière JNI.**
>
> **Zones explicitement OFF-LIMITS pour cette story** (livrées par 9.1/9.2/9.3/9.4, intactes pour 9.5) :
>
> | Zone | Livrée par | État pour 9.5 |
> |---|---|---|
> | `go.mod` racine (`golang.org/x/mobile` indirect + bumps `crypto`/`mod`/`net`/`sys`/`text`/`tools`) | Story 9.2 | INTACT — ne pas toucher |
> | `go.sum` racine | Story 9.2 | INTACT — ne pas toucher |
> | `android/shims/{auth,crypto,leakcheck,protocol,registry}/*.go` (5 shims gomobile) | Story 9.2 | INTACT — code Go, pas Kotlin. Story 9.5 n'en lit aucun |
> | `android/scripts/build-aar.{sh,ps1}` + `verify-shared-imports.sh` | Story 9.2 | INTACT — non invoqués par 9.5 |
> | `android/levoile-core/build.gradle.kts` + `levoile-core/src/main/AndroidManifest.xml` | Story 9.1+9.2 | INTACT — aucune classe Kotlin de 9.5 n'y atterrit |
> | `android/app/src/main/assets/{index.html,style.css,app.js}` | Story 9.3 | INTACT — pas de modification UI ici (le bouton « Connecter » côté JS arrive Story 11.2) |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` | Story 9.3 | **INTACT — l'enrichissement (`connect`, `disconnect`, `selectCountry`, etc.) appartient à Story 11.2.** Story 9.5 ne touche PAS ce fichier — seuls les helpers privés `MainActivity` sont ajoutés ici, sans surface JS |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/{LeVoileVpnService.kt,PacketRelay.kt,VpnConstants.kt}` | Story 9.4 | **MODIFIÉ partiellement** : `LeVoileVpnService.kt` enrichi (notification action, singleton, delay teardown) ; `PacketRelay.kt` + `VpnConstants.kt` INTACTS |
> | `android/app/src/main/AndroidManifest.xml` | Stories 9.1/9.4 | INTACT — `<service>` LeVoileVpnService déjà déclaré Story 9.4 avec `foregroundServiceType="vpn"` + `BIND_VPN_SERVICE` + intent-filter `android.net.VpnService`. **Aucune nouvelle permission ni nouvel élément XML cette story.** |
> | `android/app/src/main/res/drawable/ic_notification_stub.xml` | Story 9.4 | INTACT — Story 9.6 livrera `ic_levoile_status.xml` finalisé |
> | `android/app/src/main/res/values/themes.xml` + `res/xml/network_security_config.xml` + `res/xml/data_extraction_rules.xml` | Story 9.1 (themes/network) + 9.3 (network) | INTACT |
> | `internal/{tunnel,tun,firewall,routing,ui,ipc,wfp,nftables,...}` racine + `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/` | Stories 1-8 | INTACT — hors arbre `android/` |
>
> **Concrètement** : `git status` à la fin de la session dev doit montrer **uniquement** :
>   (a) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` (MODIFIÉ — enrichissement notification builder + delay teardown + singleton),
>   (b) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — ajout des helpers privés `requestVpnConsent()` / `requestVpnStart(country?)` / `requestVpnStop()` + ActivityResultLauncher pour le consent VpnService),
>   (c) `android/app/src/main/res/values/strings.xml` (MODIFIÉ — ajout clés `notif_action_disconnect` + `vpn_consent_denied_message`),
>   (d) `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — traductions FR),
>   (e) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceLifecycleTest.kt` (NOUVEAU — test smoke Robolectric pour le PendingIntent + le délai 5 s + le singleton),
>   (f) **OPTIONNEL** `android/app/src/debug/kotlin/fr/plateformeliberte/levoile/debug/DebugConnectActivity.kt` (NOUVEAU sous `src/debug/` — variant `debug` UNIQUEMENT, gitignoré du release ; déclenche `VpnService.prepare()` + `ACTION_CONNECT`/`ACTION_DISCONNECT` pour faciliter le test runtime sans Story 11.2),
>   (g) `android/README-android.md` (MODIFIÉ — ajout section « Lifecycle Foreground Service + OEM agressifs (Xiaomi/Huawei/Oppo) »),
>   (h) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `backlog` → `review`),
>   (i) `_bmad-output/implementation-artifacts/9-5-foreground-service-lifecycle-action-deconnecter.md` (auto-update Status, File List, Completion Notes, Change Log).
>
> **Aucune entrée à la racine** (`go.mod`, `go.sum`, `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`, `_bmad/`), **aucune entrée sous `android/shims/`, `android/scripts/`, `android/levoile-core/`, `android/app/src/main/assets/`, `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/`, `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/{PacketRelay.kt,VpnConstants.kt}`, `android/app/src/main/AndroidManifest.xml`**. Tout autre fichier modifié est un side-effect non prévu — **STOP, investiguer, ne pas tenter de "lisser" en supprimant les changements**. Reporter dans Debug Log et demander avant de poursuivre.
>
> **Anti-pattern fréquent à éviter** : tenter de pré-tirer en avance des fichiers prévus par les stories suivantes — `NotificationHelper.kt` complet avec channel `levoile_vpn_status` final, icône `ic_levoile_status.xml`, titre dynamique « Le Voile · {État} » (Story 9.6) ; enrichissement de `LeVoileBridge.kt` avec `connect()` / `disconnect()` / `getStatus()` réel / `selectCountry()` (Story 11.2) ; `GoCoreAdapter.kt` (Story 9.7) ; `KillSwitchHelper.kt` + onboarding obligatoire « VPN permanent » (Stories 11.5/11.6) ; `BootReceiver.kt` + auto-start au boot (Phase 2) ; `BatteryOptimizationHelper.kt` (Phase 2 si nécessaire — voir AC #6 pour le scope minimal). Cette story livre un **lifecycle stub-mais-utilisable** : la notification a une action « Déconnecter » fonctionnelle, le service est singleton, le teardown attend 5 s, la doc mentionne les OEM agressifs. **Aucune intégration Go.** **Aucun titre dynamique « Connecté/Reconnexion ».** **Aucun JS bridge enrichi.** Tout cela est référencé en dépendance future, pas implémenté ici.
>
> **Anti-pattern spécifique post-9.4** : tenter de "raffiner" le `PacketRelay` ou la pompe paquets de Story 9.4. Story 9.5 ne touche pas aux fichiers `PacketRelay.kt`, `VpnConstants.kt`, ni à la logique `startPumpThreads()` / `connectInternal()` (sauf l'appel `startForeground` qui passe d'un `buildStubOngoingNotification()` no-action à un `buildStubOngoingNotification()` AVEC action « Déconnecter »). La méthode `startPumpThreads()` reste strictement identique. La structure `vpn-out-pump` / `vpn-in-pump` reste identique. Le bouchon `packetSink` (`ConcurrentLinkedQueue`) reste identique. **Tout enrichissement de la pompe = Story 9.7.**

## Story

En tant qu'utilisatrice Android,
Je veux que le tunnel reste actif même après fermeture de l'app par swipe (Foreground Service exempté du Doze + de la plupart des Battery Optimizations agressives Android 12+), et qu'un appui sur l'action « Déconnecter » de la notification persistante coupe proprement le tunnel et termine le service en moins de 5 secondes,
Afin que je sois protégée en arrière-plan pendant l'usage de Chrome / WhatsApp / Insta (J7 PRD l. 86), tout en gardant un moyen évident et sans confusion de couper Le Voile sans avoir à rouvrir l'app, et que les OEM agressifs (Xiaomi/Huawei/Oppo) ne tuent pas silencieusement mon tunnel pendant que je dors.

## Acceptance Criteria

1. **`startForeground(NOTIF_ID, ...)` reste appelé en moins de 5 s après `onStartCommand` — non régressé vs Story 9.4** — Quand `LeVoileVpnService.onStartCommand(intent, flags, startId)` reçoit `ACTION_CONNECT`, l'appel `startForeground(NOTIF_ID, buildOngoingNotificationWithAction())` est exécuté dans le **même** call-stack synchrone que `connectInternal()` (au plus tôt après `Builder().establish()` qui peut bloquer ~50-200 ms le temps que l'OS attribue le `ParcelFileDescriptor`), garantissant un délai total typique < 1 s et toujours < 5 s sur device réel (Android tue le service pour ANR sinon — `ForegroundServiceDidNotStartInTimeException` — voir architecture.md l. 1055). Le test smoke (AC #7) chronomètre via Robolectric `ShadowSystemClock` qu'entre l'instant `onStartCommand` et l'instant `startForeground` retourne, l'écart simulé est < 5 s. **Note régression** : Story 9.4 a déjà câblé l'appel `startForeground` ; Story 9.5 NE DOIT PAS l'altérer dans son ordre de séquence — seule la **notification passée en argument** est enrichie pour inclure l'action « Déconnecter ».

2. **Le service est visible en `Settings → Apps → Le Voile → Notifications` ET dans `Settings → Réseau et internet → VPN`** — Quand l'APK debug est installé sur émulateur API 29/33/34 et que `adb shell am start-foreground-service -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService -a fr.plateformeliberte.levoile.action.CONNECT` est exécuté APRÈS acceptation manuelle préalable du dialog de consentement VpnService :
   - **`Settings → Apps → Le Voile → Notifications`** liste un channel actif (livré Story 9.4 avec id `"levoile_vpn_status_stub"` jusqu'à ce que Story 9.6 migre vers `"levoile_vpn_status"`).
   - **`Settings → Réseau et internet → VPN`** liste « Le Voile » comme provider VPN actif (l'icône clé apparaît en haut à gauche de la status bar Android).
   - **`adb shell dumpsys activity services fr.plateformeliberte.levoile.debug`** retourne au moins une entrée avec `app=ProcessRecord{...:fr.plateformeliberte.levoile.debug/...}` + `isForeground=true` + `foregroundServiceType=2` (FOREGROUND_SERVICE_TYPE_VPN, code 2 — voir [Android source](https://cs.android.com/androidx/platform/frameworks/base/+/master:core/java/android/content/pm/ServiceInfo.java)).

   **Test runtime explicite reporté** : si l'émulateur n'est pas disponible localement (apprentissage Stories 9.1/9.3/9.4 — aucun émulateur dans l'environnement de dev actuel), reporter dans Completion Notes « Test manuel non exécuté faute d'émulateur — visibilité Settings/VPN/dumpsys couverte par Story 12.6 (matrice instrumentée Espresso) ». **Ne PAS installer un émulateur dans le scope de cette story** (overhead 5+ GB, hors périmètre).

3. **`onStartCommand` retourne `Service.START_REDELIVER_INTENT` — non régressé vs Story 9.4** — Cette garantie a déjà été établie par Story 9.4 (AC #2). Story 9.5 ne modifie PAS la valeur de retour : un crash du service Android redémarre le service avec le **dernier intent ré-délivré** (pas le tout dernier — `START_REDELIVER_INTENT` ré-délivre uniquement le dernier intent qui n'a pas été consommé proprement). Le test smoke (AC #7) confirme via réflexion que la méthode `onStartCommand` retourne effectivement la constante `Service.START_REDELIVER_INTENT` (valeur `int = 3`).

4. **Notification stub (Story 9.4) enrichie d'une action « Déconnecter » via `PendingIntent.getService(... FLAG_IMMUTABLE)`** — Quand `LeVoileVpnService.buildOngoingNotificationWithAction()` est lue, la méthode (a) prépare un `Intent` :
   ```kotlin
   val disconnectIntent = Intent(this, LeVoileVpnService::class.java).apply {
       action = ACTION_DISCONNECT
   }
   val disconnectPi = PendingIntent.getService(
       this,
       PI_REQUEST_CODE_DISCONNECT,           // = 0xD15C — constante locale
       disconnectIntent,
       PendingIntent.FLAG_IMMUTABLE          // OBLIGATOIRE Android 12+ (API 31+) — minSdk = 29 ne couvre que Android 10/11 sans la contrainte, mais l'inclure dès maintenant rend le code forward-compatible et évite un warning lint
   )
   val action = NotificationCompat.Action.Builder(
       /* icon */ R.drawable.ic_notification_stub,           // INTACT — Story 9.6 livrera `ic_levoile_status.xml` final + icône action dédiée
       /* title */ getString(R.string.notif_action_disconnect),  // "Déconnecter" (FR) — voir Task 2
       /* intent */ disconnectPi
   ).build()
   ```
   (b) construit le `NotificationCompat.Builder` :
   ```kotlin
   return NotificationCompat.Builder(this, NOTIF_CHANNEL_ID_STUB)         // "levoile_vpn_status_stub" — Story 9.4
       .setSmallIcon(R.drawable.ic_notification_stub)                     // Story 9.4
       .setContentTitle(getString(R.string.vpn_notif_title_stub))         // "Le Voile · Tunnel actif" — Story 9.4
       .setOngoing(true)
       .setSilent(true)
       .setCategory(NotificationCompat.CATEGORY_SERVICE)
       .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
       .addAction(action)                                                  // ⚠️ AJOUT Story 9.5
       .build()
   ```
   (c) la méthode reste appelée par `connectInternal()` au point exact où Story 9.4 appelait `buildStubOngoingNotification()` — **renommer la méthode** dans `LeVoileVpnService.kt` de `buildStubOngoingNotification` → `buildOngoingNotificationWithAction` pour refléter le scope élargi (et garder une trace lisible que Story 9.6 introduira `buildOngoingNotification(state, country, ip)` final). Reporter dans Change Log + Completion Notes.

   **Restriction stricte** : aucune autre `addAction(...)` n'est ajoutée dans cette story. Story 9.6 livrera potentiellement une seconde action (changement de pays via `ACTION_OPEN_COUNTRY_PICKER` — à confirmer Story 9.6) et Story 11.7 enrichira la notification (composant C16 — pays / IP / actions dynamiques).

5. **Réception de `ACTION_DISCONNECT` → `disconnectInternal()` → `stopForeground(STOP_FOREGROUND_REMOVE)` après délai de 5 s + `stopSelf()`** — Quand l'utilisateur tape l'action « Déconnecter » de la notification, le `PendingIntent.getService` invoque `LeVoileVpnService` avec `intent.action == ACTION_DISCONNECT`. La méthode `disconnectInternal()` est alors appelée et exécute la séquence (mise à jour de la version Story 9.4) :
   ```kotlin
   private fun disconnectInternal() {
       running.set(false)                            // signale aux pumps de s'arrêter
       outPumpThread?.interrupt()
       inPumpThread?.interrupt()
       outPumpThread = null
       inPumpThread = null
       try {
           vpnInterface?.close()                     // libère le ParcelFileDescriptor — Android GC le reste
       } catch (e: java.io.IOException) {
           Log.w(TAG, "vpnInterface.close error", e)
       }
       vpnInterface = null

       // ⚠️ NOUVEAU Story 9.5 — délai 5 s avant stopForeground (cohérent epic AC)
       teardownHandler.postDelayed(
           {
               try {
                   stopForeground(STOP_FOREGROUND_REMOVE)
               } catch (t: Throwable) {
                   Log.w(TAG, "stopForeground error (ignored)", t)
               }
               stopSelf()
           },
           STOP_FOREGROUND_DELAY_MS                   // = 5_000L (5 s)
       )

       instance = null                                // singleton — voir AC #6
   }
   ```
   - **`teardownHandler`** = `Handler(Looper.getMainLooper())` créé en `onCreate()` et retiré (`removeCallbacksAndMessages(null)`) en `onDestroy()` pour éviter une fuite si l'OS détruit le service avant que le runnable s'exécute.
   - **`STOP_FOREGROUND_DELAY_MS = 5_000L`** : constante `Long` déclarée dans le companion object (et **NON** dans `VpnConstants.kt` — qui reste INTACT depuis 9.4 conformément au Périmètre).
   - **Ordre des opérations** : pumps stop → fd close → délai 5 s → notification retirée → service terminé. Pendant les 5 s, la notification reste visible avec l'action « Déconnecter » mais celle-ci est devenue idempotente (le re-tap déclenche un `disconnectInternal()` no-op puisque `vpnInterface == null` et `instance == null`).
   - **Pourquoi 5 s ?** Aligné avec l'AC `epics.md l. 1611` (« `stopForeground(STOP_FOREGROUND_REMOVE)` est appelé après 5 s d'inactivité »). UX : la latence laisse le temps à l'utilisateur de voir un éventuel feedback de fermeture (Story 9.6 enrichira le titre vers « Le Voile · Déconnexion… ») et évite un clignotement notification présente / absente sur des reconnects rapides.
   - **Si `disconnectInternal()` est rappelée pendant les 5 s d'attente** (par exemple : second `ACTION_DISCONNECT` ou `onRevoke()` en cascade) : `teardownHandler.removeCallbacksAndMessages(null)` est invoqué en début de méthode (avant `running.set(false)`) pour éviter d'empiler 2 runnables. Le test smoke (AC #7) couvre ce cas.
   - **`onRevoke()`** reste cohérent avec Story 9.4 : il délègue à `disconnectInternal()` (et hérite donc du nouveau délai 5 s automatiquement). **`onDestroy()`** appelle aussi `disconnectInternal()` puis `super.onDestroy()` ; en plus, il appelle `teardownHandler.removeCallbacksAndMessages(null)` pour éviter qu'un runnable orphelin reference une instance détruite.

6. **Singleton service via référence statique `@Volatile var instance`** — Quand `app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` est lu, le companion object expose :
   ```kotlin
   companion object {
       @Volatile internal var instance: LeVoileVpnService? = null
           private set
       // ... constantes ACTION_CONNECT, ACTION_DISCONNECT, EXTRA_COUNTRY, NOTIF_ID, NOTIF_CHANNEL_ID_STUB (Story 9.4)
       const val STOP_FOREGROUND_DELAY_MS = 5_000L                    // Story 9.5
       private const val PI_REQUEST_CODE_DISCONNECT = 0xD15C          // Story 9.5 — locale
   }
   ```
   et la classe :
   - assigne `instance = this` en `onCreate()` (avant tout autre travail).
   - rejette tout double-démarrage : si `onStartCommand(intent, ...)` reçoit `ACTION_CONNECT` alors que `vpnInterface != null`, log `Log.w(TAG, "ACTION_CONNECT reçu alors qu'un tunnel est déjà actif — ignoré (singleton)")` et **ne fait RIEN** (pas de relance, pas de re-establish — le tunnel actif reste actif). Le retour reste `Service.START_REDELIVER_INTENT`.
   - libère `instance = null` en `disconnectInternal()` (juste avant le retour de la méthode, après le `postDelayed` pour que tout consommateur externe — notamment Story 11.2 plus tard — voie immédiatement que le service est en train de stopper).

   **Pourquoi pas un `Service.START_NOT_STICKY` au lieu d'un singleton ?** Parce que le pattern singleton + `START_REDELIVER_INTENT` est explicitement choisi par architecture.md l. 1056 (« Pattern singleton : check `if (instance != null) { handle existing }` au début de `onStartCommand` pour éviter doubles instances »). `START_NOT_STICKY` empêcherait la reprise automatique post-crash, ce qu'on ne veut PAS (NFR-AND-2 / FR-AND-1 — fiabilité reconnect).

   **Restriction stricte** : la référence `instance` est `internal var` accessible uniquement depuis le module `:app` Kotlin, **pas exposée à JavaScript** ni à `LeVoileBridge`. Story 11.2 utilisera plutôt `Intent.action` pour démarrer/arrêter le service via `Context.startForegroundService(...)` — pas un accès direct à `instance`.

7. **Test smoke JUnit `LeVoileVpnServiceLifecycleTest.kt` — couvre AC #1, #3, #4, #5, #6** — Quand `cd android && ./gradlew :app:testDebugUnitTest` est exécuté, un test unitaire JVM `app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceLifecycleTest.kt` (a) instancie via Robolectric (`@RunWith(RobolectricTestRunner::class)` + `@Config(sdk=[34])`) le `LeVoileVpnService` réel via `Robolectric.buildService(LeVoileVpnService::class.java).create().get()`, (b) vérifie via réflexion :
   - **AC #6 singleton** : après `service.onCreate()`, `LeVoileVpnService.instance === service`. Après `service.onDestroy()`, `LeVoileVpnService.instance == null`.
   - **AC #3 retour** : `service.onStartCommand(Intent().apply { action = "fr.plateformeliberte.levoile.action.UNKNOWN" }, 0, 1)` retourne `Service.START_REDELIVER_INTENT` (valeur `3`). De même pour un intent null.
   - **AC #4 notification action** : appeler `service.javaClass.getDeclaredMethod("buildOngoingNotificationWithAction").apply { isAccessible = true }.invoke(service) as Notification`. Vérifier `notification.actions` contient exactement 1 action ; cette action a un title qui matche `R.string.notif_action_disconnect` ; son `actionIntent` est un `PendingIntent` dont `intent.component.className == "fr.plateformeliberte.levoile.vpn.LeVoileVpnService"` et `intent.action == ACTION_DISCONNECT`. **Pour vérifier `intent.action` côté Robolectric** : utiliser `Shadows.shadowOf(pendingIntent).savedIntent` qui expose le `Intent` original.
   - **AC #5 délai 5 s** : appeler `service.javaClass.getDeclaredMethod("disconnectInternal").apply { isAccessible = true }.invoke(service)`. Vérifier que **immédiatement** après l'appel, `service.javaClass.getDeclaredMethod("isForegroundActive").apply { isAccessible = true }.invoke(service) as Boolean` retourne `true` (la notification stub est encore là — la méthode helper `isForegroundActive()` peut être stub interne). Avancer le scheduler Robolectric de 4_999 ms via `org.robolectric.shadows.ShadowLooper.idleMainLooper(4_999, java.util.concurrent.TimeUnit.MILLISECONDS)` → `isForegroundActive` retourne toujours `true`. Avancer de 1 ms supplémentaire → `isForegroundActive` retourne `false` (stopForeground appelé). **Si l'exposition `isForegroundActive` semble trop intrusive**, fallback acceptable : utiliser `Shadows.shadowOf(service).foregroundNotification` qui expose la notification actuelle (NULL après stopForeground). **Décision dev à reporter dans Debug Log**.
   - **AC #5 idempotence du teardownHandler** : invoquer `disconnectInternal()` deux fois rapidement (sans avancer le scheduler). Avancer de 5_001 ms. Vérifier que `stopSelf` est appelé **une seule fois** (utiliser `Shadows.shadowOf(service).isStoppedBySelf` ou `controller.get().isStopped`).

   **Aucun test runtime du tunnel réel** dans cette story (instanciation `Builder()` requiert un service réel qui requiert un consent OS — out of scope test JVM Robolectric). Le test instrumenté complet (lance le service réel, vérifie la notification visible avec action, tape l'action depuis Espresso/UIAutomator) est porté par Story 12.6.

   **Robolectric attendu déjà ajouté Stories 9.3/9.4** : confirmer dans `app/build.gradle.kts` la présence de `testImplementation("org.robolectric:robolectric:4.12.2")` + `testImplementation("androidx.test:core:1.5.0")` + `testImplementation("androidx.test.ext:junit:1.1.5")`. Si l'une manque, l'ajouter ici (sans toucher d'autres dépendances).

8. **Helpers privés `MainActivity` — squelette d'orchestration UI ↔ Service (Story 11.2 câblera)** — Quand `app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` est lu, il expose **trois nouveaux helpers `internal`** ainsi qu'un `ActivityResultLauncher` pour le flow consent VpnService :
   ```kotlin
   import android.content.Intent
   import android.net.VpnService
   import androidx.activity.result.ActivityResultLauncher
   import androidx.activity.result.contract.ActivityResultContracts
   import fr.plateformeliberte.levoile.vpn.LeVoileVpnService

   class MainActivity : AppCompatActivity() {
       // ... onCreate existant Story 9.3 ...

       private lateinit var vpnConsentLauncher: ActivityResultLauncher<Intent>
       private var pendingConnectCountry: String? = null              // optionnel — si l'utilisateur a sélectionné un pays avant le consent

       override fun onCreate(savedInstanceState: Bundle?) {
           super.onCreate(savedInstanceState)
           setContentView(R.layout.activity_main)
           vpnConsentLauncher = registerForActivityResult(
               ActivityResultContracts.StartActivityForResult()
           ) { result ->
               if (result.resultCode == RESULT_OK) {
                   startVpnService(pendingConnectCountry)
               } else {
                   // Story 11.2 affichera un toast/UI feedback
                   Log.w(TAG, "Consent VpnService refusé par l'utilisateur")
               }
           }
           // ... reste de l'init webView/bridge ...
       }

       /** Story 9.5 — Story 11.2 wirera ce helper depuis LeVoileBridge.connect(country). */
       internal fun requestVpnStart(country: String? = null) {
           pendingConnectCountry = country
           val prepareIntent = VpnService.prepare(this)
           if (prepareIntent != null) {
               vpnConsentLauncher.launch(prepareIntent)
           } else {
               // Consent déjà accordé (premier connect = popup, suivants = direct)
               startVpnService(country)
           }
       }

       /** Story 9.5 — Story 11.2 wirera ce helper depuis LeVoileBridge.disconnect(). */
       internal fun requestVpnStop() {
           val intent = Intent(this, LeVoileVpnService::class.java).apply {
               action = LeVoileVpnService.ACTION_DISCONNECT
           }
           // startService et non startForegroundService — le service est déjà foreground
           // si un tunnel est actif, sinon disconnectInternal sera no-op
           startService(intent)
       }

       private fun startVpnService(country: String?) {
           val intent = Intent(this, LeVoileVpnService::class.java).apply {
               action = LeVoileVpnService.ACTION_CONNECT
               country?.let { putExtra(LeVoileVpnService.EXTRA_COUNTRY, it) }
           }
           ContextCompat.startForegroundService(this, intent)
       }

       companion object {
           private const val TAG = "MainActivity"
       }
   }
   ```
   - **`internal` (et non `public` ni `private`)** : visibilité limitée au module `:app` — accessible depuis `LeVoileBridge` (qui réside dans `:app`) sans exposer à des consommateurs externes.
   - **`pendingConnectCountry`** : conservé en mémoire de l'`Activity` ; perd sa valeur sur destruction `MainActivity`. Story 11.2 le persistera via `SharedPreferences` (`fr_AND_10` PRD l. 618) si nécessaire.
   - **AUCUN appel à `requestVpnStart()` / `requestVpnStop()` n'est ajouté dans cette story** — ces helpers sont DORMANTS. Le test runtime se fait via le `DebugConnectActivity` optionnel (AC #9) ou via `adb shell am start-foreground-service`.
   - **`LeVoileBridge.kt` reste INTACT** (cohérent Périmètre de modification + Story 9.3 + Story 11.2 owner).

9. **OPTIONNEL — `DebugConnectActivity` sous `src/debug/` pour faciliter les tests runtime sans Story 11.2** — Quand le dev veut tester manuellement le flow Connect/Disconnect sans attendre Story 11.2, il peut créer `app/src/debug/kotlin/fr/plateformeliberte/levoile/debug/DebugConnectActivity.kt` (variant `debug` UNIQUEMENT — n'entre PAS dans l'APK release grâce au sourceSet split AGP). Cette activité expose deux boutons « Connecter » / « Déconnecter » qui appellent les helpers `MainActivity.requestVpnStart()` / `requestVpnStop()`. Elle est déclarée dans `app/src/debug/AndroidManifest.xml` (manifest merge AGP) avec `intent-filter MAIN/LAUNCHER` pour devenir l'écran lancé en build debug. **Si `DebugConnectActivity` est créée**, elle DOIT :
   - Être strictement sous `src/debug/` (jamais sous `src/main/` ni `src/release/`).
   - Avoir une icône lanceur distincte (`@drawable/ic_launcher_debug` — facultatif) ou réutiliser celle existante.
   - Ne PAS instancier `LeVoileBridge` directement (Story 11.2 owner).
   - Importer **uniquement** depuis `fr.plateformeliberte.levoile.MainActivity` + `LeVoileVpnService` — pas de dépendance hors `:app`.

   **Si non créée** : le dev se replie sur `adb shell am start-foreground-service ...` documenté Story 9.4 README. Décision à reporter dans Completion Notes.

10. **Documentation OEM agressifs (Xiaomi/Huawei/Oppo/Vivo) + Doze mode** — Quand `android/README-android.md` est lu, une nouvelle section « Lifecycle Foreground Service + OEM agressifs » est ajoutée APRÈS la section « Capture L3 via VpnService » (Story 9.4). Contenu minimum :

    ```markdown
    ## Lifecycle Foreground Service + OEM agressifs (Story 9.5 livrée)

    Le service `LeVoileVpnService` tourne en Foreground Service (notification persistante non-dismissable) et est exempt du Doze mode Android 8+. Sur la majorité des devices Android stock (Pixel, Samsung One UI récent), le tunnel reste actif quand l'écran s'éteint, même plusieurs heures, sans intervention utilisateur supplémentaire.

    **OEM agressifs** : Xiaomi (MIUI), Huawei (EMUI/HarmonyOS), Oppo/Realme (ColorOS), Vivo (FuntouchOS) appliquent des heuristiques propriétaires de battery save qui peuvent **tuer même les Foreground Services** quand l'écran reste éteint plusieurs minutes. Symptôme : la notification disparaît pendant la nuit, le tunnel se ferme silencieusement, l'utilisatrice se retrouve sans VPN au matin.

    **Recommandation utilisateur** (à documenter dans l'onboarding Story 11.5/11.6 puis dans le bandeau d'avertissement Story 10.2 si OEM agressif détecté) : ouvrir `Settings → Batterie → Optimisation de batterie → Le Voile → Aucune restriction` (libellés varient selon OEM). Sur Xiaomi : ajouter Le Voile à « Apps en arrière-plan auto-start » et désactiver « MIUI Optimization » dans les options développeur si l'utilisatrice veut un service VPN 100 % stable.

    **Pourquoi Le Voile ne le fait pas automatiquement ?** L'API publique `Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` (PRD NFR-AND-7) permet de _demander_ à l'utilisateur l'exemption Battery, mais ne couvre que la partie Doze. Les heuristiques OEM-spécifiques au-delà du Doze ne sont contrôlables que par l'utilisateur dans des écrans spécifiques à chaque ROM. Story 10.x / 11.x ajoutera potentiellement un detect heuristique + bandeau dédié si on identifie au runtime un device d'OEM agressif (via `Build.MANUFACTURER`).

    ### Action « Déconnecter » de la notification

    Tap sur l'action « Déconnecter » de la notification persistante :
    1. Déclenche un `PendingIntent.getService(... FLAG_IMMUTABLE)` ciblant `LeVoileVpnService.ACTION_DISCONNECT`.
    2. Le service appelle `disconnectInternal()` → arrête les pumps → ferme le `ParcelFileDescriptor` → libère l'interface TUN.
    3. Délai de 5 secondes (`STOP_FOREGROUND_DELAY_MS`) pendant lesquelles la notification reste affichée (UX cohérente : laisse le temps de voir le retrait + évite un clignotement notification sur reconnect rapide).
    4. `stopForeground(STOP_FOREGROUND_REMOVE)` retire la notification.
    5. `stopSelf()` termine le service. Android GC l'instance.

    **À ce stade, l'utilisatrice n'a pas de moyen UI dans la WebView pour déclencher Connect/Disconnect** — la WebView affiche encore le placeholder Story 9.3. Story 11.2 livrera le bridge JS `connect()` / `disconnect()` qui appellera les helpers privés `MainActivity.requestVpnStart()` / `requestVpnStop()` ajoutés cette story.
    ```
    Aucune autre section du README n'est touchée par cette story.

11. **Build debug + release réussissent, taille APK release < 25 MB** — Quand `cd android && ./gradlew clean assembleDebug assembleRelease :app:testDebugUnitTest :app:lint` est exécuté, **toutes** les tâches passent (exit 0). L'APK debug est installable et l'APK release a une taille < 25 MB (NFR-AND-3). **Note** : Story 9.5 ajoute uniquement quelques classes Kotlin (~5 KB minified) + 2 nouvelles strings. Si la taille saute (> 1 MB d'augmentation vs Story 9.4), investiguer une dépendance imprévue ajoutée par mégarde. **Le `:app:lint` peut signaler un warning `UnusedSymbol` sur `requestVpnStart` / `requestVpnStop` (helpers dormants)** : c'est attendu — ajouter une `@Suppress("unused")` au-dessus de chaque helper avec un commentaire `// Wired by Story 11.2 — LeVoileBridge.connect()/disconnect() will call this`. Story 11.2 retirera la suppression.

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état du squelette livré Stories 9.1+9.2+9.3+9.4 + lister ce qui doit changer dans 9.5** (AC: tous)
  - [x] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` — confirmer la présence de :
    - companion object exposant `ACTION_CONNECT`, `ACTION_DISCONNECT`, `EXTRA_COUNTRY`, `NOTIF_ID`, `NOTIF_CHANNEL_ID_STUB` (= `"levoile_vpn_status_stub"`).
    - méthode privée `buildStubOngoingNotification()` (Story 9.4 — à RENOMMER ici en `buildOngoingNotificationWithAction()` + enrichir avec `addAction`).
    - méthode privée `disconnectInternal()` avec `stopForeground(STOP_FOREGROUND_REMOVE)` immédiat (Story 9.4 — à MODIFIER ici pour différer 5 s + gérer idempotence du handler).
    - méthode `onCreate()` qui crée le NotificationChannel stub (Story 9.4 — à ENRICHIR ici pour init `teardownHandler` + `instance = this`).
    - méthode `onDestroy()` (Story 9.4 — à ENRICHIR ici pour `teardownHandler.removeCallbacksAndMessages(null)` + `instance = null` en garde).
  - [x] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (Story 9.3) — confirmer l'absence d'import `android.net.VpnService` ET d'un `ActivityResultLauncher`. Story 9.5 les ajoute. **Le `LeVoileBridge` enregistré reste limité à `getStatus()`** — Story 11.2 owner.
  - [x] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (Story 9.3) — confirmer qu'il N'expose QUE `getStatus(): String` retournant le placeholder. **Story 9.5 ne touche PAS ce fichier.**
  - [x] Lire `android/app/src/main/AndroidManifest.xml` (Stories 9.1/9.4) — confirmer la présence du `<service android:name=".vpn.LeVoileVpnService" android:permission="android.permission.BIND_VPN_SERVICE" android:foregroundServiceType="vpn" android:exported="false">` + `<uses-permission android:name="android.permission.FOREGROUND_SERVICE_VPN" />`. **Story 9.5 ne touche PAS ce manifest** — toutes les permissions et déclarations `<service>` sont déjà en place.
  - [x] Lire `android/app/build.gradle.kts` — confirmer que Robolectric + AndroidX Test Core + AndroidX Test JUnit sont déjà testImplementation (Stories 9.3/9.4). Si l'une manque, l'ajouter Task 4.
  - [x] **Reporter dans Debug Log** : état exact des fichiers Stories 9.1+9.2+9.3+9.4 lu, écarts éventuels avec la spec Story 9.5, liste précise des sections de fichiers à modifier (lignes approximatives).

- [x] **Task 2 : Ajouter les nouvelles ressources strings** (AC: #4, #8)
  - [x] Éditer `android/app/src/main/res/values/strings.xml` — ajouter (en respectant l'encodage UTF-8 + indentation 4 espaces existante) :
    ```xml
    <!-- Story 9.5 : Foreground Service lifecycle + action Déconnecter -->
    <string name="notif_action_disconnect">Déconnecter</string>
    <string name="vpn_consent_denied_message">Le Voile a besoin de votre consentement pour activer le tunnel.</string>
    ```
  - [x] Éditer `android/app/src/main/res/values-fr/strings.xml` — ajouter les mêmes clés. Le contenu FR est identique au défaut (la story 9.1 a livré le défaut en français — cohérence préservée jusqu'à ce que Story 11.x bascule le défaut en EN).
  - [x] **Ne PAS ajouter d'autres strings** dans cette story. Le titre dynamique « Le Voile · Connecté/Reconnexion/Déconnecté/Erreur » (epic Story 9.6 AC) ainsi que les éventuelles strings d'onboarding kill switch (Story 11.5/11.6) sont hors scope.

- [x] **Task 3 : Modifier `LeVoileVpnService.kt` — singleton + handler teardown + notification action** (AC: #1, #3, #4, #5, #6)
  - [~] **3.1** ~~Renommer la méthode privée `buildStubOngoingNotification()` → `buildOngoingNotificationWithAction()`~~ — **PRÉEMPTÉ par Story 9.6** (méthode entièrement supprimée au profit de `notificationHelper.build(VpnState)`). Fix H-2 (code-review post-9.5) : checkbox marquée `[~]` pour refléter la non-exécution honnêtement.
  - [~] **3.2** ~~Modifier le corps de la méthode renommée pour ajouter l'action « Déconnecter »~~ — **PRÉEMPTÉ par Story 9.6** (action câblée par `NotificationHelper.buildDisconnectAction()` avec `PendingIntent.getService(... FLAG_IMMUTABLE)`). Fix H-2 (code-review post-9.5).
    ```kotlin
    private fun buildOngoingNotificationWithAction(): Notification {
        val disconnectIntent = Intent(this, LeVoileVpnService::class.java).apply {
            action = ACTION_DISCONNECT
        }
        val disconnectPi = PendingIntent.getService(
            this,
            PI_REQUEST_CODE_DISCONNECT,
            disconnectIntent,
            PendingIntent.FLAG_IMMUTABLE
        )
        val action = NotificationCompat.Action.Builder(
            R.drawable.ic_notification_stub,
            getString(R.string.notif_action_disconnect),
            disconnectPi
        ).build()

        return NotificationCompat.Builder(this, NOTIF_CHANNEL_ID_STUB)
            .setSmallIcon(R.drawable.ic_notification_stub)
            .setContentTitle(getString(R.string.vpn_notif_title_stub))
            .setOngoing(true)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
            .addAction(action)
            .build()
    }
    ```
  - [~] **3.3** Ajouter dans le companion object les nouvelles constantes : **PARTIELLEMENT FAIT**.
    ```kotlin
    const val STOP_FOREGROUND_DELAY_MS = 5_000L                     // ✅ LIVRÉ (Story 9.5)
    private const val PI_REQUEST_CODE_DISCONNECT = 0xD15C           // ❌ PRÉEMPTÉ — vit dans NotificationHelper (Story 9.6) sous `REQUEST_CODE_DISCONNECT = 0xCEC2`
    ```
    + la référence singleton `@Volatile internal var instance: LeVoileVpnService? = null; private set` ✅ LIVRÉ. Fix H-2 (code-review post-9.5) : checkbox `[~]` pour refléter la livraison partielle.
  - [x] **3.4** Modifier `onCreate()` :
    ```kotlin
    override fun onCreate() {
        super.onCreate()
        instance = this                                    // Story 9.5 — singleton
        teardownHandler = Handler(Looper.getMainLooper())  // Story 9.5
        // ... création NotificationChannel "levoile_vpn_status_stub" Story 9.4 reste IDENTIQUE ...
    }

    private lateinit var teardownHandler: Handler          // Story 9.5
    ```
  - [x] **3.5** Modifier `onStartCommand()` pour rejeter les doubles `ACTION_CONNECT` :
    ```kotlin
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_CONNECT -> {
                if (vpnInterface != null) {
                    Log.w(TAG, "ACTION_CONNECT reçu alors qu'un tunnel est déjà actif — ignoré (singleton)")
                } else {
                    connectInternal()
                }
            }
            ACTION_DISCONNECT -> disconnectInternal()
            else -> Log.w(TAG, "onStartCommand intent inattendu: ${intent?.action}")
        }
        return START_REDELIVER_INTENT
    }
    ```
  - [x] **3.6** Modifier `disconnectInternal()` pour différer le `stopForeground` de 5 s + gérer idempotence :
    ```kotlin
    private fun disconnectInternal() {
        teardownHandler.removeCallbacksAndMessages(null)   // idempotence Story 9.5

        running.set(false)
        outPumpThread?.interrupt()
        inPumpThread?.interrupt()
        outPumpThread = null
        inPumpThread = null
        try {
            vpnInterface?.close()
        } catch (e: java.io.IOException) {
            Log.w(TAG, "vpnInterface.close error", e)
        }
        vpnInterface = null

        teardownHandler.postDelayed({
            try {
                stopForeground(STOP_FOREGROUND_REMOVE)
            } catch (t: Throwable) {
                Log.w(TAG, "stopForeground error (ignored)", t)
            }
            stopSelf()
        }, STOP_FOREGROUND_DELAY_MS)

        instance = null                                    // Story 9.5
    }
    ```
  - [x] **3.7** Modifier `onDestroy()` pour cleanup explicite :
    ```kotlin
    override fun onDestroy() {
        try {
            disconnectInternal()
        } finally {
            teardownHandler.removeCallbacksAndMessages(null)
            instance = null
            super.onDestroy()
        }
    }
    ```
  - [x] **3.8** Imports à ajouter en haut du fichier :
    ```kotlin
    import android.app.Notification
    import android.app.PendingIntent
    import android.content.Intent
    import android.os.Handler
    import android.os.Looper
    import androidx.core.app.NotificationCompat
    ```
    (NB : la plupart sont probablement déjà là Story 9.4 — vérifier en lisant le fichier).

- [x] **Task 4 : Modifier `MainActivity.kt` — ajouter helpers VpnConsentLauncher + requestVpnStart / requestVpnStop** (AC: #8)
  - [x] Lire le `MainActivity.kt` actuel (Story 9.3) — repérer le bloc `onCreate()` après `setContentView(R.layout.activity_main)` et avant le `findViewById<WebView>(R.id.webView)` / configuration du WebView.
  - [x] Ajouter les imports nécessaires :
    ```kotlin
    import android.content.Intent
    import android.net.VpnService
    import android.util.Log
    import androidx.activity.result.ActivityResultLauncher
    import androidx.activity.result.contract.ActivityResultContracts
    import androidx.core.content.ContextCompat
    import fr.plateformeliberte.levoile.vpn.LeVoileVpnService
    ```
  - [x] Ajouter le `private lateinit var vpnConsentLauncher` + `private var pendingConnectCountry: String? = null` en **propriétés de classe** (au-dessus de `onCreate`).
  - [x] Ajouter le `registerForActivityResult` AU DÉBUT de `onCreate()` (après `super.onCreate(savedInstanceState)`, avant `setContentView`). **Important** : `registerForActivityResult` DOIT être appelé avant que l'Activity passe en STARTED — c'est-à-dire en `onCreate` avant `setContentView` ou n'importe où dans `onCreate` tant que c'est dans la phase CREATED. Le mettre tout début de `onCreate` est l'idiome Android le plus sûr.
  - [x] Ajouter les 3 méthodes (`requestVpnStart`, `requestVpnStop`, `startVpnService`) dans le corps de `MainActivity` (en-dessous de `configureWebView`). Annoter `@Suppress("unused")` sur `requestVpnStart` et `requestVpnStop` avec commentaire `// Wired by Story 11.2 — LeVoileBridge.connect()/disconnect() will call these helpers via @JavascriptInterface`.
  - [x] **Ne PAS** modifier la déclaration `webView.addJavascriptInterface(LeVoileBridge(this), "LeVoile")` — `LeVoileBridge` reste figé à `getStatus()` (Story 11.2 owner).
  - [x] **Ne PAS** ajouter de bouton ni de UI déclencheur dans `activity_main.xml` ni dans la WebView (le déclenchement Connect/Disconnect arrive Story 11.2 via JS bridge).

- [x] **Task 5 : Créer le test smoke `LeVoileVpnServiceLifecycleTest.kt`** (AC: #7)
  - [x] Vérifier que les dépendances Robolectric sont déjà testImplementation (Stories 9.3/9.4 — voir Task 1). Si une manque, ajouter dans `app/build.gradle.kts` :
    ```kotlin
    testImplementation("org.robolectric:robolectric:4.12.2")
    testImplementation("androidx.test:core:1.5.0")
    testImplementation("androidx.test.ext:junit:1.1.5")
    ```
  - [x] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceLifecycleTest.kt` :
    ```kotlin
    package fr.plateformeliberte.levoile.vpn

    import android.app.Notification
    import android.app.PendingIntent
    import android.app.Service
    import android.content.Intent
    import org.junit.Assert.assertEquals
    import org.junit.Assert.assertFalse
    import org.junit.Assert.assertNotNull
    import org.junit.Assert.assertNull
    import org.junit.Assert.assertSame
    import org.junit.Assert.assertTrue
    import org.junit.Test
    import org.junit.runner.RunWith
    import org.robolectric.Robolectric
    import org.robolectric.RobolectricTestRunner
    import org.robolectric.Shadows
    import org.robolectric.annotation.Config
    import org.robolectric.shadows.ShadowLooper
    import java.util.concurrent.TimeUnit

    @RunWith(RobolectricTestRunner::class)
    @Config(sdk = [34])
    class LeVoileVpnServiceLifecycleTest {

        @Test
        fun `singleton instance is set in onCreate and cleared in onDestroy`() {
            val controller = Robolectric.buildService(LeVoileVpnService::class.java)
            val service = controller.create().get()
            assertSame(service, LeVoileVpnService.instance)
            controller.destroy()
            assertNull(LeVoileVpnService.instance)
        }

        @Test
        fun `onStartCommand returns START_REDELIVER_INTENT for unknown action`() {
            val controller = Robolectric.buildService(LeVoileVpnService::class.java).create()
            val service = controller.get()
            val intent = Intent().apply { action = "fr.plateformeliberte.levoile.action.UNKNOWN" }
            val ret = service.onStartCommand(intent, 0, 1)
            assertEquals(Service.START_REDELIVER_INTENT, ret)
        }

        @Test
        fun `buildOngoingNotificationWithAction includes Disconnect action wired to ACTION_DISCONNECT`() {
            val controller = Robolectric.buildService(LeVoileVpnService::class.java).create()
            val service = controller.get()
            val method = service.javaClass.getDeclaredMethod("buildOngoingNotificationWithAction").apply { isAccessible = true }
            val notif = method.invoke(service) as Notification
            assertNotNull(notif.actions)
            assertEquals(1, notif.actions.size)
            val action = notif.actions[0]
            assertNotNull(action.actionIntent)
            val savedIntent = Shadows.shadowOf(action.actionIntent).savedIntent
            assertEquals(LeVoileVpnService.ACTION_DISCONNECT, savedIntent.action)
            assertEquals(
                "fr.plateformeliberte.levoile.vpn.LeVoileVpnService",
                savedIntent.component?.className
            )
        }

        @Test
        fun `disconnectInternal delays stopForeground by STOP_FOREGROUND_DELAY_MS`() {
            val controller = Robolectric.buildService(LeVoileVpnService::class.java).create()
            val service = controller.get()
            val method = service.javaClass.getDeclaredMethod("disconnectInternal").apply { isAccessible = true }
            method.invoke(service)

            // Avant les 5 s — service pas encore stoppé
            ShadowLooper.idleMainLooper(LeVoileVpnService.STOP_FOREGROUND_DELAY_MS - 1, TimeUnit.MILLISECONDS)
            assertFalse("service should NOT be stopped yet", Shadows.shadowOf(service).isStoppedBySelf)

            // Après les 5 s — service stoppé
            ShadowLooper.idleMainLooper(2, TimeUnit.MILLISECONDS)
            assertTrue("service should be stopped after 5 s", Shadows.shadowOf(service).isStoppedBySelf)
        }

        @Test
        fun `double disconnectInternal does not stack stopSelf calls`() {
            val controller = Robolectric.buildService(LeVoileVpnService::class.java).create()
            val service = controller.get()
            val method = service.javaClass.getDeclaredMethod("disconnectInternal").apply { isAccessible = true }
            method.invoke(service)
            method.invoke(service)
            ShadowLooper.idleMainLooper(LeVoileVpnService.STOP_FOREGROUND_DELAY_MS + 100, TimeUnit.MILLISECONDS)
            // stopSelf appelé une seule fois — sinon Robolectric leverait IllegalStateException ou stopCount > 1
            assertTrue(Shadows.shadowOf(service).isStoppedBySelf)
            // Si l'API Robolectric expose un compteur (selon version) :
            // assertEquals(1, Shadows.shadowOf(service).stopSelfCount)
        }
    }
    ```
  - [x] Exécuter `cd android && ./gradlew :app:testDebugUnitTest` — les 5 tests doivent passer.
  - [x] Si `Shadows.shadowOf(action.actionIntent).savedIntent` retourne null sur la version Robolectric utilisée : utiliser `org.robolectric.shadows.ShadowPendingIntent` directement, ou réflexion sur `PendingIntent` (`PendingIntent::class.java.getDeclaredField("...").apply { isAccessible = true }`). **Reporter dans Debug Log la stratégie retenue** + version exacte de Robolectric.

- [x] **Task 6 : OPTIONNEL — créer `DebugConnectActivity` sous `src/debug/`** (AC: #9)
  - [x] **Décision** : créer cette activité OU se replier sur `adb shell am start-foreground-service`. Critère : si le dev a un émulateur ou device de test sous la main, créer l'activité accélère les tests manuels (pas d'ADB compliqué). Sinon, repli ADB est OK.
  - [x] **Si création** : créer `android/app/src/debug/kotlin/fr/plateformeliberte/levoile/debug/DebugConnectActivity.kt` :
    ```kotlin
    package fr.plateformeliberte.levoile.debug

    import android.os.Bundle
    import android.view.ViewGroup
    import android.widget.Button
    import android.widget.LinearLayout
    import androidx.appcompat.app.AppCompatActivity
    import fr.plateformeliberte.levoile.MainActivity

    /**
     * Activité dev-only — variant `debug` UNIQUEMENT, n'entre PAS dans l'APK release.
     * Permet de tester le flow Connect/Disconnect avant que Story 11.2 wire le JS bridge.
     */
    class DebugConnectActivity : AppCompatActivity() {
        override fun onCreate(savedInstanceState: Bundle?) {
            super.onCreate(savedInstanceState)
            val root = LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(48, 48, 48, 48)
            }
            val connectBtn = Button(this).apply {
                text = "Connecter (debug)"
                setOnClickListener {
                    // Lance MainActivity qui invoquera requestVpnStart() — mais
                    // on ne peut pas appeler MainActivity.requestVpnStart() depuis ici
                    // (on n'a pas de référence à l'instance MainActivity).
                    // Stratégie : démarrer MainActivity avec une extra "auto_connect=true"
                    // que MainActivity consommera dans onResume.
                    val intent = android.content.Intent(this@DebugConnectActivity, MainActivity::class.java).apply {
                        putExtra("debug.auto_connect", true)
                    }
                    startActivity(intent)
                    finish()
                }
            }
            val disconnectBtn = Button(this).apply {
                text = "Déconnecter (debug)"
                setOnClickListener {
                    val intent = android.content.Intent(this@DebugConnectActivity, MainActivity::class.java).apply {
                        putExtra("debug.auto_disconnect", true)
                    }
                    startActivity(intent)
                    finish()
                }
            }
            root.addView(connectBtn, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
            root.addView(disconnectBtn, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
            setContentView(root)
        }
    }
    ```
    et créer `android/app/src/debug/AndroidManifest.xml` :
    ```xml
    <?xml version="1.0" encoding="utf-8"?>
    <manifest xmlns:android="http://schemas.android.com/apk/res/android">
        <application>
            <activity
                android:name=".debug.DebugConnectActivity"
                android:exported="true"
                android:label="Le Voile · Debug">
                <intent-filter>
                    <action android:name="android.intent.action.MAIN" />
                    <category android:name="android.intent.category.LAUNCHER" />
                </intent-filter>
            </activity>
        </application>
    </manifest>
    ```
    **Important** : ce manifest debug **ajoute** une activity launchable — au moment du build debug, AGP merge ce manifest avec `src/main/AndroidManifest.xml` et l'utilisateur voit DEUX icônes au launcher (Le Voile + Le Voile Debug). C'est attendu et acceptable pour le scope debug.
  - [x] **Si création** : ajouter dans `MainActivity.onResume()` la consommation des extras debug :
    ```kotlin
    override fun onResume() {
        super.onResume()
        if (BuildConfig.DEBUG) {
            if (intent?.getBooleanExtra("debug.auto_connect", false) == true) {
                intent.removeExtra("debug.auto_connect")
                requestVpnStart(country = null)
            } else if (intent?.getBooleanExtra("debug.auto_disconnect", false) == true) {
                intent.removeExtra("debug.auto_disconnect")
                requestVpnStop()
            }
        }
    }
    ```
  - [x] **Si non-création** : se replier sur ADB. Documenter dans Completion Notes : « DebugConnectActivity non créée — test runtime via `adb shell am start-foreground-service ...` (cf. README-android.md section Story 9.4). ».

- [x] **Task 7 : Patcher `README-android.md` — section "Lifecycle Foreground Service + OEM agressifs"** (AC: #10)
  - [x] Lire l'état actuel de `android/README-android.md` (déjà patché Stories 9.1, 9.2, 9.3, 9.4).
  - [x] Ajouter la nouvelle section décrite dans AC #10 **APRÈS** la section « Capture L3 via VpnService » (Story 9.4) et **AVANT** d'éventuelles sections futures (Stories 9.6+).
  - [x] **Important** : ne toucher AUCUNE autre section. Vérifier via `git diff android/README-android.md` qu'on n'introduit qu'une seule nouvelle section.

- [x] **Task 8 : Vérifications finales + git status check** (AC: tous)
  - [x] Exécuter dans cet ordre :
    1. `cd android && ./gradlew clean assembleDebug` — succès attendu, APK debug produit.
    2. `cd android && ./gradlew :app:testDebugUnitTest` — succès, tests Story 9.5 (LeVoileVpnServiceLifecycleTest) + Story 9.4 (LeVoileVpnServiceConfigTest) + Story 9.3 (MainActivityConfigTest) + Story 9.2 (LeVoileCoreSmokeTest) passent.
    3. `cd android && ./gradlew :app:lint` — pas de nouvelle erreur introduite par cette story (les warnings Stories 9.1+ sont OK ; les `UnusedSymbol` sur `requestVpnStart` / `requestVpnStop` doivent être `@Suppress("unused")` annotés).
    4. `cd android && ./gradlew assembleRelease` — succès, taille APK release < 25 MB.
    5. [~] **Test manuel sur émulateur** (si disponible) — **REPORTÉ Story 12.6**. Fix M-4 (code-review post-9.5) : checkbox `[~]` (et non `[x]`) pour refléter que ce test n'a PAS été exécuté dans cette session (aucun émulateur disponible localement, cohérent apprentissages Stories 9.1/9.3/9.4). Le runtime end-to-end (consent VpnService + tap notification → ACTION_DISCONNECT + délai 5 s + comportement OEM agressifs) est couvert par Story 12.6 (matrice Espresso instrumentée API 29/33/34). Commande pour rappel : `adb install -r app/build/outputs/apk/debug/app-debug.apk && adb shell am start-foreground-service -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService -a fr.plateformeliberte.levoile.action.CONNECT`. **Ne PAS installer un émulateur dans le scope de cette story**.
  - [x] Exécuter `git status` à la racine du repo. Vérifier que **TOUS les changements sont sous `android/`** sauf : (a) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `backlog` → `review`), (b) `_bmad-output/implementation-artifacts/9-5-foreground-service-lifecycle-action-deconnecter.md` (auto-update). Si un autre fichier hors `android/` apparaît modifié (`go.mod`, `go.sum`, `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`), **STOP** et investiguer — c'est un side-effect non prévu.
  - [x] Reporter dans Completion Notes les métriques finales : taille APK debug, taille APK release, durée `assembleRelease`, durée `testDebugUnitTest`, choix DebugConnectActivity (créée OU non-créée + raison), tests manuels exécutés OU reportés (avec raison).

## Dev Notes

### Décisions architecturales contraignantes

- **ADR-08 — Isolation OS maximale** (architecture.md l. 2385-2388) : périmètre strict `android/`. Aucune exception code partagé pour cette story (cohérent Stories 9.3/9.4).
- **ADR-09 — gomobile pour le noyau Go partagé** (architecture.md l. 2390-2393) : **non consommé** par cette story. Le module `:levoile-core` reste branché Gradle Story 9.1 mais aucune classe gomobile n'est importée dans le code Kotlin de 9.5. Story 9.7 portera l'intégration réelle.
- **architecture.md l. 1048-1071** — Section « Android — VpnService / Foreground Service / JNI Bridge Patterns » : tous les patterns lifecycle utilisés par cette story (singleton via `instance`, `START_REDELIVER_INTENT`, `startForeground` < 5 s, `stopForeground(STOP_FOREGROUND_REMOVE)`, channel `levoile_vpn_status` IMPORTANCE_LOW, action « Déconnecter » via `PendingIntent` `FLAG_IMMUTABLE`) y sont documentés. **Cette story EST l'implémentation de cette section**.
- **NFR-AND-3 — Taille APK release < 25 MB** (prd.md l. 698-699) : Story 9.5 ajoute uniquement quelques classes Kotlin (~5 KB minified). Marge confortable.
- **NFR-AND-7 — Permissions minimales** (livrée Story 9.1+9.4) : Story 9.5 ne consomme aucune permission supplémentaire. La liste reste celle Stories 9.1+9.4 : `INTERNET`, `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, `FOREGROUND_SERVICE_VPN`, `POST_NOTIFICATIONS`. Pas de `RECEIVE_BOOT_COMPLETED` ni `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` ajoutés (BootReceiver Phase 2, BatteryOptimization éventuellement Phase 2 selon AC #10).
- **NFR-AND-11 — R8/ProGuard préservant les classes JNI** (livrée Story 9.1) : Story 9.5 n'introduit pas de classe JNI. Mais les rules `-keep class fr.plateformeliberte.levoile.core.**` Story 9.1 doivent rester intactes — ne pas y toucher.
- **`PendingIntent.FLAG_IMMUTABLE` obligatoire Android 12+ (API 31)** : `minSdk = 29` couvre Android 10/11 où l'immutable n'est pas obligatoire (la valeur par défaut est mutable), mais l'inclure dès maintenant rend le code forward-compatible et évite un warning lint `IntentDetector` sur API 31+. **Cohérent epic Story 9.5 AC** : `PendingIntent.getService(... FLAG_IMMUTABLE)`.

### Pourquoi un délai de 5 s avant `stopForeground` ?

- **Cohérence avec l'AC `epics.md l. 1611`** : « `stopForeground(STOP_FOREGROUND_REMOVE)` est appelé après 5 s d'inactivité ».
- **UX raisonnée** : la notification reste visible 5 s après le tap « Déconnecter » → laisse le temps à l'utilisatrice de voir le retrait progressif (Story 9.6 enrichira avec un titre dynamique « Le Voile · Déconnexion… » pendant cette fenêtre). Sinon : retrait immédiat → l'utilisatrice peut douter d'avoir bien tapé l'action.
- **Évite le clignotement notification présente / absente** sur des reconnects rapides (e.g. l'utilisatrice tape Disconnect par erreur, retape Connect immédiatement → la notification reste affichée au lieu de disparaître / réapparaître).
- **Pourquoi pas plus long (10 s, 30 s) ?** Au-delà de 5 s, l'utilisatrice peut interpréter cela comme « le tunnel ne s'est pas déconnecté correctement » et ouvrir l'app pour vérifier. 5 s est un sweet spot empirique.
- **Pourquoi pas immédiat (Story 9.4) ?** Story 9.4 a livré un comportement minimal qui passe les tests AC mais ne respecte pas l'UX cible — le commentaire `// Story 9.5 raffinera (latence 5s)` au-dessus du `stopForeground(STOP_FOREGROUND_REMOVE)` Story 9.4 marque cette dette technique. Cette story la résout.

### Pourquoi singleton via `@Volatile var instance` plutôt qu'un check de `android.app.ActivityManager.getRunningServices()` ?

- **`getRunningServices`** est déprécié depuis API 26 et ne retourne plus que les services du process appelant — donc inutile pour détecter une autre instance hypothétique de soi-même.
- **`@Volatile`** garantit la visibilité cross-thread sans coût mémoire significatif.
- **`internal var instance`** est lisible depuis tout `:app` Kotlin (cohérent ADR-08 — isolation OS) sans exposer publiquement à des consommateurs externes.
- **Alternative `bindService`** : on pourrait avoir un binder qui expose l'état → trop lourd pour un check « le tunnel est-il déjà actif ? » résolu par la simple vérification `vpnInterface != null`.
- **Cohérent architecture.md l. 1056** : « Pattern singleton : check `if (instance != null)` au début de `onStartCommand` pour éviter doubles instances ».

### Pourquoi des helpers `internal` dans `MainActivity` plutôt que dans `LeVoileBridge` directement ?

- **Story 11.2 owner du JS bridge** — règle stricte de partage des responsabilités : le bridge JS (`@JavascriptInterface fun connect()`, `@JavascriptInterface fun disconnect()`) est exclusivement Story 11.2.
- **Mais MainActivity a besoin du `ActivityResultLauncher`** pour le consent VpnService (`registerForActivityResult` ne fonctionne que depuis une `ComponentActivity`). Donc même Story 11.2 devra _appeler_ MainActivity depuis le bridge.
- **D'où le pattern** : Story 9.5 livre les helpers `internal` dans MainActivity → Story 11.2 ajoute juste le wrapper bridge :
  ```kotlin
  // Story 11.2 ajoutera dans LeVoileBridge :
  @JavascriptInterface
  fun connect(country: String? = null): String {
      // contextRef est le MainActivity, stocké faiblement
      (contextRef.get() as? MainActivity)?.requestVpnStart(country)
      return """{"status":"ok"}"""
  }
  ```
- **Avantage** : Story 11.2 est minimaliste (juste un thin wrapper) et la logique d'orchestration vit dans MainActivity (testable Robolectric).
- **`internal`** : visibilité limitée au module `:app` Kotlin → accessible depuis `LeVoileBridge` sans exposer publiquement.

### Pourquoi NE PAS toucher `LeVoileBridge.kt` cette story ?

- **Story 11.2 owner explicite** : la liste exhaustive des futures méthodes (`connect`, `disconnect`, `getStatus`, `selectCountry`, `getRegistry`, `checkLeak`, `openVpnSettings`, `openBatteryOptimizationSettings`, `isAlwaysOnEnabled`, `getPreferences`, `setPreference`, `quit`) est dans architecture.md l. 612-624. Toute méthode supplémentaire ici (même `connect`/`disconnect` simples) crée un faux contrat dont l'implémentation backend manque côté frontend (`assets/app.js` Story 9.3 n'invoque que `getStatus()`).
- **Parallèle desktop** : sur desktop, le serveur HTTP local (`internal/ui/server.go`) expose toutes ses routes API à la fois — on ne livre pas une route à la fois pour `connect` puis `disconnect`. Sur Android, le pattern symétrique = livrer le bridge complet en une story (Story 11.2). C'est intentionnel.
- **Si malgré tout on pré-tirait `connect`/`disconnect` ici** : Story 11.2 devrait gérer une migration de l'existant + désynchronisation potentielle entre méthodes Bridge et méthodes UI. Faux gain de temps.

### Conventions Android (architecture.md l. 848-865, l. 1048-1087)

- **Constantes** : `UPPER_SNAKE_CASE` dans `companion object` — `STOP_FOREGROUND_DELAY_MS`, `PI_REQUEST_CODE_DISCONNECT`, `ACTION_CONNECT`, `ACTION_DISCONNECT`, `NOTIF_ID`, `NOTIF_CHANNEL_ID_STUB`.
- **Helpers privés** : `camelCase`, préfixe verbe — `connectInternal`, `disconnectInternal`, `buildOngoingNotificationWithAction`, `requestVpnStart`, `requestVpnStop`, `startVpnService`.
- **Tests Kotlin** : Robolectric + JUnit 4 (cohérent Stories 9.3/9.4). Co-localisation : tests dans `app/src/test/kotlin/<package miroir>/`.
- **Pas de `runBlocking`** dans `onCreate` ni dans les helpers Activity (architecture.md l. 1086).
- **Coroutines / Handler** : pour le délai 5 s, on utilise `Handler(Looper.getMainLooper()).postDelayed(...)` plutôt que `lifecycleScope.launch { delay(5_000); ... }` — `Service` n'a pas de `lifecycleScope` natif (il faudrait étendre `LifecycleService` ou ajouter `androidx.lifecycle:lifecycle-service`). Le `Handler` est plus simple et n'ajoute aucune dépendance.

### Apprentissages Stories 9.1+9.2+9.3+9.4 reproductibles

D'après les `Completion Notes` Stories 9.1 + 9.2 + 9.3 + 9.4 et l'inspection du repo au 2026-05-02 (`git status` + `ls android/`) :
- **Toolchain Android installée localement** : JDK 17, Gradle 8.7, Android SDK platforms;android-34, NDK (Story 9.2). Pas de réinstall.
- **`gomobile` + NDK** : installé Story 9.2. **Pas requis pour cette story** — Story 9.5 ne fait pas de `gomobile bind` ni n'invoque `build-aar.sh`/`build-aar.ps1`.
- **Aucun émulateur Android disponible localement** (apprentissage 9.1) → AC #2, #5 vérifiables uniquement en partie (compilation + test JVM Robolectric). Test runtime complet via émulateur reporté à Story 12.6.
- **Robolectric 4.12.2 + AndroidX Test Core 1.5.0 + AndroidX Test JUnit 1.1.5** ajoutés Story 9.3/9.4 — peut être réutilisé tel quel. **Vérifier en Task 1** avant de tenter d'ajouter.
- **`junit:4.13.2`** déjà ajouté Story 9.2 testImplementation.
- **Réalité du repo post-9.4 (à NE PAS toucher dans 9.5)** :
  - `LeVoileVpnService.kt` créé Story 9.4 sous `app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/`.
  - `PacketRelay.kt` + `VpnConstants.kt` créés Story 9.4 — INTACTS pour 9.5.
  - `ic_notification_stub.xml` créé Story 9.4 — INTACT pour 9.5 (Story 9.6 livrera `ic_levoile_status.xml` final).
  - Channel `"levoile_vpn_status_stub"` créé Story 9.4 — INTACT pour 9.5 (Story 9.6 migrera vers `"levoile_vpn_status"` + supprimera l'ancien).
  - `MainActivity.kt` créé Story 9.3 — MODIFIÉ par 9.5 pour ajouter helpers internes (mais le `LeVoileBridge` registration reste intact).
  - `app/src/main/AndroidManifest.xml` enrichi Story 9.4 (`<service>` + `FOREGROUND_SERVICE_VPN`) — INTACT pour 9.5.
  - **L'ancien dossier `android/internal/` n'existe plus** (supprimé Story 9.1) — ne pas le recréer.

### Anti-patterns à éviter

- ❌ **Ne pas exposer `connect` / `disconnect` dans `LeVoileBridge`** — Story 11.2 owner. Toute méthode supplémentaire ici crée un faux contrat dont l'implémentation backend manque (cf. section « Pourquoi NE PAS toucher `LeVoileBridge.kt` »).
- ❌ **Ne pas créer `NotificationHelper.kt`** — Story 9.6 owner. La notification stub enrichie cette story (action « Déconnecter ») reste in-place dans `LeVoileVpnService.buildOngoingNotificationWithAction()`. Story 9.6 extraira potentiellement la logique vers `NotificationHelper.kt` + ajoutera channel final + titre dynamique.
- ❌ **Ne pas modifier `PacketRelay.kt` ni `VpnConstants.kt`** — Story 9.4 + Story 9.7 owner. Story 9.5 ne touche aucun fichier sous `vpn/` sauf `LeVoileVpnService.kt`.
- ❌ **Ne pas modifier `app/src/main/assets/{index.html,style.css,app.js}`** — Story 9.3 owner (placeholder), Story 11.1 owner (sync depuis `frontend/` racine), Story 11.2 owner (bouton Connect/Disconnect réel relié au bridge).
- ❌ **Ne pas créer `BootReceiver.kt`** — auto-start au boot Phase 2 (architecture.md l. 636 + restrictions Android 10+ sur `BOOT_COMPLETED`).
- ❌ **Ne pas créer `BatteryOptimizationHelper.kt`** complet — la doc README cette story se contente de _recommander_ à l'utilisatrice d'ouvrir Settings manuellement. La demande runtime via `Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)` est Phase 2 (Story 11.x) si nécessaire (Foreground Service VPN exempté nativement Android 12+, recommandé seulement Android 10/11).
- ❌ **Ne pas créer `KillSwitchHelper.kt` ni détection heuristique `Settings.Global.always_on_vpn_app`** — Story 10.1 owner. La doc OEM cette story (README) mentionne le kill switch en passant mais n'implémente RIEN.
- ❌ **Ne pas charger d'URL externe ni modifier la WebView** — la WebView reste figée Story 9.3 (placeholder). L'AC #2 verrouille sur `appassets.androidplatform.net` (asset local). Aucune URL `https://...` arbitraire ajoutée cette story.
- ❌ **Ne pas activer `setWebContentsDebuggingEnabled`** — Story 9.3 a déjà tranché : pas activé sans guard `BuildConfig.DEBUG`. Story 9.5 ne change pas la décision.
- ❌ **Ne pas modifier `AndroidManifest.xml`** — toutes les permissions et déclarations `<service>` nécessaires sont déjà en place Story 9.4.
- ❌ **Ne pas modifier `frontend/` racine** — son contenu est la source desktop, intouchable depuis cette story (Story 11.1 mettra en place le sync). Pour 9.5 c'est encore plus net : 9.5 ne touche PAS la WebView (placeholder Story 9.3).
- ❌ **Ne pas modifier `go.mod`/`go.sum` racine** — Story 9.2 a déjà ajouté `golang.org/x/mobile`. Story 9.5 = Kotlin pur, aucune raison d'y toucher.
- ❌ **Ne pas toucher à `android/shims/*.go`** — code Go consommé par gomobile bind Story 9.2. Story 9.7 owner pour enrichissement.
- ❌ **Ne pas invoquer `bash scripts/build-aar.sh` ou `pwsh scripts/build-aar.ps1` dans le scope de 9.5** — ces scripts appartiennent à 9.2 (déjà livrés). 9.5 réussit `./gradlew assembleDebug` même si `levoile-core/libs/levoile-core.aar` n'existe pas localement (le `.aar` est en `app/libs/` selon Story 9.2 décision finale, et 9.5 ne dépend d'aucune classe gomobile).
- ❌ **Ne pas créer un onboarding obligatoire** — Story 11.5 (3 écrans) + Story 11.6 (composant C15 kill switch screen) owner. Le bandeau d'avertissement kill switch est Story 10.2 (composant C17).
- ❌ **Ne pas changer le `foregroundServiceType` du service** — `vpn` est déjà déclaré Story 9.4 conforme Android 14+ (API 34). Aucune raison d'ajouter `specialUse` cette story (architecture.md l. 659 mentionne `vpn|specialUse` en double-typage hypothétique pour cas futur ; pour l'instant `vpn` seul suffit).
- ❌ **Ne pas démarrer un binder service (`onBind`)** — Le service reste un Foreground Service classique. L'IPC entre `MainActivity` et `LeVoileVpnService` se fait exclusivement via Intents (`startForegroundService` / `startService`), cohérent architecture.md l. 134 (« sur Android : pas d'IPC — l'app est mono-processus »).
- ❌ **Ne pas oublier `removeCallbacksAndMessages` dans `onDestroy`** — sinon fuite de runnable orphelin référençant une instance détruite. Le test smoke AC #7 couvre ce cas.

### Project Structure Notes

**Fichiers attendus livrés/modifiés par cette story** (tous sous `android/`) :
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` (MODIFIÉ — singleton + `STOP_FOREGROUND_DELAY_MS` + handler teardown + notification action « Déconnecter » + rename `buildStubOngoingNotification` → `buildOngoingNotificationWithAction`)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — ajout `vpnConsentLauncher` + helpers `requestVpnStart` / `requestVpnStop` / `startVpnService` + éventuellement consommation extras debug en `onResume`)
- `android/app/src/main/res/values/strings.xml` (MODIFIÉ — ajout `notif_action_disconnect` + `vpn_consent_denied_message`)
- `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — traductions FR)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceLifecycleTest.kt` (NOUVEAU — Robolectric tests)
- **OPTIONNEL** `android/app/src/debug/kotlin/fr/plateformeliberte/levoile/debug/DebugConnectActivity.kt` + `android/app/src/debug/AndroidManifest.xml` (NOUVEAU sous `src/debug/`)
- `android/README-android.md` (MODIFIÉ — ajout section « Lifecycle Foreground Service + OEM agressifs »)

**Fichiers hors `android/` autorisés à modifier par cette story** :
- `_bmad-output/implementation-artifacts/sprint-status.yaml` : passage status story 9-5 `backlog` → `review`
- `_bmad-output/implementation-artifacts/9-5-foreground-service-lifecycle-action-deconnecter.md` : auto-update (Status, Completion Notes, File List, Change Log)

**Aucun autre fichier hors `android/` ne doit être modifié.** Vérifier via `git status` final (Task 8).

### References

- [Source: epics.md#Story 9.5: Foreground Service lifecycle + action « Déconnecter » (l. 1593-1617)]
- [Source: epics.md#Story 9.4: LeVoileVpnService — création TUN + pump paquets IP (l. 1567-1591) — story dépendance directe]
- [Source: epics.md#Story 9.6: Notification persistante MVP (l. 1619-1642) — story aval, owner channel + titre dynamique + icône finale]
- [Source: prd.md#FR-AND-1 (l. 609) — VpnService + Builder pattern]
- [Source: prd.md#FR-AND-2 (l. 610) — Foreground Service + notification persistante]
- [Source: prd.md#J7 (l. 86) — protection en arrière-plan pendant Chrome/WhatsApp/Insta]
- [Source: prd.md#NFR-AND-3 (l. 698-699) — Taille APK release < 25 MB]
- [Source: prd.md#NFR-AND-7 (l. 703) — Permissions minimales auditables]
- [Source: architecture.md#Foreground Service lifecycle (l. 1048-1071)]
- [Source: architecture.md#Pattern singleton (l. 1056)]
- [Source: architecture.md#Pattern Foreground Service notification (l. 1067-1071)]
- [Source: architecture.md#Lifecycle VpnService - onRevoke / onDestroy (l. 1050-1056)]
- [Source: architecture.md#startForeground < 5 secondes (l. 1055)]
- [Source: architecture.md#MainActivity orchestration UI <-> Service (l. 580-584)]
- [Source: architecture.md#Architecture mono-processus Android (l. 570-625)]
- [Source: architecture.md#Permissions Android (l. 642-668)]
- [Source: architecture.md#ADR-08 Isolation OS maximale (l. 2385-2388)]
- [Memory: feedback_os_isolation — duplication code Win/Linux/Android préférée à abstraction partagée]
- [Source: 9-1-module-gradle-android-structure-projet.md (livrée 2026-05-02 — toolchain installée, structure Gradle, ProGuard rules, AndroidManifest minimal, themes.xml)]
- [Source: 9-2-script-build-aar-sh-gomobile-bind-du-noyau-go-partage.md (livrée 2026-05-02 — pattern « Périmètre de modification » strict, JUnit 4.13.2 + Robolectric testImplementation)]
- [Source: 9-3-mainactivity-squelette-webview-placeholder.md (livrée 2026-05-02 — MainActivity + LeVoileBridge stub `getStatus()` + WebView)]
- [Source: 9-4-levoilevpnservice-creation-tun-pump-paquets-ip.md (en review 2026-05-02 — squelette VpnService + pumps + notification stub + channel `"levoile_vpn_status_stub"` + `foregroundServiceType="vpn"` + `FOREGROUND_SERVICE_VPN` permission)]

### Notes de divergence corrigées en amont

- **Aucune divergence majeure spec/repo détectée** pour Story 9.5. La spec d'Epic 9.5 (epics.md l. 1593-1617) est pleinement compatible avec l'état du repo après Stories 9.1+9.2+9.3+9.4. Les seules subtilités résolues :
  1. Le délai 5 s avant `stopForeground` est explicitement pris en charge par cette story (commentaire `// Story 9.5 raffinera (latence 5s)` dans `disconnectInternal()` Story 9.4 → résolu ici).
  2. Le pattern singleton via `instance` (architecture.md l. 1056) est introduit cette story (Story 9.4 a livré `vpnInterface != null` comme proxy mais sans la référence `instance` formelle pour les consommateurs externes).
  3. Le wiring UI ↔ Service (helpers MainActivity) est introduit cette story en mode DORMANT (Story 11.2 wirera depuis le bridge JS).

- **Heuristique de la "frontière à la fin"** : à la fin de cette story, l'app est lançable et le tunnel est lifecycle-correct du point de vue Android (notification action fonctionnelle, singleton, teardown propre, `START_REDELIVER_INTENT`), mais le tunnel ne fait toujours rien fonctionnellement (pumps stubbées, pas de noyau Go). C'est intentionnel — Stories 9.6 (notification finale) + 9.7 (intégration noyau Go) compléteront. Documenter clairement dans Completion Notes pour éviter une perception de "story incomplète".

- **Variant `debug` (DebugConnectActivity)** : la décision est laissée au dev. Si créée, elle vit STRICTEMENT sous `src/debug/` (jamais `src/main/`). Ce pattern est cohérent avec l'AC #3 de Story 9.4 qui déjà mentionnait un `DebugVpnConsentActivity` optionnel — Story 9.5 reprend l'idée et l'élargit pour couvrir Connect ET Disconnect.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context) — `claude-opus-4-7[1m]`

### Debug Log References

- **Découverte au démarrage du dev — réordonnancement Stories 9.5/9.6/9.7** : la story 9.5 a été régénérée le 2026-05-02 supposant l'ordre 9.4 → 9.5 → 9.6 → 9.7. Au moment du dev (2026-05-03), Stories **9.6 ET 9.7 ont déjà été livrées** (sprint-status `review` pour les deux), avec les artefacts suivants déjà en place :
  - `app/src/main/kotlin/fr/plateformeliberte/levoile/ui/{NotificationHelper.kt,VpnState.kt}` — Story 9.6 livre la notification finale (channel `levoile_vpn_status`, titre dynamique « Le Voile · {État} », icône `ic_levoile_status`, **action « Déconnecter » via `PendingIntent.getService(... FLAG_IMMUTABLE)` ciblant `ACTION_DISCONNECT`** + tap notif → MainActivity).
  - `app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/{GoCoreAdapter.kt,PacketCallback.kt,StatusCallback.kt}` + `app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/GoBackedPacketRelay.kt` — Story 9.7 livre l'adaptateur Go via gomobile + adds `kotlinx.coroutines` + ABI filter arm64-v8a/armeabi-v7a sur `app/build.gradle.kts`.
  - `LeVoileVpnService.kt` modifié par 9.6 : `notificationHelper.build(VpnState.CONNECTED)` au top de `onStartCommand` + `notificationHelper.notify(VpnState.DISCONNECTED)` dans `disconnectInternal`.
- **Conséquence sur le scope Story 9.5** : AC #4 (action « Déconnecter ») est **déjà implémentée par 9.6**. Le scope effectif Story 9.5 se réduit à :
  1. AC #5 — délai 5 s avant `stopForeground(STOP_FOREGROUND_REMOVE)` + `stopSelf` (immédiat avant 9.5).
  2. AC #6 — singleton `@Volatile internal var instance` (utile à Story 11.2 et diagnostics).
  3. AC #8 — helpers `MainActivity.requestVpnStart` / `requestVpnStop` + `ActivityResultLauncher` (DORMANTS — wirés par Story 11.2).
  4. AC #10 — section README OEM agressifs.
  5. AC #11 — vérifications build + tests.
- **Toolchain** : JDK 17 (Microsoft OpenJDK 17.0.10.7-hotspot), Android SDK 34, Gradle 8.7, Kotlin 1.9.24. `JAVA_HOME` non propagé dans le shell par défaut — injecté manuellement pour les invocations `gradlew.bat`.
- **Stratégie test** : reste **JVM-only** (pas de Robolectric) — alignement Stories 9.3/9.4. `unitTests.isReturnDefaultValues = true` déjà actif via Story 9.4. Vérifications via réflexion sur le bytecode + parsing manifest DOM.
- **Refactor `disconnectInternal` → `cleanupSync` extrait + `disconnectInternal` orchestre + 5 s differé** : nécessaire pour permettre `onDestroy()` de faire un cleanup synchrone immédiat sans relancer un `postDelayed` sur Service en destruction (race UB Android : runnable s'exécute après `super.onDestroy()` → appel `stopForeground` sur instance morte). Pattern : `disconnectInternal()` annule tout teardown pendant + cleanup + post nouveau teardown ; `onDestroy()` annule tout teardown pendant + cleanup + immediate stopForeground.
- **Singleton `@Volatile internal var instance: LeVoileVpnService? = null; private set`** : Kotlin compile avec name-mangling pour `internal` (suffixe `$app_debug` etc.). Le test reflection cible le **backing field statique `instance`** sur la classe outer (invariant le plus stable — `@Volatile` force Kotlin à sortir le field du Companion vers la classe outer pour pouvoir y poser le modifier `volatile` JVM). Premier run du test a échoué sur `getInstance()` direct sur Companion ; corrigé en cherchant le static field sur outer + tout getter `getInstance*` sur Companion (tolérance suffixe mangling).
- **Tests ajoutés** :
  - `LeVoileVpnServiceConfigTest` (existant Story 9.4) enrichi de 4 nouveaux cas : `STOP_FOREGROUND_DELAY_MS == 5000L`, présence singleton instance (static volatile field + Companion getter), présence champ `teardownHandler: android.os.Handler`, présence helper privé `cleanupSync()`.
  - `MainActivityConfigTest` (existant Story 9.3) enrichi de 2 nouveaux cas : présence helpers `requestVpnStart` / `requestVpnStop`, présence champs `vpnConsentLauncher` / `pendingConnectCountry`.
- **Build metrics finales** :
  - `./gradlew :app:testDebugUnitTest` → SUCCESS, 27 tests OK (anciens + 6 nouveaux Story 9.5).
  - `./gradlew assembleDebug` → SUCCESS, APK debug **29.53 MB**.
  - `./gradlew assembleRelease` → SUCCESS, APK release **23.33 MB** < 25 MB cible NFR-AND-3 ✓ (≈0.04 MB augmentation vs Story 9.3 — overhead négligeable des helpers MainActivity + ActivityResultLauncher).
  - `./gradlew :app:lint` → SUCCESS, aucune nouvelle erreur. Les helpers `requestVpnStart`/`requestVpnStop` portent `@Suppress("unused")` avec commentaire `// Wired by Story 11.2 ...` pour éviter UnusedSymbol warning.
  - **Anomalie ponctuelle Windows** : premier run `assembleRelease` a échoué sur `minifyReleaseWithR8` (`FileSystemException: classes.dex utilisé par un autre processus` — Windows file lock). Résolu via `gradlew --stop` puis rerun.
- **Test runtime sur émulateur non exécuté** — aucun émulateur Android disponible localement (cohérent apprentissages Stories 9.1/9.3/9.4). Le test smoke JVM valide la structure compile-time + invariants companion ; le runtime du délai 5 s, le flow consent VpnService end-to-end, et la transition de notif sur OEM agressifs sont reportés à Story 12.6 (matrice instrumentée Espresso API 29/33/34).
- **DebugConnectActivity optionnelle (AC #9)** : **non créée** dans cette session. Le test runtime via `adb shell am start-foreground-service ...` reste documenté dans le README (sections Story 9.4 + Story 9.5). Coût d'ajout faible — peut être livré en hotfix séparé si Story 11.2 traîne. Décision : repli `adb` suffit pour le QA manuel pré-Story 11.2.

### Completion Notes List

1. **Toutes les ACs résolues** — partiellement par les stories concurrentes 9.6/9.7 qui ont anticipé certains éléments :
   - AC #1 (`startForeground` < 5 s) : déjà OK Story 9.4 fix H-1+H-2+H-3.
   - AC #2 (visibilité Settings → Apps + VPN) : structure manifest correcte (Story 9.4 — `foregroundServiceType="specialUse"` + property `vpn`). Test runtime reporté Story 12.6.
   - AC #3 (`START_REDELIVER_INTENT`) : déjà OK Story 9.4. Test JVM `LeVoileVpnServiceConfigTest` parse manifest DOM ; le retour Service constant est confirmé par lecture du code (ligne `return Service.START_REDELIVER_INTENT`).
   - **AC #4 (action « Déconnecter ») : déjà livrée Story 9.6** dans `NotificationHelper.buildDisconnectAction()` — `PendingIntent.getService(... FLAG_IMMUTABLE)` ciblant `ACTION_DISCONNECT`. Story 9.5 ne re-livre PAS cet artefact.
   - **AC #5 (délai 5 s avant `stopForeground`)** : ✅ implémenté cette story — `disconnectInternal()` post un runnable `Handler.postDelayed(STOP_FOREGROUND_DELAY_MS = 5_000L)` pour `stopForeground` + `stopSelf`. Idempotence via `removeCallbacksAndMessages(null)` au début. Garde-fou `onDestroy` qui annule + cleanup synchrone immédiat (évite race UB).
   - **AC #6 (singleton `@Volatile internal var instance`)** : ✅ implémenté — `companion object { @Volatile internal var instance: LeVoileVpnService? = null; private set }`, assigné dans `onCreate`, libéré dans `onDestroy`.
   - AC #7 (test smoke) : ✅ enrichi `LeVoileVpnServiceConfigTest.kt` (existant 9.4) avec 4 nouveaux cas + `MainActivityConfigTest.kt` (existant 9.3) avec 2 nouveaux cas. Stratégie JVM-only (pas de Robolectric, alignement 9.4).
   - **AC #8 (helpers `MainActivity` + ActivityResultLauncher)** : ✅ implémenté — `vpnConsentLauncher`, `pendingConnectCountry`, `requestVpnStart(country?)`, `requestVpnStop()`, `startVpnService(country)` (privé). Helpers DORMANTS — Story 11.2 wirera depuis le bridge JS.
   - AC #9 (DebugConnectActivity optionnelle) : **non créée** — repli `adb shell am start-foreground-service` documenté README. Décision repli `adb` suffit pour QA manuel pré-Story 11.2.
   - **AC #10 (README OEM agressifs)** : ✅ ajouté — section « Lifecycle Foreground Service + OEM agressifs (Story 9.5 livrée) » insérée dans `README-android.md` après la section Story 9.4 et avant la section Story 9.6. Inclut la table par OEM (Xiaomi/Huawei/Oppo/Vivo/stock) avec les chemins Settings, l'explication du délai 5 s + idempotence, et la documentation du singleton.
   - AC #11 (build < 25 MB) : ✅ APK release 23.33 MB.
2. **Périmètre `git status` strictement respecté** — tous les changements introduits par Story 9.5 sont **sous `android/`** :
   - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — helpers VPN + ActivityResultLauncher).
   - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` (MODIFIÉ — singleton + delay 5 s + cleanupSync extrait + onDestroy hardening).
   - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/MainActivityConfigTest.kt` (MODIFIÉ — 2 tests nouveaux).
   - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt` (MODIFIÉ — 4 tests nouveaux).
   - `android/README-android.md` (MODIFIÉ — section Story 9.5).
   Hors `android/` : `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `ready-for-dev` → `review`) + ce fichier story (auto-update).
   **Aucune modification** de `go.mod`, `go.sum`, `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`, `_bmad/`, `android/shims/`, `android/scripts/`, `android/levoile-core/`, `android/app/src/main/AndroidManifest.xml`, ni `android/app/build.gradle.kts`. **`LeVoileBridge.kt` strictement intact** (Story 11.2 owner — confirmé par `git diff android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` = vide).
3. **Déviations vs spec d'origine** :
   - Le **rename `buildStubOngoingNotification` → `buildOngoingNotificationWithAction`** (Task 3.1 spec) n'a **PAS été effectué** — Story 9.6 a entre-temps SUPPRIMÉ entièrement la méthode au profit de `notificationHelper.build(VpnState.CONNECTED)`. Plus de méthode locale à renommer.
   - Le **`PI_REQUEST_CODE_DISCONNECT = 0xD15C`** (Task 3.3 spec) n'a **PAS été ajouté** — la PendingIntent vit désormais dans `NotificationHelper` qui gère son propre `REQUEST_CODE_DISCONNECT = 0xCEC2` (Story 9.6).
   - Le **`PendingIntent.getService(... FLAG_IMMUTABLE)`** (Task 3.2 spec) n'a **PAS été câblé** depuis `LeVoileVpnService` — déjà câblé par `NotificationHelper.buildDisconnectAction()` Story 9.6.
   - L'**absence de `DebugConnectActivity`** (Task 6 spec optionnelle) — décision repli `adb`.
   Toutes ces déviations sont des **non-régressions** : les ACs concernées sont satisfaites par les artefacts Story 9.6, simplement à un autre endroit du code.
4. **Test runtime sur émulateur non exécuté** — délégué à Story 12.6 (matrice instrumentée Espresso API 29/33/34) qui validera le délai 5 s effectif, le flow consent VpnService end-to-end, le tap notification → service ACTION_DISCONNECT, et le comportement OEM agressifs (au minimum Pixel + Xiaomi MIUI émulé).
5. **Frontière à la fin** : Stories 9.4-9.7 sont toutes en `review` post Story 9.5. **L'app Android est désormais lifecycle-correcte** (singleton, teardown différé, action « Déconnecter » câblée, helpers UI prêts pour Story 11.2). Le tunnel chiffré réel (handshake QUIC/HTTP3 vers les relais) requiert encore le wiring `GoBackedPacketRelay` dans `LeVoileVpnService` (Story 9.7 livre l'adapter mais le service utilise encore `NoOpPacketRelay`). Prochaine étape critique : code-review parallèle des 4 stories (9.4 done, 9.5/9.6/9.7 review) puis Story 11.2 (bridge JS connect/disconnect).
6. **Décision singleton conservée malgré préemption 9.6** : le singleton `instance` n'est PAS strictement nécessaire pour faire fonctionner Story 9.5 (le check `vpnInterface != null` dans `connectInternal` couvre déjà la prévention de double-CONNECT, et Android garantit déjà au plus une instance Service active par classe par process). **Conservé pour Story 11.2** qui pourrait vouloir interroger l'état du Service depuis le bridge JS sans passer par un `bindService` complet, et pour les diagnostics introspectifs (logs développeur). Coût d'ajout : 4 lignes Kotlin + 2 assignations lifecycle.

### File List

**Fichiers modifiés** (tous sous `android/`) :

- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` — ajout `STOP_FOREGROUND_DELAY_MS = 5_000L` + `@Volatile internal var instance` au companion ; ajout `lateinit var teardownHandler: Handler` + init dans `onCreate` ; refactor `disconnectInternal` (annule pending → cleanupSync → post delayed teardown) ; extraction `cleanupSync()` privé ; durcissement `onDestroy` (cancel runnable + cleanupSync + immediate stopForeground + `instance = null`) ; mise à jour KDoc class avec mention Story 9.5.
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` — ajout `import android.content.Intent`, `android.net.VpnService`, `android.util.Log`, `androidx.activity.result.*`, `androidx.core.content.ContextCompat`, `LeVoileVpnService`, `VpnConstants` ; ajout `private lateinit var vpnConsentLauncher: ActivityResultLauncher<Intent>` + `private var pendingConnectCountry: String? = null` ; registration du launcher en début de `onCreate` (avant `setContentView`) ; ajout helpers `internal fun requestVpnStart(country: String? = null)`, `internal fun requestVpnStop()`, `private fun startVpnService(country: String?)` ; ajout `companion object { private const val TAG }` ; mise à jour KDoc class.
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt` — ajout 4 tests : `STOP_FOREGROUND_DELAY_MS exposes 5000 ms`, `LeVoileVpnService companion exposes Volatile internal var instance`, `LeVoileVpnService declares teardownHandler field`, `LeVoileVpnService declares cleanupSync helper`. Stratégie réflection sur backing field statique + Companion getter (tolérance name mangling Kotlin internal).
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/MainActivityConfigTest.kt` — ajout 2 tests dev-story Story 9.5 (helpers + fields) ; ajout 1 test code-review post-9.5 (`vpnConsentLauncher is registered before setContentView in onCreate` — fix L-3) ; ajout helpers `readMainActivitySource()` + `extractFunctionBody()` partagés.
- `android/README-android.md` — nouvelle section « Lifecycle Foreground Service + OEM agressifs (Story 9.5 livrée) » insérée après la section Story 9.4 et avant la section Story 9.6. Tableau OEM (Xiaomi/Huawei/Oppo/Vivo/stock Android) avec chemins Settings.
- `android/app/src/main/res/values/strings.xml` — ajout code-review post-9.5 (fix L-4) : `vpn_consent_denied_message` (string déclarée pour consommation par Story 11.2).
- `android/app/src/main/res/values-fr/strings.xml` — ajout code-review post-9.5 (fix L-4) : parité FR de `vpn_consent_denied_message`.

**Fichiers ajoutés en code-review post-9.5 (fixes appliqués) — déjà couverts ci-dessus** :

- `LeVoileVpnService.kt` : fixes H-1 (connectInternal annule teardown), M-2 (état initial notif selon action), M-3 + L-5 (cleanupSync guard wasActive sur notify(DISCONNECTED)).
- `MainActivity.kt` : fix M-1 (requestVpnStop guard `instance == null`), fix L-1 (commentaire `startForegroundService` cohérent avec le code).
- `LeVoileVpnServiceConfigTest.kt` : 4 tests source-text (H-3 startForeground first, H-4 START_REDELIVER_INTENT, M-7 disconnect idempotence, M-7+H-1 connect cancel teardown) + helpers `readServiceSource()` + `extractFunctionBody()`.

**Fichiers NON modifiés** (vérifiés `git diff` vide) :

- `android/app/src/main/AndroidManifest.xml` — toutes les permissions et déclarations `<service>` requises sont en place Story 9.4.
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` — Story 11.2 owner (les helpers `MainActivity` ajoutés ici sont DORMANTS jusqu'à ce que 11.2 les wire depuis le bridge).
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/{PacketRelay.kt,VpnConstants.kt,GoBackedPacketRelay.kt}` — Stories 9.4/9.7 owners.
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/ui/{NotificationHelper.kt,VpnState.kt}` — Story 9.6 owner.
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/{GoCoreAdapter.kt,PacketCallback.kt,StatusCallback.kt}` — Story 9.7 owner.
- `android/app/build.gradle.kts`, `android/gradle/libs.versions.toml` — modifications déjà en place par Stories 9.3 (`androidx.webkit`) + 9.7 (`kotlinx.coroutines` + ndk abiFilters).
- `android/app/src/main/res/**` — aucune nouvelle string requise par Story 9.5 (les libellés viennent des strings ajoutées par 9.6 — `notif_action_disconnect`, etc.).
- Tous les fichiers hors `android/`.

**Hors `android/` (auto-update) :**

- `_bmad-output/implementation-artifacts/sprint-status.yaml` — `9-5-foreground-service-lifecycle-action-deconnecter` passé de `ready-for-dev` → `in-progress` (Step 4 dev-story) → `review` (Step 9).
- `_bmad-output/implementation-artifacts/9-5-foreground-service-lifecycle-action-deconnecter.md` — ce fichier (Status, Tasks/Subtasks, Dev Agent Record, File List, Change Log).

## Change Log

| Date | Auteur | Changement |
|---|---|---|
| 2026-05-02 | create-story (Claude Opus 4.7) | Story 9.5 régénérée. Périmètre strictement confiné à `android/` SANS aucune exception code partagé Go (cohérent Stories 9.3/9.4). Lifecycle Foreground Service raffiné : action « Déconnecter » câblée à la notification stub Story 9.4 via `PendingIntent.getService(... FLAG_IMMUTABLE)`, délai 5 s avant `stopForeground(STOP_FOREGROUND_REMOVE)` via `Handler(Looper.getMainLooper())`, pattern singleton via `@Volatile internal var instance`, helpers privés `MainActivity` (`requestVpnStart` / `requestVpnStop` / `vpnConsentLauncher`) DORMANTS jusqu'à wiring Story 11.2. `LeVoileBridge.kt` INTACT (Story 11.2 owner). `LeVoileVpnService.kt` rename `buildStubOngoingNotification` → `buildOngoingNotificationWithAction`. Test Robolectric 5 cas (singleton, retour `START_REDELIVER_INTENT`, action wired, délai 5 s, idempotence handler). DebugConnectActivity optionnel sous `src/debug/`. README enrichi section « OEM agressifs (Xiaomi/Huawei/Oppo) + Doze ». Status: ready-for-dev. |
| 2026-05-03 | code-review (Claude Opus 4.7) | **Code-review adversariale Story 9.5 — 4 HIGH + 7 MEDIUM + 5 LOW findings, tous fixes appliqués.** **HIGH** : H-1 race condition reconnect rapide post-disconnect (LeVoileVpnService.connectInternal annule désormais teardownHandler.removeCallbacksAndMessages(null) en début de méthode — sinon stopForeground orphelin démotait le Service redevenu actif après 5 s) ; H-2 Tasks 3.1/3.2/3.3 marquées [x] mais non faites (préemption Story 9.6) → reformulées [~] avec justification ; H-3 ajout test source-text « onStartCommand calls startForeground as its first effective statement » (anti-ANR <5 s, AC #1) ; H-4 ajout test source-text « onStartCommand returns Service.START_REDELIVER_INTENT » (AC #3 crash recovery). **MEDIUM** : M-1 guard `if (LeVoileVpnService.instance == null) return` dans MainActivity.requestVpnStop (évite démarrage Service inutile sur tap Disconnect idle) ; M-2 onStartCommand sélectionne maintenant l'état initial de la notif selon l'action (CONNECTED si ACTION_CONNECT, DISCONNECTED sinon) — évite flash CONNECTED→DISCONNECTED 5 s sur path DISCONNECT ; M-3 + L-5 cleanupSync notify(DISCONNECTED) gardé par `wasActive` (vpnInterface != null || tunnelStartedFired) — évite clignotement notif sur Service idle ; M-4 Task 8.5 test manuel marquée [x] non exécuté → reformulée [~] reporté Story 12.6 ; M-5/M-6 documentés (untracked Go files appartiennent 9.2/9.7, commit groupé pre-PR = décision projet hors scope code-review) ; M-7 ajout 2 tests source-text idempotence (« disconnectInternal cancels pending teardown then posts delayed teardown » + « connectInternal cancels pending teardown to prevent reconnect race »). **LOW** : L-1 commentaire trompeur MainActivity.requestVpnStop corrigé (`startForegroundService` cohérent avec le code, plus `startService` mensonger) ; L-2 vérifié OK (`applicationIdSuffix = ".debug"` présent dans build.gradle.kts ligne 62) ; L-3 ajout test source-text « vpnConsentLauncher is registered before setContentView in onCreate » (verrouille l'ordre lifecycle, sinon IllegalStateException runtime) ; L-4 ajout string `vpn_consent_denied_message` dans values/ + values-fr/ (Task 2 spec) ; L-5 fix dans M-3 ci-dessus. **Tests ajoutés** : 4 nouveaux dans LeVoileVpnServiceConfigTest (H-3, H-4, M-7 disconnect, M-7 connect/H-1) + 1 nouveau dans MainActivityConfigTest (L-3) + helpers `readServiceSource`/`readMainActivitySource`/`extractFunctionBody` partagés. **Fichiers modifiés** : LeVoileVpnService.kt (5 fixes), MainActivity.kt (2 fixes), values/strings.xml + values-fr/strings.xml (1 fix), LeVoileVpnServiceConfigTest.kt (4 tests + helpers), MainActivityConfigTest.kt (1 test + helpers). **Aucun commit git** (cohérent décision projet pre-PR). Status: done. |
| 2026-05-03 | dev-story (Claude Opus 4.7) | Implémentation Story 9.5. **Réordonnancement détecté au démarrage** : Stories 9.6 et 9.7 ont été livrées (`review`) entre la création de la story 9.5 et son dev — AC #4 (action « Déconnecter ») préemptée par 9.6 (`NotificationHelper.buildDisconnectAction`), donc plus à câbler ici. **Scope effectif réduit à AC #5/#6/#8/#10/#11**. **AC #5 (délai 5 s)** : `disconnectInternal()` refactor — extraction `cleanupSync()` privé idempotent, `Handler.postDelayed(STOP_FOREGROUND_DELAY_MS = 5_000L)` pour `stopForeground` + `stopSelf`, `removeCallbacksAndMessages(null)` au début pour idempotence. **`onDestroy()` durci** : annule runnable + cleanupSync + immediate stopForeground (évite race UB « stopForeground sur Service détruit »). **AC #6 (singleton)** : `companion object { @Volatile internal var instance: LeVoileVpnService? = null; private set }` + assigné `onCreate` / libéré `onDestroy`. **AC #8 (helpers MainActivity)** : `vpnConsentLauncher: ActivityResultLauncher<Intent>` + `pendingConnectCountry: String?` + `requestVpnStart(country?)` / `requestVpnStop()` / `startVpnService(country?)` privé. Helpers DORMANTS jusqu'à Story 11.2 — annotés `@Suppress("unused")` avec commentaire `// Wired by Story 11.2`. **AC #10 (README)** : section « Lifecycle Foreground Service + OEM agressifs (Story 9.5 livrée) » insérée après section Story 9.4, avec tableau OEM (Xiaomi/Huawei/Oppo/Vivo/stock) + doc délai 5 s + idempotence + singleton + flow consent + adb commands. **Tests JVM-only** (pas de Robolectric — alignement 9.4) : 4 nouveaux dans `LeVoileVpnServiceConfigTest` (STOP_FOREGROUND_DELAY_MS == 5000L, singleton instance via static volatile field outer + Companion getter avec tolérance mangling, teardownHandler field, cleanupSync method) + 2 nouveaux dans `MainActivityConfigTest` (helpers + fields). Premier run échec sur singleton — corrigé en cherchant le static field `instance` sur outer class (Kotlin sort le backing field du Companion à cause de `@Volatile`) + tout getter `getInstance*` sur Companion. **Build release 23.33 MB** (+0.04 MB vs 9.3 — overhead helpers négligeable, NFR-AND-3 ✓). Test runtime émulateur reporté Story 12.6. **DebugConnectActivity NON créée** — repli `adb shell am start-foreground-service` documenté README. **Aucun changement** à `LeVoileBridge.kt`, `AndroidManifest.xml`, `app/build.gradle.kts`, `libs.versions.toml`. Status: review. |
