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
 * Story 12.6 (b) — consent VpnService accordé → `LeVoileVpnService` démarre,
 * tunnel ouvert, interface TUN active.
 *
 * Cohérent epics.md l. 2210.
 *
 * **TODO ajustement runtime** :
 *  1. Pré-grant le consent VPN. Sur émulateur, `adb shell appops set
 *     fr.plateformeliberte.levoile.debug ACTIVATE_VPN allow` peut suffire.
 *     Dans la matrice CI, soit on l'effectue via un script setup, soit on
 *     skip ce test si pas de pre-consent (Assume.assumeTrue) — la matrice
 *     n'a pas vocation à tester le flow consent réel ici (couvert par
 *     VpnServiceConsentTest a).
 *  2. Mock RelayRegistry pour qu'un relais "DE-001" soit accessible (sinon
 *     le tunnel échoue à se connecter — pas le scope de ce test).
 *  3. Vérifier l'état CONNECTED via JS bridge `LeVoileBridge.getStatus()` et
 *     un IdlingResource. Pattern : enregistrer un `IdlingResource` qui
 *     observe `LeVoileVpnService.instance != null`.
 */
@RunWith(AndroidJUnit4::class)
class VpnTunnelStartupTest {

    @get:Rule
    val leVoileRule = LeVoileTestRule()

    @Ignore("Story 12.6 scaffold — runtime impl pending Phase 2 (cf. Code Review 2026-05-03)")
    @Test
    fun `consent_OK_demarre_LeVoileVpnService_avec_TUN_active`() {
        ActivityScenario.launch(MainActivity::class.java).use {
            // TODO Story 12.6 — impl runtime :
            //  1. grantVpnConsent() via adb shell appops (setup CI ou test rule).
            //  2. tapConnect() via Espresso.onWebView() sur btn-connect HTML id.
            //  3. awaitState(VpnState.CONNECTED, timeout = 10.seconds) via IdlingResource.
            //  4. Assert ConnectivityManager.getNetworkCapabilities(activeNetwork)
            //     hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true.
            //
            // Sans relais réel mockable côté gomobile, le test reste structurel —
            // il vérifie que la chaîne d'activation s'enclenche jusqu'à
            // `LeVoileVpnService.onStartCommand`. La validation tunnel-réel
            // est laissée au smoke-test mainteneur sur device physique connecté
            // à un relais DE/ES/GB/US réel.
        }
    }
}
