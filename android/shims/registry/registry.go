// Package registry est un shim Android (android/shims/registry/) exposant
// la surface "discovery + verify Ed25519 du registre relais" du noyau Go
// partage. Story 9.2 livre uniquement le mecanisme de build gomobile
// (.aar) ; l'API reelle (Client, Discoverer, failover) sera cablee
// Story 9.7 — la plupart des types racine internal/registry (Client,
// Discoverer, options) ne sont PAS gomobile-bindable directement
// (channels, *http.Client, fonctions options pattern).
//
// Localisation : android/shims/ (et NON android/internal/) car la regle
// Go "internal" interdit l'import depuis le package gobind genere par
// gomobile dans son work dir temporaire.
//
// Conformement a ADR-08 et au perimetre de Story 9.2 :
//   - Aucun import OS-specifique.
//   - Aucune modification de internal/registry ni des autres packages
//     racine (ce shim CONSOMME en lecture seule).
//   - Surface gomobile-compatible (string in/out, bool, int).
package registry

import (
	coreregistry "github.com/velia-the-veil/le_voile/internal/registry"
)

// ExtractCountryCode parse l'identifiant ISO d'un relais (ex.
// "DE-001.relay.levoile.example") et retourne le code pays ISO 3166-1
// alpha-2 ("DE"). Wrap fin de internal/registry.ExtractCountryCode.
// Source : architecture.md l. 441 (selection par pays).
func ExtractCountryCode(id, domain string) string {
	return coreregistry.ExtractCountryCode(id, domain)
}

// SupportedCountryCount retourne le nombre de pays presents dans la
// table CountryMetaMap du noyau (4 pays au moment de Story 9.2 :
// DE, ES, GB, US — source de verite : internal/registry/countries.go).
func SupportedCountryCount() int {
	return len(coreregistry.CountryMetaMap)
}
