package fr.plateformeliberte.levoile.vpn

/**
 * Constantes partagees entre LeVoileVpnService et ses futurs consommateurs
 * (MainActivity Story 9.5, NotificationHelper Story 9.6, GoCoreAdapter Story 9.7).
 *
 * Localisation : android/ (et NON un noyau Go partage) — ces constantes sont
 * Android-specifiques (taille buffer fd, IDs notification, action strings) et
 * n'ont aucun equivalent desktop. Coherent ADR-08 (isolation OS maximale) :
 * la duplication assumee s'applique aux constantes runtime de chaque OS.
 */
object VpnConstants {

    /** Taille max d'un paquet IP lu depuis le fd VpnService.
     *  32 768 = ample pour MTU 1420 + IPv6 jumbograms futurs. */
    const val MAX_IP_PACKET: Int = 32_768

    /** ID notification Foreground Service stub (Story 9.4) puis finale (Story 9.6).
     *  Hors-zero pour eviter conflit avec d'autres notifs IDLE Android. */
    const val NOTIF_ID: Int = 0xCEC1

    /** Channel ID stub Story 9.4 — sera supprime Story 9.6 au profit
     *  de "levoile_vpn_status" (coherent architecture.md l. 1066). */
    const val CHANNEL_ID_STUB: String = "levoile_vpn_status_stub"

    // Action strings — prefixees par applicationId pour eviter collisions globales Android.
    const val ACTION_CONNECT: String = "fr.plateformeliberte.levoile.action.CONNECT"
    const val ACTION_DISCONNECT: String = "fr.plateformeliberte.levoile.action.DISCONNECT"
    const val EXTRA_COUNTRY: String = "fr.plateformeliberte.levoile.extra.COUNTRY"
}
