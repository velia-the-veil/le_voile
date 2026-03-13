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

	// Use io.Pipe for upstream streaming: browser → pipe → relay request body.
	// The JSON target is prepended via MultiReader so the relay reads it first,
	// then the browser's upstream data flows through the same request body stream.
	upstreamPR, upstreamPW := io.Pipe()

	// Prepend JSON target to the upstream.
	prefixedReader := io.MultiReader(bytes.NewReader(bodyJSON), upstreamPR)

	relayReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, relayURL, prefixedReader)
	if err != nil {
		upstreamPW.Close()
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}
	relayReq.Header.Set("Authorization", "Bearer "+h.tunnelClient.SessionToken())
	relayReq.Header.Set("Content-Type", "application/json")

	// Send to relay via the tunnel's HTTP/3 client.
	relayResp, err := h.tunnelClient.HTTPClient().Do(relayReq)
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

		// Send 200 Connection Established to the browser.
		bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		bufrw.Flush()

		// Bidirectional relay:
		// browser → upstream pipe → relay request body → destination
		// destination → relay response body → browser
		var relayWg sync.WaitGroup
		relayWg.Add(2)

		// Relay response → browser
		go func() {
			defer relayWg.Done()
			io.Copy(conn, relayResp.Body)
		}()

		// Browser → relay request body
		go func() {
			defer relayWg.Done()
			defer upstreamPW.Close() // signals EOF to relay request body
			io.Copy(upstreamPW, conn)
		}()

		// Wait for BOTH directions to finish.
		relayWg.Wait()
	}()
}
