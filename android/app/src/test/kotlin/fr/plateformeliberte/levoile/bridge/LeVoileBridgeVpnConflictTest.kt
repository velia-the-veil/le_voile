package fr.plateformeliberte.levoile.bridge

import android.content.Context
import android.content.Intent
import fr.plateformeliberte.levoile.conflict.VpnConflictDetector
import fr.plateformeliberte.levoile.conflict.VpnConflictVerdict
import org.junit.Assert.assertEquals
import org.junit.Test
import org.mockito.Mockito

/**
 * Story 10.3 — tests JVM-only de [LeVoileBridge.checkVpnConflict].
 *
 * Le détecteur réel est mocké via Mockito ; on vérifie uniquement la
 * sérialisation JSON du verdict + le filtre de défense en profondeur sur
 * `foreign_app_id` (whitelist `[a-zA-Z0-9._]` + tronque 255 chars).
 *
 * Couverture critique sécurité : si Story 11.2 affiche brute la valeur
 * `foreign_app_id` côté frontend (sans escape), le filtre Kotlin est la
 * dernière ligne de défense contre une éventuelle injection / XSS — il
 * doit être testé.
 */
class LeVoileBridgeVpnConflictTest {

    private val noopContext: Context = Mockito.mock(Context::class.java)
    private val stubIntent: Intent = Intent("test.action.STUB")

    @Test
    fun `verdict NoConflict serialise en no_conflict`() {
        val bridge = bridgeFor(VpnConflictVerdict.NoConflict)
        assertEquals("""{"verdict":"no_conflict"}""", bridge.checkVpnConflict())
    }

    @Test
    fun `verdict ConsentNotGiven serialise en consent_required`() {
        val bridge = bridgeFor(VpnConflictVerdict.ConsentNotGiven(stubIntent))
        assertEquals("""{"verdict":"consent_required"}""", bridge.checkVpnConflict())
    }

    @Test
    fun `verdict ForeignVpnActive avec packageName valide est expose tel quel`() {
        val bridge = bridgeFor(VpnConflictVerdict.ForeignVpnActive("com.tailscale.ipn"))
        assertEquals(
            """{"verdict":"foreign_vpn_active","foreign_app_id":"com.tailscale.ipn"}""",
            bridge.checkVpnConflict(),
        )
    }

    @Test
    fun `detecteur null donne unverifiable`() {
        // Garde-fou : MainActivity n'a pas encore enregistre le detecteur,
        // ou le detecteur a ete pre-detruit. Le bridge doit retourner une
        // valeur stable consommable cote JS.
        val bridge = LeVoileBridge(noopContext, vpnConflictDetector = null)
        assertEquals("""{"verdict":"unverifiable"}""", bridge.checkVpnConflict())
    }

    @Test
    fun `verdict ForeignVpnActive avec foreignAppId null serialise vide`() {
        // Slot orphelin : prepare() != null mais Settings.Global vide.
        val bridge = bridgeFor(VpnConflictVerdict.ForeignVpnActive(null))
        assertEquals(
            """{"verdict":"foreign_vpn_active","foreign_app_id":""}""",
            bridge.checkVpnConflict(),
        )
    }

    @Test
    fun `verdict ForeignVpnActive avec caracteres injection est filtre par whitelist`() {
        // Defense en profondeur : si une app exotique a triche sur son
        // packageName, le bridge filtre tout ce qui n'est pas
        // [a-zA-Z0-9._] AVANT serialisation JSON. Story 11.2 pourrait
        // afficher cette valeur sans escape — ce filtre est la derniere
        // ligne de defense XSS.
        val malicious = """com.evil.app","extra":"<script>alert(1)</script>"""
        val bridge = bridgeFor(VpnConflictVerdict.ForeignVpnActive(malicious))
        // Resultat attendu : tout caractere non-[a-zA-Z0-9._] strippe.
        // "com.evil.app","extra":"<script>alert(1)</script>"
        // → "com.evil.appextrascriptalert1script"
        assertEquals(
            """{"verdict":"foreign_vpn_active","foreign_app_id":"com.evil.appextrascriptalert1script"}""",
            bridge.checkVpnConflict(),
        )
    }

    @Test
    fun `verdict ForeignVpnActive avec caracteres Unicode est strippe par whitelist ASCII`() {
        // AC #7 specifie `[a-zA-Z0-9._]` ASCII strict. Un packageName avec
        // lettres/chiffres Unicode (arabe, chinois, etc.) doit etre filtre
        // entierement — Java package names sont ASCII par spec, donc aucun
        // false-positive legitime. Garde-fou contre une eventuelle bypass
        // si une app exotique a triche sur son packageName via Unicode.
        val unicodeAppId = "com.لا.app"  // "com.لا.app"
        val bridge = bridgeFor(VpnConflictVerdict.ForeignVpnActive(unicodeAppId))
        // Les 2 caracteres arabes sont strippes ; reste "com..app".
        assertEquals(
            """{"verdict":"foreign_vpn_active","foreign_app_id":"com..app"}""",
            bridge.checkVpnConflict(),
        )
    }

    @Test
    fun `verdict ForeignVpnActive avec foreignAppId tres long est tronque a 255 chars`() {
        // Garde-fou DoS : un packageName monstrueux (>255 chars) ne doit pas
        // gonfler indefiniment le payload JSON. Le bridge tronque a 255.
        val longAppId = "a".repeat(500)
        val bridge = bridgeFor(VpnConflictVerdict.ForeignVpnActive(longAppId))
        val expected = "a".repeat(255)
        assertEquals(
            """{"verdict":"foreign_vpn_active","foreign_app_id":"$expected"}""",
            bridge.checkVpnConflict(),
        )
    }

    private fun bridgeFor(verdict: VpnConflictVerdict): LeVoileBridge {
        val detector = Mockito.mock(VpnConflictDetector::class.java)
        Mockito.`when`(detector.check()).thenReturn(verdict)
        return LeVoileBridge(
            context = noopContext,
            vpnConflictDetector = detector,
        )
    }
}
