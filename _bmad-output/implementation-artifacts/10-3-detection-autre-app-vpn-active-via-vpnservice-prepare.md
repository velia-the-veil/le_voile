# Story 10.3: Détection autre app VPN active via `VpnService.prepare()`

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Aucune exception « code partagé » n'est nécessaire pour cette story.** Story 10.3 livre une classe Kotlin `VpnConflictDetector.kt` qui combine `VpnService.prepare(context)` + heuristique `Settings.Global.always_on_vpn_app` pour distinguer 3 verdicts : `NoConflict` (cas premier-lancement, popup consent à présenter), `ConsentNotGiven` (cas premier-lancement où Le Voile lui-même n'a pas encore consenti — popup système à présenter), `ForeignVpnActive` (autre app VPN détient le slot — refus avec UI explicite). Le détecteur **réutilise la couche `SettingsReader`** introduite Story 10.1 (DI testable). Aucun fichier Go n'est lu, créé ou modifié. Aucun appel JNI vers le `.aar`. Aucun fichier sous `android/shims/`, `android/scripts/`, `android/levoile-core/` n'est touché.
>
> **Rappel ADR-08 (architecture.md l. 2397-2400) — isolation OS maximale.** La détection de conflit VPN est par construction Android-spécifique (`VpnService.prepare()` API Android pure). Toute tentative de "factoriser" cette détection avec une couche Windows/Linux est une violation directe d'ADR-08 — refusée en code review. Sur desktop, la détection de VPN concurrent vit dans `internal/vpndetect/` (Story 2.3, livrée Phase 1) avec sémantique différente (interfaces réseau scannées) — aucune mutualisation avec Android.
>
> **Zones explicitement OFF-LIMITS pour cette story** (livrées par 9.x/10.1/10.2, intactes pour 10.3) :
>
> | Zone | Livrée par | État pour 10.3 |
> |---|---|---|
> | `go.mod` / `go.sum` racine | Story 9.2 | INTACT |
> | `android/shims/*.go` (5 shims gomobile) | Story 9.2 | INTACT — code Go, pas Kotlin |
> | `android/scripts/*` | Story 9.2 / Story 11.1 (à venir) | INTACT |
> | `android/levoile-core/*` | Story 9.1+9.2 | INTACT |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/{KillSwitchDetector.kt,KillSwitchStatus.kt}` | Story 10.1 | INTACT — Story 10.3 ne modifie PAS ces classes |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/SettingsReader.kt` | Story 10.1 | **CONSOMMÉE en lecture** — Story 10.3 importe cette interface comme dépendance, pas de modification de signature |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` | Story 9.4 | INTACT — la détection conflit est en amont du démarrage du service. Le service ne sait rien du conflit (il est démarré uniquement si `VpnConflictDetector` retourne `NoConflict`) |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` | Stories 9.3+10.1+10.2 | **MODIFIÉ uniquement à la marge** — voir AC #6 (ajout d'un membre `private lateinit var vpnConflictDetector` + helper `private fun checkVpnConflictAndConnect(...)` de pré-flight, **sans encore brancher au bouton « Connecter »** car le bouton lui-même n'existe pas avant Story 11.2). |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` | Stories 9.3+10.2 | **MODIFIÉ uniquement à la marge** — voir AC #7 (ajout 1 méthode `@JavascriptInterface fun checkVpnConflict(): String` qui expose le verdict côté JS, en préparation de Story 11.2 qui consomme cette méthode dans `connect()`) |
> | `android/app/src/main/AndroidManifest.xml` | Story 9.1 | INTACT — `VpnService.prepare()` ne nécessite pas de permission spécifique (la permission `BIND_VPN_SERVICE` est attachée au tag `<service>` de `LeVoileVpnService` Story 9.4, indépendant de cette story) |
> | `android/app/build.gradle.kts` + `android/gradle/libs.versions.toml` | Stories 9.1+10.1 | INTACT — aucune nouvelle dépendance Gradle requise |
> | `internal/{tunnel,tun,firewall,routing,ui,ipc,wfp,nftables,vpndetect,...}` racine + `windows/`, `linux/`, `cmd/`, `deploy/` | Stories 1-8 | INTACT — hors arbre `android/` |
>
> **Concrètement** : `git status` à la fin de la session dev doit montrer **uniquement** :
>   (a) entrées sous `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/` (NOUVEAU — 2 fichiers : `VpnConflictDetector.kt`, `VpnConflictVerdict.kt`),
>   (b) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — ajout `lateinit var vpnConflictDetector` + helper privé pré-flight ; aucune modif de l'observer ni de l'appel `refresh()` Story 10.1/10.2),
>   (c) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (MODIFIÉ — ajout 1 méthode `@JavascriptInterface` ; aucune autre modif),
>   (d) `android/app/src/main/res/values/strings.xml` + `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉS — ajout 2 clés pour les messages d'erreur conflit + bouton « Ouvrir paramètres VPN »),
>   (e) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictDetectorTest.kt` (NOUVEAU — tests JVM-only stub-injectés),
>   (f) `android/README-android.md` (MODIFIÉ — section « Détection conflit VPN »),
>   (g) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `backlog`/`ready-for-dev` → `review`),
>   (h) `_bmad-output/implementation-artifacts/10-3-detection-autre-app-vpn-active-via-vpnservice-prepare.md` (auto-update Status, File List, Completion Notes, Change Log).
>
> **Aucune entrée à la racine** (`go.mod`, `go.sum`, `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`, `_bmad/`), **aucune entrée sous `android/shims/`, `android/scripts/`, `android/levoile-core/`, `android/app/src/main/assets/`, `android/app/src/main/kotlin/fr/plateformeliberte/levoile/{kill/,vpn/}`**. Tout autre fichier modifié est un side-effect non prévu — **STOP, investiguer, ne pas tenter de "lisser" en supprimant les changements**. Reporter dans Debug Log et demander avant de poursuivre.
>
> **Anti-pattern fréquent à éviter** : tenter de pré-tirer en avance des fichiers prévus par les stories suivantes — implémenter le bouton « Connecter » côté frontend (Story 11.2 — `window.LeVoile.connect()` complet), enrichir `LeVoileVpnService` avec une logique de conflit côté service (Story 9.4 livre le service sans cette logique ; le conflit est traité **AVANT** le démarrage du service côté `MainActivity` ou `LeVoileBridge`), introduire un dialog Material en `View` Android natif (le message d'erreur conflit est rendu côté WebView en HTML — cohérent ADR-14 — pas en `AlertDialog`), pré-implémenter `OnboardingActivity` pour gérer le popup consent VpnService (Story 11.5 — le popup consent prep dans cette story est juste un retour de string au JS, le `startActivityForResult` réel viendra Story 11.5). Cette story livre **uniquement** un détecteur métier + un bridge expose-statut + un message d'erreur prêt à être consommé par Story 11.2 quand le bouton « Connecter » sera implémenté. Si tu te retrouves à appeler `startActivityForResult(...)` ou à modifier `LeVoileVpnService.kt`, tu es hors-scope.

## Story

En tant qu'utilisateur Android,
Je veux que l'app détecte AVANT de démarrer le tunnel si une autre application VPN (Tailscale, WireGuard, OpenVPN, NordVPN, etc.) détient déjà le slot VPN Android (et donc qu'un tunnel Le Voile entrerait en conflit) ou si le consentement utilisateur n'a pas encore été donné pour Le Voile,
Afin que je ne sois jamais connecté « à l'aveugle » dans un état incohérent — cohérent FR-AND-6 (prd.md l. 614), `VpnService.prepare()` Android API officielle, et UX explicite (« Une autre application VPN est active sur cet appareil. Désactivez-la pour utiliser Le Voile. ») avec deeplink `Settings.ACTION_VPN_SETTINGS`.

## Acceptance Criteria

1. **`VpnConflictVerdict` est une `sealed class` à 3 valeurs** — Quand `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictVerdict.kt` est lu, il déclare :
   ```kotlin
   sealed class VpnConflictVerdict {
       /** Aucun conflit, aucun consent à demander — le tunnel peut démarrer immédiatement. */
       object NoConflict : VpnConflictVerdict()

       /**
        * Consentement Le Voile pas encore donné (premier lancement, ou consent révoqué).
        * `prepareIntent` contient l'Intent à passer à `startActivityForResult(...)` côté MainActivity
        * pour présenter le popup système Android natif.
        */
       data class ConsentNotGiven(val prepareIntent: android.content.Intent) : VpnConflictVerdict()

       /**
        * Une autre app VPN détient le slot. `foreignAppId` peut être null
        * (cas où prepare() retourne non-null mais Settings.Global indique l'absence d'always_on_vpn_app —
        * historiquement rare, mais possible si un VPN tier a été désinstallé sans nettoyer son slot).
        */
       data class ForeignVpnActive(val foreignAppId: String?) : VpnConflictVerdict()
   }
   ```
   **Important** :
   - Le `data class ConsentNotGiven(val prepareIntent: Intent)` **transporte l'Intent** retourné par `VpnService.prepare()` — c'est crucial car cet Intent est **non-recréable** par l'app (Android le construit côté framework et il est valide UNIQUEMENT dans la fenêtre temporelle du `prepare()` call). Si on ne le transporte pas, on perd la capacité de présenter le popup. La Story 11.5 (`OnboardingActivity`) consommera ce verdict via le bridge JS pour appeler `startActivityForResult(prepareIntent, REQ_VPN_PREPARE)`.
   - **Pas de variante `Unknown` ou `ErrorReadingSettings`** — l'heuristique `Settings.Global` qui peut échouer est traitée à l'AC #4 par la classification : si `prepare() == null`, c'est `NoConflict` (le consent existe et notre slot est libre — peu importe l'heuristique). Si `prepare() != null`, on a besoin de l'heuristique pour distinguer `ConsentNotGiven` (l'utilisateur n'a jamais consenti, slot vide) de `ForeignVpnActive` (un autre VPN détient le slot). Si l'heuristique échoue dans le second cas, on classifie **prudemment** comme `ConsentNotGiven` (le consent existant n'est pas garanti pour Le Voile, donc le présenter est sûr — pire cas : l'utilisateur voit un popup inutile mais aucune perte de protection).

2. **`VpnConflictDetector` existe sous `conflict/` avec API publique restreinte** — Quand `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictDetector.kt` est lu, il déclare :
   ```kotlin
   class VpnConflictDetector(
       private val context: android.content.Context,
       private val settingsReader: SettingsReader = ContentResolverSettingsReader(context.contentResolver),
       private val expectedAppId: String = BuildConfig.APPLICATION_ID,
   ) {
       fun check(): VpnConflictVerdict { /* ... voir AC #4 ... */ }
   }
   ```
   API publique : **uniquement** la méthode `fun check(): VpnConflictVerdict`. Le détecteur n'expose pas de `LiveData` (différent de `KillSwitchDetector` Story 10.1) — la détection de conflit est ponctuelle (au tap « Connecter »), pas continue. Pas de cache. Chaque appel re-checke depuis zéro (`prepare()` + `Settings.Global.getString`).

   La signature d'injection (`SettingsReader`, `expectedAppId`) reprend le pattern Story 10.1 pour testabilité JVM-only sans Robolectric.

3. **`VpnConflictDetector.check()` invoque `VpnService.prepare(context)` en premier** — Quand `check()` est exécutée, la première opération est :
   ```kotlin
   val prepareIntent: android.content.Intent? = android.net.VpnService.prepare(context)
   if (prepareIntent == null) {
       return VpnConflictVerdict.NoConflict
   }
   ```
   **Important** :
   - `VpnService.prepare()` est **statique sur la classe `android.net.VpnService`** — pas besoin d'instancier le service ni d'avoir le service running. Cohérent architecture.md l. 580-584.
   - Le retour `null` signifie : « le consent a déjà été donné par l'utilisateur pour cette app ET aucun autre VPN ne détient le slot ». C'est l'état idéal — on peut démarrer le tunnel immédiatement.
   - Le retour non-null signifie : « il y a quelque chose à régler » (soit consent jamais donné, soit autre VPN actif) — on passe à l'AC #4 pour distinguer.
   - **Ne PAS** ignorer le retour — si on appelle `prepare()` sans utiliser le résultat, le contrat Android est respecté côté technique (le slot est marqué « ready to be requested ») mais on perd l'info nécessaire pour décider quoi faire ensuite.

4. **Si `prepareIntent != null`, l'heuristique `Settings.Global.always_on_vpn_app` distingue les 2 cas restants** — Quand `prepareIntent` est non-null, la suite de `check()` est :
   ```kotlin
   val pinnedApp: String? = try {
       settingsReader.getString("always_on_vpn_app")
   } catch (t: Throwable) {
       // Heuristique cassée → classification prudente : on présume ConsentNotGiven
       // (présenter le popup système est toujours sûr — pire cas, l'utilisateur consent à nouveau).
       return VpnConflictVerdict.ConsentNotGiven(prepareIntent)
   }
   return when {
       // Cas 1 : pas de VPN permanent configuré OU c'est le nôtre.
       // Le prepareIntent non-null signifie alors « consent jamais donné par
       // l'utilisateur pour Le Voile ». Présenter le popup système.
       pinnedApp == null -> VpnConflictVerdict.ConsentNotGiven(prepareIntent)
       pinnedApp == expectedAppId -> VpnConflictVerdict.ConsentNotGiven(prepareIntent)
       // Cas 2 : un autre VPN détient le slot. Conflit explicite.
       else -> VpnConflictVerdict.ForeignVpnActive(pinnedApp)
   }
   ```
   **Important** :
   - La distinction est **critique** pour l'UX : un utilisateur premier-lancement attend de voir le popup consent (cas 1). Un utilisateur avec Tailscale actif attend un message clair de refus (cas 2). Mélanger les deux serait grossier.
   - **Cas particulier `pinnedApp == expectedAppId`** : cela peut arriver si l'utilisateur a activé « VPN permanent » pour Le Voile MAIS a ensuite révoqué le consent VPN (Settings → Apps → Le Voile → Permissions → VPN → Révoquer). C'est rare mais possible. Dans ce cas, `prepare()` retourne non-null (consent révoqué) mais `pinnedApp` est notre app — la classification est `ConsentNotGiven` (re-présenter le popup), pas `ForeignVpnActive`. La logique du `when` ci-dessus est correcte.
   - Le `Throwable` catch est volontairement large : `SecurityException` (ROM custom restrictive), `NoSuchMethodError` (futur Android masquant l'API), etc. Cohérent avec le pattern Story 10.1 AC #3.

5. **Le détecteur ne fait AUCUN logging avec data utilisateur** — Quand `check()` est exécutée, **aucun appel** à `android.util.Log.*` n'est introduit dans cette story. Pas de log d'audit du verdict. Pas de log du `pinnedApp` (révèlerait quel autre VPN l'utilisateur utilise). Pas de log de la stack trace en cas d'exception. Cohérent NFR-AND-9 (prd.md l. 705) + NFR22a (prd.md l. 672).

   **Si le dev pense qu'un log d'audit est utile pour debug** : OK uniquement en `Log.d(TAG, "VpnConflictDetector verdict=ConsentNotGiven")` (jamais `pinnedApp` exposé), et ce `Log.d` sera strippé en release par le `-assumenosideeffects` de `proguard-rules.pro` (livré Story 9.1, étendu Story 10.5). **Décision dev à reporter dans Completion Notes** : log `Log.d` ajouté ou non.

6. **`MainActivity` instancie le détecteur (sans encore brancher au flow Connect)** — Quand `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` est lu après cette story, il contient **en plus** des modifications Stories 9.3/10.1/10.2 :
   ```kotlin
   import fr.plateformeliberte.levoile.conflict.VpnConflictDetector

   class MainActivity : androidx.appcompat.app.AppCompatActivity() {
       // ... existing fields livrés Stories 9.3+10.1+10.2 ...
       private lateinit var vpnConflictDetector: VpnConflictDetector

       override fun onCreate(savedInstanceState: android.os.Bundle?) {
           super.onCreate(savedInstanceState)
           // ... existing code Stories 9.3+10.1+10.2 ...
           vpnConflictDetector = VpnConflictDetector(this)  // 'this' Activity context, pas applicationContext
           // (Note : VpnService.prepare(context) accepte Activity ou Application context — l'Activity est préféré
           //  car le startActivityForResult ultérieur Story 11.5 nécessitera une Activity. Anticipation.)
       }
   }
   ```
   **Important** :
   - **Aucun branchement** au flow Connect dans cette story — le bouton « Connecter » n'existe pas avant Story 11.2 (`window.LeVoile.connect()`). Le détecteur est instancié pour être consommable par le bridge JS dans cette story (AC #7), et par Story 11.2 qui consommera `bridge.checkVpnConflict()` au moment du tap utilisateur.
   - **Pas d'ajout de `onActivityResult(...)`** dans cette story — la gestion du `startActivityForResult(prepareIntent, REQ_VPN_PREPARE)` (Story 11.5) inclura l'override `onActivityResult`. Story 10.3 prépare le verdict, Story 11.5 consomme l'Intent.
   - **Aucune autre modification** — re-lire le diff avant commit. Pas de modif de l'observer LiveData (Story 10.2), pas de modif de l'instanciation du bridge (Story 10.2 le passe `(this, killSwitchDetector)` ; Story 10.3 ne modifie PAS la signature mais ajoute le détecteur conflit via setter ou via une 3ème position constructeur — voir AC #7).

7. **`LeVoileBridge.checkVpnConflict()` expose le verdict côté JS** — Quand `LeVoileBridge.kt` est lu après cette story, il contient **en plus** des méthodes Stories 9.3/10.2 :
   ```kotlin
   class LeVoileBridge(
       private val context: android.content.Context,
       private val killSwitchDetector: KillSwitchDetector? = null,
       private val vpnConflictDetector: VpnConflictDetector? = null,  // NOUVEAU param Story 10.3
   ) {
       // ... existing methods Story 9.3 (getStatus) + Story 10.2 (getKillSwitchStatus, openKillSwitchTarget) ...

       @android.webkit.JavascriptInterface
       fun checkVpnConflict(): String {
           val verdict = vpnConflictDetector?.check() ?: return "{\"verdict\":\"unverifiable\"}"
           return when (verdict) {
               is VpnConflictVerdict.NoConflict -> "{\"verdict\":\"no_conflict\"}"
               is VpnConflictVerdict.ConsentNotGiven -> "{\"verdict\":\"consent_required\"}"
               is VpnConflictVerdict.ForeignVpnActive -> {
                   // foreignAppId est exposé au JS pour permettre Story 11.x d'afficher
                   // dynamiquement le nom de l'app concurrente. Encodage défensif :
                   // un foreignAppId pourrait théoriquement contenir des caractères de contrôle
                   // (rare mais possible si une app exotique a triché sur son packageName).
                   // On limite à [a-zA-Z0-9._] pour échapper tout risque d'injection JSON.
                   val safeAppId = verdict.foreignAppId
                       ?.filter { it.isLetterOrDigit() || it == '.' || it == '_' }
                       ?.take(255)  // garde-fou longueur max
                       ?: ""
                   "{\"verdict\":\"foreign_vpn_active\",\"foreign_app_id\":\"$safeAppId\"}"
               }
           }
       }
   }
   ```
   **Important** :
   - Le retour est une **string JSON** (pas un objet Kotlin sérialisé) — cohérent avec le pattern `getStatus()` Story 9.3 (retour string). Le frontend JS fera `JSON.parse(result)`.
   - Les valeurs `"no_conflict" | "consent_required" | "foreign_vpn_active" | "unverifiable"` sont **stables** (snake_case côté JSON, mapping vers la sealed class Kotlin). Le frontend JS Story 11.2 dépendra de ces valeurs.
   - **Pas de transport de l'`Intent` côté JS** — un `Intent` Android n'est pas sérialisable en JSON cleanly. Le pattern est : Story 11.2 frontend appelle `checkVpnConflict()` → si `consent_required`, frontend appelle ensuite `bridge.requestVpnConsent()` (NOUVELLE méthode Story 11.5 — pas dans 10.3) qui fera le `startActivityForResult` côté Kotlin avec l'Intent stocké en mémoire. **Story 10.3 stocke-t-elle l'Intent ?** Non — chaque appel à `check()` recrée l'Intent via `VpnService.prepare()`. Le coût est négligeable.
   - **`safeAppId` whitelist** : `[a-zA-Z0-9._]` couvre les valid Java package names + suffixes flavor (`.debug`). Tout autre caractère est filtré (sécurité défensive — pas d'injection JSON ni de XSS si Story 11.2 affiche cette valeur sans escape).
   - **Pas de localisation côté Kotlin** — les valeurs JSON sont **machine**, pas UI. La localisation des messages d'erreur affichés à l'utilisateur appartient au frontend JS (qui consomme `verdict` + `foreign_app_id` et choisit la bonne string).

8. **Strings de fallback ajoutées dans `strings.xml` + `values-fr/strings.xml`** — Quand `android/app/src/main/res/values/strings.xml` et `values-fr/strings.xml` sont lus après cette story, ils contiennent :
   ```xml
   <!-- values/strings.xml ET values-fr/strings.xml — texte FR identique en MVP mono-langue -->
   <string name="android_vpn_conflict_title">Conflit VPN détecté</string>
   <string name="android_vpn_conflict_message">Une autre application VPN est active sur cet appareil. Désactivez-la pour utiliser Le Voile.</string>
   <string name="android_vpn_conflict_button_settings">Ouvrir les paramètres VPN</string>
   ```
   **Important** :
   - Pourquoi des `string.xml` (côté Android natif) si Story 11.2 affichera le message côté WebView en HTML ? Réponse : pour permettre à `LeVoileBridge` (et à un éventuel `Toast` ou `AlertDialog` natif) d'afficher la string sans dupliquer le texte côté JS et côté Kotlin. **Cohérence single-source-of-truth** — l'i18n vit dans `strings.xml` (canonique Android), le frontend Story 11.2 lira via une nouvelle méthode bridge `getString(key: String)` (à introduire Story 11.2, **pas dans 10.3**).
   - **Pour Story 10.3 livrée seule** : ces strings ne sont consommées par rien dans cette story (pas de Toast affiché par `VpnConflictDetector`, pas de `AlertDialog`). Elles sont posées en avance pour être consommées par Story 11.2. **C'est intentionnel** — éviter que Story 11.2 ajoute ces strings dans son périmètre alors que la sémantique appartient à 10.3.
   - Pas de string pour `consent_required` ni `no_conflict` — pour `consent_required`, la UX est de présenter directement le popup système Android (pas de message intermédiaire). Pour `no_conflict`, pas de message à afficher (on démarre).
   - Le bouton « Ouvrir les paramètres VPN » réutilise le deeplink `Settings.ACTION_VPN_SETTINGS` (cohérent Story 10.2 AC #5). Story 11.2 instanciera ce bouton côté HTML.

9. **Test JVM-only `VpnConflictDetectorTest.kt` couvre les 4 cas principaux** — Quand `cd android && ./gradlew :app:testDebugUnitTest` est exécuté après cette story, un fichier de test `app/src/test/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictDetectorTest.kt` couvre **au minimum** :

    | # | Setup `VpnService.prepare()` | Setup `Settings.Global.always_on_vpn_app` | Verdict attendu |
    |---|---|---|---|
    | T1 | retourne `null` | (n'importe quoi) | `NoConflict` |
    | T2 | retourne Intent non-null | retourne `null` | `ConsentNotGiven(intent)` |
    | T3 | retourne Intent non-null | retourne `expectedAppId` | `ConsentNotGiven(intent)` |
    | T4 | retourne Intent non-null | retourne `"com.tailscale.ipn"` | `ForeignVpnActive("com.tailscale.ipn")` |
    | T5 | retourne Intent non-null | throws `SecurityException` | `ConsentNotGiven(intent)` (fallback prudent) |

    **Difficulté** : `VpnService.prepare(context)` est une méthode **statique** Java sur `android.net.VpnService` — pas trivialement mockable JVM-only. **2 options** :
    - Option A — Refactor : introduire une `internal interface VpnPreparer { fun prepare(context: Context): Intent? }` + impl par défaut `RealVpnPreparer { override fun prepare(context: Context) = VpnService.prepare(context) }`. Constructeur `VpnConflictDetector(context, settingsReader, expectedAppId, preparer: VpnPreparer = RealVpnPreparer())`. Le test injecte un `FakeVpnPreparer` qui retourne null ou un Intent stub. **Recommandation : Option A** — cohérent avec le pattern Story 10.1 `SettingsReader`.
    - Option B — Robolectric : Robolectric peut shadow `VpnService.prepare` via `ShadowVpnService` mais c'est lourd et ajoute une dépendance Gradle. **Pas recommandé** sauf si l'équipe considère que la couche d'abstraction Option A est trop verbeuse.

    **Décision dev à reporter dans Debug Log** : Option A vs Option B. Recommandation : A.

    Pour T2-T5, le test peut utiliser un `Intent` stub `Intent("test.action.STUB")` — la valeur exacte de l'Intent ne matters pas, seul le fait qu'il est non-null compte.

    Pour T5, le `FakeSettingsReader` jette `SecurityException` dans son `getString` — le test vérifie que le verdict est `ConsentNotGiven` (fallback prudent AC #4).

10. **`README-android.md` patché — section « Détection conflit VPN »** — Quand `android/README-android.md` est lu après cette story, il contient une section additionnelle (insérée APRÈS la section Story 10.2 « Bandeau C17 kill switch ») :

    ```markdown
    ## Détection conflit VPN (Story 10.3 livrée)

    `VpnConflictDetector.check()` combine `VpnService.prepare(context)` +
    heuristique `Settings.Global.always_on_vpn_app` pour produire l'un de 3 verdicts :
    - `NoConflict` : tunnel peut démarrer immédiatement.
    - `ConsentNotGiven(prepareIntent)` : popup système consent à présenter (Story 11.5).
    - `ForeignVpnActive(foreignAppId)` : autre app VPN détient le slot — refus + UX.

    Exposé côté JS via `window.LeVoile.checkVpnConflict()` (retour JSON string).
    Le bouton « Connecter » Story 11.2 consommera ce verdict au tap utilisateur.

    **Test manuel cycle complet** :
    ```
    # 1. État premier-lancement (jamais consenti)
    adb shell pm clear fr.plateformeliberte.levoile.debug
    adb shell am start -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.MainActivity
    # Dans chrome://inspect, taper : window.LeVoile.checkVpnConflict()
    # → '{"verdict":"consent_required"}'

    # 2. Simuler conflit Tailscale
    adb shell settings put global always_on_vpn_app com.tailscale.ipn
    # Note: Tailscale doit être installé pour que prepare() retourne non-null
    # (sinon settings sont posés mais aucun service VPN concurrent).
    # Alternative : installer un VPN test simple (ProtonVPN, etc.) pour reproduire.
    ```

    Vérifier le verdict via `chrome://inspect` console JS.
    ```

    Aucune autre section du README n'est touchée.

## Tasks / Subtasks

- [ ] **Task 1 : Vérifier l'état Stories 9.x + 10.1 + 10.2 livrées** (AC: tous)
  - [ ] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` — confirmer présence des modifs 10.1 (`killSwitchDetector`) et 10.2 (observer LiveData + bridge instanciation `(this, killSwitchDetector)`).
  - [ ] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` — confirmer signature actuelle `(context, killSwitchDetector)`.
  - [ ] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/SettingsReader.kt` — confirmer signature `internal interface SettingsReader { ... }` et `internal class ContentResolverSettingsReader`. Cette interface sera importée par le détecteur conflit.
  - [ ] Lire `android/app/src/main/AndroidManifest.xml` — confirmer absence de besoin de nouvelle permission.
  - [ ] **Reporter dans Debug Log** : état exact des fichiers lus.

- [ ] **Task 2 : Créer `VpnConflictVerdict.kt`** (AC: #1)
  - [ ] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictVerdict.kt`.
  - [ ] Implémenter la `sealed class` selon AC #1 (3 variantes).
  - [ ] Kdoc sur chaque variante (3 lignes max chacun) référant FR-AND-6 + ADR-10.

- [ ] **Task 3 : Créer `VpnPreparer` interface (testabilité) et impl par défaut** (AC: #9)
  - [ ] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/VpnPreparer.kt` (ou colocaliser dans `VpnConflictDetector.kt`) :
    ```kotlin
    internal interface VpnPreparer {
        fun prepare(context: android.content.Context): android.content.Intent?
    }

    internal class RealVpnPreparer : VpnPreparer {
        override fun prepare(context: android.content.Context): android.content.Intent? =
            android.net.VpnService.prepare(context)
    }
    ```
  - [ ] Visibilité `internal` (testable depuis le module mais pas API publique).

- [ ] **Task 4 : Créer `VpnConflictDetector.kt`** (AC: #2, #3, #4, #5)
  - [ ] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictDetector.kt`.
  - [ ] Constructeur primaire avec defaults selon AC #2 :
    ```kotlin
    class VpnConflictDetector(
        private val context: Context,
        private val settingsReader: SettingsReader = ContentResolverSettingsReader(context.contentResolver),
        private val expectedAppId: String = BuildConfig.APPLICATION_ID,
        private val preparer: VpnPreparer = RealVpnPreparer(),
    )
    ```
  - [ ] Implémenter `fun check(): VpnConflictVerdict` selon AC #3-#4.
  - [ ] **Pas de logging** dans cette story (AC #5).
  - [ ] Kdoc sur la classe + Kdoc sur `check()` (3 blocs Kdoc, ~5 lignes chacun, références FR-AND-6 + ADR-10 + architecture.md l. 580-584).

- [ ] **Task 5 : Modifier `MainActivity.kt`** (AC: #6)
  - [ ] Ajouter `import fr.plateformeliberte.levoile.conflict.VpnConflictDetector`.
  - [ ] Ajouter `private lateinit var vpnConflictDetector: VpnConflictDetector`.
  - [ ] Dans `onCreate(savedInstanceState)`, après `killSwitchDetector = KillSwitchDetector(applicationContext)` (Story 10.1) : `vpnConflictDetector = VpnConflictDetector(this)` (passer Activity, pas applicationContext, anticipation Story 11.5).
  - [ ] Modifier l'instanciation bridge : `LeVoileBridge(this, killSwitchDetector, vpnConflictDetector)` (était `LeVoileBridge(this, killSwitchDetector)` Story 10.2).
  - [ ] **Aucune autre modification**. Pas de override `onActivityResult`. Pas d'ajout d'observer.

- [ ] **Task 6 : Modifier `LeVoileBridge.kt`** (AC: #7)
  - [ ] Modifier la signature constructeur en ajoutant `private val vpnConflictDetector: VpnConflictDetector? = null` en 3ème position (avec default `= null` pour compat tests).
  - [ ] Ajouter `import fr.plateformeliberte.levoile.conflict.{VpnConflictDetector, VpnConflictVerdict}`.
  - [ ] Ajouter méthode `@JavascriptInterface fun checkVpnConflict(): String` selon AC #7.
  - [ ] **NE PAS** ajouter d'autre méthode `@JavascriptInterface` — pas de `connect`, pas de `requestVpnConsent`, pas de `getString` (réservées Story 11.2/11.5).
  - [ ] Vérifier que la méthode existante `getKillSwitchStatus` (Story 10.2) reste intacte.

- [ ] **Task 7 : Ajouter strings dans `strings.xml` + `values-fr/strings.xml`** (AC: #8)
  - [ ] 3 nouvelles clés (titre, message, bouton settings).
  - [ ] Texte FR identique dans les 2 fichiers (MVP mono-langue).
  - [ ] Pas de string pour `consent_required` ni `no_conflict` (justifié AC #8).

- [ ] **Task 8 : Créer `VpnConflictDetectorTest.kt`** (AC: #9)
  - [ ] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictDetectorTest.kt`.
  - [ ] Implémenter `FakeVpnPreparer(private val intentToReturn: Intent?) : VpnPreparer`.
  - [ ] Réutiliser `FakeSettingsReader` introduit Story 10.1 (si visibilité le permet — sinon dupliquer dans ce fichier de test).
  - [ ] Implémenter les 5 tests T1-T5 (matrice AC #9).
  - [ ] **Pas de Robolectric** — JVM-only via DI complète.
  - [ ] Vérifier `cd android && ./gradlew :app:testDebugUnitTest` passe vert.

- [ ] **Task 9 : Patcher `README-android.md`** (AC: #10)
  - [ ] Insérer la section « Détection conflit VPN (Story 10.3 livrée) » au bon endroit (après section Story 10.2).

- [ ] **Task 10 : Build sanity check + test manuel device**
  - [ ] `cd android && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` — toutes tâches vert.
  - [ ] `apkanalyzer apk file-size app/build/outputs/apk/debug/app-debug.apk` — taille reste < 25 MB (NFR-AND-3).
  - [ ] Test manuel via `adb` (cf. README AC #10) — pour le cas T4 « ForeignVpnActive », installer un VPN test (ProtonVPN free) pour reproduire un conflit réaliste, ou simuler en activant un VPN system mock via `adb` (avancé). **Reporter dans Debug Log** : verdict observé via `chrome://inspect` console JS.

- [ ] **Task 11 : Mettre à jour la story et sprint-status**
  - [ ] Mettre à jour la section « Dev Agent Record » (Agent Model Used, File List, Completion Notes List, Change Log).
  - [ ] Passer le `Status` de cette story de `ready-for-dev` à `review`.
  - [ ] Passer `_bmad-output/implementation-artifacts/sprint-status.yaml` `10-3-detection-autre-app-vpn-...: backlog` → `review`.

## Dev Notes

### Pattern principal — Détecteur stateless + DI testable

`VpnConflictDetector` ne stocke aucun état (différent de `KillSwitchDetector` qui expose un `LiveData`). C'est intentionnel :
- Le check de conflit n'a pas vocation à être observé en continu — il a lieu **au tap utilisateur** (ou à un moment précis, tel `onResume` premier launch).
- Pas de cache : `prepare()` peut changer de retour entre 2 appels (utilisateur ouvre/ferme un autre VPN entre-temps), donc cache = source de bug.

L'injection 4-paramètres (`context`, `settingsReader`, `expectedAppId`, `preparer`) permet un testing JVM-only complet sans Robolectric. Pattern cohérent Story 10.1.

### Pourquoi pas une `enum class` au lieu de `sealed class` pour `VpnConflictVerdict`

Parce que les 2 variantes non-`NoConflict` portent **des données** : `ConsentNotGiven(prepareIntent: Intent)` et `ForeignVpnActive(foreignAppId: String?)`. Une `enum` ne peut pas porter de données par-instance (toutes les valeurs partagent les mêmes propriétés). `sealed class` est le bon outil ici.

### Coordination Story 11.2 et 11.5

- **Story 11.2** (« JS Bridge complet ») livrera `window.LeVoile.connect()` qui :
  1. Appelle `bridge.checkVpnConflict()` (cette story)
  2. Si `no_conflict` → directement `bridge.startTunnel()` (Story 9.5 ou 11.2)
  3. Si `consent_required` → appelle `bridge.requestVpnConsent()` (NOUVELLE méthode Story 11.5)
  4. Si `foreign_vpn_active` → affiche bandeau d'erreur frontend avec `foreign_app_id` + bouton « Ouvrir paramètres VPN »

- **Story 11.5** (`OnboardingActivity`) livrera `bridge.requestVpnConsent()` qui :
  1. Re-appelle `vpnConflictDetector.check()` pour récupérer un Intent frais
  2. Appelle `MainActivity.startActivityForResult(prepareIntent, REQ_VPN_PREPARE)` (overrided dans MainActivity — Story 11.5)
  3. `onActivityResult` reçoit `RESULT_OK` ou `RESULT_CANCELED` et notifie le frontend

Story 10.3 verrouille uniquement `checkVpnConflict()` — le flow complet « detect → consent → start » est split entre 10.3, 11.2, 11.5. Garder les frontières propres.

### Pourquoi le détecteur ne stocke PAS l'Intent

L'Intent retourné par `VpnService.prepare()` est valide tant que la condition système n'a pas changé. Stocker l'Intent et le réutiliser plus tard expose à 2 risques :
- Race condition : entre `check()` et `requestVpnConsent()`, l'utilisateur ouvre un autre VPN → l'Intent stocké est obsolète (non-évident car l'API ne déclare pas d'expiration).
- Memory leak : un `Intent` capture potentiellement le `Context` (peu probable mais théoriquement possible selon l'impl AOSP).

Pattern retenu : chaque consumer (Story 11.5) re-appelle `check()` au moment où il veut le `prepareIntent`. Le coût est négligeable (`prepare()` < 1ms typique).

### Source tree components à toucher

- **Nouveaux** :
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictDetector.kt`
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictVerdict.kt`
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/conflict/VpnPreparer.kt` (ou colocalisé dans `VpnConflictDetector.kt`)
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/conflict/VpnConflictDetectorTest.kt`
- **Modifiés à la marge** :
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (ajout `vpnConflictDetector` + passage au bridge ; aucune logique de flow)
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (ajout 1 paramètre constructeur + 1 méthode)
  - `android/app/src/main/res/values/strings.xml` (3 nouvelles clés)
  - `android/app/src/main/res/values-fr/strings.xml` (3 nouvelles clés identiques)
  - `android/README-android.md` (section nouvelle)

### Standards de testing

- Test JVM-only : 5 tests T1-T5, < 200ms total.
- Pas de test Espresso instrumenté ici — Story 12.6 traitera le flow complet (Connect button → checkVpnConflict → consent popup → tunnel start).
- Couverture : 4 verdicts × 1 cas chacun + 1 cas erreur heuristique = 5 tests minimum.

### Project Structure Notes

Le package `fr.plateformeliberte.levoile.conflict` est nouveau. Cohérent avec le découpage existant :
- `kill/` → détection kill switch (Story 10.1)
- `bridge/` → JS bridge (Story 9.3+)
- `vpn/` → service VPN (Story 9.4)
- `conflict/` → détection conflit VPN (Story 10.3 — cette story)

Pas de re-shuffling des packages existants. Pas de package `helper/` ou `util/` fourre-tout.

### References

- [architecture.md l. 580-584](_bmad-output/planning-artifacts/architecture.md) — séquence MainActivity check VpnService.prepare().
- [architecture.md l. 757](_bmad-output/planning-artifacts/architecture.md) — ordre démarrage Android : MainActivity → check prepare → si null OK, sinon prompt consent.
- [architecture.md l. 1958](_bmad-output/planning-artifacts/architecture.md) — pattern val intent = VpnService.prepare(this).
- [architecture.md l. 2026](_bmad-output/planning-artifacts/architecture.md) — schéma always_on_vpn_app=fr.plateformeliberte.levoile.
- [architecture.md l. 2413-2416](_bmad-output/planning-artifacts/architecture.md) — ADR-10.
- [architecture.md l. 2434-2437](_bmad-output/planning-artifacts/architecture.md) — ADR-14 WebView Android (rendu des messages d'erreur côté HTML, pas en AlertDialog).
- [epics.md l. 1734-1756](_bmad-output/planning-artifacts/epics.md) — Story 10.3 BDD complet (3 scénarios Given/When/Then).
- [prd.md l. 614](_bmad-output/planning-artifacts/prd.md) — FR-AND-6 détection autre app VPN active.
- [prd.md l. 705](_bmad-output/planning-artifacts/prd.md) — NFR-AND-9 logs filtrés.
- Story 9.3 (livrée) : `LeVoileBridge.kt` signature de base + `getStatus()`.
- Story 9.4 (à venir, file existante non implémentée) : `LeVoileVpnService.kt` — service VPN, indépendant de cette story (la détection est en amont du démarrage du service).
- Story 10.1 (livrée — pré-requise) : `SettingsReader` interface + `ContentResolverSettingsReader` impl, **réutilisée** par cette story.
- Story 10.2 (livrée — pré-requise) : `LeVoileBridge` signature `(context, killSwitchDetector)`, étendue ici à `(context, killSwitchDetector, vpnConflictDetector)`.
- Story 11.2 (à venir) : `window.LeVoile.connect()` consomme `checkVpnConflict()` au tap utilisateur.
- Story 11.5 (à venir) : `OnboardingActivity` + `requestVpnConsent()` consomme l'Intent retourné par `prepare()` via `startActivityForResult`.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

### Change Log
