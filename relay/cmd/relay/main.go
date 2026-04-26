package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/velia-the-veil/le_voile/relay/relay"
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
	publicIP := flag.String("public-ip", "", "relay public IP for NAT source rewriting (auto-detected from eth0 if empty)")
	tunnelEcho := flag.Bool("tunnel-echo", false, "enable /tunnel with echo forwarder (dev/test only — never use in production)")
	dnsBlocklistURL := flag.String("dns-blocklist-url", "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts", "URL for DNS blocklist (StevenBlack format)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var natShutdown func(context.Context) error

	upstreams := []string{*upstream}
	if *fallback != "" && *fallback != *upstream {
		upstreams = append(upstreams, *fallback)
	}
	dohHandler := relay.NewDoHHandler(upstreams, nil)
	dohHandler.Start(ctx)

	srv := relay.NewServer(*addr, *certFile, *keyFile)
	srv.Handler = dohHandler
	srv.RegistryFile = *registryFile

	if *signingKeyPath != "" {
		key, err := loadSigningKey(*signingKeyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "relay: load signing key: %v\n", err)
			os.Exit(1)
		}
		srv.SigningKey = key

		// Enable per-IP connection limiter and bandwidth limiter.
		// Cleanup goroutines are started by srv.ListenAndServe.
		ipLimiter := relay.NewIPLimiter(relay.IPLimiterMaxPerIP)
		bwLimiter := relay.NewBandwidthLimiter(relay.DailyQuotaBytes)
		srv.IPLimiter = ipLimiter
		srv.BWLimiter = bwLimiter

		// Enable HTTP CONNECT proxy handler.
		srv.ConnectHandler = relay.NewConnectHandler(
			key.Public().(ed25519.PublicKey), ipLimiter, bwLimiter,
			func(format string, args ...any) {
				fmt.Fprintf(os.Stderr, "relay: connect: "+format+"\n", args...)
			},
		)

		// DNS blocklist + resolver for /tunnel DNS interception (story 3.5).
		blocklist := relay.NewBlocklist(*dnsBlocklistURL, nil, 0)
		go blocklist.Start(ctx)
		dnsResolver := relay.NewDNSResolver(upstreams, blocklist, nil)

		// NAT table for /tunnel packet forwarding (story 3.4).
		relayIP := resolveRelayPublicIP(*publicIP)
		natTable := relay.NewNAT(relayIP, relay.WithContext(ctx), relay.WithDNSResolver(dnsResolver))
		natShutdown = natTable.Shutdown
		srv.NATStatsFunc = natTable.Stats

		if *tunnelEcho {
			// Dev/test: echo forwarder ignores NAT.
			th := relay.NewTunnelHandler(
				key.Public().(ed25519.PublicKey), ipLimiter,
				&echoForwarder{},
				func(format string, args ...any) {
					fmt.Fprintf(os.Stderr, "relay: tunnel: "+format+"\n", args...)
				},
			)
			th.SetBWLimiter(bwLimiter)
			srv.TunnelHandler = th
			fmt.Fprintf(os.Stderr, "relay: WARNING: /tunnel enabled with echo forwarder (dev mode)\n")
		} else {
			th := relay.NewTunnelHandler(
				key.Public().(ed25519.PublicKey), ipLimiter,
				natTable,
				func(format string, args ...any) {
					fmt.Fprintf(os.Stderr, "relay: tunnel: "+format+"\n", args...)
				},
			)
			th.SetBWLimiter(bwLimiter)
			srv.TunnelHandler = th
		}
	}

	if err := srv.ListenAndServe(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
	}
	if natShutdown != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		natShutdown(shutdownCtx)
	}
}

// echoForwarder is a dev-only PacketForwarder that echoes packets back.
// Used exclusively with -tunnel-echo for smoke testing.
// NOTE: intentional duplication with testEchoForwarder in tunnel_handler_test.go —
// test types can't be imported from main, and extracting to a shared package
// for a dev-only stub adds unnecessary complexity.
type echoForwarder struct {
	mu       sync.Mutex
	sessions map[string]chan<- []byte
}

func (e *echoForwarder) OpenSession(_ context.Context, session relay.TunnelSession) (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	e.mu.Lock()
	if e.sessions == nil {
		e.sessions = make(map[string]chan<- []byte)
	}
	e.sessions[session.ClientIPHash] = ch
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.sessions, session.ClientIPHash)
		e.mu.Unlock()
	}
}

func (e *echoForwarder) Forward(_ context.Context, session relay.TunnelSession, pkt []byte) error {
	e.mu.Lock()
	ch, ok := e.sessions[session.ClientIPHash]
	e.mu.Unlock()
	if !ok {
		return nil
	}
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	select {
	case ch <- cp:
	default:
	}
	return nil
}

// resolveRelayPublicIP returns the relay's public IP from the flag or auto-detection.
func resolveRelayPublicIP(flagValue string) net.IP {
	if flagValue != "" {
		ip := net.ParseIP(flagValue)
		if ip != nil {
			return ip
		}
		fmt.Fprintf(os.Stderr, "relay: invalid -public-ip %q, falling back to auto-detect\n", flagValue)
	}
	// Iterate every UP interface and pick the first IPv4 that's neither
	// loopback, link-local nor a private/CG-NAT range. The previous version
	// hardcoded {eth0, ens3, ens5, enp0s3} and failed on Hetzner-style
	// names (ens6, enp1s0, …) → fell back to 0.0.0.0 → the kernel still
	// auto-fills the source IP on send, but the WARNING in the logs is
	// alarming and tools that read public-ip via the management endpoint
	// got 0.0.0.0.
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}
			if ip4.IsLoopback() || ip4.IsLinkLocalUnicast() ||
				ip4.IsPrivate() || ip4.IsUnspecified() {
				continue
			}
			fmt.Fprintf(os.Stderr, "relay: auto-detected public IP %s from %s\n", ip4, iface.Name)
			return ip4
		}
	}
	fmt.Fprintf(os.Stderr, "relay: WARNING: could not detect public IP, using 0.0.0.0\n")
	return net.IPv4zero
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
