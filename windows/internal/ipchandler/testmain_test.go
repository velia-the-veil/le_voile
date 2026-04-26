//go:build windows

package ipchandler

import (
	"os"
	"testing"
)

// TestMain sets LEVOILE_IPC_LEGACY_AUTH=1 for the whole package so tests
// authored before the 2026-04 strict-auth flip keep passing empty-Auth
// requests through the gate. Tests that specifically exercise the strict
// path (strict_auth_test.go) unset or override the variable inside their
// own body via t.Setenv / os.Unsetenv, which take precedence because they
// run under t.Run scope.
func TestMain(m *testing.M) {
	os.Setenv("LEVOILE_IPC_LEGACY_AUTH", "1")
	os.Exit(m.Run())
}
