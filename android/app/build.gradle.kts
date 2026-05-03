plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "fr.plateformeliberte.levoile"
    compileSdk = 34

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

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            // Story 12.3 livrera la signingConfig release réelle (master key Ed25519, v2/v3).
            // En attendant, on signe en debug et on suffixe explicitement le versionName
            // pour que personne ne confonde cet APK avec une release valide.
            signingConfig = signingConfigs.getByName("debug")
            versionNameSuffix = "-unsigned"
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
    // n'instancie un vrai Context. Story 12.6 (tests instrumentés Espresso)
    // couvrira le runtime réel.
    testOptions {
        unitTests.isReturnDefaultValues = true
    }
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

    // Tests instrumentés — alignés avec testInstrumentationRunner ci-dessus.
    // Permet à connectedAndroidTest de s'exécuter dès la première story qui livrera
    // un test (9.4 LeVoileVpnService, 11.x UI, 12.6 matrice Espresso). Ces deps
    // n'entrent PAS dans l'APK release (scope androidTestImplementation).
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.androidx.test.espresso.core)
}
