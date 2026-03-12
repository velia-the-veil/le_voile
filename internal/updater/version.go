// Package updater implements automatic update checking, downloading, and verification.
package updater

import "time"

// Version is set at build time via -ldflags:
//
//	-X github.com/velia-the-veil/le_voile/internal/updater.Version=X.Y.Z
var Version string

// CurrentVersion returns the current application version.
// Returns "dev" if Version was not set at build time.
func CurrentVersion() string {
	if Version == "" {
		return "dev"
	}
	return Version
}

// ReleaseInfo holds metadata about a GitHub release.
type ReleaseInfo struct {
	Version          string
	DownloadURL      string
	ChecksumURL  string
	SignatureURL string
	ReleaseNotes     string
	PublishedAt      time.Time
}
