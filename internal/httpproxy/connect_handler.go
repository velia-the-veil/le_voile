package httpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
)

// connectHandler handles incoming HTTP proxy requests from browsers.
// It supports both CONNECT (HTTPS tunneling) and plain HTTP forwarding.
type connectHandler struct {
	tunnelClient  TunnelClient
	volumeTracker *VolumeTracker
	wg            *sync.WaitGroup
}

// connectBody is the JSON body sent to the relay /connect endpoint.
type connectBody struct {
	Target string `json:"target"`
}

func (h *connectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		h.handleConnect(w, r)
	} else {
		h.handleHTTP(w, r)
	}
}

// handleConnect tunnels HTTPS connections via the relay's /connect endpoint.
func (h *connectHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Parse target host:port from the CONNECT request.
	target := r.Host
	if target == "" {
		target = r.URL.Host
	}
	if target == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Ensure host:port format.
	_, _, err := net.SplitHostPort(target)
	if err != nil {
		// CONNECT usually comes as host:port, but add default 443 if missing.
		target = target + ":443"
	}

	// Always-bypass list (game-launcher CDNs): try direct from the first
	// byte. If the direct dial fails, fall through to the relay path —
	// the always-bypass list is a hint, not a hard requirement.
	if IsAlwaysBypassed(target) {
		if h.tryDirectBypass(w, r, target) {
			return
		}
	}

	// Volume bypass: if the domain is flagged, try a direct connection.
	if h.volumeTracker != nil && h.volumeTracker.IsBypassed(target) {
		if h.tryDirectBypass(w, r, target) {
			return // bypass succeeded
		}
		// Direct dial failed — fall through to relay path below.
	}

	// Ensure session token is fresh.
	if err := h.tunnelClient.EnsureSessionToken(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("Proxy Authentication Failed: %v", err), http.StatusBadGateway)
		return
	}

	// Build POST request to relay /connect with target in JSON body.
	bodyJSON, err := json.Marshal(connectBody{Target: target})
	if err != nil {
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}

	relayDomain := h.tunnelClient.RelayDomain()
	relayURL := "https://" + relayDomain + "/connect"

	// Use io.Pipe for upstream streaming: browser → pipe → relay request body.
	// The JSON target is prepended via MultiReader so the relay reads it first,
	// then the browser's upstream data flows through the same request body stream.
	upstreamPR, upstreamPW := io.Pipe()

	// Prepend JSON target to the upstream.
	prefixedReader := io.MultiReader(bytes.NewReader(bodyJSON), upstreamPR)

	// Use an independent context for the relay request. The browser's
	// r.Context() is cancelled by Go's http.Server when ServeHTTP returns
	// (after hijack + goroutine launch). If the relay request used r.Context(),
	// the HTTP/3 QUIC stream would be torn down as soon as the handler returns,
	// killing the bidirectional tunnel before any data flows.
	relayCtx, relayCancel := context.WithCancel(context.Background())

	relayReq, err := http.NewRequestWithContext(relayCtx, http.MethodPost, relayURL, prefixedReader)
	if err != nil {
		relayCancel()
		upstreamPW.Close()
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}
	relayReq.Header.Set("Authorization", "Bearer "+h.tunnelClient.SessionToken())
	relayReq.Header.Set("Content-Type", "application/json")

	// Send to relay via the tunnel's HTTP/3 client.
	relayResp, err := h.tunnelClient.HTTPClient().Do(relayReq)
	if err != nil {
		relayCancel()
		upstreamPW.Close()
		http.Error(w, fmt.Sprintf("Relay Error: %v", err), http.StatusBadGateway)
		return
	}

	if relayResp.StatusCode != http.StatusOK {
		relayCancel()
		upstreamPW.Close()
		relayResp.Body.Close()
		http.Error(w, fmt.Sprintf("Relay returned %d", relayResp.StatusCode), relayResp.StatusCode)
		return
	}

	// Hijack the browser connection for raw TCP access.
	hj, ok := w.(http.Hijacker)
	if !ok {
		relayCancel()
		upstreamPW.Close()
		relayResp.Body.Close()
		http.Error(w, "Hijack not supported", http.StatusInternalServerError)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		relayCancel()
		upstreamPW.Close()
		relayResp.Body.Close()
		return
	}

	h.wg.Add(1)

	go func() {
		defer h.wg.Done()
		defer relayCancel()
		defer conn.Close()
		defer relayResp.Body.Close()
		defer upstreamPW.Close()

		// Register connection for volume-based force-close.
		if h.volumeTracker != nil {
			connID := h.volumeTracker.Register(target, conn)
			defer h.volumeTracker.Unregister(target, connID)
		}

		// Send 200 Connection Established to the browser.
		bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		bufrw.Flush()

		// Bidirectional relay:
		// browser → upstream pipe → relay request body → destination
		// destination → relay response body → browser
		//
		// When either direction finishes (remote closes, browser disconnects),
		// force-close both ends to unblock the other goroutine. Without this,
		// the surviving io.Copy blocks indefinitely because hijacked connections
		// have no read deadline, causing CLOSE_WAIT accumulation and goroutine
		// leaks during kill switch cycles.
		done := make(chan struct{}, 2)

		// Relay response → browser (with volume counting).
		var downstream io.Reader = relayResp.Body
		if h.volumeTracker != nil {
			downstream = h.volumeTracker.WrapReader(target, relayResp.Body)
		}
		go func() {
			io.Copy(conn, downstream)
			done <- struct{}{}
		}()

		// Browser → relay request body
		go func() {
			io.Copy(upstreamPW, conn)
			done <- struct{}{}
		}()

		// Wait for first direction to finish, then force-unblock the other.
		<-done
		conn.Close()
		relayResp.Body.Close()
		upstreamPW.Close()
		<-done
	}()
}

// tryDirectBypass attempts a direct TCP connection to the target, bypassing
// the relay. Returns true if the bypass succeeded (connection established
// and data relayed), false if the direct dial failed (caller should fall
// through to the relay path).
//
// By design, bypassed connections are NOT volume-tracked: they don't consume
// relay quota, so counting them would be misleading. The user's real IP is
// exposed to the destination (accepted trade-off).
func (h *connectHandler) tryDirectBypass(w http.ResponseWriter, r *http.Request, target string) bool {
	destConn, err := net.DialTimeout("tcp", target, BypassDialTimeout)
	if err != nil {
		if h.volumeTracker != nil {
			h.volumeTracker.RecordDirectFailure(target)
		}
		return false
	}

	// Hijack the browser connection for raw TCP access.
	hj, ok := w.(http.Hijacker)
	if !ok {
		destConn.Close()
		return false
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		destConn.Close()
		return false
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer conn.Close()
		defer destConn.Close()

		bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		bufrw.Flush()

		// Use bufrw (not raw conn) for browser→dest to drain any buffered
		// bytes (e.g. TLS ClientHello) that were read before hijack.
		done := make(chan struct{}, 2)
		go func() { io.Copy(destConn, bufrw); done <- struct{}{} }()
		go func() { io.Copy(conn, destConn); done <- struct{}{} }()
		<-done
		destConn.Close()
		conn.Close()
		<-done
	}()

	return true
}

// handleHTTP forwards plain HTTP requests through the relay tunnel.
// The request is tunneled via CONNECT to the destination host, then the
// original HTTP request is sent through the tunnel.
func (h *connectHandler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract target from the absolute URL (e.g., http://example.com/path).
	if !r.URL.IsAbs() {
		http.Error(w, "Bad Request: expected absolute URL", http.StatusBadRequest)
		return
	}

	host := r.URL.Hostname()
	port := r.URL.Port()
	if port == "" {
		port = "80"
	}
	target := net.JoinHostPort(host, port)

	// Ensure session token is fresh.
	if err := h.tunnelClient.EnsureSessionToken(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("Proxy Authentication Failed: %v", err), http.StatusBadGateway)
		return
	}

	// Open a relay tunnel to the target.
	bodyJSON, err := json.Marshal(connectBody{Target: target})
	if err != nil {
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}

	relayDomain := h.tunnelClient.RelayDomain()
	relayURL := "https://" + relayDomain + "/connect"

	// For plain HTTP, we send the original request through the tunnel.
	// Serialize the request into the upstream pipe after the JSON target.
	upstreamPR, upstreamPW := io.Pipe()
	prefixedReader := io.MultiReader(bytes.NewReader(bodyJSON), upstreamPR)

	// Use an independent context — same reason as handleConnect: r.Context()
	// is cancelled when ServeHTTP returns after hijack.
	relayCtx, relayCancel := context.WithCancel(context.Background())

	relayReq, err := http.NewRequestWithContext(relayCtx, http.MethodPost, relayURL, prefixedReader)
	if err != nil {
		relayCancel()
		upstreamPW.Close()
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}
	relayReq.Header.Set("Authorization", "Bearer "+h.tunnelClient.SessionToken())
	relayReq.Header.Set("Content-Type", "application/json")

	relayResp, err := h.tunnelClient.HTTPClient().Do(relayReq)
	if err != nil {
		relayCancel()
		upstreamPW.Close()
		http.Error(w, fmt.Sprintf("Relay Error: %v", err), http.StatusBadGateway)
		return
	}

	if relayResp.StatusCode != http.StatusOK {
		relayCancel()
		upstreamPW.Close()
		relayResp.Body.Close()
		http.Error(w, fmt.Sprintf("Relay returned %d", relayResp.StatusCode), relayResp.StatusCode)
		return
	}

	// Write the original HTTP request into the tunnel.
	// Convert proxy request to a direct request (relative URL, no Proxy headers).
	outReq := r.Clone(context.Background())
	outReq.URL.Scheme = ""
	outReq.URL.Host = ""
	outReq.RequestURI = outReq.URL.RequestURI()
	outReq.Header.Del("Proxy-Connection")
	outReq.Header.Del("Proxy-Authorization")
	// Ensure Host header is set.
	if outReq.Header.Get("Host") == "" {
		outReq.Header.Set("Host", host)
	}
	// Close connection after response (no keep-alive through the tunnel).
	outReq.Header.Set("Connection", "close")

	// Read the HTTP response from the relay tunnel and forward to the client.
	// Hijack to get raw access for streaming the response.
	hj, ok := w.(http.Hijacker)
	if !ok {
		relayCancel()
		upstreamPW.Close()
		relayResp.Body.Close()
		http.Error(w, "Hijack not supported", http.StatusInternalServerError)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		relayCancel()
		upstreamPW.Close()
		relayResp.Body.Close()
		return
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer relayCancel()
		defer conn.Close()
		defer relayResp.Body.Close()
		defer upstreamPW.Close()

		// Write the original HTTP request into the tunnel.
		reqDone := make(chan struct{})
		go func() {
			defer close(reqDone)
			outReq.Write(upstreamPW)
			upstreamPW.Close()
		}()

		// Copy the raw HTTP response from the tunnel to the browser.
		if bufrw.Writer.Buffered() > 0 {
			bufrw.Flush()
		}
		io.Copy(conn, relayResp.Body)

		// Response finished — force-close to unblock outReq.Write if stuck.
		conn.Close()
		upstreamPW.Close()
		<-reqDone
	}()
}

