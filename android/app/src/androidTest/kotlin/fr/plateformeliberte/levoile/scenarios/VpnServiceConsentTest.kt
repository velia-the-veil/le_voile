package fr.plateformeliberte.levoile.scenarios

import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.Until
import fr.plateformeliberte.levoile.MainActivity
import fr.plateformeliberte.levoile.testutils.LeVoileTestRule
import org.junit.Assert.assertTrue
import org.junit.Ignore
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Story 12.6 (a) — premier lancement → `VpnService.prepare()` retourne un
 * Intent → l'activité de consent système Android s'affiche.
 *
 * Cohérent epics.md l. 2208 + Story 9.5 (vpnConsentLauncher dans MainActivity).
 *
 * Pré-requis :
 *  - Onboarding complété (LeVoileTestRule).
 *  - Pas de consent VpnService accordé sur ce device pour cette app.
 *    L'émulateur fresh-boot n'a aucun consent par défaut → OK pour le matrix CI.
 *
 * **TODO ajustement runtime** : le selector `By.pkg("com.android.vpndialogs")`
 * peut varier selon la ROM (system_server sur certaines versions). Tester sur
 * l'émulateur API 29 / 33 / 34 et adapter si nécessaire.
 */
@RunWith(AndroidJUnit4::class)
class VpnServiceConsentTest {

    @get:Rule
    val leVoileRule = LeVoileTestRule()

    /**
     * Scaffold uniquement — runtime impl pending (cf. Story 12.6 Completion
     * Notes « À FAIRE PAR LE MAINTENEUR » point 3 + fix Code Review 2026-05-03).
     *
     * Bug pré-fix : le test ne tappait pas Connect avant `device.wait`, et
     * `device.wait(Until.hasObject(...), 5000)` retourne un `Boolean`
     * autoboxé (jamais null), donc `consentVisible != null` était toujours
     * vrai → test passait par accident sans rien valider.
     *
     * Pour activer ce test, le mainteneur doit :
     *  1. Tapper Connect via Espresso.onWebView() ou reflection
     *     MainActivity.requestVpnStart(null).
     *  2. Remplacer `consentVisible != null` par `consentVisible == true`
     *     (Boolean comparison — null check était inutile).
     */
    @Ignore("Story 12.6 scaffold — runtime impl pending Phase 2 (cf. Code Review 2026-05-03)")
    @Test
    fun `premier_lancement_invoque_VpnService_prepare_et_affiche_consent`() {
        ActivityScenario.launch(MainActivity::class.java).use {
            val device = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
            val consentVisible = device.wait(
                Until.hasObject(By.pkg("com.android.vpndialogs")),
                5_000,
            )
            assertTrue(
                "VpnService consent dialog non affiché",
                consentVisible == true,
            )
        }
    }
}
