package fr.plateformeliberte.levoile.security

import android.content.pm.PackageManager
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import fr.plateformeliberte.levoile.BuildConfig
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assume
import org.junit.Test
import org.junit.runner.RunWith
import java.security.MessageDigest

/**
 * Story 12.3 squelette → Story 12.6 impl runtime.
 *
 * Vérifie que l'APK installé sur l'émulateur est signé par le certificat
 * attendu (SHA256 fingerprint hardcodé après génération master keystore
 * Story 12.3 Task 8 + provisionnement secrets GitHub Actions).
 *
 * Sur build debug : skip (le cert debug AGP est stable PAR machine mais pas
 * reproductible across machines — pas exploitable comme garantie de chaîne
 * de confiance).
 *
 * Le fingerprint canonique est extrait via :
 *   `keytool -list -v -keystore android/keystore/levoile-release.jks
 *      -alias levoile-master-2026 -storepass <password> | grep SHA256:`
 *
 * À compléter dans `EXPECTED_RELEASE_FINGERPRINT_SHA256` ci-dessous post-Story
 * 12.3 Task 8 (cf. docs/key-management-android.md).
 */
@RunWith(AndroidJUnit4::class)
class SignatureValidationTest {

    /**
     * Garde-fou structurel runnable sur debug ET release : l'APK installé
     * doit avoir une signature non-vide (un signer au minimum). Sans cette
     * vérification, un APK mal-configuré qui shipperait `signingConfig = null`
     * passerait silencieusement la matrice.
     *
     * Ajouté Code Review 2026-05-03 — combat le code mort détecté
     * (M5 : test fingerprint skip systématiquement).
     */
    @Test
    fun `APK_signing_info_present_et_non_vide`() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext

        @Suppress("DEPRECATION")
        val pi = ctx.packageManager.getPackageInfo(
            ctx.packageName,
            PackageManager.GET_SIGNING_CERTIFICATES,
        )
        val signingInfo = pi.signingInfo
        assertNotNull(
            "PackageInfo.signingInfo doit etre non-null sur API 28+ — APK non signe " +
                "ou AGP signingConfig casse",
            signingInfo,
        )
        val signers = signingInfo!!.apkContentsSigners
        org.junit.Assert.assertTrue(
            "Au moins 1 signer requis (debug ou release) — APK non signe ?",
            signers.isNotEmpty(),
        )
    }

    @Test
    fun `cert_APK_installe_match_fingerprint_attendu`() {
        // Sur debug build, le cert est l'AGP debug keystore — pas notre master key.
        // Skip pour éviter un faux positif. Le test runtime se vérifie post-tag
        // release dans un job dédié `signed-instrumented-tests` (Phase 2).
        Assume.assumeFalse(
            "Test skip sur build debug (cert AGP debug = pas notre master key)",
            BuildConfig.DEBUG,
        )
        Assume.assumeFalse(
            "Test skip tant que EXPECTED_RELEASE_FINGERPRINT_SHA256 n'est pas renseigné post-Story 12.3 Task 8",
            EXPECTED_RELEASE_FINGERPRINT_SHA256 == "TODO_FILL_AFTER_12_3_TASK_8",
        )

        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val pkg = ctx.packageName

        @Suppress("DEPRECATION")
        val pi = ctx.packageManager.getPackageInfo(
            pkg,
            PackageManager.GET_SIGNING_CERTIFICATES,
        )
        val signingInfo = pi.signingInfo
        assertNotNull("PackageInfo.signingInfo doit etre non-null sur API 28+", signingInfo)

        val signers = signingInfo!!.apkContentsSigners
        assertEquals(
            "Un seul signer attendu (master key Le Voile, pas de v3 lineage MVP)",
            1,
            signers.size,
        )

        val md = MessageDigest.getInstance("SHA-256")
        val fingerprint = md.digest(signers[0].toByteArray())
            .joinToString(":") { "%02X".format(it) }

        assertEquals(
            "Fingerprint cert APK ne match pas la master key Le Voile (Story 12.3). " +
                "Verifier docs/key-management-android.md procedure rotation incident " +
                "ou EXPECTED_RELEASE_FINGERPRINT_SHA256 obsolete. Actuel : $fingerprint",
            EXPECTED_RELEASE_FINGERPRINT_SHA256,
            fingerprint,
        )
    }

    companion object {
        /**
         * Fingerprint SHA256 attendu du certificat APK release.
         *
         * À compléter par le mainteneur dès Story 12.3 Task 8 livrée
         * (génération keystore + provisionnement secrets GitHub Actions).
         *
         * Récupération :
         * ```
         * keytool -list -v -keystore android/keystore/levoile-release.jks \
         *     -alias levoile-master-2026 -storepass <password> | grep SHA256:
         * ```
         */
        private const val EXPECTED_RELEASE_FINGERPRINT_SHA256 = "TODO_FILL_AFTER_12_3_TASK_8"
    }
}
