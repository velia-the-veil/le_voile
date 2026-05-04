package fr.plateformeliberte.levoile.scenarios

import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import fr.plateformeliberte.levoile.MainActivity
import fr.plateformeliberte.levoile.testutils.LeVoileTestRule
import org.junit.Ignore
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Story 12.6 (e) — UI Story 11.x → tap « Connect » → JS bridge invoque
 * `LeVoileBridge.connect()` → état CONNECTED ; tap « Disconnect » →
 * état DISCONNECTED.
 *
 * Cohérent epics.md l. 2217.
 *
 * **TODO ajustement runtime** :
 *  - Vérifier les IDs HTML dans le frontend desktop-shared (sync-frontend.sh
 *    Story 11.1) — `btn-connect`, `btn-disconnect` (selon `frontend/` racine).
 *  - Pattern : `Espresso.onWebView().withElement(DriverAtoms.findElement(Locator.ID, "btn-connect")).perform(DriverAtoms.webClick())`.
 *  - IdlingResource pour `awaitState(CONNECTED, 10.seconds)` (cf. Story 12.5
 *    UpdateChecker pattern : observer un état via getStatus() polling).
 *  - Comme `VpnTunnelStartupTest`, ce test exige soit un grant consent
 *    pré-fait, soit un mock `VpnService.prepare()`. Sinon il bloque sur
 *    le dialog system.
 */
@RunWith(AndroidJUnit4::class)
class JsBridgeConnectDisconnectTest {

    @get:Rule
    val leVoileRule = LeVoileTestRule()

    @Ignore("Story 12.6 scaffold — runtime impl pending Phase 2 (cf. Code Review 2026-05-03)")
    @Test
    fun `tap_connect_via_WebView_ouvre_le_tunnel_et_disconnect_le_ferme`() {
        ActivityScenario.launch(MainActivity::class.java).use {
            // TODO Story 12.6 — impl runtime :
            //  grantVpnConsent()
            //  Espresso.onWebView().withElement(DriverAtoms.findElement(Locator.ID, "btn-connect")).perform(DriverAtoms.webClick())
            //  awaitState(VpnState.CONNECTED, 10.seconds)
            //  Espresso.onWebView().withElement(DriverAtoms.findElement(Locator.ID, "btn-disconnect")).perform(DriverAtoms.webClick())
            //  awaitState(VpnState.DISCONNECTED, 10.seconds)
        }
    }
}
