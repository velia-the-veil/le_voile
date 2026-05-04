package fr.plateformeliberte.levoile.repro

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 12.4 — anti-régression sur la configuration build reproductible APK.
 *
 * Quatre contrats vérifiés :
 *  1. `app/build.gradle.kts` hook `tasks.withType<AbstractArchiveTask>` avec
 *     `isPreserveFileTimestamps = false` + `isReproducibleFileOrder = true`.
 *  2. `dependenciesInfo` désactivé (anti-régression Story 12.x existant — un
 *     dev pourrait l'enlever par accident en simplifiant le fichier).
 *  3. `gradle.properties` contient les pinnings reproductibilité
 *     (`org.gradle.parallel=false`, `caching=false`, `daemon=false`).
 *  4. `android/scripts/build-apk-release.sh` existe et invoque
 *     `assembleRelease` avec les flags `--no-daemon --no-parallel
 *     --no-build-cache` + `sha256sum`.
 */
class ReproducibleBuildConfigTest {

    @Test
    fun `build gradle hook AbstractArchiveTask reproducibilite`() {
        val content = appBuildGradle().readText()
        assertTrue(
            "build.gradle.kts doit hook tasks.withType<AbstractArchiveTask>",
            content.contains("AbstractArchiveTask"),
        )
        assertTrue(
            "isPreserveFileTimestamps = false requis (NFR-AND-6)",
            content.contains("isPreserveFileTimestamps = false"),
        )
        assertTrue(
            "isReproducibleFileOrder = true requis (NFR-AND-6)",
            content.contains("isReproducibleFileOrder = true"),
        )
    }

    @Test
    fun `build gradle dependenciesInfo desactive (anti-regression Story 12-x)`() {
        val content = appBuildGradle().readText()
        assertTrue(
            "dependenciesInfo.includeInApk = false requis (signature AGP cassait reproductibilite)",
            content.contains("includeInApk = false"),
        )
        assertTrue(
            "dependenciesInfo.includeInBundle = false requis",
            content.contains("includeInBundle = false"),
        )
    }

    @Test
    fun `gradle properties contient pinnings reproductibilite`() {
        val content = gradleProperties().readText()
        listOf(
            "org.gradle.parallel=false",
            "org.gradle.caching=false",
            "org.gradle.daemon=false",
        ).forEach { line ->
            assertTrue(
                "gradle.properties doit contenir '$line' (reproductibilite Story 12.4)",
                content.lines().any { it.trim() == line },
            )
        }
    }

    @Test
    fun `script build-apk-release sh existe et invoque assemble Release no-daemon no-parallel no-cache + sha256sum`() {
        val script = buildApkReleaseSh()
        assertTrue("android/scripts/build-apk-release.sh manquant", script.exists())
        val content = script.readText()
        // Le script construit la task `:app:assemble<Flavor>Release` via une
        // substitution shell — Story 12.5 productFlavors apkDirect/fdroid.
        // Fix Code Review 2026-05-03 : ne plus chercher la chaine litterale
        // "assembleRelease" qui n'existe plus.
        assertTrue(
            "Le script doit invoquer une task assemble<Flavor>Release",
            content.contains("assemble") && content.contains("Release"),
        )
        assertTrue(
            "Le script doit cibler le flavor apkDirect par defaut (canal GitHub direct)",
            content.contains("LEVOILE_FLAVOR:-apkDirect") ||
                content.contains("LEVOILE_FLAVOR=apkDirect"),
        )
        assertTrue(
            "Le script doit utiliser --no-daemon --no-parallel --no-build-cache",
            content.contains("--no-daemon") &&
                content.contains("--no-parallel") &&
                content.contains("--no-build-cache"),
        )
        assertTrue(
            "Le script doit calculer sha256sum",
            content.contains("sha256sum"),
        )
        assertTrue(
            "Le script doit produire un apk-content-archive.zip",
            content.contains("apk-content-archive.zip"),
        )
    }

    private fun appBuildGradle(): File = candidates(
        listOf("build.gradle.kts", "app/build.gradle.kts", "android/app/build.gradle.kts"),
        "app/build.gradle.kts",
    ) { it.exists() && it.readText().contains("applicationId") }

    private fun gradleProperties(): File = candidates(
        listOf("../gradle.properties", "gradle.properties", "android/gradle.properties"),
        "android/gradle.properties",
    )

    private fun buildApkReleaseSh(): File = candidates(
        listOf(
            "../scripts/build-apk-release.sh",
            "scripts/build-apk-release.sh",
            "android/scripts/build-apk-release.sh",
        ),
        "android/scripts/build-apk-release.sh",
    )

    private fun candidates(
        paths: List<String>,
        label: String,
        extra: (File) -> Boolean = { it.exists() },
    ): File = paths.map { File(it) }.firstOrNull(extra)
        ?: throw AssertionError(
            "$label introuvable. user.dir=${System.getProperty("user.dir")} ; candidates : ${paths.joinToString()}",
        )
}
