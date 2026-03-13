package httpproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
)

// connectHandler handles incoming HTTP CONNECT requests from browsers.
type connectHandler struct {
	tunnelClient TunnelClient
	wg           *sync.WaitGroup
}

// connectBody is the JSON body sent to the relay /connect endpoint.
type connectBody struct {
	Target string `json:"target"`
}

func (h *connectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only CONNECT method allowed. Non-CONNECT: silent TCP RST.
	if r.Method != http.MethodConnect {
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			if conn != nil {
				conn.Close()
			}
		}
		return
	}

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

	// Create the relay request using the tunnel's HTTP/3 client.
	// Use io.Pipe for bidirectional streaming: we write browser data into the pipe,
	// the HTTP/3 client sends it as the request body to the relay.
	pr, pw := io.Pipe()

	relayReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, relayURL, pr)
	if err != nil {
		pw.Close()
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}
	relayReq.Header.Set("Authorization", "Bearer "+h.tunnelClient.SessionToken())
	relayReq.Header.Set("Content-Type", "application/octet-stream")

	// First, send the JSON target as the beginning of the body.
	// The relay reads the JSON target, then streams the rest bidirectionally.
	// Actually, per the spec, the target is in the POST body JSON and the relay
	// opens a TCP connection and relays. The HTTP/3 body is then used for streaming.
	// We need a different approach: send target as JSON first, then stream.
	// But this complicates the protocol. Let's use a simpler approach:
	// The relay reads the full JSON body to get the target, dials, and then
	// the response body is the downstream data, and we need an upstream pipe.
	//
	// Actually looking at the relay handler implementation, it reads the body as JSON,
	// dials, then responds 200 and relays via r.Body (upstream) and w (downstream).
	// So the client needs to:
	// 1. Send JSON target in the request body
	// 2. After relay responds 200, relay browser data through the request body
	// 3. Read relay response body for downstream data
	//
	// The challenge: we can't write JSON first and then stream through the same body.
	// Solution: Use two separate phases. The request body contains only the JSON target.
	// The relay uses response body for downstream and we don't need upstream through body.
	// Wait - this is HTTP/3 streaming. The relay reads the request body as a stream.
	//
	// Let me reconsider the architecture: for HTTP CONNECT proxy, we need full-duplex.
	// With HTTP/3:
	// - Client → Relay: request body (stream)
	// - Relay → Client: response body (stream)
	//
	// We prepend the JSON target to the request body, then stream browser data after it.
	pw.Close() // close unused pipe

	// Simpler approach: send target as JSON body, relay connects and streams back.
	// For upstream (browser → destination), we use a fresh pipe.
	upstreamPR, upstreamPW := io.Pipe()

	// Prepend JSON target to the upstream.
	prefixedReader := io.MultiReader(bytes.NewReader(bodyJSON), upstreamPR)

	relayReq2, err := http.NewRequestWithContext(r.Context(), http.MethodPost, relayURL, prefixedReader)
	if err != nil {
		upstreamPW.Close()
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}
	relayReq2.Header.Set("Authorization", "Bearer "+h.tunnelClient.SessionToken())
	relayReq2.Header.Set("Content-Type", "application/json")

	// Send to relay via the tunnel's HTTP/3 client.
	relayResp, err := h.tunnelClient.HTTPClient().Do(relayReq2)
	if err != nil {
		upstreamPW.Close()
		http.Error(w, fmt.Sprintf("Relay Error: %v", err), http.StatusBadGateway)
		return
	}

	if relayResp.StatusCode != http.StatusOK {
		upstreamPW.Close()
		relayResp.Body.Close()
		http.Error(w, fmt.Sprintf("Relay returned %d", relayResp.StatusCode), relayResp.StatusCode)
		return
	}

	// Hijack the browser connection for raw TCP access.
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstreamPW.Close()
		relayResp.Body.Close()
		http.Error(w, "Hijack not supported", http.StatusInternalServerError)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		upstreamPW.Close()
		relayResp.Body.Close()
		return
	}

	h.wg.Add(1)

	go func() {
		defer h.wg.Done()
		defer conn.Close()
		defer relayResp.Body.Close()
		defer upstreamPW.Close()

		// Send 200 Connection Established to the browser.
		bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		bufrw.Flush()

		// Bidirectional relay:
		// browser → upstream pipe → relay request body → destination
		// destination → relay response body → browser
		done := make(chan struct{}, 2)

		// Relay response → browser
		go func() {
			io.Copy(conn, relayResp.Body)
			done <- struct{}{}
		}()

		// Browser → relay request body
		go func() {
			io.Copy(upstreamPW, conn)
			upstreamPW.Close()
			done <- struct{}{}
		}()

		// Wait for either direction to finish.
		<-done
	}()
}
