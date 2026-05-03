package fr.plateformeliberte.levoile.ui

import android.content.Context
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.mockito.Mockito.mock

/**
 * Story 11.7 — tests JVM-only pour CountryDisplay et helpers internes
 * NotificationHelper (spellOutDigits).
 *
 * NotificationHelper.build complet nécessite un Context Android réel
 * (NotificationCompat + ContextCompat.getColor) — couvert Story 12.6 Espresso.
 * Code-review post-Epic 11 (L3) : ajout coverage spellOutDigits qui était
 * testable JVM via visibilité internal.
 */
class NotificationHelperEnrichedTest {

    private lateinit var helper: NotificationHelper

    @Before
    fun setUp() {
        // mockContext suffit pour instancier — spellOutDigits ne touche pas le Context.
        helper = NotificationHelper(mock(Context::class.java))
    }

    @Test
    fun `CountryDisplay lookup case insensitive`() {
        assertEquals("Allemagne", CountryDisplay.lookup("de")?.frenchName)
        assertEquals("Allemagne", CountryDisplay.lookup("DE")?.frenchName)
        assertNull(CountryDisplay.lookup("FR"))
    }

    @Test
    fun `CountryDisplay formatShort iso null retourne tiret`() {
        assertEquals("—", CountryDisplay.formatShort(null))
        assertEquals("—", CountryDisplay.formatShort("XX"))
    }

    @Test
    fun `CountryDisplay formatShort retourne drapeau et nom francais`() {
        assertEquals("🇩🇪 Allemagne", CountryDisplay.formatShort("DE"))
        assertEquals("🇪🇸 Espagne", CountryDisplay.formatShort("ES"))
        assertEquals("🇬🇧 Royaume-Uni", CountryDisplay.formatShort("GB"))
        assertEquals("🇺🇸 États-Unis", CountryDisplay.formatShort("US"))
    }

    @Test
    fun `CountryDisplay formatTalkBack retourne nom seul`() {
        assertEquals("Allemagne", CountryDisplay.formatTalkBack("DE"))
        assertEquals("pays inconnu", CountryDisplay.formatTalkBack(null))
    }

    @Test
    fun `CountryDisplay aligne LeVoileBridge whitelist`() {
        // Cohérence cross-fichier : Story 11.7 ajoute 4 pays, Story 11.2 en a 4.
        val isoSet = setOf("DE", "ES", "GB", "US")
        isoSet.forEach { iso ->
            assertTrue(
                "CountryDisplay doit connaitre $iso (whitelist Story 11.2)",
                CountryDisplay.lookup(iso) != null
            )
        }
    }

    // === spellOutDigits — coverage L3 (code-review post-Epic 11) ===

    @Test
    fun `spellOutDigits epele les chiffres et insere point`() {
        val out = helper.spellOutDigits("5.45.6.7")
        // Chaque chiffre + espace après. Les "." remplacés par " point ".
        assertTrue("doit contenir '5'", out.contains("5"))
        assertTrue("doit contenir '4'", out.contains("4"))
        assertTrue("doit contenir 'point'", out.contains("point"))
        // Pas de chiffres collés (cohérent NFR a11y — TalkBack épelle 1 par 1).
        assertFalse("ne doit pas contenir '45' colle", out.contains("45"))
    }

    @Test
    fun `spellOutDigits ignore les caracteres non numeriques sauf point`() {
        // « 5x6 » → ignore le 'x', garde 5 et 6 espacés.
        val out = helper.spellOutDigits("5x6")
        assertTrue(out.contains("5"))
        assertTrue(out.contains("6"))
        assertFalse("ne doit pas contenir 'x'", out.contains("x"))
    }

    @Test
    fun `spellOutDigits chaine vide retourne chaine vide`() {
        assertEquals("", helper.spellOutDigits(""))
    }
}
