package fr.plateformeliberte.levoile.update

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import fr.plateformeliberte.levoile.BuildConfig
import fr.plateformeliberte.levoile.log.LeVoileLog
import fr.plateformeliberte.levoile.vpn.LeVoileVpnService
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.nio.charset.StandardCharsets
import kotlin.random.Random

/**
 * Story 12.5 — Worker périodique 24h qui vérifie GitHub releases pour le canal
 * APK direct. Court-circuit immédiat si `BuildConfig.AUTO_UPDATE_ENABLED` est
 * `false` (flavor `fdroid` — F-Droid gère les mises à jour côté store).
 *
 * Sinon, GET sur `api.github.com/repos/velia-the-veil/le_voile/releases/latest`,
 * parse `tag_name`, compare semver, poste une notification UI dismissable si
 * remote > local.
 *
 * Aucun log d'URL ni de body — cohérent NFR-AND-9 (le filtrage côté
 * [LeVoileLog] est respecté).
 *
 * Retry policy : géré par WorkManager via `Result.retry()` — backoff
 * exponentiel baseline 15min, max ~24h.
 */
internal class UpdateCheckWorker(
    appContext: Context,
    params: WorkerParameters,
) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        // Audit fix Android-§6 (2026-05-04) — partial mitigation of the
        // "passive observer can enumerate Le Voile users via 24h GitHub
        // hits" leak (the full fix moves the check inside the tunnel via
        // a relay /update-meta endpoint).
        //
        // 1. Skip when the VPN is not currently connected. WorkManager
        //    will retry under its standard schedule; in the meantime no
        //    stable 24h heartbeat is emitted from the user's real IP.
        // 2. Add a uniform jitter [0, 30 min] before the request so the
        //    fetch time decorrelates from the install time, denying a
        //    passive correlator the "every device starts checking 24h
        //    after first launch" cohort signal.
        if (LeVoileVpnService.instance == null) {
            LeVoileLog.i(TAG, "VPN inactive — update check skipped (will retry next cycle)")
            return@withContext Result.success()
        }
        val jitterMs = Random.nextLong(0, MAX_JITTER_MS)
        if (jitterMs > 0) {
            delay(jitterMs)
        }

        val checker = UpdateChecker(
            autoUpdateEnabled = BuildConfig.AUTO_UPDATE_ENABLED,
            localVersion = SemVer.parse(BuildConfig.VERSION_NAME),
            remoteVersionFetcher = ::fetchLatestVersionFromGitHub,
            notifier = { v -> UpdateNotificationHelper(applicationContext).post(v) },
            logger = { level, msg ->
                when (level) {
                    UpdateChecker.LogLevel.INFO -> LeVoileLog.i(TAG, msg)
                    UpdateChecker.LogLevel.WARN -> LeVoileLog.w(TAG, msg)
                }
            },
        )
        when (checker.check()) {
            UpdateChecker.Outcome.FETCH_FAILED -> Result.retry()
            else -> Result.success()
        }
    }

    private fun fetchLatestVersionFromGitHub(): SemVer {
        val url = URL(GITHUB_API_LATEST_RELEASE)
        val conn = url.openConnection() as HttpURLConnection
        conn.requestMethod = "GET"
        conn.connectTimeout = 10_000
        conn.readTimeout = 10_000
        // Audit fix R-T3 / Android-§6 (2026-05-04): the previous User-Agent
        // ("LeVoile/x.y.z (Android; SDK; APK direct)") was a unique
        // fingerprint that any passive observer could use to enumerate Le
        // Voile users in the wild. The check itself still leaves the
        // tunnel (the worker runs under disallowedApplications), so we
        // mimic a generic browser UA — not perfect anonymity, but it
        // raises the bar from "trivial blocklist key" to "needs SNI/IP
        // intelligence". Replacing the channel with a relay-side
        // /update-meta endpoint is tracked separately.
        conn.setRequestProperty(
            "User-Agent",
            "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
        )
        conn.setRequestProperty("Accept", "application/vnd.github+json")
        try {
            if (conn.responseCode != HTTP_OK) {
                throw IllegalStateException("HTTP ${conn.responseCode}")
            }
            val body = conn.inputStream.bufferedReader(StandardCharsets.UTF_8).readText()
            val tag = JSONObject(body).optString("tag_name", "")
            return SemVer.parse(tag)
                ?: throw IllegalStateException("tag_name invalide")
        } finally {
            conn.disconnect()
        }
    }

    companion object {
        private const val TAG = "UpdateCheckWorker"
        private const val HTTP_OK = 200
        private const val GITHUB_API_LATEST_RELEASE =
            "https://api.github.com/repos/velia-the-veil/le_voile/releases/latest"
        const val WORK_NAME = "levoile-update-check-24h"
        private const val MAX_JITTER_MS = 30L * 60L * 1000L
    }
}
