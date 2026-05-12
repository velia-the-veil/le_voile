plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "fr.plateformeliberte.levoile"
    compileSdk = 34

    // Story 12.5 — productFlavors `apkDirect` (default) et `fdroid`.
    //
    // Permet de différencier le comportement à la build time pour le worker
    // auto-update :
    //   - `apkDirect` (default) → AUTO_UPDATE_ENABLED = true. Pipeline GitHub
    //     release-android.yml invoque `assembleApkDirectRelease`.
    //   - `fdroid` → AUTO_UPDATE_ENABLED = false. F-Droid build server invoque
    //     `assembleFdroidRelease` (cf. metadata/fr.plateformeliberte.levoile.yml).
    //     Cohérent epics.md l. 2188-2192 + ADR-11.
    flavorDimensions += "distributionChannel"

    productFlavors {
        create("apkDirect") {
            dimension = "distributionChannel"
            buildConfigField("Boolean", "AUTO_UPDATE_ENABLED", "true")
        }
        create("fdroid") {
            dimension = "distributionChannel"
            buildConfigField("Boolean", "AUTO_UPDATE_ENABLED", "false")
        }
    }

    defaultConfig {
        applicationId = "fr.plateformeliberte.levoile"
        minSdk = 29
        targetSdk = 34
        versionCode = 1
        versionName = "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // Story 9.7 — NFR-AND-3 < 25 MB : le .aar gomobile (Story 9.7
        // post-extension surface tunnel) embarque les natives libs pour 4 ABIs
        // (x86, x86_64, armeabi-v7a, arm64-v8a). Sans filtre, l'APK release
        // pèse ~47 MB. Limiter aux 2 ABIs ARM (≈99% du parc Android 2026)
        // descend à ~12-15 MB, sous le seuil NFR.
        // x86/x86_64 retirés : marché négligeable (Chromebook quelques %, déjà
        // couverts par émulation native ARM via Houdini/native bridge).
        ndk {
            abiFilters.add("arm64-v8a")
            abiFilters.add("armeabi-v7a")
            // Story 12.6 — tests instrumentés CI : émulateur GitHub Actions
            // tourne en x86_64 (accélération KVM). Activer x86_64 uniquement
            // via -PciEmulator=true pour ne pas alourdir l'APK release
            // (NFR-AND-3 < 25 MB).
            if (project.hasProperty("ciEmulator")) {
                abiFilters.add("x86_64")
            }
        }
    }

    buildFeatures {
        // Génération de la classe BuildConfig (BuildConfig.APPLICATION_ID, BuildConfig.DEBUG, ...).
        // Désactivé par défaut depuis AGP 8.0. Story 10.1 (KillSwitchDetector) compare
        // Settings.Global.always_on_vpn_app à BuildConfig.APPLICATION_ID — sans ça, ne compile pas.
        buildConfig = true
    }

    // NFR-AND-6 / ADR-11 — F-Droid impose un build APK reproductible (hash SHA256 stable
    // entre 2 builds successifs depuis le même tag git). AGP injecte par défaut un blob signé
    // contenant les empreintes des dépendances dans l'APK, ce qui casse la reproductibilité.
    // Désactivé pour Story 12.4 (vérification reproductibilité APK CI).
    dependenciesInfo {
        includeInApk = false
        includeInBundle = false
    }

    // Story 12.3 — signingConfigs.release.
    // SECURITY : credentials lus EXCLUSIVEMENT depuis des variables d'environnement.
    // Aucun string literal de password ici. Voir docs/key-management-android.md
    // pour la procédure de provisionnement (génération keystore master, encodage base64,
    // ajout aux secrets GitHub Actions).
    signingConfigs {
        create("release") {
            val keystorePath = System.getenv("LEVOILE_KEYSTORE_PATH")
            val keystorePassword = System.getenv("LEVOILE_KEYSTORE_PASSWORD")
            val alias = System.getenv("LEVOILE_KEY_ALIAS")
            val keyPasswordEnv = System.getenv("LEVOILE_KEY_PASSWORD")

            if (keystorePath != null && keystorePassword != null && alias != null && keyPasswordEnv != null) {
                storeFile = file(keystorePath)
                storePassword = keystorePassword
                keyAlias = alias
                keyPassword = keyPasswordEnv
                // APK Signature Scheme v2 + v3 (key rotation).
                // v1 (JAR signing) désactivé — minSdk 29 (Android 10+) accepte v2 sans v1.
                // v4 désactivé — non requis (streaming install Android 11+ Play Asset Delivery).
                // Cf. https://source.android.com/docs/security/features/apksigning/v2
                enableV1Signing = false
                enableV2Signing = true
                enableV3Signing = true
                enableV4Signing = false
            }
            // Si les env vars sont absentes : storeFile reste null, le buildType.release
            // tombera en fallback debug (cf. ci-dessous) — pratique pour les builds locaux
            // des devs sans la master key.
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            // Story 12.3 — signingConfig conditionnel.
            //  - Si LEVOILE_KEYSTORE_PATH est défini → release signing (master key Le Voile).
            //  - Sinon → fallback debug + suffix `-unsigned-LOCAL-DEV` pour qu'aucun
            //    utilisateur ne confonde cet APK avec une release valide.
            //
            // Le fallback permet aux devs de faire `./gradlew :app:assembleRelease` localement
            // pour tester R8/ProGuard sans avoir la master key. Le job CI `sign-apk`
            // (release-android.yml) provisionne les env vars depuis les secrets GitHub Actions.
            if (System.getenv("LEVOILE_KEYSTORE_PATH") != null) {
                signingConfig = signingConfigs.getByName("release")
                // Pas de versionNameSuffix — c'est l'APK release officiel.
            } else {
                signingConfig = signingConfigs.getByName("debug")
                versionNameSuffix = "-unsigned-LOCAL-DEV"
            }
        }
        debug {
            isMinifyEnabled = false
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }

    // Story 9.4 : autorise les tests JVM-only à appeler des APIs Android stub
    // (android.util.Log, etc.) sans planter sur "Method ... not mocked".
    // Les méthodes android.* renvoient leurs valeurs par défaut (0, false, null).
    // Choix conscient : évite Robolectric (heavy dep) tant qu'aucun test
    // n'instancie un vrai Context.
    //
    // Story 12.6 — `animationsDisabled = true` désactive les animations Android
    // sur l'émulateur pour les tests instrumentés. Ceinture+bretelles avec le
    // flag `disable-animations: true` de l'action emulator-runner — au cas où
    // un dev exécuterait la matrice localement sans le wrapper CI.
    testOptions {
        unitTests.isReturnDefaultValues = true
        animationsDisabled = true
    }
}

// NFR-AND-6 / ADR-11 / Story 12.4 — Build APK reproductible.
//
// `dependenciesInfo { ... }` désactivé plus haut (block défini dans `android { }`).
// Ce hook applique l'antipattern #1 documenté reproducible-builds.org : tous les
// Zip/Jar/Aar tasks doivent produire des archives déterministes (timestamps fixes,
// file order stable). Sans ça, deux builds successifs depuis le même tag git
// produisent des SHA256 différents → F-Droid refuse le badge « Reproducible Build ».
//
// Le `tasks.withType<...>()` en racine du fichier (pas dans `android { }`) garantit
// l'application à TOUTES les archives Gradle (signedAPK, AAB, AAR transitif, etc.).
tasks.withType<AbstractArchiveTask>().configureEach {
    isPreserveFileTimestamps = false
    isReproducibleFileOrder = true
}

dependencies {
    // Le .aar du noyau Go partagé est consommé directement ici (Story 9.2).
    // Produit par scripts/build-aar.{sh,ps1} → app/libs/levoile-core.aar
    // (gitignoré, regen sur modification des shims android/shims/* ou
    // packages racine internal/{crypto,registry,leakcheck,tunnel}).
    //
    // Pourquoi pas via :levoile-core ? AGP interdit qu'un module library
    // bundle un .aar local (l'AAR sortante serait cassée). Le .aar atterrit
    // donc dans :app/libs/. Le module :levoile-core garde un rôle de
    // placeholder pour les futurs wrappers Kotlin idiomatiques (Story 9.7
    // GoCoreAdapter).
    implementation(files("libs/levoile-core.aar"))

    // Module placeholder hôte des futurs wrappers Kotlin du noyau Go
    // (Story 9.7 — GoCoreAdapter etc.). Référence présente dès maintenant
    // pour figer la frontière de dépendance (cohérent ADR-09).
    implementation(project(":levoile-core"))

    // AndroidX minimal — pas de Material/Compose dans cette story
    // (livrés progressivement dans Epic 11 selon besoin).
    // Versions dans gradle/libs.versions.toml.
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.appcompat)

    // Story 9.3 : WebViewAssetLoader (host virtuel HTTPS-like
    // https://appassets.androidplatform.net/ pour servir assets locaux —
    // plus sûr que file://, cohérent architecture.md l. 263). Scoped à :app
    // (le module :levoile-core n'a pas de WebView).
    implementation(libs.androidx.webkit)

    // Story 9.7 : Coroutines pour GoCoreAdapter (suspend fun + Dispatchers.IO
    // wrapping des appels JNI gomobile bloquants — architecture.md l. 1213-1214).
    implementation(libs.kotlinx.coroutines.android)

    // Story 10.1 : KillSwitchDetector expose un LiveData<KillSwitchStatus>.
    // Tirée transitivement par appcompat (lifecycle-livedata-core), explicitée
    // ici pour rendre l'usage volontaire et accéder à l'extension KTX (cohérent
    // version catalog — cf. gradle/libs.versions.toml).
    implementation(libs.androidx.lifecycle.livedata.ktx)

    // Story 12.5 : WorkManager pour le worker UpdateCheckWorker (check 24h
    // GitHub releases, canal apkDirect uniquement). Court-circuité au runtime
    // pour le flavor `fdroid` via BuildConfig.AUTO_UPDATE_ENABLED.
    implementation(libs.androidx.work.runtime.ktx)

    // Tests unitaires JVM-only (Story 9.2 LeVoileCoreSmokeTest, Story 9.7
    // GoCoreAdapterContractTest + GoBackedPacketRelayTest, Story 10.1
    // KillSwitchDetectorTest). N'entrent pas dans l'APK release.
    testImplementation(libs.junit)
    // Story 10.1 : InstantTaskExecutorRule pour forcer LiveData.postValue
    // synchrone côté tests JVM-only (sans Robolectric).
    testImplementation(libs.androidx.arch.core.testing)
    // Story 10.2 + 10.3 : Mockito pour mocker android.content.Context dans les
    // tests JVM-only (LeVoileBridgeKillSwitchTest, VpnConflictDetectorTest).
    // MockContext de android.jar throw "Stub!" au constructor — Mockito est
    // la voie standard Android pour ce cas.
    testImplementation(libs.mockito.core)
    // Story 11.8 : org.json:json pour fournir l'implémentation réelle de
    // JSONObject côté tests JVM (le stub android.jar retourne null/0).
    // Cohérent ConfigStoreTest + ConfigMigrationTest. Test scope only —
    // n'entre pas dans l'APK release (Android utilise sa propre org.json).
    testImplementation(libs.org.json)
    // Story 12.1 : snakeyaml pour parser metadata/fr.plateformeliberte.levoile.yml
    // dans FdroidMetadataTest. Scope testImplementation strict — anti-fuite NFR-AND-3
    // garantie par AuditCITest (~250 KB jamais embarqués dans l'APK release).
    testImplementation(libs.snakeyaml)
    // Story 12.5 : work-testing fournit WorkManagerTestInitHelper pour Story 12.6
    // instrumented runtime test. Scope testImplementation pour permettre aux
    // SemVerCompareTest / UpdateCheckerTest de s'exécuter en JVM-only sans
    // contraintes WorkManager runtime.
    testImplementation(libs.androidx.work.testing)

    // Tests instrumentés — alignés avec testInstrumentationRunner ci-dessus.
    // Permet à connectedAndroidTest de s'exécuter dès la première story qui livrera
    // un test (9.4 LeVoileVpnService, 11.x UI, 12.6 matrice Espresso). Ces deps
    // n'entrent PAS dans l'APK release (scope androidTestImplementation).
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.androidx.test.espresso.core)

    // Story 12.6 — matrice instrumentée API 29/33/34 :
    //   - androidx.test:rules    → ActivityScenarioRule, ServiceTestRule.
    //   - espresso-intents       → Intents.intended(...) pour vérifier qu'un Intent
    //                              ACTION_VIEW / ACTION_VPN_SETTINGS a été lancé.
    //   - espresso-web           → Espresso.onWebView() + DriverAtoms pour interactions
    //                              avec le WebView Le Voile (Story 11.x JS bridge).
    //   - uiautomator            → ouverture du shade notif + interaction system UI
    //                              (com.android.vpndialogs consent, Settings).
    //
    // NB : `okhttp-mockwebserver` retire post-Code Review 2026-05-03 — l'impl
    // runtime UpdateNotificationFlowTest teste UpdateNotificationHelper.post()
    // directement sans passer par le worker, donc pas de mock HTTP necessaire.
    // A reintroduire si Phase 2 livre l'injection BuildConfigField GITHUB_API_URL.
    androidTestImplementation(libs.androidx.test.rules)
    androidTestImplementation(libs.androidx.test.espresso.intents)
    androidTestImplementation(libs.androidx.test.espresso.web)
    androidTestImplementation(libs.androidx.test.uiautomator)
    androidTestImplementation(libs.androidx.work.testing)
}
