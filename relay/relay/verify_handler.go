package relay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

// maxVerifyBody is the maximum request body size for /verify (1 KB).
const maxVerifyBody = 1024

const nonceSize = 32

// SessionTokenTTL is the default session token validity duration (4 hours).
const SessionTokenTTL = 14400

// VerifyRequest is the JSON body for a verify challenge.
type VerifyRequest struct {
	Nonce string `json:"nonce"`
}

// VerifyResponse is the JSON reply containing the Ed25519 signature
// and an optional session token for authenticated proxy access.
type VerifyResponse struct {
	Signature    string `json:"signature"`
	SessionToken string `json:"session_token,omitempty"`
}

// SessionTokenPayload is the JSON structure inside a session token.
//
// Nonce (fix C4 audit sécurité, 2026-04) is a fresh 16-byte random value
// minted by the relay at every /verify call. It does not prevent replay
// by itself — the whole token is still valid for TTL — but it guarantees
// that two tokens issued for the same client IP are distinct artefacts,
// giving us:
//  1. A stable primary key for future server-side replay caches (LRU of
//     seen nonces until Issued+TTL elapses).
//  2. Tamper detection: an attacker who captures a token and tries to
//     splice a new Issued/TTL cannot do so without invalidating the
//     Ed25519 signature, because the nonce is covered by the signature
//     and its entropy prevents cheaply recomputing the payload.
type SessionTokenPayload struct {
	Nonce  string `json:"nonce"`
	IPHash string `json:"ip_hash"`
	Issued int64  `json:"issued"`
	TTL    int64  `json:"ttl"`
}

// VerifyHandler signs a client-provided nonce with the relay's Ed25519 key
// and issues a session token bound to the client's remote address (used by
// /connect and /tunnel for IP-hash verification).
type VerifyHandler struct {
	signingKey ed25519.PrivateKey
}

// NewVerifyHandler creates a VerifyHandler with the given signing key.
func NewVerifyHandler(signingKey ed25519.PrivateKey) *VerifyHandler {
	return &VerifyHandler{signingKey: signingKey}
}

// ServeHTTP handles POST requests with a JSON nonce, returning an Ed25519 signature
// and a session token (if CF validator is configured).
func (h *VerifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	var req VerifyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxVerifyBody)).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(req.Nonce)
	if err != nil || len(nonceBytes) != nonceSize {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Resolve client IP from the request's remote address (direct origin, no
	// CDN fronting). If the remote address is unparseable we still sign the
	// nonce but omit the session token — the client will retry and the next
	// request will either succeed or fail at a lower layer.
	remoteIP := clientIP(r)

	sig, err := lecrypto.Sign(h.signingKey, nonceBytes)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	resp := VerifyResponse{
		Signature: base64.StdEncoding.EncodeToString(sig),
	}

	if remoteIP != "" {
		token, tokenErr := CreateSessionToken(h.signingKey, remoteIP)
		if tokenErr == nil {
			resp.SessionToken = token
		}
	}

	w.Header().Set("Content-Type", "application/json")
	// #nosec G117 -- SessionToken DOIT être renvoyé au client : c'est la
	// finalité même de l'endpoint /verify (issue d'un token signé Ed25519
	// après challenge/response). Le canal est TLS 1.3 + cert pinning Ed25519
	// (cf. internal/tunnel/client.go). Token = data à délivrer, pas leak.
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// CreateSessionToken creates a signed session token for the given client IP.
func CreateSessionToken(signingKey ed25519.PrivateKey, clientIP string) (string, error) {
	ipHash := fmt.Sprintf("%x", sha256.Sum256([]byte(clientIP)))
	nonce, err := tokenNonce()
	if err != nil {
		return "", fmt.Errorf("session token: nonce: %w", err)
	}
	payload := SessionTokenPayload{
		Nonce:  nonce,
		IPHash: ipHash,
		Issued: time.Now().Unix(),
		TTL:    SessionTokenTTL,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("session token: marshal: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sig, err := lecrypto.Sign(signingKey, payloadJSON)
	if err != nil {
		return "", fmt.Errorf("session token: sign: %w", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return payloadB64 + "." + sigB64, nil
}

// tokenNonce returns a 16-byte random value base64-encoded without padding,
// suitable for use as SessionTokenPayload.Nonce.
func tokenNonce() (string, error) {
	return randomHex(16)
}

// VerifySessionToken verifies a session token signature and returns the payload.
func VerifySessionToken(pubKey ed25519.PublicKey, token string) (*SessionTokenPayload, error) {
	parts := splitToken(token)
	if parts == nil {
		return nil, fmt.Errorf("session token: invalid format")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("session token: decode payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("session token: decode sig: %w", err)
	}
	if !lecrypto.Verify(pubKey, payloadJSON, sig) {
		return nil, fmt.Errorf("session token: invalid signature")
	}
	var payload SessionTokenPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("session token: unmarshal: %w", err)
	}
	return &payload, nil
}

func splitToken(token string) []string {
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			if i > 0 && i < len(token)-1 {
				return []string{token[:i], token[i+1:]}
			}
			return nil
		}
	}
	return nil
}
