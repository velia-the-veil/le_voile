// Package protocol est un shim Android (android/shims/protocol/) exposant
// la surface "framing tunnel + handshake QUIC/HTTP3 + pump IP" du noyau Go
// partage. Story 9.2 a livre les constantes pure-data ; Story 9.7 cable la
// surface fonctionnelle via internal/tunnel/gomobile_facade.go (pattern
// facade additif strict — zero modification client.go/pump.go).
//
// Localisation : android/shims/ (et NON android/internal/) car la regle
// Go "internal" interdit l'import depuis le package gobind genere par
// gomobile dans son work dir temporaire.
//
// Conformement a ADR-08 et au perimetre de Story 9.7 :
//   - Aucun import OS-specifique (internal/tun, internal/firewall, etc.).
//   - Aucune modification de internal/tunnel/{client,pump,types,state,
//     reconnect}.go (tous INTACTS — vérifiable via git diff).
//   - Surface gomobile-compatible (string in/out, bool, int, []byte,
//     interfaces simples).
package protocol

import (
	"github.com/velia-the-veil/le_voile/internal/tunnel"
)

// Version retourne la version du protocole filaire utilise entre le client
// et le relais. Utile pour le smoke test JUnit (LeVoileCoreSmokeTest) qui
// resoud la classe generee par gomobile et invoque cette methode pure-data
// sans declencher de chargement JNI complet.
func Version() string {
	return "levoile-1"
}

// FramingHeaderSize retourne la taille en octets du header de framing utilise
// dans le stream HTTP/3 /tunnel : 2 octets longueur (uint16 big-endian) +
// payload IP brut. Source : architecture.md l. 437, internal/tunnel/pump.go.
func FramingHeaderSize() int {
	return 2
}

// PacketCallback est l'interface implementee cote Kotlin (Story 9.7) pour
// recevoir les paquets IP arrives du relais (sens relais -> TUN). gomobile
// genere une interface Java miroir consommable par Kotlin.
//
// IMPORTANT : la methode est appelee depuis la goroutine de pump Go ; cote
// Kotlin elle doit etre idempotente, non-bloquante, et ne jamais lever
// d'exception (qui crasherait la JVM via gomobile JNI). Voir docstring
// LeVoileBridge / GoCoreAdapter cote Kotlin pour les regles.
type PacketCallback interface {
	OnPacketReceived(packet []byte)
}

// StatusCallback est l'interface implementee cote Kotlin pour observer les
// transitions d'etat de la session : "connecting", "connected",
// "disconnected", "error". Le second argument transporte un message
// optionnel (ex. message d'erreur Go converti en string par .Error()).
type StatusCallback interface {
	OnStateChange(state string, message string)
}

// SetPacketCallback enregistre le handler de paquets IP entrants. Doit etre
// appele AVANT Connect pour ne pas perdre les premiers paquets. Passer nil
// pour desenregistrer.
//
// Comportement gomobile : l'argument cb est une interface Java
// (PacketCallback) cote Kotlin ; ce shim la convertit en func([]byte) Go
// pour la registrer dans la facade.
func SetPacketCallback(cb PacketCallback) {
	if cb == nil {
		tunnel.SetGomobilePacketCallback(nil)
		return
	}
	tunnel.SetGomobilePacketCallback(cb.OnPacketReceived)
}

// SetStatusCallback enregistre le handler de transitions d'etat.
// Passer nil pour desenregistrer.
func SetStatusCallback(cb StatusCallback) {
	if cb == nil {
		tunnel.SetGomobileStatusCallback(nil)
		return
	}
	tunnel.SetGomobileStatusCallback(cb.OnStateChange)
}

// Connect etablit une session QUIC/HTTP3 vers https://{relayDomain}/tunnel
// avec validation Ed25519 du certificat (cert pinning) et obtention d'un
// session token via /verify. Demarre la goroutine de pump bidirectionnel
// (paquets IP <-> stream HTTP/3).
//
// Singleton : retourne une erreur (tunnel: gomobile session already open)
// si une session est deja active. CALLER DOIT appeler Close() d'abord.
//
// Synchrone : retour apres /verify complete (typique < 3 sec sur LTE/Wi-Fi
// domestique RTT < 80 ms — NFR-AND-2).
//
// En cas d'erreur, aucun etat residuel n'est laisse (cleanup garanti).
//
// Le session token n'est PAS expose ici en parametre/retour : la facade le
// gere en interne. Pour l'obtenir explicitement, voir Auth.IssueSessionToken.
func Connect(relayDomain string, relayPubKeyBase64 string) error {
	return tunnel.ConnectGomobile(relayDomain, relayPubKeyBase64)
}

// WritePacket pousse un paquet IP brut dans la file d'envoi du pump.
//
// Comportement back-pressure : si la file interne est pleine, le paquet est
// silencieusement DROPPE (TCP/QUIC retransmettra). Pas d'erreur retournee
// dans ce cas — voir gomobile_facade.WritePacketGomobile.
//
// Retourne ErrNotConnected (Java Exception cote Kotlin) si aucune session
// n'est active.
func WritePacket(payload []byte) error {
	return tunnel.WritePacketGomobile(payload)
}

// Close ferme proprement la session QUIC + arrete la pompe. Idempotent :
// peut etre appele meme si aucune session n'est active.
func Close() error {
	return tunnel.CloseGomobile()
}

// IsSessionOpen retourne true si une session est actuellement active.
// Utilitaire de diagnostic cote Kotlin (ex. afficher l'etat dans un log).
func IsSessionOpen() bool {
	return tunnel.IsGomobileSessionOpen()
}
