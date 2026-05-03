package fr.plateformeliberte.levoile.bridge

import android.content.Context
import androidx.lifecycle.LiveData
import fr.plateformeliberte.levoile.kill.KillSwitchDetector
import fr.plateformeliberte.levoile.kill.KillSwitchStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.mockito.Mock
import org.mockito.Mockito.`when`
import org.mockito.junit.MockitoJUnitRunner

/**
 * Story 11.2 — tests JVM-only consolidés pour les 5 méthodes bridge :
 *   connect / disconnect / selectCountry / getStatus / whitelist.
 *
 * Le mockContext n'est PAS castable en MainActivity → branche fallback testée
 * pour connect/disconnect (cas idéal pour tests JVM purs).
 *
 * selectCountry est testé en présence d'erreur de validation seulement (la
 * persistence ConfigStore réelle nécessite un Context Android vrai —
 * couvert par ConfigStoreTest qui utilise un tempDir).
 */
@RunWith(MockitoJUnitRunner::class)
class LeVoileBridgeFullApiTest {
    @Mock private lateinit var mockContext: Context
    @Mock private lateinit var mockKillDetector: KillSwitchDetector
    @Mock private lateinit var mockKillStatusLD: LiveData<KillSwitchStatus>

    private lateinit var bridge: LeVoileBridge

    @Before
    fun setUp() {
        `when`(mockKillDetector.status).thenReturn(mockKillStatusLD)
        `when`(mockKillStatusLD.value).thenReturn(KillSwitchStatus.Unverifiable)
        bridge = LeVoileBridge(mockContext, mockKillDetector, vpnConflictDetector = null)
    }

    // === connect ===
    @Test
    fun `connect en contexte non-Activity retourne erreur context_not_activity`() {
        val r = bridge.connect("DE")
        assertTrue(r.contains("\"error\":\"context_not_activity\""))
    }

    @Test
    fun `connect avec country invalide retourne erreur invalid_country_code`() {
        val r = bridge.connect("FR")
        assertTrue(r.contains("\"error\":\"invalid_country_code\""))
    }

    @Test
    fun `connect avec country injection refuse et tronque value a 8 chars`() {
        val r = bridge.connect("DE; DROP TABLE")
        assertTrue(r.contains("\"error\":\"invalid_country_code\""))
        // value tronquée à 8 chars : "DE; DROP" (le " TABLE" est coupé).
        // L'assertion originale de la spec story était fausse — la borne 8 chars
        // englobe "DROP" mais pas "TABLE". On valide le truncate sur "TABLE".
        assertFalse(
            "value doit etre tronquee — TABLE ne doit pas apparaitre (coupe au 8e char)",
            r.contains("TABLE")
        )
    }

    @Test
    fun `connect avec country null retourne erreur context puisque pas Activity`() {
        // Country null est valide, mais sans Activity la branche fallback gagne.
        val r = bridge.connect(null)
        assertTrue(r.contains("\"error\":\"context_not_activity\""))
    }

    // === disconnect ===
    @Test
    fun `disconnect en contexte non-Activity retourne erreur context_not_activity`() {
        val r = bridge.disconnect()
        assertTrue(r.contains("\"error\":\"context_not_activity\""))
    }

    // === selectCountry ===
    @Test
    fun `selectCountry avec country invalide retourne erreur sans persister`() {
        val r = bridge.selectCountry("FR")
        assertTrue(r.contains("\"error\":\"invalid_country_code\""))
    }

    @Test
    fun `selectCountry avec null retourne erreur invalid_country_code`() {
        val r = bridge.selectCountry(null)
        assertTrue(r.contains("\"error\":\"invalid_country_code\""))
    }

    // === getStatus ===
    @Test
    fun `getStatus sans service actif retourne disconnected`() {
        val r = bridge.getStatus()
        assertTrue(r.contains("\"state\":\"disconnected\""))
        assertTrue(r.contains("\"platform\":\"android\""))
        assertTrue(r.contains("\"killSwitchStatus\":\"Unverifiable\""))
    }

    @Test
    fun `getStatus avec killSwitch Active reflete le statut`() {
        `when`(mockKillStatusLD.value).thenReturn(KillSwitchStatus.Active)
        val r = bridge.getStatus()
        assertTrue(r.contains("\"killSwitchStatus\":\"Active\""))
    }

    @Test
    fun `getStatus avec killSwitch Inactive reflete le statut`() {
        `when`(mockKillStatusLD.value).thenReturn(KillSwitchStatus.Inactive)
        val r = bridge.getStatus()
        assertTrue(r.contains("\"killSwitchStatus\":\"Inactive\""))
    }

    @Test
    fun `getStatus retour reste sous 4 Ko`() {
        val r = bridge.getStatus()
        assertTrue("Retour > 4 Ko (FR-AND-5)", r.length < 4096)
    }

    @Test
    fun `getStatus contient version`() {
        val r = bridge.getStatus()
        assertTrue(r.contains("\"version\":\"${LeVoileBridge.VERSION}\""))
    }

    // === Whitelist ===
    @Test
    fun `COUNTRIES_WHITELIST contient les 4 pays MVP`() {
        assertEquals(setOf("DE", "ES", "GB", "US"), LeVoileBridge.COUNTRIES_WHITELIST)
    }

    @Test
    fun `PREFS_NAME aligne OnboardingActivity scope app`() {
        assertEquals("levoile_prefs", LeVoileBridge.PREFS_NAME)
    }

    // === STATUS_JSON legacy ===
    @Test
    @Suppress("DEPRECATION")
    fun `STATUS_JSON legacy preserve pour retro-compat`() {
        // Story 11.2 a marqué ce constant @Deprecated mais le conserve pour les tests
        // legacy Story 9.3 qui asserteraient sur le JSON figé.
        assertNotNull(LeVoileBridge.STATUS_JSON)
        assertTrue(LeVoileBridge.STATUS_JSON.contains("placeholder"))
    }
}
