package fr.plateformeliberte.levoile.conflict

import android.content.Intent

/**
 * Verdict de [VpnConflictDetector.check] — distingue 3 situations qui
 * exigent un traitement UX distinct (cohérent FR-AND-6 prd.md l. 614 et
 * ADR-10 architecture.md l. 2413-2416).
 */
sealed class VpnConflictVerdict {

    /** Aucun conflit, consent déjà accordé — le tunnel peut démarrer immédiatement. */
    object NoConflict : VpnConflictVerdict()

    /**
     * Consentement Le Voile pas encore donné (premier lancement, ou consent
     * révoqué via Settings → Apps → Le Voile → Permissions → VPN). L'Intent
     * système Android obtenu via `VpnService.prepare()` est transporté ici
     * — à passer à `startActivityForResult` (Story 11.5) pour présenter le
     * popup système natif. **Non-recréable** par l'app : si on ne le
     * conserve pas, on perd la capacité de présenter le popup dans cette
     * passe (le consumer doit re-checker pour obtenir un Intent frais).
     */
    data class ConsentNotGiven(val prepareIntent: Intent) : VpnConflictVerdict()

    /**
     * Une autre app VPN détient le slot Android. `foreignAppId` peut être
     * null dans le cas rare où `prepare()` retourne non-null mais
     * `Settings.Global.always_on_vpn_app` est vide (slot orphelin laissé
     * par un VPN désinstallé sans cleanup).
     */
    data class ForeignVpnActive(val foreignAppId: String?) : VpnConflictVerdict()
}
