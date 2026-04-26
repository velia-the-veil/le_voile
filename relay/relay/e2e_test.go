//go:build e2e

package relay

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"

	"github.com/quic-go/quic-go/http3"
)

// TestE2E_DoHRoundTrip performs an end-to-end test: starts a real HTTP/3 relay
// server, sends a DNS query via HTTP/3 client, and verifies the response.
// This test requires network access to forward to 1.1.1.1.
func TestE2E_DoHRoundTrip(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	// Generate self-signed cert
	_, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	certPEM, keyPEM, err := lecrypto.GenerateSelfSignedTLSCert(priv)
	if err != nil {
		t.Fatalf("generate tls cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	// Start server with real upstream (Cloudflare DNS)
	addr := freeUDPAddr(t)
	srv := NewServer(addr, certPath, keyPath)
	srv.Handler = NewDoHHandler([]string{"https://1.1.1.1/dns-query"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	waitForServer(t, addr)

	// HTTP/3 client
	client := &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 10 * time.Second,
	}
	defer client.CloseIdleConnections()

	// Build DNS query for example.com type A
	dnsQuery := buildDNSQuery("example.com", 1)

	resp, err := client.Post(
		"https://"+addr+"/dns-query",
		dnsMessageContentType,
		bytes.NewReader(dnsQuery),
	)
	if err != nil {
		t.Fatalf("DoH request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != dnsMessageContentType {
		t.Errorf("expected content-type %q, got %q", dnsMessageContentType, ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	// Verify it's a DNS response (QR bit set)
	if len(body) < 12 {
		t.Fatalf("response too short for DNS: %d bytes", len(body))
	}

	if body[2]&0x80 == 0 {
		t.Error("expected QR bit set in DNS response")
	}

	// Verify ANCOUNT > 0 (at least one answer for example.com)
	ancount := int(body[6])<<8 | int(body[7])
	if ancount == 0 {
		t.Error("expected at least one answer record for example.com")
	}

	t.Logf("DoH e2e round-trip successful: %d answer records, %d bytes response", ancount, len(body))
}
