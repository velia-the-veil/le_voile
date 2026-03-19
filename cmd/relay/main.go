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

const buildTag = "2026-03-19-v3-bandwidth-limiter"

func main() {
	fmt.Fprintf(os.Stderr, "relay: starting build=%s\n", buildTag)
	addr := flag.String("addr", "0.0.0.0:443", "listen address for HTTP/3 relay")
	certFile := flag.String("cert", "cert.pem", "path to TLS certificate PEM file")
	keyFile := flag.String("key", "key.pem", "path to TLS private key PEM file")
	upstream := flag.String("upstream", "https://1.1.1.1/dns-query", "primary DoH resolver URL")
	fallback := flag.String("fallback", "https://9.9.9.9/dns-query", "fallback DoH resolver URL (empty to disable)")
	signingKeyPath := flag.String("signing-key", "", "path to Ed25519 private key file (base64); enables /verify endpoint")
	registryFile := flag.String("registry-file", "", "path to relay-registry.json (served at /.well-known/relay-registry.json)")
	cfInsecure := flag.Bool("cf-insecure", false, "trust direct source IP instead of CF-Connecting-IP (dev mode only)")
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
		// In production behind Cloudflare, use default (insecure=false).
		// Pass -cf-insecure for dev/direct mode (no Cloudflare proxy).
		cfv := relay.NewCloudflareIPValidator(*cfInsecure, nil)
		srv.CFIPValidator = cfv

		// Enable per-IP connection limiter and bandwidth limiter.
		ipLimiter := relay.NewIPLimiter(relay.IPLimiterMaxPerIP)
		bwLimiter := relay.NewBandwidthLimiter(relay.DailyQuotaBytes)
		go ipLimiter.StartCleanup(ctx)
		go bwLimiter.StartCleanup(ctx)

		// Enable HTTP CONNECT proxy handler.
		srv.ConnectHandler = relay.NewConnectHandler(
			key.Public().(ed25519.PublicKey), cfv, ipLimiter, bwLimiter,
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
