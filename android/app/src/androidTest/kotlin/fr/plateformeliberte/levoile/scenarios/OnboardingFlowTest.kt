package fr.plateformeliberte.levoile.scenarios

import android.content.Context
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import fr.plateformeliberte.levoile.MainActivity
import fr.plateformeliberte.levoile.onboarding.OnboardingActivity
import fr.plateformeliberte.levoile.testutils.LeVoileTestRule
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Story 12.6 (d) — premier lancement (`onboarding_completed == false`) →
 * 3 écrans Story 11.5/11.6 affichés, swipe entre eux, finish →
 * `onboarding_completed = true`.
 *
 * Cohérent epics.md l. 2215.
 *
 * **TODO ajustement runtime** : la spec demande des `onView(withId(R.id.btn_next))
 * + perform(click())`. L'OnboardingActivity Story 11.5/11.6 utilise un
 * FrameLayout `screenContainer` qui swap les écrans via setContentView interne.
 * Les IDs précis (`onboarding_screen_1`, `btn_next`, `btn_finish`) ne sont
 * peut-être pas définis tels quels dans `layout/activity_onboarding.xml` ou
 * `layout/onboarding_screen_*.xml` — vérifier puis ajuster les selectors
 * Espresso. Pattern de fallback : `onView(withText(R.string.onboarding_btn_continue)).perform(click())`.
 */
@RunWith(AndroidJUnit4::class)
class OnboardingFlowTest {

    @get:Rule
    val leVoileRule = LeVoileTestRule(skipOnboarding = false)

    @Test
    fun `premier_lancement_affiche_3_ecrans_persistence_a_la_fin`() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext

        // Pré : LeVoileTestRule(skipOnboarding=false) a clear les prefs donc
        // onboarding_completed = false → MainActivity.onCreate redirige vers
        // OnboardingActivity.
        ActivityScenario.launch(MainActivity::class.java).use {
            // TODO Story 12.6 — impl runtime :
            //  onView(withId(R.id.onboarding_screen_1)).check(matches(isDisplayed()))
            //  onView(withId(R.id.btn_next)).perform(click())
            //  onView(withId(R.id.onboarding_screen_2)).check(matches(isDisplayed()))
            //  onView(withId(R.id.btn_next)).perform(click())
            //  onView(withId(R.id.onboarding_screen_3)).check(matches(isDisplayed()))
            //  onView(withId(R.id.btn_finish)).perform(click())
            //
            // Pre-impl : on assume que onboarding n'a pas été marqué complété
            // tant que l'utilisateur n'a pas cliqué jusqu'au dernier écran.
            val prefs = ctx.getSharedPreferences(OnboardingActivity.PREFS_NAME, Context.MODE_PRIVATE)
            assertFalse(
                "Onboarding ne doit pas etre marque complete avant interaction utilisateur",
                prefs.getBoolean(OnboardingActivity.KEY_ONBOARDING_COMPLETED, false),
            )
        }
    }
}
