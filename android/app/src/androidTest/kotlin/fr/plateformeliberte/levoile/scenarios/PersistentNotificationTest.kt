package fr.plateformeliberte.levoile.scenarios

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.UiDevice
import fr.plateformeliberte.levoile.testutils.EmulatorAssumptions
import fr.plateformeliberte.levoile.testutils.LeVoileTestRule
import org.junit.Ignore
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Story 12.6 (g) — tunnel ouvert vers DE-001 → notification C16 affichée
 * (Story 11.7) avec « Allemagne · X.X.X.X » → IP correcte (mockée par
 * test fixture).
 *
 * Cohérent epics.md l. 2220.
 *
 * **TODO ajustement runtime** :
 *  - Mock `currentIp()` via injection dans `NotificationHelper` (Story 11.7)
 *    ou via une `LeVoileVpnService.testHook()` qui force la valeur.
 *  - `device.openNotification()` ouvre le shade.
 *  - `device.wait(Until.hasObject(By.text("Allemagne · 203.0.113.42")), 5_000)`
 *    — utiliser RFC 5737 reserved-for-doc IPs (203.0.113.42, 198.51.100.0/24).
 *  - POST_NOTIFICATIONS : sur API 33+, l'utilisatrice doit accorder
 *    explicitement la permission. Pour CI, soit grant via
 *    `adb shell pm grant fr.plateformeliberte.levoile.debug
 *     android.permission.POST_NOTIFICATIONS`, soit Assume.assumeFalse
 *     sur API < 33 où la permission n'existe pas.
 */
@RunWith(AndroidJUnit4::class)
class PersistentNotificationTest {

    @get:Rule
    val leVoileRule = LeVoileTestRule()

    @Ignore("Story 12.6 scaffold — runtime impl pending Phase 2 (cf. Code Review 2026-05-03)")
    @Test
    fun `tunnel_ouvert_affiche_notification_persistante_avec_pays_et_IP`() {
        EmulatorAssumptions.assumePostNotificationsAvailable()

        // TODO Story 12.6 — impl runtime :
        //  - mockCurrentIp("203.0.113.42")  // RFC 5737 reserved-for-doc.
        //  - grantVpnConsent() + tapConnect("DE")
        //  - awaitState(CONNECTED, 10.seconds)
        //  - val device = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
        //  - device.openNotification()
        //  - device.wait(Until.hasObject(By.textContains("203.0.113.42")), 5_000)
        //
        // Pour MVP, le test reste structurel — la logique de format
        // « Allemagne · X.X.X.X » est testée par NotificationHelperEnrichedTest
        // (JVM-only Story 11.7).
        val device = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
        device.pressHome()  // sanity check uiautomator works
    }
}
