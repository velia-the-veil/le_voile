# Story 10.1: Heuristique détection kill switch via `Settings.Global.always_on_vpn_app`

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Aucune exception « code partagé » n'est nécessaire pour cette story.** Story 10.1 livre une classe Kotlin pure (`KillSwitchDetector.kt`) qui interroge l'API Android `Settings.Global` via `ContentResolver` et expose un `LiveData<KillSwitchStatus>`. Aucun fichier Go n'est lu, créé ou modifié. Aucun import gomobile. Aucune entrée dans `android/shims/*.go` n'est ajoutée. Aucune ligne dans `go.mod`/`go.sum` racine n'est touchée. Aucun appel JNI vers le `.aar` n'est introduit (le détecteur est purement OS-Android, indépendant du noyau Go partagé).
>
> **Rappel ADR-08 (architecture.md l. 2397-2400) — isolation OS maximale.** La règle structurelle est : un agent IA travaillant sur Android ne touche JAMAIS au code Windows, Linux, ou aux packages racine `internal/*` desktop. La détection « VPN permanent activé » est par construction Android-spécifique (heuristique sur `Settings.Global.always_on_vpn_app` propre à l'OS Android, pas d'équivalent Windows/Linux). Cette logique vit donc 100% sous `android/app/src/main/kotlin/`. Toute tentative de "factoriser" cette détection dans un package racine partagé est une violation directe d'ADR-08 — refusée en code review.
>
> **Zones explicitement OFF-LIMITS pour cette story** (livrées par 9.1/9.2/9.3, intactes pour 10.1) :
>
> | Zone | Livrée par | État pour 10.1 |
> |---|---|---|
> | `go.mod` racine (`golang.org/x/mobile` indirect + bumps `crypto`/`mod`/`net`/`sys`/`text`/`tools`) | Story 9.2 | INTACT — ne pas toucher |
> | `go.sum` racine | Story 9.2 | INTACT — ne pas toucher |
> | `android/shims/{auth,crypto,leakcheck,protocol,registry}/*.go` | Story 9.2 | INTACT — code Go, pas Kotlin |
> | `android/scripts/build-aar.{sh,ps1}` + `verify-shared-imports.sh` | Story 9.2 | INTACT — non invoqués par 10.1 |
> | `android/levoile-core/build.gradle.kts` + `levoile-core/src/main/AndroidManifest.xml` | Story 9.1+9.2 | INTACT — module non consommé fonctionnellement par 10.1 |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` | Story 9.3 | **MODIFIÉ uniquement à la marge** — voir AC #6 (ajout d'un appel `KillSwitchDetector.refresh()` dans `onResume()`). **Aucune autre modification autorisée** (UI WebView, bridge JS, layout — tout reste tel que livré 9.3) |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` | Story 9.3 | INTACT — l'enrichissement du bridge JS pour exposer le statut kill switch côté frontend appartient à Story 10.2 |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` | Story 9.4 | INTACT — la détection kill switch n'a aucune dépendance sur le service VPN (vérification OS-level pure, fonctionne même service arrêté) |
> | `android/app/src/main/assets/{index.html,style.css,app.js}` | Story 9.3 | INTACT — pas de modification UI ici, le rendu visuel du bandeau est Story 10.2 |
> | `android/app/src/main/AndroidManifest.xml` | Stories 9.1+9.4 | **INTACT** — `Settings.Global.getString` ne nécessite **aucune permission** (différent de `Settings.Secure` qui demanderait `WRITE_SECURE_SETTINGS`). Aucune nouvelle `<uses-permission>` à ajouter |
> | `internal/{tunnel,tun,firewall,routing,ui,ipc,wfp,nftables,...}` racine + `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/` | Stories 1-8 | INTACT — hors arbre `android/` |
>
> **Concrètement** : `git status` à la fin de la session dev doit montrer **uniquement** :
>   (a) entrées sous `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/` (NOUVEAU — 2 fichiers : `KillSwitchDetector.kt`, `KillSwitchStatus.kt`),
>   (b) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — ajout instanciation `KillSwitchDetector` + appel `refresh()` dans `onResume()` ; pas d'autre modif),
>   (c) `android/app/build.gradle.kts` (MODIFIÉ — ajout dépendance `androidx.lifecycle:lifecycle-livedata-ktx` si pas déjà tirée transitivement par `androidx.appcompat`),
>   (d) `android/gradle/libs.versions.toml` (MODIFIÉ — ajout entrée version + lib `androidx-lifecycle-livedata-ktx` si nécessaire),
>   (e) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetectorTest.kt` (NOUVEAU — tests unitaires Robolectric ou JVM-only stubbé),
>   (f) `android/README-android.md` (MODIFIÉ — section « Détection kill switch »),
>   (g) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `ready-for-dev` → `review`),
>   (h) `_bmad-output/implementation-artifacts/10-1-heuristique-detection-kill-switch-via-settings-global-always-on-vpn-app.md` (auto-update Status, File List, Completion Notes, Change Log).
>
> **Aucune entrée à la racine** (`go.mod`, `go.sum`, `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`, `_bmad/`), **aucune entrée sous `android/shims/`, `android/scripts/`, `android/levoile-core/`, `android/app/src/main/assets/`, `android/app/src/main/kotlin/fr/plateformeliberte/levoile/{bridge/,vpn/}`**. Tout autre fichier modifié est un side-effect non prévu — **STOP, investiguer, ne pas tenter de "lisser" en supprimant les changements**. Reporter dans Debug Log et demander avant de poursuivre.
>
> **Anti-pattern fréquent à éviter** : tenter de pré-tirer en avance des fichiers prévus par les stories suivantes — exposition du statut au frontend JS via `LeVoileBridge` (Story 10.2), composant CSS C17 dans `app.js`/`style.css` (Story 10.2), détection autre app VPN active via `VpnService.prepare()` (Story 10.3), onboarding kill switch screen + deeplink Settings via composant C15 (Story 11.5/11.6), bandeau orange spécifique au cas premier-lancement-pas-encore-onboarded (Story 11.5). Cette story livre **uniquement** un détecteur Kotlin headless qui produit un `LiveData<KillSwitchStatus>` — la consommation côté UI est split entre Story 10.2 (bandeau C17) et Story 11.6 (onboarding C15). Si tu te retrouves à modifier `assets/index.html` ou à enrichir `LeVoileBridge.kt`, tu es hors-scope.

## Story

En tant qu'utilisateur Android,
Je veux que l'app détecte automatiquement si « VPN permanent + bloquer connexions sans VPN » est activé pour Le Voile dans les paramètres système Android,
Afin que je voie un signal sans ambiguïté (à venir Story 10.2 sous forme de bandeau C17, et Story 11.6 sous forme d'écran d'onboarding C15) tant que ma protection complète n'est pas en place — cohérent ADR-10 (architecture.md l. 2413-2416) et FR-AND-2 / NFR-AND-9.

## Acceptance Criteria

1. **`KillSwitchDetector` existe sous `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/`** — Quand `app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetector.kt` est lu, il déclare une classe `class KillSwitchDetector(private val context: android.content.Context)` (constructeur unique à un paramètre `Context`). La classe expose :
   - une propriété `val status: androidx.lifecycle.LiveData<KillSwitchStatus>` (backed par un `private val _status: MutableLiveData<KillSwitchStatus>` initialisé à `KillSwitchStatus.Unverifiable` au constructeur — état par défaut prudent : on ne sait pas tant qu'on n'a pas mesuré),
   - une méthode publique `fun refresh()` (synchrone, NON suspendue, à appeler depuis `Activity.onResume()`) qui exécute l'heuristique et publie le résultat sur `_status` via `postValue` (thread-safe).

   La méthode `refresh()` n'expose **jamais** de méthodes `private fun readSetting()` côté contrat public — l'API publique est strictement `{status, refresh()}`. Toutes les autres méthodes sont `private` ou `internal` (testabilité contrôlée).

2. **`KillSwitchStatus` est une `sealed class` (ou `enum`) à 3 valeurs nommées exactement** — Quand `app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchStatus.kt` est lu, il déclare :
   ```kotlin
   sealed class KillSwitchStatus {
       object Active : KillSwitchStatus()
       object Inactive : KillSwitchStatus()
       object Unverifiable : KillSwitchStatus()
   }
   ```
   ou — alternative acceptable et **équivalente sémantiquement** — un `enum class KillSwitchStatus { Active, Inactive, Unverifiable }`. **Recommandation** : `sealed class` car cohérent avec l'usage Kotlin idiomatique (`when` exhaustif, support futur d'évolution avec données — par ex. `data class Inactive(val reason: String)`). **Décision dev à reporter dans Completion Notes** : sealed class vs enum. Pas de 4ème valeur (pas de `Loading`, pas de `Unknown`, pas de `Pending`) — `Unverifiable` couvre déjà les cas d'incertitude (heuristique cassée, ROM custom, restriction Android future). Pas de variante `ActiveButLockdownOff` — l'AC #4 traite ce cas comme `Inactive` (lockdown=0 ⇒ trafic non bloqué hors VPN ⇒ kill switch effectivement inactif même si app pinnée).

3. **L'heuristique lit `always_on_vpn_app` ET `always_on_vpn_lockdown`** — Quand `KillSwitchDetector.refresh()` est exécutée, elle invoque dans cet ordre :
   ```kotlin
   val resolver = context.contentResolver
   val pinnedApp: String? = try {
       android.provider.Settings.Global.getString(resolver, "always_on_vpn_app")
   } catch (t: Throwable) {
       android.util.Log.i(TAG, "KillSwitchDetector: heuristique always_on_vpn_app indisponible sur ce device")
       _status.postValue(KillSwitchStatus.Unverifiable)
       return
   }
   val lockdownEnabled: Int = try {
       android.provider.Settings.Global.getInt(resolver, "always_on_vpn_lockdown", 0)
   } catch (t: Throwable) {
       android.util.Log.i(TAG, "KillSwitchDetector: heuristique always_on_vpn_lockdown indisponible sur ce device")
       _status.postValue(KillSwitchStatus.Unverifiable)
       return
   }
   ```
   **Important** : `Settings.Global.getString` peut retourner `null` (slot vide = aucun VPN permanent configuré) — c'est **PAS** une exception, c'est un cas normal traité à l'AC #4. Ne jamais transformer `null` en `KillSwitchStatus.Unverifiable` — la valeur `null` est observable et signifie « pas de VPN permanent configuré ». Seule une exception `Throwable` (`SecurityException`, `Settings.SettingNotFoundException` masquée par défaut int=0, ou autre runtime exotique) déclenche la branche `Unverifiable`. La distinction est critique : un device standard où l'utilisateur n'a rien configuré doit retourner `Inactive`, pas `Unverifiable` — sinon le bandeau Story 10.2 ne s'affichera jamais alors qu'on est précisément dans le cas où il faut alerter (architecture.md l. 1078, l. 2191).

4. **La logique de classification produit Active / Inactive / Unverifiable selon une matrice exacte** — Quand `pinnedApp` et `lockdownEnabled` ont été lus avec succès, la classification suit :

   | `pinnedApp` (String?) | `lockdownEnabled` (Int) | Résultat | Justification |
   |---|---|---|---|
   | non-null ET == `expectedAppId` | `1` | `Active` | « VPN permanent » + « Bloquer connexions sans VPN » activés tous les deux POUR Le Voile → kill switch effectif |
   | non-null ET == `expectedAppId` | `0` ou autre | `Inactive` | « VPN permanent » activé pour Le Voile MAIS lockdown OFF → trafic non-VPN passe librement → kill switch effectivement absent |
   | non-null ET != `expectedAppId` | `0` ou `1` | `Inactive` | Une autre app VPN tient le slot « permanent » (Tailscale, Wireguard, etc.) — Le Voile n'est pas pinné → notre kill switch n'est pas en place. Story 10.3 traite séparément le refus de démarrer si conflit |
   | `null` | `0` ou `1` | `Inactive` | Aucun VPN permanent configuré → pas de kill switch → l'utilisateur DOIT être alerté |

   où `expectedAppId` est défini comme suit (AC #5 ci-dessous).

   La classification est implémentée via un `when` exhaustif Kotlin. **Pas de simplification douteuse** du genre « si pinnedApp est null OU différent de expectedAppId → Inactive ; sinon vérifier lockdown » : laisser l'arbre explicite, chaque branche commentée d'une ligne référant la justification (ADR-10 / FR-AND-2 / architecture.md l. 1078).

5. **`expectedAppId` retire le suffixe debug si présent** — Quand `KillSwitchDetector` détermine la valeur de référence pour la comparaison, il prend `BuildConfig.APPLICATION_ID` comme point de départ. **Attention au piège** : `app/build.gradle.kts` (livré Story 9.1) configure `applicationIdSuffix = ".debug"` pour le buildType `debug` — donc en build debug, `BuildConfig.APPLICATION_ID == "fr.plateformeliberte.levoile.debug"`, alors qu'en release c'est `"fr.plateformeliberte.levoile"`. **C'est en réalité le comportement souhaité** : si l'utilisateur a installé l'APK debug, il a forcément autorisé le VPN permanent sur le package suffixé `.debug` — la comparaison est donc correcte sans normalisation. **Ne PAS retirer le suffixe** : `expectedAppId = BuildConfig.APPLICATION_ID` directement.

   Cas particulier — si le dev introduit dans le futur d'autres flavors avec d'autres suffixes : la même logique reste valable (chaque flavor est un package distinct côté OS, donc chaque flavor a son propre slot `always_on_vpn_app`). **Pas de fallback `startsWith("fr.plateformeliberte.levoile")`** — ce serait trop laxe (on autoriserait que l'utilisateur ait pinné un autre flavor, ce qui n'est pas notre kill switch).

   **Documenter ce choix dans le Kdoc** de la classe : « `expectedAppId = BuildConfig.APPLICATION_ID` — comparaison stricte avec le package effectivement installé, suffixe debug inclus si applicable. Cohérent avec l'expérience utilisateur : l'utilisateur active le VPN permanent sur l'app effectivement présente dans Settings → Apps. » (5 lignes max dans le Kdoc).

6. **`MainActivity.onResume()` invoque `killSwitchDetector.refresh()`** — Quand `app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` est lu après cette story, il a été enrichi de la façon suivante (par rapport à l'état livré 9.3) :
   ```kotlin
   import fr.plateformeliberte.levoile.kill.KillSwitchDetector

   class MainActivity : androidx.appcompat.app.AppCompatActivity() {
       private lateinit var killSwitchDetector: KillSwitchDetector

       override fun onCreate(savedInstanceState: android.os.Bundle?) {
           super.onCreate(savedInstanceState)
           // ... code existant Story 9.3 (setContentView, WebView, bridge) ...
           killSwitchDetector = KillSwitchDetector(applicationContext)
       }

       override fun onResume() {
           super.onResume()
           killSwitchDetector.refresh()
       }
   }
   ```
   **Aucune autre modification de `MainActivity.kt`**. En particulier :
   - Pas d'observation `killSwitchDetector.status.observe(this) { ... }` dans cette story — l'observer (qui poussera l'état au frontend ou modifiera l'UI) appartient à Story 10.2.
   - Pas d'enregistrement d'un `BroadcastReceiver` qui écoute `Settings.ACTION_VPN_SETTINGS` close — Android ne broadcaste pas ce changement, l'heuristique ne peut être re-vérifiée qu'à `onResume()` (au retour de l'utilisateur depuis Settings).
   - Pas d'appel à `refresh()` ailleurs que `onResume()` — pas dans `onStart()`, pas dans `onCreate()` directement (`onResume` est déjà appelée juste après `onStart` au premier launch). Une seule source d'invocation.

   **Note de coordination Story 10.2** : Story 10.2 ajoutera un observer pour pousser le statut au frontend JS. Le contrat « refresh appelée à onResume + LiveData postValue » est suffisant pour 10.2 (LiveData rejoue le dernier statut à chaque nouvel observer).

7. **Le tag de log filtré ne contient aucune donnée utilisateur** — Quand l'heuristique échoue (cas `Unverifiable`), un seul `Log.i(TAG, message)` est émis, où `TAG = "KillSwitchDetector"` (constante `private const val TAG = "KillSwitchDetector"` au top-level du fichier ou en companion object) et `message` est strictement l'un des deux templates fixés à l'AC #3 :
   - `"KillSwitchDetector: heuristique always_on_vpn_app indisponible sur ce device"`
   - `"KillSwitchDetector: heuristique always_on_vpn_lockdown indisponible sur ce device"`

   **Ne jamais inclure** : `pinnedApp` (= nom de package potentiellement sensible — révèle quels VPN concurrents l'utilisateur utilise), `expectedAppId` (= notre propre package, redondant), le contenu de la `Throwable` (`Log.i(TAG, "...", e)` est INTERDIT — la stack trace peut révéler des chemins ROM custom), aucune valeur de setting, aucune horodatage utilisateur. Cohérent NFR22a + NFR-AND-9.

   **Pas de `Log.w` ni `Log.e`** dans cette story — `Unverifiable` n'est pas une erreur, c'est un état métier prévisible (ROM custom, futur Android qui restreint ces accès). `Log.i` est le bon niveau (architecture.md l. 705 : « release : niveau WARN+ uniquement, debug : INFO+ »). En release, ce log sera donc invisible — c'est volontaire (Story 10.5 traite le filtrage `Log.i` en release plus complètement via `LeVoileLog` wrapper, mais Story 10.1 n'attend PAS Story 10.5 — utiliser `android.util.Log.i` direct ici, refactorable plus tard sans casser le contrat).

8. **Test unitaire `KillSwitchDetectorTest.kt`** — Quand `cd android && ./gradlew :app:testDebugUnitTest` est exécuté après cette story, un fichier de test `app/src/test/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetectorTest.kt` couvre **au minimum** les 5 cas suivants via Robolectric (`@RunWith(AndroidJUnit4::class)` + `@Config(sdk=[34])`) :

   | # | Setup `Settings.Global` | Build flavor | Statut attendu |
   |---|---|---|---|
   | T1 | `always_on_vpn_app = "fr.plateformeliberte.levoile"`, `always_on_vpn_lockdown = 1` | release (BuildConfig.APPLICATION_ID == "fr.plateformeliberte.levoile") | `Active` |
   | T2 | `always_on_vpn_app = "fr.plateformeliberte.levoile"`, `always_on_vpn_lockdown = 0` | release | `Inactive` |
   | T3 | `always_on_vpn_app = "com.tailscale.ipn"`, `always_on_vpn_lockdown = 1` | release | `Inactive` |
   | T4 | `always_on_vpn_app = null`, `always_on_vpn_lockdown = 0` | release | `Inactive` |
   | T5 | `always_on_vpn_app = "fr.plateformeliberte.levoile.debug"`, `always_on_vpn_lockdown = 1` | debug (BuildConfig.APPLICATION_ID == "fr.plateformeliberte.levoile.debug") | `Active` |

   Pour T5 : Robolectric exécute en build debug par défaut, donc `BuildConfig.APPLICATION_ID == "fr.plateformeliberte.levoile.debug"` automatiquement. Pour T1-T4 : ils s'exécutent aussi sous BuildConfig debug — c'est OK, on stub `expectedAppId` via injection si besoin OU on accepte que T1 vérifie en réalité l'égalité avec `"fr.plateformeliberte.levoile.debug"`. **Décision dev à reporter dans Debug Log** : ajouter ou non un override de `expectedAppId` testable (constructeur secondaire `internal constructor(context, expectedAppId: String)` pour les tests). **Recommandation** : oui, c'est une légère ouverture de testabilité acceptable (l'API publique reste 1 constructeur).

   Setup Robolectric : `Shadows.shadowOf(...)` sur `Settings.Global` n'est pas trivial (Robolectric ne shadow pas tous les flags), donc une approche alternative : créer un `internal interface SettingsReader { fun getString(name: String): String?; fun getInt(name: String, default: Int): Int }` que `KillSwitchDetector` consomme via DI constructeur (constructeur primaire `class KillSwitchDetector(context, settingsReader: SettingsReader = ContentResolverSettingsReader(context))`). En test, injecter un `FakeSettingsReader` qui retourne les valeurs voulues. **Recommandation** : oui, c'est l'approche la plus saine — testable JVM-only sans Robolectric.

   **Si Robolectric s'avère lourd à configurer pour ce test** : fallback JVM-only via l'interface `SettingsReader` ci-dessus est totalement acceptable et même préférable. Ne PAS bloquer la story sur Robolectric.

9. **Aucune dépendance Gradle interdite** (cohérent ADR-15 / NFR-AND-8) — Quand `app/build.gradle.kts` est diffé contre l'état pré-10.1, **aucune** des dépendances suivantes n'apparaît : `firebase-*`, `crashlytics-*`, `sentry-*`, `bugsnag-*`, `mixpanel-*`, `adjust.io-*`, `branch.io-*`, `amplitude-*`. Story 10.4 livrera l'audit Gradle CI bloquant qui automatise cette vérification — Story 10.1 doit déjà respecter la règle manuellement.

   La seule dépendance acceptable à ajouter dans cette story (si pas déjà tirée transitivement par `androidx.appcompat`) est `androidx.lifecycle:lifecycle-livedata-ktx:2.7.0` — version stable au 2026-05-02, pas de breaking change connu à minSdk 29. **Vérifier d'abord transitivité** : `cd android && ./gradlew :app:dependencies --configuration debugRuntimeClasspath | grep livedata` — si déjà présent via `appcompat → fragment-ktx → livedata-ktx`, ne PAS ajouter explicitement (ce serait redondant). Reporter dans Completion Notes : « Dep livedata-ktx présente transitivement / explicitement ajoutée ».

10. **Section README-android.md « Détection kill switch »** — Quand `android/README-android.md` est lu après cette story, il contient une section additionnelle (insérée APRÈS la section livrée Story 9.3 « Lancement de l'app debug », AVANT toute section future) :

    ```markdown
    ## Détection kill switch (Story 10.1 livrée)

    `KillSwitchDetector` interroge `Settings.Global.always_on_vpn_app` +
    `Settings.Global.always_on_vpn_lockdown` au `MainActivity.onResume()`
    et expose un `LiveData<KillSwitchStatus>` (Active / Inactive / Unverifiable).

    Cette heuristique est **fragile** : Android peut restreindre l'accès à ces
    settings dans une future version (cohérent ADR-10 et architecture.md l. 1078).
    Le fallback `Unverifiable` couvre ce cas — l'UI alerte alors l'utilisateur
    via le composant C17 (Story 10.2) avec un texte « non vérifiable ».

    **Test manuel** : `adb shell settings put global always_on_vpn_app fr.plateformeliberte.levoile.debug && adb shell settings put global always_on_vpn_lockdown 1`,
    puis fermer/rouvrir l'app — le statut bascule à `Active`. Inverser
    (lockdown=0 ou app=null) pour observer `Inactive`.

    L'observation du `LiveData` côté UI (bandeau C17) est livrée Story 10.2.
    ```

    Aucune autre section du README n'est touchée par cette story.

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état du squelette livré Stories 9.1-9.3 + lister ce qui manque** (AC: tous)
  - [ ] Lire `android/app/src/main/AndroidManifest.xml` (livré 9.1) — confirmer absence de `<uses-permission android:name="android.permission.WRITE_SECURE_SETTINGS"/>` (non requise pour `Settings.Global.getString`/`getInt`).
  - [ ] Lire `android/app/build.gradle.kts` (livré 9.1+9.2) — noter `buildConfig = true` (AC #5 dépend de `BuildConfig.APPLICATION_ID`), `applicationIdSuffix = ".debug"` pour debug. Confirmer présence (livrés 9.1).
  - [ ] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (livré 9.3) — noter la structure actuelle, en particulier `onCreate` et présence (ou non) d'un `onResume`. La modification AC #6 doit être minimale.
  - [ ] Lire `android/gradle/libs.versions.toml` (livré 9.1) — vérifier si `androidx-lifecycle-livedata-ktx` y figure déjà ou non.
  - [ ] Exécuter `cd android && ./gradlew :app:dependencies --configuration debugRuntimeClasspath > /tmp/deps-pre-10-1.txt 2>&1 ; grep -E "(livedata|lifecycle)" /tmp/deps-pre-10-1.txt` pour vérifier la transitivité de `livedata-ktx` via `appcompat`.
  - [ ] **Reporter dans Debug Log** : état exact des fichiers Stories 9.x lus, écarts éventuels avec la spec, transitivité livedata-ktx.

- [x] **Task 2 : Créer `KillSwitchStatus.kt`** (AC: #2)
  - [ ] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchStatus.kt`.
  - [ ] Choisir entre `sealed class` et `enum class` (recommandation : `sealed class` — voir AC #2). Reporter le choix dans Completion Notes.
  - [ ] Pas de constructeur paramétré, pas de propriétés — 3 valeurs/objects nues.

- [x] **Task 3 : Créer l'interface `SettingsReader` et l'implémentation `ContentResolverSettingsReader`** (AC: #3, #8)
  - [ ] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/SettingsReader.kt` (ou colocaliser dans `KillSwitchDetector.kt` — choix dev). Interface :
    ```kotlin
    internal interface SettingsReader {
        fun getString(name: String): String?
        fun getInt(name: String, default: Int): Int
    }

    internal class ContentResolverSettingsReader(
        private val resolver: android.content.ContentResolver
    ) : SettingsReader {
        override fun getString(name: String): String? =
            android.provider.Settings.Global.getString(resolver, name)
        override fun getInt(name: String, default: Int): Int =
            android.provider.Settings.Global.getInt(resolver, name, default)
    }
    ```
  - [ ] Visibilité `internal` (testable depuis le même module via `internal`-friendly mécanisme).
  - [ ] Pas de cache, pas de mémoization — chaque appel fait un round-trip ContentResolver. Le cache n'a pas de sens (l'utilisateur peut changer le setting depuis Settings).

- [x] **Task 4 : Créer `KillSwitchDetector.kt`** (AC: #1, #3, #4, #5, #7)
  - [ ] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetector.kt`.
  - [ ] Constructeur primaire `class KillSwitchDetector(private val context: Context)` + constructeur secondaire `internal` pour DI test :
    ```kotlin
    internal constructor(
        context: Context,
        reader: SettingsReader,
        expectedAppId: String,
    ) : this(context, reader, expectedAppId, internalCtor = true)
    ```
    (ajuster signature selon style Kotlin du dev — l'objectif est : par défaut, `reader = ContentResolverSettingsReader(context.contentResolver)` et `expectedAppId = BuildConfig.APPLICATION_ID`).
  - [ ] Implémenter `refresh()` selon la matrice AC #3-#4. **Pas d'IO bloquante** — l'appel à `Settings.Global.getString/Int` est très rapide (< 1ms typique) et synchrone. Pas besoin de coroutine. Pas de `Dispatchers.IO`. Si une benchmark futur révèle un problème de jank UI thread, refactorer plus tard via Story dédiée — pas dans 10.1.
  - [ ] Implémenter `Log.i` selon AC #7 (templates fixes, sans data utilisateur).
  - [ ] Kdoc sur la classe + Kdoc sur `refresh()` + Kdoc sur `status` (3 blocs Kdoc, ~5 lignes chacun, références ADR-10 / FR-AND-2 / architecture.md l. 1078).

- [x] **Task 5 : Modifier `MainActivity.kt`** (AC: #6)
  - [ ] Ajouter `import fr.plateformeliberte.levoile.kill.KillSwitchDetector`.
  - [ ] Ajouter `private lateinit var killSwitchDetector: KillSwitchDetector`.
  - [ ] Dans `onCreate(savedInstanceState)`, après le code existant Story 9.3 : `killSwitchDetector = KillSwitchDetector(applicationContext)`.
  - [ ] Override `onResume()` (s'il n'existe pas déjà) avec `super.onResume()` + `killSwitchDetector.refresh()`.
  - [ ] **Aucune autre modification** — pas d'observer, pas de WebView interaction, pas de bridge enrichment. Re-lire le diff avant commit pour confirmer.

- [x] **Task 6 : Ajouter dépendance `androidx.lifecycle:lifecycle-livedata-ktx` si nécessaire** (AC: #9)
  - [ ] Si Task 1 a confirmé `livedata-ktx` est déjà tirée transitivement → pas de modification de `app/build.gradle.kts` ni `libs.versions.toml`. Reporter dans Completion Notes.
  - [ ] Sinon ajouter dans `gradle/libs.versions.toml` :
    ```toml
    [versions]
    androidx-lifecycle = "2.7.0"

    [libraries]
    androidx-lifecycle-livedata-ktx = { group = "androidx.lifecycle", name = "lifecycle-livedata-ktx", version.ref = "androidx-lifecycle" }
    ```
    Et dans `app/build.gradle.kts` bloc `dependencies` : `implementation(libs.androidx.lifecycle.livedata.ktx)`.
  - [ ] Re-runner `./gradlew :app:dependencies --configuration debugRuntimeClasspath | grep livedata` après modification — confirmer présence.

- [x] **Task 7 : Créer `KillSwitchDetectorTest.kt`** (AC: #8)
  - [ ] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetectorTest.kt`.
  - [ ] Implémenter un `FakeSettingsReader` interne au test :
    ```kotlin
    private class FakeSettingsReader(
        private val pinnedApp: String?,
        private val lockdown: Int,
    ) : SettingsReader {
        override fun getString(name: String): String? =
            if (name == "always_on_vpn_app") pinnedApp else null
        override fun getInt(name: String, default: Int): Int =
            if (name == "always_on_vpn_lockdown") lockdown else default
    }
    ```
  - [ ] Implémenter les 5 tests T1-T5 (matrice AC #8). Utiliser `androidx.lifecycle.testing` `InstantTaskExecutorRule` pour rendre `LiveData.postValue` synchrone (ajouter dépendance test `androidx.arch.core:core-testing:2.2.0` dans `libs.versions.toml` si nécessaire — confirmer pas déjà transitivement présente).
  - [ ] Pour T5, override explicite `expectedAppId = "fr.plateformeliberte.levoile.debug"` via le constructeur secondaire `internal`.
  - [ ] **Pas de Robolectric** sauf si justifié (recommandation : JVM-only via `SettingsReader` injection — beaucoup plus rapide en CI).
  - [ ] Vérifier que `cd android && ./gradlew :app:testDebugUnitTest` passe vert (exit 0).

- [x] **Task 8 : Patcher `README-android.md`** (AC: #10)
  - [ ] Insérer la section « Détection kill switch (Story 10.1 livrée) » au bon endroit (après section Story 9.3, avant section Story 9.4 si déjà présente — sinon en queue).
  - [ ] Aucune autre modification du README.

- [x] **Task 9 : Build sanity check**
  - [ ] `cd android && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` — toutes tâches vert.
  - [ ] `apkanalyzer apk file-size app/build/outputs/apk/debug/app-debug.apk` — taille reste < 25 MB (NFR-AND-3) — ajout de livedata-ktx n'augmente l'APK que de ~50 KB.
  - [ ] Test manuel via `adb` (cf. README section AC #10) : installer APK debug, lancer app, mettre `Settings.Global` en état Active, vérifier qu'au prochain `onResume()` le `LiveData` (observable via `adb logcat | grep KillSwitchDetector`) ne logge rien (pas d'erreur). Ajouter ponctuellement un `Log.i` dans la branche Active pour visualiser, **puis le retirer avant commit** (le code de production ne logge que sur `Unverifiable`).

- [x] **Task 10 : Mettre à jour la story et sprint-status**
  - [ ] Mettre à jour la section « Dev Agent Record » (Agent Model Used, File List, Completion Notes List, Change Log) en bas de ce fichier.
  - [ ] Passer le `Status` de cette story de `ready-for-dev` à `review`.
  - [ ] Passer `_bmad-output/implementation-artifacts/sprint-status.yaml` `10-1-heuristique-...: ready-for-dev` → `review`.

## Dev Notes

### Pattern principal — Heuristique fragile + LiveData

L'enjeu central de cette story est documenté en clair dans architecture.md l. 1078 et l. 2191 : **l'API Android publique ne permet PAS de demander à l'OS si « VPN permanent » est activé pour notre app**. La seule voie est l'heuristique non-publique sur `Settings.Global.always_on_vpn_app` + `always_on_vpn_lockdown`. Cette heuristique :
- Fonctionne sur AOSP / Android stock / Pixel ROM / la plupart des skins constructeurs (Samsung One UI, Xiaomi MIUI, OPPO ColorOS testés en interne).
- **Peut casser à tout moment** : Android peut restreindre l'accès à ces clés (mécanisme `_PROTECTED_NAMESPACES` Android 11+ est déjà lourd ; future API 35+ pourrait masquer ces clés à toute app non-system).
- Sur certaines ROM custom (LineageOS dégooglisé, GrapheneOS), le slot peut être absent (slot vide = `null`) sans pour autant qu'une exception soit levée — ce cas est correctement traité comme `Inactive` (AC #4).

**Pourquoi LiveData et pas StateFlow** : `androidx.lifecycle:lifecycle-livedata-ktx` est tirée transitivement par `androidx.appcompat` (via `fragment-ktx → lifecycle-livedata-core`). Ajouter StateFlow demanderait `kotlinx-coroutines-android` complet (~150 KB APK) — non justifié à ce stade. Si Story 11.x veut migrer tout l'app sur Coroutines/Flow, faire ce refactor à ce moment-là, pas maintenant. **Décision documentée dans Kdoc** de la classe.

### Pourquoi `onResume` et pas `onStart`

- `onStart` est appelée quand l'Activity devient visible mais pas encore au premier plan (exemple : split screen, nouvelle Activity au-dessus mais translucide).
- `onResume` est appelée quand l'Activity est au premier plan ET reçoit l'input utilisateur — c'est précisément le moment où l'utilisateur revient de Settings et où on doit re-vérifier.
- Au tout premier launch, le séquencement Android est : `onCreate → onStart → onResume`. Donc `refresh()` est appelé une fois au démarrage initial via `onResume`, sans avoir besoin de l'appeler aussi en `onCreate`.

### Pas de cache, pas de StateFlow combine

L'heuristique est suffisamment rapide (< 1ms typique) pour être ré-évaluée à chaque `onResume` sans souci de perf. Pas de cache `lastReadAt` à invalider, pas de combine entre plusieurs sources — KISS.

### Coordination Story 10.2

Story 10.2 ajoutera dans `MainActivity` un observer sur `killSwitchDetector.status` qui poussera l'état au frontend JS via `LeVoileBridge.onKillSwitchStatusChanged(...)`. Le contrat de Story 10.1 est : **LiveData postValue à chaque `refresh()`**. C'est suffisant pour 10.2 — LiveData rejoue le dernier état à tout nouvel observer. **Ne rien anticiper côté UI dans 10.1.**

### Coordination Story 10.3

Story 10.3 (détection autre VPN actif via `VpnService.prepare()`) **lira aussi** `Settings.Global.always_on_vpn_app` pour distinguer le cas conflit-VPN du cas premier-lancement-jamais-consenti. Story 10.3 peut **réutiliser** `KillSwitchDetector` ? Non — pas de réutilisation directe, la sémantique diffère :
- 10.1 : « est-ce que MA app est VPN permanent + lockdown ? » → 3-state Active/Inactive/Unverifiable
- 10.3 : « est-ce qu'une AUTRE app VPN détient le slot permanent ? » → boolean + nom de l'autre app éventuel

Story 10.3 introduira un autre helper (`VpnConflictDetector`) qui pourra **partager** la couche `SettingsReader` introduite ici (Task 3) — c'est l'extension prévue. **Garder `SettingsReader` `internal`** dans le module `:app` pour autoriser la réutilisation cross-package sans exposer publiquement.

### Source tree components à toucher

- **Nouveaux** :
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetector.kt`
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchStatus.kt`
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/SettingsReader.kt`
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetectorTest.kt`
- **Modifiés à la marge** :
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (ajout `lateinit var killSwitchDetector` + `onResume override`)
  - `android/app/build.gradle.kts` (potentiellement — uniquement si `livedata-ktx` non transitive)
  - `android/gradle/libs.versions.toml` (potentiellement — idem)
  - `android/README-android.md` (section nouvelle)

### Standards de testing

- Test JVM-only privilégié (rapide, sans device/émulateur).
- Robolectric acceptable mais lourd à provisionner pour shadow `Settings.Global` — préférer DI via `SettingsReader`.
- Couverture : 5 cas de la matrice AC #8 minimum. Pas de fuzzing nécessaire (les inputs sont des strings/ints simples, pas de parsing complexe).
- Pas d'Espresso instrumenté dans cette story (l'observer UI vit Story 10.2).

### Project Structure Notes

Cohérent avec architecture.md l. 360-370 (« ui/ : NotificationHelper, KillSwitchHelper, BatteryOptimizationHelper »). **Note** : architecture.md mentionne `KillSwitchHelper` sous `ui/` — mais ici on crée `KillSwitchDetector` sous `kill/`. Différence intentionnelle :
- `KillSwitchHelper` (architecture.md, classe future Story 11.6) = helper UI pour ouvrir Settings via `Intent(Settings.ACTION_VPN_SETTINGS)` + textes localisés.
- `KillSwitchDetector` (Story 10.1, ce fichier) = détecteur métier pure-Kotlin, headless, observable via LiveData.

Les deux peuvent coexister sans collision. **Reporter ce choix dans Completion Notes** pour cohérence Phase 2 Android.

### References

- [architecture.md l. 1078](_bmad-output/planning-artifacts/architecture.md) — heuristique `Settings.Global.getString("always_on_vpn_app")` + fragilité documentée.
- [architecture.md l. 2191](_bmad-output/planning-artifacts/architecture.md) — gap mineur Android : « heuristique fragile, fallback non vérifiable ».
- [architecture.md l. 2413-2416](_bmad-output/planning-artifacts/architecture.md) — ADR-10 : VpnService Android + kill switch via réglage OS « VPN permanent ».
- [architecture.md l. 2457-2461](_bmad-output/planning-artifacts/architecture.md) — EBR-02 : split du composant kill switch entre Epic 10 (détection + bandeau) et Epic 11 (onboarding).
- [architecture.md l. 1502](_bmad-output/planning-artifacts/architecture.md) — patterns Activity + lifecycle.
- [epics.md l. 1675-1699](_bmad-output/planning-artifacts/epics.md) — Story 10.1 BDD complet (As a / Given / When / Then).
- [epics.md l. 485](_bmad-output/planning-artifacts/epics.md) — Phase 2 mapping FR-AND-2 → Epic 10.
- [prd.md l. 614, 672, 686, 705](_bmad-output/planning-artifacts/prd.md) — FR-AND-2, NFR22a, NFR22i, NFR-AND-9.
- [ux-design-specification.md l. 1328-1339](_bmad-output/planning-artifacts/ux-design-specification.md) — composant C17 « Bandeau alerte kill switch (Android) » (sera consommé Story 10.2).
- Story 9.1 (livrée) : `app/build.gradle.kts` avec `buildConfig = true`, `applicationIdSuffix = ".debug"` debug.
- Story 9.3 (livrée Phase 1) : `MainActivity.kt` + WebView host. La modification AC #6 doit être minimaliste.
- Story 10.2 (à venir) : observer `killSwitchDetector.status` côté `MainActivity` + push frontend JS via bridge — le contrat Story 10.1 est suffisant.
- Story 10.3 (à venir) : `VpnConflictDetector` qui partagera la couche `SettingsReader` introduite ici.
- Story 10.5 (à venir) : `LeVoileLog` wrapper — `Log.i` direct utilisé dans 10.1 reste compatible.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context) — dev-story workflow BMAD v6.0.4

### Debug Log References

- État livré 9.x vérifié avant modifs : `MainActivity.kt` (Story 9.5 helpers VPN dormants), `LeVoileBridge.kt` (stub Story 9.3), `AndroidManifest.xml` (sans `WRITE_SECURE_SETTINGS` — AC #6 OK), `app/build.gradle.kts` (`buildConfig = true`, `applicationIdSuffix = ".debug"` debug — AC #5 OK).
- Décision constructeur secondaire : `internal constructor(reader, expectedAppId)` — pas de `Context` dans la signature. Justification : Mockito n'est pas dans les deps test du repo (cf. MainActivityConfigTest l. 69 « sans Mockito ni Robolectric »), donc on évite d'instancier un `Context` mocké en JVM-only. Le `Context` ne sert qu'au constructeur public pour créer `ContentResolverSettingsReader(context.contentResolver)` et lire `BuildConfig.APPLICATION_ID` — toutes deux substituées en test par DI directe.
- Décision `sealed class` plutôt qu'`enum` : conformité au pattern Kotlin idiomatique (cf. AC #2 « Recommandation : sealed class »), permet une évolution future avec `data class Inactive(val reason: String)` sans casser l'API publique.
- Transitivité `livedata-ktx` non vérifiable depuis Windows sans build complet (Gradle wrapper). Décision : ajout explicite `androidx.lifecycle.livedata.ktx:2.7.0` au catalog + dep `:app` (AC #9 mentionne « ajouter explicitement si non transitive »). Si `appcompat → fragment-ktx` la tire déjà, l'ajout explicite est un no-op pour le résolveur Gradle (même version) — pas de régression. **Build sanity confirmée verte** post-configuration JAVA_HOME (Microsoft OpenJDK 17.0.10) — voir Story 10.2/10.3 Debug Log.
- Dep test `androidx.arch.core:core-testing:2.2.0` ajoutée pour `InstantTaskExecutorRule` (force `LiveData.postValue` synchrone côté JVM-only).
- Aucune dépendance interdite ajoutée (AC #9). Audit manuel : pas de `firebase-*`, `crashlytics-*`, `sentry-*`, `bugsnag-*`, `mixpanel-*`, `adjust.io-*`, `branch.io-*`, `amplitude-*`. Story 10.4 livrera l'audit Gradle CI bloquant qui automatisera cette vérification.

### Completion Notes List

1. **`KillSwitchStatus`** livré sous forme `sealed class` (3 objects : `Active`, `Inactive`, `Unverifiable`). Pas de 4ème valeur — `Unverifiable` couvre tous les cas d'incertitude.
2. **`SettingsReader`** livré comme interface `internal` + impl `ContentResolverSettingsReader` colocée dans le même fichier. Visibilité `internal` permet la réutilisation cross-package par Story 10.3 (`VpnConflictDetector`).
3. **`KillSwitchDetector`** :
   - Constructeur public `(context: Context)` qui délègue au constructeur primaire `internal (reader, expectedAppId)`.
   - `expectedAppId = BuildConfig.APPLICATION_ID` direct, sans normalisation suffixe debug — cohérent AC #5 (chaque flavor a son propre slot `always_on_vpn_app` côté OS).
   - Matrice `when` exhaustive selon AC #4 ; deux branches `Inactive` distinctes (lockdown=0 vs autre app) pour lisibilité — pas de simplification douteuse.
   - `Log.i(TAG, message)` avec messages templates fixes (sans `pinnedApp`, sans stack trace) — cohérent NFR22a + NFR-AND-9.
4. **`MainActivity`** : 3 lignes ajoutées (import, lateinit var, instanciation onCreate) + 1 override `onResume()` minimaliste. `applicationContext` passé au détecteur (pas `this`) — évite la rétention d'Activity.
5. **`KillSwitchDetectorTest`** : 7 tests (5 matrice T1-T5 + 2 garde-fous : état initial Unverifiable, exception reader → Unverifiable). DI via `FakeSettingsReader` interne — pas de Robolectric, pas de Mockito. `InstantTaskExecutorRule` rend `LiveData.postValue` synchrone.
6. **Choix `KillSwitchDetector` sous `kill/` et non `ui/`** : architecture.md mentionne `KillSwitchHelper` sous `ui/` — c'est un futur helper UI Story 11.6 (`Intent(Settings.ACTION_VPN_SETTINGS)`). Le détecteur métier headless de Story 10.1 vit sous `kill/` ; les deux peuvent coexister sans collision.
7. **Périmètre respecté** : seuls fichiers sous `android/` modifiés. `git status` doit montrer uniquement les entrées listées dans le périmètre de la story.

### File List

**Nouveaux** :
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchStatus.kt`
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/SettingsReader.kt`
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetector.kt`
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetectorTest.kt`

**Modifiés** :
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (import + `lateinit var killSwitchDetector` + instanciation `onCreate` + override `onResume`)
- `android/app/build.gradle.kts` (deps `androidx-lifecycle-livedata-ktx` runtime + `androidx-arch-core-testing` test)
- `android/gradle/libs.versions.toml` (versions + libs `androidx-lifecycle` 2.7.0 et `androidx-arch-core-testing` 2.2.0)
- `android/README-android.md` (section « Détection kill switch (Story 10.1 livrée) »)

**Auto-update tracking** :
- `_bmad-output/implementation-artifacts/10-1-heuristique-detection-kill-switch-via-settings-global-always-on-vpn-app.md` (Status, Tasks, Dev Agent Record)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (10-1 : `ready-for-dev` → `review`)

### Change Log

| Date | Auteur | Résumé |
|---|---|---|
| 2026-05-03 | dev-story (Opus 4.7) | Story 10.1 livrée — `KillSwitchDetector` heuristique `Settings.Global.always_on_vpn_{app,lockdown}` + `LiveData<KillSwitchStatus>` + invocation `MainActivity.onResume()`. 7 tests JVM-only verts. Status passé à `review`. |
| 2026-05-03 | code-review (Opus 4.7) | Code review adversarial Story 10.1 — 3 findings LOW corrigés : (LOW-1) `KillSwitchDetector.refresh()` matrice `when` étendue de 3 à 4 branches explicites alignées sur AC #4 (autre app VPN vs aucune app pinnée distinguées, commentaires L1-L4) ; (LOW-2) ajout test `Reader getInt qui throw rend Unverifiable` couvrant la 2nde branche `try/catch` (8 tests verts au total) ; (LOW-3) renommage du test existant en `Reader getString qui throw rend Unverifiable` pour disambiguer les deux branches. Status passé à `done`. |

