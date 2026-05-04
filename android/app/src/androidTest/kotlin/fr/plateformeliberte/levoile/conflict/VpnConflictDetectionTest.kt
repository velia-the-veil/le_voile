package fr.plateformeliberte.levoile.conflict

import androidx.test.core.app.ActivityScenario
import androidx.test.espresso.intent.Intents
import androidx.test.ext.junit.runners.AndroidJUnit4
import fr.plateformeliberte.levoile.MainActivity
import fr.plateformeliberte.levoile.testutils.LeVoileTestRule
import org.junit.After
import org.junit.Before
import org.junit.Ignore
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Story 12.6 — test instrumenté du flow VpnConflictDetector (Story 10.3).
 *
 * Quand un autre VPN est actif (`VpnService.prepare()` retourne un Intent),
 * l'UI doit afficher un message explicite et `LeVoileVpnService` ne doit
 * JAMAIS démarrer. Cohérent epics.md l. 2219-2223.
 *
 * **TODO ajustement runtime** :
 *  - Mock `VpnService.prepare()` via une abstraction `VpnPreparer` injectable
 *    (Story 10.3 livre déjà une factorisation). En l'absence d'un autre VPN
 *    réel (impossible sur émulateur CI sans Google Play), le mock retourne
 *    un Intent fake (cf. `MockVpnServicePrepareReturnsIntent`).
 *  - Vérifier l'élément HTML `vpn-conflict-banner` côté frontend (sync-frontend.sh
 *    Story 11.1) ou les strings `R.string.android_vpn_conflict_*`.
 *  - `Intents.intended(IntentMatchers.hasAction(Settings.ACTION_VPN_SETTINGS))`
 *    pour vérifier que le bouton « Ouvrir les paramètres VPN » lance le bon Intent.
 */
@RunWith(AndroidJUnit4::class)
class VpnConflictDetectionTest {

    @get:Rule
    val leVoileRule = LeVoileTestRule()

    @Before
    fun setup() {
        Intents.init()
    }

    @After
    fun teardown() {
        Intents.release()
    }

    @Ignore("Story 12.6 scaffold — runtime impl pending Phase 2 (cf. Code Review 2026-05-03 — MockVpnServicePrepareReturnsIntent injection wiring requise)")
    @Test
    fun `autre_VPN_actif_affiche_message_UI_explicite_et_LeVoileVpnService_jamais_demarre`() {
        // TODO Story 12.6 — impl runtime :
        //  - MockVpnServicePrepareReturnsIntent.install() (override VpnPreparer).
        //  - Espresso.onWebView().withElement(DriverAtoms.findElement(Locator.ID, "vpn-conflict-banner"))
        //      .check(WebViewAssertions.webMatches(getText(), containsString("Un autre VPN est actif")))
        //  - tap btn-open-vpn-settings → Intents.intended(IntentMatchers.hasAction(Settings.ACTION_VPN_SETTINGS))
        //  - assertFalse(LeVoileVpnService.isRunning(applicationContext))

        ActivityScenario.launch(MainActivity::class.java).use {
            // Sanity check : Intents.init() est actif et l'activité est lancée.
        }
    }
}
