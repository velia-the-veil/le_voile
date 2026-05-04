package fr.plateformeliberte.levoile.audit

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 10.4 — anti-regression de la liste canonique des modules interdits.
 *
 * Vérifie deux contrats critiques pour l'audit télémétrie zéro-tracking :
 *  1. Le workflow GitHub Actions invoque bien les tasks Gradle d'audit
 *     pour les 2 modules `:app` et `:levoile-core`.
 *  2. La liste canonique des 8 préfixes groupId interdits est présente
 *     dans `android/build.gradle.kts` (Option A — task factorisée
 *     `subprojects { ... }`).
 *
 * Cohérent epics.md l. 1781 : « test unitaire `AuditCITest` vérifie que
 * la liste contient au minimum les 8 modules canoniques ».
 *
 * **JVM-only**, pas de framework YAML (lecture textuelle + `contains`).
 *
 * Le cwd Gradle pour `:app:testDebugUnitTest` peut être `android/` ou
 * `android/app/` selon la version Gradle / wrapper. Les helpers ci-dessous
 * essayent plusieurs candidats — pattern aligné avec
 * MainActivityConfigTest (`parseManifest`, `resolveAsset`).
 */
class AuditCITest {

    @Test
    fun `workflow android-audit yml invoque les tasks Gradle d'audit pour les 2 modules`() {
        val workflow = resolveWorkflow()
        val content = workflow.readText()

        assertTrue(
            "Le workflow doit invoquer :app:auditTelemetryDependencies — actuel : $workflow",
            content.contains(":app:auditTelemetryDependencies"),
        )
        assertTrue(
            "Le workflow doit invoquer :levoile-core:auditTelemetryDependencies — actuel : $workflow",
            content.contains(":levoile-core:auditTelemetryDependencies"),
        )
        assertTrue(
            "Le workflow doit etre triggere sur pull_request",
            content.contains("pull_request:"),
        )
        assertTrue(
            "Le workflow doit etre triggere sur push branche main",
            content.contains("branches: [main]"),
        )
    }

    @Test
    fun `build gradle kts top-level liste les 8 prefixes groupId canoniques`() {
        // Anti-regression : si quelqu'un retire un préfixe (ex. enlever
        // "io.sentry" parce qu'il pense que Sentry n'est plus une menace),
        // ce test fail explicitement avec un message clair et l'ADR à
        // citer (ADR-15) ressort dans le diff de revert.
        val expectedCanonical = listOf(
            "com.google.firebase",
            "com.crashlytics.sdk.android",
            "io.sentry",
            "com.bugsnag",
            "com.mixpanel.android",
            "com.adjust.sdk",
            "io.branch.sdk.android",
            "com.amplitude",
        )
        val buildGradleKts = resolveAndroidTopLevelBuildGradle()
        val content = buildGradleKts.readText()
        expectedCanonical.forEach { canonicalGroup ->
            assertTrue(
                "android/build.gradle.kts doit lister le prefixe canonique '$canonicalGroup' (ADR-15 — preuve audit complet) — chemin : $buildGradleKts",
                content.contains("\"$canonicalGroup\""),
            )
        }
    }

    /**
     * Code-review post-Epic 11 (M5) : Story 11.8 a introduit
     * `testImplementation(libs.org.json)` (Maven org.json:json:20240303) pour
     * permettre les tests JVM-only de ConfigStore. La règle de l'ADR-08 +
     * NFR-AND-3 exige que cette dépendance reste **scope test only**, donc :
     *  - la déclaration doit être `testImplementation(libs.org.json)` ou
     *    `testImplementation("org.json:json:...")`.
     *  - PAS de `implementation(libs.org.json)` (qui ferait fuiter dans l'APK).
     *  - PAS de `androidTestImplementation` non plus (cohérent NFR-AND-3 :
     *    pas de bloat test instrumenté).
     *
     * Ce test fail explicitement si une PR future bascule la dépendance hors
     * scope test ou crée un usage `runtime` accidentel.
     */
    @Test
    fun `org json reste scope testImplementation only NFR-AND-3`() {
        val appBuildGradle = resolveAppBuildGradle()
        val content = appBuildGradle.readText()
        assertTrue(
            "app/build.gradle.kts doit declarer testImplementation(libs.org.json) — Story 11.8 ConfigStore",
            content.contains("testImplementation(libs.org.json)"),
        )
        // Anti-fuite : refuser implementation(libs.org.json) ou api(libs.org.json).
        assertTrue(
            "app/build.gradle.kts NE DOIT PAS contenir implementation(libs.org.json) — " +
                "violation ADR-08 + NFR-AND-3 (la stub Android.jar suffit en runtime).",
            !content.contains("\n    implementation(libs.org.json)") &&
                !content.contains("\n    api(libs.org.json)"),
        )
    }

    /**
     * Story 12.1 — anti-fuite snakeyaml. La dépendance est introduite pour
     * `FdroidMetadataTest` (parsing du YAML F-Droid en test JVM-only). Elle ne
     * doit JAMAIS basculer en `implementation` ou `androidTestImplementation`
     * (~250 KB inutiles dans l'APK release, viole NFR-AND-3 + ADR-08).
     *
     * Pattern miroir du test `org json reste scope testImplementation only`.
     */
    @Test
    fun `snakeyaml reste scope testImplementation only NFR-AND-3`() {
        val appBuildGradle = resolveAppBuildGradle()
        val content = appBuildGradle.readText()
        assertTrue(
            "app/build.gradle.kts doit declarer testImplementation(libs.snakeyaml) — Story 12.1 FdroidMetadataTest",
            content.contains("testImplementation(libs.snakeyaml)"),
        )
        assertTrue(
            "app/build.gradle.kts NE DOIT PAS contenir implementation(libs.snakeyaml) — " +
                "violation ADR-08 + NFR-AND-3 (~250 KB inutiles dans l'APK release).",
            !content.contains("\n    implementation(libs.snakeyaml)") &&
                !content.contains("\n    api(libs.snakeyaml)"),
        )
        assertTrue(
            "app/build.gradle.kts NE DOIT PAS contenir androidTestImplementation(libs.snakeyaml) — " +
                "le test FdroidMetadataTest est JVM-only.",
            !content.contains("androidTestImplementation(libs.snakeyaml)"),
        )
    }

    private fun resolveAppBuildGradle(): File {
        val candidates = listOf(
            File("build.gradle.kts"),                      // cwd = android/app/
            File("app/build.gradle.kts"),                   // cwd = android/
            File("android/app/build.gradle.kts"),           // cwd = repo root
        )
        // Filtre : on veut le module :app — détectable via `applicationId` qui
        // n'apparaît jamais dans un module library ou top-level.
        return candidates.firstOrNull { it.exists() && it.readText().contains("applicationId") }
            ?: throw AssertionError(
                "android/app/build.gradle.kts introuvable. " +
                    "user.dir=${System.getProperty("user.dir")}",
            )
    }

    @Test
    fun `auditTelemetryDependencies est branchee sur la lifecycle task check`() {
        // Garde-fou : le hook `afterEvaluate { tasks.findByName("check")?.dependsOn(...) }`
        // doit etre present sinon les devs qui font `./gradlew check` localement
        // ne verraient pas l'audit échouer avant un push. Pattern strict matché en
        // multi-line — un commentaire contenant "check" ne suffit pas.
        val buildGradleKts = resolveAndroidTopLevelBuildGradle()
        val content = buildGradleKts.readText()
        val hookRegex = Regex(
            """tasks\.findByName\("check"\)\??\.dependsOn\("auditTelemetryDependencies"\)""",
        )
        assertTrue(
            "android/build.gradle.kts doit hook auditTelemetryDependencies sur la task check via tasks.findByName(\"check\")?.dependsOn(...) — lifecycle Gradle standard",
            hookRegex.containsMatchIn(content),
        )
    }

    // ---------- Helpers — résolution chemins relatifs au cwd Gradle ----------

    private fun resolveWorkflow(): File {
        // cwd peut être android/, android/app/, ou la racine du repo selon Gradle.
        val candidates = listOf(
            File("../.github/workflows/android-audit.yml"),
            File("../../.github/workflows/android-audit.yml"),
            File("../../../.github/workflows/android-audit.yml"),
            File(".github/workflows/android-audit.yml"),
        )
        return candidates.firstOrNull { it.exists() }
            ?: throw AssertionError(
                "Workflow android-audit.yml introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates : ${candidates.joinToString { it.absolutePath }}",
            )
    }

    private fun resolveAndroidTopLevelBuildGradle(): File {
        val candidates = listOf(
            File("../build.gradle.kts"),
            File("../../build.gradle.kts"),
            File("build.gradle.kts"),
            File("android/build.gradle.kts"),
        )
        // Filtre : on veut le top-level Android (qui contient `subprojects`),
        // pas le top-level racine du repo (qui n'existe pas) ni un module.
        return candidates.firstOrNull { it.exists() && it.readText().contains("subprojects {") }
            ?: throw AssertionError(
                "android/build.gradle.kts top-level introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates : ${candidates.joinToString { it.absolutePath }}",
            )
    }
}
