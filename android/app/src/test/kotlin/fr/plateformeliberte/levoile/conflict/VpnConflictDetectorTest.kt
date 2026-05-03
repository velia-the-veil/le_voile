package fr.plateformeliberte.levoile.conflict

import android.content.Context
import android.content.Intent
import fr.plateformeliberte.levoile.kill.SettingsReader
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.mockito.Mockito

/**
 * Story 10.3 — tests unitaires JVM-only de [VpnConflictDetector].
 *
 * Stratégie : DI complète via [VpnPreparer] (introduit Story 10.3) +
 * [SettingsReader] (introduit Story 10.1). Pas de Robolectric, pas de
 * Mockito — cohérent avec NotificationHelperTest, MainActivityConfigTest,
 * KillSwitchDetectorTest. `MockContext` (SDK Android `android.test.mock`)
 * suffit car le détecteur ne déréférence pas le context dans `check()`
 * (il est passé seulement au `VpnPreparer` qui est lui-même fakeé en test).
 *
 * Couverture : matrice 5 cas T1-T5 de la story (AC #9).
 */
class VpnConflictDetectorTest {

    private val noopContext: Context = Mockito.mock(Context::class.java)
    private val stubIntent: Intent = Intent("test.action.STUB")

    @Test
    fun `T1 - prepare retourne null donne NoConflict`() {
        val detector = VpnConflictDetector(
            context = noopContext,
            settingsReader = FakeSettingsReader(pinnedApp = "doesnt.matter"),
            expectedAppId = "fr.plateformeliberte.levoile",
            preparer = FakeVpnPreparer(intentToReturn = null),
        )
        assertEquals(VpnConflictVerdict.NoConflict, detector.check())
    }

    @Test
    fun `T2 - prepare non-null et pinnedApp null donne ConsentNotGiven`() {
        val detector = VpnConflictDetector(
            context = noopContext,
            settingsReader = FakeSettingsReader(pinnedApp = null),
            expectedAppId = "fr.plateformeliberte.levoile",
            preparer = FakeVpnPreparer(intentToReturn = stubIntent),
        )
        val verdict = detector.check()
        assertTrue(
            "verdict=$verdict",
            verdict is VpnConflictVerdict.ConsentNotGiven,
        )
        assertEquals(stubIntent, (verdict as VpnConflictVerdict.ConsentNotGiven).prepareIntent)
    }

    @Test
    fun `T3 - prepare non-null et pinnedApp est expectedAppId donne ConsentNotGiven`() {
        val detector = VpnConflictDetector(
            context = noopContext,
            settingsReader = FakeSettingsReader(pinnedApp = "fr.plateformeliberte.levoile"),
            expectedAppId = "fr.plateformeliberte.levoile",
            preparer = FakeVpnPreparer(intentToReturn = stubIntent),
        )
        val verdict = detector.check()
        assertTrue(
            "verdict=$verdict",
            verdict is VpnConflictVerdict.ConsentNotGiven,
        )
    }

    @Test
    fun `T4 - prepare non-null et autre app pinnee donne ForeignVpnActive`() {
        val detector = VpnConflictDetector(
            context = noopContext,
            settingsReader = FakeSettingsReader(pinnedApp = "com.tailscale.ipn"),
            expectedAppId = "fr.plateformeliberte.levoile",
            preparer = FakeVpnPreparer(intentToReturn = stubIntent),
        )
        val verdict = detector.check()
        assertTrue(
            "verdict=$verdict",
            verdict is VpnConflictVerdict.ForeignVpnActive,
        )
        assertEquals(
            "com.tailscale.ipn",
            (verdict as VpnConflictVerdict.ForeignVpnActive).foreignAppId,
        )
    }

    @Test
    fun `T5 - settingsReader qui throw donne ConsentNotGiven (fallback prudent)`() {
        val detector = VpnConflictDetector(
            context = noopContext,
            settingsReader = object : SettingsReader {
                override fun getString(name: String): String? =
                    throw SecurityException("simulated denial on $name")
                override fun getInt(name: String, default: Int): Int = default
            },
            expectedAppId = "fr.plateformeliberte.levoile",
            preparer = FakeVpnPreparer(intentToReturn = stubIntent),
        )
        val verdict = detector.check()
        assertTrue(
            "fallback prudent attendu, verdict=$verdict",
            verdict is VpnConflictVerdict.ConsentNotGiven,
        )
    }

    private class FakeSettingsReader(
        private val pinnedApp: String?,
    ) : SettingsReader {
        override fun getString(name: String): String? =
            if (name == "always_on_vpn_app") pinnedApp else null
        override fun getInt(name: String, default: Int): Int = default
    }

    private class FakeVpnPreparer(
        private val intentToReturn: Intent?,
    ) : VpnPreparer {
        override fun prepare(context: Context): Intent? = intentToReturn
    }
}
