package fr.plateformeliberte.levoile.update

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import fr.plateformeliberte.levoile.BuildConfig
import fr.plateformeliberte.levoile.log.LeVoileLog
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.nio.charset.StandardCharsets

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
        conn.setRequestProperty(
            "User-Agent",
            "LeVoile/${BuildConfig.VERSION_NAME} (Android; ${android.os.Build.VERSION.SDK_INT}; APK direct)",
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
    }
}
