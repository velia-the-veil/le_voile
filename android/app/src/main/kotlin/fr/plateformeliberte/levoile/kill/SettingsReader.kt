package fr.plateformeliberte.levoile.kill

import android.content.ContentResolver
import android.provider.Settings

/**
 * Indirection minimaliste autour de `Settings.Global.{getString,getInt}`
 * pour rendre [KillSwitchDetector] testable JVM-only sans Robolectric.
 *
 * Visibilité `internal` : on n'expose pas cette interface hors du module
 * `:app`. Story 10.3 (`VpnConflictDetector`) pourra réutiliser le même
 * `SettingsReader` puisqu'elle vit dans le même module — c'est l'extension
 * prévue (cf. story 10.1 § « Coordination Story 10.3 »).
 *
 * Pas de cache : chaque appel fait un round-trip ContentResolver. Le
 * cache n'a pas de sens car l'utilisatrice peut changer le réglage à tout
 * moment depuis Settings → VPN.
 */
internal interface SettingsReader {
    fun getString(name: String): String?
    fun getInt(name: String, default: Int): Int
}

/**
 * Implémentation par défaut consommée par [KillSwitchDetector] en runtime.
 * Lit `Settings.Global.*` via le `ContentResolver` de l'app — opération
 * synchrone très rapide (< 1 ms typique sur AOSP).
 *
 * Heuristique `Settings.Global` exclusive : sur Android 16+, la clé a été
 * migrée vers `Settings.Secure` ET l'accès aux apps tierces a été retiré
 * (SecurityException). Le fallback `Secure` n'est donc pas utile —
 * Android 16+ utilise une autre source de vérité dans [KillSwitchDetector]
 * (API publique `VpnService.isAlwaysOn()` capturée par `LeVoileVpnService`).
 */
internal class ContentResolverSettingsReader(
    private val resolver: ContentResolver,
) : SettingsReader {
    override fun getString(name: String): String? =
        Settings.Global.getString(resolver, name)

    override fun getInt(name: String, default: Int): Int =
        Settings.Global.getInt(resolver, name, default)
}
