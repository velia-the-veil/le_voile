package fr.plateformeliberte.levoile.update

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.app.NotificationCompat
import fr.plateformeliberte.levoile.R

/**
 * Story 12.5 — Notification UI mise à jour (canal `levoile_update`,
 * `IMPORTANCE_DEFAULT` = son + heads-up, dismissable, action « Voir sur
 * GitHub » qui ouvre le browser).
 *
 * Distinct de la notification persistante C16 (Story 11.7) — channel et ID
 * dédiés. `setOngoing(false)` + `setAutoCancel(true)` — l'utilisatrice peut
 * dismisser la notif si elle veut reporter ; le worker re-postera 24h plus
 * tard si la version reste différente.
 */
internal class UpdateNotificationHelper(private val context: Context) {

    init {
        val nm = context.getSystemService(NotificationManager::class.java)
        if (nm != null && nm.getNotificationChannel(CHANNEL_ID) == null) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                context.getString(R.string.update_channel_name),
                NotificationManager.IMPORTANCE_DEFAULT,
            ).apply {
                description = context.getString(R.string.update_channel_description)
                setShowBadge(true)
            }
            nm.createNotificationChannel(channel)
        }
    }

    fun post(version: SemVer) {
        val versionString = "${version.major}.${version.minor}.${version.patch}"
        // Tag GitHub canonical = "v<major>.<minor>.<patch>[-<pre>][+<build>]" — le
        // build metadata est conservé dans le tag même s'il est ignoré pour la
        // comparaison SemVer §10. Sans cette inclusion, un tag réel
        // `v0.2.0+build123` ouvrirait `releases/tag/v0.2.0` → 404.
        // Fix Code Review 2026-05-03.
        val tag = buildString {
            append("v").append(versionString)
            version.preRelease?.let { append("-").append(it) }
            version.buildMetadata?.let { append("+").append(it) }
        }
        val url = "https://github.com/velia-the-veil/le_voile/releases/tag/$tag"

        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url)).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK
        }
        val pendingIntent = PendingIntent.getActivity(
            context,
            REQUEST_CODE,
            intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        val notification = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification_update)
            .setContentTitle(context.getString(R.string.update_notification_title, versionString))
            .setContentText(context.getString(R.string.update_notification_text))
            .setStyle(
                NotificationCompat.BigTextStyle().bigText(
                    context.getString(R.string.update_notification_text_long, versionString),
                ),
            )
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
            .setAutoCancel(true)
            .setOngoing(false)
            .addAction(
                R.drawable.ic_action_github,
                context.getString(R.string.update_notification_action),
                pendingIntent,
            )
            .setContentIntent(pendingIntent)
            .build()

        context.getSystemService(NotificationManager::class.java)
            ?.notify(NOTIFICATION_ID, notification)
    }

    companion object {
        const val CHANNEL_ID = "levoile_update"
        // ID distinct de la notif persistante C16 (Story 11.7 utilise NotificationHelper).
        const val NOTIFICATION_ID = 2026
        private const val REQUEST_CODE = 12_50001
    }
}
