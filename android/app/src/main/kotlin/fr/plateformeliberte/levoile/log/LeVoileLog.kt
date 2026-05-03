package fr.plateformeliberte.levoile.log

import android.util.Log
import fr.plateformeliberte.levoile.BuildConfig

/**
 * Story 10.5 — Wrapper logger respect zéro-data-utilisateur (NFR-AND-9
 * prd.md l. 705 + NFR22a prd.md l. 672 + ADR-15 architecture.md l. 2439-2442).
 *
 * Filtrage par buildType (cohérent architecture.md l. 705) :
 *  - **debug** : INFO+ visible Logcat (`i` / `w` / `e` — `d` et `v` strippés
 *    par ProGuard rules `-assumenosideeffects` Story 9.1, mais comme ProGuard
 *    est désactivé en debug par défaut, `Log.d`/`Log.v` direct fonctionnent).
 *  - **release** : WARN+ uniquement. Le `if (BuildConfig.DEBUG)` interne sur
 *    [i] le rend no-op à la compilation Kotlin ; ProGuard l'élimine
 *    entièrement du bytecode via la règle `-assumenosideeffects` ajoutée
 *    Story 10.5 (cf. `proguard-rules.pro`).
 *
 * **Triple ceinture de sécurité** :
 *  1. `if (BuildConfig.DEBUG)` interne — fonctionne même si ProGuard est
 *     désactivé sur un buildType custom futur.
 *  2. ProGuard strip — élimine les chaînes constantes du `.dex` release.
 *  3. `LogFilteringTest` (test JVM scan source) — fail à chaque PR si une
 *     variable interdite (`$url`, `$domain`, `$destIp`, `$pinnedApp`, etc.)
 *     apparaît dans un template `Log.*` ou `LeVoileLog.*`.
 *
 * **Pas de méthodes `d()` ni `v()`** — c'est volontaire. Les niveaux DEBUG /
 * VERBOSE sont déjà strippés Story 9.1, et les exposer dans le wrapper
 * encouragerait leur usage en runtime debug. Le dev qui veut un trace
 * verbeux passe `android.util.Log.d` directement.
 *
 * **Pas de format-string vararg** — accepter uniquement `String message`
 * (déjà formaté côté caller). Cohérent NFR22a : un format-string type
 * `LeVoileLog.i(TAG, "User %s connected to %s", id, ip)` cacherait les
 * variables au scan ; forcer le pré-formatage rend le scan trivial.
 *
 * **Convention pour les stories futures** : tout NOUVEAU log doit utiliser
 * [LeVoileLog] plutôt que `android.util.Log`. Les sites legacy pré-Story 10.5
 * restent inchangés (cf. [README-android.md] § « Filtrage logs ») — ProGuard
 * strippe les 2 wrappers de la même manière, aucun gain de sécurité à
 * migrer le legacy.
 */
internal object LeVoileLog {

    /**
     * INFO — visible Logcat en debug, **strippé en release** par ProGuard
     * (`-assumenosideeffects class fr.plateformeliberte.levoile.log.LeVoileLog`).
     * Le `if (BuildConfig.DEBUG)` interne est une 2ème ceinture défensive.
     */
    fun i(tag: String, message: String) {
        if (BuildConfig.DEBUG) {
            Log.i(tag, message)
        }
    }

    /** WARN — visible Logcat en debug ET release. */
    fun w(tag: String, message: String) {
        Log.w(tag, message)
    }

    /** WARN avec throwable — visible Logcat en debug ET release. */
    fun w(tag: String, message: String, throwable: Throwable) {
        Log.w(tag, message, throwable)
    }

    /** ERROR — visible Logcat en debug ET release. */
    fun e(tag: String, message: String) {
        Log.e(tag, message)
    }

    /** ERROR avec throwable — visible Logcat en debug ET release. */
    fun e(tag: String, message: String, throwable: Throwable) {
        Log.e(tag, message, throwable)
    }
}
