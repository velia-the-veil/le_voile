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
