package fr.plateformeliberte.levoile.ui

import android.annotation.SuppressLint
import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.Looper
import androidx.core.app.NotificationChannelCompat
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import fr.plateformeliberte.levoile.MainActivity
import fr.plateformeliberte.levoile.R
import fr.plateformeliberte.levoile.kill.KillSwitchStatus
import fr.plateformeliberte.levoile.log.LeVoileLog
import fr.plateformeliberte.levoile.vpn.LeVoileVpnService
import fr.plateformeliberte.levoile.vpn.VpnConstants

/**
 * Story 9.6 + 11.7 — Orchestrateur notification persistante du Foreground Service VPN.
 *
 * Story 11.7 enrichit :
 *   - build(state, country, ip, killStatus) avec setContentText dynamique
 *     (« 🇩🇪 Allemagne · 5.x.x.x » ou « ⚠️ Kill switch inactif · Activer »).
 *   - Animation icône RECONNECTING (alternance opacity 1.0 ↔ 0.6 toutes les 750ms).
 *   - setContentDescription complet TalkBack (chiffres IP épelés un par un).
 *   - buildContentIntent accepte launchKillSwitchFlow flag (tap notif alerte
 *     → MainActivity avec EXTRA_OPEN_KILL_SWITCH_FLOW = true).
 *
 * Le Channel reste IMPORTANCE_LOW silencieux (Story 9.6) — toute mise à jour
 * est silencieuse (cohérent ux-design-specification.md l. 1325).
 */
class NotificationHelper(private val context: Context) {

    private var animationHandler: Handler? = null
    private var animationRunnable: Runnable? = null
    private var animationStep = 0  // 0 = opacité 1.0, 1 = 0.6
    // Code-review post-11.7 (M9) : garde-fou batterie. RECONNECTING normal dure
    // 5-30s ; au-delà de RECONNECT_MAX_TICKS, on stop l'animation (texte stable
    // sans pulsation). Évite drain ~1%/30 min si état RECONNECTING coincé. La
    // transition vers ERROR sera poussée par Story 9.7+ (StatusCallback timeout).
    private var animationTickCount = 0

    fun ensureChannel() {
        val nm = NotificationManagerCompat.from(context)
        nm.deleteNotificationChannel(VpnConstants.CHANNEL_ID_STUB)

        val channel = NotificationChannelCompat.Builder(
            CHANNEL_ID,
            NotificationManagerCompat.IMPORTANCE_LOW
        )
            .setName(context.getString(R.string.notif_channel_status_name))
            .setDescription(context.getString(R.string.notif_channel_status_desc))
            .setShowBadge(false)
            .setVibrationEnabled(false)
            .setLightsEnabled(false)
            .build()
        nm.createNotificationChannel(channel)
    }

    /**
     * Story 11.7 — Construit la notification enrichie.
     *
     * @param state état VpnState
     * @param country code ISO du pays actif (depuis StatusCallback Story 9.7) — null si pas connu
     * @param ip IP visible côté relais — null si pas connu
     * @param killStatus état kill switch (Story 10.1) — null si non vérifié
     */
    fun build(
        state: VpnState,
        country: String?,
        ip: String?,
        killStatus: KillSwitchStatus?,
    ): Notification {
        val title = context.getString(R.string.notif_title_prefix) +
            " " + SEPARATOR + " " + statusLabel(state)

        // Si kill switch inactif ET tunnel CONNECTED → alerte (prime sur pays/IP).
        val isKillSwitchAlert =
            state == VpnState.CONNECTED && killStatus is KillSwitchStatus.Inactive
        val contentText = when {
            isKillSwitchAlert -> context.getString(R.string.notif_text_killswitch_inactive_alert)
            state == VpnState.CONNECTED -> {
                val countryStr = CountryDisplay.formatShort(country)
                if (ip.isNullOrBlank()) countryStr
                else "$countryStr $SEPARATOR $ip"
            }
            state == VpnState.RECONNECTING ->
                context.getString(R.string.notif_text_reconnecting)
            state == VpnState.DISCONNECTED ->
                context.getString(R.string.vpn_status_disconnected)
            state == VpnState.ERROR ->
                context.getString(R.string.vpn_status_error)
            else -> ""
        }

        val talkBack = buildTalkBackDescription(state, country, ip, killStatus)

        return NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(buildSmallIcon(state))
            .setContentTitle(title)
            .setContentText(contentText)
            .setOngoing(true)
            .setSilent(true)
            .setShowWhen(false)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
            .setColor(ContextCompat.getColor(context, R.color.primary_blue))
            .setColorized(false)
            .setContentIntent(buildContentIntent(launchKillSwitchFlow = isKillSwitchAlert))
            .addAction(buildDisconnectAction())
            .setSubText(if (isKillSwitchAlert) "⚠" else null)
            .setTicker(talkBack)
            .build()
    }

    /**
     * Story 11.7 — Convenience préservant l'API Story 9.6 pour les sites
     * d'appel non encore migrés.
     */
    @Deprecated(
        "Story 11.7 — utiliser build(state, country, ip, killStatus). Cette signature retourne une notif sans contexte enrichi.",
        ReplaceWith("build(state, null, null, null)")
    )
    fun build(state: VpnState): Notification = build(state, null, null, null)

    /**
     * Story 11.7 — notify enrichi avec country/ip/killStatus + animation reconnect.
     */
    @SuppressLint("MissingPermission")
    fun notify(
        state: VpnState,
        country: String?,
        ip: String?,
        killStatus: KillSwitchStatus?,
    ) {
        try {
            NotificationManagerCompat.from(context)
                .notify(VpnConstants.NOTIF_ID, build(state, country, ip, killStatus))
        } catch (t: Throwable) {
            LeVoileLog.i(TAG, "notify($state) ignore: ${t.javaClass.simpleName}")
        }

        if (state == VpnState.RECONNECTING) {
            startReconnectAnimation(country, ip, killStatus)
        } else {
            stopReconnectAnimation()
        }
    }

    /**
     * Story 9.6 — convenience legacy (pas de country/ip/killStatus). Conservée
     * pour compat sites d'appel pré-Story 11.7.
     */
    @SuppressLint("MissingPermission")
    @Deprecated(
        "Story 11.7 — utiliser notify(state, country, ip, killStatus).",
        ReplaceWith("notify(state, null, null, null)")
    )
    fun notify(state: VpnState) = notify(state, null, null, null)

    @SuppressLint("MissingPermission")
    private fun startReconnectAnimation(country: String?, ip: String?, killStatus: KillSwitchStatus?) {
        stopReconnectAnimation()
        val handler = Handler(Looper.getMainLooper())
        animationHandler = handler
        animationTickCount = 0
        animationRunnable = object : Runnable {
            override fun run() {
                // Code-review post-11.7 (M9) : sortie garde-fou si trop long.
                // RECONNECT_MAX_TICKS * 750ms = 5 min — au-delà on fige l'icône
                // sur l'état "dim" pour signaler un blocage (Story 9.7+ poussera
                // VpnState.ERROR à ce moment-là).
                if (animationTickCount >= RECONNECT_MAX_TICKS) {
                    LeVoileLog.w(TAG, "RECONNECTING animation timeout — stop pulsation")
                    stopReconnectAnimation()
                    return
                }
                animationTickCount++
                animationStep = (animationStep + 1) % 2
                try {
                    NotificationManagerCompat.from(context).notify(
                        VpnConstants.NOTIF_ID,
                        build(VpnState.RECONNECTING, country, ip, killStatus)
                    )
                } catch (_: Throwable) {}
                handler.postDelayed(this, 750L)
            }
        }
        handler.postDelayed(animationRunnable!!, 750L)
    }

    private fun stopReconnectAnimation() {
        animationRunnable?.let { animationHandler?.removeCallbacks(it) }
        animationRunnable = null
        animationHandler = null
        animationStep = 0
        animationTickCount = 0
    }

    private fun buildSmallIcon(state: VpnState): Int = when {
        state == VpnState.RECONNECTING && animationStep == 1 -> R.drawable.ic_levoile_status_dim
        else -> R.drawable.ic_levoile_status
    }

    /**
     * Story 11.7 — TalkBack contentDescription (visibilité internal pour test).
     */
    internal fun buildTalkBackDescription(
        state: VpnState,
        country: String?,
        ip: String?,
        killStatus: KillSwitchStatus?,
    ): String {
        val stateLabel = statusLabel(state)
        val countryLabel = CountryDisplay.formatTalkBack(country)
        val ipLabel = ip?.let { spellOutDigits(it) } ?: "inconnue"
        return when {
            killStatus is KillSwitchStatus.Inactive && state == VpnState.CONNECTED ->
                "Le Voile, alerte kill switch inactif, taper pour activer"
            state == VpnState.CONNECTED ->
                "Le Voile, état $stateLabel, pays $countryLabel, IP $ipLabel"
            else -> "Le Voile, état $stateLabel"
        }
    }

    /**
     * « 5.45.6.7 » → « 5 4 5 point 6 point 7 » pour TalkBack.
     */
    internal fun spellOutDigits(ip: String): String =
        ip.map { c ->
            when (c) {
                '.' -> " point "
                in '0'..'9' -> "$c "
                else -> ""
            }
        }.joinToString("").trim()

    private fun statusLabel(state: VpnState): String = when (state) {
        VpnState.CONNECTED -> context.getString(R.string.vpn_status_connected)
        VpnState.RECONNECTING -> context.getString(R.string.vpn_status_reconnecting)
        VpnState.DISCONNECTED -> context.getString(R.string.vpn_status_disconnected)
        VpnState.ERROR -> context.getString(R.string.vpn_status_error)
    }

    private fun buildContentIntent(launchKillSwitchFlow: Boolean = false): PendingIntent {
        val intent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP
            if (launchKillSwitchFlow) {
                putExtra(MainActivity.EXTRA_OPEN_KILL_SWITCH_FLOW, true)
            }
        }
        return PendingIntent.getActivity(
            context,
            if (launchKillSwitchFlow) REQUEST_CODE_OPEN_APP_C15 else REQUEST_CODE_OPEN_APP,
            intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
    }

    private fun buildDisconnectAction(): NotificationCompat.Action {
        val intent = Intent(context, LeVoileVpnService::class.java).apply {
            action = VpnConstants.ACTION_DISCONNECT
        }
        val pi = PendingIntent.getService(
            context,
            REQUEST_CODE_DISCONNECT,
            intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        return NotificationCompat.Action.Builder(
            0,
            context.getString(R.string.notif_action_disconnect),
            pi
        )
            .setContextual(false)
            .build()
    }

    private companion object {
        const val TAG = "NotificationHelper"
        const val CHANNEL_ID = "levoile_vpn_status"
        const val SEPARATOR = "·"
        const val REQUEST_CODE_OPEN_APP = 0xCEC3
        const val REQUEST_CODE_DISCONNECT = 0xCEC2
        // Story 11.7 — request code distinct pour le tap notif alerte.
        const val REQUEST_CODE_OPEN_APP_C15 = 0xCEC5
        // Code-review post-11.7 (M9) : 400 ticks * 750ms = 300_000ms = 5 min.
        const val RECONNECT_MAX_TICKS = 400
    }
}
