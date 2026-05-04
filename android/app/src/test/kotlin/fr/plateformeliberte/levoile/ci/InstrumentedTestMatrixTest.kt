package fr.plateformeliberte.levoile.ci

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 12.6 — anti-régression sur la matrice instrumentée API 29/33/34.
 *
 * Quatre contrats vérifiés :
 *  1. `release-android.yml` job `instrumented-tests` cible API 29 + 33 + 34
 *     et invoque `connectedApkDirectDebugAndroidTest` (flavor apkDirect car
 *     `fdroid` désactive UpdateCheckWorker — Story 12.5).
 *  2. `android-instrumented.yml` (push main only) existe avec la même matrice.
 *  3. Les 8 scénarios `androidTest/scenarios/<Name>Test.kt` + 1
 *     `VpnConflictDetectionTest.kt` existent — un dev ne peut pas en retirer
 *     un par accident.
 *  4. Les 2 squelettes Stories 12.3 / 12.5 (`SignatureValidationTest.kt` +
 *     `UpdateNotificationFlowTest.kt`) existent toujours en `androidTest/`.
 */
class InstrumentedTestMatrixTest {

    @Test
    fun `release-android yml matrix contient API 29 33 34 + connectedApkDirectDebugAndroidTest`() {
        val w = resolveWorkflow("release-android.yml")
        val content = w.readText()
        assertTrue(
            "Matrix doit contenir API 29",
            content.contains("api-level: [29, 33, 34]") || content.contains("- 29"),
        )
        assertTrue(
            "Matrix doit contenir API 33",
            content.contains("33"),
        )
        assertTrue(
            "Matrix doit contenir API 34",
            content.contains("34"),
        )
        assertTrue(
            "Job instrumented-tests doit invoquer connectedApkDirectDebugAndroidTest (Story 12.5 flavor)",
            content.contains("connectedApkDirectDebugAndroidTest"),
        )
        assertTrue(
            "L'action reactivecircus/android-emulator-runner@v2 est requise (epics.md l. 2196)",
            content.contains("reactivecircus/android-emulator-runner@v2"),
        )
    }

    @Test
    fun `android-instrumented yml existe avec matrice et trigger push main only`() {
        val w = resolveWorkflow("android-instrumented.yml")
        val content = w.readText()
        assertTrue("Le workflow doit etre nomme correctement", content.contains("Android · Instrumented"))
        assertTrue(
            "Trigger doit cibler push branches: [main] (pas pull_request)",
            content.contains("branches: [main]"),
        )
        assertTrue(
            "Le workflow ne doit PAS s'executer sur pull_request (cout matrice)",
            !Regex("""(?m)^\s*pull_request:""").containsMatchIn(content),
        )
        listOf("29", "33", "34").forEach { api ->
            assertTrue("Matrix doit contenir API $api", content.contains(api))
        }
    }

    @Test
    fun `8 scenarios + VpnConflictDetection sont presents en androidTest`() {
        val expected = listOf(
            "scenarios/VpnServiceConsentTest.kt",
            "scenarios/VpnTunnelStartupTest.kt",
            "scenarios/KillSwitchHeuristicsTest.kt",
            "scenarios/OnboardingFlowTest.kt",
            "scenarios/JsBridgeConnectDisconnectTest.kt",
            "scenarios/FailoverRelayTest.kt",
            "scenarios/PersistentNotificationTest.kt",
            "scenarios/DisconnectFromNotificationTest.kt",
            "conflict/VpnConflictDetectionTest.kt",
        )
        val androidTestRoot = resolveAndroidTestRoot()
        expected.forEach { rel ->
            val file = File(androidTestRoot, rel)
            assertTrue(
                "Fichier instrumente manquant : ${file.absolutePath}",
                file.exists(),
            )
        }
    }

    @Test
    fun `squelettes Story 12-3 et 12-5 conserves`() {
        val androidTestRoot = resolveAndroidTestRoot()
        assertTrue(
            "SignatureValidationTest.kt manquant (Story 12.3 squelette)",
            File(androidTestRoot, "security/SignatureValidationTest.kt").exists(),
        )
        assertTrue(
            "UpdateNotificationFlowTest.kt manquant (Story 12.5 squelette)",
            File(androidTestRoot, "update/UpdateNotificationFlowTest.kt").exists(),
        )
    }

    private fun resolveWorkflow(name: String): File {
        val candidates = listOf(
            "../../.github/workflows/$name",
            "../.github/workflows/$name",
            ".github/workflows/$name",
        )
        return candidates.map { File(it) }.firstOrNull { it.exists() }
            ?: throw AssertionError("$name introuvable. user.dir=${System.getProperty("user.dir")}")
    }

    private fun resolveAndroidTestRoot(): File {
        val candidates = listOf(
            "src/androidTest/kotlin/fr/plateformeliberte/levoile",
            "app/src/androidTest/kotlin/fr/plateformeliberte/levoile",
            "android/app/src/androidTest/kotlin/fr/plateformeliberte/levoile",
        )
        return candidates.map { File(it) }.firstOrNull { it.exists() }
            ?: throw AssertionError("androidTest root introuvable. user.dir=${System.getProperty("user.dir")}")
    }
}
