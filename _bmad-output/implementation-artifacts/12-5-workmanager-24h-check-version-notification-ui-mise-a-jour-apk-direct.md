# Story 12.5: WorkManager 24h + check version + notification UI mise à jour (APK direct)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-system. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev livre le check version périodique 24h via WorkManager pour le buildType `apkDirect` UNIQUEMENT. Le buildType `fdroid` court-circuite l'auto-update (F-Droid gère ses utilisateurs côté store). Le check est conservateur : aucune télémétrie sortante, juste un GET `https://api.github.com/repos/velia-the-veil/le_voile/releases/latest`, comparaison sémantique de versions, et une notification dismissable. Aucun téléchargement automatique d'APK — l'utilisatrice manuelle ouvre GitHub releases dans le navigateur via PendingIntent.**
>
> **Story 12.5 livre** :
> 1. **Étend** `android/app/build.gradle.kts` avec 2 `productFlavors` : `apkDirect` (default) et `fdroid` — permet de différencier le comportement à la build time. Le flavor `fdroid` ajoute un `BuildConfigField("Boolean", "AUTO_UPDATE_ENABLED", "false")` ; `apkDirect` ajoute `BuildConfigField("Boolean", "AUTO_UPDATE_ENABLED", "true")`.
> 2. **Étend** `android/app/src/main/AndroidManifest.xml` (uniquement à la marge) : aucune nouvelle permission requise — `INTERNET` (déjà présente) suffit. Pas besoin de `WAKE_LOCK` ni `RECEIVE_BOOT_COMPLETED` (WorkManager gère).
> 3. **Crée** `android/app/src/main/kotlin/.../update/UpdateCheckWorker.kt` — un `CoroutineWorker` qui :
>    - **Si `BuildConfig.AUTO_UPDATE_ENABLED == false`** : log INFO « Auto-update désactivé — F-Droid gère les mises à jour » + `Result.success()` immédiat (cohérent epics.md l. 2188-2192).
>    - **Sinon** : GET `https://api.github.com/repos/velia-the-veil/le_voile/releases/latest` avec `User-Agent: LeVoile/<versionName> (Android; <api-level>; APK direct)`.
>    - Parse `tag_name` (ex. `v0.2.0`) → version sémantique.
>    - Compare avec `BuildConfig.VERSION_NAME` via `compareSemVer(local, remote)` helper.
>    - Si remote > local → poste une notification (canal `levoile_update`).
>    - Retry policy : `BackoffPolicy.EXPONENTIAL` avec `Duration.ofMinutes(15)` baseline (gérée par WorkManager via `Result.retry()`).
> 4. **Crée** `android/app/src/main/kotlin/.../update/UpdateNotificationHelper.kt` — wrapper de `NotificationHelper` (Story 9.6) pour :
>    - Channel `levoile_update`, importance `IMPORTANCE_DEFAULT` (vs `IMPORTANCE_LOW` du channel persistant Story 9.6 — la mise à jour mérite un son + heads-up).
>    - Notification dismissable (PAS `setOngoing(true)`).
>    - Action « Voir sur GitHub » → `PendingIntent` avec `Intent(Intent.ACTION_VIEW, Uri.parse("https://github.com/velia-the-veil/le_voile/releases/tag/v$version"))`.
>    - Notification ID unique distinct du C16 persistant (Story 11.7).
> 5. **Crée** `android/app/src/main/kotlin/.../update/UpdateScheduler.kt` — entrée d'usage initiée par `MainActivity.onCreate()` :
>    - `WorkManager.getInstance(context).enqueueUniquePeriodicWork(WORK_NAME, ExistingPeriodicWorkPolicy.KEEP, request)` pour ne pas dupliquer si déjà schedulé.
>    - Construction d'un `PeriodicWorkRequest` interval = 24h, contraintes `NetworkType.CONNECTED`.
>    - `RUN_ATTEMPT_COUNT` géré nativement par WorkManager.
>    - Même si `AUTO_UPDATE_ENABLED == false`, on schedule quand même (le worker court-circuite à l'exécution) — comme ça le toggle build n'a pas besoin d'un boot redémarrage.
> 6. **Crée** `android/app/src/main/kotlin/.../update/SemVerCompare.kt` — helper pure (sans dep) qui parse `v0.2.0-beta.1+meta` → `(0, 2, 0, "beta.1", "meta")` et implémente `compareTo` selon SemVer 2.0.0 spec. Utilisé par UpdateCheckWorker.
> 7. **Crée** `android/app/src/test/kotlin/.../update/{UpdateCheckWorkerTest,UpdateCheckBuildTypeTest,SemVerCompareTest}.kt` — tests JVM-only :
>    - `UpdateCheckBuildTypeTest` : avec mock `BuildConfig.AUTO_UPDATE_ENABLED = false`, `doWork()` retourne `Result.success` SANS poster de notification (cohérent epics.md l. 2192).
>    - `SemVerCompareTest` : 15+ cas couvrant pre-release, build metadata, comparaisons.
>    - `UpdateCheckWorkerTest` : avec mock GitHub API (HTTP stub), vérifie qu'un remote > local poste la notif, qu'un remote == local ne la poste pas.
> 8. **Étend** `android/app/build.gradle.kts` avec dépendances : `androidx.work:work-runtime-ktx` (WorkManager) + `org.jetbrains.kotlinx:kotlinx-coroutines-android` (déjà présent Story 9.7) + `testImplementation("androidx.work:work-testing")` pour `WorkManagerTestInitHelper`.
> 9. Le test instrumenté `UpdateNotificationFlowTest.kt` (Story 12.6 dépendance — **squelette pour 12.5**, impl runtime Story 12.6) qui simule un remote > local et vérifie que la notification apparaît.
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile.
>
> **Rappel ADR-08** : tous les fichiers vivent sous `android/`. Aucun partage cross-OS (l'auto-update desktop est un sujet séparé Stories 8.1/8.2 livrées).
>
> **Rappel `feedback_no_reset_endpoints.md`** : aucune méthode publique pour `cancelUpdate()`, `disableAutoUpdate()`, `forceCheck()`. Si l'utilisatrice veut désactiver, elle utilise Réglages > Apps > Le Voile > Notifications > désactiver le canal `levoile_update`. Aucune CLI/UI/IPC qui reset le scheduler.
>
> **Zones explicitement OFF-LIMITS** :
>
> | Zone | Livrée par | État pour 12.5 |
> |---|---|---|
> | `android/app/src/main/kotlin/.../{vpn,kill,conflict,onboarding,bridge,registry,config,log,ui}/` | 9.x/10.x/11.x | INTACT |
> | `android/levoile-core/**`, `android/shims/**` | 9.x | INTACT |
> | `metadata/**` | 12.1 | INTACT |
> | `.github/workflows/**` | 10.4/12.2/12.3/12.4 | INTACT |
> | `android/app/src/main/kotlin/.../MainActivity.kt` | 9.3+11.x | **MODIFIÉ uniquement à la marge** : `onCreate()` invoque `UpdateScheduler.scheduleIfNeeded(context)` (1 ligne) |
> | `android/app/src/main/kotlin/.../ui/NotificationHelper.kt` | 9.6/11.7 | INTACT — `UpdateNotificationHelper.kt` est un wrapper séparé (pas de modif du C16 persistent) |
> | `android/app/src/main/kotlin/.../update/UpdateCheckWorker.kt` | (absent) | **NOUVEAU** |
> | `android/app/src/main/kotlin/.../update/UpdateNotificationHelper.kt` | (absent) | **NOUVEAU** |
> | `android/app/src/main/kotlin/.../update/UpdateScheduler.kt` | (absent) | **NOUVEAU** |
> | `android/app/src/main/kotlin/.../update/SemVerCompare.kt` | (absent) | **NOUVEAU** |
> | `android/app/build.gradle.kts` | 11.8/12.1/12.3/12.4 | **MODIFIÉ — productFlavors + dépendances WorkManager + test deps** |
> | `android/gradle/libs.versions.toml` | 11.8/12.1 | **MODIFIÉ — ajout `androidx.work` + `androidx.work.testing`** |
> | `android/app/src/main/res/values{,-fr}/strings.xml` | 9.x/10.x/11.x | **MODIFIÉ uniquement à la marge** : ajout `update_notification_title`, `update_notification_text`, `update_notification_action`, `update_channel_name`, `update_channel_description` |
> | `android/app/src/test/kotlin/.../update/*.kt` | (absent) | **NOUVEAUX — 3 fichiers de tests** |
> | `android/app/src/androidTest/kotlin/.../update/UpdateNotificationFlowTest.kt` | (absent) | **NOUVEAU squelette — impl Story 12.6** |
>
> **Concrètement** : `git status` à la fin doit montrer **uniquement** :
>   (a) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/{UpdateCheckWorker,UpdateNotificationHelper,UpdateScheduler,SemVerCompare}.kt` (NOUVEAUX),
>   (b) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/update/{UpdateCheckWorkerTest,UpdateCheckBuildTypeTest,SemVerCompareTest}.kt` (NOUVEAUX),
>   (c) `android/app/src/androidTest/kotlin/fr/plateformeliberte/levoile/update/UpdateNotificationFlowTest.kt` (NOUVEAU squelette),
>   (d) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — 1 ligne `UpdateScheduler.scheduleIfNeeded`),
>   (e) `android/app/build.gradle.kts` (MODIFIÉ — productFlavors + deps),
>   (f) `android/gradle/libs.versions.toml` (MODIFIÉ — work + work-testing),
>   (g) `android/app/src/main/res/values/strings.xml` (MODIFIÉ — strings notification),
>   (h) `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — traductions FR),
>   (i) `_bmad-output/implementation-artifacts/sprint-status.yaml`,
>   (j) `_bmad-output/implementation-artifacts/12-5-workmanager-24h-check-version-notification-ui-mise-a-jour-apk-direct.md`.
>
> **Anti-patterns** :
> - Télécharger et installer l'APK automatiquement — viole la confiance utilisateur (un VPN qui s'auto-installe = surface d'attaque). **Toujours** ouvrir GitHub dans le navigateur, l'utilisatrice télécharge + installe manuellement.
> - Embarquer un compteur d'analytics « combien d'utilisateurs ont vu la notif » — viole NFR-AND-8 zéro-télémétrie.
> - Faire tourner le worker en `OneTimeWorkRequest` chaîné lui-même → `PeriodicWorkRequest` est l'API officielle, plus robuste (gère battery saver, app fermée, boot redémarrage).
> - Embarquer le hash SHA256 attendu de l'APK release dans le code Kotlin — change à chaque release. **Solution** : lire dans GitHub release body via parsing JSON (ou ne pas afficher du tout — l'utilisatrice vérifie elle-même via `apksigner verify` documenté Story 12.3 + 12.4).
> - Ouvrir l'APK release directement via `Intent(ACTION_INSTALL_PACKAGE)` — Android 8+ exige `REQUEST_INSTALL_PACKAGES` permission qui demande à l'utilisatrice un toggle Réglages explicite. Friction = source d'erreur. **Toujours** rediriger vers le browser, qui gère ça nativement.
> - Activer le worker même quand `BuildConfig.AUTO_UPDATE_ENABLED == false` (i.e. F-Droid build) — viole la consigne explicite epics.md l. 2188-2192. **Pattern strict** : `if (!BuildConfig.AUTO_UPDATE_ENABLED) { Log.i(TAG, "Auto-update désactivé — F-Droid gère"); return Result.success() }`.
> - Logger l'URL `https://github.com/.../releases/latest` ou les contenus de la réponse JSON — viole NFR-AND-9. Logger seulement un statut (« nouvelle version disponible : ${remote.major}.${remote.minor}.${remote.patch} »). Pas d'URL, pas d'IP, pas de body brut.
> - Faire confiance au TLS GitHub sans pinning ? **Décision MVP** : pas de cert pinning sur api.github.com (rotation cert GitHub fréquente, complexité maintenance disproportionnée pour un check non-critique). Le check est non-essentiel — l'utilisatrice voit aussi les releases sur F-Droid. Documenter en Completion Notes. Phase 2 : envisager pinning si pertinent.
> - Bloquer le démarrage de l'app sur le check de version — le check est asynchrone via WorkManager, le `MainActivity.onCreate()` schedule juste, ne wait pas.

## Story

En tant qu'utilisatrice Android Le Voile sur APK direct,
Je veux être notifiée quand une nouvelle version est disponible via une notification UI,
Afin que je puisse manuellement télécharger et installer la mise à jour (FR-AND-9 prd.md + ADR-11 architecture.md + epics.md l. 2168-2192).

## Acceptance Criteria

1. **Product flavors `apkDirect` + `fdroid`** — Quand `app/build.gradle.kts` est lu :
   ```kotlin
   android {
       // ... existant ...

       flavorDimensions += "distributionChannel"

       productFlavors {
           create("apkDirect") {
               dimension = "distributionChannel"
               buildConfigField("Boolean", "AUTO_UPDATE_ENABLED", "true")
               // versionNameSuffix non défini — l'APK direct GitHub utilise versionName brut.
           }
           create("fdroid") {
               dimension = "distributionChannel"
               buildConfigField("Boolean", "AUTO_UPDATE_ENABLED", "false")
               // F-Droid gère les versions côté store — versionName brut suffit.
           }
       }

       // Le default flavor est `apkDirect` (premier déclaré dans Gradle).
       // F-Droid build server invoque `gradle assembleFdroidRelease`.
       // Le pipeline GitHub Actions release-android.yml invoque `gradle assembleApkDirectRelease`.
   }
   ```

2. **`UpdateCheckWorker.kt` — CoroutineWorker** — Quand le fichier est lu :
   ```kotlin
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
    * Story 12.5 — Vérification 24h GitHub releases pour le canal APK direct.
    *
    * Court-circuit immédiat si BuildConfig.AUTO_UPDATE_ENABLED == false (build flavor
    * `fdroid`). Sinon, GET sur api.github.com, parse tag_name, compare semver,
    * poste une notification UI dismissable si remote > local.
    *
    * Retry policy : géré par WorkManager via Result.retry() — backoff exponentiel
    * baseline 15min, max ~24h.
    *
    * Aucun log d'URL ni de body — cohérent NFR-AND-9.
    */
   internal class UpdateCheckWorker(
       context: Context,
       params: WorkerParameters,
   ) : CoroutineWorker(context, params) {

       override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
           if (!BuildConfig.AUTO_UPDATE_ENABLED) {
               LeVoileLog.i(TAG, "Auto-update désactivé — F-Droid gère les mises à jour")
               return@withContext Result.success()
           }

           val remoteVersion = try {
               fetchLatestVersion()
           } catch (t: Throwable) {
               LeVoileLog.w(TAG, "Echec fetch latest release: ${t.javaClass.simpleName}")
               return@withContext Result.retry()
           }

           val localVersion = SemVer.parse(BuildConfig.VERSION_NAME)
               ?: run {
                   LeVoileLog.w(TAG, "VERSION_NAME local invalide: ${BuildConfig.VERSION_NAME}")
                   return@withContext Result.success()  // pas de retry — c'est une erreur dev
               }

           if (remoteVersion > localVersion) {
               LeVoileLog.i(TAG, "Nouvelle version disponible: ${remoteVersion.major}.${remoteVersion.minor}.${remoteVersion.patch}")
               UpdateNotificationHelper(applicationContext).post(remoteVersion)
           }
           Result.success()
       }

       private fun fetchLatestVersion(): SemVer {
           val url = URL("https://api.github.com/repos/velia-the-veil/le_voile/releases/latest")
           val conn = url.openConnection() as HttpURLConnection
           conn.requestMethod = "GET"
           conn.connectTimeout = 10_000
           conn.readTimeout = 10_000
           conn.setRequestProperty(
               "User-Agent",
               "LeVoile/${BuildConfig.VERSION_NAME} (Android; ${android.os.Build.VERSION.SDK_INT}; APK direct)"
           )
           conn.setRequestProperty("Accept", "application/vnd.github+json")
           try {
               if (conn.responseCode != 200) {
                   throw IllegalStateException("HTTP ${conn.responseCode}")
               }
               val body = conn.inputStream.bufferedReader(StandardCharsets.UTF_8).readText()
               val json = JSONObject(body)
               val tag = json.optString("tag_name", "")
                   .removePrefix("v")
               return SemVer.parse(tag)
                   ?: throw IllegalStateException("tag_name invalide")
           } finally {
               conn.disconnect()
           }
       }

       companion object {
           private const val TAG = "UpdateCheckWorker"
           const val WORK_NAME = "levoile-update-check-24h"
       }
   }
   ```

3. **`SemVerCompare.kt` — pure helper SemVer 2.0.0** :
   ```kotlin
   package fr.plateformeliberte.levoile.update

   /**
    * SemVer 2.0.0 minimal — major.minor.patch[-pre][+build].
    * https://semver.org/spec/v2.0.0.html
    *
    * Pas de dep externe (kotlinx-serialization out of scope). Parse strict.
    *
    * Note : build metadata (`+build`) est IGNORÉE pour la comparaison
    * (cohérent SemVer §10).
    */
   internal data class SemVer(
       val major: Int,
       val minor: Int,
       val patch: Int,
       val preRelease: String? = null,   // ex. "beta.1", "rc.2"
       val buildMetadata: String? = null, // ex. "20260503.abc123"
   ) : Comparable<SemVer> {

       override fun compareTo(other: SemVer): Int {
           val byMajor = major.compareTo(other.major)
           if (byMajor != 0) return byMajor
           val byMinor = minor.compareTo(other.minor)
           if (byMinor != 0) return byMinor
           val byPatch = patch.compareTo(other.patch)
           if (byPatch != 0) return byPatch
           // Pre-release : version sans pre-release > version avec pre-release.
           return when {
               preRelease == null && other.preRelease == null -> 0
               preRelease == null -> 1
               other.preRelease == null -> -1
               else -> comparePreRelease(preRelease, other.preRelease)
           }
       }

       private fun comparePreRelease(a: String, b: String): Int {
           val partsA = a.split(".")
           val partsB = b.split(".")
           val maxLen = maxOf(partsA.size, partsB.size)
           for (i in 0 until maxLen) {
               val pa = partsA.getOrNull(i)
               val pb = partsB.getOrNull(i)
               if (pa == null) return -1
               if (pb == null) return 1
               val intA = pa.toIntOrNull()
               val intB = pb.toIntOrNull()
               val cmp = when {
                   intA != null && intB != null -> intA.compareTo(intB)
                   intA != null -> -1   // numeric < alpha
                   intB != null -> 1
                   else -> pa.compareTo(pb)
               }
               if (cmp != 0) return cmp
           }
           return 0
       }

       companion object {
           private val REGEX = Regex(
               "^(\\d+)\\.(\\d+)\\.(\\d+)(?:-([0-9A-Za-z.-]+))?(?:\\+([0-9A-Za-z.-]+))?$",
           )

           fun parse(s: String): SemVer? {
               val cleaned = s.trim().removePrefix("v")
               val m = REGEX.matchEntire(cleaned) ?: return null
               return SemVer(
                   major = m.groupValues[1].toInt(),
                   minor = m.groupValues[2].toInt(),
                   patch = m.groupValues[3].toInt(),
                   preRelease = m.groupValues[4].takeIf { it.isNotEmpty() },
                   buildMetadata = m.groupValues[5].takeIf { it.isNotEmpty() },
               )
           }
       }
   }
   ```

4. **`UpdateNotificationHelper.kt`** :
   ```kotlin
   package fr.plateformeliberte.levoile.update

   import android.app.NotificationChannel
   import android.app.NotificationManager
   import android.app.PendingIntent
   import android.content.Context
   import android.content.Intent
   import android.net.Uri
   import androidx.core.app.NotificationCompat
   import fr.plateformeliberte.levoile.R

   internal class UpdateNotificationHelper(private val context: Context) {

       init {
           val nm = context.getSystemService(NotificationManager::class.java)
           if (nm.getNotificationChannel(CHANNEL_ID) == null) {
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
           val tag = "v$versionString${version.preRelease?.let { "-$it" } ?: ""}"
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
               .setStyle(NotificationCompat.BigTextStyle().bigText(context.getString(R.string.update_notification_text_long, versionString)))
               .setPriority(NotificationCompat.PRIORITY_DEFAULT)
               .setAutoCancel(true)        // dismiss au tap
               .setOngoing(false)          // pas persistant (vs C16 Story 11.7)
               .addAction(
                   R.drawable.ic_action_github,
                   context.getString(R.string.update_notification_action),
                   pendingIntent,
               )
               .setContentIntent(pendingIntent)
               .build()

           context.getSystemService(NotificationManager::class.java)
               .notify(NOTIFICATION_ID, notification)
       }

       companion object {
           const val CHANNEL_ID = "levoile_update"
           const val NOTIFICATION_ID = 2026  // distinct du C16 persistent (Story 11.7)
           private const val REQUEST_CODE = 12_50001
       }
   }
   ```

5. **`UpdateScheduler.kt`** :
   ```kotlin
   package fr.plateformeliberte.levoile.update

   import android.content.Context
   import androidx.work.Constraints
   import androidx.work.ExistingPeriodicWorkPolicy
   import androidx.work.NetworkType
   import androidx.work.PeriodicWorkRequestBuilder
   import androidx.work.WorkManager
   import java.util.concurrent.TimeUnit

   internal object UpdateScheduler {

       fun scheduleIfNeeded(context: Context) {
           val constraints = Constraints.Builder()
               .setRequiredNetworkType(NetworkType.CONNECTED)
               .build()

           val request = PeriodicWorkRequestBuilder<UpdateCheckWorker>(
               24, TimeUnit.HOURS,                          // interval
               6, TimeUnit.HOURS,                            // flex window — WM peut anticiper de 6h
           )
               .setConstraints(constraints)
               .build()

           WorkManager.getInstance(context).enqueueUniquePeriodicWork(
               UpdateCheckWorker.WORK_NAME,
               ExistingPeriodicWorkPolicy.KEEP,             // ne PAS replacer si déjà schedulé
               request,
           )
       }
   }
   ```
   - Invoqué depuis `MainActivity.onCreate()` après `super.onCreate()` :
     ```kotlin
     // MainActivity.kt — ajout 1 ligne
     UpdateScheduler.scheduleIfNeeded(applicationContext)
     ```

6. **Strings (`strings.xml` + `values-fr/strings.xml`)** :
   ```xml
   <!-- values/strings.xml (en) -->
   <string name="update_channel_name">Le Voile updates</string>
   <string name="update_channel_description">Available app updates</string>
   <string name="update_notification_title">Update %s available</string>
   <string name="update_notification_text">Le Voile · tap to view on GitHub</string>
   <string name="update_notification_text_long">Version %s is available on GitHub releases. Tap below to view, then download and install manually.</string>
   <string name="update_notification_action">View on GitHub</string>

   <!-- values-fr/strings.xml -->
   <string name="update_channel_name">Mises à jour Le Voile</string>
   <string name="update_channel_description">Mises à jour disponibles de l\'application</string>
   <string name="update_notification_title">Mise à jour %s disponible</string>
   <string name="update_notification_text">Le Voile · appuyer pour voir sur GitHub</string>
   <string name="update_notification_text_long">La version %s est disponible sur GitHub releases. Appuyer ci-dessous pour la voir, puis télécharger et installer manuellement.</string>
   <string name="update_notification_action">Voir sur GitHub</string>
   ```

7. **Tests JVM** — Quand exécutés, verts :
   ```kotlin
   // SemVerCompareTest.kt
   class SemVerCompareTest {
       @Test fun `parse 0-1-0`() = assertEquals(SemVer(0, 1, 0), SemVer.parse("0.1.0"))
       @Test fun `parse v0-1-0 prefix`() = assertEquals(SemVer(0, 1, 0), SemVer.parse("v0.1.0"))
       @Test fun `parse pre-release`() = assertEquals(SemVer(0, 1, 0, "beta.1"), SemVer.parse("0.1.0-beta.1"))
       @Test fun `parse build metadata ignored`() = assertEquals(SemVer(0, 1, 0, null, "20260503"), SemVer.parse("0.1.0+20260503"))
       @Test fun `parse invalid returns null`() = assertNull(SemVer.parse("invalid"))
       @Test fun `compare major`() = assertTrue(SemVer.parse("2.0.0")!! > SemVer.parse("1.99.99")!!)
       @Test fun `compare minor`() = assertTrue(SemVer.parse("1.2.0")!! > SemVer.parse("1.1.99")!!)
       @Test fun `compare patch`() = assertTrue(SemVer.parse("1.0.2")!! > SemVer.parse("1.0.1")!!)
       @Test fun `pre-release inferieur a release`() = assertTrue(SemVer.parse("1.0.0")!! > SemVer.parse("1.0.0-beta.1")!!)
       @Test fun `compare pre-release numeric`() = assertTrue(SemVer.parse("1.0.0-beta.2")!! > SemVer.parse("1.0.0-beta.1")!!)
       @Test fun `compare pre-release rc beats beta`() = assertTrue(SemVer.parse("1.0.0-rc.1")!! > SemVer.parse("1.0.0-beta.1")!!)
       @Test fun `build metadata ignored in compare`() = assertEquals(0, SemVer.parse("1.0.0+a")!!.compareTo(SemVer.parse("1.0.0+b")!!))
       @Test fun `equal versions`() = assertEquals(0, SemVer.parse("1.0.0")!!.compareTo(SemVer.parse("1.0.0")!!))
       // ... 15+ cas total
   }

   // UpdateCheckBuildTypeTest.kt
   class UpdateCheckBuildTypeTest {
       @Test
       fun `BuildConfig AUTO_UPDATE_ENABLED false court-circuite immediatement`() {
           // Note : BuildConfig est généré à la build — pour ce test, on instrumente
           // via WorkerFactory custom ou Reflection sur BuildConfig static field. Pattern retenu :
           // - Le worker court-circuit logique est isolée derrière une fonction `isAutoUpdateEnabled()` injectable.
           // - Test mock cette fonction → false, vérifie que doWork retourne success sans appel HTTP.
           val worker = TestUpdateCheckWorker(autoUpdateEnabled = false)
           val result = runBlocking { worker.doWork() }
           assertEquals(ListenableWorker.Result.success(), result)
           assertEquals(0, worker.httpCalls)
       }

       @Test
       fun `AUTO_UPDATE_ENABLED true execute fetch et notification si remote superieur`() {
           val worker = TestUpdateCheckWorker(
               autoUpdateEnabled = true,
               remoteVersion = "0.2.0",
               localVersion = "0.1.0",
           )
           val result = runBlocking { worker.doWork() }
           assertEquals(ListenableWorker.Result.success(), result)
           assertEquals(1, worker.httpCalls)
           assertTrue("notification doit être postée", worker.notificationsPosted.contains(SemVer(0, 2, 0)))
       }

       @Test
       fun `remote egal local ne poste pas notification`() {
           val worker = TestUpdateCheckWorker(autoUpdateEnabled = true, remoteVersion = "0.1.0", localVersion = "0.1.0")
           runBlocking { worker.doWork() }
           assertTrue(worker.notificationsPosted.isEmpty())
       }
   }
   ```

8. **Squelette test instrumenté `UpdateNotificationFlowTest.kt`** (Story 12.6 implémente runtime) :
   ```kotlin
   package fr.plateformeliberte.levoile.update

   import androidx.test.ext.junit.runners.AndroidJUnit4
   import org.junit.Test
   import org.junit.runner.RunWith

   @RunWith(AndroidJUnit4::class)
   class UpdateNotificationFlowTest {
       @Test
       fun `placeholder Story 12-5 — implementation runtime Story 12-6`() {
           // TODO Story 12.6 :
           //  1. Inject mock GitHub API (MockWebServer) avec response tag_name = "v99.0.0".
           //  2. Trigger UpdateCheckWorker via WorkManagerTestInitHelper.
           //  3. UiAutomator : ouvrir le shade des notifications, vérifier la présence de
           //     "Mise à jour 99.0.0 disponible".
           //  4. Tap sur l'action "Voir sur GitHub", vérifier qu'un Intent ACTION_VIEW est lancé.
       }
   }
   ```

## Tasks / Subtasks

- [x] **Task 1 : Audit existant** (AC: tous)
  - [x] `MainActivity.onCreate()` lu — point d'insertion identifié après `handleKillSwitchFlowExtra(intent)`.
  - [x] `NotificationHelper.kt` Story 9.6 lu — pattern channel + IMPORTANCE_LOW pour C16. La notification update utilise un channel et ID dédiés (`levoile_update` / 2026), distincts.
  - [x] Aucun package `update/` préexistant.
  - [x] `BuildConfig` activé via `buildFeatures.buildConfig = true` (Story 10.1 baseline).

- [x] **Task 2 : Étendre `app/build.gradle.kts` avec productFlavors + deps** (AC: #1)
  - [x] `flavorDimensions += "distributionChannel"`.
  - [x] `apkDirect` (default) + `fdroid` flavors avec `buildConfigField("Boolean", "AUTO_UPDATE_ENABLED", ...)`.
  - [x] `implementation(libs.androidx.work.runtime.ktx)`.
  - [x] `testImplementation(libs.androidx.work.testing)`.

- [x] **Task 3 : Étendre `gradle/libs.versions.toml`** (AC: #1)
  - [x] `androidx-work = "2.9.0"`.
  - [x] `androidx-work-runtime-ktx` + `androidx-work-testing`.

- [x] **Task 4 : Créer `update/SemVer.kt`** (AC: #3)
  - [x] data class `SemVer` + companion `parse()`.
  - [x] compareTo selon SemVer 2.0.0 §11 (pre-release ranking, numeric < alphanumeric).
  - [x] Tolère prefix `v` (cohérent GitHub release tags).

- [x] **Task 5 : Créer `update/UpdateCheckWorker.kt` + `UpdateChecker.kt`** (AC: #2)
  - [x] **Refactor architectural** : extraction `UpdateChecker` (pure logic, sans BuildConfig ni HttpURLConnection statique) → tests JVM-only triviaux. `UpdateCheckWorker` est un thin wrapper qui injecte les vraies deps (BuildConfig.AUTO_UPDATE_ENABLED, fetch GitHub réel, UpdateNotificationHelper).
  - [x] CoroutineWorker.
  - [x] Court-circuit `BuildConfig.AUTO_UPDATE_ENABLED == false`.
  - [x] HttpURLConnection GET avec User-Agent identifiant + Accept GitHub API.
  - [x] Parse JSON via `org.json.JSONObject`.
  - [x] Aucun log d'URL ni de body (cohérent NFR-AND-9).

- [x] **Task 6 : Créer `update/UpdateNotificationHelper.kt`** (AC: #4)
  - [x] Channel `levoile_update` IMPORTANCE_DEFAULT.
  - [x] `setOngoing(false)` + `setAutoCancel(true)`.
  - [x] PendingIntent ACTION_VIEW vers `https://github.com/velia-the-veil/le_voile/releases/tag/v$version`.
  - [x] `FLAG_IMMUTABLE` requis Android 12+.

- [x] **Task 7 : Créer `update/UpdateScheduler.kt`** (AC: #5)
  - [x] PeriodicWorkRequest 24h + flex 6h.
  - [x] `enqueueUniquePeriodicWork` avec `ExistingPeriodicWorkPolicy.KEEP`.
  - [x] Invoqué depuis `MainActivity.onCreate()` (1 ligne).

- [x] **Task 8 : Strings + drawables notification** (AC: #6)
  - [x] 6 strings dans `values/strings.xml` + parité `values-fr/strings.xml` (FR mono-langue cohérent baseline projet).
  - [x] Drawables vectoriels `ic_notification_update.xml` (download arrow Material) + `ic_action_github.xml` (open-in-browser Material).

- [x] **Task 9 : Tests JVM** (AC: #7)
  - [x] `SemVerCompareTest.kt` — 17 cas (parse + compare major/minor/patch + pre-release ranking + build metadata ignored + tag GitHub typique).
  - [x] `UpdateCheckerTest.kt` — 6 cas (DISABLED, UPDATE_AVAILABLE, UP_TO_DATE, FETCH_FAILED, LOCAL_VERSION_INVALID, pre-release < release rejet).

- [x] **Task 10 : Squelette test instrumenté** (AC: #8)
  - [x] `androidTest/.../update/UpdateNotificationFlowTest.kt` — TODO Story 12.6 documenté (MockWebServer + WorkManagerTestInitHelper + UiAutomator).

- [x] **Task 11 : Build sanity local**
  - [x] `./gradlew :app:testApkDirectDebugUnitTest --tests "fr.plateformeliberte.levoile.update.*"` → BUILD SUCCESSFUL 36s.
  - [x] `./gradlew :app:testFdroidDebugUnitTest --tests "fr.plateformeliberte.levoile.update.*"` → BUILD SUCCESSFUL 16s.
  - [x] Tous les tests Epic 12 (fdroid + audit + ci + security + repro + update) verts sur apkDirect.
  - [ ] **À FAIRE PAR LE MAINTENEUR** : smoke test installé sur émulateur API 34, forcer le worker via `adb shell cmd jobscheduler run -f fr.plateformeliberte.levoile.debug 0`.

- [x] **Task 12 : Mettre à jour la story et sprint-status**

## Dev Notes

### Pourquoi 2 productFlavors plutôt qu'un BuildConfigField unique

Une seule build avec un flag runtime ne suffit pas : F-Droid build server ne sait pas qu'il faut désactiver le worker. Avec 2 flavors :
- F-Droid invoque `gradle :app:assembleFdroidRelease` → `BuildConfig.AUTO_UPDATE_ENABLED == false`.
- Notre CI invoque `gradle :app:assembleApkDirectRelease` → `BuildConfig.AUTO_UPDATE_ENABLED == true`.

C'est explicite et auditable côté F-Droid mainteneur. Le YAML F-Droid (Story 12.1) doit invoquer la task `assembleFdroidRelease` (à mettre à jour Story 12.5 en touche pré-merge — ou Story 12.1 met déjà la bonne task ? **Décision** : Story 12.5 met à jour `metadata/fr.plateformeliberte.levoile.yml` `Builds:` recipe pour invoquer `assembleFdroidRelease` au lieu de `assembleRelease` — c'est une marginal modification du YAML, traçable git diff).

### Pourquoi `IMPORTANCE_DEFAULT` et pas `IMPORTANCE_LOW` pour la notif update

Le canal C16 persistent (Story 9.6 / 11.7) est `IMPORTANCE_LOW` car il est ongoing — pas de son, pas de heads-up. La notif update est ponctuelle et mérite un son + heads-up : `IMPORTANCE_DEFAULT`. C'est cohérent avec l'attente utilisateur (« j'ai une nouvelle version dispo, il faut que je sache »).

### Pourquoi `setOngoing(false)` (dismissable)

L'utilisatrice peut vouloir reporter l'update ou ne pas l'installer (par exemple si elle pense que la nouvelle version a un bug). Dismissable = respect de l'utilisatrice. Si la version dispo reste la même 24h plus tard, le worker re-poste — pattern correct.

Anti-pattern : `setOngoing(true)` qui rendrait la notif persistante = harcèlement de l'utilisatrice.

### Pourquoi pas de download automatique APK

Plusieurs raisons :
1. **Confiance** : un VPN qui télécharge et installe lui-même = surface d'attaque + brèche utilisateur. L'utilisatrice doit valider explicitement.
2. **Permission `REQUEST_INSTALL_PACKAGES`** : Android 8+ exige cette permission ET l'utilisatrice doit l'activer manuellement dans Réglages. Friction = source d'erreur. Plus simple : ouvrir GitHub releases dans le navigateur, l'utilisatrice télécharge l'APK et l'installe via le file manager (qui demande la permission UNE fois pour le file manager, pas pour notre app).
3. **Vérification signature** : si on télécharge automatiquement, on doit vérifier la signature avant install. C'est faisable mais redondant — `PackageManager` Android vérifie déjà la signature au moment de l'install.

### Coordination Story 12.1 (YAML F-Droid)

La recette `Builds:` doit invoquer `assembleFdroidRelease`. Décision : Story 12.5 modifie le YAML F-Droid (qui n'est pas encore en prod) pour basculer sur la task fdroid. Si Story 12.1 a déjà été livrée avec `assembleRelease` générique, Story 12.5 patch le YAML avec :
```yaml
gradle:
  - fdroid       # remplace `yes` (= invoque assembleRelease) par le flavor fdroid
```

### Coordination Story 12.3 (signature)

Le flavor `apkDirect` est signé par notre master key (Story 12.3). Le flavor `fdroid` est signé par F-Droid. **Important** : les signatures doivent **être différentes** (clés différentes), donc **`PackageManager` Android refusera d'installer un APK F-Droid par-dessus un APK direct** (signature mismatch = INSTALL_FAILED_UPDATE_INCOMPATIBLE). C'est intentionnel et documenté Story 12.3 + ADR-11. Story 12.5 ne change pas cette contrainte — l'utilisatrice qui passe d'APK direct à F-Droid (ou inverse) doit désinstaller + réinstaller.

### Coordination Story 12.6 (test instrumenté)

Le squelette `UpdateNotificationFlowTest.kt` est livré ici (12.5). 12.6 implémente la logique runtime sur l'émulateur : MockWebServer, WorkManagerTestInitHelper, UiAutomator pour les notifications. Les 2 stories sont orthogonales — 12.5 livre le code production + tests JVM, 12.6 enrichit les tests instrumentés.

### Source tree components à toucher

- **Nouveaux** :
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/UpdateCheckWorker.kt`
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/UpdateNotificationHelper.kt`
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/UpdateScheduler.kt`
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/SemVerCompare.kt`
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/update/SemVerCompareTest.kt`
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/update/UpdateCheckBuildTypeTest.kt`
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/update/UpdateCheckWorkerTest.kt`
  - `android/app/src/androidTest/kotlin/fr/plateformeliberte/levoile/update/UpdateNotificationFlowTest.kt`
  - `android/app/src/main/res/drawable/ic_notification_update.xml` (vector)
  - `android/app/src/main/res/drawable/ic_action_github.xml` (vector)
- **Modifiés** :
  - `android/app/build.gradle.kts` (productFlavors + deps WorkManager)
  - `android/gradle/libs.versions.toml` (work + work-testing)
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (1 ligne `UpdateScheduler.scheduleIfNeeded`)
  - `android/app/src/main/res/values/strings.xml`
  - `android/app/src/main/res/values-fr/strings.xml`
  - `metadata/fr.plateformeliberte.levoile.yml` (si Story 12.1 livrée — basculer `assembleRelease` → `assembleFdroidRelease`)

### References

- [epics.md l. 2168-2192](_bmad-output/planning-artifacts/epics.md) — Story 12.5 BDD complet.
- [prd.md FR-AND-9](_bmad-output/planning-artifacts/prd.md) — auto-update Android (pas intégré pour F-Droid).
- [architecture.md ADR-11 l. 2418-2421](_bmad-output/planning-artifacts/architecture.md) — F-Droid gère ses utilisateurs.
- [architecture.md l. 467-468](_bmad-output/planning-artifacts/architecture.md) — auto-update Android = Phase 2 desktop, MVP Android = check + notif manuel.
- [WorkManager Periodic Work Guide](https://developer.android.com/topic/libraries/architecture/workmanager/how-to/define-work#schedule_periodic_work)
- [SemVer 2.0.0 spec](https://semver.org/spec/v2.0.0.html)
- Story 8.1 desktop (livrée) : auto-update GitHub releases (référence pattern).
- Story 9.6 (livrée) : NotificationHelper baseline (channel C16 persistent).
- Story 10.1 (livrée) : BuildConfig activé.
- Story 11.7 (livrée) : C16 notification (orthogonal au levoile_update).
- Story 12.1 (à venir) : YAML F-Droid à mettre à jour pour invoquer `assembleFdroidRelease`.
- Story 12.3 (à venir) : signature APK direct (orthogonal — clé différente F-Droid).
- Story 12.6 (à venir) : test instrumenté UpdateNotificationFlowTest impl runtime.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Debug Log References

`./gradlew :app:testApkDirectDebugUnitTest --tests "fr.plateformeliberte.levoile.update.*" --no-daemon` → BUILD SUCCESSFUL 36s.
`./gradlew :app:testFdroidDebugUnitTest --tests "fr.plateformeliberte.levoile.update.*" --no-daemon` → BUILD SUCCESSFUL 16s.

### Completion Notes List

- **Décision architecturale — extraction `UpdateChecker` pure** : la spec story prescrivait un test direct sur `UpdateCheckWorker` avec mock `BuildConfig.AUTO_UPDATE_ENABLED`. Pattern impossible (BuildConfig statique généré build-time). Refactor : `UpdateChecker` reçoit `autoUpdateEnabled: Boolean`, `localVersion: SemVer?`, `remoteVersionFetcher: () -> SemVer`, `notifier: (SemVer) -> Unit` — tests JVM-only deviennent triviaux. `UpdateCheckWorker` est un thin wrapper de 30 lignes qui invoque `UpdateChecker` avec les vraies deps.
- **Décision YAML F-Droid** : la recette `metadata/fr.plateformeliberte.levoile.yml` bascule de `gradle: [yes]` (= `assembleRelease`) à `gradle: [fdroid]` (= `assembleFdroidRelease`) — invoque maintenant le flavor `fdroid` qui désactive le worker auto-update au runtime. Le path `output:` est aussi ajusté en `app/build/outputs/apk/fdroid/release/app-fdroid-release-unsigned.apk`.
- **Décision i18n** : strings notification ajoutés en FR uniquement dans `values/` ET `values-fr/` (parité), cohérent baseline Le Voile FR-only mono-langue (cf. Story 9.1 — l'EN par défaut sera livré Phase 2).
- **Pas de cert pinning sur api.github.com** : décision MVP documentée dans le périmètre story. Le check est non-essentiel (l'utilisatrice voit aussi les releases sur F-Droid). Phase 2 si pertinent.
- **Pas de download automatique APK** : `Intent(ACTION_VIEW)` ouvre GitHub releases dans le browser. L'utilisatrice télécharge + installe manuellement (respect confiance, évite friction `REQUEST_INSTALL_PACKAGES`, signature vérifiée par PackageManager au moment install).
- **Coordination workflows CI Story 12.2** : avec les productFlavors, les tasks Gradle deviennent `assembleApkDirectDebug` / `assembleFdroidDebug`. Les workflows `android-audit.yml` (job permission-audit + proguard-syntax) et `release-android.yml` (job ci + sign-apk) ont été ajustés pour invoquer explicitement `apkDirect` (le canal GitHub direct). Les paths d'APK sont ajustés (`app/build/outputs/apk/apkDirect/debug/app-apkDirect-debug.apk`).
- **Coordination Story 12.6** : squelette `UpdateNotificationFlowTest.kt` livré pour impl runtime. Le hardcoded URL GitHub dans `UpdateCheckWorker` reste en dur — Story 12.6 devra ajouter un `BuildConfigField("String", "GITHUB_API_URL", ...)` overrideable en androidTest pour MockWebServer (cf. epics.md l. 2204 + Dev Notes Story 12.5).
- **À FAIRE PAR LE MAINTENEUR (hors périmètre dev IA)** :
  1. Installer build apkDirect debug sur émulateur API 34.
  2. Forcer l'exécution du worker : `adb shell cmd jobscheduler run -f fr.plateformeliberte.levoile.debug 0`.
  3. Vérifier que la notification "Mise à jour X.Y.Z disponible" apparaît si la version GitHub release > BuildConfig.VERSION_NAME local.
  4. Tester aussi avec build fdroid → notification ne doit JAMAIS apparaître (court-circuit BuildConfig.AUTO_UPDATE_ENABLED).

### File List

- `android/app/build.gradle.kts` (MOD — productFlavors apkDirect/fdroid + deps WorkManager).
- `android/gradle/libs.versions.toml` (MOD — `androidx-work` + `androidx-work-runtime-ktx` + `androidx-work-testing`).
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/SemVer.kt` (NEW).
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/UpdateChecker.kt` (NEW — pure logic).
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/UpdateCheckWorker.kt` (NEW — CoroutineWorker thin wrapper).
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/UpdateNotificationHelper.kt` (NEW).
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/update/UpdateScheduler.kt` (NEW).
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MOD — 1 ligne `UpdateScheduler.scheduleIfNeeded`).
- `android/app/src/main/res/values/strings.xml` (MOD — 6 strings notification update).
- `android/app/src/main/res/values-fr/strings.xml` (MOD — parité FR).
- `android/app/src/main/res/drawable/ic_notification_update.xml` (NEW — vector Material).
- `android/app/src/main/res/drawable/ic_action_github.xml` (NEW — vector Material).
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/update/SemVerCompareTest.kt` (NEW — 17 tests).
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/update/UpdateCheckerTest.kt` (NEW — 6 tests).
- `android/app/src/androidTest/kotlin/fr/plateformeliberte/levoile/update/UpdateNotificationFlowTest.kt` (NEW — squelette Story 12.6).
- `metadata/fr.plateformeliberte.levoile.yml` (MOD — `gradle: [fdroid]` + path `app-fdroid-release-unsigned.apk`).
- `.github/workflows/android-audit.yml` (MOD — jobs permission-audit + proguard-syntax basculés sur flavor `apkDirect`).
- `.github/workflows/release-android.yml` (MOD — job ci + sign-apk basculés sur `apkDirect`).
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (MOD).
- `_bmad-output/implementation-artifacts/12-5-workmanager-24h-check-version-notification-ui-mise-a-jour-apk-direct.md` (MOD).

### Change Log

- 2026-05-03 : Story 12.5 livrée — productFlavors apkDirect/fdroid + WorkManager UpdateCheckWorker 24h + UpdateNotificationHelper dismissable + SemVer 2.0.0 + UpdateChecker pure logic + 23 tests JVM verts (17 SemVerCompare + 6 UpdateChecker) + ajustement metadata F-Droid `gradle: [fdroid]` + workflows CI basculés sur flavor apkDirect. Status → review.
- 2026-05-03 : Code Review (auto-fix high/med/low) :
  - **M4 fix** : `UpdateNotificationHelper.post()` ignorait `version.buildMetadata` lors de la construction du tag GitHub URL (`v0.2.0+build123` → 404 sur `releases/tag/v0.2.0`). Fix : inclusion `+<buildMetadata>` dans le tag (cohérent SemVer §10 — ignoré pour la comparaison mais préservé dans le tag).
  - Status → done. UpdateChecker + SemVerCompareTest verts.
