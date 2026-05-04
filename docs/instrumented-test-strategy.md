# Le Voile · Stratégie tests instrumentés Android

Story 12.6 — runbook pour les développeurs futurs travaillant sur la matrice instrumentée Espresso/UiAutomator/AndroidX Test API 29/33/34.

## Architecture

| Outil | Rôle | Quand l'utiliser |
|---|---|---|
| **Espresso** | Tests UI in-process (Activity, Views, WebView via `Espresso.onWebView()`) | Interactions avec MainActivity / OnboardingActivity / WebView (boutons, formulaires, navigations internes). |
| **UiAutomator** | Tests cross-process (system UI, notifications shade, settings, dialogs system VPN) | `device.openNotification()`, validation présence `com.android.vpndialogs.ConfirmDialog`, ouverture Settings VPN. |
| **AndroidX Test rules** | `ActivityScenarioRule`, `ServiceTestRule`, `IdlingResource` | Lifecycle Activity, synchronization async (WorkManager, coroutines). |
| **Espresso Intents** | `Intents.intended(IntentMatchers.hasAction(...))` | Vérifier qu'un Intent ACTION_VIEW / ACTION_VPN_SETTINGS a été lancé. |
| **MockWebServer** | Mock HTTP local (`api.github.com`, registre relais externe) | UpdateNotificationFlowTest, FailoverRelayTest. |
| **WorkManagerTestInitHelper** | Force l'exécution synchrone d'un PeriodicWorkRequest | UpdateNotificationFlowTest (forcer le worker au lieu d'attendre 24h). |

## Matrice API 29 / 33 / 34

| API | Version Android | Inflexion testée |
|---|---|---|
| 29 | Android 10 | minSdk Le Voile, premiers VpnService stables, `always_on_vpn_app` Settings.Global, pas de POST_NOTIFICATIONS runtime permission. |
| 33 | Android 13 | `POST_NOTIFICATIONS` permission runtime obligatoire, début deprecation v1 signing (irrelevant pour nous). |
| 34 | Android 14 | `FOREGROUND_SERVICE_SPECIAL_USE` + `<property name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE" value="vpn">`. |

Couverture justification : 29 = baseline, 33 = milieu (POST_NOTIFICATIONS), 34 = max (FOREGROUND_SERVICE_SPECIAL_USE). Phase 2 si besoin : ajouter API 31 (Material You) ou API 30 (storage scoped).

## Comment ajouter un nouveau scénario

1. **Identifier le besoin** : nouveau flow utilisateur ou régression à protéger ? Si oui, ajouter au matrix instrumenté ; sinon, préférer un test JVM-only (plus rapide, exécuté sur chaque PR).
2. **Créer le fichier** sous `android/app/src/androidTest/kotlin/fr/plateformeliberte/levoile/scenarios/<NomTest>.kt` (ou `conflict/`, `update/`, `security/` selon domaine).
3. **Annotations** : `@RunWith(AndroidJUnit4::class)` + `@Test` + `@get:Rule val leVoileRule = LeVoileTestRule()` (ou `skipOnboarding=false` pour tester l'onboarding).
4. **Pattern recommandé** :
   ```kotlin
   @Test
   fun `mon_scenario`() {
       grantVpnConsent()    // si VPN flow (helper testutils Phase 2)
       ActivityScenario.launch(MainActivity::class.java).use { scenario ->
           Espresso.onWebView()
               .withElement(DriverAtoms.findElement(Locator.ID, "btn-xxx"))
               .perform(DriverAtoms.webClick())
           awaitState(VpnState.CONNECTED, 10.seconds)   // IdlingResource
           // assertions
       }
   }
   ```
5. **Linker au gating** : ajouter le nom du scenario dans `InstrumentedTestMatrixTest.kt` (anti-régression — refuse qu'un dev retire un scenario par accident).
6. **Documenter** dans cette page si le scenario introduit un nouvel outil (ex. première utilisation de MockWebServer).

## Comment debug un échec CI

1. **Logcat artifact** : le job `instrumented-tests` upload `logcat-api-{29,33,34}` automatiquement sur failure. Télécharger depuis le run GitHub Actions.
2. **Test report HTML** : artifact `instrumented-test-report-api-{29,33,34}` contient le rapport AGP avec stack traces + screenshots si capturés.
3. **Reproduire localement** :
   ```bash
   cd android
   bash scripts/build-aar.sh
   bash scripts/sync-frontend.sh
   ./gradlew :app:connectedApkDirectDebugAndroidTest --tests "fr.plateformeliberte.levoile.scenarios.OnboardingFlowTest"
   ```
   Pré-requis : émulateur API X démarré (Android Studio → Device Manager). Pour tester sur 3 API levels en local, créer 3 AVD séparés.

## Limites connues

* **WebView interactions parfois flaky** sur émulateur API 29 (WebView Chromium pré-installé est ancien). Mitigation : `Espresso.onWebView().forceJavascriptEnabled()` au début du test, ou skip via `EmulatorAssumptions.assumeApiAtLeast(33)` pour les flows WebView complexes.
* **`adb shell appops` pour pre-grant VpnService consent** : nécessite WRITE_SECURE_SETTINGS permission de la part du test runner. La matrice CI peut le faire via un step setup. Pour MVP Story 12.6, certains tests qui exigent un consent VPN restent **structurels** (skip ou marqués TODO) — le smoke-test mainteneur sur device physique compense.
* **Pas de Google Play Services** sur émulateur `default` target → tests qui dépendent de Google Auth, GCM, etc. doivent être skippés. Notre app n'a aucune dep Google → no-op.
* **MockWebServer URL injection** : le `UpdateCheckWorker` Story 12.5 hardcode `https://api.github.com/...`. Pour mocker, refactor Phase 2 :
  ```kotlin
  buildConfigField("String", "GITHUB_API_URL", '"https://api.github.com"')
  // androidTest override via gradle.kts androidTest.buildConfigField("String", "GITHUB_API_URL", "\"http://localhost:$port\"")
  ```
  En attendant, `UpdateNotificationFlowTest` MVP teste directement `UpdateNotificationHelper.post()` sans passer par le worker.
* **Screenshot golden tests** : non implémentés (Paparazzi ou Compose Screenshot). Phase 2 si pertinent — flaky entre API levels (system bars, fonts).

## Workflow CI exécution

* **`release-android.yml` job `instrumented-tests`** : matrice 29/33/34 sur push tag `v*` (gating release). Durée estimée ~25-35 min/job en parallèle, 30 min total.
* **`android-instrumented.yml`** : matrice identique sur push `main` uniquement (signal régulier, pas par PR).
* **`android-audit.yml`** (Story 10.4 + 12.2) : tests JVM uniquement (lint, unit-tests, permission-audit, proguard-syntax) sur chaque PR. Coût ~10-15 min × 4 jobs en parallèle.

## Références

* [Story 12.6](../_bmad-output/implementation-artifacts/12-6-tests-instrumentes-espresso-androidx-test-sur-emulateur-api-29-33-34.md) — story file complète.
* [reactivecircus/android-emulator-runner](https://github.com/ReactiveCircus/android-emulator-runner) — action CI émulateur.
* [Espresso WebView testing](https://developer.android.com/training/testing/espresso/web).
* [UiAutomator notifications](https://developer.android.com/training/testing/ui-automator).
* [WorkManager testing](https://developer.android.com/topic/libraries/architecture/workmanager/how-to/integration-testing).
* [`docs/key-management-android.md`](key-management-android.md) — Story 12.3 (SignatureValidationTest fingerprint).
* [`docs/reproducible-build-android.md`](reproducible-build-android.md) — Story 12.4 (orthogonale aux tests instrumentés).
