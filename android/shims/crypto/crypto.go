// Package crypto est un shim Android (android/shims/crypto/) exposant la
// surface "Ed25519 + pinning" du noyau Go partage. Story 9.2 livre
// uniquement le mecanisme de build gomobile (.aar) ; l'API reelle (Sign,
// Verify, VerifyEd25519CertPin) sera cablee Story 9.7 via une frontiere
// elargie.
//
// Localisation : android/shims/ (et NON android/internal/) car la regle
// Go "internal" interdit l'import depuis le package gobind genere par
// gomobile dans son work dir temporaire. Ce shim importe le package
// racine internal/crypto (la regle Go autorise cet import car le shim
// vit dans le meme module).
//
// Conformement a ADR-08 et au perimetre de Story 9.2 :
//   - Aucun import OS-specifique.
//   - Aucune modification de internal/crypto ni des autres packages
//     racine (ce shim CONSOMME en lecture seule).
//   - Surface gomobile-compatible (string in/out, bool, int).
package crypto

import (
	corecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

// Ed25519PublicKeySize retourne la taille en octets d'une cle publique
// Ed25519 (32). Source : crypto/ed25519 stdlib.
func Ed25519PublicKeySize() int {
	return 32
}

// IsValidPublicKeyBase64 retourne true si la string fournie est une cle
// publique Ed25519 base64 valide (forme attendue dans le registre signe
// et le pinning relais). Wrap fin de internal/crypto.ImportPublicKeyBase64.
//
// Sortie booleenne uniquement (gomobile mappe `error` Go en `Exception`
// Java ; on prefere ici une API booleenne plus simple cote Kotlin pour
// le smoke test).
func IsValidPublicKeyBase64(s string) bool {
	_, err := corecrypto.ImportPublicKeyBase64(s)
	return err == nil
}

// ReleasePublicKeyCurrentBase64 retourne la cle publique master Ed25519
// courante utilisee pour signer le registre relais. Source :
// internal/crypto.ReleasePublicKeyCurrentBase64.
func ReleasePublicKeyCurrentBase64() string {
	return corecrypto.ReleasePublicKeyCurrentBase64
}
