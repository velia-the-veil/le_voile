package fr.plateformeliberte.levoile.ui

/**
 * Etats du tunnel VPN affiches dans la notification persistante (Story 9.6 MVP).
 *
 * Pas de payload (pas de pays, pas d'IP, pas de message d'erreur dynamique) :
 *   - Story 9.7 cablera les transitions reelles depuis le noyau Go via .aar.
 *   - Story 11.7 enrichira la notification avec une data class separee
 *     VpnConnectionInfo(country, ip, ...) — VpnState restera l'enum statut.
 *
 * Localisation : android/ui/ (et NON un noyau Go partage) — purement Android,
 * coherent ADR-08 (isolation OS maximale).
 */
enum class VpnState {
    CONNECTED,
    RECONNECTING,
    DISCONNECTED,
    ERROR;
}
