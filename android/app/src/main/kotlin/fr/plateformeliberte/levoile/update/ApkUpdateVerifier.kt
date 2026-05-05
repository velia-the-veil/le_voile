package fr.plateformeliberte.levoile.update

import android.util.Base64
import java.security.MessageDigest
import java.security.PublicKey
import java.security.Signature
import java.security.spec.X509EncodedKeySpec
import java.security.KeyFactory

/**
 * Audit fix Android-§6 / R-T2 (2026-05-04). Verifies an Ed25519 signature
 * + SHA-256 checksum + monotonic version code over a downloaded APK
 * before the user is prompted to install it. The pubkey is shipped
 * inside the running app (see [trustedPubKeyBase64]) so the canonical
 * trust anchor is whatever Le Voile build was previously installed —
 * an attacker who substitutes a malicious APK on GitHub releases
 * cannot also rotate the pubkey embedded in the app.
 *
 * Three-layer policy:
 *   1. SHA-256(apk_bytes) must match the value advertised in the
 *      manifest (defense-in-depth against transport corruption).
 *   2. Ed25519 signature over `${versionCode}|${sha256}` must verify
 *      against the embedded pubkey (signing oracle compromise →
 *      whole-fleet vulnerable, signing oracle still safe → manifest
 *      cannot be forged).
 *   3. versionCode must be strictly greater than the currently
 *      installed versionCode (anti-rollback — defends against an
 *      attacker who has the signing key but is forced to use it on a
 *      known-vulnerable older build).
 *
 * The verifier is INERT until [trustedPubKeyBase64] is populated with a
 * non-empty Ed25519 raw-32-byte X.509 SubjectPublicKeyInfo encoded base64
 * string (see keystore/APK-SIGNATURE-MANIFEST.md). Until then the worker
 * skips the new code path and falls back to the manual install flow.
 */
internal object ApkUpdateVerifier {

    /**
     * Embedded Ed25519 trust anchor for the APK update channel. Empty
     * string means the verifier is disabled (production builds will
     * populate this via Gradle resValue at release time once the
     * release CI publishes signed manifests).
     */
    const val TRUSTED_PUB_KEY_BASE64: String = ""

    sealed class Result {
        object Disabled : Result()
        object Ok : Result()
        data class Rejected(val reason: String) : Result()
    }

    data class Manifest(
        val versionCode: Long,
        val sha256Hex: String,
        val signatureBase64: String,
    )

    /**
     * Verifies [apkBytes] against [manifest] for installation as a
     * direct-channel update. [installedVersionCode] is the currently
     * running app's versionCode used for the anti-rollback check.
     */
    fun verify(
        apkBytes: ByteArray,
        manifest: Manifest,
        installedVersionCode: Long,
    ): Result {
        if (TRUSTED_PUB_KEY_BASE64.isEmpty()) {
            return Result.Disabled
        }
        if (manifest.versionCode <= installedVersionCode) {
            return Result.Rejected("version_rollback: ${manifest.versionCode} <= $installedVersionCode")
        }
        val computed = sha256Hex(apkBytes)
        if (!computed.equals(manifest.sha256Hex, ignoreCase = true)) {
            return Result.Rejected("sha256_mismatch")
        }
        val pubKey = decodePubKey(TRUSTED_PUB_KEY_BASE64)
            ?: return Result.Rejected("pubkey_decode_failed")
        val sigBytes = runCatching { Base64.decode(manifest.signatureBase64, Base64.DEFAULT) }
            .getOrNull() ?: return Result.Rejected("signature_decode_failed")
        if (sigBytes.size != ED25519_SIG_LEN) {
            return Result.Rejected("signature_wrong_length")
        }
        val payload = "${manifest.versionCode}|${computed}".toByteArray(Charsets.US_ASCII)
        val verified = runCatching {
            val sig = Signature.getInstance("Ed25519")
            sig.initVerify(pubKey)
            sig.update(payload)
            sig.verify(sigBytes)
        }.getOrDefault(false)
        return if (verified) Result.Ok else Result.Rejected("signature_invalid")
    }

    private fun sha256Hex(bytes: ByteArray): String {
        val md = MessageDigest.getInstance("SHA-256")
        val digest = md.digest(bytes)
        val sb = StringBuilder(digest.size * 2)
        for (b in digest) {
            sb.append(HEX[(b.toInt() shr 4) and 0x0F])
            sb.append(HEX[b.toInt() and 0x0F])
        }
        return sb.toString()
    }

    private fun decodePubKey(base64: String): PublicKey? {
        return runCatching {
            val raw = Base64.decode(base64, Base64.DEFAULT)
            val spec = X509EncodedKeySpec(raw)
            KeyFactory.getInstance("Ed25519").generatePublic(spec)
        }.getOrNull()
    }

    private const val ED25519_SIG_LEN = 64
    private val HEX = "0123456789abcdef".toCharArray()
}
