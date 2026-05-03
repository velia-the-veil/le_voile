package fr.plateformeliberte.levoile.bridge

/**
 * Callback Go → Kotlin : invoqué par le facade gomobile à chaque transition
 * d'état de la session tunnel.
 *
 * États possibles (string littéraux émis par
 * `internal/tunnel/gomobile_facade.go.emitStatus`) :
 *  - `"connecting"` — handshake QUIC + /verify en cours
 *  - `"connected"`  — session ouverte, pump en route
 *  - `"disconnected"` — session fermée proprement (Close ou EOF)
 *  - `"error"`     — erreur stream / handshake / pinning ; `message` contient
 *    une classe canonique redactée côté Go (« pinning_failed », « network_error »,
 *    etc. — JAMAIS PII via `redactErrorForStatus`)
 *
 * Story 11.7-bis (2026-05-03) : 2 nouveaux paramètres :
 *  - `visibleIp` — IP publique du relais résolue via DNS au moment de
 *    `connected` (vide pour les autres états). Permet d'afficher
 *    « 🇩🇪 Allemagne · 5.45.6.7 » dans la notification persistante.
 *  - `effectiveCountry` — code ISO 3166-1 alpha-2 du pays effectif du relais
 *    (extrait du domaine via `internal/registry.ExtractCountryCode`).
 *    Vide pour les autres états. Permet de réconcilier `currentCountry`
 *    côté Service avec ce que le relais a réellement honoré (utile si le
 *    backend Go fait du round-robin inter-pays en cas de fallback).
 *
 * Mêmes règles d'invocation que [PacketCallback] : appelée depuis la
 * goroutine Go, doit être idempotente, non-bloquante, et ne JAMAIS lever
 * d'exception.
 */
fun interface StatusCallback {
    fun onStateChange(state: String, message: String, visibleIp: String, effectiveCountry: String)
}
