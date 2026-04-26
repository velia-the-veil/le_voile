//go:build linux

package config

import _ "embed"

// exampleTOML is the bootstrap skeleton written on first start when no
// config exists at the target path. Kept in sync with the repository-root
// config.example.toml (enforced manually on edits; see README).
//
//go:embed config.example.toml
var exampleTOML []byte

// ExampleTOML returns the embedded default configuration bytes. Exported so
// tests and packaging scripts can snapshot the skeleton without reading the
// filesystem.
func ExampleTOML() []byte {
	out := make([]byte, len(exampleTOML))
	copy(out, exampleTOML)
	return out
}
