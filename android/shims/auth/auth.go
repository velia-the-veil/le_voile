// Package auth est un shim Android (android/shims/auth/) exposant la
// surface "session tokens Ed25519" du noyau Go partage. Story 9.2 a livre
// les constantes pure-data ; Story 9.7 cable la surface fonctionnelle
// (Issue / Refresh / Validate) via internal/tunnel/gomobile_facade.go.
//
// Localisation : android/shims/ (et NON android/internal/) car la regle
// Go "internal" interdit l'import depuis le package gobind genere par
// gomobile dans son work dir temporaire.
//
// Conformement a ADR-08 et au perimetre de Story 9.7 :
//   - Aucun import OS-specifique.
//   - Aucune modification de internal/tunnel/{client,pump,types,...}.go
//     (tous INTACTS — la facade gomobile_facade.go est purement additive).
//   - Aucune duplication de la logique signature Ed25519 / IP hash : tout
//     est delegue a internal/tunnel/client.go (verifyRelay,
//     RefreshSessionToken). Garantie de coherence cross-OS bit-a-bit.
//   - Surface gomobile-compatible (string in/out, bool, int, int64).
package auth

import (
	"github.com/velia-the-veil/le_voile/internal/tunnel"
)

// TokenHeaderName retourne le nom du header HTTP transportant le session
// token Ed25519 vers le relais. Source : architecture.md l. 437.
//
// Note : la facade Story 9.7 utilise "Authorization: Bearer <token>"
// (cf. internal/tunnel/pump.go RunPump). Cette constante est conservee pour
// le smoke test Story 9.2 et la documentation cote Kotlin.
func TokenHeaderName() string {
	return "Authorization"
}

// TokenTTLSeconds retourne la duree de vie nominale d'un session token
// emis par le relais (4 heures). Source : architecture.md l. 447,
// PRD NFR9c.
func TokenTTLSeconds() int64 {
	return 4 * 60 * 60
}

// TokenRefreshThresholdSeconds retourne la fenetre avant expiration en
// dessous de laquelle le client doit proactivement rafraichir son token
// (15 minutes). Story 9.7 cable EnsureSessionToken sur cette valeur via
// la facade.
func TokenRefreshThresholdSeconds() int64 {
	return 15 * 60
}

// IssueSessionToken etablit une session transitoire vers le relais
// (NewClient + Connect, qui appelle /verify) et retourne le session token
// Ed25519 emis. Coherent NFR9c — la signature est strictement bit-a-bit
// identique a celle qu'emettrait le client desktop sur le meme relais (le
// relais ne peut pas distinguer un token Android d'un token desktop).
//
// Si une session globale est deja ouverte sur le meme relayDomain, retourne
// son token courant sans reemettre /verify (economie RTT).
//
// Le client transitoire est ferme avant retour (pas de fuite de socket QUIC).
//
// Cote Kotlin : exception Java si /verify echoue (relais injoignable, cert
// pinning fail, signature Ed25519 invalide).
func IssueSessionToken(relayDomain string, relayPubKeyBase64 string) (string, error) {
	return tunnel.RequestSessionTokenGomobile(relayDomain, relayPubKeyBase64)
}

// RefreshSessionToken force un refresh proactif du token de la session
// active (single-flight + backoff exponentiel + circuit breaker — heritage
// Client.RefreshSessionToken). Retourne le nouveau token.
//
// Sans session active : retour erreur (ErrNotConnected). Le caller Kotlin
// doit alors basculer sur IssueSessionToken pour reouvrir.
//
// Note signature : la version racine Client.RefreshSessionToken prend un
// context.Context — la facade utilise un timeout interne (connectTimeout)
// car gomobile ne peut pas exposer context.Context.
func RefreshSessionToken() (string, error) {
	return tunnel.RefreshSessionTokenGomobile()
}

// ValidateSessionToken retourne true si le token correspond au token
// courant de la session active ET qu'il n'est pas expire (TTL non depasse).
//
// La validation cryptographique reelle (Ed25519, IP hash) est faite par le
// relais a chaque requete /tunnel — la dupliquer cote client serait
// redondant, fuiterait la logique de signature dans la frontiere gomobile,
// et ouvrirait la porte a une divergence subtile entre client et relais.
// Ce check est donc un guard rapide ("vaut le coup d'essayer ce token")
// plutot qu'une preuve cryptographique. Coherent ADR-09 (reutilisation 100%
// — ne PAS reimplementer).
func ValidateSessionToken(token string) bool {
	return tunnel.ValidateSessionTokenGomobile(token)
}
