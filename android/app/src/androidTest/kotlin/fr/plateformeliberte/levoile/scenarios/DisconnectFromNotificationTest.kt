package fr.plateformeliberte.levoile.scenarios

import androidx.test.ext.junit.runners.AndroidJUnit4
import fr.plateformeliberte.levoile.testutils.EmulatorAssumptions
import fr.plateformeliberte.levoile.testutils.LeVoileTestRule
import org.junit.Ignore
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Story 12.6 (h) — notification C16 affichée → tap action « Déconnecter »
 * → tunnel fermé + `LeVoileVpnService` arrêté.
 *
 * Cohérent epics.md l. 2221.
 *
 * **TODO ajustement runtime** :
 *  - Pré-condition : tunnel ouvert + notification C16 visible (cf.
 *    PersistentNotificationTest).
 *  - `device.findObject(By.text("Déconnecter")).click()` → vérifier
 *    `LeVoileVpnService.instance == null` (ou awaitState(DISCONNECTED)).
 *  - Le label "Déconnecter" est dans `R.string.notif_action_disconnect`
 *    (FR — values/ et values-fr/ identiques en MVP mono-langue Story 9.6).
 */
@RunWith(AndroidJUnit4::class)
class DisconnectFromNotificationTest {

    @get:Rule
    val leVoileRule = LeVoileTestRule()

    @Ignore("Story 12.6 scaffold — runtime impl pending Phase 2 (cf. Code Review 2026-05-03)")
    @Test
    fun `tap_action_Deconnecter_de_la_notif_ferme_le_tunnel`() {
        EmulatorAssumptions.assumePostNotificationsAvailable()

        // TODO Story 12.6 — impl runtime :
        //  - grantVpnConsent() + tapConnect()
        //  - awaitState(CONNECTED, 10.seconds)
        //  - val device = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
        //  - device.openNotification()
        //  - device.findObject(By.text("Déconnecter")).click()
        //  - awaitState(DISCONNECTED, 10.seconds)
        //  - assertNull(LeVoileVpnService.instance)
    }
}
