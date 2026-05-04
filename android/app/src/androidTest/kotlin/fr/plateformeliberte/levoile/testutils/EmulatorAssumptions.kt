package fr.plateformeliberte.levoile.testutils

import android.os.Build
import org.junit.Assume

/**
 * Story 12.6 — helpers `Assume.assumeXxx` pour skip les tests sur des
 * configurations émulateur qui ne supportent pas un feature.
 *
 * Pattern : exécuter sur les 3 API levels (29/33/34) mais skip si l'API
 * spécifique manque l'API utilisée par le test. Évite les faux positifs
 * sur la matrice CI.
 */
object EmulatorAssumptions {

    /** Skip si API < requiredApi. */
    fun assumeApiAtLeast(requiredApi: Int) {
        Assume.assumeTrue(
            "Test exige API >= $requiredApi (current: ${Build.VERSION.SDK_INT})",
            Build.VERSION.SDK_INT >= requiredApi,
        )
    }

    /** Skip si POST_NOTIFICATIONS pas géré (API 33+). */
    fun assumePostNotificationsAvailable() {
        // Le runtime check n'est pas nécessaire si le manifest déclare la permission
        // (déclaration depuis API 33). Mais le shade UI peut réagir différemment
        // sur API 29 et 30 — guard explicite.
        assumeApiAtLeast(33)
    }
}
