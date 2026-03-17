package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/velia-the-veil/le_voile/internal/relay"
)

const buildTag = "2026-03-14-v2-no-iplimiter"

func main() {
	fmt.Fprintf(os.Stderr, "relay: starting build=%s\n", buildTag)
	addr := flag.String("addr", "0.0.0.0:443", "listen address for HTTP/3 relay")
	certFile := flag.String("cert", "cert.pem", "path to TLS certificate PEM file")
	keyFile := flag.String("key", "key.pem", "path to TLS private key PEM file")
	upstream := flag.String("upstream", "https://1.1.1.1/dns-query", "primary DoH resolver URL")
	fallback := flag.String("fallback", "https://9.9.9.9/dns-query", "fallback DoH resolver URL (empty to disable)")
	signingKeyPath := flag.String("signing-key", "", "path to Ed25519 private key file (base64); enables /verify endpoint")
	registryFile := flag.String("registry-file", "", "path to relay-registry.json (served at /.well-known/relay-registry.json)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	upstreams := []string{*upstream}
	if *fallback != "" && *fallback != *upstream {
		upstreams = append(upstreams, *fallback)
	}
	dohHandler := relay.NewDoHHandler(upstreams, nil)
	dohHandler.Start(ctx)

	srv := relay.NewServer(*addr, *certFile, *keyFile)
	srv.Handler = dohHandler
	srv.STUNHandler = relay.NewSTUNHandler()
	srv.RegistryFile = *registryFile

	if *signingKeyPath != "" {
		key, err := loadSigningKey(*signingKeyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "relay: load signing key: %v\n", err)
			os.Exit(1)
		}
		srv.SigningKey = key

		// Enable Cloudflare IP validation for session tokens.
		// In dev/direct mode (no Cloudflare proxy), use insecure=true
		// so the relay trusts the direct source IP.
		cfv := relay.NewCloudflareIPValidator(true, nil)
		srv.CFIPValidator = cfv

		// Enable HTTP CONNECT proxy handler.
		// IPLimiter disabled: single-user relay, no abuse risk.
		// Pass nil so ConnectHandler skips per-IP limiting entirely.
		srv.ConnectHandler = relay.NewConnectHandler(
			key.Public().(ed25519.PublicKey), cfv, nil,
			func(format string, args ...any) {
				fmt.Fprintf(os.Stderr, "relay: connect: "+format+"\n", args...)
			},
		)
	}

	if err := srv.ListenAndServe(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
		os.Exit(1)
	}
}

func loadSigningKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	if len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid key size: got %d, want %d", len(decoded), ed25519.PrivateKeySize)
	}

	return ed25519.PrivateKey(decoded), nil
}
