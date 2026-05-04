package fr.plateformeliberte.levoile.update

/**
 * Story 12.5 — logique métier du check version, isolée du runtime WorkManager
 * pour permettre les tests JVM-only sans BuildConfig ni HttpURLConnection.
 *
 * Le worker [UpdateCheckWorker] instancie un [UpdateChecker] avec les vraies
 * dépendances (BuildConfig.AUTO_UPDATE_ENABLED, HttpURLConnection vers
 * api.github.com, UpdateNotificationHelper). Les tests JVM passent des mocks
 * triviaux (lambdas).
 */
internal class UpdateChecker(
    private val autoUpdateEnabled: Boolean,
    private val localVersion: SemVer?,
    private val remoteVersionFetcher: () -> SemVer,
    private val notifier: (SemVer) -> Unit,
    private val logger: (level: LogLevel, message: String) -> Unit = { _, _ -> },
) {

    enum class LogLevel { INFO, WARN }

    enum class Outcome {
        /** Auto-update désactivé via flag build (flavor fdroid). */
        DISABLED,
        /** VERSION_NAME local invalide — ne sera pas re-tenté (pas un retry). */
        LOCAL_VERSION_INVALID,
        /** Fetch GitHub a échoué (réseau, 5xx, JSON mal formé) — caller peut Result.retry(). */
        FETCH_FAILED,
        /** Remote ≤ local — déjà à jour. */
        UP_TO_DATE,
        /** Remote > local — notification postée. */
        UPDATE_AVAILABLE,
    }

    fun check(): Outcome {
        if (!autoUpdateEnabled) {
            logger(LogLevel.INFO, "Auto-update désactivé (flavor fdroid — F-Droid gère les mises à jour)")
            return Outcome.DISABLED
        }
        val local = localVersion
        if (local == null) {
            logger(LogLevel.WARN, "VERSION_NAME local invalide — ignoré (pas de retry)")
            return Outcome.LOCAL_VERSION_INVALID
        }
        val remote = try {
            remoteVersionFetcher()
        } catch (t: Throwable) {
            logger(LogLevel.WARN, "Echec fetch latest release: ${t.javaClass.simpleName}")
            return Outcome.FETCH_FAILED
        }
        if (remote > local) {
            logger(
                LogLevel.INFO,
                "Nouvelle version disponible: ${remote.major}.${remote.minor}.${remote.patch}",
            )
            notifier(remote)
            return Outcome.UPDATE_AVAILABLE
        }
        return Outcome.UP_TO_DATE
    }
}
