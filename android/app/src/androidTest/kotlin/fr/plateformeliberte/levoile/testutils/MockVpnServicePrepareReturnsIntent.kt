package fr.plateformeliberte.levoile.testutils

import android.content.Intent
import android.provider.Settings

/**
 * Story 12.6 — utilitaire pour simuler qu'un autre VPN est actif (epics.md l. 2219).
 *
 * `VpnService.prepare(context)` retourne un Intent non-null quand un autre
 * VPN détient le slot exclusif Android — c'est le signal `VpnConflictDetector`
 * (Story 10.3) consomme.
 *
 * **Limite** : on ne peut pas vraiment installer un autre VPN sur l'émulateur
 * matrix CI (pas de Google Play). Le mock le plus pragmatique est de
 * remplacer la lecture via `Settings.Global` ou via `VpnPreparer` (Story 10.3
 * factorise `VpnService.prepare` derrière une abstraction). Ce stub fournit
 * un Intent crédible que `VpnConflictDetector.check()` interprétera comme
 * "autre VPN actif".
 */
object MockVpnServicePrepareReturnsIntent {

    /**
     * Retourne un Intent non-null typique de `VpnService.prepare()` quand
     * un autre VPN détient le slot. L'Intent réel pointe vers
     * `com.android.vpndialogs.ConfirmDialog`.
     */
    fun fake(): Intent =
        Intent().apply {
            // Approximation Story 12.6 : Intent de dialog VPN system.
            // En vrai test instrumenté, on injecte ce fake via une
            // `VpnPreparer` mockable (DI simple — Story 10.3 factorise).
            setClassName("com.android.vpndialogs", "com.android.vpndialogs.ConfirmDialog")
        }

    /**
     * Helper pour ouvrir Settings VPN sur l'émulateur (utilisé par certains
     * tests qui valident la deeplink `c15_btn_open_settings` Story 11.6).
     */
    fun vpnSettingsIntent(): Intent =
        Intent(Settings.ACTION_VPN_SETTINGS).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
}
