//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// TestApplyRestrictedPerms_OwnerCanStillRead is the minimal regression
// guard for the Windows DACL path: after Save tightens the ACL, the same
// process (owner) must still be able to read the file back. The service
// needs this for Load-on-boot; a DACL so tight it locks out even the
// writer would break the service on every restart.
func TestApplyRestrictedPerms_OwnerCanStillRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		TUN:  TUNConfig{Name: "levoile0", MTU: 1420},
		STUN: STUNConfig{DefaultServer: "stun.example:19302"},
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Owner must still be able to read — this is the round-trip the service
	// depends on at boot (Load reads the config it just wrote).
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("owner ReadFile after Save: %v (DACL too tight?)", err)
	}

	// Reload through the normal Load path — catches DACL errors that
	// manifest only via toml.DecodeFile's internal os.Open.
	if _, err := Load(path); err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
}

// TestApplyRestrictedPerms_DACLIsProtected confirms the
// PROTECTED_DACL_SECURITY_INFORMATION flag was set on the file: the DACL
// is NOT inheriting from the parent dir. Regressions here would silently
// widen effective perms if the parent %AppData%\LeVoile\ ever acquires a
// permissive inherited ACE (e.g. during an upgrade or backup restore).
func TestApplyRestrictedPerms_DACLIsProtected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{TUN: TUNConfig{Name: "levoile0", MTU: 1420}}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo: %v", err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatalf("sd.Control: %v", err)
	}
	// SE_DACL_PROTECTED = 0x1000 (the flag SetNamedSecurityInfo set via
	// PROTECTED_DACL_SECURITY_INFORMATION).
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("DACL is not protected (control=0x%x, missing SE_DACL_PROTECTED=0x1000)", control)
	}
}
