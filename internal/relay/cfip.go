package relay

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"
)

// maxCIDRResponseBytes limits Cloudflare CIDR list response to 1 MB.
const maxCIDRResponseBytes = 1 << 20

// cfIPv4URLs and cfIPv6URLs are the Cloudflare IP range endpoints.
const (
	cfIPv4URL = "https://www.cloudflare.com/ips-v4"
	cfIPv6URL = "https://www.cloudflare.com/ips-v6"
)

// minExpectedIPv4Ranges is the minimum number of IPv4 ranges expected from
// a valid Cloudflare response. If fewer are returned, the fetch is rejected.
const minExpectedIPv4Ranges = 10

// staleThreshold is the maximum time since last successful refresh before
// a warning is logged.
const staleThreshold = 30 * 24 * time.Hour

// defaultCFIPv4Ranges are the hardcoded fallback Cloudflare IPv4 ranges (as of 2026-03).
var defaultCFIPv4Ranges = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
}

// defaultCFIPv6Ranges are the hardcoded fallback Cloudflare IPv6 ranges.
var defaultCFIPv6Ranges = []string{
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
}

// CloudflareIPValidator validates that requests originate from Cloudflare
// edge servers and extracts the real client IP from CF-Connecting-IP.
type CloudflareIPValidator struct {
	ranges      atomic.Pointer[[]netip.Prefix]
	lastRefresh atomic.Int64 // unix timestamp of last successful refresh
	insecure    bool         // if true, trust non-CF sources (dev only)
	logFunc     func(format string, args ...any)
}

// NewCloudflareIPValidator creates a validator with hardcoded fallback ranges.
// Set insecure=true for development (trusts all sources).
func NewCloudflareIPValidator(insecure bool, logFunc func(string, ...any)) *CloudflareIPValidator {
	v := &CloudflareIPValidator{
		insecure: insecure,
		logFunc:  logFunc,
	}
	// Parse hardcoded ranges as initial fallback.
	initial := parsePrefixes(append(defaultCFIPv4Ranges, defaultCFIPv6Ranges...))
	v.ranges.Store(&initial)
	v.lastRefresh.Store(time.Now().Unix())
	return v
}

// IsTrustedSource returns true if remoteAddr belongs to Cloudflare IP ranges.
func (v *CloudflareIPValidator) IsTrustedSource(remoteAddr string) bool {
	if v.insecure {
		return true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ranges := v.ranges.Load()
	if ranges == nil {
		return false
	}
	for _, prefix := range *ranges {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// IsInsecure reports whether the validator is in dev/insecure mode.
func (v *CloudflareIPValidator) IsInsecure() bool {
	return v.insecure
}

// ExtractClientIP extracts the real client IP from the request.
// If the source is trusted (Cloudflare), uses CF-Connecting-IP.
// Otherwise falls back to RemoteAddr (or rejects in strict mode).
//
// NFR20: returned errors NEVER contain the client IP — log-safe.
func (v *CloudflareIPValidator) ExtractClientIP(r *http.Request) (string, error) {
	if v.IsTrustedSource(r.RemoteAddr) {
		cfIP := r.Header.Get("CF-Connecting-IP")
		if cfIP != "" {
			// Validate it's a real IP
			if _, err := netip.ParseAddr(cfIP); err != nil {
				return "", fmt.Errorf("cfip: invalid CF-Connecting-IP")
			}
			return cfIP, nil
		}
	}
	if v.insecure {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr, nil
		}
		return host, nil
	}
	return "", fmt.Errorf("cfip: untrusted source")
}

// StartRefresh starts a goroutine that refreshes Cloudflare IP ranges every 24h.
func (v *CloudflareIPValidator) StartRefresh(ctx context.Context) {
	// Do an initial refresh immediately.
	v.refresh()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			v.refresh()
			// Check staleness.
			last := time.Unix(v.lastRefresh.Load(), 0)
			if time.Since(last) > staleThreshold {
				if v.logFunc != nil {
					v.logFunc("cfip: WARNING: Cloudflare IP ranges not refreshed for >30 days")
				}
			}
		}
	}
}

func (v *CloudflareIPValidator) refresh() {
	client := &http.Client{Timeout: 10 * time.Second}

	v4, err := fetchCIDRList(client, cfIPv4URL)
	if err != nil {
		if v.logFunc != nil {
			v.logFunc("cfip: refresh IPv4 failed: %v", err)
		}
		return
	}
	v6, err := fetchCIDRList(client, cfIPv6URL)
	if err != nil {
		if v.logFunc != nil {
			v.logFunc("cfip: refresh IPv6 failed: %v", err)
		}
		return
	}

	if len(v4) < minExpectedIPv4Ranges {
		if v.logFunc != nil {
			v.logFunc("cfip: refresh rejected: only %d IPv4 ranges (expected >=%d)", len(v4), minExpectedIPv4Ranges)
		}
		return
	}

	all := parsePrefixes(append(v4, v6...))
	v.ranges.Store(&all)
	v.lastRefresh.Store(time.Now().Unix())
}

func fetchCIDRList(client *http.Client, url string) ([]string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cfip: fetch %s: status %d", url, resp.StatusCode)
	}
	var lines []string
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxCIDRResponseBytes))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func parsePrefixes(cidrs []string) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, cidr := range cidrs {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		prefixes = append(prefixes, p)
	}
	return prefixes
}
