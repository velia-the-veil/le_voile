package fr.plateformeliberte.levoile.assets

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 11.1 — vérifie que les assets web/ sont en place après le sync (ou
 * dans notre cas Option 2 : assets versionnés Android-natifs).
 *
 * Test JVM-only qui lit le filesystem post-sync — ne lance pas le script bash
 * (test portable, ne dépend pas de bash sur la CI).
 */
class SyncFrontendTest {

    @Test
    fun `assets web contient les fichiers attendus apres sync`() {
        val webDir = resolveWebDir()
        assertTrue(
            "assets/web/ absent — exécuter android/scripts/sync-frontend.sh d'abord",
            webDir.exists()
        )
        val expectedFiles = listOf(
            "index.html",
            "style.css",
            "app.js",
            "style-android.css",
        )
        expectedFiles.forEach { name ->
            val f = File(webDir, name)
            assertTrue(
                "Fichier attendu manquant : ${f.path} (cohérent AC #1 Story 11.1)",
                f.exists() && f.length() > 0
            )
        }
    }

    @Test
    fun `index html reference style-android css`() {
        val indexHtml = File(resolveWebDir(), "index.html")
        assertTrue("index.html doit exister", indexHtml.exists())
        val content = indexHtml.readText()
        assertTrue(
            "index.html ne référence pas style-android.css — sync corrompu",
            content.contains("style-android.css")
        )
    }

    @Test
    fun `style-android css cible body platform-android`() {
        val cssAndroid = File(resolveWebDir(), "style-android.css")
        assertTrue("style-android.css doit exister", cssAndroid.exists())
        val content = cssAndroid.readText()
        assertTrue(
            "style-android.css doit cibler body.platform-android",
            content.contains("body.platform-android")
        )
        assertTrue(
            "style-android.css doit désactiver .desktop-only (cohérent ux l. 1353)",
            content.contains(".desktop-only") && content.contains("display: none")
        )
    }

    /**
     * Working dir Gradle pour :app:testDebugUnitTest = android/app/.
     * On gère plusieurs candidats (selon où le test est lancé).
     */
    private fun resolveWebDir(): File {
        val candidates = listOf(
            File("src/main/assets/web"),
            File("app/src/main/assets/web"),
            File("android/app/src/main/assets/web"),
        )
        return candidates.firstOrNull { it.exists() } ?: candidates.first()
    }
}
