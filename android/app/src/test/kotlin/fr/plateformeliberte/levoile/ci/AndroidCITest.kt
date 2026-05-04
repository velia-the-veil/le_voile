package fr.plateformeliberte.levoile.ci

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 12.2 — anti-régression sur les workflows GitHub Actions Android.
 *
 * Trois contrats vérifiés :
 *  1. `.github/workflows/android-audit.yml` (renommé `Android · CI`) contient
 *     les 5 jobs Story 12.2 + `audit-dependencies` Story 10.4 + `ci-summary`.
 *  2. `.github/workflows/release-android.yml` (squelette Story 12.2) contient
 *     les jobs `ci`, `sign-apk`, `reproducibility-check`, `instrumented-tests`,
 *     `publish-release` + matrice API 29/33/34.
 *  3. La whitelist permissions du job `permission-audit` reste cohérente avec
 *     les 5 permissions NFR-AND-7 + 1 permission AGP-injectée.
 *
 * JVM-only, lecture textuelle (pas de parser YAML) — pattern aligné
 * AuditCITest Story 10.4.
 */
class AndroidCITest {

    @Test
    fun `android-audit yml contient les 5 jobs Story 12-2 + audit-dependencies Story 10-4`() {
        val w = resolveWorkflow("android-audit.yml")
        val content = w.readText()

        assertTrue(
            "Le workflow doit etre renomme 'Android · CI' (Story 12.2 — plus 'Android · Audit télémétrie')",
            content.contains("name: Android · CI"),
        )
        listOf(
            "audit-dependencies:",       // Story 10.4 — anti-régression
            "lint:",                      // 12.2
            "unit-tests:",                // 12.2
            "permission-audit:",          // 12.2
            "proguard-syntax:",           // 12.2
            "ci-summary:",                // 12.2
        ).forEach { jobKey ->
            assertTrue(
                "Le workflow doit contenir le job '$jobKey'",
                content.contains(jobKey),
            )
        }
        assertTrue(
            "Le workflow doit declencher sur metadata/** (Story 12.1)",
            content.contains("'metadata/**'"),
        )
        assertTrue(
            "Le workflow doit referencer release-android.yml dans les triggers paths",
            content.contains("'.github/workflows/release-android.yml'"),
        )
        assertTrue(
            "Le workflow doit declarer permissions pull-requests: write (sticky comment)",
            content.contains("pull-requests: write"),
        )
    }

    @Test
    fun `release-android yml squelette contient les jobs gating + matrice + placeholders`() {
        val w = resolveWorkflow("release-android.yml")
        val content = w.readText()

        assertTrue(
            "Triggers tags v* requis",
            content.contains("tags: ['v*']") ||
                (content.contains("tags:") && content.contains("'v*'")),
        )
        assertTrue(
            "Trigger workflow_dispatch requis (release manuelle ad hoc)",
            content.contains("workflow_dispatch:"),
        )
        listOf(
            "ci:",
            "sign-apk:",
            "reproducibility-check:",
            "instrumented-tests:",
            "publish-release:",
        ).forEach { job ->
            assertTrue(
                "Le workflow doit contenir le job '$job'",
                content.contains(job),
            )
        }
        assertTrue(
            "Matrix instrumented-tests doit cibler EXACTEMENT [29, 33, 34] (epics.md l. 2202-2206) — " +
                "fix L3 Code Review 2026-05-03 : refuser un fallback laxiste qui matcherait " +
                "n'importe quel YAML avec ces 3 nombres separes (ex. timeout 34)",
            content.contains("api-level: [29, 33, 34]"),
        )
        assertTrue(
            "Permission contents: write requise pour gh release create",
            content.contains("contents: write"),
        )
    }

    @Test
    fun `permission-audit whitelist coherente avec AndroidManifest xml`() {
        val w = resolveWorkflow("android-audit.yml")
        val content = w.readText()
        val expectedManifestPerms = listOf(
            "android.permission.INTERNET",
            "android.permission.FOREGROUND_SERVICE",
            "android.permission.FOREGROUND_SERVICE_DATA_SYNC",
            "android.permission.FOREGROUND_SERVICE_SPECIAL_USE",
            "android.permission.POST_NOTIFICATIONS",
        )
        expectedManifestPerms.forEach { p ->
            // Match obligatoire dans une string-quoted ('xxx'). Évite les faux-positifs
            // si la permission apparaît seulement dans un commentaire YAML — fix
            // Code Review 2026-05-03.
            assertTrue(
                "Le workflow doit lister la permission '$p' (quoted) dans la whitelist NFR-AND-7",
                content.contains("'$p'"),
            )
        }
        assertTrue(
            "Le workflow doit autoriser DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION (AGP-injectée, quoted)",
            content.contains("'fr.plateformeliberte.levoile.debug.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION'"),
        )
    }

    private fun resolveWorkflow(name: String): File {
        val candidates = listOf(
            File("../../.github/workflows/$name"),
            File("../.github/workflows/$name"),
            File(".github/workflows/$name"),
        )
        return candidates.firstOrNull { it.exists() }
            ?: throw AssertionError(
                "$name introuvable. user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates : ${candidates.joinToString { it.absolutePath }}",
            )
    }
}
