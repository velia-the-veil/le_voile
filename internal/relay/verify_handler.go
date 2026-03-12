package relay

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

const nonceSize = 32

// VerifyRequest is the JSON body for a verify challenge.
type VerifyRequest struct {
	Nonce string `json:"nonce"`
}

// VerifyResponse is the JSON reply containing the Ed25519 signature.
type VerifyResponse struct {
	Signature string `json:"signature"`
}

// VerifyHandler signs a client-provided nonce with the relay's Ed25519 key.
type VerifyHandler struct {
	signingKey ed25519.PrivateKey
}

// NewVerifyHandler creates a VerifyHandler with the given signing key.
func NewVerifyHandler(signingKey ed25519.PrivateKey) *VerifyHandler {
	return &VerifyHandler{signingKey: signingKey}
}

// ServeHTTP handles POST requests with a JSON nonce, returning an Ed25519 signature.
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}
