package fr.plateformeliberte.levoile.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Document
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * Smoke test Story 9.4 — verifie la frontiere compile-time et la coherence
 * AndroidManifest pour LeVoileVpnService.
 *
 * Strategie : JVM-only, AUCUNE dependance Robolectric (decision Debug Log
 * Story 9.4 — Robolectric absent du build.gradle.kts post-9.3 ; AC #9 autorise
 * explicitement le fallback parser direct pour la verification manifest).
 *
 * Parser retenu : javax.xml.parsers.DocumentBuilderFactory (DOM standard JDK).
 * Raison : org.xmlpull.v1.XmlPullParserFactory.newInstance() retourne null
 * dans le stub Android JAR de l'unit test (testOptions.unitTests.isReturnDefaultValues
 * = true mocke les APIs Android, et XmlPullParser n'est pas livre par le JDK).
 *
 * Les APIs android.* invoquees indirectement par NoOpPacketRelay
 * (android.util.Log.d) sont stubees via testOptions.unitTests.isReturnDefaultValues
 * = true (build.gradle.kts).
 *
 * Ce test ne charge PAS le runtime VpnService reel (qui requiert un device/
 * emulateur Android et le consentement utilisateur). Le test instrumente
 * complet (lance le service, verifie pump qui draine, ferme proprement) est
 * porte par Story 12.6 (Espresso + matrice API 29/33/34).
 */
class LeVoileVpnServiceConfigTest {

    @Test
    fun `LeVoileVpnService class is resolvable and extends VpnService`() {
        val cls = Class.forName("fr.plateformeliberte.levoile.vpn.LeVoileVpnService")
        assertNotNull(cls)
        assertTrue(
            "LeVoileVpnService doit heriter de android.net.VpnService",
            android.net.VpnService::class.java.isAssignableFrom(cls)
        )
    }

    @Test
    fun `VpnConstants exposes expected action and notif IDs`() {
        assertEquals(
            "fr.plateformeliberte.levoile.action.CONNECT",
            VpnConstants.ACTION_CONNECT
        )
        assertEquals(
            "fr.plateformeliberte.levoile.action.DISCONNECT",
            VpnConstants.ACTION_DISCONNECT
        )
        assertEquals(
            "fr.plateformeliberte.levoile.extra.COUNTRY",
            VpnConstants.EXTRA_COUNTRY
        )
        assertEquals(0xCEC1, VpnConstants.NOTIF_ID)
        assertEquals("levoile_vpn_status_stub", VpnConstants.CHANNEL_ID_STUB)
        assertEquals(32_768, VpnConstants.MAX_IP_PACKET)
    }

    @Test
    fun `PacketRelay interface declares onOutboundPacket(ByteArray, Int)`() {
        val cls = Class.forName("fr.plateformeliberte.levoile.vpn.PacketRelay")
        val method = cls.declaredMethods.firstOrNull {
            it.name == "onOutboundPacket"
                && it.parameterTypes.size == 2
                && it.parameterTypes[0] == ByteArray::class.java
                && it.parameterTypes[1] == Int::class.javaPrimitiveType
        }
        assertNotNull(
            "PacketRelay.onOutboundPacket(ByteArray, Int) doit exister",
            method
        )
    }

    @Test
    fun `NoOpPacketRelay drops packets without throwing including the throttled Log_d branch`() {
        val relay: PacketRelay = NoOpPacketRelay()
        // Fix M-4 (code-review post-9.4) : 1500 invocations pour franchir le seuil
        // throttle (Log.d emis tous les 1000 paquets dans NoOpPacketRelay). Sans
        // ce nombre, le branche Log.d n'etait jamais execute par le test, donc
        // la justification "Log.d est stube via returnDefaultValues" n'etait pas
        // effectivement validee.
        relay.onTunnelStarted()
        val buf = ByteArray(64) { it.toByte() }
        repeat(1500) {
            relay.onOutboundPacket(buf, 64)
        }
        relay.onTunnelStopped()
    }

    @Test
    fun `Manifest declares LeVoileVpnService with BIND_VPN_SERVICE permission and foregroundServiceType specialUse subtype vpn`() {
        val doc = parseManifest()

        val services = doc.getElementsByTagName("service")
        var serviceEl: Element? = null
        for (i in 0 until services.length) {
            val el = services.item(i) as Element
            val name = el.getAttributeNS(ANDROID_NS, "name")
            if (name == ".vpn.LeVoileVpnService"
                || name == "fr.plateformeliberte.levoile.vpn.LeVoileVpnService"
            ) {
                serviceEl = el
                break
            }
        }
        assertNotNull(
            "Tag <service android:name=\".vpn.LeVoileVpnService\"> doit etre declare dans AndroidManifest",
            serviceEl
        )

        val service = serviceEl!!
        assertEquals(
            "android.permission.BIND_VPN_SERVICE",
            service.getAttributeNS(ANDROID_NS, "permission")
        )
        assertEquals(
            "specialUse",
            service.getAttributeNS(ANDROID_NS, "foregroundServiceType")
        )
        assertEquals(
            "false",
            service.getAttributeNS(ANDROID_NS, "exported")
        )

        // Intent-filter <action android:name="android.net.VpnService"/>
        val actions = service.getElementsByTagName("action")
        var foundVpnAction = false
        for (i in 0 until actions.length) {
            val el = actions.item(i) as Element
            if (el.getAttributeNS(ANDROID_NS, "name") == "android.net.VpnService") {
                foundVpnAction = true
                break
            }
        }
        assertTrue(
            "intent-filter doit declarer l'action android.net.VpnService",
            foundVpnAction
        )

        // <property android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE" android:value="vpn"/>
        val properties = service.getElementsByTagName("property")
        var foundFgsSubtypeProperty = false
        for (i in 0 until properties.length) {
            val el = properties.item(i) as Element
            if (el.getAttributeNS(ANDROID_NS, "name") == "android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE"
                && el.getAttributeNS(ANDROID_NS, "value") == "vpn"
            ) {
                foundFgsSubtypeProperty = true
                break
            }
        }
        assertTrue(
            "Property android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE='vpn' doit etre declaree dans <service>",
            foundFgsSubtypeProperty
        )
    }

    @Test
    fun `STOP_FOREGROUND_DELAY_MS exposes 5000 ms (Story 9_5)`() {
        // Story 9.5 — coherent epic AC `epics.md l. 1611` : delai 5 s avant
        // stopForeground apres disconnectInternal. Le runtime du delai est
        // valide en instrumented Story 12.6 ; ici on verrouille la valeur
        // exposee au companion object.
        assertEquals(5_000L, LeVoileVpnService.STOP_FOREGROUND_DELAY_MS)
    }

    @Test
    fun `LeVoileVpnService companion exposes Volatile internal var instance (Story 9_5)`() {
        // Story 9.5 — verifie via reflection que le singleton instance est
        // accessible depuis le module :app (utile a Story 11.2 et aux
        // diagnostics introspectifs). Trois invariants acceptes (Kotlin
        // mangle les `internal` getters avec un suffixe dependant du module
        // -> `getInstance$app_debug` etc., on reste tolerant) :
        //   1. Companion class existe.
        //   2. Un champ statique `instance` est present sur la classe outer
        //      (Kotlin deplace le backing field d'une property companion
        //      avec @Volatile sur la classe outer).
        //   3. Au moins un getter dont le nom commence par "getInstance"
        //      existe quelque part (Companion ou outer).
        val cls = Class.forName("fr.plateformeliberte.levoile.vpn.LeVoileVpnService")
        val companionCls = cls.declaredClasses.firstOrNull { it.simpleName == "Companion" }
        assertNotNull(
            "LeVoileVpnService doit avoir un companion object",
            companionCls
        )

        // Invariant principal : le backing field statique `instance` doit
        // exister sur la classe outer (avec @Volatile, Kotlin pose le champ
        // sur l'outer class et NON sur le Companion, pour pouvoir y mettre
        // la modifier `volatile` JVM).
        val instanceField = cls.declaredFields.firstOrNull { it.name == "instance" }
        assertNotNull(
            "Backing field statique `instance` doit etre declare sur LeVoileVpnService " +
                "(deplace par @Volatile sur la classe outer plutot que sur Companion). " +
                "Champs vus : ${cls.declaredFields.map { it.name }}",
            instanceField
        )
        assertEquals(
            "fr.plateformeliberte.levoile.vpn.LeVoileVpnService",
            instanceField!!.type.name
        )
        // Le champ doit etre static + volatile.
        val mods = instanceField.modifiers
        assertTrue(
            "instance doit etre un champ STATIC (modifiers=$mods)",
            java.lang.reflect.Modifier.isStatic(mods)
        )
        assertTrue(
            "instance doit etre VOLATILE (modifiers=$mods)",
            java.lang.reflect.Modifier.isVolatile(mods)
        )

        // Invariant secondaire : un getter "getInstance*" doit exister sur le
        // Companion (avec ou sans suffixe de mangling Kotlin internal).
        val companionGetters = companionCls!!.declaredMethods.filter {
            it.name.startsWith("getInstance") && it.parameterCount == 0
        }
        assertTrue(
            "Au moins un getter getInstance* doit etre expose sur LeVoileVpnService.Companion. " +
                "Methodes vues : ${companionCls.declaredMethods.map { it.name }}",
            companionGetters.isNotEmpty()
        )
    }

    @Test
    fun `LeVoileVpnService declares teardownHandler field (Story 9_5)`() {
        // Story 9.5 — le delai 5 s est implemente via Handler(Looper.getMainLooper())
        // .postDelayed. Reflection verifie la presence du champ pour eviter
        // une regression silencieuse (renommage refacto futur).
        val cls = Class.forName("fr.plateformeliberte.levoile.vpn.LeVoileVpnService")
        val teardownField = cls.declaredFields.firstOrNull { it.name == "teardownHandler" }
        assertNotNull(
            "LeVoileVpnService doit declarer un champ `teardownHandler` (Handler) — Story 9.5",
            teardownField
        )
        assertEquals(
            "android.os.Handler",
            teardownField!!.type.name
        )
    }

    @Test
    fun `LeVoileVpnService declares cleanupSync helper (Story 9_5)`() {
        // Story 9.5 — cleanupSync extrait de l'ancien disconnectInternal pour
        // permettre un teardown synchrone idempotent depuis onDestroy (sans
        // relancer un postDelayed sur Service deja en destruction).
        val cls = Class.forName("fr.plateformeliberte.levoile.vpn.LeVoileVpnService")
        val cleanupMethod = cls.declaredMethods.firstOrNull {
            it.name == "cleanupSync" && it.parameterCount == 0
        }
        assertNotNull(
            "LeVoileVpnService doit declarer une methode privee cleanupSync() — Story 9.5",
            cleanupMethod
        )
    }

    // ---------- Fix H-3 / H-4 / M-7 (code-review post-9.5) : invariants source-text ----------
    //
    // Strategie pragmatique : sans Robolectric, on ne peut pas chronometrer le runtime
    // (AC #1 < 5 s) ni intercepter le retour d'`onStartCommand` (AC #3 START_REDELIVER_INTENT)
    // ni observer le comportement runtime du Handler (AC #5). Le pattern source-text
    // (lecture du .kt + assertions sur le contenu) est cher en cas de refonte mais
    // verrouille les invariants critiques contre une regression silencieuse :
    //   - H-3 : la PREMIERE instruction effective d'`onStartCommand` doit etre
    //           `startForeground(...)` (anti-ANR Android < 5 s).
    //   - H-4 : `onStartCommand` doit retourner `Service.START_REDELIVER_INTENT`
    //           (crash recovery — sinon le tunnel ne redemarre pas apres SIGKILL OS).
    //   - M-7 : `disconnectInternal` doit annuler tout teardown pending AVANT
    //           d'en re-poster un (idempotence Handler — sinon double tap "Disconnect"
    //           empilerait deux stopSelf et l'un crasherait sur Service deja stoppe).
    //   - H-1 : `connectInternal` doit annuler tout teardown pending pour eviter
    //           que la sequence disconnect -> reconnect rapide laisse executer
    //           un orphelin stopForeground sur Service redevenu actif.
    //
    // Le runtime reel (chronometrage, comportement Handler, race) est valide par
    // Story 12.6 (matrice instrumentee Espresso API 29/33/34).

    @Test
    fun `onStartCommand calls startForeground BEFORE any side-effecting call (Fix H_3 anti-ANR)`() {
        // Strategie revisee post-failure : chercher l'index de `startForeground(`
        // puis verifier que tout appel side-effecting (connectInternal,
        // disconnectInternal, stopSelf, Log.*) apparait APRES dans le corps.
        // Plus robuste que le pattern "premiere instruction effective" qui se
        // faisait pieger par les branches `when` indentees (val initialState =
        // when (action) { ACTION_CONNECT -> ... }).
        //
        // Les `val` purement preparatoires (val action, val initialState) sont
        // autorises avant — ils n'invoquent aucune API Android couteuse, juste
        // de l'enum dispatch local (zero risque ANR).
        val src = readServiceSource()
        val body = extractFunctionBody(src, "onStartCommand")
        val startForegroundIdx = body.indexOf("startForeground(")
        assertTrue(
            "AC #1 / Fix H-3 : onStartCommand DOIT appeler startForeground(...). Body :\n$body",
            startForegroundIdx >= 0
        )
        // Liste des side-effects qui DOIVENT apparaitre APRES startForeground.
        // (stopForeground autorise apres — path action inconnue qui cleanup.)
        val sideEffectCalls = listOf(
            "connectInternal(",
            "disconnectInternal(",
            "stopSelf()",
            "Log.i(",
            "Log.w(",
            "Log.e("
        )
        sideEffectCalls.forEach { call ->
            val idx = body.indexOf(call)
            if (idx >= 0) {
                assertTrue(
                    "AC #1 / Fix H-3 : '$call' apparait AVANT startForeground(...) dans onStartCommand " +
                        "(indices : startForeground=$startForegroundIdx, $call=$idx). " +
                        "Cela retarde startForeground -> ANR system Android si delai > 5 s.",
                    idx > startForegroundIdx
                )
            }
        }
    }

    @Test
    fun `onStartCommand returns Service_START_REDELIVER_INTENT (Fix H_4 AC_3 crash recovery)`() {
        val src = readServiceSource()
        val body = extractFunctionBody(src, "onStartCommand")
        // Tolere `Service.START_REDELIVER_INTENT` ou `START_REDELIVER_INTENT` (import statique).
        val hasReturn = body.contains("return Service.START_REDELIVER_INTENT") ||
            body.contains("return START_REDELIVER_INTENT")
        assertTrue(
            "AC #3 / Fix H-4 : onStartCommand DOIT retourner Service.START_REDELIVER_INTENT pour " +
                "permettre le crash recovery (cf. architecture.md l. 1051). Body :\n$body",
            hasReturn
        )
    }

    @Test
    fun `disconnectInternal cancels pending teardown then posts delayed teardown (Fix M_7 AC_5 idempotence)`() {
        val src = readServiceSource()
        val body = extractFunctionBody(src, "disconnectInternal")
        // 1. removeCallbacksAndMessages(null) DOIT etre present (idempotence du double-tap)
        assertTrue(
            "AC #5 / Fix M-7 : disconnectInternal DOIT appeler teardownHandler.removeCallbacksAndMessages(null) " +
                "pour annuler tout teardown precedent — sinon double-tap Disconnect empile deux stopSelf. " +
                "Body :\n$body",
            body.contains("removeCallbacksAndMessages(null)")
        )
        // 2. postDelayed avec STOP_FOREGROUND_DELAY_MS DOIT etre present (delai 5 s)
        assertTrue(
            "AC #5 / Fix M-7 : disconnectInternal DOIT poster un Runnable via teardownHandler.postDelayed(...) " +
                "avec STOP_FOREGROUND_DELAY_MS — sinon le delai 5 s n'est pas applique. Body :\n$body",
            body.contains("postDelayed") && body.contains("STOP_FOREGROUND_DELAY_MS")
        )
        // 3. Le runnable poste DOIT contenir stopForeground + stopSelf (le retrait notif + service)
        assertTrue(
            "AC #5 : le Runnable differe DOIT appeler stopForeground(STOP_FOREGROUND_REMOVE). Body :\n$body",
            body.contains("stopForeground(STOP_FOREGROUND_REMOVE)")
        )
        assertTrue(
            "AC #5 : le Runnable differe DOIT appeler stopSelf(). Body :\n$body",
            body.contains("stopSelf()")
        )
    }

    @Test
    fun `connectInternal cancels pending teardown to prevent reconnect race (Fix H_1)`() {
        val src = readServiceSource()
        val body = extractFunctionBody(src, "connectInternal")
        assertTrue(
            "Fix H-1 (code-review post-9.5) : connectInternal DOIT appeler " +
                "teardownHandler.removeCallbacksAndMessages(null) en debut de methode pour " +
                "eviter que la sequence disconnect -> reconnect rapide (< 5 s) laisse un " +
                "stopForeground orphelin executer apres reconnect (demote le Service redevenu actif " +
                "en background). Body :\n$body",
            body.contains("removeCallbacksAndMessages(null)")
        )
    }

    @Test
    fun `Manifest declares FOREGROUND_SERVICE_SPECIAL_USE uses-permission`() {
        val doc = parseManifest()
        val permissions = mutableSetOf<String>()
        val nodes = doc.getElementsByTagName("uses-permission")
        for (i in 0 until nodes.length) {
            val el = nodes.item(i) as Element
            el.getAttributeNS(ANDROID_NS, "name").takeIf { it.isNotEmpty() }?.let { permissions += it }
        }
        assertTrue(
            "FOREGROUND_SERVICE_SPECIAL_USE doit etre declaree (Android 14 requirement pour FGS specialUse) — actuelles : $permissions",
            "android.permission.FOREGROUND_SERVICE_SPECIAL_USE" in permissions
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
                // Fix L-11 (code-review post-9.4) : message diagnostic enrichi avec
                // user.dir + chemins absolus, pour debug rapide si une evolution
                // de Gradle ou un IDE change le working dir des unit tests.
                "AndroidManifest.xml introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates testes : ${candidates.joinToString { it.absolutePath }}"
            )
        // Fix L-10 (code-review post-9.4) : durcissement XXE meme pour un parser
        // local sur un fichier local — coherent NFR9 (defense en profondeur).
        // Si un manifeste est un jour synthetise depuis une source non-trustworthy
        // (futur scenario CI), le hardening evite XXE / billion-laughs / SSRF via
        // entites externes. Cout = 0 (factory locale au test).
        val factory = DocumentBuilderFactory.newInstance().apply {
            isNamespaceAware = true
            isXIncludeAware = false
            isExpandEntityReferences = false
            setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
            setFeature("http://xml.org/sax/features/external-general-entities", false)
            setFeature("http://xml.org/sax/features/external-parameter-entities", false)
        }
        return factory.newDocumentBuilder().parse(manifest)
    }

    /**
     * Fix H-3/H-4/M-7/H-1 (code-review post-9.5) : lecture du source Kotlin pour
     * verrouiller des invariants comportementaux que JVM-only ne peut pas tester
     * autrement (sans Robolectric ni Espresso). Cherche le fichier dans les
     * candidats classiques (cwd Gradle peut etre :app ou root).
     */
    private fun readServiceSource(): String {
        val candidates = listOf(
            File("src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt"),
            File("app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt"),
            File("../app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt")
        )
        return candidates.firstOrNull { it.exists() }?.readText()
            ?: throw AssertionError(
                "LeVoileVpnService.kt introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates testes : ${candidates.joinToString { it.absolutePath }}"
            )
    }

    /**
     * Extrait le corps (entre la premiere `{` ouvrante au niveau classe et sa `}`
     * fermante equilibree) d'une fonction nommee `name`. Suppose que la signature
     * tient sur une seule ligne (cas pour onStartCommand, connectInternal,
     * disconnectInternal — verifie au moment du fix). Si la signature est
     * multi-lignes, etendre le pattern de detection.
     *
     * Fragile contre une refonte qui changerait le nom de la fonction — c'est
     * exactement le but : forcer le dev a relire les invariants H-3/H-4/M-7/H-1
     * lors d'un rename.
     */
    private fun extractFunctionBody(src: String, name: String): String {
        // Pattern : `fun <name>(...)` ou `override fun <name>(...)` puis `{` quelque part avant fin de ligne
        val regex = Regex("""(?:override\s+)?(?:private\s+|internal\s+|public\s+)?fun\s+$name\s*\([^)]*\)[^{]*\{""")
        val match = regex.find(src) ?: throw AssertionError(
            "fun $name introuvable dans le source. Refactor depuis le code-review ? " +
                "Patterns cherchees : `(override )?(private |internal |public )?fun $name(...)... {`"
        )
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
        if (depth != 0) {
            throw AssertionError("fun $name : `{` non equilibres (parser naif). Verifier le source.")
        }
        return src.substring(openIdx + 1, i - 1)
    }

    private companion object {
        const val ANDROID_NS = "http://schemas.android.com/apk/res/android"
    }
}
