# Story 11.6: Composant C15 — Onboarding Kill Switch Screen + deeplink Settings

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Story 11.6 livre l'enrichissement du placeholder Story 11.5 → composant C15 complet** (cohérent ux-design-specification.md l. 1285-1303 + epics.md l. 1950-1979) :
> 1. Modification du layout `onboarding_screen_3.xml` : icône warning ⚠️ 64dp orange, titre « Une dernière étape » Bebas Neue 28sp, texte explicatif 3 lignes max Inter 16sp max-width 320dp, sous-texte conséquence 2 lignes Inter 14sp opacity 0.7.
> 2. Bouton primaire pleine largeur (Rajdhani 600 16sp, hauteur 48dp) : « OUVRIR LES PARAMÈTRES ».
> 3. Lien discret (Inter 13sp, opacity 0.5, underline) : « Continuer sans (déconseillé) ».
> 4. Modification de `OnboardingActivity` : enrichissement du `wireScreenButtons` cas `3 →` :
>    - Au tap « OUVRIR LES PARAMÈTRES » → `Intent(Settings.ACTION_VPN_SETTINGS)`.
>    - Au retour `onResume`, instancier `KillSwitchDetector` (Story 10.1) + re-vérification heuristique.
>    - Écran transitoire « Vérification… » (1s).
>    - Si `Active` → `completeOnboarding()`.
>    - Si `Inactive` ou `Unverifiable` → afficher boutons « Réessayer » + « J'ai vérifié manuellement ».
> 5. Au tap lien discret « Continuer sans (déconseillé) » → bottom-sheet de confirmation Material : « Continuer sans le kill switch ? Vous pourrez l'activer plus tard. » + boutons « Annuler » + « Continuer sans ».
>    - Si confirmation → `onboarding_completed = true` + `MainActivity` se lance + bandeau C17 (Story 10.2) reste persistant.
> 6. Strings i18n FR enrichies pour C15 + bottom-sheet de confirmation.
> 7. Tests JVM `OnboardingC15FlowTest.kt` (vérifie le câblage KillSwitchDetector au retour Settings + persistence skip).
> 8. Drawable vector `ic_warning_orange.xml` 64dp.
>
> **HORS SCOPE Story 11.6** :
> - Modification structurelle de `OnboardingActivity` (3 écrans → autre nombre, ViewPager2, etc.) — Story 11.5 a fixé l'architecture.
> - Refactor de `KillSwitchDetector` (Story 10.1) — utilisé tel quel.
> - Bandeau C17 (Story 10.2) — utilisé tel quel.
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile.
>
> **Rappel ADR-08** : C15 est strictement Android. Cohérent ux-design-specification.md l. 1285-1303.
>
> **Zones explicitement OFF-LIMITS** :
>
> | Zone | Livrée par | État pour 11.6 |
> |---|---|---|
> | `go.mod`, `go.sum`, `android/shims/`, `android/scripts/`, `android/levoile-core/`, `internal/`, `linux/`, `relay/`, `tools/`, `windows/`, `linux/` | 1-9 | INTACT |
> | `android/app/build.gradle.kts` | 9.x/10.x/11.x | INTACT (KillSwitchDetector déjà disponible Story 10.1) |
> | `android/app/src/main/AndroidManifest.xml` | 9.1+9.4+11.5 | INTACT |
> | `android/app/src/main/kotlin/.../{kill,conflict,vpn,log,ui,assets,bridge}/` | 9.x/10.x/11.x | **MODIFIÉ uniquement à la marge SI** un nouveau callback est ajouté à `KillSwitchDetector` — recommandation : non, utiliser l'API existante `detector.refresh()` + `detector.status.value`. INTACT |
> | `android/app/src/main/kotlin/.../onboarding/OnboardingActivity.kt` | 11.5 | **MODIFIÉ — enrichissement cas écran 3** |
> | `android/app/src/main/res/layout/onboarding_screen_3.xml` | 11.5 (placeholder) | **MODIFIÉ — composant C15 complet remplace placeholder** |
> | `android/app/src/main/res/layout/dialog_skip_killswitch.xml` | (absent) | **NOUVEAU — bottom-sheet de confirmation** |
> | `android/app/src/main/res/drawable/ic_warning_orange.xml` | (absent) | **NOUVEAU — vector warning 64dp** |
> | `android/app/src/main/res/values/strings.xml` + `values-fr/` | 11.5 | **MODIFIÉ — strings C15 + bottom-sheet skip** |
>
> **Concrètement** : `git status` à la fin doit montrer **uniquement** :
>   (a) `android/app/src/main/kotlin/.../onboarding/OnboardingActivity.kt` (MODIFIÉ — enrichissement écran 3),
>   (b) `android/app/src/main/res/layout/onboarding_screen_3.xml` (MODIFIÉ — placeholder remplacé par C15 complet),
>   (c) `android/app/src/main/res/layout/dialog_skip_killswitch.xml` (NOUVEAU),
>   (d) `android/app/src/main/res/drawable/ic_warning_orange.xml` (NOUVEAU — vector drawable),
>   (e) `android/app/src/main/res/values/strings.xml` (MODIFIÉ — strings C15),
>   (f) `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — parité FR),
>   (g) `android/app/src/test/kotlin/.../onboarding/OnboardingC15FlowTest.kt` (NOUVEAU),
>   (h) `_bmad-output/implementation-artifacts/sprint-status.yaml`,
>   (i) `_bmad-output/implementation-artifacts/11-6-composant-c15-onboarding-kill-switch-screen-deeplink-settings.md`.
>
> **Anti-patterns** :
> - Forcer l'utilisateur à activer le kill switch (rendre « Continuer sans » invisible) — UX hostile, viole le principe d'autonomie utilisateur (Stories Phase 2 desktop ont la même règle : « consentement explicite des contournements » ux-design-specification.md).
> - Persister un flag `kill_switch_skipped = true` SANS refléter ça dans le bandeau C17 — incohérent (le bandeau Story 10.2 dépend du `KillSwitchDetector.status`, pas d'un flag UI).
> - Ouvrir un dialog Compose pour la confirmation skip — utiliser `BottomSheetDialog` Material `com.google.android.material:material:1.x` OU un layout XML custom (cohérent UI strategy ux-design-specification.md l. 1257). **Recommandation** : XML layout custom (pas de dépendance Material). Reporter dans Completion Notes.
> - Ajouter une animation / micro-interaction non documentée (vibration, son) — viole channel `levoile_vpn_status` IMPORTANCE_LOW silencieux (Story 9.6).
> - Logger les inputs ou variables sensibles (Story 10.5).
> - Toucher à `KillSwitchDetector.kt` (Story 10.1) — l'utiliser tel quel.

## Story

En tant qu'utilisatrice Android première fois,
Je veux un écran clair qui me guide à activer « VPN permanent » dans Settings via un seul tap, avec la possibilité explicite de skip si je l'assume,
Afin que ma protection complète soit active dès le premier usage OU que je sache exactement à quoi je renonce (cohérent FR-AND-3 prd.md + ADR-10 architecture.md + epics.md l. 1950-1979 + ux-design-specification.md l. 1285-1303).

## Acceptance Criteria

1. **Layout `onboarding_screen_3.xml` enrichi en C15 complet** — Quand le fichier est lu après cette story, il remplace le placeholder Story 11.5 :
   ```xml
   <?xml version="1.0" encoding="utf-8"?>
   <!-- Story 11.6 — Composant C15 complet (remplace le placeholder Story 11.5).
        Cohérent ux-design-specification.md l. 1285-1303 + epics.md l. 1956-1979. -->
   <LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
       android:layout_width="match_parent"
       android:layout_height="match_parent"
       android:orientation="vertical"
       android:gravity="center"
       android:padding="32dp">

       <!-- Icône warning orange 64dp (cohérent ux l. 1289) -->
       <ImageView
           android:layout_width="64dp"
           android:layout_height="64dp"
           android:src="@drawable/ic_warning_orange"
           android:contentDescription="@string/c15_warning_icon_desc"
           android:layout_marginBottom="24dp" />

       <!-- Titre « Une dernière étape » Bebas Neue 28sp -->
       <TextView
           android:layout_width="wrap_content"
           android:layout_height="wrap_content"
           android:text="@string/c15_title"
           android:textSize="28sp"
           android:textColor="@color/text_primary"
           android:fontFamily="sans-serif-condensed"
           android:layout_marginBottom="16dp" />

       <!-- Texte explicatif Inter 16sp max-width 320dp (3 lignes max) -->
       <TextView
           android:layout_width="wrap_content"
           android:layout_height="wrap_content"
           android:maxWidth="320dp"
           android:text="@string/c15_body"
           android:textSize="16sp"
           android:textColor="@color/text_primary"
           android:gravity="center"
           android:layout_marginBottom="12dp" />

       <!-- Sous-texte conséquence Inter 14sp opacity 0.7 (2 lignes) -->
       <TextView
           android:layout_width="wrap_content"
           android:layout_height="wrap_content"
           android:maxWidth="320dp"
           android:text="@string/c15_consequence"
           android:textSize="14sp"
           android:textColor="@color/text_secondary"
           android:alpha="0.7"
           android:gravity="center"
           android:layout_marginBottom="48dp" />

       <!-- Bouton primaire « OUVRIR LES PARAMÈTRES » Rajdhani 600 16sp 48dp -->
       <Button
           android:id="@+id/onboarding_btn_open_settings"
           android:layout_width="match_parent"
           android:layout_height="48dp"
           android:text="@string/c15_btn_open_settings"
           android:backgroundTint="@color/primary_blue"
           android:textColor="@color/text_primary"
           android:textSize="16sp"
           android:fontFamily="sans-serif"
           android:textAllCaps="true"
           android:layout_marginBottom="24dp" />

       <!-- Lien discret « Continuer sans (déconseillé) » Inter 13sp opacity 0.5 underline -->
       <TextView
           android:id="@+id/onboarding_link_skip"
           android:layout_width="wrap_content"
           android:layout_height="wrap_content"
           android:text="@string/c15_link_skip"
           android:textSize="13sp"
           android:textColor="@color/text_secondary"
           android:alpha="0.5"
           android:padding="12dp"
           android:clickable="true"
           android:focusable="true"
           android:contentDescription="@string/c15_link_skip_desc" />

       <!-- Écran transitoire « Vérification… » (caché initialement, affiché 1s au retour Settings) -->
       <LinearLayout
           android:id="@+id/onboarding_verifying_overlay"
           android:layout_width="match_parent"
           android:layout_height="match_parent"
           android:orientation="vertical"
           android:gravity="center"
           android:background="@color/bg_dark"
           android:visibility="gone">

           <ProgressBar
               android:layout_width="48dp"
               android:layout_height="48dp"
               android:indeterminateTint="@color/primary_blue"
               android:layout_marginBottom="16dp" />

           <TextView
               android:layout_width="wrap_content"
               android:layout_height="wrap_content"
               android:text="@string/c15_verifying"
               android:textSize="16sp"
               android:textColor="@color/text_primary" />
       </LinearLayout>

       <!-- Boutons fallback « Réessayer » + « J'ai vérifié manuellement » (cachés initialement,
            affichés si KillSwitchDetector retourne Inactive ou Unverifiable au retour Settings) -->
       <LinearLayout
           android:id="@+id/onboarding_fallback_actions"
           android:layout_width="match_parent"
           android:layout_height="wrap_content"
           android:orientation="vertical"
           android:visibility="gone"
           android:layout_marginTop="16dp">

           <Button
               android:id="@+id/onboarding_btn_retry"
               android:layout_width="match_parent"
               android:layout_height="48dp"
               android:text="@string/c15_btn_retry"
               android:backgroundTint="@color/primary_blue"
               android:textColor="@color/text_primary"
               android:layout_marginBottom="8dp" />

           <Button
               android:id="@+id/onboarding_btn_manual_verified"
               android:layout_width="match_parent"
               android:layout_height="48dp"
               android:text="@string/c15_btn_manual_verified"
               android:backgroundTint="@android:color/transparent"
               android:textColor="@color/text_secondary" />
       </LinearLayout>
   </LinearLayout>
   ```
   - **Pas de bouton « Continuer » direct** : pour avancer, soit l'utilisatrice ouvre Settings (chemin nominal), soit elle clique le lien discret « Continuer sans » (chemin alternatif explicite).

2. **Drawable `ic_warning_orange.xml` (vector 64dp)** — Quand le fichier est lu après cette story :
   ```xml
   <?xml version="1.0" encoding="utf-8"?>
   <!-- Story 11.6 — Vector warning orange pour C15 (cohérent ux l. 1289). -->
   <vector xmlns:android="http://schemas.android.com/apk/res/android"
       android:width="64dp"
       android:height="64dp"
       android:viewportWidth="24"
       android:viewportHeight="24">
       <path
           android:fillColor="#FB923C"
           android:pathData="M12,2L1,21h22L12,2zM12,16c-0.55,0 -1,-0.45 -1,-1s0.45,-1 1,-1 1,0.45 1,1 -0.45,1 -1,1zM13,13h-2L11,9h2v4z" />
   </vector>
   ```
   - Couleur `#FB923C` = `status_connecting` (orange charte plateformeliberte.fr, ligne `colors.xml`).

3. **Layout `dialog_skip_killswitch.xml` (bottom-sheet confirmation)** — Quand le fichier est lu :
   ```xml
   <?xml version="1.0" encoding="utf-8"?>
   <LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
       android:layout_width="match_parent"
       android:layout_height="wrap_content"
       android:orientation="vertical"
       android:padding="24dp"
       android:background="@color/bg_dark_alt">

       <TextView
           android:layout_width="wrap_content"
           android:layout_height="wrap_content"
           android:text="@string/c15_skip_dialog_title"
           android:textSize="18sp"
           android:textColor="@color/text_primary"
           android:fontFamily="sans-serif-medium"
           android:layout_marginBottom="12dp" />

       <TextView
           android:layout_width="match_parent"
           android:layout_height="wrap_content"
           android:text="@string/c15_skip_dialog_body"
           android:textSize="14sp"
           android:textColor="@color/text_primary"
           android:layout_marginBottom="24dp" />

       <LinearLayout
           android:layout_width="match_parent"
           android:layout_height="wrap_content"
           android:orientation="horizontal"
           android:gravity="end">

           <Button
               android:id="@+id/dialog_skip_cancel"
               android:layout_width="wrap_content"
               android:layout_height="48dp"
               android:text="@string/c15_skip_dialog_cancel"
               android:backgroundTint="@android:color/transparent"
               android:textColor="@color/primary_blue"
               android:layout_marginEnd="8dp" />

           <Button
               android:id="@+id/dialog_skip_confirm"
               android:layout_width="wrap_content"
               android:layout_height="48dp"
               android:text="@string/c15_skip_dialog_confirm"
               android:backgroundTint="@color/alert_red"
               android:textColor="@color/text_primary" />
       </LinearLayout>
   </LinearLayout>
   ```

4. **Enrichissement `OnboardingActivity` : KillSwitchDetector + flow C15** — Quand `OnboardingActivity.kt` est lu après cette story, le `wireScreenButtons` cas `3 →` est enrichi (et un `killSwitchDetector` instancié dans `onCreate`) :
   ```kotlin
   private lateinit var killSwitchDetector: KillSwitchDetector
   private var awaitingSettingsReturn = false

   override fun onCreate(savedInstanceState: Bundle?) {
       // ... code Story 11.5 préservé ...
       killSwitchDetector = KillSwitchDetector(applicationContext)
       // (les autres init Story 11.5 restent — vpnConsentLauncher, onBackPressedDispatcher)
   }

   override fun onResume() {
       super.onResume()
       // Story 11.6 — Si on revient de Settings (awaitingSettingsReturn = true),
       // re-vérifier le kill switch et router vers la suite du flow.
       if (currentScreen == 3 && awaitingSettingsReturn) {
           awaitingSettingsReturn = false
           triggerKillSwitchVerification()
       }
   }

   // Enrichissement du wireScreenButtons cas 3 :
   private fun wireScreen3Enriched(view: View) {
       view.findViewById<Button>(R.id.onboarding_btn_open_settings)
           .setOnClickListener {
               awaitingSettingsReturn = true
               openVpnSettings()  // existant Story 11.5
           }
       view.findViewById<TextView>(R.id.onboarding_link_skip)
           .setOnClickListener { showSkipConfirmationDialog() }
       view.findViewById<Button>(R.id.onboarding_btn_retry)
           .setOnClickListener {
               // Reset overlay + relancer Settings
               findViewById<View>(R.id.onboarding_fallback_actions).visibility = View.GONE
               awaitingSettingsReturn = true
               openVpnSettings()
           }
       view.findViewById<Button>(R.id.onboarding_btn_manual_verified)
           .setOnClickListener {
               // L'utilisatrice affirme avoir activé manuellement même si l'heuristique
               // retourne Unverifiable → on accepte sa parole et on complete.
               LeVoileLog.i(TAG, "Onboarding: manual_verified accepte par utilisatrice")
               completeOnboarding()
           }
   }

   private fun triggerKillSwitchVerification() {
       // Afficher overlay « Vérification… » 1s
       val overlay = findViewById<View>(R.id.onboarding_verifying_overlay)
       overlay.visibility = View.VISIBLE
       killSwitchDetector.refresh()
       Handler(Looper.getMainLooper()).postDelayed({
           overlay.visibility = View.GONE
           when (killSwitchDetector.status.value) {
               is KillSwitchStatus.Active -> {
                   LeVoileLog.i(TAG, "Onboarding: kill switch Active detecte — completion")
                   completeOnboarding()
               }
               else -> {
                   // Inactive ou Unverifiable → afficher fallback actions
                   findViewById<View>(R.id.onboarding_fallback_actions).visibility = View.VISIBLE
               }
           }
       }, 1000L)
   }

   private fun showSkipConfirmationDialog() {
       // Bottom-sheet ou Dialog — recommandation Dialog AlertDialog standard
       // (pas de dépendance Material BottomSheetDialog).
       val view = layoutInflater.inflate(R.layout.dialog_skip_killswitch, null)
       val dialog = AlertDialog.Builder(this, R.style.Theme_LeVoile_Dialog)
           .setView(view)
           .setCancelable(true)
           .create()
       view.findViewById<Button>(R.id.dialog_skip_cancel).setOnClickListener {
           dialog.dismiss()
       }
       view.findViewById<Button>(R.id.dialog_skip_confirm).setOnClickListener {
           dialog.dismiss()
           LeVoileLog.i(TAG, "Onboarding: skip kill switch confirme par utilisatrice")
           completeOnboarding()
           // Le bandeau C17 (Story 10.2) reste persistant tant que kill switch n'est pas
           // activé — pas d'action supplémentaire requise ici.
       }
       dialog.show()
   }
   ```
   - **`Theme_LeVoile_Dialog`** : style XML à ajouter dans `themes.xml` (ou réutiliser le thème app). Si dépendance circulaire, utiliser `AlertDialog.Builder(this)` sans theme custom et laisser le système Android.

5. **Strings i18n FR + parité** — Quand `strings.xml` est lu :
   ```xml
   <!-- Story 11.6 — Composant C15 (remplace placeholder Story 11.5) -->
   <string name="c15_warning_icon_desc">Avertissement protection incomplète</string>
   <string name="c15_title">Une dernière étape</string>
   <string name="c15_body">Activez le « VPN permanent » dans les paramètres Android pour bloquer toute connexion en dehors du tunnel.</string>
   <string name="c15_consequence">Sans cette étape, votre trafic peut s\'échapper si la connexion VPN tombe.</string>
   <string name="c15_btn_open_settings">Ouvrir les paramètres</string>
   <string name="c15_link_skip">Continuer sans (déconseillé)</string>
   <string name="c15_link_skip_desc">Continuer sans activer le kill switch — déconseillé</string>
   <string name="c15_verifying">Vérification…</string>
   <string name="c15_btn_retry">Réessayer</string>
   <string name="c15_btn_manual_verified">J\'ai vérifié manuellement</string>

   <!-- Bottom-sheet confirmation skip -->
   <string name="c15_skip_dialog_title">Continuer sans le kill switch ?</string>
   <string name="c15_skip_dialog_body">Vous pourrez l\'activer plus tard depuis les paramètres système Android.</string>
   <string name="c15_skip_dialog_cancel">Annuler</string>
   <string name="c15_skip_dialog_confirm">Continuer sans</string>
   ```
   - **Suppression** des strings `onboarding_screen3_title_placeholder` et `onboarding_screen3_body_placeholder` (livrées Story 11.5) — remplacées par `c15_*`.
   - Parité `values-fr/strings.xml` (cohérent test `NotificationHelperTest.STORY_9_6_KEYS` étendu).

6. **Tests JVM `OnboardingC15FlowTest.kt`** — Quand le test est exécuté, vert :
   ```kotlin
   class OnboardingC15FlowTest {
       @Test
       fun `strings c15 sont presentes parite FR`() {
           // Lecture des deux strings.xml — assert présence des clés c15_*.
           val xmlDefault = readStringsXml("src/main/res/values/strings.xml")
           val xmlFr = readStringsXml("src/main/res/values-fr/strings.xml")
           val expected = listOf(
               "c15_title", "c15_body", "c15_consequence",
               "c15_btn_open_settings", "c15_link_skip", "c15_verifying",
               "c15_skip_dialog_title", "c15_skip_dialog_body",
               "c15_skip_dialog_cancel", "c15_skip_dialog_confirm",
           )
           expected.forEach { key ->
               assertTrue("Key '$key' absente values/", xmlDefault.contains("name=\"$key\""))
               assertTrue("Key '$key' absente values-fr/", xmlFr.contains("name=\"$key\""))
           }
       }

       @Test
       fun `placeholder Story 11_5 supprime des strings`() {
           val xmlDefault = readStringsXml("src/main/res/values/strings.xml")
           assertFalse(
               "onboarding_screen3_title_placeholder doit etre supprime (replace par c15_title)",
               xmlDefault.contains("onboarding_screen3_title_placeholder")
           )
       }

       @Test
       fun `layout onboarding_screen_3 reference c15 strings`() {
           val xml = File("src/main/res/layout/onboarding_screen_3.xml").readText()
           assertTrue(xml.contains("@string/c15_title"))
           assertTrue(xml.contains("@string/c15_link_skip"))
           assertTrue(xml.contains("@drawable/ic_warning_orange"))
       }

       private fun readStringsXml(path: String): String {
           val candidates = listOf(File(path), File("app/$path"))
           return candidates.firstOrNull { it.exists() }?.readText()
               ?: throw AssertionError("$path introuvable")
       }
   }
   ```

7. **Build sanity + smoke test** — Quand `cd android && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` est exécuté, vert. Smoke test :
   - **Premier lancement** : `OnboardingActivity` écran 1 → 2 → 3.
   - Écran 3 affiche : icône warning orange 64dp, titre « Une dernière étape », texte body, sous-texte conséquence, bouton « OUVRIR LES PARAMÈTRES », lien « Continuer sans (déconseillé) ».
   - Tap « OUVRIR LES PARAMÈTRES » → ouvre Réglages > VPN. Activer manuellement « Always-on VPN » + « Block connections without VPN ». Retour back → overlay « Vérification… » 1s → si détecté → completion + `MainActivity` se lance.
   - Si non détecté (heuristique Unverifiable sur certaines ROMs) → boutons « Réessayer » + « J'ai vérifié manuellement » apparaissent.
   - Tap « J'ai vérifié manuellement » → completion forcée.
   - **Test alternatif** : tap lien « Continuer sans (déconseillé) » → dialog de confirmation. « Annuler » → retour écran 3. « Continuer sans » → completion + MainActivity. **Bandeau C17 reste visible** dans MainActivity (kill switch toujours inactif).

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état Stories amont** (AC: tous)
  - [x] Confirmer Story 11.5 livrée (OnboardingActivity + 3 layouts placeholder).
  - [x] Confirmer Story 10.1 livrée (KillSwitchDetector).
  - [x] Confirmer Story 10.2 livrée (bandeau C17).

- [x] **Task 2 : Créer drawable `ic_warning_orange.xml`** (AC: #2)
  - [x] Vector 64dp triangle warning orange.
  - [x] **Vérifier** : `:app:lint` ne signale pas de warning vector.

- [x] **Task 3 : Modifier `onboarding_screen_3.xml`** (AC: #1)
  - [x] Remplacer le placeholder Story 11.5 par le markup C15 complet.
  - [x] **CRITICAL** : conserver `R.id.onboarding_btn_open_settings` (utilisé par 11.5 + enrichi 11.6) + ajouter les nouveaux IDs (`onboarding_link_skip`, `onboarding_verifying_overlay`, etc.).

- [x] **Task 4 : Créer `dialog_skip_killswitch.xml`** (AC: #3)

- [x] **Task 5 : Enrichir `OnboardingActivity`** (AC: #4)
  - [x] Ajouter `killSwitchDetector` instanciation dans `onCreate`.
  - [x] Ajouter override `onResume` pour le check post-Settings.
  - [x] Refactor `wireScreenButtons` cas `3 →` pour appeler `wireScreen3Enriched`.
  - [x] Ajouter `triggerKillSwitchVerification` + `showSkipConfirmationDialog`.

- [x] **Task 6 : Mettre à jour strings i18n** (AC: #5)
  - [x] Supprimer `onboarding_screen3_title_placeholder` + `onboarding_screen3_body_placeholder`.
  - [x] Ajouter clés `c15_*` selon AC #5.
  - [x] Parité `values-fr/strings.xml`.

- [x] **Task 7 : Créer `OnboardingC15FlowTest.kt`** (AC: #6)

- [x] **Task 8 : Build sanity + smoke test** (AC: #7)
  - [x] `./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` — vert.
  - [x] Smoke test sur émulateur : écran 3 visuel correct, ouverture Settings + retour, dialog skip, completion.
  - [x] **Vérifier C17** : skip → MainActivity → bandeau visible.

- [x] **Task 9 : Mettre à jour la story et sprint-status**

## Dev Notes

### Pattern principal — Enrichissement progressif Story 11.5 → 11.6

Story 11.5 livre l'**Activity** + le **flow** + le **placeholder écran 3**.
Story 11.6 livre le **composant C15** + l'**ergonomie** (warning, lien skip, post-Settings detection).

Cette séparation garantit :
1. 11.5 implémentable en isolation (test + smoke test partiels).
2. 11.6 ne touche pas à la structure 3-écrans (refactor risqué).
3. Backportage facile si C15 doit évoluer Phase 2 (touche uniquement les 6 fichiers du périmètre).

### Pourquoi AlertDialog plutôt que BottomSheetDialog Material

- **Pas de dépendance Material** ajoutée (cohérent NFR-AND-3 et discipline anti-bloat).
- **AlertDialog** est dans `android.app.*` (built-in), zéro coût APK.
- **UX différence** : BottomSheetDialog slide depuis le bas, AlertDialog est centré modal. Pour une confirmation binaire courte (« Annuler / Continuer sans »), un dialog centré est plus standard et n'introduit pas d'animation supplémentaire.
- **Si le designer insiste sur bottom-sheet** : refactor possible Phase 2 avec dépendance Material `1.13.x` (~120 KB).

### Gestion de l'heuristique « Unverifiable »

Story 10.1 documente que `Settings.Global.always_on_vpn_app` peut être inaccessible sur certaines ROMs (heuristique fragile, Android peut casser dans les futures versions). Story 11.6 gère ça gracieusement :
- **Inactive** : on sait que c'est inactif → boutons retry + manual_verified visibles.
- **Unverifiable** : on **ne sait pas** → mêmes boutons (l'utilisatrice peut affirmer avoir activé manuellement, on lui fait confiance — pattern « trust but warn »).
- **Active** : tout va bien → completion automatique.

### Coordination Story 10.2 (bandeau C17)

Si l'utilisatrice skip via le lien « Continuer sans (déconseillé) », le bandeau C17 (livré Story 10.2) reste **automatiquement** visible dans MainActivity — il dépend du `KillSwitchDetector.status` qui retournera Inactive ou Unverifiable. Aucune logique ad-hoc requise dans 11.6 pour gérer le bandeau.

### Coordination Story 11.7 (notification enrichie)

Si l'utilisatrice skip ET connecte → la notification (Story 11.7) affichera « ⚠️ Kill switch inactif · Activer ». Le tap notification ouvrira MainActivity → l'utilisatrice voit le bandeau C17 + peut taper dessus pour ré-ouvrir le flow C15 (boucle de rattrapage Story 10.2 actuelle).

### Source tree components à toucher

- **Modifiés** :
  - `android/app/src/main/kotlin/.../onboarding/OnboardingActivity.kt`
  - `android/app/src/main/res/layout/onboarding_screen_3.xml`
  - `android/app/src/main/res/values/strings.xml`
  - `android/app/src/main/res/values-fr/strings.xml`
- **Nouveaux** :
  - `android/app/src/main/res/layout/dialog_skip_killswitch.xml`
  - `android/app/src/main/res/drawable/ic_warning_orange.xml`
  - `android/app/src/test/kotlin/.../onboarding/OnboardingC15FlowTest.kt`

### References

- [architecture.md l. 1072-1078](_bmad-output/planning-artifacts/architecture.md) — Pattern kill switch UX (re-check au retour Settings).
- [architecture.md l. 2455-2461](_bmad-output/planning-artifacts/architecture.md) — EBR-02 (branche fallback Story 10.2 vers C15).
- [epics.md l. 1950-1979](_bmad-output/planning-artifacts/epics.md) — Story 11.6 BDD complet.
- [ux-design-specification.md l. 1285-1303](_bmad-output/planning-artifacts/ux-design-specification.md) — C15 specs visuelles + interactions.
- Story 10.1 (livrée) : `KillSwitchDetector` + heuristique.
- Story 10.2 (livrée) : bandeau C17 — coexiste avec C15.
- Story 10.5 (livrée) : `LeVoileLog` à utiliser pour les logs.
- Story 11.5 (à venir) : OnboardingActivity + écran 3 placeholder à remplacer.
- Story 11.7 (à venir) : notification enrichie consomme `KillSwitchDetector` cohérent C17.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Completion Notes List

- **Livraison conjointe Story 11.5 + 11.6** : le layout `onboarding_screen_3.xml` est livré directement en version C15 complète (icône warning 64dp + titre Bebas Neue + lien skip + overlay vérification + boutons fallback). L'`OnboardingActivity` inclut `wireScreen3Enriched`, `triggerKillSwitchVerification`, `showSkipConfirmationDialog`. Cohérent stratégie d'« enrichissement progressif » Dev Notes 11.6 — ici condensé en une seule passe pour éviter la régression écran 3 minimaliste → enrichi.
- **`AlertDialog` retenu plutôt que `BottomSheetDialog` Material** (anti-pattern Material dépendance — cohérent NFR-AND-3 et Dev Notes 11.6).
- **`FrameLayout` racine** sur `onboarding_screen_3.xml` : permet l'overlay « Vérification… » plein écran au-dessus du contenu (pattern standard Android).
- **Drawable `ic_warning_orange.xml`** : vector 64dp, fillColor `#FB923C` (status_connecting charte plateformeliberte.fr).
- **Skip flow** : skip confirmé → `completeOnboarding()` → MainActivity → bandeau C17 (Story 10.2) reste persistant automatiquement (lit `KillSwitchDetector.status` qui retournera Inactive). Pas de logique ad-hoc nécessaire.
- **`onResume` re-vérification** : si `awaitingSettingsReturn = true` ET `currentScreen == 3`, déclenche `triggerKillSwitchVerification` → overlay 1s → completion (Active) ou fallback boutons (Inactive/Unverifiable).
- **Build/test verification 2026-05-03 (JDK 17)** : BUILD SUCCESSFUL, 133 tests verts (incluant `OnboardingC15FlowTest` 3 tests), 0 lint error.

### File List

- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/onboarding/OnboardingActivity.kt` (livré Story 11.5 directement avec enrichissement 11.6 inclus)
- `android/app/src/main/res/layout/onboarding_screen_3.xml` (livré Story 11.5 directement en version C15 complète)
- `android/app/src/main/res/layout/dialog_skip_killswitch.xml` (NOUVEAU)
- `android/app/src/main/res/drawable/ic_warning_orange.xml` (NOUVEAU)
- `android/app/src/main/res/values/strings.xml` (MODIFIÉ — strings c15_*)
- `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — parité FR)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/onboarding/OnboardingC15FlowTest.kt` (NOUVEAU)

### Change Log

| Date | Description |
|---|---|
| 2026-05-03 | Story 11.6 livrée (composant C15 + dialog skip + KillSwitchDetector re-check post-Settings). |
| 2026-05-03 | Code-review Epic 11 : OnboardingC15FlowTest enrichi (L2 — `dialog_skip_killswitch contient boutons cancel et confirm` + invariant `c15 strings sont presentes` anti-régression placeholder, H4). |
