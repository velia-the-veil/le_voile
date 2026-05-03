package fr.plateformeliberte.levoile.bridge

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.provider.Settings
import android.webkit.JavascriptInterface
import android.widget.Toast
import fr.plateformeliberte.levoile.R
import fr.plateformeliberte.levoile.conflict.VpnConflictDetector
import fr.plateformeliberte.levoile.conflict.VpnConflictVerdict
import fr.plateformeliberte.levoile.kill.KillSwitchDetector
import fr.plateformeliberte.levoile.kill.KillSwitchStatus

/**
 * Bridge JS↔Kotlin — Story 9.3 (stub `getStatus`) + Story 10.2 (statut
 * kill switch + deeplink Settings.ACTION_VPN_SETTINGS).
 *
 * Le bridge complet (connect, disconnect, selectCountry, getRegistry,
 * checkLeak, openVpnSettings, openBatteryOptimizationSettings,
 * isAlwaysOnEnabled, getPreferences, setPreference, quit — voir
 * architecture.md l. 612-624) est livré Story 11.2 ; les méthodes liées
 * au tunnel dépendent de Story 9.4-9.7 (VpnService + Foreground + .aar
 * gomobile).
 *
 * Ne PAS étendre cette classe avec d'autres méthodes @JavascriptInterface
 * dans le scope de Story 10.2 — voir Périmètre de modification de la story.
 *
 * @param context Conservé pour Story 11.2 (SharedPreferences, Settings.Global,
 *   ContentResolver, etc.) ET utilisé Story 10.2 pour `startActivity` du
 *   deeplink Settings + Toast fallback.
 * @param killSwitchDetector Story 10.2 — détecteur consommé par
 *   [getKillSwitchStatus]. Nullable pour permettre l'instanciation de
 *   tests JVM-only sans détecteur (le contrat retourne alors `"Unverifiable"`,
 *   cohérent état prudent par défaut).
 */
class LeVoileBridge(
    private val context: Context,
    private val killSwitchDetector: KillSwitchDetector? = null,
    private val vpnConflictDetector: VpnConflictDetector? = null,
) {

    /**
     * Story 9.3 — JSON status placeholder. Story 11.2 remplacera ce stub
     * par une logique dynamique lisant l'état de `LeVoileVpnService` +
     * `applicationContext`.
     */
    @JavascriptInterface
    fun getStatus(): String = STATUS_JSON

    /**
     * Story 10.2 — retourne l'état courant du kill switch comme string
     * stable parmi `"Active" | "Inactive" | "Unverifiable"` (protocole
     * machine consommé par `assets/app.js`, AC #3 + #6).
     *
     * Pas de localisation — ces valeurs sont du protocole, pas du texte UI.
     * Si `killSwitchDetector` est null (cas de test ou erreur d'init),
     * retourne `"Unverifiable"` (état prudent par défaut, cohérent avec
     * l'initialisation Story 10.1).
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
     * Story 10.2 — ouvre le panneau natif Android « VPN » (`Settings.ACTION_VPN_SETTINGS`).
     * Branche fallback EBR-02 (architecture.md l. 2455-2461) — Story 11.6
     * enrichira pour ouvrir le composant C15 (onboarding kill switch screen)
     * si Epic 11 est livré, sans casser cette signature `@JavascriptInterface`.
     *
     * `FLAG_ACTIVITY_NEW_TASK` obligatoire si `context` peut être
     * `applicationContext` — défensif, fonctionne aussi quand `context` est
     * une Activity.
     *
     * Sur ROM custom où `ACTION_VPN_SETTINGS` n'est pas exposé,
     * `ActivityNotFoundException` déclenche un Toast pédagogique sans
     * donnée utilisateur (NFR-AND-9 : pas de log révélant l'info ROM custom).
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
     * Story 10.3 — détection de conflit VPN. Retourne une string JSON
     * stable consommée par `window.LeVoile.checkVpnConflict()` côté JS
     * (Story 11.2 cible cette méthode au tap « Connecter »).
     *
     * Contrat de retour (snake_case côté JSON, mapping vers
     * [VpnConflictVerdict] Kotlin) :
     *  - `{"verdict":"no_conflict"}` — tunnel peut démarrer
     *  - `{"verdict":"consent_required"}` — popup système Story 11.5
     *  - `{"verdict":"foreign_vpn_active","foreign_app_id":"..."}` — autre VPN
     *  - `{"verdict":"unverifiable"}` — détecteur null (cas test ou init incomplet)
     *
     * Le `foreign_app_id` est filtré sur la whitelist `[a-zA-Z0-9._]` +
     * tronqué à 255 chars — défense en profondeur contre une éventuelle
     * injection JSON / XSS si Story 11.2 affiche la valeur sans escape.
     *
     * Pas de transport d'`Intent` côté JS (non sérialisable cleanly) —
     * Story 11.5 re-callera `vpnConflictDetector.check()` au moment de
     * `requestVpnConsent()` pour récupérer un Intent frais (cohérent
     * avec le design « pas de cache » du détecteur).
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
                // Whitelist ASCII [a-zA-Z0-9._] strict — pas `isLetterOrDigit()`
                // (qui accepte aussi lettres/chiffres Unicode arabes, chinois,
                // etc.). Java package names sont ASCII par spec, donc ce filtre
                // ne perd aucun packageName légitime mais bloque toute tentative
                // d'injection via caractère Unicode exotique.
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

    companion object {
        /**
         * JSON figé Story 9.3 — exposé en const pour testabilité JVM-only sans
         * instanciation. Story 11.2 remplacera ce stub par un getStatus() dynamique.
         */
        const val STATUS_JSON =
            """{"state":"placeholder","message":"Story 9.3 — squelette UI, noyau VPN à venir Story 9.4-9.7","platform":"android","version":"0.1.0"}"""
    }
}
