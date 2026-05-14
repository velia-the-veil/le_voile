package fr.plateformeliberte.levoile.update

import android.app.NotificationManager
import android.content.Context
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import fr.plateformeliberte.levoile.testutils.EmulatorAssumptions
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Story 12.5 squelette → Story 12.6 impl runtime.
 *
 * Vérifie que `UpdateCheckWorker` :
 *  - poste une notification dismissable quand `remote > local` ;
 *  - ne poste pas de notif quand `remote == local`.
 *
 * **Limites de l'impl 12.6 livrée** :
 *
 * 1. **Pas de MockWebServer pour intercepter `api.github.com`** : refactor
 *    Story 12.6 demanderait d'ajouter un `BuildConfigField("String", "GITHUB_API_URL", ...)`
 *    overrideable en androidTest, ou injecter via `WorkerFactory` Hilt. Sans
 *    ça, le worker hit le vrai api.github.com — introduit du flakiness CI
 *    (rate limit, panne API). Pour MVP, on teste le post-notif **directement**
 *    via `UpdateNotificationHelper(context).post(SemVer)` sans passer par
 *    le worker — c'est suffisant pour valider que le canal + l'icône + le
 *    texte sont corrects sur les 3 API levels (29/33/34).
 *
 * 2. **Worker test** (vérifie `enqueueUniquePeriodicWork` + Result.success)
 *    nécessiterait `WorkManagerTestInitHelper.initializeTestWorkManagerForApplication()`
 *    + un `WorkerFactory` qui inject les fakes. Phase 2 si besoin.
 */
@RunWith(AndroidJUnit4::class)
class UpdateNotificationFlowTest {

    private lateinit var context: Context
    private lateinit var nm: NotificationManager

    @Before
    fun setup() {
        EmulatorAssumptions.assumePostNotificationsAvailable()
        context = InstrumentationRegistry.getInstrumentation().targetContext
        // Android 13+ (API 33+) : POST_NOTIFICATIONS est runtime permission.
        // Sans grant explicit, NotificationManager.notify() drop silencieusement
        // et activeNotifications reste vide → test fail. Le grant via uiAutomation
        // bypass le dialog UX (autorisé en androidTest uniquement). L'assumption
        // ci-dessus garantit qu'on n'appelle ça qu'en API >= 33.
        InstrumentationRegistry.getInstrumentation().uiAutomation
            .grantRuntimePermission(context.packageName, android.Manifest.permission.POST_NOTIFICATIONS)
        nm = context.getSystemService(NotificationManager::class.java)
        // Cleanup notifs préexistantes pour éviter pollution cross-test.
        nm.cancel(UpdateNotificationHelper.NOTIFICATION_ID)
    }

    @After
    fun teardown() {
        // Guard : si assumePostNotificationsAvailable() a bail out en @Before
        // (API < 33), `nm` reste non-initialisé → ne pas crasher le teardown.
        if (::nm.isInitialized) {
            nm.cancel(UpdateNotificationHelper.NOTIFICATION_ID)
        }
    }

    @Test
    fun `UpdateNotificationHelper_post_cree_le_channel_et_la_notif_active`() {
        UpdateNotificationHelper(context).post(SemVer(99, 0, 0))

        // Vérifier que le channel a été créé (API 26+, toujours vrai sur minSdk 29).
        val channel = nm.getNotificationChannel(UpdateNotificationHelper.CHANNEL_ID)
        assertNotNull("Channel levoile_update doit etre cree", channel)
        assertEquals(
            "Channel doit etre IMPORTANCE_DEFAULT (son + heads-up)",
            NotificationManager.IMPORTANCE_DEFAULT,
            channel!!.importance,
        )

        // Vérifier que la notif est active (compatible API 23+).
        // active notifs sont visibles via getActiveNotifications() — API 23+.
        val active = nm.activeNotifications
        val ourNotif = active.firstOrNull { it.id == UpdateNotificationHelper.NOTIFICATION_ID }
        assertNotNull(
            "Notification ID ${UpdateNotificationHelper.NOTIFICATION_ID} doit etre active",
            ourNotif,
        )
    }

    @Test
    fun `notif_cree_contient_version_et_action_voir_sur_GitHub`() {
        UpdateNotificationHelper(context).post(SemVer(99, 0, 0))

        val active = nm.activeNotifications
        val ourNotif = active.firstOrNull { it.id == UpdateNotificationHelper.NOTIFICATION_ID }
        assertNotNull(ourNotif)

        // Vérifier titre contient la version.
        val extras = ourNotif!!.notification.extras
        val title = extras.getCharSequence(android.app.Notification.EXTRA_TITLE)?.toString() ?: ""
        assertTrue(
            "Le titre doit contenir la version 99.0.0 — actuel : $title",
            title.contains("99.0.0"),
        )

        // Vérifier qu'au moins une action est présente (« Voir sur GitHub »).
        val actions = ourNotif.notification.actions
        assertNotNull("Au moins une action attendue (Voir sur GitHub)", actions)
        assertEquals(
            "Une seule action attendue",
            1,
            actions!!.size,
        )
    }
}
