//go:build windows

package dns

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// applyRestrictedPerms sets a protected DACL on the file granting access
// only to LocalSystem (SYSTEM) and the local Administrators group.
// Inheritance is blocked so a permissive %ProgramData% parent ACL cannot
// widen the file's effective permissions.
//
// Audit fix F-11 (2026-05-04): the persisted DNS state file
// (dns-original.json) lists the user's network interfaces and their
// pre-tunnel resolvers, which is profiling-grade information for an
// attacker on the same machine. Mirrors the pattern used by
// windows/internal/config/perms_windows.go and ctlauth/perms_windows.go.
func applyRestrictedPerms(path string) error {
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("dns: create LocalSystem sid: %w", err)
	}
	adminsSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("dns: create Administrators sid: %w", err)
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
		return fmt.Errorf("dns: acl from entries: %w", err)
	}

	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	); err != nil {
		return fmt.Errorf("dns: set named security info: %w", err)
	}
	return nil
}
