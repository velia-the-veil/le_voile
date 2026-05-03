package fr.plateformeliberte.levoile.bridge

import android.content.Context
import android.webkit.JavascriptInterface

/**
 * Bridge JS↔Kotlin — STUB Story 9.3.
 *
 * Cette classe expose UNIQUEMENT getStatus() avec une réponse placeholder.
 * Le bridge complet (connect, disconnect, selectCountry, getRegistry, checkLeak,
 * openVpnSettings, openBatteryOptimizationSettings, isAlwaysOnEnabled,
 * getPreferences, setPreference, quit — voir architecture.md l. 612-624) est
 * livré Story 11.2 ; les méthodes liées au tunnel dépendent de Story 9.4-9.7
 * (VpnService + Foreground + .aar gomobile).
 *
 * Ne PAS étendre cette classe avec d'autres méthodes @JavascriptInterface dans
 * le scope de Story 9.3 — voir Périmètre de modification de la story.
 *
 * @param context Conservé pour Story 11.2 qui aura besoin d'accéder à
 *   SharedPreferences, Settings.Global, ContentResolver, etc. **Non-null** :
 *   M-1 (code-review 9.3) a restauré la non-nullabilité de la spec d'origine.
 *   En runtime, MainActivity passe `applicationContext` (cf. M-3 même review).
 *   Tests JVM-only : utilisent directement la const [STATUS_JSON] sans
 *   instancier la classe — voir MainActivityConfigTest.
 */
class LeVoileBridge(@Suppress("unused") private val context: Context) {

    /**
     * Retourne le JSON status placeholder. Consommé par assets/app.js toutes
     * les 2 secondes via setInterval. Le contenu est exposé en companion
     * [STATUS_JSON] pour permettre la vérification JVM-only sans instancier
     * cette classe (Story 11.2 remplacera ce stub par une logique dynamique
     * lisant l'état de LeVoileVpnService + applicationContext).
     */
    @JavascriptInterface
    fun getStatus(): String = STATUS_JSON

    companion object {
        /**
         * JSON figé Story 9.3 — exposé en const pour testabilité JVM-only sans
         * instanciation (cohérent stratégie Story 9.4 : pas de Robolectric).
         * Valeurs vérifiées par MainActivityConfigTest :
         *   - state == "placeholder"
         *   - platform == "android"
         *   - message contient "Story 9.3"
         * Story 11.2 remplacera ce stub par un getStatus() dynamique relayant
         * l'état réel du tunnel via LeVoileVpnService.instance + GoCoreAdapter.
         */
        const val STATUS_JSON =
            """{"state":"placeholder","message":"Story 9.3 — squelette UI, noyau VPN à venir Story 9.4-9.7","platform":"android","version":"0.1.0"}"""
    }
}
