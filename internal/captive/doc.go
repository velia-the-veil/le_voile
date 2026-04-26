// Package captive detects Wi-Fi captive portals via HTTP probes (RFC 7710).
//
// The Probe function sends HTTP GET requests to well-known URLs (Apple
// hotspot-detect, Google generate_204) and analyses the response to determine
// whether a captive portal redirect is active. It intentionally uses plain
// HTTP — most captive portals intercept HTTP but pass HTTPS through.
//
// This package is OS-agnostic; platform-specific firewall/routing adjustments
// live in {windows,linux}/internal/firewall and {windows,linux}/internal/service.
package captive
