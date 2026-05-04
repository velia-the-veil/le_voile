package fr.plateformeliberte.levoile.fdroid

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.yaml.snakeyaml.Yaml
import java.io.File

/**
 * Story 12.1 — anti-régression métadonnées F-Droid.
 *
 * Vérifie que :
 *  1. Le YAML F-Droid (metadata/fr.plateformeliberte.levoile.yml) existe, parse
 *     et contient les champs minimums attendus par F-Droid lint.
 *  2. Le `CurrentVersionCode` du YAML est cohérent avec `versionCode` déclaré
 *     dans `android/app/build.gradle.kts` — sinon F-Droid build une version
 *     incohérente (un dev qui bump l'un sans l'autre verrait le test fail
 *     avec un message clair).
 *  3. Les descriptions multilingues existent pour en-US ET fr-FR.
 *  4. Au moins 4 screenshots PNG par locale, ≤ 512 KB chacun.
 *
 * JVM-only, pas de framework externe à part snakeyaml (test scope only — cf.
 * AuditCITest anti-fuite NFR-AND-3).
 */
class FdroidMetadataTest {

    @Test
    fun `metadata yaml existe et est valide`() {
        val yaml = resolveYaml()
        @Suppress("UNCHECKED_CAST")
        val parsed = Yaml().load<Map<String, Any>>(yaml.readText())
        assertEquals("GPL-3.0-or-later", parsed["License"])
        @Suppress("UNCHECKED_CAST")
        val categories = parsed["Categories"] as List<String>
        assertTrue("Categories doit contenir Security", categories.contains("Security"))
        assertEquals(
            "https://github.com/velia-the-veil/le_voile",
            parsed["SourceCode"],
        )
        @Suppress("UNCHECKED_CAST")
        val builds = parsed["Builds"] as List<*>
        assertTrue("Builds: doit contenir au moins 1 entrée", builds.isNotEmpty())
        assertEquals("Tags", parsed["UpdateCheckMode"])
        assertEquals("Le Voile", parsed["AutoName"])
    }

    @Test
    fun `versionCode YAML coherent avec build gradle kts`() {
        val yaml = resolveYaml()
        @Suppress("UNCHECKED_CAST")
        val parsed = Yaml().load<Map<String, Any>>(yaml.readText())
        val yamlVersionCode = (parsed["CurrentVersionCode"] as Number).toInt()

        val buildGradle = resolveAppBuildGradle()
        val regex = Regex("""versionCode\s*=\s*(\d+)""")
        val match = regex.find(buildGradle.readText())
            ?: throw AssertionError("versionCode introuvable dans app/build.gradle.kts")
        val gradleVersionCode = match.groupValues[1].toInt()

        assertEquals(
            "Le YAML F-Droid (CurrentVersionCode=$yamlVersionCode) doit etre coherent avec " +
                "android/app/build.gradle.kts (versionCode=$gradleVersionCode). " +
                "Bumper les 2 ensemble pour chaque release (cf. docs/fdroid-build-recipe.md).",
            gradleVersionCode,
            yamlVersionCode,
        )
    }

    @Test
    fun `descriptions existent pour en-US et fr-FR avec mention zero tracking en FR`() {
        val metadataDir = resolveMetadataDir()
        listOf("en-US", "fr-FR").forEach { lang ->
            assertTrue(
                "title.txt manquant pour $lang",
                File(metadataDir, "$lang/title.txt").exists(),
            )
            assertTrue(
                "short_description.txt manquant pour $lang",
                File(metadataDir, "$lang/short_description.txt").exists(),
            )
            assertTrue(
                "full_description.txt manquant pour $lang",
                File(metadataDir, "$lang/full_description.txt").exists(),
            )
        }
        val frFull = File(metadataDir, "fr-FR/full_description.txt").readText()
        assertTrue(
            "fr-FR/full_description.txt doit mentionner 'zéro tracking' (epics.md l. 2081)",
            frFull.contains("zéro tracking", ignoreCase = true),
        )
        assertTrue(
            "fr-FR/full_description.txt doit mentionner 'zéro télémétrie'",
            frFull.contains("zéro télémétrie", ignoreCase = true),
        )
    }

    @Test
    fun `au moins 4 screenshots par locale sous 512 KB chacun`() {
        val metadataDir = resolveMetadataDir()
        listOf("en-US", "fr-FR").forEach { lang ->
            val screenshots = File(metadataDir, "$lang/images/phoneScreenshots")
            assertTrue(
                "Dossier screenshots manquant pour $lang : ${screenshots.absolutePath}",
                screenshots.isDirectory,
            )
            val pngs = screenshots.listFiles { f -> f.name.endsWith(".png") } ?: emptyArray()
            assertTrue(
                "Au moins 4 screenshots PNG attendus pour $lang, actuel: ${pngs.size}",
                pngs.size >= 4,
            )
            pngs.forEach { png ->
                assertTrue(
                    "Screenshot ${png.name} > 512 KB (${png.length() / 1024} KB) — compresser via oxipng",
                    png.length() <= 512 * 1024,
                )
            }
        }
    }

    // ---------- Helpers — résolution chemins selon cwd Gradle ----------

    private fun resolveYaml(): File = candidates(
        listOf(
            "../../metadata/fr.plateformeliberte.levoile.yml",
            "../metadata/fr.plateformeliberte.levoile.yml",
            "metadata/fr.plateformeliberte.levoile.yml",
        ),
        "metadata/fr.plateformeliberte.levoile.yml",
    )

    private fun resolveMetadataDir(): File = candidates(
        listOf(
            "../../metadata/fr.plateformeliberte.levoile",
            "../metadata/fr.plateformeliberte.levoile",
            "metadata/fr.plateformeliberte.levoile",
        ),
        "metadata/fr.plateformeliberte.levoile/",
    )

    private fun resolveAppBuildGradle(): File = candidates(
        listOf(
            "build.gradle.kts",
            "app/build.gradle.kts",
            "android/app/build.gradle.kts",
        ),
        "android/app/build.gradle.kts",
    ) { it.exists() && it.readText().contains("applicationId") }

    private fun candidates(
        paths: List<String>,
        label: String,
        extra: (File) -> Boolean = { it.exists() },
    ): File = paths.map { File(it) }.firstOrNull(extra)
        ?: throw AssertionError(
            "$label introuvable. user.dir=${System.getProperty("user.dir")} ; " +
                "candidates : ${paths.joinToString()}",
        )
}
