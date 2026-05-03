package fr.plateformeliberte.levoile.config

/**
 * Story 11.8 — Schema persistance Le Voile Android.
 *
 * Sérialisé en JSON dans `getFilesDir()/config.json`. Permissions par défaut
 * Android (UID-only, équivalent 0600 desktop — NFR-AND-7).
 *
 * Champs MVP :
 *   - preferredCountry : code ISO 3166-1 alpha-2. Défaut "DE". Whitelist
 *     alignée LeVoileBridge.COUNTRIES_WHITELIST (Story 11.2).
 *   - registryCache : JSON string du dernier registre relais récupéré
 *     (placeholder Story 11.8, alimenté Story 9.7+).
 *   - lastVerifiedEd25519Key : empreinte hex de la master key Ed25519
 *     vérifiée (clé PUBLIQUE — non-secret). Pour TOFU + alerte rotation.
 *   - schemaVersion : version du schema config (1 initiale).
 *
 * Pas de champ secret (clés privées, tokens) — Android Keystore Phase 2 si requis.
 */
internal data class ConfigData(
    val schemaVersion: Int = CURRENT_SCHEMA_VERSION,
    val preferredCountry: String = "DE",
    val registryCache: String? = null,
    val lastVerifiedEd25519Key: String? = null,
) {
    companion object {
        /**
         * Version courante du schema. Incrémenter si on ajoute/retire un champ
         * persistance — déclencher migration via ConfigStore.migrate.
         */
        const val CURRENT_SCHEMA_VERSION = 1

        /** Config par défaut — utilisée premier lancement OU fichier corrompu. */
        val DEFAULT = ConfigData()
    }
}
