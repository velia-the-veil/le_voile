//go:build windows

package ipchandler

import (
	"os"
	"testing"
)

// TestMain flips the package-internal testBypassAuthGate so the historical
// test suite — authored before the 2026-04 strict-auth flip — keeps passing
// empty-Auth mutating requests through the gate. Tests that specifically
// exercise the strict path (strict_auth_test.go) flip the variable back to
// false inside their own bodies. The variable is package-internal and not
// driven by environment, so production binaries cannot reach this state.
func TestMain(m *testing.M) {
	testBypassAuthGate = true
	os.Exit(m.Run())
}
