package fr.plateformeliberte.levoile.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Document
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * Smoke test Story 9.6 — verifie la frontiere compile-time (NotificationHelper
 * + VpnState) et la coherence des ressources Android (strings.xml, drawable).
 *
 * Strategie : JVM-only, AUCUNE dependance Robolectric — coherent avec le pattern
 * deja en place (Story 9.3 MainActivityConfigTest, Story 9.4 LeVoileVpnServiceConfigTest).
 *
 * Pourquoi pas Robolectric :
 *   - testOptions.unitTests.isReturnDefaultValues = true (build.gradle.kts) stube
 *     les APIs Android (Log, Context.getString, NotificationManagerCompat...) en
 *     retournant null/0/false. Suffisant pour ce qu'on teste ICI.
 *   - Robolectric ajouterait ~25 MB de deps + un setup explicite.
 *   - Les tests Service runtime (notif effectivement postee, PendingIntents
 *     resolus, channel cree dans le NotificationManager systeme) appartiennent
 *     a Story 12.6 (matrice Espresso instrumentee API 29/33/34).
 *
 * Ce test couvre :
 *   - VpnState : enum a exactement 4 valeurs (CONNECTED, RECONNECTING,
 *     DISCONNECTED, ERROR) — le contrat utilise par NotificationHelper.statusLabel.
 *   - NotificationHelper : classe resolvable + methodes publiques attendues
 *     (ensureChannel, build, notify) avec signatures correctes.
 *   - strings.xml : 9 cles ajoutees Story 9.6 + 2 cles obsoletes supprimees +
 *     les memes cles presentes dans values-fr/strings.xml (parite EN/FR).
 *   - drawable/ic_levoile_status.xml : present, vector 24dp, path mono-couleur.
 *   - drawable/ic_notification_stub.xml : effectivement supprime (regression
 *     guard contre re-introduction accidentelle dans une story future).
 */
class NotificationHelperTest {

    // ---------- VpnState enum ----------

    @Test
    fun `VpnState enum exposes exactly the 4 expected states`() {
        val expected = setOf("CONNECTED", "RECONNECTING", "DISCONNECTED", "ERROR")
        val actual = VpnState.values().map { it.name }.toSet()
        assertEquals(
            "VpnState doit avoir exactement 4 valeurs et matcher le contrat Story 9.6",
            expected,
            actual
        )
    }

    // ---------- NotificationHelper class ----------

    @Test
    fun `NotificationHelper class is resolvable`() {
        val cls = Class.forName("fr.plateformeliberte.levoile.ui.NotificationHelper")
        assertNotNull(cls)
    }

    @Test
    fun `NotificationHelper exposes ensureChannel build notify with expected signatures`() {
        val cls = Class.forName("fr.plateformeliberte.levoile.ui.NotificationHelper")

        // ensureChannel(): Unit
        val ensureChannel = cls.declaredMethods.firstOrNull {
            it.name == "ensureChannel" && it.parameterTypes.isEmpty()
        }
        assertNotNull(
            "ensureChannel(): Unit doit exister",
            ensureChannel
        )

        // build(VpnState): Notification — on verifie param type, le return type
        // est android.app.Notification (stube par returnDefaultValues, on verifie
        // juste le nom de classe pour eviter de declencher un init Android).
        val build = cls.declaredMethods.firstOrNull {
            it.name == "build"
                && it.parameterTypes.size == 1
                && it.parameterTypes[0].name == "fr.plateformeliberte.levoile.ui.VpnState"
        }
        assertNotNull(
            "build(VpnState): Notification doit exister",
            build
        )
        assertEquals(
            "build doit retourner android.app.Notification",
            "android.app.Notification",
            build!!.returnType.name
        )

        // notify(VpnState): Unit — meme pattern.
        val notify = cls.declaredMethods.firstOrNull {
            it.name == "notify"
                && it.parameterTypes.size == 1
                && it.parameterTypes[0].name == "fr.plateformeliberte.levoile.ui.VpnState"
        }
        assertNotNull(
            "notify(VpnState): Unit doit exister",
            notify
        )
    }

    @Test
    fun `NotificationHelper has a single public constructor taking Context`() {
        val cls = Class.forName("fr.plateformeliberte.levoile.ui.NotificationHelper")
        val ctors = cls.declaredConstructors
        assertEquals(
            "NotificationHelper doit avoir un seul constructeur public",
            1,
            ctors.size
        )
        val ctor = ctors[0]
        assertEquals(
            "NotificationHelper(Context) — un seul parametre",
            1,
            ctor.parameterTypes.size
        )
        assertEquals(
            "Le parametre du constructeur doit etre android.content.Context",
            "android.content.Context",
            ctor.parameterTypes[0].name
        )
    }

    // ---------- strings.xml (defaults FR + values-fr/) ----------

    @Test
    fun `Default strings xml exposes the 9 Story 9_6 keys with non-empty values`() {
        val keys = parseStringResourceKeys(stringsXml(default = true))
        STORY_9_6_KEYS.forEach { key ->
            assertTrue(
                "strings.xml doit declarer <string name=\"$key\"> non-vide — actuelles : ${keys.keys}",
                keys[key]?.isNotBlank() == true
            )
        }
    }

    @Test
    fun `Default strings xml has dropped the 2 Story 9_4 obsolete keys`() {
        // Regression guard : si une refonte future re-introduit ces cles, elles
        // referenceraient un drawable et un texte morts.
        val keys = parseStringResourceKeys(stringsXml(default = true))
        OBSOLETE_KEYS.forEach { key ->
            assertFalse(
                "strings.xml ne doit PLUS declarer <string name=\"$key\"> (supprime Story 9.6)",
                keys.containsKey(key)
            )
        }
    }

    @Test
    fun `values-fr strings xml mirrors the same Story 9_6 keys`() {
        val keys = parseStringResourceKeys(stringsXml(default = false))
        STORY_9_6_KEYS.forEach { key ->
            assertTrue(
                "values-fr/strings.xml doit declarer <string name=\"$key\"> non-vide " +
                    "(parite EN/FR — actuelles : ${keys.keys})",
                keys[key]?.isNotBlank() == true
            )
        }
        OBSOLETE_KEYS.forEach { key ->
            assertFalse(
                "values-fr/strings.xml ne doit PLUS declarer <string name=\"$key\">",
                keys.containsKey(key)
            )
        }
    }

    @Test
    fun `notif_title_prefix is exactly Le Voile in both locales`() {
        // Le format final affiche par NotificationHelper.build est :
        //   "<prefix> · <statusLabel(state)>"
        // Si le prefix devient "LeVoile" ou "Le-Voile", le rendu casse la charte.
        assertEquals(
            "Le Voile",
            parseStringResourceKeys(stringsXml(default = true))["notif_title_prefix"]
        )
        assertEquals(
            "Le Voile",
            parseStringResourceKeys(stringsXml(default = false))["notif_title_prefix"]
        )
    }

    @Test
    fun `default values match values-fr for the 9 Story 9_6 keys (regression guard H-1)`() {
        // Story 9.1 a livre le FR comme locale par defaut. Tant que Story 11.x
        // n'a pas migre l'EN par defaut, les valeurs default DOIVENT etre
        // identiques aux valeurs values-fr/ (memes accents, meme casse).
        //
        // Sans ce test, un editeur peut accidentellement copier-coller des
        // chaines sans accents dans values/ ("Connecte" au lieu de "Connecte")
        // — les utilisateurs hors locale fr verraient alors des typos. C'est
        // exactement la regression H-1 detectee au code-review post-9.6.
        //
        // Quand Story 11.x livrera l'EN par defaut, ce test devra etre adapte
        // (par ex. asserter que default = EN et values-fr/ = FR avec accents).
        val defaultKeys = parseStringResourceKeys(stringsXml(default = true))
        val frKeys = parseStringResourceKeys(stringsXml(default = false))
        STORY_9_6_KEYS.forEach { key ->
            assertEquals(
                "Parite default <-> values-fr cassee pour la cle « $key » " +
                    "(Story 9.1 livre FR par defaut). Si Story 11.x a migre " +
                    "l'EN par defaut, adapter ce test.",
                frKeys[key],
                defaultKeys[key]
            )
        }
    }

    // ---------- drawable/ic_levoile_status.xml (final) ----------

    @Test
    fun `ic_levoile_status drawable exists as a 24dp mono-color vector`() {
        val drawable = drawableFile("ic_levoile_status.xml")
        assertTrue(
            "drawable/ic_levoile_status.xml doit exister — actuel : ${drawable.absolutePath}",
            drawable.exists()
        )
        val doc = hardenedDocumentBuilderFactory().newDocumentBuilder().parse(drawable)
        val root = doc.documentElement
        assertEquals("vector", root.tagName)
        assertEquals(
            "Vector 24dp largeur",
            "24dp",
            root.getAttributeNS(ANDROID_NS, "width")
        )
        assertEquals(
            "Vector 24dp hauteur",
            "24dp",
            root.getAttributeNS(ANDROID_NS, "height")
        )
        assertEquals(
            "viewportWidth 24",
            "24",
            root.getAttributeNS(ANDROID_NS, "viewportWidth")
        )
        assertEquals(
            "viewportHeight 24",
            "24",
            root.getAttributeNS(ANDROID_NS, "viewportHeight")
        )
        // Au moins un <path> avec fillColor blanc opaque (mono-couleur).
        val paths = root.getElementsByTagName("path")
        assertTrue(
            "ic_levoile_status doit contenir au moins un <path>",
            paths.length >= 1
        )
        var foundWhitePath = false
        for (i in 0 until paths.length) {
            val el = paths.item(i) as Element
            val fill = el.getAttributeNS(ANDROID_NS, "fillColor").uppercase()
            if (fill == "#FFFFFFFF" || fill == "#FFFFFF") {
                foundWhitePath = true
                break
            }
        }
        assertTrue(
            "Au moins un <path> doit avoir fillColor blanc opaque (icone notif Android = " +
                "mono-couleur, reteinte par setColor du builder)",
            foundWhitePath
        )
    }

    @Test
    fun `ic_notification_stub drawable has been removed`() {
        // Regression guard : si quelqu'un re-cree ce fichier (ex : copie hative
        // depuis un ancien diff), il faut detecter au CI.
        val stub = drawableFile("ic_notification_stub.xml")
        assertFalse(
            "drawable/ic_notification_stub.xml doit avoir ete supprime Story 9.6 — " +
                "actuel : ${stub.absolutePath} existe = ${stub.exists()}",
            stub.exists()
        )
    }

    // ---------- Helpers ----------

    private fun stringsXml(default: Boolean): File {
        val rel = if (default) "res/values/strings.xml" else "res/values-fr/strings.xml"
        val candidates = listOf(
            File("src/main/$rel"),
            File("app/src/main/$rel"),
            File("../app/src/main/$rel")
        )
        return candidates.firstOrNull { it.exists() }
            ?: throw AssertionError(
                "$rel introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates testes : ${candidates.joinToString { it.absolutePath }}"
            )
    }

    private fun drawableFile(name: String): File {
        val candidates = listOf(
            File("src/main/res/drawable/$name"),
            File("app/src/main/res/drawable/$name"),
            File("../app/src/main/res/drawable/$name")
        )
        // On prend le premier qui existe ; sinon le 2eme candidate par defaut
        // (garde un absolutePath utile pour les messages d'erreur).
        return candidates.firstOrNull { it.exists() } ?: candidates[1]
    }

    private fun parseStringResourceKeys(file: File): Map<String, String> {
        val doc = hardenedDocumentBuilderFactory().newDocumentBuilder().parse(file)
        val nodes = doc.getElementsByTagName("string")
        val out = mutableMapOf<String, String>()
        for (i in 0 until nodes.length) {
            val el = nodes.item(i) as Element
            val name = el.getAttribute("name")
            if (name.isNotEmpty()) out[name] = el.textContent
        }
        return out
    }

    private fun hardenedDocumentBuilderFactory(): DocumentBuilderFactory =
        DocumentBuilderFactory.newInstance().apply {
            isNamespaceAware = true
            isXIncludeAware = false
            isExpandEntityReferences = false
            // Coherent Story 9.4 fix L-10 — defense en profondeur, NFR9.
            setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
            setFeature("http://xml.org/sax/features/external-general-entities", false)
            setFeature("http://xml.org/sax/features/external-parameter-entities", false)
        }

    private companion object {
        const val ANDROID_NS = "http://schemas.android.com/apk/res/android"

        val STORY_9_6_KEYS = listOf(
            "notif_channel_status_name",
            "notif_channel_status_desc",
            "notif_title_prefix",
            "vpn_status_connected",
            "vpn_status_reconnecting",
            "vpn_status_disconnected",
            "vpn_status_error",
            "notif_action_disconnect",
            "notif_content_description_disconnect"
        )

        val OBSOLETE_KEYS = listOf(
            "vpn_notif_title_stub",   // Story 9.4 — disparait avec buildStubOngoingNotification
            "notif_channel_status"    // Story 9.4 — renomme en notif_channel_status_name
        )
    }
}
