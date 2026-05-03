// Top-level build script.
//
// Versions des plugins centralisées dans gradle/libs.versions.toml (Version Catalog).
// `apply false` : déclare les plugins pour les sous-modules sans les appliquer ici.
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
}
