package fr.plateformeliberte.levoile.testutils

import android.content.Context
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import fr.plateformeliberte.levoile.onboarding.OnboardingActivity
import org.junit.rules.TestWatcher
import org.junit.runner.Description

/**
 * Story 12.6 — TestRule partagée par les scenarios instrumentés.
 *
 * Rôle :
 *  - Reset les SharedPreferences avant chaque test (évite la pollution croisée).
 *  - Marque l'onboarding comme complété (`onboarding_completed = true`) pour
 *    sauter directement dans `MainActivity` au lancement, sauf pour
 *    `OnboardingFlowTest` qui doit voir l'onboarding.
 *  - Cleanup post-test (logout, disconnect VPN si actif).
 *
 * Usage :
 *   ```
 *   @get:Rule val leVoileRule = LeVoileTestRule()
 *
 *   @Test fun `mon scenario`() {
 *       launchActivity<MainActivity>().use { ... }
 *   }
 *   ```
 *
 * Pour tester l'onboarding lui-même :
 *   `@get:Rule val leVoileRule = LeVoileTestRule(skipOnboarding = false)`
 */
class LeVoileTestRule(
    private val skipOnboarding: Boolean = true,
) : TestWatcher() {

    override fun starting(description: Description) {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val prefs = onboardingPrefs(ctx)
        prefs.edit().clear().apply()
        if (skipOnboarding) {
            prefs.edit().putBoolean(OnboardingActivity.KEY_ONBOARDING_COMPLETED, true).apply()
        }
    }

    override fun finished(description: Description) {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        // Cleanup : reset des SharedPreferences pour les tests suivants.
        onboardingPrefs(ctx).edit().clear().apply()
    }

    private fun onboardingPrefs(ctx: Context): SharedPreferences =
        ctx.getSharedPreferences(OnboardingActivity.PREFS_NAME, Context.MODE_PRIVATE)
}
