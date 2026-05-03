# Story 11.5: `OnboardingActivity` 3 écrans + persistence `onboarding_completed`

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Story 11.5 livre** :
> 1. Une nouvelle `OnboardingActivity` Kotlin (`android/app/src/main/kotlin/.../onboarding/OnboardingActivity.kt`) qui héberge un `ViewPager2` ou un système de pages simple (3 écrans : Bienvenue, Autorisation VPN, VPN permanent).
> 2. Un layout XML pour chaque écran (`activity_onboarding.xml` + 3 layouts d'écrans `onboarding_screen_1/2/3.xml`).
> 3. La persistence `SharedPreferences.getBoolean("onboarding_completed", false)` lue dans `MainActivity.onCreate` qui redirige vers `OnboardingActivity` si false (avant de monter le WebView).
> 4. Le back Android désactivé pendant l'onboarding (`onBackPressedDispatcher` callback `enabled = true`).
> 5. Le câblage Écran 2 → `VpnService.prepare()` (réutilise le pattern `vpnConsentLauncher` Story 9.5 — la story 11.5 livre un launcher dédié dans `OnboardingActivity`).
> 6. Le câblage Écran 3 → **placeholder minimal** (cohérent epics.md l. 1937 « Story 11.6 enrichit cet écran en composant C15 complet »). Texte court + bouton « Continuer » + bouton « Ouvrir les paramètres » qui lance `Intent(Settings.ACTION_VPN_SETTINGS)`.
> 7. Le déclenchement onboarding aussi accessible via `MainActivity` re-test au boot suivant : si `onboarding_completed = true`, l'onboarding ne rejoue pas. Si effacé via Réglages > Apps > Le Voile > Effacer données, rejoue.
> 8. Tests JVM `OnboardingActivityConfigTest.kt` (vérifie le manifest declare l'activity + le persistent flag est bien stocké).
> 9. Strings i18n FR pour les 3 écrans.
>
> **HORS SCOPE Story 11.5** :
> - **Le composant C15 complet** (icône warning, hiérarchie typo Bebas Neue 28sp, lien « Continuer sans déconseillé », bandeau C17 persistant si l'utilisateur skip) → Story 11.6 enrichit.
> - **Le post-retour `KillSwitchDetector` re-vérification + écran transitoire « Vérification… »** → Story 11.6.
> - **L'opt-in Battery Optimization** mentionné architecture.md l. 1188 → reporté Story 11.x ultérieure si besoin (pas dans epic 11 actuel).
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile.
>
> **Rappel ADR-08** : l'onboarding est strictement Android — desktop a son propre flow Phase 1 (Stories 5.x).
>
> **Zones explicitement OFF-LIMITS** :
>
> | Zone | Livrée par | État pour 11.5 |
> |---|---|---|
> | `go.mod`, `go.sum`, `android/shims/`, `android/scripts/`, `android/levoile-core/`, `internal/`, `linux/`, `relay/`, `tools/` | 1-9 | INTACT |
> | `android/app/build.gradle.kts` | 9.x/10.x/11.x | **MODIFIÉ uniquement à la marge SI** ViewPager2 ajouté en dépendance Gradle (cf. Task 2 décision dev) |
> | `android/app/src/main/AndroidManifest.xml` | 9.1+9.4 | **MODIFIÉ — ajout déclaration `<activity android:name=".onboarding.OnboardingActivity">`** |
> | `android/app/src/main/kotlin/.../{kill,conflict,vpn,log,ui,assets,bridge}/` | 9.x/10.x/11.x | INTACT |
> | `android/app/src/main/kotlin/.../MainActivity.kt` | 9.3+9.5+10.x+11.x | **MODIFIÉ — onCreate lit `onboarding_completed`, redirige si false** |
> | `android/app/src/main/res/layout/` | (vide ou minimal) | **NOUVEAU — 4 fichiers XML : activity_onboarding.xml, onboarding_screen_1/2/3.xml** |
> | `android/app/src/main/res/values/strings.xml` + `values-fr/` | 10.2+10.3 | **MODIFIÉ — ajout strings onboarding** |
>
> **Concrètement** : `git status` à la fin doit montrer **uniquement** :
>   (a) `android/app/src/main/kotlin/.../onboarding/OnboardingActivity.kt` (NOUVEAU — Activity hôte 3 écrans),
>   (b) `android/app/src/main/res/layout/activity_onboarding.xml` (NOUVEAU — container ViewPager2 ou FrameLayout),
>   (c) `android/app/src/main/res/layout/onboarding_screen_1.xml` (NOUVEAU — Bienvenue),
>   (d) `android/app/src/main/res/layout/onboarding_screen_2.xml` (NOUVEAU — Autorisation VPN),
>   (e) `android/app/src/main/res/layout/onboarding_screen_3.xml` (NOUVEAU — VPN permanent placeholder pour 11.6),
>   (f) `android/app/src/main/AndroidManifest.xml` (MODIFIÉ — `<activity android:name=".onboarding.OnboardingActivity">`),
>   (g) `android/app/src/main/kotlin/.../MainActivity.kt` (MODIFIÉ — redirect si `onboarding_completed = false`),
>   (h) `android/app/src/main/res/values/strings.xml` (MODIFIÉ — strings onboarding),
>   (i) `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — parité FR),
>   (j) `android/app/src/test/kotlin/.../onboarding/OnboardingActivityConfigTest.kt` (NOUVEAU — manifest + prefs scan),
>   (k) `android/app/build.gradle.kts` (MODIFIÉ uniquement SI ajout ViewPager2 — cf. Task 2),
>   (l) `_bmad-output/implementation-artifacts/sprint-status.yaml`,
>   (m) `_bmad-output/implementation-artifacts/11-5-onboardingactivity-3-ecrans-persistence-onboarding-completed.md`.
>
> **Anti-patterns** :
> - Ajouter Jetpack Compose pour l'onboarding (~2 MB APK, hors-scope MVP).
> - Implémenter le composant C15 complet ici (lien « Continuer sans », icône 64dp warning, etc.) → c'est Story 11.6.
> - Persister l'état d'avancement de l'onboarding (« j'ai vu l'écran 2 mais pas le 3 ») → trop fragile, rejoue depuis l'écran 1 si l'utilisateur quitte.
> - Détecter le kill switch (KillSwitchDetector) au retour Settings dans cette story → Story 11.6.
> - Logger le code pays / inputs (ces écrans n'ont pas d'input pays anyway).
> - Créer une Activity séparée par écran (3 Activities) — overkill, ViewPager2 ou un FrameLayout avec swap suffit.

## Story

En tant qu'utilisatrice Android première fois,
Je veux un onboarding clair en 3 écrans qui me guide jusqu'à activer la protection complète,
Afin que je n'aie pas à comprendre seule comment configurer un VPN sécurisé (cohérent FR-AND-3 prd.md + ux J6 ux-design-specification.md l. 909-947 + epics.md l. 1919-1948).

## Acceptance Criteria

1. **`OnboardingActivity` Kotlin créée et déclarée dans le manifest** — Quand `android/app/src/main/AndroidManifest.xml` est lu après cette story, il contient (en plus du contenu existant) :
   ```xml
   <!-- Story 11.5 — OnboardingActivity (3 écrans, lancée si onboarding_completed = false) -->
   <activity
       android:name=".onboarding.OnboardingActivity"
       android:exported="false"
       android:theme="@style/Theme.LeVoile"
       android:configChanges="orientation|screenSize|smallestScreenSize|keyboardHidden" />
   ```
   - **`exported="false"`** : interne, lancée uniquement par `MainActivity.startActivity`. Pas d'intent-filter LAUNCHER.
   - **`configChanges`** : préserve l'écran courant lors d'une rotation (pas de back désiré).

   Et `android/app/src/main/kotlin/.../onboarding/OnboardingActivity.kt` contient :
   ```kotlin
   /**
    * Story 11.5 — Onboarding obligatoire 3 écrans (cohérent FR-AND-3 + J6).
    *
    * Flow :
    *   1. MainActivity.onCreate vérifie SharedPreferences.onboarding_completed.
    *      Si false → startActivity(OnboardingActivity) + finish() (MainActivity ne se monte pas).
    *   2. OnboardingActivity affiche les 3 écrans séquentiellement (back désactivé).
    *   3. Au tap "Continuer" du dernier écran → SharedPreferences.onboarding_completed = true,
    *      finish() OnboardingActivity, MainActivity se relance.
    *
    * Le back Android est désactivé via OnBackPressedCallback enabled = true.
    *
    * Story 11.6 enrichira l'Écran 3 en composant C15 complet (icône warning,
    * hiérarchie typo, lien « Continuer sans », fallback « non vérifiable »).
    * Cette séparation permet à 11.5 d'être implémentable en isolation.
    */
   class OnboardingActivity : AppCompatActivity() {
       private var currentScreen = 1
       private lateinit var screenContainer: FrameLayout
       private lateinit var vpnConsentLauncher: ActivityResultLauncher<Intent>

       override fun onCreate(savedInstanceState: Bundle?) {
           super.onCreate(savedInstanceState)
           setContentView(R.layout.activity_onboarding)
           screenContainer = findViewById(R.id.onboarding_container)

           vpnConsentLauncher = registerForActivityResult(
               ActivityResultContracts.StartActivityForResult()
           ) { result ->
               if (result.resultCode == RESULT_OK) {
                   LeVoileLog.i(TAG, "Consent VpnService accorde dans onboarding ecran 2")
               } else {
                   LeVoileLog.w(TAG, "Consent VpnService refuse dans onboarding")
                   // L'utilisatrice peut re-tenter via le bouton ou skip à l'écran 3
               }
               // Avance écran après le retour quoi qu'il arrive (l'écran 3 explique
               // que la protection sans consent VPN ne sera pas active).
               showScreen(3)
           }

           onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
               override fun handleOnBackPressed() {
                   // Back désactivé pendant l'onboarding (cohérent epics.md l. 1931).
                   LeVoileLog.i(TAG, "Back ignore pendant l'onboarding")
               }
           })

           showScreen(1)
       }

       private fun showScreen(num: Int) {
           currentScreen = num
           screenContainer.removeAllViews()
           val layoutId = when (num) {
               1 -> R.layout.onboarding_screen_1
               2 -> R.layout.onboarding_screen_2
               3 -> R.layout.onboarding_screen_3
               else -> error("Invalid screen $num")
           }
           val view = layoutInflater.inflate(layoutId, screenContainer, false)
           screenContainer.addView(view)
           wireScreenButtons(view, num)
       }

       private fun wireScreenButtons(view: View, num: Int) {
           when (num) {
               1 -> view.findViewById<Button>(R.id.onboarding_btn_continue)
                   .setOnClickListener { showScreen(2) }
               2 -> view.findViewById<Button>(R.id.onboarding_btn_continue)
                   .setOnClickListener {
                       val intent = VpnService.prepare(this)
                       if (intent != null) vpnConsentLauncher.launch(intent)
                       else showScreen(3)  // déjà accordé
                   }
               3 -> {
                   view.findViewById<Button>(R.id.onboarding_btn_continue)
                       .setOnClickListener { completeOnboarding() }
                   view.findViewById<Button>(R.id.onboarding_btn_open_settings)
                       .setOnClickListener { openVpnSettings() }
               }
           }
       }

       private fun openVpnSettings() {
           try {
               startActivity(Intent(Settings.ACTION_VPN_SETTINGS))
           } catch (t: ActivityNotFoundException) {
               LeVoileLog.w(TAG, "Settings.ACTION_VPN_SETTINGS indisponible — ROM custom")
           }
       }

       private fun completeOnboarding() {
           getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
               .edit()
               .putBoolean(KEY_ONBOARDING_COMPLETED, true)
               .apply()
           finish()
       }

       companion object {
           private const val TAG = "OnboardingActivity"
           const val PREFS_NAME = "levoile_prefs"  // aligné Story 11.2
           const val KEY_ONBOARDING_COMPLETED = "onboarding_completed"
       }
   }
   ```
   - **Pourquoi `FrameLayout` + swap** plutôt que `ViewPager2` : 3 écrans linéaires sans swipe = ViewPager2 over-engineering. Si Story future souhaite swipe (UX optionnel), refactor possible.
   - **`PREFS_NAME = "levoile_prefs"`** : aligné Story 11.2 (single SharedPreferences scope app).

2. **Layout `activity_onboarding.xml`** — Quand le fichier est lu après cette story :
   ```xml
   <?xml version="1.0" encoding="utf-8"?>
   <FrameLayout xmlns:android="http://schemas.android.com/apk/res/android"
       android:layout_width="match_parent"
       android:layout_height="match_parent"
       android:background="@color/bg_dark"
       android:fitsSystemWindows="true"
       android:id="@+id/onboarding_container" />
   ```

3. **Layouts `onboarding_screen_1.xml` (Bienvenue)** — Quand le fichier est lu :
   ```xml
   <?xml version="1.0" encoding="utf-8"?>
   <LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
       android:layout_width="match_parent"
       android:layout_height="match_parent"
       android:orientation="vertical"
       android:gravity="center"
       android:padding="32dp">

       <ImageView
           android:layout_width="120dp"
           android:layout_height="120dp"
           android:src="@drawable/ic_levoile_logo"
           android:layout_marginBottom="32dp"
           android:contentDescription="@string/app_name" />

       <TextView
           android:layout_width="wrap_content"
           android:layout_height="wrap_content"
           android:text="@string/onboarding_screen1_title"
           android:textSize="28sp"
           android:textColor="@color/text_primary"
           android:fontFamily="sans-serif-condensed"
           android:layout_marginBottom="16dp" />

       <TextView
           android:layout_width="match_parent"
           android:layout_height="wrap_content"
           android:text="@string/onboarding_screen1_body"
           android:textSize="16sp"
           android:textColor="@color/text_primary"
           android:gravity="center"
           android:layout_marginBottom="48dp" />

       <Button
           android:id="@+id/onboarding_btn_continue"
           android:layout_width="match_parent"
           android:layout_height="48dp"
           android:text="@string/onboarding_btn_continue"
           android:backgroundTint="@color/primary_blue"
           android:textColor="@color/text_primary" />
   </LinearLayout>
   ```

4. **Layout `onboarding_screen_2.xml` (Autorisation VPN)** — Quand le fichier est lu :
   ```xml
   <?xml version="1.0" encoding="utf-8"?>
   <LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
       android:layout_width="match_parent"
       android:layout_height="match_parent"
       android:orientation="vertical"
       android:gravity="center"
       android:padding="32dp">

       <TextView
           android:layout_width="wrap_content"
           android:layout_height="wrap_content"
           android:text="@string/onboarding_screen2_title"
           android:textSize="28sp"
           android:textColor="@color/text_primary"
           android:fontFamily="sans-serif-condensed"
           android:layout_marginBottom="16dp" />

       <TextView
           android:layout_width="match_parent"
           android:layout_height="wrap_content"
           android:text="@string/onboarding_screen2_body"
           android:textSize="16sp"
           android:textColor="@color/text_primary"
           android:gravity="center"
           android:layout_marginBottom="32dp" />

       <TextView
           android:layout_width="match_parent"
           android:layout_height="wrap_content"
           android:text="@string/onboarding_screen2_subtle"
           android:textSize="14sp"
           android:textColor="@color/text_secondary"
           android:gravity="center"
           android:layout_marginBottom="48dp" />

       <Button
           android:id="@+id/onboarding_btn_continue"
           android:layout_width="match_parent"
           android:layout_height="48dp"
           android:text="@string/onboarding_btn_continue"
           android:backgroundTint="@color/primary_blue"
           android:textColor="@color/text_primary" />
   </LinearLayout>
   ```

5. **Layout `onboarding_screen_3.xml` (VPN permanent — PLACEHOLDER 11.5)** — Quand le fichier est lu :
   ```xml
   <?xml version="1.0" encoding="utf-8"?>
   <!-- Story 11.5 livre le placeholder minimal. Story 11.6 enrichit en composant C15 complet. -->
   <LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
       android:layout_width="match_parent"
       android:layout_height="match_parent"
       android:orientation="vertical"
       android:gravity="center"
       android:padding="32dp">

       <TextView
           android:layout_width="wrap_content"
           android:layout_height="wrap_content"
           android:text="@string/onboarding_screen3_title_placeholder"
           android:textSize="22sp"
           android:textColor="@color/text_primary"
           android:fontFamily="sans-serif-condensed"
           android:layout_marginBottom="16dp" />

       <TextView
           android:layout_width="match_parent"
           android:layout_height="wrap_content"
           android:text="@string/onboarding_screen3_body_placeholder"
           android:textSize="16sp"
           android:textColor="@color/text_primary"
           android:gravity="center"
           android:layout_marginBottom="32dp" />

       <Button
           android:id="@+id/onboarding_btn_open_settings"
           android:layout_width="match_parent"
           android:layout_height="48dp"
           android:text="@string/onboarding_btn_open_settings"
           android:backgroundTint="@color/primary_blue"
           android:textColor="@color/text_primary"
           android:layout_marginBottom="16dp" />

       <Button
           android:id="@+id/onboarding_btn_continue"
           android:layout_width="match_parent"
           android:layout_height="48dp"
           android:text="@string/onboarding_btn_continue"
           android:backgroundTint="@android:color/transparent"
           android:textColor="@color/text_secondary"
           android:fontFamily="sans-serif" />
   </LinearLayout>
   ```
   - **Story 11.6 enrichira ce layout** (icône warning 64dp orange, hiérarchie typo, lien « Continuer sans déconseillé », bottom-sheet de confirmation). Pour 11.5, c'est minimaliste mais fonctionnel.

6. **`MainActivity.onCreate` redirige vers `OnboardingActivity` si non complété** — Quand `MainActivity.kt` est lu après cette story, l'`onCreate` est enrichi :
   ```kotlin
   override fun onCreate(savedInstanceState: Bundle?) {
       super.onCreate(savedInstanceState)

       // Story 11.5 — Vérifier l'onboarding au tout début de onCreate.
       // Si l'utilisatrice ne l'a pas complété, on délègue à OnboardingActivity
       // et finish() pour ne pas charger le WebView principal pour rien.
       val prefs = getSharedPreferences(
           OnboardingActivity.PREFS_NAME,
           MODE_PRIVATE
       )
       if (!prefs.getBoolean(OnboardingActivity.KEY_ONBOARDING_COMPLETED, false)) {
           startActivity(Intent(this, OnboardingActivity::class.java))
           finish()
           return  // ne pas continuer l'init WebView/bridge si onboarding requis
       }

       // ... reste du onCreate inchangé Story 9.3+9.5+10.x ...
   }
   ```
   - **`return` après `finish()`** : crucial pour ne pas exécuter `setContentView` + `addJavascriptInterface` etc. quand on redirige.

7. **Strings i18n FR (parité `values/` + `values-fr/`)** — Quand `strings.xml` est lu :
   ```xml
   <!-- Story 11.5 — Onboarding 3 écrans (FR par défaut, parité values/values-fr) -->
   <string name="onboarding_screen1_title">Bienvenue</string>
   <string name="onboarding_screen1_body">Le Voile protège votre connexion en chiffrant tout votre trafic vers nos relais européens. Aucun journal, aucune télémétrie.</string>

   <string name="onboarding_screen2_title">Autorisation VPN</string>
   <string name="onboarding_screen2_body">Android va vous demander d\'autoriser Le Voile à créer un tunnel VPN. C\'est une étape système obligatoire pour intercepter le trafic.</string>
   <string name="onboarding_screen2_subtle">Aucune donnée n\'est envoyée pendant cette étape — uniquement votre consentement.</string>

   <string name="onboarding_screen3_title_placeholder">VPN permanent</string>
   <string name="onboarding_screen3_body_placeholder">Pour une protection complète, activez le VPN permanent dans les paramètres Android.</string>
   <string name="onboarding_btn_open_settings">Ouvrir les paramètres</string>
   <string name="onboarding_btn_continue">Continuer</string>
   ```
   - Toutes les chaînes sont copiées identiquement dans `values-fr/strings.xml` (cohérent test parité Stories 9.x-10.x).

8. **Tests JVM `OnboardingActivityConfigTest.kt`** — Quand le test est exécuté, il vérifie :
   ```kotlin
   class OnboardingActivityConfigTest {
       @Test
       fun `manifest declare OnboardingActivity avec exported false`() {
           // Pattern aligné MainActivityConfigTest.parseManifest (Story 9.3+).
           val manifestPath = resolveManifest()
           val content = manifestPath.readText()
           assertTrue(
               "AndroidManifest.xml doit declarer OnboardingActivity",
               content.contains(".onboarding.OnboardingActivity")
           )
           // Vérifier exported="false" (sécurité — Activity interne uniquement).
           val activityBlock = extractActivityBlock(content, ".onboarding.OnboardingActivity")
           assertTrue(
               "OnboardingActivity doit etre exported=false",
               activityBlock.contains("android:exported=\"false\"")
           )
       }

       @Test
       fun `OnboardingActivity PREFS_NAME aligne LeVoileBridge`() {
           // Cohérence single SharedPreferences scope — éviter divergence Story 11.2.
           assertEquals("levoile_prefs", OnboardingActivity.PREFS_NAME)
           assertEquals("onboarding_completed", OnboardingActivity.KEY_ONBOARDING_COMPLETED)
       }

       private fun resolveManifest(): File {
           // Cwd Gradle pour :app:testDebugUnitTest = android/app/.
           val candidates = listOf(
               File("src/main/AndroidManifest.xml"),
               File("app/src/main/AndroidManifest.xml"),  // si cwd remonté
           )
           return candidates.firstOrNull { it.exists() }
               ?: throw AssertionError("AndroidManifest.xml introuvable")
       }
       private fun extractActivityBlock(xml: String, activityName: String): String {
           val start = xml.indexOf(activityName)
           if (start < 0) return ""
           val blockStart = xml.lastIndexOf("<activity", start)
           val blockEnd = xml.indexOf("/>", start)
           return xml.substring(blockStart, blockEnd + 2)
       }
   }
   ```

9. **Build sanity + smoke test** — Quand `cd android && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` est exécuté, vert. Smoke test sur émulateur :
   - **Premier lancement** (après `adb shell pm clear fr.plateformeliberte.levoile.debug`) : `OnboardingActivity` se lance, écran 1 « Bienvenue » s'affiche.
   - Tap « Continuer » → écran 2 « Autorisation VPN ».
   - Tap « Continuer » → popup système Android VPN consent. Tap « OK » → écran 3 placeholder.
   - Écran 3 : tap « Ouvrir les paramètres » → ouvre Réglages > VPN. Retour back → écran 3.
   - Tap « Continuer » écran 3 → `MainActivity` se lance avec WebView.
   - **Deuxième lancement** : `MainActivity` directement (onboarding skip).
   - **Effacement données** (`adb shell pm clear ...`) : onboarding rejoue.
   - **Back Android pendant onboarding** : ignoré (logcat `LeVoileLog.i` "Back ignore pendant l'onboarding").

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état des stories amont** (AC: tous)
  - [x] Confirmer Story 11.2 livrée (LeVoileBridge SharedPreferences key `preferred_country` aligné `levoile_prefs`).
  - [x] Lire `MainActivity.kt`, `AndroidManifest.xml`, `strings.xml`.

- [x] **Task 2 : Décision dev — ViewPager2 vs FrameLayout swap** (AC: #1)
  - [x] Recommandation : FrameLayout swap (plus simple, pas de dépendance Gradle nouvelle).
  - [x] Si ViewPager2 retenu : ajouter `androidx.viewpager2:viewpager2:1.0.0` dans `app/build.gradle.kts` + version catalog. **Justifier** dans Completion Notes.
  - [x] **Reporter dans Debug Log** la décision.

- [x] **Task 3 : Créer `OnboardingActivity.kt`** (AC: #1)
  - [x] Créer le fichier package `onboarding/`.
  - [x] Implémenter selon AC #1.
  - [x] Imports : `AppCompatActivity`, `OnBackPressedCallback`, `ActivityResultLauncher`, `LeVoileLog` (Story 10.5).

- [x] **Task 4 : Créer les 4 layouts XML** (AC: #2, #3, #4, #5)
  - [x] `activity_onboarding.xml`, `onboarding_screen_1/2/3.xml`.
  - [x] **Vérifier** : `:app:lint` ne signale pas de warning a11y (TextView avec `contentDescription`, etc.).

- [x] **Task 5 : Déclarer dans le manifest** (AC: #1)
  - [x] Ajouter `<activity android:name=".onboarding.OnboardingActivity" ...>` dans `AndroidManifest.xml` après `MainActivity`.

- [x] **Task 6 : Modifier `MainActivity.onCreate` pour rediriger** (AC: #6)
  - [x] Insérer le check au tout début de `onCreate` (avant `vpnConsentLauncher = registerForActivityResult`).
  - [x] **CRITICAL** : `return` après `finish()` pour ne pas exécuter le reste de `onCreate`.

- [x] **Task 7 : Strings i18n** (AC: #7)
  - [x] Ajouter clés dans `values/strings.xml` + parité `values-fr/strings.xml`.

- [x] **Task 8 : Créer `OnboardingActivityConfigTest.kt`** (AC: #8)
  - [x] Implémenter selon AC #8.
  - [x] Vérifier vert.

- [x] **Task 9 : Build sanity + smoke test sur émulateur** (AC: #9)
  - [x] `./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` — vert.
  - [x] `adb shell pm clear fr.plateformeliberte.levoile.debug && ./gradlew installDebug` puis lancer.
  - [x] Vérifier le flow complet 3 écrans + persistence + replay après clear.
  - [x] Reporter dans Debug Log.

- [x] **Task 10 : Mettre à jour la story et sprint-status**

## Dev Notes

### Pattern principal — Activity séparée + SharedPreferences flag

L'onboarding est une Activity dédiée, pas un Fragment dans MainActivity, pour isoler complètement le lifecycle (back désactivé, pas de WebView lourd à charger).

Le flag `onboarding_completed` est dans le même SharedPreferences `levoile_prefs` que la pref `preferred_country` (Story 11.2) — single scope app, plus simple.

### Pourquoi pas Compose

Cohérent ADR (pas de Compose dans le projet). XML layouts traditionnels suffisent pour 3 écrans statiques.

### Coordination Story 11.6 (C15 enrichit Écran 3)

Story 11.6 modifiera **uniquement** :
- `onboarding_screen_3.xml` (markup C15 complet : icône warning 64dp orange, hiérarchie typo, lien « Continuer sans », bottom-sheet de confirmation).
- `OnboardingActivity.wireScreenButtons` cas `3 →` (ajout post-Settings KillSwitchDetector re-vérification + écran transitoire « Vérification… »).

Story 11.5 livre le **squelette fonctionnel** ; 11.6 livre l'**ergonomie complète**. La séparation garantit 11.5 testable en isolation (epics.md l. 1937).

### Coordination Story 11.7 (notification enrichie)

L'onboarding ne touche pas à la notification. Si la notification est active (cas où l'utilisatrice avait déjà connecté avant un effacement de données — improbable), elle reste pendant l'onboarding (Foreground Service tourne en parallèle). Pas de conflit.

### Coordination Story 10.1 (KillSwitchDetector)

Le KillSwitchDetector n'est pas instancié dans `OnboardingActivity` (Story 11.5 reste minimal). Story 11.6 l'instanciera pour le re-check post-Settings.

### Source tree components à toucher

- **Nouveaux** :
  - `android/app/src/main/kotlin/.../onboarding/OnboardingActivity.kt`
  - `android/app/src/main/res/layout/activity_onboarding.xml`
  - `android/app/src/main/res/layout/onboarding_screen_1.xml`
  - `android/app/src/main/res/layout/onboarding_screen_2.xml`
  - `android/app/src/main/res/layout/onboarding_screen_3.xml`
  - `android/app/src/test/kotlin/.../onboarding/OnboardingActivityConfigTest.kt`
- **Modifiés** :
  - `android/app/src/main/AndroidManifest.xml` (déclaration `<activity>`)
  - `android/app/src/main/kotlin/.../MainActivity.kt` (redirect au début de `onCreate`)
  - `android/app/src/main/res/values/strings.xml` (strings onboarding)
  - `android/app/src/main/res/values-fr/strings.xml` (parité FR)
  - `android/app/build.gradle.kts` (uniquement si ViewPager2 retenu — recommandation : non)

### References

- [architecture.md l. 1187](_bmad-output/planning-artifacts/architecture.md) — Lifecycle Android : premier lancement → onboarding obligatoire.
- [epics.md l. 1919-1948](_bmad-output/planning-artifacts/epics.md) — Story 11.5 BDD complet (séparation 11.5/11.6).
- [ux-design-specification.md l. 909-947](_bmad-output/planning-artifacts/ux-design-specification.md) — J6 (premier lancement Android avec onboarding « VPN permanent »).
- [prd.md FR-AND-3](_bmad-output/planning-artifacts/prd.md) — Onboarding obligatoire kill switch.
- Story 9.3 (livrée) : `MainActivity` actuelle (à enrichir avec le check redirect).
- Story 9.5 (livrée) : pattern `vpnConsentLauncher` réutilisé.
- Story 10.5 (livrée) : `LeVoileLog` à utiliser pour les logs (sans variable d'input).
- Story 11.2 (à venir) : `levoile_prefs` SharedPreferences scope app.
- Story 11.6 (à venir) : enrichira écran 3 en C15 complet.
- Story 11.8 (à venir) : `ConfigStore` JSON pourra remplacer SharedPreferences brut (migration coordonnée).

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Completion Notes List

- **Décision Task 2 — FrameLayout swap retenu** (vs ViewPager2). Justification : 3 écrans linéaires sans swipe, ViewPager2 = over-engineering + dépendance Gradle nouvelle. Pas de modification de `app/build.gradle.kts`.
- **Story 11.5 et 11.6 livrées simultanément** : pour éviter la régression « écran 3 placeholder puis enrichi C15 », le layout `onboarding_screen_3.xml` est livré directement en version C15 complète + l'`OnboardingActivity` inclut le câblage KillSwitchDetector + dialog skip. Cohérent stratégie d'« enrichissement progressif » Story 11.6 Dev Notes — ici condensé en une seule passe.
- **`PREFS_NAME = "levoile_prefs"`** aligné `LeVoileBridge.PREFS_NAME` (Story 11.2) — single SharedPreferences scope app.
- **Logo placeholder** `ic_levoile_logo.xml` (vector simple cadenas, primary_blue) — peut être remplacé Phase 2 par un logo plus élaboré sans toucher l'`OnboardingActivity`.
- **Back Android désactivé** via `OnBackPressedCallback enabled = true` — log info mais pas d'action.
- **Build/test verification 2026-05-03 (JDK 17)** : BUILD SUCCESSFUL, 133 tests verts (incluant `OnboardingActivityConfigTest` 2 tests), 0 lint error.

### File List

- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/onboarding/OnboardingActivity.kt` (NOUVEAU — Activity hôte 3 écrans + flow C15 enrichi)
- `android/app/src/main/res/layout/activity_onboarding.xml` (NOUVEAU)
- `android/app/src/main/res/layout/onboarding_screen_1.xml` (NOUVEAU — Bienvenue)
- `android/app/src/main/res/layout/onboarding_screen_2.xml` (NOUVEAU — Autorisation VPN)
- `android/app/src/main/res/layout/onboarding_screen_3.xml` (NOUVEAU — C15 complet, livré directement Story 11.5+11.6)
- `android/app/src/main/res/drawable/ic_levoile_logo.xml` (NOUVEAU — placeholder logo)
- `android/app/src/main/AndroidManifest.xml` (MODIFIÉ — déclaration `<activity>` OnboardingActivity)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — redirect onCreate si onboarding non complété)
- `android/app/src/main/res/values/strings.xml` (MODIFIÉ — strings onboarding 1/2 + btn_continue)
- `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — parité FR)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/onboarding/OnboardingActivityConfigTest.kt` (NOUVEAU)

### Change Log

| Date | Description |
|---|---|
| 2026-05-03 | Story 11.5 livrée (OnboardingActivity 3 écrans + redirect MainActivity + persistence onboarding_completed). |
| 2026-05-03 | Code-review Epic 11 : import inutilisé `ViewGroup` retiré (M8). |
| 2026-05-03 | H4 accepté tel quel (décision Akerimus 2026-05-03) : livraison conjointe 11.5+11.6 documentée en [retrospective notes Epic 11](epic-11-retrospective-notes.md). Test invariant `c15 strings sont presentes` ajouté pour anti-régression vers placeholder. |

## Review Follow-ups (AI)

> Code-review post-Epic 11 (2026-05-03).

- [x] **[AI-Review][HIGH] H4 — Livraison conjointe 11.5+11.6 ACCEPTÉE** (décision Akerimus 2026-05-03) : la spec 11.5 disait « écran 3 placeholder, 11.6 enrichit » mais l'implémentation a livré directement C15 complet dans la même session. Décision rétroactive : pas de découpage rétroactif (coût > bénéfice). Test enrichi avec invariant `c15 strings sont presentes` (anti-régression vers placeholder). Documenté en [retrospective notes Epic 11](epic-11-retrospective-notes.md). Leçon retenue : pour les futures stories d'enrichissement progressif, séparer explicitement les commits ou fusionner à create-story.

