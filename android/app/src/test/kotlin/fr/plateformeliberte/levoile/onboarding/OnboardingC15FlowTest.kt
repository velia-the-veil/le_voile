package fr.plateformeliberte.levoile.onboarding

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 11.6 — vérifie la présence des strings c15_* dans les 2 locales et
 * que onboarding_screen_3.xml référence bien les nouveaux composants.
 */
class OnboardingC15FlowTest {

    @Test
    fun `strings c15 sont presentes parite FR`() {
        val xmlDefault = readResource("src/main/res/values/strings.xml")
        val xmlFr = readResource("src/main/res/values-fr/strings.xml")
        val expected = listOf(
            "c15_title", "c15_body", "c15_consequence",
            "c15_btn_open_settings", "c15_link_skip", "c15_verifying",
            "c15_skip_dialog_title", "c15_skip_dialog_body",
            "c15_skip_dialog_cancel", "c15_skip_dialog_confirm",
        )
        expected.forEach { key ->
            assertTrue("Key '$key' absente values/", xmlDefault.contains("name=\"$key\""))
            assertTrue("Key '$key' absente values-fr/", xmlFr.contains("name=\"$key\""))
        }
    }

    @Test
    fun `placeholder Story 11_5 supprime des strings`() {
        val xmlDefault = readResource("src/main/res/values/strings.xml")
        // Stories 11.5 et 11.6 livrées conjointement : le placeholder
        // `onboarding_screen3_title_placeholder` n'a jamais été commité.
        // Cette assertion valide qu'il n'apparaît jamais.
        assertFalse(
            "onboarding_screen3_title_placeholder ne doit pas exister (C15 directement livré)",
            xmlDefault.contains("onboarding_screen3_title_placeholder")
        )
    }

    /**
     * Code-review post-Epic 11 (H4) : invariant qui détecte la régression
     * « C15 retiré au profit du placeholder ». Si une story future revert
     * à un écran 3 minimaliste, ce test fail et signale la régression UX
     * (le composant C15 complet est l'état target Story 11.6).
     */
    @Test
    fun `c15 strings sont presentes preuve composant complet livre`() {
        val xmlDefault = readResource("src/main/res/values/strings.xml")
        // Les 3 strings clés du composant C15 (vs placeholder Story 11.5).
        listOf("c15_title", "c15_link_skip", "c15_consequence").forEach { key ->
            assertTrue(
                "Key '$key' absente — regression vers placeholder 11.5 ?",
                xmlDefault.contains("name=\"$key\"")
            )
        }
    }

    @Test
    fun `layout onboarding_screen_3 reference c15 strings`() {
        val xml = readResource("src/main/res/layout/onboarding_screen_3.xml")
        assertTrue(xml.contains("@string/c15_title"))
        assertTrue(xml.contains("@string/c15_link_skip"))
        assertTrue(xml.contains("@drawable/ic_warning_orange"))
    }

    /**
     * Code-review post-Epic 11 (L2) : vérifie que dialog_skip_killswitch.xml
     * contient bien les boutons attendus par OnboardingActivity.showSkipConfirmationDialog.
     */
    @Test
    fun `dialog_skip_killswitch contient boutons cancel et confirm`() {
        val xml = readResource("src/main/res/layout/dialog_skip_killswitch.xml")
        assertTrue(
            "dialog_skip_killswitch.xml doit contenir bouton dialog_skip_cancel",
            xml.contains("@+id/dialog_skip_cancel")
        )
        assertTrue(
            "dialog_skip_killswitch.xml doit contenir bouton dialog_skip_confirm",
            xml.contains("@+id/dialog_skip_confirm")
        )
        assertTrue(
            "dialog_skip_killswitch.xml doit referencer c15_skip_dialog_title",
            xml.contains("@string/c15_skip_dialog_title")
        )
        assertTrue(
            "dialog_skip_killswitch.xml doit referencer c15_skip_dialog_body",
            xml.contains("@string/c15_skip_dialog_body")
        )
    }

    private fun readResource(path: String): String {
        val candidates = listOf(File(path), File("app/$path"), File("android/app/$path"))
        return candidates.firstOrNull { it.exists() }?.readText()
            ?: throw AssertionError("$path introuvable")
    }
}
