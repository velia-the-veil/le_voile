//go:build windows

package ctlauth

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// writeRestrictedFile writes the token to disk on Windows and applies an
// explicit DACL granting access only to LocalSystem (S-1-5-18) and the local
// Administrators group (S-1-5-32-544). Inheritance is disabled so a permissive
// %ProgramData% parent ACL cannot widen the file's effective permissions.
//
// Story 5.9 H1 fix — relying solely on parent ACL inheritance was insecure
// because %ProgramData% defaults grant Users:Read+Execute, which made the
// token readable by any non-elevated process.
func writeRestrictedFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("ctlauth: open: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("ctlauth: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("ctlauth: close: %w", err)
	}
	if err := applyRestrictiveDACL(path); err != nil {
		// On failure, remove the file so we don't leave a permissively-ACL'd
		// token on disk (parent inheritance might leak it to Users).
		_ = os.Remove(path)
		return fmt.Errorf("ctlauth: dacl: %w", err)
	}
	return nil
}

// applyRestrictiveDACL builds a DACL granting GENERIC_ALL only to LocalSystem
// and Administrators, marks it as PROTECTED (no inheritance from parent), and
// applies it to the file at path.
func applyRestrictiveDACL(path string) error {
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("create LocalSystem sid: %w", err)
	}
	adminsSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("create Administrators sid: %w", err)
	}

	access := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(systemSID),
			},
		},
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(adminsSID),
			},
		},
	}

	dacl, err := windows.ACLFromEntries(access, nil)
	if err != nil {
		return fmt.Errorf("acl from entries: %w", err)
	}

	// PROTECTED_DACL_SECURITY_INFORMATION blocks inheritance from %ProgramData%.
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("utf16 path: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		windows.UTF16PtrToString(pathPtr),
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	); err != nil {
		return fmt.Errorf("set named security info: %w", err)
	}
	return nil
}
