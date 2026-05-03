package fr.plateformeliberte.levoile.ui

/**
 * Story 11.7 — Map ISO 3166-1 alpha-2 → drapeau emoji + nom français.
 *
 * Single source of truth Kotlin pour les pays affichés. Synchronisé avec :
 *   - LeVoileBridge.COUNTRIES_WHITELIST (Story 11.2) — codes acceptés
 *   - FLAGS map JS dans app.js (Story 11.4) — affichage frontend
 *
 * Toute extension future (5+ pays MVP) doit mettre à jour les 3 endroits.
 */
internal object CountryDisplay {

    data class Country(val iso: String, val flag: String, val frenchName: String)

    private val COUNTRIES = mapOf(
        "DE" to Country("DE", "🇩🇪", "Allemagne"),
        "ES" to Country("ES", "🇪🇸", "Espagne"),
        "GB" to Country("GB", "🇬🇧", "Royaume-Uni"),
        "US" to Country("US", "🇺🇸", "États-Unis"),
    )

    fun lookup(iso: String?): Country? = iso?.let { COUNTRIES[it.uppercase()] }

    /** « 🇩🇪 Allemagne » ou « — » si iso null/inconnu. */
    fun formatShort(iso: String?): String {
        val c = lookup(iso) ?: return "—"
        return "${c.flag} ${c.frenchName}"
    }

    /** « Allemagne » (sans drapeau) — pour TalkBack contentDescription. */
    fun formatTalkBack(iso: String?): String =
        lookup(iso)?.frenchName ?: "pays inconnu"
}
