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
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"

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

// ============================================================================
// Story 11.7-bis : API gomobile-friendly pour Parse + Verify + Pick.
// ============================================================================
//
// Le `coreregistry.Discoverer` n'est pas gomobile-bindable directement
// (channels, *http.Client, fonctions options pattern). Cette section expose
// une API stateless avec des types primitifs (string, int, []byte, error)
// qui couvre les 3 operations essentielles cote Android :
//   1. ParseAndVerify(jsonBytes, masterPubKeyB64) → numRelays valides
//   2. PickRelayDomainForCountry(jsonBytes, iso, roundRobinIndex) → domain
//   3. PickRelayPubKeyForCountry(jsonBytes, iso, roundRobinIndex) → pubKeyB64
//
// Le caller Kotlin maintient son propre round-robin counter (atomique)
// + cache le jsonBytes en RAM + persiste dans ConfigStore (Story 11.8).

// ParseAndVerify decode un registre relais JSON + verifie deux contraintes :
//  1. Le `master_public_key` declare dans le JSON correspond EXACTEMENT a
//     la cle Ed25519 bundled dans l'APK Android (TOFU — Trust On First Use).
//     Empeche un attaquant de servir un faux registre signe par SA propre
//     master key.
//  2. La signature Ed25519 de chaque entree est valide contre la master key.
//
// Retourne le nombre de relais valides apres verification (0 si registre
// vide, master key mismatch, ou signature invalide pour tous).
//
// `expectedMasterPubKeyB64` : 32 octets Ed25519 encodes base64 standard,
// recupere par le caller Kotlin depuis res/raw/registry_master_pubkey
// (bundled dans l'APK, pas un secret — c'est la cle PUBLIQUE du master).
//
// Erreurs possibles :
//   - ErrInvalidMasterKey : cle bundled mal formee OU mismatch avec JSON
//   - ErrRegistryEmpty / ErrNoValidRelays : registre invalide
//   - "registry: parse: ..." : JSON malforme / version incorrecte
//
// Pas de log cote Go (NFR-AND-9 — le caller Kotlin gere via LeVoileLog).
func ParseAndVerify(jsonBytes []byte, expectedMasterPubKeyB64 string) (int, error) {
	reg, err := coreregistry.Parse(jsonBytes)
	if err != nil {
		return 0, fmt.Errorf("registry: parse: %w", err)
	}
	// Pin TOFU : la master key dans le JSON DOIT matcher la cle bundled.
	// Comparaison sur le base64 brut (ed25519.Verify utilise les bytes
	// decodes — meme contrat). Strip whitespace defensif (contributeurs
	// peuvent introduire \n / espaces a la copie).
	expected := strings.TrimSpace(expectedMasterPubKeyB64)
	declared := strings.TrimSpace(reg.MasterPublicKey)
	if expected == "" || declared == "" || expected != declared {
		return 0, coreregistry.ErrInvalidMasterKey
	}
	// Sanity : la cle bundled est bien Ed25519 32 bytes.
	if expectedRaw, err := base64.StdEncoding.DecodeString(expected); err != nil ||
		len(expectedRaw) != ed25519.PublicKeySize {
		return 0, coreregistry.ErrInvalidMasterKey
	}
	verified, err := reg.VerifyAll()
	if err != nil {
		return 0, err
	}
	return len(verified), nil
}

// PickRelayDomainForCountry retourne le domaine du relais selectionne pour
// le pays demande (round-robin intra-pays via roundRobinIndex). Si le pays
// n'est pas dans la whitelist OU n'a pas de relais valides, retourne ""
// + ErrNoRelaysForCountry.
//
// Round-robin : le caller Kotlin maintient un counter atomique
// (RelayPicker.kt) ; cette fonction est stateless et utilise simplement
// `roundRobinIndex % len(relaysForCountry)` pour selectionner.
//
// Le iso est attendu en majuscules (ex. "DE") cohérent
// LeVoileBridge.COUNTRIES_WHITELIST. Convert interne en minuscules pour
// match avec coreregistry.CountryMetaMap.
func PickRelayDomainForCountry(jsonBytes []byte, iso string, roundRobinIndex int) (string, error) {
	relay, err := pickRelayHelper(jsonBytes, iso, roundRobinIndex)
	if err != nil {
		return "", err
	}
	return relay.Domain, nil
}

// PickRelayPubKeyForCountry retourne la cle Ed25519 publique du relais
// selectionne pour le pays demande, encodee en base64 standard. Meme
// algorithme que PickRelayDomainForCountry — round-robin intra-pays.
//
// Cette cle PUBLIQUE est consommee par GoCoreAdapter.connect(domain, pubKey)
// pour le pinning Ed25519 du certificat TLS du relais.
func PickRelayPubKeyForCountry(jsonBytes []byte, iso string, roundRobinIndex int) (string, error) {
	relay, err := pickRelayHelper(jsonBytes, iso, roundRobinIndex)
	if err != nil {
		return "", err
	}
	return relay.PublicKey, nil
}

// pickRelayHelper factorise la selection (parse + filter par country +
// round-robin). Pas exposee gomobile (retourne *coreregistry.RelayEntry,
// non-bindable).
func pickRelayHelper(jsonBytes []byte, iso string, roundRobinIndex int) (coreregistry.RelayEntry, error) {
	if iso == "" {
		return coreregistry.RelayEntry{}, coreregistry.ErrUnknownCountry
	}
	reg, err := coreregistry.Parse(jsonBytes)
	if err != nil {
		return coreregistry.RelayEntry{}, err
	}
	isoLower := strings.ToLower(iso)
	var matching []coreregistry.RelayEntry
	for _, r := range reg.Relays {
		// Le code pays peut etre extrait de l'ID ou du domaine.
		code := strings.ToLower(coreregistry.ExtractCountryCode(r.ID, r.Domain))
		if code == isoLower {
			matching = append(matching, r)
		}
	}
	if len(matching) == 0 {
		return coreregistry.RelayEntry{}, coreregistry.ErrNoRelaysForCountry
	}
	idx := roundRobinIndex % len(matching)
	if idx < 0 {
		idx = -idx
	}
	return matching[idx], nil
}
