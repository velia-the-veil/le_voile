package fr.plateformeliberte.levoile.bridge

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import android.webkit.JavascriptInterface
import android.widget.Toast
import fr.plateformeliberte.levoile.MainActivity
import fr.plateformeliberte.levoile.R
import fr.plateformeliberte.levoile.conflict.VpnConflictDetector
import fr.plateformeliberte.levoile.conflict.VpnConflictVerdict
import fr.plateformeliberte.levoile.kill.KillSwitchDetector
import fr.plateformeliberte.levoile.kill.KillSwitchStatus
import fr.plateformeliberte.levoile.log.LeVoileLog
import fr.plateformeliberte.levoile.vpn.LeVoileVpnService

/**
 * Bridge JS↔Kotlin. Story 9.3 → 10.2 → 10.3 → 11.2 → 11.3.
 *
 * Story 11.2 enrichit avec connect/disconnect/selectCountry et un getStatus
 * dynamique lisant LeVoileVpnService.instance + KillSwitchDetector.
 * Story 11.3 ajoute openAppDetailsSettings.
 * Story 11.8 (à venir) migrera selectCountry vers ConfigStore.
 *
 * Le bridge est un adapter mince : il valide les inputs JS, marshalle vers les
 * helpers MainActivity ou les Intents LeVoileVpnService, et retourne du JSON.
 * Aucune logique métier côté bridge (cohérent architecture.md l. 1057-1061).
 *
 * @param context applicationContext pour SharedPreferences/Settings.Global ;
 *   peut aussi être l'Activity quand utilisé pour startActivity.
 * @param killSwitchDetector Story 10.2 — null en test JVM (retourne "Unverifiable").
 * @param vpnConflictDetector Story 10.3 — null en test JVM.
 */
class LeVoileBridge(
    private val context: Context,
    private val killSwitchDetector: KillSwitchDetector? = null,
    private val vpnConflictDetector: VpnConflictDetector? = null,
) {

    /**
     * Story 11.2 — getStatus dynamique. Remplace le placeholder Story 9.3.
     *
     * Lit LeVoileVpnService.instance (null = disconnected) et KillSwitchDetector.
     * Retour < 4 Ko (FR-AND-5 epics.md l. 1854).
     */
    @JavascriptInterface
    fun getStatus(): String {
        val instance = LeVoileVpnService.instance
        val killStatus = when (killSwitchDetector?.status?.value) {
            is KillSwitchStatus.Active -> "Active"
            is KillSwitchStatus.Inactive -> "Inactive"
            is KillSwitchStatus.Unverifiable -> "Unverifiable"
            null -> "Unverifiable"
        }
        val state = if (instance != null) "connected" else "disconnected"
        // Expose preferredCountry pour que le JS puisse synchroniser son etat
        // UI (drapeau pill + checkmark bottomsheet) avec le ConfigStore. Sans
        // ca, l'UI hardcode "DE" en initial et derive de l'etat reel quand
        // l'utilisatrice a deja selectionne un autre pays.
        val preferredCountry = try {
            fr.plateformeliberte.levoile.config.ConfigStore(context).load().preferredCountry
        } catch (_: Throwable) {
            "DE"
        }
        return """{"state":"$state","platform":"android","version":"$VERSION","killSwitchStatus":"$killStatus","preferredCountry":"$preferredCountry"}"""
    }

    /**
     * Story 11.2 — démarre le tunnel VPN via le système Android.
     *
     * Validation country whitelist [DE, ES, GB, US] (Story 3.8 distribution
     * relais MVP) ou null (round-robin Go). Cast safe Context → MainActivity ;
     * runOnUiThread car requestVpnStart utilise vpnConsentLauncher (main thread).
     */
    @JavascriptInterface
    fun connect(country: String?): String {
        val safeCountry = validateCountry(country)
        if (country != null && safeCountry == null) {
            return """{"error":"invalid_country_code","value":"${escapeJson(country.take(8))}"}"""
        }
        val activity = context as? MainActivity
            ?: return """{"error":"context_not_activity"}"""
        activity.runOnUiThread { activity.requestVpnStart(safeCountry) }
        val countryJson = if (safeCountry != null) "\"$safeCountry\"" else "null"
        return """{"ok":true,"action":"connect_requested","country":$countryJson}"""
    }

    /**
     * Story 11.2 — coupe le tunnel actif. No-op si service idle.
     */
    @JavascriptInterface
    fun disconnect(): String {
        val activity = context as? MainActivity
            ?: return """{"error":"context_not_activity"}"""
        if (LeVoileVpnService.instance == null) {
            return """{"ok":true,"action":"noop","reason":"service_idle"}"""
        }
        activity.runOnUiThread { activity.requestVpnStop() }
        return """{"ok":true,"action":"disconnect_requested"}"""
    }

    /**
     * Story 11.2 — sélectionne le pays préféré sans déclencher de connexion.
     *
     * Persistence SharedPreferences MODE_PRIVATE (UID-only, NFR-AND-7).
     * Story 11.8 migrera vers ConfigStore JSON (équivalent fonctionnel desktop TOML).
     */
    @JavascriptInterface
    fun selectCountry(iso: String?): String {
        val safe = validateCountry(iso)
            ?: return """{"error":"invalid_country_code","value":"${escapeJson(iso?.take(8) ?: "")}"}"""
        try {
            // Story 11.8 — migré vers ConfigStore JSON.
            fr.plateformeliberte.levoile.config.ConfigStore(context).update {
                it.copy(preferredCountry = safe)
            }
        } catch (t: Throwable) {
            LeVoileLog.w(TAG, "selectCountry persistence echoue: ${t.javaClass.simpleName}")
            return """{"error":"persistence_failed"}"""
        }
        return """{"ok":true,"country":"$safe"}"""
    }

    /**
     * Story 10.2 — retourne l'état courant du kill switch (protocole figé
     * "Active" | "Inactive" | "Unverifiable", consommé par app.js).
     */
    @JavascriptInterface
    fun getKillSwitchStatus(): String {
        val status = killSwitchDetector?.status?.value ?: KillSwitchStatus.Unverifiable
        return when (status) {
            is KillSwitchStatus.Active -> "Active"
            is KillSwitchStatus.Inactive -> "Inactive"
            is KillSwitchStatus.Unverifiable -> "Unverifiable"
        }
    }

    /**
     * Story 10.2 — ouvre Settings.ACTION_VPN_SETTINGS (kill switch flow).
     */
    @JavascriptInterface
    fun openKillSwitchTarget() {
        val intent = Intent(Settings.ACTION_VPN_SETTINGS)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        try {
            context.startActivity(intent)
        } catch (t: ActivityNotFoundException) {
            Toast.makeText(
                context,
                context.getString(R.string.android_c17_settings_unavailable),
                Toast.LENGTH_LONG,
            ).show()
        }
    }

    /**
     * Story 11.3 — ouvre la fiche Réglages > Apps > Le Voile (permissions,
     * notifications, force-stop). Différent de openKillSwitchTarget qui cible
     * VPN settings.
     */
    @JavascriptInterface
    fun openAppDetailsSettings(): String {
        val intent = Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS)
            .setData(Uri.fromParts("package", context.packageName, null))
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        return try {
            context.startActivity(intent)
            """{"ok":true,"action":"opened_app_details"}"""
        } catch (t: ActivityNotFoundException) {
            """{"error":"settings_unavailable"}"""
        }
    }

    /**
     * Story 10.3 — détection conflit VPN. Retourne JSON stable.
     */
    @JavascriptInterface
    fun checkVpnConflict(): String {
        val verdict = vpnConflictDetector?.check() ?: return "{\"verdict\":\"unverifiable\"}"
        return when (verdict) {
            is VpnConflictVerdict.NoConflict ->
                "{\"verdict\":\"no_conflict\"}"
            is VpnConflictVerdict.ConsentNotGiven ->
                "{\"verdict\":\"consent_required\"}"
            is VpnConflictVerdict.ForeignVpnActive -> {
                val safeAppId = verdict.foreignAppId
                    ?.filter { c ->
                        c in 'a'..'z' || c in 'A'..'Z' || c in '0'..'9' ||
                            c == '.' || c == '_'
                    }
                    ?.take(255)
                    ?: ""
                "{\"verdict\":\"foreign_vpn_active\",\"foreign_app_id\":\"$safeAppId\"}"
            }
        }
    }

    /**
     * Whitelist ISO 3166-1 alpha-2 — pays MVP Le Voile (cohérent Story 3.8).
     *
     * Validation stricte (pas de trim) : un input "  DE  " est probablement un
     * bug frontend, le refuser est une défense en profondeur.
     */
    private fun validateCountry(iso: String?): String? {
        if (iso == null) return null
        return if (iso in COUNTRIES_WHITELIST) iso else null
    }

    private fun escapeJson(s: String): String =
        s.filter { c -> c in ' '..'~' && c != '"' && c != '\\' }
            .take(8)

    companion object {
        private const val TAG = "LeVoileBridge"

        /**
         * JSON figé Story 9.3 — exposé en const pour testabilité JVM-only.
         * @Deprecated Story 11.2 — getStatus() est maintenant dynamique,
         * STATUS_JSON conservé pour rétro-compat tests legacy.
         */
        @Deprecated(
            "Story 11.2 — getStatus() est dynamique, STATUS_JSON conservé pour tests legacy uniquement.",
        )
        const val STATUS_JSON =
            """{"state":"placeholder","message":"Story 9.3 — squelette UI, noyau VPN à venir Story 9.4-9.7","platform":"android","version":"0.1.0"}"""

        /**
         * Story 11.2 — whitelist pays acceptés par connect/selectCountry.
         * Synchronisé avec FLAGS map JS (app.js Story 11.4) et CountryDisplay
         * Kotlin (Story 11.7).
         */
        val COUNTRIES_WHITELIST = setOf("DE", "ES", "GB", "US")

        /**
         * Story 11.2 — namespace SharedPreferences. Aligné OnboardingActivity
         * (Story 11.5) — single scope app.
         */
        const val PREFS_NAME = "levoile_prefs"

        /**
         * Story 11.2 — clé pays préféré. Story 11.8 migre cette clé vers
         * ConfigStore.preferredCountry.
         */
        const val PREF_KEY_PREFERRED_COUNTRY = "preferred_country"

        /**
         * Story 11.2 — version exposée dans getStatus(). Aligné BuildConfig.VERSION_NAME
         * (Story 9.1) — hard-codé ici pour testabilité JVM-only sans BuildConfig.
         */
        const val VERSION = "0.1.0"
    }
}
