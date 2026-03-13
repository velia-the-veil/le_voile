package relay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

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
type SessionTokenPayload struct {
	IPHash string `json:"ip_hash"`
	Issued int64  `json:"issued"`
	TTL    int64  `json:"ttl"`
}

// VerifyHandler signs a client-provided nonce with the relay's Ed25519 key
// and optionally issues a session token for proxy CONNECT authentication.
type VerifyHandler struct {
	signingKey  ed25519.PrivateKey
	cfValidator *CloudflareIPValidator // may be nil if proxy not enabled
}

// NewVerifyHandler creates a VerifyHandler with the given signing key.
func NewVerifyHandler(signingKey ed25519.PrivateKey) *VerifyHandler {
	return &VerifyHandler{signingKey: signingKey}
}

// SetCFValidator enables session token issuance using the given validator.
func (h *VerifyHandler) SetCFValidator(v *CloudflareIPValidator) {
	h.cfValidator = v
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(req.Nonce)
	if err != nil || len(nonceBytes) != nonceSize {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	sig, err := lecrypto.Sign(h.signingKey, nonceBytes)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	resp := VerifyResponse{
		Signature: base64.StdEncoding.EncodeToString(sig),
	}

	// Issue session token if CF validator is configured.
	if h.cfValidator != nil {
		clientIP, ipErr := h.cfValidator.ExtractClientIP(r)
		if ipErr == nil {
			token, tokenErr := CreateSessionToken(h.signingKey, clientIP)
			if tokenErr == nil {
				resp.SessionToken = token
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// CreateSessionToken creates a signed session token for the given client IP.
func CreateSessionToken(signingKey ed25519.PrivateKey, clientIP string) (string, error) {
	ipHash := fmt.Sprintf("%x", sha256.Sum256([]byte(clientIP)))
	payload := SessionTokenPayload{
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
