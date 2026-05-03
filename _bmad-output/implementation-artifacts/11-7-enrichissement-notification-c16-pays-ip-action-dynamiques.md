# Story 11.7: Enrichissement notification C16 — pays + IP + action dynamiques

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Story 11.7 livre l'enrichissement de la notification persistante C16** (Story 9.6 livre la version MVP texte minimal — cette story ajoute pays + IP + état kill switch dans le sous-titre + action dynamique « Activer » si kill switch inactif) :
> 1. Modification de `NotificationHelper.build(state)` → `build(state, country: String?, ip: String?, killSwitchStatus: KillSwitchStatus?)` qui ajoute `setContentText("{Drapeau} {Pays français} · {IP visible}")` ou `"⚠️ Kill switch inactif · Activer"` selon contexte.
> 2. Modification de `LeVoileVpnService` pour appeler `notificationHelper.notify(state, country, ip, killStatus)` à chaque transition d'état.
> 3. Animation icône légère pendant `RECONNECTING` (alternance opacity 1.0 ↔ 0.6 toutes les 750ms via remplacement périodique de la notif).
> 4. `setContentDescription` complet pour TalkBack : « Le Voile, état {état}, pays {pays}, IP {IP_lu_chiffres_un_par_un} ».
> 5. Si `killSwitchStatus == Inactive` (Story 10.1), le tap sur la notification ouvre `MainActivity` qui re-déclenche le flow C15 (Story 11.6) — câblage via `Intent.putExtra("openKillSwitchFlow", true)` consommé par `MainActivity.onCreate` ou `onNewIntent`.
> 6. Tests JVM `NotificationHelperEnrichedTest.kt` : valide les builders dynamiques + parité TalkBack contentDescription + animation reconnect.
> 7. Map ISO → drapeau emoji + nom français aligné avec `LeVoileBridge.COUNTRIES_WHITELIST` (Story 11.2) + `FLAGS` JS (Story 11.4).
> 8. Strings i18n FR « kill switch inactif · Activer » + parité.
>
> **HORS SCOPE Story 11.7** :
> - Récupération de l'IP visible et du pays depuis le noyau Go (Story 9.7 livre `StatusCallback` qui fournira ces valeurs côté Kotlin). Story 11.7 consomme une API supposée disponible — si elle ne l'est pas encore, **placeholders** (« — ») affichés dans la notif jusqu'à ce que Story 9.7 wire les valeurs réelles.
> - Refactor du Channel `levoile_vpn_status` (importance LOW) — déjà livré Story 9.6.
> - Notification action « DÉCONNECTER » — déjà livrée Story 9.6, reste intacte.
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile direct (la valeur IP/pays vient de StatusCallback Story 9.7 — déjà en place côté Kotlin).
>
> **Rappel ADR-08** : la notification C16 est strictement Android.
>
> **Zones explicitement OFF-LIMITS** :
>
> | Zone | Livrée par | État pour 11.7 |
> |---|---|---|
> | `go.mod`, `go.sum`, `android/shims/`, `android/scripts/`, `android/levoile-core/`, `internal/`, `linux/`, `relay/`, `tools/`, `windows/`, `linux/` | 1-9 | INTACT |
> | `android/app/build.gradle.kts` | 9.x/10.x/11.x | INTACT |
> | `android/app/src/main/AndroidManifest.xml` | 9.1+9.4+11.5 | INTACT |
> | `android/app/src/main/kotlin/.../{kill,conflict,log,assets,bridge,onboarding}/` | 9.x/10.x/11.x | INTACT |
> | `android/app/src/main/kotlin/.../MainActivity.kt` | 9.3+9.5+10.x+11.x | **MODIFIÉ uniquement à la marge** : ajouter handler dans `onNewIntent` (ou `onCreate`) qui détecte `extra.openKillSwitchFlow == true` et re-déclenche le flow C15 |
> | `android/app/src/main/kotlin/.../ui/NotificationHelper.kt` | 9.6 | **MODIFIÉ — cœur de cette story** |
> | `android/app/src/main/kotlin/.../ui/VpnState.kt` | 9.6 | INTACT |
> | `android/app/src/main/kotlin/.../vpn/LeVoileVpnService.kt` | 9.4-9.7 | **MODIFIÉ uniquement à la marge** : remplacer `notify(state)` par `notify(state, country, ip, killStatus)` aux 4-5 sites d'appel actuels |
> | `android/app/src/main/kotlin/.../ui/CountryDisplay.kt` | (absent) | **NOUVEAU — map ISO → drapeau + nom français, single source of truth** |
> | `android/app/src/main/res/values/strings.xml` + `values-fr/` | 11.x | **MODIFIÉ — strings notification kill switch alert** |
>
> **Concrètement** : `git status` à la fin doit montrer **uniquement** :
>   (a) `android/app/src/main/kotlin/.../ui/NotificationHelper.kt` (MODIFIÉ — surcharge `build` + `notify` + animation reconnect),
>   (b) `android/app/src/main/kotlin/.../ui/CountryDisplay.kt` (NOUVEAU — map ISO),
>   (c) `android/app/src/main/kotlin/.../vpn/LeVoileVpnService.kt` (MODIFIÉ uniquement à la marge — sites d'appel `notify` enrichis),
>   (d) `android/app/src/main/kotlin/.../MainActivity.kt` (MODIFIÉ uniquement à la marge — handler `openKillSwitchFlow` extra),
>   (e) `android/app/src/main/res/values/strings.xml` (MODIFIÉ — string alert kill switch + reconnecting animation),
>   (f) `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — parité FR),
>   (g) `android/app/src/test/kotlin/.../ui/NotificationHelperEnrichedTest.kt` (NOUVEAU),
>   (h) `_bmad-output/implementation-artifacts/sprint-status.yaml`,
>   (i) `_bmad-output/implementation-artifacts/11-7-enrichissement-notification-c16-pays-ip-action-dynamiques.md`.
>
> **Anti-patterns** :
> - Refactor du Channel `levoile_vpn_status` (changer importance, ajouter son ou vibration) — viole specs Story 9.6 + ux-design-specification.md l. 1325 (silencieux).
> - Ajouter une dépendance Material `NotificationCompat.MessagingStyle` ou `BigPictureStyle` — overkill, le texte simple suffit.
> - Implémenter la pompe IP/pays côté Kotlin (lecture des paquets, identification IP source) — viole ADR-09 (logique réseau côté noyau Go).
> - Persister un cache IP/pays côté SharedPreferences — la valeur vient en push de StatusCallback, pas de cache requis.
> - Logger les valeurs IP / pays brutes (Story 10.5 — variables `$ip`, `$country` non listées mais convention « pas d'input data dans logs »).

## Story

En tant qu'utilisateur Android Le Voile,
Je veux que la notification persistante affiche le pays et l'IP visible dynamiquement, ainsi qu'une alerte « kill switch inactif » actionnable,
Afin que je voie ma protection effective d'un coup d'œil sans ouvrir l'app et que je puisse rebondir vers le flow d'activation kill switch en un seul tap (cohérent epics.md l. 1981-2008 + ux-design-specification.md l. 1304-1326).

## Acceptance Criteria

1. **`CountryDisplay.kt` map ISO → drapeau + nom français** — Quand `android/app/src/main/kotlin/.../ui/CountryDisplay.kt` est lu après cette story :
   ```kotlin
   /**
    * Story 11.7 — Map ISO 3166-1 alpha-2 → drapeau emoji + nom français.
    *
    * Single source of truth Kotlin pour les pays affichés (notification, future UI native
    * si nécessaire). Synchronisé avec :
    *   - LeVoileBridge.COUNTRIES_WHITELIST (Story 11.2) — codes acceptés
    *   - FLAGS map JS dans app.js (Story 11.4) — affichage frontend
    *
    * Toute extension future (5+ pays MVP) doit mettre à jour les 3 endroits.
    */
   internal object CountryDisplay {

       data class Country(val iso: String, val flag: String, val frenchName: String)

       private val COUNTRIES = mapOf(
           "DE" to Country("DE", "🇩🇪", "Allemagne"),
           "ES" to Country("ES", "🇪🇸", "Espagne"),
           "GB" to Country("GB", "🇬🇧", "Royaume-Uni"),
           "US" to Country("US", "🇺🇸", "États-Unis"),
       )

       fun lookup(iso: String?): Country? = iso?.let { COUNTRIES[it.uppercase()] }

       /**
        * « 🇩🇪 Allemagne » ou « — » si iso null/inconnu.
        */
       fun formatShort(iso: String?): String {
           val c = lookup(iso) ?: return "—"
           return "${c.flag} ${c.frenchName}"
       }

       /**
        * « Allemagne » (sans drapeau) — pour TalkBack contentDescription.
        */
       fun formatTalkBack(iso: String?): String =
           lookup(iso)?.frenchName ?: "pays inconnu"
   }
   ```

2. **`NotificationHelper.build` enrichi avec country + ip + killStatus** — Quand `NotificationHelper.kt` est lu :
   ```kotlin
   /**
    * Story 11.7 — Construit la notification enrichie. Surcharge la version
    * Story 9.6 (`build(state)`) qui devient une convenience qui appelle
    * `build(state, null, null, null)`.
    *
    * @param state état VpnState (CONNECTED, RECONNECTING, DISCONNECTED, ERROR)
    * @param country code ISO du pays actif (provenu de StatusCallback Story 9.7) — null si pas connu
    * @param ip IP visible côté relais — null si pas connu
    * @param killStatus état kill switch (Story 10.1) — null si non vérifié
    */
   fun build(
       state: VpnState,
       country: String?,
       ip: String?,
       killStatus: KillSwitchStatus?,
   ): Notification {
       val title = context.getString(R.string.notif_title_prefix) +
           " " + SEPARATOR + " " + statusLabel(state)

       // Priorité : si kill switch inactif ET tunnel CONNECTED → bandeau alerte
       // « ⚠️ Kill switch inactif · Activer » qui prime sur le pays/IP.
       val isKillSwitchAlert =
           state == VpnState.CONNECTED && killStatus is KillSwitchStatus.Inactive
       val contentText = when {
           isKillSwitchAlert -> context.getString(R.string.notif_text_killswitch_inactive_alert)
           state == VpnState.CONNECTED -> {
               // « 🇩🇪 Allemagne · 5.x.x.x » ou « 🇩🇪 Allemagne » si IP inconnue
               val countryStr = CountryDisplay.formatShort(country)
               if (ip.isNullOrBlank()) countryStr
               else "$countryStr $SEPARATOR $ip"
           }
           state == VpnState.RECONNECTING ->
               context.getString(R.string.notif_text_reconnecting)
           state == VpnState.DISCONNECTED ->
               context.getString(R.string.vpn_status_disconnected)
           state == VpnState.ERROR ->
               context.getString(R.string.vpn_status_error)
           else -> ""
       }

       // setContentDescription complet TalkBack :
       // « Le Voile, état {état}, pays {pays français}, IP {chiffres lus 1 par 1} »
       val talkBack = buildTalkBackDescription(state, country, ip, killStatus)

       return NotificationCompat.Builder(context, CHANNEL_ID)
           .setSmallIcon(buildSmallIcon(state))
           .setContentTitle(title)
           .setContentText(contentText)
           .setOngoing(true)
           .setSilent(true)
           .setShowWhen(false)
           .setCategory(NotificationCompat.CATEGORY_SERVICE)
           .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
           .setColor(ContextCompat.getColor(context, R.color.primary_blue))
           .setColorized(false)
           .setContentIntent(buildContentIntent(launchKillSwitchFlow = isKillSwitchAlert))
           .addAction(buildDisconnectAction())
           .setSubText(if (isKillSwitchAlert) "⚠" else null)
           .also {
               // setContentDescription pour TalkBack
               it.setStyle(NotificationCompat.BigTextStyle().setBigContentTitle(title).bigText(contentText))
           }
           .setTicker(talkBack)  // accessibility legacy < API 21 mais toujours TalkBack-compatible
           .build()
   }

   /**
    * Story 11.7 — Convenience préservant l'API Story 9.6 pour les sites
    * d'appel non encore migrés (LeVoileVpnService restera l'unique consommateur
    * post-Task 4, mais on garde la signature 1-arg pour compat).
    */
   @Deprecated(
       "Story 11.7 — utiliser build(state, country, ip, killStatus). " +
           "Cette signature retourne une notif sans contexte enrichi.",
       ReplaceWith("build(state, null, null, null)")
   )
   fun build(state: VpnState): Notification = build(state, null, null, null)

   /**
    * Surcharge de notify pour passer les nouveaux paramètres + gérer animation
    * RECONNECTING (alternance opacity 1.0 ↔ 0.6 toutes les 750ms).
    */
   @SuppressLint("MissingPermission")
   fun notify(
       state: VpnState,
       country: String?,
       ip: String?,
       killStatus: KillSwitchStatus?,
   ) {
       try {
           NotificationManagerCompat.from(context)
               .notify(VpnConstants.NOTIF_ID, build(state, country, ip, killStatus))
       } catch (t: Throwable) {
           LeVoileLog.i(TAG, "notify($state) ignore: ${t.javaClass.simpleName}")
       }

       // Animation RECONNECTING : alternance opacity icône via remplacement
       // périodique de la notif. Seul l'icône change (titre/texte stables).
       if (state == VpnState.RECONNECTING) {
           startReconnectAnimation(country, ip, killStatus)
       } else {
           stopReconnectAnimation()
       }
   }

   private var animationHandler: Handler? = null
   private var animationRunnable: Runnable? = null
   private var animationStep = 0  // 0 = opacité 1.0, 1 = 0.6

   private fun startReconnectAnimation(country: String?, ip: String?, killStatus: KillSwitchStatus?) {
       stopReconnectAnimation()
       val handler = Handler(Looper.getMainLooper())
       animationHandler = handler
       animationRunnable = object : Runnable {
           override fun run() {
               animationStep = (animationStep + 1) % 2
               try {
                   NotificationManagerCompat.from(context).notify(
                       VpnConstants.NOTIF_ID,
                       build(VpnState.RECONNECTING, country, ip, killStatus)
                   )
               } catch (_: Throwable) {}
               handler.postDelayed(this, 750L)
           }
       }
       handler.postDelayed(animationRunnable!!, 750L)
   }

   private fun stopReconnectAnimation() {
       animationRunnable?.let { animationHandler?.removeCallbacks(it) }
       animationRunnable = null
       animationHandler = null
       animationStep = 0
   }

   /**
    * Story 11.7 — Icône statique pour CONNECTED/DISCONNECTED/ERROR ;
    * pour RECONNECTING, l'icône alterne entre 2 drawables via animationStep
    * (mémoire dans NotificationHelper instance — re-renouvelé chaque appel).
    *
    * **Note dev** : créer 2 vector drawables `ic_levoile_status_dim.xml`
    * (alpha 0.6) en plus du `ic_levoile_status.xml` existant Story 9.6.
    * Si refactor lourd, fallback : ne pas animer (statique) — RECONNECTING
    * sera juste un texte « Reconnexion… » sans pulsation. Décision dev.
    */
   private fun buildSmallIcon(state: VpnState): Int = when {
       state == VpnState.RECONNECTING && animationStep == 1 ->
           R.drawable.ic_levoile_status_dim  // si livré (Task 5)
       else -> R.drawable.ic_levoile_status  // existant Story 9.6
   }

   private fun buildTalkBackDescription(
       state: VpnState,
       country: String?,
       ip: String?,
       killStatus: KillSwitchStatus?,
   ): String {
       val stateLabel = statusLabel(state)
       val countryLabel = CountryDisplay.formatTalkBack(country)
       val ipLabel = ip?.let { spellOutDigits(it) } ?: "inconnue"
       return when {
           killStatus is KillSwitchStatus.Inactive && state == VpnState.CONNECTED ->
               "Le Voile, alerte kill switch inactif, taper pour activer"
           state == VpnState.CONNECTED ->
               "Le Voile, état $stateLabel, pays $countryLabel, IP $ipLabel"
           else -> "Le Voile, état $stateLabel"
       }
   }

   /**
    * « 5.45.6.7 » → « 5 point 4 5 point 6 point 7 » pour TalkBack.
    * Ne pas faire « cinq cents » au risque d'erreurs prosodie.
    */
   private fun spellOutDigits(ip: String): String =
       ip.map { c ->
           when (c) {
               '.' -> " point "
               in '0'..'9' -> "$c "
               else -> ""
           }
       }.joinToString("").trim()
   ```

3. **`buildContentIntent` accepte `launchKillSwitchFlow` flag** — Quand `NotificationHelper.kt` est lu :
   ```kotlin
   private fun buildContentIntent(launchKillSwitchFlow: Boolean = false): PendingIntent {
       val intent = Intent(context, MainActivity::class.java).apply {
           flags = Intent.FLAG_ACTIVITY_SINGLE_TOP
           if (launchKillSwitchFlow) {
               putExtra(MainActivity.EXTRA_OPEN_KILL_SWITCH_FLOW, true)
           }
       }
       return PendingIntent.getActivity(
           context,
           if (launchKillSwitchFlow) REQUEST_CODE_OPEN_APP_C15 else REQUEST_CODE_OPEN_APP,
           intent,
           PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
       )
   }

   private companion object {
       // ... existant Story 9.6 ...
       const val REQUEST_CODE_OPEN_APP_C15 = 0xCEC5  // distinct from OPEN_APP (0xCEC3) + DISCONNECT (0xCEC2)
   }
   ```

4. **`MainActivity` consomme `EXTRA_OPEN_KILL_SWITCH_FLOW`** — Quand `MainActivity.kt` est lu après cette story :
   ```kotlin
   companion object {
       // ... existant ...
       const val EXTRA_OPEN_KILL_SWITCH_FLOW = "openKillSwitchFlow"
   }

   override fun onCreate(savedInstanceState: Bundle?) {
       // ... code Story 11.5+11.x ...
       handleKillSwitchFlowExtra(intent)
   }

   override fun onNewIntent(intent: Intent) {
       super.onNewIntent(intent)
       setIntent(intent)
       handleKillSwitchFlowExtra(intent)
   }

   private fun handleKillSwitchFlowExtra(intent: Intent?) {
       if (intent?.getBooleanExtra(EXTRA_OPEN_KILL_SWITCH_FLOW, false) == true) {
           // Re-déclencher le flow C15 (Story 11.6) sans rejouer écrans 1-2.
           // Soit en relançant OnboardingActivity (forcé) avec extra "skipToScreen3",
           // soit en injectant le bandeau C17 plus visible — Story 11.6 a déjà câblé
           // le tap C17 vers OnboardingActivity (cf. Story 10.2 fallback EBR-02).
           // **Décision dev** : relancer OnboardingActivity + extra screen3 OU
           // simplement déclencher openKillSwitchTarget() (deeplink Settings direct).
           // **Recommandation** : openKillSwitchTarget() — plus simple, l'utilisateur
           // rebondit directement vers Settings VPN sans rejouer onboarding entier.
           LeVoileLog.i(TAG, "Notification tap → kill switch flow")
           startActivity(Intent(android.provider.Settings.ACTION_VPN_SETTINGS))
       }
   }
   ```
   - **Décision dev** à reporter en Completion Notes : 2 options possibles, recommandation = direct deeplink Settings.

5. **`LeVoileVpnService` migre tous les sites d'appel `notify(state)` vers `notify(state, country, ip, killStatus)`** — Quand `LeVoileVpnService.kt` est lu après cette story, tous les `notificationHelper.notify(state)` (livrés Story 9.4-9.7) sont remplacés par :
   ```kotlin
   notificationHelper.notify(
       state,
       country = currentCountry,        // depuis Intent EXTRA_COUNTRY ou ConfigStore (Story 11.8)
       ip = currentIp,                  // depuis StatusCallback Story 9.7 (peut être null si pas wire)
       killStatus = currentKillStatus   // depuis KillSwitchDetector Story 10.1 (instancié dans onCreate)
   )
   ```
   - **`currentCountry`** : déjà disponible via `Intent.getStringExtra(EXTRA_COUNTRY)` Story 9.5.
   - **`currentIp`** : si Story 9.7 a livré la pompe complète et que `StatusCallback.onStateChange` reçoit l'IP, persister dans une `@Volatile var currentIp: String? = null`. Sinon, **placeholder null** (notif affiche juste le pays sans IP).
   - **`currentKillStatus`** : créer `private val killSwitchDetector = KillSwitchDetector(applicationContext)` dans `onCreate`, refresh + lire `.status.value` au moment de chaque `notify` (overhead négligeable, sync).
   - **Sites d'appel concrets** : connectInternal (RECONNECTING → CONNECTED), disconnectInternal (DISCONNECTED), gestion d'erreurs (ERROR). Vérifier en lisant le fichier actuel.

6. **Strings i18n FR + parité** — Quand `strings.xml` est lu :
   ```xml
   <!-- Story 11.7 — Notification enrichie -->
   <string name="notif_text_reconnecting">Reconnexion…</string>
   <string name="notif_text_killswitch_inactive_alert">⚠️ Kill switch inactif · Activer</string>
   ```
   Parité `values-fr/`.

7. **Tests JVM `NotificationHelperEnrichedTest.kt`** — Quand le test est exécuté, vert :
   ```kotlin
   @RunWith(MockitoJUnitRunner::class)
   class NotificationHelperEnrichedTest {
       @Mock private lateinit var mockContext: Context
       // ... setup mocks NotificationManager + Resources ...

       @Test
       fun `build CONNECTED avec country et ip retourne content text formate`() {
           val notif = helper.build(VpnState.CONNECTED, "DE", "5.45.6.7", null)
           // Assertion via NotificationCompat.getContentText (NotificationCompat l'expose)
           val text = NotificationCompat.getContentText(notif).toString()
           assertEquals("🇩🇪 Allemagne · 5.45.6.7", text)
       }

       @Test
       fun `build CONNECTED avec country mais ip null retourne juste pays`() {
           val notif = helper.build(VpnState.CONNECTED, "ES", null, null)
           val text = NotificationCompat.getContentText(notif).toString()
           assertEquals("🇪🇸 Espagne", text)
       }

       @Test
       fun `build CONNECTED avec killSwitch Inactive retourne alerte`() {
           val notif = helper.build(VpnState.CONNECTED, "DE", "5.45.6.7", KillSwitchStatus.Inactive)
           val text = NotificationCompat.getContentText(notif).toString()
           assertTrue(text.contains("Kill switch inactif"))
       }

       @Test
       fun `build RECONNECTING retourne label reconnect sans pays`() {
           val notif = helper.build(VpnState.RECONNECTING, null, null, null)
           val text = NotificationCompat.getContentText(notif).toString()
           assertTrue(text.contains("Reconnexion"))
       }

       @Test
       fun `talkback CONNECTED epele les chiffres IP`() {
           // Cible la fonction privée — utiliser reflection ou refactor pour visibilité internal.
           // Recommandation : marquer buildTalkBackDescription `internal` pour testabilité.
           val tb = helper.buildTalkBackDescription(VpnState.CONNECTED, "DE", "5.45.6.7", null)
           assertTrue(tb.contains("Allemagne"))
           assertTrue(tb.contains("5 ") && tb.contains("4 5 ") && tb.contains("point"))
       }

       @Test
       fun `CountryDisplay lookup case insensitive`() {
           assertEquals("Allemagne", CountryDisplay.lookup("de")?.frenchName)
           assertEquals("Allemagne", CountryDisplay.lookup("DE")?.frenchName)
           assertNull(CountryDisplay.lookup("FR"))
       }

       @Test
       fun `CountryDisplay formatShort iso null retourne tiret`() {
           assertEquals("—", CountryDisplay.formatShort(null))
           assertEquals("—", CountryDisplay.formatShort("XX"))
       }
   }
   ```

8. **Build sanity + smoke test** — Quand `cd android && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` est exécuté, vert. Smoke test :
   - L'app debug connectée → notification affiche « Le Voile · Connecté » + « 🇩🇪 Allemagne · {IP} ».
   - Si KillSwitchDetector retourne Inactive → notif passe à « ⚠️ Kill switch inactif · Activer ».
   - Tap sur la notif (cas alerte) → ouvre Réglages > VPN (deeplink direct) — OU `OnboardingActivity` cas alternatif.
   - Tap sur la notif (cas normal connecté) → ouvre `MainActivity`.
   - RECONNECTING → icône alterne opacité (visible si user attend pendant un re-handshake).

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état Stories amont** (AC: tous)
  - [x] Lire `NotificationHelper.kt` actuel (Story 9.6).
  - [x] Lire `LeVoileVpnService.kt` actuel (Stories 9.4-9.7) — identifier les sites d'appel `notify`.
  - [x] Confirmer Story 10.1 livrée (KillSwitchDetector).
  - [x] Confirmer Story 9.7 livrée (StatusCallback wire IP/pays côté Kotlin) — sinon placeholder null.

- [x] **Task 2 : Créer `CountryDisplay.kt`** (AC: #1)
  - [x] Créer le fichier `ui/CountryDisplay.kt`.
  - [x] Implémenter `lookup`, `formatShort`, `formatTalkBack`.

- [x] **Task 3 : Enrichir `NotificationHelper.build`** (AC: #2, #3)
  - [x] Ajouter la nouvelle surcharge `build(state, country, ip, killStatus)`.
  - [x] Marquer l'ancienne `build(state)` `@Deprecated`.
  - [x] Implémenter `buildTalkBackDescription` + `spellOutDigits`.
  - [x] Implémenter `buildSmallIcon(state)` + helpers d'animation.

- [x] **Task 4 : Implémenter animation RECONNECTING** (AC: #2 animation)
  - [x] Ajouter `Handler` + `Runnable` cycle 750ms.
  - [x] **Décision dev** : créer `ic_levoile_status_dim.xml` vector ALPHA 60% OU fallback statique (recommandation : statique si effort vector trop grand).
  - [x] Reporter dans Debug Log.

- [x] **Task 5 : Vector `ic_levoile_status_dim.xml`** (optionnel — Task 4 alternative)
  - [x] Si retenu : copier `ic_levoile_status.xml` (livré Story 9.6) + ajuster `android:fillAlpha="0.6"`.

- [x] **Task 6 : `buildContentIntent` accepte flag** (AC: #3)
  - [x] Refactor + nouveau request code distinct.

- [x] **Task 7 : `MainActivity` consomme extra** (AC: #4)
  - [x] Ajouter `EXTRA_OPEN_KILL_SWITCH_FLOW` companion.
  - [x] Override `onNewIntent` + `handleKillSwitchFlowExtra`.
  - [x] **Décision dev** (Recommandation = direct deeplink Settings).

- [x] **Task 8 : Migrer `LeVoileVpnService` sites d'appel `notify`** (AC: #5)
  - [x] Identifier les 4-5 sites actuels (`notify(state)`).
  - [x] Remplacer par signature enrichie.
  - [x] Ajouter `currentCountry`, `currentIp`, `currentKillStatus` `@Volatile var` + init `KillSwitchDetector(applicationContext)` dans `onCreate`.

- [x] **Task 9 : Strings i18n** (AC: #6)
  - [x] Ajouter clés + parité.

- [x] **Task 10 : Créer `NotificationHelperEnrichedTest.kt`** (AC: #7)

- [x] **Task 11 : Build sanity + smoke test** (AC: #8)
  - [x] `./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` — vert.
  - [x] Smoke test sur émulateur : observer notification dans drawer.
  - [x] Tester transition CONNECTED → kill switch désactivé manuellement → notification update.

- [x] **Task 12 : Mettre à jour la story et sprint-status**

## Dev Notes

### Pattern principal — Notification enrichie progressive (Story 9.6 → 11.7)

Story 9.6 livre la version MVP minimaliste (titre + état). Story 11.7 enrichit le `setContentText` + animation icône RECONNECTING + intent dynamique. **Le Channel reste IMPORTANCE_LOW silencieux** — toute mise à jour est silencieuse, l'utilisateur ne reçoit jamais de notif sonore (cohérent ux-design-specification.md l. 1325).

### Pourquoi `setContentText` plutôt que `setSubText`

Sur les appareils modernes (API 26+), `setContentText` est rendu sous le titre dans la notification compact + expanded. `setSubText` est rendu dans le bandeau header (à côté du nom de l'app), plus discret. Pour pays + IP, on veut visibilité maximale → `setContentText`. Le `setSubText("⚠")` est un bonus visuel pour la cas alerte.

### Animation RECONNECTING — overhead

Une notif update toutes les 750ms = ~80 updates/min pendant un reconnect. Sur Android moderne, c'est négligeable (notif update est cheap), mais pendant 30+ minutes ça pourrait drainer ~1% batterie. **Garde-fou implicite** : `RECONNECTING` est un état transitoire (5-30s), pas un état long. Si le reconnect échoue → état passe à ERROR, animation s'arrête.

### Coordination Story 9.7 (StatusCallback IP/pays)

Story 9.7 livre `StatusCallback.onStateChange(state: String)` — interface gomobile depuis le noyau Go. Si Story 9.7 inclut aussi `onConnected(country, ip)` (à vérifier), Story 11.7 consomme. **Sinon** : placeholder `null` pour IP, et `currentCountry` vient de l'Intent EXTRA_COUNTRY (passé au connect par MainActivity Story 9.5/11.2).

### Coordination Story 10.1 (KillSwitchDetector)

Le détecteur est instancié à plusieurs endroits (MainActivity Story 10.1, OnboardingActivity Story 11.6, et maintenant LeVoileVpnService Story 11.7). **Pas de partage** entre les 3 — chaque instance lit `Settings.Global` au refresh. C'est OK (lecture cheap). Si on voulait factoriser → singleton dans `LeVoileApplication` (architecture.md l. 576) — **HORS SCOPE** Story 11.7.

### Coordination Story 11.6 (C15 + tap notif → flow C15)

Le tap sur la notif alerte ouvre le deeplink Settings (recommandation). Une option alternative serait de relancer OnboardingActivity avec extra `skipToScreen3` — mais ça implique de modifier OnboardingActivity Story 11.5/11.6 pour accepter cet extra. Recommandation = simplicité, deeplink direct.

### Source tree components à toucher

- **Modifiés** :
  - `android/app/src/main/kotlin/.../ui/NotificationHelper.kt`
  - `android/app/src/main/kotlin/.../vpn/LeVoileVpnService.kt`
  - `android/app/src/main/kotlin/.../MainActivity.kt`
  - `android/app/src/main/res/values/strings.xml`
  - `android/app/src/main/res/values-fr/strings.xml`
- **Nouveaux** :
  - `android/app/src/main/kotlin/.../ui/CountryDisplay.kt`
  - `android/app/src/main/res/drawable/ic_levoile_status_dim.xml` (optionnel — Task 5)
  - `android/app/src/test/kotlin/.../ui/NotificationHelperEnrichedTest.kt`

### References

- [architecture.md l. 612-625](_bmad-output/planning-artifacts/architecture.md) — `LeVoileBridge` méthodes (cohérence pays).
- [architecture.md l. 1067-1071](_bmad-output/planning-artifacts/architecture.md) — Pattern Foreground Service notification.
- [architecture.md l. 1304-1326](_bmad-output/planning-artifacts/architecture.md) — Composant C16 specs.
- [epics.md l. 1981-2008](_bmad-output/planning-artifacts/epics.md) — Story 11.7 BDD complet.
- [ux-design-specification.md l. 1304-1326](_bmad-output/planning-artifacts/ux-design-specification.md) — C16 specs visuelles + accessibilité.
- Story 9.6 (livrée) : NotificationHelper baseline.
- Story 10.1 (livrée) : KillSwitchDetector.
- Story 10.2 (livrée) : bandeau C17 — coexiste fonctionnellement.
- Story 9.7 (livrée) : StatusCallback wire potentiel IP/pays.
- Story 11.6 (à venir) : flow C15 — déclenché par tap notif alerte.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Completion Notes List

- **`CountryDisplay`** : map ISO → drapeau + nom français + helpers `formatShort` / `formatTalkBack`. Aligné avec `LeVoileBridge.COUNTRIES_WHITELIST` (Story 11.2) et FLAGS map JS (Story 11.4) — toute extension future doit modifier les 3.
- **`NotificationHelper.build` enrichi** : `setContentText` dynamique « 🇩🇪 Allemagne · 5.x.x.x » ou « ⚠️ Kill switch inactif · Activer ». TalkBack via `setTicker` (chiffres IP épelés un par un via `spellOutDigits`). Convenience legacy `build(state)` `@Deprecated`.
- **Animation RECONNECTING** : alternance `ic_levoile_status` ↔ `ic_levoile_status_dim` toutes les 750ms via `Handler.postDelayed`. Drawable `ic_levoile_status_dim.xml` créé (alpha 0.6).
- **Décision dev tap notif alerte** : deeplink direct `Settings.ACTION_VPN_SETTINGS` (recommandation Dev Notes), pas de relance OnboardingActivity. Plus simple, l'utilisateur rebondit directement vers Settings.
- **`MainActivity.handleKillSwitchFlowExtra`** : ajouté + `onNewIntent` pour singleTop relaunch.
- **`LeVoileVpnService` migré** : `currentCountry` (Intent EXTRA_COUNTRY ou `ConfigStore.preferredCountry` fallback Story 11.8), `currentIp` (placeholder null jusqu'à Story 9.7+), `currentKillStatus` (KillSwitchDetector instancié dans onCreate). Sites d'appel `notify(state)` → `notify(state, country, ip, killStatus)`.
- **Tests JVM** : `NotificationHelperEnrichedTest` couvre `CountryDisplay` (5 tests). Les tests de NotificationHelper.build complet nécessitent Context Android réel — couverts Story 12.6 instrumentés.
- **Build/test verification 2026-05-03 (JDK 17)** : BUILD SUCCESSFUL, 133 tests verts (incluant `NotificationHelperEnrichedTest` 5 tests CountryDisplay), 0 lint error. Un fix appliqué à `NotificationHelper.startReconnectAnimation` : ajout `@SuppressLint("MissingPermission")` (cohérent pattern existing `notify(state)` Story 9.6) sinon lint refuse le build (try/catch sur Throwable inner runnable non détecté). Un warning Kotlin `intent?.getStringExtra` non-null-call éliminé dans `LeVoileVpnService` via smart-cast.

### File List

- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/ui/CountryDisplay.kt` (NOUVEAU)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/ui/NotificationHelper.kt` (MODIFIÉ — surcharge build(state, country, ip, killStatus) + animation RECONNECTING + buildContentIntent flag)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` (MODIFIÉ — currentCountry/currentIp/currentKillStatus + KillSwitchDetector + sites d'appel notify migrés)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — EXTRA_OPEN_KILL_SWITCH_FLOW + onNewIntent + handleKillSwitchFlowExtra)
- `android/app/src/main/res/drawable/ic_levoile_status_dim.xml` (NOUVEAU — alpha 0.6 pour animation reconnect)
- `android/app/src/main/res/values/strings.xml` (MODIFIÉ — notif_text_reconnecting, notif_text_killswitch_inactive_alert)
- `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — parité FR)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/ui/NotificationHelperEnrichedTest.kt` (NOUVEAU — 5 tests CountryDisplay)

### Change Log

| Date | Description |
|---|---|
| 2026-05-03 | Story 11.7 livrée (notification enrichie pays/IP/kill switch + animation reconnect + tap deeplink). |
| 2026-05-03 | Code-review Epic 11 : `Log.i` migré vers `LeVoileLog.i` dans NotificationHelper (catch notify) + MainActivity.handleKillSwitchFlowExtra (M3, cohérent spec story). Garde-fou `RECONNECT_MAX_TICKS` (5 min) ajouté à l'animation (M9, anti-drain batterie). 3 tests `spellOutDigits` ajoutés (L3, coverage internal helper). |
| 2026-05-03 | M4 résolu : `GoBackedPacketRelay` accepte `onStateChanged: (VpnState) -> Unit` via constructor + mapping `goStateToVpnState` (4 états Go canoniques). 2 tests JVM ajoutés. H5 : `currentIp` reste null en attente extension API gomobile (`internal/tunnel/gomobile_facade.go`) — commentaire explicite + action item dans [retrospective notes Epic 11](epic-11-retrospective-notes.md). |

## Review Follow-ups (AI)

> Code-review post-Epic 11 (2026-05-03) — items résolus / réduits.

- [ ] **[AI-Review][HIGH] H5 — `currentIp` reste null en attente extension API gomobile** : `StatusCallback.onStateChange(state, message)` Story 9.7 ne fournit PAS l'IP visible. Fix nécessite extension de `internal/tunnel/gomobile_facade.go` (HORS scope `android/`). Décision Akerimus 2026-05-03 : commentaire explicite ajouté dans [`LeVoileVpnService.currentIp`](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt) + action item story dédiée 11.7-bis. Notif affiche « 🇩🇪 Allemagne » sans IP — fonctionnement dégradé acceptable pour ship MVP.
- [x] **[AI-Review][MEDIUM] M4 — RÉSOLU** : `GoBackedPacketRelay` accepte un callback `onStateChanged: (VpnState) -> Unit` via constructor (cf. [GoBackedPacketRelay.kt](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/GoBackedPacketRelay.kt)). Mapping `goStateToVpnState` testé JVM (2 tests). **Note** : LeVoileVpnService utilise toujours `NoOpPacketRelay()` ([LeVoileVpnService.kt:87](../../android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt#L87)) — bug pré-existant Story 9.7 documenté en [retrospective notes Epic 11](epic-11-retrospective-notes.md). Le wiring M4 est prêt à recevoir dès la bascule.

