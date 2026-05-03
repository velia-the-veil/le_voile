package fr.plateformeliberte.levoile

import fr.plateformeliberte.levoile.bridge.LeVoileBridge
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Document
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * Smoke test Story 9.3 — vérifie la frontière compile-time (classes
 * MainActivity + LeVoileBridge) et la cohérence AndroidManifest pour
 * l'activité MAIN/LAUNCHER.
 *
 * Stratégie : JVM-only, AUCUNE dépendance Robolectric — décision cohérente
 * avec Story 9.4 (build.gradle.kts : testOptions.unitTests.isReturnDefaultValues
 * = true mocke les APIs Android, et le pattern parser DOM direct est déjà
 * utilisé par LeVoileVpnServiceConfigTest). Cela évite un poids ~25 MB de
 * dépendances Robolectric pour un seul test smoke.
 *
 * Parser retenu : javax.xml.parsers.DocumentBuilderFactory (DOM standard JDK).
 * Hardening XXE / billion-laughs / SSRF appliqué (cohérent défense en profondeur
 * NFR9 + alignement Story 9.4 fix L-10).
 *
 * Le test runtime complet (lance MainActivity réelle, vérifie WebView rendu,
 * polling JS, body.platform-android effectivement injecté) est porté par
 * Story 12.6 (matrice instrumentée Espresso API 29/33/34).
 */
class MainActivityConfigTest {

    @Test
    fun `MainActivity class is resolvable and extends AppCompatActivity`() {
        val cls = Class.forName("fr.plateformeliberte.levoile.MainActivity")
        assertNotNull(cls)
        assertTrue(
            "MainActivity doit hériter de androidx.appcompat.app.AppCompatActivity",
            androidx.appcompat.app.AppCompatActivity::class.java.isAssignableFrom(cls)
        )
    }

    @Test
    fun `LeVoileBridge exposes getStatus annotated JavascriptInterface returning placeholder JSON`() {
        val bridgeCls = Class.forName("fr.plateformeliberte.levoile.bridge.LeVoileBridge")
        val getStatusMethod = bridgeCls.getDeclaredMethods()
            .firstOrNull { it.name == "getStatus" && it.returnType == String::class.java }
        assertNotNull(
            "LeVoileBridge.getStatus(): String doit exister",
            getStatusMethod
        )

        // H-1 (code-review 9.3) : sans cette assertion, retirer @JavascriptInterface
        // par mégarde ferait passer le test mais casserait le bridge JS au runtime
        // (la méthode ne serait plus exposée à WebView). AC #4 exige explicitement
        // l'annotation `@android.webkit.JavascriptInterface` (pas l'alias).
        val jsAnnotation = getStatusMethod!!.getAnnotation(
            android.webkit.JavascriptInterface::class.java
        )
        assertNotNull(
            "AC #4 — getStatus() DOIT être annotée @android.webkit.JavascriptInterface",
            jsAnnotation
        )

        // M-1 (code-review 9.3) : on lit directement la const compagnon STATUS_JSON
        // au lieu d'instancier LeVoileBridge — le constructeur exige un Context
        // non-null (restauré M-1) qui n'est pas trivial à fournir en JVM-only sans
        // Mockito ni Robolectric. La const est exactement ce que getStatus() retourne
        // (cf. body : `fun getStatus(): String = STATUS_JSON`), donc équivalence
        // sémantique garantie par le code source ; cette équivalence est ré-vérifiée
        // au runtime par Story 12.6 (Espresso instancie le vrai bridge).
        //
        // Pas de parsing JSONObject ici : substrings ciblés (cohérent stratégie
        // Story 9.4 — pas de parser Android dans les tests unitaires JVM-only).
        val raw = LeVoileBridge.STATUS_JSON
        assertContains(raw, "\"state\":\"placeholder\"", "champ state")
        assertContains(raw, "\"platform\":\"android\"", "champ platform")
        assertContains(raw, "\"version\":\"0.1.0\"", "champ version")
        assertContains(raw, "Story 9.3", "référence Story 9.3 dans message")
    }

    @Test
    fun `JS_BRIDGE_NAME is LeVoile (drift guard with assets app_js and Story 11_2)`() {
        // M-5 (code-review 9.3) : `MainActivity.JS_BRIDGE_NAME` doit valoir "LeVoile"
        // — le frontend `assets/app.js` hardcode `window.LeVoile.getStatus()`. Si un
        // refactor renomme la const Kotlin sans mettre à jour app.js, le bridge JS
        // est invisible côté frontend et le polling tombe en mode "bridge-absent"
        // sans qu'aucun test compile-time ne le capte.
        //
        // Reflection (pas accès direct `MainActivity.JS_BRIDGE_NAME`) car le
        // compilateur Kotlin inline les `const val` à compile-time — un assert
        // direct passerait trivialement même si la valeur réelle dérivait.
        val cls = Class.forName("fr.plateformeliberte.levoile.MainActivity")
        val field = cls.getDeclaredField("JS_BRIDGE_NAME")
        field.isAccessible = true
        val value = field.get(null) as String
        assertEquals(
            "Le nom du bridge JS doit rester \"LeVoile\" (consommé par assets/app.js)",
            "LeVoile",
            value
        )
    }

    @Test
    fun `assets app_js wires window LeVoile getStatus polling`() {
        // M-5 (code-review 9.3) : verrouille le contrat frontend ↔ Kotlin côté JS.
        // Si quelqu'un retire le polling ou renomme l'accès `window.LeVoile`, le
        // dot de statut ne s'animera plus — bug runtime invisible jusqu'au test
        // instrumenté Espresso (Story 12.6). Test peu coûteux, sécurise le contrat.
        val appJs = resolveAsset("app.js")
        val src = appJs.readText()
        assertTrue(
            "app.js doit appeler window.LeVoile.getStatus — actuel : $src",
            src.contains("window.LeVoile.getStatus")
        )
        assertTrue(
            "app.js doit déclarer un setInterval pour le polling — actuel : $src",
            src.contains("setInterval")
        )
    }

    @Test
    fun `Manifest declares MainActivity with MAIN_LAUNCHER intent-filter and configChanges`() {
        val doc = parseManifest()

        val activities = doc.getElementsByTagName("activity")
        var activityEl: Element? = null
        for (i in 0 until activities.length) {
            val el = activities.item(i) as Element
            val name = el.getAttributeNS(ANDROID_NS, "name")
            if (name == ".MainActivity"
                || name == "fr.plateformeliberte.levoile.MainActivity"
            ) {
                activityEl = el
                break
            }
        }
        assertNotNull(
            "Tag <activity android:name=\".MainActivity\"> doit être déclaré dans AndroidManifest",
            activityEl
        )

        val activity = activityEl!!
        assertEquals(
            "true",
            activity.getAttributeNS(ANDROID_NS, "exported")
        )
        assertEquals(
            "singleTop",
            activity.getAttributeNS(ANDROID_NS, "launchMode")
        )

        // configChanges : architecture.md l. 1181, l. 1502 — doit inclure
        // au minimum orientation + screenSize pour préserver l'état WebView
        // sur rotation portrait↔paysage.
        val configChanges = activity.getAttributeNS(ANDROID_NS, "configChanges")
        assertTrue(
            "configChanges doit inclure 'orientation' — actuel : $configChanges",
            configChanges.contains("orientation")
        )
        assertTrue(
            "configChanges doit inclure 'screenSize' — actuel : $configChanges",
            configChanges.contains("screenSize")
        )

        // Intent-filter MAIN/LAUNCHER
        val actions = activity.getElementsByTagName("action")
        var foundMain = false
        for (i in 0 until actions.length) {
            val el = actions.item(i) as Element
            if (el.getAttributeNS(ANDROID_NS, "name") == "android.intent.action.MAIN") {
                foundMain = true
                break
            }
        }
        assertTrue(
            "intent-filter doit déclarer l'action android.intent.action.MAIN",
            foundMain
        )

        val categories = activity.getElementsByTagName("category")
        var foundLauncher = false
        for (i in 0 until categories.length) {
            val el = categories.item(i) as Element
            if (el.getAttributeNS(ANDROID_NS, "name") == "android.intent.category.LAUNCHER") {
                foundLauncher = true
                break
            }
        }
        assertTrue(
            "intent-filter doit déclarer la category android.intent.category.LAUNCHER",
            foundLauncher
        )
    }

    @Test
    fun `MainActivity declares requestVpnStart and requestVpnStop helpers (Story 9_5)`() {
        // Story 9.5 — helpers internal dormants (Story 11.2 wirera depuis
        // LeVoileBridge). Verifie via reflection :
        //   - requestVpnStart(country: String? = null) : Unit
        //   - requestVpnStop() : Unit
        // Visibilite `internal` (module-scope Kotlin) compile en `public` au
        // niveau bytecode JVM avec un suffixe $module — on accepte les deux
        // formes pour rester robuste a une evolution AGP/Kotlin.
        val cls = Class.forName("fr.plateformeliberte.levoile.MainActivity")
        val methods = cls.declaredMethods

        val requestVpnStartFound = methods.any { m ->
            m.name.startsWith("requestVpnStart")
                && m.parameterTypes.size == 1
                && m.parameterTypes[0] == String::class.java
        }
        assertTrue(
            "MainActivity doit declarer requestVpnStart(country: String?) — methodes vues : ${methods.map { it.name }}",
            requestVpnStartFound
        )

        val requestVpnStopFound = methods.any { m ->
            m.name.startsWith("requestVpnStop")
                && m.parameterTypes.isEmpty()
        }
        assertTrue(
            "MainActivity doit declarer requestVpnStop() — methodes vues : ${methods.map { it.name }}",
            requestVpnStopFound
        )
    }

    @Test
    fun `vpnConsentLauncher is registered before setContentView in onCreate (Fix L_3 code-review post-9_5)`() {
        // Fix L-3 : ActivityResultLauncher API exige que registerForActivityResult soit
        // appele PENDANT la phase CREATED du lifecycle Activity (sinon IllegalStateException
        // au runtime). Le pattern idiomatique = appeler registerForActivityResult AVANT
        // setContentView dans onCreate (super.onCreate -> registerForActivityResult ->
        // setContentView). Ce test verrouille l'ordre cote source pour eviter qu'un
        // refactor le casse silencieusement.
        //
        // Strategie revisee post-failure : on cherche dans le source brut au lieu
        // d'extraire le corps d'onCreate (extractFunctionBody fragile sur les
        // anonymous classes et lambdas du WebViewClient). Les deux strings
        // `vpnConsentLauncher = registerForActivityResult` et
        // `setContentView(R.layout.activity_main)` sont uniques dans MainActivity.kt
        // (verifie : un seul appel de chaque, dans onCreate exclusivement).
        val src = readMainActivitySource()
        val launcherIdx = src.indexOf("vpnConsentLauncher = registerForActivityResult")
        val setContentIdx = src.indexOf("setContentView(R.layout.activity_main)")
        assertTrue(
            "vpnConsentLauncher = registerForActivityResult doit etre present dans MainActivity.kt " +
                "(Story 9.5 helpers VPN)",
            launcherIdx >= 0
        )
        assertTrue(
            "setContentView(R.layout.activity_main) doit etre present dans MainActivity.kt " +
                "(cf. Story 9.3 setup WebView)",
            setContentIdx >= 0
        )
        assertTrue(
            "Fix L-3 : `vpnConsentLauncher = registerForActivityResult` DOIT apparaitre AVANT " +
                "`setContentView(R.layout.activity_main)` dans le source. " +
                "API ActivityResultLauncher exige la registration en phase CREATED " +
                "(IllegalStateException au runtime sinon). Indices : " +
                "launcher=$launcherIdx, setContent=$setContentIdx",
            launcherIdx < setContentIdx
        )
    }

    @Test
    fun `MainActivity declares vpnConsentLauncher and pendingConnectCountry fields (Story 9_5)`() {
        // Story 9.5 — verifie que l'ActivityResultLauncher + l'etat pendingConnectCountry
        // sont presents (necessaires au flow consent VpnService).
        val cls = Class.forName("fr.plateformeliberte.levoile.MainActivity")
        val fields = cls.declaredFields.map { it.name }
        assertTrue(
            "MainActivity doit declarer un champ `vpnConsentLauncher` — champs vus : $fields",
            fields.any { it == "vpnConsentLauncher" }
        )
        assertTrue(
            "MainActivity doit declarer un champ `pendingConnectCountry` — champs vus : $fields",
            fields.any { it == "pendingConnectCountry" }
        )
    }

    @Test
    fun `Network security config disables cleartext traffic`() {
        // AC #2 — vérifie que cleartextTrafficPermitted=false est bien posé
        // au niveau base-config. Le manifest déclare networkSecurityConfig
        // (Story 9.1) et le fichier @xml/network_security_config interdit
        // explicitement le cleartext (architecture.md l. 1582).
        val candidates = listOf(
            File("src/main/res/xml/network_security_config.xml"),
            File("app/src/main/res/xml/network_security_config.xml"),
            File("../app/src/main/res/xml/network_security_config.xml")
        )
        val cfg = candidates.firstOrNull { it.exists() }
            ?: throw AssertionError(
                "network_security_config.xml introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates testés : ${candidates.joinToString { it.absolutePath }}"
            )
        val factory = hardenedDocumentBuilderFactory()
        val doc = factory.newDocumentBuilder().parse(cfg)
        val baseConfigs = doc.getElementsByTagName("base-config")
        assertTrue(
            "network_security_config doit déclarer un <base-config>",
            baseConfigs.length >= 1
        )
        val base = baseConfigs.item(0) as Element
        assertEquals(
            "false",
            base.getAttribute("cleartextTrafficPermitted")
        )
    }

    @Test
    fun `Assets folder contains index_html style_css and app_js committed`() {
        // Story 11.1 — les fichiers ont été déplacés dans le sous-dossier web/
        // (séparation assets sync vs natifs). MainActivity.ASSET_INDEX_URL
        // pointe désormais vers /assets/web/index.html.
        val candidates = listOf(
            File("src/main/assets/web"),
            File("app/src/main/assets/web"),
            File("../app/src/main/assets/web")
        )
        val webDir = candidates.firstOrNull { it.isDirectory }
            ?: throw AssertionError(
                "Dossier assets/web/ introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates testés : ${candidates.joinToString { it.absolutePath }}"
            )
        assertTrue(
            "assets/web/index.html doit exister",
            File(webDir, "index.html").exists()
        )
        assertTrue(
            "assets/web/style.css doit exister",
            File(webDir, "style.css").exists()
        )
        assertTrue(
            "assets/web/app.js doit exister",
            File(webDir, "app.js").exists()
        )
    }

    // ---------- Helpers ----------

    private fun parseManifest(): Document {
        val candidates = listOf(
            File("src/main/AndroidManifest.xml"),
            File("app/src/main/AndroidManifest.xml"),
            File("../app/src/main/AndroidManifest.xml")
        )
        val manifest = candidates.firstOrNull { it.exists() }
            ?: throw AssertionError(
                "AndroidManifest.xml introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates testés : ${candidates.joinToString { it.absolutePath }}"
            )
        return hardenedDocumentBuilderFactory().newDocumentBuilder().parse(manifest)
    }

    private fun hardenedDocumentBuilderFactory(): DocumentBuilderFactory =
        DocumentBuilderFactory.newInstance().apply {
            isNamespaceAware = true
            isXIncludeAware = false
            isExpandEntityReferences = false
            // Hardening XXE / billion-laughs / SSRF — cohérent NFR9 défense en
            // profondeur, aligné Story 9.4 fix L-10.
            setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
            setFeature("http://xml.org/sax/features/external-general-entities", false)
            setFeature("http://xml.org/sax/features/external-parameter-entities", false)
        }

    private fun assertContains(haystack: String, needle: String, what: String) {
        assertTrue(
            "$what : la chaîne « $needle » doit apparaître dans la valeur retournée — actuel : $haystack",
            haystack.contains(needle)
        )
    }

    private fun resolveAsset(name: String): File {
        // Helper M-5 (code-review 9.3) : résout un fichier sous app/src/main/assets/web/.
        // Story 11.1 a déplacé les assets dans le sous-dossier web/.
        val candidates = listOf(
            File("src/main/assets/web/$name"),
            File("app/src/main/assets/web/$name"),
            File("../app/src/main/assets/web/$name")
        )
        return candidates.firstOrNull { it.exists() }
            ?: throw AssertionError(
                "assets/$name introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates testés : ${candidates.joinToString { it.absolutePath }}"
            )
    }

    /**
     * Fix L-3 (code-review post-9.5) : lecture du source pour verrouiller l'ordre
     * registerForActivityResult AVANT setContentView dans onCreate. Cherche
     * MainActivity.kt dans les candidats classiques.
     */
    private fun readMainActivitySource(): String {
        val candidates = listOf(
            File("src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt"),
            File("app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt"),
            File("../app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt")
        )
        return candidates.firstOrNull { it.exists() }?.readText()
            ?: throw AssertionError(
                "MainActivity.kt introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates testes : ${candidates.joinToString { it.absolutePath }}"
            )
    }

    /**
     * Helper miroir de LeVoileVpnServiceConfigTest.extractFunctionBody — extrait
     * le corps d'une fun en parsant les `{` `}` equilibres. Naif mais suffisant
     * pour les fonctions Story 9.3+ qui n'utilisent pas de strings template
     * contenant des accolades.
     */
    private fun extractFunctionBody(src: String, name: String): String {
        val regex = Regex("""(?:override\s+)?(?:private\s+|internal\s+|public\s+)?fun\s+$name\s*\([^)]*\)[^{]*\{""")
        val match = regex.find(src) ?: throw AssertionError("fun $name introuvable dans le source")
        val openIdx = src.indexOf('{', match.range.last - 1)
        var depth = 1
        var i = openIdx + 1
        while (i < src.length && depth > 0) {
            when (src[i]) {
                '{' -> depth++
                '}' -> depth--
            }
            i++
        }
        if (depth != 0) throw AssertionError("fun $name : `{` non equilibres")
        return src.substring(openIdx + 1, i - 1)
    }

    private companion object {
        const val ANDROID_NS = "http://schemas.android.com/apk/res/android"
    }
}
