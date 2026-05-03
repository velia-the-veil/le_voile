package fr.plateformeliberte.levoile.conflict

import android.content.Context
import android.content.Intent
import android.net.VpnService

/**
 * Indirection minimaliste autour de `VpnService.prepare(context)` pour
 * rendre [VpnConflictDetector] testable JVM-only sans Robolectric (la
 * méthode statique `VpnService.prepare` n'est pas mockable directement).
 *
 * Visibilité `internal` : on n'expose pas cette interface hors du module
 * `:app`. Pattern symétrique à `SettingsReader` introduit Story 10.1.
 */
internal interface VpnPreparer {
    fun prepare(context: Context): Intent?
}

/**
 * Implémentation par défaut consommée par [VpnConflictDetector] en runtime.
 * Délègue directement à l'API Android `VpnService.prepare`.
 */
internal class RealVpnPreparer : VpnPreparer {
    override fun prepare(context: Context): Intent? = VpnService.prepare(context)
}
