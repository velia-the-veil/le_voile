package captive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeResult indicates the outcome of a captive portal probe.
type ProbeResult int

const (
	// NoPortal means the network is open — no captive portal detected.
	NoPortal ProbeResult = iota
	// PortalDetected means the probe received a redirect or unexpected body,
	// indicating a captive portal is active.
	PortalDetected
	// ProbeError means the probe failed (timeout, DNS failure, no route).
	ProbeError
)

// String returns a human-readable name for the result.
func (r ProbeResult) String() string {
	switch r {
	case NoPortal:
		return "no_portal"
	case PortalDetected:
		return "portal_detected"
	default:
		return "probe_error"
	}
}

// ProbeDetail carries context about a probe attempt.
type ProbeDetail struct {
	Result     ProbeResult
	URL        string // URL that was probed
	StatusCode int    // HTTP status observed (0 on error)
	Err        error  // underlying error, if any
}

// DefaultProbeURLs are the well-known captive portal detection endpoints.
// Apple's is tried first because it's the most universally intercepted.
var DefaultProbeURLs = []string{
	"http://captive.apple.com/hotspot-detect.html",
	"http://connectivitycheck.gstatic.com/generate_204",
}

// DefaultTimeout is the per-URL HTTP timeout for captive portal probes.
const DefaultTimeout = 3 * time.Second

// Probe checks whether the current network has a captive portal by sending
// HTTP requests to the given URLs (or DefaultProbeURLs if urls is nil).
//
// It tries each URL in order and returns on the first conclusive result.
// A conclusive result is either NoPortal or PortalDetected. If all URLs
// fail with errors/timeouts, ProbeError is returned.
func Probe(ctx context.Context, urls []string) ProbeDetail {
	if len(urls) == 0 {
		urls = DefaultProbeURLs
	}

	var lastDetail ProbeDetail
	for _, u := range urls {
		detail := probeOne(ctx, u)
		if detail.Result != ProbeError {
			return detail
		}
		lastDetail = detail
	}
	return lastDetail
}

// probeOne sends a single HTTP GET and analyses the response.
func probeOne(ctx context.Context, url string) ProbeDetail {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	// Build a client that does NOT follow redirects — a 30x is the captive
	// portal signal we're looking for.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ProbeDetail{Result: ProbeError, URL: url, Err: fmt.Errorf("captive: build request: %w", err)}
	}

	resp, err := client.Do(req)
	if err != nil {
		return ProbeDetail{Result: ProbeError, URL: url, Err: fmt.Errorf("captive: probe %s: %w", url, err)}
	}
	defer resp.Body.Close()

	// Read body (cap at 4 KB to avoid abuse).
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	bodyStr := strings.TrimSpace(string(body))

	detail := ProbeDetail{URL: url, StatusCode: resp.StatusCode}

	// 30x redirect → captive portal.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		detail.Result = PortalDetected
		return detail
	}

	// Evaluate based on which probe URL was used.
	switch {
	case strings.Contains(url, "captive.apple.com"):
		// Apple: expects body "Success" and status 200.
		if resp.StatusCode == 200 && strings.Contains(bodyStr, "Success") {
			detail.Result = NoPortal
		} else {
			detail.Result = PortalDetected
		}
	case strings.Contains(url, "generate_204"):
		// Google: expects status 204 with empty body.
		if resp.StatusCode == 204 {
			detail.Result = NoPortal
		} else {
			detail.Result = PortalDetected
		}
	default:
		// Unknown URL pattern — 200 is "probably ok", anything else is suspect.
		if resp.StatusCode == 200 {
			detail.Result = NoPortal
		} else {
			detail.Result = PortalDetected
		}
	}

	return detail
}
