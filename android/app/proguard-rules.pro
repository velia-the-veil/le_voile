# Le Voile — Rules ProGuard spécifiques

# Préserver les classes générées par gomobile bind (.aar livré Story 9.2)
-keep class fr.plateformeliberte.levoile.core.** { *; }
-keep class go.** { *; }
-keepclassmembers class * {
    @go.Seq.Proxy <methods>;
}

# Préserver les méthodes natives JNI
-keepclasseswithmembernames class * {
    native <methods>;
}

# Préserver les annotations utilisées par gomobile
-keepattributes *Annotation*

# NFR-AND-9 : strip Log.d et Log.v en release (Log.w/.e/.i restent)
-assumenosideeffects class android.util.Log {
    public static int d(...);
    public static int v(...);
}

# TODO Story 9.7 : ajouter rules spécifiques aux callbacks Go→Kotlin
# (interfaces enregistrées via GoCoreAdapter.setCallbacks)

# TODO Story 11.x : ajouter rules pour les classes annotées @JavascriptInterface
# quand le JS Bridge sera livré
