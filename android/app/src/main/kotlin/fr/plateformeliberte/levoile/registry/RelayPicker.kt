package fr.plateformeliberte.levoile.registry

import fr.plateformeliberte.levoile.log.LeVoileLog
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger

/**
 * Story 11.7-bis — Sélecteur de relais round-robin intra-pays.
 *
 * **Refactor post-code-review (M-3 + M-9)** :
 *  - `countersByCountry` est désormais [ConcurrentHashMap] — `getOrPut`
 *    lambda atomique vs `mutableMapOf` qui pouvait créer 2 [AtomicInteger]
 *    en concurrent (race).
 *  - Pré-extraction des relais côté Kotlin via [RegistryLoader.RegistryData.relays]
 *    (parsing JSON local) → le `pick(iso)` est pure Kotlin, **zéro JNI** dans
 *    le hot path. Le shim Go reste utilisé uniquement pour le `parseAndVerify`
 *    Ed25519 (1 seul call à load-time, pas par pick).
 *
 * **Round-robin par pays** : counter séparé par pays pour éviter qu'un pays
 * peu sollicité saute des relais.
 *
 * **Thread-safety** : `pick(iso)` est appelable depuis n'importe quel thread.
 *
 * **Fallback** : si le pays demandé n'a pas de relais (registre vide ou
 * pays hors whitelist), retourne `null` — le caller [LeVoileVpnService]
 * fallback sur `NoOpPacketRelay` plutôt que de crash.
 */
internal class RelayPicker(private val registryData: RegistryLoader.RegistryData) {

    /**
     * Counter round-robin par pays. ConcurrentHashMap garantit l'atomicité
     * de `getOrPut` (computeIfAbsent sous-jacent) — pas de race sur la
     * création du counter.
     */
    private val countersByCountry = ConcurrentHashMap<String, AtomicInteger>()

    /**
     * Sélectionne un relais pour le pays demandé. Retourne `(iso, domain, pubKeyB64)`
     * ou `null` si aucun relais disponible pour ce pays.
     *
     * Code-review post-11.7-bis (M-9) : pure Kotlin sur la liste pré-extraite
     * `registryData.relays`. Plus aucun appel JNI ici — le hot path pick est
     * O(N) Kotlin sur ~10 relais. La cohérence domain/pubKey est garantie
     * structurellement (même `RelayInfo`).
     *
     * @param iso code ISO 3166-1 alpha-2 majuscules (« DE », « ES », « GB », « US »)
     * @return [RelayInfo] sur succès, `null` sinon (fallback NoOp côté Service)
     */
    fun pick(iso: String): RelayInfo? {
        if (iso.isBlank()) {
            LeVoileLog.w(TAG, "pick avec iso vide")
            return null
        }
        val matching = registryData.relays.filter { it.iso.equals(iso, ignoreCase = true) }
        if (matching.isEmpty()) {
            LeVoileLog.w(TAG, "pick : aucun relais pour le pays demande")
            return null
        }
        val counter = countersByCountry.computeIfAbsent(iso.uppercase()) { AtomicInteger(0) }
        val rrIndex = counter.getAndIncrement()
        // Modulo positif (Kotlin Int.rem peut retourner négatif si overflow).
        val idx = ((rrIndex % matching.size) + matching.size) % matching.size
        return matching[idx]
    }

    /**
     * Réinitialise le counter pour un pays donné. Utile pour les tests, ou
     * après un refresh registry où l'ordre des relais peut avoir changé.
     */
    fun resetCounter(iso: String) {
        countersByCountry[iso.uppercase()]?.set(0)
    }

    /**
     * Information minimale pour invoquer `GoCoreAdapter.connect(domain, pubKey)`
     * + identifier le pays effectif côté Service (notification + status).
     */
    internal data class RelayInfo(
        val iso: String,
        val domain: String,
        val pinnedKeyB64: String,
    )

    companion object {
        private const val TAG = "RelayPicker"
    }
}
