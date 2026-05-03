package fr.plateformeliberte.levoile.ui

import android.annotation.SuppressLint
import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.util.Log
import androidx.core.app.NotificationChannelCompat
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import fr.plateformeliberte.levoile.MainActivity
import fr.plateformeliberte.levoile.R
import fr.plateformeliberte.levoile.vpn.LeVoileVpnService
import fr.plateformeliberte.levoile.vpn.VpnConstants

/**
 * Orchestrateur unique de la notification persistante du Foreground Service VPN.
 *
 * Story 9.6 livre :
 *   - Channel final levoile_vpn_status (IMPORTANCE_LOW) + suppression du
 *     channel stub livre Story 9.4 (levoile_vpn_status_stub).
 *   - Builder NotificationCompat centralise (un seul site de construction)
 *     avec setOngoing + setSilent + setSmallIcon(ic_levoile_status).
 *   - Titre dynamique « Le Voile · {Etat} » via statusLabel(state).
 *   - Sous-texte vide MVP — Story 11.7 enrichira pays + IP via
 *     setContentText sans changer ni le channel ni l'icone (EBR-01).
 *   - Tap notif -> MainActivity (PendingIntent.getActivity, FLAG_IMMUTABLE).
 *   - Action « Deconnecter » -> LeVoileVpnService.ACTION_DISCONNECT
 *     (PendingIntent.getService, FLAG_IMMUTABLE) — request codes distincts
 *     pour eviter qu'Android ecrase un PendingIntent par l'autre.
 *
 * NotificationHelper ne touche PAS au lifecycle Foreground Service
 * (startForeground / stopForeground) — c'est LeVoileVpnService qui orchestre.
 * Pas d'etat interne (pas de timer, pas de connexion). Idempotent.
 */
class NotificationHelper(private val context: Context) {

    /**
     * Cree le channel final (idempotent — Android ignore les recreations) et
     * supprime le channel stub legacy livre Story 9.4. La suppression est
     * silencieuse pour les nouveaux installs ou le stub n'a jamais existe.
     */
    fun ensureChannel() {
        val nm = NotificationManagerCompat.from(context)
        // Suppression du channel stub legacy 9.4 — sinon Settings > Apps > Le Voile >
        // Notifications afficherait deux entrees post-mise-a-jour 9.4 -> 9.6.
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
     * Construit la Notification finale pour l'etat donne. Pure function : meme
     * input -> meme output. Aucun side-effect (pas de notify ici — voir
     * notify(state)).
     */
    fun build(state: VpnState): Notification {
        val title = context.getString(R.string.notif_title_prefix) +
            " " + SEPARATOR + " " +
            statusLabel(state)
        return NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_levoile_status)
            .setContentTitle(title)
            // Pas de setContentText — sous-texte vide MVP (EBR-01, Story 11.7 enrichira).
            .setOngoing(true)
            .setSilent(true)
            .setShowWhen(false)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
            .setColor(ContextCompat.getColor(context, R.color.primary_blue))
            .setColorized(false)
            .setContentIntent(buildContentIntent())
            .addAction(buildDisconnectAction())
            .build()
    }

    /**
     * Met a jour la notif courante (meme NOTIF_ID — Android remplace seamlessly).
     *
     * Ne PAS appeler avant ensureChannel() : Android refuse de poster sur un
     * channel inconnu et le notify est silencieux. Le contrat lifecycle est
     * gere par LeVoileVpnService (ensureChannel dans onCreate, build/notify
     * dans connectInternal/disconnectInternal).
     *
     * **Graceful degradation Android 13+** : si l'utilisateur a refuse
     * `POST_NOTIFICATIONS` runtime (architecture.md l. 1193), `notify` peut
     * lever `SecurityException`. On la catch — comportement documente
     * conforme : "Si refusée → notif foreground masquée mais service continue
     * (Android l'autorise pour les FGS en cours, juste pas de notification visible)".
     * Le tunnel reste actif, l'utilisateur ne voit pas la notif mais l'app
     * n'est pas tuee.
     *
     * **Robustesse OEM** : on catch `Throwable` au sens large (pas uniquement
     * `SecurityException`) car certains OEM (Xiaomi MIUI, Huawei EMUI, Samsung
     * One UI) patchent `NotificationManager` et peuvent lever des
     * `RuntimeException` non documentees — pattern aligne sur Story 9.4 fix
     * L-8 (packetRelay errors). Le service Foreground ne doit JAMAIS etre tue
     * par une erreur de notif (qui est cosmetique : sans elle, le tunnel
     * fonctionne quand meme).
     *
     * NB : `NotificationManagerCompat` lui-meme retourne false sur les channels
     * desactives par l'utilisateur, sans throw — le try/catch couvre uniquement
     * `POST_NOTIFICATIONS` refuse + API >= 33 + bizarreries OEM.
     */
    @SuppressLint("MissingPermission")
    fun notify(state: VpnState) {
        try {
            NotificationManagerCompat.from(context)
                .notify(VpnConstants.NOTIF_ID, build(state))
        } catch (t: Throwable) {
            // POST_NOTIFICATIONS refuse runtime ou OEM customise NotificationManager —
            // comportement attendu, pas un bug. Log INFO + le message pour aider au
            // debug si un OEM specifique remonte un cas exotique en CI Story 12.6.
            Log.i(TAG, "notify($state) ignore : ${t.javaClass.simpleName}: ${t.message}")
        }
    }

    private fun statusLabel(state: VpnState): String = when (state) {
        VpnState.CONNECTED -> context.getString(R.string.vpn_status_connected)
        VpnState.RECONNECTING -> context.getString(R.string.vpn_status_reconnecting)
        VpnState.DISCONNECTED -> context.getString(R.string.vpn_status_disconnected)
        VpnState.ERROR -> context.getString(R.string.vpn_status_error)
    }

    private fun buildContentIntent(): PendingIntent {
        // Si MainActivity est deja au top du back stack, FLAG_ACTIVITY_SINGLE_TOP
        // la ramene au premier plan via onNewIntent sans la recreer (preserve
        // l'etat WebView, coherent android:configChanges Story 9.3 et le
        // launchMode="singleTop" du manifest 9.3 — defense en profondeur, le
        // launchMode suffirait seul mais le flag explicite ne nuit pas).
        //
        // Pas de FLAG_ACTIVITY_CLEAR_TOP : MainActivity est l'unique Activity du
        // back stack en Phase 1 (Story 9.3+). Story 11.x pourrait introduire des
        // sub-activities (settings, onboarding) — destroyer arbitrairement leur
        // pile via CLEAR_TOP serait une regression UX (perte de scroll, etat
        // formulaire). Coherent fix L-1 code-review post-9.6.
        val intent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        return PendingIntent.getActivity(
            context,
            REQUEST_CODE_OPEN_APP,
            intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
    }

    private fun buildDisconnectAction(): NotificationCompat.Action {
        // PendingIntent.getService -> consomme directement par
        // LeVoileVpnService.onStartCommand (action ACTION_DISCONNECT) — pas
        // de getBroadcast, pas de getActivity (cf. spec Story 9.6 AC #7).
        val intent = Intent(context, LeVoileVpnService::class.java).apply {
            action = VpnConstants.ACTION_DISCONNECT
        }
        val pi = PendingIntent.getService(
            context,
            REQUEST_CODE_DISCONNECT,
            intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        // Icone d'action = 0 (pas d'icone) — coherent fix L-2 code-review post-9.6.
        // Pixel/AOSP n'affiche jamais l'icone d'action en compact view (deprecie
        // depuis API 7) ; certains OEM (Samsung One UI, Xiaomi MIUI) l'affichent
        // mais reutiliser ic_levoile_status (cadenas) pour « Deconnecter » est
        // semantiquement faible (cadenas = protection, pas deconnexion).
        // NotificationCompat.Action.Builder accepte 0 comme "pas d'icone" depuis
        // androidx.core 1.5+ ; le label « Deconnecter » est suffisamment explicite.
        // Si Story 11.7 souhaite un glyph dedie, livrer ic_disconnect.xml
        // (vector "X" mono-couleur).
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

        // Channel ID final post-9.6 — coherent architecture.md l. 1066-1071.
        const val CHANNEL_ID = "levoile_vpn_status"

        // Caractere middle dot U+00B7 — coherent charte plateformeliberte.fr et
        // pattern « Le Voile · {Etat} » des Stories 9.3+.
        const val SEPARATOR = "·"

        // Request codes distincts pour eviter qu'Android ecrase silencieusement
        // un PendingIntent par l'autre (meme target -> same equality semantics).
        // Distincts aussi du NOTIF_ID = 0xCEC1 (qui est une notification ID,
        // pas un request code, mais on garde la coherence visuelle).
        const val REQUEST_CODE_OPEN_APP = 0xCEC3
        const val REQUEST_CODE_DISCONNECT = 0xCEC2
    }
}
