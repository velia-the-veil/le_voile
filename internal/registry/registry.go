// Package registry handles dynamic relay discovery via a signed JSON registry.
package registry

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CurrentVersion is the only supported registry format version.
const CurrentVersion = 1

// EndpointPath is the well-known URL path for the relay registry.
const EndpointPath = "/.well-known/relay-registry.json"

// signaturePrefix prevents signature reuse across different Ed25519 contexts.
const signaturePrefix = "relay-key-v1:"

// Sentinel errors.
var (
	ErrInvalidSignature = errors.New("registry: invalid relay signature")
	ErrNoValidRelays    = errors.New("registry: no valid relays after verification")
	ErrInvalidMasterKey = errors.New("registry: invalid master public key")
	ErrRegistryEmpty    = errors.New("registry: no relays in registry")
)

// RelayEntry represents a single relay in the registry.
type RelayEntry struct {
	ID        string    `json:"id"`
	Domain    string    `json:"domain"`
	PublicKey string    `json:"public_key"`
	Signature string    `json:"signature"`
	Added     time.Time `json:"added"`
}

// Registry represents the full relay registry document.
type Registry struct {
	Version         int          `json:"version"`
	MasterPublicKey string       `json:"master_public_key"`
	Relays          []RelayEntry `json:"relays"`
	Updated         time.Time    `json:"updated"`
}

// Parse decodes a JSON registry document and validates basic structure.
func Parse(data []byte) (*Registry, error) {
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("registry: parse: %w", err)
	}
	if reg.Version != CurrentVersion {
		return nil, fmt.Errorf("registry: parse: unsupported version %d", reg.Version)
	}
	if len(reg.Relays) == 0 {
		return nil, ErrRegistryEmpty
	}
	for i, r := range reg.Relays {
		if r.ID == "" || r.Domain == "" || r.PublicKey == "" || r.Signature == "" {
			return nil, fmt.Errorf("registry: parse: relay %d has empty required field", i)
		}
	}
	return &reg, nil
}

// VerifyRelaySignature verifies that a relay's public key was signed by the master key.
// The signed message is "relay-key-v1:" + raw relay public key bytes.
func VerifyRelaySignature(masterPubKey ed25519.PublicKey, entry RelayEntry) error {
	relayPubKeyBytes, err := base64.StdEncoding.DecodeString(entry.PublicKey)
	if err != nil {
		return fmt.Errorf("registry: decode relay public key: %w", err)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(entry.Signature)
	if err != nil {
		return fmt.Errorf("registry: decode signature: %w", err)
	}

	msg := append([]byte(signaturePrefix), relayPubKeyBytes...)
	if !ed25519.Verify(masterPubKey, msg, sigBytes) {
		return ErrInvalidSignature
	}
	return nil
}

// VerifyAll verifies all relays against the master public key and returns only
// those that pass verification. Returns ErrNoValidRelays if none pass.
func (r *Registry) VerifyAll() ([]RelayEntry, error) {
	masterKeyBytes, err := base64.StdEncoding.DecodeString(r.MasterPublicKey)
	if err != nil {
		return nil, fmt.Errorf("registry: %w: %v", ErrInvalidMasterKey, err)
	}
	if len(masterKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("registry: %w: invalid key length %d", ErrInvalidMasterKey, len(masterKeyBytes))
	}
	masterPubKey := ed25519.PublicKey(masterKeyBytes)

	var verified []RelayEntry
	for _, entry := range r.Relays {
		if err := VerifyRelaySignature(masterPubKey, entry); err == nil {
			verified = append(verified, entry)
		}
	}
	if len(verified) == 0 {
		return nil, ErrNoValidRelays
	}
	return verified, nil
}
