// Package tunnel — facade gomobile-compatible.
//
// Ce fichier expose un ensemble de fonctions / variables avec types primitifs
// gomobile-friendly (string, int, int64, []byte, func) au-dessus des
// structures internes (Client, http3.Transport, context.Context, channels)
// qui ne sont PAS bindable directement par gomobile.
//
// Cible exclusive de consommation : android/shims/{protocol,auth}/*.go.
// Aucun appel depuis le code desktop (cmd/client, cmd/ui, internal/tun,
// internal/firewall, internal/wfp, internal/nftables, internal/routing, ...).
//
// RÈGLE CRITIQUE — STORY 9.7 (cohérent ADR-08, ADR-09, périmètre story) :
//
//	Ce fichier est ADDITIF. Aucune fonction ici ne doit modifier le
//	comportement existant des fonctions exportées de client.go / pump.go /
//	state.go / reconnect.go. Wrapping pur.
//
//	Toute évolution structurelle (nouveau champ dans Client, nouvelle
//	signature publique sur reconnect, etc.) appartient à une story dédiée
//	+ ADR — pas ici.
//
// Modèle de session : SINGLETON (1 client actif à la fois). Sur Android,
// VpnService garantit déjà qu'une seule session VPN est active à la fois,
// donc le pattern handle-map (souvent utilisé pour exposer plusieurs
// instances Go opaques côté Java) est inutilement complexe. Si un futur
// usage justifie plusieurs sessions concurrentes, refactor vers handle int64
// + sync.Map.
//
// REDACTION PII (NFR-AND-9, NFR22a) : les messages d'erreur Go racine
// (e.g. "post https://de-001.relay.levoile.example/verify: dial tcp
// 1.2.3.4:443: connection refused") contiennent des URLs / IPs / domaines
// qui ne doivent JAMAIS atteindre le statusCallback Kotlin (qui logge
// via Log.i en debug). Tous les chemins error → callback passent par
// redactErrorForStatus() qui ne retourne qu'une classe canonique
// (pinning_failed, verification_failed, network_error, ...).
package tunnel

import (
	"context"
	"errors"
	"sync"
)

// ErrSessionAlreadyOpen est retourné si ConnectGomobile est appelé alors
// qu'une session est déjà active (ou en cours d'établissement). Le caller
// doit appeler CloseGomobile d'abord — pas de re-connect transparent ici.
var ErrSessionAlreadyOpen = errors.New("tunnel: gomobile session already open or connecting, call CloseGomobile first")

// gomobileOutboundBufferSize borne la file de paquets IP en attente d'envoi.
// 256 paquets * MTU 1420 ≈ 360 KB max in-flight côté Go avant back-pressure
// (drop). Choix conservateur : sur un lien saturé, mieux vaut dropper que
// buffer indéfiniment (TCP/QUIC retransmettront).
const gomobileOutboundBufferSize = 256

// gomobileSession encapsule l'état runtime d'une session active.
// Tous les accès passent par gomobileMu.
type gomobileSession struct {
	client         *Client
	cancel         context.CancelFunc
	outbound       chan []byte
	closed         bool // protège contre send sur outbound déjà close
	relayDomain    string
	relayPubKeyB64 string
}

var (
	gomobileMu        sync.Mutex
	gomobileConnecting bool // M-4 : guard contre handshake concurrent dupliqué
	gomobileActive    *gomobileSession
	gomobilePacketCB  func([]byte)
	gomobileStatusCB  func(state, message string)
)

// SetGomobilePacketCallback enregistre le handler appelé pour chaque paquet
// IP arrivé du relais (sens relais → TUN). Le callback est invoqué depuis la
// goroutine de pump — il doit être idempotent et non-bloquant.
//
// Doit être appelé AVANT ConnectGomobile pour ne pas perdre les premiers
// paquets. Passer nil pour désenregistrer.
//
// REENTRANCY (M-8) : le callback NE DOIT PAS appeler de méthode mutatrice
// du facade (ConnectGomobile, WritePacketGomobile, CloseGomobile,
// RequestSessionTokenGomobile, RefreshSessionTokenGomobile). Côté Kotlin,
// GoCoreAdapter.setCallbacks adapte les SAM Kotlin et le user-code respecte
// cette contrainte (cf. doc PacketCallback.kt).
func SetGomobilePacketCallback(cb func(packet []byte)) {
	gomobileMu.Lock()
	gomobilePacketCB = cb
	gomobileMu.Unlock()
}

// SetGomobileStatusCallback enregistre le handler appelé sur changement
// d'état de la session : "connecting", "connected", "disconnected", "error".
// Passer nil pour désenregistrer.
//
// IMPORTANT (NFR-AND-9) : le paramètre `message` passé au callback est
// TOUJOURS une classe d'erreur canonique redactée (e.g. "pinning_failed",
// "network_error") — JAMAIS le message brut Go qui contiendrait URL/IP
// du relais. Voir redactErrorForStatus.
func SetGomobileStatusCallback(cb func(state, message string)) {
	gomobileMu.Lock()
	gomobileStatusCB = cb
	gomobileMu.Unlock()
}

// redactErrorForStatus convertit une erreur Go racine en classe canonique
// safe à exposer via le statusCallback. NFR-AND-9 / NFR22a : aucune
// donnée utilisateur (URL, IP, domaine, payload) ne doit fuiter via les
// logs — ce qui inclut les messages d'erreur structurés `fmt.Errorf` avec
// le `%w` (verbose stderr Go) qui contiennent typiquement le domaine
// du relais et l'IP résolue.
//
// Liste des classes exposées (toutes sentinelles tunnel + context) :
//   - pinning_failed       ErrPinningFailed
//   - verification_failed  ErrVerificationFailed
//   - connection_timeout   ErrConnectionTimeout
//   - not_connected        ErrNotConnected
//   - session_already_open ErrSessionAlreadyOpen
//   - token_expired        ErrTokenExpired
//   - canceled             context.Canceled
//   - timeout              context.DeadlineExceeded
//   - network_error        tout le reste (générique — pas de message)
func redactErrorForStatus(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrPinningFailed):
		return "pinning_failed"
	case errors.Is(err, ErrVerificationFailed):
		return "verification_failed"
	case errors.Is(err, ErrConnectionTimeout):
		return "connection_timeout"
	case errors.Is(err, ErrNotConnected):
		return "not_connected"
	case errors.Is(err, ErrSessionAlreadyOpen):
		return "session_already_open"
	case errors.Is(err, ErrTokenExpired):
		return "token_expired"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		// Catégorie générique — pas de message brut. Le développeur qui
		// debug peut consulter `slog` côté Go (qui n'est pas exposé au
		// callback Kotlin) pour la trace complète.
		return "network_error"
	}
}

// emitStatus invoque le statusCallback enregistré sous lock.
// Conçu pour être appelé sans gomobileMu détenu (sinon deadlock côté shim).
func emitStatus(state, message string) {
	gomobileMu.Lock()
	cb := gomobileStatusCB
	gomobileMu.Unlock()
	if cb != nil {
		cb(state, message)
	}
}

// emitStatusErr est le helper qui transforme une error Go en appel
// emitStatus("error", redacted) — point unique de redaction.
func emitStatusErr(err error) {
	emitStatus("error", redactErrorForStatus(err))
}

// ConnectGomobile établit la session QUIC/HTTP3 vers `relayDomain` avec
// pinning Ed25519 sur `relayPubKeyBase64`, obtient un session token via
// /verify, puis démarre la goroutine de pump bidirectionnel.
//
// Singleton : retourne ErrSessionAlreadyOpen si une session est déjà active
// OU si une autre goroutine est en train de l'établir (M-4 — flag
// connecting tenu sous lock pendant tout le NewClient + Connect). Appel
// synchrone : retour après que /verify a complété (typique < 3s sur
// LTE/Wi-Fi domestique RTT < 80 ms — NFR-AND-2).
//
// En cas d'erreur, aucune session n'est laissée active (cleanup garanti
// + flag connecting toujours libéré).
func ConnectGomobile(relayDomain, relayPubKeyBase64 string) error {
	// M-4 : claim atomique de la position connecting/active.
	gomobileMu.Lock()
	if gomobileActive != nil || gomobileConnecting {
		gomobileMu.Unlock()
		return ErrSessionAlreadyOpen
	}
	gomobileConnecting = true
	gomobileMu.Unlock()

	// Defer cleanup du flag connecting — couvre tous les chemins de retour.
	connectingCleared := false
	defer func() {
		if !connectingCleared {
			gomobileMu.Lock()
			gomobileConnecting = false
			gomobileMu.Unlock()
		}
	}()

	emitStatus("connecting", "")

	c, err := NewClient(relayDomain, relayPubKeyBase64)
	if err != nil {
		emitStatusErr(err)
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Connect(ctx); err != nil {
		cancel()
		emitStatusErr(err)
		return err
	}

	outbound := make(chan []byte, gomobileOutboundBufferSize)

	gomobileMu.Lock()
	// Sanity : gomobileActive devrait être nil (le flag connecting nous
	// protégeait). Si non — bug de logique, on bail out proprement.
	if gomobileActive != nil {
		gomobileMu.Unlock()
		cancel()
		_ = c.Disconnect()
		return ErrSessionAlreadyOpen
	}
	gomobileActive = &gomobileSession{
		client:         c,
		cancel:         cancel,
		outbound:       outbound,
		relayDomain:    relayDomain,
		relayPubKeyB64: relayPubKeyBase64,
	}
	gomobileConnecting = false
	connectingCleared = true
	gomobileMu.Unlock()

	emitStatus("connected", "")

	// Goroutine de pump — vit jusqu'à CloseGomobile ou erreur de stream.
	go runGomobilePump(ctx, c, outbound)

	return nil
}

// runGomobilePump est extrait pour clarté — exécuté dans une goroutine
// dédiée. Convertit le PacketWriter (callback in-bound de tunnel.RunPump)
// en invocation du gomobilePacketCB enregistré.
func runGomobilePump(ctx context.Context, c *Client, outbound <-chan []byte) {
	inbound := func(pkt []byte) (int, error) {
		gomobileMu.Lock()
		cb := gomobilePacketCB
		gomobileMu.Unlock()
		if cb == nil {
			// Pas de consumer côté Kotlin — paquet perdu (back-pressure logique).
			return len(pkt), nil
		}
		// Copy défensive : RunPump réutilise son buffer interne d'itération
		// en itération. Sans copie, le ByteArray côté Kotlin pointerait sur
		// un buffer écrasé au paquet suivant.
		cp := make([]byte, len(pkt))
		copy(cp, pkt)
		cb(cp)
		return len(pkt), nil
	}

	err := c.RunPump(ctx, outbound, inbound)
	if err != nil && ctx.Err() == nil {
		emitStatusErr(err)
	} else {
		emitStatus("disconnected", "")
	}
}

// WritePacketGomobile pousse un paquet IP brut dans la file de pump.
//
// Comportement back-pressure : si la file est pleine
// (gomobileOutboundBufferSize paquets en attente), le paquet est
// silencieusement DROPPÉ. Retourner une erreur ferait crasher la pompe
// Kotlin (qui ne sait pas re-tenter individuellement chaque paquet).
// TCP/QUIC retransmettront naturellement.
//
// Retourne ErrNotConnected si aucune session n'est active.
//
// L-4 : protégé contre le send sur channel close. Si CloseGomobile est
// appelé concurremment et clôt le channel entre notre check sess != nil
// et le send, le flag sess.closed nous fait retourner ErrNotConnected
// proprement plutôt que de paniquer.
func WritePacketGomobile(payload []byte) error {
	gomobileMu.Lock()
	sess := gomobileActive
	if sess == nil || sess.closed {
		gomobileMu.Unlock()
		return ErrNotConnected
	}
	out := sess.outbound
	gomobileMu.Unlock()

	// Copy défensive : le shim Kotlin/Java peut réutiliser le ByteArray
	// (allocation 0 dans la pompe Kotlin de LeVoileVpnService).
	cp := make([]byte, len(payload))
	copy(cp, payload)

	// Recover défensif : si CloseGomobile a clos le channel entre notre
	// check et le send, panic("send on closed channel"). On le swallow
	// proprement.
	defer func() {
		if r := recover(); r != nil {
			// Panic attendu — channel fermé concurremment. Pas d'action
			// (déjà retourné implicitement nil — TCP/QUIC retransmettront).
		}
	}()

	select {
	case out <- cp:
		return nil
	default:
		// File pleine — drop silencieux. Pas d'erreur (cf. doc ci-dessus).
		return nil
	}
}

// CloseGomobile ferme proprement la session QUIC + arrête la pompe.
// Idempotent : peut être appelé même si aucune session n'est active.
func CloseGomobile() error {
	gomobileMu.Lock()
	sess := gomobileActive
	gomobileActive = nil
	if sess != nil {
		// L-4 : marquer le sess comme closed AVANT de close la channel,
		// pour que les WritePacketGomobile concurrents voient le flag
		// sous lock et bail out proprement.
		sess.closed = true
	}
	gomobileMu.Unlock()

	if sess == nil {
		return nil
	}

	// Annuler le contexte avant de fermer la channel : la goroutine de pump
	// observe ctx.Done() et termine proprement (sans setErr car ctx.Err() != nil).
	if sess.cancel != nil {
		sess.cancel()
	}
	// Fermer la channel après cancel — les sends concurrents en cours sont
	// déjà bail-out par sess.closed (ou recover défensif s'ils sont passés
	// la check avant le set).
	if sess.outbound != nil {
		close(sess.outbound)
	}

	err := sess.client.Disconnect()
	emitStatus("disconnected", "")
	return err
}

// RequestSessionTokenGomobile établit une session transitoire vers un relais
// (NewClient + Connect, qui appelle /verify) et retourne le session token
// Ed25519 émis par le relais. Ne PAS lancer la pompe — utile pour valider
// la joignabilité d'un relais (ex. pré-validation registry/failover).
//
// L-6 : si une session globale est déjà ouverte sur le même relayDomain
// **ET** la même relayPubKeyBase64, retourne son token courant sans
// réémettre /verify (économie RTT). Si seul le domaine matche mais pas la
// clé pinnée → re-issue (sécurité : ne PAS exposer un token issu sous une
// autre identité Ed25519).
//
// Le client transitoire est fermé avant retour (pas de fuite de socket QUIC).
func RequestSessionTokenGomobile(relayDomain, relayPubKeyBase64 string) (string, error) {
	gomobileMu.Lock()
	sess := gomobileActive
	gomobileMu.Unlock()
	if sess != nil && sess.client != nil &&
		sess.relayDomain == relayDomain &&
		sess.relayPubKeyB64 == relayPubKeyBase64 {
		if tok := sess.client.SessionToken(); tok != "" {
			return tok, nil
		}
	}

	c, err := NewClient(relayDomain, relayPubKeyBase64)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		return "", err
	}
	return c.SessionToken(), nil
}

// RefreshSessionTokenGomobile force un refresh proactif du token de la
// session active. Retourne le nouveau token. Sans session active : retour
// ErrNotConnected.
func RefreshSessionTokenGomobile() (string, error) {
	gomobileMu.Lock()
	sess := gomobileActive
	gomobileMu.Unlock()
	if sess == nil || sess.client == nil {
		return "", ErrNotConnected
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := sess.client.RefreshSessionToken(ctx); err != nil {
		return "", err
	}
	return sess.client.SessionToken(), nil
}

// ValidateSessionTokenGomobile retourne true si `token` correspond au token
// courant de la session active ET qu'il n'est pas expiré.
//
// La validation cryptographique réelle (signature Ed25519, IP hash) est faite
// par le relais à chaque requête /tunnel — la dupliquer côté client serait
// redondant et fuiterait la logique de signature dans la frontière gomobile.
// Ce check est donc un guard rapide ("vaut le coup d'essayer ce token") plutôt
// qu'une preuve cryptographique.
func ValidateSessionTokenGomobile(token string) bool {
	if token == "" {
		return false
	}
	gomobileMu.Lock()
	sess := gomobileActive
	gomobileMu.Unlock()
	if sess == nil || sess.client == nil {
		return false
	}
	if sess.client.SessionToken() != token {
		return false
	}
	return !sess.client.SessionTokenExpired()
}

// IsGomobileSessionOpen retourne true si une session est actuellement active.
// Utilitaire de diagnostic côté Kotlin (ex. afficher l'état dans un log).
func IsGomobileSessionOpen() bool {
	gomobileMu.Lock()
	defer gomobileMu.Unlock()
	return gomobileActive != nil
}

// ResetGomobileForTest est un helper UNIQUEMENT pour les tests qui veulent
// repartir d'un état propre. Force CloseGomobile + nettoie les callbacks
// + force-clear le flag connecting (les tests peuvent en avoir besoin si
// un test précédent a paniqué pendant Connect).
// NE PAS APPELER depuis du code de production — utilisez CloseGomobile.
func ResetGomobileForTest() {
	_ = CloseGomobile()
	gomobileMu.Lock()
	gomobilePacketCB = nil
	gomobileStatusCB = nil
	gomobileConnecting = false
	gomobileMu.Unlock()
}
