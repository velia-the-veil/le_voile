package fr.plateformeliberte.levoile.bridge

import android.content.Context
import androidx.arch.core.executor.testing.InstantTaskExecutorRule
import fr.plateformeliberte.levoile.kill.KillSwitchDetector
import fr.plateformeliberte.levoile.kill.KillSwitchStatus
import fr.plateformeliberte.levoile.kill.SettingsReader
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.mockito.Mockito
import java.lang.reflect.Modifier

/**
 * Story 10.2 — tests JVM-only de [LeVoileBridge.getKillSwitchStatus].
 *
 * Stratégie : DI via le constructeur `internal` de [KillSwitchDetector]
 * (Story 10.1) — on injecte un [SettingsReader] paramétrable et on
 * déclenche `refresh()` pour fixer la valeur courante du LiveData.
 * Pas de Robolectric, pas de Mockito (cohérent NotificationHelperTest +
 * MainActivityConfigTest).
 *
 * **`openKillSwitchTarget()` n'est pas testée** : `Intent.startActivity`
 * est intestable JVM-only sans Robolectric, et le code est trivial. La
 * couverture instrumentée est portée par Story 12.6 (Espresso).
 *
 * Couverture : T1-T4 de la matrice AC #10 + bonus annotations bridge.
 */
class LeVoileBridgeKillSwitchTest {

    @get:Rule
    val instantRule = InstantTaskExecutorRule()

    @Test
    fun `T1 - detector null retourne Unverifiable`() {
        // Cas defensive : MainActivity n'a pas encore enregistre le detecteur,
        // ou le detecteur a ete pre-detruit. Le bridge doit retourner Unverifiable
        // (etat prudent par defaut, coherent Story 10.1 init).
        val bridge = LeVoileBridge(
            context = noopContext(),
            killSwitchDetector = null,
        )
        assertEquals("Unverifiable", bridge.getKillSwitchStatus())
    }

    @Test
    fun `T2 - status Active retourne Active string`() {
        val detector = detectorWith(KillSwitchStatus.Active)
        val bridge = LeVoileBridge(noopContext(), detector)
        assertEquals("Active", bridge.getKillSwitchStatus())
    }

    @Test
    fun `T3 - status Inactive retourne Inactive string`() {
        val detector = detectorWith(KillSwitchStatus.Inactive)
        val bridge = LeVoileBridge(noopContext(), detector)
        assertEquals("Inactive", bridge.getKillSwitchStatus())
    }

    @Test
    fun `T4 - status Unverifiable retourne Unverifiable string`() {
        val detector = detectorWith(KillSwitchStatus.Unverifiable)
        val bridge = LeVoileBridge(noopContext(), detector)
        assertEquals("Unverifiable", bridge.getKillSwitchStatus())
    }

    @Test
    fun `getKillSwitchStatus est annotee JavascriptInterface`() {
        // Garde-fou : retirer @JavascriptInterface par megarde casserait
        // l'expose au WebView sans que le compilateur ne s'en plaigne. AC #6
        // exige l'annotation explicitement.
        val method = LeVoileBridge::class.java.getDeclaredMethod("getKillSwitchStatus")
        assertNotNull(
            "AC #6 — getKillSwitchStatus DOIT etre annotee @android.webkit.JavascriptInterface",
            method.getAnnotation(android.webkit.JavascriptInterface::class.java),
        )
    }

    @Test
    fun `openKillSwitchTarget est annotee JavascriptInterface`() {
        val method = LeVoileBridge::class.java.getDeclaredMethod("openKillSwitchTarget")
        assertNotNull(
            "AC #5 — openKillSwitchTarget DOIT etre annotee @android.webkit.JavascriptInterface",
            method.getAnnotation(android.webkit.JavascriptInterface::class.java),
        )
        // La methode doit etre publique au niveau bytecode pour etre exposee
        // a WebView (sinon @JavascriptInterface est silencieusement ignoree).
        assertTrue(
            "openKillSwitchTarget doit etre public au niveau bytecode",
            Modifier.isPublic(method.modifiers),
        )
    }

    // ---------- Helpers ----------

    /**
     * Crée un [KillSwitchDetector] configuré pour exposer le statut donné.
     * On utilise le constructeur `internal` de Story 10.1 + un
     * [SettingsReader] qui répond pour produire le bon résultat dans
     * `refresh()` :
     *  - Active : pinnedApp == expectedAppId + lockdown=1
     *  - Inactive : pinnedApp=null + lockdown=0
     *  - Unverifiable : SettingsReader throw → fallback Story 10.1 AC #3
     */
    private fun detectorWith(target: KillSwitchStatus): KillSwitchDetector {
        val detector = KillSwitchDetector(
            reader = readerFor(target),
            expectedAppId = "fr.plateformeliberte.levoile",
        )
        detector.refresh()
        return detector
    }

    private fun readerFor(target: KillSwitchStatus): SettingsReader = when (target) {
        is KillSwitchStatus.Active -> object : SettingsReader {
            override fun getString(name: String): String? = "fr.plateformeliberte.levoile"
            override fun getInt(name: String, default: Int): Int = 1
        }
        is KillSwitchStatus.Inactive -> object : SettingsReader {
            override fun getString(name: String): String? = null
            override fun getInt(name: String, default: Int): Int = 0
        }
        is KillSwitchStatus.Unverifiable -> object : SettingsReader {
            override fun getString(name: String): String? =
                throw SecurityException("simulated denial")
            override fun getInt(name: String, default: Int): Int = default
        }
    }

    /**
     * Context noop pour les tests T1-T4 : `getKillSwitchStatus()` ne touche
     * jamais au context, donc un mock Mockito vide suffit. `MockContext` de
     * `android.test.mock` throw "Stub!" au constructor en JVM-only —
     * Mockito est la voie standard Android pour cette situation. Si un test
     * futur veut exercer `openKillSwitchTarget` (qui consomme le context),
     * passer en Robolectric ou en test instrumente Espresso (Story 12.6).
     */
    private fun noopContext(): Context = Mockito.mock(Context::class.java)
}
