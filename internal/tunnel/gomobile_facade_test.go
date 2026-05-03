package tunnel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// Tests unitaires de gomobile_facade.go — couverture des chemins ne nécessitant
// pas de vrai relais QUIC. Les chemins handshake/pump réels sont déjà couverts
// par client_test.go (TestVerifyRelayWithSelfSignedCert ...) et pump_test.go
// (TestRunPumpLoops ...) sur les types internes ; la facade ne fait que
// déléguer.

func TestGomobile_WritePacketBeforeConnect(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	if err := WritePacketGomobile([]byte{0x01, 0x02, 0x03}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("WritePacketGomobile sans session : attendu ErrNotConnected, eu %v", err)
	}
}

func TestGomobile_CloseIdempotent(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	// Deux Close consécutifs sans session active : pas d'erreur, pas de panic.
	if err := CloseGomobile(); err != nil {
		t.Fatalf("CloseGomobile #1 sans session : attendu nil, eu %v", err)
	}
	if err := CloseGomobile(); err != nil {
		t.Fatalf("CloseGomobile #2 sans session : attendu nil, eu %v", err)
	}
}

func TestGomobile_IsSessionOpenFalseInitially(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	if IsGomobileSessionOpen() {
		t.Fatal("IsGomobileSessionOpen : attendu false sans session active")
	}
}

func TestGomobile_ValidateTokenEmptyOrNoSession(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	if ValidateSessionTokenGomobile("") {
		t.Fatal("ValidateSessionTokenGomobile(\"\") : attendu false")
	}
	if ValidateSessionTokenGomobile("non-empty-but-no-session") {
		t.Fatal("ValidateSessionTokenGomobile sans session : attendu false")
	}
}

func TestGomobile_RefreshSessionTokenNoSession(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	tok, err := RefreshSessionTokenGomobile()
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("RefreshSessionTokenGomobile sans session : attendu ErrNotConnected, eu err=%v tok=%q", err, tok)
	}
	if tok != "" {
		t.Fatalf("RefreshSessionTokenGomobile sans session : token attendu vide, eu %q", tok)
	}
}

func TestGomobile_StatusCallbackInvoked(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	var mu sync.Mutex
	var captured []string

	SetGomobileStatusCallback(func(state, message, _, _ string) {
		mu.Lock()
		captured = append(captured, state+":"+message)
		mu.Unlock()
	})

	emitStatus("connecting", "")
	emitStatus("error", "boom")

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("attendu 2 status events, eu %d : %v", len(captured), captured)
	}
	if captured[0] != "connecting:" || captured[1] != "error:boom" {
		t.Fatalf("status events incorrects : %v", captured)
	}
}

func TestGomobile_PacketCallbackRegistration(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	var calls atomic.Int32
	SetGomobilePacketCallback(func(packet []byte) {
		calls.Add(1)
	})

	gomobileMu.Lock()
	cb := gomobilePacketCB
	gomobileMu.Unlock()
	if cb == nil {
		t.Fatal("SetGomobilePacketCallback : callback non enregistré")
	}

	cb([]byte{0x45, 0x00})
	if calls.Load() != 1 {
		t.Fatalf("packet callback : attendu 1 invocation, eu %d", calls.Load())
	}

	// Désinscription explicite.
	SetGomobilePacketCallback(nil)
	gomobileMu.Lock()
	cb2 := gomobilePacketCB
	gomobileMu.Unlock()
	if cb2 != nil {
		t.Fatal("SetGomobilePacketCallback(nil) : callback non désenregistré")
	}
}

func TestGomobile_StatusCallbackNilSafe(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	SetGomobileStatusCallback(nil)
	// Ne doit pas paniquer même sans callback.
	emitStatus("disconnected", "")
}

// Story 9.7 H-1 : verify that error messages are redacted before reaching
// the status callback (NFR-AND-9 — no URL/IP/PII in client-visible logs).
func TestGomobile_ErrorRedaction(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected string
	}{
		{"nil error", nil, ""},
		{"pinning failed sentinel", ErrPinningFailed, "pinning_failed"},
		{"pinning failed wrapped", fmt.Errorf("wrap: %w", ErrPinningFailed), "pinning_failed"},
		{"verification failed", ErrVerificationFailed, "verification_failed"},
		{"connection timeout", ErrConnectionTimeout, "connection_timeout"},
		{"not connected", ErrNotConnected, "not_connected"},
		{"session already open", ErrSessionAlreadyOpen, "session_already_open"},
		{"token expired", ErrTokenExpired, "token_expired"},
		{"context canceled", context.Canceled, "canceled"},
		{"context deadline exceeded", context.DeadlineExceeded, "timeout"},
		// CRITIQUE : un message contenant URL + IP doit être TOTALEMENT
		// redacté en classe générique. Si ce test échoue, NFR-AND-9 viole.
		{"PII URL", errors.New("post https://de-001.relay.levoile.example/verify: dial tcp 1.2.3.4:443: connection refused"), "network_error"},
		{"PII relay domain wrapped", fmt.Errorf("tunnel: connect: %w", errors.New("dial 5.6.7.8:443")), "network_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactErrorForStatus(tc.err)
			if got != tc.expected {
				t.Fatalf("redactErrorForStatus(%v) = %q ; attendu %q", tc.err, got, tc.expected)
			}
		})
	}
}

// Story 9.7 H-1 : verify that PII never leaks to the status callback
// even when raw errors propagate from internals (regression test for
// the wiring chain emitStatusErr → emitStatus → callback).
func TestGomobile_StatusCallbackReceivesRedactedErrors(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	var captured string
	SetGomobileStatusCallback(func(state, message, _, _ string) {
		if state == "error" {
			captured = message
		}
	})

	piiErr := errors.New("post https://de-001.relay.levoile.example/verify: dial tcp 1.2.3.4:443: i/o timeout")
	emitStatusErr(piiErr)

	if captured == "" {
		t.Fatal("statusCallback non invoqué")
	}
	// Vérification stricte : aucune trace de l'URL/domaine/IP.
	for _, forbidden := range []string{"https", "://", "de-001", "levoile.example", "1.2.3.4", "443", "i/o", "dial"} {
		if contains(captured, forbidden) {
			t.Fatalf("PII fuitée vers statusCallback : %q contient %q", captured, forbidden)
		}
	}
	if captured != "network_error" {
		t.Fatalf("attendu redaction \"network_error\", eu %q", captured)
	}
}

// Story 9.7 M-4 : ConnectGomobile en singleton — un appel concurrent
// pendant qu'un autre est en cours doit être rejeté immédiatement, sans
// déclencher un second handshake /verify (gaspillage ressources + risque
// de race sur gomobileActive).
//
// On ne peut pas tester avec un vrai handshake (pas de relais), mais on
// peut tester le flag connecting via un Connect bidon qui sleep — pour
// rester simple, on bypasse en simulant manuellement le flag.
func TestGomobile_ConnectingFlagBlocksConcurrent(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	// Simule "un Connect en cours" en posant le flag.
	gomobileMu.Lock()
	gomobileConnecting = true
	gomobileMu.Unlock()
	defer func() {
		gomobileMu.Lock()
		gomobileConnecting = false
		gomobileMu.Unlock()
	}()

	// Un appel concurrent doit immédiatement retourner ErrSessionAlreadyOpen
	// (sans tenter NewClient, qui ferait un net.LookupIP coûteux).
	err := ConnectGomobile("nonexistent.invalid", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if !errors.Is(err, ErrSessionAlreadyOpen) {
		t.Fatalf("ConnectGomobile pendant connecting flag : attendu ErrSessionAlreadyOpen, eu %v", err)
	}
}

// Story 9.7 L-4 : WritePacketGomobile doit gérer proprement le close
// concurrent du channel sans paniquer. On simule en posant manuellement
// un sess avec channel ouvert puis fermé.
func TestGomobile_WritePacketAfterClosedChannel(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	// Simule un sess actif puis explicitement closed.
	out := make(chan []byte, 1)
	gomobileMu.Lock()
	gomobileActive = &gomobileSession{
		outbound: out,
		closed:   true, // marqué closed — WritePacket doit voir ErrNotConnected
	}
	gomobileMu.Unlock()
	defer func() {
		gomobileMu.Lock()
		gomobileActive = nil
		gomobileMu.Unlock()
	}()

	err := WritePacketGomobile([]byte{0x01, 0x02})
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("WritePacketGomobile sur sess.closed=true : attendu ErrNotConnected, eu %v", err)
	}
}

// Story 9.7 L-4 + back-pressure (file pleine → drop silencieux).
func TestGomobile_WritePacketBackPressureDrop(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	// Channel buffer 1 → la 1re write passe, la 2e est dropped.
	out := make(chan []byte, 1)
	gomobileMu.Lock()
	gomobileActive = &gomobileSession{
		outbound: out,
		closed:   false,
	}
	gomobileMu.Unlock()
	defer func() {
		gomobileMu.Lock()
		gomobileActive = nil
		gomobileMu.Unlock()
	}()

	if err := WritePacketGomobile([]byte{0x01}); err != nil {
		t.Fatalf("WritePacketGomobile #1 : attendu nil, eu %v", err)
	}
	// File pleine — drop silencieux, pas d'erreur (architectural choice).
	if err := WritePacketGomobile([]byte{0x02}); err != nil {
		t.Fatalf("WritePacketGomobile #2 (file pleine) : attendu nil drop silencieux, eu %v", err)
	}

	// Vérifier que seul le premier paquet est dans la file.
	select {
	case pkt := <-out:
		if len(pkt) != 1 || pkt[0] != 0x01 {
			t.Fatalf("paquet attendu [0x01], eu %v", pkt)
		}
	default:
		t.Fatal("file vide — le 1er paquet aurait dû être enqueued")
	}
	select {
	case pkt := <-out:
		t.Fatalf("file devait être vide après drop — eu paquet %v", pkt)
	default:
		// OK
	}
}

// Story 9.7 L-6 : RequestSessionTokenGomobile ne doit PAS réutiliser un
// token cache si la pubkey diffère (sécurité — un token issu sous une
// identité Ed25519 ne doit pas matcher un caller qui pinne une autre clé).
//
// Test indirect : on pose un sess actif avec domain X + pubkey K1, on appelle
// avec domain X + pubkey K2 (différent), et on vérifie que le code essaie
// d'établir une nouvelle session (NewClient échouera sur la pubkey invalide
// ou sur la résolution DNS, peu importe le mode d'échec — l'important est
// qu'on ne retourne PAS le token cache).
func TestGomobile_RequestSessionTokenRequiresPubkeyMatch(t *testing.T) {
	ResetGomobileForTest()
	defer ResetGomobileForTest()

	// Nous ne pouvons pas faire un test 100% étanche sans relais réel, mais
	// nous pouvons valider que la guard existe en lisant le code via reflexion :
	// si le sess est en cache pour pubkeyA et qu'on demande pubkeyB, la cache
	// reuse doit être bypassée. On vérifie ici la branch logique en posant
	// un client mock minimal (qui retournerait un token cache si réutilisé).
	//
	// Le test fonctionnel complet (mock relais HTTP/3) est porté par les tests
	// d'intégration internal/tunnel existants — ce test contractuel valide
	// uniquement le guard pubkey.

	// Cas 1 : pubkey match → le client.SessionToken() serait retourné.
	// Cas 2 : pubkey différente → re-issue (NewClient avec invalid pubkey
	//          retourne erreur immediate via lecrypto.ImportPublicKeyBase64).

	const validKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	const otherKey = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA="

	// Instancier un Client réel avec validKey pour avoir une session "cache"
	// (NewClient résout l'IP — on utilise un host:port déjà-IP pour éviter le DNS).
	c, err := NewClient("127.0.0.1:8443", validKey)
	if err != nil {
		t.Fatalf("NewClient mock : %v", err)
	}
	// Injecter un sessionToken "fake" pour simuler un token cache.
	c.mu.Lock()
	c.sessionToken = "FAKE_CACHED_TOKEN"
	c.sessionTokenIssued = 1234
	c.sessionTokenTTL = 14400
	c.mu.Unlock()

	gomobileMu.Lock()
	gomobileActive = &gomobileSession{
		client:         c,
		relayDomain:    "127.0.0.1:8443",
		relayPubKeyB64: validKey,
	}
	gomobileMu.Unlock()
	defer func() {
		gomobileMu.Lock()
		gomobileActive = nil
		gomobileMu.Unlock()
		_ = c.Disconnect()
	}()

	// Cas 1 : appel avec MÊMES domain + pubkey → cache hit, retour token.
	tok, err := RequestSessionTokenGomobile("127.0.0.1:8443", validKey)
	if err != nil {
		t.Fatalf("cache hit : attendu pas d'erreur, eu %v", err)
	}
	if tok != "FAKE_CACHED_TOKEN" {
		t.Fatalf("cache hit : attendu FAKE_CACHED_TOKEN, eu %q", tok)
	}

	// Cas 2 : MÊME domain mais pubkey DIFFÉRENTE → cache bypass + tentative
	// de nouveau NewClient + Connect (qui va échouer car pas de relais sur
	// 127.0.0.1:8443). Important : on ne doit PAS recevoir le token cache.
	tok2, err2 := RequestSessionTokenGomobile("127.0.0.1:8443", otherKey)
	if err2 == nil {
		t.Fatalf("pubkey différente : attendu erreur (re-issue qui échoue), eu tok=%q sans erreur", tok2)
	}
	if tok2 == "FAKE_CACHED_TOKEN" {
		t.Fatal("CRITICAL : token cache fuité avec pubkey différente — viole guard L-6")
	}
}

// Helper : équivalent strings.Contains sans import de "strings" (on garde
// les imports facade-test minimaux).
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
