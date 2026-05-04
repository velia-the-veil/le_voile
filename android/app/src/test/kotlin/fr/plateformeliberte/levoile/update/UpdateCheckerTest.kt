package fr.plateformeliberte.levoile.update

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Story 12.5 — tests JVM-only de la logique métier `UpdateChecker`.
 *
 * Couvre 3 cas critiques (cohérent epics.md l. 2188-2192 + AC #7) :
 *  1. `BuildConfig.AUTO_UPDATE_ENABLED == false` → court-circuit, pas de fetch,
 *     pas de notif.
 *  2. `enabled = true` + remote > local → notif postée + outcome `UPDATE_AVAILABLE`.
 *  3. `enabled = true` + remote == local → pas de notif + outcome `UP_TO_DATE`.
 *  4. `enabled = true` + fetch fail → outcome `FETCH_FAILED` (caller fait
 *     `Result.retry()`).
 *  5. `enabled = true` + local invalide → outcome `LOCAL_VERSION_INVALID`
 *     (caller fait `Result.success()` — pas un retry).
 */
class UpdateCheckerTest {

    @Test
    fun `AUTO_UPDATE_ENABLED false court-circuite immediatement`() {
        val notifs = mutableListOf<SemVer>()
        var fetchCalled = false
        val checker = UpdateChecker(
            autoUpdateEnabled = false,
            localVersion = SemVer.parse("0.1.0"),
            remoteVersionFetcher = { fetchCalled = true; SemVer.parse("99.0.0")!! },
            notifier = { notifs.add(it) },
        )

        val outcome = checker.check()

        assertEquals(UpdateChecker.Outcome.DISABLED, outcome)
        assertTrue("Aucun fetch ne doit etre invoque quand auto-update desactive", !fetchCalled)
        assertTrue("Aucune notification ne doit etre postee", notifs.isEmpty())
    }

    @Test
    fun `enabled remote superieur local poste la notification`() {
        val notifs = mutableListOf<SemVer>()
        val checker = UpdateChecker(
            autoUpdateEnabled = true,
            localVersion = SemVer.parse("0.1.0"),
            remoteVersionFetcher = { SemVer.parse("0.2.0")!! },
            notifier = { notifs.add(it) },
        )

        val outcome = checker.check()

        assertEquals(UpdateChecker.Outcome.UPDATE_AVAILABLE, outcome)
        assertEquals(1, notifs.size)
        assertEquals(SemVer(0, 2, 0), notifs[0])
    }

    @Test
    fun `enabled remote egal local ne poste pas notification`() {
        val notifs = mutableListOf<SemVer>()
        val checker = UpdateChecker(
            autoUpdateEnabled = true,
            localVersion = SemVer.parse("0.1.0"),
            remoteVersionFetcher = { SemVer.parse("0.1.0")!! },
            notifier = { notifs.add(it) },
        )

        val outcome = checker.check()

        assertEquals(UpdateChecker.Outcome.UP_TO_DATE, outcome)
        assertTrue(notifs.isEmpty())
    }

    @Test
    fun `enabled fetch fail retourne FETCH_FAILED pour Result retry`() {
        val notifs = mutableListOf<SemVer>()
        val checker = UpdateChecker(
            autoUpdateEnabled = true,
            localVersion = SemVer.parse("0.1.0"),
            remoteVersionFetcher = { throw java.io.IOException("network down") },
            notifier = { notifs.add(it) },
        )

        val outcome = checker.check()

        assertEquals(UpdateChecker.Outcome.FETCH_FAILED, outcome)
        assertTrue("Pas de notification quand le fetch a echoue", notifs.isEmpty())
    }

    @Test
    fun `enabled local invalide retourne LOCAL_VERSION_INVALID sans retry`() {
        val notifs = mutableListOf<SemVer>()
        var fetchCalled = false
        val checker = UpdateChecker(
            autoUpdateEnabled = true,
            localVersion = null,    // VERSION_NAME mal formé / absent
            remoteVersionFetcher = { fetchCalled = true; SemVer.parse("99.0.0")!! },
            notifier = { notifs.add(it) },
        )

        val outcome = checker.check()

        assertEquals(UpdateChecker.Outcome.LOCAL_VERSION_INVALID, outcome)
        assertTrue(
            "Pas de fetch utile quand on ne sait meme pas comparer le local",
            !fetchCalled,
        )
        assertTrue(notifs.isEmpty())
    }

    @Test
    fun `enabled remote pre-release inferieur a release ne poste pas notif`() {
        // Use case epics.md l. 2188 : si l'utilisateur a 1.0.0 et le remote est 1.0.0-rc.1,
        // ne PAS proposer le downgrade.
        val notifs = mutableListOf<SemVer>()
        val checker = UpdateChecker(
            autoUpdateEnabled = true,
            localVersion = SemVer.parse("1.0.0"),
            remoteVersionFetcher = { SemVer.parse("1.0.0-rc.1")!! },
            notifier = { notifs.add(it) },
        )
        val outcome = checker.check()

        assertEquals(UpdateChecker.Outcome.UP_TO_DATE, outcome)
        assertTrue(notifs.isEmpty())
    }
}
