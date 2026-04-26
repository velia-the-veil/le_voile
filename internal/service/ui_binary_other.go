//go:build linux

package service

// uiBinaryName is the on-disk filename of the supervised UI process.
// Non-Windows installs delegate supervision to systemd user units, so
// this constant exists only to keep deriveUIBinaryPath compilable on
// every OS — the value is never used to spawn anything from Go on Linux.
const uiBinaryName = "levoile-ui"
