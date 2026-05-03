// Package leakcheck est un shim Android (android/shims/leakcheck/)
// exposant la surface "validation STUN anti-fuite" du noyau Go partage.
// Story 9.2 livre uniquement le mecanisme de build gomobile (.aar) ;
// l'API reelle (PeriodicScheduler, WebRTCLeakChecker, ParseXORMappedAddress)
// sera cablee Story 9.7.
//
// Localisation : android/shims/ (et NON android/internal/) car la regle
// Go "internal" interdit l'import depuis le package gobind genere par
// gomobile dans son work dir temporaire.
//
// Conformement a ADR-08 et au perimetre de Story 9.2 :
//   - Aucun import OS-specifique.
//   - Aucune modification de internal/leakcheck ni des autres packages
//     racine (ce shim CONSOMME en lecture seule).
//   - Surface gomobile-compatible (string in/out, bool, int).
//
// CE SHIM IMPORTE internal/leakcheck (qui importe transitivement
// internal/tunnel + quic-go) — c'est intentionnel : Story 9.2 valide
// ainsi que la chaine de build gomobile traverse correctement les
// dependances lourdes (quic-go HTTP/3) annoncees gomobile-compatibles
// par architecture.md l. 68.
package leakcheck

import (
	"strings"

	coreleakcheck "github.com/velia-the-veil/le_voile/internal/leakcheck"
)

// DefaultSTUNServersJoined retourne la liste des serveurs STUN par defaut
// utilises pour la validation anti-fuite, joints par virgule (gomobile
// gere mal les []string en retour direct, on prefere la version
// concatenee pour le smoke test ; la liste reelle sera deballee cote
// Kotlin via String.split() en attendant l'API Story 9.7).
//
// Source : internal/leakcheck.DefaultSTUNServers (Cloudflare + Google).
func DefaultSTUNServersJoined() string {
	return strings.Join(coreleakcheck.DefaultSTUNServers(), ",")
}

// BuildBindingRequestSize retourne la taille en octets d'une requete
// STUN Binding Request standard. Header STUN = 20 octets (type 2 +
// length 2 + magic cookie 4 + transaction id 12). RFC 5389 §6. Wrap fin
// de internal/leakcheck.BuildBindingRequest pour valider que l'ensemble
// de la chaine compile via gomobile (pull transitive de internal/tunnel
// + quic-go).
func BuildBindingRequestSize() int {
	return len(coreleakcheck.BuildBindingRequest())
}
