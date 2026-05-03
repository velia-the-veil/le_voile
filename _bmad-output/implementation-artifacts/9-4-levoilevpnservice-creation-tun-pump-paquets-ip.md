# Story 9.4: `LeVoileVpnService` — création TUN + pump paquets IP

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Aucune exception code partagé n'est nécessaire pour cette story.** Story 9.4 livre un squelette `VpnService` Android pur (Kotlin + AndroidManifest + ressources Android) qui crée l'interface TUN via `VpnService.Builder.establish()` et démarre les threads de pump paquets IP. **Le pont vers le noyau Go partagé (lecture/écriture des paquets via `GoCoreAdapter` ↔ gomobile `.aar`) est porté par Story 9.7** — donc cette story branche les pumps sur une **interface Kotlin `PacketRelay` stubbée par défaut** (no-op qui dropp les paquets en silence + log debug). Aucun fichier Go n'est lu, créé ou modifié. Aucun import gomobile dans le code Kotlin de cette story. Aucun `internal/*` racine n'est touché. Aucune entrée dans `android/shims/*.go` n'est ajoutée ou modifiée. Aucune ligne dans `go.mod`/`go.sum` racine n'est touchée.
>
> **Rappel ADR-08 (architecture.md l. 2385-2388) — isolation OS maximale.** La règle structurelle est : un agent IA travaillant sur Android ne touche JAMAIS au code Windows, Linux, ou aux packages racine `internal/*` desktop. Si tu détectes une logique qui semble dupliquer du code desktop (ex : framing tunnel, parsing registre, session token), **c'est intentionnel** — la duplication assumée est documentée ADR-08. La seule porte vers le partagé Go = `android/shims/*.go` (livré 9.2) → `.aar` gomobile (généré localement) → `GoCoreAdapter.kt` (à livrer 9.7). Story 9.4 reste **strictement avant cette porte**.
>
> **Quand l'exception "code partagé" s'applique-t-elle ?** Uniquement aux stories qui modifient explicitement la frontière partagée :
> - Story 9.2 (livrée) : ajout de `golang.org/x/mobile` à `go.mod` racine + création des 5 shims `android/shims/{auth,crypto,leakcheck,protocol,registry}/`
> - Story 9.7 (à livrer) : enrichissement des shims pour exposer la pompe paquets + connect/disconnect réels du noyau Go au consommateur Kotlin
> - Toute future story qui aurait besoin d'exposer une nouvelle fonction Go côté Android — **avec ADR justificatif obligatoire avant ajout** (ADR-09 et règle « justification obligatoire dans un ADR avant ajout au noyau partagé », architecture.md l. 1100)
>
> **Story 9.4 n'est PAS dans cette catégorie.** Le file descriptor `fd` retourné par `VpnService.Builder.establish()` est un `Int` Android — il n'a pas besoin du Go pour exister. Les pumps thread Kotlin lisent `FileInputStream(fd)` et écrivent `FileOutputStream(fd)` sans intermédiaire Go. La pompe ne fait que mettre les paquets dans une queue Kotlin / appelle une interface Kotlin (`PacketRelay.onOutboundPacket(buf, len)`). L'implémentation réelle de cette interface (qui pousse vers un stream HTTP/3 via gomobile) est l'objet exclusif de Story 9.7.
>
> **Zones explicitement OFF-LIMITS pour cette story** (livrées par 9.1/9.2/9.3, intactes pour 9.4) :
>
> | Zone | Livrée par | État pour 9.4 |
> |---|---|---|
> | `go.mod` racine (`golang.org/x/mobile` indirect + bumps `crypto`/`mod`/`net`/`sys`/`text`/`tools`) | Story 9.2 | INTACT — ne pas toucher |
> | `go.sum` racine | Story 9.2 | INTACT — ne pas toucher |
> | `android/shims/{auth,crypto,leakcheck,protocol,registry}/*.go` (5 shims gomobile) | Story 9.2 | INTACT — code Go, pas Kotlin. Story 9.4 n'en lit aucun |
> | `android/scripts/build-aar.{sh,ps1}` + `verify-shared-imports.sh` | Story 9.2 | INTACT — non invoqués par 9.4 |
> | `android/levoile-core/build.gradle.kts` + `levoile-core/src/main/AndroidManifest.xml` | Story 9.1+9.2 | INTACT — aucune classe Kotlin de 9.4 n'y atterrit (l'adapter Go arrive 9.7) |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` | Story 9.3 | INTACT — 9.4 ne touche ni l'UI ni le bridge JS (qui reste figé à `getStatus()` placeholder jusqu'à 11.2) |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` | Story 9.3 | INTACT — l'enrichissement (`connect`, `disconnect`, etc.) appartient à Story 11.2 |
> | `android/app/src/main/assets/{index.html,style.css,app.js}` | Story 9.3 | INTACT — pas de modification UI ici |
> | `android/app/src/main/res/values/themes.xml` + `res/xml/network_security_config.xml` + `res/xml/data_extraction_rules.xml` | Story 9.1 (themes/network) + 9.3 (network) | INTACT |
> | `internal/{tunnel,tun,firewall,routing,ui,ipc,wfp,nftables,...}` racine + `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/` | Stories 1-8 | INTACT — hors arbre `android/` |
>
> **Concrètement** : `git status` à la fin de la session dev doit montrer **uniquement** :
>   (a) entrées sous `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/` (NOUVEAU — 3 fichiers : `LeVoileVpnService.kt`, `PacketRelay.kt`, `VpnConstants.kt`),
>   (b) `android/app/src/main/AndroidManifest.xml` (MODIFIÉ — ajout `<service>` + 1 nouvelle `<uses-permission>`),
>   (c) `android/app/src/main/res/values/strings.xml` (MODIFIÉ ou NOUVEAU — clés service + statut),
>   (d) `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ ou NOUVEAU — traductions FR),
>   (e) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt` (NOUVEAU — test smoke Robolectric),
>   (f) `android/README-android.md` (MODIFIÉ — section « Capture L3 via VpnService »),
>   (g) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `backlog`/`ready-for-dev` → `review`),
>   (h) `_bmad-output/implementation-artifacts/9-4-levoilevpnservice-creation-tun-pump-paquets-ip.md` (auto-update Status, File List, Completion Notes, Change Log).
>
> **Aucune entrée à la racine** (`go.mod`, `go.sum`, `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`, `_bmad/`), **aucune entrée sous `android/shims/`, `android/scripts/`, `android/levoile-core/`, `android/app/src/main/assets/`, `android/app/src/main/kotlin/fr/plateformeliberte/levoile/{MainActivity.kt,bridge/}`**. Tout autre fichier modifié est un side-effect non prévu — **STOP, investiguer, ne pas tenter de "lisser" en supprimant les changements**. Reporter dans Debug Log et demander avant de poursuivre.
>
> **Anti-pattern fréquent à éviter** : tenter de pré-tirer en avance des fichiers prévus par les stories suivantes — `NotificationHelper.kt` complet (Story 9.6), `GoCoreAdapter.kt` réel (Story 9.7), action « Déconnecter » de la notification reliée au lifecycle Foreground (Story 9.5/9.6), `KillSwitchHelper.kt` (Story 11.5/11.6), `ConnectivityObserver.kt` (Phase 2 Epic 10/11), `BootReceiver.kt` (Phase 2). Cette story livre un squelette `VpnService` minimal qui se suffit à lui-même : créer la TUN, démarrer les pumps, exposer un stub notification suffisant pour passer le `startForeground(...)` dans les 5 s exigées Android (sinon ANR), gérer un `ACTION_DISCONNECT` minimal qui ferme le fd. **Aucune intégration Go.** **Aucune notification riche pays/IP.** **Aucun deeplink VPN settings.** Tout cela est référencé en dépendance future, pas implémenté ici.

## Story

En tant qu'utilisateur Android,
Je veux que l'app établisse un tunnel VPN via l'API officielle Android non-rootée (`android.net.VpnService`), affiche le popup système de consentement au premier connect, configure une interface TUN avec MTU 1420 + route par défaut + adresse virtuelle, et démarre les threads daemon de pompe paquets IP qui lisent/écrivent le `ParcelFileDescriptor` retourné par `establish()` (en relayant vers une interface Kotlin `PacketRelay` stubbée pour cette story),
Afin que tout mon trafic IP traverse Le Voile (FR-AND-1) et que la fondation Foreground Service (Story 9.5), notification persistante (Story 9.6) et intégration noyau Go QUIC/HTTP3 (Story 9.7) puissent se brancher à un service `VpnService` qui démarre déjà l'interface TUN sans erreur sur émulateur API 29/33/34.

## Acceptance Criteria

1. **`LeVoileVpnService` étend `android.net.VpnService` et déclare ses constantes lifecycle** — Quand `app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` est lu, il déclare une classe `class LeVoileVpnService : android.net.VpnService()` avec un companion object exposant 4 constantes `String` (les valeurs sont préfixées par `applicationId` pour éviter toute collision avec d'autres apps) :
   - `ACTION_CONNECT = "fr.plateformeliberte.levoile.action.CONNECT"`
   - `ACTION_DISCONNECT = "fr.plateformeliberte.levoile.action.DISCONNECT"`
   - `EXTRA_COUNTRY = "fr.plateformeliberte.levoile.extra.COUNTRY"` (optionnel, sera consommé Story 9.7 pour la sélection relais — ici uniquement déclaré pour figer le contrat)
   - `NOTIF_ID = 0xCEC1` (constante `Int` arbitraire choisie pour rester reconnaissable en logcat ; sera réutilisée Story 9.6 pour `notify(NOTIF_ID, ...)`)

   La classe surcharge **explicitement** : `onCreate()`, `onStartCommand(intent, flags, startId)`, `onDestroy()`, `onRevoke()`. Toutes les autres méthodes héritées de `VpnService`/`Service` ne sont pas surchargées dans cette story (elles le seront Story 9.5+).

2. **`onStartCommand` route sur `ACTION_CONNECT` / `ACTION_DISCONNECT` et retourne `START_REDELIVER_INTENT`** — Quand `LeVoileVpnService.onStartCommand(intent, flags, startId)` est invoquée :
   - Si `intent?.action == ACTION_CONNECT` → appelle `connectInternal()` (création TUN + démarrage pumps — voir AC #4-#5).
   - Si `intent?.action == ACTION_DISCONNECT` → appelle `disconnectInternal()` (arrêt pumps + fermeture fd — voir AC #6).
   - Si `intent == null` ou action inconnue → log via `Log.w(TAG, "onStartCommand intent inattendu: ...")` + retour `START_REDELIVER_INTENT` sans rien faire d'autre. **Ne PAS lancer une connect implicite** — c'est une régression de sécurité (l'utilisateur ne s'y attend pas, le popup VpnService n'a pas été présenté).
   - Le retour est **toujours** `Service.START_REDELIVER_INTENT` (architecture.md l. 1051), de sorte qu'un crash du service redémarre le service avec le dernier intent ré-délivré, garantissant la reprise automatique du tunnel sans intervention utilisateur.

3. **Popup système consentement VpnService déclenché côté `MainActivity` (out-of-scope) — `connectInternal()` part du principe que `VpnService.prepare()` a déjà retourné `null`** — Le déclenchement effectif du popup (`VpnService.prepare(this)` → `startActivityForResult(...)`) appartient à `MainActivity` (architecture.md l. 580-584) ET sera enrichi par Story 9.5 (intégration `MainActivity` ↔ `LeVoileVpnService` via `Intent` action `ACTION_CONNECT`). **Story 9.4 ne modifie PAS `MainActivity.kt`** (livré Story 9.3 placeholder UI). Le test manuel d'AC #1 et #4-#5 se fait via `adb shell am startservice -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService -a fr.plateformeliberte.levoile.action.CONNECT` après acceptation manuelle préalable du dialog de consentement (lancé une seule fois via une commande `am start ...VpnDialog...` ou via un appel `MainActivity` qui invoquera `VpnService.prepare(this)` Story 9.5). **Reporter dans Completion Notes** : pour faciliter ce test sans Story 9.5, le dev MAY (optionnel) ajouter une activité éphémère `app/src/debug/.../DebugVpnConsentActivity.kt` sous `src/debug/` (build variant debug **uniquement**, gitignoré dans le release) qui invoque `VpnService.prepare(this)` puis `startActivityForResult`. **Si fait, à isoler dans `src/debug/`** — ne pas polluer `src/main/`. **Sinon, OK** : le test runtime est repoussé à Story 9.5.

4. **`connectInternal()` configure `VpnService.Builder` avec MTU 1420 + route par défaut + adresse virtuelle 10.6.6.2/32 + DNS 10.6.6.1 + IPv6 + setBlocking + setUnderlyingNetworks(null)** — Quand `connectInternal()` est lue, elle exécute la séquence (architecture.md l. 593-602, FR-AND-1, prd.md l. 609) :
   ```kotlin
   private fun connectInternal() {
       if (vpnInterface != null) {
           Log.w(TAG, "connectInternal appelée alors que vpnInterface != null — ignoré")
           return
       }
       val builder = Builder()
           .setSession(getString(R.string.vpn_session_name))     // "Le Voile"
           .addAddress("10.6.6.2", 32)                          // adresse virtuelle locale tunnel
           .addRoute("0.0.0.0", 0)                              // IPv4 — toute route via VPN
           .addRoute("::", 0)                                    // IPv6 — toute route via VPN (FR-AND-1)
           .addDnsServer("10.6.6.1")                            // DNS résolu côté relais
           .setMtu(1420)                                         // MTU défini par architecture
           .setBlocking(true)                                    // FileInputStream.read bloquant kernel
           .setUnderlyingNetworks(null)                          // OS choisit Wi-Fi/cellulaire
       val pfd = builder.establish()
           ?: throw IllegalStateException("VpnService.establish() returned null — consentement non accordé ou route invalide")
       vpnInterface = pfd
       startPumpThreads(pfd)                                    // AC #5
       // startForeground stub — voir AC #7
       startForeground(NOTIF_ID, buildStubOngoingNotification())
   }
   ```
   - **Ordre critique** : `addAddress` AVANT `addRoute` (sinon `establish()` retourne null silencieusement sur certains OEM).
   - **`setUnderlyingNetworks(null)`** : laisse l'OS choisir la meilleure interface réseau (Wi-Fi / 4G / 5G) — comportement par défaut, déclaré explicitement pour clarté.
   - **`setBlocking(true)`** : nécessaire pour que `FileInputStream(fd).read(buf)` bloque jusqu'à dispo paquet (sinon retourne 0 immédiatement → boucle CPU 100%).
   - **Pas de `setConfigureIntent`** dans cette story (ce serait l'intent ouvert quand l'utilisateur tape sur l'item « Le Voile » dans le réglage système VPN — Story 11.x ajoutera).
   - **Pas de `addAllowedApplication` / `addDisallowedApplication`** (split tunneling — hors scope MVP, architecture.md l. 469).

5. **Deux threads daemon démarrent : `vpn-out-pump` (lecture fd → `PacketRelay.onOutboundPacket`) et `vpn-in-pump` (réception via `PacketSink.write` → écriture fd)** — Quand `startPumpThreads(pfd)` est lue, elle crée deux threads :
   ```kotlin
   private val running = java.util.concurrent.atomic.AtomicBoolean(false)
   private var outPumpThread: Thread? = null
   private var inPumpThread: Thread? = null
   private val packetSink = ConcurrentLinkedQueue<ByteArray>()       // bouchon Story 9.7

   private fun startPumpThreads(pfd: ParcelFileDescriptor) {
       running.set(true)
       val fis = FileInputStream(pfd.fileDescriptor)
       val fos = FileOutputStream(pfd.fileDescriptor)

       outPumpThread = Thread({
           val buf = ByteArray(MAX_IP_PACKET)                       // 32 768 octets — ample pour MTU 1420 + IPv6 jumbo
           try {
               while (running.get()) {
                   val n = fis.read(buf)
                   if (n <= 0) break
                   try {
                       packetRelay.onOutboundPacket(buf, n)         // stub no-op par défaut (Story 9.7 réécrira)
                   } catch (t: Throwable) {
                       Log.w(TAG, "out-pump packetRelay error", t)
                   }
               }
           } catch (e: java.io.IOException) {
               // attendu au close — pas un bug
               Log.d(TAG, "out-pump fermé via IOException (close attendu)")
           } finally {
               running.set(false)
           }
       }, "vpn-out-pump").apply { isDaemon = true; start() }

       inPumpThread = Thread({
           try {
               while (running.get()) {
                   val pkt = packetSink.poll() ?: run {
                       Thread.sleep(5)                              // évite busy-wait — sera remplacé Story 9.7 par BlockingQueue.take
                       null
                   } ?: continue
                   try {
                       fos.write(pkt)
                   } catch (e: java.io.IOException) {
                       Log.d(TAG, "in-pump fermé via IOException (close attendu)")
                       break
                   }
               }
           } finally {
               running.set(false)
           }
       }, "vpn-in-pump").apply { isDaemon = true; start() }
   }
   ```
   - **`MAX_IP_PACKET = 32_768`** déclaré dans `VpnConstants.kt` (livré cette story).
   - **Threads daemon** (architecture.md l. 1207) : se terminent automatiquement avec le process si jamais `stopSelf()` est appelé sans avoir explicitement arrêté les pumps.
   - **`PacketRelay`** est l'**interface** Kotlin de cette story (livrée fichier `PacketRelay.kt` — voir Task 4) ; son implémentation par défaut est `NoOpPacketRelay` qui logge en debug et drope. **Story 9.7** réécrira l'implémentation pour pousser vers `GoCoreAdapter.writePacket(buf, n)`.
   - **`packetSink`** (`ConcurrentLinkedQueue<ByteArray>`) est un bouchon temporaire pour Story 9.4. Story 9.7 le remplacera par un canal direct depuis le callback Go vers `fos.write(...)`. Le `Thread.sleep(5)` est une simplification ABUSIVE pour cette story, **acceptable car le pump in n'a strictement rien à recevoir tant que Story 9.7 n'est pas livrée** (le sink reste vide en permanence). Le test smoke (AC #9) valide que le sleep n'empêche pas l'arrêt propre via `running.set(false)`.
   - **Aucune métrique pump** dans cette story (compteurs paquets, débit). Phase 2 Epic 11 si besoin.

6. **`disconnectInternal()` arrête les pumps, ferme le fd, libère l'interface TUN, stoppe le service** — Quand `disconnectInternal()` est lue :
   ```kotlin
   private fun disconnectInternal() {
       running.set(false)
       outPumpThread?.interrupt()
       inPumpThread?.interrupt()
       outPumpThread = null
       inPumpThread = null
       try {
           vpnInterface?.close()                                    // libère l'interface TUN — Android GC le reste
       } catch (e: java.io.IOException) {
           Log.w(TAG, "vpnInterface.close error", e)
       }
       vpnInterface = null
       stopForeground(STOP_FOREGROUND_REMOVE)                       // Story 9.5 raffinera (latence 5s)
       stopSelf()
   }
   ```
   - **`stopSelf()`** termine le service ; le système Android GC l'instance.
   - **`stopForeground(STOP_FOREGROUND_REMOVE)`** retire la notification immédiatement (Story 9.5 ajustera selon UX onboarding).
   - **`vpnInterface?.close()`** ferme `ParcelFileDescriptor` → libère le file descriptor kernel → l'interface TUN disparaît (Android le GC, conformément à l'AC `epics.md l. 1591` « aucun résidu d'interface ne subsiste, Android garbage-collecte automatiquement »).
   - **`onRevoke()`** (appelée par l'OS si l'utilisateur change de VPN ou désactive Le Voile depuis Settings — architecture.md l. 606, l. 1054) **délègue à `disconnectInternal()`** pour rester cohérent avec le flow Disconnect normal :
     ```kotlin
     override fun onRevoke() {
         Log.i(TAG, "onRevoke — utilisateur a révoqué le consentement VpnService")
         disconnectInternal()
         super.onRevoke()
     }
     ```
   - **`onDestroy()`** appelle `disconnectInternal()` en garde (idempotence — si l'utilisateur kille le service depuis Réglages → Apps → Force Stop) :
     ```kotlin
     override fun onDestroy() {
         disconnectInternal()
         super.onDestroy()
     }
     ```

7. **Notification stub `buildStubOngoingNotification()` permet `startForeground(NOTIF_ID, ...)` en moins de 5 s** — Quand `LeVoileVpnService.connectInternal()` appelle `startForeground(NOTIF_ID, buildStubOngoingNotification())`, la notification est suffisante pour que le système Android ne tue pas le service par ANR. **Cette notification est volontairement minimaliste — Story 9.6 livre la version finale (titre dynamique « Le Voile · Connecté », sous-texte pays/IP, action « Déconnecter », channel `levoile_vpn_status` IMPORTANCE_LOW, icône mono-couleur dédiée).** Pour cette story 9.4 :
   - Channel à créer dans `onCreate()` via `NotificationManagerCompat.from(this).createNotificationChannel(NotificationChannelCompat.Builder("levoile_vpn_status_stub", IMPORTANCE_LOW).setName(getString(R.string.notif_channel_status)).build())`. **L'ID de channel `"levoile_vpn_status_stub"`** est utilisé pour cette story afin d'éviter qu'un préréglage IMPORTANCE_LOW intermédiaire interfère avec celui livré finalement Story 9.6 (`"levoile_vpn_status"`). Story 9.6 supprimera le channel stub via `notificationManager.deleteNotificationChannel("levoile_vpn_status_stub")` au premier démarrage post-9.6.
   - Builder via `NotificationCompat.Builder(this, "levoile_vpn_status_stub")` avec `setSmallIcon(R.drawable.ic_notification_stub)` (vector drawable mono-couleur — fichier livré cette story, **sera remplacé Story 9.6 par `ic_levoile_status.xml` finalisé**), `setContentTitle(getString(R.string.vpn_notif_title_stub))` (« Le Voile · Tunnel actif »), `setOngoing(true)`, `setSilent(true)`. **Pas d'action « Déconnecter »** dans cette story — Story 9.6 l'ajoute.
   - `POST_NOTIFICATIONS` (Android 13+) : déjà déclarée dans le manifest Story 9.1. **La demande runtime de cette permission appartient à `MainActivity`** (architecture.md l. 1182) et n'est PAS dans le scope 9.4. Si la permission est refusée par l'utilisateur, Android masque la notification mais autorise le Foreground Service à tourner — comportement attendu et documenté ci-dessus.

8. **`AndroidManifest.xml` enrichi : `<service>` + `<uses-permission FOREGROUND_SERVICE_VPN>`** — Quand `app/src/main/AndroidManifest.xml` est lu après cette story, il déclare :
   ```xml
   <!-- AJOUT : Story 9.4 — service VPN typé "vpn" (Android 14+ requiert FOREGROUND_SERVICE_VPN) -->
   <uses-permission android:name="android.permission.FOREGROUND_SERVICE_VPN" />
   ```
   et **dans `<application>`** :
   ```xml
   <service
       android:name=".vpn.LeVoileVpnService"
       android:permission="android.permission.BIND_VPN_SERVICE"
       android:foregroundServiceType="vpn"
       android:exported="false">
       <intent-filter>
           <action android:name="android.net.VpnService" />
       </intent-filter>
   </service>
   ```
   - **`android:permission="BIND_VPN_SERVICE"`** : seul le système Android peut bind ce service (sécurité), conforme architecture.md l. 658.
   - **`android:foregroundServiceType="vpn"`** : Android 14+ (API 34) requiert ce type explicite pour un Foreground Service VPN. **Story 9.1 avait commenté `dataSync`** dans le manifest — ce commentaire devient obsolète et la `<uses-permission FOREGROUND_SERVICE_DATA_SYNC>` reste **pour cohabitation** (un Service `dataSync` séparé pourrait apparaître Phase 2, ex : un `WorkManager` de check-update — Story 12.5). **Décision dev** : laisser `FOREGROUND_SERVICE_DATA_SYNC` en place ET ajouter `FOREGROUND_SERVICE_VPN` (les deux sont sans risque, NFR-AND-7 PRD l. 703 liste explicitement `FOREGROUND_SERVICE_DATA_SYNC` comme acceptable et la liste autorisée comprend implicitement les types FGS dérivés). Mettre à jour le commentaire XML pour refléter le typage final.
   - **`android:exported="false"`** : Android 12+ requiert `exported` explicite. La spec d'architecture l. 660 le confirme.
   - **`<intent-filter><action android:name="android.net.VpnService"/></intent-filter>`** : signature standard pour qu'Android reconnaisse le service comme implémentation `VpnService` (présent dans le settings « VPN » système).

9. **Test smoke JUnit `LeVoileVpnServiceConfigTest.kt` — vérifie classe résolvable, constantes correctes, manifest cohérent** — Quand `cd android && ./gradlew :app:testDebugUnitTest` est exécuté, un test unitaire JVM `app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt` (a) instancie via Robolectric (`@RunWith(RobolectricTestRunner::class)` + `@Config(sdk=[34])`) le contexte d'application minimal — **stratégie cohérente avec Story 9.3** ; (b) charge la classe `LeVoileVpnService` via `Class.forName("fr.plateformeliberte.levoile.vpn.LeVoileVpnService")` et vérifie via `Assert.assertNotNull` qu'elle hérite bien de `android.net.VpnService` (`assertTrue(android.net.VpnService::class.java.isAssignableFrom(cls))`) ; (c) vérifie via réflexion que le companion object expose les 4 constantes `ACTION_CONNECT`, `ACTION_DISCONNECT`, `EXTRA_COUNTRY`, `NOTIF_ID` avec les valeurs attendues ; (d) charge `PacketRelay` (interface) et `NoOpPacketRelay` (impl par défaut) via `Class.forName(...)` et vérifie que la méthode `onOutboundPacket(ByteArray, Int)` existe ; (e) parse `app/src/main/AndroidManifest.xml` via `applicationContext.packageManager.getServiceInfo(ComponentName(applicationContext, LeVoileVpnService::class.java), PackageManager.GET_META_DATA)` et vérifie `serviceInfo.permission == "android.permission.BIND_VPN_SERVICE"` + `serviceInfo.foregroundServiceType == FOREGROUND_SERVICE_TYPE_VPN`. **Si l'API Robolectric pour `getServiceInfo` est instable** (parfois différente entre versions 4.10 vs 4.12), fallback acceptable : parser le XML via `org.xmlpull.v1.XmlPullParser` directement sur le fichier source. **Décision dev à reporter dans Debug Log**. **Aucun test runtime du pump réel** dans cette story (instanciation `Builder()` requiert un service réel qui requiert un consent OS — out of scope test JVM Robolectric). Le test instrumenté complet (lance le service réel + vérifie pump qui draine + ferme proprement) est porté par Story 12.6.

10. **Build debug + release réussissent, taille APK release < 25 MB** — Quand `cd android && ./gradlew clean assembleDebug assembleRelease :app:testDebugUnitTest :app:lint` est exécuté, **toutes** les tâches passent (exit 0). L'APK debug est installable (`adb install -r app/build/outputs/apk/debug/app-debug.apk`) et l'APK release a une taille < 25 MB mesurée via `apkanalyzer apk file-size app/build/outputs/apk/release/app-release.apk` (NFR-AND-3). **Note** : la taille reste très inférieure à 25 MB (~2-4 MB attendu) — Story 9.4 ajoute uniquement quelques classes Kotlin (~10 KB minified). Si la taille saute (> 5 MB d'augmentation vs Story 9.3), investiguer une dépendance imprévue ajoutée par mégarde.

11. **`README-android.md` patché — section « Capture L3 via VpnService »** — Le `android/README-android.md` (livré Story 9.1, déjà patché par Stories 9.2 puis 9.3) est complété d'une section dédiée **après** la section « Lancement de l'app debug » (Story 9.3) :
   ```markdown
   ## Capture L3 via VpnService (Story 9.4 livrée)

   Le service `LeVoileVpnService` (sous `app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/`)
   crée l'interface TUN Android via `VpnService.Builder.establish()` et démarre les
   threads de pump paquets IP. À ce stade :

   - **PacketRelay** est stubbé (`NoOpPacketRelay`) — les paquets sortants sont droppés
     en silence, aucun paquet entrant n'est jamais reçu. Story 9.7 le réécrira pour
     pousser vers le noyau Go partagé via `GoCoreAdapter` (handshake QUIC/HTTP3 +
     stream `/tunnel`).
   - **Foreground Service lifecycle** : `startForeground(NOTIF_ID, ...)` est appelé en
     moins de 5 s avec une notification stub minimaliste. Story 9.6 livrera la
     notification finale (titre dynamique, sous-texte pays/IP, action Déconnecter).
   - **MainActivity** ne déclenche pas encore `VpnService.prepare()`. Story 9.5 brancha
     l'orchestration complète UI ↔ Service via Intents `ACTION_CONNECT`/`DISCONNECT`.

   Test manuel post-9.4 (sans 9.5/9.7) :
   ```
   # 1. Installer l'APK debug
   adb install -r app/build/outputs/apk/debug/app-debug.apk

   # 2. Accepter le consent VpnService manuellement (cf. variant `:debug` qui peut
   #    embarquer une activité de debug — voir AC #3 Story 9.4 pour la mise en place
   #    optionnelle).

   # 3. Démarrer le service
   adb shell am start-foreground-service \
     -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService \
     -a fr.plateformeliberte.levoile.action.CONNECT

   # 4. Vérifier l'interface TUN créée
   adb shell ip link show | grep tun

   # 5. Stopper le service
   adb shell am start-foreground-service \
     -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService \
     -a fr.plateformeliberte.levoile.action.DISCONNECT
   ```

   **À ce stade, l'app n'établit pas encore de tunnel chiffré** — `LeVoileVpnService`
   crée la TUN et démarre les pumps mais ne relaie aucun paquet. Le tunnel chiffré
   QUIC/HTTP3 vers les relais est livré Story 9.7.
   ```
   Aucune autre section du README n'est touchée par cette story.

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état du squelette livré Stories 9.1+9.2+9.3 + lister ce qui manque** (AC: tous)
  - [x] Lire `android/app/src/main/AndroidManifest.xml` — confirmer le commentaire « LeVoileVpnService sera livré Story 9.4 avec : android:foregroundServiceType="dataSync" » (Story 9.1). **Le commentaire est obsolète** : Story 9.4 corrige en `foregroundServiceType="vpn"` car Android 14 (API 34, target SDK Story 9.1) impose ce typage pour les services étendant `VpnService`. Voir AC #8 + section « Notes de divergence corrigées en amont ».
  - [x] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (livré 9.3) — confirmer qu'il N'invoque PAS `VpnService.prepare()`. C'est attendu : 9.4 ne touche pas à `MainActivity`. L'orchestration UI ↔ Service est portée par Story 9.5.
  - [x] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (livré 9.3) — confirmer qu'il expose UNIQUEMENT `getStatus(): String` (placeholder). Story 9.4 ne touche pas non plus à ce bridge ; Story 11.2 ajoutera `connect()`/`disconnect()`.
  - [x] Lire `android/app/build.gradle.kts` — noter les dépendances déjà présentes (`androidx.core.ktx`, `androidx.appcompat`, `androidx.webkit` ajouté Story 9.3, `junit:4.13.2` testImpl ajouté Story 9.2). **Story 9.4 n'ajoute aucune dépendance** sauf si Robolectric n'a pas déjà été ajouté Story 9.3 — auquel cas l'ajouter ici (`testImplementation("org.robolectric:robolectric:4.12.2")` + `androidx.test:core:1.5.0` + `androidx.test.ext:junit:1.1.5`). Vérifier avant.
  - [x] Vérifier que `android/app/src/main/res/values/strings.xml` existe (livré Story 9.1 avec `<string name="app_name">Le Voile</string>` minimal). **Story 9.4 y ajoute** : `vpn_session_name`, `vpn_notif_title_stub`, `notif_channel_status`. Vérifier l'absence pour éviter doublons.
  - [x] Vérifier que `android/app/src/main/res/values-fr/strings.xml` existe. **Le créer si absent** (Story 9.1 a livré `values/strings.xml` en français par défaut — confirmer). Story 9.4 doit fournir des traductions FR cohérentes (titre notif).
  - [x] **Reporter dans Debug Log** : état exact des fichiers Story 9.1+9.2+9.3 lu, écarts éventuels avec la spec, et confirmer que `vpn/` package n'existe pas encore (sinon STOP — quelqu'un a déjà commencé Story 9.4).

- [x] **Task 2 : Créer les ressources Android — `strings.xml` + `ic_notification_stub.xml`** (AC: #4, #7)
  - [x] Éditer `android/app/src/main/res/values/strings.xml` — ajouter (en respectant l'encodage UTF-8) :
    ```xml
    <!-- Story 9.4 : VpnService + Foreground Service stub -->
    <string name="vpn_session_name">Le Voile</string>
    <string name="vpn_notif_title_stub">Le Voile · Tunnel actif</string>
    <string name="notif_channel_status">Statut Le Voile</string>
    ```
  - [x] Créer/éditer `android/app/src/main/res/values-fr/strings.xml` — mêmes clés en français (souvent identiques car `values/` est déjà en français). Si `values-fr/` n'existe pas, le créer. **Note importance** : Story 11.x livrera la version EN par défaut (déplacement vers `values/strings.xml` EN + `values-fr/strings.xml` FR). Pour cette story, garder le FR comme défaut (cohérent avec Story 9.1).
  - [x] Créer `android/app/src/main/res/drawable/ic_notification_stub.xml` — vector drawable mono-couleur **simple** (un cadenas ou un trait — Android exige les icônes notification blanches/transparentes pour rester compatibles charte) :
    ```xml
    <?xml version="1.0" encoding="utf-8"?>
    <vector xmlns:android="http://schemas.android.com/apk/res/android"
        android:width="24dp"
        android:height="24dp"
        android:viewportWidth="24"
        android:viewportHeight="24"
        android:tint="?android:attr/colorControlNormal">
        <!-- Glyphe minimaliste : un cercle (statut connecté générique).
             Story 9.6 fournira ic_levoile_status.xml finalisé.
             Glyphe blanc/transparent obligatoire (Android: les notifications
             colorées sur fond non-blanc cassent l'affichage status bar). -->
        <path
            android:fillColor="#FFFFFFFF"
            android:pathData="M12,2C6.48,2 2,6.48 2,12s4.48,10 10,10 10,-4.48 10,-10S17.52,2 12,2zm0,18c-4.42,0 -8,-3.58 -8,-8s3.58,-8 8,-8 8,3.58 8,8 -3.58,8 -8,8z"/>
    </vector>
    ```
  - [x] Vérifier que `android/app/src/main/res/values/colors.xml` (livré Story 9.1) déclare `primary_blue`, `primary_blue_dark`, `accent_blue`, `bg_dark`. Pas de modif nécessaire (le drawable stub utilise `colorControlNormal` du thème, pas une couleur dure).

- [x] **Task 3 : Créer le package `vpn/` et son fichier `VpnConstants.kt`** (AC: #1, #5)
  - [x] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/VpnConstants.kt` :
    ```kotlin
    package fr.plateformeliberte.levoile.vpn

    /**
     * Constantes partagées entre LeVoileVpnService et ses futures consommateurs
     * (MainActivity Story 9.5, NotificationHelper Story 9.6, GoCoreAdapter Story 9.7).
     *
     * Localisation : android/ (et NON un noyau Go partagé) — ces constantes sont
     * Android-spécifiques (taille buffer fd, IDs notification, action strings) et
     * n'ont aucun équivalent desktop. Cohérent ADR-08 (isolation OS maximale) :
     * la duplication assumée s'applique aux constantes runtime de chaque OS.
     */
    object VpnConstants {

        /** Taille max d'un paquet IP lu depuis le fd VpnService.
         *  32 768 = ample pour MTU 1420 + IPv6 jumbograms futurs. */
        const val MAX_IP_PACKET: Int = 32_768

        /** ID notification Foreground Service stub (Story 9.4) puis finale (Story 9.6).
         *  Hors-zéro pour éviter conflit avec d'autres notifs IDLE Android. */
        const val NOTIF_ID: Int = 0xCEC1

        /** Channel ID stub Story 9.4 — sera supprimé Story 9.6 au profit
         *  de "levoile_vpn_status" (cohérent architecture.md l. 1066). */
        const val CHANNEL_ID_STUB: String = "levoile_vpn_status_stub"

        // Action strings — préfixées par applicationId pour éviter collisions globales Android.
        const val ACTION_CONNECT: String = "fr.plateformeliberte.levoile.action.CONNECT"
        const val ACTION_DISCONNECT: String = "fr.plateformeliberte.levoile.action.DISCONNECT"
        const val EXTRA_COUNTRY: String = "fr.plateformeliberte.levoile.extra.COUNTRY"
    }
    ```

- [x] **Task 4 : Créer l'interface `PacketRelay.kt` + l'implémentation par défaut `NoOpPacketRelay`** (AC: #5, #6)
  - [x] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/PacketRelay.kt` :
    ```kotlin
    package fr.plateformeliberte.levoile.vpn

    import android.util.Log

    /**
     * Pont entre la pompe paquets sortante du VpnService et le noyau de relayage.
     *
     * Story 9.4 livre cette interface + l'implémentation par défaut [NoOpPacketRelay]
     * qui drope les paquets en silence (placeholder). Story 9.7 livrera une implémentation
     * réelle qui pousse les paquets vers le noyau Go partagé via GoCoreAdapter.writePacket(...)
     * (handshake QUIC/HTTP3 + stream /tunnel).
     *
     * **Frontière étroite** : cette interface ne traverse JAMAIS la frontière OS-spécifique
     * (cohérent ADR-08). Elle reste purement Kotlin/Android. Le wrapper Go arrive Story 9.7
     * sous la forme d'une classe Kotlin GoBackedPacketRelay : PacketRelay qui consomme
     * la classe Java générée par gomobile dans le .aar.
     */
    interface PacketRelay {
        /**
         * Appelée pour chaque paquet IP brut lu depuis FileInputStream(fd VpnService).
         * @param buf buffer rempli par fis.read() — **NE PAS retenir la référence**, le caller
         *  réutilise le même buffer à chaque itération de la pompe (allocation 0).
         * @param length nombre d'octets valides dans buf[0 until length]. Toujours > 0.
         */
        fun onOutboundPacket(buf: ByteArray, length: Int)

        /**
         * Lifecycle hook appelé par LeVoileVpnService.connectInternal() après establish().
         * Permet à l'implémentation de préparer son state (ex : ouvrir la connexion QUIC
         * Story 9.7). Default : no-op.
         */
        fun onTunnelStarted() {}

        /**
         * Lifecycle hook appelé par LeVoileVpnService.disconnectInternal() avant fermeture
         * du fd. Permet à l'implémentation de drainer ses buffers / fermer la connexion
         * QUIC Story 9.7. Default : no-op.
         */
        fun onTunnelStopped() {}
    }

    /**
     * Implémentation par défaut Story 9.4 — drope tous les paquets en silence.
     * Utilisée jusqu'à Story 9.7 qui livrera GoBackedPacketRelay.
     */
    class NoOpPacketRelay : PacketRelay {

        override fun onOutboundPacket(buf: ByteArray, length: Int) {
            // Drop silencieux. En BuildConfig.DEBUG uniquement, log très léger
            // (1 ligne par 1000 paquets) pour confirmer que le pump tourne sans
            // saturer logcat. **Jamais de payload paquet logué** (NFR-AND-9).
            if (fr.plateformeliberte.levoile.BuildConfig.DEBUG) {
                droppedCount++
                if (droppedCount % 1000 == 0L) {
                    Log.d(TAG, "NoOpPacketRelay: $droppedCount paquets sortants droppés (placeholder Story 9.4)")
                }
            }
        }

        private var droppedCount = 0L

        companion object {
            private const val TAG = "NoOpPacketRelay"
        }
    }
    ```
  - [x] **Vérifier** que la regex CI Story 12.4 (lint logging) ne refusera pas le `Log.d` ci-dessus en build release — il est gardé par `BuildConfig.DEBUG` donc R8 (Story 9.1 `minifyEnabled=true`) éliminera mort-code en release. Cohérent NFR-AND-9 (filtrage logs par BuildType — Story 10.5 finalisera).

- [x] **Task 5 : Créer `LeVoileVpnService.kt`** (AC: #1, #2, #4, #5, #6, #7)
  - [x] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` :
    ```kotlin
    package fr.plateformeliberte.levoile.vpn

    import android.app.Service
    import android.content.Intent
    import android.net.VpnService
    import android.os.ParcelFileDescriptor
    import android.util.Log
    import androidx.core.app.NotificationChannelCompat
    import androidx.core.app.NotificationCompat
    import androidx.core.app.NotificationManagerCompat
    import fr.plateformeliberte.levoile.R
    import fr.plateformeliberte.levoile.vpn.VpnConstants.ACTION_CONNECT
    import fr.plateformeliberte.levoile.vpn.VpnConstants.ACTION_DISCONNECT
    import fr.plateformeliberte.levoile.vpn.VpnConstants.CHANNEL_ID_STUB
    import fr.plateformeliberte.levoile.vpn.VpnConstants.MAX_IP_PACKET
    import fr.plateformeliberte.levoile.vpn.VpnConstants.NOTIF_ID
    import java.io.FileInputStream
    import java.io.FileOutputStream
    import java.util.concurrent.ConcurrentLinkedQueue
    import java.util.concurrent.atomic.AtomicBoolean

    /**
     * Service VPN Le Voile — étend android.net.VpnService.
     *
     * Story 9.4 livre :
     *   - Création TUN via VpnService.Builder (MTU 1420 + route 0.0.0.0/0 + ::/0 + DNS 10.6.6.1).
     *   - Pumps Kotlin lecture (fd → PacketRelay) et écriture (sink → fd) en threads daemon.
     *   - Foreground Service avec notification stub (channel "levoile_vpn_status_stub").
     *   - Lifecycle minimal : ACTION_CONNECT / ACTION_DISCONNECT / onRevoke / onDestroy.
     *
     * Story 9.5 enrichira : MainActivity ↔ Service (Intents UI), startForeground délai 5s
     * confirmé sur OEM agressifs, START_REDELIVER_INTENT robustness sur crash test.
     * Story 9.6 enrichira : notification finale (channel "levoile_vpn_status", action
     * Déconnecter, sous-texte pays/IP).
     * Story 9.7 enrichira : remplace NoOpPacketRelay par GoBackedPacketRelay (handshake
     * QUIC/HTTP3 via gomobile + stream /tunnel).
     *
     * Hors scope définitif : split tunneling per-app (architecture.md l. 469, Phase 2).
     */
    class LeVoileVpnService : VpnService() {

        // État interne — accédé depuis onStartCommand (main thread Service) et les
        // threads pumps. AtomicBoolean pour le flag, refs primitives sous synchronisation
        // implicite via lifecycle (start avant pumps, stop après pumps).
        private var vpnInterface: ParcelFileDescriptor? = null
        private val running = AtomicBoolean(false)
        private var outPumpThread: Thread? = null
        private var inPumpThread: Thread? = null
        private val packetSink = ConcurrentLinkedQueue<ByteArray>()
        private val packetRelay: PacketRelay = NoOpPacketRelay()  // Story 9.7 injectera GoBackedPacketRelay

        // ---------- Lifecycle Service ----------

        override fun onCreate() {
            super.onCreate()
            Log.i(TAG, "onCreate")
            ensureNotificationChannel()
        }

        override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
            val action = intent?.action
            Log.i(TAG, "onStartCommand action=$action startId=$startId")
            when (action) {
                ACTION_CONNECT -> connectInternal()
                ACTION_DISCONNECT -> disconnectInternal()
                else -> Log.w(TAG, "onStartCommand action inconnue ou null — ignoré")
            }
            // Cohérent architecture.md l. 1051 : crash → relance avec dernier intent.
            return Service.START_REDELIVER_INTENT
        }

        override fun onRevoke() {
            Log.i(TAG, "onRevoke — utilisateur a révoqué le consentement VpnService")
            disconnectInternal()
            super.onRevoke()
        }

        override fun onDestroy() {
            Log.i(TAG, "onDestroy — cleanup final (idempotent)")
            disconnectInternal()
            super.onDestroy()
        }

        // ---------- Connect / Disconnect ----------

        private fun connectInternal() {
            if (vpnInterface != null) {
                Log.w(TAG, "connectInternal appelée alors que vpnInterface != null — ignoré")
                return
            }
            val builder = Builder()
                .setSession(getString(R.string.vpn_session_name))
                .addAddress("10.6.6.2", 32)
                .addRoute("0.0.0.0", 0)
                .addRoute("::", 0)
                .addDnsServer("10.6.6.1")
                .setMtu(1420)
                .setBlocking(true)
                .setUnderlyingNetworks(null)

            val pfd = builder.establish()
                ?: run {
                    Log.e(TAG, "VpnService.establish() returned null — consentement non accordé ou route invalide")
                    stopSelf()
                    return
                }
            vpnInterface = pfd
            packetRelay.onTunnelStarted()
            startPumpThreads(pfd)

            startForeground(NOTIF_ID, buildStubOngoingNotification())
            Log.i(TAG, "connectInternal: tunnel créé, pumps démarrés, foreground actif")
        }

        private fun disconnectInternal() {
            running.set(false)
            outPumpThread?.interrupt()
            inPumpThread?.interrupt()
            outPumpThread = null
            inPumpThread = null
            packetSink.clear()
            try {
                packetRelay.onTunnelStopped()
            } catch (t: Throwable) {
                Log.w(TAG, "packetRelay.onTunnelStopped error", t)
            }
            try {
                vpnInterface?.close()
            } catch (e: java.io.IOException) {
                Log.w(TAG, "vpnInterface.close error", e)
            }
            vpnInterface = null
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            Log.i(TAG, "disconnectInternal: tunnel fermé, pumps arrêtés, service stoppé")
        }

        // ---------- Pumps paquets IP ----------

        private fun startPumpThreads(pfd: ParcelFileDescriptor) {
            running.set(true)
            val fis = FileInputStream(pfd.fileDescriptor)
            val fos = FileOutputStream(pfd.fileDescriptor)

            outPumpThread = Thread({
                val buf = ByteArray(MAX_IP_PACKET)
                try {
                    while (running.get()) {
                        val n = try {
                            fis.read(buf)
                        } catch (e: java.io.IOException) {
                            Log.d(TAG, "out-pump fermé via IOException (close attendu)")
                            break
                        }
                        if (n <= 0) break
                        try {
                            packetRelay.onOutboundPacket(buf, n)
                        } catch (t: Throwable) {
                            // Ne JAMAIS laisser remonter une exception de la pompe — ça tuerait le service.
                            Log.w(TAG, "out-pump packetRelay error (continuons)", t)
                        }
                    }
                } finally {
                    running.set(false)
                }
            }, "vpn-out-pump").apply { isDaemon = true; start() }

            inPumpThread = Thread({
                try {
                    while (running.get()) {
                        val pkt = packetSink.poll()
                        if (pkt == null) {
                            // Story 9.4 : sink vide en permanence (NoOpPacketRelay).
                            // Story 9.7 remplacera par BlockingQueue.take() ou sera supprimé
                            // au profit d'un callback Go direct → fos.write(...).
                            try {
                                Thread.sleep(5)
                            } catch (e: InterruptedException) {
                                Thread.currentThread().interrupt()
                                break
                            }
                            continue
                        }
                        try {
                            fos.write(pkt)
                        } catch (e: java.io.IOException) {
                            Log.d(TAG, "in-pump fermé via IOException (close attendu)")
                            break
                        }
                    }
                } finally {
                    running.set(false)
                }
            }, "vpn-in-pump").apply { isDaemon = true; start() }
        }

        // ---------- Notification stub Foreground ----------

        private fun ensureNotificationChannel() {
            // NotificationChannel est ignoré pré-API 26, NotificationChannelCompat
            // est compat-shim safe sur toutes les API ciblées (minSdk 29).
            val channel = NotificationChannelCompat.Builder(
                CHANNEL_ID_STUB,
                NotificationManagerCompat.IMPORTANCE_LOW
            )
                .setName(getString(R.string.notif_channel_status))
                .setShowBadge(false)
                .build()
            NotificationManagerCompat.from(this).createNotificationChannel(channel)
        }

        private fun buildStubOngoingNotification(): android.app.Notification {
            return NotificationCompat.Builder(this, CHANNEL_ID_STUB)
                .setSmallIcon(R.drawable.ic_notification_stub)
                .setContentTitle(getString(R.string.vpn_notif_title_stub))
                .setOngoing(true)
                .setSilent(true)
                // Pas de setContentIntent ici — Story 9.5/9.6 brancheront sur MainActivity.
                // Pas d'addAction("Déconnecter") — Story 9.6 livrera l'action complète.
                .build()
        }

        companion object {
            private const val TAG = "LeVoileVpnService"
        }
    }
    ```
  - [x] **Note dev** : la classe est volontairement simple (pas de `CoroutineScope` ici — `Dispatchers.IO + SupervisorJob()` arrive Story 9.7 quand on appellera des suspend functions vers gomobile). Architecture.md l. 1209 décrit le scope cible mais Story 9.4 reste sur threads daemon classiques.
  - [x] Vérifier que `R.drawable.ic_notification_stub` est résolu (Task 2 livre le fichier). Si erreur de résolution, vérifier le case correct (tout lowercase + underscore — pas de tiret) et que `app/src/main/res/drawable/` existe (livré 9.1).

- [x] **Task 6 : Modifier `AndroidManifest.xml` — ajout `<service>` + `<uses-permission FOREGROUND_SERVICE_VPN>`** (AC: #8)
  - [x] Lire `android/app/src/main/AndroidManifest.xml` actuel (livré 9.1, intact 9.2/9.3).
  - [x] **Insérer dans la liste des `<uses-permission>`**, après `FOREGROUND_SERVICE_DATA_SYNC` :
    ```xml
    <!-- Story 9.4 : Android 14 (API 34) requiert FOREGROUND_SERVICE_VPN pour
         tout Foreground Service typé "vpn". BIND_VPN_SERVICE (sur le tag
         <service>) protège l'invocation système ; cette permission protège
         le typage FGS. Audit NFR-AND-7 : ajout justifié par Story 9.4. -->
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_VPN" />
    ```
  - [x] **Insérer dans `<application>`**, à l'intérieur du bloc `<application>` (et **avant** la fermeture `</application>`) :
    ```xml
    <!-- Story 9.4 : VpnService + Foreground Service typé "vpn" (Android 14+). -->
    <service
        android:name=".vpn.LeVoileVpnService"
        android:permission="android.permission.BIND_VPN_SERVICE"
        android:foregroundServiceType="vpn"
        android:exported="false">
        <intent-filter>
            <action android:name="android.net.VpnService" />
        </intent-filter>
    </service>
    ```
  - [x] **Mettre à jour le commentaire d'origine Story 9.1** dans le manifest (qui annonçait erronément `foregroundServiceType="dataSync"` pour 9.4) — le supprimer ou le réécrire pour refléter la réalité 9.4 livrée :
    ```xml
    <!-- LeVoileVpnService livré Story 9.4 — voir bloc <service> ci-dessous.
         BootReceiver / ConnectivityObserver seront ajoutés Phase 2 (Epic 10/11)
         selon besoin. -->
    ```
  - [x] **Ne PAS toucher** aux autres permissions (`INTERNET`, `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, `POST_NOTIFICATIONS`) — toutes restent valides.
  - [x] **Ne PAS toucher** à `<application>` lui-même (allowBackup, dataExtractionRules, fullBackupContent, icon, label, supportsRtl, theme, networkSecurityConfig). Tout est figé Story 9.1.
  - [x] Lancer `cd android && ./gradlew :app:processDebugManifest` pour valider la fusion manifest sans erreur.

- [x] **Task 7 : Créer le test smoke `LeVoileVpnServiceConfigTest.kt`** (AC: #9)
  - [x] Vérifier dans `app/build.gradle.kts` la présence de Robolectric + `androidx.test:core` (devrait avoir été ajouté Story 9.3 Task 8). Sinon ajouter :
    ```kotlin
    testImplementation("org.robolectric:robolectric:4.12.2")
    testImplementation("androidx.test:core:1.5.0")
    testImplementation("androidx.test.ext:junit:1.1.5")
    ```
  - [x] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt` :
    ```kotlin
    package fr.plateformeliberte.levoile.vpn

    import android.content.pm.PackageManager
    import org.junit.Assert.assertEquals
    import org.junit.Assert.assertNotNull
    import org.junit.Assert.assertTrue
    import org.junit.Test
    import org.junit.runner.RunWith
    import org.robolectric.RobolectricTestRunner
    import org.robolectric.RuntimeEnvironment
    import org.robolectric.annotation.Config

    @RunWith(RobolectricTestRunner::class)
    @Config(sdk = [34])
    class LeVoileVpnServiceConfigTest {

        @Test
        fun `LeVoileVpnService class is resolvable and extends VpnService`() {
            val cls = Class.forName("fr.plateformeliberte.levoile.vpn.LeVoileVpnService")
            assertNotNull(cls)
            assertTrue(
                "LeVoileVpnService must extend android.net.VpnService",
                android.net.VpnService::class.java.isAssignableFrom(cls)
            )
        }

        @Test
        fun `VpnConstants exposes expected action and notif IDs`() {
            assertEquals(
                "fr.plateformeliberte.levoile.action.CONNECT",
                VpnConstants.ACTION_CONNECT
            )
            assertEquals(
                "fr.plateformeliberte.levoile.action.DISCONNECT",
                VpnConstants.ACTION_DISCONNECT
            )
            assertEquals(
                "fr.plateformeliberte.levoile.extra.COUNTRY",
                VpnConstants.EXTRA_COUNTRY
            )
            assertEquals(0xCEC1, VpnConstants.NOTIF_ID)
            assertEquals("levoile_vpn_status_stub", VpnConstants.CHANNEL_ID_STUB)
            assertEquals(32_768, VpnConstants.MAX_IP_PACKET)
        }

        @Test
        fun `PacketRelay interface declares onOutboundPacket`() {
            val cls = Class.forName("fr.plateformeliberte.levoile.vpn.PacketRelay")
            val method = cls.declaredMethods.firstOrNull {
                it.name == "onOutboundPacket"
                    && it.parameterTypes.size == 2
                    && it.parameterTypes[0] == ByteArray::class.java
                    && it.parameterTypes[1] == Int::class.javaPrimitiveType
            }
            assertNotNull("PacketRelay.onOutboundPacket(ByteArray, Int) must exist", method)
        }

        @Test
        fun `NoOpPacketRelay drops packets without throwing`() {
            val relay: PacketRelay = NoOpPacketRelay()
            // Doit accepter buffer et size sans lancer d'exception.
            relay.onOutboundPacket(ByteArray(64) { it.toByte() }, 64)
            relay.onTunnelStarted()
            relay.onTunnelStopped()
        }

        @Test
        fun `Manifest declares LeVoileVpnService with BIND_VPN_SERVICE and foregroundServiceType vpn`() {
            val context = RuntimeEnvironment.getApplication()
            val component = android.content.ComponentName(
                context,
                LeVoileVpnService::class.java
            )
            val info = context.packageManager.getServiceInfo(component, PackageManager.GET_META_DATA)
            assertEquals("android.permission.BIND_VPN_SERVICE", info.permission)
            // FOREGROUND_SERVICE_TYPE_VPN = 0x00000400 (Android 14)
            // En Robolectric avec sdk=34, le champ est exposé.
            val expectedVpnFlag = 0x00000400
            assertTrue(
                "foregroundServiceType doit inclure VPN (0x400) — actuel : ${info.foregroundServiceType}",
                (info.foregroundServiceType and expectedVpnFlag) == expectedVpnFlag
            )
        }
    }
    ```
  - [x] **Si l'API Robolectric `getServiceInfo` est instable** (parfois différente entre versions) : reporter dans Debug Log et fallback parser le XML directement via `org.xmlpull.v1.XmlPullParserFactory.newInstance().newPullParser()` sur le fichier `app/src/main/AndroidManifest.xml`.
  - [x] Exécuter `cd android && ./gradlew :app:testDebugUnitTest`. Les 5 tests doivent passer.

- [x] **Task 8 : Patcher `README-android.md` — section « Capture L3 via VpnService »** (AC: #11)
  - [x] Lire l'état actuel de `android/README-android.md` (déjà patché Stories 9.2 puis 9.3).
  - [x] Ajouter la section décrite en AC #11 **après** la section « Lancement de l'app debug » (Story 9.3) et **avant** toute autre section future.
  - [x] **Important** : ne toucher AUCUNE autre section. Vérifier via `git diff android/README-android.md` qu'on n'introduit qu'une seule nouvelle section.

- [x] **Task 9 : Vérifications finales + git status check** (AC: tous)
  - [x] Exécuter dans cet ordre :
    1. `cd android && ./gradlew clean assembleDebug` — succès attendu, APK debug produit.
    2. `cd android && ./gradlew :app:testDebugUnitTest` — succès, les 5 tests `LeVoileVpnServiceConfigTest` passent + tests Story 9.3 (`MainActivityConfigTest`) passent toujours.
    3. `cd android && ./gradlew :app:lint` — pas de nouvelle erreur introduite par cette story (les warnings préexistants restent OK).
    4. `cd android && ./gradlew :app:processDebugManifest` — fusion manifest sans erreur.
    5. `cd android && ./gradlew assembleRelease` — succès, taille APK release < 25 MB.
    6. `cd android && apkanalyzer manifest permissions app/build/outputs/apk/release/app-release.apk` — vérifier la liste : `INTERNET`, `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, `FOREGROUND_SERVICE_VPN` (NOUVEAU), `POST_NOTIFICATIONS`, plus les 2 `*.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` auto-injectées par AGP 8 (cf. NFR-AND-7 prd.md l. 703 — autorisées). **Aucune autre permission** ne doit apparaître, sinon STOP et investiguer.
    7. **Test manuel sur émulateur** (si dispo) — voir AC #11 README. Sinon (cf. apprentissage Story 9.1+9.3 — aucun émulateur installé), reporter dans Completion Notes : « Test manuel non exécuté faute d'émulateur — sera couvert Story 12.6 (tests instrumentés Espresso) ». **Ne PAS installer un émulateur dans le scope de cette story** (overhead 5+ GB, hors périmètre).
  - [x] Exécuter `git status` à la racine du repo. Vérifier que **TOUS les changements sont sous `android/`** sauf : (a) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `backlog` → `review`), (b) `_bmad-output/implementation-artifacts/9-4-levoilevpnservice-creation-tun-pump-paquets-ip.md` (auto-update Status, Completion Notes, File List, Change Log). **Si un autre fichier hors `android/` apparaît modifié**, **STOP** et investiguer (voir « Périmètre de modification »).
  - [x] Reporter dans Completion Notes les métriques finales : taille APK debug, taille APK release, durée `assembleRelease`, durée `testDebugUnitTest`, choix Robolectric vs fallback XmlPullParser (Task 7), confirmation fusion manifest sans erreur, présence ou non d'émulateur pour test runtime, et toute alerte lint/CI à corriger Story suivante.

### Review Follow-ups (AI)

Issues identifiées lors du code-review adversarial post-implémentation (2026-05-03), toutes traitées dans la même session.

- [x] **[AI-Review][HIGH] H-1** Crash `ForegroundServiceDidNotStartInTimeException` sur `ACTION_DISCONNECT` invoqué via `startForegroundService()` — `disconnectInternal()` ne déclenchait jamais `startForeground()`. [LeVoileVpnService.kt:66-104](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L66-L104). **Fix** : `startForeground(NOTIF_ID, buildStubOngoingNotification())` invoqué défensivement au tout début d'`onStartCommand` (avant le `when` action), couvre les 3 chemins (CONNECT / DISCONNECT / unknown).
- [x] **[AI-Review][HIGH] H-2** `startForeground` invoqué APRÈS `establish()` au lieu d'avant — risque ANR sur OEM agressifs où `establish()` peut bloquer 3-7 s. [LeVoileVpnService.kt:87-104](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L87-L104). **Fix** : déplacé en tête d'`onStartCommand` (cf. H-1), retiré du `connectInternal()`.
- [x] **[AI-Review][HIGH] H-3** Action inconnue / intent null → `startForegroundService()` redélivré → crash car `startForeground()` jamais appelé. [LeVoileVpnService.kt:96-102](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L96-L102). **Fix** : branche `else` fait maintenant `stopForeground(STOP_FOREGROUND_REMOVE) + stopSelf()` après le `startForeground` défensif.
- [x] **[AI-Review][MEDIUM] M-4** Test `NoOpPacketRelay drops packets without throwing` ne couvrait pas le branche `Log.d` (déclenché tous les 1000 paquets). [LeVoileVpnServiceConfigTest.kt:80-93](../../android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt#L80-L93). **Fix** : boucle 1500 invocations dans le test ; le branche `Log.d` est désormais effectivement validé.
- [x] **[AI-Review][MEDIUM] M-5** `onTunnelStopped()` pouvait être appelé sans `onTunnelStarted()` correspondant (chemin `establish() == null` → `onDestroy` → `disconnectInternal`). [LeVoileVpnService.kt:54-186](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L54-L186). **Fix** : flag `tunnelStartedFired: Boolean` (volatile) ; `onTunnelStopped()` n'est appelé que si `tunnelStartedFired == true`. Garantit Story 9.7 un lifecycle Started→Stopped strict.
- [x] **[AI-Review][MEDIUM] M-6** Pump utilisait `pfd.fileDescriptor` partagé sans documenter le pattern de close-cascade ni le fallback vers `pfd.detachFd()`. [LeVoileVpnService.kt:170-178](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L170-L178). **Fix** : commentaires explicites dans `startPumpThreads` et `disconnectInternal` documentant la cascade EBADF et indiquant la bascule possible Story 9.7 vers `detachFd()` si race fd-reuse devient mesurable.
- [x] **[AI-Review][MEDIUM] M-7** `addRoute("::", 0)` IPv6 sans `addAddress(<ipv6>, ...)` correspondant — paquets v6 jamais routés via la TUN. [LeVoileVpnService.kt:107-122](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L107-L122). **Fix** : commentaire explicite dans `connectInternal()` documentant le comportement actuel (v6 hors tunnel — neutre car `NoOpPacketRelay`) et instruisant Story 9.7 à ajouter `.addAddress("fd00:6:6::2", 64)` (ULA) AVANT la route v6 si tunneling v6 réel attendu.
- [x] **[AI-Review][LOW] L-8** `vpnInterface`, `outPumpThread`, `inPumpThread` n'étaient pas `@Volatile` — visibilité cross-thread implicite via lifecycle. [LeVoileVpnService.kt:50-65](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L50-L65). **Fix** : `@Volatile` ajouté sur les 3 vars + sur `tunnelStartedFired`. L'invariant happens-before est désormais explicite dans le code.
- [x] **[AI-Review][LOW] L-9** `stopForeground(STOP_FOREGROUND_REMOVE)` invoqué sans `startForeground` préalable sur le chemin `establish() == null`. [LeVoileVpnService.kt:115-117](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L115-L117). **Fix** : auto-résolu par H-2 (startForeground désormais TOUJOURS appelé en tête d'`onStartCommand`, donc `stopForeground` a toujours un appairage symétrique).
- [x] **[AI-Review][LOW] L-10** `DocumentBuilderFactory` du test n'avait pas de hardening XXE / billion-laughs. [LeVoileVpnServiceConfigTest.kt:175-200](../../android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt#L175-L200). **Fix** : `disallow-doctype-decl` + `external-general-entities=false` + `external-parameter-entities=false` + `isXIncludeAware=false` + `isExpandEntityReferences=false`. Cohérent NFR9 (defense en profondeur).
- [x] **[AI-Review][LOW] L-11** Message d'erreur cwd-dependent du test trop laconique pour debug. [LeVoileVpnServiceConfigTest.kt:181-188](../../android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt#L181-L188). **Fix** : message enrichi avec `System.getProperty("user.dir")` + chemins absolus des candidats.

**11/11 findings traités. Validation re-exécutée** : `clean assembleDebug + testDebugUnitTest + lintDebug + assembleRelease` BUILD SUCCESSFUL en 32 s. **13 tests passent (7 Story 9.2 + 6 Story 9.4), 0 failure**. APK release toujours **23.10 MB** (< 25 MB). Aucune régression.

## Dev Notes

### Décisions architecturales contraignantes

- **ADR-08 — Isolation OS maximale** (architecture.md l. 2385-2388) : périmètre strict `android/`. **Aucune exception code partagé** pour cette story (contrairement à 9.2 qui ajoutait `golang.org/x/mobile` dans `go.mod` racine + 5 shims, et contrairement à 9.7 qui enrichira les shims pour la pompe Go réelle).
- **ADR-09 — gomobile pour le noyau Go partagé** (architecture.md l. 2390-2393) : **non consommé** par cette story. Le module `:levoile-core` reste branché Gradle Story 9.1, mais aucune classe gomobile n'est importée dans le code Kotlin de 9.4. La pompe paquets reste full-Kotlin (lecture `FileInputStream(fd)` + écriture `FileOutputStream(fd)`) avec une interface `PacketRelay` dont la seule implémentation Story 9.4 est `NoOpPacketRelay`. Story 9.7 livrera `GoBackedPacketRelay` qui consommera `GoCoreAdapter` (wrapper du `.aar`).
- **ADR-12 — Android API 29+ comme cible minimale** (architecture.md l. 2411) : `setBlocking(true)`, `addRoute("::", 0)` (IPv6), `setUnderlyingNetworks(null)`, `START_REDELIVER_INTENT`, `STOP_FOREGROUND_REMOVE` — toutes ces APIs sont disponibles à partir d'API 24/26, donc safe à API 29.
- **ADR-15 — Aucune télémétrie / crash reporter** (architecture.md l. 2427) : `LeVoileVpnService` log uniquement via `android.util.Log` (`Log.i`/`Log.w`/`Log.d`), jamais d'IP, jamais de payload paquet, jamais de session token. Le `Log.d` du `NoOpPacketRelay` est gardé par `BuildConfig.DEBUG` (R8 le strippe en release) — cohérent NFR-AND-9 (Story 10.5 finalisera le filtrage logcat par BuildType).
- **NFR-AND-1 — RAM < 60 MB** (prd.md l. 697) : à valider Story 9.7 quand le tunnel est réellement fonctionnel. Story 9.4 livre des threads daemon + un buffer 32 KB → footprint dominant resté JVM/AndroidX. Reporter dans Completion Notes la mesure approximative `adb shell dumpsys meminfo fr.plateformeliberte.levoile.debug` post-startService — si > 80 MB en idle (juste TUN créée), STOP et investiguer (probablement une fuite via thread non-daemon).
- **NFR-AND-2 — Tunnel < 3 s sur Pixel 6+ LTE/Wi-Fi RTT < 80ms** (prd.md l. 698) : non mesurable cette story (NoOpPacketRelay n'établit pas de tunnel chiffré). Story 9.7 mesurera via Trace API Android.
- **NFR-AND-3 — APK release < 25 MB** (prd.md l. 698-699) : marge confortable à ce stade. Tâche 9 vérifie (AC #10).
- **NFR-AND-7 — Permissions minimales** (prd.md l. 703) : ajout justifié de `FOREGROUND_SERVICE_VPN` documenté ci-dessous (Notes de divergence). Tous les autres permissions inchangées.
- **NFR-AND-11 — R8/ProGuard préservant les classes JNI** (livrée Story 9.1) : Story 9.4 ne consomme pas de classe JNI (`PacketRelay` est pure Kotlin, `LeVoileVpnService` étend `VpnService` dont la signature est stable côté SDK Android). Mais les rules `-keep class fr.plateformeliberte.levoile.core.**` Story 9.1 doivent rester intactes pour préparer Story 9.7. Vérifier que `proguard-rules.pro` n'introduit pas de strip pour le package `fr.plateformeliberte.levoile.vpn.**` (par défaut R8 préserve les classes utilisées par Android — `LeVoileVpnService` est référencée par le manifest, donc préservée automatiquement).

### Pourquoi un `PacketRelay` interface stub plutôt qu'un appel direct au noyau Go ?

- **Frontière étroite verrouillée par Story 9.7.** L'architecture (l. 1057-1066) exige que tous les appels Kotlin → Go passent par `GoCoreAdapter`. Story 9.4 vise à livrer un `LeVoileVpnService` qui crée la TUN et démarre les pumps **sans** introduire de dépendance vers `GoCoreAdapter` (qui n'existe pas encore — il arrive Story 9.7).
- **Une option alternative** aurait été : "intégrer immédiatement la création du `.aar` + l'instanciation `GoCoreAdapter` dans 9.4". **Rejetée** car :
  1. Le `.aar` doit être généré localement par `build-aar.sh` (Story 9.2) — non versionné. Le build CI Story 12.2 exécutera `build-aar.sh` puis `assembleDebug` ; à ce stade Story 9.7 sera livrée. Si Story 9.4 dépend du `.aar`, alors Story 9.4 n'est pas testable sans Story 9.2 entièrement opérationnelle (toolchain gomobile + NDK locale) — cassant l'incrémentalité.
  2. Si le `.aar` ne contient pas encore `GoBackedPacketRelay`, alors Story 9.4 introduit du code qui dépend d'une classe qui n'existe pas → build fail. Soit Story 9.4 livre une stub Kotlin de `GoCoreAdapter` (complexité accidentelle), soit on bloque sur la séquence 9.4 → 9.7 → tester.
- **Choix retenu** : interface `PacketRelay` Kotlin pure + `NoOpPacketRelay` par défaut. Story 9.7 livrera `GoBackedPacketRelay : PacketRelay` qui injecte `GoCoreAdapter.writePacket(...)` à l'intérieur de `onOutboundPacket(...)`. Le service ne change pas.
- **Contraintes héritées** : la signature `fun onOutboundPacket(buf: ByteArray, length: Int)` doit rester stable Story 9.7 — ne pas la modifier sans ADR. La signature évite de passer un `ByteBuffer` (gomobile mappe mal `java.nio.ByteBuffer`) et préfère `ByteArray` + `length` (gomobile mappe bien `[]byte`).

### Conventions Android (architecture.md l. 848-865, l. 1153-1183, l. 1201-1216)

- **Activity/Service** : `LeVoileVpnService` étend `VpnService` (qui étend `Service`), pas `AccessibilityService` ni `JobService`. Pattern singleton implicite via lifecycle Service Android (un seul service nommé instancié par OS, sauf si l'app appelle `startService()` à plusieurs reprises — `onStartCommand` est alors invoqué N fois sur la même instance ; le check `if (vpnInterface != null) return` au début de `connectInternal` gère le cas).
- **Threads pumps daemon** (architecture.md l. 1207) : `Thread { ... }.apply { isDaemon = true }`. Lecture/écriture fd bloquant kernel — NE PROFITE PAS des coroutines. Pas de `runBlocking` (architecture.md l. 1208). `Dispatchers.IO + SupervisorJob()` arrive Story 9.7 (besoin pour les suspend functions vers gomobile).
- **AtomicBoolean pour le flag `running`** : architecture.md l. 1211 (`AtomicReference<TunnelState>` cible mais `AtomicBoolean` suffit cette story).
- **Logging Android** : `Log.i` / `Log.w` / `Log.d` avec tag par classe (`TAG = "LeVoileVpnService"` en companion). **Jamais logger d'IP, de payload paquet, de session token** — même en debug (architecture.md l. 1034). Le `NoOpPacketRelay` log uniquement un compteur, pas le contenu paquet, et seulement en `BuildConfig.DEBUG`.
- **Tests Kotlin** : JUnit 4 + Robolectric (recommandé, cohérent Story 9.3). `@Config(sdk=[34])` pour la fidélité Android 14.
- **Localisation** : tous les textes utilisateur passent par `R.string.*` (architecture.md l. 1258). `vpn_session_name`, `vpn_notif_title_stub`, `notif_channel_status` ajoutés cette story.

### Apprentissages Stories 9.1+9.2+9.3 reproductibles

D'après les `Completion Notes` de Stories 9.1+9.2+9.3 et l'inspection du repo au 2026-05-02 :
- **Toolchain Android installée localement** : JDK 17, Gradle 8.7, Android SDK platforms;android-34. Pas de réinstall.
- **`gomobile` + NDK** : installé Story 9.2. **Pas requis pour cette story** — Story 9.4 ne fait pas de `gomobile bind` ni n'invoque `build-aar.sh`/`build-aar.ps1`. Le `.aar` n'a pas besoin d'exister pour `:app:assembleDebug` (Story 9.3 a confirmé cette hypothèse).
- **Aucun émulateur Android disponible localement** (apprentissage 9.1+9.3) → AC #4-#6 vérifiables uniquement en partie (compilation + tests JVM Robolectric). Test runtime complet via émulateur reporté à Story 12.6 (tests instrumentés Espresso).
- **`junit:4.13.2`** déjà ajouté à `app/build.gradle.kts` testImplementation Story 9.2. **Robolectric 4.12.2** + `androidx.test:core` ajoutés Story 9.3 Task 8 (à vérifier au début Story 9.4 — Task 1).
- **Réalité du repo post-9.3 (à NE PAS toucher dans 9.4)** :
  - `MainActivity.kt` (livré 9.3) — pas modifié.
  - `bridge/LeVoileBridge.kt` (livré 9.3) — pas modifié, reste limité à `getStatus()` placeholder. Story 11.2 ajoutera `connect()`/`disconnect()`.
  - `assets/{index.html,style.css,app.js}` (livré 9.3) — pas modifiés. Story 11.1 sync depuis `frontend/`.
  - `network_security_config.xml` (livré 9.1, vérifié 9.3) — pas modifié.
  - `themes.xml`, `colors.xml` (livré 9.1) — pas modifiés.
  - `levoile-core/` module (livré 9.1+9.2) — pas modifié.
  - `shims/{auth,crypto,leakcheck,protocol,registry}/*.go` (livré 9.2) — pas modifiés.
  - `go.mod`, `go.sum` racine (modifié 9.2) — pas modifiés.

### Anti-patterns à éviter

- ❌ **Ne pas démarrer `connectInternal()` sans `intent?.action == ACTION_CONNECT`** — un démarrage implicite (ex : sur action `null`) crée un tunnel à l'insu de l'utilisateur, surface d'attaque potentielle (un autre app pourrait bind le service). Le check `when (action)` est strict.
- ❌ **Ne pas appeler `establish()` sans avoir vérifié `VpnService.prepare()` côté MainActivity** — sinon `establish()` retourne `null` silencieusement et le service crashera au pump. Le check côté `MainActivity` arrive Story 9.5 ; Story 9.4 documente le contournement test (AC #3).
- ❌ **Ne pas oublier `startForeground(NOTIF_ID, ...)` dans les 5 secondes après `onStartCommand`** — sinon ANR et kill OS. Le code AC #4 appelle `startForeground` en fin de `connectInternal` — mesurer en logcat la latence (chronomètre `onStartCommand → startForeground`). Si > 5s, déplacer `startForeground` AVANT `establish()` (avec une notif "Connexion en cours..." de Story 9.5).
- ❌ **Ne pas oublier `STOP_FOREGROUND_REMOVE` lors du disconnect** — sinon la notification persiste après l'arrêt du tunnel, l'utilisateur croit être encore protégé.
- ❌ **Ne pas créer un `LeVoileBridge` avec une méthode `connect()` ou `disconnect()`** — Story 11.2 livrera. Cette story 9.4 ne touche PAS au bridge.
- ❌ **Ne pas créer un `NotificationHelper.kt`** complet (titre dynamique pays/IP, action Déconnecter) — Story 9.6. La notification stub de Story 9.4 est minimaliste **par design** (juste de quoi passer le `startForeground` < 5s). Tenter de pré-tirer la version finale crée du code obsolète qui devra être réécrit.
- ❌ **Ne pas créer de `KillSwitchHelper.kt`** — Story 11.5/11.6 (composant C15 onboarding kill switch + deeplink Settings.ACTION_VPN_SETTINGS). 9.4 ne sait pas que le kill switch existe (c'est OS-delegate).
- ❌ **Ne pas créer de `BootReceiver.kt`** — Phase 2 Epic 10/11 si demande utilisateur pour auto-start au boot.
- ❌ **Ne pas importer `gomobile.*` ni `fr.plateformeliberte.levoile.core.*`** — la frontière vers le `.aar` arrive Story 9.7 via `GoCoreAdapter`. Story 9.4 = 100% Kotlin pur + `androidx.core` + `androidx.appcompat`.
- ❌ **Ne pas modifier `frontend/` racine, `cmd/`, `internal/` racine, `windows/`, `linux/`** — son contenu est figé par les Stories 1-8 desktop. Story 9.4 = `android/` uniquement (sauf sprint-status.yaml + ce fichier story — auto-update).
- ❌ **Ne pas modifier `go.mod`/`go.sum` racine** — Story 9.2 a déjà ajouté `golang.org/x/mobile`. Story 9.4 = Kotlin pur, aucune raison d'y toucher. Si une dépendance Kotlin/Android nouvelle est requise (`androidx.webkit`, Robolectric), elle se déclare dans `android/app/build.gradle.kts` — pas dans `go.mod`.
- ❌ **Ne pas toucher à `android/shims/*.go`** — code Go consommé par gomobile bind Story 9.2. La surface réellement exposée à Kotlin (`Version()`, etc.) sera étendue Story 9.7. Si le pump de cette story semble appeler à enrichir un shim (ex : ajouter une fonction `protocol.WrapPacket(buf)`), c'est un signal qu'on déborde Story 9.4 — rester sur l'interface `PacketRelay` Kotlin et différer l'intégration Go à 9.7.
- ❌ **Ne pas invoquer `bash scripts/build-aar.sh` ou `pwsh scripts/build-aar.ps1` dans le scope de 9.4** — ces scripts appartiennent à 9.2 (livrés) et seront ré-invoqués Story 9.7 quand le `.aar` sera consommé pour de vrai. 9.4 réussit `./gradlew assembleDebug` même si `levoile-core/libs/levoile-core.aar` ou `app/libs/levoile-core.aar` n'existent pas localement (le `implementation(files("libs/levoile-core.aar"))` Story 9.2 est tolérant à l'absence du fichier au build — vérifier).
- ❌ **Ne pas activer `setNetwork(...)` ou `setUnderlyingNetworks(specificNetwork)` avec une `Network` explicite** — l'AC #4 spécifie `setUnderlyingNetworks(null)` (l'OS choisit). Forcer une interface réseau spécifique est hors scope et casse le failover Wi-Fi → 4G (Story 9.7+ via `ConnectivityObserver`).
- ❌ **Ne pas appeler `Process.killProcess(Process.myPid())` à la fin de `disconnectInternal()`** — c'est nuclear et empêche `onDestroy()` de tourner proprement. `stopSelf()` suffit.
- ❌ **Ne pas catch `Throwable` dans le pump out sans logger** — un swallow silencieux masquerait un crash fatal du `packetRelay` (Story 9.7 connectera la pompe à du code Go qui peut paniquer). Le `Log.w(TAG, "out-pump packetRelay error", t)` documente l'erreur tout en continuant la boucle (la TUN reste up, on ne perd pas le tunnel pour un paquet).

### Project Structure Notes

**Fichiers attendus livrés par cette story** (tous sous `android/`) :
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` (NOUVEAU)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/PacketRelay.kt` (NOUVEAU — interface + `NoOpPacketRelay`)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/VpnConstants.kt` (NOUVEAU)
- `android/app/src/main/AndroidManifest.xml` (MODIFIÉ — ajout `<service>` + ajout `<uses-permission FOREGROUND_SERVICE_VPN>` + mise à jour commentaire 9.1)
- `android/app/src/main/res/values/strings.xml` (MODIFIÉ — ajout 3 clés)
- `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ ou NOUVEAU — traductions FR)
- `android/app/src/main/res/drawable/ic_notification_stub.xml` (NOUVEAU — vector drawable mono-couleur, sera remplacé Story 9.6)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt` (NOUVEAU)
- `android/README-android.md` (MODIFIÉ — ajout section « Capture L3 via VpnService »)
- `android/app/build.gradle.kts` (MODIFIÉ uniquement si Robolectric non encore ajouté Story 9.3 — vérifier d'abord)

**Fichiers hors `android/` autorisés à modifier par cette story** :
- `_bmad-output/implementation-artifacts/sprint-status.yaml` : passage status story 9-4 `backlog` → `review`
- `_bmad-output/implementation-artifacts/9-4-levoilevpnservice-creation-tun-pump-paquets-ip.md` : auto-update (Status, Completion Notes, File List, Change Log)

**Aucun autre fichier hors `android/` ne doit être modifié.** Vérifier via `git status` final (Task 9).

### References

- [Source: epics.md#Story 9.4: `LeVoileVpnService` — création TUN + pump paquets IP (l. 1567-1591)]
- [Source: epics.md#Epic 9 — Noyau Android (l. 1494-1496)]
- [Source: prd.md#FR-AND-1 (l. 609)]
- [Source: prd.md#NFR-AND-1 (l. 697)]
- [Source: prd.md#NFR-AND-2 (l. 698)]
- [Source: prd.md#NFR-AND-3 (l. 698-699)]
- [Source: prd.md#NFR-AND-7 (l. 703) — permissions minimales auditables]
- [Source: prd.md#NFR-AND-11 (l. 707) — R8/ProGuard release]
- [Source: architecture.md#Selected Stack — Android-only VpnService + Foreground Service (l. 108-111)]
- [Source: architecture.md#Architecture mono-processus Android — LeVoileVpnService lifecycle (l. 586-607)]
- [Source: architecture.md#Permissions Android + service tag (l. 642-668)]
- [Source: architecture.md#Ordre de démarrage Android Connect (l. 754-767)]
- [Source: architecture.md#Logging Android (l. 1034) — jamais d'IP, payload, token]
- [Source: architecture.md#VpnService / Foreground Service / JNI Bridge Patterns (l. 1048-1087)]
- [Source: architecture.md#Concurrency Patterns Kotlin Android (l. 1201-1216)]
- [Source: architecture.md#UI Patterns Android — notification persistante (l. 1165-1182)]
- [Source: architecture.md#ADR-08 Isolation OS maximale (l. 2385-2388)]
- [Source: architecture.md#ADR-09 gomobile pour le noyau Go partagé (l. 2390-2393)]
- [Source: architecture.md#ADR-12 Android API 29+ (l. 2411)]
- [Source: architecture.md#ADR-15 Aucune télémétrie Android (l. 2427)]
- [Source: ux-design-specification.md#Hôte UI Android — Activity unique singleTop (l. 676)]
- [Source: 9-1-module-gradle-android-structure-projet.md (livrée 2026-05-02 — toolchain installée, structure Gradle, ProGuard rules, AndroidManifest minimal, themes.xml)]
- [Source: 9-2-script-build-aar-sh-gomobile-bind-du-noyau-go-partage.md (livrée 2026-05-02 — pattern « Périmètre de modification » strict, JUnit 4.13.2 testImplementation, 5 shims gomobile)]
- [Source: 9-3-mainactivity-squelette-webview-placeholder.md (livrée 2026-05-02 — Robolectric 4.12.2 testImplementation, MainActivity + WebView + body.platform-android, bridge stub)]
- [Memory: feedback_os_isolation — duplication code Win/Linux/Android préférée à abstraction partagée]

### Notes de divergence corrigées en amont

- **Divergence Manifest 9.1 vs Architecture (CORRIGÉE par cette story).** Le commentaire XML inséré Story 9.1 dans `AndroidManifest.xml` annonce `LeVoileVpnService sera livré Story 9.4 avec : android:foregroundServiceType="dataSync"`. **Ce commentaire est obsolète et erroné** : Android 14 (API 34) — qui est notre `targetSdk` (Story 9.1) — exige `foregroundServiceType="vpn"` pour tout `Foreground Service` qui étend `VpnService`. Utiliser `dataSync` provoquerait à terme un `SecurityException: Service must be of type VPN`. La spec architecturale (l. 659) confirme correctement `vpn|specialUse`. **Story 9.4 corrige** :
  - Manifest `<service>` : `foregroundServiceType="vpn"` (et non `"dataSync"`).
  - `<uses-permission FOREGROUND_SERVICE_VPN>` ajouté (Android 14 requirement).
  - Mise à jour du commentaire Story 9.1.
  - `<uses-permission FOREGROUND_SERVICE_DATA_SYNC>` reste **par cohabitation** : un futur `WorkManager` Story 12.5 (auto-update check 24h) sera typé `dataSync` et nécessite cette permission. Sans coût — NFR-AND-7 prd.md l. 703 liste explicitement `FOREGROUND_SERVICE_DATA_SYNC` comme acceptable.
- **Décision pump-in stub avec `Thread.sleep(5)` busy-wait léger.** L'AC #5 reconnaît cette dette technique : la pompe entrante n'a strictement rien à recevoir tant que Story 9.7 n'est pas livrée (le `packetSink` reste vide en permanence). Le `Thread.sleep(5)` évite un busy-wait CPU 100%. Story 9.7 remplacera par `BlockingQueue.take()` ou supprimera complètement la pompe in Kotlin au profit d'un callback Go direct vers `fos.write(...)` (architecture.md l. 1066 — choix benchmark à faire Story 9.7).
- **Décision channel notification distinct (`"levoile_vpn_status_stub"` cette story, `"levoile_vpn_status"` Story 9.6).** Permet de garder Story 9.4 isolée d'un préréglage IMPORTANCE_LOW intermédiaire qui interférerait avec celui livré finalement Story 9.6 (titre dynamique, sous-texte pays/IP, action Déconnecter). Story 9.6 fera explicitement `notificationManager.deleteNotificationChannel("levoile_vpn_status_stub")` au premier démarrage post-9.6 pour nettoyer les paramètres utilisateur (priority/vibration overrides) qu'un user aurait fait sur le channel stub.
- **Pas de modification `MainActivity.kt`.** L'orchestration UI ↔ Service (popup `VpnService.prepare()` + `startForegroundService(ACTION_CONNECT)`) appartient explicitement à Story 9.5. Story 9.4 livre le service capable de répondre aux Intents `ACTION_CONNECT`/`ACTION_DISCONNECT` ; Story 9.5 livre le code qui les envoie. Test manuel post-9.4 (sans 9.5) via `adb shell am start-foreground-service ...` (cf. AC #11 README).

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context) — `claude-opus-4-7[1m]`

### Debug Log References

**Décisions clés prises pendant le dev (2026-05-02) :**

1. **Robolectric NON ajouté** — État du repo post-9.2 confirmé absent (Story 9.3 = `ready-for-dev` mais non implémentée donc Robolectric jamais ajouté Task 8 de 9.3). Décision : fallback JVM-only (AC #9 explicitement autorise). Économie d'environ ~30 MB de deps + ~10s de build time. Story 9.3 / 11.x décideront s'il faut introduire Robolectric le moment venu (test du WebView).

2. **DOM `javax.xml.parsers.DocumentBuilderFactory` au lieu de `org.xmlpull.v1.XmlPullParserFactory`** — La factory XmlPullParser retournait `null` dans le stub Android JAR (effet `testOptions.unitTests.isReturnDefaultValues = true`), causant NPE au premier `.newPullParser()`. Bascule sur DOM standard JDK : aucune dépendance Android, parser garanti par JRE. AC #9 prévoyait explicitement ce fallback (« Décision dev à reporter dans Debug Log »).

3. **`testOptions.unitTests.isReturnDefaultValues = true` ajouté à `app/build.gradle.kts`** — Nécessaire pour que l'invocation `Log.d(...)` dans `NoOpPacketRelay.onOutboundPacket(...)` (test #4) ne plante pas avec `Method android.util.Log.d not mocked`. Toutes les autres APIs Android stubées de la même façon. Pattern documenté Google dev guide pour les unit tests JVM.

4. **DIVERGENCE corrigée — `foregroundServiceType="vpn"` REJETÉ par AAPT2 build-tools 34.0.0.** Le premier essai d'`assembleDebug` a échoué avec :
   ```
   ERROR: AndroidManifest.xml:49: AAPT: error: 'vpn' is incompatible with attribute
   foregroundServiceType (attr) flags [camera=64, connectedDevice=16, dataSync=1,
   health=256, location=8, mediaPlayback=2, mediaProjection=32, microphone=128,
   phoneCall=4, remoteMessaging=512, shortService=2048, specialUse=1073741824,
   systemExempted=1024]
   ```
   La valeur `vpn` n'existe PAS dans l'enum manifest (réservée au runtime via `setForegroundServiceType()` + `ServiceInfo.FOREGROUND_SERVICE_TYPE_VPN`). Bascule sur le pattern documenté **architecture.md l. 664-666** :
   - `foregroundServiceType="specialUse"` (valeur AAPT-acceptée),
   - `<property android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE" android:value="vpn" />` à l'intérieur du `<service>` (Android 14+ requis quand `specialUse`),
   - permission applicative `FOREGROUND_SERVICE_SPECIAL_USE` au lieu de `FOREGROUND_SERVICE_VPN`.
   Le test smoke a été mis à jour en conséquence (assertion `fgsTypeAttr == "specialUse"` + nouvelle assertion `<property>` SUBTYPE = `"vpn"`). Story spec 9.4 mentionnait `foregroundServiceType="vpn"` — corrigé ici par cette divergence documentée. **Le code-review devrait propager la correction au document architecture.md si nécessaire** (la spec architecture montre déjà `vpn|specialUse` avec property — la cible est cohérente, c'est juste la story 9.4 d'origine qui choisissait l'option flag `vpn` non-AAPT-acceptée).

5. **Test runtime VpnService NON exécuté** — Aucun émulateur Android installé localement (apprentissage hérité Stories 9.1+9.2). Validation AC #4-#5 limitée à compile + tests JVM. Le scénario complet (tap icône → consent popup → tunnel TUN créé → pumps tournent → ACTION_DISCONNECT propre) est porté par Story 12.6 (matrice Espresso instrumentés API 29/33/34).

6. **Modification noyau Go partagé** : `git status` confirme **AUCUNE modification** de `go.mod`, `go.sum`, `internal/*`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/` racine. ADR-08 isolation OS respectée. Périmètre `android/` strict tenu (cf. user instruction « le bon dossier "android" et pas à la racine sauf si exception de code partagé » — aucune exception nécessaire pour cette story).

7. **`stopForeground(STOP_FOREGROUND_REMOVE)`** : pas de `@Suppress("DEPRECATION")` requis (vérifié — `STOP_FOREGROUND_REMOVE` est un `Int` qui déclenche l'overload non-déprécié `stopForeground(int flags)`). Compilation propre sans warning.

8. **AAR gomobile présent dans `app/libs/levoile-core.aar`** : confirmé via `Unable to strip libgojni.so` au stripDebugDebugSymbols (le `.aar` Story 9.2 contient le `libgojni.so` déjà stripé). Si absent, `assembleDebug` aurait échoué — ce qui démontre que Story 9.2 est bien livrée localement.

### Completion Notes List

✅ **Implémentation complète des 11 ACs** (validation AC #9 = 6 tests JVM passent ; AC #4-#6 validation runtime reportée Story 12.6 faute d'émulateur).

**Métriques finales :**

| Métrique | Valeur | Critère |
|---|---|---|
| APK release size | **23.10 MB** | < 25 MB (NFR-AND-3) ✅ — marge serrée 1.9 MB, à surveiller Story 9.7 |
| APK debug size | 29.25 MB | hors limite officielle |
| `assembleRelease` durée | ~24 s | clean cold |
| `clean assembleDebug + testDebugUnitTest + lintDebug` durée | ~20 s | sur cache chaud |
| `testDebugUnitTest` | **6 tests Story 9.4 PASS** + 7 tests Story 9.2 PASS = 13 tests OK | AC #9 ✅ |
| `lintDebug` | aucune nouvelle erreur introduite | warnings préexistants Story 9.1 inchangés |
| Permissions APK release auditées (`apkanalyzer`) | 6 entrées : `INTERNET`, `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, **`FOREGROUND_SERVICE_SPECIAL_USE`** (NOUVEAU 9.4), `POST_NOTIFICATIONS`, `*.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` | NFR-AND-7 ✅ — toutes auditables, aucune dangereuse |
| Périmètre `git status` | tout sous `android/` + `_bmad-output/implementation-artifacts/{sprint-status.yaml, 9-4-*.md}` | ADR-08 isolation respectée ✅ |
| Toolchain | JDK 17.0.14 (Microsoft OpenJDK) + Gradle 8.7 + Android SDK platform-34 + build-tools 34.0.0 | Story 9.1 baseline confirmée |

**Périmètre user-instruction respecté** : « travailler dans le bon dossier "android" et pas à la racine sauf si exception de code partagé » — confirmé via `git status` final, aucune modification à la racine (go.mod, go.sum, internal/, etc.) introduite par cette story. Aucune exception code partagé n'a été nécessaire.

**Notes pour Story 9.5 / 9.6 / 9.7 (chaînage Epic 9) :**

- Story 9.5 (Foreground Service lifecycle + action « Déconnecter ») doit créer le `PendingIntent` de l'action notification vers `LeVoileVpnService.ACTION_DISCONNECT` et brancher `MainActivity` ↔ Service via `startForegroundService(Intent(this, LeVoileVpnService::class.java).setAction(ACTION_CONNECT))`. Le service est prêt à recevoir.
- Story 9.6 (Notification persistante MVP) doit (a) supprimer le channel `levoile_vpn_status_stub` au premier démarrage post-9.6 (`notificationManager.deleteNotificationChannel("levoile_vpn_status_stub")`), (b) recréer un channel `levoile_vpn_status` IMPORTANCE_LOW avec le titre dynamique « Le Voile · {État} », (c) remplacer `R.drawable.ic_notification_stub` par `R.drawable.ic_levoile_status` finalisé.
- Story 9.7 (Intégration noyau Go via `.aar`) : remplacer `private val packetRelay: PacketRelay = NoOpPacketRelay()` (ligne ~52 de `LeVoileVpnService.kt`) par `private val packetRelay: PacketRelay = GoBackedPacketRelay(GoCoreAdapter(this))` (ou injection via constructeur si DI introduit). La signature `PacketRelay.onOutboundPacket(buf, length)` est figée — ne pas la modifier.

**Risque résiduel (non bloquant) :** APK release 23.1 MB est à 1.9 MB de la limite NFR-AND-3 (25 MB). Story 9.7 (intégration Go core complète) augmentera potentiellement de quelques MB selon la surface réellement consommée. Si dépassement, plan de réaction : (1) split per-ABI (économie ~75% sur la `libgojni.so`), (2) audit des dépendances inutiles dans le `.aar` gomobile, (3) ProGuard rules plus agressives sur les classes Go non utilisées par Kotlin.

### File List

**Nouveaux fichiers (sous `android/app/`)** :
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/VpnConstants.kt`
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/PacketRelay.kt` (interface + `NoOpPacketRelay`)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt`
- `android/app/src/main/res/drawable/ic_notification_stub.xml`
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnServiceConfigTest.kt`

**Fichiers modifiés (sous `android/`)** :
- `android/app/src/main/AndroidManifest.xml` (ajout `<service>` + `<uses-permission FOREGROUND_SERVICE_SPECIAL_USE>` + mise à jour commentaire 9.1 obsolète)
- `android/app/src/main/res/values/strings.xml` (ajout `vpn_session_name`, `vpn_notif_title_stub`, `notif_channel_status`)
- `android/app/src/main/res/values-fr/strings.xml` (mêmes clés en français)
- `android/app/build.gradle.kts` (ajout `testOptions { unitTests.isReturnDefaultValues = true }`)
- `android/README-android.md` (ajout section « Capture L3 via VpnService (Story 9.4 livrée) » + mise à jour section « Permissions »)

**Fichiers modifiés hors `android/` (autorisés par story)** :
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (transition `ready-for-dev` → `in-progress` → `review`)
- `_bmad-output/implementation-artifacts/9-4-levoilevpnservice-creation-tun-pump-paquets-ip.md` (auto-update : Status, Tasks/Subtasks cochés, Dev Agent Record, File List, Change Log)

**Aucun fichier hors de cette liste n'a été modifié par cette story.**

## Change Log

| Date | Auteur | Changement |
|---|---|---|
| 2026-05-02 | create-story (Claude Opus 4.7) | Story 9.4 régénérée. Périmètre **strictement confiné à `android/app/`** SANS aucune exception code partagé Go (contrairement à Stories 9.2 et 9.7). VpnService + pumps Kotlin avec interface `PacketRelay` stubbée par défaut (`NoOpPacketRelay`) — Story 9.7 livrera `GoBackedPacketRelay` réel. Notification Foreground stub channel `"levoile_vpn_status_stub"` — Story 9.6 livrera la version finale. Manifest enrichi avec `<service>` + `FOREGROUND_SERVICE_VPN` (corrigeant le commentaire Story 9.1 qui annonçait erronément `foregroundServiceType="dataSync"` — Android 14 exige `vpn`). Test smoke Robolectric. Anti-patterns explicitement listés : ne pas pré-tirer Story 9.5 (UI ↔ Service), 9.6 (notif riche), 9.7 (Go core), 11.x (UI mobile). Status: ready-for-dev. |
| 2026-05-02 | dev-story (Claude Opus 4.7) | Implémentation livrée. **Divergence corrigée pendant le dev** : AAPT2 build-tools 34.0.0 refuse `foregroundServiceType="vpn"` (la valeur n'est pas dans l'enum manifest, réservée au runtime). Bascule sur le pattern documenté architecture.md l. 664-666 : `foregroundServiceType="specialUse"` + `<property PROPERTY_SPECIAL_USE_FGS_SUBTYPE="vpn"/>` + permission `FOREGROUND_SERVICE_SPECIAL_USE` (au lieu de `FOREGROUND_SERVICE_VPN`). **Décisions techniques** : Robolectric NON ajouté (fallback JVM-only autorisé par AC #9), DOM `javax.xml.parsers` au lieu de `XmlPullParser` (factory null sous returnDefaultValues), `testOptions.unitTests.isReturnDefaultValues = true` activé pour stuber `Log.d`. **Métriques** : APK release 23.10 MB (< 25 MB, marge serrée 1.9 MB), 6 tests JVM passent, lint sans nouvelle erreur. **Périmètre `git status` final** : 100 % sous `android/` + sprint-status + ce fichier story. Aucune exception code partagé n'a été nécessaire (cohérent user instruction « travailler dans le bon dossier "android" »). Status: review. |
| 2026-05-03 | code-review (Claude Opus 4.7) | Code-review adversarial post-implémentation : **11 findings identifiés (3 HIGH / 4 MEDIUM / 4 LOW), 11/11 fixés** dans la même session. **Bug critique évité** (H-1) : `ForegroundServiceDidNotStartInTimeException` garanti sur `ACTION_DISCONNECT` invoqué via `startForegroundService()` (pattern Story 9.5/9.6) car `disconnectInternal` ne déclenchait pas `startForeground`. Fix : `startForeground` défensif au tout début d'`onStartCommand` (couvre H-1/H-2/H-3 + auto-résout L-9). Autres fixes structurants : flag `tunnelStartedFired` pour lifecycle Started→Stopped strict (M-5, garantit Story 9.7), commentaires explicites sur cascade fd-close (M-6) et IPv6 sans addAddress (M-7), `@Volatile` sur vars cross-thread (L-8), test loop 1500x pour couvrir branche `Log.d` throttle (M-4), XXE hardening `DocumentBuilderFactory` (L-10), message diagnostic enrichi (L-11). Validation : `clean assembleDebug + testDebugUnitTest + lintDebug + assembleRelease` BUILD SUCCESSFUL en 32 s, 13 tests passent (7 Story 9.2 + 6 Story 9.4), 0 failure, APK release toujours 23.10 MB. Status: done. |

## Senior Developer Review (AI)

**Date** : 2026-05-03
**Reviewer** : Claude Opus 4.7 (code-review workflow adversarial)
**Outcome** : ✅ **APPROVED — All findings resolved**

### Résumé executive

11 issues identifiées au review (3 HIGH / 4 MEDIUM / 4 LOW), **11/11 traitées dans la même session**. Le bug le plus critique (H-1) aurait causé un crash garanti `ForegroundServiceDidNotStartInTimeException` dès que Story 9.5 ou 9.6 aurait branché l'action « Déconnecter » via `startForegroundService(ACTION_DISCONNECT)` — pattern documenté architecture.md l. 1170. Détecté avant régression utilisateur.

### Severity breakdown

| Severity | Found | Fixed | Status |
|---|---|---|---|
| HIGH | 3 | 3 | ✅ |
| MEDIUM | 4 | 4 | ✅ |
| LOW | 4 | 4 | ✅ |
| **Total** | **11** | **11** | **✅** |

### Action Items (tous cochés [x])

Voir section « Tasks/Subtasks → Review Follow-ups (AI) » ci-dessus pour le détail des 11 findings avec leurs fix locations (file:line). Synthèse :

- [x] **H-1** Crash `startForegroundService(DISCONNECT)` → `startForeground` défensif top-of-`onStartCommand`
- [x] **H-2** `startForeground` après `establish()` → moved to top
- [x] **H-3** Action inconnue + redélivery → branche `else` cleanup symétrique
- [x] **M-4** Test ne couvrait pas `Log.d` branche → loop 1500
- [x] **M-5** `onTunnelStopped` sans `Started` → flag `tunnelStartedFired`
- [x] **M-6** Pump fd-cascade non documentée → comments + path détachFd Story 9.7
- [x] **M-7** IPv6 route sans address → comment + path Story 9.7
- [x] **L-8** Vars sans `@Volatile` → ajouté
- [x] **L-9** `stopForeground` sans `start` → auto-résolu par H-2
- [x] **L-10** XXE hardening DOM → ajouté
- [x] **L-11** Message d'erreur cwd-laconique → enrichi avec `user.dir`

### Validation post-fix

- `cd android && ./gradlew clean :app:assembleDebug :app:testDebugUnitTest :app:lintDebug :app:assembleRelease` → **BUILD SUCCESSFUL en 32 s**
- 13 tests JVM passent (7 hérités Story 9.2 + 6 Story 9.4) — 0 failure, 0 error, 0 skipped
- `lintDebug` sans nouvelle erreur ni warning Story 9.4
- APK release : **23.10 MB** (< 25 MB NFR-AND-3, marge inchangée)
- Périmètre `git status` final : 100 % `android/` + `_bmad-output/implementation-artifacts/{sprint-status.yaml, 9-4-*.md}` (cohérent ADR-08 et user instruction « travailler dans le bon dossier "android" »)

### Note sur la limitation auto-review

Cette review a été menée par le **même** LLM (Claude Opus 4.7) que celui qui a écrit l'implémentation. Le workflow `code-review` recommande d'utiliser un LLM différent pour la review afin d'augmenter l'indépendance. Bien que 11 findings non triviaux aient été identifiés (incluant H-1 qui aurait été un vrai bug runtime), il reste possible que des biais cognitifs partagés (mêmes patterns mentaux, même connaissance des conventions) aient masqué d'autres issues. **Recommandation** : si une seconde paire d'yeux humain ou LLM différent est disponible avant de commencer Story 9.5, profitez-en pour un cross-check rapide ; à défaut, le code livré est solide et tous les tests passent.

### Risques résiduels (non bloquants)

1. **APK release size 23.10 MB / 25 MB NFR-AND-3** : marge réduite à 1.9 MB. Story 9.7 (intégration Go core complète) augmentera potentiellement le `.aar`. Plan de réaction documenté Completion Notes (split per-ABI, audit deps, ProGuard agressif).
2. **Test runtime VpnService non exécuté** : aucun émulateur Android local. Validation runtime complète reportée Story 12.6 (Espresso instrumentés API 29/33/34) — cohérent apprentissage Stories 9.1-9.3.
3. **Pump in busy-wait `Thread.sleep(5)` 200 itérations/s** : ~0.5 % CPU idle continue tant que `NoOpPacketRelay` est en place. Story 9.7 doit remplacer par `BlockingQueue.take()` ou supprimer la pompe in Kotlin au profit d'un callback Go direct (architecture.md l. 1066). **Pas un blocker pour 9.4**, design assumé.
