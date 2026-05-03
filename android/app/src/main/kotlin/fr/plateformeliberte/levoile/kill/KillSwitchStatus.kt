package fr.plateformeliberte.levoile.kill

/**
 * État du « kill switch OS-délégué » Android tel que perçu par
 * [KillSwitchDetector] via l'heuristique `Settings.Global.always_on_vpn_app`
 * + `Settings.Global.always_on_vpn_lockdown`.
 *
 * Trois valeurs métier (cohérent ADR-10 — architecture.md l. 2413-2416 et
 * FR-AND-2 / NFR-AND-9) :
 *  - [Active] : Le Voile est l'app pinnée comme « VPN permanent » ET
 *    l'option « Bloquer connexions sans VPN » (lockdown) est activée. Le
 *    kill switch OS est en place : tout trafic non-VPN est bloqué par le
 *    système Android.
 *  - [Inactive] : la protection n'est pas en place. Soit aucune app n'est
 *    pinnée comme VPN permanent, soit Le Voile est pinné sans lockdown,
 *    soit une autre app VPN tient le slot. Dans tous les cas, le kill
 *    switch n'est pas effectif pour Le Voile et l'utilisatrice doit être
 *    alertée (bandeau C17 — Story 10.2 — ou onboarding C15 — Story 11.6).
 *  - [Unverifiable] : l'heuristique a échoué (ROM custom, future
 *    restriction Android sur ces clés `_PROTECTED_NAMESPACES`, etc.). On
 *    ne peut pas conclure — l'UI doit afficher un texte « non vérifiable »
 *    plutôt qu'une fausse certitude.
 *
 * Choix `sealed class` (vs `enum class`) : cohérence Kotlin idiomatique
 * (`when` exhaustif), évolution future avec données possibles
 * (ex. `data class Inactive(val reason: String)`) sans casser l'API.
 */
sealed class KillSwitchStatus {
    object Active : KillSwitchStatus()
    object Inactive : KillSwitchStatus()
    object Unverifiable : KillSwitchStatus()
}
