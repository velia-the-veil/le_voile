package fr.plateformeliberte.levoile.bridge

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/**
 * Charge une classe sans déclencher son initializer statique. Les classes
 * générées par gomobile invoquent `System.loadLibrary("gojni")` dans leur
 * `<clinit>` ; sur la JVM des unit tests (sans device Android), cette
 * libgojni.so n'est pas disponible — d'où UnsatisfiedLinkError /
 * ExceptionInInitializerError. On vérifie ici uniquement la *résolution*
 * de la classe (preuve qu'elle est dans le classpath via le .aar) ; le
 * load JNI complet est validé en Story 12.6 (Espresso instrumenté).
 */
private fun resolveClass(name: String): Class<*> =
    Class.forName(name, false, GoCoreAdapterContractTest::class.java.classLoader)

private fun assertHasMethod(cls: Class<*>, methodName: String, vararg paramTypes: Class<*>) {
    try {
        cls.getDeclaredMethod(methodName, *paramTypes)
    } catch (e: NoSuchMethodException) {
        // Recherche tolérante : certaines versions de gomobile renomment ou
        // décorent (ex. ajout de `Companion`). On scanne par nom seul.
        val byName = cls.declaredMethods.filter { it.name == methodName }
        if (byName.isEmpty()) {
            fail(
                "${cls.simpleName}.$methodName(${paramTypes.joinToString { it.simpleName }}) " +
                    "introuvable dans le .aar — gomobile a-t-il bien bound la nouvelle " +
                    "surface Story 9.7 ? Régénérer via : bash android/scripts/build-aar.sh " +
                    "(ou pwsh build-aar.ps1). (${e.message})"
            )
        }
        // Sinon : trouvé par nom — on accepte comme preuve de présence.
    }
}

/**
 * Test contrat Story 9.7 — vérifie que les classes générées par gomobile
 * (post-extension shims protocol/auth + facade gomobile_facade.go) sont
 * résolvables et exposent les nouvelles signatures (Connect / WritePacket /
 * Close / SetPacketCallback / SetStatusCallback côté Protocol ;
 * IssueSessionToken / RefreshSessionToken / ValidateSessionToken côté Auth).
 *
 * Vérifie aussi que [GoCoreAdapter] singleton Kotlin expose les méthodes
 * documentées (frontière étroite ADR-09 — cohérent architecture l. 1059-1060).
 *
 * Ne déclenche PAS le runtime JNI — test JVM-only. Tests fonctionnels du
 * tunnel réel (handshake QUIC vers un relais) = Story 12.6.
 */
class GoCoreAdapterContractTest {

    // ----- Surface gomobile générée (côté .aar Java) -----

    @Test
    fun `gomobile Protocol class exposes new Story 9_7 surface`() {
        val cls = resolveClass("fr.plateformeliberte.levoile.core.protocol.Protocol")
        assertNotNull(cls)
        // Méthodes Story 9.2 (régression).
        assertHasMethod(cls, "version")
        assertHasMethod(cls, "framingHeaderSize")
        // Méthodes Story 9.7 (nouvelle surface fonctionnelle).
        assertHasMethod(cls, "connect")
        assertHasMethod(cls, "writePacket")
        assertHasMethod(cls, "close")
        assertHasMethod(cls, "setPacketCallback")
        assertHasMethod(cls, "setStatusCallback")
        assertHasMethod(cls, "isSessionOpen")
    }

    /**
     * **L-3 (code-review Story 9.7)** : verifie les SIGNATURES (return types,
     * arity) — pas seulement les noms — pour détecter les régressions API
     * silencieuses (ex. gomobile renommerait une méthode mais on aurait un
     * faux green sur le check par nom).
     */
    @Test
    fun `gomobile Protocol method signatures match Story 9_7 expectations`() {
        val cls = resolveClass("fr.plateformeliberte.levoile.core.protocol.Protocol")

        // version() : () -> String
        val version = cls.declaredMethods.firstOrNull { it.name == "version" }
        assertNotNull("Protocol.version manquant", version)
        assertEquals("Protocol.version() doit retourner String", String::class.java, version!!.returnType)
        assertEquals(0, version.parameterCount)

        // framingHeaderSize() : () -> long  (gomobile mappe Go int → Java long)
        val fhs = cls.declaredMethods.firstOrNull { it.name == "framingHeaderSize" }
        assertNotNull("Protocol.framingHeaderSize manquant", fhs)
        assertEquals(java.lang.Long.TYPE, fhs!!.returnType)

        // connect(String, String) : void (Java) — error -> Exception via gomobile
        val connect = cls.declaredMethods.firstOrNull { it.name == "connect" }
        assertNotNull("Protocol.connect manquant", connect)
        assertEquals(2, connect!!.parameterCount)
        assertEquals(String::class.java, connect.parameterTypes[0])
        assertEquals(String::class.java, connect.parameterTypes[1])

        // writePacket([]byte) : void
        val writePacket = cls.declaredMethods.firstOrNull { it.name == "writePacket" }
        assertNotNull("Protocol.writePacket manquant", writePacket)
        assertEquals(1, writePacket!!.parameterCount)
        assertEquals(ByteArray::class.java, writePacket.parameterTypes[0])

        // isSessionOpen() : boolean
        val isOpen = cls.declaredMethods.firstOrNull { it.name == "isSessionOpen" }
        assertNotNull("Protocol.isSessionOpen manquant", isOpen)
        assertEquals(java.lang.Boolean.TYPE, isOpen!!.returnType)
    }

    @Test
    fun `gomobile Auth class exposes new Story 9_7 surface`() {
        val cls = resolveClass("fr.plateformeliberte.levoile.core.auth.Auth")
        assertNotNull(cls)
        // Méthodes Story 9.2 (régression).
        assertHasMethod(cls, "tokenHeaderName")
        assertHasMethod(cls, "tokenTTLSeconds")
        assertHasMethod(cls, "tokenRefreshThresholdSeconds")
        // Méthodes Story 9.7.
        assertHasMethod(cls, "issueSessionToken")
        assertHasMethod(cls, "refreshSessionToken")
        assertHasMethod(cls, "validateSessionToken")
    }

    /**
     * **L-3** : signatures Auth — détection régression API silencieuse.
     */
    @Test
    fun `gomobile Auth method signatures match Story 9_7 expectations`() {
        val cls = resolveClass("fr.plateformeliberte.levoile.core.auth.Auth")

        // issueSessionToken(String, String) : String (token base64)
        val issue = cls.declaredMethods.firstOrNull { it.name == "issueSessionToken" }
        assertNotNull("Auth.issueSessionToken manquant", issue)
        assertEquals(2, issue!!.parameterCount)
        assertEquals(String::class.java, issue.returnType)

        // refreshSessionToken() : String
        val refresh = cls.declaredMethods.firstOrNull { it.name == "refreshSessionToken" }
        assertNotNull("Auth.refreshSessionToken manquant", refresh)
        assertEquals(0, refresh!!.parameterCount)
        assertEquals(String::class.java, refresh.returnType)

        // validateSessionToken(String) : boolean
        val validate = cls.declaredMethods.firstOrNull { it.name == "validateSessionToken" }
        assertNotNull("Auth.validateSessionToken manquant", validate)
        assertEquals(1, validate!!.parameterCount)
        assertEquals(String::class.java, validate.parameterTypes[0])
        assertEquals(java.lang.Boolean.TYPE, validate.returnType)

        // tokenTTLSeconds() : long (Go int64 → Java long)
        val ttl = cls.declaredMethods.firstOrNull { it.name == "tokenTTLSeconds" }
        assertNotNull("Auth.tokenTTLSeconds manquant", ttl)
        assertEquals(java.lang.Long.TYPE, ttl!!.returnType)
    }

    @Test
    fun `gomobile PacketCallback interface is resolvable`() {
        val cls = resolveClass("fr.plateformeliberte.levoile.core.protocol.PacketCallback")
        assertNotNull(cls)
        assertTrue(
            "${cls.name} doit être une interface (gomobile binding d'un Go interface)",
            cls.isInterface,
        )
        assertHasMethod(cls, "onPacketReceived", ByteArray::class.java)
    }

    @Test
    fun `gomobile StatusCallback interface is resolvable`() {
        val cls = resolveClass("fr.plateformeliberte.levoile.core.protocol.StatusCallback")
        assertNotNull(cls)
        assertTrue(
            "${cls.name} doit être une interface (gomobile binding d'un Go interface)",
            cls.isInterface,
        )
        assertHasMethod(cls, "onStateChange", String::class.java, String::class.java)
    }

    // ----- Surface Kotlin GoCoreAdapter (frontière étroite ADR-09) -----

    @Test
    fun `GoCoreAdapter Kotlin singleton exposes documented methods`() {
        // Réflexion sur les fonctions déclarées du singleton (object).
        // Les fonctions retournant Result<T> sont mangled par le compilateur
        // Kotlin (Result<T> est une inline value class) — leur nom JVM porte
        // un suffixe "-<hash>" (ex. connect-0E7RQCE). On compare donc par
        // préfixe (split sur '-').
        val baseNames = GoCoreAdapter::class.java.declaredMethods
            .map { it.name.substringBefore('-') }
            .toSet()

        listOf(
            "connect",
            "disconnect",
            "writePacket",
            "requestSessionToken",
            "refreshSessionToken",
            "isSessionTokenValid",
            "setCallbacks",
            "isSessionOpen",
            "protocolVersion",
            "framingHeaderSize",
        ).forEach { name ->
            assertTrue(
                "GoCoreAdapter.$name attendu dans la surface (architecture l. 1059-1060) — " +
                    "noms de base trouvés : ${baseNames.sorted()}",
                baseNames.contains(name),
            )
        }
    }

    @Test
    fun `LeVoileCoreException is a public Exception subclass`() {
        // Garantit que les consommateurs Kotlin peuvent catcher uniquement
        // LeVoileCoreException sans dépendre des types gomobile générés.
        val cls = LeVoileCoreException::class.java
        assertTrue(
            "LeVoileCoreException doit hériter de Exception",
            Exception::class.java.isAssignableFrom(cls),
        )
        // Constructeur (String, Throwable?)
        assertNotNull(cls.getDeclaredConstructor(String::class.java, Throwable::class.java))
    }

    @Test
    fun `PacketCallback Kotlin SAM has onPacketReceived(ByteArray)`() {
        assertHasMethod(PacketCallback::class.java, "onPacketReceived", ByteArray::class.java)
    }

    @Test
    fun `StatusCallback Kotlin SAM has onStateChange(String, String)`() {
        assertHasMethod(
            StatusCallback::class.java,
            "onStateChange",
            String::class.java,
            String::class.java,
        )
    }
}
