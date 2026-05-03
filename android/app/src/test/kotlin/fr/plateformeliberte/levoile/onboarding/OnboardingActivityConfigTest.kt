package fr.plateformeliberte.levoile.onboarding

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 11.5 — vérifie que le manifest déclare OnboardingActivity avec
 * exported=false et que les constantes companion sont alignées avec
 * LeVoileBridge (single SharedPreferences scope app).
 */
class OnboardingActivityConfigTest {

    @Test
    fun `manifest declare OnboardingActivity avec exported false`() {
        val content = readManifest()
        assertTrue(
            "AndroidManifest.xml doit declarer OnboardingActivity",
            content.contains(".onboarding.OnboardingActivity")
        )
        val activityBlock = extractActivityBlock(content, ".onboarding.OnboardingActivity")
        assertTrue(
            "OnboardingActivity doit etre exported=false (Activity interne)",
            activityBlock.contains("android:exported=\"false\"")
        )
    }

    @Test
    fun `OnboardingActivity PREFS_NAME aligne LeVoileBridge`() {
        // Cohérence single SharedPreferences scope.
        assertEquals("levoile_prefs", OnboardingActivity.PREFS_NAME)
        assertEquals("onboarding_completed", OnboardingActivity.KEY_ONBOARDING_COMPLETED)
    }

    private fun readManifest(): String {
        val candidates = listOf(
            File("src/main/AndroidManifest.xml"),
            File("app/src/main/AndroidManifest.xml"),
            File("android/app/src/main/AndroidManifest.xml"),
        )
        return candidates.firstOrNull { it.exists() }?.readText()
            ?: throw AssertionError("AndroidManifest.xml introuvable depuis cwd=${File(".").absolutePath}")
    }

    private fun extractActivityBlock(xml: String, activityName: String): String {
        val start = xml.indexOf(activityName)
        if (start < 0) return ""
        val blockStart = xml.lastIndexOf("<activity", start)
        val blockEnd = xml.indexOf("/>", start)
        return if (blockStart >= 0 && blockEnd > blockStart) {
            xml.substring(blockStart, blockEnd + 2)
        } else ""
    }
}
