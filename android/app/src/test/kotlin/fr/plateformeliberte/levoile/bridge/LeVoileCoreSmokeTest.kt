package fr.plateformeliberte.levoile.bridge

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/**
 * Charge une classe sans declencher son initializer statique. Les classes
 * generees par gomobile invoquent `System.loadLibrary("gojni")` dans leur
 * `<clinit>` ; sur la JVM des unit tests (sans device Android), cette
 * libgojni.so n'est pas disponible — d'ou UnsatisfiedLinkError /
 * ExceptionInInitializerError. On verifie ici uniquement la *resolution*
 * de la classe (preuve qu'elle est dans le classpath via le .aar) ; le
 * load JNI complet est valide cote instrumente Story 9.7.
 */
private fun resolveClass(name: String): Class<*> =
    Class.forName(name, false, LeVoileCoreSmokeTest::class.java.classLoader)

/**
 * Smoke test Story 9.2 — verifie que les classes generees par gomobile bind
 * sont resolvables au compile-time + class-loading runtime JVM.
 *
 * Ce test ne charge PAS le runtime JNI Go (qui requiert un device/emulateur
 * Android reel et un .so packagee dans l'APK — voir Story 9.7 pour le test
 * instrumente). Il valide uniquement la frontiere compile-time + le nommage
 * du package Java genere par gomobile via le flag `-javapkg=fr.plateformeliberte.levoile.core`.
 *
 * Les 5 classes attendues (constatees a l'inspection du .aar genere) :
 *   - fr.plateformeliberte.levoile.core.protocol.Protocol
 *   - fr.plateformeliberte.levoile.core.auth.Auth
 *   - fr.plateformeliberte.levoile.core.crypto.Crypto
 *   - fr.plateformeliberte.levoile.core.registry.Registry
 *   - fr.plateformeliberte.levoile.core.leakcheck.Leakcheck
 *
 * Pour que ce test passe, il faut avoir genere le .aar au prealable :
 *   bash android/scripts/build-aar.sh    # Linux/macOS
 *   pwsh android/scripts/build-aar.ps1   # Windows
 */
class LeVoileCoreSmokeTest {

    @Test
    fun `gomobile generated Protocol class is resolvable`() {
        assertNotNull(resolveClass("fr.plateformeliberte.levoile.core.protocol.Protocol"))
    }

    @Test
    fun `gomobile generated Auth class is resolvable`() {
        assertNotNull(resolveClass("fr.plateformeliberte.levoile.core.auth.Auth"))
    }

    @Test
    fun `gomobile generated Crypto class is resolvable`() {
        assertNotNull(resolveClass("fr.plateformeliberte.levoile.core.crypto.Crypto"))
    }

    @Test
    fun `gomobile generated Registry class is resolvable`() {
        assertNotNull(resolveClass("fr.plateformeliberte.levoile.core.registry.Registry"))
    }

    @Test
    fun `gomobile generated Leakcheck class is resolvable`() {
        assertNotNull(resolveClass("fr.plateformeliberte.levoile.core.leakcheck.Leakcheck"))
    }

    @Test
    fun `gomobile go support classes are resolvable`() {
        // Sanity check : les classes go.Seq et go.Universe doivent aussi etre presentes
        // dans le .aar (preuve que la chaine gomobile complete est embarquee).
        // Ces classes sont protegees par la rule ProGuard `-keep class go.** { *; }`
        // de Story 9.1 — ne pas la supprimer.
        assertNotNull(resolveClass("go.Seq"))
        assertNotNull(resolveClass("go.Universe"))
    }

    @Test
    fun `Protocol class has Version method with correct return type`() {
        // Validation methode pure-data sans declencher JNI loadLibrary :
        // on resout la methode statique via reflection + verifie le type
        // de retour. Ce check garantit que le shim android/shims/protocol/
        // a bien ete bound par gomobile et expose Version().
        val protocolClass = resolveClass("fr.plateformeliberte.levoile.core.protocol.Protocol")
        val versionMethod = try {
            protocolClass.getMethod("version")
        } catch (e: NoSuchMethodException) {
            fail(
                "Protocol.version() introuvable dans le .aar — gomobile a-t-il bind " +
                    "android/shims/protocol/protocol.go avec le flag -javapkg ? " +
                    "(${e.message})"
            )
            return  // unreachable, fail() throws
        }
        assertEquals(String::class.java, versionMethod.returnType)
        assertTrue(java.lang.reflect.Modifier.isStatic(versionMethod.modifiers))
    }
}
