package fr.plateformeliberte.levoile.registry

import android.content.Context
import androidx.annotation.RawRes
import fr.plateformeliberte.levoile.R
import fr.plateformeliberte.levoile.config.ConfigStore
import fr.plateformeliberte.levoile.log.LeVoileLog
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException

/**
 * Story 11.7-bis — Loader registre relais Le Voile pour Android.
 *
 * Charge le `relay-registry.json` depuis 3 sources, en ordre de préférence :
 *  1. Cache `ConfigStore.registryCache` (Story 11.8) — si présent ET TTL
 *     non expiré (24h, code-review post-11.7-bis M-8).
 *  2. Bundle `res/raw/registry_bootstrap_relays` (cohérent Story 4.2 desktop).
 *  3. Fetch online via HTTPS (Story future — TODO si besoin de refresh online
 *     dans l'app au lieu de bundle).
 *
 * Vérifie la signature Ed25519 du registre via le shim Go
 * `core.registry.Registry.parseAndVerify(jsonBytes, expectedMasterPubKeyB64)`.
 * La master pubkey est bundled dans l'APK (`res/raw/registry_master_pubkey`)
 * pour empêcher un attaquant de servir un faux registre signé par SA propre
 * master key (TOFU — Trust On First Use).
 *
 * **Refactor post-code-review (M-7 + M-9)** :
 *  - Pré-extraction Kotlin des relais via `org.json.JSONObject` après
 *    verify Ed25519 OK. Le shim Go fait UN SEUL call de verify ; le pick
 *    consomme une `List<RelayPicker.RelayInfo>` Kotlin (zéro JNI).
 *  - `verifyRegistry` n'est plus invoqué 2x (le résultat de la 1ère vérif
 *    cache est réutilisé).
 *
 * **Sécurité** : aucune confiance n'est faite au cache `ConfigStore` —
 * chaque chargement re-vérifie la signature complète. Pas d'optimisation
 * sur ce point (le verify Ed25519 sur 4-8 entrées coûte < 5ms).
 *
 * **Cache TTL (M-8)** : 24h via `Updated` timestamp du registry. Si
 * timestamp absent ou parsing échoue, le cache est considéré expiré
 * (fail-closed sécurité).
 */
internal class RegistryLoader(private val context: Context) {

    /**
     * Charge le registre depuis le cache (préféré, si TTL OK) OU depuis le
     * bundle. Retourne le JSON brut + la liste de relais pré-extraite + le
     * pays effectif extrait des entrées.
     *
     * @return [RegistryData] sur succès, OU `null` si aucune source disponible.
     */
    suspend fun load(): RegistryData? {
        val store = ConfigStore(context)
        val config = store.load()

        // 1. Tentative cache (avec check TTL).
        config.registryCache?.let { cachedJson ->
            val cachedBytes = cachedJson.toByteArray(Charsets.UTF_8)
            val verified = verifyRegistry(cachedBytes)
            if (verified > 0) {
                if (cacheIsFresh(cachedJson)) {
                    LeVoileLog.i(TAG, "Registry charge depuis cache : $verified relais valides")
                    return parseAndBuildRegistryData(cachedBytes, verified)
                } else {
                    LeVoileLog.i(TAG, "Cache registry expire (>${CACHE_TTL_HOURS}h) — fallback bundle")
                }
            } else {
                LeVoileLog.w(TAG, "Cache registry invalide (signature/format) — fallback bundle")
            }
        }

        // 2. Fallback bundle.
        val bundled = readRawResource(R.raw.registry_bootstrap_relays) ?: return null
        val verified = verifyRegistry(bundled)
        if (verified <= 0) {
            // Bundle invalide = build cassé. Pas de fallback runtime —
            // l'app doit refuser de continuer (sécurité).
            LeVoileLog.e(TAG, "Bundle registry invalide — build casse ou tamper")
            return null
        }
        LeVoileLog.i(TAG, "Registry charge depuis bundle : $verified relais valides")

        // Persist le bundle dans le cache pour les prochains starts.
        try {
            store.update { it.copy(registryCache = String(bundled, Charsets.UTF_8)) }
        } catch (t: Throwable) {
            // Cache update non-bloquant — on continue avec le bundle en RAM.
            LeVoileLog.w(TAG, "Persist cache registry echoue : ${t.javaClass.simpleName}")
        }
        return parseAndBuildRegistryData(bundled, verified)
    }

    /**
     * Force le refresh du cache depuis le bundle (utile si une story future
     * livre un fetch online — pour l'instant rebascule simplement sur bundle).
     */
    suspend fun refreshFromBundle(): RegistryData? {
        val bundled = readRawResource(R.raw.registry_bootstrap_relays) ?: return null
        val verified = verifyRegistry(bundled)
        if (verified <= 0) {
            LeVoileLog.e(TAG, "Bundle registry invalide lors du refresh")
            return null
        }
        try {
            ConfigStore(context).update {
                it.copy(registryCache = String(bundled, Charsets.UTF_8))
            }
        } catch (t: Throwable) {
            LeVoileLog.w(TAG, "Persist cache registry echoue : ${t.javaClass.simpleName}")
        }
        return parseAndBuildRegistryData(bundled, verified)
    }

    /**
     * Parse le JSON Kotlin (org.json) après verify Ed25519 OK, extrait la
     * liste des relais sous forme [RelayPicker.RelayInfo]. Le pays ISO est
     * extrait via heuristique sur l'ID (`relay-{code}-...`) ou le domaine
     * (`{code}.levoile.dev` / `{code}-{num}.levoile.dev`) — cohérent
     * `internal/registry.ExtractCountryCode` côté desktop.
     *
     * **Important** : ce parse est invoqué APRÈS `verifyRegistry` qui a déjà
     * validé que tout le registry passe le verify Ed25519. Donc on parse
     * en confiance ici (les relais retournés ont leur signature OK).
     */
    private fun parseAndBuildRegistryData(
        jsonBytes: ByteArray,
        numRelays: Int,
    ): RegistryData? {
        val rawJson = String(jsonBytes, Charsets.UTF_8)
        val obj = try {
            JSONObject(rawJson)
        } catch (e: JSONException) {
            LeVoileLog.w(TAG, "parseAndBuildRegistryData : JSON invalide post-verify")
            return null
        }
        val relaysJson = obj.optJSONArray("relays") ?: return null
        val relays = mutableListOf<RelayPicker.RelayInfo>()
        for (i in 0 until relaysJson.length()) {
            val r = relaysJson.optJSONObject(i) ?: continue
            val id = r.optString("id", "")
            val domain = r.optString("domain", "")
            val pubKey = r.optString("public_key", "")
            if (domain.isBlank() || pubKey.isBlank()) continue
            val iso = extractCountryCode(id, domain).uppercase()
            if (iso.isBlank()) continue
            relays.add(RelayPicker.RelayInfo(iso = iso, domain = domain, pinnedKeyB64 = pubKey))
        }
        return RegistryData(
            jsonBytes = jsonBytes,
            numRelays = numRelays,
            relays = relays.toList(),
        )
    }

    /**
     * Code pays extrait de l'ID (`relay-{code}-{num}`) ou du domaine
     * (`{code}.levoile.dev` ou `{code}-{num}.levoile.dev`). Mirror Kotlin
     * de `internal/registry.ExtractCountryCode` (Story 9.2 shim).
     *
     * Renvoie "" si aucun pattern ne matche — le relais est ignoré au pick.
     */
    private fun extractCountryCode(id: String, domain: String): String {
        val supported = setOf("de", "es", "gb", "us", "fr", "is", "fi")
        // Format ID : relay-{code}-{num}
        if (id.startsWith("relay-")) {
            val rest = id.removePrefix("relay-")
            val dashIdx = rest.indexOf('-')
            if (dashIdx >= 2) {
                val code = rest.substring(0, dashIdx).lowercase()
                if (code in supported) return code
            }
        }
        // Format domaine : {code}.levoile.dev ou {code}-{num}.levoile.dev
        val dotIdx = domain.indexOf('.')
        if (dotIdx >= 2) {
            val prefix = domain.substring(0, dotIdx).lowercase()
            if (prefix in supported) return prefix
            val dashIdx = prefix.indexOf('-')
            if (dashIdx >= 2) {
                val code = prefix.substring(0, dashIdx)
                if (code in supported) return code
            }
        }
        return ""
    }

    /**
     * Cache TTL check (code-review post-11.7-bis M-8) : le registry est
     * considéré frais si `Updated` < 24h. Si le parsing échoue ou la date est
     * dans le futur (clock drift), fail-closed (cache expiré).
     */
    private fun cacheIsFresh(rawJson: String): Boolean {
        return try {
            val obj = JSONObject(rawJson)
            val updated = obj.optString("updated", "")
            if (updated.isBlank()) return false
            // Format ISO-8601 RFC3339 — Java Instant.parse standard.
            val updatedAt = java.time.Instant.parse(updated)
            val now = java.time.Instant.now()
            val ageMs = now.toEpochMilli() - updatedAt.toEpochMilli()
            ageMs in 0..(CACHE_TTL_HOURS * 3_600_000L)
        } catch (t: Throwable) {
            // Date manquante / format inattendu / clock skew → fail-closed.
            false
        }
    }

    /**
     * Délégue au shim Go `core.registry.Registry.parseAndVerify` qui :
     *  1. Vérifie que `r.master_public_key == expectedMasterPubKeyB64`
     *     (TOFU avec la clé bundled).
     *  2. Vérifie la signature Ed25519 de chaque entrée contre la master key.
     *
     * Retourne le nombre de relais valides (≥ 0). 0 si invalid.
     */
    private fun verifyRegistry(jsonBytes: ByteArray): Int {
        val expectedKey = readMasterPubKey() ?: return 0
        return try {
            // Délégation gomobile — wrap dans try/catch car le JNI peut throw
            // sur JSON malformé / decode error.
            fr.plateformeliberte.levoile.core.registry.Registry.parseAndVerify(
                jsonBytes,
                expectedKey,
            ).toInt()
        } catch (t: Throwable) {
            LeVoileLog.w(TAG, "verifyRegistry exception : ${t.javaClass.simpleName}")
            0
        }
    }

    /**
     * Lit la master pubkey Ed25519 depuis `res/raw/registry_master_pubkey`.
     * Format : base64 standard sur une ligne (32 octets Ed25519 → 44 chars
     * base64 + padding). Trim whitespace défensif.
     */
    private fun readMasterPubKey(): String? {
        val raw = readRawResource(R.raw.registry_master_pubkey) ?: return null
        return String(raw, Charsets.UTF_8).trim().takeIf { it.isNotEmpty() }
    }

    private fun readRawResource(@RawRes resId: Int): ByteArray? = try {
        context.resources.openRawResource(resId).use { it.readBytes() }
    } catch (e: IOException) {
        LeVoileLog.w(TAG, "readRawResource $resId echoue : ${e.javaClass.simpleName}")
        null
    } catch (e: android.content.res.Resources.NotFoundException) {
        LeVoileLog.w(TAG, "readRawResource $resId introuvable : ${e.javaClass.simpleName}")
        null
    }

    /**
     * Donnée registre validée + relais pré-extraits.
     */
    internal data class RegistryData(
        val jsonBytes: ByteArray,
        val numRelays: Int,
        val relays: List<RelayPicker.RelayInfo>,
    ) {
        // data class avec ByteArray nécessite equals/hashCode manuels
        // (sinon comparaison par référence par défaut).
        override fun equals(other: Any?): Boolean {
            if (this === other) return true
            if (other !is RegistryData) return false
            return numRelays == other.numRelays &&
                jsonBytes.contentEquals(other.jsonBytes) &&
                relays == other.relays
        }

        override fun hashCode(): Int {
            var result = jsonBytes.contentHashCode()
            result = 31 * result + numRelays
            result = 31 * result + relays.hashCode()
            return result
        }
    }

    companion object {
        private const val TAG = "RegistryLoader"
        private const val CACHE_TTL_HOURS = 24L
    }
}
