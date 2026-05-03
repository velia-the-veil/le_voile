package fr.plateformeliberte.levoile.kill

import android.content.Context
import android.util.Log
import androidx.lifecycle.LiveData
import androidx.lifecycle.MutableLiveData
import fr.plateformeliberte.levoile.BuildConfig

/**
 * Détecteur du « kill switch OS-délégué » Android.
 *
 * Lit l'heuristique non-publique `Settings.Global.always_on_vpn_app` +
 * `Settings.Global.always_on_vpn_lockdown` (cf. architecture.md l. 1078,
 * l. 2191) et publie un [KillSwitchStatus] observable via [status].
 *
 * **Heuristique fragile** : Android peut restreindre l'accès à ces clés
 * dans une future version (mécanisme `_PROTECTED_NAMESPACES` Android 11+
 * existe déjà). Le fallback [KillSwitchStatus.Unverifiable] couvre ce cas
 * — l'UI alerte alors via bandeau C17 (Story 10.2) avec un texte
 * « non vérifiable ».
 *
 * **Pas de StateFlow / pas de cache** : LiveData suffit (rejoue le
 * dernier état à tout nouvel observer), pas besoin de Coroutines pour
 * un appel synchrone < 1 ms typique.
 *
 * Cohérent ADR-10 (architecture.md l. 2413-2416), FR-AND-2,
 * NFR-AND-9, NFR22a.
 */
class KillSwitchDetector internal constructor(
    private val reader: SettingsReader,
    private val expectedAppId: String,
) {

    /**
     * Constructeur public — wirage runtime via [ContentResolverSettingsReader]
     * et `BuildConfig.APPLICATION_ID`.
     *
     * `expectedAppId = BuildConfig.APPLICATION_ID` — comparaison stricte
     * avec le package effectivement installé, suffixe `.debug` inclus si
     * applicable (cf. `app/build.gradle.kts` `applicationIdSuffix`).
     * Cohérent avec l'expérience utilisateur : l'utilisatrice active le
     * VPN permanent sur l'app effectivement présente dans Settings → Apps.
     */
    constructor(context: Context) : this(
        reader = ContentResolverSettingsReader(context.contentResolver),
        expectedAppId = BuildConfig.APPLICATION_ID,
    )

    private val _status: MutableLiveData<KillSwitchStatus> =
        MutableLiveData(KillSwitchStatus.Unverifiable)

    /**
     * État observable du kill switch. Initialisé à
     * [KillSwitchStatus.Unverifiable] (état prudent par défaut — on ne
     * sait pas tant qu'on n'a pas mesuré). Mis à jour par [refresh].
     *
     * Story 10.2 ajoutera un observer dans `MainActivity` pour pousser
     * l'état au frontend JS via `LeVoileBridge`.
     */
    val status: LiveData<KillSwitchStatus> get() = _status

    /**
     * Ré-évalue l'heuristique et publie le résultat sur [status].
     *
     * Appel synchrone (non `suspend`), thread-safe via
     * `MutableLiveData.postValue`. À invoquer depuis `Activity.onResume()`
     * — Android ne broadcaste pas les changements de
     * `always_on_vpn_app`, donc la seule occasion fiable de re-vérifier
     * est le retour au premier plan.
     */
    fun refresh() {
        val pinnedApp: String? = try {
            reader.getString(KEY_ALWAYS_ON_VPN_APP)
        } catch (t: Throwable) {
            Log.i(TAG, "KillSwitchDetector: heuristique always_on_vpn_app indisponible sur ce device")
            _status.postValue(KillSwitchStatus.Unverifiable)
            return
        }
        val lockdownEnabled: Int = try {
            reader.getInt(KEY_ALWAYS_ON_VPN_LOCKDOWN, 0)
        } catch (t: Throwable) {
            Log.i(TAG, "KillSwitchDetector: heuristique always_on_vpn_lockdown indisponible sur ce device")
            _status.postValue(KillSwitchStatus.Unverifiable)
            return
        }

        // Matrice de classification — cf. AC #4 de Story 10.1. Arbre explicite
        // 4 branches (pas de simplification : chaque ligne de la matrice de la
        // story est représentée individuellement pour faciliter la traçabilité
        // ADR-10 / FR-AND-2 / architecture.md l. 1078).
        val resolved: KillSwitchStatus = when {
            // L1 — Le Voile pinné + lockdown ON → kill switch effectif.
            pinnedApp == expectedAppId && lockdownEnabled == 1 ->
                KillSwitchStatus.Active

            // L2 — Le Voile pinné mais lockdown OFF → trafic non-VPN passe librement.
            pinnedApp == expectedAppId ->
                KillSwitchStatus.Inactive

            // L3 — Autre app VPN tient le slot (Tailscale, Wireguard…) — Le Voile
            // n'est pas pinné. Story 10.3 traite séparément le refus de démarrer
            // si conflit.
            pinnedApp != null ->
                KillSwitchStatus.Inactive

            // L4 — Aucun VPN permanent configuré → utilisatrice DOIT être
            // alertée (bandeau C17 — Story 10.2).
            else ->
                KillSwitchStatus.Inactive
        }
        _status.postValue(resolved)
    }

    private companion object {
        private const val TAG = "KillSwitchDetector"
        private const val KEY_ALWAYS_ON_VPN_APP = "always_on_vpn_app"
        private const val KEY_ALWAYS_ON_VPN_LOCKDOWN = "always_on_vpn_lockdown"
    }
}
