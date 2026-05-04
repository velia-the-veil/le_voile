package fr.plateformeliberte.levoile.security

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 12.3 — anti-régression sur la `signingConfigs.release` de
 * `android/app/build.gradle.kts`.
 *
 * Cinq contrats vérifiés, JVM-only :
 *  1. `signingConfigs.release` est déclaré (pas seulement le default debug).
 *  2. Les credentials sont lus EXCLUSIVEMENT depuis env vars (`System.getenv`)
 *     — pas de string literal de password dans le fichier.
 *  3. v1 désactivé + v2/v3 activés + v4 désactivé (cohérent NFR-AND-5 +
 *     min-sdk-version 29 documenté).
 *  4. `buildTypes.release.signingConfig` est conditionnel (release si env vars
 *     présentes, sinon debug).
 *  5. Le fallback debug ajoute `versionNameSuffix = "-unsigned-LOCAL-DEV"`
 *     pour qu'aucun utilisateur ne confonde un build local avec une release.
 *
 * Pas de framework externe — lecture textuelle du fichier source. Pattern
 * aligné avec `AuditCITest`.
 */
class SigningConfigTest {

    @Test
    fun `app build gradle declare un signingConfigs release`() {
        val content = appBuildGradle().readText()
        assertTrue(
            "signingConfigs.release manquant — Story 12.3",
            content.contains("create(\"release\")") && content.contains("signingConfigs"),
        )
        assertTrue(
            "buildTypes.release doit reference signingConfigs release conditionnellement",
            content.contains("signingConfigs.getByName(\"release\")"),
        )
    }

    @Test
    fun `signingConfigs lit credentials depuis env vars uniquement`() {
        val content = appBuildGradle().readText()
        listOf(
            "LEVOILE_KEYSTORE_PATH",
            "LEVOILE_KEYSTORE_PASSWORD",
            "LEVOILE_KEY_ALIAS",
            "LEVOILE_KEY_PASSWORD",
        ).forEach { envVar ->
            assertTrue(
                "build.gradle.kts doit lire $envVar via System.getenv",
                content.contains("System.getenv(\"$envVar\")"),
            )
        }
    }

    @Test
    fun `aucun string literal de password dans build gradle`() {
        val content = appBuildGradle().readText()
        // Pattern dangereux : storePassword = "literal" (entourage de "). Autorise
        // les valeurs qui resolvent une variable Kotlin (System.getenv, val nommee).
        //
        // NB : on refuse aussi la chaine vide `""` — un dev qui rendrait le
        // password optionnel via `storePassword = ""` ouvrirait un bypass.
        // Fix L4 Code Review 2026-05-03.
        val literalPasswordRegex = Regex(
            """(storePassword|keyPassword)\s*=\s*"([^"$\\n]*)"""",
        )
        val matches = literalPasswordRegex.findAll(content).toList()
        assertTrue(
            "Mots de passe en clair (incl. chaine vide) detectes dans build.gradle.kts : " +
                matches.joinToString { it.value },
            matches.isEmpty(),
        )
    }

    @Test
    fun `v1 signing desactive et v2 v3 actives et v4 desactive sur release`() {
        val content = appBuildGradle().readText()
        assertTrue(
            "enableV1Signing = false attendu (minSdk 29+ — v2 suffit)",
            content.contains("enableV1Signing = false"),
        )
        assertTrue(
            "enableV2Signing = true attendu (NFR-AND-5)",
            content.contains("enableV2Signing = true"),
        )
        assertTrue(
            "enableV3Signing = true attendu (key rotation NFR-AND-5)",
            content.contains("enableV3Signing = true"),
        )
        assertFalse(
            "enableV4Signing doit rester desactive (non requis MVP — Play Asset Delivery)",
            content.contains("enableV4Signing = true"),
        )
    }

    @Test
    fun `fallback debug + versionNameSuffix unsigned LOCAL DEV en absence env var`() {
        val content = appBuildGradle().readText()
        assertTrue(
            "Fallback debug requis pour les builds locaux sans la master key",
            content.contains("versionNameSuffix = \"-unsigned-LOCAL-DEV\""),
        )
        assertTrue(
            "Le fallback doit verifier l'absence de LEVOILE_KEYSTORE_PATH",
            content.contains("System.getenv(\"LEVOILE_KEYSTORE_PATH\")"),
        )
    }

    private fun appBuildGradle(): File {
        val candidates = listOf(
            File("build.gradle.kts"),
            File("app/build.gradle.kts"),
            File("android/app/build.gradle.kts"),
        )
        return candidates.firstOrNull { it.exists() && it.readText().contains("applicationId") }
            ?: throw AssertionError(
                "android/app/build.gradle.kts introuvable. " +
                    "user.dir=${System.getProperty("user.dir")}",
            )
    }
}
