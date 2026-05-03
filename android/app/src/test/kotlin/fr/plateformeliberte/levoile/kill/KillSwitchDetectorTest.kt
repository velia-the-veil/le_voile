package fr.plateformeliberte.levoile.kill

import androidx.arch.core.executor.testing.InstantTaskExecutorRule
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test

/**
 * Story 10.1 — tests unitaires JVM-only de [KillSwitchDetector].
 *
 * Stratégie : DI via [SettingsReader] (interface introduite Story 10.1) +
 * `InstantTaskExecutorRule` pour forcer `LiveData.postValue` synchrone.
 * Pas de Robolectric, pas de Mockito — cohérent avec la stratégie Story 9.4
 * (`testOptions.unitTests.isReturnDefaultValues = true`) et MainActivityConfigTest.
 * Le constructeur `internal` injecte `reader` + `expectedAppId` directement,
 * donc aucun `Context` Android n'est nécessaire en JVM-only.
 *
 * Couverture : matrice 5 cas de la story (T1-T5 dans Tasks/Subtasks
 * Task 7) + 2 tests garde-fous (état initial, exception reader).
 */
class KillSwitchDetectorTest {

    @get:Rule
    val instantRule = InstantTaskExecutorRule()

    @Test
    fun `T1 - Le Voile pinne et lockdown actif rend Active (release)`() {
        val detector = KillSwitchDetector(
            reader = FakeSettingsReader(
                pinnedApp = "fr.plateformeliberte.levoile",
                lockdown = 1,
            ),
            expectedAppId = "fr.plateformeliberte.levoile",
        )
        detector.refresh()
        assertEquals(KillSwitchStatus.Active, detector.status.value)
    }

    @Test
    fun `T2 - Le Voile pinne mais lockdown off rend Inactive`() {
        val detector = KillSwitchDetector(
            reader = FakeSettingsReader(
                pinnedApp = "fr.plateformeliberte.levoile",
                lockdown = 0,
            ),
            expectedAppId = "fr.plateformeliberte.levoile",
        )
        detector.refresh()
        assertEquals(KillSwitchStatus.Inactive, detector.status.value)
    }

    @Test
    fun `T3 - autre app VPN tient le slot rend Inactive`() {
        val detector = KillSwitchDetector(
            reader = FakeSettingsReader(
                pinnedApp = "com.tailscale.ipn",
                lockdown = 1,
            ),
            expectedAppId = "fr.plateformeliberte.levoile",
        )
        detector.refresh()
        assertEquals(KillSwitchStatus.Inactive, detector.status.value)
    }

    @Test
    fun `T4 - aucun VPN permanent configure rend Inactive`() {
        val detector = KillSwitchDetector(
            reader = FakeSettingsReader(
                pinnedApp = null,
                lockdown = 0,
            ),
            expectedAppId = "fr.plateformeliberte.levoile",
        )
        detector.refresh()
        assertEquals(KillSwitchStatus.Inactive, detector.status.value)
    }

    @Test
    fun `T5 - Le Voile debug pinne et lockdown actif rend Active (debug)`() {
        // Build flavor debug : applicationIdSuffix = ".debug" => BuildConfig.APPLICATION_ID
        // == "fr.plateformeliberte.levoile.debug". On override expectedAppId
        // via le constructeur internal plutot que de dependre du flavor effectif
        // du runtime test.
        val detector = KillSwitchDetector(
            reader = FakeSettingsReader(
                pinnedApp = "fr.plateformeliberte.levoile.debug",
                lockdown = 1,
            ),
            expectedAppId = "fr.plateformeliberte.levoile.debug",
        )
        detector.refresh()
        assertEquals(KillSwitchStatus.Active, detector.status.value)
    }

    @Test
    fun `Initial status est Unverifiable avant refresh`() {
        // Garde-fou pour le contrat « etat prudent par defaut » (cf. AC #1
        // de Story 10.1) — sans refresh, l'observer recoit Unverifiable et
        // non null / Inactive.
        val detector = KillSwitchDetector(
            reader = FakeSettingsReader(pinnedApp = null, lockdown = 0),
            expectedAppId = "fr.plateformeliberte.levoile",
        )
        assertEquals(KillSwitchStatus.Unverifiable, detector.status.value)
    }

    @Test
    fun `Reader getString qui throw rend Unverifiable`() {
        // Cas heuristique cassee sur la 1re lecture : ROM custom ou
        // Settings.Global rejette les acces a always_on_vpn_app
        // (futur Android _PROTECTED_NAMESPACES). L'AC #3 garantit le
        // fallback Unverifiable + log sans donnee utilisateur.
        val detector = KillSwitchDetector(
            reader = object : SettingsReader {
                override fun getString(name: String): String? =
                    throw SecurityException("simulated denial on $name")
                override fun getInt(name: String, default: Int): Int = default
            },
            expectedAppId = "fr.plateformeliberte.levoile",
        )
        detector.refresh()
        assertEquals(KillSwitchStatus.Unverifiable, detector.status.value)
    }

    @Test
    fun `Reader getInt qui throw rend Unverifiable`() {
        // Cas heuristique cassee uniquement sur la 2e lecture (always_on_vpn_lockdown).
        // Garde-fou : la 1re branche try / catch a deja reussi, mais la 2nde leve
        // une SecurityException. L'AC #3 garantit que le second early-return
        // produit aussi Unverifiable (et non Active / Inactive avec une valeur
        // par defaut potentiellement fausse).
        val detector = KillSwitchDetector(
            reader = object : SettingsReader {
                override fun getString(name: String): String? =
                    "fr.plateformeliberte.levoile"
                override fun getInt(name: String, default: Int): Int =
                    throw SecurityException("simulated denial on $name")
            },
            expectedAppId = "fr.plateformeliberte.levoile",
        )
        detector.refresh()
        assertEquals(KillSwitchStatus.Unverifiable, detector.status.value)
    }

    private class FakeSettingsReader(
        private val pinnedApp: String?,
        private val lockdown: Int,
    ) : SettingsReader {
        override fun getString(name: String): String? =
            if (name == "always_on_vpn_app") pinnedApp else null

        override fun getInt(name: String, default: Int): Int =
            if (name == "always_on_vpn_lockdown") lockdown else default
    }
}
