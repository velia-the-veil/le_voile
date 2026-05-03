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

# NFR-AND-9 : strip Log.d / Log.v / Log.i en release (Log.w / Log.e restent
# visibles cohérent NFR-AND-9 prd.md l. 705 « release : WARN+ uniquement »).
# Story 9.1 a livré la version d / v ; Story 10.5 étend à i.
-assumenosideeffects class android.util.Log {
    public static int d(...);
    public static int v(...);
    public static int i(...);
}

# Story 10.5 : strip LeVoileLog.i (notre wrapper) en release. Le
# `if (BuildConfig.DEBUG)` interne du wrapper le rend déjà no-op en release,
# mais cette rule permet à ProGuard d'éliminer entièrement les call-sites du
# bytecode (économie taille APK + élimination des chaînes constantes du .dex).
# Les méthodes `w` et `e` du wrapper restent intactes — visibles en release.
-assumenosideeffects class fr.plateformeliberte.levoile.log.LeVoileLog {
    public final void i(java.lang.String, java.lang.String);
}

# TODO Story 9.7 : ajouter rules spécifiques aux callbacks Go→Kotlin
# (interfaces enregistrées via GoCoreAdapter.setCallbacks)

# TODO Story 11.x : ajouter rules pour les classes annotées @JavascriptInterface
# quand le JS Bridge sera livré
