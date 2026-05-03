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
 *    `err.Error()` côté Go (peut être affiché tel quel — pas de PII : le
 *    facade ne logge pas URL/IP/payload)
 *
 * Story 9.4-9.6 brancheront cette interface pour mettre à jour la
 * notification persistante (titre « Le Voile · {État} »). Story 11.x pour
 * la barre de statut UI principale.
 *
 * Mêmes règles d'invocation que [PacketCallback] : appelée depuis la
 * goroutine Go, doit être idempotente, non-bloquante, et ne JAMAIS lever
 * d'exception.
 */
fun interface StatusCallback {
    fun onStateChange(state: String, message: String)
}
