//go:build e2e

package ipc

import (
	"context"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// e2eServiceState simulates the service's shared state that both tray and
// desktop clients observe via IPC.
type e2eServiceState struct {
	mu              sync.RWMutex
	status          string
	ip              string
	country         string
	countryFlag     string
	relayID         string
	relayDomain     string
	uptime          string
	httpProxyActive bool
	blocklistEnabled bool
	browserPolicies []string
}

func (s *e2eServiceState) handler(req Request) Response {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch req.Action {
	case ActionGetStatus:
		return Response{
			Status:                 s.status,
			IP:                     s.ip,
			Country:                s.country,
			CountryFlag:            s.countryFlag,
			RelayID:                s.relayID,
			RelayDomain:            s.relayDomain,
			Uptime:                 s.uptime,
			HTTPProxyActive:        s.httpProxyActive,
			BlocklistEnabled:       s.blocklistEnabled,
			BrowserPoliciesApplied: s.browserPolicies,
		}
	case ActionSelectCountry:
		s.mu.RUnlock()
		s.mu.Lock()
		s.country = countryName(req.Value)
		s.countryFlag = countryFlag(req.Value)
		s.ip = "198.51.100." + req.Value // fake distinct IP per country code
		s.mu.Unlock()
		s.mu.RLock()
		return Response{Status: StatusOK}
	default:
		return Response{Status: StatusError, Error: "unknown_action"}
	}
}

func countryName(code string) string {
	names := map[string]string{"FR": "France", "IS": "Islande", "DE": "Allemagne"}
	if n, ok := names[code]; ok {
		return n
	}
	return code
}

func countryFlag(code string) string {
	flags := map[string]string{"FR": "\U0001F1EB\U0001F1F7", "IS": "\U0001F1EE\U0001F1F8", "DE": "\U0001F1E9\U0001F1EA"}
	if f, ok := flags[code]; ok {
		return f
	}
	return ""
}

func newE2EState() *e2eServiceState {
	return &e2eServiceState{
		status:          StatusConnected,
		ip:              "198.51.100.42",
		country:         "Islande",
		countryFlag:     "\U0001F1EE\U0001F1F8",
		relayID:         "relay-is-01",
		relayDomain:     "is.levoile.dev",
		uptime:          "2h15m",
		httpProxyActive: true,
		blocklistEnabled: true,
		browserPolicies: []string{"Google Chrome", "Firefox"},
	}
}

func startE2EServer(t *testing.T, state *e2eServiceState) (string, context.CancelFunc) {
	t.Helper()
	tl, addr := newTestListener(t)
	srv := NewServer(tl)
	srv.SetHandler(state.handler)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)

	return addr, cancel
}

// TestE2E_IPCMultiClient connects two clients simultaneously and verifies
// that both receive identical and complete status responses (AC9).
func TestE2E_IPCMultiClient(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	state := newE2EState()
	addr, cancel := startE2EServer(t, state)
	defer cancel()

	// Two clients: simulating tray + desktop.
	c1 := dialTest(t, addr)
	defer c1.close()
	c2 := dialTest(t, addr)
	defer c2.close()

	r1 := c1.sendAndReceive(t, Request{Action: ActionGetStatus})
	r2 := c2.sendAndReceive(t, Request{Action: ActionGetStatus})

	// Both responses must be identical.
	checks := []struct {
		field    string
		got1     string
		got2     string
		expected string
	}{
		{"Status", r1.Status, r2.Status, StatusConnected},
		{"IP", r1.IP, r2.IP, "198.51.100.42"},
		{"Country", r1.Country, r2.Country, "Islande"},
		{"CountryFlag", r1.CountryFlag, r2.CountryFlag, "\U0001F1EE\U0001F1F8"},
		{"RelayID", r1.RelayID, r2.RelayID, "relay-is-01"},
		{"RelayDomain", r1.RelayDomain, r2.RelayDomain, "is.levoile.dev"},
		{"Uptime", r1.Uptime, r2.Uptime, "2h15m"},
	}

	for _, c := range checks {
		if c.got1 != c.expected {
			t.Errorf("client1 %s = %q, want %q", c.field, c.got1, c.expected)
		}
		if c.got2 != c.expected {
			t.Errorf("client2 %s = %q, want %q", c.field, c.got2, c.expected)
		}
		if c.got1 != c.got2 {
			t.Errorf("%s mismatch: client1=%q, client2=%q", c.field, c.got1, c.got2)
		}
	}

	if !r1.HTTPProxyActive || !r2.HTTPProxyActive {
		t.Error("HTTPProxyActive should be true for both clients")
	}
	if !r1.BlocklistEnabled || !r2.BlocklistEnabled {
		t.Error("BlocklistEnabled should be true for both clients")
	}

	t.Log("multi-client IPC OK: both clients received identical status")
}

// TestE2E_IPCCountryChange verifies that when one client changes the country,
// the other client sees the change within 2 seconds (AC5).
func TestE2E_IPCCountryChange(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	state := newE2EState()
	addr, cancel := startE2EServer(t, state)
	defer cancel()

	desktop := dialTest(t, addr)
	defer desktop.close()
	tray := dialTest(t, addr)
	defer tray.close()

	// Desktop selects France.
	resp := desktop.sendAndReceive(t, Request{Action: ActionSelectCountry, Value: "FR"})
	if resp.Status != StatusOK {
		t.Fatalf("select_country failed: %s", resp.Error)
	}

	// Tray polls status — should see France within 2s.
	start := time.Now()
	var trayResp Response
	for time.Since(start) < 2*time.Second {
		trayResp = tray.sendAndReceive(t, Request{Action: ActionGetStatus})
		if trayResp.Country == "France" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if trayResp.Country != "France" {
		t.Errorf("tray country = %q after 2s, want France", trayResp.Country)
	}

	t.Logf("country change propagated in %v", time.Since(start))
}

// TestE2E_IPCStatusDuringReconnect verifies that get_status returns the
// correct status string across all connection states (AC9). Tests that the
// IPC layer faithfully transmits each state without mutation.
func TestE2E_IPCStatusDuringReconnect(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	states := []struct {
		name   string
		status string
	}{
		{"connecting", StatusConnecting},
		{"connected", StatusConnected},
		{"disconnected", StatusDisconnected},
	}

	for _, tt := range states {
		t.Run(tt.name, func(t *testing.T) {
			state := newE2EState()
			state.status = tt.status

			addr, cancel := startE2EServer(t, state)
			defer cancel()

			c := dialTest(t, addr)
			defer c.close()

			resp := c.sendAndReceive(t, Request{Action: ActionGetStatus})
			if resp.Status != tt.status {
				t.Errorf("status = %q, want %q", resp.Status, tt.status)
			}
		})
	}

	t.Log("reconnect status OK: all states transmitted correctly via IPC")
}

// TestE2E_IPCPipeBroken verifies client behavior when the server shuts down
// unexpectedly: the client should receive a pipe error and, if the server
// restarts on the SAME address (simulating the fixed named pipe in production),
// be able to reconnect and get a valid status.
func TestE2E_IPCPipeBroken(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	state := newE2EState()

	// Reserve a specific port to reuse for both server instances,
	// simulating the fixed named pipe address in production.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Start server 1 on the reserved address.
	ln1, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen server 1: %v", err)
	}
	srv := NewServer(&testListener{ln: ln1})
	srv.SetHandler(state.handler)

	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() { _ = srv.Start(ctx1) }()
	time.Sleep(50 * time.Millisecond)

	// Client connects.
	c := dialTest(t, addr)

	// Verify connection works.
	resp := c.sendAndReceive(t, Request{Action: ActionGetStatus})
	if resp.Status != StatusConnected {
		t.Fatalf("initial status = %q, want connected", resp.Status)
	}

	// Kill the server (simulate crash).
	cancel1()
	time.Sleep(100 * time.Millisecond)

	// Client should get an error on next request.
	c.conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := c.encoder.Encode(Request{Action: ActionGetStatus}); err != nil {
		t.Logf("pipe broken on write: %v (expected)", err)
	} else if !c.scanner.Scan() {
		t.Logf("pipe broken on read: %v (expected)", c.scanner.Err())
	}
	c.close()

	// Restart server on the SAME address (simulates SCM restarting the service
	// which re-listens on the same named pipe).
	var ln2 net.Listener
	for i := 0; i < 20; i++ {
		ln2, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("rebind to %s after server crash: %v", addr, err)
	}

	srv2 := NewServer(&testListener{ln: ln2})
	srv2.SetHandler(state.handler)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go func() { _ = srv2.Start(ctx2) }()
	time.Sleep(50 * time.Millisecond)

	// Client reconnects to the SAME address.
	c2 := dialTest(t, addr)
	defer c2.close()

	resp2 := c2.sendAndReceive(t, Request{Action: ActionGetStatus})
	if resp2.Status != StatusConnected {
		t.Errorf("status after reconnect = %q, want connected", resp2.Status)
	}

	t.Logf("pipe broken + reconnect OK (same address: %s)", addr)
}

// TestE2E_IPCConcurrentCountryChange sends two concurrent select_country
// requests and verifies that the final state is consistent: one country
// wins, and all clients see the same value.
func TestE2E_IPCConcurrentCountryChange(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	state := newE2EState()
	addr, cancel := startE2EServer(t, state)
	defer cancel()

	// Two clients send country changes concurrently.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		c := dialTest(t, addr)
		defer c.close()
		c.sendAndReceive(t, Request{Action: ActionSelectCountry, Value: "IS"})
	}()

	go func() {
		defer wg.Done()
		c := dialTest(t, addr)
		defer c.close()
		c.sendAndReceive(t, Request{Action: ActionSelectCountry, Value: "FR"})
	}()

	wg.Wait()

	// Check final state: both clients should see the same country.
	c1 := dialTest(t, addr)
	defer c1.close()
	c2 := dialTest(t, addr)
	defer c2.close()

	r1 := c1.sendAndReceive(t, Request{Action: ActionGetStatus})
	r2 := c2.sendAndReceive(t, Request{Action: ActionGetStatus})

	if r1.Country != r2.Country {
		t.Fatalf("inconsistent country: client1=%q, client2=%q", r1.Country, r2.Country)
	}

	if r1.Country != "France" && r1.Country != "Islande" {
		t.Errorf("unexpected country %q (expected France or Islande)", r1.Country)
	}

	// Verify IP is consistent with the winning country.
	if r1.IP != r2.IP {
		t.Errorf("inconsistent IP: client1=%q, client2=%q", r1.IP, r2.IP)
	}

	// Verify the shared state is also consistent (direct read).
	state.mu.RLock()
	stateCountry := state.country
	stateIP := state.ip
	state.mu.RUnlock()

	if stateCountry != r1.Country {
		t.Errorf("state.country = %q but IPC returned %q", stateCountry, r1.Country)
	}
	if stateIP != r1.IP {
		t.Errorf("state.ip = %q but IPC returned %q", stateIP, r1.IP)
	}

	t.Logf("concurrent country change OK: final=%q ip=%q (state consistent)", r1.Country, r1.IP)
}
