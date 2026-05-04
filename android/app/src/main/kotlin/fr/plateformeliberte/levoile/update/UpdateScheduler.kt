package fr.plateformeliberte.levoile.update

import android.content.Context
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit

/**
 * Story 12.5 — Schedule l'`UpdateCheckWorker` toutes les 24h via WorkManager.
 *
 * Invoqué depuis [fr.plateformeliberte.levoile.MainActivity.onCreate] avec
 * `applicationContext`. `enqueueUniquePeriodicWork(... KEEP)` garantit qu'on
 * ne duplique pas le worker si déjà schedulé (idempotent — chaque ouverture
 * de l'app re-confirme le scheduling sans redémarrer le compteur).
 *
 * Même si `BuildConfig.AUTO_UPDATE_ENABLED == false` (flavor fdroid), on
 * schedule quand même — le worker court-circuit à l'exécution. Avantage : un
 * éventuel toggle build/runtime futur n'a pas besoin de boot redémarrage pour
 * réactiver le scheduling.
 */
internal object UpdateScheduler {

    fun scheduleIfNeeded(context: Context) {
        val constraints = Constraints.Builder()
            .setRequiredNetworkType(NetworkType.CONNECTED)
            .build()

        val request = PeriodicWorkRequestBuilder<UpdateCheckWorker>(
            24, TimeUnit.HOURS,    // interval
            6, TimeUnit.HOURS,     // flex window — WM peut anticiper de 6h
        )
            .setConstraints(constraints)
            .build()

        WorkManager.getInstance(context).enqueueUniquePeriodicWork(
            UpdateCheckWorker.WORK_NAME,
            ExistingPeriodicWorkPolicy.KEEP,
            request,
        )
    }
}
