package fr.plateformeliberte.levoile.bridge

import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Story 11.3 — test structural pour openAppDetailsSettings.
 *
 * Cohérent pattern Story 10.2 (LeVoileBridgeKillSwitchTest) :
 * `Intent.startActivity` est intestable JVM-only sans Robolectric (le
 * constructeur `Intent(String)` est stubbé par android.jar et retourne null,
 * d'où NPE sur `.setData(...)` immédiatement après).
 *
 * Code-review post-Epic 11 (M2) : tentative de tests fonctionnels Mockito
 * abandonnée — le pattern structurel était la bonne décision Story 10.2.
 * On vérifie l'API surface (annotation + return type) ; couverture
 * comportementale réelle = Story 12.6 (Espresso instrumenté).
 */
class LeVoileBridgeAppDetailsTest {

    @Test
    fun `openAppDetailsSettings est annotee JavascriptInterface`() {
        val method = LeVoileBridge::class.java.getDeclaredMethod("openAppDetailsSettings")
        assertNotNull(
            "AC #3 Story 11.3 — openAppDetailsSettings DOIT etre annotee @android.webkit.JavascriptInterface",
            method.getAnnotation(android.webkit.JavascriptInterface::class.java),
        )
    }

    @Test
    fun `openAppDetailsSettings retourne String`() {
        val method = LeVoileBridge::class.java.getDeclaredMethod("openAppDetailsSettings")
        assertNotNull(method.returnType)
        assertTrue(
            "openAppDetailsSettings doit retourner String (cohérent contrat JSON bridge)",
            method.returnType == String::class.java,
        )
    }
}
