plugins {
    alias(libs.plugins.android.library)
}

android {
    namespace = "fr.plateformeliberte.levoile.core"
    compileSdk = 34

    defaultConfig {
        minSdk = 29
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    // Le .aar produit par gomobile bind (Story 9.2) est consommé directement
    // par :app via implementation(files("libs/levoile-core.aar")) — voir
    // android/app/build.gradle.kts.
    //
    // Pourquoi pas ici ? AGP refuse qu'un module `android.library` bundle un
    // .aar local en dépendance fichier (l'AAR de sortie serait cassée car les
    // classes du .aar embarqué ne seraient pas re-packagées). Ce module
    // :levoile-core garde donc un rôle de placeholder : il existera pour
    // héberger les éventuels wrappers Kotlin idiomatiques de Story 9.7
    // (GoCoreAdapter etc.) qui consommeront le .aar via :app, pas ici.
    //
    // Architecturalement, la frontière logique reste « code Go partagé →
    // .aar → adapter Kotlin » mais le packaging Gradle place le .aar dans
    // :app pour respecter les contraintes AGP.
}
