package fr.plateformeliberte.levoile.conflict

import android.content.Context
import fr.plateformeliberte.levoile.BuildConfig
import fr.plateformeliberte.levoile.kill.ContentResolverSettingsReader
import fr.plateformeliberte.levoile.kill.SettingsReader

/**
 * Détecteur de conflit VPN — combine `VpnService.prepare(context)` +
 * heuristique `Settings.Global.always_on_vpn_app` pour produire un
 * [VpnConflictVerdict] qui distingue 3 cas (cohérent FR-AND-6
 * prd.md l. 614, architecture.md l. 580-584, l. 757) :
 *  - [VpnConflictVerdict.NoConflict] : tunnel peut démarrer immédiatement.
 *  - [VpnConflictVerdict.ConsentNotGiven] : popup système consent à présenter.
 *  - [VpnConflictVerdict.ForeignVpnActive] : autre app VPN détient le slot.
 *
 * **Stateless** : pas de cache, pas de `LiveData`. Chaque appel à [check]
 * re-mesure depuis zéro. La détection est ponctuelle (au tap « Connecter »
 * — Story 11.2), pas continue.
 *
 * **Réutilise [SettingsReader] de Story 10.1** : permet une couche
 * d'injection partagée entre `KillSwitchDetector` et `VpnConflictDetector`,
 * cohérent avec la coordination prévue (cf. Story 10.1 § Coordination 10.3).
 */
class VpnConflictDetector internal constructor(
    private val context: Context,
    private val settingsReader: SettingsReader,
    private val expectedAppId: String,
    private val preparer: VpnPreparer,
) {

    /**
     * Constructeur public — wirage runtime via [ContentResolverSettingsReader],
     * `BuildConfig.APPLICATION_ID` et [RealVpnPreparer]. Le constructeur
     * primaire (avec injections) reste `internal` pour que le module `:app`
     * puisse l'utiliser en test sans exposer `SettingsReader` / `VpnPreparer`
     * (interfaces `internal`).
     */
    constructor(context: Context) : this(
        context,
        ContentResolverSettingsReader(context.contentResolver),
        BuildConfig.APPLICATION_ID,
        RealVpnPreparer(),
    )

    /**
     * Évalue le conflit VPN courant. Aucun side-effect — purement
     * lecture (pas de log, pas de mutation Settings).
     */
    fun check(): VpnConflictVerdict {
        val prepareIntent = preparer.prepare(context)
            ?: return VpnConflictVerdict.NoConflict

        val pinnedApp: String? = try {
            settingsReader.getString(KEY_ALWAYS_ON_VPN_APP)
        } catch (t: Throwable) {
            // Heuristique cassée → classification prudente : présume
            // ConsentNotGiven (présenter le popup système est toujours sûr,
            // pire cas l'utilisateur consent à nouveau). Cohérent AC #4
            // de Story 10.3.
            return VpnConflictVerdict.ConsentNotGiven(prepareIntent)
        }

        return when {
            // Pas de VPN permanent configuré → consent jamais donné par
            // l'utilisateur pour Le Voile. Présenter le popup système.
            pinnedApp == null ->
                VpnConflictVerdict.ConsentNotGiven(prepareIntent)

            // Le Voile est pinné comme VPN permanent mais prepare() retourne
            // non-null → consent révoqué entre-temps (rare). Re-présenter
            // le popup, pas un conflit étranger.
            pinnedApp == expectedAppId ->
                VpnConflictVerdict.ConsentNotGiven(prepareIntent)

            // Autre app VPN détient le slot — conflit explicite à signaler.
            else ->
                VpnConflictVerdict.ForeignVpnActive(pinnedApp)
        }
    }

    private companion object {
        private const val KEY_ALWAYS_ON_VPN_APP = "always_on_vpn_app"
    }
}
