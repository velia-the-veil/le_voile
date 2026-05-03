package fr.plateformeliberte.levoile.log

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 10.5 — scan source qui fail si un site `Log.*` ou `LeVoileLog.*`
 * contient une variable interpolée révélatrice du trafic ou de
 * l'environnement utilisateur.
 *
 * Cohérent NFR-AND-9 (prd.md l. 705) + NFR22a (prd.md l. 672) + ADR-15.
 *
 * Stratégie : pas de parser AST Kotlin (overkill MVP), pas de regex
 * multi-ligne. Lecture ligne-par-ligne + `String.contains` ciblant les
 * patterns d'interpolation Kotlin `$name` ou `${'$'}{name}`.
 *
 * Scan limité à `src/main/kotlin/` — les `src/test/` peuvent légitimement
 * contenir des fixtures qui matcheraient (ex. assertion sur une chaîne
 * de log attendue dans un test BDD futur). Test = code non-production.
 *
 * Le scan **inclut** `LeVoileLog.*` ET `android.util.Log.*` — le wrapper
 * n'échappe pas. Inclut aussi `Log.w` / `Log.e` qui ne sont **pas**
 * strippés en release (release WARN+ visible) : un dev pourrait introduire
 * une fuite via `Log.w(TAG, "User clicked $url")` qui passe ProGuard et
 * reste visible Logcat. Le scan statique est le filet de sécurité ultime.
 */
class LogFilteringTest {

    private val forbiddenInterpolations = listOf(
        "url",
        "domain",
        "destIp",
        "userContent",
        "requestBody",
        "responseBody",
        "packageName",
        "foreignAppId",
        "pinnedApp",
    )

    @Test
    fun `aucun log ne contient de variable interdite`() {
        val srcDir = resolveMainKotlinDir()
        val violations = mutableListOf<String>()
        var scannedFileCount = 0

        // Exclusion path-based (vs filename-based) : si un dev futur crée un autre
        // fichier `LeVoileLog.kt` ailleurs, le scan doit le visiter. Seul le wrapper
        // canonique sous .../log/LeVoileLog.kt est exclu (son Kdoc mentionne
        // intentionnellement les variables interdites pour documenter la règle).
        val canonicalWrapperPath = "fr/plateformeliberte/levoile/log/LeVoileLog.kt"
            .replace('/', File.separatorChar)

        srcDir.walkTopDown()
            .filter { it.isFile && it.extension == "kt" }
            .filter { ktFile ->
                val rel = ktFile.relativeTo(srcDir).path
                rel != canonicalWrapperPath
            }
            .forEach { ktFile ->
                scannedFileCount++
                val content = ktFile.readText()
                content.lineSequence().forEachIndexed { idx, line ->
                    if (!isLogCallLine(line)) return@forEachIndexed
                    forbiddenInterpolations.forEach { variable ->
                        val patternBraced = "\${$variable}"
                        val patternBare = "\$$variable"
                        if (line.contains(patternBraced) || line.contains(patternBare)) {
                            violations += "${ktFile.relativeTo(srcDir).path}:${idx + 1} → variable interdite '\$$variable' (NFR-AND-9)"
                        }
                    }
                }
            }

        // Garde-fou fail-open : si le scan a parcouru 0 fichier (mauvais cwd
        // Gradle, repo vide, mauvaise résolution chemin), un PASS silencieux
        // est trompeur — on lève une AssertionError explicite.
        assertTrue(
            "LogFilteringTest n'a scanné aucun fichier .kt — vérifier resolveMainKotlinDir() et le cwd Gradle. srcDir=$srcDir",
            scannedFileCount > 0,
        )

        if (violations.isNotEmpty()) {
            val report = violations.joinToString("\n  ", prefix = "  ")
            throw AssertionError(
                """
                |❌ Pattern log interdit détecté — cohérent NFR-AND-9 (prd.md l. 705) :
                |$report
                |
                |Le client Android Le Voile ne doit JAMAIS logger : URL, domaine, IP destination,
                |contenu utilisateur, packageName d'app concurrente, ou tout autre identifiant
                |révélateur du trafic ou de l'environnement utilisateur.
                |Reformulez le log en supprimant la variable, ou justifiez via ADR si la variable
                |est en réalité non-sensible (ex. constante de build).
                """.trimMargin()
            )
        }
    }

    @Test
    fun `liste forbiddenInterpolations contient les 6 variables canoniques`() {
        // Anti-regression : si quelqu'un retire une variable de la liste, ce
        // test fail explicitement. Les 6 variables canoniques sont définies
        // dans epics.md l. 1799 + l. 1802.
        val canonical = listOf(
            "url",
            "domain",
            "destIp",
            "userContent",
            "requestBody",
            "responseBody",
        )
        canonical.forEach { v ->
            assertTrue(
                "La liste forbiddenInterpolations doit contenir '$v' (cohérent epics.md l. 1799-1802)",
                forbiddenInterpolations.contains(v),
            )
        }
    }

    private fun isLogCallLine(line: String): Boolean {
        // Détection naïve mais suffisante : présence d'un appel Log.[diwev]( ou LeVoileLog.[iwe](
        // sur la ligne. Faux-positifs possibles sur les commentaires « // Log.i(...) à éviter »
        // — c'est intentionnel : un commentaire qui mentionne un log avec variable interdite
        // signale probablement un site futur de violation, autant l'attraper.
        // Pour LeVoileLog : on exige une parenthèse `(` derrière le nom de méthode
        // pour ne pas matcher les `import fr...LeVoileLog.i` ni les références sans appel.
        return line.contains("Log.d(") ||
            line.contains("Log.v(") ||
            line.contains("Log.i(") ||
            line.contains("Log.w(") ||
            line.contains("Log.e(") ||
            line.contains("LeVoileLog.i(") ||
            line.contains("LeVoileLog.w(") ||
            line.contains("LeVoileLog.e(")
    }

    private fun resolveMainKotlinDir(): File {
        // cwd Gradle pour :app:testDebugUnitTest peut etre android/, android/app/,
        // ou la racine du repo. On essaie plusieurs candidats — pattern aligne
        // avec MainActivityConfigTest.parseManifest et AuditCITest.resolveWorkflow.
        val candidates = listOf(
            File("src/main/kotlin"),
            File("app/src/main/kotlin"),
            File("../app/src/main/kotlin"),
            File("android/app/src/main/kotlin"),
        )
        return candidates.firstOrNull { it.isDirectory }
            ?: throw AssertionError(
                "Dossier src/main/kotlin/ introuvable. " +
                    "user.dir=${System.getProperty("user.dir")} ; " +
                    "candidates : ${candidates.joinToString { it.absolutePath }}",
            )
    }
}
